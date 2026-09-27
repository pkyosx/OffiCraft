package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"unicode/utf8"
)

var errDocumentHistoryCap = errors.New("restoring this version would violate the existing document size limit")

const legacyTaskManualKindMsg = "document history kind \"task_manual\" was retired: " +
	"use \"task_manual_sop\""

// Refused BY NAME: the storage is gone, and letting these kinds reach a list would
// answer an empty 200 indistinguishable from "no versions yet". The message
// deliberately names no migration number (a rebase could renumber it silently).
const legacyMemoryKindsMsg = "document history kinds \"lessons\" and " +
	"\"task_manual_learnings\" were retired: the legacy memory documents and " +
	"their retained revisions were dropped from the database, so there is " +
	"nothing left to list or restore"

func historyKeyParts(kind, key string) (string, bool) {
	return key, key != ""
}

func documentHistoryContent(h DocumentHistory) (map[string]string, error) {
	content := map[string]string{}
	if err := json.Unmarshal([]byte(h.ContentJSON), &content); err != nil {
		return nil, err
	}
	return content, nil
}

// documentHistoryDTO serves field SIZES, never text: a text-bearing listing had
// no ceiling (hundreds of thousands of characters in one answer).
func documentHistoryDTO(h DocumentHistory) (DocumentHistoryDTO, error) {
	content, err := documentHistoryContent(h)
	if err != nil {
		return DocumentHistoryDTO{}, err
	}
	fieldChars := map[string]int{}
	for name, text := range content {
		if name == "tombstoned" {
			continue
		}
		fieldChars[name] = utf8.RuneCountInString(text)
	}
	return DocumentHistoryDTO{
		Id:         h.ID,
		CreatedTs:  h.CreatedTS,
		ActorId:    h.ActorID,
		Tombstoned: historyTombstoned(content),
		FieldChars: fieldChars,
	}, nil
}

// documentHistoryRestoreDTO still carries `content`: the restore route's wire shape
// is unchanged.
func documentHistoryRestoreDTO(h DocumentHistory) (DocumentHistoryRestoreDTO, error) {
	content, err := documentHistoryContent(h)
	if err != nil {
		return DocumentHistoryRestoreDTO{}, err
	}
	return DocumentHistoryRestoreDTO{Id: h.ID, Content: content, CreatedTs: h.CreatedTS, ActorId: h.ActorID}, nil
}

// A tombstone means "follow the seed"; writing the same text back as a live
// overlay would silently turn a default document into a customized one.
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
	// The role NAME is deliberately not versioned (owner ruling, T-1f39); a
	// restore leaves the current name standing.
	return historyJSON(map[string]string{
		"definition_md": current.DefinitionMD,
		"tombstoned":    strconv.FormatBool(current.Tombstoned),
	})
}

// These readers run inside the write transaction and re-read the document on
// purpose: trusting a value folded earlier lets two racing writers retain the same
// ancestor, making the revision written in between unrecoverable.
func userContextSnapshotIn(q sqlRowQuerier) (string, error) {
	current, err := getUserContextOn(q)
	if err != nil {
		return "", err
	}
	return userContextHistorySnapshot(current)
}

func roleDefSnapshotIn(roleKey string) func(sqlRowQuerier) (string, error) {
	return func(q sqlRowQuerier) (string, error) {
		current, err := getRoleDefOn(q, roleKey)
		if err != nil {
			return "", err
		}
		return roleDefHistorySnapshot(current)
	}
}

