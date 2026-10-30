// Package config loads the application's configuration from environment
// variables. It is the only package that reads the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
)

// Config holds every setting the application reads from the environment.
// Each variable is documented in .env.example.
type Config struct {
	Env             string        // APP_ENV: "dev" or "prod"
	HTTPAddr        string        // HTTP_ADDR
	LogFormat       string        // LOG_FORMAT: "text" or "json"
	LogLevel        slog.Level    // LOG_LEVEL
	ShutdownDrain   time.Duration // SHUTDOWN_DRAIN
	ShutdownTimeout time.Duration // SHUTDOWN_TIMEOUT
}

// Dev reports whether the application runs in development mode.
func (c Config) Dev() bool { return c.Env == "dev" }

// LogValue lists the settings worth logging at startup. Secrets must never
// be added here.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("http_addr", c.HTTPAddr),
		slog.String("log_format", c.LogFormat),
		slog.String("log_level", c.LogLevel.String()),
		slog.Duration("shutdown_drain", c.ShutdownDrain),
		slog.Duration("shutdown_timeout", c.ShutdownTimeout),
	)
}

// Load reads the configuration using getenv (os.Getenv in production) and
// validates it. All problems are reported together.
func Load(getenv func(string) string) (Config, error) {
	l := loader{getenv: getenv}

	var c Config
	c.Env = l.oneOf("APP_ENV", "", "dev", "prod")
	c.HTTPAddr = l.str("HTTP_ADDR", ":8080")
	defaultFormat := "json"
	if c.Dev() {
		defaultFormat = "text"
	}
	c.LogFormat = l.oneOf("LOG_FORMAT", defaultFormat, "text", "json")
	c.LogLevel = l.level("LOG_LEVEL", slog.LevelInfo)
	c.ShutdownDrain = l.duration("SHUTDOWN_DRAIN", 0)
	c.ShutdownTimeout = l.duration("SHUTDOWN_TIMEOUT", 25*time.Second)

	if c.ShutdownDrain < 0 {
		l.fail("SHUTDOWN_DRAIN", "must not be negative")
	}
	if c.ShutdownTimeout <= 0 {
		l.fail("SHUTDOWN_TIMEOUT", "must be positive")
	}

	if len(l.errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n%w", errors.Join(l.errs...))
	}
	return c, nil
}

// loader reads typed values and collects every problem instead of stopping
// at the first one.
type loader struct {
	getenv func(string) string
	errs   []error
}

func (l *loader) fail(key, msg string) {
	l.errs = append(l.errs, fmt.Errorf("%s %s", key, msg))
}

func (l *loader) str(key, def string) string {
	if v := l.getenv(key); v != "" {
		return v
	}
	return def
}

func (l *loader) oneOf(key, def string, allowed ...string) string {
	v := l.str(key, def)
	switch {
	case v == "":
		l.fail(key, "is required (one of: "+strings.Join(allowed, ", ")+")")
	case !slices.Contains(allowed, v):
		l.fail(key, fmt.Sprintf("must be one of: %s (got %q)", strings.Join(allowed, ", "), v))
	}
	return v
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v := l.getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.fail(key, fmt.Sprintf("must be a duration such as 30s or 5m (got %q)", v))
		return def
	}
	return d
}

func (l *loader) level(key string, def slog.Level) slog.Level {
	v := l.getenv(key)
	if v == "" {
		return def
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(v)); err != nil {
		l.fail(key, fmt.Sprintf("must be debug, info, warn or error (got %q)", v))
		return def
	}
	return lvl
}
