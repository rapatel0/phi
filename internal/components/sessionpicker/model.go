// Package sessionpicker renders a tree dialog for choosing a persisted
// session. Projects are branches and sessions are leaves.
package sessionpicker

import (
	"strings"
	"time"
)

// Session is one selectable session row.
type Session struct {
	// ID is the full session id.
	ID string
	// File is the absolute jsonl path. Resume uses this so a session from
	// another project can be opened without changing the session directory.
	File string
	// Preview is the truncated first user message.
	Preview string
	// Mtime is the file modification time.
	Mtime time.Time
}

// Project groups the sessions recorded in one working directory.
type Project struct {
	// Label is the short display name, e.g. "alpha".
	Label string
	// Cwd is the full working directory, shown when it differs from Label.
	Cwd string
	// Current marks the project the TUI is running in.
	Current bool
	// Sessions are ordered newest first.
	Sessions []Session
}

// rowKind distinguishes a project header from a session row.
type rowKind int

const (
	rowProject rowKind = iota
	rowSession
)

// row is one flattened, visible line in the tree.
type row struct {
	kind rowKind
	// project indexes into Picker.Projects.
	project int
	// session indexes into that project's filtered sessions; -1 on a header.
	session int
	// isLast marks the last child under its parent, for the tree connector.
	isLast bool
}

// matchesFilter reports whether a session matches the query. The id, the
// preview text, and the project label are all searched, so "oauth" and a
// partial id both work.
func matchesFilter(s Session, projectLabel, query string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(s.ID), q) ||
		strings.Contains(strings.ToLower(s.Preview), q) ||
		strings.Contains(strings.ToLower(projectLabel), q)
}

// shortID truncates a session id for display.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// formatMtime renders a compact timestamp: a clock for today, else a date.
func formatMtime(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	if sameDay(t, now) {
		return t.Format("15:04")
	}
	if sameDay(t, now.AddDate(0, 0, -1)) {
		return "Yesterday"
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 02")
	}
	return t.Format("2006-01-02")
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
