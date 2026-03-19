package main

import (
	"fmt"
	"os"

	"github.com/alenicomar/oxy-backup/internal/config"
	"github.com/alenicomar/oxy-backup/internal/initialize"
	"github.com/alenicomar/oxy-backup/internal/logging"
	"github.com/spf13/cobra"
)

var (
	interactive   bool
	gitRemote     string
	dbMode        string
	dbContainer   string
	dbHost        string
	dbPort        int
	dbUsername    string
	dbName        string
	dbDatabase    string
	passwordEnv   string
	partitionSize string
	outputDir     string
	force         bool

	// SSH flags
	sshKeyPath        string
	sshKeyPassEnv     string
	sshKnownHostsPath string
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize a new backup repository",
	Long: `Initializes a new directory as an oxy backup repository.
Creates oxy.yaml configuration, .gitignore, and runs git init.

If no path is given, the current directory is used (like git init).
Use --interactive for a guided setup wizard.`,
	Args: cobra.MaximumNArgs(1),
	// Override parent's PersistentPreRunE — only setup logging, skip config loading
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		logger, err = logging.Setup(logLevel, logFormat)
		if err != nil {
			return fmt.Errorf("logging setup: %w", err)
		}
		return nil
	},
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVarP(&interactive, "interactive", "i", false,
		"run interactive setup wizard")
	initCmd.Flags().StringVar(&gitRemote, "git-remote", "",
		"git remote URL to add as 'origin'")
	initCmd.Flags().StringVar(&dbMode, "db-mode", "docker",
		"database connection mode: docker or host")
	initCmd.Flags().StringVar(&dbContainer, "db-container", "",
		"docker container name (required when --db-mode=docker)")
	initCmd.Flags().StringVar(&dbHost, "db-host", "localhost",
		"PostgreSQL host (used when --db-mode=host)")
	initCmd.Flags().IntVar(&dbPort, "db-port", 5432,
		"PostgreSQL port (used when --db-mode=host)")
	initCmd.Flags().StringVar(&dbUsername, "db-username", "postgres",
		"PostgreSQL username")
	initCmd.Flags().StringVar(&dbName, "db-name", "",
		"logical database name (required in non-interactive mode)")
	initCmd.Flags().StringVar(&dbDatabase, "db-database", "",
		"PostgreSQL database name (defaults to --db-name)")
	initCmd.Flags().StringVar(&passwordEnv, "password-env", "PGPASSWORD",
		"environment variable name for the database password")
	initCmd.Flags().StringVar(&partitionSize, "partition-size", "100KB",
		"backup partition size (e.g. 100KB, 1MB, 5MB)")
	initCmd.Flags().StringVar(&outputDir, "output-dir", "./backups",
		"output directory for backup files within the repo")
	initCmd.Flags().BoolVarP(&force, "force", "f", false,
		"overwrite existing oxy.yaml if present")
	initCmd.Flags().StringVar(&sshKeyPath, "ssh-key", "",
		"path to SSH private key (enables go-git SSH transport)")
	initCmd.Flags().StringVar(&sshKeyPassEnv, "ssh-key-pass-env", "",
		"env var holding SSH key passphrase (if key is encrypted)")
	initCmd.Flags().StringVar(&sshKnownHostsPath, "ssh-known-hosts", "",
		"path to known_hosts file (default: ~/.ssh/known_hosts)")

	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	// Resolve target directory
	targetDir := "."
	if len(args) > 0 {
		targetDir = args[0]
	}

	var opts *initialize.InitOptions

	if interactive {
		// Interactive mode — prompts override all flags
		prompter := initialize.NewPrompter(os.Stdin, os.Stdout)
		var err error
		opts, err = prompter.RunInteractive()
		if err != nil {
			return fmt.Errorf("interactive setup: %w", err)
		}
	} else {
		// Flag mode — validate required fields
		var err error
		opts, err = buildInitOptionsFromFlags()
		if err != nil {
			return err
		}
	}

	svc := &initialize.Service{
		Logger: logger,
		Out:    os.Stdout,
	}

	return svc.Run(cmd.Context(), targetDir, opts, force)
}

func buildInitOptionsFromFlags() (*initialize.InitOptions, error) {
	if dbName == "" {
		return nil, fmt.Errorf("--db-name is required (or use --interactive)")
	}

	switch dbMode {
	case "docker":
		if dbContainer == "" {
			return nil, fmt.Errorf("--db-container is required when --db-mode=docker")
		}
	case "host":
		// dbHost defaults to "localhost", dbPort to 5432 — valid
	default:
		return nil, fmt.Errorf("--db-mode must be 'docker' or 'host', got %q", dbMode)
	}

	if dbDatabase == "" {
		dbDatabase = dbName
	}

	if _, err := config.ParseSize(partitionSize); err != nil {
		return nil, fmt.Errorf("invalid --partition-size: %w", err)
	}

	// Warn if SSH remote is used without --ssh-key
	if initialize.IsSSHURL(gitRemote) && sshKeyPath == "" {
		fmt.Fprintf(os.Stderr,
			"  ⚠ SSH remote detected but --ssh-key not set. Push/pull will use system git (no go-git SSH transport).\n"+
				"    To enable SSH key auth, add: --ssh-key ~/.ssh/id_ed25519\n")
	}

	return &initialize.InitOptions{
		Mode:              dbMode,
		Container:         dbContainer,
		Host:              dbHost,
		Port:              dbPort,
		Username:          dbUsername,
		DbName:            dbName,
		DbDatabase:        dbDatabase,
		PasswordEnv:       passwordEnv,
		PartitionSize:     partitionSize,
		OutputDir:         outputDir,
		GitRemote:         gitRemote,
		SSHKeyPath:        sshKeyPath,
		SSHKeyPassEnv:     sshKeyPassEnv,
		SSHKnownHostsPath: sshKnownHostsPath,
	}, nil
}
