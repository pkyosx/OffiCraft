package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// chatGroupSeed writes a group state straight through the store, below the
// handler's checks, the way a removal path or a later manual delete finds it.
func chatGroupSeed(t *testing.T, d *DAL, groups ...ChatGroupWriteDTO) {
	t.Helper()
	if err := d.inTx(func(tx *writeTx) error { return replaceChatGroupsOn(tx, groups, 1) }); err != nil {
		t.Fatalf("replaceChatGroupsOn: %v", err)
	}
}

func chatGroupWrite(id, tab, name string, members, types []string) ChatGroupWriteDTO {
	g := ChatGroupWriteDTO{Tab: ChatGroupWriteDTOTab(tab), Name: name, MemberIds: members, TaskTypeKeys: types}
	if members == nil {
		g.MemberIds = []string{}
	}
	if types == nil {
		g.TaskTypeKeys = []string{}
	}
	if id != "" {
		g.Id = &id
	}
	return g
}

// chatGroupColumn reads one column raw, without the joins GET applies, so a row
// left behind for a member who is gone still shows.
func chatGroupColumn(t *testing.T, d *DAL, query string) []string {
	t.Helper()
	rows, err := d.rdb.Query(query)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

func chatGroupWantMemberRows(t *testing.T, d *DAL, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	got := chatGroupColumn(t, d, `SELECT m.member_id FROM chat_group_member m
		JOIN chat_group g ON g.id = m.group_id ORDER BY g.tab, g.position, m.position`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chat_group_member rows: want %v, got %v", want, got)
	}
}

// chatGroupFixture is a station with staff kip and mira, a dismissed zed, active
// outsource workers ow-1 and ow-2, a released ow-gone, and two station task
// manuals tm-a and tm-b.
func chatGroupFixture(t *testing.T) (*apiServer, http.Handler, *DAL, string) {
	t.Helper()
	api, h, d, owner := newAPITestServer(t)
	for _, m := range []Member{
		{ID: "zed", Name: "Zed", Kind: KindStaff, RosterStatus: RosterStatusRemoved},
		{ID: "ow-1", Name: "O-1", Kind: KindOutsource, RosterStatus: RosterStatusActive},
		{ID: "ow-2", Name: "O-2", Kind: KindOutsource, RosterStatus: RosterStatusActive},
		{ID: "ow-gone", Name: "O-9", Kind: KindOutsource, RosterStatus: RosterStatusRemoved},
	} {
		if err := d.putMemberWholeRowForTest(m); err != nil {
			t.Fatalf("PutMember(%q): %v", m.ID, err)
		}
	}
	for _, key := range []string{"tm-a", "tm-b"} {
		dalPutManual(t, d, TaskManual{TypeKey: key, DisplayName: key, Fields: "[]", Assignee: "{}"})
	}
	return api, h, d, owner
}

// chatGroupIDs answers the response's group ids in order, each checked for the
// cg- prefix the wire promises.
func chatGroupIDs(t *testing.T, data map[string]any) []string {
	t.Helper()
	groups, _ := data["groups"].([]any)
	ids := []string{}
	for _, g := range groups {
		id, _ := g.(map[string]any)["id"].(string)
		if !strings.HasPrefix(id, "cg-") || len(id) <= len("cg-") {
			t.Fatalf("want a minted cg- id, got %q", id)
		}
		ids = append(ids, id)
	}
	return ids
}

// chatGroupBody is one group as the wire carries it; id "" is the null that asks
// the server to create the group.
func chatGroupBody(id, tab, name string, members, types []any) map[string]any {
	var wireID any = id
	if id == "" {
		wireID = nil
	}
	return map[string]any{"id": wireID, "tab": tab, "name": name, "member_ids": members, "task_type_keys": types}
}

func chatGroupFrame(seq int, trigger string) map[string]any {
	return map[string]any{
		"seq":   seq,
		"topic": "chat_group",
		"op":    "patch",
		"data": map[string]any{
			"entity":  "chat_group",
			"key":     "owner",
			"epoch":   seq,
			"deleted": false,
			"payload": nil,
		},
		"ts":      apiAnyNumber,
		"trigger": trigger,
	}
}

func chatGroupPut(t *testing.T, h http.Handler, owner string, groups ...map[string]any) map[string]any {
	t.Helper()
	if groups == nil {
		groups = []map[string]any{}
	}
	raw, err := json.Marshal(map[string]any{"groups": groups})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	status, data := apiJSON(t, h, "PUT", "/api/chat/groups", owner, string(raw))
	if status != 200 {
		t.Fatalf("PUT /api/chat/groups: want 200, got %d (%v)", status, data)
	}
	return data
}

func TestHandleGetChatGroupsApiChatGroupsGet(t *testing.T) {
	t.Run("on a fresh station there are no groups", func(t *testing.T) {
		_, h, _, owner := chatGroupFixture(t)

		status, data := apiJSON(t, h, "GET", "/api/chat/groups", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"groups": []any{}})
	})

	t.Run("staff groups come first, and members who left the roster and task types whose manual is gone are left out", func(t *testing.T) {
		_, h, d, owner := chatGroupFixture(t)
		chatGroupSeed(t, d,
			chatGroupWrite("cg-ops", "outsource", "Ops", []string{"ow-1", "ow-gone", "ow-2"}, []string{"tm-b", "tm-a", "tm-ghost"}),
			chatGroupWrite("cg-core", "staff", "Core", []string{"zed", "mira", "kip"}, nil),
		)
		if status, data := apiJSON(t, h, "DELETE", "/api/task-manuals/tm-b", owner, ""); status != 200 {
			t.Fatalf("delete manual: %d %v", status, data)
		}

		_, data := apiJSON(t, h, "GET", "/api/chat/groups", owner, "")
		apiWantBody(t, data, map[string]any{"groups": []any{
			chatGroupBody("cg-core", "staff", "Core", []any{"mira", "kip"}, []any{}),
			chatGroupBody("cg-ops", "outsource", "Ops", []any{"ow-1", "ow-2"}, []any{"tm-a"}),
		}})
	})

	t.Run("an agent token answers 403 and no token answers 401", func(t *testing.T) {
		api, h, _, _ := chatGroupFixture(t)

		for _, tok := range []string{apiTestAgentToken(t, api, "mira", ""), apiTestAgentToken(t, api, "kip", "")} {
			status, data := apiJSON(t, h, "GET", "/api/chat/groups", tok, "")
			if status != 403 {
				t.Fatalf("agent: want 403, got %d (%v)", status, data)
			}
			apiWantError(t, data, "forbidden", "principal not permitted")
		}
		status, data := apiJSON(t, h, "GET", "/api/chat/groups", "", "")
		if status != 401 {
			t.Fatalf("no token: want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUpdateChatGroupsApiChatGroupsPut(t *testing.T) {
	t.Run("creating groups mints cg- ids, answers the stored state and fans one chat_group frame to the owner only", func(t *testing.T) {
		api, h, _, owner := chatGroupFixture(t)
		dashboard := apiTestListen(t, api, "")
		agent := apiTestListen(t, api, "kip")

		data := chatGroupPut(t, h, owner,
			chatGroupBody("", "outsource", "Core", []any{"ow-2", "ow-1"}, []any{"tm-b", "tm-a"}),
			chatGroupBody("", "staff", "  Core  ", []any{"mira", "kip"}, []any{}),
			chatGroupBody("", "staff", strings.Repeat("群", 40), []any{}, []any{}),
		)
		ids := chatGroupIDs(t, data)
		want := map[string]any{"groups": []any{
			chatGroupBody(ids[0], "staff", "Core", []any{"mira", "kip"}, []any{}),
			chatGroupBody(ids[1], "staff", strings.Repeat("群", 40), []any{}, []any{}),
			chatGroupBody(ids[2], "outsource", "Core", []any{"ow-2", "ow-1"}, []any{"tm-b", "tm-a"}),
		}}
		apiWantBody(t, data, want)
		if len(map[string]bool{ids[0]: true, ids[1]: true, ids[2]: true}) != 3 {
			t.Fatalf("want three distinct ids, got %v", ids)
		}
		dashboard.wantFrames(chatGroupFrame(1, "owner"))
		agent.wantFrames()

		_, data = apiJSON(t, h, "GET", "/api/chat/groups", owner, "")
		apiWantBody(t, data, want)
	})

	t.Run("a rename, a reorder, a member move and a task-type move land as sent and keep the group ids", func(t *testing.T) {
		_, h, d, owner := chatGroupFixture(t)
		chatGroupSeed(t, d,
			chatGroupWrite("cg-a", "outsource", "A", []string{"ow-1"}, []string{"tm-a"}),
			chatGroupWrite("cg-b", "outsource", "B", []string{"ow-2"}, []string{"tm-b"}),
		)

		data := chatGroupPut(t, h, owner,
			chatGroupBody("cg-b", "outsource", "B2", []any{"ow-2", "ow-1"}, []any{"tm-b", "tm-a"}),
			chatGroupBody("cg-a", "outsource", "A", []any{}, []any{}),
		)
		want := map[string]any{"groups": []any{
			chatGroupBody("cg-b", "outsource", "B2", []any{"ow-2", "ow-1"}, []any{"tm-b", "tm-a"}),
			chatGroupBody("cg-a", "outsource", "A", []any{}, []any{}),
		}}
		apiWantBody(t, data, want)
		_, data = apiJSON(t, h, "GET", "/api/chat/groups", owner, "")
		apiWantBody(t, data, want)
	})

	t.Run("a group left out is deleted, its members become ungrouped and its task types are gone", func(t *testing.T) {
		_, h, d, owner := chatGroupFixture(t)
		chatGroupSeed(t, d,
			chatGroupWrite("cg-a", "outsource", "A", []string{"ow-1"}, []string{"tm-a"}),
			chatGroupWrite("cg-b", "outsource", "B", []string{"ow-2"}, nil),
		)

		data := chatGroupPut(t, h, owner, chatGroupBody("cg-b", "outsource", "B", []any{"ow-2"}, []any{}))
		apiWantBody(t, data, map[string]any{"groups": []any{
			chatGroupBody("cg-b", "outsource", "B", []any{"ow-2"}, []any{}),
		}})
		chatGroupWantMemberRows(t, d, "ow-2")
		if got := chatGroupColumn(t, d, `SELECT type_key FROM chat_group_task_type`); len(got) != 0 {
			t.Fatalf("want no task-type rules left, got %v", got)
		}
	})

	t.Run("an empty list deletes every group", func(t *testing.T) {
		_, h, d, owner := chatGroupFixture(t)
		chatGroupSeed(t, d, chatGroupWrite("cg-a", "staff", "A", []string{"kip"}, nil))

		apiWantBody(t, chatGroupPut(t, h, owner), map[string]any{"groups": []any{}})
		chatGroupWantMemberRows(t, d)
	})

	t.Run("attaching a task type moves no ungrouped worker that already has a task of that type", func(t *testing.T) {
		api, h, d, owner := chatGroupFixture(t)
		dalWorkerOnTask(t, d, "ow-3", "O-3", "T-1", 100)
		dalPutTask(t, d, Task{ID: "T-1", TypeKey: "tm-a", Title: "Work", Inputs: map[string]any{},
			Status: TaskStatusInProgress, Priority: TaskPriorityMid, ExecutorKind: TaskExecutorOutsource,
			ExecutorID: "ow-3", CreatorID: wireOwnerID, CreatedTS: 100, UpdatedTS: 100})
		chatGroupSeed(t, d, chatGroupWrite("cg-a", "outsource", "A", nil, nil))
		dashboard := apiTestListen(t, api, "")

		data := chatGroupPut(t, h, owner, chatGroupBody("cg-a", "outsource", "A", []any{}, []any{"tm-a"}))
		apiWantBody(t, data, map[string]any{"groups": []any{
			chatGroupBody("cg-a", "outsource", "A", []any{}, []any{"tm-a"}),
		}})
		dashboard.wantFrames(chatGroupFrame(1, "owner"))
	})

	for _, c := range []struct {
		name    string
		groups  func(staff, ops string) string
		message string
	}{
		{"a group id nothing carries",
			func(staff, ops string) string {
				return `[{"id":"cg-nope","tab":"staff","name":"X","member_ids":[],"task_type_keys":[]}]`
			},
			"group 'cg-nope' does not exist"},
		{"a group id listed twice",
			func(staff, ops string) string {
				return fmt.Sprintf(`[{"id":%q,"tab":"staff","name":"X","member_ids":[],"task_type_keys":[]},{"id":%q,"tab":"staff","name":"Y","member_ids":[],"task_type_keys":[]}]`, staff, staff)
			},
			"group '{staff}' is listed more than once"},
		{"a group moved to the other tab",
			func(staff, ops string) string {
				return fmt.Sprintf(`[{"id":%q,"tab":"outsource","name":"Core","member_ids":[],"task_type_keys":[]}]`, staff)
			},
			"group '{staff}' is on the staff tab and cannot move to the outsource tab"},
		{"a name blank after trimming",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"staff","name":"   ","member_ids":[],"task_type_keys":[]}]`
			},
			"groups[0].name is blank"},
		{"a name of 41 characters",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"staff","name":"` + strings.Repeat("群", 41) + `","member_ids":[],"task_type_keys":[]}]`
			},
			"groups[0].name is longer than 40 characters"},
		{"two groups of one tab with the same name after trimming",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"staff","name":"Core","member_ids":[],"task_type_keys":[]},{"id":null,"tab":"staff","name":" Core ","member_ids":[],"task_type_keys":[]}]`
			},
			"the staff tab has two groups named 'Core'"},
		{"a member listed in two groups",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"staff","name":"X","member_ids":["kip"],"task_type_keys":[]},{"id":null,"tab":"staff","name":"Y","member_ids":["mira","kip"],"task_type_keys":[]}]`
			},
			"member 'kip' is listed more than once"},
		{"a staff member in an outsource group",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"outsource","name":"X","member_ids":["kip"],"task_type_keys":[]}]`
			},
			"member 'kip' is not an active member of the outsource tab"},
		{"an outsource worker in a staff group",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"staff","name":"X","member_ids":["ow-1"],"task_type_keys":[]}]`
			},
			"member 'ow-1' is not an active member of the staff tab"},
		{"a machine in a staff group",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"staff","name":"X","member_ids":["` + ServerSelfHost + `"],"task_type_keys":[]}]`
			},
			"member '" + ServerSelfHost + "' is not an active member of the staff tab"},
		{"a dismissed staff member",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"staff","name":"X","member_ids":["zed"],"task_type_keys":[]}]`
			},
			"member 'zed' is not an active member of the staff tab"},
		{"a released outsource worker",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"outsource","name":"X","member_ids":["ow-gone"],"task_type_keys":[]}]`
			},
			"member 'ow-gone' is not an active member of the outsource tab"},
		{"an id nobody carries",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"staff","name":"X","member_ids":["ghost"],"task_type_keys":[]}]`
			},
			"member 'ghost' is not an active member of the staff tab"},
		{"task types on a staff group",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"staff","name":"X","member_ids":[],"task_type_keys":["tm-a"]}]`
			},
			"groups[0] is a staff group; task_type_keys must be empty"},
		{"a task type listed in two groups",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"outsource","name":"X","member_ids":[],"task_type_keys":["tm-a"]},{"id":null,"tab":"outsource","name":"Y","member_ids":[],"task_type_keys":["tm-b","tm-a"]}]`
			},
			"task type 'tm-a' is listed more than once"},
		{"a task type that names no task manual",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"outsource","name":"X","member_ids":[],"task_type_keys":["tm-ghost"]}]`
			},
			"task type 'tm-ghost' names no task manual"},
		{"51 groups in one tab",
			func(staff, ops string) string {
				var groups []string
				for i := 0; i < 51; i++ {
					groups = append(groups, fmt.Sprintf(`{"id":null,"tab":"staff","name":"G%d","member_ids":[],"task_type_keys":[]}`, i))
				}
				return "[" + strings.Join(groups, ",") + "]"
			},
			"the staff tab has more than 50 groups"},
		{"a tab outside the two",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"machines","name":"X","member_ids":[],"task_type_keys":[]}]`
			},
			"groups[0].tab must be 'staff' or 'outsource'"},
		{"a null member list",
			func(staff, ops string) string {
				return `[{"id":null,"tab":"staff","name":"X","member_ids":null,"task_type_keys":[]}]`
			},
			"groups[0].member_ids and .task_type_keys must be lists"},
		{"a null group list",
			func(staff, ops string) string { return `null` },
			"groups must be a list; an empty list deletes every group"},
	} {
		t.Run("under "+c.name+", the answer is 422 and the stored groups and the stream are untouched", func(t *testing.T) {
			api, h, _, owner := chatGroupFixture(t)
			seeded := chatGroupPut(t, h, owner,
				chatGroupBody("", "staff", "Core", []any{"kip"}, []any{}),
				chatGroupBody("", "outsource", "Ops", []any{"ow-1"}, []any{"tm-a"}),
			)
			ids := chatGroupIDs(t, seeded)
			dashboard := apiTestListen(t, api, "")

			status, data := apiJSON(t, h, "PUT", "/api/chat/groups", owner, `{"groups":`+c.groups(ids[0], ids[1])+`}`)
			if status != 422 {
				t.Fatalf("want 422, got %d (%v)", status, data)
			}
			apiWantError(t, data, "validation_error", strings.ReplaceAll(c.message, "{staff}", ids[0]))
			dashboard.wantFrames()
			_, data = apiJSON(t, h, "GET", "/api/chat/groups", owner, "")
			apiWantBody(t, data, map[string]any{"groups": []any{
				chatGroupBody(ids[0], "staff", "Core", []any{"kip"}, []any{}),
				chatGroupBody(ids[1], "outsource", "Ops", []any{"ow-1"}, []any{"tm-a"}),
			}})
		})
	}

	t.Run("a body with no groups key answers 422 naming it", func(t *testing.T) {
		_, h, _, owner := chatGroupFixture(t)

		status, data := apiJSON(t, h, "PUT", "/api/chat/groups", owner, `{}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: groups")
	})

	t.Run("an agent token answers 403 and no token answers 401, and neither writes", func(t *testing.T) {
		api, h, d, _ := chatGroupFixture(t)
		body := `{"groups":[{"id":null,"tab":"staff","name":"X","member_ids":["kip"],"task_type_keys":[]}]}`

		for _, tok := range []string{apiTestAgentToken(t, api, "mira", ""), apiTestAgentToken(t, api, "kip", "")} {
			status, data := apiJSON(t, h, "PUT", "/api/chat/groups", tok, body)
			if status != 403 {
				t.Fatalf("agent: want 403, got %d (%v)", status, data)
			}
			apiWantError(t, data, "forbidden", "principal not permitted")
		}
		status, data := apiJSON(t, h, "PUT", "/api/chat/groups", "", body)
		if status != 401 {
			t.Fatalf("no token: want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		if got := chatGroupColumn(t, d, `SELECT id FROM chat_group`); len(got) != 0 {
			t.Fatalf("want no group written, got %v", got)
		}
	})
}
