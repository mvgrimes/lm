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

	"mccwk.com/lm/internal/database"
	"mccwk.com/lm/internal/models"
	"mccwk.com/lm/internal/services"
)

type AddLinkModel struct {
	urlInput      textinput.Model
	categoryInput textinput.Model
	tagsInput     textinput.Model
	focusIndex    int  // 0=url, 1=category, 2=tags, 3=summary viewport, 4=content viewport, 5=Save(btn), 6=Cancel(btn)
	inModal       bool // whether rendered in modal

	// Save/unsaved state
	linkID        *int64
	savedCategory string
	savedTags     []string
	pendingSave   bool

	// Viewports for scrolling
	contentViewport viewport.Model
	summaryViewport viewport.Model
	viewportReady   bool
	summaryReady    bool

	// Processing state
	isProcessing bool
	processStage string // e.g. "Fetching...", "Extracting...", "Summarizing..."
	previewText  string
	summary      string

	// Suggested values
	suggestedCategory string
	suggestedTags     []string

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

func (m AddLinkModel) resetForm() AddLinkModel {
	m.urlInput.SetValue("")
	m.categoryInput.SetValue("")
	m.tagsInput.SetValue("")
	m.isProcessing = false
	m.processStage = ""
	m.previewText = ""
	m.summary = ""
	m.suggestedCategory = ""
	m.suggestedTags = nil
	m.linkID = nil
	m.savedCategory = ""
	m.savedTags = nil
	m.pendingSave = false
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
	return m
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
		)

		// Start with total height minus fixed overhead
		availableHeight := m.height - helpTextLines - spacingLines

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
			// Cycle focus; in modal include buttons
			m.focusIndex++
			maxIdx := 2
			if m.inModal {
				maxIdx = 6
			}
			if m.focusIndex > maxIdx {
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
			// Cycle backward; in modal include buttons
			m.focusIndex--
			minIdx := 0
			maxIdx := 2
			if m.inModal {
				maxIdx = 6
			}
			if m.focusIndex < minIdx {
				m.focusIndex = maxIdx
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
			// Activate buttons if focused in modal
			if m.inModal && !m.isProcessing {
				if m.focusIndex == 5 { // Save button
					if m.linkID == nil {
						url := m.urlInput.Value()
						if url != "" {
							m.isProcessing = true
							m.processStage = "Fetching..."
							m.previewText = ""
							m.summary = ""
							m.suggestedCategory = ""
							m.suggestedTags = nil
							m.pendingSave = true
							if m.viewportReady {
								m.contentViewport.SetContent("")
							}
							return m, m.fetchLink(url, db, fetcher, ctx)
						}
						return m, nil
					}
					return m, m.saveMetadata(db)
				}
				if m.focusIndex == 6 { // Cancel button — closes the dialog
					return m, func() tea.Msg { return addLinkCloseRequestedMsg{} }
				}
			}
			url := m.urlInput.Value()
			if url != "" && !m.isProcessing {
				m.isProcessing = true
				m.previewText = ""
				m.summary = ""
				m.suggestedCategory = ""
				m.suggestedTags = nil
				if m.viewportReady {
					m.contentViewport.SetContent("")
				}
				m.processStage = "Fetching..."
				return m, m.fetchLink(url, db, fetcher, ctx)
			}

		}

	case linkFetchedMsg:
		m.processStage = "Extracting..."
		return m, m.extractLink(msg.url, msg.html, extractor)

	case linkExtractedMsg:
		m.processStage = "Summarizing..."
		return m, m.summarizeAndSave(msg.url, msg.title, msg.text, msg.content, msg.preview, db, summarizer, ctx)

	case linkProcessCompleteMsg:
		m.processStage = ""
		m.isProcessing = false
		m.previewText = msg.preview
		m.summary = msg.summary
		m.suggestedCategory = msg.category
		m.suggestedTags = msg.tags
		m.linkID = &msg.linkID

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

		if m.pendingSave {
			m.pendingSave = false
			return m, tea.Batch(m.saveMetadata(db), notifyCmd("info", "Link saved!"))
		}
		return m, notifyCmd("info", "Link fetched!")

	case linkProcessErrorMsg:
		m.isProcessing = false
		m.processStage = ""
		return m, notifyCmd("error", msg.err.Error())

	case metadataSavedMsg:
		// update saved state for highlighting
		m.savedCategory = strings.TrimSpace(m.categoryInput.Value())
		curTags := []string{}
		if strings.TrimSpace(m.tagsInput.Value()) != "" {
			for _, s := range strings.Split(m.tagsInput.Value(), ",") {
				t := strings.ToLower(strings.TrimSpace(s))
				if t != "" {
					curTags = append(curTags, t)
				}
			}
		}
		m.savedTags = curTags
		// Close the dialog after saving and notify
		return m, tea.Batch(
			notifyCmd("info", "Link saved!"),
			func() tea.Msg { return addLinkCloseRequestedMsg{} },
		)
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

		content := titleStyle.Render("Add Link") + "\n\n"
		content += m.urlInput.View() + "\n\n"
		content += m.categoryInput.View() + "\n\n"
		content += m.tagsInput.View() + "\n\n"

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

	suggestionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Italic(true)

	// Left panel - inputs
	leftContent := titleStyle.Render("Add Link")
	if m.taskID != nil {
		leftContent = titleStyle.Render("Add Link to Task")
	}
	leftContent += "\n\n"
	leftContent += m.urlInput.View() + "\n\n"
	// Highlight unsaved fields
	unsavedCat := m.linkID != nil && strings.TrimSpace(m.categoryInput.Value()) != strings.TrimSpace(m.savedCategory)
	unsavedTags := false
	if m.linkID != nil {
		curTags := []string{}
		if strings.TrimSpace(m.tagsInput.Value()) != "" {
			for _, s := range strings.Split(m.tagsInput.Value(), ",") {
				t := strings.ToLower(strings.TrimSpace(s))
				if t != "" {
					curTags = append(curTags, t)
				}
			}
		}
		// simple set compare
		if len(curTags) != len(m.savedTags) {
			unsavedTags = true
		} else {
			mset := map[string]struct{}{}
			for _, t := range m.savedTags {
				mset[t] = struct{}{}
			}
			for _, t := range curTags {
				if _, ok := mset[t]; !ok {
					unsavedTags = true
					break
				}
			}
		}
	}

	catLabel := "Category:"
	if unsavedCat {
		catLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("Category (unsaved):")
	}
	tagLabel := "Tags:"
	if unsavedTags {
		tagLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("Tags (unsaved):")
	}

	leftContent += lipgloss.NewStyle().Bold(true).Render(catLabel) + "\n" + m.categoryInput.View() + "\n\n"
	leftContent += lipgloss.NewStyle().Bold(true).Render(tagLabel) + "\n" + m.tagsInput.View() + "\n\n"

	// Progress indicator
	if m.processStage != "" {
		steps := []string{"Fetching...", "Extracting...", "Summarizing..."}
		currentStep := 0
		for i, s := range steps {
			if s == m.processStage {
				currentStep = i
			}
		}
		progressStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
		dimStyle2 := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		var progressBar strings.Builder
		for i, s := range steps {
			if i < currentStep {
				progressBar.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("✓ "+s))
			} else if i == currentStep {
				progressBar.WriteString(progressStyle.Render("⟳ "+s))
			} else {
				progressBar.WriteString(dimStyle2.Render("○ "+s))
			}
			if i < len(steps)-1 {
				progressBar.WriteString("\n")
			}
		}
		leftContent += progressBar.String() + "\n\n"
	}

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

	// Right panel - summary and content boxes
	var rightContent string

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
func (m AddLinkModel) saveMetadata(db *database.Database) tea.Cmd {
	linkID := m.linkID
	category := strings.TrimSpace(m.categoryInput.Value())
	tagStr := m.tagsInput.Value()
	return func() tea.Msg {
		if linkID == nil {
			return linkProcessErrorMsg{err: fmt.Errorf("no link to save")}
		}
		// Save category if provided
		if category != "" {
			cat, err := db.Queries.GetCategoryByName(context.Background(), category)
			if err != nil {
				// create if not exists
				cat, err = db.Queries.CreateCategory(context.Background(), models.CreateCategoryParams{
					Name:        category,
					Description: sql.NullString{Valid: false},
				})
				if err != nil {
					return linkProcessErrorMsg{err: fmt.Errorf("category save failed: %w", err)}
				}
			}
			// Link category
			_ = db.Queries.LinkCategory(context.Background(), models.LinkCategoryParams{LinkID: *linkID, CategoryID: cat.ID})
		}
		// Save tags
		if strings.TrimSpace(tagStr) != "" {
			tags := strings.Split(tagStr, ",")
			for i := range tags {
				tags[i] = strings.ToLower(strings.TrimSpace(tags[i]))
				if tags[i] == "" {
					continue
				}
				t, err := db.Queries.GetTagByName(context.Background(), tags[i])
				if err != nil {
					t, err = db.Queries.CreateTag(context.Background(), tags[i])
					if err != nil {
						return linkProcessErrorMsg{err: fmt.Errorf("tag save failed: %w", err)}
					}
				}
				_ = db.Queries.LinkTag(context.Background(), models.LinkTagParams{LinkID: *linkID, TagID: t.ID})
			}
		}
		return metadataSavedMsg{}
	}
}

func (m AddLinkModel) ViewModal(maxWidth, maxHeight int) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6"))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	var content strings.Builder

	// Title
	if m.taskID != nil {
		content.WriteString(titleStyle.Render("Add Link to Task") + "\n\n")
	} else {
		content.WriteString(titleStyle.Render("Add Link") + "\n\n")
	}

	// Inputs with unsaved highlighting
	content.WriteString(m.urlInput.View() + "\n\n")
	unsavedCat := m.linkID != nil && strings.TrimSpace(m.categoryInput.Value()) != strings.TrimSpace(m.savedCategory)
	unsavedTags := false
	if m.linkID != nil {
		curTags := []string{}
		if strings.TrimSpace(m.tagsInput.Value()) != "" {
			for _, s := range strings.Split(m.tagsInput.Value(), ",") {
				t := strings.ToLower(strings.TrimSpace(s))
				if t != "" {
					curTags = append(curTags, t)
				}
			}
		}
		if len(curTags) != len(m.savedTags) {
			unsavedTags = true
		} else {
			mset := map[string]struct{}{}
			for _, t := range m.savedTags {
				mset[t] = struct{}{}
			}
			for _, t := range curTags {
				if _, ok := mset[t]; !ok {
					unsavedTags = true
					break
				}
			}
		}
	}
	catLabel := "Category:"
	if unsavedCat {
		catLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("Category (unsaved):")
	}
	tagLabel := "Tags:"
	if unsavedTags {
		tagLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render("Tags (unsaved):")
	}
	content.WriteString(lipgloss.NewStyle().Bold(true).Render(catLabel) + "\n")
	content.WriteString(m.categoryInput.View() + "\n\n")
	content.WriteString(lipgloss.NewStyle().Bold(true).Render(tagLabel) + "\n")
	content.WriteString(m.tagsInput.View() + "\n\n")

	// Progress indicator (modal)
	if m.processStage != "" {
		steps := []string{"Fetching...", "Extracting...", "Summarizing..."}
		progressStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
		dimProgress := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		currentStep := 0
		for i, s := range steps {
			if s == m.processStage {
				currentStep = i
			}
		}
		for i, s := range steps {
			if i < currentStep {
				content.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("✓ "+s) + "\n")
			} else if i == currentStep {
				content.WriteString(progressStyle.Render("⟳ "+s) + "\n")
			} else {
				content.WriteString(dimProgress.Render("○ "+s) + "\n")
			}
		}
		content.WriteString("\n")
	}

	// Summary preview (if available)
	summaryFocused := m.focusIndex == 3
	summaryStyle := lipgloss.NewStyle().Bold(true)
	if summaryFocused {
		summaryStyle = summaryStyle.Foreground(lipgloss.Color("10"))
	}
	if m.summary != "" || summaryFocused {
		label := "Summary:"
		if summaryFocused {
			label = "▶ Summary:"
		}
		content.WriteString(summaryStyle.Render(label) + "\n")
		if m.summary != "" {
			summaryPreview := m.summary
			if len(summaryPreview) > 200 {
				summaryPreview = summaryPreview[:197] + "..."
			}
			content.WriteString(dimStyle.Render(summaryPreview) + "\n\n")
		} else {
			content.WriteString(dimStyle.Render("(summary will appear here after fetching)") + "\n\n")
		}
	}

	// Content section focus indicator (content not shown in modal view)
	if m.focusIndex == 4 {
		contentFocusStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
		content.WriteString(contentFocusStyle.Render("▶ Page Content: (visible in full view)") + "\n\n")
	}

	// Buttons row (Save, Cancel)
	btnBase := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1)

	saveStyle := btnBase
	if m.focusIndex == 5 {
		saveStyle = saveStyle.Bold(true).Foreground(lipgloss.Color("10")).BorderForeground(lipgloss.Color("10"))
	}
	saveBtn := saveStyle.Render(" Save ")

	cancelStyle := btnBase
	if m.focusIndex == 6 {
		cancelStyle = cancelStyle.Bold(true).Foreground(lipgloss.Color("9")).BorderForeground(lipgloss.Color("9"))
	}
	cancelBtn := cancelStyle.Render(" Cancel ")

	content.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, saveBtn, "  ", cancelBtn) + "\n\n")

	// Help text
	content.WriteString(dimStyle.Render("Tab: cycle fields • Enter: submit/save/click • Esc: close"))

	return content.String()
}

