package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mccwk.com/lm/internal/database"
	"mccwk.com/lm/internal/models"
)

type tagsMode int

const (
	tagsViewMode tagsMode = iota
	tagsCreateMode
)

type TagsModel struct {
	tags         []models.Tag
	filteredTags []models.Tag
	cursor       int
	db           *database.Database
	ctx          context.Context
	mode         tagsMode
	links        []models.Link

	// Search and focus
	searchInput textinput.Model
	focus       panelFocus

	// Detail viewport for links panel
	detailViewport viewport.Model
	viewportReady  bool

	// Create mode
	nameInput textinput.Model

	width  int
	height int
}

func NewTagsModel(db *database.Database) TagsModel {
	searchInput := textinput.New()
	searchInput.Placeholder = "Search tags..."
	searchInput.Width = 50
	searchInput.Prompt = "🔍 "
	searchInput.Focus()

	nameInput := textinput.New()
	nameInput.Placeholder = "Tag name..."
	nameInput.Width = 50
	nameInput.Prompt = "Name: "

	return TagsModel{
		db:          db,
		ctx:         context.Background(),
		mode:        tagsViewMode,
		searchInput: searchInput,
		nameInput:   nameInput,
		focus:       panelFocusSearch,
	}
}

func (m TagsModel) Init() tea.Cmd {
	return m.loadTags()
}

func (m TagsModel) Update(msg tea.Msg) (TagsModel, tea.Cmd) {
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
		} else {
			m.detailViewport.Width = rightWidth - 4
			m.detailViewport.Height = detailHeight
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case tagsViewMode:
			return m.handleViewMode(msg)
		case tagsCreateMode:
			return m.handleCreateMode(msg)
		}

	case tagsLoadedMsg:
		m.tags = msg.tags
		m.filterTags()
		if len(m.filteredTags) > 0 {
			return m, m.loadTagLinks(m.filteredTags[m.cursor].ID)
		}
		return m, nil

	case tagCreatedMsg:
		m.mode = tagsViewMode
		m.nameInput.SetValue("")
		m.nameInput.Blur()
		m.searchInput.Focus()
		return m, tea.Batch(m.loadTags(), notifyCmd("info", "Tag created!"))

	case tagLinksLoadedMsg:
		m.links = msg.links
		m.updateLinksView()
		return m, nil
	}

	return m, nil
}

func (m TagsModel) handleViewMode(msg tea.KeyMsg) (TagsModel, tea.Cmd) {
	// Tab / Shift+Tab cycle focus between search → list → detail.
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
				if len(m.filteredTags) > 0 {
					return m, m.loadTagLinks(m.filteredTags[m.cursor].ID)
				}
			}
		case "down", "j":
			if m.cursor < len(m.filteredTags)-1 {
				m.cursor++
				if len(m.filteredTags) > 0 {
					return m, m.loadTagLinks(m.filteredTags[m.cursor].ID)
				}
			}
		case "n":
			m.mode = tagsCreateMode
			m.focus = panelFocusSearch
			m.searchInput.Blur()
			m.nameInput.Focus()
		case "d":
			if len(m.filteredTags) > 0 && m.cursor < len(m.filteredTags) {
				return m, m.deleteTag(m.filteredTags[m.cursor].ID)
			}
		case "esc":
			m.focus = panelFocusSearch
			m.searchInput.Focus()
		}
		return m, nil

	case panelFocusDetail:
		switch msg.String() {
		case "pgup", "pgdown":
			if m.viewportReady {
				var cmd tea.Cmd
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
				if len(m.filteredTags) > 0 {
					return m, m.loadTagLinks(m.filteredTags[m.cursor].ID)
				}
			}
			return m, nil
		case "down":
			if m.cursor < len(m.filteredTags)-1 {
				m.cursor++
				if len(m.filteredTags) > 0 {
					return m, m.loadTagLinks(m.filteredTags[m.cursor].ID)
				}
			}
			return m, nil
		case "n":
			m.mode = tagsCreateMode
			m.searchInput.Blur()
			m.nameInput.Focus()
			return m, nil
		case "d":
			if len(m.filteredTags) > 0 && m.cursor < len(m.filteredTags) {
				return m, m.deleteTag(m.filteredTags[m.cursor].ID)
			}
			return m, nil
		case "esc":
			m.searchInput.SetValue("")
			m.filterTags()
			return m, nil
		}
		// All other keys feed the search input
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		prevLen := len(m.filteredTags)
		m.filterTags()
		if len(m.filteredTags) > 0 && (len(m.filteredTags) != prevLen || m.cursor == 0) {
			if m.cursor >= len(m.filteredTags) {
				m.cursor = 0
			}
			return m, tea.Batch(cmd, m.loadTagLinks(m.filteredTags[m.cursor].ID))
		}
		return m, cmd
	}
}

