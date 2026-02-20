package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.dalton.dog/bubbleup"

	"mccwk.com/lm/internal/database"
	"mccwk.com/lm/internal/logging"
	"mccwk.com/lm/internal/models"
	"mccwk.com/lm/internal/services"
)

type Tab int

const (
	TabLinks Tab = iota
	TabTasks
	TabActivities
	TabReadLater
	TabTags
	TabCategories
)

// logPanelHeight is the total screen rows reserved for the log panel (including
// its border and title) when it is visible.
const logPanelHeight = 12

// notifyMsg is sent by sub-models to surface a user-visible notification.
type notifyMsg struct {
	level   string // "info" | "success" | "warning" | "error"
	message string
}

// notifyCmd returns a tea.Cmd that fires a notifyMsg.
func notifyCmd(level, message string) tea.Cmd {
	return func() tea.Msg { return notifyMsg{level: level, message: message} }
}

func notifyKey(level string) string {
	switch level {
	case "warning":
		return bubbleup.WarnKey
	case "error":
		return bubbleup.ErrorKey
	default: // "info", "success"
		return bubbleup.InfoKey
	}
}

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
	activitiesModel ActivitiesModel
	readLaterModel  ReadLaterModel
	tagsModel       TagsModel
	categoriesModel CategoriesModel

	// Add link modal
	addLinkModel     AddLinkModel
	showAddLinkModal bool

	// LLM cost tracking
	totalLLMCost float64

	// Notifications overlay
	alert bubbleup.AlertModel

	// Log panel
	logSink        *logging.MemorySink
	logViewport    viewport.Model
	logReady       bool
	showLogPanel   bool
}

