package main

// server_test.go — the behaviour lock: publish→latest→binary happy path, the
// beta/GA dual-channel semantics (publish→beta, promote→GA, latest defaults
// to GA), 401 on bad/revoked credentials, sha256-mismatch refusal, and the
// install.sh face. Everything runs against a throwaway store in t.TempDir()
// through the real handler (httptest) — no network, no fixed ports.

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testRig is one isolated updater: store + handler + one live credential of
// each kind (plaintexts kept for the requests).
type testRig struct {
	store      *Store
	handler    http.Handler
	publishTok string
	inviteTok  string
	inviteID   int64
}

func newTestRig(t *testing.T) *testRig {
	t.Helper()
	store, err := openStore(t.TempDir())
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	rig := &testRig{store: store, handler: newHandler(store)}

	rig.publishTok = mustMint(t, store, kindPublish, "joey")
	inviteTok, inviteID := mustMintID(t, store, kindInvite, "alice")
	rig.inviteTok, rig.inviteID = inviteTok, inviteID
	return rig
}

func mustMint(t *testing.T, store *Store, kind, name string) string {
	t.Helper()
	tok, _ := mustMintID(t, store, kind, name)
	return tok
}

func mustMintID(t *testing.T, store *Store, kind, name string) (string, int64) {
	t.Helper()
	plaintext, hash, err := mintSecret(kind)
	if err != nil {
		t.Fatalf("mintSecret: %v", err)
	}
	id, err := store.InsertCredential(kind, name, hash)
	if err != nil {
		t.Fatalf("InsertCredential: %v", err)
	}
	return plaintext, id
}

