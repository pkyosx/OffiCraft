package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// role_delete_contractor_t14i6_test.go — T-14 項目 6, the named debt made
// falsifiable.
//
// DELETE /api/roles/{role} reads dal.ListMembers() and hard-deletes every row
// whose RoleKey matches — the member, its whole conversation, its receipts, its
// lessons. Since T-14 項目 6 that roster read is the WHOLE member table
// (`WHERE kind != 'outsource'` is gone), so contractors are now IN the scanned
// population for the first time.
//
// The ONLY thing keeping them out of the cascade is that an outsource row's
// role_key is the empty string and a custom role's key never is
// (memberFromWorker hardcodes RoleKey: "" — dal_tasks.go). That is a true
// statement about today's code and a fragile one: T-14's own direction is
// 外包＝正職, and the day a contractor gets a role, this handler hard-deletes a
// LIVE contractor together with its chat, with no confirmation and no receipt.
// The 409 online gate does not save it either — it only fires for a contractor
// that is currently holding an SSE connection, so an ACTIVE but momentarily
// offline worker walks straight into the cascade.
//
// This file is that debt's tooth. It does not fix anything: it makes the day
// the assumption stops holding a RED TEST rather than a silent deletion.

// TestDeleteRole_ACustomRoleCascadeCannotReachAContractor drives the real
// handler with a contractor sitting in the roster.
func TestDeleteRole_ACustomRoleCascadeCannotReachAContractor(t *testing.T) {
	api := newTasksTestServer(t)
	api.noOutsource = true
	const role = "r-cascade-contractor"

	if err := api.dal.PutRoleDef(RoleDef{
		RoleKey: role, Name: role, DefinitionMD: "v0",
	}); err != nil {
		t.Fatalf("seed role: %v", err)
	}

	// The positive control: a STAFF member wearing the role. If the cascade does
	// not take this one, a surviving contractor proves nothing — it would just
	// mean the cascade did not run.
	if err := api.dal.PutMember(Member{
		ID: "m-wearer", Name: "wearer", Kind: KindStaff, RoleKey: role,
		RosterStatus: RosterStatusActive, DesiredState: DesiredStateOffline,
	}); err != nil {
		t.Fatalf("seed staff wearer: %v", err)
	}

	// The row under test: an ACTIVE contractor. Deliberately NOT online, because
	// the handler's 409 gate only refuses ONLINE members — an active-but-offline
	// contractor is the shape that would be hard-deleted silently.
	if err := api.dal.PutOutsourceWorker(OutsourceWorker{
		ID: "ow-bystander", Codename: "B-1", Runtime: RuntimeClaude, Model: "opus",
		Effort: "medium", TaskID: "t-1", Status: WorkerStatusActive,
		CreatedTS: 1.0, DesiredState: DesiredStateOnline,
	}); err != nil {
		t.Fatalf("seed contractor: %v", err)
	}

	// The premise. If ListMembers ever narrows again, the cascade stops seeing
	// contractors for a reason that has nothing to do with role_key, and this
	// test would go green while testing nothing.
	roster, err := api.dal.ListMembers()
	if err != nil {
		t.Fatalf("roster read: %v", err)
	}
	sawContractor, sawWearer := false, false
	for _, m := range roster {
		switch m.ID {
		case "ow-bystander":
			sawContractor = true
			if m.RoleKey != "" {
				t.Fatalf("the contractor's role_key is %q, not \"\" — the assumption "+
					"this whole cascade rests on has changed. That is the DEBT COMING "+
					"DUE, not a broken fixture: DELETE /api/roles/{role} now has a path "+
					"to hard-delete a live contractor and its entire chat with no "+
					"confirmation. Give this handler an explicit "+
					"`if m.Kind == KindOutsource { continue }` (or an owner-gated "+
					"decision about what a contractor with a role should mean) BEFORE "+
					"relaxing this assertion.", m.RoleKey)
			}
		case "m-wearer":
			sawWearer = true
		}
	}
	if !sawContractor || !sawWearer {
		t.Fatalf("the cascade's roster read did not contain both rows "+
			"(contractor=%v wearer=%v) — the scan cannot be shown to have spared "+
			"anything it never looked at", sawContractor, sawWearer)
	}

	rec := httptest.NewRecorder()
	api.HandleDeleteRoleApiRolesRoleDelete(rec, taskReq(t, http.MethodDelete,
		"/api/roles/"+role, nil, "owner", "owner"), role)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete role: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Positive control fires first.
	gone, err := api.dal.GetMember("m-wearer")
	if err != nil {
		t.Fatalf("reload wearer: %v", err)
	}
	if gone != nil {
		t.Fatalf("POSITIVE CONTROL FAILED: the staff member wearing %s survived the "+
			"cascade, so this test cannot tell 「外包被放過了」 from 「連鎖刪除根本沒跑」",
			role)
	}

	survivor, err := api.dal.GetMember("ow-bystander")
	if err != nil {
		t.Fatalf("reload contractor: %v", err)
	}
	if survivor == nil {
		t.Errorf("deleting the custom role %s HARD-DELETED an unrelated contractor. "+
			"Since T-14 項目 6 this handler scans the whole member table; the only "+
			"thing that was keeping contractors out of the cascade is role_key == "+
			"\"\", and that is now false or bypassed.", role)
	}
}

// TestMemberFromWorker_AContractorCarriesNoRoleKey pins the assumption at its
// source, one layer below the handler. The test above needs a whole HTTP
// cascade to fail; this one fails the moment the projection changes, which is
// where the change would actually be written.
func TestMemberFromWorker_AContractorCarriesNoRoleKey(t *testing.T) {
	m := memberFromWorker(OutsourceWorker{
		ID: "ow-proj", Codename: "P-1", Runtime: RuntimeClaude, Model: "opus",
		Effort: "medium", TaskID: "t-1", Status: WorkerStatusActive, CreatedTS: 1.0,
	})
	if m.Kind != KindOutsource {
		t.Fatalf("memberFromWorker produced kind=%q — fixture is wrong", m.Kind)
	}
	if m.RoleKey != "" {
		t.Errorf("memberFromWorker set role_key=%q. TWO readers of dal.ListMembers "+
			"quietly depend on this being \"\" now that the roster read is merged "+
			"(T-14 項目 6): DELETE /api/roles/{role}'s hard-delete cascade "+
			"(api_roles.go — the expensive one), and the role-name collision scan "+
			"in the create-role handler (which today is IMPROVED by seeing "+
			"contractors — it stops a new staff codename colliding with a live "+
			"contractor's — so the two readers want opposite things). "+
			"Giving contractors a role is a legitimate direction, but it is an "+
			"OWNER-GATED one and each of those readers has to be re-decided first. "+
			"Do not just update this expectation.", m.RoleKey)
	}
}
