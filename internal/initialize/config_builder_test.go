package initialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alenicomar/oxy-backup/internal/config"
	"gopkg.in/yaml.v3"
)

func TestBuildConfig_DockerMode(t *testing.T) {
	opts := &InitOptions{
		Mode:          "docker",
		Container:     "pg_container",
		DbName:        "myapp",
		DbDatabase:    "myapp_prod",
		PasswordEnv:   "PGPASSWORD",
		PartitionSize: "1MB",
		OutputDir:     "./backups",
		GitRemote:     "origin",
	}

	cfg := BuildConfig(opts)

	if cfg.Version != "1" {
		t.Errorf("expected Version '1', got %q", cfg.Version)
	}

	// Defaults
	if cfg.Defaults.Mode != "docker" {
		t.Errorf("expected Defaults.Mode 'docker', got %q", cfg.Defaults.Mode)
	}
	if cfg.Defaults.Container != "pg_container" {
		t.Errorf("expected Defaults.Container 'pg_container', got %q", cfg.Defaults.Container)
	}
	if cfg.Defaults.PartitionSize != "1MB" {
		t.Errorf("expected Defaults.PartitionSize '1MB', got %q", cfg.Defaults.PartitionSize)
	}
	if cfg.Defaults.OutputDir != "./backups" {
		t.Errorf("expected Defaults.OutputDir './backups', got %q", cfg.Defaults.OutputDir)
	}
	if cfg.Defaults.Password != "${PGPASSWORD}" {
		t.Errorf("expected Defaults.Password '${PGPASSWORD}', got %q", cfg.Defaults.Password)
	}

	// Git
	if cfg.Git.AutoPush == nil {
		t.Fatal("expected Git.AutoPush to be non-nil")
	}
	if !*cfg.Git.AutoPush {
		t.Error("expected Git.AutoPush to be true")
	}
	if cfg.Git.Remote != "origin" {
		t.Errorf("expected Git.Remote 'origin', got %q", cfg.Git.Remote)
	}
	if cfg.Git.Branch != "main" {
		t.Errorf("expected Git.Branch 'main', got %q", cfg.Git.Branch)
	}

	// Databases
	if len(cfg.Databases) != 1 {
		t.Fatalf("expected 1 database, got %d", len(cfg.Databases))
	}
	db := cfg.Databases[0]
	if db.Name != "myapp" {
		t.Errorf("expected db.Name 'myapp', got %q", db.Name)
	}
	if db.Database != "myapp_prod" {
		t.Errorf("expected db.Database 'myapp_prod', got %q", db.Database)
	}
	if db.Mode != "docker" {
		t.Errorf("expected db.Mode 'docker', got %q", db.Mode)
	}

	// Logging defaults
	if cfg.Logging.Level != "info" {
		t.Errorf("expected Logging.Level 'info', got %q", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "text" {
		t.Errorf("expected Logging.Format 'text', got %q", cfg.Logging.Format)
	}
}

func TestBuildConfig_HostMode(t *testing.T) {
	opts := &InitOptions{
		Mode:          "host",
		Host:          "db.example.com",
		Port:          5433,
		Username:      "admin",
		DbName:        "analytics",
		DbDatabase:    "analytics_db",
		PasswordEnv:   "DB_PASS",
		PartitionSize: "100KB",
		OutputDir:     "./dumps",
	}

	cfg := BuildConfig(opts)

	if cfg.Defaults.Mode != "host" {
		t.Errorf("expected Defaults.Mode 'host', got %q", cfg.Defaults.Mode)
	}
	if cfg.Defaults.Host != "db.example.com" {
		t.Errorf("expected Defaults.Host 'db.example.com', got %q", cfg.Defaults.Host)
	}
	if cfg.Defaults.Port != 5433 {
		t.Errorf("expected Defaults.Port 5433, got %d", cfg.Defaults.Port)
	}
	if cfg.Defaults.Username != "admin" {
		t.Errorf("expected Defaults.Username 'admin', got %q", cfg.Defaults.Username)
	}
	if cfg.Defaults.Container != "" {
		t.Errorf("expected Defaults.Container to be empty in host mode, got %q", cfg.Defaults.Container)
	}
	if cfg.Defaults.Password != "${DB_PASS}" {
		t.Errorf("expected Defaults.Password '${DB_PASS}', got %q", cfg.Defaults.Password)
	}
}

func TestWriteConfig(t *testing.T) {
	tmpDir := t.TempDir()

	opts := &InitOptions{
		Mode:          "docker",
		Container:     "pg",
		DbName:        "test",
		DbDatabase:    "testdb",
		PasswordEnv:   "PGPASSWORD",
		PartitionSize: "100KB",
		OutputDir:     "./backups",
	}
	cfg := BuildConfig(opts)

	if err := WriteConfig(tmpDir, cfg); err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}

	path := filepath.Join(tmpDir, "oxy.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read written config: %v", err)
	}

	content := string(data)
	if !strings.HasPrefix(content, "# Oxy Backup") {
		t.Errorf("expected file to start with '# Oxy Backup' header, got: %q", content[:min(50, len(content))])
	}

	// Unmarshal back and verify key fields
	var roundTrip config.Config
	// Strip header lines before unmarshaling
	yamlStart := strings.Index(content, "version:")
	if yamlStart < 0 {
		t.Fatal("could not find 'version:' in written file")
	}
	if err := yaml.Unmarshal([]byte(content[yamlStart:]), &roundTrip); err != nil {
		t.Fatalf("failed to unmarshal written config: %v", err)
	}

	if roundTrip.Version != "1" {
		t.Errorf("round-trip Version: expected '1', got %q", roundTrip.Version)
	}
	if len(roundTrip.Databases) != 1 {
		t.Fatalf("round-trip Databases: expected 1, got %d", len(roundTrip.Databases))
	}
	if roundTrip.Databases[0].Name != "test" {
		t.Errorf("round-trip db.Name: expected 'test', got %q", roundTrip.Databases[0].Name)
	}
}

