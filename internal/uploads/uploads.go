// Package uploads stores files that users upload and serves them back to
// their owner only.
//
// A file's type is decided from its contents, not its name or what the
// browser claims, and only the types in allowedTypes are accepted. Files are
// stored under random keys, never the user's filename, and served with
// headers that stop a browser running them: images inline, everything else
// as a download.
package uploads

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/ukharwa/kickstart/internal/auth"
	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/jobs"
	"github.com/ukharwa/kickstart/internal/uploads/store"
	"github.com/ukharwa/kickstart/internal/validate"
)

// MaxSize is the largest file Save accepts.
const MaxSize = 10 << 20 // 10 MiB

// allowedTypes are the content types Save accepts, as
// http.DetectContentType names them from a file's first bytes. Add to it as
// the app needs; never add a type a browser would run, such as text/html or
// image/svg+xml.
var allowedTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf"}

// DeleteFilesKind is the job that deletes stored files once their records
// are gone.
const DeleteFilesKind = "uploads.delete_files"

// ErrNotFound means there is no such upload, or it belongs to someone else.
var ErrNotFound = errors.New("no such file")

// Upload is a stored file's details.
type Upload struct {
	ID          uuid.UUID
	Filename    string // as the user named it, cleaned up
	ContentType string // detected from the contents
	Size        int64
	CreatedAt   time.Time
	key         string
}

// Inline reports whether browsers may show the file in a page (images)
// rather than only download it.
func (u Upload) Inline() bool { return strings.HasPrefix(u.ContentType, "image/") }

// SizeText is the size for people to read, such as "2.4 MB".
func (u Upload) SizeText() string {
	switch {
	case u.Size < 1<<10:
		return fmt.Sprintf("%d B", u.Size)
	case u.Size < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(u.Size)/(1<<10))
	default:
		return fmt.Sprintf("%.1f MB", float64(u.Size)/(1<<20))
	}
}

func toUpload(r store.Upload) Upload {
	return Upload{ID: r.ID, Filename: r.Filename, ContentType: r.ContentType, Size: r.Size, CreatedAt: r.CreatedAt, key: r.StorageKey}
}

// Service stores and serves uploads.
type Service struct {
	db      *db.DB
	storage Storage
	logger  *slog.Logger
}

// NewService returns a Service keeping files in storage. Deleting an account
// deletes its files.
func NewService(d *db.DB, storage Storage, accounts *auth.Service, logger *slog.Logger) *Service {
	s := &Service{db: d, storage: storage, logger: logger}
	accounts.OnUserDeleting(s.beforeUserDeleted)
	return s
}

// Save stores a file the user owner uploaded as filename, reading it from
// r. A file that is too big, empty, or not an allowed type returns
// validate.Errors for the field "file".
func (s *Service) Save(ctx context.Context, owner uuid.UUID, filename string, r io.Reader) (Upload, error) {
	head := make([]byte, 512) // all http.DetectContentType looks at
	n, err := io.ReadFull(r, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return Upload{}, readErr(err)
	}
	head = head[:n]
	contentType, _, _ := mime.ParseMediaType(http.DetectContentType(head))
	if n == 0 || !slices.Contains(allowedTypes, contentType) {
		return Upload{}, validate.Errors{"file": "Upload an image (PNG, JPEG, GIF or WebP) or a PDF."}
	}

	key := rand.Text()
	size, err := s.storage.Put(ctx, key, io.LimitReader(io.MultiReader(bytes.NewReader(head), r), MaxSize+1))
	if err == nil && size > MaxSize {
		err = tooBig()
	}
	if err != nil {
		s.deleteFile(ctx, key)
		return Upload{}, readErr(err)
	}
	row, err := store.New(s.db.Write).CreateUpload(ctx, store.CreateUploadParams{
		ID: db.NewID(), UserID: owner, StorageKey: key, Filename: cleanName(filename),
		ContentType: contentType, Size: size, CreatedAt: db.Now(),
	})
	if err != nil {
		s.deleteFile(ctx, key)
		return Upload{}, err
	}
	return toUpload(row), nil
}

func tooBig() error {
	return validate.Errors{"file": fmt.Sprintf("Upload a file of up to %d MB.", MaxSize>>20)}
}

// readErr turns a request body over its size limit into the same message
// as a file over MaxSize.
func readErr(err error) error {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return tooBig()
	}
	return err
}

// cleanName keeps the last part of a path, without control characters, at
// most 200 characters long.
func cleanName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if r := []rune(name); len(r) > 200 {
		name = string(r[:200])
	}
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	return name
}

// deleteFile removes a file stored by a Save that then failed.
func (s *Service) deleteFile(ctx context.Context, key string) {
	if err := s.storage.Delete(context.WithoutCancel(ctx), key); err != nil {
		s.logger.WarnContext(ctx, "delete file of failed upload", "err", err)
	}
}

// ForUser returns owner's uploads, newest first.
func (s *Service) ForUser(ctx context.Context, owner uuid.UUID) ([]Upload, error) {
	rows, err := store.New(s.db.Read).ListUserUploads(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]Upload, len(rows))
	for i, r := range rows {
		out[i] = toUpload(r)
	}
	return out, nil
}

// Get returns owner's upload with id, or ErrNotFound.
func (s *Service) Get(ctx context.Context, owner, id uuid.UUID) (Upload, error) {
	row, err := store.New(s.db.Read).GetUserUpload(ctx, store.GetUserUploadParams{ID: id, UserID: owner})
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return Upload{}, ErrNotFound
	}
	return toUpload(row), err
}

// Open returns an upload's contents.
func (s *Service) Open(ctx context.Context, u Upload) (io.ReadCloser, error) {
	return s.storage.Open(ctx, u.key)
}

// Delete deletes owner's upload with id, or returns ErrNotFound. The file
// itself is deleted by a job once the record is gone.
func (s *Service) Delete(ctx context.Context, owner, id uuid.UUID) error {
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		key, err := store.New(tx).DeleteUserUpload(ctx, store.DeleteUserUploadParams{ID: id, UserID: owner})
		if errors.Is(db.Err(err), db.ErrNotFound) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		return jobs.Enqueue(ctx, tx, DeleteFilesKind, deleteFiles{Keys: []string{key}})
	})
}

// beforeUserDeleted queues the deletion of a deleted account's files; the
// database deletes their records with the account.
func (s *Service) beforeUserDeleted(ctx context.Context, tx *sql.Tx, u auth.User) error {
	keys, err := store.New(tx).ListUserStorageKeys(ctx, u.ID)
	if err != nil || len(keys) == 0 {
		return err
	}
	return jobs.Enqueue(ctx, tx, DeleteFilesKind, deleteFiles{Keys: keys})
}

type deleteFiles struct {
	Keys []string `json:"keys"`
}

// DeleteFiles returns the handler for DeleteFilesKind.
func (s *Service) DeleteFiles() jobs.Handler {
	return func(ctx context.Context, payload []byte) error {
		var p deleteFiles
		if err := json.Unmarshal(payload, &p); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}
		for _, key := range p.Keys {
			if err := s.storage.Delete(ctx, key); err != nil {
				return fmt.Errorf("delete file: %w", err)
			}
		}
		return nil
	}
}
