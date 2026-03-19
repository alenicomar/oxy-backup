package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/backup-lite/backup-lite/internal/config"
	"github.com/backup-lite/backup-lite/internal/exitcode"
	"github.com/backup-lite/backup-lite/internal/git"
	"github.com/backup-lite/backup-lite/internal/postgres"
	"github.com/backup-lite/backup-lite/internal/restore"
	"github.com/spf13/cobra"
)

var restoreTimestamp string

func init() {
	restoreCmd.Flags().StringVar(&restoreTimestamp, "timestamp", "", "backup timestamp to restore (e.g. 20240115-143022)")
	rootCmd.AddCommand(restoreCmd)
}

var restoreCmd = &cobra.Command{
	Use:   "restore <database-name>",
	Short: "Restore a database from a backup",
	Long: `Reassembles partitioned backup files and loads them into PostgreSQL via psql.
Requires a database name and a --timestamp flag.
If --timestamp is omitted, available timestamps are listed.`,
	Args: cobra.ExactArgs(1),
	RunE: runRestore,
}

func runRestore(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	dbName := args[0]

	dbCfg, err := findDatabase(dbName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitcode.ConfigError)
	}

	// Dry-run: validate config and show what would be done.
	if dryRun {
		return dryRunRestore(dbCfg, dbName)
	}

	// If no timestamp provided, list available timestamps.
	if restoreTimestamp == "" {
		timestamps, listErr := restore.ListTimestamps(*dbCfg)
		if listErr != nil {
			fmt.Fprintf(os.Stderr, "error listing timestamps: %v\n", listErr)
			os.Exit(exitcode.RestoreError)
		}
		if len(timestamps) == 0 {
			fmt.Fprintf(os.Stderr, "no backups found for database %q\n", dbName)
			os.Exit(exitcode.RestoreError)
		}
		fmt.Fprintf(os.Stdout, "Available timestamps for %q:\n", dbName)
		for _, ts := range timestamps {
			fmt.Fprintf(os.Stdout, "  %s\n", ts)
		}
		fmt.Fprintf(os.Stderr, "\nUse --timestamp to select one.\n")
		os.Exit(exitcode.ConfigError)
		return nil
	}

	pgExec := &postgres.ExecPgExecutor{Logger: logger}
	gitClient := &git.ExecGitClient{
		WorkDir: ".",
		Remote:  cfg.Git.Remote,
		Branch:  cfg.Git.Branch,
		Logger:  logger,
	}

	svc := &restore.Service{
		PgExecutor: pgExec,
		GitClient:  gitClient,
		Logger:     logger,
	}

	if err := svc.Run(ctx, *dbCfg, restoreTimestamp); err != nil {
		fmt.Fprintf(os.Stderr, "restore failed: %v\n", err)
		os.Exit(mapRestoreExitCode(err))
	}

	return nil
}

func dryRunRestore(dbCfg *config.DatabaseConfig, dbName string) error {
	fmt.Fprintf(os.Stdout, "[dry-run] Config validated successfully (%s)\n", cfgPath)
	fmt.Fprintf(os.Stdout, "[dry-run] Restore target: %s (mode=%s, database=%s)\n",
		dbName, dbCfg.Mode, dbCfg.Database)

	if restoreTimestamp != "" {
		fmt.Fprintf(os.Stdout, "[dry-run] Timestamp: %s\n", restoreTimestamp)
		fmt.Fprintf(os.Stdout, "[dry-run] Would: validate manifest → reassemble partitions → psql --single-transaction → cleanup → git restore\n")
	} else {
		timestamps, err := restore.ListTimestamps(*dbCfg)
		if err != nil {
			fmt.Fprintf(os.Stdout, "[dry-run] Could not list timestamps: %v\n", err)
		} else {
			fmt.Fprintf(os.Stdout, "[dry-run] Available timestamps: %d\n", len(timestamps))
			for _, ts := range timestamps {
				fmt.Fprintf(os.Stdout, "[dry-run]   - %s\n", ts)
			}
		}
	}
	return nil
}

func mapRestoreExitCode(err error) int {
	if err == nil {
		return exitcode.OK
	}

	msg := err.Error()

	switch {
	case strings.Contains(msg, "database load failed"), strings.Contains(msg, "psql"):
		return exitcode.RestoreError
	case strings.Contains(msg, "assembly failed"):
		return exitcode.RestoreError
	case strings.Contains(msg, "manifest"), strings.Contains(msg, "partition count mismatch"):
		return exitcode.RestoreError
	case strings.Contains(msg, "git"):
		return exitcode.GitError
	default:
		return exitcode.RestoreError
	}
}
