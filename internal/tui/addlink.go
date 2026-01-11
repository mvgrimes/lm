package tui

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mccwk.com/lk/internal/database"
	"mccwk.com/lk/internal/models"
	"mccwk.com/lk/internal/services"
)

type AddLinkModel struct {
	urlInput      textinput.Model
	categoryInput textinput.Model
	tagsInput     textinput.Model
	focusIndex    int // 0=url, 1=category, 2=tags, 3=summary viewport, 4=content viewport

	// Viewports for scrolling
	contentViewport viewport.Model
	summaryViewport viewport.Model
	viewportReady   bool
	summaryReady    bool

	// Processing state
	isProcessing bool
	statusText   string
	previewText  string
	summary      string

	// Suggested values
	suggestedCategory string
	suggestedTags     []string

	// Results
	message string
	success bool

	width  int
	height int

	// Optional: Task ID if adding link from tasks mode
	taskID *int64
}

func NewAddLinkModel() AddLinkModel {
	return NewAddLinkModelForTask(nil)
}

func NewAddLinkModelForTask(taskID *int64) AddLinkModel {
	urlInput := textinput.New()
	urlInput.Placeholder = "https://example.com"
	urlInput.Focus()
	urlInput.Width = 40
	urlInput.Prompt = "URL: "

	categoryInput := textinput.New()
	categoryInput.Placeholder = "e.g., Technology"
	categoryInput.Width = 40
	categoryInput.Prompt = "Category: "

	tagsInput := textinput.New()
	tagsInput.Placeholder = "e.g., golang, programming, tutorial"
	tagsInput.Width = 40
	tagsInput.Prompt = "Tags: "

	return AddLinkModel{
		urlInput:      urlInput,
		categoryInput: categoryInput,
		tagsInput:     tagsInput,
		focusIndex:    0,
		taskID:        taskID,
	}
}

func (m AddLinkModel) Init() tea.Cmd {
	return nil
}

