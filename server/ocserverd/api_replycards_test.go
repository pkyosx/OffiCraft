package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestPublishReplyCard(t *testing.T) {
	t.Run("an answered card publishes its partial delta to the owner and initiator", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		initiator := apiTestListen(t, api, "mira")

		api.publishReplyCard(ReplyCard{
			ID: "rc-1", FromMember: "mira", Status: "answered",
		}, "owner")

		want := apiTestReplyCardFrame(1, "rc-1", "mira", "answered", "owner")
		dashboard.wantFrames(want)
		initiator.wantFrames(want)
	})
}

func TestWaitingReplyCards(t *testing.T) {
	t.Run("waiting cards are ordered from oldest to newest and other statuses are omitted", func(t *testing.T) {
		got := waitingReplyCards([]ReplyCard{
			{ID: "late", Status: "waiting", CreatedTS: 30},
			{ID: "answered", Status: "answered", CreatedTS: 1},
			{ID: "early", Status: "waiting", CreatedTS: 10},
			{ID: "same", Status: "waiting", CreatedTS: 10},
		})
		want := []ReplyCard{
			{ID: "early", Status: "waiting", CreatedTS: 10},
			{ID: "same", Status: "waiting", CreatedTS: 10},
			{ID: "late", Status: "waiting", CreatedTS: 30},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("waitingReplyCards = %#v, want %#v", got, want)
		}
	})
}

func TestRecentAnsweredReplyCards(t *testing.T) {
	t.Run("answered cards inside the 24-hour window are newest first and the boundary is included", func(t *testing.T) {
		got := recentAnsweredReplyCards([]ReplyCard{
			{ID: "old", Status: "answered", AnsweredTS: 13599},
			{ID: "boundary", Status: "answered", AnsweredTS: 13600},
			{ID: "newest", Status: "answered", AnsweredTS: 99990},
			{ID: "waiting", Status: "waiting", AnsweredTS: 99999},
		}, 100000)
		want := []ReplyCard{
			{ID: "newest", Status: "answered", AnsweredTS: 99990},
			{ID: "boundary", Status: "answered", AnsweredTS: 13600},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("recentAnsweredReplyCards = %#v, want %#v", got, want)
		}
	})
}

func TestRecentExpiredReplyCards(t *testing.T) {
	t.Run("expired cards inside the 24-hour window are newest first and the boundary is included", func(t *testing.T) {
		got := recentExpiredReplyCards([]ReplyCard{
			{ID: "old", Status: "expired", ExpiredTS: 13599},
			{ID: "boundary", Status: "expired", ExpiredTS: 13600},
			{ID: "newest", Status: "expired", ExpiredTS: 99990},
			{ID: "answered", Status: "answered", ExpiredTS: 99999},
		}, 100000)
		want := []ReplyCard{
			{ID: "newest", Status: "expired", ExpiredTS: 99990},
			{ID: "boundary", Status: "expired", ExpiredTS: 13600},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("recentExpiredReplyCards = %#v, want %#v", got, want)
		}
	})
}

func TestValidateReplyCardOptions(t *testing.T) {
	pick := true
	optionSet := func(n int) []ReplyCardOptionDTO {
		options := make([]ReplyCardOptionDTO, n)
		for i := range options {
			options[i].Text = "option"
		}
		return options
	}
	t.Run("valid option text is trimmed and its recommendation flag is preserved", func(t *testing.T) {
		got, problem := validateReplyCardOptions([]ReplyCardOptionDTO{
			{Text: "  ship  ", AiPick: &pick},
			{Text: "hold"},
		}, "single")
		if problem != "" {
			t.Fatalf("validateReplyCardOptions(valid) problem = %q", problem)
		}
		want := []ReplyCardOption{{Text: "ship", AIPick: true}, {Text: "hold"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("validateReplyCardOptions(valid) = %#v, want %#v", got, want)
		}
	})

	cases := []struct {
		name    string
		options []ReplyCardOptionDTO
		mode    string
		want    string
	}{
		{name: "empty", want: "options must carry at least one choice"},
		{name: "single cap", options: optionSet(5), mode: "single", want: "a single-select card may carry at most 4 options"},
		{name: "multi cap", options: optionSet(21), mode: "multi", want: "a multi-select card may carry at most 20 options"},
		{name: "blank", options: []ReplyCardOptionDTO{{Text: "  "}}, mode: "single", want: "options must not be blank"},
		{name: "two single recommendations", options: []ReplyCardOptionDTO{{Text: "one", AiPick: &pick}, {Text: "two", AiPick: &pick}}, mode: "single", want: "a single-select card may mark at most one option ai_pick"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, problem := validateReplyCardOptions(tc.options, tc.mode)
			if problem != tc.want {
				t.Fatalf("validateReplyCardOptions problem = %q, want %q", problem, tc.want)
			}
		})
	}
}

