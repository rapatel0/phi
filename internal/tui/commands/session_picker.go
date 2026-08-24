package commands

import (
	"github.com/rapatel0/alpha/internal/components/sessionpicker"
	"github.com/rapatel0/alpha/internal/session"
)

// SessionPickerProjects converts stored session groups into picker rows.
// currentDir is the session directory of the running TUI; that project is
// marked so the dialog can expand and label it.
func SessionPickerProjects(groups []session.ProjectSessions, currentDir string) []sessionpicker.Project {
	out := make([]sessionpicker.Project, 0, len(groups))
	for _, g := range groups {
		sessions := make([]sessionpicker.Session, 0, len(g.Sessions))
		for _, m := range g.Sessions {
			sessions = append(sessions, sessionpicker.Session{
				ID:      m.ID,
				File:    m.File,
				Preview: m.Preview,
				Mtime:   m.Mtime,
			})
		}
		out = append(out, sessionpicker.Project{
			Label:    g.Label,
			Cwd:      g.Cwd,
			Current:  currentDir != "" && g.Dir == currentDir,
			Sessions: sessions,
		})
	}
	return out
}
