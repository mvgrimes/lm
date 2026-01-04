package cmd

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"log/slog"
)

const VERSION = "1.0.0"

var (
	debug bool
)

var rootCmd = &cobra.Command{
	Use:   "lk",
	Short: "Link manager",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("no subcommand specified")
		// TODO: list commands, verify ENV vars
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
	level := slog.LevelInfo
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
