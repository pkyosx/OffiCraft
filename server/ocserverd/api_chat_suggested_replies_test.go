package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func chatSuggestedRepliesSeed(t *testing.T, d *DAL, memberID string, replies ...string) {
	t.Helper()
	if err := d.inTx(func(tx *writeTx) error { return replaceChatSuggestedRepliesOn(tx, memberID, replies) }); err != nil {
		t.Fatalf("replaceChatSuggestedRepliesOn: %v", err)
	}
}

// chatSuggestedRepliesRows reads the table raw, without the join GET applies, so
// a row left behind for a member who is gone still shows.
func chatSuggestedRepliesRows(t *testing.T, d *DAL) []string {
	t.Helper()
	return chatGroupColumn(t, d, `SELECT member_id || ':' || position || ':' || text FROM chat_suggested_reply
		ORDER BY member_id, position`)
}

func chatSuggestedRepliesWantRows(t *testing.T, d *DAL, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := chatSuggestedRepliesRows(t, d); !reflect.DeepEqual(got, want) {
		t.Fatalf("chat_suggested_reply rows: want %v, got %v", want, got)
	}
}

func chatSuggestedReplyFrame(seq int) map[string]any {
	return map[string]any{
		"seq":   seq,
		"topic": "chat_suggested_reply",
		"op":    "patch",
		"data": map[string]any{
			"entity":  "chat_suggested_reply",
			"key":     "owner",
			"epoch":   seq,
			"deleted": false,
			"payload": nil,
		},
		"ts":      apiAnyNumber,
		"trigger": "owner",
	}
}

