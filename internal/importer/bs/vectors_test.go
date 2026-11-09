package bs

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
)

// Password compatibility vectors (design 02 §6 item 3): every password method of BS
// (app/Services/Cipher), with and without a salt, hashed the way BS hashes it. After
// the import the password must sign in, and the first sign-in must ask for argon2id.
func TestPasswordVectors(t *testing.T) {
	const pw = "Pa55-wörd 测试"
	hexOf := func(b []byte) string { return hex.EncodeToString(b) }
	md5s := func(s string) string { h := md5.Sum([]byte(s)); return hexOf(h[:]) }
	sha256s := func(s string) string { h := sha256.Sum256([]byte(s)); return hexOf(h[:]) }
	sha512s := func(s string) string { h := sha512.Sum512([]byte(s)); return hexOf(h[:]) }
	// PHP's password_hash() writes $2y$; Go's bcrypt writes $2a$ for the same thing.
	phpBcrypt := func(string, string) string {
		h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		return "$2y$" + strings.TrimPrefix(string(h), "$2a$")
	}
	// PHP's PASSWORD_ARGON2I / ARGON2ID encoding (libargon2's), with PHP's defaults.
	phpArgon := func(id bool) func(string, string) string {
		return func(string, string) string {
			salt := []byte("0123456789abcdef")
			name, key := "argon2i", argon2.Key([]byte(pw), salt, 4, 64*1024, 1, 32)
			if id {
				name, key = "argon2id", argon2.IDKey([]byte(pw), salt, 4, 64*1024, 1, 32)
			}
			enc := base64.RawStdEncoding
			return fmt.Sprintf("$%s$v=19$m=65536,t=4,p=1$%s$%s", name, enc.EncodeToString(salt), enc.EncodeToString(key))
		}
	}
	methods := map[string]func(pw, salt string) string{
		"MD5":               func(p, _ string) string { return md5s(p) },
		"SALTED2MD5":        func(p, s string) string { return md5s(md5s(p) + s) },
		"SHA256":            func(p, _ string) string { return sha256s(p) },
		"SALTED2SHA256":     func(p, s string) string { return sha256s(sha256s(p) + s) },
		"SHA512":            func(p, _ string) string { return sha512s(p) },
		"SALTED2SHA512":     func(p, s string) string { return sha512s(sha512s(p) + s) },
		"BCRYPT":            phpBcrypt,
		"PHP_PASSWORD_HASH": phpBcrypt, // PASSWORD_DEFAULT is bcrypt
		"ARGON2I":           phpArgon(false),
		"ARGON2ID":          phpArgon(true), // what PASSWORD_DEFAULT may become
	}
	h := account.NewHasher(1)
	ctx := context.Background()
	for method, hash := range methods {
		for _, salt := range []string{"", "pepper", "带,逗号 的盐"} {
			t.Run(method+"/"+salt, func(t *testing.T) {
				stored := hash(pw, salt)
				d := &sourceData{Users: []srcUser{{ID: 1, Email: "a@example.com", Nickname: "a", Password: stored, Permission: 2}}, Options: map[string]string{}}
				r := &Report{}
				p := plan(d, nil, planOptions{Location: shanghai, PasswordMethod: method, Salt: salt, Now: time.Now()}, r)
				if len(p.Users) != 1 {
					t.Fatalf("user not planned: %v", r.Entries)
				}
				u := p.Users[0]
				ok, rehash, err := h.Verify(ctx, u.PasswordAlgo, u.PasswordHash, pw)
				if err != nil || !ok {
					t.Fatalf("%s %q does not verify (stored as %s): %v; report %v", method, stored, u.PasswordAlgo, err, r.Entries)
				}
				if !rehash {
					t.Errorf("%s: no upgrade to argon2id asked for", u.PasswordAlgo)
				}
				if bad, _, _ := h.Verify(ctx, u.PasswordAlgo, u.PasswordHash, pw+"x"); bad {
					t.Errorf("%s: a wrong password verifies", u.PasswordAlgo)
				}
				if n := r.Count(Warning); n != 0 {
					t.Errorf("warnings for a well-formed hash: %v", r.Entries)
				}
			})
		}
	}
}
