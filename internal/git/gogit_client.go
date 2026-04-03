package git

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/skeema/knownhosts"
	"golang.org/x/crypto/ssh"
)

// GoGitClient implements GitClient using go-git with native SSH transport.
type GoGitClient struct {
	WorkDir        string // working directory (repo root)
	Remote         string // e.g. "origin"
	Branch         string // e.g. "main"
	SSHKeyPath     string // absolute path to SSH private key
	SSHKeyPassEnv  string // env var name holding key passphrase
	KnownHostsPath string // absolute path to known_hosts file
	Logger         *slog.Logger
}

var _ GitClient = (*GoGitClient)(nil)

// ValidateRepo checks that the working directory is a git repository.
func (g *GoGitClient) ValidateRepo(ctx context.Context) error {
	_, err := gogit.PlainOpen(g.WorkDir)
	if err != nil {
		return fmt.Errorf("not a git repository (or any parent): %w", err)
	}
	return nil
}

// Add stages files at the given paths.
func (g *GoGitClient) Add(ctx context.Context, paths ...string) error {
	repo, err := gogit.PlainOpen(g.WorkDir)
	if err != nil {
		return fmt.Errorf("opening repo: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting worktree: %w", err)
	}

	for _, p := range paths {
		// go-git expects paths relative to the worktree root.
		relPath, relErr := filepath.Rel(g.WorkDir, p)
		if relErr != nil {
			// If Rel fails, assume p is already relative.
			relPath = p
		}

		g.Logger.Debug("git add", "path", relPath)

		if _, addErr := wt.Add(relPath); addErr != nil {
			return fmt.Errorf("git add %s failed: %w", relPath, addErr)
		}
	}

	return nil
}

// Commit creates a commit with the given message.
func (g *GoGitClient) Commit(ctx context.Context, message string) error {
	repo, err := gogit.PlainOpen(g.WorkDir)
	if err != nil {
		return fmt.Errorf("opening repo: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("getting worktree: %w", err)
	}

	now := time.Now()
	_, err = wt.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  "oxy-backup",
			Email: "oxy-backup@automated",
			When:  now,
		},
	})
	if err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}

	return nil
}

// Push pushes to the configured remote/branch.
// On failure, attempts pull --rebase and retries once.
func (g *GoGitClient) Push(ctx context.Context) error {
	auth, err := g.sshAuth()
	if err != nil {
		return fmt.Errorf("ssh auth setup: %w", err)
	}

	repo, err := gogit.PlainOpen(g.WorkDir)
	if err != nil {
		return fmt.Errorf("opening repo: %w", err)
	}

	refSpec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", g.Branch, g.Branch))

	pushOpts := &gogit.PushOptions{
		RemoteName: g.Remote,
		RefSpecs:   []config.RefSpec{refSpec},
		Auth:       auth,
	}

	g.Logger.Debug("git push", "remote", g.Remote, "branch", g.Branch)

	err = repo.PushContext(ctx, pushOpts)
	if err == nil {
		return nil
	}

	// If already up to date, that's fine.
	if err == gogit.NoErrAlreadyUpToDate {
		g.Logger.Debug("push: already up to date")
		return nil
	}

	g.Logger.Warn("push failed, attempting pull --rebase and retry",
		"error", err,
		"remote", g.Remote,
		"branch", g.Branch,
	)

	// Pull with rebase
	wt, wtErr := repo.Worktree()
	if wtErr != nil {
		return fmt.Errorf("push failed and could not get worktree for pull: push=%w, wt=%v", err, wtErr)
	}

	pullOpts := &gogit.PullOptions{
		RemoteName:    g.Remote,
		ReferenceName: plumbing.NewBranchReferenceName(g.Branch),
		Auth:          auth,
	}

	if pullErr := wt.PullContext(ctx, pullOpts); pullErr != nil && pullErr != gogit.NoErrAlreadyUpToDate {
		return fmt.Errorf("push failed and pull also failed: push=%w, pull=%v", err, pullErr)
	}

	// Retry push
	if retryErr := repo.PushContext(ctx, pushOpts); retryErr != nil && retryErr != gogit.NoErrAlreadyUpToDate {
		return fmt.Errorf("push failed after pull: %w", retryErr)
	}

	g.Logger.Info("push succeeded after pull")
	return nil
}

