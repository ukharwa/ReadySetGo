package auth

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth/store"
	"github.com/ukharwa/readysetgo/internal/db"
)

// Session is a logged-in browser or an API token. Only a hash of its token
// is kept.
type Session struct {
	TokenHash     string
	ID            uuid.UUID // names it in the device list, which never shows the hash
	UserID        uuid.UUID
	UserAgent     string // of the browser or app that started it
	IP            string // the address it was last seen from
	CreatedAt     time.Time
	LastSeenAt    time.Time
	IdleExpiresAt time.Time // moves forward while the session is used
	ExpiresAt     time.Time // fixed: the session's maximum age
}

// SessionStore keeps sessions. It is a seam: DBSessions, the default, uses
// the application's database; SCALING.md covers moving sessions to a shared
// cache if their lookups ever become a load problem.
type SessionStore interface {
	Create(ctx context.Context, s Session) error
	// Get returns the session with tokenHash, or an error wrapping
	// db.ErrNotFound.
	Get(ctx context.Context, tokenHash string) (Session, error)
	// List returns a user's sessions that haven't expired by now, most
	// recently used first.
	List(ctx context.Context, userID uuid.UUID, now time.Time) ([]Session, error)
	Touch(ctx context.Context, tokenHash string, lastSeen, idleExpires time.Time, ip string) error
	Delete(ctx context.Context, tokenHash string) error
	// DeleteByID deletes one of a user's sessions, reporting whether there
	// was one.
	DeleteByID(ctx context.Context, userID, id uuid.UUID) (bool, error)
	// DeleteForUser deletes all of a user's sessions except keep's (pass ""
	// to delete them all).
	DeleteForUser(ctx context.Context, userID uuid.UUID, keep string) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// DBSessions is a SessionStore in the application's database. It uses the
// write handle even for reads, so a session is found straight after it is
// created when reads go to a lagging replica.
type DBSessions struct {
	DB *db.DB
}

func (s DBSessions) q() *store.Queries { return store.New(s.DB.Write) }

// Create implements SessionStore.
func (s DBSessions) Create(ctx context.Context, sess Session) error {
	return s.q().CreateSession(ctx, store.CreateSessionParams{
		TokenHash: sess.TokenHash, ID: sess.ID, UserID: sess.UserID, UserAgent: sess.UserAgent, Ip: sess.IP,
		CreatedAt: sess.CreatedAt, LastSeenAt: sess.LastSeenAt, IdleExpiresAt: sess.IdleExpiresAt, ExpiresAt: sess.ExpiresAt,
	})
}

func toSession(r store.Session) Session {
	return Session{
		TokenHash: r.TokenHash, ID: r.ID, UserID: r.UserID, UserAgent: r.UserAgent, IP: r.Ip,
		CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt, IdleExpiresAt: r.IdleExpiresAt, ExpiresAt: r.ExpiresAt,
	}
}

// Get implements SessionStore.
func (s DBSessions) Get(ctx context.Context, tokenHash string) (Session, error) {
	row, err := s.q().GetSession(ctx, tokenHash)
	if err != nil {
		return Session{}, db.Err(err)
	}
	return toSession(row), nil
}

// List implements SessionStore.
func (s DBSessions) List(ctx context.Context, userID uuid.UUID, now time.Time) ([]Session, error) {
	rows, err := s.q().ListUserSessions(ctx, store.ListUserSessionsParams{UserID: userID, Now: now})
	if err != nil {
		return nil, err
	}
	sessions := make([]Session, len(rows))
	for i, r := range rows {
		sessions[i] = toSession(r)
	}
	return sessions, nil
}

// Touch implements SessionStore.
func (s DBSessions) Touch(ctx context.Context, tokenHash string, lastSeen, idleExpires time.Time, ip string) error {
	return s.q().TouchSession(ctx, store.TouchSessionParams{
		TokenHash: tokenHash, LastSeenAt: lastSeen, IdleExpiresAt: idleExpires, Ip: ip,
	})
}

// Delete implements SessionStore.
func (s DBSessions) Delete(ctx context.Context, tokenHash string) error {
	return s.q().DeleteSession(ctx, tokenHash)
}

// DeleteByID implements SessionStore.
func (s DBSessions) DeleteByID(ctx context.Context, userID, id uuid.UUID) (bool, error) {
	n, err := s.q().DeleteUserSession(ctx, store.DeleteUserSessionParams{UserID: userID, ID: id})
	return n > 0, err
}

// DeleteForUser implements SessionStore.
func (s DBSessions) DeleteForUser(ctx context.Context, userID uuid.UUID, keep string) error {
	return s.q().DeleteUserSessions(ctx, store.DeleteUserSessionsParams{UserID: userID, Keep: keep})
}

// DeleteExpired implements SessionStore.
func (s DBSessions) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	return s.q().DeleteExpiredSessions(ctx, now)
}
