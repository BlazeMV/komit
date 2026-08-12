package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--version"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "komit") {
		t.Errorf("stdout = %q", out.String())
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
