package main

// api_signing_key_observation_t80_test.go — T-80: the station records WHICH
// signing key each machine's credential is actually signed by, so the owner can
// tell whether it is safe to press 「移除」 on a retired key (an act with no
// grace period at all).
//
// 🔴 EVERY TEST HERE ENTERS THROUGH A REAL HTTP REQUEST on the production
// assembly (t62Stack → buildAPIHandler), never by calling requireAuth,
// verifyJWTAnyKey or the DAL setter directly. That is not stylistic. The failure
// this whole file exists to prevent has already happened once in this repo: a
// feature was proved correct by tests that called its internals, and the ONE
// line connecting it to the live path was unguarded — deleting it left 2716
// tests green. Here that line is the observeTokenKey call inside requireAuth,
// and TestAMachineAuthenticatingRecordsTheKeyThatVerifiedItsCredential is the
// test that dies with it.
//
// Nothing here touches a real machine, a real warden or a real key: temp sqlite,
// a temp ring, httptest.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// t80Warden plants an ACTIVE warden roster row — the only kind of row this
// feature stamps — and hands back the permanent credential a real warden holds.
//
// KindWarden is the same string machineKind names ("warden"); the in-flight
// 'assistant' → 'staff' kind rename does not touch it, so nothing here is
// keyed on the value being renamed.
func t80Warden(t *testing.T, api *apiServer, id, name string) string {
	t.Helper()
	putTestMember(t, api, Member{
		ID: id, Name: name, Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	// mintWardenToken is the production mint for this credential shape: scope
	// "agent", no exp, no machine_id binding. Going through it rather than
	// hand-rolling a token means a change to how warden credentials are signed
	// reaches these tests.
	m, err := api.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("seed warden %s: %v %v", id, m, err)
	}
	tok, err := api.mintWardenToken(*m)
	if err != nil {
		t.Fatalf("mintWardenToken %s: %v", id, err)
	}
	return tok
}

// t80Get makes one real request through the built handler. /api/members is an
// ordinary gated route a live warden genuinely calls (see the liveCall list in
// api_auth_machine_revoke_test.go) and it writes nothing, which the
// write-suppression test below depends on.
// t80Connect opens the machine's LONG-LIVED stream through the real handler and
// then hangs up — the event T-80 records on.
//
// 🔴 IT MUST GO THROUGH REAL HTTP, not HandleEventsApiEventsGet with a
// hand-built context. The whole feature is that requireAuth resolves WHICH key
// verified the credential and hands it down; a test that builds the context
// itself supplies that answer instead of measuring it, and would stay green with
// the middleware unwired entirely.
//
// The observation runs before the stream is established, so a response header is
// already proof it happened; the body is never read and the request is cancelled
// immediately.
func t80Connect2(t *testing.T, base, token string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, ""
	}
	// 🔴 READ THE STREAM, DO NOT POP THE QUEUE. A summons raised by THIS connect
	// is drained onto THIS stream and the pending row is deleted as it is
	// written, so the queue is empty afterwards whether the frame was sent or
	// never existed. Reading the socket is the only measurement that tells those
	// two apart — and it is also the stronger one, because it is the bytes the
	// warden would actually receive.
	var sb strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, rerr := resp.Body.Read(buf)
			sb.Write(buf[:n])
			if rerr != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		cancel()
		<-done
	}
	return resp.StatusCode, sb.String()
}

func t80Get(t *testing.T, url, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	return resp.StatusCode, sb.String()
}

func t80TokenKeyOf(t *testing.T, dal *DAL, id string) string {
	t.Helper()
	m, err := dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("read back %s: %v %v", id, m, err)
	}
	return m.TokenKeyID
}

// ---------------------------------------------------------------------------
// ① the observation happens at all, and it happens ON THE LIVE PATH
// ---------------------------------------------------------------------------

// TestAMachineAuthenticatingRecordsTheKeyThatVerifiedItsCredential is the
// load-bearing test of this ticket.
//
// A machine makes one ordinary authenticated request. Afterwards the station
// knows which signing key that machine's credential is signed by. Before the
// request it knew nothing — that BEFORE arm is inside the test on purpose, so a
// version of this that always passed (or a probe that never authenticated at
// all) cannot go green.
//
// Mutant: delete the `observeTokenKey(claims, keyID)` call in requireAuth
// (server.go) — the feature is then complete, correct and wired to nothing, and
// this test is the thing that goes red.
func TestAMachineAuthenticatingRecordsTheKeyThatVerifiedItsCredential(t *testing.T) {
	srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
	tok := t80Warden(t, api, "m-box", "box-1")

	if got := t80TokenKeyOf(t, dal, "m-box"); got != "" {
		t.Fatalf("PREMISE FAILED: a machine that has never authenticated must "+
			"carry no observation; got %q", got)
	}

	if st, body := t80Connect2(t, srv.URL, tok); st != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED — a live warden credential must pass "+
			"the gate; got %d %s", st, body)
	}

	want := keys.activeKeyID()
	if want == "" {
		t.Fatalf("PREMISE FAILED: the ring must have a signing key")
	}
	if got := t80TokenKeyOf(t, dal, "m-box"); got != want {
		t.Fatalf("after one authenticated request the station must know which "+
			"key verified that machine's credential: member.token_key_id = %q, "+
			"want %q.\nThis is the ONLY source of 「還剩幾台沒換」. If you just "+
			"removed the observeTokenKey call from requireAuth, that is the line "+
			"to restore.", got, want)
	}
}

