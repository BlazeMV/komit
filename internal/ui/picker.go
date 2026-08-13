package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/BlazeMV/komit/internal/ai"
)

// openPicker builds a row per configured block. Usability is resolved here
// rather than per render: it reads the environment and PATH, and the answer
// cannot change while the list is on screen.
func (m *Model) openPicker() {
	labels := m.cfg.Labels()
	if len(labels) == 0 {
		m.status = "no providers configured — run `komit init`"
		m.err = nil
		return
	}

	m.pickRows = make([]providerRow, len(labels))
	m.pickCursor = 0
	for i, label := range labels {
		p := m.cfg.Providers[label]
		m.pickRows[i] = providerRow{
			label:   label,
			kind:    p.Type,
			model:   p.Model,
			problem: m.cfg.Usable(label),
		}
		if label == m.cfg.Provider {
			m.pickCursor = i
		}
	}
	m.picking = true
}

func (m *Model) movePick(delta int) {
	m.pickCursor += delta
	if m.pickCursor < 0 {
		m.pickCursor = 0
	}
	if m.pickCursor > len(m.pickRows)-1 {
		m.pickCursor = len(m.pickRows) - 1
	}
}

// choose switches to the row under the cursor. An unusable block leaves the
// picker open so another can be tried without reopening it.
func (m Model) choose() (tea.Model, tea.Cmd) {
	row := m.pickRows[m.pickCursor]
	if row.problem != nil {
		m.status = row.problem.Error()
		m.err = nil
		return m, nil
	}

	cfg := m.cfg.WithProvider(row.label)
	runner, err := ai.New(cfg)
	if err != nil {
		m.status = err.Error()
		m.err = nil
		return m, nil
	}

	m.cfg = cfg
	m.runner = runner
	m.picking = false
	m.status = "provider: " + row.label
	m.err = nil
	return m, nil
}

// firstLine trims a config error down to its headline. The rest of these
// messages tells you how to fix the problem, which belongs in the status line
// rather than inside a list row.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
