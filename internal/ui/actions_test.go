package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/BlazeMV/komit/internal/config"
	"github.com/BlazeMV/komit/internal/git"
)

type fakeRunner struct {
	prompt string
	out    string
	err    error
}

func (f *fakeRunner) Run(_ context.Context, _ string, prompt string) (string, error) {
	f.prompt = prompt
	return f.out, f.err
}

func newTestModel(t *testing.T, runner *fakeRunner) Model {
	t.Helper()
	repo := newUIRepo(t) // helper below: temp git repo with two modified files
	m := New(repo, config.Config{Model: "haiku", Prompt: "{{diff}}"}, runner)
	m.width, m.height = 100, 30
	m = update(m, statusMsg{
		files:  []git.FileChange{{Path: "a.go", Index: ' ', Worktree: 'M'}},
		branch: git.Branch{Name: "master"},
	})
	return m
}

func TestGenerateFillsMessageFromRunner(t *testing.T) {
	runner := &fakeRunner{out: "feat: from runner"}
	m := newTestModel(t, runner)

	_, cmd := m.Update(key("g"))
	if cmd == nil {
		t.Fatal("g produced no command")
	}
	msg := drain(t, cmd) // runs the command, returns the final tea.Msg
	m = update(m, msg)

	if m.message() != "feat: from runner" {
		t.Errorf("message = %q", m.message())
	}
	if !strings.Contains(runner.prompt, "a.go") && !strings.Contains(runner.prompt, "diff") {
		t.Errorf("prompt does not contain the diff: %q", runner.prompt)
	}
}

func TestGenerateWithNothingSelectedIsRefused(t *testing.T) {
	m := newTestModel(t, &fakeRunner{out: "x"})
	m = update(m, key("a")) // toggle all -> everything already selected, so clears
	if len(m.selectedPaths()) != 0 {
		t.Fatalf("precondition: expected empty selection, got %v", m.selectedPaths())
	}

	m = update(m, key("g"))
	if !strings.Contains(m.View().Content, "no files selected") {
		t.Errorf("view does not explain the refusal:\n%s", m.View().Content)
	}
}

func TestCommitWithEmptyMessageIsRefused(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = update(m, key("c"))
	if !strings.Contains(strings.ToLower(m.View().Content), "message") {
		t.Errorf("view does not explain the refusal:\n%s", m.View().Content)
	}
}

func TestCommitClearsMessageAndRefreshes(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = update(m, generatedMsg{message: "feat: thing"})
	m = update(m, committedMsg{summary: "committed 1 file"})

	if m.message() != "" {
		t.Errorf("message = %q, want cleared after commit", m.message())
	}
	if !strings.Contains(m.View().Content, "committed 1 file") {
		t.Errorf("view missing the commit confirmation:\n%s", m.View().Content)
	}
}

func TestAmendRefusedWhenHeadIsPushed(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m.headPushed = true

	m = update(m, key("A"))
	if m.amend {
		t.Error("amend mode enabled on a pushed HEAD")
	}
	if !strings.Contains(m.View().Content, "already pushed") {
		t.Errorf("view does not explain the refusal:\n%s", m.View().Content)
	}
}

func TestAmendToggles(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = update(m, key("A"))
	if !m.amend {
		t.Fatal("amend not enabled")
	}
	if !strings.Contains(m.View().Content, "AMEND") {
		t.Error("view does not show the amend banner")
	}
	m = update(m, key("A"))
	if m.amend {
		t.Error("amend did not toggle off")
	}
}

func TestGenerationErrorIsShownAndNotFatal(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = update(m, errMsg{err: errFake{}})
	if !strings.Contains(m.View().Content, "boom") {
		t.Error("error not displayed")
	}
	if len(m.items) == 0 {
		t.Error("file list lost on error")
	}
}