// TestACredentialTheGateRefusesRecordsNothing is the other half of ①: the
// observation is evidence, so it must come only from a credential that was
// actually ACCEPTED. A token the ring cannot verify must leave the machine's
// row exactly as it was.
//
// Mutant: move the observeTokenKey call in requireAuth above the refusals (or
// have verifyJWTAnyKey report an id on a failure instead of "") and this goes
// red.
func TestACredentialTheGateRefusesRecordsNothing(t *testing.T) {
	srv, _, dal, api := t62Stack(t, []byte(interopSecret))
	t80Warden(t, api, "m-box", "box-1")

	// Signed by a key that is not on the ring at all — a forgery, from the
	// gate's point of view.
	forged, err := mintJWT("m-box", "agent", 3600, []byte("not-a-ring-key-at-all"),
		time.Now().Unix(), "")
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := t80Get(t, srv.URL+"/api/members", forged); st != http.StatusUnauthorized {
		t.Fatalf("PREMISE FAILED: a token signed by a key outside the ring must "+
			"be refused; got %d", st)
	}
	if got := t80TokenKeyOf(t, dal, "m-box"); got != "" {
		t.Fatalf("a REFUSED credential proves nothing about which key a machine "+
			"is on, yet member.token_key_id = %q", got)
	}

	// The second arm is about ORDER, not about signatures. This credential is
	// perfectly signed by a key on the ring — it fails a LATER gate (the roster
	// says the machine is gone, authz.go revocationRefusal). An observation made
	// before that gate would record it anyway, and the owner's count would
	// include a machine that no longer exists.
	revoked := t80Warden(t, api, "m-gone", "gone-box")
	gone, err := dal.GetMember("m-gone")
	if err != nil || gone == nil {
		t.Fatalf("read m-gone: %v %v", gone, err)
	}
	gone.RosterStatus = RosterStatusRemoved
	if err := dal.PutMember(*gone); err != nil {
		t.Fatalf("soft-delete m-gone: %v", err)
	}
	if st, _ := t80Get(t, srv.URL+"/api/members", revoked); st != http.StatusUnauthorized {
		t.Fatalf("PREMISE FAILED: a deleted machine's credential must be refused; got %d", st)
	}
	if got := t80TokenKeyOf(t, dal, "m-gone"); got != "" {
		t.Fatalf("a credential that VERIFIED but was refused by a later gate "+
			"must not be recorded either: member.token_key_id = %q.\n"+
			"The observation belongs AFTER every refusal in requireAuth.", got)
	}
}

// TestARefusedCredentialNeverNamesAKeyOnTheWire pins the property jwt.go's
// header declares: the refusal says a token did not verify, and never anything
// about the ring. Adding a key id to the returned value must not have leaked one
// into the answer.
//
// Mutant: make verifyJWTAnyKey's error mention the candidate id, or have
// requireAuth put keyID on the response, and this goes red.
func TestARefusedCredentialNeverNamesAKeyOnTheWire(t *testing.T) {
	srv, keys, dal, _ := t62Stack(t, []byte(interopSecret))
	// A ring with several keys, so there is more than one id that could leak.
	for i := 0; i < 2; i++ {
		if _, err := keys.rotate(dal); err != nil {
			t.Fatalf("rotate: %v", err)
		}
	}
	ids := []string{}
	for _, meta := range keys.snapshot() {
		ids = append(ids, meta.ID)
	}
	if len(ids) < 3 {
		t.Fatalf("PREMISE FAILED: want a multi-key ring, got %v", ids)
	}

	forged, err := mintJWT("m-nobody", "agent", 3600, []byte("not-a-ring-key-at-all"),
		time.Now().Unix(), "")
	if err != nil {
		t.Fatal(err)
	}
	st, body := t80Get(t, srv.URL+"/api/members", forged)
	if st != http.StatusUnauthorized {
		t.Fatalf("PREMISE FAILED: forged token must be refused; got %d %s", st, body)
	}
	for _, id := range ids {
		if strings.Contains(body, id) {
			t.Fatalf("the refusal names a signing key (%q) — a refusal must say "+
				"that a token did not verify and nothing about the ring: %s", id, body)
		}
	}
}

// ---------------------------------------------------------------------------
// ② the recorded key is the RIGHT one, and it survives a rotation honestly
// ---------------------------------------------------------------------------

