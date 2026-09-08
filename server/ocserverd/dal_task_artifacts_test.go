package main

// dal_task_artifacts_test.go — the pinned-deliverable tables: what a pin stores,
// what a replace retains, which blobs a removal takes with it, and what a failed
// write leaves behind.

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
)

func TestScanTaskArtifact(t *testing.T) {
	d := newAPITestDAL(t)
	full := dalTestArtifact("ta-1", "T-1")
	dalPutArtifact(t, d, full)
	bare := dalPutArtifact(t, d, TaskArtifact{
		ID: "ta-2", TaskID: "T-1", Kind: ArtifactKindLink, AttachmentID: "att-2",
	})

	query := `SELECT ` + taskArtifactColumns + ` FROM task_artifact WHERE id = ?`

	got, err := scanTaskArtifact(d.rdb.QueryRow(query, "ta-1"))
	if err != nil {
		t.Fatalf("scanTaskArtifact(ta-1): %v", err)
	}
	if !reflect.DeepEqual(got, full) {
		t.Fatalf("scanTaskArtifact(ta-1):\n got %+v\nwant %+v", got, full)
	}

	got, err = scanTaskArtifact(d.rdb.QueryRow(query, "ta-2"))
	if err != nil {
		t.Fatalf("scanTaskArtifact(ta-2): %v", err)
	}
	if !reflect.DeepEqual(got, bare) {
		t.Fatalf("scanTaskArtifact(ta-2):\n got %+v\nwant %+v", got, bare)
	}

	if _, err := scanTaskArtifact(d.rdb.QueryRow(query, "ta-ghost")); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("scanTaskArtifact(ta-ghost): want sql.ErrNoRows, got %v", err)
	}
}

func TestListTaskArtifacts(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("a task with nothing pinned reads back as nil", func(t *testing.T) {
		got, err := d.ListTaskArtifacts("T-1")
		if err != nil {
			t.Fatalf("ListTaskArtifacts: %v", err)
		}
		if got != nil {
			t.Fatalf("ListTaskArtifacts on an empty card: want nil, got %+v", got)
		}
	})

	late := dalTestArtifact("ta-late", "T-1")
	late.CreatedTS = 300
	earlyB := dalTestArtifact("ta-b", "T-1")
	earlyB.CreatedTS = 100
	earlyA := dalTestArtifact("ta-a", "T-1")
	earlyA.CreatedTS = 100
	other := dalTestArtifact("ta-other", "T-2")
	other.CreatedTS = 1
	for _, a := range []TaskArtifact{late, earlyB, earlyA, other} {
		dalPutArtifact(t, d, a)
	}

	t.Run("one task's pins come back oldest first, ties broken by id, and no other task's", func(t *testing.T) {
		got, err := d.ListTaskArtifacts("T-1")
		if err != nil {
			t.Fatalf("ListTaskArtifacts: %v", err)
		}
		want := []TaskArtifact{earlyA, earlyB, late}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ListTaskArtifacts(T-1):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a task id nothing was pinned to is still nil while its neighbours are not", func(t *testing.T) {
		got, err := d.ListTaskArtifacts("T-3")
		if err != nil {
			t.Fatalf("ListTaskArtifacts(T-3): %v", err)
		}
		if got != nil {
			t.Fatalf("ListTaskArtifacts(T-3): want nil, got %+v", got)
		}
		neighbour, err := d.ListTaskArtifacts("T-2")
		if err != nil {
			t.Fatalf("ListTaskArtifacts(T-2): %v", err)
		}
		if !reflect.DeepEqual(neighbour, []TaskArtifact{other}) {
			t.Fatalf("ListTaskArtifacts(T-2):\n got %+v\nwant %+v", neighbour, []TaskArtifact{other})
		}
	})
}

func TestGetTaskArtifactOn(t *testing.T) {
	d := newAPITestDAL(t)
	stored := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))

	t.Run("a stored pin reads back whole and an unknown id is nil, not an error", func(t *testing.T) {
		got, err := getTaskArtifactOn(d.rdb, "ta-1")
		if err != nil {
			t.Fatalf("getTaskArtifactOn(ta-1): %v", err)
		}
		if got == nil || !reflect.DeepEqual(*got, stored) {
			t.Fatalf("getTaskArtifactOn(ta-1):\n got %+v\nwant %+v", got, stored)
		}
		missing, err := getTaskArtifactOn(d.rdb, "ta-ghost")
		if err != nil {
			t.Fatalf("getTaskArtifactOn(ta-ghost): %v", err)
		}
		if missing != nil {
			t.Fatalf("getTaskArtifactOn(ta-ghost): want nil, got %+v", *missing)
		}
	})

	t.Run("inside a transaction it reads that transaction's own uncommitted write, while the pool still reads the old row", func(t *testing.T) {
		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if _, err := tx.Exec(`UPDATE task_artifact SET name = ? WHERE id = ?`, "renamed in flight", "ta-1"); err != nil {
			t.Fatalf("update in the transaction: %v", err)
		}
		inTx, err := getTaskArtifactOn(tx, "ta-1")
		if err != nil {
			t.Fatalf("getTaskArtifactOn(tx): %v", err)
		}
		want := stored
		want.Name = "renamed in flight"
		if inTx == nil || !reflect.DeepEqual(*inTx, want) {
			t.Fatalf("getTaskArtifactOn(tx):\n got %+v\nwant %+v", inTx, want)
		}
		onPool, err := getTaskArtifactOn(d.rdb, "ta-1")
		if err != nil {
			t.Fatalf("getTaskArtifactOn(pool): %v", err)
		}
		if onPool == nil || !reflect.DeepEqual(*onPool, stored) {
			t.Fatalf("getTaskArtifactOn(pool) mid-transaction:\n got %+v\nwant %+v", onPool, stored)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		dalWantArtifact(t, d, stored)
	})
}

