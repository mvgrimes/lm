package tui

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pkg/browser"

	"mccwk.com/lk/internal/database"
	"mccwk.com/lk/internal/models"
	"mccwk.com/lk/internal/services"
)

type tasksMode int

const (
	tasksViewMode tasksMode = iota
	tasksCreateMode
	tasksAddLinkMode
)

type TasksModel struct {
	tasks      []models.Task
	cursor     int
	db         *database.Database
	ctx        context.Context
	fetcher    *services.Fetcher
	extractor  *services.Extractor
	summarizer *services.Summarizer
	links      []models.Link
	showLinks  bool

	// Mode management
	mode tasksMode

	// Create task inputs
	nameInput   textinput.Model
	descInput   textinput.Model
	createFocus int

	// Add link mode - use the AddLinkModel as a dialog
	addLinkModel AddLinkModel

	message string
	width   int
	height  int
}

func NewTasksModel(tasks []models.Task, db *database.Database) TasksModel {
	nameInput := textinput.New()
	nameInput.Placeholder = "Task name..."
	nameInput.Width = 50
	nameInput.Prompt = "Name: "

	descInput := textinput.New()
	descInput.Placeholder = "Description (optional)..."
	descInput.Width = 50
	descInput.Prompt = "Description: "

	return TasksModel{
		db:        db,
		tasks:     tasks,
		mode:      tasksViewMode,
		nameInput: nameInput,
		descInput: descInput,
		ctx:       context.Background(),
	}
}

func (m *TasksModel) SetServices(fetcher *services.Fetcher, extractor *services.Extractor, summarizer *services.Summarizer) {
	m.fetcher = fetcher
	m.extractor = extractor
	m.summarizer = summarizer
}

func (m TasksModel) Update(msg tea.Msg) (TasksModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Forward to add link model if active
		if m.mode == tasksAddLinkMode {
			m.addLinkModel, cmd = m.addLinkModel.Update(msg, m.db, m.fetcher, m.extractor, m.summarizer, m.ctx)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		// If in add link mode, delegate to addLinkModel
		if m.mode == tasksAddLinkMode {
			// Check for esc to exit add link mode
			if msg.String() == "esc" {
				m.mode = tasksViewMode
				return m, nil
			}
			m.addLinkModel, cmd = m.addLinkModel.Update(msg, m.db, m.fetcher, m.extractor, m.summarizer, m.ctx)
			return m, cmd
		}

		switch m.mode {
		case tasksViewMode:
			return m.handleViewMode(msg)
		case tasksCreateMode:
			return m.handleCreateMode(msg)
		}

	case linkProcessCompleteMsg:
		// Link was added successfully, link it to the current task
		if m.mode == tasksAddLinkMode && len(m.tasks) > 0 {
			taskID := m.tasks[m.cursor].ID
			linkID := msg.linkID
			m.mode = tasksViewMode
			m.message = "Link added to task!"
			return m, tea.Batch(
				m.linkToTask(taskID, linkID),
				m.loadTaskLinks(taskID),
			)
		}
		return m, nil

	case taskLinksLoadedMsg:
		m.links = msg.links
		m.showLinks = true
		return m, nil

	case tasksLoadedMsg:
		m.tasks = msg.tasks
		return m, nil

	case taskCreatedMsg:
		m.mode = tasksViewMode
		m.message = "Task created!"
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		return m, m.loadTasks()

	case linkAddedToTaskMsg:
		m.message = "Link associated with task!"
		return m, nil
	}

	return m, nil
}

func (m TasksModel) handleViewMode(msg tea.KeyMsg) (TasksModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.showLinks = false
		}
	case "down", "j":
		if m.cursor < len(m.tasks)-1 {
			m.cursor++
			m.showLinks = false
		}
	case "enter":
		if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			return m, m.loadTaskLinks(m.tasks[m.cursor].ID)
		}
	case "o":
		// Open all links for current task
		if m.showLinks && len(m.links) > 0 {
			return m, m.openLinks()
		}
	case "n":
		// Create new task
		m.mode = tasksCreateMode
		m.createFocus = 0
		m.nameInput.Focus()
		m.descInput.Blur()
		m.message = ""
	case "a":
		// Add link to current task
		if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			m.mode = tasksAddLinkMode
			m.message = ""
			// Create a new add link model with the task ID
			taskID := m.tasks[m.cursor].ID
			m.addLinkModel = NewAddLinkModelForTask(&taskID)
			// Send window size to initialize viewport
			return m, func() tea.Msg {
				return tea.WindowSizeMsg{Width: m.width, Height: m.height}
			}
		}
	case "c":
		// Toggle task completion
		if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			task := m.tasks[m.cursor]
			return m, m.toggleTaskCompletion(task.ID, !task.Completed)
		}
	}
	return m, nil
}

