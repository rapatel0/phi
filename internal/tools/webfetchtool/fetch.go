package webfetchtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/tools/tooldef"
	"github.com/rapatel0/alpha/internal/util"
)

const (
	fetchTimeout   = 20 * time.Second
	maxFetchBytes  = 512 << 10 // 512 KiB raw
	maxTextRunes   = 24_000
	fetchUserAgent = "alpha/1.0 (webfetch)"
)

var (
	scriptRe = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe  = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	tagRe    = regexp.MustCompile(`<[^>]+>`)
	spaceRe  = regexp.MustCompile(`[ \t\r\n]+`)
)

// allowPrivateHosts is a test-only SSRF escape hatch (httptest loopback).
var allowPrivateHosts []string

var fetchClientOverride *http.Client

// WebFetchTool returns an HTTPS fetch tool (HTML reduced to text).
func WebFetchTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name: "webfetch",
			Description: `Fetch an https URL and return text (HTML tags stripped).

Use for documentation, raw files, or pages. Will not follow redirects onto private hosts.`,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"url": llm.Object{
						"type":        "string",
						"description": "HTTPS URL to fetch. Example: https://example.com/docs",
					},
				},
				Required: []string{"url"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.URL)
		},
		Run: runWebFetch,
	}
}

func runWebFetch(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("webfetch args: %w", err)
	}
	raw, err := FetchRaw(ctx, in.URL)
	if err != nil {
		return tooldef.Result{}, err
	}
	text := htmlToText(string(raw))
	runes := []rune(text)
	if len(runes) > maxTextRunes {
		text = string(runes[:maxTextRunes]) + "\n…[truncated]"
	}
	return tooldef.Result{Content: text, Output: text, Detail: strings.TrimSpace(in.URL)}, nil
}

// FetchText GETs an https URL with SSRF checks and returns stripped text.
func FetchText(ctx context.Context, rawURL string) (string, error) {
	body, err := FetchRaw(ctx, rawURL)
	if err != nil {
		return "", err
	}
	text := htmlToText(string(body))
	runes := []rune(text)
	if len(runes) > maxTextRunes {
		text = string(runes[:maxTextRunes]) + "\n…[truncated]"
	}
	return text, nil
}

// FetchRaw GETs an https URL with the same SSRF checks as FetchText.
func FetchRaw(ctx context.Context, rawURL string) ([]byte, error) {
	return FetchRawWithUA(ctx, rawURL, fetchUserAgent)
}

// FetchRawWithUA is FetchRaw with a custom User-Agent (DuckDuckGo HTML).
func FetchRawWithUA(ctx context.Context, rawURL, ua string) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("webfetch: parse url: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return nil, errors.New("webfetch: only https URLs are allowed")
	}
	if u.Host == "" {
		return nil, errors.New("webfetch: url has no host")
	}
	if err := rejectPrivateHost(ctx, u.Hostname()); err != nil {
		return nil, fmt.Errorf("webfetch: %w", err)
	}
	if strings.TrimSpace(ua) == "" {
		ua = fetchUserAgent
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "text/html, text/plain, application/json, */*;q=0.5")

	client := fetchClientOverride
	if client == nil {
		c := *util.DefaultHTTPClient()
		c.CheckRedirect = checkRedirect(ctx)
		client = &c
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webfetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("webfetch: http %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return nil, fmt.Errorf("webfetch: read: %w", err)
	}
	if int64(len(body)) > maxFetchBytes {
		body = body[:maxFetchBytes]
	}
	return body, nil
}

// checkRedirect keeps redirects on https and off private hosts.
func checkRedirect(ctx context.Context) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL == nil || !strings.EqualFold(req.URL.Scheme, "https") {
			return errors.New("redirect is not https")
		}
		return rejectPrivateHost(ctx, req.URL.Hostname())
	}
}

func htmlToText(s string) string {
	s = scriptRe.ReplaceAllString(s, " ")
	s = styleRe.ReplaceAllString(s, " ")
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = spaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func rejectPrivateHost(ctx context.Context, hostname string) error {
	if hostname == "" {
		return errors.New("url has no host")
	}
	if ip := net.ParseIP(hostname); ip != nil {
		if err := rejectPrivateIP(ip); err != nil && !isAllowedPrivateHost(ip) {
			return err
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", hostname, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("no addresses for %s", hostname)
	}
	for _, addr := range addrs {
		if err := rejectPrivateIP(addr.IP); err != nil {
			if isAllowedPrivateHost(addr.IP) {
				continue
			}
			return fmt.Errorf("%s resolves to private address: %w", hostname, err)
		}
	}
	return nil
}

func isAllowedPrivateHost(ip net.IP) bool {
	for _, allowed := range allowPrivateHosts {
		if allowed != "" && ip.Equal(net.ParseIP(allowed)) {
			return true
		}
	}
	return false
}

func rejectPrivateIP(ip net.IP) error {
	if ip == nil {
		return errors.New("nil ip")
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return errors.New("private or local address")
	}
	return nil
}
