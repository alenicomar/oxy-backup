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
	validateRepoErr error
	restoreCalled   bool
	restorePaths    []string
	restoreErr      error
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
	return nil, nil
}

func (m *mockGitClient) CheckoutFiles(_ context.Context, _ string, _ ...string) error {
	return nil
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

// setupPartDir creates the full partition directory structure under a temp dir
// and returns the temp root (used as OutputDir) plus the partition dir path.
func setupPartDir(t *testing.T, dbName, timestamp string) (outputDir, partDir string) {
	t.Helper()

	outputDir = t.TempDir()
	partDir = filepath.Join(outputDir, "partitions", dbName, timestamp)
	if err := os.MkdirAll(partDir, 0755); err != nil {
		t.Fatalf("creating partition dir: %v", err)
	}
	return outputDir, partDir
}

func newTestService(pg *mockPgExecutor, git *mockGitClient) *Service {
	return &Service{
		PgExecutor: pg,
		GitClient:  git,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// --- Service.Run tests ---

func TestServiceRunSuccess(t *testing.T) {
	const (
		dbName    = "testdb"
		timestamp = "20260318-140000"
	)

	outputDir, partDir := setupPartDir(t, dbName, timestamp)

	parts := []backup.PartitionInfo{
		{Filename: "part_0001.sql", SizeBytes: 19},
		{Filename: "part_0002.sql", SizeBytes: 29},
	}
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

	err := svc.Run(context.Background(), dbCfg, timestamp)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// LoadDatabase should have been called
	if !pg.loadCalled {
		t.Error("PgExecutor.LoadDatabase was not called")
	}

	// Destination file should be removed after successful load
	destPath := filepath.Join(partDir, "restore_"+dbName+"_"+timestamp+".sql")
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Errorf("destination file %s should be removed after success", destPath)
	}

	// Git restore should have been called with the correct partition paths
	if !gitMock.restoreCalled {
		t.Error("GitClient.Restore was not called")
	}

	expectedPaths := []string{
		filepath.Join(partDir, "part_0001.sql"),
		filepath.Join(partDir, "part_0002.sql"),
	}
	if len(gitMock.restorePaths) != len(expectedPaths) {
		t.Fatalf("Restore paths count = %d, want %d", len(gitMock.restorePaths), len(expectedPaths))
	}
	for i, got := range gitMock.restorePaths {
		if got != expectedPaths[i] {
			t.Errorf("Restore path[%d] = %q, want %q", i, got, expectedPaths[i])
		}
	}
}

func TestServiceRunMissingManifest(t *testing.T) {
	const (
		dbName    = "testdb"
		timestamp = "20260318-140000"
	)

	outputDir, _ := setupPartDir(t, dbName, timestamp)
	// No manifest.json created

	pg := &mockPgExecutor{}
	gitMock := &mockGitClient{}
	svc := newTestService(pg, gitMock)

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	err := svc.Run(context.Background(), dbCfg, timestamp)
	if err == nil {
		t.Fatal("Run() expected error when manifest is missing")
	}

	if got := err.Error(); !contains(got, "loading manifest") {
		t.Errorf("error = %q, want substring %q", got, "loading manifest")
	}

	if pg.loadCalled {
		t.Error("LoadDatabase should not be called when manifest is missing")
	}
}

func TestServiceRunPartitionCountMismatch(t *testing.T) {
	const (
		dbName    = "testdb"
		timestamp = "20260318-140000"
	)

	outputDir, partDir := setupPartDir(t, dbName, timestamp)

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

	err := svc.Run(context.Background(), dbCfg, timestamp)
	if err == nil {
		t.Fatal("Run() expected error for partition count mismatch")
	}

	if got := err.Error(); !contains(got, "partition count mismatch") {
		t.Errorf("error = %q, want substring %q", got, "partition count mismatch")
	}

	if pg.loadCalled {
		t.Error("LoadDatabase should not be called when partitions are missing")
	}
}

func TestServiceRunLoadDatabaseFailure(t *testing.T) {
	const (
		dbName    = "testdb"
		timestamp = "20260318-140000"
	)

	outputDir, partDir := setupPartDir(t, dbName, timestamp)

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

	err := svc.Run(context.Background(), dbCfg, timestamp)
	if err == nil {
		t.Fatal("Run() expected error when LoadDatabase fails")
	}

	if got := err.Error(); !contains(got, "database load failed") {
		t.Errorf("error = %q, want substring %q", got, "database load failed")
	}

	// Destination file should be PRESERVED for debugging
	destPath := filepath.Join(partDir, "restore_"+dbName+"_"+timestamp+".sql")
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		t.Error("destination file should be preserved after LoadDatabase failure")
	}

	// Git restore should NOT have been called
	if gitMock.restoreCalled {
		t.Error("GitClient.Restore should not be called when LoadDatabase fails")
	}
}

func TestServiceRunGitRestoreFailureNonFatal(t *testing.T) {
	const (
		dbName    = "testdb"
		timestamp = "20260318-140000"
	)

	outputDir, partDir := setupPartDir(t, dbName, timestamp)

	parts := []backup.PartitionInfo{
		{Filename: "part_0001.sql", SizeBytes: 19},
	}
	createTestManifest(t, partDir, parts)
	createTestPartition(t, partDir, "part_0001.sql", "CREATE TABLE test;\n")

	pg := &mockPgExecutor{}
	gitMock := &mockGitClient{restoreErr: errors.New("not a git repository")}
	svc := newTestService(pg, gitMock)

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	err := svc.Run(context.Background(), dbCfg, timestamp)
	if err != nil {
		t.Fatalf("Run() returned error %v, want nil (git restore failure should be non-fatal)", err)
	}

	if !gitMock.restoreCalled {
		t.Error("GitClient.Restore should still have been called")
	}
}

func TestServiceRunGitValidateFailureNonFatal(t *testing.T) {
	const (
		dbName    = "testdb"
		timestamp = "20260318-140000"
	)

	outputDir, partDir := setupPartDir(t, dbName, timestamp)

	parts := []backup.PartitionInfo{
		{Filename: "part_0001.sql", SizeBytes: 19},
	}
	createTestManifest(t, partDir, parts)
	createTestPartition(t, partDir, "part_0001.sql", "CREATE TABLE test;\n")

	pg := &mockPgExecutor{}
	gitMock := &mockGitClient{validateRepoErr: errors.New("not a git repository")}
	svc := newTestService(pg, gitMock)

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	err := svc.Run(context.Background(), dbCfg, timestamp)
	if err != nil {
		t.Fatalf("Run() returned error %v, want nil (git validate failure should be non-fatal)", err)
	}

	// Load should still have happened
	if !pg.loadCalled {
		t.Error("LoadDatabase should be called even when ValidateRepo fails")
	}
}

// --- ListTimestamps tests ---

func TestListTimestampsMultipleDirs(t *testing.T) {
	const dbName = "testdb"

	outputDir := t.TempDir()
	baseDir := filepath.Join(outputDir, "partitions", dbName)

	// Create 3 timestamp dirs, each with a manifest.json
	expected := []string{"20260318-100000", "20260318-120000", "20260318-140000"}
	for _, ts := range expected {
		dir := filepath.Join(baseDir, ts)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("creating dir: %v", err)
		}
		createTestManifest(t, dir, []backup.PartitionInfo{
			{Filename: "part_0001.sql", SizeBytes: 10},
		})
	}

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	timestamps, err := ListTimestamps(dbCfg)
	if err != nil {
		t.Fatalf("ListTimestamps() error: %v", err)
	}

	if len(timestamps) != len(expected) {
		t.Fatalf("ListTimestamps() returned %d timestamps, want %d", len(timestamps), len(expected))
	}

	// os.ReadDir returns sorted entries, so the order should match
	for i, got := range timestamps {
		if got != expected[i] {
			t.Errorf("timestamp[%d] = %q, want %q", i, got, expected[i])
		}
	}
}

