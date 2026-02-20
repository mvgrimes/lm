package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pkg/browser"

	"mccwk.com/lm/internal/database"
	"mccwk.com/lm/internal/models"
	"mccwk.com/lm/internal/services"
)

type LinksModel struct {
	links         []models.Link
	filteredLinks []models.Link
	cursor        int
	db            *database.Database
	ctx           context.Context

	// Search functionality
	searchInput   textinput.Model
	searchFocused bool

	// Detail view
	detailViewport viewport.Model
	viewportReady  bool

	// Edit mode
	editMode      bool
	editLinkModel EditLinkModel

	// Services for edit dialog
	fetcher    *services.Fetcher
	extractor  *services.Extractor
	summarizer *services.Summarizer

	width  int
	height int
}

func NewLinksModel(db *database.Database) LinksModel {
	searchInput := textinput.New()
	searchInput.Placeholder = "Search links..."
	searchInput.Width = 50
	searchInput.Prompt = "🔍 "

	return LinksModel{
		db:            db,
		ctx:           context.Background(),
		searchInput:   searchInput,
		searchFocused: false,
	}
}

func (m *LinksModel) SetServices(fetcher *services.Fetcher, extractor *services.Extractor, summarizer *services.Summarizer) {
	m.fetcher = fetcher
	m.extractor = extractor
	m.summarizer = summarizer
}

func (m LinksModel) Init() tea.Cmd {
	return m.loadLinks()
}