func TestAllTaskArtifactCounts(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("no artifacts at all is an empty map, not nil", func(t *testing.T) {
		got, err := d.AllTaskArtifactCounts()
		if err != nil {
			t.Fatalf("AllTaskArtifactCounts: %v", err)
		}
		if !reflect.DeepEqual(got, map[string]int{}) {
			t.Fatalf("AllTaskArtifactCounts on an empty table: want an empty map, got %v", got)
		}
	})

	t.Run("each task counts its own pins and a task with none is simply absent", func(t *testing.T) {
		dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
		dalPutArtifact(t, d, dalTestArtifact("ta-2", "T-1"))
		dalPutArtifact(t, d, dalTestArtifact("ta-3", "T-2"))
		dalPutTask(t, d, dalTestTask("T-9"))

		got, err := d.AllTaskArtifactCounts()
		if err != nil {
			t.Fatalf("AllTaskArtifactCounts: %v", err)
		}
		want := map[string]int{"T-1": 2, "T-2": 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("AllTaskArtifactCounts: want %v, got %v", want, got)
		}
	})
}

func TestCountTaskArtifacts(t *testing.T) {
	d := newAPITestDAL(t)
	got, err := d.CountTaskArtifacts("T-1")
	if err != nil {
		t.Fatalf("CountTaskArtifacts on an empty card: %v", err)
	}
	if got != 0 {
		t.Fatalf("CountTaskArtifacts on an empty card: want 0, got %d", got)
	}

	dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
	dalPutArtifact(t, d, dalTestArtifact("ta-2", "T-1"))
	dalPutArtifact(t, d, dalTestArtifact("ta-3", "T-2"))

	if got, err = d.CountTaskArtifacts("T-1"); err != nil || got != 2 {
		t.Fatalf("CountTaskArtifacts(T-1): want 2, got %d (%v)", got, err)
	}
	if got, err = d.CountTaskArtifacts("T-2"); err != nil || got != 1 {
		t.Fatalf("CountTaskArtifacts(T-2): want 1, got %d (%v)", got, err)
	}
	if got, err = d.CountTaskArtifacts("T-3"); err != nil || got != 0 {
		t.Fatalf("CountTaskArtifacts(T-3): want 0, got %d (%v)", got, err)
	}
}

