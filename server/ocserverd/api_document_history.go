package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

var errDocumentHistoryCap = errors.New("restoring this version would violate the existing document size limit")

// Naming both replacements is the whole point of refusing loudly: a caller who
// still says "task_manual" learns which of the two series it wanted.
const legacyTaskManualKindMsg = "document history kind \"task_manual\" was retired: " +
	"use \"task_manual_sop\" or \"task_manual_learnings\""

func historyKeyParts(kind, key string) (string, string, bool) {
	if kind != "lessons" {
		return key, "", key != ""
	}
	parts := strings.SplitN(key, "::", 2)
	return parts[0], func() string {
		if len(parts) == 2 {
			return parts[1]
		}
		return ""
	}(), len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func documentHistoryDTO(h DocumentHistory) (DocumentHistoryDTO, error) {
	content := map[string]string{}
	if err := json.Unmarshal([]byte(h.ContentJSON), &content); err != nil {
		return DocumentHistoryDTO{}, err
	}
	return DocumentHistoryDTO{Id: h.ID, Content: content, CreatedTs: h.CreatedTS, ActorId: h.ActorID}, nil
}

// Overlay documents must retain their persisted tombstone state, not only the
// folded text exposed to readers. A tombstone means "follow the seed"; writing
// that same text back as a live overlay would silently turn a default document
// into a customized one.
func historyTombstoned(content map[string]string) bool {
	value, _ := strconv.ParseBool(content["tombstoned"])
	return value
}

func userContextHistorySnapshot(current *UserContext) (string, error) {
	if current == nil {
		return "{}", nil
	}
	return historyJSON(map[string]string{
		"text": current.Text, "tombstoned": strconv.FormatBool(current.Tombstoned),
	})
}

func roleDefHistorySnapshot(current *RoleDef) (string, error) {
	if current == nil {
		return "{}", nil
	}
	// The role's NAME is deliberately absent (owner ruling, T-1f39: 「名稱不用留
	// 版本」— 角色誌本身不說明它自己叫什麼，只說明它做什麼). It is a label on the
	// document, not part of it: a rename retains nothing, and a restore leaves
	// the current name standing rather than silently renaming the role behind
	// a reader who came to put the TEXT back.
	return historyJSON(map[string]string{
		"definition_md": current.DefinitionMD,
		"tombstoned":    strconv.FormatBool(current.Tombstoned),
	})
}

func lessonsHistorySnapshot(current *Lessons) (string, error) {
	if current == nil {
		return "{}", nil
	}
	return historyJSON(map[string]string{
		"text": current.Text, "tombstoned": strconv.FormatBool(current.Tombstoned),
	})
}

// The four readers below are what SaveWithDocumentHistory calls from inside the
// write transaction. They deliberately re-read the document rather than trust a
// value the handler folded earlier: the retained revision must be the state
// this write replaced, otherwise two writers racing on one document both retain
// the same ancestor and the revision written in between becomes unrecoverable.
func userContextSnapshotIn(q sqlQuerier) (string, error) {
	current, err := getUserContextOn(q)
	if err != nil {
		return "", err
	}
	return userContextHistorySnapshot(current)
}

func roleDefSnapshotIn(roleKey string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
		current, err := getRoleDefOn(q, roleKey)
		if err != nil {
			return "", err
		}
		return roleDefHistorySnapshot(current)
	}
}

func lessonsSnapshotIn(roleKey, taskType string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
		current, err := getLessonsOn(q, roleKey, taskType)
		if err != nil {
			return "", err
		}
		return lessonsHistorySnapshot(current)
	}
}

func manualSnapshotIn(typeKey string, of func(TaskManual) (string, error)) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
		current, err := getTaskManualOn(q, typeKey)
		if err != nil {
			return "", err
		}
		if current == nil {
			return "{}", nil
		}
		return of(*current)
	}
}

// taskManualHistoryStreams names the series a manual write must retain. SOP and
// learnings are versioned INDEPENDENTLY and only when the write actually
// changes them; purpose, the identifier fields, display_name and assignee are
// not versioned at all (owner ruling, T-1f39), so a write touching only those
// returns no streams and retains nothing anywhere.
func taskManualHistoryStreams(typeKey, actor string, sopChanged, learningsChanged bool) []documentHistoryStream {
	var streams []documentHistoryStream
	if sopChanged {
		streams = append(streams, documentHistoryStream{
			Kind: docKindTaskManualSop, Key: typeKey, ActorID: actor,
			Snapshot: manualSnapshotIn(typeKey, taskManualSopHistorySnapshot),
		})
	}
	if learningsChanged {
		streams = append(streams, documentHistoryStream{
			Kind: docKindTaskManualLearnings, Key: typeKey, ActorID: actor,
			Snapshot: manualSnapshotIn(typeKey, taskManualLearningsHistorySnapshot),
		})
	}
	return streams
}

