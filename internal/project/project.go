package project

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/rapatel0/alpha/internal/brand"
	"github.com/rapatel0/alpha/internal/debuglog"
	"github.com/rapatel0/alpha/internal/profile"
)

// GlobalLayout describes the global home directory (~/.alpha).
type GlobalLayout struct {
	root string
	// profile selects which credential set to use. Empty means the
	// default, whose files stay at the root so an install that never uses
	// profiles keeps them where they were.
	profile string
}

// Root returns the global home directory (~/.alpha).
func (g GlobalLayout) Root() string { return g.root }

// Profile returns the active credential profile name.
func (g GlobalLayout) Profile() string {
	if g.profile == "" {
		return profile.Default
	}
	return g.profile
}

// ProfileDir returns the directory holding the active profile's state.
func (g GlobalLayout) ProfileDir() string { return profile.Dir(g.root, g.profile) }

// ConfigFile returns the path to the global config file.
func (g GlobalLayout) ConfigFile() string { return filepath.Join(g.root, "config.yaml") }

// AuthFile returns the OAuth credential store for the active profile.
//
// The default profile uses ~/.alpha/auth.json; a named one uses
// ~/.alpha/profiles/<name>/auth.json. Every caller reaches credentials through
// here, so switching profile switches all of them at once.
func (g GlobalLayout) AuthFile() string { return profile.AuthFile(g.root, g.profile) }

// BinDir returns the directory for downloaded tool binaries.
func (g GlobalLayout) BinDir() string { return filepath.Join(g.root, "bin") }

// LookBin returns name from BinDir if present, otherwise PATH.
func (g GlobalLayout) LookBin(name string) (string, error) {
	custom := filepath.Join(g.BinDir(), name)
	if _, err := os.Stat(custom); err == nil {
		return custom, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s is not available: install to ~/%s/bin or PATH", name, brand.HomeDirName)
	}
	return p, nil
}

// agentsRoot is ~/.agents, sibling of the alpha state dir (~/.alpha).
func (g GlobalLayout) agentsRoot() string {
	return filepath.Join(filepath.Dir(g.root), brand.AgentsDirName)
}

// SkillsDir returns ~/.agents/skills, the shared SKILL.md directory.
func (g GlobalLayout) SkillsDir() string { return filepath.Join(g.agentsRoot(), "skills") }

// LegacySkillsDir returns ~/.alpha/skills, kept so an older install still loads.
func (g GlobalLayout) LegacySkillsDir() string { return filepath.Join(g.root, "skills") }

// HooksDir returns ~/.agents/hooks, the shared hook-plugin directory.
func (g GlobalLayout) HooksDir() string { return filepath.Join(g.agentsRoot(), "hooks") }

// LegacyHooksDir returns ~/.alpha/hooks, kept so an older install still loads.
func (g GlobalLayout) LegacyHooksDir() string { return filepath.Join(g.root, "hooks") }

// SessionBase returns the root directory for persisted sessions.
func (g GlobalLayout) SessionBase() string { return filepath.Join(g.root, "session") }

// JobsDir returns the directory for sub-agent job artifacts.
func (g GlobalLayout) JobsDir() string { return filepath.Join(g.root, "jobs") }

// SessionDir returns the per-cwd session storage directory
// (~/.alpha/session/<encoded-cwd>/), matching panda's layout.
func (p *Project) SessionDir() string {
	return ProjectSessionDir(p.global.SessionBase(), p.root)
}

// JobsDir returns ~/.alpha/jobs for sub-agent job artifacts.
func (p *Project) JobsDir() string {
	return p.global.JobsDir()
}

// HooksDir returns <root>/.agents/hooks, the per-project hooks directory
// (user hooks live under Global().HooksDir()).
func (p *Project) HooksDir() string {
	return filepath.Join(brand.AgentsProject(p.root), "hooks")
}

// LegacyHooksDir returns <root>/.alpha/hooks, kept so an older project still loads.
func (p *Project) LegacyHooksDir() string {
	return filepath.Join(brand.ProjectDir(p.root), "hooks")
}

// MCPConfigFile returns <root>/.agents/mcp.json, the per-project MCP config.
func (p *Project) MCPConfigFile() string {
	return filepath.Join(brand.AgentsProject(p.root), "mcp.json")
}