// TestAfterARotationOnlyMachinesThatCameBackReadAsOnTheCurrentKey is the
// question the owner actually asks before pressing 「移除」.
//
// Two machines authenticate on the original key. The ring rotates. One is
// re-credentialled; the other KEEPS CALLING ON ITS OLD TOKEN, which is what a
// machine nobody has touched actually does — warden credentials are permanent
// and the old key still verifies, so the requests keep succeeding. On the wire
// the re-credentialled one reads as on the current key and the untouched one
// still names the OLD key and reads as not current.
//
// 🔴 THAT SECOND MACHINE IS THE LOAD-BEARING HALF, and it is why it keeps making
// requests rather than going quiet: a version that recorded "whichever key is
// signing now" instead of "the key that actually verified" would look correct
// for a machine that never calls again, and would quietly mark this one as
// migrated on its very next heartbeat — telling the owner the fleet had moved
// when not one byte of it had. Mutant: return kr.activeKeyID() from
// verifyJWTAnyKey instead of the verifying candidate's id.
func TestAfterARotationOnlyMachinesThatCameBackReadAsOnTheCurrentKey(t *testing.T) {
	srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
	ownerTok := mintOwnerAt(t, keys, time.Now().Unix())

	movedTok := t80Warden(t, api, "m-moved", "moved-box")
	stuckTok := t80Warden(t, api, "m-stuck", "stuck-box")
	oldKey := keys.activeKeyID()

	for who, tok := range map[string]string{"m-moved": movedTok, "m-stuck": stuckTok} {
		if st, body := t80Connect2(t, srv.URL, tok); st != http.StatusOK {
			t.Fatalf("POSITIVE CONTROL FAILED — %s must pass the gate before the "+
				"rotation; got %d %s", who, st, body)
		}
	}

	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	newKey := keys.activeKeyID()
	if newKey == oldKey {
		t.Fatalf("PREMISE FAILED: a rotation must move the signing key")
	}

	// Only m-moved re-credentials. Its old token still verifies (that is the
	// whole point of the ring), so the machine must present a token minted
	// SINCE the rotation for the station to see it on the new key — exactly the
	// real-world sequence.
	moved, err := dal.GetMember("m-moved")
	if err != nil || moved == nil {
		t.Fatalf("read m-moved: %v %v", moved, err)
	}
	reissued, err := api.mintWardenToken(*moved)
	if err != nil {
		t.Fatalf("re-mint: %v", err)
	}
	if st, body := t80Connect2(t, srv.URL, reissued); st != http.StatusOK {
		t.Fatalf("the re-credentialled machine must pass the gate; got %d %s", st, body)
	}

	// m-stuck goes on working, on the credential it has always had. The old key
	// is still on the ring, so this is a 200 — that is the whole point of the
	// ring and the whole reason the owner cannot tell by looking at failures.
	if st, body := t80Connect2(t, srv.URL, stuckTok); st != http.StatusOK {
		t.Fatalf("PREMISE FAILED: a machine on the OUTGOING key must still be "+
			"served (that is what makes this question hard); got %d %s", st, body)
	}

	rows := t80ListMachines(t, srv.URL, ownerTok)
	movedRow, ok := rows["m-moved"]
	if !ok {
		t.Fatalf("GET /api/machines does not list m-moved: %v", rows)
	}
	stuckRow, ok := rows["m-stuck"]
	if !ok {
		t.Fatalf("GET /api/machines does not list m-stuck: %v", rows)
	}

	if movedRow.TokenKeyID == nil || *movedRow.TokenKeyID != newKey {
		t.Fatalf("the machine that came back must read as signed by the CURRENT "+
			"key: token_key_id = %v, want %q", derefStr(movedRow.TokenKeyID), newKey)
	}
	if movedRow.TokenKeyCurrent == nil || !*movedRow.TokenKeyCurrent {
		t.Fatalf("the machine that came back must read as token_key_current=true, got %v",
			derefBool(movedRow.TokenKeyCurrent))
	}
	if stuckRow.TokenKeyID == nil || *stuckRow.TokenKeyID != oldKey {
		t.Fatalf("the machine that did NOT come back must still name the key it "+
			"was last SEEN on (%q), got %v — anything else would tell the owner "+
			"the fleet had migrated when it has not",
			oldKey, derefStr(stuckRow.TokenKeyID))
	}
	if stuckRow.TokenKeyCurrent == nil || *stuckRow.TokenKeyCurrent {
		t.Fatalf("the machine that did NOT come back must read as "+
			"token_key_current=false, got %v", derefBool(stuckRow.TokenKeyCurrent))
	}
}

// TestAMachineThatHasNeverAuthenticatedIsNotCountedEitherWay keeps the third
// state distinguishable. "never seen" is not "still on the old key": one means
// the owner has no information, the other means he has information and it says
// no. Folding either into the other is how a removal gets pressed too early or
// never at all.
func TestAMachineThatHasNeverAuthenticatedIsNotCountedEitherWay(t *testing.T) {
	srv, keys, _, api := t62Stack(t, []byte(interopSecret))
	ownerTok := mintOwnerAt(t, keys, time.Now().Unix())
	t80Warden(t, api, "m-silent", "silent-box")

	rows := t80ListMachines(t, srv.URL, ownerTok)
	row, ok := rows["m-silent"]
	if !ok {
		t.Fatalf("GET /api/machines does not list m-silent: %v", rows)
	}
	if row.TokenKeyID != nil {
		t.Fatalf("a machine this station has never authenticated must report a "+
			"null token_key_id, got %q", *row.TokenKeyID)
	}
	if row.TokenKeyCurrent != nil {
		t.Fatalf("…and no verdict at all on whether it is on the current key, "+
			"got %v", *row.TokenKeyCurrent)
	}
}

// ---------------------------------------------------------------------------
// ③ the observation must not cost a database write per request
// ---------------------------------------------------------------------------

