package restore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alenicomar/oxy-backup/internal/backup"
	"github.com/alenicomar/oxy-backup/internal/config"
	"github.com/alenicomar/oxy-backup/internal/git"
)

// --- Mocks ---

type mockPgExecutor struct {
	loadCalled   bool
	loadFilePath string
	loadErr      error
}

func (m *mockPgExecutor) DumpDatabase(_ context.Context, _ config.DatabaseConfig) (io.Reader, error) {
	return nil, nil
}

func (m *mockPgExecutor) LoadDatabase(_ context.Context, _ config.DatabaseConfig, sqlFilePath string) error {
	m.loadCalled = true
	m.loadFilePath = sqlFilePath
	return m.loadErr
}

type mockGitClient struct {
	validateRepoErr    error
	restoreCalled      bool
	restorePaths       []string
	restoreErr         error
	checkoutFilesCalls []checkoutFilesCall
	checkoutFilesErr   error
	logResult          []git.CommitInfo
	logErr             error
}

type checkoutFilesCall struct {
	SHA   string
	Paths []string
}

func (m *mockGitClient) ValidateRepo(_ context.Context) error {
	return m.validateRepoErr
}

func (m *mockGitClient) Add(_ context.Context, _ ...string) error { return nil }

func (m *mockGitClient) Commit(_ context.Context, _ string) error { return nil }

func (m *mockGitClient) Push(_ context.Context) error { return nil }

func (m *mockGitClient) Restore(_ context.Context, paths ...string) error {
	m.restoreCalled = true
	m.restorePaths = paths
	return m.restoreErr
}

func (m *mockGitClient) Init(_ context.Context) error { return nil }

func (m *mockGitClient) RemoteAdd(_ context.Context, _, _ string) error { return nil }

func (m *mockGitClient) Log(_ context.Context, _ string, _ int) ([]git.CommitInfo, error) {
	return m.logResult, m.logErr
}

// CheckoutFiles simulates checking out files by creating them on disk
// if the mock doesn't have an error set.
func (m *mockGitClient) CheckoutFiles(_ context.Context, sha string, paths ...string) error {
	m.checkoutFilesCalls = append(m.checkoutFilesCalls, checkoutFilesCall{SHA: sha, Paths: paths})
	return m.checkoutFilesErr
}

// --- Helpers ---

func createTestManifest(t *testing.T, dir string, parts []backup.PartitionInfo) {
	t.Helper()

	m := backup.Manifest{
		DbName:    "testdb",
		PartCount: len(parts),
		Parts:     parts,
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshaling test manifest: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644); err != nil {
		t.Fatalf("writing test manifest: %v", err)
	}
}

func createTestPartition(t *testing.T, dir, filename, content string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, filename), []byte(content), 0644); err != nil {
		t.Fatalf("writing test partition %s: %v", filename, err)
	}
}

// setupPartDir creates the fixed partition directory structure under a temp dir.
func setupPartDir(t *testing.T, dbName string) (outputDir, partDir string) {
	t.Helper()

	outputDir = t.TempDir()
	partDir = filepath.Join(outputDir, "partitions", dbName)
	if err := os.MkdirAll(partDir, 0755); err != nil {
		t.Fatalf("creating partition dir: %v", err)
	}
	return outputDir, partDir
}

