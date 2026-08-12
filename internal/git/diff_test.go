package git

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestDiffOnlyIncludesGivenPaths(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "package main\n")
	write(t, r, "b.go", "package main\n")
	commitAll(t, r, "init")

	write(t, r, "a.go", "package main // changed a\n")
	write(t, r, "b.go", "package main // changed b\n")

	diff, err := r.Diff([]string{"a.go"})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "changed a") {
		t.Errorf("diff missing a.go change:\n%s", diff)
	}
	if strings.Contains(diff, "changed b") {
		t.Errorf("diff leaked b.go change:\n%s", diff)
	}
}

func TestDiffIncludesStagedAndUnstagedChanges(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "one\n")
	commitAll(t, r, "init")

	write(t, r, "a.go", "one\ntwo\n")
	gitDo(t, r, "add", "a.go")
	write(t, r, "a.go", "one\ntwo\nthree\n")

	diff, err := r.Diff([]string{"a.go"})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	for _, want := range []string{"+two", "+three"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff missing %q (must be against HEAD, not the index):\n%s", want, diff)
		}
	}
}

func TestDiffBeforeFirstCommit(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "package main\n")
	cleanup, err := r.MarkIntent([]string{"a.go"})
	if err != nil {
		t.Fatalf("MarkIntent: %v", err)
	}
	defer cleanup()

	diff, err := r.Diff([]string{"a.go"})
	if err != nil {
		t.Fatalf("Diff on repo with no HEAD: %v", err)
	}
	if !strings.Contains(diff, "package main") {
		t.Errorf("diff missing new file content:\n%s", diff)
	}
}

func TestMarkIntentMakesUntrackedFileVisibleThenCleansUp(t *testing.T) {
	r := newRepo(t)
	write(t, r, "old.go", "package main\n")
	commitAll(t, r, "init")
	write(t, r, "new.go", "package main // brand new\n")

	cleanup, err := r.MarkIntent([]string{"new.go"})
	if err != nil {
		t.Fatalf("MarkIntent: %v", err)
	}

	diff, err := r.Diff([]string{"new.go"})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "brand new") {
		t.Errorf("diff missing untracked file content:\n%s", diff)
	}

	cleanup()

	changes, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	for _, c := range changes {
		if c.Path == "new.go" && !c.Untracked() {
			t.Errorf("new.go is %+v after cleanup, want untracked again", c)
		}
	}
}

func TestMarkIntentCleanupBeforeFirstCommit(t *testing.T) {
	r := newRepo(t)
	write(t, r, "new.go", "package main\n")

	cleanup, err := r.MarkIntent([]string{"new.go"})
	if err != nil {
		t.Fatalf("MarkIntent: %v", err)
	}
	cleanup()

	changes, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(changes) != 1 || !changes[0].Untracked() {
		t.Errorf("got %+v, want new.go untracked again", changes)
	}
}

// C3.1: browsing an untracked file must not need the index at all. Holding
// index.lock is what a concurrent add -N/reset pair does to this path.
func TestDiffUntrackedNeitherLocksNorRewritesTheIndex(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	commitAll(t, r, "init")
	write(t, r, "new.go", "package main\n")

	idx := filepath.Join(r.Dir, ".git", "index")
	before, err := os.ReadFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	lock := idx + ".lock"
	if err := os.WriteFile(lock, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	diff, err := r.DiffUntracked("new.go")
	if err != nil {
		t.Fatalf("DiffUntracked while the index is locked: %v", err)
	}
	if !strings.Contains(diff, "package main") {
		t.Errorf("diff missing untracked file content:\n%s", diff)
	}

	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("DiffUntracked rewrote the index")
	}
	changes, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(changes) != 1 || !changes[0].Untracked() {
		t.Errorf("status = %+v, want new.go still untracked", changes)
	}
}