func TestNormalizeAnswerOptionIdxs(t *testing.T) {
	t.Run("duplicate and unordered indices are stored once in ascending order", func(t *testing.T) {
		got := normalizeAnswerOptionIdxs([]int{2, 0, 2, -1, 0})
		want := []int{-1, 0, 2}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("normalizeAnswerOptionIdxs = %#v, want %#v", got, want)
		}
	})
	t.Run("an empty index list is represented as nil", func(t *testing.T) {
		if got := normalizeAnswerOptionIdxs([]int{}); got != nil {
			t.Fatalf("normalizeAnswerOptionIdxs(empty) = %#v, want nil", got)
		}
	})
	t.Run("a nil index list remains nil", func(t *testing.T) {
		if got := normalizeAnswerOptionIdxs(nil); got != nil {
			t.Fatalf("normalizeAnswerOptionIdxs(nil) = %#v, want nil", got)
		}
	})
}

func TestOpenReplyCard(t *testing.T) {
	t.Run("a valid ask stores its card and companion message and publishes both deltas", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		initiator := apiTestListen(t, api, "mira")
		wantPushed := apiTestWebPushSink(t, api)
		pick := true
		body := "full question body"
		card, problem, err := api.openReplyCard("mira", ReplyCardCreateDTO{
			Kind:    ReplyCardCreateDTOKind("decision"),
			Summary: "ship this",
			Body:    &body,
			Options: []ReplyCardOptionDTO{{Text: "ship", AiPick: &pick}, {Text: "hold"}},
		}, "", "")
		if err != nil || problem != "" {
			t.Fatalf("openReplyCard = card:%#v problem:%q err:%v", card, problem, err)
		}
		if card == nil {
			t.Fatal("openReplyCard returned nil card")
		}
		if card.ID == "" || card.ChatMessageID == "" || card.CreatedTS <= 0 || card.FromMember != "mira" ||
			card.Kind != "decision" || card.Summary != "ship this" || card.Body != "full question body" ||
			card.SelectMode != "single" || card.Status != "waiting" {
			t.Fatalf("opened card = %#v", card)
		}

		stored, err := d.GetReplyCard(card.ID)
		if err != nil || stored == nil {
			t.Fatalf("GetReplyCard: %#v, %v", stored, err)
		}
		if stored.ID != card.ID || stored.FromMember != "mira" || stored.Kind != "decision" ||
			stored.Summary != "ship this" || stored.Body != "full question body" ||
			!reflect.DeepEqual(stored.Options, []ReplyCardOption{{Text: "ship", AIPick: true}, {Text: "hold"}}) ||
			stored.SelectMode != "single" || stored.Status != "waiting" || stored.TaskID != "" || stored.TaskStepID != "" {
			t.Fatalf("stored card = %#v", stored)
		}
		messages, err := d.ListChat()
		if err != nil || len(messages) != 1 {
			t.Fatalf("ListChat = %d messages, err %v", len(messages), err)
		}
		message := messages[0]
		if message.ID != card.ChatMessageID || message.Sender != "mira" || message.Recipient != "owner" ||
			message.Body != "ship this" || !reflect.DeepEqual(message.Meta, map[string]any{"reply_card_id": card.ID}) {
			t.Fatalf("companion message = %#v", message)
		}

		chatFrame := map[string]any{
			"seq": 1, "topic": "chat", "op": "patch",
			"data": map[string]any{
				"entity": "chat", "key": "owner::" + card.ChatMessageID,
				"epoch": 1, "deleted": false,
				"payload": map[string]any{"id": card.ChatMessageID, "from": "mira", "to": "owner"},
			},
			"ts": apiAnyNumber, "trigger": "mira",
		}
		cardFrame := apiTestReplyCardFrame(2, card.ID, "mira", "waiting", "mira")
		dashboard.wantFrames(chatFrame, cardFrame)
		initiator.wantFrames(chatFrame, cardFrame)
		wantPushed(map[string]any{
			"kind": "reply_card", "chat_id": card.ChatMessageID, "reply_card_id": card.ID,
			"title": "OffiCraft：需要你決定", "body": "你有一張新的請示卡。", "needs_decision": true,
		})
	})

	t.Run("a task binding without a step returns the complete structural error", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		_, problem, err := api.openReplyCard("mira", ReplyCardCreateDTO{
			Kind:    ReplyCardCreateDTOKind("decision"),
			Summary: "orphan",
			Options: []ReplyCardOptionDTO{{Text: "yes"}},
		}, "T-1", "")
		want := "refusing to mint a reply card bound to task 'T-1' with no step: a step-less task binding places no 等我回覆 hold and orphans the card when the task closes"
		if err == nil || problem != "" || err.Error() != want {
			t.Fatalf("step-less binding = problem:%q err:%v, want %q", problem, err, want)
		}
	})
}