func newTestService(pg *mockPgExecutor, gitClient *mockGitClient) *Service {
	return &Service{
		PgExecutor: pg,
		GitClient:  gitClient,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// --- Service.Run tests ---

const testSHA = "abc1234567890def1234567890abcdef12345678"

func TestServiceRunSuccess(t *testing.T) {
	const dbName = "testdb"

	outputDir, partDir := setupPartDir(t, dbName)

	parts := []backup.PartitionInfo{
		{Filename: "part_0001.sql", SizeBytes: 19},
		{Filename: "part_0002.sql", SizeBytes: 29},
	}

	// Pre-create files on disk (simulating what CheckoutFiles would do)
	createTestManifest(t, partDir, parts)
	createTestPartition(t, partDir, "part_0001.sql", "CREATE TABLE test;\n")
	createTestPartition(t, partDir, "part_0002.sql", "INSERT INTO test VALUES (1);\n")

	pg := &mockPgExecutor{}
	gitMock := &mockGitClient{}
	svc := newTestService(pg, gitMock)

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	err := svc.Run(context.Background(), dbCfg, testSHA)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// CheckoutFiles should have been called (manifest + partitions)
	if len(gitMock.checkoutFilesCalls) != 2 {
		t.Fatalf("CheckoutFiles called %d times, want 2", len(gitMock.checkoutFilesCalls))
	}

	// First call: manifest only
	if gitMock.checkoutFilesCalls[0].SHA != testSHA {
		t.Errorf("first CheckoutFiles SHA = %q, want %q", gitMock.checkoutFilesCalls[0].SHA, testSHA)
	}
	if len(gitMock.checkoutFilesCalls[0].Paths) != 1 {
		t.Errorf("first CheckoutFiles paths count = %d, want 1 (manifest)", len(gitMock.checkoutFilesCalls[0].Paths))
	}

	// Second call: partition files
	if len(gitMock.checkoutFilesCalls[1].Paths) != 2 {
		t.Errorf("second CheckoutFiles paths count = %d, want 2 (partitions)", len(gitMock.checkoutFilesCalls[1].Paths))
	}

	// LoadDatabase should have been called
	if !pg.loadCalled {
		t.Error("PgExecutor.LoadDatabase was not called")
	}

	// Destination file should be removed after successful load
	destPath := filepath.Join(partDir, "restore_"+dbName+".sql")
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Errorf("destination file %s should be removed after success", destPath)
	}

	// Git restore should have been called to reset working tree to HEAD
	if !gitMock.restoreCalled {
		t.Error("GitClient.Restore was not called")
	}

	// Restore should include manifest + all partition paths
	expectedRestoreCount := 1 + len(parts) // manifest + partitions
	if len(gitMock.restorePaths) != expectedRestoreCount {
		t.Fatalf("Restore paths count = %d, want %d", len(gitMock.restorePaths), expectedRestoreCount)
	}
}

func TestServiceRunCheckoutManifestFailure(t *testing.T) {
	const dbName = "testdb"

	outputDir, _ := setupPartDir(t, dbName)

	pg := &mockPgExecutor{}
	gitMock := &mockGitClient{
		checkoutFilesErr: errors.New("commit not found"),
	}
	svc := newTestService(pg, gitMock)

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	err := svc.Run(context.Background(), dbCfg, testSHA)
	if err == nil {
		t.Fatal("Run() expected error when CheckoutFiles fails")
	}

	if !contains(err.Error(), "checking out manifest") {
		t.Errorf("error = %q, want substring %q", err.Error(), "checking out manifest")
	}

	if pg.loadCalled {
		t.Error("LoadDatabase should not be called when checkout fails")
	}
}

func TestServiceRunGitValidateFailure(t *testing.T) {
	const dbName = "testdb"

	outputDir, _ := setupPartDir(t, dbName)

	pg := &mockPgExecutor{}
	gitMock := &mockGitClient{
		validateRepoErr: errors.New("not a git repository"),
	}
	svc := newTestService(pg, gitMock)

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	err := svc.Run(context.Background(), dbCfg, testSHA)
	if err == nil {
		t.Fatal("Run() expected error when git validation fails")
	}

	if !contains(err.Error(), "git validation failed") {
		t.Errorf("error = %q, want substring %q", err.Error(), "git validation failed")
	}

	if pg.loadCalled {
		t.Error("LoadDatabase should not be called when git validation fails")
	}
}

func TestServiceRunPartitionCountMismatch(t *testing.T) {
	const dbName = "testdb"

	outputDir, partDir := setupPartDir(t, dbName)

	// Manifest declares 3 parts but we only create 1 file
	parts := []backup.PartitionInfo{
		{Filename: "part_0001.sql", SizeBytes: 10},
		{Filename: "part_0002.sql", SizeBytes: 10},
		{Filename: "part_0003.sql", SizeBytes: 10},
	}
	createTestManifest(t, partDir, parts)
	createTestPartition(t, partDir, "part_0001.sql", "some data\n")
	// part_0002.sql and part_0003.sql intentionally missing

	pg := &mockPgExecutor{}
	gitMock := &mockGitClient{}
	svc := newTestService(pg, gitMock)

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	err := svc.Run(context.Background(), dbCfg, testSHA)
	if err == nil {
		t.Fatal("Run() expected error for partition count mismatch")
	}

	if !contains(err.Error(), "partition count mismatch") {
		t.Errorf("error = %q, want substring %q", err.Error(), "partition count mismatch")
	}

	if pg.loadCalled {
		t.Error("LoadDatabase should not be called when partitions are missing")
	}
}

func TestServiceRunLoadDatabaseFailure(t *testing.T) {
	const dbName = "testdb"

	outputDir, partDir := setupPartDir(t, dbName)

	parts := []backup.PartitionInfo{
		{Filename: "part_0001.sql", SizeBytes: 19},
	}
	createTestManifest(t, partDir, parts)
	createTestPartition(t, partDir, "part_0001.sql", "CREATE TABLE test;\n")

	pg := &mockPgExecutor{loadErr: errors.New("connection refused")}
	gitMock := &mockGitClient{}
	svc := newTestService(pg, gitMock)

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	err := svc.Run(context.Background(), dbCfg, testSHA)
	if err == nil {
		t.Fatal("Run() expected error when LoadDatabase fails")
	}

	if !contains(err.Error(), "database load failed") {
		t.Errorf("error = %q, want substring %q", err.Error(), "database load failed")
	}

	// Destination file should be PRESERVED for debugging
	destPath := filepath.Join(partDir, "restore_"+dbName+".sql")
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		t.Error("destination file should be preserved after LoadDatabase failure")
	}

	// Git restore should NOT have been called (failed before cleanup step)
	if gitMock.restoreCalled {
		t.Error("GitClient.Restore should not be called when LoadDatabase fails")
	}
}

