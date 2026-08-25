# Extension API

This document defines the internal extension API for alpha. It records what
exists today, what is missing, and the order in which to close the gap.

Alpha does not load TypeScript. Extensions are Go packages under
`internal/ext/<name>` that call `ext.Register` in `init` and are wired with a
blank import in `cmd/plugins.go`. See [plugins.md](plugins.md) for the mapping
from Pi packages to alpha.

## Why this document exists

The pi extensions on this machine were measured to find which host calls they
actually make. The result decides what alpha must offer.

| Host call | Extensions that need it | Alpha today |
| --- | --- | --- |
| `registerCommand` | 16 of 17 | Private to hooks |
| `pi.on(<event>)` | 15 | 7 of 26 events |
| `registerTool` | 7 | `Host.RegisterTool` |
| `registerProvider` | 5 | Not modeled |

Two facts follow. First, slash commands matter more than any other capability.
Second, alpha already has most of the machinery; it is reachable from external
process hooks but not from in-process Go code.

## The two halves that exist today

Alpha has two extension systems. They do not talk to each other.

**`internal/ext`** is for compiled-in Go code. A plugin can register a tool,
add a footer fragment, observe token usage, and install a question asker. It
runs in process and holds Go state.

**`internal/hooks`** is for external programs. A hook can gate and observe the
tool loop, react to session lifecycle events, and register a slash command.
Results come back as intents, not as UI calls.

The gap is the diagonal: a Go extension cannot register a slash command, and it
cannot observe session events. Both already work for a shell script.

## The design

Do not build a parallel system. Let `internal/ext` reach the hook machinery
that already exists.

`hooks.Hook` is the in-process extension point, and its doc comment already
anticipates this use. `FuncHook` adapts closures to that interface. A Go
extension registers a `hooks.Entry`, and the existing manager dispatches it.

The command registry needs no new concept either. `CommandRegistry.registerHook`
already adds a command at runtime, keeps it separate from builtins with the
`fromHook` field, and drops it on reload. Extension commands reuse that path.

### Intents, not UI handles

A command returns `hooks.CommandResult`, which already carries the full
vocabulary:

| Field | Effect |
| --- | --- |
| `Submit` | Send text as a user message |
| `Toast` | Show a notification |
| `Status` / `StatusSet` | Set or clear the footer status |
| `List` | Push a palette page |

An extension returns a value that describes what it wants. It never touches the
TUI. This keeps widgets dumb, keeps extensions testable without a terminal, and
means an extension behaves the same in headless mode.

## Proposed host surface

Three methods on `ext.Host`, each backed by machinery that already exists.

```go
// RegisterCommand adds a slash command and a palette row.
func (h *Host) RegisterCommand(cmd Command)

// OnSession subscribes to a session lifecycle event.
func (h *Host) OnSession(kind hooks.Kind, fn SessionFunc)

// OnTool observes or gates the tool loop.
func (h *Host) OnTool(match string, pre PreFunc, post PostFunc)
```

`Command` stays small and free of TUI types:

```go
type Command struct {
    Name        string // without the leading slash
    Description string
    Run         func(ctx context.Context, args []string) (hooks.CommandResult, error)
}
```

The host collects these at startup. The controller converts them into
`hooks.Entry` values and merges them with the entries from directory discovery.
One manager dispatches both, so ordering, fail-closed behavior, and async
semantics stay in one place.

## Event coverage

Alpha fires 7 hook kinds. Pi fires 26. Add events by demand, not for symmetry.

Present: `session_start`, `session_shutdown`, `session_before_switch`,
`pre_tool`, `post_tool`, `command`, `post_turn`.

Worth adding, with the number of pi extensions that use each:

| Event | Users | Note |
| --- | --- | --- |
| `agent_end` | 10 | Fires once per agent completion. `post_turn` fires per assistant stream, which is not the same. |
| `agent_start` | 11 | Pairs with `agent_end`. |
| `session_compact` | 7 | Compaction already exists in `internal/session/compaction`. |
| `session_before_compact` | 6 | Lets an extension veto or adjust. |

The remaining events have one or two users each. Leave them out.

