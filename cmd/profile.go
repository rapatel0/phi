package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/profile"
	"github.com/rapatel0/alpha/internal/project"
)

func profileCmd(args []string) int {
	sub := ""
	if len(args) > 0 {
		sub = strings.ToLower(strings.TrimSpace(args[0]))
	}
	if sub == "" || sub == "-h" || sub == "--help" || sub == "help" {
		printProfileUsage(os.Stdout)
		if sub == "" {
			return ExitUsage
		}
		return ExitOK
	}

	proj, err := project.Discover("")
	if err != nil {
		fmt.Fprintln(os.Stderr, "alpha profile:", err)
		return ExitError
	}
	root := proj.Global().Root()

	switch sub {
	case "list", "ls":
		return profileList(root)
	case "show", "current":
		return profileShow(root)
	case "create", "new":
		return profileMutate(root, args[1:], "create", profile.Create)
	case "use", "switch":
		return profileMutate(root, args[1:], "use", profile.Use)
	case "delete", "rm", "remove":
		return profileMutate(root, args[1:], "delete", profile.Delete)
	default:
		fmt.Fprintf(os.Stderr, "alpha profile: unknown subcommand %q (want list, show, create, use, delete)\n", sub)
		return ExitUsage
	}
}

// profileList prints every profile with the providers it holds, marking the
// active one. The providers are what makes the list useful: a name alone does
// not say whether that profile is logged in.
func profileList(root string) int {
	active, source := profile.Resolve(root)
	for _, name := range profile.List(root) {
		marker := " "
		if name == active {
			marker = "*"
		}
		providers := auth.OpenStore(profile.AuthFile(root, name)).Providers()
		detail := "no credentials"
		if len(providers) > 0 {
			detail = strings.Join(providers, ", ")
		}
		fmt.Printf("%s %-16s %s\n", marker, name, detail)
	}
	fmt.Fprintf(os.Stderr, "\nactive: %s (from %s)\n", active, source)
	return ExitOK
}

// profileShow prints only the active profile name, so a shell prompt or script
// can read it without parsing the list.
func profileShow(root string) int {
	active, source := profile.Resolve(root)
	fmt.Println(active)
	fmt.Fprintf(os.Stderr, "from %s; credentials in %s\n", source, profile.AuthFile(root, active))
	return ExitOK
}

// profileMutate runs one name-taking subcommand and reports the result.
func profileMutate(root string, args []string, verb string, fn func(string, string) error) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "alpha profile %s: need a profile name\n", verb)
		return ExitUsage
	}
	name := strings.TrimSpace(args[0])
	if err := fn(root, name); err != nil {
		fmt.Fprintln(os.Stderr, "alpha profile:", err)
		return ExitError
	}

	switch verb {
	case "create":
		fmt.Fprintf(os.Stderr, "created profile %s\n", name)
		fmt.Fprintf(os.Stderr, "log in to it with:  %s=%s alpha login anthropic\n", profile.EnvVar, name)
	case "use":
		fmt.Fprintf(os.Stderr, "now using profile %s\n", name)
		if env := strings.TrimSpace(os.Getenv(profile.EnvVar)); env != "" && env != name {
			// The pointer was written but will not take effect here,
			// which otherwise looks like the command did nothing.
			fmt.Fprintf(os.Stderr, "warning: %s=%s is set and overrides this; unset it to use %s\n",
				profile.EnvVar, env, name)
		}
	case "delete":
		fmt.Fprintf(os.Stderr, "deleted profile %s and its credentials\n", name)
	}
	return ExitOK
}

func printProfileUsage(w *os.File) {
	fmt.Fprintf(w, `usage: alpha profile <subcommand>

  alpha profile list            list profiles and the providers each holds
  alpha profile show            print the active profile name
  alpha profile create <name>   create an empty profile
  alpha profile use <name>      make <name> the default for later commands
  alpha profile delete <name>   delete a profile and its credentials

A profile is a named set of credentials. Each one has its own auth.json, so a
work login and a personal login can both exist without overwriting each other.

The active profile is chosen in this order:

  1. %s in the environment
  2. the profile set by 'alpha profile use'
  3. %s

The environment wins, so one shell or one project can use a different profile
without changing the others:

  export %s=work          # this shell
  %s=work alpha           # one command

The %s profile keeps its credentials in ~/.alpha/auth.json. A named profile
uses ~/.alpha/profiles/<name>/auth.json.
`, profile.EnvVar, profile.Default, profile.EnvVar, profile.EnvVar, profile.Default)
}
