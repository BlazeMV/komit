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
