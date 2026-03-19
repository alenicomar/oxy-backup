package logging

import (
	"log/slog"
	"strings"
	"testing"
)

func TestSetup(t *testing.T) {
	tests := []struct {
		name        string
		level       string
		format      string
		wantErr     bool
		errContains string
		wantHandler string // "*slog.TextHandler" or "*slog.JSONHandler"
	}{
		{
			name:        "text format",
			level:       "info",
			format:      "text",
			wantHandler: "*slog.TextHandler",
		},
		{
			name:        "json format",
			level:       "info",
			format:      "json",
			wantHandler: "*slog.JSONHandler",
		},
		{
			name:        "empty format defaults to text",
			level:       "info",
			format:      "",
			wantHandler: "*slog.TextHandler",
		},
		{
			name:        "empty level defaults to info",
			level:       "",
			format:      "text",
			wantHandler: "*slog.TextHandler",
		},
		{
			name:        "both empty",
			level:       "",
			format:      "",
			wantHandler: "*slog.TextHandler",
		},
		{
			name:        "invalid format",
			level:       "info",
			format:      "xml",
			wantErr:     true,
			errContains: "invalid log format",
		},
		{
			name:        "invalid format yaml",
			level:       "info",
			format:      "yaml",
			wantErr:     true,
			errContains: "invalid log format",
		},
		{
			name:        "invalid level",
			level:       "verbose",
			format:      "text",
			wantErr:     true,
			errContains: "invalid log level",
		},
		{
			name:        "invalid level trace",
			level:       "trace",
			format:      "text",
			wantErr:     true,
			errContains: "invalid log level",
		},
		{
			name:        "case insensitive format",
			level:       "info",
			format:      "JSON",
			wantHandler: "*slog.JSONHandler",
		},
		{
			name:        "case insensitive level",
			level:       "INFO",
			format:      "text",
			wantHandler: "*slog.TextHandler",
		},
		{
			name:        "case insensitive both",
			level:       "DEBUG",
			format:      "JSON",
			wantHandler: "*slog.JSONHandler",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := Setup(tt.level, tt.format)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.errContains)
				}
				if logger != nil {
					t.Error("expected nil logger on error, got non-nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if logger == nil {
				t.Fatal("expected non-nil logger, got nil")
			}

			switch tt.wantHandler {
			case "*slog.TextHandler":
				if _, ok := logger.Handler().(*slog.TextHandler); !ok {
					t.Errorf("handler type = %T, want *slog.TextHandler", logger.Handler())
				}
			case "*slog.JSONHandler":
				if _, ok := logger.Handler().(*slog.JSONHandler); !ok {
					t.Errorf("handler type = %T, want *slog.JSONHandler", logger.Handler())
				}
			default:
				t.Fatalf("test bug: unknown wantHandler %q", tt.wantHandler)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLevel slog.Level
		wantErr   bool
	}{
		{name: "debug", input: "debug", wantLevel: slog.LevelDebug},
		{name: "info", input: "info", wantLevel: slog.LevelInfo},
		{name: "empty defaults to info", input: "", wantLevel: slog.LevelInfo},
		{name: "warn", input: "warn", wantLevel: slog.LevelWarn},
		{name: "warning alias", input: "warning", wantLevel: slog.LevelWarn},
		{name: "error", input: "error", wantLevel: slog.LevelError},
		{name: "invalid", input: "verbose", wantLevel: slog.LevelInfo, wantErr: true},
		{name: "case insensitive", input: "DEBUG", wantLevel: slog.LevelDebug},
		{name: "case insensitive WARN", input: "WARN", wantLevel: slog.LevelWarn},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLevel(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.wantLevel {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.wantLevel)
			}
		})
	}
}
