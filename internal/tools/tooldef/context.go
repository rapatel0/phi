package tooldef

import (
	"context"
	"strings"

	"github.com/rapatel0/alpha/internal/llm"
)

type toolCallIDKey struct{}

type cwdKey struct{}

type modelKey struct{}

// WithToolCallID attaches the active tool_use id to ctx for UI correlation.
func WithToolCallID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, toolCallIDKey{}, id)
}

// ToolCallID returns the tool_use id from ctx, or empty.
func ToolCallID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(toolCallIDKey{}).(string)
	return v
}

// WithCwd attaches the session working directory used to resolve relative tool paths.
// Empty cwd is ignored so callers can pass through an unset session cwd.
func WithCwd(ctx context.Context, cwd string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ctx
	}
	return context.WithValue(ctx, cwdKey{}, cwd)
}

func cwdFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(cwdKey{}).(string)
	return strings.TrimSpace(v)
}

// WithModel attaches the active LLM connection so tools can pick a native backend.
func WithModel(ctx context.Context, cfg llm.ModelConfig) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, modelKey{}, cfg)
}

// Model returns the LLM connection from ctx, or a zero config.
func Model(ctx context.Context) llm.ModelConfig {
	if ctx == nil {
		return llm.ModelConfig{}
	}
	v, _ := ctx.Value(modelKey{}).(llm.ModelConfig)
	return v
}
