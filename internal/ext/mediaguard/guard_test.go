package mediaguard

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/llm"
)

// img builds a message carrying one image of n bytes.
func img(name string, n int) llm.Message {
	return llm.Message{
		Role:    llm.RoleUser,
		Content: "look",
		Images:  []llm.Image{{MIME: "image/png", Filename: name, Data: make([]byte, n)}},
	}
}

// A request inside the budget must pass through untouched, including the same
// backing slice: the common case must cost nothing.
func TestApplyUnderBudgetIsUnchanged(t *testing.T) {
	in := []llm.Message{img("a.png", 10), img("b.png", 10)}
	out, d := Apply(in, Budget{MaxBytes: 1000, MaxImages: 10})

	assert.False(t, d.Applied())
	assert.Equal(t, 2, d.ImagesKept)
	assert.Equal(t, 20, d.BytesBefore)
	require.Len(t, out, 2)
	assert.Len(t, out[0].Images, 1)
}

// Messages with no images are the normal case and must not be copied or
// annotated.
func TestApplyWithoutImages(t *testing.T) {
	in := []llm.Message{{Role: llm.RoleUser, Content: "hello"}}
	out, d := Apply(in, DefaultBudget())

	assert.Zero(t, d.ImagesBefore)
	assert.False(t, d.Applied())
	assert.Equal(t, "hello", out[0].Content)
}

// Over the byte budget the newest image survives, because the current turn is
// usually about the most recent media.
func TestApplyKeepsNewestWithinByteBudget(t *testing.T) {
	in := []llm.Message{img("old.png", 100), img("new.png", 100)}
	out, d := Apply(in, Budget{MaxBytes: 150, MaxImages: 10})

	require.True(t, d.Applied())
	assert.Equal(t, 1, d.ImagesKept)
	assert.Empty(t, out[0].Images, "the older image is dropped first")
	require.Len(t, out[1].Images, 1)
	assert.Equal(t, "new.png", out[1].Images[0].Filename)
}

// The image count is a separate limit: many small images can still be too many.
func TestApplyEnforcesImageCount(t *testing.T) {
	in := []llm.Message{img("a.png", 1), img("b.png", 1), img("c.png", 1)}
	out, d := Apply(in, Budget{MaxBytes: 1 << 20, MaxImages: 2})

	require.True(t, d.Applied())
	assert.Equal(t, 2, d.ImagesKept)
	assert.Empty(t, out[0].Images)
	assert.Len(t, out[2].Images, 1, "the newest is kept")
}

// A dropped image must leave a factual note, so the model knows something was
// there rather than seeing a silent gap.
func TestApplyLeavesEvidenceNote(t *testing.T) {
	in := []llm.Message{img("diagram.png", 100), img("new.png", 10)}
	out, _ := Apply(in, Budget{MaxBytes: 50, MaxImages: 10})

	assert.Contains(t, out[0].Content, "diagram.png")
	assert.Contains(t, out[0].Content, "image/png")
	assert.Contains(t, out[0].Content, "media omitted")
	assert.Contains(t, out[0].Content, "look", "the original text must survive")
}

// The caller's slice must not be mutated: the session keeps the full media.
func TestApplyDoesNotMutateInput(t *testing.T) {
	in := []llm.Message{img("old.png", 100), img("new.png", 10)}
	_, d := Apply(in, Budget{MaxBytes: 50, MaxImages: 10})

	require.True(t, d.Applied())
	require.Len(t, in[0].Images, 1, "the input message must still hold its image")
	assert.Equal(t, "look", in[0].Content, "the input text must not gain a note")
}

// A single image larger than the whole budget cannot be sent. Dropping it with
// a note beats failing the request with an opaque provider error.
func TestApplyOversizedSingleImage(t *testing.T) {
	in := []llm.Message{img("huge.png", 1000)}
	out, d := Apply(in, Budget{MaxBytes: 100, MaxImages: 10})

	assert.True(t, d.Applied())
	assert.Zero(t, d.ImagesKept)
	assert.Empty(t, out[0].Images)
	assert.Contains(t, out[0].Content, "huge.png")
}

// A zero budget means "unset", not "drop everything".
func TestApplyZeroBudgetUsesDefaults(t *testing.T) {
	in := []llm.Message{img("a.png", 10)}
	_, d := Apply(in, Budget{})
	assert.False(t, d.Applied(), "an unset budget must fall back to the defaults")
}

// An image with no filename still needs a usable note.
func TestDescribeUnnamedImage(t *testing.T) {
	got := describe(llm.Image{Data: make([]byte, 2048)})
	assert.Contains(t, got, "image")
	assert.Contains(t, got, "unknown type")
	assert.Contains(t, got, "2KB")
}

func TestSummaryReadsNaturally(t *testing.T) {
	assert.Equal(t, "no media", Decision{}.Summary())
	assert.Equal(t, "2 img / 1.0MB",
		Decision{ImagesBefore: 2, ImagesKept: 2, BytesKept: 1 << 20}.Summary())
	assert.Contains(t,
		Decision{ImagesBefore: 5, ImagesKept: 2, BytesKept: 1 << 20}.Summary(),
		"2 of 5 img")
}

func TestHumanBytes(t *testing.T) {
	assert.Equal(t, "512B", humanBytes(512))
	assert.Equal(t, "2KB", humanBytes(2048))
	assert.Equal(t, "1.5MB", humanBytes(3<<19))
}

// Multiple drops in one message must produce one readable note, not several
// stacked brackets.
func TestApplyJoinsMultipleNotes(t *testing.T) {
	in := []llm.Message{{
		Role:    llm.RoleUser,
		Content: "two",
		Images: []llm.Image{
			{MIME: "image/png", Filename: "a.png", Data: make([]byte, 100)},
			{MIME: "image/png", Filename: "b.png", Data: make([]byte, 100)},
		},
	}}
	out, _ := Apply(in, Budget{MaxBytes: 10, MaxImages: 10})

	assert.Equal(t, 1, strings.Count(out[0].Content, "media omitted"),
		"two drops must share one note, not stack brackets")
	assert.Contains(t, out[0].Content, "a.png")
	assert.Contains(t, out[0].Content, "b.png")
	assert.Contains(t, out[0].Content, "; ", "the notes must be joined into one list")
}