func manualSnapshotIn(typeKey string, of func(TaskManual) (string, error)) func(sqlRowQuerier) (string, error) {
	return func(q sqlRowQuerier) (string, error) {
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

// Only the SOP is versioned; purpose, the identifier fields, display_name and
// assignee are not (owner ruling, T-1f39).
func taskManualHistoryStreams(typeKey, actor string, sopChanged bool) []documentHistoryStream {
	var streams []documentHistoryStream
	if sopChanged {
		streams = append(streams, documentHistoryStream{
			Kind: docKindTaskManualSop, Key: typeKey, ActorID: actor,
			Snapshot: manualSnapshotIn(typeKey, taskManualSopHistorySnapshot),
		})
	}
	return streams
}

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
	primary, valid := historyKeyParts(kind, key)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid document history key")
		return false
	}
	switch kind {
	case docKindSystemInteraction, docKindBootSequence, docKindOffboard,
		docKindAcceleratedStop, docKindTaskCloseout, docKindTaskReassignPredecessor,
		docKindTaskTakeoverWithPredecessor, docKindTaskUnblocked,
		docKindTaskReadyForDone:
		// Restoring one of these puts text into every agent's boot context — a
		// governance write, gated like the edit route. A key this server does not
		// serve is refused first, never answered as "no versions yet".
		if !bootDocHistoryKeyKnown(kind, key) {
			writeError(w, http.StatusBadRequest, unknownBootDocKeyMsg(kind, key))
			return false
		}
		// Read-only is refused BEFORE the capability check: no principal may
		// restore it, so a 403 would send the owner hunting for a role to grant.
		if write {
			if spec, ok := s.bootDocSpecFor(kind, key); ok && spec.ReadOnly {
				writeError(w, http.StatusMethodNotAllowed, bootDocReadOnlyRefusal(spec))
				return false
			}
		}
		if write && !principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
			writeError(w, http.StatusForbidden, "restoring this document requires admin capability")
			return false
		}
	case "global_context", "role_definition":
		if write && !principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
			writeError(w, http.StatusForbidden, "restoring this document requires admin capability")
			return false
		}
	case "insight":
		// `write &&` on purpose: reading insight versions is open to every
		// authenticated caller (owner ruling rc-dc171587220c).
		if write && !s.insightWriteAuthz(w, r, primary) {
			return false
		}
	case docKindTaskManualSop:
	case docKindTaskDescription:
		// Per-DOCUMENT gate: the same predicate as the edit route
		// (callerMayEditTaskText), so a restore never puts back text the caller
		// could not have written.
		if write && !s.taskDescriptionRestoreAuthz(w, r, primary) {
			return false
		}
	case docKindTaskTitle:
		if write && !s.taskTitleRestoreAuthz(w, r, primary) {
			return false
		}
	case "lessons", "task_manual_learnings":
		writeError(w, http.StatusBadRequest, legacyMemoryKindsMsg)
		return false
	case docKindTaskManual:
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

// The id is scoped to the kind/key pair (GetDocumentHistory matches all three),
// so an id belonging to another document 404s instead of leaking its text.
func (s *apiServer) HandleGetDocumentVersionApiDocumentHistoryKindKeyIdGet(w http.ResponseWriter, r *http.Request, kind string, key string, id int64) {
	if !s.documentHistoryAllowed(w, r, kind, key, false) {
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
	content, err := documentHistoryContent(*history)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, DocumentHistoryVersionDTO{
		Kind: kind, Key: key, Id: history.ID, Content: content,
	})
}

