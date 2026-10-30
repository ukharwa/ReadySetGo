package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		env        string
		wantFormat string
	}{
		{"dev", "text"},
		{"prod", "json"},
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Parallel()
			c, err := Load(env(map[string]string{"APP_ENV": tt.env}))
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			want := Config{
				Env:             tt.env,
				HTTPAddr:        ":8080",
				LogFormat:       tt.wantFormat,
				LogLevel:        slog.LevelInfo,
				ShutdownDrain:   0,
				ShutdownTimeout: 25 * time.Second,
			}
			if c != want {
				t.Errorf("Load = %+v, want %+v", c, want)
			}
		})
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Parallel()
	c, err := Load(env(map[string]string{
		"APP_ENV":          "prod",
		"HTTP_ADDR":        "127.0.0.1:9000",
		"LOG_FORMAT":       "text",
		"LOG_LEVEL":        "debug",
		"SHUTDOWN_DRAIN":   "5s",
		"SHUTDOWN_TIMEOUT": "1m",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Env:             "prod",
		HTTPAddr:        "127.0.0.1:9000",
		LogFormat:       "text",
		LogLevel:        slog.LevelDebug,
		ShutdownDrain:   5 * time.Second,
		ShutdownTimeout: time.Minute,
	}
	if c != want {
		t.Errorf("Load = %+v, want %+v", c, want)
	}
}

// TestLoadReportsAllErrors checks that every invalid variable is named in a
// single error, so a misconfigured deploy can be fixed in one pass.
func TestLoadReportsAllErrors(t *testing.T) {
	t.Parallel()
	_, err := Load(env(map[string]string{
		"LOG_FORMAT":       "xml",
		"LOG_LEVEL":        "loud",
		"SHUTDOWN_DRAIN":   "soon",
		"SHUTDOWN_TIMEOUT": "0s",
	}))
	if err == nil {
		t.Fatal("Load succeeded, want error")
	}
	for _, key := range []string{"APP_ENV", "LOG_FORMAT", "LOG_LEVEL", "SHUTDOWN_DRAIN", "SHUTDOWN_TIMEOUT"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s:\n%v", key, err)
		}
	}
}
