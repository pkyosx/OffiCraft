package main

// worker_crossref_test.go — T-108b follow-up.
//
// TWO guards live here, and they are in their OWN FILE on purpose.
//
// Guard 1 — dangling cross-references. "Excluded sections" and "in-document §N
// cross-references" are two different things. The original T-108b work handled
// only the first: it removed §9 / §5.1 / §10.1 / §10.1b / §10.1c from the
// worker's copy, but left the POINTERS to them alive elsewhere in the text. The
// FAIL-CLOSED anchor guard cannot see this — the anchors still resolve; there is
// just one more pointer than before. So the next time upstream adds a sentence
// pointing at an excluded section, nothing catches it. This guard does.
//
// Guard 2 — commit coupling. The two T-108b commits (ec3cdce docs, 8153aa3 code)
// are NOT independently revertible, and the failure was SILENT in one direction:
// reverting the code commit alone still built, deleted its own tests along with
// itself, and dropped the worker context from 58,636 to 7,104 bytes with the
// risk language back to zero — worse than before the ticket, with nothing red.
// A guard that lives in worker_sharedcore_test.go cannot catch that, because
// that file IS part of the commit being reverted. This file is not part of
// either commit, so it survives a revert of either one and goes red.

import (
	"regexp"
	"strings"
	"testing"
)

// crossrefWorkerCtx deliberately DUPLICATES workerCtx from
// worker_sharedcore_test.go instead of calling it.
//
// That duplication is the entire point of Guard 2. worker_sharedcore_test.go is
// part of commit 8153aa3; reverting that commit deletes it. If this file called
// its helper, a revert would break COMPILATION here — which looks loud, but the
// obvious way to make a build error go away is to delete the file that no longer
// compiles, and the guard dies with it. Depending only on symbols that predate
// T-108b (newWorkerTestServer, putWorkerFixture, putTaskFixture,
// buildWorkerBootContext) means a revert leaves this file COMPILING and going
// red on the assertion itself — a failure that states what is actually wrong.
func crossrefWorkerCtx(t *testing.T) string {
	t.Helper()
	s := newWorkerTestServer(t)
	w := OutsourceWorker{ID: "ow-t108b-xref", Codename: "O-8", Model: "sonnet", Effort: "medium"}
	task := Task{ID: "tk-t108b-xref", Title: "T-108b crossref fixture", TypeKey: "general",
		Priority: "mid", ExecutorKind: TaskExecutorOutsource, ExecutorID: w.ID}
	putWorkerFixture(t, s, w)
	putTaskFixture(t, s, task)
	ctx, err := s.buildWorkerBootContext(w, task, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}
	return ctx
}

// crossrefMemberCtx is the member-side equivalent, decoupled for the same reason.
func crossrefMemberCtx(t *testing.T) string {
	t.Helper()
	s := newWorkerTestServer(t)
	bc, err := s.buildBootContext("", nil, "")
	if err != nil || bc == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	return bc.Context
}

// sectionRefRe matches an in-document pointer: §0, §4.1, §8b, §10.1c.
var sectionRefRe = regexp.MustCompile(`§(\d+(?:\.\d+)?[a-z]?)`)

// sectionHeadingRe matches the id of an ATX heading: "## 0. …", "### 8b. …",
// "### 10.1c …", "### 5.1 …".
var sectionHeadingRe = regexp.MustCompile(`^#{1,6}\s+(\d+(?:\.\d+)?[a-z]?)[.\s]`)

// appendixRefRe / appendixHeadingRe do the same for 附錄 A / 附錄 B.
var appendixRefRe = regexp.MustCompile(`附錄 ([A-Z])`)
var appendixHeadingRe = regexp.MustCompile(`^#{1,6}\s+附錄 ([A-Z])`)

// headingIDs collects every section id a reader of doc can actually navigate to.
func headingIDs(doc string) map[string]bool {
	ids := map[string]bool{}
	for _, line := range strings.Split(doc, "\n") {
		if m := sectionHeadingRe.FindStringSubmatch(line); m != nil {
			ids[m[1]] = true
		}
		if m := appendixHeadingRe.FindStringSubmatch(line); m != nil {
			ids["附錄 "+m[1]] = true
		}
	}
	return ids
}

// refIDs collects every pointer the doc makes.
func refIDs(doc string) map[string]bool {
	refs := map[string]bool{}
	for _, m := range sectionRefRe.FindAllStringSubmatch(doc, -1) {
		refs[m[1]] = true
	}
	for _, m := range appendixRefRe.FindAllStringSubmatch(doc, -1) {
		refs["附錄 "+m[1]] = true
	}
	return refs
}

// dangling returns the pointers in doc that resolve to nothing in doc.
func dangling(doc string) []string {
	ids := headingIDs(doc)
	var out []string
	for ref := range refIDs(doc) {
		if !ids[ref] {
			out = append(out, ref)
		}
	}
	return out
}

