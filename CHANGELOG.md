# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **Renamed the project from phi to alpha.** The binary is now `alpha`, the
  module path is `github.com/rapatel0/alpha`, state lives in `~/.alpha`, and
  environment variables use the `ALPHA_` prefix.

  The first run moves an existing `~/.phi` to `~/.alpha`, so OAuth tokens,
  sessions, skills, and hooks are kept. If `~/.alpha` already exists, nothing
  is moved and `~/.phi` is left in place.

  `PHI_*` variables are still read when the matching `ALPHA_*` variable is
  unset. Command hooks receive both `ALPHA_HOOK_*` and `PHI_HOOK_*`.
  Update your scripts: the legacy names are deprecated.

### Added

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
