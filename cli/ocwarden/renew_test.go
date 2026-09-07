package main

import (
	"reflect"
	"testing"
	"time"
)

const (
	credIssuedAtMillion    = "header.eyJzdWIiOiJ3YXJkZW4tMSIsImlhdCI6MTAwMDAwMH0.sig"
	credNoIatNoExp         = "header.eyJzdWIiOiJ3YXJkZW4tMSJ9.sig"
	credIatZero            = "header.eyJzdWIiOiJ3YXJkZW4tMSIsImlhdCI6MH0.sig"
	credIatNegative        = "header.eyJzdWIiOiJ3YXJkZW4tMSIsImlhdCI6LTV9.sig"
	credIatString          = "header.eyJzdWIiOiJ3YXJkZW4tMSIsImlhdCI6IjEwMDAwMDAifQ.sig"
	credExpiringWithIat    = "header.eyJzdWIiOiJ3YXJkZW4tMSIsImlhdCI6MTAwMDAwMCwiZXhwIjoxMDAzMDAwfQ.sig"
	credExpiringNoIat      = "header.eyJzdWIiOiJ3YXJkZW4tMSIsImV4cCI6MTAwMzAwMH0.sig"
	credIatAfterExpiry     = "header.eyJzdWIiOiJ3YXJkZW4tMSIsImlhdCI6MTAwNDAwMCwiZXhwIjoxMDAzMDAwfQ.sig"
	credTwoSegments        = "header.eyJzdWIiOiJ3YXJkZW4tMSJ9"
	credUndecodablePayload = "header.!!!not-base64!!!.sig"
	credPayloadNotAnObject = "header.WyJhbiIsImFycmF5Il0.sig"
	credFreshForWardenOne  = "header.eyJzdWIiOiJ3YXJkZW4tMSIsImlhdCI6MzE2MDAwMH0.sig"
	credFreshForWardenTwo  = "header.eyJzdWIiOiJ3YXJkZW4tMiIsImlhdCI6MzE2MDAwMH0.sig"
)

func TestCredentialRenewAfter(t *testing.T) {
	cases := []struct {
		name       string
		lifetime   int64
		machineID  string
		want       time.Duration
		wantJitter time.Duration
	}{
		{"no answer from the station falls back to the shipped 30 days", 0, "warden-1",
			1728000000000000 + 159408867406, 159408867406},
		{"a lifetime under the one-day floor is discarded, not clamped", 24*60*60 - 1, "warden-1",
			1728000000000000 + 159408867406, 159408867406},
		{"a lifetime over the 400-day ceiling is discarded, not clamped", 400*24*60*60 + 1, "warden-1",
			1728000000000000 + 159408867406, 159408867406},
		{"a negative lifetime is discarded", -1, "warden-1",
			1728000000000000 + 159408867406, 159408867406},
		{"the floor itself is honoured", 24 * 60 * 60, "warden-1",
			57600000000000 + 159408867406, 159408867406},
		{"the ceiling itself is honoured", 400 * 24 * 60 * 60, "warden-1",
			23040000000000000 + 159408867406, 159408867406},
		{"an unconfigured machine gets no stagger at all", 0, "",
			1728000000000000, 0},
		{"a different machine gets a different stagger from the same lifetime", 0, "machine-7",
			1728000000000000 + 121649784170, 121649784170},
	}
	for _, c := range cases {
		got := credentialRenewAfter(c.lifetime, c.machineID)
		if got != c.want {
			t.Errorf("%s: credentialRenewAfter(%d, %q) = %s (%d ns), want %s (%d ns)",
				c.name, c.lifetime, c.machineID, got, got, c.want, c.want)
		}
		if jitter := got - (c.want - c.wantJitter); jitter != c.wantJitter {
			t.Errorf("%s: stagger = %s, want %s", c.name, jitter, c.wantJitter)
		}
	}

	if a, b := credentialRenewAfter(0, "warden-1"), credentialRenewAfter(0, "warden-1"); a != b {
		t.Errorf("the same machine got %s then %s — the stagger must be stable across restarts", a, b)
	}
}

func TestCredentialRenewJitter(t *testing.T) {
	cases := []struct {
		name      string
		machineID string
		base      time.Duration
		want      time.Duration
	}{
		{"an empty id gets no stagger", "", 20 * 24 * time.Hour, 0},
		{"the window is one hour when the threshold is generous", "a", 20 * 24 * time.Hour, 2000555641996},
		{"a base of exactly eight hours still allows the whole hour", "a", 8 * time.Hour, 2000555641996},
		{"a threshold under eight hours caps the stagger at an eighth of it", "a", time.Hour, 200555641996},
		{"a zero threshold leaves no room for a stagger", "a", 0, 0},
		{"a negative threshold leaves no room either", "a", -time.Hour, 0},
	}
	for _, c := range cases {
		got := credentialRenewJitter(c.machineID, c.base)
		if got != c.want {
			t.Errorf("%s: credentialRenewJitter(%q, %s) = %s (%d ns), want %s (%d ns)",
				c.name, c.machineID, c.base, got, got, c.want, c.want)
		}
	}
}