func TestGetTaskArtifactBlob(t *testing.T) {
	d := newAPITestDAL(t)
	report := "quarterly-report.pdf"
	dalPutBlob(t, d, ChatAttachment{
		ID: "att-file", Mime: "application/pdf", Data: []byte("%PDF-1.7 bytes"), Filename: &report,
	})
	dalPutBlob(t, d, ChatAttachment{
		ID: "att-link", Mime: linkTargetMime, Data: []byte("https://example.test/pr/1\r\n"),
	})
	dalPutBlob(t, d, ChatAttachment{ID: "att-nameless", Mime: "image/png", Data: []byte("png bytes")})

	t.Run("an ordinary blob answers its mime and filename while its bytes stay in the store", func(t *testing.T) {
		got, err := d.GetTaskArtifactBlob("att-file")
		if err != nil {
			t.Fatalf("GetTaskArtifactBlob(att-file): %v", err)
		}
		want := ChatAttachment{ID: "att-file", Mime: "application/pdf", Data: nil, Filename: &report}
		if got == nil || !reflect.DeepEqual(*got, want) {
			t.Fatalf("GetTaskArtifactBlob(att-file):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a link target answers with its bytes, because the target IS the content", func(t *testing.T) {
		got, err := d.GetTaskArtifactBlob("att-link")
		if err != nil {
			t.Fatalf("GetTaskArtifactBlob(att-link): %v", err)
		}
		want := ChatAttachment{ID: "att-link", Mime: linkTargetMime, Data: []byte("https://example.test/pr/1\r\n")}
		if got == nil || !reflect.DeepEqual(*got, want) {
			t.Fatalf("GetTaskArtifactBlob(att-link):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a blob stored with no filename answers a nil filename rather than an empty one", func(t *testing.T) {
		got, err := d.GetTaskArtifactBlob("att-nameless")
		if err != nil {
			t.Fatalf("GetTaskArtifactBlob(att-nameless): %v", err)
		}
		want := ChatAttachment{ID: "att-nameless", Mime: "image/png"}
		if got == nil || !reflect.DeepEqual(*got, want) {
			t.Fatalf("GetTaskArtifactBlob(att-nameless):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("an id the store never held is nil rather than an error", func(t *testing.T) {
		got, err := d.GetTaskArtifactBlob("att-ghost")
		if err != nil {
			t.Fatalf("GetTaskArtifactBlob(att-ghost): %v", err)
		}
		if got != nil {
			t.Fatalf("GetTaskArtifactBlob(att-ghost): want nil, got %+v", *got)
		}
	})
}

func TestPutTaskArtifact(t *testing.T) {
	t.Run("one pin lands whole and its neighbour is untouched", func(t *testing.T) {
		d := newAPITestDAL(t)
		first := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
		second := dalTestArtifact("ta-2", "T-1")
		second.Kind = ArtifactKindLink
		second.Name = "the pull request"
		dalPutArtifact(t, d, second)
		dalWantArtifact(t, d, first)
		dalWantArtifact(t, d, second)
	})

	t.Run("registering the same id twice is refused and leaves the stored pin as it was", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))

		clash := dalTestArtifact("ta-1", "T-2")
		clash.Name = "a second registration"
		err := d.PutTaskArtifact(clash)
		if err == nil {
			t.Fatalf("PutTaskArtifact onto an occupied id: want an error, got nil")
		}
		if err.Error() != "constraint failed: UNIQUE constraint failed: task_artifact.id (1555)" {
			t.Fatalf("PutTaskArtifact onto an occupied id: got %q", err.Error())
		}
		dalWantArtifact(t, d, stored)
		if got := dalArtifactIDs(t, d); !reflect.DeepEqual(got, []string{"ta-1"}) {
			t.Fatalf("stored artifacts after the refusal: want [ta-1], got %v", got)
		}
	})

	t.Run("a kind outside the closed set is refused and writes nothing", func(t *testing.T) {
		d := newAPITestDAL(t)
		bad := dalTestArtifact("ta-1", "T-1")
		bad.Kind = "spreadsheet"
		err := d.PutTaskArtifact(bad)
		if err == nil {
			t.Fatalf("PutTaskArtifact with an unknown kind: want an error, got nil")
		}
		if err.Error() != "constraint failed: CHECK constraint failed: kind IN ('file', 'image', 'link') (275)" {
			t.Fatalf("PutTaskArtifact with an unknown kind: got %q", err.Error())
		}
		if got := dalArtifactIDs(t, d); !reflect.DeepEqual(got, []string{}) {
			t.Fatalf("stored artifacts after the refusal: want none, got %v", got)
		}
	})
}

func TestPutTaskArtifactMintingBlob(t *testing.T) {
	t.Run("the pin and the blob it was given land together", func(t *testing.T) {
		d := newAPITestDAL(t)
		a := dalTestArtifact("ta-1", "T-1")
		a.AttachmentID = "att-new"
		blob := ChatAttachment{ID: "att-new", Mime: linkTargetMime, Data: []byte("https://example.test/pr/1")}
		if err := d.PutTaskArtifactMintingBlob(a, &blob); err != nil {
			t.Fatalf("PutTaskArtifactMintingBlob: %v", err)
		}
		dalWantArtifact(t, d, a)
		dalWantBlob(t, d, blob)
	})

	t.Run("a pin that reuses a blob already in the store mints nothing new", func(t *testing.T) {
		d := newAPITestDAL(t)
		existing := ChatAttachment{ID: "att-1", Mime: "image/png", Data: []byte("png bytes")}
		dalPutBlob(t, d, existing)
		a := dalTestArtifact("ta-1", "T-1")
		a.AttachmentID = "att-1"
		if err := d.PutTaskArtifactMintingBlob(a, nil); err != nil {
			t.Fatalf("PutTaskArtifactMintingBlob: %v", err)
		}
		dalWantArtifact(t, d, a)
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{"att-1"}) {
			t.Fatalf("blobs after a reusing pin: want [att-1], got %v", got)
		}
	})

	t.Run("a refused pin takes its freshly minted blob down with it", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
		stored := dalTestArtifact("ta-1", "T-1")

		clash := dalTestArtifact("ta-1", "T-2")
		clash.AttachmentID = "att-doomed"
		blob := ChatAttachment{ID: "att-doomed", Mime: linkTargetMime, Data: []byte("https://example.test/pr/2")}
		err := d.PutTaskArtifactMintingBlob(clash, &blob)
		if err == nil {
			t.Fatalf("PutTaskArtifactMintingBlob onto an occupied id: want an error, got nil")
		}
		if err.Error() != "constraint failed: UNIQUE constraint failed: task_artifact.id (1555)" {
			t.Fatalf("PutTaskArtifactMintingBlob onto an occupied id: got %q", err.Error())
		}
		dalWantArtifact(t, d, stored)
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{}) {
			t.Fatalf("blobs after the rollback: want none, got %v", got)
		}

		if err := d.PutTaskArtifactMintingBlob(dalTestArtifact("ta-2", "T-1"), nil); err != nil {
			t.Fatalf("the write pool is wedged after the rollback: %v", err)
		}
	})
}

func TestReplaceTaskArtifactMintingBlob(t *testing.T) {
	t.Run("the new blob and the swap land together, and the replaced version is retained", func(t *testing.T) {
		d := newAPITestDAL(t)
		before := dalTestArtifact("ta-1", "T-1")
		before.AttachmentID = "att-1"
		dalPutBlob(t, d, ChatAttachment{ID: "att-1", Mime: "image/png", Data: []byte("first bytes")})
		dalPutArtifact(t, d, before)

		next := before
		next.AttachmentID = "att-2"
		next.Name = "the second draft"
		next.CreatedTS = 400
		blob := ChatAttachment{ID: "att-2", Mime: "image/png", Data: []byte("second bytes")}
		replaced, err := d.ReplaceTaskArtifactMintingBlob(next, &blob)
		if err != nil {
			t.Fatalf("ReplaceTaskArtifactMintingBlob: %v", err)
		}
		if !replaced {
			t.Fatalf("ReplaceTaskArtifactMintingBlob: want true, got false")
		}
		dalWantArtifact(t, d, next)
		dalWantBlob(t, d, blob)
		dalWantArtifactHistory(t, d, "ta-1", []TaskArtifactHistory{dalHistoryOf(before)})
	})

	t.Run("no blob at all is the plain replace", func(t *testing.T) {
		d := newAPITestDAL(t)
		before := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
		next := before
		next.Name = "renamed only"
		replaced, err := d.ReplaceTaskArtifactMintingBlob(next, nil)
		if err != nil {
			t.Fatalf("ReplaceTaskArtifactMintingBlob: %v", err)
		}
		if !replaced {
			t.Fatalf("ReplaceTaskArtifactMintingBlob: want true, got false")
		}
		dalWantArtifact(t, d, next)
		dalWantArtifactHistory(t, d, "ta-1", []TaskArtifactHistory{dalHistoryOf(before)})
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{}) {
			t.Fatalf("blobs after a blob-less replace: want none, got %v", got)
		}
	})

	t.Run("an id naming no artifact reports false — and the blob it was handed is stored anyway", func(t *testing.T) {
		d := newAPITestDAL(t)
		bystander := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
		blob := ChatAttachment{ID: "att-orphan", Mime: "image/png", Data: []byte("orphan bytes")}
		replaced, err := d.ReplaceTaskArtifactMintingBlob(dalTestArtifact("ta-ghost", "T-1"), &blob)
		if err != nil {
			t.Fatalf("ReplaceTaskArtifactMintingBlob(ta-ghost): %v", err)
		}
		if replaced {
			t.Fatalf("ReplaceTaskArtifactMintingBlob(ta-ghost): want false, got true")
		}
		dalWantArtifact(t, d, bystander)
		if got := dalArtifactIDs(t, d); !reflect.DeepEqual(got, []string{"ta-1"}) {
			t.Fatalf("artifacts after the lost race: want [ta-1], got %v", got)
		}
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{"att-orphan"}) {
			t.Fatalf("blobs after the lost race: want [att-orphan], got %v", got)
		}
		dalWantArtifactHistory(t, d, "ta-ghost", nil)
	})
}

