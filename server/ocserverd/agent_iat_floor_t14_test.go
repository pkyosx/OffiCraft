package main

// agent_iat_floor_t14_test.go — T-14 項目 4B, ONE invariant:
//
//	the moment a member's NEW session says it is up (POST /api/self/waking),
//	every credential minted for an EARLIER session of that same member stops
//	working.
//
// 🔴 WHY. A member's generations overlap on purpose: the replacement boots and
// connects while the outgoing session is still working its close-out. Every
// server-side effect keyed on the MEMBER id therefore cannot tell which
// generation is speaking — the outgoing session's last words land on its
// successor. The owner's ruling (2026-08-30, rc-fe6451abe579, option 1) is that
// the NEW generation coming up is what ends the old one's authority: 「新的一輪
// 一上線就失效」, with a handover cut in half being the knowingly accepted cost.
//
// The discriminator is the caller's own credential. Every generation boots on a
// token minted for THAT spawn, so its `iat` names the generation speaking.
// report_waking stamps the CALLER'S OWN iat as member.agent_iat_floor, and
// requireAuth refuses any agent-scope token whose iat is STRICTLY LESS THAN it.
// Own-iat rather than now() is what keeps the stamping session from locking
// itself out; strictly-less-than is what makes that exact.
//
// ⚠️ NOT SOLVED, and deliberately so (owner 2026-08-28: 「先不管搶同一秒」):
// `iat` is whole seconds, so two sessions of one member that start inside the
// SAME second are indistinguishable to this gate. Nothing below tests that case
// because nothing below fixes it.
//
// Everything runs against a temp sqlite + httptest server, on the whole wired
// stack (requireAuth → RBAC choke → handler), with tokens the server itself
// minted. Nothing here touches a real machine, warden, or agent.

import (
	"log"
	"net/http"
	"strings"
	"testing"
	"time"
)

// wakeWith calls the REAL POST /api/self/waking over the wire with `token`,
// which is the only way the floor is ever stamped in production.
func wakeWith(t *testing.T, srvURL, token string) {
	t.Helper()
	st, body := revokeCall(t, "POST", srvURL+"/api/self/waking", token, `{}`)
	if st != http.StatusOK {
		t.Fatalf("POST /api/self/waking: want 200, got %d %s", st, body)
	}
}

// ---------------------------------------------------------------------------
// ① the superseded generation's token is refused
// ---------------------------------------------------------------------------

// TestAgentIatFloor_SupersededSessionTokenIsRefused is the deny half. The
// positive control is inside the test: the SAME token on the SAME request is
// asserted 200 before the new session wakes, so a test that always refused (or
// a probe that never worked) cannot pass.
//
// Mutant: delete the agentIatFloorRefusal call in requireAuth → both AFTER arms
// go back to 200 and this test is red.
func TestAgentIatFloor_SupersededSessionTokenIsRefused(t *testing.T) {
	srv, secret, api := revokeStack(t)
	agent := testAgent("m-t14-superseded")
	putTestMember(t, api, agent)

	now := time.Now().Unix()
	// Generation N booted ten minutes ago; generation N+1 booted just now.
	oldTok, err := mintJWT(agent.ID, "agent", 3600, secret, now-600, "")
	if err != nil {
		t.Fatal(err)
	}
	newTok, err := mintJWT(agent.ID, "agent", 3600, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}

	// BEFORE — the positive control. Nothing below means anything if these fail.
	for _, c := range []liveCall{
		{"the outgoing session reading the roster", "GET", "/api/members", ""},
		{"the outgoing session's close-out report", "POST", "/api/self/stopped", `{}`},
	} {
		if st, body := revokeCall(t, c.method, srv.URL+c.path, oldTok, c.body); st != http.StatusOK {
			t.Fatalf("POSITIVE CONTROL FAILED — %s must be 200 before the replacement "+
				"wakes, got %d %s", c.who, st, body)
		}
	}

	// …and now the replacement reports that it is up.
	wakeWith(t, srv.URL, newTok)

	// AFTER — the same token, the same requests.
	for _, c := range []liveCall{
		{"the superseded session reading the roster", "GET", "/api/members", ""},
		{"the superseded session's close-out report", "POST", "/api/self/stopped", `{}`},
	} {
		st, body := revokeCall(t, c.method, srv.URL+c.path, oldTok, c.body)
		if st != http.StatusUnauthorized {
			t.Fatalf("%s: a credential minted for a generation this member has "+
				"already replaced must be 401 once the replacement has reported "+
				"waking, got %d %s", c.who, st, body)
		}
	}
}

