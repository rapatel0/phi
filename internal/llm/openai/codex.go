package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"

	"github.com/pulseaiclub/phi/internal/auth"
	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/util"
)

const codexResponsesPath = "/responses"

// UseCodexBackend reports whether this config should hit ChatGPT's Codex
// responses API instead of platform /v1/chat/completions.
func UseCodexBackend(cfg llm.ModelConfig) bool {
	if !auth.IsCodexOAuthToken(cfg.APIKey) {
		return false
	}
	base := strings.ToLower(cfg.BaseURL)
	return base == "" || strings.Contains(base, "chatgpt.com") || strings.Contains(base, "backend-api/codex") ||
		strings.Contains(base, "api.openai.com")
}

type responsesRequest struct {
	Model        string          `json:"model"`
	Instructions string          `json:"instructions,omitempty"`
	Input        []responsesItem `json:"input"`
	Tools        []responsesTool `json:"tools,omitempty"`
	Stream       bool            `json:"stream"`
	Store        bool            `json:"store"`
}

type responsesItem struct {
	Role    string `json:"role,omitempty"`
	Content any    `json:"content,omitempty"`
	Type    string `json:"type,omitempty"`
	Name    string `json:"name,omitempty"`
	CallID  string `json:"call_id,omitempty"`
	Output  string `json:"output,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// BuildResponsesRequest converts Phi messages into the Codex Responses body.
func BuildResponsesRequest(
	cfg llm.ModelConfig,
	system string,
	messages []llm.Message,
	tools []llm.ToolDefinition,
) responsesRequest {
	req := responsesRequest{
		Model:        cfg.Name,
		Instructions: system,
		Stream:       true,
		Store:        false,
	}
	for _, m := range messages {
		switch m.Role {
		case llm.RoleAssistant:
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					req.Input = append(req.Input, responsesItem{
						Type:   "function_call",
						Name:   tc.Function.Name,
						CallID: tc.ID,
						Output: tc.Function.Arguments,
					})
				}
				if m.Content != "" {
					req.Input = append(req.Input, responsesItem{
						Role:    "assistant",
						Content: m.Content,
					})
				}
				continue
			}
			req.Input = append(req.Input, responsesItem{Role: "assistant", Content: m.Content})
		case llm.RoleTool:
			req.Input = append(req.Input, responsesItem{
				Type:   "function_call_output",
				CallID: m.ToolCallID,
				Output: m.Content,
			})
			if m.HasImages() {
				req.Input = append(req.Input, responsesItem{
					Role:    "user",
					Content: responsesUserContent(llm.Message{Images: m.Images}),
				})
			}
		default:
			req.Input = append(req.Input, responsesItem{Role: "user", Content: responsesUserContent(m)})
		}
	}
	for _, t := range tools {
		params, _ := json.Marshal(t.Params)
		if len(params) == 0 || bytes.Equal(bytes.TrimSpace(params), []byte("null")) {
			params = json.RawMessage("{}")
		}
		req.Tools = append(req.Tools, responsesTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		})
	}
	return req
}

func setCodexHeaders(req *http.Request, apiKey string) {
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", util.ContentEventStream)
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("originator", "phi")
	if id := auth.ChatGPTAccountID(apiKey); id != "" {
		req.Header.Set("chatgpt-account-id", id)
	}
}

func StreamCodex(
	ctx context.Context,
	httpClient *http.Client,
	cfg llm.ModelConfig,
	payload responsesRequest,
) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		body, err := json.Marshal(payload)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		base := strings.TrimRight(cfg.BaseURL, "/")
		if base == "" || strings.Contains(base, "api.openai.com") {
			base = auth.CodexBackendBaseURL
		}
		url := base
		if !strings.HasSuffix(url, codexResponsesPath) {
			url += codexResponsesPath
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		setCodexHeaders(httpReq, cfg.APIKey)
		httpResp, err := util.DoWithRetry(httpClient, httpReq)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(httpResp.Body)
			yield(llm.StreamEvent{}, fmt.Errorf("codex API error: (%d) %s", httpResp.StatusCode, string(respBody)))
			return
		}
		processCodexStream(httpResp.Body, yield)
	}
}

func processCodexStream(body io.Reader, yield func(llm.StreamEvent, error) bool) {
	var (
		content   strings.Builder
		toolCalls []llm.ToolCall
		current   *llm.ToolCall
		usage     llm.Usage
	)
	for data, parseErr := range util.ParseDataStream(body) {
		if parseErr != nil {
			yield(llm.StreamEvent{Type: llm.StreamEventTypeError, Err: parseErr.Error()}, parseErr)
			return
		}
		line := bytes.TrimSpace(data)
		if len(line) == 0 || bytes.Equal(line, []byte("[DONE]")) {
			continue
		}
		var envelope struct {
			Type  string          `json:"type"`
			Delta json.RawMessage `json:"delta"`
			Item  json.RawMessage `json:"item"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			continue
		}
		switch envelope.Type {
		case "response.output_text.delta":
			var delta struct {
				Delta string `json:"delta"`
			}
			text := ""
			if json.Unmarshal(envelope.Delta, &delta) == nil && delta.Delta != "" {
				text = delta.Delta
			} else {
				_ = json.Unmarshal(envelope.Delta, &text)
			}
			if text == "" {
				var wrap struct {
					Delta string `json:"delta"`
				}
				if json.Unmarshal(line, &wrap) == nil {
					text = wrap.Delta
				}
			}
			if text == "" {
				continue
			}
			content.WriteString(text)
			if !yield(llm.StreamEvent{
				Type:  llm.StreamEventTypeDelta,
				Delta: llm.StreamDelta{Content: text},
			}, nil) {
				return
			}
		case "response.output_item.added":
			var item struct {
				Type   string `json:"type"`
				ID     string `json:"id"`
				Name   string `json:"name"`
				CallID string `json:"call_id"`
			}
			if json.Unmarshal(envelope.Item, &item) != nil {
				continue
			}
			if item.Type == "function_call" {
				id := item.CallID
				if id == "" {
					id = item.ID
				}
				current = &llm.ToolCall{
					Index: len(toolCalls),
					ID:    id,
					Type:  "function",
					Function: llm.Function{
						Name: item.Name,
					},
				}
			}
		case "response.function_call_arguments.delta":
			if current == nil {
				continue
			}
			var delta struct {
				Delta string `json:"delta"`
			}
			if json.Unmarshal(line, &delta) != nil {
				_ = json.Unmarshal(envelope.Delta, &delta.Delta)
			}
			current.Function.Arguments += delta.Delta
			if !yield(llm.StreamEvent{
				Type:  llm.StreamEventTypeDelta,
				Delta: llm.StreamDelta{ToolCalls: []llm.ToolCall{*current}},
			}, nil) {
				return
			}
		case "response.output_item.done":
			if current != nil {
				toolCalls = append(toolCalls, *current)
				current = nil
			}
		case "response.completed":
			out := llm.Response{
				Choices: []llm.Choice{{
					Message: llm.Message{
						Role:      llm.RoleAssistant,
						Content:   content.String(),
						ToolCalls: toolCalls,
					},
				}},
				Usage: usage,
			}
			yield(llm.StreamEvent{Type: llm.StreamEventTypeDone, Partial: out}, nil)
			return
		}
	}
	out := llm.Response{
		Choices: []llm.Choice{{
			Message: llm.Message{
				Role:      llm.RoleAssistant,
				Content:   content.String(),
				ToolCalls: toolCalls,
			},
		}},
		Usage: usage,
	}
	yield(llm.StreamEvent{Type: llm.StreamEventTypeDone, Partial: out}, nil)
}
