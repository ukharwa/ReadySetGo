// Package config loads the application's configuration from environment
// variables. It is the only package that reads the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Config holds every setting the application reads from the environment.
// Each variable is documented in .env.example.
type Config struct {
	Env             string         // APP_ENV: "dev" or "prod"
	AppName         string         // APP_NAME: shown in pages and emails
	AppURL          string         // APP_URL: the site's address, for links in emails
	HTTPAddr        string         // HTTP_ADDR
	LogFormat       string         // LOG_FORMAT: "text" or "json"
	LogLevel        slog.Level     // LOG_LEVEL
	ShutdownDrain   time.Duration  // SHUTDOWN_DRAIN
	ShutdownTimeout time.Duration  // SHUTDOWN_TIMEOUT
	TrustedProxies  []netip.Prefix // TRUSTED_PROXIES

	DatabaseURL    string // DATABASE_URL (secret: may hold a password)
	MigrateOnStart bool   // MIGRATE_ON_START
	// kickstart:begin db=postgres
	DatabaseReadURL   string        // DATABASE_READ_URL (secret)
	DBMaxOpenConns    int           // DB_MAX_OPEN_CONNS
	DBConnMaxIdleTime time.Duration // DB_CONN_MAX_IDLE_TIME
	DBConnMaxLifetime time.Duration // DB_CONN_MAX_LIFETIME
	// kickstart:end

	SMTPHost     string // SMTP_HOST: empty in dev means emails are logged, not sent
	SMTPPort     int    // SMTP_PORT
	SMTPUsername string // SMTP_USERNAME
	SMTPPassword string // SMTP_PASSWORD (secret)
	SMTPFrom     string // SMTP_FROM
	SMTPTLS      string // SMTP_TLS: "starttls", "tls" or "none"

	WorkerEnabled     bool // WORKER_ENABLED: run the job worker inside `serve`
	WorkerConcurrency int  // WORKER_CONCURRENCY

	PasswordMinLength int // PASSWORD_MIN_LENGTH
}

// Dev reports whether the application runs in development mode.
func (c Config) Dev() bool { return c.Env == "dev" }

// LogValue lists the settings worth logging at startup. Secrets must never
// be added here.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.Env),
		slog.String("app_name", c.AppName),
		slog.String("app_url", c.AppURL),
		slog.String("http_addr", c.HTTPAddr),
		slog.String("log_format", c.LogFormat),
		slog.String("log_level", c.LogLevel.String()),
		slog.Duration("shutdown_drain", c.ShutdownDrain),
		slog.Duration("shutdown_timeout", c.ShutdownTimeout),
		slog.Any("trusted_proxies", c.TrustedProxies),
		slog.Bool("migrate_on_start", c.MigrateOnStart),
		// kickstart:begin db=postgres
		slog.Bool("read_replica", c.DatabaseReadURL != ""),
		slog.Int("db_max_open_conns", c.DBMaxOpenConns),
		slog.Duration("db_conn_max_idle_time", c.DBConnMaxIdleTime),
		slog.Duration("db_conn_max_lifetime", c.DBConnMaxLifetime),
		// kickstart:end
		slog.String("smtp_host", c.SMTPHost),
		slog.Int("smtp_port", c.SMTPPort),
		slog.String("smtp_tls", c.SMTPTLS),
		slog.Bool("worker_enabled", c.WorkerEnabled),
		slog.Int("worker_concurrency", c.WorkerConcurrency),
		slog.Int("password_min_length", c.PasswordMinLength),
	)
}

