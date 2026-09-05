package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

func waitingCard(id string, created float64) ReplyCard {
	return ReplyCard{
		ID: id, FromMember: "m-a", Kind: replyCardKindDecision,
		Summary: "s", Options: []ReplyCardOption{{Text: "A"}, {Text: "B"}},
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

func opt(text string) ReplyCardOptionDTO { return ReplyCardOptionDTO{Text: text} }

func opts(n int) []ReplyCardOptionDTO {
	out := make([]ReplyCardOptionDTO, n)
	for i := range out {
		out[i] = opt("opt-" + strconv.Itoa(i))
	}
	return out
}

func aiOpt(text string) ReplyCardOptionDTO {
	pick := true
	return ReplyCardOptionDTO{Text: text, AiPick: &pick}
}

func TestValidateReplyCardOptions(t *testing.T) {
	cases := []struct {
		name       string
		options    []ReplyCardOptionDTO
		selectMode string
		wantOK     bool
	}{
		{"empty", nil, replyCardSelectModeSingle, false},
		{"one", []ReplyCardOptionDTO{opt("A")}, replyCardSelectModeSingle, true},
		{"four", []ReplyCardOptionDTO{opt("A"), opt("B"), opt("C"), opt("D")},
			replyCardSelectModeSingle, true},
		{"five", []ReplyCardOptionDTO{opt("A"), opt("B"), opt("C"), opt("D"), opt("E")},
			replyCardSelectModeSingle, false},
		// T-43: the cap is per select_mode. The five-option single above and
		// these three rows are the whole contract — a multi card takes 20,
		// refuses 21, and the single cap does NOT move with it.
		{"multi five", opts(5), replyCardSelectModeMulti, true},
		{"multi twenty", opts(20), replyCardSelectModeMulti, true},
		{"multi twenty-one", opts(21), replyCardSelectModeMulti, false},
		{"blank member", []ReplyCardOptionDTO{opt("A"), opt("  ")},
			replyCardSelectModeSingle, false},
		{"single with one ai_pick", []ReplyCardOptionDTO{aiOpt("A"), opt("B")},
			replyCardSelectModeSingle, true},
		{"single with no ai_pick", []ReplyCardOptionDTO{opt("A"), opt("B")},
			replyCardSelectModeSingle, true},
		{"single with two ai_picks", []ReplyCardOptionDTO{aiOpt("A"), aiOpt("B")},
			replyCardSelectModeSingle, false},
		{"multi with two ai_picks", []ReplyCardOptionDTO{aiOpt("A"), aiOpt("B")},
			replyCardSelectModeMulti, true},
		{"multi with four ai_picks",
			[]ReplyCardOptionDTO{aiOpt("A"), aiOpt("B"), aiOpt("C"), aiOpt("D")},
			replyCardSelectModeMulti, true},
	}
	for _, tc := range cases {
		got, problem := validateReplyCardOptions(tc.options, tc.selectMode)
		if (problem == "") != tc.wantOK {
			t.Fatalf("%s: wantOK=%v got problem=%q", tc.name, tc.wantOK, problem)
		}
		if tc.wantOK && len(got) != len(tc.options) {
			t.Fatalf("%s: validated options lost entries: %v", tc.name, got)
		}
	}
	// The refusal NAMES which cap was hit; conformance pins the same two
	// sentences on the wire, and an agent that reads "at most 4" on a multi
	// card would stop at the wrong number.
	for _, tc := range []struct {
		options    []ReplyCardOptionDTO
		selectMode string
		want       string
	}{
		{opts(5), replyCardSelectModeSingle, "a single-select card may carry at most 4 options"},
		{opts(21), replyCardSelectModeMulti, "a multi-select card may carry at most 20 options"},
	} {
		if _, problem := validateReplyCardOptions(tc.options, tc.selectMode); problem != tc.want {
			t.Fatalf("%s over-cap refusal = %q, want %q", tc.selectMode, problem, tc.want)
		}
	}

	trimmed, problem := validateReplyCardOptions(
		[]ReplyCardOptionDTO{opt(" A "), aiOpt("B")}, replyCardSelectModeSingle)
	if problem != "" {
		t.Fatalf("unexpected problem: %q", problem)
	}
	if !reflect.DeepEqual(trimmed,
		[]ReplyCardOption{{Text: "A"}, {Text: "B", AIPick: true}}) {
		t.Fatalf("options must be trimmed and carry ai_pick per option: %+v", trimmed)
	}
}

// normalizeAnswerOptionIdxs is the whole reason the stored answer cannot depend
// on the owner's click order: [2,0] and [0,2] are the same decision, and a
// reader that could tell them apart once mistook a re-ordered re-answer for a
// changed one and swallowed the delivery.
func TestNormalizeAnswerOptionIdxs(t *testing.T) {
	cases := []struct {
		name string
		in   []int
		want []int
	}{
		{"nil is nil", nil, nil},
		{"empty is nil", []int{}, nil},
		{"single", []int{2}, []int{2}},
		{"descending sorts", []int{2, 0}, []int{0, 2}},
		{"ascending unchanged", []int{0, 2}, []int{0, 2}},
		{"duplicates collapse", []int{1, 1, 0, 1}, []int{0, 1}},
	}
	for _, tc := range cases {
		if got := normalizeAnswerOptionIdxs(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: normalize(%v) = %v, want %v", tc.name, tc.in, got, tc.want)
		}
	}
	if !reflect.DeepEqual(normalizeAnswerOptionIdxs([]int{2, 0}),
		normalizeAnswerOptionIdxs([]int{0, 2})) {
		t.Fatal("[2,0] and [0,2] must normalize to the same stored answer")
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
	mustDTO := func(m ChatMessage) chatMessageDTO {
		t.Helper()
		d, err := s.servedChatMessageDTO(m)
		if err != nil {
			t.Fatalf("servedChatMessageDTO: %v", err)
		}
		return d
	}
	if got := mustDTO(msg).ReplyCardStatus; got != replyCardStatusWaiting {
		t.Fatalf("waiting join: got %q want waiting", got)
	}
	// Answering the card flips the read-time join (it is NOT stored on the msg).
	card.Status = replyCardStatusAnswered
	card.AnsweredTS = 20
	if err := s.dal.PutReplyCard(card); err != nil {
		t.Fatalf("answer card: %v", err)
	}
	if got := mustDTO(msg).ReplyCardStatus; got != replyCardStatusAnswered {
		t.Fatalf("answered flip: got %q want answered", got)
	}
	// A plain message (no reply_card_id) has an empty status.
	plain := ChatMessage{ID: "c-2", Sender: "m-a", Recipient: wireOwnerID, Body: "hi", TS: 11}
	if got := mustDTO(plain).ReplyCardStatus; got != "" {
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
	c := answeredCard("rc-2", 5, 9)
	c.AnswerOptionIdxs = []int{1}
	c.AnswerText = "ok"
	c.AnswerAttachments = []any{
		map[string]any{"id": "att-1", "mime": "image/png", "filename": "a.png"},
	}
	dto = newReplyCardDTO(c)
	if dto.AnsweredTS == nil || *dto.AnsweredTS != 9 {
		t.Fatalf("answered_ts not projected: %+v", dto)
	}
	if dto.Answer == nil || !reflect.DeepEqual(dto.Answer.OptionIdxs, []int{1}) ||
		dto.Answer.Text != "ok" {
		t.Fatalf("answer not projected: %+v", dto.Answer)
	}
	if len(dto.Answer.Attachments) != 1 ||
		dto.Answer.Attachments[0].URL != "/api/chat/attachment/att-1" {
		t.Fatalf("attachment refs not projected: %+v", dto.Answer.Attachments)
	}
}

func TestReplyCardDALRoundTrip(t *testing.T) {
	dal := newTestDAL(t)
	card := ReplyCard{
		ID: "rc-round", FromMember: "m-a", Kind: replyCardKindAction,
		Summary: "do the thing", Body: "details",
		Options: []ReplyCardOption{{Text: "done, continue"}},
		Status:  replyCardStatusAnswered, CreatedTS: 1.5, AnsweredTS: 2.5,
		ChatMessageID: "c-1", AnswerOptionIdxs: []int{0}, AnswerText: "done",
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
	if !reflect.DeepEqual(got.Options, []ReplyCardOption{{Text: "done, continue"}}) {
		t.Fatalf("options JSON round trip: %+v", got.Options)
	}
	if !reflect.DeepEqual(got.AnswerOptionIdxs, []int{0}) {
		t.Fatalf("answer_option_idxs must round-trip [0] (not fold to null): %+v",
			got.AnswerOptionIdxs)
	}
	if len(got.AnswerAttachments) != 1 {
		t.Fatalf("answer_attachments JSON round trip: %+v", got.AnswerAttachments)
	}
	missing, err := dal.GetReplyCard("rc-absent")
	if err != nil || missing != nil {
		t.Fatalf("absent card must read nil,nil: %v %v", missing, err)
	}
}

// ── the ONE card-open entrance: linked_task (T-18) ───────────────────────────
// create_reply_card is the only way a card opens, and linked_task is REQUIRED:
// null (this ask is not about a task) or {task_id, step_id} (it is about this
// step). Nothing is inferred. The tests below pin all three shapes plus the
// SENTENCES the refusals carry, because on this ticket the message IS the
// feature — a 400 that only says "invalid request" sends the caller back to
// the docs, which is the same silence the old auto-binding had.

// openPlainCard posts one unbound POST /api/reply-cards as the given actor.
func openPlainCard(t *testing.T, api *apiServer, actor string) replyCardDTO {
	t.Helper()
	rec := openPlainCardRaw(t, api, actor)
	if rec.Code != http.StatusOK {
		t.Fatalf("create card: %d %s", rec.Code, rec.Body.String())
	}
	return createdCardView(t, api, rec)
}

// createdCardView turns a create_reply_card recorder into the full card view.
//
// 🔴 T-91 RESHAPED create_reply_card's ANSWER, and this helper is where the
// tests absorb it. The write answers a receipt — {id, chat_message_id,
// created_ts, attachments} — because the summary, body and options were all
// the caller's own bytes one line earlier. The two ids ARE news (both minted
// here) and so are the attachment ids (an inline upload has none until the
// server mints one), which is why those four survive.
//
// Every helper below that used to read the whole card off the create response
// now reads it through get_reply_card, which is the door the cockpit's own
// per-card refetch uses. Tests that were reaching into the create response for
// body/options/task were not pinning the create SHAPE — they were using it as
// a free read, and this keeps that read honest by making it a read.
func createdCardView(t *testing.T, api *apiServer, rec *httptest.ResponseRecorder) replyCardDTO {
	t.Helper()
	receipt := decodeBody[replyCardCreateReceiptDTO](t, rec)
	if receipt.ID == "" {
		t.Fatalf("create receipt carried no card id: %s", rec.Body.String())
	}
	fresh := getReplyCardRaw(t, api, receipt.ID)
	if fresh.Code != http.StatusOK {
		t.Fatalf("get_reply_card %s: %d %s", receipt.ID, fresh.Code, fresh.Body.String())
	}
	return decodeBody[replyCardDTO](t, fresh)
}

// openPlainCardRaw is openPlainCard without the 200 assertion — the REFUSAL
// tests need the recorder to read the status AND the reason off.
func openPlainCardRaw(t *testing.T, api *apiServer, actor string) *httptest.ResponseRecorder {
	t.Helper()
	return createCardRaw(t, api, actor, map[string]any{
		"kind": "decision", "summary": "which way?",
		"options": []map[string]any{{"text": "A"}, {"text": "B"}}, "linked_task": nil,
	})
}

// openBoundCardRaw is the {task_id, step_id} shape — the twin of the retired
// open_gate route, now the same door as every other card.
func openBoundCardRaw(t *testing.T, api *apiServer, actor, taskID, stepID string) *httptest.ResponseRecorder {
	t.Helper()
	return createCardRaw(t, api, actor, map[string]any{
		"kind": "decision", "summary": "which way?", "options": []map[string]any{{"text": "A"}, {"text": "B"}},
		"linked_task": map[string]any{"task_id": taskID, "step_id": stepID},
	})
}

func openBoundCard(t *testing.T, api *apiServer, actor, taskID, stepID string) replyCardDTO {
	t.Helper()
	rec := openBoundCardRaw(t, api, actor, taskID, stepID)
	if rec.Code != http.StatusOK {
		t.Fatalf("create bound card: %d %s", rec.Code, rec.Body.String())
	}
	return createdCardView(t, api, rec)
}

// createCardRaw posts an arbitrary create body.
func createCardRaw(t *testing.T, api *apiServer, actor string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleCreateReplyCardApiReplyCardsPost(rec,
		taskReq(t, "POST", "/api/reply-cards", body, actor, "agent"))
	return rec
}

// errorMessageOf reads the unified error envelope's message
// ({"error":{"code","message"}}). A guard test that asserts only the STATUS
// cannot tell "correctly refused" from "accidentally broken" — both are 409 —
// so every refusal assertion below reads the REASON too.
func errorMessageOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error envelope: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Message
}

func assertNoCardMinted(t *testing.T, api *apiServer) {
	t.Helper()
	cards, err := api.dal.ListReplyCards()
	if err != nil {
		t.Fatalf("list cards: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("a refused create must mint no card, got %d: %+v", len(cards), cards)
	}
	msgs, err := api.dal.ListChat()
	if err != nil {
		t.Fatalf("list chat: %v", err)
	}
	for _, m := range msgs {
		if m.Meta != nil && m.Meta["reply_card_id"] != nil {
			t.Fatalf("a refused create must leave no companion chat message: %+v", m)
		}
	}
}

// startStep drives one step to in_progress (the agent's own report).
func startStep(t *testing.T, api *apiServer, taskID, stepID, actor string) {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskStepStatusApiTasksTaskIdStepsStepIdStatusPost(rec,
		taskReq(t, "POST", "/x", map[string]any{"status": "in_progress"}, actor, "agent"),
		taskID, stepID)
	if rec.Code != http.StatusOK {
		t.Fatalf("step start: %d %s", rec.Code, rec.Body.String())
	}
}

// TestCreateReplyCardWithoutLinkedTaskNamesBothLegalShapes is the ticket's
// centre of gravity, and it deliberately pins the SENTENCE rather than only the
// 400. The whole design is "not deciding must be impossible to do silently"; an
// error trimmed to `invalid request` would satisfy the status code and undo the
// feature, so the message is the assertion.
func TestCreateReplyCardWithoutLinkedTaskNamesBothLegalShapes(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "recon", "dod": "understood"},
	})
	startStep(t, api, task.ID, view.Steps[0].ID, "m-exec")

	// A body that would have auto-bound perfectly well before T-18 — the caller
	// is the executor of exactly one active task with exactly one running step.
	// It is still refused, because the caller never SAID anything.
	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind": "decision", "summary": "which way?", "options": []map[string]any{{"text": "A"}, {"text": "B"}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an omitted linked_task must be a 400, got %d %s", rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != linkedTaskRequiredMsg {
		t.Fatalf("the refusal must be the sentence that spells out both legal shapes: %q", msg)
	}
	assertNoCardMinted(t, api)

	// The step and the task are untouched: a refused create changes nothing.
	step, _ := api.dal.GetTaskStep(view.Steps[0].ID)
	if step.Status != StepStatusInProgress || step.ReplyCardID != "" {
		t.Fatalf("a refused create must not touch the step: %+v", step)
	}
}

// TestCreateReplyCardWithTaskIdButNoStepIdIsRefused guards the ORPHAN SHAPE.
// T-4166 spent a whole ticket making "bound to a task, bound to no step"
// unreachable through the old entrance — a card in that shape places no
// waiting_owner hold, so the task marches to done underneath the question and
// the owner's answer is refused 409 forever. The new entrance must not hand it
// back, so this gate is not optional and neither is its message.
func TestCreateReplyCardWithTaskIdButNoStepIdIsRefused(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "recon", "dod": "understood"},
	})
	startStep(t, api, task.ID, view.Steps[0].ID, "m-exec")

	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind": "decision", "summary": "which way?", "options": []map[string]any{{"text": "A"}, {"text": "B"}},
		"linked_task": map[string]any{"task_id": task.ID},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a task-only linked_task must be a 400, got %d %s", rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != linkedTaskStepRequiredMsg {
		t.Fatalf("the refusal must be the sentence naming the missing step and what it costs: %q", msg)
	}
	assertNoCardMinted(t, api)

	// An explicitly BLANK step_id is the same offence, not a way round it.
	rec = createCardRaw(t, api, "m-exec", map[string]any{
		"kind": "decision", "summary": "which way?", "options": []map[string]any{{"text": "A"}, {"text": "B"}},
		"linked_task": map[string]any{"task_id": task.ID, "step_id": "  "},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a blank step_id must be a 400 too, got %d %s", rec.Code, rec.Body.String())
	}
	assertNoCardMinted(t, api)
}

