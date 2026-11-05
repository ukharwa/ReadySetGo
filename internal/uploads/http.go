package uploads

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// uploadTime is how long an upload request may take: long enough for
// MaxSize over a slow connection.
const uploadTime = 5 * time.Minute

// Limit is the middleware for upload routes, a feature's included: a body
// limit of MaxSize plus room for the form around the file.
var Limit = httpx.MaxBytes(MaxSize + 64<<10)

// RequestContext gives an upload request the time it needs, past the
// server's usual read and write timeouts and the request's context deadline
// (which a slow upload would outlive). Call the returned cancel when done.
func RequestContext(w http.ResponseWriter, r *http.Request) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(uploadTime)
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(deadline) // unsupported only by test recorders
	_ = rc.SetWriteDeadline(deadline)
	return context.WithDeadline(context.WithoutCancel(r.Context()), deadline)
}

// FilePart returns the multipart form's "file" field, streamed rather than
// buffered, or validate.Errors if there isn't one. Pass it to Service.Save.
func FilePart(r *http.Request) (*multipart.Part, error) {
	noFile := validate.Errors{"file": "Choose a file to upload."}
	mr, err := r.MultipartReader()
	if err != nil {
		return nil, noFile
	}
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, noFile
		} else if err != nil {
			return nil, readErr(err)
		}
		if p.FormName() == "file" && p.FileName() != "" {
			return p, nil
		}
	}
}

// serveFile writes an upload's contents. Images may be shown inline;
// anything else is a download, and a sandboxing Content-Security-Policy
// stops a browser running the file even if it tries.
func (s *Service) serveFile(w http.ResponseWriter, r *http.Request, u Upload) {
	f, err := s.Open(r.Context(), u)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "open upload", "err", err)
		httpx.Error(w, r, http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()
	disposition := "attachment"
	if u.Inline() {
		disposition = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", u.ContentType)
	h.Set("Content-Length", strconv.FormatInt(u.Size, 10))
	h.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": u.Filename}))
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "private, no-store")
	if _, err := io.Copy(w, f); err != nil {
		s.logger.WarnContext(r.Context(), "send upload", "err", err)
	}
}
