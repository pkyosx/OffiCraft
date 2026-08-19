package main

// renew_tfc53_test.go — when this machine decides its own credential is due.
//
// The two failure directions are NOT symmetric, and the tests are shaped around
// that. Renewing too eagerly costs one request per poll per machine, fleet-wide.
// Renewing too late costs a machine that cannot come back — on a remote host,
// that means somebody physically going there. So the "never due" arms are the
// load-bearing ones, and each is paired with a control that would catch a
// version of this function that simply always said no.

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// jwtWith builds an unsigned token carrying the given claims. The signature is
// deliberately nonsense: this code path must never verify, and a test that
// signed properly would hide a version of it that started trying to.
func jwtWith(t *testing.T, claims map[string]any) string {
	t.Helper()
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return "aGVhZGVy." + base64.RawURLEncoding.EncodeToString(body) + ".not-a-signature"
}

func TestCredentialDueForRenewal(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	const day = int64(86400)
	lifetime := 30 * day
	iat := now.Unix() - 25*day // issued 25 days ago
	exp := iat + lifetime      // 5 days left, well inside the third

	for _, tc := range []struct {
		name  string
		token string
		want  bool
		why   string
	}{
		{
			name:  "five days left of thirty",
			token: jwtWith(t, map[string]any{"exp": exp, "iat": iat}),
			want:  true,
			why:   "under a third remaining is the whole point of the mechanism",
		},
		{
			name:  "twenty days left of thirty",
			token: jwtWith(t, map[string]any{"exp": now.Unix() + 20*day, "iat": now.Unix() - 10*day}),
			want:  false,
			why: "a healthy machine two thirds through its credential must do nothing — " +
				"renewing here would put the whole fleet on the endpoint every poll",
		},
		{
			name:  "exactly a third left",
			token: jwtWith(t, map[string]any{"exp": now.Unix() + 10*day, "iat": now.Unix() - 20*day}),
			want:  false,
			why:   "the boundary is strictly-below, so the threshold fires one poll later, not one early",
		},
		{
			name:  "no expiry claim at all",
			token: jwtWith(t, map[string]any{"sub": "m-box", "iat": iat}),
			want:  false,
			why: "warden credentials are permanent today: 'no expiry' must read as " +
				"NOTHING TO RENEW. Read the other way, every warden in the fleet renews " +
				"on every poll, forever, and the server mints every time",
		},
		{
			name:  "already expired",
			token: jwtWith(t, map[string]any{"exp": now.Unix() - day, "iat": now.Unix() - 31*day}),
			want:  true,
			why:   "past the expiry is still due — trying and failing beats not trying",
		},
		{
			name:  "malformed token",
			token: "not.a.jwt",
			want:  false,
			why: "a parse failure is not evidence of expiry; a machine whose credential " +
				"this code cannot read is one to leave alone, not to push through a swap",
		},
		{
			name:  "not three segments",
			token: "onlyonesegment",
			want:  false,
			why:   "same as above — no reading, no decision",
		},
		{
			name:  "exp present but iat missing",
			token: jwtWith(t, map[string]any{"exp": now.Unix() + day}),
			want:  false,
			why: "with no iat the ORIGINAL lifetime is unknown, so the fraction has " +
				"nothing to be a fraction of; fall back to the expiry itself, which has " +
				"not passed yet",
		},
		{
			name:  "exp present, iat missing, and expired",
			token: jwtWith(t, map[string]any{"exp": now.Unix() - 1}),
			want:  true,
			why:   "the fallback still fires once the moment has actually passed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := credentialDueForRenewal(tc.token, now); got != tc.want {
				t.Errorf("credentialDueForRenewal = %v, want %v — %s", got, tc.want, tc.why)
			}
		})
	}
}

// TestJwtLifetimeNeverGuesses pins the one property the decision above rests
// on: ok is false unless an expiry was actually read. A version that returned
// a zero expiry with ok=true would make every unreadable credential look
// expired, which is the direction that swaps credentials on machines that were
// fine.
func TestJwtLifetimeNeverGuesses(t *testing.T) {
	for _, tc := range []struct{ name, token string }{
		{"empty", ""},
		{"two segments", "a.b"},
		{"payload is not base64", "a.!!!.c"},
		{"payload is not json", "a." + base64.RawURLEncoding.EncodeToString([]byte("nope")) + ".c"},
		{"no exp claim", jwtWith(t, map[string]any{"sub": "m-box"})},
		{"exp is a string", jwtWith(t, map[string]any{"exp": "soon"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, ok := jwtLifetime(tc.token); ok {
				t.Errorf("jwtLifetime reported ok on %q — every caller reads ok as "+
					"'an expiry was really there', and a false yes here becomes a "+
					"credential swap on a machine that needed none", tc.token)
			}
		})
	}

	// Control: a well-formed token IS read, so the arms above cannot be passing
	// because this function never reports ok at all.
	exp, iat, ok := jwtLifetime(jwtWith(t, map[string]any{"exp": 200, "iat": 100}))
	if !ok || exp != 200 || iat != 100 {
		t.Fatalf("jwtLifetime on a well-formed token = (%d, %d, %v), want (200, 100, true) — "+
			"without this control the refusals above prove nothing", exp, iat, ok)
	}
}

// TestRenewalDoesNotVerifyTheSignature is here because the temptation to "do it
// properly" is real and would break this at the worst moment: the warden has no
// secret, so a version that verified would refuse every real credential and the
// machine would never renew — silently, since refusing to renew looks exactly
// like not being due yet.
func TestRenewalDoesNotVerifyTheSignature(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	token := jwtWith(t, map[string]any{
		"exp": now.Unix() + 86400, "iat": now.Unix() - 29*86400,
	})
	if !strings.HasSuffix(token, ".not-a-signature") {
		t.Fatal("this test's fixture stopped carrying a junk signature, so it no longer " +
			"proves anything about verification")
	}
	if !credentialDueForRenewal(token, now) {
		t.Error("a credential with one day left was not due — if this started failing " +
			"after a change that added signature checking, that is the bug: the warden " +
			"holds no secret and could never verify its own token")
	}
}