// Load reads the configuration using getenv (os.Getenv in production) and
// validates it. All problems are reported together.
func Load(getenv func(string) string) (Config, error) {
	l := loader{getenv: getenv}

	var c Config
	c.Env = l.oneOf("APP_ENV", "", "dev", "prod")
	c.AppName = l.str("APP_NAME", "kickstart")
	defaultURL := ""
	if c.Dev() {
		defaultURL = "http://localhost:8080"
	}
	c.AppURL = strings.TrimSuffix(l.str("APP_URL", defaultURL), "/")
	if u, err := url.Parse(c.AppURL); c.AppURL == "" || err != nil || u.Host == "" ||
		(u.Scheme != "https" && (u.Scheme != "http" || !c.Dev())) || u.Path != "" || u.RawQuery != "" {
		l.fail("APP_URL", fmt.Sprintf("must be the site's address, like https://example.com (http only in dev; got %q)", c.AppURL))
	}
	c.HTTPAddr = l.str("HTTP_ADDR", ":8080")
	defaultFormat := "json"
	if c.Dev() {
		defaultFormat = "text"
	}
	c.LogFormat = l.oneOf("LOG_FORMAT", defaultFormat, "text", "json")
	c.LogLevel = l.level("LOG_LEVEL", slog.LevelInfo)
	c.ShutdownDrain = l.duration("SHUTDOWN_DRAIN", 0)
	c.ShutdownTimeout = l.duration("SHUTDOWN_TIMEOUT", 25*time.Second)
	c.TrustedProxies = l.prefixes("TRUSTED_PROXIES")

	c.DatabaseURL = l.required("DATABASE_URL")
	c.MigrateOnStart = l.boolean("MIGRATE_ON_START", true)
	// kickstart:begin db=postgres
	c.DatabaseReadURL = l.str("DATABASE_READ_URL", "")
	c.DBMaxOpenConns = l.integer("DB_MAX_OPEN_CONNS", 25)
	c.DBConnMaxIdleTime = l.duration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute)
	c.DBConnMaxLifetime = l.duration("DB_CONN_MAX_LIFETIME", 30*time.Minute)
	if c.DBMaxOpenConns < 1 {
		l.fail("DB_MAX_OPEN_CONNS", "must be at least 1")
	}
	// kickstart:end

	c.SMTPHost = l.str("SMTP_HOST", "")
	c.SMTPPort = l.integer("SMTP_PORT", 587)
	c.SMTPUsername = l.str("SMTP_USERNAME", "")
	c.SMTPPassword = l.str("SMTP_PASSWORD", "")
	c.SMTPFrom = l.str("SMTP_FROM", "")
	c.SMTPTLS = l.oneOf("SMTP_TLS", "starttls", "starttls", "tls", "none")
	switch {
	case c.SMTPHost == "" && !c.Dev():
		l.fail("SMTP_HOST", "is required in prod (only dev may log emails instead of sending them)")
	case c.SMTPHost != "":
		if _, err := mail.ParseAddress(c.SMTPFrom); err != nil {
			l.fail("SMTP_FROM", fmt.Sprintf("must be an address such as App <noreply@example.com> (got %q)", c.SMTPFrom))
		}
		if c.SMTPPort < 1 || c.SMTPPort > 65535 {
			l.fail("SMTP_PORT", "must be between 1 and 65535")
		}
		if c.SMTPTLS == "none" && !c.Dev() && !isLoopback(c.SMTPHost) {
			l.fail("SMTP_TLS", "may only be none in prod for a relay on this machine (localhost)")
		}
	}

	c.WorkerEnabled = l.boolean("WORKER_ENABLED", true)
	c.WorkerConcurrency = l.integer("WORKER_CONCURRENCY", 4)
	if c.WorkerConcurrency < 1 {
		l.fail("WORKER_CONCURRENCY", "must be at least 1")
	}

	c.PasswordMinLength = l.integer("PASSWORD_MIN_LENGTH", 12)
	if c.PasswordMinLength < 8 || c.PasswordMinLength > 64 {
		l.fail("PASSWORD_MIN_LENGTH", "must be between 8 and 64")
	}

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

func (l *loader) required(key string) string {
	v := l.getenv(key)
	if v == "" {
		l.fail(key, "is required")
	}
	return v
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

func (l *loader) boolean(key string, def bool) bool {
	v := l.getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.fail(key, fmt.Sprintf("must be true or false (got %q)", v))
		return def
	}
	return b
}

func (l *loader) integer(key string, def int) int {
	v := l.getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.fail(key, fmt.Sprintf("must be a whole number (got %q)", v))
		return def
	}
	return n
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

// prefixes reads a comma-separated list of IP addresses and CIDR ranges.
func (l *loader) prefixes(key string) []netip.Prefix {
	var out []netip.Prefix
	for item := range strings.SplitSeq(l.getenv(key), ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if p, err := netip.ParsePrefix(item); err == nil {
			out = append(out, p.Masked())
		} else if ip, err := netip.ParseAddr(item); err == nil {
			out = append(out, netip.PrefixFrom(ip, ip.BitLen()))
		} else {
			l.fail(key, fmt.Sprintf("must list IP addresses or CIDR ranges (bad entry %q)", item))
		}
	}
	return out
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

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}
