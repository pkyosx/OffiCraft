package main

// member_resume_summary_t8b0d_test.go — GET /api/members/{member_id}/resume-summary
// (T-8b0d): the SAME bounded wake snapshot as GET /api/resume-summary, for a
// TARGET member instead of the caller. The acceptance criterion is "same
// payload, not an approximation" — the handler must call the existing,
// unmodified resumeSnapshotParts(targetID), not a near-copy of the assembly.
// This file proves that by fetching the same member's snapshot through BOTH
// routes and diffing the raw bytes.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// resumeSummaryHTTPGet issues a bearer-authed GET and returns (status, raw body).
func resumeSummaryHTTPGet(t *testing.T, url, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// hireResumeSummaryTarget hires a fresh plain member (owner-authed) and
// returns its id — a live roster member distinct from the seeded "mira".
func hireResumeSummaryTarget(t *testing.T, srv, ownerTok, name string) string {
	t.Helper()
	req, err := http.NewRequest("POST", srv+"/api/members",
		strings.NewReader(`{"name":"`+name+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ownerTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hire target member: %d %s", resp.StatusCode, body)
	}
	var dto struct {
		Id string `json:"id"`
	}
	if err := json.Unmarshal(body, &dto); err != nil || dto.Id == "" {
		t.Fatalf("hire target member: could not read id from %s (%v)", body, err)
	}
	return dto.Id
}

// postResumeSummaryChat posts one chat message AS the given sub (its own
// token) so the target's resume-summary snapshot carries real content —
// otherwise an equality check on two empty snapshots would be undiscriminating.
func postResumeSummaryChat(t *testing.T, srv, token, to, body string) {
	t.Helper()
	req, err := http.NewRequest("POST", srv+"/api/chat",
		strings.NewReader(`{"to":"`+to+`","body":"`+body+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post chat: %d %s", resp.StatusCode, respBody)
	}
}

// TestGetMemberResumeSummary_OwnerToken200 — an owner-scoped token pulling a
// TARGET member's snapshot gets 200 with that member's identity, not the
// caller's.
func TestGetMemberResumeSummary_OwnerToken200(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")

	target := hireResumeSummaryTarget(t, srv.URL, ownerTok, "resume-target-owner")

	status, body := resumeSummaryHTTPGet(t, srv.URL+"/api/members/"+target+"/resume-summary", ownerTok)
	if status != http.StatusOK {
		t.Fatalf("owner on new route: want 200, got %d %s", status, body)
	}
	if !strings.Contains(string(body), `"identity":"`+target+`"`) {
		t.Fatalf("response identity must be the TARGET (%s), got %s", target, body)
	}
}

// TestGetMemberResumeSummary_AdminRoleMember200 — a non-owner caller whose own
// roster row is role_key=="assistant" (the seeded Mira) also clears the
// admin_agent floor.
func TestGetMemberResumeSummary_AdminRoleMember200(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")
	miraTok, _ := mintJWT("mira", "agent", 300, secret, now, "")

	target := hireResumeSummaryTarget(t, srv.URL, ownerTok, "resume-target-admin")

	status, body := resumeSummaryHTTPGet(t, srv.URL+"/api/members/"+target+"/resume-summary", miraTok)
	if status != http.StatusOK {
		t.Fatalf("admin-role member (mira) on new route: want 200, got %d %s", status, body)
	}
	if !strings.Contains(string(body), `"identity":"`+target+`"`) {
		t.Fatalf("response identity must be the TARGET (%s), got %s", target, body)
	}
}

// TestGetMemberResumeSummary_OrdinaryAgent403 — a plain agent-scope caller
// (unseeded sub, deny-by-default classifies as principalAgent) is refused
// BEFORE any target resolution, with the standard forbidden envelope.
func TestGetMemberResumeSummary_OrdinaryAgent403(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	agentTok, _ := mintJWT("kyle", "agent", 300, secret, now, "")

	status, body := resumeSummaryHTTPGet(t, srv.URL+"/api/members/mira/resume-summary", agentTok)
	if status != http.StatusForbidden || !strings.Contains(string(body), `"code":"forbidden"`) {
		t.Fatalf("plain agent on new route: want 403 forbidden envelope, got %d %s", status, body)
	}
}

// TestGetMemberResumeSummary_UnknownMember404 — an admin-capable caller
// against a member_id that does not resolve to a live roster member (never
// existed) gets 404, not a zero-value snapshot.
func TestGetMemberResumeSummary_UnknownMember404(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")

	status, body := resumeSummaryHTTPGet(t, srv.URL+"/api/members/m-does-not-exist/resume-summary", ownerTok)
	if status != http.StatusNotFound {
		t.Fatalf("unknown member_id: want 404, got %d %s", status, body)
	}
}

// TestGetMemberResumeSummary_SameAssemblyAsSelfScoped is the hard acceptance
// criterion: the payload the admin route serves for a target member must be
// produced by the SAME resumeSnapshotParts assembly the target's own
// self-scoped /api/resume-summary uses — not a near-copy. Proven by fetching
// the SAME member's snapshot through both routes and diffing raw bytes, with
// real chat content in the fixture so an accidental "both are empty" pass is
// not possible.
func TestGetMemberResumeSummary_SameAssemblyAsSelfScoped(t *testing.T) {
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	ownerTok, _ := mintJWT(wireOwnerID, "owner", 300, secret, now, "")

	target := hireResumeSummaryTarget(t, srv.URL, ownerTok, "resume-target-parity")
	targetTok, _ := mintJWT(target, "agent", 300, secret, now, "")

	// Give the target's snapshot real content: a chat message it sent itself,
	// so resumeSnapshotParts(target) has a non-trivial chat array + chat_chars.
	const marker = "T-8b0d parity marker — do not drop"
	postResumeSummaryChat(t, srv.URL, targetTok, "mira", marker)

	selfStatus, selfBody := resumeSummaryHTTPGet(t, srv.URL+"/api/resume-summary", targetTok)
	if selfStatus != http.StatusOK {
		t.Fatalf("self-scoped resume-summary: want 200, got %d %s", selfStatus, selfBody)
	}
	if !strings.Contains(string(selfBody), marker) {
		t.Fatalf("fixture is not discriminating — marker missing from self-scoped body: %s", selfBody)
	}

	adminStatus, adminBody := resumeSummaryHTTPGet(t, srv.URL+"/api/members/"+target+"/resume-summary", ownerTok)
	if adminStatus != http.StatusOK {
		t.Fatalf("admin-scoped resume-summary: want 200, got %d %s", adminStatus, adminBody)
	}

	if string(selfBody) != string(adminBody) {
		t.Fatalf("admin-route payload must be BYTE-IDENTICAL to the target's own "+
			"self-scoped payload (same resumeSnapshotParts assembly, not a "+
			"near-copy):\nself-scoped:  %s\nadmin-scoped: %s", selfBody, adminBody)
	}
}

// TestGetMemberResumeSummary_RouteSpecPinnedAdminAgent — table-level: the new
// row sits at requires=admin_agent (control-others floor per root CLAUDE.md「核心不變量／授權單一化」), exposes
// the MCPTool get_member_resume_summary, and — critically — the pre-existing
// /api/resume-summary row is untouched by this addition (still the
// identity-locked floor row, still no MCPTool name change).
func TestGetMemberResumeSummary_RouteSpecPinnedAdminAgent(t *testing.T) {
	specs := defaultRouteSpecs()
	if len(specs) == 0 {
		t.Fatalf("empty route table — every assertion below would be vacuous")
	}
	var newRoute, selfRoute *RouteSpec
	for i := range specs {
		s := specs[i]
		switch {
		case s.Method == "GET" && s.Path == "/api/members/{member_id}/resume-summary":
			newRoute = &s
		case s.Method == "GET" && s.Path == "/api/resume-summary":
			selfRoute = &s
		}
	}
	if newRoute == nil {
		t.Fatal("GET /api/members/{member_id}/resume-summary must be in the route table")
	}
	if selfRoute == nil {
		t.Fatal("GET /api/resume-summary must still be in the route table")
	}
	if newRoute.Requires != principalAdminAgent {
		t.Fatalf("new route Requires: want %q, got %q", principalAdminAgent, newRoute.Requires)
	}
	if newRoute.MCPTool != "get_member_resume_summary" {
		t.Fatalf("new route MCPTool: want %q, got %q", "get_member_resume_summary", newRoute.MCPTool)
	}
	if newRoute.MCPExclude {
		t.Fatalf("new route must be a real MCP tool, not MCPExclude")
	}
	// The self-scoped route's own identity-lock floor must be untouched.
	if selfRoute.Requires != principalMachine {
		t.Fatalf("self-scoped /api/resume-summary Requires must remain %q (untouched by this addition), got %q",
			principalMachine, selfRoute.Requires)
	}
	if selfRoute.MCPTool != "resume_summary" {
		t.Fatalf("self-scoped /api/resume-summary MCPTool must remain %q (untouched), got %q",
			"resume_summary", selfRoute.MCPTool)
	}
}
