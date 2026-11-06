// readysetgo:begin module=social
package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth/store"
	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// What the social module needs from accounts: people it has identified
// through another service sign up without a password, and log in without
// one.

// ErrInvitationNeeded is returned by SignUpVerified when signing up needs an
// invitation and the address has none.
var ErrInvitationNeeded = errors.New("signing up needs an invitation")

// EventAddressClaimed is recorded when ClaimAddress clears an unconfirmed
// account's password.
const EventAddressClaimed = "auth.address_claimed"

// SignUpVerified creates an account in tx for address, which the caller has
// proved belongs to the person signing up, with no password: they log in
// through the caller, or set a password with a reset link. With InviteOnly,
// the address needs an invitation, as Register's does. An address that
// already has an account returns an error matching db.ErrConflict.
func (s *Service) SignUpVerified(ctx context.Context, tx *sql.Tx, address, name, zone, how string) (User, error) {
	address, ok := validate.Email(address)
	if !ok {
		return User{}, validate.Errors{"email": "Enter a valid email address."}
	}
	if name, ok = cleanName(name); !ok {
		name = ""
	}
	if timeZone(zone) == nil {
		zone = DefaultTimeZone
	}
	q := store.New(tx)
	now := s.now()
	if s.opts.InviteOnly {
		invited, err := q.CountSignupInvitations(ctx, store.CountSignupInvitationsParams{Email: address, Now: now})
		if err != nil {
			return User{}, err
		}
		vouched, err := s.vouchedFor(ctx, tx, address)
		if err != nil {
			return User{}, err
		}
		if invited == 0 && !vouched {
			return User{}, ErrInvitationNeeded
		}
	}
	user, err := q.CreateUser(ctx, store.CreateUserParams{
		ID: db.NewID(), Email: address, Name: name, TimeZone: zone, Role: RoleUser,
		EmailVerifiedAt: sql.NullTime{Time: now, Valid: true}, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return User{}, db.Err(err)
	}
	if err := q.DeleteSignupInvitations(ctx, address); err != nil {
		return User{}, err
	}
	if err := runHooks(ctx, tx, s.created, toUser(user)); err != nil {
		return User{}, err
	}
	return toUser(user), s.Record(ctx, tx, Event{Name: EventUserCreated, UserID: user.ID, Detail: how})
}

// ClaimAddress is for someone who proved, through another service, that
// userID's address is theirs. If the address wasn't confirmed, whoever made
// the account may not own it, and could be waiting to use its password: the
// password, any email change it asked for, and every session are cleared,
// then the address is confirmed.
func (s *Service) ClaimAddress(ctx context.Context, userID uuid.UUID) error {
	var claimed bool
	err := s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		u, err := q.GetUserByID(ctx, userID)
		if err != nil || u.EmailVerifiedAt.Valid {
			return db.Err(err)
		}
		claimed = true
		if err := q.UpdatePasswordHash(ctx, store.UpdatePasswordHashParams{ID: u.ID, UpdatedAt: s.now()}); err != nil {
			return err
		}
		if err := cancelEmailChange(ctx, q, u.ID); err != nil {
			return err
		}
		if err := s.MarkEmailVerified(ctx, tx, u.ID, u.Email); err != nil {
			return err
		}
		return s.Record(ctx, tx, Event{Name: EventAddressClaimed, UserID: u.ID})
	})
	if err != nil || !claimed {
		return err
	}
	return s.sessions.DeleteForUser(ctx, userID, "")
}

// LogInAs starts a session for userID, whom the caller identified without a
// password; how names the way, such as "google", for the login event. A
// disabled account returns ErrInvalidCredentials.
// readysetgo:begin module=twofactor
// With two-factor login on, it returns *CodeRequiredError, as Login does.
// readysetgo:end
func (s *Service) LogInAs(ctx context.Context, userID uuid.UUID, client Client, life Lifetime, how string) (string, error) {
	u, err := store.New(s.db.Write).GetUserByID(ctx, userID)
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return "", ErrInvalidCredentials
	} else if err != nil {
		return "", err
	}
	if u.DisabledAt.Valid {
		return "", s.loginFailed(ctx, u.ID, client.IP, "account disabled")
	}
	// readysetgo:begin module=twofactor
	if err := s.askForCode(ctx, u.ID); err != nil {
		return "", err
	}
	// readysetgo:end
	return s.loggedIn(ctx, u.ID, client, life, how)
}

// SetSessionCookie gives the browser a session token from LogInAs. Like a
// login without "keep me logged in", the cookie ends with the browser.
func SetSessionCookie(w http.ResponseWriter, token string) {
	setSessionCookie(w, token, BrowserSession, false)
}

// readysetgo:end
