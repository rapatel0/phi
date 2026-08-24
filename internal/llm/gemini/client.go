package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"net/url"
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/util"
)

const defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

type part struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *inlineData       `json:"inlineData,omitempty"`
	FunctionCall     *functionCall     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponse `json:"functionResponse,omitempty"`
}

type inlineData struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}

type functionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type functionResponse struct {
	Name     string `json:"name"`
	Response any    `json:"response"`
}

type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}

type functionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type generateRequest struct {
	SystemInstruction *content  `json:"system_instruction,omitempty"`
	Contents          []content `json:"contents"`
	Tools             []struct {
		FunctionDeclarations []functionDecl `json:"functionDeclarations"`
	} `json:"tools,omitempty"`
}

// IsProvider reports a Gemini model or Google AI Studio base URL.
func IsProvider(cfg llm.ModelConfig) bool {
	base := strings.ToLower(cfg.BaseURL)
	name := strings.ToLower(cfg.Name)
	if strings.Contains(base, "generativelanguage.googleapis.com") ||
		strings.Contains(base, "aiplatform.googleapis.com") {
		return true
	}
	return strings.HasPrefix(name, "gemini")
}

func normalizeBase(base string) string {
	if strings.TrimSpace(base) == "" {
		return defaultBaseURL
	}
	return strings.TrimRight(base, "/")
}

func BuildRequest(
	cfg llm.ModelConfig,
	system string,
	messages []llm.Message,
	tools []llm.ToolDefinition,
) generateRequest {
	req := generateRequest{}
	if strings.TrimSpace(system) != "" {
		req.SystemInstruction = &content{Parts: []part{{Text: system}}}
	}
	for _, m := range messages {
		switch m.Role {
		case llm.RoleAssistant:
			c := content{Role: "model"}
			if m.Content != "" {
				c.Parts = append(c.Parts, part{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				c.Parts = append(c.Parts, part{FunctionCall: &functionCall{Name: tc.Function.Name, Args: args}})
			}
			if len(c.Parts) == 0 {
				c.Parts = []part{{Text: ""}}
			}
			req.Contents = append(req.Contents, c)
		case llm.RoleTool:
			c := content{
				Role: "user",
				Parts: []part{{FunctionResponse: &functionResponse{
					Name:     toolNameFromCall(m),
					Response: map[string]any{"output": m.Content},
				}}},
			}
			if m.HasImages() {
				c.Parts = append(c.Parts, geminiUserParts(llm.Message{Images: m.Images})...)
			}
			req.Contents = append(req.Contents, c)
		default:
			req.Contents = append(req.Contents, content{Role: "user", Parts: geminiUserParts(m)})
		}
	}
	if len(tools) > 0 {
		decls := make([]functionDecl, 0, len(tools))
		for _, t := range tools {
			params, _ := json.Marshal(t.Params)
			if len(params) == 0 || bytes.Equal(bytes.TrimSpace(params), []byte("null")) {
				params = json.RawMessage(`{"type":"object"}`)
			}
			decls = append(decls, functionDecl{Name: t.Name, Description: t.Description, Parameters: params})
		}
		req.Tools = []struct {
			FunctionDeclarations []functionDecl `json:"functionDeclarations"`
		}{{FunctionDeclarations: decls}}
	}
	return req
}

func geminiUserParts(m llm.Message) []part {
	if !m.HasImages() {
		return []part{{Text: m.Content}}
	}
	var parts []part
	if m.Content != "" {
		parts = append(parts, part{Text: m.Content})
	}
	for _, img := range m.Images {
		mime := img.MIME
		if mime == "" {
			mime = "image/png"
		}
		parts = append(parts, part{InlineData: &inlineData{
			MIMEType: mime,
			Data:     base64.StdEncoding.EncodeToString(img.Data),
		}})
	}
	if len(parts) == 0 {
		return []part{{Text: ""}}
	}
	return parts
}

func toolNameFromCall(m llm.Message) string {
	if m.ToolCallID != "" {
		return m.ToolCallID
	}
	return "tool"
}

func streamURL(cfg llm.ModelConfig) string {
	base := normalizeBase(cfg.BaseURL)
	model := strings.TrimSpace(cfg.Name)
	if model == "" {
		model = "gemini-2.5-flash"
	}
	if !strings.Contains(model, "/") {
		model = "models/" + model
	}
	u, _ := url.Parse(base + "/" + model + ":streamGenerateContent")
	q := u.Query()
	q.Set("alt", "sse")
	if cfg.APIKey != "" && !strings.Contains(base, "aiplatform.googleapis.com") {
		q.Set("key", cfg.APIKey)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Stream POSTs streamGenerateContent and yields Alpha stream events.
func Stream(
	ctx context.Context,
	httpClient *http.Client,
	cfg llm.ModelConfig,
	req generateRequest,
) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		body, err := json.Marshal(req)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, streamURL(cfg), bytes.NewReader(body))
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if strings.Contains(strings.ToLower(cfg.BaseURL), "aiplatform.googleapis.com") && cfg.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
		}
		httpResp, err := util.DoWithRetry(httpClient, httpReq)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(httpResp.Body)
			yield(llm.StreamEvent{}, fmt.Errorf("gemini API error: (%d) %s", httpResp.StatusCode, string(raw)))
			return
		}
		processStream(httpResp.Body, yield)
	}
}