func TestDeleteTaskArtifact(t *testing.T) {
	t.Run("un-pinning a file removes the row and reports true, while its live blob is spared", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutBlob(t, d, ChatAttachment{ID: "att-1", Mime: "application/pdf", Data: []byte("report bytes")})
		dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
		bystander := dalPutArtifact(t, d, dalTestArtifact("ta-2", "T-1"))

		removed, err := d.DeleteTaskArtifact("ta-1")
		if err != nil {
			t.Fatalf("DeleteTaskArtifact(ta-1): %v", err)
		}
		if !removed {
			t.Fatalf("DeleteTaskArtifact(ta-1): want true, got false")
		}
		if got := dalArtifactIDs(t, d); !reflect.DeepEqual(got, []string{"ta-2"}) {
			t.Fatalf("artifacts after the un-pin: want [ta-2], got %v", got)
		}
		dalWantArtifact(t, d, bystander)
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{"att-1"}) {
			t.Fatalf("blobs after un-pinning a file: want [att-1] kept, got %v", got)
		}
	})

	t.Run("un-pinning a link puts its own blob up for collection, and nothing else references it", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutBlob(t, d, ChatAttachment{ID: "att-link", Mime: linkTargetMime, Data: []byte("https://example.test/pr/1")})
		dalPutBlob(t, d, ChatAttachment{ID: "att-other", Mime: "image/png", Data: []byte("png bytes")})
		link := dalTestArtifact("ta-1", "T-1")
		link.Kind = ArtifactKindLink
		link.AttachmentID = "att-link"
		dalPutArtifact(t, d, link)
		keeper := dalTestArtifact("ta-2", "T-1")
		keeper.AttachmentID = "att-other"
		dalPutArtifact(t, d, keeper)

		if _, err := d.DeleteTaskArtifact("ta-1"); err != nil {
			t.Fatalf("DeleteTaskArtifact(ta-1): %v", err)
		}
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{"att-other"}) {
			t.Fatalf("blobs after un-pinning a link: want [att-other], got %v", got)
		}
		dalWantArtifact(t, d, keeper)
	})

	t.Run("a link's blob that a second task also pins survives the collection", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutBlob(t, d, ChatAttachment{ID: "att-link", Mime: linkTargetMime, Data: []byte("https://example.test/pr/1")})
		for _, pair := range [][2]string{{"ta-1", "T-1"}, {"ta-2", "T-2"}} {
			link := dalTestArtifact(pair[0], pair[1])
			link.Kind = ArtifactKindLink
			link.AttachmentID = "att-link"
			dalPutArtifact(t, d, link)
		}
		if _, err := d.DeleteTaskArtifact("ta-1"); err != nil {
			t.Fatalf("DeleteTaskArtifact(ta-1): %v", err)
		}
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{"att-link"}) {
			t.Fatalf("blobs after un-pinning one of two link pins: want [att-link], got %v", got)
		}
	})

	t.Run("the retained versions go with the row, and the blobs only they named are collected", func(t *testing.T) {
		d := newAPITestDAL(t)
		for _, id := range []string{"att-1", "att-2", "att-3"} {
			dalPutBlob(t, d, ChatAttachment{ID: id, Mime: "image/png", Data: []byte("bytes of " + id)})
		}
		first := dalTestArtifact("ta-1", "T-1")
		first.AttachmentID = "att-1"
		dalPutArtifact(t, d, first)
		second := first
		second.AttachmentID = "att-2"
		dalReplaceArtifact(t, d, second, true)
		third := first
		third.AttachmentID = "att-3"
		dalReplaceArtifact(t, d, third, true)
		dalWantArtifactHistory(t, d, "ta-1", []TaskArtifactHistory{dalHistoryOf(second), dalHistoryOf(first)})

		removed, err := d.DeleteTaskArtifact("ta-1")
		if err != nil {
			t.Fatalf("DeleteTaskArtifact(ta-1): %v", err)
		}
		if !removed {
			t.Fatalf("DeleteTaskArtifact(ta-1): want true, got false")
		}
		dalWantArtifactHistory(t, d, "ta-1", nil)
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{"att-3"}) {
			t.Fatalf("blobs after the un-pin: want the live one [att-3] spared, got %v", got)
		}
	})

	t.Run("an id naming no artifact reports false and removes nothing", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutBlob(t, d, ChatAttachment{ID: "att-1", Mime: "image/png", Data: []byte("png bytes")})
		stored := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))

		removed, err := d.DeleteTaskArtifact("ta-ghost")
		if err != nil {
			t.Fatalf("DeleteTaskArtifact(ta-ghost): %v", err)
		}
		if removed {
			t.Fatalf("DeleteTaskArtifact(ta-ghost): want false, got true")
		}
		dalWantArtifact(t, d, stored)
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{"att-1"}) {
			t.Fatalf("blobs after the no-op delete: want [att-1], got %v", got)
		}
	})
}

