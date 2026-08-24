package websearchtool

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/pulseaiclub/phi/internal/tools/webfetchtool"
)

const (
	ddgHTML    = "https://html.duckduckgo.com/html/?q="
	ddgUA      = "Mozilla/5.0 (compatible; PhiSearch/1.0)"
	maxResults = 8
)

var (
	ddgTitleRe   = regexp.MustCompile(`(?is)<a[^>]+class="result__a"[^>]*href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRe = regexp.MustCompile(`(?is)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)
	htmlTagRe    = regexp.MustCompile(`<[^>]+>`)
)

// fetchRaw is swapped in tests.
var fetchRaw = webfetchtool.FetchRawWithUA

func ddgSearch(ctx context.Context, query string) (string, error) {
	raw, err := fetchRaw(ctx, ddgHTML+url.QueryEscape(query), ddgUA)
	if err != nil {
		return "", fmt.Errorf("websearch: %w", err)
	}
	hits := parseDDG(string(raw))
	if len(hits) == 0 {
		return "No results for " + query, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Search: %s\n", query)
	for i, h := range hits {
		if i >= maxResults {
			break
		}
		fmt.Fprintf(&b, "\n%d. %s\n   %s", i+1, h.title, h.link)
		if h.snippet != "" {
			fmt.Fprintf(&b, "\n   %s", h.snippet)
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

type hit struct{ title, link, snippet string }

func parseDDG(page string) []hit {
	titles := ddgTitleRe.FindAllStringSubmatch(page, maxResults)
	if len(titles) == 0 {
		return parsePlainLinks(page)
	}
	snippets := ddgSnippetRe.FindAllStringSubmatch(page, maxResults)
	out := make([]hit, 0, len(titles))
	for i, m := range titles {
		link := decodeDDGLink(html.UnescapeString(m[1]))
		title := strings.TrimSpace(html.UnescapeString(stripTags(m[2])))
		if link == "" {
			continue
		}
		h := hit{title: title, link: link}
		if i < len(snippets) {
			h.snippet = strings.TrimSpace(html.UnescapeString(stripTags(snippets[i][1])))
		}
		out = append(out, h)
	}
	return out
}

func parsePlainLinks(page string) []hit {
	re := regexp.MustCompile(`https://[^\s<>"]+`)
	seen := map[string]struct{}{}
	var out []hit
	for _, loc := range re.FindAllString(page, 40) {
		loc = strings.TrimRight(loc, ".,);")
		if strings.Contains(loc, "duckduckgo.com") {
			continue
		}
		if _, ok := seen[loc]; ok {
			continue
		}
		seen[loc] = struct{}{}
		out = append(out, hit{title: loc, link: loc})
		if len(out) >= maxResults {
			break
		}
	}
	return out
}

func decodeDDGLink(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		if strings.HasPrefix(href, "http") {
			return href
		}
		return ""
	}
	if v := u.Query().Get("uddg"); v != "" {
		if out, err := url.QueryUnescape(v); err == nil {
			return out
		}
		return v
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return href
	}
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	return ""
}

func stripTags(s string) string {
	return htmlTagRe.ReplaceAllString(s, "")
}