func TestCreateReplyCardWithStepIdButNoTaskIdIsRefused(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "recon", "dod": "understood"},
	})
	startStep(t, api, task.ID, view.Steps[0].ID, "m-exec")

	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind": "decision", "summary": "which way?", "options": []map[string]any{{"text": "A"}, {"text": "B"}},
		"linked_task": map[string]any{"step_id": view.Steps[0].ID},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a step-only linked_task must be a 400, got %d %s", rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != linkedTaskTaskRequiredMsg {
		t.Fatalf("the refusal must be the sentence naming the missing task_id: %q", msg)
	}
	assertNoCardMinted(t, api)
}

// TestCreateReplyCardWithNullLinkedTaskOpensAnUnboundCard: null is a legal
// answer, not a fallback. It must work for an agent holding live work too —
// otherwise "this ask is not about my task" would be unsayable for exactly the
// people who need to say it.
func TestCreateReplyCardWithNullLinkedTaskOpensAnUnboundCard(t *testing.T) {
	api := newTasksTestServer(t)

	// No work at all.
	card := openPlainCard(t, api, "m-free")
	if card.Task != nil {
		t.Fatalf("an unbound card must carry no task ref: %+v", card.Task)
	}
	stored, err := api.dal.GetReplyCard(card.ID)
	if err != nil || stored == nil {
		t.Fatalf("stored card: %v %v", stored, err)
	}
	if stored.TaskID != "" || stored.TaskStepID != "" {
		t.Fatalf("an unbound card must store no binding: %+v", stored)
	}

	// A perfectly bindable executor may still declare "not about the task".
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "recon", "dod": "understood"},
	})
	startStep(t, api, task.ID, view.Steps[0].ID, "m-exec")

	second := openPlainCard(t, api, "m-exec")
	if second.Task != nil {
		t.Fatalf("linked_task=null must stay unbound even for a busy executor: %+v", second.Task)
	}
	step, _ := api.dal.GetTaskStep(view.Steps[0].ID)
	if step.Status != StepStatusInProgress || step.ReplyCardID != "" {
		t.Fatalf("an unbound card must place no hold: %+v", step)
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusInProgress {
		t.Fatalf("the task must keep running, got %s", got.Status)
	}
}

