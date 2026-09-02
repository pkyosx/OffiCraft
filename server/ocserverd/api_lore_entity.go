package main

// api_lore_entity.go — T-33. The three routes that make the subject review
// queue something the cockpit can see and act on.
//
// 🔴 WHY THESE EXIST. `entity.pending = 1` has been written on every minted
// subject since the write route landed, and nothing served it. The visible
// symptom was not an error and not an empty screen: an entry filed only against
// a pending subject IS in the table, IS returned by a direct read, and is
// missing from every agent's boot subject directory — which filters `pending =
// 0`. So the store grew names nobody had approved, holding entries nobody could
// reach by subject, and every count agreed with every other count.
//
// 🔴 THIS FILE HOLDS NO POLICY, the same rule api_lore_governance.go states.
// Whether an entity may be approved, whether a merge target is legal, what a
// merge writes — all of it is in dal_lore_entity.go, where the transaction is.
// This layer supplies the one fact only it can know (WHO is asking, from the
// verified token) and maps named errors onto status codes.
//
// 🔴 THE QUEUE ROW CARRIES A SUGGESTION AND NOTHING ACTS ON IT. The owner's
// second ruling (2026-09-02) asked for the homework, not for an automatic
// verdict: 「我還是做最後的裁決」. So the list route computes `suggestion` /
// `similar` / `sample_short`, and the two act routes below never read them —
// approving still needs a human on an admin token, and a row suggesting `merge`
// changes nothing until somebody calls merge. A suggestion that could act would
// be an auto-approve wearing a recommendation's clothes.
//
// 🔴 AND THERE IS NO 「駁回」 HANDLER HERE. The owner ruled on approving and on
// merging; nothing has been ruled about discarding a parked name. Adding that
// exit would be this layer deciding it for him, in the direction that destroys
// rows.

import (
	"errors"
	"net/http"
)

// writeLoreEntityError turns the entity seam's named errors into the wire.
//
// The split between 404, 409 and 422 is the one a caller has to act on:
//   - 404 — the id names nothing. Check what you sent.
//   - 409 — the id names something that is not in the state this act needs.
//     The queue has moved on; re-read it.
//   - 422 — the id names something real and the ACT is illegal against it. No
//     amount of re-reading changes that; the request itself is wrong.
func writeLoreEntityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrLoreEntityUnknown):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrLoreEntityNotPending):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrLoreEntityMergeSelf),
		errors.Is(err, ErrLoreEntityTargetPending),
		errors.Is(err, ErrLoreEntityTargetMerged):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrLoreActorBlank):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		internalError(w, err)
	}
}

// writeLoreEntityReceipt answers with the state the entity is in NOW plus the
// journal row that was just written.
//
// 🔴 IT READS BOTH BACK RATHER THAN ECHOING THE REQUEST, for the same reason
// writeLoreGovernanceReceipt does: an echo would answer `pending: false` for a
// write that did not happen, which is the one thing a receipt exists to rule
// out.
func (s *apiServer) writeLoreEntityReceipt(w http.ResponseWriter, entityID string) {
	entity, err := s.dal.GetLoreEntity(entityID)
	if err != nil {
		internalError(w, err)
		return
	}
	if entity == nil {
		writeError(w, http.StatusNotFound, "lore: no subject entity carries the id '"+entityID+"'")
		return
	}
	event, err := s.dal.LatestLoreGovernanceEvent(entityID)
	if err != nil {
		internalError(w, err)
		return
	}
	if event == nil {
		// The act and its journal row are one transaction, so this is
		// unreachable through the two handlers below. It is an internal error
		// rather than an empty receipt because a governance act with no record
		// is precisely the state this design refuses to produce, and reporting
		// it as success would hide it.
		internalError(w, errors.New("lore: the governance act left no journal row for "+entityID))
		return
	}
	writeJSON(w, http.StatusOK, LoreEntityGovernanceDTO{
		EntityId:   entity.ID,
		Canonical:  entity.Canonical,
		Pending:    entity.Pending,
		MergedInto: entity.MergedInto,
		Kind:       event.Kind,
		Reason:     event.Reason,
		ActorId:    event.ActorID,
		CreatedTs:  event.CreatedTS,
	})
}

