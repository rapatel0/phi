// Package profile selects which set of credentials alpha uses.
//
// One person often holds several accounts for the same provider: a work
// subscription and a personal one, or a client's key kept apart from their
// own. A single auth.json forces a re-login to move between them, which loses
// the credential that was already there.
//
// A profile is a named directory holding its own auth.json. Switching profile
// switches every provider at once, so a work profile can carry a work
// Anthropic login and a work Gemini key together.
package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/rapatel0/alpha/internal/brand"
)

// Default is the profile used when none is selected. Its credentials live in
// the original ~/.alpha/auth.json, so an existing install keeps working and
// nothing has to be migrated.
const Default = "default"

// EnvVar names the environment variable that selects a profile.
const EnvVar = brand.EnvPrefix + "PROFILE"

// pointerFile holds the persisted choice from "alpha profile use".
const pointerFile = "profile"

// validName allows what is safe in a directory name on every platform. A
// profile name becomes a path, so a name with a separator or a leading dot
// would escape the profiles directory or hide the result.
var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// ValidateName reports whether a profile name can be used as a directory.
func ValidateName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return errors.New("profile: empty name")
	case len(name) > 64:
		return fmt.Errorf("profile: name is longer than 64 characters: %q", name)
	case !validName.MatchString(name):
		return fmt.Errorf(
			"profile: invalid name %q: use letters, digits, dot, dash, or underscore, starting with a letter or digit",
			name)
	case strings.Contains(name, ".."):
		return fmt.Errorf("profile: invalid name %q: %q is not allowed in a name", name, "..")
	}
	return nil
}

// Resolve returns the active profile name and where it came from.
//
// The environment wins over the persisted pointer, so a profile can be set for
// one command or one shell without changing what every other shell uses. That
// is what makes a per-project .envrc work.
func Resolve(root string) (name, source string) {
	if v := strings.TrimSpace(os.Getenv(EnvVar)); v != "" {
		if err := ValidateName(v); err != nil {
			// An unusable name must not silently fall back to another
			// profile's credentials: say so and use the default.
			fmt.Fprintf(os.Stderr, "alpha: %s: %v (using %s)\n", EnvVar, err, Default)
			return Default, "fallback"
		}
		return v, EnvVar
	}
	if v := readPointer(root); v != "" {
		return v, "alpha profile use"
	}
	return Default, "default"
}

// Dir returns the directory holding a profile's state.
//
// The default profile stays at the global root rather than under profiles/,
// so an install that never uses profiles keeps its files where they were.
func Dir(root, name string) string {
	if name == "" || name == Default {
		return root
	}
	return filepath.Join(root, "profiles", name)
}

// AuthFile returns the credential store for a profile.
func AuthFile(root, name string) string {
	return filepath.Join(Dir(root, name), "auth.json")
}

// List returns every profile that exists, including the default.
func List(root string) []string {
	out := []string{Default}
	entries, err := os.ReadDir(filepath.Join(root, "profiles"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() || ValidateName(e.Name()) != nil {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// Exists reports whether a profile has been created.
//
// The default always exists: it is the global root, which alpha creates at
// startup.
func Exists(root, name string) bool {
	if name == Default {
		return true
	}
	st, err := os.Stat(Dir(root, name))
	return err == nil && st.IsDir()
}

// Create makes a profile directory. Creating one that exists is not an error,
// so the command is safe to repeat.
func Create(root, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if name == Default {
		return nil
	}
	return os.MkdirAll(Dir(root, name), 0o700)
}

// Use persists the profile to fall back on when the environment says nothing.
//
// Passing Default removes the pointer rather than writing the name, so the
// file exists only while a non-default profile is chosen.
func Use(root, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if name != Default && !Exists(root, name) {
		return fmt.Errorf("profile: %q does not exist: create it with 'alpha profile create %s'", name, name)
	}
	path := filepath.Join(root, pointerFile)
	if name == Default {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("profile: clear pointer: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("profile: mkdir: %w", err)
	}
	if err := os.WriteFile(path, []byte(name+"\n"), 0o600); err != nil {
		return fmt.Errorf("profile: write pointer: %w", err)
	}
	return nil
}

// Delete removes a profile and the credentials it holds.
func Delete(root, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	if name == Default {
		return errors.New("profile: cannot delete the default profile")
	}
	if !Exists(root, name) {
		return fmt.Errorf("profile: %q does not exist", name)
	}
	if err := os.RemoveAll(Dir(root, name)); err != nil {
		return fmt.Errorf("profile: delete %s: %w", name, err)
	}
	// A pointer to a deleted profile would resolve to a directory that is
	// gone, which reads as "logged out of everything".
	if cur := readPointer(root); cur == name {
		return Use(root, Default)
	}
	return nil
}

// readPointer returns the persisted profile name, or "" when unset or unusable.
func readPointer(root string) string {
	data, err := os.ReadFile(filepath.Join(root, pointerFile))
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(data))
	if ValidateName(name) != nil {
		return ""
	}
	return name
}
