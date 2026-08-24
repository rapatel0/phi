# Project layout

| Path                     | Purpose                                        |
| ------------------------ | ---------------------------------------------- |
| `cmd/`                   | Entry points (`main.go`, `alpha run`, `alpha update`, `alpha sessions`) |
| `internal/util/update/`  | Self-update check + GitHub Releases install    |
| `internal/agent/`        | Agent engine, executor, jobs                     |
| `internal/agent/prompt/` | System prompt templates + Skills/MCP catalogs    |
| `internal/components/`   | TUI widgets (chat, input, palette, mention, …) |
| `internal/llm/`          | LLM clients (OpenAI-compatible + Anthropic), streaming, skills |
| `internal/media/`        | Image sniff/compress + OS clipboard paste                      |
| `internal/termimg/`      | Kitty graphics protocol (inline transcript images)             |
| `internal/project/`      | Workspace layout and config                    |
| `internal/session/`      | Session persistence, load/apply                |
| `internal/job/`          | Sub-agent job manager (spawn/wait/cancel)      |
| `internal/tools/`        | Agent tools (`*tool` packages + `tooldef`)     |
| `internal/toolmanager/`  | External tool discovery/download               |
| `internal/tui/editor/`   | TUI root widget (`Editor`), layout, dispatch, branch watch |
| `internal/tui/transcript/` | Session→widget projection (Mapper, Pane) |
| `internal/tui/composer/` | Chat input, slash/@ pickers, palette |
| `internal/tui/footer/`   | Activity spinner, token labels, update hint |
| `internal/tui/overlays/` | Permission / continue-ask panels |
| `internal/tui/submit/`   | Submit, cancel, slash dispatch, bash runner |
| `internal/tui/commands/` | Slash/palette registry, session/hook commands |
| `internal/tui/pathutil/` | Cwd + git branch path labels |
| `internal/tui/controller/` | Engine lifecycle, Bus/Msg, activity, sub-agent attach |
| `internal/tui/tasks/`    | TASKS sidebar (Ctrl+B)                         |
| `internal/tui/childview/` | Sub-agent view popup (steer is opt-in)        |
| `internal/version/`      | Build-time `Version` (splash / `alpha update`) |
| `internal/util/`         | Shared helpers (diff, retry, SSE, file search, …) |
| `internal/ext/`          | In-process Go extension host + bundled plugins |
| `internal/llm/gemini/`   | Google Generative Language `streamGenerateContent` |
| `internal/auth/`         | Claude Pro/Max, Codex, SuperGrok OAuth (login, store, refresh) |
| `internal/permission/`   | Permission policy and ask gate                 |
| `internal/hooks/`        | Tool-loop hooks (`plugin.json`, Manager, CommandHook) |
| `internal/mcp/`          | MCP config + stdio client + pool (meta-tool route) |

## Design docs

| Path | Purpose |
| ---- | ------- |
| [`hooks.md`](hooks.md) | Hooks: concepts, authoring, protocol reference |
| [`mcp.md`](mcp.md) | MCP: zero schema pollution, meta-tools, config, CLI |
| [`tui.md`](tui.md) | TUI: package layout, aggregation, interaction flows |
