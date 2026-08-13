// Package ui is komit's bubbletea interface.
package ui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
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

	focus      focus
	amend      bool
	status     string
	err        error
	headPushed bool

	showDiff bool
	diffPath string
	diff     viewport.Model
	msgInput textarea.Model
	busy     bool
	spinner  spinner.Model
	cancel   context.CancelFunc
	epoch    int
	nudging  bool
	nudge    textinput.Model

	// warnings are config problems that do not stop komit. They show until the
	// first keypress, so a startup notice cannot be missed or become wallpaper.
	warnings []string

	// focused starts true so terminals that never report focus keep polling.
	focused bool
	pollGen int

	width, height int
}

// New builds the initial model. Files are loaded by the Init command.
func New(repo *git.Repo, cfg config.Config, runner ai.Runner, warnings []string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	ni := textinput.New()
	ni.Placeholder = "how should it change? (enter to regenerate, esc to cancel)"

	return Model{
		repo:     repo,
		cfg:      cfg,
		runner:   runner,
		warnings: warnings,
		diff:     viewport.New(),
		msgInput: newMessageInput(),
		spinner:  sp,
		nudge:    ni,
		focused:  true,
	}
}

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

// statusMsg carries a refreshed working-tree state. preserve carries the
// current selection over; without it the startup rule reapplies.
type statusMsg struct {
	files      []git.FileChange
	branch     git.Branch
	headPushed bool
	preserve   bool
}

// refreshTickMsg is one beat of the background poll. A tick from a superseded
// chain carries a stale gen and is dropped, so focus changes cannot stack loops.
type refreshTickMsg struct{ gen int }

// errMsg carries a failure to display without tearing the TUI down. A non-zero
// epoch ties it to one generation; anything else is always applied.
type errMsg struct {
	err   error
	epoch int
}

// diffMsg carries a loaded diff for one file.
type diffMsg struct {
	path string
	body string
}

// generatedMsg carries a finished commit message.
type generatedMsg struct {
	message string
	epoch   int
}

// committedMsg reports a finished commit; err is set when the push that
// followed it failed, which does not undo the commit.
type committedMsg struct {
	summary string
	err     error
}

// current reports whether an epoch-tagged message has not been superseded.
func (m Model) current(epoch int) bool { return epoch == 0 || epoch == m.epoch }

// spinnerTick is bubbles' spinner tick, aliased so tests can recognise it.
type spinnerTick = spinner.TickMsg

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

// mergeSelection carries the current selection across a refresh, keyed by path.
// A file that was not in the list arrives selected only when every file already
// was — an empty list counts, so the first change in a clean repo comes ticked.
func mergeSelection(old, fresh []item) []item {
	was := make(map[string]bool, len(old))
	all := true
	for _, it := range old {
		was[it.Path] = it.selected
		if !it.selected {
			all = false
		}
	}
	for i := range fresh {
		if selected, ok := was[fresh[i].Path]; ok {
			fresh[i].selected = selected
		} else {
			fresh[i].selected = all
		}
	}
	return fresh
}

// focusPath puts the cursor back on path across a refresh, clamping to the old
// index when that file is gone.
func (m *Model) focusPath(path string) {
	for i, it := range m.items {
		if it.Path == path {
			m.cursor = i
			return
		}
	}
	m.moveCursor(0)
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
