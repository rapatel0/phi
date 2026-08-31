package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rapatel0/alpha/internal/debuglog"
	"github.com/rapatel0/alpha/internal/llm"
	"github.com/rapatel0/alpha/internal/llm/modellist"
	"github.com/rapatel0/alpha/internal/profile"
	"github.com/rapatel0/alpha/internal/project"
)

// Profile returns the active credential profile name.
func (c *Controller) Profile() string {
	if c == nil || c.proj == nil {
		return ""
	}
	return c.proj.Global().Profile()
}

// Profiles lists the credential profiles available to switch to.
func (c *Controller) Profiles() []string {
	if c == nil || c.proj == nil {
		return nil
	}
	return profile.List(c.proj.Global().Root())
}

// SetProfile switches the credential set the running session uses.
//
// The session tree is kept, so the conversation survives the switch. The
// current model has to be re-resolved because credentials are folded into the
// config when it loads: keeping the old connection would send the previous
// account's token to the provider.
//
// A model that the new profile cannot reach is a failure, and the profile is
// put back rather than leaving the session pointed at a model it cannot use.
func (c *Controller) SetProfile(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("empty profile name")
	}
	if c == nil || c.proj == nil {
		return errors.New("project not available")
	}
	previous := c.proj.Global().Profile()
	if name == previous {
		return nil
	}
	if c.engine == nil {
		return errors.New("agent not configured")
	}

	// A switch mid-stream would answer the running request with the other
	// account's credential.
	c.Cancel()

	if err := c.proj.UseProfile(name); err != nil {
		return err
	}

	cfg := c.proj.Config()
	if cfg == nil {
		_ = c.proj.UseProfile(previous)
		return errors.New("project not available")
	}
	model := cfg.ConnectionForName(c.modelCfg.Name)
	if model.Name == "" {
		_ = c.proj.UseProfile(previous)
		return fmt.Errorf("profile %s has no model %s: log in to it first", name, c.modelCfg.Name)
	}

	c.engine.SetAuthFile(c.proj.Global().AuthFile())
	if err := c.engine.SetModel(model); err != nil {
		_ = c.proj.UseProfile(previous)
		c.engine.SetAuthFile(c.proj.Global().AuthFile())
		return err
	}
	c.modelCfg = model

	return nil
}

// CreateProfile makes a named credential set. It does not switch to it:
// an empty profile cannot run the current model until you log in.
func (c *Controller) CreateProfile(name string) error {
	name = strings.TrimSpace(name)
	if c == nil || c.proj == nil {
		return errors.New("project not available")
	}
	_, err := profile.Create(c.proj.Global().Root(), name)
	return err
}

// SetModel replaces the LLM client while keeping the same session tree.
func (c *Controller) SetModel(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("empty model name")
	}
	if c.proj == nil {
		return errors.New("project not available")
	}
	cfg := c.proj.Config()
	if cfg == nil {
		if err := c.proj.LoadConfig(); err != nil {
			return err
		}
		cfg = c.proj.Config()
	}
	if cfg == nil {
		return errors.New("project not available")
	}
	model := cfg.ConnectionForName(name)
	c.Cancel()
	c.initGate(cfg.Permissions)
	if c.engine == nil {
		return errors.New("agent not configured")
	}
	c.engine.SetPermission(c.gate, c.askPermission)
	c.engine.SetContinueAsk(c.askContinue)
	c.engine.SetJobs(c.engineJobs())
	if _, _, err := c.ReloadHooks(); err != nil {
		debuglog.Logf("hooks: reload on SetModel: %v", err)
	}
	if err := c.engine.SetModel(model); err != nil {
		return err
	}
	c.modelCfg = model
	return nil
}

const modelListTTL = 2 * time.Minute

// RefreshModelCatalog fetches /models from each unique provider endpoint and
// merges IDs into the live config so the palette and SetModel share them.
// Failures keep the config/catalog list. ALPHA_MODEL_LIST=0 skips the network.
func (c *Controller) RefreshModelCatalog(ctx context.Context) []string {
	if c == nil || c.proj == nil || c.proj.Config() == nil {
		return nil
	}
	cfg := c.proj.Config()
	c.modelListMu.Lock()
	if time.Since(c.modelListAt) < modelListTTL && len(c.modelList) > 0 {
		out := append([]string(nil), c.modelList...)
		c.modelListMu.Unlock()
		return out
	}
	c.modelListMu.Unlock()

	if !modellist.Disabled() {
		var (
			wg    sync.WaitGroup
			mu    sync.Mutex
			extra []llm.ModelConfig
		)
		for _, ep := range uniqueModelEndpoints(cfg) {
			wg.Add(1)
			go func(ep llm.ModelConfig) {
				defer wg.Done()
				ids, err := modellist.Fetch(ctx, ep)
				if err != nil || len(ids) == 0 {
					return
				}
				mu.Lock()
				for _, id := range ids {
					m := ep
					m.Name = id
					extra = append(extra, m)
				}
				mu.Unlock()
			}(ep)
		}
		wg.Wait()
		cfg.AddModels(extra)
	}

	names := make([]string, 0, len(cfg.AllModels()))
	for _, m := range cfg.AllModels() {
		if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	c.modelListMu.Lock()
	c.modelList = names
	c.modelListAt = time.Now()
	c.modelListMu.Unlock()
	return names
}

func uniqueModelEndpoints(cfg *project.Config) []llm.ModelConfig {
	if cfg == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []llm.ModelConfig
	for _, m := range cfg.AllModels() {
		if strings.TrimSpace(m.APIKey) == "" {
			continue
		}
		key := strings.ToLower(strings.TrimRight(m.BaseURL, "/")) + "\x00" + m.APIKey
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, m)
	}
	return out
}

// LiveJobCount returns in-flight sub-agent jobs (0 if jobs disabled).
func (c *Controller) authFile() string {
	if c == nil || c.proj == nil {
		return ""
	}
	return c.proj.Global().AuthFile()
}