// publishRequest builds the multipart POST /api/publish request. An empty
// version OMITS the field entirely (the auto-generate path).
func publishRequest(t *testing.T, token, version, claimedSHA string, binary []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fields := map[string]string{
		"sha256":  claimedSHA,
		"git_sha": "cafe123",
		"notes":   "test release",
	}
	if version != "" {
		fields["version"] = version
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("WriteField(%s): %v", k, err)
		}
	}
	fw, err := mw.CreateFormFile("binary", "ocserverd")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(binary); err != nil {
		t.Fatalf("write binary part: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req := httptest.NewRequest("POST", "/api/publish", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func (rig *testRig) do(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	rig.handler.ServeHTTP(rec, req)
	return rec
}

func (rig *testRig) get(t *testing.T, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return rig.do(req)
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// promoteRequest builds the JSON POST /api/promote request.
func promoteRequest(t *testing.T, token, version string) *http.Request {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/promote",
		strings.NewReader(`{"version":`+strconv.Quote(version)+`}`))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

// latestBody GETs /api/latest with the given raw query ("" = default channel)
// and decodes the body (any status).
func (rig *testRig) latestBody(t *testing.T, query, token string) (int, map[string]any) {
	t.Helper()
	rec := rig.get(t, "/api/latest"+query, token)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("latest%s: not JSON (%v): %s", query, err, rec.Body.String())
	}
	return rec.Code, body
}

// TestPublishLatestBinaryHappyPath drives the whole loop: publish a fake
// binary, read it back as the beta latest, download byte-identical bytes.
func TestPublishLatestBinaryHappyPath(t *testing.T) {
	rig := newTestRig(t)
	binary := []byte("#!/bin/sh\necho fake ocserverd v1\n")
	digest := sha256hex(binary)

	rec := rig.do(publishRequest(t, rig.publishTok, "v260101-0001", digest, binary))
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish: got %d, want 201 — body: %s", rec.Code, rec.Body.String())
	}

	rec = rig.get(t, "/api/latest?channel=beta", rig.inviteTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("latest: got %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}
	var latest map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &latest); err != nil {
		t.Fatalf("latest: not JSON: %v", err)
	}
	if latest["version"] != "v260101-0001" || latest["sha256"] != digest || latest["git_sha"] != "cafe123" {
		t.Fatalf("latest: wrong payload: %v", latest)
	}
	if _, ok := latest["published_at"].(float64); !ok {
		t.Fatalf("latest: published_at missing/not a number: %v", latest["published_at"])
	}
	// A fresh publish is beta-only: the DTO says so, honestly.
	if latest["channel_ga"] != false || latest["ga_at"] != nil {
		t.Fatalf("fresh publish must be beta-only (channel_ga=false, ga_at=null): %v", latest)
	}

	rec = rig.get(t, "/api/binary?version=v260101-0001", rig.inviteTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("binary: got %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), binary) {
		t.Fatalf("binary: downloaded bytes differ from the published upload")
	}
	if got := rec.Header().Get("X-Checksum-Sha256"); got != digest {
		t.Fatalf("binary: X-Checksum-Sha256 = %q, want %q", got, digest)
	}
}

// TestAuthRefusals locks the 401 face: missing / bogus / cross-kind / revoked
// credentials are all one indistinguishable "no".
func TestAuthRefusals(t *testing.T) {
	rig := newTestRig(t)
	binary := []byte("payload")
	digest := sha256hex(binary)

	// Publish with a wrong token (an invite is NOT a publish credential).
	for _, tok := range []string{"", "ocu-pub-bogus", rig.inviteTok} {
		if rec := rig.do(publishRequest(t, tok, "v260101-0001", digest, binary)); rec.Code != http.StatusUnauthorized {
			t.Fatalf("publish with token %q: got %d, want 401", tok, rec.Code)
		}
	}
	// Nothing may have landed through those refusals.
	if rel, _ := rig.store.LatestRelease(channelBeta); rel != nil {
		t.Fatalf("a refused publish still landed a release: %+v", rel)
	}

	// Read routes with a wrong token (a publish token is NOT an invite).
	for _, tok := range []string{"", "ocu-inv-bogus", rig.publishTok} {
		if rec := rig.get(t, "/api/latest", tok); rec.Code != http.StatusUnauthorized {
			t.Fatalf("latest with token %q: got %d, want 401", tok, rec.Code)
		}
		if rec := rig.get(t, "/api/binary?version=v260101-0001", tok); rec.Code != http.StatusUnauthorized {
			t.Fatalf("binary with token %q: got %d, want 401", tok, rec.Code)
		}
	}

	// A REVOKED invite stops working immediately.
	if ok, err := rig.store.RevokeInvite(rig.inviteID); err != nil || !ok {
		t.Fatalf("RevokeInvite: ok=%v err=%v", ok, err)
	}
	if rec := rig.get(t, "/api/latest", rig.inviteTok); rec.Code != http.StatusUnauthorized {
		t.Fatalf("latest with revoked invite: got %d, want 401", rec.Code)
	}
}

// TestPublishSha256MismatchRefused: the server recomputes the digest and
// refuses a mismatch — nothing is stored, no blob is left behind.
func TestPublishSha256MismatchRefused(t *testing.T) {
	rig := newTestRig(t)
	binary := []byte("real bytes")
	wrongDigest := sha256hex([]byte("different bytes"))

	rec := rig.do(publishRequest(t, rig.publishTok, "v260101-0001", wrongDigest, binary))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatched publish: got %d, want 400 — body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "sha256 mismatch") {
		t.Fatalf("mismatched publish: refusal must say what happened, got: %s", rec.Body.String())
	}
	if rel, _ := rig.store.LatestRelease(channelBeta); rel != nil {
		t.Fatalf("mismatched publish still landed a release: %+v", rel)
	}
	// No blob (verified or temp) may survive the refusal.
	entries, err := os.ReadDir(rig.store.blobsDir())
	if err != nil {
		t.Fatalf("ReadDir blobs: %v", err)
	}
	for _, e := range entries {
		t.Fatalf("mismatched publish left a file in blobs/: %s", e.Name())
	}
}

// TestPublishDuplicateVersionConflict: published versions are immutable — the
// same version string cannot be published twice.
func TestPublishDuplicateVersionConflict(t *testing.T) {
	rig := newTestRig(t)
	binary := []byte("v1 bytes")
	if rec := rig.do(publishRequest(t, rig.publishTok, "v260101-0001", sha256hex(binary), binary)); rec.Code != http.StatusCreated {
		t.Fatalf("first publish: got %d, want 201", rec.Code)
	}
	again := []byte("other bytes")
	if rec := rig.do(publishRequest(t, rig.publishTok, "v260101-0001", sha256hex(again), again)); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate publish: got %d, want 409", rec.Code)
	}
}

