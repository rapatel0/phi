package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/rapatel0/alpha/internal/components/palette"
	"github.com/rapatel0/alpha/internal/components/toast"
)

// profileCommand runs /profile.
//
//   - no argument: open the profile picker (or toast a list if none)
//   - create|new <name>: create that profile
//   - <name>: switch to it
func profileCommand(ctx CommandContext) error {
	args := ctx.Args
	if len(args) == 0 {
		if ctx.PushSubmenu != nil {
			cmd := profileSettingsCommand(ctx.SetProfile, ctx.Prefill, ctx.Profile, ctx.Profiles)
			items := cmd.Submenu
			if cmd.SubmenuFn != nil {
				items = cmd.SubmenuFn()
			}
			ctx.PushSubmenu("Select Profile", items)
			return nil
		}
		ctx.toast(profileSummary(ctx), toast.ToastSuccess, 6*time.Second)
		return nil
	}

	if args[0] == "create" || args[0] == "new" {
		if len(args) < 2 {
			if ctx.Prefill != nil {
				ctx.Prefill("/profile create ")
				ctx.toast("type a profile name, then Enter", toast.ToastSuccess, 4*time.Second)
				return nil
			}
			ctx.toast("usage: /profile create <name>", toast.ToastError, 4*time.Second)
			return nil
		}
		return createProfile(ctx, args[1])
	}

	return switchProfile(ctx, strings.TrimSpace(strings.Join(args, " ")))
}

func createProfile(ctx CommandContext, name string) error {
	name = strings.TrimSpace(name)
	if ctx.CreateProfile == nil {
		ctx.toast("profiles are not available here", toast.ToastError, 4*time.Second)
		return nil
	}
	if err := ctx.CreateProfile(name); err != nil {
		ctx.toast(err.Error(), toast.ToastError, 8*time.Second)
		return nil
	}
	ctx.toast("created "+name+". Log in with: alpha login <provider>", toast.ToastSuccess, 8*time.Second)
	return nil
}

func switchProfile(ctx CommandContext, name string) error {
	active := ""
	if ctx.Profile != nil {
		active = ctx.Profile()
	}
	if name == active {
		ctx.toast("already using profile "+name, toast.ToastSuccess, 3*time.Second)
		return nil
	}
	if ctx.SetProfile == nil {
		ctx.toast("profiles are not available here", toast.ToastError, 4*time.Second)
		return nil
	}
	if err := ctx.SetProfile(name); err != nil {
		ctx.toast(err.Error(), toast.ToastError, 8*time.Second)
		return nil
	}
	ctx.toast("switched to profile "+name, toast.ToastSuccess, 4*time.Second)
	return nil
}

// profileSummary lists the profiles on one line, marking the active one.
func profileSummary(ctx CommandContext) string {
	active := ""
	if ctx.Profile != nil {
		active = ctx.Profile()
	}
	var names []string
	if ctx.Profiles != nil {
		names = ctx.Profiles()
	}
	if len(names) == 0 {
		if active == "" {
			return "no profiles"
		}
		return "profile: " + active
	}

	marked := make([]string, 0, len(names))
	for _, n := range names {
		if n == active {
			n += " (active)"
		}
		marked = append(marked, n)
	}
	return fmt.Sprintf("profiles: %s  ·  /profile <name> or /profile create <name>", strings.Join(marked, ", "))
}

// profileSettingsCommand returns the settings → profile submenu.
func profileSettingsCommand(
	onProfile func(name string) error,
	prefill func(text string),
	current func() string,
	list func() []string,
) palette.PaletteCommand {
	build := func() []palette.PaletteCommand {
		var names []string
		if list != nil {
			names = list()
		}
		active := ""
		if current != nil {
			active = current()
		}

		out := make([]palette.PaletteCommand, 0, len(names)+1)
		out = append(out, palette.PaletteCommand{
			ID:       "profile-create",
			Verb:     "create new profile…",
			Keywords: []string{"new", "add", "create"},
			Run: func() {
				if prefill != nil {
					prefill("/profile create ")
				}
			},
		})
		for _, name := range names {
			verb := name
			if name == active {
				verb += "  (active)"
			}
			out = append(out, palette.PaletteCommand{
				ID:   "profile-" + name,
				Verb: verb,
				Run: func() {
					if onProfile != nil {
						_ = onProfile(name)
					}
				},
			})
		}
		return out
	}

	return palette.PaletteCommand{
		ID:           "settings-profile",
		Noun:         "settings",
		Verb:         "profile",
		Keywords:     []string{"profile", "account", "credentials", "login", "create"},
		SubmenuTitle: "Select Profile",
		Submenu:      build(),
		SubmenuFn:    build,
	}
}
