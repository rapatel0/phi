package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/rapatel0/alpha/internal/components/palette"
	"github.com/rapatel0/alpha/internal/components/toast"
)

// profileCommand runs /profile: no argument lists the profiles and marks the
// active one, a name switches to it.
func profileCommand(ctx CommandContext) error {
	name := strings.TrimSpace(strings.Join(ctx.Args, " "))
	if name == "" {
		ctx.toast(profileSummary(ctx), toast.ToastSuccess, 6*time.Second)
		return nil
	}

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
		// The usual cause is a profile that is not logged in to the model
		// in use, which the message names.
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
	return fmt.Sprintf("profiles: %s  ·  switch with /profile <name>", strings.Join(marked, ", "))
}

// profileSettingsCommand returns the settings → profile submenu.
func profileSettingsCommand(
	onProfile func(name string) error,
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

		out := make([]palette.PaletteCommand, 0, len(names))
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
						// The error surfaces through the slash
						// path; here the footer shows the result.
						_ = onProfile(name)
					}
				},
			})
		}
		if len(out) == 0 {
			out = append(out, palette.PaletteCommand{
				ID:       "profile-empty",
				Verb:     "No profiles configured",
				Disabled: true,
			})
		}
		return out
	}

	return palette.PaletteCommand{
		ID:           "settings-profile",
		Noun:         "settings",
		Verb:         "profile",
		Keywords:     []string{"profile", "account", "credentials", "login"},
		SubmenuTitle: "Select Profile",
		Submenu:      build(),
		SubmenuFn:    build,
	}
}
