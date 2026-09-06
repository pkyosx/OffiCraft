package main

// api_tasks_artifact_replace_t60_test.go — T-60's third verb on the task
// artifact set: replacing ONE pinned deliverable's content while its id stays
// put, the versions that replacement retains, and the two places a retained
// version's blob has to be collected (the trim, and the un-pin that deletes the
// whole series).
//
// The blob assertions here are deliberately about the BLOB STORE, not about the
// length of the version list: a history that is three long proves only that the
// list was trimmed, and trimming a row while leaving its file behind is the
// pre-existing un-pin leak moved to a new address.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// replaceArtifact posts replace_task_artifact as (sub, scope).
func replaceArtifact(
	t *testing.T, api *apiServer, taskID, artID string, body map[string]any, sub, scope string,
) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReplaceTaskArtifactApiTasksTaskIdArtifactArtifactIdReplacePost(rec,
		taskReq(t, "POST", "/api/tasks/"+taskID+"/artifact/"+artID+"/replace",
			body, sub, scope),
		taskID, artID)
	return rec
}

// artifactHistory reads the version list as (sub, scope).
func artifactHistory(
	t *testing.T, api *apiServer, taskID, artID, sub, scope string,
) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleListTaskArtifactHistoryApiTasksTaskIdArtifactArtifactIdHistoryGet(rec,
		taskReq(t, "GET", "/api/tasks/"+taskID+"/artifact/"+artID+"/history",
			nil, sub, scope),
		taskID, artID)
	return rec
}

// seedArtifactBlob stores one blob whose bytes are its own id, so a wrong-row
// delete is visible rather than merely a missing row.
func seedArtifactBlob(t *testing.T, api *apiServer, id string) {
	t.Helper()
	name := id + ".pdf"
	if err := api.dal.PutChatAttachment(ChatAttachment{
		ID: id, Mime: "application/pdf", Data: []byte(id), Filename: &name,
	}); err != nil {
		t.Fatalf("seed blob %s: %v", id, err)
	}
}

// fileArtifactOn pins one file deliverable carrying blob attID and answers its id.
func fileArtifactOn(t *testing.T, api *apiServer, taskID, attID string) string {
	t.Helper()
	rec := addArtifact(t, api, taskID,
		map[string]any{"kind": "file", "attachment_id": attID}, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("add file artifact: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[taskArtifactReceiptDTO](t, rec).ArtifactID
}

func TestReplaceArtifactKeepsTheIdAndRetainsTheReplacedVersion(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1", "label": "PR #1"},
		"m-exec", "agent")
	artID := decodeBody[taskArtifactReceiptDTO](t, rec).ArtifactID

	// Before any replace the deliverable has exactly one version — itself.
	if got := getTaskArtifacts(t, api, task.ID).Artifacts[0].VersionCount; got != 1 {
		t.Fatalf("a never-replaced artifact has 1 version, got %d", got)
	}

	rec = replaceArtifact(t, api, task.ID, artID,
		map[string]any{"url": "https://x/pr/2", "label": "PR #2"}, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body.String())
	}
	receipt := decodeBody[taskArtifactReplaceReceiptDTO](t, rec)
	if receipt != (taskArtifactReplaceReceiptDTO{
		TaskID: task.ID, ArtifactID: artID, ArtifactCount: 1, VersionCount: 2,
	}) {
		t.Fatalf("replace receipt wrong shape: %+v", receipt)
	}

	// THE WHOLE POINT: one artifact, the same id, new content.
	view := getTaskArtifacts(t, api, task.ID)
	if len(view.Artifacts) != 1 {
		t.Fatalf("replace must not add a row, got %+v", view.Artifacts)
	}
	live := view.Artifacts[0]
	if live.ID != artID {
		t.Fatalf("the artifact id must never move: %q became %q", artID, live.ID)
	}
	if live.URL != "https://x/pr/2" || live.Label != "PR #2" ||
		live.Kind != "link" || live.VersionCount != 2 {
		t.Fatalf("live artifact wrong shape after replace: %+v", live)
	}

	// The version it replaced is retained whole.
	hrec := artifactHistory(t, api, task.ID, artID, "m-exec", "agent")
	if hrec.Code != http.StatusOK {
		t.Fatalf("history: %d %s", hrec.Code, hrec.Body.String())
	}
	versions := decodeBody[[]taskArtifactVersionDTO](t, hrec)
	if len(versions) != 1 {
		t.Fatalf("expected exactly the replaced version, got %+v", versions)
	}
	if versions[0].URL != "https://x/pr/1" || versions[0].Label != "PR #1" ||
		versions[0].Kind != "link" || versions[0].CreatedBy != "m-exec" {
		t.Fatalf("retained version wrong shape: %+v", versions[0])
	}
}

