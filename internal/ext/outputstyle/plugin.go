package outputstyle

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/rapatel0/alpha/internal/brand"
	"github.com/rapatel0/alpha/internal/ext"
	"github.com/rapatel0/alpha/internal/hooks"
)

func init() { ext.Register(&Plugin{}) }

// Plugin swaps a named prompt style into the system prompt each turn.
type Plugin struct {
	st store
}

func (*Plugin) Name() string { return "outputstyle" }

func (p *Plugin) Register(h *ext.Host) error {
	p.st.dirs = defaultDirs()

	// Apply on every turn rather than once: the file may change on disk, and
	// the engine rebuilds nothing between turns.
	h.OnBeforeAgentStart(func(_ context.Context, _, systemPrompt string) (string, error) {
		style, ok := p.st.resolve()
		if !ok {
			// No style, or the selected one was deleted. Strip a block a
			// previous turn added so clearing takes effect.
			return stripApplied(systemPrompt), nil
		}
		return Apply(systemPrompt, style), nil
	})

	h.RegisterCommand(ext.Command{
		Name:        "style",
		Description: "Select an output style, list them, or clear with: /style off",
		Run:         p.run,
	})

	h.AddFooter(func() string {
		if name := p.st.get(); name != "" {
			return "style:" + name
		}
		return ""
	})
	return nil
}

// run handles /style with no argument (list), a name (select), or off (clear).
func (p *Plugin) run(_ context.Context, args []string) (hooks.CommandResult, error) {
	dirs := p.st.styleDirs()
	if len(dirs) == 0 {
		return hooks.CommandResult{}, errNoStyleDir
	}
	styles := Available(dirs)

	if len(args) == 0 {
		return hooks.CommandResult{List: p.list(styles)}, nil
	}

	name := strings.TrimSpace(args[0])
	if name == "off" || name == "none" {
		p.st.set("")
		return hooks.CommandResult{
			Toast:     "style cleared",
			Status:    "",
			StatusSet: true,
		}, nil
	}
	if _, ok := styles[name]; !ok {
		return hooks.CommandResult{}, errUnknownStyle(name, styles)
	}
	p.st.set(name)
	return hooks.CommandResult{
		Toast:     "style: " + name,
		Status:    "style:" + name,
		StatusSet: true,
	}, nil
}

// list builds a palette page. Selecting an item re-runs /style with that name,
// so the list and the direct command share one path.
func (p *Plugin) list(styles map[string]Style) *hooks.CommandList {
	active := p.st.get()
	out := &hooks.CommandList{Title: "Output styles"}
	for _, n := range Names(styles) {
		label := n
		if n == active {
			label = n + " (active)"
		}
		out.Items = append(out.Items, hooks.CommandListItem{
			Label:  label,
			Detail: styles[n].Description,
			Submit: "/style " + n,
		})
	}
	if active != "" {
		out.Items = append(out.Items, hooks.CommandListItem{
			Label:  "off",
			Detail: "Clear the active style",
			Submit: "/style off",
		})
	}
	if len(out.Items) == 0 {
		out.Title = "No styles found under " + strings.Join(p.st.styleDirs(), " or ")
	}
	return out
}

// defaultDirs returns the project style directory first, then the user one, so
// a project can override a personal style of the same name.
func defaultDirs() []string {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	return styleDirs(cwd, home)
}

// styleDirs lists search paths. Earlier entries win. Alpha project and user
// dirs come first, then ~/.agents and peer homes such as ~/.claude/output-styles.
func styleDirs(cwd, home string) []string {
	var dirs []string
	if cwd != "" {
		dirs = append(dirs, filepath.Join(brand.ProjectDir(cwd), "styles"))
		dirs = append(dirs, filepath.Join(brand.AgentsProject(cwd), "output-styles"))
		dirs = append(dirs, brand.PeerJoin(cwd, "output-styles")...)
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(brand.HomeDir(home), "styles"))
		dirs = append(dirs, filepath.Join(brand.AgentsHome(home), "output-styles"))
		dirs = append(dirs, brand.PeerJoin(home, "output-styles")...)
	}
	return dirs
}
