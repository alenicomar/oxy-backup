package initialize

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alenicomar/oxy-backup/internal/config"
	"gopkg.in/yaml.v3"
)

func newTestService() (*Service, *bytes.Buffer) {
	out := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Service{Logger: logger, Out: out}, out
}

func dockerOpts() *InitOptions {
	return &InitOptions{
		Mode:          "docker",
		Container:     "test-pg-container",
		DbName:        "testdb",
		DbDatabase:    "testdb",
		PasswordEnv:   "PGPASSWORD",
		PartitionSize: "1MB",
		OutputDir:     "./backups",
		GitRemote:     "git@github.com:user/repo.git",
	}
}

func hostOpts() *InitOptions {
	return &InitOptions{
		Mode:          "host",
		Host:          "localhost",
		Port:          5432,
		Username:      "postgres",
		DbName:        "testdb",
		DbDatabase:    "testdb",
		PasswordEnv:   "PGPASSWORD",
		PartitionSize: "1MB",
		OutputDir:     "./backups",
	}
}

func TestService_Run_Success_DockerMode(t *testing.T) {
	svc, out := newTestService()
	dir := t.TempDir()
	opts := dockerOpts()

	err := svc.Run(context.Background(), dir, opts, false)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// oxy.yaml exists
	cfgPath := filepath.Join(dir, "oxy.yaml")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		t.Fatal("oxy.yaml was not created")
	}

	// .gitignore exists
	giPath := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(giPath); os.IsNotExist(err) {
		t.Fatal(".gitignore was not created")
	}

	// .git directory exists (git init worked)
	gitDir := filepath.Join(dir, ".git")
	if info, err := os.Stat(gitDir); os.IsNotExist(err) || !info.IsDir() {
		t.Fatal(".git directory was not created by git init")
	}

	// oxy.yaml can be unmarshaled back to a Config
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("reading oxy.yaml: %v", err)
	}
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal oxy.yaml: %v", err)
	}
	if cfg.Version != "1" {
		t.Errorf("config version = %q, want %q", cfg.Version, "1")
	}
	if len(cfg.Databases) != 1 {
		t.Fatalf("expected 1 database, got %d", len(cfg.Databases))
	}
	if cfg.Databases[0].Name != "testdb" {
		t.Errorf("database name = %q, want %q", cfg.Databases[0].Name, "testdb")
	}

	// Output contains the expected summary
	if !strings.Contains(out.String(), "Initialized oxy backup repository") {
		t.Errorf("output should contain init message, got:\n%s", out.String())
	}

	// Output should mention the remote since we set one
	if !strings.Contains(out.String(), "origin") {
		t.Errorf("output should mention remote 'origin', got:\n%s", out.String())
	}
}

func TestService_Run_Success_HostMode(t *testing.T) {
	svc, out := newTestService()
	dir := t.TempDir()
	opts := hostOpts() // no GitRemote set

	err := svc.Run(context.Background(), dir, opts, false)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Files created
	if _, err := os.Stat(filepath.Join(dir, "oxy.yaml")); os.IsNotExist(err) {
		t.Fatal("oxy.yaml was not created")
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); os.IsNotExist(err) {
		t.Fatal(".gitignore was not created")
	}

	// No remote-related warning (Remote 'origin' → <url>) since GitRemote is empty
	if strings.Contains(out.String(), "Remote 'origin' →") {
		t.Errorf("output should NOT contain remote URL line when no remote set, got:\n%s", out.String())
	}
}

func TestService_Run_Success_NoRemote(t *testing.T) {
	svc, out := newTestService()
	dir := t.TempDir()
	opts := dockerOpts()
	opts.GitRemote = "" // explicitly no remote

	err := svc.Run(context.Background(), dir, opts, false)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Should still init successfully
	if !strings.Contains(out.String(), "Initialized oxy backup repository") {
		t.Errorf("output should contain init message, got:\n%s", out.String())
	}

	// Should not mention a remote URL
	if strings.Contains(out.String(), "Remote 'origin' →") {
		t.Errorf("output should NOT contain remote URL line, got:\n%s", out.String())
	}
}

func TestService_Run_AlreadyInitialized(t *testing.T) {
	svc, _ := newTestService()
	dir := t.TempDir()

	// Pre-create oxy.yaml
	if err := os.WriteFile(filepath.Join(dir, "oxy.yaml"), []byte("version: '1'\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := svc.Run(context.Background(), dir, dockerOpts(), false)

	if err == nil {
		t.Fatal("Run() should return error when already initialized with force=false")
	}
	if !strings.Contains(err.Error(), "already initialized") {
		t.Errorf("error = %q, want it to contain 'already initialized'", err.Error())
	}
}

func TestService_Run_AlreadyInitialized_Force(t *testing.T) {
	svc, _ := newTestService()
	dir := t.TempDir()

	// Pre-create oxy.yaml with different content
	if err := os.WriteFile(filepath.Join(dir, "oxy.yaml"), []byte("old content\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := svc.Run(context.Background(), dir, dockerOpts(), true)
	if err != nil {
		t.Fatalf("Run(force=true) error: %v", err)
	}

	// oxy.yaml should have been overwritten
	data, err := os.ReadFile(filepath.Join(dir, "oxy.yaml"))
	if err != nil {
		t.Fatalf("reading oxy.yaml: %v", err)
	}
	if strings.Contains(string(data), "old content") {
		t.Error("oxy.yaml should have been overwritten but still contains old content")
	}
}

func TestService_Run_CreatesDirectory(t *testing.T) {
	svc, _ := newTestService()
	base := t.TempDir()
	nested := filepath.Join(base, "sub", "dir", "project")

	err := svc.Run(context.Background(), nested, dockerOpts(), false)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Verify nested directory was created with expected files
	if _, err := os.Stat(filepath.Join(nested, "oxy.yaml")); os.IsNotExist(err) {
		t.Fatal("oxy.yaml was not created in nested directory")
	}
	if _, err := os.Stat(filepath.Join(nested, ".gitignore")); os.IsNotExist(err) {
		t.Fatal(".gitignore was not created in nested directory")
	}
	if info, err := os.Stat(filepath.Join(nested, ".git")); os.IsNotExist(err) || !info.IsDir() {
		t.Fatal(".git directory was not created in nested directory")
	}
}
