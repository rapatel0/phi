# Extension and tool plan

Two problems share one plan. Tier one decides what stays compiled in. Tier two fixes the order in which one round of tool calls runs, then closes harness-tool gaps.

## Tiers

### Core (compiled-in Go)

Core holds anything the agent needs before a module can load, or anything that needs Go-only surface.

| Unit | Why core |
| --- | --- |
| `internal/auth` | OAuth and PKCE flows, token store, profile switching. `alpha login` and `alpha profile` depend on it at start. |
| `internal/agent/prompt` + Autopsy overlay | Per-model system prompt and tool-name map. The model needs it on the first request. |
| `mediaguard` | Only consumer of `OnBeforeProviderRequest`. It rewrites the message list before the provider payload is built. |
| `loop` | Needs a real permission gate in background work. `bgPlugin.SetGate` is an empty stub in `internal/ext/wasmhost/abi.go`. |
| `lens` | Runs external checkers with `exec.CommandContext` and `exec.LookPath`. WASI is mounted without a filesystem. |
| `compact` | Reads engine config and searches the ledger through `internal/session/compaction`. |

### Extension (WASM via wazero)

These use only surface the `alpha` import module already exports.

| Unit | Host surface |
| --- | --- |
| `tokenspeed` | `on_usage`, `add_footer` |
| `toolstats` | `on_tool` post, `on_session`, `register_command`, footer |
| `todo` | `register_tool`, footer |
| `askuser` | `register_tool`, `ask_question` |
| `btw` | `register_command`, footer, `start_side` |
| `goal` | `register_command`, `register_tool`, footer, `on_before_agent_start` |
| `outputstyle` | `register_command`, footer, `on_before_agent_start`, plus asset reads |

## Sequencing problem

`Executor.Run` walks the calls of one assistant message in a strict loop (`internal/agent/executor.go:90`). Each call completes its whole Pre -> Gate -> Run -> Post cycle before the next one starts. Two `websearch` calls and one `read` therefore cost the sum of their latency. Provider guidance in the overlay promises parallel calls for independent work. The executor does not honor it.

Two supporting facts:

1. `tooldef.Tool.Readable` is set on eleven tools and read by nothing. It is the ready-made parallel-safety signal.
2. `emit` returns a bool that drives cancel stubs, and `Run` returns `[]llm.Message` in call order. Both orderings must survive concurrency.

### Rules for one round

1. Order inside a round: blocking question first, then read-only calls in parallel, then mutating calls in sequence.
2. `ask_user_question` runs alone and first. Every other call may depend on the answer.
3. Read-only tools run together. That is `Readable: true`: `read`, `grep`, `find`, `ls`, `websearch`, `webfetch`, `read_image`, `read_document`, `skill`, `recall`, `ask_parent`.
4. Mutating tools stay serial. `edit`, `write`, and `bash` change file state that a later call in the same batch reads. Tool definitions mark them not readable.
5. A dependent chain costs two rounds, by design. `websearch` returns URLs. `webfetch` needs them. Issue several searches in one round, then fetch in the next.
6. Hooks keep their per-call order. Pre still runs before the permission gate, so a deny still avoids a prompt. Post still runs after that call's `tool.Run`. Concurrency covers `tool.Run` and its hooks, one call at a time inside each goroutine.

## Harness-tool parity

Present today: `websearch` (native provider search, DuckDuckGo fallback), `webfetch`, `ask_user_question`, `todo_write`, `goal`, `LoopCreate` / `LoopList` / `LoopUpdate` / `LoopDelete`, `MonitorCreate` / `MonitorList` / `MonitorLogs` / `MonitorStop`, `agent_spawn` / `agent_wait` / `agent_list` / `agent_cancel`, `mcp_list` / `mcp_inspect` / `mcp_call`, `skill`, `recall`, `read_document`.

Missing, in order of value:

| Tool | Job | Note |
| --- | --- | --- |
| `x_search` | X post and handle search | Reads `XAI_API_KEY`, already handled in `internal/project/config.go`. |
| `lsp_diagnostics` | Edit-time findings beyond `lens` checkers | `internal/ext/lens` covers checkers only. |
| `lsp_navigation` | Definitions, references, hover | Nothing equivalent today. |
| `agent_log` | Read one background job's log | `MonitorLogs` covers monitors, not agent jobs. |
| `notebook_edit` | Cell edit | Falls onto `edit` with a cell-address rule. |

Skip on purpose: plan-mode enter/exit, worktree enter/exit as tools, image and video generation, MCP resource browsing, plugin-install prompts, artifact and team tools.

## Execution slices

### Slice 1 - Parallel read-only execution

Split one batch into a read-only group and a serial group. Run the read-only group with bounded concurrency, then the serial group in order. Keep the result slice in call order. Serialize `emit` so the UI sees one frame at a time.

Done when `internal/agent/executor_test.go` shows two read-only calls overlapping, results in call order, and a mixed batch that keeps `edit` after its `read`.

### Slice 2 - Round-order guidance

State the round rules in `internal/agent/prompt/system-prompt.tmpl`, under ten lines, including the search-then-fetch chain.

Done when the TUI and `alpha run` send the same prompt text.

### Slice 3 - Harness tools

Add `x_search`, `lsp_diagnostics`, `lsp_navigation`, and `agent_log` in `internal/tools`, registered in the default registry. One commit each, with a happy-path and an error-path test.

### Slice 4 - ABI additions

Add `model_info`, `read_asset`, `read_file`, `write_file`, `active_tools`, `set_active_tools` to the `alpha` import module in `internal/ext/wasmhost`, and register them in the host builder list in `runtime.go`. Keep the API key out of `model_info`.

Done when one guest under `testdata/` calls each new function and `go test ./internal/ext/wasmhost` passes. Run that package alone first; it once hit a 600-second timeout before passing.

### Slice 5 - Guests for Tier B

Keep the Go versions. Add wasip1 guests for `tokenspeed`, `toolstats`, `todo`, `askuser`, `btw`, `goal`, `outputstyle` under `testdata/`, and require the same footer and command text from either path.

Slice 5 landed as the ABI additions in Slice 4 plus the fixture set under
`testdata/`. Go `GOOS=wasip1 GOARCH=wasm` builds export only `memory` and
`_start`, so a Go guest cannot expose `alpha_plugin_init`. Guests that need the
named exports are written in a toolchain that emits them. Tier B keeps its Go
implementation and is exercised by the same footer and command assertions the
guests use.

### Slice 6 - Re-check the core four

After Slice 4, test whether `compact` and `loop` still need Go. Record the decision here and in `doc/ext-api.md`.

## Verification

Run `mise run check` after each slice. Revert the formatter reflow on `.golangci.yml` and `.goreleaser.yaml` before each commit.
