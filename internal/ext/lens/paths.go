package lens

import (
	"os"
	"path/filepath"
	"strings"
)

// fileExists reports whether a path exists and is not a directory.
func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// relDir returns the directory of file relative to root, as a package path.
//
// It falls back to "." when the file is outside root, which makes a
// package-wide checker run against the project rather than a path that
// escapes it.
func relDir(root, file string) string {
	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, file)
	}
	rel, err := filepath.Rel(root, filepath.Dir(abs))
	if err != nil || rel == "" || strings.HasPrefix(rel, "..") {
		return "."
	}
	return rel
}

// sameFile reports whether two paths name the same file.
//
// Checkers disagree about how they print a path: go vet prints it relative to
// the module root, ruff prints it as given, and gopls prints it absolute.
// Comparing the resolved absolute paths is what makes one filter work for all
// of them.
func sameFile(root, a, b string) bool {
	return resolve(root, a) == resolve(root, b)
}

// resolve makes a path absolute and follows symlinks when it can.
//
// EvalSymlinks is what handles a root under /tmp on macOS, where /tmp is a
// link to /private/tmp: without it a checker's absolute path never matches
// the path the tool was given.
func resolve(root, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}
