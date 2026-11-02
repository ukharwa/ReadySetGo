package auth

import (
	"context"
	"fmt"

	"github.com/ukharwa/readysetgo/internal/auth/store"
	"github.com/ukharwa/readysetgo/internal/jobs"
)

// CleanupKind is the scheduled job that deletes expired sessions and links.
const CleanupKind = "auth.cleanup"

// Cleanup returns the handler for CleanupKind.
func (s *Service) Cleanup() jobs.Handler {
	return func(ctx context.Context, _ []byte) error {
		now := s.now()
		sessions, err := s.sessions.DeleteExpired(ctx, now)
		if err != nil {
			return fmt.Errorf("delete expired sessions: %w", err)
		}
		tokens, err := store.New(s.db.Write).DeleteExpiredUserTokens(ctx, now)
		if err != nil {
			return fmt.Errorf("delete expired tokens: %w", err)
		}
		s.logger.InfoContext(ctx, "expired sessions and links deleted", "sessions", sessions, "links", tokens)
		return nil
	}
}
