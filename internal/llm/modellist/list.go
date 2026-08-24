// Package modellist fetches chat-model IDs from a provider's models API.
package modellist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/pulseaiclub/phi/internal/auth"
	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/llm/gemini"
	"github.com/pulseaiclub/phi/internal/llm/openai"
	"github.com/pulseaiclub/phi/internal/util"
)

const (
	bodyLimit      = int64(4 << 20)
	anthropicVer   = "2023-06-01"
	oauthBeta      = "claude-code-20250219,oauth-2025-04-20,fine-grained-tool-streaming-2025-05-14"
	oauthUserAgent = "claude-cli/2.1.75"
)

// Disabled reports PHI_MODEL_LIST=0 (skip live fetches; use config/catalog).
func Disabled() bool {
	v := strings.TrimSpace(os.Getenv("PHI_MODEL_LIST"))
	return v == "0" || strings.EqualFold(v, "false")
}

type listPayload struct {
	Data   []listItem `json:"data"`
	Models []listItem `json:"models"`
}

type listItem struct {
	ID                         string   `json:"id"`
	Name                       string   `json:"name"`
	DisplayName                string   `json:"display_name"`
	DisplayNameCamel           string   `json:"displayName"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
}

// Fetch returns chat-capable model IDs for cfg's provider.
func Fetch(ctx context.Context, cfg llm.ModelConfig) ([]string, error) {
	if Disabled() {
		return nil, nil
	}
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.APIKey == "" {
		return nil, errors.New("modellist: missing api key")
	}
	endpoint, kind, err := endpoint(cfg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("modellist: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	setHeaders(req, cfg, kind)

	resp, err := listClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("modellist: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit+1))
	if err != nil {
		return nil, fmt.Errorf("modellist: read: %w", err)
	}
	if int64(len(body)) > bodyLimit {
		return nil, errors.New("modellist: response too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("modellist: %s", msg)
	}
	ids, err := parseIDs(body, kind)
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	return ids, nil
}

type providerKind int

const (
	kindOpenAI providerKind = iota
	kindAnthropic
	kindGemini
	kindCodex
)

func endpoint(cfg llm.ModelConfig) (string, providerKind, error) {
	switch {
	case gemini.IsProvider(cfg):
		base := strings.TrimRight(cfg.BaseURL, "/")
		if base == "" {
			base = "https://generativelanguage.googleapis.com/v1beta"
		}
		u, err := url.Parse(base + "/models")
		if err != nil {
			return "", 0, errors.New("modellist: bad gemini url")
		}
		if !strings.Contains(strings.ToLower(base), "aiplatform.googleapis.com") {
			q := u.Query()
			q.Set("key", cfg.APIKey)
			q.Set("pageSize", "100")
			u.RawQuery = q.Encode()
		}
		return u.String(), kindGemini, nil
	case isAnthropic(cfg):
		base := strings.TrimRight(cfg.BaseURL, "/")
		if base == "" {
			base = "https://api.anthropic.com"
		}
		if !strings.HasSuffix(base, "/v1") {
			base += "/v1"
		}
		return base + "/models", kindAnthropic, nil
	case openai.UseCodexBackend(cfg):
		base := strings.TrimRight(cfg.BaseURL, "/")
		if base == "" || strings.Contains(strings.ToLower(base), "api.openai.com") {
			base = auth.CodexBackendBaseURL
		}
		return strings.TrimRight(base, "/") + "/models", kindCodex, nil
	default:
		base := strings.TrimRight(cfg.BaseURL, "/")
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		if !strings.HasSuffix(base, "/models") {
			base += "/models"
		}
		return base, kindOpenAI, nil
	}
}

func isAnthropic(cfg llm.ModelConfig) bool {
	base := strings.ToLower(cfg.BaseURL)
	name := strings.ToLower(cfg.Name)
	return strings.Contains(base, "anthropic") || strings.HasPrefix(name, "claude")
}

func setHeaders(req *http.Request, cfg llm.ModelConfig, kind providerKind) {
	switch kind {
	case kindAnthropic:
		if auth.IsAnthropicOAuthToken(cfg.APIKey) {
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
			req.Header.Del("X-Api-Key")
			req.Header.Set("anthropic-beta", oauthBeta)
			req.Header.Set("User-Agent", oauthUserAgent)
			req.Header.Set("x-app", "cli")
		} else {
			req.Header.Set("X-Api-Key", cfg.APIKey)
		}
		req.Header.Set("Anthropic-Version", anthropicVer)
	case kindGemini:
		if strings.Contains(strings.ToLower(cfg.BaseURL), "aiplatform.googleapis.com") {
			req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}
	case kindCodex:
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		req.Header.Set("originator", "phi")
		if id := auth.ChatGPTAccountID(cfg.APIKey); id != "" {
			req.Header.Set("chatgpt-account-id", id)
		}
	default:
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
}

func parseIDs(body []byte, kind providerKind) ([]string, error) {
	var payload listPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("modellist: decode: %w", err)
	}
	items := append(payload.Data, payload.Models...)
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		if kind == kindGemini && len(it.SupportedGenerationMethods) > 0 &&
			!supportsGenerate(it.SupportedGenerationMethods) {
			continue
		}
		id := strings.TrimSpace(it.ID)
		if id == "" {
			id = strings.TrimSpace(it.Name)
		}
		if id == "" {
			id = strings.TrimSpace(it.DisplayName)
		}
		if id == "" {
			id = strings.TrimSpace(it.DisplayNameCamel)
		}
		id = strings.TrimPrefix(id, "models/")
		if id == "" || !keep(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func supportsGenerate(methods []string) bool {
	for _, m := range methods {
		switch strings.ToLower(m) {
		case "generatecontent", "streamgeneratecontent":
			return true
		}
	}
	return false
}

func keep(id string) bool {
	l := strings.ToLower(id)
	skip := []string{
		"embed", "whisper", "tts", "dall-e", "davinci", "babbage", "moderation",
		"imagine", "veo", "sora", "transcribe", "realtime", "aqa", "imagen",
		"computer-use",
	}
	for _, s := range skip {
		if strings.Contains(l, s) {
			return false
		}
	}
	return true
}

func listClient() *http.Client {
	c := *util.DefaultHTTPClient()
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		origin := via[0].URL
		if !strings.EqualFold(req.URL.Scheme, origin.Scheme) ||
			!strings.EqualFold(req.URL.Host, origin.Host) {
			return errors.New("modellist redirect changed origin")
		}
		return nil
	}
	return &c
}
