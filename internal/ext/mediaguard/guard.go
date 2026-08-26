// Package mediaguard is the pi-media-guard analog: an aggregate media budget
// for model requests.
//
// Individually valid images become unsafe in aggregate. A long session can
// accumulate more image bytes than the model accepts, and the request fails
// with an opaque provider error. This package keeps the newest images and
// replaces the rest with a factual note, so the turn survives and the model
// still knows something was there.
//
// The stored session is never modified: only the outgoing request is trimmed.
package mediaguard

import (
	"fmt"
	"strings"
	"sync"

	"github.com/rapatel0/alpha/internal/llm"
)

// Defaults chosen to sit under the smallest limit among supported providers
// rather than to match any one of them.
const (
	// DefaultMaxBytes caps the total image payload for one request.
	DefaultMaxBytes = 12 << 20 // 12 MiB
	// DefaultMaxImages caps how many images one request may carry.
	DefaultMaxImages = 20
)

// Budget is the aggregate limit applied to one request.
type Budget struct {
	MaxBytes  int
	MaxImages int
}

// DefaultBudget returns the shipped limits.
func DefaultBudget() Budget {
	return Budget{MaxBytes: DefaultMaxBytes, MaxImages: DefaultMaxImages}
}

// Decision records what one pass did, for /media and for tests.
type Decision struct {
	ImagesBefore int
	ImagesKept   int
	BytesBefore  int
	BytesKept    int
}

// Dropped reports how many images were replaced with a note.
func (d Decision) Dropped() int { return d.ImagesBefore - d.ImagesKept }

// Applied reports whether the budget changed the request.
func (d Decision) Applied() bool { return d.Dropped() > 0 }

// Summary renders the decision for a footer or a command.
func (d Decision) Summary() string {
	if d.ImagesBefore == 0 {
		return "no media"
	}
	if !d.Applied() {
		return fmt.Sprintf("%d img / %s", d.ImagesKept, humanBytes(d.BytesKept))
	}
	return fmt.Sprintf("%d of %d img / %s (%d trimmed)",
		d.ImagesKept, d.ImagesBefore, humanBytes(d.BytesKept), d.Dropped())
}

// Apply trims messages to fit the budget and returns the result.
//
// The newest images are kept, because recent media is what the current turn is
// usually about. A dropped image is replaced by a note naming the file, so the
// model can still refer to it rather than seeing a silent gap.
//
// messages is not modified: only messages that change are copied.
func Apply(messages []llm.Message, b Budget) ([]llm.Message, Decision) {
	if b.MaxBytes <= 0 {
		b.MaxBytes = DefaultMaxBytes
	}
	if b.MaxImages <= 0 {
		b.MaxImages = DefaultMaxImages
	}

	var d Decision
	for _, m := range messages {
		for _, img := range m.Images {
			d.ImagesBefore++
			d.BytesBefore += len(img.Data)
		}
	}
	if d.ImagesBefore == 0 {
		return messages, d
	}

	// Walk newest first so the images nearest the current turn survive.
	keep := map[int]map[int]bool{}
	budget, count := b.MaxBytes, 0
	for mi := len(messages) - 1; mi >= 0; mi-- {
		imgs := messages[mi].Images
		for ii := len(imgs) - 1; ii >= 0; ii-- {
			size := len(imgs[ii].Data)
			if count >= b.MaxImages || size > budget {
				continue
			}
			budget -= size
			count++
			if keep[mi] == nil {
				keep[mi] = map[int]bool{}
			}
			keep[mi][ii] = true
		}
	}
	d.ImagesKept = count
	d.BytesKept = b.MaxBytes - budget
	if d.ImagesKept == d.ImagesBefore {
		return messages, d
	}

	out := make([]llm.Message, len(messages))
	copy(out, messages)
	for mi, m := range out {
		if len(m.Images) == 0 {
			continue
		}
		kept, notes := splitImages(m.Images, keep[mi])
		if len(kept) == len(m.Images) {
			continue
		}
		m.Images = kept
		m.Content = appendNotes(m.Content, notes)
		out[mi] = m
	}
	return out, d
}

// splitImages returns the images to keep and a note for each dropped one.
func splitImages(imgs []llm.Image, keep map[int]bool) ([]llm.Image, []string) {
	var kept []llm.Image
	var notes []string
	for i, img := range imgs {
		if keep[i] {
			kept = append(kept, img)
			continue
		}
		notes = append(notes, describe(img))
	}
	return kept, notes
}

// describe names a dropped image factually. It never guesses at content: the
// point is to tell the model something existed, not to invent a caption.
func describe(img llm.Image) string {
	name := img.Filename
	if name == "" {
		name = "image"
	}
	mime := img.MIME
	if mime == "" {
		mime = "unknown type"
	}
	return fmt.Sprintf("%s (%s, %s)", name, mime, humanBytes(len(img.Data)))
}

// appendNotes adds the evidence note to a message body.
func appendNotes(content string, notes []string) string {
	if len(notes) == 0 {
		return content
	}
	note := "[media omitted to fit the request budget: " +
		strings.Join(notes, "; ") + "]"
	if content == "" {
		return note
	}
	return content + "\n\n" + note
}

// humanBytes renders a byte count for a person, not for arithmetic.
func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fKB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// ledger holds the most recent decision for the footer and /media.
type ledger struct {
	mu   sync.RWMutex
	last Decision
	seen bool
}

func (l *ledger) record(d Decision) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.last, l.seen = d, true
}

func (l *ledger) snapshot() (Decision, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.last, l.seen
}
