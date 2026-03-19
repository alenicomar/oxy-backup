// Package config handles YAML configuration loading, env var interpolation,
// validation, and defaults merging for oxy-backup.
package config

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// Config is the top-level configuration structure.
type Config struct {
	Version   string           `yaml:"version"`
	Defaults  DatabaseConfig   `yaml:"defaults"`
	Git       GitConfig        `yaml:"git"`
	Databases []DatabaseConfig `yaml:"databases"`
	Logging   LoggingConfig    `yaml:"logging"`
}

// DatabaseConfig holds per-database backup/restore settings.
type DatabaseConfig struct {
	Name          string   `yaml:"name"`
	Mode          string   `yaml:"mode"`                // "docker" or "host"
	Container     string   `yaml:"container,omitempty"` // required if mode=docker
	Host          string   `yaml:"host,omitempty"`      // required if mode=host
	Port          int      `yaml:"port,omitempty"`
	Username      string   `yaml:"username,omitempty"`
	Password      string   `yaml:"password,omitempty"`
	Database      string   `yaml:"database"`
	PgDumpArgs    []string `yaml:"pg_dump_args,omitempty"`
	PartitionSize string   `yaml:"partition_size,omitempty"` // e.g. "100KB", "1MB"
	OutputDir     string   `yaml:"output_dir,omitempty"`
}

// GitConfig holds Git operation settings.
type GitConfig struct {
	AutoPush              *bool  `yaml:"auto_push,omitempty"`
	CommitMessageTemplate string `yaml:"commit_message_template,omitempty"`
	Remote                string `yaml:"remote,omitempty"`
	Branch                string `yaml:"branch,omitempty"`
}

// AutoPushEnabled returns true if auto_push is nil (default) or explicitly true.
func (g *GitConfig) AutoPushEnabled() bool {
	if g.AutoPush == nil {
		return true // spec default: auto_push=true
	}
	return *g.AutoPush
}

// LoggingConfig holds logging preferences.
type LoggingConfig struct {
	Level  string `yaml:"level,omitempty"`
	Format string `yaml:"format,omitempty"`
}

// Validate checks required fields and mode-specific constraints.
func (c *Config) Validate(logger *slog.Logger) error {
	if c.Version == "" {
		return fmt.Errorf("config validation: 'version' is required")
	}

	if len(c.Databases) == 0 {
		return fmt.Errorf("config validation: at least one database must be configured")
	}

	for i, db := range c.Databases {
		prefix := fmt.Sprintf("databases[%d]", i)

		if db.Name == "" {
			return fmt.Errorf("config validation: %s.name is required", prefix)
		}
		if db.Database == "" {
			return fmt.Errorf("config validation: %s.database is required", prefix)
		}

		switch db.Mode {
		case "docker":
			if db.Container == "" {
				return fmt.Errorf("config validation: %s.container is required when mode is 'docker'", prefix)
			}
		case "host":
			if db.Host == "" {
				return fmt.Errorf("config validation: %s.host is required when mode is 'host'", prefix)
			}
		default:
			return fmt.Errorf("config validation: %s.mode invalid value %q: must be 'docker' or 'host'", prefix, db.Mode)
		}

		if db.Password == "" && logger != nil {
			logger.Warn("password is empty after env var interpolation",
				"database", db.Name, "index", i)
		}
	}

	return nil
}

// MergeDefaults copies default values into each database config where fields
// are zero-valued. Database-level values always override defaults.
func (c *Config) MergeDefaults() {
	for i := range c.Databases {
		db := &c.Databases[i]

		if db.Mode == "" {
			db.Mode = c.Defaults.Mode
		}
		if db.Container == "" {
			db.Container = c.Defaults.Container
		}
		if db.Host == "" && db.Mode == "host" {
			db.Host = c.Defaults.Host
		}
		if db.Port == 0 {
			if c.Defaults.Port != 0 {
				db.Port = c.Defaults.Port
			} else {
				db.Port = 5432
			}
		}
		if db.Username == "" {
			db.Username = c.Defaults.Username
		}
		if db.Password == "" {
			db.Password = c.Defaults.Password
		}
		if db.PartitionSize == "" {
			if c.Defaults.PartitionSize != "" {
				db.PartitionSize = c.Defaults.PartitionSize
			} else {
				db.PartitionSize = "100KB"
			}
		}
		if len(db.PgDumpArgs) == 0 && len(c.Defaults.PgDumpArgs) > 0 {
			db.PgDumpArgs = make([]string, len(c.Defaults.PgDumpArgs))
			copy(db.PgDumpArgs, c.Defaults.PgDumpArgs)
		}
		if db.OutputDir == "" {
			db.OutputDir = c.Defaults.OutputDir
		}
	}

	// Git defaults
	if c.Git.Remote == "" {
		c.Git.Remote = "origin"
	}
	if c.Git.Branch == "" {
		c.Git.Branch = "main"
	}
	if c.Git.CommitMessageTemplate == "" {
		c.Git.CommitMessageTemplate = "backup: {{.DbName}} @ {{.Timestamp}}"
	}

	// Logging defaults
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "text"
	}
}

// ParseSize converts a human-readable size string to bytes.
// Supports: "100KB", "1MB", "1GB", or plain numbers (bytes).
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 102400, nil // default 100KB
	}

	upper := strings.ToUpper(s)

	multipliers := map[string]int64{
		"KB": 1024,
		"MB": 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
	}

	for suffix, mult := range multipliers {
		if strings.HasSuffix(upper, suffix) {
			numStr := strings.TrimSpace(s[:len(s)-len(suffix)])
			n, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size %q: %w", s, err)
			}
			return int64(n * float64(mult)), nil
		}
	}

	// Try plain number (bytes)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: must be a number or use KB/MB/GB suffix", s)
	}
	return n, nil
}

// PartitionSizeBytes returns the partition size in bytes for a database config.
func (d *DatabaseConfig) PartitionSizeBytes() (int64, error) {
	return ParseSize(d.PartitionSize)
}
