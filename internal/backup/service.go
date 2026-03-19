package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alenicomar/oxy-backup/internal/config"
	"github.com/alenicomar/oxy-backup/internal/git"
	"github.com/alenicomar/oxy-backup/internal/postgres"
)

// Service orchestrates the backup flow: dump → partition → git.
type Service struct {
	PgExecutor postgres.PgExecutor
	GitClient  git.GitClient
	GitConfig  config.GitConfig
	Logger     *slog.Logger
}

// Run executes the full backup pipeline for a single database.
func (s *Service) Run(ctx context.Context, dbCfg config.DatabaseConfig) error {
	var cleanupStack []func()
	cleanup := func() {
		for i := len(cleanupStack) - 1; i >= 0; i-- {
			cleanupStack[i]()
		}
	}

	// 1. Generate timestamp
	timestamp := time.Now().UTC().Format("20060102-150405")

	// 2. Create output directory
	partDir := filepath.Join(dbCfg.OutputDir, "partitions", dbCfg.Name, timestamp)
	if err := os.MkdirAll(partDir, 0755); err != nil {
		return fmt.Errorf("creating partition dir: %w", err)
	}
	cleanupStack = append(cleanupStack, func() {
		s.Logger.Debug("cleanup: removing partition dir", "dir", partDir)
		os.RemoveAll(partDir)
	})

	s.Logger.Info("starting backup",
		"database", dbCfg.Name,
		"mode", dbCfg.Mode,
		"timestamp", timestamp,
		"output_dir", partDir,
	)

	// 3. Check pg_dump version (best-effort, logs warning only)
	if execPg, ok := s.PgExecutor.(*postgres.ExecPgExecutor); ok {
		execPg.CheckVersion(ctx, dbCfg)
	}

	// 4. Execute pg_dump
	reader, err := s.PgExecutor.DumpDatabase(ctx, dbCfg)
	if err != nil {
		cleanup()
		return fmt.Errorf("dump failed: %w", err)
	}

	// 4. Partition the dump
	maxBytes, err := dbCfg.PartitionSizeBytes()
	if err != nil {
		cleanup()
		return fmt.Errorf("invalid partition size: %w", err)
	}

	partitioner := &Partitioner{
		MaxBytes: maxBytes,
		Logger:   s.Logger,
	}

	parts, err := partitioner.Split(ctx, reader, partDir)
	if err != nil {
		cleanup()
		return fmt.Errorf("partitioning failed: %w", err)
	}

	// 5. Write manifest
	if err := WriteManifest(partDir, dbCfg.Name, timestamp, parts); err != nil {
		cleanup()
		return fmt.Errorf("writing manifest: %w", err)
	}

	s.Logger.Info("backup partitioned successfully",
		"database", dbCfg.Name,
		"partitions", len(parts),
		"directory", partDir,
	)

	// 6. Validate git repo before any git operations
	if err := s.GitClient.ValidateRepo(ctx); err != nil {
		return fmt.Errorf("git validation failed: %w", err)
	}

	// 7. Git add (from here on, don't clean up partition files — they're valid)
	if err := s.GitClient.Add(ctx, partDir); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}

	// 8. Git commit
	commitMsg := s.formatCommitMessage(dbCfg.Name, timestamp)
	if err := s.GitClient.Commit(ctx, commitMsg); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}

	// 9. Git push (if configured)
	if s.GitConfig.AutoPushEnabled() {
		if err := s.GitClient.Push(ctx); err != nil {
			return fmt.Errorf("git push failed: %w", err)
		}
	}

	s.Logger.Info("backup completed successfully",
		"database", dbCfg.Name,
		"timestamp", timestamp,
		"partitions", len(parts),
	)

	return nil
}

// RunAll executes backup for all databases sequentially.
// Returns nil only if ALL succeed. Returns an error listing failures if any fail.
func (s *Service) RunAll(ctx context.Context, databases []config.DatabaseConfig) error {
	var failures []string

	for _, db := range databases {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := s.Run(ctx, db); err != nil {
			s.Logger.Error("backup failed for database",
				"database", db.Name,
				"error", err,
			)
			failures = append(failures, fmt.Sprintf("%s: %v", db.Name, err))
		}
	}

	if len(failures) > 0 {
		if len(failures) == len(databases) {
			return fmt.Errorf("all backups failed:\n%s", strings.Join(failures, "\n"))
		}
		return &PartialError{Failures: failures}
	}

	return nil
}

func (s *Service) formatCommitMessage(dbName, timestamp string) string {
	tmpl := s.GitConfig.CommitMessageTemplate
	tmpl = strings.ReplaceAll(tmpl, "{{.DbName}}", dbName)
	tmpl = strings.ReplaceAll(tmpl, "{{.Timestamp}}", timestamp)
	return tmpl
}

// PartialError indicates some databases failed while others succeeded.
type PartialError struct {
	Failures []string
}

func (e *PartialError) Error() string {
	return fmt.Sprintf("partial failure (%d databases failed):\n%s",
		len(e.Failures), strings.Join(e.Failures, "\n"))
}
