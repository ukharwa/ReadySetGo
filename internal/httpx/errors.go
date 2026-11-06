package httpx

import (
	"net/http"
	"unicode"
	"unicode/utf8"

	"github.com/ukharwa/readysetgo/internal/logging"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// Error responds with a minimal page or JSON problem for an error status.
// Server errors include the request ID, so a user can quote it and the
// matching log lines can be found. It never includes error details.
func Error(w http.ResponseWriter, r *http.Request, status int) {
	ErrorMessage(w, r, status, "")
}

// ErrorMessage is Error with a message for the user. The message must be
// safe to show: never pass an error's text.
func ErrorMessage(w http.ResponseWriter, r *http.Request, status int, message string) {
	// readysetgo:begin ui=html
	if wantsPage(r) {
		writeErrorPage(w, r, status, message)
		return
	}
	// readysetgo:end
	writeProblem(w, r, status, message, nil)
}

// Sentence turns an error whose message is safe to show, such as a
// service's named error, into a sentence: "you can't do that here" becomes
// "You can't do that here.".
func Sentence(err error) string {
	s := err.Error()
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:] + "."
}

// Problem is a JSON error response in the RFC 9457 format
// (application/problem+json), with the request ID and any field errors as
// extensions.
type Problem struct {
	Title     string          `json:"title"`
	Status    int             `json:"status"`
	Detail    string          `json:"detail,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	Errors    validate.Errors `json:"errors,omitempty"`
}

// WriteInvalid responds 422 with the field errors errs.
func WriteInvalid(w http.ResponseWriter, r *http.Request, errs validate.Errors) {
	writeProblem(w, r, http.StatusUnprocessableEntity, "Some fields are invalid.", errs)
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, detail string, errs validate.Errors) {
	p := Problem{Title: http.StatusText(status), Status: status, Detail: detail, Errors: errs}
	if status >= 500 {
		p.RequestID = logging.RequestID(r.Context())
	}
	writeJSON(w, status, "application/problem+json", p)
}
