package project

import (
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

// SkillsDir returns the directory for SKILL.md files.
func (g GlobalLayout) SkillsDir() string { return filepath.Join(g.root, "skills") }

// HooksDir returns the directory for hook plugins (plugin.json).
func (g GlobalLayout) HooksDir() string { return filepath.Join(g.root, "hooks") }

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

// HooksDir returns <root>/.alpha/hooks, the per-project hooks directory
// (user hooks live under Global().HooksDir()).
func (p *Project) HooksDir() string {
	return filepath.Join(brand.ProjectDir(p.root), "hooks")
}

// MCPConfigFile returns <root>/.alpha/mcp.json, the per-project MCP config
// file (the user config is ~/.alpha/mcp.json).
func (p *Project) MCPConfigFile() string {
	return filepath.Join(brand.ProjectDir(p.root), "mcp.json")
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

// ensureGlobalDirs creates the global alpha home directories. It is what makes
// ~/.alpha/{bin,skills,hooks,session,jobs} exist from the very first startup.
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
	if err := profile.Create(root, active); err != nil {
		return nil, err
	}
	return &Project{root: absRoot, global: global}, nil
}
