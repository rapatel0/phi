package composer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/components/chat"
	"github.com/rapatel0/alpha/internal/components/layout"
	"github.com/rapatel0/alpha/internal/components/mention"
	"github.com/rapatel0/alpha/internal/components/palette"
	"github.com/rapatel0/alpha/internal/components/sessionpicker"
	"github.com/rapatel0/alpha/internal/components/toast"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/llm/skills"
	"github.com/rapatel0/alpha/internal/media"
	"github.com/rapatel0/alpha/internal/tui/commands"
	"github.com/rapatel0/alpha/internal/tui/controller"
	"github.com/rapatel0/alpha/internal/tui/footer"
	"github.com/rapatel0/alpha/internal/tui/pathutil"
	"github.com/rapatel0/alpha/internal/tui/transcript"
	"github.com/rapatel0/alpha/internal/util/filesearch"
)

// ComposerPane owns the chat input, slash/@ pickers, and palette.
type ComposerPane struct {
	theme     components.Theme
	cwd       string
	skillPath string

	Chat     chat.ChatInput
	mention  mention.Picker
	slash    mention.Picker
	skill    mention.Picker
	palette  palette.CommandPalette
	sessions sessionpicker.Picker

	mentionGen int
	// mentionCancel stops the search in flight; nil before the first search.
	mentionCancel context.CancelFunc
	commands      *commands.CommandRegistry

	transcript *transcript.TranscriptPane
	submitter  BusyChecker

	publish  func(controller.Msg)
	drainBus func()
	onRedraw func()

	overlayBlocksComposer func() bool
	handlePermissionKey   WireKeyHandler
	handleContinueKey     WireKeyHandler
	handleQuestionKey     WireKeyHandler
	handleCopyKey         WireKeyHandler
	requestFocusEditor    func()
	requestFocus          func(components.Widget)
	ctrlClose             func()
	openSessions          func()
	onIdleEscape          func() bool
	onNotice              func(msg string, kind toast.ToastKind)
	images                []llm.Image
}

// NewComposerPane builds composer widgets; call Wire before use.
func NewComposerPane(theme components.Theme, model, cwd string) *ComposerPane {
	return &ComposerPane{
		theme: theme,
		cwd:   cwd,
		Chat:  newChatInput(theme, model, cwd),
		mention: mention.Picker{
			Theme: theme,
		},
		slash: mention.Picker{
			Theme:  theme,
			Prefix: "/",
		},
		skill: mention.Picker{
			Theme:  theme,
			Prefix: "$",
		},
		palette: palette.CommandPalette{
			Theme: theme,
		},
		sessions: sessionpicker.Picker{
			Theme: theme,
		},
	}
}

// Wire binds bus, transcript, and editor overlay hooks after Editor assembly.
func (c *ComposerPane) Wire(
	transcript *transcript.TranscriptPane,
	submitter BusyChecker,
	commands *commands.CommandRegistry,
	cwd string,
	publish func(controller.Msg),
	drainBus func(),
	onRedraw func(),
	overlayBlocksComposer func() bool,
	handlePermissionKey WireKeyHandler,
	handleContinueKey WireKeyHandler,
	handleQuestionKey WireKeyHandler,
	handleCopyKey WireKeyHandler,
	requestFocusEditor func(),
	requestFocus func(components.Widget),
	ctrlClose func(),
) {
	if c == nil {
		return
	}
	c.cwd = cwd
	c.commands = commands
	c.transcript = transcript
	c.submitter = submitter
	c.publish = publish
	c.drainBus = drainBus
	c.onRedraw = onRedraw
	c.overlayBlocksComposer = overlayBlocksComposer
	c.handlePermissionKey = handlePermissionKey
	c.handleContinueKey = handleContinueKey
	c.handleQuestionKey = handleQuestionKey
	c.handleCopyKey = handleCopyKey
	c.requestFocusEditor = requestFocusEditor
	c.requestFocus = requestFocus
	c.ctrlClose = ctrlClose

	c.palette.FocusReturn = &c.Chat
	c.Chat.OnSubmit = func(text string) {
		if c.publish != nil {
			c.publish(controller.SubmitMsg{Text: text})
		}
		if c.drainBus != nil {
			c.drainBus()
		}
	}
	c.Chat.OnChange = func(text string) {
		c.SyncBashBorder(text)
		if c.onRedraw != nil {
			c.onRedraw()
		}
	}
	c.Chat.OnMentionChange = c.onMentionChange
	c.Chat.OnSlashChange = c.onSlashChange
	c.Chat.OnSkillChange = c.onSkillChange
	c.Chat.OnPendingImagesChange = c.syncImagesFromLabels
	c.mention.OnAccept = c.acceptMention
	c.slash.OnAccept = c.acceptSlash
	c.skill.OnAccept = c.acceptSkill
}

