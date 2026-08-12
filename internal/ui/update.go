package ui

import (
	tea "charm.land/bubbletea/v2"
)

func (m Model) Init() tea.Cmd {
	return m.loadStatus()
}

// loadStatus refreshes the working tree and branch state off the UI goroutine.
func (m Model) loadStatus() tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		files, err := repo.Status()
		if err != nil {
			return errMsg{err}
		}
		branch, err := repo.BranchState()
		if err != nil {
			return errMsg{err}
		}
		return statusMsg{files: files, branch: branch}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case statusMsg:
		items := make([]item, len(msg.files))
		for i, f := range msg.files {
			items[i] = item{FileChange: f}
		}
		m.items = applyStartupSelection(items)
		m.branch = msg.branch
		m.moveCursor(0)
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyQuit, "ctrl+c":
		return m, tea.Quit
	case keyUp, keyUpAlt:
		m.moveCursor(-1)
	case keyDown, keyDownAlt:
		m.moveCursor(1)
	case keyToggle:
		m.toggle()
	case keyToggleAll:
		m.toggleAll()
	}
	return m, nil
}