func (m AddLinkModel) Update(msg tea.Msg, db *database.Database, fetcher *services.Fetcher, extractor *services.Extractor, summarizer *services.Summarizer, ctx context.Context) (AddLinkModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Calculate responsive widths
		leftWidth := int(float64(m.width) * 0.4)
		if leftWidth < 45 {
			leftWidth = 45
		}
		rightWidth := m.width - leftWidth - 6
		if rightWidth < 40 {
			rightWidth = 40
		}

		// Height accounting - be precise about every UI element
		const (
			helpTextLines     = 2 // Help text at bottom
			spacingLines      = 2 // Spacing between panels and help
			borderOverhead    = 4 // Each box has 2 lines of border (top+bottom)
			summaryTitleLines = 3 // "Summary" title + spacing
			contentTitleLines = 3 // "Page Content" title + spacing
			statusLines       = 2 // Status text when present
		)

		// Start with total height minus fixed overhead
		availableHeight := m.height - helpTextLines - spacingLines
		if m.statusText != "" {
			availableHeight -= statusLines
		}

		// Summary gets fixed 6 lines of viewport space
		const summaryViewportLines = 6
		summaryBoxTotalHeight := summaryViewportLines + summaryTitleLines + borderOverhead

		// Content gets whatever is left
		contentViewportLines := availableHeight - summaryBoxTotalHeight - contentTitleLines - borderOverhead
		if contentViewportLines < 5 {
			contentViewportLines = 5
		}

		// Initialize or update summary viewport
		if !m.summaryReady {
			m.summaryViewport = viewport.New(rightWidth-4, summaryViewportLines)
			m.summaryViewport.SetContent("")
			m.summaryReady = true
		} else {
			m.summaryViewport.Width = rightWidth - 4
			m.summaryViewport.Height = summaryViewportLines
		}

		// Initialize or update content viewport
		if !m.viewportReady {
			m.contentViewport = viewport.New(rightWidth-4, contentViewportLines)
			m.contentViewport.SetContent("")
			m.viewportReady = true
		} else {
			m.contentViewport.Width = rightWidth - 4
			m.contentViewport.Height = contentViewportLines
		}

		return m, nil

	case tea.KeyMsg:
		// Don't accept most input while processing
		if m.isProcessing && msg.String() != "ctrl+c" && msg.String() != "esc" {
			return m, nil
		}

		switch msg.String() {
		case "tab":
			// Cycle through inputs only (0-2)
			m.focusIndex++
			if m.focusIndex > 2 {
				m.focusIndex = 0
			}

			m.urlInput.Blur()
			m.categoryInput.Blur()
			m.tagsInput.Blur()

			switch m.focusIndex {
			case 0:
				m.urlInput.Focus()
			case 1:
				m.categoryInput.Focus()
			case 2:
				m.tagsInput.Focus()
			}

			return m, nil

		case "shift+tab":
			// Cycle through inputs backward (0-2)
			m.focusIndex--
			if m.focusIndex < 0 {
				m.focusIndex = 2
			}

			m.urlInput.Blur()
			m.categoryInput.Blur()
			m.tagsInput.Blur()

			switch m.focusIndex {
			case 0:
				m.urlInput.Focus()
			case 1:
				m.categoryInput.Focus()
			case 2:
				m.tagsInput.Focus()
			}

			return m, nil

		case "ctrl+n":
			// Cycle focus forward: url(0) -> category(1) -> tags(2) -> summary(3) -> content(4) -> url(0)
			m.focusIndex++
			if m.focusIndex > 4 {
				m.focusIndex = 0
			}

			// Update input focus
			m.urlInput.Blur()
			m.categoryInput.Blur()
			m.tagsInput.Blur()

			if m.focusIndex <= 2 {
				switch m.focusIndex {
				case 0:
					m.urlInput.Focus()
				case 1:
					m.categoryInput.Focus()
				case 2:
					m.tagsInput.Focus()
				}
			}
			return m, nil

		case "ctrl+p":
			// Cycle focus backward
			m.focusIndex--
			if m.focusIndex < 0 {
				m.focusIndex = 4
			}

			// Update input focus
			m.urlInput.Blur()
			m.categoryInput.Blur()
			m.tagsInput.Blur()

			if m.focusIndex <= 2 {
				switch m.focusIndex {
				case 0:
					m.urlInput.Focus()
				case 1:
					m.categoryInput.Focus()
				case 2:
					m.tagsInput.Focus()
				}
			}
			return m, nil

		case "pgup", "pgdown":
			// Scroll the focused viewport
			if m.focusIndex == 3 && m.summaryReady {
				// Scroll summary
				m.summaryViewport, cmd = m.summaryViewport.Update(msg)
				return m, cmd
			} else if m.focusIndex == 4 && m.viewportReady {
				// Scroll content
				m.contentViewport, cmd = m.contentViewport.Update(msg)
				return m, cmd
			} else if m.viewportReady {
				// Default to content if no viewport is focused
				m.contentViewport, cmd = m.contentViewport.Update(msg)
				return m, cmd
			}

		case "ctrl+l":
			// Accept LLM suggestions
			if m.suggestedCategory != "" {
				m.categoryInput.SetValue(m.suggestedCategory)
			}
			if len(m.suggestedTags) > 0 {
				m.tagsInput.SetValue(strings.Join(m.suggestedTags, ", "))
			}
			return m, nil

		case "enter":
			url := m.urlInput.Value()
			if url != "" && !m.isProcessing {
				m.isProcessing = true
				m.statusText = "Fetching URL..."
				m.message = ""
				m.success = false
				m.previewText = ""
				m.summary = ""
				m.suggestedCategory = ""
				m.suggestedTags = nil
				if m.viewportReady {
					m.contentViewport.SetContent("")
				}
				return m, m.processLink(url, db, fetcher, extractor, summarizer, ctx)
			}

		case "ctrl+r":
			// Reset form
			m.urlInput.SetValue("")
			m.categoryInput.SetValue("")
			m.tagsInput.SetValue("")
			m.isProcessing = false
			m.statusText = ""
			m.previewText = ""
			m.summary = ""
			m.message = ""
			m.success = false
			m.suggestedCategory = ""
			m.suggestedTags = nil
			m.focusIndex = 0
			m.urlInput.Focus()
			m.categoryInput.Blur()
			m.tagsInput.Blur()
			if m.viewportReady {
				m.contentViewport.SetContent("")
				m.contentViewport.GotoTop()
			}
			if m.summaryReady {
				m.summaryViewport.SetContent("")
				m.summaryViewport.GotoTop()
			}
			return m, nil
		}

	case linkProcessCompleteMsg:
		m.isProcessing = false
		m.statusText = "Complete!"
		m.message = "Link added successfully!"
		m.success = true
		m.previewText = msg.preview
		m.summary = msg.summary
		m.suggestedCategory = msg.category
		m.suggestedTags = msg.tags

		// Update viewport contents
		if m.viewportReady {
			m.contentViewport.SetContent(msg.preview)
			m.contentViewport.GotoTop()
		}

		if m.summaryReady {
			m.summaryViewport.SetContent(msg.summary)
			m.summaryViewport.GotoTop()
		}

		// Auto-fill if empty
		if m.categoryInput.Value() == "" && msg.category != "" {
			m.categoryInput.SetValue(msg.category)
		}
		if m.tagsInput.Value() == "" && len(msg.tags) > 0 {
			m.tagsInput.SetValue(strings.Join(msg.tags, ", "))
		}
		return m, nil

	case linkProcessErrorMsg:
		m.isProcessing = false
		m.message = "Error: " + msg.err.Error()
		m.success = false
		m.statusText = ""
		return m, nil
	}

	// Update the focused input
	switch m.focusIndex {
	case 0:
		m.urlInput, cmd = m.urlInput.Update(msg)
	case 1:
		m.categoryInput, cmd = m.categoryInput.Update(msg)
	case 2:
		m.tagsInput, cmd = m.tagsInput.Update(msg)
	}

	return m, cmd
}

