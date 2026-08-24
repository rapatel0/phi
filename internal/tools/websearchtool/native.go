package websearchtool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/util"
)

const (
	nativeTimeout      = 45 * time.Second
	maxNativeBytes     = 1 << 20
	anthropicDefault   = "https://api.anthropic.com"
	geminiDefault      = "https://generativelanguage.googleapis.com/v1beta"
	openaiDefault      = "https://api.openai.com/v1"
	xaiDefault         = "https://api.x.ai/v1"
	anthropicVersion   = "2023-06-01"
	anthropicOAuthBeta = "claude-code-20250219,oauth-2025-04-20,fine-grained-tool-streaming-2025-05-14"
	anthropicOAuthUA   = "claude-cli/2.1.75"
)

// httpClient is swapped in tests (httptest).
var httpClient *http.Client

func client() *http.Client {
	if httpClient != nil {
		return httpClient
	}
	return util.DefaultHTTPClient()
}

func nativeBackend(cfg llm.ModelConfig) string {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return ""
	}
	base := strings.ToLower(cfg.BaseURL)
	name := strings.ToLower(cfg.Name)
	switch {
	case strings.Contains(base, "anthropic") || strings.HasPrefix(name, "claude"):
		return "anthropic"
	case strings.Contains(base, "generativelanguage.googleapis.com") ||
		strings.Contains(base, "aiplatform.googleapis.com") ||
		strings.HasPrefix(name, "gemini"):
		return "gemini"
	case strings.Contains(base, "x.ai") || strings.HasPrefix(name, "grok"):
		return "xai"
	case strings.Contains(base, "chatgpt.com") || strings.Contains(base, "backend-api/codex") ||
		auth.IsCodexOAuthToken(cfg.APIKey):
		return ""
	case strings.Contains(base, "api.openai.com") || openaiish(name, base):
		return "openai"
	default:
		return ""
	}
}

func openaiish(name, base string) bool {
	if base != "" && !strings.Contains(base, "openai.com") {
		return false
	}
	return strings.HasPrefix(name, "gpt-") ||
		strings.HasPrefix(name, "o1") ||
		strings.HasPrefix(name, "o3") ||
		strings.HasPrefix(name, "o4")
}

func nativeSearch(ctx context.Context, backend, query string, cfg llm.ModelConfig) (string, error) {
	switch backend {
	case "anthropic":
		return anthropicSearch(ctx, query, cfg)
	case "gemini":
		return geminiSearch(ctx, query, cfg)
	case "openai":
		return responsesSearch(ctx, query, cfg, openaiDefault)
	case "xai":
		return responsesSearch(ctx, query, cfg, xaiDefault)
	default:
		return "", fmt.Errorf("unknown backend %s", backend)
	}
}

func anthropicSearch(ctx context.Context, query string, cfg llm.ModelConfig) (string, error) {
	payload := map[string]any{
		"model":      cfg.Name,
		"max_tokens": 4096,
		"messages":   []map[string]string{{"role": "user", "content": query}},
		"tools":      []map[string]string{{"type": "web_search_20250305", "name": "web_search"}},
	}
	headers := map[string]string{"anthropic-version": anthropicVersion}
	if auth.IsAnthropicOAuthToken(cfg.APIKey) {
		headers["Authorization"] = "Bearer " + cfg.APIKey
		headers["anthropic-beta"] = anthropicOAuthBeta
		headers["User-Agent"] = anthropicOAuthUA
		headers["x-app"] = "cli"
	} else {
		headers["x-api-key"] = cfg.APIKey
	}
	body, err := postJSON(ctx, anthropicMessagesURL(cfg.BaseURL), headers, payload)
	if err != nil {
		return "", err
	}
	return parseAnthropicSearch(body)
}

func geminiSearch(ctx context.Context, query string, cfg llm.ModelConfig) (string, error) {
	payload := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": query}}},
		},
		"tools": []map[string]any{{"google_search": map[string]any{}}},
	}
	body, err := postJSON(ctx, geminiGenerateURL(cfg.BaseURL, cfg.Name, cfg.APIKey), nil, payload)
	if err != nil {
		return "", err
	}
	return parseGeminiSearch(body)
}

func responsesSearch(ctx context.Context, query string, cfg llm.ModelConfig, fallback string) (string, error) {
	payload := map[string]any{
		"model": cfg.Name,
		"tools": []map[string]string{{"type": "web_search"}},
		"input": query,
	}
	headers := map[string]string{"Authorization": "Bearer " + cfg.APIKey}
	body, err := postJSON(ctx, responsesURL(cfg.BaseURL, fallback), headers, payload)
	if err != nil {
		return "", err
	}
	return parseResponsesSearch(body)
}

func anthropicMessagesURL(base string) string {
	if strings.TrimSpace(base) == "" {
		base = anthropicDefault
	}
	b := strings.TrimRight(base, "/")
	switch {
	case strings.HasSuffix(b, "/messages"):
		return b
	case strings.HasSuffix(b, "/v1"):
		return b + "/messages"
	default:
		return b + "/v1/messages"
	}
}