// TestRepeatedRequestsOnAnUnchangedKeyCostNoFurtherWrites is a real constraint,
// not a micro-optimisation: this observation runs on EVERY authenticated request
// on every gated route, and the write pool is ONE connection wide
// (server/CLAUDE.md §7). A write per request would serialise the whole server
// behind a bookkeeping column.
//
// It asks the DATABASE what happened (sqlite total_changes() on the write
// connection) rather than counting calls to a fake, for the reason
// single_column_writes_t14_test.go gives: a test that watched the code path
// would go green on a rewrite that still wrote every time.
//
// Mutant: drop the memo check in noteTokenKeyObservation (call
// SetMemberTokenKeyID unconditionally) and this goes red.
func TestRepeatedRequestsOnAnUnchangedKeyCostNoFurtherWrites(t *testing.T) {
	srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
	tok := t80Warden(t, api, "m-box", "box-1")

	// The FIRST request is the one that legitimately writes.
	if st, body := t80Connect2(t, srv.URL, tok); st != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED — got %d %s", st, body)
	}
	if got := t80TokenKeyOf(t, dal, "m-box"); got == "" {
		t.Fatalf("PREMISE FAILED: the first request must have recorded something")
	}

	before := t80TotalChanges(t, dal)
	const requests = 25
	for i := 0; i < requests; i++ {
		if st, body := t80Connect2(t, srv.URL, tok); st != http.StatusOK {
			t.Fatalf("request %d: got %d %s", i, st, body)
		}
	}
	after := t80TotalChanges(t, dal)
	if after != before {
		t.Fatalf("%d further requests on the SAME key changed %d database rows, "+
			"want 0.\nThe write pool is one connection wide; only a CHANGE of "+
			"observed key may reach the database. If you removed the memo check "+
			"in noteTokenKeyObservation, that is the line to restore.",
			requests, after-before)
	}

	// …and the suppression is not "it never writes again": a genuine change
	// must still land. Without this arm the test above would pass on a version
	// that recorded nothing at all after the first request.
	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	m, err := dal.GetMember("m-box")
	if err != nil || m == nil {
		t.Fatalf("read m-box: %v %v", m, err)
	}
	reissued, err := api.mintWardenToken(*m)
	if err != nil {
		t.Fatalf("re-mint: %v", err)
	}
	if st, body := t80Connect2(t, srv.URL, reissued); st != http.StatusOK {
		t.Fatalf("re-credentialled request: got %d %s", st, body)
	}
	if got, want := t80TokenKeyOf(t, dal, "m-box"), keys.activeKeyID(); got != want {
		t.Fatalf("a CHANGED key must still be recorded: token_key_id = %q, want %q",
			got, want)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// t80TotalChanges reads sqlite's total_changes() on the WRITE connection — the
// count of rows inserted, updated or deleted through it since it was opened.
// Reads do not move it, so it measures exactly the thing this feature must not
// do on every request.
func t80TotalChanges(t *testing.T, d *DAL) int64 {
	t.Helper()
	var n int64
	if err := d.wdb.QueryRow(`SELECT total_changes()`).Scan(&n); err != nil {
		t.Fatalf("total_changes(): %v", err)
	}
	return n
}

type t80MachineRow struct {
	MachineID       string  `json:"machine_id"`
	TokenKeyID      *string `json:"token_key_id"`
	TokenKeyCurrent *bool   `json:"token_key_current"`
}

func t80ListMachines(t *testing.T, base, ownerTok string) map[string]t80MachineRow {
	t.Helper()
	st, body := t80Get(t, base+"/api/machines", ownerTok)
	if st != http.StatusOK {
		t.Fatalf("GET /api/machines: want 200, got %d %s", st, body)
	}
	var rows []t80MachineRow
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("GET /api/machines: %v (%s)", err, body)
	}
	out := map[string]t80MachineRow{}
	for _, r := range rows {
		out[r.MachineID] = r
	}
	return out
}

func derefStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func derefBool(p *bool) any {
	if p == nil {
		return nil
	}
	return *p
}

// TestTheRefusalFromTheRingNeverNamesAKeyIsTheContractOfVerifyJWTAnyKey covers
// what the wire cannot see. requireAuth flattens every cause into one "invalid
// token", so the wire test above proves nothing has leaked TO A CLIENT but
// cannot see the error verifyJWTAnyKey itself hands back — and jwt.go's header
// declares that error must never say WHICH key failed. This is the one
// assertion in this file made below the HTTP surface, because the property
// being asserted is a property of that return value and of nothing else.
//
// Mutant: wrap the per-candidate error with its id before assigning lastErr and
// this goes red.
func TestTheRefusalFromTheRingNeverNamesAKeyIsTheContractOfVerifyJWTAnyKey(t *testing.T) {
	dal := newTestDAL(t)
	keys, err := loadKeyring(dal, []byte(interopSecret))
	if err != nil {
		t.Fatalf("loadKeyring: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := keys.rotate(dal); err != nil {
			t.Fatalf("rotate: %v", err)
		}
	}
	ids := []string{}
	for _, meta := range keys.snapshot() {
		ids = append(ids, meta.ID)
	}
	if len(ids) < 3 {
		t.Fatalf("PREMISE FAILED: want a multi-key ring, got %v", ids)
	}

	now := time.Now().Unix()
	forged, err := mintJWT("m-nobody", "agent", 3600, []byte("not-a-ring-key-at-all"), now, "")
	if err != nil {
		t.Fatal(err)
	}
	claims, keyID, err := verifyJWTAnyKey(keys, forged, now)
	if err == nil {
		t.Fatalf("PREMISE FAILED: a forged token must not verify (claims %v)", claims)
	}
	if keyID != "" {
		t.Fatalf("a FAILED verification must report no key at all, got %q — a key "+
			"id is evidence that a credential was accepted, and this one was not", keyID)
	}
	for _, id := range ids {
		if strings.Contains(err.Error(), id) {
			t.Fatalf("the refusal names key %q. The error must say that a token "+
				"did not verify and NOTHING about the ring — jwt.go's header "+
				"declares this, and a per-key error is what it forbids: %v", id, err)
		}
	}
}

