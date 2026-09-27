package main

// 🔴 The mechanism is specified in docs/design/boot-documents.md, which
// deliberately does NOT list which documents exist: bootDocRegistry below is
// the server's list, and the wire's list is the BootDocKind enum in
// spec/openapi.json.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type bootDocSpec struct {
	Kind     string
	Key      string
	SeedFile string
	Cap      int

	DocName string
	// nil opts the kind out of variable validation entirely (doc_vars.go); a
	// non-nil empty slice means validated and allows none.
	Vars []string

	Split bool
	Join  string
	// Shown but never editable (owner's ruling: 「以前 global context 是固定內容
	// 我們也是會顯示 只是不給改」).
	ReadOnly bool
}

type bootDocReg struct {
	Kind string

	Keys []string

	SeedFor func(key string) string
	DocName func(key string) string
	Cap     func(s *apiServer) int

	Vars []string

	Split    bool
	Join     string
	ReadOnly bool
}

// 🔴 Split and the seed's docBodyMarker must change together
// (TestBootDocRegistry_ASeedCarriesAMarkerExactlyWhenItsKindIsSplit): dropping
// only the marker makes every write a 500 and every notice ""; dropping only
// Split hands the owner an editable body that contains the read-only head.
//
// ⚠️ read_only is mirrored in bin/tests/fixtures/boot-doc-registry.tsv (the
// cockpit's own copy); the mirror test on both sides makes a one-sided change
// red instead of invisible.
//
// Join is a blank line where the body opens on a list or paragraph, so the
// head's statement of fact does not fold into the first instruction.
var bootDocRegistry = []bootDocReg{{
	Kind:    docKindSystemInteraction,
	Keys:    []string{systemInteractionDocKey},
	SeedFor: func(string) string { return systemInteractionSeedMD },
	DocName: func(string) string { return "system interaction block" },
	Cap:     func(s *apiServer) int { return s.systemInteractionCap() },
	// Vars stays nil: an owner-edited body may quote JSON (`{"id": "…"}`) that the
	// {name} syntax cannot tell from a variable.
}, {
	Kind: docKindBootSequence,
	Keys: []string{bootSequenceKeyClaude, bootSequenceKeyCodex},

	SeedFor: bootSequenceSeedName,
	DocName: func(key string) string { return "boot steps (" + key + ")" },
	Cap:     func(s *apiServer) int { return s.bootSequenceCap() },
}, {
	Kind:    docKindOffboard,
	Keys:    []string{offboardDocKey},
	SeedFor: func(string) string { return offboardSeedMD },
	DocName: func(string) string { return "Stop document" },
	Cap:     func(s *apiServer) int { return s.offboardCap() },
	// EMPTY, NOT nil: validated, declares none. The unsplit document itself is
	// sent literally, including any braces in its body.
	Vars: []string{},
}, {
	// Shares the offboard cap on purpose: the same procedure under a shorter
	// clock.
	Kind:    docKindAcceleratedStop,
	Keys:    []string{acceleratedStopDocKey},
	SeedFor: func(string) string { return acceleratedStopSeedMD },
	DocName: func(string) string { return "accelerated stop sequence" },
	Cap:     func(s *apiServer) int { return s.offboardCap() },

	Split: true,
	Join:  "\n",
	Vars:  []string{"deadline"},
}, {
	Kind:    docKindTaskCloseout,
	Keys:    []string{taskCloseoutDocKey},
	SeedFor: func(string) string { return taskCloseoutSeedMD },
	DocName: func(string) string { return "task close-out procedure" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	// 🔴 The head carries only facts the notice is the ONLY source of (owner's
	// decision 3): the ticket number, and who closed it — not on the ticket.
	Split: true,
	Join:  "\n",
	Vars:  []string{"task_no", "closed_by"},
}, {
	Kind:    docKindTaskReassignPredecessor,
	Keys:    []string{taskReassignPredecessorDocKey},
	SeedFor: func(string) string { return taskReassignPredecessorSeedMD },
	DocName: func(string) string { return "task reassignment document (to the predecessor)" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	// 🔴 The successor is deliberately NOT named (owner, 2026-08-24:
	// 「如果完全不提到接手人是誰呢」「讓他自己去查」「不管是不是 outsource」).
	Split: true,
	Join:  "\n\n",
	Vars:  []string{"task_no"},
}, {
	Kind:    docKindTaskTakeoverWithPredecessor,
	Keys:    []string{taskTakeoverWithPredecessorDocKey},
	SeedFor: func(string) string { return taskTakeoverWithPredecessorSeedMD },
	DocName: func(string) string { return "task reassignment document (to the successor)" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	// 🔴 No {note} (owner, rc-0c36d8739b8f: 「拿掉 —— 交接備註只留在任務上」).
	// {predecessor} is ONE slot carrying name and id, e.g. 「銀月（mira）」 (owner's
	// decision 1): the body's first instruction is to post_chat the predecessor.
	Split: true,
	Join:  "\n\n",
	Vars:  []string{"task_no", "predecessor"},
}, {
	Kind:    docKindTaskUnblocked,
	Keys:    []string{taskUnblockedDocKey},
	SeedFor: func(string) string { return taskUnblockedSeedMD },
	DocName: func(string) string { return "dependency-released notice" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	Split:   true,

	Join: "\n\n",

	Vars: []string{"blocked_task_no"},
}, {
	Kind:    docKindTaskReadyForDone,
	Keys:    []string{taskReadyForDoneDocKey},
	SeedFor: func(string) string { return taskReadyForDoneSeedMD },
	DocName: func(string) string { return "ready-for-done notice" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	Split:   true,

	Join: "\n\n",
	// {visit_no}: a task re-enters ready_for_done every time a step is added and
	// done, so without the count a repeat notice reads as a duplicate delivery. It
	// counts ARRIVALS (task.ready_for_done_visits), so it survives restarts.
	Vars: []string{"task_no", "visit_no"},
}}

func bootDocRegFor(kind string) (bootDocReg, bool) {
	for _, reg := range bootDocRegistry {
		if reg.Kind == kind {
			return reg, true
		}
	}
	return bootDocReg{}, false
}

func (reg bootDocReg) serves(key string) bool {
	for _, k := range reg.Keys {
		if k == key {
			return true
		}
	}
	return false
}

func (s *apiServer) systemInteractionSpec() bootDocSpec {
	return s.mustBootDocSpec(docKindSystemInteraction, systemInteractionDocKey)
}

func (s *apiServer) offboardSpec() bootDocSpec {
	return s.mustBootDocSpec(docKindOffboard, offboardDocKey)
}

func (s *apiServer) acceleratedStopSpec() bootDocSpec {
	return s.mustBootDocSpec(docKindAcceleratedStop, acceleratedStopDocKey)
}

func (s *apiServer) mustBootDocSpec(kind, key string) bootDocSpec {
	spec, ok := s.bootDocSpecFor(kind, key)
	if !ok {
		panic("boot document " + kind + "/" + key + " is not in bootDocRegistry")
	}
	return spec
}

// ok=false → the caller answers 404 rather than falling back to claude, which
// is how a codex reader would end up with the sequence that keeps it from
// booting.
func (s *apiServer) bootSequenceSpecFor(runtimeKey string) (bootDocSpec, bool) {
	return s.bootDocSpecFor(docKindBootSequence, runtimeKey)
}

func (s *apiServer) bootDocSpecFor(kind, key string) (bootDocSpec, bool) {
	reg, ok := bootDocRegFor(kind)
	if !ok || !reg.serves(key) {
		return bootDocSpec{}, false
	}
	return bootDocSpec{
		Kind:     reg.Kind,
		Key:      key,
		SeedFile: reg.SeedFor(key),
		Cap:      reg.Cap(s),
		DocName:  reg.DocName(key),
		Vars:     reg.Vars,
		Split:    reg.Split,
		Join:     reg.Join,
		ReadOnly: reg.ReadOnly,
	}, true
}

// Asked BEFORE the history faces list or restore, so "wrong key" and "no
// versions yet" do not look the same.
func bootDocHistoryKeyKnown(kind, key string) bool {
	reg, ok := bootDocRegFor(kind)
	return ok && reg.serves(key)
}

func unknownBootDocKeyMsg(kind, key string) string {
	reg, ok := bootDocRegFor(kind)
	if !ok {
		return "document history kind '" + kind + "' names no editable document on this server"
	}
	quoted := make([]string, 0, len(reg.Keys))
	for _, k := range reg.Keys {
		quoted = append(quoted, "'"+k+"'")
	}
	return "document history key '" + key + "' does not name a " + kind +
		" document — the key is " + strings.Join(quoted, " or ")
}

// ⚠️ DocName is user-facing prose (a refusal names the document by it) that
// follows the cockpit's names; frontend/src/api/mock.ts and the OpenAPI
// descriptions carry copies, and nothing enforces any of them.

func (s *apiServer) foldBootDocDTO(spec bootDocSpec) (*bootDocDTO, error) {
	overlay, err := s.dal.GetBootDocument(spec.Kind, spec.Key)
	if err != nil {
		return nil, err
	}
	seedMD, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		return nil, err
	}
	text, isDefault := FoldBootDocument(overlay, seedMD, hasSeed)
	// Owner's ruling 「讀取有這個 key，回寫沒有這個 key」: the read names the
	// read-only half (read_only_head) and returns the editable half as `body`, the
	// same name the write takes. `text` stays the WHOLE stored document — what the
	// history stores and what size_chars counts.
	head := ""
	if spec.Split {
		if h, _, split := DocSplitHeadBody(text); split {
			head = h
		}
	}
	return &bootDocDTO{
		SizeChars:     utf8.RuneCountInString(text),
		CapChars:      spec.Cap,
		Kind:          spec.Kind,
		Key:           spec.Key,
		Text:          text,
		ReadOnlyHead:  head,
		Body:          bootDocBodyOf(spec, text),
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     isDefault,
		HasSeed:       hasSeed,
		ReadOnly:      spec.ReadOnly,
	}, nil
}

// 🔴 Read sites take the RENDERED text (a reader must not see the marker);
// the cockpit takes the stored one.
func (s *apiServer) systemInteractionText() (string, error) {
	spec := s.systemInteractionSpec()
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		return "", err
	}
	return DocRendered(dto.Text, spec.Join), nil
}

// 🔴 WHICH DOCUMENT is the whole soft/hard distinction: soft reads 〈停止〉,
// final reads 〈加速停止〉 (the only head with {deadline}). Handing the hard arm
// 〈停止〉 is a flat lie — it opens 「這類停止沒有收尾倒數」 while the clock runs.
func (s *apiServer) winddownNoticeText(kind string, deadline float64) string {
	spec := s.offboardSpec()
	values := map[string]string{}
	if kind == offboardKindFinal {
		// Unreachable (final implies a positive deadline); refuse rather than send
		// a 1970 deadline.
		if deadline <= 0 {
			return ""
		}
		spec = s.acceleratedStopSpec()
		// .UTC(): the agent need not run on this host, and the client de-dupes on the
		// whole sentence verbatim, so one epoch must render to one string.
		values["deadline"] = time.Unix(int64(deadline), 0).UTC().Format(time.RFC3339)
	}
	return s.eventNoticeText(spec, values)
}

// 🔴 Exists because the head makes a claim the body does not: 〈任務結案〉 opens
// 「任務 {task_no} 已結束。」, which is FALSE for an outsource worker wound down
// mid-task that still needs the instructions.
func (s *apiServer) taskEventBodyText(kind string) string {
	spec := s.mustBootDocSpec(kind, bootDocSingletonKey)
	dto, err := s.foldBootDocDTO(spec)
	if err != nil || dto == nil {
		return ""
	}
	if _, _, split := DocSplitHeadBody(dto.Text); spec.Split && !split {
		return ""
	}
	return strings.TrimSpace(bootDocBodyOf(spec, dto.Text))
}

// 🔴 The kind is the whole of which words go out, and a wrong one reads as a
// coherent notice, so send-site tests compare the whole posted body.
//
// TrimSpace because a chat row is one message. The wind-down notices do NOT
// trim: theirs is an SSE field whose bytes the client de-dupes against.
func (s *apiServer) taskNoticeText(kind string, values map[string]string) string {
	return strings.TrimSpace(
		s.eventNoticeText(s.mustBootDocSpec(kind, bootDocSingletonKey), values))
}

const takeoverNoPredecessorHead = "[{task_no}] 你接手了這張任務，這張任務沒有前任。"

func (s *apiServer) takeoverNoticeText(taskNo, predecessor string) string {
	values := map[string]string{"task_no": taskNo, "predecessor": predecessor}
	spec := s.mustBootDocSpec(docKindTaskTakeoverWithPredecessor, bootDocSingletonKey)
	if predecessor != "" {
		return strings.TrimSpace(s.eventNoticeText(spec, values))
	}
	return strings.TrimSpace(s.eventNoticeTextWithHead(spec, takeoverNoPredecessorHead, values))
}

// "" on any fault, and every caller omits the notice: a sentence with
// `{deadline}` still in it reads as a real instant nobody can parse.
//
// 🔴 A SPLIT KIND WHOSE STORED TEXT HAS NO MARKER IS A FAULT HERE, deliberately
// not in DocRendered, whose lenient branch the boot folds need. A headless
// notice ships NON-EMPTY: it disarms cli/ocagent's offboardFallback (which arms
// on an ABSENT notice) and it misleads, since which task / which deadline lives
// only in the head. The reachable way in is an overlay written before
// docBodyMarker existed — no migration rewrote those rows.
func (s *apiServer) eventNoticeText(spec bootDocSpec, values map[string]string) string {
	return s.eventNoticeTextWithHead(spec, "", values)
}

func (s *apiServer) eventNoticeTextWithHead(spec bootDocSpec, head string, values map[string]string) string {
	dto, err := s.foldBootDocDTO(spec)
	if err != nil || dto == nil {
		return ""
	}
	if !spec.Split {
		return dto.Text
	}
	storedHead, body, split := DocSplitHeadBody(dto.Text)
	if !split {
		return ""
	}
	if head == "" {
		head = storedHead
	}
	head, err = RenderDocVars(head, spec.Vars, values)
	if err != nil {
		return ""
	}
	return head + spec.Join + body
}

func (s *apiServer) bootSequenceText(runtime string) (string, error) {
	spec, ok := s.bootSequenceSpecFor(bootSequenceDocKey(runtime))
	if !ok {
		// Unreachable by construction; fail closed rather than serve a blank boot
		// sequence, which would look like a successful boot.
		return "", errNotFound
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		return "", err
	}
	return DocRendered(dto.Text, spec.Join), nil
}

// Runs INSIDE the write transaction so the retained revision is the state THIS
// write replaced; otherwise two racing writers lose the version between them.
func bootDocSnapshotIn(kind, key string) func(sqlRowQuerier) (string, error) {
	return func(q sqlRowQuerier) (string, error) {
		current, err := getBootDocumentOn(q, kind, key)
		if err != nil {
			return "", err
		}
		return bootDocHistorySnapshot(current)
	}
}

func bootDocHistorySnapshot(current *BootDocument) (string, error) {
	if current == nil {
		return "{}", nil
	}
	return historyJSON(map[string]string{
		"text": current.Text, "tombstoned": strconv.FormatBool(current.Tombstoned),
	})
}

// Deliberately NOT a new topic: a topic outside the closed vocabulary is
// dropped SILENTLY (see sseTopics), and the cockpit's 全域情境 pane renders
// these blocks together.
func (s *apiServer) publishBootDoc(r *http.Request) {
	s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil,
		audienceOwnerOnly(), requestTrigger(r))
}

// 🔴 A WRITE THAT CHANGES NOTHING RETAINS NOTHING (owner's ruling): each idle
// save would otherwise push the version the owner wants one step closer to
// falling off the history. global_context deliberately does NOT do this — a
// known gap, not a precedent.
func (s *apiServer) writeBootDoc(r *http.Request, spec bootDocSpec, current *bootDocDTO, next BootDocument, nextText string) (bool, error) {
	// Both halves: comparing only the text would swallow adopting the seed's own
	// bytes as an edit (which changes the next reset); only the flag would let a
	// genuine rewrite pass as a no-op.
	if current.Text == nextText && current.IsDefault == next.Tombstoned {
		return false, nil
	}
	if err := s.dal.SaveWithDocumentHistory(spec.Kind, spec.Key, currentActor(r),
		bootDocSnapshotIn(spec.Kind, spec.Key), func(ex sqlExecer) error {
			return putBootDocumentOn(ex, next)
		}); err != nil {
		return false, err
	}
	s.publishBootDoc(r)
	return true, nil
}

// 🔴 IT TAKES THE BODY, NOT THE DOCUMENT (owner's ruling, 2026-08-23:
// 「唯讀區應該無法回寫，讀取有這個 key，回寫沒有這個 key，沒有人有任何方式可以
// 回寫」): the wire has no field for the head, and the shipped head is joined
// back on here. Side effect: the first write repairs a pre-marker row.
func (s *apiServer) replaceBootDoc(w http.ResponseWriter, r *http.Request, spec bootDocSpec, body string, allowShrink bool) {
	if spec.ReadOnly {
		writeError(w, http.StatusMethodNotAllowed, bootDocReadOnlyRefusal(spec))
		return
	}
	current, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	next, err := s.bootDocStoredText(spec, body)
	if err != nil {
		internalError(w, err)
		return
	}
	// 🔴 WIPE judges the BODY (the head survives every write, so the joined text
	// could never read as emptied); THE CAP judges the STORED text (the number
	// size_chars and the cockpit show). Pinned by
	// TestReplaceBootDoc_TheWipeGuardJudgesTheBodyAndTheCapJudgesTheStoredDocument.
	if !allowShrink && WholeDocWipeBlocked(bootDocBodyOf(spec, current.Text), body) {
		writeError(w, http.StatusBadRequest,
			docWipeRefusal(spec.DocName, ", or reset it to the shipped default"))
		return
	}
	// Hard cap, checked UNCONDITIONALLY: allow_shrink is not a bypass.
	if DocCapBlocked(spec.Cap, current.Text, next) {
		writeError(w, http.StatusBadRequest, docCapRefusal(spec.Cap, spec.DocName, current.Text, next))
		return
	}
	if _, err := s.writeBootDoc(r, spec, current,
		BootDocument{Kind: spec.Kind, Key: spec.Key, Text: next, Tombstoned: false}, next); err != nil {
		internalError(w, err)
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bootDocReceiptOf(dto))
}

func bootDocReceiptOf(dto *bootDocDTO) bootDocumentReceiptDTO {
	return bootDocumentReceiptDTO{
		Kind:      dto.Kind,
		Key:       dto.Key,
		IsDefault: dto.IsDefault,
		SizeChars: dto.SizeChars,
		CapChars:  dto.CapChars,
		Sha256:    receiptSha256(dto.Text),
	}
}

// 🔴 THE HEAD COMES FROM THE SEED, not from the stored row: otherwise one row
// that ever acquired a wrong head would hand it to every write after it.
func (s *apiServer) bootDocStoredText(spec bootDocSpec, body string) (string, error) {
	if !spec.Split {
		return body, nil
	}
	seedMD, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		return "", err
	}
	seedHead, _, seedSplit := DocSplitHeadBody(seedMD)
	if !hasSeed || !seedSplit {
		// 500, not a refusal: nothing the caller typed caused it, and writing the
		// body alone would silently retire the read-only half.
		return "", errors.New("boot document " + spec.Kind + "/" + spec.Key +
			" is declared split but its seed carries no " + docBodyMarker + " line")
	}
	return DocJoinHeadBody(seedHead, body), nil
}

// DELIBERATELY LENIENT on a no-marker row (all body), matching DocRendered;
// the strict reading is eventNoticeText's.
func bootDocBodyOf(spec bootDocSpec, text string) string {
	if !spec.Split {
		return text
	}
	_, body, split := DocSplitHeadBody(text)
	if !split {
		return text
	}
	return body
}

// 🔴 NO CAP IS CHECKED HERE: this is the path that has to work when a bad edit
// has stopped agents booting, and nobody is online to ask.
func (s *apiServer) resetBootDoc(w http.ResponseWriter, r *http.Request, spec bootDocSpec) {
	// 405, not 200: reset is a WRITE (tombstone, history revision,
	// global_context frame) for a document that can never leave the default.
	if spec.ReadOnly {
		writeError(w, http.StatusMethodNotAllowed, bootDocReadOnlyRefusal(spec))
		return
	}
	seedMD, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		internalError(w, err)
		return
	}
	if !hasSeed {
		writeError(w, http.StatusNotFound,
			"document '"+spec.Kind+"/"+spec.Key+"' has no shipped default to reset to")
		return
	}
	current, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	if _, err := s.writeBootDoc(r, spec, current,
		BootDocument{Kind: spec.Kind, Key: spec.Key, Tombstoned: true}, seedMD); err != nil {
		internalError(w, err)
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bootDocReceiptOf(dto))
}

