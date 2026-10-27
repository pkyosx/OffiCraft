package main

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

const (
	chatGroupNameMaxRunes = 40
	chatGroupsPerTabMax   = 50
)

func (s *apiServer) HandleGetChatGroupsApiChatGroupsGet(w http.ResponseWriter, r *http.Request) {
	groups, err := s.listChatGroups()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ChatGroupsDTO{Groups: groups})
}

func (s *apiServer) HandleUpdateChatGroupsApiChatGroupsPut(w http.ResponseWriter, r *http.Request) {
	var body ChatGroupsUpdateDTO
	if !decodeJSONBodyRequired(w, r, &body, "groups") {
		return
	}
	if msg := chatGroupsWriteRefusal(body.Groups); msg != "" {
		writeError(w, http.StatusUnprocessableEntity, msg)
		return
	}
	var saved []ChatGroupDTO
	err := s.dal.inTx(func(tx *writeTx) error {
		if err := s.checkChatGroupsAgainstStore(tx, body.Groups); err != nil {
			return err
		}
		if err := replaceChatGroupsOn(tx, body.Groups, nowSecs()); err != nil {
			return err
		}
		var err error
		saved, err = s.listChatGroups()
		return err
	})
	if err != nil {
		writeTxError(w, err)
		return
	}
	s.publishChatGroups(requestTrigger(r))
	writeJSON(w, http.StatusOK, ChatGroupsDTO{Groups: saved})
}

// Owner-only: no agent consumes chat_group. The payload is null; the cockpit
// refetches GET /api/chat/groups.
func (s *apiServer) publishChatGroups(trigger string) {
	s.hub.Publish("chat_group", "patch", "chat_group", wireOwnerID, nil, audienceOwnerOnly(), trigger)
}

// chatGroupsWriteRefusal checks what the request alone decides; what needs the
// store (group ids, members, task manuals) is checkChatGroupsAgainstStore.
func chatGroupsWriteRefusal(groups []ChatGroupWriteDTO) string {
	if groups == nil {
		return "groups must be a list; an empty list deletes every group"
	}
	perTab := map[ChatGroupWriteDTOTab]int{}
	names := map[ChatGroupWriteDTOTab]map[string]bool{}
	ids := map[string]bool{}
	members := map[string]bool{}
	types := map[string]bool{}
	for i, g := range groups {
		at := fmt.Sprintf("groups[%d]", i)
		if !g.Tab.Valid() {
			return at + ".tab must be 'staff' or 'outsource'"
		}
		if g.MemberIds == nil || g.TaskTypeKeys == nil {
			return at + ".member_ids and .task_type_keys must be lists"
		}
		if perTab[g.Tab]++; perTab[g.Tab] > chatGroupsPerTabMax {
			return fmt.Sprintf("the %s tab has more than %d groups", g.Tab, chatGroupsPerTabMax)
		}
		name := strings.TrimSpace(g.Name)
		if name == "" {
			return at + ".name is blank"
		}
		if utf8.RuneCountInString(name) > chatGroupNameMaxRunes {
			return fmt.Sprintf("%s.name is longer than %d characters", at, chatGroupNameMaxRunes)
		}
		if names[g.Tab] == nil {
			names[g.Tab] = map[string]bool{}
		}
		if names[g.Tab][name] {
			return fmt.Sprintf("the %s tab has two groups named '%s'", g.Tab, name)
		}
		names[g.Tab][name] = true
		if g.Id != nil {
			if ids[*g.Id] {
				return fmt.Sprintf("group '%s' is listed more than once", *g.Id)
			}
			ids[*g.Id] = true
		}
		for _, id := range g.MemberIds {
			if members[id] {
				return fmt.Sprintf("member '%s' is listed more than once", id)
			}
			members[id] = true
		}
		if g.Tab == ChatGroupWriteDTOTabStaff && len(g.TaskTypeKeys) > 0 {
			return at + " is a staff group; task_type_keys must be empty"
		}
		for _, key := range g.TaskTypeKeys {
			if types[key] {
				return fmt.Sprintf("task type '%s' is listed more than once", key)
			}
			types[key] = true
		}
	}
	return ""
}

func (s *apiServer) checkChatGroupsAgainstStore(tx *writeTx, groups []ChatGroupWriteDTO) error {
	stored, err := chatGroupTabsOn(tx)
	if err != nil {
		return err
	}
	manuals, err := s.chatGroupManualKeys()
	if err != nil {
		return err
	}
	for _, g := range groups {
		if g.Id != nil {
			tab, ok := stored[*g.Id]
			if !ok {
				return refuseInTx(http.StatusUnprocessableEntity, fmt.Sprintf("group '%s' does not exist", *g.Id))
			}
			if tab != string(g.Tab) {
				return refuseInTx(http.StatusUnprocessableEntity,
					fmt.Sprintf("group '%s' is on the %s tab and cannot move to the %s tab", *g.Id, tab, g.Tab))
			}
		}
		for _, id := range g.MemberIds {
			m, err := getMemberOn(tx, id)
			if err != nil {
				return err
			}
			if !belongsOnChatGroupTab(m, g.Tab) {
				return refuseInTx(http.StatusUnprocessableEntity,
					fmt.Sprintf("member '%s' is not an active member of the %s tab", id, g.Tab))
			}
		}
		for _, key := range g.TaskTypeKeys {
			if !manuals[key] {
				return refuseInTx(http.StatusUnprocessableEntity, fmt.Sprintf("task type '%s' names no task manual", key))
			}
		}
	}
	return nil
}

