// Package auth handles accounts: registration, login and sessions, email
// verification, password reset, email and password changes, and account
// deletion.
//
// Service holds the logic and knows nothing about HTTP. The HTML or JSON
// handlers on top of it, and the CLI, call it.
//
// Two rules run through it: responses never reveal whether an email address
// has an account, and everything sent by email (tokens) is stored only as a
// hash, used once, and expires.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth/store"
	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/email"
	"github.com/ukharwa/readysetgo/internal/httpx"
	"github.com/ukharwa/readysetgo/internal/logging"
	"github.com/ukharwa/readysetgo/internal/ratelimit"
	"github.com/ukharwa/readysetgo/internal/validate"
)

//go:embed emails/*.txt
var emailFS embed.FS

// Roles a user can have.
const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// Errors returned by Service. Their messages are safe to show users.
var (
	ErrInvalidCredentials = errors.New("email or password is incorrect")
	ErrInvalidToken       = errors.New("this link is invalid or has expired")
	ErrUnauthenticated    = errors.New("not logged in")
	ErrUserNotFound       = errors.New("no user with that email address")
)

// RateLimitedError reports too many attempts.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("too many attempts; try again in %s", e.RetryAfter.Round(time.Minute))
}

// User is an account, as other packages see it.
type User struct {
	ID            uuid.UUID
	Email         string
	Role          string
	EmailVerified bool
	CreatedAt     time.Time
}

func toUser(u store.User) User {
	return User{ID: u.ID, Email: u.Email, Role: u.Role, EmailVerified: u.EmailVerifiedAt.Valid, CreatedAt: u.CreatedAt}
}

// Lifetime is how long a session lasts: it ends after Idle without use, and
// after Max regardless.
type Lifetime struct {
	Idle, Max time.Duration
}

// Session lifetimes. A browser session's cookie ends when the browser
// closes; "remember me" keeps it for 30 days.
var (
	BrowserSession    = Lifetime{Idle: 24 * time.Hour, Max: 7 * 24 * time.Hour}
	RememberedSession = Lifetime{Idle: 30 * 24 * time.Hour, Max: 30 * 24 * time.Hour}
	// readysetgo:begin ui=api
	APIToken = Lifetime{Idle: 30 * 24 * time.Hour, Max: 90 * 24 * time.Hour}
	// readysetgo:end
)

// How long emailed links work.
const (
	verifyTokenTTL      = 24 * time.Hour
	resetTokenTTL       = time.Hour
	changeEmailTokenTTL = 24 * time.Hour
)

// touchInterval limits how often a session's "last seen" time is written, so
// reading pages doesn't mean a database write per request.
const touchInterval = 5 * time.Minute

// Options configures a Service.
type Options struct {
	AppName           string
	BaseURL           string // links in emails start with this, e.g. https://example.com
	MinPasswordLength int
}

// Service implements the account flows.
type Service struct {
	db        *db.DB
	sessions  SessionStore
	limiter   ratelimit.Limiter
	logger    *slog.Logger
	opts      Options
	hasher    *hasher
	emails    *email.Templates
	dummyHash string           // compared against when an email is unknown, so timing matches
	now       func() time.Time // replaced in tests
	created   []Hook
	deleting  []Hook
	events    []EventHook
}

// Hook lets another feature act when an account is created or deleted,
// inside the same transaction, so its changes commit or roll back with the
// account. A deletion hook can refuse by returning validate.Errors, whose
// messages the user sees. Add hooks while setting up, before serving.
type Hook func(ctx context.Context, tx *sql.Tx, u User) error

// OnUserCreated adds a hook that runs when an account is created.
func (s *Service) OnUserCreated(h Hook) { s.created = append(s.created, h) }

// OnUserDeleting adds a hook that runs just before an account is deleted.
func (s *Service) OnUserDeleting(h Hook) { s.deleting = append(s.deleting, h) }

func runHooks(ctx context.Context, tx *sql.Tx, hooks []Hook, u User) error {
	for _, h := range hooks {
		if err := h(ctx, tx, u); err != nil {
			return err
		}
	}
	return nil
}

