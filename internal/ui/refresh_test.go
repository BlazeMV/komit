package ui

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/BlazeMV/komit/internal/config"
	"github.com/BlazeMV/komit/internal/git"
	"github.com/charmbracelet/x/exp/teatest/v2"
)

// refreshModel is a focused two-file model backed by a real repo, so the
// commands a refresh returns can be drained. Callers that drain must leave the
// interval at 0: a scheduled poll is a tea.Tick, and draining one would block.
func refreshModel(t *testing.T, r config.Refresh) Model {
	t.Helper()
	m := New(newUIRepo(t), config.Config{Refresh: r}, &fakeRunner{})
	m.width, m.height = 100, 30
	return update(m, statusMsg{
		files: []git.FileChange{
			{Path: "a.go", Index: ' ', Worktree: 'M'},
			{Path: "b.go", Index: '?', Worktree: '?'},
		},
		branch: git.Branch{Name: "master"},
	})
}

func paths(in []item) []string {
	out := make([]string, len(in))
	for i, it := range in {
		out[i] = it.Path
	}
	return out
}

func fresh(names ...string) []item {
	out := make([]item, len(names))
	for i, n := range names {
		out[i] = item{FileChange: git.FileChange{Path: n, Index: ' ', Worktree: 'M'}}
	}
	return out
}

func selection(in []item) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, it := range in {
		out[it.Path] = it.selected
	}
	return out
}

func TestMergeSelectionKeepsEachFilesTick(t *testing.T) {
	old := fresh("a.go", "b.go", "c.go")
	old[0].selected = true
	old[2].selected = true

	got := selection(mergeSelection(old, fresh("a.go", "b.go", "c.go")))
	want := map[string]bool{"a.go": true, "b.go": false, "c.go": true}
	for path, sel := range want {
		if got[path] != sel {
			t.Errorf("%s selected = %v, want %v", path, got[path], sel)
		}
	}
}

func TestMergeSelectionNewFileJoinsAnAllSelectedList(t *testing.T) {
	old := fresh("a.go")
	old[0].selected = true

	got := selection(mergeSelection(old, fresh("a.go", "new.go")))
	if !got["new.go"] {
		t.Error("new file arrived unselected even though everything was selected")
	}
}

func TestMergeSelectionNewFileStaysOutOfACuratedList(t *testing.T) {
	old := fresh("a.go", "b.go")
	old[0].selected = true // b.go deliberately left out

	got := selection(mergeSelection(old, fresh("a.go", "b.go", "new.go")))
	if got["new.go"] {
		t.Error("new file joined a curated selection")
	}
	if !got["a.go"] || got["b.go"] {
		t.Errorf("curated selection changed: %v", got)
	}
}

// An empty list counts as all-selected, so the first change in a clean repo
// arrives ticked rather than needing a keypress.
func TestMergeSelectionFirstChangeInACleanRepoArrivesSelected(t *testing.T) {
	got := selection(mergeSelection(nil, fresh("first.go")))
	if !got["first.go"] {
		t.Error("the only change in a clean repo arrived unselected")
	}
}

func TestMergeSelectionDropsVanishedFiles(t *testing.T) {
	old := fresh("a.go", "gone.go")
	old[0].selected = true

	got := mergeSelection(old, fresh("a.go"))
	if len(got) != 1 || got[0].Path != "a.go" {
		t.Errorf("items = %v, want [a.go]", paths(got))
	}
}

func TestPreservingRefreshKeepsSelection(t *testing.T) {
	m := refreshModel(t, config.Refresh{})
	m.items[1].selected = false // curate down to a.go

	m = update(m, statusMsg{
		preserve: true,
		files: []git.FileChange{
			{Path: "a.go", Index: ' ', Worktree: 'M'},
			{Path: "b.go", Index: '?', Worktree: '?'},
			{Path: "late.go", Index: '?', Worktree: '?'},
		},
	})

	if got := m.selectedPaths(); len(got) != 1 || got[0] != "a.go" {
		t.Errorf("selected %v, want only a.go carried over", got)
	}
}

func TestResettingRefreshReappliesTheStartupRule(t *testing.T) {
	m := refreshModel(t, config.Refresh{})
	m.items[0].selected = false
	m.items[1].selected = false

	m = update(m, statusMsg{files: []git.FileChange{
		{Path: "a.go", Index: 'M', Worktree: ' '},
		{Path: "b.go", Index: ' ', Worktree: 'M'},
	}})

	if got := m.selectedPaths(); len(got) != 1 || got[0] != "a.go" {
		t.Errorf("selected %v, want the staged a.go", got)
	}
}

