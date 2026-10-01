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
		}, nil, nil, nil, "mira")
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
		}, &Task{ID: "T-1", Status: TaskStatusInProgress}, nil, nil, "mira")
		want := "refusing to mint a reply card bound to task 'T-1' with no step: a step-less task binding places no 等我回覆 hold and orphans the card when the task closes"
		if err == nil || problem != "" || err.Error() != want {
			t.Fatalf("step-less binding = problem:%q err:%v, want %q", problem, err, want)
		}
	})

	// Both halves of the pair the id guards cannot see: their subject is the
	// derived id, so a pointer carrying a blank one reads to them as "nothing
	// was bound". Reached only from inside this package, which is why they are
	// driven here rather than through the endpoint.
	t.Run("a step pointer whose id is blank, with no task, is refused instead of dereferencing the missing task", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		_, problem, err := api.openReplyCard("mira", ReplyCardCreateDTO{
			Kind:    ReplyCardCreateDTOKind("decision"),
			Summary: "orphan",
			Options: []ReplyCardOptionDTO{{Text: "yes"}},
		}, nil, &TaskStep{ID: ""}, nil, "mira")
		want := "refusing to mint a reply card from a half-resolved binding: the task and the step must be resolved together or not at all"
		if err == nil || problem != "" || err.Error() != want {
			t.Fatalf("blank-id step with no task = problem:%q err:%v, want %q", problem, err, want)
		}
	})

	t.Run("a card that both binds a step and names a task without one is refused", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		_, problem, err := api.openReplyCard("mira", ReplyCardCreateDTO{
			Kind:    ReplyCardCreateDTOKind("decision"),
			Summary: "both",
			Options: []ReplyCardOptionDTO{{Text: "yes"}},
		}, &Task{ID: "T-1", Status: TaskStatusInProgress}, &TaskStep{ID: "ts-1"}, &Task{ID: "T-1"}, "mira")
		want := "refusing to mint a reply card that both binds a step and names a task without one"
		if err == nil || problem != "" || err.Error() != want {
			t.Fatalf("bind and about = problem:%q err:%v, want %q", problem, err, want)
		}
	})

	t.Run("a card about a task whose id is blank is refused", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		_, problem, err := api.openReplyCard("mira", ReplyCardCreateDTO{
			Kind:    ReplyCardCreateDTOKind("decision"),
			Summary: "blank",
			Options: []ReplyCardOptionDTO{{Text: "yes"}},
		}, nil, nil, &Task{ID: ""}, "mira")
		want := "refusing to mint a reply card about a task with a blank id"
		if err == nil || problem != "" || err.Error() != want {
			t.Fatalf("blank about = problem:%q err:%v, want %q", problem, err, want)
		}
	})

	t.Run("a task pointer whose id is blank, with no step, is refused instead of minting an unbound card", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		_, problem, err := api.openReplyCard("mira", ReplyCardCreateDTO{
			Kind:    ReplyCardCreateDTOKind("decision"),
			Summary: "orphan",
			Options: []ReplyCardOptionDTO{{Text: "yes"}},
		}, &Task{ID: "", Status: TaskStatusInProgress}, nil, nil, "mira")
		want := "refusing to mint a reply card from a half-resolved binding: the task and the step must be resolved together or not at all"
		if err == nil || problem != "" || err.Error() != want {
			t.Fatalf("blank-id task with no step = problem:%q err:%v, want %q", problem, err, want)
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
			"chat_message_id": "c-1", "answer": nil, "task": nil, "task_executor": "",
		})
	})
}

