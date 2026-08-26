package readimagetool

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/tools/tooldef"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 200, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestReadImageLocal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	require.NoError(t, os.WriteFile(path, pngBytes(t), 0o644))

	raw, err := json.Marshal(inputArgs{FilePath: path})
	require.NoError(t, err)
	out, err := runReadImage(t.Context(), raw)
	require.NoError(t, err)
	require.Len(t, out.Images, 1)
	assert.Equal(t, "image/png", out.Images[0].MIME)
	var body resultBody
	require.NoError(t, json.Unmarshal([]byte(out.Content), &body))
	assert.Equal(t, "image/png", body.MIMEType)
	assert.Equal(t, 4, body.Width)
	assert.Equal(t, 4, body.Height)
}

func TestReadImageRejectsHTTP(t *testing.T) {
	raw, _ := json.Marshal(inputArgs{FilePath: "http://example.com/a.png"})
	_, err := runReadImage(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
}

func TestReadImageRejectsLoopback(t *testing.T) {
	raw, _ := json.Marshal(inputArgs{FilePath: "https://127.0.0.1/a.png"})
	_, err := runReadImage(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loopback")
}

func TestReadImageHTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes(t))
	}))
	t.Cleanup(srv.Close)

	host, _, _ := strings.Cut(strings.TrimPrefix(srv.URL, "https://"), ":")
	prevHosts, prevClient := allowPrivateHosts, fetchClientOverride
	allowPrivateHosts = []string{host, "127.0.0.1", "::1"}
	fetchClientOverride = srv.Client()
	t.Cleanup(func() {
		allowPrivateHosts = prevHosts
		fetchClientOverride = prevClient
	})

	raw, err := json.Marshal(inputArgs{FilePath: srv.URL + "/shot.png"})
	require.NoError(t, err)
	out, err := runReadImage(t.Context(), raw)
	require.NoError(t, err)
	require.Len(t, out.Images, 1)
	var body resultBody
	require.NoError(t, json.Unmarshal([]byte(out.Content), &body))
	assert.Equal(t, srv.URL+"/shot.png", body.SourceURL)
}

func TestReadImageToolSchema(t *testing.T) {
	tl := ReadImageTool()
	assert.Equal(t, "read_image", tl.Definition.Name)
	assert.True(t, tl.Definition.Readable)
}

func TestWithCwdRelative(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.png"), pngBytes(t), 0o644))
	raw, _ := json.Marshal(inputArgs{FilePath: "a.png"})
	out, err := runReadImage(tooldef.WithCwd(t.Context(), dir), raw)
	require.NoError(t, err)
	require.Len(t, out.Images, 1)
}

// quadrants writes an image whose four corners differ, so a region read can be
// verified by the color it returns.
func quadrants(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{0, 0, 255, 255}
			if x < w/2 && y < h/2 {
				c = color.RGBA{255, 0, 0, 255}
			} else if x >= w/2 && y >= h/2 {
				c = color.RGBA{0, 255, 0, 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	path := filepath.Join(t.TempDir(), "shot.png")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	return path
}

func callTool(t *testing.T, args map[string]any) (resultBody, []byte) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)

	res, err := ReadImageTool().Run(t.Context(), raw)
	require.NoError(t, err)
	require.Len(t, res.Images, 1, "the model must actually receive an image")

	var body resultBody
	require.NoError(t, json.Unmarshal([]byte(res.Content), &body))
	return body, res.Images[0].Data
}

func centerColor(t *testing.T, data []byte) color.RGBA {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	b := img.Bounds()
	r, g, bl, a := img.At(b.Min.X+b.Dx()/2, b.Min.Y+b.Dy()/2).RGBA()
	return color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8), uint8(a >> 8)}
}

// Reading a region must return that part of the image, not the whole thing.
func TestRegionReadsRequestedArea(t *testing.T) {
	path := quadrants(t, 1000, 1000)

	body, data := callTool(t, map[string]any{
		"file_path": path,
		"region":    map[string]int{"x": 0, "y": 0, "w": 400, "h": 400},
	})
	assert.Equal(t, color.RGBA{255, 0, 0, 255}, centerColor(t, data), "top-left is red")
	require.NotNil(t, body.Region)
	assert.Equal(t, 1000, body.FullWidth, "the model must know the full size to aim the next read")
	assert.Equal(t, 1000, body.FullHeight)

	_, data = callTool(t, map[string]any{
		"file_path": path,
		"region":    map[string]int{"x": 600, "y": 600, "w": 300, "h": 300},
	})
	assert.Equal(t, color.RGBA{0, 255, 0, 255}, centerColor(t, data), "bottom-right is green")
}

// Omitting region must keep the previous whole-image behavior.
func TestWithoutRegionReadsWholeImage(t *testing.T) {
	path := quadrants(t, 600, 400)
	body, _ := callTool(t, map[string]any{"file_path": path})
	assert.Nil(t, body.Region)
	assert.Zero(t, body.FullWidth, "full size is only reported for a region read")
	assert.Equal(t, 600, body.Width)
	assert.Equal(t, 400, body.Height)
}

// A small region must come back enlarged: that is what makes detail readable.
func TestRegionEnlargesSmallAreas(t *testing.T) {
	path := quadrants(t, 2000, 2000)
	body, _ := callTool(t, map[string]any{
		"file_path": path,
		"region":    map[string]int{"x": 10, "y": 10, "w": 80, "h": 80},
	})
	assert.Greater(t, body.Width, 80, "a small region must be scaled up")
}

// A region past the edge must fail with the image size, so the model can retry.
func TestRegionOutsideImageExplains(t *testing.T) {
	path := quadrants(t, 500, 500)
	raw, err := json.Marshal(map[string]any{
		"file_path": path,
		"region":    map[string]int{"x": 9000, "y": 9000, "w": 100, "h": 100},
	})
	require.NoError(t, err)

	_, err = ReadImageTool().Run(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500x500", "the error must state the real size")
}

// A partly out-of-bounds region is clamped rather than refused.
func TestRegionClampsToEdge(t *testing.T) {
	path := quadrants(t, 800, 800)
	body, _ := callTool(t, map[string]any{
		"file_path": path,
		"region":    map[string]int{"x": 700, "y": 700, "w": 400, "h": 400},
	})
	assert.Positive(t, body.Width)
	assert.LessOrEqual(t, body.Width, 800)
}

func TestRegionRejectsZeroSize(t *testing.T) {
	path := quadrants(t, 400, 400)
	raw, err := json.Marshal(map[string]any{
		"file_path": path,
		"region":    map[string]int{"x": 0, "y": 0, "w": 0, "h": 100},
	})
	require.NoError(t, err)
	_, err = ReadImageTool().Run(t.Context(), raw)
	require.Error(t, err)
}

// The tool schema must advertise region, or the model will never use it.
func TestSchemaAdvertisesRegion(t *testing.T) {
	props := ReadImageTool().Definition.Params.Properties
	require.Contains(t, props, "region")
	assert.NotContains(t, ReadImageTool().Definition.Params.Required, "region",
		"region must stay optional")
}

// The call detail must show the region, so the transcript says what was read.
func TestDetailShowsRegion(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"file_path": "/tmp/a.png",
		"region":    map[string]int{"x": 10, "y": 20, "w": 30, "h": 40},
	})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/a.png [10,20 30x40]", ReadImageTool().DetailFromArgs(raw))

	raw, err = json.Marshal(map[string]any{"file_path": "/tmp/a.png"})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/a.png", ReadImageTool().DetailFromArgs(raw))
}