// TestCreateReplyCardWithLinkedTaskArmsTheStepAndFlipsTheTask is the state
// machine the retired open_gate route used to drive, now reached through the
// one entrance.
func TestCreateReplyCardWithLinkedTaskArmsTheStepAndFlipsTheTask(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "recon", "dod": "understood"},
		{"name": "build", "dod": "built"},
	})
	startStep(t, api, task.ID, view.Steps[1].ID, "m-exec")

	card := openBoundCard(t, api, "m-exec", task.ID, view.Steps[1].ID)
	if card.Task == nil || card.Task.ID != task.ID {
		t.Fatalf("a bound card must carry the task ref: %+v", card.Task)
	}
	stored, err := api.dal.GetReplyCard(card.ID)
	if err != nil || stored == nil {
		t.Fatalf("stored card: %v %v", stored, err)
	}
	if stored.TaskID != task.ID || stored.TaskStepID != view.Steps[1].ID {
		t.Fatalf("card must store the declared binding: %+v", stored)
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
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusWaitingOwner {
		t.Fatalf("task must follow into waiting_owner, got %s", got.Status)
	}

	// The untouched sibling step never moves.
	other, _ := api.dal.GetTaskStep(view.Steps[0].ID)
	if other.Status != StepStatusPending || other.ReplyCardID != "" {
		t.Fatalf("sibling step must stay untouched: %+v", other)
	}

	// A FOLLOW-UP ask on a step that already waits re-points it at the NEW card.
	second := openBoundCard(t, api, "m-exec", task.ID, view.Steps[1].ID)
	step, _ = api.dal.GetTaskStep(view.Steps[1].ID)
	if step.Status != StepStatusWaitingOwner || step.ReplyCardID != second.ID {
		t.Fatalf("follow-up ask must re-point the step at the new card: %+v", step)
	}
}

