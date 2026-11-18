package main

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	sharedDocNameMaxChars        = 128
	sharedDocDescriptionMaxChars = 1024
	sharedDocInlineTextMaxBytes  = 256 << 10
)

const (
	sharedDocTextOmittedBinary   = "binary"
	sharedDocTextOmittedTooLarge = "too_large"
)

// Every shared-doc write fans to every agent connection: a shared document
// belongs to no one member (spec/sse.md §4).
func (s *apiServer) publishSharedDoc(id, op, trigger string) {
	s.hub.Publish("shared_doc", op, "shared_doc", wireOwnerID+"::"+id,
		map[string]any{"id": id}, audienceAll(), trigger)
}

func sharedDocTextOrError(w http.ResponseWriter, field, value string, max int) bool {
	if n := utf8.RuneCountInString(value); n > max {
		writeError(w, http.StatusBadRequest, "shared doc "+field+" is "+
			strconv.Itoa(n)+" chars, over the "+strconv.Itoa(max)+"-char limit")
		return false
	}
	return true
}

// sharedDocKindPhrase is "a file", "an image" or "a link".
func sharedDocKindPhrase(kind string) string {
	if kind == ArtifactKindImage {
		return "an " + kind
	}
	return "a " + kind
}

func sharedDocNothingToReplace(kind string) string {
	field := "attachment_id"
	if kind == ArtifactKindLink {
		field = "url"
	}
	return "nothing to replace: send " + field + " for " + sharedDocKindPhrase(kind) +
		" shared doc, a name, or a description"
}

func sharedDocKindRefusal(kind, field string) string {
	return sharedDocKindPhrase(kind) + " shared doc does not take " + field +
		": its kind is fixed, so create a new shared doc instead"
}

// sharedDocContentOrError resolves the content fields of a create or replace for
// kind: a link mints its uri-list blob (stored by the caller's transaction), a
// file or image must name a blob already in the store.
func (s *apiServer) sharedDocContentOrError(
	w http.ResponseWriter, kind string, isLink bool, urlPtr, attIDPtr *string,
) (string, *ChatAttachment, bool) {
	url, attID := trimmedOrEmpty(urlPtr), trimmedOrEmpty(attIDPtr)
	if isLink {
		if attID != "" {
			writeError(w, http.StatusBadRequest, sharedDocKindRefusal(kind, "attachment_id"))
			return "", nil, false
		}
		if url == "" {
			writeError(w, http.StatusBadRequest, "url is required for a link shared doc")
			return "", nil, false
		}
		if refusal := artifactLinkURLRefusal(url); refusal != "" {
			writeError(w, http.StatusBadRequest, refusal)
			return "", nil, false
		}
		id, minted := mintLinkTargetBlob(url)
		return id, minted, true
	}
	if url != "" {
		writeError(w, http.StatusBadRequest, sharedDocKindRefusal(kind, "url"))
		return "", nil, false
	}
	if attID == "" {
		writeError(w, http.StatusBadRequest,
			"attachment_id is required for "+sharedDocKindPhrase(kind)+" shared doc")
		return "", nil, false
	}
	if isMemberAvatarAttachmentID(attID) {
		writeError(w, http.StatusBadRequest,
			"attachment '"+attID+"' is reserved for a member avatar")
		return "", nil, false
	}
	att, err := s.dal.GetTaskArtifactBlob(attID)
	if err != nil {
		internalError(w, err)
		return "", nil, false
	}
	if att == nil {
		writeError(w, http.StatusBadRequest,
			"attachment '"+attID+"' not found (upload it first via POST /api/chat/attachments)")
		return "", nil, false
	}
	return attID, nil, true
}

func (s *apiServer) sharedDocBlob(attachmentID string) (*ChatAttachment, error) {
	if attachmentID == "" {
		return nil, nil
	}
	return s.dal.GetTaskArtifactBlob(attachmentID)
}

func (s *apiServer) sharedDocDTOOf(doc SharedDoc) (sharedDocDTO, error) {
	att, err := s.sharedDocBlob(doc.AttachmentID)
	if err != nil {
		return sharedDocDTO{}, err
	}
	versions, err := s.dal.ListSharedDocVersions(doc.ID)
	if err != nil {
		return sharedDocDTO{}, err
	}
	return newSharedDocDTO(doc, att, len(versions)), nil
}

