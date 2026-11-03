package auth

import (
	"context"
	"strings"
	"time"
	_ "time/tzdata" // time zones work wherever the binary runs, image or not

	"github.com/ukharwa/readysetgo/internal/auth/store"
	"github.com/ukharwa/readysetgo/internal/db"
	"github.com/ukharwa/readysetgo/internal/validate"
)

// DefaultTimeZone is a user's time zone until they choose one.
const DefaultTimeZone = "UTC"

// Location returns u's time zone, for showing times as they'd read them.
func (u User) Location() *time.Location {
	if loc := timeZone(u.TimeZone); loc != nil {
		return loc
	}
	return time.UTC
}

// timeZone returns the time zone with an IANA name, such as
// Africa/Johannesburg, or nil if there's none. It refuses "" and "Local",
// which LoadLocation would take as UTC and as the server's own zone.
func timeZone(name string) *time.Location {
	if name == "" || name == "Local" {
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return loc
}

// cleanName collapses the spaces in a display name, reporting false if it's
// too long. An empty name is fine.
func cleanName(name string) (string, bool) {
	name = strings.Join(strings.Fields(name), " ")
	return name, validate.Length(name, 0, 100)
}

const nameTooLong = "Keep your name to 100 characters."

// UpdateProfile changes u's name and time zone, returning the updated user.
// Invalid input returns validate.Errors.
func (s *Service) UpdateProfile(ctx context.Context, u User, name, zone string) (User, error) {
	errs := validate.Errors{}
	name, ok := cleanName(name)
	if !ok {
		errs["name"] = nameTooLong
	}
	if timeZone(zone) == nil {
		errs["time_zone"] = "Choose a time zone from the list."
	}
	if len(errs) > 0 {
		return User{}, errs
	}
	row, err := store.New(s.db.Write).UpdateProfile(ctx, store.UpdateProfileParams{
		ID: u.ID, Name: name, TimeZone: zone, UpdatedAt: s.now(),
	})
	if err != nil {
		return User{}, db.Err(err)
	}
	return toUser(row), nil
}
