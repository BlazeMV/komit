package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// withRepoConfig writes a .komit.yml into a fresh temp repo root.
func withRepoConfig(t *testing.T, contents string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, RepoFile), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// fakeBin puts an executable named name on PATH so LookPath finds it.
func fakeBin(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// noKeysInEnv clears the vendor variables so a developer's own exported key
// cannot make a "missing key" case pass.
func noKeysInEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY"} {
		t.Setenv(k, "")
	}
}

func load(t *testing.T, repoRoot string) Config {
	t.Helper()
	cfg, _, err := Load(repoRoot)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
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

	cfg := load(t, t.TempDir())
	if cfg.Provider != ProviderCLI {
		t.Errorf("Provider = %q, want %q", cfg.Provider, ProviderCLI)
	}
	if cfg.Active().Model == "" {
		t.Error("the default provider has no model")
	}
	if !strings.Contains(cfg.Prompt, "{{diff}}") {
		t.Errorf("default prompt has no {{diff}} placeholder:\n%s", cfg.Prompt)
	}
}

func TestEveryProviderHasADefaultModel(t *testing.T) {
	cfg := Default()
	for _, name := range ProviderKinds {
		if cfg.Providers[name].Model == "" {
			t.Errorf("provider %q has no default model", name)
		}
	}
}

func TestGlobalConfigOverridesOnlyKeysItSets(t *testing.T) {
	withConfigHome(t, "providers:\n  claude-cli:\n    model: sonnet\n")

	cfg := load(t, t.TempDir())
	if got := cfg.Active().Model; got != "sonnet" {
		t.Errorf("model = %q, want sonnet", got)
	}
	if cfg.Prompt != Default().Prompt {
		t.Error("Prompt was clobbered by a config that only set a model")
	}
}

func TestRepoConfigOverridesGlobal(t *testing.T) {
	withConfigHome(t, "providers:\n  claude-cli:\n    model: sonnet\nprompt: global prompt {{diff}}\n")
	repo := withRepoConfig(t, "prompt: repo prompt {{diff}}\n")

	cfg := load(t, repo)
	if cfg.Prompt != "repo prompt {{diff}}" {
		t.Errorf("Prompt = %q, want the repo one", cfg.Prompt)
	}
	if got := cfg.Active().Model; got != "sonnet" {
		t.Errorf("model = %q, want sonnet carried over from global", got)
	}
}

// Unmarshalling into a live map replaces whole blocks, so a repo file setting
// one key must not wipe base_url and api_key from the global config.
func TestProviderBlockMergeKeepsSiblingKeys(t *testing.T) {
	withConfigHome(t, "provider: openai\nproviders:\n  openai:\n    model: gpt-5-mini\n    base_url: https://example.test/v1\n    api_key: sk-global\n")
	repo := withRepoConfig(t, "providers:\n  openai:\n    model: gpt-5\n")

	cfg := load(t, repo)
	p := cfg.Active()
	if p.Model != "gpt-5" {
		t.Errorf("model = %q, want the repo override", p.Model)
	}
	if p.BaseURL != "https://example.test/v1" {
		t.Errorf("base_url = %q, want the global value kept", p.BaseURL)
	}
	if p.APIKey != "sk-global" {
		t.Errorf("api_key = %q, want the global value kept", p.APIKey)
	}
}

func TestRepoConfigCannotContributeAnAPIKey(t *testing.T) {
	noKeysInEnv(t)
	withConfigHome(t, "provider: openai\n")
	repo := withRepoConfig(t, "providers:\n  openai:\n    api_key: sk-committed\n")

	cfg, warnings, err := Load(repo)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Active().APIKey; got != "" {
		t.Errorf("api_key = %q, want the repo file's key dropped", got)
	}
	if cfg.APIKey() != "" {
		t.Errorf("APIKey() = %q, want empty", cfg.APIKey())
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "api_key") {
		t.Errorf("warnings = %v, want one naming api_key", warnings)
	}
}

