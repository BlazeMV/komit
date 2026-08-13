// Package config resolves the prompt configuration: built-in defaults, then the
// user's global file, then the repo's .komit.yml — each overriding per key.
package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed default.yml
var defaultYAML []byte

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
// missing file is skipped; a malformed one is an error.
func Load(repoRoot string) (Config, error) {
	cfg := Default()

	userPath, err := UserPath()
	if err != nil {
		return cfg, err
	}
	for _, path := range []string{userPath, filepath.Join(repoRoot, RepoFile)} {
		if err := mergeFile(&cfg, path); err != nil {
			return cfg, err
		}
	}
	cfg.Prompt = strings.TrimSpace(cfg.Prompt)
	return cfg, nil
}

// mergeFile updates cfg from path. Missing files are skipped; malformed ones error.
func mergeFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
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