// HandleListPendingLoreEntitiesApiLoreEntitiesPendingGet — GET
// /api/lore/entities/pending.
//
// The response is a plain array (the /api/roles shape) rather than a wrapper:
// there is no second fact to carry beside the rows, and a one-key envelope
// invented for symmetry is a key every reader has to unwrap forever.
func (s *apiServer) HandleListPendingLoreEntitiesApiLoreEntitiesPendingGet(w http.ResponseWriter, r *http.Request) {
	rows, err := s.dal.ListPendingLoreEntities()
	if err != nil {
		internalError(w, err)
		return
	}
	// Non-nil so the wire carries `[]` rather than `null`: a reader that has to
	// treat null and empty as the same thing eventually treats one of them
	// wrongly. An EMPTY queue is the good state and has to be sayable.
	out := make([]LorePendingEntityRowDTO, 0, len(rows))
	for _, row := range rows {
		// `similar` is non-nil for the same reason the outer slice is, and it
		// carries the extra weight here: an EMPTY similar list is what makes
		// `suggestion: approve` readable — 「nothing in the ontology looks like
		// this」 — and `null` would leave that as something to infer.
		similar := make([]LoreEntitySimilarDTO, 0, len(row.Similar))
		for _, s := range row.Similar {
			similar = append(similar, LoreEntitySimilarDTO{
				EntityId: s.EntityID, Canonical: s.Canonical, Reason: s.Reason,
			})
		}
		out = append(out, LorePendingEntityRowDTO{
			EntityId:    row.ID,
			Canonical:   row.Canonical,
			Type:        row.Type,
			Name:        row.Name,
			CreatedTs:   row.CreatedTS,
			Entries:     row.Entries,
			Suggestion:  row.Suggestion,
			MergeTarget: row.MergeTarget,
			Similar:     similar,
			SampleShort: row.SampleShort,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// HandleApproveLoreEntityApiLoreEntitiesEntityIdApprovePost — POST
// /api/lore/entities/{entity_id}/approve.
//
// The caller identity is the VERIFIED token subject; the CLASS gate is the
// route floor (principalAdminAgent) and is not re-derived here. That is the
// opposite of the retire route on purpose: retirement's split is per-REASON, a
// thing `Requires` has no vocabulary for, whereas the owner's ruling here is
// exactly a principal class and the table says it once.
func (s *apiServer) HandleApproveLoreEntityApiLoreEntitiesEntityIdApprovePost(w http.ResponseWriter, r *http.Request, entityID string) {
	var body LoreEntityApproveDTO
	if !decodeJSONBodyStrict(w, r, &body) {
		return
	}
	if err := s.dal.ApproveLoreEntity(
		entityID, currentActor(r), strOrEmpty(body.Reason), nowSecs()); err != nil {
		writeLoreEntityError(w, err)
		return
	}
	s.writeLoreEntityReceipt(w, entityID)
}

// HandleMergeLoreEntityApiLoreEntitiesEntityIdMergePost — POST
// /api/lore/entities/{entity_id}/merge.
//
// 🔑 THIS IS THE REPAIR APPROVE CANNOT MAKE. The queue's commonest real content
// is not a bad name, it is a SECOND name for something already in the ontology
// — `repo:offcraft` beside `repo:officraft`. Approving that publishes the
// duplicate; the boot directory then carries one subject twice under two names,
// and because the directory is truncated the duplicate also eats a slot a real
// subject needed.
func (s *apiServer) HandleMergeLoreEntityApiLoreEntitiesEntityIdMergePost(w http.ResponseWriter, r *http.Request, entityID string) {
	var body LoreEntityMergeDTO
	if !decodeJSONBodyStrict(w, r, &body, "into") {
		return
	}
	if err := s.dal.MergeLoreEntity(
		entityID, body.Into, currentActor(r), strOrEmpty(body.Reason), nowSecs()); err != nil {
		writeLoreEntityError(w, err)
		return
	}
	s.writeLoreEntityReceipt(w, entityID)
}
