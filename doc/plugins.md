# Pi plugins → Phi

Phi does not load TypeScript Pi packages. Analogues are either built in or
compiled-in Go extensions under `internal/ext/*` that register with
[`internal/ext`](../internal/ext). Add a third-party Go extension by blank-importing
it from `cmd/plugins.go` (or a local `cmd/plugins_local.go`).

Your `~/.pi/agent/settings.json` packages:

| Pi package | Analog in Phi | Notes |
|---|---|---|
| `@gotgenes/pi-anthropic-auth` | `phi login anthropic` + Anthropic OAuth headers | Subscription billing identity + tool-name mapping |
| `pi-subagents` | `agent_spawn` / `wait` / `list` / `cancel` | Built-in job manager + TUI cards/sidebar; Enter/Ctrl+Enter attaches and talks to the child |
| `pi-powerline-footer` | Footer chrome + extension footer bits | Not pixel-identical; tokens, jobs, tok/s, todos |
| `@siva-sub/pi-docparser` | — | Use MCP (`mcp_list` / `inspect` / `call`) or a Go ext |
| `pi-token-speed` | `internal/ext/tokenspeed` | Footer `N tok/s` |
| `pi-image-paste` | Ctrl+V / path paste / `/image` + `read_image` | User paste plus agent vision ingest (pi-go) |
| `pi-media-guard` | size/type sniff in `internal/media` | Compresses to 4MB / 2048px; not a full sanitizer |
| `@narumitw/pi-goal` | — | Slot: `internal/ext/goal` later |
| `pi-btw` | — | Slot: side-session overlay later |
| `@narumitw/pi-accounts` | `~/.phi/auth.json` per provider | One credential per provider, not named profiles |
| `pi-auto-router` | — | Slot: `internal/ext/router` later |
| `pi-output-styles` | system prompt / skills | No live `/style` switcher yet |
| `pi-blackhole` | session compaction | Observational memory not ported |
| `pi-background-tasks` | `agent_spawn` jobs | No separate durable bash task manager |
| `@juicesharp/rpiv-ask-user-question` | `internal/ext/askuser` → `ask_user_question` | Overlay in composer slot |
| `@juicesharp/rpiv-todo` | `internal/ext/todo` → `todo_write` | Footer `n/m todos` |
| `pi-lens` | — | LSP is a pi-go/OMP feature; MCP or later ext |
| `pi-vimmode` | — | Keybindings still hardcoded |

Local Pi extras (`~/.pi/agent/extensions/b2-sync`, `subagent`) stay as shell
hooks / skills in Phi (`~/.phi/hooks`, `~/.phi/skills`).

## Oh My Pi (OMP) providers stolen this round

| OMP / Pi | Phi |
|---|---|
| Google Gemini (`generateContent` SSE) | `internal/llm/gemini` + `phi login gemini` / `GEMINI_API_KEY` |
| SuperGrok / xAI OAuth (device code) | `internal/auth/xai.go` + `phi login xai` / `XAI_API_KEY` |
| Grok chat | OpenAI-compatible `https://api.x.ai/v1` |

Antigravity OAuth and Vertex ADC are not ported (API key / Bearer on the Gemini
URL covers AI Studio and a custom `base_url`).

## Adding a Go extension

```go
package myext

import "github.com/pulseaiclub/phi/internal/ext"

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