func chatSuggestedRepliesBody(t *testing.T, replies ...string) string {
	t.Helper()
	if replies == nil {
		replies = []string{}
	}
	raw, err := json.Marshal(map[string]any{"replies": replies})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func TestHandleGetChatSuggestedRepliesApiChatSuggestedRepliesGet(t *testing.T) {
	t.Run("on a fresh station there are no lists", func(t *testing.T) {
		_, h, _, owner := chatGroupFixture(t)

		status, data := apiJSON(t, h, "GET", "/api/chat/suggested-replies", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"lists": []any{}})
	})

	t.Run("every roster member's list comes back in display order and a member who left is not listed", func(t *testing.T) {
		_, h, d, owner := chatGroupFixture(t)
		chatSuggestedRepliesSeed(t, d, "kip", "b", "a")
		chatSuggestedRepliesSeed(t, d, "ow-1", "繼續")
		chatSuggestedRepliesSeed(t, d, "zed", "gone staff")
		chatSuggestedRepliesSeed(t, d, "ow-gone", "gone worker")

		status, data := apiJSON(t, h, "GET", "/api/chat/suggested-replies", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"lists": []any{
			map[string]any{"member_id": "kip", "replies": []any{"b", "a"}},
			map[string]any{"member_id": "ow-1", "replies": []any{"繼續"}},
		}})
	})

	t.Run("an agent token answers 403 and no token answers 401", func(t *testing.T) {
		api, h, _, _ := chatGroupFixture(t)

		status, data := apiJSON(t, h, "GET", "/api/chat/suggested-replies", apiTestAgentToken(t, api, "kip", ""), "")
		if status != 403 {
			t.Fatalf("agent: want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		status, data = apiJSON(t, h, "GET", "/api/chat/suggested-replies", "", "")
		if status != 401 {
			t.Fatalf("no token: want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleUpdateChatSuggestedRepliesApiChatSuggestedRepliesMemberIdPut(t *testing.T) {
	for _, memberID := range []string{"kip", "ow-1"} {
		t.Run("a list for "+memberID+" is stored trimmed in the order sent, leaves other lists alone and fans one frame to the owner only", func(t *testing.T) {
			api, h, d, owner := chatGroupFixture(t)
			chatSuggestedRepliesSeed(t, d, "mira", "keep me")
			dashboard := apiTestListen(t, api, "")
			agent := apiTestListen(t, api, "kip")

			status, data := apiJSON(t, h, "PUT", "/api/chat/suggested-replies/"+memberID, owner,
				chatSuggestedRepliesBody(t, "  second  ", "first", "first"))
			if status != 200 {
				t.Fatalf("want 200, got %d (%v)", status, data)
			}
			apiWantBody(t, data, map[string]any{"member_id": memberID, "replies": []any{"second", "first", "first"}})
			dashboard.wantFrames(chatSuggestedReplyFrame(1))
			agent.wantFrames()
			want := []string{memberID + ":0:second", memberID + ":1:first", memberID + ":2:first", "mira:0:keep me"}
			sort.Strings(want)
			chatSuggestedRepliesWantRows(t, d, want...)
		})
	}

	t.Run("a shorter list replaces the whole of the old one and an empty list removes it", func(t *testing.T) {
		_, h, d, owner := chatGroupFixture(t)
		chatSuggestedRepliesSeed(t, d, "kip", "a", "b", "c")

		status, data := apiJSON(t, h, "PUT", "/api/chat/suggested-replies/kip", owner, chatSuggestedRepliesBody(t, "c"))
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		chatSuggestedRepliesWantRows(t, d, "kip:0:c")

		status, data = apiJSON(t, h, "PUT", "/api/chat/suggested-replies/kip", owner, chatSuggestedRepliesBody(t))
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"member_id": "kip", "replies": []any{}})
		chatSuggestedRepliesWantRows(t, d)
	})

	t.Run("exactly 20 entries of exactly 120 characters are accepted", func(t *testing.T) {
		_, h, d, owner := chatGroupFixture(t)
		replies := make([]string, 20)
		for i := range replies {
			replies[i] = fmt.Sprintf("%02d", i) + strings.Repeat("句", 118)
		}

		status, data := apiJSON(t, h, "PUT", "/api/chat/suggested-replies/kip", owner, chatSuggestedRepliesBody(t, replies...))
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		if got := len(chatSuggestedRepliesRows(t, d)); got != 20 {
			t.Fatalf("want 20 rows, got %d", got)
		}
	})

	twentyOne := make([]string, 21)
	for i := range twentyOne {
		twentyOne[i] = "s"
	}
	for _, tc := range []struct {
		name, member, body, message string
	}{
		{"a body without replies", "kip", `{}`, "field required: replies"},
		{"a null list", "kip", `{"replies":null}`, "replies must be a list; an empty list removes the member's suggested replies"},
		{"a blank entry", "kip", chatSuggestedRepliesBody(t, "ok", "   "), "replies[1] is blank"},
		{"an entry of 121 characters", "kip", chatSuggestedRepliesBody(t, strings.Repeat("句", 121)), "replies must be at most 120 characters per entry"},
		{"21 entries", "kip", chatSuggestedRepliesBody(t, twentyOne...), "replies must be at most 20 entries"},
		{"a dismissed member", "zed", chatSuggestedRepliesBody(t, "x"), "member 'zed' is not an active member"},
		{"a released worker", "ow-gone", chatSuggestedRepliesBody(t, "x"), "member 'ow-gone' is not an active member"},
		{"a warden", "m-warden", chatSuggestedRepliesBody(t, "x"), "member 'm-warden' is not an active member"},
		{"an unknown member", "ghost", chatSuggestedRepliesBody(t, "x"), "member 'ghost' is not an active member"},
	} {
		t.Run(tc.name+" answers 422, writes nothing and fans nothing", func(t *testing.T) {
			api, h, d, owner := chatGroupFixture(t)
			if err := d.putMemberWholeRowForTest(Member{ID: "m-warden", Name: "W", Kind: machineKind, RosterStatus: RosterStatusActive}); err != nil {
				t.Fatalf("PutMember: %v", err)
			}
			chatSuggestedRepliesSeed(t, d, "kip", "existing")
			dashboard := apiTestListen(t, api, "")

			status, data := apiJSON(t, h, "PUT", "/api/chat/suggested-replies/"+tc.member, owner, tc.body)
			if status != 422 {
				t.Fatalf("want 422, got %d (%v)", status, data)
			}
			apiWantError(t, data, "validation_error", tc.message)
			chatSuggestedRepliesWantRows(t, d, "kip:0:existing")
			dashboard.wantFrames()
		})
	}

	t.Run("an agent token answers 403 and no token answers 401, and neither writes", func(t *testing.T) {
		api, h, d, _ := chatGroupFixture(t)
		body := chatSuggestedRepliesBody(t, "x")

		status, data := apiJSON(t, h, "PUT", "/api/chat/suggested-replies/kip", apiTestAgentToken(t, api, "kip", ""), body)
		if status != 403 {
			t.Fatalf("agent: want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		status, data = apiJSON(t, h, "PUT", "/api/chat/suggested-replies/kip", "", body)
		if status != 401 {
			t.Fatalf("no token: want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		chatSuggestedRepliesWantRows(t, d)
	})
}
