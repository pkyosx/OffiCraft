package main

// Exactly one key SIGNS, every key in the ring VERIFIES. There is no timer:
// removing a key is a human decision, never an expiry (owner 2026-09-03).
//
// 🔴 The ring is shared BY POINTER — that is the whole hot-reload mechanism. A
// consumer must hold the *keyring and call the accessors per use; caching the
// returned []byte re-creates the restart-to-rotate bug. Limit: this reloads
// within ONE process; a second ocserverd on the same DB learns of a rotation
// only on restart.
//
// 🔴 KEY IDS ARE RANDOM, NEVER DERIVED FROM THE KEY. An install that predates
// the DB secret carries a PASSWORD-DERIVED key (jwt.go deriveSecretFromPassword
// = SHA-256 over the owner password), so publishing any hash of a key hands out
// an offline dictionary attack on that password. Key bytes never reach a
// response, a log or an error message.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"ocserverd/txguard"
)

const (
	settingJWTKeys = "auth.jwt_keys"

	settingJWTActiveKeyID = "auth.jwt_active_key_id"

	jwtKeyBytes = 32
)

// Enforced at mintJWTClaims (jwt.go), the single seam every mint passes through.
var errNoSigningKey = errors.New("signing keyring: no active key")

type signingKey struct {
	ID        string  `json:"id"`
	Key       []byte  `json:"-"`
	CreatedTS float64 `json:"created_ts"`
}

// Deliberately has no field that could carry key material.
type keyMeta struct {
	ID string `json:"key_id"`
	// 0 for a key in use since before the ring existed: render it as "unknown",
	// never as the epoch.
	CreatedTS float64 `json:"created_ts"`
	IsSigning bool    `json:"is_signing"`
}

type keyring struct {
	mu       txguard.RWMutex
	keys     []signingKey
	activeID string
}

func newKeyring(keys []signingKey, activeID string) *keyring {
	return &keyring{keys: keys, activeID: activeID}
}

func singleKeyring(secret []byte) *keyring {
	if len(secret) == 0 {
		return newKeyring(nil, "")
	}
	k := signingKey{ID: legacyKeyID, Key: secret}
	return newKeyring([]signingKey{k}, k.ID)
}

// Only for synthesised rings in TESTS: a real install's pre-ring key gets a
// RANDOM persisted id (loadKeyring), so no id is a function of key material.
const legacyKeyID = "k-legacy"

func newKeyID() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "k-" + hex.EncodeToString(raw), nil
}

func newSigningKeyBytes() ([]byte, error) {
	key := make([]byte, jwtKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

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

// The order (signing key first) is the contract and lives only here —
// verifySecrets derives from it. Two orderings would drift silently: every token
// still verifies and only the reported key id goes wrong.
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

// Takes NO lock of its own: sync.RWMutex is not reentrant, and a second RLock
// could deadlock against a waiting writer (rotate / remove).
func (k *keyring) verifySecrets() [][]byte {
	candidates := k.verifyCandidates()
	out := make([][]byte, 0, len(candidates))
	for _, key := range candidates {
		out = append(out, key.Key)
	}
	return out
}

func (k *keyring) activeKeyID() string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.activeID
}

func (k *keyring) snapshot() []keyMeta {
	k.mu.RLock()
	defer k.mu.RUnlock()
	out := make([]keyMeta, 0, len(k.keys))
	for _, key := range k.keys {
		out = append(out, keyMeta{ID: key.ID, CreatedTS: key.CreatedTS, IsSigning: key.ID == k.activeID})
	}
	return out
}

type storedKey struct {
	ID        string  `json:"id"`
	Key       string  `json:"key"`
	CreatedTS float64 `json:"created_ts"`
}

// The pre-ring key KEEPS its settingJWTSecret row: every already-issued token is
// signed with it, and deleting the row would be a silent mass logout on a
// downgrade.
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
			// Refuse rather than pick one: guessing which key signs would mint tokens
			// under a key the operator did not choose, invisibly until the wrong key was
			// removed.
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
	kr := newKeyring([]signingKey{{ID: id, Key: legacySecret, CreatedTS: 0}}, id)
	if err := kr.persist(d); err != nil {
		return nil, err
	}
	return kr, nil
}

func (k *keyring) persist(d *DAL) error {
	k.mu.RLock()
	keys, active := k.keys, k.activeID
	k.mu.RUnlock()
	return persistRing(d, keys, active)
}

// Takes no lock, so rotate/remove can persist inside their own write-locked
// critical section (sync.RWMutex is not reentrant).
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

// 🔴 DB write FIRST, in-memory swap only on success, write lock held across
// both. Both alternatives were tried and are wrong: memory-first mints under a
// key never persisted (those tokens die at the next restart, silently); releasing
// the lock before persisting lets a rollback undo a concurrent remove.
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
		return keyMeta{}, err
	}
	k.keys, k.activeID = next, id
	return keyMeta{ID: id, CreatedTS: created, IsSigning: true}, nil
}

var errRemoveSigningKey = errors.New("signing keyring: the key that is currently signing cannot be removed — rotate first")

var errUnknownKey = errors.New("signing keyring: no such key")

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
	// DB first under the same lock, as in rotate: a removal applied only in memory
	// comes back at the next restart — a revocation that silently un-revokes.
	if err := persistRing(d, next, k.activeID); err != nil {
		return err
	}
	k.keys = next
	return nil
}
