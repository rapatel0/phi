# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- `agent_spawn` requires `description`, a short label for TASKS and the child
  view. An empty value fails so the model retries with one.
- Shortcuts live in one table (`MacKeymap` / `UnixKeymap`). macOS uses Cmd.
  The terminal is asked to report Cmd+letter keys. Ctrl still works.
- The TASKS sidebar is an agent tree. Click a row (or Cmd+O) to read that
  sub-agent's transcript. Cmd+I steers it. A child `ask_parent` call goes
  to the parent agent, which can answer or prompt you.
- Skills, hooks, and MCP configs live under `~/.agents/` and `<cwd>/.agents/`.
  Config, auth, sessions, jobs, and bin stay in `~/.alpha/`.
  Older `~/.alpha/skills`, hooks, and `mcp.json` still load.
  Skills, hooks, and `AGENTS.md` also load from `~/.claude`, `~/.codex`,
  and `~/.grok` (and the same names under the project). `~/.agents` wins
  when a name clashes.
- The `@` file picker is now core Go rather than a wrapper around an external
  binary. This adds one direct dependency, `github.com/boyter/gocodewalker`,
  for the parallel walk and `.gitignore` matching. Both are pure Go, so the
  release still builds with `CGO_ENABLED=0` and cross-compiles to every
  target from one runner.
- **Renamed the project from phi to alpha.** The binary is now `alpha`, the
  module path is `github.com/rapatel0/alpha`, state lives in `~/.alpha`, and
  environment variables use the `ALPHA_` prefix.

  The first run moves an existing `~/.phi` to `~/.alpha`, so OAuth tokens,
  sessions, skills, and hooks are kept. If `~/.alpha` already exists, nothing
  is moved and `~/.phi` is left in place.

  `PHI_*` variables are still read when the matching `ALPHA_*` variable is
  unset. Command hooks receive both `ALPHA_HOOK_*` and `PHI_HOOK_*`.
  Update your scripts: the legacy names are deprecated.

### Fixed

- The skills palette follows a symlink skills directory. `~/.agents/skills`
  pointed at `~/.claude/skills` and the walker treated the link as a file.
  The palette also scans Claude, Codex, and Grok homes.
- Ctrl+C restores Kitty keyboard mode. Flag 8 otherwise stays on and the
  shell prints CSI-u junk (`9;5u…`).
- Composer underscores no longer sit under the previous letter. Unicode
  mode 2027 could give `_` width 0. Shift+minus inserts `_` when Kitty
  reports the unshifted key. The Shift key itself is not inserted
  (Kitty flag 8 reports it as a private-use rune that looked like `≈`).
- Shift works as a modifier again. Kitty flag 4 sent `97:65;2u` and
  xui dropped the key. Flags are now 11 (no alternate keys). The caret
  no longer flickers: idle frames skip the 60fps redraw.
- A new session no longer shows leftover sub-agents in the sidebar.
  Those jobs belong to their parent session. Resume that session to see them.
- Opening a sub-agent after the home directory moved no longer shows an empty
  welcome screen. Job meta still pointed at `~/.phi`. The store root is the
  path that is used. The agent-tree selection bar now paints text on that bar.
- Startup no longer fails because one provider has no usable credential.
  Alpha aborted the whole config load on the first OAuth error.
  A stale antigravity credential then named that provider, even when
  Anthropic or Codex was ready. Unusable models stay in the palette
  without a key. If you did not set an explicit default, Alpha uses
  the first model that can run.
- `alpha login antigravity` now reads OAuth client credentials from
  `ALPHA_ANTIGRAVITY_CLIENT_ID` and `ALPHA_ANTIGRAVITY_CLIENT_SECRET`.
  Alpha does not store these credentials in the source tree.
- Changing model no longer drops OAuth token refresh. The engine took the
  credential store as an option but did not keep it, so rebuilding the client
  for a new model built one without it. Renewal then stopped silently and the
  next expiry returned 401 with nothing to act on.
