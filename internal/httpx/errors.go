package httpx

import (
	"fmt"
	"net/http"

	"github.com/ukharwa/kickstart/internal/logging"
)

// Error writes a minimal response for an error status. Server errors include
// the request ID, so a user can quote it and the matching log lines can be
// found. It never includes error details.
func Error(w http.ResponseWriter, r *http.Request, status int) {
	body := http.StatusText(status)
	if status >= 500 {
		body = fmt.Sprintf("%s\nError ID: %s", body, logging.RequestID(r.Context()))
	}
	writeText(w, status, body+"\n")
}
