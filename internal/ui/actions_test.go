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

func TestCommitWithNothingSelectedIsRefused(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m = update(m, key("a")) // toggle all -> everything already selected, so clears
	if len(m.selectedPaths()) != 0 {
		t.Fatalf("precondition: expected empty selection, got %v", m.selectedPaths())
	}
	m.msgInput.SetValue("a message")

	m = update(m, key("c"))
	if !strings.Contains(m.View().Content, "no files selected") {
		t.Errorf("view does not explain the refusal:\n%s", m.View().Content)
	}
}

func TestGenerateRefusalClearsStaleError(t *testing.T) {
	m := newTestModel(t, &fakeRunner{out: "x"})
	m = update(m, errMsg{err: errFake{}})
	if !strings.Contains(m.View().Content, "boom") {
		t.Fatal("precondition: error not shown")
	}

	m = update(m, key("a")) // toggle all -> everything already selected, so clears
	if len(m.selectedPaths()) != 0 {
		t.Fatalf("precondition: expected empty selection, got %v", m.selectedPaths())
	}
	m = update(m, key("g"))

	out := m.View().Content
	if strings.Contains(out, "boom") {
		t.Errorf("stale error masks the refusal:\n%s", out)
	}
	if !strings.Contains(out, "no files selected") {
		t.Errorf("view does not show the refusal:\n%s", out)
	}
}

func TestSecondGenerateWhileBusyIsIgnored(t *testing.T) {
	m := newTestModel(t, &fakeRunner{out: "x"})

	next, cmd1 := m.Update(key("g"))
	if cmd1 == nil {
		t.Fatal("first g produced no command")
	}
	busy := next.(Model)
	if !busy.busy {
		t.Fatal("model not busy after first g")
	}

	_, cmd2 := busy.Update(key("g"))
	if cmd2 != nil {
		t.Error("second g while busy started another generation")
	}
}

func TestEscDuringCommitIgnoresStaleGenerationCancel(t *testing.T) {
	runner := &fakeRunner{out: "feat: thing"}
	m := newTestModel(t, runner)

	genNext, genCmd := m.Update(key("g"))
	m = genNext.(Model)
	if m.cancel == nil {
		t.Fatal("precondition: cancel should be set while generation is in flight")
	}
	m = update(m, drain(t, genCmd))
	if m.cancel != nil {
		t.Fatal("precondition: cancel should be cleared once generation finishes")
	}

	next, cmd := m.Update(key("c"))
	busy := next.(Model)
	if cmd == nil {
		t.Fatal("c produced no command")
	}
	if !busy.busy {
		t.Fatal("precondition: model not busy while commit is in flight")
	}

	afterEsc := update(busy, key("esc"))
	if !afterEsc.busy {
		t.Error("esc cancelled an in-flight commit via a stale generation CancelFunc")
	}
	if strings.Contains(afterEsc.View().Content, "cancelled") {
		t.Errorf("view falsely reports the commit as cancelled:\n%s", afterEsc.View().Content)
	}
}