// TestInstallScript: a valid invite gets a script pinned to the requested
// channel's latest release (default ga); a bogus code gets the same 401; an
// empty channel is a 404.
func TestInstallScript(t *testing.T) {
	rig := newTestRig(t)

	if rec := rig.get(t, "/install.sh?code="+rig.inviteTok, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("install.sh with empty catalog: got %d, want 404", rec.Code)
	}

	binary := []byte("release bytes")
	digest := sha256hex(binary)
	if rec := rig.do(publishRequest(t, rig.publishTok, "v260101-0007", digest, binary)); rec.Code != http.StatusCreated {
		t.Fatalf("publish: got %d, want 201", rec.Code)
	}

	// The default install channel is GA: a beta-only catalog has nothing to
	// install (honest 404) until either ?channel=beta is asked for or a
	// version is promoted.
	if rec := rig.get(t, "/install.sh?code="+rig.inviteTok, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("install.sh (default ga) with beta-only catalog: got %d, want 404", rec.Code)
	}
	if rec := rig.get(t, "/install.sh?code="+rig.inviteTok+"&channel=nope", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("install.sh with bogus channel: got %d, want 400", rec.Code)
	}
	if rec := rig.get(t, "/install.sh?code="+rig.inviteTok+"&channel=beta", ""); rec.Code != http.StatusOK {
		t.Fatalf("install.sh?channel=beta: got %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}
	if rec := rig.do(promoteRequest(t, rig.publishTok, "v260101-0007")); rec.Code != http.StatusOK {
		t.Fatalf("promote: got %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}

	rec := rig.get(t, "/install.sh?code="+rig.inviteTok, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("install.sh: got %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}
	script := rec.Body.String()
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"/api/binary?version=v260101-0007",
		digest,
		"Bearer " + rig.inviteTok,
		// The pre-fill step: the SAME invite code + the face the client
		// downloaded through, handed to the installed server's local seam.
		"OC_UPDATER_INVITE_CODE='" + rig.inviteTok + "' ./ocserverd set-updater",
		"OC_UPDATER_URL='http://",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh: script lacks %q:\n%s", want, script)
		}
	}

	if rec := rig.get(t, "/install.sh?code=ocu-inv-bogus", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("install.sh with bogus code: got %d, want 401", rec.Code)
	}
	if rec := rig.get(t, "/install.sh", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("install.sh without code: got %d, want 401", rec.Code)
	}
}

// TestBinaryUnknownVersion: precise, human errors on the download route.
func TestBinaryUnknownVersion(t *testing.T) {
	rig := newTestRig(t)
	if rec := rig.get(t, "/api/binary", rig.inviteTok); rec.Code != http.StatusBadRequest {
		t.Fatalf("binary without version: got %d, want 400", rec.Code)
	}
	if rec := rig.get(t, "/api/binary?version=nope", rig.inviteTok); rec.Code != http.StatusNotFound {
		t.Fatalf("binary with unknown version: got %d, want 404", rec.Code)
	}
}

// TestPublishAutoVersion: publishing WITHOUT a version gets a server-generated
// date-form vYYMMDD-NNNN back in the 201 body; the daily serial increments;
// latest/download resolve the generated string exactly.
func TestPublishAutoVersion(t *testing.T) {
	rig := newTestRig(t)

	publishAuto := func(payload []byte) string {
		t.Helper()
		rec := rig.do(publishRequest(t, rig.publishTok, "", sha256hex(payload), payload))
		if rec.Code != http.StatusCreated {
			t.Fatalf("auto-version publish: got %d, want 201 — body: %s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("publish response: not JSON: %v", err)
		}
		version, _ := body["version"].(string)
		if !isValidVersion(version) {
			t.Fatalf("publish response version %q does not match vYYMMDD-NNNN", version)
		}
		return version
	}

	first := publishAuto([]byte("first auto release"))
	prefix := versionPrefix(time.Now())
	if first != prefix+"0001" {
		// Tolerate only a midnight rollover between publish and this check.
		if !strings.HasSuffix(first, "-0001") {
			t.Fatalf("first auto version = %q, want serial 0001 (prefix %s)", first, prefix)
		}
	}

	secondPayload := []byte("second auto release")
	second := publishAuto(secondPayload)
	if second == first {
		t.Fatalf("second auto publish reused version %q", first)
	}
	if strings.HasPrefix(second, first[:len("v260101-")]) && !strings.HasSuffix(second, "-0002") {
		t.Fatalf("second auto version = %q, want the next serial after %q", second, first)
	}

	// latest (beta — publish lands there) reports the generated version;
	// download resolves it exactly.
	rec := rig.get(t, "/api/latest?channel=beta", rig.inviteTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("latest: got %d, want 200 — body: %s", rec.Code, rec.Body.String())
	}
	var latest map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &latest); err != nil {
		t.Fatalf("latest: not JSON: %v", err)
	}
	if latest["version"] != second {
		t.Fatalf("latest version = %v, want %q", latest["version"], second)
	}
	rec = rig.get(t, "/api/binary?version="+second, rig.inviteTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("binary %q: got %d, want 200", second, rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), secondPayload) {
		t.Fatalf("binary %q: downloaded bytes differ from the published upload", second)
	}
}

// TestPublishVersionFormatRefused: a client MAY still choose the version, but
// it must match the date-form shape — anything else is a 400 that teaches the
// omit-it-and-let-the-server-generate path.
func TestPublishVersionFormatRefused(t *testing.T) {
	rig := newTestRig(t)
	binary := []byte("payload")
	digest := sha256hex(binary)

	for _, bad := range []string{
		"v1",            // the old free-string era
		"260101-0001",   // missing the v
		"v2601010001",   // missing the dash
		"v260101-001",   // serial too short
		"v260101-00010", // serial too long
		"v260101-0001x", // trailing junk
	} {
		rec := rig.do(publishRequest(t, rig.publishTok, bad, digest, binary))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("publish version %q: got %d, want 400 — body: %s", bad, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "vYYMMDD-NNNN") {
			t.Fatalf("publish version %q: refusal must teach the shape, got: %s", bad, rec.Body.String())
		}
	}
	if rel, _ := rig.store.LatestRelease(channelBeta); rel != nil {
		t.Fatalf("a refused version format still landed a release: %+v", rel)
	}

	// A well-shaped client-chosen version still publishes fine.
	if rec := rig.do(publishRequest(t, rig.publishTok, "v251231-0042", digest, binary)); rec.Code != http.StatusCreated {
		t.Fatalf("well-shaped explicit version: got %d, want 201 — body: %s", rec.Code, rec.Body.String())
	}
}

// TestMaxDailySerial: the serial allocator ignores rows outside its day and
// any row not matching the strict 4-digit shape.
func TestMaxDailySerial(t *testing.T) {
	rig := newTestRig(t)
	insert := func(version string) {
		t.Helper()
		if _, err := rig.store.InsertRelease(Release{Version: version, SHA256: "x", BlobPath: "y"}); err != nil {
			t.Fatalf("InsertRelease(%s): %v", version, err)
		}
	}
	insert("v260101-0003")
	insert("v260101-0007")
	insert("v260102-0009") // different day
	got, err := rig.store.MaxDailySerial("v260101-")
	if err != nil || got != 7 {
		t.Fatalf("MaxDailySerial(v260101-) = %d, %v; want 7", got, err)
	}
	got, err = rig.store.MaxDailySerial("v260103-")
	if err != nil || got != 0 {
		t.Fatalf("MaxDailySerial(v260103-) = %d, %v; want 0 (empty day)", got, err)
	}
}

// TestLatestEmptyCatalog: latest on a fresh store is an honest 404 on BOTH
// channels.
func TestLatestEmptyCatalog(t *testing.T) {
	rig := newTestRig(t)
	for _, q := range []string{"", "?channel=ga", "?channel=beta"} {
		if rec := rig.get(t, "/api/latest"+q, rig.inviteTok); rec.Code != http.StatusNotFound {
			t.Fatalf("latest%s on empty catalog: got %d, want 404", q, rec.Code)
		}
	}
}

// TestChannelSplitPublishThenPromote locks the dual-channel contract end to
// end over the HTTP face:
//
//	publish v1, promote v1        → both channels answer v1
//	publish v2                    → beta advances to v2, GA STAYS v1
//	promote v2                    → GA advances to v2
//
// plus: /api/latest without ?channel= means GA (the D1 default), an unknown
// channel is a 400, and any published version stays downloadable regardless of
// its channel.
func TestChannelSplitPublishThenPromote(t *testing.T) {
	rig := newTestRig(t)
	publish := func(version string, payload []byte) {
		t.Helper()
		if rec := rig.do(publishRequest(t, rig.publishTok, version, sha256hex(payload), payload)); rec.Code != http.StatusCreated {
			t.Fatalf("publish %s: got %d, want 201 — body: %s", version, rec.Code, rec.Body.String())
		}
	}
	promote := func(version string) map[string]any {
		t.Helper()
		rec := rig.do(promoteRequest(t, rig.publishTok, version))
		if rec.Code != http.StatusOK {
			t.Fatalf("promote %s: got %d, want 200 — body: %s", version, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("promote %s: not JSON: %v", version, err)
		}
		return body
	}
	wantLatest := func(query, version string) {
		t.Helper()
		code, body := rig.latestBody(t, query, rig.inviteTok)
		if code != http.StatusOK || body["version"] != version {
			t.Fatalf("latest%s: got %d %v, want 200 version %s", query, code, body, version)
		}
	}

	publish("v260101-0001", []byte("v1 bytes"))

	// Beta sees the publish immediately; GA (and the bare default) is still
	// honestly empty.
	wantLatest("?channel=beta", "v260101-0001")
	for _, q := range []string{"", "?channel=ga"} {
		if rec := rig.get(t, "/api/latest"+q, rig.inviteTok); rec.Code != http.StatusNotFound {
			t.Fatalf("latest%s before any promote: got %d, want 404 — body: %s", q, rec.Code, rec.Body.String())
		}
	}

	promoted := promote("v260101-0001")
	if promoted["channel_ga"] != true {
		t.Fatalf("promote response must carry channel_ga=true: %v", promoted)
	}
	gaStamp, ok := promoted["ga_at"].(float64)
	if !ok || gaStamp <= 0 {
		t.Fatalf("promote response must carry the ga_at stamp: %v", promoted)
	}
	wantLatest("", "v260101-0001") // bare default = GA
	wantLatest("?channel=ga", "v260101-0001")

	// A second promote is idempotent AND keeps the ORIGINAL stamp.
	if again := promote("v260101-0001"); again["ga_at"] != gaStamp {
		t.Fatalf("re-promote must keep the original ga_at %v, got %v", gaStamp, again["ga_at"])
	}

	// The next publish advances beta only.
	publish("v260101-0002", []byte("v2 bytes"))
	wantLatest("?channel=beta", "v260101-0002")
	wantLatest("", "v260101-0001")
	wantLatest("?channel=ga", "v260101-0001")

	// The beta version's bytes are downloadable even while it is not GA —
	// channel only decides who "latest" is, never what may be fetched.
	if rec := rig.get(t, "/api/binary?version=v260101-0002", rig.inviteTok); rec.Code != http.StatusOK {
		t.Fatalf("binary of a beta-only version: got %d, want 200", rec.Code)
	}

	// Promote v2: GA advances.
	promote("v260101-0002")
	wantLatest("", "v260101-0002")
	wantLatest("?channel=ga", "v260101-0002")

	// Unknown channel value is a 400, not a silent default.
	if rec := rig.get(t, "/api/latest?channel=nightly", rig.inviteTok); rec.Code != http.StatusBadRequest {
		t.Fatalf("latest with unknown channel: got %d, want 400 — body: %s", rec.Code, rec.Body.String())
	}
}

// TestPromoteRefusals: promote needs a PUBLISH token (invite codes are
// read-side), a known version (404 otherwise), and a well-formed body.
func TestPromoteRefusals(t *testing.T) {
	rig := newTestRig(t)
	payload := []byte("bytes")
	if rec := rig.do(publishRequest(t, rig.publishTok, "v260101-0001", sha256hex(payload), payload)); rec.Code != http.StatusCreated {
		t.Fatalf("publish: got %d, want 201", rec.Code)
	}

	for _, tok := range []string{"", "ocu-pub-bogus", rig.inviteTok} {
		if rec := rig.do(promoteRequest(t, tok, "v260101-0001")); rec.Code != http.StatusUnauthorized {
			t.Fatalf("promote with token %q: got %d, want 401", tok, rec.Code)
		}
	}
	// None of those refusals may have stamped GA.
	if rel, _ := rig.store.LatestRelease(channelGA); rel != nil {
		t.Fatalf("a refused promote still stamped GA: %+v", rel)
	}

	if rec := rig.do(promoteRequest(t, rig.publishTok, "v269999-0001")); rec.Code != http.StatusNotFound {
		t.Fatalf("promote of an unknown version: got %d, want 404", rec.Code)
	}
	if rec := rig.do(promoteRequest(t, rig.publishTok, "")); rec.Code != http.StatusBadRequest {
		t.Fatalf("promote without a version: got %d, want 400", rec.Code)
	}
}

// TestGAOrderIsPromoteOrder: an OLDER publish promoted LATER becomes the GA
// latest — GA order is promote order, decoupled from publish order (the
// forward-fix path: roll GA back to a previously published build by promoting
// it now).
func TestGAOrderIsPromoteOrder(t *testing.T) {
	rig := newTestRig(t)
	for _, v := range []string{"v260101-0001", "v260101-0002"} {
		payload := []byte("bytes of " + v)
		if rec := rig.do(publishRequest(t, rig.publishTok, v, sha256hex(payload), payload)); rec.Code != http.StatusCreated {
			t.Fatalf("publish %s: got %d, want 201", v, rec.Code)
		}
	}
	// Promote the NEWER publish first, the older one after.
	for _, v := range []string{"v260101-0002", "v260101-0001"} {
		if rec := rig.do(promoteRequest(t, rig.publishTok, v)); rec.Code != http.StatusOK {
			t.Fatalf("promote %s: got %d, want 200", v, rec.Code)
		}
	}
	code, body := rig.latestBody(t, "?channel=ga", rig.inviteTok)
	if code != http.StatusOK || body["version"] != "v260101-0001" {
		t.Fatalf("GA latest must follow promote order (want v260101-0001): %d %v", code, body)
	}
	// Beta latest stays publish order.
	code, body = rig.latestBody(t, "?channel=beta", rig.inviteTok)
	if code != http.StatusOK || body["version"] != "v260101-0002" {
		t.Fatalf("beta latest must stay publish order (want v260101-0002): %d %v", code, body)
	}
}

// TestGAColumnBackfill: a database created by the PRE-CHANNEL schema (no
// ga_at column) is upgraded in place at open — existing releases read as
// beta-only and promote works from there.
func TestGAColumnBackfill(t *testing.T) {
	dir := t.TempDir()
	// Recreate the old-shape DB exactly as the pre-channel schema did.
	legacy, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "updater.db"))
	if err != nil {
		t.Fatalf("open legacy: %v", err)
	}
	if _, err := legacy.Exec(`CREATE TABLE release (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		version      TEXT    NOT NULL UNIQUE,
		git_sha      TEXT    NOT NULL DEFAULT '',
		sha256       TEXT    NOT NULL,
		size         INTEGER NOT NULL,
		notes        TEXT    NOT NULL DEFAULT '',
		blob_path    TEXT    NOT NULL,
		published_at REAL    NOT NULL
	)`); err != nil {
		t.Fatalf("create legacy release table: %v", err)
	}
	if _, err := legacy.Exec(
		`INSERT INTO release (version, git_sha, sha256, size, notes, blob_path, published_at)
		 VALUES ('v260101-0001', 'cafe123', 'deadbeef', 4, '', 'x', 1.0)`); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	store, err := openStore(dir)
	if err != nil {
		t.Fatalf("openStore over a legacy DB: %v", err)
	}
	defer store.Close()

	rel, err := store.LatestRelease(channelBeta)
	if err != nil || rel == nil || rel.Version != "v260101-0001" || rel.GAAt != nil {
		t.Fatalf("legacy row must read as beta-only: %+v, %v", rel, err)
	}
	if rel, err = store.LatestRelease(channelGA); err != nil || rel != nil {
		t.Fatalf("legacy row must NOT be GA: %+v, %v", rel, err)
	}
	if rel, err = store.PromoteRelease("v260101-0001"); err != nil || rel == nil || rel.GAAt == nil {
		t.Fatalf("promote over a backfilled DB: %+v, %v", rel, err)
	}
}
