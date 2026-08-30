package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/components/app"
)

// keysCmd prints each key event so you can see what the terminal delivers.
// Terminals claim some combinations for themselves, and a key that never
// arrives shows nothing here.
func keysCmd(args []string) int {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			printKeysUsage(os.Stdout)
			return 0
		}
	}

	vx, err := xui.New(xui.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "alpha keys: terminal UI:", err)
		return ExitError
	}
	enableMacKeyboard(vx)
	shutdown := closeTerminal(vx)
	defer shutdown()

	application := app.NewApp(vx)
	probe := &keyProbe{theme: components.DefaultTheme()}
	if err := application.Run(probe); err != nil {
		fmt.Fprintln(os.Stderr, "alpha keys:", err)
		return ExitError
	}
	return 0
}

// keyProbe is a widget that echoes key events. It exists to answer one
// question: does this key reach the application at all?
type keyProbe struct {
	theme components.Theme
	lines []string
	done  bool
}

const keyProbeMaxLines = 20

func (p *keyProbe) Handle(ctx *components.EventContext, ev xui.Event) {
	ke, ok := ev.(xui.KeyEvent)
	if !ok || !ke.Press {
		return
	}
	p.lines = append(p.lines, describeKey(ke))
	if len(p.lines) > keyProbeMaxLines {
		p.lines = p.lines[len(p.lines)-keyProbeMaxLines:]
	}
	// Ctrl+C or a bare q exits. Both are echoed first, so the line for the
	// exit key is visible in the scrollback.
	if ke.Code == xui.KeyRune && (ke.Rune == 'c' || ke.Rune == 'C') && ke.Mods.Has(xui.ModCtrl) {
		p.done = true
	}
	if ke.Code == xui.KeyRune && ke.Rune == 'q' && ke.Mods == 0 {
		p.done = true
	}
	if p.done {
		ctx.Quit = true
		return
	}
	ctx.ConsumeAndRedraw()
}

func (p *keyProbe) Draw(ctx components.DrawContext) components.Surface {
	w, h := ctx.Max.Width, ctx.Max.Height
	s := components.NewSurface(w, h, p)
	th := p.theme

	header := []string{
		"alpha keys — press keys to see how this terminal reports them.",
		"A key that prints nothing was consumed by the terminal.",
		"Press Ctrl+C or q to exit.",
	}
	y := 0
	for _, line := range header {
		s.Print(0, y, line, th.Muted, ctx.Method)
		y++
	}
	y++
	for _, line := range p.lines {
		if y >= h {
			break
		}
		s.Print(0, y, line, th.Foreground, ctx.Method)
		y++
	}
	return s
}

// describeKey renders one key event as a single readable line.
func describeKey(ke xui.KeyEvent) string {
	var mods []string
	if ke.Mods.Has(xui.ModCtrl) {
		mods = append(mods, "ctrl")
	}
	if ke.Mods.Has(xui.ModSuper) {
		mods = append(mods, "cmd")
	}
	if ke.Mods.Has(xui.ModAlt) {
		mods = append(mods, "alt")
	}
	if ke.Mods.Has(xui.ModShift) {
		mods = append(mods, "shift")
	}
	combo := "(none)"
	if len(mods) > 0 {
		combo = strings.Join(mods, "+")
	}
	return fmt.Sprintf("  %-20s mods=%s", keyName(ke), combo)
}

// keyName names the key itself, without modifiers.
func keyName(ke xui.KeyEvent) string {
	if ke.Code == xui.KeyRune {
		if ke.Rune == ' ' {
			return "space"
		}
		return fmt.Sprintf("rune %q", ke.Rune)
	}
	switch ke.Code {
	case xui.KeyEnter:
		return "enter"
	case xui.KeyEscape:
		return "escape"
	case xui.KeyTab:
		return "tab"
	case xui.KeyBackspace:
		return "backspace"
	case xui.KeyDelete:
		return "delete"
	case xui.KeyUp:
		return "up"
	case xui.KeyDown:
		return "down"
	case xui.KeyLeft:
		return "left"
	case xui.KeyRight:
		return "right"
	case xui.KeyHome:
		return "home"
	case xui.KeyEnd:
		return "end"
	case xui.KeyPageUp:
		return "page_up"
	case xui.KeyPageDown:
		return "page_down"
	default:
		return fmt.Sprintf("code %v", ke.Code)
	}
}

// printKeysUsage documents the shortcut table, including which Cmd
// combinations a terminal is likely to claim.
func printKeysUsage(w io.Writer) {
	km := components.Keys
	fmt.Fprintln(w, "usage: alpha keys")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Print key events as they arrive, to show what this terminal delivers.")
	fmt.Fprintln(w, "A key that prints nothing was consumed by the terminal.")
	fmt.Fprintln(w)
	if km.Name == "cmd" {
		fmt.Fprintln(w, "On macOS, use Cmd. Ctrl still works over SSH and in tmux.")
	} else {
		fmt.Fprintln(w, "Shortcuts accept Ctrl or Cmd, except where noted.")
	}
	fmt.Fprintln(w)
	row := func(label, desc string) {
		fmt.Fprintf(w, "  %-22s %s\n", label, desc)
	}
	row(km.Label(km.Palette), "command palette")
	row(km.Label(km.Sessions), "session tree")
	row(km.Label(km.AgentTree), "agent tree sidebar ("+km.Label(km.AgentTreeT)+" also works)")
	row(km.Label(km.ChildView), "sub-agent transcript")
	row(km.Label(km.ChildSteer), "steer the selected sub-agent")
	row(km.Label(km.ChildEnter), "view the selected agent-tree row")
	row("click a tree row", "view that sub-agent transcript")
	row(km.Label(km.ImagePaste), "attach a clipboard image")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Terminals usually claim Cmd+K, Cmd+T, Cmd+Enter, and Cmd+V.")
}
