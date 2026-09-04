package main

// receipt_reporter_identity_test.go — WHO SENT THIS RECEIPT.
//
// A command_result receipt has always carried WHAT happened (rpc / ok / reason)
// and WHO IT WAS ABOUT (member_id / worker_id). It has never carried WHICH
// MACHINE SAID SO — and the server, which is handed that fact by the verified
// token on every single POST, threw it away and used it only as the SSE event
// signature (`trigger`).
//
// Two consequences, both pinned here:
//
//  1. the worker STOP retry (worker_spawn.go retryUnlandedWorkerStop) judged
//     "did my kill take?" on PRESENCE ALONE. A warden answering "there is no
//     such session here" was indistinguishable from a warden that never
//     answered at all, so a kill that was correctly delivered and correctly
//     no-op'd was re-dispatched every stop_retry, forever.
//  2. the receipt deadline (receipt_watch.go noteReceiptArrived) disarmed on
//     the TARGET id alone. An identity sweep broadcasts a stop to every warden
//     in the fleet, so ANY of their polite idempotent receipts silently
//     cancelled the deadline that was waiting on ONE specific machine — the
//     watch could not fire even when the machine it was actually waiting on
//     had gone dark.
//
// ZERO WIRE CHANGE. Nothing below adds a field to CommandResult. The reporter
// identity is read where it always was: the verified token
// (caller-identity-convention — "facts derive from the verified token, not a
// self-report"). A warden's credential is minted by mintWardenToken with
// sub == the warden member's own id, and a warden member's id IS the machine id
// (api_machines.go onboard). TestReceiptReporter_PremiseWardenSubIsTheMachineID
// nails that premise down so a future re-mint cannot quietly break it.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stopNoopReceiptBody builds the exact receipt a warden POSTs when its robust
// stop found nothing to kill (cli/ocwarden/command.go rpcStop, stopNoopReason).
// key is "member_id" (the converged P5b verb) or "worker_id" (the legacy one) —
// both routes must learn the reporter, so both are exercised.
func stopNoopReceiptBody(key, id string) string {
	return fmt.Sprintf(
		`{"command_result":{%q:%q,"rpc":"stop","ok":true,`+
			`"reason":"no_such_session: stop was a no-op (no session, no member process on this warden)",`+
			`"log":"session=oc-%s: no_such_session","at":"2026-08-26T00:00:00Z"}}`,
		key, id, id)
}

// postWardenReceipt drives the REAL ingest handler with a warden-shaped token:
// scope=agent, sub = the machine id, NO machine_id claim (a warden carries no
// self-binding — authz.go). This is the whole point of the premise: the server
// already holds the reporter's identity on every receipt POST.
func postWardenReceipt(t *testing.T, s *apiServer, wardenID, body string) {
	t.Helper()
	rec := doIngestTelemetry(s, wardenID, "", body)
	if rec.Code != 200 {
		t.Fatalf("receipt ingest from %s: %d %s", wardenID, rec.Code, rec.Body.String())
	}
}

// ── the premise ──────────────────────────────────────────────────────────────

// 🔴 THE LOAD-BEARING PREMISE. Everything in this file assumes the server can
// name the reporting machine WITHOUT any payload change, because a warden's
// verified token sub already IS its machine id. If a future mint ever gives a
// warden a different sub (or starts stamping a machine_id claim on it), the
// reporter resolution below silently stops matching and both fixes rot into
// no-ops — so the premise gets its own test rather than a comment.
func TestReceiptReporter_PremiseWardenSubIsTheMachineID(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-premise")

	m, err := s.dal.GetMember("m-premise")
	if err != nil || m == nil {
		t.Fatalf("reload warden: %v", err)
	}
	tok, err := s.mintWardenToken(*m)
	if err != nil {
		t.Fatalf("mint warden token: %v", err)
	}
	claims, err := verifyJWT(tok, s.keys.signingSecret(), 0)
	if err != nil {
		t.Fatalf("verify warden token: %v", err)
	}
	if sub, _ := claims["sub"].(string); sub != "m-premise" {
		t.Fatalf("a warden's token sub must BE its machine id — the receipt path has no "+
			"other way to name the reporter without changing the wire; got sub=%q", sub)
	}
	if claim, _ := claims["machine_id"].(string); claim != "" {
		t.Fatalf("a warden carries NO self-binding machine_id claim (authz.go); a non-empty "+
			"one here means the reporter must be read from the claim instead of the sub, "+
			"got %q", claim)
	}
}

// ── (1) the retry loop must read the receipt, not just presence ──────────────