// ---------------------------------------------------------------------------
// ② nothing else is refused — the load-bearing half
// ---------------------------------------------------------------------------

// TestAgentIatFloor_TheLiveGenerationAndItsNeighboursStillWork is the
// collateral-damage guard. A floor that refused everyone would pass ① and be
// worthless. Three separate arms, each one a way of getting this wrong:
//
//	a) the session that STAMPED the floor is not locked out by its own stamp —
//	   the reason report_waking stores the caller's own iat and not now();
//	b) a token minted AFTER the floor for the same member works — the gate is a
//	   floor, not an allow-list of one;
//	c) another member's older token is untouched — the floor is PER MEMBER, and
//	   a version keyed on one global number would fail here.
func TestAgentIatFloor_TheLiveGenerationAndItsNeighboursStillWork(t *testing.T) {
	srv, secret, api := revokeStack(t)
	live := testAgent("m-t14-live")
	putTestMember(t, api, live)
	bystander := testAgent("m-t14-bystander")
	putTestMember(t, api, bystander)

	now := time.Now().Unix()
	liveTok, err := mintJWT(live.ID, "agent", 3600, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	// Minted a minute after the one that stamps the floor.
	laterTok, err := mintJWT(live.ID, "agent", 3600, secret, now+60, "")
	if err != nil {
		t.Fatal(err)
	}
	// The bystander's token is OLDER than the floor the other member sets.
	bystanderTok, err := mintJWT(bystander.ID, "agent", 3600, secret, now-600, "")
	if err != nil {
		t.Fatal(err)
	}

	wakeWith(t, srv.URL, liveTok)

	for who, token := range map[string]string{
		"the session that stamped the floor with its own iat": liveTok,
		"a token minted after the floor for the same member":  laterTok,
		"another member's older token":                        bystanderTok,
	} {
		if st, body := revokeCall(t, "GET", srv.URL+"/api/members", token, ""); st != http.StatusOK {
			t.Fatalf("%s must still be 200, got %d %s", who, st, body)
		}
	}
	// And the live session's own close-out report is still collected — the
	// T-2123 hole (an agent that says it is finished staying online forever)
	// must not be re-opened by this gate.
	if st, body := revokeCall(t, "POST", srv.URL+"/api/self/stopped", liveTok, `{}`); st != http.StatusOK {
		t.Fatalf("the live session's own close-out report must still be 200, got %d %s", st, body)
	}
}

// TestAgentIatFloor_TheWakingCallerIsNeverLockedOutByItsOwnStamp is the arm
// that separates "the caller's own iat" from "now()" — the ② arms above cannot,
// because a token minted a moment before the wake sits either side of the
// server clock by fractions of a second.
//
// The gap here is FIVE MINUTES and it is the realistic one: a token is minted
// when the START is dispatched, and the agent's process has to launch, load its
// boot document and reach report_waking before it is used. A floor taken from
// the server clock at that moment sits five minutes ABOVE the caller's own iat,
// and the very first thing that agent does after reporting itself awake is 401.
//
// Mutant: stamp nowSecs() instead of the caller's iat in stampAgentIatFloor →
// this test is red, and it is the only one that reliably is.
func TestAgentIatFloor_TheWakingCallerIsNeverLockedOutByItsOwnStamp(t *testing.T) {
	srv, secret, api := revokeStack(t)
	agent := testAgent("m-t14-slowboot")
	putTestMember(t, api, agent)

	// Minted five minutes before this session got as far as reporting itself up.
	tok, err := mintJWT(agent.ID, "agent", 3600, secret, time.Now().Unix()-300, "")
	if err != nil {
		t.Fatal(err)
	}
	wakeWith(t, srv.URL, tok)

	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", tok, ""); st != http.StatusOK {
		t.Fatalf("the token that REPORTED the wake must still work after it: a floor "+
			"taken from the server clock rather than from this caller's own iat puts "+
			"the whole mint-to-boot gap above it, so the session locks itself out on "+
			"its first request after saying it is awake. got %d %s", st, body)
	}
}