func TestReplyCardDTOOf(t *testing.T) {
	t.Run("an unbound waiting card is projected with the complete full-card shape", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		card := ReplyCard{
			ID: "rc-1", FromMember: "mira", Kind: "decision",
			Summary: "ask", Body: "body", Options: []ReplyCardOption{{Text: "yes"}},
			Status: "waiting", CreatedTS: 12, ChatMessageID: "c-1",
		}
		got, err := api.replyCardDTOOf(card)
		if err != nil {
			t.Fatalf("replyCardDTOOf(unbound): %v", err)
		}
		want := replyCardDTO{
			ID: "rc-1", From: "mira", Kind: "decision", Summary: "ask", Body: "body",
			Options: []ReplyCardOption{{Text: "yes"}}, SelectMode: "single", Status: "waiting", CreatedTS: 12,
			Attachments: []chatAttachmentDTO{}, ChatMessageID: "c-1",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unbound dto = %#v, want %#v", got, want)
		}
	})

	t.Run("a task-bound waiting card carries the task id, type and title", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		if err := d.PutTask(task); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
		card := ReplyCard{
			ID: "rc-1", FromMember: "mira", Kind: "decision",
			Summary: "ask", Body: "body", Options: []ReplyCardOption{{Text: "yes"}},
			Status: "waiting", CreatedTS: 12, ChatMessageID: "c-1", TaskID: "T-1", TaskStepID: "ts-1",
		}
		got, err := api.replyCardDTOOf(card)
		if err != nil {
			t.Fatalf("replyCardDTOOf(bound): %v", err)
		}
		want := replyCardDTO{
			ID: "rc-1", From: "mira", Kind: "decision", Summary: "ask", Body: "body",
			Options: []ReplyCardOption{{Text: "yes"}}, SelectMode: "single", Status: "waiting", CreatedTS: 12,
			Attachments: []chatAttachmentDTO{}, ChatMessageID: "c-1",
			Task: &taskRefDTO{ID: "T-1", TypeKey: "type-alpha", Title: "reconcile the yard ledger"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("bound dto = %#v, want %#v", got, want)
		}
	})
}

func TestWriteReplyCard(t *testing.T) {
	t.Run("a waiting action card is encoded with every full-card field", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		rec := httptest.NewRecorder()
		api.writeReplyCard(rec, ReplyCard{
			ID: "rc-1", FromMember: "mira", Kind: "action",
			Summary: "do it", Body: "details", Options: []ReplyCardOption{{Text: "yes", AIPick: true}},
			SelectMode: "multi", Status: "waiting", CreatedTS: 12, ChatMessageID: "c-1",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("writeReplyCard status = %d, want 200", rec.Code)
		}
		apiWantBody(t, apiTestDecodeJSONBody(t, rec), map[string]any{
			"id": "rc-1", "from": "mira", "kind": "action", "summary": "do it", "body": "details",
			"options":     []any{map[string]any{"text": "yes", "ai_pick": true}},
			"select_mode": "multi", "status": "waiting", "created_ts": 12,
			"attachments": []any{}, "answered_ts": nil, "expired_ts": nil,
			"chat_message_id": "c-1", "answer": nil, "task": nil,
		})
	})
}

func TestWriteReplyCardCreateReceipt(t *testing.T) {
	t.Run("a create receipt reports only the minted ids, timestamp and landed attachments", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		rec := httptest.NewRecorder()
		api.writeReplyCardCreateReceipt(rec, ReplyCard{
			ID: "rc-1", ChatMessageID: "c-1", CreatedTS: 12,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("writeReplyCardCreateReceipt status = %d, want 200", rec.Code)
		}
		apiWantBody(t, apiTestDecodeJSONBody(t, rec), map[string]any{
			"id": "rc-1", "chat_message_id": "c-1", "created_ts": 12, "attachments": []any{},
		})
	})
}

