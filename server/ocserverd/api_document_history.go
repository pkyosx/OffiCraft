package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
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

func documentHistoryContent(h DocumentHistory) (map[string]string, error) {
	content := map[string]string{}
	if err := json.Unmarshal([]byte(h.ContentJSON), &content); err != nil {
		return nil, err
	}
	return content, nil
}

// documentHistoryDTO is the CATALOGUE row: identity, provenance, the tombstone
// flag, and the SIZE of every field the revision holds — never the text. The
// listing is where a reader picks a revision, and picking does not need the
// prose: one answer had a structural ceiling in the hundreds of thousands of
// characters with no narrowing of any kind. The chosen revision's body comes
// from HandleGetDocumentVersion… below.
//
// `tombstoned` is lifted OUT of the field map and served as its own boolean:
// it is the only entry of `content` that is a flag rather than a document, so
// leaving it in field_chars would report "4" for it — a character count of the
// string "true", which measures nothing anybody asked about.
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

// documentHistoryRestoreDTO is the RESTORE receipt, and it deliberately still
// carries `content` — the shape that route has always answered with. A restore
// names exactly one revision and its whole point is that this text is now what
// the live document holds; handing back sizes there would answer a question
// nobody asked. Splitting the two shapes is what lets the listing get light
// without changing the write face's wire at all.
func documentHistoryRestoreDTO(h DocumentHistory) (DocumentHistoryRestoreDTO, error) {
	content, err := documentHistoryContent(h)
	if err != nil {
		return DocumentHistoryRestoreDTO{}, err
	}
	return DocumentHistoryRestoreDTO{Id: h.ID, Content: content, CreatedTs: h.CreatedTS, ActorId: h.ActorID}, nil
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
	case docKindSystemInteraction, docKindBootSequence, docKindOffboard:
		// T-791e. Same class gate as global_context below — restoring one of
		// these puts text into every agent's boot context, so it is a governance
		// write (owner or the admin 助理), exactly as the edit route is. Reading
		// stays at the floor the route table declares, like every other kind
		// here.
		//
		// A key this server does not serve is refused BEFORE any of that: the
		// list/restore faces must not answer for boot_sequence/opus as if it
		// were a document that merely has no versions yet.
		if !bootDocHistoryKeyKnown(kind, key) {
			writeError(w, http.StatusBadRequest, unknownBootDocKeyMsg(kind, key))
			return false
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
	case docKindTaskDescription:
		// T-e271. The only kind whose restore gate is per-DOCUMENT rather than
		// per-class: a task description is writable by that task's executor (or
		// an admin), which is a fact about THIS key, so the ladder alone cannot
		// decide it. Reuses callerMayDriveTask — the same predicate the edit
		// route uses — so a restore can never put back text the caller was not
		// allowed to write in the first place. Reading stays open like the
		// manual series: the task itself is already readable at this floor.
		if write && !s.taskDescriptionRestoreAuthz(w, r, primary) {
			return false
		}
	case docKindTaskTitle:
		// T-2ebe. Same per-DOCUMENT posture as the description above, and for
		// the same reason: who may correct THIS task's text is a fact about this
		// key, not about the caller's class. Shares the one predicate rather
		// than growing a second — a restore must never put back a title the
		// caller was not allowed to write.
		if write && !s.taskTitleRestoreAuthz(w, r, primary) {
			return false
		}
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

// GET /api/document-history/{kind}/{key}/{id} — the BODY of one named revision.
//
// The other half of the pair the listing became: list_document_history says
// which revisions exist and how big each of their fields is, this says what one
// of them held. Same floor and the same addressing gate as the listing and the
// seed route (`documentHistoryAllowed(..., write=false)`) — reading one
// revision is not a bigger permission than reading the catalogue that names it.
//
// The id is scoped to the kind/key pair, because GetDocumentHistory looks up
// all three: an id belonging to some OTHER document 404s here rather than
// handing back that other document's text. That is why the address is the whole
// triple and never the id alone.
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

// documentSeedContent answers "what would a reset of this document write back",
// in the SAME field names a retained revision carries — which is what lets the
// cockpit hand it to the very same reader/diff the retained versions use.
//
// The second return is "this document HAS a shipped default". It is true for
// exactly the THREE documents that own a reset route (`POST
// /api/global-context/reset`, `POST /api/roles/{role}/reset` on a SEED role,
// `POST /api/insight/{role_key}/reset` on a role with an insight seed) and
// false everywhere else, so the 404 here lands in exactly the places the reset
// itself 404s. That symmetry is the point: the cockpit renders its 初始版本 row
// from the presence of a reset, and a row whose "compare" 404s while its
// "restore" works (or the reverse) would be worse than no row.
//
// `tombstoned` rides along because that IS how both resets are written (an
// overlay tombstone means "follow the seed"), and because the surfaces render
// it as the 「當時為預設內容」 badge — the seed row is the one entry for which
// that badge is unconditionally true.
func (s *apiServer) documentSeedContent(kind, key string) (map[string]string, bool, error) {
	switch kind {
	case "global_context":
		// The user-custom block has no file seed: its default IS the empty
		// document (reset = tombstone → `text=""`, `is_default=true`). Empty is
		// a real answer, not a missing one — the diff against it is exactly
		// "everything you wrote would go away", which is what the owner needs
		// to see before pressing 還原.
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
	case docKindSystemInteraction, docKindBootSequence, docKindOffboard:
		// T-791e. The seed content comes from readSeedFile through the same
		// resolver the reset uses (bootDocSpecFor → seedBlockMD), so "what the
		// compare view shows" and "what 還原 would write" cannot be two different
		// texts. Field name `text`, matching bootDocHistorySnapshot — the
		// cockpit diffs the two maps key by key, so a mismatched name renders
		// 「沒有差異」 against every retained version instead of erroring.
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
		// 🔴 `text`, NOT `definition_md` — that is the field name
		// insightHistorySnapshot writes into a retained insight revision, and
		// the cockpit diffs the two maps key-by-key. Naming it after the role
		// definition's field would not error anywhere: the modal would simply
		// render 「沒有差異」 against every retained version, which is the worst
		// possible way to be wrong about a destructive restore.
		//
		// The roster is the SET OF FILES (seeds/insight_<role_key>.md), not
		// seedRoleKeys() — see seedInsightMD. So this is per-role by
		// construction and 404s for a role with no insight seed, which is
		// exactly where `POST /api/insight/{role_key}/reset` 404s too.
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

// GET /api/document-history/{kind}/{key}/seed — the document's shipped default.
//
// READ-ONLY on purpose, and that is the whole reason it exists: before it, the
// seed text only ever reached a client as the RESPONSE TO A RESET, so the one
// entry in the version list whose restore is least reversible was also the only
// one nobody could look at first. Same floor as reading the retained versions
// (`documentHistoryAllowed(..., write=false)`) — comparing is reading.
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
	case docKindSystemInteraction, docKindBootSequence, docKindOffboard:
		// T-791e — the same frame the edit routes fan (see publishBootDoc).
		// Forgetting to be in THIS switch is the silent failure the insight case
		// above documents: 200, DB changed, nothing on any screen.
		s.publishBootDoc(r)
	case docKindTaskManualSop, docKindTaskManualLearnings:
		s.publishTaskManual(key, requestTrigger(r))
	case docKindTaskDescription, docKindTaskTitle:
		// Both fan the same task delta: the cockpit's list and card reconcile by
		// re-reading the task, so one topic serves either field. T-2ebe rides
		// the description's arm rather than adding a second identical one —
		// see the insight case above for why forgetting to be here at all is
		// the failure this switch is most prone to.
		if t, err := s.resolveTask(key); err == nil {
			s.publishTask(*t, requestTrigger(r))
		}
	}
}

// taskDescriptionRestoreAuthz answers whether this caller may put an earlier
// description back, and writes the refusal when not (T-e271). Same ladder as the
// edit route by construction — it calls the same function — so the two faces of
// "who may change this text" cannot drift apart.
func (s *apiServer) taskDescriptionRestoreAuthz(w http.ResponseWriter, r *http.Request, taskID string) bool {
	t, err := s.resolveTask(taskID)
	if err != nil {
		writeResolveError(w, err, "task", taskID)
		return false
	}
	if !s.callerMayDriveTask(r, *t) {
		writeError(w, http.StatusForbidden, "caller is not the task's executor")
		return false
	}
	return true
}

// taskTitleRestoreAuthz answers whether this caller may put an earlier title
// back, and writes the refusal when not (T-2ebe). Twin of the description
// predicate above, and it calls the same callerMayDriveTask for the same reason:
// the restore face and the edit face of "who may change this task's text" must
// be one decision, not two that can drift.
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
		// The cap applies to a restore too (T-ae38), exactly as it already did
		// for lessons and insight below — and this branch is the reason the
		// edit-door check alone would not be a cap at all: edit the definition
		// down to 999 chars and then restore a 4,000-char earlier revision, and
		// nothing would ever have looked. Duty was the ONLY kind in this switch
		// with no check; lessons and insight are the shape to copy.
		if DocCapBlocked(s.dutyCap(), folded.DefinitionMD, content["definition_md"]) {
			return errDocumentHistoryCap
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
		if DocCapBlocked(s.learningCap(), current.Text, content["text"]) {
			return errDocumentHistoryCap
		}
		return s.dal.SaveWithDocumentHistory(kind, key, actor, lessonsSnapshotIn(roleKey, taskType), func(ex sqlExecer) error {
			return putLessonsOn(ex, Lessons{RoleKey: roleKey, TaskType: taskType, Text: content["text"], Tombstoned: historyTombstoned(content)})
		})
	case docKindTaskDescription:
		// T-e271. No doc cap: the description has never had a length ceiling on
		// the create side either, so a cap applied only here would mean an
		// already-long description can only ever be restored to a SHORTER
		// version — DocCapBlocked refuses over-cap writes that are not shorter
		// than what is stored, and restoring an earlier, longer wording is
		// exactly such a write. Reasoning in full at the edit door
		// (api_tasks_description.go); it is stated there once, including the
		// correction of an earlier draft that overclaimed this as "permanently
		// uneditable".
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
		// T-2ebe. No doc cap, for the reason its edit door states: create_task
		// has never capped this field either.
		//
		// 🔴 The blank check below is BELT-AND-BRACES, and saying so is the point:
		// an earlier draft of this comment claimed it was the thing that caught
		// the "{}" snapshot case, which is not true and was corrected by
		// independent review rather than left to read as a mechanism.
		//
		// What actually happens: a vanished task is refused at the DOOR —
		// documentHistoryAllowed → taskTitleRestoreAuthz → resolveTask answers a
		// clean 404 before this function runs. A retained revision can only have
		// been written through a door that already refused blanks, so no stored
		// revision restores to one either. The guard is therefore expected to be
		// unreachable; it stays because the cost of being wrong about that is a
		// blank title on the task list, which is the one state this whole
		// capability exists to prevent.
		//
		// ⚠️ Its failure shape is inherited, not chosen: errNotFound from any arm
		// of this switch is funnelled into internalError, so it would surface as
		// a 500 rather than a 404. That is pre-existing (the description arm at
		// the top of this switch does the same) and is NOT fixed here — flagged
		// so the next reader knows it was seen and scoped out, not missed.
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
		// The key is the BARE role_key — insight has no task_type axis, so
		// there is nothing to split out of it the way lessons does above.
		current, err := s.foldInsightDTO(key)
		if err != nil {
			return err
		}
		// The cap applies to a restore too: an older, larger revision is still
		// a write, and letting history walk a doc back over the limit would
		// make the cap a suggestion.
		if DocCapBlocked(s.insightCap(), current.Text, content["text"]) {
			return errDocumentHistoryCap
		}
		return s.dal.SaveWithDocumentHistory(kind, key, actor, insightSnapshotIn(key), func(ex sqlExecer) error {
			return putInsightOn(ex, Insight{RoleKey: key, Text: content["text"], Tombstoned: historyTombstoned(content)})
		})
	case docKindSystemInteraction, docKindBootSequence, docKindOffboard:
		// T-791e. The cap applies to a restore, exactly as it does for lessons
		// and insight above: an older, larger revision is still a write, and
		// letting history walk a document back over the ceiling would make the
		// ceiling a suggestion. (The RESET path is the deliberate opposite — see
		// resetBootDoc: the factory text is the product, not something a caller
		// wrote.)
		spec, ok := s.bootDocSpecFor(kind, key)
		if !ok {
			return errNotFound
		}
		current, err := s.foldBootDocDTO(spec)
		if err != nil {
			return err
		}
		if DocCapBlocked(spec.Cap, current.Text, content["text"]) {
			return errDocumentHistoryCap
		}
		return s.dal.SaveWithDocumentHistory(kind, key, actor, bootDocSnapshotIn(kind, key), func(ex sqlExecer) error {
			return putBootDocumentOn(ex, BootDocument{
				Kind: kind, Key: key,
				Text:       content["text"],
				Tombstoned: historyTombstoned(content),
			})
		})
	case docKindTaskManualSop:
		return s.restoreTaskManualField(key, taskManualHistoryStreams(key, actor, true, false),
			func(m *TaskManual) error {
				if DocCapBlocked(s.manualSopCap(), m.SopMD, content["sop_md"]) {
					return errDocumentHistoryCap
				}
				m.SopMD = content["sop_md"]
				return nil
			})
	case docKindTaskManualLearnings:
		return s.restoreTaskManualField(key, taskManualHistoryStreams(key, actor, false, true),
			func(m *TaskManual) error {
				if DocCapBlocked(s.manualLearningsCap(), m.Learnings, content["learnings"]) {
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
