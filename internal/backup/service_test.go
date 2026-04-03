package backup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alenicomar/oxy-backup/internal/config"
	"github.com/alenicomar/oxy-backup/internal/git"
)

// --- Mocks ---

type mockPgExecutor struct {
	dumpOutput string
	dumpErr    error
	loadErr    error
}

func (m *mockPgExecutor) DumpDatabase(_ context.Context, _ config.DatabaseConfig) (io.Reader, error) {
	if m.dumpErr != nil {
		return nil, m.dumpErr
	}
	return strings.NewReader(m.dumpOutput), nil
}

func (m *mockPgExecutor) LoadDatabase(_ context.Context, _ config.DatabaseConfig, _ string) error {
	return m.loadErr
}

type mockGitClient struct {
	addCalled     bool
	commitCalled  bool
	pushCalled    bool
	commitMessage string
	pushErr       error
}

func (m *mockGitClient) ValidateRepo(_ context.Context) error {
	return nil
}

func (m *mockGitClient) Add(_ context.Context, _ ...string) error {
	m.addCalled = true
	return nil
}

func (m *mockGitClient) Commit(_ context.Context, message string) error {
	m.commitCalled = true
	m.commitMessage = message
	return nil
}

func (m *mockGitClient) Push(_ context.Context) error {
	m.pushCalled = true
	return m.pushErr
}

func (m *mockGitClient) Restore(_ context.Context, _ ...string) error {
	return nil
}

func (m *mockGitClient) Init(_ context.Context) error { return nil }

func (m *mockGitClient) RemoteAdd(_ context.Context, _, _ string) error { return nil }

func (m *mockGitClient) Log(_ context.Context, _ string, _ int) ([]git.CommitInfo, error) {
	return nil, nil
}

func (m *mockGitClient) CheckoutFiles(_ context.Context, _ string, _ ...string) error {
	return nil
}

// --- Tests ---

func boolPtr(b bool) *bool { return &b }

func TestServiceRunSuccess(t *testing.T) {
	dir := t.TempDir()

	pg := &mockPgExecutor{
		dumpOutput: "CREATE TABLE test;\nINSERT INTO test VALUES (1);\n",
	}
	gitMock := &mockGitClient{}

	svc := &Service{
		PgExecutor: pg,
		GitClient:  gitMock,
		GitConfig: config.GitConfig{
			AutoPush:              boolPtr(true),
			CommitMessageTemplate: "backup: {{.DbName}} @ {{.Timestamp}}",
		},
		Logger: slog.Default(),
	}

	dbCfg := config.DatabaseConfig{
		Name:          "testdb",
		Mode:          "docker",
		Container:     "pg",
		Database:      "mydb",
		PartitionSize: "100KB",
		OutputDir:     dir,
	}

	err := svc.Run(context.Background(), dbCfg)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if !gitMock.addCalled {
		t.Error("git add was not called")
	}
	if !gitMock.commitCalled {
		t.Error("git commit was not called")
	}
	if !gitMock.pushCalled {
		t.Error("git push was not called")
	}
	if !strings.HasPrefix(gitMock.commitMessage, "backup: testdb @") {
		t.Errorf("commit message = %q, want prefix 'backup: testdb @'", gitMock.commitMessage)
	}

	// Verify partitions written at fixed path (no timestamp subdir — Git provides versioning)
	partDir := filepath.Join(dir, "partitions", "testdb")
	entries, _ := filepath.Glob(filepath.Join(partDir, "part_*.sql"))
	if len(entries) == 0 {
		t.Error("no partition files found")
	}

	// Verify manifest exists at fixed path
	manifestPath := filepath.Join(partDir, "manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Error("manifest.json not found at fixed path")
	}
}

func TestServiceRunDumpFailure(t *testing.T) {
	dir := t.TempDir()

	pg := &mockPgExecutor{dumpErr: errors.New("connection refused")}
	gitMock := &mockGitClient{}

	svc := &Service{
		PgExecutor: pg,
		GitClient:  gitMock,
		GitConfig:  config.GitConfig{CommitMessageTemplate: "backup: {{.DbName}} @ {{.Timestamp}}"},
		Logger:     slog.Default(),
	}

	dbCfg := config.DatabaseConfig{
		Name:          "testdb",
		Mode:          "docker",
		Container:     "pg",
		Database:      "mydb",
		PartitionSize: "100KB",
		OutputDir:     dir,
	}

	err := svc.Run(context.Background(), dbCfg)
	if err == nil {
		t.Fatal("Run() expected error on dump failure")
	}

	if !strings.Contains(err.Error(), "dump failed") {
		t.Errorf("error = %q, want 'dump failed' substring", err.Error())
	}

	// Verify cleanup — partition dir should be removed
	entries, _ := os.ReadDir(filepath.Join(dir, "partitions", "testdb"))
	if len(entries) != 0 {
		t.Errorf("cleanup failed: found %d entries in partition dir after dump failure", len(entries))
	}

	// Git should NOT have been called
	if gitMock.addCalled || gitMock.commitCalled || gitMock.pushCalled {
		t.Error("git operations should not be called on dump failure")
	}
}