// TestReplaceArtifactOnTerminalTaskIs409 is the THIRD copy of the freeze (owner
// ruling 2026-07-25): add and remove each refuse a closed task, and a replace
// verb without the same 409 is the freeze's back door — the content behind a
// frozen deliverable could be swapped for anything while the count on the card
// never moved. The open-task replace is the positive control, so a mutant that
// freezes replace unconditionally reddens too.
func TestReplaceArtifactOnTerminalTaskIs409(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	artID := decodeBody[taskArtifactReceiptDTO](t, addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1"},
		"m-exec", "agent")).ArtifactID

	if rec := replaceArtifact(t, api, task.ID, artID,
		map[string]any{"url": "https://x/pr/2"}, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("open task replace must be 200, got %d %s", rec.Code, rec.Body.String())
	}

	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/x", nil, "owner", "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
	}

	if rec := replaceArtifact(t, api, task.ID, artID,
		map[string]any{"url": "https://x/pr/3"}, "m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("terminal task replace must 409, got %d %s", rec.Code, rec.Body.String())
	}
	// The owner (admin capability) is not exempt either — the freeze is a task
	// state rule, not a permission rule.
	if rec := replaceArtifact(t, api, task.ID, artID,
		map[string]any{"url": "https://x/pr/3"}, "owner", "owner"); rec.Code != http.StatusConflict {
		t.Fatalf("terminal task replace by owner must 409, got %d %s", rec.Code, rec.Body.String())
	}
	// The refusals left the deliverable exactly as the close froze it.
	live := getTaskArtifacts(t, api, task.ID).Artifacts[0]
	if live.URL != "https://x/pr/2" || live.VersionCount != 2 {
		t.Fatalf("a refused replace must change nothing, got %+v", live)
	}
}

// TestReplaceArtifactRefusesACrossKindReplacement pins the other scope line: an
// artifact id that resolved to an image must never resolve to a link later. All
// three ways to ask for it are refused, and none of them may half-apply.
func TestReplaceArtifactRefusesACrossKindReplacement(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	seedArtifactBlob(t, api, "att-v1")
	seedArtifactBlob(t, api, "att-v2")
	fileID := fileArtifactOn(t, api, task.ID, "att-v1")
	linkID := decodeBody[taskArtifactReceiptDTO](t, addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1"},
		"m-exec", "agent")).ArtifactID

	for _, tc := range []struct {
		name  string
		artID string
		body  map[string]any
	}{
		{"an explicit kind that is not the pinned one", fileID,
			map[string]any{"kind": "link", "url": "https://x/pr/9"}},
		{"a url where the artifact is a file", fileID,
			map[string]any{"url": "https://x/pr/9"}},
		{"an attachment_id where the artifact is a link", linkID,
			map[string]any{"attachment_id": "att-v2"}},
	} {
		rec := replaceArtifact(t, api, task.ID, tc.artID, tc.body, "m-exec", "agent")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s must 400, got %d %s", tc.name, rec.Code, rec.Body.String())
		}
	}

	// Nothing moved, and nothing was versioned by a refused write.
	for _, a := range getTaskArtifacts(t, api, task.ID).Artifacts {
		if a.VersionCount != 1 {
			t.Fatalf("a refused replace must retain no version: %+v", a)
		}
		if a.ID == fileID && (a.Kind != "file" || a.AttachmentID != "att-v1") {
			t.Fatalf("file artifact moved: %+v", a)
		}
		if a.ID == linkID && (a.Kind != "link" || a.URL != "https://x/pr/1") {
			t.Fatalf("link artifact moved: %+v", a)
		}
	}
}

