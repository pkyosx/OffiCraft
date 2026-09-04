package main

// api_diff.go — a comparison is a URL (T-59).
//
// It used to be an ATTACHMENT: a `application/vnd.officraft.diff` blob holding
// the two addresses, hung on a chat message, a reply card or a task artifact.
// That is gone. Two addresses are not a file, and storing them as one bought a
// blob id, three carrier surfaces and a mime nobody could read, for something a
// link expresses directly.
//
// TWO FLAVOURS OF THE SAME URL, and the difference is exactly one query
// parameter:
//
//	internal   /diff?before=…&after=…              — a signed-in reader
//	external   /diff?before=…&after=…&sig=…        — no login at all
//
// The internal one needs no minting: it is a pure function of the two
// addresses, which is why `ocagent diff` prints it without asking the server
// anything. The external one is minted here (GET /api/diff/share-link),
// mirroring the attachment share link in shape, naming and posture — the server
// mints a SERVER-RELATIVE path and the caller prefixes its own origin.
//
// No expiry and no per-link revocation, on the owner's explicit ruling and
// matching the attachment share link — including the one thing that DOES end
// one: the sig is derived from whichever signing key minted it, so removing
// that key from the ring (keyring.go) voids every comparison link it signed at
// the same instant it voids that key's tokens and file links. Coarse, and a
// person's decision, never a timer's.

import (
	"net/http"
	"net/url"
	"strconv"
)

// The page the two flavours point at, and the parameter names both flavours and
// the data route spell them with.
//
// ⚠️ `cli/ocagent/diff.go` builds the INTERNAL url from these same five
// spellings and cannot import them (separate Go module); its mirror test
// confronts its copy against THIS file's source. Renaming anything here without
// renaming it there reddens that test rather than silently shipping a link the
// cockpit cannot read.
const (
	diffPagePath        = "/diff"
	diffParamBefore     = "before"
	diffParamAfter      = "after"
	diffParamLabelBefor = "label_before"
	diffParamLabelAfter = "label_after"
	diffParamSig        = "sig"
)

// diffPageQuery builds the page URL's query. Empty labels are LEFT OUT rather
// than sent blank — an absent label and a blank one mean the same thing to the
// reader, and the signature covers the canonical four-field form either way, so
// the two cannot come apart.
func diffPageQuery(before, after, labelBefore, labelAfter, sig string) string {
	q := url.Values{diffParamBefore: {before}, diffParamAfter: {after}}
	for name, value := range map[string]string{
		diffParamLabelBefor: labelBefore,
		diffParamLabelAfter: labelAfter,
		diffParamSig:        sig,
	} {
		if value != "" {
			q.Set(name, value)
		}
	}
	return q.Encode()
}

// optString reads an optional query parameter WITHOUT trimming it. Trimming
// would judge a string the reader never sees: a padded address is one nothing
// can resolve, and silently accepting it splits "it was accepted" from "it will
// draw".
func optString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// GET /api/diff/share-link — mint the EXTERNAL link for one comparison. Twin of
// HandleGetChatAttachmentShareLink…: same gate, same server-relative posture, a
// different (domain-separated) label over the SAME ring key — so it signs under
// whichever key signs now, and dies with that key.
func (s *apiServer) HandleGetDiffShareLinkApiDiffShareLinkGet(
	w http.ResponseWriter, r *http.Request, params HandleGetDiffShareLinkApiDiffShareLinkGetParams,
) {
	labelBefore, labelAfter := optString(params.LabelBefore), optString(params.LabelAfter)
	if !diffSidesSayable(w, params.Before, params.After) {
		return
	}
	sig := diffSigForRing(s.keys, params.Before, params.After, labelBefore, labelAfter)
	writeJSON(w, http.StatusOK, DiffShareLinkDTO{
		Url: diffPagePath + "?" + diffPageQuery(params.Before, params.After, labelBefore, labelAfter, sig),
	})
}

