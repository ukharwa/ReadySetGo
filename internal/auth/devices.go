package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ukharwa/readysetgo/internal/httpx"
)

// ErrDeviceNotFound means the user has no session with that ID.
var ErrDeviceNotFound = errors.New("no such device")

// Client is where a login comes from, kept with its session for the device
// list.
type Client struct {
	IP        string
	UserAgent string // the User-Agent header
}

// ClientOf returns the client that sent r.
func ClientOf(r *http.Request) Client {
	c := Client{UserAgent: r.UserAgent()}
	if ip := httpx.ClientIP(r.Context()); ip.IsValid() {
		c.IP = ip.String()
	}
	return c
}

// userAgent returns the User-Agent header cut to a length worth storing.
func (c Client) userAgent() string {
	ua := c.UserAgent
	if len(ua) > 512 {
		ua = strings.ToValidUTF8(ua[:512], "")
	}
	return ua
}

// Device is one of a user's sessions, as the device list shows it.
type Device struct {
	ID         uuid.UUID
	Name       string // such as "Firefox on Windows"
	IP         string // the address it was last seen from
	CreatedAt  time.Time
	LastSeenAt time.Time
	Current    bool // the session the list was asked for from
}

// Devices returns u's signed-in sessions, browsers and API tokens alike,
// most recently used first. currentToken's session is marked Current.
func (s *Service) Devices(ctx context.Context, u User, currentToken string) ([]Device, error) {
	sessions, err := s.sessions.List(ctx, u.ID, s.now())
	if err != nil {
		return nil, err
	}
	current := hashToken(currentToken)
	devices := make([]Device, len(sessions))
	for i, sess := range sessions {
		devices[i] = Device{
			ID: sess.ID, Name: deviceName(sess.UserAgent), IP: sess.IP,
			CreatedAt: sess.CreatedAt, LastSeenAt: sess.LastSeenAt, Current: sess.TokenHash == current,
		}
	}
	return devices, nil
}

// SignOutDevice ends u's session with id, returning ErrDeviceNotFound if
// there's no such session.
func (s *Service) SignOutDevice(ctx context.Context, u User, id uuid.UUID) error {
	found, err := s.sessions.DeleteByID(ctx, u.ID, id)
	if err != nil {
		return err
	}
	if !found {
		return ErrDeviceNotFound
	}
	return s.record(ctx, nil, Event{Name: EventDeviceSignedOut, UserID: u.ID})
}

// SignOutOtherDevices ends every one of u's sessions except currentToken's.
func (s *Service) SignOutOtherDevices(ctx context.Context, u User, currentToken string) error {
	if err := s.sessions.DeleteForUser(ctx, u.ID, hashToken(currentToken)); err != nil {
		return err
	}
	return s.record(ctx, nil, Event{Name: EventDeviceSignedOut, UserID: u.ID, Detail: "all other devices"})
}

// The browsers and systems deviceName knows, in the order they're checked:
// Edge's header also names Chrome and Safari, Android's names Linux, and so
// on, so the more specific comes first.
var (
	browsers = []struct{ token, name string }{
		{"Edg/", "Edge"}, {"OPR/", "Opera"}, {"SamsungBrowser/", "Samsung Internet"},
		{"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"}, {"CriOS/", "Chrome"},
		{"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	}
	systems = []struct{ token, name string }{
		{"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Android", "Android"}, {"CrOS", "ChromeOS"},
		{"Windows", "Windows"}, {"Macintosh", "macOS"}, {"Linux", "Linux"},
	}
)

// deviceName describes a User-Agent header briefly, such as "Firefox on
// Windows". A header naming no browser it knows is shown by its first word
// ("curl/8.5.0", "MyApp/1.2").
func deviceName(ua string) string {
	find := func(list []struct{ token, name string }) string {
		for _, e := range list {
			if strings.Contains(ua, e.token) {
				return e.name
			}
		}
		return ""
	}
	browser, system := find(browsers), find(systems)
	switch {
	case browser != "" && system != "":
		return browser + " on " + system
	case browser != "":
		return browser
	}
	if first, _, _ := strings.Cut(ua, " "); first != "" {
		return first
	}
	return "Unknown device"
}
