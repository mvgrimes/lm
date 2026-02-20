package tui

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mccwk.com/lm/internal/database"
	"mccwk.com/lm/internal/models"
	"mccwk.com/lm/internal/services"
)

type EditLinkModel struct {
	link          models.Link
	summaryInput  textarea.Model
	categoryInput textinput.Model
	tagsInput     textinput.Model
	focusIndex    int // 0=summary, 1=category, 2=tags, 3=save, 4=reload

	// Processing state
	isProcessing bool

	width  int
	height int

	db         *database.Database
	ctx        context.Context
	fetcher    *services.Fetcher
	extractor  *services.Extractor
	summarizer *services.Summarizer
}

func NewEditLinkModel(link models.Link, db *database.Database, ctx context.Context, fetcher *services.Fetcher, extractor *services.Extractor, summarizer *services.Summarizer) EditLinkModel {
	summaryInput := textarea.New()
	summaryInput.Placeholder = "Enter summary..."
	summaryInput.SetWidth(50)
	summaryInput.SetHeight(4)
	if link.Summary.Valid {
		summaryInput.SetValue(link.Summary.String)
	}
	summaryInput.Focus()

	categoryInput := textinput.New()
	categoryInput.Placeholder = "e.g., Technology"
	categoryInput.Width = 50
	categoryInput.Prompt = "Category: "

	tagsInput := textinput.New()
	tagsInput.Placeholder = "e.g., golang, programming, tutorial"
	tagsInput.Width = 50
	tagsInput.Prompt = "Tags: "

	return EditLinkModel{
		link:          link,
		summaryInput:  summaryInput,
		categoryInput: categoryInput,
		tagsInput:     tagsInput,
		focusIndex:    0,
		db:            db,
		ctx:           ctx,
		fetcher:       fetcher,
		extractor:     extractor,
		summarizer:    summarizer,
	}
}

func (m EditLinkModel) Update(msg tea.Msg) (EditLinkModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Match textarea to modal inner width (modalWidth = clamp(width-20,60,80); inner = modalWidth-4)
		modalWidth := m.width - 20
		if modalWidth > 80 {
			modalWidth = 80
		}
		if modalWidth < 60 {
			modalWidth = 60
		}
		inner := modalWidth - 4
		if inner < 20 {
			inner = 20
		}
		m.summaryInput.SetWidth(inner)
		return m, nil

	case tea.KeyMsg:
		// Don't accept most input while processing
		if m.isProcessing && msg.String() != "ctrl+c" && msg.String() != "esc" {
			return m, nil
		}

		switch msg.String() {
		case "tab":
			// Cycle through inputs
			m.focusIndex++
			if m.focusIndex > 4 {
				m.focusIndex = 0
			}

			m.summaryInput.Blur()
			m.categoryInput.Blur()
			m.tagsInput.Blur()

			switch m.focusIndex {
			case 0:
				m.summaryInput.Focus()
			case 1:
				m.categoryInput.Focus()
			case 2:
				m.tagsInput.Focus()
			}

			return m, nil

		case "shift+tab":
			// Cycle through inputs backward
			m.focusIndex--
			if m.focusIndex < 0 {
				m.focusIndex = 4
			}

			m.summaryInput.Blur()
			m.categoryInput.Blur()
			m.tagsInput.Blur()

			switch m.focusIndex {
			case 0:
				m.summaryInput.Focus()
			case 1:
				m.categoryInput.Focus()
			case 2:
				m.tagsInput.Focus()
			}

			return m, nil

		case "ctrl+s":
			if !m.isProcessing {
				m.isProcessing = true
				return m, tea.Batch(m.saveChanges(), notifyCmd("info", "Saving..."))
			}

		case "ctrl+r":
			if !m.isProcessing {
				m.isProcessing = true
				return m, tea.Batch(m.reloadContent(), notifyCmd("info", "Reloading content..."))
			}
		case "enter":
			if !m.isProcessing {
				if m.focusIndex == 3 {
					m.isProcessing = true
					return m, tea.Batch(m.saveChanges(), notifyCmd("info", "Saving..."))
				}
				if m.focusIndex == 4 {
					m.isProcessing = true
					return m, tea.Batch(m.reloadContent(), notifyCmd("info", "Reloading content..."))
				}
			}
		}

	case editLinkCompleteMsg:
		m.isProcessing = false
		return m, notifyCmd("info", "Link updated!")

	case editLinkErrorMsg:
		m.isProcessing = false
		return m, notifyCmd("error", msg.err.Error())

	case reloadContentCompleteMsg:
		m.isProcessing = false
		if msg.summary != "" {
			m.summaryInput.SetValue(msg.summary)
		}
		return m, notifyCmd("info", "Content reloaded!")
	}

	// Update the focused input
	switch m.focusIndex {
	case 0:
		m.summaryInput, cmd = m.summaryInput.Update(msg)
	case 1:
		m.categoryInput, cmd = m.categoryInput.Update(msg)
	case 2:
		m.tagsInput, cmd = m.tagsInput.Update(msg)
	}

	return m, cmd
}

