package main

// Answering is governance: owner / admin only.

import (
	"database/sql"
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

	// Single ≤4: a longer road list is an unconverged question, not a choice.
	// Multi lists whatever the world holds, so a six-item ask must fit.
	replyCardMaxOptionsSingle = 4
	replyCardMaxOptionsMulti  = 20

	replyCardSelectModeSingle = "single"
	replyCardSelectModeMulti  = "multi"

	// Retention of the recent answered AND expired panes (SPEC: 近期已回覆保留一天).
	replyCardRecentWindowSecs = 24 * 60 * 60

	replyCardAnswerTextPreview = 200
)

// The payload is a hint, never the answer (spec/sse.md §2.2). The hub always
// delivers to the owner cockpit; the audience adds the initiator, whose
// ocagent handleReplyCard filters to from==self.
func (s *apiServer) publishReplyCard(c ReplyCard, trigger string) {
	s.hub.Publish("reply_card", "patch", "reply_card", wireOwnerID+"::"+c.ID,
		map[string]any{"id": c.ID, "from": c.FromMember, "status": c.Status},
		audienceMembers(c.FromMember), trigger)
}

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

func recentAnsweredReplyCards(cards []ReplyCard, now float64) []ReplyCard {
	out := []ReplyCard{}
	for _, c := range cards {
		if c.Status == replyCardStatusAnswered &&
			now-c.AnsweredTS <= replyCardRecentWindowSecs {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].AnsweredTS > out[j].AnsweredTS
	})
	return out
}

func recentExpiredReplyCards(cards []ReplyCard, now float64) []ReplyCard {
	out := []ReplyCard{}
	for _, c := range cards {
		if c.Status == replyCardStatusExpired &&
			now-c.ExpiredTS <= replyCardRecentWindowSecs {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ExpiredTS > out[j].ExpiredTS
	})
	return out
}

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

// Stored canonical (deduped, ascending, nil for none) because readers compare
// stored answers to spot a revision: a mere click-order difference was once
// read as a changed answer and swallowed a delivery.
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

func (s *apiServer) openReplyCard(
	actor string, body ReplyCardCreateDTO, t *Task, step *TaskStep, trigger string,
) (*ReplyCard, string, error) {
	taskID, taskStepID := "", ""
	if t != nil {
		taskID = t.ID
	}
	if step != nil {
		taskStepID = step.ID
	}
	if taskID != "" && taskStepID == "" {
		return nil, "", errors.New("refusing to mint a reply card bound to task '" +
			taskID + "' with no step: a step-less task binding places no 等我回覆 hold " +
			"and orphans the card when the task closes")
	}
	if taskStepID != "" && taskID == "" {
		return nil, "", errors.New("refusing to mint a reply card bound to step '" +
			taskStepID + "' with no task: the step's task is what the hold derives")
	}
	if (t == nil) != (step == nil) {
		return nil, "", errors.New("refusing to mint a reply card from a half-resolved " +
			"binding: the task and the step must be resolved together or not at all")
	}
	// The generated enum types are never Valid()-checked on decode, so this
	// closed-set check (and select_mode's below) is the only one, and it must
	// answer 400, not 422 (conformance/test_reply_cards.py pins it).
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
	if selectMode != replyCardSelectModeSingle && selectMode != replyCardSelectModeMulti {
		return nil, "select_mode must be 'single' or 'multi'", nil
	}
	options, problem := validateReplyCardOptions(body.Options, selectMode)
	if problem != "" {
		return nil, problem, nil
	}
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
		// Mirrored into the message meta: the attachment gallery and the blob
		// GC's candidate walk read message meta only; the card's own column is
		// what vetoes GC.
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
	var heldTask *Task
	if step == nil {
		if err := s.dal.PutReplyCardWithChat(card, msg, fresh); err != nil {
			return nil, "", err
		}
	} else {
		err := s.dal.inTx(func(tx *sql.Tx) error {
			cur, err := getTaskOn(tx, taskID)
			if err != nil {
				return err
			}
			if err := cardBindingTaskRefusal(cur, taskID); err != nil {
				return err
			}
			st, err := getTaskStepOn(tx, taskStepID)
			if err != nil {
				return err
			}
			if err := cardBindingStepRefusal(st, taskID, taskStepID); err != nil {
				return err
			}
			held, err := prepareStepHeldByCardOn(tx, cur, st, card.ID, now)
			if err != nil {
				return err
			}
			heldTask = held
			return putReplyCardWithChatStepAndTaskOn(tx, card, msg, fresh, *st, held)
		})
		if err != nil {
			return nil, "", err
		}
	}
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), actor)
	s.publishReplyCard(card, actor)
	if heldTask != nil {
		s.publishTask(*heldTask, trigger)
	}
	s.enqueueWebPush(webPushPayload{
		Kind: "reply_card", ChatMessageID: msg.ID, ReplyCardID: card.ID,
		Title: "OffiCraft：需要你決定", Body: "你有一張新的請示卡。",
		NeedsDecision: card.Status == replyCardStatusWaiting,
	})
	return &card, "", nil
}

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

