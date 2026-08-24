package readtool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunRead_ImagePointsAtReadImage(t *testing.T) {
	root := t.TempDir()
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0, 0, 0, 0, 0, 0, 0, 0,
	}
	path := filepath.Join(root, "shot.png")
	require.NoError(t, os.WriteFile(path, png, 0o644))
	t.Chdir(root)

	raw, err := json.Marshal(readInput{Path: "shot.png"})
	require.NoError(t, err)
	out, err := runRead(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "read_image")
	assert.NotContains(t, out.Content, "@file")
}

func TestRunRead_RelativeHeader(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n"), 0o644))
	t.Chdir(root)

	raw, err := json.Marshal(readInput{Path: "src/main.go"})
	require.NoError(t, err)
	out, err := runRead(t.Context(), raw)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out.Content, "@file src/main.go#"))
	assert.Equal(t, "src/main.go", out.Detail)
}
