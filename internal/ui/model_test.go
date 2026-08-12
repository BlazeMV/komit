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
