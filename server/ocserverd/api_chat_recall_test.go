package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"testing"
)

func apiTestRecallNoticeBody(id string) string {
	return "Owner 刪除了訊息 #" + id + "，該則內容已作廢。若沒讀過可以直接忽略；需要原文時，用 get_chat ids=" + id + " 讀回。"
}

func apiTestRecalledRow(id, from, to, body string) map[string]any {
	row := apiTestChatRow(id, from, to, body)
	row["recalled_ts"] = apiAnyNumber
	return row
}

func apiTestRecallNoticeRow(noticeID, recalledID, recalledTo string) map[string]any {
	row := apiTestChatRow(noticeID, "system", recalledTo, apiTestRecallNoticeBody(recalledID))
	row["meta"] = map[string]any{"reply_to": recalledID}
	row["reply_to"] = recalledID
	row["reply_to_chat"] = map[string]any{
		"id": recalledID, "from": "owner", "from_name": "", "to": recalledTo, "to_name": "",
		"content": "", "recalled": true,
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

func apiTestPostChat(t *testing.T, h http.Handler, token, body string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/chat", token, body)
	if status != 200 {
		t.Fatalf("post %s: %d %v", body, status, data)
	}
	id, _ := data["id"].(string)
	return id
}

func apiTestRecall(t *testing.T, h http.Handler, token, id string) map[string]any {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/chat/"+id+"/recall", token, "")
	if status != 200 {
		t.Fatalf("recall %s: %d %v", id, status, data)
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

// apiTestNoticeAfter answers the id of the one message the recall appended.
func apiTestNoticeAfter(t *testing.T, d *DAL, before int) string {
	t.Helper()
	msgs := apiTestAllChat(t, d)
	if len(msgs) != before+1 {
		t.Fatalf("want exactly one message appended by the recall, the stream went %d -> %d", before, len(msgs))
	}
	return msgs[len(msgs)-1].ID
}

func TestHandleRecallChatMessageApiChatMessageIdRecallPost(t *testing.T) {
	t.Run("the owner recalls an ordinary message of their own: 200 with the message, a system notice to the recipient, and a chat delta for each", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		id := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		before := len(apiTestAllChat(t, d))
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "mira")
		bystander := apiTestListen(t, api, "kip")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/chat/"+id+"/recall", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, apiTestRecalledRow(id, "owner", "mira", "the secret plan"))

		noticeID := apiTestNoticeAfter(t, d, before)
		frames := []map[string]any{
			apiTestChatFrame(2, id, "owner", "mira", "owner"),
			apiTestChatFrame(3, noticeID, "system", "mira", "owner"),
		}
		dashboard.wantFrames(frames...)
		recipient.wantFrames(frames...)
		bystander.wantFrames()
		wantPushed()

		apiWantValue(t, "notice", apiTestChatMessages(t, h, owner, "/api/chat?ids="+noticeID),
			[]any{apiTestRecallNoticeRow(noticeID, id, "mira")})
	})

	t.Run("a member stops seeing the recalled message in every listing while a by-id read still returns it whole", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		first := apiTestPostChat(t, h, owner, `{"to":"mira","body":"before"}`)
		recalled := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		last := apiTestPostChat(t, h, owner, `{"to":"mira","body":"after"}`)
		apiTestRecall(t, h, owner, recalled)

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
		apiWantValue(t, "window anchored on the recalled message", apiTestRowIDs(apiTestChatMessages(t, h, member,
			"/api/chat?with=owner&start_id="+recalled+"&limit=10")), []any{last})

		unread := apiTestChatMessages(t, h, member, "/api/chat?unread=true")
		notice := apiTestRowIDs(unread)
		if len(notice) != 3 {
			t.Fatalf("unread: want the two kept messages and the notice, got %v", notice)
		}
		apiWantValue(t, "unread", unread, []any{
			apiTestChatRow(first, "owner", "mira", "before"),
			apiTestChatRow(last, "owner", "mira", "after"),
			apiTestRecallNoticeRow(notice[2].(string), recalled, "mira"),
		})

		apiWantValue(t, "by id", apiTestChatMessages(t, h, member, "/api/chat?ids="+recalled),
			[]any{apiTestRecalledRow(recalled, "owner", "mira", "the secret plan")})
	})

	t.Run("the owner's own listing keeps the recalled message, flagged", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		first := apiTestPostChat(t, h, owner, `{"to":"mira","body":"before"}`)
		recalled := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		apiTestRecall(t, h, owner, recalled)

		rows := apiTestChatMessages(t, h, owner, "/api/chat?with=mira")
		ids := apiTestRowIDs(rows)
		if len(ids) != 3 {
			t.Fatalf("want the two messages and the notice, got %v", ids)
		}
		apiWantValue(t, "owner listing", rows, []any{
			apiTestChatRow(first, "owner", "mira", "before"),
			apiTestRecalledRow(recalled, "owner", "mira", "the secret plan"),
			apiTestRecallNoticeRow(ids[2].(string), recalled, "mira"),
		})
	})

	t.Run("the member's wake snapshot leaves the recalled message out", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		kept := apiTestPostChat(t, h, owner, `{"to":"mira","body":"kept"}`)
		recalled := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		apiTestRecall(t, h, owner, recalled)

		status, snap := apiJSON(t, h, "GET", "/api/resume-summary", member, "")
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
		}, map[string]any{"from": "system", "body": apiTestRecallNoticeBody(recalled), "reply_to": recalled})
	})

	t.Run("a reply quoting a recalled message serves the quote recalled, with neither its text nor the excerpt", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		recalled := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		reply := apiTestPostChat(t, h, member,
			`{"to":"owner","body":"got it","reply_to":"`+recalled+`","reply_excerpt":"secret"}`)
		apiTestRecall(t, h, owner, recalled)

		want := apiTestChatRow(reply, "mira", "owner", "got it")
		want["meta"] = map[string]any{"reply_to": recalled}
		want["reply_to"] = recalled
		want["reply_to_chat"] = map[string]any{
			"id": recalled, "from": "owner", "from_name": "", "to": "mira", "to_name": "",
			"content": "", "recalled": true,
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
			"id": recalled, "from": "owner", "from_name": "Owner", "to": "mira", "to_name": "Mira",
			"content": "", "recalled": true,
		})
	})

	t.Run("the gallery stops listing a recalled message's attachment while the file and the message's own list stay", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		_, uploaded := apiJSON(t, h, "POST",
			"/api/chat/attachments?filename=notes.txt&mime=text/plain", owner, "hello")
		attachmentID, _ := uploaded["id"].(string)
		recalled := apiTestPostChat(t, h, owner,
			`{"to":"mira","body":"see this","attachments":[{"id":"`+attachmentID+`"}]}`)
		apiTestRecall(t, h, owner, recalled)

		status, gallery := apiChatDecoded(t, h, "GET", "/api/chat/attachments?with=mira", owner)
		if status != 200 {
			t.Fatalf("gallery: %d %v", status, gallery)
		}
		apiWantValue(t, "gallery", gallery, []any{})

		attachment := map[string]any{
			"id": attachmentID, "url": "/api/chat/attachment/" + attachmentID,
			"filename": "notes.txt", "mime": "text/plain", "is_image": false,
		}
		want := apiTestRecalledRow(recalled, "owner", "mira", "see this")
		want["meta"] = map[string]any{"attachments": []any{map[string]any{
			"id": attachmentID, "filename": "notes.txt", "mime": "text/plain",
		}}}
		want["attachments"] = []any{attachment}
		apiWantValue(t, "by id", apiTestChatMessages(t, h, owner, "/api/chat?ids="+recalled), []any{want})

		rec := apiRequest(t, h, "GET", "/api/chat/attachment/"+attachmentID, owner, "")
		if rec.Code != 200 || rec.Body.String() != "hello" {
			t.Fatalf("the file stays stored: %d %q", rec.Code, rec.Body.String())
		}
	})

	t.Run("recalling it again answers the same message and sends no second notice", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		id := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		first := apiTestRecall(t, h, owner, id)
		before := apiTestAllChat(t, d)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/chat/"+id+"/recall", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		want := apiTestChatRow(id, "owner", "mira", "the secret plan")
		want["ts"] = first["ts"]
		want["recalled_ts"] = first["recalled_ts"]
		apiWantBody(t, data, want)
		dalWantChats(t, "the stream after a second recall", apiTestAllChat(t, d), before)
		dashboard.wantFrames()
		recipient.wantFrames()
	})

	t.Run("parallel recalls of one message all answer 200 recalled and leave exactly one notice", func(t *testing.T) {
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
				rec := apiRequest(t, h, "POST", "/api/chat/"+id+"/recall", owner, "")
				var data map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &data)
				answers[i] = answer{rec.Code, data}
			}()
		}
		close(start)
		wg.Wait()

		for i, a := range answers {
			if a.status != 200 {
				t.Fatalf("recall %d: want 200, got %d (%v)", i, a.status, a.data)
			}
			if ts, _ := a.data["recalled_ts"].(float64); ts <= 0 {
				t.Fatalf("recall %d: want recalled_ts set, got %v", i, a.data)
			}
		}
		noticeID := apiTestNoticeAfter(t, d, before)
		apiWantValue(t, "notice", apiTestChatMessages(t, h, owner, "/api/chat?ids="+noticeID),
			[]any{apiTestRecallNoticeRow(noticeID, id, "mira")})
	})

	t.Run("a message whose meta is JSON null is recalled like any other", func(t *testing.T) {
		_, h, d, owner := newAPITestServer(t)
		if _, err := d.wdb.Exec(`INSERT INTO chat_message (id, sender, recipient, body, ts, meta)
			VALUES ('c-0000000000bb', 'owner', 'mira', 'legacy', ?, 'null')`, nowSecs()); err != nil {
			t.Fatalf("seed: %v", err)
		}
		before := len(apiTestAllChat(t, d))

		status, data := apiJSON(t, h, "POST", "/api/chat/c-0000000000bb/recall", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		want := apiTestRecalledRow("c-0000000000bb", "owner", "mira", "legacy")
		apiWantBody(t, data, want)
		apiWantValue(t, "by id, read back", apiTestChatMessages(t, h, owner, "/api/chat?ids=c-0000000000bb"),
			[]any{want})
		apiTestNoticeAfter(t, d, before)
	})

	t.Run("an agent caller answers 403 and nothing is recalled", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		member := apiTestAgentToken(t, api, "mira", "")
		id := apiTestPostChat(t, h, owner, `{"to":"mira","body":"the secret plan"}`)
		before := apiTestAllChat(t, d)

		status, data := apiJSON(t, h, "POST", "/api/chat/"+id+"/recall", member, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dalWantChats(t, "the stream after a refused recall", apiTestAllChat(t, d), before)
	})

	t.Run("an id that names no message answers 404", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		before := apiTestAllChat(t, d)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/chat/c-000000000000/recall", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "chat message 'c-000000000000' not found")
		dalWantChats(t, "the stream after a refused recall", apiTestAllChat(t, d), before)
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
				return "chat message '" + id + "' was sent by 'mira' — only the owner's own messages can be recalled"
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
				return "chat message '" + id + "' is a message on task T-1 — task messages cannot be recalled"
			},
		},
		{
			name: "a message carrying a reply card",
			seed: func(t *testing.T, _ *apiServer, h http.Handler, _ *DAL, owner string) string {
				return apiTestPostChat(t, h, owner, `{"to":"mira","body":"pick one","meta":{"reply_card_id":"rc-000000000001"}}`)
			},
			message: func(id string) string {
				return "chat message '" + id + "' carries a reply card — expire the card instead; a card message cannot be recalled"
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
				return "chat message '" + id + "' was sent more than 24 hours ago — only messages younger than 24 hours can be recalled"
			},
		},
	} {
		t.Run(tc.name+" answers 409 and nothing changes", func(t *testing.T) {
			api, h, d, owner := newAPITestServer(t)
			id := tc.seed(t, api, h, d, owner)
			before := apiTestAllChat(t, d)
			dashboard := apiTestListen(t, api, "")

			status, data := apiJSON(t, h, "POST", "/api/chat/"+id+"/recall", owner, "")
			if status != 409 {
				t.Fatalf("want 409, got %d (%v)", status, data)
			}
			apiWantError(t, data, "conflict", tc.message(id))
			dalWantChats(t, "the stream after a refused recall", apiTestAllChat(t, d), before)
			dashboard.wantFrames()
		})
	}
}
