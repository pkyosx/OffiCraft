package main

// T-c9c0 — the 下線程序 document. Its replace / reset / cap / no-op / authz /
// history behaviour is covered by the shared table in api_bootdocs_t791e_test.go
// (bootDocCases). What is here is the part that table cannot see: the three
// places where forgetting this kind fails SILENTLY.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 🔴 publishDocumentHistoryRestore is a switch with no default: omit the
// offboard kind and the restore still answers 200 and changes the database,
// while every open surface keeps showing the old text. Nothing else in the
// build goes red. Same shape, same reason as the insight case
// (api_document_history_insight_publish_test.go).
func TestRestoringTheOffboardDocFansAGlobalContextDelta(t *testing.T) {
	f := newHistoryFixture(t)

	// The offboard document carries a read-only head (T-3201) that every write
	// must return verbatim; this test is about the RESTORE fanning a delta, so
	// the head rides along rather than being spelled at each call.
	offboardSeed, _, err := f.api.root.seedBlockMD(offboardSeedMD)
	if err != nil {
		t.Fatal(err)
	}
	offboardHead, _, split := DocSplitHeadBody(offboardSeed)
	if !split {
		t.Fatal("the offboard seed lost its read-only head")
	}
	writeOffboard := func(body string) {
		t.Helper()
		text := DocJoinHeadBody(offboardHead, body)
		rec := httptest.NewRecorder()
		f.api.HandleReplaceOffboardApiOffboardPost(rec,
			f.req(http.MethodPost, "/api/offboard", map[string]any{"text": text}))
		if rec.Code != http.StatusOK {
			t.Fatalf("replace offboard %q: status=%d body=%s", text, rec.Code, rec.Body.String())
		}
	}
	writeOffboard("# 下線程序\n\nfirst\n")
	writeOffboard("# 下線程序\n\nsecond\n")

	// Connect AFTER the writes so the queue holds only the restore frame.
	listener, err := f.api.hub.Connect("", "")
	if err != nil {
		t.Fatal(err)
	}

	versions := f.list(docKindOffboard, offboardDocKey)
	if len(versions) == 0 {
		t.Fatal("offboard kept no revision to restore — the assertion below would be vacuous")
	}
	f.restore(docKindOffboard, offboardDocKey, versions[0].Id)

	raw := listener.pop()
	if raw == nil {
		t.Fatal("restoring the 下線程序 doc fanned NO frame: the restore answered 200 and changed the " +
			"database, so nothing else in the build will tell you — every open surface is now showing " +
			"stale text. Add docKindOffboard to publishDocumentHistoryRestore.")
	}
	_, envelope := parseSSEFrame(t, raw)
	if envelope["topic"] != "global_context" {
		t.Fatalf("restore fanned topic=%v, want \"global_context\" (the topic publishBootDoc uses; a "+
			"topic outside the closed set in sseTopics is dropped SILENTLY)", envelope["topic"])
	}
}

// The retained depth is a per-KIND table lookup with a silent fallback: a kind
// missing from documentHistoryKeepByKind keeps three versions instead of ten and
// nothing says so — the owner simply finds the version worth going back to gone.
func TestOffboardKeepsTenRetainedVersions(t *testing.T) {
	if got := documentHistoryKeepFor(docKindOffboard); got != 10 {
		t.Fatalf("documentHistoryKeepFor(%q) = %d, want 10 — without an entry in "+
			"documentHistoryKeepByKind it silently falls back to %d",
			docKindOffboard, got, documentHistoryKeepDefault)
	}
	// Control: the fallback really is the smaller number, so the assertion
	// above distinguishes a present entry from an absent one.
	if got := documentHistoryKeepFor("global_context"); got != documentHistoryKeepDefault {
		t.Fatalf("control: global_context depth = %d, want the default %d", got, documentHistoryKeepDefault)
	}
}

// unknownBootDocKeyMsg used to answer the system-interaction key for every kind
// that was not boot_sequence, so a mistyped offboard key would have been told to
// use a key belonging to another document.
func TestUnknownBootDocKeyMsgNamesEachKindsOwnKey(t *testing.T) {
	for _, c := range []struct{ kind, want string }{
		{docKindOffboard, "'" + offboardDocKey + "'"},
		{docKindSystemInteraction, "'" + systemInteractionDocKey + "'"},
		{docKindBootSequence, "'" + bootSequenceKeyClaude + "' or '" + bootSequenceKeyCodex + "'"},
	} {
		msg := unknownBootDocKeyMsg(c.kind, "typo")
		if !strings.Contains(msg, c.kind) || !strings.HasSuffix(msg, c.want) {
			t.Fatalf("unknownBootDocKeyMsg(%q, \"typo\") = %q, want it to name %q and end with %s",
				c.kind, msg, c.kind, c.want)
		}
	}
	// A kind this server serves no document for must not borrow another
	// document's key list.
	if msg := unknownBootDocKeyMsg("not_a_kind", "typo"); strings.Contains(msg, systemInteractionDocKey) {
		t.Fatalf("an unknown kind was described with the system-interaction key: %q", msg)
	}
}
