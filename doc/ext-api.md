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

## Host surface

Three methods on `ext.Host`, each backed by machinery that already existed.
They live in [`internal/ext/api.go`](../internal/ext/api.go).

```go
// RegisterCommand adds a slash command and a palette row.
func (h *Host) RegisterCommand(cmd Command)

// OnSession subscribes to a session lifecycle event.
func (h *Host) OnSession(kind hooks.Kind, fn SessionFunc)

// OnTool observes or gates the tool loop.
func (h *Host) OnTool(match string, pre PreFunc, post PostFunc)

// OnToolResult adds a note to a tool result the model reads.
func (h *Host) OnToolResult(match string, fn ResultFunc)
```

`match` takes one tool name, several separated by commas (`"edit,write"`), or
`""` for every tool.

### Work that outlives a turn

Two more methods cover work that does not end when a turn does. They live in
[`internal/ext/host.go`](../internal/ext/host.go).

```go
// Wake starts a turn with text the user did not type.
func (h *Host) Wake(text string) error

// Backgrounds returns extensions that own background work.
func (h *Host) Backgrounds() []Background
```

`Wake` is how a scheduled fire reaches the agent when nobody is typing. The
shell installs it, the same way it installs the side channel; without it `Wake`
reports that nothing is listening, which is the correct answer in a headless
run. It also refuses while a turn is streaming, because two prompts in flight
would interleave. Treat the error as "try again later", not as a failure.

An extension that owns work outliving a turn implements `Background`:

```go
type Background interface {
    SetGate(gate permission.Gate)
    Start(ctx context.Context)
    Stop()
}
```

The shell drives all three. `SetGate` comes first, so nothing runs a command
before the permission gate exists. `Stop` runs at shutdown, so nothing outlives
the session that started it. An extension cannot arrange either on its own,
because it is registered at init and knows nothing about the shell.

A background command is still a command. Route it through the supplied gate and
refuse until one arrives, or the tool becomes a way around a denied `bash`
call. [`internal/ext/loop`](../internal/ext/loop) is the worked example.

`OnToolResult` is how an extension tells the model something about work it just
did. The returned string is appended to the tool result; returning `""` adds
nothing, which is the right answer when there is nothing to report. Unlike
`OnTool`'s post handler it runs synchronously, because the note has to be ready
before the result reaches the model. Keep the work short and bound anything
that waits on a process.

An `OnTool` post handler cannot do this: those entries are dispatched async and
the manager discards their result. `internal/ext/lens` uses `OnToolResult` to
report code problems after a write.

`Command` stays small and free of TUI types:

```go
type Command struct {
    Name        string // without the leading slash
    Description string
    Run         func(ctx context.Context, args []string) (hooks.CommandResult, error)
}
```

The host collects these at startup. `Host.HookEntries` converts them into
`hooks.Entry` values, and `Controller.ReloadHooks` merges them with the entries
from directory discovery. One manager dispatches both, so ordering,
fail-closed behavior, and async semantics stay in one place.

### Rules the implementation follows

- A command with no name or no `Run` is dropped. Both make it unusable.
- A duplicate name keeps the first registration, so a collision does not depend
  on extension load order.
- Extension entries are never fail-closed. An extension is compiled in, and one
  bad hook must not make the agent refuse to run tools.
- `post_tool`, `session_start`, `session_shutdown`, and `post_turn` are async,
  matching discovered hooks. `session_before_switch` is synchronous, because it
  is the one session event a hook can veto.
- A `PreFunc` error blocks the tool. It cannot grant permission: the gate stays
  the only thing that allows a tool.
- An `OnSession` error denies only `session_before_switch`. For the other kinds
  the event already happened, so the error is reported and the action stays
  Allow.

### Reference user

[`internal/ext/toolstats`](../internal/ext/toolstats/toolstats.go) uses all
three methods: it counts tool calls through `OnTool`, resets per session
through `OnSession`, and reports through a `/toolstats` command. A new
extension needs a blank import in [`cmd/plugins.go`](../cmd/plugins.go), or
nothing reaches its `init`.

## Event coverage

Alpha fires 13 hook kinds. Pi fires 26. Add events by demand, not for symmetry.

Present: `session_start`, `session_shutdown`, `session_before_switch`,
`session_before_compact`, `session_compact`, `before_agent_start`,
`agent_start`, `agent_end`, `before_provider_request`, `pre_tool`,
`post_tool`, `command`, `post_turn`.