func (m LinksModel) Update(msg tea.Msg) (LinksModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Calculate responsive widths for split view
		leftWidth := int(float64(m.width) * 0.35)
		if leftWidth < 30 {
			leftWidth = 30
		}
		rightWidth := m.width - leftWidth - 8

		// Calculate height for detail viewport
		// Account for: title(2) + tabs(3) + search(3) + footer(2) + borders(2)
		detailHeight := m.height - 12
		if detailHeight < 5 {
			detailHeight = 5
		}

		// Initialize or update detail viewport
		if !m.viewportReady {
			m.detailViewport = viewport.New(rightWidth-4, detailHeight)
			m.detailViewport.SetContent("")
			m.viewportReady = true
		} else {
			m.detailViewport.Width = rightWidth - 4
			m.detailViewport.Height = detailHeight
		}

		return m, nil

	case tea.KeyMsg:
		// If in edit mode, delegate to editLinkModel
		if m.editMode {
			if msg.String() == "esc" {
				m.editMode = false
				return m, m.loadLinks() // Reload links to show any changes
			}
			m.editLinkModel, cmd = m.editLinkModel.Update(msg)
			return m, cmd
		}

		// Handle search focus toggle
		if msg.String() == "/" && !m.searchFocused {
			m.searchFocused = true
			m.searchInput.Focus()
			return m, nil
		}

		if msg.String() == "esc" && m.searchFocused {
			m.searchFocused = false
			m.searchInput.Blur()
			return m, nil
		}

		// If search is focused, handle input
		if m.searchFocused {
			if msg.String() == "enter" {
				m.searchFocused = false
				m.searchInput.Blur()
				m.filterLinks()
				return m, nil
			}
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.filterLinks()
			return m, cmd
		}

		// Navigation keys when search not focused
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.updateDetailView()
			}
		case "down", "j":
			if m.cursor < len(m.filteredLinks)-1 {
				m.cursor++
				m.updateDetailView()
			}
		case "o", "enter":
			if len(m.filteredLinks) > 0 && m.cursor < len(m.filteredLinks) {
				return m, m.openLink(m.filteredLinks[m.cursor].Url)
			}
		case "d":
			// Delete link
			if len(m.filteredLinks) > 0 && m.cursor < len(m.filteredLinks) {
				return m, m.deleteLink(m.filteredLinks[m.cursor].ID)
			}
		case "e":
			// Edit link
			if len(m.filteredLinks) > 0 && m.cursor < len(m.filteredLinks) {
				m.editMode = true
				m.editLinkModel = NewEditLinkModel(
					m.filteredLinks[m.cursor],
					m.db,
					m.ctx,
					m.fetcher,
					m.extractor,
					m.summarizer,
				)
				// Load existing categories and tags for the link
				categories, _ := m.db.Queries.GetCategoriesForLink(m.ctx, m.filteredLinks[m.cursor].ID)
				if len(categories) > 0 {
					catNames := []string{}
					for _, cat := range categories {
						catNames = append(catNames, cat.Name)
					}
					m.editLinkModel.categoryInput.SetValue(strings.Join(catNames, ", "))
				}
				tags, _ := m.db.Queries.GetTagsForLink(m.ctx, m.filteredLinks[m.cursor].ID)
				if len(tags) > 0 {
					tagNames := []string{}
					for _, tag := range tags {
						tagNames = append(tagNames, tag.Name)
					}
					m.editLinkModel.tagsInput.SetValue(strings.Join(tagNames, ", "))
				}
				// Send window size to initialize
				return m, func() tea.Msg {
					return tea.WindowSizeMsg{Width: m.width, Height: m.height}
				}
			}
		case "pgup", "pgdown":
			// Scroll detail viewport
			if m.viewportReady {
				m.detailViewport, cmd = m.detailViewport.Update(msg)
				return m, cmd
			}
		}

	case linksLoadedMsg:
		m.links = msg.links
		m.filterLinks()
		if len(m.filteredLinks) > 0 {
			m.updateDetailView()
		}
		return m, nil

	case linkDeletedMsg:
		return m, m.loadLinks()
	default:
		if m.editMode {
			m.editLinkModel, cmd = m.editLinkModel.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m LinksModel) View() string {
	// Show edit dialog if in edit mode
	if m.editMode {
		modalWidth := m.width - 20
		if modalWidth > 80 {
			modalWidth = 80
		}
		if modalWidth < 60 {
			modalWidth = 60
		}

		modalContent := m.editLinkModel.View()

		// Style the modal
		modalStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("10")).
			Padding(2).
			Width(modalWidth)

		modal := modalStyle.Render(modalContent)

		// Center the modal
		centeredModal := lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			modal,
		)

		return centeredModal
	}

	if m.width == 0 {
		return "Loading..."
	}

	// Calculate responsive widths
	leftWidth := int(float64(m.width) * 0.35)
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := m.width - leftWidth - 8

	// Title and search bar
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6"))

	searchBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(0, 1).
		Width(leftWidth - 4)

	var searchBox string
	if m.searchFocused {
		searchBox = searchBoxStyle.
			BorderForeground(lipgloss.Color("10")).
			Render(m.searchInput.View())
	} else {
		searchBox = searchBoxStyle.Render(m.searchInput.View())
	}

	// Left panel - link list
	leftPanelStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	leftContent := searchBox + "\n\n"

	if len(m.filteredLinks) == 0 {
		if m.searchInput.Value() != "" {
			leftContent += dimStyle.Render("No links match your search.\n")
		} else {
			leftContent += dimStyle.Render("No links yet. Press Ctrl+A to add one!\n")
		}
	} else {
		// Show links list with scrolling
		maxLinks := m.height - 15 // Account for UI elements
		if maxLinks < 3 {
			maxLinks = 3
		}

		startIdx := 0
		endIdx := len(m.filteredLinks)

		// Ensure cursor is visible
		if m.cursor >= maxLinks {
			startIdx = m.cursor - maxLinks + 1
		}
		if endIdx > startIdx+maxLinks {
			endIdx = startIdx + maxLinks
		}

		for i := startIdx; i < endIdx; i++ {
			link := m.filteredLinks[i]
			cursor := "  "
			if i == m.cursor {
				cursor = "• "
			}

			title := link.Title.String
			if title == "" {
				title = link.Url
			}
			// Truncate title to fit
			if len(title) > leftWidth-8 {
				title = title[:leftWidth-11] + "..."
			}

			line := fmt.Sprintf("%s%s", cursor, title)

			if i == m.cursor {
				leftContent += selectedStyle.Render(line) + "\n"
			} else {
				leftContent += line + "\n"
			}

			// Show short summary for all items
			if link.Summary.Valid && link.Summary.String != "" {
				summary := link.Summary.String
				if len(summary) > leftWidth-8 {
					summary = summary[:leftWidth-11] + "..."
				}
				leftContent += dimStyle.Render("  "+summary) + "\n"
			}
		}

		// Show scroll indicator
		if len(m.filteredLinks) > maxLinks {
			leftContent += "\n" + dimStyle.Render(fmt.Sprintf("  [%d/%d links]", m.cursor+1, len(m.filteredLinks)))
		}
	}

	leftPanel := leftPanelStyle.Render(leftContent)

	// Right panel - detail view
	rightPanelStyle := lipgloss.NewStyle().
		Width(rightWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(1)

	var rightContent string

	if len(m.filteredLinks) > 0 && m.cursor < len(m.filteredLinks) {
		link := m.filteredLinks[m.cursor]

		titleLine := titleStyle.Render("Details") + "\n\n"

		// URL
		urlStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
		titleLine += urlStyle.Render(link.Url) + "\n\n"

		rightContent = titleLine + m.detailViewport.View()

		// Show scroll indicator
		if m.viewportReady && m.detailViewport.TotalLineCount() > m.detailViewport.Height {
			scrollPercent := int(m.detailViewport.ScrollPercent() * 100)
			scrollInfo := lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Render(fmt.Sprintf("\n[%d%% - PgUp/PgDn to scroll]", scrollPercent))
			rightContent += scrollInfo
		}
	} else {
		rightContent = dimStyle.Render("Select a link to view details...")
	}

	rightPanel := rightPanelStyle.Render(rightContent)

	// Combine panels
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, "  ", rightPanel)

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpText := "\n" + helpStyle.Render("/: search • arrows/j/k: navigate • Enter/o: open • e: edit • d: delete • PgUp/PgDn: scroll details")

	return mainContent + helpText
}

