package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"mccwk.com/lk/internal/database"
	"mccwk.com/lk/internal/models"
	"mccwk.com/lk/internal/services"
)

type Tab int

const (
	TabLinks Tab = iota
	TabTasks
	TabReadLater
	TabTags
	TabCategories
)

type Model struct {
	currentTab Tab
	db         *database.Database
	ctx        context.Context
	fetcher    *services.Fetcher
	extractor  *services.Extractor
	summarizer *services.Summarizer
	width      int
	height     int

	// Tab models
	linksModel      LinksModel
	tasksModel      TasksModel
	readLaterModel  ReadLaterModel
	tagsModel       TagsModel
	categoriesModel CategoriesModel

	// Add link modal
	addLinkModel     AddLinkModel
	showAddLinkModal bool

	err error
}

func NewModel(db *database.Database, apiKey string) Model {
	var summarizer *services.Summarizer
	if apiKey != "" {
		summarizer = services.NewSummarizer(apiKey)
	}

	fetcher := services.NewFetcher()
	extractor := services.NewExtractor()

	linksModel := NewLinksModel(db)
	linksModel.SetServices(fetcher, extractor, summarizer)

	return Model{
		currentTab:      TabLinks,
		db:              db,
		ctx:             context.Background(),
		fetcher:         fetcher,
		extractor:       extractor,
		summarizer:      summarizer,
		linksModel:      linksModel,
		tagsModel:       NewTagsModel(db),
		categoriesModel: NewCategoriesModel(db),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.linksModel.Init(),
		m.tagsModel.Init(),
		m.categoriesModel.Init(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If add link modal is showing, delegate to it first
	if m.showAddLinkModal {
		return m.updateAddLinkModal(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "ctrl+a":
			// Show add link modal
			m.showAddLinkModal = true
			m.addLinkModel = NewAddLinkModel()
			m.addLinkModel.width = m.width
			m.addLinkModel.height = m.height
			return m, func() tea.Msg {
				return tea.WindowSizeMsg{Width: m.width, Height: m.height}
			}

		case "ctrl+n":
			// Next tab
			m.currentTab = (m.currentTab + 1) % 5
			return m, m.loadTabData()

		case "ctrl+p":
			// Previous tab
			m.currentTab = (m.currentTab - 1 + 5) % 5
			return m, m.loadTabData()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Update all sub-models with dimensions
		m.tasksModel.width = m.width
		m.tasksModel.height = m.height
		m.linksModel.width = m.width
		m.linksModel.height = m.height
		m.readLaterModel.width = m.width
		m.readLaterModel.height = m.height
		m.tagsModel.width = m.width
		m.tagsModel.height = m.height
		m.categoriesModel.width = m.width
		m.categoriesModel.height = m.height
		// Fall through to pass message to current tab's model

	case tasksLoadedMsg:
		m.tasksModel = NewTasksModel(msg.tasks, m.db)
		m.tasksModel.SetServices(m.fetcher, m.extractor, m.summarizer)
		// Preserve width and height
		m.tasksModel.width = m.width
		m.tasksModel.height = m.height
		return m, nil

	case linksLoadedMsg:
		if m.currentTab == TabReadLater {
			m.readLaterModel = NewReadLaterModel(msg.links)
		}
		// Don't return here, let it fall through to LinksModel too
		// so it can handle its own linksLoadedMsg

	case errMsg:
		m.err = msg.err
		return m, nil
	}

	// Delegate to current tab's model
	var cmd tea.Cmd
	switch m.currentTab {
	case TabLinks:
		m.linksModel, cmd = m.linksModel.Update(msg)
	case TabTasks:
		m.tasksModel, cmd = m.tasksModel.Update(msg)
	case TabReadLater:
		m.readLaterModel, cmd = m.readLaterModel.Update(msg)
	case TabTags:
		m.tagsModel, cmd = m.tagsModel.Update(msg)
	case TabCategories:
		m.categoriesModel, cmd = m.categoriesModel.Update(msg)
	}

	return m, cmd
}

func (m Model) updateAddLinkModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	var extraCmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" {
			m.showAddLinkModal = false
			return m, nil
		}
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case linkProcessCompleteMsg:
		// Keep modal open to show success state, but refresh data in background
		extraCmd = m.loadTabData()

	case linkProcessErrorMsg:
		// Keep modal open to show error
	}

	var cmd tea.Cmd
	m.addLinkModel, cmd = m.addLinkModel.Update(msg, m.db, m.fetcher, m.extractor, m.summarizer, m.ctx)
	if extraCmd != nil {
		return m, tea.Batch(cmd, extraCmd)
	}
	return m, cmd
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// If add link modal is showing, render it on top
	if m.showAddLinkModal {
		return m.renderAddLinkModal()
	}

	// Render tabs and current content
	return m.renderTabs() + "\n" + m.renderCurrentTab()
}

