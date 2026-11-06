package httpx

import (
	"net/url"
	"path"
	"strings"
)

// SafeRedirect returns next if it is a path on this site, and fallback
// otherwise, so a ?next= parameter can't send users to another site.
// Parsing rejects control characters, which browsers drop from URLs:
// "/\t/evil.example" would otherwise reach them as "//evil.example".
func SafeRedirect(next, fallback string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, `\`) {
		return fallback
	}
	if u, err := url.Parse(next); err != nil || u.Host != "" || u.Scheme != "" || path.Clean(u.Path) == "" {
		return fallback
	}
	return next
}
