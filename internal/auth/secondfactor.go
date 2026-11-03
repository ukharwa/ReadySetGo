// readysetgo:begin module=twofactor
package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth/store"
	"github.com/ukharwa/readysetgo/internal/db"
)

// Errors from the second step of logging in.
var (
	ErrInvalidCode  = errors.New("that code isn't right")
	ErrLoginExpired = errors.New("logging in took too long")
)

// SecondFactor is what Login asks for after a right password, for users who
// turned two-factor login on. The twofactor package implements it.
type SecondFactor interface {
	// Challenge returns a token that continues userID's login once they
	// give a code, or "" if their password is enough.
	Challenge(ctx context.Context, userID uuid.UUID) (string, error)
	// ChallengeUser returns whose login challenge continues, or
	// ErrLoginExpired if it is unknown or too old.
	ChallengeUser(challenge string) (uuid.UUID, error)
	// CheckCode reports whether code is right for userID, using it up.
	CheckCode(ctx context.Context, userID uuid.UUID, code string) (bool, error)
}

// UseSecondFactor makes Login ask for f after the password.
func (s *Service) UseSecondFactor(f SecondFactor) { s.secondFactor = f }

// askForCode returns *CodeRequiredError if userID must give a code to finish
// logging in.
func (s *Service) askForCode(ctx context.Context, userID uuid.UUID) error {
	if s.secondFactor == nil {
		return nil
	}
	challenge, err := s.secondFactor.Challenge(ctx, userID)
	if err != nil {
		return err
	}
	if challenge != "" {
		return &CodeRequiredError{Challenge: challenge}
	}
	return nil
}

// CodeRequiredError is what Login returns when the password was right and
// a code is needed too. FinishLogin continues with Challenge and the code.
type CodeRequiredError struct {
	Challenge string
}

func (e *CodeRequiredError) Error() string { return "a code is needed to finish logging in" }

// FinishLogin starts a session for a login that Login answered with
// *CodeRequiredError, once code is right. Wrong codes are limited per
// account (5 per 15 minutes, whatever the challenge), since whoever sends
// them knows the password.
func (s *Service) FinishLogin(ctx context.Context, challenge, code string, client Client, life Lifetime) (string, error) {
	userID, err := s.secondFactor.ChallengeUser(challenge)
	if err != nil {
		return "", err
	}
	key := "login:code:" + userID.String()
	if err := s.allow(ctx, key, 5, 15*time.Minute); err != nil {
		return "", err
	}
	u, err := store.New(s.db.Write).GetUserByID(ctx, userID)
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return "", ErrLoginExpired
	} else if err != nil {
		return "", err
	}
	if u.DisabledAt.Valid {
		return "", s.loginFailed(ctx, u.ID, client.IP, "account disabled")
	}
	ok, err := s.secondFactor.CheckCode(ctx, u.ID, code)
	if err != nil {
		return "", err
	}
	if !ok {
		if err := s.Record(ctx, nil, Event{Name: EventLoginFailed, UserID: u.ID, IP: client.IP, Detail: "wrong code"}); err != nil {
			return "", err
		}
		return "", ErrInvalidCode
	}
	if err := s.limiter.Reset(ctx, key); err != nil {
		return "", err
	}
	return s.loggedIn(ctx, u.ID, client, life, "")
}

// readysetgo:end
