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
			c, err := Load(env(map[string]string{"APP_ENV": tt.env, "DATABASE_URL": "db"}))
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
				DatabaseURL:     "db",
				MigrateOnStart:  true,
				// kickstart:begin db=postgres
				DBMaxOpenConns:    25,
				DBConnMaxIdleTime: 5 * time.Minute,
				DBConnMaxLifetime: 30 * time.Minute,
				// kickstart:end
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
		"DATABASE_URL":     "db",
		"MIGRATE_ON_START": "false",
		// kickstart:begin db=postgres
		"DATABASE_READ_URL":     "replica",
		"DB_MAX_OPEN_CONNS":     "10",
		"DB_CONN_MAX_IDLE_TIME": "1m",
		"DB_CONN_MAX_LIFETIME":  "1h",
		// kickstart:end
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
		DatabaseURL:     "db",
		MigrateOnStart:  false,
		// kickstart:begin db=postgres
		DatabaseReadURL:   "replica",
		DBMaxOpenConns:    10,
		DBConnMaxIdleTime: time.Minute,
		DBConnMaxLifetime: time.Hour,
		// kickstart:end
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
		"MIGRATE_ON_START": "maybe",
	}))
	if err == nil {
		t.Fatal("Load succeeded, want error")
	}
	for _, key := range []string{
		"APP_ENV", "LOG_FORMAT", "LOG_LEVEL", "SHUTDOWN_DRAIN", "SHUTDOWN_TIMEOUT",
		"DATABASE_URL", "MIGRATE_ON_START",
	} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s:\n%v", key, err)
		}
	}
}

// TestLogValueHidesSecrets guards against a database password reaching the
// logs through the startup "config" line.
func TestLogValueHidesSecrets(t *testing.T) {
	t.Parallel()
	c := Config{DatabaseURL: "db://user:s3cret@host/db"}
	// kickstart:begin db=postgres
	c.DatabaseReadURL = "db://user:s3cret@replica/db"
	// kickstart:end
	if s := c.LogValue().String(); strings.Contains(s, "s3cret") {
		t.Errorf("LogValue leaks a secret: %s", s)
	}
}
