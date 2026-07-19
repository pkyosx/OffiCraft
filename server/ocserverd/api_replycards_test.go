package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func waitingCard(id string, created float64) ReplyCard {
	return ReplyCard{
		ID: id, FromMember: "m-a", Kind: replyCardKindDecision,
		Summary: "s", Options: []string{"A", "B"},
		Status: replyCardStatusWaiting, CreatedTS: created,
	}
}

func answeredCard(id string, created, answered float64) ReplyCard {
	c := waitingCard(id, created)
	c.Status = replyCardStatusAnswered
	c.AnsweredTS = answered
	return c
}

func expiredCard(id string, created, expired float64) ReplyCard {
	c := waitingCard(id, created)
	c.Status = replyCardStatusExpired
	c.ExpiredTS = expired
	return c
}

func TestWaitingReplyCardsSortsLongestWaitingFirstAndDropsAnswered(t *testing.T) {
	cards := []ReplyCard{
		answeredCard("rc-done", 1, 5),
		waitingCard("rc-newer", 30),
		waitingCard("rc-older", 10),
	}
	got := waitingReplyCards(cards)
	if len(got) != 2 || got[0].ID != "rc-older" || got[1].ID != "rc-newer" {
		t.Fatalf("expected [rc-older rc-newer], got %+v", got)
	}
}

func TestRecentAnsweredReplyCardsAppliesThe24hWindowNewestFirst(t *testing.T) {
	now := 200000.0
	cards := []ReplyCard{
		waitingCard("rc-waiting", 1),
		answeredCard("rc-in-early", 1, now-replyCardAnsweredWindowSecs), // boundary: kept
		answeredCard("rc-in-late", 1, now-10),
		answeredCard("rc-expired", 1, now-replyCardAnsweredWindowSecs-1),
	}
	got := recentAnsweredReplyCards(cards, now)
	if len(got) != 2 || got[0].ID != "rc-in-late" || got[1].ID != "rc-in-early" {
		t.Fatalf("expected [rc-in-late rc-in-early], got %+v", got)
	}
}

func TestRecentExpiredReplyCardsAppliesThe24hWindowNewestFirst(t *testing.T) {
	now := 200000.0
	cards := []ReplyCard{
		waitingCard("rc-waiting", 1),
		answeredCard("rc-answered", 1, now-10),
		expiredCard("rc-in-early", 1, now-replyCardAnsweredWindowSecs), // boundary: kept
		expiredCard("rc-in-late", 1, now-10),
		expiredCard("rc-aged", 1, now-replyCardAnsweredWindowSecs-1),
	}
	got := recentExpiredReplyCards(cards, now)
	if len(got) != 2 || got[0].ID != "rc-in-late" || got[1].ID != "rc-in-early" {
		t.Fatalf("expected [rc-in-late rc-in-early], got %+v", got)
	}
}

func TestValidateReplyCardOptionsEnforcesOneToFourNonBlank(t *testing.T) {
	cases := []struct {
		name    string
		options []string
		wantOK  bool
	}{
		{"empty", nil, false},
		{"one", []string{"A"}, true},
		{"four", []string{"A", "B", "C", "D"}, true},
		{"five", []string{"A", "B", "C", "D", "E"}, false},
		{"blank member", []string{"A", "  "}, false},
	}
	for _, tc := range cases {
		got, problem := validateReplyCardOptions(tc.options)
		if (problem == "") != tc.wantOK {
			t.Fatalf("%s: wantOK=%v got problem=%q", tc.name, tc.wantOK, problem)
		}
		if tc.wantOK && len(got) != len(tc.options) {
			t.Fatalf("%s: trimmed options lost entries: %v", tc.name, got)
		}
	}
	trimmed, problem := validateReplyCardOptions([]string{" A ", "B"})
	if problem != "" || trimmed[0] != "A" {
		t.Fatalf("options must be trimmed: %v %q", trimmed, problem)
	}
}

// ── read-time reply_card_status join (lazy-load wire field) ──────────────────

func TestServedChatMessageDTOJoinsLiveReplyCardStatus(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	card := waitingCard("rc-msg", 10)
	card.ChatMessageID = "c-1"
	if err := s.dal.PutReplyCard(card); err != nil {
		t.Fatalf("put card: %v", err)
	}
	msg := ChatMessage{
		ID: "c-1", Sender: "m-a", Recipient: wireOwnerID, Body: "ask?", TS: 10,
		Meta: map[string]any{"reply_card_id": "rc-msg"},
	}
	if err := s.dal.PutChat(msg); err != nil {
		t.Fatalf("put chat: %v", err)
	}

	// A card-bearing message reflects the card's LIVE status.
	if got := s.servedChatMessageDTO(msg).ReplyCardStatus; got != replyCardStatusWaiting {
		t.Fatalf("waiting join: got %q want waiting", got)
	}
	// Answering the card flips the read-time join (it is NOT stored on the msg).
	card.Status = replyCardStatusAnswered
	card.AnsweredTS = 20
	if err := s.dal.PutReplyCard(card); err != nil {
		t.Fatalf("answer card: %v", err)
	}
	if got := s.servedChatMessageDTO(msg).ReplyCardStatus; got != replyCardStatusAnswered {
		t.Fatalf("answered flip: got %q want answered", got)
	}
	// A plain message (no reply_card_id) has an empty status.
	plain := ChatMessage{ID: "c-2", Sender: "m-a", Recipient: wireOwnerID, Body: "hi", TS: 11}
	if got := s.servedChatMessageDTO(plain).ReplyCardStatus; got != "" {
		t.Fatalf("plain message must carry empty reply_card_status, got %q", got)
	}
}

