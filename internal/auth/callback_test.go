package auth

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// freePort finds a port nothing is listening on, so the tests do not collide
// with a real login or with each other.
func freePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

// get performs the redirect the browser would perform, and returns the status,
// content type, and body.
func get(t *testing.T, port int, query string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/cb?%s", port, query), http.NoBody)
	require.NoError(t, err)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
}

// The browser redirect must finish the login without the user copying
// anything.
func TestCallbackReceivesTheRedirect(t *testing.T) {
	port := freePort(t)
	srv := listenCallback(t.Context(), port, "/cb", "xyz", "<p>done</p>")
	require.NotNil(t, srv)
	defer srv.close()

	status, _, _ := get(t, port, "code=abc123&state=xyz")
	assert.Equal(t, http.StatusOK, status)

	select {
	case got := <-srv.wait():
		require.NoError(t, got.err)
		assert.Equal(t, "abc123", got.code)
		assert.Equal(t, "xyz", got.state)
	case <-time.After(2 * time.Second):
		t.Fatal("the redirect never arrived")
	}
}

// The user is left looking at this page, so it has to say the login worked.
func TestCallbackTellsTheUserItWorked(t *testing.T) {
	port := freePort(t)
	srv := listenCallback(t.Context(), port, "/cb", "xyz", "<p>login complete</p>")
	require.NotNil(t, srv)
	defer srv.close()

	_, ctype, body := get(t, port, "code=abc&state=xyz")
	assert.Contains(t, ctype, "text/html")
	assert.Contains(t, body, "login complete")
}

// A refusal must name a reason. "access_denied" alone does not say what to do.
func TestCallbackReportsARefusal(t *testing.T) {
	port := freePort(t)
	srv := listenCallback(t.Context(), port, "/cb", "xyz", "ok")
	require.NotNil(t, srv)
	defer srv.close()

	q := url.Values{"error": {"access_denied"}, "error_description": {"the user said no"}}
	status, _, _ := get(t, port, q.Encode())
	assert.Equal(t, http.StatusBadRequest, status)

	got := <-srv.wait()
	require.Error(t, got.err)
	assert.Contains(t, got.err.Error(), "the user said no")
}

func TestCallbackRejectsARedirectWithNoCode(t *testing.T) {
	port := freePort(t)
	srv := listenCallback(t.Context(), port, "/cb", "xyz", "ok")
	require.NotNil(t, srv)
	defer srv.close()

	status, _, _ := get(t, port, "state=xyz")
	assert.Equal(t, http.StatusBadRequest, status)

	got := <-srv.wait()
	require.Error(t, got.err)
	assert.Contains(t, got.err.Error(), "no authorization code")
}

// The port is registered with the provider and cannot be moved, so a second
// alpha waiting on it must not stop the login. Paste still works.
func TestCallbackDegradesWhenThePortIsTaken(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	assert.Nil(t, listenCallback(t.Context(), port, "/cb", "xyz", "ok"), "a taken port must not be fatal")
}

// A nil server is the normal degraded state, so every method has to tolerate
// it rather than panicking.
func TestNilCallbackServerIsSafe(t *testing.T) {
	var srv *callbackServer
	assert.Nil(t, srv.wait(), "a nil channel blocks, which is what select needs")
	assert.NotPanics(t, srv.close)
}

// The port must be released, or logging in twice in one session fails the
// second time.
func TestCallbackReleasesThePort(t *testing.T) {
	port := freePort(t)
	srv := listenCallback(t.Context(), port, "/cb", "xyz", "ok")
	require.NotNil(t, srv)
	srv.close()

	again := listenCallback(t.Context(), port, "/cb", "xyz", "ok")
	require.NotNil(t, again, "the port was not released")
	again.close()
}

// A browser that follows the redirect twice must not block the handler.
func TestCallbackSurvivesADuplicateRedirect(t *testing.T) {
	port := freePort(t)
	srv := listenCallback(t.Context(), port, "/cb", "xyz", "ok")
	require.NotNil(t, srv)
	defer srv.close()

	for range 3 {
		status, _, _ := get(t, port, "code=abc&state=xyz")
		assert.Equal(t, http.StatusOK, status)
	}
	assert.Equal(t, "abc", (<-srv.wait()).code)
}

func TestDescribeOAuthErrorPrefersTheDescription(t *testing.T) {
	assert.Equal(t, "the user said no",
		describeOAuthError(url.Values{"error": {"access_denied"}, "error_description": {"the user said no"}}))
	assert.Equal(t, "access_denied", describeOAuthError(url.Values{"error": {"access_denied"}}))
}

// A redirect from another login must not leave the user looking at a page that
// says the login worked.
func TestCallbackRejectsAForeignStateOnThePage(t *testing.T) {
	port := freePort(t)
	srv := listenCallback(t.Context(), port, "/cb", "xyz", "<p>login complete</p>")
	require.NotNil(t, srv)
	defer srv.close()

	status, _, body := get(t, port, "code=abc&state=someone-elses")
	assert.Equal(t, http.StatusBadRequest, status)
	assert.NotContains(t, body, "login complete")
	assert.Contains(t, body, "different login")

	got := <-srv.wait()
	require.Error(t, got.err)
}

// stdinLines closes its channel when stdin ends, which happens at once when
// stdin is not a terminal. A receive from a closed channel yields "", so a
// select over it would take the paste branch before the browser could redirect
// and fail the login with "missing authorization code".
func TestWaitPasteIgnoresAClosedChannel(t *testing.T) {
	closed := make(chan string)
	close(closed)

	select {
	case line := <-waitPaste(closed):
		t.Fatalf("a closed channel must not yield a paste, got %q", line)
	case <-time.After(100 * time.Millisecond):
	}
}

// A blank line is someone pressing enter, not an authorization code.
func TestWaitPasteSkipsBlankLines(t *testing.T) {
	in := make(chan string, 3)
	in <- ""
	in <- "   "
	in <- "the-code"

	select {
	case line := <-waitPaste(in):
		assert.Equal(t, "the-code", line)
	case <-time.After(2 * time.Second):
		t.Fatal("the real paste never arrived")
	}
}

func TestWaitPasteOnNilBlocks(t *testing.T) {
	select {
	case <-waitPaste(nil):
		t.Fatal("a nil channel must not yield")
	case <-time.After(50 * time.Millisecond):
	}
}