- `alpha profile create` no longer reports "created" for a profile that
  already exists, which read as having replaced the credentials in it.
- `alpha login anthropic` no longer fails at once when stdin is not a
  terminal. The paste channel closes when stdin ends, and a receive from a
  closed channel returns an empty string, so the login took the paste branch
  before the browser could redirect and reported a missing authorization code.
- Images are converted to a format the provider documents. The supported
  formats differ, and a provider rejects an undocumented format outright rather
  than degrading it, so the request fails. xAI documents JPEG and PNG only and
  is reached over the OpenAI-compatible path, so a GIF or WebP that any other
  provider accepts arrived there and was refused. Gemini documents WebP but not
  GIF. Conversion targets PNG, because such an image is usually a screenshot
  and JPEG artifacts around text are what matters most.
- Image budgets are sized from each provider's documented limits. Providers
  state their limits on the base64 payload, which is a third larger than the
  raw bytes, and both budgets compared raw bytes against those limits. A
  normalized image could reach 5.6 MB encoded against a 5 MB per-image limit,
  and the request budget could reach 16.8 MB against a 20 MB cap that also had
  to hold the prompt. Budgets are now chosen per request, because the limits
  differ by more than a factor of three and the model can change mid-session.
  xAI was getting OpenAI's budget against a documented limit three times
  smaller. `/media` reports the budget that was applied and its provider.
  Sources are recorded in `doc/media-limits.md`.
- Images are no longer degraded on the way to the model. Two defects, both
  silent. Every GIF was re-encoded as JPEG, so a 4 KB file already inside every
  limit came back twice as large, and an animation was flattened to one frame.
  An image within the limits is now sent byte for byte. Downscaling also
  sampled one source pixel per output pixel, which stepped over thin features
  and dropped them, so a screenshot of text could lose whole rows of strokes at
  one size and survive at the next. It now averages the pixels it covers.

### Added

- Cmd+K → settings → profile lists profiles, marks the active one, and
  has **create new profile…**. `/profile` opens that picker.
  `/profile create <name>` creates one. Log in with `alpha login` after.
- **Profiles.** A profile is a named set of credentials, each with its own
  `auth.json`, so a work login and a personal login no longer overwrite each
  other. `alpha profile list / show / create / use / delete`. The active one
  comes from `ALPHA_PROFILE`, then `alpha profile use`, then `default`. The
  environment wins, so one shell or one project can differ from the rest. The
  default profile keeps `~/.alpha/auth.json`, so nothing has to move. See
  [doc/profiles.md](doc/profiles.md).
- **Loops and monitors.** `LoopCreate` runs a prompt on an interval or a cron
  schedule; `MonitorCreate` runs a long command in the background. Both replace
  a shell `sleep` in a `bash` call, which holds the tool loop open for the whole
  wait. Background commands pass the same permission gate as `bash` and stop
  when the session ends. The footer shows active loops and running commands.
  See [doc/loops.md](doc/loops.md).
- The footer always names the active profile, and `/profile <name>` switches
  the running session without discarding the conversation. A profile that is
  not logged in to the model in use is refused with that reason, and nothing
  changes.
- **Antigravity provider.** `alpha login antigravity` reaches Gemini 3 and
  Claude 4.6 through Google's Antigravity endpoint. The browser redirect
  finishes the login, with a pasted URL as the fallback for SSH. That endpoint
  is undocumented and can be withdrawn without notice, so alpha reports that as
  a distinct condition rather than as a login problem. See
  [doc/antigravity.md](doc/antigravity.md).
- `read_document` extracts the text of a PDF, Word document, spreadsheet,
  presentation, or CSV. `read` returns the bytes, which is not text for these
  formats. The Office formats use only the standard library; PDF adds one
  dependency, `github.com/ledongthuc/pdf`, chosen by measuring 605 real
  documents. A scanned PDF has no text layer and is reported as such. See
  [doc/documents.md](doc/documents.md).
