package initialize

import (
	"os"
	"path/filepath"
	"strings"
)

const gitignoreContent = `# Oxy Backup — auto-generated .gitignore

# Temporary restore files
restore_*.sql
*.restore.tmp

# Environment files (may contain database passwords)
.env
.env.*
.env.local

# OS metadata
.DS_Store
Thumbs.db

# Editor swap/temp files
*.swp
*.swo
*~

# Oxy binary (if accidentally placed here)
/oxy
`

// WriteGitignore creates or appends to .gitignore in the target directory.
// If .gitignore already exists, only missing patterns are appended.
func WriteGitignore(targetDir string) error {
	path := filepath.Join(targetDir, ".gitignore")

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if os.IsNotExist(err) {
		// No existing file — write the full content
		return os.WriteFile(path, []byte(gitignoreContent), 0644)
	}

	// File exists — append only missing patterns
	existingStr := string(existing)
	lines := strings.Split(gitignoreContent, "\n")
	var toAppend []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.Contains(existingStr, trimmed) {
			toAppend = append(toAppend, trimmed)
		}
	}

	if len(toAppend) == 0 {
		return nil // nothing to add
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	content := "\n# Added by oxy init\n"
	for _, pattern := range toAppend {
		content += pattern + "\n"
	}

	_, err = f.WriteString(content)
	return err
}