func TestWriteReplyCardTransitionReceipt(t *testing.T) {
	t.Run("an answered transition reports its normalized answer and released step", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		rec := httptest.NewRecorder()
		api.writeReplyCardTransitionReceipt(rec, ReplyCard{
			ID: "rc-1", Status: "answered", AnsweredTS: 14,
			AnswerOptionIdxs: []int{0, 2}, AnswerText: "approved", TaskID: "T-1", TaskStepID: "ts-1",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("answered receipt status = %d, want 200", rec.Code)
		}
		apiWantBody(t, apiTestDecodeJSONBody(t, rec), map[string]any{
			"id": "rc-1", "status": "answered", "answered_ts": 14, "expired_ts": nil,
			"answer": map[string]any{
				"option_idxs": []any{0, 2}, "text": "approved", "attachments": []any{},
			},
			"task_id": "T-1", "step_id": "ts-1",
		})
	})

	t.Run("an expired transition reports the expiry and no answer", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		rec := httptest.NewRecorder()
		api.writeReplyCardTransitionReceipt(rec, ReplyCard{
			ID: "rc-2", Status: "expired", ExpiredTS: 15,
			TaskID: "T-2", TaskStepID: "ts-2",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expired receipt status = %d, want 200", rec.Code)
		}
		apiWantBody(t, apiTestDecodeJSONBody(t, rec), map[string]any{
			"id": "rc-2", "status": "expired", "answered_ts": nil, "expired_ts": 15,
			"answer": nil, "task_id": "T-2", "step_id": "ts-2",
		})
	})
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
	api, _, d, _ := newAPITestServer(t)
	task := dalTestTask("T-1")
	if err := d.PutTask(task); err != nil {
		t.Fatalf("PutTask: %v", err)
	}

	answerText := strings.Repeat("字", replyCardAnswerTextPreview+1)
	answeredAt := 14.0
	got, err := api.replyCardListItemOf(ReplyCard{
		ID: "rc-1", FromMember: "mira", Kind: replyCardKindDecision,
		Summary: "choose a route", Body: "private question body",
		Options: []ReplyCardOption{{Text: "first"}, {Text: "second"}, {Text: "third"}},
		Status:  replyCardStatusAnswered, CreatedTS: 12, AnsweredTS: answeredAt,
		AnswerOptionIdxs: []int{2, 0, 99}, AnswerText: answerText,
		AnswerAttachments: []any{"att-1", "att-2"}, TaskID: task.ID,
	})
	if err != nil {
		t.Fatalf("replyCardListItemOf: %v", err)
	}
	wantText := string([]rune(answerText)[:replyCardAnswerTextPreview]) + "…"
	want := replyCardListItemDTO{
		ID: "rc-1", From: "mira", Kind: replyCardKindDecision,
		Summary: "choose a route", Status: replyCardStatusAnswered, CreatedTS: 12,
		AnsweredTS: &answeredAt,
		Answer: &replyCardAnswerBriefDTO{
			OptionIdxs: []int{2, 0, 99}, Options: []string{"third", "first"},
			Text: wantText, Attachments: 2,
		},
		Task: &taskRefDTO{ID: task.ID, TypeKey: task.TypeKey, Title: task.Title},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("answered list item = %#v, want %#v", got, want)
	}

	if got, err := api.replyCardListItemOf(ReplyCard{
		ID: "rc-waiting", FromMember: "mira", Kind: replyCardKindAction,
		Summary: "still waiting", Status: replyCardStatusWaiting, CreatedTS: 10,
	}); err != nil {
		t.Fatalf("waiting replyCardListItemOf: %v", err)
	} else if got.Answer != nil || got.AnsweredTS != nil || got.ExpiredTS != nil || got.Task != nil {
		t.Fatalf("waiting list item carries terminal fields: %#v", got)
	}

	expiredAt := 15.0
	if got, err := api.replyCardListItemOf(ReplyCard{
		ID: "rc-expired", FromMember: "mira", Kind: replyCardKindAction,
		Summary: "stale ask", Status: replyCardStatusExpired, CreatedTS: 11,
		ExpiredTS: expiredAt, AnswerText: "must not become a digest",
	}); err != nil {
		t.Fatalf("expired replyCardListItemOf: %v", err)
	} else if got.ExpiredTS == nil || *got.ExpiredTS != expiredAt || got.Answer != nil || got.AnsweredTS != nil {
		t.Fatalf("expired list item = %#v", got)
	}
}