// ---------------------------------------------------------------------------
// ④ the SUMMONS — the station tells a stale machine to go renew (owner ruling A)
// ---------------------------------------------------------------------------
//
// 🔴 THIS BLOCK EXISTS BECAUSE THE LOOP WAS ONCE OPEN. The station could see and
// count which machines were still on the outgoing key, and the warden could act
// on a `renew` verb, and NOBODY SENT ONE — so the count was correct, honest, and
// permanently frozen at "nobody moved". Every test below drives real HTTP and
// then reads the frames that would actually go out on that machine's downlink.

// t80OnlineWarden brings a warden's SSE downlink up so the fail-closed
// reachability gate in enqueueToWarden admits frames. A frame is only ever
// enqueued for a machine the hub says is connected — that is the production
// rule, not a test convenience.
func t80OnlineWarden(t *testing.T, api *apiServer, id string) {
	t.Helper()
	if _, err := api.hub.Connect(id, id); err != nil {
		t.Fatalf("bring %s online: %v", id, err)
	}
	if !api.hub.IsOnline(id) {
		t.Fatalf("PREMISE FAILED: %s must be online for a frame to be enqueued", id)
	}
}

// t80DrainRenews pops that warden's FIFO and returns the frames on it. It uses
// the same drain the SSE connection uses, so what it inspects is byte-for-byte
// what would have been written to the socket.
func t80DrainRenews(t *testing.T, api *apiServer, id string) [][]byte {
	t.Helper()
	out := [][]byte{}
	for _, c := range api.hub.DrainWardenCommands(id) {
		out = append(out, c.Frame)
	}
	return out
}

// t80RenewFramesIn pulls the renew frames out of what a connect actually WROTE
// to the socket.
//
// 🔴 IT REPLACES POPPING THE QUEUE, AND THE REASON IS A BEHAVIOUR CHANGE, NOT A
// STYLE ONE. A summons raised while a machine connects is drained onto THAT
// connection and its pending row is deleted as the bytes go out, so afterwards
// the queue is empty whether the frame was delivered or never existed. Counting
// the queue therefore reports zero for both, which is the measurement failing,
// not the feature. These bytes are what the warden receives.
func t80RenewFramesIn(body string) []string {
	out := []string{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		digest, ok := decodeWardenCommandFrame([]byte(line))
		if !ok || digest.Verb != reconcileCmdRenew {
			continue
		}
		out = append(out, line)
	}
	return out
}

// t80StaleMachine sets up the whole premise in one place: a machine seen on the
// key that is signing now, then a rotation, so its permanent credential is now
// signed by an OUTGOING key while still verifying perfectly.
func t80StaleMachine(t *testing.T, srv *httptest.Server, api *apiServer, keys *keyring,
	dal *DAL, id string) string {
	t.Helper()
	tok := t80Warden(t, api, id, id+"-box")
	t80OnlineWarden(t, api, id)
	if st, body := t80Connect2(t, srv.URL, tok); st != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED — %s must pass the gate before the "+
			"rotation; got %d %s", id, st, body)
	}
	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	// Anything the pre-rotation traffic queued is not what these tests are about.
	t80DrainRenews(t, api, id)
	return tok
}

// TestAMachineStillOnAnOutgoingKeyIsToldToRenew is the load-bearing test of the
// summons half, and the one whose absence left this loop open.
//
// Mutant: delete the enqueueToWarden call in askMachineToRenewIfStale — the
// station still sees, still counts, still reports, and this is what goes red.
func TestAMachineStillOnAnOutgoingKeyIsToldToRenew(t *testing.T) {
	srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
	tok := t80StaleMachine(t, srv, api, keys, dal, "m-stale")

	st, body := t80Connect2(t, srv.URL, tok)
	if st != http.StatusOK {
		t.Fatalf("PREMISE FAILED: the old key is still on the ring, so this must "+
			"be a 200; got %d %s", st, body)
	}

	frames := t80RenewFramesIn(body)
	if len(frames) != 1 {
		t.Fatalf("a machine observed on an OUTGOING key must be told to renew: "+
			"got %d frames, want 1.\nWithout this the station counts correctly "+
			"and the fleet never moves — the number is right and frozen forever.",
			len(frames))
	}
	digest, ok := decodeWardenCommandFrame([]byte(frames[0]))
	if !ok {
		t.Fatalf("the frame is not a warden-command frame: %s", frames[0])
	}
	if digest.Verb != reconcileCmdRenew {
		t.Fatalf("verb = %q, want %q", digest.Verb, reconcileCmdRenew)
	}
	if digest.MemberID != "m-stale" {
		t.Fatalf("the frame must address the machine it is for: member_id = %q",
			digest.MemberID)
	}
}

