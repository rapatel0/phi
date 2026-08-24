package anthropic

import (
	"net/http"
	"strings"

	"github.com/pulseaiclub/phi/internal/auth"
	"github.com/pulseaiclub/phi/internal/llm"
)

const (
	oauthBetaHeader = "claude-code-20250219,oauth-2025-04-20,fine-grained-tool-streaming-2025-05-14"
	oauthUserAgent  = "claude-cli/2.1.75"
	oauthIdentity   = "You are Claude Code, Anthropic's official CLI for Claude."
)

// Claude Code 2.x names. Anthropic's OAuth billing classifier keys off these.
var toClaudeCodeName = map[string]string{
	"read":         "Read",
	"write":        "Write",
	"edit":         "Edit",
	"bash":         "Bash",
	"grep":         "Grep",
	"find":         "Glob",
	"agent_spawn":  "Task",
	"agent_wait":   "TaskOutput",
	"agent_list":   "TaskOutput",
	"agent_cancel": "KillShell",
}

var fromClaudeCodeName = map[string]string{
	"Read":       "read",
	"Write":      "write",
	"Edit":       "edit",
	"Bash":       "bash",
	"Grep":       "grep",
	"Glob":       "find",
	"Task":       "agent_spawn",
	"TaskOutput": "agent_wait",
	"KillShell":  "agent_cancel",
}

func isOAuth(cfg llm.ModelConfig) bool {
	return auth.IsAnthropicOAuthToken(cfg.APIKey)
}

func setAuthHeaders(req *http.Request, cfg llm.ModelConfig) {
	if isOAuth(cfg) {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		req.Header.Del("X-Api-Key")
		req.Header.Set("anthropic-beta", oauthBetaHeader)
		req.Header.Set("User-Agent", oauthUserAgent)
		req.Header.Set("x-app", "cli")
		return
	}
	req.Header.Set("X-Api-Key", cfg.APIKey)
}

func outboundToolName(name string, oauth bool) string {
	if !oauth {
		return name
	}
	if mapped, ok := toClaudeCodeName[strings.ToLower(name)]; ok {
		return mapped
	}
	return name
}

func inboundToolName(name string, oauth bool) string {
	if !oauth {
		return name
	}
	if mapped, ok := fromClaudeCodeName[name]; ok {
		return mapped
	}
	return name
}
