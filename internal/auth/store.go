package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Provider names stored in auth.json.
const (
	ProviderAnthropic = "anthropic"
	ProviderCodex     = "codex"
	ProviderGemini    = "gemini"
	// ProviderXAI is declared with the SuperGrok login in xai.go.
)

// Credential is one OAuth login.
type Credential struct {
	Provider     string    `json:"provider"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

// Valid reports a non-empty access token.
func (c Credential) Valid() bool {
	return strings.TrimSpace(c.AccessToken) != ""
}

// Expired reports whether the access token is past ExpiresAt (with a 5-minute
// skew). Zero ExpiresAt never expires.
func (c Credential) Expired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(c.ExpiresAt.Add(-5 * time.Minute))
}

type fileStore struct {
	Credentials map[string]Credential `json:"credentials"`
}

// Store is a JSON file of provider credentials.
type Store struct {
	path string
	mu   sync.Mutex
}

// OpenStore loads path (missing file is empty).
func OpenStore(path string) *Store {
	return &Store{path: path}
}

// Get returns the credential for provider, if any.
func (s *Store) Get(provider string) (Credential, bool) {
	if s == nil {
		return Credential{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fs, err := s.read()
	if err != nil {
		return Credential{}, false
	}
	c, ok := fs.Credentials[strings.ToLower(provider)]
	if !ok || !c.Valid() {
		return Credential{}, false
	}
	return c, true
}

// Providers returns logged-in provider names with a non-empty access token.
func (s *Store) Providers() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fs, err := s.read()
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(fs.Credentials))
	for p, c := range fs.Credentials {
		if c.Valid() {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// Put writes cred, creating the parent directory.
func (s *Store) Put(cred Credential) error {
	if s == nil {
		return fmt.Errorf("auth: nil store")
	}
	if !cred.Valid() {
		return fmt.Errorf("auth: empty access token")
	}
	cred.Provider = strings.ToLower(strings.TrimSpace(cred.Provider))
	if cred.Provider == "" {
		return fmt.Errorf("auth: missing provider")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fs, err := s.read()
	if err != nil {
		return err
	}
	if fs.Credentials == nil {
		fs.Credentials = make(map[string]Credential)
	}
	fs.Credentials[cred.Provider] = cred
	return s.write(fs)
}

func (s *Store) read() (fileStore, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileStore{Credentials: map[string]Credential{}}, nil
		}
		return fileStore{}, fmt.Errorf("auth: read %s: %w", s.path, err)
	}
	var fs fileStore
	if err := json.Unmarshal(data, &fs); err != nil {
		return fileStore{}, fmt.Errorf("auth: parse %s: %w", s.path, err)
	}
	if fs.Credentials == nil {
		fs.Credentials = map[string]Credential{}
	}
	return fs, nil
}

func (s *Store) write(fs fileStore) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("auth: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(fs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("auth: write: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("auth: rename: %w", err)
	}
	return nil
}
