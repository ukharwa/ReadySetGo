// Package validate holds the small helpers every feature uses to check user
// input, and Errors, which carries messages per form field to HTML forms and
// JSON responses alike.
package validate

import (
	"maps"
	"net/mail"
	"slices"
	"strings"
	"unicode/utf8"
)

// Errors maps input fields to messages that are safe to show the user.
type Errors map[string]string

// Error lists the messages, sorted by field.
func (e Errors) Error() string {
	var parts []string
	for _, field := range slices.Sorted(maps.Keys(e)) {
		parts = append(parts, field+": "+e[field])
	}
	return "invalid input: " + strings.Join(parts, "; ")
}

// Email trims and lowercases an email address and reports whether it is a
// single, plain address.
func Email(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, err := mail.ParseAddress(s)
	return s, err == nil && a.Address == s && len(s) <= 254
}

// Length reports whether s has between lo and hi characters.
func Length(s string, lo, hi int) bool {
	n := utf8.RuneCountInString(s)
	return n >= lo && n <= hi
}