func TestReplaceTaskArtifact(t *testing.T) {
	t.Run("the content is swapped, the id stays put, and the version replaced is retained", func(t *testing.T) {
		d := newAPITestDAL(t)
		before := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
		bystander := dalPutArtifact(t, d, dalTestArtifact("ta-2", "T-1"))

		next := TaskArtifact{
			ID: "ta-1", TaskID: "T-1", Kind: ArtifactKindLink, AttachmentID: "att-9",
			Name: "the second draft", Description: "what changed", CreatedTS: 500, CreatedBy: "bob",
		}
		replaced, err := d.ReplaceTaskArtifact(next)
		if err != nil {
			t.Fatalf("ReplaceTaskArtifact: %v", err)
		}
		if !replaced {
			t.Fatalf("ReplaceTaskArtifact: want true, got false")
		}
		dalWantArtifact(t, d, next)
		dalWantArtifact(t, d, bystander)
		dalWantArtifactHistory(t, d, "ta-1", []TaskArtifactHistory{dalHistoryOf(before)})
		dalWantArtifactHistory(t, d, "ta-2", nil)
	})

	t.Run("the task_id a replace carries is ignored — the row keeps the card it was pinned to", func(t *testing.T) {
		d := newAPITestDAL(t)
		before := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
		next := before
		next.TaskID = "T-2"
		next.Name = "moved?"
		if _, err := d.ReplaceTaskArtifact(next); err != nil {
			t.Fatalf("ReplaceTaskArtifact: %v", err)
		}
		want := next
		want.TaskID = "T-1"
		dalWantArtifact(t, d, want)
	})

	t.Run("only the newest three versions are retained and the blobs that fall off the end are collected", func(t *testing.T) {
		d := newAPITestDAL(t)
		for i := 1; i <= 5; i++ {
			dalPutBlob(t, d, ChatAttachment{ID: dalAttID(i), Mime: "image/png", Data: []byte("bytes")})
		}
		versions := make([]TaskArtifact, 0, 5)
		for i := 1; i <= 5; i++ {
			v := dalTestArtifact("ta-1", "T-1")
			v.AttachmentID = dalAttID(i)
			v.Name = "draft " + dalAttID(i)
			versions = append(versions, v)
		}
		dalPutArtifact(t, d, versions[0])
		for _, v := range versions[1:] {
			dalReplaceArtifact(t, d, v, true)
		}

		dalWantArtifact(t, d, versions[4])
		dalWantArtifactHistory(t, d, "ta-1", []TaskArtifactHistory{
			dalHistoryOf(versions[3]), dalHistoryOf(versions[2]), dalHistoryOf(versions[1]),
		})
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{"att-2", "att-3", "att-4", "att-5"}) {
			t.Fatalf("blobs after five versions: want att-1 collected, got %v", got)
		}
	})

	t.Run("an id naming no artifact reports false, writes no version and touches no other row", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
		replaced, err := d.ReplaceTaskArtifact(dalTestArtifact("ta-ghost", "T-1"))
		if err != nil {
			t.Fatalf("ReplaceTaskArtifact(ta-ghost): %v", err)
		}
		if replaced {
			t.Fatalf("ReplaceTaskArtifact(ta-ghost): want false, got true")
		}
		dalWantArtifact(t, d, stored)
		if got := dalArtifactIDs(t, d); !reflect.DeepEqual(got, []string{"ta-1"}) {
			t.Fatalf("artifacts after the lost race: want [ta-1], got %v", got)
		}
		dalWantArtifactHistory(t, d, "ta-ghost", nil)
	})
}

func TestReplaceTaskArtifactOn(t *testing.T) {
	t.Run("the swap and its retained version are the caller's to commit", func(t *testing.T) {
		d := newAPITestDAL(t)
		before := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))

		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		next := before
		next.Name = "the second draft"
		replaced, err := replaceTaskArtifactOn(tx, next)
		if err != nil {
			t.Fatalf("replaceTaskArtifactOn: %v", err)
		}
		if !replaced {
			t.Fatalf("replaceTaskArtifactOn: want true, got false")
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		dalWantArtifact(t, d, next)
		dalWantArtifactHistory(t, d, "ta-1", []TaskArtifactHistory{dalHistoryOf(before)})
	})

	t.Run("a rolled-back swap leaves neither the new content nor a retained version", func(t *testing.T) {
		d := newAPITestDAL(t)
		before := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))

		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		next := before
		next.Name = "never committed"
		if _, err := replaceTaskArtifactOn(tx, next); err != nil {
			t.Fatalf("replaceTaskArtifactOn: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		dalWantArtifact(t, d, before)
		dalWantArtifactHistory(t, d, "ta-1", nil)

		if _, err := d.ReplaceTaskArtifact(next); err != nil {
			t.Fatalf("the write pool is wedged after the rollback: %v", err)
		}
		dalWantArtifact(t, d, next)
	})

	t.Run("an id naming no artifact reports false without writing a version", func(t *testing.T) {
		d := newAPITestDAL(t)
		stored := dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))

		tx, err := d.wdb.Begin()
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		replaced, err := replaceTaskArtifactOn(tx, dalTestArtifact("ta-ghost", "T-1"))
		if err != nil {
			t.Fatalf("replaceTaskArtifactOn(ta-ghost): %v", err)
		}
		if replaced {
			t.Fatalf("replaceTaskArtifactOn(ta-ghost): want false, got true")
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		dalWantArtifact(t, d, stored)
		dalWantArtifactHistory(t, d, "ta-ghost", nil)
	})
}

