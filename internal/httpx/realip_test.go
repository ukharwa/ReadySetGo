package httpx

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	t.Parallel()
	caddy := []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}
	tests := []struct {
		name    string
		trusted []netip.Prefix
		remote  string
		xff     []string
		want    string
	}{
		{"no proxy", nil, "203.0.113.7:5000", nil, "203.0.113.7"},
		{"untrusted sender's header is ignored", nil, "203.0.113.7:5000", []string{"1.2.3.4"}, "203.0.113.7"},
		{"behind caddy", caddy, "172.18.0.2:5000", []string{"203.0.113.7"}, "203.0.113.7"},
		{"spoofed entry before the real one", caddy, "172.18.0.2:5000", []string{"1.2.3.4, 203.0.113.7"}, "203.0.113.7"},
		{"several trusted hops", caddy, "172.18.0.2:5000", []string{"203.0.113.7, 172.20.0.5"}, "203.0.113.7"},
		{"header split across lines", caddy, "172.18.0.2:5000", []string{"1.2.3.4", "203.0.113.7"}, "203.0.113.7"},
		{"malformed entry", caddy, "172.18.0.2:5000", []string{"203.0.113.7, nonsense"}, "172.18.0.2"},
		{"trusted with no header", caddy, "172.18.0.2:5000", nil, "172.18.0.2"},
		{"ipv6", nil, "[2001:db8::1]:5000", nil, "2001:db8::1"},
		{"ipv4-mapped ipv6", caddy, "[::ffff:172.18.0.2]:5000", []string{"203.0.113.7"}, "203.0.113.7"},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = tt.remote
		for _, v := range tt.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		var got netip.Addr
		RealIP(tt.trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = ClientIP(r.Context())
		})).ServeHTTP(httptest.NewRecorder(), r)
		if got.String() != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, got, tt.want)
		}
	}
}
