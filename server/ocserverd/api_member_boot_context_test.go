package main

import (
	"strings"
	"testing"
)

const (
	memberPreviewRoleKey      = "preview-writer"
	memberPreviewClaudeBoot   = "# CLAUDE BOOT STEPS FOR THE PREVIEW"
	memberPreviewCodexBoot    = "# CODEX BOOT STEPS FOR THE PREVIEW"
	memberPreviewEveryoneLore = "EVERYONE LORE BODY FOR THE PREVIEW"
)

// memberPreviewSeed stores a role, both runtimes' boot steps, the given staff
// rows, one everyone entry and one own entry per member, all through the DAL.
func memberPreviewSeed(t *testing.T, d *DAL, members ...Member) {
	t.Helper()
	if err := d.PutRoleDef(RoleDef{
		RoleKey: memberPreviewRoleKey, Name: "Preview Writer", DefinitionMD: "# Preview role instructions",
	}); err != nil {
		t.Fatalf("PutRoleDef: %v", err)
	}
	for _, doc := range []BootDocument{
		{Kind: docKindBootSequence, Key: bootSequenceKeyClaude, Text: memberPreviewClaudeBoot},
		{Kind: docKindBootSequence, Key: bootSequenceKeyCodex, Text: memberPreviewCodexBoot},
	} {
		if err := d.PutBootDocument(doc); err != nil {
			t.Fatalf("PutBootDocument(%s): %v", doc.Key, err)
		}
	}
	entries := []LoreEntry{{ScopeKind: LoreScopeEveryone, Title: "everyone", Body: memberPreviewEveryoneLore}}
	for _, m := range members {
		if m.Kind == "" {
			m.Kind = KindStaff
		}
		if m.RosterStatus == "" {
			m.RosterStatus = RosterStatusActive
		}
		if err := d.putMemberWholeRowForTest(m); err != nil {
			t.Fatalf("PutMember(%s): %v", m.ID, err)
		}
		entries = append(entries, LoreEntry{
			ScopeKind: LoreScopeAgent, ScopeKey: m.ID, Title: "own", Body: "OWN LORE OF " + m.ID,
		})
	}
	for _, e := range entries {
		e.AuthorID, e.State = "owner", LoreStateActive
		e.EffectiveTS, e.CreatedTS, e.UpdatedTS = 1, 1, 1
		e.LoreType = LoreTypeOther
		if _, err := d.CreateLoreEntryMintingID(e); err != nil {
			t.Fatalf("CreateLoreEntryMintingID(%s): %v", e.Body, err)
		}
	}
}

// memberPreviewStartPersona drives one reconcile pass for memberID (seeded
// desired online on m-preview-box) and returns the persona_context of the
// START it dispatched.
func memberPreviewStartPersona(t *testing.T, api *apiServer, d *DAL, memberID string) string {
	t.Helper()
	if err := d.putMemberWholeRowForTest(Member{ID: "m-preview-box", Name: "Preview Box", Kind: KindWarden, RosterStatus: RosterStatusActive}); err != nil {
		t.Fatalf("PutMember(warden): %v", err)
	}
	api.telemetry.Set("m-preview-box", map[string]any{"runtimes": map[string]any{
		"claude": map[string]any{"installed": true, "logged_in": true},
		"codex":  map[string]any{"installed": true, "logged_in": true},
	}})
	reconcileTestOnline(t, api, "m-preview-box", "")
	hubTestStderr(t, func() { api.reconcileMemberNow(memberID) })
	frames := drainFrames(t, api, "m-preview-box")
	if len(frames) != 1 || frames[0].RPC != reconcileCmdStart {
		t.Fatalf("want exactly one START for %s, got %+v", memberID, frames)
	}
	persona, _ := frames[0].Args["persona_context"].(string)
	if persona == "" {
		t.Fatalf("the START carries no persona_context: %+v", frames[0])
	}
	return persona
}

