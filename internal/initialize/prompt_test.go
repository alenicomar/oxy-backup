package initialize

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrompter_Ask_WithDefault(t *testing.T) {
	in := strings.NewReader("\n")
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got := p.ask("Enter name", "defaultName")

	if got != "defaultName" {
		t.Errorf("ask() = %q, want %q", got, "defaultName")
	}
	if !strings.Contains(out.String(), "[defaultName]") {
		t.Errorf("output should contain default hint, got: %s", out.String())
	}
}

func TestPrompter_Ask_WithInput(t *testing.T) {
	in := strings.NewReader("custom\n")
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got := p.ask("Enter name", "defaultName")

	if got != "custom" {
		t.Errorf("ask() = %q, want %q", got, "custom")
	}
}

func TestPrompter_AskRequired_RetriesOnEmpty(t *testing.T) {
	in := strings.NewReader("\nvalue\n")
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got := p.askRequired("Enter field")

	if got != "value" {
		t.Errorf("askRequired() = %q, want %q", got, "value")
	}
	if !strings.Contains(out.String(), "This field is required") {
		t.Errorf("output should contain 'This field is required', got: %s", out.String())
	}
}

func TestPrompter_SelectOne_Default(t *testing.T) {
	in := strings.NewReader("\n")
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got := p.selectOne("Pick one:", []string{"alpha", "beta", "gamma"}, 0)

	if got != "alpha" {
		t.Errorf("selectOne() = %q, want %q", got, "alpha")
	}
}

func TestPrompter_SelectOne_ValidChoice(t *testing.T) {
	in := strings.NewReader("2\n")
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got := p.selectOne("Pick one:", []string{"alpha", "beta", "gamma"}, 0)

	if got != "beta" {
		t.Errorf("selectOne() = %q, want %q", got, "beta")
	}
}

func TestPrompter_SelectOne_InvalidChoice(t *testing.T) {
	// Invalid input "99" triggers re-prompt, then empty input accepts default
	in := strings.NewReader("99\n\n")
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got := p.selectOne("Pick one:", []string{"alpha", "beta", "gamma"}, 0)

	if got != "alpha" {
		t.Errorf("selectOne() = %q, want default %q for invalid input", got, "alpha")
	}
	if !strings.Contains(out.String(), "Invalid choice") {
		t.Error("expected 'Invalid choice' message in output")
	}
}

func TestRunInteractive_DockerMode(t *testing.T) {
	// Answers in the order the prompts ask:
	// 1. Git remote URL
	// 2. Database mode (1 = docker)
	// 3. Docker container name (required)
	// 4. Database name (required)
	// 5. PostgreSQL database name (default = db name)
	// 6. Password env var name (default = PGPASSWORD)
	// 7. Partition size (default = 100KB)
	// 8. Output directory (default = ./backups)
	answers := strings.Join([]string{
		"git@github.com:user/repo.git", // git remote
		"1",                            // docker
		"my-postgres-container",        // container name
		"mydb",                         // database name (label)
		"mydb_prod",                    // postgresql database name
		"MY_PG_PASS",                   // password env var
		"2MB",                          // partition size
		"./output",                     // output dir
	}, "\n") + "\n"

	in := strings.NewReader(answers)
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	opts, err := p.RunInteractive()
	if err != nil {
		t.Fatalf("RunInteractive() error: %v", err)
	}

	assertField := func(name, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	assertField("GitRemote", opts.GitRemote, "git@github.com:user/repo.git")
	assertField("Mode", opts.Mode, "docker")
	assertField("Container", opts.Container, "my-postgres-container")
	assertField("DbName", opts.DbName, "mydb")
	assertField("DbDatabase", opts.DbDatabase, "mydb_prod")
	assertField("PasswordEnv", opts.PasswordEnv, "MY_PG_PASS")
	assertField("PartitionSize", opts.PartitionSize, "2MB")
	assertField("OutputDir", opts.OutputDir, "./output")
}

func TestRunInteractive_HostMode(t *testing.T) {
	// Answers in the order the prompts ask:
	// 1. Git remote URL
	// 2. Database mode (2 = host)
	// 3. PostgreSQL host (default = localhost)
	// 4. PostgreSQL port (default = 5432)
	// 5. PostgreSQL username (default = postgres)
	// 6. Database name (required)
	// 7. PostgreSQL database name (default = db name)
	// 8. Password env var name (default = PGPASSWORD)
	// 9. Partition size (default = 100KB)
	// 10. Output directory (default = ./backups)
	answers := strings.Join([]string{
		"git@github.com:user/backup.git", // git remote
		"2",                              // host
		"",                               // host (default localhost)
		"",                               // port (default 5432)
		"",                               // username (default postgres)
		"production_db",                  // database name (label)
		"",                               // postgresql database name (default = label)
		"",                               // password env var (default PGPASSWORD)
		"",                               // partition size (default 100KB)
		"",                               // output dir (default ./backups)
	}, "\n") + "\n"

	in := strings.NewReader(answers)
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	opts, err := p.RunInteractive()
	if err != nil {
		t.Fatalf("RunInteractive() error: %v", err)
	}

	assertField := func(name, got, want string) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	assertField("GitRemote", opts.GitRemote, "git@github.com:user/backup.git")
	assertField("Mode", opts.Mode, "host")
	assertField("Host", opts.Host, "localhost")
	if opts.Port != 5432 {
		t.Errorf("Port = %d, want 5432", opts.Port)
	}
	assertField("Username", opts.Username, "postgres")
	assertField("DbName", opts.DbName, "production_db")
	assertField("DbDatabase", opts.DbDatabase, "production_db")
	assertField("PasswordEnv", opts.PasswordEnv, "PGPASSWORD")
	assertField("PartitionSize", opts.PartitionSize, "100KB")
	assertField("OutputDir", opts.OutputDir, "./backups")
}
