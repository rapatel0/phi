package skills

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rapatel0/alpha/internal/brand"
)

// SearchDirs returns the skill directories to scan, highest priority first.
// LoadDirs keeps the first skill for a name, so an earlier directory wins.
//
// skillPath is the configured directory and may be empty. cwd is the session
// working directory and may be empty when there is none.
//
// The TUI picker and the skill tool both call this, so the picker cannot offer
// a skill that the tool would fail to load.
func SearchDirs(skillPath, cwd string) []string {
	var dirs []string
	add := func(p string) {
		if p = strings.TrimSpace(p); p != "" {
			dirs = append(dirs, p)
		}
	}

	add(skillPath)
	add(brand.Env("SKILL_PATH"))
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(brand.AgentsHome(home), "skills"))
		add(filepath.Join(brand.HomeDir(home), "skills"))
	}
	if cwd != "" {
		add(filepath.Join(brand.AgentsProject(cwd), "skills"))
		add(filepath.Join(brand.ProjectDir(cwd), "skills"))
	}
	return dirs
}
