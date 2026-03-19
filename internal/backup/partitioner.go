package backup

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Partitioner splits an io.Reader into files at line boundaries,
// each not exceeding MaxBytes (except for the trailing line).
type Partitioner struct {
	MaxBytes int64
	Logger   *slog.Logger
}

// Split reads from reader line by line and writes partition files
// to outputDir. Returns the list of created partitions.
func (p *Partitioner) Split(ctx context.Context, reader io.Reader, outputDir string) ([]PartitionInfo, error) {
	scanner := bufio.NewScanner(reader)
	// Set a large buffer to handle long lines (up to 10MB per line)
	const maxLineSize = 10 * 1024 * 1024
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	var parts []PartitionInfo
	var currentFile *os.File
	var currentBytes int64
	partNum := 0

	closeCurrentFile := func() {
		if currentFile != nil {
			currentFile.Close()
			currentFile = nil
		}
	}
	defer closeCurrentFile()

	newPartition := func() error {
		closeCurrentFile()
		partNum++
		filename := fmt.Sprintf("part_%04d.sql", partNum)
		path := filepath.Join(outputDir, filename)

		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("creating partition %s: %w", filename, err)
		}

		currentFile = f
		currentBytes = 0

		p.Logger.Debug("started new partition", "file", filename, "number", partNum)
		return nil
	}

	// Start first partition
	if err := newPartition(); err != nil {
		return nil, err
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			closeCurrentFile()
			return nil, ctx.Err()
		default:
		}

		line := scanner.Bytes()
		lineLen := int64(len(line)) + 1 // +1 for newline

		// If current partition exceeds max and we have data, start new one
		if currentBytes > 0 && currentBytes+lineLen > p.MaxBytes {
			// Record the completed partition
			info, err := os.Stat(currentFile.Name())
			if err != nil {
				return nil, fmt.Errorf("stat partition: %w", err)
			}
			parts = append(parts, PartitionInfo{
				Filename:  filepath.Base(currentFile.Name()),
				SizeBytes: info.Size(),
			})

			p.Logger.Info("partition complete",
				"file", filepath.Base(currentFile.Name()),
				"size", info.Size(),
				"total_parts", len(parts),
			)

			if err := newPartition(); err != nil {
				return nil, err
			}
		}

		// Write line + newline
		if _, err := currentFile.Write(line); err != nil {
			return nil, fmt.Errorf("writing to partition: %w", err)
		}
		if _, err := currentFile.Write([]byte("\n")); err != nil {
			return nil, fmt.Errorf("writing newline to partition: %w", err)
		}
		currentBytes += lineLen
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading input: %w", err)
	}

	// Record final partition (if it has data)
	if currentBytes > 0 {
		info, err := os.Stat(currentFile.Name())
		if err != nil {
			return nil, fmt.Errorf("stat final partition: %w", err)
		}
		parts = append(parts, PartitionInfo{
			Filename:  filepath.Base(currentFile.Name()),
			SizeBytes: info.Size(),
		})

		p.Logger.Info("final partition complete",
			"file", filepath.Base(currentFile.Name()),
			"size", info.Size(),
			"total_parts", len(parts),
		)
	}

	closeCurrentFile()
	return parts, nil
}
