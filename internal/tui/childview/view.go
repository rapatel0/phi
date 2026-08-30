package childview

import (
	"fmt"
	"strings"
	"time"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/components/status"
	"github.com/rapatel0/alpha/internal/job"
	"github.com/rapatel0/alpha/internal/session"
	"github.com/rapatel0/alpha/internal/tui/transcript"
)

// View is a modal popup of one sub-agent transcript (view-only).
// Steering is opt-in: Handle returns steer=true when the user asks to intervene.
type View struct {
	Theme components.Theme
	info  job.Info
	inner *transcript.TranscriptPane
}

// Open builds a viewer for a job snapshot.
func Open(theme components.Theme, info job.Info, snap session.Snapshot, spin *status.Spinner) *View {
	title := strings.TrimSpace(info.Description)
	if title == "" {
		title = string(info.Role)
	}
	if title == "" {
		title = info.ID
	}
	pane := transcript.NewTranscriptPane(theme, spin, title)
	pane.DisableWelcome()
	pane.LoadReplay(snap)
	pane.Sync()
	pane.StickToBottom()
	return &View{Theme: theme, info: info, inner: pane}
}

// JobID is the viewed job.
func (v *View) JobID() string {
	if v == nil {
		return ""
	}
	return v.info.ID
}

// SetInfo refreshes chrome (status/duration) without replacing the transcript.
func (v *View) SetInfo(info job.Info) {
	if v != nil {
		v.info = info
	}
}

// Apply a live session event from the child engine.
func (v *View) Apply(ev session.Event) {
	if v == nil || v.inner == nil || ev == nil {
		return
	}
	atBottom := v.inner.AtBottom()
	v.inner.ApplySession(ev)
	v.inner.Sync()
	if atBottom {
		v.inner.StickToBottom()
	}
}

// Handle keys/mouse. keep=false closes. steer=true means the user wants to
// send messages to this child (caller should Attach).
func (v *View) Handle(ctx *components.EventContext, ev xui.Event) (keep, steer bool) {
	if v == nil {
		return false, false
	}
	switch e := ev.(type) {
	case xui.KeyEvent:
		if !e.Press {
			return true, false
		}
		if e.Code == xui.KeyEscape {
			ctx.ConsumeAndRedraw()
			return false, false
		}
		km := components.Keys
		if km.Hit(e, km.ChildView) {
			ctx.ConsumeAndRedraw()
			return false, false
		}
		if km.Hit(e, km.ChildSteer) {
			ctx.ConsumeAndRedraw()
			return false, true
		}
		if e.Code == xui.KeyUp {
			if v.inner != nil {
				v.inner.ScrollBy(1)
			}
			ctx.ConsumeAndRedraw()
			return true, false
		}
		if e.Code == xui.KeyDown {
			if v.inner != nil {
				v.inner.ScrollBy(-1)
			}
			ctx.ConsumeAndRedraw()
			return true, false
		}
		if e.Code == xui.KeyPageUp || e.Code == xui.KeyPageDown {
			if v.inner != nil {
				v.inner.HandlePageKey(ctx, e)
			}
			ctx.ConsumeAndRedraw()
			return true, false
		}
		return true, false
	case xui.MouseEvent:
		if e.Button == xui.MouseWheelUp || e.Button == xui.MouseWheelDown {
			if v.inner != nil {
				v.inner.HandleMouse(ctx, e, nil)
			}
			ctx.ConsumeAndRedraw()
			return true, false
		}
		return true, false
	}
	return true, false
}

// Draw paints a framed popup. The inner transcript is inset by 1.
func (v *View) Draw(ctx components.DrawContext, width, height int) components.Surface {
	if width < 12 {
		width = 12
	}
	if height < 6 {
		height = 6
	}
	s := components.NewSurface(width, height, nil)
	th := v.Theme
	title := "view · " + childTitle(v.info)
	hint := "esc close · " + components.Keys.Hint(components.Keys.ChildSteer) + " steer · ↑↓ scroll"
	s.Print(0, 0, "┌"+strings.Repeat("─", max(width-2, 0)), th.Border, ctx.Method)
	s.Print(2, 0, clipTitle(title, width-4, ctx.Method), th.Warning, ctx.Method)
	for y := 1; y < height-2; y++ {
		s.Print(0, y, "│", th.Border, ctx.Method)
		s.Print(width-1, y, "│", th.Border, ctx.Method)
	}
	s.Print(0, height-2, "│", th.Border, ctx.Method)
	s.Print(2, height-2, clipTitle(hint, width-4, ctx.Method), th.Muted, ctx.Method)
	s.Print(width-1, height-2, "│", th.Border, ctx.Method)
	s.Print(0, height-1, "└"+strings.Repeat("─", max(width-2, 0))+"┘", th.Border, ctx.Method)

	innerH := height - 3
	innerW := width - 2
	if v.inner != nil && innerH > 0 && innerW > 0 {
		inner := v.inner.Draw(ctx, innerW, innerH)
		s.Children = append(s.Children, components.SubSurface{
			Origin:  components.Point{X: 1, Y: 1},
			Surface: inner,
			Z:       1,
		})
	}
	return s
}

func childTitle(info job.Info) string {
	icon := "●"
	if info.Status.Terminal() {
		if info.Status == job.StatusCompleted {
			icon = "✓"
		} else {
			icon = "✗"
		}
	}
	label := strings.TrimSpace(info.Description)
	if label == "" {
		label = string(info.Role)
	}
	if label == "" {
		label = info.ID
	}
	meta := string(info.Role)
	if !info.StartedAt.IsZero() && !info.Status.Terminal() {
		meta = fmt.Sprintf("%s · %s", info.Role, time.Since(info.StartedAt).Truncate(time.Second))
	} else if !info.StartedAt.IsZero() && !info.FinishedAt.IsZero() {
		meta = fmt.Sprintf("%s · %s", info.Role, info.FinishedAt.Sub(info.StartedAt).Truncate(time.Second))
	}
	if meta != "" {
		return icon + " " + label + " · " + meta
	}
	return icon + " " + label
}

func clipTitle(s string, cols int, method xui.WidthMethod) string {
	if cols <= 0 {
		return ""
	}
	if xui.StringWidth(s, method) <= cols {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		w := xui.StringWidth(string(r), method)
		if n+w > cols-1 {
			break
		}
		b.WriteRune(r)
		n += w
	}
	b.WriteRune('…')
	return b.String()
}