func TestTrimTaskArtifactHistory(t *testing.T) {
	t.Run("the newest three versions stay, the older ones go, and their blobs go with them", func(t *testing.T) {
		d := newAPITestDAL(t)
		for i := 1; i <= 5; i++ {
			dalPutBlob(t, d, ChatAttachment{ID: dalAttID(i), Mime: "image/png", Data: []byte("bytes")})
		}
		for i := 1; i <= 5; i++ {
			dalSeedArtifactHistory(t, d, "ta-1", dalAttID(i), "draft "+dalAttID(i))
		}
		dalSeedArtifactHistory(t, d, "ta-2", "att-other", "another artifact's version")
		dalPutBlob(t, d, ChatAttachment{ID: "att-other", Mime: "image/png", Data: []byte("bytes")})

		dalInTx(t, d, func(tx *sql.Tx) error { return trimTaskArtifactHistory(tx, "ta-1") })

		got, err := d.ListTaskArtifactHistory("ta-1")
		if err != nil {
			t.Fatalf("ListTaskArtifactHistory: %v", err)
		}
		var names []string
		for _, h := range got {
			names = append(names, h.Name)
		}
		if want := []string{"draft att-5", "draft att-4", "draft att-3"}; !reflect.DeepEqual(names, want) {
			t.Fatalf("retained versions after the trim: want %v, got %v", want, names)
		}
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{"att-3", "att-4", "att-5", "att-other"}) {
			t.Fatalf("blobs after the trim: want att-1 and att-2 collected, got %v", got)
		}
		if n := dalArtifactHistoryCount(t, d, "ta-2"); n != 1 {
			t.Fatalf("another artifact's versions after the trim: want 1, got %d", n)
		}
	})

	t.Run("a dropped version's blob that the live row still points at survives", func(t *testing.T) {
		d := newAPITestDAL(t)
		dalPutBlob(t, d, ChatAttachment{ID: "att-shared", Mime: "image/png", Data: []byte("bytes")})
		live := dalTestArtifact("ta-1", "T-1")
		live.AttachmentID = "att-shared"
		dalPutArtifact(t, d, live)
		dalSeedArtifactHistory(t, d, "ta-1", "att-shared", "the oldest version")
		for i := 2; i <= 5; i++ {
			dalSeedArtifactHistory(t, d, "ta-1", "", "version without a blob")
		}

		dalInTx(t, d, func(tx *sql.Tx) error { return trimTaskArtifactHistory(tx, "ta-1") })

		if n := dalArtifactHistoryCount(t, d, "ta-1"); n != 3 {
			t.Fatalf("retained versions after the trim: want 3, got %d", n)
		}
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{"att-shared"}) {
			t.Fatalf("blobs after the trim: want [att-shared] kept, got %v", got)
		}
	})

	t.Run("three or fewer versions are left exactly as they stand", func(t *testing.T) {
		d := newAPITestDAL(t)
		for i := 1; i <= 3; i++ {
			dalPutBlob(t, d, ChatAttachment{ID: dalAttID(i), Mime: "image/png", Data: []byte("bytes")})
			dalSeedArtifactHistory(t, d, "ta-1", dalAttID(i), "draft "+dalAttID(i))
		}
		dalInTx(t, d, func(tx *sql.Tx) error { return trimTaskArtifactHistory(tx, "ta-1") })
		if n := dalArtifactHistoryCount(t, d, "ta-1"); n != 3 {
			t.Fatalf("retained versions after a no-op trim: want 3, got %d", n)
		}
		if got := dalBlobIDs(t, d); !reflect.DeepEqual(got, []string{"att-1", "att-2", "att-3"}) {
			t.Fatalf("blobs after a no-op trim: want all three, got %v", got)
		}
	})
}

