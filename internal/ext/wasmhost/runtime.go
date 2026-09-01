package wasmhost

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"github.com/rapatel0/alpha/internal/debuglog"
	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
)

const (
	hostModule = "alpha"
	exportInit = "alpha_plugin_init"
	exportCmd  = "alpha_plugin_command"
)

type loaded struct {
	name    string
	mod     api.Module
	mem     api.Memory
	cmd     api.Function
	mu      sync.Mutex
	toast   string
	nextID  int32
	cmdName map[int32]string
	cmdDesc map[int32]string
}

var loadOnce sync.Once

// LoadDefault loads user and project WASM plugins onto the process host once.
func LoadDefault(cwd string) {
	loadOnce.Do(func() {
		_ = Load(context.Background(), ext.Default(), Dirs(cwd))
	})
}

// Load instantiates every *.wasm under dirs onto h. A bad module is skipped.
func Load(ctx context.Context, h *ext.Host, dirs []string) error {
	if h == nil {
		return nil
	}
	for _, path := range Files(dirs) {
		if err := loadOne(ctx, h, path); err != nil {
			debuglog.Logf("wasm plugin %s: %v", path, err)
		}
	}
	return nil
}

func loadOne(ctx context.Context, h *ext.Host, path string) error {
	bin, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	plug := &loaded{
		name:    name,
		cmdName: map[int32]string{},
		cmdDesc: map[int32]string{},
	}

	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(32),
	)
	_, err = rt.NewHostModuleBuilder(hostModule).
		NewFunctionBuilder().WithFunc(plug.registerCommand).Export("register_command").
		NewFunctionBuilder().WithFunc(plug.setToast).Export("set_toast").
		Instantiate(ctx)
	if err != nil {
		_ = rt.Close(ctx)
		return fmt.Errorf("host: %w", err)
	}
	mod, err := rt.InstantiateWithConfig(ctx, bin, wazero.NewModuleConfig().WithName(name))
	if err != nil {
		_ = rt.Close(ctx)
		return fmt.Errorf("instantiate: %w", err)
	}
	plug.mod = mod
	mem := mod.Memory()
	if mem == nil {
		_ = rt.Close(ctx)
		return fmt.Errorf("missing memory export")
	}
	plug.mem = mem
	initFn := mod.ExportedFunction(exportInit)
	if initFn == nil {
		_ = rt.Close(ctx)
		return fmt.Errorf("missing %s", exportInit)
	}
	results, err := initFn.Call(ctx)
	if err != nil {
		_ = rt.Close(ctx)
		return fmt.Errorf("init: %w", err)
	}
	if len(results) > 0 && results[0] != 0 {
		_ = rt.Close(ctx)
		return fmt.Errorf("init status %d", results[0])
	}
	plug.cmd = mod.ExportedFunction(exportCmd)
	return h.Add(plug)
}

func (p *loaded) Name() string { return "wasm:" + p.name }

func (p *loaded) Register(h *ext.Host) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, name := range p.cmdName {
		id, name, desc := id, name, p.cmdDesc[id]
		if desc == "" {
			desc = "wasm plugin " + p.name
		}
		h.RegisterCommand(ext.Command{
			Name:        name,
			Description: desc,
			Run: func(ctx context.Context, args []string) (hooks.CommandResult, error) {
				return p.runCommand(ctx, id, args)
			},
		})
	}
	return nil
}

func (p *loaded) registerCommand(_ context.Context, namePtr, nameLen, descPtr, descLen uint32) uint32 {
	name, ok := p.read(namePtr, nameLen)
	if !ok || strings.TrimSpace(name) == "" {
		return 0
	}
	desc, _ := p.read(descPtr, descLen)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	id := uint32(p.nextID)
	p.cmdName[int32(id)] = strings.TrimSpace(name)
	p.cmdDesc[int32(id)] = strings.TrimSpace(desc)
	return id
}

func (p *loaded) setToast(_ context.Context, ptr, length uint32) {
	s, ok := p.read(ptr, length)
	if !ok {
		return
	}
	p.mu.Lock()
	p.toast = s
	p.mu.Unlock()
}

func (p *loaded) read(ptr, length uint32) (string, bool) {
	if p.mem == nil {
		return "", false
	}
	b, ok := p.mem.Read(ptr, length)
	if !ok {
		return "", false
	}
	return string(b), true
}

func (p *loaded) runCommand(ctx context.Context, id int32, _ []string) (hooks.CommandResult, error) {
	p.mu.Lock()
	p.toast = ""
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil {
		return hooks.CommandResult{}, nil
	}
	if _, err := cmd.Call(ctx, uint64(uint32(id)), 0, 0); err != nil {
		return hooks.CommandResult{}, err
	}
	p.mu.Lock()
	toast := p.toast
	p.mu.Unlock()
	return hooks.CommandResult{Toast: toast}, nil
}
