package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/alenicomar/oxy-backup/internal/config"
	"github.com/alenicomar/oxy-backup/internal/exitcode"
	"github.com/alenicomar/oxy-backup/internal/git"
	"github.com/alenicomar/oxy-backup/internal/postgres"
	"github.com/alenicomar/oxy-backup/internal/restore"
	"github.com/spf13/cobra"
)

var restoreCommit string

func init() {
	restoreCmd.Flags().StringVar(&restoreCommit, "commit", "", "commit SHA to restore (e.g. abc1234)")
	rootCmd.AddCommand(restoreCmd)
}

var restoreCmd = &cobra.Command{
	Use:   "restore <database-name>",
	Short: "Restore a database from a backup",
	Long: `Reassembles partitioned backup files and loads them into PostgreSQL via psql.
Requires a database name and a --commit flag.
If --commit is omitted, available backup commits are listed.`,
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

	gitClient := git.NewClient(cfg.Git, ".", logger)

	// If no commit provided, list available backups.
	if restoreCommit == "" {
		commits, listErr := restore.ListBackups(ctx, gitClient, *dbCfg, 0)
		if listErr != nil {
			fmt.Fprintf(os.Stderr, "error listing backups: %v\n", listErr)
			os.Exit(exitcode.RestoreError)
		}
		if len(commits) == 0 {
			fmt.Fprintf(os.Stderr, "no backups found for database %q\n", dbName)
			os.Exit(exitcode.RestoreError)
		}
		fmt.Fprintf(os.Stdout, "Available backups for %q:\n", dbName)
		for _, c := range commits {
			fmt.Fprintf(os.Stdout, "  %s  %s  %s\n",
				c.ShortSHA,
				c.Date.Format("2006-01-02 15:04"),
				c.Message,
			)
		}
		fmt.Fprintf(os.Stderr, "\nUse --commit <sha> to select one.\n")
		os.Exit(exitcode.ConfigError)
		return nil
	}

	pgExec := &postgres.ExecPgExecutor{Logger: logger}

	svc := &restore.Service{
		PgExecutor: pgExec,
		GitClient:  gitClient,
		Logger:     logger,
	}

	if err := svc.Run(ctx, *dbCfg, restoreCommit); err != nil {
		fmt.Fprintf(os.Stderr, "restore failed: %v\n", err)
		os.Exit(mapRestoreExitCode(err))
	}

	return nil
}

func dryRunRestore(dbCfg *config.DatabaseConfig, dbName string) error {
	fmt.Fprintf(os.Stdout, "[dry-run] Config validated successfully (%s)\n", cfgPath)
	fmt.Fprintf(os.Stdout, "[dry-run] Restore target: %s (mode=%s, database=%s)\n",
		dbName, dbCfg.Mode, dbCfg.Database)

	if restoreCommit != "" {
		fmt.Fprintf(os.Stdout, "[dry-run] Commit: %s\n", restoreCommit)
		fmt.Fprintf(os.Stdout, "[dry-run] Would: checkout files from commit → validate manifest → reassemble partitions → psql --single-transaction → cleanup → git restore HEAD\n")
	} else {
		gitClient := git.NewClient(cfg.Git, ".", logger)
		commits, err := restore.ListBackups(rootCmd.Context(), gitClient, *dbCfg, 0)
		if err != nil {
			fmt.Fprintf(os.Stdout, "[dry-run] Could not list backups: %v\n", err)
		} else {
			fmt.Fprintf(os.Stdout, "[dry-run] Available backups: %d\n", len(commits))
			for _, c := range commits {
				fmt.Fprintf(os.Stdout, "[dry-run]   - %s  %s  %s\n",
					c.ShortSHA,
					c.Date.Format("2006-01-02 15:04"),
					c.Message,
				)
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
