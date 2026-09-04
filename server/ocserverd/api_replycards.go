package main

// api_replycards.go — the reply-card (等我回覆卡) surface, M2 reply-card
// batch B1 (+ the T-1aa4 expired terminal). A card is an ask the OWNER must
// answer before the initiating agent proceeds; the state machine is
// deliberately closed:
//
//   waiting --(POST answer: the only POSITIVE close)--> answered
//   waiting --(POST expire: 標為過期 by its AUTHOR, the owner, or an admin
//             agent — NOT an answer)--> expired
//   waiting --(the SERVER sweep: expireWaitingCards)--> expired
//   answered --(PUT answer: 重新決定, replace the answer)--> answered
//
// T-6020 (owner 2026-07-26) put expire at the admin floor, so until 2026-08-07
// an agent had NO exit at all: its own stale ask could only be retired by asking
// the owner to press the button. T-1b88 (owner 2026-08-07, card
// rc-3ff94b116970) revised that for expire alone — the AUTHOR may now retire
// its own still-unanswered card (callerMayExpireCard). What did NOT change:
// ANSWERING is still governance (owner / admin only), and an already-answered
// card can no longer be EXPIRED by anyone, the owner included — a decision must
// not be overwritten by an answerless terminal. ⚠️ That is scoped to THIS verb:
// the owner may still REPLACE the answer via PUT (重新決定, line 12 above), which
// keeps the card answered. An earlier draft of this comment said "immutable for
// everyone", which the PUT route four lines up already falsifies. The third exit
// is the SERVER's, and it is
// not an owner action: when the thing a card waits on goes away (its task is
// reassigned to someone else, or lands terminal, or its asker is dismissed) the
// server retires the card itself (T-4166; reassign grew this first, closeTask
// and the dismissal seams now share it). Without it the card would sit in the
// owner's 等我回覆 pane forever, unanswerable — its answer route rejects a card
// whose task already closed. answered and expired are terminal (no reopen); no
// generic close/skip exists BY CONSTRUCTION (no such route); an agent whose
// question was not settled — or whose card was expired while the question still
// matters — opens a NEW card. A card also rides the chat
// stream: create posts one ordinary chat message (initiator → owner,
// meta.reply_card_id) so the unread red dot + permanent history come free;
// the card's chat_message_id is the jump-to-origin anchor. Answer
// attachments reuse the chat attachment machinery wholesale (same decode,
// same caps, same blob store, same serve route).

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
)

const (
	replyCardKindDecision = "decision"
	replyCardKindAction   = "action"

	replyCardStatusWaiting  = "waiting"
	replyCardStatusAnswered = "answered"
	replyCardStatusExpired  = "expired"

	// The quick-reply choice cap, and it is TWO numbers because the two
	// select_modes ask different questions (T-43). A single card asks "which
	// road", and a road list that runs past four is an un-converged question
	// dressed up as a choice — it stays at 4. A multi card asks "which of
	// these", where the list is whatever the world actually holds, so a
	// six-item ask must be expressible: 20. WHICH option the AI recommends is
	// carried by each option's own ai_pick flag — never by its position (T-40).
	replyCardMaxOptionsSingle = 4
	replyCardMaxOptionsMulti  = 20

	// The select_mode closed set (T-40): how many options the owner may
	// circle. Orthogonal to kind — kind says what the owner must DO, this says
	// how many choices the answer may carry — which is why kind (and its
	// schema CHECK) was left alone rather than grown a third value.
	replyCardSelectModeSingle = "single"
	replyCardSelectModeMulti  = "multi"

	// replyCardAnsweredWindowSecs is the recently-answered pane retention
	// (SPEC: 近期已回覆保留一天).
	replyCardAnsweredWindowSecs = 24 * 60 * 60

	// replyCardAnswerTextPreview truncates the answer text on a LIGHT list row
	// (T-3f31; the chat-preview posture — the full text is one get_reply_card
	// away).
	replyCardAnswerTextPreview = 200
)

// publishReplyCard fans one reply_card delta (create / answer / revision all
// ride op patch; spec/sse.md §2.2 — the payload is the partial {id, from,
// status} hint, never the answer).
func (s *apiServer) publishReplyCard(c ReplyCard, trigger string) {
	// A reply_card delta reaches its INITIATOR (the ocagent handleReplyCard
	// filters to from==self anyway) plus the owner cockpit (待回覆 pane) — spec §4.
	s.hub.Publish("reply_card", "patch", "reply_card", wireOwnerID+"::"+c.ID,
		map[string]any{"id": c.ID, "from": c.FromMember, "status": c.Status},
		audienceMembers(c.FromMember), trigger)
}

// waitingReplyCards projects the 待回覆 pane: status waiting, longest-waiting
// first (created ascending).
func waitingReplyCards(cards []ReplyCard) []ReplyCard {
	out := []ReplyCard{}
	for _, c := range cards {
		if c.Status == replyCardStatusWaiting {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedTS < out[j].CreatedTS
	})
	return out
}

// recentAnsweredReplyCards projects the 近期已回覆 pane: answered within the
// 24h window (keyed off the LATEST answer ts — a revision re-enters the
// window), newest answer first. Older cards drop off this pane only; the row
// and its chat message live forever.
func recentAnsweredReplyCards(cards []ReplyCard, now float64) []ReplyCard {
	out := []ReplyCard{}
	for _, c := range cards {
		if c.Status == replyCardStatusAnswered &&
			now-c.AnsweredTS <= replyCardAnsweredWindowSecs {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].AnsweredTS > out[j].AnsweredTS
	})
	return out
}

// recentExpiredReplyCards projects the recently-expired pane: expired within
// the 24h window (keyed off expired_ts — the same retention the answered pane
// holds), newest first. Older expired cards drop off this pane only; the row
// and its chat message live forever.
func recentExpiredReplyCards(cards []ReplyCard, now float64) []ReplyCard {
	out := []ReplyCard{}
	for _, c := range cards {
		if c.Status == replyCardStatusExpired &&
			now-c.ExpiredTS <= replyCardAnsweredWindowSecs {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ExpiredTS > out[j].ExpiredTS
	})
	return out
}

