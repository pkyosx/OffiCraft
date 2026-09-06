package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The reply-card WRITE endpoints answer a RECEIPT — never the whole card.
//
// 🔴 THIS FILE USED TO PIN THE OPPOSITE, AND ITS REASON IS REWRITTEN HERE
// RATHER THAN DELETED, because the reason was real and still needs an answer.
// It ran: the cockpit's action path reconciles from the write's OWN response
// instead of waiting for a `reply_card` SSE frame (frontend's `adoptWrite`),
// because a lost or absent frame left an answered card rendering as waiting,
// the owner clicked it again and hit a 409. So a write that answered with a
// NARROWER projection would let the cockpit swap a fully-rendered card for a
// partial one — and the client's own tests could not tell, since they run
// against api/mock, which returns whole cards by construction.
//
// AND THE COCKPIT NO LONGER SWAPS THE CARD — IT MERGES INTO IT. That is the
// new premise this file rests on, and it is a FRONTEND fact, so it is named
// here rather than assumed: `adoptWrite` (frontend/src/hooks/useReplyCards.tsx)
// used to store the write's answer AS the card, which is why a narrower
// projection would have blanked the question, its options, its attachments and
// its task ref the day the receipt landed. It now folds only the transition
// into the card that pane had ALREADY read (`mergeReplyCardWrite`), keeping
// everything the receipt does not speak about. So the original need — the pane
// converges from the write instead of waiting for a `reply_card` frame that may
// never arrive, which is what once left an answered card showing 待回覆 until
// the next click hit a 409 — is met by a receipt that reports the transition
// and nothing else. If that merge ever goes back to being a replace, the
// receipt's narrowness becomes a data loss again; the two are one contract.
//
// WHAT T-91 CHANGED IS THE ANSWER TO THAT NEED, NOT THE NEED. Owner ruled on
// 2026-09-05 that a write must not hand back the content it was just given
// (「自己發送出去的內容 … 不應該再回傳回來」), and the card's summary, body and
// options are exactly that. What `adoptWrite` actually has to reconcile is the
// TRANSITION — did the card settle, when, into what, and what did that release
// — and the receipt carries every bit of it: `status` (all three verbs can
// DECLINE to move a card, so having called expire is not evidence it expired),
// the mutually exclusive `answered_ts`/`expired_ts` pair, the normalised
// `answer` as STORED, and `task_id` + `step_id` for the step this write
// released from waiting_owner.
//
// 🔴 THE task_id/step_id PAIR IS A REPAIR, NOT A REDUCTION. The old whole-card
// echo carried a taskRefDTO (id, type_key, title) — it named the TASK but not
// the STEP, so it could not say what the write had actually released, since
// releaseCardHold acts on the card's one stored task_step_id. Owner caught this
// on rc-bf25374aa0e8, asking why answering a card returns a task title.
//
// SO THE ASSERTIONS BELOW INVERTED. Each of the three verbs is pinned on two
// halves, and both are needed:
//   • THE RECEIPT SAYS WHAT THE WRITE DID — the settled status, the stamp that
//     belongs to it, the stored answer, and the released task/step. Without
//     this half a receipt that answered {} would pass.
//   • THE RECEIPT IS NOT THE CARD — no body, no options, no summary. Without
//     this half a regression that quietly restored the whole-card echo would go
//     green, and the whole point of the reshape is that it must not.
// The corpus is still a card bound to a LIVE task and its current step, because
// that is the only fixture in which the task/step release is expressible at all.
//
// The whole card remains available, unchanged, through get_reply_card — which
// is what these tests read when they need it, and what the cockpit's per-card
// refetch has always used.

// getReplyCardRaw fetches the card through the single-card endpoint — the shape
// the cockpit's own per-card refetch (ChatReplyCard) and its full list rows are
// both built from.
func getReplyCardRaw(t *testing.T, api *apiServer, cardID string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetReplyCardApiReplyCardsCardIdGet(rec,
		taskReq(t, "GET", "/api/reply-cards/"+cardID, nil, "owner", "owner"), cardID)
	return rec
}

