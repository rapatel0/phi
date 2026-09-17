package anthropic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/llm"
)

const (
	oauthBetaHeader  = "claude-code-20250219,oauth-2025-04-20,fine-grained-tool-streaming-2025-05-14"
	oauthUserAgent   = "claude-cli/2.1.260"
	oauthIdentity    = "You are Claude Code, Anthropic's official CLI for Claude."
	oauthBillingSalt = "59cf53e54c78"
	oauthEntrypoint  = "sdk-cli"
)

const oauthClaudeCodeVersionEnv = "PI_ANTHROPIC_AUTH_CLAUDE_CODE_VERSION"

var claudeCodeVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Claude Code 2.x names. Anthropic's OAuth billing classifier keys off these.
var toClaudeCodeName = map[string]string{
	"read":              "Read",
	"write":             "Write",
	"edit":              "Edit",
	"bash":              "Bash",
	"grep":              "Grep",
	"find":              "Glob",
	"agent_spawn":       "Task",
	"agent_wait":        "TaskOutput",
	"agent_list":        "TaskList",
	"agent_cancel":      "KillShell",
	"skill":             "Skill",
	"webfetch":          "WebFetch",
	"websearch":         "WebSearch",
	"ask_user_question": "AskUserQuestion",
	"todo_write":        "TodoWrite",
}

var fromClaudeCodeName = map[string]string{
	"Read":            "read",
	"Write":           "write",
	"Edit":            "edit",
	"Bash":            "bash",
	"Grep":            "grep",
	"Glob":            "find",
	"Task":            "agent_spawn",
	"TaskOutput":      "agent_wait",
	"TaskList":        "agent_list",
	"KillShell":       "agent_cancel",
	"Skill":           "skill",
	"WebFetch":        "webfetch",
	"WebSearch":       "websearch",
	"AskUserQuestion": "ask_user_question",
	"TodoWrite":       "todo_write",
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

func resolveClaudeCodeVersion() (string, error) {
	version := strings.TrimSpace(os.Getenv(oauthClaudeCodeVersionEnv))
	if version == "" {
		return "2.1.260", nil
	}
	if !claudeCodeVersionPattern.MatchString(version) {
		return "", fmt.Errorf("%s must be a bare X.Y.Z version, got %q", oauthClaudeCodeVersionEnv, version)
	}
	return version, nil
}

func billingHeader(messages []llm.Message) (string, error) {
	var text string
	for _, message := range messages {
		if message.Role == llm.RoleUser {
			text = message.Content
			break
		}
	}
	if text == "" {
		return "", nil
	}
	version, err := resolveClaudeCodeVersion()
	if err != nil {
		return "", err
	}
	messageHash := sha256.Sum256([]byte(text))
	sampled := make([]byte, 0, 3)
	for _, index := range []int{4, 7, 20} {
		if index < len(text) {
			sampled = append(sampled, text[index])
		} else {
			sampled = append(sampled, '0')
		}
	}
	suffixHash := sha256.Sum256([]byte(oauthBillingSalt + string(sampled) + version))
	return fmt.Sprintf("x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=%s; cch=%s;",
		version, hex.EncodeToString(suffixHash[:])[:3], oauthEntrypoint, hex.EncodeToString(messageHash[:])[:5]), nil
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

// uniqueOutboundName maps name for OAuth, then falls back to the original if
// the mapped name is already used (Anthropic requires unique tool names).
func uniqueOutboundName(name string, oauth bool, used map[string]struct{}) string {
	out := outboundToolName(name, oauth)
	if _, ok := used[out]; !ok {
		used[out] = struct{}{}
		return out
	}
	if out != name {
		if _, ok := used[name]; !ok {
			used[name] = struct{}{}
			return name
		}
	}
	return ""
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
