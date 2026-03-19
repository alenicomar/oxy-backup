package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/backup-lite/backup-lite/internal/backup"
	"github.com/backup-lite/backup-lite/internal/config"
	"github.com/backup-lite/backup-lite/internal/exitcode"
)

func TestMapBackupExitCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{"nil error", nil, exitcode.OK},
		{"dump failed", errors.New("dump failed: connection refused"), exitcode.DumpError},
		{"partitioning failed", errors.New("partitioning failed: disk full"), exitcode.PartitionError},
		{"invalid partition size", errors.New("invalid partition size: -1"), exitcode.PartitionError},
		{"git push error", errors.New("git push failed: rejected"), exitcode.GitError},
		{"git commit error", errors.New("git commit failed"), exitcode.GitError},
		{"config error", errors.New("config validation failed"), exitcode.ConfigError},
		{"creating partition dir", errors.New("creating partition dir: permission denied"), exitcode.ConfigError},
		{"partial error", &backup.PartialError{Failures: []string{"db2: fail"}}, exitcode.PartialFailure},
		{"unknown error", errors.New("something unexpected"), exitcode.DumpError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapBackupExitCode(tt.err)
			if got != tt.expected {
				t.Errorf("mapBackupExitCode(%v) = %d, want %d", tt.err, got, tt.expected)
			}
		})
	}
}

func TestMapRestoreExitCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{"nil error", nil, exitcode.OK},
		{"database load failed", errors.New("database load failed: connection refused"), exitcode.RestoreError},
		{"psql error", errors.New("psql restore failed"), exitcode.RestoreError},
		{"assembly failed", errors.New("assembly failed: I/O error"), exitcode.RestoreError},
		{"manifest error", errors.New("loading manifest: file not found"), exitcode.RestoreError},
		{"partition count mismatch", errors.New("partition count mismatch: expected 5, missing 2"), exitcode.RestoreError},
		{"git restore error", errors.New("git restore failed"), exitcode.GitError},
		{"unknown error", errors.New("something unexpected"), exitcode.RestoreError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapRestoreExitCode(tt.err)
			if got != tt.expected {
				t.Errorf("mapRestoreExitCode(%v) = %d, want %d", tt.err, got, tt.expected)
			}
		})
	}
}

func TestFindDatabase(t *testing.T) {
	// Set up package-level cfg for the test.
	cfg = &config.Config{
		Databases: []config.DatabaseConfig{
			{Name: "db1", Mode: "docker", Container: "pg", Database: "db1"},
			{Name: "db2", Mode: "docker", Container: "pg", Database: "db2"},
		},
	}

	t.Run("found", func(t *testing.T) {
		got, err := findDatabase("db2")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != "db2" {
			t.Errorf("got database %q, want %q", got.Name, "db2")
		}
		// Verify it returns a pointer to the original slice element, not a copy.
		if got != &cfg.Databases[1] {
			t.Error("expected pointer to original slice element")
		}
	})

	t.Run("not found", func(t *testing.T) {
		got, err := findDatabase("db3")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if got != nil {
			t.Errorf("expected nil result, got %+v", got)
		}
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("error message %q should contain %q", err.Error(), "not found")
		}
	})
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{"found", "hello world", "world", true},
		{"not found", "hello world", "xyz", false},
		{"empty substr", "hello", "", true},
		{"empty s", "", "a", false},
		{"both empty", "", "", true},
		{"exact match", "abc", "abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.s, tt.substr)
			if got != tt.expected {
				t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.expected)
			}
		})
	}
}
