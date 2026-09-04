package main

// renew_demand_t80_test.go — the station-demanded credential replacement (T-80).
//
// WHAT THIS FEATURE IS. A warden credential carries no expiry, so the renewal
// path that already exists (renew.go / renewapply.go) never fires: its only
// question is "is this credential running out", and the answer is permanently no.
// That is why removing a retired signing key would today cut every machine off at
// once — nothing would have replaced the credentials it signed. The `renew`
// warden-command is the second reason to renew, raised by the station because it
// is the only party that knows which key signed what (the JWT header is a
// constant; there is no kid to read locally).
//
// WHAT THESE TESTS ARE FOR. The dangerous half of this feature is not "does a
// demand renew" — it is what happens to a demand that could not be satisfied. A
// demand consumed by a failed attempt leaves the machine sitting on the retired
// key until a person notices, and the failure is silent: the station keeps
// counting it as un-converged and nobody can tell that from a machine that is
// merely offline. So the survival of a demand across every failure is asserted
// case by case, not once.

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// demandedHarness is newRenewHarness with the station's demand already raised and
// a credential that is NOWHERE NEAR due. The freshness is the point: it makes the
// expiry rule answer "no", so anything that renews here renewed because of the
// demand and could not have renewed without it.
func demandedHarness(t *testing.T, now time.Time, status int, body map[string]any, transportErr error) *renewHarness {
	t.Helper()
	h := newRenewHarness(t, freshToken(t, now), t.TempDir()+"/exec-warden.tok", now, status, body, transportErr)
	h.u.renewDemanded.Store(true)
	return h
}

func TestMaybeRenewCredential_ADemandRenewsACredentialThatIsNotNearExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	fresh := jwtWith(t, map[string]any{"sub": thisMachine, "iat": now.Unix(), "exp": now.Unix() + 30*86400})
	h := demandedHarness(t, now, http.StatusOK, map[string]any{"token": fresh}, nil)

	if !h.u.maybeRenewCredential() {
		t.Fatal("a demanded renewal must report that the process should re-exec")
	}
	if got := h.written[h.u.tokfilePath]; got != fresh {
		t.Fatalf("the replacement credential was not written: %q", got)
	}
	if !h.logged("the station has asked this machine to replace its credential") {
		t.Fatalf("a demanded renewal must say so in the log; got %v", h.logs)
	}
}

// The control for the test above: identical in every respect except the demand.
// Without it, nothing happens at all — which is what makes the assertion above a
// statement about the demand rather than about the fixture.
func TestMaybeRenewCredential_WithoutADemandACredentialThatIsNotNearExpiryIsLeftAlone(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	fresh := jwtWith(t, map[string]any{"sub": thisMachine, "iat": now.Unix(), "exp": now.Unix() + 30*86400})
	h := newRenewHarness(t, freshToken(t, now), t.TempDir()+"/exec-warden.tok", now,
		http.StatusOK, map[string]any{"token": fresh}, nil)

	if h.u.maybeRenewCredential() {
		t.Fatal("a credential that is neither due nor demanded must not be renewed")
	}
	if h.renewCalls != 0 {
		t.Fatalf("the station was asked for a credential %d times; want 0", h.renewCalls)
	}
	if len(h.written) != 0 {
		t.Fatalf("something was written: %v", h.written)
	}
}

// A satisfied demand must not renew again on the next poll. The station stops
// asking once it observes the new key id, but it observes that on this machine's
// next request — so between the write and that observation there is a window in
// which the flag, if left standing, would mint a credential per poll on every
// machine in the fleet at once.
func TestMaybeRenewCredential_ASatisfiedDemandDoesNotRenewAgain(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	fresh := jwtWith(t, map[string]any{"sub": thisMachine, "iat": now.Unix(), "exp": now.Unix() + 30*86400})
	h := demandedHarness(t, now, http.StatusOK, map[string]any{"token": fresh}, nil)

	h.u.maybeRenewCredential()
	if h.u.renewDemanded.Load() {
		t.Fatal("the demand must be cleared once a replacement has been written")
	}
	first := h.renewCalls
	h.u.maybeRenewCredential()
	if h.renewCalls != first {
		t.Fatalf("the station was asked again after the demand was satisfied (%d → %d)", first, h.renewCalls)
	}
}