// 🔴 THE 21-RESPAWN HOLE. The kill was handed to the target warden, the target
// warden answered "no such session here", and the worker id is STILL online on
// that machine (a stale/rebound SSE claim is exactly the state that makes
// presence lie). Presence alone says "the kill did not take" and re-fires.
// The receipt from the machine we aimed at says otherwise, and it outranks
// presence: that warden looked and there was nothing there.
func TestWorkerStopRetry_NoSuchSessionFromTargetEndsTheRetry(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stoppedLiveWorker(t, s, "ow-live", ServerSelfHost)
	// The worker's roster row, so the receipt takes the production routing
	// (foldCommandResult → KindOutsource → foldWorkerCommandResult).
	putTestMember(t, s, Member{
		ID: "ow-live", Name: "O-ow-live", Kind: KindOutsource,
		RosterStatus: RosterStatusActive,
	})

	base := nowSecs()
	s.outsourceMu.Lock()
	s.stopWorkerNow(w)
	s.outsourceMu.Unlock()
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 1 {
		t.Fatalf("SENTINEL: the first kill must reach the warden's FIFO, got %d frames", len(got))
	}

	postWardenReceipt(t, s, ServerSelfHost, stopNoopReceiptBody("member_id", "ow-live"))

	s.runOutsourceTick(base + defaultReconcileConfig().StopRetry + 1)
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 0 {
		t.Fatalf("the machine we aimed the kill at answered \"there is no such session "+
			"here\" — that is the strongest evidence available that the kill ARRIVED and "+
			"the target was already gone. Re-dispatching anyway is the silent 21-respawn "+
			"loop: presence and the receipt were never compared, so \"the blade missed\" "+
			"and \"the blade never left\" looked identical. got %d frames, want 0", len(got))
	}
}

// 🔴 THE OTHER DIRECTION — and it is the one a blanket "any receipt stops the
// retry" fix would break. An identity sweep broadcasts the stop to EVERY warden;
// every warden that never hosted this session answers no_such_session as a
// matter of course. Those are polite noise about someone else's machine, and a
// retry that folded them would abandon a genuinely undelivered kill on the word
// of a machine that was never involved.
func TestWorkerStopRetry_NoSuchSessionFromAnotherWardenKeepsTheRetry(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-bystander")
	w := stoppedLiveWorker(t, s, "ow-live", ServerSelfHost)
	putTestMember(t, s, Member{
		ID: "ow-live", Name: "O-ow-live", Kind: KindOutsource,
		RosterStatus: RosterStatusActive,
	})

	base := nowSecs()
	s.outsourceMu.Lock()
	s.stopWorkerNow(w)
	s.outsourceMu.Unlock()
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 1 {
		t.Fatalf("SENTINEL: the first kill must reach the warden's FIFO, got %d frames", len(got))
	}

	postWardenReceipt(t, s, "m-bystander", stopNoopReceiptBody("member_id", "ow-live"))

	s.runOutsourceTick(base + defaultReconcileConfig().StopRetry + 1)
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 1 {
		t.Fatalf("a broadcast bystander answered about ITS OWN empty tmux, not about the "+
			"machine the kill went to — the retry it owes %q is untouched. got %d frames, "+
			"want 1", ServerSelfHost, len(got))
	}
}

// 🔴 THE GAP A MUTANT FOUND (M6, and it was GREEN until this test existed).
// Widening the retry's new ear from "no_such_session" to "any receipt from the
// target" passed everything else in this file, and it is the most dangerous
// direction the change could drift: the warden's OTHER stop verdict is
// "stop incomplete (session still present / broken probe / member process
// survived the sweep)" — a receipt from the right machine saying THE KILL DID
// NOT TAKE. Folding that as "收工" would disarm the retry on the exact evidence
// that most demands it, and turn a re-dispatch bug into the 殘活 session the
// retry was built to prevent (owner ruling: 零容忍).
//
// The narrow reading is the whole fix: only the receipt that says "I looked and
// there was nothing here" ends the retry.
func TestWorkerStopRetry_FailedStopReceiptFromTargetKeepsTheRetry(t *testing.T) {
	s := newWorkerTestServer(t)
	w := stoppedLiveWorker(t, s, "ow-live", ServerSelfHost)
	putTestMember(t, s, Member{
		ID: "ow-live", Name: "O-ow-live", Kind: KindOutsource,
		RosterStatus: RosterStatusActive,
	})

	base := nowSecs()
	s.outsourceMu.Lock()
	s.stopWorkerNow(w)
	s.outsourceMu.Unlock()
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 1 {
		t.Fatalf("SENTINEL: the first kill must reach the warden's FIFO, got %d frames", len(got))
	}

	// The verbatim ok=false verdict from cli/ocwarden/command.go rpcStop.
	postWardenReceipt(t, s, ServerSelfHost,
		`{"command_result":{"member_id":"ow-live","rpc":"stop","ok":false,`+
			`"reason":"stop incomplete (session still present / broken probe / member process survived the sweep)",`+
			`"log":"session=oc-ow-live: stop incomplete","at":"2026-08-26T00:00:00Z"}}`)

	s.runOutsourceTick(base + defaultReconcileConfig().StopRetry + 1)
	if got := s.hub.DrainWardenCommands(ServerSelfHost); len(got) != 1 {
		t.Fatalf("the target machine reported that the kill did NOT take — the one receipt "+
			"that must never end the retry. Only \"there is no such session here\" is "+
			"收工 evidence; \"the session is still present\" is the opposite of it. "+
			"got %d frames, want 1", len(got))
	}
}

