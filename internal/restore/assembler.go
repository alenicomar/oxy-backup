package restore

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/backup-lite/backup-lite/internal/backup"
)

// Assembler reassembles partition files into a single SQL file,
// deleting each partition from the filesystem after copying.
type Assembler struct {
	Logger *slog.Logger
}

// Reassemble reads partitions in order from the manifest, appends each to
// destPath, and removes each partition file after successful copy.
func (a *Assembler) Reassemble(ctx context.Context, manifest *backup.Manifest, partDir, destPath string) error {
	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("creating destination file: %w", err)
	}
	defer destFile.Close()

	for i, part := range manifest.Parts {
		select {
		case <-ctx.Done():
			destFile.Close()
			os.Remove(destPath)
			return ctx.Err()
		default:
		}

		partPath := filepath.Join(partDir, part.Filename)

		a.Logger.Info("assembling partition",
			"file", part.Filename,
			"index", i+1,
			"total", manifest.PartCount,
		)

		if err := copyAndDelete(partPath, destFile); err != nil {
			return fmt.Errorf("assembling partition %s: %w", part.Filename, err)
		}
	}

	return nil
}

// copyAndDelete copies the content of src file into dest writer,
// then removes the src file from the filesystem.
func copyAndDelete(srcPath string, dest io.Writer) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening partition: %w", err)
	}

	if _, err := io.Copy(dest, src); err != nil {
		src.Close()
		return fmt.Errorf("copying partition: %w", err)
	}
	src.Close()

	// Delete partition file from filesystem (NOT from Git)
	if err := os.Remove(srcPath); err != nil {
		return fmt.Errorf("deleting partition after copy: %w", err)
	}

	return nil
}
