package main

// keyring.go — the signing-key ring: the live, rotatable set of HS256 keys the
// server signs and verifies identity tokens with (T-62).
//
// WHY A RING AND NOT A KEY. Before this, the signing secret was one immutable
// []byte read once at boot (settings.go loadAuthSettings) and copied into two
// places that never looked at the DB again. Replacing it therefore meant a
// restart AND invalidating every token in existence at the same instant. A ring
// separates the two halves of "having a key": exactly ONE key SIGNS, every key
// in the ring VERIFIES. Rotation adds a key and moves the signing mark; the
// tokens already out there keep verifying against the key that signed them
// until a human removes it. There is no timer — removal is a decision, not an
// expiry (owner 2026-09-03).
//
// 🔴 THE RING IS SHARED BY POINTER, AND THAT IS THE WHOLE HOT-RELOAD MECHANISM.
// apiServer holds one (apiServer.keys) and buildHandler hands the SAME pointer
// to every gated route's requireAuth closure. Nothing copies the key bytes out
// and keeps them, so a rotate that swaps the ring's contents under its own lock
// is visible to every signer and verifier in the process on the very next
// request — no restart, no handler rebuild. Anyone adding a new consumer must
// hold the *keyring and call these accessors per use; caching the []byte it
// returns re-creates exactly the bug this file exists to remove.
//
// ⚠️ LIMIT, stated rather than left to be discovered: this is a reload within
// ONE PROCESS. The DB row is the durable authority, but a second ocserverd
// serving the same database does not learn of a rotation until it restarts.
// The deployment is a single serve process; nothing enforces that.
//
// 🔴 KEY IDS ARE RANDOM, NEVER DERIVED FROM THE KEY. Hashing the key to get a
// stable public id would be safe for a 32-byte random key and CATASTROPHIC for
// the other kind: an install that predates the DB secret carries a
// PASSWORD-DERIVED key (jwt.go deriveSecretFromPassword = SHA-256 over the
// owner password), so publishing any hash of it hands out an offline
// dictionary attack on that password. The id is drawn from crypto/rand and
// persisted instead, and it is the ONLY key-identifying value that ever leaves
// this file — the key bytes themselves never reach a response, a log or an
// error message.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

const (
	// settingJWTKeys is the JSON array of every key in the ring, oldest first:
	// [{"id":...,"key":<base64url raw bytes>,"created_ts":<epoch seconds>}].
	// Absent = this install has never rotated; the ring is then synthesised
	// from the single legacy settingJWTSecret row (see loadKeyring).
	settingJWTKeys = "auth.jwt_keys"
	// settingJWTActiveKeyID names the one key that SIGNS. Every key in
	// settingJWTKeys verifies; only this one mints.
	settingJWTActiveKeyID = "auth.jwt_active_key_id"

	// jwtKeyBytes is the size of a freshly minted signing key, matching the
	// fresh-install mint in loadAuthSettings (32 bytes = the HMAC-SHA256 block
	// size, so nothing is wasted and nothing is short).
	jwtKeyBytes = 32
)

// errNoSigningKey means the ring has no key marked for signing — a state the
// server must refuse to mint in rather than silently sign with something else.
//
// It is enforced at mintJWTClaims (jwt.go), the single seam EVERY mint passes
// through, rather than at each caller: this symbol spent one review cycle
// declared, documented and referenced by nothing while two mint paths had no
// check at all. A guard's home is the choke point, not the doc comment.
var errNoSigningKey = errors.New("signing keyring: no active key")

// signingKey is one key in the ring. `Key` is the raw HMAC key; it is the only
// field that must never be serialised anywhere but the settings row.
type signingKey struct {
	ID        string  `json:"id"`
	Key       []byte  `json:"-"`
	CreatedTS float64 `json:"created_ts"`
}

// keyMeta is the OUTSIDE-SAFE view of one key: what the settings page and the
// API are allowed to know. It deliberately has no field that could carry key
// material, so "did I leak the key" is answered by this type's shape rather
// than by remembering to strip a field at each call site.
type keyMeta struct {
	ID string `json:"key_id"`
	// CreatedTS is 0 for the key an install has been using since before the
	// ring existed: its creation time was never recorded, and 0 says exactly
	// that. Callers must render it as "unknown", never as the epoch.
	CreatedTS float64 `json:"created_ts"`
	IsSigning bool    `json:"is_signing"`
}

