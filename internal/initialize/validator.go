// Package initialize provides the init subcommand logic for setting up
// new backup repositories.
package initialize

import (
	"fmt"
	"os/exec"
)

// Prerequisite represents a tool check result.
type Prerequisite struct {
	Name     string
	Path     string
	Required bool
	Found    bool
}

// ValidatePrerequisites checks that required tools are available in PATH.
// git is always required. pg_dump, psql, and docker are optional warnings.
func ValidatePrerequisites(mode string) ([]Prerequisite, error) {
	prereqs := []Prerequisite{
		{Name: "git", Required: true},
	}

	switch mode {
	case "docker":
		prereqs = append(prereqs, Prerequisite{Name: "docker", Required: false})
	case "host":
		prereqs = append(prereqs,
			Prerequisite{Name: "pg_dump", Required: false},
			Prerequisite{Name: "psql", Required: false},
		)
	}

	for i := range prereqs {
		path, err := exec.LookPath(prereqs[i].Name)
		if err == nil {
			prereqs[i].Found = true
			prereqs[i].Path = path
		}
		if prereqs[i].Required && !prereqs[i].Found {
			return prereqs, fmt.Errorf("required tool %q not found in PATH", prereqs[i].Name)
		}
	}

	return prereqs, nil
}