func TestServiceRunGitRestoreFailureNonFatal(t *testing.T) {
	const dbName = "testdb"

	outputDir, partDir := setupPartDir(t, dbName)

	parts := []backup.PartitionInfo{
		{Filename: "part_0001.sql", SizeBytes: 19},
	}
	createTestManifest(t, partDir, parts)
	createTestPartition(t, partDir, "part_0001.sql", "CREATE TABLE test;\n")

	pg := &mockPgExecutor{}
	gitMock := &mockGitClient{restoreErr: errors.New("git restore failed")}
	svc := newTestService(pg, gitMock)

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	err := svc.Run(context.Background(), dbCfg, testSHA)
	if err != nil {
		t.Fatalf("Run() returned error %v, want nil (git restore failure should be non-fatal)", err)
	}

	if !gitMock.restoreCalled {
		t.Error("GitClient.Restore should still have been called")
	}
}

// --- ListBackups tests ---

func TestListBackupsSuccess(t *testing.T) {
	now := time.Now().UTC()

	gitMock := &mockGitClient{
		logResult: []git.CommitInfo{
			{SHA: "abc1234567890", ShortSHA: "abc1234", Date: now, Message: "backup: testdb @ 20260318-140000"},
			{SHA: "def5678901234", ShortSHA: "def5678", Date: now.Add(-24 * time.Hour), Message: "backup: testdb @ 20260317-140000"},
		},
	}

	dbCfg := config.DatabaseConfig{
		Name:      "testdb",
		OutputDir: "/tmp/test",
	}

	commits, err := ListBackups(context.Background(), gitMock, dbCfg, 10)
	if err != nil {
		t.Fatalf("ListBackups() error: %v", err)
	}

	if len(commits) != 2 {
		t.Fatalf("ListBackups() returned %d commits, want 2", len(commits))
	}

	if commits[0].ShortSHA != "abc1234" {
		t.Errorf("commits[0].ShortSHA = %q, want %q", commits[0].ShortSHA, "abc1234")
	}
	if commits[1].ShortSHA != "def5678" {
		t.Errorf("commits[1].ShortSHA = %q, want %q", commits[1].ShortSHA, "def5678")
	}
}

func TestListBackupsEmpty(t *testing.T) {
	gitMock := &mockGitClient{
		logResult: nil,
	}

	dbCfg := config.DatabaseConfig{
		Name:      "testdb",
		OutputDir: "/tmp/test",
	}

	commits, err := ListBackups(context.Background(), gitMock, dbCfg, 0)
	if err != nil {
		t.Fatalf("ListBackups() error: %v", err)
	}

	if len(commits) != 0 {
		t.Errorf("ListBackups() = %v, want empty slice", commits)
	}
}

func TestListBackupsError(t *testing.T) {
	gitMock := &mockGitClient{
		logErr: errors.New("not a git repository"),
	}

	dbCfg := config.DatabaseConfig{
		Name:      "testdb",
		OutputDir: "/tmp/test",
	}

	_, err := ListBackups(context.Background(), gitMock, dbCfg, 0)
	if err == nil {
		t.Fatal("ListBackups() expected error")
	}

	if !contains(err.Error(), "listing backups") {
		t.Errorf("error = %q, want substring %q", err.Error(), "listing backups")
	}
}

// --- shortSHA tests ---

func TestShortSHA(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc1234567890def1234567890abcdef12345678", "abc1234"},
		{"abc1234", "abc1234"},
		{"abc", "abc"},
		{"", ""},
	}

	for _, tt := range tests {
		got := shortSHA(tt.input)
		if got != tt.want {
			t.Errorf("shortSHA(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// contains is a small helper to avoid importing strings for one check.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
