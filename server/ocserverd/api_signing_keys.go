package main

// Owner-only and off the MCP surface (principalOwner + MCPExclude, like the
// password rows): an admin_agent could otherwise rotate the key that governs it
// or remove the one its own credential is signed under.

import (
	"errors"
	"net/http"
)

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

func (s *apiServer) HandleSigningKeysApiAuthSigningKeysGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.signingKeysDTO())
}

func (s *apiServer) HandleSigningKeyRotateApiAuthSigningKeysRotatePost(w http.ResponseWriter, r *http.Request) {
	if _, err := s.keys.rotate(s.dal); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.signingKeysDTO())
}

// No undo: every token AND every attachment share link signed by the key stops
// verifying on return — share links go with it by owner ruling rc-cf9c27c07442.
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
