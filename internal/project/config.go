package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/rapatel0/alpha/internal/auth"
	"github.com/rapatel0/alpha/internal/brand"
	"github.com/rapatel0/alpha/internal/debuglog"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/permission"
)

// ErrNoModels reports a config with no usable model.
//
// A profile that was created but never logged in to is the ordinary cause, so
// a caller switching profiles treats it as an empty profile rather than a
// broken one.
var ErrNoModels = errors.New("missing models")

// Config is the project-level configuration loaded from ~/.alpha/config.yaml.
// All models live in one flat list under the models key; DefaultModel names
// the entry used to start sessions (empty → the first entry).
type Config struct {
	Models       []llm.ModelConfig
	DefaultModel string // name of the default model; "" → first entry
	SkillPath    string
	Permissions  permission.Policy
	Agents       AgentsConfig
}

// AgentsConfig controls whether the main agent may spawn sub-agents
// (agent_spawn / agent_wait / …). Default is enabled; set enabled: false
// to keep ordinary sessions lean and avoid loading the extra tool schemas.
type AgentsConfig struct {
	Enabled bool // true when absent from config
}

// Model returns the default model config with the skill path applied, ready
// for agent.NewEngine.
func (c *Config) Model() llm.ModelConfig {
	m := *c.defaultEntry()
	if m.SkillPath == "" {
		m.SkillPath = c.SkillPath
	}
	return m
}

// AllModels returns every configured model with the skill path applied — the
// complete set of switchable models.
func (c *Config) AllModels() []llm.ModelConfig {
	all := make([]llm.ModelConfig, len(c.Models))
	copy(all, c.Models)
	for i := range all {
		if all[i].SkillPath == "" {
			all[i].SkillPath = c.SkillPath
		}
	}
	return all
}

// FindModel returns the configured model whose name matches, so callers can
// switch to it with its own api_key/base_url/context_window.
func (c *Config) FindModel(name string) (llm.ModelConfig, bool) {
	for _, m := range c.AllModels() {
		if m.Name == name {
			return m, true
		}
	}
	return llm.ModelConfig{}, false
}

// AddModels appends extra models whose names are not already present.
func (c *Config) AddModels(extra []llm.ModelConfig) {
	if c == nil || len(extra) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(c.Models)+len(extra))
	for _, m := range c.Models {
		if m.Name != "" {
			seen[m.Name] = struct{}{}
		}
	}
	for _, m := range extra {
		if m.Name == "" {
			continue
		}
		if _, ok := seen[m.Name]; ok {
			continue
		}
		seen[m.Name] = struct{}{}
		c.Models = append(c.Models, m)
	}
}

// ConnectionForName returns the model entry for name, or a copy of a sibling
// from the same provider (so API-listed IDs reuse that provider's key/URL).
func (c *Config) ConnectionForName(name string) llm.ModelConfig {
	if m, ok := c.FindModel(name); ok {
		return m
	}
	low := strings.ToLower(strings.TrimSpace(name))
	var match func(llm.ModelConfig) bool
	switch {
	case strings.HasPrefix(low, "claude"):
		match = func(m llm.ModelConfig) bool {
			return strings.Contains(strings.ToLower(m.BaseURL), "anthropic") ||
				strings.HasPrefix(strings.ToLower(m.Name), "claude")
		}
	case strings.HasPrefix(low, "gemini"):
		match = func(m llm.ModelConfig) bool {
			return strings.Contains(strings.ToLower(m.BaseURL), "generativelanguage.googleapis.com") ||
				strings.HasPrefix(strings.ToLower(m.Name), "gemini")
		}
	case strings.HasPrefix(low, "grok"):
		match = func(m llm.ModelConfig) bool {
			return strings.Contains(strings.ToLower(m.BaseURL), "x.ai") ||
				strings.HasPrefix(strings.ToLower(m.Name), "grok")
		}
	case strings.Contains(low, "codex") || strings.HasPrefix(low, "gpt-") ||
		strings.HasPrefix(low, "o1") || strings.HasPrefix(low, "o3") || strings.HasPrefix(low, "o4"):
		match = func(m llm.ModelConfig) bool {
			return strings.Contains(strings.ToLower(m.BaseURL), "chatgpt.com") ||
				strings.Contains(strings.ToLower(m.BaseURL), "codex") ||
				strings.Contains(strings.ToLower(m.Name), "codex")
		}
	}
	if match != nil {
		for _, m := range c.AllModels() {
			if match(m) {
				m.Name = name
				return m
			}
		}
	}
	m := c.Model()
	m.Name = name
	return m
}