func TestReplyCardCountReturnsWaitingAndRecentAnswered(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	now := nowSecs()
	cards := []ReplyCard{
		waitingCard("rc-w1", now-100),
		waitingCard("rc-w2", now-50),
		answeredCard("rc-a-recent", now-1000, now-60),
		answeredCard("rc-a-expired", now-100000, now-replyCardAnsweredWindowSecs-100),
		expiredCard("rc-x-recent", now-1000, now-30),
		expiredCard("rc-x-aged", now-100000, now-replyCardAnsweredWindowSecs-100),
	}
	for _, c := range cards {
		if err := s.dal.PutReplyCard(c); err != nil {
			t.Fatalf("put %s: %v", c.ID, err)
		}
	}
	rec := httptest.NewRecorder()
	s.HandleReplyCardCountApiReplyCardsCountGet(rec,
		httptest.NewRequest("GET", "/api/reply-cards/count", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("count: %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[replyCardCountDTO](t, rec)
	if got.Waiting != 2 {
		t.Fatalf("waiting: got %d want 2", got.Waiting)
	}
	if got.Answered != 1 {
		t.Fatalf("answered (24h window): got %d want 1", got.Answered)
	}
	if got.Expired != 1 {
		t.Fatalf("expired (24h window): got %d want 1", got.Expired)
	}
}

func TestTaskStepReplyCardStatusJoinsBoundCards(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	if err := s.dal.PutReplyCard(waitingCard("rc-wait", 1)); err != nil {
		t.Fatalf("put waiting: %v", err)
	}
	if err := s.dal.PutReplyCard(answeredCard("rc-ans", 1, 2)); err != nil {
		t.Fatalf("put answered: %v", err)
	}
	steps := []TaskStep{
		{ID: "st-1", TaskID: "t-1", ReplyCardID: "rc-wait", Status: StepStatusWaitingOwner, OrderIdx: 0},
		{ID: "st-2", TaskID: "t-1", ReplyCardID: "rc-ans", Status: StepStatusInProgress, OrderIdx: 1},
		{ID: "st-3", TaskID: "t-1", ReplyCardID: "", Status: StepStatusPending, OrderIdx: 2},
	}
	statuses := s.replyCardStatusesForSteps(steps)
	if got := newTaskStepDTO(steps[0], statuses).ReplyCardStatus; got != replyCardStatusWaiting {
		t.Fatalf("st-1 (waiting card): got %q", got)
	}
	if got := newTaskStepDTO(steps[1], statuses).ReplyCardStatus; got != replyCardStatusAnswered {
		t.Fatalf("st-2 (answered card): got %q", got)
	}
	if got := newTaskStepDTO(steps[2], statuses).ReplyCardStatus; got != "" {
		t.Fatalf("st-3 (no card): got %q", got)
	}
}

func TestNewReplyCardDTONullsAnswerWhileWaiting(t *testing.T) {
	dto := newReplyCardDTO(waitingCard("rc-1", 5))
	if dto.AnsweredTS != nil || dto.Answer != nil || dto.ExpiredTS != nil {
		t.Fatalf("waiting card must serialise answered_ts/answer/expired_ts null: %+v", dto)
	}
	dto = newReplyCardDTO(expiredCard("rc-x", 5, 9))
	if dto.ExpiredTS == nil || *dto.ExpiredTS != 9 {
		t.Fatalf("expired_ts not projected: %+v", dto)
	}
	if dto.AnsweredTS != nil || dto.Answer != nil {
		t.Fatalf("an expired card carries no answer projection: %+v", dto)
	}
	idx := 1
	c := answeredCard("rc-2", 5, 9)
	c.AnswerOptionIdx = &idx
	c.AnswerText = "ok"
	c.AnswerAttachments = []any{
		map[string]any{"id": "att-1", "mime": "image/png", "filename": "a.png"},
	}
	dto = newReplyCardDTO(c)
	if dto.AnsweredTS == nil || *dto.AnsweredTS != 9 {
		t.Fatalf("answered_ts not projected: %+v", dto)
	}
	if dto.Answer == nil || *dto.Answer.OptionIdx != 1 || dto.Answer.Text != "ok" {
		t.Fatalf("answer not projected: %+v", dto.Answer)
	}
	if len(dto.Answer.Attachments) != 1 ||
		dto.Answer.Attachments[0].URL != "/api/chat/attachment/att-1" {
		t.Fatalf("attachment refs not projected: %+v", dto.Answer.Attachments)
	}
}

func TestReplyCardDALRoundTrip(t *testing.T) {
	dal := newTestDAL(t)
	idx := 0
	card := ReplyCard{
		ID: "rc-round", FromMember: "m-a", Kind: replyCardKindAction,
		Summary: "do the thing", Body: "details",
		Options: []string{"done, continue"},
		Status:  replyCardStatusAnswered, CreatedTS: 1.5, AnsweredTS: 2.5,
		ChatMessageID: "c-1", AnswerOptionIdx: &idx, AnswerText: "done",
		AnswerAttachments: []any{
			map[string]any{"id": "att-1", "mime": "image/png", "filename": "a.png"},
		},
	}
	if err := dal.PutReplyCard(card); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := dal.GetReplyCard("rc-round")
	if err != nil || got == nil {
		t.Fatalf("get: %v %v", got, err)
	}
	if got.Kind != card.Kind || got.Summary != card.Summary ||
		got.Status != card.Status || got.ChatMessageID != "c-1" ||
		got.AnsweredTS != 2.5 || got.AnswerText != "done" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if len(got.Options) != 1 || got.Options[0] != "done, continue" {
		t.Fatalf("options JSON round trip: %+v", got.Options)
	}
	if got.AnswerOptionIdx == nil || *got.AnswerOptionIdx != 0 {
		t.Fatalf("answer_option_idx must round-trip 0 (not fold to null): %+v",
			got.AnswerOptionIdx)
	}
	if len(got.AnswerAttachments) != 1 {
		t.Fatalf("answer_attachments JSON round trip: %+v", got.AnswerAttachments)
	}
	missing, err := dal.GetReplyCard("rc-absent")
	if err != nil || missing != nil {
		t.Fatalf("absent card must read nil,nil: %v %v", missing, err)
	}
}

// ── auto card→step binding (owner design 2026-07-14) ─────────────────────────
// A plain create_reply_card by an agent executing exactly one active task
// binds the card to that task's CURRENT step and drives the same waiting
// state machine as open_gate; anything ambiguous degrades honestly.

// openPlainCard posts one plain POST /api/reply-cards as the given actor.
func openPlainCard(t *testing.T, api *apiServer, actor string) replyCardDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateReplyCardApiReplyCardsPost(rec,
		taskReq(t, "POST", "/api/reply-cards", map[string]any{
			"kind": "decision", "summary": "which way?",
			"options": []string{"A", "B"},
		}, actor, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create card: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[replyCardDTO](t, rec)
}

func TestPlainCardAutoBindsTheCurrentStepAndEntersWaiting(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "recon", "dod": "understood"},
		{"name": "build", "dod": "built"},
	})
	// Step 2 is the CURRENT one (the single in_progress step).
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"status": "in_progress"},
			"m-exec", "agent"),
		task.ID, view.Steps[1].ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("step start: %d %s", rec.Code, rec.Body.String())
	}

	card := openPlainCard(t, api, "m-exec")
	if card.Task == nil || card.Task.ID != task.ID {
		t.Fatalf("auto-bound card must carry the task ref: %+v", card.Task)
	}
	stored, err := api.dal.GetReplyCard(card.ID)
	if err != nil || stored == nil {
		t.Fatalf("stored card: %v %v", stored, err)
	}
	if stored.TaskID != task.ID || stored.TaskStepID != view.Steps[1].ID {
		t.Fatalf("card must bind the current step: %+v", stored)
	}
	step, err := api.dal.GetTaskStep(view.Steps[1].ID)
	if err != nil || step == nil {
		t.Fatalf("step: %v %v", step, err)
	}
	if step.Status != StepStatusWaitingOwner || step.ReplyCardID != card.ID {
		t.Fatalf("bound step must be waiting_owner + point at the card: %+v", step)
	}
	if step.StartedTS <= 0 {
		t.Fatalf("arming must stamp started_ts: %+v", step)
	}
	got, err := api.dal.GetTask(task.ID)
	if err != nil || got == nil {
		t.Fatalf("task: %v %v", got, err)
	}
	if got.Status != TaskStatusWaitingOwner {
		t.Fatalf("task must follow into waiting_owner, got %s", got.Status)
	}

	// The untouched sibling step never moves.
	other, _ := api.dal.GetTaskStep(view.Steps[0].ID)
	if other.Status != StepStatusPending || other.ReplyCardID != "" {
		t.Fatalf("sibling step must stay untouched: %+v", other)
	}

	// A FOLLOW-UP ask while the step already waits (the answer did not settle
	// it) re-binds the step to the NEW card — the step keeps waiting.
	second := openPlainCard(t, api, "m-exec")
	step, _ = api.dal.GetTaskStep(view.Steps[1].ID)
	if step.Status != StepStatusWaitingOwner || step.ReplyCardID != second.ID {
		t.Fatalf("follow-up ask must re-point the step at the new card: %+v", step)
	}
}

