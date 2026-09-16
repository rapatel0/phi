package composer

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/pulseaiclub/xui"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/components/mention"
	"github.com/rapatel0/alpha/internal/tui/commands"
)

const testPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func TestAcceptedImageMentionAttachesOnlyWhenEnabled(t *testing.T) {
	dir := t.TempDir()
	data, err := base64.StdEncoding.DecodeString(testPNG)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pixel.png"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	c := NewComposerPane(components.DefaultTheme(), "m", dir)
	c.imageEnabled = func() bool { return true }
	c.Chat.Value = "@pixel.png"
	c.Chat.Cursor = len(c.Chat.Value)
	c.acceptMention(mention.Item{Path: "pixel.png"})
	if len(c.PendingImages()) != 1 || c.Chat.Value != "" {
		t.Fatalf("images=%d value=%q", len(c.PendingImages()), c.Chat.Value)
	}

	c = NewComposerPane(components.DefaultTheme(), "m", dir)
	c.imageEnabled = func() bool { return false }
	c.Chat.Value = "@pixel.png"
	c.Chat.Cursor = len(c.Chat.Value)
	c.acceptMention(mention.Item{Path: "pixel.png"})
	if len(c.PendingImages()) != 0 || c.Chat.Value != "@pixel.png " {
		t.Fatalf("images=%d value=%q", len(c.PendingImages()), c.Chat.Value)
	}
}

func TestSlashTabFillsWithoutSubmitting(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", t.TempDir())
	reg := commands.NewCommandRegistry()
	reg.Register(commands.Command{Name: "clear", Slash: true, Insert: "/clear"})
	c.Wire(nil, nil, reg, c.cwd, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	c.Chat.Value = "/cl"
	c.Chat.Cursor = len(c.Chat.Value)
	c.onSlashChange(true, "cl")
	c.Handle(&components.EventContext{}, xui.KeyEvent{Code: xui.KeyTab, Press: true})
	if c.Chat.Value != "/clear " || c.slash.Open {
		t.Fatalf("value=%q open=%v", c.Chat.Value, c.slash.Open)
	}
}

func TestQuestionPickerUsesComposerOverlay(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", t.TempDir())
	c.Chat.Value = "?ctrl"
	c.Chat.Cursor = len(c.Chat.Value)
	c.onQuestionChange(true, "ctrl")
	if !c.Chat.QuestionOpen || !c.question.Open || len(c.question.Items) == 0 {
		t.Fatal("question picker did not open")
	}
	c.Handle(&components.EventContext{}, xui.KeyEvent{Code: xui.KeyEscape, Press: true})
	if c.question.Open || c.Chat.QuestionOpen {
		t.Fatal("question picker did not close")
	}
}