// TestTheRenewFrameCarriesNoCredentialAtAll is owner ruling A on the wire: the
// station SUMMONS, it never sends. It asserts the BYTES that would reach the
// socket, not the absence of a line of code — a frame shape that gained a token
// field later would be caught here even though nothing about this file changed.
//
// Mutant: give the renew frame a wardenStartArgs-shaped payload (which carries
// MemberToken) instead of wardenTargetArgs, and this goes red.
func TestTheRenewFrameCarriesNoCredentialAtAll(t *testing.T) {
	srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
	tok := t80StaleMachine(t, srv, api, keys, dal, "m-stale")
	st, body := t80Connect2(t, srv.URL, tok)
	if st != http.StatusOK {
		t.Fatalf("PREMISE FAILED: got %d", st)
	}
	frames := t80RenewFramesIn(body)
	if len(frames) != 1 {
		t.Fatalf("PREMISE FAILED: want exactly one frame, got %d", len(frames))
	}
	raw := frames[0]

	// ① No field whose NAME could carry a credential. The renew args shape is
	// member_id and nothing else, so any of these appearing means the payload
	// changed shape.
	for _, forbidden := range []string{
		"member_token", "token", "secret", "key", "persona_context", "credential",
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("the renew frame contains the field name %q — the station "+
				"tells a machine to GO, it never sends it anything (owner ruling "+
				"A). Frame: %s", forbidden, raw)
		}
	}
	// ② And no VALUE that is one, whatever it is called: the machine's own live
	// credential and every key id on the ring.
	if strings.Contains(raw, tok) {
		t.Fatalf("the renew frame carries the machine's own credential: %s", raw)
	}
	for _, meta := range keys.snapshot() {
		if strings.Contains(raw, meta.ID) {
			t.Fatalf("the renew frame names signing key %q: %s", meta.ID, raw)
		}
	}
	// ③ Positive control: the frame really is the renew, so ①/② are not passing
	// against an empty string.
	if !strings.Contains(raw, reconcileCmdRenew) || !strings.Contains(raw, "m-stale") {
		t.Fatalf("POSITIVE CONTROL FAILED — this is not the renew frame: %s", raw)
	}
}

// TestAStaleMachineIsAskedOnceNoMatterHowOftenItCallsBack pins the suppression.
// A stale machine keeps working and keeps making requests; one frame per request
// would be one per heartbeat per machine, forever.
//
// Mutant: delete the claimRenewAsk gate (ask unconditionally) and this goes red
// naming the count.
func TestAStaleMachineIsAskedOnceNoMatterHowOftenItCallsBack(t *testing.T) {
	srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
	tok := t80StaleMachine(t, srv, api, keys, dal, "m-stale")

	const requests = 25
	seen := 0
	for i := 0; i < requests; i++ {
		st, body := t80Connect2(t, srv.URL, tok)
		if st != http.StatusOK {
			t.Fatalf("request %d: %d %s", i, st, body)
		}
		seen += len(t80RenewFramesIn(body))
	}
	if seen != 1 {
		t.Fatalf("%d connects from one stale machine produced %d renew frames, "+
			"want exactly 1. The ask is throttled by the memo, and the QUEUE is "+
			"not that state — a pending row is deleted the moment the frame is "+
			"written, so an empty queue never means 'already asked'.",
			requests, seen)
	}
}

// TestAStaleMachineIsAskedAgainOnceTheIntervalHasPassed is the other half, and
// it is not symmetry for its own sake: the warden's "I was asked" flag is
// process-local and dies with a warden restart, so a one-shot ask is silently
// lost by exactly the machines that most need asking.
//
// Time is moved with the injected clock rather than slept.
//
// Mutant: make the ask fire once and never again (e.g. return early whenever
// renewAskedAt is non-zero) and this goes red.
func TestAStaleMachineIsAskedAgainOnceTheIntervalHasPassed(t *testing.T) {
	srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
	base := time.Now()
	var offset time.Duration
	api.keyRenewClock = func() time.Time { return base.Add(offset) }

	tok := t80StaleMachine(t, srv, api, keys, dal, "m-stale")
	st, body := t80Connect2(t, srv.URL, tok)
	if st != http.StatusOK {
		t.Fatalf("PREMISE FAILED")
	}
	if got := len(t80RenewFramesIn(body)); got != 1 {
		t.Fatalf("PREMISE FAILED: the first ask must have happened, got %d", got)
	}

	// Just short of the interval: still silent.
	offset = renewAskInterval - time.Second
	st, body = t80Connect2(t, srv.URL, tok)
	if st != http.StatusOK {
		t.Fatalf("request inside the interval failed")
	}
	if got := len(t80RenewFramesIn(body)); got != 0 {
		t.Fatalf("inside the interval the machine must not be asked again, got %d frames", got)
	}

	// Past it: asked again.
	offset = renewAskInterval + time.Second
	st, body = t80Connect2(t, srv.URL, tok)
	if st != http.StatusOK {
		t.Fatalf("request past the interval failed")
	}
	frames := t80RenewFramesIn(body)
	if len(frames) != 1 {
		t.Fatalf("past the interval the machine must be asked AGAIN, got %d "+
			"frames. A warden that restarted has forgotten it was ever asked, "+
			"and nothing on that side will remind it.", len(frames))
	}
	if digest, ok := decodeWardenCommandFrame([]byte(frames[0])); !ok || digest.Verb != reconcileCmdRenew {
		t.Fatalf("the re-ask must be the same verb: %s", frames[0])
	}
}

