package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectSessions is one project directory under the session base, with the
// sessions it holds (newest first).
type ProjectSessions struct {
	// Dir is the absolute project session directory.
	Dir string
	// Name is the encoded directory name, e.g. "--Users-me-proj--".
	Name string
	// Label is a short human-readable name, e.g. "proj".
	Label string
	// Cwd is the working directory the sessions were recorded in. It is read
	// from a session header and is empty when no session carries one.
	Cwd string
	// Sessions are the project's sessions, newest mtime first.
	Sessions []SessionMeta
}

// BrowseSessions lists every project directory under baseDir with its
// sessions. Projects are sorted by their newest session, newest first, so the
// most recently used project leads. Empty and unreadable directories are
// skipped rather than reported, matching ListSessions.
func BrowseSessions(baseDir string) ([]ProjectSessions, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	out := make([]ProjectSessions, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(baseDir, e.Name())
		list, err := ListSessions(dir)
		if err != nil || len(list) == 0 {
			continue
		}
		out = append(out, ProjectSessions{
			Dir:      dir,
			Name:     e.Name(),
			Label:    projectLabel(e.Name()),
			Cwd:      firstCwd(list),
			Sessions: list,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Sessions[0].Mtime.After(out[j].Sessions[0].Mtime)
	})
	return out, nil
}

// firstCwd returns the first non-empty session cwd.
func firstCwd(list []SessionMeta) string {
	for _, m := range list {
		if strings.TrimSpace(m.Cwd) != "" {
			return m.Cwd
		}
	}
	return ""
}

// projectLabel turns an encoded directory name into a short display name.
// "--Users-me-proj--" becomes "proj". The encoding replaces every path
// separator with "-", so a directory name that itself contains "-" cannot be
// recovered exactly. The full path is available from ProjectSessions.Cwd.
func projectLabel(name string) string {
	trimmed := strings.Trim(name, "-")
	if trimmed == "" {
		return name
	}
	if i := strings.LastIndexByte(trimmed, '-'); i >= 0 && i+1 < len(trimmed) {
		return trimmed[i+1:]
	}
	return trimmed
}