// Restore recovers files from the Git index (equivalent to git restore).
// It reads each file's content from the latest commit (HEAD) and writes it back
// to the working directory, effectively discarding local modifications.
func (g *GoGitClient) Restore(ctx context.Context, paths ...string) error {
	repo, err := gogit.PlainOpen(g.WorkDir)
	if err != nil {
		return fmt.Errorf("opening repo: %w", err)
	}

	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("getting HEAD: %w", err)
	}

	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return fmt.Errorf("getting HEAD commit: %w", err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return fmt.Errorf("getting commit tree: %w", err)
	}

	for _, p := range paths {
		rel, relErr := filepath.Rel(g.WorkDir, p)
		if relErr != nil {
			rel = p
		}

		// Normalize to forward slashes for go-git tree lookups.
		rel = filepath.ToSlash(rel)

		g.Logger.Debug("git restore file", "path", rel)

		file, fileErr := tree.File(rel)
		if fileErr != nil {
			return fmt.Errorf("git restore: file %s not found in HEAD: %w", rel, fileErr)
		}

		contents, readErr := file.Contents()
		if readErr != nil {
			return fmt.Errorf("git restore: reading %s from HEAD: %w", rel, readErr)
		}

		absPath := filepath.Join(g.WorkDir, filepath.FromSlash(rel))
		if writeErr := os.WriteFile(absPath, []byte(contents), 0644); writeErr != nil {
			return fmt.Errorf("git restore: writing %s: %w", rel, writeErr)
		}
	}

	return nil
}

// Init initializes a new git repository in the working directory.
func (g *GoGitClient) Init(ctx context.Context) error {
	g.Logger.Debug("git init", "dir", g.WorkDir)

	_, err := gogit.PlainInit(g.WorkDir, false)
	if err != nil {
		// If already initialized, that's fine (idempotent).
		if err == gogit.ErrRepositoryAlreadyExists {
			return nil
		}
		return fmt.Errorf("git init failed: %w", err)
	}

	return nil
}

// RemoteAdd adds a named remote with the given URL.
func (g *GoGitClient) RemoteAdd(ctx context.Context, name, url string) error {
	repo, err := gogit.PlainOpen(g.WorkDir)
	if err != nil {
		return fmt.Errorf("opening repo: %w", err)
	}

	g.Logger.Debug("git remote add", "name", name, "url", url)

	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{url},
	})
	if err != nil {
		return fmt.Errorf("git remote add %s failed: %w", name, err)
	}

	return nil
}

// Log returns commit history filtered by path, newest first.
func (g *GoGitClient) Log(ctx context.Context, path string, limit int) ([]CommitInfo, error) {
	repo, err := gogit.PlainOpen(g.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("opening repo: %w", err)
	}

	logOpts := &gogit.LogOptions{
		Order: gogit.LogOrderCommitterTime,
	}
	if path != "" {
		// Normalize to forward slashes for go-git.
		normalized := filepath.ToSlash(path)
		// Make relative if absolute.
		if rel, relErr := filepath.Rel(g.WorkDir, normalized); relErr == nil {
			normalized = filepath.ToSlash(rel)
		}
		filterPath := normalized
		logOpts.PathFilter = func(p string) bool {
			return strings.HasPrefix(p, filterPath)
		}
	}

	iter, err := repo.Log(logOpts)
	if err != nil {
		// Empty repo (no commits / HEAD not found) — return empty slice, not error.
		if err.Error() == "reference not found" {
			return nil, nil
		}
		return nil, fmt.Errorf("git log: %w", err)
	}

	var commits []CommitInfo
	err = iter.ForEach(func(c *object.Commit) error {
		if limit > 0 && len(commits) >= limit {
			return fmt.Errorf("stop") // break iteration
		}

		sha := c.Hash.String()
		short := sha
		if len(sha) >= 7 {
			short = sha[:7]
		}

		commits = append(commits, CommitInfo{
			SHA:      sha,
			ShortSHA: short,
			Date:     c.Author.When.UTC(),
			Message:  strings.SplitN(c.Message, "\n", 2)[0],
		})
		return nil
	})

	// The "stop" error is our way to break early — not a real error.
	if err != nil && err.Error() != "stop" {
		return nil, fmt.Errorf("iterating log: %w", err)
	}

	return commits, nil
}

