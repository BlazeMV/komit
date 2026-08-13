// Package config resolves the prompt configuration: built-in defaults, then the
// user's global file, then the repo's .komit.yml — each overriding per key.
package config

import (
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed default.yml
var defaultYAML []byte

// Supported provider names.
const (
	ProviderCLI       = "claude-cli"
	ProviderAnthropic = "anthropic"
	ProviderOpenAI    = "openai"
)

// ProviderKinds lists every provider implementation a block can select.
var ProviderKinds = []string{ProviderCLI, ProviderAnthropic, ProviderOpenAI}

// DefaultBin is the claude executable looked up when no bin is configured.
const DefaultBin = "claude"

// Vendor endpoints used when a provider sets no base_url.
const (
	AnthropicBaseURL = "https://api.anthropic.com"
	OpenAIBaseURL    = "https://api.openai.com/v1"
)

// defaultKeyEnv is the environment variable each API provider reads when the
// config names none.
var defaultKeyEnv = map[string]string{
	ProviderAnthropic: "ANTHROPIC_API_KEY",
	ProviderOpenAI:    "OPENAI_API_KEY",
}

// Provider is one backend's settings, stored under a free-form label. Type
// names the implementation and defaults to the label, so a block called openai
// needs no type; a second OpenAI-compatible endpoint sets type: openai. Which
// other keys apply depends on the kind: Bin belongs to claude-cli, BaseURL and
// the key fields to the API providers.
type Provider struct {
	Type      string `yaml:"type"`
	Model     string `yaml:"model"`
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key"`
	APIKeyEnv string `yaml:"api_key_env"`
	Bin       string `yaml:"bin"`
}

// Refresh controls how the change list picks up work done outside komit.
type Refresh struct {
	OnFocus  bool `yaml:"on_focus"`
	Interval int  `yaml:"interval"` // seconds between polls; 0 disables them
}

// Every is Interval as a duration, or 0 when polling is off.
func (r Refresh) Every() time.Duration {
	if r.Interval <= 0 {
		return 0
	}
	return time.Duration(r.Interval) * time.Second
}

// Config is komit's entire configuration surface.
type Config struct {
	Provider  string              `yaml:"provider"`
	Providers map[string]Provider `yaml:"providers"`
	// Model was v0.2's top-level key; it is read only so Validate can name the
	// rewrite instead of silently ignoring it.
	Model string `yaml:"model"`
	// RecentCommits is how many commit subjects feed {{recent_commits}}; 0 omits them.
	RecentCommits int     `yaml:"recent_commits"`
	Prompt        string  `yaml:"prompt"`
	Refresh       Refresh `yaml:"refresh"`
}

// RepoFile is the per-repository override read from the repo root.
const RepoFile = ".komit.yml"

// Default returns the built-in configuration.
func Default() Config {
	var c Config
	if err := yaml.Unmarshal(defaultYAML, &c); err != nil {
		panic("embedded default.yml is invalid: " + err.Error())
	}
	c.Prompt = strings.TrimSpace(c.Prompt)
	return c
}

// DefaultYAML is the built-in configuration verbatim, for `komit init` to write.
func DefaultYAML() []byte {
	return append([]byte(nil), defaultYAML...)
}

// UserPath is the global config location: $XDG_CONFIG_HOME/komit/config.yml,
// falling back to ~/.config/komit/config.yml.
func UserPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "komit", "config.yml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "komit", "config.yml"), nil
}

// Load merges the built-in defaults, the global file and the repo file. A
// missing file is skipped; a malformed one is an error. Warnings are advisory —
// they are returned for display, not raised.
func Load(repoRoot string) (Config, []string, error) {
	cfg := Default()

	userPath, err := UserPath()
	if err != nil {
		return cfg, nil, err
	}

	var warnings []string
	for _, src := range []struct {
		path         string
		allowSecrets bool
	}{
		{userPath, true},
		{filepath.Join(repoRoot, RepoFile), false},
	} {
		w, err := mergeFile(&cfg, src.path, src.allowSecrets)
		if err != nil {
			return cfg, warnings, err
		}
		warnings = append(warnings, w...)
	}
	cfg.Prompt = strings.TrimSpace(cfg.Prompt)
	return cfg, warnings, nil
}

// mergeFile updates cfg from path. Missing files are skipped; malformed ones
// error. Top-level keys merge by unmarshalling over cfg, which touches only the
// keys the file names — interval: 0 has to stay distinct from an absent
// interval. Providers cannot: unmarshalling into a live map replaces whole
// blocks, so they are merged field by field instead.
func mergeFile(cfg *Config, path string, allowSecrets bool) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	merged := cfg.Providers
	cfg.Providers = nil
	if err := yaml.Unmarshal(data, cfg); err != nil {
		cfg.Providers = merged
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	incoming := cfg.Providers

	var warnings []string
	if !allowSecrets {
		for name, p := range incoming {
			if p.APIKey == "" {
				continue
			}
			warnings = append(warnings, fmt.Sprintf(
				"%s: ignored api_key under providers.%s — keep keys out of a file the repo can commit", path, name))
			p.APIKey = ""
			incoming[name] = p
		}
	}
	cfg.Providers = mergeProviders(merged, incoming)
	return warnings, nil
}

// mergeProviders overlays incoming onto base per field. Every Provider field is
// a string whose zero value means unset, so any non-empty value wins.
func mergeProviders(base, incoming map[string]Provider) map[string]Provider {
	if len(incoming) == 0 {
		return base
	}
	out := make(map[string]Provider, len(base)+len(incoming))
	for name, p := range base {
		out[name] = p
	}
	for name, in := range incoming {
		p := out[name]
		for _, f := range []struct{ dst, src *string }{
			{&p.Type, &in.Type},
			{&p.Model, &in.Model},
			{&p.BaseURL, &in.BaseURL},
			{&p.APIKey, &in.APIKey},
			{&p.APIKeyEnv, &in.APIKeyEnv},
			{&p.Bin, &in.Bin},
		} {
			if *f.src != "" {
				*f.dst = *f.src
			}
		}
		out[name] = p
	}
	return out
}