`allKinds` in [`internal/hooks/manager.go`](../internal/hooks/manager.go) is the
single list. `notifyKinds` and `asyncKinds` derive the rules from it, and error
messages render it, so adding a kind in one place updates the validator and the
text together.

### before_agent_start

`before_agent_start` fires after the user submits and before the turn runs, so
unlike `agent_start` it can still change the turn. A hook returns a replacement
system prompt, and handlers run in order with each seeing the previous one's
result. That chaining is what lets two extensions compose instead of
overwriting each other.

An error is logged and the turn continues unchanged. A styling or bookkeeping
concern must never cost a turn.

### Turn events

`agent_start` and `agent_end` bracket one agent turn: the prompt goes in, tool
rounds run, and the model stops calling tools. They fire from `Engine.Loop`,
not from the TUI, which matters more than it first appears.

`post_turn` fires from `Controller.recordUsage`, so it exists only in the
interactive shell. A headless `alpha run` fired no turn event at all. Putting
the turn events in the engine covers both modes from one call site.

`agent_end` is deferred, so it fires on every exit: a clean finish, an error, a
cancelled context, and a caller that stops consuming the iterator. A hook that
allocates on `agent_start` can rely on the pairing. The deferred call uses
`context.WithoutCancel`, or the cancelled turn could not report its own end.

### Compaction events

`session_before_compact` is the second event a hook can veto. It fires after
`PrepareCompact` decides there is something to compact and before the summary
is built, so a denial costs nothing and leaves the turn intact. A denial is not
an error: compaction is an optimization.

`session_compact` fires after the summary is written, so a hook that reads the
session sees the compacted state rather than a state that is about to change.

The remaining pi events have one or two users each. Leave them out.

## What stays out of scope

**Providers.** Five pi extensions register a provider for auth, routing, or
failover. Alpha compiles providers in (`internal/auth`: Anthropic, Codex, xAI).
Opening that surface is a separate decision about whether alpha stays lean, and
it should not ride along with this API.

**A `.so` loader.** Extensions are compiled in. This is deliberate.

## Request interception

This was out of scope. The earlier reasoning was that `Client.Stream` builds
and sends inline with no seam, and that two users did not justify the change.
Both halves proved wrong.

The seam exists. `Stream` assembles `[]llm.Message` before it branches to a
provider, so one interception point there covers Anthropic, Gemini, Codex, and
OpenAI at once. Intercepting after the branch would have meant four
implementations, which is what the original note was really objecting to.

The demand was undercounted. Rewriting the request is what an aggregate media
budget needs, and alpha has no image budget at all: images accumulate until a
provider rejects the request with an opaque error.

`before_provider_request` runs on the message list, never on a provider
payload. A hook returns the messages to send. Returning the input observes
without changing anything, and an error is logged and skipped, so a budget
cannot break a request.

It is registered through `hooks.RegisterProviderHook` rather than a `Manager`
entry, because the callback shape differs from the four `Hook` methods and
adding a fifth would break every implementer for one event.

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

### Test colocation

Tests for `foo.go` belong in `foo_test.go`. A test file with no matching source
file splits a package's tests across names that no longer say what they cover.

`TestTestFilesAreColocated` in
[`internal/agent/architecture_test.go`](../internal/agent/architecture_test.go)
checks the whole tree. A file declaring `package x_test` is exempt, because an
external test cannot merge into an internal test file. The remaining 26 files
predate the rule and live in `scripts/colocation-baseline.txt`, which ratchets
like the dead-code baseline: a new orphan fails, and a fixed file must be
removed from the list. The baseline is a to-do list, not an exemption.

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

All five steps are done.

1. ~~**`RegisterCommand`.**~~ Unblocks 16 of 17 measured extensions. The registry
   path already existed; this exposed it to Go.
2. ~~**`OnSession`.**~~ Lets an extension react to session lifecycle events.
3. ~~**`agent_start` / `agent_end`.**~~ The most requested missing events, and
   the only turn events a headless run fires.
4. ~~**Compaction events.**~~ Needed for an observational-memory extension.
5. ~~**`OnTool`.**~~ Lowest demand. The tool loop was already gated for external
   hooks, so this is a convenience for Go code.

Add the next event when an extension needs it.

## Constraints

- The tool loop stays PreHooks then Gate/Ask then Run then PostHooks. An
  extension must not bypass the permission gate.
- Extensions return intents. They do not hold TUI references.
- An extension command must not replace a builtin command. The registry already
  enforces this.
- Keep the host surface small. Add a method when an extension needs it, not
  before.
