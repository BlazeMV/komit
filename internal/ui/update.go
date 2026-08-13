package ui

import (
	"context"
	"errors"
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
	return tea.Batch(m.loadStatus(false), m.schedulePoll())
}

// loadStatus refreshes the working tree and branch state off the UI goroutine.
func (m Model) loadStatus(preserve bool) tea.Cmd {
	repo := m.repo
	return func() tea.Msg {
		files, err := repo.Status()
		if err != nil {
			return errMsg{err: err}
		}
		// Branch metadata is decoration; it used to abort the whole refresh and
		// leave the file list empty forever on a repo with no commits.
		branch, _ := repo.BranchState()
		headPushed, _ := repo.HeadPushed()
		return statusMsg{files: files, branch: branch, headPushed: headPushed, preserve: preserve}
	}
}

// schedulePoll queues the next beat of the poll chain, tagged with the current
// generation so a blur or a re-focus can strand it.
func (m Model) schedulePoll() tea.Cmd {
	every := m.cfg.Refresh.Every()
	if every == 0 {
		return nil
	}
	gen := m.pollGen
	return tea.Tick(every, func(time.Time) tea.Msg { return refreshTickMsg{gen: gen} })
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
		var under string
		if len(m.items) > 0 {
			under = m.items[m.cursor].Path
		}
		if msg.preserve {
			m.items = mergeSelection(m.items, items)
		} else {
			m.items = applyStartupSelection(items)
		}
		m.branch = msg.branch
		m.headPushed = msg.headPushed
		m.focusPath(under)
		if m.showDiff {
			return m, m.loadDiff()
		}
		return m, nil

	case tea.FocusMsg:
		m.focused = true
		m.pollGen++
		cmds := []tea.Cmd{m.schedulePoll()}
		if m.cfg.Refresh.OnFocus && !m.busy {
			cmds = append(cmds, m.loadStatus(true))
		}
		return m, tea.Batch(cmds...)

	case tea.BlurMsg:
		// The in-flight tick still lands; it finds focused false and ends the
		// chain there, so the poll costs nothing until focus comes back.
		m.focused = false
		return m, nil

	case refreshTickMsg:
		if msg.gen != m.pollGen || !m.focused {
			return m, nil
		}
		if m.busy {
			return m, m.schedulePoll()
		}
		return m, tea.Batch(m.loadStatus(true), m.schedulePoll())

	case errMsg:
		if !m.current(msg.epoch) {
			return m, nil
		}
		m.err = msg.err
		if msg.epoch != 0 {
			m.busy = false
			m.cancel = nil
		}
		return m, nil

	case diffMsg:
		if len(m.items) == 0 || msg.path != m.items[m.cursor].Path {
			return m, nil
		}
		// Only a different file rewinds the scroll; a refresh reloading the diff
		// you are reading used to yank it back to the top.
		fresh := msg.path != m.diffPath
		m.diffPath = msg.path
		m.diff.SetContent(msg.body)
		if fresh {
			m.diff.GotoTop()
		}
		return m, nil

	case generatedMsg:
		if !m.current(msg.epoch) {
			return m, nil
		}
		m.busy = false
		m.cancel = nil
		m.status = ""
		m.msgInput.SetValue(msg.message)
		return m, nil

	case committedMsg:
		m.busy = false
		m.msgInput.SetValue("")
		m.amend = false
		m.status = msg.summary
		m.err = msg.err
		return m, m.loadStatus(false)

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
			cmd := m.generate(nudge)
			return m, cmd
		case keyCancel:
			m.nudging = false
			return m, nil
		case "ctrl+c":
			cmd := m.quit()
			return m, cmd
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
			cmd := m.quit()
			return m, cmd
		}
		var cmd tea.Cmd
		m.msgInput, cmd = m.msgInput.Update(msg)
		return m, cmd
	}

	// While the diff pane has focus, all keys but cancel/tab/quit/q/d scroll it.
	if m.focus == focusDiff {
		switch msg.String() {
		case keyCancel:
			return m.moveFocus(focusFiles)
		case keyFocus:
			return m.moveFocus(m.nextFocus())
		case "ctrl+c":
			cmd := m.quit()
			return m, cmd
		case keyQuit:
			cmd := m.quit()
			return m, cmd
		case keyDiff:
			m.showDiff = false
			return m.moveFocus(focusFiles)
		}
		var cmd tea.Cmd
		m.diff, cmd = m.diff.Update(msg)
		return m, cmd
	}

	switch msg.String() {
	case keyQuit, "ctrl+c":
		cmd := m.quit()
		return m, cmd
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
		if m.busy {
			m.status = "generation in progress — wait for it to finish before opening the editor"
			m.err = nil
			return m, nil
		}
		return m, m.openEditor()
	case keyGenerate:
		if m.busy {
			return m, nil
		}
		cmd := m.generate("")
		return m, cmd
	case keyRefresh:
		if m.busy {
			return m, nil
		}
		m.status = "refreshed"
		m.err = nil
		return m, m.loadStatus(true)
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
		cmd := m.commit(false)
		return m, cmd
	case keyPush:
		if m.busy {
			return m, nil
		}
		cmd := m.commit(true)
		return m, cmd
	case keyAmend:
		if !m.amend && m.headPushed {
			m.status = "HEAD is already pushed — amending would rewrite published history"
			m.err = nil
			return m, nil
		}
		m.amend = !m.amend
	case keyCancel:
		if m.busy && m.cancel != nil {
			m.cancel()
			m.cancel = nil
			m.busy = false
			m.epoch++
			m.status = "generation cancelled"
		}
	}
	return m, nil
}