func TestHandleGetMemberBootContextApiMembersMemberIdBootContextGet(t *testing.T) {
	for _, tc := range []struct {
		name      string
		runtime   string
		wantBoot  string
		otherBoot string
	}{
		{
			name:      "a Claude staff member's preview equals the persona its reconcile START carries, with its own 傳承 and the Claude boot steps",
			runtime:   RuntimeClaude,
			wantBoot:  memberPreviewClaudeBoot,
			otherBoot: memberPreviewCodexBoot,
		},
		{
			name:      "a Codex staff member's preview equals the persona its reconcile START carries, with the Codex boot steps",
			runtime:   RuntimeCodex,
			wantBoot:  memberPreviewCodexBoot,
			otherBoot: memberPreviewClaudeBoot,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, h, d, owner := newAPITestServer(t)
			memberPreviewSeed(t, d, Member{
				ID: "m-writer", Name: "Writer", RoleKey: memberPreviewRoleKey, Runtime: tc.runtime,
				DesiredState: DesiredStateOnline, DesiredMachineID: "m-preview-box",
			})
			persona := memberPreviewStartPersona(t, api, d, "m-writer")

			status, data := apiJSON(t, h, "GET", "/api/members/m-writer/boot-context", owner, "")
			if status != 200 {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			apiWantBody(t, data, map[string]any{"context": persona})

			context := data["context"].(string)
			if !strings.Contains(context, "\n\n# Role: Preview Writer\n\n# Preview role instructions\n\n") {
				t.Fatalf("want the role block: %s", context)
			}
			everyoneAt := strings.Index(context, memberPreviewEveryoneLore)
			ownAt := strings.Index(context, "OWN LORE OF m-writer")
			if !strings.Contains(context, "\n\n# 傳承\n\n") || everyoneAt < 0 || ownAt < everyoneAt {
				t.Fatalf("want a 傳承 block with the everyone entry before the member's own: %s", context)
			}
			if !strings.HasSuffix(context, "\n\n"+tc.wantBoot+"\n") || strings.Contains(context, tc.otherBoot) {
				t.Fatalf("want %q as the last block and no %q: %s", tc.wantBoot, tc.otherBoot, context)
			}
		})
	}

	t.Run("two staff members of one role each preview their own 傳承 and not the other's", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		memberPreviewSeed(t, d,
			Member{ID: "m-first", Name: "First", RoleKey: memberPreviewRoleKey, Runtime: RuntimeClaude},
			Member{ID: "m-second", Name: "Second", RoleKey: memberPreviewRoleKey, Runtime: RuntimeClaude},
		)
		for _, tc := range []struct{ id, own, other string }{
			{id: "m-first", own: "OWN LORE OF m-first", other: "OWN LORE OF m-second"},
			{id: "m-second", own: "OWN LORE OF m-second", other: "OWN LORE OF m-first"},
		} {
			status, data := apiJSON(t, h, "GET", "/api/members/"+tc.id+"/boot-context", owner, "")
			if status != 200 {
				t.Fatalf("%s: want 200, got %d (%v)", tc.id, status, data)
			}
			context, _ := data["context"].(string)
			if !strings.Contains(context, tc.own) || strings.Contains(context, tc.other) {
				t.Fatalf("%s: want %q and not %q: %s", tc.id, tc.own, tc.other, context)
			}
		}
	})

	t.Run("an admin agent's token reads the preview", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		memberPreviewSeed(t, d, Member{ID: "m-writer", Name: "Writer", RoleKey: memberPreviewRoleKey})
		admin := apiTestPrincipalToken(t, api, d, principalAdminAgent, "m-preview-admin")

		status, data := apiJSON(t, h, "GET", "/api/members/m-writer/boot-context", admin, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"context": apiAnyString})
	})

	for _, tc := range []struct {
		name    string
		id      string
		message string
	}{
		{name: "an id no roster row carries answers 404 naming the member", id: "m-nope", message: "member 'm-nope' not found"},
		{name: "a removed staff member answers 404 naming the member", id: "m-gone", message: "member 'm-gone' not found"},
		{name: "an outsource worker answers 404 naming the member", id: "ow-abc123", message: "member 'ow-abc123' not found"},
		{name: "a machine answers 404 naming the member", id: "m-preview-machine", message: "member 'm-preview-machine' not found"},
		{name: "a staff member whose role has no definition answers 404 naming the role", id: apiTestPlainAgentID, message: "role 'engineer' not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, h, d, owner := newAPITestServer(t)
			memberPreviewSeed(t, d,
				Member{ID: "m-gone", Name: "Gone", RoleKey: memberPreviewRoleKey, RosterStatus: RosterStatusRemoved},
				Member{ID: "m-preview-machine", Name: "Machine", Kind: KindWarden},
			)
			apiTestWorkerFixture(t, h, d, owner, "ow-abc123", WorkerStatusAssigned)

			status, data := apiJSON(t, h, "GET", "/api/members/"+tc.id+"/boot-context", owner, "")
			if status != 404 {
				t.Fatalf("want 404, got %d (%v)", status, data)
			}
			apiWantError(t, data, "not_found", tc.message)
		})
	}

	t.Run("a plain agent's token answers 403 at the admin_agent floor", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		memberPreviewSeed(t, d, Member{ID: "m-writer", Name: "Writer", RoleKey: memberPreviewRoleKey})
		housekeeper := apiTestAgentToken(t, api, apiTestPlainAgentID, "")

		status, data := apiJSON(t, h, "GET", "/api/members/m-writer/boot-context", housekeeper, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, d, _ := newAPITestServer(t)
		memberPreviewSeed(t, d, Member{ID: "m-writer", Name: "Writer", RoleKey: memberPreviewRoleKey})

		status, data := apiJSON(t, h, "GET", "/api/members/m-writer/boot-context", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}
