package main

// api_diff.go — a comparison is a URL, in two flavours:
//
//	internal   /diff?before=…&after=…              — a signed-in reader
//	external   /diff?before=…&after=…&sig=…        — no login at all
//
// The internal one is a pure function of the two addresses (`ocagent diff`
// prints it without asking the server). The external one is minted by GET
// /api/diff/share-link as a SERVER-RELATIVE path. No expiry and no per-link
// revocation (owner ruling): removing the minting key from the ring
// (keyring.go) voids every comparison link it signed.

import (
	"net/http"
	"net/url"
	"strconv"
)

// ⚠️ cli/ocagent/diff.go builds the internal url from these same spellings
// and cannot import them (separate Go module); its mirror test confronts its
// copy against THIS file's source.
const (
	diffPagePath        = "/diff"
	diffParamBefore     = "before"
	diffParamAfter      = "after"
	diffParamLabelBefor = "label_before"
	diffParamLabelAfter = "label_after"
	diffParamSig        = "sig"
)

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

// optString never trims: a padded address resolves to nothing, so trimming
// would accept what can never draw.
func optString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

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

// Both sides in ONE request on purpose: the sig signs exactly what one
// request returns. The unauthenticated path is shareSigGate (server.go) on
// this row's RouteSpec, not code here.
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

// Shape only: whether an address still resolves is a read-time fact (the
// side's `gone` marker), never a refusal.
func diffSidesSayable(w http.ResponseWriter, before, after string) bool {
	for _, side := range []struct{ name, raw string }{{"before", before}, {"after", after}} {
		if _, msg := parseDiffSide(side.raw); msg != "" {
			writeError(w, http.StatusUnprocessableEntity, "the "+side.name+" side: "+msg)
			return false
		}
	}
	return true
}

func (s *apiServer) resolveDiffSide(raw, label string) DiffSideDTO {
	dto := DiffSideDTO{Address: raw}
	if label != "" {
		dto.Label = &label
	}
	side, msg := parseDiffSide(raw)
	if msg != "" {
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

// diffDocContent answers in the SAME field map a retained revision carries,
// so any two points in time are comparable. false = "this side is gone", not
// an error.
func (s *apiServer) diffDocContent(addr diffDocAddress) (map[string]string, bool, error) {
	switch addr.At {
	case diffAtSeed:
		return s.documentSeedContent(addr.Kind, addr.Key)
	case diffAtCurrent:
		return s.currentDocumentContent(addr.Kind, addr.Key)
	}
	// Reachable: diffAtRevision allows up to 19 digits, past int64, so an
	// unparseable id is "gone" like a pruned revision.
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

// 🔴 Not the `*SnapshotIn` readers (api_document_history.go): those read the
// OVERLAY row and answer EMPTY for a never-edited document. A `current` side
// goes through the same FOLDS the read routes use. documentSeedContent and
// restoreDocumentHistory are the same kind switch.
func (s *apiServer) currentDocumentContent(kind, key string) (map[string]string, bool, error) {
	one := func(field, text string, err error) (map[string]string, bool, error) {
		if err != nil {
			return nil, false, err
		}
		return map[string]string{field: text}, true, nil
	}
	switch kind {
	case "global_context":
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
	case "insight":
		folded, err := s.foldInsightDTO(key)
		if err != nil || folded == nil {
			return nil, false, err
		}
		return one("text", folded.Text, nil)
	case docKindSystemInteraction, docKindBootSequence, docKindOffboard,
		docKindAcceleratedStop, docKindTaskCloseout, docKindTaskReassignPredecessor,
		docKindTaskTakeoverWithPredecessor, docKindTaskUnblocked,
		docKindTaskReadyForDone:
		spec, ok := s.bootDocSpecFor(kind, key)
		if !ok {
			return nil, false, nil
		}
		folded, err := s.foldBootDocDTO(spec)
		if err != nil || folded == nil {
			return nil, false, err
		}
		// `text`, the WHOLE stored document — the same half
		// bootDocHistorySnapshot retains.
		return one("text", folded.Text, nil)
	case docKindTaskManualSop:
		manual, err := s.dal.GetTaskManual(key)
		if err != nil || manual == nil {
			return nil, false, err
		}
		return one("sop_md", manual.SopMD, nil)
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
