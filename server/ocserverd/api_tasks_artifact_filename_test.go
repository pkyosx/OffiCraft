package main

// api_tasks_artifact_filename_test.go — the LIVE artifact row carries the
// BLOB'S OWN name beside the display name, and the two are not the same
// question.
//
// WHY THIS FILE EXISTS AT ALL. T-92 dropped `filename` from taskArtifactDTO on
// the reasoning that Name derives from it and therefore replaces it. That
// reasoning holds for what a reader SEES and fails for what a reader DECIDES:
// the cockpit's preview asks the NAME for an extension whenever the mime cannot
// say what the bytes are, and the agent upload path stores most of the .md
// reports pinned here as `application/octet-stream`. With `name` now a
// human-written sentence, .md deliverables stopped previewing and rendered as
// bare download rows.
//
// 🔴 NOTHING WENT RED WHEN IT BROKE, and that is the reason these cases assert
// on a row where the two values DIFFER. Every existing artifact fixture — and
// every migrated production row — has a stored name that either IS the blob's
// filename or is absent so the server derives it from that filename, so the
// distinction is invisible in all of them. A fixture whose name is a sentence
// and whose blob is `audit-2026-q3.md` is the only shape that can tell the two
// fields apart.
//
// The honest-empty half matters just as much: a link has no blob name to report
// (its blob is a text/uri-list nobody opens by name) and a file whose blob is
// gone has none either, and neither may be back-filled from the display name —
// that would hand the preview an extension the bytes never had.

import (
	"net/http"
	"testing"
)

// artifactWithHumanName pins one file/image deliverable whose DISPLAY name is a
// sentence and whose blob is called something else entirely, and answers the
// artifact id. The mime is the one the agent upload path actually produces for
// a .md, so the fixture is the production case rather than a convenient one.
func artifactWithHumanName(
	t *testing.T, api *apiServer, taskID, attID, kind, mime, blobName, displayName string,
) string {
	t.Helper()
	if err := api.dal.PutChatAttachment(ChatAttachment{
		ID: attID, Mime: mime, Data: []byte("# 稽核\n"), Filename: &blobName,
	}); err != nil {
		t.Fatalf("seed blob %s: %v", attID, err)
	}
	rec := addArtifact(t, api, taskID,
		map[string]any{"kind": kind, "attachment_id": attID, "name": displayName},
		"m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("add %s artifact: %d %s", kind, rec.Code, rec.Body.String())
	}
	return decodeBody[taskArtifactReceiptDTO](t, rec).ArtifactID
}

// artifactByID picks one row out of the full read, failing loudly when it is
// absent — a missing row must not read as an empty filename.
func artifactByID(t *testing.T, arts []taskArtifactDTO, id string) taskArtifactDTO {
	t.Helper()
	for _, a := range arts {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("%s 沒有讀回來:%+v", id, arts)
	return taskArtifactDTO{}
}

// TestArtifactFilenameIsTheBlobsOwnNameNotTheDisplayName is the regression the
// preview needed: a file and an image pinned under a human title still report
// the blob's own name, WITH its extension, and go on displaying the title.
func TestArtifactFilenameIsTheBlobsOwnNameNotTheDisplayName(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	const fileDisplay = "第三季稽核報告"
	const fileBlobName = "audit-2026-q3.md"
	fileID := artifactWithHumanName(t, api, task.ID, "att-humanfile",
		"file", "application/octet-stream", fileBlobName, fileDisplay)

	const imgDisplay = "登入頁的錯誤畫面"
	const imgBlobName = "login-error.png"
	imgID := artifactWithHumanName(t, api, task.ID, "att-humanimg",
		"image", "image/png", imgBlobName, imgDisplay)

	arts := getTaskArtifacts(t, api, task.ID).Artifacts
	// 反恆真:兩列各自報自己的 blob 檔名,所以「拿錯 blob」的 mutant 也紅。
	for _, want := range []struct{ id, display, blobName string }{
		{fileID, fileDisplay, fileBlobName},
		{imgID, imgDisplay, imgBlobName},
	} {
		got := artifactByID(t, arts, want.id)
		if got.Filename != want.blobName {
			t.Fatalf("%s 的 filename 應是 blob 自己的檔名 %q,得到 %q",
				want.id, want.blobName, got.Filename)
		}
		// The display name must survive untouched — reading the blob name into
		// `name` would take the T-92 feature back out, and a test that only
		// looked at `filename` would not notice.
		if got.Name != want.display {
			t.Fatalf("%s 的 name 應仍是人寫的顯示名 %q,得到 %q",
				want.id, want.display, got.Name)
		}
	}
}

// TestArtifactFilenameIsEmptyForALinkAndForAMissingBlob pins the honest-empty
// half. Both rows have a perfectly good display name, so a projection that
// back-filled `filename` from `name` would pass every case above and fail here
// — which is precisely the mistake worth catching.
func TestArtifactFilenameIsEmptyForALinkAndForAMissingBlob(t *testing.T) {
	api := newTasksTestServer(t)
	task := createAdHocTask(t, api, "m-exec")

	rec := addArtifact(t, api, task.ID,
		map[string]any{"kind": "link", "url": "https://x/pr/92", "name": "PR #92"},
		"m-exec", "agent")
	if rec.Code != http.StatusOK {
		t.Fatalf("add link: %d %s", rec.Code, rec.Body.String())
	}
	linkID := decodeBody[taskArtifactReceiptDTO](t, rec).ArtifactID

	// A file row whose blob is GONE — seeded straight through the DAL, because
	// the write door will not mint a row pointing at a blob that is not there.
	if err := api.dal.PutTaskArtifact(TaskArtifact{
		ID: "ta-deadblob", TaskID: task.ID, Kind: ArtifactKindFile,
		AttachmentID: "att-collected", Name: "第三季稽核報告",
		CreatedTS: 1000, CreatedBy: "m-exec",
	}); err != nil {
		t.Fatalf("seed dead-blob artifact: %v", err)
	}

	arts := getTaskArtifacts(t, api, task.ID).Artifacts
	if got := artifactByID(t, arts, linkID); got.Filename != "" {
		t.Fatalf("link 沒有可報的 blob 檔名,filename 應是空的,得到 %q", got.Filename)
	}
	dead := artifactByID(t, arts, "ta-deadblob")
	if dead.Filename != "" {
		t.Fatalf("blob 不在了就沒有檔名可報,filename 應是空的,得到 %q", dead.Filename)
	}
	// 反恆真:這列的 name 還在,所以上面的空不是「整列都空了」。
	if dead.Name == "" {
		t.Fatalf("語料不合格:這列的 name 應該還在,得到 %+v", dead)
	}
}
