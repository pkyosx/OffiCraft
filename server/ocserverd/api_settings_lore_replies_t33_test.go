package main

import (
	"strings"
	"testing"
)

// TestSuggestedRepliesLoreMessageSettingRoundTrip covers the THIRD 建議回覆 list
// (T-33): the one under a 傳承 entry's message box.
//
// 🔴 IT IS A LIST OF ITS OWN, WITH THE SAME BOUNDS, and both halves of that
// sentence fail silently in opposite ways. A third field wired to an existing
// row would look perfect on any screen where the sentences match — so every
// assertion below names WHICH list it expects and checks the neighbours did not
// move. And a third field that skipped the bounds check would be accepted here
// and refused nowhere until the owner had already typed twenty-one sentences —
// so the two caps are asserted against THIS key, not inherited by argument from
// the fact that the other two keys have them.
func TestSuggestedRepliesLoreMessageSettingRoundTrip(t *testing.T) {
	api, srv, d, _ := newSettingsTestServer(t, "lore-sugg-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"lore-sugg-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	list := func(m map[string]any, key string) []any {
		v, ok := m[key].([]any)
		if !ok {
			t.Fatalf("%s must be an ARRAY on the wire, never null: %#v", key, m[key])
		}
		return v
	}

	// Default: [] — an array, not null and not absent. A reader must never have
	// to tell "the owner configured none" apart from "this server is older".
	if status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != 200 {
		t.Fatalf("get: %d", status)
	}
	if len(list(data, "suggested_replies_lore_message")) != 0 {
		t.Fatalf("the 傳承 list must default to []: %v", data)
	}

	// Over the per-entry rune cap → 422, nothing written.
	long := `{"suggested_replies_lore_message":["` +
		strings.Repeat("水", maxSuggestedReplyLen+1) + `"]}`
	if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner, long); status != 422 {
		t.Fatalf("an over-long 傳承 entry must 422: got %d", status)
	}
	// Over the entry-count cap → 422, nothing written.
	many := `{"suggested_replies_lore_message":[` +
		strings.TrimSuffix(strings.Repeat(`"收到",`, maxSuggestedReplies+1), ",") + `]}`
	if status, _ := doJSON(t, "PATCH", srv.URL+"/api/settings", owner, many); status != 422 {
		t.Fatalf("too many 傳承 entries must 422: got %d", status)
	}
	if v, err := d.GetSetting(settingSuggestedRepliesLoreMessage); err != nil || v != nil {
		t.Fatalf("a rejected patch must write nothing: %v %v", v, err)
	}

	// A valid patch: trimmed, blanks dropped, echoed, durable, live.
	body := `{"suggested_replies_lore_message":["  這條還適用嗎  ","","   ","這條可以退場了"]}`
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, body); status != 200 {
		t.Fatalf("valid patch: %d %v", status, data)
	}
	got := list(data, "suggested_replies_lore_message")
	if len(got) != 2 || got[0] != "這條還適用嗎" || got[1] != "這條可以退場了" {
		t.Fatalf("entries must be trimmed and blanks dropped: %v", got)
	}
	if v, err := d.GetSetting(settingSuggestedRepliesLoreMessage); err != nil || v == nil ||
		*v != `["這條還適用嗎","這條可以退場了"]` {
		t.Fatalf("the 傳承 list must be durable under its OWN key: %v %v", v, err)
	}
	if len(api.settingsView().SuggestedRepliesLoreMessage) != 2 {
		t.Fatalf("the 傳承 list must be live in the snapshot")
	}

	// 🔴 THE OTHER TWO LISTS DID NOT MOVE, and their DB rows do not even exist.
	// This is the assertion a "one shared list" regression cannot survive.
	if len(list(data, "suggested_replies_reply_card")) != 0 ||
		len(list(data, "suggested_replies_task_message")) != 0 {
		t.Fatalf("patching the 傳承 list must not populate the others: %v", data)
	}
	for _, key := range []string{
		settingSuggestedRepliesReplyCard, settingSuggestedRepliesTaskMessage,
	} {
		if v, err := d.GetSetting(key); err != nil || v != nil {
			t.Fatalf("%s must not be written: %v %v", key, v, err)
		}
	}

	// And the reverse direction: the task-message list moves without touching
	// this one. One assertion cannot tell "independent" from "the new key is
	// simply never written".
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"suggested_replies_task_message":["之後再說"]}`); status != 200 {
		t.Fatalf("task_message patch: %d %v", status, data)
	}
	if len(list(data, "suggested_replies_task_message")) != 1 ||
		len(list(data, "suggested_replies_lore_message")) != 2 {
		t.Fatalf("the lists must move independently: %v", data)
	}

	// An EXPLICIT EMPTY ARRAY is legal and clears it — that is what turns the
	// chips off, and it must stay reachable.
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"suggested_replies_lore_message":[]}`); status != 200 {
		t.Fatalf("an explicit empty array must be accepted: %d %v", status, data)
	}
	if len(list(data, "suggested_replies_lore_message")) != 0 {
		t.Fatalf("an explicit empty array must clear the 傳承 list: %v", data)
	}
	if len(api.settingsView().SuggestedRepliesLoreMessage) != 0 {
		t.Fatalf("the cleared 傳承 list must be live in the snapshot")
	}
}

// TestLoadAuthSettingsLoreSuggestedReplies pins the BOOT half: a row already in
// the database is loaded back verbatim, and a row a PATCH would have refused
// STOPS THE SERVER rather than quietly becoming what the cockpit offers. The
// two faces have to refuse the same values — a hand-edited row is the door the
// PATCH validation does not cover.
func TestLoadAuthSettingsLoreSuggestedReplies(t *testing.T) {
	d := newTestDAL(t)
	if err := d.PutSetting(settingSuggestedRepliesLoreMessage,
		`["這條還適用嗎","這條可以退場了"]`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, _ := loadForTest(t, d, defaultConfig())
	if len(got.suggestedRepliesLoreMessage) != 2 ||
		got.suggestedRepliesLoreMessage[0] != "這條還適用嗎" ||
		got.suggestedRepliesLoreMessage[1] != "這條可以退場了" {
		t.Fatalf("the 傳承 list must load verbatim: %v", got.suggestedRepliesLoreMessage)
	}
	// The other two are absent rows and must load EMPTY, not nil-with-surprises
	// and certainly not a copy of the one that was seeded.
	if len(got.suggestedRepliesReplyCard) != 0 || len(got.suggestedRepliesTaskMessage) != 0 {
		t.Fatalf("absent rows must load empty: %v %v",
			got.suggestedRepliesReplyCard, got.suggestedRepliesTaskMessage)
	}

	for name, row := range map[string]string{
		"unparseable": `{"nope":1}`,
		"too long":    `["` + strings.Repeat("水", maxSuggestedReplyLen+1) + `"]`,
		"too many": "[" + strings.TrimSuffix(
			strings.Repeat(`"x",`, maxSuggestedReplies+1), ",") + "]",
	} {
		dd := newTestDAL(t)
		if err := dd.PutSetting(settingSuggestedRepliesLoreMessage, row); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		// loadAuthSettings directly, NOT the loadForTest helper: that helper
		// t.Fatalf's on an error, which is the very outcome this loop expects.
		if _, err := loadAuthSettings(dd, defaultConfig(), func(string) {}); err == nil {
			t.Fatalf("a %s 傳承 row must stop the boot, not be installed", name)
		}
	}
}