func (m TagsModel) handleCreateMode(msg tea.KeyMsg) (TagsModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg.String() {
	case "esc":
		m.mode = tagsViewMode
		m.nameInput.SetValue("")
		m.nameInput.Blur()
		m.searchInput.Focus()
		return m, nil
	case "enter":
		name := m.nameInput.Value()
		if name != "" {
			return m, m.createTag(name)
		}
	}

	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

func (m *TagsModel) filterTags() {
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filteredTags = m.tags
		if m.cursor >= len(m.filteredTags) {
			m.cursor = 0
		}
		return
	}

	m.filteredTags = []models.Tag{}
	for _, tag := range m.tags {
		if strings.Contains(strings.ToLower(tag.Name), query) {
			m.filteredTags = append(m.filteredTags, tag)
		}
	}
	if m.cursor >= len(m.filteredTags) {
		m.cursor = 0
	}
}

func (m *TagsModel) updateLinksView() {
	if !m.viewportReady {
		return
	}

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	urlStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

	var content strings.Builder
	if len(m.links) == 0 {
		content.WriteString(dimStyle.Render("No links with this tag."))
	} else {
		for _, link := range m.links {
			title := link.Title.String
			if title == "" {
				title = link.Url
			}
			content.WriteString(fmt.Sprintf("• %s\n", title))
			content.WriteString(urlStyle.Render("  "+link.Url) + "\n")
			if link.Summary.Valid && link.Summary.String != "" {
				summary := link.Summary.String
				wrapped := wrapText(summary, m.detailViewport.Width-4)
				content.WriteString(dimStyle.Render("  "+wrapped) + "\n")
			}
			content.WriteString("\n")
		}
	}
	m.detailViewport.SetContent(content.String())
	m.detailViewport.GotoTop()
}

func (m TagsModel) View() string {
	switch m.mode {
	case tagsViewMode:
		return m.viewTags()
	case tagsCreateMode:
		return m.viewCreateTag()
	}
	return ""
}

