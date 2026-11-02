package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ukharwa/readysetgo/internal/logging"
)

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestChainOrder(t *testing.T) {
	t.Parallel()
	var order []string
	tag := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }),
		tag("outer"), tag("inner"))
	serve(h, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := strings.Join(order, ","); got != "outer,inner,handler" {
		t.Errorf("order = %s", got)
	}
}

func TestRequestID(t *testing.T) {
	t.Parallel()
	var seen string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = logging.RequestID(r.Context())
	}))

	tests := []struct {
		name, incoming string
		keep           bool
	}{
		{"none", "", false},
		{"caddy uuid", "0193c2b1-7c7e-7d2a-8f00-0242ac120002", true},
		{"too long", strings.Repeat("a", 65), false},
		{"newline", "abc\nlevel=ERROR", false},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if tt.incoming != "" {
			r.Header.Set("X-Request-ID", tt.incoming)
		}
		rec := serve(h, r)
		got := rec.Header().Get("X-Request-ID")
		if got == "" || got != seen {
			t.Errorf("%s: header %q, context %q: want the same non-empty ID", tt.name, got, seen)
		}
		if (got == tt.incoming) != tt.keep {
			t.Errorf("%s: kept incoming ID = %v, want %v", tt.name, got == tt.incoming, tt.keep)
		}
	}
}

func TestAccessLog(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := logging.New(&buf, "json", slog.LevelInfo)
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "hello")
	}), RealIP(nil), RequestID, AccessLog(logger))

	serve(h, httptest.NewRequest(http.MethodPost, "/reset?token=secret", nil))

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line: %v\n%s", err, buf.String())
	}
	want := map[string]any{"msg": "request", "method": "POST", "path": "/reset", "status": 201.0, "bytes": 5.0, "ip": "192.0.2.1"}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("%s = %v, want %v", k, line[k], v)
		}
	}
	if line["request_id"] == nil {
		t.Error("access log line has no request_id")
	}
	if strings.Contains(buf.String(), "secret") {
		t.Error("access log contains the query string")
	}
}

func TestRecover(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := logging.New(&buf, "json", slog.LevelInfo)
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
		RequestID, Recover(logger))

	rec := serve(h, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	id := rec.Header().Get("X-Request-ID")
	if !strings.Contains(rec.Body.String(), id) {
		t.Errorf("error body doesn't show the request ID %q:\n%s", id, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Error("error body leaks the panic value")
	}
	if !strings.Contains(buf.String(), "boom") || !strings.Contains(buf.String(), "stack") {
		t.Errorf("panic not logged with its stack:\n%s", buf.String())
	}
}

func TestRecoverRethrowsAbort(t *testing.T) {
	t.Parallel()
	h := Recover(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if err, _ := recover().(error); !errors.Is(err, http.ErrAbortHandler) {
			t.Errorf("recovered %v, want http.ErrAbortHandler to pass through", err)
		}
	}()
	serve(h, httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	ok := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	page := serve(SecurityHeaders(true)(ok), httptest.NewRequest(http.MethodGet, "/account", nil)).Header()
	for k, v := range map[string]string{
		"Content-Security-Policy":    pageCSP,
		"Strict-Transport-Security":  "max-age=63072000; includeSubDomains",
		"X-Content-Type-Options":     "nosniff",
		"Referrer-Policy":            "strict-origin-when-cross-origin",
		"Cross-Origin-Opener-Policy": "same-origin",
		"Cache-Control":              "no-store",
	} {
		if got := page.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	api := serve(SecurityHeaders(false)(ok), httptest.NewRequest(http.MethodGet, "/api/things", nil)).Header()
	if got := api.Get("Content-Security-Policy"); got != apiCSP {
		t.Errorf("API CSP = %q, want %q", got, apiCSP)
	}
	if api.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS sent with hsts disabled")
	}
}

func TestCrossOrigin(t *testing.T) {
	t.Parallel()
	h := CrossOrigin(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	tests := []struct {
		method, site string
		want         int
	}{
		{http.MethodPost, "cross-site", http.StatusForbidden},
		{http.MethodPost, "same-origin", http.StatusOK},
		{http.MethodPost, "", http.StatusOK}, // no browser headers: curl, mobile apps
		{http.MethodGet, "cross-site", http.StatusOK},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(tt.method, "/", nil)
		if tt.site != "" {
			r.Header.Set("Sec-Fetch-Site", tt.site)
		}
		if got := serve(h, r).Code; got != tt.want {
			t.Errorf("%s with Sec-Fetch-Site %q: status %d, want %d", tt.method, tt.site, got, tt.want)
		}
	}
}

func TestMaxBytes(t *testing.T) {
	t.Parallel()
	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			t.Errorf("read body: %v", err)
		}
	})
	body := func(n int) io.Reader { return strings.NewReader(strings.Repeat("x", n)) }

	global := MaxBytes(10)(read)
	if got := serve(global, httptest.NewRequest(http.MethodPost, "/", body(10))).Code; got != http.StatusOK {
		t.Errorf("at limit: %d, want 200", got)
	}
	if got := serve(global, httptest.NewRequest(http.MethodPost, "/", body(11))).Code; got != http.StatusRequestEntityTooLarge {
		t.Errorf("over limit: %d, want 413", got)
	}

	raised := Chain(read, MaxBytes(10), MaxBytes(100)) // a route raising the default
	if got := serve(raised, httptest.NewRequest(http.MethodPost, "/", body(50))).Code; got != http.StatusOK {
		t.Errorf("raised limit: %d, want 200", got)
	}
}

func TestTimeout(t *testing.T) {
	t.Parallel()
	h := Timeout(time.Minute)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > time.Minute {
			t.Errorf("deadline = %v (set: %v), want about a minute from now", deadline, ok)
		}
	}))
	serve(h, httptest.NewRequest(http.MethodGet, "/", nil))
}
