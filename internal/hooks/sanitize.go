package hooks

import (
	"os"
	"strings"

	"github.com/rapatel0/alpha/internal/brand"
)

// Env keys injected into every command hook process.
const (
	EnvHookEvent  = brand.EnvPrefix + "HOOK_EVENT"
	EnvSessionID  = brand.EnvPrefix + "SESSION_ID"
	EnvCwd        = brand.EnvPrefix + "CWD"
	EnvProjectDir = brand.EnvPrefix + "PROJECT_DIR"
)

// Legacy aliases injected alongside the current names so hook scripts written
// against the previous product name keep working.
const (
	legacyHookEvent  = brand.LegacyEnvPrefix + "HOOK_EVENT"
	legacySessionID  = brand.LegacyEnvPrefix + "SESSION_ID"
	legacyCwd        = brand.LegacyEnvPrefix + "CWD"
	legacyProjectDir = brand.LegacyEnvPrefix + "PROJECT_DIR"
)

// sensitiveEnvSubstrings match (case-insensitive) against the env key.
// Keys containing any of these are stripped before spawning a hook.
var sensitiveEnvSubstrings = []string{
	"API_KEY",
	"SECRET",
	"TOKEN",
	"PASSWORD",
	"CREDENTIAL",
	"PRIVATE_KEY",
	"AUTHORIZATION",
	"BEARER",
	"OAUTH",
	"AWS_ACCESS_KEY",
	"AWS_SECRET",
	"AWS_SESSION_TOKEN",
	brand.EnvPrefix + "API_KEY",
	brand.LegacyEnvPrefix + "API_KEY",
}

type hookEnv struct {
	Event      string
	SessionID  string
	Cwd        string
	ProjectDir string
}

// sanitizeEnv copies parent env, drops sensitive keys, and injects the hook
// variables under both the current and the legacy prefix.
func sanitizeEnv(parent []string, extra hookEnv) []string {
	out := make([]string, 0, len(parent)+8)
	for _, kv := range parent {
		key, _, _ := strings.Cut(kv, "=")
		if isSensitiveEnvKey(key) {
			continue
		}
		// Drop keys we are about to overwrite so duplicates don't confuse hooks.
		switch key {
		case EnvHookEvent, EnvSessionID, EnvCwd, EnvProjectDir,
			legacyHookEvent, legacySessionID, legacyCwd, legacyProjectDir:
			continue
		}
		out = append(out, kv)
	}
	out = append(out,
		EnvHookEvent+"="+extra.Event,
		EnvSessionID+"="+extra.SessionID,
		EnvCwd+"="+extra.Cwd,
		EnvProjectDir+"="+extra.ProjectDir,
		legacyHookEvent+"="+extra.Event,
		legacySessionID+"="+extra.SessionID,
		legacyCwd+"="+extra.Cwd,
		legacyProjectDir+"="+extra.ProjectDir,
	)
	return out
}

func isSensitiveEnvKey(key string) bool {
	k := strings.ToUpper(key)
	if brand.IsEnvKey(k, "API_KEY") {
		return true
	}
	for _, sub := range sensitiveEnvSubstrings {
		if strings.Contains(k, sub) {
			return true
		}
	}
	return false
}

// environ is overridable in tests.
var environ = os.Environ