func (s *apiServer) HandleGetSystemInteractionApiSystemInteractionGet(w http.ResponseWriter, r *http.Request) {
	dto, err := s.foldBootDocDTO(s.systemInteractionSpec())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *apiServer) HandleReplaceSystemInteractionApiSystemInteractionPost(w http.ResponseWriter, r *http.Request) {
	var in BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &in, "body") {
		return
	}
	s.replaceBootDoc(w, r, s.systemInteractionSpec(), in.Body,
		in.AllowShrink != nil && *in.AllowShrink)
}

func (s *apiServer) HandleResetSystemInteractionApiSystemInteractionResetPost(w http.ResponseWriter, r *http.Request) {
	s.resetBootDoc(w, r, s.systemInteractionSpec())
}

func (s *apiServer) HandleGetOffboardApiOffboardGet(w http.ResponseWriter, r *http.Request) {
	dto, err := s.foldBootDocDTO(s.offboardSpec())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *apiServer) HandleReplaceOffboardApiOffboardPost(w http.ResponseWriter, r *http.Request) {
	var in BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &in, "body") {
		return
	}
	s.replaceBootDoc(w, r, s.offboardSpec(), in.Body,
		in.AllowShrink != nil && *in.AllowShrink)
}

func (s *apiServer) HandleResetOffboardApiOffboardResetPost(w http.ResponseWriter, r *http.Request) {
	s.resetBootDoc(w, r, s.offboardSpec())
}

