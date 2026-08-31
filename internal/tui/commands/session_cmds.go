package commands

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rapatel0/alpha/internal/components/toast"
	"github.com/rapatel0/alpha/internal/session"
	"github.com/rapatel0/alpha/internal/tui/controller"
	"github.com/rapatel0/alpha/internal/tui/footer"
	"github.com/rapatel0/alpha/internal/tui/transcript"
)

// SessionCommands owns /sessions, /resume, and /clear UI side effects.
type SessionCommands struct {
	Ctrl            *controller.Controller
	Transcript      *transcript.TranscriptPane
	Footer          *footer.FooterChrome
	Toast           *toast.Toast
	SyncHooks       func()
	OnAbandonAttach func() // drop sub-agent focus before resume/clear
	OnSessionChange func() // reload TASKS after resume/clear
	// ShowPicker opens the session tree dialog. The editor supplies it; when
	// nil, Show falls back to printing the list into the transcript.
	ShowPicker func([]session.ProjectSessions, string)
}

// NewSessionCommands builds session command handlers.
func NewSessionCommands(
	ctrl *controller.Controller,
	transcript *transcript.TranscriptPane,
	footer *footer.FooterChrome,
	toast *toast.Toast,
	syncHooks func(),
) *SessionCommands {
	return &SessionCommands{
		Ctrl:       ctrl,
		Transcript: transcript,
		Footer:     footer,
		Toast:      toast,
		SyncHooks:  syncHooks,
	}
}

// Show opens the session tree dialog, or prints the list when no dialog is
// wired (tests and headless callers).
func (s *SessionCommands) Show() {
	if s == nil {
		return
	}
	dir := ""
	if s.Ctrl != nil {
		dir = s.Ctrl.SessionDir()
	}
	if s.ShowPicker != nil && s.Ctrl != nil {
		projects, err := s.Ctrl.BrowseSessions()
		if err != nil {
			s.Toast.Show(err.Error(), toast.ToastError, 3*time.Second)
			return
		}
		s.ShowPicker(projects, dir)
		return
	}
	list, err := session.ListSessions(dir)
	if err != nil {
		s.Toast.Show(err.Error(), toast.ToastError, 3*time.Second)
		return
	}
	const maxN = 12
	var b strings.Builder
	if len(list) == 0 {
		b.WriteString("No sessions for this directory")
	} else {
		fmt.Fprintf(&b, "Sessions in this directory (%d):\n", len(list))
		n := len(list)
		n = min(n, maxN)
		for i := 0; i < n; i++ {
			m := list[i]
			short := m.ID
			if len(short) > 8 {
				short = short[:8]
			}
			preview := m.Preview
			if preview == "" {
				preview = "(no preview)"
			}
			fmt.Fprintf(&b, "  %s  %s  %s\n", short, m.Mtime.Format("01-02 15:04"), preview)
		}
		b.WriteString("Resume with /resume or /resume <id>")
	}
	s.Transcript.ApplySession(session.AssistantMessageUpdate{Message: session.Message{
		ID:    fmt.Sprintf("sessions-%d", time.Now().UnixNano()),
		State: session.StateComplete,
		Text:  b.String(),
		Content: []session.ContentBlock{
			{Type: session.BlockText, Text: b.String()},
		},
	}})
	s.Transcript.Sync()
	s.Transcript.StickToBottom()
}

// Resume loads a prior session into the UI. Empty id resumes the latest.
func (s *SessionCommands) Resume(id string) {
	if s == nil {
		return
	}
	if s.OnAbandonAttach != nil {
		s.OnAbandonAttach()
	}
	warn, err := s.Ctrl.Resume(id)
	if err != nil {
		s.Toast.Show(err.Error(), toast.ToastError, 4*time.Second)
		return
	}
	if s.SyncHooks != nil {
		s.SyncHooks()
	}
	s.Transcript.ResetSubagents()
	s.Transcript.LoadReplay(s.Ctrl.ReplaySnapshot())
	s.Transcript.Sync()
	s.Transcript.StickToBottom()
	if s.OnSessionChange != nil {
		s.OnSessionChange()
	}
	if s.Footer != nil {
		s.Footer.Activity().Apply(controller.ActivityIdle)
		s.Footer.ClearTokenDisplay()
		snap := s.Transcript.Snapshot()
		s.Footer.SyncFromSnap(snap)
		if u := lastReportedUsage(snap); u.Reported() {
			s.Footer.UpdateTokenDisplay(u)
		}
	}
	msg := "Resumed " + shortSessionID(s.Ctrl.SessionID())
	if warn != "" {
		s.Toast.Show(msg+": "+warn, toast.ToastWarning, 5*time.Second)
		return
	}
	s.Toast.Show(msg, toast.ToastSuccess, 3*time.Second)
}

// Clear starts a new empty session. Caller must ensure the stream is idle
// (see Submitter.StreamActive / CommandBridge ClearSession).
func (s *SessionCommands) Clear() {
	if s == nil {
		return
	}
	if s.OnAbandonAttach != nil {
		s.OnAbandonAttach()
	}
	if err := s.Ctrl.Clear(); err != nil {
		s.Toast.Show(err.Error(), toast.ToastError, 4*time.Second)
		return
	}
	s.Transcript.LoadReplay(s.Ctrl.ReplaySnapshot())
	s.Transcript.ResetSubagents()
	s.Footer.ClearTokenDisplay()
	s.Footer.Activity().Apply(controller.ActivityIdle)
	s.Transcript.Sync()
	s.Transcript.StickToBottom()
	if s.OnSessionChange != nil {
		s.OnSessionChange()
	}
	s.Toast.Show("Cleared "+shortSessionID(s.Ctrl.SessionID()), toast.ToastSuccess, 3*time.Second)
}

func shortSessionID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func lastReportedUsage(snap session.Snapshot) session.TokenUsage {
	for _, v := range slices.Backward(snap.Messages) {
		if v.Role == session.RoleAssistant && v.Usage.Reported() {
			return v.Usage
		}
	}
	return session.TokenUsage{}
}