func TestCursorFollowsItsFileAcrossARefresh(t *testing.T) {
	m := refreshModel(t, config.Refresh{})
	m = update(m, key("down")) // cursor on b.go

	m = update(m, statusMsg{
		preserve: true,
		files: []git.FileChange{
			{Path: "early.go", Index: '?', Worktree: '?'},
			{Path: "a.go", Index: ' ', Worktree: 'M'},
			{Path: "b.go", Index: '?', Worktree: '?'},
		},
	})

	if got := m.items[m.cursor].Path; got != "b.go" {
		t.Errorf("cursor on %q, want b.go", got)
	}
}

func TestCursorClampsWhenItsFileIsGone(t *testing.T) {
	m := refreshModel(t, config.Refresh{})
	m = update(m, key("down")) // cursor on b.go

	m = update(m, statusMsg{
		preserve: true,
		files:    []git.FileChange{{Path: "a.go", Index: ' ', Worktree: 'M'}},
	})

	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 after b.go vanished", m.cursor)
	}
}

func TestRefreshKeyReloadsPreservingSelection(t *testing.T) {
	m := refreshModel(t, config.Refresh{})

	next, cmd := m.Update(key("R"))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("R produced no command")
	}
	if m.status != "refreshed" {
		t.Errorf("status = %q, want refreshed", m.status)
	}

	st, ok := drain(t, cmd).(statusMsg)
	if !ok {
		t.Fatalf("R did not load status")
	}
	if !st.preserve {
		t.Error("R reset the selection instead of preserving it")
	}
}

func TestRefreshKeyIsIgnoredWhileBusy(t *testing.T) {
	m := refreshModel(t, config.Refresh{})
	m.busy = true
	m.status = "generating…"

	next, cmd := m.Update(key("R"))
	if cmd != nil {
		t.Error("R refreshed while a generation was in flight")
	}
	if next.(Model).status != "generating…" {
		t.Errorf("status = %q, want the generation status untouched", next.(Model).status)
	}
}

func TestFocusRefreshesWhenConfigured(t *testing.T) {
	m := refreshModel(t, config.Refresh{OnFocus: true})
	m.focused = false

	next, cmd := m.Update(tea.FocusMsg{})
	m = next.(Model)
	if !m.focused {
		t.Error("focus did not mark the model focused")
	}
	if cmd == nil {
		t.Fatal("focus produced no command")
	}
	st, ok := drain(t, cmd).(statusMsg)
	if !ok || !st.preserve {
		t.Errorf("focus msg = %#v, want a preserving statusMsg", st)
	}
}

func TestFocusDoesNotRefreshWhenDisabled(t *testing.T) {
	m := refreshModel(t, config.Refresh{OnFocus: false})

	next, cmd := m.Update(tea.FocusMsg{})
	if cmd != nil {
		t.Error("focus refreshed with on_focus disabled")
	}
	if !next.(Model).focused {
		t.Error("focus did not mark the model focused")
	}
}

func TestFocusDoesNotRefreshWhileBusy(t *testing.T) {
	m := refreshModel(t, config.Refresh{OnFocus: true})
	m.busy = true

	if _, cmd := m.Update(tea.FocusMsg{}); cmd != nil {
		t.Error("focus refreshed while a generation was in flight")
	}
}

func TestBlurPausesThePoll(t *testing.T) {
	m := refreshModel(t, config.Refresh{Interval: 10})
	gen := m.pollGen

	next, cmd := m.Update(tea.BlurMsg{})
	m = next.(Model)
	if cmd != nil {
		t.Error("blur scheduled more work")
	}

	if _, cmd := m.Update(refreshTickMsg{gen: gen}); cmd != nil {
		t.Error("the tick in flight at blur kept the poll chain alive")
	}
}

// Re-focusing starts a fresh chain; without a new generation a tick still in
// flight from the old one would double the poll rate.
func TestRefocusStrandsTheOldPollChain(t *testing.T) {
	m := refreshModel(t, config.Refresh{Interval: 10})
	gen := m.pollGen

	m = update(m, tea.BlurMsg{})
	m = update(m, tea.FocusMsg{})

	if _, cmd := m.Update(refreshTickMsg{gen: gen}); cmd != nil {
		t.Error("a tick from the pre-blur chain survived the re-focus")
	}
}

func TestTickIsDroppedWhileBlurred(t *testing.T) {
	m := refreshModel(t, config.Refresh{Interval: 10})
	m.focused = false

	if _, cmd := m.Update(refreshTickMsg{gen: m.pollGen}); cmd != nil {
		t.Error("the poll ran while the terminal was blurred")
	}
}

func TestStaleTickIsDropped(t *testing.T) {
	m := refreshModel(t, config.Refresh{Interval: 10})
	m.pollGen = 3

	if _, cmd := m.Update(refreshTickMsg{gen: 2}); cmd != nil {
		t.Error("a tick from a superseded chain still ran")
	}
}

