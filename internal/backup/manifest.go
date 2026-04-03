package backup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Manifest holds metadata about a partitioned backup.
type Manifest struct {
	DbName    string          `json:"db_name"`
	PartCount int             `json:"part_count"`
	Parts     []PartitionInfo `json:"parts"`
}

// PartitionInfo describes a single partition file.
type PartitionInfo struct {
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
}

// WriteManifest writes the manifest as JSON to the given directory.
func WriteManifest(dir, dbName string, parts []PartitionInfo) error {
	m := Manifest{
		DbName:    dbName,
		PartCount: len(parts),
		Parts:     parts,
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	return nil
}

// LoadManifest reads and parses a manifest.json file.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	return &m, nil
}