func (m *LinksModel) filterLinks() {
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filteredLinks = m.links
		m.cursor = 0
		return
	}

	m.filteredLinks = []models.Link{}
	for _, link := range m.links {
		// Search in URL, title, content, and summary
		if strings.Contains(strings.ToLower(link.Url), query) ||
			(link.Title.Valid && strings.Contains(strings.ToLower(link.Title.String), query)) ||
			(link.Content.Valid && strings.Contains(strings.ToLower(link.Content.String), query)) ||
			(link.Summary.Valid && strings.Contains(strings.ToLower(link.Summary.String), query)) {
			m.filteredLinks = append(m.filteredLinks, link)
		}
	}

	// Reset cursor
	if m.cursor >= len(m.filteredLinks) {
		m.cursor = 0
	}
}

func (m *LinksModel) updateDetailView() {
	if !m.viewportReady || len(m.filteredLinks) == 0 || m.cursor >= len(m.filteredLinks) {
		return
	}

	link := m.filteredLinks[m.cursor]

	// Get viewport width for wrapping
	wrapWidth := m.detailViewport.Width
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	var content strings.Builder

	// Title
	if link.Title.Valid && link.Title.String != "" {
		wrapped := wrapText(link.Title.String, wrapWidth)
		content.WriteString(lipgloss.NewStyle().Bold(true).Render(wrapped))
		content.WriteString("\n\n")
	}

	// Summary
	if link.Summary.Valid && link.Summary.String != "" {
		summaryStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Bold(true)
		content.WriteString(summaryStyle.Render("Summary:") + "\n")
		wrapped := wrapText(link.Summary.String, wrapWidth)
		content.WriteString(wrapped)
		content.WriteString("\n\n")
	}

	// Tags (if any)
	tags, _ := m.db.Queries.GetTagsForLink(m.ctx, link.ID)
	if len(tags) > 0 {
		tagStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
		content.WriteString(tagStyle.Render("Tags: "))
		tagNames := []string{}
		for _, tag := range tags {
			tagNames = append(tagNames, tag.Name)
		}
		tagsText := strings.Join(tagNames, ", ")
		wrapped := wrapText(tagsText, wrapWidth-7) // Account for "Tags: " prefix
		content.WriteString(wrapped)
		content.WriteString("\n\n")
	}

	// Categories (if any)
	categories, _ := m.db.Queries.GetCategoriesForLink(m.ctx, link.ID)
	if len(categories) > 0 {
		catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
		content.WriteString(catStyle.Render("Categories: "))
		catNames := []string{}
		for _, cat := range categories {
			catNames = append(catNames, cat.Name)
		}
		catsText := strings.Join(catNames, ", ")
		wrapped := wrapText(catsText, wrapWidth-12) // Account for "Categories: " prefix
		content.WriteString(wrapped)
		content.WriteString("\n\n")
	}

	// Content
	if link.Content.Valid && link.Content.String != "" {
		contentStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("7"))
		contentLabelStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("11")) // Yellow color
		content.WriteString(contentLabelStyle.Render("Content:") + "\n")
		wrapped := wrapText(link.Content.String, wrapWidth)
		content.WriteString(contentStyle.Render(wrapped))
	}

	m.detailViewport.SetContent(content.String())
	m.detailViewport.GotoTop()
}

func (m LinksModel) loadLinks() tea.Cmd {
	return func() tea.Msg {
		// Load all links, not just by status
		links, err := m.db.Queries.ListLinks(m.ctx, models.ListLinksParams{
			Limit:  1000,
			Offset: 0,
		})
		if err != nil {
			return errMsg{err: err}
		}
		return linksLoadedMsg{links: links}
	}
}

func (m LinksModel) openLink(url string) tea.Cmd {
	return func() tea.Msg {
		_ = browser.OpenURL(url)
		return nil
	}
}

func (m LinksModel) deleteLink(linkID int64) tea.Cmd {
	return func() tea.Msg {
		err := m.db.Queries.DeleteLink(m.ctx, linkID)
		if err != nil {
			return errMsg{err: err}
		}
		return linkDeletedMsg{}
	}
}

type linkDeletedMsg struct{}