func TestPlainCardBindsTaskOnlyWhenTheStepIsAmbiguous(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "lane-a", "dod": "a done", "parallel_group": "pg"},
		{"name": "lane-b", "dod": "b done", "parallel_group": "pg"},
	})
	// TWO steps in_progress → the current step is ambiguous.
	for _, st := range view.Steps {
		rec := httptest.NewRecorder()
		api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
			taskReq(t, "POST", "/x", map[string]any{"status": "in_progress"},
				"m-exec", "agent"),
			task.ID, st.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("step start: %d %s", rec.Code, rec.Body.String())
		}
	}
	card := openPlainCard(t, api, "m-exec")
	stored, _ := api.dal.GetReplyCard(card.ID)
	if stored.TaskID != task.ID || stored.TaskStepID != "" {
		t.Fatalf("ambiguous step must degrade to task-only binding: %+v", stored)
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusInProgress {
		t.Fatalf("task-only binding must not move the task, got %s", got.Status)
	}
	for _, st := range view.Steps {
		s2, _ := api.dal.GetTaskStep(st.ID)
		if s2.Status != StepStatusInProgress || s2.ReplyCardID != "" {
			t.Fatalf("no step may arm on an ambiguous bind: %+v", s2)
		}
	}
}

// TestPlainCardOnAGroupedStepFlipsTheWholeTask pins the T-9ca5 carve-out
// removal: arming a card on a parallel-lane step now DERIVES the WHOLE task to
// waiting_owner (owner ruling: any step 等我回覆 → task 等我回覆). The old lane-only
// hold is gone.
func TestPlainCardOnAGroupedStepFlipsTheWholeTask(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "lane-a", "dod": "a done", "parallel_group": "pg"},
		{"name": "lane-b", "dod": "b done", "parallel_group": "pg"},
	})
	// Only lane-a runs → it is the unambiguous current step.
	if rec := reportStepStatus(t, api, task.ID, view.Steps[0].ID, "m-exec",
		"in_progress", ""); rec.Code != http.StatusOK {
		t.Fatalf("step start: %d %s", rec.Code, rec.Body.String())
	}
	card := openPlainCard(t, api, "m-exec")
	step, _ := api.dal.GetTaskStep(view.Steps[0].ID)
	if step.Status != StepStatusWaitingOwner || step.ReplyCardID != card.ID {
		t.Fatalf("grouped lane must arm: %+v", step)
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusWaitingOwner {
		t.Fatalf("arming a grouped lane now flips the whole task to waiting_owner, got %s",
			got.Status)
	}
}