func processStream(body io.Reader, yield func(llm.StreamEvent, error) bool) {
	var (
		text      strings.Builder
		toolCalls []llm.ToolCall
		usage     llm.Usage
	)
	for data, parseErr := range util.ParseDataStream(body) {
		if parseErr != nil {
			yield(llm.StreamEvent{Type: llm.StreamEventTypeError, Err: parseErr.Error()}, parseErr)
			return
		}
		line := bytes.TrimSpace(data)
		if len(line) == 0 {
			continue
		}
		var chunk struct {
			Candidates []struct {
				Content struct {
					Parts []part `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			UsageMetadata struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
				TotalTokenCount      int `json:"totalTokenCount"`
			} `json:"usageMetadata"`
		}
		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}
		u := chunk.UsageMetadata
		if u.TotalTokenCount > 0 || u.PromptTokenCount > 0 {
			usage.PromptTokens = u.PromptTokenCount
			usage.CompletionTokens = u.CandidatesTokenCount
			usage.TotalTokens = u.TotalTokenCount
		}
		for _, cand := range chunk.Candidates {
			for _, p := range cand.Content.Parts {
				if p.Text != "" {
					text.WriteString(p.Text)
					if !yield(llm.StreamEvent{
						Type:  llm.StreamEventTypeDelta,
						Delta: llm.StreamDelta{Content: p.Text},
					}, nil) {
						return
					}
				}
				if p.FunctionCall != nil && p.FunctionCall.Name != "" {
					args, _ := json.Marshal(p.FunctionCall.Args)
					tc := llm.ToolCall{
						Index: len(toolCalls),
						ID:    p.FunctionCall.Name,
						Type:  "function",
						Function: llm.Function{
							Name:      p.FunctionCall.Name,
							Arguments: string(args),
						},
					}
					toolCalls = append(toolCalls, tc)
					if !yield(llm.StreamEvent{
						Type:  llm.StreamEventTypeDelta,
						Delta: llm.StreamDelta{ToolCalls: []llm.ToolCall{tc}},
					}, nil) {
						return
					}
				}
			}
		}
	}
	yield(llm.StreamEvent{
		Type: llm.StreamEventTypeDone,
		Partial: llm.Response{
			Choices: []llm.Choice{{
				Message: llm.Message{
					Role:      llm.RoleAssistant,
					Content:   text.String(),
					ToolCalls: toolCalls,
				},
			}},
			Usage: usage,
		},
	}, nil)
}

// Compact is a non-streaming generateContent call.
func Compact(ctx context.Context, httpClient *http.Client, cfg llm.ModelConfig, prompt string) (string, error) {
	req := BuildRequest(cfg, "", []llm.Message{{Role: llm.RoleUser, Content: prompt}}, nil)
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	base := normalizeBase(cfg.BaseURL)
	model := cfg.Name
	if model == "" {
		model = "gemini-2.5-flash"
	}
	if !strings.Contains(model, "/") {
		model = "models/" + model
	}
	u := base + "/" + model + ":generateContent"
	if cfg.APIKey != "" {
		u += "?key=" + url.QueryEscape(cfg.APIKey)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := util.DoWithRetry(httpClient, httpReq)
	if err != nil {
		return "", err
	}
	defer httpResp.Body.Close()
	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return "", err
	}
	if httpResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini API error: (%d) %s", httpResp.StatusCode, string(raw))
	}
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []part `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, c := range resp.Candidates {
		for _, p := range c.Content.Parts {
			b.WriteString(p.Text)
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("gemini API error: empty response")
	}
	return b.String(), nil
}
