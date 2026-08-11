package git

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenFindsToplevelFromSubdir(t *testing.T) {
	r := newRepo(t)
	write(t, r, "pkg/sub/file.go", "package sub\n")

	got, err := Open(filepath.Join(r.Dir, "pkg", "sub"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// macOS temp dirs are symlinked (/var -> /private/var); compare resolved paths.
	if !strings.HasSuffix(got.Dir, strings.TrimPrefix(r.Dir, "/private")) {
		t.Errorf("Dir = %q, want toplevel of %q", got.Dir, r.Dir)
	}
}

func TestOpenOutsideRepoFails(t *testing.T) {
	if _, err := Open(t.TempDir()); err == nil {
		t.Fatal("expected error opening a non-repo directory")
	}
}

func TestRunErrorCarriesStderr(t *testing.T) {
	r := newRepo(t)
	_, err := r.run("cat-file", "-p", "deadbeef")

	var gitErr *Error
	if !errors.As(err, &gitErr) {
		t.Fatalf("error %v is not *git.Error", err)
	}
	if gitErr.Stderr == "" {
		t.Error("Stderr is empty, want git's message")
	}
	if !strings.Contains(gitErr.Error(), gitErr.Stderr) {
		t.Errorf("Error() = %q, want it to include stderr", gitErr.Error())
	}
}

func TestStatusReportsAllChangeKinds(t *testing.T) {
	r := newRepo(t)
	write(t, r, "tracked.go", "package main\n")
	write(t, r, "deleted.go", "package main\n")
	commitAll(t, r, "init")

	write(t, r, "tracked.go", "package main // edited\n")
	write(t, r, "untracked.go", "package main\n")
	gitDo(t, r, "rm", "--quiet", "deleted.go")

	changes, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	byPath := map[string]FileChange{}
	for _, c := range changes {
		byPath[c.Path] = c
	}
	if len(byPath) != 3 {
		t.Fatalf("got %d changes, want 3: %+v", len(byPath), changes)
	}
	if c := byPath["untracked.go"]; !c.Untracked() {
		t.Errorf("untracked.go: %+v, want untracked", c)
	}
	if c := byPath["tracked.go"]; c.Letter() != "M" {
		t.Errorf("tracked.go letter = %q, want M", c.Letter())
	}
	if c := byPath["deleted.go"]; c.Letter() != "D" {
		t.Errorf("deleted.go letter = %q, want D", c.Letter())
	}
}

func TestStatusCleanRepoIsEmpty(t *testing.T) {
	r := newRepo(t)
	write(t, r, "a.go", "package main\n")
	commitAll(t, r, "init")

	changes, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("got %+v, want no changes", changes)
	}
}
