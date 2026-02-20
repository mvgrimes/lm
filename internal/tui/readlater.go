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
)

type ReadLaterModel struct {
	links         []models.Link
	filteredLinks []models.Link
	cursor        int
	db            *database.Database
	ctx           context.Context

	// Search and focus
	searchInput textinput.Model
	focus       panelFocus

	// Detail view
	detailViewport viewport.Model
	viewportReady  bool

	width  int
	height int
}

func NewReadLaterModel(db *database.Database) ReadLaterModel {
	searchInput := textinput.New()
	searchInput.Placeholder = "Search read-later links..."
	searchInput.Width = 50
	searchInput.Prompt = "🔍 "
	searchInput.Focus()

	return ReadLaterModel{
		db:          db,
		ctx:         context.Background(),
		searchInput: searchInput,
		focus:       panelFocusSearch,
	}
}

func (m ReadLaterModel) Init() tea.Cmd {
	return tea.Batch(m.loadLinks(), textinput.Blink)
}

func (m ReadLaterModel) Update(msg tea.Msg) (ReadLaterModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		leftWidth := int(float64(m.width) * 0.35)
		if leftWidth < 30 {
			leftWidth = 30
		}
		rightWidth := m.width - leftWidth - 8
		detailHeight := m.height - 12
		if detailHeight < 5 {
			detailHeight = 5
		}

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
		halfPage := (m.height - 15) / 2
		if halfPage < 1 {
			halfPage = 1
		}

		switch msg.String() {
		case "tab":
			m.focus = cycleFocusForward(m.focus)
			if m.focus == panelFocusSearch {
				m.searchInput.Focus()
			} else {
				m.searchInput.Blur()
			}
			return m, nil
		case "shift+tab":
			m.focus = cycleFocusBackward(m.focus)
			if m.focus == panelFocusSearch {
				m.searchInput.Focus()
			} else {
				m.searchInput.Blur()
			}
			return m, nil
		}

		switch m.focus {
		case panelFocusList:
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
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.filterLinks()
			if len(m.filteredLinks) > 0 {
				m.updateDetailView()
			}
			return m, cmd
		}

	case readLaterLoadedMsg:
		m.links = msg.links
		m.filterLinks()
		if len(m.filteredLinks) > 0 {
			m.updateDetailView()
		}
		return m, nil
	}

	return m, nil
}

