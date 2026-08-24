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

	"github.com/pulseaiclub/phi/internal/tools/tooldef"
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
