package main

import (
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func TestSingleKeyring(t *testing.T) {
	secret := []byte("legacy secret")
	got := singleKeyring(secret)
	if got.activeKeyID() != legacyKeyID {
		t.Fatalf("active key id = %q, want %q", got.activeKeyID(), legacyKeyID)
	}
	if !reflect.DeepEqual(got.signingSecret(), secret) {
		t.Fatalf("signing secret = %q, want %q", got.signingSecret(), secret)
	}
	want := []signingKey{{ID: legacyKeyID, Key: secret}}
	if candidates := got.verifyCandidates(); !reflect.DeepEqual(candidates, want) {
		t.Fatalf("verify candidates = %#v, want %#v", candidates, want)
	}
	empty := singleKeyring(nil)
	if empty.activeKeyID() != "" || empty.signingSecret() != nil || len(empty.verifyCandidates()) != 0 {
		t.Fatalf("empty single keyring = %#v", empty)
	}
}

func TestNewKeyID(t *testing.T) {
	first, err := newKeyID()
	if err != nil {
		t.Fatalf("newKeyID: %v", err)
	}
	second, err := newKeyID()
	if err != nil {
		t.Fatalf("second newKeyID: %v", err)
	}
	for _, id := range []string{first, second} {
		if len(id) != len("k-")+16 || id[:2] != "k-" {
			t.Fatalf("key id = %q, want k- plus 16 hex characters", id)
		}
		if _, err := hex.DecodeString(id[2:]); err != nil {
			t.Fatalf("key id suffix %q is not hex: %v", id[2:], err)
		}
	}
	if first == second {
		t.Fatalf("two key ids collided: %q", first)
	}
}

func TestNewSigningKeyBytes(t *testing.T) {
	key, err := newSigningKeyBytes()
	if err != nil {
		t.Fatalf("newSigningKeyBytes: %v", err)
	}
	if len(key) != jwtKeyBytes {
		t.Fatalf("key length = %d, want %d", len(key), jwtKeyBytes)
	}
	allZero := true
	for _, b := range key {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("new signing key is all zero bytes")
	}
}

func TestSigningSecret(t *testing.T) {
	old := []byte("old")
	active := []byte("active")
	got := newKeyring([]signingKey{
		{ID: "old", Key: old},
		{ID: "active", Key: active},
	}, "active")
	if secret := got.signingSecret(); !reflect.DeepEqual(secret, active) {
		t.Fatalf("signing secret = %q, want %q", secret, active)
	}
	if secret := newKeyring([]signingKey{{ID: "old", Key: old}}, "missing").signingSecret(); secret != nil {
		t.Fatalf("missing active key returned %q, want nil", secret)
	}
	if secret := newKeyring(nil, "").signingSecret(); secret != nil {
		t.Fatalf("empty ring returned %q, want nil", secret)
	}
}

func TestVerifyCandidates(t *testing.T) {
	old := signingKey{ID: "old", Key: []byte("old")}
	active := signingKey{ID: "active", Key: []byte("active")}
	newer := signingKey{ID: "newer", Key: []byte("newer")}
	got := newKeyring([]signingKey{old, active, newer}, active.ID).verifyCandidates()
	want := []signingKey{active, old, newer}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("verify candidates = %#v, want %#v", got, want)
	}
	noActive := newKeyring([]signingKey{old, active}, "missing").verifyCandidates()
	if !reflect.DeepEqual(noActive, []signingKey{old, active}) {
		t.Fatalf("candidates without active key = %#v, want original order", noActive)
	}
}

func TestVerifySecrets(t *testing.T) {
	old := []byte("old")
	active := []byte("active")
	got := newKeyring([]signingKey{
		{ID: "old", Key: old},
		{ID: "active", Key: active},
	}, "active").verifySecrets()
	want := [][]byte{active, old}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("verify secrets = %#v, want %#v", got, want)
	}
}

func TestActiveKeyID(t *testing.T) {
	if got := newKeyring([]signingKey{{ID: "active", Key: []byte("key")}}, "active").activeKeyID(); got != "active" {
		t.Fatalf("active key id = %q, want active", got)
	}
	if got := newKeyring(nil, "").activeKeyID(); got != "" {
		t.Fatalf("empty ring active key id = %q, want empty", got)
	}
}