func (m ReadLaterModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	leftWidth := int(float64(m.width) * 0.35)
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := m.width - leftWidth - 8

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	// Search box
	searchBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(panelBorderColor(m.focus == panelFocusSearch))).
		Padding(0, 1).
		Width(leftWidth - 4)
	searchBox := searchBoxStyle.Render(m.searchInput.View())

	// Left panel
	leftPanelStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(panelBorderColor(m.focus == panelFocusList))).
		Padding(1)

	leftContent := searchBox + "\n\n"

	if len(m.filteredLinks) == 0 {
		if m.searchInput.Value() != "" {
			leftContent += dimStyle.Render("No links match your search.\n")
		} else {
			leftContent += dimStyle.Render("No links to read later. Add one with Ctrl+A!\n")
		}
	} else {
		maxLinks := m.height - 15
		if maxLinks < 3 {
			maxLinks = 3
		}
		startIdx := 0
		endIdx := len(m.filteredLinks)
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
			if len(title) > leftWidth-8 {
				title = title[:leftWidth-11] + "..."
			}
			line := fmt.Sprintf("%s%s", cursor, title)
			if i == m.cursor {
				leftContent += selectedStyle.Render(line) + "\n"
				if link.Summary.Valid && link.Summary.String != "" {
					summary := link.Summary.String
					if len(summary) > leftWidth-8 {
						summary = summary[:leftWidth-11] + "..."
					}
					leftContent += dimStyle.Render("  "+summary) + "\n"
				}
			} else {
				leftContent += line + "\n"
			}
		}
		if len(m.filteredLinks) > maxLinks {
			leftContent += "\n" + dimStyle.Render(fmt.Sprintf("  [%d/%d links]", m.cursor+1, len(m.filteredLinks)))
		}
	}

	leftPanel := leftPanelStyle.Render(leftContent)

	// Right panel — detail view
	rightBorderColor := "12"
	if m.focus == panelFocusDetail {
		rightBorderColor = "10"
	}
	rightPanelStyle := lipgloss.NewStyle().
		Width(rightWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(rightBorderColor)).
		Padding(1)

	var rightContent string
	if len(m.filteredLinks) > 0 && m.cursor < len(m.filteredLinks) {
		link := m.filteredLinks[m.cursor]
		titleLine := titleStyle.Render("Details") + "\n\n"
		urlStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
		titleLine += urlStyle.Render(link.Url) + "\n\n"
		rightContent = titleLine + m.detailViewport.View()
		if m.viewportReady && m.detailViewport.TotalLineCount() > m.detailViewport.Height {
			scrollPercent := int(m.detailViewport.ScrollPercent() * 100)
			rightContent += "\n" + dimStyle.Render(fmt.Sprintf("[%d%% - PgUp/PgDn to scroll]", scrollPercent))
		}
	} else {
		rightContent = dimStyle.Render("Select a link to view details...")
	}

	rightPanel := rightPanelStyle.Render(rightContent)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, "  ", rightPanel)

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	var helpMsg string
	switch m.focus {
	case panelFocusList:
		helpMsg = "Tab: detail • ↑/↓/j/k: navigate • PgUp/PgDn/Ctrl+U/D: jump • Enter/Ctrl+O: open • Ctrl+A: add • Esc: search"
	case panelFocusDetail:
		helpMsg = "Tab: search • ↑/↓/j/k/PgUp/PgDn: scroll • Ctrl+O: open • Esc: search"
	default:
		helpMsg = "type to search • Tab: list • ↑/↓: navigate • Enter/Ctrl+O: open • Ctrl+A: add • Esc: clear"
	}
	helpText := "\n" + helpStyle.Render(helpMsg)

	return mainContent + helpText
}

func (m *ReadLaterModel) filterLinks() {
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filteredLinks = m.links
		if m.cursor >= len(m.filteredLinks) {
			m.cursor = 0
		}
		return
	}
	m.filteredLinks = []models.Link{}
	for _, link := range m.links {
		if linkMatchesQuery(link.Url, link.Title.String, link.Content.String, link.Summary.String, query) {
			m.filteredLinks = append(m.filteredLinks, link)
		}
	}
	if m.cursor >= len(m.filteredLinks) {
		m.cursor = 0
	}
}

func (m *ReadLaterModel) updateDetailView() {
	if !m.viewportReady || len(m.filteredLinks) == 0 || m.cursor >= len(m.filteredLinks) {
		return
	}
	link := m.filteredLinks[m.cursor]

	var doc strings.Builder

	if link.Title.Valid && link.Title.String != "" {
		doc.WriteString("# " + link.Title.String + "\n\n")
	}
	if link.Summary.Valid && link.Summary.String != "" {
		doc.WriteString("**Summary:** " + link.Summary.String + "\n\n")
	}
	if link.Content.Valid && link.Content.String != "" {
		doc.WriteString("---\n\n")
		doc.WriteString(link.Content.String)
	}

	m.detailViewport.SetContent(renderMarkdown(doc.String(), m.detailViewport.Width))
	m.detailViewport.GotoTop()
}

func (m ReadLaterModel) loadLinks() tea.Cmd {
	return func() tea.Msg {
		links, err := m.db.Queries.ListLinksByStatus(m.ctx, models.ListLinksByStatusParams{
			Status: "read_later",
			Limit:  1000,
			Offset: 0,
		})
		if err != nil {
			return errMsg{err: err}
		}
		return readLaterLoadedMsg{links: links}
	}
}

func (m ReadLaterModel) openLink(url string) tea.Cmd {
	return func() tea.Msg {
		_ = browser.OpenURL(url)
		return nil
	}
}

type readLaterLoadedMsg struct {
	links []models.Link
}
