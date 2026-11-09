// Package mail sends email: an SMTP driver, a log driver for development, MIME
// building and the localized templates. Sending normally happens in a background job
// (internal/jobs); the account service enqueues the rendered Message.
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// Drivers.
const (
	DriverNone = "none"
	DriverSMTP = "smtp"
	DriverLog  = "log" // prints mail to the server log instead of sending (development)
)

// ErrDisabled means no mail driver is configured.
var ErrDisabled = errors.New("mail is not configured")

// Config is the active mail setup (from settings).
type Config struct {
	Driver     string
	From       string // "Name <address>" or a bare address
	Host       string
	Port       int
	Username   string
	Password   string
	Encryption string // tls (implicit, usually port 465) | starttls (usually 587) | none
}

// Enabled reports whether mail can be sent.
func (c Config) Enabled() bool { return c.Driver == DriverSMTP || c.Driver == DriverLog }

// Message is one email, already rendered.
type Message struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
}

// Mailer sends messages with the configured driver.
type Mailer struct {
	log *slog.Logger
	now func() time.Time
}

// New creates a Mailer.
func New(log *slog.Logger) *Mailer { return &Mailer{log: log, now: time.Now} }

// Send delivers m. SMTP errors are returned as is: callers log them or show them to
// administrators, never to visitors.
func (s *Mailer) Send(ctx context.Context, c Config, m Message) error {
	switch c.Driver {
	case DriverLog:
		s.log.Info("mail (log driver, not sent)", "to", m.To, "subject", m.Subject, "text", m.Text)
		return nil
	case DriverSMTP:
		from, err := mail.ParseAddress(c.From)
		if err != nil {
			return fmt.Errorf("mail.from %q is not a valid address: %w", c.From, err)
		}
		to, err := mail.ParseAddress(m.To)
		if err != nil {
			return fmt.Errorf("recipient %q: %w", m.To, err)
		}
		raw, err := build(from, to, m, s.now())
		if err != nil {
			return err
		}
		// The envelope takes the bare address; m.To may be "Name <address>".
		return sendSMTP(ctx, c, from.Address, to.Address, raw)
	default:
		return ErrDisabled
	}
}

// build renders m as a multipart/alternative MIME message (text and HTML).
func build(from, to *mail.Address, m Message, now time.Time) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, part := range []struct{ typ, content string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		if part.content == "" {
			continue
		}
		w, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.typ + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(part.content)); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	id := make([]byte, 12)
	_, _ = rand.Read(id)
	domain := from.Address[strings.LastIndexByte(from.Address, '@')+1:]
	var out bytes.Buffer
	// Address.String and QEncoding encode anything outside printable ASCII, CR and LF
	// included, so a site name or subject cannot inject headers.
	headers := [][2]string{
		{"From", from.String()},
		{"To", to.String()},
		{"Subject", mime.QEncoding.Encode("utf-8", oneLine(m.Subject))},
		{"Date", now.Format(time.RFC1123Z)},
		{"Message-ID", "<" + hex.EncodeToString(id) + "@" + domain + ">"},
		{"MIME-Version", "1.0"},
		{"Content-Type", "multipart/alternative; boundary=" + strconv.Quote(mw.Boundary())},
		{"Auto-Submitted", "auto-generated"},
	}
	for _, h := range headers {
		fmt.Fprintf(&out, "%s: %s\r\n", h[0], h[1])
	}
	out.WriteString("\r\n")
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func sendSMTP(ctx context.Context, c Config, from, to string, raw []byte) error {
	if c.Host == "" {
		return errors.New("mail.smtp.host is not set")
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, Deadline: deadline}
	tlsConf := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	var conn net.Conn
	var err error
	if c.Encryption == "tls" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConf}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	_ = conn.SetDeadline(deadline)
	cl, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp greeting from %s: %w", addr, err)
	}
	defer cl.Close()
	if c.Encryption == "starttls" {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s does not offer STARTTLS; use encryption = tls or none", addr)
		}
		if err := cl.StartTLS(tlsConf); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if c.Username != "" {
		ok, mechs := cl.Extension("AUTH")
		if !ok {
			return fmt.Errorf("%s does not accept authentication", addr)
		}
		var auth smtp.Auth = smtp.PlainAuth("", c.Username, c.Password, c.Host)
		if !strings.Contains(" "+mechs+" ", " PLAIN ") && strings.Contains(" "+mechs+" ", " LOGIN ") {
			auth = loginAuth{c.Username, c.Password}
		}
		if err := cl.Auth(auth); err != nil {
			return fmt.Errorf("smtp authentication: %w", err)
		}
	}
	if err := cl.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := cl.Rcpt(to); err != nil {
		return fmt.Errorf("RCPT TO: %w", err)
	}
	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("message rejected: %w", err)
	}
	return cl.Quit()
}

// loginAuth is AUTH LOGIN, for servers that do not offer PLAIN (e.g. Office 365).
// Like smtp.PlainAuth it refuses to send the password without TLS, except to localhost.
type loginAuth struct{ user, pass string }

func (a loginAuth) Start(s *smtp.ServerInfo) (string, []byte, error) {
	if !s.TLS && s.Name != "localhost" && s.Name != "127.0.0.1" && s.Name != "::1" {
		return "", nil, errors.New("unencrypted connection")
	}
	return "LOGIN", nil, nil
}

func (a loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSuffix(string(fromServer), ":")) {
	case "username":
		return []byte(a.user), nil
	case "password":
		return []byte(a.pass), nil
	}
	return nil, fmt.Errorf("unexpected LOGIN challenge %q", fromServer)
}
