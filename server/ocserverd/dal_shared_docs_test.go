package main

import (
	"errors"
	"net/http"
	"reflect"
	"testing"
)

func dalTestSharedDoc(id, attachmentID string) SharedDoc {
	return SharedDoc{
		ID: id, Kind: ArtifactKindFile, AttachmentID: attachmentID,
		Name: "Runbook", Description: "how we ship",
		CreatedTS: 100, CreatedBy: "kip", UpdatedTS: 100, UpdatedBy: "kip",
	}
}

func dalCreateSharedDoc(t *testing.T, d *DAL, doc SharedDoc) {
	t.Helper()
	if err := d.CreateSharedDoc(doc, nil); err != nil {
		t.Fatalf("CreateSharedDoc(%s): %v", doc.ID, err)
	}
}

func dalSharedDocVersions(t *testing.T, d *DAL, docID string) []SharedDocVersion {
	t.Helper()
	got, err := d.ListSharedDocVersions(docID)
	if err != nil {
		t.Fatalf("ListSharedDocVersions(%s): %v", docID, err)
	}
	return got
}

func dalMustGetSharedDoc(t *testing.T, d *DAL, id string) SharedDoc {
	t.Helper()
	doc, err := d.GetSharedDoc(id)
	if err != nil || doc == nil {
		t.Fatalf("GetSharedDoc(%s): %+v, %v", id, doc, err)
	}
	return *doc
}

func dalWantRefusal(t *testing.T, err error, status int, msg string) {
	t.Helper()
	var refusal *txRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("want a %d refusal, got %v", status, err)
	}
	if refusal.status != status || refusal.msg != msg {
		t.Fatalf("want %d %q, got %d %q", status, msg, refusal.status, refusal.msg)
	}
}

// dalReplaceSharedDocWith writes attachmentID as the next version at ts.
func dalReplaceSharedDocWith(t *testing.T, d *DAL, id, attachmentID, actor string, ts float64) {
	t.Helper()
	if err := d.ReplaceSharedDoc(SharedDocReplacement{
		ID: id, AttachmentID: attachmentID, UpdatedBy: actor, UpdatedTS: ts,
	}, nil); err != nil {
		t.Fatalf("ReplaceSharedDoc(%s → %s): %v", id, attachmentID, err)
	}
}

func TestListSharedDocs(t *testing.T) {
	d := newAPITestDAL(t)
	b := dalTestSharedDoc("sd-b", "att-1")
	b.Name = "Beta"
	a2 := dalTestSharedDoc("sd-a2", "att-1")
	a2.Name = "Alpha"
	a1 := dalTestSharedDoc("sd-a1", "att-1")
	a1.Name = "Alpha"
	for _, doc := range []SharedDoc{b, a2, a1} {
		dalCreateSharedDoc(t, d, doc)
	}

	got, err := d.ListSharedDocs()
	if err != nil {
		t.Fatalf("ListSharedDocs: %v", err)
	}
	if want := []SharedDoc{a1, a2, b}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ListSharedDocs sorted by name then id:\n got %+v\nwant %+v", got, want)
	}
}

func TestCreateSharedDoc(t *testing.T) {
	t.Run("a minted link blob is stored with the row that pins it", func(t *testing.T) {
		d := newAPITestDAL(t)
		doc := dalTestSharedDoc("sd-1", "att-link")
		doc.Kind = ArtifactKindLink
		if err := d.CreateSharedDoc(doc, &ChatAttachment{
			ID: "att-link", Mime: linkTargetMime, Data: []byte("https://example.com"),
		}); err != nil {
			t.Fatalf("CreateSharedDoc: %v", err)
		}
		if got := dalMustGetSharedDoc(t, d, "sd-1"); !reflect.DeepEqual(got, doc) {
			t.Fatalf("stored row:\n got %+v\nwant %+v", got, doc)
		}
		if got, want := dalStoredBlobIDs(t, d), []string{"att-link"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("stored blobs: want %v, got %v", want, got)
		}
	})
}

