package git

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func skipIfNoGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found in PATH, skipping")
	}
}

// gitExec runs a git command directly (not through ExecGitClient) for test
// setup/assertions. Returns trimmed stdout.
func gitExec(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\noutput: %s", args[0], err, string(out))
	}
	return strings.TrimSpace(string(out))
}

// initRepo creates a git repo in dir with user config so commits work.
func initRepo(t *testing.T, dir string) {
	t.Helper()
	gitExec(t, dir, "init", "-b", "main")
	gitExec(t, dir, "config", "user.email", "test@test.com")
	gitExec(t, dir, "config", "user.name", "Test")
}

// initBareRepo creates a bare git repo in a temp dir and returns its path.
func initBareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitExec(t, dir, "init", "--bare", "-b", "main")
	return dir
}

// cloneRepo clones bareRepoPath into a new temp dir and returns the clone path.
func cloneRepo(t *testing.T, bareRepoPath string) string {
	t.Helper()
	dir := t.TempDir()
	// Clone into a subdirectory so t.TempDir() itself isn't the .git root conflict
	clone := filepath.Join(dir, "repo")
	gitExec(t, dir, "clone", bareRepoPath, "repo")
	gitExec(t, clone, "config", "user.email", "test@test.com")
	gitExec(t, clone, "config", "user.name", "Test")
	return clone
}

// newClient builds an ExecGitClient pointing at dir with sensible defaults.
func newClient(dir string) *ExecGitClient {
	return &ExecGitClient{
		WorkDir: dir,
		Remote:  "origin",
		Branch:  "main",
		Logger:  slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestValidateRepo_ValidRepo(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir()
	initRepo(t, dir)

	client := newClient(dir)
	if err := client.ValidateRepo(context.Background()); err != nil {
		t.Fatalf("expected nil error for valid repo, got: %v", err)
	}
}

func TestValidateRepo_NotARepo(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir() // empty dir, no git init

	client := newClient(dir)
	err := client.ValidateRepo(context.Background())
	if err == nil {
		t.Fatal("expected error for non-repo directory, got nil")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("expected error to contain 'not a git repository', got: %v", err)
	}
}

func TestAdd_StagesFiles(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir()
	initRepo(t, dir)

	// Create a file to stage.
	filePath := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	client := newClient(dir)
	if err := client.Add(context.Background(), "hello.txt"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Verify the file is staged.
	status := gitExec(t, dir, "status", "--porcelain")
	if !strings.Contains(status, "A  hello.txt") {
		t.Fatalf("expected staged file 'A  hello.txt' in status, got:\n%s", status)
	}
}

func TestCommit_CreatesCommit(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir()
	initRepo(t, dir)

	filePath := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(filePath, []byte("readme"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	client := newClient(dir)
	if err := client.Add(context.Background(), "readme.txt"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	const msg = "test commit"
	if err := client.Commit(context.Background(), msg); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	log := gitExec(t, dir, "log", "--oneline")
	if !strings.Contains(log, msg) {
		t.Fatalf("expected commit message %q in log, got:\n%s", msg, log)
	}
}

func TestPush_SuccessWithBareRemote(t *testing.T) {
	skipIfNoGit(t)

	bare := initBareRepo(t)
	clone := cloneRepo(t, bare)

	// Create, add, commit a file in the clone.
	filePath := filepath.Join(clone, "file.txt")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitExec(t, clone, "add", "file.txt")
	gitExec(t, clone, "commit", "-m", "initial")

	client := newClient(clone)
	if err := client.Push(context.Background()); err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	// Verify the bare repo received the commit.
	log := gitExec(t, bare, "log", "--oneline")
	if !strings.Contains(log, "initial") {
		t.Fatalf("expected commit 'initial' in bare repo log, got:\n%s", log)
	}
}

func TestPush_RetryAfterConflict(t *testing.T) {
	skipIfNoGit(t)

	bare := initBareRepo(t)

	// Clone A pushes first.
	cloneA := cloneRepo(t, bare)
	fileA := filepath.Join(cloneA, "fileA.txt")
	if err := os.WriteFile(fileA, []byte("from A"), 0o644); err != nil {
		t.Fatalf("write fileA: %v", err)
	}
	gitExec(t, cloneA, "add", "fileA.txt")
	gitExec(t, cloneA, "commit", "-m", "commit from A")
	gitExec(t, cloneA, "push", "origin", "main")

	// Clone B commits a DIFFERENT file, then pushes (should fail, rebase, retry).
	cloneB := cloneRepo(t, bare)
	fileB := filepath.Join(cloneB, "fileB.txt")
	if err := os.WriteFile(fileB, []byte("from B"), 0o644); err != nil {
		t.Fatalf("write fileB: %v", err)
	}
	gitExec(t, cloneB, "add", "fileB.txt")
	gitExec(t, cloneB, "commit", "-m", "commit from B")

	client := newClient(cloneB)
	if err := client.Push(context.Background()); err != nil {
		t.Fatalf("Push with retry should have succeeded, got: %v", err)
	}

	// Both commits should be in the bare repo now.
	log := gitExec(t, bare, "log", "--oneline")
	if !strings.Contains(log, "commit from A") {
		t.Fatalf("expected 'commit from A' in bare repo log, got:\n%s", log)
	}
	if !strings.Contains(log, "commit from B") {
		t.Fatalf("expected 'commit from B' in bare repo log, got:\n%s", log)
	}
}

func TestRestore_RecoversFile(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir()
	initRepo(t, dir)

	filePath := filepath.Join(dir, "data.txt")

	// Create, add, commit with original content.
	if err := os.WriteFile(filePath, []byte("original"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitExec(t, dir, "add", "data.txt")
	gitExec(t, dir, "commit", "-m", "initial")

	// Overwrite with modified content.
	if err := os.WriteFile(filePath, []byte("modified"), 0o644); err != nil {
		t.Fatalf("overwrite file: %v", err)
	}

	client := newClient(dir)
	if err := client.Restore(context.Background(), "data.txt"); err != nil {
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

func TestContextCancellation(t *testing.T) {
	skipIfNoGit(t)

	dir := t.TempDir()
	initRepo(t, dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := newClient(dir)
	err := client.ValidateRepo(ctx)
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}
