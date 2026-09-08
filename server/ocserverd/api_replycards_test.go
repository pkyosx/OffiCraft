// Skeleton generated from server/ocserverd/api_replycards.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPublishReplyCard(t *testing.T) {
	t.Skip("TODO: publishReplyCard fans one reply_card delta (create / answer / revision all ride op patch; spec/sse.md §2.2 — the payload is the partial {id, from, status} hint, never the answer).")
}

func TestWaitingReplyCards(t *testing.T) {
	t.Skip("TODO: waitingReplyCards projects the 待回覆 pane: status waiting, longest-waiting first (created ascending).")
}

func TestRecentAnsweredReplyCards(t *testing.T) {
	t.Skip("TODO: recentAnsweredReplyCards projects the 近期已回覆 pane: answered within the 24h window (keyed off the LATEST answer ts — a revision re-enters the window), newest answer first.")
}

func TestRecentExpiredReplyCards(t *testing.T) {
	t.Skip("TODO: recentExpiredReplyCards projects the recently-expired pane: expired within the 24h window (keyed off expired_ts — the same retention the answered pane holds), newest first.")
}

func TestValidateReplyCardOptions(t *testing.T) {
	t.Skip("TODO: validateReplyCardOptions enforces the quick-reply contract: at least one option, at most as many as the card's select_mode allows (single 4, multi 20), each with non-blank text (trimmed in place), plus the ai_pick budget that same select_mode allows (\"\" = no violation).")
}

func TestNormalizeAnswerOptionIdxs(t *testing.T) {
	t.Skip("TODO: normalizeAnswerOptionIdxs is the ONE place a circled-option list becomes its stored form: deduped, ascending.")
}

func TestOpenReplyCard(t *testing.T) {
	t.Skip("TODO: openReplyCard is the ONE create machinery both entry points share (the plain POST /api/reply-cards ask AND the M3 task-gate arming): validate the body, mint the card + its companion chat message (initiator → owner, meta.reply_card_id), store both, fan the chat + reply_card deltas.")
}

func TestReplyCardDTOOf(t *testing.T) {
	t.Skip("TODO: replyCardDTOOf builds the served card view, resolving the task reference when the card was armed from a task gate (SPEC §3.6 請示 → 任務 jump); a plain chat 請示 serialises task: null.")
}

func TestWriteReplyCard(t *testing.T) {
	t.Skip("TODO: writeReplyCard is the common single-card READ response tail.")
}

func TestWriteReplyCardCreateReceipt(t *testing.T) {
	t.Skip("TODO: writeReplyCardCreateReceipt answers create_reply_card (T-91).")
}

func TestWriteReplyCardTransitionReceipt(t *testing.T) {
	t.Skip("TODO: writeReplyCardTransitionReceipt answers the three card TRANSITIONS — answer, reanswer and expire (T-91).")
}

