package ui

import (
	"os"
	"os/exec"
	"strings"

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
	if m.msgInput.Placeholder == "" {
		m.msgInput = newMessageInput()
	}
	m.resizePanes()

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resizePanes()
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

	case diffMsg:
		m.diffPath = msg.path
		m.diff.SetContent(msg.body)
		m.diff.GotoTop()
		return m, nil

	case generatedMsg:
		m.busy = false
		m.msgInput.SetValue(msg.message)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// resizePanes syncs the diff viewport and message editor to the current
// window size; viewport.View() renders empty at zero width or height, and
// tests set width/height directly without ever sending a WindowSizeMsg.
func (m *Model) resizePanes() {
	if m.width == 0 {
		return
	}
	m.diff.SetWidth(m.width/2 - 4)
	m.diff.SetHeight(m.height - 12)
	m.msgInput.SetWidth(m.width - 4)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While the editor has focus, all runes belong to it.
	if m.focus == focusMessage {
		switch msg.String() {
		case keyCancel:
			m.focus = focusFiles
			m.msgInput.Blur()
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.msgInput, cmd = m.msgInput.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case keyQuit, "ctrl+c":
		return m, tea.Quit
	case keyUp, keyUpAlt:
		m.moveCursor(-1)
		if m.showDiff {
			return m, m.loadDiff()
		}
	case keyDown, keyDownAlt:
		m.moveCursor(1)
		if m.showDiff {
			return m, m.loadDiff()
		}
	case keyToggle:
		m.toggle()
	case keyToggleAll:
		m.toggleAll()
	case keyDiff:
		m.showDiff = !m.showDiff
		if m.showDiff {
			return m, m.loadDiff()
		}
	case keyEdit:
		m.focus = focusMessage
		return m, m.msgInput.Focus()
	case keyEditor:
		return m, m.openEditor()
	}
	return m, nil
}

// loadDiff fetches the diff for the file under the cursor, not the selection.
func (m Model) loadDiff() tea.Cmd {
	if len(m.items) == 0 {
		return nil
	}
	repo, it := m.repo, m.items[m.cursor]
	return func() tea.Msg {
		var cleanup func()
		if it.Untracked() {
			c, err := repo.MarkIntent([]string{it.Path})
			if err != nil {
				return errMsg{err}
			}
			cleanup = c
		}
		body, err := repo.Diff([]string{it.Path})
		if cleanup != nil {
			cleanup()
		}
		if err != nil {
			return errMsg{err}
		}
		if strings.TrimSpace(body) == "" {
			body = "(no textual diff)"
		}
		return diffMsg{path: it.Path, body: body}
	}
}

// editorDoneMsg carries the result of shelling out to $EDITOR.
type editorDoneMsg struct {
	message string
	err     error
}

// openEditor shells out to $EDITOR (or vi) on a temp file seeded with the
// current message, and feeds the edited result back into the textarea.
func (m Model) openEditor() tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	f, err := os.CreateTemp("", "komit-*.txt")
	if err != nil {
		return func() tea.Msg { return errMsg{err} }
	}
	path := f.Name()
	f.WriteString(m.msgInput.Value())
	f.Close()

	return tea.ExecProcess(exec.Command(editor, path), func(err error) tea.Msg {
		if err != nil {
			os.Remove(path)
			return errMsg{err}
		}
		data, readErr := os.ReadFile(path)
		os.Remove(path)
		if readErr != nil {
			return errMsg{readErr}
		}
		return generatedMsg{message: strings.TrimSpace(string(data))}
	})
}
