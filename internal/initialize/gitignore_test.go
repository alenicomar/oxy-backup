package initialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// oxyPatterns returns the non-empty, non-comment lines from gitignoreContent.
func oxyPatterns() []string {
	var patterns []string
	for _, line := range strings.Split(gitignoreContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		patterns = append(patterns, trimmed)
	}
	return patterns
}

func TestWriteGitignore_NewFile(t *testing.T) {
	tmpDir := t.TempDir()

	if err := WriteGitignore(tmpDir); err != nil {
		t.Fatalf("WriteGitignore failed: %v", err)
	}

	path := filepath.Join(tmpDir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}

	content := string(data)
	for _, pattern := range oxyPatterns() {
		if !strings.Contains(content, pattern) {
			t.Errorf("expected .gitignore to contain %q", pattern)
		}
	}

	// Verify specific well-known patterns
	for _, expected := range []string{"restore_*.sql", ".env", "/oxy"} {
		if !strings.Contains(content, expected) {
			t.Errorf("expected .gitignore to contain %q", expected)
		}
	}
}

func TestWriteGitignore_ExistingFile_AppendsNew(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, ".gitignore")

	original := "*.log\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("failed to create existing .gitignore: %v", err)
	}

	if err := WriteGitignore(tmpDir); err != nil {
		t.Fatalf("WriteGitignore failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}

	content := string(data)

	// Original content preserved
	if !strings.Contains(content, "*.log") {
		t.Error("expected original '*.log' to be preserved")
	}

	// Oxy patterns appended
	for _, pattern := range oxyPatterns() {
		if !strings.Contains(content, pattern) {
			t.Errorf("expected appended pattern %q", pattern)
		}
	}

	// Should have the "Added by oxy init" comment
	if !strings.Contains(content, "# Added by oxy init") {
		t.Error("expected '# Added by oxy init' marker in appended content")
	}
}

func TestWriteGitignore_ExistingFile_NoDuplicates(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, ".gitignore")

	// Write the full oxy content as if it already existed
	if err := os.WriteFile(path, []byte(gitignoreContent), 0644); err != nil {
		t.Fatalf("failed to create existing .gitignore: %v", err)
	}

	beforeData, _ := os.ReadFile(path)
	beforeContent := string(beforeData)

	if err := WriteGitignore(tmpDir); err != nil {
		t.Fatalf("WriteGitignore failed: %v", err)
	}

	afterData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	afterContent := string(afterData)

	// File should be unchanged — nothing to append
	if beforeContent != afterContent {
		t.Errorf("file should be unchanged when all patterns already exist.\nbefore length: %d\nafter length:  %d",
			len(beforeContent), len(afterContent))
	}
}

func TestWriteGitignore_ExistingFile_PartialOverlap(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, ".gitignore")

	// Pre-populate with some (but not all) oxy patterns
	partial := "# My project\n*.log\nrestore_*.sql\n.env\n"
	if err := os.WriteFile(path, []byte(partial), 0644); err != nil {
		t.Fatalf("failed to create existing .gitignore: %v", err)
	}

	if err := WriteGitignore(tmpDir); err != nil {
		t.Fatalf("WriteGitignore failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}

	content := string(data)

	// Original content preserved
	if !strings.Contains(content, "*.log") {
		t.Error("expected original '*.log' to be preserved")
	}

	// All oxy patterns should be present (either original or appended)
	for _, pattern := range oxyPatterns() {
		if !strings.Contains(content, pattern) {
			t.Errorf("expected pattern %q to be present", pattern)
		}
	}

	// Already-existing patterns should NOT be duplicated in the appended section
	appendIdx := strings.Index(content, "# Added by oxy init")
	if appendIdx < 0 {
		t.Fatal("expected '# Added by oxy init' marker — there ARE missing patterns to append")
	}
	appendedSection := content[appendIdx:]

	if strings.Contains(appendedSection, "restore_*.sql") {
		t.Error("restore_*.sql was already present; should NOT be in appended section")
	}
	if strings.Contains(appendedSection, ".env\n") {
		// Check specifically for ".env" as a standalone line in the appended section
		// (not .env.* or .env.local which are different patterns)
		for _, line := range strings.Split(appendedSection, "\n") {
			if strings.TrimSpace(line) == ".env" {
				t.Error(".env was already present; should NOT be in appended section")
				break
			}
		}
	}

	// Patterns that were NOT in the original should be in the appended section
	if !strings.Contains(appendedSection, "/oxy") {
		t.Error("expected /oxy to be in appended section (was not in original)")
	}
}
