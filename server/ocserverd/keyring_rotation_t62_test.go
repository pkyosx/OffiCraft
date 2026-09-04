package main

// keyring_rotation_t62_test.go — the load-bearing proofs for T-62.
//
// 🔴 EVERY TEST HERE GOES THROUGH THE REAL GATE: a handler built by
// buildHandler, driven by a real HTTP request, exactly as serve() assembles it.
// Calling keyring methods directly would prove the ring works and prove nothing
// about whether it is WIRED, and "a projection nobody calls is not a defence"
// is the specific way this server has been punctured before
// (api_infra_session_anchor_t4235_test.go). The handler is built ONCE per test
// and never rebuilt after a rotation — if any of these pass while the process
// still needs a restart, they are not testing what they say.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// t62Stack is the production seam in miniature: DAL → loadKeyring → apiServer →
// buildHandler, with the SAME ring pointer reaching both halves, as server.go
// does. It returns the live handler, the ring and the DAL.
func t62Stack(t *testing.T, legacySecret []byte) (*httptest.Server, *keyring, *DAL, *apiServer) {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "t62-rotation.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	dal := NewDAL(db)
	if err := seedOutOfBox(dal); err != nil {
		t.Fatalf("seed: %v", err)
	}
	keys, err := loadKeyring(dal, legacySecret)
	if err != nil {
		t.Fatalf("loadKeyring: %v", err)
	}
	api := newAPIServer(dal, NewHub(), keys, 3600, "../..")
	// 🔴 THE PRODUCTION SEAM, not a hand-assembled twin: serve() builds its
	// handler through this same function, so a change that hands the gate a
	// different ring reddens every rotation test below instead of sailing past
	// them (a stack assembled here by hand would be a copy nobody guards).
	h, err := buildAPIHandler(api, dal.GetMember)
	if err != nil {
		t.Fatalf("buildAPIHandler: %v", err)
	}
	api.loopback = h
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, keys, dal, api
}

