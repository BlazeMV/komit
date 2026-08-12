package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/BlazeMV/komit/internal/ai"
	"github.com/BlazeMV/komit/internal/config"
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
		headPushed, err := repo.HeadPushed()
		if err != nil {
			return errMsg{err}
		}
		return statusMsg{files: files, branch: branch, headPushed: headPushed}
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
		m.headPushed = msg.headPushed
		m.moveCursor(0)
		return m, nil

	case errMsg:
		m.err = msg.err
		m.busy = false
		m.cancel = nil
		return m, nil

	case diffMsg:
		if len(m.items) == 0 || msg.path != m.items[m.cursor].Path {
			return m, nil
		}
		m.diffPath = msg.path
		m.diff.SetContent(msg.body)
		m.diff.GotoTop()
		return m, nil

	case generatedMsg:
		m.busy = false
		m.cancel = nil
		m.msgInput.SetValue(msg.message)
		return m, nil

	case committedMsg:
		m.busy = false
		m.msgInput.SetValue("")
		m.amend = false
		m.status = msg.summary
		return m, m.loadStatus()

	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// resizePanes must run whenever width/height changes; a zero-sized
// viewport renders as an empty string.
func (m *Model) resizePanes() {
	if m.width == 0 {
		return
	}
	m.diff.SetWidth(m.width/2 - 4)
	m.diff.SetHeight(m.height - 12)
	m.msgInput.SetWidth(m.width - 4)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While nudging, all runes belong to the nudge input.
	if m.nudging {
		switch msg.String() {
		case "enter":
			nudge := m.nudge.Value()
			m.nudging = false
			return m, m.generate(nudge)
		case keyCancel:
			m.nudging = false
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.nudge, cmd = m.nudge.Update(msg)
		return m, cmd
	}

	// While the editor has focus, all runes belong to it.
	if m.focus == focusMessage {
		switch msg.String() {
		case keyCancel:
			return m.moveFocus(focusFiles)
		case keyFocus:
			return m.moveFocus(m.nextFocus())
		case "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.msgInput, cmd = m.msgInput.Update(msg)
		return m, cmd
	}

	// While the diff pane has focus, all keys but cancel/tab/quit scroll it.
	if m.focus == focusDiff {
		switch msg.String() {
		case keyCancel:
			return m.moveFocus(focusFiles)
		case keyFocus:
			return m.moveFocus(m.nextFocus())
		case "ctrl+c":
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.diff, cmd = m.diff.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case keyQuit, "ctrl+c":
		return m, tea.Quit
	case keyFocus:
		return m.moveFocus(m.nextFocus())
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
		return m.moveFocus(focusMessage)
	case keyEditor:
		return m, m.openEditor()
	case keyGenerate:
		if m.busy {
			return m, nil
		}
		return m, m.generate("")
	case keyRegen:
		if m.busy {
			return m, nil
		}
		m.nudging = true
		m.nudge.SetValue("")
		return m, m.nudge.Focus()
	case keyCommit:
		if m.busy {
			return m, nil
		}
		return m, m.commit(false)
	case keyPush:
		if m.busy {
			return m, nil
		}
		return m, m.commit(true)
	case keyAmend:
		if !m.amend && m.headPushed {
			m.status = "HEAD is already pushed — amending would rewrite published history"
			return m, nil
		}
		m.amend = !m.amend
	case keyCancel:
		if m.busy && m.cancel != nil {
			m.cancel()
			m.cancel = nil
			m.busy = false
			m.status = "generation cancelled"
		}
	}
	return m, nil
}

// nextFocus skips focusDiff while the diff pane is hidden.
func (m Model) nextFocus() focus {
	f := m.focus
	for {
		f = (f + 1) % 3
		if f != focusDiff || m.showDiff {
			return f
		}
	}
}

func (m Model) moveFocus(f focus) (Model, tea.Cmd) {
	if m.focus == focusMessage {
		m.msgInput.Blur()
	}
	m.focus = f
	if f == focusMessage {
		return m, m.msgInput.Focus()
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
		var cleanup func() error
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

const generateTimeout = 30 * time.Second

func (m *Model) generate(nudge string) tea.Cmd {
	paths := m.selectedPaths()
	if len(paths) == 0 {
		m.status = "no files selected"
		m.err = nil
		return nil
	}

	repo, cfg, runner := m.repo, m.cfg, m.runner
	untracked := m.untrackedSelected()
	branch := m.branch.Name
	amend := m.amend
	prompt := cfg.Prompt
	if nudge != "" {
		prompt += "\n\nRevise according to this instruction: " + nudge
	}
	cfg.Prompt = prompt

	ctx, cancel := context.WithTimeout(context.Background(), generateTimeout)
	m.cancel = cancel
	m.busy = true
	m.status = "generating…"
	m.err = nil

	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		defer cancel()

		cleanup := func() error { return nil }
		if len(untracked) > 0 {
			c, err := repo.MarkIntent(untracked)
			if err != nil {
				return errMsg{err}
			}
			cleanup = c
		}
		defer cleanup()

		diffOf := repo.Diff
		if amend {
			diffOf = repo.DiffAmend
		}
		diff, err := diffOf(paths)
		if err != nil {
			return errMsg{err}
		}
		recent, err := repo.RecentCommits(10)
		if err != nil {
			return errMsg{err}
		}

		out, err := ai.Generate(ctx, runner, cfg, config.Vars{
			Diff:          diff,
			Files:         strings.Join(paths, ", "),
			Branch:        branch,
			RecentCommits: recent,
		})
		if err != nil {
			return errMsg{err}
		}
		return generatedMsg{message: out}
	})
}

func (m *Model) commit(push bool) tea.Cmd {
	paths := m.selectedPaths()
	if len(paths) == 0 {
		m.status = "no files selected"
		m.err = nil
		return nil
	}
	if m.message() == "" {
		m.status = "write a message first — g to generate, e to type"
		m.err = nil
		return nil
	}

	repo, msg, amend := m.repo, m.message(), m.amend
	untracked := m.untrackedSelected()
	count := len(paths)
	m.busy = true
	m.err = nil

	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		cleanup := func() error { return nil }
		if len(untracked) > 0 {
			c, err := repo.MarkIntent(untracked)
			if err != nil {
				return errMsg{err}
			}
			cleanup = c
		}

		if err := repo.Commit(paths, msg, amend); err != nil {
			cleanup() // commit failed: leave the index as we found it
			return errMsg{err}
		}

		summary := fmt.Sprintf("committed %d file(s)", count)
		if amend {
			summary = "amended HEAD"
		}
		if push {
			if err := repo.Push(); err != nil {
				return errMsg{err}
			}
			summary += " and pushed"
		}
		return committedMsg{summary: summary}
	})
}