func (m AddLinkModel) View() string {
	const minTerminalHeight = 24
	const minTerminalWidth = 80

	// Width check - show minimal view
	if m.width == 0 {
		return "Loading..."
	}

	// Height check - show minimal URL input only
	if m.height < minTerminalHeight {
		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6"))

		warningStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9"))

		content := titleStyle.Render("Add Link") + "\n\n"
		content += m.urlInput.View() + "\n\n"
		content += warningStyle.Render(fmt.Sprintf(
			"⚠ Terminal too small (height: %d, need: %d)\n"+
				"Please resize for full interface",
			m.height, minTerminalHeight))
		return content
	}

	// Width check - show left panel only
	if m.width < minTerminalWidth {
		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6"))

		warningStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9"))

		messageStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("10"))

		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9"))

		content := titleStyle.Render("Add Link") + "\n\n"
		content += m.urlInput.View() + "\n\n"
		content += m.categoryInput.View() + "\n\n"
		content += m.tagsInput.View() + "\n\n"

		if m.message != "" {
			if m.success {
				content += messageStyle.Render(m.message) + "\n\n"
			} else {
				content += errorStyle.Render(m.message) + "\n\n"
			}
		}

		content += warningStyle.Render(fmt.Sprintf(
			"⚠ Terminal too narrow (width: %d, need: %d)\n"+
				"Preview/summary hidden - resize for full interface",
			m.width, minTerminalWidth))
		return content
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	// Calculate responsive widths
	leftWidth := int(float64(m.width) * 0.4)
	if leftWidth < 45 {
		leftWidth = 45
	}
	rightWidth := m.width - leftWidth - 6
	if rightWidth < 40 {
		rightWidth = 40
	}

	leftPanelStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1)

	// Don't set fixed height - let content flow naturally within viewport
	summaryBoxStyle := lipgloss.NewStyle().
		Width(rightWidth - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("10")).
		Padding(1)

	contentBoxStyle := lipgloss.NewStyle().
		Width(rightWidth - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(1)

	progressStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true)

	suggestionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Italic(true)

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10"))

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9"))

	// Left panel - inputs
	leftContent := titleStyle.Render("Add Link")
	if m.taskID != nil {
		leftContent = titleStyle.Render("Add Link to Task")
	}
	leftContent += "\n\n"
	leftContent += m.urlInput.View() + "\n\n"
	leftContent += m.categoryInput.View() + "\n\n"
	leftContent += m.tagsInput.View() + "\n\n"

	if m.suggestedCategory != "" || len(m.suggestedTags) > 0 {
		leftContent += suggestionStyle.Render("💡 Suggestions:") + "\n"
		if m.suggestedCategory != "" {
			leftContent += suggestionStyle.Render(fmt.Sprintf("  Category: %s", m.suggestedCategory)) + "\n"
		}
		if len(m.suggestedTags) > 0 {
			leftContent += suggestionStyle.Render(fmt.Sprintf("  Tags: %s", strings.Join(m.suggestedTags, ", "))) + "\n"
		}
		leftContent += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render("Press Ctrl+L to accept") + "\n"
	}

	if m.message != "" {
		if m.success {
			leftContent += "\n" + messageStyle.Render(m.message)
		} else {
			leftContent += "\n" + errorStyle.Render(m.message)
		}
	}

	// Right panel - summary and content boxes
	var rightContent string

	// Status
	if m.statusText != "" {
		rightContent += progressStyle.Render("Status: "+m.statusText) + "\n\n"
	}

	// Summary box with viewport
	summaryBoxContent := ""

	// Add visual indicator if this viewport has focus
	if m.focusIndex == 3 {
		summaryBoxContent = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("10")).
			Render("Summary ◀") + "\n\n"
	} else {
		summaryBoxContent = lipgloss.NewStyle().Bold(true).Render("Summary") + "\n\n"
	}

	if m.summaryReady {
		// Update viewport content
		summaryContent := m.summary
		if summaryContent == "" {
			summaryContent = "Summary will appear here..."
		}
		m.summaryViewport.SetContent(summaryContent)

		// Render viewport
		summaryBoxContent += m.summaryViewport.View()

		// Show scroll indicator if content is scrollable
		if m.summaryViewport.TotalLineCount() > m.summaryViewport.Height {
			scrollPercent := int(m.summaryViewport.ScrollPercent() * 100)
			scrollInfo := lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Render(fmt.Sprintf("\n[%d%% - PgUp/PgDn]", scrollPercent))
			summaryBoxContent += scrollInfo
		}
	}

	summaryBox := summaryBoxStyle.Render(summaryBoxContent)
	rightContent += summaryBox + "\n\n"

	// Content box with viewport
	contentBoxContent := ""

	// Add visual indicator if this viewport has focus
	if m.focusIndex == 4 {
		contentBoxContent = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12")).
			Render("Page Content ◀") + "\n\n"
	} else {
		contentBoxContent = lipgloss.NewStyle().Bold(true).Render("Page Content") + "\n\n"
	}

	if m.viewportReady {
		// Update viewport content if we have preview text
		if m.previewText != "" {
			m.contentViewport.SetContent(m.previewText)
		}

		// Render viewport
		contentBoxContent += m.contentViewport.View()

		// Show scroll indicator if content is scrollable
		if m.contentViewport.TotalLineCount() > m.contentViewport.Height {
			scrollPercent := int(m.contentViewport.ScrollPercent() * 100)
			scrollInfo := lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Render(fmt.Sprintf("\n[%d%% - PgUp/PgDn]", scrollPercent))
			contentBoxContent += scrollInfo
		}
	} else {
		contentBoxContent += lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Render("Page content will appear here...")
	}

	contentBox := contentBoxStyle.Render(contentBoxContent)
	rightContent += contentBox

	// Combine panels
	leftPanel := leftPanelStyle.Render(leftContent)
	rightPanel := lipgloss.NewStyle().Width(rightWidth).Render(rightContent)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, "  ", rightPanel)

	// Help text
	helpText := "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Tab: cycle inputs • Ctrl+N/P: cycle sections • Enter: submit • Ctrl+R: reset • Ctrl+L: accept • PgUp/PgDn: scroll focused")

	return mainContent + helpText
}