// Names of the security events Service records.
const (
	EventUserCreated            = "user.created"
	EventUserDeleted            = "user.deleted"
	EventRoleChanged            = "user.role_changed"
	EventUserDisabled           = "user.disabled"
	EventUserEnabled            = "user.enabled"
	EventLogin                  = "auth.login"
	EventLoginFailed            = "auth.login_failed"
	EventPasswordResetRequested = "auth.password_reset_requested"
	EventPasswordReset          = "auth.password_reset"
	EventPasswordChanged        = "auth.password_changed"
	EventEmailChanged           = "auth.email_changed"
)

// Event is a security-relevant change to an account, or attempt at one.
// Every event is logged; OnEvent hooks can also keep them.
type Event struct {
	Name      string    // one of the Event… constants
	UserID    uuid.UUID // the account; uuid.Nil if none matched
	ActorID   uuid.UUID // the logged-in user who acted; uuid.Nil for a visitor or the CLI
	Detail    string    // such as the new role; never an address, password or token
	IP        string    // the client's address, if the event came from a request
	RequestID string
	At        time.Time
}

// EventHook receives every event, inside the transaction of the change it
// describes (or a transaction of its own), so what it writes commits with
// the change. An error rolls the change back.
type EventHook func(ctx context.Context, tx *sql.Tx, e Event) error

// OnEvent adds a hook that runs for every security event.
func (s *Service) OnEvent(h EventHook) { s.events = append(s.events, h) }

// record logs an event and passes it to the event hooks, in tx or, if tx
// is nil, in a transaction of its own.
func (s *Service) record(ctx context.Context, tx *sql.Tx, e Event) error {
	e.At, e.RequestID = s.now(), logging.RequestID(ctx)
	if u, ok := UserFrom(ctx); ok {
		e.ActorID = u.ID
	}
	if ip := httpx.ClientIP(ctx); e.IP == "" && ip.IsValid() {
		e.IP = ip.String()
	}
	attrs := []any{"event", e.Name} // and whichever of the rest are known
	if e.UserID != uuid.Nil {
		attrs = append(attrs, "user_id", e.UserID)
	}
	if e.ActorID != uuid.Nil {
		attrs = append(attrs, "actor_id", e.ActorID)
	}
	if e.Detail != "" {
		attrs = append(attrs, "detail", e.Detail)
	}
	if e.IP != "" {
		attrs = append(attrs, "ip", e.IP)
	}
	s.logger.InfoContext(ctx, "security event", attrs...)
	run := func(tx *sql.Tx) error {
		for _, h := range s.events {
			if err := h(ctx, tx, e); err != nil {
				return fmt.Errorf("event hook: %w", err)
			}
		}
		return nil
	}
	switch {
	case len(s.events) == 0:
		return nil
	case tx != nil:
		return run(tx)
	default:
		return s.db.InTx(ctx, run)
	}
}

// NewService returns a Service. It hashes one dummy password, so it takes a
// moment.
func NewService(d *db.DB, sessions SessionStore, limiter ratelimit.Limiter, logger *slog.Logger, opts Options) (*Service, error) {
	emails, err := email.ParseTemplates(emailFS, "emails/*.txt")
	if err != nil {
		return nil, err
	}
	s := &Service{
		db: d, sessions: sessions, limiter: limiter, logger: logger, opts: opts,
		hasher: newHasher(), emails: emails, now: db.Now,
	}
	if s.dummyHash, err = s.hasher.Hash(context.Background(), rand.Text()); err != nil {
		return nil, err
	}
	return s, nil
}