// keyring is the live ring. Every read and write goes through the mutex; the
// pattern is the one apiServer.settingsMu already establishes for the live
// settings snapshot (api_stub.go) — an in-place update behind accessors, not a
// value copied at boot.
type keyring struct {
	mu       sync.RWMutex
	keys     []signingKey // oldest first
	activeID string
}

// newKeyring builds a ring from an explicit key set. activeID must name one of
// them; it is the caller's job to have checked that (loadKeyring does).
func newKeyring(keys []signingKey, activeID string) *keyring {
	return &keyring{keys: keys, activeID: activeID}
}

// singleKeyring is the one-key ring: the shape every install has until it
// rotates for the first time, and the shape a test wanting "the old single
// secret" asks for. It is NOT a second door into the server — apiServer and
// buildHandler take a *keyring and only a *keyring — it is just a ring with one
// key in it.
func singleKeyring(secret []byte) *keyring {
	if len(secret) == 0 {
		return newKeyring(nil, "")
	}
	k := signingKey{ID: legacyKeyID, Key: secret}
	return newKeyring([]signingKey{k}, k.ID)
}

// legacyKeyID is the id given to a synthesised single-key ring in TESTS and to
// nothing else: loadKeyring mints a RANDOM id for a real install's pre-ring key
// and persists it, precisely so that no id is ever a function of key material.
const legacyKeyID = "k-legacy"

// newKeyID draws a fresh random key id. Random, never derived — see the file
// header for why deriving one from the key would be a password oracle.
func newKeyID() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "k-" + hex.EncodeToString(raw), nil
}

