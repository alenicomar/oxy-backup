package backup

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitSmallInput(t *testing.T) {
	dir := t.TempDir()
	p := &Partitioner{
		MaxBytes: 102400, // 100KB
		Logger:   slog.Default(),
	}

	input := "CREATE TABLE test;\nINSERT INTO test VALUES (1);\n"
	reader := strings.NewReader(input)

	parts, err := p.Split(context.Background(), reader, dir)
	if err != nil {
		t.Fatalf("Split() error: %v", err)
	}

	if len(parts) != 1 {
		t.Fatalf("parts count = %d, want 1", len(parts))
	}

	// Verify content
	content, err := os.ReadFile(filepath.Join(dir, parts[0].Filename))
	if err != nil {
		t.Fatalf("reading partition: %v", err)
	}
	if string(content) != input {
		t.Errorf("partition content = %q, want %q", string(content), input)
	}
}

func TestSplitMultiplePartitions(t *testing.T) {
	dir := t.TempDir()
	p := &Partitioner{
		MaxBytes: 50, // 50 bytes per partition
		Logger:   slog.Default(),
	}

	// Each line is ~30 bytes, so 2 lines should trigger a partition
	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, "INSERT INTO test VALUES ("+strings.Repeat("x", 20)+");")
	}
	input := strings.Join(lines, "\n") + "\n"
	reader := strings.NewReader(input)

	parts, err := p.Split(context.Background(), reader, dir)
	if err != nil {
		t.Fatalf("Split() error: %v", err)
	}

	if len(parts) < 2 {
		t.Fatalf("parts count = %d, want >= 2", len(parts))
	}

	// Verify no mid-line splits — each partition should have complete lines
	for _, part := range parts {
		content, err := os.ReadFile(filepath.Join(dir, part.Filename))
		if err != nil {
			t.Fatalf("reading partition %s: %v", part.Filename, err)
		}
		// Must end with newline
		if !strings.HasSuffix(string(content), "\n") {
			t.Errorf("partition %s doesn't end with newline", part.Filename)
		}
	}

	// Verify concatenation matches original input
	var reconstructed strings.Builder
	for _, part := range parts {
		content, _ := os.ReadFile(filepath.Join(dir, part.Filename))
		reconstructed.Write(content)
	}
	if reconstructed.String() != input {
		t.Errorf("reconstructed content doesn't match original")
	}
}

func TestSplitNaming(t *testing.T) {
	dir := t.TempDir()
	p := &Partitioner{
		MaxBytes: 10,
		Logger:   slog.Default(),
	}

	input := "line1\nline2\nline3\n"
	parts, err := p.Split(context.Background(), strings.NewReader(input), dir)
	if err != nil {
		t.Fatalf("Split() error: %v", err)
	}

	for i, part := range parts {
		expected := "part_" + zeroPad(i+1) + ".sql"
		if part.Filename != expected {
			t.Errorf("part[%d].Filename = %q, want %q", i, part.Filename, expected)
		}
	}
}

func TestSplitContextCancellation(t *testing.T) {
	dir := t.TempDir()
	p := &Partitioner{
		MaxBytes: 10,
		Logger:   slog.Default(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := p.Split(ctx, strings.NewReader("some\ndata\n"), dir)
	if err == nil {
		t.Fatal("Split() expected error on cancelled context, got nil")
	}
}

func zeroPad(n int) string {
	s := "000" + strings.Repeat("", 0)
	num := strings.TrimLeft(s, "0")
	_ = num
	// Simple approach
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
