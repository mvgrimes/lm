package tui

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pkg/browser"

	"mccwk.com/lk/internal/database"
	"mccwk.com/lk/internal/models"
)

type SearchModel struct {
	input   textinput.Model
	results []models.Link
	cursor  int
}

func NewSearchModel() SearchModel {
	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.Focus()
	ti.Width = 50

	return SearchModel{
		input: ti,
	}
}

func (m SearchModel) Update(msg tea.Msg, db *database.Database, ctx context.Context) (SearchModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			query := m.input.Value()
			if query != "" {
				return m, m.search(query, db, ctx)
			}
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "j":
			if m.cursor < len(m.results)-1 {
				m.cursor++
			}
			return m, nil
		case "o":
			if len(m.results) > 0 && m.cursor < len(m.results) {
				return m, m.openLink(m.results[m.cursor].Url)
			}
			return m, nil
		}

	case searchResultsMsg:
		m.results = msg.links
		m.cursor = 0
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m SearchModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	s := titleStyle.Render("Search") + "\n\n"
	s += m.input.View() + "\n\n"

	if len(m.results) > 0 {
		s += lipgloss.NewStyle().Bold(true).Render("Results:") + "\n\n"

		for i, link := range m.results {
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
			} else {
				s += line + "\n"
			}
		}
	}

	s += "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Type to search • Enter to search • o to open • arrows/j/k to navigate")

	return s
}

func (m SearchModel) search(query string, db *database.Database, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		// Use LIKE search with wildcards
		pattern := "%" + query + "%"

		links, err := db.Queries.SearchLinks(ctx, models.SearchLinksParams{
			Url:     pattern,
			Title:   sql.NullString{String: pattern, Valid: true},
			Content: sql.NullString{String: pattern, Valid: true},
			Summary: sql.NullString{String: pattern, Valid: true},
			Limit:   20,
			Offset:  0,
		})

		if err != nil {
			return errMsg{err: err}
		}

		return searchResultsMsg{links: links}
	}
}

func (m SearchModel) openLink(url string) tea.Cmd {
	return func() tea.Msg {
		_ = browser.OpenURL(url)
		return nil
	}
}

type searchResultsMsg struct {
	links []models.Link
}
