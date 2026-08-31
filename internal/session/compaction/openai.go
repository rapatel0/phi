package compaction

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/util"
)

func openaiLike(cfg llm.ModelConfig) bool {
	u := strings.ToLower(cfg.BaseURL)
	if u == "" {
		return false
	}
	return strings.Contains(u, "openai.com") ||
		strings.Contains(u, "chatgpt.com") ||
		strings.Contains(u, "localhost") ||
		strings.Contains(u, "127.0.0.1")
}

func responsesBase(baseURL string) string {
	u := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u = strings.TrimSuffix(u, "/chat/completions")
	return u
}

func originAllowed(endpoint string, allowed []string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported scheme %s", parsed.Scheme)
	}
	host := parsed.Hostname()
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if parsed.Scheme == "http" && !loopback {
		return errors.New("non-loopback OpenAI Responses endpoint must use HTTPS")
	}
	origin := parsed.Scheme + "://" + parsed.Host
	if loopback {
		return nil
	}
	if slices.Contains(allowed, origin) {
		return nil
	}
	return fmt.Errorf("endpoint origin is not allowed: %s", origin)
}

type responsesPayload struct {
	OutputText string          `json:"output_text"`
	Output     json.RawMessage `json:"output"`
}

func textFromResponses(p responsesPayload) string {
	if strings.TrimSpace(p.OutputText) != "" {
		return p.OutputText
	}
	if len(p.Output) == 0 {
		return ""
	}
	var items []map[string]any
	if json.Unmarshal(p.Output, &items) != nil {
		return ""
	}
	var b strings.Builder
	for _, item := range items {
		content, _ := item["content"].([]any)
		for _, part := range content {
			block, _ := part.(map[string]any)
			if block["type"] == "output_text" {
				if t, ok := block["text"].(string); ok {
					if b.Len() > 0 {
						b.WriteByte('\n')
					}
					b.WriteString(t)
				}
			}
		}
	}
	return b.String()
}

func compactViaResponses(
	ctx context.Context,
	cfg llm.ModelConfig,
	prompt string,
	allowed []string,
	timeout time.Duration,
	maxOut int,
) (string, error) {
	if cfg.APIKey == "" || cfg.BaseURL == "" {
		return "", errors.New("openai compact: missing endpoint")
	}
	base := responsesBase(cfg.BaseURL)
	compactURL := base + "/responses/compact"
	if err := originAllowed(compactURL, allowed); err != nil {
		return "", err
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	httpClient := util.DefaultHTTPClient()
	compacted, err := postResponses(ctx, httpClient, compactURL, cfg, map[string]any{
		"model": cfg.Name,
		"input": []map[string]string{
			{"role": "developer", "content": "Preserve this context for a later continuation handoff."},
			{"role": "user", "content": prompt},
		},
	}, timeout)
	if err != nil {
		return "", err
	}
	if len(compacted.Output) == 0 || !json.Valid(compacted.Output) || string(compacted.Output) == "null" {
		return "", errors.New("openai compact: no output window")
	}
	var output any
	if err := json.Unmarshal(compacted.Output, &output); err != nil {
		return "", err
	}
	input := []any{}
	if arr, ok := output.([]any); ok {
		input = arr
	} else {
		return "", errors.New("openai compact: output window is not an array")
	}
	input = append(input, map[string]string{
		"role":    "user",
		"content": "Now produce the continuation handoff from the preserved context.",
	})
	respURL := base + "/responses"
	if err := originAllowed(respURL, allowed); err != nil {
		return "", err
	}
	body := map[string]any{
		"model":             cfg.Name,
		"input":             input,
		"max_output_tokens": maxOut,
		"store":             false,
	}
	done, err := postResponses(ctx, httpClient, respURL, cfg, body, timeout)
	if err != nil {
		return "", err
	}
	text := textFromResponses(done)
	if strings.TrimSpace(text) == "" {
		return "", errors.New("openai compact: empty handoff")
	}
	return text, nil
}

func postResponses(
	ctx context.Context,
	httpClient *http.Client,
	endpoint string,
	cfg llm.ModelConfig,
	body any,
	timeout time.Duration,
) (responsesPayload, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return responsesPayload{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return responsesPayload{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	resp, err := httpClient.Do(req)
	if err != nil {
		return responsesPayload{}, err
	}
	defer resp.Body.Close()
	slurp, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := string(slurp)
		if len(detail) > 500 {
			detail = detail[:500]
		}
		return responsesPayload{}, fmt.Errorf("openai %s failed (%d): %s", endpoint, resp.StatusCode, detail)
	}
	var payload responsesPayload
	if err := json.Unmarshal(slurp, &payload); err != nil {
		return responsesPayload{}, fmt.Errorf("openai %s: invalid JSON", endpoint)
	}
	return payload, nil
}
