// Package outputstyle is the pi-output-styles analog: named, swappable
// additions to the system prompt, selected with /style.
//
// A style is a markdown file with optional YAML-ish frontmatter:
//
//	---
//	name: concise
//	description: Minimal words; answer first
//	---
//	Answer in the fewest words that are still correct.
//
// Styles are discovered from the project first, then the user directory, so a
// project can override a personal style of the same name.
package outputstyle

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed styles/*.md
var builtinStyles embed.FS

// Builtin returns the styles shipped with the binary. They are the lowest
// priority source, so a user or project file of the same name replaces one.
func Builtin() map[string]Style {
	out := map[string]Style{}
	entries, err := builtinStyles.ReadDir("styles")
	if err != nil {
		return out
	}
	for _, e := range entries {
		data, err := builtinStyles.ReadFile("styles/" + e.Name())
		if err != nil {
			continue
		}
		s := Parse(string(data), strings.TrimSuffix(e.Name(), ".md"))
		out[s.Name] = s
	}
	return out
}

// Style is one named prompt addition.
type Style struct {
	Name        string
	Description string
	Body        string
}

// marker labels the appended block so a second turn replaces it rather than
// stacking another copy.
const marker = "<!-- alpha-output-style:"

var frontmatter = regexp.MustCompile(`(?s)\A---\r?\n(.*?)\r?\n---[ \t]*\r?\n?(.*)\z`)

// Parse reads a style file. Frontmatter is optional; without it the file name
// is the style name and the whole text is the body.
func Parse(text, fallbackName string) Style {
	s := Style{Name: fallbackName, Body: text}
	m := frontmatter.FindStringSubmatch(text)
	if m == nil {
		s.Body = strings.TrimSpace(text)
		return s
	}
	s.Body = strings.TrimSpace(m[2])
	for line := range strings.SplitSeq(m[1], "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		// A quoted value keeps its inner text only.
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			if value != "" {
				s.Name = value
			}
		case "description":
			s.Description = value
		}
	}
	return s
}

// Apply returns base with the style appended, replacing a previously applied
// style rather than stacking. An empty body leaves base untouched.
func Apply(base string, s Style) string {
	base = stripApplied(base)
	if strings.TrimSpace(s.Body) == "" {
		return base
	}
	block := fmt.Sprintf("%s%s -->\n%s", marker, s.Name, strings.TrimSpace(s.Body))
	if base == "" {
		return block
	}
	return base + "\n\n# Personality\n" + block
}

// stripApplied removes a block a previous turn appended, so switching styles
// does not accumulate them.
func stripApplied(text string) string {
	before, _, ok := strings.Cut(text, marker)
	if !ok {
		return text
	}
	// Drop the "# Personality" heading that introduces the block.
	if h := strings.LastIndex(before, "\n\n# Personality\n"); h >= 0 {
		return text[:h]
	}
	return strings.TrimRight(before, "\n")
}

// Discover reads styles from dirs only. Earlier directories win, so a project
// style shadows a user style with the same name. Use Available to include the
// built-in styles.
func Discover(dirs []string) map[string]Style {
	out := map[string]Style{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a missing style directory is normal
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".md")
			if _, taken := out[name]; taken {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			style := Parse(string(data), name)
			out[style.Name] = style
		}
	}
	return out
}

// Available returns every style a user can select: files under dirs first,
// then the built-in styles for any name a file did not already claim.
func Available(dirs []string) map[string]Style {
	out := Discover(dirs)
	for name, style := range Builtin() {
		if _, taken := out[name]; !taken {
			out[name] = style
		}
	}
	return out
}

// Names returns the discovered style names in a stable order.
func Names(styles map[string]Style) []string {
	names := make([]string, 0, len(styles))
	for n := range styles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// store holds the active selection. It is process-wide because the system
// prompt is process-wide.
type store struct {
	mu     sync.RWMutex
	active string
	dirs   []string
}

func (s *store) set(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = name
}

func (s *store) get() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *store) styleDirs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.dirs...)
}

// resolve returns the active style, or false when none is selected or the
// selected one no longer exists.
func (s *store) resolve() (Style, bool) {
	name := s.get()
	if name == "" {
		return Style{}, false
	}
	st, ok := Available(s.styleDirs())[name]
	return st, ok
}

// errUnknownStyle reports a name that is not on disk, listing what is.
func errUnknownStyle(name string, styles map[string]Style) error {
	available := strings.Join(Names(styles), ", ")
	if available == "" {
		available = "(none found)"
	}
	return fmt.Errorf("unknown style %q. Available: %s", name, available)
}

var errNoStyleDir = errors.New("no style directory is configured")
