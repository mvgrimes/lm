package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pkg/browser"

	"mccwk.com/lm/internal/database"
	"mccwk.com/lm/internal/models"
)

type ReadLaterModel struct {
	links  []models.Link
	cursor int
	db     *database.Database
	ctx    context.Context

	// Detail view
	detailViewport viewport.Model
	viewportReady  bool

	width  int
	height int
}

func NewReadLaterModel(links []models.Link) ReadLaterModel {
	return ReadLaterModel{
		links: links,
		ctx:   context.Background(),
	}
}

func (m ReadLaterModel) Update(msg tea.Msg) (ReadLaterModel, tea.Cmd) {
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

		if len(m.links) > 0 {
			m.updateDetailView()
		}

		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.updateDetailView()
			}
		case "down", "j":
			if m.cursor < len(m.links)-1 {
				m.cursor++
				m.updateDetailView()
			}
		case "o", "enter":
			if len(m.links) > 0 && m.cursor < len(m.links) {
				return m, m.openLink(m.links[m.cursor].Url)
			}
		case "pgup", "pgdown":
			// Scroll detail viewport
			if m.viewportReady {
				m.detailViewport, cmd = m.detailViewport.Update(msg)
				return m, cmd
			}
		}
	}

	return m, nil
}

func (m ReadLaterModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Calculate responsive widths
	leftWidth := int(float64(m.width) * 0.35)
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := m.width - leftWidth - 8

	// Left panel - link list
	leftPanelStyle := lipgloss.NewStyle().
		Width(leftWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	var leftContent string

	if len(m.links) == 0 {
		leftContent = dimStyle.Render("No links to read later.\nAdd some with Ctrl+A!\n")
	} else {
		// Show links list with scrolling
		maxLinks := m.height - 15
		if maxLinks < 3 {
			maxLinks = 3
		}

		startIdx := 0
		endIdx := len(m.links)

		// Ensure cursor is visible
		if m.cursor >= maxLinks {
			startIdx = m.cursor - maxLinks + 1
		}
		if endIdx > startIdx+maxLinks {
			endIdx = startIdx + maxLinks
		}

		for i := startIdx; i < endIdx; i++ {
			link := m.links[i]
			cursor := "  "
			if i == m.cursor {
				cursor = "• "
			}

			title := link.Title.String
			if title == "" {
				title = link.Url
			}
			// Truncate title to fit
			if len(title) > leftWidth-8 {
				title = title[:leftWidth-11] + "..."
			}

			line := fmt.Sprintf("%s%s", cursor, title)

			if i == m.cursor {
				leftContent += selectedStyle.Render(line) + "\n"
				// Show short summary
				if link.Summary.Valid && link.Summary.String != "" {
					summary := link.Summary.String
					if len(summary) > leftWidth-8 {
						summary = summary[:leftWidth-11] + "..."
					}
					leftContent += dimStyle.Render("  "+summary) + "\n"
				}
			} else {
				leftContent += line + "\n"
			}
		}

		// Show scroll indicator
		if len(m.links) > maxLinks {
			leftContent += "\n" + dimStyle.Render(fmt.Sprintf("  [%d/%d links]", m.cursor+1, len(m.links)))
		}
	}

	leftPanel := leftPanelStyle.Render(leftContent)

	// Right panel - detail view
	rightPanelStyle := lipgloss.NewStyle().
		Width(rightWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(1)

	var rightContent string

	if len(m.links) > 0 && m.cursor < len(m.links) {
		link := m.links[m.cursor]

		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6"))

		titleLine := titleStyle.Render("Details") + "\n\n"

		// URL
		urlStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
		titleLine += urlStyle.Render(link.Url) + "\n\n"

		rightContent = titleLine + m.detailViewport.View()

		// Show scroll indicator
		if m.viewportReady && m.detailViewport.TotalLineCount() > m.detailViewport.Height {
			scrollPercent := int(m.detailViewport.ScrollPercent() * 100)
			scrollInfo := lipgloss.NewStyle().
				Foreground(lipgloss.Color("243")).
				Render(fmt.Sprintf("\n[%d%% - PgUp/PgDn to scroll]", scrollPercent))
			rightContent += scrollInfo
		}
	} else {
		rightContent = dimStyle.Render("Select a link to view details...")
	}

	rightPanel := rightPanelStyle.Render(rightContent)

	// Combine panels
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, "  ", rightPanel)

	// Help text
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpText := "\n" + helpStyle.Render("arrows/j/k: navigate • Enter/o: open • PgUp/PgDn: scroll details")

	return mainContent + helpText
}

func (m *ReadLaterModel) updateDetailView() {
	if !m.viewportReady || len(m.links) == 0 || m.cursor >= len(m.links) {
		return
	}

	link := m.links[m.cursor]

	// Get viewport width for wrapping
	wrapWidth := m.detailViewport.Width
	if wrapWidth < 20 {
		wrapWidth = 20
	}

	var content strings.Builder

	// Title
	if link.Title.Valid && link.Title.String != "" {
		wrapped := wrapText(link.Title.String, wrapWidth)
		content.WriteString(lipgloss.NewStyle().Bold(true).Render(wrapped))
		content.WriteString("\n\n")
	}

	// Summary
	if link.Summary.Valid && link.Summary.String != "" {
		summaryStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Bold(true)
		content.WriteString(summaryStyle.Render("Summary:") + "\n")
		wrapped := wrapText(link.Summary.String, wrapWidth)
		content.WriteString(wrapped)
		content.WriteString("\n\n")
	}

	// Content
	if link.Content.Valid && link.Content.String != "" {
		contentStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("7"))
		content.WriteString(lipgloss.NewStyle().Bold(true).Render("Content:") + "\n")
		wrapped := wrapText(link.Content.String, wrapWidth)
		content.WriteString(contentStyle.Render(wrapped))
	}

	m.detailViewport.SetContent(content.String())
	m.detailViewport.GotoTop()
}

func (m ReadLaterModel) openLink(url string) tea.Cmd {
	return func() tea.Msg {
		_ = browser.OpenURL(url)
		return nil
	}
}
