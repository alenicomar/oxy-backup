package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/alenicomar/oxy-backup/internal/backup"
	"github.com/alenicomar/oxy-backup/internal/config"
	"github.com/alenicomar/oxy-backup/internal/exitcode"
	"github.com/alenicomar/oxy-backup/internal/git"
	"github.com/alenicomar/oxy-backup/internal/postgres"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(backupCmd)
}

var backupCmd = &cobra.Command{
	Use:   "backup [database-name]",
	Short: "Back up one or all configured databases",
	Long: `Executes pg_dump, partitions the output, and commits to Git.
If no database name is provided, all configured databases are backed up sequentially.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBackup,
}

func runBackup(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Dry-run: validate config and show what would be done.
	if dryRun {
		return dryRunBackup(args)
	}

	pgExec := &postgres.ExecPgExecutor{Logger: logger}
	gitClient := git.NewClient(cfg.Git, ".", logger)

	svc := &backup.Service{
		PgExecutor: pgExec,
		GitClient:  gitClient,
		GitConfig:  cfg.Git,
		Logger:     logger,
	}

	if len(args) == 1 {
		// Specific database.
		dbName := args[0]
		dbCfg, err := findDatabase(dbName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(exitcode.ConfigError)
		}
		if err := svc.Run(ctx, *dbCfg); err != nil {
			fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
			os.Exit(mapBackupExitCode(err))
		}
		return nil
	}

	// All databases.
	if err := svc.RunAll(ctx, cfg.Databases); err != nil {
		var partial *backup.PartialError
		if errors.As(err, &partial) {
			fmt.Fprintf(os.Stderr, "partial failure: %v\n", err)
			os.Exit(exitcode.PartialFailure)
		}
		fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
		os.Exit(mapBackupExitCode(err))
	}

	return nil
}

func findDatabase(name string) (*config.DatabaseConfig, error) {
	for i := range cfg.Databases {
		if cfg.Databases[i].Name == name {
			return &cfg.Databases[i], nil
		}
	}
	return nil, fmt.Errorf("database %q not found in configuration", name)
}

func mapBackupExitCode(err error) int {
	if err == nil {
		return exitcode.OK
	}

	var partial *backup.PartialError
	if errors.As(err, &partial) {
		return exitcode.PartialFailure
	}

	msg := err.Error()

	switch {
	case contains(msg, "dump failed"):
		return exitcode.DumpError
	case contains(msg, "partitioning failed"), contains(msg, "invalid partition size"):
		return exitcode.PartitionError
	case contains(msg, "git"):
		return exitcode.GitError
	case contains(msg, "config"), contains(msg, "creating partition dir"):
		return exitcode.ConfigError
	default:
		return exitcode.DumpError
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func dryRunBackup(args []string) error {
	databases := cfg.Databases
	if len(args) == 1 {
		dbCfg, err := findDatabase(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(exitcode.ConfigError)
		}
		databases = []config.DatabaseConfig{*dbCfg}
	}

	fmt.Fprintf(os.Stdout, "[dry-run] Config validated successfully (%s)\n", cfgPath)
	fmt.Fprintf(os.Stdout, "[dry-run] Databases to backup: %d\n", len(databases))
	for _, db := range databases {
		partSize, _ := db.PartitionSizeBytes()
		fmt.Fprintf(os.Stdout, "[dry-run]   - %s (mode=%s, database=%s, partition_size=%d bytes)\n",
			db.Name, db.Mode, db.Database, partSize)
	}
	fmt.Fprintf(os.Stdout, "[dry-run] Git: remote=%s, branch=%s, auto_push=%v\n",
		cfg.Git.Remote, cfg.Git.Branch, cfg.Git.AutoPushEnabled())
	return nil
}
