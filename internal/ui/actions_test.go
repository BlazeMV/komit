package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/BlazeMV/komit/internal/ai"
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

	next, cmd := m.Update(key("g"))
	m = next.(Model)
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

// C: claude missing from PATH gets an install hint, not a bare error.
func TestGenerationMissingClaudeShowsInstallHint(t *testing.T) {
	m := newTestModel(t, &fakeRunner{err: ai.ErrMissing})

	next, cmd := m.Update(key("g"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("g produced no command")
	}
	m = update(m, drain(t, cmd))

	out := m.View().Content
	if !strings.Contains(out, "not found on PATH") || !strings.Contains(out, "install") {
		t.Errorf("view does not show the missing-claude install hint:\n%s", out)
	}
	if !strings.Contains(out, "$EDITOR") {
		t.Errorf("view does not mention writing the message by hand:\n%s", out)
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

// D8: g, r, c and P must all be refused while busy, not just g.
func TestBusyGuardBlocksAllBusyGuardedKeys(t *testing.T) {
	for _, k := range []string{"g", "r", "c", "P"} {
		t.Run(k, func(t *testing.T) {
			m := newTestModel(t, &fakeRunner{out: "x"})

			next, cmd1 := m.Update(key("g"))
			if cmd1 == nil {
				t.Fatal("g produced no command")
			}
			busy := next.(Model)
			if !busy.busy {
				t.Fatal("model not busy after g")
			}

			_, cmd2 := busy.Update(key(k))
			if cmd2 != nil {
				t.Errorf("%s while busy produced a command, want it refused", k)
			}
		})
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

// D9: E must be refused while busy — a generation finishing mid-edit would
// otherwise overwrite what the user just typed.
func TestEditorRefusedWhileBusy(t *testing.T) {
	m := newTestModel(t, &fakeRunner{out: "x"})

	next, cmd1 := m.Update(key("g"))
	if cmd1 == nil {
		t.Fatal("g produced no command")
	}
	busy := next.(Model)
	if !busy.busy {
		t.Fatal("model not busy after g")
	}

	after, cmd2 := busy.Update(key("E"))
	if cmd2 != nil {
		t.Error("E opened the editor while busy, want it refused")
	}
	am := after.(Model)
	if !strings.Contains(am.View().Content, "generat") {
		t.Errorf("view does not explain the refusal:\n%s", am.View().Content)
	}
}

// D9: selection toggles and A must stay unguarded — verify A's own refusal
// path (headPushed) still fires the same whether or not a generation is busy.
func TestAmendStillWorksWhileBusy(t *testing.T) {
	m := newTestModel(t, &fakeRunner{out: "x"})

	next, cmd1 := m.Update(key("g"))
	if cmd1 == nil {
		t.Fatal("g produced no command")
	}
	busy := next.(Model)
	if !busy.busy {
		t.Fatal("model not busy after g")
	}

	toggled := update(busy, key("A"))
	if !toggled.amend {
		t.Error("A did not toggle amend while busy — it should be unguarded")
	}

	busy.headPushed = true
	refused := update(busy, key("A"))
	if refused.amend {
		t.Error("amend enabled on a pushed HEAD even while busy")
	}
	if !strings.Contains(refused.View().Content, "already pushed") {
		t.Errorf("view does not explain the refusal:\n%s", refused.View().Content)
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

// E2: a requested push must still happen even when the post-commit intent
// cleanup fails, and the failure must not be swallowed.
func TestPushStillHappensWhenPostCommitCleanupFails(t *testing.T) {
	dir := gitInit(t)
	writeRepoFile(t, dir, "a.go", "1\n")
	commitAllRepo(t, dir, "init")

	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare", "--quiet")
	gitRun(t, dir, "remote", "add", "origin", remote)
	gitRun(t, dir, "push", "--quiet", "-u", "origin", "master")

	writeRepoFile(t, dir, "new.go", "package main\n")
	// A post-commit hook plants a stale index.lock so unmark(nil) — the
	// `git reset` that runs right after Commit returns — fails.
	writeHook(t, dir, "post-commit", "touch .git/index.lock")

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

	_, cmd := m.Update(key("P"))
	if cmd == nil {
		t.Fatal("P produced no command")
	}
	msg := drain(t, cmd)

	if err := os.Remove(filepath.Join(dir, ".git", "index.lock")); err != nil {
		t.Fatal(err)
	}

	cm, ok := msg.(committedMsg)
	if !ok {
		t.Fatalf("msg = %#v, want committedMsg", msg)
	}
	if cm.err == nil {
		t.Error("committedMsg does not surface the cleanup failure")
	}
	if !strings.Contains(cm.summary, "pushed") {
		t.Errorf("summary = %q, want the push to still have happened despite the cleanup failure", cm.summary)
	}

	pushed := gitRun(t, remote, "log", "-1", "--format=%s")
	if strings.TrimSpace(pushed) != "add new" {
		t.Errorf("remote HEAD = %q, want the push to have landed", pushed)
	}
}

// blockingRunner parks a generation in flight until its context is cancelled.
type blockingRunner struct{ started chan struct{} }

func (b *blockingRunner) Run(ctx context.Context, _, _ string) (string, error) {
	close(b.started)
	<-ctx.Done()
	return "", ctx.Err()
}

// C1: bubbletea abandons command goroutines on quit, so the deferred cleanup
// inside generate never runs and the intent-to-add entry survives the process.
func TestQuitDuringGenerationUndoesIntentToAdd(t *testing.T) {
	dir := gitInit(t)
	writeRepoFile(t, dir, "a.go", "1\n")
	commitAllRepo(t, dir, "init")
	writeRepoFile(t, dir, "brand-new.go", "package main\n")

	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	runner := &blockingRunner{started: make(chan struct{})}
	m := New(repo, config.Config{Model: "haiku", Prompt: "{{diff}}"}, runner)
	m.width, m.height = 100, 30
	m = update(m, statusMsg{
		files:  []git.FileChange{{Path: "brand-new.go", Index: '?', Worktree: '?'}},
		branch: git.Branch{Name: "master"},
	})

	next, cmd := m.Update(key("g"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("g produced no command")
	}
	go func() {
		batch, ok := cmd().(tea.BatchMsg)
		if !ok {
			return
		}
		for _, c := range batch {
			go c()
		}
	}()
	<-runner.started

	if status := gitRun(t, dir, "status", "--porcelain"); strings.Contains(status, "?? brand-new.go") {
		t.Fatalf("precondition: generation should have staged brand-new.go, status = %q", status)
	}

	m.Update(key("q"))

	if status := gitRun(t, dir, "status", "--porcelain"); !strings.Contains(status, "?? brand-new.go") {
		t.Errorf("status = %q, want brand-new.go untracked again once quit returns", status)
	}
}

// C2: BranchState fails on an unborn branch; the file list must survive it.
func TestLoadStatusInRepoWithNoCommits(t *testing.T) {
	dir := gitInit(t)
	writeRepoFile(t, dir, "first.go", "package main\n")

	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(repo, config.Config{}, &fakeRunner{})
	m.width, m.height = 100, 30

	msg := m.loadStatus(false)()
	st, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("msg = %#v, want statusMsg in a repository with no commits", msg)
	}
	if len(st.files) != 1 || st.files[0].Path != "first.go" {
		t.Fatalf("files = %+v, want first.go", st.files)
	}
	if st.branch.Name != "master" {
		t.Errorf("branch = %+v, want master", st.branch)
	}

	m = update(m, st)
	if !strings.Contains(m.View().Content, "first.go") {
		t.Errorf("file list is empty in a repository with no commits:\n%s", m.View().Content)
	}
}

// C3.1: browsing a diff must not contend for index.lock.
func TestLoadDiffForUntrackedFileLeavesTheIndexAlone(t *testing.T) {
	dir := gitInit(t)
	writeRepoFile(t, dir, "a.go", "1\n")
	commitAllRepo(t, dir, "init")
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

	lock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	msg := m.loadDiff()()
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}

	dm, ok := msg.(diffMsg)
	if !ok {
		t.Fatalf("msg = %#v, want a diffMsg — browsing must not need the index", msg)
	}
	if !strings.Contains(dm.body, "package main") {
		t.Errorf("diff = %q, want the untracked file's content", dm.body)
	}
	if status := gitRun(t, dir, "status", "--porcelain"); !strings.Contains(status, "?? new.go") {
		t.Errorf("status = %q, want new.go still untracked", status)
	}
}

// I1: the commit landed, so the list must still refresh and the message clear.
func TestFailedPushStillReportsTheCommit(t *testing.T) {
	dir := gitInit(t)
	writeRepoFile(t, dir, "a.go", "1\n")
	commitAllRepo(t, dir, "init")
	writeRepoFile(t, dir, "a.go", "2\n")

	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := New(repo, config.Config{}, &fakeRunner{})
	m.width, m.height = 100, 30
	m = update(m, statusMsg{
		files:  []git.FileChange{{Path: "a.go", Index: ' ', Worktree: 'M'}},
		branch: git.Branch{Name: "master"},
	})
	m.msgInput.SetValue("change a")

	_, cmd := m.Update(key("P")) // no upstream configured, so the push fails
	if cmd == nil {
		t.Fatal("P produced no command")
	}
	msg := drain(t, cmd)
	cm, ok := msg.(committedMsg)
	if !ok {
		t.Fatalf("msg = %#v, want a committedMsg carrying the push failure", msg)
	}
	if cm.err == nil {
		t.Error("committedMsg does not carry the push error")
	}
	if subject := strings.TrimSpace(gitRun(t, dir, "log", "-1", "--format=%s")); subject != "change a" {
		t.Fatalf("HEAD subject = %q, want the commit to have landed", subject)
	}

	after, refresh := m.Update(cm)
	if refresh == nil {
		t.Error("a failed push skipped the status refresh")
	}
	am := after.(Model)
	if am.message() != "" {
		t.Errorf("message = %q, want it cleared — the commit landed", am.message())
	}
	if am.amend {
		t.Error("amend still set after a commit that landed")
	}
	if am.err == nil {
		t.Error("push failure not shown")
	}
}

// I2: generation #1's late result must not disturb generation #2.
func TestSupersededGenerationResultIsIgnored(t *testing.T) {
	m := newTestModel(t, &fakeRunner{out: "x"})

	first, _ := m.Update(key("g"))
	m = first.(Model)
	staleErr := errMsg{err: errFake{}, epoch: m.epoch}
	staleGen := generatedMsg{message: "stale message", epoch: m.epoch}

	m = update(m, key("esc"))
	second, _ := m.Update(key("g"))
	m = second.(Model)

	m = update(m, staleErr)
	if !m.busy {
		t.Error("a superseded error cleared busy for the newer generation")
	}
	if m.cancel == nil {
		t.Error("a superseded error cleared cancel for the newer generation")
	}
	if m.err != nil {
		t.Errorf("a superseded error was displayed: %v", m.err)
	}

	m.msgInput.SetValue("typed while generating")
	m = update(m, staleGen)
	if m.message() != "typed while generating" {
		t.Errorf("message = %q, want the user's own text kept", m.message())
	}
}

// B: an untagged error (epoch 0) — e.g. a transient failure loading a diff
// while a generation is in flight — must not disarm that generation's cancel.
func TestUntaggedErrorDoesNotDisarmALiveGeneration(t *testing.T) {
	runner := &fakeRunner{out: "feat: thing"}
	m := newTestModel(t, runner)

	next, cmd := m.Update(key("g"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("g produced no command")
	}
	if m.cancel == nil {
		t.Fatal("precondition: cancel should be set while generation is in flight")
	}

	m = update(m, errMsg{err: errFake{}}) // epoch 0: e.g. loadDiff's error

	if !m.busy {
		t.Error("an untagged error cleared busy for a live generation")
	}
	if m.cancel == nil {
		t.Error("an untagged error cleared cancel for a live generation")
	}
	if m.err == nil || m.err.Error() != "boom" {
		t.Errorf("the untagged error was not recorded: %v", m.err)
	}
}

func TestCancelledGenerationKeepsItsStatus(t *testing.T) {
	m := newTestModel(t, &fakeRunner{out: "x"})

	next, _ := m.Update(key("g"))
	m = next.(Model)
	stale := errMsg{err: context.Canceled, epoch: m.epoch}

	m = update(m, key("esc"))
	m = update(m, stale)

	if !strings.Contains(m.View().Content, "generation cancelled") {
		t.Errorf("view does not report the cancellation:\n%s", m.View().Content)
	}
}

// I3: the spinner's status must not outlive the generation.
func TestGeneratedMessageClearsTheGeneratingStatus(t *testing.T) {
	m := newTestModel(t, &fakeRunner{out: "feat: done"})

	next, cmd := m.Update(key("g"))
	m = next.(Model)
	if !strings.Contains(m.View().Content, "generating") {
		t.Fatal("precondition: status bar does not report generating")
	}

	m = update(m, drain(t, cmd))
	if strings.Contains(m.View().Content, "generating") {
		t.Errorf("status bar still reports generating after the message arrived:\n%s", m.View().Content)
	}
}

// I6: the view ranks err above status, so a stale one hides the refusal.
func TestAmendRefusalClearsStaleError(t *testing.T) {
	m := newTestModel(t, &fakeRunner{})
	m.headPushed = true
	m = update(m, errMsg{err: errFake{}})

	m = update(m, key("A"))

	out := m.View().Content
	if strings.Contains(out, "boom") {
		t.Errorf("stale error masks the amend refusal:\n%s", out)
	}
	if !strings.Contains(out, "already pushed") {
		t.Errorf("view does not show the refusal:\n%s", out)
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