func TestReplyCardOptionWording(t *testing.T) {
	card := ReplyCard{
		Options:          []ReplyCardOption{{Text: "first"}, {Text: "second"}, {Text: "third"}},
		AnswerOptionIdxs: []int{2, 0, 99, -1, 0},
	}
	got := replyCardOptionWording(card)
	want := []string{"third", "first", "first"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("replyCardOptionWording = %#v, want %#v", got, want)
	}
	if got := replyCardOptionWording(ReplyCard{AnswerOptionIdxs: []int{0}}); got == nil || len(got) != 0 {
		t.Fatalf("replyCardOptionWording with no options = %#v, want empty", got)
	}
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
	t.Run("an answer is normalized, persisted, fanned, and returned as a transition receipt", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)
		card := ReplyCard{
			ID: "rc-1", FromMember: "kip", Kind: replyCardKindAction,
			Summary: "choose routes", Options: []ReplyCardOption{
				{Text: "first"}, {Text: "second"}, {Text: "third"},
			}, SelectMode: "multi", Status: "waiting", CreatedTS: 12,
			ChatMessageID: "c-1",
		}
		if err := d.PutReplyCard(card); err != nil {
			t.Fatalf("PutReplyCard: %v", err)
		}
		dashboard := apiTestListen(t, api, "")

		var req *http.Request
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) {
			req = r
			r.Body = io.NopCloser(strings.NewReader(`{"option_idxs":[2,0,2],"text":" approved "}`))
		})
		rec := httptest.NewRecorder()
		api.applyReplyCardAnswer(rec, req, card)
		if rec.Code != http.StatusOK {
			t.Fatalf("applyReplyCardAnswer status = %d, want 200 (%s)", rec.Code, rec.Body.String())
		}
		apiWantBody(t, apiTestDecodeJSONBody(t, rec), map[string]any{
			"id": "rc-1", "status": "answered", "answered_ts": apiAnyNumber,
			"expired_ts": nil,
			"answer": map[string]any{
				"option_idxs": []any{0, 2}, "text": "approved", "attachments": []any{},
			},
			"task_id": "", "step_id": "",
		})

		stored, err := d.GetReplyCard(card.ID)
		if err != nil || stored == nil {
			t.Fatalf("GetReplyCard: %#v, %v", stored, err)
		}
		if stored.Status != "answered" || stored.AnsweredTS <= 0 || stored.ExpiredTS != 0 ||
			!reflect.DeepEqual(stored.AnswerOptionIdxs, []int{0, 2}) ||
			stored.AnswerText != "approved" || len(stored.AnswerAttachments) != 0 {
			t.Fatalf("stored answer = %#v", stored)
		}
		dashboard.wantFrames(apiTestReplyCardFrame(1, "rc-1", "kip", "answered", "owner"))
	})

	t.Run("an empty answer returns a validation error and leaves the card waiting", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)
		card := ReplyCard{
			ID: "rc-1", FromMember: "kip", Kind: replyCardKindDecision,
			Summary: "choose", Options: []ReplyCardOption{{Text: "yes"}},
			SelectMode: "single", Status: "waiting", CreatedTS: 12,
		}
		if err := d.PutReplyCard(card); err != nil {
			t.Fatalf("PutReplyCard: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		var req *http.Request
		taskTestUnderCaller(t, api, d, owner, func(r *http.Request) {
			req = r
			r.Body = io.NopCloser(strings.NewReader(`{"option_idxs":[]}`))
		})
		rec := httptest.NewRecorder()
		api.applyReplyCardAnswer(rec, req, card)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("applyReplyCardAnswer status = %d, want 400 (%s)", rec.Code, rec.Body.String())
		}
		apiWantError(t, apiTestDecodeJSONBody(t, rec), "validation_error",
			"answer must carry an option, text, or an attachment")
		dashboard.wantFrames()
		stored, err := d.GetReplyCard(card.ID)
		if err != nil || stored == nil {
			t.Fatalf("GetReplyCard: %#v, %v", stored, err)
		}
		if stored.Status != "waiting" || stored.AnsweredTS != 0 || stored.AnswerText != "" ||
			stored.AnswerOptionIdxs != nil || len(stored.AnswerAttachments) != 0 {
			t.Fatalf("card changed after invalid answer = %#v", stored)
		}
	})
}

