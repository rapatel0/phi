package footer

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"

	"github.com/rapatel0/alpha/internal/components"
)

// drawFooter renders the footer and returns its text.
func drawFooter(f *FooterChrome) string {
	const width = 80
	surf := f.Draw(components.DrawContext{
		Max:    components.Size{Width: width, Height: 1},
		Method: xui.WidthUnicode,
	}, width)

	var sb strings.Builder
	for _, cell := range surf.Buffer {
		if cell.Char == "" {
			sb.WriteByte(' ')
			continue
		}
		sb.WriteString(cell.Char)
	}
	return strings.TrimSpace(sb.String())
}

// The profile is always shown. Using the wrong account is a costly mistake,
// and an indicator that appears only sometimes cannot be told apart from a
// missing one.
func TestFooterShowsTheProfile(t *testing.T) {
	f := NewFooterChrome(components.Theme{}, 1000)
	f.SetProfile(func() string { return "work" })

	assert.Contains(t, drawFooter(f), "profile:work")
}

// The default profile is shown too, so "no indicator" never has to be
// interpreted.
func TestFooterShowsTheDefaultProfile(t *testing.T) {
	f := NewFooterChrome(components.Theme{}, 1000)
	f.SetProfile(func() string { return "default" })

	assert.Contains(t, drawFooter(f), "profile:default")
}

// Before the controller exists there is no profile to report, and printing a
// bare "profile:" would be worse than printing nothing.
func TestFooterOmitsAnEmptyProfile(t *testing.T) {
	f := NewFooterChrome(components.Theme{}, 1000)
	f.SetProfile(func() string { return "  " })

	assert.NotContains(t, drawFooter(f), "profile")
}

func TestFooterWithoutAProfileSource(t *testing.T) {
	f := NewFooterChrome(components.Theme{}, 1000)

	assert.NotContains(t, drawFooter(f), "profile")
}

// The profile sits beside the other footer bits rather than replacing them.
//
// The hook status is set because it is written before the profile: a bit added
// afterwards would survive an overwrite and prove nothing.
func TestFooterKeepsOtherBits(t *testing.T) {
	f := NewFooterChrome(components.Theme{}, 1000)
	f.SetHookStatus("hooks:3")
	f.SetProfile(func() string { return "work" })
	f.SetLiveJobs(func() int { return 2 })

	out := drawFooter(f)
	assert.Contains(t, out, "hooks:3", "an earlier bit must survive")
	assert.Contains(t, out, "profile:work")
	assert.Contains(t, out, "2 jobs", "a later bit must survive")
}
