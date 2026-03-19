package initialize

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/alenicomar/oxy-backup/internal/git"
)

// Service orchestrates the init process.
type Service struct {
	Logger *slog.Logger
	Out    io.Writer
}

// Run executes the full init flow.
func (s *Service) Run(ctx context.Context, targetDir string, opts *InitOptions, force bool) error {
	// 1. Resolve target path
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	// 2. Create directory if needed
	if err := os.MkdirAll(absDir, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %w", absDir, err)
	}

	// 3. Check if already initialized
	configPath := filepath.Join(absDir, configFileName)
	if _, err := os.Stat(configPath); err == nil && !force {
		return fmt.Errorf("already initialized: %s exists (use --force to overwrite)", configPath)
	}

	// 4. Validate prerequisites
	prereqs, err := ValidatePrerequisites(opts.Mode)
	if err != nil {
		return err
	}
	for _, p := range prereqs {
		if p.Found {
			s.Logger.Debug("prerequisite found", "tool", p.Name, "path", p.Path)
		} else if !p.Required {
			fmt.Fprintf(s.Out, "  ⚠ %s not found in PATH (optional, needed at runtime)\n", p.Name)
		}
	}

	// 5. Git init
	gitClient := &git.ExecGitClient{
		WorkDir: absDir,
		Remote:  "origin",
		Branch:  "main",
		Logger:  s.Logger,
	}

	if err := gitClient.Init(ctx); err != nil {
		return fmt.Errorf("git init: %w", err)
	}
	s.Logger.Debug("git repository initialized", "path", absDir)

	// 6. Git remote add (conditional)
	if opts.GitRemote != "" {
		if err := gitClient.RemoteAdd(ctx, "origin", opts.GitRemote); err != nil {
			// Non-fatal — remote may already exist
			fmt.Fprintf(s.Out, "  ⚠ Could not add remote 'origin': %v\n", err)
			s.Logger.Warn("git remote add failed", "error", err)
		} else {
			s.Logger.Debug("git remote added", "name", "origin", "url", opts.GitRemote)
		}
	}

	// 7. Generate oxy.yaml
	cfg := BuildConfig(opts)
	if err := WriteConfig(absDir, cfg); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	// 8. Generate .gitignore
	if err := WriteGitignore(absDir); err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}

	// 9. Print summary
	s.printSummary(absDir, opts)

	return nil
}

func (s *Service) printSummary(dir string, opts *InitOptions) {
	fmt.Fprintln(s.Out, "")
	fmt.Fprintf(s.Out, "Initialized oxy backup repository in %s\n", dir)
	fmt.Fprintln(s.Out, "")
	fmt.Fprintln(s.Out, "Created:")
	fmt.Fprintln(s.Out, "  oxy.yaml      — backup configuration")
	fmt.Fprintln(s.Out, "  .gitignore    — git ignore rules")
	fmt.Fprintln(s.Out, "")
	fmt.Fprintln(s.Out, "Git:")
	fmt.Fprintln(s.Out, "  Initialized repository")
	if opts.GitRemote != "" {
		fmt.Fprintf(s.Out, "  Remote 'origin' → %s\n", opts.GitRemote)
	}
	fmt.Fprintln(s.Out, "")
	fmt.Fprintln(s.Out, "Next steps:")
	fmt.Fprintf(s.Out, "  1. Set the %s environment variable\n", opts.PasswordEnv)
	fmt.Fprintln(s.Out, "  2. Review oxy.yaml and adjust as needed")
	fmt.Fprintln(s.Out, "  3. Run 'oxy backup' to create your first backup")
	fmt.Fprintln(s.Out, "")
}