func TestTaskArtifactHistoryBlobs(t *testing.T) {
	d := newAPITestDAL(t)
	dalSeedArtifactHistory(t, d, "ta-1", "att-1", "first")
	dalSeedArtifactHistory(t, d, "ta-1", "att-2", "second")
	dalSeedArtifactHistory(t, d, "ta-1", "att-1", "first again")
	dalSeedArtifactHistory(t, d, "ta-2", "att-9", "another artifact")

	t.Run("the matching rows' blob ids come back as a set, deduplicated", func(t *testing.T) {
		var got map[string]bool
		dalInTx(t, d, func(tx *sql.Tx) error {
			var err error
			got, err = taskArtifactHistoryBlobs(tx,
				`SELECT attachment_id FROM task_artifact_history WHERE artifact_id = ?`, "ta-1")
			return err
		})
		want := map[string]bool{"att-1": true, "att-2": true}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("taskArtifactHistoryBlobs(ta-1): want %v, got %v", want, got)
		}
	})

	t.Run("a query matching nothing is an empty set, not nil", func(t *testing.T) {
		var got map[string]bool
		dalInTx(t, d, func(tx *sql.Tx) error {
			var err error
			got, err = taskArtifactHistoryBlobs(tx,
				`SELECT attachment_id FROM task_artifact_history WHERE artifact_id = ?`, "ta-none")
			return err
		})
		if got == nil || len(got) != 0 {
			t.Fatalf("taskArtifactHistoryBlobs(ta-none): want an empty set, got %#v", got)
		}
	})

	t.Run("a version that names no blob contributes nothing", func(t *testing.T) {
		dalSeedArtifactHistory(t, d, "ta-3", "", "a version with no blob")
		dalSeedArtifactHistory(t, d, "ta-3", "att-7", "a version with one")
		var got map[string]bool
		dalInTx(t, d, func(tx *sql.Tx) error {
			var err error
			got, err = taskArtifactHistoryBlobs(tx,
				`SELECT attachment_id FROM task_artifact_history WHERE artifact_id = ?`, "ta-3")
			return err
		})
		want := map[string]bool{"att-7": true}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("taskArtifactHistoryBlobs(ta-3): want %v, got %v", want, got)
		}
	})
}

func TestListTaskArtifactHistory(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("an artifact never replaced has no versions at all", func(t *testing.T) {
		dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
		got, err := d.ListTaskArtifactHistory("ta-1")
		if err != nil {
			t.Fatalf("ListTaskArtifactHistory: %v", err)
		}
		if got != nil {
			t.Fatalf("ListTaskArtifactHistory on a never-replaced artifact: want nil, got %+v", got)
		}
	})

	t.Run("the versions come back newest first, and only this artifact's", func(t *testing.T) {
		first := dalTestArtifact("ta-1", "T-1")
		second := first
		second.Name = "the second draft"
		second.AttachmentID = "att-2"
		third := first
		third.Name = "the third draft"
		third.AttachmentID = "att-3"
		dalReplaceArtifact(t, d, second, true)
		dalReplaceArtifact(t, d, third, true)
		dalPutArtifact(t, d, dalTestArtifact("ta-2", "T-1"))
		other := dalTestArtifact("ta-2", "T-1")
		other.Name = "another card's second draft"
		dalReplaceArtifact(t, d, other, true)

		dalWantArtifactHistory(t, d, "ta-1", []TaskArtifactHistory{dalHistoryOf(second), dalHistoryOf(first)})
		dalWantArtifactHistory(t, d, "ta-2", []TaskArtifactHistory{dalHistoryOf(dalTestArtifact("ta-2", "T-1"))})
	})
}

func TestTaskArtifactHistoryCounts(t *testing.T) {
	d := newAPITestDAL(t)

	t.Run("a task whose artifacts were never replaced is an empty map, not nil", func(t *testing.T) {
		dalPutArtifact(t, d, dalTestArtifact("ta-1", "T-1"))
		got, err := d.TaskArtifactHistoryCounts("T-1")
		if err != nil {
			t.Fatalf("TaskArtifactHistoryCounts: %v", err)
		}
		if !reflect.DeepEqual(got, map[string]int{}) {
			t.Fatalf("TaskArtifactHistoryCounts before any replace: want an empty map, got %v", got)
		}
	})

	t.Run("each replaced artifact counts its own versions and the other task's are not folded in", func(t *testing.T) {
		next := dalTestArtifact("ta-1", "T-1")
		next.Name = "the second draft"
		dalReplaceArtifact(t, d, next, true)
		next.Name = "the third draft"
		dalReplaceArtifact(t, d, next, true)

		dalPutArtifact(t, d, dalTestArtifact("ta-2", "T-1"))
		second := dalTestArtifact("ta-2", "T-1")
		second.Name = "one replace only"
		dalReplaceArtifact(t, d, second, true)

		dalPutArtifact(t, d, dalTestArtifact("ta-3", "T-1"))

		dalPutArtifact(t, d, dalTestArtifact("ta-9", "T-2"))
		elsewhere := dalTestArtifact("ta-9", "T-2")
		elsewhere.Name = "another card"
		dalReplaceArtifact(t, d, elsewhere, true)

		got, err := d.TaskArtifactHistoryCounts("T-1")
		if err != nil {
			t.Fatalf("TaskArtifactHistoryCounts: %v", err)
		}
		want := map[string]int{"ta-1": 2, "ta-2": 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("TaskArtifactHistoryCounts(T-1): want %v, got %v", want, got)
		}
		if got, err = d.TaskArtifactHistoryCounts("T-2"); err != nil ||
			!reflect.DeepEqual(got, map[string]int{"ta-9": 1}) {
			t.Fatalf("TaskArtifactHistoryCounts(T-2): want map[ta-9:1], got %v (%v)", got, err)
		}
	})
}

// dalTestArtifact is one pinned deliverable with every column carrying a
// distinct non-zero value, so a write that touches a column it should not
// shows up.

func dalTestArtifact(id, taskID string) TaskArtifact {
	return TaskArtifact{
		ID:           id,
		TaskID:       taskID,
		Kind:         ArtifactKindFile,
		AttachmentID: "att-1",
		Name:         "the first draft",
		Description:  "what the deliverable is",
		CreatedTS:    200,
		CreatedBy:    "ann",
	}
}

// dalHistoryOf is the retained version a replace writes for an artifact that
// stood in this state — every field but the minted id, which dalWantArtifactHistory
// checks for plausibility instead.

