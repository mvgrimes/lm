package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// renderMarkdown renders a markdown string for display in the terminal using
// glamour.  width is the viewport width; glamour's default style adds 2-char
// margins on each side, so the word-wrap is set to width-4.
func renderMarkdown(md string, width int) string {
	ww := width - 4
	if ww < 20 {
		ww = 20
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(ww),
	)
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return out
}

// panelFocus is shared by all split-view tabs.
// 0=search box, 1=list panel, 2=right/detail panel
type panelFocus int

const (
	panelFocusSearch panelFocus = iota
	panelFocusList
	panelFocusDetail
)

// cycleFocusForward advances focus in the order search → list → detail → search.
func cycleFocusForward(f panelFocus) panelFocus { return (f + 1) % 3 }

// cycleFocusBackward retreats focus in the reverse order.
func cycleFocusBackward(f panelFocus) panelFocus { return (f + 2) % 3 }

// panelBorderColor returns the border colour for a panel depending on whether
// it currently holds focus (active=green, inactive=dim).
func panelBorderColor(focused bool) string {
	if focused {
		return "10"
	}
	return "8"
}

type splitViewDims struct {
	leftWidth    int
	rightWidth   int
	panelHeight  int
	detailHeight int
}

func calcSplitViewDims(width, height int) splitViewDims {
	leftWidth := int(float64(width) * 0.35)
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := width - leftWidth - 8
	panelHeight := height - 6
	if panelHeight < 5 {
		panelHeight = 5
	}
	detailHeight := height - 14
	if detailHeight < 5 {
		detailHeight = 5
	}

	return splitViewDims{
		leftWidth:    leftWidth,
		rightWidth:   rightWidth,
		panelHeight:  panelHeight,
		detailHeight: detailHeight,
	}
}

func initOrResizeViewport(vp *viewport.Model, ready *bool, width, height int, onReady func()) {
	if !*ready {
		*vp = viewport.New(width, height)
		vp.SetContent("")
		*ready = true
		if onReady != nil {
			onReady()
		}
		return
	}
	vp.Width = width
	vp.Height = height
}

func renderSearchBox(input textinput.Model, focused bool, width int) string {
	searchBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(panelBorderColor(focused))).
		Padding(0, 1).
		Width(width)
	return searchBoxStyle.Render(input.View())
}

func renderScrollHint(vp viewport.Model, dimStyle lipgloss.Style) string {
	if vp.TotalLineCount() <= vp.Height {
		return ""
	}
	scrollPercent := int(vp.ScrollPercent() * 100)
	return "\n" + dimStyle.Render(fmt.Sprintf("[%d%% - PgUp/PgDn to scroll]", scrollPercent))
}

func visibleRange(cursor, itemCount, maxItems int) (int, int) {
	if itemCount <= 0 || maxItems <= 0 {
		return 0, 0
	}
	startIdx := 0
	endIdx := itemCount
	if cursor >= maxItems {
		startIdx = cursor - maxItems + 1
	}
	if endIdx > startIdx+maxItems {
		endIdx = startIdx + maxItems
	}
	return startIdx, endIdx
}

// linkMatchesQuery returns true when a link matches every whitespace-separated
// word in the query (case-insensitive AND search). Word order is ignored.
func linkMatchesQuery(url, title, content, summary, query string) bool {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return true
	}
	haystack := strings.ToLower(url + " " + title + " " + content + " " + summary)
	for _, w := range words {
		if !strings.Contains(haystack, w) {
			return false
		}
	}
	return true
}

// wrapText wraps text to the specified width, breaking on word boundaries
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}

		// If line is already shorter than width, keep it
		if len(line) <= width {
			result.WriteString(line)
			continue
		}

		// Wrap the line
		words := strings.Fields(line)
		if len(words) == 0 {
			continue
		}

		currentLine := words[0]
		for _, word := range words[1:] {
			// Check if adding this word would exceed width
			if len(currentLine)+1+len(word) > width {
				result.WriteString(currentLine)
				result.WriteString("\n")
				currentLine = word
			} else {
				currentLine += " " + word
			}
		}
		result.WriteString(currentLine)
	}

	return result.String()
}

func sanitizeInlineText(text string) string {
	if text == "" {
		return ""
	}
	replacer := strings.NewReplacer("\n", " ", "\r", " ", "\t", " ")
	clean := strings.TrimSpace(replacer.Replace(text))
	if clean == "" {
		return ""
	}
	return strings.Join(strings.Fields(clean), " ")
}

func truncateInlineText(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	clean := sanitizeInlineText(text)
	if clean == "" {
		return ""
	}
	if runewidth.StringWidth(clean) <= maxWidth {
		return clean
	}
	if maxWidth <= runewidth.StringWidth("...") {
		return runewidth.Truncate(clean, maxWidth, "")
	}
	return runewidth.Truncate(clean, maxWidth, "...")
}