func (s *apiServer) writeReplyCard(w http.ResponseWriter, c ReplyCard) {
	dto, err := s.replyCardDTOOf(c)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *apiServer) writeReplyCardCreateReceipt(w http.ResponseWriter, c ReplyCard) {
	writeJSON(w, http.StatusOK, replyCardCreateReceiptDTO{
		ID:            c.ID,
		ChatMessageID: c.ChatMessageID,
		CreatedTS:     c.CreatedTS,
		Attachments:   attachmentDTOsFromRefs(c.Attachments),
	})
}

func (s *apiServer) writeReplyCardTransitionReceipt(w http.ResponseWriter, c ReplyCard) {
	receipt := replyCardReceiptDTO{
		ID:     c.ID,
		Status: c.Status,
		TaskID: c.TaskID,
		StepID: c.TaskStepID,
	}
	if c.Status == replyCardStatusExpired {
		ts := c.ExpiredTS
		receipt.ExpiredTS = &ts
	}
	if c.Status == replyCardStatusAnswered {
		ts := c.AnsweredTS
		receipt.AnsweredTS = &ts
		receipt.Answer = &replyCardAnswerDTO{
			OptionIdxs:  c.AnswerOptionIdxs,
			Text:        c.AnswerText,
			Attachments: attachmentDTOsFromRefs(c.AnswerAttachments),
		}
	}
	writeJSON(w, http.StatusOK, receipt)
}

// The whole sentence, naming both legal shapes, is how an agent learns the
// contract; do not shorten it to a bare "missing parameter".
const linkedTaskRequiredMsg = "linked_task is required and has no default — say whether " +
	"this ask is about a task. Two legal shapes: send linked_task=null if it is NOT about a " +
	"task (a plain unbound 請示), or linked_task={\"task_id\": \"t-...\", \"step_id\": " +
	"\"ts-...\"} to bind the ask to the step it is about, which then holds in waiting_owner " +
	"until you are answered. The server does not infer a binding from the work you hold: a " +
	"guess that missed used to open a card with no 等我回覆 hold and tell you nothing."

const linkedTaskStepRequiredMsg = "linked_task.step_id is required: a card bound to a task " +
	"but to no step places no 等我回覆 hold, so the task would finish underneath your " +
	"question and the owner's answer would then be rejected for good. Send " +
	"linked_task={\"task_id\": \"t-...\", \"step_id\": \"ts-...\"} naming the step you " +
	"are on, or linked_task=null if this ask is not about a task."

const linkedTaskTaskRequiredMsg = "linked_task.task_id is required: name the task the step " +
	"belongs to, or send linked_task=null if this ask is not about a task."

func (s *apiServer) HandleCreateReplyCardApiReplyCardsPost(w http.ResponseWriter, r *http.Request) {
	var body ReplyCardCreateDTO
	sent, ok := decodeJSONBodyPresent(w, r, &body, "kind", "summary", "options")
	if !ok {
		return
	}
	// Presence, not nil: `linked_task: null` is a valid declaration, but the
	// decoded pointer is nil for both null and omitted.
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
			writeError(w, http.StatusForbidden, taskActorRefusal)
			return
		}
		if err := cardBindingTaskRefusal(t, taskID); err != nil {
			writeTxError(w, err)
			return
		}
		step, err = s.dal.GetTaskStep(stepID)
		if err != nil {
			internalError(w, err)
			return
		}
		if err := cardBindingStepRefusal(step, taskID, stepID); err != nil {
			writeTxError(w, err)
			return
		}
	}
	card, problem, err := s.openReplyCard(currentActor(r), body, t, step, requestTrigger(r))
	if err != nil {
		writeTxError(w, err)
		return
	}
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	s.writeReplyCardCreateReceipt(w, *card)
}

