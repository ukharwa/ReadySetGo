package workspaces

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/ukharwa/kickstart/internal/auth"
	"github.com/ukharwa/kickstart/internal/httpx"
)

type contextKey struct{}

// Require is middleware for routes with a {workspaceID} path segment. It
// lets only the workspace's members through and puts the workspace, with
// the member's role, in the context for From. Everyone else gets 404, so a
// workspace's existence isn't revealed to outsiders. Use it inside
// auth.RequireUser.
func (s *Service) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _ := auth.UserFrom(r.Context())
		id, err := uuid.Parse(r.PathValue("workspaceID"))
		if err != nil {
			httpx.Error(w, r, http.StatusNotFound)
			return
		}
		ws, err := s.Membership(r.Context(), id, user)
		switch {
		case errors.Is(err, ErrNotFound):
			httpx.Error(w, r, http.StatusNotFound)
			return
		case err != nil:
			s.logger.ErrorContext(r.Context(), "load workspace", "err", err)
			httpx.Error(w, r, http.StatusInternalServerError)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, ws)))
	})
}

// From returns the workspace Require loaded for this request. Scope every
// query for workspace-owned data by its ID.
func From(ctx context.Context) Workspace {
	ws, _ := ctx.Value(contextKey{}).(Workspace)
	return ws
}

// pathID parses a UUID path value, such as a member's {userID}.
func pathID(r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	return id, err == nil
}