func TestHandleCreateReplyCardApiReplyCardsPost(t *testing.T) {
	t.Run("an unbound card answers 200 with a receipt, fans the chat and reply_card deltas, and hands push the decision notification", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")
		asker := apiTestListen(t, api, "mira")
		bystander := apiTestListen(t, api, "kip")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨",`+
				`"options":[{"text":"出","ai_pick":true},{"text":"不出"}],"linked_task":null}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":              apiAnyString,
			"chat_message_id": apiAnyString,
			"created_ts":      apiAnyNumber,
			"attachments":     []any{},
		})
		cardID, _ := data["id"].(string)
		messageID, _ := data["chat_message_id"].(string)

		chatFrame := map[string]any{
			"seq":   1,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     "owner::" + messageID,
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{"id": messageID, "from": "mira", "to": "owner"},
			},
			"ts":      apiAnyNumber,
			"trigger": "mira",
		}
		cardFrame := map[string]any{
			"seq":   2,
			"topic": "reply_card",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "reply_card",
				"key":     "owner::" + cardID,
				"epoch":   2,
				"deleted": false,
				"payload": map[string]any{"id": cardID, "from": "mira", "status": "waiting"},
			},
			"ts":      apiAnyNumber,
			"trigger": "mira",
		}
		dashboard.wantFrames(chatFrame, cardFrame)
		asker.wantFrames(chatFrame, cardFrame)
		bystander.wantFrames()
		wantPushed(map[string]any{
			"kind":           "reply_card",
			"chat_id":        messageID,
			"reply_card_id":  cardID,
			"title":          "OffiCraft：需要你決定",
			"body":           "你有一張新的請示卡。",
			"needs_decision": true,
		})
	})
	t.Run("an omitted linked_task answers 400 naming both legal shapes and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"linked_task is required and has no default — say whether this ask is about a task. "+
				"Two legal shapes: send linked_task=null if it is NOT about a task (a plain unbound 請示), "+
				"or linked_task={\"task_id\": \"t-...\", \"step_id\": \"ts-...\"} to bind the ask to the "+
				"step it is about, which then holds in waiting_owner until you are answered. The server does "+
				"not infer a binding from the work you hold: a guess that missed used to open a card with no "+
				"等我回覆 hold and tell you nothing.")
		dashboard.wantFrames()
		wantPushed()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a linked_task naming a task but no step answers 400 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":{"task_id":"T-1","step_id":""}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"linked_task.step_id is required: a card bound to a task but to no step places no 等我回覆 hold, "+
				"so the task would finish underneath your question and the owner's answer would then be "+
				"rejected for good. Send linked_task={\"task_id\": \"t-...\", \"step_id\": \"ts-...\"} "+
				"naming the step you are on, or linked_task=null if this ask is not about a task.")
		dashboard.wantFrames()
		wantPushed()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a linked_task naming a step but no task answers 400 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":{"task_id":"","step_id":"ts-1"}}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"linked_task.task_id is required: name the task the step belongs to, or send linked_task=null "+
				"if this ask is not about a task.")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a linked_task naming a task nobody created answers 404 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":{"task_id":"T-9","step_id":"ts-1"}}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-9' not found")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a kind outside decision and action answers 400 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"poll","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":null}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "kind must be 'decision' or 'action'")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a blank summary answers 400 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"   ","options":[{"text":"出"}],"linked_task":null}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "summary must not be blank")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a select_mode outside single and multi answers 400 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","select_mode":"many","options":[{"text":"出"}],"linked_task":null}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "select_mode must be 'single' or 'multi'")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a card carrying no option at all answers 400 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[],"linked_task":null}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "options must carry at least one choice")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a fifth option on a single-select card answers 400 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"甲"},{"text":"乙"},{"text":"丙"},{"text":"丁"},{"text":"戊"}],"linked_task":null}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "a single-select card may carry at most 4 options")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a second ai_pick on a single-select card answers 400 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出","ai_pick":true},{"text":"不出","ai_pick":true}],"linked_task":null}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "a single-select card may mark at most one option ai_pick")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a blank option text answers 400 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"  "}],"linked_task":null}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "options must not be blank")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("an omitted summary key answers 422 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "mira", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","options":[{"text":"出"}],"linked_task":null}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: summary")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", "",
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":null}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		wantPushed()
		apiTestWantNoReplyCards(t, h, owner)
	})
}

func apiTestOpenReplyCard(t *testing.T, h http.Handler, token, body string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/reply-cards", token, body)
	if status != 200 {
		t.Fatalf("open reply card: %d %v", status, data)
	}
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatalf("open reply card must mint an id: %v", data)
	}
	return id
}

func apiTestReplyCardPane(t *testing.T, h http.Handler, token, query string) []any {
	t.Helper()
	rec := apiRequest(t, h, "GET", "/api/reply-cards"+query, token, "")
	if rec.Code != 200 {
		t.Fatalf("list reply cards%s: %d %s", query, rec.Code, rec.Body.String())
	}
	var got []any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("non-JSON body: %s", rec.Body.String())
	}
	return got
}

func apiTestWantNoReplyCards(t *testing.T, h http.Handler, token string) {
	t.Helper()
	apiWantValue(t, "waiting pane", any(apiTestReplyCardPane(t, h, token, "")), any([]any{}))
	_, counts := apiJSON(t, h, "GET", "/api/reply-cards/count", token, "")
	apiWantBody(t, counts, map[string]any{"waiting": 0, "answered": 0, "expired": 0})
}

func apiTestReplyCardFrame(seq int, cardID, from, status, trigger string) map[string]any {
	return map[string]any{
		"seq":   seq,
		"topic": "reply_card",
		"op":    "patch",
		"data": map[string]any{
			"entity":  "reply_card",
			"key":     "owner::" + cardID,
			"epoch":   seq,
			"deleted": false,
			"payload": map[string]any{"id": cardID, "from": from, "status": status},
		},
		"ts":      apiAnyNumber,
		"trigger": trigger,
	}
}

func TestReplyCardListItemOf(t *testing.T) {
	t.Skip("TODO: replyCardListItemOf builds one LIGHT list row (T-3f31 owner ruling: 卡只需要 title+決策): summary + status/timestamps + the decision digest on an answered card (picked option index + its ORIGINAL wording, answer text truncated to a preview, attachment COUNT) — never the body or the options full text (get_reply_card serves those).")
}

func TestReplyCardOptionWording(t *testing.T) {
	t.Skip("TODO: replyCardOptionWording resolves the circled indices back to the ORIGINAL wording, one entry per index and in the same order.")
}

func TestHandleListReplyCardsApiReplyCardsGet(t *testing.T) {
	t.Run("a station holding no card answers an empty waiting pane", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		apiWantValue(t, "waiting pane", any(apiTestReplyCardPane(t, h, owner, "")), any([]any{}))
		dashboard.wantFrames()
	})

	t.Run("the waiting pane leads with the longest-waiting card and carries no body or options", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		first := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","body":"客戶議價","options":[{"text":"漲","ai_pick":true},{"text":"不漲"}],"linked_task":null}`)
		second := apiTestOpenReplyCard(t, h, admin,
			`{"kind":"action","summary":"請批出貨","select_mode":"multi","options":[{"text":"甲"},{"text":"乙"},{"text":"丙"}],"linked_task":null}`)
		dashboard := apiTestListen(t, api, "")

		apiWantValue(t, "waiting pane", any(apiTestReplyCardPane(t, h, owner, "")), any([]any{
			map[string]any{
				"id":          first,
				"from":        apiTestPlainAgentID,
				"kind":        "decision",
				"summary":     "要不要漲價",
				"status":      "waiting",
				"created_ts":  apiAnyNumber,
				"answered_ts": nil,
				"expired_ts":  nil,
				"answer":      nil,
				"task":        nil,
			},
			map[string]any{
				"id":          second,
				"from":        "mira",
				"kind":        "action",
				"summary":     "請批出貨",
				"status":      "waiting",
				"created_ts":  apiAnyNumber,
				"answered_ts": nil,
				"expired_ts":  nil,
				"answer":      nil,
				"task":        nil,
			},
		}))
		dashboard.wantFrames()
	})

	t.Run("a positive limit keeps the pane's first rows", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		first := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`)
		apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":null}`)

		apiWantValue(t, "waiting pane", any(apiTestReplyCardPane(t, h, owner, "?limit=1")), any([]any{
			map[string]any{
				"id":          first,
				"from":        apiTestPlainAgentID,
				"kind":        "decision",
				"summary":     "要不要漲價",
				"status":      "waiting",
				"created_ts":  apiAnyNumber,
				"answered_ts": nil,
				"expired_ts":  nil,
				"answer":      nil,
				"task":        nil,
			},
		}))
	})

	t.Run("the answered pane leads with the newest answer and digests every circled option", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		single := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"},{"text":"不漲"}],"linked_task":null}`)
		multi := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"action","summary":"請批出貨","select_mode":"multi","options":[{"text":"甲"},{"text":"乙"},{"text":"丙"}],"linked_task":null}`)
		apiJSON(t, h, "POST", "/api/reply-cards/"+single+"/answer", owner, `{"option_idxs":[0],"text":"就漲"}`)
		apiJSON(t, h, "POST", "/api/reply-cards/"+multi+"/answer", owner, `{"option_idxs":[2,0]}`)

		apiWantValue(t, "answered pane", any(apiTestReplyCardPane(t, h, owner, "?status=answered")), any([]any{
			map[string]any{
				"id":          multi,
				"from":        apiTestPlainAgentID,
				"kind":        "action",
				"summary":     "請批出貨",
				"status":      "answered",
				"created_ts":  apiAnyNumber,
				"answered_ts": apiAnyNumber,
				"expired_ts":  nil,
				"answer": map[string]any{
					"option_idxs": []any{0, 2},
					"options":     []any{"甲", "丙"},
					"text":        "",
					"attachments": 0,
				},
				"task": nil,
			},
			map[string]any{
				"id":          single,
				"from":        apiTestPlainAgentID,
				"kind":        "decision",
				"summary":     "要不要漲價",
				"status":      "answered",
				"created_ts":  apiAnyNumber,
				"answered_ts": apiAnyNumber,
				"expired_ts":  nil,
				"answer": map[string]any{
					"option_idxs": []any{0},
					"options":     []any{"漲"},
					"text":        "就漲",
					"attachments": 0,
				},
				"task": nil,
			},
		}))
	})

	t.Run("an answer longer than the preview is truncated on the digest with an ellipsis", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		card := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`)
		apiJSON(t, h, "POST", "/api/reply-cards/"+card+"/answer", owner,
			`{"text":"`+strings.Repeat("字", 201)+`"}`)

		apiWantValue(t, "answered pane", any(apiTestReplyCardPane(t, h, owner, "?status=answered")), any([]any{
			map[string]any{
				"id":          card,
				"from":        apiTestPlainAgentID,
				"kind":        "decision",
				"summary":     "要不要漲價",
				"status":      "answered",
				"created_ts":  apiAnyNumber,
				"answered_ts": apiAnyNumber,
				"expired_ts":  nil,
				"answer": map[string]any{
					"option_idxs": nil,
					"options":     []any{},
					"text":        strings.Repeat("字", 200) + "…",
					"attachments": 0,
				},
				"task": nil,
			},
		}))
	})

	t.Run("the expired pane carries the retired card keyed off its expiry stamp", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		card := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`)
		apiJSON(t, h, "POST", "/api/reply-cards/"+card+"/expire", agent, `{}`)

		apiWantValue(t, "expired pane", any(apiTestReplyCardPane(t, h, owner, "?status=expired")), any([]any{
			map[string]any{
				"id":          card,
				"from":        apiTestPlainAgentID,
				"kind":        "decision",
				"summary":     "要不要漲價",
				"status":      "expired",
				"created_ts":  apiAnyNumber,
				"answered_ts": nil,
				"expired_ts":  apiAnyNumber,
				"answer":      nil,
				"task":        nil,
			},
		}))
		apiWantValue(t, "waiting pane", any(apiTestReplyCardPane(t, h, owner, "")), any([]any{}))
	})

	t.Run("a status outside the three panes answers 400", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/reply-cards?status=settled", owner, "")
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "status must be 'waiting', 'answered' or 'expired'")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/reply-cards", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleReplyCardCountApiReplyCardsCountGet(t *testing.T) {
	t.Run("a station holding no card answers three zeroes", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/reply-cards/count", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"waiting": 0, "answered": 0, "expired": 0})
		dashboard.wantFrames()
	})

	t.Run("each of the three panes is counted separately", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		answered := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`)
		expired := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":null}`)
		apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"還在等","options":[{"text":"等"}],"linked_task":null}`)
		apiJSON(t, h, "POST", "/api/reply-cards/"+answered+"/answer", owner, `{"option_idxs":[0]}`)
		apiJSON(t, h, "POST", "/api/reply-cards/"+expired+"/expire", agent, `{}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/reply-cards/count", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"waiting": 1, "answered": 1, "expired": 1})
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/reply-cards/count", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetReplyCardApiReplyCardsCardIdGet(t *testing.T) {
	t.Run("a waiting card is served in full with its body, options and chat anchor", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		statusCode, created := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要漲價","body":"客戶議價","options":[{"text":"漲","ai_pick":true},{"text":"不漲"}],"linked_task":null}`)
		if statusCode != 200 {
			t.Fatalf("open card: %d %v", statusCode, created)
		}
		cardID, _ := created["id"].(string)
		messageID, _ := created["chat_message_id"].(string)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":      cardID,
			"from":    apiTestPlainAgentID,
			"kind":    "decision",
			"summary": "要不要漲價",
			"body":    "客戶議價",
			"options": []any{
				map[string]any{"text": "漲", "ai_pick": true},
				map[string]any{"text": "不漲", "ai_pick": false},
			},
			"select_mode":     "single",
			"status":          "waiting",
			"created_ts":      apiAnyNumber,
			"attachments":     []any{},
			"answered_ts":     nil,
			"expired_ts":      nil,
			"chat_message_id": messageID,
			"answer":          nil,
			"task":            nil,
		})
		dashboard.wantFrames()
	})

	t.Run("an answered card carries the stored answer beside the original wording", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		statusCode, created := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"},{"text":"不漲"}],"linked_task":null}`)
		if statusCode != 200 {
			t.Fatalf("open card: %d %v", statusCode, created)
		}
		cardID, _ := created["id"].(string)
		messageID, _ := created["chat_message_id"].(string)
		apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner, `{"option_idxs":[1],"text":"先撐著"}`)

		status, data := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":      cardID,
			"from":    apiTestPlainAgentID,
			"kind":    "decision",
			"summary": "要不要漲價",
			"body":    "",
			"options": []any{
				map[string]any{"text": "漲", "ai_pick": false},
				map[string]any{"text": "不漲", "ai_pick": false},
			},
			"select_mode":     "single",
			"status":          "answered",
			"created_ts":      apiAnyNumber,
			"attachments":     []any{},
			"answered_ts":     apiAnyNumber,
			"expired_ts":      nil,
			"chat_message_id": messageID,
			"answer": map[string]any{
				"option_idxs": []any{1},
				"text":        "先撐著",
				"attachments": []any{},
			},
			"task": nil,
		})
	})

	t.Run("an unknown card id answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/reply-cards/rc-ghost", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "reply card 'rc-ghost' not found")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/reply-cards/rc-ghost", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestApplyReplyCardAnswer(t *testing.T) {
	t.Skip("TODO: applyReplyCardAnswer validates + stores one answer (shared by POST answer and PUT re-answer — same body, same rules), stamps answered_ts, fans the delta and writes the card DTO.")
}