// SetSkillPath sets the configured skill directory used by the $ picker.
// Empty is valid: the other search directories still apply.
func (c *ComposerPane) SetSkillPath(path string) {
	if c != nil {
		c.skillPath = path
	}
}

// HideCompleters closes mention and slash pickers.
func (c *ComposerPane) HideCompleters() {
	if c == nil {
		return
	}
	c.mention.Hide()
	c.Chat.MentionOpen = false
	c.abandonMentionSearch()
	c.slash.Hide()
	c.Chat.SlashOpen = false
	c.skill.Hide()
	c.Chat.SkillOpen = false
}

// HidePalette closes the command palette if open.
func (c *ComposerPane) HidePalette() {
	if c != nil {
		c.palette.Hide()
	}
}

// ClearInput clears the chat composer text.
func (c *ComposerPane) ClearInput() {
	if c == nil {
		return
	}
	c.Chat.Value = ""
	c.Chat.Cursor = 0
}

// PendingSkills returns attached skill names awaiting submit.
func (c *ComposerPane) PendingSkills() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, len(c.Chat.PendingSkills))
	out = append(out, c.Chat.PendingSkills...)
	return out
}

// ClearPendingSkills removes attached skills from the composer.
func (c *ComposerPane) ClearPendingSkills() {
	if c != nil {
		c.Chat.ClearPendingSkills()
	}
}

// PendingImages returns attached images for the next submit.
func (c *ComposerPane) PendingImages() []llm.Image {
	if c == nil {
		return nil
	}
	out := make([]llm.Image, len(c.images))
	copy(out, c.images)
	return out
}

// ClearPendingImages drops queued attachments.
func (c *ComposerPane) ClearPendingImages() {
	if c == nil {
		return
	}
	c.images = nil
	c.Chat.ClearPendingImages()
}

// SetOnNotice reports attach errors (clipboard empty, unreadable file).
func (c *ComposerPane) SetOnNotice(fn func(msg string, kind toast.ToastKind)) {
	if c != nil {
		c.onNotice = fn
	}
}

func (c *ComposerPane) notice(msg string, kind toast.ToastKind) {
	if c != nil && c.onNotice != nil {
		c.onNotice(msg, kind)
	}
}

func (c *ComposerPane) syncImagesFromLabels(names []string) {
	if c == nil {
		return
	}
	if len(names) >= len(c.images) {
		return
	}
	c.images = c.images[:len(names)]
}

// AttachClipboard reads an image from the OS clipboard and toasts on failure.
func (c *ComposerPane) AttachClipboard() bool {
	return c.attachClipboard(true)
}

func (c *ComposerPane) attachClipboard(noticeEmpty bool) bool {
	if c == nil {
		return false
	}
	img, err := media.ReadClipboard()
	if err != nil {
		if noticeEmpty || err != media.ErrEmptyClipboard {
			if err == media.ErrEmptyClipboard {
				c.notice("Clipboard has no image — copy a screenshot first", toast.ToastWarning)
			} else {
				c.notice(err.Error(), toast.ToastWarning)
			}
		}
		return false
	}
	ok := c.addImage(img)
	if ok && noticeEmpty {
		c.notice("Attached "+img.Label(), toast.ToastSuccess)
	}
	return ok
}

