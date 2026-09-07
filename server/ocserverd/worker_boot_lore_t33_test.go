package main

// worker_boot_lore_t33_test.go — the guard that replaced
// TestWorkerBootContextIsTheStaffFoldMinusThePersona (owner 2026-09-07, card
// rc-3c24fdc61ed3).
//
// 🔴 WHY THE OLD GUARD COULD NOT SIMPLY BE LOOSENED. It said 「外包的開機檔 ＝
// 正職的開機檔扣掉第 3 格」, and it was the only thing keeping the two assembly
// paths in step — assets.go buildBootContext and worker_spawn.go
// buildWorkerBootContext each build slots 1, 2 and 4 with their own code, and
// that equality is what caught a divergence between them. Slot 3 is no longer
// empty on the worker side, so the equality is now false.
//
// The cheap repair would have been to widen it: cut BOTH slot 3s out and compare
// what is left. That is a guard that gets weaker every time it goes red, and the
// next person to hit it has the same cheap repair available again. So it is
// replaced by two assertions that are each NARROWER than the original and
// together say more:
//
//	1. TestBothBootPathsShareSlots124ByteForByte — the divergence check, kept
//	   whole. Slots 1, 2 and 4 are still compared byte for byte.
//	2. TestNeitherBootPathCarriesTheOtherScopesLore — new, and impossible to
//	   state under the old equality: each path's slot 3 carries its OWN lore
//	   scope and never the other's.
//
// 🔴 BOTH FIXTURES SEED LORE ON BOTH SIDES. With no entries anywhere the lore
// block renders as "" on both paths and every assertion below passes without
// touching the thing it is about — the old guard would itself still be green
// today for exactly that reason. Seeding is what makes these non-vacuous, and
// the length floors are what say so out loud.

import (
	"strings"
	"testing"
)

// workerBootLoreFixture stands up one server carrying one staff member's entry
// and one outsource member's entry, and returns both assembled documents.
//
// Both folds run on ONE server so the shared slots are the same bytes by
// construction rather than by a second re-derivation of them here.
//
// 🔴 SINCE THE SCOPES COLLAPSED, BOTH MARKERS SIT IN THE SAME SCOPE KIND AND
// DIFFER ONLY BY KEY (owner 2026-09-07, card rc-a43100fd0486 [0]). That makes
// TestNeitherBootPathCarriesTheOtherScopesLore STRICTER than it was, not weaker:
// before, an implementation that ignored the scope KEY and selected on kind
// alone would still have separated these two documents, because one was `role`
// and the other `agent`. Now both are `agent`, so only a selection that honours
// the key keeps them apart — the exact bug the test names, and one it could not
// previously have caught.
//
// 🔴 A REAL *Member IS PASSED TO buildBootContext, WHERE nil USED TO DO. It has
// to be: the staff fold keys its 傳承 by the member id now, and buildBootContext
// with no member has no id to key by and deliberately emits no lore block at all
// (the cockpit's role PREVIEW path). Passing nil here would leave the staff side
// permanently empty and every assertion below would pass while testing nothing —
// which is why the inertness checks at the bottom of this fixture are load-bearing
// rather than decorative.
func workerBootLoreFixture(t *testing.T) (staffDoc, workerDoc, workerID string) {
	t.Helper()
	s := newWorkerTestServer(t)
	const ownerMark = "T33-OWNER-CUSTOM-MARKER"
	if err := s.dal.PutUserContext(UserContext{Text: ownerMark}); err != nil {
		t.Fatalf("put user context: %v", err)
	}

	// A staff member on the roster, carrying a role_key. The role is set on
	// purpose even though nothing keys by it any more: it is what makes an
	// implementation that quietly went back to keying by role FAIL here rather
	// than find an empty string and emit nothing.
	staffMember := &Member{
		ID: "m-t33boot", Name: "Staffer", Kind: KindStaff, RoleKey: defaultBootRole,
		Runtime: RuntimeClaude, RosterStatus: RosterStatusActive,
	}
	if err := s.dal.PutMember(*staffMember); err != nil {
		t.Fatalf("PutMember: %v", err)
	}

	staff, err := s.buildBootContext("", staffMember)
	if err != nil || staff == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	workerID = "ow-t33boot"
	seedLore(t, s.dal, LoreScopeAgent, staffMember.ID, "STAFF-SCOPED-MARKER", LoreStateActive, 100)
	seedLore(t, s.dal, LoreScopeAgent, workerID, "WORKER-SCOPED-MARKER", LoreStateActive, 100)

	// Rebuild the staff document so it carries the entry seeded above.
	staff, err = s.buildBootContext("", staffMember)
	if err != nil || staff == nil {
		t.Fatalf("buildBootContext (after seeding): %v", err)
	}
	worker, err := s.buildWorkerBootContext(
		OutsourceWorker{ID: workerID, Codename: "O-9", Model: "opus", Effort: "high",
			Runtime: RuntimeClaude},
		Task{ID: "t-aabbccddeeff", TypeKey: "review-pr", Title: "Review PR 42",
			Priority: TaskPriorityHigh},
		&TaskManual{TypeKey: "review-pr", DisplayName: "審查 PR",
			Purpose: "review 一個 PR", SopMD: "先看 diff 再留結論"})
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}

	// The fixture must actually contain both entries, or every assertion built on
	// it is about a document that has nothing in the slot under test.
	if !strings.Contains(staff.Context, "STAFF-SCOPED-MARKER") {
		t.Fatalf("fixture is inert: the staff document carries no member-scoped 傳承 — " +
			"slot 3 on the staff path is empty, so nothing below is being tested")
	}
	if !strings.Contains(worker, "WORKER-SCOPED-MARKER") {
		t.Fatalf("fixture is inert: the outsource document carries no agent-scoped 傳承 — " +
			"slot 3 is still empty on the worker path, so nothing below is being tested")
	}
	return staff.Context, worker, workerID
}

