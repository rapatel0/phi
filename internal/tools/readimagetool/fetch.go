package readimagetool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pulseaiclub/phi/internal/media"
	"github.com/pulseaiclub/phi/internal/util"
)

const (
	imageFetchTimeout  = 30 * time.Second
	readImageUserAgent = "phi/1.0"
)

var allowedImageSchemes = map[string]bool{"https": true}

// allowPrivateHosts is a test-only SSRF escape hatch (httptest loopback).
var allowPrivateHosts []string

// fetchClientOverride is a test-only HTTP client (self-signed httptest TLS).
var fetchClientOverride *http.Client

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func fetchImageToCache(ctx context.Context, rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	if !allowedImageSchemes[strings.ToLower(u.Scheme)] {
		return "", fmt.Errorf("scheme %q not allowed (only https)", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("url has no host")
	}
	if err := rejectPrivateHost(u.Hostname()); err != nil {
		return "", err
	}

	cachePath := cachedImagePath(rawURL, u)
	if st, err := os.Stat(cachePath); err == nil && st.Size() > 0 {
		return cachePath, nil
	}
	if err := downloadToCache(ctx, rawURL, cachePath); err != nil {
		return "", err
	}
	return cachePath, nil
}

func cachedImagePath(rawURL string, u *url.URL) string {
	sum := sha256.Sum256([]byte(rawURL))
	name := hex.EncodeToString(sum[:]) + extFromURL(u)
	return filepath.Join(os.TempDir(), "phi-read-image", name)
}

func extFromURL(u *url.URL) string {
	last := u.Path
	if i := strings.LastIndex(last, "/"); i >= 0 {
		last = last[i+1:]
	}
	if dot := strings.LastIndex(last, "."); dot >= 0 && dot < len(last)-1 {
		ext := strings.ToLower(last[dot:])
		switch ext {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp":
			return ext
		}
	}
	return ".img"
}

func downloadToCache(parent context.Context, rawURL, cachePath string) error {
	ctx, cancel := context.WithTimeout(parent, imageFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", readImageUserAgent)
	req.Header.Set("Accept", "image/png, image/jpeg, image/gif, image/webp, image/*;q=0.8")

	client := fetchClientOverride
	if client == nil {
		client = util.DefaultHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %s", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		mt, _, _ := mime.ParseMediaType(ct)
		if mt != "" && !strings.HasPrefix(mt, "image/") {
			return fmt.Errorf("content-type %q is not an image", ct)
		}
	}

	limited := io.LimitReader(resp.Body, maxRawBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if int64(len(data)) > maxRawBytes {
		return fmt.Errorf("image too large: exceeds %d byte limit", maxRawBytes)
	}
	if len(data) == 0 {
		return errors.New("empty response body")
	}
	if media.DetectMIME(data) == "" && !strings.HasPrefix(http.DetectContentType(data), "image/") {
		return fmt.Errorf("downloaded content is not an image")
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(cachePath, data, 0o644)
}

func rejectPrivateHost(hostname string) error {
	if hostname == "" {
		return errors.New("url has no host")
	}
	if ip := net.ParseIP(hostname); ip != nil {
		if err := rejectPrivateIP(ip); err != nil && !isAllowedPrivateHost(ip) {
			return err
		}
		return nil
	}
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", hostname, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no addresses for %s", hostname)
	}
	for _, ip := range ips {
		if err := rejectPrivateIP(ip); err != nil {
			if isAllowedPrivateHost(ip) {
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
	if ip.IsUnspecified() {
		return errors.New("unspecified address (0.0.0.0 or ::)")
	}
	if ip.IsLoopback() {
		return errors.New("loopback address")
	}
	if ip.IsPrivate() {
		return errors.New("private address")
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return errors.New("link-local address")
	}
	if ip.IsMulticast() {
		return errors.New("multicast address")
	}
	if ip.IsInterfaceLocalMulticast() {
		return errors.New("interface-local multicast address")
	}
	return nil
}
