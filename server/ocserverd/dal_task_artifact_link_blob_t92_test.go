package main

// dal_task_artifact_link_blob_t92_test.go — un-pinning a LINK must not strand
// the blob that was minted for it.
//
// DeleteTaskArtifact spares the live row's blob, because a blob a member
// uploaded may also be riding a chat message. T-92 gave links a blob too, and
// for those the premise is false: mintLinkTargetBlob makes a fresh
// text/uri-list blob inside the pin's own transaction, so at creation nothing
// else can name it. Un-pinning a link was therefore a guaranteed orphan, and
// the survivor scan only ever revisits blobs a delete put on its candidate
// list. Owner ruling rc-27107ca914a7 lifted the exemption for links.
//
// 🔴 WHAT THIS FILE HAS TO PROVE IS BOTH DIRECTIONS, because "delete it" alone
// is the dangerous half. The question the owner asked before ruling was the
// second case below: if some other agent pinned that same blob onto a SECOND
// task, it must survive. That is not this function's doing — source 4 of
// collectSurvivingBlobRefs scans EVERY task's artifacts — so the third case
// pins the file exemption as well, to keep a future edit from "simplifying"
// the two branches into one.

import (
	"testing"
)

// t92LinkBlobWorld seeds one task and returns a DAL on a fresh migrated DB.
func t92LinkBlobWorld(t *testing.T) *DAL {
	t.Helper()
	dal := newTestDAL(t)
	for _, id := range []string{"t-aaaa0001", "t-aaaa0002"} {
		if err := dal.PutTask(Task{
			ID: id, Title: "holder", Status: TaskStatusNotStarted,
			Priority: TaskPriorityMid, ExecutorKind: TaskExecutorMember,
			ExecutorID: "m-exec", CreatedTS: 1, UpdatedTS: 1,
		}); err != nil {
			t.Fatalf("seed task %s: %v", id, err)
		}
	}
	return dal
}

// t92PinLink pins a link artifact, minting its blob the way the API does.
func t92PinLink(t *testing.T, dal *DAL, taskID, artifactID, url string) string {
	t.Helper()
	attID, blob := mintLinkTargetBlob(url)
	if err := dal.PutTaskArtifactMintingBlob(TaskArtifact{
		ID: artifactID, TaskID: taskID, Kind: ArtifactKindLink,
		AttachmentID: attID, Name: "the link", CreatedTS: 1, CreatedBy: "m-exec",
	}, blob); err != nil {
		t.Fatalf("pin link %s: %v", artifactID, err)
	}
	return attID
}

func t92BlobExists(t *testing.T, dal *DAL, attID string) bool {
	t.Helper()
	got, err := dal.GetChatAttachment(attID)
	if err != nil {
		t.Fatalf("read blob %s: %v", attID, err)
	}
	return got != nil
}

// TestUnpinningALinkCollectsTheBlobMintedForIt is the ruling itself. Before
// rc-27107ca914a7 this blob outlived every reference to it, for good, with no
// error anywhere.
func TestUnpinningALinkCollectsTheBlobMintedForIt(t *testing.T) {
	dal := t92LinkBlobWorld(t)
	attID := t92PinLink(t, dal, "t-aaaa0001", "ta-link0001", "https://example.test/only")

	if !t92BlobExists(t, dal, attID) {
		t.Fatalf("precondition: the minted blob should exist before the delete")
	}
	removed, err := dal.DeleteTaskArtifact("ta-link0001")
	if err != nil || !removed {
		t.Fatalf("delete: removed=%v err=%v", removed, err)
	}
	if t92BlobExists(t, dal, attID) {
		t.Fatalf("the link's minted blob %s survived its only referrer — "+
			"nothing will ever collect it (the survivor scan revisits only "+
			"blobs a delete puts on its candidate list)", attID)
	}
}

// TestUnpinningALinkSparesABlobAnotherTaskAlsoPinned is the owner's own
// question, made executable: a second task pinning the SAME blob must veto the
// collection. The veto comes from collectSurvivingBlobRefs source 4 scanning
// every task's artifacts, not from the exemption this ticket removed — so this
// case would go red on a change that collected the blob unconditionally.
func TestUnpinningALinkSparesABlobAnotherTaskAlsoPinned(t *testing.T) {
	dal := t92LinkBlobWorld(t)
	attID := t92PinLink(t, dal, "t-aaaa0001", "ta-link0002", "https://example.test/shared")

	// The second pin REUSES the existing blob — the shape a member reaches by
	// passing an attachment_id it already holds.
	if err := dal.PutTaskArtifact(TaskArtifact{
		ID: "ta-link0003", TaskID: "t-aaaa0002", Kind: ArtifactKindLink,
		AttachmentID: attID, Name: "same target, other task",
		CreatedTS: 1, CreatedBy: "m-other",
	}); err != nil {
		t.Fatalf("second pin: %v", err)
	}

	if _, err := dal.DeleteTaskArtifact("ta-link0002"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !t92BlobExists(t, dal, attID) {
		t.Fatalf("blob %s was collected while a SECOND task's artifact still "+
			"pointed at it — that artifact now resolves to nothing", attID)
	}
}

// TestUnpinningAFileStillSparesItsBlob keeps the other branch honest. A file's
// blob arrived through the chat attachment store and may be referenced from
// places this delete has no business collecting on behalf of; that exemption
// predates T-92 and the ruling did not touch it.
func TestUnpinningAFileStillSparesItsBlob(t *testing.T) {
	dal := t92LinkBlobWorld(t)
	const attID = "att-file00000001"
	filename := "report.md"
	if err := dal.PutChatAttachment(ChatAttachment{
		ID: attID, Mime: "text/markdown", Filename: &filename,
		Data: []byte("# report"),
	}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	if err := dal.PutTaskArtifact(TaskArtifact{
		ID: "ta-file0001", TaskID: "t-aaaa0001", Kind: ArtifactKindFile,
		AttachmentID: attID, Name: "report", CreatedTS: 1, CreatedBy: "m-exec",
	}); err != nil {
		t.Fatalf("pin file: %v", err)
	}

	if _, err := dal.DeleteTaskArtifact("ta-file0001"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !t92BlobExists(t, dal, attID) {
		t.Fatalf("a FILE's blob was collected on un-pin; the link ruling " +
			"(rc-27107ca914a7) was scoped to links and this exemption stands")
	}
}