func TestReleaseCardHold(t *testing.T) {
	t.Skip("TODO: releaseCardHold releases the waiting_owner HOLD a reply card placed on a task/step, fired when — and only when — the card leaves waiting through an OWNER action: the FIRST answer (POST /answer: waiting → answered) or the expire action (POST /expire: waiting → expired).")
}

func TestExpireWaitingCards(t *testing.T) {
	t.Skip("TODO: expireWaitingCards is the SERVER-SIDE card sweep: it applies the exact semantics of the expire route (status flip + expired_ts + releaseCardHold + delta) to every waiting card the predicate selects.")
}

func TestExpireWaitingCardsForTask(t *testing.T) {
	t.Skip("TODO: expireWaitingCardsForTask sweeps every waiting card bound to one task — the task is being reassigned away from its asker, or has just landed terminal (closeTask): either way nobody is left to consume an answer, so the card must not keep sitting in the owner's 等我回覆 pane counting toward the 紅點 with a 409 as its only reward.")
}

func TestExpireWaitingCardsFromMember(t *testing.T) {
	t.Skip("TODO: expireWaitingCardsFromMember sweeps every waiting card OPENED BY one member, fired when that member is dismissed (HandleDismissMember / dismissOutsourceWorkerByID): the asker is gone, so no answer can ever be delivered to it.")
}

