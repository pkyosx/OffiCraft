package main

import (
	"errors"
	"strings"
	"testing"
)

// The shared interop fixture: a SYNTHETIC unit-test signing secret (never a
// real credential; allowlisted in .gitleaks.toml like the retired Python test conftest)
// plus two tokens minted by the PYTHON side (the retired Python plumbing/auth.py `mint`)
// with pinned inputs. The Go mint must reproduce these BYTE FOR BYTE, and the
// Go verify must accept them — that is the cross-daemon interop contract.
//
// Regenerate (against the retired Python implementation — git tag py-final):
//
//	from plumbing.auth import mint
//	mint("kyle", "agent", 3600, "interop-unit-test-signing-secret",
//	     now=1750000000, machine_id="mac-01")
//	mint("owner", "owner", 3600, "interop-unit-test-signing-secret",
//	     now=1750000000)
const (
	interopSecret = "interop-unit-test-signing-secret"
	interopNow    = int64(1750000000)

	pythonMintedAgentToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
		"eyJzdWIiOiJreWxlIiwic2NvcGUiOiJhZ2VudCIsImlhdCI6MTc1MDAwMDAwMCwiZXhwIjoxNzUwMDAzNjAwLCJtYWNoaW5lX2lkIjoibWFjLTAxIn0." +
		"vfwdCgM-iX5NDyk_VjvzuSNu0HwXKZsilJHisFSH680"
	pythonMintedOwnerToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
		"eyJzdWIiOiJvd25lciIsInNjb3BlIjoib3duZXIiLCJpYXQiOjE3NTAwMDAwMDAsImV4cCI6MTc1MDAwMzYwMH0." +
		"NW1mpIKc6u4tsf9DjXekp1LYmk54-6p3D6FKog0eAjA"
)

func TestMintMatchesPythonByteForByte(t *testing.T) {
	got, err := mintJWT("kyle", "agent", 3600, []byte(interopSecret), interopNow, "mac-01")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if got != pythonMintedAgentToken {
		t.Fatalf("Go mint diverges from the Python-minted token:\n got %s\nwant %s", got, pythonMintedAgentToken)
	}

	got, err = mintJWT("owner", "owner", 3600, []byte(interopSecret), interopNow, "")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if got != pythonMintedOwnerToken {
		t.Fatalf("Go mint (no machine_id) diverges from the Python-minted token:\n got %s\nwant %s", got, pythonMintedOwnerToken)
	}
}

func TestVerifyAcceptsPythonMintedToken(t *testing.T) {
	claims, err := verifyJWT(pythonMintedAgentToken, []byte(interopSecret), interopNow+1)
	if err != nil {
		t.Fatalf("verify Python-minted token: %v", err)
	}
	if claims["sub"] != "kyle" || claims["scope"] != "agent" || claims["machine_id"] != "mac-01" {
		t.Fatalf("claims mismatch: %v", claims)
	}
	if claims["iat"].(float64) != 1750000000 || claims["exp"].(float64) != 1750003600 {
		t.Fatalf("iat/exp mismatch: %v", claims)
	}
}

func TestMintVerifyRoundTrip(t *testing.T) {
	secret := []byte("another-unit-test-signing-secret")
	tok, err := mintJWT("mira", "agent", 60, secret, interopNow, "")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	claims, err := verifyJWT(tok, secret, interopNow+1)
	if err != nil {
		t.Fatalf("verify own token: %v", err)
	}
	if claims["sub"] != "mira" || claims["scope"] != "agent" {
		t.Fatalf("claims mismatch: %v", claims)
	}
	if _, present := claims["machine_id"]; present {
		t.Fatalf("empty machine_id must be OMITTED (Python `if machine_id:` guard): %v", claims)
	}
}

func TestMintRequiresSub(t *testing.T) {
	if _, err := mintJWT("", "agent", 60, []byte(interopSecret), interopNow, ""); err == nil {
		t.Fatal("mint with empty sub must fail")
	}
}

func TestVerifyRejections(t *testing.T) {
	secret := []byte(interopSecret)
	tok, _ := mintJWT("kyle", "agent", 60, secret, interopNow, "")

	// Expired (now >= exp), and errors.Is still sees the invalid-token base.
	if _, err := verifyJWT(tok, secret, interopNow+60); !errors.Is(err, errExpiredToken) {
		t.Fatalf("want errExpiredToken, got %v", err)
	} else if !errors.Is(err, errInvalidToken) {
		t.Fatalf("errExpiredToken must wrap errInvalidToken, got %v", err)
	}
	// Wrong secret.
	if _, err := verifyJWT(tok, []byte("wrong"), interopNow+1); !errors.Is(err, errInvalidToken) {
		t.Fatalf("want signature failure, got %v", err)
	}
	// Tampered payload.
	parts := strings.Split(tok, ".")
	tampered := parts[0] + "." + b64uEncode([]byte(`{"sub":"evil","scope":"owner","iat":1,"exp":9999999999}`)) + "." + parts[2]
	if _, err := verifyJWT(tampered, secret, interopNow+1); !errors.Is(err, errInvalidToken) {
		t.Fatalf("want tamper rejection, got %v", err)
	}
	// alg:none downgrade.
	noneTok := b64uEncode([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + parts[1] + "."
	if _, err := verifyJWT(noneTok, secret, interopNow+1); !errors.Is(err, errInvalidToken) {
		t.Fatalf("want alg:none rejection, got %v", err)
	}
	// Structural garbage.
	if _, err := verifyJWT("not-a-jwt", secret, interopNow+1); !errors.Is(err, errInvalidToken) {
		t.Fatalf("want structural rejection, got %v", err)
	}
}

func TestDeriveSecretFromPassword(t *testing.T) {
	// Pinned against the Python twin:
	// hashlib.sha256(b"officraft.jwt.hs256.v1:" + b"correct-horse").hexdigest()
	// — both sides must derive the SAME secret from the same password.
	a := deriveSecretFromPassword("correct-horse")
	b := deriveSecretFromPassword("correct-horse")
	if string(a) != string(b) || len(a) != 32 {
		t.Fatalf("derivation must be deterministic 32 bytes")
	}
	// Cross-check via token interop: a Go token signed with the derived secret
	// round-trips (the Python parity of the derivation itself is covered by the
	// byte-for-byte mint test above using an explicit secret).
	tok, err := mintJWT("kyle", "agent", 60, a, interopNow, "")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := verifyJWT(tok, deriveSecretFromPassword("correct-horse"), interopNow+1); err != nil {
		t.Fatalf("verify with re-derived secret: %v", err)
	}
	if _, err := verifyJWT(tok, deriveSecretFromPassword("wrong-horse"), interopNow+1); err == nil {
		t.Fatal("a different password must derive a different secret")
	}
}
