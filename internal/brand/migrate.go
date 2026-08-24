package brand

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MigrateResult reports what Migrate did.
type MigrateResult struct {
	// Moved is true when the legacy directory became the current one.
	Moved bool
	// From is the legacy directory that was moved.
	From string
	// To is the current directory.
	To string
}

// Migrate moves an existing ~/.phi to ~/.alpha so a rename does not lose
// OAuth tokens, sessions, skills, and hooks.
//
// It does nothing and reports no error when:
//   - the legacy directory does not exist, or
//   - the current directory already exists (never merge or overwrite state), or
//   - the legacy path is a symlink (the user wired it up deliberately).
//
// A failed rename is not fatal to the caller. The current directory is created
// fresh instead, and the legacy directory stays untouched on disk.
func Migrate(home string) (MigrateResult, error) {
	current := HomeDir(home)
	legacy := LegacyHomeDir(home)
	res := MigrateResult{From: legacy, To: current}

	if _, err := os.Lstat(current); err == nil {
		return res, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return res, fmt.Errorf("check %s: %w", current, err)
	}

	info, err := os.Lstat(legacy)
	if errors.Is(err, os.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("check %s: %w", legacy, err)
	}
	// A symlinked legacy path is intentional. Leave it alone rather than
	// moving the link and breaking whatever it points at.
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return res, nil
	}

	if err := os.MkdirAll(filepath.Dir(current), 0o700); err != nil {
		return res, fmt.Errorf("create %s: %w", filepath.Dir(current), err)
	}
	if err := os.Rename(legacy, current); err != nil {
		return res, fmt.Errorf("move %s to %s: %w", legacy, current, err)
	}
	res.Moved = true
	return res, nil
}