// fetchLink is stage 1: check if link exists (return complete) or fetch HTML.
func (m AddLinkModel) fetchLink(url string, db *database.Database, fetcher *services.Fetcher, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		// Check if link already exists
		existingLink, err := db.Queries.GetLinkByURL(ctx, url)
		if err == nil {
			return linkProcessCompleteMsg{
				linkID:   existingLink.ID,
				preview:  existingLink.Content.String,
				summary:  existingLink.Summary.String,
				category: "",
				tags:     []string{},
				llmCost:  0,
			}
		}
		html, err := fetcher.FetchURL(ctx, url)
		if err != nil {
			return linkProcessErrorMsg{err: fmt.Errorf("fetch failed: %w", err)}
		}
		return linkFetchedMsg{url: url, html: html}
	}
}

// extractLink is stage 2: extract text from fetched HTML.
func (m AddLinkModel) extractLink(url, html string, extractor *services.Extractor) tea.Cmd {
	return func() tea.Msg {
		title, text, err := extractor.ExtractText(html)
		if err != nil {
			return linkProcessErrorMsg{err: fmt.Errorf("extraction failed: %w", err)}
		}
		preview := text
		content := extractor.TruncateText(text, 10000)
		return linkExtractedMsg{url: url, title: title, text: text, content: content, preview: preview}
	}
}

