package main

import (
	"fmt"
	"net/http"
)

func (s *apiServer) HandleGetChatPinsApiChatPinsGet(w http.ResponseWriter, r *http.Request) {
	ids, err := listChatPinsOn(s.dal.rdb)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ChatPinsDTO{MemberIds: ids})
}

func (s *apiServer) HandleUpdateChatPinsApiChatPinsPut(w http.ResponseWriter, r *http.Request) {
	var body ChatPinsUpdateDTO
	if !decodeJSONBodyRequired(w, r, &body, "member_ids") {
		return
	}
	if body.MemberIds == nil {
		writeError(w, http.StatusUnprocessableEntity, "member_ids must be a list; an empty list unpins everyone")
		return
	}
	seen := map[string]bool{}
	for _, id := range body.MemberIds {
		if seen[id] {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("member_ids lists '%s' more than once", id))
			return
		}
		seen[id] = true
	}
	var stored []string
	err := s.dal.inTx(func(tx *writeTx) error {
		for _, id := range body.MemberIds {
			m, err := getMemberOn(tx, id)
			if err != nil {
				return err
			}
			if !isPinnableMember(m) {
				return refuseInTx(http.StatusUnprocessableEntity,
					fmt.Sprintf("member_ids: '%s' is not an active staff or outsource member", id))
			}
		}
		if err := replaceChatPinsOn(tx, body.MemberIds); err != nil {
			return err
		}
		var err error
		stored, err = listChatPinsOn(tx)
		return err
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	s.hub.Publish("chat_pin", "patch", "chat_pin", wireOwnerID,
		map[string]any{"member_ids": stored}, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, ChatPinsDTO{MemberIds: stored})
}

func isPinnableMember(m *Member) bool {
	return m != nil && m.Kind != machineKind && m.RosterStatus != RosterStatusRemoved
}

// The join keeps a row whose member has since left the roster off the list
// even if a removal path ever forgets deleteChatPinOn.
func listChatPinsOn(q sqlQuerier) ([]string, error) {
	rows, err := q.Query(`
		SELECT p.member_id FROM chat_pin p
		JOIN member m ON m.id = p.member_id
		WHERE m.kind != ? AND m.roster_status != ?
		ORDER BY p.position`, machineKind, RosterStatusRemoved)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func replaceChatPinsOn(ex sqlExecer, ids []string) error {
	if _, err := ex.Exec(`DELETE FROM chat_pin`); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := ex.Exec(`INSERT INTO chat_pin (member_id, position) VALUES (?, ?)`, id, i); err != nil {
			return err
		}
	}
	return nil
}

// Every path that takes a staff or outsource member off the roster calls this in
// the same transaction, so a pin never outlives its member.
func deleteChatPinOn(ex sqlExecer, memberID string) error {
	_, err := ex.Exec(`DELETE FROM chat_pin WHERE member_id = ?`, memberID)
	return err
}