- `lens` reports code problems to the model right after it writes a file, so a
  mistake is corrected in the same turn rather than at the next build. It runs
  the checker the project already uses — `go vet`, `ruff`, `shellcheck`,
  `cargo check`, or `tsc` — and appends the findings to the tool result. A
  checker whose binary is missing is skipped silently, and a clean file
  produces no note. `/lens` shows the findings for the file last written. See
  [doc/lens.md](doc/lens.md).
- Extensions can add a note to a tool result the model reads, through
  `Host.OnToolResult`. An `OnTool` post handler could not: those run detached
  and the manager discards their result.
- An extension hook can watch several tools at once, as `"edit,write"`.
  Previously only one exact name or every tool could be matched.
- `read_image` takes an optional `region` in pixels, to read one part of an
  image. A whole image is shrunk to fit the model's limits, so small text in a
  large screenshot arrives unreadable. Reading a region spends the same budget
  on that area instead: on a 6000px screenshot, strokes that render at 0.34
  contrast in the whole image return at 0.996. A small region is enlarged.
  Coordinates are clamped to the image, and a region entirely outside it
  reports the real dimensions so the next attempt can be aimed.
- `$` completes skill names in the composer, following the Codex convention.
  Accepting inserts the literal `$name` token, so `$code-review the auth
  package` reads as written and the model loads that skill before it answers.
  The picker lists the same skills the `skill` tool loads, so it cannot offer
  one that fails to load. Shell text such as `$HOME`, `${VAR}`, `$(cmd)`, `$$`,
  and `$5` leaves the picker closed. A name must start with a letter, so shell
  text such as `$_` and `$1` stays quiet too. The variable check is
  case-sensitive, so a skill named `home` still completes as `$home`. A
  plugin-qualified name such as `$plugin:skill` is one token.
- `/goal` sets a session objective the agent keeps working toward. The
  objective is fed back at the start of each turn, and the goal ends only for a
  stated reason through `goal_complete`, `goal_blocked`, or `goal_wait`. Each
  closing tool takes the goal id it believes is active, so a turn that started
  under an older goal cannot close the one that replaced it. A turn limit stops
  a goal that loops without finishing.
- `/btw` asks a side question without spending main context. It runs as a
  sub-agent and returns only the summary, so the exchange stays out of the main
  thread. `--tangent` starts a side thread that does not inherit the
  conversation. The child transcript is a normal job, viewable in the usual
  popup.
- `/style` selects a named addition to the system prompt, `/style off` clears
  it, and a bare `/style` opens a picker. Four styles ship built in; a `.md`
  file under `<project>/.alpha/styles` or `~/.alpha/styles` overrides a
  built-in of the same name.
- An aggregate media budget keeps a long session sendable. Images accumulated
  until a provider rejected the request with an opaque error. The newest images
  are now kept and the rest replaced with a factual note naming the file, so
  the turn survives and the model still knows something was there. The stored
  session keeps every image; only the request is trimmed. `/media` reports the
  last decision.
- Two hook events: `before_agent_start`, which can replace the system prompt
  for the turn about to run, and `before_provider_request`, which can rewrite
  the message list before it becomes a provider payload. The second was
  previously out of scope; `doc/ext-api.md` records why that changed.
- Four hook events: `agent_start`, `agent_end`, `session_before_compact`, and
  `session_compact`. The turn events fire from the engine rather than the TUI,
  so a headless `alpha run` gets them too; it previously fired no turn event at
  all, because `post_turn` comes from the interactive controller.
  `agent_end` is deferred, so it also fires when a turn ends by error,
  cancellation, or a caller that stops reading. `session_before_compact` can
  deny, which skips compaction and leaves the turn intact.
- Go extensions can register slash commands, watch session lifecycle events,
  and observe the tool loop. `ext.Host` gains `RegisterCommand`, `OnSession`,
  and `OnTool`; the controller merges the result with hooks discovered from
  disk, so one manager dispatches both. Until now a Go extension could only add
  a tool and a footer string, while an external shell hook could do all three.
  See [doc/ext-api.md](doc/ext-api.md).
