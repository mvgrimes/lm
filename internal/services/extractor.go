package services

import (
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

type Extractor struct{}

func NewExtractor() *Extractor {
	return &Extractor{}
}

// ExtractText parses HTML content and extracts readable text
func (e *Extractor) ExtractText(html string) (title string, text string, err error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract title
	title = doc.Find("title").First().Text()
	title = strings.TrimSpace(title)

	// Remove script and style elements
	doc.Find("script, style, nav, header, footer, aside").Each(func(i int, s *goquery.Selection) {
		s.Remove()
	})

	// Extract text from main content areas
	var textParts []string

	// Try to find main content area
	mainContent := doc.Find("article, main, .content, #content, .post, .entry-content")
	if mainContent.Length() > 0 {
		mainContent.Each(func(i int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			if text != "" {
				textParts = append(textParts, text)
			}
		})
	} else {
		// Fallback to paragraphs
		doc.Find("p, h1, h2, h3, h4, h5, h6, li").Each(func(i int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			if text != "" {
				textParts = append(textParts, text)
			}
		})
	}

	// Join all text parts
	text = strings.Join(textParts, "\n\n")

	// Clean up extra whitespace and collapse to readable format
	text = e.CollapseWhitespace(text)
	text = strings.TrimSpace(text)

	return title, text, nil
}

// TruncateText truncates text to a maximum length
func (e *Extractor) TruncateText(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}

	// Try to truncate at a word boundary
	truncated := text[:maxLength]
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > maxLength/2 {
		truncated = truncated[:lastSpace]
	}

	return truncated + "..."
}

// CollapseWhitespace reduces multiple whitespace to single spaces while preserving paragraphs
func (e *Extractor) CollapseWhitespace(text string) string {
	// Split into paragraphs (separated by multiple newlines)
	paragraphs := strings.Split(text, "\n\n")

	var cleaned []string
	for _, para := range paragraphs {
		// For each paragraph, collapse all whitespace to single spaces
		fields := strings.Fields(para) // Splits on any whitespace
		if len(fields) > 0 {
			cleaned = append(cleaned, strings.Join(fields, " "))
		}
	}

	// Join paragraphs with single empty line (two newlines)
	return strings.Join(cleaned, "\n\n")
}