func (m TasksModel) handleCreateMode(msg tea.KeyMsg) (TasksModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = tasksViewMode
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
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
			return m, m.createTask(name, m.descInput.Value())
		}
	}

	// Update the focused input
	var cmd tea.Cmd
	if m.createFocus == 0 {
		m.nameInput, cmd = m.nameInput.Update(msg)
	} else {
		m.descInput, cmd = m.descInput.Update(msg)
	}
	return m, cmd
}

func (m TasksModel) View() string {
	switch m.mode {
	case tasksViewMode:
		return m.viewTasks()
	case tasksCreateMode:
		return m.viewCreateTask()
	case tasksAddLinkMode:
		// Use modal view for add link
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

		// Style the modal
		modalStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("10")).
			Padding(1).
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
	return ""
}

func (m TasksModel) viewTasks() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10"))

	s := titleStyle.Render("Tasks") + "\n\n"

	if m.message != "" {
		s += messageStyle.Render(m.message) + "\n\n"
	}

	if len(m.tasks) == 0 {
		s += "No tasks yet. Press 'n' to create one!\n"
	} else {
		for i, task := range m.tasks {
			cursor := "  "
			if i == m.cursor {
				cursor = "• "
			}

			status := "[ ]"
			if task.Completed {
				status = "[✓]"
			}

			line := fmt.Sprintf("%s%s %s", cursor, status, task.Name)

			if i == m.cursor {
				s += selectedStyle.Render(line) + "\n"
			} else {
				s += line + "\n"
			}
		}
	}

	if m.showLinks {
		s += "\n" + lipgloss.NewStyle().Bold(true).Render("Links:") + "\n"
		if len(m.links) == 0 {
			s += "  No links yet. Press 'a' to add a link.\n"
		} else {
			for _, link := range m.links {
				title := link.Title.String
				if title == "" {
					title = link.Url
				}
				s += fmt.Sprintf("  • %s\n", title)
			}
			s += "\nPress 'o' to open all links in browser"
		}
	}

	s += "\n\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("n: new task • a: add link • c: toggle complete • Enter: view links • arrows/j/k: navigate")

	return s
}

func (m TasksModel) viewCreateTask() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	s := titleStyle.Render("Create New Task") + "\n\n"
	s += m.nameInput.View() + "\n\n"
	s += m.descInput.View() + "\n\n"
	s += lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Tab: switch fields • Enter: create • Esc: cancel")

	return s
}

func (m TasksModel) loadTasks() tea.Cmd {
	return func() tea.Msg {
		tasks, err := m.db.Queries.ListTasks(context.Background())
		if err != nil {
			return errMsg{err: err}
		}
		return tasksLoadedMsg{tasks: tasks}
	}
}

func (m TasksModel) loadTaskLinks(taskID int64) tea.Cmd {
	return func() tea.Msg {
		links, err := m.db.Queries.GetLinksForTask(context.Background(), taskID)
		if err != nil {
			return errMsg{err: err}
		}
		return taskLinksLoadedMsg{links: links}
	}
}

func (m TasksModel) openLinks() tea.Cmd {
	return func() tea.Msg {
		for _, link := range m.links {
			_ = browser.OpenURL(link.Url)
		}
		return nil
	}
}

func (m TasksModel) createTask(name, description string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.db.Queries.CreateTask(context.Background(), models.CreateTaskParams{
			Name:        name,
			Description: sql.NullString{String: description, Valid: description != ""},
		})
		if err != nil {
			return errMsg{err: err}
		}
		return taskCreatedMsg{}
	}
}

func (m TasksModel) linkToTask(taskID, linkID int64) tea.Cmd {
	return func() tea.Msg {
		err := m.db.Queries.LinkTask(context.Background(), models.LinkTaskParams{
			LinkID: linkID,
			TaskID: taskID,
		})
		if err != nil {
			return errMsg{err: err}
		}
		return linkAddedToTaskMsg{}
	}
}

func (m TasksModel) toggleTaskCompletion(taskID int64, completed bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if completed {
			err = m.db.Queries.CompleteTask(context.Background(), taskID)
		} else {
			// We need to get the current task details to update it
			// For now, just reload tasks
		}
		if err != nil {
			return errMsg{err: err}
		}
		// Reload tasks
		tasks, err := m.db.Queries.ListTasks(context.Background())
		if err != nil {
			return errMsg{err: err}
		}
		return tasksLoadedMsg{tasks: tasks}
	}
}

type taskLinksLoadedMsg struct {
	links []models.Link
}

type tasksLoadedMsg struct {
	tasks []models.Task
}

type taskCreatedMsg struct{}

type linkAddedToTaskMsg struct{}
