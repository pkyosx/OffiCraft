// Skeleton generated from server/ocserverd/api_replycards.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

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
	t.Run("a POST /api/reply-cards request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/reply-cards reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/reply-cards request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestReplyCardListItemOf(t *testing.T) {
	t.Skip("TODO: replyCardListItemOf builds one LIGHT list row (T-3f31 owner ruling: 卡只需要 title+決策): summary + status/timestamps + the decision digest on an answered card (picked option index + its ORIGINAL wording, answer text truncated to a preview, attachment COUNT) — never the body or the options full text (get_reply_card serves those).")
}

func TestReplyCardOptionWording(t *testing.T) {
	t.Skip("TODO: replyCardOptionWording resolves the circled indices back to the ORIGINAL wording, one entry per index and in the same order.")
}

func TestHandleListReplyCardsApiReplyCardsGet(t *testing.T) {
	t.Run("a well-formed GET /api/reply-cards answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/reply-cards request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/reply-cards reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/reply-cards request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleReplyCardCountApiReplyCardsCountGet(t *testing.T) {
	t.Run("a well-formed GET /api/reply-cards/count answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/reply-cards/count request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/reply-cards/count reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/reply-cards/count request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleGetReplyCardApiReplyCardsCardIdGet(t *testing.T) {
	t.Run("a well-formed GET /api/reply-cards/{card_id} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/reply-cards/{card_id} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/reply-cards/{card_id} reaches this handler with card_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/reply-cards/{card_id} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
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
	t.Run("a well-formed POST /api/reply-cards/{card_id}/answer answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/reply-cards/{card_id}/answer request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/reply-cards/{card_id}/answer reaches this handler with card_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/reply-cards/{card_id}/answer request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleReanswerReplyCardApiReplyCardsCardIdAnswerPut(t *testing.T) {
	t.Run("a well-formed PUT /api/reply-cards/{card_id}/answer answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a PUT /api/reply-cards/{card_id}/answer request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to PUT /api/reply-cards/{card_id}/answer reaches this handler with card_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a PUT /api/reply-cards/{card_id}/answer request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestCallerMayExpireCard(t *testing.T) {
	t.Skip("TODO: callerMayExpireCard is the in-handler half of the T-1b88 authorization (owner 2026-08-07, card rc-3ff94b116970): admin capability may expire any card, and an ordinary agent may expire exactly the cards IT opened.")
}

func TestHandleExpireReplyCardApiReplyCardsCardIdExpirePost(t *testing.T) {
	t.Run("a well-formed POST /api/reply-cards/{card_id}/expire answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/reply-cards/{card_id}/expire request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("an authenticated machine identity answers 403 because this row requires agent", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to POST /api/reply-cards/{card_id}/expire reaches this handler with card_id bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a POST /api/reply-cards/{card_id}/expire request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}
