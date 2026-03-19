package postgres

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/alenicomar/oxy-backup/internal/config"
)

// ExecPgExecutor implements PgExecutor using os/exec.
type ExecPgExecutor struct {
	Logger *slog.Logger
}

var _ PgExecutor = (*ExecPgExecutor)(nil)

// versionRegex extracts major version numbers from pg_dump/psql --version output.
var versionRegex = regexp.MustCompile(`(\d+)\.?(\d+)?`)

// CheckVersion detects pg_dump client version and logs a warning if it's older
// than the server version. Best-effort — never returns error.
func (e *ExecPgExecutor) CheckVersion(ctx context.Context, cfg config.DatabaseConfig) {
	// Get pg_dump version
	clientMajor := e.getPgDumpVersion(ctx, cfg)
	if clientMajor == 0 {
		return
	}

	// Get server version (via psql or pg_dump connection)
	serverMajor := e.getServerVersion(ctx, cfg)
	if serverMajor == 0 {
		return
	}

	e.Logger.Debug("version check",
		"pg_dump_version", clientMajor,
		"server_version", serverMajor,
	)

	if clientMajor < serverMajor {
		e.Logger.Warn("pg_dump version is older than server version — dump may be incomplete",
			"pg_dump_version", clientMajor,
			"server_version", serverMajor,
		)
	}
}

func (e *ExecPgExecutor) getPgDumpVersion(ctx context.Context, cfg config.DatabaseConfig) int {
	var cmd *exec.Cmd
	switch cfg.Mode {
	case "docker":
		cmd = exec.CommandContext(ctx, "docker", "exec", cfg.Container, "pg_dump", "--version")
	default:
		cmd = exec.CommandContext(ctx, "pg_dump", "--version")
	}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return 0
	}

	return parseMajorVersion(stdout.String())
}

func (e *ExecPgExecutor) getServerVersion(ctx context.Context, cfg config.DatabaseConfig) int {
	var args []string
	var env []string

	switch cfg.Mode {
	case "docker":
		args = []string{
			"docker", "exec",
			"-e", fmt.Sprintf("PGPASSWORD=%s", cfg.Password),
			cfg.Container,
			"psql", "-U", cfg.Username, "-d", cfg.Database,
			"-t", "-A", "-c", "SHOW server_version;",
		}
	case "host":
		args = []string{
			"psql",
			"-h", cfg.Host, "-p", strconv.Itoa(cfg.Port),
			"-U", cfg.Username, "-d", cfg.Database,
			"-t", "-A", "-c", "SHOW server_version;",
		}
		env = []string{fmt.Sprintf("PGPASSWORD=%s", cfg.Password)}
	default:
		return 0
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return 0
	}

	return parseMajorVersion(stdout.String())
}

// parseMajorVersion extracts the major version number from version strings like
// "pg_dump (PostgreSQL) 16.2" or "16.2 (Debian 16.2-1.pgdg120+2)".
func parseMajorVersion(s string) int {
	matches := versionRegex.FindStringSubmatch(strings.TrimSpace(s))
	if len(matches) < 2 {
		return 0
	}
	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}
	return major
}

// DumpDatabase executes pg_dump and returns the SQL output.
func (e *ExecPgExecutor) DumpDatabase(ctx context.Context, cfg config.DatabaseConfig) (io.Reader, error) {
	args, env, err := e.buildDumpCommand(cfg)
	if err != nil {
		return nil, err
	}

	e.Logger.Info("executing pg_dump",
		"mode", cfg.Mode,
		"database", cfg.Database,
		"container", cfg.Container,
	)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), env...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pg_dump failed: %w\nstderr: %s", err, stderr.String())
	}

	if stderr.Len() > 0 {
		e.Logger.Debug("pg_dump stderr", "output", stderr.String())
	}

	return &stdout, nil
}

// LoadDatabase executes psql with --single-transaction, piping SQL from file.
func (e *ExecPgExecutor) LoadDatabase(ctx context.Context, cfg config.DatabaseConfig, sqlFilePath string) error {
	args, env, err := e.buildLoadCommand(cfg)
	if err != nil {
		return err
	}

	e.Logger.Info("executing psql restore",
		"mode", cfg.Mode,
		"database", cfg.Database,
		"file", sqlFilePath,
	)

	sqlFile, err := os.Open(sqlFilePath)
	if err != nil {
		return fmt.Errorf("opening SQL file for restore: %w", err)
	}
	defer sqlFile.Close()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = sqlFile

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("psql restore failed: %w\nstderr: %s", err, stderr.String())
	}

	return nil
}

func (e *ExecPgExecutor) buildDumpCommand(cfg config.DatabaseConfig) (args []string, env []string, err error) {
	pgArgs := []string{
		"-U", cfg.Username,
		"-d", cfg.Database,
		"--format=plain",
	}
	pgArgs = append(pgArgs, cfg.PgDumpArgs...)

	switch cfg.Mode {
	case "docker":
		// docker exec -e PGPASSWORD=... container pg_dump ...
		args = []string{
			"docker", "exec",
			"-e", fmt.Sprintf("PGPASSWORD=%s", cfg.Password),
			cfg.Container,
			"pg_dump",
		}
		args = append(args, pgArgs...)
	case "host":
		args = []string{"pg_dump"}
		pgArgs = append([]string{
			"-h", cfg.Host,
			"-p", strconv.Itoa(cfg.Port),
		}, pgArgs...)
		args = append(args, pgArgs...)
		env = []string{fmt.Sprintf("PGPASSWORD=%s", cfg.Password)}
	default:
		return nil, nil, fmt.Errorf("unsupported mode: %s", cfg.Mode)
	}

	return args, env, nil
}

func (e *ExecPgExecutor) buildLoadCommand(cfg config.DatabaseConfig) (args []string, env []string, err error) {
	psqlArgs := []string{
		"-U", cfg.Username,
		"-d", cfg.Database,
		"--single-transaction",
	}

	switch cfg.Mode {
	case "docker":
		args = []string{
			"docker", "exec", "-i",
			"-e", fmt.Sprintf("PGPASSWORD=%s", cfg.Password),
			cfg.Container,
			"psql",
		}
		args = append(args, psqlArgs...)
	case "host":
		args = []string{"psql"}
		psqlArgs = append([]string{
			"-h", cfg.Host,
			"-p", strconv.Itoa(cfg.Port),
		}, psqlArgs...)
		args = append(args, psqlArgs...)
		env = []string{fmt.Sprintf("PGPASSWORD=%s", cfg.Password)}
	default:
		return nil, nil, fmt.Errorf("unsupported mode: %s", cfg.Mode)
	}

	return args, env, nil
}
