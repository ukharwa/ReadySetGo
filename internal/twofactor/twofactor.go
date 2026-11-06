// Package twofactor adds two-factor login. Someone who turns it on gives a
// code from an authenticator app (TOTP, RFC 6238) after their password, or
// one of their single-use recovery codes if they've lost their phone. It
// plugs into auth.Service as its SecondFactor, so logging in asks for the
// code; this package keeps the secrets and checks the codes.
//
// Secrets are encrypted with TWO_FACTOR_KEY, so a copy of the database alone
// can't produce codes. Losing the key means everyone with two-factor login
// on needs a recovery code to log in.
package twofactor

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/email"
	"github.com/ukharwa/readysetgo/internal/twofactor/store"
	"github.com/ukharwa/readysetgo/internal/validate"
)

//go:embed emails/*.txt
var emailFS embed.FS

const (
	challengeTTL  = 10 * time.Minute // how long the code step of logging in waits
	recoveryCodes = 10               // how many a user gets at a time
)

// Names of the security events Service records.
const (
	EventEnabled              = "twofactor.enabled"
	EventDisabled             = "twofactor.disabled"
	EventRecoveryCodesCreated = "twofactor.recovery_codes_created"
	EventRecoveryCodeUsed     = "twofactor.recovery_code_used"
)

// Errors returned by Service. Their messages are safe to show users.
var (
	ErrAlreadyOn = errors.New("two-factor login is already on")
	ErrNotOn     = errors.New("two-factor login is off")
)

// Associated data for each kind of encrypted value, so one can't pass for
// another.
var challengeData = []byte("login challenge")

func secretData(userID uuid.UUID) []byte { return []byte("secret " + userID.String()) }

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// Options configures a Service.
type Options struct {
	AppName string // names the account in authenticator apps and emails
	Key     []byte // 32 bytes that encrypt the secrets (TWO_FACTOR_KEY)
}

// Service turns two-factor login on and off and checks codes.
type Service struct {
	db       *db.DB
	accounts *auth.Service
	logger   *slog.Logger
	opts     Options
	aead     cipher.AEAD
	emails   *email.Templates
	now      func() time.Time // replaced in tests
}

// NewService returns a Service and makes accounts ask for a code at login
// from everyone who turned two-factor login on.
func NewService(d *db.DB, accounts *auth.Service, logger *slog.Logger, opts Options) (*Service, error) {
	block, err := aes.NewCipher(opts.Key)
	if err != nil {
		return nil, fmt.Errorf("two-factor key: %w", err)
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, err
	}
	emails, err := email.ParseTemplates(emailFS, "emails/*.txt")
	if err != nil {
		return nil, err
	}
	s := &Service{db: d, accounts: accounts, logger: logger, opts: opts, aead: aead, emails: emails, now: db.Now}
	accounts.UseSecondFactor(s)
	return s, nil
}

// Status is whether someone has two-factor login on.
type Status struct {
	On                bool
	Since             time.Time
	RecoveryCodesLeft int
}

// Status returns u's two-factor status.
func (s *Service) Status(ctx context.Context, u auth.User) (Status, error) {
	q := store.New(s.db.Read)
	row, err := q.GetTwoFactor(ctx, u.ID)
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return Status{}, nil
	} else if err != nil {
		return Status{}, db.Err(err)
	}
	left, err := q.CountRecoveryCodes(ctx, u.ID)
	if err != nil {
		return Status{}, db.Err(err)
	}
	return Status{On: true, Since: row.EnabledAt, RecoveryCodesLeft: int(left)}, nil
}

// Setup is a new secret for someone turning two-factor login on. They add it
// to their authenticator app by scanning URI as a QR code, or by typing
// Secret.
type Setup struct {
	Secret string // base32, as the apps take it
	URI    string // otpauth://…
}

// NewSetup returns a new secret for u. Nothing is stored until Enable.
func (s *Service) NewSetup(u auth.User) Setup {
	b := make([]byte, 20) // 160 bits, as RFC 4226 recommends
	_, _ = rand.Read(b)   // never fails
	return s.setup(u, b32.EncodeToString(b))
}

func (s *Service) setup(u auth.User, secret string) Setup {
	return Setup{Secret: secret, URI: keyURI(s.opts.AppName, u.Email, secret)}
}

