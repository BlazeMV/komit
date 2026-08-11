package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func headFiles(t *testing.T, r *Repo) string {
	t.Helper()
	return gitDo(t, r, "show", "--name-only", "--format=", "HEAD")
}

func TestCommitOnlyWritesSelectedPaths(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	write(t, r, "b.go", "1\n")
	commitAll(t, r, "init")

	write(t, r, "a.go", "2\n")
	write(t, r, "b.go", "2\n")

	if err := r.Commit([]string{"a.go"}, "change a", false); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if files := headFiles(t, r); !strings.Contains(files, "a.go") || strings.Contains(files, "b.go") {
		t.Errorf("HEAD touched %q, want only a.go", files)
	}
	changes, _ := r.Status()
	if len(changes) != 1 || changes[0].Path != "b.go" {
		t.Errorf("after commit status = %+v, want b.go still modified", changes)
	}
}

func TestCommitLeavesUnselectedStagedFileStaged(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	write(t, r, "b.go", "1\n")
	commitAll(t, r, "init")

	write(t, r, "a.go", "2\n")
	write(t, r, "b.go", "2\n")
	gitDo(t, r, "add", "b.go") // b is staged but NOT selected

	if err := r.Commit([]string{"a.go"}, "change a", false); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if files := headFiles(t, r); strings.Contains(files, "b.go") {
		t.Fatalf("commit swallowed staged-but-unselected b.go: %q", files)
	}
	changes, _ := r.Status()
	if len(changes) != 1 || changes[0].Index != 'M' {
		t.Errorf("status = %+v, want b.go still staged", changes)
	}
}

func TestCommitPartiallyStagedFileTakesWorkingTree(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "one\n")
	commitAll(t, r, "init")

	write(t, r, "a.go", "one\ntwo\n")
	gitDo(t, r, "add", "a.go")
	write(t, r, "a.go", "one\ntwo\nthree\n")

	if err := r.Commit([]string{"a.go"}, "change a", false); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	committed := gitDo(t, r, "show", "HEAD:a.go")
	if !strings.Contains(committed, "three") {
		t.Errorf("committed content = %q, want the full working tree version", committed)
	}
	if changes, _ := r.Status(); len(changes) != 0 {
		t.Errorf("status = %+v, want clean", changes)
	}
}

func TestCommitUntrackedFile(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	commitAll(t, r, "init")
	write(t, r, "new.go", "package main\n")

	cleanup, err := r.MarkIntent([]string{"new.go"})
	if err != nil {
		t.Fatalf("MarkIntent: %v", err)
	}
	defer cleanup()

	if err := r.Commit([]string{"new.go"}, "add new", false); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if got := gitDo(t, r, "show", "HEAD:new.go"); !strings.Contains(got, "package main") {
		t.Errorf("HEAD:new.go = %q", got)
	}
}

func TestCommitNoPaths(t *testing.T) {
	r := newRepo(t)
	if err := r.Commit(nil, "nothing", false); !errors.Is(err, ErrNoPaths) {
		t.Fatalf("err = %v, want ErrNoPaths", err)
	}
}

func TestCommitFailedHookReturnsStderr(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	commitAll(t, r, "init")

	hook := filepath.Join(r.Dir, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'hook says no' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, r, "a.go", "2\n")

	err := r.Commit([]string{"a.go"}, "change a", false)
	var gitErr *Error
	if !errors.As(err, &gitErr) {
		t.Fatalf("err = %v, want *git.Error", err)
	}
	if !strings.Contains(gitErr.Stderr, "hook says no") {
		t.Errorf("Stderr = %q, want the hook's message", gitErr.Stderr)
	}
}

func TestAmendRewritesHeadAndKeepsOtherFiles(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	write(t, r, "b.go", "1\n")
	commitAll(t, r, "init")

	write(t, r, "a.go", "2\n")
	if err := r.Commit([]string{"a.go"}, "change a", false); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	before := strings.TrimSpace(gitDo(t, r, "rev-parse", "HEAD"))

	write(t, r, "a.go", "3\n")
	if err := r.Commit([]string{"a.go"}, "change a properly", true); err != nil {
		t.Fatalf("amend: %v", err)
	}

	if after := strings.TrimSpace(gitDo(t, r, "rev-parse", "HEAD")); after == before {
		t.Error("HEAD unchanged after amend")
	}
	if subj := strings.TrimSpace(gitDo(t, r, "log", "-1", "--format=%s")); subj != "change a properly" {
		t.Errorf("subject = %q", subj)
	}
	if count := strings.TrimSpace(gitDo(t, r, "rev-list", "--count", "HEAD")); count != "2" {
		t.Errorf("commit count = %s, want 2", count)
	}
	if got := gitDo(t, r, "show", "HEAD:b.go"); !strings.Contains(got, "1") {
		t.Errorf("amend dropped b.go from the tree")
	}
}

func TestBranchStateWithoutUpstream(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	commitAll(t, r, "init")

	b, err := r.BranchState()
	if err != nil {
		t.Fatalf("BranchState: %v", err)
	}
	if b.Name != "master" {
		t.Errorf("Name = %q, want master", b.Name)
	}
	if b.Upstream != "" || b.Ahead != 0 || b.Behind != 0 {
		t.Errorf("BranchState = %+v, want no upstream", b)
	}
}

func TestBranchStateAheadOfUpstream(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	commitAll(t, r, "init")

	remote := t.TempDir()
	gitDo(t, r, "init", "--bare", "--quiet", remote)
	gitDo(t, r, "remote", "add", "origin", remote)
	gitDo(t, r, "push", "--quiet", "-u", "origin", "master")

	write(t, r, "a.go", "2\n")
	commitAll(t, r, "second")

	b, err := r.BranchState()
	if err != nil {
		t.Fatalf("BranchState: %v", err)
	}
	if b.Ahead != 1 || b.Behind != 0 {
		t.Errorf("BranchState = %+v, want ahead 1", b)
	}

	pushed, err := r.HeadPushed()
	if err != nil {
		t.Fatalf("HeadPushed: %v", err)
	}
	if pushed {
		t.Error("HeadPushed = true for an unpushed commit")
	}

	if err := r.Push(); err != nil {
		t.Fatalf("Push: %v", err)
	}
	pushed, err = r.HeadPushed()
	if err != nil {
		t.Fatalf("HeadPushed: %v", err)
	}
	if !pushed {
		t.Error("HeadPushed = false right after pushing")
	}
}

func TestHeadPushedWithoutUpstreamIsFalse(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	commitAll(t, r, "init")

	pushed, err := r.HeadPushed()
	if err != nil {
		t.Fatalf("HeadPushed: %v", err)
	}
	if pushed {
		t.Error("HeadPushed = true with no upstream configured")
	}
}
