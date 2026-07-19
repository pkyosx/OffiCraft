package main

// api_replycards.go — the reply-card (等我回覆卡) surface, M2 reply-card
// batch B1 (+ the T-1aa4 expired terminal). A card is an ask the OWNER must
// answer before the initiating agent proceeds; the state machine is
// deliberately closed:
//
//   waiting --(POST answer: the only POSITIVE close)--> answered
//   waiting --(POST expire: owner-only 標為過期, NOT an answer)--> expired
//   answered --(PUT answer: 重新決定, replace the answer)--> answered
//
// Both exits are OWNER actions — an agent still has no way to close its own
// card. answered and expired are terminal (no reopen); no generic close/skip
// exists BY CONSTRUCTION (no such route); an agent whose question was not
// settled — or whose card was expired while the question still matters —
// opens a NEW card. A card also rides the chat
// stream: create posts one ordinary chat message (initiator → owner,
// meta.reply_card_id) so the unread red dot + permanent history come free;
// the card's chat_message_id is the jump-to-origin anchor. Answer
// attachments reuse the chat attachment machinery wholesale (same decode,
// same caps, same blob store, same serve route).

import (
	"errors"
	"net/http"
	"sort"
)

const (
	replyCardKindDecision = "decision"
	replyCardKindAction   = "action"

	replyCardStatusWaiting  = "waiting"
	replyCardStatusAnswered = "answered"
	replyCardStatusExpired  = "expired"

	// replyCardMaxOptions caps the quick-reply choices (SPEC: ≤4, [0] = the
	// AI recommendation).
	replyCardMaxOptions = 4

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

// validateReplyCardOptions enforces the quick-reply contract: 1..4 non-blank
// options ("" = the violation message; trims in place).
func validateReplyCardOptions(options []string) ([]string, string) {
	if len(options) == 0 {
		return nil, "options must carry at least one choice (index 0 = the AI recommendation)"
	}
	if len(options) > replyCardMaxOptions {
		return nil, "a reply card may carry at most 4 options"
	}
	trimmed := make([]string, len(options))
	for i, opt := range options {
		trimmed[i] = trimString(opt)
		if trimmed[i] == "" {
			return nil, "options must not be blank"
		}
	}
	return trimmed, ""
}

// openReplyCard is the ONE create machinery both entry points share (the
// plain POST /api/reply-cards ask AND the M3 task-gate arming): validate the
// body, mint the card + its companion chat message (initiator → owner,
// meta.reply_card_id), store both, fan the chat + reply_card deltas.
// taskID/taskStepID are the gate linkage ("" = plain chat 請示). A validation
// violation answers (nil, problem, nil) — the caller writes the 400.
func (s *apiServer) openReplyCard(actor string, body ReplyCardCreateDTO, taskID, taskStepID string) (*ReplyCard, string, error) {
	kind := trimString(body.Kind)
	if kind != replyCardKindDecision && kind != replyCardKindAction {
		return nil, "kind must be 'decision' or 'action'", nil
	}
	summary := trimString(body.Summary)
	if summary == "" {
		return nil, "summary must not be blank", nil
	}
	options, problem := validateReplyCardOptions(body.Options)
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
	refs, err := s.storeResolvedAttachments(resolved)
	if err != nil {
		return nil, "", err
	}
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
	if err := s.dal.PutChat(msg); err != nil {
		return nil, "", err
	}
	card := ReplyCard{
		ID:            cardID,
		FromMember:    msg.Sender,
		Kind:          kind,
		Summary:       summary,
		Body:          strOrEmpty(body.Body),
		Options:       options,
		Status:        replyCardStatusWaiting,
		CreatedTS:     now,
		ChatMessageID: msg.ID,
		Attachments:   refs,
		TaskID:        taskID,
		TaskStepID:    taskStepID,
	}
	if err := s.dal.PutReplyCard(card); err != nil {
		return nil, "", err
	}
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), actor)
	s.publishReplyCard(card, actor)
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

