# Oxy Backup

<!-- TODO: Add badges (Go version, CI status, license) -->

**Automated PostgreSQL backups with Git-based version control.**

## Overview

Oxy Backup (`oxy`) is a CLI tool that automates PostgreSQL database backups using `pg_dump`, partitions the output into manageable chunks for efficient Git storage, and commits the results to a Git repository. It supports point-in-time restore by reassembling partitions and loading them back via `psql`.

The tool works with PostgreSQL running in Docker containers or directly on the host network, and is designed for both local use and CI/CD pipelines.

## Features

- **Dump and partition** -- runs `pg_dump` and splits the output into configurable chunks (KB, MB, GB)
- **Git-backed versioning** -- each backup is committed with a timestamped message and optionally pushed to a remote
- **SSH key authentication** -- native SSH transport via `go-git/v5` for private repos. Configure an SSH key and oxy handles auth without shelling out to git
- **Point-in-time restore** -- reassembles partitions from any backup timestamp and loads via `psql --single-transaction`
- **Multi-database support** -- configure and back up multiple databases sequentially, with per-database overrides
- **Defaults merging** -- define shared settings once under `defaults:`, override at the database level
- **Docker and host modes** -- connect to PostgreSQL via `docker exec` or direct host/port
- **Environment variable interpolation** -- use `${VAR_NAME}` syntax in config to avoid hardcoding credentials
- **Interactive setup wizard** -- `oxy init --interactive` guides you through first-time configuration, including SSH key detection for private repos
- **Dry-run mode** -- validate config and preview actions without executing anything
- **Structured exit codes** -- distinct codes for config, dump, partition, git, restore, and partial failure errors (CI-friendly)
- **JSON and text logging** -- configurable via flags or config file
- **Hexagonal architecture** -- GitClient port with two adapters (ExecGitClient, GoGitClient), clean separation of concerns across packages

## Quick Start

```bash
# Initialize a new backup repository
oxy init --db-mode=docker --db-container=my-postgres --db-name=myapp

# Or with a private SSH remote
oxy init --db-mode=docker --db-container=my-postgres --db-name=myapp \
  --git-remote git@github.com:you/backups.git \
  --ssh-key ~/.ssh/id_ed25519

# Set the database password
export PGPASSWORD="your-password"

# Run a backup
oxy backup

# List available restore points
oxy restore myapp

# Restore from a specific timestamp
oxy restore myapp --timestamp 20240115-143022
```

## Installation

### From source

Requires Go 1.25+.

```bash
git clone https://github.com/alenicomar/oxy-backup.git
cd oxy-backup

# Build the binary in the current directory
make build

# Install to /usr/local/bin (may require sudo)
make install

# Or install to a custom prefix
make install PREFIX=$HOME/.local
```

### Docker

```bash
docker build -t oxy-backup .
docker run --rm -v $(pwd):/data oxy-backup backup --config oxy.yaml
```

The Docker image is a multi-stage build based on `alpine:3.21` and includes `postgresql-client`, `git`, `openssh-client`, and the `oxy` binary. It runs as a non-root user.

## Configuration

Oxy Backup is configured via an `oxy.yaml` file. Run `oxy init` to generate one, or create it manually based on the example below.

### Example `oxy.yaml`

```yaml
version: "1"

# Shared defaults -- merged into each database where fields are zero-valued.
defaults:
  mode: docker
  container: my-postgres
  port: 5432
  username: postgres
  password: "${PGPASSWORD}"
  partition_size: "1MB"
  output_dir: "./backups"
  pg_dump_args:
    - "--no-owner"
    - "--no-acl"

# Git settings
git:
  auto_push: true
  remote: origin
  branch: main
  commit_message_template: "backup: {{.DbName}} @ {{.Timestamp}}"

  # SSH authentication for private repos (optional).
  # When ssh_key_path is set, oxy uses go-git with native SSH transport
  # instead of shelling out to the system git binary.
  # ssh_key_path: "~/.ssh/id_ed25519"
  # ssh_key_pass_env: "SSH_KEY_PASSPHRASE"    # env var holding key passphrase
  # ssh_known_hosts_path: "~/.ssh/known_hosts"

# Logging
logging:
  level: info       # debug, info, warn, error
  format: text      # text, json

# Databases to back up
databases:
  - name: app_production
    mode: docker
    container: postgres-prod
    database: app_db
    username: app_user
    password: "${APP_DB_PASSWORD}"
    partition_size: "5MB"
    pg_dump_args:
      - "--no-owner"
      - "--no-acl"
      - "--exclude-table=sessions"

  - name: analytics
    mode: host
    host: db.example.com
    port: 5432
    database: analytics_db
    username: analytics
    password: "${ANALYTICS_DB_PASSWORD}"
    partition_size: "10MB"

  # Minimal entry -- inherits everything from defaults
  - name: staging
    database: staging_db
```

