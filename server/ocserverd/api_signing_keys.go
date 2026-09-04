package main

// api_signing_keys.go — the three owner actions on the signing-key ring
// (T-62): read it, rotate it, remove a retired key from it.
//
// WHY OWNER-ONLY, AND WHY OFF THE MCP SURFACE. These routes govern the key
// that authenticates every caller, including the agent making the call. An
// admin_agent that could reach them could rotate the key that governs it, or
// remove the one its own credential is signed under. They therefore carry the
// same two labels as the password and second-factor rows in routes.go —
// principalOwner + MCPExclude — for the same reason those do: how the OWNER
// authenticates is never something an agent does on the owner's behalf.
//
// 🔴 NOTHING HERE EVER EMITS KEY MATERIAL. Not the key, not a fingerprint, not
// a hash prefix. keyMeta (keyring.go) has no field that could carry one, and
// the errors below name a key by its id or say nothing at all. See keyring.go's
// header for the reason a hash would be worse than useless: on an install that
// predates the ring the signing key IS a SHA-256 of the owner password, so any
// digest of it is an offline dictionary attack on that password.

import (
	"errors"
	"net/http"
)

// signingKeysDTO is the wire answer for all three routes: the WHOLE ring,
// oldest first. Every mutating call answers with the full ring rather than with
// just what it changed, so the settings page never re-fetches to learn where it
// now stands.
func (s *apiServer) signingKeysDTO() SigningKeysDTO {
	metas := s.keys.snapshot()
	out := SigningKeysDTO{Keys: make([]SigningKeyDTO, 0, len(metas))}
	for _, m := range metas {
		out.Keys = append(out.Keys, SigningKeyDTO{
			KeyId:     m.ID,
			CreatedTs: m.CreatedTS,
			IsSigning: m.IsSigning,
		})
	}
	return out
}

// HandleSigningKeysApiAuthSigningKeysGet lists the ring.
func (s *apiServer) HandleSigningKeysApiAuthSigningKeysGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.signingKeysDTO())
}

// HandleSigningKeyRotateApiAuthSigningKeysRotatePost mints a key and hands
// signing over to it. The outgoing key stays in the ring, still verifying —
// this is the transition, and nothing is revoked here.
//
// It takes effect on the NEXT REQUEST: keyring.rotate swaps the ring the HTTP
// gate is already holding by pointer. No restart, no handler rebuild.
func (s *apiServer) HandleSigningKeyRotateApiAuthSigningKeysRotatePost(w http.ResponseWriter, r *http.Request) {
	if _, err := s.keys.rotate(s.dal); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.signingKeysDTO())
}

// HandleSigningKeyRemoveApiAuthSigningKeysKeyIdRemovePost drops a retired key.
//
// 🔴 THIS IS THE REVOCATION AND IT HAS NO UNDO: every token that key signed,
// and every attachment share link produced under it, stops verifying the moment
// this returns. The owner ruled (card rc-cf9c27c07442) that share links go with
// it rather than being carved out — a key being removed because it may have
// leaked must not leave the file-reading half of its authority alive.
//
// The refusals are deliberately DIFFERENT statuses because they call for
// different actions: 409 means "rotate first, then come back" (the key is still
// signing), 404 means the id names nothing.
func (s *apiServer) HandleSigningKeyRemoveApiAuthSigningKeysKeyIdRemovePost(w http.ResponseWriter, r *http.Request, keyId string) {
	err := s.keys.remove(s.dal, keyId)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, s.signingKeysDTO())
	case errors.Is(err, errRemoveSigningKey):
		writeError(w, http.StatusConflict,
			"key '"+keyId+"' is the one currently signing and cannot be removed — rotate first, then remove it")
	case errors.Is(err, errUnknownKey):
		writeError(w, http.StatusNotFound, "no signing key '"+keyId+"'")
	default:
		internalError(w, err)
	}
}
