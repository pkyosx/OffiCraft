package main

// T-5336 — the admin agent (Mira) may write ANY role's lessons.
//
// THE DEFECT: lessonsWriteAuthz judged on the TOKEN SCOPE
// (`currentScope(r) != "agent"` → allow, else compare the caller's member
// role_key against the path role_key). Every member token — Mira's included —
// is minted with scope="agent" (api_auth.go), so the office's admin was folded
// into the self-role-only rule and 403'd on every role but her own
// ("assistant"). Her admin standing did not exist on this path at all.
//
// THREE ARMS, one test each. (b) and (c) together are what discriminates a
// fixed authz seam from a REMOVED one; (a) is documentation, and says so:
//
//	(a) TestLessonsWriteArmA_LegacyScopeRuleRefusedTheAdminCrossRole — a
//	    DOCUMENTARY RECORD of the retired rule, NOT a counterfactual experiment.
//	    ⚠️ It evaluates a copy of the old predicate that lives in this test file
//	    and calls NOTHING in production: mutate lessonsWriteAuthz however you
//	    like and this arm stays green, so it has ZERO discriminating power over
//	    the shipped code. What it is good for is pinning, in executable form,
//	    the two facts that explain the defect — the admin's token really is
//	    agent-scoped (read from a REAL minted token), and the old rule refused
//	    her on a foreign role while ALLOWING her on her own (which is why every
//	    pre-existing admin-face test, all aimed at "assistant", stayed green).
//	    Its one live assertion is a fixture control (READ is 200).
//	(b) TestLessonsWriteArmB_AdminAgentWritesAnotherRolesLessons — the fix.
//	(c) TestLessonsWriteArmC_PlainAgentStillRefusedAnotherRolesLessons — the
//	    guard is narrowed, not deleted: a PLAIN agent aiming at someone else's
//	    role is still a flat 403, on both write verbs.

import (
	"strings"
	"testing"
	"time"
)

// legacyScopeOnlyLessonsWriteAuthz is a TEST-SIDE TRANSCRIPTION of the retired
// predicate: the pre-T-5336 lessonsWriteAuthz body reduced to its decision
// (scope-based allow, else self-role-only). Nothing in production calls it, and
// nothing keeps it honest to the real history except this comment — it is a
// record, not a probe.
func legacyScopeOnlyLessonsWriteAuthz(scope, memberRoleKey, pathRoleKey string) bool {
	if scope != "agent" {
		return true
	}
	return memberRoleKey == pathRoleKey
}

