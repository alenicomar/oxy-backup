package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()

	parts := []PartitionInfo{
		{Filename: "part_0001.sql", SizeBytes: 102400},
		{Filename: "part_0002.sql", SizeBytes: 98000},
		{Filename: "part_0003.sql", SizeBytes: 45000},
	}

	err := WriteManifest(dir, "mydb", "20260318-140000", parts)
	if err != nil {
		t.Fatalf("WriteManifest() error: %v", err)
	}

	// Verify file exists
	manifestPath := filepath.Join(dir, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest.json not found: %v", err)
	}

	// Load it back
	m, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}

	if m.DbName != "mydb" {
		t.Errorf("DbName = %q, want %q", m.DbName, "mydb")
	}
	if m.Timestamp != "20260318-140000" {
		t.Errorf("Timestamp = %q, want %q", m.Timestamp, "20260318-140000")
	}
	if m.PartCount != 3 {
		t.Errorf("PartCount = %d, want 3", m.PartCount)
	}
	if len(m.Parts) != 3 {
		t.Fatalf("Parts count = %d, want 3", len(m.Parts))
	}
	if m.Parts[0].Filename != "part_0001.sql" {
		t.Errorf("Parts[0].Filename = %q, want %q", m.Parts[0].Filename, "part_0001.sql")
	}
	if m.Parts[0].SizeBytes != 102400 {
		t.Errorf("Parts[0].SizeBytes = %d, want 102400", m.Parts[0].SizeBytes)
	}
}

func TestLoadManifestNotFound(t *testing.T) {
	_, err := LoadManifest("/nonexistent/manifest.json")
	if err == nil {
		t.Fatal("LoadManifest() expected error for missing file")
	}
}
