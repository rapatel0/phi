//go:build windows

package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

func readClipboard(ctx context.Context) ([]byte, error) {
	dir, err := os.MkdirTemp("", "alpha-clip-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "clip.png")
	ps := `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$img = [System.Windows.Forms.Clipboard]::GetImage()
if ($null -eq $img) { exit 1 }
$img.Save('` + out + `', [System.Drawing.Imaging.ImageFormat]::Png)
`
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	if err := cmd.Run(); err != nil {
		return nil, ErrEmptyClipboard
	}
	data, err := os.ReadFile(out)
	if err != nil || DetectMIME(data) == "" {
		return nil, ErrEmptyClipboard
	}
	return data, nil
}
