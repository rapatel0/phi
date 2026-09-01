# Pi plugins → Alpha

Alpha does not load TypeScript Pi packages. Analogues are either built in or
compiled-in Go extensions under `internal/ext/*` that register with
[`internal/ext`](../internal/ext). Add a third-party Go extension by blank-importing
it from `cmd/plugins.go` (or a local `cmd/plugins_local.go`).

Your `~/.pi/agent/settings.json` packages:

| Pi package | Analog in Alpha | Notes |
| --- | --- | --- |
| `@gotgenes/pi-anthropic-auth` | `alpha login anthropic` + Anthropic OAuth headers | Subscription billing identity + tool-name mapping |
| `pi-subagents` | `agent_spawn` / `wait` / `list` / `cancel` | Built-in job manager + TUI cards/sidebar; Enter/Ctrl+Enter attaches and talks to the child |
| `pi-powerline-footer` | Footer chrome + extension footer bits | Not pixel-identical; tokens, jobs, tok/s, todos |
| `@siva-sub/pi-docparser` | `read_document` | PDF, Word, Excel, PowerPoint, CSV. No OCR: a scanned PDF is reported as such |
| `pi-token-speed` | `internal/ext/tokenspeed` | Footer `N tok/s` |
| `pi-image-paste` | Ctrl+V / path paste / `/image` + `read_image` | User paste plus agent vision ingest (pi-go) |
| `pi-media-guard` | size/type sniff in `internal/media` | Compresses to 4MB / 2048px; not a full sanitizer |
| `@narumitw/pi-goal` | `internal/ext/goal` | `/goal` with completion and blocked states |
| `pi-btw` | `internal/ext/btw` | Side-session overlay |
| `@narumitw/pi-accounts` | `alpha profile` | Named credential sets; `ALPHA_PROFILE` or `alpha profile use` |
| `pi-auto-router` | — | Slot: `internal/ext/router` later |
| `pi-output-styles` | `internal/ext/outputstyle` | `/style` selects, lists, or clears |
| `pi-blackhole` | session compaction | Observational memory not ported |
| `pi-vcc-llm-compaction` | `internal/ext/compact` + session compaction | Hybrid evidence + LLM handoff, `/compact` |
| `pi-background-tasks` | `agent_spawn` jobs | No separate durable bash task manager |
| `@juicesharp/rpiv-ask-user-question` | `internal/ext/askuser` → `ask_user_question` | Overlay in composer slot |
| `@juicesharp/rpiv-todo` | `internal/ext/todo` → `todo_write` | Footer `n/m todos` |
| `pi-lens` | `internal/ext/lens` | Edit-time diagnostics only. See [lens.md](lens.md); LSP navigation and structural rules not ported |
| `pi-vimmode` | — | Keybindings still hardcoded |

Local Pi extras (`~/.pi/agent/extensions/b2-sync`, `subagent`) stay as shell
hooks / skills in Alpha (`~/.agents/hooks`, `~/.agents/skills`).

## Oh My Pi (OMP) providers stolen this round

| OMP / Pi | Alpha |
| --- | --- |
| Google Gemini (`generateContent` SSE) | `internal/llm/gemini` + `alpha login gemini` / `GEMINI_API_KEY` |
| SuperGrok / xAI OAuth (device code) | `internal/auth/xai.go` + `alpha login xai` / `XAI_API_KEY` |
| Grok chat | OpenAI-compatible `https://api.x.ai/v1` |
| `@cortexkit/pi-antigravity-auth` | `internal/auth/antigravity.go` + `alpha login antigravity` |

Vertex ADC is not ported (API key / Bearer on the Gemini URL covers AI Studio
and a custom `base_url`). Antigravity OAuth is ported: see the table below.

## Adding a Go extension

```go
package myext

import "github.com/rapatel0/alpha/internal/ext"

func init() { ext.Register(Plugin{}) }

type Plugin struct{}

func (Plugin) Name() string { return "myext" }

func (Plugin) Register(h *ext.Host) error {
    h.RegisterTool(...)
    h.AddFooter(func() string { return "ok" })
    return nil
}
```

Then in `cmd/plugins.go`:

```go
import _ "example.com/myext"
```

## WASM plugins

Drop a `*.wasm` file in `~/.agents/plugins/` or `<project>/.agents/plugins/`.
Alpha loads it with wazero (pure Go). Guests can be Go `GOOS=wasip1 GOARCH=wasm`
or any wasm32 toolchain.

The host instantiates WASI without a filesystem so Go `GOOS=wasip1` guests
can load. The `alpha` import module matches `ext.Host`:

- register: `register_command`, `register_tool`, `add_footer`,
  `on_session`, `on_before_agent_start`, `on_tool`, `on_tool_result`,
  `on_usage`, `enable_background`
- results: `set_toast`, `set_result`, `set_submit`, `set_status`,
  `set_list`, `log`
- host calls: `wake`, `compact`, `start_side`, `ask_question`

Exports: `memory`, `alpha_plugin_init`, and optionally
`alpha_plugin_command`, `alpha_plugin_tool`, `alpha_plugin_footer`,
`alpha_plugin_session`, `alpha_plugin_prompt`, `alpha_plugin_tool_pre`,
`alpha_plugin_tool_post`, `alpha_plugin_tool_result`,
`alpha_plugin_usage`, `alpha_plugin_start`, `alpha_plugin_stop`.
A bad module is skipped.
