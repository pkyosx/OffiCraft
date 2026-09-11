package main

// wire_test.go — the behaviour of server/ocserverd/wire.go's projections: what
// each stored row becomes on the wire, which fields go honest-empty rather than
// fabricated, and which nullable fields serialise as null.

import (
	"reflect"
	"strings"
	"testing"
)

func wireTestFilename(s string) *string { return &s }

func TestChatReplyQuoteContent(t *testing.T) {
	t.Run("runs of whitespace, newlines included, collapse to single spaces", func(t *testing.T) {
		got := chatReplyQuoteContent("  first line\n\n  second\tthird   ")
		want := "first line second third"
		if got != want {
			t.Fatalf("chatReplyQuoteContent(multi-line) = %q, want %q", got, want)
		}
	})

	t.Run("a body at the rune limit is carried whole and one rune past it is cut with an ellipsis", func(t *testing.T) {
		exact := strings.Repeat("a", chatReplyQuoteMaxChars)
		if got := chatReplyQuoteContent(exact); got != exact {
			t.Fatalf("chatReplyQuoteContent(exactly %d runes) = %q, want it whole",
				chatReplyQuoteMaxChars, got)
		}
		over := strings.Repeat("a", chatReplyQuoteMaxChars+1)
		want := exact + "…"
		if got := chatReplyQuoteContent(over); got != want {
			t.Fatalf("chatReplyQuoteContent(one rune over):\n got %q\nwant %q", got, want)
		}
	})

	t.Run("the cut is by runes, so a CJK body is never sliced mid-codepoint", func(t *testing.T) {
		body := strings.Repeat("字", 100)
		got := chatReplyQuoteContent(body)
		want := strings.Repeat("字", chatReplyQuoteMaxChars) + "…"
		if got != want {
			t.Fatalf("chatReplyQuoteContent(CJK) = %q, want %q", got, want)
		}
		if len([]rune(got)) != chatReplyQuoteMaxChars+1 {
			t.Fatalf("chatReplyQuoteContent(CJK) kept %d runes, want %d",
				len([]rune(got)), chatReplyQuoteMaxChars+1)
		}
	})

	t.Run("an attachment-only message quotes as the empty string", func(t *testing.T) {
		for _, body := range []string{"", "   ", "\n\t "} {
			if got := chatReplyQuoteContent(body); got != "" {
				t.Fatalf("chatReplyQuoteContent(%q) = %q, want the empty string", body, got)
			}
		}
	})
}

