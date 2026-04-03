package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/alenicomar/oxy-backup/internal/exitcode"
	"github.com/alenicomar/oxy-backup/internal/git"
	"github.com/spf13/cobra"
)

// timestampDirPattern matches directories named YYYYMMDD-HHMMSS.
var timestampDirPattern = regexp.MustCompile(`^\d{8}-\d{6}$`)

func init() {
	rootCmd.AddCommand(migrateCmd)
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate backup repos from timestamp-based directories to fixed paths",
	Long: `Scans all configured databases for timestamp-based subdirectories under
partitions/<db-name>/ and migrates to the fixed-path model where Git history
provides versioning instead of filesystem directories.

The latest timestamp directory's contents are promoted to the fixed path,
and all timestamp directories are removed. The result is committed to Git.`,
	Args: cobra.NoArgs,
	RunE: runMigrate,
}

func runMigrate(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	if dryRun {
		return dryRunMigrate()
	}

	gitClient := git.NewClient(cfg.Git, ".", logger)

	migrated := 0

	for _, db := range cfg.Databases {
		partBase := filepath.Join(db.OutputDir, "partitions", db.Name)

		entries, err := os.ReadDir(partBase)
		if err != nil {
			if os.IsNotExist(err) {
				logger.Debug("no partition directory found, skipping", "database", db.Name)
				continue
			}
			return fmt.Errorf("reading %s: %w", partBase, err)
		}

		// Find timestamp subdirectories
		var tsDirs []string
		for _, entry := range entries {
			if entry.IsDir() && timestampDirPattern.MatchString(entry.Name()) {
				tsDirs = append(tsDirs, entry.Name())
			}
		}

		if len(tsDirs) == 0 {
			logger.Info("no timestamp directories found, already migrated or empty",
				"database", db.Name)
			continue
		}

		// Sort ascending — latest is last
		sort.Strings(tsDirs)
		latest := tsDirs[len(tsDirs)-1]
		latestDir := filepath.Join(partBase, latest)

		logger.Info("migrating database",
			"database", db.Name,
			"timestamp_dirs", len(tsDirs),
			"latest", latest,
		)

		// Copy files from latest timestamp dir to partBase (fixed path)
		latestEntries, err := os.ReadDir(latestDir)
		if err != nil {
			return fmt.Errorf("reading latest dir %s: %w", latestDir, err)
		}

		for _, f := range latestEntries {
			if f.IsDir() {
				continue // skip subdirectories
			}
			src := filepath.Join(latestDir, f.Name())
			dst := filepath.Join(partBase, f.Name())

			if err := copyFile(src, dst); err != nil {
				return fmt.Errorf("copying %s → %s: %w", src, dst, err)
			}
			logger.Debug("copied file", "src", src, "dst", dst)
		}

		// Remove ALL timestamp directories
		for _, ts := range tsDirs {
			tsDir := filepath.Join(partBase, ts)
			if err := os.RemoveAll(tsDir); err != nil {
				return fmt.Errorf("removing timestamp dir %s: %w", tsDir, err)
			}
			logger.Debug("removed timestamp directory", "dir", tsDir)
		}

		migrated++
		fmt.Fprintf(os.Stdout, "  ✓ %s: promoted %s, removed %d timestamp dirs\n",
			db.Name, latest, len(tsDirs))
	}

	if migrated == 0 {
		fmt.Fprintln(os.Stdout, "Nothing to migrate — all databases already use fixed paths.")
		return nil
	}

	// Git add + commit the migration
	if err := gitClient.Add(ctx, "."); err != nil {
		fmt.Fprintf(os.Stderr, "git add failed: %v\n", err)
		os.Exit(exitcode.GitError)
	}

	commitMsg := fmt.Sprintf("chore: migrate %d database(s) from timestamp dirs to fixed paths", migrated)
	if err := gitClient.Commit(ctx, commitMsg); err != nil {
		fmt.Fprintf(os.Stderr, "git commit failed: %v\n", err)
		os.Exit(exitcode.GitError)
	}

	if cfg.Git.AutoPushEnabled() {
		if err := gitClient.Push(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "git push failed: %v\n", err)
			os.Exit(exitcode.GitError)
		}
	}

	fmt.Fprintf(os.Stdout, "\nMigration complete: %d database(s) migrated.\n", migrated)
	return nil
}

func dryRunMigrate() error {
	fmt.Fprintf(os.Stdout, "[dry-run] Config validated successfully (%s)\n", cfgPath)

	for _, db := range cfg.Databases {
		partBase := filepath.Join(db.OutputDir, "partitions", db.Name)
		entries, err := os.ReadDir(partBase)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stdout, "[dry-run] %s: no partition directory\n", db.Name)
				continue
			}
			return fmt.Errorf("reading %s: %w", partBase, err)
		}

		var tsDirs []string
		for _, entry := range entries {
			if entry.IsDir() && timestampDirPattern.MatchString(entry.Name()) {
				tsDirs = append(tsDirs, entry.Name())
			}
		}

		if len(tsDirs) == 0 {
			fmt.Fprintf(os.Stdout, "[dry-run] %s: already migrated (no timestamp dirs)\n", db.Name)
		} else {
			sort.Strings(tsDirs)
			fmt.Fprintf(os.Stdout, "[dry-run] %s: would promote %s, remove %d dirs\n",
				db.Name, tsDirs[len(tsDirs)-1], len(tsDirs))
		}
	}
	return nil
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
