package tui

import (
	"context"
	"database/sql"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"mccwk.com/lk/internal/database"
	"mccwk.com/lk/internal/services"
)

type Mode int

const (
	ModeMenu Mode = iota
	ModeAddLink
	ModeTasks
	ModeReadLater
	ModeRemember
	ModeSearch
)

type Model struct {
	mode       Mode
	db         *database.Database
	ctx        context.Context
	fetcher    *services.Fetcher
	extractor  *services.Extractor
	summarizer *services.Summarizer
	width      int
	height     int

	// Sub-models for each mode
	menuModel      MenuModel
	addLinkModel   AddLinkModel
	tasksModel     TasksModel
	readLaterModel ReadLaterModel
	rememberModel  RememberModel
	searchModel    SearchModel

	err error
}

func NewModel(db *database.Database, apiKey string) Model {
	var summarizer *services.Summarizer
	if apiKey != "" {
		summarizer = services.NewSummarizer(apiKey)
	}

	return Model{
		mode:       ModeMenu,
		db:         db,
		ctx:        context.Background(),
		fetcher:    services.NewFetcher(),
		extractor:  services.NewExtractor(),
		summarizer: summarizer,
		menuModel:  NewMenuModel(),
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.mode == ModeMenu {
				return m, tea.Quit
			}
			// Go back to menu from any other mode
			m.mode = ModeMenu
			m.err = nil
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case modeChangeMsg:
		m.mode = msg.mode
		m.err = nil

		// Initialize sub-models when switching modes
		switch m.mode {
		case ModeAddLink:
			m.addLinkModel = NewAddLinkModel()
		case ModeTasks:
			return m, m.loadTasks()
		case ModeReadLater:
			return m, m.loadReadLater()
		case ModeRemember:
			return m, m.loadRemember()
		case ModeSearch:
			m.searchModel = NewSearchModel()
		}
		return m, nil

	case tasksLoadedMsg:
		m.tasksModel = NewTasksModel(msg.tasks, m.db)
		return m, nil

	case linksLoadedMsg:
		if m.mode == ModeReadLater {
			m.readLaterModel = NewReadLaterModel(msg.links)
		} else if m.mode == ModeRemember {
			m.rememberModel = NewRememberModel(msg.links, m.db)
		}
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil
	}

	// Delegate to sub-models based on current mode
	switch m.mode {
	case ModeMenu:
		newModel, cmd := m.menuModel.Update(msg)
		m.menuModel = newModel

		// Handle mode selection from menu
		if m.menuModel.selected >= 0 {
			newMode := Mode(m.menuModel.selected)
			m.menuModel.selected = -1
			return m.Update(modeChangeMsg{mode: newMode})
		}

		return m, cmd

	case ModeAddLink:
		newModel, cmd := m.addLinkModel.Update(msg, m.db, m.fetcher, m.extractor, m.summarizer, m.ctx)
		m.addLinkModel = newModel
		return m, cmd

	case ModeTasks:
		newModel, cmd := m.tasksModel.Update(msg)
		m.tasksModel = newModel
		return m, cmd

	case ModeReadLater:
		newModel, cmd := m.readLaterModel.Update(msg)
		m.readLaterModel = newModel
		return m, cmd

	case ModeRemember:
		newModel, cmd := m.rememberModel.Update(msg)
		m.rememberModel = newModel
		return m, cmd

	case ModeSearch:
		newModel, cmd := m.searchModel.Update(msg, m.db, m.ctx)
		m.searchModel = newModel
		return m, cmd
	}

	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var content string

	switch m.mode {
	case ModeMenu:
		content = m.menuModel.View()
	case ModeAddLink:
		content = m.addLinkModel.View()
	case ModeTasks:
		content = m.tasksModel.View()
	case ModeReadLater:
		content = m.readLaterModel.View()
	case ModeRemember:
		content = m.rememberModel.View()
	case ModeSearch:
		content = m.searchModel.View()
	}

	// Show error if any
	if m.err != nil {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true)
		content = errorStyle.Render("Error: "+m.err.Error()) + "\n\n" + content
	}

	// Add footer with help text
	footer := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Press 'q' to go back • Ctrl+C to quit")

	return lipgloss.JoinVertical(lipgloss.Left, content, "\n"+footer)
}

// Messages
type modeChangeMsg struct {
	mode Mode
}

type tasksLoadedMsg struct {
	tasks []sql.NullString // Placeholder - will use proper Task type
}

type linksLoadedMsg struct {
	links []sql.NullString // Placeholder - will use proper Link type
}

type errMsg struct {
	err error
}

// Helper commands to load data
func (m Model) loadTasks() tea.Cmd {
	return func() tea.Msg {
		// TODO: Load tasks from database
		return tasksLoadedMsg{tasks: []sql.NullString{}}
	}
}

func (m Model) loadReadLater() tea.Cmd {
	return func() tea.Msg {
		// TODO: Load read later links from database
		return linksLoadedMsg{links: []sql.NullString{}}
	}
}

func (m Model) loadRemember() tea.Cmd {
	return func() tea.Msg {
		// TODO: Load remember links from database
		return linksLoadedMsg{links: []sql.NullString{}}
	}
}
