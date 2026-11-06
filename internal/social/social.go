// Package social lets people log in with Google or GitHub (OAuth 2.0's code
// flow, with PKCE). A provider is on when its client ID and secret are set.
//
// Coming back from a provider signs someone in as, in order:
//   - the account that provider account was linked to before;
//   - otherwise the account with the same address, once the provider has
//     verified it. If the account never confirmed its address, its password
//     and sessions are cleared first (auth.Service.ClaimAddress), since
//     whoever made it may not own the address;
//   - otherwise a new account with no password, if signing up allows it.
//
// Someone logged in can connect providers to their account, and disconnect
// them, as long as a way to log in remains.
package social

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/ukharwa/readysetgo/internal/auth"
	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/email"
	"github.com/ukharwa/readysetgo/internal/social/store"
)

//go:embed emails/*.txt
var emailFS embed.FS

// Names of the security events Service records.
const (
	EventConnected    = "social.connected"
	EventDisconnected = "social.disconnected"
)

// Errors returned by Service. Their messages are safe to show users.
var (
	ErrUnknownProvider    = errors.New("there's no such way to log in")
	ErrUnverifiedEmail    = errors.New("that account's email address isn't verified, so it can't be used here")
	ErrLinkedElsewhere    = errors.New("that account is connected to someone else here")
	ErrAlreadyConnected   = errors.New("you've already connected an account from there: disconnect it first")
	ErrLastWayIn          = errors.New("that's your only way to log in: set a password first")
	ErrUnconfirmedAddress = errors.New("confirm your email address before connecting another way to log in")
)

// Options configures a Service.
type Options struct {
	AppName string
	BaseURL string // APP_URL: providers send people back to it
	Google  Credentials
	GitHub  Credentials
}

// Credentials are the app's client ID and secret at a provider. A provider
// is on when they're set.
type Credentials struct {
	ClientID, ClientSecret string
}

// Service links accounts at providers to users, and signs people in with
// them.
type Service struct {
	db        *db.DB
	accounts  *auth.Service
	logger    *slog.Logger
	opts      Options
	emails    *email.Templates
	providers []*provider
	now       func() time.Time // replaced in tests
}

// NewService returns a Service with the providers opts has credentials for.
func NewService(d *db.DB, accounts *auth.Service, logger *slog.Logger, opts Options) (*Service, error) {
	emails, err := email.ParseTemplates(emailFS, "emails/*.txt")
	if err != nil {
		return nil, err
	}
	return &Service{
		db: d, accounts: accounts, logger: logger, opts: opts, emails: emails,
		providers: newProviders(opts), now: db.Now,
	}, nil
}

// Provider is a service people can log in with.
type Provider struct {
	ID   string // in URLs, such as "google"
	Name string // to show, such as "Google"
}

// Providers returns the providers that are on.
func (s *Service) Providers() []Provider {
	out := make([]Provider, len(s.providers))
	for i, p := range s.providers {
		out[i] = Provider{ID: p.id, Name: p.name}
	}
	return out
}

func (s *Service) provider(id string) (*provider, error) {
	for _, p := range s.providers {
		if p.id == id {
			return p, nil
		}
	}
	return nil, ErrUnknownProvider
}

// authURL returns provider id's consent page, which sends the person back to
// callback with a code for state. verifier is PKCE's: Exchange needs it.
func (s *Service) authURL(id, callback, state, verifier string) (string, error) {
	p, err := s.provider(id)
	if err != nil {
		return "", err
	}
	cfg := p.oauth
	cfg.RedirectURL = callback
	return cfg.AuthCodeURL(state, append(p.extra, oauth2.S256ChallengeOption(verifier))...), nil
}

