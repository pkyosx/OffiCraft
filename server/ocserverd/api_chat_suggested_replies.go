package main

import (
	"fmt"
	"net/http"
	"strings"
)

func (s *apiServer) HandleGetChatSuggestedRepliesApiChatSuggestedRepliesGet(w http.ResponseWriter, r *http.Request) {
	lists, err := s.listChatSuggestedReplies()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ChatSuggestedRepliesDTO{Lists: lists})
}

func (s *apiServer) HandleUpdateChatSuggestedRepliesApiChatSuggestedRepliesMemberIdPut(w http.ResponseWriter, r *http.Request, memberID string) {
	var body ChatSuggestedRepliesUpdateDTO
	if !decodeJSONBodyRequired(w, r, &body, "replies") {
		return
	}
	replies, msg := chatSuggestedRepliesWriteRefusal(body.Replies)
	if msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}
	err := s.dal.inTx(func(tx *writeTx) error {
		m, err := getMemberOn(tx, memberID)
		if err != nil {
			return err
		}
		if !hasChatSuggestedReplies(m) {
			return refuseInTx(http.StatusUnprocessableEntity,
				fmt.Sprintf("member '%s' is not an active member", memberID))
		}
		return replaceChatSuggestedRepliesOn(tx, memberID, replies)
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	s.publishChatSuggestedReplies(requestTrigger(r))
	writeJSON(w, http.StatusOK, ChatMemberSuggestedRepliesDTO{MemberId: memberID, Replies: replies})
}

// Owner-only: no agent consumes chat_suggested_reply. The payload is null; the
// cockpit refetches GET /api/chat/suggested-replies.
func (s *apiServer) publishChatSuggestedReplies(trigger string) {
	s.hub.Publish("chat_suggested_reply", "patch", "chat_suggested_reply", wireOwnerID, nil, audienceOwnerOnly(), trigger)
}

// A blank entry is a 422 here, though the settings lists drop one silently: this
// wire's spec promises the refusal, and the cockpit editor never sends one.
func chatSuggestedRepliesWriteRefusal(replies []string) ([]string, string) {
	if replies == nil {
		return nil, "replies must be a list; an empty list removes the member's suggested replies"
	}
	for i, v := range replies {
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Sprintf("replies[%d] is blank", i)
		}
	}
	out, err := canonicalSuggestedReplies(replies)
	if err != nil {
		return nil, fmt.Sprintf("replies %v", err)
	}
	return out, ""
}

// A warden has no chat room and a removed member's room is read-only.
func hasChatSuggestedReplies(m *Member) bool {
	return m != nil && m.RosterStatus != RosterStatusRemoved && m.Kind != machineKind
}

func replaceChatSuggestedRepliesOn(tx *writeTx, memberID string, replies []string) error {
	if err := deleteChatSuggestedRepliesOn(tx, memberID); err != nil {
		return err
	}
	for i, text := range replies {
		if _, err := tx.Exec(`INSERT INTO chat_suggested_reply (member_id, position, text) VALUES (?, ?, ?)`,
			memberID, i, text); err != nil {
			return err
		}
	}
	return nil
}

func (s *apiServer) listChatSuggestedReplies() ([]ChatMemberSuggestedRepliesDTO, error) {
	rows, err := s.dal.rdb.Query(`SELECT r.member_id, r.text FROM chat_suggested_reply r
		JOIN member m ON m.id = r.member_id
		WHERE m.roster_status != ?
		ORDER BY r.member_id, r.position`, RosterStatusRemoved)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lists := []ChatMemberSuggestedRepliesDTO{}
	for rows.Next() {
		var memberID, text string
		if err := rows.Scan(&memberID, &text); err != nil {
			return nil, err
		}
		if n := len(lists); n == 0 || lists[n-1].MemberId != memberID {
			lists = append(lists, ChatMemberSuggestedRepliesDTO{MemberId: memberID, Replies: []string{}})
		}
		last := &lists[len(lists)-1]
		last.Replies = append(last.Replies, text)
	}
	return lists, rows.Err()
}

// Every path that takes a member off the roster calls this in the same
// transaction, so a removed member's sentences do not outlive it.
func deleteChatSuggestedRepliesOn(ex sqlExecer, memberID string) error {
	_, err := ex.Exec(`DELETE FROM chat_suggested_reply WHERE member_id = ?`, memberID)
	return err
}
