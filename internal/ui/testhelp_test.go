package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/BlazeMV/komit/internal/git"
)

// newUIRepo builds a temp repo with one committed, then modified, file.
func newUIRepo(t *testing.T) *git.Repo {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "master"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// gitInit builds a bare working repo (identity configured, no commits) for
// tests that need more control over history than newUIRepo provides.
func gitInit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "master"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		gitRun(t, dir, args...)
	}
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeRepoFile(t *testing.T, dir, path, content string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commitAllRepo stages everything and commits, giving the repo a HEAD.
func commitAllRepo(t *testing.T, dir, msg string) {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", msg)
}

// writeHook installs an executable git hook; script runs under /bin/sh.
func writeHook(t *testing.T, dir, name, script string) {
	t.Helper()
	hook := filepath.Join(dir, ".git", "hooks", name)
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// drain runs a command (following one level of batch) and returns its message.
func drain(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			if m := c(); m != nil {
				if _, isTick := m.(spinnerTick); !isTick {
					return m
				}
			}
		}
	}
	return msg
}