// cardBindingTaskRefusal and cardBindingStepRefusal run twice: once before the
// body is judged, and again inside the transaction that places the hold, on the
// rows as they stand at the write.
func cardBindingTaskRefusal(t *Task, taskID string) error {
	if t == nil {
		return refuseInTx(http.StatusNotFound, "task '"+taskID+"' not found")
	}
	if t.Status != TaskStatusInProgress && t.Status != TaskStatusWaitingOwner {
		return refuseInTx(http.StatusConflict,
			"a card can only bind to an in_progress or waiting_owner task (is "+t.Status+")")
	}
	return nil
}

func cardBindingStepRefusal(step *TaskStep, taskID, stepID string) error {
	if step == nil || step.TaskID != taskID {
		return refuseInTx(http.StatusNotFound, "step '"+stepID+"' not found")
	}
	if StepIsTerminal(step.Status) {
		return refuseInTx(http.StatusConflict, "step '"+stepID+"' is already "+step.Status)
	}
	return nil
}

// Owner ruling T-3f31 (卡只需要 title+決策): a light row, never the body or the
// full option list.
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

func replyCardOptionWording(c ReplyCard) []string {
	out := []string{}
	for _, i := range c.AnswerOptionIdxs {
		if i >= 0 && i < len(c.Options) {
			out = append(out, c.Options[i].Text)
		}
	}
	return out
}

