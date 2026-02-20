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

const VERSION = "0.1.1"

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
	if dir, err := configDir(); err == nil {
		_ = loadEnvFile(dir)
	}

	db := database.New(dbPathFromEnv())
	defer db.Close()

	model := tui.NewModel(db, apiKeyFromEnv())
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		slog.Error("TUI error", "error", err)
		os.Exit(1)
	}
}

// loadEnvFile loads the .env file from the given config directory.
func loadEnvFile(dir string) error {
	return godotenv.Load(filepath.Join(dir, ".env"))
}

// dbPathFromEnv returns the database path from the DB_PATH env var or the default location.
func dbPathFromEnv() string {
	if path := os.Getenv("DB_PATH"); path != "" {
		return path
	}
	dir, err := configDir()
	if err != nil {
		slog.Error("failed to get config directory", "error", err)
		os.Exit(1)
	}
	return filepath.Join(dir, "lm.db")
}

// apiKeyFromEnv returns the OpenAI API key from the environment.
func apiKeyFromEnv() string {
	return os.Getenv("OPENAI_API_KEY")
}