// Exchange trades the code provider id sent back to callback for the
// person's account there.
func (s *Service) Exchange(ctx context.Context, id, callback, code, verifier string) (Account, error) {
	p, err := s.provider(id)
	if err != nil {
		return Account{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cfg := p.oauth
	cfg.RedirectURL = callback
	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Account{}, fmt.Errorf("%s: exchange code: %w", id, err)
	}
	acct, err := p.account(ctx, cfg.Client(ctx, token), p.apiURL)
	if err != nil {
		return Account{}, fmt.Errorf("%s: read account: %w", id, err)
	}
	if acct.Subject == "" {
		return Account{}, fmt.Errorf("%s: %w", id, errNoSubject)
	}
	return acct, nil
}

// SignIn returns the user acct at provider id logs in as, linking or
// creating an account by the rules in the package comment. zone is the
// time zone for a new account. Signing up when it needs an invitation the
// address lacks returns auth.ErrInvitationNeeded.
func (s *Service) SignIn(ctx context.Context, id string, acct Account, zone string) (uuid.UUID, error) {
	p, err := s.provider(id)
	if err != nil {
		return uuid.Nil, err
	}
	ident, err := store.New(s.db.Write).GetIdentity(ctx, store.GetIdentityParams{Provider: p.id, Subject: acct.Subject})
	if err == nil {
		return ident.UserID, nil
	} else if !errors.Is(db.Err(err), db.ErrNotFound) {
		return uuid.Nil, db.Err(err)
	}
	if !acct.EmailVerified {
		return uuid.Nil, ErrUnverifiedEmail
	}

	u, err := s.accounts.UserByEmail(ctx, acct.Email)
	switch {
	case err == nil:
		if !u.EmailVerified {
			if err := s.accounts.ClaimAddress(ctx, u.ID); err != nil {
				return uuid.Nil, err
			}
		}
		return u.ID, s.db.InTx(ctx, func(tx *sql.Tx) error { return s.link(ctx, tx, u, p, acct, true) })
	case errors.Is(err, auth.ErrUserNotFound):
		err := s.db.InTx(ctx, func(tx *sql.Tx) error {
			if u, err = s.accounts.SignUpVerified(ctx, tx, acct.Email, acct.Name, zone, p.id); err != nil {
				return err
			}
			return s.link(ctx, tx, u, p, acct, false)
		})
		return u.ID, err
	default:
		return uuid.Nil, err
	}
}

// Connect links acct at provider id to u, who is logged in. An account
// whose address isn't confirmed can't: whoever made it may not own the
// address, and would keep this way in after its owner claimed the account.
func (s *Service) Connect(ctx context.Context, u auth.User, id string, acct Account) error {
	p, err := s.provider(id)
	if err != nil {
		return err
	}
	if !u.EmailVerified {
		return ErrUnconfirmedAddress
	}
	ident, err := store.New(s.db.Write).GetIdentity(ctx, store.GetIdentityParams{Provider: p.id, Subject: acct.Subject})
	switch {
	case err == nil && ident.UserID == u.ID:
		return nil
	case err == nil:
		return ErrLinkedElsewhere
	case !errors.Is(db.Err(err), db.ErrNotFound):
		return db.Err(err)
	}
	return s.db.InTx(ctx, func(tx *sql.Tx) error { return s.link(ctx, tx, u, p, acct, true) })
}

// link connects acct at p to u in tx, emailing u about it if notify is set.
func (s *Service) link(ctx context.Context, tx *sql.Tx, u auth.User, p *provider, acct Account, notify bool) error {
	err := store.New(tx).CreateIdentity(ctx, store.CreateIdentityParams{
		Provider: p.id, Subject: acct.Subject, UserID: u.ID, Email: acct.Email, CreatedAt: s.now(),
	})
	if errors.Is(db.Err(err), db.ErrConflict) {
		return ErrAlreadyConnected
	} else if err != nil {
		return db.Err(err)
	}
	if err := s.accounts.Record(ctx, tx, auth.Event{Name: EventConnected, UserID: u.ID, Detail: p.id}); err != nil {
		return err
	}
	if !notify {
		return nil
	}
	m, err := s.emails.Message("connected.txt", u.Email, map[string]any{
		"AppName": s.opts.AppName, "Name": u.Name, "Provider": p.name, "Account": acct.Email,
	})
	if err != nil {
		return err
	}
	return email.Enqueue(ctx, tx, m)
}

// Identity is a provider account linked to a user.
type Identity struct {
	Provider    Provider
	Email       string // the address the provider gave
	ConnectedAt time.Time
}

// Identities returns the provider accounts linked to u.
func (s *Service) Identities(ctx context.Context, u auth.User) ([]Identity, error) {
	rows, err := store.New(s.db.Read).ListIdentities(ctx, u.ID)
	if err != nil {
		return nil, db.Err(err)
	}
	out := make([]Identity, 0, len(rows))
	for _, row := range rows {
		name := row.Provider // a provider turned off since keeps its ID as its name
		if p, err := s.provider(row.Provider); err == nil {
			name = p.name
		}
		out = append(out, Identity{
			Provider: Provider{ID: row.Provider, Name: name}, Email: row.Email, ConnectedAt: row.CreatedAt,
		})
	}
	return out, nil
}

// Disconnect unlinks u's account at provider id, unless it's their only way
// to log in.
func (s *Service) Disconnect(ctx context.Context, u auth.User, id string) error {
	return s.db.InTx(ctx, func(tx *sql.Tx) error {
		q := store.New(tx)
		rows, err := q.ListIdentities(ctx, u.ID)
		if err != nil {
			return db.Err(err)
		}
		if !slices.ContainsFunc(rows, func(r store.UserIdentity) bool { return r.Provider == id }) {
			return nil
		}
		if !u.HasPassword && len(rows) == 1 {
			return ErrLastWayIn
		}
		if err := q.DeleteIdentity(ctx, store.DeleteIdentityParams{UserID: u.ID, Provider: id}); err != nil {
			return db.Err(err)
		}
		return s.accounts.Record(ctx, tx, auth.Event{Name: EventDisconnected, UserID: u.ID, Detail: id})
	})
}
