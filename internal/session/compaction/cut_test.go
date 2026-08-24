package compaction

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/session"
)

func TestFindCutIndex_ExceedsTokensChoosesNearestCutPoint(t *testing.T) {
	entries := []session.MessageEntry{
		session.SessionMessageEntry{
			Message: llm.Message{Usage: llm.Usage{TotalTokens: 10}},
		},
		session.SessionMessageEntry{
			Message: llm.Message{Usage: llm.Usage{TotalTokens: 20}},
		},
		session.BranchSummaryEntry{}, // non-message, should be skipped for token accumulation
		session.SessionMessageEntry{
			Message: llm.Message{Usage: llm.Usage{TotalTokens: 30}},
		},
		session.SessionMessageEntry{
			Message: llm.Message{Usage: llm.Usage{TotalTokens: 40}},
		},
	}

	startIndex := 0
	endIndex := len(entries)
	keepRecentTokens := 50
	cutPoints := []int{1, 3, 4}

	cutIndex := findCutIndex(entries, startIndex, endIndex, keepRecentTokens, cutPoints)

	assert.Equal(t, 3, cutIndex)
}

func TestFindCutIndex_NotExceedTokensReturnsFirstCutPoint(t *testing.T) {
	entries := []session.MessageEntry{
		session.SessionMessageEntry{
			Message: llm.Message{Usage: llm.Usage{TotalTokens: 10}},
		},
		session.SessionMessageEntry{
			Message: llm.Message{Usage: llm.Usage{TotalTokens: 20}},
		},
		session.SessionMessageEntry{
			Message: llm.Message{Usage: llm.Usage{TotalTokens: 30}},
		},
	}

	startIndex := 0
	endIndex := len(entries)
	keepRecentTokens := 200
	cutPoints := []int{0, 1, 2}

	cutIndex := findCutIndex(entries, startIndex, endIndex, keepRecentTokens, cutPoints)

	assert.Equal(t, cutPoints[0], cutIndex)
}
