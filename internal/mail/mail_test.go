package mail

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	stdmail "net/mail"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuild(t *testing.T) {
	from, _ := stdmail.ParseAddress("云织 <noreply@skins.example.com>")
	m := Render(KindResetPassword, "zh-CN", "steve@example.com", Data{Site: "我的皮肤站", Nickname: "史蒂夫", Link: "https://skins.example.com/auth/reset/abc"})
	m.Subject += "\r\nBcc: victim@example.com" // must not become a header
	to, _ := stdmail.ParseAddress(m.To)
	raw, err := build(from, to, m, time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := stdmail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Bcc") != "" {
		t.Fatal("subject injected a header")
	}
	dec := new(mime.WordDecoder)
	subject, _ := dec.DecodeHeader(msg.Header.Get("Subject"))
	fromHdr, _ := dec.DecodeHeader(msg.Header.Get("From"))
	if !strings.HasPrefix(subject, "重置你在 我的皮肤站 的密码") || fromHdr != "云织 <noreply@skins.example.com>" {
		t.Fatalf("subject %q from %q", subject, fromHdr)
	}
	if !strings.HasSuffix(msg.Header.Get("Message-ID"), "@skins.example.com>") {
		t.Fatalf("Message-ID %q", msg.Header.Get("Message-ID"))
	}
	_, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var parts []string
	for {
		p, err := mr.NextPart() // decodes quoted-printable
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p)
		parts = append(parts, p.Header.Get("Content-Type")+"\n"+string(b))
	}
	if len(parts) != 2 || !strings.Contains(parts[0], "史蒂夫，你好") || !strings.Contains(parts[0], m.Text[:10]) ||
		!strings.Contains(parts[1], `href="https://skins.example.com/auth/reset/abc"`) {
		t.Fatalf("parts: %q", parts)
	}
}

func TestRender(t *testing.T) {
	d := Data{Site: "Skins", Nickname: "<script>x</script>", Link: "https://s.example/auth/verify/t"}
	en := Render(KindVerifyEmail, "en", "a@example.com", d)
	if en.Subject != "Verify your email for Skins" || !strings.Contains(en.Text, "https://s.example/auth/verify/t") {
		t.Fatalf("en: %+v", en)
	}
	if strings.Contains(en.HTML, "<script>") || !strings.Contains(en.HTML, "&lt;script&gt;") {
		t.Fatal("HTML must escape user data")
	}
	if zh := Render(KindVerifyEmail, "fr", "a@example.com", d); !strings.HasPrefix(zh.Subject, "验证你在") {
		t.Fatalf("unknown locales fall back to zh-CN: %q", zh.Subject)
	}
}

func TestDrivers(t *testing.T) {
	ctx := context.Background()
	m := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := m.Send(ctx, Config{Driver: DriverNone}, Message{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("none: %v", err)
	}
	if err := m.Send(ctx, Config{Driver: DriverLog}, Message{To: "a@example.com"}); err != nil {
		t.Fatalf("log: %v", err)
	}
	if err := m.Send(ctx, Config{Driver: DriverSMTP, From: "not an address"}, Message{To: "a@example.com"}); err == nil {
		t.Fatal("a bad sender must be reported")
	}
}

// fakeSMTP is just enough of an SMTP server for net/smtp: no TLS, PLAIN and LOGIN auth.
type fakeSMTP struct {
	addr     string
	mechs    string
	rejectTo string

	mu   sync.Mutex
	auth []string
	from string
	to   string
	data string
}

func startFakeSMTP(t *testing.T, mechs string) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &fakeSMTP{addr: ln.Addr().String(), mechs: mechs}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	say := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
	read := func() string { l, _ := r.ReadString('\n'); return strings.TrimRight(l, "\r\n") }
	say("220 fake ESMTP")
	for {
		line := read()
		cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		f.mu.Lock()
		switch {
		case cmd == "EHLO":
			say("250-fake")
			say("250 AUTH " + f.mechs)
		case strings.HasPrefix(strings.ToUpper(line), "AUTH PLAIN"):
			b, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
			f.auth = append(f.auth, "PLAIN "+string(b))
			say("235 ok")
		case strings.HasPrefix(strings.ToUpper(line), "AUTH LOGIN"):
			say("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
			f.mu.Unlock()
			u, _ := base64.StdEncoding.DecodeString(read())
			say("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
			p, _ := base64.StdEncoding.DecodeString(read())
			f.mu.Lock()
			f.auth = append(f.auth, "LOGIN "+string(u)+":"+string(p))
			say("235 ok")
		case cmd == "MAIL":
			f.from = line
			say("250 ok")
		case cmd == "RCPT":
			if f.rejectTo != "" && strings.Contains(line, f.rejectTo) {
				say("550 no such user")
			} else {
				f.to = line
				say("250 ok")
			}
		case cmd == "DATA":
			say("354 go ahead")
			var b strings.Builder
			for {
				l := read()
				if l == "." {
					break
				}
				b.WriteString(l + "\n")
			}
			f.data = b.String()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			f.mu.Unlock()
			return
		default:
			say("502 not implemented")
		}
		f.mu.Unlock()
	}
}

func smtpConfig(f *fakeSMTP) Config {
	host, port, _ := net.SplitHostPort(f.addr)
	p, _ := strconv.Atoi(port)
	return Config{Driver: DriverSMTP, From: "SkinLoom <noreply@example.com>", Host: host, Port: p,
		Username: "user", Password: "secret", Encryption: "none"}
}

func TestSMTP(t *testing.T) {
	ctx := context.Background()
	mailer := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	msg := Render(KindTest, "en", "steve@example.com", Data{Site: "Skins"})

	f := startFakeSMTP(t, "PLAIN LOGIN")
	if err := mailer.Send(ctx, smtpConfig(f), msg); err != nil {
		t.Fatal(err)
	}
	if len(f.auth) != 1 || f.auth[0] != "PLAIN \x00user\x00secret" || !strings.Contains(f.from, "<noreply@example.com>") ||
		!strings.Contains(f.to, "<steve@example.com>") || !strings.Contains(f.data, "Subject: Skins test email") {
		t.Fatalf("auth %q from %q to %q\n%s", f.auth, f.from, f.to, f.data)
	}

	named := msg
	named.To = "Steve <steve@example.com>"
	if err := mailer.Send(ctx, smtpConfig(f), named); err != nil {
		t.Fatal(err)
	}
	if f.to != "RCPT TO:<steve@example.com>" || !strings.Contains(f.data, "To: \"Steve\" <steve@example.com>") {
		t.Fatalf("the envelope takes the bare address: %q\n%s", f.to, f.data)
	}

	login := startFakeSMTP(t, "LOGIN") // e.g. Office 365 offers no PLAIN
	if err := mailer.Send(ctx, smtpConfig(login), msg); err != nil {
		t.Fatal(err)
	}
	if len(login.auth) != 1 || login.auth[0] != "LOGIN user:secret" {
		t.Fatalf("LOGIN auth %q", login.auth)
	}

	f.rejectTo = "steve@"
	if err := mailer.Send(ctx, smtpConfig(f), msg); err == nil || !strings.Contains(err.Error(), "RCPT") {
		t.Fatalf("rejected recipient: %v", err)
	}
	c := smtpConfig(f)
	c.Port = 1 // nothing listens there
	if err := mailer.Send(ctx, c, msg); err == nil {
		t.Fatal("unreachable server must be an error")
	}
}
