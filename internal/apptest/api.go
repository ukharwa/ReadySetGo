package apptest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// Response is a JSON response, with its body read.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Decode unmarshals the body into v, failing the test on error.
func (a *App) Decode(r Response, v any) {
	a.t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		a.t.Fatalf("decode %s: %v", r.Body, err)
	}
}

// JSON sends body as JSON to path with method, using c's cookies and, if
// token isn't empty, a bearer token.
func (a *App) JSON(c *http.Client, method, path, token string, body any) Response {
	a.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(a.t.Context(), method, a.URL(path), reader)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		a.t.Fatal(err)
	}
	return Response{Status: resp.StatusCode, Header: resp.Header, Body: b}
}
