package db_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/ukharwa/kickstart/internal/auth/store"
	"github.com/ukharwa/kickstart/internal/db"
	"github.com/ukharwa/kickstart/internal/db/dbtest"
)

func newUser(email string) store.CreateUserParams {
	now := db.Now()
	return store.CreateUserParams{
		ID:           db.NewID(),
		Email:        email,
		PasswordHash: "hash",
		Role:         "user",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// TestQueries checks a round trip through the generated queries: types map
// the same way on every database and driver errors become db errors.
func TestQueries(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	ctx := t.Context()
	q := store.New(d.Write)

	arg := newUser("ada@example.com")
	created, err := q.CreateUser(ctx, arg)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := store.New(d.Read).GetUserByEmail(ctx, arg.Email)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got.ID != arg.ID || got.Email != arg.Email || got.Role != arg.Role {
		t.Errorf("GetUserByEmail = %+v, want the user just created", got)
	}
	if !got.CreatedAt.Equal(arg.CreatedAt) || !created.CreatedAt.Equal(arg.CreatedAt) {
		t.Errorf("created_at = %v, want %v", got.CreatedAt, arg.CreatedAt)
	}
	if got.CreatedAt.Location().String() != "UTC" {
		t.Errorf("created_at location = %v, want UTC", got.CreatedAt.Location())
	}
	if got.EmailVerifiedAt.Valid {
		t.Errorf("email_verified_at = %v, want NULL", got.EmailVerifiedAt)
	}

	_, err = q.CreateUser(ctx, newUser(arg.Email))
	if err := db.Err(err); !errors.Is(err, db.ErrConflict) {
		t.Errorf("duplicate email: err = %v, want ErrConflict", err)
	}

	_, err = q.GetUserByID(ctx, db.NewID())
	if err := db.Err(err); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("missing user: err = %v, want ErrNotFound", err)
	}
}

func TestInTx(t *testing.T) {
	t.Parallel()
	d := dbtest.New(t)
	ctx := t.Context()

	committed := newUser("commit@example.com")
	err := d.InTx(ctx, func(tx *sql.Tx) error {
		_, err := store.New(tx).CreateUser(ctx, committed)
		return err
	})
	if err != nil {
		t.Fatalf("InTx: %v", err)
	}

	rolledBack := newUser("rollback@example.com")
	errBoom := errors.New("boom")
	err = d.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := store.New(tx).CreateUser(ctx, rolledBack); err != nil {
			return err
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("InTx = %v, want errBoom", err)
	}

	q := store.New(d.Read)
	if _, err := q.GetUserByID(ctx, committed.ID); err != nil {
		t.Errorf("committed user: %v", err)
	}
	if _, err := q.GetUserByID(ctx, rolledBack.ID); !errors.Is(db.Err(err), db.ErrNotFound) {
		t.Errorf("rolled-back user: err = %v, want ErrNotFound", err)
	}
}

func TestNewID(t *testing.T) {
	t.Parallel()
	a, b := db.NewID(), db.NewID()
	if a.Version() != 7 {
		t.Errorf("version = %d, want 7", a.Version())
	}
	if a.String() >= b.String() {
		t.Errorf("IDs not time-ordered: %s then %s", a, b)
	}
}
