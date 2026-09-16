package wasmhost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rapatel0/alpha/internal/brand"
	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/permission"
)

type bgPlugin struct{ *loaded }

func (b bgPlugin) SetGate(permission.Gate) {}

func (b bgPlugin) Start(ctx context.Context) {
	_, _ = b.call(ctx, "alpha_plugin_start")
}

func (b bgPlugin) Stop() {
	_, _ = b.call(context.Background(), "alpha_plugin_stop")
}

func (p *loaded) addFooter(context.Context) { p.wantFooter = true }

func (p *loaded) onBeforeAgentStart(context.Context) { p.wantPrompt = true }

func (p *loaded) onUsage(context.Context) { p.wantUsage = true }

func (p *loaded) enableBackground(context.Context) { p.wantBG = true }

func (p *loaded) onSession(_ context.Context, ptr, length uint32) {
	kind, ok := p.read(ptr, length)
	if !ok || kind == "" {
		return
	}
	k := hooks.Kind(kind)
	if !hooks.IsSessionKind(k) {
		return
	}
	p.sessionKinds = append(p.sessionKinds, k)
}

func (p *loaded) onTool(_ context.Context, ptr, length uint32, pre, post int32) {
	match, _ := p.read(ptr, length)
	p.toolWatch = append(p.toolWatch, toolWatch{match: match, pre: pre != 0, post: post != 0})
}

func (p *loaded) onToolResult(_ context.Context, ptr, length uint32) {
	match, _ := p.read(ptr, length)
	p.resultMatch = append(p.resultMatch, match)
}

func (p *loaded) setSubmit(_ context.Context, ptr, length uint32) {
	s, ok := p.read(ptr, length)
	if !ok {
		return
	}
	p.submit = s
}

func (p *loaded) setStatus(_ context.Context, ptr, length, set uint32) {
	s, _ := p.read(ptr, length)
	p.status = s
	p.statusSet = set != 0
}

func (p *loaded) setList(_ context.Context, ptr, length uint32) {
	s, ok := p.read(ptr, length)
	if !ok || s == "" {
		return
	}
	var list hooks.CommandList
	if json.Unmarshal([]byte(s), &list) != nil {
		return
	}
	p.list = &list
}

func (p *loaded) hostWake(_ context.Context, ptr, length uint32) int32 {
	text, _ := p.read(ptr, length)
	if p.host == nil {
		return 1
	}
	if err := p.host.Wake(text); err != nil {
		return 1
	}
	return 0
}

func (p *loaded) hostCompact(ctx context.Context) int32 {
	if p.host == nil {
		return 1
	}
	if err := p.host.Compact(ctx); err != nil {
		return 1
	}
	return 0
}

func (p *loaded) hostStartSide(ctx context.Context, promptPtr, promptLen, inherit, rolePtr, roleLen uint32) int32 {
	if p.host == nil {
		return 1
	}
	prompt, _ := p.read(promptPtr, promptLen)
	role, _ := p.read(rolePtr, roleLen)
	res, err := p.host.StartSide(ctx, ext.SideRequest{
		Prompt:  prompt,
		Inherit: inherit != 0,
		Role:    role,
	})
	raw, _ := json.Marshal(res)
	if err != nil {
		raw, _ = json.Marshal(map[string]string{"error": err.Error()})
		_ = p.writeAt(replyScratch, raw)
		return 1
	}
	_ = p.writeAt(replyScratch, raw)
	return 0
}

func (p *loaded) hostAskQuestion(ctx context.Context, hPtr, hLen, pPtr, pLen, oPtr, oLen uint32) int32 {
	if p.host == nil {
		return 1
	}
	header, _ := p.read(hPtr, hLen)
	prompt, _ := p.read(pPtr, pLen)
	optJSON, _ := p.read(oPtr, oLen)
	var opts []string
	if optJSON != "" {
		_ = json.Unmarshal([]byte(optJSON), &opts)
	}
	ans, err := p.host.AskQuestion(ctx, ext.Question{Header: header, Prompt: prompt, Options: opts})
	raw, _ := json.Marshal(ans)
	if err != nil {
		raw, _ = json.Marshal(map[string]string{"error": err.Error()})
		_ = p.writeAt(replyScratch, raw)
		return 1
	}
	_ = p.writeAt(replyScratch, raw)
	return 0
}

