package wasmhost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/rapatel0/alpha/internal/debuglog"
	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools"
)

const (
	hostModule  = "alpha"
	exportInit  = "alpha_plugin_init"
	exportCmd   = "alpha_plugin_command"
	exportTool  = "alpha_plugin_tool"
	argsScratch = 4096
	argsMax     = 16384
)

type toolSpec struct {
	name   string
	desc   string
	schema string
}

type loaded struct {
	name     string
	mod      api.Module
	mem      api.Memory
	cmd      api.Function
	toolFn   api.Function
	mu       sync.Mutex
	toast    string
	result   string
	nextID   int32
	cmdName  map[int32]string
	cmdDesc  map[int32]string
	toolSpec map[int32]toolSpec
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
		name:     name,
		cmdName:  map[int32]string{},
		cmdDesc:  map[int32]string{},
		toolSpec: map[int32]toolSpec{},
	}

	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(32),
	)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		_ = rt.Close(ctx)
		return fmt.Errorf("wasi: %w", err)
	}
	_, err = rt.NewHostModuleBuilder(hostModule).
		NewFunctionBuilder().WithFunc(plug.registerCommand).Export("register_command").
		NewFunctionBuilder().WithFunc(plug.registerTool).Export("register_tool").
		NewFunctionBuilder().WithFunc(plug.setToast).Export("set_toast").
		NewFunctionBuilder().WithFunc(plug.setResult).Export("set_result").
		NewFunctionBuilder().WithFunc(plug.log).Export("log").
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
	plug.toolFn = mod.ExportedFunction(exportTool)
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
	for id, spec := range p.toolSpec {
		id, spec := id, spec
		params := parseSchema(spec.schema)
		h.RegisterTool(tools.Tool{
			Definition: llm.ToolDefinition{
				Name:        spec.name,
				Description: spec.desc,
				Params:      params,
			},
			Run: func(ctx context.Context, input json.RawMessage) (tools.Result, error) {
				return p.runTool(ctx, id, input)
			},
		})
	}
	return nil
}

func parseSchema(schema string) *llm.FunctionParameters {
	params := &llm.FunctionParameters{Type: "object", Properties: llm.Object{}}
	if strings.TrimSpace(schema) == "" {
		return params
	}
	if err := json.Unmarshal([]byte(schema), params); err != nil {
		return params
	}
	if params.Type == "" {
		params.Type = "object"
	}
	if params.Properties == nil {
		params.Properties = llm.Object{}
	}
	return params
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

func (p *loaded) registerTool(_ context.Context, namePtr, nameLen, descPtr, descLen, schemaPtr, schemaLen uint32) uint32 {
	name, ok := p.read(namePtr, nameLen)
	if !ok || strings.TrimSpace(name) == "" {
		return 0
	}
	desc, _ := p.read(descPtr, descLen)
	schema, _ := p.read(schemaPtr, schemaLen)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	id := uint32(p.nextID)
	p.toolSpec[int32(id)] = toolSpec{
		name:   strings.TrimSpace(name),
		desc:   strings.TrimSpace(desc),
		schema: schema,
	}
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

func (p *loaded) setResult(_ context.Context, ptr, length uint32) {
	s, ok := p.read(ptr, length)
	if !ok {
		return
	}
	p.mu.Lock()
	p.result = s
	p.mu.Unlock()
}

func (p *loaded) log(_ context.Context, ptr, length uint32) {
	s, ok := p.read(ptr, length)
	if !ok {
		return
	}
	debuglog.Logf("wasm %s: %s", p.name, s)
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

func (p *loaded) writeScratch(b []byte) (ptr, length uint32, ok bool) {
	if p.mem == nil {
		return 0, 0, false
	}
	if len(b) > argsMax {
		b = b[:argsMax]
	}
	if !p.mem.Write(argsScratch, b) {
		return 0, 0, false
	}
	return argsScratch, uint32(len(b)), true
}

func (p *loaded) runCommand(ctx context.Context, id int32, args []string) (hooks.CommandResult, error) {
	raw := []byte(strings.Join(args, " "))
	p.mu.Lock()
	p.toast = ""
	cmd := p.cmd
	ptr, n, ok := p.writeScratch(raw)
	p.mu.Unlock()
	if cmd == nil {
		return hooks.CommandResult{}, nil
	}
	if !ok && len(raw) > 0 {
		return hooks.CommandResult{}, fmt.Errorf("wasm %s: cannot write args", p.name)
	}
	if _, err := cmd.Call(ctx, uint64(uint32(id)), uint64(ptr), uint64(n)); err != nil {
		return hooks.CommandResult{}, err
	}
	p.mu.Lock()
	toast := p.toast
	p.mu.Unlock()
	return hooks.CommandResult{Toast: toast}, nil
}

func (p *loaded) runTool(ctx context.Context, id int32, input json.RawMessage) (tools.Result, error) {
	p.mu.Lock()
	p.result = ""
	fn := p.toolFn
	ptr, n, ok := p.writeScratch(input)
	p.mu.Unlock()
	if fn == nil {
		return tools.Result{}, fmt.Errorf("wasm %s: no tool export", p.name)
	}
	if !ok && len(input) > 0 {
		return tools.Result{}, fmt.Errorf("wasm %s: cannot write args", p.name)
	}
	if _, err := fn.Call(ctx, uint64(uint32(id)), uint64(ptr), uint64(n)); err != nil {
		return tools.Result{}, err
	}
	p.mu.Lock()
	body := p.result
	p.mu.Unlock()
	return tools.Result{Content: body, Output: body}, nil
}
