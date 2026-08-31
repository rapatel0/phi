package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/rapatel0/alpha/internal/components"
	"github.com/rapatel0/alpha/internal/components/toast"
)

func TestSessionCommandsToastSharesEditorToast(t *testing.T) {
	vis := toast.Toast{Theme: components.DefaultTheme()}
	s := NewSessionCommands(nil, nil, nil, &vis, nil)
	s.Toast.Show("hi", toast.ToastSuccess, time.Minute)
	assert.True(t, vis.Visible(), "session commands must show on the editor toast")
}