// ---------------------------------------------------------------------------
// ③ warden is exempt — a SAFETY property, not an optimisation
// ---------------------------------------------------------------------------

// TestAgentIatFloor_WardenPermanentTokenIsExempt pins the one exclusion that
// cannot be left to a coincidence of today's client.
//
// mintWardenToken issues scope="agent" credentials for a machine member, so a
// warden token is indistinguishable from an agent token by scope alone.
// cli/ocwarden does not call report_waking today — but that is a fact about
// today's warden, not a contract. If one line there ever raised a floor above a
// machine's credential, that machine could not renew either (the renewal
// endpoint sits behind this same gate), so the one path out is shut at the same
// instant and the recovery is a hand re-install. The gate therefore excludes
// Kind == machineKind by name.
//
// 🔴 THIS TEST MINTS THE PRE-第二段 SHAPE ON PURPOSE — mintJWTWithoutExpiry, a
// PERMANENT credential — and that is not a leftover. Every machine installed
// before T-fc53 第二段 is holding exactly this, and it is the population the
// exemption matters most for: it has no exp to expire out of the way at all, so
// a floor raised above one would be permanent rather than bounded. The
// exp-BEARING shape the station mints today is covered in the second arm below,
// through the real mint.
//
// Mutant: drop the machineKind exclusion from agentIatFloorRefusal → the AFTER
// arm here turns 401 and this test is red.
func TestAgentIatFloor_WardenPermanentTokenIsExempt(t *testing.T) {
	srv, secret, api := revokeStack(t)
	putTestMember(t, api, Member{
		ID: "m-t14-box", Name: "t14-box", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})

	now := time.Now().Unix()
	// The permanent credential a machine was installed with, ten minutes ago.
	oldPermanent, err := mintJWTWithoutExpiry("m-t14-box", "agent", secret, now-600, "")
	if err != nil {
		t.Fatal(err)
	}
	// A newer credential for the same machine — whatever raises the floor.
	newPermanent, err := mintJWTWithoutExpiry("m-t14-box", "agent", secret, now, "")
	if err != nil {
		t.Fatal(err)
	}

	// Positive control: the old permanent credential works to start with.
	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", oldPermanent, ""); st != http.StatusOK {
		t.Fatalf("POSITIVE CONTROL FAILED — an active machine's permanent credential "+
			"must be 200, got %d %s", st, body)
	}

	// The floor is raised on the machine's own member row, through the same
	// public seam any client would use.
	wakeWith(t, srv.URL, newPermanent)

	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", oldPermanent, ""); st != http.StatusOK {
		t.Fatalf("a machine's PERMANENT credential must never be refused by the "+
			"agent iat floor (it has no exp to expire out of the way — refusing it "+
			"takes the machine off the fleet until someone re-installs it by hand), "+
			"got %d %s", st, body)
	}

	// ── the shape the station mints TODAY (T-fc53 第二段: it carries an exp) ──
	//
	// The gate keys on KIND, not on the presence of exp, so this arm should be
	// redundant — and that is exactly why it is written down. Giving warden
	// credentials an expiry back is the change most likely to prompt someone to
	// "simplify" the exemption on the grounds that a warden token can now expire
	// out of the way like any other; every assertion above would stay green
	// while doing it, because they all exercise the exp-less shape.
	current, err := api.mintWardenToken(Member{ID: "m-t14-box", Kind: KindWarden})
	if err != nil {
		t.Fatalf("mint the current warden credential shape: %v", err)
	}
	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", current, ""); st != http.StatusOK {
		t.Fatalf("a machine's CURRENT (exp-bearing) credential was refused by the "+
			"agent iat floor: %d %s — the exemption is on the member's kind, and a "+
			"warden refused here cannot reach POST /api/machines/renew-credential "+
			"either, so it stays off the fleet until its credential runs out and "+
			"somebody re-installs the host", st, body)
	}
}