// GET /api/diff — resolve BOTH sides in one answer.
//
// The pair is one request on purpose: the sig signs exactly what one request
// returns, so a recipient cannot swap one address or relabel one column and
// still hold a server-minted signature. The unauthenticated path is NOT wired
// here — it is shareSigGate (server.go) on this row's RouteSpec, the same third
// auth path the attachment blob GET uses.
func (s *apiServer) HandleGetDiffApiDiffGet(
	w http.ResponseWriter, r *http.Request, params HandleGetDiffApiDiffGetParams,
) {
	if !diffSidesSayable(w, params.Before, params.After) {
		return
	}
	before, after := s.resolveDiffSide(params.Before, optString(params.LabelBefore)),
		s.resolveDiffSide(params.After, optString(params.LabelAfter))
	writeJSON(w, http.StatusOK, DiffPairDTO{Before: before, After: after})
}

// diffSidesSayable judges the SHAPE of both sides before anything is read, and
// writes the 422 itself. Shape only: whether an address still resolves is a
// read-time fact answered by the side's `gone` marker, never by a refusal.
func diffSidesSayable(w http.ResponseWriter, before, after string) bool {
	for _, side := range []struct{ name, raw string }{{"before", before}, {"after", after}} {
		if _, msg := parseDiffSide(side.raw); msg != "" {
			writeError(w, http.StatusUnprocessableEntity, "the "+side.name+" side: "+msg)
			return false
		}
	}
	return true
}

// resolveDiffSide turns one address into the column the reader draws. It never
// fails: an address that names nothing is that side's honest "this side is
// gone", and the OTHER side still draws.
func (s *apiServer) resolveDiffSide(raw, label string) DiffSideDTO {
	dto := DiffSideDTO{Address: raw}
	if label != "" {
		dto.Label = &label
	}
	side, msg := parseDiffSide(raw)
	if msg != "" { // unreachable: diffSidesSayable ran first. Fail closed anyway.
		return diffGone(dto, msg)
	}
	if side.Doc == nil {
		att, err := s.dal.GetChatAttachment(side.AttachmentID)
		if err != nil {
			return diffGone(dto, "this side could not be read")
		}
		if att == nil {
			return diffGone(dto, "attachment '"+side.AttachmentID+"' is no longer stored")
		}
		text, mime := string(att.Data), att.Mime
		dto.Text, dto.Mime = &text, &mime
		return dto
	}
	content, ok, err := s.diffDocContent(*side.Doc)
	if err != nil {
		return diffGone(dto, "this side could not be read")
	}
	if !ok {
		return diffGone(dto, "'"+raw+"' names no document this station holds — "+
			"a revision that has been pruned, a document with no shipped default, or a kind/key that does not exist")
	}
	text, has := content[side.Doc.Field]
	if !has {
		return diffGone(dto, "'"+side.Doc.Kind+"/"+side.Doc.Key+"' has no field '"+side.Doc.Field+"' at "+side.Doc.At)
	}
	dto.Text = &text
	return dto
}

func diffGone(dto DiffSideDTO, reason string) DiffSideDTO {
	dto.Gone, dto.Text, dto.Mime = true, nil, nil
	dto.GoneReason = &reason
	return dto
}

