package auth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// callbackResult is what the browser redirect carried back.
type callbackResult struct {
	code  string
	state string
	err   error
}

// callbackServer receives the OAuth redirect on a fixed loopback port.
//
// The port is not ours to choose: it is registered with the provider as part
// of the redirect URI, so binding a different one would make the redirect miss
// this process.
type callbackServer struct {
	results <-chan callbackResult
	stop    func()
}

// listenCallback serves path on port until the caller stops it.
//
// A bind failure is not fatal and returns a nil server. Another alpha may
// already be waiting on that port, or the machine may forbid the bind. The
// caller still has the paste path, which is the only option over SSH anyway.
// wantState must come back in the redirect, proving it belongs to the login
// this process started.
func listenCallback(ctx context.Context, port int, path, wantState, successHTML string) *callbackServer {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil
	}

	// Buffered: the handler must not block if the caller already stopped
	// waiting, or the browser hangs and the goroutine leaks.
	results := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		res := callbackResult{code: q.Get("code"), state: q.Get("state")}
		switch {
		case q.Get("error") != "":
			res.err = fmt.Errorf("the provider refused the login: %s", describeOAuthError(q))
		case res.code == "":
			res.err = errors.New("the redirect carried no authorization code")
		case res.state != wantState:
			// Checked here rather than only in the caller, so the page the
			// user is left looking at does not claim success.
			res.err = errors.New("this redirect belongs to a different login")
		}

		if res.err != nil {
			http.Error(w, res.err.Error(), http.StatusBadRequest)
		} else {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, successHTML)
		}
		select {
		case results <- res:
		default:
		}
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	var once sync.Once
	return &callbackServer{
		results: results,
		stop: func() {
			once.Do(func() {
				// Shutdown alone leaves a window: it only closes
				// listeners Serve has registered, and Serve runs in
				// another goroutine that may not have started. Closing
				// the listener here frees the port before this returns,
				// so a second login can bind the same fixed port.
				_ = ln.Close()

				shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = srv.Shutdown(shut)
			})
		},
	}
}

// wait returns the channel the redirect arrives on, or nil when the server
// never started. A receive from a nil channel blocks forever, which is what a
// select over it should do.
func (c *callbackServer) wait() <-chan callbackResult {
	if c == nil {
		return nil
	}
	return c.results
}

// close releases the port.
func (c *callbackServer) close() {
	if c != nil {
		c.stop()
	}
}

// describeOAuthError prefers the human-readable description when the provider
// sends one, since "access_denied" alone does not say what to do next.
func describeOAuthError(q url.Values) string {
	if d := q.Get("error_description"); d != "" {
		return d
	}
	return q.Get("error")
}
