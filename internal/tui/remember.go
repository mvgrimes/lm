package tui

import (
	"database/sql"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mccwk.com/lk/internal/database"
	"mccwk.com/lk/internal/models"
)

type RememberModel struct {
	links  []models.Link
	cursor int
	db     *database.Database
}

func NewRememberModel(links []sql.NullString, db *database.Database) RememberModel {
	// TODO: Pass proper links
	return RememberModel{
		links: []models.Link{},
		db:    db,
	}
}

func (m RememberModel) Update(msg tea.Msg) (RememberModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.links)-1 {
				m.cursor++
			}
		}
	}

	return m, nil
}

func (m RememberModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	summaryStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243")).
		PaddingLeft(4)

	s := titleStyle.Render("Remember") + "\n\n"

	if len(m.links) == 0 {
		s += "No links to categorize. Add some links first!\n"
	} else {
		for i, link := range m.links {
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}

			title := link.Title.String
			if title == "" {
				title = link.Url
			}

			line := fmt.Sprintf("%s%s", cursor, title)

			if i == m.cursor {
				s += selectedStyle.Render(line) + "\n"
				if link.Summary.Valid && link.Summary.String != "" {
					s += summaryStyle.Render(link.Summary.String) + "\n"
				}
			} else {
				s += line + "\n"
			}
		}
	}

	s += "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Use arrows or j/k to navigate • Coming soon: tagging and categorization")

	return s
}
