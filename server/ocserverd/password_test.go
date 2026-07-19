package main

import (
	"strings"
	"testing"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	phc, err := hashPassword("correct-horse")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !strings.HasPrefix(phc, "$argon2id$v=19$") {
		t.Fatalf("PHC argon2id shape expected: %q", phc)
	}
	if strings.Contains(phc, "correct-horse") {
		t.Fatal("the plaintext must never appear in the hash")
	}
	if !verifyPassword("correct-horse", phc) {
		t.Fatal("the hashed password must verify")
	}
	if verifyPassword("wrong-horse", phc) {
		t.Fatal("a wrong password must not verify")
	}
	// A fresh salt every hash: two hashes of the same password differ, both verify.
	phc2, err := hashPassword("correct-horse")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if phc2 == phc {
		t.Fatal("hashes must be salted (two hashes of one password must differ)")
	}
	if !verifyPassword("correct-horse", phc2) {
		t.Fatal("the second hash must verify too")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	for _, phc := range []string{
		"",
		"plaintext",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$AAAA",  // wrong variant
		"$argon2id$v=18$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$AAAA", // wrong version
		"$argon2id$v=19$m=0,t=0,p=0$c2FsdHNhbHRzYWx0c2FsdA$AAAA",     // zero params
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$AAAA",                    // bad base64 salt
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$",     // empty key
	} {
		if verifyPassword("pw", phc) {
			t.Fatalf("malformed hash must not verify: %q", phc)
		}
	}
}