// summarizeAndSave is stage 3: summarize with AI and save to DB.
func (m AddLinkModel) summarizeAndSave(url, title, text, content, preview string, db *database.Database, summarizer *services.Summarizer, ctx context.Context) tea.Cmd {
	return func() tea.Msg {
		var summary string
		var category string
		var tags []string
		var totalInputTokens, totalOutputTokens int

		if summarizer != nil {
			var inTok, outTok int
			summary, inTok, outTok, _ = summarizer.Summarize(ctx, title, text)
			totalInputTokens += inTok
			totalOutputTokens += outTok
			category, tags, inTok, outTok, _ = summarizer.SuggestMetadata(ctx, title, text)
			totalInputTokens += inTok
			totalOutputTokens += outTok
		}

		// GPT-4o-mini pricing: $0.150/1M input tokens, $0.600/1M output tokens
		llmCost := float64(totalInputTokens)*0.15/1_000_000.0 +
			float64(totalOutputTokens)*0.60/1_000_000.0

		if category == "" {
			category = "General"
		}
		if len(tags) == 0 {
			tags = []string{}
		}

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

		return linkProcessCompleteMsg{
			linkID:   link.ID,
			preview:  preview,
			summary:  summary,
			category: category,
			tags:     tags,
			llmCost:  llmCost,
		}
	}
}

// Messages

type linkFetchedMsg struct {
	url  string
	html string
}

type linkExtractedMsg struct {
	url     string
	title   string
	text    string
	content string
	preview string
}

type linkProcessCompleteMsg struct {
	linkID   int64
	preview  string
	summary  string
	category string
	tags     []string
	llmCost  float64 // USD cost of LLM calls (0 if no LLM was used)
}

type linkProcessErrorMsg struct {
	err error
}

type addLinkCloseRequestedMsg struct{}

type metadataSavedMsg struct{}
