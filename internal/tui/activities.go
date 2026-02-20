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
	activities         []models.Activity
	filteredActivities []models.Activity
	cursor             int
	db                 *database.Database
	ctx                context.Context
	fetcher            *services.Fetcher
	extractor          *services.Extractor
	summarizer         *services.Summarizer
	links              []models.Link
	showLinks          bool

	// Mode management
	mode activitiesMode

	// Search
	searchInput textinput.Model

	// Create activity inputs
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

func NewActivitiesModel(db *database.Database) ActivitiesModel {
	searchInput := textinput.New()
	searchInput.Placeholder = "Search activities..."
	searchInput.Width = 50
	searchInput.Prompt = "🔍 "
	searchInput.Focus()

	nameInput := textinput.New()
	nameInput.Placeholder = "Activity name..."
	nameInput.Width = 50
	nameInput.Prompt = "Name: "

	descInput := textinput.New()
	descInput.Placeholder = "Description (optional)..."
	descInput.Width = 50
	descInput.Prompt = "Description: "

	return ActivitiesModel{
		db:          db,
		mode:        activitiesViewMode,
		searchInput: searchInput,
		nameInput:   nameInput,
		descInput:   descInput,
		ctx:         context.Background(),
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

	case addLinkCloseRequestedMsg:
		if m.mode == activitiesAddLinkMode {
			m.mode = activitiesViewMode
			return m, nil
		}

	case linkProcessCompleteMsg:
		if m.mode == activitiesAddLinkMode && len(m.filteredActivities) > 0 {
			activityID := m.filteredActivities[m.cursor].ID
			linkID := msg.linkID
			m.mode = activitiesViewMode
			return m, tea.Batch(
				m.linkToActivity(activityID, linkID),
				m.loadActivityLinks(activityID),
				notifyCmd("info", "Link added to activity!"),
			)
		}
		return m, nil

	case activityLinksLoadedMsg:
		m.links = msg.links
		m.showLinks = true
		return m, nil

	case activitiesLoadedMsg:
		m.activities = msg.activities
		m.filterActivities()
		// Automatically load links for the first activity
		if len(m.filteredActivities) > 0 && m.cursor < len(m.filteredActivities) {
			return m, m.loadActivityLinks(m.filteredActivities[m.cursor].ID)
		}
		return m, nil

	case activityCreatedMsg:
		m.mode = activitiesViewMode
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		m.nameInput.Blur()
		m.descInput.Blur()
		m.searchInput.Focus()
		return m, tea.Batch(m.loadActivities(), notifyCmd("info", "Activity created!"))
	}

	// Forward remaining messages to addLinkModel when in add link mode
	// (handles linkProcessErrorMsg, metadataSavedMsg, tea.WindowSizeMsg, etc.)
	if m.mode == activitiesAddLinkMode {
		m.addLinkModel, cmd = m.addLinkModel.Update(msg, m.db, m.fetcher, m.extractor, m.summarizer, m.ctx)
		return m, cmd
	}

	return m, nil
}

func (m ActivitiesModel) handleViewMode(msg tea.KeyMsg) (ActivitiesModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			if len(m.filteredActivities) > 0 {
				return m, m.loadActivityLinks(m.filteredActivities[m.cursor].ID)
			}
		}
		return m, nil
	case "down", "j":
		if m.cursor < len(m.filteredActivities)-1 {
			m.cursor++
			if len(m.filteredActivities) > 0 {
				return m, m.loadActivityLinks(m.filteredActivities[m.cursor].ID)
			}
		}
		return m, nil
	case "o":
		// Open all links for current activity
		if m.showLinks && len(m.links) > 0 {
			return m, m.openLinks()
		}
		return m, nil
	case "n":
		// Create new activity
		m.mode = activitiesCreateMode
		m.createFocus = 0
		m.searchInput.Blur()
		m.nameInput.Focus()
		m.descInput.Blur()
		return m, nil
	case "a":
		// Add link to current activity
		if len(m.filteredActivities) > 0 && m.cursor < len(m.filteredActivities) {
			m.mode = activitiesAddLinkMode
			m.addLinkModel = NewAddLinkModel()
			m.addLinkModel.inModal = true
			return m, func() tea.Msg {
				return tea.WindowSizeMsg{Width: m.width, Height: m.height}
			}
		}
		return m, nil
	case "pgup", "pgdown":
		if m.viewportReady && m.showLinks {
			var cmd tea.Cmd
			m.detailViewport, cmd = m.detailViewport.Update(msg)
			return m, cmd
		}
		return m, nil
	case "esc":
		m.searchInput.SetValue("")
		m.filterActivities()
		return m, nil
	}

	// All other keys go to search input
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	prevLen := len(m.filteredActivities)
	m.filterActivities()
	if len(m.filteredActivities) > 0 && (len(m.filteredActivities) != prevLen || m.cursor == 0) {
		if m.cursor >= len(m.filteredActivities) {
			m.cursor = 0
		}
		return m, tea.Batch(cmd, m.loadActivityLinks(m.filteredActivities[m.cursor].ID))
	}
	return m, cmd
}

