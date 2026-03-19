// Package main is the entry point for the oxy CLI.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alenicomar/oxy-backup/internal/config"
	"github.com/alenicomar/oxy-backup/internal/exitcode"
	"github.com/alenicomar/oxy-backup/internal/logging"
	"github.com/spf13/cobra"

	"log/slog"
)

var (
	cfgPath   string
	logFormat string
	logLevel  string
	dryRun    bool

	// Shared state set by PersistentPreRunE.
	cfg    *config.Config
	logger *slog.Logger
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(exitcode.ConfigError)
	}
}

var rootCmd = &cobra.Command{
	Use:   "oxy",
	Short: "PostgreSQL backup & restore with Git-based version control",
	Long: `oxy automates PostgreSQL database backups via pg_dump,
partitions the output into manageable chunks for efficient Git storage,
commits the results, and supports point-in-time restore.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Set up logging first (use flag values, then override from config if loaded).
		var err error
		logger, err = logging.Setup(logLevel, logFormat)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return err
		}

		// Load config.
		cfg, err = config.Load(cfgPath, logger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
			return err
		}

		// Override logging from config if flags were not explicitly set.
		if !cmd.Flags().Changed("log-level") && cfg.Logging.Level != "" {
			logLevel = cfg.Logging.Level
		}
		if !cmd.Flags().Changed("log-format") && cfg.Logging.Format != "" {
			logFormat = cfg.Logging.Format
		}

		// Re-setup logger with final values.
		logger, err = logging.Setup(logLevel, logFormat)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "oxy.yaml", "path to config file")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "text", "log format (text|json)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug|info|warn|error)")
	rootCmd.PersistentFlags().BoolVar(&dryRun, "dry-run", false, "validate config and show what would be done without executing")
}
