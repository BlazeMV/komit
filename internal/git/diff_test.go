package git

import (
	"strings"
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
