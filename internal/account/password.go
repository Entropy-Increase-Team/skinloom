package account

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"runtime"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Password length limits, the same everywhere (registration, login, reset, admin
// edits); Blessing Skin used four different ones (S15).
const (
	MinPasswordLen = 8
	MaxPasswordLen = 128
)

// AlgoArgon2id is the only algorithm SkinLoom writes. The others verify hashes
// imported from Blessing Skin and are replaced on the user's next successful login.
const AlgoArgon2id = "argon2id"

// Blessing Skin algorithms (PWD_METHOD). Salted digests are stored as "<hex>:<salt>".
// PHP password_hash() results are stored under the algorithm their prefix names.
const (
	AlgoBcrypt        = "bcrypt"
	AlgoArgon2i       = "argon2i"
	AlgoMD5           = "md5"
	AlgoSalted2MD5    = "salted2md5"
	AlgoSHA256        = "sha256"
	AlgoSalted2SHA256 = "salted2sha256"
	AlgoSHA512        = "sha512"
	AlgoSalted2SHA512 = "salted2sha512"
)

// Argon2id parameters: the OWASP minimum (19 MiB, 2 passes, 1 lane). Hashes made with
// other parameters still verify and are rehashed on login.
type argonParams struct {
	memory  uint32 // KiB
	time    uint32
	threads uint8
}

var currentParams = argonParams{memory: 19 * 1024, time: 2, threads: 1}

const (
	saltLen = 16
	keyLen  = 32
)

// ValidatePassword checks the length policy (counted in characters, not bytes).
func ValidatePassword(pw string) error {
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordLen || n > MaxPasswordLen {
		return &FieldError{Field: "password", Message: fmt.Sprintf("must be %d to %d characters", MinPasswordLen, MaxPasswordLen)}
	}
	return nil
}

// Hasher hashes and verifies passwords. Each argon2id call takes ~19 MiB and tens of
// milliseconds, so concurrent calls are capped: a login flood queues instead of
// exhausting memory.
type Hasher struct {
	sem chan struct{}
}

// NewHasher allows up to n concurrent hash operations (0 = number of CPUs, at least 2).
func NewHasher(n int) *Hasher {
	if n <= 0 {
		n = max(2, runtime.NumCPU())
	}
	return &Hasher{sem: make(chan struct{}, n)}
}

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hasher) release() { <-h.sem }

// Hash returns an argon2id PHC string: $argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>.
func (h *Hasher) Hash(ctx context.Context, pw string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	p := currentParams
	key := argon2.IDKey([]byte(pw), salt, p.time, p.memory, p.threads, keyLen)
	return encodePHC("argon2id", p, salt, key), nil
}

// Verify reports whether pw matches the stored hash, and whether the hash should be
// replaced with a fresh argon2id one (legacy algorithm or outdated parameters).
func (h *Hasher) Verify(ctx context.Context, algo, stored, pw string) (ok, rehash bool, err error) {
	if err := h.acquire(ctx); err != nil {
		return false, false, err
	}
	defer h.release()
	switch algo {
	case AlgoArgon2id, AlgoArgon2i:
		variant, p, salt, key, err := decodePHC(stored)
		if err != nil || variant != algo {
			return false, false, errors.New("malformed argon2 hash")
		}
		var got []byte
		if variant == "argon2id" {
			got = argon2.IDKey([]byte(pw), salt, p.time, p.memory, p.threads, uint32(len(key)))
		} else {
			got = argon2.Key([]byte(pw), salt, p.time, p.memory, p.threads, uint32(len(key)))
		}
		ok = subtle.ConstantTimeCompare(got, key) == 1
		return ok, ok && (algo != AlgoArgon2id || p != currentParams || len(key) != keyLen), nil
	case AlgoBcrypt:
		err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(pw))
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, false, nil
		}
		return err == nil, err == nil, err
	case AlgoMD5, AlgoSHA256, AlgoSHA512:
		ok = digestEqual(hexDigest(newHash(algo), pw), stored)
		return ok, ok, nil
	case AlgoSalted2MD5, AlgoSalted2SHA256, AlgoSalted2SHA512:
		digest, salt, found := strings.Cut(stored, ":")
		if !found {
			return false, false, errors.New("salted hash without salt")
		}
		inner := strings.TrimPrefix(algo, "salted2")
		// Blessing Skin: hash(hash(password) . salt), both as lowercase hex
		ok = digestEqual(hexDigest(newHash(inner), hexDigest(newHash(inner), pw)+salt), digest)
		return ok, ok, nil
	default:
		return false, false, fmt.Errorf("unknown password algorithm %q", algo)
	}
}

// dummyHash is verified when a login names an unknown account, so both cases take the
// same time and the response cannot be used to enumerate accounts (S05).
var dummyHash = encodePHC("argon2id", currentParams, make([]byte, saltLen), make([]byte, keyLen))

func (h *Hasher) burn(ctx context.Context, pw string) {
	_, _, _ = h.Verify(ctx, AlgoArgon2id, dummyHash, pw)
}

func newHash(algo string) hash.Hash {
	switch algo {
	case AlgoMD5:
		return md5.New()
	case AlgoSHA256:
		return sha256.New()
	default:
		return sha512.New()
	}
}

func hexDigest(h hash.Hash, s string) string {
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func digestEqual(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(strings.ToLower(want))) == 1
}

var b64 = base64.RawStdEncoding

func encodePHC(variant string, p argonParams, salt, key []byte) string {
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s", variant, argon2.Version, p.memory, p.time, p.threads,
		b64.EncodeToString(salt), b64.EncodeToString(key))
}

// decodePHC parses $argon2{i,id}$v=19$m=..,t=..,p=..$salt$key (PHP's format too).
func decodePHC(s string) (variant string, p argonParams, salt, key []byte, err error) {
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[0] != "" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return "", p, nil, nil, errors.New("not a PHC argon2 string")
	}
	for kv := range strings.SplitSeq(parts[3], ",") {
		k, v, _ := strings.Cut(kv, "=")
		n, perr := strconv.ParseUint(v, 10, 32)
		if perr != nil {
			return "", p, nil, nil, perr
		}
		switch k {
		case "m":
			p.memory = uint32(n)
		case "t":
			p.time = uint32(n)
		case "p":
			if n > 255 {
				return "", p, nil, nil, errors.New("too many lanes")
			}
			p.threads = uint8(n)
		}
	}
	// Refuse parameters that would let a crafted hash (e.g. from a bad import) burn
	// gigabytes of memory per login attempt: at most 256 MiB, 16 passes, 16 lanes
	// (PHP's defaults are 64 MiB, 4 passes, 1 lane).
	if p.memory == 0 || p.memory > 256*1024 || p.time == 0 || p.time > 16 || p.threads == 0 || p.threads > 16 {
		return "", p, nil, nil, errors.New("argon2 parameters out of range")
	}
	if salt, err = b64.DecodeString(parts[4]); err != nil {
		return "", p, nil, nil, err
	}
	if key, err = b64.DecodeString(parts[5]); err != nil || len(key) < 16 {
		return "", p, nil, nil, errors.New("bad argon2 key")
	}
	return parts[1], p, salt, key, nil
}