- `/toolstats` reports tool call counts for the current session, with failures
  called out and counters reset when a session starts. It is also the reference
  user of the extension API.
- `mise.toml` is now the source of truth for the toolchain and every task. The
  Makefile forwards each target to `mise run <task>`, so `make build` and
  `make check` keep working. CI installs mise and runs `mise run check`, so a
  green local gate means a green CI. Add tasks in `mise.toml`, not the
  Makefile. `mise tasks` lists them.
- `make deadcode` reports unreachable functions and fails on new ones.
  `golangci-lint`'s `unused` works per package and treats every exported
  identifier as API, so an exported function that nothing calls is invisible to
  it. `deadcode` analyzes reachability from `main` across the whole program and
  catches it. The 48 known findings are tracked in
  `scripts/deadcode-baseline.txt`, so only new dead code fails the build.
  Wired into `make check`, `mise run check`, and CI.
- Extension API spec ([doc/ext-api.md](doc/ext-api.md)) and the linters that
  enforce it. Three `depguard` rules keep the TUI out of core, out of widgets,
  and out of extensions. Three tests in `internal/agent/architecture_test.go`
  keep the tool loop in order: PreHooks, then the permission gate, then the
  tool. Every rule was verified by writing the violation it guards against.
- Shortcuts accept `Cmd` as well as `Ctrl` on macOS. `Cmd+R`, `Cmd+B`,
  `Cmd+O`, and `Cmd+I` mirror their `Ctrl` forms, and the command palette adds
  `Cmd+Shift+K`. `Ctrl` keeps working everywhere, which matters over SSH,
  inside tmux, and on Linux.

  Terminals claim some `Cmd` combinations before alpha sees them. Ghostty binds
  `Cmd+K`, `Cmd+T`, `Cmd+Enter`, and `Cmd+V` to its own actions, so those keep
  a `Ctrl` binding. Image paste stays on `Ctrl+V`, because a terminal paste
  delivers text rather than a key press.
- `alpha keys` prints key events as they arrive, so you can see which
  combinations your terminal delivers and which it consumes.
- Session tree dialog (`/sessions` or `Ctrl+R`). Sessions are grouped by
  project, with the current project expanded and the others collapsed. Type to
  filter by preview text, session id, or project name. `↑`/`↓` move, `←`/`→`
  fold a project, `Enter` resumes, `Esc` closes. Picking a session from another
  project resumes it and warns that the session cwd differs; the working
  directory does not change. `/sessions` used to print a flat list into the
  transcript.
- Built-in `skill`, `webfetch`, and `websearch` tools. Search uses the current model's native API when available (Anthropic `web_search_20250305`, Gemini `google_search`, OpenAI/xAI Responses `web_search`) and falls back to DuckDuckGo HTML. `webfetch` is https-only with SSRF checks.
- `mise.toml` pins Go 1.26.3 and golangci-lint 2.13.0, with tasks for build / test / fmt / lint (`mise run check`).
- Go extension host (`internal/ext`): compiled-in plugins register tools and footer bits. Bundled: `tokenspeed`, `todo_write`, `ask_user_question`.
- Gemini (`internal/llm/gemini`) and SuperGrok/xAI (`alpha login xai`, `https://api.x.ai/v1`). See [doc/plugins.md](doc/plugins.md).
- `alpha login anthropic` / `alpha login codex`: Claude Pro/Max (PKCE) and ChatGPT Codex (device code) OAuth. Tokens live in `~/.alpha/auth.json`; config `api_key` still wins. OAuth Anthropic requests use Claude Code identity headers and tool names so subscription billing applies.
- Image attach: Ctrl+V (clipboard), paste/drag a `.png`/`.jpg`/`.gif`/`.webp` path, or `/image` / `/image <path>`. Multipart vision content for Anthropic, OpenAI, Codex, and Gemini. Composer shows an `Images:` chip; backspace pops the last attach. Kitty/Ghostty terminals render attached images inline (Kitty graphics protocol; `ALPHA_KITTY_GRAPHICS=0` to disable).
- `read_image` tool (from pi-go): the agent can look at local images or `https://` URLs (SSRF-gated). `read` on an image/PDF/binary now points at `read_image` / `pdftotext` instead of dumping bytes. Vision parts are injected on the next model turn.
- TUI task sidebar (Ctrl+B): live/recent sub-agent jobs. Enter on an `agent_spawn` card, Ctrl+Enter on a TASKS row, or **Ctrl+O** opens a scrollable **view** popup (composer stays on the parent). **Ctrl+I** in that popup steers (opt-in attach). Esc closes the view or, while steering, returns to the parent.
- Sub-agent cards use the spawn description as the title and collapse when the job finishes.

