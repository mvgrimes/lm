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
)

type categoriesMode int

const (
	categoriesViewMode categoriesMode = iota
	categoriesCreateMode
)

type CategoriesModel struct {
	categories         []models.Category
	filteredCategories []models.Category
	cursor             int
	db                 *database.Database
	ctx                context.Context
	mode               categoriesMode
	links              []models.Link

	// Search and focus
	searchInput textinput.Model
	focus       panelFocus

	// Detail viewport for links panel
	detailViewport viewport.Model
	viewportReady  bool

	// Create mode
	nameInput   textinput.Model
	descInput   textinput.Model
	createFocus int

	width  int
	height int
}

func NewCategoriesModel(db *database.Database) CategoriesModel {
	searchInput := textinput.New()
	searchInput.Placeholder = "Search categories..."
	searchInput.Width = 50
	searchInput.Prompt = "🔍 "
	searchInput.Focus()

	nameInput := textinput.New()
	nameInput.Placeholder = "Category name..."
	nameInput.Width = 50
	nameInput.Prompt = "Name: "

	descInput := textinput.New()
	descInput.Placeholder = "Description (optional)..."
	descInput.Width = 50
	descInput.Prompt = "Description: "

	return CategoriesModel{
		db:          db,
		ctx:         context.Background(),
		mode:        categoriesViewMode,
		searchInput: searchInput,
		nameInput:   nameInput,
		descInput:   descInput,
		focus:       panelFocusSearch,
	}
}

func (m CategoriesModel) Init() tea.Cmd {
	return m.loadCategories()
}

func (m CategoriesModel) Update(msg tea.Msg) (CategoriesModel, tea.Cmd) {
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
			m.updateLinksView() // populate if links were loaded before viewport was ready
		} else {
			m.detailViewport.Width = rightWidth - 4
			m.detailViewport.Height = detailHeight
		}
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case categoriesViewMode:
			return m.handleViewMode(msg)
		case categoriesCreateMode:
			return m.handleCreateMode(msg)
		}

	case categoriesLoadedMsg:
		m.categories = msg.categories
		m.filterCategories()
		if len(m.filteredCategories) > 0 {
			return m, m.loadCategoryLinks(m.filteredCategories[m.cursor].ID)
		}
		return m, nil

	case categoryCreatedMsg:
		m.mode = categoriesViewMode
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		m.nameInput.Blur()
		m.descInput.Blur()
		m.searchInput.Focus()
		return m, tea.Batch(m.loadCategories(), notifyCmd("info", "Category created!"))

	case categoryLinksLoadedMsg:
		m.links = msg.links
		m.updateLinksView()
		return m, nil
	}

	return m, nil
}

func (m CategoriesModel) handleViewMode(msg tea.KeyMsg) (CategoriesModel, tea.Cmd) {
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
				if len(m.filteredCategories) > 0 {
					return m, m.loadCategoryLinks(m.filteredCategories[m.cursor].ID)
				}
			}
		case "down", "j":
			if m.cursor < len(m.filteredCategories)-1 {
				m.cursor++
				if len(m.filteredCategories) > 0 {
					return m, m.loadCategoryLinks(m.filteredCategories[m.cursor].ID)
				}
			}
		case "n":
			m.mode = categoriesCreateMode
			m.createFocus = 0
			m.focus = panelFocusSearch
			m.searchInput.Blur()
			m.nameInput.Focus()
			m.descInput.Blur()
		case "d":
			if len(m.filteredCategories) > 0 && m.cursor < len(m.filteredCategories) {
				return m, m.deleteCategory(m.filteredCategories[m.cursor].ID)
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
				if len(m.filteredCategories) > 0 {
					return m, m.loadCategoryLinks(m.filteredCategories[m.cursor].ID)
				}
			}
			return m, nil
		case "down":
			if m.cursor < len(m.filteredCategories)-1 {
				m.cursor++
				if len(m.filteredCategories) > 0 {
					return m, m.loadCategoryLinks(m.filteredCategories[m.cursor].ID)
				}
			}
			return m, nil
		case "n":
			m.mode = categoriesCreateMode
			m.createFocus = 0
			m.searchInput.Blur()
			m.nameInput.Focus()
			m.descInput.Blur()
			return m, nil
		case "d":
			if len(m.filteredCategories) > 0 && m.cursor < len(m.filteredCategories) {
				return m, m.deleteCategory(m.filteredCategories[m.cursor].ID)
			}
			return m, nil
		case "esc":
			m.searchInput.SetValue("")
			m.filterCategories()
			return m, nil
		}
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		prevLen := len(m.filteredCategories)
		m.filterCategories()
		if len(m.filteredCategories) > 0 && (len(m.filteredCategories) != prevLen || m.cursor == 0) {
			if m.cursor >= len(m.filteredCategories) {
				m.cursor = 0
			}
			return m, tea.Batch(cmd, m.loadCategoryLinks(m.filteredCategories[m.cursor].ID))
		}
		return m, cmd
	}
}