func (m EditLinkModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	var content strings.Builder

	// Title
	title := m.link.Title.String
	if title == "" {
		title = m.link.Url
	}
	content.WriteString(titleStyle.Render("Edit Link: "+title) + "\n\n")

	// URL
	content.WriteString(dimStyle.Render("URL: "+m.link.Url) + "\n\n")

	// Inputs
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	content.WriteString(labelStyle.Render("Summary:") + "\n")
	content.WriteString(m.summaryInput.View() + "\n\n")
	content.WriteString(m.categoryInput.View() + "\n\n")
	content.WriteString(m.tagsInput.View() + "\n\n")

	// Buttons and help
	btnBase := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1)

	// Save button
	saveStyle := btnBase
	if m.focusIndex == 3 {
		saveStyle = saveStyle.Bold(true).Foreground(lipgloss.Color("10")).BorderForeground(lipgloss.Color("10"))
	}
	saveBtn := saveStyle.Render(" Save ")

	// Reload button
	reloadStyle := btnBase
	if m.focusIndex == 4 {
		reloadStyle = reloadStyle.Bold(true).Foreground(lipgloss.Color("12")).BorderForeground(lipgloss.Color("12"))
	}
	reloadBtn := reloadStyle.Render(" Reload ")

	content.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, saveBtn, "  ", reloadBtn) + "\n\n")
	// Help text
	content.WriteString(dimStyle.Render("Tab: cycle • Enter on Save/Reload: perform action • Esc: close"))

	return content.String()
}

func (m EditLinkModel) saveChanges() tea.Cmd {
	return func() tea.Msg {
		// Update link summary
		summary := m.summaryInput.Value()
		_, err := m.db.Queries.UpdateLink(m.ctx, models.UpdateLinkParams{
			ID:      m.link.ID,
			Title:   m.link.Title,
			Content: m.link.Content,
			Summary: sql.NullString{String: summary, Valid: summary != ""},
			Status:  m.link.Status,
		})
		if err != nil {
			return editLinkErrorMsg{err: fmt.Errorf("failed to update link: %w", err)}
		}

		// Handle category
		categoryName := strings.TrimSpace(m.categoryInput.Value())
		if categoryName != "" {
			// Get or create category
			category, err := m.db.Queries.GetCategoryByName(m.ctx, categoryName)
			if err != nil {
				// Category doesn't exist, create it
				category, err = m.db.Queries.CreateCategory(m.ctx, models.CreateCategoryParams{
					Name:        categoryName,
					Description: sql.NullString{Valid: false},
				})
				if err != nil {
					return editLinkErrorMsg{err: fmt.Errorf("failed to create category: %w", err)}
				}
			}

			// Link category to link (remove old categories first)
			// Note: We're not removing old categories here, but we could add that functionality
			err = m.db.Queries.LinkCategory(m.ctx, models.LinkCategoryParams{
				LinkID:     m.link.ID,
				CategoryID: category.ID,
			})
			if err != nil {
				// Ignore duplicate errors
				if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
					return editLinkErrorMsg{err: fmt.Errorf("failed to link category: %w", err)}
				}
			}
		}

		// Handle tags
		tagsStr := strings.TrimSpace(m.tagsInput.Value())
		if tagsStr != "" {
			tags := strings.Split(tagsStr, ",")
			for _, tagName := range tags {
				tagName = strings.TrimSpace(tagName)
				if tagName == "" {
					continue
				}

				// Get or create tag
				tag, err := m.db.Queries.GetTagByName(m.ctx, tagName)
				if err != nil {
					// Tag doesn't exist, create it
					tag, err = m.db.Queries.CreateTag(m.ctx, tagName)
					if err != nil {
						return editLinkErrorMsg{err: fmt.Errorf("failed to create tag: %w", err)}
					}
				}

				// Link tag to link
				err = m.db.Queries.LinkTag(m.ctx, models.LinkTagParams{
					LinkID: m.link.ID,
					TagID:  tag.ID,
				})
				if err != nil {
					// Ignore duplicate errors
					if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
						return editLinkErrorMsg{err: fmt.Errorf("failed to link tag: %w", err)}
					}
				}
			}
		}

		return editLinkCompleteMsg{}
	}
}

func (m EditLinkModel) reloadContent() tea.Cmd {
	return func() tea.Msg {
		// Fetch the URL
		html, err := m.fetcher.FetchURL(m.ctx, m.link.Url)
		if err != nil {
			return editLinkErrorMsg{err: fmt.Errorf("fetch failed: %w", err)}
		}

		// Extract text
		title, text, err := m.extractor.ExtractText(html)
		if err != nil {
			return editLinkErrorMsg{err: fmt.Errorf("extraction failed: %w", err)}
		}

		// Truncate content for storage
		content := m.extractor.TruncateText(text, 10000)

		// Generate summary if OpenAI is configured
		var summary string
		if m.summarizer != nil {
			summary, _ = m.summarizer.Summarize(m.ctx, title, text)
		}

		// Update link
		_, err = m.db.Queries.UpdateLink(m.ctx, models.UpdateLinkParams{
			ID:      m.link.ID,
			Title:   sql.NullString{String: title, Valid: title != ""},
			Content: sql.NullString{String: content, Valid: content != ""},
			Summary: sql.NullString{String: summary, Valid: summary != ""},
			Status:  m.link.Status,
		})
		if err != nil {
			return editLinkErrorMsg{err: fmt.Errorf("failed to update link: %w", err)}
		}

		// Update fetched_at timestamp
		err = m.db.Queries.UpdateLinkFetchedAt(m.ctx, m.link.ID)
		if err != nil {
			return editLinkErrorMsg{err: fmt.Errorf("failed to update fetched_at: %w", err)}
		}

		return reloadContentCompleteMsg{summary: summary}
	}
}

// Messages
type editLinkCompleteMsg struct{}

type editLinkErrorMsg struct {
	err error
}

type reloadContentCompleteMsg struct {
	summary string
}
