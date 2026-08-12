package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Repo is a handle on a git working tree, identified by its toplevel directory.
// It carries locks, so it must be used as a pointer and never copied.
type Repo struct {
	Dir string

	index sync.Mutex

	mu      sync.Mutex
	pending map[int]func() error
	nextID  int
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

// runTolerating treats one exit status as success: `git diff --no-index` exits 1
// whenever the two sides differ, which is the ordinary case here.
func (r *Repo) runTolerating(code int, args ...string) (string, error) {
	out, err := r.run(args...)
	var gitErr *Error
	var exitErr *exec.ExitError
	if errors.As(err, &gitErr) && errors.As(gitErr.Err, &exitErr) && exitErr.ExitCode() == code {
		return out, nil
	}
	return out, err
}

// LockIndex serialises index-mutating operations against each other and against
// DrainIntents.
func (r *Repo) LockIndex() { r.index.Lock() }

// UnlockIndex releases LockIndex.
func (r *Repo) UnlockIndex() { r.index.Unlock() }

// registerIntent returns the undo for paths MarkIntent staged. It deregisters
// only after git succeeds, so a failed undo stays pending for DrainIntents.
func (r *Repo) registerIntent(paths []string) func() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		r.pending = make(map[int]func() error)
	}
	id := r.nextID
	r.nextID++

	undo := func() error {
		r.mu.Lock()
		_, live := r.pending[id]
		r.mu.Unlock()
		if !live {
			return nil
		}
		if err := r.unmarkIntent(paths); err != nil {
			return err
		}
		r.mu.Lock()
		delete(r.pending, id)
		r.mu.Unlock()
		return nil
	}
	r.pending[id] = undo
	return undo
}

func (r *Repo) unmarkIntent(paths []string) error {
	if r.hasHEAD() {
		_, err := r.run(append([]string{"reset", "--quiet", "HEAD", "--"}, paths...)...)
		return err
	}
	_, err := r.run(append([]string{"rm", "--cached", "--quiet", "--force", "--ignore-unmatch", "--"}, paths...)...)
	return err
}

// DrainIntents undoes every MarkIntent whose cleanup has not run yet; quitting
// abandons the goroutines that would otherwise do it. Draining twice is safe.
func (r *Repo) DrainIntents() error {
	r.mu.Lock()
	undos := make([]func() error, 0, len(r.pending))
	for _, undo := range r.pending {
		undos = append(undos, undo)
	}
	r.mu.Unlock()
	if len(undos) == 0 {
		return nil
	}

	r.LockIndex()
	defer r.UnlockIndex()
	var firstErr error
	for _, undo := range undos {
		if err := undo(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Status lists every change in the working tree, including untracked files.
func (r *Repo) Status() ([]FileChange, error) {
	out, err := r.run("status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	return ParseStatus(out)
}

func itoa(n int) string { return strconv.Itoa(n) }
