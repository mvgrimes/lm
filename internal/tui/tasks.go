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
	"github.com/pkg/browser"

	"mccwk.com/lm/internal/database"
	"mccwk.com/lm/internal/models"
	"mccwk.com/lm/internal/services"
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

	// Detail view for links
	detailViewport viewport.Model
	viewportReady  bool

	width  int
	height int
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

		// Calculate responsive widths for split view
		leftWidth := int(float64(m.width) * 0.35)
		if leftWidth < 30 {
			leftWidth = 30
		}
		rightWidth := m.width - leftWidth - 8

		// Calculate height for detail viewport
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

	case addLinkCloseRequestedMsg:
		if m.mode == tasksAddLinkMode {
			m.mode = tasksViewMode
			return m, nil
		}

	case linkProcessCompleteMsg:
		if m.mode == tasksAddLinkMode && len(m.tasks) > 0 {
			taskID := m.tasks[m.cursor].ID
			linkID := msg.linkID
			m.mode = tasksViewMode
			return m, tea.Batch(
				m.linkToTask(taskID, linkID),
				m.loadTaskLinks(taskID),
				notifyCmd("info", "Link added to task!"),
			)
		}
		return m, nil

	case taskLinksLoadedMsg:
		m.links = msg.links
		m.showLinks = true
		return m, nil

	case tasksLoadedMsg:
		m.tasks = msg.tasks
		// Automatically load links for the first task
		if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			return m, m.loadTaskLinks(m.tasks[m.cursor].ID)
		}
		return m, nil

	case taskCreatedMsg:
		m.mode = tasksViewMode
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		return m, tea.Batch(m.loadTasks(), notifyCmd("info", "Task created!"))

	case linkAddedToTaskMsg:
		return m, nil
	}

	// Forward remaining messages to addLinkModel when in add link mode
	// (handles linkProcessErrorMsg, metadataSavedMsg, tea.WindowSizeMsg, etc.)
	if m.mode == tasksAddLinkMode {
		m.addLinkModel, cmd = m.addLinkModel.Update(msg, m.db, m.fetcher, m.extractor, m.summarizer, m.ctx)
		return m, cmd
	}

	return m, nil
}