func TestReconcileOrphanReplyCardsOnBoot(t *testing.T) {
	t.Skip("TODO: reconcileOrphanReplyCardsOnBoot retires the EXISTING orphans (T-4166 存量): a waiting card whose bound task is already terminal can never be answered (the answer route 409s it) and can never leave the owner's pane on its own, so it pins the cockpit red dot forever.")
}

func TestHandleAnswerReplyCardApiReplyCardsCardIdAnswerPost(t *testing.T) {
	openSingle := func(t *testing.T, h http.Handler, token string) string {
		t.Helper()
		return apiTestOpenReplyCard(t, h, token,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"},{"text":"不漲"}],"linked_task":null}`)
	}
	wantWaiting := func(t *testing.T, h http.Handler, owner, cardID string) {
		t.Helper()
		_, data := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		apiWantValue(t, "card.status", data["status"], "waiting")
		apiWantValue(t, "card.answer", data["answer"], nil)
		apiWantValue(t, "card.answered_ts", data["answered_ts"], nil)
	}

	t.Run("a first answer flips the card to answered and fans the delta to the owner and the asker", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openSingle(t, h, agent)
		dashboard := apiTestListen(t, api, "")
		asker := apiTestListen(t, api, apiTestPlainAgentID)
		bystander := apiTestListen(t, api, "mira")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner,
			`{"option_idxs":[0],"text":"就漲"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          cardID,
			"status":      "answered",
			"answered_ts": apiAnyNumber,
			"expired_ts":  nil,
			"answer": map[string]any{
				"option_idxs": []any{0},
				"text":        "就漲",
				"attachments": []any{},
			},
			"task_id": "",
			"step_id": "",
		})
		frame := apiTestReplyCardFrame(3, cardID, apiTestPlainAgentID, "answered", "owner")
		dashboard.wantFrames(frame)
		asker.wantFrames(frame)
		bystander.wantFrames()
		wantPushed()
	})

	t.Run("the circled options are stored deduped and ascending", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"action","summary":"請批出貨","select_mode":"multi","options":[{"text":"甲"},{"text":"乙"},{"text":"丙"}],"linked_task":null}`)

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner,
			`{"option_idxs":[2,0,2]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          cardID,
			"status":      "answered",
			"answered_ts": apiAnyNumber,
			"expired_ts":  nil,
			"answer": map[string]any{
				"option_idxs": []any{0, 2},
				"text":        "",
				"attachments": []any{},
			},
			"task_id": "",
			"step_id": "",
		})
	})

	t.Run("an answer carrying no option, no text and no attachment answers 400 and leaves the card waiting", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openSingle(t, h, agent)
		dashboard := apiTestListen(t, api, "")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner, `{"option_idxs":[]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "answer must carry an option, text, or an attachment")
		dashboard.wantFrames()
		wantPushed()
		wantWaiting(t, h, owner, cardID)
	})

	t.Run("an option index the card does not have answers 400 and leaves the card waiting", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openSingle(t, h, agent)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner, `{"option_idxs":[5]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "option_idxs out of range")
		dashboard.wantFrames()
		wantWaiting(t, h, owner, cardID)
	})

	t.Run("a second index on a single-select card answers 400 and leaves the card waiting", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openSingle(t, h, agent)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner, `{"option_idxs":[0,1]}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"this card is single-select: option_idxs may carry at most one index")
		dashboard.wantFrames()
		wantWaiting(t, h, owner, cardID)
	})

	t.Run("a card that is already answered answers 409 and keeps the first answer", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openSingle(t, h, agent)
		apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner, `{"option_idxs":[0],"text":"就漲"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner, `{"option_idxs":[1]}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"reply card '"+cardID+"' is already answered — revise it via PUT (重新決定)")
		dashboard.wantFrames()
		_, card := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		apiWantValue(t, "card.answer", card["answer"], map[string]any{
			"option_idxs": []any{0},
			"text":        "就漲",
			"attachments": []any{},
		})
	})

	t.Run("a card that already expired answers 409 and stays expired", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openSingle(t, h, agent)
		apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/expire", agent, `{}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner, `{"option_idxs":[0]}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"reply card '"+cardID+"' is expired — a terminal state; the agent opens a new card if the question still matters")
		dashboard.wantFrames()
		_, card := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		apiWantValue(t, "card.status", card["status"], "expired")
		apiWantValue(t, "card.answer", card["answer"], nil)
	})

	t.Run("an unknown card id answers 404", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/rc-ghost/answer", owner, `{"option_idxs":[0]}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "reply card 'rc-ghost' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openSingle(t, h, agent)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", agent, `{"option_idxs":[0]}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		wantWaiting(t, h, owner, cardID)
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openSingle(t, h, agent)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", "", `{"option_idxs":[0]}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		wantWaiting(t, h, owner, cardID)
	})
}

