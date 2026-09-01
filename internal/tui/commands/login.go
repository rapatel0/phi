package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/components/palette"
	"github.com/rapatel0/alpha/internal/components/toast"
	"github.com/rapatel0/alpha/internal/profile"
	"github.com/rapatel0/alpha/internal/project"
)

var loginProviders = []struct {
	id, label, hint string
}{
	{"anthropic", "Anthropic", "Claude Pro/Max browser OAuth"},
	{"codex", "Codex", "ChatGPT device code"},
	{"xai", "xAI", "SuperGrok device code"},
	{"gemini", "Gemini", "paste an AI Studio API key"},
	{"antigravity", "Antigravity", "Google browser OAuth"},
}

// loginCommand runs /login.
//
//	/login                     provider picker
//	/login <provider>          show the CLI command for the active profile
//	/login gemini <key>        store a Gemini key in the active profile
func loginCommand(ctx CommandContext) error {
	args := ctx.Args
	profileName := activeProfile(ctx)
	if len(args) == 0 {
		if ctx.PushSubmenu != nil {
			ctx.PushSubmenu("Login · profile "+profileName, loginProviderItems(ctx, profileName))
			return nil
		}
		ctx.toast(loginCLI(profileName, "<provider>"), toast.ToastSuccess, 8*time.Second)
		return nil
	}
	if args[0] == "gemini" || args[0] == "google" {
		if len(args) >= 2 {
			return storeGeminiKey(ctx, profileName, strings.Join(args[1:], " "))
		}
		ctx.toast("usage: /login gemini <api-key>", toast.ToastError, 4*time.Second)
		return nil
	}
	provider := strings.ToLower(strings.TrimSpace(args[0]))
	if !knownLoginProvider(provider) {
		ctx.toast("unknown provider "+provider+" (want anthropic, codex, xai, gemini, antigravity)", toast.ToastError, 6*time.Second)
		return nil
	}
	ctx.toast(loginCLI(profileName, provider), toast.ToastSuccess, 10*time.Second)
	return nil
}

func loginProviderItems(ctx CommandContext, profileName string) []palette.PaletteCommand {
	out := make([]palette.PaletteCommand, 0, len(loginProviders))
	for _, p := range loginProviders {
		p := p
		out = append(out, palette.PaletteCommand{
			ID:       "login-" + p.id,
			Verb:     p.label,
			Keywords: []string{"login", "auth", p.id},
			Run: func() {
				if p.id == "gemini" {
					if ctx.Prefill != nil {
						ctx.Prefill("/login gemini ")
					}
					ctx.toast("paste the Gemini API key after /login gemini", toast.ToastSuccess, 6*time.Second)
					return
				}
				ctx.toast(loginCLI(profileName, p.id), toast.ToastSuccess, 10*time.Second)
			},
		})
	}
	return out
}

func loginCLI(profileName, provider string) string {
	if profileName == "" || profileName == profile.Default {
		return fmt.Sprintf("in a terminal:  alpha login %s", provider)
	}
	return fmt.Sprintf("in a terminal:  alpha login --profile %s %s", profileName, provider)
}

func knownLoginProvider(name string) bool {
	switch name {
	case "anthropic", "claude", "codex", "chatgpt", "openai", "xai", "grok", "supergrok", "gemini", "google", "antigravity", "google-antigravity":
		return true
	default:
		return false
	}
}

func activeProfile(ctx CommandContext) string {
	if ctx.Profile != nil {
		if n := strings.TrimSpace(ctx.Profile()); n != "" {
			return n
		}
	}
	return profile.Default
}

func storeGeminiKey(ctx CommandContext, profileName, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		ctx.toast("empty Gemini key", toast.ToastError, 4*time.Second)
		return nil
	}
	proj, err := project.Discover(ctx.Cwd)
	if err != nil {
		ctx.toast(err.Error(), toast.ToastError, 6*time.Second)
		return nil
	}
	root := proj.Global().Root()
	if _, err := profile.Create(root, profileName); err != nil {
		ctx.toast(err.Error(), toast.ToastError, 6*time.Second)
		return nil
	}
	path := profile.AuthFile(root, profileName)
	if err := auth.OpenStore(path).Put(auth.Credential{
		Provider:    auth.ProviderGemini,
		AccessToken: key,
	}); err != nil {
		ctx.toast(err.Error(), toast.ToastError, 6*time.Second)
		return nil
	}
	ctx.toast("saved Gemini key for profile "+profileName, toast.ToastSuccess, 6*time.Second)
	return nil
}
