package tui

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mccwk.com/lk/internal/database"
	"mccwk.com/lk/internal/models"
)

type categoriesMode int

const (
	categoriesViewMode categoriesMode = iota
	categoriesCreateMode
)

type CategoriesModel struct {
	categories   []models.Category
	cursor       int
	db           *database.Database
	ctx          context.Context
	mode         categoriesMode
	links        []models.Link
	showingLinks bool

	// Create mode
	nameInput   textinput.Model
	descInput   textinput.Model
	createFocus int

	message string
	width   int
	height  int
}

func NewCategoriesModel(db *database.Database) CategoriesModel {
	nameInput := textinput.New()
	nameInput.Placeholder = "Category name..."
	nameInput.Width = 50
	nameInput.Prompt = "Name: "

	descInput := textinput.New()
	descInput.Placeholder = "Description (optional)..."
	descInput.Width = 50
	descInput.Prompt = "Description: "

	return CategoriesModel{
		db:        db,
		ctx:       context.Background(),
		mode:      categoriesViewMode,
		nameInput: nameInput,
		descInput: descInput,
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
		return m, nil

	case categoryCreatedMsg:
		m.mode = categoriesViewMode
		m.message = "Category created!"
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		return m, m.loadCategories()

	case categoryLinksLoadedMsg:
		m.links = msg.links
		m.showingLinks = true
		return m, nil
	}

	return m, nil
}

func (m CategoriesModel) handleViewMode(msg tea.KeyMsg) (CategoriesModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.showingLinks = false
		}
	case "down", "j":
		if m.cursor < len(m.categories)-1 {
			m.cursor++
			m.showingLinks = false
		}
	case "enter":
		if len(m.categories) > 0 && m.cursor < len(m.categories) {
			return m, m.loadCategoryLinks(m.categories[m.cursor].ID)
		}
	case "n":
		// Create new category
		m.mode = categoriesCreateMode
		m.createFocus = 0
		m.nameInput.Focus()
		m.descInput.Blur()
		m.message = ""
	case "d":
		// Delete category
		if len(m.categories) > 0 && m.cursor < len(m.categories) {
			return m, m.deleteCategory(m.categories[m.cursor].ID)
		}
	}
	return m, nil
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

	// Update focused input
	if m.createFocus == 0 {
		m.nameInput, cmd = m.nameInput.Update(msg)
	} else {
		m.descInput, cmd = m.descInput.Update(msg)
	}
	return m, cmd
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
	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10"))

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	var s string

	if m.message != "" {
		s += messageStyle.Render(m.message) + "\n\n"
	}

	if len(m.categories) == 0 {
		s += dimStyle.Render("No categories yet. Press 'n' to create one!\n")
	} else {
		for i, cat := range m.categories {
			cursor := "  "
			if i == m.cursor {
				cursor = "• "
			}

			line := fmt.Sprintf("%s%s", cursor, cat.Name)

			if i == m.cursor {
				s += selectedStyle.Render(line) + "\n"
				if cat.Description.Valid && cat.Description.String != "" {
					s += dimStyle.Render("  "+cat.Description.String) + "\n"
				}
			} else {
				s += line + "\n"
			}
		}
	}

	if m.showingLinks {
		s += "\n" + lipgloss.NewStyle().Bold(true).Render("Links in this category:") + "\n"
		if len(m.links) == 0 {
			s += dimStyle.Render("  No links in this category.\n")
		} else {
			for _, link := range m.links {
				title := link.Title.String
				if title == "" {
					title = link.Url
				}
				s += fmt.Sprintf("  • %s\n", title)
			}
		}
	}

	s += "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("n: new category • d: delete • Enter: view links • arrows/j/k: navigate")

	return s
}

func (m CategoriesModel) viewCreateCategory() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	s := titleStyle.Render("Create New Category") + "\n\n"
	s += m.nameInput.View() + "\n\n"
	s += m.descInput.View() + "\n\n"
	s += lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Tab: switch fields • Enter: create • Esc: cancel")

	return s
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
		// Reload categories
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