func (c *ComposerPane) handlePaste(text string) bool {
	if paths := media.ImagePaths(text); len(paths) > 0 {
		n := 0
		for _, p := range paths {
			if c.AttachPath(p) {
				n++
			}
		}
		return n > 0
	}
	if strings.TrimSpace(text) == "" {
		return c.attachClipboard(false)
	}
	return false
}

// AttachPath loads an image file.
func (c *ComposerPane) AttachPath(path string) bool {
	if c == nil {
		return false
	}
	img, err := media.LoadFile(path)
	if err != nil {
		c.notice(err.Error(), toast.ToastWarning)
		return false
	}
	return c.addImage(img)
}

func (c *ComposerPane) addImage(img llm.Image) bool {
	if len(c.images) >= 8 {
		c.notice("At most 8 images per message", toast.ToastWarning)
		return false
	}
	img.Filename = uniqueImageName(img.Label(), c.images)
	c.images = append(c.images, img)
	c.Chat.AddPendingImage(img.Label())
	if c.onRedraw != nil {
		c.onRedraw()
	}
	return true
}

// SyncBashBorder updates composer chrome for "!cmd" prefix.
func (c *ComposerPane) SyncBashBorder(text string) {
	if c != nil && c.submitter != nil {
		c.submitter.SyncBashBorder(text)
	}
}

// CloseMentionSlash hides @ and / pickers.
func (c *ComposerPane) CloseMentionSlash() {
	if c == nil {
		return
	}
	c.mention.Hide()
	c.Chat.MentionOpen = false
	c.abandonMentionSearch()
	c.slash.Hide()
	c.Chat.SlashOpen = false
	c.skill.Hide()
	c.Chat.SkillOpen = false
}

// SetBashBorderActive toggles bash-mode border styling.
func (c *ComposerPane) SetBashBorderActive(active bool) {
	if c == nil {
		return
	}
	if active {
		c.Chat.BorderStyle = c.theme.ToolName
	} else {
		c.Chat.BorderStyle = c.theme.Border
	}
}

// FocusChat requests keyboard focus on the chat input.
func (c *ComposerPane) FocusChat() {
	if c != nil && c.requestFocus != nil {
		c.requestFocus(&c.Chat)
	}
}

// AddPendingSkill attaches a skill badge to the composer.
func (c *ComposerPane) AddPendingSkill(name string) {
	if c != nil {
		c.Chat.AddPendingSkill(name)
	}
}

// SetModelLabel updates the model name in the composer header.
func (c *ComposerPane) SetModelLabel(name string) {
	if c != nil {
		c.Chat.TopRightLabel.Text = name
	}
}

// SetAttachLabel sets the composer top-left chrome while a sub-agent is focused.
func (c *ComposerPane) SetAttachLabel(text string) {
	if c == nil {
		return
	}
	if strings.TrimSpace(text) == "" {
		c.Chat.TopLeftLabel = layout.BorderLabel{}
		return
	}
	c.Chat.TopLeftLabel = layout.BorderLabel{Text: text, Style: c.theme.Warning}
}

// SetOnIdleEscape is called on Esc when pickers are closed and nothing is streaming.
// Return true if the key was consumed (e.g. detach from a sub-agent).
func (c *ComposerPane) SetOnIdleEscape(fn func() bool) {
	if c != nil {
		c.onIdleEscape = fn
	}
}

// SetBranchLabel updates the path label in the composer footer.
func (c *ComposerPane) SetBranchLabel(text string) {
	if c != nil {
		c.Chat.BottomRightLabel.Text = text
	}
}

// ClearBottomLeftLabel clears token/context stats in the composer footer.
func (c *ComposerPane) ClearBottomLeftLabel() {
	if c != nil {
		c.Chat.BottomLeftLabel = layout.BorderLabel{}
	}
}