## What stays out of scope

**Providers.** Five pi extensions register a provider for auth, routing, or
failover. Alpha compiles providers in (`internal/auth`: Anthropic, Codex, xAI).
Opening that surface is a separate decision about whether alpha stays lean, and
it should not ride along with this API.

**Request interception.** Two extensions rewrite the request before it is sent.
`Client.Stream` builds and sends inline, with no seam. Two users does not
justify the change yet.

**A `.so` loader.** Extensions are compiled in. This is deliberate.

## Enforcement

The rules above are enforced by the build. A boundary that is only written
down is a boundary that erodes.

### Import rules (`depguard`)

Three rules in `.golangci.yml`. Each names the packages it governs and gives
the fix in its message.

| Rule | Governs | Denies |
| --- | --- | --- |
| `core-no-tui` | agent, session, llm, tools, hooks, job, permission, mcp | `internal/tui` |
| `components-no-tui` | components | `internal/tui` |
| `ext-no-ui` | ext | `internal/tui`, `internal/components`, `xui` |

All three rules pass today with zero violations. They lock in the current
structure rather than demand new work.

Each rule was verified by writing a deliberate violation and confirming the
linter rejected it. A rule that never fires gives false confidence.

### Dead code (`make deadcode`)

`golangci-lint` already runs `unused`, but `unused` works per package and
assumes every exported identifier is API. An exported function that nothing
calls is invisible to it. That is the exact residue an agent leaves after a
refactor: a helper that lost its last caller.

`deadcode` does whole-program reachability from `main`, so it sees across
packages and through the export boundary. Verified: an exported orphan in
`internal/util` is missed by `unused` and caught by `deadcode`.

Run it with `make deadcode` or `mise run deadcode`. The `-test` flag counts
functions reachable from tests as live, which drops the finding count on this
tree from 114 to 48.

The tree has 48 known unreachable functions, mostly unused widgets under
`internal/components`. Failing on all of them would make the check useless on
day one, so they live in `scripts/deadcode-baseline.txt`. New dead code fails
the build. The baseline is a to-do list, not a permanent exemption.

| Command | Effect |
| --- | --- |
| `make deadcode` | Fail on findings outside the baseline |
| `scripts/deadcode.sh --list` | Print every finding |
| `scripts/deadcode.sh --update` | Rewrite the baseline after deleting code |

The baseline stores `<file> <func>` without line numbers, so editing above a
function does not churn it. When dead code is deleted, the script says so and
asks for an update, so the baseline shrinks instead of drifting.

### Call-order rules (`internal/agent/architecture_test.go`)

`depguard` reasons about imports. It cannot see call order, so the tool loop
is enforced by tests that parse `executor.go`.

| Test | Invariant |
| --- | --- |
| `TestToolLoopOrderIsPreHookGateRun` | PreHooks run, then the gate, then the tool |
| `TestPermissionGateHasSingleCallSite` | The gate is called from exactly one place |
| `TestCheckPermissionHandlesEveryDecision` | Allow, Deny, and Ask are all handled |

Each test was verified by injecting the violation it guards against. Moving
`tool.Run` before the permission check fails the first test. Adding a second
`gate.Check` call fails the second.

## Order of work

Each step is useful on its own and does not require the next.

1. **`RegisterCommand`.** Unblocks 16 of 17 measured extensions. The registry
   path already exists; this exposes it to Go.
2. **`OnSession`.** Lets an extension react to session lifecycle events. Enough
   to port `b2-sync`, which needs `session_shutdown` and a periodic trigger.
3. **`agent_start` / `agent_end`.** The most requested missing events.
4. **Compaction events.** Needed for an observational-memory extension.
5. **`OnTool`.** Lowest demand. The tool loop is already gated for external
   hooks, so this is a convenience for Go code.

## Constraints

- The tool loop stays PreHooks then Gate/Ask then Run then PostHooks. An
  extension must not bypass the permission gate.
- Extensions return intents. They do not hold TUI references.
- An extension command must not replace a builtin command. The registry already
  enforces this.
- Keep the host surface small. Add a method when an extension needs it, not
  before.
