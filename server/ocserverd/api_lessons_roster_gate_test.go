package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// api_lessons_roster_gate_test.go — T-2 follow-up.
//
// 🔴 THE HOLE, STATED AS THE THING A CALLER COULD ACTUALLY DO.
// Nothing on the two lessons WRITE routes ever compared role_key against the
// roster. So a caller with admin capability could:
//
//	POST /api/lessons/r-there-is-no-such-role  {"text": "…"}   → 200 OK
//
// and thereby create a REAL document that
//
//   - draws on the same lessons cap as every other one,
//   - is folded into no boot context (buildBootContext keys off the member's
//     ROSTER role), so no agent will ever read it,
//   - and is listed by peek_doc_sizes NOWHERE, because that listing walks
//     listRoleKeys() and this document hangs off nothing on it.
//
// Write succeeds, quota is spent, nobody can find it. That is the same disease
// the rest of T-2 removed — a drawer nobody opens — surviving on the one axis
// T-2 did not touch: not the classification, the ROLE NAME.
//
// 🔑 THE JUDGE IS "DOES THIS DOCUMENT HAVE A READER?", and both readers are
// enumerated rather than guessed at:
//
//  1. listRoleKeys() — not a lookalike of what peek_doc_sizes walks, it IS that
//     function, so a doc under such a key is on that page; and
//  2. the member roster — nothing cross-checks role_key at hire time, so a
//     member CAN carry an off-roster key, and resolveBootRoleKey reads that
//     same key, so buildBootContext folds the doc into that member's persona
//     on every wake.
//
//	A LESSONS WRITE THAT SUCCEEDS PRODUCES A DOCUMENT WITH AT LEAST ONE
//	READER — the peek_doc_sizes page, or some member's boot context.
//
// 🔴 A ROSTER-ONLY JUDGE WAS THE FIRST DRAFT, AND TWO EXISTING TESTS KILLED IT.
// It bought a crisper sentence at the cost of 404-ing a member carrying an
// off-roster role_key — taking the learning loop away from an agent actively
// using it, which is T-d483 verbatim (see api_lessons_notfound_test.go: "the
// lessons API answered not_found for an existing role, breaking the learning
// loop"). TestLessonsMCPDefaultsCloseTheLearningLoop and
// TestLessonsWriteArmB_AdminAgentWritesAnotherRolesLessons both went red on
// that draft. Worth recording because the failure was NOT visible by reading
// the gate: it needed the fixtures that already encode the office's history.