// Enable turns two-factor login on for u with setup's secret, once code
// shows their app has it, and returns their recovery codes to show once. It
// signs out u's other devices, so a session someone stole doesn't outlive
// the change. u's address must be confirmed: whoever made an account may not
// own its address, and would lock the owner out of it.
func (s *Service) Enable(ctx context.Context, u auth.User, secret, code, password string) ([]string, error) {
	if !u.EmailVerified {
		return nil, validate.Errors{"code": "Confirm your email address first. Your account page can resend the link."}
	}
	if err := s.accounts.CheckCurrentPassword(ctx, u, "password", password); err != nil {
		return nil, err
	}
	raw, err := b32.DecodeString(secret)
	n, ok := matchStep(raw, cleanCode(code), s.now())
	if err != nil || len(raw) < 16 || !ok {
		return nil, validate.Errors{"code": "That code isn't right. Check your phone sets its clock automatically, and use the newest code."}
	}
	codes := newRecoveryCodes()
	err = s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		err := q.CreateTwoFactor(ctx, store.CreateTwoFactorParams{
			UserID: u.ID, Secret: s.seal(raw, secretData(u.ID)), LastStep: n, EnabledAt: s.now(),
		})
		if errors.Is(db.Err(err), db.ErrConflict) {
			return ErrAlreadyOn
		} else if err != nil {
			return db.Err(err)
		}
		if err := storeRecoveryCodes(ctx, q, u.ID, codes); err != nil {
			return err
		}
		if err := s.accounts.Record(ctx, tx, auth.Event{Name: EventEnabled, UserID: u.ID}); err != nil {
			return err
		}
		return s.emailUser(ctx, tx, u.ID, "enabled.txt", nil)
	})
	if err != nil {
		return nil, err
	}
	if err := s.accounts.SignOutOtherDevices(ctx, u, auth.SessionToken(ctx)); err != nil {
		s.logger.ErrorContext(ctx, "sign out other devices after turning on two-factor login", "err", err)
	}
	return codes, nil
}

// NewRecoveryCodes replaces u's recovery codes, after checking their
// password, and returns the new ones to show once.
func (s *Service) NewRecoveryCodes(ctx context.Context, u auth.User, password string) ([]string, error) {
	if err := s.accounts.CheckCurrentPassword(ctx, u, "password", password); err != nil {
		return nil, err
	}
	codes := newRecoveryCodes()
	err := s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		if _, err := q.GetTwoFactor(ctx, u.ID); errors.Is(db.Err(err), db.ErrNotFound) {
			return ErrNotOn
		} else if err != nil {
			return db.Err(err)
		}
		if err := q.DeleteRecoveryCodes(ctx, u.ID); err != nil {
			return db.Err(err)
		}
		if err := storeRecoveryCodes(ctx, q, u.ID, codes); err != nil {
			return err
		}
		return s.accounts.Record(ctx, tx, auth.Event{Name: EventRecoveryCodesCreated, UserID: u.ID})
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// Disable turns two-factor login off for u, after checking their password.
func (s *Service) Disable(ctx context.Context, u auth.User, password string) error {
	if err := s.accounts.CheckCurrentPassword(ctx, u, "password", password); err != nil {
		return err
	}
	return s.turnOff(ctx, u.ID, "")
}

// Reset turns two-factor login off for someone who lost both their phone and
// their recovery codes, without their password: the `user reset-two-factor`
// command. Check who's asking before running it.
func (s *Service) Reset(ctx context.Context, userID uuid.UUID) error {
	return s.turnOff(ctx, userID, "reset")
}

func (s *Service) turnOff(ctx context.Context, userID uuid.UUID, detail string) error {
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		n, err := q.DeleteTwoFactor(ctx, userID)
		if err != nil {
			return db.Err(err)
		}
		if n == 0 {
			return ErrNotOn
		}
		if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
			return db.Err(err)
		}
		if err := s.accounts.Record(ctx, tx, auth.Event{Name: EventDisabled, UserID: userID, Detail: detail}); err != nil {
			return err
		}
		return s.emailUser(ctx, tx, userID, "disabled.txt", nil)
	})
}

// Challenge implements auth.SecondFactor. The challenge is the user's ID and
// an expiry time, encrypted, so it needs no table; a code is still needed to
// use it, and each code works once.
func (s *Service) Challenge(ctx context.Context, userID uuid.UUID) (string, error) {
	_, err := store.New(s.db.Write).GetTwoFactor(ctx, userID)
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return "", nil
	} else if err != nil {
		return "", db.Err(err)
	}
	expires := s.now().Add(challengeTTL).Unix()
	b := binary.BigEndian.AppendUint64(userID[:], uint64(expires)) //nolint:gosec // Unix times now are positive
	return base64.RawURLEncoding.EncodeToString(s.seal(b, challengeData)), nil
}

