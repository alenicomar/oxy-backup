package git

import (
	"log/slog"

	"github.com/alenicomar/oxy-backup/internal/config"
)

// NewClient creates the appropriate GitClient based on configuration.
// If SSH key is configured, returns GoGitClient with native SSH transport.
// Otherwise, returns ExecGitClient using the system git binary.
func NewClient(gitCfg config.GitConfig, workDir string, logger *slog.Logger) GitClient {
	if gitCfg.SSHEnabled() {
		logger.Debug("using go-git client with SSH transport",
			"ssh_key_path", gitCfg.SSHKeyPath,
		)

		return &GoGitClient{
			WorkDir:        workDir,
			Remote:         gitCfg.Remote,
			Branch:         gitCfg.Branch,
			SSHKeyPath:     gitCfg.ResolvedSSHKeyPath(),
			SSHKeyPassEnv:  gitCfg.SSHKeyPassEnv,
			KnownHostsPath: gitCfg.ResolvedSSHKnownHostsPath(),
			Logger:         logger,
		}
	}

	logger.Debug("using exec git client (system git binary)")

	return &ExecGitClient{
		WorkDir: workDir,
		Remote:  gitCfg.Remote,
		Branch:  gitCfg.Branch,
		Logger:  logger,
	}
}