// TestReplaceArtifactTrimCollectsTheDiscardedVersionsBlob is the acceptance the
// version list cannot give: once a version falls off the end of the retained
// depth, NOTHING can ever name its blob again — no artifact row, no version row,
// no reader — so the blob must go with it. Asserting only that the list is three
// long would pass on a trim that leaves the file behind forever, which is the
// existing un-pin leak wearing a new hat.
func TestReplaceArtifactTrimCollectsTheDiscardedVersionsBlob(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	blobs := []string{"att-v0", "att-v1", "att-v2", "att-v3", "att-v4"}
	for _, id := range blobs {
		seedArtifactBlob(t, api, id)
	}
	artID := fileArtifactOn(t, api, task.ID, blobs[0])

	// Three replacements fill the retained depth exactly; the oldest version is
	// still the original, so every blob is still referenced by something.
	for _, id := range blobs[1:4] {
		if rec := replaceArtifact(t, api, task.ID, artID,
			map[string]any{"attachment_id": id}, "m-exec", "agent"); rec.Code != http.StatusOK {
			t.Fatalf("replace with %s: %d %s", id, rec.Code, rec.Body.String())
		}
	}
	for _, id := range blobs[:4] {
		mustBlobAlive(t, api.dal, id, "still the live row or one of the retained versions")
	}

	// The FOURTH replacement pushes the original off the end.
	rec := replaceArtifact(t, api, task.ID, artID,
		map[string]any{"attachment_id": blobs[4]}, "m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("fourth replace: %d %s", rec.Code, rec.Body.String())
	}
	mustBlobGone(t, api.dal, blobs[0], "its version was trimmed off the retained depth")
	for _, id := range blobs[1:] {
		mustBlobAlive(t, api.dal, id, "still the live row or one of the retained versions")
	}

	// The id never moved and the depth stopped climbing.
	receipt := decodeBody[taskArtifactReplaceReceiptDTO](t, rec)
	if receipt.ArtifactID != artID || receipt.VersionCount != documentHistoryKeepDefault+1 {
		t.Fatalf("replace receipt wrong shape at the retained depth: %+v", receipt)
	}
	versions := decodeBody[[]taskArtifactVersionDTO](t,
		artifactHistory(t, api, task.ID, artID, "m-exec", "agent"))
	if len(versions) != documentHistoryKeepDefault {
		t.Fatalf("expected %d retained versions, got %d", documentHistoryKeepDefault, len(versions))
	}
	// Newest first, and the trimmed one is not among them.
	if versions[0].AttachmentID != blobs[3] || versions[2].AttachmentID != blobs[1] {
		t.Fatalf("retained versions must be newest-first %v, got %+v", blobs[1:4], versions)
	}
}

// TestRemoveArtifactCollectsEveryRetainedVersion — un-pinning a deliverable that
// has been replaced must not leave its previous versions behind: nothing can
// address them once the artifact id is gone, so an owner-less version row and a
// permanently unreachable blob are the same defect. The LIVE blob keeps its
// standing exemption (it may be shared with a chat message), which is the
// control that stops this from being "delete everything in sight".
func TestRemoveArtifactCollectsEveryRetainedVersion(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	for _, id := range []string{"att-v0", "att-v1", "att-live", "att-other"} {
		seedArtifactBlob(t, api, id)
	}
	artID := fileArtifactOn(t, api, task.ID, "att-v0")
	// A second, untouched deliverable — a mutant that collects the whole blob
	// store instead of this artifact's versions reddens on it.
	otherID := fileArtifactOn(t, api, task.ID, "att-other")

	for _, id := range []string{"att-v1", "att-live"} {
		if rec := replaceArtifact(t, api, task.ID, artID,
			map[string]any{"attachment_id": id}, "m-exec", "agent"); rec.Code != http.StatusOK {
			t.Fatalf("replace with %s: %d %s", id, rec.Code, rec.Body.String())
		}
	}

	if rec := removeArtifact(t, api, task.ID, artID, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}

	// No version may outlive the artifact it belonged to…
	versions, err := api.dal.ListTaskArtifactHistory(artID)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("an un-pinned artifact leaves no versions behind, got %+v", versions)
	}
	// …and their blobs go with them.
	mustBlobGone(t, api.dal, "att-v0", "its artifact was un-pinned")
	mustBlobGone(t, api.dal, "att-v1", "its artifact was un-pinned")
	// The live row's blob keeps the exemption that predates T-60, and the
	// untouched deliverable is not collateral.
	mustBlobAlive(t, api.dal, "att-live", "the live blob's exemption predates T-60")
	mustBlobAlive(t, api.dal, "att-other", "pinned by an artifact nobody removed")
	if got := getTaskView(t, api, task.ID).Artifacts; len(got) != 1 || got[0].ID != otherID {
		t.Fatalf("only the removed artifact may disappear, got %+v", got)
	}
}

