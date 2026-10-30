package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id settings: OWASP's recommendation of 19 MiB of memory, 2
// iterations and 1 thread. Raising them later is safe: Verify reports hashes
// made with older settings, and Login re-hashes those passwords.
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	saltLength   = 16
	keyLength    = 32
)

var errMalformedHash = errors.New("malformed password hash")

// hasher hashes passwords with argon2id. Each hash takes 19 MiB of memory,
// so at most GOMAXPROCS run at once; a burst of logins queues instead of
// exhausting a small server's memory. The limit is per instance and holds no
// user data.
type hasher struct {
	slots chan struct{}
}

func newHasher() *hasher {
	return &hasher{slots: make(chan struct{}, runtime.GOMAXPROCS(0))}
}

func (h *hasher) acquire(ctx context.Context) error {
	select {
	case h.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *hasher) release() { <-h.slots }

// Hash returns the password's hash in the standard PHC format:
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
func (h *hasher) Hash(ctx context.Context, password string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	salt := make([]byte, saltLength)
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, keyLength)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify reports whether password matches encoded, and whether encoded was
// made with settings other than the current ones and should be replaced.
func (h *hasher) Verify(ctx context.Context, encoded, password string) (ok, rehash bool, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, false, errMalformedHash
	}
	var version int
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, false, errMalformedHash
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, false, errMalformedHash
	}
	b64 := base64.RawStdEncoding
	salt, err1 := b64.DecodeString(parts[4])
	want, err2 := b64.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 || len(want) > 1024 {
		return false, false, errMalformedHash
	}
	wantLength := uint32(len(want)) //nolint:gosec // bounded to 1024 just above

	if err := h.acquire(ctx); err != nil {
		return false, false, err
	}
	defer h.release()
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, wantLength)
	ok = subtle.ConstantTimeCompare(got, want) == 1
	rehash = version != argon2.Version || memory != argonMemory || time != argonTime ||
		threads != argonThreads || len(want) != keyLength
	return ok, rehash, nil
}
