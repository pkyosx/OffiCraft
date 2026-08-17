package main

// api_bootdocs.go — the two boot-context document kinds the owner may now
// edit (T-791e):
// 系統互動 (system_interaction/global) and 啟動程序 (boot_sequence/claude,
// boot_sequence/codex).
//
// WHY THIS EXISTS (owner, 2026-08-13, verbatim): 「我們可以把系統互動改成可以修改
// 嗎 跟銀月的 insight 一樣是有 history / restore to default」「不用每次都改 code」
// 「啟動程序也是一樣」. Of the three segments the boot context is assembled from,
// only the middle one (使用者自訂) had an owner-editable representation; the other
// two were go:embed seeds, so correcting one sentence cost a release.
//
// 🔴 THREE DOCUMENTS, NOT ONE WITH A VARIANT FIELD — and the reason is NOT that
// the texts differ. Step 3 of the two boot sequences says OPPOSITE things (the
// claude one tells the agent to mount its own `ocagent listen`; the codex one
// forbids exactly that, because the App Server sidecar owns the listener).
// Serving the wrong one leaves the agent unable to come online, and that failure
// is SILENT: nothing that never boots is around to report it. Which is also why
// the runtime→document choice is made in exactly one place
// (bootSequenceSeedName / bootSequenceDocKey, assets.go) and never re-derived.
//
// 🔴 EDITING NEVER TOUCHES THE SEED. The stored edit is an OVERLAY row; the
// factory text stays in the binary's go:embed copy. "Restore to default" is
// therefore answered from a source no write path can reach, and it needs no
// agent, no MCP client and no member identity — the cockpit's owner token alone
// walks the whole way back (api_bootdocs_reset_t791e_test.go pins that: an owner
// token whose sub is on nobody's roster).

import (
	"net/http"
	"strconv"
	"unicode/utf8"
)

// bootDocSpec addresses ONE editable boot-context block: which document-history
// series it is, which seed file backs it, and which cap judges its writes.
// Resolved once per request and passed around, so the three handlers of a block
// cannot disagree about any of the four.
type bootDocSpec struct {
	Kind     string
	Key      string
	SeedFile string
	Cap      int
	// DocName is what a refusal calls this document. It goes into
	// docCapRefusal, which is the message a caller reads when its write is
	// rejected — naming the wrong document sends the reader to edit the wrong
	// one (the defect insightWriteAuthz exists to avoid on the authz face).
	DocName string
}

func (s *apiServer) systemInteractionSpec() bootDocSpec {
	return bootDocSpec{
		Kind:     docKindSystemInteraction,
		Key:      systemInteractionDocKey,
		SeedFile: systemInteractionSeedMD,
		Cap:      s.systemInteractionCap(),
		DocName:  "system interaction block",
	}
}

// offboardSpec resolves the 下線程序 document (T-c9c0). Singleton shape, key
// `global`, exactly like systemInteractionSpec: there is no runtime axis here on
// purpose — being collected is the same procedure for every agent.
func (s *apiServer) offboardSpec() bootDocSpec {
	return bootDocSpec{
		Kind:     docKindOffboard,
		Key:      offboardDocKey,
		SeedFile: offboardSeedMD,
		Cap:      s.offboardCap(),
		DocName:  "offboard sequence",
	}
}

// bootSequenceSpecFor resolves the boot-sequence document for a runtime key as
// it arrives on the URL. ok=false means the key names no document this server
// serves — the caller answers 404 rather than quietly falling back to claude,
// because falling back is precisely how a codex reader ends up holding the
// sequence that keeps it from booting.
func (s *apiServer) bootSequenceSpecFor(runtimeKey string) (bootDocSpec, bool) {
	seed, ok := bootSequenceSeedForKey(runtimeKey)
	if !ok {
		return bootDocSpec{}, false
	}
	return bootDocSpec{
		Kind:     docKindBootSequence,
		Key:      runtimeKey,
		SeedFile: seed,
		Cap:      s.bootSequenceCap(),
		DocName:  "boot sequence (" + runtimeKey + ")",
	}, true
}

