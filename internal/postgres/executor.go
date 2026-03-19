// Package postgres defines the port (interface) and adapter for PostgreSQL
// tool execution (pg_dump, psql) in both Docker and host modes.
package postgres

import (
	"context"
	"io"

	"github.com/alenicomar/oxy-backup/internal/config"
)

// PgExecutor abstracts PostgreSQL tool execution for testability.
type PgExecutor interface {
	// DumpDatabase executes pg_dump and returns the SQL output as a reader.
	DumpDatabase(ctx context.Context, cfg config.DatabaseConfig) (io.Reader, error)

	// LoadDatabase executes psql with --single-transaction, piping the SQL
	// file contents to stdin.
	LoadDatabase(ctx context.Context, cfg config.DatabaseConfig, sqlFilePath string) error
}
