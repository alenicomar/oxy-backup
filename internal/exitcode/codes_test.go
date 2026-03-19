package exitcode

import "testing"

func TestExitCodeValues(t *testing.T) {
	tests := []struct {
		name  string
		got   int
		value int
	}{
		{"OK", OK, 0},
		{"ConfigError", ConfigError, 1},
		{"ConnectionError", ConnectionError, 2},
		{"DumpError", DumpError, 3},
		{"PartitionError", PartitionError, 4},
		{"GitError", GitError, 5},
		{"RestoreError", RestoreError, 6},
		{"PartialFailure", PartialFailure, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.value {
				t.Errorf("%s = %d, want %d", tt.name, tt.got, tt.value)
			}
		})
	}
}

func TestExitCodesAreUnique(t *testing.T) {
	codes := map[int]string{
		OK:              "OK",
		ConfigError:     "ConfigError",
		ConnectionError: "ConnectionError",
		DumpError:       "DumpError",
		PartitionError:  "PartitionError",
		GitError:        "GitError",
		RestoreError:    "RestoreError",
		PartialFailure:  "PartialFailure",
	}

	seen := make(map[int]string)
	for value, name := range codes {
		if prev, exists := seen[value]; exists {
			t.Errorf("exit code %d is shared by %q and %q", value, prev, name)
		}
		seen[value] = name
	}

	const expectedCount = 8
	if len(seen) != expectedCount {
		t.Errorf("expected %d unique exit codes, got %d", expectedCount, len(seen))
	}
}
