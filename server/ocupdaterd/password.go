package main

// password.go — argon2id portal-password hashing, a deliberate line-for-line
// twin of server/ocserverd/password.go (the owner-designated template for the
// portal's first-run set-password / login flow). The DB stores ONLY the
// PHC-encoded hash ($argon2id$v=19$…); plaintext never persists. argon2id
// over bcrypt: pure Go (x/crypto), no 72-byte input truncation.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// OWASP-recommended argon2id cost profile (m=19 MiB, t=2, p=1) — an
// interactive-login hash, verified once per /api/login.
const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonSaltLen   = 16
	argonKeyLen    = 32
)

// hashPassword produces a PHC-format argon2id string
// ($argon2id$v=19$m=…,t=…,p=…$<b64 salt>$<b64 key>; unpadded standard base64
// per the PHC spec).
func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// verifyPassword checks password against a PHC argon2id string (parameters are
// read from the string, so a future cost bump keeps verifying old hashes).
// Any malformed input is a plain false — the login path answers a flat 401
// with no distinguishing hint either way.
func verifyPassword(password, phc string) bool {
	parts := strings.Split(phc, "$")
	// ["", "argon2id", "v=19", "m=…,t=…,p=…", salt, key]
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}
	if memory == 0 || time == 0 || threads == 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
