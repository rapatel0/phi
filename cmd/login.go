package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/profile"
	"github.com/rapatel0/alpha/internal/project"
)

func loginCmd(args []string) int {
	opts, err := parseLoginArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "alpha login:", err)
		printLoginUsage(os.Stderr)
		return ExitUsage
	}
	if opts.help || opts.provider == "" {
		printLoginUsage(os.Stdout)
		if opts.provider == "" && !opts.help {
			return ExitUsage
		}
		return ExitOK
	}
	provider := opts.provider

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	proj, err := project.Discover("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "alpha login:", err)
		return ExitError
	}
	root := proj.Global().Root()
	profileName := opts.profile
	if profileName == "" {
		profileName, _ = profile.Resolve(root)
	} else {
		if _, err := profile.Create(root, profileName); err != nil {
			fmt.Fprintln(os.Stderr, "alpha login:", err)
			return ExitUsage
		}
	}
	authFile := profile.AuthFile(root, profileName)
	store := auth.OpenStore(authFile)

	var cred auth.Credential
	switch provider {
	case "anthropic", "claude":
		fmt.Fprintln(os.Stderr, "Claude Pro/Max login. A browser window should open.")
		fmt.Fprintln(os.Stderr, "If it doesn't, paste the URL below into a browser.")
		fmt.Fprintln(os.Stderr, "On another machine, paste the final redirect URL here.")
		paste := stdinLines(ctx)
		cred, err = auth.LoginAnthropic(ctx, auth.LoginOpts{
			OpenBrowser: auth.OpenBrowser,
			OnURL: func(u string) {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, u)
				fmt.Fprintln(os.Stderr)
			},
			Paste: paste,
		})
	case "xai", "grok", "supergrok":
		sess, startErr := auth.StartXAIDevice(ctx)
		if startErr != nil {
			fmt.Fprintln(os.Stderr, "alpha login:", startErr)
			return ExitError
		}
		fmt.Fprintln(os.Stderr, "SuperGrok / X Premium login. Open this URL and enter the code:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  ", sess.VerificationURL)
		fmt.Fprintln(os.Stderr, "  code:", sess.UserCode)
		fmt.Fprintln(os.Stderr)
		_ = auth.OpenBrowser(sess.VerificationURL)
		cred, err = auth.CompleteXAIDevice(ctx, sess)
	case "antigravity", "google-antigravity":
		fmt.Fprintln(os.Stderr, "Google Antigravity login. A browser window should open.")
		fmt.Fprintln(os.Stderr, "Approve the consent screen, then paste the URL you land on here.")
		fmt.Fprintln(os.Stderr, "The page will not load: copy the address from the browser bar.")
		paste := stdinLines(ctx)
		cred, err = auth.LoginAntigravity(ctx, auth.LoginOpts{
			OpenBrowser: auth.OpenBrowser,
			OnURL: func(u string) {
				fmt.Fprintln(os.Stderr)
				fmt.Fprintln(os.Stderr, u)
				fmt.Fprintln(os.Stderr)
			},
			Paste: paste,
		})
	case "gemini", "google":
		fmt.Fprintln(os.Stderr, "Paste a Gemini API key (AI Studio / GEMINI_API_KEY).")
		fmt.Fprint(os.Stderr, "key: ")
		sc := bufio.NewScanner(os.Stdin)
		if !sc.Scan() {
			fmt.Fprintln(os.Stderr, "alpha login: no key")
			return ExitError
		}
		key := strings.TrimSpace(sc.Text())
		if key == "" {
			fmt.Fprintln(os.Stderr, "alpha login: empty key")
			return ExitError
		}
		cred = auth.Credential{Provider: auth.ProviderGemini, AccessToken: key}
	case "codex", "chatgpt", "openai":
		sess, startErr := auth.StartCodexDevice(ctx)
		if startErr != nil {
			fmt.Fprintln(os.Stderr, "alpha login:", startErr)
			return ExitError
		}
		fmt.Fprintln(os.Stderr, "ChatGPT Codex login. Open this URL and enter the code:")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  ", sess.VerificationURL)
		fmt.Fprintln(os.Stderr, "  code:", sess.UserCode)
		fmt.Fprintln(os.Stderr)
		_ = auth.OpenBrowser(sess.VerificationURL)
		cred, err = auth.CompleteCodexDevice(ctx, sess)
	default:
		fmt.Fprintf(os.Stderr,
			"alpha login: unknown provider %q (want anthropic, codex, xai, gemini, antigravity)\n", provider)
		return ExitUsage
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "alpha login:", err)
		return ExitError
	}
	if err := store.Put(cred); err != nil {
		fmt.Fprintln(os.Stderr, "alpha login:", err)
		return ExitError
	}
	fmt.Fprintf(os.Stderr, "saved %s credentials for profile %s\n  %s\n", cred.Provider, profileName, authFile)
	if profileName != profile.Default {
		fmt.Fprintf(os.Stderr, "switch with:  alpha profile use %s\n", profileName)
	}
	fmt.Fprintln(os.Stderr, "Those models now appear in the TUI palette (Ctrl+K → settings → model).")
	return ExitOK
}

type loginOpts struct {
	provider string
	profile  string
	help     bool
}

func parseLoginArgs(args []string) (loginOpts, error) {
	var opts loginOpts
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "help":
			opts.help = true
		case a == "--profile" || a == "-p":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("%s needs a profile name", a)
			}
			opts.profile = strings.TrimSpace(args[i])
		case strings.HasPrefix(a, "--profile="):
			opts.profile = strings.TrimSpace(strings.TrimPrefix(a, "--profile="))
		case strings.HasPrefix(a, "-") && a != "-":
			return opts, fmt.Errorf("unknown flag %s", a)
		default:
			if opts.provider != "" {
				return opts, fmt.Errorf("unexpected argument %q", a)
			}
			opts.provider = strings.ToLower(strings.TrimSpace(a))
		}
	}
	return opts, nil
}

func stdinLines(ctx context.Context) <-chan string {
	ch := make(chan string, 1)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(os.Stdin)
		if !sc.Scan() {
			return
		}
		select {
		case ch <- sc.Text():
		case <-ctx.Done():
		}
	}()
	return ch
}

func printLoginUsage(w *os.File) {
	fmt.Fprintf(w, `usage: alpha login [--profile NAME] <provider>

  alpha login anthropic                 Claude Pro/Max (browser OAuth)
  alpha login --profile work anthropic  same, into the work profile
  alpha login codex                     ChatGPT Codex (device code)
  alpha login xai                       SuperGrok / X Premium (device code)
  alpha login gemini                    Google Gemini API key (AI Studio)
  alpha login antigravity               Google Antigravity (browser OAuth)

Credentials are stored in that profile's auth.json (mode 0600).
--profile writes there without switching the default profile.
See 'alpha profile --help' to create and switch profiles.
Config models[].api_key still wins when set.

antigravity reaches Gemini and Claude models through Google's internal
Antigravity endpoint. It is undocumented and can stop working without
notice; alpha reports that as a distinct error rather than a login problem.
`)
}
