package git

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newGoGitClient builds a GoGitClient pointing at dir with sensible defaults.
func newGoGitClient(dir string) *GoGitClient {
	return &GoGitClient{
		WorkDir: dir,
		Remote:  "origin",
		Branch:  "main",
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestGoGit_Init_CreatesRepo(t *testing.T) {
	dir := t.TempDir()
	client := newGoGitClient(dir)

	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify .git directory exists
	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		t.Fatal(".git directory not created")
	}
}

func TestGoGit_Init_Idempotent(t *testing.T) {
	dir := t.TempDir()
	client := newGoGitClient(dir)

	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("first Init failed: %v", err)
	}
	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("second Init (idempotent) failed: %v", err)
	}
}

func TestGoGit_ValidateRepo_ValidRepo(t *testing.T) {
	dir := t.TempDir()
	client := newGoGitClient(dir)

	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := client.ValidateRepo(context.Background()); err != nil {
		t.Fatalf("expected nil error for valid repo, got: %v", err)
	}
}

func TestGoGit_ValidateRepo_NotARepo(t *testing.T) {
	dir := t.TempDir() // empty dir, no init
	client := newGoGitClient(dir)

	err := client.ValidateRepo(context.Background())
	if err == nil {
		t.Fatal("expected error for non-repo directory, got nil")
	}
}

func TestGoGit_Add_StagesFiles(t *testing.T) {
	dir := t.TempDir()
	client := newGoGitClient(dir)

	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create a file to stage.
	filePath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Add using absolute path (as the real code does).
	if err := client.Add(context.Background(), filePath); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Verify via git status using the exec helper (needs git binary).
	skipIfNoGit(t)
	status := gitExec(t, dir, "status", "--porcelain")
	if status == "" {
		t.Fatal("expected staged file in status, got empty")
	}
}

func TestGoGit_Add_RelativePath(t *testing.T) {
	dir := t.TempDir()
	client := newGoGitClient(dir)

	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	filePath := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	// Add using relative path.
	if err := client.Add(context.Background(), "data.txt"); err != nil {
		t.Fatalf("Add with relative path failed: %v", err)
	}
}

func TestGoGit_Commit_CreatesCommit(t *testing.T) {
	skipIfNoGit(t) // need git binary for verification

	dir := t.TempDir()
	client := newGoGitClient(dir)

	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	filePath := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(filePath, []byte("readme"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := client.Add(context.Background(), filePath); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	const msg = "test commit from go-git"
	if err := client.Commit(context.Background(), msg); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify commit exists using git log.
	log := gitExec(t, dir, "log", "--oneline")
	if log == "" {
		t.Fatal("expected at least one commit in log, got empty")
	}
	if !containsStr(log, msg) {
		t.Fatalf("expected commit message %q in log, got:\n%s", msg, log)
	}
}

func TestGoGit_Restore_RecoversFile(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir()
	client := newGoGitClient(dir)

	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	filePath := filepath.Join(dir, "data.txt")

	// Create, add, commit with original content.
	if err := os.WriteFile(filePath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := client.Add(context.Background(), filePath); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := client.Commit(context.Background(), "initial"); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Overwrite with modified content.
	if err := os.WriteFile(filePath, []byte("modified"), 0o644); err != nil {
		t.Fatalf("overwrite file: %v", err)
	}

	// Restore should bring back "original".
	if err := client.Restore(context.Background(), filePath); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("expected file content 'original', got %q", string(got))
	}
}

func TestGoGit_Restore_SubdirectoryFile(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir()
	client := newGoGitClient(dir)

	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create a file in a subdirectory.
	subDir := filepath.Join(dir, "backups", "db1")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	filePath := filepath.Join(subDir, "part_0000.sql")
	if err := os.WriteFile(filePath, []byte("CREATE TABLE;"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := client.Add(context.Background(), filePath); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := client.Commit(context.Background(), "add partition"); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Modify the file.
	if err := os.WriteFile(filePath, []byte("DROP TABLE;"), 0o644); err != nil {
		t.Fatalf("overwrite file: %v", err)
	}

	// Restore should recover original.
	if err := client.Restore(context.Background(), filePath); err != nil {
		t.Fatalf("Restore failed: %v", err)
	}

	got, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != "CREATE TABLE;" {
		t.Fatalf("expected 'CREATE TABLE;', got %q", string(got))
	}
}

func TestGoGit_RemoteAdd(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir()
	client := newGoGitClient(dir)

	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := client.RemoteAdd(context.Background(), "origin", "git@github.com:test/repo.git"); err != nil {
		t.Fatalf("RemoteAdd failed: %v", err)
	}

	// Verify remote was added.
	output := gitExec(t, dir, "remote", "-v")
	if !containsStr(output, "git@github.com:test/repo.git") {
		t.Fatalf("remote not found in output: %s", output)
	}
}

func TestGoGit_RemoteAdd_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	client := newGoGitClient(dir)

	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := client.RemoteAdd(context.Background(), "origin", "git@github.com:test/repo.git"); err != nil {
		t.Fatalf("first RemoteAdd failed: %v", err)
	}

	err := client.RemoteAdd(context.Background(), "origin", "git@github.com:test/other.git")
	if err == nil {
		t.Fatal("expected error when adding duplicate remote, got nil")
	}
}

func TestGoGit_Add_DirectoryRecursive(t *testing.T) {
	dir := t.TempDir()
	client := newGoGitClient(dir)

	if err := client.Init(context.Background()); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create a directory with multiple files (like backup partitions).
	partDir := filepath.Join(dir, "backups", "mydb", "20240101-120000")
	if err := os.MkdirAll(partDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, name := range []string{"part_0000.sql", "part_0001.sql", "manifest.json"} {
		fp := filepath.Join(partDir, name)
		if err := os.WriteFile(fp, []byte("content-"+name), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Add the directory (this is what backup.Service does).
	if err := client.Add(context.Background(), partDir); err != nil {
		t.Fatalf("Add directory failed: %v", err)
	}

	if err := client.Commit(context.Background(), "add partitions"); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify all files were committed.
	skipIfNoGit(t)
	log := gitExec(t, dir, "log", "--stat", "--oneline")
	for _, name := range []string{"part_0000.sql", "part_0001.sql", "manifest.json"} {
		if !containsStr(log, name) {
			t.Fatalf("expected %s in commit log, got:\n%s", name, log)
		}
	}
}

// containsStr is a simple helper to check string inclusion in tests.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
