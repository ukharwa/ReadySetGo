package twofactor

import (
	"testing"
	"time"
)

// TestCode checks codes against RFC 6238's SHA-1 test vectors, which are 8
// digits long: a 6-digit code is their last 6.
func TestCode(t *testing.T) {
	t.Parallel()
	secret := []byte("12345678901234567890")
	tests := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	}
	for _, tt := range tests {
		if got := code(secret, step(time.Unix(tt.unix, 0))); got != tt.want {
			t.Errorf("code at %d = %s, want %s", tt.unix, got, tt.want)
		}
	}
}

func TestMatchStep(t *testing.T) {
	t.Parallel()
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111111, 0)
	n := step(now)
	tests := []struct {
		name string
		code string
		want int64
		ok   bool
	}{
		{"current", code(secret, n), n, true},
		{"previous", code(secret, n-1), n - 1, true},
		{"next", code(secret, n+1), n + 1, true},
		{"two steps old", code(secret, n-2), 0, false},
		{"wrong", "000000", 0, false},
	}
	for _, tt := range tests {
		if got, ok := matchStep(secret, tt.code, now); got != tt.want || ok != tt.ok {
			t.Errorf("%s: matchStep = %d, %t; want %d, %t", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestKeyURI(t *testing.T) {
	t.Parallel()
	got := keyURI("My App", "ada@example.com", "JBSWY3DPEHPK3PXP")
	want := "otpauth://totp/My%20App:ada@example.com?issuer=My+App&secret=JBSWY3DPEHPK3PXP"
	if got != want {
		t.Errorf("keyURI = %s, want %s", got, want)
	}
}