func TestWriteReplyCardCreateReceipt(t *testing.T) {
	t.Run("a create receipt reports only the minted ids, timestamp, landed attachments and the hold note", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		rec := httptest.NewRecorder()
		api.writeReplyCardCreateReceipt(rec, ReplyCard{
			ID: "rc-1", ChatMessageID: "c-1", CreatedTS: 12,
		}, "the task will not wait")
		if rec.Code != http.StatusOK {
			t.Fatalf("writeReplyCardCreateReceipt status = %d, want 200", rec.Code)
		}
		apiWantBody(t, apiTestDecodeJSONBody(t, rec), map[string]any{
			"id": "rc-1", "chat_message_id": "c-1", "created_ts": 12, "attachments": []any{},
			"hold_note": "the task will not wait",
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
			"hold_note":       "",
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
				"payload": map[string]any{"id": cardID, "from": "mira", "status": "waiting", "task_executor": ""},
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

	t.Run("a non-executor binding a step answers 403 pointing at about_task_id, opens no card and holds nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		_, steps := apiTestTwoStepTask(t, api, h, owner)
		intruder := apiTestAgentToken(t, api, "joey", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", intruder,
			`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],`+
				`"linked_task":{"task_id":"T-1","step_id":"`+steps[0]+`"}}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden",
			"caller is not the task's executor — only the task's executor can make one of its steps "+
				"wait on a card. To tell the owner which task this ask is about without holding a step, "+
				"send linked_task=null with about_task_id.")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
		apiTestWantStepStatuses(t, d, "T-1", StepStatusInProgress, StepStatusPending)
	})

	t.Run("about_task_id with linked_task null opens a card under that task that holds no step, and hold_note names who is told", func(t *testing.T) {
		const holdNote = "This card is about task 'T-1' but holds none of its steps, so that task keeps running and " +
			"will NOT wait for this answer. When the owner answers, whoever executes the task at that moment " +
			"is told as well as you"
		cases := []struct {
			name, asker, wantNote string
			executor              bool
		}{
			{"asked by another member", "joey", holdNote + " (right now 'kip').", true},
			{"asked by the executor itself", "kip", holdNote + " (right now that is you).", true},
			{"about a task with no executor", "joey", holdNote + " (nobody executes it right now).", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				api, h, d, owner := newAPITestServer(t)
				if tc.executor {
					apiTestTwoStepTask(t, api, h, owner)
				} else {
					unassigned := dalTestTask("T-1")
					unassigned.ExecutorID, unassigned.Lock, unassigned.ReassignedFrom = "", "", ""
					if err := d.PutTask(unassigned); err != nil {
						t.Fatalf("PutTask: %v", err)
					}
				}
				asker := apiTestAgentToken(t, api, tc.asker, "")
				before, err := d.GetTask("T-1")
				if err != nil {
					t.Fatalf("GetTask: %v", err)
				}

				status, data := apiJSON(t, h, "POST", "/api/reply-cards", asker,
					`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],`+
						`"linked_task":null,"about_task_id":" T-1 "}`)
				if status != 200 {
					t.Fatalf("want 200, got %d (%v)", status, data)
				}
				apiWantBody(t, data, map[string]any{
					"id":              apiAnyString,
					"chat_message_id": apiAnyString,
					"created_ts":      apiAnyNumber,
					"attachments":     []any{},
					"hold_note":       tc.wantNote,
				})
				cardID, _ := data["id"].(string)
				stored, err := d.GetReplyCard(cardID)
				if err != nil || stored == nil {
					t.Fatalf("GetReplyCard: %#v, %v", stored, err)
				}
				if stored.TaskID != "T-1" || stored.TaskStepID != "" || stored.Status != replyCardStatusWaiting {
					t.Fatalf("stored card task=%q step=%q status=%q, want T-1 / \"\" / waiting",
						stored.TaskID, stored.TaskStepID, stored.Status)
				}
				after, err := d.GetTask("T-1")
				if err != nil {
					t.Fatalf("GetTask: %v", err)
				}
				if after.Status != before.Status || after.UpdatedTS != before.UpdatedTS {
					t.Fatalf("task moved: status %q→%q updated_ts %v→%v",
						before.Status, after.Status, before.UpdatedTS, after.UpdatedTS)
				}
				if tc.executor {
					apiTestWantStepStatuses(t, d, "T-1", StepStatusInProgress, StepStatusPending)
				}
			})
		}
	})

	t.Run("a card about another member's task fans its create delta to that task's executor and names the predecessor under a reassign hold", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestTwoStepTask(t, api, h, owner)
		if status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/reassign", owner,
			`{"target":{"kind":"staff","member_id":"mira"}}`); status != 200 {
			t.Fatalf("reassign: %d %v", status, data)
		}
		asker := apiTestAgentToken(t, api, "joey", "")
		predecessor := apiTestListen(t, api, "kip")
		successor := apiTestListen(t, api, "mira")
		author := apiTestListen(t, api, "joey")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", asker,
			`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],"linked_task":null,"about_task_id":"T-1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantValue(t, "hold_note", data["hold_note"],
			"This card is about task 'T-1' but holds none of its steps, so that task keeps running and "+
				"will NOT wait for this answer. When the owner answers, whoever executes the task at that moment "+
				"is told as well as you (right now 'kip').")
		cardID, _ := data["id"].(string)
		frame := apiTestAboutCardFrame(cardID, "joey", "waiting", "joey", "kip")
		predecessor.wantFrames(frame)
		successor.wantFrames()
		author.wantFrames(map[string]any{
			"seq": apiAnyNumber, "topic": "chat", "op": "patch",
			"data": map[string]any{
				"entity": "chat", "key": apiAnyString, "epoch": apiAnyNumber, "deleted": false,
				"payload": map[string]any{"id": apiAnyString, "from": "joey", "to": "owner"},
			},
			"ts": apiAnyNumber, "trigger": "joey",
		}, frame)
	})

	t.Run("binding a card to the step that waits on the outside world answers 409 and leaves the wait in place", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		executor, steps := apiTestTwoStepTask(t, api, h, owner)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[0]+"/status", executor,
			`{"status":"waiting_external","waiting_reason":"CI"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", executor,
			`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],`+
				`"linked_task":{"task_id":"T-1","step_id":"`+steps[0]+`"}}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"step '"+steps[0]+"' is waiting on the outside world (waiting_external) — binding a card to it "+
				"would drop that wait; bind the card to a step that is not waiting")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
		apiTestWantStepStatuses(t, d, "T-1", StepStatusWaitingExternal, StepStatusPending)
		stored, err := d.ListTaskSteps("T-1")
		if err != nil || stored[0].WaitingReason != "CI" {
			t.Fatalf("step 0 waiting_reason = %q (%v), want CI", stored[0].WaitingReason, err)
		}
	})

	t.Run("a task that has not started or is ready to close still refuses a bound card with 409", func(t *testing.T) {
		cases := []struct {
			name, wantStatus string
			prepare          func(t *testing.T, h http.Handler, executor string, steps []string)
		}{
			{"not_started", "not_started", nil},
			{"ready_for_done", "ready_for_done", func(t *testing.T, h http.Handler, executor string, steps []string) {
				apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[0]+"/status", executor, `{"status":"done"}`)
				apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[1]+"/status", executor, `{"status":"in_progress"}`)
				apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[1]+"/status", executor, `{"status":"done"}`)
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				api, h, d, owner := newAPITestServer(t)
				var executor string
				var steps []string
				if tc.prepare == nil {
					apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
					executor = apiTestAgentToken(t, api, "kip", "")
					apiJSON(t, h, "POST", "/api/tasks/T-1/plan", executor,
						`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`)
					planned, err := d.ListTaskSteps("T-1")
					if err != nil || len(planned) != 2 {
						t.Fatalf("ListTaskSteps: %d, %v", len(planned), err)
					}
					steps = []string{planned[0].ID, planned[1].ID}
				} else {
					executor, steps = apiTestTwoStepTask(t, api, h, owner)
					tc.prepare(t, h, executor, steps)
				}
				task, err := d.GetTask("T-1")
				if err != nil || task.Status != tc.wantStatus {
					t.Fatalf("precondition: task = %#v, %v, want %s", task, err, tc.wantStatus)
				}
				dashboard := apiTestListen(t, api, "")

				status, data := apiJSON(t, h, "POST", "/api/reply-cards", executor,
					`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],`+
						`"linked_task":{"task_id":"T-1","step_id":"`+steps[1]+`"}}`)
				if status != 409 {
					t.Fatalf("want 409, got %d (%v)", status, data)
				}
				apiWantError(t, data, "conflict",
					"a card can only bind to an in_progress, waiting_owner or waiting_external task (is "+tc.wantStatus+")")
				dashboard.wantFrames()
				apiTestWantNoReplyCards(t, h, owner)
			})
		}
	})

	t.Run("about_task_id naming a task nobody created answers 404 before the body is judged, and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "joey", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"   ","options":[{"text":"走 A"}],"linked_task":null,"about_task_id":"T-9"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "task 'T-9' not found")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("about_task_id naming a closed task answers 409 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestTwoStepTask(t, api, h, owner)
		if status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/mark-terminated", owner, `{}`); status != 200 {
			t.Fatalf("terminate: %d %v", status, data)
		}
		agent := apiTestAgentToken(t, api, "joey", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],"linked_task":null,"about_task_id":"T-1"}`)
		if status != 409 {
			t.Fatalf("want 409, got %d (%v)", status, data)
		}
		apiWantError(t, data, "conflict",
			"task 'T-1' is already closed (terminated) — a card about a closed task can no longer be answered")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("about_task_id naming another task than the binding answers 400 and opens no card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		executor, steps := apiTestTwoStepTask(t, api, h, owner)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", executor,
			`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],`+
				`"linked_task":{"task_id":"T-1","step_id":"`+steps[0]+`"},"about_task_id":"T-2"}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"about_task_id must name the same task as linked_task: the binding already says which task "+
				"this ask is about. Drop about_task_id, or send linked_task=null to name a task without "+
				"holding a step.")
		dashboard.wantFrames()
		apiTestWantNoReplyCards(t, h, owner)
	})

	t.Run("a binding next to an about_task_id naming the same task holds the step and carries no hold_note", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		executor, steps := apiTestTwoStepTask(t, api, h, owner)

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", executor,
			`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],`+
				`"linked_task":{"task_id":"T-1","step_id":"`+steps[0]+`"},"about_task_id":"T-1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":              apiAnyString,
			"chat_message_id": apiAnyString,
			"created_ts":      apiAnyNumber,
			"attachments":     []any{},
			"hold_note":       "",
		})
		apiTestWantStepStatuses(t, d, "T-1", StepStatusWaitingOwner, StepStatusPending)
	})

	t.Run("a task whose other step waits on the outside world still takes a bound card, and that step holds", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		executor, steps := apiTestTwoStepTask(t, api, h, owner)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[0]+"/status", executor,
			`{"status":"waiting_external","waiting_reason":"CI"}`)
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[1]+"/status", executor, `{"status":"in_progress"}`)
		task, err := d.GetTask("T-1")
		if err != nil || task.Status != TaskStatusWaitingExternal {
			t.Fatalf("precondition: task = %#v, %v, want waiting_external", task, err)
		}

		status, data := apiJSON(t, h, "POST", "/api/reply-cards", executor,
			`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],`+
				`"linked_task":{"task_id":"T-1","step_id":"`+steps[1]+`"}}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiTestWantStepStatuses(t, d, "T-1", StepStatusWaitingExternal, StepStatusWaitingOwner)
		task, err = d.GetTask("T-1")
		if err != nil || task.Status != TaskStatusWaitingOwner {
			t.Fatalf("task = %#v, %v, want waiting_owner", task, err)
		}
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

	t.Run("a storage fault on a task-bound ask answers 500, opens no card, places no hold and announces nothing", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/tasks", owner, `{"title":"Ship it","executor_member_id":"kip"}`)
		agent := apiTestAgentToken(t, api, "kip", "")
		apiJSON(t, h, "POST", "/api/tasks/T-1/plan", agent,
			`{"steps":[{"name":"Draft","dod":"a draft exists"}]}`)
		steps, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[0].ID+"/status", agent, `{"status":"in_progress"}`)
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		wantPushed := apiTestWebPushSink(t, api)
		// Only the WRITE pool: the reads on the way in (task, step, plan) go through
		// the read pool, so the request still reaches the one transaction this case
		// is about and fails there rather than at the door.
		if err := d.wdb.Close(); err != nil {
			t.Fatalf("close write pool: %v", err)
		}
		var probe any
		probeErr := d.wdb.QueryRow("SELECT 1").Scan(&probe)
		if probeErr == nil {
			t.Fatal("closed write pool unexpectedly accepted a query")
		}
		status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
			`{"kind":"decision","summary":"ship this","options":[{"text":"yes"}],`+
				`"linked_task":{"task_id":"T-1","step_id":"`+steps[0].ID+`"}}`)
		if status != 500 {
			t.Fatalf("want 500, got %d (%v)", status, data)
		}
		// The storage error reaches the caller verbatim. That is this site's
		// shared internalError helper, unchanged by this package and identical on
		// main — T-252 replaced it with a fixed message on the PUBLIC webhook inlet
		// only. Recorded here as what the endpoint really answers, not as a shape
		// anyone signed off; a fix would make this line the one that goes red.
		apiWantError(t, data, "internal_error", "internal error: sql: database is closed")
		// 🔴 The three announcement assertions are what this case exists for. Move the
		// deltas or the push back above the commit and they are the only thing that
		// goes red — that ordering is the shape that put a card in the owner's stream
		// which the database never held, and the asker was told it had failed.
		dashboard.wantFrames()
		executor.wantFrames()
		wantPushed()
		apiTestWantNoReplyCards(t, h, owner)
		stored, err := d.ListTaskSteps("T-1")
		if err != nil {
			t.Fatalf("ListTaskSteps after the fault: %v", err)
		}
		if stored[0].Status != StepStatusInProgress {
			t.Fatalf("step status = %q, want %q", stored[0].Status, StepStatusInProgress)
		}
		if stored[0].ReplyCardID != "" {
			t.Fatalf("step reply_card_id = %q, want empty", stored[0].ReplyCardID)
		}
		task, err := d.GetTask("T-1")
		if err != nil {
			t.Fatalf("GetTask after the fault: %v", err)
		}
		if task.Status != TaskStatusInProgress {
			t.Fatalf("task status = %q, want %q", task.Status, TaskStatusInProgress)
		}
	})
}

