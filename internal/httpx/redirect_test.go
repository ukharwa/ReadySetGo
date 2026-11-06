package httpx

import "testing"

func TestSafeRedirect(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"/account?tab=email":   "/account?tab=email",
		"":                     "/",
		"account":              "/",
		"https://evil.example": "/",
		"//evil.example":       "/",
		`/\evil.example`:       "/",
		"/\t/evil.example":     "/", // browsers drop tabs and newlines
		"/\n/evil.example":     "/",
		"/\r/evil.example":     "/",
	}
	for next, want := range tests {
		if got := SafeRedirect(next, "/"); got != want {
			t.Errorf("SafeRedirect(%q) = %q, want %q", next, got, want)
		}
	}
}