// roleDefHistoryStreams is the role's counterpart of taskManualHistoryStreams:
// the ONE series a role write may retain, and only when the definition text
// itself changed. A rename touches no versioned field, so it returns nothing
// and the write retains nothing anywhere.
func roleDefHistoryStreams(roleKey, actor string, definitionChanged bool) []documentHistoryStream {
	if !definitionChanged {
		return nil
	}
	return []documentHistoryStream{{
		Kind: "role_definition", Key: roleKey, ActorID: actor,
		Snapshot: roleDefSnapshotIn(roleKey),
	}}
}

func (s *apiServer) documentHistoryAllowed(w http.ResponseWriter, r *http.Request, kind, key string, write bool) bool {
	primary, _, valid := historyKeyParts(kind, key)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid document history key")
		return false
	}
	switch kind {
	case "global_context", "role_definition":
		if write && !principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
			writeError(w, http.StatusForbidden, "restoring this document requires admin capability")
			return false
		}
	case "lessons":
		if write && !s.lessonsWriteAuthz(w, r, primary) {
			return false
		}
	case "insight":
		// Same posture as lessons — and the `write &&` is the point, not a
		// copy-paste: reading any role's retained insight versions is open to
		// every authenticated caller, exactly like reading the current doc
		// (owner ruling rc-dc171587220c). An earlier draft of this design said
		// the opposite; the ruling settled it.
		if write && !s.insightWriteAuthz(w, r, primary) {
			return false
		}
	case docKindTaskManualSop, docKindTaskManualLearnings:
	case docKindTaskManual:
		// The legacy four-field bundle. Its rows were deleted by migration 00045
		// (owner ruling, T-1f39), so the kind names nothing at all — an empty
		// list here would be indistinguishable from "this manual has no history".
		writeError(w, http.StatusBadRequest, legacyTaskManualKindMsg)
		return false
	default:
		writeError(w, http.StatusBadRequest, "unknown document history kind")
		return false
	}
	return true
}

func (s *apiServer) HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(w http.ResponseWriter, r *http.Request, kind string, key string) {
	if !s.documentHistoryAllowed(w, r, kind, key, false) {
		return
	}
	history, err := s.dal.ListDocumentHistory(kind, key)
	if err != nil {
		internalError(w, err)
		return
	}
	result := make([]DocumentHistoryDTO, 0, len(history))
	for _, h := range history {
		dto, err := documentHistoryDTO(h)
		if err != nil {
			internalError(w, err)
			return
		}
		result = append(result, dto)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *apiServer) HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(w http.ResponseWriter, r *http.Request, kind string, key string, id int64) {
	if !s.documentHistoryAllowed(w, r, kind, key, true) {
		return
	}
	history, err := s.dal.GetDocumentHistory(kind, key, id)
	if err != nil {
		internalError(w, err)
		return
	}
	if history == nil {
		writeError(w, http.StatusNotFound, "document history version not found")
		return
	}
	content := map[string]string{}
	if err := json.Unmarshal([]byte(history.ContentJSON), &content); err != nil {
		internalError(w, err)
		return
	}
	if err := s.restoreDocumentHistory(r, kind, key, content); err != nil {
		if errors.Is(err, errDocumentHistoryCap) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		internalError(w, err)
		return
	}
	s.publishDocumentHistoryRestore(r, kind, key)
	dto, err := documentHistoryDTO(*history)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *apiServer) publishDocumentHistoryRestore(r *http.Request, kind, key string) {
	switch kind {
	case "global_context":
		s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil, audienceOwnerOnly(), requestTrigger(r))
	case "role_definition":
		// "role_def", the topic every other write to this document publishes.
		// "role" is not in the closed topic set, so it was dropped at the
		// publish seam and a restore fanned nothing at all.
		s.hub.Publish("role_def", "patch", "role_def", wireOwnerID+"::"+key, nil, audienceOwnerOnly(), requestTrigger(r))
	case "lessons":
		s.hub.Publish("lessons", "patch", "lessons", wireOwnerID+"::"+key, nil, audienceOwnerOnly(), requestTrigger(r))
	case "insight":
		// 🔴 THE SILENT ONE. This switch has no default: omitting a kind here
		// costs nothing visible — the restore succeeds, the DB is changed, the
		// DTO comes back, HTTP 200, no error, no failing test — and the only
		// symptom is that every other surface keeps showing the old text until
		// someone reloads by hand. role_definition already made this mistake
		// once (see the case above). api_document_history_insight_publish_test.go
		// exists solely because nothing else in the build would go red here.
		s.hub.Publish("insight", "patch", "insight", wireOwnerID+"::"+key, nil, audienceOwnerOnly(), requestTrigger(r))
	case docKindTaskManualSop, docKindTaskManualLearnings:
		s.publishTaskManual(key, requestTrigger(r))
	}
}