func TestReplaceSharedDoc(t *testing.T) {
	t.Run("the displaced version is retained with who wrote it and when", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalCreateSharedDoc(t, d, dalTestSharedDoc("sd-1", "att-1"))
		name, description := "Runbook v2", ""
		if err := d.ReplaceSharedDoc(SharedDocReplacement{
			ID: "sd-1", AttachmentID: "att-2", Name: &name, Description: &description,
			UpdatedTS: 200, UpdatedBy: "ann",
		}, nil); err != nil {
			t.Fatalf("ReplaceSharedDoc: %v", err)
		}
		next := dalTestSharedDoc("sd-1", "att-2")
		next.Name, next.Description, next.UpdatedTS, next.UpdatedBy = "Runbook v2", "", 200, "ann"
		if got := dalMustGetSharedDoc(t, d, "sd-1"); !reflect.DeepEqual(got, next) {
			t.Fatalf("current:\n got %+v\nwant %+v", got, next)
		}
		got := dalSharedDocVersions(t, d, "sd-1")
		want := []SharedDocVersion{{
			ID: 1, DocID: "sd-1", Kind: ArtifactKindFile, AttachmentID: "att-1",
			Name: "Runbook", Description: "how we ship", CreatedTS: 100, CreatedBy: "kip",
		}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("versions:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a replace that omits the name keeps a rename committed after the caller read the doc", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalCreateSharedDoc(t, d, dalTestSharedDoc("sd-1", "att-1"))
		renamed := "Renamed"
		if err := d.ReplaceSharedDoc(SharedDocReplacement{
			ID: "sd-1", AttachmentID: "att-2", Name: &renamed, UpdatedTS: 200, UpdatedBy: "ann",
		}, nil); err != nil {
			t.Fatalf("first ReplaceSharedDoc: %v", err)
		}
		if err := d.ReplaceSharedDoc(SharedDocReplacement{
			ID: "sd-1", AttachmentID: "att-3", UpdatedTS: 300, UpdatedBy: "bob",
		}, nil); err != nil {
			t.Fatalf("second ReplaceSharedDoc: %v", err)
		}
		want := SharedDoc{
			ID: "sd-1", Kind: ArtifactKindFile, AttachmentID: "att-3",
			Name: "Renamed", Description: "how we ship",
			CreatedTS: 100, CreatedBy: "kip", UpdatedTS: 300, UpdatedBy: "bob",
		}
		if got := dalMustGetSharedDoc(t, d, "sd-1"); !reflect.DeepEqual(got, want) {
			t.Fatalf("current:\n got %+v\nwant %+v", got, want)
		}
		wantVersions := []SharedDocVersion{
			{ID: 2, DocID: "sd-1", Kind: ArtifactKindFile, AttachmentID: "att-2",
				Name: "Renamed", Description: "how we ship", CreatedTS: 200, CreatedBy: "ann"},
			{ID: 1, DocID: "sd-1", Kind: ArtifactKindFile, AttachmentID: "att-1",
				Name: "Runbook", Description: "how we ship", CreatedTS: 100, CreatedBy: "kip"},
		}
		if got := dalSharedDocVersions(t, d, "sd-1"); !reflect.DeepEqual(got, wantVersions) {
			t.Fatalf("versions:\n got %+v\nwant %+v", got, wantVersions)
		}
	})

	t.Run("versions beyond the retention depth are discarded with the blobs only they cited", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutBlobs(t, d, "att-1", "att-2", "att-3", "att-4", "att-5", "att-6")
		dalPutChats(t, d, dalChatWithAtts("m1", "ann", "bob", 100, dalAttRef("att-2", "image/png", "b.png")))
		dalCreateSharedDoc(t, d, dalTestSharedDoc("sd-1", "att-1"))
		for i, att := range []string{"att-2", "att-3", "att-4", "att-5", "att-6"} {
			dalReplaceSharedDocWith(t, d, "sd-1", att, "kip", float64(200+i))
		}
		var kept []string
		for _, v := range dalSharedDocVersions(t, d, "sd-1") {
			kept = append(kept, v.AttachmentID)
		}
		if want := []string{"att-5", "att-4", "att-3"}; !reflect.DeepEqual(kept, want) {
			t.Fatalf("retained versions newest first: want %v, got %v", want, kept)
		}
		want := []string{"att-2", "att-3", "att-4", "att-5", "att-6"}
		if got := dalStoredBlobIDs(t, d); !reflect.DeepEqual(got, want) {
			t.Fatalf("stored blobs (att-2 survives on its chat message): want %v, got %v", want, got)
		}
	})

	t.Run("an empty attachment id keeps the current content while the name changes", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalCreateSharedDoc(t, d, dalTestSharedDoc("sd-1", "att-1"))
		renamed := "Renamed"
		if err := d.ReplaceSharedDoc(SharedDocReplacement{
			ID: "sd-1", Name: &renamed, UpdatedTS: 200, UpdatedBy: "ann",
		}, nil); err != nil {
			t.Fatalf("ReplaceSharedDoc: %v", err)
		}
		want := dalTestSharedDoc("sd-1", "att-1")
		want.Name, want.UpdatedTS, want.UpdatedBy = "Renamed", 200, "ann"
		if got := dalMustGetSharedDoc(t, d, "sd-1"); !reflect.DeepEqual(got, want) {
			t.Fatalf("current:\n got %+v\nwant %+v", got, want)
		}
		wantVersions := []SharedDocVersion{{
			ID: 1, DocID: "sd-1", Kind: ArtifactKindFile, AttachmentID: "att-1",
			Name: "Runbook", Description: "how we ship", CreatedTS: 100, CreatedBy: "kip",
		}}
		if got := dalSharedDocVersions(t, d, "sd-1"); !reflect.DeepEqual(got, wantVersions) {
			t.Fatalf("versions:\n got %+v\nwant %+v", got, wantVersions)
		}
	})

	t.Run("an unknown id is refused with a 404", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalWantRefusal(t, d.ReplaceSharedDoc(SharedDocReplacement{ID: "sd-ghost", AttachmentID: "att-1"}, nil),
			http.StatusNotFound, "shared doc 'sd-ghost' not found")
	})
}