func TestCommitRunsAndOnlySelectedPathsLand(t *testing.T) {
	dir := gitInit(t)
	writeRepoFile(t, dir, "a.go", "1\n")
	writeRepoFile(t, dir, "b.go", "1\n")
	commitAllRepo(t, dir, "init")
	writeRepoFile(t, dir, "a.go", "2\n")
	writeRepoFile(t, dir, "b.go", "2\n")

	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(repo, config.Config{}, &fakeRunner{})
	m.width, m.height = 100, 30
	m = update(m, statusMsg{
		files: []git.FileChange{
			{Path: "a.go", Index: ' ', Worktree: 'M'},
			{Path: "b.go", Index: ' ', Worktree: 'M'},
		},
		branch: git.Branch{Name: "master"},
	})
	m.items[1].selected = false // deselect b.go
	m.msgInput.SetValue("change a")

	_, cmd := m.Update(key("c"))
	if cmd == nil {
		t.Fatal("c produced no command")
	}
	msg := drain(t, cmd)
	cm, ok := msg.(committedMsg)
	if !ok {
		t.Fatalf("msg = %#v, want committedMsg", msg)
	}
	if !strings.Contains(cm.summary, "1 file") {
		t.Errorf("summary = %q, want it to name 1 file", cm.summary)
	}

	touched := gitRun(t, dir, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(touched, "a.go") || strings.Contains(touched, "b.go") {
		t.Errorf("HEAD touched %q, want only a.go", touched)
	}
	status := gitRun(t, dir, "status", "--porcelain")
	if !strings.Contains(status, "b.go") {
		t.Errorf("status = %q, want b.go still modified and uncommitted", status)
	}
}

func TestCommitUntrackedFileIsCommittedAndOthersStayUntracked(t *testing.T) {
	dir := gitInit(t)
	writeRepoFile(t, dir, "a.go", "1\n")
	commitAllRepo(t, dir, "init")
	writeRepoFile(t, dir, "new.go", "package main\n")
	writeRepoFile(t, dir, "other.go", "package other\n")

	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(repo, config.Config{}, &fakeRunner{})
	m.width, m.height = 100, 30
	m = update(m, statusMsg{
		files: []git.FileChange{
			{Path: "new.go", Index: '?', Worktree: '?'},
			{Path: "other.go", Index: '?', Worktree: '?'},
		},
		branch: git.Branch{Name: "master"},
	})
	m.items[1].selected = false // other.go stays unselected and untracked
	m.msgInput.SetValue("add new")

	_, cmd := m.Update(key("c"))
	if cmd == nil {
		t.Fatal("c produced no command")
	}
	msg := drain(t, cmd)
	if _, ok := msg.(committedMsg); !ok {
		t.Fatalf("msg = %#v, want committedMsg", msg)
	}

	if got := gitRun(t, dir, "show", "HEAD:new.go"); !strings.Contains(got, "package main") {
		t.Errorf("HEAD:new.go = %q", got)
	}
	status := gitRun(t, dir, "status", "--porcelain")
	if !strings.Contains(status, "?? other.go") {
		t.Errorf("status = %q, want other.go still fully untracked (no leftover intent-to-add)", status)
	}
}

func TestFailedCommitWithUntrackedSelectedCleansUpIndex(t *testing.T) {
	dir := gitInit(t)
	writeRepoFile(t, dir, "a.go", "1\n")
	commitAllRepo(t, dir, "init")
	writeHook(t, dir, "pre-commit", "echo 'hook says no' >&2\nexit 1")
	writeRepoFile(t, dir, "new.go", "package main\n")

	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(repo, config.Config{}, &fakeRunner{})
	m.width, m.height = 100, 30
	m = update(m, statusMsg{
		files:  []git.FileChange{{Path: "new.go", Index: '?', Worktree: '?'}},
		branch: git.Branch{Name: "master"},
	})
	m.msgInput.SetValue("add new")

	_, cmd := m.Update(key("c"))
	if cmd == nil {
		t.Fatal("c produced no command")
	}
	msg := drain(t, cmd)
	if _, ok := msg.(errMsg); !ok {
		t.Fatalf("msg = %#v, want errMsg from the failing hook", msg)
	}

	status := gitRun(t, dir, "status", "--porcelain")
	if !strings.Contains(status, "?? new.go") {
		t.Errorf("status = %q, want new.go untracked again after the failed commit", status)
	}
}

func TestAmendGenerateUsesDiffAmendNotDiff(t *testing.T) {
	dir := gitInit(t)
	writeRepoFile(t, dir, "a.go", "one\n")
	commitAllRepo(t, dir, "init")
	writeRepoFile(t, dir, "a.go", "one\ntwo\n")
	commitAllRepo(t, dir, "add two") // the commit amend will target
	writeRepoFile(t, dir, "a.go", "one\ntwo\nthree\n")

	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{out: "feat: amended"}
	m := New(repo, config.Config{Model: "haiku", Prompt: "{{diff}}"}, runner)
	m.width, m.height = 100, 30
	m = update(m, statusMsg{
		files:  []git.FileChange{{Path: "a.go", Index: ' ', Worktree: 'M'}},
		branch: git.Branch{Name: "master"},
	})
	m.amend = true

	_, cmd := m.Update(key("g"))
	if cmd == nil {
		t.Fatal("g produced no command")
	}
	msg := drain(t, cmd)
	if _, ok := msg.(generatedMsg); !ok {
		t.Fatalf("msg = %#v, want generatedMsg", msg)
	}
	// "two" only shows as an added line (+two) against HEAD~1 (DiffAmend); a
	// plain Diff against HEAD would only add "three", with "two" as context.
	if !strings.Contains(runner.prompt, "+two") {
		t.Errorf("prompt = %q, want the amended commit's own change (DiffAmend, not Diff)", runner.prompt)
	}
}
