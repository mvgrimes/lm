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

type activitiesMode int

const (
	activitiesViewMode activitiesMode = iota
	activitiesCreateMode
	activitiesAddLinkMode
)

type ActivitiesModel struct {
	activities []models.Activity
	cursor     int
	db         *database.Database
	ctx        context.Context
	fetcher    *services.Fetcher
	extractor  *services.Extractor
	summarizer *services.Summarizer
	links      []models.Link
	showLinks  bool

	// Mode management
	mode activitiesMode

	// Create activity inputs
	nameInput   textinput.Model
	descInput   textinput.Model
	createFocus int

	// Add link mode - use the AddLinkModel as a dialog
	addLinkModel AddLinkModel

	// Detail view for links
	detailViewport viewport.Model
	viewportReady  bool

	message string
	width   int
	height  int
}

func NewActivitiesModel(db *database.Database) ActivitiesModel {
	nameInput := textinput.New()
	nameInput.Placeholder = "Activity name..."
	nameInput.Width = 50
	nameInput.Prompt = "Name: "

	descInput := textinput.New()
	descInput.Placeholder = "Description (optional)..."
	descInput.Width = 50
	descInput.Prompt = "Description: "

	return ActivitiesModel{
		db:        db,
		mode:      activitiesViewMode,
		nameInput: nameInput,
		descInput: descInput,
		ctx:       context.Background(),
	}
}

func (m *ActivitiesModel) SetServices(fetcher *services.Fetcher, extractor *services.Extractor, summarizer *services.Summarizer) {
	m.fetcher = fetcher
	m.extractor = extractor
	m.summarizer = summarizer
}

func (m ActivitiesModel) Update(msg tea.Msg) (ActivitiesModel, tea.Cmd) {
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
		if m.mode == activitiesAddLinkMode {
			m.addLinkModel, cmd = m.addLinkModel.Update(msg, m.db, m.fetcher, m.extractor, m.summarizer, m.ctx)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		// If in add link mode, delegate to addLinkModel
		if m.mode == activitiesAddLinkMode {
			// Check for esc to exit add link mode
			if msg.String() == "esc" {
				m.mode = activitiesViewMode
				return m, nil
			}
			m.addLinkModel, cmd = m.addLinkModel.Update(msg, m.db, m.fetcher, m.extractor, m.summarizer, m.ctx)
			return m, cmd
		}

		switch m.mode {
		case activitiesViewMode:
			return m.handleViewMode(msg)
		case activitiesCreateMode:
			return m.handleCreateMode(msg)
		}

	case linkProcessCompleteMsg:
		// Link was added successfully, link it to the current activity (task)
		if m.mode == activitiesAddLinkMode && len(m.activities) > 0 {
			activityID := m.activities[m.cursor].ID
			linkID := msg.linkID
			m.mode = activitiesViewMode
			m.message = "Link added to activity!"
			return m, tea.Batch(
				m.linkToActivity(activityID, linkID),
				m.loadActivityLinks(activityID),
			)
		}
		return m, nil

	case activityLinksLoadedMsg:
		m.links = msg.links
		m.showLinks = true
		return m, nil

	case activitiesLoadedMsg:
		m.activities = msg.activities
		// Automatically load links for the first activity
		if len(m.activities) > 0 && m.cursor < len(m.activities) {
			return m, m.loadActivityLinks(m.activities[m.cursor].ID)
		}
		return m, nil

	case activityCreatedMsg:
		m.mode = activitiesViewMode
		m.message = "Activity created!"
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		return m, m.loadActivities()
	}

	return m, nil
}