func TestNewChatReplyQuoteDTO(t *testing.T) {
	quoted := ChatMessage{
		ID:        "c-1",
		Sender:    "ann",
		Recipient: "bob",
		Body:      "the\nquoted  body",
	}

	t.Run("without a name map both addresses are carried and both names stay empty", func(t *testing.T) {
		got := newChatReplyQuoteDTO(quoted, nil)
		want := &chatReplyQuoteDTO{
			ID: "c-1", From: "ann", FromName: "", To: "bob", ToName: "",
			Content: "the quoted body",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newChatReplyQuoteDTO(no names):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("with a name map each side resolves, and an id absent from it stays empty", func(t *testing.T) {
		got := newChatReplyQuoteDTO(quoted, map[string]string{"ann": "Ann"})
		want := &chatReplyQuoteDTO{
			ID: "c-1", From: "ann", FromName: "Ann", To: "bob", ToName: "",
			Content: "the quoted body",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newChatReplyQuoteDTO(partial names):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("the owner has no roster row and resolves through its own special case", func(t *testing.T) {
		fromOwner := ChatMessage{ID: "c-2", Sender: wireOwnerID, Recipient: "ann", Body: "go"}
		got := newChatReplyQuoteDTO(fromOwner, map[string]string{"ann": "Ann"})
		want := &chatReplyQuoteDTO{
			ID: "c-2", From: wireOwnerID, FromName: resumeOwnerDisplayName,
			To: "ann", ToName: "Ann", Content: "go",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newChatReplyQuoteDTO(owner sender):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("the quoted body is shortened by the same rule the quote line uses", func(t *testing.T) {
		long := ChatMessage{ID: "c-3", Sender: "ann", Recipient: "bob",
			Body: strings.Repeat("b", chatReplyQuoteMaxChars+5)}
		got := newChatReplyQuoteDTO(long, nil)
		want := strings.Repeat("b", chatReplyQuoteMaxChars) + "…"
		if got.Content != want {
			t.Fatalf("newChatReplyQuoteDTO(long body).Content = %q, want %q", got.Content, want)
		}
	})
}

func TestNewRoleDefListItemDTO(t *testing.T) {
	t.Run("every listing field is carried across and the persona body is the only thing left behind", func(t *testing.T) {
		folded := roleDefDTO{
			SizeChars: 412, CapChars: 1000, Key: "engineer", Name: "Engineer",
			DefinitionMD: "# the whole persona", OwnerID: wireOwnerID,
			SchemaVersion: wireSchemaVersion, IsDefault: true, IsSeed: true,
		}
		got := newRoleDefListItemDTO(folded)
		want := roleDefListItemDTO{
			SizeChars: 412, CapChars: 1000, Key: "engineer", Name: "Engineer",
			OwnerID: wireOwnerID, SchemaVersion: wireSchemaVersion,
			IsDefault: true, IsSeed: true,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newRoleDefListItemDTO(seed role):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a custom role's own flags ride across unchanged", func(t *testing.T) {
		folded := roleDefDTO{
			SizeChars: 0, CapChars: 1000, Key: "r-abc", Name: "Bookkeeper",
			OwnerID: wireOwnerID, SchemaVersion: wireSchemaVersion,
			IsDefault: false, IsSeed: false,
		}
		got := newRoleDefListItemDTO(folded)
		want := roleDefListItemDTO{
			SizeChars: 0, CapChars: 1000, Key: "r-abc", Name: "Bookkeeper",
			OwnerID: wireOwnerID, SchemaVersion: wireSchemaVersion,
			IsDefault: false, IsSeed: false,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newRoleDefListItemDTO(custom role):\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestReceiptSha256(t *testing.T) {
	for text, want := range map[string]string{
		"":         "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"hello":    "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		"# 角色定義\n": "6a95e829eb02cea2e314325fabb2b08349d4f24b5c1e1f669369671181be7624",
	} {
		if got := receiptSha256(text); got != want {
			t.Fatalf("receiptSha256(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestNewTaskArtifactVersionDTO(t *testing.T) {
	base := TaskArtifactHistory{
		ID: 7, ArtifactID: "ta-1", AttachmentID: "att-1",
		Name: "Report", Description: "the write-up",
		CreatedTS: 1700.5, CreatedBy: "ann",
	}

	t.Run("a file version reads its url, mime, filename and image flag off the blob", func(t *testing.T) {
		h := base
		h.Kind = ArtifactKindFile
		att := &ChatAttachment{ID: "att-1", Mime: "application/octet-stream",
			Filename: wireTestFilename("report.md")}
		got := newTaskArtifactVersionDTO(h, att)
		want := taskArtifactVersionDTO{
			ID: 7, Kind: "file", URL: "/api/chat/attachment/att-1", Name: "Report",
			Description: "the write-up", Filename: "report.md",
			Mime: "application/octet-stream", IsImage: false,
			AttachmentID: "att-1", CreatedTS: 1700.5, CreatedBy: "ann",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskArtifactVersionDTO(file):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("an image blob sets the image flag", func(t *testing.T) {
		h := base
		h.Kind = ArtifactKindImage
		att := &ChatAttachment{ID: "att-1", Mime: "image/png", Filename: wireTestFilename("shot.png")}
		got := newTaskArtifactVersionDTO(h, att)
		if !got.IsImage || got.Mime != "image/png" || got.URL != "/api/chat/attachment/att-1" {
			t.Fatalf("newTaskArtifactVersionDTO(image) = %+v, want the image blob facts", got)
		}
	})

	t.Run("a link version takes its url out of the blob's bytes and carries no blob facts", func(t *testing.T) {
		h := base
		h.Kind = ArtifactKindLink
		att := &ChatAttachment{ID: "att-1", Mime: "text/uri-list",
			Data: []byte("https://example.test/spec\r\n"), Filename: wireTestFilename("link.uri")}
		got := newTaskArtifactVersionDTO(h, att)
		want := taskArtifactVersionDTO{
			ID: 7, Kind: "link", URL: "https://example.test/spec", Name: "Report",
			Description: "the write-up", Filename: "", Mime: "", IsImage: false,
			AttachmentID: "att-1", CreatedTS: 1700.5, CreatedBy: "ann",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskArtifactVersionDTO(link):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a gone blob leaves every blob-derived field honest-empty while the row's own fields stand", func(t *testing.T) {
		h := base
		h.Kind = ArtifactKindFile
		got := newTaskArtifactVersionDTO(h, nil)
		want := taskArtifactVersionDTO{
			ID: 7, Kind: "file", URL: "", Name: "Report", Description: "the write-up",
			Filename: "", Mime: "", IsImage: false, AttachmentID: "att-1",
			CreatedTS: 1700.5, CreatedBy: "ann",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskArtifactVersionDTO(blob gone):\n got %+v\nwant %+v", got, want)
		}
		h.Kind = ArtifactKindLink
		if got := newTaskArtifactVersionDTO(h, nil); got.URL != "" {
			t.Fatalf("newTaskArtifactVersionDTO(link, blob gone).URL = %q, want the empty string", got.URL)
		}
	})

	t.Run("a version with no stored name keeps the empty name — this row derives none", func(t *testing.T) {
		h := base
		h.Kind = ArtifactKindFile
		h.Name = ""
		att := &ChatAttachment{ID: "att-1", Mime: "text/markdown", Filename: wireTestFilename("report.md")}
		if got := newTaskArtifactVersionDTO(h, att); got.Name != "" {
			t.Fatalf("newTaskArtifactVersionDTO(no stored name).Name = %q, want the empty string", got.Name)
		}
	})
}

func TestNewTaskStepDTO(t *testing.T) {
	step := TaskStep{
		ID: "s-1", TaskID: "t-1", OrderIdx: 2, Name: "build", DoD: "it compiles",
		Status: StepStatusInProgress, ParallelGroup: "g1", IsGate: true,
		ReplyCardID: "rc-1", WaitingReason: "vendor", Note: "台北 note",
		StartedTS: 100.5, FinishedTS: 0,
	}

	t.Run("every row field rides across and the note is reported as a rune count, never carried", func(t *testing.T) {
		got := newTaskStepDTO(step, map[string]string{"rc-1": "waiting"}, 10000)
		want := taskStepDTO{
			ID: "s-1", TaskID: "t-1", OrderIdx: 2, Name: "build", DoD: "it compiles",
			Status: "in_progress", ParallelGroup: "g1", IsGate: true,
			ReplyCardID: "rc-1", ReplyCardStatus: "waiting", WaitingReason: "vendor",
			NoteSizeChars: 7, NoteCapChars: 10000, StartedTS: 100.5, FinishedTS: 0,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskStepDTO:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a step with no card, or a card id absent from the map, serialises an empty card status", func(t *testing.T) {
		noCard := step
		noCard.ReplyCardID = ""
		if got := newTaskStepDTO(noCard, map[string]string{"rc-1": "waiting"}, 10); got.ReplyCardStatus != "" {
			t.Fatalf("newTaskStepDTO(no card).ReplyCardStatus = %q, want the empty string", got.ReplyCardStatus)
		}
		if got := newTaskStepDTO(step, nil, 10); got.ReplyCardStatus != "" {
			t.Fatalf("newTaskStepDTO(nil card map).ReplyCardStatus = %q, want the empty string", got.ReplyCardStatus)
		}
		if got := newTaskStepDTO(step, map[string]string{"rc-1": "answered"}, 10); got.ReplyCardStatus != "answered" {
			t.Fatalf("newTaskStepDTO(answered card).ReplyCardStatus = %q, want %q",
				got.ReplyCardStatus, "answered")
		}
	})

	t.Run("an empty note measures zero and the reported cap is whatever the caller handed in", func(t *testing.T) {
		bare := TaskStep{ID: "s-2", TaskID: "t-1"}
		got := newTaskStepDTO(bare, nil, 4000)
		want := taskStepDTO{ID: "s-2", TaskID: "t-1", NoteSizeChars: 0, NoteCapChars: 4000}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskStepDTO(bare step):\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestNewTaskStepDetailDTO(t *testing.T) {
	step := TaskStep{
		ID: "s-1", TaskID: "t-1", OrderIdx: 2, Name: "build", DoD: "it compiles",
		Status: StepStatusInProgress, ParallelGroup: "g1", IsGate: true,
		ReplyCardID: "rc-1", WaitingReason: "vendor", Note: "台北 note",
		StartedTS: 100.5, FinishedTS: 200.5,
	}

	t.Run("the single-step wire declares itself full and carries the note text beside its size", func(t *testing.T) {
		got := newTaskStepDetailDTO(step, map[string]string{"rc-1": "answered"}, 10000)
		want := taskStepDetailDTO{
			DetailLevel: "full", ID: "s-1", TaskID: "t-1", OrderIdx: 2, Name: "build",
			DoD: "it compiles", Status: "in_progress", ParallelGroup: "g1", IsGate: true,
			ReplyCardID: "rc-1", ReplyCardStatus: "answered", WaitingReason: "vendor",
			Note: "台北 note", NoteSizeChars: 7, NoteCapChars: 10000,
			StartedTS: 100.5, FinishedTS: 200.5,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskStepDetailDTO:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("the two step faces agree on every field they share", func(t *testing.T) {
		cards := map[string]string{"rc-1": "waiting"}
		summary := newTaskStepDTO(step, cards, 555)
		detail := newTaskStepDetailDTO(step, cards, 555)
		if summary.ReplyCardStatus != detail.ReplyCardStatus ||
			summary.NoteSizeChars != detail.NoteSizeChars ||
			summary.NoteCapChars != detail.NoteCapChars ||
			summary.Status != detail.Status || summary.OrderIdx != detail.OrderIdx {
			t.Fatalf("the two step faces disagree:\n summary %+v\n detail  %+v", summary, detail)
		}
	})

	t.Run("an empty note is served as the empty string with a zero size", func(t *testing.T) {
		bare := TaskStep{ID: "s-2", TaskID: "t-1"}
		got := newTaskStepDetailDTO(bare, nil, 4000)
		want := taskStepDetailDTO{DetailLevel: "full", ID: "s-2", TaskID: "t-1", NoteCapChars: 4000}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskStepDetailDTO(bare step):\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestNewTaskDTO(t *testing.T) {
	task := Task{
		ID: "t-abc", TypeKey: "bugfix", Title: "Fix the thing",
		DedupeKey: "k1", Inputs: map[string]any{"PR Link": "https://example.test/1"},
		Description: "a description", Status: TaskStatusInProgress, Lock: TaskLockReassigning,
		Priority: TaskPriorityHigh, ExecutorKind: TaskExecutorStaff, ExecutorID: "ann",
		CreatorID: "owner", ReassignedFrom: "bob", ReassignedFromKind: "staff",
		HandoverNote: "picked up mid-flight", HandoverNoteTS: 90, HandoverNoteBy: "bob",
		WaitingReason: "vendor", CreatedTS: 10, UpdatedTS: 20, ClosedTS: 0,
		CloseoutTS: 0, DuplicateOf: "", Handoff: HandoffFollowUp,
		HandoffNote: "see t-next", HandoffTaskID: "t-next", FrozenBy: "",
	}
	steps := []TaskStep{
		{ID: "s-1", TaskID: "t-abc", OrderIdx: 0, Name: "read", Status: StepStatusDone},
		{ID: "s-2", TaskID: "t-abc", OrderIdx: 1, Name: "build", Status: StepStatusInProgress,
			ReplyCardID: "rc-1", Note: "abc"},
		{ID: "s-3", TaskID: "t-abc", OrderIdx: 2, Name: "old", Status: StepStatusSuperseded},
	}

	t.Run("an open task projects whole: task_no is the id, progress derives from the steps, closed_ts is null", func(t *testing.T) {
		got := newTaskDTO(task, steps, []string{"t-dep"}, map[string]string{"rc-1": "waiting"}, 10000)
		want := taskDTO{
			ID: "t-abc", TaskNo: "t-abc", TypeKey: "bugfix", Title: "Fix the thing",
			DedupeKey: "k1", Inputs: map[string]any{"PR Link": "https://example.test/1"},
			Description: "a description", DuplicateOf: "", Status: "in_progress",
			Lock: "reassigning", Priority: "high", ExecutorKind: "staff", ExecutorID: "ann",
			CreatorID: "owner", ReassignedFrom: "bob", ReassignedFromKind: "staff",
			HandoverNote: "picked up mid-flight", HandoverNoteTS: 90, HandoverNoteBy: "bob",
			WaitingReason: "vendor", CreatedTS: 10, UpdatedTS: 20, ClosedTS: nil,
			Deps: []string{"t-dep"},
			Steps: []taskStepDTO{
				{ID: "s-1", TaskID: "t-abc", OrderIdx: 0, Name: "read", Status: "done", NoteCapChars: 10000},
				{ID: "s-2", TaskID: "t-abc", OrderIdx: 1, Name: "build", Status: "in_progress",
					ReplyCardID: "rc-1", ReplyCardStatus: "waiting", NoteSizeChars: 3, NoteCapChars: 10000},
				{ID: "s-3", TaskID: "t-abc", OrderIdx: 2, Name: "old", Status: "superseded", NoteCapChars: 10000},
			},
			DetailLevel: "summary", NotesIncluded: false,
			ProgressDone: 1, ProgressTotal: 2, CloseoutReported: false,
			ArtifactCount: 0, Handoff: "follow_up", HandoffNote: "see t-next",
			HandoffTaskID: "t-next", Blocking: []taskDepRefDTO{}, FrozenBy: "",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskDTO(open task):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a closed task serialises closed_ts as the stamp itself and reports the close-out flag", func(t *testing.T) {
		closed := task
		closed.ClosedTS = 555.25
		closed.CloseoutTS = 600
		got := newTaskDTO(closed, nil, nil, nil, 10)
		if got.ClosedTS == nil || *got.ClosedTS != 555.25 {
			t.Fatalf("newTaskDTO(closed).ClosedTS = %v, want a pointer to 555.25", got.ClosedTS)
		}
		if !got.CloseoutReported {
			t.Fatal("newTaskDTO(closeout stamped).CloseoutReported = false, want true")
		}
	})

	t.Run("nil deps, nil inputs and no steps serialise as empty containers, never as JSON null", func(t *testing.T) {
		bare := Task{ID: "t-1"}
		got := newTaskDTO(bare, nil, nil, nil, 10)
		if got.Deps == nil || len(got.Deps) != 0 {
			t.Fatalf("newTaskDTO(nil deps).Deps = %#v, want an empty non-nil slice", got.Deps)
		}
		if got.Inputs == nil || len(got.Inputs) != 0 {
			t.Fatalf("newTaskDTO(nil inputs).Inputs = %#v, want an empty non-nil map", got.Inputs)
		}
		if got.Steps == nil || len(got.Steps) != 0 {
			t.Fatalf("newTaskDTO(no steps).Steps = %#v, want an empty non-nil slice", got.Steps)
		}
		if got.Blocking == nil || len(got.Blocking) != 0 {
			t.Fatalf("newTaskDTO(pure builder).Blocking = %#v, want an empty non-nil slice", got.Blocking)
		}
		if got.ProgressDone != 0 || got.ProgressTotal != 0 {
			t.Fatalf("newTaskDTO(no steps) progress = (%d, %d), want (0, 0)",
				got.ProgressDone, got.ProgressTotal)
		}
	})
}

func TestNewTaskArtifactDTO(t *testing.T) {
	row := TaskArtifact{
		ID: "ta-abc", TaskID: "t-1", AttachmentID: "att-1",
		Name: "Q3 report", Description: "the write-up", CreatedTS: 1700, CreatedBy: "ann",
	}

	t.Run("a file artifact folds the blob's url, mime and filename, and counts the live version too", func(t *testing.T) {
		a := row
		a.Kind = ArtifactKindFile
		att := &ChatAttachment{ID: "att-1", Mime: "text/markdown", Filename: wireTestFilename("q3.md")}
		got := newTaskArtifactDTO(a, att, 2)
		want := taskArtifactDTO{
			ID: "ta-abc", Kind: "file", AttachmentID: "att-1", Name: "Q3 report",
			Filename: "q3.md", Description: "the write-up",
			URL: "/api/chat/attachment/att-1", Mime: "text/markdown",
			CreatedTS: 1700, CreatedBy: "ann", VersionCount: 3,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskArtifactDTO(file):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a never-replaced artifact reads one version rather than zero", func(t *testing.T) {
		a := row
		a.Kind = ArtifactKindImage
		att := &ChatAttachment{ID: "att-1", Mime: "image/png", Filename: wireTestFilename("shot.png")}
		if got := newTaskArtifactDTO(a, att, 0); got.VersionCount != 1 {
			t.Fatalf("newTaskArtifactDTO(never replaced).VersionCount = %d, want 1", got.VersionCount)
		}
	})

	t.Run("a link takes its url from the blob's bytes, its mime from the blob, and no filename", func(t *testing.T) {
		a := row
		a.Kind = ArtifactKindLink
		att := &ChatAttachment{ID: "att-1", Mime: "text/uri-list",
			Data: []byte("https://example.test/doc\n"), Filename: wireTestFilename("l.uri")}
		got := newTaskArtifactDTO(a, att, 0)
		want := taskArtifactDTO{
			ID: "ta-abc", Kind: "link", AttachmentID: "att-1", Name: "Q3 report",
			Filename: "", Description: "the write-up", URL: "https://example.test/doc",
			Mime: "text/uri-list", CreatedTS: 1700, CreatedBy: "ann", VersionCount: 1,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskArtifactDTO(link):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a gone blob keeps the attachment id but leaves the resolved fields empty and derives a name from the id", func(t *testing.T) {
		a := row
		a.Kind = ArtifactKindFile
		a.Name = ""
		got := newTaskArtifactDTO(a, nil, 0)
		want := taskArtifactDTO{
			ID: "ta-abc", Kind: "file", AttachmentID: "att-1", Name: "#abc",
			Filename: "", Description: "the write-up", URL: "", Mime: "",
			CreatedTS: 1700, CreatedBy: "ann", VersionCount: 1,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskArtifactDTO(blob gone):\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestLinkTargetOf(t *testing.T) {
	t.Run("the blob's bytes are the target, with trailing newlines stripped", func(t *testing.T) {
		for _, data := range []string{
			"https://example.test/a",
			"https://example.test/a\n",
			"https://example.test/a\r\n",
			"https://example.test/a\n\n\r\n",
		} {
			got := linkTargetOf(&ChatAttachment{ID: "att-1", Data: []byte(data)})
			if got != "https://example.test/a" {
				t.Fatalf("linkTargetOf(%q) = %q, want %q", data, got, "https://example.test/a")
			}
		}
	})

	t.Run("a gone blob is honest-empty rather than the row's id dressed up as a url", func(t *testing.T) {
		if got := linkTargetOf(nil); got != "" {
			t.Fatalf("linkTargetOf(nil) = %q, want the empty string", got)
		}
		if got := linkTargetOf(&ChatAttachment{ID: "att-1"}); got != "" {
			t.Fatalf("linkTargetOf(empty blob) = %q, want the empty string", got)
		}
	})

	t.Run("only trailing newlines are trimmed — leading and inner text stands", func(t *testing.T) {
		got := linkTargetOf(&ChatAttachment{Data: []byte("  https://example.test/a b\n")})
		if got != "  https://example.test/a b" {
			t.Fatalf("linkTargetOf(padded) = %q, want the leading space kept", got)
		}
	})
}

func TestArtifactDisplayName(t *testing.T) {
	t.Run("a stored name wins over every fallback", func(t *testing.T) {
		a := TaskArtifact{ID: "ta-abc", Kind: ArtifactKindFile, Name: "Q3 report"}
		att := &ChatAttachment{Filename: wireTestFilename("q3.md")}
		if got := artifactDisplayName(a, att); got != "Q3 report" {
			t.Fatalf("artifactDisplayName(stored name) = %q, want %q", got, "Q3 report")
		}
	})

	t.Run("a nameless file or image falls back to the blob's own filename", func(t *testing.T) {
		att := &ChatAttachment{Filename: wireTestFilename("q3.md")}
		for _, kind := range []string{ArtifactKindFile, ArtifactKindImage} {
			a := TaskArtifact{ID: "ta-abc", Kind: kind}
			if got := artifactDisplayName(a, att); got != "q3.md" {
				t.Fatalf("artifactDisplayName(nameless %s) = %q, want %q", kind, got, "q3.md")
			}
		}
	})

	t.Run("a nameless link falls back to its target", func(t *testing.T) {
		a := TaskArtifact{ID: "ta-abc", Kind: ArtifactKindLink}
		att := &ChatAttachment{Data: []byte("https://example.test/doc\n"),
			Filename: wireTestFilename("ignored.uri")}
		if got := artifactDisplayName(a, att); got != "https://example.test/doc" {
			t.Fatalf("artifactDisplayName(nameless link) = %q, want the target", got)
		}
	})

	t.Run("with nothing else to say it answers the id without its ta- prefix", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			a    TaskArtifact
			att  *ChatAttachment
		}{
			{"file, blob gone", TaskArtifact{ID: "ta-abc", Kind: ArtifactKindFile}, nil},
			{"file, blob with no filename", TaskArtifact{ID: "ta-abc", Kind: ArtifactKindFile}, &ChatAttachment{}},
			{"file, blob with a blank filename", TaskArtifact{ID: "ta-abc", Kind: ArtifactKindFile},
				&ChatAttachment{Filename: wireTestFilename("")}},
			{"link, blob gone", TaskArtifact{ID: "ta-abc", Kind: ArtifactKindLink}, nil},
			{"link, empty target", TaskArtifact{ID: "ta-abc", Kind: ArtifactKindLink}, &ChatAttachment{}},
		} {
			if got := artifactDisplayName(tc.a, tc.att); got != "#abc" {
				t.Fatalf("artifactDisplayName(%s) = %q, want %q", tc.name, got, "#abc")
			}
		}
	})

	t.Run("an id without the ta- prefix is carried whole behind the hash", func(t *testing.T) {
		a := TaskArtifact{ID: "legacy-1", Kind: ArtifactKindFile}
		if got := artifactDisplayName(a, nil); got != "#legacy-1" {
			t.Fatalf("artifactDisplayName(unprefixed id) = %q, want %q", got, "#legacy-1")
		}
	})
}

func TestArtifactBlobFacts(t *testing.T) {
	t.Run("a resolved blob yields the serve path, the mime, its own name and the image flag", func(t *testing.T) {
		att := &ChatAttachment{ID: "att-1", Mime: "image/png", Filename: wireTestFilename("shot.png")}
		got, ok := artifactBlobFacts(att)
		want := artifactBlobFields{url: "/api/chat/attachment/att-1", mime: "image/png",
			filename: "shot.png", isImage: true}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("artifactBlobFacts(image) = (%+v, %v), want (%+v, true)", got, ok, want)
		}
	})

	t.Run("a non-image mime, a mime shorter than the image prefix and an empty mime all read not-image", func(t *testing.T) {
		for _, mime := range []string{"text/markdown", "image", "img", "", "IMAGE/PNG"} {
			got, ok := artifactBlobFacts(&ChatAttachment{ID: "att-1", Mime: mime})
			if !ok {
				t.Fatalf("artifactBlobFacts(mime %q) reported no blob", mime)
			}
			if got.isImage {
				t.Fatalf("artifactBlobFacts(mime %q).isImage = true, want false", mime)
			}
		}
	})

	t.Run("a blob with no filename column leaves the name empty rather than fabricating one", func(t *testing.T) {
		got, ok := artifactBlobFacts(&ChatAttachment{ID: "att-1", Mime: "text/plain"})
		want := artifactBlobFields{url: "/api/chat/attachment/att-1", mime: "text/plain"}
		if !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("artifactBlobFacts(no filename) = (%+v, %v), want (%+v, true)", got, ok, want)
		}
	})

	t.Run("ok is false exactly when the blob is gone, and the fields are then the zero value", func(t *testing.T) {
		got, ok := artifactBlobFacts(nil)
		if ok || !reflect.DeepEqual(got, artifactBlobFields{}) {
			t.Fatalf("artifactBlobFacts(nil) = (%+v, %v), want (zero, false)", got, ok)
		}
	})
}

func TestNewTaskListItemDTO(t *testing.T) {
	task := Task{
		ID: "t-abc", TypeKey: "bugfix", Title: "Fix the thing", DedupeKey: "k1",
		DuplicateOf: "", Status: TaskStatusInProgress, Lock: TaskLockReassigning,
		Priority: TaskPriorityMid, ExecutorKind: TaskExecutorOutsource, ExecutorID: "w-1",
		CreatorID: "owner", ReassignedFrom: "bob", ReassignedFromKind: "staff",
		WaitingReason: "vendor", CreatedTS: 10, UpdatedTS: 20, ClosedTS: 0,
		Description: "dropped by the light list",
		Inputs:      map[string]any{"dropped": true},
	}
	population := map[string]Task{
		"t-dep": {ID: "t-dep", Title: "The blocker", Status: TaskStatusDone},
	}

	t.Run("an open task projects the light row: deps resolve, closed_ts is null, the heavy fields are absent", func(t *testing.T) {
		got := newTaskListItemDTO(task, []string{"t-dep", "t-gone"}, 1, 3, 4, population,
			TaskCurrentStep{ID: "s-2", Name: "build"})
		want := taskListItemDTO{
			ID: "t-abc", TaskNo: "t-abc", TypeKey: "bugfix", Title: "Fix the thing",
			DedupeKey: "k1", DuplicateOf: "", Status: "in_progress", Lock: "reassigning",
			Priority: "mid", ExecutorKind: "outsource", ExecutorID: "w-1",
			CreatorID: "owner", ReassignedFrom: "bob", ReassignedFromKind: "staff",
			WaitingReason: "vendor", CreatedTS: 10, UpdatedTS: 20, ClosedTS: nil,
			Deps: []string{"t-dep", "t-gone"},
			DepTasks: []taskDepRefDTO{
				{ID: "t-dep", TaskNo: "t-dep", Title: "The blocker", Status: "done"},
				{ID: "t-gone", TaskNo: "t-gone", Title: "", Status: ""},
			},
			ProgressDone: 1, ProgressTotal: 3,
			CurrentStepID: "s-2", CurrentStepName: "build", ArtifactCount: 4,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskListItemDTO(open task):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a closed task carries the stamp behind the pointer", func(t *testing.T) {
		closed := task
		closed.ClosedTS = 777.5
		got := newTaskListItemDTO(closed, nil, 3, 3, 0, nil, TaskCurrentStep{})
		if got.ClosedTS == nil || *got.ClosedTS != 777.5 {
			t.Fatalf("newTaskListItemDTO(closed).ClosedTS = %v, want a pointer to 777.5", got.ClosedTS)
		}
	})

	t.Run("nil deps become an empty list on both fields, and a zero current step is an honest no-current-step", func(t *testing.T) {
		got := newTaskListItemDTO(task, nil, 0, 0, 0, nil, TaskCurrentStep{})
		if got.Deps == nil || len(got.Deps) != 0 {
			t.Fatalf("newTaskListItemDTO(nil deps).Deps = %#v, want an empty non-nil slice", got.Deps)
		}
		if got.DepTasks == nil || len(got.DepTasks) != 0 {
			t.Fatalf("newTaskListItemDTO(nil deps).DepTasks = %#v, want an empty non-nil slice", got.DepTasks)
		}
		if got.CurrentStepID != "" || got.CurrentStepName != "" {
			t.Fatalf("newTaskListItemDTO(no current step) = (%q, %q), want two empty strings",
				got.CurrentStepID, got.CurrentStepName)
		}
	})

	t.Run("a nil population leaves every dep unresolved rather than inventing a status", func(t *testing.T) {
		got := newTaskListItemDTO(task, []string{"t-dep"}, 0, 0, 0, nil, TaskCurrentStep{})
		want := []taskDepRefDTO{{ID: "t-dep", TaskNo: "t-dep"}}
		if !reflect.DeepEqual(got.DepTasks, want) {
			t.Fatalf("newTaskListItemDTO(nil population).DepTasks = %+v, want %+v", got.DepTasks, want)
		}
	})
}

func TestNewTaskDepRefDTOs(t *testing.T) {
	population := map[string]Task{
		"t-1": {ID: "t-1", Title: "First", Status: TaskStatusDone},
		"t-2": {ID: "t-2", Title: "Second", Status: TaskStatusInProgress},
	}

	t.Run("each dep resolves in order, and a dep whose task is gone keeps its number with blank display facts", func(t *testing.T) {
		got := newTaskDepRefDTOs([]string{"t-2", "t-gone", "t-1"}, population)
		want := []taskDepRefDTO{
			{ID: "t-2", TaskNo: "t-2", Title: "Second", Status: "in_progress"},
			{ID: "t-gone", TaskNo: "t-gone", Title: "", Status: ""},
			{ID: "t-1", TaskNo: "t-1", Title: "First", Status: "done"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskDepRefDTOs:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("no deps yields an empty non-nil slice while one dep yields one entry", func(t *testing.T) {
		got := newTaskDepRefDTOs(nil, population)
		if got == nil || len(got) != 0 {
			t.Fatalf("newTaskDepRefDTOs(nil) = %#v, want an empty non-nil slice", got)
		}
		if one := newTaskDepRefDTOs([]string{"t-1"}, population); len(one) != 1 {
			t.Fatalf("newTaskDepRefDTOs(one dep) = %+v, want exactly one entry", one)
		}
	})
}

func TestNewTaskManualDTO(t *testing.T) {
	manual := TaskManual{
		TypeKey: "bugfix", DisplayName: "Bug fix", Purpose: "修 bug",
		Fields:    `[{"name":"PR Link","required":true,"is_key":true}]`,
		SopMD:     "台北",
		Learnings: "abcd",
		Assignee:  `{"kind":"outsource","model":"opus"}`,
		UpdatedTS: 42.5,
	}

	t.Run("the stored blobs parse and both documents report their rune size beside their own cap", func(t *testing.T) {
		got, err := newTaskManualDTO(manual, 11000, 12000)
		if err != nil {
			t.Fatalf("newTaskManualDTO: %v", err)
		}
		want := taskManualDTO{
			LearningsChars: 4, SopMDChars: 2,
			LearningsCapChars: 12000, SopMDCapChars: 11000, CapChars: 12000,
			TypeKey: "bugfix", DisplayName: "Bug fix", Purpose: "修 bug",
			Fields:    []ManualField{{Name: "PR Link", Required: true, IsKey: true}},
			SopMD:     "台北",
			Learnings: "abcd",
			Assignee:  map[string]any{"kind": "outsource", "model": "opus"},
			UpdatedTS: 42.5,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskManualDTO:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("an unset fields blob and an unset assignee serialise as empty containers, never as null", func(t *testing.T) {
		bare := TaskManual{TypeKey: "bare"}
		got, err := newTaskManualDTO(bare, 1, 2)
		if err != nil {
			t.Fatalf("newTaskManualDTO(bare): %v", err)
		}
		if got.Fields == nil || len(got.Fields) != 0 {
			t.Fatalf("newTaskManualDTO(bare).Fields = %#v, want an empty non-nil slice", got.Fields)
		}
		if got.Assignee == nil || len(got.Assignee) != 0 {
			t.Fatalf("newTaskManualDTO(bare).Assignee = %#v, want an empty non-nil map", got.Assignee)
		}
	})

	t.Run("a corrupt fields blob and a corrupt assignee blob are both errors, never a silent empty", func(t *testing.T) {
		bad := manual
		bad.Fields = "{not json"
		got, err := newTaskManualDTO(bad, 1, 2)
		if err == nil || !strings.HasPrefix(err.Error(), "task_manual fields: bad JSON: ") {
			t.Fatalf("newTaskManualDTO(corrupt fields) error = %v", err)
		}
		if !reflect.DeepEqual(got, taskManualDTO{}) {
			t.Fatalf("newTaskManualDTO(corrupt fields) = %+v, want the zero DTO", got)
		}
		bad = manual
		bad.Assignee = "not json"
		got, err = newTaskManualDTO(bad, 1, 2)
		if err == nil || !strings.HasPrefix(err.Error(), "task_manual bugfix: bad assignee JSON: ") {
			t.Fatalf("newTaskManualDTO(corrupt assignee) error = %v", err)
		}
		if !reflect.DeepEqual(got, taskManualDTO{}) {
			t.Fatalf("newTaskManualDTO(corrupt assignee) = %+v, want the zero DTO", got)
		}
	})
}

func TestNewTaskManualListItemDTO(t *testing.T) {
	manual := TaskManual{
		TypeKey: "bugfix", DisplayName: "Bug fix", Purpose: "修 bug",
		Fields:    `[{"name":"PR Link","required":true,"is_key":true}]`,
		SopMD:     "台北市",
		Learnings: "abcd",
		Assignee:  `{"kind":"outsource"}`,
		UpdatedTS: 42.5,
	}

	t.Run("the row carries identity, fields and assignee, and reports both omitted documents' stored sizes", func(t *testing.T) {
		got, err := newTaskManualListItemDTO(manual, 11000, 12000)
		if err != nil {
			t.Fatalf("newTaskManualListItemDTO: %v", err)
		}
		want := taskManualListItemDTO{
			LearningsChars: 4, SopMDChars: 3,
			LearningsCapChars: 12000, SopMDCapChars: 11000, CapChars: 12000,
			TypeKey: "bugfix", DisplayName: "Bug fix", Purpose: "修 bug",
			Fields:    []ManualField{{Name: "PR Link", Required: true, IsKey: true}},
			Assignee:  map[string]any{"kind": "outsource"},
			UpdatedTS: 42.5,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newTaskManualListItemDTO:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("the sizes and caps agree with the full read of the same row", func(t *testing.T) {
		full, err := newTaskManualDTO(manual, 11000, 12000)
		if err != nil {
			t.Fatalf("newTaskManualDTO: %v", err)
		}
		row, err := newTaskManualListItemDTO(manual, 11000, 12000)
		if err != nil {
			t.Fatalf("newTaskManualListItemDTO: %v", err)
		}
		if row.LearningsChars != full.LearningsChars || row.SopMDChars != full.SopMDChars ||
			row.LearningsCapChars != full.LearningsCapChars ||
			row.SopMDCapChars != full.SopMDCapChars || row.CapChars != full.CapChars {
			t.Fatalf("the listing row and the full read disagree:\n row  %+v\n full %+v", row, full)
		}
	})

	t.Run("an unset fields blob and an unset assignee serialise as empty containers", func(t *testing.T) {
		got, err := newTaskManualListItemDTO(TaskManual{TypeKey: "bare"}, 1, 2)
		if err != nil {
			t.Fatalf("newTaskManualListItemDTO(bare): %v", err)
		}
		if got.Fields == nil || len(got.Fields) != 0 {
			t.Fatalf("newTaskManualListItemDTO(bare).Fields = %#v, want an empty non-nil slice", got.Fields)
		}
		if got.Assignee == nil || len(got.Assignee) != 0 {
			t.Fatalf("newTaskManualListItemDTO(bare).Assignee = %#v, want an empty non-nil map", got.Assignee)
		}
	})

	t.Run("a corrupt blob on either side is an error and the zero row", func(t *testing.T) {
		bad := manual
		bad.Fields = "{"
		got, err := newTaskManualListItemDTO(bad, 1, 2)
		if err == nil || !strings.HasPrefix(err.Error(), "task_manual fields: bad JSON: ") {
			t.Fatalf("newTaskManualListItemDTO(corrupt fields) error = %v", err)
		}
		if !reflect.DeepEqual(got, taskManualListItemDTO{}) {
			t.Fatalf("newTaskManualListItemDTO(corrupt fields) = %+v, want the zero row", got)
		}
		bad = manual
		bad.Assignee = "["
		got, err = newTaskManualListItemDTO(bad, 1, 2)
		if err == nil || !strings.HasPrefix(err.Error(), "task_manual bugfix: bad assignee JSON: ") {
			t.Fatalf("newTaskManualListItemDTO(corrupt assignee) error = %v", err)
		}
		if !reflect.DeepEqual(got, taskManualListItemDTO{}) {
			t.Fatalf("newTaskManualListItemDTO(corrupt assignee) = %+v, want the zero row", got)
		}
	})
}

func TestFoldActorRuntime(t *testing.T) {
	t.Run("a full telemetry and gauge entry folds every fact, with the account proven by its runtime stamp", func(t *testing.T) {
		tele := map[string]any{"cost": 1.25, "account": "acct-1", accountRuntimeKey: "claude"}
		gauge := map[string]any{"context_pct": 42.0, "compaction_count": 3}
		got := foldActorRuntime(tele, gauge, 9.5, "claude")
		if got.account != "acct-1" {
			t.Fatalf("foldActorRuntime account = %q, want %q", got.account, "acct-1")
		}
		if got.cost == nil || *got.cost != 1.25 {
			t.Fatalf("foldActorRuntime cost = %v, want a pointer to 1.25", got.cost)
		}
		if got.contextPct == nil || *got.contextPct != 42.0 {
			t.Fatalf("foldActorRuntime contextPct = %v, want a pointer to 42", got.contextPct)
		}
		if got.compactionCount == nil || *got.compactionCount != 3 {
			t.Fatalf("foldActorRuntime compactionCount = %v, want a pointer to 3", got.compactionCount)
		}
		if got.bankedCost == nil || *got.bankedCost != 9.5 {
			t.Fatalf("foldActorRuntime bankedCost = %v, want a pointer to 9.5", got.bankedCost)
		}
	})

	t.Run("nil maps and a zero banked cost fold all-empty rather than to fabricated zeros", func(t *testing.T) {
		got := foldActorRuntime(nil, nil, 0, "claude")
		if !reflect.DeepEqual(got, actorRuntimeFold{}) {
			t.Fatalf("foldActorRuntime(nothing reported) = %+v, want the zero fold", got)
		}
	})

	t.Run("an account whose runtime stamp disagrees with the actor's runtime is not served", func(t *testing.T) {
		tele := map[string]any{"account": "acct-1", accountRuntimeKey: "codex"}
		if got := foldActorRuntime(tele, nil, 0, "claude"); got.account != "" {
			t.Fatalf("foldActorRuntime(runtime mismatch).account = %q, want the empty string", got.account)
		}
		unstamped := map[string]any{"account": "acct-1"}
		if got := foldActorRuntime(unstamped, nil, 0, "claude"); got.account != "" {
			t.Fatalf("foldActorRuntime(unstamped account).account = %q, want the empty string", got.account)
		}
		stamped := map[string]any{"account": "acct-1", accountRuntimeKey: "claude"}
		if got := foldActorRuntime(stamped, nil, 0, ""); got.account != "acct-1" {
			t.Fatalf("foldActorRuntime(blank actor runtime normalises to claude).account = %q, want %q",
				got.account, "acct-1")
		}
	})

	t.Run("a zero cost and a zero context percentage are reported, but a zero banked cost is not", func(t *testing.T) {
		got := foldActorRuntime(map[string]any{"cost": 0.0}, map[string]any{"context_pct": 0.0}, 0, "claude")
		if got.cost == nil || *got.cost != 0 {
			t.Fatalf("foldActorRuntime(zero cost).cost = %v, want a pointer to 0", got.cost)
		}
		if got.contextPct == nil || *got.contextPct != 0 {
			t.Fatalf("foldActorRuntime(zero pct).contextPct = %v, want a pointer to 0", got.contextPct)
		}
		if got.bankedCost != nil {
			t.Fatalf("foldActorRuntime(zero banked).bankedCost = %v, want nil", got.bankedCost)
		}
		if neg := foldActorRuntime(nil, nil, -2.5, "claude"); neg.bankedCost == nil || *neg.bankedCost != -2.5 {
			t.Fatalf("foldActorRuntime(negative banked).bankedCost = %v, want a pointer to -2.5", neg.bankedCost)
		}
	})

	t.Run("a compaction count of the wrong Go type, or a negative one, is not served", func(t *testing.T) {
		if got := foldActorRuntime(nil, map[string]any{"compaction_count": 3.0}, 0, "claude"); got.compactionCount != nil {
			t.Fatalf("foldActorRuntime(float compaction_count) = %v, want nil", got.compactionCount)
		}
		if got := foldActorRuntime(nil, map[string]any{"compaction_count": -1}, 0, "claude"); got.compactionCount != nil {
			t.Fatalf("foldActorRuntime(negative compaction_count) = %v, want nil", got.compactionCount)
		}
		zero := foldActorRuntime(nil, map[string]any{"compaction_count": 0}, 0, "claude")
		if zero.compactionCount == nil || *zero.compactionCount != 0 {
			t.Fatalf("foldActorRuntime(zero compaction_count) = %v, want a pointer to 0", zero.compactionCount)
		}
	})

	t.Run("a cost of the wrong Go type is not served", func(t *testing.T) {
		if got := foldActorRuntime(map[string]any{"cost": "1.25"}, nil, 0, "claude"); got.cost != nil {
			t.Fatalf("foldActorRuntime(string cost).cost = %v, want nil", got.cost)
		}
	})
}

func TestNewOutsourceWorkerDTO(t *testing.T) {
	okTrue := true
	worker := OutsourceWorker{
		ID: "w-1", Codename: "O-7", Runtime: "", Model: "claude-opus-4-6", Effort: "high",
		ActualModel: "claude-opus-4-5", ActualRuntime: "claude", ActualEffort: "medium",
		TaskID: "t-1", Status: WorkerStatusActive, CreatedTS: 100,
		LastOp: "start", LastOpOK: &okTrue, LastOpLog: "ok", LastOpReason: "",
		LastOpAt: 150, DesiredMachineID: "m-2", LastMachineID: "m-1",
		BankedCost:   3.5,
		RefocusSince: 900, RefocusOp: refocusOpAcceleratedStop, DesiredState: DesiredStateOnline,
	}
	proj := outsourceWorkerProjection{
		unread: 4, now: 1000, online: true,
		cfg:            reconcileConfig{RecycleGrace: 60},
		tele:           map[string]any{"cost": 2.5, "account": "acct-1", accountRuntimeKey: "claude"},
		gaugeEntry:     map[string]any{"context_pct": 30.0, "compaction_count": 2},
		machineDisplay: func(id string) string { return "display:" + id },
		spawnTarget:    "m-3",
		accountDisplay: func(key string) string { return "Studio " + key },
		delegatedBy:    "Ann",
		typeDisplay:    func(key string) string { return "Type " + key },
	}
	task := &Task{ID: "t-1", Title: "Fix the thing", Status: TaskStatusInProgress,
		CreatorID: "ann", CreatedTS: 50, TypeKey: "bugfix"}

	t.Run("a bound worker projects whole: runtime facts fold, the machine and account resolve, the task rides along", func(t *testing.T) {
		got := newOutsourceWorkerDTO(worker, task, proj)
		wantCost, wantPct, wantBanked := 2.5, 30.0, 3.5
		wantCompaction := 2
		wantAccount := "Studio acct-1"
		want := outsourceWorkerDTO{
			ID: "w-1", Codename: "O-7",
			Runtime: "claude", Model: "claude-opus-4-6", Effort: "high",
			ActualModel: "claude-opus-4-5", ActualRuntime: "claude", ActualEffort: "medium",
			Status: "active", TaskID: "t-1", TaskTitle: "Fix the thing",
			TaskStatus: "in_progress", TaskNo: "t-1", TaskCreatedTS: 50,
			TaskTypeKey: "bugfix", TaskTypeName: "Type bugfix",
			CreatedTS: 100, UnreadCount: 4, Presence: "online",
			Machine: "display:m-3", DesiredMachineID: "m-2", ActualMachine: "m-1",
			Account: &wantAccount, ContextPct: &wantPct, CompactionCount: &wantCompaction,
			Cost: &wantCost, BankedCost: &wantBanked,
			LastOp: "start", LastOpOK: &okTrue, LastOpLog: "ok", LastOpReason: "",
			LastOpAt: 150, CreatorID: "ann", DelegatedBy: "Ann",
			RefocusSince: 900, RefocusOp: refocusOpAcceleratedStop,
			RefocusDeadline: 960, DesiredState: "online",
		}
		if !wireWorkerDTOEqual(got, want) {
			t.Fatalf("newOutsourceWorkerDTO(bound worker):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a nil task leaves every task field honest-empty and the row still lists", func(t *testing.T) {
		got := newOutsourceWorkerDTO(worker, nil, proj)
		if got.TaskTitle != "" || got.TaskStatus != "" || got.TaskNo != "" ||
			got.TaskCreatedTS != 0 || got.TaskTypeKey != "" || got.TaskTypeName != "" ||
			got.CreatorID != "" {
			t.Fatalf("newOutsourceWorkerDTO(nil task) fabricated task facts: %+v", got)
		}
		if got.ID != "w-1" || got.TaskID != "t-1" || got.Presence != "online" {
			t.Fatalf("newOutsourceWorkerDTO(nil task) dropped the row itself: %+v", got)
		}
	})

	t.Run("nothing observed and nothing reported leaves the machine, account and runtime facts empty", func(t *testing.T) {
		bare := OutsourceWorker{ID: "w-2", Status: WorkerStatusAssigned}
		got := newOutsourceWorkerDTO(bare, nil, outsourceWorkerProjection{now: 1000})
		if got.Machine != "" {
			t.Fatalf("newOutsourceWorkerDTO(nothing dispatched).Machine = %q, want the empty string", got.Machine)
		}
		if got.Account != nil || got.Cost != nil || got.ContextPct != nil ||
			got.BankedCost != nil || got.CompactionCount != nil {
			t.Fatalf("newOutsourceWorkerDTO(nothing reported) fabricated runtime facts: %+v", got)
		}
		if got.AvatarIconID != nil {
			t.Fatalf("newOutsourceWorkerDTO(no avatar choice).AvatarIconID = %q, want null", *got.AvatarIconID)
		}
		if got.Runtime != "claude" {
			t.Fatalf("newOutsourceWorkerDTO(blank runtime).Runtime = %q, want the normalised default", got.Runtime)
		}
		if got.Presence != "offline" {
			t.Fatalf("newOutsourceWorkerDTO(offline, no anchor).Presence = %q, want %q", got.Presence, "offline")
		}
		if got.RefocusDeadline != 0 {
			t.Fatalf("newOutsourceWorkerDTO(no wind-down).RefocusDeadline = %v, want 0", got.RefocusDeadline)
		}
	})

	t.Run("a resolved account key that has no readable name is served as null, not as the raw key", func(t *testing.T) {
		p := proj
		p.accountDisplay = func(string) string { return "" }
		if got := newOutsourceWorkerDTO(worker, nil, p); got.Account != nil {
			t.Fatalf("newOutsourceWorkerDTO(unreadable account).Account = %v, want nil", got.Account)
		}
		p.accountDisplay = nil
		if got := newOutsourceWorkerDTO(worker, nil, p); got.Account != nil {
			t.Fatalf("newOutsourceWorkerDTO(no resolver).Account = %v, want nil", got.Account)
		}
	})

	t.Run("a spawn target with no machine resolver leaves the machine label empty", func(t *testing.T) {
		p := proj
		p.machineDisplay = nil
		if got := newOutsourceWorkerDTO(worker, nil, p); got.Machine != "" {
			t.Fatalf("newOutsourceWorkerDTO(no machine resolver).Machine = %q, want the empty string", got.Machine)
		}
	})

	t.Run("a stopping worker reports the wind-down deadline of the offline axis", func(t *testing.T) {
		w := worker
		w.DesiredState = DesiredStateOffline
		w.StoppingSince = 800
		w.RefocusOp = refocusOpAcceleratedStop
		got := newOutsourceWorkerDTO(w, nil, proj)
		if got.RefocusDeadline != 860 {
			t.Fatalf("newOutsourceWorkerDTO(stopping).RefocusDeadline = %v, want 860", got.RefocusDeadline)
		}
		if got.Presence != "stopping" {
			t.Fatalf("newOutsourceWorkerDTO(stopping, online).Presence = %q, want %q", got.Presence, "stopping")
		}
	})
}

// wireWorkerDTOEqual compares two worker DTOs by VALUE, dereferencing the five
// nullable fields so a pointer identity difference is never mistaken for a
// payload difference.
func wireWorkerDTOEqual(a, b outsourceWorkerDTO) bool {
	eqStr := func(x, y *string) bool {
		return (x == nil) == (y == nil) && (x == nil || *x == *y)
	}
	eqF := func(x, y *float64) bool {
		return (x == nil) == (y == nil) && (x == nil || *x == *y)
	}
	eqI := func(x, y *int) bool {
		return (x == nil) == (y == nil) && (x == nil || *x == *y)
	}
	eqB := func(x, y *bool) bool {
		return (x == nil) == (y == nil) && (x == nil || *x == *y)
	}
	if !eqStr(a.Account, b.Account) || !eqF(a.ContextPct, b.ContextPct) ||
		!eqI(a.CompactionCount, b.CompactionCount) || !eqF(a.Cost, b.Cost) ||
		!eqF(a.BankedCost, b.BankedCost) || !eqB(a.LastOpOK, b.LastOpOK) {
		return false
	}
	a.Account, b.Account = nil, nil
	a.ContextPct, b.ContextPct = nil, nil
	a.CompactionCount, b.CompactionCount = nil, nil
	a.Cost, b.Cost = nil, nil
	a.BankedCost, b.BankedCost = nil, nil
	a.LastOpOK, b.LastOpOK = nil, nil
	return reflect.DeepEqual(a, b)
}

func TestWorkerPresence(t *testing.T) {
	const now = 10_000.0

	t.Run("a released worker is off-panel and has no presence word rather than a wrong one", func(t *testing.T) {
		w := OutsourceWorker{ID: "w-1", Status: WorkerStatusReleased,
			DesiredState: DesiredStateOnline, WakingSince: now - 1}
		if got := workerPresence(w, now, true); got != "" {
			t.Fatalf("workerPresence(released, online) = %q, want the empty string", got)
		}
		if got := workerPresence(w, now, false); got != "" {
			t.Fatalf("workerPresence(released, offline) = %q, want the empty string", got)
		}
	})

	t.Run("a live worker answers the same word the staff roster gets for the same anchors", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			w      OutsourceWorker
			online bool
			want   string
		}{
			{"online session", OutsourceWorker{Status: WorkerStatusActive}, true, "online"},
			{"fresh wake, not yet connected", OutsourceWorker{Status: WorkerStatusAssigned,
				DesiredState: DesiredStateOnline, WakingSince: now - 1}, false, "waking"},
			{"stale wake", OutsourceWorker{Status: WorkerStatusAssigned,
				DesiredState: DesiredStateOnline, WakingSince: now - WakingTTLSecs - 1}, false, "offline"},
			{"stop in flight", OutsourceWorker{Status: WorkerStatusActive,
				StoppingSince: now - 1}, true, "stopping"},
			{"stop finished", OutsourceWorker{Status: WorkerStatusActive,
				StoppingSince: now - 1}, false, "stopped"},
		} {
			got := workerPresence(tc.w, now, tc.online)
			if got != tc.want {
				t.Fatalf("workerPresence(%s) = %q, want %q", tc.name, got, tc.want)
			}
			if member := PresenceState(memberFromWorker(tc.w), now, tc.online); member != tc.want {
				t.Fatalf("workerPresence(%s) = %q but PresenceState on the same row = %q",
					tc.name, got, member)
			}
		}
	})
}

func TestAttachmentDTOsFromRefs(t *testing.T) {
	t.Run("each ref becomes a served view with its own blob url and image flag", func(t *testing.T) {
		refs := []any{
			map[string]any{"id": "att-1", "mime": "image/png", "filename": "shot.png"},
			map[string]any{"id": "att-2", "mime": "text/markdown", "filename": "notes.md"},
		}
		got := attachmentDTOsFromRefs(refs)
		want := []chatAttachmentDTO{
			{ID: "att-1", URL: "/api/chat/attachment/att-1", Filename: "shot.png",
				Mime: "image/png", IsImage: true},
			{ID: "att-2", URL: "/api/chat/attachment/att-2", Filename: "notes.md",
				Mime: "text/markdown", IsImage: false},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("attachmentDTOsFromRefs:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a ref with no usable id is dropped rather than given a fabricated serve url", func(t *testing.T) {
		refs := []any{
			map[string]any{"id": ""},
			map[string]any{"mime": "image/png"},
			map[string]any{"id": 7},
			"not-a-map",
			map[string]any{"id": "att-9"},
		}
		got := attachmentDTOsFromRefs(refs)
		want := []chatAttachmentDTO{{ID: "att-9", URL: "/api/chat/attachment/att-9"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("attachmentDTOsFromRefs(unusable refs):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("no refs at all yields an empty non-nil slice, so the wire never carries null", func(t *testing.T) {
		for name, refs := range map[string][]any{"nil": nil, "empty": {}} {
			got := attachmentDTOsFromRefs(refs)
			if got == nil || len(got) != 0 {
				t.Fatalf("attachmentDTOsFromRefs(%s) = %#v, want an empty non-nil slice", name, got)
			}
		}
	})

	t.Run("a mime shorter than the image prefix does not set the image flag", func(t *testing.T) {
		refs := []any{map[string]any{"id": "att-1", "mime": "image"}}
		if got := attachmentDTOsFromRefs(refs); got[0].IsImage {
			t.Fatalf("attachmentDTOsFromRefs(mime \"image\").IsImage = true, want false")
		}
	})
}

func TestNewChatMessageDTO(t *testing.T) {
	t.Run("a stored row projects with its attachments derived from the light meta refs", func(t *testing.T) {
		m := ChatMessage{
			ID: "c-1", Sender: "ann", Recipient: "bob", Body: "hi", TS: 42.5,
			Meta: map[string]any{
				"attachments":       []any{map[string]any{"id": "att-1", "mime": "image/png", "filename": "s.png"}},
				chatReplyToMetaKey:  "c-0",
				"anything_the_post": "rides through",
			},
		}
		got := newChatMessageDTO(m)
		want := chatMessageDTO{
			ID: "c-1", From: "ann", To: "bob", Body: "hi", TS: 42.5,
			Meta: m.Meta,
			Attachments: []chatAttachmentDTO{
				{ID: "att-1", URL: "/api/chat/attachment/att-1", Filename: "s.png",
					Mime: "image/png", IsImage: true},
			},
			ReplyTo: "c-0",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newChatMessageDTO:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a row with nil meta serialises an empty meta map and an empty attachment list", func(t *testing.T) {
		got := newChatMessageDTO(ChatMessage{ID: "c-2", Sender: "ann", Recipient: "bob"})
		if got.Meta == nil || len(got.Meta) != 0 {
			t.Fatalf("newChatMessageDTO(nil meta).Meta = %#v, want an empty non-nil map", got.Meta)
		}
		if got.Attachments == nil || len(got.Attachments) != 0 {
			t.Fatalf("newChatMessageDTO(nil meta).Attachments = %#v, want an empty non-nil slice",
				got.Attachments)
		}
		if got.ReplyTo != "" {
			t.Fatalf("newChatMessageDTO(nil meta).ReplyTo = %q, want the empty string", got.ReplyTo)
		}
	})

	t.Run("the display names, the quote and the card are left for the serving layer to fill", func(t *testing.T) {
		got := newChatMessageDTO(ChatMessage{ID: "c-3", Sender: "ann", Recipient: "bob",
			Meta: map[string]any{chatReplyToMetaKey: "c-0", "reply_card_id": "rc-1"}})
		if got.FromName != "" || got.ToName != "" {
			t.Fatalf("newChatMessageDTO resolved names it was never given: %+v", got)
		}
		if got.ReplyToChat != nil || got.Card != nil || got.ReplyCardStatus != "" ||
			got.TSDisplay != "" || got.BodyOmittedChars != 0 {
			t.Fatalf("newChatMessageDTO filled a serving-layer field: %+v", got)
		}
	})
}

func TestReplyToFromMeta(t *testing.T) {
	t.Run("the stored reply_to is served back", func(t *testing.T) {
		if got := replyToFromMeta(map[string]any{chatReplyToMetaKey: "c-0"}); got != "c-0" {
			t.Fatalf("replyToFromMeta(with a reply_to) = %q, want %q", got, "c-0")
		}
	})

	t.Run("a nil map, a missing key and a non-string value all read as replying to nothing", func(t *testing.T) {
		if got := replyToFromMeta(nil); got != "" {
			t.Fatalf("replyToFromMeta(nil) = %q, want the empty string", got)
		}
		if got := replyToFromMeta(map[string]any{}); got != "" {
			t.Fatalf("replyToFromMeta(empty) = %q, want the empty string", got)
		}
		if got := replyToFromMeta(map[string]any{chatReplyToMetaKey: 7}); got != "" {
			t.Fatalf("replyToFromMeta(non-string) = %q, want the empty string", got)
		}
		if got := replyToFromMeta(map[string]any{chatReplyToMetaKey: nil}); got != "" {
			t.Fatalf("replyToFromMeta(null) = %q, want the empty string", got)
		}
	})
}

func TestReplyCardIDFromMeta(t *testing.T) {
	t.Run("the stored reply_card_id is served back", func(t *testing.T) {
		if got := replyCardIDFromMeta(map[string]any{"reply_card_id": "rc-1"}); got != "rc-1" {
			t.Fatalf("replyCardIDFromMeta(with a card) = %q, want %q", got, "rc-1")
		}
	})

	t.Run("a nil map, a missing key and a non-string value all read as carrying no card", func(t *testing.T) {
		if got := replyCardIDFromMeta(nil); got != "" {
			t.Fatalf("replyCardIDFromMeta(nil) = %q, want the empty string", got)
		}
		if got := replyCardIDFromMeta(map[string]any{chatReplyToMetaKey: "c-0"}); got != "" {
			t.Fatalf("replyCardIDFromMeta(no card key) = %q, want the empty string", got)
		}
		if got := replyCardIDFromMeta(map[string]any{"reply_card_id": 7}); got != "" {
			t.Fatalf("replyCardIDFromMeta(non-string) = %q, want the empty string", got)
		}
	})
}

func TestNewReplyCardDTO(t *testing.T) {
	base := ReplyCard{
		ID: "rc-1", FromMember: "ann", Kind: "decision", Summary: "Which one?",
		Body: "the long question", Options: []ReplyCardOption{{Text: "A", AIPick: true}, {Text: "B"}},
		SelectMode: "multi", CreatedTS: 100, ChatMessageID: "c-1",
		Attachments: []any{map[string]any{"id": "att-1", "mime": "image/png", "filename": "q.png"}},
		AnsweredTS:  200,
		ExpiredTS:   300,
		AnswerText:  "I pick A",
		TaskID:      "t-1",
		TaskStepID:  "s-1",
	}

	t.Run("a waiting card serialises answered_ts, expired_ts and answer as null", func(t *testing.T) {
		c := base
		c.Status = "waiting"
		got := newReplyCardDTO(c)
		want := replyCardDTO{
			ID: "rc-1", From: "ann", Kind: "decision", Summary: "Which one?",
			Body: "the long question", Options: []ReplyCardOption{{Text: "A", AIPick: true}, {Text: "B"}},
			SelectMode: "multi", Status: "waiting", CreatedTS: 100,
			Attachments: []chatAttachmentDTO{{ID: "att-1", URL: "/api/chat/attachment/att-1",
				Filename: "q.png", Mime: "image/png", IsImage: true}},
			AnsweredTS: nil, ExpiredTS: nil, ChatMessageID: "c-1", Answer: nil, Task: nil,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newReplyCardDTO(waiting):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("an answered card carries the answer block and the answered stamp, but still no expired stamp", func(t *testing.T) {
		c := base
		c.Status = "answered"
		c.AnswerOptionIdxs = []int{0, 1}
		c.AnswerAttachments = []any{map[string]any{"id": "att-2", "mime": "text/plain"}}
		got := newReplyCardDTO(c)
		if got.AnsweredTS == nil || *got.AnsweredTS != 200 {
			t.Fatalf("newReplyCardDTO(answered).AnsweredTS = %v, want a pointer to 200", got.AnsweredTS)
		}
		if got.ExpiredTS != nil {
			t.Fatalf("newReplyCardDTO(answered).ExpiredTS = %v, want nil", got.ExpiredTS)
		}
		wantAnswer := &replyCardAnswerDTO{
			OptionIdxs: []int{0, 1}, Text: "I pick A",
			Attachments: []chatAttachmentDTO{{ID: "att-2", URL: "/api/chat/attachment/att-2",
				Mime: "text/plain"}},
		}
		if !reflect.DeepEqual(got.Answer, wantAnswer) {
			t.Fatalf("newReplyCardDTO(answered).Answer:\n got %+v\nwant %+v", got.Answer, wantAnswer)
		}
	})

	t.Run("an expired card carries the expired stamp and no answer", func(t *testing.T) {
		c := base
		c.Status = "expired"
		got := newReplyCardDTO(c)
		if got.ExpiredTS == nil || *got.ExpiredTS != 300 {
			t.Fatalf("newReplyCardDTO(expired).ExpiredTS = %v, want a pointer to 300", got.ExpiredTS)
		}
		if got.AnsweredTS != nil || got.Answer != nil {
			t.Fatalf("newReplyCardDTO(expired) carried an answer: %+v", got)
		}
	})

	t.Run("a card with no options and no select mode serialises an empty option list and the single default", func(t *testing.T) {
		c := ReplyCard{ID: "rc-2", Status: "waiting"}
		got := newReplyCardDTO(c)
		if got.Options == nil || len(got.Options) != 0 {
			t.Fatalf("newReplyCardDTO(no options).Options = %#v, want an empty non-nil slice", got.Options)
		}
		if got.SelectMode != "single" {
			t.Fatalf("newReplyCardDTO(no select mode).SelectMode = %q, want %q", got.SelectMode, "single")
		}
		if got.Attachments == nil || len(got.Attachments) != 0 {
			t.Fatalf("newReplyCardDTO(no attachments).Attachments = %#v, want an empty non-nil slice",
				got.Attachments)
		}
	})

	t.Run("the task linkage is left for the serving layer even on a card armed from a gate", func(t *testing.T) {
		c := base
		c.Status = "waiting"
		if got := newReplyCardDTO(c); got.Task != nil {
			t.Fatalf("newReplyCardDTO(gate card).Task = %+v, want nil", got.Task)
		}
	})
}

func TestNewScheduledMessageDTO(t *testing.T) {
	t.Run("a custom schedule projects whole with its four sets sorted and deduplicated", func(t *testing.T) {
		m := ScheduledMessage{
			ID: "sm-1", MemberID: "ann", Label: "standup", Body: "早安",
			Cadence: "custom", DayOfWeek: 3, DayOfMonth: 15, Hour: 9, Minute: 30,
			CustomMonths:  []int{12, 1, 1},
			CustomDays:    []int{2, 1},
			CustomHours:   []int{9},
			CustomMinutes: []int{40, 0, 20},
			Timezone:      "Asia/Taipei", Status: "enabled",
			LastFiredSlot: "2026-08-10T09:00+08:00", LastFiredTS: 500, CreatedTS: 100,
		}
		got := newScheduledMessageDTO(m)
		want := scheduledMessageDTO{
			ID: "sm-1", MemberID: "ann", Label: "standup", Body: "早安",
			Cadence: "custom", DayOfWeek: 3, DayOfMonth: 15, Hour: 9, Minute: 30,
			CustomMonths: []int{1, 12}, CustomDays: []int{1, 2},
			CustomHours: []int{9}, CustomMinutes: []int{0, 20, 40},
			Timezone: "Asia/Taipei", Status: "enabled",
			LastFiredSlot: "2026-08-10T09:00+08:00", LastFiredTS: 500, CreatedTS: 100,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newScheduledMessageDTO(custom):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a daily schedule still emits all four sets, as empty arrays rather than null", func(t *testing.T) {
		m := ScheduledMessage{ID: "sm-2", MemberID: "ann", Cadence: "daily",
			Hour: 9, Minute: 0, Timezone: "UTC", Status: "enabled"}
		got := newScheduledMessageDTO(m)
		for name, set := range map[string][]int{
			"custom_months": got.CustomMonths, "custom_days": got.CustomDays,
			"custom_hours": got.CustomHours, "custom_minutes": got.CustomMinutes,
		} {
			if set == nil || len(set) != 0 {
				t.Fatalf("newScheduledMessageDTO(daily).%s = %#v, want an empty non-nil slice", name, set)
			}
		}
		if got.LastFiredSlot != "" || got.LastFiredTS != 0 {
			t.Fatalf("newScheduledMessageDTO(never fired) cursor = (%q, %v), want (\"\", 0)",
				got.LastFiredSlot, got.LastFiredTS)
		}
	})
}

func TestScheduledMessageReceiptOf(t *testing.T) {
	m := ScheduledMessage{
		ID: "sm-1", MemberID: "ann", Label: "standup", Body: "早安 studio",
		Cadence: "custom", DayOfWeek: 3, DayOfMonth: 15, Hour: 9, Minute: 30,
		CustomMonths: []int{12, 1}, CustomDays: []int{5}, CustomHours: []int{9},
		CustomMinutes: []int{0}, Timezone: "Asia/Taipei", Status: "enabled",
		LastFiredSlot: "2026-08-10T09:00+08:00", LastFiredTS: 500, CreatedTS: 100,
	}

	t.Run("the receipt reports the body as a rune count and keeps only the server-decided fields", func(t *testing.T) {
		got := scheduledMessageReceiptOf(m)
		want := scheduledMessageReceiptDTO{
			ID: "sm-1", MemberID: "ann", Label: "standup", BodySizeChars: 9,
			Cadence: "custom", CustomMonths: []int{1, 12}, DayOfMonth: 15, DayOfWeek: 3,
			Status: "enabled", LastFiredSlot: "2026-08-10T09:00+08:00",
			LastFiredTS: 500, CreatedTS: 100,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("scheduledMessageReceiptOf:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a non-custom schedule still emits custom_months, as an empty array rather than null", func(t *testing.T) {
		daily := ScheduledMessage{ID: "sm-2", MemberID: "ann", Cadence: "daily", Status: "enabled"}
		got := scheduledMessageReceiptOf(daily)
		if got.CustomMonths == nil || len(got.CustomMonths) != 0 {
			t.Fatalf("scheduledMessageReceiptOf(daily).CustomMonths = %#v, want an empty non-nil slice",
				got.CustomMonths)
		}
		if got.BodySizeChars != 0 {
			t.Fatalf("scheduledMessageReceiptOf(empty body).BodySizeChars = %d, want 0", got.BodySizeChars)
		}
	})

	t.Run("the receipt and the read face agree on every field they share", func(t *testing.T) {
		read := newScheduledMessageDTO(m)
		receipt := scheduledMessageReceiptOf(m)
		if read.ID != receipt.ID || read.MemberID != receipt.MemberID ||
			read.Label != receipt.Label || read.Cadence != receipt.Cadence ||
			read.DayOfMonth != receipt.DayOfMonth || read.DayOfWeek != receipt.DayOfWeek ||
			read.Status != receipt.Status || read.LastFiredSlot != receipt.LastFiredSlot ||
			read.LastFiredTS != receipt.LastFiredTS || read.CreatedTS != receipt.CreatedTS ||
			!reflect.DeepEqual(read.CustomMonths, receipt.CustomMonths) {
			t.Fatalf("the receipt and the read face disagree:\n read    %+v\n receipt %+v", read, receipt)
		}
	})
}

func TestIntSetOrEmpty(t *testing.T) {
	t.Run("values come back sorted and deduplicated", func(t *testing.T) {
		got := intSetOrEmpty([]int{40, 0, 20, 40, 0})
		want := []int{0, 20, 40}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("intSetOrEmpty([40 0 20 40 0]) = %v, want %v", got, want)
		}
	})

	t.Run("a nil or empty input renders as an empty array so the wire never carries null", func(t *testing.T) {
		for name, in := range map[string][]int{"nil": nil, "empty": {}} {
			got := intSetOrEmpty(in)
			if got == nil || len(got) != 0 {
				t.Fatalf("intSetOrEmpty(%s) = %#v, want an empty non-nil slice", name, got)
			}
		}
	})

	t.Run("a single value survives as a one-element set", func(t *testing.T) {
		if got := intSetOrEmpty([]int{7}); !reflect.DeepEqual(got, []int{7}) {
			t.Fatalf("intSetOrEmpty([7]) = %v, want [7]", got)
		}
	})
}

func TestNewWebhookRequestLogDTO(t *testing.T) {
	t.Run("every ring-buffer field rides across unchanged", func(t *testing.T) {
		l := WebhookRequestLog{TS: 42.5, Outcome: "delivered",
			Headers: `{"X-Test":"1"}`, Body: "payload", Truncated: true}
		got := newWebhookRequestLogDTO(l)
		want := webhookRequestLogDTO{TS: 42.5, Outcome: "delivered",
			Headers: `{"X-Test":"1"}`, Body: "payload", Truncated: true}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newWebhookRequestLogDTO:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a dropped row with no body still projects its own zero values", func(t *testing.T) {
		got := newWebhookRequestLogDTO(WebhookRequestLog{Outcome: "sig_failed"})
		want := webhookRequestLogDTO{Outcome: "sig_failed"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newWebhookRequestLogDTO(bare row):\n got %+v\nwant %+v", got, want)
		}
	})
}

func TestNewWebhookEndpointDTO(t *testing.T) {
	t.Run("the row projects whole, and a configured signing secret is reported as a bare yes", func(t *testing.T) {
		e := WebhookEndpoint{
			Token: "wht-abc", MemberID: "ann", EndpointID: "deploy-bot",
			Purpose: "CI", Status: "enabled", CreatedTS: 100,
			Platform: WebhookPlatformSlack, SigningSecret: "s3cr3t",
			LastReceivedTS: 200, DeliveredCount: 7, DroppedCount: 2,
			LastDropReason: "sig_failed",
		}
		got := newWebhookEndpointDTO(e)
		want := webhookEndpointDTO{
			EndpointID: "deploy-bot", Purpose: "CI", Status: "enabled", CreatedTS: 100,
			Token: "wht-abc", Platform: "slack", HasSigningSecret: true,
			LastReceivedTS: 200, DeliveredCount: 7, DroppedCount: 2,
			LastDropReason: "sig_failed",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newWebhookEndpointDTO:\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("the secret is reduced to a yes and the owning member id is not part of the payload at all", func(t *testing.T) {
		e := WebhookEndpoint{Token: "wht-abc", MemberID: "ann", EndpointID: "e1",
			SigningSecret: "s3cr3t", Platform: WebhookPlatformGithub}
		got := newWebhookEndpointDTO(e)
		want := webhookEndpointDTO{EndpointID: "e1", Token: "wht-abc", Platform: "github",
			HasSigningSecret: true}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newWebhookEndpointDTO(secret + member id):\n got %+v\nwant %+v", got, want)
		}
	})

	t.Run("a pre-platform row defaults to the generic preset, and no secret reports false", func(t *testing.T) {
		got := newWebhookEndpointDTO(WebhookEndpoint{Token: "wht-1", EndpointID: "e1"})
		want := webhookEndpointDTO{EndpointID: "e1", Token: "wht-1", Platform: "generic",
			HasSigningSecret: false}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("newWebhookEndpointDTO(pre-platform row):\n got %+v\nwant %+v", got, want)
		}
	})
}