func TestHandleReanswerReplyCardApiReplyCardsCardIdAnswerPut(t *testing.T) {
	openAnswered := func(t *testing.T, h http.Handler, agent, owner string) string {
		t.Helper()
		cardID := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"},{"text":"不漲"}],"linked_task":null}`)
		if status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner,
			`{"option_idxs":[0],"text":"就漲"}`); status != 200 {
			t.Fatalf("first answer: %d %v", status, data)
		}
		return cardID
	}

	t.Run("a revision replaces the stored answer and keeps the card answered", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openAnswered(t, h, agent, owner)
		dashboard := apiTestListen(t, api, "")
		asker := apiTestListen(t, api, apiTestPlainAgentID)
		bystander := apiTestListen(t, api, "mira")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "PUT", "/api/reply-cards/"+cardID+"/answer", owner, `{"text":"改主意"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          cardID,
			"status":      "answered",
			"answered_ts": apiAnyNumber,
			"expired_ts":  nil,
			"answer": map[string]any{
				"option_idxs": nil,
				"text":        "改主意",
				"attachments": []any{},
			},
			"task_id": "",
			"step_id": "",
		})
		frame := apiTestReplyCardFrame(4, cardID, apiTestPlainAgentID, "answered", "owner")
		dashboard.wantFrames(frame)
		asker.wantFrames(frame)
		bystander.wantFrames()
		wantPushed()
		_, card := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		apiWantValue(t, "card.answer", card["answer"], map[string]any{
			"option_idxs": nil,
			"text":        "改主意",
			"attachments": []any{},
		})
	})

	t.Run("a card still waiting answers 409 and stays waiting", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/reply-cards/"+cardID+"/answer", owner, `{"text":"改主意"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"reply card '"+cardID+"' is not answered yet — answer it via POST")
		dashboard.wantFrames()
		_, card := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		apiWantValue(t, "card.status", card["status"], "waiting")
		apiWantValue(t, "card.answer", card["answer"], nil)
	})

	t.Run("an expired card answers 409 and cannot be re-decided", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`)
		apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/expire", agent, `{}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/reply-cards/"+cardID+"/answer", owner, `{"text":"改主意"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"reply card '"+cardID+"' is expired — a terminal state; it cannot be re-decided")
		dashboard.wantFrames()
		_, card := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		apiWantValue(t, "card.status", card["status"], "expired")
		apiWantValue(t, "card.answer", card["answer"], nil)
	})

	t.Run("an unknown card id answers 404", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/reply-cards/rc-ghost/answer", owner, `{"text":"改主意"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "reply card 'rc-ghost' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openAnswered(t, h, agent, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/reply-cards/"+cardID+"/answer", agent, `{"text":"改主意"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		_, card := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		apiWantValue(t, "card.answer", card["answer"], map[string]any{
			"option_idxs": []any{0},
			"text":        "就漲",
			"attachments": []any{},
		})
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openAnswered(t, h, agent, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PUT", "/api/reply-cards/"+cardID+"/answer", "", `{"text":"改主意"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		_, card := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		apiWantValue(t, "card.answer", card["answer"], map[string]any{
			"option_idxs": []any{0},
			"text":        "就漲",
			"attachments": []any{},
		})
	})
}