// CheckoutFiles restores files from a specific commit SHA.
// Reads file contents from the commit's tree and writes them to the working directory.
func (g *GoGitClient) CheckoutFiles(ctx context.Context, sha string, paths ...string) error {
	repo, err := gogit.PlainOpen(g.WorkDir)
	if err != nil {
		return fmt.Errorf("opening repo: %w", err)
	}

	hash := plumbing.NewHash(sha)
	commit, err := repo.CommitObject(hash)
	if err != nil {
		return fmt.Errorf("getting commit %s: %w", sha, err)
	}

	tree, err := commit.Tree()
	if err != nil {
		return fmt.Errorf("getting tree for %s: %w", sha, err)
	}

	for _, p := range paths {
		rel, relErr := filepath.Rel(g.WorkDir, p)
		if relErr != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)

		g.Logger.Debug("checkout file from commit", "sha", sha[:7], "path", rel)

		file, fileErr := tree.File(rel)
		if fileErr != nil {
			return fmt.Errorf("checkout: file %s not found in commit %s: %w", rel, sha[:7], fileErr)
		}

		contents, readErr := file.Contents()
		if readErr != nil {
			return fmt.Errorf("checkout: reading %s from commit %s: %w", rel, sha[:7], readErr)
		}

		absPath := filepath.Join(g.WorkDir, filepath.FromSlash(rel))

		// Ensure parent directory exists.
		if mkErr := os.MkdirAll(filepath.Dir(absPath), 0755); mkErr != nil {
			return fmt.Errorf("checkout: creating parent dir for %s: %w", rel, mkErr)
		}

		if writeErr := os.WriteFile(absPath, []byte(contents), 0644); writeErr != nil {
			return fmt.Errorf("checkout: writing %s: %w", rel, writeErr)
		}
	}

	return nil
}

// sshAuth builds the SSH public key auth method from the configured key.
func (g *GoGitClient) sshAuth() (transport.AuthMethod, error) {
	if g.SSHKeyPath == "" {
		return nil, fmt.Errorf("ssh_key_path is not configured")
	}

	keyData, err := os.ReadFile(g.SSHKeyPath)
	if err != nil {
		return nil, fmt.Errorf("reading SSH key %s: %w", g.SSHKeyPath, err)
	}

	// Resolve passphrase from env var.
	passphrase := ""
	if g.SSHKeyPassEnv != "" {
		passphrase = os.Getenv(g.SSHKeyPassEnv)
	}

	var signer ssh.Signer
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(keyData)
	}
	if err != nil {
		return nil, fmt.Errorf("parsing SSH key: %w", err)
	}

	auth := &gitssh.PublicKeys{
		User:   "git",
		Signer: signer,
	}

	// Set up known_hosts verification.
	if g.KnownHostsPath != "" {
		hostKeyCallback, khErr := knownhosts.New(g.KnownHostsPath)
		if khErr != nil {
			return nil, fmt.Errorf("loading known_hosts %s: %w", g.KnownHostsPath, khErr)
		}
		auth.HostKeyCallback = ssh.HostKeyCallback(hostKeyCallback)
	} else {
		// If no known_hosts configured, use InsecureIgnoreHostKey.
		// This is common in CI/Docker environments.
		auth.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	return auth, nil
}

// isSSHURL returns true if the URL looks like an SSH remote.
func isSSHURL(url string) bool {
	return strings.HasPrefix(url, "git@") ||
		strings.HasPrefix(url, "ssh://") ||
		strings.Contains(url, "@") && strings.Contains(url, ":")
}