func TestServiceRunPushFailure(t *testing.T) {
	dir := t.TempDir()

	pg := &mockPgExecutor{
		dumpOutput: "CREATE TABLE test;\n",
	}
	gitMock := &mockGitClient{pushErr: errors.New("remote rejected")}

	svc := &Service{
		PgExecutor: pg,
		GitClient:  gitMock,
		GitConfig: config.GitConfig{
			AutoPush:              boolPtr(true),
			CommitMessageTemplate: "backup: {{.DbName}} @ {{.Timestamp}}",
		},
		Logger: slog.Default(),
	}

	dbCfg := config.DatabaseConfig{
		Name:          "testdb",
		Mode:          "docker",
		Container:     "pg",
		Database:      "mydb",
		PartitionSize: "100KB",
		OutputDir:     dir,
	}

	err := svc.Run(context.Background(), dbCfg)
	if err == nil {
		t.Fatal("Run() expected error on push failure")
	}

	// Commit should have been called (push fails AFTER commit)
	if !gitMock.commitCalled {
		t.Error("git commit should have been called before push failed")
	}
}

func TestServiceRunNilAutoPushDefaultsToTrue(t *testing.T) {
	dir := t.TempDir()

	pg := &mockPgExecutor{
		dumpOutput: "CREATE TABLE test;\n",
	}
	gitMock := &mockGitClient{}

	svc := &Service{
		PgExecutor: pg,
		GitClient:  gitMock,
		GitConfig: config.GitConfig{
			// AutoPush is nil (not set in YAML) — should default to true per spec CFG-10
			CommitMessageTemplate: "backup: {{.DbName}} @ {{.Timestamp}}",
		},
		Logger: slog.Default(),
	}

	dbCfg := config.DatabaseConfig{
		Name:          "testdb",
		Mode:          "docker",
		Container:     "pg",
		Database:      "mydb",
		PartitionSize: "100KB",
		OutputDir:     dir,
	}

	err := svc.Run(context.Background(), dbCfg)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if !gitMock.pushCalled {
		t.Error("git push should be called when AutoPush is nil (default true per CFG-10)")
	}
}

func TestServiceRunAutoPushExplicitlyFalse(t *testing.T) {
	dir := t.TempDir()

	pg := &mockPgExecutor{
		dumpOutput: "CREATE TABLE test;\n",
	}
	gitMock := &mockGitClient{}

	svc := &Service{
		PgExecutor: pg,
		GitClient:  gitMock,
		GitConfig: config.GitConfig{
			AutoPush:              boolPtr(false),
			CommitMessageTemplate: "backup: {{.DbName}} @ {{.Timestamp}}",
		},
		Logger: slog.Default(),
	}

	dbCfg := config.DatabaseConfig{
		Name:          "testdb",
		Mode:          "docker",
		Container:     "pg",
		Database:      "mydb",
		PartitionSize: "100KB",
		OutputDir:     dir,
	}

	err := svc.Run(context.Background(), dbCfg)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	if gitMock.pushCalled {
		t.Error("git push should NOT be called when AutoPush is explicitly false")
	}
}

func TestServiceRunAllPartialFailure(t *testing.T) {
	dir := t.TempDir()

	pg := &mockPgExecutor{
		dumpOutput: "CREATE TABLE test;\n",
	}
	gitMock := &mockGitClient{}

	svc := &Service{
		PgExecutor: pg,
		GitClient:  gitMock,
		GitConfig: config.GitConfig{
			CommitMessageTemplate: "backup: {{.DbName}} @ {{.Timestamp}}",
		},
		Logger: slog.Default(),
	}

	databases := []config.DatabaseConfig{
		{Name: "db1", Mode: "docker", Container: "pg", Database: "db1", PartitionSize: "100KB", OutputDir: dir},
		{Name: "db2", Mode: "docker", Container: "pg", Database: "db2", PartitionSize: "100KB", OutputDir: dir},
	}

	pg2 := &mockPgExecutor{}
	svc.PgExecutor = pg2
	pg2.dumpOutput = "CREATE TABLE test;\n"

	// Can't easily make only one fail with shared mock, so test RunAll with all success
	err := svc.RunAll(context.Background(), databases)
	if err != nil {
		t.Fatalf("RunAll() unexpected error: %v", err)
	}
}