// SetBottomLeftLabel sets token/context stats in the composer footer.
func (c *ComposerPane) SetBottomLeftLabel(label layout.BorderLabel) {
	if c != nil {
		c.Chat.BottomLeftLabel = label
	}
}

// SetPaletteCommands replaces Ctrl+K root commands.
func (c *ComposerPane) SetPaletteCommands(cmds []palette.PaletteCommand) {
	if c != nil {
		c.palette.Commands = cmds
	}
}

// PushPalette opens or nests a palette submenu.
func (c *ComposerPane) PushPalette(title string, cmds []palette.PaletteCommand) {
	if c == nil {
		return
	}
	if !c.palette.Open {
		c.palette.Show()
	}
	c.palette.Push(title, cmds)
	if c.requestFocus != nil {
		c.requestFocus(&c.palette)
	}
}

// SetTheme updates composer widget themes.
func (c *ComposerPane) SetTheme(th components.Theme) {
	if c == nil {
		return
	}
	c.theme = th
	c.Chat.Theme = th
	c.Chat.BorderStyle = th.Border
	c.Chat.TextStyle = th.Foreground
	c.Chat.BottomRightLabel.Style = footer.PathLabelStyle(th)
	c.Chat.TopRightLabel.Style = th.Success
	if c.Chat.TopLeftLabel.Text != "" {
		c.Chat.TopLeftLabel.Style = th.Warning
	}
	c.palette.Theme = th
	c.sessions.Theme = th
	c.mention.Theme = th
	c.slash.Theme = th
	c.skill.Theme = th
	c.SyncBashBorder(c.Chat.Value)
}

// ApplyMentionResults updates the @ picker from async file search.
func (c *ComposerPane) ApplyMentionResults(msg controller.MentionResultsMsg) {
	if c == nil || msg.Gen != c.mentionGen || !c.mention.Open {
		return
	}
	if msg.ErrText != "" {
		c.mention.SetResults(nil, msg.ErrText)
		return
	}
	items := make([]mention.Item, 0, len(msg.Paths))
	for _, p := range msg.Paths {
		items = append(items, mention.Item{Path: p})
	}
	status := ""
	switch {
	case len(items) == 0:
		status = "No matching files"
	case msg.Truncated:
		// Say the list is partial, so a missing file reads as "narrow the
		// query" rather than "the file is not there".
		status = fmt.Sprintf("First %d matches — type more to narrow", len(items))
	}
	c.mention.SetResults(items, status)
}

// PreferredHeight reports the chat input area height.
func (c *ComposerPane) PreferredHeight(width int, method xui.WidthMethod) int {
	if c == nil {
		return 5
	}
	chatH := c.Chat.PreferredHeight(width, method)
	minChatH := 5
	if len(c.Chat.PendingSkills) > 0 {
		minChatH++
	}
	if len(c.Chat.PendingImages) > 0 {
		minChatH++
	}
	if chatH < minChatH {
		chatH = minChatH
	}
	return chatH
}

// DrawChat renders the chat input surface.
func (c *ComposerPane) DrawChat(ctx components.DrawContext, width, height int) components.Surface {
	if c == nil {
		return components.Surface{}
	}
	return c.Chat.Draw(
		ctx.WithConstraints(components.Size{}, components.Size{Width: width, Height: height}),
	)
}

// PickerOverlays returns slash and @ picker surfaces anchored above the composer.
func (c *ComposerPane) PickerOverlays(ctx components.DrawContext, listH, width int) []components.SubSurface {
	if c == nil {
		return nil
	}
	var out []components.SubSurface
	if c.slash.Open {
		c.slash.AnchorBottomY = listH
		c.slash.AnchorX = 0
		c.slash.AnchorWidth = width
		out = append(out, components.SubSurface{
			Origin:  components.Point{X: 0, Y: 0},
			Surface: c.slash.Draw(ctx),
			Z:       15,
		})
	}
	if c.skill.Open {
		c.skill.AnchorBottomY = listH
		c.skill.AnchorX = 0
		c.skill.AnchorWidth = width
		out = append(out, components.SubSurface{
			Origin:  components.Point{X: 0, Y: 0},
			Surface: c.skill.Draw(ctx),
			Z:       15,
		})
	}
	if c.mention.Open {
		c.mention.AnchorBottomY = listH
		c.mention.AnchorX = 0
		c.mention.AnchorWidth = width
		out = append(out, components.SubSurface{
			Origin:  components.Point{X: 0, Y: 0},
			Surface: c.mention.Draw(ctx),
			Z:       15,
		})
	}
	return out
}

