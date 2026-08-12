package git

import (
	"os"
	"strings"
)

// emptyTree is git's well-known hash of the empty tree. Diffing against it
// makes a repo with no commits behave like any other.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

func (r *Repo) hasHEAD() bool {
	_, err := r.run("rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}

func (r *Repo) diffBase() string {
	if r.hasHEAD() {
		return "HEAD"
	}
	return emptyTree
}

// Diff returns the working-tree diff of paths against HEAD (staged and unstaged
// changes together) — the same content that Commit will write.
func (r *Repo) Diff(paths []string) (string, error) {
	args := append([]string{"diff", "--no-color", r.diffBase(), "--"}, paths...)
	return r.run(args...)
}

// DiffUntracked diffs a single untracked file against nothing. It replaces a
// MarkIntent/Diff/cleanup round trip so that browsing cannot touch the index.
func (r *Repo) DiffUntracked(path string) (string, error) {
	return r.runTolerating(1, "diff", "--no-color", "--no-index", "--", os.DevNull, path)
}

// DiffAmend diffs against HEAD's parent — the content the amended commit will
// hold. Against HEAD it would omit what is already committed.
func (r *Repo) DiffAmend(paths []string) (string, error) {
	base := emptyTree
	if _, err := r.run("rev-parse", "--verify", "--quiet", "HEAD~1"); err == nil {
		base = "HEAD~1"
	}
	args := append([]string{"diff", "--no-color", base, "--"}, paths...)
	return r.run(args...)
}

// MarkIntent makes untracked paths visible to diff and --only; skipping cleanup
// leaves them staged. The cleanup is usable even when MarkIntent itself fails.
func (r *Repo) MarkIntent(paths []string) (func() error, error) {
	if len(paths) == 0 {
		return func() error { return nil }, nil
	}
	cleanup := r.registerIntent(paths)
	if _, err := r.run(append([]string{"add", "-N", "--"}, paths...)...); err != nil {
		return cleanup, err
	}
	return cleanup, nil
}

// RecentCommits returns the last n commit subjects, newest first. An empty repo
// yields an empty string rather than an error.
func (r *Repo) RecentCommits(n int) (string, error) {
	if !r.hasHEAD() {
		return "", nil
	}
	out, err := r.run("log", "--format=%s", "-n", itoa(n))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
