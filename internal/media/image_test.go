package media

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/pulseaiclub/phi/internal/llm"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 10, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDetectMIME(t *testing.T) {
	data := pngBytes(t, 2, 2)
	if got := DetectMIME(data); got != "image/png" {
		t.Fatalf("got %q", got)
	}
	if DetectMIME([]byte("hello")) != "" {
		t.Fatal("expected empty")
	}
}

func TestNormalizePNG(t *testing.T) {
	img, err := Normalize(llm.Image{Data: pngBytes(t, 8, 8), Filename: "shot.png"})
	if err != nil {
		t.Fatal(err)
	}
	if img.MIME != "image/png" {
		t.Fatalf("mime %q", img.MIME)
	}
	if DetectMIME(img.Data) != "image/png" {
		t.Fatal("roundtrip sniff")
	}
}

func TestImagePaths(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	if err := os.WriteFile(p, pngBytes(t, 2, 2), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ImagePaths(p)
	if len(got) != 1 || got[0] != p {
		t.Fatalf("got %v", got)
	}
	if ImagePaths("please look at "+p) != nil {
		t.Fatal("prose must not parse as paths")
	}
	quoted := `"` + p + `"`
	if got := ImagePaths(quoted); len(got) != 1 {
		t.Fatalf("quoted: %v", got)
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.png")
	if err := os.WriteFile(p, pngBytes(t, 4, 4), 0o644); err != nil {
		t.Fatal(err)
	}
	img, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if img.Filename != "x.png" {
		t.Fatalf("name %q", img.Filename)
	}
}
