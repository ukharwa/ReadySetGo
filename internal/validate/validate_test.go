package validate

import "testing"

func TestEmail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"  Ada@Example.COM ", "ada@example.com", true},
		{"ada@example.com", "ada@example.com", true},
		{"Ada <ada@example.com>", "", false},
		{"ada@example.com, eve@example.com", "", false},
		{"not-an-address", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := Email(tt.in)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("Email(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestErrors(t *testing.T) {
	t.Parallel()
	e := Errors{"password": "too short", "email": "required"}
	if got, want := e.Error(), "invalid input: email: required; password: too short"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
