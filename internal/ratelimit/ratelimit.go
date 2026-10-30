// Package ratelimit counts events per key in fixed time windows: logins per
// IP, failed logins per email, emails sent per address.
//
// Limiter is a seam: Memory, the default, keeps counters in this process
// only. When the app runs as several instances, each counts separately, so
// an attacker gets up to N times the attempts; SCALING.md covers moving the
// counters to a shared store.
package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ukharwa/kickstart/internal/httpx"
)

// Limiter counts hits per key.
type Limiter interface {
	// Allow counts one hit for key and reports whether the key is still
	// within limit hits in the current window. When it isn't, retryAfter is
	// the time until the window resets.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (ok bool, retryAfter time.Duration, err error)
	// Reset clears key's count, for example after a successful login, so
	// only consecutive failures count towards its limit.
	Reset(ctx context.Context, key string) error
}

// Memory is a Limiter that keeps fixed-window counters in memory.
//
// ponytail: one mutex for all keys; shard the map if it ever shows up in
// profiles, or move to a shared store (SCALING.md) first.
type Memory struct {
	mu        sync.Mutex
	windows   map[string]window
	lastSweep time.Time
	now       func() time.Time // replaced in tests
}

type window struct {
	count   int
	resetAt time.Time
}

// NewMemory returns an empty in-memory limiter.
func NewMemory() *Memory {
	return &Memory{windows: map[string]window{}, now: time.Now}
}

// Allow implements Limiter.
func (m *Memory) Allow(_ context.Context, key string, limit int, period time.Duration) (bool, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	m.sweep(now)
	w := m.windows[key]
	if !now.Before(w.resetAt) {
		w = window{resetAt: now.Add(period)}
	}
	w.count++
	m.windows[key] = w
	if w.count > limit {
		return false, w.resetAt.Sub(now), nil
	}
	return true, 0, nil
}

// Reset implements Limiter.
func (m *Memory) Reset(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.windows, key)
	return nil
}

// sweep drops expired windows at most once a minute, so memory stays bounded
// by the keys seen recently. The caller holds m.mu.
func (m *Memory) sweep(now time.Time) {
	if now.Sub(m.lastSweep) < time.Minute {
		return
	}
	m.lastSweep = now
	for k, w := range m.windows {
		if !now.Before(w.resetAt) {
			delete(m.windows, k)
		}
	}
}

// PerIP is middleware that allows each client IP limit requests per window
// across the routes it wraps, answering the rest with 429 Too Many Requests
// and a Retry-After header. name keeps different limits' counters apart.
func PerIP(l Limiter, name string, limit int, window time.Duration) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := fmt.Sprintf("%s:ip:%s", name, httpx.ClientIP(r.Context()))
			ok, retryAfter, err := l.Allow(r.Context(), key, limit, window)
			if err != nil {
				// Fail closed: an attacker shouldn't gain unlimited attempts
				// because the limiter's store is down.
				httpx.Error(w, r, http.StatusServiceUnavailable)
				return
			}
			if !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Round(time.Second).Seconds())))
				httpx.Error(w, r, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
