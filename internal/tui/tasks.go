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
	tasks         []models.Task
	filteredTasks []models.Task
	cursor        int
	db            *database.Database
	ctx           context.Context
	fetcher       *services.Fetcher
	extractor     *services.Extractor
	summarizer    *services.Summarizer
	links         []models.Link
	showLinks     bool

	// Mode management
	mode tasksMode

	// Search and focus
	searchInput textinput.Model
	focus       panelFocus

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
	searchInput := textinput.New()
	searchInput.Placeholder = "Search tasks..."
	searchInput.Width = 50
	searchInput.Prompt = "🔍 "
	searchInput.Focus()

	nameInput := textinput.New()
	nameInput.Placeholder = "Task name..."
	nameInput.Width = 50
	nameInput.Prompt = "Name: "

	descInput := textinput.New()
	descInput.Placeholder = "Description (optional)..."
	descInput.Width = 50
	descInput.Prompt = "Description: "

	return TasksModel{
		db:            db,
		tasks:         tasks,
		filteredTasks: tasks,
		mode:          tasksViewMode,
		searchInput:   searchInput,
		nameInput:     nameInput,
		descInput:     descInput,
		ctx:           context.Background(),
		focus:         panelFocusSearch,
	}
}

func (m *TasksModel) filterTasks() {
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filteredTasks = m.tasks
		if m.cursor >= len(m.filteredTasks) {
			m.cursor = 0
		}
		return
	}
	m.filteredTasks = []models.Task{}
	for _, t := range m.tasks {
		if strings.Contains(strings.ToLower(t.Name), query) ||
			(t.Description.Valid && strings.Contains(strings.ToLower(t.Description.String), query)) {
			m.filteredTasks = append(m.filteredTasks, t)
		}
	}
	if m.cursor >= len(m.filteredTasks) {
		m.cursor = 0
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
		if m.mode == tasksAddLinkMode && len(m.filteredTasks) > 0 {
			taskID := m.filteredTasks[m.cursor].ID
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
		m.filterTasks()
		if len(m.filteredTasks) > 0 && m.cursor < len(m.filteredTasks) {
			return m, m.loadTaskLinks(m.filteredTasks[m.cursor].ID)
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
	halfPage := (m.height - 15) / 2
	if halfPage < 1 {
		halfPage = 1
	}

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
				if len(m.filteredTasks) > 0 {
					return m, m.loadTaskLinks(m.filteredTasks[m.cursor].ID)
				}
			}
		case "down", "j":
			if m.cursor < len(m.filteredTasks)-1 {
				m.cursor++
				if len(m.filteredTasks) > 0 {
					return m, m.loadTaskLinks(m.filteredTasks[m.cursor].ID)
				}
			}
		case "pgup", "ctrl+u":
			if m.cursor-halfPage >= 0 {
				m.cursor -= halfPage
			} else {
				m.cursor = 0
			}
			if len(m.filteredTasks) > 0 {
				return m, m.loadTaskLinks(m.filteredTasks[m.cursor].ID)
			}
		case "pgdown", "ctrl+d":
			if m.cursor+halfPage < len(m.filteredTasks) {
				m.cursor += halfPage
			} else if len(m.filteredTasks) > 0 {
				m.cursor = len(m.filteredTasks) - 1
			}
			if len(m.filteredTasks) > 0 {
				return m, m.loadTaskLinks(m.filteredTasks[m.cursor].ID)
			}
		case "ctrl+a":
			m.mode = tasksCreateMode
			m.createFocus = 0
			m.focus = panelFocusSearch
			m.searchInput.Blur()
			m.nameInput.Focus()
			m.descInput.Blur()
		case "space":
			if len(m.filteredTasks) > 0 && m.cursor < len(m.filteredTasks) {
				task := m.filteredTasks[m.cursor]
				return m, m.toggleTaskCompletion(task.ID, !task.Completed)
			}
		case "enter", "ctrl+o":
			if m.showLinks && len(m.links) > 0 {
				return m, m.openLinks()
			}
		case "esc":
			m.focus = panelFocusSearch
			m.searchInput.Focus()
		}
		return m, nil

	case panelFocusDetail:
		switch msg.String() {
		case "pgup", "pgdown", "ctrl+u", "ctrl+d":
			if m.viewportReady && m.showLinks {
				var cmd tea.Cmd
				m.detailViewport, cmd = m.detailViewport.Update(msg)
				return m, cmd
			}
		case "up", "k":
			if m.viewportReady && m.showLinks {
				m.detailViewport.ScrollUp(1)
			}
		case "down", "j":
			if m.viewportReady && m.showLinks {
				m.detailViewport.ScrollDown(1)
			}
		case "ctrl+a":
			if len(m.filteredTasks) > 0 && m.cursor < len(m.filteredTasks) {
				m.mode = tasksAddLinkMode
				taskID := m.filteredTasks[m.cursor].ID
				m.addLinkModel = NewAddLinkModelForTask(&taskID)
				m.addLinkModel.inModal = true
				return m, func() tea.Msg {
					return tea.WindowSizeMsg{Width: m.width, Height: m.height}
				}
			}
		case "ctrl+o":
			if m.showLinks && len(m.links) > 0 {
				return m, m.openLinks()
			}
		case "esc":
			m.focus = panelFocusSearch
			m.searchInput.Focus()
		}
		return m, nil

	default: // panelFocusSearch — only ctrl/arrow/special keys trigger actions
		switch msg.String() {
		case "up":
			if m.cursor > 0 {
				m.cursor--
				if len(m.filteredTasks) > 0 {
					return m, m.loadTaskLinks(m.filteredTasks[m.cursor].ID)
				}
			}
			return m, nil
		case "down":
			if m.cursor < len(m.filteredTasks)-1 {
				m.cursor++
				if len(m.filteredTasks) > 0 {
					return m, m.loadTaskLinks(m.filteredTasks[m.cursor].ID)
				}
			}
			return m, nil
		case "pgup", "ctrl+u":
			if m.cursor-halfPage >= 0 {
				m.cursor -= halfPage
			} else {
				m.cursor = 0
			}
			if len(m.filteredTasks) > 0 {
				return m, m.loadTaskLinks(m.filteredTasks[m.cursor].ID)
			}
			return m, nil
		case "pgdown", "ctrl+d":
			if m.cursor+halfPage < len(m.filteredTasks) {
				m.cursor += halfPage
			} else if len(m.filteredTasks) > 0 {
				m.cursor = len(m.filteredTasks) - 1
			}
			if len(m.filteredTasks) > 0 {
				return m, m.loadTaskLinks(m.filteredTasks[m.cursor].ID)
			}
			return m, nil
		case "ctrl+a":
			m.mode = tasksCreateMode
			m.createFocus = 0
			m.searchInput.Blur()
			m.nameInput.Focus()
			m.descInput.Blur()
			return m, nil
		case "ctrl+o", "enter":
			if m.showLinks && len(m.links) > 0 {
				return m, m.openLinks()
			}
			return m, nil
		case "esc":
			m.searchInput.SetValue("")
			m.filterTasks()
			return m, nil
		}
		// All other keys feed the search input
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		prevLen := len(m.filteredTasks)
		m.filterTasks()
		if len(m.filteredTasks) > 0 && (len(m.filteredTasks) != prevLen || m.cursor == 0) {
			if m.cursor >= len(m.filteredTasks) {
				m.cursor = 0
			}
			return m, tea.Batch(cmd, m.loadTaskLinks(m.filteredTasks[m.cursor].ID))
		}
		return m, cmd
	}
}

