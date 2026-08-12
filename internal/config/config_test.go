package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfigHome points XDG_CONFIG_HOME at a temp dir and optionally writes a
// global config into it.
func withConfigHome(t *testing.T, contents string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	if contents == "" {
		return
	}
	dir := filepath.Join(home, "komit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

// D3: without XDG_CONFIG_HOME, UserPath falls back to ~/.config.
func TestUserPathFallsBackToHomeConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := UserPath()
	if err != nil {
		t.Fatalf("UserPath: %v", err)
	}
	want := filepath.Join(home, ".config", "komit", "config.yml")
	if got != want {
		t.Errorf("UserPath = %q, want %q", got, want)
	}
}

func TestLoadDefaultsWhenNothingConfigured(t *testing.T) {
	withConfigHome(t, "")

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model == "" {
		t.Error("Model is empty, want a built-in default")
	}
	if !strings.Contains(cfg.Prompt, "{{diff}}") {
		t.Errorf("default prompt has no {{diff}} placeholder:\n%s", cfg.Prompt)
	}
}

func TestGlobalConfigOverridesOnlyKeysItSets(t *testing.T) {
	withConfigHome(t, "model: sonnet\n")

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "sonnet" {
		t.Errorf("Model = %q, want sonnet", cfg.Model)
	}
	if cfg.Prompt != Default().Prompt {
		t.Error("Prompt was clobbered by a config that only set model")
	}
}

func TestRepoConfigOverridesGlobal(t *testing.T) {
	withConfigHome(t, "model: sonnet\nprompt: global prompt {{diff}}\n")

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".komit.yml"), []byte("prompt: repo prompt {{diff}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(repo)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Prompt != "repo prompt {{diff}}" {
		t.Errorf("Prompt = %q, want the repo one", cfg.Prompt)
	}
	if cfg.Model != "sonnet" {
		t.Errorf("Model = %q, want sonnet carried over from global", cfg.Model)
	}
}

func TestLoadMalformedYAMLFails(t *testing.T) {
	withConfigHome(t, "model: [unclosed\n")

	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

func TestRender(t *testing.T) {
	got := Render("b={{branch}} f={{files}} r={{recent_commits}} d={{diff}}", Vars{
		Diff:          "DIFF",
		Files:         "FILES",
		Branch:        "BRANCH",
		RecentCommits: "RECENT",
	})
	want := "b=BRANCH f=FILES r=RECENT d=DIFF"
	if got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestRenderLeavesUnknownPlaceholders(t *testing.T) {
	got := Render("{{nope}} {{diff}}", Vars{Diff: "D"})
	if got != "{{nope}} D" {
		t.Errorf("Render = %q", got)
	}
}
