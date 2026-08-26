package mediaguard

import (
	"encoding/base64"
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

// Every provider states its limit on the base64 payload, which is 4/3 the raw
// size. A budget that ignores that overshoots by a third: the shipped default
// was once 12 MiB raw, or 16 MiB on the wire, against a 20 MB request cap.
func TestBudgetsStayUnderDocumentedEncodedLimits(t *testing.T) {
	// Documented request-level limits, in decimal MB as the providers state
	// them. Sources are recorded in doc/media-limits.md.
	limits := map[string]float64{
		"anthropic": 32,  // 32 MB per request
		"openai":    512, // 512 MB total payload
		"gemini":    20,  // 20 MB for the whole inline request
		"xai":       20,  // 20 MiB per image
	}
	for provider, mb := range limits {
		b := BudgetFor(provider)
		encoded := float64(b.EncodedBytes()) / 1e6
		assert.Less(t, encoded, mb,
			"%s: %.1f MB encoded must stay under the documented %.0f MB", provider, encoded, mb)
	}
}

// Gemini's 20 MB covers the whole request, not just images, so the image share
// must leave room for the prompt, tool schemas, and the rest of the turn.
func TestGeminiBudgetLeavesRoomForTheRequest(t *testing.T) {
	encoded := float64(BudgetFor("gemini").EncodedBytes()) / 1e6
	assert.Less(t, encoded, 10.0, "images must not claim more than half of Gemini's 20 MB")
}

// EncodedBytes must report the wire size, which is what a provider limit
// applies to.
func TestEncodedBytesMatchesBase64(t *testing.T) {
	b := Budget{MaxBytes: 3 << 20}
	assert.Equal(t, base64.StdEncoding.EncodedLen(3<<20), b.EncodedBytes(),
		"the reported encoded size must match what the encoder produces")
}

// An unknown provider must get the conservative default, not the largest
// budget: guessing high is what produces an opaque provider rejection.
func TestUnknownProviderGetsTheDefault(t *testing.T) {
	assert.Equal(t, DefaultBudget(), BudgetFor(""))
	assert.Equal(t, DefaultBudget(), BudgetFor("some-new-provider"))

	def := DefaultBudget().MaxBytes
	for _, p := range []string{"anthropic", "openai", "codex", "xai"} {
		assert.GreaterOrEqual(t, BudgetFor(p).MaxBytes, def,
			"%s should not be tighter than the unknown-provider fallback", p)
	}
}

func TestBudgetForKnownProviders(t *testing.T) {
	for _, p := range []string{"anthropic", "openai", "codex", "gemini", "xai"} {
		b := BudgetFor(p)
		assert.Positive(t, b.MaxBytes, "%s must have a byte budget", p)
		assert.Positive(t, b.MaxImages, "%s must have an image budget", p)
	}
}

// Anthropic applies a stricter per-image dimension limit above 20 images, so
// the budget must not exceed that threshold.
func TestAnthropicImageCountStaysUnderTheStricterTier(t *testing.T) {
	assert.LessOrEqual(t, BudgetFor("anthropic").MaxImages, 20)
}

// xAI is served over the OpenAI-compatible path, so it is the case most likely
// to be mislabeled. Its documented per-image limit is 20 MiB, well under
// OpenAI's, and applying OpenAI's budget to it would overshoot.
func TestXAIBudgetIsTighterThanOpenAI(t *testing.T) {
	assert.Less(t, BudgetFor("xai").MaxBytes, BudgetFor("openai").MaxBytes)
	encoded := float64(BudgetFor("xai").EncodedBytes())
	assert.Less(t, encoded, 20*1024*1024.0, "must stay under the documented 20 MiB")
}
