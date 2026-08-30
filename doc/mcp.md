# MCP

alpha connects to MCP the **mcptoon way**: configure as many servers as you want; **tool schemas never enter the model context**.

That is the main difference from hosts that dump every `tools/list` schema into the prompt — ten or a hundred servers will not burn tens of thousands of tokens before you ask a question.

| Audience | This document |
| --- | --- |
| Users | Configure servers, CLI, how to call from the TUI |
| Contributors | Interaction flow and code map |

---

## Why this is a highlight

| Pain | Typical MCP host | alpha |
| --- | --- | --- |
| Context | All schemas injected at startup | Model sees only three meta-tools |
| Many servers | Uninstall / reload Tetris | Always configured; call on demand |
| Permissions | Separate system or wide open | Same Gate / Ask / Hooks as builtins |
| Footprint | Heavy SDKs, always-on processes | Hand-rolled Go stdio client, lazy start |

Agent-facing tools:

- `mcp_list` — list tool **names** on one server (compact text)
- `mcp_inspect` — slim parameter summary for one tool
- `mcp_call` — actually invoke

Configured **server names** are listed in the system prompt (like Skills), so the model knows what exists without calling `mcp_list` first. Schemas still stay out of context.

Typical rhythm: pick a server from the prompt → `mcp_list(server=…)` → `mcp_inspect` → `mcp_call`.

---

## Interaction flow

```text
Start TUI / alpha run
  → load ~/.agents/mcp.json + <cwd>/.agents/mcp.json
  → build Pool (no subprocess yet)
  → tool list += mcp_list / mcp_inspect / mcp_call
  → system prompt += MCP catalog (server names only)

User prompt
  → model may call mcp_list(server=…) directly from the catalog
  → lazy Client → spawn → initialize → tools/list → names only
  → mcp_inspect → compact param summary
  → mcp_call → Executor → PreHooks → Gate → tools/call → result to model
```

Human CLI and the agent share the same `internal/mcp` stack:

```text
alpha mcp doctor|call  ──┐
                       ├──► Pool ──► Client (stdio JSON-RPC)
model mcp_* ───────────┘
```

Sub-agents do **not** inherit MCP meta-tools by default. Disable with `ALPHA_MCP=off`.

---

## Quick start

Config file: `~/.agents/mcp.json` (project `<cwd>/.agents/mcp.json` overrides same-named servers).
Older `~/.alpha/mcp.json` files still load.

```json
{
  "servers": {
    "browsermcp": {
      "transport": "stdio",
      "command": ["npx"],
      "args": ["@browsermcp/mcp@latest"]
    },
    "remote": {
      "transport": "http",
      "url": "http://127.0.0.1:3001/mcp",
      "headers": { "Authorization": "Bearer …" }
    }
  }
}
```

Or via CLI:

```sh
alpha mcp add browsermcp -- npx @browsermcp/mcp@latest
alpha mcp list
alpha mcp doctor
```

**Restart alpha** after config changes (Pool loads at startup).

### Migrating from Claude Desktop config

Claude / Cursor style:

```json
{
  "mcpServers": {
    "browsermcp": {
      "command": "npx",
      "args": ["@browsermcp/mcp@latest"]
    }
  }
}
```

alpha equivalent:

```json
{
  "servers": {
    "browsermcp": {
      "transport": "stdio",
      "command": ["npx"],
      "args": ["@browsermcp/mcp@latest"]
    }
  }
}
```

`mcpServers` → `servers`; string `command` becomes the first element of the `command` array.

---

## CLI

```text
alpha mcp list                         list configured servers
alpha mcp add <name> -- <cmd> [args…]  write ~/.agents/mcp.json
alpha mcp remove <name>                remove from user config
alpha mcp call <server> <tool> [json]  call a tool directly
alpha mcp doctor                       check config + connectivity
```

Logs: `~/.alpha/logs/mcp/<name>.log` (override with `ALPHA_MCP_LOG_DIR`).

---

## Limits (v1)

- Transports: **stdio** and **http** (POST JSON / SSE `data:` bodies, `Mcp-Session-Id`)
- MCP tools are not registered individually into the model tool list (by design)
- Dead subprocesses reconnect on the next call; no elaborate self-heal state machine
- Some third-party packages may crash on start — use `doctor` + logs.

---

## Related code

| Path | Role |
| --- | --- |
| `internal/mcp/` | config, Client, session, stdio/http transports, Pool |
| `internal/tools/mcptool/` | `mcp_list` / `mcp_inspect` / `mcp_call` |
| `internal/agent/engine.go` | `EngineOpts.MCP` wires meta-tools |
| `cmd/mcp.go` | `alpha mcp` subcommand |