func TestUserConfigMayHoldAnAPIKey(t *testing.T) {
	noKeysInEnv(t)
	withConfigHome(t, "provider: openai\nproviders:\n  openai:\n    api_key: sk-mine\n")

	cfg, warnings, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIKey() != "sk-mine" {
		t.Errorf("APIKey() = %q, want sk-mine", cfg.APIKey())
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
}

func TestAPIKeyResolution(t *testing.T) {
	tests := []struct {
		name   string
		config string
		env    map[string]string
		want   string
	}{
		{
			name:   "vendor variable",
			config: "provider: openai\n",
			env:    map[string]string{"OPENAI_API_KEY": "sk-env"},
			want:   "sk-env",
		},
		{
			name:   "environment beats the config value",
			config: "provider: openai\nproviders:\n  openai:\n    api_key: sk-file\n",
			env:    map[string]string{"OPENAI_API_KEY": "sk-env"},
			want:   "sk-env",
		},
		{
			name:   "config value when nothing is exported",
			config: "provider: openai\nproviders:\n  openai:\n    api_key: sk-file\n",
			want:   "sk-file",
		},
		{
			name:   "api_key_env names the variable",
			config: "provider: openai\nproviders:\n  openai:\n    api_key_env: OPENROUTER_API_KEY\n",
			env:    map[string]string{"OPENROUTER_API_KEY": "sk-or"},
			want:   "sk-or",
		},
		{
			name:   "api_key_env opts out of the vendor variable",
			config: "provider: openai\nproviders:\n  openai:\n    api_key_env: OPENROUTER_API_KEY\n",
			env:    map[string]string{"OPENAI_API_KEY": "sk-wrong"},
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			noKeysInEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			withConfigHome(t, tt.config)

			if got := load(t, t.TempDir()).APIKey(); got != tt.want {
				t.Errorf("APIKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A label picks the block; type picks the implementation. Two OpenAI-compatible
// endpoints must be able to coexist, which is the whole point of labels.
func TestLabelledBlocksCoexist(t *testing.T) {
	noKeysInEnv(t)
	withConfigHome(t, `provider: ollama
providers:
  openai:
    model: gpt-5.6-luna
    api_key: sk-openai
  ollama:
    type: openai
    model: qwen3
    base_url: http://localhost:11434/v1
  openrouter:
    type: openai
    model: anthropic/claude-haiku-4.5
    api_key: sk-or
`)

	cfg := load(t, t.TempDir())
	if got := cfg.Kind(); got != ProviderOpenAI {
		t.Errorf("Kind() = %q, want %q", got, ProviderOpenAI)
	}
	if got := cfg.Active().Model; got != "qwen3" {
		t.Errorf("model = %q, want the ollama block's", got)
	}
	if got := cfg.BaseURL(); got != "http://localhost:11434/v1" {
		t.Errorf("BaseURL() = %q", got)
	}
	if got := cfg.APIKey(); got != "" {
		t.Errorf("APIKey() = %q, want the ollama block to carry no key", got)
	}
	if errs := cfg.Validate(); len(errs) != 0 {
		t.Errorf("Validate = %v, want no problems", errs)
	}

	// Switching backends is one word, and the other blocks are untouched.
	cfg.Provider = "openrouter"
	if got := cfg.APIKey(); got != "sk-or" {
		t.Errorf("after switching, APIKey() = %q, want sk-or", got)
	}
	if got := cfg.BaseURL(); got != OpenAIBaseURL {
		t.Errorf("after switching, BaseURL() = %q, want the vendor default", got)
	}
}

// A label is never read as a kind, even when it happens to name one.
func TestTypeIsRequiredAndNeverInferredFromTheLabel(t *testing.T) {
	noKeysInEnv(t)
	cfg := Config{
		Provider:  ProviderAnthropic,
		Providers: map[string]Provider{ProviderAnthropic: {Model: "claude-haiku-4-5", APIKey: "sk-x"}},
	}

	if got := cfg.Kind(); got != "" {
		t.Errorf("Kind() = %q, want empty until type is set", got)
	}
	errs := cfg.Validate()
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "type is required") {
		t.Fatalf("Validate = %v, want one problem naming the missing type", errs)
	}
}

// A bad block you have not switched to still has to be reported.
func TestValidateChecksEveryBlockNotJustTheActiveOne(t *testing.T) {
	noKeysInEnv(t)
	fakeBin(t, DefaultBin)
	withConfigHome(t, "providers:\n  ollama:\n    model: qwen3\n  broken:\n    type: openai-compatible\n    model: x\n")

	var joined []string
	for _, e := range load(t, t.TempDir()).Validate() {
		joined = append(joined, e.Error())
	}
	all := strings.Join(joined, "\n")
	if !strings.Contains(all, "providers.ollama.type is required") {
		t.Errorf("Validate = %v, want the untyped inactive block reported", joined)
	}
	if !strings.Contains(all, `providers.broken.type "openai-compatible"`) {
		t.Errorf("Validate = %v, want the mistyped inactive block reported", joined)
	}
}

func TestBaseURL(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{"anthropic default", "provider: anthropic\n", AnthropicBaseURL},
		{"openai default", "provider: openai\n", OpenAIBaseURL},
		{"cli has none", "provider: claude-cli\n", ""},
		{
			name:   "override wins",
			config: "provider: openai\nproviders:\n  openai:\n    base_url: http://localhost:11434/v1\n",
			want:   "http://localhost:11434/v1",
		},
		{
			name:   "trailing slash trimmed",
			config: "provider: openai\nproviders:\n  openai:\n    base_url: http://localhost:11434/v1/\n",
			want:   "http://localhost:11434/v1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withConfigHome(t, tt.config)
			if got := load(t, t.TempDir()).BaseURL(); got != tt.want {
				t.Errorf("BaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		config string
		env    map[string]string
		noBin  bool
		want   string // substring of the reported problem; empty means valid
	}{
		{name: "default config with the CLI present"},
		{name: "missing claude binary", config: "provider: claude-cli\n", noBin: true, want: "on PATH"},
		{name: "provider names no block", config: "provider: gemini\n", want: "no block under providers"},
		{
			name:   "block with no type",
			config: "provider: ollama\nproviders:\n  ollama:\n    model: qwen3\n",
			want:   "type is required",
		},
		{
			name:   "mistyped type",
			config: "provider: ollama\nproviders:\n  ollama:\n    type: openai-compatible\n    model: qwen3\n",
			want:   "is not one of",
		},
		{
			name:   "labelled openai block against a local server",
			config: "provider: ollama\nproviders:\n  ollama:\n    type: openai\n    model: qwen3\n    base_url: http://localhost:11434/v1\n",
		},
		{
			name:   "anthropic without a key",
			config: "provider: anthropic\n",
			want:   "needs an API key",
		},
		{
			name:   "anthropic with a key",
			config: "provider: anthropic\n",
			env:    map[string]string{"ANTHROPIC_API_KEY": "sk-ant"},
		},
		{
			name:   "anthropic still needs a key behind a proxy",
			config: "provider: anthropic\nproviders:\n  anthropic:\n    base_url: https://proxy.test\n",
			want:   "needs an API key",
		},
		{
			name:   "openai without a key",
			config: "provider: openai\n",
			want:   "needs an API key",
		},
		{
			name:   "openai against a local server needs no key",
			config: "provider: openai\nproviders:\n  openai:\n    base_url: http://localhost:11434/v1\n",
		},
		{
			name:   "unparseable base_url",
			config: "provider: openai\nproviders:\n  openai:\n    base_url: localhost:11434\n    api_key: sk-x\n",
			want:   "base_url",
		},
		{
			name:   "v0.2 top-level model",
			config: "model: haiku\n",
			want:   "moved under providers",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			noKeysInEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if tt.noBin {
				t.Setenv("PATH", t.TempDir())
			} else {
				fakeBin(t, DefaultBin)
			}
			withConfigHome(t, tt.config)

			errs := load(t, t.TempDir()).Validate()
			if tt.want == "" {
				if len(errs) != 0 {
					t.Fatalf("Validate = %v, want no problems", errs)
				}
				return
			}
			if len(errs) == 0 {
				t.Fatalf("Validate found nothing, want a problem naming %q", tt.want)
			}
			var joined []string
			for _, e := range errs {
				joined = append(joined, e.Error())
			}
			if !strings.Contains(strings.Join(joined, "\n"), tt.want) {
				t.Errorf("Validate = %v, want a problem naming %q", joined, tt.want)
			}
		})
	}
}

// A blank model cannot be reached through Load — an empty override means unset,
// so the built-in default survives — but the rule guards the runner all the same.
func TestValidateRejectsAProviderWithNoModel(t *testing.T) {
	noKeysInEnv(t)
	cfg := Config{
		Provider:  ProviderOpenAI,
		Providers: map[string]Provider{ProviderOpenAI: {Type: ProviderOpenAI, APIKey: "sk-x"}},
	}

	errs := cfg.Validate()
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "model is empty") {
		t.Errorf("Validate = %v, want one problem naming the empty model", errs)
	}
}

// The rewrite is the whole point of keeping the dead key, so it must name the
// value the user actually had.
func TestValidateShowsTheModelRewrite(t *testing.T) {
	fakeBin(t, DefaultBin)
	withConfigHome(t, "model: sonnet\n")

	errs := load(t, t.TempDir()).Validate()
	if len(errs) == 0 {
		t.Fatal("Validate found nothing")
	}
	msg := errs[0].Error()
	for _, want := range []string{"providers", ProviderCLI, "sonnet"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not mention %q", msg, want)
		}
	}
}

func TestDefaultRefreshPollsAndFollowsFocus(t *testing.T) {
	r := Default().Refresh
	if !r.OnFocus {
		t.Error("on_focus is off by default")
	}
	if r.Every() != 10*time.Second {
		t.Errorf("Every = %v, want 10s", r.Every())
	}
}

func TestRefreshOverridesOnlyKeysItSets(t *testing.T) {
	withConfigHome(t, "refresh:\n  interval: 30\n")

	cfg := load(t, t.TempDir())
	if cfg.Refresh.Every() != 30*time.Second {
		t.Errorf("Every = %v, want 30s", cfg.Refresh.Every())
	}
	if !cfg.Refresh.OnFocus {
		t.Error("on_focus was clobbered by a config that only set interval")
	}
}

func TestRefreshCanBeTurnedOffEntirely(t *testing.T) {
	withConfigHome(t, "refresh:\n  on_focus: false\n  interval: 0\n")

	cfg := load(t, t.TempDir())
	if cfg.Refresh.OnFocus {
		t.Error("on_focus stayed on")
	}
	if cfg.Refresh.Every() != 0 {
		t.Errorf("Every = %v, want 0 to disable polling", cfg.Refresh.Every())
	}
}

// interval: 0 must stay distinguishable from an absent interval, which is why
// the top-level keys merge by unmarshalling rather than field by field.
func TestRepoConfigCanDisablePolling(t *testing.T) {
	withConfigHome(t, "refresh:\n  interval: 30\n")
	repo := withRepoConfig(t, "refresh:\n  interval: 0\n")

	cfg := load(t, repo)
	if cfg.Refresh.Every() != 0 {
		t.Errorf("Every = %v, want the repo file to disable polling", cfg.Refresh.Every())
	}
	if !cfg.Refresh.OnFocus {
		t.Error("on_focus was clobbered by a config that only set interval")
	}
}

func TestRefreshEveryTreatsNegativeAsOff(t *testing.T) {
	if got := (Refresh{Interval: -5}).Every(); got != 0 {
		t.Errorf("Every = %v, want 0", got)
	}
}

func TestLoadMalformedYAMLFails(t *testing.T) {
	withConfigHome(t, "provider: [unclosed\n")

	if _, _, err := Load(t.TempDir()); err == nil {
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
