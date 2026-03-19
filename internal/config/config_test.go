package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	t.Setenv("TEST_PG_USER", "admin")
	t.Setenv("TEST_PG_PASS", "secret123")

	yaml := `
version: "1"
defaults:
  mode: docker
  partition_size: "100KB"
  pg_dump_args:
    - "--no-owner"
    - "--clean"
git:
  auto_push: true
  remote: origin
  branch: main
databases:
  - name: mydb
    container: pg_main
    database: myapp
    username: "${TEST_PG_USER}"
    password: "${TEST_PG_PASS}"
    output_dir: "./backups/mydb"
`
	cfg := loadFromString(t, yaml)

	if cfg.Version != "1" {
		t.Errorf("version = %q, want %q", cfg.Version, "1")
	}
	if len(cfg.Databases) != 1 {
		t.Fatalf("databases count = %d, want 1", len(cfg.Databases))
	}

	db := cfg.Databases[0]
	if db.Username != "admin" {
		t.Errorf("username = %q, want %q (env interpolation)", db.Username, "admin")
	}
	if db.Password != "secret123" {
		t.Errorf("password = %q, want %q (env interpolation)", db.Password, "secret123")
	}
	if db.Mode != "docker" {
		t.Errorf("mode = %q, want %q (from defaults)", db.Mode, "docker")
	}
	if db.Port != 5432 {
		t.Errorf("port = %d, want %d (default)", db.Port, 5432)
	}
	if len(db.PgDumpArgs) != 2 {
		t.Errorf("pg_dump_args count = %d, want 2 (from defaults)", len(db.PgDumpArgs))
	}
}

func TestMissingRequiredField_Name(t *testing.T) {
	yaml := `
version: "1"
databases:
  - mode: docker
    container: pg
    database: mydb
`
	expectLoadError(t, yaml, "name is required")
}

func TestMissingRequiredField_Database(t *testing.T) {
	yaml := `
version: "1"
databases:
  - name: mydb
    mode: docker
    container: pg
`
	expectLoadError(t, yaml, "database is required")
}

func TestMissingVersion(t *testing.T) {
	yaml := `
databases:
  - name: mydb
    mode: docker
    container: pg
    database: myapp
`
	expectLoadError(t, yaml, "version")
}

func TestDockerModeWithoutContainer(t *testing.T) {
	yaml := `
version: "1"
databases:
  - name: mydb
    mode: docker
    database: myapp
`
	expectLoadError(t, yaml, "container is required")
}

func TestHostModeWithoutHost(t *testing.T) {
	yaml := `
version: "1"
databases:
  - name: mydb
    mode: host
    database: myapp
`
	expectLoadError(t, yaml, "host is required")
}

func TestInvalidMode(t *testing.T) {
	yaml := `
version: "1"
databases:
  - name: mydb
    mode: kubernetes
    database: myapp
`
	expectLoadError(t, yaml, "invalid value \"kubernetes\"")
}

func TestDatabaseOverridesDefaults(t *testing.T) {
	yaml := `
version: "1"
defaults:
  mode: docker
  partition_size: "100KB"
  port: 5432
databases:
  - name: mydb
    container: pg
    database: myapp
    partition_size: "200KB"
    port: 5433
    password: "pass"
`
	cfg := loadFromString(t, yaml)
	db := cfg.Databases[0]

	if db.PartitionSize != "200KB" {
		t.Errorf("partition_size = %q, want %q (override)", db.PartitionSize, "200KB")
	}
	if db.Port != 5433 {
		t.Errorf("port = %d, want %d (override)", db.Port, 5433)
	}
}

func TestUndefinedEnvVarResolvesToEmpty(t *testing.T) {
	// Make sure the env var does NOT exist
	os.Unsetenv("OXY_UNDEFINED_VAR_XYZ")

	yaml := `
version: "1"
databases:
  - name: mydb
    mode: docker
    container: pg
    database: myapp
    password: "${OXY_UNDEFINED_VAR_XYZ}"
`
	cfg := loadFromString(t, yaml)
	if cfg.Databases[0].Password != "" {
		t.Errorf("password = %q, want empty string for undefined env var", cfg.Databases[0].Password)
	}
}

func TestInvalidYAML(t *testing.T) {
	content := `
version: "1"
databases:
  - name: [invalid yaml
`
	expectLoadError(t, content, "parsing config YAML")
}

func TestGitDefaults(t *testing.T) {
	yaml := `
version: "1"
databases:
  - name: mydb
    mode: docker
    container: pg
    database: myapp
    password: "pass"
`
	cfg := loadFromString(t, yaml)

	if cfg.Git.Remote != "origin" {
		t.Errorf("git.remote = %q, want %q", cfg.Git.Remote, "origin")
	}
	if cfg.Git.Branch != "main" {
		t.Errorf("git.branch = %q, want %q", cfg.Git.Branch, "main")
	}
}

// --- helpers ---

func loadFromString(t *testing.T, yamlContent string) *Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	return cfg
}

func expectLoadError(t *testing.T, yamlContent, wantSubstr string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	_, err := Load(path, nil)
	if err == nil {
		t.Fatal("Load() expected error, got nil")
	}
	if !contains(err.Error(), wantSubstr) {
		t.Errorf("error = %q, want substring %q", err.Error(), wantSubstr)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