// probeGate makes a real request through the built handler with this token and
// returns the status. /api/members is an ordinary gated route: 200 = the gate
// accepted the credential, 401 = it did not.
func probeGate(t *testing.T, srv *httptest.Server, token string) int {
	t.Helper()
	req, err := http.NewRequest("GET", srv.URL+"/api/members", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func mintOwnerAt(t *testing.T, kr *keyring, now int64) string {
	t.Helper()
	tok, err := mintJWT(wireOwnerID, "owner", 3600, kr.signingSecret(), now, "")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	return tok
}

// TestT62_RotationTakesEffectWithoutRestart is the ticket's load-bearing claim.
// One handler, built once. After a rotation performed on the LIVE ring:
//   - a token minted before the rotation still passes the real gate, and
//   - a token minted after it is signed by a DIFFERENT key, and also passes.
//
// Nothing is rebuilt and nothing is reloaded between the two probes. If the
// ring were a value copied at boot — the pre-T-62 shape — the second token
// would be signed by a key the gate has never heard of and would 401.
func TestT62_RotationTakesEffectWithoutRestart(t *testing.T) {
	srv, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	now := time.Now().Unix()

	before := mintOwnerAt(t, keys, now)
	beforeKey := append([]byte(nil), keys.signingSecret()...)
	if st := probeGate(t, srv, before); st != http.StatusOK {
		t.Fatalf("PREMISE FAILED: a token minted from the ring must pass the gate before any rotation; got %d", st)
	}

	meta, err := keys.rotate(dal)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if !meta.IsSigning {
		t.Fatalf("rotate must hand back the key it just put in charge of signing")
	}

	// The signing key really moved — otherwise everything below is vacuous.
	if string(keys.signingSecret()) == string(beforeKey) {
		t.Fatalf("after a rotation the ring must sign with a DIFFERENT key")
	}

	after := mintOwnerAt(t, keys, now)
	if st := probeGate(t, srv, after); st != http.StatusOK {
		t.Fatalf("a token minted AFTER the rotation must pass the SAME handler with no restart; got %d", st)
	}
	if st := probeGate(t, srv, before); st != http.StatusOK {
		t.Fatalf("a token minted BEFORE the rotation must keep working — that is the whole point of the transition; got %d", st)
	}
	// And prove the new token is genuinely signed by the new key rather than
	// merely accepted: the OLD key must not verify it.
	if _, err := verifyJWT(after, beforeKey, now); err == nil {
		t.Fatalf("the post-rotation token verifies under the RETIRED key — the ring is still signing with the old one")
	}
}

// TestT62_RetiredKeyVerifiesButNeverSigns is the owner's sentence in test form:
// 「舊的這時只能接收不能 sign new token」.
func TestT62_RetiredKeyVerifiesButNeverSigns(t *testing.T) {
	_, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	retired := append([]byte(nil), keys.signingSecret()...)

	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// It still VERIFIES: it is in the list the gate tries.
	inRing := false
	for _, s := range keys.verifySecrets() {
		if string(s) == string(retired) {
			inRing = true
		}
	}
	if !inRing {
		t.Fatalf("a rotated-out key must stay in the verify set until a human removes it")
	}
	// It never SIGNS again, no matter how many more rotations happen.
	for i := 0; i < 3; i++ {
		if _, err := keys.rotate(dal); err != nil {
			t.Fatalf("rotate %d: %v", i, err)
		}
		if string(keys.signingSecret()) == string(retired) {
			t.Fatalf("the retired key came back as the signing key after rotation %d", i)
		}
	}
}

// TestT62_RemovingAKeyRejectsItsTokensThroughTheRealGate is the AC that says a
// removal must be PROVED, not asserted: a token that only the removed key
// signed is refused by the live handler, with no restart in between.
func TestT62_RemovingAKeyRejectsItsTokensThroughTheRealGate(t *testing.T) {
	srv, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	now := time.Now().Unix()

	oldToken := mintOwnerAt(t, keys, now)
	oldID := keys.snapshot()[0].ID
	if st := probeGate(t, srv, oldToken); st != http.StatusOK {
		t.Fatalf("PREMISE FAILED: the token must be accepted before its key is removed; got %d", st)
	}
	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if st := probeGate(t, srv, oldToken); st != http.StatusOK {
		t.Fatalf("PREMISE FAILED: after rotation and BEFORE removal the old token must still pass; got %d", st)
	}

	if err := keys.remove(dal, oldID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if st := probeGate(t, srv, oldToken); st != http.StatusUnauthorized {
		t.Fatalf("after its signing key was removed the token must be REFUSED by the live gate, got %d — removal is the revocation seam and it is not working", st)
	}
}

// TestT62_TheSigningKeyCannotBeRemoved: removing the key that signs would leave
// the server unable to mint anything. It must be refused, and the ring must be
// unchanged afterwards.
func TestT62_TheSigningKeyCannotBeRemoved(t *testing.T) {
	_, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	activeID := ""
	for _, k := range keys.snapshot() {
		if k.IsSigning {
			activeID = k.ID
		}
	}
	if activeID == "" {
		t.Fatalf("PREMISE FAILED: the ring must have a signing key")
	}
	before := len(keys.snapshot())
	if err := keys.remove(dal, activeID); err != errRemoveSigningKey {
		t.Fatalf("removing the signing key must be refused with errRemoveSigningKey, got %v", err)
	}
	if after := len(keys.snapshot()); after != before {
		t.Fatalf("a refused removal must leave the ring untouched: %d keys before, %d after", before, after)
	}
	if len(keys.signingSecret()) == 0 {
		t.Fatalf("a refused removal must leave the server able to sign")
	}
}

// TestT62_LegacyInstallLoadsUnchanged: an install that has never rotated has
// only the pre-ring auth.jwt_secret row. Its tokens must keep working, its row
// must survive, and the id it is given must be STABLE across boots (a new id
// every boot would make the settings page lie about which key is which).
func TestT62_LegacyInstallLoadsUnchanged(t *testing.T) {
	srv, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	now := time.Now().Unix()

	if got := len(keys.snapshot()); got != 1 {
		t.Fatalf("an install that has never rotated must present exactly ONE key, got %d", got)
	}
	if keys.snapshot()[0].CreatedTS != 0 {
		t.Fatalf("the pre-ring key's creation time was never recorded; it must read 0 (unknown), not an invented time")
	}
	tok := mintOwnerAt(t, keys, now)
	if st := probeGate(t, srv, tok); st != http.StatusOK {
		t.Fatalf("a legacy install must behave exactly as before: got %d", st)
	}
	// The key material is the legacy secret verbatim — no re-mint, no derivation.
	if string(keys.signingSecret()) != interopSecret {
		t.Fatalf("loadKeyring must adopt the existing secret verbatim; a fresh mint here is a silent mass logout")
	}
	// A second load (i.e. the next boot) reads the ring back, keeping the id.
	again, err := loadKeyring(dal, []byte(interopSecret))
	if err != nil {
		t.Fatalf("second loadKeyring: %v", err)
	}
	if again.snapshot()[0].ID != keys.snapshot()[0].ID {
		t.Fatalf("the key id must be persisted, not re-drawn each boot: %q then %q",
			keys.snapshot()[0].ID, again.snapshot()[0].ID)
	}
}

// TestT62_KeyIdsAreNotDerivedFromKeyMaterial guards the password-oracle hazard
// in keyring.go's header: two rings built over the SAME key bytes must not
// produce the same id, because an id that is a function of the key is an
// offline dictionary attack on a password-derived key.
func TestT62_KeyIdsAreNotDerivedFromKeyMaterial(t *testing.T) {
	_, a, _, _ := t62Stack(t, []byte(interopSecret))
	_, b, _, _ := t62Stack(t, []byte(interopSecret))
	if a.snapshot()[0].ID == b.snapshot()[0].ID {
		t.Fatalf("two independent installs holding the same key produced the same key id (%q) — the id is a function of the key material, which turns a password-derived key into an offline dictionary attack",
			a.snapshot()[0].ID)
	}
}

// TestT62_TheRingNeverLeaksKeyMaterial: the outside-safe view and the two error
// paths must never carry key bytes. keyMeta has no field for it by design; this
// pins that the errors do not either.
func TestT62_TheRingNeverLeaksKeyMaterial(t *testing.T) {
	_, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	// A corrupted stored ring must name the KEY ID, never the key.
	if err := dal.PutSetting(settingJWTKeys, `[{"id":"k-bad","key":"!!not-base64!!","created_ts":0}]`); err != nil {
		t.Fatalf("seed corrupt ring: %v", err)
	}
	_, err := loadKeyring(dal, []byte(interopSecret))
	if err == nil {
		t.Fatalf("a ring holding an undecodable key must refuse to load")
	}
	if strings.Contains(err.Error(), "!!not-base64!!") {
		t.Fatalf("the load error quotes the stored key material: %v", err)
	}
}

// ── the failure and refusal paths (added after independent review) ───────────
//
// Every test below covers a claim that survived a mutant in the first pass. The
// pattern in all four is the same and it is the pattern the first pass missed:
// the happy path was driven through the real gate, and the path where something
// REFUSES or FAILS was left to the comment describing it.

// failingDAL returns a DAL whose writes fail, by closing the database under it.
// A closed handle is a real error from the real code path — no injection seam,
// no interface to keep honest.
func failingDAL(t *testing.T) *DAL {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	d := NewDAL(db)
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return d
}

// TestT62_AFailedPersistLeavesTheRingUntouched: the DB is the authority, so a
// rotation that could not be written must not exist in memory either. The
// earlier shape swapped memory first and rolled back — which meant a window
// where the process signed with a key that was never persisted, and (with the
// lock released across the write) a rollback that could silently undo a
// concurrent removal.
func TestT62_AFailedPersistLeavesTheRingUntouched(t *testing.T) {
	srv, keys, _, _ := t62Stack(t, []byte(interopSecret))
	now := time.Now().Unix()
	before := keys.snapshot()
	beforeKey := append([]byte(nil), keys.signingSecret()...)
	tok := mintOwnerAt(t, keys, now)

	if _, err := keys.rotate(failingDAL(t)); err == nil {
		t.Fatalf("a rotation whose DB write fails must report the failure, not succeed quietly")
	}

	after := keys.snapshot()
	if len(after) != len(before) {
		t.Fatalf("a failed rotation changed the ring: %d keys before, %d after", len(before), len(after))
	}
	if string(keys.signingSecret()) != string(beforeKey) {
		t.Fatalf("a failed rotation moved the signing key — the process is now signing with something no restart could recover")
	}
	// And the whole point of that: what was minted before still works, and what
	// is minted now is minted under the same key.
	if st := probeGate(t, srv, tok); st != http.StatusOK {
		t.Fatalf("a failed rotation must not disturb live credentials, got %d", st)
	}
	if st := probeGate(t, srv, mintOwnerAt(t, keys, now)); st != http.StatusOK {
		t.Fatalf("the server must still mint working tokens after a failed rotation, got %d", st)
	}
}

// TestT62_AFailedRemovePersistDoesNotDropTheKey is the same property on the
// other action, and it matters MORE there: a removal applied in memory but not
// on disk is a key that comes BACK at the next restart — a revocation that
// silently un-revokes.
func TestT62_AFailedRemovePersistDoesNotDropTheKey(t *testing.T) {
	srv, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	now := time.Now().Unix()
	oldTok := mintOwnerAt(t, keys, now)
	oldID := keys.snapshot()[0].ID
	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("PREMISE FAILED: rotate: %v", err)
	}

	if err := keys.remove(failingDAL(t), oldID); err == nil {
		t.Fatalf("a removal whose DB write fails must report the failure")
	}
	if len(keys.snapshot()) != 2 {
		t.Fatalf("a failed removal dropped the key from memory: %+v", keys.snapshot())
	}
	// The credential that key signed must still be accepted — a revocation that
	// did not reach the DB has not happened.
	if st := probeGate(t, srv, oldTok); st != http.StatusOK {
		t.Fatalf("a failed removal revoked the credential anyway, got %d — the DB and the gate now disagree", st)
	}
}

// TestT62_ANewShareLinkIsSignedByTheKeyThatSignsNow. Signing a fresh link with
// any OTHER ring key is invisible to the eye and fatal in effect: the link would
// be killed by the next removal of a key the operator regards as long retired,
// silently, for a link minted seconds earlier.
func TestT62_ANewShareLinkIsSignedByTheKeyThatSignsNow(t *testing.T) {
	_, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	retired := append([]byte(nil), keys.signingSecret()...)
	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	sig := shareSigForRing(keys, "att-t62")
	if sig == "" {
		t.Fatalf("PREMISE FAILED: the ring must be able to sign a share link")
	}
	// It verifies under the ring (both keys are in it) …
	if !verifyShareSigAnyKey(keys, "att-t62", sig) {
		t.Fatalf("a freshly minted share sig must verify")
	}
	// … but NOT under the retired key alone, which is what pins it to the
	// current signer rather than merely to "some key in the ring".
	if verifyShareSig(retired, "att-t62", sig) {
		t.Fatalf("a share link minted AFTER the rotation is signed by the RETIRED key — it would die at the next removal of a key the operator thinks is long gone")
	}
}

// TestT62_LoadRefusesWhenTheActiveIdNamesNoKey. The refusal carries the
// strongest comment in keyring.go — guessing which key signs would mint under a
// key the operator did not choose, and the mistake stays invisible until the
// wrong key is removed — and until now nothing held it.
func TestT62_LoadRefusesWhenTheActiveIdNamesNoKey(t *testing.T) {
	_, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	// Positive control: this ring loads cleanly before the active id is broken,
	// so the refusal below is about the id and not about the blob.
	if _, err := loadKeyring(dal, []byte(interopSecret)); err != nil {
		t.Fatalf("PREMISE FAILED: the ring must load before we break it: %v", err)
	}

	if err := dal.PutSetting(settingJWTActiveKeyID, "k-not-in-this-ring"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := loadKeyring(dal, []byte(interopSecret)); err == nil {
		t.Fatalf("an active key id naming no key in the ring must REFUSE to load, not pick one")
	}
}

// TestT62_AnEmptyRingRefusesToMintRatherThanSignUnderNothing. An empty HMAC key
// is a perfectly valid HMAC key, so without a refusal the server would answer
// 200 with a token signed under nothing — and on the warden path that token
// carries no exp at all.
func TestT62_AnEmptyRingRefusesToMintRatherThanSignUnderNothing(t *testing.T) {
	empty := singleKeyring(nil)
	if len(empty.signingSecret()) != 0 {
		t.Fatalf("PREMISE FAILED: this ring must have no signing key")
	}
	if _, err := mintJWT("someone", "owner", 3600, empty.signingSecret(), time.Now().Unix(), ""); !errors.Is(err, errNoSigningKey) {
		t.Fatalf("minting with an empty ring must fail with errNoSigningKey, got %v", err)
	}
	// The exp-less path is the one that mattered: a permanent credential signed
	// under nothing would never expire its way out of existence.
	if _, err := mintJWTWithoutExpiry("m-warden", "agent", empty.signingSecret(), time.Now().Unix(), ""); !errors.Is(err, errNoSigningKey) {
		t.Fatalf("minting a PERMANENT credential with an empty ring must fail with errNoSigningKey, got %v", err)
	}
}