func (m TagsModel) viewTags() string {
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

	// Left panel — tags list
	leftPanelStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(panelBorderColor(m.focus == panelFocusList))).
		Padding(1)

	var leftContent strings.Builder
	leftContent.WriteString(searchBox + "\n\n")

	if len(m.filteredTags) == 0 {
		if m.searchInput.Value() != "" {
			leftContent.WriteString(dimStyle.Render("No tags match your search.\n"))
		} else {
			leftContent.WriteString(dimStyle.Render("No tags yet. Press 'n' to create one!\n"))
		}
	} else {
		maxItems := m.height - 15
		if maxItems < 3 {
			maxItems = 3
		}
		startIdx := 0
		endIdx := len(m.filteredTags)
		if m.cursor >= maxItems {
			startIdx = m.cursor - maxItems + 1
		}
		if endIdx > startIdx+maxItems {
			endIdx = startIdx + maxItems
		}

		for i := startIdx; i < endIdx; i++ {
			tag := m.filteredTags[i]
			cursor := "  "
			if i == m.cursor {
				cursor = "• "
			}
			line := fmt.Sprintf("%s%s", cursor, tag.Name)
			if i == m.cursor {
				leftContent.WriteString(selectedStyle.Render(line) + "\n")
			} else {
				leftContent.WriteString(line + "\n")
			}
		}
		if len(m.filteredTags) > maxItems {
			leftContent.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  [%d/%d tags]", m.cursor+1, len(m.filteredTags))))
		}
	}

	leftPanel := leftPanelStyle.Render(leftContent.String())

	// Right panel — links for selected tag
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
	if len(m.filteredTags) > 0 && m.cursor < len(m.filteredTags) {
		tag := m.filteredTags[m.cursor]
		header := titleStyle.Render("Links for: "+tag.Name) + "\n\n"

		if m.viewportReady {
			rightContent = header + m.detailViewport.View()
			if m.detailViewport.TotalLineCount() > m.detailViewport.Height {
				scrollPercent := int(m.detailViewport.ScrollPercent() * 100)
				rightContent += "\n" + dimStyle.Render(fmt.Sprintf("[%d%% - PgUp/PgDn to scroll]", scrollPercent))
			}
		} else {
			rightContent = header + dimStyle.Render("Loading...")
		}
	} else {
		rightContent = dimStyle.Render("Select a tag to view its links.")
	}

	rightPanel := rightPanelStyle.Render(rightContent)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, "  ", rightPanel)

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	var helpMsg string
	switch m.focus {
	case panelFocusList:
		helpMsg = "Tab: focus right • ↑/↓/j/k: navigate • n: new tag • d: delete • Esc: back to search"
	case panelFocusDetail:
		helpMsg = "Tab: focus search • ↑/↓/j/k: scroll • PgUp/PgDn: scroll • Esc: back to search"
	default:
		helpMsg = "type to search • Tab: focus list • ↑/↓: navigate • n: new tag • d: delete • Esc: clear search"
	}
	helpText := "\n" + helpStyle.Render(helpMsg)

	return mainContent + helpText
}

func (m TagsModel) viewCreateTag() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("10")).
		Padding(1, 2).
		Width(50)

	var content strings.Builder
	content.WriteString(titleStyle.Render("Create New Tag") + "\n\n")
	content.WriteString(m.nameInput.View() + "\n\n")
	content.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Enter: create • Esc: cancel"))

	modal := modalStyle.Render(content.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func (m TagsModel) loadTags() tea.Cmd {
	return func() tea.Msg {
		tags, err := m.db.Queries.ListTags(m.ctx)
		if err != nil {
			return errMsg{err: err}
		}
		return tagsLoadedMsg{tags: tags}
	}
}

func (m TagsModel) loadTagLinks(tagID int64) tea.Cmd {
	return func() tea.Msg {
		links, err := m.db.Queries.GetLinksForTag(m.ctx, tagID)
		if err != nil {
			return errMsg{err: err}
		}
		return tagLinksLoadedMsg{links: links}
	}
}

func (m TagsModel) createTag(name string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.db.Queries.CreateTag(m.ctx, name)
		if err != nil {
			return errMsg{err: err}
		}
		return tagCreatedMsg{}
	}
}

func (m TagsModel) deleteTag(tagID int64) tea.Cmd {
	return func() tea.Msg {
		err := m.db.Queries.DeleteTag(m.ctx, tagID)
		if err != nil {
			return errMsg{err: err}
		}
		tags, err := m.db.Queries.ListTags(context.Background())
		if err != nil {
			return errMsg{err: err}
		}
		return tagsLoadedMsg{tags: tags}
	}
}

type tagsLoadedMsg struct {
	tags []models.Tag
}

type tagCreatedMsg struct{}

type tagLinksLoadedMsg struct {
	links []models.Link
}