func (s *apiServer) writeSharedDocReceipt(w http.ResponseWriter, id string) {
	doc, err := s.dal.GetSharedDoc(id)
	if err != nil {
		internalError(w, err)
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, sharedDocNotFound(id))
		return
	}
	dto, err := s.sharedDocDTOOf(*doc)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sharedDocReceiptDTO{
		ID: dto.ID, UpdatedTS: dto.UpdatedTS, VersionCount: dto.VersionCount,
	})
}

func (s *apiServer) HandleListSharedDocsApiSharedDocsGet(w http.ResponseWriter, r *http.Request) {
	docs, err := s.dal.ListSharedDocs()
	if err != nil {
		internalError(w, err)
		return
	}
	retained, err := s.dal.SharedDocVersionCounts()
	if err != nil {
		internalError(w, err)
		return
	}
	out := sharedDocListDTO{Docs: []sharedDocDTO{}}
	for _, doc := range docs {
		att, err := s.sharedDocBlob(doc.AttachmentID)
		if err != nil {
			internalError(w, err)
			return
		}
		out.Docs = append(out.Docs, newSharedDocDTO(doc, att, retained[doc.ID]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *apiServer) HandleCreateSharedDocApiSharedDocsPost(w http.ResponseWriter, r *http.Request) {
	var body SharedDocCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "kind", "name") {
		return
	}
	kind := trimString(body.Kind)
	if !ValidArtifactKind(kind) {
		writeError(w, http.StatusBadRequest, "kind must be one of file, image, link")
		return
	}
	name, description := trimString(body.Name), trimmedOrEmpty(body.Description)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !sharedDocTextOrError(w, "name", name, sharedDocNameMaxChars) ||
		!sharedDocTextOrError(w, "description", description, sharedDocDescriptionMaxChars) {
		return
	}
	attID, minted, ok := s.sharedDocContentOrError(w, kind, kind == ArtifactKindLink, body.Url, body.AttachmentId)
	if !ok {
		return
	}
	now, actor := nowSecs(), currentActor(r)
	doc := SharedDoc{
		ID: "sd-" + newHexID(12), Kind: kind, AttachmentID: attID,
		Name: name, Description: description,
		CreatedTS: now, CreatedBy: actor, UpdatedTS: now, UpdatedBy: actor,
	}
	if err := s.dal.CreateSharedDoc(doc, minted); err != nil {
		internalError(w, err)
		return
	}
	s.publishSharedDoc(doc.ID, "patch", requestTrigger(r))
	s.writeSharedDocReceipt(w, doc.ID)
}

func (s *apiServer) HandleGetSharedDocApiSharedDocsDocIdGet(w http.ResponseWriter, r *http.Request, docId string) {
	doc, err := s.dal.GetSharedDoc(docId)
	if err != nil {
		internalError(w, err)
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, sharedDocNotFound(docId))
		return
	}
	dto, err := s.sharedDocDTOOf(*doc)
	if err != nil {
		internalError(w, err)
		return
	}
	text, omitted, err := s.sharedDocInlineText(*doc, dto)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sharedDocDetailDTO{sharedDocDTO: dto, Text: text, TextOmitted: omitted})
}

// sharedDocInlineText answers exactly one of the two: the content as text, or
// why it is left out. Only the size-capped read touches the blob's bytes.
func (s *apiServer) sharedDocInlineText(doc SharedDoc, dto sharedDocDTO) (*string, *string, error) {
	omit := func(reason string) (*string, *string, error) { return nil, &reason, nil }
	if doc.Kind == ArtifactKindLink {
		url := dto.URL
		return &url, nil, nil
	}
	base := attachmentMimeBase(dto.Mime)
	if base != "application/json" && !strings.HasPrefix(base, "text/") {
		return omit(sharedDocTextOmittedBinary)
	}
	data, found, err := s.dal.GetChatAttachmentDataWithin(doc.AttachmentID, sharedDocInlineTextMaxBytes)
	if err == ErrChatAttachmentTooLarge {
		return omit(sharedDocTextOmittedTooLarge)
	}
	if err != nil {
		return nil, nil, err
	}
	if !found || !utf8.Valid(data) {
		return omit(sharedDocTextOmittedBinary)
	}
	text := string(data)
	return &text, nil, nil
}