func TestPlainCardStaysUnboundWithoutOneClearActiveTask(t *testing.T) {
	api := newTasksTestServer(t)

	// No task at all → plain chat 請示 (the M2 behaviour, unchanged).
	card := openPlainCard(t, api, "m-free")
	stored, _ := api.dal.GetReplyCard(card.ID)
	if stored.TaskID != "" || stored.TaskStepID != "" {
		t.Fatalf("task-less asker must open an unbound card: %+v", stored)
	}

	// TWO active tasks → ambiguous, unbound.
	t1 := createAdHocTask(t, api, "m-busy")
	t2 := createAdHocTask(t, api, "m-busy")
	for _, id := range []string{t1.ID, t2.ID} {
		submitPlan(t, api, id, "m-busy", []map[string]any{{"name": "work", "dod": "d"}})
		startFirstStep(t, api, id, "m-busy")
	}
	card = openPlainCard(t, api, "m-busy")
	stored, _ = api.dal.GetReplyCard(card.ID)
	if stored.TaskID != "" || stored.TaskStepID != "" {
		t.Fatalf("two active tasks must degrade to an unbound card: %+v", stored)
	}

	// A NOT-STARTED task is not "active" — still unbound.
	createAdHocTask(t, api, "m-idle")
	card = openPlainCard(t, api, "m-idle")
	stored, _ = api.dal.GetReplyCard(card.ID)
	if stored.TaskID != "" {
		t.Fatalf("a not_started task must not capture plain asks: %+v", stored)
	}
}

