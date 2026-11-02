// Package audit keeps the accounts' security events (logins, password
// resets, role changes and the rest) in the audit_events table, for good,
// and shows them to admins. The events are logged either way; this module
// makes them part of the app's own history.
//
// Deleting a user keeps their events and clears the references to them.
package audit

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/audit/store"
	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// PageSize is how many events List returns at a time.
const PageSize = 50

// Service keeps and lists security events.
type Service struct {
	db     *db.DB
	logger *slog.Logger
}

// NewService returns a Service that keeps every event accounts records.
func NewService(d *db.DB, accounts *auth.Service, logger *slog.Logger) *Service {
	s := &Service{db: d, logger: logger}
	accounts.OnEvent(s.keep)
	return s
}

// keep stores e in the transaction of the change it describes.
func (s *Service) keep(ctx context.Context, tx *sql.Tx, e auth.Event) error {
	return store.New(tx).InsertEvent(ctx, store.InsertEventParams{
		ID: db.NewID(), Event: e.Name, UserID: nullID(e.UserID), ActorID: nullID(e.ActorID),
		Detail: e.Detail, Ip: e.IP, RequestID: e.RequestID, CreatedAt: e.At,
	})
}

func nullID(id uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: id, Valid: id != uuid.Nil}
}

// Entry is a kept event. UserEmail and ActorEmail are the addresses those
// accounts have now, or empty if there is no such account (any more).
type Entry struct {
	ID         uuid.UUID
	Event      string
	UserID     uuid.NullUUID
	UserEmail  string
	ActorID    uuid.NullUUID
	ActorEmail string
	Detail     string
	IP         string
	RequestID  string
	At         time.Time
}

// List returns page (counting from 1) of the events, newest first, and
// whether there are more. If address isn't empty, it lists only the events
// about that account.
func (s *Service) List(ctx context.Context, address string, page int) ([]Entry, bool, error) {
	page = min(max(page, 1), 1<<20) // the cap keeps the offset from overflowing
	address, _ = validate.Email(address)
	rows, err := store.New(s.db.Read).ListEvents(ctx, store.ListEventsParams{
		Email: address, Skip: int64((page - 1) * PageSize), MaxRows: PageSize + 1,
	})
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > PageSize
	rows = rows[:min(len(rows), PageSize)]
	entries := make([]Entry, len(rows))
	for i, r := range rows {
		entries[i] = Entry{
			ID: r.ID, Event: r.Event, UserID: r.UserID, UserEmail: r.UserEmail,
			ActorID: r.ActorID, ActorEmail: r.ActorEmail, Detail: r.Detail,
			IP: r.Ip, RequestID: r.RequestID, At: r.CreatedAt,
		}
	}
	return entries, more, nil
}
