package metrics_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ukharwa/kickstart/internal/db/dbtest"
	"github.com/ukharwa/kickstart/internal/jobs"
	"github.com/ukharwa/kickstart/internal/metrics"
)

func TestMetrics(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	m := metrics.New(d, slog.New(slog.DiscardHandler))
	if err := jobs.Enqueue(t.Context(), d.Write, "test.job", nil); err != nil {
		t.Fatal(err)
	}

	app := http.NewServeMux()
	app.HandleFunc("GET /things/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := m.Middleware(app)
	for _, path := range []string{"/things/1", "/things/2", "/nowhere"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	body := fetch(t, srv.URL+"/metrics")
	for _, want := range []string{
		`http_requests_total{code="418",route="GET /things/{id}"} 2`,
		`http_requests_total{code="404",route="unmatched"} 1`,
		`http_request_duration_seconds_count{route="GET /things/{id}"} 2`,
		"jobs_queued 1",
		"jobs_failed 0",
		`go_sql_open_connections{db_name="write"}`,
		"go_goroutines",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %s", want)
		}
	}
	if body := fetch(t, srv.URL+"/debug/pprof/"); !strings.Contains(body, "goroutine") {
		t.Error("no profiler index")
	}
}

func fetch(t *testing.T, url string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
