package ext

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// An extension watching file writes has to name both edit and write.
func TestMatcherAcceptsSeveralToolNames(t *testing.T) {
	m := matcher("edit,write")
	assert.True(t, m("edit"))
	assert.True(t, m("write"))
	assert.False(t, m("bash"))
	assert.False(t, m("edit,write"), "the list itself is not a tool name")
}

func TestMatcherTrimsAndIgnoresBlanks(t *testing.T) {
	m := matcher(" edit , , write ")
	assert.True(t, m("edit"))
	assert.True(t, m("write"))
	assert.False(t, m(""))
}

// An empty match means every tool, which is what a nil MatchFn already means.
func TestMatcherEmptyMatchesEverything(t *testing.T) {
	assert.Nil(t, matcher(""))
	assert.Nil(t, matcher(" , , "))
}