// newToken returns a random token for a link or session, and the hash that
// is stored instead of it. Tokens are looked up by hash; with 128 random
// bits behind each one, the lookup's timing reveals nothing usable.
func newToken() (token, hash string) {
	token = rand.Text()
	return token, hashToken(token)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// checkPassword returns the problems with a new password, keyed by field.
func (s *Service) checkPassword(field, password string) validate.Errors {
	if !validate.Length(password, s.opts.MinPasswordLength, 256) {
		return validate.Errors{field: fmt.Sprintf("Use between %d and 256 characters.", s.opts.MinPasswordLength)}
	}
	return nil
}

// allow applies a rate limit, returning *RateLimitedError when it is hit.
func (s *Service) allow(ctx context.Context, key string, limit int, window time.Duration) error {
	ok, retryAfter, err := s.limiter.Allow(ctx, key, limit, window)
	if err != nil {
		return fmt.Errorf("rate limit: %w", err)
	}
	if !ok {
		return &RateLimitedError{RetryAfter: retryAfter}
	}
	return nil
}

// sendEmail renders and queues an email in tx. Each address gets at most 3
// emails an hour, so the app can't be used to flood an inbox; extra ones are
// dropped quietly, because saying so would reveal whether an account exists.
func (s *Service) sendEmail(ctx context.Context, tx *sql.Tx, template, to string, data map[string]any) error {
	err := s.allow(ctx, "email:"+to, 3, time.Hour)
	if _, limited := errors.AsType[*RateLimitedError](err); limited {
		s.logger.InfoContext(ctx, "email dropped: too many to one address", "template", template)
		return nil
	} else if err != nil {
		return err
	}
	data["AppName"] = s.opts.AppName
	data["BaseURL"] = s.opts.BaseURL
	m, err := s.emails.Message(template, to, data)
	if err != nil {
		return err
	}
	return email.Enqueue(ctx, tx, m)
}

// link returns an absolute URL for path with a token query parameter.
func (s *Service) link(path, token string) string {
	return s.opts.BaseURL + path + "?" + url.Values{"token": {token}}.Encode()
}

// createToken stores a new emailed token and returns it.
func createToken(ctx context.Context, q *store.Queries, userID uuid.UUID, purpose, address string, ttl time.Duration, now time.Time) (string, error) {
	token, hash := newToken()
	err := q.CreateUserToken(ctx, store.CreateUserTokenParams{
		TokenHash: hash, UserID: userID, Purpose: purpose, Email: address,
		CreatedAt: now, ExpiresAt: now.Add(ttl),
	})
	return token, err
}

// takeToken uses up an emailed token, returning ErrInvalidToken if it is
// unknown or expired.
func (s *Service) takeToken(ctx context.Context, q *store.Queries, token, purpose string) (store.UserToken, error) {
	t, err := q.TakeUserToken(ctx, store.TakeUserTokenParams{TokenHash: hashToken(token), Purpose: purpose})
	if errors.Is(db.Err(err), db.ErrNotFound) || (err == nil && !s.now().Before(t.ExpiresAt)) {
		return store.UserToken{}, ErrInvalidToken
	}
	return t, err
}

// Register creates an account and emails a verification link. If the
// address already has an account, the owner gets an email about the attempt
// instead, and Register returns exactly as if it had created one: the form
// never reveals who has an account. Invalid input returns validate.Errors.
func (s *Service) Register(ctx context.Context, address, password string) error {
	address, ok := validate.Email(address)
	errs := validate.Errors{}
	if !ok {
		errs["email"] = "Enter a valid email address."
	}
	for k, v := range s.checkPassword("password", password) {
		errs[k] = v
	}
	if len(errs) > 0 {
		return errs
	}

	// Hash even when the address is taken, so both paths take as long.
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		if _, err := q.GetUserByEmail(ctx, address); err == nil {
			return s.sendEmail(ctx, tx, "account_exists.txt", address, map[string]any{
				"LoginLink": s.opts.BaseURL + "/login", "ResetLink": s.opts.BaseURL + "/forgot-password",
			})
		} else if !errors.Is(db.Err(err), db.ErrNotFound) {
			return err
		}

		now := s.now()
		user, err := q.CreateUser(ctx, store.CreateUserParams{
			ID: db.NewID(), Email: address, PasswordHash: hash, Role: RoleUser, CreatedAt: now, UpdatedAt: now,
		})
		if errors.Is(db.Err(err), db.ErrConflict) {
			return nil // registered a moment ago by a concurrent request
		} else if err != nil {
			return err
		}
		if err := runHooks(ctx, tx, s.created, toUser(user)); err != nil {
			return err
		}
		if err := s.record(ctx, tx, Event{Name: EventUserCreated, UserID: user.ID}); err != nil {
			return err
		}
		token, err := createToken(ctx, q, user.ID, "verify_email", address, verifyTokenTTL, now)
		if err != nil {
			return err
		}
		return s.sendEmail(ctx, tx, "verify_email.txt", address, map[string]any{"Link": s.link("/verify-email", token)})
	})
}