// boundWaitingCard opens a card that is bound to a live task and its current
// step — the shape whose DTO exercises the task-reference join.
func boundWaitingCard(t *testing.T, api *apiServer) replyCardDTO {
	t.Helper()
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "work", "dod": "d"},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	// A real BODY and a real option list on purpose: those are exactly the two
	// things the T-3f31 light row drops, so without them a light projection and a
	// whole card would be indistinguishable and the identity compare below would
	// pin nothing (openPlainCard's fixture leaves body empty).
	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind":        "decision",
		"summary":     "which way?",
		"body":        "the long ask body that the light row does not carry",
		"options":     []map[string]any{{"text": "AI 建議:照做"}, {"text": "先等等"}},
		"linked_task": map[string]any{"task_id": task.ID, "step_id": view.Steps[0].ID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create card: %d %s", rec.Code, rec.Body.String())
	}
	card := createdCardView(t, api, rec)
	if card.Task == nil {
		t.Fatalf("corpus is not exercising the task join: %+v", card)
	}
	if card.Body == "" || len(card.Options) == 0 {
		t.Fatalf("corpus cannot tell a receipt from a whole card: %+v", card)
	}
	return card
}

// assertReceiptIsNotTheWholeCard is the second half of every case below: the
// write's answer must NOT be the object get_reply_card serves.
//
// It reads the raw JSON keys rather than a decoded struct on purpose — decoding
// a whole card into replyCardReceiptDTO would silently drop the extra fields
// and this check would pass over a full echo, which is the exact regression it
// exists to catch. The three names it forbids are the three the corpus above
// proves are non-empty on this card.
func assertReceiptIsNotTheWholeCard(t *testing.T, echo *httptest.ResponseRecorder) {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(echo.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode receipt: %v (%s)", err, echo.Body.String())
	}
	for _, forbidden := range []string{"body", "options", "summary", "kind", "select_mode"} {
		if _, present := raw[forbidden]; present {
			t.Fatalf("the write receipt must not carry %q — that is the content the "+
				"caller sent, and get_reply_card serves it: %s", forbidden, echo.Body.String())
		}
	}
	// Anti-vacuity: the keys that MUST be there. Without this a handler that
	// answered `{}` would satisfy every "must not carry" above.
	for _, required := range []string{"id", "status", "answered_ts", "expired_ts", "answer"} {
		if _, present := raw[required]; !present {
			t.Fatalf("the write receipt must carry %q (null is fine, absent is not): %s",
				required, echo.Body.String())
		}
	}
}

// assertTransitionReceipt is the contract: the write answers a receipt that
// AGREES WITH THE STORED CARD about everything it reports, and is not the card.
//
// The agreement half is read against a fresh get_reply_card rather than against
// the values this test posted — a receipt assembled from the REQUEST instead of
// from what landed would satisfy a self-comparison and is precisely the drift
// worth catching.
func assertTransitionReceipt(t *testing.T, api *apiServer, cardID string, echo *httptest.ResponseRecorder) replyCardReceiptDTO {
	t.Helper()
	if echo.Code != http.StatusOK {
		t.Fatalf("write: %d %s", echo.Code, echo.Body.String())
	}
	assertReceiptIsNotTheWholeCard(t, echo)
	fresh := getReplyCardRaw(t, api, cardID)
	if fresh.Code != http.StatusOK {
		t.Fatalf("get_reply_card: %d %s", fresh.Code, fresh.Body.String())
	}
	card := decodeBody[replyCardDTO](t, fresh)
	got := decodeBody[replyCardReceiptDTO](t, echo)
	if got.ID != cardID {
		t.Fatalf("receipt id = %q, want %q", got.ID, cardID)
	}
	if got.Status != card.Status {
		t.Fatalf("receipt status = %q, stored card says %q", got.Status, card.Status)
	}
	// The stamps are a MUTUALLY EXCLUSIVE pair — exactly one of them is set on
	// any settled card, and a receipt that filled both would be describing a
	// state no write produces.
	if (got.AnsweredTS != nil) == (got.ExpiredTS != nil) {
		t.Fatalf("answered_ts/expired_ts must be exclusive, got %v / %v",
			got.AnsweredTS, got.ExpiredTS)
	}
	// The release the write performed, per STEP and not merely per task — the
	// half the retired taskRefDTO could not express.
	if card.Task != nil {
		if got.TaskID != card.Task.ID {
			t.Fatalf("receipt task_id = %q, want %q", got.TaskID, card.Task.ID)
		}
		if got.StepID == "" {
			t.Fatalf("a bound card's receipt must name the STEP it released: %+v", got)
		}
		stored, err := api.dal.GetReplyCard(cardID)
		if err != nil || stored == nil {
			t.Fatalf("stored card: %v %v", stored, err)
		}
		if got.StepID != stored.TaskStepID {
			t.Fatalf("receipt step_id = %q, stored card binds %q", got.StepID, stored.TaskStepID)
		}
	}
	return got
}

