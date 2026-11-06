package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestSuggestedRepliesChatSettingRoundTrip(t *testing.T) {
	api, srv, d, _ := newSettingsTestServer(t, "chat-sugg-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "", `{"password":"chat-sugg-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	if status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, ""); status != 200 {
		t.Fatalf("get: %d", status)
	}
	if !reflect.DeepEqual(data["suggested_replies_chat"], []any{}) {
		t.Fatalf("want [] by default, got %#v", data["suggested_replies_chat"])
	}

	long := `{"suggested_replies_chat":["` + strings.Repeat("水", maxSuggestedReplyLen+1) + `"]}`
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, long); status != 422 {
		t.Fatalf("over-long entry: want 422, got %d", status)
	}
	many := `{"suggested_replies_chat":[` + strings.TrimSuffix(strings.Repeat(`"收到",`, maxSuggestedReplies+1), ",") + `]}`
	if status, _ = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, many); status != 422 {
		t.Fatalf("too many entries: want 422, got %d", status)
	}
	if v, err := d.GetSetting(settingSuggestedRepliesChat); err != nil || v != nil {
		t.Fatalf("a rejected patch must write nothing: %v %v", v, err)
	}

	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"suggested_replies_chat":["  繼續  ","","先放著"]}`); status != 200 {
		t.Fatalf("valid patch: %d %v", status, data)
	}
	if !reflect.DeepEqual(data["suggested_replies_chat"], []any{"繼續", "先放著"}) {
		t.Fatalf("want trimmed entries with blanks dropped, got %#v", data["suggested_replies_chat"])
	}
	if v, err := d.GetSetting(settingSuggestedRepliesChat); err != nil || v == nil || *v != `["繼續","先放著"]` {
		t.Fatalf("want the list durable under its own key: %v %v", v, err)
	}
	if got := api.settingsView().SuggestedRepliesChat; !reflect.DeepEqual(got, []string{"繼續", "先放著"}) {
		t.Fatalf("want the list live in the snapshot, got %v", got)
	}
	for _, key := range []string{
		settingSuggestedRepliesReplyCard, settingSuggestedRepliesTaskMessage, settingSuggestedRepliesLoreMessage,
	} {
		if v, err := d.GetSetting(key); err != nil || v != nil {
			t.Fatalf("%s must not be written: %v %v", key, v, err)
		}
	}

	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner,
		`{"suggested_replies_lore_message":["之後再說"]}`); status != 200 {
		t.Fatalf("lore patch: %d %v", status, data)
	}
	if !reflect.DeepEqual(data["suggested_replies_chat"], []any{"繼續", "先放著"}) {
		t.Fatalf("patching another list must not move this one: %#v", data["suggested_replies_chat"])
	}

	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"suggested_replies_chat":[]}`); status != 200 {
		t.Fatalf("empty list: %d %v", status, data)
	}
	if !reflect.DeepEqual(data["suggested_replies_chat"], []any{}) {
		t.Fatalf("want an explicit [] to clear, got %#v", data["suggested_replies_chat"])
	}
}

func TestLoadAuthSettingsChatSuggestedReplies(t *testing.T) {
	d := newTestDAL(t)
	if err := d.PutSetting(settingSuggestedRepliesChat, `["繼續","先放著"]`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, _ := loadForTest(t, d, defaultConfig())
	if !reflect.DeepEqual(got.suggestedRepliesChat, []string{"繼續", "先放著"}) {
		t.Fatalf("want the row loaded verbatim, got %v", got.suggestedRepliesChat)
	}
	if len(got.suggestedRepliesLoreMessage) != 0 {
		t.Fatalf("want other lists empty, got %v", got.suggestedRepliesLoreMessage)
	}

	dd := newTestDAL(t)
	if err := dd.PutSetting(settingSuggestedRepliesChat, `{"nope":1}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := loadAuthSettings(dd, defaultConfig(), func(string) {}); err == nil {
		t.Fatalf("want an unparseable row to stop the boot")
	}
}
