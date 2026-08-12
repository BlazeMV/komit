package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const helpLine = "space sel · a all · d diff · g gen · r regen · e edit · c commit · P push · q quit"

// View satisfies tea.Model, which renders a View (not a string) as of v2.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("komit"))
	b.WriteString(dimStyle.Render(" · " + m.branchLine()))
	if m.amend {
		b.WriteString(" " + amendStyle.Render("AMEND"))
	}
	b.WriteString("\n\n")

	if len(m.items) == 0 {
		b.WriteString(dimStyle.Render("no changes in this repository"))
		b.WriteString("\n\n")
	} else if m.showDiff {
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, paneStyle.Render(m.fileList()), paneStyle.Render(m.diffPane())))
		b.WriteString("\n")
	} else {
		b.WriteString(m.fileList())
		b.WriteString("\n")
	}

	b.WriteString(m.msgInput.View())
	b.WriteString("\n")

	if m.nudging {
		b.WriteString(m.nudge.View())
		b.WriteString("\n")
	}

	switch {
	case m.busy:
		b.WriteString(m.spinner.View() + " " + m.status)
		b.WriteString("\n")
	case m.err != nil:
		b.WriteString(errStyle.Render(m.err.Error()))
		b.WriteString("\n")
	case m.status != "":
		b.WriteString(dimStyle.Render(m.status))
		b.WriteString("\n")
	}

	b.WriteString(dimStyle.Render(helpLine))
	return b.String()
}

func (m Model) branchLine() string {
	s := m.branch.Name
	if m.branch.Ahead > 0 {
		s += fmt.Sprintf(" ↑%d", m.branch.Ahead)
	}
	if m.branch.Behind > 0 {
		s += fmt.Sprintf(" ↓%d", m.branch.Behind)
	}
	return s
}

func (m Model) diffPane() string {
	if m.diffPath == "" {
		return dimStyle.Render("loading diff…")
	}
	return titleStyle.Render(m.diffPath) + "\n" + m.diff.View()
}

func (m Model) fileList() string {
	var b strings.Builder
	for i, it := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("▸ ")
		}
		box := "[ ]"
		if it.selected {
			box = selectedStyle.Render("[x]")
		}
		mark := " "
		if it.PartiallyStaged() {
			mark = "±"
		}
		fmt.Fprintf(&b, "%s%s %s%s %s\n", cursor, box, it.Letter(), mark, it.Path)
	}
	return b.String()
}
