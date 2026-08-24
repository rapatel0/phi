package todo

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/ext"
)

func TestTodoWrite(t *testing.T) {
	reset()
	h := ext.NewHost()
	require.NoError(t, Plugin{}.Register(h))
	tools := h.Tools()
	require.Len(t, tools, 1)
	raw, _ := json.Marshal(map[string]any{
		"todos": []item{{ID: "1", Text: "a", Status: "pending"}, {ID: "2", Text: "b", Status: "completed"}},
	})
	res, err := tools[0].Run(t.Context(), raw)
	require.NoError(t, err)
	require.Contains(t, res.Detail, "1/2")
	got := Snapshot()
	require.Len(t, got, 2)
	bits := h.FooterBits()
	require.Equal(t, []string{"1/2 todos"}, bits)
}
