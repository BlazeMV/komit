# komit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `komit` — a bubbletea TUI that lets you select changed files, have Claude write the commit message, and commit exactly those files.

**Architecture:** Four internal packages behind a bubbletea UI. `internal/git` shells out to the real `git` binary (hooks, aliases, signing, worktrees all keep working). `internal/config` resolves a global + per-repo YAML prompt. `internal/ai` builds the prompt and runs `claude -p` through a `Runner` interface so tests never hit the network. `internal/ui` is the bubbletea model wiring them together.

**Tech Stack:** Go 1.25, bubbletea v2.0.8, bubbles v2.1.1, lipgloss v2.0.6, yaml.v3 v3.0.1, teatest/v2 for UI tests.

Design spec: [`komit.md`](komit.md).

## Global Constraints

- Module path `github.com/BlazeMV/komit`, branch `master`
- **Go 1.25** — `bubbles/v2` declares a `go >= 1.25.0` floor. `go get` bumps the `go` directive automatically; a 1.24 toolchain downloads 1.25 on demand.
- Dependency set is fixed. Charm's v2 line lives on a **vanity module path** — `charm.land/…`, not `github.com/charmbracelet/…`. Importing the github path fails with `module declares its path as: charm.land/…`:

| import path | version |
|---|---|
| `charm.land/bubbletea/v2` | v2.0.8 |
| `charm.land/bubbles/v2` | v2.1.1 |
| `charm.land/lipgloss/v2` | v2.0.6 |
| `charm.land/x/exp/teatest/v2` | latest |
| `gopkg.in/yaml.v3` | v3.0.1 |

- **Code comments follow the user's CLAUDE.md policy**, which is stricter than the comments shown in this plan's snippets: default to none; only for non-obvious or shady code; hard cap 2 lines of prose including docblocks; state the invariant and what breaks if violated — never the mechanism, the call chain, or why this approach was picked over another. If a comment in a snippet exceeds that, shorten it when you write the file.
- **There is no `tea.WithAltScreen()` in v2.** Alt screen is a field on the view: set `v.AltScreen = true` inside `View()`. Passing it as a program option does not compile.
- **`tea.Model` in v2 is `Init() Cmd`, `Update(Msg) (Model, Cmd)`, `View() View`** — note `View()` returns `tea.View`, NOT a string (verified against v2.0.8 source). Keep the layout in `render() string` and make `View()` a thin `tea.NewView(m.render())` wrapper; tests assert on `m.View().Content`.
- Key handling matches on `tea.KeyMsg.String()`. Verified against v2.0.8: a rune key yields `"g"`, shifted yields `"A"`, and **space yields `"space"`, not `" "`**. `tea.KeyMsg` is an interface in v2 satisfied by `tea.KeyPressMsg`, so `case tea.KeyMsg:` in a type switch still works.
- Commit messages: single line, lowercase, no body, **no AI attribution** (no `Co-Authored-By`, no generated-with footer)
- TDD: every task writes the failing test first and runs it to see it fail before implementing
- `git` is always invoked as `git -C <repo dir>` — never rely on the process working directory
- Every `git` error surfaced to the UI must carry git's own stderr; no swallowed errors, no silent fallbacks
- No network in tests. `claude` is only ever exercised through a fake binary on `PATH`.
- Verified against the real CLI on 2026-08-12: `printf '...' | claude -p --model haiku --output-format text --safe-mode --no-session-persistence` exits 0 and prints the bare completion.

---

### Task 1: Parse `git status` porcelain output

**Files:**
- Create: `internal/git/status.go`
- Test: `internal/git/status_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `type FileChange struct { Path, Orig string; Index, Worktree byte }`, methods `Untracked() bool`, `PartiallyStaged() bool`, `Letter() string`, and `func ParseStatus(out string) ([]FileChange, error)`

- [ ] **Step 1: Write the failing test**

```go
package git