// ── (2) the receipt deadline must only be disarmed by the machine it waits on ─

// 🔴 THE MIS-CANCELLED TIMER. The watch records the machine the frame was handed
// to (pendingReceipt.Warden) and has done so since it was written — it simply
// never read it back. A receipt from ANY machine cancelled it, so during an
// identity sweep the deadline for a dark machine was routinely cancelled by a
// healthy one, and receipt_missing could not fire for the case it exists for.
func TestReceiptWatch_AnotherWardensReceiptDoesNotDisarm(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-dark")
	putWardenFixture(t, s, "m-bystander")
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-owed", Codename: "O-owed", Runtime: "claude",
		Status: WorkerStatusActive,
	})

	s.armReceiptWatch("ow-owed", reconcileCmdStop, "m-dark", nowSecs())
	postWardenReceipt(t, s, "m-bystander", stopNoopReceiptBody("worker_id", "ow-owed"))

	s.sweepLapsedReceipts(nowSecs() + receiptDeadlineSecs + 1)
	got, err := s.dal.GetOutsourceWorker("ow-owed")
	if err != nil || got == nil {
		t.Fatalf("reload worker: %v", err)
	}
	if !strings.HasPrefix(got.LastOpReason, receiptMissingReasonCode+":") {
		t.Fatalf("the deadline was waiting on %q and %q is the only machine that can "+
			"answer it; a bystander's broadcast receipt cancelled the watch and the "+
			"dark machine's silence became unreportable. last_op_reason = %q, want the "+
			"%s stamp", "m-dark", "m-dark", got.LastOpReason, receiptMissingReasonCode)
	}
}

// The complementary direction: the machine we ARE waiting on answers, and the
// deadline must go away. Without this the fix above could be "never disarm",
// which would stamp receipt_missing on every healthy op in the fleet.
func TestReceiptWatch_TargetWardensReceiptDisarms(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-dark")
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-answered", Codename: "O-answered", Runtime: "claude",
		Status: WorkerStatusActive,
	})

	s.armReceiptWatch("ow-answered", reconcileCmdStop, "m-dark", nowSecs())
	postWardenReceipt(t, s, "m-dark", stopNoopReceiptBody("worker_id", "ow-answered"))

	s.sweepLapsedReceipts(nowSecs() + receiptDeadlineSecs + 1)
	got, err := s.dal.GetOutsourceWorker("ow-answered")
	if err != nil || got == nil {
		t.Fatalf("reload worker: %v", err)
	}
	if strings.Contains(got.LastOpReason, receiptMissingReasonCode) {
		t.Fatalf("the machine the watch was waiting on ANSWERED — the receipt channel "+
			"demonstrably works, which is the entire question the deadline asks. "+
			"Stamping it anyway cries wolf on a healthy op. last_op_reason = %q",
			got.LastOpReason)
	}
}

// An UNRESOLVED watch (Warden == "", the dispatch could not name a machine)
// must keep its old permissive behaviour: any receipt for that target disarms
// it. Tightening a watch that never knew who it was waiting on would invent
// receipt_missing stamps out of nothing.
func TestReceiptWatch_UnresolvedWardenStillDisarmsOnAnyReceipt(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-someone")
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-unbound", Codename: "O-unbound", Runtime: "claude",
		Status: WorkerStatusActive,
	})

	s.armReceiptWatch("ow-unbound", reconcileCmdStop, "", nowSecs())
	postWardenReceipt(t, s, "m-someone", stopNoopReceiptBody("worker_id", "ow-unbound"))

	s.sweepLapsedReceipts(nowSecs() + receiptDeadlineSecs + 1)
	got, err := s.dal.GetOutsourceWorker("ow-unbound")
	if err != nil || got == nil {
		t.Fatalf("reload worker: %v", err)
	}
	if strings.Contains(got.LastOpReason, receiptMissingReasonCode) {
		t.Fatalf("a watch that never resolved a machine has nobody to compare against; "+
			"it must stay permissive. last_op_reason = %q", got.LastOpReason)
	}
}