// defaultEntry returns the default model entry (DefaultModel by name, else
// the first entry), creating one if the config has no models yet so env-only
// setups can still apply ALPHA_* overrides.
func (c *Config) defaultEntry() *llm.ModelConfig {
	if c.DefaultModel != "" {
		for i := range c.Models {
			if c.Models[i].Name == c.DefaultModel {
				return &c.Models[i]
			}
		}
	}
	if len(c.Models) > 0 {
		return &c.Models[0]
	}
	c.Models = append(c.Models, llm.ModelConfig{})
	return &c.Models[0]
}

// firstKeyed returns the first model that already has an API key.
func (c *Config) firstKeyed() *llm.ModelConfig {
	if c == nil {
		return nil
	}
	for i := range c.Models {
		if c.Models[i].APIKey != "" {
			return &c.Models[i]
		}
	}
	return nil
}

// loadConfig reads the config file, applies environment overrides, and fills
// in defaults. A missing file yields a zero Config so env-only setups work.
func loadConfig(global GlobalLayout) (*Config, error) {
	cfg, err := parseConfigFile(global.ConfigFile())
	if err != nil {
		return nil, err
	}
	applyEnvOverrides(cfg)
	injectAuthModels(cfg, global.AuthFile())
	applyProviderEnvKeys(cfg)

	if len(cfg.Models) == 0 {
		return nil, fmt.Errorf(
			"%w (add at least one model in %s, or run alpha login)",
			ErrNoModels,
			global.ConfigFile(),
		)
	}
	def := cfg.defaultEntry()
	if def.Name == "" {
		return nil, fmt.Errorf(
			"missing model name (set %s or models[].name in %s)",
			brand.EnvName("MODEL"), global.ConfigFile(),
		)
	}
	applyOAuthKeys(cfg, global.AuthFile())
	def = cfg.defaultEntry()
	if def.APIKey == "" && cfg.DefaultModel == "" {
		// Catalog injection lists providers alphabetically. An expired
		// antigravity login then sits in front of Codex or Gemini and
		// becomes the implicit default, so a working login never starts.
		if keyed := cfg.firstKeyed(); keyed != nil {
			cfg.DefaultModel = keyed.Name
			def = keyed
		}
	}
	if def.APIKey == "" {
		return nil, fmt.Errorf(
			"missing api_key (set "+brand.EnvName("API_KEY")+" or models[].api_key in %s, "+
				"or run "+brand.Name+" login anthropic / "+brand.Name+" login codex)",
			global.ConfigFile(),
		)
	}
	for i := range cfg.Models {
		if cfg.Models[i].BaseURL == "" {
			name := strings.ToLower(cfg.Models[i].Name)
			switch {
			case strings.HasPrefix(name, "claude"):
				cfg.Models[i].BaseURL = "https://api.anthropic.com"
			case strings.HasPrefix(name, "gemini"):
				cfg.Models[i].BaseURL = "https://generativelanguage.googleapis.com/v1beta"
			case strings.HasPrefix(name, "grok"):
				cfg.Models[i].BaseURL = "https://api.x.ai/v1"
			default:
				cfg.Models[i].BaseURL = "https://api.openai.com/v1"
			}
		}
	}
	if cfg.SkillPath == "" {
		cfg.SkillPath = global.SkillsDir()
	}
	return cfg, nil
}

// parseConfigFile reads models, skill_path, and permissions from the YAML
// config file. A missing file yields a zero Config with DefaultPolicy for
// permissions; a malformed file is an error so bad config never silently
// degrades to defaults.
func parseConfigFile(path string) (*Config, error) {
	cfg := &Config{Permissions: permission.DefaultPolicy(), Agents: AgentsConfig{Enabled: true}}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	// Pointer fields distinguish "key absent" from "zero value", so per-key
	// defaults (and permission.DefaultPolicy) survive decoding and are only
	// overridden by keys that are actually present.
	var raw fileConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	for _, m := range raw.Models {
		mc := modelEntryToConfig(m)
		if m.Default && cfg.DefaultModel == "" {
			cfg.DefaultModel = mc.Name
		}
		cfg.Models = append(cfg.Models, mc)
	}
	if raw.SkillPath != nil {
		cfg.SkillPath = *raw.SkillPath
	}
	if raw.Permissions != nil {
		applyPermissions(&cfg.Permissions, raw.Permissions)
	}
	if raw.Agents != nil {
		cfg.Agents.Enabled = raw.Agents.Enabled
	}
	return cfg, nil
}