// TestArtifactHistoryGuards — the read shares NEITHER of the writes' gates: no
// executor guard (anyone who can read the task can read what its deliverables
// used to be) and no terminal-task freeze (a closed task's history is exactly
// when a reader wants it). Only the WRITE verbs keep both.
func TestArtifactHistoryGuards(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	other := createAdHocTask(t, api, "m-exec")
	artID := decodeBody[taskArtifactReceiptDTO](t, addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1"},
		"m-exec", "agent")).ArtifactID
	if rec := replaceArtifact(t, api, task.ID, artID,
		map[string]any{"url": "https://x/pr/2"}, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body.String())
	}

	// The READ carries no executor guard (owner ruling, T-60): the plain task
	// read already serves this artifact set to any caller, so the history door
	// must not answer differently about the same rows.
	if rec := artifactHistory(t, api, task.ID, artID, "m-stranger", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("a non-executor reads the version list, got %d %s", rec.Code, rec.Body.String())
	}
	// The WRITE verbs keep it.
	if rec := replaceArtifact(t, api, task.ID, artID,
		map[string]any{"url": "https://x/pr/3"}, "m-stranger", "agent"); rec.Code != http.StatusForbidden {
		t.Fatalf("a non-executor must not replace, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := removeArtifact(t, api, task.ID, artID, "m-stranger", "agent"); rec.Code != http.StatusForbidden {
		t.Fatalf("a non-executor must not un-pin, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/4"},
		"m-stranger", "agent"); rec.Code != http.StatusForbidden {
		t.Fatalf("a non-executor must not pin, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := artifactHistory(t, api, task.ID, "ta-nope", "m-exec", "agent"); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown artifact must be 404, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := artifactHistory(t, api, other.ID, artID, "m-exec", "agent"); rec.Code != http.StatusBadRequest {
		t.Fatalf("another task's artifact must be 400, got %d %s", rec.Code, rec.Body.String())
	}

	// Closing the task freezes the writes and leaves the read open.
	rec := httptest.NewRecorder()
	api.HandleTerminateTaskApiTasksTaskIdTerminatePost(rec,
		taskReq(t, "POST", "/x", nil, "owner", "owner"), task.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate: %d %s", rec.Code, rec.Body.String())
	}
	hrec := artifactHistory(t, api, task.ID, artID, "m-exec", "agent")
	if hrec.Code != http.StatusOK {
		t.Fatalf("a closed task's history stays readable, got %d %s", hrec.Code, hrec.Body.String())
	}
	if len(decodeBody[[]taskArtifactVersionDTO](t, hrec)) != 1 {
		t.Fatalf("the retained version must still be listed: %s", hrec.Body.String())
	}
	if rec := replaceArtifact(t, api, task.ID, artID,
		map[string]any{"url": "https://x/pr/5"}, "m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("a closed task still refuses replace with 409, got %d %s", rec.Code, rec.Body.String())
	}
	if rec := removeArtifact(t, api, task.ID, artID, "m-exec", "agent"); rec.Code != http.StatusConflict {
		t.Fatalf("a closed task still refuses un-pin with 409, got %d %s", rec.Code, rec.Body.String())
	}
	// A stranger reads a CLOSED task's history too — the two exemptions are
	// independent, and a reader that arrives after the card is filed is the
	// commonest one.
	if rec := artifactHistory(t, api, task.ID, artID, "m-stranger", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("a non-executor reads a closed task's version list, got %d %s",
			rec.Code, rec.Body.String())
	}
}

// TestArtifactHistoryCarriesEachVersionsOwnFilename — the version list is what
// the cockpit's diff reads, and whether it can diff two versions at all turns on
// whether it can tell that their bytes are TEXT. The mime cannot answer for the
// deliverable this journal mostly holds: an agent-uploaded .md report arrives
// under application/octet-stream, so the NAME is the only remaining witness.
//
// A version's label is optional and usually absent (nothing makes an agent pass
// one), so a wire that carries only the label leaves exactly that class of
// version nameless — and permanently un-diffable. The filename is resolved from
// the version's OWN retained blob, the same read the live projection does, so
// neither side of the comparison is named more generously than the other.
func TestArtifactHistoryCarriesEachVersionsOwnFilename(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	for id, name := range map[string]string{"att-old": "report.md", "att-new": "report.md"} {
		blobName := name
		if err := api.dal.PutChatAttachment(ChatAttachment{
			ID: id, Mime: "application/octet-stream", Data: []byte(id), Filename: &blobName,
		}); err != nil {
			t.Fatalf("seed blob %s: %v", id, err)
		}
	}
	// No label anywhere — the case the label-only wire could not name.
	artID := fileArtifactOn(t, api, task.ID, "att-old")
	if rec := replaceArtifact(t, api, task.ID, artID,
		map[string]any{"attachment_id": "att-new"}, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body.String())
	}

	versions := decodeBody[[]taskArtifactVersionDTO](t,
		artifactHistory(t, api, task.ID, artID, "m-exec", "agent"))
	if len(versions) != 1 {
		t.Fatalf("expected exactly the replaced version, got %+v", versions)
	}
	if versions[0].Label != "" {
		t.Fatalf("this case is about a version with NO label, got %+v", versions[0])
	}
	if versions[0].Filename != "report.md" {
		t.Fatalf("a retained version must carry its own blob's filename, got %+v", versions[0])
	}

	// A link version has no blob, so it has no filename to borrow either.
	linkID := decodeBody[taskArtifactReceiptDTO](t, addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1"},
		"m-exec", "agent")).ArtifactID
	if rec := replaceArtifact(t, api, task.ID, linkID,
		map[string]any{"url": "https://x/pr/2"}, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("replace link: %d %s", rec.Code, rec.Body.String())
	}
	linkVersions := decodeBody[[]taskArtifactVersionDTO](t,
		artifactHistory(t, api, task.ID, linkID, "m-exec", "agent"))
	if len(linkVersions) != 1 || linkVersions[0].Filename != "" {
		t.Fatalf("a link version has no blob and so no filename, got %+v", linkVersions)
	}
}