func (m ActivitiesModel) handleViewMode(msg tea.KeyMsg) (ActivitiesModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			if len(m.activities) > 0 {
				return m, m.loadActivityLinks(m.activities[m.cursor].ID)
			}
		}
	case "down", "j":
		if m.cursor < len(m.activities)-1 {
			m.cursor++
			if len(m.activities) > 0 {
				return m, m.loadActivityLinks(m.activities[m.cursor].ID)
			}
		}
	case "o":
		// Open all links for current activity
		if m.showLinks && len(m.links) > 0 {
			return m, m.openLinks()
		}
	case "n":
		// Create new activity
		m.mode = activitiesCreateMode
		m.createFocus = 0
		m.nameInput.Focus()
		m.descInput.Blur()
		m.message = ""
	case "a":
		// Add link to current activity
		if len(m.activities) > 0 && m.cursor < len(m.activities) {
			m.mode = activitiesAddLinkMode
			m.message = ""
			m.addLinkModel = NewAddLinkModel()
			return m, func() tea.Msg {
				return tea.WindowSizeMsg{Width: m.width, Height: m.height}
			}
		}
	case "pgup", "pgdown":
		if m.viewportReady && m.showLinks {
			var cmd tea.Cmd
			m.detailViewport, cmd = m.detailViewport.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m ActivitiesModel) handleCreateMode(msg tea.KeyMsg) (ActivitiesModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = activitiesViewMode
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
			return m, m.createActivity(name, m.descInput.Value())
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

func (m ActivitiesModel) View() string {
	switch m.mode {
	case activitiesViewMode:
		return m.viewActivities()
	case activitiesCreateMode:
		return m.viewCreateActivity()
	case activitiesAddLinkMode:
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

func (m ActivitiesModel) viewActivities() string {
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

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10"))

	// Left panel - activities list
	leftPanelStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1)

	var leftContent strings.Builder
	leftContent.WriteString(titleStyle.Render("Activities") + "\n\n")

	if m.message != "" {
		leftContent.WriteString(messageStyle.Render(m.message) + "\n\n")
	}

	if len(m.activities) == 0 {
		leftContent.WriteString(dimStyle.Render("No activities yet. Press 'n' to create one!\n"))
	} else {
		// Show activities list with scrolling
		maxItems := m.height - 15
		if maxItems < 3 {
			maxItems = 3
		}

		startIdx := 0
		endIdx := len(m.activities)

		// Ensure cursor is visible
		if m.cursor >= maxItems {
			startIdx = m.cursor - maxItems + 1
		}
		if endIdx > startIdx+maxItems {
			endIdx = startIdx + maxItems
		}

		for i := startIdx; i < endIdx; i++ {
			activity := m.activities[i]
			cursor := "  "
			if i == m.cursor {
				cursor = "• "
			}

			name := activity.Name
			if len(name) > leftWidth-8 {
				name = name[:leftWidth-11] + "..."
			}

			line := fmt.Sprintf("%s%s", cursor, name)

			if i == m.cursor {
				leftContent.WriteString(selectedStyle.Render(line) + "\n")
			} else {
				leftContent.WriteString(line + "\n")
			}

			// Show description
			if activity.Description.Valid && activity.Description.String != "" {
				desc := activity.Description.String
				if len(desc) > leftWidth-8 {
					desc = desc[:leftWidth-11] + "..."
				}
				leftContent.WriteString(dimStyle.Render("  "+desc) + "\n")
			}
		}

		// Scroll indicator
		if len(m.activities) > maxItems {
			leftContent.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  [%d/%d activities]", m.cursor+1, len(m.activities))))
		}
	}

	leftPanel := leftPanelStyle.Render(leftContent.String())

	// Right panel - links for selected activity
	rightPanelStyle := lipgloss.NewStyle().
		Width(rightWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(1)

	var rightContent string

	if len(m.activities) > 0 && m.cursor < len(m.activities) {
		activity := m.activities[m.cursor]

		var rightBuilder strings.Builder
		rightBuilder.WriteString(titleStyle.Render("Links for: "+activity.Name) + "\n\n")

		if m.showLinks {
			if len(m.links) == 0 {
				rightBuilder.WriteString(dimStyle.Render("No links yet. Press 'a' to add a link."))
			} else {
				var detailContent strings.Builder
				for _, link := range m.links {
					title := link.Title.String
					if title == "" {
						title = link.Url
					}
					detailContent.WriteString(fmt.Sprintf("• %s\n", title))
					detailContent.WriteString(dimStyle.Render("  "+link.Url) + "\n")
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
		rightContent = dimStyle.Render("Select an activity to view its links...")
	}

	rightPanel := rightPanelStyle.Render(rightContent)

	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, "  ", rightPanel)

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpText := "\n" + helpStyle.Render("n: new activity • a: add link • o: open links • arrows/j/k: navigate • PgUp/PgDn: scroll")

	return mainContent + helpText
}

func (m ActivitiesModel) viewCreateActivity() string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("6")).
		MarginBottom(1)

	s := titleStyle.Render("Create New Activity") + "\n\n"
	s += m.nameInput.View() + "\n\n"
	s += m.descInput.View() + "\n\n"
	s += lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Tab: switch fields • Enter: create • Esc: cancel")

	return s
}

func (m ActivitiesModel) loadActivities() tea.Cmd {
	return func() tea.Msg {
		items, err := m.db.Queries.ListActivities(context.Background())
		if err != nil {
			return errMsg{err: err}
		}
		return activitiesLoadedMsg{activities: items}
	}
}

func (m ActivitiesModel) loadActivityLinks(activityID int64) tea.Cmd {
	return func() tea.Msg {
		links, err := m.db.Queries.GetLinksForActivity(context.Background(), activityID)
		if err != nil {
			return errMsg{err: err}
		}
		return activityLinksLoadedMsg{links: links}
	}
}

func (m ActivitiesModel) openLinks() tea.Cmd {
	return func() tea.Msg {
		for _, link := range m.links {
			_ = browser.OpenURL(link.Url)
		}
		return nil
	}
}

func (m ActivitiesModel) createActivity(name, description string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.db.Queries.CreateActivity(context.Background(), models.CreateActivityParams{
			Name:        name,
			Description: sql.NullString{String: description, Valid: description != ""},
		})
		if err != nil {
			return errMsg{err: err}
		}
		return activityCreatedMsg{}
	}
}

func (m ActivitiesModel) linkToActivity(activityID, linkID int64) tea.Cmd {
	return func() tea.Msg {
		err := m.db.Queries.LinkActivity(context.Background(), models.LinkActivityParams{
			LinkID:     linkID,
			ActivityID: activityID,
		})
		if err != nil {
			return errMsg{err: err}
		}
		return linkAddedToActivityMsg{}
	}
}

// Messages

type activityLinksLoadedMsg struct {
	links []models.Link
}

type activitiesLoadedMsg struct {
	activities []models.Activity
}

type activityCreatedMsg struct{}

type linkAddedToActivityMsg struct{}