func TestTickRefreshesPreservingSelection(t *testing.T) {
	m := refreshModel(t, config.Refresh{}) // interval 0: only the refresh comes back

	_, cmd := m.Update(refreshTickMsg{gen: m.pollGen})
	if cmd == nil {
		t.Fatal("the tick produced no command")
	}
	st, ok := drain(t, cmd).(statusMsg)
	if !ok || !st.preserve {
		t.Errorf("tick msg = %#v, want a preserving statusMsg", st)
	}
}

func TestTickDoesNotRefreshWhileBusy(t *testing.T) {
	m := refreshModel(t, config.Refresh{}) // interval 0: a reschedule would be nil
	m.busy = true

	if _, cmd := m.Update(refreshTickMsg{gen: m.pollGen}); cmd != nil {
		t.Error("the poll refreshed while a generation was in flight")
	}
}

// A busy tick must still keep the chain alive, or polling would stop for good
// after the first commit.
func TestBusyTickStillReschedules(t *testing.T) {
	m := refreshModel(t, config.Refresh{Interval: 10})
	m.busy = true

	if _, cmd := m.Update(refreshTickMsg{gen: m.pollGen}); cmd == nil {
		t.Error("a busy tick ended the poll chain")
	}
}

func TestSchedulePollHonoursTheInterval(t *testing.T) {
	if cmd := refreshModel(t, config.Refresh{}).schedulePoll(); cmd != nil {
		t.Error("a zero interval still scheduled a poll")
	}
	if cmd := refreshModel(t, config.Refresh{Interval: 10}).schedulePoll(); cmd == nil {
		t.Error("a non-zero interval scheduled no poll")
	}
}

func TestRefreshReloadsAnOpenDiff(t *testing.T) {
	m := refreshModel(t, config.Refresh{})
	m = update(m, key("d"))

	_, cmd := m.Update(statusMsg{
		preserve: true,
		files:    []git.FileChange{{Path: "a.go", Index: ' ', Worktree: 'M'}},
	})
	if cmd == nil {
		t.Error("a refresh left the open diff pane stale")
	}
}

func TestReloadingTheSameDiffKeepsTheScrollPosition(t *testing.T) {
	m := refreshModel(t, config.Refresh{})
	m = update(m, key("d"))

	body := strings.Repeat("line\n", 100)
	m = update(m, diffMsg{path: "a.go", body: body})
	m.diff.ScrollDown(5)
	off := m.diff.YOffset()
	if off == 0 {
		t.Fatal("the diff viewport did not scroll")
	}

	m = update(m, diffMsg{path: "a.go", body: body})
	if got := m.diff.YOffset(); got != off {
		t.Errorf("offset = %d, want %d — a reload rewound the diff being read", got, off)
	}
}

func TestADifferentDiffStillRewinds(t *testing.T) {
	m := refreshModel(t, config.Refresh{})
	m = update(m, key("d"))

	body := strings.Repeat("line\n", 100)
	m = update(m, diffMsg{path: "a.go", body: body})
	m.diff.ScrollDown(5)

	m = update(m, key("down")) // cursor on b.go
	m = update(m, diffMsg{path: "b.go", body: body})
	if got := m.diff.YOffset(); got != 0 {
		t.Errorf("offset = %d, want 0 for a different file", got)
	}
}

// The unit tests synthesise refreshTickMsg; this one proves Init really starts
// the chain and that a tick comes back through the runtime unprompted.
func TestPollPicksUpAFileWithNoKeypress(t *testing.T) {
	repo := newUIRepo(t)
	cfg := config.Config{Model: "haiku", Prompt: "{{diff}}", Refresh: config.Refresh{Interval: 1}}
	m := New(repo, cfg, e2eRunner{})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "a.go")
	}, teatest.WithDuration(3*time.Second))

	writeRepoFile(t, repo.Dir, "polled.go", "package main\n")

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "polled.go")
	}, teatest.WithDuration(10*time.Second))

	tm.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
	io.ReadAll(tm.FinalOutput(t))
}

func TestRefreshPicksUpAFileWrittenWhileRunning(t *testing.T) {
	repo := newUIRepo(t)
	m := New(repo, config.Config{Model: "haiku", Prompt: "{{diff}}"}, e2eRunner{})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "a.go")
	}, teatest.WithDuration(3*time.Second))

	writeRepoFile(t, repo.Dir, "late.go", "package main\n")
	tm.Send(tea.KeyPressMsg{Code: 'R', Text: "R"})

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "late.go")
	}, teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
	io.ReadAll(tm.FinalOutput(t))
}