func TestAnswerAnswersATransitionReceiptNotTheCard(t *testing.T) {
	api := newTasksTestServer(t)
	card := boundWaitingCard(t, api)

	rec := answerCard(t, api, card.ID, map[string]any{
		"option_idxs": []int{0},
		"text":        "就這樣辦",
	})
	got := assertTransitionReceipt(t, api, card.ID, rec)

	// The status the cockpit adopts must be the settled one — this is what makes
	// the card leave the waiting pane.
	if got.Status != replyCardStatusAnswered {
		t.Fatalf("answer receipt status: got %q want answered", got.Status)
	}
	if got.AnsweredTS == nil || *got.AnsweredTS <= 0 {
		t.Fatalf("an answer must carry the server stamp: %+v", got.AnsweredTS)
	}
	// The answer as STORED, not as sent: the server normalises the option
	// indices (deduped, ascending), so this is what the write PRODUCED.
	if got.Answer == nil || got.Answer.Text != "就這樣辦" {
		t.Fatalf("answer receipt must carry the stored answer: %+v", got.Answer)
	}
}

func TestReanswerAnswersATransitionReceiptNotTheCard(t *testing.T) {
	api := newTasksTestServer(t)
	card := boundWaitingCard(t, api)
	if rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": []int{0}}); rec.Code != http.StatusOK {
		t.Fatalf("seed answer: %d %s", rec.Code, rec.Body.String())
	}

	put := httptest.NewRecorder()
	api.HandleReanswerReplyCardApiReplyCardsCardIdAnswerPut(put,
		taskReq(t, "PUT", "/x", map[string]any{
			"option_idxs": []int{1},
			"text":        "改主意了",
		}, "owner", "owner"), card.ID)
	got := assertTransitionReceipt(t, api, card.ID, put)

	// The revision itself has to be IN the receipt — that is the value the
	// cockpit renders in place of the previous answer, and the one thing on
	// this write that is genuinely news rather than an echo.
	if got.Answer == nil || got.Answer.Text != "改主意了" {
		t.Fatalf("re-answer receipt must carry the new answer: %+v", got.Answer)
	}
	if got.ExpiredTS != nil {
		t.Fatalf("a re-answer must not stamp expired_ts: %+v", got.ExpiredTS)
	}
}

func TestExpireAnswersATransitionReceiptNotTheCard(t *testing.T) {
	api := newTasksTestServer(t)
	card := boundWaitingCard(t, api)

	rec := expireCardReq(t, api, card.ID, "owner", "owner")
	got := assertTransitionReceipt(t, api, card.ID, rec)

	if got.Status != replyCardStatusExpired {
		t.Fatalf("expire receipt status: got %q want expired", got.Status)
	}
	if got.ExpiredTS == nil || *got.ExpiredTS <= 0 {
		t.Fatalf("expire receipt must carry the terminal stamp: %+v", got.ExpiredTS)
	}
	// An expire produces no answer, and the field is null rather than absent so
	// a reader never has to tell "no answer" from "this build does not say".
	if got.Answer != nil {
		t.Fatalf("an expired card has no answer: %+v", got.Answer)
	}
}
