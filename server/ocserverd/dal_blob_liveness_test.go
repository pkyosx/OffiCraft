package main

import "testing"

// dal_blob_liveness_test.go — the two-directional guard on the ONLY production
// path that deletes chat_attachment blobs (DAL.DeleteChatInvolving).
//
// A delete path needs BOTH halves pinned or a fix for one becomes a defect in
// the other:
//
//	(1) a blob a SURVIVING record still references must NOT be deleted
//	    (T-62a8: task_artifact.attachment_id was not consulted, so a
//	    deliverable pinned on a task card died with the chat message it was
//	    uploaded in — unrecoverable once the task freezes its artifact set);
//	(2) a blob NOTHING surviving references must STILL be deleted (otherwise
//	    the fix for (1) trades data loss for a silent disk leak).
//
// Both assertions round-trip through GetChatAttachment — the returned counter
// is a claim, the re-read is the fact.

// seedLivenessBlobs stores one blob per id (bytes = the id, so a wrong-row
// delete is visible).
func seedLivenessBlobs(t *testing.T, d *DAL, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := d.PutChatAttachment(ChatAttachment{ID: id, Data: []byte(id)}); err != nil {
			t.Fatalf("put attachment %s: %v", id, err)
		}
	}
}

// mustBlobAlive re-reads the blob store and fails if the blob is gone.
func mustBlobAlive(t *testing.T, d *DAL, id, why string) {
	t.Helper()
	a, err := d.GetChatAttachment(id)
	if err != nil {
		t.Fatalf("re-read %s: %v", id, err)
	}
	if a == nil {
		t.Fatalf("%s: blob %s must survive but was deleted", why, id)
	}
	if string(a.Data) != id {
		t.Fatalf("%s: blob %s round-tripped wrong bytes %q", why, id, a.Data)
	}
}

// mustBlobGone re-reads the blob store and fails if the blob is still there.
func mustBlobGone(t *testing.T, d *DAL, id, why string) {
	t.Helper()
	a, err := d.GetChatAttachment(id)
	if err != nil {
		t.Fatalf("re-read %s: %v", id, err)
	}
	if a != nil {
		t.Fatalf("%s: blob %s must be collected but is still stored", why, id)
	}
}

// TestDeleteChatInvolvingSparesTaskArtifactPinnedBlobs is sentinel (1).
//
// A file uploaded in a chat message and then PINNED onto a task card as a
// deliverable: removing the member deletes the message, but the task_artifact
// row survives (artifact rows have no cascade, and a terminal task's set is
// frozen in both directions) — so the blob is still referenced and must live.
//
// On 2e74953 the survivor scan read chat_message.meta and both reply_card
// columns only, so this blob was cascaded and the task card was left holding a
// dead link. This assertion is NOT vacuous there: it fails.
func TestDeleteChatInvolvingSparesTaskArtifactPinnedBlobs(t *testing.T) {
	d := newTestDAL(t)
	seedLivenessBlobs(t, d, "a-pinned", "a-unpinned")

	// Both blobs ride messages that this deletion removes.
	for _, m := range []ChatMessage{
		{ID: "c-pinned", Sender: "m-1", Recipient: "owner", TS: 1.0,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "a-pinned"},
			}}},
		{ID: "c-unpinned", Sender: "m-1", Recipient: "owner", TS: 2.0,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "a-unpinned"},
			}}},
	} {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put chat: %v", err)
		}
	}
	// …but only a-pinned is also a task deliverable.
	if err := d.PutTaskArtifact(TaskArtifact{
		ID: "ta-1", TaskID: "t-1", Kind: ArtifactKindFile,
		AttachmentID: "a-pinned", Label: "deliverable.pdf", CreatedTS: 3.0,
		CreatedBy: "m-1",
	}); err != nil {
		t.Fatalf("put task artifact: %v", err)
	}

	msgs, atts, err := d.DeleteChatInvolving("m-1")
	if err != nil {
		t.Fatalf("delete involving: %v", err)
	}
	if msgs != 2 {
		t.Fatalf("both messages must be deleted, got %d", msgs)
	}
	// Round-trip first: the store is the fact, the counter is a claim.
	mustBlobAlive(t, d, "a-pinned", "pinned as a task_artifact deliverable")
	mustBlobGone(t, d, "a-unpinned", "referenced by nothing that survived")
	// Only the un-pinned blob may be collected.
	if atts != 1 {
		t.Fatalf("exactly the un-pinned blob may cascade, got %d", atts)
	}

	// The artifact row itself is untouched, so the card still points at a
	// blob that resolves — that is the whole point of sentinel (1).
	arts, err := d.ListTaskArtifacts("t-1")
	if err != nil || len(arts) != 1 || arts[0].AttachmentID != "a-pinned" {
		t.Fatalf("artifact row must survive intact, got %+v (%v)", arts, err)
	}
}

