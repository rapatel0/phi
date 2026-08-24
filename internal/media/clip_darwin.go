//go:build darwin

package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func readClipboard(ctx context.Context) ([]byte, error) {
	if data, err := runClip(ctx, "pngpaste", "-"); err == nil && DetectMIME(data) != "" {
		return data, nil
	}
	dir, err := os.MkdirTemp("", "alpha-clip-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "clip.png")
	script := `set p to POSIX file ` + appleString(out) + `
set pngData to missing value
try
	set pngData to (the clipboard as «class PNGf»)
end try
if pngData is missing value then error "no image on clipboard"
set f to open for access p with write permission
set eof of f to 0
write pngData to f
close access f
`
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	if err := cmd.Run(); err != nil {
		return nil, ErrEmptyClipboard
	}
	data, err := os.ReadFile(out)
	if err != nil || DetectMIME(data) == "" {
		return nil, ErrEmptyClipboard
	}
	return data, nil
}

func appleString(path string) string {
	path = strings.ReplaceAll(path, `\`, `\\`)
	path = strings.ReplaceAll(path, `"`, `\"`)
	return `"` + path + `"`
}
