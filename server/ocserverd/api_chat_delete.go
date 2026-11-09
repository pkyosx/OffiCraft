package main

import "net/http"

const (
	chatDeletedTSKey = "deleted_ts"

	// Marks the system notice a delete sends, so hide_delete_notices can find it
	// without matching the wording; migration 00125 backfills the same shape.
	chatDeleteNoticeMetaKey = "delete_notice"
	chatDeleteNoticeIDKey   = "message_id"

	chatDeleteWindowSecs = 24 * 60 * 60
)

func chatDeleteGate(m ChatMessage, now float64) error {
	if m.Sender != wireOwnerID {
		return refuseInTx(http.StatusConflict, "chat message '"+m.ID+"' was sent by '"+
			m.Sender+"' — only the owner's own messages can be deleted")
	}
	if taskID, _ := m.Meta["task_id"].(string); taskID != "" {
		return refuseInTx(http.StatusConflict, "chat message '"+m.ID+"' is a message on task "+
			taskID+" — task messages cannot be deleted")
	}
	if replyCardIDFromMeta(m.Meta) != "" {
		return refuseInTx(http.StatusConflict, "chat message '"+m.ID+
			"' carries a reply card — expire the card instead; a card message cannot be deleted")
	}
	if now-m.TS >= chatDeleteWindowSecs {
		return refuseInTx(http.StatusConflict, "chat message '"+m.ID+
			"' was sent more than 24 hours ago — only messages younger than 24 hours can be deleted")
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

func (s *apiServer) HandleDeleteChatMessageApiChatMessageIdDelete(w http.ResponseWriter, r *http.Request, messageId string) {
	gate := func(now float64) (*ChatMessage, bool, error) {
		m, err := s.chatMessageByID(messageId)
		if err != nil {
			return nil, false, err
		}
		if m == nil {
			return nil, false, chatMessageNotFound(messageId)
		}
		if m.DeletedTS > 0 {
			return m, false, nil
		}
		return m, true, chatDeleteGate(*m, now)
	}
	if _, _, err := gate(nowSecs()); err != nil {
		writeTxError(w, err)
		return
	}
	var msg, notice *ChatMessage
	now := nowSecs()
	// The notice commits with the mark: a delete that landed without it would be
	// final, since a retry answers the already-deleted message and sends nothing.
	err := s.dal.inTx(func(tx *writeTx) error {
		m, pending, err := gate(now)
		if err != nil || !pending {
			msg = m
			return err
		}
		if err := markChatDeletedOn(tx, m.ID, now); err != nil {
			return err
		}
		n := chatDeleteNotice(*m, now)
		if err := putChatOn(tx, n); err != nil {
			return err
		}
		m.DeletedTS = now
		msg, notice = m, &n
		return nil
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	if notice != nil {
		trigger := requestTrigger(r)
		s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
			map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient,
				chatDeletedTSKey: msg.DeletedTS},
			audienceMembers(msg.Sender, msg.Recipient), trigger)
		s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+notice.ID,
			map[string]any{"id": notice.ID, "from": notice.Sender, "to": notice.Recipient},
			audienceMembers(notice.Sender, notice.Recipient), trigger)
	}
	dto, err := s.servedChatMessageDTO(*msg)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// The notice replies to the deleted message so ocagent prints ↩#id beside it.
func chatDeleteNotice(deleted ChatMessage, now float64) ChatMessage {
	return ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    wireSystemSender,
		Recipient: deleted.Recipient,
		Body:      chatDeleteNoticeBody(deleted.ID),
		TS:        now,
		Meta: map[string]any{
			chatReplyToMetaKey:      deleted.ID,
			chatDeleteNoticeMetaKey: map[string]any{chatDeleteNoticeIDKey: deleted.ID},
		},
	}
}

// Owner-chosen wording, delivered to members as-is: no boot document explains a
// deleted message, so this text alone must tell them what to do.
func chatDeleteNoticeBody(id string) string {
	return "Owner 刪除了訊息 #" + id + "，該則內容已作廢。若沒讀過可以直接忽略；需要原文時，用 get_chat ids=" +
		id + " 讀回。"
}