func (m CategoriesModel) handleCreateMode(msg tea.KeyMsg) (CategoriesModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg.String() {
	case "esc":
		m.mode = categoriesViewMode
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		m.nameInput.Blur()
		m.descInput.Blur()
		m.searchInput.Focus()
		return m, nil
	case "tab", "shift+tab":
		m.createFocus = (m.createFocus + 1) % 2
		if m.createFocus == 0 {
			m.nameInput.Focus()
			m.descInput.Blur()
		} else {
			m.nameInput.Blur()
			m.descInput.Focus()
		}
		return m, nil
	case "enter":
		name := m.nameInput.Value()
		if name != "" {
			return m, m.createCategory(name, m.descInput.Value())
		}
	}

	if m.createFocus == 0 {
		m.nameInput, cmd = m.nameInput.Update(msg)
	} else {
		m.descInput, cmd = m.descInput.Update(msg)
	}
	return m, cmd
}

func (m *CategoriesModel) filterCategories() {
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filteredCategories = m.categories
		if m.cursor >= len(m.filteredCategories) {
			m.cursor = 0
		}
		return
	}

	m.filteredCategories = []models.Category{}
	for _, cat := range m.categories {
		if strings.Contains(strings.ToLower(cat.Name), query) ||
			(cat.Description.Valid && strings.Contains(strings.ToLower(cat.Description.String), query)) {
			m.filteredCategories = append(m.filteredCategories, cat)
		}
	}
	if m.cursor >= len(m.filteredCategories) {
		m.cursor = 0
	}
}