func TestReleaseCardHold(t *testing.T) {
	t.Run("settling a held card restores its step and task and publishes the task state", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		task.Status = "waiting_owner"
		task.WaitingReason = ""
		task.ClosedTS = 0
		task.CloseoutTS = 0
		step := dalTestStep("ts-1", task.ID)
		step.Status = "waiting_owner"
		step.ReplyCardID = "rc-1"
		card := ReplyCard{ID: "rc-1", FromMember: "kip", Kind: "decision", Status: "answered", TaskID: task.ID, TaskStepID: step.ID}
		if err := d.PutTask(task); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
		if err := d.PutTaskStep(step); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		if err := d.PutReplyCard(card); err != nil {
			t.Fatalf("PutReplyCard: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, task.ExecutorID)

		if err := api.releaseCardHold(card, "owner"); err != nil {
			t.Fatalf("releaseCardHold: %v", err)
		}
		steps, err := d.ListTaskSteps(task.ID)
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		if len(steps) != 1 || steps[0].Status != "in_progress" || steps[0].ReplyCardID != "rc-1" {
			t.Fatalf("released step = %#v", steps)
		}
		stored, err := d.GetTask(task.ID)
		if err != nil || stored == nil {
			t.Fatalf("GetTask: %#v, %v", stored, err)
		}
		if stored.Status != "in_progress" || stored.UpdatedTS <= task.UpdatedTS || stored.ClosedTS != 0 {
			t.Fatalf("released task = %#v", stored)
		}
		frame := map[string]any{
			"seq": 1, "topic": "task", "op": "patch",
			"data": map[string]any{
				"entity": "task", "key": "owner::T-1", "epoch": 1, "deleted": false,
				"payload": map[string]any{"id": "T-1", "priority": "high", "status": "in_progress"},
			},
			"ts": apiAnyNumber, "trigger": "owner",
		}
		dashboard.wantFrames(frame)
		executor.wantFrames(frame)
	})

	t.Run("a terminal task remains closed when its orphaned card is settled", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		task.Status = "done"
		task.ClosedTS = 30
		step := dalTestStep("ts-1", task.ID)
		step.Status = "waiting_owner"
		step.ReplyCardID = "rc-1"
		card := ReplyCard{ID: "rc-1", FromMember: "kip", Kind: "decision", Status: "answered", TaskID: task.ID, TaskStepID: step.ID}
		if err := d.PutTask(task); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
		if err := d.PutTaskStep(step); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		if err := d.PutReplyCard(card); err != nil {
			t.Fatalf("PutReplyCard: %v", err)
		}
		if err := api.releaseCardHold(card, "owner"); err != nil {
			t.Fatalf("releaseCardHold: %v", err)
		}
		storedTask, err := d.GetTask(task.ID)
		if err != nil || storedTask == nil {
			t.Fatalf("GetTask: %#v, %v", storedTask, err)
		}
		if !reflect.DeepEqual(*storedTask, task) {
			t.Fatalf("terminal task changed: got %#v, want %#v", *storedTask, task)
		}
		storedSteps, err := d.ListTaskSteps(task.ID)
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		if len(storedSteps) != 1 || !reflect.DeepEqual(storedSteps[0], step) {
			t.Fatalf("terminal task step changed: got %#v, want %#v", storedSteps, step)
		}
	})
}

func TestExpireWaitingCards(t *testing.T) {
	t.Run("the sweep expires only selected waiting cards and publishes their terminal state", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		cards := []ReplyCard{
			{ID: "rc-target", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 10},
			{ID: "rc-other", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 11},
			{ID: "rc-answered", FromMember: "mira", Kind: "decision", Status: "answered", CreatedTS: 12},
		}
		for _, card := range cards {
			if err := d.PutReplyCard(card); err != nil {
				t.Fatalf("PutReplyCard(%q): %v", card.ID, err)
			}
		}
		dashboard := apiTestListen(t, api, "")
		initiator := apiTestListen(t, api, "mira")

		count, err := api.expireWaitingCards(func(card ReplyCard) bool {
			return card.ID == "rc-target"
		}, 42, "sweep")
		if err != nil {
			t.Fatalf("expireWaitingCards: %v", err)
		}
		if count != 1 {
			t.Fatalf("expired count = %d, want 1", count)
		}
		got, err := d.ListReplyCards()
		if err != nil {
			t.Fatalf("ListReplyCards: %v", err)
		}
		want := []ReplyCard{
			{ID: "rc-target", FromMember: "mira", Kind: "decision", SelectMode: "single", Status: "expired", CreatedTS: 10, ExpiredTS: 42, AnswerAttachments: []any{}, Attachments: []any{}, Options: []ReplyCardOption{}},
			{ID: "rc-other", FromMember: "mira", Kind: "decision", SelectMode: "single", Status: "waiting", CreatedTS: 11, AnswerAttachments: []any{}, Attachments: []any{}, Options: []ReplyCardOption{}},
			{ID: "rc-answered", FromMember: "mira", Kind: "decision", SelectMode: "single", Status: "answered", CreatedTS: 12, AnswerAttachments: []any{}, Attachments: []any{}, Options: []ReplyCardOption{}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("reply cards after sweep = %#v, want %#v", got, want)
		}
		frame := apiTestReplyCardFrame(1, "rc-target", "mira", "expired", "sweep")
		dashboard.wantFrames(frame)
		initiator.wantFrames(frame)
	})
}

