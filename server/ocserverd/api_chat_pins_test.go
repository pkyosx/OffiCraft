package main

import (
	"reflect"
	"testing"
)

// chatPinRows reads the chat_pin table raw, without the roster join GET applies,
// so a row left behind for a member who is gone still shows.
func chatPinRows(t *testing.T, d *DAL) []string {
	t.Helper()
	rows, err := d.rdb.Query(`SELECT member_id FROM chat_pin ORDER BY position`)
	if err != nil {
		t.Fatalf("read chat_pin: %v", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan chat_pin: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read chat_pin: %v", err)
	}
	return ids
}

func chatPinWantRows(t *testing.T, d *DAL, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := chatPinRows(t, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("chat_pin rows: want %v, got %v", want, got)
	}
}

func chatPinPutMembers(t *testing.T, d *DAL, members ...Member) {
	t.Helper()
	for _, m := range members {
		if err := d.putMemberWholeRowForTest(m); err != nil {
			t.Fatalf("PutMember(%q): %v", m.ID, err)
		}
	}
}

func TestHandleGetChatPinsApiChatPinsGet(t *testing.T) {
	t.Run("on a fresh station the list is Mira alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/chat/pins", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"member_ids": []any{"mira"}})
	})

	t.Run("a stored pin whose member is removed, a machine or unknown is left off and the rest keep their order", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		chatPinPutMembers(t, d, Member{ID: "zed", Name: "Zed", Kind: KindStaff, RosterStatus: RosterStatusRemoved})
		if err := replaceChatPinsOn(d.wdb, []string{"kip", "zed", ServerSelfHost, "ghost", "mira"}); err != nil {
			t.Fatalf("replaceChatPinsOn: %v", err)
		}

		_, data := apiJSON(t, h, "GET", "/api/chat/pins", owner, "")
		apiWantBody(t, data, map[string]any{"member_ids": []any{"kip", "mira"}})
	})

	t.Run("an agent token answers 403 and no token answers 401", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")

		status, data := apiJSON(t, h, "GET", "/api/chat/pins", admin, "")
		if status != 403 {
			t.Fatalf("admin agent: want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		status, data = apiJSON(t, h, "GET", "/api/chat/pins", "", "")
		if status != 401 {
			t.Fatalf("no token: want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUpdateChatPinsApiChatPinsPut(t *testing.T) {
	t.Run("a new list answers the stored list, reads back in order and fans one chat_pin frame to the owner only", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		chatPinPutMembers(t, d, Member{ID: "ow-1", Name: "O-1", Kind: KindOutsource, RosterStatus: RosterStatusActive})
		dashboard := apiTestListen(t, api, "")
		agent := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "PUT", "/api/chat/pins", owner, `{"member_ids":["ow-1","kip","mira"]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"member_ids": []any{"ow-1", "kip", "mira"}})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "chat_pin",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat_pin",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{"member_ids": []any{"ow-1", "kip", "mira"}},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		agent.wantFrames()

		_, data = apiJSON(t, h, "GET", "/api/chat/pins", owner, "")
		apiWantBody(t, data, map[string]any{"member_ids": []any{"ow-1", "kip", "mira"}})
	})

	t.Run("an empty list unpins everyone and the frame carries the empty list", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/chat/pins", owner, `{"member_ids":[]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"member_ids": []any{}})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "chat_pin",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat_pin",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{"member_ids": []any{}},
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		chatPinWantRows(t, d)
	})

	for _, c := range []struct{ name, body, message string }{
		{"an id listed twice", `{"member_ids":["kip","mira","kip"]}`, "member_ids lists 'kip' more than once"},
		{"an id nobody carries", `{"member_ids":["kip","ghost"]}`, "member_ids: 'ghost' is not an active staff or outsource member"},
		{"a machine id", `{"member_ids":["kip","` + ServerSelfHost + `"]}`, "member_ids: '" + ServerSelfHost + "' is not an active staff or outsource member"},
		{"a dismissed staff member", `{"member_ids":["kip","zed"]}`, "member_ids: 'zed' is not an active staff or outsource member"},
		{"a released outsource worker", `{"member_ids":["kip","ow-gone"]}`, "member_ids: 'ow-gone' is not an active staff or outsource member"},
		{"a null list", `{"member_ids":null}`, "member_ids must be a list; an empty list unpins everyone"},
		{"no member_ids key", `{}`, "field required: member_ids"},
	} {
		t.Run("under "+c.name+", the answer is 422 and the stored list and the stream are untouched", func(t *testing.T) {
			api, h, d, owner := newAPITestServer(t)
			chatPinPutMembers(t, d,
				Member{ID: "zed", Name: "Zed", Kind: KindStaff, RosterStatus: RosterStatusRemoved},
				Member{ID: "ow-gone", Name: "O-9", Kind: KindOutsource, RosterStatus: RosterStatusRemoved},
			)
			dashboard := apiTestListen(t, api, "")

			status, data := apiJSON(t, h, "PUT", "/api/chat/pins", owner, c.body)
			if status != 422 {
				t.Fatalf("want 422, got %d (%v)", status, data)
			}
			apiWantError(t, data, "validation_error", c.message)
			dashboard.wantFrames()
			chatPinWantRows(t, d, "mira")
		})
	}

	t.Run("an agent token answers 403 and no token answers 401, and neither writes", func(t *testing.T) {
		api, h, d, _ := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")
		plain := apiTestAgentToken(t, api, "kip", "")

		for _, tok := range []string{admin, plain} {
			status, data := apiJSON(t, h, "PUT", "/api/chat/pins", tok, `{"member_ids":["kip"]}`)
			if status != 403 {
				t.Fatalf("agent: want 403, got %d (%v)", status, data)
			}
			apiWantError(t, data, "forbidden", "principal not permitted")
		}
		status, data := apiJSON(t, h, "PUT", "/api/chat/pins", "", `{"member_ids":["kip"]}`)
		if status != 401 {
			t.Fatalf("no token: want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		chatPinWantRows(t, d, "mira")
	})
}
