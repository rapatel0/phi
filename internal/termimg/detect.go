package termimg

import (
	"os"
	"strings"
)

// Supported reports whether Kitty-style inline graphics should be used.
func Supported() bool {
	switch os.Getenv("PHI_KITTY_GRAPHICS") {
	case "0", "false", "no":
		return false
	case "1", "true", "yes":
		return true
	}
	// tmux swallows APC unless allow-passthrough is on; skip by default.
	if os.Getenv("TMUX") != "" && os.Getenv("KITTY_WINDOW_ID") == "" &&
		os.Getenv("GHOSTTY_RESOURCES_DIR") == "" {
		return false
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	if os.Getenv("GHOSTTY_RESOURCES_DIR") != "" || os.Getenv("GHOSTTY_BIN") != "" {
		return true
	}
	term := strings.ToLower(os.Getenv("TERM"))
	if strings.Contains(term, "kitty") || strings.Contains(term, "ghostty") {
		return true
	}
	return false
}