// hostModelInfo answers with a small description of the active model, so a
// guest can pick per-model text without parsing the config file. The API key is
// left out: a plugin that needs a credential uses the host calls instead.
// The return value is the JSON length; the bytes sit at replyScratch.
func (p *loaded) hostModelInfo() uint32 {
	if p.host == nil {
		return 0
	}
	raw := modelJSON(p.host.ModelInfo())
	if !p.writeAt(replyScratch, raw) {
		return 0
	}
	return replyLen(raw)
}

// hostReadAsset reads a file next to the module, which is how a plugin ships
// its own text: prompt fragments, templates, a small table. WASI is mounted
// without a filesystem, so this is the only read path a guest has.
func (p *loaded) hostReadAsset(_ context.Context, ptr, length uint32) uint32 {
	rel, ok := p.read(ptr, length)
	if !ok {
		return 0
	}
	full := assetPath(p.dir, rel)
	raw, err := os.ReadFile(full)
	if err != nil {
		return 0
	}
	if len(raw) > argsMax {
		raw = raw[:argsMax]
	}
	if !p.writeAt(replyScratch, raw) {
		return 0
	}
	return replyLen(raw)
}

// hostReadFile reads from the plugin's own state directory.
func (p *loaded) hostReadFile(_ context.Context, ptr, length uint32) uint32 {
	rel, ok := p.read(ptr, length)
	if !ok {
		return 0
	}
	raw, err := os.ReadFile(statePath(pluginHome(), p.name, rel))
	if err != nil {
		return 0
	}
	if len(raw) > argsMax {
		raw = raw[:argsMax]
	}
	if !p.writeAt(replyScratch, raw) {
		return 0
	}
	return replyLen(raw)
}

// hostWriteFile writes into the plugin's own state directory. Arguments are
// the relative path and the contents, in that order.
func (p *loaded) hostWriteFile(_ context.Context, ptr, length, valPtr, valLen uint32) int32 {
	rel, ok := p.read(ptr, length)
	body, okVal := p.read(valPtr, valLen)
	if !ok || !okVal {
		return 1
	}
	full := statePath(pluginHome(), p.name, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return 1
	}
	if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
		return 1
	}
	return 0
}

// hostActiveTools lists the tools the model is currently offered.
func (p *loaded) hostActiveTools() uint32 {
	if p.host == nil {
		return 0
	}
	raw, err := json.Marshal(p.host.ToolNames())
	if err != nil {
		return 0
	}
	if !p.writeAt(replyScratch, raw) {
		return 0
	}
	return replyLen(raw)
}

// hostSetActiveTools narrows the advertised tool set. An empty list restores
// every tool, so a plugin that mis-filters can always undo it.
func (p *loaded) hostSetActiveTools(_ context.Context, ptr, length uint32) int32 {
	text, ok := p.read(ptr, length)
	if !ok || p.host == nil {
		return 1
	}
	p.host.ApplyToolScope(scopeNames(text))
	return 0
}

// replyLen matches the truncation in writeAt, so the length a guest reads back
// is the length that was written.
func replyLen(b []byte) uint32 {
	if len(b) > argsMax {
		return argsMax
	}
	return uint32(len(b))
}

// modelJSON renders the model fields a plugin may branch on.
func modelJSON(cfg llm.ModelConfig) []byte {
	raw, err := json.Marshal(map[string]any{
		"name":           cfg.Name,
		"base_url":       cfg.BaseURL,
		"context_window": cfg.ContextWindow,
		"skill_path":     cfg.SkillPath,
	})
	if err != nil {
		return []byte(`{"name":""}`)
	}
	return raw
}

// scopeNames accepts either a JSON array or a comma-separated list, because a
// guest that only knows strings should not have to build JSON.
func scopeNames(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if strings.HasPrefix(text, "[") {
		var list []string
		if json.Unmarshal([]byte(text), &list) == nil {
			return list
		}
	}
	parts := strings.Split(text, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// assetPath keeps a guest inside the directory that holds its own module.
func assetPath(dir, rel string) string {
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, filepath.Clean("/"+strings.TrimSpace(rel)))
}

// statePath keeps plugin state under ~/.alpha/plugins/<name>/.
func statePath(home, name, rel string) string {
	if home == "" {
		home = "."
	}
	return filepath.Join(brand.HomeDir(home), "plugins", name, filepath.Clean("/"+strings.TrimSpace(rel)))
}

// pluginHome roots plugin state in the user's alpha directory.
func pluginHome() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	return "."
}

