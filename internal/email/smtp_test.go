package email

import (
	"context"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func TestCompose(t *testing.T) {
	t.Parallel()
	from, _ := mail.ParseAddress("Kick Start <noreply@example.com>")
	to, _ := mail.ParseAddress("ada@example.com")
	body := "Hello Ada,\n\nReset your password: https://example.com/reset?token=" + strings.Repeat("x", 100) + "\n"
	raw := compose(from, to, Message{Subject: "Café ☕", Body: body}, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))

	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("not a valid message: %v\n%s", err, raw)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != "Café ☕" {
		t.Errorf("subject = %q (%v)", subject, err)
	}
	if got := msg.Header.Get("From"); got != `"Kick Start" <noreply@example.com>` {
		t.Errorf("From = %q", got)
	}
	if !strings.HasSuffix(msg.Header.Get("Message-ID"), "@example.com>") {
		t.Errorf("Message-ID = %q", msg.Header.Get("Message-ID"))
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.ReplaceAll(string(decoded), "\r\n", "\n"); got != body {
		t.Errorf("body round trip:\ngot  %q\nwant %q", got, body)
	}
	for line := range strings.SplitSeq(string(raw), "\r\n") {
		if len(line) > 78 {
			t.Errorf("line longer than 78 characters: %q", line)
		}
	}
}

// fakeServer accepts one SMTP session on localhost and sends back what it
// received. starttls controls whether it advertises STARTTLS.
func fakeServer(t *testing.T, starttls bool) (port int, received <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		tp := textproto.NewConn(conn)
		_ = tp.PrintfLine("220 fake ESMTP")
		var log strings.Builder
		for {
			line, err := tp.ReadLine()
			if err != nil {
				got <- log.String()
				return
			}
			verb, _, _ := strings.Cut(line, " ")
			switch strings.ToUpper(verb) {
			case "EHLO":
				if starttls {
					_ = tp.PrintfLine("250-fake")
					_ = tp.PrintfLine("250 STARTTLS")
				} else {
					_ = tp.PrintfLine("250 fake")
				}
			case "DATA":
				_ = tp.PrintfLine("354 go ahead")
				lines, _ := tp.ReadDotLines()
				log.WriteString(strings.Join(lines, "\n") + "\n")
				_ = tp.PrintfLine("250 queued")
			case "QUIT":
				_ = tp.PrintfLine("221 bye")
				got <- log.String()
				return
			default:
				log.WriteString(line + "\n")
				_ = tp.PrintfLine("250 ok")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, got
}

func TestSMTPSend(t *testing.T) {
	t.Parallel()
	port, received := fakeServer(t, false)
	s, err := NewSMTP(SMTPConfig{Host: "127.0.0.1", Port: port, From: "App <noreply@example.com>", TLS: "none"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Send(ctx, Message{To: "ada@example.com", Subject: "Hi", Body: ".hidden line\nHello"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := <-received
	for _, want := range []string{"MAIL FROM:<noreply@example.com>", "RCPT TO:<ada@example.com>", "Subject: Hi", ".hidden line"} {
		if !strings.Contains(got, want) {
			t.Errorf("server did not receive %q:\n%s", want, got)
		}
	}
}

func TestSMTPRequiresStartTLS(t *testing.T) {
	t.Parallel()
	port, _ := fakeServer(t, false)
	s, _ := NewSMTP(SMTPConfig{Host: "127.0.0.1", Port: port, From: "noreply@example.com", TLS: "starttls"})
	err := s.Send(context.Background(), Message{To: "ada@example.com", Subject: "Hi", Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("Send to a server without STARTTLS: err = %v, want a refusal", err)
	}
}

func TestSMTPUnreachable(t *testing.T) {
	t.Parallel()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	s, _ := NewSMTP(SMTPConfig{Host: "127.0.0.1", Port: port, From: "noreply@example.com", TLS: "none"})
	if err := s.Send(context.Background(), Message{To: "ada@example.com", Subject: "Hi", Body: "x"}); err == nil {
		t.Error("Send to a closed port succeeded")
	}
}