func TestLoadKeyring(t *testing.T) {
	t.Run("adopts legacy secret once", func(t *testing.T) {
		d := newAPITestDAL(t)
		legacy := []byte("legacy secret")
		first, err := loadKeyring(d, legacy)
		if err != nil {
			t.Fatalf("load legacy keyring: %v", err)
		}
		firstID := first.activeKeyID()
		if firstID == "" || firstID == legacyKeyID {
			t.Fatalf("legacy active key id = %q, want persisted random id", firstID)
		}
		if got := first.snapshot(); len(got) != 1 || got[0].CreatedTS != 0 || !got[0].IsSigning {
			t.Fatalf("legacy snapshot = %#v, want one unknown-age signing key", got)
		}
		second, err := loadKeyring(d, []byte("a different legacy secret"))
		if err != nil {
			t.Fatalf("reload keyring: %v", err)
		}
		if second.activeKeyID() != firstID || !reflect.DeepEqual(second.signingSecret(), legacy) {
			t.Fatalf("reload = id %q secret %q, want id %q secret %q", second.activeKeyID(), second.signingSecret(), firstID, legacy)
		}
	})

	t.Run("keeps empty install empty", func(t *testing.T) {
		d := newAPITestDAL(t)
		got, err := loadKeyring(d, nil)
		if err != nil {
			t.Fatalf("load empty keyring: %v", err)
		}
		if got.activeKeyID() != "" || len(got.verifyCandidates()) != 0 {
			t.Fatalf("empty keyring = %#v", got)
		}
	})

	t.Run("loads explicit persisted ring", func(t *testing.T) {
		d := newAPITestDAL(t)
		keys := []signingKey{
			{ID: "k-old", Key: []byte("old"), CreatedTS: 10},
			{ID: "k-current", Key: []byte("current"), CreatedTS: 20},
		}
		if err := persistRing(d, keys, "k-current"); err != nil {
			t.Fatalf("persist ring: %v", err)
		}
		got, err := loadKeyring(d, []byte("ignored"))
		if err != nil {
			t.Fatalf("load explicit ring: %v", err)
		}
		if !reflect.DeepEqual(got.snapshot(), []keyMeta{
			{ID: "k-old", CreatedTS: 10},
			{ID: "k-current", CreatedTS: 20, IsSigning: true},
		}) {
			t.Fatalf("snapshot = %#v", got.snapshot())
		}
		if !reflect.DeepEqual(got.verifyCandidates(), []signingKey{keys[1], keys[0]}) {
			t.Fatalf("verify candidates = %#v", got.verifyCandidates())
		}
	})

	for _, tc := range []struct {
		name   string
		keys   string
		active string
	}{
		{name: "malformed json", keys: "{"},
		{name: "empty ring", keys: "[]"},
		{name: "invalid key encoding", keys: `[{"id":"k-a","key":"!","created_ts":0}]`},
		{name: "missing key id", keys: `[{"id":"","key":"AQ","created_ts":0}]`},
		{name: "active id not present", keys: `[{"id":"k-a","key":"AQ","created_ts":0}]`, active: "k-b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newAPITestDAL(t)
			if err := d.PutSetting(settingJWTKeys, tc.keys); err != nil {
				t.Fatalf("PutSetting keys: %v", err)
			}
			if tc.active != "" {
				if err := d.PutSetting(settingJWTActiveKeyID, tc.active); err != nil {
					t.Fatalf("PutSetting active id: %v", err)
				}
			}
			if _, err := loadKeyring(d, nil); err == nil {
				t.Fatal("loadKeyring succeeded for invalid persisted ring")
			}
		})
	}
}

func TestPersist(t *testing.T) {
	d := newAPITestDAL(t)
	keys := []signingKey{
		{ID: "k-old", Key: []byte("old"), CreatedTS: 1},
		{ID: "k-active", Key: []byte("active"), CreatedTS: 2},
	}
	kr := newKeyring(keys, "k-active")
	if err := kr.persist(d); err != nil {
		t.Fatalf("persist: %v", err)
	}
	rawKeys, err := d.GetSetting(settingJWTKeys)
	if err != nil {
		t.Fatalf("read persisted keys: %v", err)
	}
	if rawKeys == nil || *rawKeys == "" {
		t.Fatalf("persisted keys = %#v, want nonempty JSON", rawKeys)
	}
	rawActive, err := d.GetSetting(settingJWTActiveKeyID)
	if err != nil {
		t.Fatalf("read persisted active id: %v", err)
	}
	if rawActive == nil || *rawActive != "k-active" {
		t.Fatalf("persisted active id = %#v, want k-active", rawActive)
	}
	loaded, err := loadKeyring(d, nil)
	if err != nil {
		t.Fatalf("load persisted ring: %v", err)
	}
	if !reflect.DeepEqual(loaded.verifyCandidates(), []signingKey{keys[1], keys[0]}) {
		t.Fatalf("loaded candidates = %#v", loaded.verifyCandidates())
	}
}

