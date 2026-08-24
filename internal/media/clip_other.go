//go:build !darwin && !linux && !windows

package media

import "context"

func readClipboard(context.Context) ([]byte, error) {
	return nil, ErrEmptyClipboard
}
