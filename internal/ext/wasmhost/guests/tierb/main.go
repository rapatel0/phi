//go:build wasip1

// Command tierb is the WASM twin of the compiled-in Tier B extensions. Those
// packages register themselves on the process host, so the guest only forwards
// Alpha's calls through the alpha import module. That keeps one implementation
// of each footer and command string.
package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unsafe"

	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"

	// The Tier B packages register themselves at init. Importing them for the
	// side effect is what puts their commands and tools on the process host.
	_ "github.com/rapatel0/alpha/internal/ext/askuser"
	_ "github.com/rapatel0/alpha/internal/ext/btw"
	_ "github.com/rapatel0/alpha/internal/ext/goal"
	_ "github.com/rapatel0/alpha/internal/ext/outputstyle"
	_ "github.com/rapatel0/alpha/internal/ext/todo"
	_ "github.com/rapatel0/alpha/internal/ext/tokenspeed"
	_ "github.com/rapatel0/alpha/internal/ext/toolstats"
)

// replyScratch is where the host leaves getter output. It ends at the first
// zero byte, which is how a guest learns the length.
const replyScratch = 48000

//go:wasmimport alpha register_command
func registerCommand(namePtr, nameLen, descPtr, descLen uint32) uint32

//go:wasmimport alpha register_tool
func registerTool(namePtr, nameLen, descPtr, descLen, schemaPtr, schemaLen uint32) uint32

//go:wasmimport alpha add_footer
func addFooter()

//go:wasmimport alpha on_usage
func importUsage()

//go:wasmimport alpha on_tool
func importTool(matchPtr, matchLen uint32, pre, post int32)

//go:wasmimport alpha on_session
func importSession(kindPtr, kindLen uint32)

//go:wasmimport alpha on_before_agent_start
func importPrompt()

//go:wasmimport alpha set_toast
func setToast(ptr, length uint32)

//go:wasmimport alpha set_result
func setResult(ptr, length uint32)

//go:wasmimport alpha set_detail
func setDetail(ptr, length uint32)

//go:wasmimport alpha set_submit
func setSubmit(ptr, length uint32)

//go:wasmimport alpha set_status
func setStatus(ptr, length, set uint32)

//go:wasmimport alpha set_list
func setList(ptr, length uint32)

//go:wasmimport alpha ask_question
func askQuestion(headerPtr, headerLen, promptPtr, promptLen, optsPtr, optsLen uint32) uint32

//go:wasmimport alpha start_side
func startSide(promptPtr, promptLen, inherit, rolePtr, roleLen uint32) uint32

var (
	h       *ext.Host
	mgr     *hooks.Manager
	cmds    = map[uint32]ext.Command{}
	toolFns = map[uint32]func(context.Context, json.RawMessage) (string, string, error){}
)

func main() {}

//go:wasmexport alpha_plugin_init
func initPlugin() int32 {
	// Each Tier B package registers itself on the process host at init, so the
	// guest starts from that populated host instead of listing them again.
	h = ext.Default()
	mgr = hooks.NewManager(h.HookEntries()...)
	h.SetQuestionAsker(askViaHost)
	h.SetSideChannel(sideViaHost)

	for _, c := range h.Commands() {
		np, nl := pair(c.Name)
		dp, dl := pair(c.Description)
		cmds[uint32(registerCommand(np, nl, dp, dl))] = c
	}
	for _, t := range h.Tools() {
		schema, _ := json.Marshal(t.Definition.Params)
		run := t.Run
		np, nl := pair(t.Definition.Name)
		dp, dl := pair(t.Definition.Description)
		sp, sl := pair(string(schema))
		id := uint32(registerTool(np, nl, dp, dl, sp, sl))
		toolFns[id] = func(ctx context.Context, raw json.RawMessage) (string, string, error) {
			res, err := run(ctx, raw)
			return res.Content, res.Detail, err
		}
	}
	addFooter()
	importUsage()
	importTool(0, 0, 0, 1)
	kind := string(hooks.KindSessionStart)
	kp, kl := pair(kind)
	importSession(kp, kl)
	importPrompt()
	return 0
}

//go:wasmexport alpha_plugin_command
func runCommand(id, ptr, length uint32) int32 {
	cmd, ok := cmds[id]
	if !ok || cmd.Run == nil {
		return 1
	}
	res, err := cmd.Run(context.Background(), strings.Fields(text(ptr, length)))
	if err != nil {
		emit(setResult, err.Error())
		return 1
	}
	if res.Submit != "" {
		emit(setSubmit, res.Submit)
	}
	if res.Toast != "" {
		emit(setToast, res.Toast)
	}
	if res.StatusSet {
		setStatus(strPtr(res.Status), uint32(len(res.Status)), 1)
	}
	if res.List != nil {
		raw, _ := json.Marshal(res.List)
		emit(setList, string(raw))
	}
	return 0
}