func TestCallerMayExpireCard(t *testing.T) {
	t.Skip("TODO: callerMayExpireCard is the in-handler half of the T-1b88 authorization (owner 2026-08-07, card rc-3ff94b116970): admin capability may expire any card, and an ordinary agent may expire exactly the cards IT opened.")
}

func TestHandleExpireReplyCardApiReplyCardsCardIdExpirePost(t *testing.T) {
	openCard := func(t *testing.T, h http.Handler, token string) string {
		t.Helper()
		return apiTestOpenReplyCard(t, h, token,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"},{"text":"不漲"}],"linked_task":null}`)
	}
	wantWaiting := func(t *testing.T, h http.Handler, owner, cardID string) {
		t.Helper()
		_, card := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		apiWantValue(t, "card.status", card["status"], "waiting")
		apiWantValue(t, "card.expired_ts", card["expired_ts"], nil)
	}

	t.Run("the card's own author retires it and fans the expired delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openCard(t, h, agent)
		dashboard := apiTestListen(t, api, "")
		asker := apiTestListen(t, api, apiTestPlainAgentID)
		bystander := apiTestListen(t, api, "mira")
		wantPushed := apiTestWebPushSink(t, api)

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/expire", agent, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          cardID,
			"status":      "expired",
			"answered_ts": nil,
			"expired_ts":  apiAnyNumber,
			"answer":      nil,
			"task_id":     "",
			"step_id":     "",
		})
		frame := apiTestReplyCardFrame(3, cardID, apiTestPlainAgentID, "expired", apiTestPlainAgentID)
		dashboard.wantFrames(frame)
		asker.wantFrames(frame)
		bystander.wantFrames()
		wantPushed()
		_, card := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		apiWantValue(t, "card.status", card["status"], "expired")
		apiWantValue(t, "card.answer", card["answer"], nil)
	})

	t.Run("an admin agent retires a card it did not open", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		admin := apiTestAgentToken(t, api, "mira", "")
		cardID := openCard(t, h, agent)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/expire", admin, `{}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          cardID,
			"status":      "expired",
			"answered_ts": nil,
			"expired_ts":  apiAnyNumber,
			"answer":      nil,
			"task_id":     "",
			"step_id":     "",
		})
		dashboard.wantFrames(apiTestReplyCardFrame(3, cardID, apiTestPlainAgentID, "expired", "mira"))
		apiWantValue(t, "waiting pane", any(apiTestReplyCardPane(t, h, owner, "")), any([]any{}))
	})

	t.Run("an agent that did not open the card answers 403 and leaves it waiting", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		admin := apiTestAgentToken(t, api, "mira", "")
		cardID := openCard(t, h, admin)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/expire", agent, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden",
			"only the card's own author (or the owner / an admin agent) may mark it expired")
		dashboard.wantFrames()
		wantWaiting(t, h, owner, cardID)
	})

	t.Run("an already answered card answers 409 and keeps its answer", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openCard(t, h, agent)
		apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner, `{"option_idxs":[0],"text":"就漲"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/expire", agent, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"reply card '"+cardID+"' is already answered — only a waiting card can expire")
		dashboard.wantFrames()
		_, card := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		apiWantValue(t, "card.status", card["status"], "answered")
		apiWantValue(t, "card.answer", card["answer"], map[string]any{
			"option_idxs": []any{0},
			"text":        "就漲",
			"attachments": []any{},
		})
	})

	t.Run("an already expired card answers 409", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openCard(t, h, agent)
		apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/expire", agent, `{}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/expire", agent, `{}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"reply card '"+cardID+"' is already expired — only a waiting card can expire")
		dashboard.wantFrames()
	})

	t.Run("an unknown card id answers 404", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/rc-ghost/expire", agent, `{}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "reply card 'rc-ghost' not found")
		dashboard.wantFrames()
	})

	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		machine := apiTestAgentToken(t, api, ServerSelfHost, "")
		cardID := openCard(t, h, agent)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/expire", machine, `{}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		dashboard.wantFrames()
		wantWaiting(t, h, owner, cardID)
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		cardID := openCard(t, h, agent)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/expire", "", `{}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		dashboard.wantFrames()
		wantWaiting(t, h, owner, cardID)
	})
}
