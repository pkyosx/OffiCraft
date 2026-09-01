package main

// api_lore_governance.go — T-33. The two routes that put lore retirement and
// revival within reach of something other than a test.
//
// 🔴 WHY THIS FILE EXISTS AT ALL. dal_lore_governance.go shipped the whole
// governance act — the three reasons, the owner split, the journal row, the
// revival — and NOTHING called it. Its own header said so: the gate was real,
// tested, and driven only by tests. A gate nothing can reach is not guarding
// production, it is a description of a gate. These two handlers are the
// difference.
//
// 🔴 THE SPLIT IS NOT RE-DERIVED HERE, AND THAT IS THE POINT. Who may file
// which retirement reason is answered by loreRetireNeedsOwner() in the DAL and
// by nothing else. This layer supplies the two facts only it can know — WHO is
// asking (from the verified token, never from the body) and whether that
// principal is the owner — and hands them down. A second copy of the switch
// here would be a second answer to one question, and the two would drift the
// first time a reason is added.
//
// 🔴 THE ROUTE FLOOR AND THE REASON SPLIT ARE TWO DIFFERENT GATES, deliberately
// stacked rather than merged:
//
//   - `Requires` (routes.go) is the ladder floor. Retiring is an act of an
//     AGENT curating what it knows, so the retire row sits at principalAgent —
//     a machine (a warden) is not a governance principal and is refused before
//     any handler runs. Revive sits at principalOwner.
//   - loreRetireNeedsOwner is the per-REASON gate, and the floor cannot express
//     it: one route admits `expired` from an agent and refuses `falsified` from
//     the same caller in the same request shape.
//
// ⚠️ Revive is therefore gated TWICE (owner floor + the DAL's own owner check)
// and that redundancy is kept on purpose: the DAL check is what makes the
// function safe for any future caller, and the route floor is what makes the
// refusal visible at the door instead of after a body has been parsed.

import (
	"errors"
	"net/http"
)

// writeLoreGovernanceError turns the DAL's named errors into the wire.
//
// Matching on named errors rather than on driver wording is the whole reason
// dal_lore_governance.go declares them: a status code derived from a message
// string changes the day somebody rewords the message.
func writeLoreGovernanceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrLoreEntryUnknown):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrLoreRetireOwnerOnly),
		errors.Is(err, ErrLoreReviveOwnerOnly),
		errors.Is(err, ErrLoreActorBlank):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrLoreRetireReasonUnknown):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrLoreEntryNotRetired):
		writeError(w, http.StatusConflict, err.Error())
	default:
		internalError(w, err)
	}
}

// writeLoreGovernanceReceipt answers with the state the entry is in NOW plus
// the journal row that was just written.
//
// 🔴 IT READS BOTH BACK RATHER THAN ECHOING WHAT THE CALLER SENT. An echo would
// answer "retired" for a write that did not happen, which is the one thing a
// receipt exists to rule out; and the reason has to come from the journal
// because that is where it lives — the entry carries no reason column, on
// purpose (an entry can be retired, revived and retired again for a different
// reason, and a column would only remember the last one).
func (s *apiServer) writeLoreGovernanceReceipt(w http.ResponseWriter, entryID string) {
	entry, err := s.dal.GetLoreEntry(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	if entry == nil {
		writeError(w, http.StatusNotFound, "lore entry '"+entryID+"' not found")
		return
	}
	event, err := s.dal.LatestLoreGovernanceEvent(entryID)
	if err != nil {
		internalError(w, err)
		return
	}
	if event == nil {
		// The act and its journal row are one transaction, so this is
		// unreachable through the two handlers below. It is answered as an
		// internal error rather than as an empty receipt because a governance
		// act with no record is precisely the state this whole design refuses
		// to produce, and reporting it as success would hide it.
		internalError(w, errors.New("lore: the governance act left no journal row for "+entryID))
		return
	}
	writeJSON(w, http.StatusOK, LoreGovernanceDTO{
		EntryId:    entry.ID,
		Status:     entry.Status,
		Kind:       event.Kind,
		Reason:     event.Reason,
		ReplacedBy: event.ReplacedBy,
		ActorId:    event.ActorID,
		CreatedTs:  event.CreatedTS,
	})
}

// HandleRetireLoreEntryApiLoreEntriesEntryIdRetirePost — POST
// /api/lore/entries/{entry_id}/retire.
//
// The caller identity is the VERIFIED token subject (currentActor) and the
// owner test is the resolved principal class; neither is readable from the
// body, so "who is asking" cannot be asserted by the asker.
//
// ⚠️ principalOwner AND NOT principalAtLeast(…, principalAdminAgent): the owner
// ruling is about the OWNER's judgement of truth, and an admin agent is still
// an agent. Widening this to the admin class would quietly hand the falsified
// verdict to the office assistant.
func (s *apiServer) HandleRetireLoreEntryApiLoreEntriesEntryIdRetirePost(w http.ResponseWriter, r *http.Request, entryID string) {
	var body LoreRetireDTO
	if !decodeJSONBodyStrict(w, r, &body, "reason") {
		return
	}
	err := s.dal.RetireLoreEntry(
		entryID, body.Reason, currentActor(r), strOrEmpty(body.ReplacedBy),
		s.principalOfRequest(r) == principalOwner, nowSecs())
	if err != nil {
		writeLoreGovernanceError(w, err)
		return
	}
	s.writeLoreGovernanceReceipt(w, entryID)
}

// HandleReviveLoreEntryApiLoreEntriesEntryIdRevivePost — POST
// /api/lore/entries/{entry_id}/revive.
//
// 🔑 This route is what lets retirement be described as reversible. Without it
// "retire is not a delete, it can come back" was a sentence with nothing behind
// it — and the design says in as many words that anything relying on
// retirement being reversible must not ship ahead of it.
func (s *apiServer) HandleReviveLoreEntryApiLoreEntriesEntryIdRevivePost(w http.ResponseWriter, r *http.Request, entryID string) {
	var body LoreReviveDTO
	if !decodeJSONBodyStrict(w, r, &body) {
		return
	}
	err := s.dal.ReviveLoreEntry(
		entryID, currentActor(r), strOrEmpty(body.Reason),
		s.principalOfRequest(r) == principalOwner, nowSecs())
	if err != nil {
		writeLoreGovernanceError(w, err)
		return
	}
	s.writeLoreGovernanceReceipt(w, entryID)
}