func TestPlainCardWithOneActiveTaskButNoRunningStepBindsTaskOnly(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	// An early done step + a later pending one: the task DERIVES to in_progress
	// (active) yet has no single running "current" step, so a plain ask degrades
	// to task-only binding.
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "early", "dod": "d1"}, {"name": "later", "dod": "d2"},
	})
	for _, status := range []string{"in_progress", "done"} {
		if rec := reportStepStatus(t, api, task.ID, view.Steps[0].ID, "m-exec",
			status, ""); rec.Code != http.StatusOK {
			t.Fatalf("drive early %s: %d %s", status, rec.Code, rec.Body.String())
		}
	}
	card := openPlainCard(t, api, "m-exec")
	stored, _ := api.dal.GetReplyCard(card.ID)
	if stored.TaskID != task.ID || stored.TaskStepID != "" {
		t.Fatalf("no running step → task-only binding: %+v", stored)
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusInProgress {
		t.Fatalf("task-only binding must not move the task, got %s", got.Status)
	}
}

// ── the expired terminal (T-1aa4): owner-only expire, hold release, orphans ──

// expireCardReq drives POST /api/reply-cards/{id}/expire as the given actor.
func expireCardReq(t *testing.T, api *apiServer, cardID, sub, scope string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleExpireReplyCardApiReplyCardsCardIdExpirePost(rec,
		taskReq(t, "POST", "/x", nil, sub, scope), cardID)
	return rec
}

func TestExpireFlipsAWaitingCardToTerminalExpired(t *testing.T) {
	api := newTasksTestServer(t)
	card := openPlainCard(t, api, "m-a")

	rec := expireCardReq(t, api, card.ID, "owner", "owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("expire: %d %s", rec.Code, rec.Body.String())
	}
	dto := decodeBody[replyCardDTO](t, rec)
	if dto.Status != replyCardStatusExpired {
		t.Fatalf("status: got %q want expired", dto.Status)
	}
	if dto.ExpiredTS == nil || *dto.ExpiredTS <= 0 {
		t.Fatalf("expired_ts must stamp: %+v", dto.ExpiredTS)
	}
	if dto.Answer != nil || dto.AnsweredTS != nil {
		t.Fatalf("an expiry is NOT an answer: %+v", dto)
	}

	// Terminal, no reopen: a second expire, an answer, and a re-answer all 409.
	if rec := expireCardReq(t, api, card.ID, "owner", "owner"); rec.Code != http.StatusConflict {
		t.Fatalf("double expire must 409, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := answerCard(t, api, card.ID, map[string]any{"option_idx": 0}); rec.Code != http.StatusConflict {
		t.Fatalf("answer on an expired card must 409, got %d %s", rec.Code, rec.Body.String())
	}
	put := httptest.NewRecorder()
	api.HandleReanswerReplyCardApiReplyCardsCardIdAnswerPut(put,
		taskReq(t, "PUT", "/x", map[string]any{"option_idx": 0}, "owner", "owner"), card.ID)
	if put.Code != http.StatusConflict {
		t.Fatalf("PUT on an expired card must 409, got %d %s", put.Code, put.Body.String())
	}
	stored, _ := api.dal.GetReplyCard(card.ID)
	if stored.Status != replyCardStatusExpired || stored.AnswerText != "" {
		t.Fatalf("the refused writes must leave the card expired and answerless: %+v", stored)
	}
}

func TestExpireOnAnsweredOrMissingCardIsRefused(t *testing.T) {
	api := newTasksTestServer(t)
	card := openPlainCard(t, api, "m-a")
	if rec := answerCard(t, api, card.ID, map[string]any{"option_idx": 0}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	if rec := expireCardReq(t, api, card.ID, "owner", "owner"); rec.Code != http.StatusConflict {
		t.Fatalf("expire on an answered card must 409, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := expireCardReq(t, api, "rc-missing", "owner", "owner"); rec.Code != http.StatusNotFound {
		t.Fatalf("expire on a missing card must 404, got %d", rec.Code)
	}
}

func TestExpiringAGateCardResumesTheTaskAndStep(t *testing.T) {
	// The expire twin of TestAnsweringACardResumesTheTaskAndStep: the owner
	// declining a stale ask releases the waiting_owner hold the same way a
	// first answer does (releaseCardHold) — the agent then decides itself
	// whether to reopen a fresh card or advance.
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "approve", "dod": "go", "is_gate": true},
	})
	gateStep := view.Steps[0]
	startFirstStep(t, api, task.ID, "m-exec")
	card := openGateCard(t, api, task.ID, "m-exec", gateStep.ID, "go?")

	if rec := expireCardReq(t, api, card.ID, "owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("expire: %d %s", rec.Code, rec.Body.String())
	}
	step, _ := api.dal.GetTaskStep(gateStep.ID)
	if step.Status != StepStatusInProgress {
		t.Fatalf("expired card must restore the step to in_progress, got %s", step.Status)
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusInProgress {
		t.Fatalf("expired card must restore the task to in_progress, got %s", got.Status)
	}
	// The freed agent can advance the step itself.
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"status": "done"}, "m-exec", "agent"),
		task.ID, gateStep.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("the agent advances the released step: %d %s", rec.Code, rec.Body.String())
	}
}