// ViewModal renders a compact version of the add link form suitable for modal display
func (m AddLinkModel) ViewModal(maxWidth, maxHeight int) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6"))

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10"))

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9"))

	progressStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	var content strings.Builder

	// Title
	if m.taskID != nil {
		content.WriteString(titleStyle.Render("Add Link to Task") + "\n\n")
	} else {
		content.WriteString(titleStyle.Render("Add Link") + "\n\n")
	}

	// Status
	if m.statusText != "" {
		content.WriteString(progressStyle.Render("⏳ "+m.statusText) + "\n\n")
	}

	// Message
	if m.message != "" {
		if m.success {
			content.WriteString(messageStyle.Render("✓ "+m.message) + "\n\n")
		} else {
			content.WriteString(errorStyle.Render("✗ "+m.message) + "\n\n")
		}
	}

	// Inputs
	content.WriteString(m.urlInput.View() + "\n\n")
	content.WriteString(m.categoryInput.View() + "\n\n")
	content.WriteString(m.tagsInput.View() + "\n\n")

	// Summary preview (if available)
	if m.summary != "" {
		content.WriteString(lipgloss.NewStyle().Bold(true).Render("Summary:") + "\n")
		summaryPreview := m.summary
		if len(summaryPreview) > 200 {
			summaryPreview = summaryPreview[:197] + "..."
		}
		content.WriteString(dimStyle.Render(summaryPreview) + "\n\n")
	}

	// Help text
	content.WriteString(dimStyle.Render("Tab: cycle • Enter: submit • Ctrl+R: reset • Esc: close"))

	return content.String()
}

