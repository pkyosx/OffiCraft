package main

import "net/http"

const (
	chatRecalledTSMetaKey = "recalled_ts"

	chatRecallWindowSecs = 24 * 60 * 60
)

// Only the owner's own reads keep recalled messages, so the owner's thread can
// show where one was; a by-id read returns them to everyone.
func chatListingHidesRecalled(actor string) bool {
	return actor != wireOwnerID
}

func chatRecallGate(m ChatMessage, now float64) error {
	if m.Sender != wireOwnerID {
		return refuseInTx(http.StatusConflict, "chat message '"+m.ID+"' was sent by '"+
			m.Sender+"' — only the owner's own messages can be recalled")
	}
	if taskID, _ := m.Meta["task_id"].(string); taskID != "" {
		return refuseInTx(http.StatusConflict, "chat message '"+m.ID+"' is a message on task "+
			taskID+" — task messages cannot be recalled")
	}
	if replyCardIDFromMeta(m.Meta) != "" {
		return refuseInTx(http.StatusConflict, "chat message '"+m.ID+
			"' carries a reply card — expire the card instead; a card message cannot be recalled")
	}
	if now-m.TS >= chatRecallWindowSecs {
		return refuseInTx(http.StatusConflict, "chat message '"+m.ID+
			"' was sent more than 24 hours ago — only messages younger than 24 hours can be recalled")
	}
	return nil
}

func (s *apiServer) chatMessageByID(id string) (*ChatMessage, error) {
	msgs, err := s.dal.ListChatByIDs([]string{id})
	if err != nil || len(msgs) == 0 {
		return nil, err
	}
	return &msgs[0], nil
}

func chatMessageNotFound(id string) error {
	return refuseInTx(http.StatusNotFound, "chat message '"+id+"' not found")
}

func (s *apiServer) HandleRecallChatMessageApiChatMessageIdRecallPost(w http.ResponseWriter, r *http.Request, messageId string) {
	gate := func(now float64) (*ChatMessage, bool, error) {
		m, err := s.chatMessageByID(messageId)
		if err != nil {
			return nil, false, err
		}
		if m == nil {
			return nil, false, chatMessageNotFound(messageId)
		}
		if chatRecalledTS(m.Meta) > 0 {
			return m, false, nil
		}
		return m, true, chatRecallGate(*m, now)
	}
	if _, _, err := gate(nowSecs()); err != nil {
		writeTxError(w, err)
		return
	}
	var msg, notice *ChatMessage
	now := nowSecs()
	// The notice commits with the mark: a recall that landed without it would be
	// final, since a retry answers the already-recalled message and sends nothing.
	err := s.dal.inTx(func(tx *writeTx) error {
		m, pending, err := gate(now)
		if err != nil || !pending {
			msg = m
			return err
		}
		if err := markChatRecalledOn(tx, m.ID, now); err != nil {
			return err
		}
		n := chatRecallNotice(*m, now)
		if err := putChatOn(tx, n); err != nil {
			return err
		}
		if m.Meta == nil {
			m.Meta = map[string]any{}
		}
		m.Meta[chatRecalledTSMetaKey] = now
		msg, notice = m, &n
		return nil
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	if notice != nil {
		trigger := requestTrigger(r)
		for _, c := range []*ChatMessage{msg, notice} {
			s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+c.ID,
				map[string]any{"id": c.ID, "from": c.Sender, "to": c.Recipient},
				audienceMembers(c.Sender, c.Recipient), trigger)
		}
	}
	dto, err := s.servedChatMessageDTO(*msg)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// The notice replies to the recalled message so ocagent prints ↩#id beside it.
func chatRecallNotice(recalled ChatMessage, now float64) ChatMessage {
	return ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    wireSystemSender,
		Recipient: recalled.Recipient,
		Body:      chatRecallNoticeBody(recalled.ID),
		TS:        now,
		Meta:      map[string]any{chatReplyToMetaKey: recalled.ID},
	}
}

// Owner-chosen wording; seeds/system_interaction.md teaches members to read it.
func chatRecallNoticeBody(id string) string {
	return "Owner 刪除了訊息 #" + id + "，該則內容已作廢。若沒讀過可以直接忽略；需要原文時，用 get_chat ids=" +
		id + " 讀回。"
}