func TestWriteConfig_ValidYAML(t *testing.T) {
	tmpDir := t.TempDir()

	opts := &InitOptions{
		Mode:          "host",
		Host:          "localhost",
		Port:          5432,
		Username:      "postgres",
		DbName:        "app",
		DbDatabase:    "app_db",
		PasswordEnv:   "PG_PASS",
		PartitionSize: "1MB",
		OutputDir:     "./out",
	}
	cfg := BuildConfig(opts)

	if err := WriteConfig(tmpDir, cfg); err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, "oxy.yaml"))
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// yaml.Unmarshal should handle the full file content (comments are valid YAML)
	var parsed config.Config
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("file is not valid YAML: %v", err)
	}

	if parsed.Version != "1" {
		t.Errorf("expected Version '1', got %q", parsed.Version)
	}
	if parsed.Defaults.Mode != "host" {
		t.Errorf("expected Defaults.Mode 'host', got %q", parsed.Defaults.Mode)
	}
	if parsed.Defaults.Host != "localhost" {
		t.Errorf("expected Defaults.Host 'localhost', got %q", parsed.Defaults.Host)
	}
	if parsed.Git.AutoPush == nil || !*parsed.Git.AutoPush {
		t.Error("expected Git.AutoPush to be true after round-trip")
	}
	if len(parsed.Databases) != 1 {
		t.Fatalf("expected 1 database, got %d", len(parsed.Databases))
	}
	if parsed.Databases[0].Database != "app_db" {
		t.Errorf("expected db.Database 'app_db', got %q", parsed.Databases[0].Database)
	}
}

func TestBuildConfig_WithSSHFields(t *testing.T) {
	opts := &InitOptions{
		Mode:              "docker",
		Container:         "pg",
		DbName:            "mydb",
		DbDatabase:        "mydb",
		PasswordEnv:       "PGPASSWORD",
		PartitionSize:     "100KB",
		OutputDir:         "./backups",
		GitRemote:         "git@github.com:user/repo.git",
		SSHKeyPath:        "~/.ssh/id_ed25519",
		SSHKeyPassEnv:     "SSH_PASS",
		SSHKnownHostsPath: "/custom/known_hosts",
	}

	cfg := BuildConfig(opts)

	if cfg.Git.SSHKeyPath != "~/.ssh/id_ed25519" {
		t.Errorf("Git.SSHKeyPath = %q, want %q", cfg.Git.SSHKeyPath, "~/.ssh/id_ed25519")
	}
	if cfg.Git.SSHKeyPassEnv != "SSH_PASS" {
		t.Errorf("Git.SSHKeyPassEnv = %q, want %q", cfg.Git.SSHKeyPassEnv, "SSH_PASS")
	}
	if cfg.Git.SSHKnownHostsPath != "/custom/known_hosts" {
		t.Errorf("Git.SSHKnownHostsPath = %q, want %q", cfg.Git.SSHKnownHostsPath, "/custom/known_hosts")
	}
}

func TestBuildConfig_WithoutSSHFields(t *testing.T) {
	opts := &InitOptions{
		Mode:          "docker",
		Container:     "pg",
		DbName:        "mydb",
		DbDatabase:    "mydb",
		PasswordEnv:   "PGPASSWORD",
		PartitionSize: "100KB",
		OutputDir:     "./backups",
	}

	cfg := BuildConfig(opts)

	if cfg.Git.SSHKeyPath != "" {
		t.Errorf("Git.SSHKeyPath should be empty, got %q", cfg.Git.SSHKeyPath)
	}
	if cfg.Git.SSHKeyPassEnv != "" {
		t.Errorf("Git.SSHKeyPassEnv should be empty, got %q", cfg.Git.SSHKeyPassEnv)
	}
	if cfg.Git.SSHKnownHostsPath != "" {
		t.Errorf("Git.SSHKnownHostsPath should be empty, got %q", cfg.Git.SSHKnownHostsPath)
	}
}
