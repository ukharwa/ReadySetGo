package httpx

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/ukharwa/kickstart/internal/logging"
)

// Middleware wraps a handler with behaviour shared by many routes.
type Middleware = func(http.Handler) http.Handler

// Chain wraps h in middleware. The first one listed is the outermost: it
// sees the request first and the response last.
func Chain(h http.Handler, middleware ...Middleware) http.Handler {
	for _, m := range slices.Backward(middleware) {
		h = m(h)
	}
	return h
}

// Stack returns the middleware every application request passes through,
// outermost first: real client IP, request ID, access log, panic recovery,
// security headers, the cross-origin (CSRF) check, the body limit and the
// request timeout. hsts turns on Strict-Transport-Security.
func Stack(logger *slog.Logger, trustedProxies []netip.Prefix, hsts bool) []Middleware {
	return []Middleware{
		RealIP(trustedProxies),
		RequestID,
		AccessLog(logger),
		Recover(logger),
		SecurityHeaders(hsts),
		CrossOrigin(logger),
		MaxBytes(DefaultMaxBodyBytes),
		Timeout(RequestTimeout),
	}
}

// validRequestID accepts the IDs Caddy generates (UUIDs) and similar, and
// nothing that could make a log line misleading.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// RequestID gives every request an ID: the incoming X-Request-ID header if
// it is well-formed (Caddy sets one), otherwise a new random one. The ID is
// put in the request's context, so slog's …Context methods log it, and
// returned in the X-Request-ID response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID.MatchString(id) {
			id = rand.Text()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(logging.WithRequestID(r.Context(), id)))
	})
}

// AccessLog writes one log line per request. It logs the path without the
// query string, because password-reset and verification links carry tokens
// there.
func AccessLog(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &responseRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.Status()),
				slog.Duration("duration", time.Since(start)),
				slog.Int64("bytes", rec.bytes),
				slog.String("ip", ClientIP(r.Context()).String()),
			)
		})
	}
}

// responseRecorder notes the status code and body size of a response.
type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseRecorder) WriteHeader(code int) {
	if w.status == 0 && code >= 200 { // 1xx responses are informational
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *responseRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Status returns the status code sent, or 200 if the handler wrote nothing.
func (w *responseRecorder) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// Recover turns a panic in a handler into a 500 response and an error log
// with the stack trace, instead of a dropped connection.
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v) // net/http's deliberate way to abort a response
				}
				logger.ErrorContext(r.Context(), "panic serving request",
					"panic", v, "stack", string(debug.Stack()))
				Error(w, r, http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Content-Security-Policy values. Pages may load scripts, styles and images
// only from this site and can't be framed; JSON responses need nothing.
const (
	pageCSP = "default-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'self'; " +
		"frame-ancestors 'none'; form-action 'self'"
	apiCSP = "default-src 'none'; frame-ancestors 'none'"
)

// SecurityHeaders sets the security headers every response carries. hsts
// turns on Strict-Transport-Security; only enable it when the site is served
// over HTTPS (production). Responses default to Cache-Control: no-store so
// private pages never linger in a browser's cache; handlers for cacheable
// content (static files) override it.
func SecurityHeaders(hsts bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			if strings.HasPrefix(r.URL.Path, "/api/") {
				h.Set("Content-Security-Policy", apiCSP)
			} else {
				h.Set("Content-Security-Policy", pageCSP)
			}
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cache-Control", "no-store")
			next.ServeHTTP(w, r)
		})
	}
}

// CrossOrigin rejects state-changing requests (POST, PUT, PATCH, DELETE) that
// a browser marks as coming from another site: the application's CSRF
// protection. It uses net/http's CrossOriginProtection, which reads the
// Sec-Fetch-Site and Origin headers, so forms need no CSRF tokens. Requests
// without those headers (curl, mobile apps) pass, since CSRF needs a browser.
func CrossOrigin(logger *slog.Logger) Middleware {
	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.InfoContext(r.Context(), "cross-origin request rejected",
			"method", r.Method, "path", r.URL.Path)
		Error(w, r, http.StatusForbidden)
	}))
	return cop.Handler
}

// DefaultMaxBodyBytes is the request body limit unless a route sets its own.
const DefaultMaxBodyBytes = 1 << 20 // 1 MiB

type rawBodyKey struct{}

// MaxBytes limits request bodies to n bytes; reading past the limit fails
// with *http.MaxBytesError. Applied again further in, for one route, the
// innermost limit wins, so a route can raise or lower the global default.
func MaxBytes(n int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			raw, ok := ctx.Value(rawBodyKey{}).(io.ReadCloser)
			if !ok {
				raw = r.Body
				ctx = context.WithValue(ctx, rawBodyKey{}, raw)
			}
			r = r.WithContext(ctx) // a copy, so the caller's request keeps its body
			r.Body = http.MaxBytesReader(w, raw, n)
			next.ServeHTTP(w, r)
		})
	}
}

// RequestTimeout bounds the context of every request, so database and
// outbound calls made with it can't outlive the response. It is a little
// below the server's 30-second write timeout, so a handler sees its deadline
// before the connection is cut.
const RequestTimeout = 25 * time.Second

// Timeout gives each request's context a deadline d from now.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