// apiTestTwoStepTask creates T-1 executed by kip with steps [in_progress,
// pending] and returns kip's token and the step ids.
func apiTestTwoStepTask(t *testing.T, api *apiServer, h http.Handler, owner string) (string, []string) {
	t.Helper()
	if status, data := apiJSON(t, h, "POST", "/api/tasks", owner,
		`{"title":"Ship it","executor_member_id":"kip"}`); status != 200 {
		t.Fatalf("create task: %d %v", status, data)
	}
	executor := apiTestAgentToken(t, api, "kip", "")
	if status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/plan", executor,
		`{"steps":[{"name":"Draft","dod":"a draft exists"},{"name":"Review","dod":"a review is signed off"}]}`); status != 200 {
		t.Fatalf("plan: %d %v", status, data)
	}
	steps, err := api.dal.ListTaskSteps("T-1")
	if err != nil || len(steps) != 2 {
		t.Fatalf("ListTaskSteps: %d steps, %v", len(steps), err)
	}
	if status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/steps/"+steps[0].ID+"/status", executor,
		`{"status":"in_progress"}`); status != 200 {
		t.Fatalf("start step: %d %v", status, data)
	}
	return executor, []string{steps[0].ID, steps[1].ID}
}

// apiTestAboutCardFrame is the reply_card frame of a card about T-1 whose
// executor is executor, at whatever seq the test has reached.
func apiTestAboutCardFrame(cardID, from, status, trigger, executor string) map[string]any {
	frame := apiTestReplyCardFrame(0, cardID, from, status, trigger)
	frame["seq"] = apiAnyNumber
	data := frame["data"].(map[string]any)
	data["epoch"] = apiAnyNumber
	data["payload"].(map[string]any)["task_executor"] = executor
	return frame
}

