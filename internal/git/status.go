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