func TestExpireWaitingCardsForTask(t *testing.T) {
	t.Run("cards bound to the named task expire while cards for other tasks remain waiting", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		cards := []ReplyCard{
			{ID: "rc-task", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 10, TaskID: "T-1", TaskStepID: "ts-1"},
			{ID: "rc-other", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 11, TaskID: "T-2", TaskStepID: "ts-2"},
			{ID: "rc-plain", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 12},
		}
		for _, card := range cards {
			if err := d.PutReplyCard(card); err != nil {
				t.Fatalf("PutReplyCard(%q): %v", card.ID, err)
			}
		}
		dashboard := apiTestListen(t, api, "")

		count, err := api.expireWaitingCardsForTask("T-1", 42, "reassign")
		if err != nil {
			t.Fatalf("expireWaitingCardsForTask: %v", err)
		}
		if count != 1 {
			t.Fatalf("expired count = %d, want 1", count)
		}
		got, err := d.ListReplyCards()
		if err != nil {
			t.Fatalf("ListReplyCards: %v", err)
		}
		if got[0].Status != "expired" || got[0].ExpiredTS != 42 || got[1].Status != "waiting" || got[2].Status != "waiting" {
			t.Fatalf("cards after task sweep = %#v", got)
		}
		dashboard.wantFrames(apiTestReplyCardFrame(1, "rc-task", "mira", "expired", "reassign"))
	})

	t.Run("an empty task id is rejected before any card is selected", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		_, err := api.expireWaitingCardsForTask("", 42, "reassign")
		if err == nil || err.Error() != "expireWaitingCardsForTask: blank task id" {
			t.Fatalf("empty task id error = %v", err)
		}
	})
}

func TestExpireWaitingCardsFromMember(t *testing.T) {
	t.Run("cards opened by the dismissed member expire while other authors remain waiting", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		cards := []ReplyCard{
			{ID: "rc-member", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 10},
			{ID: "rc-other", FromMember: "kip", Kind: "decision", Status: "waiting", CreatedTS: 11},
			{ID: "rc-answered", FromMember: "mira", Kind: "decision", Status: "answered", CreatedTS: 12},
		}
		for _, card := range cards {
			if err := d.PutReplyCard(card); err != nil {
				t.Fatalf("PutReplyCard(%q): %v", card.ID, err)
			}
		}
		dashboard := apiTestListen(t, api, "")
		initiator := apiTestListen(t, api, "mira")

		count, err := api.expireWaitingCardsFromMember("mira", 42, "dismiss")
		if err != nil {
			t.Fatalf("expireWaitingCardsFromMember: %v", err)
		}
		if count != 1 {
			t.Fatalf("expired count = %d, want 1", count)
		}
		got, err := d.ListReplyCards()
		if err != nil {
			t.Fatalf("ListReplyCards: %v", err)
		}
		if got[0].Status != "expired" || got[0].ExpiredTS != 42 || got[1].Status != "waiting" || got[2].Status != "answered" {
			t.Fatalf("cards after member sweep = %#v", got)
		}
		frame := apiTestReplyCardFrame(1, "rc-member", "mira", "expired", "dismiss")
		dashboard.wantFrames(frame)
		initiator.wantFrames(frame)
	})

	t.Run("an empty member id is rejected before any card is selected", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		_, err := api.expireWaitingCardsFromMember("", 42, "dismiss")
		if err == nil || err.Error() != "expireWaitingCardsFromMember: blank member id" {
			t.Fatalf("empty member id error = %v", err)
		}
	})
}