func (p *loaded) writeAt(off uint32, b []byte) bool {
	if p.mem == nil {
		return false
	}
	if len(b) > argsMax {
		b = b[:argsMax]
	}
	return p.mem.Write(off, b)
}

func (p *loaded) call(ctx context.Context, name string, args ...uint64) (uint64, error) {
	if p.mod == nil {
		return 0, nil
	}
	fn := p.mod.ExportedFunction(name)
	if fn == nil {
		return 0, nil
	}
	results, err := fn.Call(ctx, args...)
	if err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, nil
	}
	return results[0], nil
}

func (p *loaded) handleSession(ctx context.Context, ev hooks.SessionEvent) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	kind := []byte(ev.Kind)
	if !p.writeAt(argsScratch, kind) || !p.writeAt(eventScratch, raw) {
		return fmt.Errorf("wasm %s: cannot write session event", p.name)
	}
	code, err := p.call(ctx, "alpha_plugin_session",
		uint64(argsScratch), uint64(len(kind)),
		uint64(eventScratch), uint64(len(raw)))
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("wasm %s denied %s", p.name, ev.Kind)
	}
	return nil
}

func (p *loaded) handlePrompt(ctx context.Context, user, sys string) (string, error) {
	ub, sb := []byte(user), []byte(sys)
	if !p.writeAt(argsScratch, ub) || !p.writeAt(eventScratch, sb) {
		return "", fmt.Errorf("wasm %s: cannot write prompt", p.name)
	}
	p.result = ""
	_, err := p.call(ctx, "alpha_plugin_prompt",
		uint64(argsScratch), uint64(len(ub)),
		uint64(eventScratch), uint64(len(sb)))
	if err != nil {
		return "", err
	}
	return p.result, nil
}

func (p *loaded) handleToolPre(ctx context.Context, ev hooks.Event) error {
	raw, _ := json.Marshal(ev)
	if !p.writeAt(argsScratch, raw) {
		return fmt.Errorf("wasm %s: cannot write tool event", p.name)
	}
	code, err := p.call(ctx, "alpha_plugin_tool_pre", uint64(argsScratch), uint64(len(raw)))
	if err != nil {
		return err
	}
	if code != 0 {
		msg := p.result
		if msg == "" {
			msg = "blocked by wasm plugin " + p.name
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func (p *loaded) handleToolPost(ctx context.Context, ev hooks.Event) error {
	raw, _ := json.Marshal(ev)
	if !p.writeAt(argsScratch, raw) {
		return nil
	}
	_, err := p.call(ctx, "alpha_plugin_tool_post", uint64(argsScratch), uint64(len(raw)))
	return err
}

func (p *loaded) handleToolResult(ctx context.Context, ev hooks.Event) (string, error) {
	raw, _ := json.Marshal(ev)
	p.result = ""
	if !p.writeAt(argsScratch, raw) {
		return "", nil
	}
	_, err := p.call(ctx, "alpha_plugin_tool_result", uint64(argsScratch), uint64(len(raw)))
	if err != nil {
		return "", err
	}
	return p.result, nil
}

func (p *loaded) handleUsage(promptTok, completionTok int, elapsed time.Duration) {
	_, _ = p.call(context.Background(), "alpha_plugin_usage",
		uint64(uint32(promptTok)), uint64(uint32(completionTok)), uint64(uint32(elapsed.Milliseconds())))
}

func (p *loaded) handleFooter() string {
	p.result = ""
	_, _ = p.call(context.Background(), "alpha_plugin_footer")
	return p.result
}

func (p *loaded) wireHost(h *ext.Host) {
	if p.wantFooter {
		h.AddFooter(p.handleFooter)
	}
	if p.wantUsage {
		h.OnUsage(p.handleUsage)
	}
	if p.wantPrompt {
		h.OnBeforeAgentStart(p.handlePrompt)
	}
	for _, kind := range p.sessionKinds {
		kind := kind
		h.OnSession(kind, func(ctx context.Context, ev hooks.SessionEvent) error {
			return p.handleSession(ctx, ev)
		})
	}
	for _, w := range p.toolWatch {
		w := w
		var pre ext.PreFunc
		var post ext.PostFunc
		if w.pre {
			pre = p.handleToolPre
		}
		if w.post {
			post = p.handleToolPost
		}
		h.OnTool(w.match, pre, post)
	}
	for _, match := range p.resultMatch {
		match := match
		h.OnToolResult(match, p.handleToolResult)
	}
}
