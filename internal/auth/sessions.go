package auth

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/kickstart/internal/auth/store"
	"github.com/ukharwa/kickstart/internal/db"
)

// Session is a logged-in browser or an API token. Only a hash of its token
// is kept.
type Session struct {
	TokenHash     string
	UserID        uuid.UUID
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
	Touch(ctx context.Context, tokenHash string, lastSeen, idleExpires time.Time) error
	Delete(ctx context.Context, tokenHash string) error
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
	return s.q().CreateSession(ctx, store.CreateSessionParams(sess))
}

// Get implements SessionStore.
func (s DBSessions) Get(ctx context.Context, tokenHash string) (Session, error) {
	row, err := s.q().GetSession(ctx, tokenHash)
	if err != nil {
		return Session{}, db.Err(err)
	}
	return Session(row), nil
}

// Touch implements SessionStore.
func (s DBSessions) Touch(ctx context.Context, tokenHash string, lastSeen, idleExpires time.Time) error {
	return s.q().TouchSession(ctx, store.TouchSessionParams{
		TokenHash: tokenHash, LastSeenAt: lastSeen, IdleExpiresAt: idleExpires,
	})
}

// Delete implements SessionStore.
func (s DBSessions) Delete(ctx context.Context, tokenHash string) error {
	return s.q().DeleteSession(ctx, tokenHash)
}

// DeleteForUser implements SessionStore.
func (s DBSessions) DeleteForUser(ctx context.Context, userID uuid.UUID, keep string) error {
	return s.q().DeleteUserSessions(ctx, store.DeleteUserSessionsParams{UserID: userID, Keep: keep})
}

// DeleteExpired implements SessionStore.
func (s DBSessions) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	return s.q().DeleteExpiredSessions(ctx, now)
}