func apiTestWantStepStatuses(t *testing.T, d *DAL, taskID string, want ...string) {
	t.Helper()
	steps, err := d.ListTaskSteps(taskID)
	if err != nil {
		t.Fatalf("ListTaskSteps: %v", err)
	}
	got := make([]string, len(steps))
	for i, st := range steps {
		got[i] = st.Status
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("step statuses = %v, want %v", got, want)
	}
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
			"payload": map[string]any{"id": cardID, "from": from, "status": status, "task_executor": ""},
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

	t.Run("a card about a task carries the task and its acting executor on the row and on the card", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestTwoStepTask(t, api, h, owner)
		asker := apiTestAgentToken(t, api, "joey", "")
		cardID := apiTestOpenReplyCard(t, h, asker,
			`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],"linked_task":null,"about_task_id":"T-1"}`)

		apiWantValue(t, "waiting pane", any(apiTestReplyCardPane(t, h, owner, "")), any([]any{
			map[string]any{
				"id":            cardID,
				"from":          "joey",
				"kind":          "decision",
				"summary":       "出貨路線",
				"status":        "waiting",
				"created_ts":    apiAnyNumber,
				"answered_ts":   nil,
				"expired_ts":    nil,
				"answer":        nil,
				"task":          map[string]any{"id": "T-1", "type_key": "", "title": "Ship it"},
				"task_executor": "kip",
			},
		}))
		status, card := apiJSON(t, h, "GET", "/api/reply-cards/"+cardID, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, card)
		}
		apiWantBody(t, card, map[string]any{
			"id":              cardID,
			"from":            "joey",
			"kind":            "decision",
			"summary":         "出貨路線",
			"body":            "",
			"options":         []any{map[string]any{"text": "走 A", "ai_pick": false}},
			"select_mode":     "single",
			"status":          "waiting",
			"created_ts":      apiAnyNumber,
			"attachments":     []any{},
			"answered_ts":     nil,
			"expired_ts":      nil,
			"chat_message_id": apiAnyString,
			"answer":          nil,
			"task":            map[string]any{"id": "T-1", "type_key": "", "title": "Ship it"},
			"task_executor":   "kip",
		})
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
				"id":            first,
				"from":          apiTestPlainAgentID,
				"kind":          "decision",
				"summary":       "要不要漲價",
				"status":        "waiting",
				"created_ts":    apiAnyNumber,
				"answered_ts":   nil,
				"expired_ts":    nil,
				"answer":        nil,
				"task":          nil,
				"task_executor": "",
			},
			map[string]any{
				"id":            second,
				"from":          "mira",
				"kind":          "action",
				"summary":       "請批出貨",
				"status":        "waiting",
				"created_ts":    apiAnyNumber,
				"answered_ts":   nil,
				"expired_ts":    nil,
				"answer":        nil,
				"task":          nil,
				"task_executor": "",
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
				"id":            first,
				"from":          apiTestPlainAgentID,
				"kind":          "decision",
				"summary":       "要不要漲價",
				"status":        "waiting",
				"created_ts":    apiAnyNumber,
				"answered_ts":   nil,
				"expired_ts":    nil,
				"answer":        nil,
				"task":          nil,
				"task_executor": "",
			},
		}))
	})

	t.Run("opened_by narrows the pane before the limit truncates it", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`)
		second := apiTestOpenReplyCard(t, h, admin,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":null}`)
		third := apiTestOpenReplyCard(t, h, admin,
			`{"kind":"action","summary":"請批退款","options":[{"text":"退"}],"linked_task":null}`)
		apiTestOpenReplyCard(t, h, admin,
			`{"kind":"decision","summary":"要不要加班","options":[{"text":"加"}],"linked_task":null}`)

		apiWantValue(t, "waiting pane",
			any(apiTestReplyCardPane(t, h, owner, "?opened_by=mira&limit=2")), any([]any{
				map[string]any{
					"id":            second,
					"from":          "mira",
					"kind":          "decision",
					"summary":       "要不要出貨",
					"status":        "waiting",
					"created_ts":    apiAnyNumber,
					"answered_ts":   nil,
					"expired_ts":    nil,
					"answer":        nil,
					"task":          nil,
					"task_executor": "",
				},
				map[string]any{
					"id":            third,
					"from":          "mira",
					"kind":          "action",
					"summary":       "請批退款",
					"status":        "waiting",
					"created_ts":    apiAnyNumber,
					"answered_ts":   nil,
					"expired_ts":    nil,
					"answer":        nil,
					"task":          nil,
					"task_executor": "",
				},
			}))
	})

	t.Run("an empty opened_by is not a filter and the whole pane comes back", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		admin := apiTestAgentToken(t, api, "mira", "")
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		first := apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`)
		second := apiTestOpenReplyCard(t, h, admin,
			`{"kind":"decision","summary":"要不要出貨","options":[{"text":"出"}],"linked_task":null}`)

		for _, query := range []string{"?opened_by=", "?opened_by=%20"} {
			apiWantValue(t, "waiting pane "+query,
				any(apiTestReplyCardPane(t, h, owner, query)), any([]any{
					map[string]any{
						"id":            first,
						"from":          apiTestPlainAgentID,
						"kind":          "decision",
						"summary":       "要不要漲價",
						"status":        "waiting",
						"created_ts":    apiAnyNumber,
						"answered_ts":   nil,
						"expired_ts":    nil,
						"answer":        nil,
						"task":          nil,
						"task_executor": "",
					},
					map[string]any{
						"id":            second,
						"from":          "mira",
						"kind":          "decision",
						"summary":       "要不要出貨",
						"status":        "waiting",
						"created_ts":    apiAnyNumber,
						"answered_ts":   nil,
						"expired_ts":    nil,
						"answer":        nil,
						"task":          nil,
						"task_executor": "",
					},
				}))
		}
	})

	t.Run("an opened_by nobody carries answers an empty pane rather than an error", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, apiTestPlainAgentID, "")
		apiTestOpenReplyCard(t, h, agent,
			`{"kind":"decision","summary":"要不要漲價","options":[{"text":"漲"}],"linked_task":null}`)

		apiWantValue(t, "waiting pane",
			any(apiTestReplyCardPane(t, h, owner, "?opened_by=nobody")), any([]any{}))
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
				"task":          nil,
				"task_executor": "",
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
				"task":          nil,
				"task_executor": "",
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
				"task":          nil,
				"task_executor": "",
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
				"id":            card,
				"from":          apiTestPlainAgentID,
				"kind":          "decision",
				"summary":       "要不要漲價",
				"status":        "expired",
				"created_ts":    apiAnyNumber,
				"answered_ts":   nil,
				"expired_ts":    apiAnyNumber,
				"answer":        nil,
				"task":          nil,
				"task_executor": "",
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
			"task_executor":   "",
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
			"task":          nil,
			"task_executor": "",
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
		api.applyReplyCardAnswer(rec, req, card, firstAnswerGate)
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
		api.applyReplyCardAnswer(rec, req, card, firstAnswerGate)
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
		step := dalTestStep("ts-1", task.ID)
		step.Status = "waiting_owner"
		step.ReplyCardID = "rc-1"
		card := ReplyCard{ID: "rc-1", FromMember: "kip", Kind: "decision", Status: "waiting", TaskID: task.ID, TaskStepID: step.ID}
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

		_, rel, err := api.settleReplyCard(card.ID, nowSecs(), nil, func(cur *ReplyCard, _ func(string) (*Task, error)) error {
			cur.Status = "answered"
			return nil
		})
		if err != nil {
			t.Fatalf("settleReplyCard: %v", err)
		}
		api.announceCardHoldRelease(rel, "owner")
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
		card := ReplyCard{ID: "rc-1", FromMember: "kip", Kind: "decision", Status: "waiting", TaskID: task.ID, TaskStepID: step.ID}
		if err := d.PutTask(task); err != nil {
			t.Fatalf("PutTask: %v", err)
		}
		if err := d.PutTaskStep(step); err != nil {
			t.Fatalf("PutTaskStep: %v", err)
		}
		if err := d.PutReplyCard(card); err != nil {
			t.Fatalf("PutReplyCard: %v", err)
		}
		_, rel, err := api.settleReplyCard(card.ID, nowSecs(), nil, func(cur *ReplyCard, _ func(string) (*Task, error)) error {
			cur.Status = "answered"
			return nil
		})
		if err != nil {
			t.Fatalf("settleReplyCard: %v", err)
		}
		api.announceCardHoldRelease(rel, "owner")
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

// TestAnsweringACardAnnouncesOnlyAfterTheWriteLands pins the ORDER the answer
// route keeps: plan, write, and only then fan out. A delta sent for a write
// that then rolls back tells every reader the task resumed when it did not,
// and nothing later corrects it — the card is still waiting, so no second
// answer ever re-publishes.
func TestAnsweringACardAnnouncesOnlyAfterTheWriteLands(t *testing.T) {
	seed := func(t *testing.T) (*apiServer, *DAL, Task, TaskStep) {
		t.Helper()
		api, _, d, _ := newAPITestServer(t)
		task := dalTestTask("T-1")
		task.Status = TaskStatusWaitingOwner
		task.WaitingReason = ""
		task.ClosedTS = 0
		step := dalTestStep("ts-1", task.ID)
		step.Status = StepStatusWaitingOwner
		step.ReplyCardID = "rc-1"
		card := ReplyCard{
			ID: "rc-1", FromMember: "kip", Kind: "decision",
			Status: replyCardStatusWaiting, TaskID: task.ID, TaskStepID: step.ID,
		}
		for _, w := range []func() error{
			func() error { return d.PutTask(task) },
			func() error { return d.PutTaskStep(step) },
			func() error { return d.PutReplyCard(card) },
		} {
			if err := w(); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
		return api, d, task, step
	}

	t.Run("a task write that fails fans nothing out", func(t *testing.T) {
		api, d, task, step := seed(t)
		// Fail the WRITE and only the write: reads stay intact, so the release
		// is planned in full and the route reaches the announce. Dropping the
		// table instead would fail the planning read first, and the mutant that
		// announces early would never be reached.
		if _, err := d.wdb.Exec(
			`CREATE TRIGGER refuse_task_update BEFORE UPDATE ON task
			 BEGIN SELECT RAISE(FAIL, 'the task write fails'); END`); err != nil {
			t.Fatalf("create trigger: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, task.ExecutorID)

		rec := answerCard(t, api, "rc-1", map[string]any{"text": "go"})

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("a settle that cannot be written must fail the request: %d %s",
				rec.Code, rec.Body.String())
		}
		dashboard.wantFrames()
		executor.wantFrames()
		storedCard, err := d.GetReplyCard("rc-1")
		if err != nil || storedCard == nil {
			t.Fatalf("card: %#v, %v", storedCard, err)
		}
		if storedCard.Status != replyCardStatusWaiting {
			t.Fatalf("a rolled-back settle leaves the card waiting, got %s", storedCard.Status)
		}
		stored, err := d.GetTask(task.ID)
		if err != nil || stored == nil {
			t.Fatalf("task: %#v, %v", stored, err)
		}
		if stored.Status != TaskStatusWaitingOwner || stored.UpdatedTS != task.UpdatedTS {
			t.Fatalf("a rolled-back settle leaves the task as it was, got %#v", stored)
		}
		steps, err := d.ListTaskSteps(task.ID)
		if err != nil {
			t.Fatalf("ListTaskSteps: %v", err)
		}
		if len(steps) != 1 || steps[0].Status != StepStatusWaitingOwner {
			t.Fatalf("a rolled-back settle leaves the step held, got %#v", steps)
		}
		_ = step
	})

	// The negative case above is only evidence if this path fans anything at
	// all when the write does land.
	t.Run("a settle that lands fans the task delta", func(t *testing.T) {
		api, d, task, _ := seed(t)
		dashboard := apiTestListen(t, api, "")

		rec := answerCard(t, api, "rc-1", map[string]any{"text": "go"})

		if rec.Code != http.StatusOK {
			t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
		}
		dashboard.wantFrames(
			map[string]any{
				"seq": 1, "topic": "reply_card", "op": "patch",
				"data": map[string]any{
					"entity": "reply_card", "key": "owner::rc-1", "epoch": 1, "deleted": false,
					"payload": map[string]any{"id": "rc-1", "from": "kip", "status": replyCardStatusAnswered, "task_executor": ""},
				},
				"ts": apiAnyNumber, "trigger": "owner",
			},
			map[string]any{
				"seq": 2, "topic": "task", "op": "patch",
				"data": map[string]any{
					"entity": "task", "key": "owner::T-1", "epoch": 2, "deleted": false,
					"payload": map[string]any{"id": "T-1", "priority": "high", "status": TaskStatusInProgress},
				},
				"ts": apiAnyNumber, "trigger": "owner",
			},
		)
		stored, err := d.GetTask(task.ID)
		if err != nil || stored == nil {
			t.Fatalf("task: %#v, %v", stored, err)
		}
		if stored.Status != TaskStatusInProgress {
			t.Fatalf("a landed settle resumes the task, got %s", stored.Status)
		}
	})
}

// TestAnsweringACardStoresItsInlineAttachment guards the CALLSITE, not the DAL
// function: settling the card moved the answer's blobs onto the transaction
// that also releases the step and the task, and nothing in this package ever
// sent an answer carrying inline bytes, so the callsite could stop passing them
// and every test still passed.
func TestAnsweringACardStoresItsInlineAttachment(t *testing.T) {
	api, _, d, _ := newAPITestServer(t)
	task := dalTestTask("T-1")
	task.Status = TaskStatusWaitingOwner
	task.WaitingReason = ""
	task.ClosedTS = 0
	step := dalTestStep("ts-1", task.ID)
	step.Status = StepStatusWaitingOwner
	step.ReplyCardID = "rc-1"
	card := ReplyCard{
		ID: "rc-1", FromMember: "kip", Kind: "decision",
		Status: replyCardStatusWaiting, TaskID: task.ID, TaskStepID: step.ID,
	}
	for _, w := range []func() error{
		func() error { return d.PutTask(task) },
		func() error { return d.PutTaskStep(step) },
		func() error { return d.PutReplyCard(card) },
	} {
		if err := w(); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	rec := answerCard(t, api, "rc-1", map[string]any{
		"text": "go",
		"attachments": []map[string]any{
			{"filename": "answer.txt", "mime": "text/plain", "data_b64": "aGVsbG8="},
		},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	blobs := dalStoredBlobIDs(t, d)
	if len(blobs) != 1 {
		t.Fatalf("the answer's blob must be stored: got %v", blobs)
	}
	stored, err := d.GetReplyCard("rc-1")
	if err != nil || stored == nil {
		t.Fatalf("card: %#v, %v", stored, err)
	}
	if len(stored.AnswerAttachments) != 1 {
		t.Fatalf("the card must name its answer blob: %#v", stored.AnswerAttachments)
	}
	ref, _ := stored.AnswerAttachments[0].(map[string]any)
	if ref["id"] != blobs[0] || ref["filename"] != "answer.txt" {
		t.Fatalf("the card's ref must name the stored blob: ref=%#v stored=%v", ref, blobs)
	}
	// The release still happened in the same write — the attachment rides the
	// transaction, it does not replace it.
	if got, err := d.GetTask("T-1"); err != nil || got == nil ||
		got.Status != TaskStatusInProgress {
		t.Fatalf("task after an answer with an attachment = %#v, %v", got, err)
	}
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
	t.Run("cards bound to or naming the task expire while cards for other tasks remain waiting", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		cards := []ReplyCard{
			{ID: "rc-task", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 10, TaskID: "T-1", TaskStepID: "ts-1"},
			{ID: "rc-other", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 11, TaskID: "T-2", TaskStepID: "ts-2"},
			{ID: "rc-plain", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 12},
			{ID: "rc-about", FromMember: "joey", Kind: "decision", Status: "waiting", CreatedTS: 13, TaskID: "T-1"},
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
		if count != 2 {
			t.Fatalf("expired count = %d, want 2", count)
		}
		got, err := d.ListReplyCards()
		if err != nil {
			t.Fatalf("ListReplyCards: %v", err)
		}
		if got[0].Status != "expired" || got[0].ExpiredTS != 42 || got[1].Status != "waiting" || got[2].Status != "waiting" ||
			got[3].Status != "expired" || got[3].ExpiredTS != 42 {
			t.Fatalf("cards after task sweep = %#v", got)
		}
		dashboard.wantFrames(
			apiTestReplyCardFrame(1, "rc-task", "mira", "expired", "reassign"),
			apiTestReplyCardFrame(2, "rc-about", "joey", "expired", "reassign"))
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

		count, err := api.expireWaitingCardsByAuthor("mira", 42, "dismiss")
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
		_, err := api.expireWaitingCardsByAuthor("", 42, "dismiss")
		if err == nil || err.Error() != "expireWaitingCardsFromMember: blank member id" {
			t.Fatalf("empty member id error = %v", err)
		}
	})
}

func TestReconcileOrphanReplyCardsOnBoot(t *testing.T) {
	t.Run("waiting cards bound to or naming closed or missing tasks expire while live and unbound cards remain", func(t *testing.T) {
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
			{ID: "rc-about-closed", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 14, TaskID: "T-closed"},
			{ID: "rc-about-live", FromMember: "mira", Kind: "decision", Status: "waiting", CreatedTS: 15, TaskID: "T-live"},
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
		if count != 3 {
			t.Fatalf("reconciled count = %d, want 3", count)
		}
		got, err := d.ListReplyCards()
		if err != nil {
			t.Fatalf("ListReplyCards: %v", err)
		}
		if got[0].Status != "expired" || got[0].ExpiredTS <= 0 || got[1].Status != "expired" || got[1].ExpiredTS <= 0 ||
			got[2].Status != "waiting" || got[3].Status != "waiting" ||
			got[4].Status != "expired" || got[4].ExpiredTS <= 0 || got[5].Status != "waiting" {
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
			apiTestReplyCardFrame(3, "rc-about-closed", "mira", "expired", "boot-reconcile"),
		)
		initiator.wantFrames(
			apiTestReplyCardFrame(1, "rc-closed", "mira", "expired", "boot-reconcile"),
			apiTestReplyCardFrame(2, "rc-missing", "mira", "expired", "boot-reconcile"),
			apiTestReplyCardFrame(3, "rc-about-closed", "mira", "expired", "boot-reconcile"),
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

	t.Run("answering a card that only names a task settles it without touching the task or its steps", func(t *testing.T) {
		api, h, d, owner := newAPITestServer(t)
		apiTestTwoStepTask(t, api, h, owner)
		asker := apiTestAgentToken(t, api, "joey", "")
		cardID := apiTestOpenReplyCard(t, h, asker,
			`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],"linked_task":null,"about_task_id":"T-1"}`)
		before, err := d.GetTask("T-1")
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		dashboard := apiTestListen(t, api, "")
		executor := apiTestListen(t, api, "kip")
		author := apiTestListen(t, api, "joey")
		bystander := apiTestListen(t, api, "mira")

		status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner, `{"option_idxs":[0]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":          cardID,
			"status":      "answered",
			"answered_ts": apiAnyNumber,
			"expired_ts":  nil,
			"answer":      map[string]any{"option_idxs": []any{0}, "text": "", "attachments": []any{}},
			"task_id":     "",
			"step_id":     "",
		})
		after, err := d.GetTask("T-1")
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if after.Status != before.Status || after.UpdatedTS != before.UpdatedTS {
			t.Fatalf("task moved: status %q→%q updated_ts %v→%v",
				before.Status, after.Status, before.UpdatedTS, after.UpdatedTS)
		}
		apiTestWantStepStatuses(t, d, "T-1", StepStatusInProgress, StepStatusPending)
		frame := apiTestAboutCardFrame(cardID, "joey", "answered", "owner", "kip")
		dashboard.wantFrames(frame)
		executor.wantFrames(frame)
		author.wantFrames(frame)
		bystander.wantFrames()
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

	t.Run("re-deciding a card about a task fans the delta to the task's executor as well as the author", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestTwoStepTask(t, api, h, owner)
		asker := apiTestAgentToken(t, api, "joey", "")
		cardID := apiTestOpenReplyCard(t, h, asker,
			`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"},{"text":"走 B"}],"linked_task":null,"about_task_id":"T-1"}`)
		apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/answer", owner, `{"option_idxs":[0]}`)
		executor := apiTestListen(t, api, "kip")
		author := apiTestListen(t, api, "joey")
		bystander := apiTestListen(t, api, "mira")

		if status, data := apiJSON(t, h, "PUT", "/api/reply-cards/"+cardID+"/answer", owner, `{"option_idxs":[1]}`); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		frame := apiTestAboutCardFrame(cardID, "joey", "answered", "owner", "kip")
		executor.wantFrames(frame)
		author.wantFrames(frame)
		bystander.wantFrames()
	})

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

	t.Run("the author retiring a card about a task fans the delta to the task's executor as well", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiTestTwoStepTask(t, api, h, owner)
		asker := apiTestAgentToken(t, api, "joey", "")
		cardID := apiTestOpenReplyCard(t, h, asker,
			`{"kind":"decision","summary":"出貨路線","options":[{"text":"走 A"}],"linked_task":null,"about_task_id":"T-1"}`)
		executor := apiTestListen(t, api, "kip")
		author := apiTestListen(t, api, "joey")
		bystander := apiTestListen(t, api, "mira")

		if status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+cardID+"/expire", asker, `{}`); status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		frame := apiTestAboutCardFrame(cardID, "joey", "expired", "joey", "kip")
		executor.wantFrames(frame)
		author.wantFrames(frame)
		bystander.wantFrames()
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