// LegacyMCPConfigFile returns <root>/.alpha/mcp.json.
func (p *Project) LegacyMCPConfigFile() string {
	return filepath.Join(brand.ProjectDir(p.root), "mcp.json")
}

// UserHookDirs is the user hook search path, lowest priority first.
// ~/.agents/hooks replaces a same-named hook in ~/.alpha/hooks.
func (p *Project) UserHookDirs() []string {
	return uniquePaths(p.global.LegacyHooksDir(), p.global.HooksDir())
}

// ProjectHookDirs is the project hook search path, lowest priority first.
// <cwd>/.agents/hooks replaces a same-named hook in <cwd>/.alpha/hooks.
func (p *Project) ProjectHookDirs() []string {
	return uniquePaths(p.LegacyHooksDir(), p.HooksDir())
}

func uniquePaths(paths ...string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// Project is the resolved alpha workspace: the current working directory plus
// the global layout and its loaded configuration.
type Project struct {
	root   string
	global GlobalLayout
	config *Config
}

// Root returns the working directory the project was resolved from.
func (p *Project) Root() string { return p.root }

// UseProfile points the project at another credential profile.
//
// Only the credential path changes. Sessions, skills, and hooks describe the
// machine rather than the account, so they stay where they are.
//
// The config is dropped because credentials are folded into it when it loads:
// models gain the keys of the profile that was active, and keeping them would
// send the previous account's token to the provider.
//
// A profile with no models loads no config, which is not an error here: an
// empty profile is the normal state between creating one and logging in. The
// caller reports that, because only it knows which model the session needs.
func (p *Project) UseProfile(name string) error {
	if p == nil {
		return errors.New("project: not available")
	}
	if err := profile.ValidateName(name); err != nil {
		return err
	}
	if name != profile.Default && !profile.Exists(p.global.root, name) {
		return fmt.Errorf("project: profile %q does not exist", name)
	}

	// LoadConfig replaces the config outright, which is what rebuilds the
	// credentials: they are folded in from the store of whichever profile
	// is active when it runs.
	previous, previousCfg := p.global.profile, p.config
	p.global.profile = name
	if err := p.LoadConfig(); err != nil {
		debuglog.Logf("project: profile %q has no usable config: %v", name, err)
		if !errors.Is(err, ErrNoModels) {
			p.global.profile, p.config = previous, previousCfg
			return err
		}
	}
	return nil
}

// Global returns the global layout (~/.alpha).
func (p *Project) Global() GlobalLayout { return p.global }

// Config returns the loaded configuration, or nil before LoadConfig.
func (p *Project) Config() *Config { return p.config }

// LoadConfig reads, env-overrides and finalizes the global configuration.
// The result is cached on the Project until the next LoadConfig call.
func (p *Project) LoadConfig() error {
	cfg, err := loadConfig(p.global)
	if err != nil {
		return err
	}
	p.config = cfg
	return nil
}

// ensureGlobalDirs creates the global alpha home directories and the shared
// ~/.agents/{skills,hooks} trees. Skills and hooks live under ~/.agents so
// other tools can use the same files. Session state stays under ~/.alpha.
func ensureGlobalDirs(global GlobalLayout) error {
	dirs := []string{
		global.Root(),
		global.BinDir(),
		global.SkillsDir(),
		global.HooksDir(),
		global.SessionBase(),
		global.JobsDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %q: %w", dir, err)
		}
	}
	return nil
}

// Discover resolves the alpha workspace starting from startDir ("" = cwd) and
// ensures the global directory layout exists.
func Discover(startDir string) (*Project, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	absRoot, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}
	// Move a pre-rename ~/.alpha across before anything reads the directory.
	// A failure here is not fatal: a fresh ~/.alpha is created instead and the
	// old directory stays on disk for manual recovery.
	if _, mErr := brand.Migrate(home); mErr != nil {
		debuglog.Logf("brand: migrate home dir: %v", mErr)
	}
	root := brand.HomeDir(home)
	active, _ := profile.Resolve(root)
	global := GlobalLayout{root: root, profile: active}
	if err := ensureGlobalDirs(global); err != nil {
		return nil, err
	}
	// A profile selected but never created would silently read an empty
	// credential store, which looks like being logged out.
	if _, err := profile.Create(root, active); err != nil {
		return nil, err
	}
	return &Project{root: absRoot, global: global}, nil
}