// 🔴 THE CENTRE OF THIS FILE. Every way an attempt can fail, asserted to leave the
// demand standing. Each of these is a real path in renewapply.go, and each one
// that dropped the demand would strand this machine on a key that is about to
// stop verifying — while the station, which is counting key ids and not answers,
// would show it as simply not yet converged.
func TestMaybeRenewCredential_ADemandSurvivesEveryFailedAttempt(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	forThisMachine := jwtWith(t, map[string]any{"sub": thisMachine, "iat": now.Unix(), "exp": now.Unix() + 30*86400})

	cases := []struct {
		name  string
		setUp func(*renewHarness)
		h     func(*testing.T) *renewHarness
	}{
		{
			name: "the station is unreachable",
			h: func(t *testing.T) *renewHarness {
				return demandedHarness(t, now, 0, nil, errors.New("dial tcp: connection refused"))
			},
		},
		{
			name: "the station refuses the request",
			h: func(t *testing.T) *renewHarness {
				return demandedHarness(t, now, http.StatusForbidden, nil, nil)
			},
		},
		{
			name: "the station answers 200 with no credential",
			h: func(t *testing.T) *renewHarness {
				return demandedHarness(t, now, http.StatusOK, map[string]any{}, nil)
			},
		},
		{
			name: "what came back is a credential for another machine",
			h: func(t *testing.T) *renewHarness {
				other := jwtWith(t, map[string]any{"sub": "m-someone-else", "iat": now.Unix(), "exp": now.Unix() + 30*86400})
				return demandedHarness(t, now, http.StatusOK, map[string]any{"token": other}, nil)
			},
		},
		{
			name: "the station will not accept the replacement",
			h: func(t *testing.T) *renewHarness {
				h := demandedHarness(t, now, http.StatusOK, map[string]any{"token": forThisMachine}, nil)
				h.verifyStatus = http.StatusUnauthorized
				return h
			},
		},
		{
			name: "the write fails",
			h: func(t *testing.T) *renewHarness {
				h := demandedHarness(t, now, http.StatusOK, map[string]any{"token": forThisMachine}, nil)
				h.writeErr = errors.New("read-only file system")
				return h
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := c.h(t)
			if h.u.maybeRenewCredential() {
				t.Fatal("a failed attempt must not report that the process should re-exec")
			}
			if len(h.written) != 0 {
				t.Fatalf("a failed attempt wrote something: %v", h.written)
			}
			if !h.u.renewDemanded.Load() {
				t.Fatal("the demand was dropped by a failed attempt — this machine would " +
					"stay on the retired key with nothing left to make it try again")
			}
		})
	}
}

// A demand raised while a replacement is already on disk must not start a second
// renewal: this process is the only thing still holding the old credential, and
// the machine is one restart away from the new one.
func TestMaybeRenewCredential_ADemandIsIgnoredWhileAReplacementAwaitsARestart(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	h := demandedHarness(t, now, http.StatusOK, nil, nil)
	h.u.renewedAwaitingRestart = true

	if h.u.maybeRenewCredential() {
		t.Fatal("nothing should happen while a replacement is already on disk")
	}
	if h.renewCalls != 0 {
		t.Fatalf("the station was asked for a credential %d times; want 0", h.renewCalls)
	}
}

// The two guards that refuse to renew AT ALL must also refuse a demanded renewal:
// an explicit OC_TOKEN survives the exec and overrides the file, so writing one
// changes nothing; an unresolvable token path has nowhere safe to write.
func TestMaybeRenewCredential_ADemandDoesNotOverrideTheGuardsThatRefuseToWrite(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	t.Run("OC_TOKEN is set explicitly in the environment", func(t *testing.T) {
		h := demandedHarness(t, now, http.StatusOK, nil, nil)
		h.u.envToken = "an-explicitly-exported-token"
		if h.u.maybeRenewCredential() {
			t.Fatal("renewal must not proceed when OC_TOKEN would override the file")
		}
		if h.renewCalls != 0 {
			t.Fatalf("the station was asked for a credential %d times; want 0", h.renewCalls)
		}
	})

	t.Run("the token file path does not resolve", func(t *testing.T) {
		h := demandedHarness(t, now, http.StatusOK, nil, nil)
		h.u.tokfilePath = ""
		if h.u.maybeRenewCredential() {
			t.Fatal("renewal must not proceed without a resolved token file path")
		}
		if h.renewCalls != 0 {
			t.Fatalf("the station was asked for a credential %d times; want 0", h.renewCalls)
		}
	})
}

// RenewNow must do BOTH halves: without the flag the woken cycle finds nothing
// due and returns; without the wake the demand waits out a whole poll interval.
//
// ⚠️ THIS DOES NOT OBSERVE THE ORDER, and the production comment claims one (flag
// before wake, so the woken cycle is guaranteed to see it). Stated rather than
// faked: with the loop's own goroutine absent there is nothing that can read the
// flag at the instant of the wake, and a reader added to catch it would be racing
// the Store rather than ordering against it. Reversing the two lines therefore
// leaves this test green — what it costs in production is one poll interval of
// delay, not a missed renewal, because the flag stays raised.
func TestRenewNow_RaisesTheDemandAndWakesTheLoop(t *testing.T) {
	u := &updater{kick: make(chan struct{}, 1)}
	u.RenewNow()

	select {
	case <-u.kick:
	default:
		t.Fatal("RenewNow did not wake the poll loop")
	}
	if !u.renewDemanded.Load() {
		t.Fatal("RenewNow did not raise the demand")
	}
}

// Kick is the self-update wake and must stay exactly that: if it also raised the
// demand, every SSE reconnect would renew this machine's credential.
func TestKick_DoesNotDemandACredentialReplacement(t *testing.T) {
	u := &updater{kick: make(chan struct{}, 1)}
	u.Kick()
	if u.renewDemanded.Load() {
		t.Fatal("Kick raised a credential-replacement demand; a reconnect must not renew")
	}
}
