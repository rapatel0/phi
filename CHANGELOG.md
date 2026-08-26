# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Built-in `skill`, `webfetch`, and `websearch` tools. Search uses the current model's native API when available (Anthropic `web_search_20250305`, Gemini `google_search`, OpenAI/xAI Responses `web_search`) and falls back to DuckDuckGo HTML. `webfetch` is https-only with SSRF checks.
- `mise.toml` pins Go 1.26.3 and golangci-lint 2.13.0, with tasks for build / test / fmt / lint (`mise run check`).
- Go extension host (`internal/ext`): compiled-in plugins register tools and footer bits. Bundled: `tokenspeed`, `todo_write`, `ask_user_question`.
- Gemini (`internal/llm/gemini`) and SuperGrok/xAI (`phi login xai`, `https://api.x.ai/v1`). See [doc/plugins.md](doc/plugins.md).
- `phi login anthropic` / `phi login codex`: Claude Pro/Max (PKCE) and ChatGPT Codex (device code) OAuth. Tokens live in `~/.phi/auth.json`; config `api_key` still wins. OAuth Anthropic requests use Claude Code identity headers and tool names so subscription billing applies.
- Image attach: Ctrl+V (clipboard), paste/drag a `.png`/`.jpg`/`.gif`/`.webp` path, or `/image` / `/image <path>`. Multipart vision content for Anthropic, OpenAI, Codex, and Gemini. Composer shows an `Images:` chip; backspace pops the last attach. Kitty/Ghostty terminals render attached images inline (Kitty graphics protocol; `PHI_KITTY_GRAPHICS=0` to disable).
- `read_image` tool (from pi-go): the agent can look at local images or `https://` URLs (SSRF-gated). `read` on an image/PDF/binary now points at `read_image` / `pdftotext` instead of dumping bytes. Vision parts are injected on the next model turn.
- TUI task sidebar (Ctrl+B): live/recent sub-agent jobs. Enter on an `agent_spawn` card, Ctrl+Enter on a TASKS row, or **Ctrl+O** opens a scrollable **view** popup (composer stays on the parent). **Ctrl+I** in that popup steers (opt-in attach). Esc closes the view or, while steering, returns to the parent.
- Sub-agent cards use the spawn description as the title and collapse when the job finishes.

- `phi run --yolo`: skip all permission checks for one headless run (benchmarks / CI).
- Hooks: session lifecycle events now include `usage` — token counts of the latest completed assistant turn.
- Hooks: `post_turn` event fires after each completed assistant stream with per-round `usage` (for audit metrics such as cache hit ratio).

### Changed

### Deprecated

### Removed

### Fixed

- Ctrl+B hid the TASKS sidebar for one frame then immediately re-showed it whenever sub-agents were running. Hide is now sticky until you toggle again. `Ctrl+T` is the same binding (tmux eats Ctrl+B).
- `/resume` with no id now continues the latest session for this directory (it used to only print usage). Replay restores tool rows and sub-agent cards, not just user/assistant text.
- TUI model palette only listed `config.yaml` entries, so Anthropic / Codex / Grok / Gemini never appeared after `phi login`. Logged-in (or env-keyed) providers now inject their catalog into settings → model.
- Ctrl+K → settings → model fetches live IDs from each provider's `/models` API (OpenAI-compat, Anthropic, Gemini, Codex). Static catalog is the fallback; `PHI_MODEL_LIST=0` skips the network.
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

[Unreleased]: https://github.com/pulseaiclub/phi/compare/v0.16.0...HEAD
[0.16.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.16.0
[0.15.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.15.0
[0.14.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.14.0
[0.13.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.13.0
[0.12.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.12.0
[0.11.0]: https://github.com/pulseaiclub/phi/releases/tag/v0.11.0