// validateReplyCardOptions enforces the quick-reply contract: at least one
// option, at most as many as the card's select_mode allows (single 4, multi
// 20), each with non-blank text (trimmed in place), plus the ai_pick budget
// that same select_mode allows ("" = no violation).
//
// An unrecognised select_mode is capped as single — the strict side. The
// caller rejects anything outside the closed set BEFORE reaching here, so this
// is a fallback that cannot widen the cap, never a second vocabulary.
//
// The ai_pick budget is the whole point of T-40. A `single` card may mark AT
// MOST ONE option as the AI's recommendation, because the owner can only circle
// one — two recommendations on a card that accepts one answer is a question with
// no honest reading. A `multi` card may mark any number, zero included.
func validateReplyCardOptions(options []ReplyCardOptionDTO, selectMode string) ([]ReplyCardOption, string) {
	if len(options) == 0 {
		return nil, "options must carry at least one choice"
	}
	maxOptions, modeLabel := replyCardMaxOptionsSingle, replyCardSelectModeSingle
	if selectMode == replyCardSelectModeMulti {
		maxOptions, modeLabel = replyCardMaxOptionsMulti, replyCardSelectModeMulti
	}
	if len(options) > maxOptions {
		return nil, "a " + modeLabel + "-select card may carry at most " +
			strconv.Itoa(maxOptions) + " options"
	}
	out := make([]ReplyCardOption, len(options))
	picks := 0
	for i, opt := range options {
		out[i].Text = trimString(opt.Text)
		if out[i].Text == "" {
			return nil, "options must not be blank"
		}
		out[i].AIPick = opt.AiPick != nil && *opt.AiPick
		if out[i].AIPick {
			picks++
		}
	}
	if selectMode == replyCardSelectModeSingle && picks > 1 {
		return nil, "a single-select card may mark at most one option ai_pick"
	}
	return out, ""
}

// normalizeAnswerOptionIdxs is the ONE place a circled-option list becomes its
// stored form: deduped, ascending. It exists because the owner's CLICK ORDER is
// not part of the decision — an answer of [2,0] and an answer of [0,2] say the
// same thing — and a reader that saw the raw order once mistook a re-ordered
// re-answer for a CHANGED one and swallowed a delivery. Storing the canonical
// form makes the two byte-identical, so no reader can ever draw that
// distinction again.
//
// Returns nil for an empty input: nil and [] are the same fact ("no option was
// circled") and must not be two representations of it.
func normalizeAnswerOptionIdxs(idxs []int) []int {
	if len(idxs) == 0 {
		return nil
	}
	seen := make(map[int]bool, len(idxs))
	out := make([]int, 0, len(idxs))
	for _, i := range idxs {
		if seen[i] {
			continue
		}
		seen[i] = true
		out = append(out, i)
	}
	sort.Ints(out)
	return out
}

// openReplyCard is the ONE create machinery both entry points share (the
// plain POST /api/reply-cards ask AND the M3 task-gate arming): validate the
// body, mint the card + its companion chat message (initiator → owner,
// meta.reply_card_id), store both, fan the chat + reply_card deltas.
// taskID/taskStepID are the gate linkage ("" = plain chat 請示). A validation
// violation answers (nil, problem, nil) — the caller writes the 400.
func (s *apiServer) openReplyCard(actor string, body ReplyCardCreateDTO, taskID, taskStepID string) (*ReplyCard, string, error) {
	// T-4166 STRUCTURAL INVARIANT: task binding implies step binding. A card
	// bound to a task but to no step is the orphan shape — it places no
	// waiting_owner hold (armStepWithCard needs the step), so the task runs on
	// to done underneath it and the answer route then 409s forever. Every
	// caller must resolve both or neither; enforced HERE, at the single mint,
	// so the shape is unrepresentable no matter which entry point grows next.
	// Loud on purpose: an error (500), not a silent degrade.
	if taskID != "" && taskStepID == "" {
		return nil, "", errors.New("refusing to mint a reply card bound to task '" +
			taskID + "' with no step: a step-less task binding places no 等我回覆 hold " +
			"and orphans the card when the task closes")
	}
	// string(): the generated request type now carries the enum spec/openapi.json
	// declares, so this is a named string type rather than a bare string. The
	// closed-set check below is UNCHANGED and still load-bearing — the generated
	// Valid() is never called on the decode path, so an out-of-set kind still has
	// to be rejected HERE, and still as a 400 rather than the decoder's 422
	// (conformance/test_reply_cards.py pins {"kind": "poll"} == 400).
	kind := trimString(string(body.Kind))
	if kind != replyCardKindDecision && kind != replyCardKindAction {
		return nil, "kind must be 'decision' or 'action'", nil
	}
	summary := trimString(body.Summary)
	if summary == "" {
		return nil, "summary must not be blank", nil
	}
	selectMode := replyCardSelectModeSingle
	if body.SelectMode != nil {
		selectMode = trimString(string(*body.SelectMode))
	}
	// Same posture as kind above: the generated type carries the enum the spec
	// declares, but the decode path never calls its Valid(), so the closed set
	// is checked HERE and answered as a 400 rather than the decoder's 422.
	if selectMode != replyCardSelectModeSingle && selectMode != replyCardSelectModeMulti {
		return nil, "select_mode must be 'single' or 'multi'", nil
	}
	options, problem := validateReplyCardOptions(body.Options, selectMode)
	if problem != "" {
		return nil, problem, nil
	}
	// Question-side attachments (T-5e8a) reuse the chat machinery WHOLESALE:
	// same input shape ({id} ref or inline data_b64), same caps, same
	// all-or-nothing resolve — every item validates BEFORE any blob is stored,
	// and the card + companion message are minted only after that, so a
	// rejected create leaves no orphan of any kind.
	var inputs []ChatAttachmentInputDTO
	if body.Attachments != nil {
		inputs = *body.Attachments
	}
	if len(inputs) > chatAttachmentsMaxCount {
		return nil, "a reply card may carry at most 10 attachments", nil
	}
	resolved, status, problem := s.resolveChatAttachmentInputs(inputs)
	if problem != "" {
		if status != http.StatusBadRequest {
			return nil, "", errors.New(problem)
		}
		return nil, problem, nil
	}
	refs, fresh := pendingAttachments(resolved)
	now := nowSecs()
	cardID := "rc-" + newHexID(12)
	meta := map[string]any{"reply_card_id": cardID}
	if len(refs) > 0 {
		// Stamp the SAME refs into the companion message meta: the member
		// attachment gallery scans message meta only, and the GC candidate
		// walk starts there — the card's own column is the survival veto.
		meta["attachments"] = refs
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    actor,
		Recipient: wireOwnerID,
		Body:      summary,
		TS:        now,
		Meta:      meta,
	}
	card := ReplyCard{
		ID:            cardID,
		FromMember:    msg.Sender,
		Kind:          kind,
		Summary:       summary,
		Body:          strOrEmpty(body.Body),
		Options:       options,
		SelectMode:    selectMode,
		Status:        replyCardStatusWaiting,
		CreatedTS:     now,
		ChatMessageID: msg.ID,
		Attachments:   refs,
		TaskID:        taskID,
		TaskStepID:    taskStepID,
	}
	// Blobs + companion message + card in ONE transaction: the message's
	// meta.reply_card_id names the card, so a partial write would put a
	// permanently dangling ask in the owner's stream (T-e2b2).
	if err := s.dal.PutReplyCardWithChat(card, msg, fresh); err != nil {
		return nil, "", err
	}
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), actor)
	s.publishReplyCard(card, actor)
	s.enqueueWebPush(webPushPayload{
		Kind: "reply_card", ChatID: msg.ID, ReplyCardID: card.ID,
		Title: "OffiCraft：需要你決定", Body: "你有一張新的請示卡。",
		NeedsDecision: card.Status == replyCardStatusWaiting,
	})
	return &card, "", nil
}

