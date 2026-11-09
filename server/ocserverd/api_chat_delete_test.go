package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"testing"
)

func apiTestDeleteNoticeBody(id string) string {
	return "Owner 刪除了訊息 #" + id + "，該則內容已作廢。若沒讀過可以直接忽略；需要原文時，用 get_chat ids=" + id + " 讀回。"
}

func apiTestDeletedRow(id, from, to, body string) map[string]any {
	row := apiTestChatRow(id, from, to, body)
	row["deleted_ts"] = apiAnyNumber
	return row
}

func apiTestDeleteNoticeRow(noticeID, deletedID, deletedTo string) map[string]any {
	row := apiTestChatRow(noticeID, "system", deletedTo, apiTestDeleteNoticeBody(deletedID))
	row["meta"] = map[string]any{
		"reply_to":      deletedID,
		"delete_notice": map[string]any{"message_id": deletedID},
	}
	row["reply_to"] = deletedID
	row["reply_to_chat"] = map[string]any{
		"id": deletedID, "from": "owner", "from_name": "", "to": deletedTo, "to_name": "",
		"content": "", "deleted": true,
	}
	return row
}

func apiTestChatFrame(seq int, id, from, to, trigger string) map[string]any {
	return map[string]any{
		"seq":   seq,
		"topic": "chat",
		"op":    "patch",
		"data": map[string]any{
			"entity":  "chat",
			"key":     "owner::" + id,
			"epoch":   seq,
			"deleted": false,
			"payload": map[string]any{"id": id, "from": from, "to": to},
		},
		"ts":      apiAnyNumber,
		"trigger": trigger,
	}
}

func apiTestDeleteFrame(seq int, id, to string, deletedTS any) map[string]any {
	frame := apiTestChatFrame(seq, id, "owner", to, "owner")
	frame["data"].(map[string]any)["payload"] = map[string]any{
		"id": id, "from": "owner", "to": to, "deleted_ts": deletedTS,
	}
	return frame
}

func apiTestPostChat(t *testing.T, h http.Handler, token, body string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/chat", token, body)
	if status != 200 {
		t.Fatalf("post %s: %d %v", body, status, data)
	}
	id, _ := data["id"].(string)
	return id
}

func apiTestDelete(t *testing.T, h http.Handler, token, id string) map[string]any {
	t.Helper()
	status, data := apiJSON(t, h, "DELETE", "/api/chat/"+id, token, "")
	if status != 200 {
		t.Fatalf("delete %s: %d %v", id, status, data)
	}
	return data
}

func apiTestChatPage(t *testing.T, h http.Handler, token, target string) map[string]any {
	t.Helper()
	status, data := apiJSON(t, h, "GET", target, token, "")
	if status != 200 {
		t.Fatalf("GET %s: %d %v", target, status, data)
	}
	return data
}

func apiTestChatMessages(t *testing.T, h http.Handler, token, target string) any {
	t.Helper()
	return apiTestChatPage(t, h, token, target)["messages"]
}

func apiTestRowIDs(rows any) []any {
	ids := []any{}
	list, _ := rows.([]any)
	for _, row := range list {
		m, _ := row.(map[string]any)
		ids = append(ids, m["id"])
	}
	return ids
}

func apiTestAllChat(t *testing.T, d *DAL) []ChatMessage {
	t.Helper()
	msgs, err := d.ListChat()
	if err != nil {
		t.Fatalf("ListChat: %v", err)
	}
	return msgs
}

// apiTestNoticeAfter answers the id of the one message the delete appended.
func apiTestNoticeAfter(t *testing.T, d *DAL, before int) string {
	t.Helper()
	msgs := apiTestAllChat(t, d)
	if len(msgs) != before+1 {
		t.Fatalf("want exactly one message appended by the delete, the stream went %d -> %d", before, len(msgs))
	}
	return msgs[len(msgs)-1].ID
}