// TestAMachineAlreadyOnTheCurrentKeyIsNeverAsked is the discriminating half. A
// summons that went to everybody would pass every test above while being exactly
// wrong: it would tell an already-migrated fleet to churn its credentials on
// every interval, forever.
//
// The second arm covers the install that has NEVER rotated — a one-key ring must
// summon nobody, and it gets that for free because the key that verified is by
// construction the only key there is.
//
// Mutant: drop the `keyID == active` early return in askMachineToRenewIfStale
// and both arms go red.
func TestAMachineAlreadyOnTheCurrentKeyIsNeverAsked(t *testing.T) {
	t.Run("a machine on the current key", func(t *testing.T) {
		srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
		t80StaleMachine(t, srv, api, keys, dal, "m-fresh")
		// It re-credentials, which is what the summons is for.
		m, err := dal.GetMember("m-fresh")
		if err != nil || m == nil {
			t.Fatalf("read m-fresh: %v %v", m, err)
		}
		reissued, err := api.mintWardenToken(*m)
		if err != nil {
			t.Fatalf("re-mint: %v", err)
		}
		for i := 0; i < 5; i++ {
			if st, body := t80Connect2(t, srv.URL, reissued); st != http.StatusOK {
				t.Fatalf("request %d: %d %s", i, st, body)
			}
		}
		if frames := t80DrainRenews(t, api, "m-fresh"); len(frames) != 0 {
			t.Fatalf("a machine already on the CURRENT key must never be told to "+
				"renew, got %d frames: %s", len(frames), frames[0])
		}
	})

	t.Run("an install that has never rotated", func(t *testing.T) {
		srv, _, _, api := t62Stack(t, []byte(interopSecret))
		tok := t80Warden(t, api, "m-only", "only-box")
		t80OnlineWarden(t, api, "m-only")
		for i := 0; i < 5; i++ {
			if st, body := t80Connect2(t, srv.URL, tok); st != http.StatusOK {
				t.Fatalf("request %d: %d %s", i, st, body)
			}
		}
		if frames := t80DrainRenews(t, api, "m-only"); len(frames) != 0 {
			t.Fatalf("a ring with one key has nothing to migrate to, so nobody "+
				"may be summoned; got %d frames: %s", len(frames), frames[0])
		}
	})
}

// TestEphemeralIdentitiesLeaveNoTraceInTheObservationMemo is a MEMORY-SHAPE
// guard, and it reads an internal field because the property has no other
// surface: a map that grows without bound looks identical from the wire until
// the station is already fat.
//
// The memo is keyed by the token's `sub`. Machines are a roster and keep their
// ids; outsource workers mint a brand-new `ow-…` per ticket and pass through the
// very same gate — memoising them would grow this map monotonically with every
// worker the station has ever run, with nothing reclaiming it.
//
// Mutant: memoise the non-machine branch of noteTokenKeyObservation (an
// `s.rememberTokenKey(sub, …)` before its return) and this goes red.
func TestEphemeralIdentitiesLeaveNoTraceInTheObservationMemo(t *testing.T) {
	srv, keys, _, api := t62Stack(t, []byte(interopSecret))
	t80Warden(t, api, "m-box", "box-1")
	now := time.Now().Unix()

	// Twelve one-shot identities, each authenticating once — the shape a station
	// sees over its lifetime as tickets come and go.
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("ow-ephemeral-%d", i)
		w := testAgent(id)
		putTestMember(t, api, w)
		tok, err := mintJWT(id, "agent", 3600, keys.signingSecret(), now, "")
		if err != nil {
			t.Fatal(err)
		}
		if st, body := t80Connect2(t, srv.URL, tok); st != http.StatusOK {
			t.Fatalf("POSITIVE CONTROL FAILED — %s must authenticate; got %d %s",
				id, st, body)
		}
	}

	api.tokenKeyObsMu.Lock()
	got := len(api.tokenKeyObs)
	keysSeen := make([]string, 0, got)
	for k := range api.tokenKeyObs {
		keysSeen = append(keysSeen, k)
	}
	api.tokenKeyObsMu.Unlock()
	if got != 0 {
		t.Fatalf("twelve one-shot identities left %d memo entries (%v). Only "+
			"MACHINES may be memoised — they are a bounded roster; worker ids are "+
			"minted per ticket and never reused, so memoising them is a leak with "+
			"no ceiling and no owner.", got, keysSeen)
	}
}

