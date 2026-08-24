package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/llm"
)

// discoverInTempHome runs Discover("") with HOME redirected to a temp dir so
// tests never touch the real ~/.alpha.
func discoverInTempHome(t *testing.T) *Project {
	t.Helper()
	home := t.TempDir()
	// os.UserHomeDir uses HOME on Unix and USERPROFILE on Windows.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	p, err := Discover("")
	require.NoError(t, err)
	return p
}

func TestDiscoverCreatesGlobalDirs(t *testing.T) {
	p := discoverInTempHome(t)

	for _, dir := range []string{
		p.Global().Root(),
		p.Global().BinDir(),
		p.Global().SkillsDir(),
		p.Global().HooksDir(),
		p.Global().SessionBase(),
		p.Global().JobsDir(),
	} {
		info, err := os.Stat(dir)
		assert.NoErrorf(t, err, "expected dir %q to exist", dir)
		if err == nil {
			assert.Truef(t, info.IsDir(), "expected %q to be a directory", dir)
		}
	}
}

func TestHooksDirPath(t *testing.T) {
	p := discoverInTempHome(t)
	assert.Equal(t, filepath.Join(p.Global().Root(), "hooks"), p.Global().HooksDir())
}

func TestLookBinPrefersBinDir(t *testing.T) {
	p := discoverInTempHome(t)
	fake := filepath.Join(p.Global().BinDir(), "rg")
	require.NoError(t, os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755))

	got, err := p.Global().LookBin("rg")
	require.NoError(t, err)
	assert.Equal(t, fake, got)
}

func TestLookBinFallsBackToPATH(t *testing.T) {
	p := discoverInTempHome(t)
	got, err := p.Global().LookBin("sh")
	if err != nil {
		t.Skip(err.Error())
	}
	require.NotEmpty(t, got)
	assert.NotContains(t, got, p.Global().BinDir())
}

func TestProjectDirs(t *testing.T) {
	p := discoverInTempHome(t)
	assert.Equal(t, filepath.Join(p.Root(), ".alpha", "hooks"), p.HooksDir())
	assert.Equal(t, filepath.Join(p.Root(), ".alpha", "mcp.json"), p.MCPConfigFile())
}

func TestLoadConfigDefaults(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: deepseek-chat
    api_key: sk-test
`), 0o644))

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()

	assert.Equal(t, "deepseek-chat", cfg.Model().Name)
	assert.Equal(t, "sk-test", cfg.Model().APIKey)
	assert.Equal(t, "https://api.openai.com/v1", cfg.Model().BaseURL)
	assert.Equal(t, p.Global().SkillsDir(), cfg.SkillPath)
	// Model() carries the skill path for agent.NewEngine.
	assert.Equal(t, p.Global().SkillsDir(), cfg.Model().SkillPath)
}

func TestLoadConfigEnvOverrides(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: file-model
    api_key: file-key
skill_path: /from/file
`), 0o644))

	t.Setenv("ALPHA_MODEL", "env-model")
	t.Setenv("ALPHA_API_KEY", "env-key")
	t.Setenv("ALPHA_BASE_URL", "https://env.example/v1")
	t.Setenv("ALPHA_SKILL_PATH", "/from/env")

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()
	assert.Equal(t, "env-model", cfg.Model().Name)
	assert.Equal(t, "env-key", cfg.Model().APIKey)
	assert.Equal(t, "https://env.example/v1", cfg.Model().BaseURL)
	assert.Equal(t, "/from/env", cfg.SkillPath)
}

func TestLoadConfigMissingAPIKey(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte("models:\n  - name: x\n"), 0o644))
	t.Setenv("ALPHA_API_KEY", "")

	err := p.LoadConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_key")
}

func TestLoadConfigConfigFileMissing(t *testing.T) {
	// Env-only setup: no config file, all values from environment.
	p := discoverInTempHome(t)
	t.Setenv("ALPHA_MODEL", "env-model")
	t.Setenv("ALPHA_API_KEY", "env-key")

	require.NoError(t, p.LoadConfig())
	assert.Equal(t, "env-model", p.Config().Model().Name)
	assert.Equal(t, "interactive", string(p.Config().Permissions.Mode))
}

func TestLoadConfigPermissions(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
permissions:
  mode: headless-strict
  ask_timeout_sec: 30
  workspace_only_writes: true
  bash:
    default: ask
    allow:
      - '^echo\b'
    deny:
      - '\bsudo\b'
`), 0o644))

	require.NoError(t, p.LoadConfig())
	perm := p.Config().Permissions
	assert.Equal(t, "headless-strict", string(perm.Mode))
	assert.Equal(t, 30, perm.AskTimeoutSec)
	assert.Equal(t, []string{`^echo\b`}, perm.BashAllow)
	assert.Equal(t, []string{`\bsudo\b`}, perm.BashDeny)
	assert.True(t, p.Config().Agents.Enabled) // default on when agents: absent
}

func TestLoadConfigAgentsEnabled(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
agents:
  enabled: true
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.True(t, p.Config().Agents.Enabled)
}

func TestLoadConfigAgentsDisabled(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
agents:
  enabled: false
`), 0o644))

	require.NoError(t, p.LoadConfig())
	assert.False(t, p.Config().Agents.Enabled)
}