// inferCardTaskStep implements the AUTO card→step binding (owner design
// 2026-07-14): a plain ask opened by an agent that is currently executing
// EXACTLY ONE active task binds to that task — the server records the task's
// CURRENT step at card-open time; no explicit API field exists (存量 client
// 不變, wire shape untouched). "Current" is the single in_progress step, or —
// when none is in_progress — the single waiting_owner step (a follow-up ask
// on the same held step). Anything ambiguous (0 or 2+ candidates at either
// level) degrades honestly: task-only binding when the task is clear, a plain
// unbound 請示 otherwise — never a guess.
func (s *apiServer) inferCardTaskStep(actor string) (*Task, *TaskStep, error) {
	tasks, err := s.dal.ListTasks()
	if err != nil {
		return nil, nil, err
	}
	var active []Task
	for _, t := range tasks {
		if t.ExecutorID == actor &&
			(t.Status == TaskStatusInProgress || t.Status == TaskStatusWaitingOwner) {
			active = append(active, t)
		}
	}
	if len(active) != 1 {
		return nil, nil, nil
	}
	task := active[0]
	steps, err := s.dal.ListTaskSteps(task.ID)
	if err != nil {
		return nil, nil, err
	}
	single := func(status string) *TaskStep {
		var found *TaskStep
		for i := range steps {
			if steps[i].Status != status {
				continue
			}
			if found != nil {
				return nil // 2+ candidates — ambiguous, no step binding
			}
			found = &steps[i]
		}
		return found
	}
	step := single(StepStatusInProgress)
	if step == nil {
		step = single(StepStatusWaitingOwner)
	}
	return &task, step, nil
}

// POST /api/reply-cards — open a card. The initiator is ALWAYS the verified
// JWT sub; the server mints the id, timestamps, and posts the companion chat
// message (initiator → owner) the card rides in. When the initiator is the
// executor of exactly one active task, the card AUTO-binds to that task's
// current step (inferCardTaskStep) and the step enters waiting_owner — the
// same state machine the explicit open_gate path drives (armStepWithCard).
func (s *apiServer) HandleCreateReplyCardApiReplyCardsPost(w http.ResponseWriter, r *http.Request) {
	var body ReplyCardCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "kind", "summary", "options") {
		return
	}
	task, step, err := s.inferCardTaskStep(currentActor(r))
	if err != nil {
		internalError(w, err)
		return
	}
	taskID, stepID := "", ""
	if task != nil {
		taskID = task.ID
	}
	if step != nil {
		stepID = step.ID
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
	if task != nil && step != nil {
		if err := s.armStepWithCard(task, step, card.ID, requestTrigger(r)); err != nil {
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
		option := ""
		if c.AnswerOptionIdx != nil && *c.AnswerOptionIdx >= 0 &&
			*c.AnswerOptionIdx < len(c.Options) {
			option = c.Options[*c.AnswerOptionIdx]
		}
		dto.Answer = &replyCardAnswerBriefDTO{
			OptionIdx:   c.AnswerOptionIdx,
			Option:      option,
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

// GET /api/reply-cards — the three panes as LIGHT rows (T-3f31):
// ?status=waiting (default; longest-waiting first) | ?status=answered (last
// 24h, newest answer first) | ?status=expired (last 24h keyed expired_ts,
// newest first — the ocagent drain's offline-expiry catch-up pane). ?limit=N
// (N > 0) caps the rows AFTER the pane's ordering — the pane's first N
// survive; absent / non-positive = the whole pane. Full card via
// GET /api/reply-cards/{card_id}.
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
	if body.OptionIdx != nil &&
		(*body.OptionIdx < 0 || *body.OptionIdx >= len(card.Options)) {
		writeError(w, http.StatusBadRequest, "option_idx out of range")
		return
	}
	var inputs []ChatAttachmentInputDTO
	if body.Attachments != nil {
		for _, a := range *body.Attachments {
			if strOrEmpty(a.DataB64) != "" {
				inputs = append(inputs, a)
			}
		}
	}
	if len(inputs) > chatAttachmentsMaxCount {
		writeError(w, http.StatusBadRequest,
			"an answer may carry at most 10 attachments")
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
	if body.OptionIdx == nil && text == "" && len(decoded) == 0 {
		writeError(w, http.StatusBadRequest,
			"answer must carry an option, text, or an attachment")
		return
	}
	refs := []any{}
	for _, att := range decoded {
		if err := s.dal.PutChatAttachment(*att); err != nil {
			internalError(w, err)
			return
		}
		refs = append(refs, attachmentRef(att))
	}
	card.Status = replyCardStatusAnswered
	card.AnsweredTS = nowSecs()
	card.AnswerOptionIdx = body.OptionIdx
	card.AnswerText = text
	card.AnswerAttachments = refs
	if err := s.dal.PutReplyCard(card); err != nil {
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

// POST /api/reply-cards/{card_id}/expire — mark a WAITING card EXPIRED
// (標為過期): the owner-only terminal exit that is NOT an answer. The owner is
// saying the ask went stale (懸太久、答案已不可靠) — or its task already closed
// — and declines to answer; the initiating agent decides itself whether the
// question still matters (open a FRESH card with current context) or not
// (close out / proceed). No body, no undo, no reopen. The waiting_owner hold
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
