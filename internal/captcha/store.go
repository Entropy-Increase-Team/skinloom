package captcha

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"strings"
	"sync"
	"time"
)

const (
	answerLen = 5
	// No 0/O, 1/I/L, 5/S, 8/B: easy to tell apart in a distorted image.
	alphabet  = "234679ACDEFGHJKMNPQRTUVWXYZ"
	imageTTL  = 5 * time.Minute
	maxImages = 20000
)

// store keeps image answers in memory. Each challenge can be tried once: right or
// wrong, the attempt consumes it, so answers cannot be brute-forced. Per process:
// behind several backend replicas use a widget provider, or sticky sessions.
type store struct {
	mu      sync.Mutex
	entries map[string]entry
	now     func() time.Time
}

type entry struct {
	answer  string
	expires time.Time
}

func newStore() *store {
	return &store{entries: map[string]entry{}, now: time.Now}
}

func (s *store) put(answer string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := base64.RawURLEncoding.EncodeToString(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if len(s.entries) >= maxImages {
		// Bound memory under a flood: drop expired entries, then arbitrary ones.
		for k, e := range s.entries {
			if now.After(e.expires) {
				delete(s.entries, k)
			}
		}
		for k := range s.entries {
			if len(s.entries) < maxImages*9/10 {
				break
			}
			delete(s.entries, k)
		}
	}
	s.entries[id] = entry{answer: answer, expires: now.Add(imageTTL)}
	return id
}

// take checks and consumes a challenge.
func (s *store) take(id, typed string) bool {
	s.mu.Lock()
	e, ok := s.entries[id]
	delete(s.entries, id)
	s.mu.Unlock()
	if !ok || s.now().After(e.expires) {
		return false
	}
	typed = strings.ToUpper(strings.TrimSpace(typed))
	return subtle.ConstantTimeCompare([]byte(typed), []byte(e.answer)) == 1
}

// randomText draws n characters uniformly from alphabet (rejection sampling: a plain
// modulo would make the first characters slightly more likely).
func randomText(n int) string {
	limit := 256 - 256%len(alphabet)
	out := make([]byte, 0, n)
	buf := make([]byte, 2*n)
	for len(out) < n {
		_, _ = rand.Read(buf)
		for _, c := range buf {
			if int(c) < limit && len(out) < n {
				out = append(out, alphabet[int(c)%len(alphabet)])
			}
		}
	}
	return string(out)
}