// PaletteOverlay returns the Ctrl+K palette surface when open.
func (c *ComposerPane) PaletteOverlay(ctx components.DrawContext) (components.SubSurface, bool) {
	if c == nil || !c.palette.Open {
		return components.SubSurface{}, false
	}
	return components.SubSurface{
		Origin:  components.Point{X: 0, Y: 0},
		Surface: c.palette.Draw(ctx),
		Z:       20,
	}, true
}

// SessionOverlay returns the session tree dialog surface when open.
func (c *ComposerPane) SessionOverlay(ctx components.DrawContext) (components.SubSurface, bool) {
	if c == nil || !c.sessions.Open {
		return components.SubSurface{}, false
	}
	return components.SubSurface{
		Origin:  components.Point{X: 0, Y: 0},
		Surface: c.sessions.Draw(ctx),
		Z:       20,
	}, true
}

// ShowSessionPicker opens the session tree dialog. onPick receives the chosen
// session file path.
func (c *ComposerPane) ShowSessionPicker(projects []sessionpicker.Project, onPick func(file string)) {
	if c == nil {
		return
	}
	c.HideCompleters()
	c.palette.Hide()
	c.sessions.OnAccept = func(s sessionpicker.Session) {
		if onPick != nil {
			onPick(s.File)
		}
	}
	c.sessions.FocusReturn = &c.Chat
	c.sessions.Show(projects)
	if c.requestFocus != nil {
		c.requestFocus(&c.sessions)
	}
}

// SessionPickerOpen reports whether the session dialog is showing.
func (c *ComposerPane) SessionPickerOpen() bool {
	return c != nil && c.sessions.Open
}

// SetSessionOpener installs the Ctrl+R handler that opens the session dialog.
// Wire already takes many parameters, so this stays a separate setter.
func (c *ComposerPane) SetSessionOpener(open func()) {
	if c != nil {
		c.openSessions = open
	}
}

