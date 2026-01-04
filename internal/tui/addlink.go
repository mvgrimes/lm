package tui

import (
	"context"
	"database/sql"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mccwk.com/lk/internal/database"
	"mccwk.com/lk/internal/models"
	"mccwk.com/lk/internal/services"
)

type AddLinkModel struct {
	input   textinput.Model
	message string
	success bool
}

func NewAddLinkModel() AddLinkModel {
	ti := textinput.New()
	ti.Placeholder = "https://example.com"
	ti.Focus()
	ti.Width = 50

	return AddLinkModel{
		input: ti,
	}
}

func (m AddLinkModel) Update(msg tea.Msg, db *database.Database, fetcher *services.Fetcher, extractor *services.Extractor, summarizer *services.Summarizer, ctx context.Context) (AddLinkModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			url := m.input.Value()
			if url != "" {
				m.message = "Fetching and processing link..."
				return m, m.processLink(url, db, fetcher, extractor, summarizer, ctx)
			}
		case "esc":
			m.input.SetValue("")
			m.message = ""
			m.success = false
		}

	case linkProcessedMsg:
		m.message = "Link added successfully!"
		m.success = true
		m.input.SetValue("")
		return m, nil

	case linkProcessErrorMsg:
		m.message = "Error: " + msg.err.Error()
		m.success = false
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m AddLinkModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10"))

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9"))

	s := titleStyle.Render("Add Link") + "\n\n"
	s += "Enter URL:\n"
	s += m.input.View() + "\n\n"

	if m.message != "" {
		if m.success {
			s += messageStyle.Render(m.message) + "\n"
		} else {
			s += errorStyle.Render(m.message) + "\n"
		}
	}

	s += "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Press Enter to add • Esc to clear")

	return s
}

func (m AddLinkModel) processLink(url string, db *database.Database, fetcher *services.Fetcher, extractor *services.Extractor, summarizer *services.Summarizer, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		// Fetch the URL
		html, err := fetcher.FetchURL(ctx, url)
		if err != nil {
			return linkProcessErrorMsg{err: err}
		}

		// Extract text
		title, text, err := extractor.ExtractText(html)
		if err != nil {
			return linkProcessErrorMsg{err: err}
		}

		// Truncate content for storage
		content := extractor.TruncateText(text, 10000)

		// Generate summary if OpenAI is configured
		var summary string
		if summarizer != nil {
			summary, err = summarizer.Summarize(ctx, title, text)
			if err != nil {
				// Don't fail if summarization fails, just log it
				summary = ""
			}
		}

		// Save to database
		_, err = db.Queries.CreateLink(ctx, models.CreateLinkParams{
			Url:     url,
			Title:   sql.NullString{String: title, Valid: title != ""},
			Content: sql.NullString{String: content, Valid: content != ""},
			Summary: sql.NullString{String: summary, Valid: summary != ""},
			Status:  "read_later",
		})

		if err != nil {
			return linkProcessErrorMsg{err: err}
		}

		return linkProcessedMsg{}
	}
}

type linkProcessedMsg struct{}

type linkProcessErrorMsg struct {
	err error
}
