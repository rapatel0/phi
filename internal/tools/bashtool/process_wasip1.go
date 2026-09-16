//go:build wasip1

package bashtool

import (
	"syscall"
)

// processGroupAttr returns an empty attribute set. wasip1 has no POSIX process
// groups, so a shell runs in the group of the runtime that started it.
func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// killProcessTree reports success without signaling. wasip1 has no negative-pid
// kill, and the runtime stops the shell when the instance closes.
func killProcessTree(pid int) error {
	return nil
}
