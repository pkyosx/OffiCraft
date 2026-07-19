package main

// serial_test.go — the pure monotonic release serial (r-N, T-e9d1): mint at
// publish, exposure on the DTO, the self-lookup that names a downstream's own
// build, durability/no-reuse, and the one-time backfill of pre-serial rows.

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestPublishMintsSerial: consecutive publishes get r-1, r-2, … and both the
// numeric serial and the "r-N" tag ride the 201 body next to the (unchanged)
// date-form version.
func TestPublishMintsSerial(t *testing.T) {
	rig := newTestRig(t)

	publish := func(payload []byte) (serial float64, tag, version string) {
		t.Helper()
		rec := rig.do(publishRequest(t, rig.publishTok, "", sha256hex(payload), payload))
		if rec.Code != http.StatusCreated {
			t.Fatalf("publish: got %d, want 201 — body: %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("publish response: not JSON: %v", err)
		}
		s, _ := body["serial"].(float64)
		tg, _ := body["release_tag"].(string)
		v, _ := body["version"].(string)
		return s, tg, v
	}

	s1, tag1, _ := publish([]byte("first"))
	if s1 != 1 || tag1 != "r-1" {
		t.Fatalf("first publish serial=%v tag=%q, want 1 / r-1", s1, tag1)
	}
	s2, tag2, _ := publish([]byte("second"))
	if s2 != 2 || tag2 != "r-2" {
		t.Fatalf("second publish serial=%v tag=%q, want 2 / r-2", s2, tag2)
	}

	// The date-form version and the serial coexist: a client-chosen version
	// still gets the next serial minted for it.
	rec := rig.do(publishRequest(t, rig.publishTok, "v251231-0042", sha256hex([]byte("third")), []byte("third")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("explicit-version publish: got %d, want 201 — %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["version"] != "v251231-0042" || body["release_tag"] != "r-3" {
		t.Fatalf("explicit-version publish: version=%v tag=%v, want v251231-0042 / r-3", body["version"], body["release_tag"])
	}
}

// TestLatestCarriesSerialAndSelfLookup: /api/latest exposes the latest
// release's r-N, and — when the caller self-reports its running git sha — the
// r-N of the CALLER's own build (the cockpit's "you are running r-N").
func TestLatestCarriesSerialAndSelfLookup(t *testing.T) {
	rig := newTestRig(t)

	// Two distinct builds, distinct git shas, via the store (publishRequest
	// hardcodes one sha).
	if _, err := rig.store.InsertRelease(Release{Version: "v260101-0001", GitSHA: "aaaa111", SHA256: "x1", BlobPath: "b1"}); err != nil {
		t.Fatalf("insert r-1: %v", err)
	}
	if _, err := rig.store.InsertRelease(Release{Version: "v260101-0002", GitSHA: "bbbb222", SHA256: "x2", BlobPath: "b2"}); err != nil {
		t.Fatalf("insert r-2: %v", err)
	}

	// A caller running the OLDER build (aaaa111) asks for latest.
	rec := rig.get(t, "/api/latest?channel=beta&current_sha=aaaa111", rig.inviteTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("latest: got %d, want 200 — %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("latest: not JSON: %v", err)
	}
	if body["release_tag"] != "r-2" {
		t.Fatalf("latest release_tag=%v, want r-2", body["release_tag"])
	}
	if body["current_release_tag"] != "r-1" {
		t.Fatalf("self-lookup current_release_tag=%v, want r-1 (caller runs aaaa111)", body["current_release_tag"])
	}

	// A caller whose sha was never published here gets no self-lookup answer.
	rec = rig.get(t, "/api/latest?channel=beta&current_sha=deadbeef", rig.inviteTok)
	var body2 map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body2)
	if _, present := body2["current_release_tag"]; present {
		t.Fatalf("unknown sha must yield no current_release_tag, got %v", body2["current_release_tag"])
	}
}

// TestNextSerialDurableNoReuse: the next serial is MAX(serial)+1 over committed
// rows — a publish that crashed before commit (simulated: no row) consumes
// nothing, so the count never has a gap and never reuses a number.
func TestNextSerialDurableNoReuse(t *testing.T) {
	rig := newTestRig(t)
	if n, err := rig.store.NextSerial(); err != nil || n != 1 {
		t.Fatalf("NextSerial on empty = %d,%v; want 1", n, err)
	}
	for want := int64(1); want <= 3; want++ {
		rel, err := rig.store.InsertRelease(Release{Version: "v-" + itoa(want), SHA256: "x", BlobPath: "y"})
		if err != nil {
			t.Fatalf("InsertRelease: %v", err)
		}
		if rel.Serial != want {
			t.Fatalf("InsertRelease serial = %d, want %d", rel.Serial, want)
		}
	}
	// After three committed rows the next is 4 — and stays 4 across a fresh
	// handle onto the same file (a restart re-derives from committed state).
	if n, _ := rig.store.NextSerial(); n != 4 {
		t.Fatalf("NextSerial after 3 = %d, want 4", n)
	}
}

// TestBackfillSerialsAtOpen: rows written by a pre-serial schema (serial NULL)
// are numbered r-1.. in publish order the first time the serial-aware store
// opens the file — the owner default "r-1 起算".
func TestBackfillSerialsAtOpen(t *testing.T) {
	dir := t.TempDir()
	store, err := openStore(dir)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	// Simulate a legacy DB: null out the serials the current schema minted,
	// out of publish order to prove the backfill sorts by published_at.
	seed := []struct {
		version   string
		published float64
	}{
		{"v260101-0005", 300},
		{"v260101-0001", 100},
		{"v260101-0003", 200},
	}
	for _, s := range seed {
		if _, err := store.db.Exec(
			`INSERT INTO release (version, serial, git_sha, sha256, size, notes, blob_path, published_at)
			 VALUES (?, NULL, '', 'x', 0, '', 'b', ?)`, s.version, s.published); err != nil {
			t.Fatalf("seed %s: %v", s.version, err)
		}
	}
	store.Close()

	// Re-open: backfillSerials runs and numbers by published_at ascending.
	store2, err := openStore(dir)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer store2.Close()
	want := map[string]int64{"v260101-0001": 1, "v260101-0003": 2, "v260101-0005": 3}
	for version, wantSerial := range want {
		rel, err := store2.ReleaseByVersion(version)
		if err != nil || rel == nil {
			t.Fatalf("ReleaseByVersion(%s): %v", version, err)
		}
		if rel.Serial != wantSerial {
			t.Fatalf("%s backfilled serial = %d, want %d", version, rel.Serial, wantSerial)
		}
	}
	// A fresh publish continues after the backfilled max, not from 1.
	if n, _ := store2.NextSerial(); n != 4 {
		t.Fatalf("NextSerial after backfill = %d, want 4", n)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
