package main

// auth.go — credential minting + verification. Deliberately SIMPLER than
// ocserverd's HS256 JWT plumbing (owner-approved M1 shape): a credential is
// 32 random bytes, hex-encoded behind a human-readable kind prefix, stored
// HASHED (sha256) in the credential table, revocable by stamping revoked_at.
// No expiry in M1 — invites are long-lived until revoked (the invite-code
// semantics design card may change this; the storage already carries what a
// TTL would need: created_at + a nullable revoked_at).

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// tokenPrefix makes a credential self-describing to a HUMAN reading a config
// file or a shell history ("which of these strings is the invite?"). The
// server never trusts the prefix: verification hashes the WHOLE string and
// requires the row's kind column to match (portal claim/session tokens are
// not credential rows at all — their prefix is purely cosmetic).
func tokenPrefix(kind string) string {
	switch kind {
	case kindPublish:
		return "ocu-pub-"
	case kindInvite:
		return "ocu-inv-"
	default:
		return "ocu-" + kind + "-" // e.g. ocu-claim-, ocu-session-
	}
}

// mintSecret generates a fresh credential: the plaintext (returned ONCE, never
// stored) and the sha256 hex the store keeps.
func mintSecret(kind string) (plaintext, secretHash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("crypto/rand: %w", err)
	}
	plaintext = tokenPrefix(kind) + hex.EncodeToString(raw)
	return plaintext, hashSecret(plaintext), nil
}

// hashSecret maps a presented credential onto its stored form.
func hashSecret(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
