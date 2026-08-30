package hooks

import (
	"fmt"

	"github.com/rapatel0/alpha/internal/debuglog"
)

// Load discovers hooks under userDir and projectDir and builds a Manager.
// Discovery warnings are returned; only unexpected I/O fails with err.
// When ALPHA_HOOKS=off, returns an empty Manager and no warnings.
func Load(userDir, projectDir string) (*Manager, []Warning, error) {
	return LoadFrom(compactDirs(userDir), compactDirs(projectDir))
}

// LoadFrom is Load with more than one directory per layer.
func LoadFrom(userDirs, projectDirs []string) (*Manager, []Warning, error) {
	found, warns, err := DiscoverFrom(userDirs, projectDirs)
	if err != nil {
		return nil, warns, err
	}
	return NewManager(EntriesFromDiscovered(found)...), warns, nil
}

// LogWarnings writes each warning to the debug log (ALPHA_DEBUG=1).
func LogWarnings(warns []Warning) {
	for _, w := range warns {
		debuglog.Logf("hooks: %s", w.String())
	}
	if n := len(warns); n > 0 {
		debuglog.Logf("hooks: %d warning(s) while loading", n)
	}
}

// FormatWarningsSummary is a one-line status for stderr / UI.
func FormatWarningsSummary(warns []Warning) string {
	if len(warns) == 0 {
		return ""
	}
	return fmt.Sprintf("hooks: %d warning(s); set ALPHA_DEBUG=1 for details", len(warns))
}