func geminiGenerateURL(base, model, apiKey string) string {
	if strings.TrimSpace(base) == "" {
		base = geminiDefault
	}
	base = strings.TrimRight(base, "/")
	return fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		base, url.PathEscape(model), url.QueryEscape(apiKey))
}

func responsesURL(base, fallback string) string {
	if strings.TrimSpace(base) == "" {
		base = fallback
	}
	b := strings.TrimRight(base, "/")
	if strings.HasSuffix(b, "/responses") {
		return b
	}
	return b + "/responses"
}

func postJSON(ctx context.Context, rawURL string, headers map[string]string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, nativeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxNativeBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxNativeBytes {
		body = body[:maxNativeBytes]
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(body))
		runes := []rune(snippet)
		if len(runes) > 200 {
			snippet = string(runes[:200])
		}
		return nil, fmt.Errorf("http %s: %s", resp.Status, snippet)
	}
	return body, nil
}

func parseAnthropicSearch(body []byte) (string, error) {
	var data struct {
		Content []struct {
			Type      string `json:"type"`
			Text      string `json:"text"`
			Content   []src  `json:"content"`
			Citations []src  `json:"citations"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("anthropic search: %w", err)
	}
	var parts []string
	var sources []src
	seen := map[string]struct{}{}
	add := func(s src) {
		s.URL = strings.TrimSpace(s.URL)
		if s.URL == "" {
			return
		}
		if _, ok := seen[s.URL]; ok {
			return
		}
		seen[s.URL] = struct{}{}
		sources = append(sources, s)
	}
	for _, block := range data.Content {
		switch block.Type {
		case "text":
			if t := strings.TrimSpace(block.Text); t != "" {
				parts = append(parts, t)
			}
			for _, c := range block.Citations {
				add(c)
			}
		case "web_search_tool_result":
			for _, r := range block.Content {
				if r.Type == "web_search_result" || r.Type == "" {
					add(r)
				}
			}
		}
	}
	if len(sources) > 0 {
		parts = append(parts, formatSources(sources))
	}
	if len(parts) == 0 {
		return "No results found.", nil
	}
	return strings.Join(parts, "\n"), nil
}

type src struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func parseGeminiSearch(body []byte) (string, error) {
	var data struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
			GroundingMetadata struct {
				GroundingChunks []struct {
					Web struct {
						Title string `json:"title"`
						URI   string `json:"uri"`
					} `json:"web"`
				} `json:"groundingChunks"`
			} `json:"groundingMetadata"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("gemini search: %w", err)
	}
	if len(data.Candidates) == 0 {
		return "No results found.", nil
	}
	c := data.Candidates[0]
	var parts []string
	for _, p := range c.Content.Parts {
		if t := strings.TrimSpace(p.Text); t != "" {
			parts = append(parts, t)
		}
	}
	var sources []src
	for _, ch := range c.GroundingMetadata.GroundingChunks {
		if ch.Web.URI == "" {
			continue
		}
		sources = append(sources, src{Title: ch.Web.Title, URL: ch.Web.URI})
	}
	if len(sources) > 0 {
		parts = append(parts, formatSources(sources))
	}
	if len(parts) == 0 {
		return "No results found.", nil
	}
	return strings.Join(parts, "\n"), nil
}

func parseResponsesSearch(body []byte) (string, error) {
	var data struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Text        string `json:"text"`
				Annotations []struct {
					Type  string `json:"type"`
					Title string `json:"title"`
					URL   string `json:"url"`
				} `json:"annotations"`
			} `json:"content"`
		} `json:"output"`
		Citations []string `json:"citations"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("responses search: %w", err)
	}
	var parts []string
	var sources []src
	seen := map[string]struct{}{}
	for _, item := range data.Output {
		if item.Type != "message" {
			continue
		}
		for _, c := range item.Content {
			if t := strings.TrimSpace(c.Text); t != "" {
				parts = append(parts, t)
			}
			for _, a := range c.Annotations {
				if a.Type != "url_citation" || a.URL == "" {
					continue
				}
				if _, ok := seen[a.URL]; ok {
					continue
				}
				seen[a.URL] = struct{}{}
				sources = append(sources, src{Title: a.Title, URL: a.URL})
			}
		}
	}
	for _, c := range data.Citations {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		sources = append(sources, src{URL: c})
	}
	if len(sources) > 0 {
		parts = append(parts, formatSources(sources))
	}
	if len(parts) == 0 {
		return "No results found.", nil
	}
	return strings.Join(parts, "\n"), nil
}

func formatSources(sources []src) string {
	if len(sources) > maxResults {
		sources = sources[:maxResults]
	}
	var b strings.Builder
	b.WriteString("\n## Sources:")
	for _, s := range sources {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			title = s.URL
		}
		fmt.Fprintf(&b, "\n- [%s](%s)", title, s.URL)
	}
	return b.String()
}
