package main

// api_tasks_artifact_upload.go — the ONE-CALL doors that store a deliverable's
// bytes and pin them in the same breath (T-92, owner card rc-210fc77beea1
// option ①).
//
// 🔴 WHY THEY EXIST AT ALL, given add_task_artifact and replace_task_artifact
// already work. Those take an attachment_id, which means the bytes had to be
// uploaded FIRST, by a separate call. That gap is where unreferenced blobs come
// from: a caller that uploads and then does not bind leaves a blob nothing
// points at, and nothing goes looking for it either — the collector only ever
// revisits blobs a delete already put on its candidate list, so it is not a
// sweep and will never find one. Measured on the live store while this ticket
// was open: 565 of 5,303 blobs had no referent at all, 48.8 MiB, 7.0%.
//
// One call has no gap. Both handlers below write the blob and the artifact row
// inside ONE transaction, so either both land or neither does.
//
// They are deliberately OFF the MCP surface (x-mcp include:false), for the same
// reason POST /api/chat/attachments is: the request body is raw bytes, which
// cannot ride inside a JSON tool call without a 4/3× base64 detour through an
// LLM's context. NOTHING SHIPPED IN THIS REPO REACHES THEM: there is no MCP
// tool and no `ocagent` subcommand, so today only a client written directly
// against the REST API — and the conformance suite — can take this door. That
// is the owner's ruling (rc-6c3c7debcd05), not an oversight: a CLI subcommand
// was written for them and then removed. This line used to name ocagent as the
// client, which was never true and is the reason it is spelled out here now.
// add_task_artifact stays the JSON door for a link, and for pinning a blob that
// is ALREADY in the store — that two-step path (ocagent upload, then
// add_task_artifact with the id it prints) is how a local file becomes a
// deliverable.

import (
	"io"
	"net/http"
)

// HandleUploadTaskArtifactApiTasksTaskIdArtifactsUploadPost pins a local file or
// image in one call: the raw body IS the bytes.
//
// Guard order is add's, in the same sequence, because it is the same write with
// a different transport: 400 body-shape (name/description/size) → 404 task →
// 403 not the executor (admin excepted, §14) → 409 terminal task.
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
	att, ok := s.readArtifactUploadBody(w, r, params.Filename, params.Mime)
	if !ok {
		return
	}
	t, err := s.resolveTask(taskId)
	if err != nil {
		writeResolveError(w, err, "task", taskId)
		return
	}
	if !s.callerMayEditTaskText(r, *t) {
		writeError(w, http.StatusForbidden, executorGuardRefusal)
		return
	}
	if TaskIsTerminal(t.Status) {
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

// HandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPost
// swaps a pinned file/image's content from raw bytes, keeping the artifact id.
//
// name/description follow the JSON replace exactly: omitted = carried forward,
// and a cap is checked only against a value actually sent.
func (s *apiServer) HandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPost(
	w http.ResponseWriter, r *http.Request, taskId, artifactId string,
	params HandleUploadReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplaceUploadPostParams,
) {
	t, art, ok := s.artifactOnTask(w, r, taskId, artifactId, artifactWrite)
	if !ok {
		return
	}
	// THE KIND CANNOT CHANGE ACROSS VERSIONS, and a link's content is a url
	// rather than a file — so this door is closed to one, explicitly, instead of
	// quietly turning it into a file.
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
	att, ok := s.readArtifactUploadBody(w, r, params.Filename, params.Mime)
	if !ok {
		return
	}
	// 🔴 The kind of the NEW bytes must match what is pinned. An image replacing
	// a file (or the reverse) is a kind change wearing a content change's
	// clothes, and the id a reader already resolved must not change species.
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

// readArtifactUploadBody reads the raw body under the same size caps and the
// same mime resolution the chat-attachment upload uses — ONE upload mechanism,
// not two — and mints the blob without storing it (the caller's transaction
// does that, together with the pin). ok=false means the 400 is already written.
func (s *apiServer) readArtifactUploadBody(
	w http.ResponseWriter, r *http.Request, filename, mime *string,
) (*ChatAttachment, bool) {
	// Bound the read at cap+1: one extra byte proves over-cap without ever
	// buffering an unbounded body.
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
	att, rerr := resolveChatAttachment(raw, trimmedOrEmpty(filename), trimmedOrEmpty(mime))
	if rerr != nil {
		writeError(w, http.StatusBadRequest, rerr.Error())
		return nil, false
	}
	return att, true
}

// artifactKindOfBlob decides file vs image from the blob's mime — the same read
// taskArtifactDTO's consumers make. `kind` is not a parameter on the upload
// routes because the bytes already answer it, and a caller-declared kind that
// disagreed with the content would be a third source of truth.
func artifactKindOfBlob(att *ChatAttachment) string {
	if len(att.Mime) >= 6 && att.Mime[:6] == "image/" {
		return ArtifactKindImage
	}
	return ArtifactKindFile
}
