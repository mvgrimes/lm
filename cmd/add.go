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

var (
	addCategory     string
	addTags         string
	addType         string
	addTaskName     string
	addActivityName string
)

var addCmd = &cobra.Command{
	Use:   "add [url...]",
	Short: "Add one or more links from the command line",
	Long: `Fetch URLs, optionally summarise with AI, and save to the database.
URLs may be provided as arguments or piped via stdin (one per line).

  --type link (default)   Save as a standalone link.
  --type task             Create (or find) a task and associate this link.
  --type activity         Create (or find) an activity and associate this link.`,
	Args: cobra.ArbitraryArgs,
	RunE: runAdd,
}

func init() {
	addCmd.Flags().StringVarP(&addCategory, "category", "c", "", "Category to assign (created if it does not exist)")
	addCmd.Flags().StringVarP(&addTags, "tags", "t", "", "Comma-separated tags to assign (created if they do not exist)")
	addCmd.Flags().StringVar(&addType, "type", "link", "Association type: link, task, or activity")
	addCmd.Flags().StringVar(&addTaskName, "task-name", "", "Task name when --type task (defaults to the page title)")
	addCmd.Flags().StringVar(&addActivityName, "activity-name", "", "Activity name when --type activity (defaults to the page title)")
	rootCmd.AddCommand(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Validate --type
	switch addType {
	case "link", "task", "activity":
	default:
		return fmt.Errorf("invalid --type %q: must be link, task, or activity", addType)
	}

	// Load env / config
	if dir, err := configDir(); err == nil {
		_ = loadEnvFile(dir)
	}

	dbPath := dbPathFromEnv()
	db := database.New(dbPath)
	defer db.Close()

	apiKey := apiKeyFromEnv()

	fetcher := services.NewFetcher()
	extractor := services.NewExtractor()
	var summarizer *services.Summarizer
	if apiKey != "" {
		summarizer = services.NewSummarizer(apiKey)
	}

	// Collect URLs: positional args first, then stdin if it is a pipe.
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

	// Process each URL, accumulating token usage across all of them.
	var grandInputTok, grandOutputTok int
	var processed, skipped int
	multi := len(urls) > 1

	for i, url := range urls {
		if multi {
			fmt.Printf("\n[%d/%d] %s\n", i+1, len(urls), url)
		}
		inTok, outTok, err := addURL(ctx, db, fetcher, extractor, summarizer, url)
		grandInputTok += inTok
		grandOutputTok += outTok
		if err != nil {
			slog.Error("failed to add URL", "url", url, "error", err)
			skipped++
			continue
		}
		processed++
	}

	// Grand-total summary.
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

// addURL fetches, extracts, summarises, and saves a single URL.
// It returns the number of LLM input and output tokens consumed.
func addURL(ctx context.Context, db *database.Database, fetcher *services.Fetcher, extractor *services.Extractor, summarizer *services.Summarizer, url string) (inputTok, outputTok int, err error) {
	fmt.Printf("Fetching %s ...\n", url)

	// Skip duplicates.
	existing, err := db.Queries.GetLinkByURL(ctx, url)
	if err == nil {
		fmt.Printf("Already exists (id=%d): %s\n", existing.ID, existing.Title.String)
		return 0, 0, nil
	}

	html, err := fetcher.FetchURL(ctx, url)
	if err != nil {
		return 0, 0, fmt.Errorf("fetch failed: %w", err)
	}

	fmt.Println("Extracting content ...")
	title, text, err := extractor.ExtractText(html, url)
	if err != nil {
		return 0, 0, fmt.Errorf("extraction failed: %w", err)
	}
	content := extractor.TruncateText(text, 10000)

	var summary, suggestedCat string
	var suggestedTags []string

	if summarizer != nil {
		fmt.Println("Summarising ...")
		var inTok, outTok int

		summary, inTok, outTok, _ = summarizer.Summarize(ctx, title, text)
		inputTok += inTok
		outputTok += outTok

		suggestedCat, suggestedTags, inTok, outTok, _ = summarizer.SuggestMetadata(ctx, title, text)
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
	}

	// Save link.
	link, err := db.Queries.CreateLink(ctx, models.CreateLinkParams{
		Url:     url,
		Title:   sql.NullString{String: title, Valid: title != ""},
		Content: sql.NullString{String: content, Valid: content != ""},
		Summary: sql.NullString{String: summary, Valid: summary != ""},
		Status:  "read_later",
	})
	if err != nil {
		return inputTok, outputTok, fmt.Errorf("failed to save link: %w", err)
	}

	fmt.Printf("Saved: [%d] %s\n", link.ID, link.Title.String)

	// Category: flag value takes priority over AI suggestion.
	catName := strings.TrimSpace(addCategory)
	if catName == "" {
		catName = strings.TrimSpace(suggestedCat)
	}
	if catName != "" {
		cat, catErr := db.Queries.GetCategoryByName(ctx, catName)
		if catErr != nil {
			cat, catErr = db.Queries.CreateCategory(ctx, models.CreateCategoryParams{
				Name:        catName,
				Description: sql.NullString{Valid: false},
			})
			if catErr != nil {
				slog.Warn("could not create category", "name", catName, "error", catErr)
			}
		}
		if catErr == nil {
			_ = db.Queries.LinkCategory(ctx, models.LinkCategoryParams{LinkID: link.ID, CategoryID: cat.ID})
			fmt.Printf("Category: %s\n", cat.Name)
		}
	}

	// Tags: flag value takes priority over AI suggestion.
	tagList := parseTags(addTags)
	if len(tagList) == 0 {
		tagList = suggestedTags
	}
	for _, tagName := range tagList {
		if tagName == "" {
			continue
		}
		t, tagErr := db.Queries.GetTagByName(ctx, tagName)
		if tagErr != nil {
			t, tagErr = db.Queries.CreateTag(ctx, tagName)
			if tagErr != nil {
				slog.Warn("could not create tag", "name", tagName, "error", tagErr)
				continue
			}
		}
		_ = db.Queries.LinkTag(ctx, models.LinkTagParams{LinkID: link.ID, TagID: t.ID})
	}
	if len(tagList) > 0 {
		fmt.Printf("Tags: %s\n", strings.Join(tagList, ", "))
	}

	// Task / Activity association.
	switch addType {
	case "task":
		taskName := strings.TrimSpace(addTaskName)
		if taskName == "" {
			taskName = title
		}
		if taskName == "" {
			taskName = url
		}
		task, taskErr := db.Queries.CreateTask(ctx, models.CreateTaskParams{
			Name:        taskName,
			Description: sql.NullString{Valid: false},
		})
		if taskErr != nil {
			slog.Warn("could not create task", "name", taskName, "error", taskErr)
		} else {
			_ = db.Queries.LinkTask(ctx, models.LinkTaskParams{LinkID: link.ID, TaskID: task.ID})
			fmt.Printf("Task: %s (id=%d)\n", task.Name, task.ID)
		}

	case "activity":
		actName := strings.TrimSpace(addActivityName)
		if actName == "" {
			actName = title
		}
		if actName == "" {
			actName = url
		}
		activity, actErr := db.Queries.CreateActivity(ctx, models.CreateActivityParams{
			Name:        actName,
			Description: sql.NullString{Valid: false},
		})
		if actErr != nil {
			slog.Warn("could not create activity", "name", actName, "error", actErr)
		} else {
			_ = db.Queries.LinkActivity(ctx, models.LinkActivityParams{LinkID: link.ID, ActivityID: activity.ID})
			fmt.Printf("Activity: %s (id=%d)\n", activity.Name, activity.ID)
		}
	}

	if summary != "" {
		fmt.Printf("\nSummary: %s\n", summary)
	}

	return inputTok, outputTok, nil
}

func parseTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var out []string
	for _, p := range parts {
		t := strings.ToLower(strings.TrimSpace(p))
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
