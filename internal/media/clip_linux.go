//go:build linux

package media

import "context"

func readClipboard(ctx context.Context) ([]byte, error) {
	if data, err := runClip(
		ctx,
		"wl-paste",
		"--no-newline",
		"--type",
		"image/png",
	); err == nil &&
		DetectMIME(data) != "" {
		return data, nil
	}
	if data, err := runClip(
		ctx,
		"wl-paste",
		"--no-newline",
		"--type",
		"image/jpeg",
	); err == nil &&
		DetectMIME(data) != "" {
		return data, nil
	}
	if data, err := runClip(
		ctx,
		"xclip",
		"-selection",
		"clipboard",
		"-t",
		"image/png",
		"-o",
	); err == nil &&
		DetectMIME(data) != "" {
		return data, nil
	}
	if data, err := runClip(
		ctx,
		"xclip",
		"-selection",
		"clipboard",
		"-t",
		"image/jpeg",
		"-o",
	); err == nil &&
		DetectMIME(data) != "" {
		return data, nil
	}
	return nil, ErrEmptyClipboard
}
