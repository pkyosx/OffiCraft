package main

// api_machines_renew_tfc53_test.go — POST /api/machines/renew-credential.
//
// The point of the endpoint is that a machine whose credential is running out
// does not need a human to go and reinstall that host. The point of THIS file
// is the three properties that make it safe to leave running unattended on
// every machine at once:
//
//	① a warden renews its own and the answer is a credential that WORKS;
//	② nothing that is not an active machine gets one — and the caller cannot
//	   name a target, so "A renews B's" is not a check that can be forgotten,
//	   it is a request shape that does not exist;
//	③ a machine deleted from the roster is refused. That refusal belongs to a
//	   DIFFERENT gate (the auth-layer revocation check), and inheriting a guard
//	   without measuring it is how you find out later that it moved.
//
// ⚠️ WHAT THIS FILE DELIBERATELY DOES NOT ASSERT: that the new credential
// differs from the old one, and T-fc53 第二段 did NOT change that. The reason it
// used to give — "warden credentials are minted WITHOUT an exp claim, so two
// mints for the same machine in the same second are byte-identical" — has been
// replaced by a reason that survives the expiry: exp is derived as iat + a fixed
// lifetime, so two mints inside the same second still produce identical claims
// and an inequality assertion would pass or fail on clock luck. Renewal is
// observable by the exp having MOVED, which needs a controlled clock this file
// does not have; the file that does is api_machines_test.go.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const renewPath = "/api/machines/renew-credential"