func (s *apiServer) HandleGetBootSequenceApiBootSequenceRuntimeKeyGet(w http.ResponseWriter, r *http.Request, runtimeKey string) {
	spec, ok := s.bootSequenceSpecFor(runtimeKey)
	if !ok {
		writeUnknownBootSequence(w, runtimeKey)
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *apiServer) HandleReplaceBootSequenceApiBootSequenceRuntimeKeyPost(w http.ResponseWriter, r *http.Request, runtimeKey string) {
	spec, ok := s.bootSequenceSpecFor(runtimeKey)
	if !ok {
		writeUnknownBootSequence(w, runtimeKey)
		return
	}
	var in BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &in, "body") {
		return
	}
	s.replaceBootDoc(w, r, spec, in.Body, in.AllowShrink != nil && *in.AllowShrink)
}

func (s *apiServer) HandleResetBootSequenceApiBootSequenceRuntimeKeyResetPost(w http.ResponseWriter, r *http.Request, runtimeKey string) {
	spec, ok := s.bootSequenceSpecFor(runtimeKey)
	if !ok {
		writeUnknownBootSequence(w, runtimeKey)
		return
	}
	s.resetBootDoc(w, r, spec)
}

func writeUnknownBootSequence(w http.ResponseWriter, runtimeKey string) {
	writeError(w, http.StatusNotFound,
		"no boot sequence for runtime '"+runtimeKey+"' — the runtimes with their own boot sequence are '"+
			bootSequenceKeyClaude+"' and '"+bootSequenceKeyCodex+"'")
}

