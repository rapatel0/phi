package overlays

import (
	"fmt"
	"strings"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/tui/controller"
)

type questionState struct {
	header  string
	prompt  string
	options []string
	sel     int
	reply   chan controller.QuestionReply
}

func (o *Overlays) beginQuestion(msg controller.QuestionAskMsg) {
	if o.perm != nil {
		o.resolvePermission(controller.AskReply{})
	}
	if o.cont != nil {
		o.resolveContinue(controller.ContinueReply{})
	}
	if o.q != nil {
		o.resolveQuestion(controller.QuestionReply{Index: -1})
	}
	if o.composer != nil {
		o.composer.HideCompleters()
		o.composer.HidePalette()
	}
	o.q = &questionState{
		header:  msg.Header,
		prompt:  msg.Prompt,
		options: msg.Options,
		reply:   msg.Reply,
	}
	if o.focusEditor != nil {
		o.focusEditor()
	}
}

func (o *Overlays) resolveQuestion(r controller.QuestionReply) {
	st := o.q
	if st == nil {
		return
	}
	o.q = nil
	if st.reply != nil {
		select {
		case st.reply <- r:
		default:
		}
	}
	if o.focusChat != nil {
		o.focusChat()
	}
}

func (o *Overlays) HandleQuestionKey(ctx *components.EventContext, e xui.KeyEvent) bool {
	st := o.q
	if st == nil || !e.Press {
		return false
	}
	n := len(st.options)
	if n == 0 {
		o.resolveQuestion(controller.QuestionReply{Index: -1})
		ctx.ConsumeAndRedraw()
		return true
	}
	if e.Mods.Has(xui.ModAlt) && e.Code == xui.KeyRune && e.Rune >= '1' && e.Rune <= '9' {
		idx := int(e.Rune - '1')
		if idx < n {
			o.acceptQuestion(idx)
		}
		ctx.ConsumeAndRedraw()
		return true
	}
	switch e.Code {
	case xui.KeyEscape:
		o.resolveQuestion(controller.QuestionReply{Index: -1})
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyUp:
		if st.sel > 0 {
			st.sel--
		} else {
			st.sel = n - 1
		}
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyDown, xui.KeyTab:
		st.sel = (st.sel + 1) % n
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyEnter:
		o.acceptQuestion(st.sel)
		ctx.ConsumeAndRedraw()
		return true
	case xui.KeyRune:
		if e.Rune >= '1' && e.Rune <= '9' {
			idx := int(e.Rune - '1')
			if idx < n {
				o.acceptQuestion(idx)
			}
			ctx.ConsumeAndRedraw()
			return true
		}
	}
	ctx.ConsumeAndRedraw()
	return true
}

func (o *Overlays) acceptQuestion(idx int) {
	st := o.q
	if st == nil || idx < 0 || idx >= len(st.options) {
		return
	}
	o.resolveQuestion(controller.QuestionReply{Index: idx, Label: st.options[idx]})
}

func (o *Overlays) questionHeight() int {
	st := o.q
	if st == nil {
		return 0
	}
	return 3 + len(st.options)
}

func (o *Overlays) drawQuestion(ctx components.DrawContext, width, height int) components.Surface {
	st := o.q
	s := components.NewSurface(width, height, nil)
	if st == nil {
		return s
	}
	th := o.theme
	title := strings.TrimSpace(st.header)
	if title == "" {
		title = "Question"
	}
	s.Print(1, 0, title, th.Warning, ctx.Method)
	s.Print(1, 1, st.prompt, th.Foreground, ctx.Method)
	for i, opt := range st.options {
		y := 2 + i
		if y >= height {
			break
		}
		mark := " "
		styl := th.Muted
		if i == st.sel {
			mark = ">"
			styl = th.SelectionFg
		}
		line := fmt.Sprintf("%s %d. %s", mark, i+1, opt)
		s.Print(1, y, line, styl, ctx.Method)
	}
	return s
}
