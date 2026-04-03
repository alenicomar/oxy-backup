package git

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
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

// Init initializes a new git repository in the working directory.
func (g *ExecGitClient) Init(ctx context.Context) error {
	_, err := g.run(ctx, "init")
	return err
}

// RemoteAdd adds a named remote with the given URL.
func (g *ExecGitClient) RemoteAdd(ctx context.Context, name, url string) error {
	_, err := g.run(ctx, "remote", "add", name, url)
	return err
}

// Log returns commit history filtered by path, newest first.
// Uses: git log --format=<format> [--max-count=N] -- <path>
func (g *ExecGitClient) Log(ctx context.Context, path string, limit int) ([]CommitInfo, error) {
	// Format: SHA<tab>short SHA<tab>ISO date<tab>subject
	const logFormat = "%H\t%h\t%aI\t%s"

	args := []string{"log", "--format=" + logFormat}
	if limit > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", limit))
	}
	if path != "" {
		args = append(args, "--", path)
	}

	out, err := g.run(ctx, args...)
	if err != nil {
		// Empty repo (no commits yet) — return empty slice, not error.
		if strings.Contains(err.Error(), "does not have any commits") {
			return nil, nil
		}
		return nil, fmt.Errorf("git log: %w", err)
	}

	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}

	lines := strings.Split(out, "\n")
	commits := make([]CommitInfo, 0, len(lines))

	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			continue
		}

		date, parseErr := time.Parse(time.RFC3339, parts[2])
		if parseErr != nil {
			// Best effort — use zero time.
			date = time.Time{}
		}

		commits = append(commits, CommitInfo{
			SHA:      parts[0],
			ShortSHA: parts[1],
			Date:     date.UTC(),
			Message:  parts[3],
		})
	}

	return commits, nil
}

// CheckoutFiles restores files from a specific commit SHA.
// Uses: git checkout <sha> -- <paths...>
func (g *ExecGitClient) CheckoutFiles(ctx context.Context, sha string, paths ...string) error {
	args := append([]string{"checkout", sha, "--"}, paths...)
	_, err := g.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("git checkout %s: %w", sha, err)
	}
	return nil
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