func TestDiffUntrackedMatchesTheIntentToAddDiff(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	commitAll(t, r, "init")
	write(t, r, "new.go", "package main\nfunc main() {}\n")

	cleanup, err := r.MarkIntent([]string{"new.go"})
	if err != nil {
		t.Fatalf("MarkIntent: %v", err)
	}
	want, err := r.Diff([]string{"new.go"})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	got, err := r.DiffUntracked("new.go")
	if err != nil {
		t.Fatalf("DiffUntracked: %v", err)
	}
	if got != want {
		t.Errorf("DiffUntracked output differs from the intent-to-add diff:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// C1: a cleanup that never ran must still be reachable and must be idempotent.
func TestDrainIntentsUndoesLeakedEntriesTwiceOver(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	commitAll(t, r, "init")
	write(t, r, "one.go", "package main\n")
	write(t, r, "two.go", "package main\n")

	for _, p := range []string{"one.go", "two.go"} {
		if _, err := r.MarkIntent([]string{p}); err != nil {
			t.Fatalf("MarkIntent(%s): %v", p, err)
		}
	}
	if status := gitDo(t, r, "status", "--porcelain"); strings.Contains(status, "?? one.go") {
		t.Fatalf("precondition: one.go should be staged, status = %q", status)
	}

	for i := 0; i < 2; i++ {
		if err := r.DrainIntents(); err != nil {
			t.Fatalf("DrainIntents (pass %d): %v", i+1, err)
		}
	}

	changes, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("status = %+v, want two entries", changes)
	}
	for _, c := range changes {
		if !c.Untracked() {
			t.Errorf("%s is %+v after draining, want untracked again", c.Path, c)
		}
	}
}

// I5: a failed MarkIntent must still hand back a usable cleanup.
func TestMarkIntentReturnsCleanupOnTheErrorPath(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	commitAll(t, r, "init")
	write(t, r, "new.go", "package main\n")

	cleanup, err := r.MarkIntent([]string{"new.go", "missing.go"})
	if err == nil {
		t.Fatal("MarkIntent with a nonexistent path succeeded, want an error")
	}
	if cleanup == nil {
		t.Fatal("MarkIntent returned a nil cleanup, so anything it staged leaks")
	}
	if err := cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	changes, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(changes) != 1 || !changes[0].Untracked() {
		t.Errorf("status = %+v, want new.go untracked", changes)
	}
}

func TestMarkIntentCleanupReportsFailure(t *testing.T) {
	r := newRepo(t)
	write(t, r, "new.go", "package main\n")

	cleanup, err := r.MarkIntent([]string{"new.go"})
	if err != nil {
		t.Fatalf("MarkIntent: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(r.Dir, ".git")); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(); err == nil {
		t.Error("cleanup swallowed a failed git invocation")
	}
}

// C3.2: index-mutating work runs under LockIndex, so it cannot collide with
// itself or with a concurrent drain.
func TestLockedIndexWorkNeverCollidesWithDrainIntents(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	commitAll(t, r, "init")

	const files, rounds = 6, 12
	paths := make([]string, files)
	for i := range paths {
		paths[i] = fmt.Sprintf("n%d.go", i)
		write(t, r, paths[i], "package main\n")
	}

	errs := make(chan error, files*rounds*2)
	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		for _, p := range paths {
			wg.Add(1)
			go func(p string) {
				defer wg.Done()
				r.LockIndex()
				defer r.UnlockIndex()
				cleanup, err := r.MarkIntent([]string{p})
				if err != nil {
					errs <- err
					return
				}
				if _, err := r.Diff([]string{p}); err != nil {
					errs <- err
				}
				if err := cleanup(); err != nil {
					errs <- err
				}
			}(p)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.DrainIntents(); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent index operation failed: %v", err)
	}

	if err := r.DrainIntents(); err != nil {
		t.Fatalf("final DrainIntents: %v", err)
	}
	changes, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(changes) != files {
		t.Fatalf("status = %+v, want %d entries", changes, files)
	}
	for _, c := range changes {
		if !c.Untracked() {
			t.Errorf("%s leaked into the index as %+v", c.Path, c)
		}
	}
}

func TestDiffAmendSpansTheCommitBeingAmended(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "one\n")
	commitAll(t, r, "init")

	write(t, r, "a.go", "one\ntwo\n")
	commitAll(t, r, "add two") // the commit that will be amended

	write(t, r, "a.go", "one\ntwo\nthree\n")

	diff, err := r.DiffAmend([]string{"a.go"})
	if err != nil {
		t.Fatalf("DiffAmend: %v", err)
	}
	for _, want := range []string{"+two", "+three"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff missing %q — amend context must span HEAD~1 to working tree:\n%s", want, diff)
		}
	}
}

func TestDiffAmendOnRootCommit(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "one\n")
	commitAll(t, r, "init")
	write(t, r, "a.go", "one\ntwo\n")

	diff, err := r.DiffAmend([]string{"a.go"})
	if err != nil {
		t.Fatalf("DiffAmend on a root commit: %v", err)
	}
	if !strings.Contains(diff, "+one") {
		t.Errorf("amending the root commit should show the file as added:\n%s", diff)
	}
}

func TestRecentCommits(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "1\n")
	commitAll(t, r, "first thing")
	write(t, r, "a.go", "2\n")
	commitAll(t, r, "second thing")

	out, err := r.RecentCommits(5)
	if err != nil {
		t.Fatalf("RecentCommits: %v", err)
	}
	if !strings.Contains(out, "second thing") || !strings.Contains(out, "first thing") {
		t.Errorf("RecentCommits = %q, want both subjects", out)
	}
}

func TestRecentCommitsOnEmptyRepo(t *testing.T) {
	r := newRepo(t)
	out, err := r.RecentCommits(5)
	if err != nil {
		t.Fatalf("RecentCommits on empty repo: %v", err)
	}
	if out != "" {
		t.Errorf("RecentCommits = %q, want empty string", out)
	}
}
