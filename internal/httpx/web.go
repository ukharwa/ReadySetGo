package httpx

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/ukharwa/readysetgo/internal/logging"
)

// wantsPage reports whether r should get an HTML page rather than JSON:
// everything outside /api/.
func wantsPage(r *http.Request) bool {
	return !strings.HasPrefix(r.URL.Path, "/api/")
}

// errorPage is deliberately standalone (no layout or template data), so it
// renders even when the rest of the app can't.
var errorPage = template.Must(template.New("error").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="icon" href="data:,">
<title>{{.Title}}</title>
<link rel="stylesheet" href="/static/app.css">
</head>
<body>
<main class="narrow">
<h1>{{.Title}}</h1>
{{with .Message}}<p>{{.}}</p>{{end}}
{{with .RequestID}}<p class="muted">Error ID: <code>{{.}}</code></p>{{end}}
<p><a href="/">Go to the home page</a></p>
</main>
</body>
</html>
`))

func writeErrorPage(w http.ResponseWriter, r *http.Request, status int, message string) {
	data := struct{ Title, Message, RequestID string }{Title: http.StatusText(status), Message: message}
	if status >= 500 {
		data.RequestID = logging.RequestID(r.Context())
		if message == "" {
			data.Message = "Something went wrong on our side. Please try again in a moment."
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = errorPage.Execute(w, data)
}