// documentSeedContent answers "what would a reset write back", in the SAME field
// names the history snapshot uses: the cockpit diffs the maps key by key, and a
// mismatched name renders 「沒有差異」 against every version instead of erroring.
//
// hasSeed is true only for documents that own a reset route, so this 404s exactly
// where the reset does — the cockpit renders its 初始版本 row from that. Seeds are
// tombstoned because that is how resets are written; it drives the
// 「當時為預設內容」 badge.
func (s *apiServer) documentSeedContent(kind, key string) (map[string]string, bool, error) {
	switch kind {
	case "global_context":
		// No file seed: the default IS the empty document — a real answer, not a
		// missing one.
		return map[string]string{"text": "", "tombstoned": "true"}, true, nil
	case "role_definition":
		seedMD, hasSeed, err := s.root.seedRoleDefinitionMD(key)
		if err != nil {
			return nil, false, err
		}
		if !hasSeed {
			return nil, false, nil
		}
		return map[string]string{"definition_md": seedMD, "tombstoned": "true"}, true, nil
	case docKindSystemInteraction, docKindBootSequence, docKindOffboard,
		docKindAcceleratedStop, docKindTaskCloseout, docKindTaskReassignPredecessor,
		docKindTaskTakeoverWithPredecessor, docKindTaskUnblocked,
		docKindTaskReadyForDone:
		// Same resolver the reset uses, so compare and 還原 cannot show two texts.
		spec, ok := s.bootDocSpecFor(kind, key)
		if !ok {
			return nil, false, nil
		}
		seedMD, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
		if err != nil {
			return nil, false, err
		}
		if !hasSeed {
			return nil, false, nil
		}
		return map[string]string{"text": seedMD, "tombstoned": "true"}, true, nil
	case "insight":
		// 🔴 `text`, NOT `definition_md` — insightHistorySnapshot's field name.
		seedMD, hasSeed, err := s.root.seedInsightMD(key)
		if err != nil {
			return nil, false, err
		}
		if !hasSeed {
			return nil, false, nil
		}
		return map[string]string{"text": seedMD, "tombstoned": "true"}, true, nil
	}
	return nil, false, nil
}

func (s *apiServer) HandleGetDocumentSeedApiDocumentHistoryKindKeySeedGet(w http.ResponseWriter, r *http.Request, kind string, key string) {
	if !s.documentHistoryAllowed(w, r, kind, key, false) {
		return
	}
	content, hasSeed, err := s.documentSeedContent(kind, key)
	if err != nil {
		internalError(w, err)
		return
	}
	if !hasSeed {
		writeError(w, http.StatusNotFound,
			"document '"+kind+"/"+key+"' has no shipped default to compare against")
		return
	}
	writeJSON(w, http.StatusOK, DocumentSeedDTO{Kind: kind, Key: key, Content: content})
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
	dto, err := documentHistoryRestoreDTO(*history)
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
		// "role_def" — "role" is outside the closed topic set and was silently dropped.
		s.hub.Publish("role_def", "patch", "role_def", wireOwnerID+"::"+key, nil, audienceOwnerOnly(), requestTrigger(r))
	case "insight":
		// 🔴 This switch has no default: a missing kind still returns 200 with the
		// DB changed, and every other surface shows the old text until a manual
		// reload. A test exists solely because nothing else would go red.
		s.hub.Publish("insight", "patch", "insight", wireOwnerID+"::"+key, nil, audienceOwnerOnly(), requestTrigger(r))
	case docKindSystemInteraction, docKindBootSequence, docKindOffboard,
		docKindAcceleratedStop, docKindTaskCloseout, docKindTaskReassignPredecessor,
		docKindTaskTakeoverWithPredecessor, docKindTaskUnblocked,
		docKindTaskReadyForDone:
		s.publishBootDoc(r)
	case docKindTaskManualSop:
		s.publishTaskManual(key, requestTrigger(r))
	case docKindTaskDescription, docKindTaskTitle:
		if t, err := s.resolveTask(key); err == nil {
			s.publishTask(*t, requestTrigger(r))
		}
	}
}

func (s *apiServer) taskDescriptionRestoreAuthz(w http.ResponseWriter, r *http.Request, taskID string) bool {
	t, err := s.resolveTask(taskID)
	if err != nil {
		writeResolveError(w, err, "task", taskID)
		return false
	}
	if !s.callerMayEditTaskText(r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return false
	}
	return true
}