// Login checks an email and password and starts a session with lifetime life,
// returning its token. Wrong details return ErrInvalidCredentials, whether or
// not the account exists; too many attempts return *RateLimitedError.
//
// Attempts are limited per account and IP address (5 per 15 minutes) and per
// account (50 an hour). Both counts reset after a successful login, so only
// consecutive failures count, and a stranger elsewhere can't lock anyone
// out.
func (s *Service) Login(ctx context.Context, address, password, ip string, life Lifetime) (string, error) {
	address, _ = validate.Email(address)
	keys := []string{"login:account-ip:" + address + "|" + ip, "login:account:" + address}
	if err := s.allow(ctx, keys[0], 5, 15*time.Minute); err != nil {
		return "", err
	}
	if err := s.allow(ctx, keys[1], 50, time.Hour); err != nil {
		return "", err
	}

	u, err := store.New(s.db.Write).GetUserByEmail(ctx, address)
	if errors.Is(db.Err(err), db.ErrNotFound) {
		_, _, _ = s.hasher.Verify(ctx, s.dummyHash, password)
		return "", s.loginFailed(ctx, uuid.Nil, ip, "unknown address")
	} else if err != nil {
		return "", err
	}
	ok, rehash, err := s.hasher.Verify(ctx, u.PasswordHash, password)
	switch {
	case err != nil:
		return "", err
	case !ok:
		return "", s.loginFailed(ctx, u.ID, ip, "wrong password")
	case u.DisabledAt.Valid:
		return "", s.loginFailed(ctx, u.ID, ip, "account disabled")
	}

	for _, k := range keys {
		if err := s.limiter.Reset(ctx, k); err != nil {
			return "", err
		}
	}
	if rehash {
		if err := s.setPassword(ctx, store.New(s.db.Write), u.ID, password); err != nil {
			s.logger.WarnContext(ctx, "re-hash password", "err", err)
		}
	}
	token, err := s.startSession(ctx, u.ID, life)
	if err != nil {
		return "", err
	}
	return token, s.record(ctx, nil, Event{Name: EventLogin, UserID: u.ID, IP: ip})
}

// loginFailed records a failed login and returns ErrInvalidCredentials.
func (s *Service) loginFailed(ctx context.Context, userID uuid.UUID, ip, why string) error {
	if err := s.record(ctx, nil, Event{Name: EventLoginFailed, UserID: userID, IP: ip, Detail: why}); err != nil {
		return err
	}
	return ErrInvalidCredentials
}

func (s *Service) startSession(ctx context.Context, userID uuid.UUID, life Lifetime) (string, error) {
	token, hash := newToken()
	now := s.now()
	err := s.sessions.Create(ctx, Session{
		TokenHash: hash, UserID: userID, CreatedAt: now, LastSeenAt: now,
		IdleExpiresAt: now.Add(life.Idle), ExpiresAt: now.Add(life.Max),
	})
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// Authenticate returns the user a session token belongs to, or
// ErrUnauthenticated if the session is unknown, has expired, or belongs to a
// disabled account.
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrUnauthenticated
	}
	sess, err := s.sessions.Get(ctx, hashToken(token))
	if errors.Is(err, db.ErrNotFound) {
		return User{}, ErrUnauthenticated
	} else if err != nil {
		return User{}, err
	}
	now := s.now()
	if !now.Before(sess.ExpiresAt) || !now.Before(sess.IdleExpiresAt) {
		return User{}, ErrUnauthenticated
	}
	if now.Sub(sess.LastSeenAt) >= touchInterval {
		idleExpires := now.Add(sess.IdleExpiresAt.Sub(sess.LastSeenAt))
		if idleExpires.After(sess.ExpiresAt) {
			idleExpires = sess.ExpiresAt
		}
		if err := s.sessions.Touch(ctx, sess.TokenHash, now, idleExpires); err != nil {
			s.logger.WarnContext(ctx, "touch session", "err", err)
		}
	}

	u, err := store.New(s.db.Write).GetUserByID(ctx, sess.UserID)
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return User{}, ErrUnauthenticated
	} else if err != nil {
		return User{}, err
	}
	if u.DisabledAt.Valid {
		return User{}, ErrUnauthenticated
	}
	return toUser(u), nil
}

