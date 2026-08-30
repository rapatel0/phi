// Package brand owns the product name and the compatibility shims left over
// from the phi -> alpha rename. Everything that reads an environment variable
// or resolves the home directory goes through here, so the legacy names are
// handled in one place.
package brand

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// Name is the product and binary name.
	Name = "alpha"
	// LegacyName is the previous product name.
	LegacyName = "phi"

	// EnvPrefix is the environment variable prefix.
	EnvPrefix = "ALPHA_"
	// LegacyEnvPrefix is the previous prefix, still accepted.
	LegacyEnvPrefix = "PHI_"

	// HomeDirName is the per-user state directory, under $HOME and per project.
	HomeDirName = "." + Name
	// LegacyHomeDirName is the previous state directory.
	LegacyHomeDirName = "." + LegacyName
	// AgentsDirName is the shared agent-content directory (skills, hooks, MCP).
	// It is not product-branded, so the same files work for other tools.
	AgentsDirName = ".agents"
)

// PeerDirNames are other coding-agent homes under $HOME and per-project.
// Alpha reads skills, hooks, and AGENTS.md from these after its own dirs.
var PeerDirNames = []string{".claude", ".codex", ".grok"}

// Env returns the value of an ALPHA_* variable.
//
// suffix is the part after the prefix, for example "API_KEY". If the ALPHA_
// name is unset, the PHI_ name is read instead, so existing shell profiles and
// hook scripts keep working.
func Env(suffix string) string {
	suffix = strings.TrimPrefix(suffix, EnvPrefix)
	suffix = strings.TrimPrefix(suffix, LegacyEnvPrefix)
	if v, ok := os.LookupEnv(EnvPrefix + suffix); ok {
		return v
	}
	return os.Getenv(LegacyEnvPrefix + suffix)
}

// EnvLookup is Env with the "was it set" flag of os.LookupEnv.
func EnvLookup(suffix string) (string, bool) {
	suffix = strings.TrimPrefix(suffix, EnvPrefix)
	suffix = strings.TrimPrefix(suffix, LegacyEnvPrefix)
	if v, ok := os.LookupEnv(EnvPrefix + suffix); ok {
		return v, true
	}
	return os.LookupEnv(LegacyEnvPrefix + suffix)
}

// EnvName returns the canonical variable name for a suffix, for help text.
func EnvName(suffix string) string {
	suffix = strings.TrimPrefix(suffix, EnvPrefix)
	suffix = strings.TrimPrefix(suffix, LegacyEnvPrefix)
	return EnvPrefix + suffix
}

// IsEnvKey reports whether an environment key belongs to this product under
// either the current or the legacy prefix. key may be "NAME" or "NAME=value".
func IsEnvKey(key, suffix string) bool {
	if i := strings.IndexByte(key, '='); i >= 0 {
		key = key[:i]
	}
	suffix = strings.TrimPrefix(suffix, EnvPrefix)
	suffix = strings.TrimPrefix(suffix, LegacyEnvPrefix)
	return key == EnvPrefix+suffix || key == LegacyEnvPrefix+suffix
}

// HasEnvPrefix reports whether an environment key uses either prefix.
func HasEnvPrefix(key string) bool {
	if i := strings.IndexByte(key, '='); i >= 0 {
		key = key[:i]
	}
	return strings.HasPrefix(key, EnvPrefix) || strings.HasPrefix(key, LegacyEnvPrefix)
}

// HomeDir returns the per-user state directory, ~/.alpha.
func HomeDir(home string) string { return filepath.Join(home, HomeDirName) }

// LegacyHomeDir returns the previous state directory, ~/.phi.
func LegacyHomeDir(home string) string { return filepath.Join(home, LegacyHomeDirName) }

// ProjectDir returns the per-project state directory, <root>/.alpha.
func ProjectDir(root string) string { return filepath.Join(root, HomeDirName) }

// LegacyProjectDir returns the previous per-project directory, <root>/.phi.
func LegacyProjectDir(root string) string { return filepath.Join(root, LegacyHomeDirName) }

// AgentsHome returns the shared agent-content directory, ~/.agents.
func AgentsHome(home string) string { return filepath.Join(home, AgentsDirName) }

// AgentsProject returns the per-project agent-content directory, <root>/.agents.
func AgentsProject(root string) string { return filepath.Join(root, AgentsDirName) }

// PeerJoin returns root/<peer>/<elem> for each name in PeerDirNames.
func PeerJoin(root, elem string) []string {
	if root == "" {
		return nil
	}
	out := make([]string, 0, len(PeerDirNames))
	for _, name := range PeerDirNames {
		out = append(out, filepath.Join(root, name, elem))
	}
	return out
}

// PeerHomes returns ~/.claude, ~/.codex, ~/.grok.
func PeerHomes(home string) []string {
	return PeerJoin(home, "")
}
