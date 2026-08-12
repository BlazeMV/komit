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
// ahead/behind it is. A missing upstream or an unborn branch is not an error.
func (r *Repo) BranchState() (Branch, error) {
	name, err := r.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		unborn, nameErr := r.run("branch", "--show-current")
		if nameErr != nil {
			return Branch{}, err
		}
		return Branch{Name: strings.TrimSpace(unborn)}, nil
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