// ---------------------------------------------------------------------------
// ④ the floor only ever moves FORWARD
// ---------------------------------------------------------------------------

// TestAgentIatFloor_ALaterWriteWithAnOlderIatCannotLowerTheFloor pins the
// direction of the write, which is the one safety property in this feature that
// nothing else was holding.
//
// 🔴 WHY IT NEEDS ITS OWN TEST. Every other test here drives the floor through
// POST /api/self/waking, and that route is itself gated by the floor — so once a
// newer generation has stamped, an older token can no longer reach the handler
// and no test that goes over the wire can produce a backwards write at all. The
// backwards write happens when two generations are IN FLIGHT AT THE SAME TIME:
// both pass requireAuth (neither floor is set yet), then the older one's UPDATE
// lands second. The Go code cannot order them; `max()` in the statement is what
// makes the loser harmless, and a read-modify-write in Go would not.
//
// The consequence of losing that is not a stale number: it is the REVOCATION
// being undone. The floor drops back to the superseded generation's own iat, its
// credentials start working again, and the outgoing session's last words land on
// its successor — exactly what T-14 項目 4B exists to stop.
//
// So the second write below is made through the SOLE writer of the column
// (SetMemberAgentIatFloor — the same call the handler makes), which is precisely
// the losing racer's write, and the assertion is on the observable end: the
// superseded token stays 401.
//
// Mutant: `max(agent_iat_floor, ?)` → `?` in dal.go SetMemberAgentIatFloor →
// the floor drops to the older iat and this test is red on both arms.
func TestAgentIatFloor_ALaterWriteWithAnOlderIatCannotLowerTheFloor(t *testing.T) {
	srv, secret, api := revokeStack(t)
	agent := testAgent("m-t14-forward-only")
	putTestMember(t, api, agent)

	now := time.Now().Unix()
	oldIat, newIat := now-600, now
	oldTok, err := mintJWT(agent.ID, "agent", 3600, secret, oldIat, "")
	if err != nil {
		t.Fatal(err)
	}
	newTok, err := mintJWT(agent.ID, "agent", 3600, secret, newIat, "")
	if err != nil {
		t.Fatal(err)
	}

	// The replacement wakes: the floor rises to its own iat and the outgoing
	// generation's token is dead. (Positive control for everything below.)
	wakeWith(t, srv.URL, newTok)
	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", oldTok, ""); st != http.StatusUnauthorized {
		t.Fatalf("POSITIVE CONTROL FAILED — the superseded token must be 401 right "+
			"after the replacement wakes, got %d %s", st, body)
	}

	// …and now the OUTGOING generation's own stamp lands, late. This is the
	// losing side of two report_waking calls in flight together; it writes its
	// own (older) iat through the same and only writer.
	if err := api.dal.SetMemberAgentIatFloor(agent.ID, float64(oldIat)); err != nil {
		t.Fatal(err)
	}

	m, err := api.dal.GetMember(agent.ID)
	if err != nil || m == nil {
		t.Fatalf("GetMember: %v", err)
	}
	if int64(m.AgentIatFloor) != newIat {
		t.Fatalf("the credential floor MOVED BACKWARDS: want it held at the newer "+
			"generation's iat %d, got %d. A write that carries an older iat must "+
			"never lower the floor — the loser of two concurrent wakes would "+
			"otherwise hand the superseded session its credentials back",
			newIat, int64(m.AgentIatFloor))
	}
	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", oldTok, ""); st != http.StatusUnauthorized {
		t.Fatalf("the superseded session's token CAME BACK TO LIFE after its own "+
			"late stamp lowered the floor: a revocation that a losing racer can "+
			"undo is not a revocation. want 401, got %d %s", st, body)
	}
}

