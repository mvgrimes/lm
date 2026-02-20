package services

import (
	"fmt"
	"regexp"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/PuerkitoBio/goquery"
)

var (
	multipleBlankLines = regexp.MustCompile(`\n\p{Z}*(\n\p{Z}*)+\n`)
	// mdImage matches ![alt](url) — images must be replaced before links so
	// the image-inside-link pattern [![alt](img)](link) is handled correctly.
	mdImage = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	// mdLink matches [text](url)
	mdLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

type Extractor struct{}

func NewExtractor() *Extractor {
	return &Extractor{}
}

// ExtractText parses HTML content and returns the title and content as Markdown.
// The pageURL is used to resolve relative links to absolute URLs.
func (e *Extractor) ExtractText(html, pageURL string) (title string, text string, err error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract title
	title = strings.TrimSpace(doc.Find("title").First().Text())

	// Remove noisy structural elements; script/style are also handled by the
	// converter but removing them first keeps content selection cleaner.
	doc.Find("script, style, nav, header, footer, aside").Remove()

	// Prefer a focused content area; fall back to the whole body.
	var contentHTML string
	mainContent := doc.Find("article, main, [role=main], .content, #content, .post, .entry-content").First()
	if mainContent.Length() > 0 {
		contentHTML, err = mainContent.Html()
	} else {
		contentHTML, err = doc.Find("body").Html()
	}
	if err != nil {
		return "", "", fmt.Errorf("failed to extract content HTML: %w", err)
	}

	md, err := htmltomarkdown.ConvertString(contentHTML, converter.WithDomain(pageURL))
	if err != nil {
		return "", "", fmt.Errorf("failed to convert HTML to markdown: %w", err)
	}

	// fmt.Println(strings.ReplaceAll(strings.ReplaceAll(md, " ", "."), "\n", "\\n\n"))

	// Replace images with a short placeholder, keeping alt text when present.
	md = mdImage.ReplaceAllStringFunc(md, func(match string) string {
		if sub := mdImage.FindStringSubmatch(match); len(sub) > 1 && strings.TrimSpace(sub[1]) != "" {
			return "[image: " + strings.TrimSpace(sub[1]) + "]"
		}
		return "[image]"
	})
	// Strip link URLs, keeping the visible link text.
	md = mdLink.ReplaceAllString(md, "$1")

	text = strings.TrimSpace(multipleBlankLines.ReplaceAllString(md, "\n\n"))
	return title, text, nil
}

// TruncateText truncates text to a maximum length at a word boundary.
func (e *Extractor) TruncateText(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}

	truncated := text[:maxLength]
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > maxLength/2 {
		truncated = truncated[:lastSpace]
	}

	return truncated + "..."
}
