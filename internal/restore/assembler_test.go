package restore

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/alenicomar/oxy-backup/internal/backup"
)

func TestReassembleSuccess(t *testing.T) {
	dir := t.TempDir()
	assembler := &Assembler{Logger: slog.Default()}

	// Create partition files
	partContents := []string{
		"CREATE TABLE test;\n",
		"INSERT INTO test VALUES (1);\n",
		"INSERT INTO test VALUES (2);\n",
	}

	manifest := &backup.Manifest{
		DbName:    "testdb",
		Timestamp: "20260318-140000",
		PartCount: 3,
	}

	for i, content := range partContents {
		filename := "part_" + zeroPad(i+1) + ".sql"
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("writing partition: %v", err)
		}
		manifest.Parts = append(manifest.Parts, backup.PartitionInfo{
			Filename:  filename,
			SizeBytes: int64(len(content)),
		})
	}

	destPath := filepath.Join(dir, "restore_testdb.sql")

	err := assembler.Reassemble(context.Background(), manifest, dir, destPath)
	if err != nil {
		t.Fatalf("Reassemble() error: %v", err)
	}

	// Verify destination file contains all content in order
	content, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}

	expected := "CREATE TABLE test;\nINSERT INTO test VALUES (1);\nINSERT INTO test VALUES (2);\n"
	if string(content) != expected {
		t.Errorf("destination content = %q, want %q", string(content), expected)
	}

	// Verify partition files are deleted
	for _, part := range manifest.Parts {
		partPath := filepath.Join(dir, part.Filename)
		if _, err := os.Stat(partPath); !os.IsNotExist(err) {
			t.Errorf("partition %s should be deleted after assembly", part.Filename)
		}
	}
}

func TestReassembleContextCancellation(t *testing.T) {
	dir := t.TempDir()
	assembler := &Assembler{Logger: slog.Default()}

	// Create one partition
	if err := os.WriteFile(filepath.Join(dir, "part_0001.sql"), []byte("data\n"), 0644); err != nil {
		t.Fatal(err)
	}

	manifest := &backup.Manifest{
		PartCount: 1,
		Parts:     []backup.PartitionInfo{{Filename: "part_0001.sql", SizeBytes: 5}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	destPath := filepath.Join(dir, "restore.sql")
	err := assembler.Reassemble(ctx, manifest, dir, destPath)
	if err == nil {
		t.Fatal("Reassemble() expected error on cancelled context")
	}

	// Destination file should be cleaned up
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Error("destination file should be removed on context cancellation")
	}
}

func TestReassembleMissingPartition(t *testing.T) {
	dir := t.TempDir()
	assembler := &Assembler{Logger: slog.Default()}

	manifest := &backup.Manifest{
		PartCount: 1,
		Parts:     []backup.PartitionInfo{{Filename: "part_0001.sql", SizeBytes: 100}},
	}

	destPath := filepath.Join(dir, "restore.sql")
	err := assembler.Reassemble(context.Background(), manifest, dir, destPath)
	if err == nil {
		t.Fatal("Reassemble() expected error for missing partition")
	}
}

func zeroPad(n int) string {
	if n < 10 {
		return "000" + string(rune('0'+n))
	}
	if n < 100 {
		return "00" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	result := ""
	for n > 0 {
		result = string(rune('0'+n%10)) + result
		n /= 10
	}
	return result
}
