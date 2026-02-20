package cmd

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"mccwk.com/lm/internal/database"
	"mccwk.com/lm/internal/models"
	"mccwk.com/lm/internal/services"
)

var refetchCmd = &cobra.Command{
	Use:   "refetch [url...]",
	Short: "Re-fetch, re-extract, and re-summarise existing links",
	Long: `Re-fetch content for URLs that already exist in the database.

Fetches fresh HTML, converts it to Markdown, and (if an API key is
configured) generates a new AI summary. The link's title, content, and
summary are updated in-place; tags, categories, and status are preserved.

URLs may be provided as arguments or piped via stdin (one per line).`,
	Args: cobra.ArbitraryArgs,
	RunE: runRefetch,
}

func init() {
	rootCmd.AddCommand(refetchCmd)
}

func runRefetch(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if dir, err := configDir(); err == nil {
		_ = loadEnvFile(dir)
	}

	db := database.New(dbPathFromEnv())
	defer db.Close()

	apiKey := apiKeyFromEnv()
	fetcher := services.NewFetcher()
	extractor := services.NewExtractor()
	var summarizer *services.Summarizer
	if apiKey != "" {
		summarizer = services.NewSummarizer(apiKey)
	}

	// Collect URLs from args and stdin.
	urls := append([]string(nil), args...)
	stat, _ := os.Stdin.Stat()
	if stat.Mode()&os.ModeCharDevice == 0 {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" && !strings.HasPrefix(line, "#") {
				urls = append(urls, line)
			}
		}
	}

	if len(urls) == 0 {
		return fmt.Errorf("no URLs provided: pass as arguments or pipe via stdin")
	}

	var grandInputTok, grandOutputTok int
	var processed, skipped int
	multi := len(urls) > 1

	for i, url := range urls {
		if multi {
			fmt.Printf("\n[%d/%d] %s\n", i+1, len(urls), url)
		}
		inTok, outTok, err := refetchURL(ctx, db, fetcher, extractor, summarizer, url)
		grandInputTok += inTok
		grandOutputTok += outTok
		if err != nil {
			slog.Error("failed to refetch URL", "url", url, "error", err)
			skipped++
			continue
		}
		processed++
	}

	if multi {
		fmt.Printf("\n--- Summary ---\n")
		fmt.Printf("Processed: %d  Skipped: %d\n", processed, skipped)
	}

	if grandInputTok+grandOutputTok > 0 {
		cost := float64(grandInputTok)*0.15/1_000_000.0 +
			float64(grandOutputTok)*0.60/1_000_000.0
		slog.Info("LLM usage total",
			"input_tokens", grandInputTok,
			"output_tokens", grandOutputTok,
			"cost_usd", fmt.Sprintf("$%.5f", cost),
		)
		if multi {
			fmt.Printf("LLM cost:  $%.5f  (%d in + %d out tokens)\n", cost, grandInputTok, grandOutputTok)
		}
	}

	return nil
}

func refetchURL(ctx context.Context, db *database.Database, fetcher *services.Fetcher, extractor *services.Extractor, summarizer *services.Summarizer, url string) (inputTok, outputTok int, err error) {
	existing, err := db.Queries.GetLinkByURL(ctx, url)
	if err != nil {
		return 0, 0, fmt.Errorf("URL not found in database (use 'lm add' to add it first): %s", url)
	}

	fmt.Printf("Fetching %s ...\n", url)
	html, err := fetcher.FetchURL(ctx, url)
	if err != nil {
		return 0, 0, fmt.Errorf("fetch failed: %w", err)
	}
	_ = db.Queries.UpdateLinkFetchedAt(ctx, existing.ID)

	fmt.Println("Extracting content ...")
	title, text, err := extractor.ExtractText(html, url)
	if err != nil {
		return 0, 0, fmt.Errorf("extraction failed: %w", err)
	}
	content := extractor.TruncateText(text, 10000)

	var summary string
	if summarizer != nil {
		fmt.Println("Summarising ...")
		var inTok, outTok int
		summary, inTok, outTok, _ = summarizer.Summarize(ctx, title, text)
		inputTok += inTok
		outputTok += outTok

		if inputTok+outputTok > 0 {
			cost := float64(inputTok)*0.15/1_000_000.0 +
				float64(outputTok)*0.60/1_000_000.0
			slog.Info("LLM usage",
				"url", url,
				"input_tokens", inputTok,
				"output_tokens", outputTok,
				"cost_usd", fmt.Sprintf("$%.5f", cost),
			)
		}
		_ = db.Queries.UpdateLinkSummarizedAt(ctx, existing.ID)
	}

	_, err = db.Queries.UpdateLink(ctx, models.UpdateLinkParams{
		ID:      existing.ID,
		Title:   sql.NullString{String: title, Valid: title != ""},
		Content: sql.NullString{String: content, Valid: content != ""},
		Summary: sql.NullString{String: summary, Valid: summary != ""},
		Status:  existing.Status,
	})
	if err != nil {
		return inputTok, outputTok, fmt.Errorf("failed to update link: %w", err)
	}

	fmt.Printf("Updated: [%d] %s\n", existing.ID, title)
	if summary != "" {
		fmt.Printf("\nSummary: %s\n", summary)
	}

	return inputTok, outputTok, nil
}