// TestArtifactHistoryServesAFileVersionsBlobEndpoint — the version list is read
// by a cockpit that has to FETCH what it lists, and for a file/image the only
// address that resolves is the blob serve path. `task_artifact.url` is not it:
// that column holds the external link for a link kind and the EMPTY STRING for
// a file/image, so a version projection that copies the row hands the reader
// nothing, and a client that treats a url-less version as gone is right to.
//
// The mime rides along for the same reason the live artifact's does — it is the
// first word on what the bytes ARE, and is_image is the read every surface that
// shows a deliverable makes. Asserted through the real handler rather than the
// projection function, because copying the row's url was exactly the kind of
// mistake a DTO-level fixture (which carries a url of its own) cannot see.
func TestArtifactHistoryServesAFileVersionsBlobEndpoint(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	seed := func(id, mime, name string) {
		t.Helper()
		blobName := name
		if err := api.dal.PutChatAttachment(ChatAttachment{
			ID: id, Mime: mime, Data: []byte(id), Filename: &blobName,
		}); err != nil {
			t.Fatalf("seed blob %s: %v", id, err)
		}
	}
	seed("att-report-old", "application/octet-stream", "report.md")
	seed("att-report-new", "application/octet-stream", "report.md")

	artID := fileArtifactOn(t, api, task.ID, "att-report-old")
	if rec := replaceArtifact(t, api, task.ID, artID,
		map[string]any{"attachment_id": "att-report-new"},
		"m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body.String())
	}

	versions := decodeBody[[]taskArtifactVersionDTO](t,
		artifactHistory(t, api, task.ID, artID, "m-exec", "agent"))
	if len(versions) != 1 {
		t.Fatalf("expected exactly the replaced version, got %+v", versions)
	}
	if versions[0].URL != "/api/chat/attachment/att-report-old" {
		t.Fatalf("a file version's url must be its OWN retained blob's serve path, got %+v",
			versions[0])
	}
	if versions[0].Mime != "application/octet-stream" {
		t.Fatalf("a file version must carry its own blob's mime, got %+v", versions[0])
	}
	if versions[0].IsImage {
		t.Fatalf("an octet-stream version is not an image, got %+v", versions[0])
	}

	// An image deliverable answers the same three questions the other way — so a
	// projection that hard-coded either answer cannot pass both halves.
	seed("att-shot-old", "image/png", "shot.png")
	seed("att-shot-new", "image/png", "shot.png")
	imgID := decodeBody[taskArtifactReceiptDTO](t, addArtifact(t, api, task.ID,
		map[string]any{"kind": "image", "attachment_id": "att-shot-old"},
		"m-exec", "agent")).ArtifactID
	if rec := replaceArtifact(t, api, task.ID, imgID,
		map[string]any{"attachment_id": "att-shot-new"},
		"m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("replace image: %d %s", rec.Code, rec.Body.String())
	}
	imgVersions := decodeBody[[]taskArtifactVersionDTO](t,
		artifactHistory(t, api, task.ID, imgID, "m-exec", "agent"))
	if len(imgVersions) != 1 {
		t.Fatalf("expected exactly the replaced image version, got %+v", imgVersions)
	}
	if imgVersions[0].URL != "/api/chat/attachment/att-shot-old" ||
		imgVersions[0].Mime != "image/png" || !imgVersions[0].IsImage {
		t.Fatalf("an image version must serve its blob path, mime and is_image, got %+v",
			imgVersions[0])
	}

	// A link version keeps the row's own external url, and has no blob to
	// describe — the control that stops the rewrite from applying to every kind.
	linkID := decodeBody[taskArtifactReceiptDTO](t, addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1"},
		"m-exec", "agent")).ArtifactID
	if rec := replaceArtifact(t, api, task.ID, linkID,
		map[string]any{"url": "https://x/pr/2"}, "m-exec", "agent"); rec.Code != http.StatusOK {
		t.Fatalf("replace link: %d %s", rec.Code, rec.Body.String())
	}
	linkVersions := decodeBody[[]taskArtifactVersionDTO](t,
		artifactHistory(t, api, task.ID, linkID, "m-exec", "agent"))
	if len(linkVersions) != 1 || linkVersions[0].URL != "https://x/pr/1" ||
		linkVersions[0].Mime != "" || linkVersions[0].IsImage {
		t.Fatalf("a link version keeps its external url and describes no blob, got %+v",
			linkVersions)
	}
}