func TestExpiringOneCardLeavesTheTaskHeldByAnotherWaitingCard(t *testing.T) {
	// SPEC §3.2 one task, many cards: expiring ONE bound card releases only its
	// own step; the task stays waiting_owner while a sibling card still waits.
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "gate-1", "dod": "d1", "is_gate": true},
		{"name": "gate-2", "dod": "d2", "is_gate": true},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	first := openGateCard(t, api, task.ID, "m-exec", view.Steps[0].ID, "one?")
	second := openGateCard(t, api, task.ID, "m-exec", view.Steps[1].ID, "two?")

	if rec := expireCardReq(t, api, first.ID, "owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("expire: %d %s", rec.Code, rec.Body.String())
	}
	step1, _ := api.dal.GetTaskStep(view.Steps[0].ID)
	if step1.Status != StepStatusInProgress {
		t.Fatalf("the expired card's own step must release, got %s", step1.Status)
	}
	step2, _ := api.dal.GetTaskStep(view.Steps[1].ID)
	if step2.Status != StepStatusWaitingOwner || step2.ReplyCardID != second.ID {
		t.Fatalf("the sibling card's step must keep waiting: %+v", step2)
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusWaitingOwner {
		t.Fatalf("the task stays held while another card waits, got %s", got.Status)
	}
}

func TestExpiringAStaleCardNeverClobbersARearmedStep(t *testing.T) {
	// A follow-up ask re-armed the step with a NEWER card; expiring the OLD
	// card must not release the newer hold.
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "approve", "dod": "go", "is_gate": true},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	old := openGateCard(t, api, task.ID, "m-exec", view.Steps[0].ID, "old?")
	fresh := openGateCard(t, api, task.ID, "m-exec", view.Steps[0].ID, "fresh?")

	if rec := expireCardReq(t, api, old.ID, "owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("expire: %d %s", rec.Code, rec.Body.String())
	}
	step, _ := api.dal.GetTaskStep(view.Steps[0].ID)
	if step.Status != StepStatusWaitingOwner || step.ReplyCardID != fresh.ID {
		t.Fatalf("the re-armed step must keep waiting on the fresh card: %+v", step)
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusWaitingOwner {
		t.Fatalf("the task stays held behind the fresh card, got %s", got.Status)
	}
}

func TestExpiringAnOrphanCardSucceedsWithoutTouchingTheClosedTask(t *testing.T) {
	// T-f571 left orphaned cards (task already terminal) with NO exit — answer
	// is 409. Expire IS that exit: 200, the card closes, and the terminal task
	// is left byte-identical (no status change, no UpdatedTS bump that would
	// float it back up the cockpit).
	for _, status := range []string{TaskStatusTerminated, TaskStatusDone} {
		t.Run(status, func(t *testing.T) {
			api := newTasksTestServer(t)
			task := createAdHocTask(t, api, "m-exec")
			view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
				{"name": "approve", "dod": "go", "is_gate": true},
			})
			startFirstStep(t, api, task.ID, "m-exec")
			card := openGateCard(t, api, task.ID, "m-exec", view.Steps[0].ID, "go?")
			// closeTask directly (same package) — the shared terminal helper
			// behind terminate() and the agent's done report — to orphan the
			// still-waiting card on both terminal branches (the T-f571 test's
			// construction).
			stored, err := api.dal.GetTask(task.ID)
			if err != nil || stored == nil {
				t.Fatalf("task: %v %v", stored, err)
			}
			if err := api.closeTask(stored, status, nowSecs(), "test"); err != nil {
				t.Fatalf("closeTask: %v", err)
			}
			before, _ := api.dal.GetTask(task.ID)

			// The orphan still cannot be ANSWERED (T-f571 unchanged)…
			if rec := answerCard(t, api, card.ID,
				map[string]any{"option_idx": 0}); rec.Code != http.StatusConflict {
				t.Fatalf("orphan answer must stay 409, got %d", rec.Code)
			}
			// …but it CAN be expired.
			rec := expireCardReq(t, api, card.ID, "owner", "owner")
			if rec.Code != http.StatusOK {
				t.Fatalf("orphan expire: %d %s", rec.Code, rec.Body.String())
			}
			storedCard, _ := api.dal.GetReplyCard(card.ID)
			if storedCard.Status != replyCardStatusExpired || storedCard.ExpiredTS <= 0 {
				t.Fatalf("orphan card must close expired: %+v", storedCard)
			}
			after, _ := api.dal.GetTask(task.ID)
			if after.Status != before.Status || after.UpdatedTS != before.UpdatedTS {
				t.Fatalf("the closed task must be untouched: before %+v after %+v",
					before, after)
			}
		})
	}
}

