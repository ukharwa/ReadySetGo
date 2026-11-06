package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/ukharwa/readysetgo/internal/httpx"
)

// clock is a settable time source for tests.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestMemory() (*Memory, *clock) {
	c := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	m := NewMemory()
	m.now = c.now
	return m, c
}

func TestMemoryWindow(t *testing.T) {
	t.Parallel()
	m, c := newTestMemory()
	ctx := context.Background()
	allow := func() (bool, time.Duration) {
		ok, retry, err := m.Allow(ctx, "k", 3, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return ok, retry
	}

	for i := range 3 {
		if ok, _ := allow(); !ok {
			t.Fatalf("hit %d denied, want allowed", i+1)
		}
	}
	c.t = c.t.Add(20 * time.Second)
	if ok, retry := allow(); ok || retry != 40*time.Second {
		t.Errorf("4th hit: ok=%v retry=%v, want denied with 40s to wait", ok, retry)
	}

	c.t = c.t.Add(40 * time.Second) // the window ends
	if ok, _ := allow(); !ok {
		t.Error("first hit of the next window denied")
	}
}

func TestMemoryReset(t *testing.T) {
	t.Parallel()
	m, _ := newTestMemory()
	ctx := context.Background()
	for range 2 {
		_, _, _ = m.Allow(ctx, "login", 2, time.Minute)
	}
	if err := m.Reset(ctx, "login"); err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := m.Allow(ctx, "login", 2, time.Minute); !ok {
		t.Error("hit after Reset denied")
	}
}

func TestMemorySweep(t *testing.T) {
	t.Parallel()
	m, c := newTestMemory()
	ctx := context.Background()
	_, _, _ = m.Allow(ctx, "old", 1, time.Minute)
	c.t = c.t.Add(2 * time.Minute)
	_, _, _ = m.Allow(ctx, "new", 1, time.Minute)
	if _, kept := m.windows["old"]; kept {
		t.Error("expired window not swept")
	}
}

func TestMemoryConcurrent(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for range 100 {
		wg.Go(func() {
			if ok, _, _ := m.Allow(context.Background(), "k", 10, time.Minute); ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if allowed != 10 {
		t.Errorf("allowed %d of 100 concurrent hits, want exactly 10", allowed)
	}
}

func TestPerIP(t *testing.T) {
	t.Parallel()
	m, _ := newTestMemory()
	h := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		httpx.RealIP(nil), PerIP(m, "auth", 2, time.Minute))

	request := func(ip string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/login", nil)
		r.RemoteAddr = netip.AddrPortFrom(netip.MustParseAddr(ip), 1234).String()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	request("203.0.113.1")
	request("203.0.113.1")
	rec := request("203.0.113.1")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "60" {
		t.Errorf("3rd request: status %d, Retry-After %q; want 429 and 60", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := request("203.0.113.2"); rec.Code != http.StatusOK {
		t.Errorf("another IP: status %d, want 200", rec.Code)
	}

	// One IPv6 network (a /64) is one client: a single machine usually has
	// all of it, so counting each address apart would set no limit.
	request("2001:db8:1:1::1")
	request("2001:db8:1:1::2")
	if rec := request("2001:db8:1:1::3"); rec.Code != http.StatusTooManyRequests {
		t.Errorf("3rd request from one /64: status %d, want 429", rec.Code)
	}
	if rec := request("2001:db8:1:2::1"); rec.Code != http.StatusOK {
		t.Errorf("another /64: status %d, want 200", rec.Code)
	}
}