// Logout ends the session with token.
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.sessions.Delete(ctx, hashToken(token))
}

// VerifyEmail marks the address a verification token was sent to as
// verified.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		t, err := s.takeToken(ctx, q, token, "verify_email")
		if err != nil {
			return err
		}
		if _, err := q.MarkEmailVerified(ctx, store.MarkEmailVerifiedParams{
			ID: t.UserID, Email: t.Email, VerifiedAt: sql.NullTime{Time: s.now(), Valid: true},
		}); err != nil {
			return err
		}
		return nil
	})
}

// ResendVerification emails u a new verification link, unless u is already
// verified.
func (s *Service) ResendVerification(ctx context.Context, u User) error {
	if u.EmailVerified {
		return nil
	}
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		if err := q.DeleteUserTokens(ctx, store.DeleteUserTokensParams{UserID: u.ID, Purpose: "verify_email"}); err != nil {
			return err
		}
		token, err := createToken(ctx, q, u.ID, "verify_email", u.Email, verifyTokenTTL, s.now())
		if err != nil {
			return err
		}
		return s.sendEmail(ctx, tx, "verify_email.txt", u.Email, map[string]any{"Link": s.link("/verify-email", token)})
	})
}

// RequestPasswordReset emails a reset link if the address has an account.
// It returns the same either way.
func (s *Service) RequestPasswordReset(ctx context.Context, address string) error {
	address, ok := validate.Email(address)
	if !ok {
		return validate.Errors{"email": "Enter a valid email address."}
	}
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		u, err := q.GetUserByEmail(ctx, address)
		if errors.Is(db.Err(err), db.ErrNotFound) || (err == nil && u.DisabledAt.Valid) {
			return nil
		} else if err != nil {
			return err
		}
		if err := q.DeleteUserTokens(ctx, store.DeleteUserTokensParams{UserID: u.ID, Purpose: "reset_password"}); err != nil {
			return err
		}
		token, err := createToken(ctx, q, u.ID, "reset_password", address, resetTokenTTL, s.now())
		if err != nil {
			return err
		}
		if err := s.record(ctx, tx, Event{Name: EventPasswordResetRequested, UserID: u.ID}); err != nil {
			return err
		}
		return s.sendEmail(ctx, tx, "reset_password.txt", address, map[string]any{"Link": s.link("/reset-password", token)})
	})
}

// ResetPassword sets a new password using a reset token and logs out every
// session. Opening the emailed link also proves the address, so it is marked
// verified.
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if errs := s.checkPassword("password", password); errs != nil {
		return errs // checked first, so a typo doesn't use up the link
	}
	var userID uuid.UUID
	err := s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		t, err := s.takeToken(ctx, q, token, "reset_password")
		if err != nil {
			return err
		}
		userID = t.UserID
		if err := s.setPassword(ctx, q, t.UserID, password); err != nil {
			return err
		}
		if _, err := q.MarkEmailVerified(ctx, store.MarkEmailVerifiedParams{
			ID: t.UserID, Email: t.Email, VerifiedAt: sql.NullTime{Time: s.now(), Valid: true},
		}); err != nil {
			return err
		}
		if err := q.DeleteUserTokens(ctx, store.DeleteUserTokensParams{UserID: t.UserID, Purpose: "reset_password"}); err != nil {
			return err
		}
		return s.record(ctx, tx, Event{Name: EventPasswordReset, UserID: t.UserID})
	})
	if err != nil {
		return err
	}
	return s.sessions.DeleteForUser(ctx, userID, "")
}

func (s *Service) setPassword(ctx context.Context, q *store.Queries, userID uuid.UUID, password string) error {
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	return q.UpdatePasswordHash(ctx, store.UpdatePasswordHashParams{ID: userID, PasswordHash: hash, UpdatedAt: s.now()})
}

