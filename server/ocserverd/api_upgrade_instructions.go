package main

// api_upgrade_instructions.go — the HTTP face of 換版交代單 (T-79): the owner's
// standing instructions to the assistant, handed to her in a chat message at
// every station upgrade until somebody ticks one off.
//
// THE AUTHZ SHAPE, AND WHY IT IS NOT ONE FLOOR. The route table's `Requires`
// admits admin_agent to all four rows, which is the widest any of them needs;
// two of them are narrower than that and say so HERE, because the ladder cannot
// express "this particular member":
//
//   - WRITING and WITHDRAWING are the OWNER's alone. An instruction is his
//     order to the assistant; if she could author or retract her own, the row
//     would stop being evidence of anything. This is the reason the floor is
//     not simply admin_agent for all four.
//   - TICKING is the owner's or the assistant's. She ticks what she did; he
//     ticks when he did it himself or watched it happen.
//
// Caller identity comes from the verified token (currentActor) and never from
// the request body or a path parameter — server/CLAUDE.md, and the reason is
// that any identity a caller can state is one it can also state falsely.

import (
	"net/http"
	"strings"
)

// upgradeInstructionBodyCap bounds one instruction. The number is not a
// storage concern — SQLite would take far more — it is a READING concern: every
// open instruction is pasted into the upgrade hand-over message, so N of them
// land in the assistant's context at once, and an instruction long enough to
// need a scrollbar is one she will skim. 2,000 runes is about the length of a
// long chat message, which is the shape this is.
const upgradeInstructionBodyCap = 2000

// callerMayWriteUpgradeInstruction — the owner alone. principalOfRequest is the
// ladder position the token proved; nothing here reads the body.
func (s *apiServer) callerMayWriteUpgradeInstruction(r *http.Request) bool {
	return principalAtLeast(s.principalOfRequest(r), principalOwner)
}

// callerMayTickUpgradeInstruction — the owner, or the assistant the
// instructions are addressed to.
func (s *apiServer) callerMayTickUpgradeInstruction(r *http.Request) bool {
	return principalAtLeast(s.principalOfRequest(r), principalOwner) ||
		currentActor(r) == seedMiraID
}

func upgradeInstructionDTO(u UpgradeInstruction) UpgradeInstructionDTO {
	return UpgradeInstructionDTO{
		Id:        u.ID,
		Body:      u.Body,
		CreatedTs: u.CreatedTS,
		CreatedBy: u.CreatedBy,
		Done:      u.Done,
		DoneTs:    u.DoneTS,
		DoneBy:    u.DoneBy,
	}
}

// HandleListUpgradeInstructionsApiUpgradeInstructionsGet answers the whole set,
// open first, with the open count alongside.
//
// The count is computed here rather than left to the caller because it is the
// number that makes this feature's only failure mode visible: an instruction
// nobody ever acts on. A client that derives it from the array gets the same
// answer today and a different one the moment this list is ever paged.
func (s *apiServer) HandleListUpgradeInstructionsApiUpgradeInstructionsGet(w http.ResponseWriter, r *http.Request) {
	rows, err := s.dal.ListUpgradeInstructions()
	if err != nil {
		internalError(w, err)
		return
	}
	dto := UpgradeInstructionsDTO{Instructions: []UpgradeInstructionDTO{}}
	for _, u := range rows {
		if !u.Done {
			dto.OpenCount++
		}
		dto.Instructions = append(dto.Instructions, upgradeInstructionDTO(u))
	}
	writeJSON(w, http.StatusOK, dto)
}

// HandleCreateUpgradeInstructionApiUpgradeInstructionsPost writes one
// instruction. Owner only.
func (s *apiServer) HandleCreateUpgradeInstructionApiUpgradeInstructionsPost(w http.ResponseWriter, r *http.Request) {
	if !s.callerMayWriteUpgradeInstruction(r) {
		writeError(w, http.StatusForbidden,
			"only the owner may write an upgrade instruction")
		return
	}
	var req UpgradeInstructionCreateDTO
	if !decodeJSONBodyRequired(w, r, &req, "body") {
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeError(w, http.StatusUnprocessableEntity,
			"body must not be blank — a blank instruction is handed over at every upgrade while saying nothing")
		return
	}
	if len([]rune(body)) > upgradeInstructionBodyCap {
		writeError(w, http.StatusUnprocessableEntity,
			"body is too long — an instruction is a chat message, not a document")
		return
	}
	u := UpgradeInstruction{
		ID:        "uin-" + newHexID(12),
		Body:      body,
		CreatedTS: nowSecs(),
		CreatedBy: currentActor(r),
	}
	if err := s.dal.PutUpgradeInstruction(u); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, upgradeInstructionDTO(u))
}

// HandleCompleteUpgradeInstructionApiUpgradeInstructionsInstructionIdDonePost
// ticks one instruction off.
//
// 🔴 A SECOND TICK IS A 200, NOT A CONFLICT, and it changes nothing. The
// assistant is handed the whole open set at EVERY upgrade, so two of her
// sessions holding the same instruction is the ordinary case; making the loser
// of that race read as an error would teach her to distrust a correct answer.
// The row itself enforces first-tick-wins, so the read-back below is the state
// after whichever call won — never a report of what this call would have
// written.
func (s *apiServer) HandleCompleteUpgradeInstructionApiUpgradeInstructionsInstructionIdDonePost(w http.ResponseWriter, r *http.Request, instructionId string) {
	if !s.callerMayTickUpgradeInstruction(r) {
		writeError(w, http.StatusForbidden,
			"only the owner or the assistant may tick an upgrade instruction off")
		return
	}
	existing, err := s.dal.GetUpgradeInstruction(instructionId)
	if err != nil {
		internalError(w, err)
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound,
			"upgrade instruction '"+instructionId+"' not found")
		return
	}
	if _, err := s.dal.MarkUpgradeInstructionDone(
		instructionId, currentActor(r), nowSecs()); err != nil {
		internalError(w, err)
		return
	}
	after, err := s.dal.GetUpgradeInstruction(instructionId)
	if err != nil {
		internalError(w, err)
		return
	}
	if after == nil {
		// Withdrawn between the tick and the read-back. Nothing is wrong with
		// the tick; there is simply no row to report, and inventing one from
		// the pre-read copy would answer with a row that no longer exists.
		writeError(w, http.StatusNotFound,
			"upgrade instruction '"+instructionId+"' not found")
		return
	}
	writeJSON(w, http.StatusOK, upgradeInstructionDTO(*after))
}

// HandleDeleteUpgradeInstructionApiUpgradeInstructionsInstructionIdDelete
// withdraws one instruction. Owner only, permanent.
//
// Answers with the row that was removed rather than an empty body: the caller
// asked for it by id and this is the last moment anyone can read what it said.
func (s *apiServer) HandleDeleteUpgradeInstructionApiUpgradeInstructionsInstructionIdDelete(w http.ResponseWriter, r *http.Request, instructionId string) {
	if !s.callerMayWriteUpgradeInstruction(r) {
		writeError(w, http.StatusForbidden,
			"only the owner may withdraw an upgrade instruction")
		return
	}
	existing, err := s.dal.GetUpgradeInstruction(instructionId)
	if err != nil {
		internalError(w, err)
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound,
			"upgrade instruction '"+instructionId+"' not found")
		return
	}
	if _, err := s.dal.DeleteUpgradeInstruction(instructionId); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, upgradeInstructionDTO(*existing))
}
