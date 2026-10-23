package account

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

func TestHashAndVerify(t *testing.T) {
	ctx := context.Background()
	h := NewHasher(2)
	hash, err := h.Hash(ctx, "correct horse 电池")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected hash format %q", hash)
	}
	if ok, rehash, err := h.Verify(ctx, AlgoArgon2id, hash, "correct horse 电池"); !ok || rehash || err != nil {
		t.Fatalf("Verify(right) = %v, %v, %v", ok, rehash, err)
	}
	if ok, _, err := h.Verify(ctx, AlgoArgon2id, hash, "correct horse"); ok || err != nil {
		t.Fatalf("Verify(wrong) = %v, %v", ok, err)
	}
	if other, _ := h.Hash(ctx, "correct horse 电池"); other == hash {
		t.Fatal("two hashes of one password must use different salts")
	}

	// Hashes made with other parameters (e.g. PHP's argon2id defaults) still verify
	// and are flagged for an upgrade.
	php := argonParams{memory: 65536, time: 4, threads: 1}
	salt := []byte("0123456789abcdef")
	old := encodePHC("argon2id", php, salt, argon2.IDKey([]byte("pw-12345"), salt, php.time, php.memory, php.threads, 32))
	if ok, rehash, err := h.Verify(ctx, AlgoArgon2id, old, "pw-12345"); !ok || !rehash || err != nil {
		t.Fatalf("Verify(old params) = %v, %v, %v", ok, rehash, err)
	}
}

// Blessing Skin's PWD_METHOD algorithms, computed here independently from the
// formulas in app/Services/Cipher/*.php.
func TestBlessingSkinHashes(t *testing.T) {
	ctx := context.Background()
	h := NewHasher(2)
	const pw, salt = "secret-123", "s4lt:with:colons"
	hexOf := func(sum []byte) string { return hex.EncodeToString(sum) }
	md5hex := func(s string) string { x := md5.Sum([]byte(s)); return hexOf(x[:]) }
	sha256hex := func(s string) string { x := sha256.Sum256([]byte(s)); return hexOf(x[:]) }
	sha512hex := func(s string) string { x := sha512.Sum512([]byte(s)); return hexOf(x[:]) }

	bc, _ := bcrypt.GenerateFromPassword([]byte(pw), 4)
	php2y := "$2y$" + strings.TrimPrefix(string(bc), "$2a$") // PHP writes the $2y$ prefix
	p := argonParams{memory: 1024, time: 2, threads: 1}
	asalt := []byte("abcdefghijklmnop")
	argon2i := encodePHC("argon2i", p, asalt, argon2.Key([]byte(pw), asalt, p.time, p.memory, p.threads, 32))

	cases := []struct{ algo, stored string }{
		{AlgoMD5, md5hex(pw)},
		{AlgoMD5, strings.ToUpper(md5hex(pw))},
		{AlgoSalted2MD5, md5hex(md5hex(pw)+salt) + ":" + salt},
		{AlgoSHA256, sha256hex(pw)},
		{AlgoSalted2SHA256, sha256hex(sha256hex(pw)+salt) + ":" + salt},
		{AlgoSHA512, sha512hex(pw)},
		{AlgoSalted2SHA512, sha512hex(sha512hex(pw)+salt) + ":" + salt},
		{AlgoBcrypt, php2y},
		{AlgoArgon2i, argon2i},
	}
	for _, c := range cases {
		ok, rehash, err := h.Verify(ctx, c.algo, c.stored, pw)
		if !ok || !rehash || err != nil {
			t.Errorf("%s: Verify(right) = %v, %v, %v; want true, true (legacy hashes are upgraded)", c.algo, ok, rehash, err)
		}
		if ok, _, _ := h.Verify(ctx, c.algo, c.stored, pw+"x"); ok {
			t.Errorf("%s: wrong password accepted", c.algo)
		}
	}
	if _, _, err := h.Verify(ctx, "rot13", "x", pw); err == nil {
		t.Error("unknown algorithm must be an error")
	}
}

func TestDecodePHCRejectsHostileParameters(t *testing.T) {
	for _, s := range []string{
		"$argon2id$v=19$m=4194304,t=2,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5",  // 4 GiB
		"$argon2id$v=19$m=1048576,t=2,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5",  // 1 GiB
		"$argon2id$v=19$m=19456,t=2,p=64$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5",   // 64 lanes
		"$argon2id$v=19$m=19456,t=1000,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5", // 1000 passes
		"$argon2id$v=19$m=19456,t=2,p=0$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=16$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0$a2V5a2V5a2V5a2V5a2V5a2V5",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$a2V5", // key too short
		"not a hash",
	} {
		if _, _, _, _, err := decodePHC(s); err == nil {
			t.Errorf("decodePHC(%q) accepted", s)
		}
	}
}

func TestHasherHonoursContext(t *testing.T) {
	h := NewHasher(1)
	h.sem <- struct{}{} // the only slot is busy
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := h.Hash(ctx, "whatever-123"); err == nil {
		t.Fatal("Hash must give up when the context ends while queued")
	}
}

func TestValidatePassword(t *testing.T) {
	for pw, ok := range map[string]bool{
		"1234567":                false,
		"12345678":               true,
		"密码密码密码密码":               true, // 8 characters, 24 bytes
		strings.Repeat("a", 128): true,
		strings.Repeat("a", 129): false,
		strings.Repeat("密", 128): true, // limit is characters, not bytes (S15)
	} {
		if err := ValidatePassword(pw); (err == nil) != ok {
			t.Errorf("ValidatePassword(%d chars) = %v, want ok=%v", len([]rune(pw)), err, ok)
		}
	}
}

func TestValidateEmailAndNickname(t *testing.T) {
	for email, ok := range map[string]bool{
		"steve@example.com":                true,
		" Steve@Example.COM ":              true,
		"Steve <steve@example.com>":        false,
		"steve@localhost":                  false,
		"no-at-sign":                       false,
		strings.Repeat("a", 251) + "@x.io": false, // 256 bytes
	} {
		if err := ValidateEmail(email); (err == nil) != ok {
			t.Errorf("ValidateEmail(%q) = %v, want ok=%v", email, err, ok)
		}
	}
	if NormalizeEmail(" Steve@Example.COM ") != "steve@example.com" {
		t.Error("NormalizeEmail must trim and lower-case")
	}
	for nick, ok := range map[string]bool{"Alex": true, "  ": false, "a\u0000b": false, strings.Repeat("字", 32): true, strings.Repeat("字", 33): false} {
		if err := ValidateNickname(nick); (err == nil) != ok {
			t.Errorf("ValidateNickname(%q) = %v, want ok=%v", nick, err, ok)
		}
	}
	errs := FieldErrors(validateNewAccount("bad", "", "short"))
	if len(errs) != 3 {
		t.Fatalf("want one error per bad field, got %v", errs)
	}
}

func TestTokens(t *testing.T) {
	a, b := NewToken(), NewToken()
	if a == b || len(a) != tokenLen || !wellFormedToken(a) {
		t.Fatalf("NewToken() = %q, %q", a, b)
	}
	if wellFormedToken(a[:42]) || wellFormedToken(strings.Repeat("!", tokenLen)) {
		t.Fatal("malformed tokens must be rejected before any database lookup")
	}
	if len(HashToken(a)) != 64 {
		t.Fatal("HashToken must be hex SHA-256")
	}
}
