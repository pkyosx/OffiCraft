// Skeleton generated from server/ocserverd/password.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"strings"
	"testing"
)

func TestHashPassword(t *testing.T) {
	phc, err := hashPassword("correct-horse")
	if err != nil {
		t.Fatalf("hashPassword returned an error: %v", err)
	}
	parts := strings.Split(phc, "$")
	if len(parts) != 6 {
		t.Fatalf("hashPassword returned %q, want six PHC segments", phc)
	}
	if parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=19456,t=2,p=1" {
		t.Fatalf("hashPassword returned unexpected PHC parameters: %q", phc)
	}
	if parts[4] == "" || parts[5] == "" {
		t.Fatalf("hashPassword returned an empty salt or key: %q", phc)
	}
	if strings.Contains(phc, "correct-horse") {
		t.Fatalf("hashPassword returned the plaintext in %q", phc)
	}
	if !verifyPassword("correct-horse", phc) {
		t.Fatal("the hashed password must verify")
	}
	if verifyPassword("wrong-horse", phc) {
		t.Fatal("a wrong password must not verify")
	}
	phc2, err := hashPassword("correct-horse")
	if err != nil {
		t.Fatalf("second hashPassword returned an error: %v", err)
	}
	if phc2 == phc {
		t.Fatal("two hashes of one password must use different salts")
	}
	if !verifyPassword("correct-horse", phc2) {
		t.Fatal("the second salted hash must verify")
	}
}

func TestVerifyPassword(t *testing.T) {
	for _, tc := range []struct {
		name string
		phc  string
	}{
		{name: "empty input", phc: ""},
		{name: "plaintext is not a PHC hash", phc: "plaintext"},
		{name: "the argon2i variant is refused", phc: "$argon2i$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$AAAA"},
		{name: "an unsupported version is refused", phc: "$argon2id$v=18$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$AAAA"},
		{name: "zero cost parameters are refused", phc: "$argon2id$v=19$m=0,t=0,p=0$c2FsdHNhbHRzYWx0c2FsdA$AAAA"},
		{name: "an invalid salt is refused", phc: "$argon2id$v=19$m=19456,t=2,p=1$!!!$AAAA"},
		{name: "an invalid key is refused", phc: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$!!!"},
		{name: "an empty key is refused", phc: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if verifyPassword("pw", tc.phc) {
				t.Fatalf("verifyPassword accepted malformed PHC %q", tc.phc)
			}
		})
	}
}
