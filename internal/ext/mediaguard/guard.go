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

// Budgets are expressed in raw bytes, but every provider states its limit on
// the base64-encoded payload, which is 4/3 the size. Counting raw bytes
// against a base64 limit overshoots by a third: a 12 MiB raw budget is 16 MiB
// on the wire. The constants below are therefore raw values derived from the
// documented encoded limit.
const (
	// DefaultMaxBytes caps the total image payload for one request. It is the
	// raw budget whose encoded form stays under the smallest documented
	// request limit, which is Gemini's 20 MB for the whole request. Half of
	// that is left for prompt text, tool schemas, and the rest of the turn.
	DefaultMaxBytes = 7 << 20 // 7 MiB raw is about 9.8 MB base64
	// DefaultMaxImages caps how many images one request may carry. Anthropic
	// applies a stricter per-image dimension limit above 20 images, so 20 is
	// the point where behavior changes rather than an arbitrary count.
	DefaultMaxImages = 20
)

// encodedRatio converts a raw byte count to its base64 size.
const encodedRatio = 4.0 / 3.0

// Budget is the aggregate limit applied to one request.
type Budget struct {
	// MaxBytes is the total raw image payload allowed, before base64.
	MaxBytes  int
	MaxImages int
}

// EncodedBytes reports what MaxBytes becomes on the wire, which is the number
// a provider limit actually applies to.
func (b Budget) EncodedBytes() int { return int(float64(b.MaxBytes) * encodedRatio) }

// DefaultBudget returns the limits used when the provider is unknown.
func DefaultBudget() Budget {
	return Budget{MaxBytes: DefaultMaxBytes, MaxImages: DefaultMaxImages}
}

// BudgetFor returns the budget for a provider, falling back to DefaultBudget
// for one that is not recognized.
//
// The values come from each provider's published limits. They are deliberately
// conservative: a request that is refused costs a turn, while one that is
// trimmed too eagerly only loses an older image that the note still describes.
//
// Anthropic and OpenAI both downscale server-side, so sending more pixels than
// their tiers accept buys nothing. The budgets here bound total payload, not
// resolution, and leave resolution to the provider.
func BudgetFor(provider string) Budget {
	switch provider {
	case "anthropic":
		// 32 MB per request, and 10 MB per image on the direct API but 5 MB
		// on Bedrock and Vertex. Sizing for the 5 MB floor keeps the same
		// build working on every platform.
		return Budget{MaxBytes: 18 << 20, MaxImages: 20}
	case "openai", "codex":
		// 512 MB per request and 1500 images, far above anything a terminal
		// session produces. The cap here exists to bound context growth, not
		// to satisfy the API.
		return Budget{MaxBytes: 24 << 20, MaxImages: 40}
	case "gemini":
		// 20 MB covers the whole inline request, not just images, so the
		// image share must leave room for everything else in the turn.
		return Budget{MaxBytes: 7 << 20, MaxImages: 20}
	case "xai":
		// 20 MiB per image with no documented image count limit.
		return Budget{MaxBytes: 14 << 20, MaxImages: 20}
	default:
		return DefaultBudget()
	}
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
	mu sync.RWMutex
	// budget and provider are recorded with the decision because the budget
	// is chosen per request: reporting a stored default would describe a
	// limit that was never applied.
	last     Decision
	budget   Budget
	provider string
	seen     bool
}

func (l *ledger) record(d Decision, b Budget, provider string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.last, l.budget, l.provider, l.seen = d, b, provider, true
}

func (l *ledger) snapshot() (Decision, Budget, string, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.last, l.budget, l.provider, l.seen
}
