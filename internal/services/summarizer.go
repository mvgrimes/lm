package services

import (
	"context"
	"fmt"

	"github.com/sashabaranov/go-openai"
)

type Summarizer struct {
	client *openai.Client
}

func NewSummarizer(apiKey string) *Summarizer {
	return &Summarizer{
		client: openai.NewClient(apiKey),
	}
}

// Summarize generates a summary of the given text using OpenAI
func (s *Summarizer) Summarize(ctx context.Context, title, text string) (string, error) {
	if s.client == nil {
		return "", fmt.Errorf("OpenAI client not configured")
	}

	// Truncate text if too long (GPT-4 has limits)
	maxLength := 8000
	if len(text) > maxLength {
		text = text[:maxLength] + "..."
	}

	prompt := fmt.Sprintf("Please provide a concise summary (2-3 sentences) of the following web page:\n\nTitle: %s\n\nContent:\n%s", title, text)

	resp, err := s.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: openai.GPT4oMini,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a helpful assistant that summarizes web content concisely.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens:   200,
			Temperature: 0.7,
		},
	)

	if err != nil {
		return "", fmt.Errorf("failed to generate summary: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no summary generated")
	}

	return resp.Choices[0].Message.Content, nil
}

// SuggestMetadata generates suggested category and tags for the given content
func (s *Summarizer) SuggestMetadata(ctx context.Context, title, text string) (category string, tags []string, err error) {
	if s.client == nil {
		return "", nil, fmt.Errorf("OpenAI client not configured")
	}

	// Truncate text if too long
	maxLength := 6000
	if len(text) > maxLength {
		text = text[:maxLength] + "..."
	}

	prompt := fmt.Sprintf(`Analyze the following web page and suggest:
1. A single category (e.g., "Technology", "Business", "Health", "Education", etc.)
2. 3-5 relevant tags (comma-separated, lowercase)

Title: %s

Content:
%s

Respond in the format:
Category: <category>
Tags: <tag1>, <tag2>, <tag3>`, title, text)

	resp, err := s.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: openai.GPT4oMini,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "You are a helpful assistant that categorizes and tags web content. Always respond in the exact format requested.",
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			MaxTokens:   150,
			Temperature: 0.5,
		},
	)

	if err != nil {
		return "", nil, fmt.Errorf("failed to generate suggestions: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", nil, fmt.Errorf("no suggestions generated")
	}

	// Parse the response
	response := resp.Choices[0].Message.Content
	return parseMetadataResponse(response)
}

// parseMetadataResponse parses the LLM response to extract category and tags
func parseMetadataResponse(response string) (category string, tags []string, err error) {
	lines := []string{}
	for _, line := range []rune(response) {
		if line == '\n' {
			lines = append(lines, "")
		} else if len(lines) == 0 {
			lines = append(lines, string(line))
		} else {
			lines[len(lines)-1] += string(line)
		}
	}

	for _, line := range lines {
		if len(line) > 10 && line[:9] == "Category:" {
			category = line[9:]
			// Trim whitespace
			for len(category) > 0 && (category[0] == ' ' || category[0] == '\t') {
				category = category[1:]
			}
			for len(category) > 0 && (category[len(category)-1] == ' ' || category[len(category)-1] == '\t') {
				category = category[:len(category)-1]
			}
		} else if len(line) > 5 && line[:5] == "Tags:" {
			tagsStr := line[5:]
			// Trim whitespace
			for len(tagsStr) > 0 && (tagsStr[0] == ' ' || tagsStr[0] == '\t') {
				tagsStr = tagsStr[1:]
			}
			// Split by comma
			tag := ""
			for _, char := range tagsStr {
				if char == ',' {
					// Trim tag and add
					for len(tag) > 0 && (tag[0] == ' ' || tag[0] == '\t') {
						tag = tag[1:]
					}
					for len(tag) > 0 && (tag[len(tag)-1] == ' ' || tag[len(tag)-1] == '\t') {
						tag = tag[:len(tag)-1]
					}
					if tag != "" {
						tags = append(tags, tag)
					}
					tag = ""
				} else {
					tag += string(char)
				}
			}
			// Add last tag
			for len(tag) > 0 && (tag[0] == ' ' || tag[0] == '\t') {
				tag = tag[1:]
			}
			for len(tag) > 0 && (tag[len(tag)-1] == ' ' || tag[len(tag)-1] == '\t') {
				tag = tag[:len(tag)-1]
			}
			if tag != "" {
				tags = append(tags, tag)
			}
		}
	}

	if category == "" {
		category = "General"
	}
	if len(tags) == 0 {
		tags = []string{"uncategorized"}
	}

	return category, tags, nil
}
