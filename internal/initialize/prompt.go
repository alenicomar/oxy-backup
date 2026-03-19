package initialize

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/alenicomar/oxy-backup/internal/config"
)

// Prompter handles interactive user input for the init wizard.
type Prompter struct {
	scanner *bufio.Scanner
	out     io.Writer
}

// NewPrompter creates a Prompter reading from r and writing to w.
func NewPrompter(r io.Reader, w io.Writer) *Prompter {
	return &Prompter{
		scanner: bufio.NewScanner(r),
		out:     w,
	}
}

// errEOF is returned when the input reader is exhausted.
var errEOF = fmt.Errorf("unexpected end of input")

// ask prints a prompt with optional default and reads a line.
func (p *Prompter) ask(label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Fprintf(p.out, "%s [%s]: ", label, defaultVal)
	} else {
		fmt.Fprintf(p.out, "%s: ", label)
	}
	if !p.scanner.Scan() {
		return defaultVal
	}
	val := strings.TrimSpace(p.scanner.Text())
	if val == "" {
		return defaultVal
	}
	return val
}

// askRequired prints a prompt and keeps asking until non-empty input.
// Returns an error if input is exhausted before a value is provided.
func (p *Prompter) askRequired(label string) (string, error) {
	for {
		fmt.Fprintf(p.out, "%s: ", label)
		if !p.scanner.Scan() {
			return "", errEOF
		}
		val := strings.TrimSpace(p.scanner.Text())
		if val != "" {
			return val, nil
		}
		fmt.Fprintln(p.out, "  This field is required.")
	}
}

// selectOne presents a numbered choice and returns the selected option string.
// Re-prompts on invalid input; empty input accepts the default.
// Returns an error if input is exhausted.
func (p *Prompter) selectOne(label string, options []string, defaultIdx int) (string, error) {
	for {
		fmt.Fprintln(p.out, label)
		for i, opt := range options {
			fmt.Fprintf(p.out, "  [%d] %s\n", i+1, opt)
		}
		fmt.Fprintf(p.out, "Choice [%d]: ", defaultIdx+1)
		if !p.scanner.Scan() {
			return options[defaultIdx], errEOF
		}
		val := strings.TrimSpace(p.scanner.Text())
		if val == "" {
			return options[defaultIdx], nil
		}
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 || n > len(options) {
			fmt.Fprintf(p.out, "  Invalid choice. Please enter 1-%d.\n", len(options))
			continue
		}
		return options[n-1], nil
	}
}

// IsSSHURL returns true if the URL looks like an SSH git remote.
// Matches patterns like "git@host:path" or "ssh://host/path".
func IsSSHURL(url string) bool {
	if strings.HasPrefix(url, "ssh://") {
		return true
	}
	// SCP-like syntax: git@github.com:user/repo.git
	// Must have @ before the first : and no // (to exclude https://)
	if strings.Contains(url, "@") && strings.Contains(url, ":") && !strings.Contains(url, "://") {
		return true
	}
	return false
}

// RunInteractive runs the full interactive init wizard and returns InitOptions.
func (p *Prompter) RunInteractive() (*InitOptions, error) {
	fmt.Fprintln(p.out, "")
	fmt.Fprintln(p.out, "Oxy Backup — Interactive Setup")
	fmt.Fprintln(p.out, "──────────────────────────────")
	fmt.Fprintln(p.out, "")

	opts := &InitOptions{}

	// 1. Git remote
	opts.GitRemote = p.ask("Git remote URL (empty to skip)", "")

	// 1b. SSH configuration — prompted when remote is an SSH URL
	if IsSSHURL(opts.GitRemote) {
		fmt.Fprintln(p.out, "")
		fmt.Fprintln(p.out, "SSH remote detected — configuring key authentication:")
		opts.SSHKeyPath = p.ask("SSH private key path", "~/.ssh/id_ed25519")
		hasPass := p.ask("Is the key passphrase-protected? (y/n)", "n")
		if strings.EqualFold(hasPass, "y") || strings.EqualFold(hasPass, "yes") {
			opts.SSHKeyPassEnv = p.ask("Env var holding passphrase", "SSH_KEY_PASS")
		}
		opts.SSHKnownHostsPath = p.ask("known_hosts path (empty for default)", "")
	}

	fmt.Fprintln(p.out, "")

	// 2. Database mode
	var err error
	opts.Mode, err = p.selectOne("Database mode:", []string{"docker", "host"}, 0)
	if err != nil {
		return nil, err
	}

	fmt.Fprintln(p.out, "")

	// 3. Mode-specific fields
	switch opts.Mode {
	case "docker":
		opts.Container, err = p.askRequired("Docker container name")
		if err != nil {
			return nil, err
		}
	case "host":
		opts.Host = p.ask("PostgreSQL host", "localhost")
		portStr := p.ask("PostgreSQL port", "5432")
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			port = 5432
		}
		opts.Port = port
		opts.Username = p.ask("PostgreSQL username", "postgres")
	}

	fmt.Fprintln(p.out, "")

	// 4. Database info
	opts.DbName, err = p.askRequired("Database name (logical label)")
	if err != nil {
		return nil, err
	}
	opts.DbDatabase = p.ask("PostgreSQL database name", opts.DbName)
	opts.PasswordEnv = p.ask("Password env var name", "PGPASSWORD")

	fmt.Fprintln(p.out, "")

	// 5. Backup settings
	opts.PartitionSize = p.ask("Partition size", "100KB")
	// Validate partition size
	if _, err := config.ParseSize(opts.PartitionSize); err != nil {
		opts.PartitionSize = "100KB"
		fmt.Fprintf(p.out, "  Invalid size, using default: 100KB\n")
	}

	opts.OutputDir = p.ask("Output directory", "./backups")

	fmt.Fprintln(p.out, "")

	return opts, nil
}
