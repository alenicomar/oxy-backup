package config

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"100KB", 102400},
		{"1MB", 1048576},
		{"1GB", 1073741824},
		{"100kb", 102400},
		{"512", 512},
		{"", 102400}, // default
		{"0.5MB", 524288},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSize(tt.input)
			if err != nil {
				t.Fatalf("ParseSize(%q) error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSizeErrors(t *testing.T) {
	tests := []string{
		"abc",
		"100XB",
		"KB",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := ParseSize(input)
			if err == nil {
				t.Errorf("ParseSize(%q) expected error, got nil", input)
			}
		})
	}
}
