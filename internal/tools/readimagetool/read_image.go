package readimagetool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/media"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
)

const maxRawBytes = 20 << 20 // 20 MB before compress; stolen from pi-go

var toolDescription = `Read an image and make it visible to the model (vision).

Use this after a screenshot is saved to disk, or to inspect a local image / https URL so you can actually see it.

Required: file_path — absolute or cwd-relative path, or an https:// URL.

URL fetching is TLS-only (https) and blocks loopback / private / link-local / multicast hosts.`

// ReadImageTool returns the vision ingest tool (pi-go read_image).
func ReadImageTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "read_image",
			Description: toolDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"file_path": llm.Object{
						"type":        "string",
						"description": "Local path or https:// URL of the image",
					},
				},
				Required: []string{"file_path"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in inputArgs
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.FilePath)
		},
		Run: runReadImage,
	}
}

type inputArgs struct {
	FilePath string `json:"file_path"`
}

type resultBody struct {
	Path      string `json:"path"`
	MIMEType  string `json:"mime_type"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Size      int    `json:"size"`
	SourceURL string `json:"source_url,omitempty"`
}

func runReadImage(ctx context.Context, raw json.RawMessage) (tooldef.Result, error) {
	var in inputArgs
	if err := json.Unmarshal(raw, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse read_image arguments: %w", err)
	}
	path := strings.TrimSpace(in.FilePath)
	if path == "" {
		return tooldef.Result{}, fmt.Errorf("file_path is required")
	}

	sourceURL := ""
	if isHTTPURL(path) {
		cached, err := fetchImageToCache(ctx, path)
		if err != nil {
			return tooldef.Result{}, fmt.Errorf("fetching image: %w", err)
		}
		sourceURL = path
		path = cached
	} else {
		resolved, err := tooldef.ResolveToCwd(ctx, path)
		if err != nil {
			return tooldef.Result{}, err
		}
		path = resolved
	}

	st, err := os.Stat(path)
	if err != nil {
		return tooldef.Result{}, err
	}
	if st.IsDir() {
		return tooldef.Result{}, fmt.Errorf("%s is a directory", path)
	}
	if st.Size() > maxRawBytes {
		return tooldef.Result{}, fmt.Errorf("image too large: %d bytes exceeds %d byte limit", st.Size(), maxRawBytes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return tooldef.Result{}, err
	}
	img, err := media.Normalize(llm.Image{
		Data:     data,
		Filename: displayName(path, sourceURL),
	})
	if err != nil {
		return tooldef.Result{}, err
	}

	w, h := decodeSize(img.Data)
	display := path
	if sourceURL == "" {
		display = tooldef.RelToCwd(ctx, path)
	}
	body := resultBody{
		Path:      display,
		MIMEType:  img.MIME,
		Width:     w,
		Height:    h,
		Size:      len(img.Data),
		SourceURL: sourceURL,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return tooldef.Result{}, err
	}
	out := string(encoded)
	return tooldef.Result{
		Content: out,
		Detail:  display,
		Output:  out,
		Images:  []llm.Image{img},
	}, nil
}

func displayName(path, sourceURL string) string {
	if sourceURL != "" {
		return "url-image" + filepath.Ext(path)
	}
	return filepath.Base(path)
}

func decodeSize(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}
