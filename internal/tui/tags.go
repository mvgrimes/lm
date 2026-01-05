package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mccwk.com/lk/internal/database"
	"mccwk.com/lk/internal/models"
)

type tagsMode int

const (
	tagsViewMode tagsMode = iota
	tagsCreateMode
)

type TagsModel struct {
	tags         []models.Tag
	cursor       int
	db           *database.Database
	ctx          context.Context
	mode         tagsMode
	links        []models.Link
	showingLinks bool

	// Create mode
	nameInput textinput.Model

	message string
	width   int
	height  int
}

func NewTagsModel(db *database.Database) TagsModel {
	nameInput := textinput.New()
	nameInput.Placeholder = "Tag name..."
	nameInput.Width = 50
	nameInput.Prompt = "Name: "

	return TagsModel{
		db:        db,
		ctx:       context.Background(),
		mode:      tagsViewMode,
		nameInput: nameInput,
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
		return m, nil

	case tagCreatedMsg:
		m.mode = tagsViewMode
		m.message = "Tag created!"
		m.nameInput.SetValue("")
		return m, m.loadTags()

	case tagLinksLoadedMsg:
		m.links = msg.links
		m.showingLinks = true
		return m, nil
	}

	return m, nil
}

func (m TagsModel) handleViewMode(msg tea.KeyMsg) (TagsModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.showingLinks = false
		}
	case "down", "j":
		if m.cursor < len(m.tags)-1 {
			m.cursor++
			m.showingLinks = false
		}
	case "enter":
		if len(m.tags) > 0 && m.cursor < len(m.tags) {
			return m, m.loadTagLinks(m.tags[m.cursor].ID)
		}
	case "n":
		// Create new tag
		m.mode = tagsCreateMode
		m.nameInput.Focus()
		m.message = ""
	case "d":
		// Delete tag
		if len(m.tags) > 0 && m.cursor < len(m.tags) {
			return m, m.deleteTag(m.tags[m.cursor].ID)
		}
	}
	return m, nil
}

func (m TagsModel) handleCreateMode(msg tea.KeyMsg) (TagsModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg.String() {
	case "esc":
		m.mode = tagsViewMode
		m.nameInput.SetValue("")
		m.nameInput.Blur()
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

	if len(m.tags) == 0 {
		s += dimStyle.Render("No tags yet. Press 'n' to create one!\n")
	} else {
		for i, tag := range m.tags {
			cursor := "  "
			if i == m.cursor {
				cursor = "• "
			}

			line := fmt.Sprintf("%s%s", cursor, tag.Name)

			if i == m.cursor {
				s += selectedStyle.Render(line) + "\n"
			} else {
				s += line + "\n"
			}
		}
	}

	if m.showingLinks {
		s += "\n" + lipgloss.NewStyle().Bold(true).Render("Links with this tag:") + "\n"
		if len(m.links) == 0 {
			s += dimStyle.Render("  No links with this tag.\n")
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
		Render("n: new tag • d: delete • Enter: view links • arrows/j/k: navigate")

	return s
}

func (m TagsModel) viewCreateTag() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	s := titleStyle.Render("Create New Tag") + "\n\n"
	s += m.nameInput.View() + "\n\n"
	s += lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Enter: create • Esc: cancel")

	return s
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
		// Reload tags
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