// bootDocSpecFor resolves ANY (kind, key) pair naming an editable boot-context
// block — the form the document-history faces address documents in. ok=false
// means the pair names none of the three.
func (s *apiServer) bootDocSpecFor(kind, key string) (bootDocSpec, bool) {
	switch kind {
	case docKindSystemInteraction:
		if key != systemInteractionDocKey {
			return bootDocSpec{}, false
		}
		return s.systemInteractionSpec(), true
	case docKindBootSequence:
		return s.bootSequenceSpecFor(key)
	case docKindOffboard:
		if key != offboardDocKey {
			return bootDocSpec{}, false
		}
		return s.offboardSpec(), true
	}
	return bootDocSpec{}, false
}

// bootDocHistoryKeyKnown is bootDocSpecFor's server-free half: does this
// (kind, key) name one of the three documents at all? The document-history
// faces ask this BEFORE they list or restore, so an address this server does
// not serve is refused rather than answered with an empty version list — "you
// used the wrong key" and "this document has no versions yet" must not look the
// same.
func bootDocHistoryKeyKnown(kind, key string) bool {
	switch kind {
	case docKindSystemInteraction:
		return key == systemInteractionDocKey
	case docKindBootSequence:
		_, ok := bootSequenceSeedForKey(key)
		return ok
	case docKindOffboard:
		return key == offboardDocKey
	}
	return false
}

// unknownBootDocKeyMsg names the keys that DO exist for this kind, for the same
// reason writeUnknownBootSequence does: a caller holding a typo needs to be able
// to tell it from a document that is simply empty.
//
// 🔴 SWITCH, NOT AN IF/ELSE: it used to answer the system-interaction key for
// every kind that was not boot_sequence, so a fourth kind would silently have
// been described by the wrong key. A kind nobody taught it now says so instead
// of naming a key that does not belong to it.
func unknownBootDocKeyMsg(kind, key string) string {
	var want string
	switch kind {
	case docKindSystemInteraction:
		want = "'" + systemInteractionDocKey + "'"
	case docKindBootSequence:
		want = "'" + bootSequenceKeyClaude + "' or '" + bootSequenceKeyCodex + "'"
	case docKindOffboard:
		want = "'" + offboardDocKey + "'"
	default:
		return "document history kind '" + kind + "' names no editable document on this server"
	}
	return "document history key '" + key + "' does not name a " + kind + " document — the key is " + want
}

// foldBootDocDTO folds one block: overlay ⊕ the embedded seed.
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
	return &bootDocDTO{
		SizeChars:     utf8.RuneCountInString(text),
		CapChars:      spec.Cap,
		Kind:          spec.Kind,
		Key:           spec.Key,
		Text:          text,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     isDefault,
		HasSeed:       hasSeed,
	}, nil
}

// systemInteractionText / bootSequenceText are what the BOOT FOLDS read
// (buildBootContext for staff, worker_sharedcore.go for outsource). They are the
// reason this ticket is worth anything: before T-791e both were bare
// readSeedFile calls, so an edit could exist in the database and still never
// reach an agent.
//
// The runtime→document choice goes through bootSequenceDocKey, which reads the
// answer of bootSequenceSeedName instead of testing the runtime again — one
// decision point for staff and outsource alike.
func (s *apiServer) systemInteractionText() (string, error) {
	dto, err := s.foldBootDocDTO(s.systemInteractionSpec())
	if err != nil {
		return "", err
	}
	return dto.Text, nil
}

// offboardText is what the SERVER carries into the offboard notice itself
// (T-a9d6): the owner ruled that the steps must ride the notice the server
// pushes, not be fetched back by the agent — 「改回真的推播」. It answers "" on
// any fault, and every caller degrades to the sentence alone rather than going
// silent: losing the checklist is survivable, losing the notice is not.
func (s *apiServer) offboardText() string {
	dto, err := s.foldBootDocDTO(s.offboardSpec())
	if err != nil || dto == nil {
		return ""
	}
	return dto.Text
}