// TestCreateReplyCardArmsAPlainNonGateStep: is_gate is a plan-declared property
// (submit_plan) and arming does not rewrite it — an ad-hoc 請示 on the node you
// are standing on is legitimate. This was open_gate's behaviour and it survives.
func TestCreateReplyCardArmsAPlainNonGateStep(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "recon", "dod": "understood"},
	})
	startStep(t, api, task.ID, view.Steps[0].ID, "m-exec")

	card := openBoundCard(t, api, "m-exec", task.ID, view.Steps[0].ID)
	step, _ := api.dal.GetTaskStep(view.Steps[0].ID)
	if step.Status != StepStatusWaitingOwner || step.ReplyCardID != card.ID {
		t.Fatalf("a plain step must arm: %+v", step)
	}
	if step.IsGate {
		t.Fatalf("arming must not rewrite is_gate: %+v", step)
	}
}

func TestCreateReplyCardRefusesAStepThatIsNotOnTheTask(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "recon", "dod": "understood"},
	})
	startStep(t, api, task.ID, view.Steps[0].ID, "m-exec")

	other := createAdHocTask(t, api, "m-exec")
	otherView := submitPlan(t, api, other.ID, "m-exec", []map[string]any{
		{"name": "elsewhere", "dod": "done"},
	})

	rec := openBoundCardRaw(t, api, "m-exec", task.ID, otherView.Steps[0].ID)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a step of another task must be a 404, got %d %s", rec.Code, rec.Body.String())
	}
	if msg, want := errorMessageOf(t, rec),
		"step '"+otherView.Steps[0].ID+"' not found"; msg != want {
		t.Fatalf("the refusal must name the step it could not find, want %q got %q", want, msg)
	}
	assertNoCardMinted(t, api)
}

func TestCreateReplyCardRefusesATerminalStep(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "recon", "dod": "understood"},
		{"name": "build", "dod": "built"},
	})
	startStep(t, api, task.ID, view.Steps[0].ID, "m-exec")
	if rec := reportStepStatus(t, api, task.ID, view.Steps[0].ID, "m-exec",
		"done", ""); rec.Code != http.StatusOK {
		t.Fatalf("step done: %d %s", rec.Code, rec.Body.String())
	}
	startStep(t, api, task.ID, view.Steps[1].ID, "m-exec")

	rec := openBoundCardRaw(t, api, "m-exec", task.ID, view.Steps[0].ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("a done step must be a 409, got %d %s", rec.Code, rec.Body.String())
	}
	if msg, want := errorMessageOf(t, rec),
		"step '"+view.Steps[0].ID+"' is already done"; msg != want {
		t.Fatalf("the refusal must name the terminal status, want %q got %q", want, msg)
	}
	assertNoCardMinted(t, api)
}

func TestCreateReplyCardRefusesACallerWhoDoesNotDriveTheTask(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "recon", "dod": "understood"},
	})
	startStep(t, api, task.ID, view.Steps[0].ID, "m-exec")

	rec := openBoundCardRaw(t, api, "m-stranger", task.ID, view.Steps[0].ID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a stranger must be a 403, got %d %s", rec.Code, rec.Body.String())
	}
	assertNoCardMinted(t, api)
}

func TestOpenReplyCardRefusesAStepLessTaskBinding(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	body := ReplyCardCreateDTO{
		Kind: "decision", Summary: "which way?", Options: []ReplyCardOptionDTO{opt("A"), opt("B")},
	}
	card, problem, err := api.openReplyCard("m-exec", body, task.ID, "")
	if err == nil {
		t.Fatalf("a step-less task binding must fail loudly, got card=%+v problem=%q",
			card, problem)
	}
	want := "refusing to mint a reply card bound to task '" + task.ID +
		"' with no step: a step-less task binding places no 等我回覆 hold " +
		"and orphans the card when the task closes"
	if err.Error() != want {
		t.Fatalf("the refusal must name the offence and the task, want %q got %q", want, err)
	}
	if card != nil {
		t.Fatalf("a refused mint must return no card: %+v", card)
	}
	assertNoCardMinted(t, api)
}

// TestCreateReplyCardOnAGroupedStepFlipsTheWholeTask pins the T-9ca5 carve-out
// removal: arming a card on a parallel-lane step DERIVES the WHOLE task to
// waiting_owner (owner ruling: any step 等我回覆 → task 等我回覆).
func TestCreateReplyCardOnAGroupedStepFlipsTheWholeTask(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "lane-a", "dod": "a done", "parallel_group": "pg"},
		{"name": "lane-b", "dod": "b done", "parallel_group": "pg"},
	})
	if rec := reportStepStatus(t, api, task.ID, view.Steps[0].ID, "m-exec",
		"in_progress", ""); rec.Code != http.StatusOK {
		t.Fatalf("step start: %d %s", rec.Code, rec.Body.String())
	}
	card := openBoundCard(t, api, "m-exec", task.ID, view.Steps[0].ID)
	step, _ := api.dal.GetTaskStep(view.Steps[0].ID)
	if step.Status != StepStatusWaitingOwner || step.ReplyCardID != card.ID {
		t.Fatalf("grouped lane must arm: %+v", step)
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusWaitingOwner {
		t.Fatalf("arming a grouped lane flips the whole task to waiting_owner, got %s",
			got.Status)
	}
}

// TestCreateReplyCardRejectsTheRetiredBindField: bind was the auto-binding
// opt-out and it is GONE. A caller still sending it gets the decoder's
// unknown-field 422 rather than a silent drop — the same fail-closed typo
// behaviour every other write has.
func TestCreateReplyCardRejectsTheRetiredBindField(t *testing.T) {
	api := newTasksTestServer(t)
	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind": "decision", "summary": "which way?", "options": []map[string]any{{"text": "A"}, {"text": "B"}},
		"linked_task": nil, "bind": "none",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("the retired bind field must be refused, got %d %s", rec.Code, rec.Body.String())
	}
	assertNoCardMinted(t, api)
}