// checkCurrentPassword returns validate.Errors for field if password isn't
// u's.
func (s *Service) checkCurrentPassword(ctx context.Context, u User, field, password string) error {
	row, err := store.New(s.db.Write).GetUserByID(ctx, u.ID)
	if err != nil {
		return db.Err(err)
	}
	ok, _, err := s.hasher.Verify(ctx, row.PasswordHash, password)
	if err != nil {
		return err
	}
	if !ok {
		return validate.Errors{field: "That isn't your current password."}
	}
	return nil
}

// ChangePassword replaces u's password after checking the current one, and
// logs out every other session; currentToken's session stays logged in.
func (s *Service) ChangePassword(ctx context.Context, u User, currentToken, current, password string) error {
	if errs := s.checkPassword("new_password", password); errs != nil {
		return errs
	}
	if err := s.checkCurrentPassword(ctx, u, "current_password", current); err != nil {
		return err
	}
	err := s.db.InTx(ctx, func(tx *sql.Tx) error {
		if err := s.setPassword(ctx, store.New(tx), u.ID, password); err != nil {
			return err
		}
		return s.record(ctx, tx, Event{Name: EventPasswordChanged, UserID: u.ID})
	})
	if err != nil {
		return err
	}
	return s.sessions.DeleteForUser(ctx, u.ID, hashToken(currentToken))
}

// RequestEmailChange emails a confirmation link to the new address after
// checking u's password. If the new address already has an account, its
// owner is told instead, and the response is the same.
func (s *Service) RequestEmailChange(ctx context.Context, u User, password, newAddress string) error {
	newAddress, ok := validate.Email(newAddress)
	switch {
	case !ok:
		return validate.Errors{"email": "Enter a valid email address."}
	case newAddress == u.Email:
		return validate.Errors{"email": "That is already your email address."}
	}
	if err := s.checkCurrentPassword(ctx, u, "password", password); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		if _, err := q.GetUserByEmail(ctx, newAddress); err == nil {
			return s.sendEmail(ctx, tx, "email_in_use.txt", newAddress, map[string]any{})
		} else if !errors.Is(db.Err(err), db.ErrNotFound) {
			return err
		}
		if err := q.DeleteUserTokens(ctx, store.DeleteUserTokensParams{UserID: u.ID, Purpose: "change_email"}); err != nil {
			return err
		}
		token, err := createToken(ctx, q, u.ID, "change_email", newAddress, changeEmailTokenTTL, s.now())
		if err != nil {
			return err
		}
		return s.sendEmail(ctx, tx, "change_email.txt", newAddress, map[string]any{"Link": s.link("/confirm-email", token)})
	})
}

// ConfirmEmailChange switches the account to the address a change token was
// sent to, and tells the old address.
func (s *Service) ConfirmEmailChange(ctx context.Context, token string) error {
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		t, err := s.takeToken(ctx, q, token, "change_email")
		if err != nil {
			return err
		}
		old, err := q.GetUserByID(ctx, t.UserID)
		if err != nil {
			return db.Err(err)
		}
		// Taken since the link was sent: treat the link as spent.
		if _, err := q.GetUserByEmail(ctx, t.Email); err == nil {
			return ErrInvalidToken
		} else if !errors.Is(db.Err(err), db.ErrNotFound) {
			return err
		}
		if err := q.UpdateEmail(ctx, store.UpdateEmailParams{
			ID: t.UserID, Email: t.Email, VerifiedAt: sql.NullTime{Time: s.now(), Valid: true},
		}); err != nil {
			return db.Err(err)
		}
		if err := s.record(ctx, tx, Event{Name: EventEmailChanged, UserID: t.UserID}); err != nil {
			return err
		}
		return s.sendEmail(ctx, tx, "email_changed.txt", old.Email, map[string]any{"NewEmail": t.Email})
	})
}

// DeleteAccount deletes u and everything that belongs to them, after
// checking their password.
func (s *Service) DeleteAccount(ctx context.Context, u User, password string) error {
	if err := s.checkCurrentPassword(ctx, u, "password", password); err != nil {
		return err
	}
	return s.DeleteUser(ctx, u.ID)
}

