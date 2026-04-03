// Package git defines the port (interface) and adapter for Git operations.
package git

import (
	"context"
	"time"
)

// CommitInfo holds metadata about a single Git commit.
type CommitInfo struct {
	SHA      string    // Full 40-char hex SHA.
	ShortSHA string    // First 7 characters of SHA.
	Date     time.Time // Author date in UTC.
	Message  string    // First line of the commit message.
}

// GitClient abstracts Git operations for testability.
// Implementations may use os/exec (v1) or go-git (v2).
type GitClient interface {
	// ValidateRepo checks that the working directory is a git repository.
	ValidateRepo(ctx context.Context) error

	// Add stages files at the given paths.
	Add(ctx context.Context, paths ...string) error

	// Commit creates a commit with the given message.
	Commit(ctx context.Context, message string) error

	// Push pushes to the configured remote/branch.
	// On failure, implementations SHOULD attempt pull --rebase and retry once.
	Push(ctx context.Context) error

	// Restore recovers files from Git index (git restore).
	Restore(ctx context.Context, paths ...string) error

	// Init initializes a new git repository in the working directory.
	Init(ctx context.Context) error

	// RemoteAdd adds a named remote with the given URL.
	RemoteAdd(ctx context.Context, name, url string) error

	// Log returns commit history filtered by path, newest first.
	// limit <= 0 means no limit.
	Log(ctx context.Context, path string, limit int) ([]CommitInfo, error)

	// CheckoutFiles restores files from a specific commit SHA.
	// Equivalent to: git checkout <sha> -- <paths...>
	CheckoutFiles(ctx context.Context, sha string, paths ...string) error
}
