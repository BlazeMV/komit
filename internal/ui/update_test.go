package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/BlazeMV/komit/internal/git"
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
