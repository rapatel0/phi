package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeLF(t *testing.T) {
	require.Equal(t, "a\nb\nc", NormalizeLF("a\r\nb\rc"))
	require.Equal(t, "already\nlf", NormalizeLF("already\nlf"))
	require.Empty(t, NormalizeLF(""))
}

func TestTruncate(t *testing.T) {
	require.Equal(t, "ab", Truncate("ab", 2))
	require.Equal(t, "ab…", Truncate("abcd", 2))
	require.Equal(t, "abcd", Truncate("abcd", -1))
}

func TestMustJSON(t *testing.T) {
	require.Equal(t, `{"n":1}`, MustJSON(map[string]int{"n": 1}))
	require.Equal(t, "{\n  \"n\": 1\n}", MustJSONIndent(map[string]int{"n": 1}))
}
