package filesearch

import (
	"errors"
	"sync"

	"github.com/rapatel0/alpha/internal/project"
)

var (
	fdPathOnce sync.Once
	fdPath     string
	fdPathErr  error
)

// ResolveFD returns the path to the fd binary: ~/.alpha/bin/fd first, then PATH.
func ResolveFD() (string, error) {
	fdPathOnce.Do(func() {
		p, err := project.GetDefaultProject().Global().LookBin("fd")
		if err != nil {
			fdPathErr = errors.New("fd is not available: install to ~/.alpha/bin or PATH")
			return
		}
		fdPath = p
	})
	return fdPath, fdPathErr
}

// ResetResolveFDForTest clears the cached fd path (tests only).
func ResetResolveFDForTest() {
	fdPathOnce = sync.Once{}
	fdPath = ""
	fdPathErr = nil
}