// Owner ruling 2026-09-07: one shape only, the light row (no ?view=full).
// ?status=expired is also the ocagent drain's offline-expiry catch-up.
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
	if openedBy := trimmedOrEmpty(params.OpenedBy); openedBy != "" {
		kept := []ReplyCard{}
		for _, c := range pane {
			if c.FromMember == openedBy {
				kept = append(kept, c)
			}
		}
		pane = kept
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

func (s *apiServer) applyReplyCardAnswer(w http.ResponseWriter, r *http.Request, card ReplyCard, gate replyCardGate) {
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
	if card.SelectMode != replyCardSelectModeMulti && len(optionIdxs) > 1 {
		writeError(w, http.StatusBadRequest,
			"this card is single-select: option_idxs may carry at most one index")
		return
	}
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
	if len(optionIdxs) == 0 && text == "" && len(decoded) == 0 {
		writeError(w, http.StatusBadRequest,
			"answer must carry an option, text, or an attachment")
		return
	}
	// Blobs go in the same write as the card that names them: nothing reclaims
	// a blob no record references (DeleteChatInvolving walks from record refs).
	refs := []any{}
	fresh := make([]ChatAttachment, 0, len(decoded))
	for _, att := range decoded {
		fresh = append(fresh, *att)
		refs = append(refs, attachmentRef(att))
	}
	now := nowSecs()
	settled, rel, err := s.settleReplyCard(card.ID, now, fresh, func(cur *ReplyCard, getTask func(string) (*Task, error)) error {
		if err := gate(*cur, getTask); err != nil {
			return err
		}
		cur.Status = replyCardStatusAnswered
		cur.AnsweredTS = now
		cur.AnswerOptionIdxs = optionIdxs
		cur.AnswerText = text
		cur.AnswerAttachments = refs
		return nil
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	s.publishReplyCard(settled, requestTrigger(r))
	s.announceCardHoldRelease(rel, requestTrigger(r))
	s.writeReplyCardTransitionReceipt(w, settled)
}

// replyCardGate refuses a transition from the card (and, through getTask, its
// task) as they stand. Each route runs it before judging the body and again
// inside the settling transaction.
type replyCardGate func(card ReplyCard, getTask func(string) (*Task, error)) error

// settleReplyCard re-reads the card inside the transaction and hands it to
// settle, which refuses or mutates it. A card that was waiting releases its
// step's hold in the same transaction; card, step and task commit together and
// are announced by the caller after it returns — a card committed alone cannot
// heal (a retried POST 409s, a PUT skips the release) and strands the step and
// task in waiting_owner.
func (s *apiServer) settleReplyCard(
	cardID string, now float64, atts []ChatAttachment,
	settle func(cur *ReplyCard, getTask func(string) (*Task, error)) error,
) (ReplyCard, cardHoldRelease, error) {
	var settled ReplyCard
	var rel cardHoldRelease
	err := s.dal.inTx(func(tx *sql.Tx) error {
		cur, err := getReplyCardOn(tx, cardID)
		if err != nil {
			return err
		}
		if cur == nil {
			return refuseInTx(http.StatusNotFound, "reply card '"+cardID+"' not found")
		}
		wasWaiting := cur.Status == replyCardStatusWaiting
		if err := settle(cur, func(id string) (*Task, error) { return getTaskOn(tx, id) }); err != nil {
			return err
		}
		rel = cardHoldRelease{}
		if wasWaiting {
			if rel, err = planCardHoldReleaseOn(tx, *cur, now); err != nil {
				return err
			}
		}
		settled = *cur
		return putReplyCardWithStepAndTaskOn(tx, *cur, atts, rel.step, rel.task)
	})
	return settled, rel, err
}

type sqlReader interface {
	sqlRowQuerier
	sqlQuerier
}

// Computes only: settleReplyCard writes and the caller announces.
func planCardHoldReleaseOn(q sqlReader, card ReplyCard, now float64) (cardHoldRelease, error) {
	var rel cardHoldRelease
	if card.TaskID == "" {
		return rel, nil
	}
	t, err := getTaskOn(q, card.TaskID)
	if err != nil {
		return rel, err
	}
	if t != nil && TaskIsTerminal(t.Status) {
		return rel, nil
	}
	if card.TaskStepID != "" {
		step, err := getTaskStepOn(q, card.TaskStepID)
		if err != nil {
			return rel, err
		}
		if step != nil && step.Status == StepStatusWaitingOwner &&
			step.ReplyCardID == card.ID {
			step.Status = StepStatusInProgress
			rel.step = step
		}
	}
	if t == nil {
		return rel, nil
	}
	steps, err := listTaskStepsOn(q, t.ID)
	if err != nil {
		return rel, err
	}
	if rel.step != nil {
		for i := range steps {
			if steps[i].ID == rel.step.ID {
				steps[i] = *rel.step
			}
		}
	}
	rel.arrivedReadyForDone = rederiveTask(t, steps, now)
	rel.task = t
	return rel, nil
}

type cardHoldRelease struct {
	step                *TaskStep
	task                *Task
	arrivedReadyForDone bool
}

func (s *apiServer) announceCardHoldRelease(rel cardHoldRelease, trigger string) {
	if rel.task == nil {
		return
	}
	s.announceDerivedTask(*rel.task, rel.arrivedReadyForDone, trigger)
}

// The one implementation behind every server-side expiry.
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
		settled, rel, err := s.settleReplyCard(c.ID, now, nil, func(cur *ReplyCard, _ func(string) (*Task, error)) error {
			if cur.Status != replyCardStatusWaiting {
				return errReplyCardSettledMeanwhile
			}
			cur.Status = replyCardStatusExpired
			cur.ExpiredTS = now
			return nil
		})
		if errors.Is(err, errReplyCardSettledMeanwhile) {
			continue
		}
		if err != nil {
			return n, err
		}
		s.publishReplyCard(settled, trigger)
		s.announceCardHoldRelease(rel, trigger)
		n++
	}
	return n, nil
}

var errReplyCardSettledMeanwhile = errors.New("reply card no longer waiting")

func (s *apiServer) expireWaitingCardsForTask(taskID string, now float64, trigger string) (int, error) {
	if taskID == "" {
		return 0, errors.New("expireWaitingCardsForTask: blank task id")
	}
	return s.expireWaitingCards(func(c ReplyCard) bool {
		return c.TaskID == taskID
	}, now, trigger)
}

func (s *apiServer) expireWaitingCardsByAuthor(memberID string, now float64, trigger string) (int, error) {
	if memberID == "" {
		return 0, errors.New("expireWaitingCardsFromMember: blank member id")
	}
	return s.expireWaitingCards(func(c ReplyCard) bool {
		return c.FromMember == memberID
	}, now, trigger)
}

// Retires waiting cards whose task is already terminal or gone: orphans
// minted before the lifecycle sweeps existed.
func (s *apiServer) reconcileOrphanReplyCardsOnBoot() (int, error) {
	cards, err := s.dal.ListReplyCards()
	if err != nil {
		return 0, err
	}
	orphanTaskIDs := map[string]bool{}
	for _, c := range cards {
		if c.Status != replyCardStatusWaiting || c.TaskID == "" || orphanTaskIDs[c.TaskID] {
			continue
		}
		t, err := s.dal.GetTask(c.TaskID)
		if err != nil {
			return 0, err
		}
		if t == nil || TaskIsTerminal(t.Status) {
			orphanTaskIDs[c.TaskID] = true
		}
	}
	return s.expireWaitingCards(func(c ReplyCard) bool {
		return c.TaskID != "" && orphanTaskIDs[c.TaskID]
	}, nowSecs(), "boot-reconcile")
}

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
	if err := firstAnswerGate(*card, s.dal.GetTask); err != nil {
		writeTxError(w, err)
		return
	}
	s.applyReplyCardAnswer(w, r, *card, firstAnswerGate)
}

