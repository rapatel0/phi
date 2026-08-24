package media

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/rapatel0/alpha/internal/llm"
)

const clipTimeout = 2 * time.Second

// ReadClipboard loads an image from the OS clipboard and normalizes it.
func ReadClipboard() (llm.Image, error) {
	ctx, cancel := context.WithTimeout(context.Background(), clipTimeout)
	defer cancel()
	data, err := readClipboard(ctx)
	if err != nil {
		if errors.Is(err, ErrEmptyClipboard) {
			return llm.Image{}, err
		}
		return llm.Image{}, fmtClip(err)
	}
	if DetectMIME(data) == "" {
		return llm.Image{}, ErrEmptyClipboard
	}
	return Normalize(llm.Image{Data: data, Filename: "clipboard" + extFor(DetectMIME(data))})
}

func fmtClip(err error) error {
	if err == nil {
		return ErrEmptyClipboard
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmtClipTimeout()
	}
	return err
}

func fmtClipTimeout() error {
	return errors.New("media: clipboard read timed out")
}

func runClip(ctx context.Context, name string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath(name); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if stdout.Len() == 0 {
			return nil, err
		}
	}
	out := stdout.Bytes()
	if len(out) == 0 {
		return nil, ErrEmptyClipboard
	}
	return out, nil
}