// Handle dispatches keyboard/mouse input to the composer area.
func (c *ComposerPane) Handle(ctx *components.EventContext, ev xui.Event) {
	if c == nil {
		return
	}
	switch ev := ev.(type) {
	case xui.FocusEvent:
		if c.overlayBlocksComposer != nil && c.overlayBlocksComposer() {
			if c.requestFocusEditor != nil {
				c.requestFocusEditor()
			}
		} else if c.sessions.Open {
			if c.requestFocus != nil {
				c.requestFocus(&c.sessions)
			}
		} else if c.palette.Open {
			if c.requestFocus != nil {
				c.requestFocus(&c.palette)
			}
		} else {
			c.FocusChat()
		}
	case xui.KeyEvent:
		if ev.CtrlC() {
			if c.ctrlClose != nil {
				c.ctrlClose()
			}
			ctx.Quit = true
			return
		}
		if c.handlePermissionKey != nil && c.handlePermissionKey(ctx, ev) {
			return
		}
		if c.handleContinueKey != nil && c.handleContinueKey(ctx, ev) {
			return
		}
		if c.handleQuestionKey != nil && c.handleQuestionKey(ctx, ev) {
			return
		}
		if c.handleCopyKey != nil && c.handleCopyKey(ctx, ev) {
			return
		}
		if ev.Press && ev.Code == xui.KeyEscape {
			if c.slash.Open {
				c.slash.Cancel()
				c.Chat.SlashOpen = false
				ctx.ConsumeAndRedraw()
				return
			}
			if c.mention.Open {
				c.mention.Cancel()
				c.Chat.MentionOpen = false
				c.abandonMentionSearch()
				ctx.ConsumeAndRedraw()
				return
			}
			if c.skill.Open {
				c.skill.Cancel()
				c.Chat.SkillOpen = false
				ctx.ConsumeAndRedraw()
				return
			}
			if c.submitter != nil && (c.submitter.RunningBash() || c.submitter.IsBusy()) {
				if c.publish != nil {
					c.publish(controller.CancelStreamMsg{})
				}
				if c.drainBus != nil {
					c.drainBus()
				}
				ctx.ConsumeAndRedraw()
				return
			}
			if c.transcript != nil && c.transcript.SelectionActive() {
				c.transcript.ClearSelection()
				ctx.ConsumeAndRedraw()
				return
			}
			if c.onIdleEscape != nil && c.onIdleEscape() {
				ctx.ConsumeAndRedraw()
				return
			}
		}
		// Ctrl-only: terminals map Cmd+V to their own paste, which arrives as
		// text rather than a key event.
		if ev.Press && components.CtrlOnly(ev) && ev.Code == xui.KeyRune &&
			(ev.Rune == 'v' || ev.Rune == 'V') {
			if c.attachClipboard(false) {
				ctx.ConsumeAndRedraw()
				return
			}
		}
		if ev.Press && components.IsChord(ev, 'r', 'R') && c.openSessions != nil {
			// Same binding closes the dialog it opened.
			if c.sessions.Open {
				c.sessions.Hide()
				c.FocusChat()
			} else {
				c.openSessions()
			}
			ctx.ConsumeAndRedraw()
			return
		}
		// Ctrl+K, or Cmd+Shift+K: Ghostty binds plain Cmd+K to clear_screen.
		if ev.Press && ev.Code == xui.KeyRune && (ev.Rune == 'k' || ev.Rune == 'K') &&
			(components.CtrlOnly(ev) || (ev.Mods.Has(xui.ModSuper) && ev.Mods.Has(xui.ModShift))) {
			if c.palette.Open {
				c.palette.Hide()
				c.FocusChat()
			} else {
				c.HideCompleters()
				c.palette.Show()
				if c.requestFocus != nil {
					c.requestFocus(&c.palette)
				}
			}
			ctx.ConsumeAndRedraw()
			return
		}
		// The session dialog owns every key while it is open, so plain typing
		// filters instead of reaching the composer.
		if c.sessions.Open {
			c.sessions.Handle(ctx, ev)
			if !c.sessions.Open {
				c.FocusChat()
			}
			return
		}
		if c.palette.Open {
			c.palette.Handle(ctx, ev)
			if !c.palette.Open {
				c.FocusChat()
			}
			return
		}
		if c.slash.Open && mentionNavKey(ev) {
			c.slash.Handle(ctx, ev)
			if !c.slash.Open {
				c.Chat.SlashOpen = false
			}
			return
		}
		if c.skill.Open && mentionNavKey(ev) {
			c.skill.Handle(ctx, ev)
			if !c.skill.Open {
				c.Chat.SkillOpen = false
			}
			return
		}
		if c.mention.Open && mentionNavKey(ev) {
			c.mention.Handle(ctx, ev)
			if !c.mention.Open {
				c.Chat.MentionOpen = false
			}
			return
		}
		if ev.Code == xui.KeyPageUp || ev.Code == xui.KeyPageDown {
			if c.transcript != nil {
				c.transcript.HandlePageKey(ctx, ev)
			}
			return
		}
		c.Chat.Handle(ctx, ev)
	case xui.MouseEvent:
		if c.sessions.Open {
			c.sessions.Handle(ctx, ev)
			return
		}
		if c.palette.Open {
			c.palette.Handle(ctx, ev)
			return
		}
		if c.transcript != nil {
			c.transcript.HandleMouse(ctx, ev, c.FocusChat)
		}
	case xui.PasteEvent:
		if c.sessions.Open {
			c.sessions.Handle(ctx, ev)
			return
		}
		if c.palette.Open {
			c.palette.Handle(ctx, ev)
			return
		}
		if c.handlePaste(ev.Text) {
			ctx.ConsumeAndRedraw()
			return
		}
		c.Chat.Handle(ctx, ev)
	}
}