// TestWorkerBootContextHasNoDanglingSectionRefs is the regression guard the
// original change was missing: EVERY §N / 附錄 X pointer the worker can read
// must resolve to a heading the worker can actually reach.
//
// This is deliberately stated as a closure property of the assembled document
// rather than as a blacklist of today's excluded section numbers. A blacklist
// would need editing every time the exclusion list changes; this does not, and
// it also catches plain typos and upstream renames.
func TestWorkerBootContextHasNoDanglingSectionRefs(t *testing.T) {
	ctx := crossrefWorkerCtx(t)

	// Positive control FIRST: a bare "grep found nothing" is worthless if the
	// extraction is broken. Prove both halves of the machinery actually fire on
	// this document before trusting the negative assertion below.
	if got := len(refIDs(ctx)); got < 5 {
		t.Fatalf("ref extraction looks broken: only %d §N refs found in the worker "+
			"context — the negative assertion below would be vacuously true", got)
	}
	if got := len(headingIDs(ctx)); got < 5 {
		t.Fatalf("heading extraction looks broken: only %d headings found in the "+
			"worker context — every ref would falsely look dangling", got)
	}
	// And prove the resolver rejects what it should: a synthetic pointer to a
	// section that is definitely excluded must be reported as dangling.
	if d := dangling(ctx + "\n\n見 §10.1c 的發包流程。\n"); len(d) == 0 {
		t.Fatal("resolver has no teeth: an injected pointer to the excluded §10.1c " +
			"was not reported as dangling")
	}

	if d := dangling(ctx); len(d) > 0 {
		t.Errorf("worker boot context points at sections it does not contain: %v\n"+
			"每一個 §N 指標都必須在外包讀得到的同一份文件裡解析得到。"+
			"指向被排除章節的指標不是「被後面覆寫」，是壞掉的指示。", d)
	}
}

// TestMemberBootContextHasNoDanglingSectionRefs is the paired control. If the
// member fold — which excludes nothing — also had dangling refs, the worker
// assertion above would be measuring a pre-existing defect in the seed rather
// than anything T-108b did.
func TestMemberBootContextHasNoDanglingSectionRefs(t *testing.T) {
	if d := dangling(crossrefMemberCtx(t)); len(d) > 0 {
		t.Errorf("member boot context has dangling refs %v — the worker-side "+
			"assertion cannot attribute anything to the worker exclusions until "+
			"this baseline is clean", d)
	}
}

// ── Guard 2: the two T-108b commits must move together ───────────────────────

// riskLanguageFloor lists the safety vocabulary that reaching an outsource
// worker IS the motivation for T-108b. Measured on base (42ea399) it was zero
// for all of these; the worker is the only role authorised to do destructive
// work itself.
var riskLanguageFloor = []string{
	"安全邊界",
	"成本剎車",
	"backup-before-destructive",
	"verify-before-assert",
}

// TestWorkerBootContextRevertCoupling fails loudly if EITHER T-108b commit is
// reverted without the other.
//
// Why this is a test and not a note in the commit message: the observed failure
// mode was silent. Reverting 8153aa3 alone removed worker_sharedcore.go AND
// worker_sharedcore_test.go together, so every assertion about the shared core
// vanished with the thing it was asserting about — `go build` passed and no
// test went red, while the worker silently dropped to a state strictly worse
// than before the ticket. Tests that ship inside the commit under test cannot
// guard that commit. This file ships separately.
//
// It is intentionally a floor, not an equality: it must not become a
// change-detector that has to be re-baselined on every seed edit.
func TestWorkerBootContextRevertCoupling(t *testing.T) {
	ctx := crossrefWorkerCtx(t)

	// The overlay alone (the ec3cdce-only state) was 7,104 bytes; base was
	// 15,279. Anything near either number means the shared core is not being
	// folded in and the "differences overlay" is pointing at a document the
	// worker cannot see.
	const floor = 30000
	if len(ctx) < floor {
		t.Fatalf("worker boot context is %d bytes, want >= %d.\n"+
			"這幾乎一定表示共用核心（Global Context 三塊）沒有被組進來——"+
			"也就是有人只退了 T-108b 兩顆 commit 的其中一顆。"+
			"這兩顆必須一起進退：差異覆寫段假設共用核心就在它上面。",
			len(ctx), floor)
	}

	for _, kw := range riskLanguageFloor {
		if !strings.Contains(ctx, kw) {
			t.Errorf("worker boot context lost risk language %q — this is the whole "+
				"safety motivation of T-108b (measured zero on base 42ea399). "+
				"若這條紅了而 context 長度沒紅，先確認共用核心是否被部分排除掉。", kw)
		}
	}

	// The overlay must still be there too — the other revert direction.
	if !strings.Contains(ctx, "外包工作者 —— 你與正職成員的差異") {
		t.Error("worker boot context lost the outsource overlay — the shared core " +
			"alone tells a worker to do member-only things")
	}
}