// An OMITTED label carries the pinned one forward (owner ruling 2026-09-05):
// updating a deliverable's content should not cost it its display name, which
// is what happened while an absent label was stored as the empty string. The
// three arms are the whole contract — absent keeps, explicit replaces, explicit
// blank clears — so a mutant that collapses any two of them reddens.
func TestReplaceArtifactLabelAbsentKeepsExplicitReplacesBlankClears(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")
	rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/1", "label": "PR #1"},
		"m-exec", "agent")
	artID := decodeBody[taskArtifactReceiptDTO](t, rec).ArtifactID

	live := func() taskArtifactDTO {
		t.Helper()
		return getTaskArtifacts(t, api, task.ID).Artifacts[0]
	}
	replace := func(body map[string]any) {
		t.Helper()
		r := replaceArtifact(t, api, task.ID, artID, body, "m-exec", "agent")
		if r.Code != http.StatusOK {
			t.Fatalf("replace %+v: %d %s", body, r.Code, r.Body.String())
		}
	}

	replace(map[string]any{"url": "https://x/pr/2"})
	if got := live().Label; got != "PR #1" {
		t.Fatalf("an omitted label must keep the pinned one, got %q", got)
	}

	replace(map[string]any{"url": "https://x/pr/3", "label": "PR #3"})
	if got := live().Label; got != "PR #3" {
		t.Fatalf("an explicit label must replace, got %q", got)
	}

	replace(map[string]any{"url": "https://x/pr/4", "label": ""})
	if got := live().Label; got != "" {
		t.Fatalf("an explicit blank label must clear, got %q", got)
	}

	// Once cleared, an omitted label keeps it cleared — inheritance is of what
	// is pinned, not of the last non-empty label anyone ever set.
	replace(map[string]any{"url": "https://x/pr/5"})
	if got := live().Label; got != "" {
		t.Fatalf("an omitted label must keep the pinned empty one, got %q", got)
	}
}
