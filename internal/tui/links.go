package tui

import (
	"context"
	"fmt"
	"sort"
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

// linksSortMode controls the order of the links list.
type linksSortMode int

const (
	linksSortDateDesc  linksSortMode = iota // newest first (default)
	linksSortDateAsc                        // oldest first
	linksSortTitleAsc                       // A → Z
	linksSortTitleDesc                      // Z → A
)

func (s linksSortMode) String() string {
	switch s {
	case linksSortDateAsc:
		return "date ↑"
	case linksSortTitleAsc:
		return "title A-Z"
	case linksSortTitleDesc:
		return "title Z-A"
	default:
		return "date ↓"
	}
}

type LinksModel struct {
	links         []models.Link
	filteredLinks []models.Link
	cursor        int
	db            *database.Database
	ctx           context.Context

	// Search and sort
	searchInput textinput.Model
	focus       panelFocus
	sortMode    linksSortMode

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
	searchInput.Focus()

	return LinksModel{
		db:          db,
		ctx:         context.Background(),
		searchInput: searchInput,
		focus:       panelFocusSearch,
	}
}

func (m *LinksModel) SetServices(fetcher *services.Fetcher, extractor *services.Extractor, summarizer *services.Summarizer) {
	m.fetcher = fetcher
	m.extractor = extractor
	m.summarizer = summarizer
}

func (m LinksModel) Init() tea.Cmd {
	return tea.Batch(m.loadLinks(), textinput.Blink)
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
			m.updateDetailView() // populate if links were loaded before viewport was ready
		} else {
			m.detailViewport.Width = rightWidth - 4
			m.detailViewport.Height = detailHeight
		}
		m.updateDetailView()

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

		halfPage := (m.height - 15) / 2
		if halfPage < 1 {
			halfPage = 1
		}

		// Tab / Shift+Tab cycle focus between search → list → detail.
		// s cycles the sort mode from any focus area.
		switch msg.String() {
		case "tab":
			m.focus = (m.focus + 1) % 3
			if m.focus == panelFocusSearch {
				m.searchInput.Focus()
			} else {
				m.searchInput.Blur()
			}
			return m, nil
		case "shift+tab":
			m.focus = (m.focus + 2) % 3 // -1 mod 3
			if m.focus == panelFocusSearch {
				m.searchInput.Focus()
			} else {
				m.searchInput.Blur()
			}
			return m, nil
		case "s":
			// Only cycle sort when focus is NOT on the search input
			// (so typing 's' in search still filters).
			if m.focus != panelFocusSearch {
				m.sortMode = (m.sortMode + 1) % 4
				m.filterLinks()
				m.updateDetailView()
				return m, nil
			}
		}

		switch m.focus {
		case panelFocusList:
			// List-focused: navigate with arrows/j/k, open with Enter/Ctrl+O, back to search with Esc.
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
			case "pgup", "ctrl+u":
				m.cursor -= halfPage
				if m.cursor < 0 {
					m.cursor = 0
				}
				m.updateDetailView()
			case "pgdown", "ctrl+d":
				m.cursor += halfPage
				if m.cursor >= len(m.filteredLinks) {
					m.cursor = len(m.filteredLinks) - 1
				}
				m.updateDetailView()
			case "enter", "ctrl+o":
				if len(m.filteredLinks) > 0 && m.cursor < len(m.filteredLinks) {
					return m, m.openLink(m.filteredLinks[m.cursor].Url)
				}
			case "ctrl+a":
				return m, func() tea.Msg { return openAddLinkModalMsg{} }
			case "esc":
				m.focus = panelFocusSearch
				m.searchInput.Focus()
			}
			return m, nil

		case panelFocusDetail:
			// Detail-focused: scroll the viewport, Esc goes back.
			switch msg.String() {
			case "pgup", "pgdown", "ctrl+u", "ctrl+d":
				if m.viewportReady {
					m.detailViewport, cmd = m.detailViewport.Update(msg)
					return m, cmd
				}
			case "up", "k":
				if m.viewportReady {
					m.detailViewport.ScrollUp(1)
				}
			case "down", "j":
				if m.viewportReady {
					m.detailViewport.ScrollDown(1)
				}
			case "esc":
				m.focus = panelFocusSearch
				m.searchInput.Focus()
			}
			return m, nil

		default: // panelFocusSearch
			// Up/Down navigate the list; ctrl+a/ctrl+o work from any focus.
			switch msg.String() {
			case "up":
				if m.cursor > 0 {
					m.cursor--
					m.updateDetailView()
				}
				return m, nil
			case "down":
				if m.cursor < len(m.filteredLinks)-1 {
					m.cursor++
					m.updateDetailView()
				}
				return m, nil
			case "enter", "ctrl+o":
				if len(m.filteredLinks) > 0 && m.cursor < len(m.filteredLinks) {
					return m, m.openLink(m.filteredLinks[m.cursor].Url)
				}
				return m, nil
			case "ctrl+a":
				return m, func() tea.Msg { return openAddLinkModalMsg{} }
			case "esc":
				m.searchInput.SetValue("")
				m.filterLinks()
				return m, nil
			}
			// All other keys feed the search input for live filtering.
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.filterLinks()
			return m, cmd
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

	searchBorderColor := lipgloss.Color("10")
	if m.focus != panelFocusSearch {
		searchBorderColor = lipgloss.Color("8")
	}
	searchBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(searchBorderColor).
		Padding(0, 1).
		Width(leftWidth - 4)

	searchBox := searchBoxStyle.Render(m.searchInput.View())

	// Left panel — link list; highlight border when list is focused.
	leftBorderColor := lipgloss.Color("8")
	if m.focus == panelFocusList {
		leftBorderColor = lipgloss.Color("10")
	}
	leftPanelStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(leftBorderColor).
		Padding(1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	sortStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	sortIndicator := sortStyle.Render(fmt.Sprintf("  sort: %s", m.sortMode.String()))
	leftContent := searchBox + "\n" + sortIndicator + "\n\n"

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

	// Right panel — detail view; highlight border when detail panel is focused.
	rightBorderColor := lipgloss.Color("12")
	if m.focus == panelFocusDetail {
		rightBorderColor = lipgloss.Color("10")
	}
	rightPanelStyle := lipgloss.NewStyle().
		Width(rightWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rightBorderColor).
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

	// Help text — adapt to current focus area
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	var helpMsg string
	switch m.focus {
	case panelFocusList:
		helpMsg = "Tab: detail • ↑/↓/j/k: navigate • PgUp/PgDn/Ctrl+U/D: jump • Enter/Ctrl+O: open • Ctrl+A: add • s: sort • Esc: search"
	case panelFocusDetail:
		helpMsg = "Tab: search • ↑/↓/j/k/PgUp/PgDn: scroll • Ctrl+O: open • Esc: search"
	default:
		helpMsg = "type to search • Tab: list • ↑/↓: navigate • Enter/Ctrl+O: open • Ctrl+A: add • Esc: clear"
	}
	helpText := "\n" + helpStyle.Render(helpMsg)

	return mainContent + helpText
}

func (m *LinksModel) filterLinks() {
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		// Copy slice so we can sort without mutating m.links
		filtered := make([]models.Link, len(m.links))
		copy(filtered, m.links)
		m.filteredLinks = filtered
	} else {
		m.filteredLinks = []models.Link{}
		for _, link := range m.links {
			if linkMatchesQuery(link.Url, link.Title.String, link.Content.String, link.Summary.String, query) {
				m.filteredLinks = append(m.filteredLinks, link)
			}
		}
	}

	// Apply sort
	switch m.sortMode {
	case linksSortDateAsc:
		sort.Slice(m.filteredLinks, func(i, j int) bool {
			return m.filteredLinks[i].CreatedAt.Before(m.filteredLinks[j].CreatedAt)
		})
	case linksSortTitleAsc:
		sort.Slice(m.filteredLinks, func(i, j int) bool {
			ti := strings.ToLower(m.filteredLinks[i].Title.String)
			tj := strings.ToLower(m.filteredLinks[j].Title.String)
			if ti == "" {
				ti = strings.ToLower(m.filteredLinks[i].Url)
			}
			if tj == "" {
				tj = strings.ToLower(m.filteredLinks[j].Url)
			}
			return ti < tj
		})
	case linksSortTitleDesc:
		sort.Slice(m.filteredLinks, func(i, j int) bool {
			ti := strings.ToLower(m.filteredLinks[i].Title.String)
			tj := strings.ToLower(m.filteredLinks[j].Title.String)
			if ti == "" {
				ti = strings.ToLower(m.filteredLinks[i].Url)
			}
			if tj == "" {
				tj = strings.ToLower(m.filteredLinks[j].Url)
			}
			return ti > tj
		})
	default: // linksSortDateDesc
		sort.Slice(m.filteredLinks, func(i, j int) bool {
			return m.filteredLinks[i].CreatedAt.After(m.filteredLinks[j].CreatedAt)
		})
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

	var doc strings.Builder

	// Title
	if link.Title.Valid && link.Title.String != "" {
		doc.WriteString("# " + link.Title.String + "\n\n")
	}

	// Summary
	if link.Summary.Valid && link.Summary.String != "" {
		doc.WriteString("**Summary:** " + link.Summary.String + "\n\n")
	}

	// Tags
	tags, _ := m.db.Queries.GetTagsForLink(m.ctx, link.ID)
	if len(tags) > 0 {
		tagNames := make([]string, len(tags))
		for i, t := range tags {
			tagNames[i] = t.Name
		}
		doc.WriteString("**Tags:** " + strings.Join(tagNames, ", ") + "\n\n")
	}

	// Categories
	categories, _ := m.db.Queries.GetCategoriesForLink(m.ctx, link.ID)
	if len(categories) > 0 {
		catNames := make([]string, len(categories))
		for i, c := range categories {
			catNames[i] = c.Name
		}
		doc.WriteString("**Categories:** " + strings.Join(catNames, ", ") + "\n\n")
	}

	// Content (already markdown from the extractor)
	if link.Content.Valid && link.Content.String != "" {
		doc.WriteString("---\n\n")
		doc.WriteString(link.Content.String)
	}

	m.detailViewport.SetContent(renderMarkdown(doc.String(), m.detailViewport.Width))
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