- `alpha run --yolo`: skip all permission checks for one headless run (benchmarks / CI).
- Hooks: session lifecycle events now include `usage` — token counts of the latest completed assistant turn.
- Hooks: `post_turn` event fires after each completed assistant stream with per-round `usage` (for audit metrics such as cache hit ratio).

### Changed

### Deprecated

### Removed

### Fixed

- A Go extension that vetoed compaction was ignored. The adapter in
  `internal/ext` named `session_before_switch` as the only event that could
  deny, so a `session_before_compact` subscription was registered as
  fire-and-forget and its denial arrived after the decision it was meant to
  change. Both the adapter and the async rule now ask `hooks.CanDeny`.
- The `@` file picker started a search for every keystroke and never stopped
  the previous one. On a large tree each search walked the whole directory, so
  the searches piled up faster than they finished: six keystrokes left four
  running at once. A new keystroke now cancels the search in flight, and
  closing the picker cancels it too.
- The `@` file picker no longer shells out to the `fd` binary. It walks the
  tree in process and stops as soon as it has a page of matches, so it does
  not pay to walk what it will not show: about 1ms on this repo against 10ms
  to 20ms before, and 66ms to 129ms on a tree of 363k files against 1.2s. The
  picker works on a machine without `fd` installed. The walk honors
  `.gitignore`, including negation and nested files, so it still never offers
  a build artifact. The `find` tool is unchanged and still uses `fd`.
- The `@` file picker showed `fd: signal: killed` when a search hit its
  deadline. It now reports `Search timed out — type more of the path`.
- The `@` file picker silently cut its list at 20 matches, so a missing file
  looked absent rather than out of view. It now says `First 20 matches — type
  more to narrow` when more exist.
- Ctrl+B hid the TASKS sidebar for one frame then immediately re-showed it whenever sub-agents were running. Hide is now sticky until you toggle again. `Ctrl+T` is the same binding (tmux eats Ctrl+B).
- `/resume` with no id now continues the latest session for this directory (it used to only print usage). Replay restores tool rows and sub-agent cards, not just user/assistant text.
- TUI model palette only listed `config.yaml` entries, so Anthropic / Codex / Grok / Gemini never appeared after `alpha login`. Logged-in (or env-keyed) providers now inject their catalog into settings → model.
- Ctrl+K → settings → model fetches live IDs from each provider's `/models` API (OpenAI-compat, Anthropic, Gemini, Codex). Static catalog is the fallback; `ALPHA_MODEL_LIST=0` skips the network.
- Anthropic OAuth mapped both `agent_wait` and `agent_list` to `TaskOutput`, so Claude rejected the request with "tools: Tool names must be unique." `agent_list` is now `TaskList`; remaining collisions keep the original name.

### Security

<!-- Released section -->
<!-- Don't change this section unless doing release -->

## [0.16.0] - 2026-08-22

### Added

- Hooks: `command` UI intents — `status` (footer), `list` (palette page).
- Hooks: session lifecycle events `session_start`, `session_shutdown`, `session_before_switch`.