### Key sections

| Section | Purpose |
|---|---|
| `defaults` | Shared database settings. Per-database values override these. |
| `git` | Remote, branch, auto-push behavior, commit message template, and SSH key settings. `auto_push` defaults to `true`. |
| `git.ssh_*` | SSH key authentication. When `ssh_key_path` is set, oxy uses `go-git` with native SSH transport instead of shelling out to the system `git` binary. |
| `logging` | Log level (`debug`, `info`, `warn`, `error`) and format (`text`, `json`). |
| `databases` | List of databases to back up. Each requires `name` and `database` at minimum. |

### Environment variable expansion

Use `${VAR_NAME}` anywhere in the YAML to reference environment variables. This is the recommended approach for credentials:

```yaml
password: "${PGPASSWORD}"
```

### Partition size

The `partition_size` field accepts human-readable values: `100KB`, `1MB`, `5MB`, `1GB`, or plain byte counts. Defaults to `100KB` if omitted.

## Usage

### Global flags

```
--config string    path to config file (default "oxy.yaml")
--log-format string   log format: text or json (default "text")
--log-level string    log level: debug, info, warn, error (default "info")
--dry-run             validate config and show what would be done
```

### `oxy init`

Initialize a new backup repository. Creates `oxy.yaml`, `.gitignore`, and runs `git init`.

```bash
# Interactive setup wizard
oxy init --interactive

# Non-interactive with flags
oxy init --db-mode=docker --db-container=my-postgres --db-name=myapp

# Host mode
oxy init --db-mode=host --db-host=db.example.com --db-port=5432 \
  --db-username=postgres --db-name=myapp

# With a private SSH remote
oxy init --db-mode=docker --db-container=pg --db-name=app \
  --git-remote git@github.com:you/backups.git \
  --ssh-key ~/.ssh/id_ed25519

# SSH remote with encrypted key
oxy init --db-mode=docker --db-container=pg --db-name=app \
  --git-remote git@github.com:you/backups.git \
  --ssh-key ~/.ssh/id_ed25519 \
  --ssh-key-pass-env SSH_KEY_PASSPHRASE

# Initialize in a specific directory
oxy init ./my-backups --db-mode=docker --db-container=pg --db-name=app

# Overwrite existing config
oxy init --force --db-mode=docker --db-container=pg --db-name=app
```

In interactive mode, the wizard automatically detects SSH remote URLs and prompts for the SSH key path, passphrase environment variable, and known_hosts file.

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `-i`, `--interactive` | `false` | Run the guided setup wizard |
| `--git-remote` | | Git remote URL to add as `origin` |
| `--db-mode` | `docker` | Connection mode: `docker` or `host` |
| `--db-container` | | Docker container name (required for docker mode) |
| `--db-host` | `localhost` | PostgreSQL host (host mode) |
| `--db-port` | `5432` | PostgreSQL port (host mode) |
| `--db-username` | `postgres` | PostgreSQL username |
| `--db-name` | | Logical database name (required) |
| `--db-database` | same as `--db-name` | PostgreSQL database name |
| `--password-env` | `PGPASSWORD` | Environment variable for the database password |
| `--partition-size` | `100KB` | Backup partition size |
| `--output-dir` | `./backups` | Output directory for backup files |
| `--ssh-key` | | Path to SSH private key (enables go-git SSH transport) |
| `--ssh-key-pass-env` | | Env var holding SSH key passphrase (if key is encrypted) |
| `--ssh-known-hosts` | `~/.ssh/known_hosts` | Path to known_hosts file |
| `-f`, `--force` | `false` | Overwrite existing `oxy.yaml` |

### `oxy backup`

Dump, partition, and commit one or all configured databases.

```bash
# Back up all databases
oxy backup

# Back up a specific database
oxy backup app_production

# Validate config without executing
oxy backup --dry-run

# With JSON logging for CI
oxy backup --log-format=json --log-level=info
```

When no database name is given, all databases in the config are backed up sequentially. If some databases succeed and others fail, the process exits with code `7` (partial failure).

### `oxy restore`

Reassemble partitioned backups and load into PostgreSQL.

