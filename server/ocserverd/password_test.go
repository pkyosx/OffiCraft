// Skeleton generated from server/ocserverd/password.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestHashPassword(t *testing.T) {
	t.Skip("TODO: hashPassword produces a PHC-format argon2id string ($argon2id$v=19$m=…,t=…,p=…$<b64 salt>$<b64 key>; unpadded standard base64 per the PHC spec).")
}

func TestVerifyPassword(t *testing.T) {
	t.Skip("TODO: verifyPassword checks password against a PHC argon2id string (parameters are read from the string, so a future cost bump keeps verifying old hashes).")
}
