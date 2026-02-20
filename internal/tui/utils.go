package tui

import "strings"

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
