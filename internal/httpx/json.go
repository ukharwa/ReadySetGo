package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// WriteJSON responds with v as JSON.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	writeJSON(w, status, "application/json", v)
}

func writeJSON(w http.ResponseWriter, status int, contentType string, v any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ReadJSON decodes the request body, a single JSON object, into dst. On
// failure it responds with a 400 or 413 problem itself and returns false.
// Unknown fields are rejected, so a client's typo doesn't pass silently.
func ReadJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil && dec.Decode(&struct{}{}) != io.EOF {
		err = errors.New("body must hold a single JSON object")
	}
	if err == nil {
		return true
	}
	if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
		writeProblem(w, r, http.StatusRequestEntityTooLarge, "The request body is too large.", nil)
		return false
	}
	writeProblem(w, r, http.StatusBadRequest, fmt.Sprintf("The request body isn't valid JSON for this endpoint: %v.", jsonProblem(err)), nil)
	return false
}

// jsonProblem describes a decoding error without echoing request data.
func jsonProblem(err error) string {
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syntax):
		return fmt.Sprintf("malformed JSON at byte %d", syntax.Offset)
	case errors.As(err, &typ):
		return fmt.Sprintf("field %q has the wrong type", typ.Field)
	case errors.Is(err, io.EOF):
		return "the body is empty"
	default:
		return err.Error() // unknown field, or more than one value
	}
}
