package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rapatel0/alpha/internal/project"
	"github.com/rapatel0/alpha/internal/tui/controller"
)

const DefaultAddr = "127.0.0.1:38765"

// Server is a loopback HTTP control plane over Controller.
type Server struct {
	ctrl *controller.Controller
	bus  *controller.Bus
	http *http.Server

	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

// Options configure Listen.
type Options struct {
	Addr string // default DefaultAddr; must be loopback
	Cwd  string
}

// Listen binds a loopback control plane. It refuses non-loopback hosts.
func Listen(ctx context.Context, proj *project.Project, opts Options) (*Server, error) {
	addr := strings.TrimSpace(opts.Addr)
	if addr == "" {
		addr = DefaultAddr
	}
	if err := requireLoopback(addr); err != nil {
		return nil, err
	}
	bus := controller.NewBus(nil)
	ctrl, err := controller.NewController(bus, proj, opts.Cwd)
	if err != nil {
		return nil, err
	}
	s := &Server{
		ctrl:    ctrl,
		bus:     bus,
		clients: map[chan []byte]struct{}{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /v1/session", s.handleSession)
	mux.HandleFunc("POST /v1/prompt", s.handlePrompt)
	mux.HandleFunc("GET /v1/events", s.handleEvents)
	s.http = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go s.pump(ctx)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		ctrl.Close()
		return nil, err
	}
	go func() {
		_ = s.http.Serve(ln)
	}()
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	return s, nil
}

// Addr returns the bound address.
func (s *Server) Addr() string {
	if s == nil || s.http == nil {
		return ""
	}
	return s.http.Addr
}

// Close stops the server and the controller.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.http.Shutdown(ctx)
	if s.ctrl != nil {
		s.ctrl.Close()
	}
	return nil
}

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("serve: %s is not loopback; bind 127.0.0.1", addr)
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSession(w http.ResponseWriter, _ *http.Request) {
	id := ""
	if s.ctrl != nil {
		id = s.ctrl.SessionID()
	}
	writeJSON(w, http.StatusOK, map[string]string{"session_id": id})
}

func (s *Server) handlePrompt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	if s.ctrl == nil {
		http.Error(w, "controller unavailable", http.StatusServiceUnavailable)
		return
	}
	s.ctrl.StartPrompt(text, nil, nil)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan []byte, 16)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()
	for {
		select {
		case <-r.Context().Done():
			return
		case payload := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (s *Server) pump(ctx context.Context) {
	if s.bus == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.bus.Chan():
			for _, m := range s.bus.Drain() {
				payload, err := json.Marshal(map[string]any{
					"type": fmt.Sprintf("%T", m),
				})
				if err != nil {
					continue
				}
				s.mu.Lock()
				for ch := range s.clients {
					select {
					case ch <- payload:
					default:
					}
				}
				s.mu.Unlock()
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