func TestPersistRing(t *testing.T) {
	d := newAPITestDAL(t)
	keys := []signingKey{{ID: "k-a", Key: []byte("a"), CreatedTS: 3}}
	if err := persistRing(d, keys, "k-a"); err != nil {
		t.Fatalf("persistRing: %v", err)
	}
	loaded, err := loadKeyring(d, nil)
	if err != nil {
		t.Fatalf("load persisted ring: %v", err)
	}
	if loaded.activeKeyID() != "k-a" || !reflect.DeepEqual(loaded.signingSecret(), []byte("a")) {
		t.Fatalf("loaded ring = %#v, want k-a signing with a", loaded)
	}
}

func TestRotate(t *testing.T) {
	d := newAPITestDAL(t)
	old := signingKey{ID: "k-old", Key: []byte("old"), CreatedTS: 10}
	kr := newKeyring([]signingKey{old}, old.ID)
	meta, err := kr.rotate(d)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if meta.ID == "" || meta.ID == old.ID || !meta.IsSigning || meta.CreatedTS <= 0 {
		t.Fatalf("rotation meta = %#v, want fresh signing key metadata", meta)
	}
	if kr.activeKeyID() != meta.ID {
		t.Fatalf("active key id = %q, want %q", kr.activeKeyID(), meta.ID)
	}
	if got := kr.snapshot(); len(got) != 2 || got[0] != (keyMeta{ID: old.ID, CreatedTS: old.CreatedTS}) || got[1] != meta {
		t.Fatalf("rotated snapshot = %#v", got)
	}
	candidates := kr.verifyCandidates()
	if len(candidates) != 2 || candidates[0].ID != meta.ID || candidates[1].ID != old.ID || len(candidates[0].Key) != jwtKeyBytes {
		t.Fatalf("rotated candidates = %#v", candidates)
	}
	loaded, err := loadKeyring(d, nil)
	if err != nil {
		t.Fatalf("load rotated ring: %v", err)
	}
	if !reflect.DeepEqual(loaded.snapshot(), kr.snapshot()) || !reflect.DeepEqual(loaded.verifyCandidates(), kr.verifyCandidates()) {
		t.Fatalf("loaded rotated ring = %#v / %#v, want %#v / %#v", loaded.snapshot(), loaded.verifyCandidates(), kr.snapshot(), kr.verifyCandidates())
	}
}

func TestRemove(t *testing.T) {
	d := newAPITestDAL(t)
	old := signingKey{ID: "k-old", Key: []byte("old"), CreatedTS: 10}
	active := signingKey{ID: "k-active", Key: []byte("active"), CreatedTS: 20}
	kr := newKeyring([]signingKey{old, active}, active.ID)
	if err := kr.persist(d); err != nil {
		t.Fatalf("persist initial ring: %v", err)
	}
	if err := kr.remove(d, active.ID); !errors.Is(err, errRemoveSigningKey) {
		t.Fatalf("remove active error = %v, want %v", err, errRemoveSigningKey)
	}
	if got := kr.snapshot(); !reflect.DeepEqual(got, []keyMeta{
		{ID: old.ID, CreatedTS: old.CreatedTS},
		{ID: active.ID, CreatedTS: active.CreatedTS, IsSigning: true},
	}) {
		t.Fatalf("ring after refused active removal = %#v", got)
	}
	if err := kr.remove(d, "k-missing"); !errors.Is(err, errUnknownKey) {
		t.Fatalf("remove unknown error = %v, want %v", err, errUnknownKey)
	}
	if err := kr.remove(d, old.ID); err != nil {
		t.Fatalf("remove retired key: %v", err)
	}
	if got := kr.snapshot(); !reflect.DeepEqual(got, []keyMeta{{ID: active.ID, CreatedTS: active.CreatedTS, IsSigning: true}}) {
		t.Fatalf("ring after retired removal = %#v", got)
	}
	loaded, err := loadKeyring(d, nil)
	if err != nil {
		t.Fatalf("load ring after removal: %v", err)
	}
	if !reflect.DeepEqual(loaded.snapshot(), kr.snapshot()) {
		t.Fatalf("persisted ring after removal = %#v, want %#v", loaded.snapshot(), kr.snapshot())
	}
}