func (s *apiServer) restoreDocumentHistory(r *http.Request, kind, key string, content map[string]string) error {
	actor := currentActor(r)
	switch kind {
	case "global_context":
		return s.dal.SaveWithDocumentHistory(kind, key, actor, userContextSnapshotIn, func(ex sqlExecer) error {
			return putUserContextOn(ex, UserContext{Text: content["text"], Tombstoned: historyTombstoned(content)})
		})
	case "role_definition":
		folded, err := s.foldRoleDefDTO(key)
		if err != nil {
			return err
		}
		if folded == nil {
			return errNotFound
		}
		// The CURRENT name stands: it is not versioned, so a revision has no
		// name to put back (older rows may still carry one — it is ignored on
		// purpose rather than resurrected).
		name := folded.Name
		return s.dal.SaveWithDocumentHistory(kind, key, actor, roleDefSnapshotIn(key), func(ex sqlExecer) error {
			return putRoleDefOn(ex, RoleDef{RoleKey: key, Name: name, DefinitionMD: content["definition_md"], Tombstoned: historyTombstoned(content)})
		})
	case "lessons":
		roleKey, taskType, _ := historyKeyParts(kind, key)
		current, err := s.foldLessonsDTO(roleKey, taskType)
		if err != nil {
			return err
		}
		if DocCapBlocked(s.docCap(), current.Text, content["text"]) {
			return errDocumentHistoryCap
		}
		return s.dal.SaveWithDocumentHistory(kind, key, actor, lessonsSnapshotIn(roleKey, taskType), func(ex sqlExecer) error {
			return putLessonsOn(ex, Lessons{RoleKey: roleKey, TaskType: taskType, Text: content["text"], Tombstoned: historyTombstoned(content)})
		})
	case "insight":
		// The key is the BARE role_key — insight has no task_type axis, so
		// there is nothing to split out of it the way lessons does above.
		current, err := s.foldInsightDTO(key)
		if err != nil {
			return err
		}
		// The cap applies to a restore too: an older, larger revision is still
		// a write, and letting history walk a doc back over the limit would
		// make the cap a suggestion.
		if DocCapBlocked(s.docCap(), current.Text, content["text"]) {
			return errDocumentHistoryCap
		}
		return s.dal.SaveWithDocumentHistory(kind, key, actor, insightSnapshotIn(key), func(ex sqlExecer) error {
			return putInsightOn(ex, Insight{RoleKey: key, Text: content["text"], Tombstoned: historyTombstoned(content)})
		})
	case docKindTaskManualSop:
		return s.restoreTaskManualField(key, taskManualHistoryStreams(key, actor, true, false),
			func(m *TaskManual) error {
				if DocCapBlocked(s.docCap(), m.SopMD, content["sop_md"]) {
					return errDocumentHistoryCap
				}
				m.SopMD = content["sop_md"]
				return nil
			})
	case docKindTaskManualLearnings:
		return s.restoreTaskManualField(key, taskManualHistoryStreams(key, actor, false, true),
			func(m *TaskManual) error {
				if DocCapBlocked(s.docCap(), m.Learnings, content["learnings"]) {
					return errDocumentHistoryCap
				}
				m.Learnings = content["learnings"]
				return nil
			})
	}
	return errNotFound
}

// restoreTaskManualField writes back exactly the one field its stream versions
// and leaves every other field of the manual as it stands. apply also judges
// the cap, on THAT field alone: restoring a SOP has nothing to do with how long
// the current learnings doc is, and before the split (T-1f39) an over-cap
// learnings doc blocked the SOP restore too.
func (s *apiServer) restoreTaskManualField(key string, streams []documentHistoryStream, apply func(*TaskManual) error) error {
	current, err := s.dal.GetTaskManual(key)
	if err != nil {
		return err
	}
	if current == nil {
		return errNotFound
	}
	next := *current
	if err := apply(&next); err != nil {
		return err
	}
	next.UpdatedTS = nowSecs()
	return s.dal.SaveWithDocumentHistories(streams, func(ex sqlExecer) error {
		return putTaskManualOn(ex, next)
	})
}
