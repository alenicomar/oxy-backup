package restore

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/alenicomar/oxy-backup/internal/backup"
	"github.com/alenicomar/oxy-backup/internal/config"
	"github.com/alenicomar/oxy-backup/internal/git"
	"github.com/alenicomar/oxy-backup/internal/postgres"
)

// Service orchestrates the restore flow: validate → assemble → load → cleanup.
type Service struct {
	PgExecutor postgres.PgExecutor
	GitClient  git.GitClient
	Logger     *slog.Logger
}

// Run executes the full restore pipeline for a single database and timestamp.
func (s *Service) Run(ctx context.Context, dbCfg config.DatabaseConfig, timestamp string) error {
	partDir := filepath.Join(dbCfg.OutputDir, "partitions", dbCfg.Name, timestamp)

	// 0. Validate git repo (partitions will be recovered via git restore later)
	if err := s.GitClient.ValidateRepo(ctx); err != nil {
		s.Logger.Warn("git repo validation failed — git restore after load will not work",
			"error", err,
		)
	}

	// 1. Load and validate manifest
	manifestPath := filepath.Join(partDir, "manifest.json")
	manifest, err := backup.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	s.Logger.Info("restore starting",
		"database", dbCfg.Name,
		"timestamp", timestamp,
		"partitions", manifest.PartCount,
	)

	// 2. Validate partition files exist
	if err := s.validatePartitions(partDir, manifest); err != nil {
		return err
	}

	// 3. Create destination file and assemble
	destPath := filepath.Join(partDir, fmt.Sprintf("restore_%s_%s.sql", dbCfg.Name, timestamp))

	assembler := &Assembler{Logger: s.Logger}
	if err := assembler.Reassemble(ctx, manifest, partDir, destPath); err != nil {
		// Clean up destination file on assembly failure
		os.Remove(destPath)
		return fmt.Errorf("assembly failed: %w", err)
	}

	s.Logger.Info("partitions assembled",
		"database", dbCfg.Name,
		"destination", destPath,
	)

	// 4. Load into database via psql --single-transaction
	if err := s.PgExecutor.LoadDatabase(ctx, dbCfg, destPath); err != nil {
		// Keep destination file for debugging
		s.Logger.Error("restore load failed — destination file preserved for debugging",
			"file", destPath,
			"error", err,
		)
		return fmt.Errorf("database load failed: %w", err)
	}

	s.Logger.Info("database restored successfully", "database", dbCfg.Name)

	// 5. Delete destination file
	if err := os.Remove(destPath); err != nil {
		s.Logger.Warn("failed to remove destination file", "file", destPath, "error", err)
	}

	// 6. Git restore all partition files
	partPaths := make([]string, 0, len(manifest.Parts))
	for _, part := range manifest.Parts {
		partPaths = append(partPaths, filepath.Join(partDir, part.Filename))
	}

	if err := s.GitClient.Restore(ctx, partPaths...); err != nil {
		s.Logger.Warn("git restore failed — data was loaded successfully, but partition files could not be recovered",
			"error", err,
		)
		// Non-fatal: the data is loaded, git restore is best-effort
	}

	s.Logger.Info("restore completed successfully",
		"database", dbCfg.Name,
		"timestamp", timestamp,
	)

	return nil
}

// validatePartitions checks that all partition files listed in the manifest exist.
func (s *Service) validatePartitions(partDir string, manifest *backup.Manifest) error {
	missing := 0
	for _, part := range manifest.Parts {
		partPath := filepath.Join(partDir, part.Filename)
		if _, err := os.Stat(partPath); os.IsNotExist(err) {
			s.Logger.Error("missing partition file", "file", part.Filename)
			missing++
		}
	}

	if missing > 0 {
		return fmt.Errorf("partition count mismatch: expected %d, missing %d files",
			manifest.PartCount, missing)
	}

	return nil
}

// ListTimestamps returns available backup timestamps for a database.
func ListTimestamps(dbCfg config.DatabaseConfig) ([]string, error) {
	partBaseDir := filepath.Join(dbCfg.OutputDir, "partitions", dbCfg.Name)
	entries, err := os.ReadDir(partBaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing backup timestamps: %w", err)
	}

	var timestamps []string
	for _, entry := range entries {
		if entry.IsDir() {
			// Verify it has a manifest.json
			manifestPath := filepath.Join(partBaseDir, entry.Name(), "manifest.json")
			if _, err := os.Stat(manifestPath); err == nil {
				timestamps = append(timestamps, entry.Name())
			}
		}
	}

	return timestamps, nil
}