// t5336Fixture stands up the wired lessons server with three identities and one
// foreign role that carries a lessons doc: the admin agent (role_key
// "assistant" — the adminRoleKey the resolver keys on), a plain agent, and the
// owner. Returns (server URL, admin token, plain-agent token, foreign role key).
func t5336Fixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	srv, dal, secret := newLessonsTestServer(t)
	now := time.Now().Unix()

	// The foreign role — NOT the admin's own role, NOT the plain agent's.
	const foreignRole = "r-t5336foreign"
	// T-2 follow-up: this fixture used to seed the doc WITHOUT ever creating
	// the role, so "another role's lessons" was in fact a lessons doc hanging
	// off no role at all — the exact artifact the roster gate now refuses.
	// Making it a real role is a fixture-realism fix, not a weakening: the
	// arms below assert reach ACROSS roles, and this role is still neither the
	// admin's own nor the plain agent's. Nothing in any assertion moved.
	if err := dal.PutRoleDef(RoleDef{
		RoleKey: foreignRole, Name: "T-5336 Foreign Role", DefinitionMD: "foreign role\n",
	}); err != nil {
		t.Fatalf("PutRoleDef(foreign): %v", err)
	}
	seedLessonsOverlay(t, dal, foreignRole, "foreign role baseline\n")

	if err := dal.PutMember(Member{
		ID: "mira-t5336", Kind: KindStaff, RoleKey: adminRoleKey,
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember(admin): %v", err)
	}
	// Same realism fix as the foreign role above: a plain agent that is making
	// requests at all must be on a role that can BOOT, or it could never have
	// obtained the token it is calling with.
	if err := dal.PutRoleDef(RoleDef{
		RoleKey: "r-t5336plain", Name: "T-5336 Plain Role", DefinitionMD: "plain\n",
	}); err != nil {
		t.Fatalf("PutRoleDef(plain): %v", err)
	}
	if err := dal.PutMember(Member{
		ID: "plain-t5336", Kind: KindStaff, RoleKey: "r-t5336plain",
		DesiredState: DesiredStateOnline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("PutMember(plain): %v", err)
	}
	adminTok, err := mintJWT("mira-t5336", "agent", 300, secret, now, "")
	if err != nil {
		t.Fatalf("mint admin token: %v", err)
	}
	plainTok, err := mintJWT("plain-t5336", "agent", 300, secret, now, "")
	if err != nil {
		t.Fatalf("mint plain token: %v", err)
	}
	return srv.URL, adminTok, plainTok, foreignRole
}

// ── arm (a) — the documentary record (no production code is exercised) ──────

func TestLessonsWriteArmA_LegacyScopeRuleRefusedTheAdminCrossRole(t *testing.T) {
	url, adminTok, _, foreignRole := t5336Fixture(t)

	// The two facts the retired rule ran on, read from the REAL credential and
	// the REAL roster row rather than asserted in prose.
	claims, err := verifyJWT(adminTok, []byte(interopSecret), time.Now().Unix())
	if err != nil {
		t.Fatalf("verify admin token: %v", err)
	}
	scope, _ := claims["scope"].(string)
	if scope != "agent" {
		t.Fatalf("the premise of this defect is that an ADMIN token's scope is "+
			"still %q; got %q — if member tokens stopped being agent-scoped, "+
			"re-derive this whole arm", "agent", scope)
	}

	// The transcribed rule, on this exact triple: REFUSE. This is what shipped
	// before T-5336, and it is why Mira could not write another role. Failing
	// here means the TRANSCRIPTION drifted from the history it records — it says
	// nothing about the shipped predicate either way.
	if legacyScopeOnlyLessonsWriteAuthz(scope, adminRoleKey, foreignRole) {
		t.Fatalf("this record is mis-stated: the legacy scope-only rule REFUSED "+
			"an admin (scope=%q, role=%q) writing role %q", scope, adminRoleKey, foreignRole)
	}

	// And the legacy rule was not refusing everybody: the admin writing its OWN
	// role passed (which is exactly why the defect stayed invisible — every
	// existing admin-face test aimed at "assistant").
	if !legacyScopeOnlyLessonsWriteAuthz(scope, adminRoleKey, adminRoleKey) {
		t.Fatalf("this record is mis-stated: the legacy rule DID allow the admin " +
			"to write its own role")
	}

	// Live control on the same server: the READ face was never gated, so a 200
	// here proves the 403s in arm (c) come from the write authz, not from a
	// broken fixture.
	if status, _ := doJSON(t, "GET", url+"/api/lessons/"+foreignRole,
		adminTok, ""); status != 200 {
		t.Fatalf("fixture control: admin READ of the foreign role must be 200, got %d", status)
	}
}

// ── arm (b) — the fix ───────────────────────────────────────────────────────

func TestLessonsWriteArmB_AdminAgentWritesAnotherRolesLessons(t *testing.T) {
	url, adminTok, _, foreignRole := t5336Fixture(t)

	const marker = "T-5336 admin cross-role replace"
	status, data := doJSON(t, "POST", url+"/api/lessons/"+foreignRole,
		adminTok, `{"text":"`+marker+`"}`)
	if status != 200 {
		t.Fatalf("admin_agent must write ANOTHER role's lessons, got %d: %v", status, data)
	}
	if got := getLessonsText(t, url, adminTok, foreignRole); !strings.Contains(got, marker) {
		t.Fatalf("the replace must land; doc is now: %q", got)
	}

	// Both write verbs share lessonsWriteAuthz — pin the patch face too.
	status, data = patchLessons(t, url, adminTok, foreignRole,
		`{"edits":[{"old":"","new":"T-5336 admin cross-role patch"}]}`)
	if status != 200 {
		t.Fatalf("admin_agent must PATCH another role's lessons, got %d: %v", status, data)
	}

	// The MCP loopback re-enters the same mux, so the tool face must agree.
	if isErr, code, text := lessonsCall(t, url, adminTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"replace_lessons",`+
			`"arguments":{"role_key":"`+foreignRole+`","text":"T-5336 admin via MCP"}}}`); isErr {
		t.Fatalf("admin_agent replace_lessons over MCP must land, code=%q body=%s", code, text)
	}
}

// ── arm (c) — the guard is narrowed, not deleted ────────────────────────────

func TestLessonsWriteArmC_PlainAgentStillRefusedAnotherRolesLessons(t *testing.T) {
	url, _, plainTok, foreignRole := t5336Fixture(t)

	status, data := doJSON(t, "POST", url+"/api/lessons/"+foreignRole,
		plainTok, `{"text":"plain agent poison attempt"}`)
	if status != 403 {
		t.Fatalf("a PLAIN agent writing ANOTHER role's lessons must stay 403, got %d: %v",
			status, data)
	}
	if status, data = patchLessons(t, url, plainTok, foreignRole,
		`{"edits":[{"old":"","new":"plain agent poison attempt"}]}`); status != 403 {
		t.Fatalf("a PLAIN agent patching ANOTHER role's lessons must stay 403, got %d: %v",
			status, data)
	}

	// Nothing was written — a 403 that still mutated would be the worse bug.
	if got := getLessonsText(t, url, plainTok, foreignRole); strings.Contains(got, "poison") {
		t.Fatalf("the refused write must leave the doc untouched; doc is now: %q", got)
	}

	// The same agent writing its OWN role still passes: arm (c) must fail on a
	// removed guard, not on a blanket lockout.
	if status, data := doJSON(t, "POST", url+"/api/lessons/r-t5336plain",
		plainTok, `{"text":"own role, still allowed"}`); status != 200 {
		t.Fatalf("a plain agent must still write its OWN role, got %d: %v", status, data)
	}
}