func modelEntryToConfig(m modelEntry) llm.ModelConfig {
	cfg := llm.ModelConfig{Name: m.Name, APIKey: m.APIKey, BaseURL: m.BaseURL}
	if m.ContextWindow != nil && *m.ContextWindow > 0 {
		cfg.ContextWindow = *m.ContextWindow
	}
	return cfg
}

// fileConfig mirrors the YAML keys in ~/.alpha/config.yaml.
type fileConfig struct {
	Models      []modelEntry  `yaml:"models"`
	SkillPath   *string       `yaml:"skill_path"`
	Permissions *permConfig   `yaml:"permissions"`
	Agents      *agentsConfig `yaml:"agents"`
}

type agentsConfig struct {
	Enabled bool `yaml:"enabled"`
}

type modelEntry struct {
	Name          string `yaml:"name"`
	APIKey        string `yaml:"api_key"`
	BaseURL       string `yaml:"base_url"`
	ContextWindow *int   `yaml:"context_window"`
	Default       bool   `yaml:"default"`
}

type permConfig struct {
	Mode                permission.Mode `yaml:"mode"`
	WorkspaceOnlyWrites *bool           `yaml:"workspace_only_writes"`
	AskTimeoutSec       *int            `yaml:"ask_timeout_sec"`
	DangerouslyAllowAll *bool           `yaml:"dangerously_allow_all"`
	Bash                *bashConfig     `yaml:"bash"`
}

type bashConfig struct {
	Default *string     `yaml:"default"`
	Allow   *stringList `yaml:"allow"`
	Deny    *stringList `yaml:"deny"`
}

// applyPermissions merges the file's permissions block over DefaultPolicy.
// An explicitly set list (even an empty one) replaces the default list.
func applyPermissions(p *permission.Policy, raw *permConfig) {
	if raw.Mode != "" {
		p.Mode = raw.Mode
	}
	if raw.WorkspaceOnlyWrites != nil {
		p.WorkspaceOnlyWrites = *raw.WorkspaceOnlyWrites
	}
	if raw.AskTimeoutSec != nil && *raw.AskTimeoutSec > 0 {
		p.AskTimeoutSec = *raw.AskTimeoutSec
	}
	if raw.DangerouslyAllowAll != nil {
		p.DangerouslyAllowAll = *raw.DangerouslyAllowAll
	}
	if b := raw.Bash; b != nil {
		if b.Default != nil {
			p.BashDefault = parseDecision(*b.Default, p.BashDefault)
		}
		if b.Allow != nil {
			p.BashAllow = *b.Allow
		}
		if b.Deny != nil {
			p.BashDeny = *b.Deny
		}
	}
}

// stringList accepts either a single YAML scalar or a sequence, so both
// `allow: "go test ./..."` and the block list form in the README work.
type stringList []string

func (s *stringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		*s = stringList{node.Value}
	case yaml.SequenceNode:
		items := make(stringList, 0, len(node.Content))
		for _, n := range node.Content {
			items = append(items, n.Value)
		}
		*s = items
	default:
		return errors.New("expected a string or a list of strings")
	}
	return nil
}

func countIndent(line string) int {
	n := 0
	for _, r := range line {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 2
		default:
			return n / 2
		}
	}
	// Treat 2 spaces as one indent level for our hand-rolled parser.
	return n / 2
}

func parseDecision(val string, def permission.Decision) permission.Decision {
	switch strings.ToLower(strings.TrimSpace(val)) {
	case "allow":
		return permission.Allow
	case "deny", "reject":
		return permission.Deny
	case "ask":
		return permission.Ask
	default:
		return def
	}
}