// diffDocContent answers one document address as the SAME field map a retained
// revision carries — which is what lets one reader compare any two of the three
// points in time against each other.
//
// The second return is "this address names something". False is not an error:
// a pruned revision, a document that ships no default, a kind this station does
// not serve — all of them are the reader's honest "this side is gone".
func (s *apiServer) diffDocContent(addr diffDocAddress) (map[string]string, bool, error) {
	switch addr.At {
	case diffAtSeed:
		return s.documentSeedContent(addr.Kind, addr.Key)
	case diffAtCurrent:
		return s.currentDocumentContent(addr.Kind, addr.Key)
	}
	// REACHABLE, despite diffAtRevision having matched: that pattern allows up
	// to 19 digits and an int64 stops short of the largest of them, so
	// "9999999999999999999" is a sayable address that does not parse. The
	// honest answer is the same one a pruned revision gets — this side is gone
	// — never an error about a number the reader did not know was one.
	id, err := strconv.ParseInt(addr.At, 10, 64)
	if err != nil {
		return nil, false, nil
	}
	history, err := s.dal.GetDocumentHistory(addr.Kind, addr.Key, id)
	if err != nil || history == nil {
		return nil, false, err
	}
	content, err := documentHistoryContent(*history)
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

// currentDocumentContent reads the LIVE content of one editable document in the
// field names its retained revisions carry.
//
// 🔴 THIS IS NOT THE SNAPSHOT FUNCTIONS. The `*SnapshotIn` readers in
// api_document_history.go read the OVERLAY row, so a document nobody has edited
// answers EMPTY there while every live reader answers the seed. A `current`
// side has to say what the document actually holds, so it goes through the same
// FOLDS the read routes use.
//
// It is a kind switch, and so are its two neighbours (documentSeedContent,
// restoreDocumentHistory). Deliberately NOT a fourth enumeration written in
// prose: a kind this switch does not carry answers "this side is gone", which
// is the same sentence a pruned revision gets, rather than an error nobody can
// act on.
func (s *apiServer) currentDocumentContent(kind, key string) (map[string]string, bool, error) {
	one := func(field, text string, err error) (map[string]string, bool, error) {
		if err != nil {
			return nil, false, err
		}
		return map[string]string{field: text}, true, nil
	}
	switch kind {
	case "global_context":
		// The one singleton. Refusing any other key keeps this face honest with
		// the other two: `/seed` and a revision id both miss on a wrong key, and
		// silently answering about the real document would make a wrong address
		// look like a right one.
		if key != "global" {
			return nil, false, nil
		}
		folded, err := s.foldUserContextDTO()
		if err != nil || folded == nil {
			return nil, false, err
		}
		return one("text", folded.Text, nil)
	case "role_definition":
		folded, err := s.foldRoleDefDTO(key)
		if err != nil || folded == nil {
			return nil, false, err
		}
		return one("definition_md", folded.DefinitionMD, nil)
	case "lessons":
		folded, err := s.foldLessonsDTO(key)
		if err != nil || folded == nil {
			return nil, false, err
		}
		return one("text", folded.Text, nil)
	case "insight":
		folded, err := s.foldInsightDTO(key)
		if err != nil || folded == nil {
			return nil, false, err
		}
		return one("text", folded.Text, nil)
	case docKindSystemInteraction, docKindBootSequence, docKindOffboard,
		docKindAcceleratedStop, docKindTaskCloseout, docKindTaskReassignPredecessor,
		docKindTaskTakeoverWithPredecessor, docKindTaskTakeoverFresh, docKindTaskUnblocked:
		spec, ok := s.bootDocSpecFor(kind, key)
		if !ok {
			return nil, false, nil
		}
		folded, err := s.foldBootDocDTO(spec)
		if err != nil || folded == nil {
			return nil, false, err
		}
		// `text`, the WHOLE stored document — the same half bootDocHistorySnapshot
		// retains, so a `current` side and a revision side are comparable.
		return one("text", folded.Text, nil)
	case docKindTaskManualSop, docKindTaskManualLearnings:
		manual, err := s.dal.GetTaskManual(key)
		if err != nil || manual == nil {
			return nil, false, err
		}
		if kind == docKindTaskManualSop {
			return one("sop_md", manual.SopMD, nil)
		}
		return one("learnings", manual.Learnings, nil)
	case docKindTaskDescription, docKindTaskTitle:
		t, err := s.resolveTask(key)
		if err != nil || t == nil {
			return nil, false, nil
		}
		if kind == docKindTaskTitle {
			return one("title", t.Title, nil)
		}
		return one("description", t.Description, nil)
	}
	return nil, false, nil
}
