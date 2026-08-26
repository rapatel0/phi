package filesearch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/boyter/gocodewalker"
)

// ErrTimeout reports that the search did not finish in time. A tree with no
// match must be walked in full, which can outlast the caller's deadline.
// Callers show a hint rather than a raw error.
var ErrTimeout = errors.New("file search timed out")

// Search walks cwd in process and returns paths relative to it
// (slash-separated), at most limit. truncated reports that more matches exist
// than limit, so the caller can say the list is partial. An empty query lists
// files without a filter.
//
// The walk respects .gitignore, so the picker never offers a build artifact.
// It stops as soon as limit+1 matches are found: the picker shows one page, so
// walking a large tree to completion is wasted work.
func Search(ctx context.Context, cwd, query string, limit int) (paths []string, truncated bool, err error) {
	if limit <= 0 {
		limit = 20
	}
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return nil, false, err
		}
	}
	// A relative root would make every result relative to the process working
	// directory instead of cwd.
	root, err := filepath.Abs(cwd)
	if err != nil {
		return nil, false, err
	}

	// Match case-insensitively on the path relative to the root. Matching the
	// absolute path would let a directory above the root match everything.
	needle := strings.ToLower(strings.TrimSpace(query))

	// Stopping the walker is the only way to end it early, and it must happen
	// on both the limit and a cancelled context.
	walkCtx, stop := context.WithCancel(ctx)
	defer stop()

	files := make(chan *gocodewalker.File, 256)
	walker := gocodewalker.NewFileWalker(root, files)
	// Hidden files are noise in a picker, and .git alone is thousands of
	// entries on a large repo.
	walker.IgnoreIgnoreFile = false
	walker.IgnoreGitIgnore = false

	// walkErr is written by the walk goroutine and read after the range over
	// files ends. Start closes files, so that close orders the write before
	// the read; done makes the ordering explicit on the early-return path too.
	var walkErr error
	done := make(chan struct{})
	walker.SetErrorHandler(func(error) bool {
		// An unreadable directory must not end the walk. A picker showing
		// fewer files beats a picker showing an error.
		return true
	})

	go func() {
		<-walkCtx.Done()
		walker.Terminate()
	}()
	// Start closes files when the walk ends, so this must not close it too.
	go func() {
		defer close(done)
		if err := walker.Start(); err != nil {
			walkErr = err
		}
	}()

	// Collect one more than needed: the extra match proves more exist.
	out := make([]string, 0, limit+1)
	for f := range files {
		rel, err := filepath.Rel(root, f.Location)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if rel == "" || rel == "." {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(rel), needle) {
			continue
		}
		out = append(out, rel)
		if len(out) > limit {
			// Enough to answer; stop walking and drain so the walker's
			// goroutines exit rather than block on a full channel.
			stop()
			for range files { //nolint:revive // drain to unblock the walker
			}
			return out[:limit], true, nil
		}
	}

	<-done
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return nil, false, ErrTimeout
		}
		return nil, false, ctxErr
	}
	if walkErr != nil {
		return nil, false, walkErr
	}
	return out, false, nil
}