func (m Model) renderTabs() string {
	tabs := []string{"Links", "Tasks", "Read Later", "Tags", "Categories"}

	var renderedTabs []string
	for i, tab := range tabs {
		tabStyle := lipgloss.NewStyle().
			Padding(0, 3)

		if Tab(i) == m.currentTab {
			// Active tab
			tabStyle = tabStyle.
				Bold(true).
				Foreground(lipgloss.Color("10")).
				Background(lipgloss.Color("236")).
				Border(lipgloss.RoundedBorder(), true, true, false, false).
				BorderForeground(lipgloss.Color("10"))
		} else {
			// Inactive tab
			tabStyle = tabStyle.
				Foreground(lipgloss.Color("243")).
				Border(lipgloss.RoundedBorder(), true, true, false, false).
				BorderForeground(lipgloss.Color("237"))
		}

		renderedTabs = append(renderedTabs, tabStyle.Render(tab))
	}

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		Padding(0, 2)

	title := titleStyle.Render("LK · Link Manager")

	// Join tabs
	tabBar := lipgloss.JoinHorizontal(lipgloss.Bottom, renderedTabs...)

	// Combine title and tabs
	header := lipgloss.JoinVertical(lipgloss.Left, title, tabBar)

	// Add separator line
	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("237")).
		Width(m.width).
		Render(lipgloss.Border{}.Top)

	return header + "\n" + separator
}

func (m Model) renderCurrentTab() string {
	// Calculate available height for tab content
	// Account for: title(1) + tabs(3) + separator(1) + footer(2) = 7
	availableHeight := m.height - 7

	var content string
	switch m.currentTab {
	case TabLinks:
		content = m.linksModel.View()
	case TabTasks:
		content = m.tasksModel.View()
	case TabReadLater:
		content = m.readLaterModel.View()
	case TabTags:
		content = m.tagsModel.View()
	case TabCategories:
		content = m.categoriesModel.View()
	}

	// Show error if any
	if m.err != nil {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true)
		content = errorStyle.Render("Error: "+m.err.Error()) + "\n\n" + content
	}

	// Add footer with help text
	footer := "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Ctrl+A: add link • Ctrl+N/P: prev/next tab • Ctrl+C: quit")

	// Ensure content doesn't exceed available height
	contentStyle := lipgloss.NewStyle().
		MaxHeight(availableHeight)

	return contentStyle.Render(content) + footer
}

func (m Model) renderAddLinkModal() string {
	// Create a compact modal that fits on screen
	modalWidth := m.width - 10
	if modalWidth > 100 {
		modalWidth = 100
	}
	if modalWidth < 60 {
		modalWidth = 60
	}

	modalHeight := m.height - 10
	if modalHeight < 20 {
		modalHeight = 20
	}

	// Get modal content from addLinkModel
	modalContent := m.addLinkModel.ViewModal(modalWidth, modalHeight)

	// Style the modal
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("10")).
		Padding(1).
		Width(modalWidth).
		MaxHeight(modalHeight)

	modal := modalStyle.Render(modalContent)

	// Position modal in center of screen
	verticalPadding := (m.height - lipgloss.Height(modal)) / 2
	if verticalPadding < 0 {
		verticalPadding = 0
	}

	horizontalPadding := (m.width - modalWidth) / 2
	if horizontalPadding < 0 {
		horizontalPadding = 0
	}

	// Add padding to center the modal
	centeredModal := lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		modal,
	)

	return centeredModal
}

func (m Model) loadTabData() tea.Cmd {
	switch m.currentTab {
	case TabLinks:
		return m.linksModel.loadLinks()
	case TabTasks:
		return m.loadTasks()
	case TabReadLater:
		return m.loadReadLater()
	case TabTags:
		return m.tagsModel.loadTags()
	case TabCategories:
		return m.categoriesModel.loadCategories()
	}
	return nil
}

// Messages
type modeChangeMsg struct {
	mode int
}

type linksLoadedMsg struct {
	links []models.Link
}

type errMsg struct {
	err error
}

// Helper commands to load data
func (m Model) loadTasks() tea.Cmd {
	return func() tea.Msg {
		tasks, err := m.db.Queries.ListTasks(context.Background())
		if err != nil {
			return errMsg{err: err}
		}
		return tasksLoadedMsg{tasks: tasks}
	}
}

func (m Model) loadReadLater() tea.Cmd {
	return func() tea.Msg {
		links, err := m.db.Queries.ListLinksByStatus(context.Background(), models.ListLinksByStatusParams{
			Status: "read_later",
			Limit:  100,
			Offset: 0,
		})
		if err != nil {
			return errMsg{err: err}
		}
		return linksLoadedMsg{links: links}
	}
}
