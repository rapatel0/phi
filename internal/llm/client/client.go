package client

import (
	"context"
	"iter"
	"net/http"
	"strings"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/llm/anthropic"
	"github.com/rapatel0/alpha/internal/llm/gemini"
	"github.com/rapatel0/alpha/internal/llm/openai"
	"github.com/rapatel0/alpha/internal/util"
)

// Client talks to the configured LLM endpoint: the OpenAI-compatible
// /chat/completions API by default, or the Anthropic Messages API when the
// config targets anthropic (see isAnthropicProvider).
type Client struct {
	httpClient *http.Client
	cfg        llm.ModelConfig
	tools      []llm.ToolDefinition
	system     string
	anthropic  bool
	gemini     bool
	authFile   string
}

// NewClient builds a streaming chat client.
func NewClient(cfg llm.ModelConfig, tools []llm.ToolDefinition, systemPrompt string) *Client {
	return newClient(cfg, tools, systemPrompt, "")
}

// NewClientWithAuth is NewClient plus OAuth token refresh from authFile
// (~/.alpha/auth.json) before each request.
func NewClientWithAuth(cfg llm.ModelConfig, tools []llm.ToolDefinition, systemPrompt, authFile string) *Client {
	return newClient(cfg, tools, systemPrompt, authFile)
}

func newClient(cfg llm.ModelConfig, tools []llm.ToolDefinition, systemPrompt, authFile string) *Client {
	return &Client{
		httpClient: util.DefaultHTTPClient(),
		cfg:        cfg,
		tools:      tools,
		system:     systemPrompt,
		anthropic:  isAnthropicProvider(cfg),
		gemini:     gemini.IsProvider(cfg),
		authFile:   authFile,
	}
}

// Stream runs a streaming chat completion over messages (+ optional system prompt / tools).
func (c *Client) Stream(ctx context.Context, messages []llm.Message) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		if err := c.refresh(ctx); err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		if c.anthropic {
			req := anthropic.BuildRequest(c.cfg, c.system, messages, c.tools)
			for ev, err := range anthropic.Stream(ctx, c.httpClient, c.cfg, &req) {
				if !yield(ev, err) {
					return
				}
			}
			return
		}
		if c.gemini {
			req := gemini.BuildRequest(c.cfg, c.system, messages, c.tools)
			for ev, err := range gemini.Stream(ctx, c.httpClient, c.cfg, req) {
				if !yield(ev, err) {
					return
				}
			}
			return
		}
		if openai.UseCodexBackend(c.cfg) {
			req := openai.BuildResponsesRequest(c.cfg, c.system, messages, c.tools)
			for ev, err := range openai.StreamCodex(ctx, c.httpClient, c.cfg, req) {
				if !yield(ev, err) {
					return
				}
			}
			return
		}
		req := openai.BuildRequest(c.cfg, c.system, messages, c.tools)
		for ev, err := range openai.StreamChatCompletion(ctx, c.httpClient, c.cfg.BaseURL, c.cfg.APIKey, req) {
			if !yield(ev, err) {
				return
			}
		}
	}
}

// Compact sends a single non-streaming chat request and returns the
// assistant text. It satisfies llm.Compactor for session compaction.
func (c *Client) Compact(ctx context.Context, prompt string) (string, error) {
	if err := c.refresh(ctx); err != nil {
		return "", err
	}
	if c.anthropic {
		return anthropic.Compact(ctx, c.httpClient, c.cfg, prompt)
	}
	if c.gemini {
		return gemini.Compact(ctx, c.httpClient, c.cfg, prompt)
	}
	return openai.Compact(ctx, c.httpClient, c.cfg, prompt)
}

func (c *Client) refresh(ctx context.Context) error {
	if c.authFile == "" {
		return nil
	}
	return auth.EnsureAccess(ctx, &c.cfg, c.authFile)
}

// isAnthropicProvider reports whether the config targets the Anthropic
// Messages API: either an anthropic base URL or a claude model name.
func isAnthropicProvider(cfg llm.ModelConfig) bool {
	base := strings.ToLower(cfg.BaseURL)
	if strings.Contains(base, "anthropic") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(cfg.Name), "claude")
}