// renewedToken drives the endpoint through the whole wired stack and returns
// the minted credential plus the machine it says it is bound to.
func renewedToken(t *testing.T, srv string, token string) (string, string) {
	t.Helper()
	code, body := revokeCall(t, "POST", srv+renewPath, token, "")
	if code != http.StatusOK {
		t.Fatalf("renew: want 200, got %d %s", code, body)
	}
	var got struct {
		Token     string `json:"token"`
		MachineID string `json:"machine_id"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode renew response: %v (body %s)", err, body)
	}
	return got.Token, got.MachineID
}

// TestWardenRenewsItsOwnCredentialAndTheNewOneWorks is property ①, and the
// assertion is deliberately about USING the new credential rather than about
// its shape: a mint that returned a well-formed string nothing accepts would
// satisfy every structural check and still leave the machine dead at renewal
// time, which is the exact failure this endpoint must never have.
func TestWardenRenewsItsOwnCredentialAndTheNewOneWorks(t *testing.T) {
	srv, secret, api := revokeStack(t)
	putTestMember(t, api, Member{
		ID: "m-box", Name: "box-1", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	old, err := api.mintWardenToken(Member{ID: "m-box", Kind: KindWarden})
	if err != nil {
		t.Fatalf("mint starting credential: %v", err)
	}

	fresh, boundTo := renewedToken(t, srv.URL, old)
	if boundTo != "m-box" {
		t.Errorf("renewal bound the credential to %q, not to the caller m-box — "+
			"the machine acted on must be the caller's own verified sub", boundTo)
	}
	if fresh == "" {
		t.Fatal("renewal answered 200 with an empty token")
	}

	// The new credential must be accepted on a real warden request shape.
	if code, body := revokeCall(t, "POST", srv.URL+"/api/monitoring/telemetry",
		fresh, wardenHeartbeat); code != http.StatusOK {
		t.Errorf("the freshly renewed credential was refused on the warden's own "+
			"heartbeat: got %d %s — a renewal that mints something unusable is "+
			"worse than no renewal, because the caller has already replaced the "+
			"credential that did work", code, body)
	}
	_ = secret
}

// TestRenewIsRefusedToEveryCallerThatIsNotAnActiveMachine is property ②.
//
// There is no "A renews B" arm here and that is not an omission: the request
// carries no target, so the only machine the endpoint can reach is the caller.
// What CAN go wrong is the caller not being a machine at all, and the route
// choke does not stop that — principalMachine is the LOWEST rank, so an
// ordinary agent clears it. The handler is the only thing standing there.
//
// Mutant: drop the resolveMachine guard (mint straight from the sub) and the
// agent arm goes 200 — an ordinary member would be able to mint itself a
// PERMANENT credential, which is precisely what mintWardenToken exists to
// prevent.
func TestRenewIsRefusedToEveryCallerThatIsNotAnActiveMachine(t *testing.T) {
	srv, _, api := revokeStack(t)
	putTestMember(t, api, Member{
		ID: "m-box", Name: "box-1", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	putTestMember(t, api, Member{
		ID: "m-person", Name: "a member", Kind: KindStaff, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})

	// Positive control FIRST: without it, a test where every arm is refused
	// cannot tell "the guard works" from "the endpoint is broken".
	wardenTok, err := api.mintWardenToken(Member{ID: "m-box", Kind: KindWarden})
	if err != nil {
		t.Fatalf("mint warden credential: %v", err)
	}
	if code, body := revokeCall(t, "POST", srv.URL+renewPath, wardenTok, ""); code != http.StatusOK {
		t.Fatalf("positive control failed — an ACTIVE machine could not renew: %d %s",
			code, body)
	}

	agentTok, err := api.mintAgentToken("m-person", "", 3600)
	if err != nil {
		t.Fatalf("mint agent token: %v", err)
	}
	if code, body := revokeCall(t, "POST", srv.URL+renewPath, agentTok, ""); code != http.StatusForbidden {
		t.Errorf("an ordinary member renewed a MACHINE credential: want 403, got %d %s — "+
			"principalMachine is the lowest rank, so the route choke lets this caller "+
			"through and the handler is the only guard", code, body)
	}
}

// TestDeletedMachineIsTurnedAwayByTheAuthGateNotByThisHandler is property ③,
// and its shape is the whole point.
//
// TWO independent gates refuse a deleted machine here: requireAuth's revocation
// check (401, naming the machine) and, behind it, resolveMachine's own
// RosterStatusActive test (403). A test that only asserted "not 200" would be
// satisfied by EITHER — so it would keep passing after the auth-layer gate
// stopped covering this route, and the failure it was written to catch would
// ship silently. Measured, not assumed: with the revocation call stubbed out,
// a not-200 assertion stayed green because the handler had quietly taken over.
//
// So this pins WHICH gate answers. The status code and the message are what
// tell them apart, and the message is compared against the same helper the auth
// layer builds it with rather than a copy of its wording.
func TestDeletedMachineIsTurnedAwayByTheAuthGateNotByThisHandler(t *testing.T) {
	srv, _, api := revokeStack(t)
	putTestMember(t, api, Member{
		ID: "m-box", Name: "box-1", Kind: KindWarden, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	})
	tok, err := api.mintWardenToken(Member{ID: "m-box", Kind: KindWarden})
	if err != nil {
		t.Fatalf("mint warden credential: %v", err)
	}

	if code, body := revokeCall(t, "POST", srv.URL+renewPath, tok, ""); code != http.StatusOK {
		t.Fatalf("BEFORE the delete this machine must be able to renew, got %d %s — "+
			"without this half, the refusal below proves nothing", code, body)
	}

	revokeMachine(t, api, "m-box")

	code, body := revokeCall(t, "POST", srv.URL+renewPath, tok, "")
	if code == http.StatusOK {
		t.Fatalf("a machine deleted from the roster renewed its own credential — "+
			"deletion is the ONLY revocation this system has, and a renewable "+
			"credential would make it permanent (got %d %s)", code, body)
	}
	if code != http.StatusUnauthorized {
		t.Fatalf("the deleted machine was refused by the HANDLER (%d %s), not by the "+
			"auth layer. Both refuse today, so a not-200 assertion would not have "+
			"noticed: measured with the revocation call stubbed out, the handler's "+
			"own RosterStatusActive test silently took over and a not-200 test "+
			"stayed green.", code, body)
	}

	// 🔴 WHICH auth-layer gate answers, and THE ANSWER FLIPPED IN T-fc53 第二段.
	//
	// It used to be permanentCredentialRefusal: warden credentials carried no exp
	// claim, so that gate fired first and answered the generic "invalid token",
	// and revocationRefusal's machine arm was never reached on this route. 第一段
	// left this assertion pinned to that wording with a note saying it was
	// EXPECTED TO FLIP once the expiry came back, and to re-measure rather than
	// re-point it. It came back; this is the re-measurement.
	//
	// The credential now carries an exp, so permanentCredentialRefusal returns
	// false on it and revocationRefusal is the ONLY gate standing here. The two
	// answer DIFFERENT sentences, which is what makes this a discriminator rather
	// than a rename: "invalid token" is permanentCredentialRefusal's, and
	// machineRevokedMsg names the machine. Asserting the named one therefore
	// fails if the permanent gate somehow answers again — e.g. if a future mint
	// drops the exp — instead of quietly accepting either.
	//
	// ⚠️ A credential minted BEFORE 第二段 has no exp and would still be answered
	// by permanentCredentialRefusal here. That path is not exercised in this test
	// because it is not the one a live machine reaches after this release; it is
	// pinned where it belongs, in the auth-layer files (auth_refusal_exits_t14_test.go).
	if want := machineRevokedMsg("m-box"); !strings.Contains(body, want) {
		t.Errorf("refusal wording moved: got %d %s\nwant %q, which is the sentence "+
			"revocationRefusal answers with. The generic \"invalid token\" here would "+
			"mean permanentCredentialRefusal answered instead, i.e. the credential "+
			"this route mints no longer carries an exp.",
			code, body, want)
	}
}