func dalHistoryOf(a TaskArtifact) TaskArtifactHistory {
	return TaskArtifactHistory{
		ArtifactID:   a.ID,
		Kind:         a.Kind,
		AttachmentID: a.AttachmentID,
		Name:         a.Name,
		Description:  a.Description,
		CreatedTS:    a.CreatedTS,
		CreatedBy:    a.CreatedBy,
	}
}

func dalAttID(n int) string { return "att-" + string(rune('0'+n)) }

func dalPutArtifact(t *testing.T, d *DAL, a TaskArtifact) TaskArtifact {
	t.Helper()
	if err := d.PutTaskArtifact(a); err != nil {
		t.Fatalf("PutTaskArtifact(%q): %v", a.ID, err)
	}
	return a
}

func dalReplaceArtifact(t *testing.T, d *DAL, next TaskArtifact, wantReplaced bool) {
	t.Helper()
	replaced, err := d.ReplaceTaskArtifact(next)
	if err != nil {
		t.Fatalf("ReplaceTaskArtifact(%q): %v", next.ID, err)
	}
	if replaced != wantReplaced {
		t.Fatalf("ReplaceTaskArtifact(%q): want %v, got %v", next.ID, wantReplaced, replaced)
	}
}

func dalWantArtifact(t *testing.T, d *DAL, want TaskArtifact) {
	t.Helper()
	got, err := d.GetTaskArtifact(want.ID)
	if err != nil {
		t.Fatalf("GetTaskArtifact(%q): %v", want.ID, err)
	}
	if got == nil {
		t.Fatalf("GetTaskArtifact(%q): no row", want.ID)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("GetTaskArtifact(%q):\n got %+v\nwant %+v", want.ID, *got, want)
	}
}

// dalWantArtifactHistory asserts one artifact's retained versions entry by
// entry, with each row's minted id checked for order rather than written down.

func dalWantArtifactHistory(t *testing.T, d *DAL, artifactID string, want []TaskArtifactHistory) {
	t.Helper()
	got, err := d.ListTaskArtifactHistory(artifactID)
	if err != nil {
		t.Fatalf("ListTaskArtifactHistory(%q): %v", artifactID, err)
	}
	if len(got) != len(want) {
		t.Fatalf("ListTaskArtifactHistory(%q): want %d versions, got %+v", artifactID, len(want), got)
	}
	for i := range got {
		if got[i].ID <= 0 {
			t.Fatalf("ListTaskArtifactHistory(%q)[%d]: want a minted id, got %d", artifactID, i, got[i].ID)
		}
		if i > 0 && got[i].ID >= got[i-1].ID {
			t.Fatalf("ListTaskArtifactHistory(%q): want newest first, got ids %d then %d",
				artifactID, got[i-1].ID, got[i].ID)
		}
		entry := got[i]
		entry.ID = 0
		if !reflect.DeepEqual(entry, want[i]) {
			t.Fatalf("ListTaskArtifactHistory(%q)[%d]:\n got %+v\nwant %+v", artifactID, i, entry, want[i])
		}
	}
}

func dalArtifactIDs(t *testing.T, d *DAL) []string {
	t.Helper()
	return dalScanStrings(t, d, `SELECT id FROM task_artifact ORDER BY id`)
}

func dalBlobIDs(t *testing.T, d *DAL) []string {
	t.Helper()
	return dalScanStrings(t, d, `SELECT id FROM chat_attachment ORDER BY id`)
}

func dalScanStrings(t *testing.T, d *DAL, query string) []string {
	t.Helper()
	rows, err := d.rdb.Query(query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, s)
	}
	return out
}

func dalArtifactHistoryCount(t *testing.T, d *DAL, artifactID string) int {
	t.Helper()
	var n int
	if err := d.rdb.QueryRow(
		`SELECT COUNT(*) FROM task_artifact_history WHERE artifact_id = ?`, artifactID).Scan(&n); err != nil {
		t.Fatalf("count versions of %q: %v", artifactID, err)
	}
	return n
}

// dalSeedArtifactHistory writes one retained version directly, so a test can
// stage more of them than a replace path would leave behind.

func dalSeedArtifactHistory(t *testing.T, d *DAL, artifactID, attachmentID, name string) {
	t.Helper()
	if _, err := d.wdb.Exec(`INSERT INTO task_artifact_history
		(artifact_id, kind, attachment_id, name, description, created_ts, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		artifactID, ArtifactKindFile, attachmentID, name, "", 100.0, "ann"); err != nil {
		t.Fatalf("seed a retained version of %q: %v", artifactID, err)
	}
}

func dalPutBlob(t *testing.T, d *DAL, a ChatAttachment) {
	t.Helper()
	if err := d.PutChatAttachment(a); err != nil {
		t.Fatalf("PutChatAttachment(%q): %v", a.ID, err)
	}
}

func dalWantBlob(t *testing.T, d *DAL, want ChatAttachment) {
	t.Helper()
	got, err := d.GetChatAttachment(want.ID)
	if err != nil {
		t.Fatalf("GetChatAttachment(%q): %v", want.ID, err)
	}
	if got == nil {
		t.Fatalf("GetChatAttachment(%q): no blob", want.ID)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("GetChatAttachment(%q):\n got %+v\nwant %+v", want.ID, *got, want)
	}
}

func dalInTx(t *testing.T, d *DAL, fn func(tx *sql.Tx) error) {
	t.Helper()
	if err := d.inTx(fn); err != nil {
		t.Fatalf("inTx: %v", err)
	}
}
