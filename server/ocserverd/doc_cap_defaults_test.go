package main

import (
	"net/http"
	"strings"
	"testing"
)

// The shipped document caps, pinned as literals on purpose: a test that names
// the constant stays green when somebody edits the constant, which is the one
// change this file exists to notice.

func TestShippedDocumentCapsAreServedWhenUnset(t *testing.T) {
	_, h, _, owner := newAPITestServer(t)
	status, data := apiJSON(t, h, "GET", "/api/settings", owner, "")
	if status != 200 {
		t.Fatalf("GET /api/settings: %d %v", status, data)
	}
	want := map[string]float64{
		"doc_cap_chars_manual_sop":         37500,
		"doc_cap_chars_duty":               2500,
		"doc_cap_chars_system_interaction": 150000,
		"step_note_cap_chars":              25000,
		"lore_cap_chars_title":             200,
		"lore_cap_chars_body":              1250,
		"lore_cap_chars_role":              25000,
		"lore_cap_chars_manual":            25000,
		// Not raised: insight, boot sequence, offboard.
		"doc_cap_chars_insight":       15000,
		"doc_cap_chars_boot_sequence": 15000,
		"doc_cap_chars_offboard":      15000,
	}
	for field, v := range want {
		if data[field] != v {
			t.Errorf("%s = %v, want %v", field, data[field], v)
		}
	}
}

func TestShippedDocumentCapsBindWrites(t *testing.T) {
	t.Run("task manual SOP: 37500 characters saves, 37501 is refused", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		over := `{"sop_md":"` + strings.Repeat("x", 37501) + `"}`
		status, data := apiJSON(t, h, "POST", "/api/task-manuals/builtin-role-design", owner, over)
		if status != 400 {
			t.Fatalf("37501-char SOP: want 400, got %d (%v)", status, data)
		}
		at := `{"sop_md":"` + strings.Repeat("x", 37500) + `"}`
		if status, data := apiJSON(t, h, "POST", "/api/task-manuals/builtin-role-design", owner, at); status != 200 {
			t.Fatalf("37500-char SOP: want 200, got %d (%v)", status, data)
		}
	})

	t.Run("role definition: 2500 characters saves, 2501 is refused", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		if err := d.PutRoleDef(RoleDef{RoleKey: "r-design", Name: "Design", DefinitionMD: "# Duty"}); err != nil {
			t.Fatalf("PutRoleDef: %v", err)
		}
		status, data := apiJSON(t, h, "POST", "/api/roles/r-design", owner,
			`{"definition_md":"`+strings.Repeat("x", 2501)+`"}`)
		if status != 400 {
			t.Fatalf("2501-char duty: want 400, got %d (%v)", status, data)
		}
		if status, data := apiJSON(t, h, "POST", "/api/roles/r-design", owner,
			`{"definition_md":"`+strings.Repeat("x", 2500)+`"}`); status != 200 {
			t.Fatalf("2500-char duty: want 200, got %d (%v)", status, data)
		}
	})

	t.Run("system interaction: 150000 characters saves, 150001 is refused", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner,
			`{"body":"`+strings.Repeat("x", 150001)+`"}`)
		if status != 400 {
			t.Fatalf("150001-char block: want 400, got %d (%v)", status, data)
		}
		if status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner,
			`{"body":"`+strings.Repeat("x", 150000)+`"}`); status != 200 {
			t.Fatalf("150000-char block: want 200, got %d (%v)", status, data)
		}
	})

	t.Run("step note: 25000 characters saves, 25001 is refused", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent, `{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		stepID := apiTestOnlyStepID(t, h, owner, "T-1")
		path := "/api/tasks/T-1/steps/" + stepID + "/note"
		status, data := apiJSON(t, h, "POST", path, agent, `{"note":"`+strings.Repeat("x", 25001)+`"}`)
		if status != 400 {
			t.Fatalf("25001-char note: want 400, got %d (%v)", status, data)
		}
		if status, data := apiJSON(t, h, "POST", path, agent, `{"note":"`+strings.Repeat("x", 25000)+`"}`); status != 200 {
			t.Fatalf("25000-char note: want 200, got %d (%v)", status, data)
		}
	})

	t.Run("lore entry: a 200-character title and a 1250-character body save, one more is refused", func(t *testing.T) {
		s := loreTestServer(t)
		me := hireLoreStaff(t, s, "m-lore-1", "researcher")
		for _, tc := range []struct {
			name        string
			title, body int
			want        int
		}{
			{"title 201", 201, 10, http.StatusBadRequest},
			{"body 1251", 10, 1251, http.StatusBadRequest},
			{"title 200 + body 1250", 200, 1250, http.StatusOK},
		} {
			rec := postLore(t, s, me, map[string]any{
				"title": strings.Repeat("t", tc.title),
				"body":  strings.Repeat("b", tc.body),
			})
			if rec.Code != tc.want {
				t.Fatalf("%s: want %d, got %d %s", tc.name, tc.want, rec.Code, rec.Body.String())
			}
		}
	})
}

func TestDocCapCeilingAdmitsTheLargestDefault(t *testing.T) {
	_, h, _, owner := newAPITestServer(t)
	if status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"doc_cap_chars_system_interaction":250000}`); status != 200 {
		t.Fatalf("PATCH 250000: want 200, got %d (%v)", status, data)
	}
	if status, data := apiJSON(t, h, "PATCH", "/api/settings", owner, `{"doc_cap_chars_system_interaction":250001}`); status != 422 {
		t.Fatalf("PATCH 250001: want 422, got %d (%v)", status, data)
	}
}
