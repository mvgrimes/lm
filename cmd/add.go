package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
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
	Use:   "add <url>",
	Short: "Add a link from the command line",
	Long: `Fetch a URL, optionally summarise it with AI, and save it to the database.

  --type link (default)   Save as a standalone link.
  --type task             Create (or find) a task and associate this link with it.
  --type activity         Create (or find) an activity and associate this link with it.`,
	Args: cobra.ExactArgs(1),
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
	url := args[0]
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

	// --- Stage 1: Fetch ---
	fmt.Printf("Fetching %s ...\n", url)

	// Check for duplicate
	existing, err := db.Queries.GetLinkByURL(ctx, url)
	if err == nil {
		fmt.Printf("Link already exists (id=%d): %s\n", existing.ID, existing.Title.String)
		return nil
	}

	html, err := fetcher.FetchURL(ctx, url)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	// --- Stage 2: Extract ---
	fmt.Println("Extracting content ...")
	title, text, err := extractor.ExtractText(html)
	if err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}
	content := extractor.TruncateText(text, 10000)

	// --- Stage 3: Summarise ---
	var summary, category string
	var tags []string
	var totalInputTok, totalOutputTok int

	if summarizer != nil {
		fmt.Println("Summarising ...")
		var inTok, outTok int
		summary, inTok, outTok, _ = summarizer.Summarize(ctx, title, text)
		totalInputTok += inTok
		totalOutputTok += outTok

		category, tags, inTok, outTok, _ = summarizer.SuggestMetadata(ctx, title, text)
		totalInputTok += inTok
		totalOutputTok += outTok
	}

	// Log LLM cost if applicable
	if totalInputTok+totalOutputTok > 0 {
		llmCost := float64(totalInputTok)*0.15/1_000_000.0 +
			float64(totalOutputTok)*0.60/1_000_000.0
		slog.Info("LLM usage",
			"input_tokens", totalInputTok,
			"output_tokens", totalOutputTok,
			"cost_usd", fmt.Sprintf("$%.5f", llmCost),
		)
	}

	// --- Stage 4: Save link ---
	link, err := db.Queries.CreateLink(ctx, models.CreateLinkParams{
		Url:     url,
		Title:   sql.NullString{String: title, Valid: title != ""},
		Content: sql.NullString{String: content, Valid: content != ""},
		Summary: sql.NullString{String: summary, Valid: summary != ""},
		Status:  "read_later",
	})
	if err != nil {
		return fmt.Errorf("failed to save link: %w", err)
	}

	fmt.Printf("Saved: [%d] %s\n", link.ID, link.Title.String)

	// --- Stage 5: Category ---
	catName := strings.TrimSpace(addCategory)
	if catName == "" {
		catName = strings.TrimSpace(category)
	}
	if catName != "" {
		cat, err := db.Queries.GetCategoryByName(ctx, catName)
		if err != nil {
			cat, err = db.Queries.CreateCategory(ctx, models.CreateCategoryParams{
				Name:        catName,
				Description: sql.NullString{Valid: false},
			})
			if err != nil {
				slog.Warn("could not create category", "name", catName, "error", err)
			}
		}
		if err == nil {
			_ = db.Queries.LinkCategory(ctx, models.LinkCategoryParams{LinkID: link.ID, CategoryID: cat.ID})
			fmt.Printf("Category: %s\n", cat.Name)
		}
	}

	// --- Stage 6: Tags ---
	tagList := parseTags(addTags)
	if len(tagList) == 0 {
		tagList = tags
	}
	for _, tagName := range tagList {
		if tagName == "" {
			continue
		}
		t, err := db.Queries.GetTagByName(ctx, tagName)
		if err != nil {
			t, err = db.Queries.CreateTag(ctx, tagName)
			if err != nil {
				slog.Warn("could not create tag", "name", tagName, "error", err)
				continue
			}
		}
		_ = db.Queries.LinkTag(ctx, models.LinkTagParams{LinkID: link.ID, TagID: t.ID})
	}
	if len(tagList) > 0 {
		fmt.Printf("Tags: %s\n", strings.Join(tagList, ", "))
	}

	// --- Stage 7: Task / Activity association ---
	switch addType {
	case "task":
		taskName := strings.TrimSpace(addTaskName)
		if taskName == "" {
			taskName = title
		}
		if taskName == "" {
			taskName = url
		}
		task, err := db.Queries.CreateTask(ctx, models.CreateTaskParams{
			Name:        taskName,
			Description: sql.NullString{Valid: false},
		})
		if err != nil {
			slog.Warn("could not create task", "name", taskName, "error", err)
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
		activity, err := db.Queries.CreateActivity(ctx, models.CreateActivityParams{
			Name:        actName,
			Description: sql.NullString{Valid: false},
		})
		if err != nil {
			slog.Warn("could not create activity", "name", actName, "error", err)
		} else {
			_ = db.Queries.LinkActivity(ctx, models.LinkActivityParams{LinkID: link.ID, ActivityID: activity.ID})
			fmt.Printf("Activity: %s (id=%d)\n", activity.Name, activity.ID)
		}
	}

	if summary != "" {
		fmt.Printf("\nSummary: %s\n", summary)
	}

	return nil
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
