// Package admin lets admins find and manage users (role, disabling,
// deleting, resending the verification email) and retry failed background
// jobs. Changes to accounts go through the accounts service, so they are
// checked, recorded and hooked exactly as the CLI's are.
//
// Admins can't change their own account here, so no one locks themselves
// out by accident; another admin, or the CLI, can.
package admin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/admin/store"
	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/db"
)

// PageSize is how many users Users returns at a time.
const PageSize = 50

// Errors returned by Service. Their messages are safe to show users.
var (
	ErrNotFound   = errors.New("not found")
	ErrOwnAccount = errors.New("you can't change your own account here; another admin can")
)

// Service implements the admin tasks.
type Service struct {
	db       *db.DB
	accounts *auth.Service
	logger   *slog.Logger
}

// NewService returns a Service.
func NewService(d *db.DB, accounts *auth.Service, logger *slog.Logger) *Service {
	return &Service{db: d, accounts: accounts, logger: logger}
}

// User is an account as admins see it.
type User struct {
	ID            uuid.UUID
	Email         string
	Name          string
	Role          string
	EmailVerified bool
	Disabled      bool
	CreatedAt     time.Time
}

// likeEscaper escapes LIKE's wildcards (and its escape character), so a
// search matches them literally.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Users returns page (counting from 1) of the users whose address or name
// contains search, by address, and whether there are more.
func (s *Service) Users(ctx context.Context, search string, page int) ([]User, bool, error) {
	page = min(max(page, 1), 1<<20) // the cap keeps the offset from overflowing
	pattern := "%" + likeEscaper.Replace(strings.ToLower(strings.TrimSpace(search))) + "%"
	rows, err := store.New(s.db.Read).SearchUsers(ctx, store.SearchUsersParams{
		Pattern: pattern, Skip: int64((page - 1) * PageSize), MaxRows: PageSize + 1,
	})
	if err != nil {
		return nil, false, err
	}
	more := len(rows) > PageSize
	rows = rows[:min(len(rows), PageSize)]
	users := make([]User, len(rows))
	for i, r := range rows {
		users[i] = toUser(store.GetUserRow(r))
	}
	return users, more, nil
}

func toUser(r store.GetUserRow) User {
	return User{
		ID: r.ID, Email: r.Email, Name: r.Name, Role: r.Role, EmailVerified: r.EmailVerifiedAt.Valid,
		Disabled: r.DisabledAt.Valid, CreatedAt: r.CreatedAt,
	}
}

// User returns the user with id, or ErrNotFound.
func (s *Service) User(ctx context.Context, id uuid.UUID) (User, error) {
	r, err := store.New(s.db.Read).GetUser(ctx, id)
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return User{}, ErrNotFound
	} else if err != nil {
		return User{}, err
	}
	return toUser(r), nil
}

// notSelf returns ErrOwnAccount if admin is trying to change their own
// account.
func notSelf(admin auth.User, id uuid.UUID) error {
	if admin.ID == id {
		return ErrOwnAccount
	}
	return nil
}

// accountErr turns the accounts service's "no such user" into ErrNotFound.
func accountErr(err error) error {
	if errors.Is(err, auth.ErrUserNotFound) {
		return ErrNotFound
	}
	return err
}

// SetRole gives the user with id a role, on admin's behalf.
func (s *Service) SetRole(ctx context.Context, admin auth.User, id uuid.UUID, role string) error {
	if err := notSelf(admin, id); err != nil {
		return err
	}
	return accountErr(s.accounts.SetRole(ctx, id, role))
}

// SetDisabled disables or re-enables the user with id, on admin's behalf.
func (s *Service) SetDisabled(ctx context.Context, admin auth.User, id uuid.UUID, disabled bool) error {
	if err := notSelf(admin, id); err != nil {
		return err
	}
	return accountErr(s.accounts.SetDisabled(ctx, id, disabled))
}

// Delete deletes the user with id and everything that belongs to them, on
// admin's behalf. Another feature can refuse with validate.Errors.
func (s *Service) Delete(ctx context.Context, admin auth.User, id uuid.UUID) error {
	if err := notSelf(admin, id); err != nil {
		return err
	}
	return accountErr(s.accounts.DeleteUser(ctx, id))
}

// ResendVerification emails the user with id a new verification link, if
// their address isn't verified yet.
func (s *Service) ResendVerification(ctx context.Context, id uuid.UUID) error {
	u, err := s.User(ctx, id)
	if err != nil {
		return err
	}
	return s.accounts.ResendVerification(ctx, auth.User{
		ID: u.ID, Email: u.Email, Role: u.Role, EmailVerified: u.EmailVerified, CreatedAt: u.CreatedAt,
	})
}

// Job is a background job that failed after all its attempts.
type Job struct {
	ID        uuid.UUID
	Kind      string
	Attempts  int64
	LastError string
	FailedAt  time.Time
	CreatedAt time.Time
}

// FailedJobs returns the 100 most recent failed jobs. Failed jobs are
// deleted after 30 days.
func (s *Service) FailedJobs(ctx context.Context) ([]Job, error) {
	rows, err := store.New(s.db.Read).ListFailedJobs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Job, len(rows))
	for i, r := range rows {
		out[i] = Job{
			ID: r.ID, Kind: r.Kind, Attempts: r.Attempts, LastError: r.LastError.String,
			FailedAt: r.FailedAt.Time, CreatedAt: r.CreatedAt,
		}
	}
	return out, nil
}

// RetryJob runs a failed job again, with a fresh set of attempts. It
// returns ErrNotFound if there is no such failed job.
func (s *Service) RetryJob(ctx context.Context, id uuid.UUID) error {
	n, err := store.New(s.db.Write).RetryJob(ctx, store.RetryJobParams{ID: id, RunAt: db.Now()})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	s.logger.InfoContext(ctx, "failed job retried", "job_id", id)
	return nil
}

// pathID parses the UUID in the path parameter name.
func pathID(r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	return id, err == nil
}
