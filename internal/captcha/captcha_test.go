package captcha

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// answerOf reads a pending challenge's answer (tests only).
func answerOf(s *Service, id string) string {
	s.images.mu.Lock()
	defer s.images.mu.Unlock()
	return s.images.entries[id].answer
}

func TestImageChallenge(t *testing.T) {
	ctx := context.Background()
	s := NewService(Options{})
	c := Config{Provider: Image}

	id, pic, err := s.NewImage()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(pic))
	if err != nil || img.Bounds().Dx() != imageW || img.Bounds().Dy() != imageH {
		t.Fatalf("not a %dx%d PNG: %v", imageW, imageH, err)
	}
	answer := answerOf(s, id)
	if len(answer) != answerLen || strings.Trim(answer, alphabet) != "" {
		t.Fatalf("answer %q is not %d characters from the alphabet", answer, answerLen)
	}
	// Case and surrounding spaces do not matter; a challenge works once.
	if err := s.Verify(ctx, c, Answer{ID: id, Value: " " + strings.ToLower(answer) + " "}, ""); err != nil {
		t.Fatalf("right answer: %v", err)
	}
	if err := s.Verify(ctx, c, Answer{ID: id, Value: answer}, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reused challenge: %v", err)
	}

	// A wrong guess burns the challenge too, so answers cannot be brute-forced.
	id, _, _ = s.NewImage()
	answer = answerOf(s, id)
	_ = s.Verify(ctx, c, Answer{ID: id, Value: "WRONG"}, "")
	if err := s.Verify(ctx, c, Answer{ID: id, Value: answer}, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("challenge must be gone after a wrong guess: %v", err)
	}
}

func TestImageChallengeExpires(t *testing.T) {
	s := NewService(Options{})
	now := time.Now()
	s.images.now = func() time.Time { return now }
	id, _, _ := s.NewImage()
	answer := answerOf(s, id)
	now = now.Add(imageTTL + time.Second)
	if err := s.Verify(context.Background(), Config{Provider: Image}, Answer{ID: id, Value: answer}, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired challenge: %v", err)
	}
}

func TestImagesDiffer(t *testing.T) {
	s := NewService(Options{})
	_, a, _ := s.NewImage()
	_, b, _ := s.NewImage()
	if bytes.Equal(a, b) {
		t.Fatal("two challenges rendered the same image")
	}
}

func TestStoreIsBounded(t *testing.T) {
	st := newStore()
	for range maxImages + 10 {
		st.put("X")
	}
	if n := len(st.entries); n > maxImages {
		t.Fatalf("%d entries kept, limit %d", n, maxImages)
	}
}

func TestRandomTextUsesWholeAlphabet(t *testing.T) {
	seen := map[rune]bool{}
	for range 2000 {
		for _, r := range randomText(answerLen) {
			seen[r] = true
		}
	}
	if len(seen) != len(alphabet) {
		t.Fatalf("saw %d of %d characters", len(seen), len(alphabet))
	}
}

func TestSiteverify(t *testing.T) {
	var got map[string]string
	reply := `{"success": true}`
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = map[string]string{}
		for k := range r.PostForm {
			got[k] = r.PostForm.Get(k)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	ctx := context.Background()
	s := NewService(Options{Endpoints: map[string]string{Turnstile: srv.URL, ReCAPTCHA: srv.URL, HCaptcha: srv.URL}})

	hc := Config{Provider: HCaptcha, SiteKey: "site", SecretKey: "secret"}
	if err := s.Verify(ctx, hc, Answer{Value: "token"}, "203.0.113.5"); err != nil {
		t.Fatal(err)
	}
	if got["secret"] != "secret" || got["response"] != "token" || got["remoteip"] != "203.0.113.5" || got["sitekey"] != "site" {
		t.Fatalf("siteverify form %v", got)
	}

	ts := Config{Provider: Turnstile, SiteKey: "site", SecretKey: "secret"}
	cases := []struct {
		reply  string
		status int
		want   error
	}{
		{`{"success": false, "error-codes": ["invalid-input-response"]}`, 200, ErrInvalid},
		{`{"success": false, "error-codes": ["invalid-input-secret"]}`, 200, ErrUnavailable}, // our keys are wrong
		{`<html>bad gateway</html>`, 200, ErrUnavailable},
		{`{}`, 502, ErrUnavailable},
	}
	for _, c := range cases {
		reply, status = c.reply, c.status
		if err := s.Verify(ctx, ts, Answer{Value: "token"}, ""); !errors.Is(err, c.want) {
			t.Errorf("reply %d %s: %v, want %v", c.status, c.reply, err, c.want)
		}
	}
	if err := s.Verify(ctx, ts, Answer{Value: " "}, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty token: %v", err)
	}
	if err := s.Verify(ctx, Config{Provider: ReCAPTCHA}, Answer{Value: "token"}, ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("no secret configured: %v", err)
	}
	srv.Close()
	if err := s.Verify(ctx, ts, Answer{Value: "token"}, ""); !errors.Is(err, ErrUnavailable) {
		t.Errorf("provider down: %v", err)
	}
	if err := s.Verify(ctx, Config{Provider: None}, Answer{}, ""); err != nil {
		t.Errorf("captcha off: %v", err)
	}
}