```bash
# List available backup timestamps
oxy restore myapp

# Restore a specific timestamp
oxy restore myapp --timestamp 20240115-143022

# Preview what would happen
oxy restore myapp --timestamp 20240115-143022 --dry-run
```

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--timestamp` | | Backup timestamp to restore (e.g. `20240115-143022`). If omitted, lists available timestamps. |

The restore process validates the manifest, reassembles partitions into a single SQL file, loads it via `psql --single-transaction`, then cleans up the temporary file.

## Architecture

The project follows a hexagonal (ports & adapters) architecture. Core logic depends on interfaces (ports), with concrete implementations (adapters) injected at runtime.

```
cmd/oxy/              CLI entry point (cobra commands)
internal/
  backup/             Dump orchestration, partitioning, and manifest generation
  restore/            Partition reassembly and database loading
  config/             YAML loading, env var interpolation, validation, defaults merging
  git/                GitClient port + adapters:
                        - ExecGitClient (default, shells out to system git)
                        - GoGitClient (native SSH transport via go-git/v5)
                        - Factory selects adapter based on config
  postgres/           Port (interface) and adapter for pg_dump/psql execution
  initialize/         Init subcommand: interactive prompts, SSH detection, config generation
  logging/            slog handler configuration (text/json)
  exitcode/           Process exit codes for CI integration
```

### Git client selection

When `git.ssh_key_path` is configured in `oxy.yaml`, the factory creates a `GoGitClient` that uses `go-git/v5` with native SSH transport. This avoids requiring the system `git` binary to have SSH key access configured.

When no SSH key is set, the factory creates an `ExecGitClient` that shells out to the system `git` binary for all operations.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | Configuration error |
| `2` | Connection error |
| `3` | pg_dump error |
| `4` | Partition error |
| `5` | Git error |
| `6` | Restore error |
| `7` | Partial failure (some databases succeeded, others failed) |

## Development

### Prerequisites

- Go 1.25+
- PostgreSQL client tools (`pg_dump`, `psql`)
- Git

### Make targets

```bash
make build        # Compile the binary
make test         # Run all unit tests
make test-cover   # Run tests with HTML coverage report
make lint         # Run go vet
make clean        # Remove build artifacts
make install      # Install to /usr/local/bin
make uninstall    # Remove from /usr/local/bin
make help         # Show available targets
```

### Dependencies

The project uses a focused set of dependencies:

- [`github.com/spf13/cobra`](https://github.com/spf13/cobra) -- CLI framework
- [`gopkg.in/yaml.v3`](https://github.com/go-yaml/yaml) -- YAML parsing
- [`github.com/go-git/go-git/v5`](https://github.com/go-git/go-git) -- Pure Go git implementation (SSH transport)
- [`golang.org/x/crypto`](https://pkg.go.dev/golang.org/x/crypto) -- SSH key parsing
- [`github.com/skeema/knownhosts`](https://github.com/skeema/knownhosts) -- SSH known_hosts verification

## Docker

### Build

```bash
docker build -t oxy-backup .
```

The Dockerfile uses a multi-stage build: Go compilation in `golang:1.25-alpine`, then a minimal `alpine:3.21` runtime with `postgresql-client`, `git`, `openssh-client`, and `ca-certificates`. The final image runs as a non-root `oxy` user.

### Run

```bash
# Mount your backup repo and config
docker run --rm \
  -v $(pwd):/data \
  -e PGPASSWORD="your-password" \
  oxy-backup backup

# With a custom config path
docker run --rm \
  -v $(pwd):/data \
  -e PGPASSWORD="your-password" \
  oxy-backup backup --config /data/oxy.yaml

# With SSH key for private repos
docker run --rm \
  -v $(pwd):/data \
  -v ~/.ssh/id_ed25519:/home/oxy/.ssh/id_ed25519:ro \
  -v ~/.ssh/known_hosts:/home/oxy/.ssh/known_hosts:ro \
  -e PGPASSWORD="your-password" \
  oxy-backup backup
```

The container's working directory is `/data` and the entrypoint is `oxy`.

## CI/CD

An example GitHub Actions workflow is provided at `.github/workflows/backup.yaml.example`. It demonstrates:

- Scheduled daily backups via cron (`0 2 * * *`)
- Manual trigger via `workflow_dispatch`
- Building `oxy` from source
- Configuring Git for automated commits
- Injecting credentials via repository secrets
- Pushing backup commits to the remote

Copy the example and adapt it to your setup:

```bash
cp .github/workflows/backup.yaml.example .github/workflows/backup.yaml
```

## License

<!-- TODO: Add license -->
