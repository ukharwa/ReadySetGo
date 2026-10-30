package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// SMTPConfig describes the mail server.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string // empty: no authentication
	Password string
	From     string // "App <noreply@example.com>"
	// TLS is "starttls" (upgrade the connection; port 587), "tls"
	// (encrypted from the start; port 465) or "none" (local relays and
	// Mailpit only).
	TLS string
}

// SMTP sends email through a mail server, one connection per message.
type SMTP struct {
	cfg  SMTPConfig
	from *mail.Address
}

// NewSMTP returns an SMTP sender for cfg.
func NewSMTP(cfg SMTPConfig) (*SMTP, error) {
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("email: invalid From address %q: %w", cfg.From, err)
	}
	return &SMTP{cfg: cfg, from: from}, nil
}

// Send implements Sender. The whole exchange is bounded by ctx's deadline,
// or 30 seconds if it has none.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	if err := m.Validate(); err != nil {
		return err
	}
	to, _ := mail.ParseAddress(m.To) // validated above
	msg := compose(s.from, to, m, time.Now())

	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(30 * time.Second)
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	tlsConfig := &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	var err error
	if s.cfg.TLS == "tls" {
		conn, err = (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp: connect to %s: %w", addr, err)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: %w", err)
	}

	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: greeting: %w", err)
	}
	defer func() { _ = c.Close() }()
	if err := c.Hello(domain(s.from.Address)); err != nil {
		return fmt.Errorf("smtp: hello: %w", err)
	}
	if s.cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: server does not offer STARTTLS; refusing to send unencrypted")
		}
		if err := c.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}
	if s.cfg.Username != "" {
		// PlainAuth refuses to send the password over an unencrypted
		// connection to anything but localhost.
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("smtp: auth: %w", err)
		}
	}
	if err := c.Mail(s.from.Address); err != nil {
		return fmt.Errorf("smtp: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("smtp: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp: write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: send message: %w", err)
	}
	return c.Quit()
}

// compose renders m as an RFC 5322 message: UTF-8 plain text, encoded as
// quoted-printable so long lines and non-ASCII survive any mail server.
func compose(from, to *mail.Address, m Message, now time.Time) []byte {
	var b bytes.Buffer
	header := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	header("From", from.String())
	header("To", to.String())
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", now.Format(time.RFC1123Z))
	header("Message-ID", "<"+rand.Text()+"@"+domain(from.Address)+">")
	header("MIME-Version", "1.0")
	header("Content-Type", "text/plain; charset=utf-8")
	header("Content-Transfer-Encoding", "quoted-printable")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(m.Body))
	_ = qp.Close()
	return b.Bytes()
}

func domain(address string) string {
	_, d, _ := strings.Cut(address, "@")
	return d
}