// cutSlot3 returns the document with its slot-3 span removed, and the byte
// offset the cut was made at.
//
// Slot 4 does NOT begin at the 啟動步驟 heading: the owner's 2026-08-15 rewrite
// hoisted the runtime 執行環境 note into a top-level section that leads the
// block, so cutting at 啟動步驟 would leave that note on one side only.
//
// 🔴 startAnchor MUST BE THE WHOLE OPENING LINE OF THE BLOCK, not the heading
// text on its own. `# 傳承` alone is a SUBSTRING of `### 傳承寫入位置`, a heading
// the shared handbook in slot 1 carries — so anchoring on the bare heading finds
// a match up in slot 1 and silently cuts 2,378 bytes of SHARED text out of one
// side only. That is not a hypothetical: it is what this guard did on its first
// run, and the symptom was a byte-count mismatch that reads exactly like a real
// divergence between the two assembly paths.
func cutSlot3(t *testing.T, doc, startAnchor string, minSpan int) (string, int) {
	t.Helper()
	if n := strings.Count(doc, startAnchor); n != 1 {
		t.Fatalf("slot-3 start anchor %q matches %d times, not once — a multi-match anchor "+
			"cuts at whichever one comes first, which may be in a different slot entirely",
			startAnchor, n)
	}
	start := strings.Index(doc, startAnchor)
	end := strings.Index(doc, "# Claude Code 執行環境")
	if start < 0 || end < 0 || start >= end {
		t.Fatalf("cannot locate slot 3 (start %q=%d, slot 4=%d) — the assembly moved "+
			"and this guard must be re-derived, not deleted", startAnchor, start, end)
	}
	// Positive control: the span really is substantial, so "minus slot 3" is a
	// real subtraction rather than a no-op that makes the comparison vacuous.
	if end-start < minSpan {
		t.Fatalf("slot 3 is only %d bytes (floor %d) — too small to be the block it "+
			"claims to be, so removing it would prove nothing", end-start, minSpan)
	}
	return doc[:start] + doc[end:], start
}

// TestBothBootPathsShareSlots124ByteForByte is the divergence check the old
// equality carried, kept at its original strength: slots 1, 2 and 4 are compared
// BYTE FOR BYTE, not with a "contains".
//
// 🔴 SCOPE, INHERITED FROM THE GUARD IT REPLACES: because one side is built from
// the other's actual output, an edit to a SHARED SEED moves both sides and this
// stays green. It answers "are the two assemblies still the same shape?" and says
// nothing about whether the shared documents still say the right thing.
func TestBothBootPathsShareSlots124ByteForByte(t *testing.T) {
	staffDoc, workerDoc, _ := workerBootLoreFixture(t)

	// The staff persona begins at 角色說明 and runs through 判準 → 長期筆記 → 傳承.
	staffRest, staffCut := cutSlot3(t, staffDoc, "# Role: ", 200)
	// The worker's slot 3 is the lore block alone, so it begins at that heading.
	workerRest, _ := cutSlot3(t, workerDoc, loreBlockHeading+"\n\n## ", 20)

	// The owner block must sit in slot 2 — ABOVE the cut — on the side the cut was
	// measured from. If it had drifted below, this equality could hold while the
	// two documents disagreed about where the owner's additions live.
	if o := strings.Index(staffRest, "T33-OWNER-CUSTOM-MARKER"); o < 0 || o > staffCut {
		t.Fatalf("使用者自訂 must sit in slot 2, above the persona (found at %d, cut at %d)",
			o, staffCut)
	}

	if workerRest != staffRest {
		t.Errorf("slots 1, 2 and 4 are NOT identical across the two assembly paths\n"+
			"outsource: %d bytes\nstaff:     %d bytes\n"+
			"這兩條組裝路徑各自寫自己的第 1、2、4 格，這一句是唯一會在它們分岔時變紅的東西。"+
			"⚠️ 它守的是【組裝結構】，不是 seed 的文字：改 seed 兩邊一起動，這一句不會紅。",
			len(workerRest), len(staffRest))
	}
}

// TestNeitherBootPathCarriesTheOtherScopesLore is the half the old equality could
// not express. Each path fills its own slot 3, and the failure this catches is a
// selection that reads the wrong scope KEY — which produces NO error, just a
// member reading somebody else's traditions or missing its own.
func TestNeitherBootPathCarriesTheOtherScopesLore(t *testing.T) {
	staffDoc, workerDoc, workerID := workerBootLoreFixture(t)

	if strings.Contains(staffDoc, "WORKER-SCOPED-MARKER") {
		t.Errorf("the STAFF boot document carries the OUTSOURCE member's 傳承 entry. " +
			"Both scopes are `agent` now, so the only thing separating them is the " +
			"scope KEY — this fires when the selection stopped honouring it.")
	}
	if strings.Contains(staffDoc, workerID) {
		t.Errorf("the STAFF boot document names outsource member %s — one member's "+
			"scope leaked into another's fold", workerID)
	}
	if strings.Contains(workerDoc, "STAFF-SCOPED-MARKER") {
		t.Errorf("the OUTSOURCE boot document carries the STAFF member's 傳承 entry. " +
			"A member reads its own 傳承 and nobody else's.")
	}
	// And the persona itself never crosses either: slot 3 on the worker path is
	// the lore block and nothing else.
	if strings.Contains(workerDoc, "# Role: ") {
		t.Errorf("the OUTSOURCE boot document carries the 角色說明 block — slot 3 on " +
			"this path is that member's own 傳承 and nothing else")
	}
}
