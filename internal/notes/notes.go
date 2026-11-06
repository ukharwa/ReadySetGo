// Package notes is the example feature: notes that belong to the user who
// wrote them, created, read, updated and deleted through pages or a JSON
// API. It shows the whole pattern for a feature, from migration and queries
// to service, handlers and tests; copy its shape for a new one.
//
// `make remove-example` deletes it once it has served its purpose.
package notes

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/notes/store"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// ErrNotFound means there is no such note, or it belongs to someone else:
// the two are indistinguishable on purpose.
var ErrNotFound = errors.New("no such note")

// Note is a note, as handlers see it.
type Note struct {
	ID        uuid.UUID
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func toNote(r store.Note) Note {
	return Note{ID: r.ID, Title: r.Title, Body: r.Body, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

// Service implements the notes feature. Every method takes the owner's
// user ID, and the queries filter on it.
type Service struct {
	db     *db.DB
	logger *slog.Logger
}

// NewService returns a Service.
func NewService(d *db.DB, logger *slog.Logger) *Service {
	return &Service{db: d, logger: logger}
}

// check cleans up a note's fields and returns validate.Errors if they're
// unacceptable.
func check(title, body string) (string, string, error) {
	title = strings.TrimSpace(title)
	errs := validate.Errors{}
	if !validate.Length(title, 1, 200) {
		errs["title"] = "Enter a title of up to 200 characters."
	}
	if !validate.Length(body, 0, 10_000) {
		errs["body"] = "Keep the note to 10,000 characters."
	}
	if len(errs) > 0 {
		return "", "", errs
	}
	return title, body, nil
}

// Create adds a note for owner.
func (s *Service) Create(ctx context.Context, owner uuid.UUID, title, body string) (Note, error) {
	title, body, err := check(title, body)
	if err != nil {
		return Note{}, err
	}
	now := db.Now()
	row, err := store.New(s.db.Write).CreateNote(ctx, store.CreateNoteParams{
		ID: db.NewID(), UserID: owner, Title: title, Body: body, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Note{}, err
	}
	return toNote(row), nil
}

// List returns owner's notes, most recently changed first.
func (s *Service) List(ctx context.Context, owner uuid.UUID) ([]Note, error) {
	rows, err := store.New(s.db.Read).ListNotes(ctx, owner)
	if err != nil {
		return nil, err
	}
	notes := make([]Note, len(rows))
	for i, r := range rows {
		notes[i] = toNote(r)
	}
	return notes, nil
}

// Get returns owner's note with id, or ErrNotFound.
func (s *Service) Get(ctx context.Context, owner, id uuid.UUID) (Note, error) {
	row, err := store.New(s.db.Read).GetNote(ctx, store.GetNoteParams{ID: id, UserID: owner})
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return Note{}, ErrNotFound
	} else if err != nil {
		return Note{}, err
	}
	return toNote(row), nil
}

// Update replaces the title and body of owner's note with id.
func (s *Service) Update(ctx context.Context, owner, id uuid.UUID, title, body string) (Note, error) {
	title, body, err := check(title, body)
	if err != nil {
		return Note{}, err
	}
	row, err := store.New(s.db.Write).UpdateNote(ctx, store.UpdateNoteParams{
		ID: id, UserID: owner, Title: title, Body: body, UpdatedAt: db.Now(),
	})
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return Note{}, ErrNotFound
	} else if err != nil {
		return Note{}, err
	}
	return toNote(row), nil
}

// Delete deletes owner's note with id.
func (s *Service) Delete(ctx context.Context, owner, id uuid.UUID) error {
	n, err := store.New(s.db.Write).DeleteNote(ctx, store.DeleteNoteParams{ID: id, UserID: owner})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