func TestListReplyCardsServesTheExpiredPane(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	now := nowSecs()
	cards := []ReplyCard{
		waitingCard("rc-w", now-10),
		answeredCard("rc-a", now-1000, now-50),
		expiredCard("rc-x-old", now-1000, now-500),
		expiredCard("rc-x-new", now-1000, now-20),
		expiredCard("rc-x-aged", now-100000, now-replyCardAnsweredWindowSecs-100),
	}
	for _, c := range cards {
		if err := s.dal.PutReplyCard(c); err != nil {
			t.Fatalf("put %s: %v", c.ID, err)
		}
	}
	expired := "expired"
	rec := httptest.NewRecorder()
	s.HandleListReplyCardsApiReplyCardsGet(rec,
		httptest.NewRequest("GET", "/api/reply-cards?status=expired", nil),
		HandleListReplyCardsApiReplyCardsGetParams{Status: &expired})
	if rec.Code != http.StatusOK {
		t.Fatalf("list expired: %d %s", rec.Code, rec.Body.String())
	}
	rows := decodeBody[[]replyCardListItemDTO](t, rec)
	if len(rows) != 2 || rows[0].ID != "rc-x-new" || rows[1].ID != "rc-x-old" {
		t.Fatalf("expired pane must window 24h newest-first: %+v", rows)
	}
	if rows[0].ExpiredTS == nil || rows[0].Answer != nil || rows[0].AnsweredTS != nil {
		t.Fatalf("an expired row carries expired_ts and no digest: %+v", rows[0])
	}

	// The unknown-status guard now names all three panes.
	junk := "closed"
	rec = httptest.NewRecorder()
	s.HandleListReplyCardsApiReplyCardsGet(rec,
		httptest.NewRequest("GET", "/api/reply-cards?status=closed", nil),
		HandleListReplyCardsApiReplyCardsGetParams{Status: &junk})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("junk status must 400, got %d", rec.Code)
	}
}

// ── question-side attachments (T-5e8a 開卡帶附件) ────────────────────────────
// A card create may carry attachments — the same input mechanism post_chat
// uses ({id} ref or inline data_b64, same caps, all-or-nothing resolve). The
// refs land on the card's own column AND the companion message's meta (the
// gallery/GC seam); the served DTO carries the download-url projection.

// createCardWithAttachments posts POST /api/reply-cards with the given
// attachments and returns the raw recorder (callers assert the outcome).
func createCardWithAttachments(t *testing.T, api *apiServer, actor string, attachments []map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateReplyCardApiReplyCardsPost(rec,
		taskReq(t, "POST", "/api/reply-cards", map[string]any{
			"kind": "decision", "summary": "which way?",
			"options": []string{"A", "B"}, "attachments": attachments,
		}, actor, "agent"))
	return rec
}