// Active returns the selected provider's settings.
func (c Config) Active() Provider { return c.Providers[c.Provider] }

// Kind is the implementation the active block selects: its type, or its label
// when it sets none.
func (c Config) Kind() string {
	if t := c.Active().Type; t != "" {
		return t
	}
	return c.Provider
}

// labels lists the configured provider blocks, for error messages.
func (c Config) labels() []string {
	out := make([]string, 0, len(c.Providers))
	for name := range c.Providers {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

// Bin is the claude executable to run.
func (c Config) Bin() string {
	if bin := c.Active().Bin; bin != "" {
		return bin
	}
	return DefaultBin
}

// BaseURL is the active provider's endpoint root, falling back to the vendor's.
func (c Config) BaseURL() string {
	if u := c.Active().BaseURL; u != "" {
		return strings.TrimRight(u, "/")
	}
	switch c.Kind() {
	case ProviderAnthropic:
		return AnthropicBaseURL
	case ProviderOpenAI:
		return OpenAIBaseURL
	}
	return ""
}

// APIKey resolves the active provider's key: the named environment variable if
// api_key_env is set, else the provider's own, else the configured value.
// Naming api_key_env opts out of the default variable entirely, so a stray
// OPENAI_API_KEY cannot stand in for the OpenRouter key you meant.
func (c Config) APIKey() string {
	p := c.Active()
	env := p.APIKeyEnv
	if env == "" {
		env = defaultKeyEnv[c.Kind()]
	}
	if env != "" {
		if v := os.Getenv(env); v != "" {
			return v
		}
	}
	return p.APIKey
}

// Validate reports everything that must be fixed before komit can generate, so
// one run surfaces them all rather than one per attempt.
func (c Config) Validate() []error {
	var errs []error

	if c.Model != "" {
		errs = append(errs, c.movedModelError())
	}

	p, ok := c.Providers[c.Provider]
	if !ok {
		return append(errs, fmt.Errorf("provider %q has no block under providers — configured: %s",
			c.Provider, strings.Join(c.labels(), ", ")))
	}

	kind := c.Kind()
	if !slices.Contains(ProviderKinds, kind) {
		return append(errs, c.unknownKindError())
	}

	if p.Model == "" {
		errs = append(errs, fmt.Errorf("providers.%s.model is empty — name the model to generate with", c.Provider))
	}

	switch kind {
	case ProviderCLI:
		if _, err := exec.LookPath(c.Bin()); err != nil {
			errs = append(errs, fmt.Errorf(
				"provider %q needs the %s binary on PATH, but it was not found\n"+
					"  install it from https://claude.com/claude-code, or pick another provider in %s",
				c.Provider, c.Bin(), configPath()))
		}
	case ProviderAnthropic, ProviderOpenAI:
		if p.BaseURL != "" {
			if err := checkURL(p.BaseURL); err != nil {
				errs = append(errs, fmt.Errorf("providers.%s.base_url: %w", c.Provider, err))
			}
		}
		// A base_url means a self-hosted or proxied endpoint, and Ollama and LM
		// Studio need no key at all — only the vendor endpoints must have one.
		if c.APIKey() == "" && (kind == ProviderAnthropic || p.BaseURL == "") {
			errs = append(errs, c.missingKeyError())
		}
	}
	return errs
}

// unknownKindError distinguishes a mistyped type from a label that was never
// given one — the second is the likelier mistake, and needs a different fix.
func (c Config) unknownKindError() error {
	kinds := strings.Join(ProviderKinds, ", ")
	if c.Active().Type == "" {
		return fmt.Errorf("providers.%s sets no type, and %q is not a built-in kind\n"+
			"  add `type:` naming one of: %s", c.Provider, c.Provider, kinds)
	}
	return fmt.Errorf("providers.%s.type %q is not one of: %s", c.Provider, c.Active().Type, kinds)
}

func (c Config) movedModelError() error {
	name := c.Provider
	if _, ok := c.Providers[name]; !ok {
		name = ProviderCLI
	}
	return fmt.Errorf("top-level 'model' moved under providers.<name>.model in v0.3\n"+
		"  in %s, replace:\n    model: %s\n  with:\n    providers:\n      %s:\n        model: %s",
		configPath(), c.Model, name, c.Model)
}

func (c Config) missingKeyError() error {
	return fmt.Errorf("provider %q needs an API key\n"+
		"  export %s, or set api_key_env / api_key under providers.%s in %s",
		c.Provider, defaultKeyEnv[c.Kind()], c.Provider, configPath())
}

func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("must be an http or https URL")
	}
	if u.Host == "" {
		return errors.New("has no host")
	}
	return nil
}

// configPath is the user config location for error messages, degrading to the
// bare filename when the home directory cannot be resolved.
func configPath() string {
	path, err := UserPath()
	if err != nil {
		return "your komit config"
	}
	return path
}

// Vars are the values substituted into a prompt template.
type Vars struct {
	Diff          string
	Files         string
	Branch        string
	RecentCommits string
}

// Render substitutes the {{...}} placeholders. Unknown ones are left as-is.
func Render(prompt string, v Vars) string {
	return strings.NewReplacer(
		"{{diff}}", v.Diff,
		"{{files}}", v.Files,
		"{{branch}}", v.Branch,
		"{{recent_commits}}", v.RecentCommits,
	).Replace(prompt)
}