func TestListTimestampsEmptyDir(t *testing.T) {
	const dbName = "testdb"

	outputDir := t.TempDir()
	baseDir := filepath.Join(outputDir, "partitions", dbName)
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	timestamps, err := ListTimestamps(dbCfg)
	if err != nil {
		t.Fatalf("ListTimestamps() error: %v", err)
	}

	if len(timestamps) != 0 {
		t.Errorf("ListTimestamps() = %v, want empty/nil slice", timestamps)
	}
}

func TestListTimestampsNonExistentDir(t *testing.T) {
	dbCfg := config.DatabaseConfig{
		Name:      "nope",
		OutputDir: filepath.Join(t.TempDir(), "does-not-exist"),
	}

	timestamps, err := ListTimestamps(dbCfg)
	if err != nil {
		t.Fatalf("ListTimestamps() error: %v, want nil for non-existent dir", err)
	}

	if timestamps != nil {
		t.Errorf("ListTimestamps() = %v, want nil", timestamps)
	}
}

func TestListTimestampsDirsWithoutManifest(t *testing.T) {
	const dbName = "testdb"

	outputDir := t.TempDir()
	baseDir := filepath.Join(outputDir, "partitions", dbName)

	// Create dirs but no manifest.json inside them
	for _, ts := range []string{"20260318-100000", "20260318-120000"} {
		dir := filepath.Join(baseDir, ts)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("creating dir: %v", err)
		}
		// Write a random file, NOT manifest.json
		createTestPartition(t, dir, "part_0001.sql", "data")
	}

	dbCfg := config.DatabaseConfig{
		Name:      dbName,
		OutputDir: outputDir,
	}

	timestamps, err := ListTimestamps(dbCfg)
	if err != nil {
		t.Fatalf("ListTimestamps() error: %v", err)
	}

	if len(timestamps) != 0 {
		t.Errorf("ListTimestamps() = %v, want empty/nil slice for dirs without manifest", timestamps)
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