// newSigningKeyBytes mints a fresh random signing key.
func newSigningKeyBytes() ([]byte, error) {
	key := make([]byte, jwtKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// signingSecret returns the key that signs, or nil when the ring is empty.
// 🔴 Callers must call this PER MINT and never hold the result: a value cached
// across a rotation signs with a key the ring has already retired.
func (k *keyring) signingSecret() []byte {
	k.mu.RLock()
	defer k.mu.RUnlock()
	for _, key := range k.keys {
		if key.ID == k.activeID {
			return key.Key
		}
	}
	return nil
}

// verifyCandidates returns every key that may verify a token, the SIGNING key
// first (the common case costs one HMAC). Removing a key from the ring is what
// makes tokens signed by it stop verifying — that is the entire revocation
// mechanism, so nothing here may fall back to a key the ring no longer holds.
//
// It hands back the signingKey VALUES rather than bare bytes because a verifier
// that succeeds has learned something the caller cannot recover afterwards:
// WHICH key it was. T-80 needs that answer (verifyJWTAnyKey reports the id of
// the key that actually verified, so the station can record which key each
// machine's credential is still signed by), and the only place it exists is
// here, inside the loop.
//
// 🔴 THE ORDER IS THE CONTRACT, AND THERE IS EXACTLY ONE COPY OF IT. verifySecrets
// below is DERIVED from this — it is not a second implementation that happens to
// agree today. Two orderings of the same ring is precisely the "same fact, two
// implementations" shape this repo has already been bitten by: they would drift
// silently, because both would still verify every token and only the reported id
// would be wrong.
//
// ⚠️ The returned slice carries KEY MATERIAL (signingKey.Key is the raw HMAC
// key). It has the same handling rules as signingSecret(): call per use, never
// cache, never let it reach a response, a log or an error message. Only the ID
// half is outside-safe.
func (k *keyring) verifyCandidates() []signingKey {
	k.mu.RLock()
	defer k.mu.RUnlock()
	out := make([]signingKey, 0, len(k.keys))
	for _, key := range k.keys {
		if key.ID == k.activeID {
			out = append(out, key)
		}
	}
	for _, key := range k.keys {
		if key.ID != k.activeID {
			out = append(out, key)
		}
	}
	return out
}

// verifySecrets is the bytes-only projection of verifyCandidates, kept because
// sharesig.go and several tests want the keys and have no use for the ids. Its
// signature, its order and its behaviour are unchanged; what changed is that the
// ordering rule now lives in exactly one function.
//
// It takes NO lock of its own — verifyCandidates already took it. sync.RWMutex
// is not reentrant, and a second RLock here could deadlock against a waiting
// writer (rotate / remove hold the write lock across a DB write).
func (k *keyring) verifySecrets() [][]byte {
	candidates := k.verifyCandidates()
	out := make([][]byte, 0, len(candidates))
	for _, key := range candidates {
		out = append(out, key.Key)
	}
	return out
}

// activeKeyID names the key that SIGNS right now, "" when the ring is empty.
//
// It is the outside-safe half of signingSecret(): an id, never key material (see
// the file header on why ids are random and therefore safe to publish). Read it
// PER USE like every other accessor here — a value cached across a rotation
// answers for a key that is no longer the signing one, which is the exact
// question T-80 asks it ("is this machine still on the current key?").
func (k *keyring) activeKeyID() string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.activeID
}

// snapshot is the outside-safe listing: ids, creation times, which one signs.
// Oldest first, matching the stored order.
func (k *keyring) snapshot() []keyMeta {
	k.mu.RLock()
	defer k.mu.RUnlock()
	out := make([]keyMeta, 0, len(k.keys))
	for _, key := range k.keys {
		out = append(out, keyMeta{ID: key.ID, CreatedTS: key.CreatedTS, IsSigning: key.ID == k.activeID})
	}
	return out
}

// ── persistence ──────────────────────────────────────────────────────────────

// storedKey is the on-disk shape. `Key` IS the key material, so this type never
// leaves this file: it is written to the settings row and read back, nothing else.
type storedKey struct {
	ID        string  `json:"id"`
	Key       string  `json:"key"` // base64url of the raw bytes, as settingJWTSecret has always been
	CreatedTS float64 `json:"created_ts"`
}

// loadKeyring reads the ring from the settings store, falling back to the
// single pre-ring key when this install has never rotated.
//
// legacySecret is the value loadAuthSettings resolved for settingJWTSecret
// (which itself already ran the oc.toml → DB migration, so it is never empty on
// a live server). The pre-ring key KEEPS ITS ROW: settingJWTSecret is not
// deleted, because it is the key every already-issued token is signed with and
// removing the row would be a silent mass logout on a downgrade.
//
// Synthesising the ring PERSISTS it (with a fresh random id), so an install's
// first key gets a stable id exactly once rather than a new one every boot.
func loadKeyring(d *DAL, legacySecret []byte) (*keyring, error) {
	raw, err := d.GetSetting(settingJWTKeys)
	if err != nil {
		return nil, err
	}
	if raw != nil && *raw != "" {
		var stored []storedKey
		if err := json.Unmarshal([]byte(*raw), &stored); err != nil {
			return nil, fmt.Errorf("settings %s: not valid JSON: %v", settingJWTKeys, err)
		}
		keys := make([]signingKey, 0, len(stored))
		for _, s := range stored {
			key, err := base64.RawURLEncoding.DecodeString(s.Key)
			if err != nil || len(key) == 0 {
				// The key bytes must not reach the message: name the id only.
				return nil, fmt.Errorf("settings %s: key %q is not valid base64url", settingJWTKeys, s.ID)
			}
			if s.ID == "" {
				return nil, fmt.Errorf("settings %s: a key has no id", settingJWTKeys)
			}
			keys = append(keys, signingKey{ID: s.ID, Key: key, CreatedTS: s.CreatedTS})
		}
		if len(keys) == 0 {
			return nil, fmt.Errorf("settings %s: the ring is empty", settingJWTKeys)
		}
		activeRaw, err := d.GetSetting(settingJWTActiveKeyID)
		if err != nil {
			return nil, err
		}
		active := ""
		if activeRaw != nil {
			active = *activeRaw
		}
		found := false
		for _, key := range keys {
			if key.ID == active {
				found = true
				break
			}
		}
		if !found {
			// Refuse rather than pick one: guessing which key signs would mint
			// tokens under a key the operator did not choose, and the mistake
			// would be invisible until the wrong key was removed.
			return nil, fmt.Errorf("settings %s: %q names no key in %s", settingJWTActiveKeyID, active, settingJWTKeys)
		}
		return newKeyring(keys, active), nil
	}

	if len(legacySecret) == 0 {
		return newKeyring(nil, ""), nil
	}
	id, err := newKeyID()
	if err != nil {
		return nil, err
	}
	// CreatedTS 0 = "in use since before the ring existed". The real creation
	// time was never recorded and inventing one would put a false fact on the
	// settings page.
	kr := newKeyring([]signingKey{{ID: id, Key: legacySecret, CreatedTS: 0}}, id)
	if err := kr.persist(d); err != nil {
		return nil, err
	}
	return kr, nil
}

// persist writes the ring to the settings store. The caller must hold no lock;
// persist takes the read lock itself.
func (k *keyring) persist(d *DAL) error {
	k.mu.RLock()
	keys, active := k.keys, k.activeID
	k.mu.RUnlock()
	return persistRing(d, keys, active)
}

// persistRing writes an EXPLICIT key set, taking no lock of its own — so a
// caller holding the write lock can persist the state it is about to install
// without releasing it. That is what lets rotate/remove be a single critical
// section (see their headers); sync.RWMutex is not reentrant, so a lock-taking
// persist could not be called from inside one.
func persistRing(d *DAL, keys []signingKey, active string) error {
	stored := make([]storedKey, 0, len(keys))
	for _, key := range keys {
		stored = append(stored, storedKey{
			ID:        key.ID,
			Key:       base64.RawURLEncoding.EncodeToString(key.Key),
			CreatedTS: key.CreatedTS,
		})
	}
	blob, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	if err := d.PutSetting(settingJWTKeys, string(blob)); err != nil {
		return err
	}
	return d.PutSetting(settingJWTActiveKeyID, active)
}

// ── the two operator actions ─────────────────────────────────────────────────

// rotate mints a new key, appends it and makes it the signing key. Every key
// already in the ring stays — this is the transition, not the cut-over: tokens
// signed by the outgoing key keep verifying until a human removes it.
//
// 🔴 ORDER IS LOAD-BEARING, AND SO IS THE SINGLE CRITICAL SECTION. The DB write
// happens FIRST and the in-memory swap only on its success, with the write lock
// held across both. Two earlier shapes are wrong and both were tried:
//
//   - memory first, then persist, rolling back on failure: between the swap and
//     the failure the process MINTS under a key that was never persisted, and
//     those tokens die at the next restart with no error anywhere.
//   - the same, but releasing the lock before persisting: the rollback restores
//     a wholesale snapshot captured earlier, so a concurrent remove that
//     completed in between is silently undone in memory, leaving the process
//     signing with a key the DB no longer holds. (Independent review traced the
//     interleaving; owner-only routes make it unlikely, not impossible, and this
//     file explicitly invites future consumers.)
//
// Holding the lock across the DB write costs readers a moment on a
// human-pressed, owner-only action. That is the right trade.
func (k *keyring) rotate(d *DAL) (keyMeta, error) {
	key, err := newSigningKeyBytes()
	if err != nil {
		return keyMeta{}, err
	}
	id, err := newKeyID()
	if err != nil {
		return keyMeta{}, err
	}
	created := nowSecs()

	k.mu.Lock()
	defer k.mu.Unlock()
	next := make([]signingKey, len(k.keys), len(k.keys)+1)
	copy(next, k.keys)
	next = append(next, signingKey{ID: id, Key: key, CreatedTS: created})
	if err := persistRing(d, next, id); err != nil {
		// Nothing to roll back: memory was never touched.
		return keyMeta{}, err
	}
	k.keys, k.activeID = next, id
	return keyMeta{ID: id, CreatedTS: created, IsSigning: true}, nil
}

// errRemoveSigningKey refuses to remove the key that is currently signing —
// doing so would leave the server unable to mint anything at all. Rotate first;
// then the old key becomes removable.
var errRemoveSigningKey = errors.New("signing keyring: the key that is currently signing cannot be removed — rotate first")

// errUnknownKey names a key id the ring does not hold.
var errUnknownKey = errors.New("signing keyring: no such key")

// remove drops one retired key. THIS is the revocation: every token and every
// share-link signature produced under that key stops verifying the moment the
// call returns, with no grace period — which is the point, and why it is a
// human's decision and never a timer's.
func (k *keyring) remove(d *DAL, id string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if id == k.activeID {
		return errRemoveSigningKey
	}
	idx := -1
	for i, key := range k.keys {
		if key.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errUnknownKey
	}
	next := make([]signingKey, 0, len(k.keys)-1)
	next = append(next, k.keys[:idx]...)
	next = append(next, k.keys[idx+1:]...)
	// DB first, under the same lock, for the reasons on rotate above: a removal
	// applied in memory and not on disk is a key that comes BACK at the next
	// restart — a revocation that silently un-revokes.
	if err := persistRing(d, next, k.activeID); err != nil {
		return err
	}
	k.keys = next
	return nil
}
