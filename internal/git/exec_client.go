package git

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
)

// ExecGitClient implements GitClient using the git CLI via os/exec.
type ExecGitClient struct {
	WorkDir string // working directory for git commands
	Remote  string // e.g. "origin"
	Branch  string // e.g. "main"
	Logger  *slog.Logger
}

var _ GitClient = (*ExecGitClient)(nil)

// ValidateRepo checks that the working directory is a git repository.
func (g *ExecGitClient) ValidateRepo(ctx context.Context) error {
	_, err := g.run(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return fmt.Errorf("not a git repository (or any parent): %w", err)
	}
	return nil
}

// Add stages files: git add <paths...>
func (g *ExecGitClient) Add(ctx context.Context, paths ...string) error {
	args := append([]string{"add"}, paths...)
	_, err := g.run(ctx, args...)
	return err
}

// Commit creates a commit: git commit -m <message>
func (g *ExecGitClient) Commit(ctx context.Context, message string) error {
	_, err := g.run(ctx, "commit", "-m", message)
	return err
}

// Push pushes to remote/branch. On failure, attempts pull --rebase and retries once.
func (g *ExecGitClient) Push(ctx context.Context) error {
	_, err := g.run(ctx, "push", g.Remote, g.Branch)
	if err == nil {
		return nil
	}

	g.Logger.Warn("push failed, attempting pull --rebase and retry",
		"error", err,
		"remote", g.Remote,
		"branch", g.Branch,
	)

	// Attempt pull --rebase
	if _, pullErr := g.run(ctx, "pull", "--rebase", g.Remote, g.Branch); pullErr != nil {
		return fmt.Errorf("push failed and pull --rebase also failed: push=%w, pull=%v", err, pullErr)
	}

	// Retry push
	if _, retryErr := g.run(ctx, "push", g.Remote, g.Branch); retryErr != nil {
		return fmt.Errorf("push failed after pull --rebase: %w", retryErr)
	}

	g.Logger.Info("push succeeded after pull --rebase")
	return nil
}

// Restore recovers files from git index: git restore <paths...>
func (g *ExecGitClient) Restore(ctx context.Context, paths ...string) error {
	args := append([]string{"restore"}, paths...)
	_, err := g.run(ctx, args...)
	return err
}

func (g *ExecGitClient) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.WorkDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	g.Logger.Debug("git command", "args", args)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s failed: %w\nstderr: %s", args[0], err, stderr.String())
	}

	return stdout.String(), nil
}
