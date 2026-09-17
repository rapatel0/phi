package controller

import (
	"path/filepath"
	"testing"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/project"
)

func TestAddLoggedInModelsAddsCurrentCodexCatalog(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "auth.json")
	if err := auth.OpenStore(authFile).Put(auth.Credential{
		Provider:    auth.ProviderCodex,
		AccessToken: "oat-codex",
	}); err != nil {
		t.Fatal(err)
	}

	cfg := &project.Config{}
	addLoggedInModels(t.Context(), cfg, authFile)
	for _, name := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		model, ok := cfg.FindModel(name)
		if !ok {
			t.Fatalf("missing model %q", name)
		}
		if model.APIKey != "oat-codex" {
			t.Fatalf("model %q has API key %q", name, model.APIKey)
		}
	}
}
