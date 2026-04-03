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
	// 1. Generate timestamp for commit message
	timestamp := time.Now().UTC().Format("20060102-150405")

	// 2. Fixed output directory (no timestamp in path — Git provides versioning)
	partDir := filepath.Join(dbCfg.OutputDir, "partitions", dbCfg.Name)
	if err := os.MkdirAll(partDir, 0755); err != nil {
		return fmt.Errorf("creating partition dir: %w", err)
	}

	s.Logger.Info("starting backup",
		"database", dbCfg.Name,
		"mode", dbCfg.Mode,
		"timestamp", timestamp,
		"output_dir", partDir,
	)

	// 3. Clean stale partition files (partition count may change between backups)
	if err := cleanStalePartitions(partDir); err != nil {
		return fmt.Errorf("cleaning stale partitions: %w", err)
	}

	// 4. Check pg_dump version (best-effort, logs warning only)
	if execPg, ok := s.PgExecutor.(*postgres.ExecPgExecutor); ok {
		execPg.CheckVersion(ctx, dbCfg)
	}

	// 5. Execute pg_dump
	reader, err := s.PgExecutor.DumpDatabase(ctx, dbCfg)
	if err != nil {
		return fmt.Errorf("dump failed: %w", err)
	}

	// 6. Partition the dump
	maxBytes, err := dbCfg.PartitionSizeBytes()
	if err != nil {
		return fmt.Errorf("invalid partition size: %w", err)
	}

	partitioner := &Partitioner{
		MaxBytes: maxBytes,
		Logger:   s.Logger,
	}

	parts, err := partitioner.Split(ctx, reader, partDir)
	if err != nil {
		return fmt.Errorf("partitioning failed: %w", err)
	}

	// 7. Write manifest
	if err := WriteManifest(partDir, dbCfg.Name, parts); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	s.Logger.Info("backup partitioned successfully",
		"database", dbCfg.Name,
		"partitions", len(parts),
		"directory", partDir,
	)

	// 8. Validate git repo before any git operations
	if err := s.GitClient.ValidateRepo(ctx); err != nil {
		return fmt.Errorf("git validation failed: %w", err)
	}

	// 9. Git add — explicit file list (manifest + partitions + extra_paths)
	addPaths := s.collectAddPaths(partDir, parts)
	if err := s.GitClient.Add(ctx, addPaths...); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}

	// 10. Git commit
	commitMsg := s.formatCommitMessage(dbCfg.Name, timestamp)
	if err := s.GitClient.Commit(ctx, commitMsg); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}

	// 11. Git push (if configured)
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

// cleanStalePartitions removes existing part_*.sql and manifest.json from the directory.
// This prevents orphan files when partition count changes between backups.
func cleanStalePartitions(dir string) error {
	patterns := []string{"part_*.sql", "manifest.json"}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return fmt.Errorf("globbing %s: %w", pattern, err)
		}
		for _, match := range matches {
			if err := os.Remove(match); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("removing stale file %s: %w", match, err)
			}
		}
	}
	return nil
}

// collectAddPaths builds the explicit file list for git add:
// manifest.json + all partition files + extra_paths from config.
func (s *Service) collectAddPaths(partDir string, parts []PartitionInfo) []string {
	paths := make([]string, 0, len(parts)+1+len(s.GitConfig.ExtraPaths))

	// Manifest
	paths = append(paths, filepath.Join(partDir, "manifest.json"))

	// Partition files
	for _, p := range parts {
		paths = append(paths, filepath.Join(partDir, p.Filename))
	}

	// Extra paths from config (already validated as relative in config.Validate)
	paths = append(paths, s.GitConfig.ExtraPaths...)

	return paths
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