// ---------------------------------------------------------------------------
// ⑤ the refusal names itself on the response — the client half depends on it
// ---------------------------------------------------------------------------

// TestAgentIatFloor_TheRefusalIsMarkedAuthoritativeAndNothingElseIs is the
// server end of the fix for the orphan this cut would otherwise create.
//
// A superseded session's listener keeps a long-lived SSE; when it drops and
// re-dials it gets 401 here. cli/ocagent's reconnect loop treats an unmarked
// non-200 as "retry with backoff, forever" — right for a restarting station or
// an expired token, and a permanent orphan for THIS refusal (the floor only ever
// rises, so it can never resolve). The client cannot tell them apart from the
// status line; the server can, and says so with X-OC-Auth-Refusal.
//
// BOTH arms matter. Marking every 401 would be worse than the bug it fixes: the
// listener would kill its own tmux session — and everything running under it —
// whenever the station was briefly unable to authenticate anyone.
//
// Mutant: delete the w.Header().Set(authRefusalHeader, …) line in requireAuth →
// arm (a) is red.
//
// ⚠️ arm (b) IS ONE SAMPLE, NOT A SWEEP, and this comment used to claim
// otherwise ("set it on the other 401s too → arm (b) is red"). It is not: (b)
// sends ONE garbage token, which reaches only the verifyJWT exit, so marking
// missing-credentials + permanentCredentialRefusal + revocationRefusal left
// this file — and the whole suite — green. The exhaustive version lives in
// auth_refusal_exits_t14_test.go, which probes EVERY 401 exit requireAuth has
// and counts them out of its AST so a new one cannot be added unprobed. (b)
// stays here as the local sanity check it always was.
func TestAgentIatFloor_TheRefusalIsMarkedAuthoritativeAndNothingElseIs(t *testing.T) {
	srv, secret, api := revokeStack(t)
	agent := testAgent("m-t14-marked")
	putTestMember(t, api, agent)

	now := time.Now().Unix()
	oldTok, err := mintJWT(agent.ID, "agent", 3600, secret, now-600, "")
	if err != nil {
		t.Fatal(err)
	}
	newTok, err := mintJWT(agent.ID, "agent", 3600, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	wakeWith(t, srv.URL, newTok)

	refusalMark := func(t *testing.T, token string) (int, string) {
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
		return resp.StatusCode, resp.Header.Get(authRefusalHeader)
	}

	// (a) the floor's own refusal is marked.
	st, mark := refusalMark(t, oldTok)
	if st != http.StatusUnauthorized {
		t.Fatalf("POSITIVE CONTROL FAILED — the superseded token must be 401, got %d", st)
	}
	if mark != refusalAgentSuperseded {
		t.Fatalf("the superseded-generation 401 must carry %s: %q so the process still "+
			"holding that session's socket can stop retrying and shut itself down. "+
			"Without it the refusal is indistinguishable from a station hiccup and "+
			"the orphan reconnects every ≤15s forever, invisibly. got %q",
			authRefusalHeader, refusalAgentSuperseded, mark)
	}

	// (b) an ORDINARY 401 is not marked — a token this server cannot verify at
	// all. Marking it would turn a self-healing retry into a self-kill.
	st, mark = refusalMark(t, "not-a-token")
	if st != http.StatusUnauthorized {
		t.Fatalf("POSITIVE CONTROL FAILED — a garbage token must be 401, got %d", st)
	}
	if mark != "" {
		t.Fatalf("an ordinary 401 must NOT be marked %s (got %q): the marker tells a "+
			"listener to KILL ITS OWN SESSION, so putting it on a refusal that may "+
			"resolve on its own — an expired token, a secret not loaded yet, a "+
			"restart in flight — trades noisy self-healing for killing healthy agents",
			authRefusalHeader, mark)
	}
}

// ---------------------------------------------------------------------------
// ⑦ the refusal leaves a trace on the station
// ---------------------------------------------------------------------------

// TestAgentIatFloor_TheRefusalIsLoggedWithTheMemberAndNoSecret pins the one
// line of server-side evidence this cut produces.
//
// 🔴 WHY A LOG LINE IS WORTH A TEST. This refusal ENDS A LIVE SESSION: the
// process that reads the marker kills its own tmux and the model session under
// it. Every other way a member's session dies leaves a trace on the station —
// a stop dispatch, a receipt, a reconcile decision. This one does not. Without
// the log.Printf in requireAuth's agent-floor branch, the owner sees a member's
// tmux simply VANISH with nothing in the server log, and the only remaining
// evidence is a 401 status code byte-identical to the five ordinary ones next
// to it.
//
// That line therefore has to survive refactors that have no idea what it is
// for, and until now nothing stopped one: deleting the log.Printf block (and
// the "log" import it orphans) left this file, this package and the whole suite
// green.
//
// The second half pins the comment's own claim that nothing secret is written.
// The refusal is triggered BY a credential, so the credential is the obvious
// thing to reach for when someone later wants the line to be more helpful —
// and a bearer token in a server log is a token in every log shipper,
// screenshot and paste after it. iat and sub are a timestamp and a member id;
// they are not.
//
// Mutants: delete the log.Printf block → arm (a) is red. Add the token (or its
// signature segment) to the line → arm (b) is red.
func TestAgentIatFloor_TheRefusalIsLoggedWithTheMemberAndNoSecret(t *testing.T) {
	srv, secret, api := revokeStack(t)
	agent := testAgent("m-t14-logged")
	putTestMember(t, api, agent)

	now := time.Now().Unix()
	oldTok, err := mintJWT(agent.ID, "agent", 3600, secret, now-600, "")
	if err != nil {
		t.Fatal(err)
	}
	newTok, err := mintJWT(agent.ID, "agent", 3600, secret, now, "")
	if err != nil {
		t.Fatal(err)
	}
	wakeWith(t, srv.URL, newTok)

	// Capture only from here: the waking call above is not the subject.
	var logs lockedLogBuffer
	oldOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldOutput) })

	if st, body := revokeCall(t, "GET", srv.URL+"/api/members", oldTok, ""); st != http.StatusUnauthorized {
		t.Fatalf("POSITIVE CONTROL FAILED — the superseded token must be 401 "+
			"before anything about its log line means something, got %d %s", st, body)
	}
	got := logs.String()

	// (a) the trace exists and NAMES THE MEMBER. A line that does not say who
	// was refused cannot be matched to the tmux session that disappeared.
	if !strings.Contains(got, agent.ID) {
		t.Fatalf("the agent-floor refusal wrote no log line naming %s.\n"+
			"Server log captured during the refusal:\n%s\n"+
			"That line is the ONLY trace this refusal leaves on the station: the "+
			"process reading X-OC-Auth-Refusal kills its own tmux and the model "+
			"session under it, and the 401 status alone is indistinguishable from "+
			"the ordinary refusals next to it. If it was removed in a refactor, "+
			"put it back rather than deleting this test.", agent.ID, got)
	}
	if !strings.Contains(got, "agent_iat_floor") {
		t.Fatalf("the refusal line does not say WHY it refused (no mention of "+
			"agent_iat_floor), so a reader has a member id and no reason:\n%s", got)
	}

	// (b) …and nothing secret. The token that triggered it, and in particular
	// its signature segment, must not be in the line.
	if strings.Contains(got, oldTok) {
		t.Fatalf("the refusal line contains the CREDENTIAL that triggered it. A " +
			"bearer token in a server log is a bearer token in every log shipper, " +
			"screenshot and paste downstream of it — and this one is refused only " +
			"by THIS station's floor, so it is still live against anything that " +
			"does not share the roster row. Log the sub and the iat; those are a " +
			"member id and a timestamp.")
	}
	if sig := oldTok[strings.LastIndex(oldTok, ".")+1:]; sig != "" && strings.Contains(got, sig) {
		t.Fatalf("the refusal line contains the token's SIGNATURE segment (%q). "+
			"Even without the header and payload that is the secret-derived half "+
			"of the credential and must never reach a log.", sig)
	}
}