func firstAnswerGate(card ReplyCard, getTask func(string) (*Task, error)) error {
	if card.Status == replyCardStatusExpired {
		return refuseInTx(http.StatusConflict,
			"reply card '"+card.ID+"' is expired — a terminal state; the agent opens a new card if the question still matters")
	}
	if card.Status != replyCardStatusWaiting {
		return refuseInTx(http.StatusConflict,
			"reply card '"+card.ID+"' is already answered — revise it via PUT (重新決定)")
	}
	if card.TaskID == "" {
		return nil
	}
	t, err := getTask(card.TaskID)
	if err != nil {
		return err
	}
	if t != nil && TaskIsTerminal(t.Status) {
		return refuseInTx(http.StatusConflict,
			"task '"+card.TaskID+"' is already closed ("+t.Status+") — this card is orphaned and can no longer be answered")
	}
	return nil
}

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
	if err := reanswerGate(*card, nil); err != nil {
		writeTxError(w, err)
		return
	}
	s.applyReplyCardAnswer(w, r, *card, reanswerGate)
}

func reanswerGate(card ReplyCard, _ func(string) (*Task, error)) error {
	if card.Status == replyCardStatusExpired {
		return refuseInTx(http.StatusConflict,
			"reply card '"+card.ID+"' is expired — a terminal state; it cannot be re-decided")
	}
	if card.Status != replyCardStatusAnswered {
		return refuseInTx(http.StatusConflict,
			"reply card '"+card.ID+"' is not answered yet — answer it via POST")
	}
	return nil
}

const expireNotYourCardMsg = "only the card's own author (or the owner / an admin agent) may mark it expired"

// The expire route's floor is principalAgent (routes.go); this per-card author
// check (owner ruling T-1b88) is what keeps other agents out.
func (s *apiServer) callerMayExpireCard(r *http.Request, card ReplyCard) bool {
	if principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
		return true
	}
	return currentActor(r) == card.FromMember
}

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
	if !s.callerMayExpireCard(r, *card) {
		writeError(w, http.StatusForbidden, expireNotYourCardMsg)
		return
	}
	if err := expireGate(*card); err != nil {
		writeTxError(w, err)
		return
	}
	now := nowSecs()
	settled, rel, err := s.settleReplyCard(card.ID, now, nil, func(cur *ReplyCard, _ func(string) (*Task, error)) error {
		if err := expireGate(*cur); err != nil {
			return err
		}
		cur.Status = replyCardStatusExpired
		cur.ExpiredTS = now
		return nil
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	s.publishReplyCard(settled, requestTrigger(r))
	s.announceCardHoldRelease(rel, requestTrigger(r))
	s.writeReplyCardTransitionReceipt(w, settled)
}

func expireGate(card ReplyCard) error {
	if card.Status != replyCardStatusWaiting {
		return refuseInTx(http.StatusConflict,
			"reply card '"+card.ID+"' is already "+card.Status+" — only a waiting card can expire")
	}
	return nil
}