//go:wasmexport alpha_plugin_tool
func runTool(id, ptr, length uint32) int32 {
	fn, ok := toolFns[id]
	if !ok {
		return 1
	}
	content, detail, err := fn(context.Background(), json.RawMessage(buf(ptr, length)))
	if err != nil {
		emit(setResult, err.Error())
		return 1
	}
	emit(setResult, content)
	if detail != "" {
		emit(setDetail, detail)
	}
	return 0
}

//go:wasmexport alpha_plugin_footer
func footer() int32 {
	emit(setResult, strings.Join(h.FooterBits(), " "))
	return 0
}

//go:wasmexport alpha_plugin_usage
func usage(promptTok, completionTok, elapsedMS uint32) int32 {
	h.EmitUsage(int(promptTok), int(completionTok), time.Duration(elapsedMS)*time.Millisecond)
	return 0
}

//go:wasmexport alpha_plugin_prompt
func prompt(userPtr, userLen, sysPtr, sysLen uint32) int32 {
	out := mgr.BeforeAgentStart(context.Background(), hooks.SessionEvent{
		Kind:         hooks.KindBeforeAgentStart,
		Prompt:       text(userPtr, userLen),
		SystemPrompt: text(sysPtr, sysLen),
	})
	if out.SystemPromptSet {
		emit(setResult, out.SystemPrompt)
	}
	return 0
}

//go:wasmexport alpha_plugin_session
func session(ptr, length uint32) int32 {
	kind := hooks.Kind(text(ptr, length))
	if kind == "" {
		kind = hooks.KindSessionStart
	}
	runEntries(
		kind,
		func(e hooks.Entry) { _, _ = e.Hook.Session(context.Background(), hooks.SessionEvent{Kind: kind}) },
	)
	return 0
}

//go:wasmexport alpha_plugin_tool_post
func toolPost(ptr, length uint32) int32 {
	var ev hooks.Event
	if err := json.Unmarshal(buf(ptr, length), &ev); err != nil {
		return 1
	}
	// The ABI is synchronous, so post hooks run inline. A detached goroutine in
	// the instance only advances when the host enters again, which would leave a
	// counter a turn behind.
	runEntries(hooks.KindPostTool, func(e hooks.Entry) {
		if e.Hook.Match(ev.Tool) {
			_, _ = e.Hook.PostTool(context.Background(), ev)
		}
	})
	return 0
}

// runEntries calls every entry of one kind that matches the event.
func runEntries(kind hooks.Kind, call func(hooks.Entry)) {
	needle := string(kind)
	for _, e := range h.HookEntries() {
		if string(e.Kind) != needle {
			continue
		}
		call(e)
	}
}

// askViaHost answers a question through the host, which owns the UI.
func askViaHost(_ context.Context, q ext.Question) (ext.Answer, error) {
	opts, _ := json.Marshal(q.Options)
	hp, hl := pair(q.Header)
	pp, pl := pair(q.Prompt)
	op, ol := pair(string(opts))
	if askQuestion(hp, hl, pp, pl, op, ol) != 0 {
		return ext.Answer{}, context.Canceled
	}
	var ans ext.Answer
	if err := json.Unmarshal(readReply(), &ans); err != nil {
		return ext.Answer{}, err
	}
	return ans, nil
}

// sideViaHost runs a side conversation through the host's job manager.
func sideViaHost(_ context.Context, req ext.SideRequest) (ext.SideResult, error) {
	var inherit uint32
	if req.Inherit {
		inherit = 1
	}
	pp, pl := pair(req.Prompt)
	rp, rl := pair(req.Role)
	if startSide(pp, pl, inherit, rp, rl) != 0 {
		return ext.SideResult{}, context.Canceled
	}
	var res ext.SideResult
	if err := json.Unmarshal(readReply(), &res); err != nil {
		return ext.SideResult{}, err
	}
	return res, nil
}

// readReply copies the bytes at the reply offset up to its terminating zero.
// The cap matches the host reply scratch, so a long getter reply arrives whole.
func readReply() []byte {
	out := make([]byte, 0, 256)
	for i := range 16384 {
		b := buf(replyScratch+uint32(i), 1)
		if len(b) == 0 || b[0] == 0 {
			break
		}
		out = append(out, b[0])
	}
	return out
}

func buf(ptr, length uint32) []byte {
	if ptr == 0 || length == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length) //nolint:gosec // offsets come from the host
}

func text(ptr, length uint32) string { return string(buf(ptr, length)) }

// pair returns the offset and length a host call reads for s.
func pair(s string) (uint32, uint32) { return strPtr(s), uint32(len(s)) }

func strPtr(s string) uint32 {
	if s == "" {
		return 0
	}
	return uint32(uintptr(unsafe.Pointer(unsafe.StringData(s)))) //nolint:gosec // wasip1 memory fits in 32 bits
}

func emit(fn func(uint32, uint32), s string) {
	fn(strPtr(s), uint32(len(s)))
}