func (m *ActivitiesModel) filterActivities() {
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filteredActivities = m.activities
		if m.cursor >= len(m.filteredActivities) {
			m.cursor = 0
		}
		return
	}
	m.filteredActivities = []models.Activity{}
	for _, a := range m.activities {
		if strings.Contains(strings.ToLower(a.Name), query) ||
			(a.Description.Valid && strings.Contains(strings.ToLower(a.Description.String), query)) {
			m.filteredActivities = append(m.filteredActivities, a)
		}
	}
	if m.cursor >= len(m.filteredActivities) {
		m.cursor = 0
	}
}

func (m ActivitiesModel) handleCreateMode(msg tea.KeyMsg) (ActivitiesModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = activitiesViewMode
		m.nameInput.SetValue("")
		m.descInput.SetValue("")
		m.nameInput.Blur()
		m.descInput.Blur()
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

	leftWidth := int(float64(m.width) * 0.35)
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := m.width - leftWidth - 8

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))

	// Search box
	searchBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("10")).
		Padding(0, 1).
		Width(leftWidth - 4)
	searchBox := searchBoxStyle.Render(m.searchInput.View())

	// Left panel — activities list
	leftPanelStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1)

	var leftContent strings.Builder
	leftContent.WriteString(searchBox + "\n\n")

	if len(m.filteredActivities) == 0 {
		if m.searchInput.Value() != "" {
			leftContent.WriteString(dimStyle.Render("No activities match your search.\n"))
		} else {
			leftContent.WriteString(dimStyle.Render("No activities yet. Press 'n' to create one!\n"))
		}
	} else {
		maxItems := m.height - 15
		if maxItems < 3 {
			maxItems = 3
		}
		startIdx := 0
		endIdx := len(m.filteredActivities)
		if m.cursor >= maxItems {
			startIdx = m.cursor - maxItems + 1
		}
		if endIdx > startIdx+maxItems {
			endIdx = startIdx + maxItems
		}

		for i := startIdx; i < endIdx; i++ {
			activity := m.filteredActivities[i]
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
			if activity.Description.Valid && activity.Description.String != "" {
				desc := activity.Description.String
				if len(desc) > leftWidth-8 {
					desc = desc[:leftWidth-11] + "..."
				}
				leftContent.WriteString(dimStyle.Render("  "+desc) + "\n")
			}
		}
		if len(m.filteredActivities) > maxItems {
			leftContent.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  [%d/%d activities]", m.cursor+1, len(m.filteredActivities))))
		}
	}

	leftPanel := leftPanelStyle.Render(leftContent.String())

	// Right panel — links for selected activity
	rightPanelStyle := lipgloss.NewStyle().
		Width(rightWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(1)

	var rightContent string

	if len(m.filteredActivities) > 0 && m.cursor < len(m.filteredActivities) {
		activity := m.filteredActivities[m.cursor]

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
						rightBuilder.WriteString(dimStyle.Render(fmt.Sprintf("\n[%d%% - PgUp/PgDn to scroll]", scrollPercent)))
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
	helpText := "\n" + helpStyle.Render("type to search • ↑/↓/j/k: navigate • n: new • a: add link • o: open links • PgUp/PgDn: scroll • Esc: clear")

	return mainContent + helpText
}

func (m ActivitiesModel) viewCreateActivity() string {
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
	content.WriteString(titleStyle.Render("Create New Activity") + "\n\n")
	content.WriteString(m.nameInput.View() + "\n\n")
	content.WriteString(m.descInput.View() + "\n\n")
	content.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Tab: switch fields • Enter: create • Esc: cancel"))

	modal := modalStyle.Render(content.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
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