func (s *apiServer) bootSequenceText(runtime string) (string, error) {
	spec, ok := s.bootSequenceSpecFor(bootSequenceDocKey(runtime))
	if !ok {
		// Unreachable by construction: bootSequenceDocKey only ever answers a
		// key bootSequenceSeedForKey accepts. Fail closed rather than serve a
		// blank boot sequence, which would look like a successful boot.
		return "", errNotFound
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		return "", err
	}
	return dto.Text, nil
}

// bootDocSnapshotIn is what SaveWithDocumentHistory calls from INSIDE the write
// transaction — the same posture insightSnapshotIn takes, and for the same
// reason: the retained revision must be the state THIS write replaced, not a
// value the handler folded earlier, or two racing writers retain one common
// ancestor and the version written between them becomes unrecoverable.
func bootDocSnapshotIn(kind, key string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
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

// publishBootDoc fans the change on the `global_context` topic.
//
// Deliberately NOT a new topic: the closed vocabulary is enforced at the publish
// seam and a topic outside it is dropped SILENTLY (see sseTopics), so adding one
// means teaching every consumer at once or fanning nothing at all. All three
// blocks are one surface to a reader — the cockpit's 全域情境 pane renders them
// together — so the frame that already means "that surface moved, re-read it" is
// the honest one to send. Same key as the 使用者自訂 writes for the same reason.
func (s *apiServer) publishBootDoc(r *http.Request) {
	s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil,
		audienceOwnerOnly(), requestTrigger(r))
}

