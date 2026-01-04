package tui

import (
	"context"
	"database/sql"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pkg/browser"

	"mccwk.com/lk/internal/database"
	"mccwk.com/lk/internal/models"
)

type TasksModel struct {
	tasks     []models.Task
	cursor    int
	db        *database.Database
	links     []models.Link
	showLinks bool
}

func NewTasksModel(tasks []sql.NullString, db *database.Database) TasksModel {
	// TODO: Implement proper task loading
	return TasksModel{
		db:    db,
		tasks: []models.Task{},
	}
}

func (m TasksModel) Update(msg tea.Msg) (TasksModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.tasks)-1 {
				m.cursor++
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
		}

	case taskLinksLoadedMsg:
		m.links = msg.links
		m.showLinks = true
		return m, nil
	}

	return m, nil
}

func (m TasksModel) View() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	s := titleStyle.Render("Tasks") + "\n\n"

	if len(m.tasks) == 0 {
		s += "No tasks yet. Create a task first!\n"
	} else {
		for i, task := range m.tasks {
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
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
		for _, link := range m.links {
			title := link.Title.String
			if title == "" {
				title = link.Url
			}
			s += fmt.Sprintf("  • %s\n", title)
		}
		s += "\nPress 'o' to open all links in browser"
	}

	s += "\n\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Use arrows or j/k • Enter to view links")

	return s
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

type taskLinksLoadedMsg struct {
	links []models.Link
}
