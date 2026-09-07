// Skeleton generated from cli/ocwarden/renew.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestCredentialRenewAfter(t *testing.T) {
	t.Skip("TODO: credentialRenewAfter turns the station's LIFETIME into the age at which THIS machine renews: two thirds of the lifetime, plus this machine's own stagger.")
}

func TestCredentialRenewJitter(t *testing.T) {
	t.Skip("TODO: credentialRenewJitter is this machine's stable offset within the window.")
}

func TestCredentialDueForRenewal(t *testing.T) {
	t.Skip("TODO: credentialDueForRenewal reports whether the credential should be replaced now.")
}

func TestJwtIssuedAt(t *testing.T) {
	t.Skip("TODO: jwtIssuedAt reads `iat` out of a JWT payload WITHOUT verifying the signature, for the same reason jwtLifetime does not: verification is the server's job and needs a secret this process does not have.")
}

func TestJwtLifetime(t *testing.T) {
	t.Skip("TODO: jwtLifetime reads `exp` and `iat` out of a JWT payload WITHOUT verifying the signature.")
}

func TestJwtClaimsOf(t *testing.T) {
	t.Skip("TODO: jwtClaimsOf decodes the payload segment.")
}