// TestDeleteChatInvolvingSparesMemberAvatarBlobs pins the fifth liveness
// source: a member row that still points at a blob vetoes collection even if
// a deleted chat message also referenced that id. The public write paths
// reject ava- ids in general attachment graphs, but this sentinel deliberately
// seeds the cross-reference directly so removing that distant prefix guard
// cannot turn into silent avatar data loss.
func TestDeleteChatInvolvingSparesMemberAvatarBlobs(t *testing.T) {
	d := newTestDAL(t)
	seedLivenessBlobs(t, d, "ava-member", "a-unreferenced")

	avatarOwner := testAgent("m-avatar-owner")
	avatarOwner.AvatarAttachmentID = "ava-member"
	if err := d.PutMember(avatarOwner); err != nil {
		t.Fatalf("put avatar owner: %v", err)
	}
	for _, m := range []ChatMessage{
		{ID: "c-avatar", Sender: "m-deleted", Recipient: "owner", TS: 1.0,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "ava-member"},
			}}},
		{ID: "c-garbage", Sender: "m-deleted", Recipient: "owner", TS: 2.0,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "a-unreferenced"},
			}}},
	} {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put chat: %v", err)
		}
	}

	msgs, atts, err := d.DeleteChatInvolving("m-deleted")
	if err != nil {
		t.Fatalf("delete involving: %v", err)
	}
	if msgs != 2 || atts != 1 {
		t.Fatalf("delete counts = messages %d attachments %d, want 2/1", msgs, atts)
	}
	mustBlobAlive(t, d, "ava-member", "referenced by surviving member avatar")
	mustBlobGone(t, d, "a-unreferenced", "referenced by nothing that survived")
}

// TestHardDeleteMemberSparesAvatarBlobReferencedElsewhere pins the reverse
// asymmetry. Hard-deleting the owning member removes its own pointer, but must
// not delete the blob if any other stored record still references it. Together
// with TestHardDeleteMemberRemovesOwnedAvatarBlob this is the two-directional
// fence: referenced survives; truly unreferenced is collected.
func TestHardDeleteMemberSparesAvatarBlobReferencedElsewhere(t *testing.T) {
	d := newTestDAL(t)
	owner := testAgent("m-avatar-delete")
	if err := d.PutMember(owner); err != nil {
		t.Fatalf("put member: %v", err)
	}
	avatar := ChatAttachment{
		ID: "ava-hard-delete-shared", Mime: "image/png", Data: []byte("ava-hard-delete-shared"),
	}
	if err := d.ReplaceMemberAvatar(owner.ID, avatar); err != nil {
		t.Fatalf("seed avatar: %v", err)
	}
	if err := d.PutChat(ChatMessage{
		ID: "c-survivor", Sender: "owner", Recipient: "m-other", TS: 1.0,
		Meta: map[string]any{"attachments": []any{
			map[string]any{"id": avatar.ID},
		}},
	}); err != nil {
		t.Fatalf("put surviving chat: %v", err)
	}

	deleted, err := d.HardDeleteMember(owner.ID)
	if err != nil || !deleted {
		t.Fatalf("hard delete: deleted=%v err=%v", deleted, err)
	}
	mustBlobAlive(t, d, avatar.ID, "referenced by surviving chat after member deletion")
}

// TestDeleteChatInvolvingStillCollectsUnreferencedBlobs is sentinel (2) — the
// opposite direction. Sparing referenced blobs must not degrade into sparing
// everything: a blob whose ONLY referrers are the deleted messages still has
// to go, and the deletion must be observable in the store, not just in the
// returned count.
//
// This is red under a "survive everything / delete nothing" mutant of the
// liveness verdict, and is not vacuous on 2e74953 either (it passes there —
// deliberately: it is the regression fence around the T-62a8 fix, not a
// repro of it).
func TestDeleteChatInvolvingStillCollectsUnreferencedBlobs(t *testing.T) {
	d := newTestDAL(t)
	seedLivenessBlobs(t, d, "a-garbage", "a-garbage-2", "a-link-era", "a-untouched")

	for _, m := range []ChatMessage{
		{ID: "c-1", Sender: "m-1", Recipient: "owner", TS: 1.0,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "a-garbage"},
				map[string]any{"id": "a-garbage-2"},
			}}},
		{ID: "c-2", Sender: "m-1", Recipient: "owner", TS: 2.0,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "a-link-era"},
			}}},
		// Unrelated conversation — its blob is never a candidate.
		{ID: "c-3", Sender: "owner", Recipient: "m-2", TS: 3.0,
			Meta: map[string]any{"attachments": []any{
				map[string]any{"id": "a-untouched"},
			}}},
	} {
		if err := d.PutChat(m); err != nil {
			t.Fatalf("put chat: %v", err)
		}
	}
	// A LINK artifact carries no attachment_id. It must not be read as a
	// reference to the empty-string blob, and it must not keep any blob
	// alive — otherwise every link artifact on the board would pin an
	// arbitrary blob forever.
	if err := d.PutTaskArtifact(TaskArtifact{
		ID: "ta-link", TaskID: "t-1", Kind: ArtifactKindLink,
		URL: "https://example.invalid/pr/1", Label: "PR #1", CreatedTS: 4.0,
	}); err != nil {
		t.Fatalf("put link artifact: %v", err)
	}
	// An artifact pointing at a DIFFERENT task's blob must not spare the
	// candidates either (a reference is per-blob, not per-table).
	if err := d.PutTaskArtifact(TaskArtifact{
		ID: "ta-other", TaskID: "t-2", Kind: ArtifactKindFile,
		AttachmentID: "a-untouched", CreatedTS: 5.0,
	}); err != nil {
		t.Fatalf("put other artifact: %v", err)
	}

	msgs, atts, err := d.DeleteChatInvolving("m-1")
	if err != nil {
		t.Fatalf("delete involving: %v", err)
	}
	if msgs != 2 {
		t.Fatalf("want 2 deleted messages, got %d", msgs)
	}
	for _, id := range []string{"a-garbage", "a-garbage-2", "a-link-era"} {
		mustBlobGone(t, d, id, "nothing surviving references it")
	}
	mustBlobAlive(t, d, "a-untouched", "referenced by a surviving message")
	if atts != 3 {
		t.Fatalf("all 3 unreferenced blobs must be collected, got %d", atts)
	}
}