func TestReconcileOrphanReplyCardsOnBoot(t *testing.T) {
	t.Run("waiting cards bound to closed or missing tasks expire while live and unbound cards remain", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		closed := dalTestTask("T-closed")
		closed.Status = "done"
		live := dalTestTask("T-live")
		live.Status = "in_progress"
		for _, task := range []Task{closed, live} {
			if err := d.PutTask(task); err != nil {
				t.Fatalf("PutTask(%q): %v", task.ID, err)
			}
		}
		cards := []ReplyCard{
			{ID: "rc-closed", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 10, TaskID: "T-closed", TaskStepID: "ts-closed"},
			{ID: "rc-missing", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 11, TaskID: "T-missing", TaskStepID: "ts-missing"},
			{ID: "rc-live", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 12, TaskID: "T-live", TaskStepID: "ts-live"},
			{ID: "rc-plain", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 13},
		}
		for _, card := range cards {
			if err := d.PutReplyCard(card); err != nil {
				t.Fatalf("PutReplyCard(%q): %v", card.ID, err)
			}
		}
		dashboard := apiTestListen(t, api, "")
		initiator := apiTestListen(t, api, "mira")

		count, err := api.reconcileOrphanReplyCardsOnBoot()
		if err != nil {
			t.Fatalf("reconcileOrphanReplyCardsOnBoot: %v", err)
		}
		if count != 2 {
			t.Fatalf("reconciled count = %d, want 2", count)
		}
		got, err := d.ListReplyCards()
		if err != nil {
			t.Fatalf("ListReplyCards: %v", err)
		}
		if got[0].Status != "expired" || got[0].ExpiredTS <= 0 || got[1].Status != "expired" || got[1].ExpiredTS <= 0 ||
			got[2].Status != "waiting" || got[3].Status != "waiting" {
			t.Fatalf("cards after boot reconciliation = %#v", got)
		}
		closedAfter, err := d.GetTask("T-closed")
		if err != nil || closedAfter == nil {
			t.Fatalf("GetTask(T-closed): %#v, %v", closedAfter, err)
		}
		if !reflect.DeepEqual(*closedAfter, closed) {
			t.Fatalf("closed task changed: got %#v, want %#v", *closedAfter, closed)
		}
		liveAfter, err := d.GetTask("T-live")
		if err != nil || liveAfter == nil {
			t.Fatalf("GetTask(T-live): %#v, %v", liveAfter, err)
		}
		if !reflect.DeepEqual(*liveAfter, live) {
			t.Fatalf("live task changed: got %#v, want %#v", *liveAfter, live)
		}
		dashboard.wantFrames(
			apiTestReplyCardFrame(1, "rc-closed", "mira", "expired", "boot-reconcile"),
			apiTestReplyCardFrame(2, "rc-missing", "mira", "expired", "boot-reconcile"),
		)
		initiator.wantFrames(
			apiTestReplyCardFrame(1, "rc-closed", "mira", "expired", "boot-reconcile"),
			apiTestReplyCardFrame(2, "rc-missing", "mira", "expired", "boot-reconcile"),
		)
	})
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
	t.Run("the owner and admin agent may expire a card opened by another member", func(t *testing.T) {
		api, _, d, owner := newAPITestServer(t)
		card := ReplyCard{ID: "rc-1", FromMember: "kip"}
		for _, tc := range []struct {
			name  string
			token string
		}{
			{name: "owner", token: owner},
			{name: "admin agent", token: apiTestAgentToken(t, api, "mira", "")},
		} {
			t.Run(tc.name, func(t *testing.T) {
				taskTestUnderCaller(t, api, d, tc.token, func(r *http.Request) {
					if !api.callerMayExpireCard(r, card) {
						t.Fatalf("callerMayExpireCard(%q) = false, want true", tc.name)
					}
				})
			})
		}
	})

	t.Run("a plain agent may expire only a card it opened", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		token := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		for _, tc := range []struct {
			name     string
			from     string
			wantPass bool
		}{
			{name: "own card", from: apiTestPlainAgentID, wantPass: true},
			{name: "another member card", from: "mira", wantPass: false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				taskTestUnderCaller(t, api, d, token, func(r *http.Request) {
					got := api.callerMayExpireCard(r, ReplyCard{ID: "rc-1", FromMember: tc.from})
					if got != tc.wantPass {
						t.Fatalf("callerMayExpireCard(from=%q) = %v, want %v", tc.from, got, tc.wantPass)
					}
				})
			})
		}
	})
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