var chatGroupTabKind = map[ChatGroupWriteDTOTab]string{
	ChatGroupWriteDTOTabStaff:     KindStaff,
	ChatGroupWriteDTOTabOutsource: KindOutsource,
}

// A warden matches neither tab's kind.
func belongsOnChatGroupTab(m *Member, tab ChatGroupWriteDTOTab) bool {
	return m != nil && m.RosterStatus != RosterStatusRemoved && m.Kind == chatGroupTabKind[tab]
}

func (s *apiServer) chatGroupManualKeys() (map[string]bool, error) {
	manuals, err := s.foldTaskManuals()
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(manuals))
	for _, m := range manuals {
		keys[m.TypeKey] = true
	}
	return keys, nil
}

func chatGroupTabsOn(q sqlQuerier) (map[string]string, error) {
	rows, err := q.Query(`SELECT id, tab FROM chat_group`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tabs := map[string]string{}
	for rows.Next() {
		var id, tab string
		if err := rows.Scan(&id, &tab); err != nil {
			return nil, err
		}
		tabs[id] = tab
	}
	return tabs, rows.Err()
}

func replaceChatGroupsOn(tx *writeTx, groups []ChatGroupWriteDTO, now float64) error {
	created := map[string]float64{}
	rows, err := tx.Query(`SELECT id, created_ts FROM chat_group`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var ts float64
		if err := rows.Scan(&id, &ts); err != nil {
			rows.Close()
			return err
		}
		created[id] = ts
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, table := range []string{"chat_group_member", "chat_group_task_type", "chat_group"} {
		if _, err := tx.Exec(`DELETE FROM ` + table); err != nil {
			return err
		}
	}
	position := map[ChatGroupWriteDTOTab]int{}
	for _, g := range groups {
		id, ts := "cg-"+newHexID(12), now
		if g.Id != nil {
			id, ts = *g.Id, created[*g.Id]
		}
		if _, err := tx.Exec(`INSERT INTO chat_group (id, tab, name, position, created_ts) VALUES (?, ?, ?, ?, ?)`,
			id, string(g.Tab), strings.TrimSpace(g.Name), position[g.Tab], ts); err != nil {
			return err
		}
		position[g.Tab]++
		for i, member := range g.MemberIds {
			if _, err := tx.Exec(`INSERT INTO chat_group_member (member_id, group_id, position) VALUES (?, ?, ?)`,
				member, id, i); err != nil {
				return err
			}
		}
		for i, key := range g.TaskTypeKeys {
			if _, err := tx.Exec(`INSERT INTO chat_group_task_type (type_key, group_id, position) VALUES (?, ?, ?)`,
				key, id, i); err != nil {
				return err
			}
		}
	}
	return nil
}

// listChatGroups answers the stored groups with members who left the roster and
// task types whose manual is gone left out, so a cockpit that echoes GET back on
// its next PUT never trips the PUT's own checks.
func (s *apiServer) listChatGroups() ([]ChatGroupDTO, error) {
	manuals, err := s.chatGroupManualKeys()
	if err != nil {
		return nil, err
	}
	groups := []ChatGroupDTO{}
	index := map[string]int{}
	rows, err := s.dal.rdb.Query(`SELECT id, tab, name FROM chat_group
		ORDER BY CASE tab WHEN ? THEN 0 ELSE 1 END, position`, string(ChatGroupDTOTabStaff))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var g ChatGroupDTO
		if err := rows.Scan(&g.Id, &g.Tab, &g.Name); err != nil {
			return nil, err
		}
		g.MemberIds, g.TaskTypeKeys = []string{}, []string{}
		index[g.Id] = len(groups)
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	members, err := s.dal.rdb.Query(`SELECT gm.group_id, gm.member_id FROM chat_group_member gm
		JOIN member m ON m.id = gm.member_id
		WHERE m.roster_status != ?
		ORDER BY gm.position`, RosterStatusRemoved)
	if err != nil {
		return nil, err
	}
	defer members.Close()
	for members.Next() {
		var groupID, memberID string
		if err := members.Scan(&groupID, &memberID); err != nil {
			return nil, err
		}
		if i, ok := index[groupID]; ok {
			groups[i].MemberIds = append(groups[i].MemberIds, memberID)
		}
	}
	if err := members.Err(); err != nil {
		return nil, err
	}
	types, err := s.dal.rdb.Query(`SELECT group_id, type_key FROM chat_group_task_type ORDER BY position`)
	if err != nil {
		return nil, err
	}
	defer types.Close()
	for types.Next() {
		var groupID, key string
		if err := types.Scan(&groupID, &key); err != nil {
			return nil, err
		}
		if i, ok := index[groupID]; ok && manuals[key] {
			groups[i].TaskTypeKeys = append(groups[i].TaskTypeKeys, key)
		}
	}
	return groups, types.Err()
}

// joinChatGroupByTaskTypeOn appends a newly assigned worker to the group whose
// auto-join rule names its task's type, and answers whether it did.
func joinChatGroupByTaskTypeOn(tx *writeTx, workerID, typeKey string) (bool, error) {
	res, err := tx.Exec(`
		INSERT INTO chat_group_member (member_id, group_id, position)
		SELECT ?, t.group_id, COALESCE(
			(SELECT MAX(position) + 1 FROM chat_group_member WHERE group_id = t.group_id), 0)
		FROM chat_group_task_type t WHERE t.type_key = ?`, workerID, typeKey)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Every path that takes a staff or outsource member off the roster calls this in
// the same transaction, so a removed member never sits in a group.
func deleteChatGroupMemberOn(ex sqlExecer, memberID string) error {
	_, err := ex.Exec(`DELETE FROM chat_group_member WHERE member_id = ?`, memberID)
	return err
}
