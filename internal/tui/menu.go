package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type MenuModel struct {
	cursor   int
	options  []string
	selected int // -1 means no selection
}

func NewMenuModel() MenuModel {
	return MenuModel{
		cursor: 0,
		options: []string{
			"Menu",
			"Add Link",
			"Tasks",
			"Read Later",
			"Remember",
			"Search",
		},
		selected: -1,
	}
}

func (m MenuModel) Update(msg tea.Msg) (MenuModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 1 { // Skip index 0 (Menu)
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "enter":
			m.selected = m.cursor
		}
	}
	return m, nil
}

func (m MenuModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	optionStyle := lipgloss.NewStyle().
		PaddingLeft(2)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true).
		PaddingLeft(1)

	cursorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10"))

	s := titleStyle.Render("Link Manager") + "\n\n"

	for i, option := range m.options {
		if i == 0 {
			// Skip "Menu" option in display
			continue
		}

		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("> ")
		}

		line := fmt.Sprintf("%s%s", cursor, option)

		if i == m.cursor {
			s += selectedStyle.Render(line) + "\n"
		} else {
			s += optionStyle.Render(line) + "\n"
		}
	}

	s += "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Use arrow keys or j/k to navigate • Enter to select")

	return s
}