func TestCredentialDueForRenewal(t *testing.T) {
	issued := time.Unix(1000000, 0)
	cases := []struct {
		name       string
		token      string
		now        time.Time
		renewAfter time.Duration
		want       bool
	}{
		{"a credential younger than the threshold is not due", credIssuedAtMillion,
			issued.Add(29 * 24 * time.Hour), 30 * 24 * time.Hour, false},
		{"a credential exactly at the threshold is due", credIssuedAtMillion,
			issued.Add(30 * 24 * time.Hour), 30 * 24 * time.Hour, true},
		{"a credential past the threshold is due", credIssuedAtMillion,
			issued.Add(31 * 24 * time.Hour), 30 * 24 * time.Hour, true},
		{"a zero threshold disables the age arm", credIssuedAtMillion,
			issued.Add(365 * 24 * time.Hour), 0, false},
		{"a negative threshold disables the age arm", credIssuedAtMillion,
			issued.Add(365 * 24 * time.Hour), -time.Hour, false},
		{"an expiry-less credential the age arm cannot judge is never due", credNoIatNoExp,
			issued.Add(365 * 24 * time.Hour), 30 * 24 * time.Hour, false},
		{"a zero iat is not a timestamp, so no arm fires", credIatZero,
			issued.Add(365 * 24 * time.Hour), 30 * 24 * time.Hour, false},
		{"a negative iat is not a timestamp either", credIatNegative,
			issued.Add(365 * 24 * time.Hour), 30 * 24 * time.Hour, false},
		{"a non-numeric iat is not read", credIatString,
			issued.Add(365 * 24 * time.Hour), 30 * 24 * time.Hour, false},
		{"an unreadable token is never due", "not-a-token",
			issued.Add(365 * 24 * time.Hour), 30 * 24 * time.Hour, false},
		{"an empty token is never due", "",
			issued.Add(365 * 24 * time.Hour), 30 * 24 * time.Hour, false},
		{"the expiry arm fires with over two thirds of the lifetime spent", credExpiringWithIat,
			time.Unix(1002001, 0), 0, true},
		{"the expiry arm holds with under two thirds spent", credExpiringWithIat,
			time.Unix(1001999, 0), 0, false},
		{"an exp with no iat falls back to the expiry moment itself", credExpiringNoIat,
			time.Unix(1002999, 0), 0, false},
		{"an exp with no iat is due once the moment has passed", credExpiringNoIat,
			time.Unix(1003000, 0), 0, true},
		{"an iat that does not precede exp falls back to the expiry moment", credIatAfterExpiry,
			time.Unix(1002999, 0), 0, false},
		{"the age arm answers even when the expiry arm would not", credIssuedAtMillion,
			issued.Add(21 * 24 * time.Hour), 20 * 24 * time.Hour, true},
	}
	for _, c := range cases {
		if got := credentialDueForRenewal(c.token, c.now, c.renewAfter); got != c.want {
			t.Errorf("%s: credentialDueForRenewal(now=%d, renewAfter=%s) = %v, want %v",
				c.name, c.now.Unix(), c.renewAfter, got, c.want)
		}
	}
}

func TestJwtIssuedAt(t *testing.T) {
	cases := []struct {
		name   string
		token  string
		want   int64
		wantOK bool
	}{
		{"a positive iat is read", credIssuedAtMillion, 1000000, true},
		{"an iat beside an exp is read", credExpiringWithIat, 1000000, true},
		{"no iat claim", credNoIatNoExp, 0, false},
		{"a zero iat is refused", credIatZero, 0, false},
		{"a negative iat is refused", credIatNegative, 0, false},
		{"a string iat is refused", credIatString, 0, false},
		{"a two-segment token is malformed", credTwoSegments, 0, false},
		{"an undecodable payload", credUndecodablePayload, 0, false},
		{"a payload that is not an object", credPayloadNotAnObject, 0, false},
		{"an empty token", "", 0, false},
	}
	for _, c := range cases {
		got, ok := jwtIssuedAt(c.token)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: jwtIssuedAt = (%d, %v), want (%d, %v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}

func TestJwtLifetime(t *testing.T) {
	cases := []struct {
		name    string
		token   string
		wantExp int64
		wantIat int64
		wantOK  bool
	}{
		{"exp and iat together", credExpiringWithIat, 1003000, 1000000, true},
		{"an exp with no iat reads iat as zero", credExpiringNoIat, 1003000, 0, true},
		{"an iat later than exp is still reported verbatim", credIatAfterExpiry, 1003000, 1004000, true},
		{"no exp claim is not a guess", credIssuedAtMillion, 0, 0, false},
		{"no claims at all", credNoIatNoExp, 0, 0, false},
		{"a two-segment token is malformed", credTwoSegments, 0, 0, false},
		{"an undecodable payload", credUndecodablePayload, 0, 0, false},
		{"an empty token", "", 0, 0, false},
	}
	for _, c := range cases {
		exp, iat, ok := jwtLifetime(c.token)
		if exp != c.wantExp || iat != c.wantIat || ok != c.wantOK {
			t.Errorf("%s: jwtLifetime = (%d, %d, %v), want (%d, %d, %v)",
				c.name, exp, iat, ok, c.wantExp, c.wantIat, c.wantOK)
		}
	}
}

func TestJwtClaimsOf(t *testing.T) {
	claims, ok := jwtClaimsOf(credExpiringWithIat)
	if !ok {
		t.Fatal("jwtClaimsOf refused a well-formed token")
	}
	want := map[string]any{"sub": "warden-1", "iat": float64(1000000), "exp": float64(1003000)}
	if !reflect.DeepEqual(claims, want) {
		t.Errorf("claims = %#v, want %#v", claims, want)
	}

	for _, c := range []struct {
		name  string
		token string
	}{
		{"an empty token", ""},
		{"one segment", "header"},
		{"two segments", credTwoSegments},
		{"four segments", credExpiringWithIat + ".extra"},
		{"an undecodable payload", credUndecodablePayload},
		{"a payload that is not an object", credPayloadNotAnObject},
		{"standard base64 padding, which is not raw URL encoding", "header.eyJzdWIiOiJ3YXJkZW4tMSJ9==.sig"},
	} {
		if claims, ok := jwtClaimsOf(c.token); ok || claims != nil {
			t.Errorf("%s: jwtClaimsOf = (%#v, %v), want (nil, false)", c.name, claims, ok)
		}
	}
}