func (m *CategoriesModel) updateLinksView() {
	if !m.viewportReady {
		return
	}

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	urlStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

	var content strings.Builder
	if len(m.links) == 0 {
		content.WriteString(dimStyle.Render("No links in this category."))
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

func (m CategoriesModel) View() string {
	switch m.mode {
	case categoriesViewMode:
		return m.viewCategories()
	case categoriesCreateMode:
		return m.viewCreateCategory()
	}
	return ""
}

func (m CategoriesModel) viewCategories() string {
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

	// Left panel — categories list
	leftPanelStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(panelBorderColor(m.focus == panelFocusList))).
		Padding(1)

	var leftContent strings.Builder
	leftContent.WriteString(searchBox + "\n\n")

	if len(m.filteredCategories) == 0 {
		if m.searchInput.Value() != "" {
			leftContent.WriteString(dimStyle.Render("No categories match your search.\n"))
		} else {
			leftContent.WriteString(dimStyle.Render("No categories yet. Press 'n' to create one!\n"))
		}
	} else {
		maxItems := m.height - 15
		if maxItems < 3 {
			maxItems = 3
		}
		startIdx := 0
		endIdx := len(m.filteredCategories)
		if m.cursor >= maxItems {
			startIdx = m.cursor - maxItems + 1
		}
		if endIdx > startIdx+maxItems {
			endIdx = startIdx + maxItems
		}

		for i := startIdx; i < endIdx; i++ {
			cat := m.filteredCategories[i]
			cursor := "  "
			if i == m.cursor {
				cursor = "• "
			}
			line := fmt.Sprintf("%s%s", cursor, cat.Name)
			if i == m.cursor {
				leftContent.WriteString(selectedStyle.Render(line) + "\n")
				if cat.Description.Valid && cat.Description.String != "" {
					leftContent.WriteString(dimStyle.Render("  "+cat.Description.String) + "\n")
				}
			} else {
				leftContent.WriteString(line + "\n")
			}
		}
		if len(m.filteredCategories) > maxItems {
			leftContent.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  [%d/%d categories]", m.cursor+1, len(m.filteredCategories))))
		}
	}

	leftPanel := leftPanelStyle.Render(leftContent.String())

	// Right panel — links for selected category
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
	if len(m.filteredCategories) > 0 && m.cursor < len(m.filteredCategories) {
		cat := m.filteredCategories[m.cursor]
		header := titleStyle.Render("Links in: "+cat.Name) + "\n\n"

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
		rightContent = dimStyle.Render("Select a category to view its links.")
	}

	rightPanel := rightPanelStyle.Render(rightContent)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, "  ", rightPanel)

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	var helpMsg string
	switch m.focus {
	case panelFocusList:
		helpMsg = "Tab: focus right • ↑/↓/j/k: navigate • n: new category • d: delete • Esc: back to search"
	case panelFocusDetail:
		helpMsg = "Tab: focus search • ↑/↓/j/k: scroll • PgUp/PgDn: scroll • Esc: back to search"
	default:
		helpMsg = "type to search • Tab: focus list • ↑/↓: navigate • n: new category • d: delete • Esc: clear search"
	}
	helpText := "\n" + helpStyle.Render(helpMsg)

	return mainContent + helpText
}

func (m CategoriesModel) viewCreateCategory() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("10")).
		Padding(1, 2).
		Width(56)

	var content strings.Builder
	content.WriteString(titleStyle.Render("Create New Category") + "\n\n")
	content.WriteString(m.nameInput.View() + "\n\n")
	content.WriteString(m.descInput.View() + "\n\n")
	content.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Tab: switch fields • Enter: create • Esc: cancel"))

	modal := modalStyle.Render(content.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func (m CategoriesModel) loadCategories() tea.Cmd {
	return func() tea.Msg {
		categories, err := m.db.Queries.ListCategories(m.ctx)
		if err != nil {
			return errMsg{err: err}
		}
		return categoriesLoadedMsg{categories: categories}
	}
}

func (m CategoriesModel) loadCategoryLinks(categoryID int64) tea.Cmd {
	return func() tea.Msg {
		links, err := m.db.Queries.GetLinksForCategory(m.ctx, categoryID)
		if err != nil {
			return errMsg{err: err}
		}
		return categoryLinksLoadedMsg{links: links}
	}
}

func (m CategoriesModel) createCategory(name, description string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.db.Queries.CreateCategory(m.ctx, models.CreateCategoryParams{
			Name:        name,
			Description: sql.NullString{String: description, Valid: description != ""},
		})
		if err != nil {
			return errMsg{err: err}
		}
		return categoryCreatedMsg{}
	}
}

func (m CategoriesModel) deleteCategory(categoryID int64) tea.Cmd {
	return func() tea.Msg {
		err := m.db.Queries.DeleteCategory(m.ctx, categoryID)
		if err != nil {
			return errMsg{err: err}
		}
		categories, err := m.db.Queries.ListCategories(context.Background())
		if err != nil {
			return errMsg{err: err}
		}
		return categoriesLoadedMsg{categories: categories}
	}
}

type categoriesLoadedMsg struct {
	categories []models.Category
}

type categoryCreatedMsg struct{}

type categoryLinksLoadedMsg struct {
	links []models.Link
}