// ChallengeUser implements auth.SecondFactor.
func (s *Service) ChallengeUser(challenge string) (uuid.UUID, error) {
	sealed, err := base64.RawURLEncoding.DecodeString(challenge)
	if err != nil {
		return uuid.Nil, auth.ErrLoginExpired
	}
	b, err := s.aead.Open(nil, nil, sealed, challengeData)
	if err != nil || len(b) != 24 {
		return uuid.Nil, auth.ErrLoginExpired
	}
	if expires := binary.BigEndian.Uint64(b[16:]); uint64(s.now().Unix()) >= expires { //nolint:gosec // as in Challenge
		return uuid.Nil, auth.ErrLoginExpired
	}
	return uuid.UUID(b[:16]), nil
}

// CheckCode implements auth.SecondFactor: code is one from userID's app, or
// one of their recovery codes.
func (s *Service) CheckCode(ctx context.Context, userID uuid.UUID, code string) (bool, error) {
	code = cleanCode(code)
	if len(code) != digits {
		return s.useRecoveryCode(ctx, userID, code)
	}
	q := store.New(s.db.Write)
	row, err := q.GetTwoFactor(ctx, userID)
	if errors.Is(db.Err(err), db.ErrNotFound) {
		return false, nil
	} else if err != nil {
		return false, db.Err(err)
	}
	secret, err := s.aead.Open(nil, nil, row.Secret, secretData(userID))
	if err != nil {
		return false, fmt.Errorf("decrypt two-factor secret (has TWO_FACTOR_KEY changed?): %w", err)
	}
	n, ok := matchStep(secret, code, s.now())
	if !ok || n <= row.LastStep {
		return false, nil
	}
	used, err := q.UseStep(ctx, store.UseStepParams{UserID: userID, LastStep: n})
	return used == 1, db.Err(err)
}

func (s *Service) useRecoveryCode(ctx context.Context, userID uuid.UUID, code string) (bool, error) {
	var used bool
	err := s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		n, err := q.TakeRecoveryCode(ctx, store.TakeRecoveryCodeParams{UserID: userID, CodeHash: hashCode(code)})
		if err != nil || n == 0 {
			return db.Err(err)
		}
		used = true
		left, err := q.CountRecoveryCodes(ctx, userID)
		if err != nil {
			return db.Err(err)
		}
		e := auth.Event{Name: EventRecoveryCodeUsed, UserID: userID, Detail: fmt.Sprintf("%d left", left)}
		if err := s.accounts.Record(ctx, tx, e); err != nil {
			return err
		}
		return s.emailUser(ctx, tx, userID, "recovery_code_used.txt", map[string]any{"Left": left})
	})
	return used, err
}

// seal encrypts b with the key, bound to data; aead.Open reverses it.
func (s *Service) seal(b, data []byte) []byte {
	return s.aead.Seal(nil, nil, b, data) //nolint:gosec // the AEAD picks a random nonce for each call (NewGCMWithRandomNonce)
}

// emailUser queues an email to userID in tx.
func (s *Service) emailUser(ctx context.Context, tx *sql.Tx, userID uuid.UUID, template string, data map[string]any) error {
	u, err := store.New(tx).GetUser(ctx, userID)
	if err != nil {
		return db.Err(err)
	}
	if data == nil {
		data = map[string]any{}
	}
	data["AppName"], data["Name"] = s.opts.AppName, u.Name
	m, err := s.emails.Message(template, u.Email, data)
	if err != nil {
		return err
	}
	return email.Enqueue(ctx, tx, m)
}

// newRecoveryCodes returns new recovery codes, each 60 random bits written
// as XXXX-XXXX-XXXX.
func newRecoveryCodes() []string {
	codes := make([]string, recoveryCodes)
	for i := range codes {
		t := rand.Text()
		codes[i] = t[:4] + "-" + t[4:8] + "-" + t[8:12]
	}
	return codes
}

func storeRecoveryCodes(ctx context.Context, q *store.Queries, userID uuid.UUID, codes []string) error {
	for _, c := range codes {
		err := q.CreateRecoveryCode(ctx, store.CreateRecoveryCodeParams{UserID: userID, CodeHash: hashCode(cleanCode(c))})
		if err != nil {
			return db.Err(err)
		}
	}
	return nil
}

// cleanCode drops the spaces and dashes people type in codes, and
// capitalises recovery codes.
func cleanCode(c string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(c))
}

// hashCode is how a recovery code is stored: like an emailed token, it is
// random enough that a plain hash can't be reversed by guessing.
func hashCode(c string) string {
	sum := sha256.Sum256([]byte(c))
	return hex.EncodeToString(sum[:])
}