func (c *ComposerPane) onMentionChange(active bool, query string) {
	if c == nil {
		return
	}
	if !active {
		c.mention.Hide()
		c.Chat.MentionOpen = false
		c.abandonMentionSearch()
		return
	}
	if c.slash.Open || c.Chat.SlashOpen {
		return
	}
	c.slash.Hide()
	c.Chat.SlashOpen = false
	c.skill.Hide()
	c.Chat.SkillOpen = false
	c.mention.Show()
	c.Chat.MentionOpen = true
	if len(c.mention.Items) == 0 {
		c.mention.Status = "Searching…"
	}
	c.scheduleMentionSearch(query)
}

func (c *ComposerPane) onSlashChange(active bool, query string) {
	if c == nil {
		return
	}
	if !active {
		c.slash.Hide()
		c.Chat.SlashOpen = false
		return
	}
	c.mention.Hide()
	c.Chat.MentionOpen = false
	c.skill.Hide()
	c.Chat.SkillOpen = false
	c.abandonMentionSearch()
	items := []mention.Item{}
	if c.commands != nil {
		items = c.commands.FilterSlash(query)
	}
	status := ""
	if len(items) == 0 {
		status = "No matching commands"
	}
	c.slash.SetResults(items, status)
	c.slash.Show()
	c.Chat.SlashOpen = true
}

func (c *ComposerPane) onSkillChange(active bool, query string) {
	if c == nil {
		return
	}
	if !active {
		c.skill.Hide()
		c.Chat.SkillOpen = false
		return
	}
	// One completer at a time; the file picker owns an in-flight search.
	if c.mention.Open || c.Chat.MentionOpen || c.slash.Open || c.Chat.SlashOpen {
		return
	}

	list := skills.Filter(skills.LoadDirs(skills.SearchDirs(c.skillPath, c.cwd)), query)
	items := make([]mention.Item, 0, len(list))
	for _, s := range list {
		items = append(items, mention.Item{Path: s.Name, Description: s.Description})
	}
	status := ""
	if len(items) == 0 {
		status = "No matching skills"
	}
	c.skill.SetResults(items, status)
	c.skill.Show()
	c.Chat.SkillOpen = true
}

// acceptSkill inserts the literal $name token. The model reads it and decides
// whether to call the skill tool, so accepting never submits by itself.
func (c *ComposerPane) acceptSkill(item mention.Item) {
	if c == nil {
		return
	}
	_, start, end, ok := chat.ActiveSkill(c.Chat.Value, c.Chat.Cursor)
	if !ok {
		start, end = c.Chat.Cursor, c.Chat.Cursor
	}
	c.skill.Hide()
	c.Chat.SkillOpen = false
	c.Chat.ReplaceRange(start, end, "$"+item.Path+" ")
}

// mentionSearchLimit is how many file rows the picker shows.
const mentionSearchLimit = 20

// abandonMentionSearch drops any result still in flight and stops the work
// behind it. Bumping the generation alone only makes the UI ignore the answer;
// the fd process keeps walking the tree. Every path that closes the picker or
// starts a new query goes through here, so the two always happen together.
func (c *ComposerPane) abandonMentionSearch() {
	c.mentionGen++
	if c.mentionCancel != nil {
		c.mentionCancel()
		c.mentionCancel = nil
	}
}

