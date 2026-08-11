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
