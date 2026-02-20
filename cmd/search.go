package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"mccwk.com/lm/internal/database"
	"mccwk.com/lm/internal/models"
)

var (
	searchCategory string
	searchTags     string
	searchType     string
)

var searchCmd = &cobra.Command{
	Use:   "search <text>",
	Short: "Search links from the command line",
	Long: `Search for links stored in the database.

  --category <name>   Filter to links in the named category.
  --tags <t1,t2>      Filter to links that have ALL of the listed tags.
  --type link|task|activity
                      Filter by association:
                        link     – standalone links (not in a task or activity)
                        task     – links associated with at least one task
                        activity – links associated with at least one activity`,
	Args: cobra.ExactArgs(1),
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().StringVarP(&searchCategory, "category", "c", "", "Filter by category name")
	searchCmd.Flags().StringVarP(&searchTags, "tags", "t", "", "Filter by comma-separated tags (link must have all)")
	searchCmd.Flags().StringVar(&searchType, "type", "", "Filter by type: link, task, or activity")
	rootCmd.AddCommand(searchCmd)
}

func runSearch(cmd *cobra.Command, args []string) error {
	query := strings.TrimSpace(args[0])
	ctx := context.Background()

	if searchType != "" {
		switch searchType {
		case "link", "task", "activity":
		default:
			return fmt.Errorf("invalid --type %q: must be link, task, or activity", searchType)
		}
	}

	// Load env / config
	if dir, err := configDir(); err == nil {
		_ = loadEnvFile(dir)
	}

	db := database.New(dbPathFromEnv())
	defer db.Close()

	// Fetch matching links
	pattern := "%" + query + "%"
	links, err := db.Queries.SearchLinks(ctx, models.SearchLinksParams{
		Url:     pattern,
		Title:   sql.NullString{String: pattern, Valid: true},
		Content: sql.NullString{String: pattern, Valid: true},
		Summary: sql.NullString{String: pattern, Valid: true},
		Limit:   100,
		Offset:  0,
	})
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	// Apply category filter
	if searchCategory != "" {
		cat, err := db.Queries.GetCategoryByName(ctx, searchCategory)
		if err != nil {
			fmt.Printf("Category %q not found.\n", searchCategory)
			return nil
		}
		catLinks, err := db.Queries.GetLinksForCategory(ctx, cat.ID)
		if err != nil {
			return fmt.Errorf("category lookup failed: %w", err)
		}
		catIDs := make(map[int64]struct{}, len(catLinks))
		for _, l := range catLinks {
			catIDs[l.ID] = struct{}{}
		}
		filtered := links[:0]
		for _, l := range links {
			if _, ok := catIDs[l.ID]; ok {
				filtered = append(filtered, l)
			}
		}
		links = filtered
	}

	// Apply tag filter
	wantTags := parseTags(searchTags)
	if len(wantTags) > 0 {
		filtered := links[:0]
		for _, l := range links {
			if linkHasAllTags(ctx, db, l.ID, wantTags) {
				filtered = append(filtered, l)
			}
		}
		links = filtered
	}

	// Apply type filter
	if searchType != "" {
		filtered := links[:0]
		for _, l := range links {
			match, err := linkMatchesType(ctx, db, l.ID, searchType)
			if err == nil && match {
				filtered = append(filtered, l)
			}
		}
		links = filtered
	}

	if len(links) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	fmt.Printf("Found %d result(s):\n\n", len(links))
	for i, l := range links {
		title := l.Title.String
		if title == "" {
			title = l.Url
		}
		fmt.Printf("%d. %s\n", i+1, title)
		fmt.Printf("   %s\n", l.Url)
		if l.Summary.Valid && l.Summary.String != "" {
			fmt.Printf("   %s\n", truncate(l.Summary.String, 120))
		}
		fmt.Println()
	}

	return nil
}

func linkHasAllTags(ctx context.Context, db *database.Database, linkID int64, wantTags []string) bool {
	linkTags, err := db.Queries.GetTagsForLink(ctx, linkID)
	if err != nil {
		return false
	}
	have := make(map[string]struct{}, len(linkTags))
	for _, t := range linkTags {
		have[strings.ToLower(t.Name)] = struct{}{}
	}
	for _, want := range wantTags {
		if _, ok := have[want]; !ok {
			return false
		}
	}
	return true
}

func linkMatchesType(ctx context.Context, db *database.Database, linkID int64, linkType string) (bool, error) {
	switch linkType {
	case "task":
		tasks, err := db.Queries.GetTasksForLink(ctx, linkID)
		if err != nil {
			return false, err
		}
		return len(tasks) > 0, nil
	case "activity":
		activities, err := db.Queries.GetActivitiesForLink(ctx, linkID)
		if err != nil {
			return false, err
		}
		return len(activities) > 0, nil
	case "link":
		tasks, err := db.Queries.GetTasksForLink(ctx, linkID)
		if err != nil {
			return false, err
		}
		if len(tasks) > 0 {
			return false, nil
		}
		activities, err := db.Queries.GetActivitiesForLink(ctx, linkID)
		if err != nil {
			return false, err
		}
		return len(activities) == 0, nil
	}
	return true, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