### Changed

### Deprecated

### Removed

### Fixed

### Security

## [0.15.0] - 2026-08-20

### Added

### Changed

### Deprecated

### Removed

- `agent_list` `status` filter parameter (always returns the full list; each row still includes `status`).

### Fixed

### Security

## [0.14.0] - 2026-08-20

### Added

- Hook event `command`: `plugin.json` entries register TUI slash commands (`/name` runs `run`). stdout may `submit` a user message or `toast`.

### Changed

- `write` creates or overwrites files (no longer create-only). Use `edit` for surgical changes.
- File tools resolve relative paths against the session cwd and print cwd-relative results (`find`/`ls`/`grep`/`read`/`write`/`edit`). Absolute paths are used internally (including rg/fd) and returned only when the file is outside cwd.
- `find` (formerly `glob`) uses `fd` from `~/.phi/bin` (same as `rg`): respects `.gitignore`, early-stops at limit, optional `limit` arg.
- Renamed directory listing tool `list` → `ls`.

### Deprecated

### Removed

- Built-in `fetch` tool (and `permissions.fetch` config). Use MCP if you still need URL fetching.
- `agent_log` tool (parent agents only get `agent_wait` summaries; job logs remain on disk under `~/.phi/jobs/`).

### Fixed

- `phi update` on Windows: stage the download next to the installed binary (same volume) and fall back to copy when rename still cannot cross drives.
- Assistant fenced code blocks drop the box/`-----` chrome; a muted language caption sits above the highlighted code so mouse selection stays copy-clean.

### Security

## [0.13.0] - 2026-08-18

### Added
- TUI hot-reloads the git branch in the path label: switching branches outside the app (another terminal, an editor) refreshes the label automatically.

### Changed
- TUI activity: tool rows keep a 1-cell braille spinner; the footer uses an
  Knight-Rider scan bar so the two don't share the same glyph.
- Tool routing: bash is no longer described as an inspection tool; grep/glob no
  longer nudge `agent_spawn`; `edit.hash` is the 4 hex chars after `#` in
  `@file path#TAG` (leading `#` / full header copy-paste is accepted).
- **Breaking:** hooks are declared in `plugin.json` (one file, many hooks) instead of
  per-directory `hook.json`. Load `~/.phi/hooks/plugin.json` and
  `~/.phi/hooks/<plugin>/plugin.json` (same under the project `.phi/hooks/`).

### Deprecated

### Removed
- Per-hook `hook.json` directories. Use `plugin.json` instead.

### Fixed

### Security

## [0.12.0] - 2026-08-17

### Added

- Changelog gate: PRs must update `CHANGELOG.md` (with skip labels / `[chore]`), released sections are protected, and GitHub Release notes are taken from this file.

### Changed

- Hashline `edit` now requires a whole-file `@file path#TAG` (`hash` field) from `read`/`grep`; after a successful edit, re-read before another `edit` on that path. Per-line hashes are 3 letters (a-z) and no longer use digits.

### Removed

- Remove the redundant `agent_task` tool; compose `agent_spawn` + `agent_wait` instead.

## [0.11.0] - 2026-08-16

Baseline release when this changelog became the source of truth for user-visible changes.
Earlier releases are available from GitHub tags only.

<!-- Released section ended -->

[Unreleased]: https://github.com/rapatel0/alpha/compare/v0.16.0...HEAD
[0.16.0]: https://github.com/rapatel0/alpha/releases/tag/v0.16.0
[0.15.0]: https://github.com/rapatel0/alpha/releases/tag/v0.15.0
[0.14.0]: https://github.com/rapatel0/alpha/releases/tag/v0.14.0
[0.13.0]: https://github.com/rapatel0/alpha/releases/tag/v0.13.0
[0.12.0]: https://github.com/rapatel0/alpha/releases/tag/v0.12.0
[0.11.0]: https://github.com/rapatel0/alpha/releases/tag/v0.11.0
