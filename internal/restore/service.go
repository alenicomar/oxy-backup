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

// Service orchestrates the restore flow: checkout → validate → assemble → load → git restore.
type Service struct {
	PgExecutor postgres.PgExecutor
	GitClient  git.GitClient
	Logger     *slog.Logger
}

// Run executes the full restore pipeline for a single database at a specific commit.
// It checks out the backup files from the given commit SHA, reassembles them,
// loads into PostgreSQL, then restores the working tree to HEAD.
func (s *Service) Run(ctx context.Context, dbCfg config.DatabaseConfig, commitSHA string) error {
	partDir := filepath.Join(dbCfg.OutputDir, "partitions", dbCfg.Name)

	// 1. Validate git repo (required for checkout-based restore)
	if err := s.GitClient.ValidateRepo(ctx); err != nil {
		return fmt.Errorf("git validation failed: %w", err)
	}

	// 2. Checkout backup files from the target commit
	manifestPath := filepath.Join(partDir, "manifest.json")
	if err := s.GitClient.CheckoutFiles(ctx, commitSHA, manifestPath); err != nil {
		return fmt.Errorf("checking out manifest from %s: %w", shortSHA(commitSHA), err)
	}

	// 3. Load and validate manifest
	manifest, err := backup.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}

	s.Logger.Info("restore starting",
		"database", dbCfg.Name,
		"commit", shortSHA(commitSHA),
		"partitions", manifest.PartCount,
	)

	// 4. Checkout all partition files from the target commit
	partPaths := make([]string, 0, len(manifest.Parts))
	for _, part := range manifest.Parts {
		partPaths = append(partPaths, filepath.Join(partDir, part.Filename))
	}
	if err := s.GitClient.CheckoutFiles(ctx, commitSHA, partPaths...); err != nil {
		return fmt.Errorf("checking out partitions from %s: %w", shortSHA(commitSHA), err)
	}

	// 5. Validate partition files exist on disk
	if err := s.validatePartitions(partDir, manifest); err != nil {
		return err
	}

	// 6. Create destination file and assemble
	destPath := filepath.Join(partDir, fmt.Sprintf("restore_%s.sql", dbCfg.Name))

	assembler := &Assembler{Logger: s.Logger}
	if err := assembler.Reassemble(ctx, manifest, partDir, destPath); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("assembly failed: %w", err)
	}

	s.Logger.Info("partitions assembled",
		"database", dbCfg.Name,
		"destination", destPath,
	)

	// 7. Load into database via psql --single-transaction
	if err := s.PgExecutor.LoadDatabase(ctx, dbCfg, destPath); err != nil {
		s.Logger.Error("restore load failed — destination file preserved for debugging",
			"file", destPath,
			"error", err,
		)
		return fmt.Errorf("database load failed: %w", err)
	}

	s.Logger.Info("database restored successfully", "database", dbCfg.Name)

	// 8. Delete destination file
	if err := os.Remove(destPath); err != nil {
		s.Logger.Warn("failed to remove destination file", "file", destPath, "error", err)
	}

	// 9. Git restore working tree to HEAD (undo the checkout)
	allPaths := append([]string{manifestPath}, partPaths...)
	if err := s.GitClient.Restore(ctx, allPaths...); err != nil {
		s.Logger.Warn("git restore failed — data was loaded successfully, but working tree not restored to HEAD",
			"error", err,
		)
		// Non-fatal: the data is loaded, git restore is best-effort
	}

	s.Logger.Info("restore completed successfully",
		"database", dbCfg.Name,
		"commit", shortSHA(commitSHA),
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

// ListBackups returns commit history for a database's backup directory.
// Each entry represents a point-in-time backup that can be restored.
func ListBackups(ctx context.Context, gitClient git.GitClient, dbCfg config.DatabaseConfig, limit int) ([]git.CommitInfo, error) {
	path := filepath.Join("partitions", dbCfg.Name)
	commits, err := gitClient.Log(ctx, path, limit)
	if err != nil {
		return nil, fmt.Errorf("listing backups for %s: %w", dbCfg.Name, err)
	}
	return commits, nil
}

// shortSHA returns the first 7 characters of a SHA, or the full string if shorter.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
