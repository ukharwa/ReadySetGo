package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTimeZone(t *testing.T) {
	t.Parallel()
	for name, ok := range map[string]bool{
		"Africa/Johannesburg": true, "UTC": true, "Asia/Kolkata": true,
		"": false, "Local": false, "Mars/Olympus_Mons": false, "../../etc/passwd": false,
	} {
		if got := timeZone(name) != nil; got != ok {
			t.Errorf("timeZone(%q) found = %v, want %v", name, got, ok)
		}
	}
	if loc := (User{TimeZone: "nowhere"}).Location(); loc != time.UTC {
		t.Errorf("an unknown zone's Location = %v, want UTC", loc)
	}
}

func TestCleanName(t *testing.T) {
	t.Parallel()
	if got, ok := cleanName("  Ada \t King  "); got != "Ada King" || !ok {
		t.Errorf("cleanName = %q, %v", got, ok)
	}
	if _, ok := cleanName(strings.Repeat("x", 101)); ok {
		t.Error("a 101-character name was accepted")
	}
}