// onePixelPNGB64 is a tiny valid-enough PNG payload (magic bytes only matter
// to the sniffer) for inline-attachment tests.
const onePixelPNGB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func TestCreateCardWithInlineAttachmentStampsCardAndCompanionMessage(t *testing.T) {
	api := newTasksTestServer(t)
	rec := createCardWithAttachments(t, api, "m-exec", []map[string]any{
		{"data_b64": onePixelPNGB64, "filename": "shot.png", "mime": "image/png"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create with inline attachment: %d %s", rec.Code, rec.Body.String())
	}
	card := decodeBody[replyCardDTO](t, rec)
	if len(card.Attachments) != 1 {
		t.Fatalf("served card must carry ONE question attachment: %+v", card.Attachments)
	}
	att := card.Attachments[0]
	if att.ID == "" || att.URL != "/api/chat/attachment/"+att.ID ||
		att.Filename != "shot.png" || att.Mime != "image/png" || !att.IsImage {
		t.Fatalf("served ref must carry the download url + identity: %+v", att)
	}
	// The blob landed in the shared store.
	blob, err := api.dal.GetChatAttachment(att.ID)
	if err != nil || blob == nil {
		t.Fatalf("blob must land in chat_attachment: %v %v", blob, err)
	}
	// The stored card holds the light refs.
	stored, err := api.dal.GetReplyCard(card.ID)
	if err != nil || stored == nil || len(stored.Attachments) != 1 {
		t.Fatalf("stored card refs: %+v %v", stored, err)
	}
	// The companion chat message carries the SAME refs in its meta (the
	// gallery scans meta only; the GC candidate walk starts there).
	msgs, err := api.dal.ListChat()
	if err != nil || len(msgs) != 1 {
		t.Fatalf("companion message: %+v %v", msgs, err)
	}
	refs, _ := msgs[0].Meta["attachments"].([]any)
	if len(refs) != 1 {
		t.Fatalf("companion meta must stamp the refs: %+v", msgs[0].Meta)
	}
	ref, _ := refs[0].(map[string]any)
	if ref["id"] != att.ID {
		t.Fatalf("companion meta ref must name the same blob: %+v", ref)
	}
}

func TestCreateCardWithRefAttachmentReusesTheStoredBlob(t *testing.T) {
	api := newTasksTestServer(t)
	name := "report.pdf"
	if err := api.dal.PutChatAttachment(ChatAttachment{
		ID: "att-preup", Mime: "application/pdf", Data: []byte("%PDF"),
		Filename: &name,
	}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	// The alongside filename/mime are IGNORED — the stored blob is
	// authoritative (upload-response paste-back semantics).
	rec := createCardWithAttachments(t, api, "m-exec", []map[string]any{
		{"id": "att-preup", "filename": "ignored.bin", "mime": "text/plain"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create with ref attachment: %d %s", rec.Code, rec.Body.String())
	}
	card := decodeBody[replyCardDTO](t, rec)
	if len(card.Attachments) != 1 || card.Attachments[0].ID != "att-preup" ||
		card.Attachments[0].Filename != "report.pdf" ||
		card.Attachments[0].Mime != "application/pdf" {
		t.Fatalf("ref attachment must serve the STORED identity: %+v", card.Attachments)
	}
	var blobs int
	if err := api.dal.db.QueryRow(
		`SELECT COUNT(*) FROM chat_attachment`).Scan(&blobs); err != nil {
		t.Fatalf("count blobs: %v", err)
	}
	if blobs != 1 {
		t.Fatalf("a ref must not duplicate the blob: %d rows", blobs)
	}
}

func TestCreateCardWithBadAttachmentsRejectsAtomically(t *testing.T) {
	api := newTasksTestServer(t)
	cases := []struct {
		name string
		atts []map[string]any
	}{
		{"unknown ref", []map[string]any{{"id": "att-nope"}}},
		{"id and data_b64 together", []map[string]any{
			{"id": "att-x", "data_b64": onePixelPNGB64}}},
		{"bad base64", []map[string]any{{"data_b64": "@@not-base64@@"}}},
		{"good sibling before a bad item", []map[string]any{
			{"data_b64": onePixelPNGB64}, {"id": "att-nope"}}},
	}
	over := make([]map[string]any, chatAttachmentsMaxCount+1)
	for i := range over {
		over[i] = map[string]any{"data_b64": onePixelPNGB64}
	}
	cases = append(cases, struct {
		name string
		atts []map[string]any
	}{"over the count cap", over})
	for _, tc := range cases {
		rec := createCardWithAttachments(t, api, "m-exec", tc.atts)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}
	// NOTHING was created by any rejected attempt: no card, no companion
	// message, no orphan blob (all-or-nothing resolve runs before any store).
	cards, err := api.dal.ListReplyCards()
	if err != nil || len(cards) != 0 {
		t.Fatalf("no card may exist after rejects: %+v %v", cards, err)
	}
	msgs, err := api.dal.ListChat()
	if err != nil || len(msgs) != 0 {
		t.Fatalf("no companion message may exist after rejects: %+v %v", msgs, err)
	}
	var blobs int
	if err := api.dal.db.QueryRow(
		`SELECT COUNT(*) FROM chat_attachment`).Scan(&blobs); err != nil {
		t.Fatalf("count blobs: %v", err)
	}
	if blobs != 0 {
		t.Fatalf("no orphan blob may survive a reject: %d rows", blobs)
	}
}

func TestCreateCardWithoutAttachmentsKeepsTheOldShape(t *testing.T) {
	api := newTasksTestServer(t)
	card := openPlainCard(t, api, "m-exec")
	if card.Attachments == nil || len(card.Attachments) != 0 {
		t.Fatalf("a card without attachments serves attachments: [] (never null): %+v",
			card.Attachments)
	}
	msgs, err := api.dal.ListChat()
	if err != nil || len(msgs) != 1 {
		t.Fatalf("companion message: %+v %v", msgs, err)
	}
	if _, stamped := msgs[0].Meta["attachments"]; stamped {
		t.Fatalf("an attachment-less create must NOT stamp meta[attachments]: %+v",
			msgs[0].Meta)
	}
}

func TestOpenGateCarriesQuestionAttachments(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "approve", "dod": "owner said go", "is_gate": true},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	rec := httptest.NewRecorder()
	api.HandleOpenTaskGateApiTasksTaskIdStepsStepIdGatePost(rec,
		taskReq(t, "POST", "/x", map[string]any{
			"kind": "decision", "summary": "ship it?",
			"options": []string{"ship", "hold"},
			"attachments": []map[string]any{
				{"data_b64": onePixelPNGB64, "filename": "diff.png", "mime": "image/png"},
			},
		}, "m-exec", "agent"), task.ID, view.Steps[0].ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("open gate with attachment: %d %s", rec.Code, rec.Body.String())
	}
	card := decodeBody[replyCardDTO](t, rec)
	if len(card.Attachments) != 1 || card.Attachments[0].Filename != "diff.png" {
		t.Fatalf("gate card must carry the question attachment: %+v", card.Attachments)
	}
}