func NewModel(db *database.Database, apiKey string, logSink *logging.MemorySink) Model {
	var summarizer *services.Summarizer
	if apiKey != "" {
		summarizer = services.NewSummarizer(apiKey)
	}

	fetcher := services.NewFetcher()
	extractor := services.NewExtractor()

	linksModel := NewLinksModel(db)
	linksModel.SetServices(fetcher, extractor, summarizer)
	activitiesModel := NewActivitiesModel(db)
	activitiesModel.SetServices(fetcher, extractor, summarizer)

	alert := bubbleup.NewAlertModel(70, false, 4*time.Second).
		WithMinWidth(20).
		WithPosition(bubbleup.TopRightPosition)

	return Model{
		currentTab:      TabLinks,
		db:              db,
		ctx:             context.Background(),
		fetcher:         fetcher,
		extractor:       extractor,
		summarizer:      summarizer,
		linksModel:      linksModel,
		activitiesModel: activitiesModel,
		readLaterModel:  NewReadLaterModel(db),
		tagsModel:       NewTagsModel(db),
		categoriesModel: NewCategoriesModel(db),
		alert:           alert,
		logSink:         logSink,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.linksModel.Init(),
		m.readLaterModel.Init(),
		m.tagsModel.Init(),
		m.categoriesModel.Init(),
		m.alert.Init(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Always tick the alert model so its dismiss timer works.
	outAlert, alertCmd := m.alert.Update(msg)
	m.alert = outAlert.(bubbleup.AlertModel)
	if alertCmd != nil {
		cmds = append(cmds, alertCmd)
	}

	// Sub-models surface notifications via notifyMsg.
	if n, ok := msg.(notifyMsg); ok {
		cmds = append(cmds, m.alert.NewAlertCmd(notifyKey(n.level), n.message))
		return m, tea.Batch(cmds...)
	}

	// Surface DB / async errors as notifications.
	if e, ok := msg.(errMsg); ok {
		cmds = append(cmds, m.alert.NewAlertCmd(bubbleup.ErrorKey, e.err.Error()))
		return m, tea.Batch(cmds...)
	}

	// If add link modal is showing, delegate to it first.
	if m.showAddLinkModal {
		var cmd tea.Cmd
		m, cmd = m.updateAddLinkModal(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	// Sub-models can fire this to request the global add-link modal.
	if _, ok := msg.(openAddLinkModalMsg); ok {
		m.showAddLinkModal = true
		m.addLinkModel = NewAddLinkModel()
		m.addLinkModel.width = m.width
		m.addLinkModel.height = m.height
		m.addLinkModel.inModal = true
		cmds = append(cmds, func() tea.Msg {
			return tea.WindowSizeMsg{Width: m.width, Height: m.height}
		})
		return m, tea.Batch(cmds...)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "ctrl+l":
			m.showLogPanel = !m.showLogPanel
			if m.showLogPanel {
				m.refreshLogViewport()
			}
			// Re-send window size so tab models recalculate heights.
			cmds = append(cmds, func() tea.Msg {
				return tea.WindowSizeMsg{Width: m.width, Height: m.height}
			})
			return m, tea.Batch(cmds...)

		case "ctrl+n":
			m.currentTab = (m.currentTab + 1) % 6
			cmds = append(cmds, m.loadTabData())
			return m, tea.Batch(cmds...)

		case "ctrl+p":
			m.currentTab = (m.currentTab - 1 + 6) % 6
			cmds = append(cmds, m.loadTabData())
			return m, tea.Batch(cmds...)
		}

		// Forward PgUp/PgDn to the log viewport when the panel is visible.
		if m.showLogPanel && m.logReady {
			switch msg.String() {
			case "pgup", "pgdown":
				var vpCmd tea.Cmd
				m.logViewport, vpCmd = m.logViewport.Update(msg)
				if vpCmd != nil {
					cmds = append(cmds, vpCmd)
				}
				return m, tea.Batch(cmds...)
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.tasksModel.width = m.width
		m.tasksModel.height = m.height
		m.activitiesModel.width = m.width
		m.activitiesModel.height = m.height

		// Initialise / resize the log viewport.
		logInnerH := logPanelHeight - 4 // subtract border rows + title
		if logInnerH < 2 {
			logInnerH = 2
		}
		if !m.logReady {
			m.logViewport = viewport.New(m.width-4, logInnerH)
			m.logReady = true
		} else {
			m.logViewport.Width = m.width - 4
			m.logViewport.Height = logInnerH
		}
		if m.showLogPanel {
			m.refreshLogViewport()
		}

		// Forward WindowSizeMsg to all tab models so their viewports are
		// initialized regardless of which tab is currently active.
		var wCmd tea.Cmd
		m.linksModel, wCmd = m.linksModel.Update(msg)
		if wCmd != nil {
			cmds = append(cmds, wCmd)
		}
		m.readLaterModel, wCmd = m.readLaterModel.Update(msg)
		if wCmd != nil {
			cmds = append(cmds, wCmd)
		}
		m.tagsModel, wCmd = m.tagsModel.Update(msg)
		if wCmd != nil {
			cmds = append(cmds, wCmd)
		}
		m.categoriesModel, wCmd = m.categoriesModel.Update(msg)
		if wCmd != nil {
			cmds = append(cmds, wCmd)
		}

	case tasksLoadedMsg:
		m.tasksModel = NewTasksModel(msg.tasks, m.db)
		m.tasksModel.SetServices(m.fetcher, m.extractor, m.summarizer)
		m.tasksModel.width = m.width
		m.tasksModel.height = m.height
		return m, tea.Batch(cmds...)

	}

	// Delegate to current tab's model.
	var tabCmd tea.Cmd
	switch m.currentTab {
	case TabLinks:
		m.linksModel, tabCmd = m.linksModel.Update(msg)
	case TabTasks:
		m.tasksModel, tabCmd = m.tasksModel.Update(msg)
	case TabActivities:
		m.activitiesModel, tabCmd = m.activitiesModel.Update(msg)
	case TabReadLater:
		m.readLaterModel, tabCmd = m.readLaterModel.Update(msg)
	case TabTags:
		m.tagsModel, tabCmd = m.tagsModel.Update(msg)
	case TabCategories:
		m.categoriesModel, tabCmd = m.categoriesModel.Update(msg)
	}
	if tabCmd != nil {
		cmds = append(cmds, tabCmd)
	}

	return m, tea.Batch(cmds...)
}

// refreshLogViewport updates the log viewport content from the in-memory sink
// and scrolls to the most-recent entry.
func (m *Model) refreshLogViewport() {
	if !m.logReady || m.logSink == nil {
		return
	}
	content := m.logSink.Render(m.logViewport.Width)
	m.logViewport.SetContent(content)
	m.logViewport.GotoBottom()
}

func (m Model) updateAddLinkModal(msg tea.Msg) (Model, tea.Cmd) {
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

	case addLinkCloseRequestedMsg:
		m.showAddLinkModal = false
		return m, m.loadTabData()

	case linkProcessCompleteMsg:
		extraCmd = m.loadTabData()
		if msg.llmCost > 0 {
			m.totalLLMCost += msg.llmCost
		}

	case linkProcessErrorMsg:
		// modal stays open to show retry option
	}

	var cmd tea.Cmd
	m.addLinkModel, cmd = m.addLinkModel.Update(msg, m.db, m.fetcher, m.extractor, m.summarizer, m.ctx)
	return m, tea.Batch(cmd, extraCmd)
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var content string
	if m.showAddLinkModal {
		content = m.renderAddLinkModal()
	} else {
		tabContent := m.renderTabs() + "\n" + m.renderCurrentTab()
		if m.showLogPanel {
			content = tabContent + "\n" + m.renderLogPanel()
		} else {
			content = tabContent
		}
	}

	return m.alert.Render(content)
}

func (m Model) renderTabs() string {
	tabs := []string{"Links", "Tasks", "Activities", "Read Later", "Tags", "Categories"}

	var renderedTabs []string
	for i, tab := range tabs {
		tabStyle := lipgloss.NewStyle().
			Padding(0, 3)

		if Tab(i) == m.currentTab {
			tabStyle = tabStyle.
				Bold(true).
				Foreground(lipgloss.Color("10")).
				Background(lipgloss.Color("236")).
				Border(lipgloss.RoundedBorder(), true, true, false, false).
				BorderForeground(lipgloss.Color("10"))
		} else {
			tabStyle = tabStyle.
				Foreground(lipgloss.Color("243")).
				Border(lipgloss.RoundedBorder(), true, true, false, false).
				BorderForeground(lipgloss.Color("237"))
		}

		renderedTabs = append(renderedTabs, tabStyle.Render(tab))
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		Padding(0, 2)

	title := titleStyle.Render("lm · Link Manager")
	tabBar := lipgloss.JoinHorizontal(lipgloss.Bottom, renderedTabs...)
	header := lipgloss.JoinVertical(lipgloss.Left, title, tabBar)

	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color("237")).
		Width(m.width).
		Render(lipgloss.Border{}.Top)

	return header + "\n" + separator
}

func (m Model) renderCurrentTab() string {
	// Reduce available height when the log panel is visible.
	extra := 0
	if m.showLogPanel {
		extra = logPanelHeight + 1 // +1 for the separator newline
	}
	availableHeight := m.height - 7 - extra

	var content string
	switch m.currentTab {
	case TabLinks:
		content = m.linksModel.View()
	case TabTasks:
		content = m.tasksModel.View()
	case TabActivities:
		content = m.activitiesModel.View()
	case TabReadLater:
		content = m.readLaterModel.View()
	case TabTags:
		content = m.tagsModel.View()
	case TabCategories:
		content = m.categoriesModel.View()
	}

	footerText := "Ctrl+A: add link • Ctrl+N/P: prev/next tab • Ctrl+L: logs • Ctrl+C: quit"
	if m.totalLLMCost > 0 {
		costStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		footerText += costStyle.Render(fmt.Sprintf(" • LLM: $%.5f", m.totalLLMCost))
	}
	footer := "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(footerText)

	contentStyle := lipgloss.NewStyle().
		MaxHeight(availableHeight)

	return contentStyle.Render(content) + footer
}

func (m Model) renderLogPanel() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6"))

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	title := titleStyle.Render("Logs") +
		hintStyle.Render("  PgUp/PgDn: scroll • Ctrl+L: close")

	var body string
	if m.logReady {
		body = title + "\n" + m.logViewport.View()
	} else {
		body = title + "\n" + hintStyle.Render("(no log sink configured)")
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("237")).
		Padding(0, 1).
		Width(m.width - 4)

	return panelStyle.Render(body)
}

func (m Model) renderAddLinkModal() string {
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

	modalContent := m.addLinkModel.ViewModal(modalWidth, modalHeight)

	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("10")).
		Padding(1).
		Width(modalWidth).
		MaxHeight(modalHeight)

	modal := modalStyle.Render(modalContent)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		modal,
	)
}

func (m Model) loadTabData() tea.Cmd {
	switch m.currentTab {
	case TabLinks:
		return m.linksModel.loadLinks()
	case TabTasks:
		return m.loadTasks()
	case TabActivities:
		return m.activitiesModel.loadActivities()
	case TabReadLater:
		return m.readLaterModel.loadLinks()
	case TabTags:
		return m.tagsModel.loadTags()
	case TabCategories:
		return m.categoriesModel.loadCategories()
	}
	return nil
}

// Messages
// openAddLinkModalMsg is fired by any tab to ask the root model to open the
// global add-link modal.
type openAddLinkModalMsg struct{}

type linksLoadedMsg struct {
	links []models.Link
}

type errMsg struct {
	err error
}

func (m Model) loadTasks() tea.Cmd {
	return func() tea.Msg {
		tasks, err := m.db.Queries.ListTasks(context.Background())
		if err != nil {
			return errMsg{err: err}
		}
		return tasksLoadedMsg{tasks: tasks}
	}
}
