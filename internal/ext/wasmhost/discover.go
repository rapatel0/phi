package wasmhost

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rapatel0/alpha/internal/brand"
)

// Dirs returns plugin search paths. Later directories win on the same
// base name. Project .agents/plugins is last so a repo can override.
func Dirs(cwd string) []string {
	var dirs []string
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		dirs = append(dirs, filepath.Join(brand.AgentsHome(home), "plugins"))
		dirs = append(dirs, filepath.Join(brand.HomeDir(home), "plugins"))
	}
	if cwd != "" {
		dirs = append(dirs, filepath.Join(brand.AgentsProject(cwd), "plugins"))
		dirs = append(dirs, filepath.Join(brand.ProjectDir(cwd), "plugins"))
	}
	return dirs
}

// Files lists *.wasm in dirs. Later paths replace earlier ones with the
// same file name.
func Files(dirs []string) []string {
	byName := map[string]string{}
	var order []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".wasm") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if _, seen := byName[e.Name()]; !seen {
				order = append(order, e.Name())
			}
			byName[e.Name()] = path
		}
	}
	out := make([]string, 0, len(order))
	for _, name := range order {
		out = append(out, byName[name])
	}
	return out
}