func (m TasksModel) handleCreateMode(msg tea.KeyMsg) (TasksModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = tasksViewMode
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		m.focus = panelFocusSearch
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

	// Search box
	searchBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(panelBorderColor(m.focus == panelFocusSearch))).
		Padding(0, 1).
		Width(leftWidth - 4)
	searchBox := searchBoxStyle.Render(m.searchInput.View())

	// Left panel - task list
	leftPanelStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(panelBorderColor(m.focus == panelFocusList))).
		Padding(1)

	var leftContent strings.Builder
	leftContent.WriteString(searchBox + "\n\n")

	if len(m.filteredTasks) == 0 {
		if m.searchInput.Value() != "" {
			leftContent.WriteString(dimStyle.Render("No tasks match your search.\n"))
		} else {
			leftContent.WriteString(dimStyle.Render("No tasks yet. Press Ctrl+A to create one!\n"))
		}
	} else {
		maxTasks := m.height - 15
		if maxTasks < 3 {
			maxTasks = 3
		}
		startIdx := 0
		endIdx := len(m.filteredTasks)
		if m.cursor >= maxTasks {
			startIdx = m.cursor - maxTasks + 1
		}
		if endIdx > startIdx+maxTasks {
			endIdx = startIdx + maxTasks
		}

		for i := startIdx; i < endIdx; i++ {
			task := m.filteredTasks[i]
			cursor := "  "
			if i == m.cursor {
				cursor = "• "
			}
			status := "[ ]"
			if task.Completed {
				status = "[✓]"
			}
			taskName := task.Name
			if len(taskName) > leftWidth-10 {
				taskName = taskName[:leftWidth-13] + "..."
			}
			line := fmt.Sprintf("%s%s %s", cursor, status, taskName)
			if i == m.cursor {
				leftContent.WriteString(selectedStyle.Render(line) + "\n")
			} else {
				leftContent.WriteString(line + "\n")
			}
			if task.Description.Valid && task.Description.String != "" {
				desc := task.Description.String
				if len(desc) > leftWidth-8 {
					desc = desc[:leftWidth-11] + "..."
				}
				leftContent.WriteString(dimStyle.Render("  "+desc) + "\n")
			}
		}
		if len(m.filteredTasks) > maxTasks {
			leftContent.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  [%d/%d tasks]", m.cursor+1, len(m.filteredTasks))))
		}
	}

	leftPanel := leftPanelStyle.Render(leftContent.String())

	// Right panel - links for selected task
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

	if len(m.filteredTasks) > 0 && m.cursor < len(m.filteredTasks) {
		task := m.filteredTasks[m.cursor]

		var rightBuilder strings.Builder
		rightBuilder.WriteString(titleStyle.Render("Links for: "+task.Name) + "\n\n")

		if m.showLinks {
			if len(m.links) == 0 {
				rightBuilder.WriteString(dimStyle.Render("No links yet. Tab to detail panel, then Ctrl+A to add."))
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

				rightBuilder.WriteString("\n\n" + dimStyle.Render("Ctrl+O: open all links"))
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
	var helpMsg string
	switch m.focus {
	case panelFocusList:
		helpMsg = "Tab: detail • ↑/↓/j/k: navigate • PgUp/PgDn/Ctrl+U/D: jump • Ctrl+A: new task • Space: toggle • Ctrl+O: open links • Esc: search"
	case panelFocusDetail:
		helpMsg = "Tab: search • ↑/↓/j/k/PgUp/PgDn: scroll • Ctrl+A: add link • Ctrl+O: open links • Esc: search"
	default: // panelFocusSearch
		helpMsg = "type to search • Tab: list • ↑/↓: navigate • Ctrl+A: new task • Ctrl+O: open links • Esc: clear"
	}
	helpText := "\n" + helpStyle.Render(helpMsg)

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