func (s *apiServer) taskTitleRestoreAuthz(w http.ResponseWriter, r *http.Request, taskID string) bool {
	return s.taskDescriptionRestoreAuthz(w, r, taskID)
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
		// The cap applies to restore too: otherwise edit down, then restore a
		// larger earlier revision, and nothing ever checks it.
		if DocCapBlocked(s.dutyCap(), folded.DefinitionMD, content["definition_md"]) {
			return errDocumentHistoryCap
		}
		name := folded.Name
		return s.dal.SaveWithDocumentHistory(kind, key, actor, roleDefSnapshotIn(key), func(ex sqlExecer) error {
			return putRoleDefOn(ex, RoleDef{RoleKey: key, Name: name, DefinitionMD: content["definition_md"], Tombstoned: historyTombstoned(content)})
		})
	case docKindTaskDescription:
		// No doc cap: the description has none on the create side either, so a
		// cap here would only allow restoring SHORTER versions (reasoning at the
		// edit door, api_tasks_description.go).
		t, err := s.resolveTask(key)
		if err != nil {
			return err
		}
		ok, err := s.writeTaskDescription(t, actor, content["description"])
		if err != nil {
			return err
		}
		if !ok {
			return errNotFound
		}
		return nil
	case docKindTaskTitle:
		// No doc cap (create_task has none). The blank check is belt-and-braces:
		// a vanished task already 404s at the door, and no stored revision is
		// blank. ⚠️ errNotFound from any arm of this switch surfaces as a 500 via
		// internalError — known, scoped out.
		t, err := s.resolveTask(key)
		if err != nil {
			return err
		}
		title := trimString(content["title"])
		if title == "" {
			return errNotFound
		}
		ok, err := s.writeTaskTitle(t, actor, title)
		if err != nil {
			return err
		}
		if !ok {
			return errNotFound
		}
		return nil
	case "insight":
		// The key is the BARE role_key.
		current, err := s.foldInsightDTO(key)
		if err != nil {
			return err
		}
		if DocCapBlocked(s.insightCap(), current.Text, content["text"]) {
			return errDocumentHistoryCap
		}
		return s.dal.SaveWithDocumentHistory(kind, key, actor, insightSnapshotIn(key), func(ex sqlExecer) error {
			return putInsightOn(ex, Insight{RoleKey: key, Text: content["text"], Tombstoned: historyTombstoned(content)})
		})
	case docKindSystemInteraction, docKindBootSequence, docKindOffboard,
		docKindAcceleratedStop, docKindTaskCloseout, docKindTaskReassignPredecessor,
		docKindTaskTakeoverWithPredecessor, docKindTaskUnblocked,
		docKindTaskReadyForDone:
		// The cap applies here; the RESET path deliberately skips it (see resetBootDoc).
		//
		// 🔴 Restore goes through bootDocBodyOf + bootDocStoredText, the same join
		// replaceBootDoc uses, so the old body lands under the SHIPPED head; handing
		// content["text"] straight to putBootDocumentOn would re-arm a pre-marker,
		// headless version. The wipe guard is deliberately absent: a restore has
		// no 「清空」 intent.
		spec, ok := s.bootDocSpecFor(kind, key)
		if !ok {
			return errNotFound
		}
		current, err := s.foldBootDocDTO(spec)
		if err != nil {
			return err
		}
		body := bootDocBodyOf(spec, content["text"])
		restored, err := s.bootDocStoredText(spec, body)
		if err != nil {
			return err
		}
		if DocCapBlocked(spec.Cap, current.Text, restored) {
			return errDocumentHistoryCap
		}
		return s.dal.SaveWithDocumentHistory(kind, key, actor, bootDocSnapshotIn(kind, key), func(ex sqlExecer) error {
			return putBootDocumentOn(ex, BootDocument{
				Kind: kind, Key: key,
				Text:       restored,
				Tombstoned: historyTombstoned(content),
			})
		})
	case docKindTaskManualSop:
		return s.restoreTaskManualField(key, taskManualHistoryStreams(key, actor, true),
			func(m *TaskManual) error {
				if DocCapBlocked(s.manualSopCap(), m.SopMD, content["sop_md"]) {
					return errDocumentHistoryCap
				}
				m.SopMD = content["sop_md"]
				return nil
			})
	}
	return errNotFound
}

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