// rosterREST issues an authenticated REST call and returns (status, body).
func rosterREST(t *testing.T, srvURL, token, method, path, body string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srvURL+path, rdr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// TestLessonsWriteRefusesARoleTheRosterDoesNotCarry is the named assertion a
// mutant that removes requireLessonsRosterRole has to turn red.
func TestLessonsWriteRefusesARoleTheRosterDoesNotCarry(t *testing.T) {
	srv, _, secret := newLessonsTestServer(t)
	// The OWNER on purpose: it clears lessonsWriteAuthz for every role, so a
	// refusal seen here is the ROSTER gate and cannot be an authz refusal
	// wearing its coat. It is also the identity that could actually reach this
	// hole in production — everyone below admin is confined to its own
	// member's roster role_key, which by construction IS on the roster.
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	const ghost = "r-there-is-no-such-role"
	for _, tc := range []struct{ name, path, body string }{
		{"replace_lessons", "/api/lessons/" + ghost, `{"text":"a doc with no role to hang off"}`},
		{"patch_lessons", "/api/lessons/" + ghost + "/patch", `{"edits":[{"old":"","new":"a doc with no role to hang off"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := rosterREST(t, srv.URL, ownerTok, "POST", tc.path, tc.body)
			if status == http.StatusOK {
				t.Fatalf("writing lessons for a role the roster does not carry was ACCEPTED (200): %s\n"+
					"That 200 is a lie by omission: the document is real, it spends the lessons "+
					"cap, and it appears in NO listing and NO boot context. It is the drawer "+
					"nobody opens that this whole ticket exists to remove.", body)
			}
			if status != http.StatusNotFound {
				t.Errorf("answered %d, want 404 — 'there is no such role' is what GET "+
					"/api/roles/{role} already answers for the same name, and the refusal "+
					"should not invent a second vocabulary for it", status)
			}
			if !strings.Contains(body, ghost) {
				t.Errorf("the refusal does not name the role_key it refused: %s", body)
			}
			if !strings.Contains(body, "list_roles") {
				t.Errorf("the refusal does not say WHERE the valid names are (list_roles): %s. "+
					"A caller told only 'no' has to guess, and guessing is how the typo "+
					"gets sent again", body)
			}
		})
	}
}

// TestLessonsWriteImpliesDocSizesVisibility is the INVARIANT, asserted end to
// end rather than by reading the two functions and believing they agree.
//
// Both directions are checked, because only the pair is meaningful:
//   - a role the roster carries → the write lands AND the document is on the
//     peek_doc_sizes page (this is the positive control; without it a gate that
//     refused everything would pass the negative half); and
//   - a role the roster does not carry → the write is refused AND that name is
//     on no row of the page (so the refusal is not merely cosmetic — nothing
//     leaked through another seam).
func TestLessonsWriteImpliesDocSizesVisibility(t *testing.T) {
	srv, _, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	const marker = "T-2 follow-up roster invariant"
	// POSITIVE: a roster role.
	if status, body := rosterREST(t, srv.URL, ownerTok, "POST", "/api/lessons/assistant",
		`{"text":"`+marker+`"}`); status != http.StatusOK {
		t.Fatalf("positive control: a ROSTER role must still be writable, got %d: %s\n"+
			"A gate that refuses a real role has broken the learning loop, which costs "+
			"far more than the hole it was closing.", status, body)
	}
	sizesStatus, sizes := rosterREST(t, srv.URL, ownerTok, "GET", "/api/doc-sizes", "")
	if sizesStatus != http.StatusOK {
		t.Fatalf("GET /api/doc-sizes: %d %s", sizesStatus, sizes)
	}
	if !strings.Contains(sizes, `"assistant"`) {
		t.Fatalf("the role just written to is not on the doc-sizes page: %s\n"+
			"The invariant this change buys is exactly that a successful lessons write "+
			"is a visible lessons document. If this fails, the gate is guarding the "+
			"wrong roster.", sizes)
	}

	// NEGATIVE: a ghost role, refused, and absent from the page afterwards.
	const ghost = "r-ghost-role-nobody-carries"
	if status, _ := rosterREST(t, srv.URL, ownerTok, "POST", "/api/lessons/"+ghost,
		`{"text":"`+marker+`"}`); status != http.StatusNotFound {
		t.Fatalf("ghost role write answered %d, want 404", status)
	}
	_, sizesAfter := rosterREST(t, srv.URL, ownerTok, "GET", "/api/doc-sizes", "")
	if strings.Contains(sizesAfter, ghost) {
		t.Errorf("the ghost role appears on the doc-sizes page: %s", sizesAfter)
	}
}

// TestLessonsRosterGateAcceptsASeedRoleWithATombstonedOverlay pins the ONE
// roster edge case a hand-rolled "does a role_def row exist" check would get
// wrong — and it is not hypothetical: on the live station today `assistant` is
// exactly this shape (a seed role whose role_def overlay carries tombstoned=1,
// meaning the owner reset it back to the factory text).
//
// listRoleKeys() seeds the roster from seedRoleKeys() FIRST and only then folds
// in non-tombstoned custom overlays, so a reset seed role is still a role. A
// gate that asked the role_def table instead would 404 the office's own
// assistant and take its lessons doc — the largest on the station — offline.
func TestLessonsRosterGateAcceptsASeedRoleWithATombstonedOverlay(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	if err := dal.PutRoleDef(RoleDef{RoleKey: "assistant", Name: "Assistant", Tombstoned: true}); err != nil {
		t.Fatalf("PutRoleDef: %v", err)
	}
	// The precondition, asserted rather than assumed: the roster still carries it.
	keysStatus, keys := rosterREST(t, srv.URL, ownerTok, "GET", "/api/roles", "")
	if keysStatus != http.StatusOK || !strings.Contains(keys, `"assistant"`) {
		t.Fatalf("precondition: a tombstoned seed role must still be on the roster; got %d %s", keysStatus, keys)
	}
	if status, body := rosterREST(t, srv.URL, ownerTok, "POST", "/api/lessons/assistant",
		`{"text":"a reset seed role is still a role"}`); status != http.StatusOK {
		t.Fatalf("a seed role with a tombstoned overlay was refused: %d %s\n"+
			"The roster is listRoleKeys(), not the role_def table. Asking the table "+
			"instead 404s the office's own assistant.", status, body)
	}
}

// TestLessonsRosterGateDoesNotDisplaceAuthz keeps the two refusals in the right
// ORDER and the right vocabulary.
//
// A below-admin agent may write only its OWN role's lessons. It must keep
// getting 403 for someone else's role — including a role that does not exist —
// rather than the roster gate's 404, because a 404 there would turn this route
// into a way to ask "does role X exist?" from an identity that could never have
// written to it. Authz is judged first for exactly that reason.
func TestLessonsRosterGateDoesNotDisplaceAuthz(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	now := time.Now().Unix()
	// 🔑 The plain agent must NOT carry role_key "assistant": that key IS the
	// admin_agent principal (T-5336), so an "assistant" member would clear
	// lessonsWriteAuthz for every role and this test would be measuring the
	// admin path while claiming to measure the plain one.
	const plainRole = "r-plain-roster-gate"
	if err := dal.PutRoleDef(RoleDef{
		RoleKey: plainRole, Name: "Plain", DefinitionMD: "plain\n",
	}); err != nil {
		t.Fatalf("PutRoleDef: %v", err)
	}
	if err := dal.PutMember(Member{
		ID: "joey", Kind: KindStaff, RoleKey: plainRole,
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	joeyTok, _ := mintJWT("joey", "agent", 300, secret, now, "")

	for _, tc := range []struct{ name, role string }{
		{"another_real_role", "r-someone-else"},
		{"a_role_that_does_not_exist", "r-there-is-no-such-role"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := rosterREST(t, srv.URL, joeyTok, "POST", "/api/lessons/"+tc.role, `{"text":"nope"}`)
			if status != http.StatusForbidden {
				t.Errorf("a below-admin agent writing another role answered %d, want 403: %s\n"+
					"Authz must be judged BEFORE the roster, or this route becomes a role "+
					"existence oracle for identities that could never write to it.", status, body)
			}
		})
	}

	// Positive control: its OWN role still writes, so the 403s above are about
	// the target and not about this identity being unable to write at all.
	if status, body := rosterREST(t, srv.URL, joeyTok, "POST", "/api/lessons/"+plainRole,
		`{"text":"its own role still writes"}`); status != http.StatusOK {
		t.Fatalf("positive control: an agent must still write its OWN role's lessons, got %d: %s", status, body)
	}
}

// TestLessonsReadOfAnUnknownRoleIsStillServed states the deliberate limit of
// this change, so a later reader does not "finish the job" by mistake.
//
// The defect is a WRITE that spends cap and then hides. A READ spends nothing
// and hides nothing: get_lessons on an unknown role folds to the seed and
// answers 200, exactly as before. Narrowing reads would break the MCP identity
// fold's own fallback path and buy nothing.
func TestLessonsReadOfAnUnknownRoleIsStillServed(t *testing.T) {
	srv, _, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	if status, body := rosterREST(t, srv.URL, ownerTok, "GET",
		"/api/lessons/r-there-is-no-such-role", ""); status != http.StatusOK {
		t.Errorf("reading an unknown role's lessons answered %d, want 200 (unchanged): %s", status, body)
	}
}

// TestMemberCarriedRoleIsAddressableEvenThoughItCannotBoot pins the exact
// mechanism the two earlier versions of this gate each got wrong in opposite
// directions. It is the experiment, kept executable, so nobody has to trust a
// paragraph again.
//
// The member is built THROUGH THE PRODUCTION ROUTES — hire with a role_key that
// names no role, then mint — which is precisely how conformance's auth matrix
// builds its ordinary agents. Three facts, measured together because only
// together do they mean anything:
//
//	boot  → 404   the role folds to nothing, so no boot can ever load this doc
//	read  → 200   BUT the owner can mint that member a token (/api/mint does not
//	              go through the boot fold), and get_lessons serves it
//	write → 200   therefore the doc IS addressable and must not be refused
//
// 🔴 Draft (a) of this gate accepted the write while claiming the doc was
// "folded into that member's persona on every wake" — the boot assertion below
// is what falsifies that. 🔴 Draft (b) then refused the write on the ground that
// such a member has no reader at all — the read assertion below is what
// falsifies THAT, and it is the fact /api/mint contributes.
func TestMemberCarriedRoleIsAddressableEvenThoughItCannotBoot(t *testing.T) {
	srv, _, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	const offRoster = "conf-shaped-role-naming-no-role"
	status, body := rosterREST(t, srv.URL, ownerTok, "POST", "/api/members",
		`{"name":"Off Roster Agent","kind":"staff","role_key":"`+offRoster+`"}`)
	if status != http.StatusOK {
		t.Fatalf("hire through the production route failed: %d %s", status, body)
	}
	memberID := jsonStringField(t, body, "id")

	// PRECONDITION: the key really is off the ROLE roster, or this test is
	// measuring something else entirely.
	if rs, roles := rosterREST(t, srv.URL, ownerTok, "GET", "/api/roles", ""); rs != http.StatusOK ||
		strings.Contains(roles, offRoster) {
		t.Fatalf("precondition broken: %q is on the role roster (%d): %s", offRoster, rs, roles)
	}

	// ① CANNOT BOOT — falsifies draft (a)'s stated reason.
	if bs, bb := rosterREST(t, srv.URL, ownerTok, "POST", "/api/bootstrap",
		`{"member_id":"`+memberID+`"}`); bs != http.StatusNotFound {
		t.Errorf("a member whose role_key folds to no role BOOTED: %d %s\n"+
			"If this ever succeeds, the 'cannot boot' half of this gate's reasoning "+
			"has changed and the wording must be revisited.", bs, bb)
	}

	// ② CAN BE MINTED A TOKEN AND CAN READ — falsifies draft (b)'s conclusion.
	//    /api/mint is a THIRD token path that does not consult the boot fold.
	ms, mb := rosterREST(t, srv.URL, ownerTok, "POST", "/api/mint",
		`{"member_id":"`+memberID+`","ttl_days":1}`)
	if ms != http.StatusOK {
		t.Fatalf("owner mint for that member failed: %d %s — without a token the "+
			"read assertion below cannot run and this test proves nothing", ms, mb)
	}
	agentTok := jsonStringField(t, mb, "token")
	if rs, rb := rosterREST(t, srv.URL, agentTok, "GET", "/api/lessons/"+offRoster, ""); rs != http.StatusOK {
		t.Errorf("the member could NOT read its own lessons with its minted token: %d %s\n"+
			"That read is the whole reason this role_key is addressable at all.", rs, rb)
	}

	// ③ SO THE WRITE MUST LAND. Refusing it removes a capability conformance
	//    pins at 200 (test_auth_matrix, POST /api/lessons/{role_key}[agent_self]).
	if ws, wb := rosterREST(t, srv.URL, agentTok, "POST", "/api/lessons/"+offRoster,
		`{"text":"its own doc, readable by it"}`); ws != http.StatusOK {
		t.Fatalf("the agent could not write its OWN role's lessons: %d %s\n"+
			"This is the shape conformance's auth matrix builds through the public "+
			"API; refusing it breaks a wire-pinned capability.", ws, wb)
	}
}

// jsonStringField pulls one top-level string field out of a JSON body without
// a struct — the tests here only ever need an id or a token, and a decode would
// couple them to DTO shapes they are not about.
func jsonStringField(t *testing.T, body, field string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("decode %s: %v (body=%s)", field, err, body)
	}
	v, _ := m[field].(string)
	if v == "" {
		t.Fatalf("no %q in body: %s", field, body)
	}
	return v
}
