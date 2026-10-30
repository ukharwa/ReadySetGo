package httpx

import (
	"context"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

type clientIPKey struct{}

// RealIP finds the client's IP address and puts it in the request's
// context, where ClientIP reads it.
//
// X-Forwarded-For is only believed when the request arrives from one of the
// trusted proxies (Caddy), because anyone can send the header. The header is
// then read from the right, skipping trusted proxies; the first address that
// isn't trusted is the client. Without this, a client could pick its own IP
// and dodge per-IP rate limits.
func RealIP(trusted []netip.Prefix) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientIP(r, trusted)
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip)))
		})
	}
}

// ClientIP returns the client IP address found by RealIP. It is the zero
// Addr (which prints as "invalid IP") if RealIP didn't run.
func ClientIP(ctx context.Context) netip.Addr {
	ip, _ := ctx.Value(clientIPKey{}).(netip.Addr)
	return ip
}

func clientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	isTrusted := func(ip netip.Addr) bool {
		return slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(ip) })
	}

	remote, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	ip := remote.Addr().Unmap()
	if !isTrusted(ip) {
		return ip
	}

	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for _, hop := range slices.Backward(hops) {
		hopIP, err := netip.ParseAddr(strings.TrimSpace(hop))
		if err != nil {
			break // malformed: keep the last address a trusted proxy vouched for
		}
		ip = hopIP.Unmap()
		if !isTrusted(ip) {
			break
		}
	}
	return ip
}