// applyOAuthKeys fills in stored credentials, one model at a time.
//
// A provider that cannot supply a key leaves its own models without one; it
// does not stop the others. Aborting here made an expired credential for a
// provider the user does not use take down every model they do use, and the
// message named the unused provider rather than anything actionable.
//
// The model itself still fails later if it is the one selected, which is where
// the failure belongs: at the model that cannot run, not at startup.
func applyOAuthKeys(c *Config, authFile string) {
	ctx := context.Background()
	for i := range c.Models {
		if err := auth.Apply(ctx, &c.Models[i], authFile); err != nil {
			debuglog.Logf("oauth: %s has no usable credential: %v", c.Models[i].Name, err)
		}
	}
}

// injectAuthModels appends catalog models for logged-in (or env-keyed)
// providers so they show up in the TUI palette without editing config.yaml.
func injectAuthModels(c *Config, authFile string) {
	if c == nil {
		return
	}
	seen := make(map[string]struct{}, len(c.Models))
	for _, m := range c.Models {
		if m.Name != "" {
			seen[m.Name] = struct{}{}
		}
	}
	add := func(models []llm.ModelConfig) {
		for _, m := range models {
			if m.Name == "" {
				continue
			}
			if _, ok := seen[m.Name]; ok {
				continue
			}
			seen[m.Name] = struct{}{}
			c.Models = append(c.Models, m)
		}
	}
	for _, p := range auth.OpenStore(authFile).Providers() {
		add(auth.Catalog(p))
	}
	if firstEnv("XAI_API_KEY") != "" {
		add(auth.Catalog(auth.ProviderXAI))
	}
	if firstEnv("GEMINI_API_KEY", "GOOGLE_API_KEY") != "" {
		add(auth.Catalog(auth.ProviderGemini))
	}
}

func applyEnvOverrides(c *Config) {
	if v := brand.Env("API_KEY"); v != "" {
		c.defaultEntry().APIKey = v
	}
	if v := brand.Env("BASE_URL"); v != "" {
		c.defaultEntry().BaseURL = v
	}
	if v := brand.Env("MODEL"); v != "" {
		c.defaultEntry().Name = v
		c.DefaultModel = v
	}
	if v := brand.Env("SKILL_PATH"); v != "" {
		c.SkillPath = v
	}
	applyProviderEnvKeys(c)
}

func applyProviderEnvKeys(c *Config) {
	for i := range c.Models {
		if c.Models[i].APIKey != "" {
			continue
		}
		name := strings.ToLower(c.Models[i].Name)
		if strings.HasPrefix(name, "gemini") {
			if v := firstEnv("GEMINI_API_KEY", "GOOGLE_API_KEY"); v != "" {
				c.Models[i].APIKey = v
			}
		}
		if strings.HasPrefix(name, "grok") {
			if v := firstEnv("XAI_API_KEY"); v != "" {
				c.Models[i].APIKey = v
			}
		}
	}
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// SetDangerouslyAllowAll persists permissions.dangerously_allow_all in config.yaml
// ("Allow All for Every Session"). Best-effort rewrite of that key.
func SetDangerouslyAllowAll(global GlobalLayout, enabled bool) error {
	path := global.ConfigFile()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := []string{}
	if len(data) > 0 {
		lines = strings.Split(string(data), "\n")
	}
	val := "false"
	if enabled {
		val = "true"
	}
	inPerm := false
	found := false
	out := make([]string, 0, len(lines)+2)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		indent := countIndent(line)
		if indent == 0 && strings.HasPrefix(trimmed, "permissions:") {
			inPerm = true
			out = append(out, line)
			continue
		}
		if indent == 0 && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			if inPerm && !found {
				out = append(out, "  dangerously_allow_all: "+val)
				found = true
			}
			inPerm = false
		}
		if inPerm && indent == 1 && strings.HasPrefix(trimmed, "dangerously_allow_all:") {
			out = append(out, "  dangerously_allow_all: "+val)
			found = true
			continue
		}
		out = append(out, line)
	}
	if inPerm && !found {
		out = append(out, "  dangerously_allow_all: "+val)
		found = true
	}
	if !found {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
		out = append(out, "permissions:", "  dangerously_allow_all: "+val)
	}
	//nolint:gosec // G306: config.yaml is meant to be user-readable
	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}
