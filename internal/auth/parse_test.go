package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAuthorizationInput(t *testing.T) {
	code, state := parseAuthorizationInput("http://localhost:53692/callback?code=abc&state=xyz")
	require.Equal(t, "abc", code)
	require.Equal(t, "xyz", state)

	code, state = parseAuthorizationInput("abc#xyz")
	require.Equal(t, "abc", code)
	require.Equal(t, "xyz", state)

	code, state = parseAuthorizationInput("just-the-code")
	require.Equal(t, "just-the-code", code)
	require.Equal(t, "", state)
}
