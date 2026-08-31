package compaction

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// Anchor is an exact literal taken from source text.
type Anchor struct {
	ID    string
	Value string
	Kind  string // command, error, file, hash, url, count, test
}

type anchorPat struct {
	kind    string
	pattern *regexp.Regexp
}

var anchorPats = []anchorPat{
	{kind: "url", pattern: regexp.MustCompile(`https?://[^\s)` + "`]+")},
	{
		kind: "file",
		pattern: regexp.MustCompile(
			`(?:^|\s)(?:[~./][\w.@/\-]+\.[A-Za-z0-9_-]+|[\w@-]+(?:/[\w.@-]+)+\.[A-Za-z0-9_-]+)(?::\d+(?::\d+)?)?`,
		),
	},
	{kind: "hash", pattern: regexp.MustCompile(`(?i)\b[a-f0-9]{7,64}\b`)},
	{kind: "command", pattern: regexp.MustCompile("`(?:npm|pnpm|yarn|git|node|python|cargo|go|make|mise)\\s+[^`]+`")},
	{kind: "test", pattern: regexp.MustCompile(`\b(?:PASS|FAIL|SKIP)\s+[^\n]+`)},
	{
		kind: "error",
		pattern: regexp.MustCompile(
			`\b(?:Error|TypeError|ReferenceError|AssertionError|Cannot|Failed|FAIL)[:\s][^\n]+`,
		),
	},
	{kind: "count", pattern: regexp.MustCompile(`(?i)\b\d+(?:\.\d+)?(?:k|ms|s|%|\s+(?:tests?|files?|errors?))\b`)},
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

func extractAnchors(text string) []Anchor {
	found := make(map[string]Anchor)
	var order []string
	for _, pat := range anchorPats {
		for _, match := range pat.pattern.FindAllString(text, -1) {
			value := strings.TrimSpace(match)
			if value == "" {
				continue
			}
			if _, ok := found[value]; ok {
				continue
			}
			found[value] = Anchor{Value: value, Kind: pat.kind}
			order = append(order, value)
		}
	}
	out := make([]Anchor, 0, len(order))
	for i, value := range order {
		a := found[value]
		a.ID = "A" + pad3(i+1)
		out = append(out, a)
	}
	return out
}

func filesFromAnchors(anchors []Anchor) []string {
	var files []string
	for _, a := range anchors {
		if a.Kind == "file" {
			files = append(files, a.Value)
		}
	}
	return files
}

func pad3(n int) string {
	if n < 10 {
		return "00" + itoa(n)
	}
	if n < 100 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
