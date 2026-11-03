package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ukharwa/readysetgo/internal/auth/store"
	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// inviteTTL is how long a sign-up invitation works.
const inviteTTL = 7 * 24 * time.Hour

// SignupCheck lets another feature vouch for an address when sign-up is by
// invitation only, by reporting whether the address holds one of that
// feature's own invitations (to join a team, say). It runs inside the
// sign-up's transaction.
type SignupCheck func(ctx context.Context, tx *sql.Tx, address string) (bool, error)

// AllowSignup adds a SignupCheck. Add checks while setting up, before
// serving.
func (s *Service) AllowSignup(c SignupCheck) { s.signupChecks = append(s.signupChecks, c) }

// InviteOnly reports whether signing up needs an invitation.
func (s *Service) InviteOnly() bool { return s.opts.InviteOnly }

// Invite emails address a link to sign up, replacing any invitation sent
// before. Signing up through it confirms the address. An address that has
// an account gets validate.Errors: only admins and the CLI invite, and they
// may know who has one.
func (s *Service) Invite(ctx context.Context, address string) error {
	address, ok := validate.Email(address)
	if !ok {
		return validate.Errors{"email": "Enter a valid email address."}
	}
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		if _, err := q.GetUserByEmail(ctx, address); err == nil {
			return validate.Errors{"email": "That address already has an account."}
		} else if !errors.Is(db.Err(err), db.ErrNotFound) {
			return err
		}
		if err := q.DeleteSignupInvitations(ctx, address); err != nil {
			return err
		}
		token, hash := newToken()
		now := s.now()
		if err := q.CreateSignupInvitation(ctx, store.CreateSignupInvitationParams{
			TokenHash: hash, Email: address, CreatedAt: now, ExpiresAt: now.Add(inviteTTL),
		}); err != nil {
			return err
		}
		if err := s.record(ctx, tx, Event{Name: EventUserInvited}); err != nil {
			return err
		}
		return s.sendEmail(ctx, tx, "invitation.txt", address, map[string]any{"Link": s.link("/register", token)})
	})
}

// Invitation returns the address a sign-up invitation was sent to, for the
// sign-up form, or ErrInvalidToken if it's unknown or has expired. It
// doesn't use the invitation up.
func (s *Service) Invitation(ctx context.Context, token string) (string, error) {
	inv, err := store.New(s.db.Read).GetSignupInvitation(ctx, hashToken(token))
	if errors.Is(db.Err(err), db.ErrNotFound) || (err == nil && !s.now().Before(inv.ExpiresAt)) {
		return "", ErrInvalidToken
	}
	return inv.Email, err
}

// invited reports whether token is a live invitation for address; no token
// means no invitation. An unknown or expired token is ErrInvalidToken, and
// one sent to another address is validate.Errors.
func (s *Service) invited(ctx context.Context, q *store.Queries, token, address string) (bool, error) {
	if token == "" {
		return false, nil
	}
	inv, err := q.GetSignupInvitation(ctx, hashToken(token))
	switch {
	case errors.Is(db.Err(err), db.ErrNotFound) || (err == nil && !s.now().Before(inv.ExpiresAt)):
		return false, ErrInvalidToken
	case err != nil:
		return false, err
	case inv.Email != address:
		return false, validate.Errors{"email": "Use the address the invitation was sent to."}
	}
	return true, nil
}

// vouchedFor reports whether a SignupCheck vouches for address.
func (s *Service) vouchedFor(ctx context.Context, tx *sql.Tx, address string) (bool, error) {
	for _, check := range s.signupChecks {
		if ok, err := check(ctx, tx, address); err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// registeredMessage is what to tell someone who has just signed up: it
// can't say whether an account was made, unless they came with an
// invitation.
func (s *Service) registeredMessage(in Registration) string {
	switch {
	case in.Invite != "":
		return "Your account is ready. You can log in now."
	case s.opts.InviteOnly:
		return "Check your inbox for an email about signing up."
	}
	return "Check your inbox for a link to confirm your email address. You can log in now."
}
