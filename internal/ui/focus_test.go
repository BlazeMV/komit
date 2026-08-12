package ui

import (
	"strings"
	"testing"
)

func TestTabCyclesFocusSkipsHiddenDiffPane(t *testing.T) {
	m := modelWithFiles()
	if m.focus != focusFiles {
		t.Fatalf("focus = %v, want focusFiles", m.focus)
	}

	m = update(m, key("tab"))
	if m.focus != focusMessage {
		t.Errorf("focus = %v, want focusMessage (diff hidden, should be skipped)", m.focus)
	}

	m = update(m, key("tab"))
	if m.focus != focusFiles {
		t.Errorf("focus = %v, want focusFiles", m.focus)
	}
}

func TestTabCyclesFocusThroughDiffPaneWhenVisible(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("d")) // open diff pane

	m = update(m, key("tab"))
	if m.focus != focusDiff {
		t.Fatalf("focus = %v, want focusDiff", m.focus)
	}

	m = update(m, key("tab"))
	if m.focus != focusMessage {
		t.Errorf("focus = %v, want focusMessage", m.focus)
	}

	m = update(m, key("tab"))
	if m.focus != focusFiles {
		t.Errorf("focus = %v, want focusFiles", m.focus)
	}
}

func TestScrollKeyWhileDiffFocusedReachesViewport(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("d"))
	m = update(m, diffMsg{path: "a.go", body: strings.Repeat("line\n", 60)})
	m = update(m, key("tab"))
	if m.focus != focusDiff {
		t.Fatalf("focus = %v, want focusDiff", m.focus)
	}

	before := m.diff.YOffset()
	m = update(m, key("down"))
	if got := m.diff.YOffset(); got <= before {
		t.Errorf("YOffset = %d, want > %d after scrolling down while diff-focused", got, before)
	}
}

// D6: esc from focusDiff must return focus to the file list.
func TestEscFromFocusDiffReturnsToFiles(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("d"))
	m = update(m, key("tab"))
	if m.focus != focusDiff {
		t.Fatalf("focus = %v, want focusDiff", m.focus)
	}

	m = update(m, key("esc"))
	if m.focus != focusFiles {
		t.Errorf("focus = %v, want focusFiles", m.focus)
	}
}

// D7: q must still quit while the diff pane has focus, not be swallowed by
// the viewport.
func TestKeyQFromFocusDiffQuits(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("d"))
	m = update(m, key("tab"))
	if m.focus != focusDiff {
		t.Fatalf("focus = %v, want focusDiff", m.focus)
	}

	_, cmd := m.Update(key("q"))
	if cmd == nil {
		t.Fatal("q from focusDiff produced no command, want tea.Quit")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("quit command produced no message")
	}
}

// D7: d must hide the diff pane and return focus, not be swallowed by the
// viewport.
func TestKeyDFromFocusDiffHidesPane(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("d"))
	m = update(m, key("tab"))
	if m.focus != focusDiff {
		t.Fatalf("focus = %v, want focusDiff", m.focus)
	}

	m = update(m, key("d"))
	if m.showDiff {
		t.Error("d from focusDiff did not hide the pane")
	}
	if m.focus == focusDiff {
		t.Error("focus still on the now-hidden diff pane")
	}
}

func TestStaleDiffMsgForNonCursorFileIsDiscarded(t *testing.T) {
	m := modelWithFiles()
	m = update(m, key("d"))
	m = update(m, diffMsg{path: "a.go", body: "@@ real @@"})
	// A diff for a file other than the one under the cursor arrives late.
	m = update(m, diffMsg{path: "b.go", body: "@@ stale @@"})

	out := m.View().Content
	if strings.Contains(out, "stale") {
		t.Error("stale diff for non-cursor file was applied")
	}
	if !strings.Contains(out, "real") {
		t.Error("valid diff for cursor file was discarded")
	}
}
