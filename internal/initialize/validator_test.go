package initialize

import (
	"os/exec"
	"testing"
)

func TestValidatePrerequisites_GitFound(t *testing.T) {
	prereqs, err := ValidatePrerequisites("docker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gitPrereq *Prerequisite
	for i := range prereqs {
		if prereqs[i].Name == "git" {
			gitPrereq = &prereqs[i]
			break
		}
	}

	if gitPrereq == nil {
		t.Fatal("git prerequisite not found in results")
	}
	if !gitPrereq.Found {
		t.Error("expected git.Found to be true")
	}
	if gitPrereq.Path == "" {
		t.Error("expected git.Path to be non-empty when found")
	}
}

func TestValidatePrerequisites_DockerMode(t *testing.T) {
	prereqs, err := ValidatePrerequisites("docker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make(map[string]bool)
	for _, p := range prereqs {
		names[p.Name] = true
	}

	if !names["git"] {
		t.Error("expected git in prerequisites for docker mode")
	}
	if !names["docker"] {
		t.Error("expected docker in prerequisites for docker mode")
	}
	if names["pg_dump"] {
		t.Error("pg_dump should NOT be in prerequisites for docker mode")
	}
	if names["psql"] {
		t.Error("psql should NOT be in prerequisites for docker mode")
	}

	if len(prereqs) != 2 {
		t.Errorf("expected 2 prerequisites for docker mode, got %d", len(prereqs))
	}
}

func TestValidatePrerequisites_HostMode(t *testing.T) {
	prereqs, err := ValidatePrerequisites("host")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make(map[string]bool)
	for _, p := range prereqs {
		names[p.Name] = true
	}

	if !names["git"] {
		t.Error("expected git in prerequisites for host mode")
	}
	if !names["pg_dump"] {
		t.Error("expected pg_dump in prerequisites for host mode")
	}
	if !names["psql"] {
		t.Error("expected psql in prerequisites for host mode")
	}
	if names["docker"] {
		t.Error("docker should NOT be in prerequisites for host mode")
	}

	if len(prereqs) != 3 {
		t.Errorf("expected 3 prerequisites for host mode, got %d", len(prereqs))
	}
}

func TestValidatePrerequisites_GitRequired(t *testing.T) {
	// Verify git is available on this system — if it is, ValidatePrerequisites
	// must succeed without error for any valid mode.
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not found in PATH; skipping test")
	}

	prereqs, err := ValidatePrerequisites("docker")
	if err != nil {
		t.Fatalf("expected no error when git is in PATH, got: %v", err)
	}

	for _, p := range prereqs {
		if p.Name == "git" {
			if !p.Required {
				t.Error("git should be marked as Required")
			}
			if !p.Found {
				t.Error("git should be Found when present in PATH")
			}
			return
		}
	}
	t.Fatal("git prerequisite not present in returned slice")
}