func TestBoundCardStillAnswersAndReleasesTheHold(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "work", "dod": "d"},
	})
	startFirstStep(t, api, task.ID, "m-exec")

	card := openBoundCard(t, api, "m-exec", task.ID, view.Steps[0].ID)
	if card.Task == nil || card.Task.ID != task.ID {
		t.Fatalf("the good path must carry the task ref: %+v", card.Task)
	}
	stored, _ := api.dal.GetReplyCard(card.ID)
	if stored.TaskID != task.ID || stored.TaskStepID != view.Steps[0].ID {
		t.Fatalf("the good path must bind BOTH levels: %+v", stored)
	}
	if got, _ := api.dal.GetTask(task.ID); got.Status != TaskStatusWaitingOwner {
		t.Fatalf("the bound task must hold in waiting_owner, got %s", got.Status)
	}

	if rec := answerCard(t, api, card.ID,
		map[string]any{"option_idxs": []int{0}}); rec.Code != http.StatusOK {
		t.Fatalf("a live bound card must still answer 200, got %d %s",
			rec.Code, rec.Body.String())
	}
	stored, _ = api.dal.GetReplyCard(card.ID)
	if stored.Status != replyCardStatusAnswered {
		t.Fatalf("answered card must flip: %+v", stored)
	}
	step, _ := api.dal.GetTaskStep(view.Steps[0].ID)
	if step.Status != StepStatusInProgress {
		t.Fatalf("the answer must release the step hold, got %s", step.Status)
	}
	if got, _ := api.dal.GetTask(task.ID); got.Status != TaskStatusInProgress {
		t.Fatalf("the answer must release the task hold, got %s", got.Status)
	}
}

// ── the expired terminal (T-1aa4): expire — the owner / an admin agent since
// T-6020, and the card's OWN AUTHOR since T-1b88 (owner 2026-08-07, card
// rc-3ff94b116970) — hold release, orphans ──
//
// ⚠️ Everything in this block drives the handler FUNCTION directly, so it never
// passes through requirePrincipalClass. It therefore proves nothing about the
// route's principal floor: that half lives in
// routes_t6020_governance_test.go (table) and conformance/test_auth_matrix.py
// (live). Do not cite a green here as evidence that the floor moved.

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
	if rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": []int{0}}); rec.Code != http.StatusConflict {
		t.Fatalf("answer on an expired card must 409, got %d %s", rec.Code, rec.Body.String())
	}
	put := httptest.NewRecorder()
	api.HandleReanswerReplyCardApiReplyCardsCardIdAnswerPut(put,
		taskReq(t, "PUT", "/x", map[string]any{"option_idxs": []int{0}}, "owner", "owner"), card.ID)
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
	if rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": []int{0}}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	if rec := expireCardReq(t, api, card.ID, "owner", "owner"); rec.Code != http.StatusConflict {
		t.Fatalf("expire on an answered card must 409, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := expireCardReq(t, api, "rc-missing", "owner", "owner"); rec.Code != http.StatusNotFound {
		t.Fatalf("expire on a missing card must 404, got %d", rec.Code)
	}
}

// ── T-1b88: the author exception, one test per rung ──
//
// The rungs are 404 → 403 (not your card) → 409 (yours, already settled), and
// they are deliberately SEPARATE tests: a single table would let one mutant
// redden four rows at once, and then "the guard reddened" would not say which
// guard. Each test below also asserts the card's stored state after the refusal —
// a half-applied refusal is worse than none.

func TestExpireByTheCardsOwnAuthorIsAllowed(t *testing.T) {
	// The point of the whole ticket: the agent that opened the ask retires it
	// itself, with no owner in the loop. Scope is a plain "agent" — NOT owner.
	api := newTasksTestServer(t)
	card := openPlainCard(t, api, "m-a")

	rec := expireCardReq(t, api, card.ID, "m-a", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("the author must be able to expire its own card: %d %s", rec.Code, rec.Body.String())
	}
	dto := decodeBody[replyCardDTO](t, rec)
	if dto.Status != replyCardStatusExpired {
		t.Fatalf("status: got %q want expired", dto.Status)
	}
	// Withdrawn, NOT answered — that distinction is what the cockpit renders.
	if dto.ExpiredTS == nil || *dto.ExpiredTS <= 0 {
		t.Fatalf("expired_ts must stamp: %+v", dto.ExpiredTS)
	}
	if dto.Answer != nil || dto.AnsweredTS != nil {
		t.Fatalf("a withdrawal is NOT an answer: %+v", dto)
	}
}

func TestExpireByAnotherAgentIsRefusedAsNotItsCard(t *testing.T) {
	api := newTasksTestServer(t)
	card := openPlainCard(t, api, "m-a")

	rec := expireCardReq(t, api, card.ID, "m-b", "agent")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a stranger must be refused 403, got %d %s", rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != expireNotYourCardMsg {
		t.Fatalf("the refusal must name the boundary, got %q", msg)
	}
	stored, _ := api.dal.GetReplyCard(card.ID)
	if stored.Status != replyCardStatusWaiting || stored.ExpiredTS != 0 {
		t.Fatalf("a refused expire must leave the card untouched: %+v", stored)
	}
}

func TestExpireByTheAuthorOnAnAnsweredCardIsRefusedAsSettled(t *testing.T) {
	// The author may retire an ask nobody answered. Once the owner HAS answered,
	// that answer is a decision and no one — author included — erases it.
	api := newTasksTestServer(t)
	card := openPlainCard(t, api, "m-a")
	if rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": []int{0}}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}

	rec := expireCardReq(t, api, card.ID, "m-a", "agent")
	if rec.Code != http.StatusConflict {
		t.Fatalf("an answered card must refuse with 409, got %d %s", rec.Code, rec.Body.String())
	}
	stored, _ := api.dal.GetReplyCard(card.ID)
	if stored.Status != replyCardStatusAnswered || stored.ExpiredTS != 0 {
		t.Fatalf("the owner's answer must survive: %+v", stored)
	}
}

