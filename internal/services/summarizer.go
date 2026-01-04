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
