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

func TestPrompter_Ask_EOF_ReturnsDefault(t *testing.T) {
	in := strings.NewReader("") // empty — EOF immediately
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got := p.ask("Enter name", "fallback")

	if got != "fallback" {
		t.Errorf("ask() on EOF = %q, want default %q", got, "fallback")
	}
}

func TestPrompter_AskRequired_RetriesOnEmpty(t *testing.T) {
	in := strings.NewReader("\nvalue\n")
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got, err := p.askRequired("Enter field")
	if err != nil {
		t.Fatalf("askRequired() error: %v", err)
	}

	if got != "value" {
		t.Errorf("askRequired() = %q, want %q", got, "value")
	}
	if !strings.Contains(out.String(), "This field is required") {
		t.Errorf("output should contain 'This field is required', got: %s", out.String())
	}
}

func TestPrompter_AskRequired_EOF_ReturnsError(t *testing.T) {
	in := strings.NewReader("") // EOF immediately
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	_, err := p.askRequired("Enter field")
	if err == nil {
		t.Fatal("askRequired() on EOF should return error")
	}
	if err != errEOF {
		t.Errorf("askRequired() error = %v, want errEOF", err)
	}
}

func TestPrompter_SelectOne_Default(t *testing.T) {
	in := strings.NewReader("\n")
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got, err := p.selectOne("Pick one:", []string{"alpha", "beta", "gamma"}, 0)
	if err != nil {
		t.Fatalf("selectOne() error: %v", err)
	}

	if got != "alpha" {
		t.Errorf("selectOne() = %q, want %q", got, "alpha")
	}
}

func TestPrompter_SelectOne_ValidChoice(t *testing.T) {
	in := strings.NewReader("2\n")
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got, err := p.selectOne("Pick one:", []string{"alpha", "beta", "gamma"}, 0)
	if err != nil {
		t.Fatalf("selectOne() error: %v", err)
	}

	if got != "beta" {
		t.Errorf("selectOne() = %q, want %q", got, "beta")
	}
}

func TestPrompter_SelectOne_InvalidChoice(t *testing.T) {
	// Invalid input "99" triggers re-prompt, then empty input accepts default
	in := strings.NewReader("99\n\n")
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got, err := p.selectOne("Pick one:", []string{"alpha", "beta", "gamma"}, 0)
	if err != nil {
		t.Fatalf("selectOne() error: %v", err)
	}

	if got != "alpha" {
		t.Errorf("selectOne() = %q, want default %q for invalid input", got, "alpha")
	}
	if !strings.Contains(out.String(), "Invalid choice") {
		t.Error("expected 'Invalid choice' message in output")
	}
}

func TestPrompter_SelectOne_EOF_ReturnsDefault(t *testing.T) {
	in := strings.NewReader("") // EOF
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	got, err := p.selectOne("Pick one:", []string{"alpha", "beta"}, 1)
	if err == nil {
		t.Fatal("selectOne() on EOF should return error")
	}
	// Even on error, it should return the default value
	if got != "beta" {
		t.Errorf("selectOne() on EOF = %q, want default %q", got, "beta")
	}
}

// --- IsSSHURL ---

func TestIsSSHURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"git@github.com:user/repo.git", true},
		{"git@gitlab.com:org/project.git", true},
		{"ssh://git@github.com/user/repo.git", true},
		{"ssh://server.com/path", true},
		{"https://github.com/user/repo.git", false},
		{"http://github.com/user/repo.git", false},
		{"", false},
		{"/local/path", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := IsSSHURL(tt.url)
			if got != tt.want {
				t.Errorf("IsSSHURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// --- RunInteractive with SSH ---

func TestRunInteractive_DockerMode(t *testing.T) {
	// Answers in order:
	// 1. Git remote URL (SSH — triggers SSH prompts)
	// 2. SSH private key path (default)
	// 3. Passphrase-protected? (n)
	// 4. known_hosts path (empty for default)
	// 5. Database mode (1 = docker)
	// 6. Docker container name (required)
	// 7. Database name (required)
	// 8. PostgreSQL database name
	// 9. Password env var name
	// 10. Partition size
	// 11. Output directory
	answers := strings.Join([]string{
		"git@github.com:user/repo.git", // git remote (SSH)
		"~/.ssh/id_rsa",                // ssh key path
		"n",                            // no passphrase
		"",                             // known_hosts (default)
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
	assertField("SSHKeyPath", opts.SSHKeyPath, "~/.ssh/id_rsa")
	assertField("SSHKeyPassEnv", opts.SSHKeyPassEnv, "")
	assertField("Mode", opts.Mode, "docker")
	assertField("Container", opts.Container, "my-postgres-container")
	assertField("DbName", opts.DbName, "mydb")
	assertField("DbDatabase", opts.DbDatabase, "mydb_prod")
	assertField("PasswordEnv", opts.PasswordEnv, "MY_PG_PASS")
	assertField("PartitionSize", opts.PartitionSize, "2MB")
	assertField("OutputDir", opts.OutputDir, "./output")

	// Verify SSH section was printed
	if !strings.Contains(out.String(), "SSH remote detected") {
		t.Error("output should contain 'SSH remote detected'")
	}
}

func TestRunInteractive_DockerMode_SSHWithPassphrase(t *testing.T) {
	answers := strings.Join([]string{
		"git@github.com:user/repo.git", // git remote (SSH)
		"~/.ssh/id_ed25519",            // ssh key path
		"y",                            // yes passphrase
		"MY_SSH_PASS",                  // passphrase env var
		"",                             // known_hosts (default)
		"1",                            // docker
		"pg-container",                 // container name
		"mydb",                         // database name
		"",                             // postgresql db (default = mydb)
		"",                             // password env (default PGPASSWORD)
		"",                             // partition size (default 100KB)
		"",                             // output dir (default ./backups)
	}, "\n") + "\n"

	in := strings.NewReader(answers)
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	opts, err := p.RunInteractive()
	if err != nil {
		t.Fatalf("RunInteractive() error: %v", err)
	}

	if opts.SSHKeyPath != "~/.ssh/id_ed25519" {
		t.Errorf("SSHKeyPath = %q, want %q", opts.SSHKeyPath, "~/.ssh/id_ed25519")
	}
	if opts.SSHKeyPassEnv != "MY_SSH_PASS" {
		t.Errorf("SSHKeyPassEnv = %q, want %q", opts.SSHKeyPassEnv, "MY_SSH_PASS")
	}
}

func TestRunInteractive_HostMode(t *testing.T) {
	// Answers in order:
	// 1. Git remote URL (SSH — triggers SSH prompts)
	// 2. SSH private key path (default)
	// 3. Passphrase-protected? (n)
	// 4. known_hosts path (empty for default)
	// 5. Database mode (2 = host)
	// 6. PostgreSQL host (default = localhost)
	// 7. PostgreSQL port (default = 5432)
	// 8. PostgreSQL username (default = postgres)
	// 9. Database name (required)
	// 10. PostgreSQL database name (default = db name)
	// 11. Password env var name (default = PGPASSWORD)
	// 12. Partition size (default = 100KB)
	// 13. Output directory (default = ./backups)
	answers := strings.Join([]string{
		"git@github.com:user/backup.git", // git remote (SSH)
		"",                               // ssh key path (default ~/.ssh/id_ed25519)
		"n",                              // no passphrase
		"",                               // known_hosts (default)
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
	assertField("SSHKeyPath", opts.SSHKeyPath, "~/.ssh/id_ed25519")
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

func TestRunInteractive_HTTPSRemote_NoSSHPrompts(t *testing.T) {
	// HTTPS remote should NOT trigger SSH prompts
	answers := strings.Join([]string{
		"https://github.com/user/repo.git", // HTTPS remote
		"1",                                // docker
		"my-container",                     // container name
		"testdb",                           // database name
		"",                                 // postgresql db (default)
		"",                                 // password env (default)
		"",                                 // partition size (default)
		"",                                 // output dir (default)
	}, "\n") + "\n"

	in := strings.NewReader(answers)
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	opts, err := p.RunInteractive()
	if err != nil {
		t.Fatalf("RunInteractive() error: %v", err)
	}

	if opts.SSHKeyPath != "" {
		t.Errorf("SSHKeyPath should be empty for HTTPS remote, got %q", opts.SSHKeyPath)
	}
	if strings.Contains(out.String(), "SSH remote detected") {
		t.Error("HTTPS remote should NOT trigger SSH prompts")
	}
}

func TestRunInteractive_NoRemote_NoSSHPrompts(t *testing.T) {
	// Empty remote should NOT trigger SSH prompts
	answers := strings.Join([]string{
		"",             // no remote
		"1",            // docker
		"my-container", // container name
		"testdb",       // database name
		"",             // postgresql db (default)
		"",             // password env (default)
		"",             // partition size (default)
		"",             // output dir (default)
	}, "\n") + "\n"

	in := strings.NewReader(answers)
	out := &bytes.Buffer{}
	p := NewPrompter(in, out)

	opts, err := p.RunInteractive()
	if err != nil {
		t.Fatalf("RunInteractive() error: %v", err)
	}

	if opts.SSHKeyPath != "" {
		t.Errorf("SSHKeyPath should be empty when no remote, got %q", opts.SSHKeyPath)
	}
}
