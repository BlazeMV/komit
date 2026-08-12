// Package ui is komit's bubbletea interface.
package ui

import (
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

	focus  focus
	amend  bool
	status string
	err    error

	width, height int
}

// New builds the initial model. Files are loaded by the Init command.
func New(repo *git.Repo, cfg config.Config, runner ai.Runner) Model {
	return Model{repo: repo, cfg: cfg, runner: runner}
}

// statusMsg carries a refreshed working-tree state.
type statusMsg struct {
	files  []git.FileChange
	branch git.Branch
}

// errMsg carries a failure to display without tearing the TUI down.
type errMsg struct{ err error }

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
