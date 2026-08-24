package auth

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsAnthropicOAuthToken(t *testing.T) {
	require.True(t, IsAnthropicOAuthToken("sk-ant-oat-abc"))
	require.False(t, IsAnthropicOAuthToken("sk-ant-api03-abc"))
	require.False(t, IsAnthropicOAuthToken(""))
}

func TestIsCodexOAuthToken(t *testing.T) {
	require.False(t, IsCodexOAuthToken("sk-abc"))
	require.False(t, IsCodexOAuthToken("not-a-jwt"))
	tok := fakeCodexJWT(t, "acct-1")
	require.True(t, IsCodexOAuthToken(tok))
	require.Equal(t, "acct-1", ChatGPTAccountID(tok))
}

func fakeCodexJWT(t *testing.T, accountID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": accountID},
	})
	require.NoError(t, err)
	enc := base64.RawURLEncoding.EncodeToString(payload)
	return "eyJhbGciOiJub25lIn0." + enc + ".sig"
}
