package tui

import (
	"database/sql"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pkg/browser"

	"mccwk.com/lk/internal/models"
)

type ReadLaterModel struct {
	links  []models.Link
	cursor int
}

func NewReadLaterModel(links []sql.NullString) ReadLaterModel {
	// TODO: Pass proper links
	return ReadLaterModel{
		links: []models.Link{},
	}
}

func (m ReadLaterModel) Update(msg tea.Msg) (ReadLaterModel, tea.Cmd) {
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
		case "o", "enter":
			if len(m.links) > 0 && m.cursor < len(m.links) {
				return m, m.openLink(m.links[m.cursor].Url)
			}
		}
	}

	return m, nil
}

func (m ReadLaterModel) View() string {
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

	s := titleStyle.Render("Read Later") + "\n\n"

	if len(m.links) == 0 {
		s += "No links to read. Add some links first!\n"
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
		Render("Use arrows or j/k • Enter/o to open link")

	return s
}

func (m ReadLaterModel) openLink(url string) tea.Cmd {
	return func() tea.Msg {
		_ = browser.OpenURL(url)
		return nil
	}
}