// replyCardDTOOf builds the served card view, resolving the task reference
// when the card was armed from a task gate (SPEC §3.6 請示 → 任務 jump); a
// plain chat 請示 serialises task: null.
func (s *apiServer) replyCardDTOOf(c ReplyCard) (replyCardDTO, error) {
	dto := newReplyCardDTO(c)
	if c.TaskID != "" {
		t, err := s.dal.GetTask(c.TaskID)
		if err != nil {
			return dto, err
		}
		if t != nil {
			dto.Task = &taskRefDTO{ID: t.ID, TypeKey: t.TypeKey, Title: t.Title}
		}
	}
	return dto, nil
}

// writeReplyCard is the common single-card response tail.
func (s *apiServer) writeReplyCard(w http.ResponseWriter, c ReplyCard) {
	dto, err := s.replyCardDTOOf(c)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// ── the ONE card-open entrance (T-18) ────────────────────────────────────────
//
// Until T-18 a card could be opened two ways — create_reply_card, which
// INFERRED its task/step binding from whatever work the caller happened to
// hold, and open_gate, which named the step explicitly. The inference is what
// this design deletes. Its failure mode was silence: an asker who never thought
// about binding got a 200 and a card with no 等我回覆 hold, so the task kept
// marching through its remaining steps into done while the agent waited, and
// the owner's eventual answer was refused 409 forever (the code called it the
// orphan factory).
//
// 🔑 WHY A REQUIRED FIELD AND NOT A WRITTEN RULE. The old default was "say
// nothing → the server guesses → if it cannot guess, something quiet happens".
// The new default is REFUSAL: say nothing and the call does not go out at all,
// and the 400 hands you both legal spellings. A rule someone has to remember
// and a parameter you cannot omit are not the same strength of guarantee — the
// second cannot be forgotten, only answered.
//
// So the field is required with NO default, and its two legal shapes are the
// two honest answers to "is this ask about a task?": null (no) and
// {task_id, step_id} (yes, this step).

// linkedTaskRequiredMsg answers an OMITTED linked_task. It names BOTH legal
// shapes on purpose: an error that only says "missing parameter" sends the
// caller back to the docs, which is the same silence in a different costume.
// ⚠️ conformance and api_replycards_test.go pin this SENTENCE, not just the
// 400 — an error message is the whole feature here, and a later "tidy-up" to a
// bare `invalid request` would quietly undo the ticket.
const linkedTaskRequiredMsg = "linked_task is required and has no default — say whether " +
	"this ask is about a task. Two legal shapes: send linked_task=null if it is NOT about a " +
	"task (a plain unbound 請示), or linked_task={\"task_id\": \"t-...\", \"step_id\": " +
	"\"ts-...\"} to bind the ask to the step it is about, which then holds in waiting_owner " +
	"until you are answered. The server does not infer a binding from the work you hold: a " +
	"guess that missed used to open a card with no 等我回覆 hold and tell you nothing."

// linkedTaskStepRequiredMsg answers the ORPHAN SHAPE — a linked_task naming a
// task but no step. T-4166 spent a whole ticket making that shape unreachable
// through the old entrance; the new entrance must not hand it back. It is a 400
// at the door rather than the mint's 500, so the caller gets a sentence it can
// act on.
const linkedTaskStepRequiredMsg = "linked_task.step_id is required: a card bound to a task " +
	"but to no step places no 等我回覆 hold, so the task would finish underneath your " +
	"question and the owner's answer would then be rejected for good. Send " +
	"linked_task={\"task_id\": \"t-...\", \"step_id\": \"ts-...\"} naming the step you " +
	"are on, or linked_task=null if this ask is not about a task."

// linkedTaskTaskRequiredMsg is the mirror: a step with no task to hold.
const linkedTaskTaskRequiredMsg = "linked_task.task_id is required: name the task the step " +
	"belongs to, or send linked_task=null if this ask is not about a task."

// POST /api/reply-cards — the ONLY way a reply card is opened. The initiator is
// ALWAYS the verified JWT sub; the server mints the id, timestamps, and posts
// the companion chat message (initiator → owner) the card rides in.
//
// linked_task is REQUIRED (see the block above). null opens a plain unbound
// 請示. {task_id, step_id} arms that step: the guards below are the ones the
// retired open_gate route carried, moved here verbatim with it — caller must
// drive the task (403), task must be in_progress|waiting_owner (409), the step
// must belong to the task (404) and must not be terminal (409) — and then the
// step (and its task) enters waiting_owner carrying the card (armStepWithCard).
// A plain non-gate step is armable too: is_gate is a plan-declared property
// (submit_plan) and arming does not rewrite it. Only a terminal step is
// refused: done (nothing waits any more) and superseded (frozen replan history
// — its bound card pointer is audit trail and must not be re-armed).
func (s *apiServer) HandleCreateReplyCardApiReplyCardsPost(w http.ResponseWriter, r *http.Request) {
	var body ReplyCardCreateDTO
	sent, ok := decodeJSONBodyPresent(w, r, &body, "kind", "summary", "options")
	if !ok {
		return
	}
	// PRESENCE, not nil-ness: `linked_task: null` is a DECLARATION (no task) and
	// must pass, while an omitted key is the refusal this ticket exists for. A
	// Go pointer folds both to nil, which is why the decoder reports the key set.
	if !sent["linked_task"] {
		writeError(w, http.StatusBadRequest, linkedTaskRequiredMsg)
		return
	}
	var t *Task
	var step *TaskStep
	taskID, stepID := "", ""
	if link := body.LinkedTask; link != nil {
		taskID = trimString(link.TaskId)
		stepID = trimString(link.StepId)
		if taskID == "" {
			writeError(w, http.StatusBadRequest, linkedTaskTaskRequiredMsg)
			return
		}
		if stepID == "" {
			writeError(w, http.StatusBadRequest, linkedTaskStepRequiredMsg)
			return
		}
		var err error
		t, err = s.resolveTask(taskID)
		if err != nil {
			writeResolveError(w, err, "task", taskID)
			return
		}
		if !s.callerMayDriveTask(r, *t) {
			writeError(w, http.StatusForbidden, executorGuardRefusal)
			return
		}
		if t.Status != TaskStatusInProgress && t.Status != TaskStatusWaitingOwner {
			writeError(w, http.StatusConflict,
				"a card can only bind to an in_progress or waiting_owner task (is "+t.Status+")")
			return
		}
		step, err = s.dal.GetTaskStep(stepID)
		if err != nil {
			internalError(w, err)
			return
		}
		if step == nil || step.TaskID != taskID {
			writeError(w, http.StatusNotFound, "step '"+stepID+"' not found")
			return
		}
		if StepIsTerminal(step.Status) {
			writeError(w, http.StatusConflict, "step '"+stepID+"' is already "+step.Status)
			return
		}
	}
	card, problem, err := s.openReplyCard(currentActor(r), body, taskID, stepID)
	if err != nil {
		internalError(w, err)
		return
	}
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	if t != nil && step != nil {
		if err := s.armStepWithCard(t, step, card.ID, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
	}
	s.writeReplyCard(w, *card)
}

// replyCardListItemOf builds one LIGHT list row (T-3f31 owner ruling: 卡只需要
// title+決策): summary + status/timestamps + the decision digest on an
// answered card (picked option index + its ORIGINAL wording, answer text
// truncated to a preview, attachment COUNT) — never the body or the options
// full text (get_reply_card serves those). The task reference resolves the
// same way the full DTO's does.
func (s *apiServer) replyCardListItemOf(c ReplyCard) (replyCardListItemDTO, error) {
	dto := replyCardListItemDTO{
		ID:        c.ID,
		From:      c.FromMember,
		Kind:      c.Kind,
		Summary:   c.Summary,
		Status:    c.Status,
		CreatedTS: c.CreatedTS,
	}
	if c.Status == replyCardStatusExpired {
		ts := c.ExpiredTS
		dto.ExpiredTS = &ts
	}
	if c.Status == replyCardStatusAnswered {
		ts := c.AnsweredTS
		dto.AnsweredTS = &ts
		text := c.AnswerText
		if len([]rune(text)) > replyCardAnswerTextPreview {
			text = string([]rune(text)[:replyCardAnswerTextPreview]) + "…"
		}
		// EVERY circled option's wording, not just the first. The light row is
		// the agent-facing contract, so a digest that reported one option of a
		// multi-select answer would tell the asker the owner chose less than it
		// did — silently, since the row would still look well-formed.
		dto.Answer = &replyCardAnswerBriefDTO{
			OptionIdxs:  c.AnswerOptionIdxs,
			Options:     replyCardOptionWording(c),
			Text:        text,
			Attachments: len(c.AnswerAttachments),
		}
	}
	if c.TaskID != "" {
		t, err := s.dal.GetTask(c.TaskID)
		if err != nil {
			return dto, err
		}
		if t != nil {
			dto.Task = &taskRefDTO{ID: t.ID, TypeKey: t.TypeKey, Title: t.Title}
		}
	}
	return dto, nil
}

// replyCardOptionWording resolves the circled indices back to the ORIGINAL
// wording, one entry per index and in the same order. An index that no longer
// addresses an option contributes nothing rather than a placeholder: the row is
// a digest for a human/agent to read, and inventing text for an unresolvable
// index would be worse than the shorter list.
func replyCardOptionWording(c ReplyCard) []string {
	out := []string{}
	for _, i := range c.AnswerOptionIdxs {
		if i >= 0 && i < len(c.Options) {
			out = append(out, c.Options[i].Text)
		}
	}
	return out
}

// The ?view projection (T-a3e4, owner-approved 2026-08-02). LIGHT is the
// default and is the ONLY thing the list_reply_cards MCP tool can ask for —
// `view` is deliberately absent from the frozen catalog (see the
// deliberatelyOffMCP entry in spec_catalog_conformance_test.go): the light row
// IS the agent-facing contract (T-3f31 owner ruling 卡只需要 title+決策), and a
// lever that pulls whole panes of full cards into an agent's context would undo
// exactly what that ticket shrank.
const (
	replyCardViewLight = "light"
	replyCardViewFull  = "full"
)

// GET /api/reply-cards — the three panes (T-3f31 LIGHT rows by default):
// ?status=waiting (default; longest-waiting first) | ?status=answered (last
// 24h, newest answer first) | ?status=expired (last 24h keyed expired_ts,
// newest first — the ocagent drain's offline-expiry catch-up pane). ?limit=N
// (N > 0) caps the rows AFTER the pane's ordering — the pane's first N
// survive; absent / non-positive = the whole pane.
//
// ?view=full (T-a3e4) serves the SAME pane, same rows, same order, as FULL
// cards — built by the SAME replyCardDTOOf that GET /api/reply-cards/{card_id}
// uses, so a full row is byte-identical to that card's own response. It exists
// because a renderer that draws the whole card (the cockpit's panes and its
// inline chat cards do) otherwise has to follow the light list with one GET per
// row: opening one pane costs one ROUND TRIP PER WAITING CARD. The win is the
// round trips, not the bytes — a full pane is very nearly the same size either
// way, so do not sell this as saving bandwidth.
//
// Absent / "light" is the historical response, unchanged to the byte. Any other
// value is a 400 naming both: silently falling back to light would restore the
// per-row fan-out with no signal, which is the cost this parameter removes.
// (This is the one place T-a3e4 departs from the ?view=list / ?fields=light
// precedents, which do fall back silently — owner was told and did not object.)
func (s *apiServer) HandleListReplyCardsApiReplyCardsGet(w http.ResponseWriter, r *http.Request, params HandleListReplyCardsApiReplyCardsGetParams) {
	status := trimmedOrEmpty(params.Status)
	if status == "" {
		status = replyCardStatusWaiting
	}
	if status != replyCardStatusWaiting && status != replyCardStatusAnswered &&
		status != replyCardStatusExpired {
		writeError(w, http.StatusBadRequest,
			"status must be 'waiting', 'answered' or 'expired'")
		return
	}
	view := trimmedOrEmpty(params.View)
	if view == "" {
		view = replyCardViewLight
	}
	if view != replyCardViewLight && view != replyCardViewFull {
		writeError(w, http.StatusBadRequest,
			"view must be 'light' or 'full'")
		return
	}
	cards, err := s.dal.ListReplyCards()
	if err != nil {
		internalError(w, err)
		return
	}
	var pane []ReplyCard
	switch status {
	case replyCardStatusWaiting:
		pane = waitingReplyCards(cards)
	case replyCardStatusExpired:
		pane = recentExpiredReplyCards(cards, nowSecs())
	default:
		pane = recentAnsweredReplyCards(cards, nowSecs())
	}
	if params.Limit != nil && *params.Limit > 0 && *params.Limit < len(pane) {
		pane = pane[:*params.Limit]
	}
	if view == replyCardViewFull {
		full := []replyCardDTO{}
		for _, c := range pane {
			dto, err := s.replyCardDTOOf(c)
			if err != nil {
				internalError(w, err)
				return
			}
			full = append(full, dto)
		}
		writeJSON(w, http.StatusOK, full)
		return
	}
	out := []replyCardListItemDTO{}
	for _, c := range pane {
		dto, err := s.replyCardListItemOf(c)
		if err != nil {
			internalError(w, err)
			return
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/reply-cards/count — the cockpit badge (waiting) plus the recently-
// answered and recently-expired (24h) counts. The badge still counts waiting
// only (SPEC: answered/expired cards never count toward the nav badge);
// `answered` + `expired` are SEPARATE signals the 等我回覆 page uses to render
// its collapsed 近期已處理 header (and hide the pane at zero) without fetching
// the lists.
func (s *apiServer) HandleReplyCardCountApiReplyCardsCountGet(w http.ResponseWriter, r *http.Request) {
	cards, err := s.dal.ListReplyCards()
	if err != nil {
		internalError(w, err)
		return
	}
	now := nowSecs()
	writeJSON(w, http.StatusOK, replyCardCountDTO{
		Waiting:  len(waitingReplyCards(cards)),
		Answered: len(recentAnsweredReplyCards(cards, now)),
		Expired:  len(recentExpiredReplyCards(cards, now)),
	})
}

// GET /api/reply-cards/{card_id} — one card in full: the agent's pull path
// after a reply_card delta (the answer rides here WITH the card context —
// summary, original option wording, attachments).
func (s *apiServer) HandleGetReplyCardApiReplyCardsCardIdGet(w http.ResponseWriter, r *http.Request, cardId string) {
	card, err := s.dal.GetReplyCard(cardId)
	if err != nil {
		internalError(w, err)
		return
	}
	if card == nil {
		writeError(w, http.StatusNotFound, "reply card '"+cardId+"' not found")
		return
	}
	s.writeReplyCard(w, *card)
}

// applyReplyCardAnswer validates + stores one answer (shared by POST answer
// and PUT re-answer — same body, same rules), stamps answered_ts, fans the
// delta and writes the card DTO. The status-precondition split is the
// caller's.
func (s *apiServer) applyReplyCardAnswer(w http.ResponseWriter, r *http.Request, card ReplyCard) {
	// The FIRST answer (POST: waiting → answered) releases the task/step hold;
	// a PUT re-answer (answered → answered) replaces the answer only — the task
	// already resumed, so it must NOT re-fire the resume.
	firstAnswer := card.Status == replyCardStatusWaiting
	var body ReplyCardAnswerPostDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	var optionIdxs []int
	if body.OptionIdxs != nil {
		optionIdxs = normalizeAnswerOptionIdxs(*body.OptionIdxs)
	}
	for _, idx := range optionIdxs {
		if idx < 0 || idx >= len(card.Options) {
			writeError(w, http.StatusBadRequest, "option_idxs out of range")
			return
		}
	}
	// A single-select card accepts one circled option, full stop. Silently
	// keeping the first would record an answer the owner did not give.
	if card.SelectMode != replyCardSelectModeMulti && len(optionIdxs) > 1 {
		writeError(w, http.StatusBadRequest,
			"this card is single-select: option_idxs may carry at most one index")
		return
	}
	// EVERY item is judged (T-e2b2, review R2): this face used to drop anything
	// without data_b64 — the ticket's founding defect surviving on a fourth
	// face, and one the owner's ruling covers by name. An item with neither id
	// nor bytes is refused exactly as on the chat faces; an item carrying ONLY
	// an {id} ref is refused with its own message, because this face has never
	// resolved refs (it decodes inline bytes only) and silently discarding one
	// is the very thing being removed. Whether the answer side should GAIN ref
	// support — server/CLAUDE.md claims answers reuse the chat mechanism
	// wholesale, which is not true today — is an owner call, asked separately.
	var inputs []ChatAttachmentInputDTO
	if body.Attachments != nil {
		inputs = *body.Attachments
	}
	if len(inputs) > chatAttachmentsMaxCount {
		writeError(w, http.StatusBadRequest,
			"an answer may carry at most 10 attachments")
		return
	}
	for _, a := range inputs {
		// Review U3: this face used to take the bytes and drop the id on the
		// floor, answering 200 — while the shared schema promises a 400 for an
		// item carrying both. Same rule here as everywhere else.
		if strOrEmpty(a.DataB64) != "" && trimmedOrEmpty(a.Id) != "" {
			writeError(w, http.StatusBadRequest,
				"attachment carries both id and data_b64")
			return
		}
		if strOrEmpty(a.DataB64) != "" {
			continue
		}
		if trimmedOrEmpty(a.Id) != "" {
			writeError(w, http.StatusBadRequest,
				"an answer attachment must carry data_b64; a stored-blob id "+
					"reference is not accepted on this face")
			return
		}
		writeError(w, http.StatusBadRequest,
			"attachment carries neither id nor data_b64")
		return
	}
	// All attachments decode/validate BEFORE any is stored (chat parity: a
	// rejected item never leaves earlier siblings as orphaned blobs).
	var decoded []*ChatAttachment
	for _, a := range inputs {
		att, err := decodeChatAttachment(
			strOrEmpty(a.DataB64), strOrEmpty(a.Filename), strOrEmpty(a.Mime))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		decoded = append(decoded, att)
	}
	text := trimmedOrEmpty(body.Text)
	// 🔴 len(), NOT a nil check. This guard used to read `body.OptionIdx == nil`
	// against a *int, where "absent" was the only way to have no option. Against
	// a LIST, `option_idxs: []` decodes to a non-nil EMPTY slice — a nil check
	// waves it through, and an answer carrying nothing at all gets stored as an
	// answer, closing the card and releasing the task hold on a decision the
	// owner never made.
	if len(optionIdxs) == 0 && text == "" && len(decoded) == 0 {
		writeError(w, http.StatusBadRequest,
			"answer must carry an option, text, or an attachment")
		return
	}
	// The answer side obeys the same all-or-nothing rule as the question side
	// (T-e2b2): blobs and the card row that names them go in ONE write. Before
	// this, a blob written here and a failing PutReplyCard left a blob no record
	// could ever name — and NOTHING reclaims it (the only cascade,
	// DeleteChatInvolving, walks from record refs).
	refs := []any{}
	fresh := make([]ChatAttachment, 0, len(decoded))
	for _, att := range decoded {
		fresh = append(fresh, *att)
		refs = append(refs, attachmentRef(att))
	}
	card.Status = replyCardStatusAnswered
	card.AnsweredTS = nowSecs()
	card.AnswerOptionIdxs = optionIdxs
	card.AnswerText = text
	card.AnswerAttachments = refs
	if err := s.dal.PutReplyCardWithAttachments(card, fresh); err != nil {
		internalError(w, err)
		return
	}
	if firstAnswer {
		if err := s.releaseCardHold(card, requestTrigger(r)); err != nil {
			internalError(w, err)
			return
		}
	}
	s.publishReplyCard(card, requestTrigger(r))
	s.writeReplyCard(w, card)
}

// releaseCardHold releases the waiting_owner HOLD a reply card placed on a
// task/step, fired when — and only when — the card leaves waiting through an
// OWNER action: the FIRST answer (POST /answer: waiting → answered) or the
// expire action (POST /expire: waiting → expired). It is the exit twin of
// armStepWithCard: the bound step returns to in_progress (the owner settled
// the ask → the step is actionable again; the agent then advances it — after
// an expiry it decides itself whether to reopen a fresh card or move on), and
// the task returns to in_progress too UNLESS another bound card still waits on
// it (SPEC §3.2 — one task, many cards) or the task never flipped in the first
// place (a parallel-group step's card leaves the task in_progress;
// armStepWithCard). This is the server-driven "答卡→回前態": the agent no
// longer self-reports the resume, so a task can never linger in waiting_owner
// behind an already-settled card. Work progress PAST in_progress stays the
// agent's to report (the surviving half of H4: the server releases the hold,
// it does not finish the work). A card orphaned on an already-terminal task
// (reachable via expire only — answer rejects orphans at the door) leaves the
// closed task untouched: nothing to resume, no UpdatedTS bump that would float
// it back up the cockpit.
func (s *apiServer) releaseCardHold(card ReplyCard, trigger string) error {
	if card.TaskID == "" {
		return nil // a plain unbound 請示 — no task hold to release
	}
	if t, err := s.dal.GetTask(card.TaskID); err != nil {
		return err
	} else if t != nil && TaskIsTerminal(t.Status) {
		return nil // orphan on a closed task — leave the terminal task alone
	}
	// Restore the bound step, but only if it STILL holds this very card in
	// waiting_owner: a later re-arm (a fresh card on the same step) or an agent
	// that already moved it wins — never clobber a newer state.
	if card.TaskStepID != "" {
		step, err := s.dal.GetTaskStep(card.TaskStepID)
		if err != nil {
			return err
		}
		if step != nil && step.Status == StepStatusWaitingOwner &&
			step.ReplyCardID == card.ID {
			step.Status = StepStatusInProgress
			if err := s.dal.PutTaskStep(*step); err != nil {
				return err
			}
		}
	}
	t, err := s.dal.GetTask(card.TaskID)
	if err != nil {
		return err
	}
	if t == nil {
		return nil
	}
	// The task status is DERIVED (T-9ca5): now that the bound step left
	// waiting_owner (above), re-project the task. If another bound card still
	// waits, the derivation keeps the task in waiting_owner (any waiting_owner
	// step → waiting_owner, SPEC §3.2 — one task, many cards); otherwise it
	// falls to the steps' honest state. The seam always fans the delta (a lane
	// resume still refreshes the cockpit even when the value is unchanged).
	return s.deriveAndPersistTask(t, nowSecs(), trigger)
}

// expireWaitingCards is the SERVER-SIDE card sweep: it applies the exact
// semantics of the expire route (status flip + expired_ts +
// releaseCardHold + delta) to every waiting card the predicate selects. It is
// the ONE implementation the three lifecycle seams share (T-4166) — the reassign
// pass that first grew it, the terminal-task close (closeTask), and member
// dismissal — so "a card outlives the thing it was waiting on" has a single
// place to be right. Returns how many cards it expired.
//
// On a task that is ALREADY terminal, releaseCardHold deliberately no-ops (it
// will not resume or re-stamp a closed task), so the sweep flips the card and
// leaves the closed task alone — exactly what a manual expire does to
// an orphan today.
func (s *apiServer) expireWaitingCards(pick func(ReplyCard) bool, now float64, trigger string) (int, error) {
	cards, err := s.dal.ListReplyCards()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, c := range cards {
		if c.Status != replyCardStatusWaiting || !pick(c) {
			continue
		}
		c.Status = replyCardStatusExpired
		c.ExpiredTS = now
		if err := s.dal.PutReplyCard(c); err != nil {
			return n, err
		}
		if err := s.releaseCardHold(c, trigger); err != nil {
			return n, err
		}
		s.publishReplyCard(c, trigger)
		n++
	}
	return n, nil
}

// expireWaitingCardsForTask sweeps every waiting card bound to one task — the
// task is being reassigned away from its asker, or has just landed terminal
// (closeTask): either way nobody is left to consume an answer, so the card must
// not keep sitting in the owner's 等我回覆 pane counting toward the 紅點 with a
// 409 as its only reward.
func (s *apiServer) expireWaitingCardsForTask(taskID string, now float64, trigger string) (int, error) {
	if taskID == "" {
		// Defence, not politeness: an empty id would match EVERY plain unbound
		// 請示 in the database (c.TaskID == "") and retire the lot.
		return 0, errors.New("expireWaitingCardsForTask: blank task id")
	}
	return s.expireWaitingCards(func(c ReplyCard) bool {
		return c.TaskID == taskID
	}, now, trigger)
}

// expireWaitingCardsFromMember sweeps every waiting card OPENED BY one member,
// fired when that member is dismissed (HandleDismissMember /
// dismissOutsourceWorkerByID): the asker is gone, so no answer can ever be
// delivered to it. Best-effort at the call sites — a dismissal must not fail
// because a card write did.
func (s *apiServer) expireWaitingCardsFromMember(memberID string, now float64, trigger string) (int, error) {
	if memberID == "" {
		return 0, errors.New("expireWaitingCardsFromMember: blank member id")
	}
	return s.expireWaitingCards(func(c ReplyCard) bool {
		return c.FromMember == memberID
	}, now, trigger)
}

// reconcileOrphanReplyCardsOnBoot retires the EXISTING orphans (T-4166 存量): a
// waiting card whose bound task is already terminal can never be answered (the
// answer route 409s it) and can never leave the owner's pane on its own, so it
// pins the cockpit red dot forever. The lifecycle fix above stops NEW ones; this
// one-shot clears the ones minted before it. Terminal tasks are left untouched
// (releaseCardHold's orphan branch). Returns the number of cards retired, for
// the boot log.
func (s *apiServer) reconcileOrphanReplyCardsOnBoot() (int, error) {
	cards, err := s.dal.ListReplyCards()
	if err != nil {
		return 0, err
	}
	orphans := map[string]bool{}
	for _, c := range cards {
		if c.Status != replyCardStatusWaiting || c.TaskID == "" || orphans[c.TaskID] {
			continue
		}
		t, err := s.dal.GetTask(c.TaskID)
		if err != nil {
			return 0, err
		}
		// A card pointing at a task row that no longer exists is orphaned too:
		// nothing will ever close it.
		if t == nil || TaskIsTerminal(t.Status) {
			orphans[c.TaskID] = true
		}
	}
	return s.expireWaitingCards(func(c ReplyCard) bool {
		return c.TaskID != "" && orphans[c.TaskID]
	}, nowSecs(), "boot-reconcile")
}

// POST /api/reply-cards/{card_id}/answer — answer a WAITING card: the only
// POSITIVE close. Any real answer — a picked option, typed text (a
// counter-question included), or an attachment — flips it to answered; an
// already-answered card is a 409 (revise via PUT); an expired card is a 409
// too (terminal — the agent opens a NEW card if the question still matters).
func (s *apiServer) HandleAnswerReplyCardApiReplyCardsCardIdAnswerPost(w http.ResponseWriter, r *http.Request, cardId string) {
	card, err := s.dal.GetReplyCard(cardId)
	if err != nil {
		internalError(w, err)
		return
	}
	if card == nil {
		writeError(w, http.StatusNotFound, "reply card '"+cardId+"' not found")
		return
	}
	if card.Status == replyCardStatusExpired {
		writeError(w, http.StatusConflict,
			"reply card '"+cardId+"' is expired — a terminal state; the agent opens a new card if the question still matters")
		return
	}
	if card.Status != replyCardStatusWaiting {
		writeError(w, http.StatusConflict,
			"reply card '"+cardId+"' is already answered — revise it via PUT (重新決定)")
		return
	}
	// T-68b7 補審(T-f571): terminate/done never closes a card still bound to
	// the task, so a card can outlive its task — orphaned on a task that is
	// done/terminated. Answering it would flip the card to answered and
	// (releaseCardHold) bump the closed task's UpdatedTS,
	// floating an already-closed task back to the cockpit's "recently
	// updated" top. Reject at the door instead: the card lifecycle no longer
	// has a live task to resume.
	if card.TaskID != "" {
		t, err := s.dal.GetTask(card.TaskID)
		if err != nil {
			internalError(w, err)
			return
		}
		if t != nil && TaskIsTerminal(t.Status) {
			writeError(w, http.StatusConflict,
				"task '"+card.TaskID+"' is already closed ("+t.Status+") — this card is orphaned and can no longer be answered")
			return
		}
	}
	s.applyReplyCardAnswer(w, r, *card)
}

// PUT /api/reply-cards/{card_id}/answer — 重新決定: replace an ANSWERED
// card's answer wholesale. Status STAYS answered (never reopens, never
// re-counts the badge); answered_ts re-stamps so the card re-enters the 24h
// recently-answered window; the agent picks the revision up off the delta.
func (s *apiServer) HandleReanswerReplyCardApiReplyCardsCardIdAnswerPut(w http.ResponseWriter, r *http.Request, cardId string) {
	card, err := s.dal.GetReplyCard(cardId)
	if err != nil {
		internalError(w, err)
		return
	}
	if card == nil {
		writeError(w, http.StatusNotFound, "reply card '"+cardId+"' not found")
		return
	}
	if card.Status == replyCardStatusExpired {
		writeError(w, http.StatusConflict,
			"reply card '"+cardId+"' is expired — a terminal state; it cannot be re-decided")
		return
	}
	if card.Status != replyCardStatusAnswered {
		writeError(w, http.StatusConflict,
			"reply card '"+cardId+"' is not answered yet — answer it via POST")
		return
	}
	s.applyReplyCardAnswer(w, r, *card)
}

// expireNotYourCardMsg is the ONE refusal text for "this card is not yours".
// It names the boundary (author, owner, admin) so a refused agent does not go
// looking for a flag: there is no bypass, and closing someone else's ask is
// still governance.
const expireNotYourCardMsg = "only the card's own author (or the owner / an admin agent) may mark it expired"

// callerMayExpireCard is the in-handler half of the T-1b88 authorization (owner
// 2026-08-07, card rc-3ff94b116970): admin capability may expire any card, and
// an ordinary agent may expire exactly the cards IT opened. The author fact is
// per-card (ReplyCard.FromMember, stamped at create time and never rewritten),
// so the route table — which can only name a principal class — cannot express
// it; the floor there is principalAgent and this is what keeps a stranger out.
// Identity comes from the verified token sub (root CLAUDE.md §14: never a
// request field), so an agent cannot claim someone else's authorship.
func (s *apiServer) callerMayExpireCard(r *http.Request, card ReplyCard) bool {
	if principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		return true
	}
	return currentActor(r) == card.FromMember
}

// POST /api/reply-cards/{card_id}/expire — mark a WAITING card EXPIRED
// (標為過期): the terminal exit that is NOT an answer, open to the card's own
// AUTHOR as well as to the owner / an admin agent (T-1b88). Whoever presses it
// is saying the ask went stale (懸太久、答案已不可靠) — or its task already
// closed — and that no answer is coming; the initiating agent decides itself
// whether the question still matters (open a FRESH card with current context) or
// not (close out / proceed). No body, no undo, no reopen. The waiting_owner hold
// releases exactly like a first answer (releaseCardHold); a card orphaned on a
// terminal task (whose answer is 409 — T-f571) finds its ONLY exit here, the
// closed task untouched. answered/expired → 409.
func (s *apiServer) HandleExpireReplyCardApiReplyCardsCardIdExpirePost(w http.ResponseWriter, r *http.Request, cardId string) {
	card, err := s.dal.GetReplyCard(cardId)
	if err != nil {
		internalError(w, err)
		return
	}
	if card == nil {
		writeError(w, http.StatusNotFound, "reply card '"+cardId+"' not found")
		return
	}
	// T-1b88: the author exception. Order matters and each rung is isolated so a
	// test can tell WHICH one refused: 404 (no such card, above) → 403 (a card
	// that is not yours) → 409 (yours, but already settled), and so a refusal
	// names the caller's ACTUAL problem instead of the first one it trips over.
	// ⚠️ NOT a confidentiality boundary — do not build on it as one: reading a
	// card is a separate, unrestricted surface (GET /api/reply-cards/{card_id}
	// and the list route are at the machine floor with NO ownership check), so
	// this order hides nothing that is not already readable by any agent.
	// PREMISE: that the read surface carries no ownership check — add one and
	// this sentence, and every copy of it, becomes false at the same moment.
	if !s.callerMayExpireCard(r, *card) {
		writeError(w, http.StatusForbidden, expireNotYourCardMsg)
		return
	}
	if card.Status != replyCardStatusWaiting {
		writeError(w, http.StatusConflict,
			"reply card '"+cardId+"' is already "+card.Status+" — only a waiting card can expire")
		return
	}
	card.Status = replyCardStatusExpired
	card.ExpiredTS = nowSecs()
	if err := s.dal.PutReplyCard(*card); err != nil {
		internalError(w, err)
		return
	}
	if err := s.releaseCardHold(*card, requestTrigger(r)); err != nil {
		internalError(w, err)
		return
	}
	s.publishReplyCard(*card, requestTrigger(r))
	s.writeReplyCard(w, *card)
}