// 🔴 THE WIRE STRING IS THE CONTRACT WITH ANOTHER MODULE, SO IT IS PINNED AS A
// LITERAL AND NOT AS THE CONSTANT UNDER TEST.
//
// Every other assertion in this file compares against reconcileCmdRenew, which
// is right for them — they are asking "did the station emit the renew frame",
// and the constant is how you say that. It is exactly wrong for THIS question.
// The reader of this frame is cli/ocwarden, a SEPARATE Go module with its own
// copy of the string (rpcRenew) and no compiler between the two. Rename this
// side and the expected value renames with it: this package stays green, and so
// does the warden's, because its own tests pin its own literal. What actually
// happens is that every renew frame is refused by every warden in the fleet as
// unknown-rpc — logged, skipped, reader loop unharmed, nothing red anywhere, and
// the credentials this whole ticket exists to rotate never move again.
//
// The `update` verb has the same shape and no such test; that gap is why this
// one is written out rather than left to the reviewer to notice.
//
// The literal below MUST stay byte-identical to cli/ocwarden's rpcRenew. If you
// are changing one of them on purpose, change both and say so in the commit.
func TestTheRenewVerbOnTheWireIsTheLiteralTheWardenParses(t *testing.T) {
	const wardenSideLiteral = "renew" // cli/ocwarden/command.go: rpcRenew

	if reconcileCmdRenew != wardenSideLiteral {
		t.Fatalf("the station emits rpc=%q but cli/ocwarden accepts %q — every "+
			"renew frame would be refused as unknown-rpc by every warden, "+
			"silently, and no other test in either module would notice",
			reconcileCmdRenew, wardenSideLiteral)
	}

	// And prove the string actually reaches the wire in that position, rather
	// than only agreeing as a constant: a frame whose verb lives somewhere the
	// warden does not read is the same outage.
	frame, ok := buildTargetFrame(reconcileCmdRenew, "m-somewhere")
	if !ok {
		t.Fatal("buildTargetFrame refused to build a renew frame")
	}
	digest, ok := decodeWardenCommandFrame(frame)
	if !ok {
		t.Fatalf("the renew frame is not a warden-command frame: %s", frame)
	}
	if digest.Verb != wardenSideLiteral {
		t.Fatalf("the frame's rpc field reads %q; the warden reads that field and "+
			"expects %q", digest.Verb, wardenSideLiteral)
	}
}

// 🔴 THE REGRESSION TEST FOR THE DEFECT THIS DESIGN EXISTS TO PREVENT.
//
// The scenario, in the order it happens on a real host (cli/ocwarden/renewapply.go):
//
//  1. the warden asks the station for a fresh credential
//  2. it PRESENTS that candidate on a gated route to check the station accepts it
//  3. only then does it write the credential to disk
//  4. and only then does it exec into it
//
// Step 3 can fail — a read-only filesystem, a full disk — and the warden is
// written to survive that: it keeps the old credential and retries. So the
// machine goes on running on the OUTGOING key.
//
// The first shape of this feature recorded the signing key at the auth gate,
// which meant step 2 alone marked the machine converged. The owner would then be
// looking at a screen saying every machine has moved, press remove — an act with
// no grace period and no undo — and cut off a machine that never adopted the new
// credential at all.
//
// This test is that story with nothing else in it: present a new-key credential
// on an ordinary gated route, write nothing, and require that the station has
// not changed its mind about which key that machine is on.
func TestPresentingACredentialOnAnOrdinaryRouteIsNotEvidenceOfRunningOnIt(t *testing.T) {
	srv, keys, dal, api := t62Stack(t, []byte(interopSecret))
	tok := t80Warden(t, api, "m-probe", "m-probe-box")
	t80OnlineWarden(t, api, "m-probe")

	// The machine is up and running on the key that signs today.
	if st, body := t80Connect2(t, srv.URL, tok); st != http.StatusOK {
		t.Fatalf("PREMISE FAILED: %d %s", st, body)
	}
	beforeKey := t80TokenKeyOf(t, dal, "m-probe")
	if beforeKey == "" {
		t.Fatal("PREMISE FAILED: the running machine must have been observed")
	}

	// The owner rotates. The machine is now on an outgoing key and does not know it.
	if _, err := keys.rotate(dal); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// The warden mints itself a candidate and PRESENTS it — step 2. This is a
	// real request with a real new-key credential; the only thing that does NOT
	// happen is the machine adopting it.
	candidate, err := api.mintWardenToken(Member{ID: "m-probe", Kind: machineKind})
	if err != nil {
		t.Fatalf("mint candidate: %v", err)
	}
	if st, body := t80Get(t, srv.URL+"/api/machines", candidate); st != http.StatusOK {
		t.Fatalf("PREMISE FAILED: the candidate must be ACCEPTED — that is what "+
			"the warden's probe checks, and the whole trap is that it succeeds; "+
			"got %d %s", st, body)
	}

	// Step 3 failed. Nothing was written. The machine is still running on the old
	// credential, so the station must still say so.
	if got := t80TokenKeyOf(t, dal, "m-probe"); got != beforeKey {
		t.Fatalf("the station moved this machine to %q on the strength of a "+
			"credential it merely PRESENTED (want it left at %q).\n"+
			"On a real host the write after that probe can fail, and the machine "+
			"keeps running on the outgoing key — while this number tells the "+
			"owner it is safe to press remove. That press has no grace period "+
			"and no undo.", got, beforeKey)
	}
	// And the same thing read the way the OWNER reads it, off the wire.
	ownerTok := mintOwnerAt(t, keys, time.Now().Unix())
	row, ok := t80ListMachines(t, srv.URL, ownerTok)["m-probe"]
	if !ok {
		t.Fatalf("GET /api/machines does not list m-probe")
	}
	if row.TokenKeyCurrent != nil && *row.TokenKeyCurrent {
		t.Fatal("the machine reads as token_key_current=true after only " +
			"PRESENTING a credential it never adopted — this is the number " +
			"saying SAFE at the one moment it must not")
	}
}