func (m AddLinkModel) processLink(url string, db *database.Database, fetcher *services.Fetcher, extractor *services.Extractor, summarizer *services.Summarizer, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		// Check if link already exists
		existingLink, err := db.Queries.GetLinkByURL(ctx, url)
		if err == nil {
			// Link exists, return it
			return linkProcessCompleteMsg{
				linkID:   existingLink.ID,
				preview:  existingLink.Content.String,
				summary:  existingLink.Summary.String,
				category: "", // We don't store category directly on link
				tags:     []string{},
			}
		}

		// Link doesn't exist, create it
		// Fetch the URL
		html, err := fetcher.FetchURL(ctx, url)
		if err != nil {
			return linkProcessErrorMsg{err: fmt.Errorf("fetch failed: %w", err)}
		}

		// Extract text
		title, text, err := extractor.ExtractText(html)
		if err != nil {
			return linkProcessErrorMsg{err: fmt.Errorf("extraction failed: %w", err)}
		}

		// Use full text as preview (viewport will handle scrolling)
		preview := text

		// Truncate content for storage
		content := extractor.TruncateText(text, 10000)

		// Generate summary and suggestions if OpenAI is configured
		var summary string
		var category string
		var tags []string

		if summarizer != nil {
			summary, _ = summarizer.Summarize(ctx, title, text)
			category, tags, _ = summarizer.SuggestMetadata(ctx, title, text)
		}

		if category == "" {
			category = "General"
		}
		if len(tags) == 0 {
			tags = []string{}
		}

		// Save to database
		link, err := db.Queries.CreateLink(ctx, models.CreateLinkParams{
			Url:     url,
			Title:   sql.NullString{String: title, Valid: title != ""},
			Content: sql.NullString{String: content, Valid: content != ""},
			Summary: sql.NullString{String: summary, Valid: summary != ""},
			Status:  "read_later",
		})

		if err != nil {
			return linkProcessErrorMsg{err: fmt.Errorf("save failed: %w", err)}
		}

		// If this is being added from tasks mode, link it to the task
		// (This will be handled by the calling code)

		return linkProcessCompleteMsg{
			linkID:   link.ID,
			preview:  preview,
			summary:  summary,
			category: category,
			tags:     tags,
		}
	}
}

// Messages
type linkProcessCompleteMsg struct {
	linkID   int64
	preview  string
	summary  string
	category string
	tags     []string
}

type linkProcessErrorMsg struct {
	err error
}