// ── the guard nobody was pinning: claim-bearing ⇒ NOT the machine ────────────

// 🔴 THE UNGUARDED GUARD (T-5b62 gap 1). receiptReporterMachine's whole body is
// two lines, and the FIRST one —
//
//	if currentMachineClaim(r) != "" { return "" }
//
// — had no test at all: deleting it left the entire ocserverd suite green. It is
// the line that says "a token carrying a placement claim is something running ON
// a machine, not the machine itself", and without it the resolver hands back a
// MEMBER id (an agent/worker id) to two consumers that will compare it against a
// MACHINE id and act on the mismatch.
//
// This is the cheapest place to see the damage. The watch is waiting on m-dark.
// An AGENT LIVING ON m-dark posts a receipt for the target: its sub is the
// agent's own member id, its machine_id claim is m-dark. Resolved correctly the
// reporter is "" — UNKNOWN, the permissive case — and the watch disarms, because
// the receipt channel demonstrably worked. Drop the guard and the reporter
// becomes the AGENT's member id, which is not m-dark, so the watch reads it as
// "someone else's machine answered", keeps waiting, and stamps receipt_missing
// on a member whose receipt is sitting in the server's own hands.
//
// Note the direction: the guard's failure mode is NOT "a stranger sneaks past".
// It is the resolver confidently naming a machine that does not exist, and the
// UNKNOWN fallback every consumer was built around never being reached.
func TestReceiptReporter_ClaimBearingTokenIsNotTheMachineItRunsOn(t *testing.T) {
	s := newWorkerTestServer(t)
	putWardenFixture(t, s, "m-dark")
	// The agent living ON m-dark: its own roster id, pinned to that machine.
	putTestMember(t, s, Member{
		ID: "m-onbox", Name: "Onbox", Kind: KindStaff, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	putWorkerFixture(t, s, OutsourceWorker{
		ID: "ow-owed", Codename: "O-owed", Runtime: "claude",
		Status: WorkerStatusActive,
	})

	s.armReceiptWatch("ow-owed", reconcileCmdStop, "m-dark", nowSecs())

	// sub = the agent's member id, machine_id claim = the machine it runs on.
	// mintAgentToken's exact shape (api_auth.go) — the ONE token class the
	// guard exists to reject as a machine name.
	rec := doIngestTelemetry(s, "m-onbox", "m-dark",
		stopNoopReceiptBody("worker_id", "ow-owed"))
	if rec.Code != 200 {
		t.Fatalf("receipt ingest from m-onbox: %d %s", rec.Code, rec.Body.String())
	}

	s.sweepLapsedReceipts(nowSecs() + receiptDeadlineSecs + 1)
	got, err := s.dal.GetOutsourceWorker("ow-owed")
	if err != nil || got == nil {
		t.Fatalf("reload worker: %v", err)
	}
	if strings.Contains(got.LastOpReason, receiptMissingReasonCode) {
		t.Fatalf("a token carrying a machine_id claim is something RUNNING ON a "+
			"machine, not the machine — receiptReporterMachine must answer \"\" "+
			"(UNKNOWN) for it, and UNKNOWN is the permissive case. Reading the "+
			"claim-bearer's MEMBER id as a MACHINE id makes it look like a "+
			"different machine answered, so the watch kept waiting and stamped "+
			"%s on a receipt the server had already received and read. "+
			"last_op_reason = %q", receiptMissingReasonCode, got.LastOpReason)
	}

	// And the resolver itself, stated once so the failure above cannot be
	// mistaken for a receipt_watch bug: the claim-bearer resolves to UNKNOWN,
	// never to its own sub.
	if got := receiptReporterMachine(claimReq("m-onbox", "m-dark")); got != "" {
		t.Fatalf("receiptReporterMachine with a machine_id claim must be \"\" "+
			"(UNKNOWN — this is a thing running on a machine, not the machine), got %q", got)
	}
	// The complementary arm, so the fix cannot degenerate into "always UNKNOWN":
	// a claim-less token (the warden shape) still names its sub as the machine.
	if got := receiptReporterMachine(claimReq("m-dark", "")); got != "m-dark" {
		t.Fatalf("a claim-less warden token's sub IS the machine id, got %q", got)
	}
}

// claimReq builds a bare request carrying only the verified-claims context the
// identity accessors read — no route, no body, nothing else in play.
func claimReq(sub, machineClaim string) *http.Request {
	claims := map[string]any{"sub": sub, "scope": "agent"}
	if machineClaim != "" {
		claims["machine_id"] = machineClaim
	}
	req := httptest.NewRequest("POST", "/api/monitoring/telemetry", nil)
	return req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
}