func bootDocReadOnlyRefusal(spec bootDocSpec) string {
	return "the " + spec.DocName + " is a read-only document — it is shown so you can " +
		"see what agents are told, but no caller may edit it and there is no version " +
		"of it other than the shipped one; nothing was written"
}

// 404 rather than 400 on purpose, unlike the document-history siblings: here
// the pair addresses A DOCUMENT, and one this server lacks is not found.
func (s *apiServer) genericBootDocSpec(w http.ResponseWriter, kind, key string) (bootDocSpec, bool) {
	spec, ok := s.bootDocSpecFor(kind, key)
	if !ok {
		writeError(w, http.StatusNotFound, unknownBootDocKeyMsg(kind, key))
		return bootDocSpec{}, false
	}
	return spec, true
}

func (s *apiServer) HandleGetBootDocApiBootDocsKindKeyGet(w http.ResponseWriter, r *http.Request, kind BootDocKind, key string) {
	spec, ok := s.genericBootDocSpec(w, string(kind), key)
	if !ok {
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *apiServer) HandleReplaceBootDocApiBootDocsKindKeyPost(w http.ResponseWriter, r *http.Request, kind BootDocKind, key string) {
	spec, ok := s.genericBootDocSpec(w, string(kind), key)
	if !ok {
		return
	}
	var in BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &in, "body") {
		return
	}
	s.replaceBootDoc(w, r, spec, in.Body, in.AllowShrink != nil && *in.AllowShrink)
}

func (s *apiServer) HandleResetBootDocApiBootDocsKindKeyResetPost(w http.ResponseWriter, r *http.Request, kind BootDocKind, key string) {
	spec, ok := s.genericBootDocSpec(w, string(kind), key)
	if !ok {
		return
	}
	s.resetBootDoc(w, r, spec)
}