func TestRestoreSharedDocVersion(t *testing.T) {
	t.Run("the restored version leaves history and the displaced current one becomes the newest version", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalCreateSharedDoc(t, d, dalTestSharedDoc("sd-1", "att-1"))
		dalReplaceSharedDocWith(t, d, "sd-1", "att-2", "ann", 200)
		dalReplaceSharedDocWith(t, d, "sd-1", "att-3", "bob", 300)
		if err := d.RestoreSharedDocVersion("sd-1", 1, "owner", 400); err != nil {
			t.Fatalf("RestoreSharedDocVersion: %v", err)
		}
		wantDoc := dalTestSharedDoc("sd-1", "att-1")
		wantDoc.UpdatedTS, wantDoc.UpdatedBy = 400, "owner"
		if got := dalMustGetSharedDoc(t, d, "sd-1"); !reflect.DeepEqual(got, wantDoc) {
			t.Fatalf("current:\n got %+v\nwant %+v", got, wantDoc)
		}
		got := dalSharedDocVersions(t, d, "sd-1")
		want := []SharedDocVersion{
			{ID: 3, DocID: "sd-1", Kind: ArtifactKindFile, AttachmentID: "att-3",
				Name: "Runbook", Description: "how we ship", CreatedTS: 300, CreatedBy: "bob"},
			{ID: 2, DocID: "sd-1", Kind: ArtifactKindFile, AttachmentID: "att-2",
				Name: "Runbook", Description: "how we ship", CreatedTS: 200, CreatedBy: "ann"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("versions:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a version id of another document is refused with a 404", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalCreateSharedDoc(t, d, dalTestSharedDoc("sd-1", "att-1"))
		dalCreateSharedDoc(t, d, dalTestSharedDoc("sd-2", "att-1"))
		dalReplaceSharedDocWith(t, d, "sd-2", "att-2", "ann", 200)
		dalWantRefusal(t, d.RestoreSharedDocVersion("sd-1", 1, "owner", 400),
			http.StatusNotFound, "version 1 is not a retained version of shared doc 'sd-1'")
	})
}

func TestDeleteSharedDoc(t *testing.T) {
	t.Run("a document goes with its versions and every blob nothing else cites", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutBlobs(t, d, "att-1", "att-2", "att-3", "att-other")
		dalPutChats(t, d, dalChatWithAtts("m1", "ann", "bob", 100, dalAttRef("att-1", "image/png", "a.png")))
		dalCreateSharedDoc(t, d, dalTestSharedDoc("sd-1", "att-1"))
		dalReplaceSharedDocWith(t, d, "sd-1", "att-2", "ann", 200)
		dalReplaceSharedDocWith(t, d, "sd-1", "att-3", "ann", 300)
		dalCreateSharedDoc(t, d, dalTestSharedDoc("sd-2", "att-other"))

		if err := d.DeleteSharedDoc("sd-1"); err != nil {
			t.Fatalf("DeleteSharedDoc: %v", err)
		}
		if doc, err := d.GetSharedDoc("sd-1"); err != nil || doc != nil {
			t.Fatalf("GetSharedDoc after delete: want nil, got %+v, %v", doc, err)
		}
		if got := dalSharedDocVersions(t, d, "sd-1"); got != nil {
			t.Fatalf("versions after delete: want none, got %+v", got)
		}
		want := []string{"att-1", "att-other"}
		if got := dalStoredBlobIDs(t, d); !reflect.DeepEqual(got, want) {
			t.Fatalf("stored blobs (att-1 survives on its chat message): want %v, got %v", want, got)
		}
	})

	t.Run("an unknown id is refused with a 404", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalWantRefusal(t, d.DeleteSharedDoc("sd-ghost"),
			http.StatusNotFound, "shared doc 'sd-ghost' not found")
	})
}