func (m TasksModel) handleViewMode(msg tea.KeyMsg) (TasksModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			// Automatically load links for the newly selected task
			if len(m.tasks) > 0 {
				return m, m.loadTaskLinks(m.tasks[m.cursor].ID)
			}
		}
	case "down", "j":
		if m.cursor < len(m.tasks)-1 {
			m.cursor++
			// Automatically load links for the newly selected task
			if len(m.tasks) > 0 {
				return m, m.loadTaskLinks(m.tasks[m.cursor].ID)
			}
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
	case "a":
		// Add link to current task
		if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
			m.mode = tasksAddLinkMode
			taskID := m.tasks[m.cursor].ID
			m.addLinkModel = NewAddLinkModelForTask(&taskID)
			m.addLinkModel.inModal = true
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
	case "pgup", "pgdown":
		// Scroll detail viewport
		if m.viewportReady && m.showLinks {
			var cmd tea.Cmd
			m.detailViewport, cmd = m.detailViewport.Update(msg)
			return m, cmd
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
	if m.width == 0 {
		return "Loading..."
	}

	// Calculate responsive widths
	leftWidth := int(float64(m.width) * 0.35)
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := m.width - leftWidth - 8

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6"))

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	// Left panel - task list
	leftPanelStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1)

	var leftContent strings.Builder
	leftContent.WriteString(titleStyle.Render("Tasks") + "\n\n")

	if len(m.tasks) == 0 {
		leftContent.WriteString(dimStyle.Render("No tasks yet. Press 'n' to create one!\n"))
	} else {
		// Show tasks list with scrolling
		maxTasks := m.height - 15
		if maxTasks < 3 {
			maxTasks = 3
		}

		startIdx := 0
		endIdx := len(m.tasks)

		// Ensure cursor is visible
		if m.cursor >= maxTasks {
			startIdx = m.cursor - maxTasks + 1
		}
		if endIdx > startIdx+maxTasks {
			endIdx = startIdx + maxTasks
		}

		for i := startIdx; i < endIdx; i++ {
			task := m.tasks[i]
			cursor := "  "
			if i == m.cursor {
				cursor = "• "
			}

			status := "[ ]"
			if task.Completed {
				status = "[✓]"
			}

			taskName := task.Name
			// Truncate task name to fit
			if len(taskName) > leftWidth-10 {
				taskName = taskName[:leftWidth-13] + "..."
			}

			line := fmt.Sprintf("%s%s %s", cursor, status, taskName)

			if i == m.cursor {
				leftContent.WriteString(selectedStyle.Render(line) + "\n")
			} else {
				leftContent.WriteString(line + "\n")
			}

			// Show description for all tasks
			if task.Description.Valid && task.Description.String != "" {
				desc := task.Description.String
				if len(desc) > leftWidth-8 {
					desc = desc[:leftWidth-11] + "..."
				}
				leftContent.WriteString(dimStyle.Render("  "+desc) + "\n")
			}
		}

		// Show scroll indicator
		if len(m.tasks) > maxTasks {
			leftContent.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  [%d/%d tasks]", m.cursor+1, len(m.tasks))))
		}
	}

	leftPanel := leftPanelStyle.Render(leftContent.String())

	// Right panel - links for selected task
	rightPanelStyle := lipgloss.NewStyle().
		Width(rightWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(1)

	var rightContent string

	if len(m.tasks) > 0 && m.cursor < len(m.tasks) {
		task := m.tasks[m.cursor]

		var rightBuilder strings.Builder
		rightBuilder.WriteString(titleStyle.Render("Links for: "+task.Name) + "\n\n")

		if m.showLinks {
			if len(m.links) == 0 {
				rightBuilder.WriteString(dimStyle.Render("No links yet. Press 'a' to add a link."))
			} else {
				// Build content for viewport
				var detailContent strings.Builder
				for _, link := range m.links {
					title := link.Title.String
					if title == "" {
						title = link.Url
					}
					detailContent.WriteString(fmt.Sprintf("• %s\n", title))

					// Show URL in dim style
					detailContent.WriteString(dimStyle.Render("  "+link.Url) + "\n")

					// Show summary if available
					if link.Summary.Valid && link.Summary.String != "" {
						summary := link.Summary.String
						wrapped := wrapText(summary, rightWidth-6)
						detailContent.WriteString(dimStyle.Render("  "+wrapped) + "\n")
					}
					detailContent.WriteString("\n")
				}

				if m.viewportReady {
					m.detailViewport.SetContent(detailContent.String())
					rightBuilder.WriteString(m.detailViewport.View())

					// Show scroll indicator
					if m.detailViewport.TotalLineCount() > m.detailViewport.Height {
						scrollPercent := int(m.detailViewport.ScrollPercent() * 100)
						scrollInfo := dimStyle.Render(fmt.Sprintf("\n[%d%% - PgUp/PgDn to scroll]", scrollPercent))
						rightBuilder.WriteString(scrollInfo)
					}
				} else {
					rightBuilder.WriteString(detailContent.String())
				}

				rightBuilder.WriteString("\n\n" + dimStyle.Render("Press 'o' to open all links"))
			}
		} else {
			rightBuilder.WriteString(dimStyle.Render("Loading links..."))
		}

		rightContent = rightBuilder.String()
	} else {
		rightContent = dimStyle.Render("Select a task to view its links...")
	}

	rightPanel := rightPanelStyle.Render(rightContent)

	// Combine panels
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, "  ", rightPanel)

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpText := "\n" + helpStyle.Render("n: new task • a: add link • c: toggle complete • o: open links • arrows/j/k: navigate • PgUp/PgDn: scroll")

	return mainContent + helpText
}

func (m TasksModel) viewCreateTask() string {
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
	content.WriteString(titleStyle.Render("Create New Task") + "\n\n")
	content.WriteString(m.nameInput.View() + "\n\n")
	content.WriteString(m.descInput.View() + "\n\n")
	content.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Tab: switch fields • Enter: create • Esc: cancel"))

	modal := modalStyle.Render(content.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
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