// DeleteUser deletes a user and everything that belongs to them, returning
// ErrUserNotFound if there is no such user. A deletion hook may refuse with
// validate.Errors.
func (s *Service) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	err := s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		row, err := q.GetUserByID(ctx, userID)
		if errors.Is(db.Err(err), db.ErrNotFound) {
			return ErrUserNotFound
		} else if err != nil {
			return err
		}
		if err := runHooks(ctx, tx, s.deleting, toUser(row)); err != nil {
			return err
		}
		// Recorded before the delete, so what a hook keeps can point at
		// the user until the database clears the reference.
		if err := s.record(ctx, tx, Event{Name: EventUserDeleted, UserID: userID}); err != nil {
			return err
		}
		return q.DeleteUser(ctx, userID)
	})
	if err != nil {
		return err
	}
	// The database deletes them with the user; a shared session store
	// wouldn't.
	return s.sessions.DeleteForUser(ctx, userID, "")
}

// CreateUser adds a user directly, for the CLI and seed data.
func (s *Service) CreateUser(ctx context.Context, address, password, role string, verified bool) (User, error) {
	address, ok := validate.Email(address)
	if !ok {
		return User{}, validate.Errors{"email": "Enter a valid email address."}
	}
	if errs := s.checkPassword("password", password); errs != nil {
		return User{}, errs
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return User{}, err
	}
	now := s.now()
	var u User
	err = s.db.InTx(ctx, func(tx *sql.Tx) error {
		row, err := store.New(tx).CreateUser(ctx, store.CreateUserParams{
			ID: db.NewID(), Email: address, PasswordHash: hash, Role: role,
			EmailVerifiedAt: sql.NullTime{Time: now, Valid: verified}, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return db.Err(err)
		}
		u = toUser(row)
		if err := runHooks(ctx, tx, s.created, u); err != nil {
			return err
		}
		return s.record(ctx, tx, Event{Name: EventUserCreated, UserID: u.ID})
	})
	return u, err
}

// MarkEmailVerified records, in tx, that userID controls address: another
// feature's emailed link (an invitation, say) was opened. It does nothing if
// address is no longer the user's.
func (s *Service) MarkEmailVerified(ctx context.Context, tx *sql.Tx, userID uuid.UUID, address string) error {
	_, err := store.New(tx).MarkEmailVerified(ctx, store.MarkEmailVerifiedParams{
		ID: userID, Email: address, VerifiedAt: sql.NullTime{Time: s.now(), Valid: true},
	})
	return err
}

// UserByEmail returns the user with address, or ErrUserNotFound.
func (s *Service) UserByEmail(ctx context.Context, address string) (User, error) {
	address, _ = validate.Email(address)
	u, err := store.New(s.db.Read).GetUserByEmail(ctx, address)
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return User{}, ErrUserNotFound
	}
	return toUser(u), err
}

// SetRole gives a user a role: RoleUser or RoleAdmin.
func (s *Service) SetRole(ctx context.Context, userID uuid.UUID, role string) error {
	if role != RoleUser && role != RoleAdmin {
		return validate.Errors{"role": "Choose user or admin."}
	}
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		n, err := store.New(tx).SetRole(ctx, store.SetRoleParams{ID: userID, Role: role, UpdatedAt: s.now()})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrUserNotFound
		}
		return s.record(ctx, tx, Event{Name: EventRoleChanged, UserID: userID, Detail: role})
	})
}

// SetDisabled disables or re-enables a user. Disabling logs out all their
// sessions.
func (s *Service) SetDisabled(ctx context.Context, userID uuid.UUID, disabled bool) error {
	event := EventUserEnabled
	if disabled {
		event = EventUserDisabled
	}
	err := s.db.InTx(ctx, func(tx *sql.Tx) error {
		n, err := store.New(tx).SetDisabled(ctx, store.SetDisabledParams{
			ID: userID, DisabledAt: sql.NullTime{Time: s.now(), Valid: disabled}, UpdatedAt: s.now(),
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrUserNotFound
		}
		return s.record(ctx, tx, Event{Name: event, UserID: userID})
	})
	if err != nil || !disabled {
		return err
	}
	return s.sessions.DeleteForUser(ctx, userID, "")
}
