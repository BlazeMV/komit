package ui

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/BlazeMV/komit/internal/config"
	"github.com/BlazeMV/komit/internal/git"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

type e2eRunner struct{}

func (e2eRunner) Run(context.Context, string) (string, error) {
	return "feat: end to end\n", nil
}

func TestSelectGenerateCommitFlow(t *testing.T) {
	repo := newUIRepo(t) // one committed file, modified in the working tree
	m := New(repo, config.Config{Prompt: "{{diff}}"}, e2eRunner{}, nil)

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "a.go")
	}, teatest.WithDuration(3*time.Second))

	tm.Send(tea.KeyPressMsg{Code: 'g', Text: "g"})

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "feat: end to end")
	}, teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyPressMsg{Code: 'c', Text: "c"})

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "committed")
	}, teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
	io.ReadAll(tm.FinalOutput(t))

	subject, err := exec.Command("git", "-C", repo.Dir, "log", "-1", "--format=%s").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if strings.TrimSpace(string(subject)) != "feat: end to end" {
		t.Errorf("HEAD subject = %q, want the generated message", strings.TrimSpace(string(subject)))
	}
}

// newPushedUIRepo pushes HEAD to a bare remote before dirtying the tree:
// Init's loadStatus overwrites a headPushed field poked in directly by a test.
func newPushedUIRepo(t *testing.T) *git.Repo {
	t.Helper()
	dir := gitInit(t)
	writeRepoFile(t, dir, "a.go", "one\n")
	commitAllRepo(t, dir, "init")

	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare", "--quiet")
	gitRun(t, dir, "remote", "add", "origin", remote)
	gitRun(t, dir, "push", "--quiet", "-u", "origin", "master")

	writeRepoFile(t, dir, "a.go", "one\ntwo\n")
	r, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAmendRefusalFlow(t *testing.T) {
	repo := newPushedUIRepo(t)
	m := New(repo, config.Config{Prompt: "{{diff}}"}, e2eRunner{}, nil)

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "a.go")
	}, teatest.WithDuration(3*time.Second))

	tm.Send(tea.KeyPressMsg{Code: 'A', Text: "A"})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "already pushed")
	}, teatest.WithDuration(3*time.Second))

	tm.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}