func TestExpireByTheAuthorOnAnAlreadyExpiredCardIsRefusedAsTerminal(t *testing.T) {
	api := newTasksTestServer(t)
	card := openPlainCard(t, api, "m-a")
	if rec := expireCardReq(t, api, card.ID, "m-a", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("first expire: %d %s", rec.Code, rec.Body.String())
	}

	rec := expireCardReq(t, api, card.ID, "m-a", "agent")
	if rec.Code != http.StatusConflict {
		t.Fatalf("a terminal card must refuse with 409, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestExpireRefusesAStrangerBeforeItLooksAtTheStatus(t *testing.T) {
	// Rung ORDER, not just the set of rungs: a stranger asking about a settled
	// card gets 403, never 409, so a refusal names the caller's ACTUAL problem
	// ("not your card") instead of the first one it trips over. ⚠️ NOT a
	// confidentiality boundary — do not read the assertion below as one: reading
	// a card is a separate, unrestricted surface (GET /api/reply-cards/{card_id}
	// and the list route sit at the machine floor with NO ownership check), so
	// the order hides nothing that is not already readable by any agent. The
	// The refusal TEXT is the observable that distinguishes the two rungs: swap
	// the checks and it starts talking about the card's state instead of the
	// caller's standing, so the assertion below pins the whole authorship
	// sentence rather than probing for a keyword.
	api := newTasksTestServer(t)
	card := openPlainCard(t, api, "m-a")
	if rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": []int{0}}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}

	rec := expireCardReq(t, api, card.ID, "m-b", "agent")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("authorship is checked before status: got %d %s", rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != expireNotYourCardMsg {
		t.Fatalf("the authorship rung must answer, not the status rung, got %q", msg)
	}
}

func TestExpiringAGateCardAsItsAuthorResumesTheTaskAndStep(t *testing.T) {
	// Acceptance 3 on the NEW path: the existing owner-driven twin
	// (TestExpiringAGateCardResumesTheTaskAndStep) proves the hold release still
	// works when the owner presses it; this one proves the author's own
	// withdrawal goes through the very same releaseCardHold seam, so the step and
	// the task fall back to in_progress and the agent can carry on by itself.
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "approve", "dod": "go", "is_gate": true},
	})
	gateStep := view.Steps[0]
	startFirstStep(t, api, task.ID, "m-exec")
	card := openCardOnStep(t, api, task.ID, "m-exec", gateStep.ID, "go?")

	if rec := expireCardReq(t, api, card.ID, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("the author withdraws its own gate card: %d %s", rec.Code, rec.Body.String())
	}
	step, _ := api.dal.GetTaskStep(gateStep.ID)
	if step.Status != StepStatusInProgress {
		t.Fatalf("a withdrawn card must restore the step to in_progress, got %s", step.Status)
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusInProgress {
		t.Fatalf("a withdrawn card must restore the task to in_progress, got %s", got.Status)
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
	card := openCardOnStep(t, api, task.ID, "m-exec", gateStep.ID, "go?")

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
	first := openCardOnStep(t, api, task.ID, "m-exec", view.Steps[0].ID, "one?")
	second := openCardOnStep(t, api, task.ID, "m-exec", view.Steps[1].ID, "two?")

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
	old := openCardOnStep(t, api, task.ID, "m-exec", view.Steps[0].ID, "old?")
	fresh := openCardOnStep(t, api, task.ID, "m-exec", view.Steps[0].ID, "fresh?")

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

// strandLegacyOrphanCard re-creates the PRE-T-4166 orphan on purpose: a waiting
// card bound to an already-terminal task. closeTask now sweeps such cards
// itself, so this shape is no longer reachable through any route — but rows
// minted before the fix still exist in live DBs, so the guards that catch them
// (the answer 409, the expire exit, the boot reconcile) must stay tested. Force
// the row straight through the DAL, under the lifecycle.
func strandLegacyOrphanCard(t *testing.T, api *apiServer, cardID string) ReplyCard {
	t.Helper()
	c, err := api.dal.GetReplyCard(cardID)
	if err != nil || c == nil {
		t.Fatalf("card: %v %v", c, err)
	}
	c.Status = replyCardStatusWaiting
	c.ExpiredTS = 0
	if err := api.dal.PutReplyCard(*c); err != nil {
		t.Fatalf("strand card: %v", err)
	}
	return *c
}

func TestExpiringAnOrphanCardSucceedsWithoutTouchingTheClosedTask(t *testing.T) {
	// T-f571 left orphaned cards (task already terminal) with NO exit — answer
	// is 409. Expire IS that exit: 200, the card closes, and the terminal task
	// is left byte-identical (no status change, no UpdatedTS bump that would
	// float it back up the cockpit). Since T-4166 closeTask retires these itself,
	// so the fixture is forced back to waiting through the DAL — the legacy rows
	// this guard exists for.
	for _, status := range []string{TaskStatusTerminated, TaskStatusDone} {
		t.Run(status, func(t *testing.T) {
			api := newTasksTestServer(t)
			task := createAdHocTask(t, api, "m-exec")
			view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
				{"name": "approve", "dod": "go", "is_gate": true},
			})
			startFirstStep(t, api, task.ID, "m-exec")
			card := openCardOnStep(t, api, task.ID, "m-exec", view.Steps[0].ID, "go?")
			// closeTask directly (same package) — the shared terminal helper
			// behind terminate() and the agent's done report — on both terminal
			// branches (the T-f571 test's construction).
			stored, err := api.dal.GetTask(task.ID)
			if err != nil || stored == nil {
				t.Fatalf("task: %v %v", stored, err)
			}
			if err := api.closeTask(stored, status, nowSecs(), "test"); err != nil {
				t.Fatalf("closeTask: %v", err)
			}
			strandLegacyOrphanCard(t, api, card.ID)
			before, _ := api.dal.GetTask(task.ID)

			// The orphan still cannot be ANSWERED (T-f571 unchanged)…
			if rec := answerCard(t, api, card.ID,
				map[string]any{"option_idxs": []int{0}}); rec.Code != http.StatusConflict {
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

// ── T-4166 layer 2: the lifecycle seams that must retire a card ─────────────

// TestClosingATaskRetiresItsWaitingCards pins the fix at the seam that MINTED
// the production orphans: closeTask (done AND terminated — the single terminal
// helper behind terminate(), the derived all-steps-done close, and duplicate
// marking) now expires every card still bound to the task. The closed task is
// left byte-identical, exactly as the owner's manual expire leaves it.
func TestClosingATaskRetiresItsWaitingCards(t *testing.T) {
	for _, status := range []string{TaskStatusTerminated, TaskStatusDone} {
		t.Run(status, func(t *testing.T) {
			api := newTasksTestServer(t)
			task := createAdHocTask(t, api, "m-exec")
			view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
				{"name": "approve", "dod": "go", "is_gate": true},
			})
			startFirstStep(t, api, task.ID, "m-exec")
			card := openCardOnStep(t, api, task.ID, "m-exec", view.Steps[0].ID, "go?")

			// A BYSTANDER on a different, still-live task: the sweep is scoped
			// to this task, not a purge (the dismissal seams have this sentinel;
			// closeTask must not be the asymmetric one).
			other := createAdHocTask(t, api, "m-other")
			otherView := submitPlan(t, api, other.ID, "m-other", []map[string]any{
				{"name": "work", "dod": "d"},
			})
			startFirstStep(t, api, other.ID, "m-other")
			bystander := openCardOnStep(t, api, other.ID, "m-other", otherView.Steps[0].ID, "mine?")

			stored, _ := api.dal.GetTask(task.ID)
			if err := api.closeTask(stored, status, nowSecs(), "test"); err != nil {
				t.Fatalf("closeTask: %v", err)
			}
			after, _ := api.dal.GetReplyCard(card.ID)
			if after.Status != replyCardStatusExpired || after.ExpiredTS <= 0 {
				t.Fatalf("closing the task must retire its waiting card, got %+v", after)
			}
			kept, _ := api.dal.GetReplyCard(bystander.ID)
			if kept.Status != replyCardStatusWaiting {
				t.Fatalf("another task's card must survive the close, got %s", kept.Status)
			}
			if got, _ := api.dal.GetTask(other.ID); got.Status != TaskStatusWaitingOwner {
				t.Fatalf("the bystander task must keep its hold, got %s", got.Status)
			}
			// …and the pane/red-dot clears of THIS task's card.
			cards, _ := api.dal.ListReplyCards()
			waiting := waitingReplyCards(cards)
			if len(waiting) != 1 || waiting[0].ID != bystander.ID {
				t.Fatalf("the 等我回覆 pane must shed exactly this task's card, got %+v", waiting)
			}
			got, _ := api.dal.GetTask(task.ID)
			if got.Status != status || got.UpdatedTS != stored.UpdatedTS {
				t.Fatalf("the card sweep must not re-touch the closed task: %+v vs %+v",
					got, stored)
			}
		})
	}
}

// TestTerminatingATaskOverAWaitingCardRetiresIt drives a REAL owner route
// end-to-end (POST /api/tasks/{id}/terminate) rather than calling closeTask by
// hand: the owner kills a task while a card still waits on it, and the card
// must not survive its task.
func TestTerminatingATaskOverAWaitingCardRetiresIt(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "only", "dod": "d"},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	card := openCardOnStep(t, api, task.ID, "m-exec", view.Steps[0].ID, "go?")

	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/x", map[string]any{"reason": "no longer needed"},
			"owner", "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
	}
	got, _ := api.dal.GetTask(task.ID)
	if got.Status != TaskStatusTerminated {
		t.Fatalf("the task must terminate, got %s", got.Status)
	}
	after, _ := api.dal.GetReplyCard(card.ID)
	if after.Status != replyCardStatusExpired {
		t.Fatalf("terminating the task must retire its card, got %s", after.Status)
	}
	cards, _ := api.dal.ListReplyCards()
	if n := len(waitingReplyCards(cards)); n != 0 {
		t.Fatalf("the 等我回覆 pane must clear, got %d", n)
	}
}

// TestDismissingAMemberRetiresItsWaitingCards: the asker is gone, so nobody can
// consume an answer — the card must not keep pinning the owner's red dot.
func TestDismissingAMemberRetiresItsWaitingCards(t *testing.T) {
	api := newTasksTestServer(t)
	if err := api.dal.PutMember(Member{
		ID: "m-leaver", Name: "Leaver", Kind: KindStaff,
		RoleKey: "assistant", RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	card := openPlainCard(t, api, "m-leaver")
	// A bystander's card must survive — the sweep is by MEMBER, not a purge.
	other := openPlainCard(t, api, "m-stayer")

	rec := httptest.NewRecorder()
	api.HandleDismissMemberApiMembersMemberIdDelete(rec,
		taskReq(t, "DELETE", "/x", nil, "owner", "owner"), "m-leaver")
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss: %d %s", rec.Code, rec.Body.String())
	}
	gone, _ := api.dal.GetReplyCard(card.ID)
	if gone.Status != replyCardStatusExpired || gone.ExpiredTS <= 0 {
		t.Fatalf("a dismissed member's waiting card must retire, got %+v", gone)
	}
	kept, _ := api.dal.GetReplyCard(other.ID)
	if kept.Status != replyCardStatusWaiting {
		t.Fatalf("a bystander's card must survive the dismissal, got %s", kept.Status)
	}
}

// TestSweepRefusesABlankScope pins the blank-id defence. It is not politeness:
// an empty task id matches EVERY plain unbound 請示 in the database
// (c.TaskID == ""), so one caller passing "" through would retire the lot. The
// mutant that deletes the guard survived until this test existed.
func TestSweepRefusesABlankScope(t *testing.T) {
	api := newTasksTestServer(t)
	plain := waitingCard("rc-plain", nowSecs()) // TaskID "" — the victim
	anon := waitingCard("rc-anon", nowSecs())   // FromMember set below
	anon.FromMember = ""
	for _, c := range []ReplyCard{plain, anon} {
		if err := api.dal.PutReplyCard(c); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if n, err := api.expireWaitingCardsForTask("", nowSecs(), "test"); err == nil || n != 0 {
		t.Fatalf("a blank task id must be refused, got n=%d err=%v", n, err)
	}
	if n, err := api.expireWaitingCardsFromMember("", nowSecs(), "test"); err == nil || n != 0 {
		t.Fatalf("a blank member id must be refused, got n=%d err=%v", n, err)
	}
	for _, id := range []string{plain.ID, anon.ID} {
		got, _ := api.dal.GetReplyCard(id)
		if got.Status != replyCardStatusWaiting {
			t.Fatalf("card %s must be untouched, got %s", id, got.Status)
		}
	}
}

// TestDismissingAnOutsourceWorkerRetiresItsWaitingCards covers the third
// dismissal seam (dismissOutsourceWorkerByID — the deferred handover fires the
// predecessor by worker id). Same rule as a member dismissal: the asker is
// gone, so its waiting cards can never be consumed.
func TestDismissingAnOutsourceWorkerRetiresItsWaitingCards(t *testing.T) {
	api := newTasksTestServer(t)
	now := nowSecs()
	mine := waitingCard("rc-fired", now-60)
	mine.FromMember = "ow-fired"
	bystander := waitingCard("rc-other", now-60)
	bystander.FromMember = "ow-live"
	for _, c := range []ReplyCard{mine, bystander} {
		if err := api.dal.PutReplyCard(c); err != nil {
			t.Fatalf("seed card: %v", err)
		}
	}

	api.dismissOutsourceWorkerByID("ow-fired", now, "test")

	got, _ := api.dal.GetReplyCard(mine.ID)
	if got.Status != replyCardStatusExpired || got.ExpiredTS <= 0 {
		t.Fatalf("a fired worker's waiting card must retire, got %+v", got)
	}
	kept, _ := api.dal.GetReplyCard(bystander.ID)
	if kept.Status != replyCardStatusWaiting {
		t.Fatalf("another worker's card must survive, got %s", kept.Status)
	}
}

// TestOrphanReplyCardBootReconcileRetiresStrandedCards covers the 存量: rows
// minted before the lifecycle fix. Boot retires waiting cards whose task is
// already terminal (or gone) — the ONLY way the cockpit red dot they pin ever
// clears without the owner hand-expiring each one.
func TestOrphanReplyCardBootReconcileRetiresStrandedCards(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "approve", "dod": "go", "is_gate": true},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	orphan := openCardOnStep(t, api, task.ID, "m-exec", view.Steps[0].ID, "go?")
	stored, _ := api.dal.GetTask(task.ID)
	if err := api.closeTask(stored, TaskStatusDone, nowSecs(), "test"); err != nil {
		t.Fatalf("closeTask: %v", err)
	}
	strandLegacyOrphanCard(t, api, orphan.ID)
	// A card pointing at a task row that no longer EXISTS is orphaned too —
	// nothing will ever close it, so nothing would ever take it off the pane
	// (G11: the `t == nil` half of the orphan test, which the terminal-status
	// half cannot reach).
	dangling := waitingCard("rc-dangling", nowSecs())
	dangling.TaskID = "t-vanished"
	if err := api.dal.PutReplyCard(dangling); err != nil {
		t.Fatalf("seed dangling card: %v", err)
	}
	// An ALREADY-ANSWERED card on the very same closed task must be left alone
	// — the sweep is scoped to waiting rows, and re-stamping a settled card
	// would rewrite history (G13).
	settled := answeredCard("rc-settled", nowSecs()-100, nowSecs()-50)
	settled.TaskID = task.ID
	if err := api.dal.PutReplyCard(settled); err != nil {
		t.Fatalf("seed settled card: %v", err)
	}
	// A LIVE card on a live task, and a plain unbound ask — neither is orphaned.
	live := createAdHocTask(t, api, "m-live")
	liveView := submitPlan(t, api, live.ID, "m-live", []map[string]any{
		{"name": "work", "dod": "d"},
	})
	startFirstStep(t, api, live.ID, "m-live")
	liveCard := openCardOnStep(t, api, live.ID, "m-live", liveView.Steps[0].ID, "still?")
	plain := openPlainCard(t, api, "m-free")

	n, err := api.reconcileOrphanReplyCardsOnBoot()
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if n != 2 {
		t.Fatalf("exactly the two stranded cards must retire, got %d", n)
	}
	if got, _ := api.dal.GetReplyCard(orphan.ID); got.Status != replyCardStatusExpired {
		t.Fatalf("the stranded card must retire, got %s", got.Status)
	}
	if got, _ := api.dal.GetReplyCard(dangling.ID); got.Status != replyCardStatusExpired {
		t.Fatalf("a card on a VANISHED task must retire, got %s", got.Status)
	}
	if got, _ := api.dal.GetReplyCard(settled.ID); got.Status != replyCardStatusAnswered ||
		got.ExpiredTS != 0 || got.AnsweredTS != settled.AnsweredTS {
		t.Fatalf("an answered card must be left byte-identical, got %+v", got)
	}
	if got, _ := api.dal.GetReplyCard(liveCard.ID); got.Status != replyCardStatusWaiting {
		t.Fatalf("a card on a LIVE task must survive boot, got %s", got.Status)
	}
	if got, _ := api.dal.GetReplyCard(plain.ID); got.Status != replyCardStatusWaiting {
		t.Fatalf("an unbound ask must survive boot, got %s", got.Status)
	}
	// The red dot the owner could never clear: 待回覆 drops to the two live ones.
	cards, _ := api.dal.ListReplyCards()
	if got := len(waitingReplyCards(cards)); got != 2 {
		t.Fatalf("the waiting pane must shed exactly the orphan, got %d", got)
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
			"options": []map[string]any{{"text": "A"}, {"text": "B"}}, "linked_task": nil, "attachments": attachments,
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
	if err := api.dal.rdb.QueryRow(
		`SELECT COUNT(*) FROM chat_attachment`).Scan(&blobs); err != nil {
		t.Fatalf("count blobs: %v", err)
	}
	if blobs != 1 {
		t.Fatalf("a ref must not duplicate the blob: %d rows", blobs)
	}
}

func TestCreateCardWithBadAttachmentsRejectsAtomically(t *testing.T) {
	api := newTasksTestServer(t)
	type badAttachmentCase struct {
		name    string
		atts    []map[string]any
		wantMsg string
	}
	cases := []badAttachmentCase{
		{"unknown ref", []map[string]any{{"id": "att-nope"}},
			"attachment 'att-nope' not found"},
		{"id and data_b64 together", []map[string]any{
			{"id": "att-x", "data_b64": onePixelPNGB64}},
			"attachment carries both id and data_b64"},
		{"bad base64", []map[string]any{{"data_b64": "@@not-base64@@"}},
			"attachment is not valid base64"},
		{"good sibling before a bad item", []map[string]any{
			{"data_b64": onePixelPNGB64}, {"id": "att-nope"}},
			"attachment 'att-nope' not found"},
	}
	over := make([]map[string]any, chatAttachmentsMaxCount+1)
	for i := range over {
		over[i] = map[string]any{"data_b64": onePixelPNGB64}
	}
	cases = append(cases, badAttachmentCase{
		"over the count cap", over, "a reply card may carry at most 10 attachments"})
	for _, tc := range cases {
		rec := createCardWithAttachments(t, api, "m-exec", tc.atts)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d %s", tc.name, rec.Code, rec.Body.String())
		}
		// 🔴 400 FOR THE RIGHT REASON. linked_task is required since T-18 and its
		// refusal is ALSO a 400, so the status alone cannot tell "the attachment
		// rule fired" from "we never reached it". The conformance twin of this
		// table shipped exactly that false green for one commit. Pinning the
		// WHOLE message is what keeps that distinction: the linked_task refusal
		// is a different sentence, so it cannot pass as this one.
		if msg := errorMessageOf(t, rec); msg != tc.wantMsg {
			t.Fatalf("%s: want refusal %q, got %q", tc.name, tc.wantMsg, msg)
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
	if err := api.dal.rdb.QueryRow(
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

func TestBoundCardCarriesQuestionAttachments(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	view := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "approve", "dod": "owner said go", "is_gate": true},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind": "decision", "summary": "ship it?",
		"options":     []map[string]any{{"text": "ship"}, {"text": "hold"}},
		"linked_task": map[string]any{"task_id": task.ID, "step_id": view.Steps[0].ID},
		"attachments": []map[string]any{
			{"data_b64": onePixelPNGB64, "filename": "diff.png", "mime": "image/png"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("bound card with attachment: %d %s", rec.Code, rec.Body.String())
	}
	card := decodeBody[replyCardDTO](t, rec)
	if len(card.Attachments) != 1 || card.Attachments[0].Filename != "diff.png" {
		t.Fatalf("bound card must carry the question attachment: %+v", card.Attachments)
	}
}
