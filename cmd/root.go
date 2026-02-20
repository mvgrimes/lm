package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"log/slog"

	"mccwk.com/lm/internal/database"
	"mccwk.com/lm/internal/tui"
)

const VERSION = "1.0.0"

var (
	debug bool
)

var rootCmd = &cobra.Command{
	Use:   "lm",
	Short: "Link manager",
	Run: func(cmd *cobra.Command, args []string) {
		startTUI()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// err := godotenv.Load()
	// if err != nil {
	// 	slog.Warn("unable to load .env file", "err", err)
	// }

	slog.Debug(fmt.Sprintf("Version: %s", VERSION))

	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Display debugging output")

	setupLogging()
}

func setupLogging() {
	// level := slog.LevelInfo
	level := slog.LevelDebug
	if debug {
		level = slog.LevelDebug
	}

	if os.Getenv("MODE") == "production" {
		logger := slog.New(slog.NewJSONHandler(os.Stdout,
			&slog.HandlerOptions{
				Level: level,
			}))
		slog.SetDefault(logger)
	} else {
		logger := slog.New(tint.NewHandler(os.Stdout,
			&tint.Options{
				Level: level,
			}))
		slog.SetDefault(logger)
	}
}

func configDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(homeDir, ".config", "lm")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func startTUI() {
	// Load .env file from config dir if it exists
	if dir, err := configDir(); err == nil {
		_ = godotenv.Load(filepath.Join(dir, ".env"))
	}

	// Get database path from environment or use default
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dir, err := configDir()
		if err != nil {
			slog.Error("failed to get config directory", "error", err)
			os.Exit(1)
		}
		dbPath = filepath.Join(dir, "lm.db")
	}

	// Get OpenAI API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")

	// Initialize database
	db := database.New(dbPath)
	defer db.Close()

	// Create and run TUI
	model := tui.NewModel(db, apiKey)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		slog.Error("TUI error", "error", err)
		os.Exit(1)
	}
}