func (s *apiServer) HandleReplaceSharedDocApiSharedDocsDocIdReplacePost(w http.ResponseWriter, r *http.Request, docId string) {
	var body SharedDocReplaceDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	doc, err := s.dal.GetSharedDoc(docId)
	if err != nil {
		internalError(w, err)
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, sharedDocNotFound(docId))
		return
	}
	repl := SharedDocReplacement{ID: docId}
	var minted *ChatAttachment
	if body.Name != nil {
		name := trimString(*body.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest,
				"name cannot be blank: omit it to keep the current name")
			return
		}
		if !sharedDocTextOrError(w, "name", name, sharedDocNameMaxChars) {
			return
		}
		repl.Name = &name
	}
	if body.Description != nil {
		description := trimString(*body.Description)
		if !sharedDocTextOrError(w, "description", description, sharedDocDescriptionMaxChars) {
			return
		}
		repl.Description = &description
	}
	// No content field at all keeps the current content: a name or description
	// edit is still a version of its own, with its own author and time.
	if trimmedOrEmpty(body.Url) == "" && trimmedOrEmpty(body.AttachmentId) == "" {
		if repl.Name == nil && repl.Description == nil {
			writeError(w, http.StatusBadRequest, sharedDocNothingToReplace(doc.Kind))
			return
		}
	} else {
		attID, blob, ok := s.sharedDocContentOrError(w, doc.Kind, doc.Kind == ArtifactKindLink, body.Url, body.AttachmentId)
		if !ok {
			return
		}
		repl.AttachmentID, minted = attID, blob
	}
	repl.UpdatedTS, repl.UpdatedBy = nowSecs(), currentActor(r)
	if err := s.dal.ReplaceSharedDoc(repl, minted); err != nil {
		writeTxError(w, err)
		return
	}
	s.publishSharedDoc(docId, "patch", requestTrigger(r))
	s.writeSharedDocReceipt(w, docId)
}

func (s *apiServer) HandleListSharedDocVersionsApiSharedDocsDocIdVersionsGet(w http.ResponseWriter, r *http.Request, docId string) {
	doc, err := s.dal.GetSharedDoc(docId)
	if err != nil {
		internalError(w, err)
		return
	}
	if doc == nil {
		writeError(w, http.StatusNotFound, sharedDocNotFound(docId))
		return
	}
	versions, err := s.dal.ListSharedDocVersions(docId)
	if err != nil {
		internalError(w, err)
		return
	}
	out := sharedDocVersionListDTO{DocID: docId, Versions: []sharedDocVersionDTO{}}
	for _, v := range versions {
		att, err := s.sharedDocBlob(v.AttachmentID)
		if err != nil {
			internalError(w, err)
			return
		}
		out.Versions = append(out.Versions, newSharedDocVersionDTO(v, att))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *apiServer) HandleRestoreSharedDocVersionApiSharedDocsDocIdVersionsVersionIdRestorePost(w http.ResponseWriter, r *http.Request, docId string, versionId int64) {
	if err := s.dal.RestoreSharedDocVersion(docId, versionId, currentActor(r), nowSecs()); err != nil {
		writeTxError(w, err)
		return
	}
	s.publishSharedDoc(docId, "patch", requestTrigger(r))
	s.writeSharedDocReceipt(w, docId)
}

func (s *apiServer) HandleDeleteSharedDocApiSharedDocsDocIdDelete(w http.ResponseWriter, r *http.Request, docId string) {
	if err := s.dal.DeleteSharedDoc(docId); err != nil {
		writeTxError(w, err)
		return
	}
	s.publishSharedDoc(docId, "remove", requestTrigger(r))
	writeJSON(w, http.StatusOK, sharedDocDeleteReceiptDTO{ID: docId})
}
