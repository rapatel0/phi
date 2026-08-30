package parentask

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolRequiresAQuestion(t *testing.T) {
	tool := Tool(func(context.Context, string) (string, error) {
		t.Fatal("ask must not run")
		return "", nil
	})
	_, err := tool.Run(t.Context(), json.RawMessage(`{}`))
	require.Error(t, err)
}

func TestToolCallsParent(t *testing.T) {
	tool := Tool(func(_ context.Context, q string) (string, error) {
		assert.Equal(t, "which API?", q)
		return "use REST", nil
	})
	got, err := tool.Run(t.Context(), json.RawMessage(`{"question":"which API?"}`))
	require.NoError(t, err)
	assert.Equal(t, "use REST", got.Content)
}