import "testing"

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []FileChange
	}{
		{
			name: "staged modification",
			in:   "M  cmd/main.go\x00",
			want: []FileChange{{Index: 'M', Worktree: ' ', Path: "cmd/main.go"}},
		},
		{
			name: "unstaged modification",
			in:   " M cmd/main.go\x00",
			want: []FileChange{{Index: ' ', Worktree: 'M', Path: "cmd/main.go"}},
		},
		{
			name: "untracked",
			in:   "?? new.go\x00",
			want: []FileChange{{Index: '?', Worktree: '?', Path: "new.go"}},
		},
		{
			name: "rename carries original path",
			in:   "R  new.go\x00old.go\x00",
			want: []FileChange{{Index: 'R', Worktree: ' ', Path: "new.go", Orig: "old.go"}},
		},
		{
			name: "path with spaces is not split",
			in:   " M a file with spaces.go\x00",
			want: []FileChange{{Index: ' ', Worktree: 'M', Path: "a file with spaces.go"}},
		},
		{
			name:"multiple entries",
			in:   "M  a.go\x00?? b.go\x00",
			want: []FileChange{
				{Index: 'M', Worktree: ' ', Path: "a.go"},
				{Index: '?', Worktree: '?', Path: "b.go"},
			},
		},
		{name: "empty output", in: "", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStatus(tt.in)
			if err != nil {
				t.Fatalf("ParseStatus(%q) error: %v", tt.in, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d changes, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("entry %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseStatusRenameMissingOriginal(t *testing.T) {
	if _, err := ParseStatus("R  new.go\x00"); err == nil {
		t.Fatal("expected error for rename entry with no original path")
	}
}

func TestFileChangeClassification(t *testing.T) {
	tests := []struct {
		name              string
		c                 FileChange
		untracked, partial bool
		letter            string
	}{
		{"untracked", FileChange{Index: '?', Worktree: '?'}, true, false, "?"},
		{"staged only", FileChange{Index: 'M', Worktree: ' '}, false, false, "M"},
		{"unstaged only", FileChange{Index: ' ', Worktree: 'M'}, false, false, "M"},
		{"partially staged", FileChange{Index: 'M', Worktree: 'M'}, false, true, "M"},
		{"added", FileChange{Index: 'A', Worktree: ' '}, false, false, "A"},
		{"deleted in worktree", FileChange{Index: ' ', Worktree: 'D'}, false, false, "D"},
		{"renamed", FileChange{Index: 'R', Worktree: ' '}, false, false, "R"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.Untracked(); got != tt.untracked {
				t.Errorf("Untracked() = %v, want %v", got, tt.untracked)
			}
			if got := tt.c.PartiallyStaged(); got != tt.partial {
				t.Errorf("PartiallyStaged() = %v, want %v", got, tt.partial)
			}
			if got := tt.c.Letter(); got != tt.letter {
				t.Errorf("Letter() = %q, want %q", got, tt.letter)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestParseStatus -v`
Expected: build failure — `undefined: ParseStatus`, `undefined: FileChange`

- [ ] **Step 3: Write minimal implementation**

```go
// Package git wraps the git binary. Every operation shells out so that hooks,
// aliases, signing config and worktrees behave exactly as they do on the CLI.
package git

import (
	"fmt"
	"strings"
)

// FileChange is one entry of `git status --porcelain -z`. Index and Worktree
// are the two status columns (X and Y); Orig is set only for renames/copies.
type FileChange struct {
	Path     string
	Orig     string
	Index    byte
	Worktree byte
}

func (f FileChange) Untracked() bool { return f.Index == '?' }

// PartiallyStaged means --only will commit the full working-tree version, not
// just the staged hunks. The UI must mark these.
func (f FileChange) PartiallyStaged() bool {
	return f.Index != '?' && f.Index != ' ' && f.Worktree != ' '
}

// Letter is the single character shown in the file list.
func (f FileChange) Letter() string {
	if f.Untracked() {
		return "?"
	}
	if f.Index != ' ' {
		return string(f.Index)
	}
	return string(f.Worktree)
}

// ParseStatus parses NUL-separated `git status --porcelain -z` output.
func ParseStatus(out string) ([]FileChange, error) {
	out = strings.TrimSuffix(out, "\x00")
	if out == "" {
		return nil, nil
	}
	parts := strings.Split(out, "\x00")

	var changes []FileChange
	for i := 0; i < len(parts); i++ {
		entry := parts[i]
		if entry == "" {
			continue
		}
		if len(entry) < 4 {
			return nil, fmt.Errorf("malformed status entry %q", entry)
		}
		c := FileChange{Index: entry[0], Worktree: entry[1], Path: entry[3:]}
		if c.Index == 'R' || c.Index == 'C' {
			i++
			if i >= len(parts) {
				return nil, fmt.Errorf("rename entry %q has no original path", entry)
			}
			c.Orig = parts[i]
		}
		changes = append(changes, c)
	}
	return changes, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -v`
Expected: PASS (3 test functions)

- [ ] **Step 5: Commit**

```bash
cd ~/projects/blaze/komit
git add internal/git/status.go internal/git/status_test.go
git commit -m "add git status porcelain parsing"
```

---

### Task 2: Repo handle, command runner, error type

**Files:**
- Create: `internal/git/repo.go`
- Test: `internal/git/repo_test.go`, `internal/git/testhelp_test.go`

**Interfaces:**
- Consumes: `FileChange`, `ParseStatus` (Task 1)
- Produces: `type Repo struct { Dir string }`, `func Open(dir string) (*Repo, error)`, `func (r *Repo) run(args ...string) (string, error)` (unexported), `func (r *Repo) Status() ([]FileChange, error)`, `type Error struct { Args []string; Stderr string; Err error }` with `Error() string`
- Test helpers used by every later git task: `newRepo(t)`, `write(t, r, path, content)`, `gitDo(t, r, args...)`

- [ ] **Step 1: Write the failing test**

```go
// internal/git/testhelp_test.go
package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	setup := [][]string{
		{"init", "-b", "master"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "komit test"},
		{"config", "commit.gpgsign", "false"},
	}
	for _, args := range setup {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return &Repo{Dir: dir}
}

func write(t *testing.T, r *Repo, path, content string) {
	t.Helper()
	full := filepath.Join(r.Dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitDo(t *testing.T, r *Repo, args ...string) string {
	t.Helper()
	out, err := r.run(args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

// commitAll stages everything and commits, giving the repo a HEAD.
func commitAll(t *testing.T, r *Repo, msg string) {
	t.Helper()
	gitDo(t, r, "add", "-A")
	gitDo(t, r, "commit", "-m", msg)
}
```

```go
// internal/git/repo_test.go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run 'TestOpen|TestRun|TestStatus' -v`
Expected: build failure — `undefined: Open`, `undefined: Error`, `r.Status undefined`

- [ ] **Step 3: Write minimal implementation**

```go
package git

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Repo is a handle on a git working tree, identified by its toplevel directory.
type Repo struct {
	Dir string
}

// Error wraps a failed git invocation, keeping stderr so the UI can show git's
// own message rather than a paraphrase.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), e.Stderr)
}

func (e *Error) Unwrap() error { return e.Err }

// Open resolves dir to the toplevel of the repository containing it.
func Open(dir string) (*Repo, error) {
	r := &Repo{Dir: dir}
	out, err := r.run("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	return &Repo{Dir: strings.TrimSpace(out)}, nil
}

func (r *Repo) run(args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.Dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), &Error{
			Args:   args,
			Stderr: strings.TrimSpace(stderr.String()),
			Err:    err,
		}
	}
	return stdout.String(), nil
}

// Status lists every change in the working tree, including untracked files.
func (r *Repo) Status() ([]FileChange, error) {
	out, err := r.run("status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	return ParseStatus(out)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/git/
git commit -m "add repo handle, git runner and status"
```

---

### Task 3: Diff and intent-to-add for untracked files

**Files:**
- Modify: `internal/git/repo.go`
- Create: `internal/git/diff.go`
- Test: `internal/git/diff_test.go`

**Interfaces:**
- Consumes: `Repo.run`, `FileChange` (Tasks 1–2)
- Produces: `func (r *Repo) Diff(paths []string) (string, error)`, `func (r *Repo) DiffAmend(paths []string) (string, error)`, `func (r *Repo) MarkIntent(paths []string) (cleanup func(), err error)`, `func (r *Repo) hasHEAD() bool`, `func (r *Repo) RecentCommits(n int) (string, error)`

**Why `MarkIntent` exists:** `git commit --only` and `git diff HEAD` both ignore untracked files. Staging them with `git add -N` makes them visible without staging content. The returned cleanup undoes that if the user quits or the commit fails — komit must not leave the index dirtier than it found it.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run 'TestDiff|TestMarkIntent|TestRecent' -v`
Expected: build failure — `r.Diff undefined`, `r.MarkIntent undefined`, `r.RecentCommits undefined`

- [ ] **Step 3: Write minimal implementation**

```go
package git

import "strings"

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

// MarkIntent makes untracked paths visible to diff and --only. Not calling
// cleanup leaves them staged in the user's index.
func (r *Repo) MarkIntent(paths []string) (func(), error) {
	if len(paths) == 0 {
		return func() {}, nil
	}
	if _, err := r.run(append([]string{"add", "-N", "--"}, paths...)...); err != nil {
		return nil, err
	}
	done := false
	return func() {
		if done {
			return
		}
		done = true
		if r.hasHEAD() {
			r.run(append([]string{"reset", "--quiet", "HEAD", "--"}, paths...)...)
			return
		}
		r.run(append([]string{"rm", "--cached", "--quiet", "--force", "--"}, paths...)...)
	}, nil
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
```

Add to `repo.go`:

```go
import "strconv"

func itoa(n int) string { return strconv.Itoa(n) }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/git/
git commit -m "add diff, intent-to-add and recent commits"
```

---

### Task 4: Commit, amend, push, branch state

**Files:**
- Create: `internal/git/commit.go`
- Test: `internal/git/commit_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–3
- Produces:
  - `func (r *Repo) Commit(paths []string, msg string, amend bool) error`
  - `func (r *Repo) Push() error`
  - `type Branch struct { Name, Upstream string; Ahead, Behind int }`
  - `func (r *Repo) BranchState() (Branch, error)`
  - `func (r *Repo) HeadPushed() (bool, error)`
  - `var ErrNoPaths = errors.New("no files selected")`

**This is the data-loss-sensitive task.** The partial-staging and unselected-file tests are the point of it — do not weaken them.

- [ ] **Step 1: Write the failing test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run 'TestCommit|TestAmend|TestBranch|TestHead' -v`
Expected: build failure — `r.Commit undefined`, `ErrNoPaths undefined`, `r.BranchState undefined`

- [ ] **Step 3: Write minimal implementation**

```go
package git

import (
	"errors"
	"strconv"
	"strings"
)

// ErrNoPaths is returned when a commit is attempted with nothing selected.
var ErrNoPaths = errors.New("no files selected")

// Commit writes msg as a commit containing exactly paths, taken from the working
// tree. The index entries of unselected files are left untouched.
func (r *Repo) Commit(paths []string, msg string, amend bool) error {
	if len(paths) == 0 {
		return ErrNoPaths
	}
	args := []string{"commit", "--only", "--quiet", "-m", msg}
	if amend {
		args = append(args, "--amend")
	}
	args = append(args, "--")
	args = append(args, paths...)
	_, err := r.run(args...)
	return err
}

// Push pushes the current branch to its upstream.
func (r *Repo) Push() error {
	_, err := r.run("push")
	return err
}

// Branch describes the checked-out branch relative to its upstream.
type Branch struct {
	Name     string
	Upstream string
	Ahead    int
	Behind   int
}

// BranchState reports the branch name and, when an upstream exists, how far
// ahead/behind it is. A missing upstream is not an error.
func (r *Repo) BranchState() (Branch, error) {
	name, err := r.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return Branch{}, err
	}
	b := Branch{Name: strings.TrimSpace(name)}

	up, err := r.run("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return b, nil // no upstream configured
	}
	b.Upstream = strings.TrimSpace(up)

	counts, err := r.run("rev-list", "--left-right", "--count", b.Upstream+"...HEAD")
	if err != nil {
		return b, nil
	}
	fields := strings.Fields(counts)
	if len(fields) == 2 {
		b.Behind, _ = strconv.Atoi(fields[0])
		b.Ahead, _ = strconv.Atoi(fields[1])
	}
	return b, nil
}

// HeadPushed reports whether HEAD is already contained in its upstream, in which
// case amending would rewrite published history. No upstream means not pushed.
func (r *Repo) HeadPushed() (bool, error) {
	out, err := r.run("rev-list", "--count", "@{u}..HEAD")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(out) == "0", nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -v`
Expected: PASS — in particular `TestCommitLeavesUnselectedStagedFileStaged` and `TestCommitPartiallyStagedFileTakesWorkingTree`

- [ ] **Step 5: Commit**

```bash
git add internal/git/
git commit -m "add commit, amend, push and branch state"
```

---

### Task 5: Config loading and prompt rendering

**Files:**
- Create: `internal/config/config.go`, `internal/config/default.yml`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `type Config struct { Model string; Prompt string }`
  - `func Load(repoRoot string) (Config, error)` — defaults, then `$XDG_CONFIG_HOME/komit/config.yml` (falling back to `~/.config`), then `<repoRoot>/.komit.yml`; each layer overrides only the keys it sets
  - `func Default() Config`
  - `func UserPath() (string, error)`
  - `type Vars struct { Diff, Files, Branch, RecentCommits string }`
  - `func Render(prompt string, v Vars) string`

- [ ] **Step 1: Write the failing test**

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withConfigHome points XDG_CONFIG_HOME at a temp dir and optionally writes a
// global config into it.
func withConfigHome(t *testing.T, contents string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	if contents == "" {
		return
	}
	dir := filepath.Join(home, "komit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefaultsWhenNothingConfigured(t *testing.T) {
	withConfigHome(t, "")

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model == "" {
		t.Error("Model is empty, want a built-in default")
	}
	if !strings.Contains(cfg.Prompt, "{{diff}}") {
		t.Errorf("default prompt has no {{diff}} placeholder:\n%s", cfg.Prompt)
	}
}

func TestGlobalConfigOverridesOnlyKeysItSets(t *testing.T) {
	withConfigHome(t, "model: sonnet\n")

	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Model != "sonnet" {
		t.Errorf("Model = %q, want sonnet", cfg.Model)
	}
	if cfg.Prompt != Default().Prompt {
		t.Error("Prompt was clobbered by a config that only set model")
	}
}

func TestRepoConfigOverridesGlobal(t *testing.T) {
	withConfigHome(t, "model: sonnet\nprompt: global prompt {{diff}}\n")

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, ".komit.yml"), []byte("prompt: repo prompt {{diff}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(repo)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Prompt != "repo prompt {{diff}}" {
		t.Errorf("Prompt = %q, want the repo one", cfg.Prompt)
	}
	if cfg.Model != "sonnet" {
		t.Errorf("Model = %q, want sonnet carried over from global", cfg.Model)
	}
}

func TestLoadMalformedYAMLFails(t *testing.T) {
	withConfigHome(t, "model: [unclosed\n")

	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("expected an error for malformed YAML")
	}
}

func TestRender(t *testing.T) {
	got := Render("b={{branch}} f={{files}} r={{recent_commits}} d={{diff}}", Vars{
		Diff:          "DIFF",
		Files:         "FILES",
		Branch:        "BRANCH",
		RecentCommits: "RECENT",
	})
	want := "b=BRANCH f=FILES r=RECENT d=DIFF"
	if got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestRenderLeavesUnknownPlaceholders(t *testing.T) {
	got := Render("{{nope}} {{diff}}", Vars{Diff: "D"})
	if got != "{{nope}} D" {
		t.Errorf("Render = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: build failure — `undefined: Load`, `undefined: Render`

- [ ] **Step 3: Write minimal implementation**

`internal/config/default.yml`:

```yaml
model: haiku
prompt: |
  Write a git commit message for the diff below.

  Rules:
  - single line, imperative mood, lowercase, no trailing period
  - no body unless the change is non-obvious
  - describe what changed, not which files changed
  - output only the message, nothing else

  Recent commits in this repo, for style:
  {{recent_commits}}

  Files: {{files}}

  Diff:
  {{diff}}
```

`internal/config/config.go`:

```go
// Package config resolves the prompt configuration: built-in defaults, then the
// user's global file, then the repo's .komit.yml — each overriding per key.
package config

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed default.yml
var defaultYAML []byte

// Config is komit's entire configuration surface.
type Config struct {
	Model  string `yaml:"model"`
	Prompt string `yaml:"prompt"`
}

// RepoFile is the per-repository override read from the repo root.
const RepoFile = ".komit.yml"

// Default returns the built-in configuration.
func Default() Config {
	var c Config
	if err := yaml.Unmarshal(defaultYAML, &c); err != nil {
		panic("embedded default.yml is invalid: " + err.Error())
	}
	c.Prompt = strings.TrimSpace(c.Prompt)
	return c
}

// UserPath is the global config location: $XDG_CONFIG_HOME/komit/config.yml,
// falling back to ~/.config/komit/config.yml.
func UserPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "komit", "config.yml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "komit", "config.yml"), nil
}

// Load merges the built-in defaults, the global file and the repo file. A
// missing file is skipped; a malformed one is an error.
func Load(repoRoot string) (Config, error) {
	cfg := Default()

	userPath, err := UserPath()
	if err != nil {
		return cfg, err
	}
	for _, path := range []string{userPath, filepath.Join(repoRoot, RepoFile)} {
		if err := mergeFile(&cfg, path); err != nil {
			return cfg, err
		}
	}
	cfg.Prompt = strings.TrimSpace(cfg.Prompt)
	return cfg, nil
}

// mergeFile unmarshals path onto cfg. yaml.v3 only writes fields the document
// actually contains, which gives per-key override for free.
func mergeFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// Vars are the values substituted into a prompt template.
type Vars struct {
	Diff          string
	Files         string
	Branch        string
	RecentCommits string
}

// Render substitutes the {{...}} placeholders. Unknown ones are left as-is.
func Render(prompt string, v Vars) string {
	return strings.NewReplacer(
		"{{diff}}", v.Diff,
		"{{files}}", v.Files,
		"{{branch}}", v.Branch,
		"{{recent_commits}}", v.RecentCommits,
	).Replace(prompt)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go get gopkg.in/yaml.v3@v3.0.1 && go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/ go.mod go.sum
git commit -m "add layered config and prompt rendering"
```

---

### Task 6: Diff truncation and output cleaning

**Files:**
- Create: `internal/ai/prompt.go`
- Test: `internal/ai/prompt_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `const MaxDiffBytes = 60 << 10`, `const MaxFileBytes = 20 << 10`, `func TruncateDiff(diff string) string`, `func Clean(out string) string`

**Note on `Clean`:** it strips code fences and surrounding whitespace only. Preamble ("Here's a commit message:") is prevented by the prompt, not by regex — heuristic preamble stripping would eventually eat a real message.

- [ ] **Step 1: Write the failing test**

```go
package ai

import (
	"strings"
	"testing"
)

func TestTruncateDiffLeavesSmallDiffAlone(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n@@ -1 +1 @@\n-one\n+two\n"
	if got := TruncateDiff(diff); got != diff {
		t.Errorf("small diff was modified:\n%s", got)
	}
}

func TestTruncateDiffCapsPerFile(t *testing.T) {
	big := strings.Repeat("+line of a very long generated file\n", 2000) // ~72KB
	diff := "diff --git a/small.go b/small.go\n+kept\n" +
		"diff --git a/big.go b/big.go\n" + big

	got := TruncateDiff(diff)
	if !strings.Contains(got, "+kept") {
		t.Error("truncation dropped the small file")
	}
	if !strings.Contains(got, "truncated") {
		t.Error("no truncation marker in output")
	}
	if len(got) > MaxDiffBytes {
		t.Errorf("output is %d bytes, want <= %d", len(got), MaxDiffBytes)
	}
	if !strings.Contains(got, "big.go") {
		t.Error("truncated file lost its header")
	}
}

func TestTruncateDiffCapsWhole(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("diff --git a/f.go b/f.go\n")
		b.WriteString(strings.Repeat("+x\n", 500))
	}
	got := TruncateDiff(b.String())
	if len(got) > MaxDiffBytes {
		t.Errorf("output is %d bytes, want <= %d", len(got), MaxDiffBytes)
	}
	if !strings.Contains(got, "truncated") {
		t.Error("no truncation marker in output")
	}
}

func TestClean(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "feat: add thing", "feat: add thing"},
		{"trailing newline", "feat: add thing\n\n", "feat: add thing"},
		{"fenced", "```\nfeat: add thing\n```", "feat: add thing"},
		{"fenced with language", "```text\nfeat: add thing\n```\n", "feat: add thing"},
		{"body preserved", "feat: add thing\n\nbecause reasons", "feat: add thing\n\nbecause reasons"},
		{"inner backticks kept", "fix: escape `--only` correctly", "fix: escape `--only` correctly"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clean(tt.in); got != tt.want {
				t.Errorf("Clean(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ai/ -v`
Expected: build failure — `undefined: TruncateDiff`, `undefined: Clean`

- [ ] **Step 3: Write minimal implementation**

```go
// Package ai turns a diff into a commit message using the claude CLI.
package ai

import (
	"fmt"
	"strings"
)

const (
	// MaxDiffBytes caps the whole diff sent to the model.
	MaxDiffBytes = 60 << 10
	// MaxFileBytes caps any single file's diff before the whole-diff cap runs.
	MaxFileBytes = 20 << 10
)

// TruncateDiff keeps a diff under MaxDiffBytes: per-file cap first, then the
// tail. Every cut is marked — unmarked truncation reads as a complete diff.
func TruncateDiff(diff string) string {
	if len(diff) <= MaxDiffBytes {
		return diff
	}

	const sep = "diff --git "

	var b strings.Builder
	for i, part := range strings.Split(diff, sep) {
		if i == 0 {
			if part != "" {
				b.WriteString(capPart(part))
			}
			continue
		}
		b.WriteString(sep)
		b.WriteString(capPart(part))
	}

	out := b.String()
	if len(out) > MaxDiffBytes {
		out = out[:MaxDiffBytes] + fmt.Sprintf("\n... [diff truncated, %d bytes omitted]\n", len(out)-MaxDiffBytes)
	}
	return out
}

func capPart(part string) string {
	if len(part) <= MaxFileBytes {
		return part
	}
	return part[:MaxFileBytes] + fmt.Sprintf("\n... [truncated, %d bytes omitted]\n", len(part)-MaxFileBytes)
}

// Clean normalises the model's output into a commit message: surrounding code
// fences removed, whitespace trimmed. Nothing else is stripped.
func Clean(out string) string {
	s := strings.TrimSpace(out)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	lines = lines[1:] // drop opening fence (with or without a language)
	if strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/ -v && go vet ./internal/ai/`
Expected: PASS, no vet output

- [ ] **Step 5: Commit**

```bash
git add internal/ai/
git commit -m "add diff truncation and output cleaning"
```

---

### Task 7: The claude runner

**Files:**
- Create: `internal/ai/claude.go`, `internal/ai/generator.go`
- Test: `internal/ai/claude_test.go`

**Interfaces:**
- Consumes: `TruncateDiff`, `Clean` (Task 6), `config.Config`, `config.Vars`, `config.Render` (Task 5)
- Produces:
  - `type Runner interface { Run(ctx context.Context, model, prompt string) (string, error) }`
  - `type CLI struct { Bin string }` implementing `Runner` (empty `Bin` means `claude`)
  - `var ErrMissing = errors.New("claude CLI not found on PATH")`
  - `func Generate(ctx context.Context, r Runner, cfg config.Config, v config.Vars) (string, error)` — renders the prompt with a truncated diff, runs, cleans

- [ ] **Step 1: Write the failing test**

```go
package ai

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BlazeMV/komit/internal/config"
)

// fakeClaude installs a script named "claude" on PATH. body is shell that can
// read the prompt from stdin and write to stdout; args land in "$@".
func fakeClaude(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCLIPassesPromptOnStdinAndReturnsStdout(t *testing.T) {
	fakeClaude(t, `cat > /dev/null; echo "feat: from fake"`)

	got, err := CLI{}.Run(context.Background(), "haiku", "the prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(got) != "feat: from fake" {
		t.Errorf("Run = %q", got)
	}
}

func TestCLIPromptReachesStdin(t *testing.T) {
	fakeClaude(t, `read -r line; echo "got:$line"`)

	got, err := CLI{}.Run(context.Background(), "haiku", "hello prompt")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(got) != "got:hello prompt" {
		t.Errorf("Run = %q, want the prompt echoed back", got)
	}
}

func TestCLIPassesExpectedFlags(t *testing.T) {
	fakeClaude(t, `cat > /dev/null; echo "$@"`)

	got, err := CLI{}.Run(context.Background(), "haiku", "p")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"-p", "--model haiku", "--output-format text", "--safe-mode", "--no-session-persistence"} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q missing %q", strings.TrimSpace(got), want)
		}
	}
}

func TestCLIMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := CLI{}.Run(context.Background(), "haiku", "p")
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("err = %v, want ErrMissing", err)
	}
}

func TestCLIFailureIncludesStderr(t *testing.T) {
	fakeClaude(t, `cat > /dev/null; echo "credit balance too low" >&2; exit 1`)

	_, err := CLI{}.Run(context.Background(), "haiku", "p")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "credit balance too low") {
		t.Errorf("err = %v, want claude's stderr", err)
	}
}

func TestCLIRespectsContextCancellation(t *testing.T) {
	fakeClaude(t, `cat > /dev/null; sleep 30`)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := (CLI{}).Run(ctx, "haiku", "p"); err == nil {
		t.Fatal("expected an error when the context expires")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Run took %s, want it to stop with the context", elapsed)
	}
}

type stubRunner struct {
	gotModel  string
	gotPrompt string
	out       string
}

func (s *stubRunner) Run(_ context.Context, model, prompt string) (string, error) {
	s.gotModel, s.gotPrompt = model, prompt
	return s.out, nil
}

func TestGenerateRendersPromptAndCleansOutput(t *testing.T) {
	stub := &stubRunner{out: "```\nfeat: add thing\n```"}
	cfg := config.Config{Model: "haiku", Prompt: "files={{files}} diff={{diff}}"}

	got, err := Generate(context.Background(), stub, cfg, config.Vars{
		Diff:  "THEDIFF",
		Files: "a.go",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got != "feat: add thing" {
		t.Errorf("Generate = %q", got)
	}
	if stub.gotModel != "haiku" {
		t.Errorf("model = %q", stub.gotModel)
	}
	if stub.gotPrompt != "files=a.go diff=THEDIFF" {
		t.Errorf("prompt = %q", stub.gotPrompt)
	}
}

func TestGenerateTruncatesDiff(t *testing.T) {
	stub := &stubRunner{out: "msg"}
	cfg := config.Config{Model: "haiku", Prompt: "{{diff}}"}

	_, err := Generate(context.Background(), stub, cfg, config.Vars{
		Diff: strings.Repeat("+x\n", 100_000),
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(stub.gotPrompt) > MaxDiffBytes+1024 {
		t.Errorf("prompt is %d bytes, want the diff truncated", len(stub.gotPrompt))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ai/ -run 'TestCLI|TestGenerate' -v`
Expected: build failure — `undefined: CLI`, `undefined: ErrMissing`, `undefined: Generate`

- [ ] **Step 3: Write minimal implementation**

`internal/ai/generator.go`:

```go
package ai

import (
	"context"

	"github.com/BlazeMV/komit/internal/config"
)

// Runner executes a prompt against a model and returns the raw completion.
type Runner interface {
	Run(ctx context.Context, model, prompt string) (string, error)
}

// Generate renders cfg.Prompt with vars (diff truncated first), runs it and
// cleans the result into a commit message.
func Generate(ctx context.Context, r Runner, cfg config.Config, v config.Vars) (string, error) {
	v.Diff = TruncateDiff(v.Diff)
	out, err := r.Run(ctx, cfg.Model, config.Render(cfg.Prompt, v))
	if err != nil {
		return "", err
	}
	return Clean(out), nil
}
```

`internal/ai/claude.go`:

```go
package ai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrMissing means the claude CLI is not installed or not on PATH.
var ErrMissing = errors.New("claude CLI not found on PATH")

// CLI runs the claude binary in headless print mode. --safe-mode is load-bearing:
// without it the repo's CLAUDE.md, hooks and MCP servers change the output.
type CLI struct {
	Bin string
}

func (c CLI) Run(ctx context.Context, model, prompt string) (string, error) {
	bin := c.Bin
	if bin == "" {
		bin = "claude"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return "", ErrMissing
	}

	cmd := exec.CommandContext(ctx, bin,
		"-p",
		"--model", model,
		"--output-format", "text",
		"--safe-mode",
		"--no-session-persistence",
	)
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("claude: %s", msg)
	}
	return stdout.String(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/ -v`
Expected: PASS. Then one manual check against the real CLI:
`printf 'Reply with exactly: OK' | claude -p --model haiku --output-format text --safe-mode --no-session-persistence` → prints `OK`

- [ ] **Step 5: Commit**

```bash
git add internal/ai/
git commit -m "add claude cli runner and generate"
```

---

### Task 8: File list model — selection and startup rule

**Files:**
- Create: `internal/ui/model.go`, `internal/ui/keys.go`, `internal/ui/theme.go`
- Test: `internal/ui/model_test.go`

**Interfaces:**
- Consumes: `git.Repo`, `git.FileChange`, `git.Branch`, `config.Config`, `ai.Runner`
- Produces:
  - `type item struct { git.FileChange; selected bool }`
  - `type Model struct {...}` implementing `tea.Model`
  - `func New(repo *git.Repo, cfg config.Config, runner ai.Runner) Model`
  - `func (m Model) selectedPaths() []string`
  - `func applyStartupSelection(items []item) []item` — staged files if any are staged, otherwise everything
  - messages: `type statusMsg struct { files []git.FileChange; branch git.Branch }`, `type errMsg struct{ err error }`

- [ ] **Step 1: Write the failing test**

```go
package ui

import (
	"testing"

	"github.com/BlazeMV/komit/internal/git"
)

func items(changes ...git.FileChange) []item {
	out := make([]item, len(changes))
	for i, c := range changes {
		out[i] = item{FileChange: c}
	}
	return out
}

func selectedNames(in []item) []string {
	var out []string
	for _, it := range in {
		if it.selected {
			out = append(out, it.Path)
		}
	}
	return out
}

func TestStartupSelectionPrefersStagedFiles(t *testing.T) {
	got := applyStartupSelection(items(
		git.FileChange{Path: "staged.go", Index: 'M', Worktree: ' '},
		git.FileChange{Path: "unstaged.go", Index: ' ', Worktree: 'M'},
		git.FileChange{Path: "untracked.go", Index: '?', Worktree: '?'},
	))
	names := selectedNames(got)
	if len(names) != 1 || names[0] != "staged.go" {
		t.Errorf("selected %v, want [staged.go]", names)
	}
}

func TestStartupSelectionSelectsAllWhenNothingStaged(t *testing.T) {
	got := applyStartupSelection(items(
		git.FileChange{Path: "a.go", Index: ' ', Worktree: 'M'},
		git.FileChange{Path: "b.go", Index: '?', Worktree: '?'},
	))
	if names := selectedNames(got); len(names) != 2 {
		t.Errorf("selected %v, want both", names)
	}
}

func TestStartupSelectionEmpty(t *testing.T) {
	if got := applyStartupSelection(nil); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestToggleAndSelectAll(t *testing.T) {
	m := Model{items: applyStartupSelection(items(
		git.FileChange{Path: "a.go", Index: ' ', Worktree: 'M'},
		git.FileChange{Path: "b.go", Index: ' ', Worktree: 'M'},
	))}

	m.cursor = 0
	m.toggle()
	if names := selectedNames(m.items); len(names) != 1 || names[0] != "b.go" {
		t.Errorf("after toggle selected %v, want [b.go]", names)
	}

	m.toggleAll() // some selected -> select all
	if len(selectedNames(m.items)) != 2 {
		t.Errorf("toggleAll did not select everything: %v", selectedNames(m.items))
	}

	m.toggleAll() // all selected -> clear
	if len(selectedNames(m.items)) != 0 {
		t.Errorf("toggleAll did not clear: %v", selectedNames(m.items))
	}
}

func TestSelectedPathsUsesOriginalPathForRenames(t *testing.T) {
	m := Model{items: []item{
		{FileChange: git.FileChange{Path: "new.go", Orig: "old.go", Index: 'R'}, selected: true},
		{FileChange: git.FileChange{Path: "b.go", Index: 'M'}, selected: false},
	}}

	got := m.selectedPaths()
	if len(got) != 2 {
		t.Fatalf("selectedPaths = %v, want both sides of the rename", got)
	}
	if got[0] != "new.go" || got[1] != "old.go" {
		t.Errorf("selectedPaths = %v, want [new.go old.go]", got)
	}
}

func TestCursorMovementClamps(t *testing.T) {
	m := Model{items: items(
		git.FileChange{Path: "a.go"},
		git.FileChange{Path: "b.go"},
	)}

	m.moveCursor(-1)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
	m.moveCursor(5)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 (last item)", m.cursor)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -v`
Expected: build failure — `undefined: item`, `undefined: applyStartupSelection`, `undefined: Model`

- [ ] **Step 3: Write minimal implementation**

`internal/ui/model.go`:

```go
// Package ui is komit's bubbletea interface.
package ui

import (
	"github.com/BlazeMV/komit/internal/ai"
	"github.com/BlazeMV/komit/internal/config"
	"github.com/BlazeMV/komit/internal/git"
)

type item struct {
	git.FileChange
	selected bool
}

type focus int

const (
	focusFiles focus = iota
	focusDiff
	focusMessage
)

// Model is the whole TUI state.
type Model struct {
	repo   *git.Repo
	cfg    config.Config
	runner ai.Runner

	items  []item
	cursor int
	branch git.Branch

	focus  focus
	amend  bool
	status string
	err    error

	width, height int
}

// New builds the initial model. Files are loaded by the Init command.
func New(repo *git.Repo, cfg config.Config, runner ai.Runner) Model {
	return Model{repo: repo, cfg: cfg, runner: runner}
}

// statusMsg carries a refreshed working-tree state.
type statusMsg struct {
	files  []git.FileChange
	branch git.Branch
}

// errMsg carries a failure to display without tearing the TUI down.
type errMsg struct{ err error }

// applyStartupSelection selects staged files if anything is staged, otherwise
// everything.
func applyStartupSelection(in []item) []item {
	anyStaged := false
	for _, it := range in {
		if !it.Untracked() && it.Index != ' ' {
			anyStaged = true
			break
		}
	}
	for i := range in {
		if anyStaged {
			in[i].selected = !in[i].Untracked() && in[i].Index != ' '
		} else {
			in[i].selected = true
		}
	}
	return in
}

func (m *Model) toggle() {
	if len(m.items) == 0 {
		return
	}
	m.items[m.cursor].selected = !m.items[m.cursor].selected
}

// toggleAll selects everything unless everything is already selected.
func (m *Model) toggleAll() {
	all := true
	for _, it := range m.items {
		if !it.selected {
			all = false
			break
		}
	}
	for i := range m.items {
		m.items[i].selected = !all
	}
}

func (m *Model) moveCursor(delta int) {
	if len(m.items) == 0 {
		m.cursor = 0
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.items)-1 {
		m.cursor = len(m.items) - 1
	}
}

// selectedPaths lists the pathspecs to hand to git. A rename contributes both
// its new and original path so the deletion side is committed too.
func (m Model) selectedPaths() []string {
	var out []string
	for _, it := range m.items {
		if !it.selected {
			continue
		}
		out = append(out, it.Path)
		if it.Orig != "" {
			out = append(out, it.Orig)
		}
	}
	return out
}

// untrackedSelected lists selected paths that git does not track yet.
func (m Model) untrackedSelected() []string {
	var out []string
	for _, it := range m.items {
		if it.selected && it.Untracked() {
			out = append(out, it.Path)
		}
	}
	return out
}
```

`internal/ui/keys.go` — key names matched against `tea.KeyMsg.String()`:

```go
package ui

const (
	keyUp        = "up"
	keyDown      = "down"
	keyUpAlt     = "k"
	keyDownAlt   = "j"
	keyToggle    = "space" // v2 KeyMsg.String() for the space bar
	keyToggleAll = "a"
	keyDiff      = "d"
	keyGenerate  = "g"
	keyRegen     = "r"
	keyEdit      = "e"
	keyEditor    = "E"
	keyAmend     = "A"
	keyCommit    = "c"
	keyPush      = "P"
	keyFocus     = "tab"
	keyCancel    = "esc"
	keyQuit      = "q"
)
```

`internal/ui/theme.go`:

```go
package ui

import "charm.land/lipgloss/v2"

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	amendStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	paneStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go get charm.land/lipgloss/v2@v2.0.6 && go test ./internal/ui/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/ go.mod go.sum
git commit -m "add file list model with selection rules"
```

---

### Task 9: Wire the bubbletea loop — Init, Update, View

**Files:**
- Modify: `internal/ui/model.go`
- Create: `internal/ui/update.go`, `internal/ui/view.go`
- Test: `internal/ui/update_test.go`

**Interfaces:**
- Consumes: everything from Task 8
- Produces: `func (m Model) Init() tea.Cmd`, `func (m Model) Update(tea.Msg) (tea.Model, tea.Cmd)`, `func (m Model) View() tea.View` (a thin `tea.NewView(m.render())` wrapper) and `func (m Model) render() string`, `func (m Model) loadStatus() tea.Cmd`

- [ ] **Step 1: Write the failing test**

```go
package ui

import (
	"strings"
	"testing"

	"github.com/BlazeMV/komit/internal/git"
	tea "charm.land/bubbletea/v2"
)

// key builds a v2 key press. Named keys use their Code constant; anything else
// is a rune with matching Text, which is what a real terminal delivers.
func key(s string) tea.KeyPressMsg {
	named := map[string]rune{
		"up": tea.KeyUp, "down": tea.KeyDown, "tab": tea.KeyTab,
		"esc": tea.KeyEsc, "enter": tea.KeyEnter, " ": tea.KeySpace,
	}
	if code, ok := named[s]; ok {
		msg := tea.KeyPressMsg{Code: code}
		if s == " " {
			msg.Text = " " // String() still reports "space"
		}
		return msg
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func modelWithFiles() Model {
	m := Model{width: 100, height: 30}
	m = update(m, statusMsg{
		files: []git.FileChange{
			{Path: "a.go", Index: ' ', Worktree: 'M'},
			{Path: "b.go", Index: '?', Worktree: '?'},
		},
		branch: git.Branch{Name: "master", Ahead: 2},
	})
	return m
}

func TestStatusMsgPopulatesItems(t *testing.T) {
	m := modelWithFiles()
	if len(m.items) != 2 {
		t.Fatalf("items = %+v, want 2", m.items)
	}
	if len(m.selectedPaths()) != 2 {
		t.Errorf("nothing staged, so both should be selected: %v", m.selectedPaths())
	}
}

func TestViewShowsFilesBranchAndHelp(t *testing.T) {
	out := modelWithFiles().View().Content
	for _, want := range []string{"a.go", "b.go", "master", "commit"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
}

func TestViewMarksSelectionAndPartialStaging(t *testing.T) {
	m := Model{width: 100, height: 30}
	m = update(m, statusMsg{files: []git.FileChange{
		{Path: "partial.go", Index: 'M', Worktree: 'M'},
	}})

	out := m.View().Content
	if !strings.Contains(out, "±") {
		t.Errorf("view does not mark the partially staged file:\n%s", out)
	}
}

func TestKeyToggleUpdatesSelection(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key(" "))
	if got := m.selectedPaths(); len(got) != 1 || got[0] != "b.go" {
		t.Errorf("selected %v, want [b.go]", got)
	}
}

func TestKeyNavigationMovesCursor(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("down"))
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}
	m = update(m, key("j"))
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want to clamp at 1", m.cursor)
	}
	m = update(m, key("k"))
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
}

func TestErrMsgIsDisplayedNotFatal(t *testing.T) {
	m := update(modelWithFiles(), errMsg{err: errFake{}})
	out := m.View().Content
	if !strings.Contains(out, "boom") {
		t.Errorf("view does not show the error:\n%s", out)
	}
	if len(m.items) != 2 {
		t.Error("error wiped the file list")
	}
}

type errFake struct{}

func (errFake) Error() string { return "boom" }

func TestEmptyRepoShowsEmptyState(t *testing.T) {
	m := Model{width: 100, height: 30}
	m = update(m, statusMsg{files: nil, branch: git.Branch{Name: "master"}})
	if out := m.View().Content; !strings.Contains(out, "no changes") {
		t.Errorf("view missing empty state:\n%s", out)
	}
}

func TestQuitReturnsQuitCommand(t *testing.T) {
	m := modelWithFiles()
	_, cmd := m.Update(key("q"))
	if cmd == nil {
		t.Fatal("q produced no command, want tea.Quit")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("quit command produced no message")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestStatusMsg|TestView|TestKey|TestErrMsg|TestEmpty|TestQuit' -v`
Expected: build failure — `m.Update undefined`, `m.View undefined`

- [ ] **Step 3: Write minimal implementation**

`internal/ui/update.go`:

```go
package ui

import (
	tea "charm.land/bubbletea/v2"
)

func (m Model) Init() tea.Cmd {
	return m.loadStatus()
}

// loadStatus refreshes the working tree and branch state off the UI goroutine.
func (m Model) loadStatus() tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		files, err := repo.Status()
		if err != nil {
			return errMsg{err}
		}
		branch, err := repo.BranchState()
		if err != nil {
			return errMsg{err}
		}
		return statusMsg{files: files, branch: branch}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case statusMsg:
		items := make([]item, len(msg.files))
		for i, f := range msg.files {
			items[i] = item{FileChange: f}
		}
		m.items = applyStartupSelection(items)
		m.branch = msg.branch
		m.moveCursor(0)
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyQuit, "ctrl+c":
		return m, tea.Quit
	case keyUp, keyUpAlt:
		m.moveCursor(-1)
	case keyDown, keyDownAlt:
		m.moveCursor(1)
	case keyToggle:
		m.toggle()
	case keyToggleAll:
		m.toggleAll()
	}
	return m, nil
}
```

`internal/ui/view.go`:

```go
package ui

import (
	"fmt"
	"strings"
)

const helpLine = "space sel · a all · d diff · g gen · r regen · e edit · c commit · P push · q quit"

// bubbletea v2's Model interface is View() tea.View, not View() string.
// render() holds the actual layout; View wraps it.
func (m Model) View() tea.View {
	return tea.NewView(m.render())
}

func (m Model) render() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("komit"))
	b.WriteString(dimStyle.Render(" · " + m.branchLine()))
	if m.amend {
		b.WriteString(" " + amendStyle.Render("AMEND"))
	}
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString(dimStyle.Render("no changes in this repository"))
		b.WriteString("\n\n")
	} else {
		b.WriteString(m.fileList())
		b.WriteString("\n")
	}

	if m.err != nil {
		b.WriteString(errStyle.Render(m.err.Error()))
		b.WriteString("\n")
	} else if m.status != "" {
		b.WriteString(dimStyle.Render(m.status))
		b.WriteString("\n")
	}

	b.WriteString(dimStyle.Render(helpLine))
	return b.String()
}

func (m Model) branchLine() string {
	s := m.branch.Name
	if m.branch.Ahead > 0 {
		s += fmt.Sprintf(" ↑%d", m.branch.Ahead)
	}
	if m.branch.Behind > 0 {
		s += fmt.Sprintf(" ↓%d", m.branch.Behind)
	}
	return s
}

func (m Model) fileList() string {
	var b strings.Builder
	for i, it := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("▸ ")
		}
		box := "[ ]"
		if it.selected {
			box = selectedStyle.Render("[x]")
		}
		mark := " "
		if it.PartiallyStaged() {
			mark = "±"
		}
		fmt.Fprintf(&b, "%s%s %s%s %s\n", cursor, box, it.Letter(), mark, it.Path)
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go get charm.land/bubbletea/v2@v2.0.8 && go test ./internal/ui/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/ go.mod go.sum
git commit -m "wire bubbletea loop with file list view"
```

---

### Task 10: Diff pane and message editor

**Files:**
- Modify: `internal/ui/model.go`, `internal/ui/update.go`, `internal/ui/view.go`
- Test: `internal/ui/panes_test.go`

**Interfaces:**
- Consumes: Task 9's loop
- Produces: `diffMsg` type, `func (m Model) loadDiff() tea.Cmd`, `func (m Model) resizePanes()`, `nextFocus`/`moveFocus` focus helpers, fields `diff viewport.Model`, `msgInput textarea.Model`, `showDiff bool`, `diffPath string`; `d` toggles the pane, `tab` cycles focus (skipping `focusDiff` while hidden, routing keys to `m.diff.Update` while focused), `e` focuses the editor, `E` shells out to `$EDITOR`
- `diffMsg` is discarded unless its path matches the file under the cursor — two in-flight loads can resolve out of order.
- Panes must be sized from `resizePanes()` called at the top of `Update`, not only in the `tea.WindowSizeMsg` case: a zero-sized viewport renders as an empty string.

- [ ] **Step 1: Write the failing test**

```go
package ui

import (
	"strings"
	"testing"

	"github.com/BlazeMV/komit/internal/git"
)

func TestDiffPaneTogglesAndShowsContent(t *testing.T) {
	m := modelWithFiles()
	if strings.Contains(m.View().Content, "@@") {
		t.Fatal("diff pane visible before it was requested")
	}

	m = update(m, key("d"))
	m = update(m, diffMsg{path: "a.go", body: "@@ -1 +1 @@\n-old\n+new\n"})

	out := m.View().Content
	if !strings.Contains(out, "+new") {
		t.Errorf("diff pane missing content:\n%s", out)
	}

	m = update(m, key("d"))
	if strings.Contains(m.View().Content, "+new") {
		t.Error("diff pane did not hide")
	}
}

func TestDiffLoadsForCursorFileNotSelection(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("d"))
	m = update(m, key("down")) // cursor on b.go
	m = update(m, diffMsg{path: "b.go", body: "@@ b @@"})

	if !strings.Contains(m.View().Content, "b.go") {
		t.Error("diff pane header does not name the cursor file")
	}
}

func TestMessageEditorAcceptsTyping(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("e"))
	for _, r := range "fix: thing" {
		m = update(m, key(string(r)))
	}
	if got := m.message(); got != "fix: thing" {
		t.Errorf("message = %q, want %q", got, "fix: thing")
	}
	if !strings.Contains(m.View().Content, "fix: thing") {
		t.Error("view does not show the typed message")
	}
}

func TestEscapeLeavesEditorAndKeysBindAgain(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("e"))
	m = update(m, key("a")) // typed, not select-all
	m = update(m, key("esc"))
	m = update(m, key("a")) // bound to toggleAll again, not typed

	if m.message() != "a" {
		t.Errorf("message = %q, want the typed 'a'", m.message())
	}
	// modelWithFiles() starts fully selected, so toggleAll clears it.
	if len(m.selectedPaths()) != 0 {
		t.Errorf("toggleAll did not run after esc: %v", m.selectedPaths())
	}
}

func TestGeneratedMsgFillsEditor(t *testing.T) {
	m := modelWithFiles()
	m = update(m, generatedMsg{message: "feat: generated"})
	if m.message() != "feat: generated" {
		t.Errorf("message = %q", m.message())
	}
	if m.busy {
		t.Error("busy flag still set after generation finished")
	}
}

func TestPartialStagingWarningShownForSelectedFile(t *testing.T) {
	m := Model{width: 100, height: 30}
	m = update(m, statusMsg{files: []git.FileChange{
		{Path: "partial.go", Index: 'M', Worktree: 'M'},
	}})
	if !strings.Contains(m.View().Content, "±") {
		t.Error("no partial-staging marker")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestDiff|TestMessage|TestEscape|TestGenerated|TestPartial' -v`
Expected: build failure — `undefined: diffMsg`, `undefined: generatedMsg`, `m.message undefined`

- [ ] **Step 3: Write minimal implementation**

Add to `model.go`:

```go
import (
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
)

// added to Model:
//   showDiff bool
//   diffPath string
//   diff     viewport.Model
//   msgInput textarea.Model
//   busy     bool

// diffMsg carries a loaded diff for one file.
type diffMsg struct {
	path string
	body string
}

// generatedMsg carries a finished commit message.
type generatedMsg struct{ message string }

func newMessageInput() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "commit message — press g to generate, e to type"
	ta.ShowLineNumbers = false
	ta.SetHeight(3)
	return ta
}

func (m Model) message() string {
	return strings.TrimSpace(m.msgInput.Value())
}
```

`New` must initialise both components:

```go
func New(repo *git.Repo, cfg config.Config, runner ai.Runner) Model {
	return Model{
		repo:     repo,
		cfg:      cfg,
		runner:   runner,
		diff:     viewport.New(),
		msgInput: newMessageInput(),
	}
}
```

Tests construct `Model{width: 100, height: 30}` directly, so `Update` must tolerate a zero-value `msgInput`; initialise it lazily at the top of `Update`:

```go
if m.msgInput.Placeholder == "" {
	m.msgInput = newMessageInput()
}
```

Key handling in `handleKey`, before the existing switch:

```go
// While the editor has focus, all runes belong to it.
if m.focus == focusMessage {
	switch msg.String() {
	case keyCancel:
		m.focus = focusFiles
		m.msgInput.Blur()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.msgInput, cmd = m.msgInput.Update(msg)
	return m, cmd
}
```

New cases in the main switch:

```go
case keyDiff:
	m.showDiff = !m.showDiff
	if m.showDiff {
		return m, m.loadDiff()
	}
case keyEdit:
	m.focus = focusMessage
	return m, m.msgInput.Focus()
```

Cursor movement reloads the diff when the pane is open:

```go
case keyUp, keyUpAlt:
	m.moveCursor(-1)
	if m.showDiff {
		return m, m.loadDiff()
	}
```
(same for down)

New message cases in `Update`:

```go
case diffMsg:
	m.diffPath = msg.path
	m.diff.SetContent(msg.body)
	m.diff.GotoTop()
	return m, nil

case generatedMsg:
	m.busy = false
	m.msgInput.SetValue(msg.message)
	return m, nil
```

`loadDiff`:

```go
func (m Model) loadDiff() tea.Cmd {
	if len(m.items) == 0 {
		return nil
	}
	repo, it := m.repo, m.items[m.cursor]
	return func() tea.Msg {
		var cleanup func()
		if it.Untracked() {
			c, err := repo.MarkIntent([]string{it.Path})
			if err != nil {
				return errMsg{err}
			}
			cleanup = c
		}
		body, err := repo.Diff([]string{it.Path})
		if cleanup != nil {
			cleanup()
		}
		if err != nil {
			return errMsg{err}
		}
		if strings.TrimSpace(body) == "" {
			body = "(no textual diff)"
		}
		return diffMsg{path: it.Path, body: body}
	}
}
```

`View` renders the diff pane beside the list when `m.showDiff`, using
`lipgloss.JoinHorizontal(lipgloss.Top, listPane, diffPane)`, and always renders
the message editor under it via `m.msgInput.View()`. Size both panes from
`m.width`/`m.height` in the `tea.WindowSizeMsg` case:

```go
case tea.WindowSizeMsg:
	m.width, m.height = msg.Width, msg.Height
	m.diff.SetWidth(msg.Width/2 - 4)
	m.diff.SetHeight(msg.Height - 12)
	m.msgInput.SetWidth(msg.Width - 4)
	return m, nil
```

`E` opens `$EDITOR` on a temp file via `tea.ExecProcess`, reading the result back:

```go
case keyEditor:
	return m, m.openEditor()
```

```go
func (m Model) openEditor() tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	f, err := os.CreateTemp("", "komit-*.txt")
	if err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	path := f.Name()
	f.WriteString(m.msgInput.Value())
	f.Close()

	return tea.ExecProcess(exec.Command(editor, path), func(err error) tea.Msg {
		if err != nil {
			os.Remove(path)
			return errMsg{err}
		}
		data, readErr := os.ReadFile(path)
		os.Remove(path)
		if readErr != nil {
			return errMsg{readErr}
		}
		return generatedMsg{message: strings.TrimSpace(string(data))}
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go get charm.land/bubbles/v2@v2.1.1 && go test ./internal/ui/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/ go.mod go.sum
git commit -m "add diff pane and message editor"
```

---

### Task 11: Generate, regenerate, commit, push, amend

**Files:**
- Modify: `internal/ui/model.go`, `internal/ui/update.go`, `internal/ui/view.go`
- Test: `internal/ui/actions_test.go`

**Interfaces:**
- Consumes: Tasks 9–10, `ai.Generate`, `git.Repo.Commit/Push/HeadPushed`
- Generate/regen/commit/push are refused while `busy`; `m.cancel` is scoped to generation only (reset on success, error and cancel) so `esc` cannot misreport an uncancellable commit as cancelled.
- Refusal branches must clear `m.err` — `render()` prioritises `err` over `status`, so a stale error masks the refusal.
- `commit()` primes the spinner tick the same way `generate()` does.
- Produces: `func (m Model) generate(nudge string) tea.Cmd`, `func (m Model) commit(push bool) tea.Cmd`, messages `committedMsg`, `busyMsg`; fields `spinner spinner.Model`, `cancel context.CancelFunc`, `nudge textinput.Model`

- [ ] **Step 1: Write the failing test**

```go
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
```

Add the two helpers to `internal/ui/testhelp_test.go`:

```go
package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/BlazeMV/komit/internal/git"
	tea "charm.land/bubbletea/v2"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run 'TestGenerate|TestCommit|TestAmend' -v`
Expected: build failure — `undefined: committedMsg`, `m.headPushed undefined`

- [ ] **Step 3: Write minimal implementation**

Add to `model.go`:

```go
// added to Model:
//   busy       bool
//   headPushed bool
//   spinner    spinner.Model
//   cancel     context.CancelFunc
//   nudging    bool
//   nudge      textinput.Model

// committedMsg reports a finished commit (and push, if requested).
type committedMsg struct{ summary string }

// spinnerTick is bubbles' spinner tick, aliased so tests can recognise it.
type spinnerTick = spinner.TickMsg
```

`New` must also initialise the spinner and the nudge input, alongside the
viewport and textarea added in Task 10. Imports:
`charm.land/bubbles/v2/spinner`, `charm.land/bubbles/v2/textinput`.

```go
func New(repo *git.Repo, cfg config.Config, runner ai.Runner) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	ni := textinput.New()
	ni.Placeholder = "how should it change? (enter to regenerate, esc to cancel)"

	return Model{
		repo:     repo,
		cfg:      cfg,
		runner:   runner,
		diff:     viewport.New(),
		msgInput: newMessageInput(),
		spinner:  sp,
		nudge:    ni,
	}
}
```

`generate`:

```go
const generateTimeout = 30 * time.Second

func (m *Model) generate(nudge string) tea.Cmd {
	paths := m.selectedPaths()
	if len(paths) == 0 {
		m.status = "no files selected"
		return nil
	}

	repo, cfg, runner := m.repo, m.cfg, m.runner
	untracked := m.untrackedSelected()
	branch := m.branch.Name
	amend := m.amend
	prompt := cfg.Prompt
	if nudge != "" {
		prompt += "\n\nRevise according to this instruction: " + nudge
	}
	cfg.Prompt = prompt

	ctx, cancel := context.WithTimeout(context.Background(), generateTimeout)
	m.cancel = cancel
	m.busy = true
	m.status = "generating…"
	m.err = nil

	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		defer cancel()

		cleanup := func() {}
		if len(untracked) > 0 {
			c, err := repo.MarkIntent(untracked)
			if err != nil {
				return errMsg{err}
			}
			cleanup = c
		}
		defer cleanup()

		diffOf := repo.Diff
		if amend {
			diffOf = repo.DiffAmend
		}
		diff, err := diffOf(paths)
		if err != nil {
			return errMsg{err}
		}
		recent, err := repo.RecentCommits(10)
		if err != nil {
			return errMsg{err}
		}

		out, err := ai.Generate(ctx, runner, cfg, config.Vars{
			Diff:          diff,
			Files:         strings.Join(paths, ", "),
			Branch:        branch,
			RecentCommits: recent,
		})
		if err != nil {
			return errMsg{err}
		}
		return generatedMsg{message: out}
	})
}
```

`commit`:

```go
func (m *Model) commit(push bool) tea.Cmd {
	paths := m.selectedPaths()
	if len(paths) == 0 {
		m.status = "no files selected"
		return nil
	}
	if m.message() == "" {
		m.status = "write a message first — g to generate, e to type"
		return nil
	}

	repo, msg, amend := m.repo, m.message(), m.amend
	untracked := m.untrackedSelected()
	count := len(paths)
	m.busy = true
	m.err = nil

	return func() tea.Msg {
		cleanup := func() {}
		if len(untracked) > 0 {
			c, err := repo.MarkIntent(untracked)
			if err != nil {
				return errMsg{err}
			}
			cleanup = c
		}

		if err := repo.Commit(paths, msg, amend); err != nil {
			cleanup() // commit failed: leave the index as we found it
			return errMsg{err}
		}

		summary := fmt.Sprintf("committed %d file(s)", count)
		if amend {
			summary = "amended HEAD"
		}
		if push {
			if err := repo.Push(); err != nil {
				return errMsg{err}
			}
			summary += " and pushed"
		}
		return committedMsg{summary: summary}
	}
}
```

New key cases:

```go
case keyGenerate:
	return m, m.generate("")
case keyRegen:
	m.nudging = true
	m.nudge.SetValue("")
	return m, m.nudge.Focus()
case keyCommit:
	return m, m.commit(false)
case keyPush:
	return m, m.commit(true)
case keyAmend:
	if !m.amend && m.headPushed {
		m.status = "HEAD is already pushed — amending would rewrite published history"
		return m, nil
	}
	m.amend = !m.amend
case keyCancel:
	if m.busy && m.cancel != nil {
		m.cancel()
		m.busy = false
		m.status = "generation cancelled"
	}
```

Nudge input is handled ahead of the main switch, like the message editor: `enter` runs `m.generate(m.nudge.Value())` and clears `m.nudging`; `esc` cancels it.

New message cases:

```go
case committedMsg:
	m.busy = false
	m.msgInput.SetValue("")
	m.amend = false
	m.status = msg.summary
	return m, m.loadStatus()

case spinner.TickMsg:
	if !m.busy {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
```

`errMsg` must also clear `busy`. `statusMsg` gains `headPushed` — extend the struct and set it in `loadStatus` from `repo.HeadPushed()`.

`View` renders `m.spinner.View() + " " + m.status` while `m.busy`, and the nudge
input line while `m.nudging`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS across all packages

- [ ] **Step 5: Commit**

```bash
git add internal/ui/
git commit -m "add generate, regenerate, commit, push and amend"
```

---

### Task 12: `cmd/komit` entrypoint and `komit init`

**Files:**
- Create: `cmd/komit/main.go`
- Test: `cmd/komit/main_test.go`

**Interfaces:**
- Consumes: `git.Open`, `config.Load`, `config.Default`, `config.UserPath`, `ui.New`, `ai.CLI`
- Produces: `func run(args []string, stdout, stderr io.Writer) int`, `func initConfig(stdout io.Writer) error`; `main` is a thin wrapper around `run`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--version"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "komit") {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestOutsideRepoExitsWithMessage(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code == 0 {
		t.Fatal("exit code = 0 outside a repository, want non-zero")
	}
	if !strings.Contains(strings.ToLower(errOut.String()), "git repository") {
		t.Errorf("stderr = %q, want an explanation", errOut.String())
	}
}

func TestInitWritesDefaultConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)

	var out, errOut bytes.Buffer
	if code := run([]string{"init"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}

	path := filepath.Join(home, "komit", "config.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !strings.Contains(string(data), "{{diff}}") {
		t.Errorf("written config has no prompt:\n%s", data)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("stdout = %q, want the written path", out.String())
	}
}

func TestInitDoesNotClobberExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	dir := filepath.Join(home, "komit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("model: mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run([]string{"init"}, &out, &errOut); code == 0 {
		t.Error("exit code = 0, want non-zero when the config already exists")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "model: mine\n" {
		t.Errorf("existing config was overwritten: %q", data)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/komit/ -v`
Expected: build failure — `undefined: run`

- [ ] **Step 3: Write minimal implementation**

```go
// Command komit is a TUI for selecting changed files and committing them with a
// Claude-generated message.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/BlazeMV/komit/internal/ai"
	"github.com/BlazeMV/komit/internal/config"
	"github.com/BlazeMV/komit/internal/git"
	"github.com/BlazeMV/komit/internal/ui"
	tea "charm.land/bubbletea/v2"
)

// version is set by the linker at release time.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version":
			fmt.Fprintf(stdout, "komit %s\n", version)
			return 0
		case "init":
			if err := initConfig(stdout); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			return 0
		default:
			fmt.Fprintf(stderr, "unknown argument %q — usage: komit [init|--version]\n", args[0])
			return 2
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	repo, err := git.Open(cwd)
	if err != nil {
		fmt.Fprintln(stderr, "not a git repository (or git is not installed)")
		return 1
	}
	cfg, err := config.Load(repo.Dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	p := tea.NewProgram(ui.New(repo, cfg, ai.CLI{}))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// initConfig writes the built-in defaults to the user config path, refusing to
// overwrite an existing file.
func initConfig(stdout io.Writer) error {
	path, err := config.UserPath()
	if err != nil {
		return err
	}
	// O_EXCL is what decides: a bare Stat+WriteFile truncates a file that
	// appears in between, destroying the user's prompt.
	// (create with os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644),
	// treat os.IsExist as "already exists", and os.Remove on a failed write)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cfg := config.Default()
	body := fmt.Sprintf("model: %s\nprompt: |\n", cfg.Model)
	for _, line := range splitLines(cfg.Prompt) {
		body += "  " + line + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\n", path)
	return nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... && go build ./... && go vet ./...`
Expected: PASS, binary builds, no vet output

- [ ] **Step 5: Commit**

```bash
git add cmd/
git commit -m "add komit entrypoint and init command"
```

---

### Task 13: End-to-end TUI test with teatest

**Files:**
- Create: `internal/ui/e2e_test.go`

**Interfaces:**
- Consumes: everything
- Produces: nothing — this task only adds coverage of the full loop through the real bubbletea runtime

- [ ] **Step 1: Write the failing test**

```go
package ui

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/BlazeMV/komit/internal/config"
	tea "charm.land/bubbletea/v2"
	"charm.land/x/exp/teatest/v2"
)

type e2eRunner struct{}

func (e2eRunner) Run(context.Context, string, string) (string, error) {
	return "feat: end to end\n", nil
}

func TestSelectGenerateCommitFlow(t *testing.T) {
	repo := newUIRepo(t) // one committed file, modified in the working tree
	m := New(repo, config.Config{Model: "haiku", Prompt: "{{diff}}"}, e2eRunner{})

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

func TestAmendRefusalFlow(t *testing.T) {
	repo := newUIRepo(t)
	m := New(repo, config.Config{Model: "haiku", Prompt: "{{diff}}"}, e2eRunner{})
	m.headPushed = true

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go get charm.land/x/exp/teatest/v2@latest && go test ./internal/ui/ -run TestSelectGenerateCommitFlow -v`
Expected: FAIL — most likely `headPushed` is not exported/settable from the test or timing assertions miss; fix the model, not the test's intent

- [ ] **Step 3: Make it pass**

Adjust `Model` so the flow works end to end through the real runtime: `Init` must load status, `g` must run generation to completion, `c` must commit and refresh. No new behavior — this task exposes wiring bugs from Tasks 9–11.

- [ ] **Step 4: Run the full suite**

Run: `go test ./... -race`
Expected: PASS with the race detector clean

- [ ] **Step 5: Commit**

```bash
git add internal/ui/ go.mod go.sum
git commit -m "add end to end tui tests"
```

---

### Task 14: Packaging and release

**Files:**
- Create: `README.md`, `.goreleaser.yml`, `.github/workflows/release.yml`
- Modify: `cmd/komit/main.go` (version var already present)

**Interfaces:**
- Consumes: the working binary
- Produces: `go install github.com/BlazeMV/komit@latest`, GitHub release binaries for darwin/linux × amd64/arm64

- [ ] **Step 1: Write the release config**

`.goreleaser.yml`:

```yaml
version: 2
project_name: komit
builds:
  - main: ./cmd/komit
    binary: komit
    env: [CGO_ENABLED=0]
    goos: [darwin, linux]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X main.version={{.Version}}
archives:
  - formats: [tar.gz]
checksum:
  name_template: checksums.txt
changelog:
  use: git
```

`.github/workflows/release.yml`:

```yaml
name: release
on:
  push:
    tags: ["v*"]
permissions:
  contents: write
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version: "1.25" }
      - run: go test ./...
      - uses: goreleaser/goreleaser-action@v6
        with: { args: release --clean }
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 2: Write the README**

Keep it to: one-line description, a screenshot placeholder, install (`go install github.com/BlazeMV/komit@latest`), the keymap table from the spec, and the config example from `internal/config/default.yml`. Nothing else.

- [ ] **Step 3: Verify the build locally**

Run:
```bash
go build -ldflags "-X main.version=v0.1.0" -o /tmp/komit ./cmd/komit
/tmp/komit --version
```
Expected: `komit v0.1.0`

- [ ] **Step 4: Dogfood it**

Run `komit` inside the komit repo itself, select the packaging files, press `g`, then `c`. Confirm HEAD contains only those files.

- [ ] **Step 5: Commit and tag**

```bash
git add README.md .goreleaser.yml .github/
git commit -m "add packaging and release workflow"
git tag v0.1.0
```

Push and create the GitHub repo only when you say so — the plan stops at the local tag.

---

## Done criteria

- [ ] `go test ./... -race` passes
- [ ] Selecting a subset and committing leaves every unselected file's index entry untouched (Task 4 tests)
- [ ] Untracked and partially-staged files commit correctly; quitting without committing leaves the index as it was found
- [ ] `.komit.yml` overrides the global prompt per key
- [ ] Amend is refused when HEAD is already on the upstream
- [ ] `claude` missing → TUI still usable with a typed message
- [ ] `go install github.com/BlazeMV/komit@latest` works from a clean machine