func TestHandleDeleteChatMessageApiChatMessageIdDelete(t *testing.T) {
	t.Run("the owner deletes an ordinary message of their own: 200 with the message, a system notice to the recipient, and a chat delta for each", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		id := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		before := len(apiTestAllChat(t, d))
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "mira")
		bystander := apiTestListen(t, api, "kip")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "DELETE", "/api/chat/"+id, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestDeletedRow(id, "owner", "mira", "the secret plan"))

		noticeID := apiTestNoticeAfter(t, d, before)
		frames := []map[string]any{
			apiTestDeleteFrame(2, id, "mira", data["deleted_ts"]),
			apiTestChatFrame(3, noticeID, "system", "mira", "owner"),
		}
		dashboard.wantFrames(frames...)
		recipient.wantFrames(frames...)
		bystander.wantFrames()
		wantPushed()

		apiWantValue(t, "notice", apiTestChatMessages(t, h, owner, "/api/chat?ids="+noticeID),
			[]any{apiTestDeleteNoticeRow(noticeID, id, "mira")})
	})

	t.Run("a member stops seeing the deleted message in every listing while a by-id read still returns it whole", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		first := apiTestPostChat(t, h, owner, `{"to":"mira","body":"before"}`)
		deleted := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		last := apiTestPostChat(t, h, owner, `{"to":"mira","body":"after"}`)
		apiTestDelete(t, h, owner, deleted)

		apiWantValue(t, "latest", apiTestChatMessages(t, h, member, "/api/chat?with=owner"), []any{
			apiTestChatRow(first, "owner", "mira", "before"),
			apiTestChatRow(last, "owner", "mira", "after"),
		})

		page := apiTestChatPage(t, h, member, "/api/chat?with=owner&limit=1")
		apiWantValue(t, "first page", apiTestRowIDs(page["messages"]), []any{last})
		cursor, _ := page["next_cursor"].(string)
		if cursor == "" {
			t.Fatalf("a one-row page over two visible messages must carry a cursor: %v", page)
		}
		apiWantValue(t, "cursor page", apiTestRowIDs(apiTestChatMessages(t, h, member,
			"/api/chat?with=owner&limit=1&cursor="+url.QueryEscape(cursor))), []any{first})

		lastRow := apiTestChatMessages(t, h, member, "/api/chat?ids="+last).([]any)[0].(map[string]any)
		apiWantValue(t, "before", apiTestRowIDs(apiTestChatMessages(t, h, member,
			"/api/chat?with=owner&before_id="+last+"&before_ts="+
				strconv.FormatFloat(lastRow["ts"].(float64), 'f', -1, 64))), []any{first})

		apiWantValue(t, "window", apiTestRowIDs(apiTestChatMessages(t, h, member,
			"/api/chat?with=owner&start_id="+first+"&end_id="+last+"&limit=10")), []any{first, last})
		apiWantValue(t, "window anchored on the deleted message", apiTestRowIDs(apiTestChatMessages(t, h, member,
			"/api/chat?with=owner&start_id="+deleted+"&limit=10")), []any{last})

		unread := apiTestChatMessages(t, h, member, "/api/chat?unread=true")
		notice := apiTestRowIDs(unread)
		if len(notice) != 3 {
			t.Fatalf("unread: want the two kept messages and the notice, got %v", notice)
		}
		apiWantValue(t, "unread", unread, []any{
			apiTestChatRow(first, "owner", "mira", "before"),
			apiTestChatRow(last, "owner", "mira", "after"),
			apiTestDeleteNoticeRow(notice[2].(string), deleted, "mira"),
		})

		apiWantValue(t, "by id", apiTestChatMessages(t, h, member, "/api/chat?ids="+deleted),
			[]any{apiTestDeletedRow(deleted, "owner", "mira", "the secret plan")})
	})

	t.Run("a deleted row from a sender other than the owner leaves the member's listing and unread count and is listed flagged only on request", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		now := nowSecs()
		for _, m := range []ChatMessage{
			{ID: "c-0000000000k1", Sender: "kip", Recipient: "mira", Body: "kept", TS: now - 20, Meta: map[string]any{}},
			{ID: "c-0000000000k2", Sender: "kip", Recipient: "mira", Body: "the secret plan", TS: now - 10,
				Meta: map[string]any{}, DeletedTS: now - 5},
		} {
			if err := d.PutChat(m); err != nil {
				t.Fatalf("PutChat: %v", err)
			}
		}

		apiWantValue(t, "member listing", apiTestRowIDs(apiTestChatMessages(t, h, member, "/api/chat?with=kip")),
			[]any{"c-0000000000k1"})
		apiWantValue(t, "member unread backfill", apiTestRowIDs(apiTestChatMessages(t, h, member, "/api/chat?unread=true")),
			[]any{"c-0000000000k1"})
		status, unread := apiJSON(t, h, "GET", "/api/chat/unread-count", member, "")
		if status != 200 {
			t.Fatalf("unread-count: %d %v", status, unread)
		}
		apiWantBody(t, unread, map[string]any{"unread": 1})

		kept := apiTestChatRow("c-0000000000k1", "kip", "mira", "kept")
		deleted := apiTestChatRow("c-0000000000k2", "kip", "mira", "the secret plan")
		deleted["deleted_ts"] = now - 5
		apiWantValue(t, "owner listing", apiTestChatMessages(t, h, owner, "/api/chat?with=mira"), []any{kept})
		apiWantValue(t, "owner listing with include_deleted", apiTestChatMessages(t, h, owner,
			"/api/chat?with=mira&include_deleted=true"), []any{kept, deleted})
	})

	t.Run("every caller's listing leaves a deleted message out unless include_deleted=true asks for it, flagged", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		first := apiTestPostChat(t, h, owner, `{"to":"mira","body":"before"}`)
		deleted := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		apiTestDelete(t, h, owner, deleted)
		var notice any

		for _, c := range []struct{ who, token, with string }{{"owner", owner, "mira"}, {"member", member, "mira"}} {
			rows := apiTestChatMessages(t, h, c.token, "/api/chat?with="+c.with)
			ids := apiTestRowIDs(rows)
			if len(ids) != 2 {
				t.Fatalf("%s default listing: want the kept message and the notice, got %v", c.who, ids)
			}
			notice = ids[1]
			apiWantValue(t, c.who+" default listing", rows, []any{
				apiTestChatRow(first, "owner", "mira", "before"),
				apiTestDeleteNoticeRow(ids[1].(string), deleted, "mira"),
			})
			apiWantValue(t, c.who+" include_deleted listing", apiTestChatMessages(t, h, c.token,
				"/api/chat?with="+c.with+"&include_deleted=true"), []any{
				apiTestChatRow(first, "owner", "mira", "before"),
				apiTestDeletedRow(deleted, "owner", "mira", "the secret plan"),
				apiTestDeleteNoticeRow(ids[1].(string), deleted, "mira"),
			})
			apiWantValue(t, c.who+" include_deleted=yes reads as not sent", apiTestRowIDs(apiTestChatMessages(t, h, c.token,
				"/api/chat?with="+c.with+"&include_deleted=yes")), ids)
			apiWantValue(t, c.who+" include_deleted window", apiTestRowIDs(apiTestChatMessages(t, h, c.token,
				"/api/chat?with="+c.with+"&start_id="+first+"&limit=10&include_deleted=true")), []any{first, deleted, ids[1]})
		}

		status, data := apiJSON(t, h, "GET", "/api/chat?unread=true&include_deleted=true", member, "")
		if status != 422 {
			t.Fatalf("unread with include_deleted: want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"unread cannot be combined with include_deleted — a deleted message is never unread")

		status, snap := apiJSON(t, h, "GET", "/api/members/mira/resume-summary?include_delete_notices=true", owner, "")
		if status != 200 {
			t.Fatalf("owner reading mira's resume summary: %d %v", status, snap)
		}
		apiWantValue(t, "owner's view of the wake snapshot", apiTestRowIDs(snap["chat"]), []any{first, notice})
	})

	t.Run("include_deleted reaches the before_ts/before_id path and a cursor continuation", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		first := apiTestPostChat(t, h, owner, `{"to":"mira","body":"before"}`)
		deleted := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		last := apiTestPostChat(t, h, owner, `{"to":"mira","body":"after"}`)
		apiTestDelete(t, h, owner, deleted)
		lastRow := apiTestChatMessages(t, h, owner, "/api/chat?ids="+last).([]any)[0].(map[string]any)
		before := "/api/chat?with=mira&sender=owner&before_id=" + last + "&before_ts=" +
			strconv.FormatFloat(lastRow["ts"].(float64), 'f', -1, 64)

		apiWantValue(t, "before, default", apiTestChatMessages(t, h, owner, before),
			[]any{apiTestChatRow(first, "owner", "mira", "before")})
		apiWantValue(t, "before, include_deleted", apiTestChatMessages(t, h, owner, before+"&include_deleted=true"),
			[]any{apiTestChatRow(first, "owner", "mira", "before"), apiTestDeletedRow(deleted, "owner", "mira", "the secret plan")})

		for _, c := range []struct {
			label, extra string
			want         []any
		}{
			{"cursor, default", "", []any{apiTestChatRow(first, "owner", "mira", "before")}},
			{"cursor, include_deleted", "&include_deleted=true", []any{apiTestDeletedRow(deleted, "owner", "mira", "the secret plan")}},
		} {
			page := apiTestChatPage(t, h, owner, "/api/chat?with=mira&sender=owner&limit=1"+c.extra)
			apiWantValue(t, c.label+" first page", apiTestRowIDs(page["messages"]), []any{last})
			cursor, _ := page["next_cursor"].(string)
			if cursor == "" {
				t.Fatalf("%s: a one-row page with older rows must carry a cursor: %v", c.label, page)
			}
			apiWantValue(t, c.label+" continuation", apiTestChatMessages(t, h, owner,
				"/api/chat?with=mira&sender=owner&limit=1&cursor="+url.QueryEscape(cursor)+c.extra), c.want)
		}
	})

	t.Run("hide_delete_notices=true leaves the delete notice out of every listing path; without it the notice is listed", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		kept := apiTestPostChat(t, h, owner, `{"to":"mira","body":"kept"}`)
		deleted := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		apiTestDelete(t, h, owner, deleted)
		ids := apiTestRowIDs(apiTestChatMessages(t, h, owner, "/api/chat?with=mira"))
		if len(ids) != 2 || ids[0] != kept {
			t.Fatalf("default listing: want the kept message then the notice, got %v", ids)
		}
		notice := ids[1].(string)
		noticeRow := apiTestChatMessages(t, h, owner, "/api/chat?ids="+notice).([]any)[0].(map[string]any)
		before := "/api/chat?with=mira&before_id=" + notice + "&before_ts=" +
			strconv.FormatFloat(noticeRow["ts"].(float64)+1, 'f', -1, 64)

		for _, c := range []struct {
			label, target string
			plain, hidden []any
		}{
			{"latest", "/api/chat?with=mira", []any{kept, notice}, []any{kept}},
			{"window", "/api/chat?with=mira&start_id=" + kept + "&limit=10", []any{kept, notice}, []any{kept}},
			{"before", before, []any{kept, notice}, []any{kept}},
			{"recipient filter", "/api/chat?recipient=mira", []any{kept, notice}, []any{kept}},
			{"with include_deleted", "/api/chat?with=mira&include_deleted=true",
				[]any{kept, deleted, notice}, []any{kept, deleted}},
		} {
			apiWantValue(t, c.label+", default", apiTestRowIDs(apiTestChatMessages(t, h, owner, c.target)), c.plain)
			apiWantValue(t, c.label+", hide_delete_notices", apiTestRowIDs(apiTestChatMessages(t, h, owner,
				c.target+"&hide_delete_notices=true")), c.hidden)
		}
		apiWantValue(t, "hide_delete_notices=yes reads as not sent", apiTestRowIDs(apiTestChatMessages(t, h, owner,
			"/api/chat?with=mira&hide_delete_notices=yes")), []any{kept, notice})
		apiWantValue(t, "ids ignores hide_delete_notices", apiTestRowIDs(apiTestChatMessages(t, h, member,
			"/api/chat?ids="+notice+"&hide_delete_notices=true")), []any{notice})

		status, data := apiJSON(t, h, "GET", "/api/chat?unread=true&hide_delete_notices=true", member, "")
		if status != 422 {
			t.Fatalf("unread with hide_delete_notices: want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", chatUnreadDeleteNoticesMsg)
	})

	t.Run("a system message without the marker, and anyone else's message carrying it, stay listed under hide_delete_notices", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		forged := apiTestPostChat(t, h, member,
			`{"to":"owner","body":"look","meta":{"delete_notice":{"message_id":"c-x"}}}`)
		if err := d.PutChat(ChatMessage{ID: "c-00000000sys1", Sender: wireSystemSender, Recipient: "mira",
			Body: "some other system line", TS: nowSecs(), Meta: map[string]any{}}); err != nil {
			t.Fatalf("PutChat: %v", err)
		}
		apiWantValue(t, "hide_delete_notices", apiTestRowIDs(apiTestChatMessages(t, h, owner,
			"/api/chat?with=mira&hide_delete_notices=true")), []any{forged, "c-00000000sys1"})
	})

	t.Run("every resume-summary face leaves the delete notice out unless include_delete_notices=true, and the peek sizes what that resume carries", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		kept := apiTestPostChat(t, h, owner, `{"to":"mira","body":"kept"}`)
		deleted := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		apiTestDelete(t, h, owner, deleted)
		get := func(token, target string) map[string]any {
			t.Helper()
			status, data := apiJSON(t, h, "GET", target, token, "")
			if status != 200 {
				t.Fatalf("GET %s: %d %v", target, status, data)
			}
			return data
		}
		ids := apiTestRowIDs(get(member, "/api/resume-summary?include_delete_notices=true")["chat"])
		if len(ids) != 2 || ids[0] != kept {
			t.Fatalf("snapshot with notices: want the kept message then the notice, got %v", ids)
		}
		notice := ids[1]

		for _, c := range []struct{ label, token, target string }{
			{"resume_summary", member, "/api/resume-summary"},
			{"get_member_resume_summary", owner, "/api/members/mira/resume-summary"},
		} {
			apiWantValue(t, c.label+", default", apiTestRowIDs(get(c.token, c.target)["chat"]), []any{kept})
			apiWantValue(t, c.label+", include_delete_notices", apiTestRowIDs(get(c.token,
				c.target+"?include_delete_notices=true")["chat"]), []any{kept, notice})
			apiWantValue(t, c.label+", include_delete_notices=yes reads as not sent", apiTestRowIDs(get(c.token,
				c.target+"?include_delete_notices=yes")["chat"]), []any{kept})
		}

		for _, q := range []string{"", "?include_delete_notices=true"} {
			resumeOverview := get(member, "/api/resume-summary"+q)["overview"].(map[string]any)
			peek := get(member, "/api/resume-summary-size"+q)
			apiWantValue(t, "peek overview"+q, peek["overview"], resumeOverview)
		}
		hidden := get(member, "/api/resume-summary-size")["overview"].(map[string]any)["chat_chars"].(float64)
		shown := get(member, "/api/resume-summary-size?include_delete_notices=true")["overview"].(map[string]any)["chat_chars"].(float64)
		if hidden >= shown {
			t.Fatalf("chat_chars by default = %v, want less than %v with the notice included", hidden, shown)
		}
	})

	t.Run("the member's wake snapshot leaves the deleted message out, and carries its notice on request", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		kept := apiTestPostChat(t, h, owner, `{"to":"mira","body":"kept"}`)
		deleted := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		apiTestDelete(t, h, owner, deleted)

		status, snap := apiJSON(t, h, "GET", "/api/resume-summary?include_delete_notices=true", member, "")
		if status != 200 {
			t.Fatalf("resume-summary: %d %v", status, snap)
		}
		ids := apiTestRowIDs(snap["chat"])
		if len(ids) != 2 || ids[0] != kept {
			t.Fatalf("want the kept message then the notice, got %v", ids)
		}
		notice := snap["chat"].([]any)[1].(map[string]any)
		apiWantValue(t, "notice", map[string]any{
			"from": notice["from"], "body": notice["body"], "reply_to": notice["reply_to"],
		}, map[string]any{"from": "system", "body": apiTestDeleteNoticeBody(deleted), "reply_to": deleted})
	})

	t.Run("a reply quoting a deleted message serves the quote deleted, with neither its text nor the excerpt", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		deleted := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		reply := apiTestPostChat(t, h, member,
			`{"to":"owner","body":"got it","reply_to":"`+deleted+`","reply_excerpt":"secret"}`)
		apiTestDelete(t, h, owner, deleted)

		want := apiTestChatRow(reply, "mira", "owner", "got it")
		want["meta"] = map[string]any{"reply_to": deleted}
		want["reply_to"] = deleted
		want["reply_to_chat"] = map[string]any{
			"id": deleted, "from": "owner", "from_name": "", "to": "mira", "to_name": "",
			"content": "", "deleted": true,
		}
		apiWantValue(t, "reply", apiTestChatMessages(t, h, owner, "/api/chat?ids="+reply), []any{want})

		status, snap := apiJSON(t, h, "GET", "/api/resume-summary", member, "")
		if status != 200 {
			t.Fatalf("resume-summary: %d %v", status, snap)
		}
		var quote any
		for _, row := range snap["chat"].([]any) {
			if m := row.(map[string]any); m["id"] == reply {
				quote = m["reply_to_chat"]
			}
		}
		apiWantValue(t, "wake snapshot quote", quote, map[string]any{
			"id": deleted, "from": "owner", "from_name": "Owner", "to": "mira", "to_name": "Mira",
			"content": "", "deleted": true,
		})
	})

	t.Run("the gallery stops listing a deleted message's attachment while the file and the message's own list stay", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, uploaded := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename=notes.txt&mime=text/plain", owner, "hello")
		attachmentID, _ := uploaded["id"].(string)
		deleted := apiTestPostChat(t, h, owner,
			`{"to":"mira","body":"see this","attachments":[{"id":"`+attachmentID+`"}]}`)
		apiTestDelete(t, h, owner, deleted)

		status, gallery := apiChatDecoded(t, h, "GET", "/api/chat/attachments?with=mira", owner)
		if status != 200 {
			t.Fatalf("gallery: %d %v", status, gallery)
		}
		apiWantValue(t, "gallery", gallery, []any{})

		attachment := map[string]any{
			"id": attachmentID, "url": "/api/chat/attachment/" + attachmentID,
			"filename": "notes.txt", "mime": "text/plain", "is_image": false,
		}
		want := apiTestDeletedRow(deleted, "owner", "mira", "see this")
		want["meta"] = map[string]any{"attachments": []any{map[string]any{
			"id": attachmentID, "filename": "notes.txt", "mime": "text/plain",
		}}}
		want["attachments"] = []any{attachment}
		apiWantValue(t, "by id", apiTestChatMessages(t, h, owner, "/api/chat?ids="+deleted), []any{want})

		rec := apiRequest(t, h, "GET", "/api/chat/attachment/"+attachmentID, owner, "")
		if rec.Code != 200 || rec.Body.String() != "hello" {
			t.Fatalf("the file stays stored: %d %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("deleting it again answers the same message and sends no second notice", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		id := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		first := apiTestDelete(t, h, owner, id)
		before := apiTestAllChat(t, d)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "DELETE", "/api/chat/"+id, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		want := apiTestChatRow(id, "owner", "mira", "the secret plan")
		want["ts"] = first["ts"]
		want["deleted_ts"] = first["deleted_ts"]
		apiWantBody(t, data, want)
		dalWantChats(t, "the stream after a second delete", apiTestAllChat(t, d), before)
		dashboard.wantFrames()
		recipient.wantFrames()
	})

	t.Run("parallel deletes of one message all answer 200 deleted and leave exactly one notice", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		id := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		before := len(apiTestAllChat(t, d))

		const n = 16
		type answer struct {
			status int
			data   map[string]any
		}
		answers := make([]answer, n)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range answers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				rec := apiRequest(t, h, "DELETE", "/api/chat/"+id, owner, "")
				var data map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &data)
				answers[i] = answer{rec.Code, data}
			}()
		}
		close(start)
		wg.Wait()

		for i, a := range answers {
			if a.status != 200 {
				t.Fatalf("delete %d: want 200, got %d (%v)", i, a.status, a.data)
			}
			if ts, _ := a.data["deleted_ts"].(float64); ts <= 0 {
				t.Fatalf("delete %d: want deleted_ts set, got %v", i, a.data)
			}
		}
		noticeID := apiTestNoticeAfter(t, d, before)
		apiWantValue(t, "notice", apiTestChatMessages(t, h, owner, "/api/chat?ids="+noticeID),
			[]any{apiTestDeleteNoticeRow(noticeID, id, "mira")})
	})

	t.Run("a notice that cannot be written leaves the message undeleted, and a retry deletes it with one notice", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		id := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		before := apiTestAllChat(t, d)
		if _, err := d.wdb.Exec(`CREATE TRIGGER test_refuse_system_chat BEFORE INSERT ON chat_message
			WHEN NEW.sender = 'system' BEGIN SELECT RAISE(ABORT, 'notice refused'); END`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "DELETE", "/api/chat/"+id, owner, "")
		if status != 500 {
			t.Fatalf("want 500, got %d (%v)", status, data)
		}
		apiWantError(t, data, "internal_error", "internal error: constraint failed: notice refused (1811)")
		apiWantValue(t, "by id after the failed delete", apiTestChatMessages(t, h, owner, "/api/chat?ids="+id),
			[]any{apiTestChatRow(id, "owner", "mira", "the secret plan")})
		dalWantChats(t, "the stream after the failed delete", apiTestAllChat(t, d), before)
		dashboard.wantFrames()
		recipient.wantFrames()

		if _, err := d.wdb.Exec(`DROP TRIGGER test_refuse_system_chat`); err != nil {
			t.Fatalf("drop trigger: %v", err)
		}
		apiTestDelete(t, h, owner, id)
		noticeID := apiTestNoticeAfter(t, d, len(before))
		apiWantValue(t, "notice", apiTestChatMessages(t, h, owner, "/api/chat?ids="+noticeID),
			[]any{apiTestDeleteNoticeRow(noticeID, id, "mira")})
	})

	t.Run("a message whose meta is JSON null is deleted like any other", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		if _, err := d.wdb.Exec(`INSERT INTO chat_message (id, sender, recipient, body, ts, meta)
			VALUES ('c-0000000000bb', 'owner', 'mira', 'legacy', ?, 'null')`, nowSecs()); err != nil {
			t.Fatalf("seed: %v", err)
		}
		before := len(apiTestAllChat(t, d))

		status, data := apiJSON(t, h, "DELETE", "/api/chat/c-0000000000bb", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		want := apiTestDeletedRow("c-0000000000bb", "owner", "mira", "legacy")
		apiWantBody(t, data, want)
		apiWantValue(t, "by id, read back", apiTestChatMessages(t, h, owner, "/api/chat?ids=c-0000000000bb"),
			[]any{want})
		apiTestNoticeAfter(t, d, before)
	})

	t.Run("an agent caller answers 403 and nothing is deleted", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		id := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		before := apiTestAllChat(t, d)

		status, data := apiJSON(t, h, "DELETE", "/api/chat/"+id, member, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dalWantChats(t, "the stream after a refused delete", apiTestAllChat(t, d), before)
	})

	t.Run("an id that names no message answers 404", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		before := apiTestAllChat(t, d)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/chat/c-000000000000", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "chat message 'c-000000000000' not found")
		dalWantChats(t, "the stream after a refused delete", apiTestAllChat(t, d), before)
		dashboard.wantFrames()
	})

	for _, tc := range []struct {
		name    string
		seed    func(t *testing.T, api *apiServer, h http.Handler, d *DAL, owner string) string
		message func(id string) string
	}{
		{
			name: "a message a member sent",
			seed: func(t *testing.T, api *apiServer, h http.Handler, _ *DAL, _ string) string {
				return apiTestPostChat(t, h, apiTestAgentToken(t, api, "mira", ""), `{"to":"owner","body":"hi boss"}`)
			},
			message: func(id string) string {
				return "chat message '" + id + "' was sent by 'mira' — only the owner's own messages can be deleted"
			},
		},
		{
			name: "a task message",
			seed: func(t *testing.T, _ *apiServer, h http.Handler, _ *DAL, owner string) string {
				apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
				status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/message", owner, `{"body":"any update?"}`)
				if status != 200 {
					t.Fatalf("task message: %d %v", status, data)
				}
				return data["id"].(string)
			},
			message: func(id string) string {
				return "chat message '" + id + "' is a message on task T-1 — task messages cannot be deleted"
			},
		},
		{
			name: "a message carrying a reply card",
			seed: func(t *testing.T, _ *apiServer, h http.Handler, _ *DAL, owner string) string {
				return apiTestPostChat(t, h, owner, `{"to":"mira","body":"pick one","meta":{"reply_card_id":"rc-000000000001"}}`)
			},
			message: func(id string) string {
				return "chat message '" + id + "' carries a reply card — expire the card instead; a card message cannot be deleted"
			},
		},
		{
			name: "a message sent more than 24 hours ago",
			seed: func(t *testing.T, _ *apiServer, _ http.Handler, d *DAL, _ string) string {
				old := ChatMessage{ID: "c-0000000000aa", Sender: "owner", Recipient: "mira",
					Body: "yesterday", TS: nowSecs() - 24*60*60 - 5, Meta: map[string]any{}}
				if err := d.PutChat(old); err != nil {
					t.Fatalf("PutChat: %v", err)
				}
				return old.ID
			},
			message: func(id string) string {
				return "chat message '" + id + "' was sent more than 24 hours ago — only messages younger than 24 hours can be deleted"
			},
		},
	} {
		t.Run(tc.name+" answers 409 and nothing changes", func(t *testing.T) {
			api, h, d, owner := newAPITestServer(t)
			id := tc.seed(t, api, h, d, owner)
			before := apiTestAllChat(t, d)
			dashboard := apiTestListen(t, api, "")

			status, data := apiJSON(t, h, "DELETE", "/api/chat/"+id, owner, "")
			if status != 409 {
				t.Fatalf("want 409, got %d (%v)", status, data)
			}
			apiWantError(t, data, "conflict", tc.message(id))
			dalWantChats(t, "the stream after a refused delete", apiTestAllChat(t, d), before)
			dashboard.wantFrames()
		})
	}
}
