package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BlazeMV/komit/internal/config"
)

// fakeClaude puts a stub claude on PATH so the default provider validates
// without the real CLI installed.
func fakeClaude(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.DefaultBin), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// inRepo makes a fresh git repository the working directory.
func inRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-b", "master").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(dir)
	return dir
}

// writeUserConfig puts contents at the global config path under home.
func writeUserConfig(t *testing.T, home, contents string) {
	t.Helper()
	dir := filepath.Join(home, "komit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBadConfigStopsBeforeTheTUI(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{"provider names no block", "provider: gemini\n", "has no block under providers"},
		{"v0.2 top-level model", "model: haiku\n", "moved under providers"},
		{"api provider with no key", "provider: anthropic\n", "needs an API key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ANTHROPIC_API_KEY", "")
			inRepo(t)
			fakeClaude(t)
			home := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", home)
			writeUserConfig(t, home, tt.config)

			var out, errOut bytes.Buffer
			if code := run(nil, &out, &errOut); code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if !strings.Contains(errOut.String(), tt.want) {
				t.Errorf("stderr = %q, want it to name %q", errOut.String(), tt.want)
			}
		})
	}
}

func TestVersionFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--version"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "komit") {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name    string
		linker  string
		stamped string
		want    string
	}{
		{"released tarball", "v0.2.0", "v0.2.0", "v0.2.0"},
		{"go install at a tag", "dev", "v0.2.0", "v0.2.0"},
		{"built past a tag", "dev", "v0.2.1-0.20260812160034-51b0ae8aecd2", "v0.2.1-0.20260812160034-51b0ae8aecd2"},
		{"built with local edits", "dev", "v0.2.0+dirty", "v0.2.0+dirty"},
		{"built without a repo", "dev", "(devel)", "dev"},
		{"no build info", "dev", "", "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.linker, tt.stamped); got != tt.want {
				t.Errorf("resolveVersion(%q, %q) = %q, want %q", tt.linker, tt.stamped, got, tt.want)
			}
		})
	}
}

func TestOutsideRepoExitsWithMessage(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code == 0 {
		t.Fatal("exit code = 0 outside a repository, want non-zero")
	}
	if !strings.Contains(strings.ToLower(errOut.String()), "git repository") {
		t.Errorf("stderr = %q, want an explanation", errOut.String())
	}
}

func TestInitWritesDefaultConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	var out, errOut bytes.Buffer
	if code := run([]string{"init"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}

	path := filepath.Join(home, "komit", "config.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !strings.Contains(string(data), "{{diff}}") {
		t.Errorf("written config has no prompt:\n%s", data)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("stdout = %q, want the written path", out.String())
	}
}

func TestInitWritesEveryDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	var out, errOut bytes.Buffer
	if code := run([]string{"init"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}

	// Load reads the file just written, so a missing key shows up as a default.
	got, _, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, config.Default()) {
		t.Errorf("round-tripped config = %+v, want the defaults %+v", got, config.Default())
	}

	data, err := os.ReadFile(filepath.Join(home, "komit", "config.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"refresh:", "providers:", "provider: " + config.ProviderCLI} {
		if !strings.Contains(string(data), want) {
			t.Errorf("written config has no %q:\n%s", want, data)
		}
	}
}

// A config komit writes must be one komit will start with.
func TestInitWritesAConfigThatValidates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	fakeClaude(t)

	var out, errOut bytes.Buffer
	if code := run([]string{"init"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}

	cfg, _, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if errs := cfg.Validate(); len(errs) != 0 {
		t.Errorf("the written config does not validate: %v", errs)
	}
}

func TestInitDoesNotClobberExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	dir := filepath.Join(home, "komit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("model: mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"init"}, &out, &errOut); code == 0 {
		t.Error("exit code = 0, want non-zero when the config already exists")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "model: mine\n" {
		t.Errorf("existing config was overwritten: %q", data)
	}
}