// quit undoes an in-flight generation's intent-to-add; bubbletea abandons
// command goroutines rather than letting their deferred cleanup run.
func (m *Model) quit() tea.Cmd {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.epoch++
	if m.repo != nil {
		m.repo.DrainIntents() // reported by main, which drains again
	}
	return tea.Quit
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
		var body string
		var err error
		if it.Untracked() {
			body, err = repo.DiffUntracked(it.Path)
		} else {
			body, err = repo.Diff([]string{it.Path})
		}
		if err != nil {
			return errMsg{err: err}
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
		return func() tea.Msg { return errMsg{err: err} }
	}
	path := f.Name()
	f.WriteString(m.msgInput.Value())
	f.Close()

	return tea.ExecProcess(exec.Command(editor, path), func(err error) tea.Msg {
		if err != nil {
			os.Remove(path)
			return errMsg{err: err}
		}
		data, readErr := os.ReadFile(path)
		os.Remove(path)
		if readErr != nil {
			return errMsg{err: readErr}
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
	m.epoch++
	epoch := m.epoch
	m.status = "generating…"
	m.err = nil

	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		defer cancel()
		repo.LockIndex()
		defer repo.UnlockIndex()

		var cleanup func() error
		finish := func(msg tea.Msg, err error) tea.Msg {
			if cleanup != nil {
				if cerr := cleanup(); cerr != nil {
					err = errors.Join(err, cerr)
				}
			}
			if err != nil {
				return errMsg{err: err, epoch: epoch}
			}
			return msg
		}

		if len(untracked) > 0 {
			c, err := repo.MarkIntent(untracked)
			cleanup = c
			if err != nil {
				return finish(nil, err)
			}
		}

		diffOf := repo.Diff
		if amend {
			diffOf = repo.DiffAmend
		}
		diff, err := diffOf(paths)
		if err != nil {
			return finish(nil, err)
		}
		var recent string
		if n := cfg.RecentCommits; n > 0 {
			recent, err = repo.RecentCommits(n)
			if err != nil {
				return finish(nil, err)
			}
		}

		out, err := ai.Generate(ctx, runner, cfg, config.Vars{
			Diff:          diff,
			Files:         strings.Join(paths, ", "),
			Branch:        branch,
			RecentCommits: recent,
		})
		if err != nil {
			return finish(nil, err)
		}
		return finish(generatedMsg{message: out, epoch: epoch}, nil)
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
		repo.LockIndex()
		defer repo.UnlockIndex()

		var cleanup func() error
		unmark := func(err error) error {
			if cleanup == nil {
				return err
			}
			if cerr := cleanup(); cerr != nil {
				return errors.Join(err, cerr)
			}
			return err
		}

		if len(untracked) > 0 {
			c, err := repo.MarkIntent(untracked)
			cleanup = c
			if err != nil {
				return errMsg{err: unmark(err)}
			}
		}

		if err := repo.Commit(paths, msg, amend); err != nil {
			return errMsg{err: unmark(err)} // leave the index as we found it
		}

		summary := fmt.Sprintf("committed %d file(s)", count)
		if amend {
			summary = "amended HEAD"
		}
		// Past this point the commit has landed, so nothing may report an errMsg:
		// it would keep the message and amend flag as if it had not.
		unmarkErr := unmark(nil)
		if push {
			if err := repo.Push(); err != nil {
				return committedMsg{summary: summary + ", push failed", err: errors.Join(unmarkErr, err)}
			}
			summary += " and pushed"
		}
		if unmarkErr != nil {
			return committedMsg{summary: summary, err: unmarkErr}
		}
		return committedMsg{summary: summary}
	})
}
