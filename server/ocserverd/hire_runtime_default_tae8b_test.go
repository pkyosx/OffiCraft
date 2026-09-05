package main

// hire_runtime_default_tae8b_test.go — the runtime a CREATION path writes when
// the caller names none (T-ae8b).
//
// T-b3d0 made "" the durable "nobody has picked yet" and taught placement to
// resolve it against the machine the member actually lands on
// (resolveEmptyRuntimeForPlacement). Every creation path then wrote a literal
// claude, so that resolver was reachable only from the out-of-box seed — hire a
// member on a codex-only box and it was born claude and died at spawn.
//
// The capability fixture is the REAL warden shape (codexOnlyRuntimes, defined
// in reconcile_runtime_resolution_tb3d0_test.go): claude present with
// installed:false, not a map with the claude key missing.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// hireMember posts POST /api/members as the owner and returns the minted id.
func hireMember(t *testing.T, s *apiServer, body map[string]any) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleHireMemberApiMembersPost(rec,
		taskReq(t, http.MethodPost, "/api/members", body, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("hire: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var hired memberDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &hired); err != nil {
		t.Fatalf("decode hire result: %v", err)
	}
	return hired.ID
}

// createRole posts POST /api/roles as the owner and returns the founding
// member's minted id.
func createRole(t *testing.T, s *apiServer, body map[string]any) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandleCreateRoleApiRolesPost(rec,
		taskReq(t, http.MethodPost, "/api/roles", body, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create role: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created roleCreateResultDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode role result: %v", err)
	}
	return created.MemberID
}

// TestHireMember_LeavesRuntimeUnsetWhenCallerNamesNone pins the hire seam
// (MCP/API). A hire that names no runtime must persist "", so placement — not
// this handler — picks.
func TestHireMember_LeavesRuntimeUnsetWhenCallerNamesNone(t *testing.T) {
	s := newReconcileTestServer(t)

	id := hireMember(t, s, map[string]any{"name": "Nova"})

	if got := storedRuntime(t, s, id); got != "" {
		t.Fatalf("hired member runtime = %q, want \"\" (unset): a hire that named "+
			"no runtime must leave the choice to resolveEmptyRuntimeForPlacement, "+
			"not pin the member to a runtime at birth", got)
	}
}

// TestCreateRole_LeavesFoundingMemberRuntimeUnset pins the path the cockpit's
// 招攬新成員 button really uses: SettingsPage sends {name} only.
func TestCreateRole_LeavesFoundingMemberRuntimeUnset(t *testing.T) {
	s := newReconcileTestServer(t)

	id := createRole(t, s, map[string]any{"name": "研究員"})

	if got := storedRuntime(t, s, id); got != "" {
		t.Fatalf("founding member runtime = %q, want \"\" (unset): 招攬新成員 sends "+
			"only a name, so this creation path must not pin a runtime either", got)
	}
}

// TestHireMember_KeepsTheRuntimeTheCallerNamed — an explicit choice is still
// the owner's, on both creation paths. Unsetting the default must not have
// turned into ignoring the field.
func TestHireMember_KeepsTheRuntimeTheCallerNamed(t *testing.T) {
	s := newReconcileTestServer(t)

	for _, runtime := range []string{RuntimeClaude, RuntimeCodex} {
		hired := hireMember(t, s, map[string]any{"name": "Pin" + runtime, "runtime": runtime})
		if got := storedRuntime(t, s, hired); got != runtime {
			t.Fatalf("hired member runtime = %q, want the named %q", got, runtime)
		}
		founding := createRole(t, s, map[string]any{"name": "Role" + runtime, "runtime": runtime})
		if got := storedRuntime(t, s, founding); got != runtime {
			t.Fatalf("founding member runtime = %q, want the named %q", got, runtime)
		}
	}
}

// TestHiredMemberIsBornCodexOnACodexOnlyHost is the ticket's actual promise,
// end to end: hire on a box that reports only codex, and the member placed
// there runs codex — on the roster row and on the START frame — with nobody
// having typed a runtime anywhere.
func TestHiredMemberIsBornCodexOnACodexOnlyHost(t *testing.T) {
	s := newReconcileTestServer(t)
	connectOnline(t, s, ServerSelfHost)
	if rec := doIngestTelemetry(s, ServerSelfHost, ServerSelfHost,
		codexOnlyRuntimes); rec.Code != 200 {
		t.Fatalf("telemetry ingest: %d %s", rec.Code, rec.Body.String())
	}
	id := hireMember(t, s, map[string]any{"name": "Rin"})
	m, err := s.dal.GetMember(id)
	if err != nil || m == nil {
		t.Fatalf("get hired member: %v", err)
	}
	m.DesiredState = DesiredStateOnline
	m.DesiredMachineID = ServerSelfHost
	putTestMember(t, s, *m)

	decision := s.reconcileOne(*m, reconcileState{}, 1000)

	if decision.Command != reconcileCmdStart {
		t.Fatalf("want a dispatched START, got command %q reason %q",
			decision.Command, decision.Reason)
	}
	if got := storedRuntime(t, s, id); got != RuntimeCodex {
		t.Fatalf("hired member roster runtime = %q, want %q: a member hired on a "+
			"codex-only box must be resolved to codex at placement", got, RuntimeCodex)
	}
	if got := startFrameRuntime(t, s, ServerSelfHost); got != RuntimeCodex {
		t.Fatalf("START frame runtime = %q, want %q", got, RuntimeCodex)
	}
}
