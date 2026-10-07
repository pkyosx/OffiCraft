package main

// One-call doors that store a deliverable's bytes and pin them in ONE
// transaction. The two-step path (upload, then add_task_artifact with the
// attachment_id) leaves an unreferenced blob whenever the caller never binds,
// and the collector only revisits blobs a delete put on its candidate list, so
// it never finds them. add_task_artifact stays the JSON door for a link and for
// a blob already in the store.
//
// Deliberately OFF the MCP surface (raw-byte body) and with no ocagent
// subcommand — owner ruling rc-6c3c7debcd05. ⚠️ Their only executable coverage
// is the conformance suite, which `go test` does not run.

import (
	"io"
	"net/http"
)

// Guard order mirrors add_task_artifact's: 400 body-shape → 404 task → 403 not
// the executor → 409 terminal task.
func (s *apiServer) HandleUploadTaskArtifactApiTasksTaskIdArtifactsUploadPost(
	w http.ResponseWriter, r *http.Request, taskId string,
	params HandleUploadTaskArtifactApiTasksTaskIdArtifactsUploadPostParams,
) {
	name, description, ok := artifactTextOrError(w, &params.Name, params.Description)
	if !ok {
		return
	}
	if name == "" {
		writeError(w, http.StatusBadRequest,
			"name is required: give this deliverable a short display name")
		return
	}
	att, ok := s.readArtifactUploadBody(w, r, params.Filename)
	if !ok {
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !callerMayEditTaskText(s.dal.GetMember, r, *t) {
		writeError(w, http.StatusForbidden, taskActorRefusal)
		return
	}
	if TaskRecordReadOnly(t.Status) {
		writeError(w, http.StatusConflict, taskFrozenDeliverablesRefusal(*t))
		return
	}
	art := TaskArtifact{
		ID:           "ta-" + newHexID(12),
		TaskID:       t.ID,
		Kind:         artifactKindOfBlob(att),
		AttachmentID: att.ID,
		Name:         name,
		Description:  description,
		CreatedTS:    nowSecs(),
		CreatedBy:    currentActor(r),
	}
	if err := s.dal.PutTaskArtifactMintingBlob(art, att); err != nil {
		internalError(w, err)
		return
	}
	s.publishTask(*t, requestTrigger(r))
	s.writeTaskArtifactReceipt(w, *t, art.ID)
}

func (s *apiServer) HandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPost(
	w http.ResponseWriter, r *http.Request, taskId, artifactId string,
	params HandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPostParams,
) {
	t, art, ok := s.artifactOnTask(w, r, taskId, artifactId, artifactWrite)
	if !ok {
		return
	}
	if art.Kind == ArtifactKindLink {
		writeError(w, http.StatusBadRequest,
			artifactKindRefusal(art.Kind, ArtifactKindFile))
		return
	}
	name, description := art.Name, art.Description
	if params.Name != nil {
		v, _, ok := artifactTextOrError(w, params.Name, nil)
		if !ok {
			return
		}
		if v == "" {
			writeError(w, http.StatusBadRequest,
				"name cannot be blank: omit it to keep the name this deliverable already has")
			return
		}
		name = v
	}
	if params.Description != nil {
		_, v, ok := artifactTextOrError(w, nil, params.Description)
		if !ok {
			return
		}
		description = v
	}
	att, ok := s.readArtifactUploadBody(w, r, params.Filename)
	if !ok {
		return
	}
	if k := artifactKindOfBlob(att); k != art.Kind {
		writeError(w, http.StatusBadRequest, artifactKindRefusal(art.Kind, k))
		return
	}
	next := TaskArtifact{
		ID:           art.ID,
		TaskID:       art.TaskID,
		Kind:         art.Kind,
		AttachmentID: att.ID,
		Name:         name,
		Description:  description,
		CreatedTS:    nowSecs(),
		CreatedBy:    currentActor(r),
	}
	replaced, err := s.dal.ReplaceTaskArtifactMintingBlob(next, att)
	if err != nil {
		internalError(w, err)
		return
	}
	if !replaced {
		writeError(w, http.StatusNotFound, "artifact '"+artifactId+"' not found")
		return
	}
	s.publishTask(*t, requestTrigger(r))
	s.writeTaskArtifactReplaceReceipt(w, *t, next.ID)
}

func (s *apiServer) readArtifactUploadBody(
	w http.ResponseWriter, r *http.Request, filename *string,
) (*ChatAttachment, bool) {
	if r.ContentLength > chatAttachmentMaxBytes {
		writeError(w, http.StatusBadRequest,
			"attachment exceeds the 100 MB size limit")
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, chatAttachmentMaxBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read request body")
		return nil, false
	}
	if len(raw) > chatAttachmentMaxBytes {
		writeError(w, http.StatusBadRequest,
			"attachment exceeds the 100 MB size limit")
		return nil, false
	}
	att, rerr := resolveChatAttachment(raw, trimmedOrEmpty(filename))
	if rerr != nil {
		writeError(w, http.StatusBadRequest, rerr.Error())
		return nil, false
	}
	return att, true
}

// Same file-vs-image read taskArtifactDTO's consumers make.
func artifactKindOfBlob(att *ChatAttachment) string {
	if isImageMime(att.Mime) {
		return ArtifactKindImage
	}
	return ArtifactKindFile
}