// scheduleMentionSearch debounces, then searches. Each call cancels the search
// in flight: without that, every keystroke leaves an fd process walking the
// tree, and on a large one they pile up faster than they finish.
func (c *ComposerPane) scheduleMentionSearch(query string) {
	if c == nil {
		return
	}
	c.abandonMentionSearch()
	gen := c.mentionGen
	cwd := c.cwd
	publish := c.publish

	ctx, cancel := context.WithCancel(context.Background())
	c.mentionCancel = cancel

	go func() {
		defer cancel()
		// Debounce. A newer keystroke cancels this before any work starts.
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return
		}

		searchCtx, searchCancel := context.WithTimeout(ctx, 3*time.Second)
		defer searchCancel()
		paths, truncated, err := filesearch.Search(searchCtx, cwd, query, mentionSearchLimit)

		// A superseded search has nothing useful to report.
		if ctx.Err() != nil {
			return
		}

		msg := controller.MentionResultsMsg{Gen: gen, Query: query, Paths: paths, Truncated: truncated}
		switch {
		case err == nil:
		case errors.Is(err, filesearch.ErrTimeout):
			msg.ErrText = "Search timed out — type more of the path"
		default:
			msg.ErrText = err.Error()
		}
		if publish != nil {
			publish(msg)
		}
	}()
}

func (c *ComposerPane) acceptMention(item mention.Item) {
	if c == nil {
		return
	}
	_, start, end, ok := chat.ActiveMention(c.Chat.Value, c.Chat.Cursor)
	if !ok {
		start, end = c.Chat.Cursor, c.Chat.Cursor
	}
	c.abandonMentionSearch()
	c.mention.Hide()
	c.Chat.MentionOpen = false
	c.Chat.ReplaceRange(start, end, "@"+item.Path+" ")
}

func (c *ComposerPane) acceptSlash(item mention.Item) {
	if c == nil {
		return
	}
	_, start, end, ok := chat.ActiveSlash(c.Chat.Value, c.Chat.Cursor)
	if !ok {
		start, end = 0, c.Chat.Cursor
	}
	insert := ""
	if c.commands != nil {
		insert = c.commands.LookupInsert(item.Path)
	}
	if insert == "" {
		insert = "/" + item.Path
	}
	c.Chat.ReplaceRange(start, end, insert)
	c.slash.Hide()
	c.Chat.SlashOpen = false
	if !strings.HasSuffix(insert, " ") {
		if c.publish != nil {
			c.publish(controller.SubmitMsg{Text: strings.TrimSpace(insert)})
		}
		if c.drainBus != nil {
			c.drainBus()
		}
	}
}

func newChatInput(theme components.Theme, model, cwd string) chat.ChatInput {
	return chat.ChatInput{
		MinBodyRows:    3,
		MaxBodyRows:    8,
		UseBlockCursor: false,
		PaddingX:       1,
		Theme:          theme,
		BorderStyle:    theme.Border,
		TextStyle:      theme.Foreground,
		CursorStyle:    xui.Style{Reverse: true},
		TopRightLabel: layout.BorderLabel{
			Text:  model,
			Style: theme.Success,
		},
		BottomRightLabel: layout.BorderLabel{
			Text:  pathutil.PathWithBranch(cwd),
			Style: footer.PathLabelStyle(theme),
		},
	}
}

func uniqueImageName(name string, existing []llm.Image) string {
	have := make(map[string]struct{}, len(existing))
	for _, img := range existing {
		have[img.Label()] = struct{}{}
	}
	if _, ok := have[name]; !ok {
		return name
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 2; i < 99; i++ {
		cand := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, ok := have[cand]; !ok {
			return cand
		}
	}
	return name
}

func mentionNavKey(e xui.KeyEvent) bool {
	if !e.Press {
		return false
	}
	switch e.Code {
	case xui.KeyUp, xui.KeyDown, xui.KeyTab, xui.KeyEnter, xui.KeyEscape:
		return true
	case xui.KeyRune:
		if components.AcceptsCmd(e) && (e.Rune == 'n' || e.Rune == 'N' || e.Rune == 'p' || e.Rune == 'P') {
			return true
		}
	}
	return false
}

// Ensure ComposerPane implements Input.
var _ Input = (*ComposerPane)(nil)