func TestSecondInitLeavesTheFirstConfigIntact(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	var out1, errOut1 bytes.Buffer
	if code := run([]string{"init"}, &out1, &errOut1); code != 0 {
		t.Fatalf("first init failed: exit code = %d, stderr = %q", code, errOut1.String())
	}

	path := filepath.Join(home, "komit", "config.yml")
	data1, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config after first init: %v", err)
	}

	var out2, errOut2 bytes.Buffer
	if code := run([]string{"init"}, &out2, &errOut2); code == 0 {
		t.Error("second init should fail, got exit code = 0")
	}

	data2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config after second init: %v", err)
	}

	if string(data1) != string(data2) {
		t.Errorf("config was modified by failed second init:\nbefore: %q\nafter: %q", string(data1), string(data2))
	}
}

func TestInitLocalWritesRepoConfig(t *testing.T) {
	dir := inRepo(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out, errOut bytes.Buffer
	if code := run([]string{"init", "--local"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}

	path := filepath.Join(dir, config.RepoFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !strings.Contains(string(data), "{{diff}}") {
		t.Errorf("written config has no prompt:\n%s", data)
	}
	if !strings.Contains(out.String(), config.RepoFile) {
		t.Errorf("stdout = %q, want the written path", out.String())
	}
}

// The template must not model the keys komit will refuse to read from it.
func TestInitLocalWritesNoBackendKeys(t *testing.T) {
	inRepo(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out, errOut bytes.Buffer
	if code := run([]string{"init", "--local"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}

	cfg, warnings, err := config.Load(".")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want a template komit reads without complaint", warnings)
	}
	if cfg.Provider != config.Default().Provider {
		t.Errorf("the template changed the provider to %q", cfg.Provider)
	}
}

func TestInitLocalDoesNotClobberExistingConfig(t *testing.T) {
	dir := inRepo(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := filepath.Join(dir, config.RepoFile)
	if err := os.WriteFile(path, []byte("prompt: mine {{diff}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"init", "--local"}, &out, &errOut); code == 0 {
		t.Error("exit code = 0, want non-zero when .komit.yml already exists")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "prompt: mine {{diff}}\n" {
		t.Errorf("existing .komit.yml was overwritten: %q", data)
	}
}

func TestInitLocalOutsideARepoFails(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out, errOut bytes.Buffer
	if code := run([]string{"init", "--local"}, &out, &errOut); code == 0 {
		t.Fatal("exit code = 0 outside a repository, want non-zero")
	}
	if !strings.Contains(strings.ToLower(errOut.String()), "git repository") {
		t.Errorf("stderr = %q, want an explanation", errOut.String())
	}
}

func TestInitRejectsUnknownFlag(t *testing.T) {
	inRepo(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out, errOut bytes.Buffer
	if code := run([]string{"init", "--globl"}, &out, &errOut); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "--local") {
		t.Errorf("stderr = %q, want the usage line", errOut.String())
	}
}

func TestHelpFlag(t *testing.T) {
	// Outside a repository, so help cannot depend on one.
	t.Chdir(t.TempDir())

	for _, arg := range []string{"--help", "-h", "help"} {
		t.Run(arg, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run([]string{arg}, &out, &errOut); code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
			}
			for _, want := range []string{"usage:", "komit init", "--local", "--version"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("help output missing %q:\n%s", want, out.String())
				}
			}
			if errOut.Len() != 0 {
				t.Errorf("stderr = %q, want help on stdout", errOut.String())
			}
		})
	}
}

func TestVersionAliases(t *testing.T) {
	for _, arg := range []string{"--version", "-v", "version"} {
		t.Run(arg, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run([]string{arg}, &out, &errOut); code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
			}
			if !strings.Contains(out.String(), "komit") {
				t.Errorf("stdout = %q", out.String())
			}
		})
	}
}

func TestUnknownArgumentPointsAtHelp(t *testing.T) {
	t.Chdir(t.TempDir())

	var out, errOut bytes.Buffer
	if code := run([]string{"--bogus"}, &out, &errOut); code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "--help") {
		t.Errorf("stderr = %q, want it to point at --help", errOut.String())
	}
}