// writeBootDoc is the one write path shared by replace and reset, and the one
// place the no-op rule lives.
//
// 🔴 A WRITE THAT CHANGES NOTHING RETAINS NOTHING (owner's ruling for these three
// documents). These blocks are now editable from a text box, so idle saves are
// expected — and every idle save that retained a revision would push the version
// the owner actually wants ("the one from before I broke it") one step closer to
// falling off the end of the list. The comparison is on the FOLDED text, so
// saving the seed's own bytes back over an untouched document is a no-op too.
// role_def and the task manual already work this way (roleDefHistoryStreams /
// taskManualHistoryStreams); global_context deliberately does NOT, and that is a
// known gap on that document, not a precedent to copy.
//
// Returns whether anything was written.
func (s *apiServer) writeBootDoc(r *http.Request, spec bootDocSpec, current *bootDocDTO, next BootDocument, nextText string) (bool, error) {
	// "Nothing changed" is BOTH halves: the text a reader would get, and whether
	// the document reads as the shipped default afterwards (next.Tombstoned) as
	// it does now (current.IsDefault). Comparing only the text would swallow the
	// one gesture that changes nothing visible but everything about the next
	// reset — adopting the seed's own bytes as an edit — and comparing only the
	// flag would let a genuine rewrite pass as a no-op.
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

// replaceBootDoc is the whole-document replace shared by both blocks.
func (s *apiServer) replaceBootDoc(w http.ResponseWriter, r *http.Request, spec bootDocSpec, text string, allowShrink bool) {
	current, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	// Wipe guard, the posture replace_global_context / replace_insight carry:
	// emptying a document that had content has to be said out loud. It matters
	// more here than anywhere else — an empty boot sequence is not a small
	// document, it is an agent with no instructions.
	if !allowShrink && WholeDocWipeBlocked(current.Text, text) {
		writeError(w, http.StatusBadRequest,
			"this would replace the existing "+spec.DocName+" with an empty one — pass allow_shrink=true "+
				"if that is intended, or reset it to the shipped default; nothing was written")
		return
	}
	// Hard cap, checked UNCONDITIONALLY: allow_shrink governs the opposite
	// direction and is not a bypass. The refusal names three numbers (what you
	// wrote, the cap, what is stored) because being refused is otherwise the
	// only way to learn any of them.
	if DocCapBlocked(spec.Cap, current.Text, text) {
		writeError(w, http.StatusBadRequest, docCapRefusal(spec.Cap, spec.DocName, current.Text, text))
		return
	}
	if _, err := s.writeBootDoc(r, spec, current,
		BootDocument{Kind: spec.Kind, Key: spec.Key, Text: text, Tombstoned: false}, text); err != nil {
		internalError(w, err)
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// resetBootDoc tombstones the overlay so the folded read falls back to the
// SHIPPED seed.
//
// 🔴 NO CAP IS CHECKED HERE, matching reset_role and reset_insight. The factory
// text is part of the product, not something the caller wrote, so a ceiling the
// owner set afterwards must never be able to block the way back to it. This is
// the path that has to work when a bad edit has stopped agents booting, and at
// that moment there is nobody online to ask.
//
// 404 when the seed file is missing: there must be a factory version to reset
// TO, and the same 404 is what GET .../seed answers, so a surface offering the
// reset and a surface offering the comparison agree.
func (s *apiServer) resetBootDoc(w http.ResponseWriter, r *http.Request, spec bootDocSpec) {
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
	writeJSON(w, http.StatusOK, dto)
}

// GET /api/system-interaction — the folded 系統互動 block.
func (s *apiServer) HandleGetSystemInteractionApiSystemInteractionGet(w http.ResponseWriter, r *http.Request) {
	dto, err := s.foldBootDocDTO(s.systemInteractionSpec())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/system-interaction — whole-document replace ({text}).
func (s *apiServer) HandleReplaceSystemInteractionApiSystemInteractionPost(w http.ResponseWriter, r *http.Request) {
	var body BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	s.replaceBootDoc(w, r, s.systemInteractionSpec(), body.Text,
		body.AllowShrink != nil && *body.AllowShrink)
}

// POST /api/system-interaction/reset — back to the shipped seed.
func (s *apiServer) HandleResetSystemInteractionApiSystemInteractionResetPost(w http.ResponseWriter, r *http.Request) {
	s.resetBootDoc(w, r, s.systemInteractionSpec())
}

// GET /api/offboard — the folded 下線程序 block.
func (s *apiServer) HandleGetOffboardApiOffboardGet(w http.ResponseWriter, r *http.Request) {
	dto, err := s.foldBootDocDTO(s.offboardSpec())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/offboard — whole-document replace ({text}).
func (s *apiServer) HandleReplaceOffboardApiOffboardPost(w http.ResponseWriter, r *http.Request) {
	var body BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	s.replaceBootDoc(w, r, s.offboardSpec(), body.Text,
		body.AllowShrink != nil && *body.AllowShrink)
}

// POST /api/offboard/reset — back to the shipped seed.
func (s *apiServer) HandleResetOffboardApiOffboardResetPost(w http.ResponseWriter, r *http.Request) {
	s.resetBootDoc(w, r, s.offboardSpec())
}

// GET /api/boot-sequence/{runtime_key} — the folded 啟動程序 block for ONE runtime.
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

// POST /api/boot-sequence/{runtime_key} — whole-document replace ({text}).
func (s *apiServer) HandleReplaceBootSequenceApiBootSequenceRuntimeKeyPost(w http.ResponseWriter, r *http.Request, runtimeKey string) {
	spec, ok := s.bootSequenceSpecFor(runtimeKey)
	if !ok {
		writeUnknownBootSequence(w, runtimeKey)
		return
	}
	var body BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	s.replaceBootDoc(w, r, spec, body.Text, body.AllowShrink != nil && *body.AllowShrink)
}

// POST /api/boot-sequence/{runtime_key}/reset — back to that runtime's shipped seed.
func (s *apiServer) HandleResetBootSequenceApiBootSequenceRuntimeKeyResetPost(w http.ResponseWriter, r *http.Request, runtimeKey string) {
	spec, ok := s.bootSequenceSpecFor(runtimeKey)
	if !ok {
		writeUnknownBootSequence(w, runtimeKey)
		return
	}
	s.resetBootDoc(w, r, spec)
}

// writeUnknownBootSequence NAMES the runtimes that exist. A bare "not found"
// would leave a caller that typed "Codex" or passed an empty string unable to
// tell a typo from a server that has no boot sequences at all.
func writeUnknownBootSequence(w http.ResponseWriter, runtimeKey string) {
	writeError(w, http.StatusNotFound,
		"no boot sequence for runtime '"+runtimeKey+"' — the runtimes with their own boot sequence are '"+
			bootSequenceKeyClaude+"' and '"+bootSequenceKeyCodex+"'")
}
