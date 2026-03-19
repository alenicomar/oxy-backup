package postgres

import (
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/alenicomar/oxy-backup/internal/config"
)

// ---------------------------------------------------------------------------
// parseMajorVersion
// ---------------------------------------------------------------------------

func TestParseMajorVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "pg_dump 16.2", input: "pg_dump (PostgreSQL) 16.2", want: 16},
		{name: "pg_dump 14 ubuntu", input: "pg_dump (PostgreSQL) 14.10 (Ubuntu 14.10-1.pgdg22.04+1)", want: 14},
		{name: "debian 16", input: "16.2 (Debian 16.2-1.pgdg120+2)", want: 16},
		{name: "psql 15", input: "psql (PostgreSQL) 15.4", want: 15},
		{name: "empty string", input: "", want: 0},
		{name: "whitespace only", input: "   ", want: 0},
		{name: "no version", input: "no version here", want: 0},
		{name: "legacy 9.6", input: "9.6.24", want: 9},
		{name: "pg_dump 17", input: "pg_dump (PostgreSQL) 17.0", want: 17},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMajorVersion(tt.input)
			if got != tt.want {
				t.Errorf("parseMajorVersion(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildDumpCommand
// ---------------------------------------------------------------------------

func TestBuildDumpCommand(t *testing.T) {
	e := &ExecPgExecutor{Logger: slog.Default()}

	dockerCfg := config.DatabaseConfig{
		Mode:      "docker",
		Container: "pg_container",
		Username:  "admin",
		Password:  "secret",
		Database:  "mydb",
	}

	hostCfg := config.DatabaseConfig{
		Mode:     "host",
		Host:     "db.example.com",
		Port:     5433,
		Username: "admin",
		Password: "secret",
		Database: "mydb",
	}

	t.Run("docker mode", func(t *testing.T) {
		args, env, err := e.buildDumpCommand(dockerCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantPrefix := []string{"docker", "exec", "-e", "PGPASSWORD=secret", "pg_container", "pg_dump"}
		if !slices.Equal(args[:len(wantPrefix)], wantPrefix) {
			t.Errorf("args prefix = %v, want %v", args[:len(wantPrefix)], wantPrefix)
		}

		assertContainsSubsequence(t, args, []string{"-U", "admin"})
		assertContainsSubsequence(t, args, []string{"-d", "mydb"})
		assertContains(t, args, "--format=plain")

		if len(env) != 0 {
			t.Errorf("env = %v, want nil/empty for docker mode", env)
		}
	})

	t.Run("host mode", func(t *testing.T) {
		args, env, err := e.buildDumpCommand(hostCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if args[0] != "pg_dump" {
			t.Errorf("args[0] = %q, want %q", args[0], "pg_dump")
		}

		assertContainsSubsequence(t, args, []string{"-h", "db.example.com"})
		assertContainsSubsequence(t, args, []string{"-p", "5433"})
		assertContainsSubsequence(t, args, []string{"-U", "admin"})
		assertContainsSubsequence(t, args, []string{"-d", "mydb"})
		assertContains(t, args, "--format=plain")

		assertContains(t, env, "PGPASSWORD=secret")
	})

	t.Run("docker mode with extra pg_dump_args", func(t *testing.T) {
		cfg := dockerCfg
		cfg.PgDumpArgs = []string{"--no-owner", "--exclude-table=logs"}

		args, _, err := e.buildDumpCommand(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertContains(t, args, "--no-owner")
		assertContains(t, args, "--exclude-table=logs")
	})

	t.Run("host mode with extra pg_dump_args", func(t *testing.T) {
		cfg := hostCfg
		cfg.PgDumpArgs = []string{"--no-owner", "--exclude-table=logs"}

		args, _, err := e.buildDumpCommand(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertContains(t, args, "--no-owner")
		assertContains(t, args, "--exclude-table=logs")
	})

	t.Run("unsupported mode", func(t *testing.T) {
		cfg := config.DatabaseConfig{Mode: "kubernetes"}
		_, _, err := e.buildDumpCommand(cfg)
		if err == nil {
			t.Fatal("expected error for unsupported mode, got nil")
		}
		if !strings.Contains(err.Error(), "unsupported mode") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "unsupported mode")
		}
	})
}

// ---------------------------------------------------------------------------
// buildLoadCommand
// ---------------------------------------------------------------------------

func TestBuildLoadCommand(t *testing.T) {
	e := &ExecPgExecutor{Logger: slog.Default()}

	dockerCfg := config.DatabaseConfig{
		Mode:      "docker",
		Container: "pg_container",
		Username:  "admin",
		Password:  "secret",
		Database:  "mydb",
	}

	hostCfg := config.DatabaseConfig{
		Mode:     "host",
		Host:     "db.example.com",
		Port:     5433,
		Username: "admin",
		Password: "secret",
		Database: "mydb",
	}

	t.Run("docker mode", func(t *testing.T) {
		args, env, err := e.buildLoadCommand(dockerCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantPrefix := []string{"docker", "exec", "-i", "-e", "PGPASSWORD=secret", "pg_container", "psql"}
		if !slices.Equal(args[:len(wantPrefix)], wantPrefix) {
			t.Errorf("args prefix = %v, want %v", args[:len(wantPrefix)], wantPrefix)
		}

		assertContainsSubsequence(t, args, []string{"-U", "admin"})
		assertContainsSubsequence(t, args, []string{"-d", "mydb"})
		assertContains(t, args, "--single-transaction")

		if len(env) != 0 {
			t.Errorf("env = %v, want nil/empty for docker mode", env)
		}
	})

	t.Run("host mode", func(t *testing.T) {
		args, env, err := e.buildLoadCommand(hostCfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if args[0] != "psql" {
			t.Errorf("args[0] = %q, want %q", args[0], "psql")
		}

		assertContainsSubsequence(t, args, []string{"-h", "db.example.com"})
		assertContainsSubsequence(t, args, []string{"-p", "5433"})
		assertContainsSubsequence(t, args, []string{"-U", "admin"})
		assertContainsSubsequence(t, args, []string{"-d", "mydb"})
		assertContains(t, args, "--single-transaction")

		assertContains(t, env, "PGPASSWORD=secret")
	})

	t.Run("unsupported mode", func(t *testing.T) {
		cfg := config.DatabaseConfig{Mode: "kubernetes"}
		_, _, err := e.buildLoadCommand(cfg)
		if err == nil {
			t.Fatal("expected error for unsupported mode, got nil")
		}
		if !strings.Contains(err.Error(), "unsupported mode") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "unsupported mode")
		}
	})
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// assertContains checks that needle is present in haystack.
func assertContains(t *testing.T, haystack []string, needle string) {
	t.Helper()
	if !slices.Contains(haystack, needle) {
		t.Errorf("expected %v to contain %q", haystack, needle)
	}
}

// assertContainsSubsequence checks that sub appears as consecutive elements in s.
func assertContainsSubsequence(t *testing.T, s, sub []string) {
	t.Helper()
	if len(sub) == 0 {
		return
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if slices.Equal(s[i:i+len(sub)], sub) {
			return
		}
	}
	t.Errorf("expected %v to contain subsequence %v", s, sub)
}