func TestLoadConfigScalarOrInlineListForms(t *testing.T) {
	// The old line scanner only understood block lists (and treated an inline
	// sequence as one literal string); real YAML handles scalar and flow forms.
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: m
    api_key: k
permissions:
  bash:
    allow: "go test ./..."
    deny: ['rm -rf', 'sudo']
`), 0o644))

	require.NoError(t, p.LoadConfig())
	perm := p.Config().Permissions
	assert.Equal(t, []string{"go test ./..."}, perm.BashAllow)
	assert.Equal(t, []string{"rm -rf", "sudo"}, perm.BashDeny)
}

func TestLoadConfigInvalidYAML(t *testing.T) {
	// A malformed config must fail loudly instead of silently degrading to
	// defaults (the old line scanner ignored malformed lines).
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(
		"models:\n  - name: m\n    api_key: k\npermissions: [unclosed\n",
	), 0o644))

	require.Error(t, p.LoadConfig())
}

func TestLoadConfigModelsFlat(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: p
    api_key: pk
    base_url: https://primary.example/v1
    context_window: 1000
    default: true
  - name: a1
    api_key: ak1
    base_url: https://a1.example/v1
  - name: a2
    api_key: ak2
    base_url: https://a2.example/v1
    context_window: 2000
`), 0o644))

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()
	require.Len(t, cfg.Models, 3)
	assert.Equal(t, "p", cfg.Models[0].Name)
	assert.Equal(t, "ak1", cfg.Models[1].APIKey)
	assert.Equal(t, 2000, cfg.Models[2].ContextWindow)
	assert.Equal(t, "p", cfg.DefaultModel)

	// Model() returns the entry marked default, with the skill path applied.
	m := cfg.Model()
	assert.Equal(t, "p", m.Name)
	assert.Equal(t, p.Global().SkillsDir(), m.SkillPath)

	// Models() lists every entry with the skill path applied.
	models := cfg.AllModels()
	require.Len(t, models, 3)
	for _, mm := range models {
		assert.Equal(t, p.Global().SkillsDir(), mm.SkillPath)
	}

	// FindModel returns the full per-model connection config.
	a2, ok := cfg.FindModel("a2")
	require.True(t, ok)
	assert.Equal(t, "https://a2.example/v1", a2.BaseURL)
	_, ok = cfg.FindModel("nope")
	assert.False(t, ok)
}

func TestConnectionForNameUsesProviderSibling(t *testing.T) {
	cfg := &Config{Models: []llm.ModelConfig{
		{Name: "local", APIKey: "lk", BaseURL: "https://llm.example/v1"},
		{Name: "claude-sonnet-5", APIKey: "ak", BaseURL: "https://api.anthropic.com"},
	}}
	got := cfg.ConnectionForName("claude-opus-5")
	assert.Equal(t, "claude-opus-5", got.Name)
	assert.Equal(t, "ak", got.APIKey)
	assert.Equal(t, "https://api.anthropic.com", got.BaseURL)
	got = cfg.ConnectionForName("mystery")
	assert.Equal(t, "mystery", got.Name)
	assert.Equal(t, "lk", got.APIKey)
}

func TestLoadConfigDefaultFallsBackToFirst(t *testing.T) {
	// No entry marked default → the first entry wins.
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: first
    api_key: k1
  - name: second
    api_key: k2
`), 0o644))

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()
	assert.Empty(t, cfg.DefaultModel)
	assert.Equal(t, "first", cfg.Model().Name)
}

func TestLoadConfigInjectsLoggedInCatalog(t *testing.T) {
	p := discoverInTempHome(t)
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: local
    api_key: k
    default: true
`), 0o644))
	st := auth.OpenStore(p.Global().AuthFile())
	require.NoError(t, st.Put(auth.Credential{Provider: auth.ProviderAnthropic, AccessToken: "sk-ant-oat-x"}))
	require.NoError(t, st.Put(auth.Credential{Provider: auth.ProviderCodex, AccessToken: "oat-codex"}))

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()
	assert.Equal(t, "local", cfg.Model().Name)
	claude, ok := cfg.FindModel("claude-sonnet-5")
	require.True(t, ok)
	assert.Equal(t, "sk-ant-oat-x", claude.APIKey)
	assert.Equal(t, "https://api.anthropic.com", claude.BaseURL)
	codex, ok := cfg.FindModel("gpt-5.5")
	require.True(t, ok)
	assert.Equal(t, "oat-codex", codex.APIKey)
	_, ok = cfg.FindModel("grok-4.6")
	assert.False(t, ok)
}

func TestLoadConfigInjectsEnvGrokGemini(t *testing.T) {
	p := discoverInTempHome(t)
	require.NoError(t, os.WriteFile(p.Global().ConfigFile(), []byte(`
models:
  - name: local
    api_key: k
`), 0o644))
	t.Setenv("XAI_API_KEY", "xai-k")
	t.Setenv("GEMINI_API_KEY", "gem-k")

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()
	grok, ok := cfg.FindModel("grok-4.6")
	require.True(t, ok)
	assert.Equal(t, "xai-k", grok.APIKey)
	gem, ok := cfg.FindModel("gemini-2.5-flash")
	require.True(t, ok)
	assert.Equal(t, "gem-k", gem.APIKey)
}

func TestLoadConfigOAuthOnly(t *testing.T) {
	p := discoverInTempHome(t)
	t.Setenv("ALPHA_MODEL", "")
	t.Setenv("ALPHA_API_KEY", "")
	st := auth.OpenStore(p.Global().AuthFile())
	require.NoError(t, st.Put(auth.Credential{Provider: auth.ProviderAnthropic, AccessToken: "sk-ant-oat-x"}))

	require.NoError(t, p.LoadConfig())
	cfg := p.Config()
	assert.Equal(t, "claude-opus-5", cfg.Model().Name)
	assert.Equal(t, "sk-ant-oat-x", cfg.Model().APIKey)
}
