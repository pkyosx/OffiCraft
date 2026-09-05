package main

// T-33 second round — the 對象目錄 as it reaches the two boot contexts.

import (
	"fmt"
	"strings"
	"testing"
)

// seedLoreDirectoryFixture files a small, deliberately MIXED
// directory: two entity types, a human-origin subject and agent-origin ones, so
// the grouping, the ordering and the human rule all have something to bite on.
//
// It is used by the boot-context equality guard in worker_spawn_test.go as well
// as by the tests below — one fixture, so "what a directory looks like" is not
// re-invented per test.
// enableLoreForTest turns the T-33 station-wide lore switch ON for one test
// server. It exists as a NAMED helper rather than a bare field assignment so
// every place that needs the feature on says the same sentence and is greppable
// — the day the switch grows a second dimension (per-role, say) there is one
// line to change, not fifteen.
//
// 🔴 THE DEFAULT IS OFF AND STAYS OFF. Nothing here weakens that: the shipped
// default is what lore_toggle_t33_test.go asserts, with controls. Calling this
// is a test SAYING OUT LOUD that it is about the feature's on-state.
func enableLoreForTest(s *apiServer) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.loreEnabled = true
}

func seedLoreDirectoryFixture(t *testing.T, s *apiServer) {
	t.Helper()
	// A station with a seeded directory and the feature switched off would fold
	// to nothing, and every assertion downstream would be about that absence.
	enableLoreForTest(s)
	t33Entity(t, s.dal, "en-seth", "human", "human:Seth")
	t33Entity(t, s.dal, "en-kyle", "agent", "agent:Kyle")
	t33Entity(t, s.dal, "en-repo", "repo", "repo:officraft")

	put := func(id, origin string, subjects ...string) {
		t.Helper()
		e := t33Entry(id)
		e.Origin = origin
		t33Put(t, s.dal, e)
		for _, sub := range subjects {
			if err := s.dal.PutLoreSubject(id, sub); err != nil {
				t.Fatalf("file %s against %s: %v", id, sub, err)
			}
		}
	}
	put("me-h1", "human:Seth", "en-seth", "en-repo")
	put("me-a1", "agent:Kyle", "en-kyle", "en-repo")
	put("me-a2", "agent:Kyle", "en-kyle")
}

// TestDirectoryIsAbsentWhenThereAreNoSubjects — the 使用者自訂 rule, applied to
// this block: nothing to say ⇒ the section does not exist at all, rather than a
// heading with nothing under it.
//
// Positive control: the SAME server with the fixture loaded does emit it, so
// this is not just asserting that some unrelated thing is broken.
func TestDirectoryIsAbsentWhenThereAreNoSubjects(t *testing.T) {
	s := newWorkerTestServer(t)
	got, err := s.foldLoreSection("m-anyone")
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if got != "" {
		t.Fatalf("an empty ontology must produce NO section, got %d bytes", len(got))
	}
	bc, err := s.buildBootContext("", nil)
	if err != nil || bc == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	if strings.Contains(bc.Context, loreSectionH1) {
		t.Error("boot context carries an empty 對象目錄 heading")
	}

	seedLoreDirectoryFixture(t, s)
	if got, err := s.foldLoreSection("m-anyone"); err != nil || got == "" {
		t.Fatalf("positive control: a seeded ontology must produce a section (err=%v, %d bytes)",
			err, len(got))
	}
}

// TestDirectoryCarriesNoEntryBody — the scope line of this whole round, as an
// assertion. The block names subjects and counts; not one cell of an entry's
// six body fields may ride along.
//
// The strings below are the fixture's ACTUAL body text (t33Entry), so this is
// checking for the real payload rather than for a placeholder that could never
// have appeared.
func TestDirectoryCarriesNoEntryBody(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s)

	// 🔴 這一支不能拿 t33Entry 的原文當搜尋標的，而它原本就是這樣做的。
	// t33Entry 的標題格逐字出現在 seeds/system_interaction.md 的「一條寫成的
	// 樣子」裡（owner 2026-09-04 於 rc-d4eec95e18ea 裁「開機檔只放一條最短的好
	// 例子」而落地）。開機檔本來就含 global context ⇒ 搜得到那句話是因為**說明
	// 文件在講它**，不是因為目錄洩漏了正文，而**兩者長得一模一樣**。
	// ⇒ 改用只可能從資料庫來的哨兵字串。這讓這道守衛變強不是變弱：命中就一定
	// 是洩漏，不可能是有人在文件裡引用了同一句話。
	body := t33Entry("me-probe")
	body.Content = "SENTINEL-CONTENT-4f1c9a"
	body.RetireWhen = "SENTINEL-RETIRE-4f1c9a"
	body.Impact = "SENTINEL-IMPACT-4f1c9a"
	// 🔴 標題格也是正文。v8 把它加進來的時候，它是最像「應該放進目錄」的一格——
	// 它就是為了給人讀而存在的。少了這個哨兵，哪天有人順手把標題折進目錄，這道
	// 守衛會全綠，而每一個成員的開機檔會開始多帶 N 條標題。
	body.Heading = "SENTINEL-HEADING-4f1c9a"
	t33Put(t, s.dal, body)
	if err := s.dal.PutLoreSubject("me-probe", "en-repo"); err != nil {
		t.Fatalf("file me-probe against en-repo: %v", err)
	}

	bc, err := s.buildBootContext("", nil)
	if err != nil || bc == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	worker, err := s.buildWorkerBootContext(
		OutsourceWorker{ID: "ow-body", Runtime: RuntimeClaude}, Task{ID: "t-1"}, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}
	// Positive control: the directory really is in both documents, so the
	// absences below are absences from a document that HAS the block.
	for name, doc := range map[string]string{"staff": bc.Context, "outsource": worker} {
		if !strings.Contains(doc, loreSectionH1) {
			t.Fatalf("%s boot context has no 對象目錄 — the checks below would be vacuous", name)
		}
		for field, text := range map[string]string{
			"content":     body.Content,
			"retire_when": body.RetireWhen, "impact": body.Impact,
			"heading": body.Heading,
		} {
			if strings.Contains(doc, text) {
				t.Errorf("%s boot context leaks an entry's %s cell — 這一段只放目錄，"+
					"一條條目的正文都不准出現", name, field)
			}
		}
	}
}

// seedManySubjects files n agent-origin subjects, plus one human-origin subject
// whose name sorts LAST in the whole set — so if the human rule were a mere
// ordering preference rather than a reservation, this one would be the first
// thing the cap threw away.
func seedManySubjects(t *testing.T, s *apiServer, n int) (humanCanonical string) {
	t.Helper()
	enableLoreForTest(s)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("en-bulk-%03d", i)
		t33Entity(t, s.dal, id, "agent", fmt.Sprintf("agent:zz-bulk-%03d", i))
		e := t33Entry(fmt.Sprintf("me-bulk-%03d", i))
		e.Origin = "agent:Kyle"
		t33Put(t, s.dal, e)
		if err := s.dal.PutLoreSubject(e.ID, id); err != nil {
			t.Fatalf("file bulk %d: %v", i, err)
		}
	}
	t33Entity(t, s.dal, "en-owner", "agent", "agent:zzzz-owner-said-this")
	e := t33Entry("me-owner")
	e.Origin = "human:Seth"
	t33Put(t, s.dal, e)
	if err := s.dal.PutLoreSubject(e.ID, "en-owner"); err != nil {
		t.Fatalf("file owner subject: %v", err)
	}
	return "agent:zzzz-owner-said-this"
}

// TestDirectoryAnnouncesItsOwnTruncation — a truncated list that does not SAY it
// is truncated is the silent loss this whole ticket is about: the reader has no
// way to tell "not listed" from "does not exist".
//
// The count is asserted too, not just the presence of a warning: a notice that
// says "some were dropped" without saying how many lets an order-of-magnitude
// overflow read the same as one missing row.
func TestDirectoryAnnouncesItsOwnTruncation(t *testing.T) {
	s := newWorkerTestServer(t)
	over := loreSubjectIndexMaxSubjects + 7
	seedManySubjects(t, s, over)

	got, err := s.foldLoreSection("m-reader")
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	lines := strings.Count(got, "\n- ")
	if lines >= over+1 {
		t.Fatalf("nothing was truncated (%d subject lines for %d subjects) — "+
			"the caps below are not being applied and this test proves nothing",
			lines, over+1)
	}
	omitted := over + 1 - lines
	want := loreTruncationLine(omitted)
	if !strings.Contains(got, want) {
		t.Fatalf("截斷了卻沒有印出那一行。少了：%q\n"+
			"a truncated directory MUST say so, with the number, or 「沒列到」 and "+
			"「不存在」 become indistinguishable", want)
	}
	// It must be readable by someone who reads the top of the section: directly
	// under the heading, ahead of every subject line, not filed at the bottom.
	notice := strings.Index(got, want)
	first := strings.Index(got, "\n- ")
	if first >= 0 && notice > first {
		t.Errorf("截斷訊號在第一個對象後面（notice=%d, first entry=%d）—— "+
			"它要在一定會被讀到的位置，不是附註", notice, first)
	}
}

// TestHumanOriginSubjectsSurviveTruncation — the hard rule, in the only shape
// that distinguishes a RESERVATION from a WEIGHTING: the human-origin subject is
// named so it sorts dead last, and the directory is overflowed well past both
// caps. Under any ranking scheme it would be gone; under the reservation it is
// present.
func TestHumanOriginSubjectsSurviveTruncation(t *testing.T) {
	s := newWorkerTestServer(t)
	humanCanonical := seedManySubjects(t, s, loreSubjectIndexMaxSubjects*3)

	got, err := s.foldLoreSection("m-reader")
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	// Positive control: this IS an overflowed directory, so surviving it means
	// something. Without this the assertion below passes on a directory that
	// dropped nothing.
	if !strings.Contains(got, "🔴 這份目錄被截斷了") {
		t.Fatal("the fixture did not overflow the caps — 「撐過截斷」 would be vacuous")
	}
	if !strings.Contains(got, humanCanonical) {
		t.Fatalf("human 來源的對象 %q 被截斷掉了。這是硬規則不是加權："+
			"origin 是 human: 開頭的條目所掛的對象，永遠先保留", humanCanonical)
	}
	// And the bulk really did lose rows, so the human survivor is not just
	// riding along in a directory where everything survived.
	if strings.Contains(got, "agent:zz-bulk-119") {
		t.Error("nothing was actually dropped; the cap is not being enforced")
	}
}

// TestDirectoryIsGroupedByTypeAndDeterministic pins the two shape promises: one
// group per entity type, and the same table producing the same bytes twice — a
// boot document that reshuffles itself makes every diff of it noise.
func TestDirectoryIsGroupedByTypeAndDeterministic(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s)

	got, err := s.foldLoreSection("m-reader")
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	for _, want := range []string{"## agent", "## human", "## repo"} {
		if !strings.Contains(got, want) {
			t.Errorf("directory is not grouped by type: missing %q", want)
		}
	}
	// The counts: repo:officraft carries two entries, agent:Kyle two,
	// human:Seth one. A directory whose numbers are wrong is worse than none.
	for _, want := range []string{
		"- agent:Kyle — 2 條", "- human:Seth — 1 條", "- repo:officraft — 2 條",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing subject line %q\n--- got ---\n%s", want, got)
		}
	}
	again, err := s.foldLoreSection("m-reader")
	if err != nil {
		t.Fatalf("fold again: %v", err)
	}
	if got != again {
		t.Error("two folds over an unchanged table produced different bytes")
	}
}

// t33EntityFlag sets one of `entity`'s two lifecycle columns after the fact.
// The seeding helper writes an entity in its normal state (approved, unmerged);
// these two states are what the roster query has to filter, so a test needs a
// way to put a row into them without a second, near-identical seeder.
func t33EntityFlag(t *testing.T, d *DAL, id, column, value string) {
	t.Helper()
	if _, err := d.wdb.Exec(
		`UPDATE entity SET `+column+` = ? WHERE id = ?`, value, id); err != nil {
		t.Fatalf("set %s=%s on %s: %v", column, value, id, err)
	}
}

// TestPendingSubjectsDoNotReachTheDirectory — `entity` takes agent writes with
// NO gate, so `pending = 1` is the whole review queue. A pending subject in the
// directory is an unreviewed name being published, as fact, into every agent's
// boot document on the station — the ontology asserting something no human has
// looked at.
//
// The approved subject is the positive control: it proves the filter is a filter
// and not a query that returns nothing.
func TestPendingSubjectsDoNotReachTheDirectory(t *testing.T) {
	s := newWorkerTestServer(t)
	t33Entity(t, s.dal, "en-reviewed", "repo", "repo:reviewed")
	t33Entity(t, s.dal, "en-unreviewed", "repo", "repo:unreviewed")
	t33EntityFlag(t, s.dal, "en-unreviewed", "pending", "1")

	for id, subject := range map[string]string{
		"me-reviewed": "en-reviewed", "me-unreviewed": "en-unreviewed",
	} {
		e := t33Entry(id)
		e.Origin = "agent:Kyle"
		t33Put(t, s.dal, e)
		if err := s.dal.PutLoreSubject(id, subject); err != nil {
			t.Fatalf("file %s against %s: %v", id, subject, err)
		}
	}

	got, err := s.foldLoreSection("m-reader")
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if strings.Contains(got, "repo:unreviewed") {
		t.Errorf("a pending entity reached the boot directory\n--- got ---\n%s", got)
	}
	if !strings.Contains(got, "repo:reviewed") {
		t.Fatalf("the approved subject is missing too — the assertion above proves "+
			"nothing, the directory is simply empty\n--- got ---\n%s", got)
	}
}

// TestMergedAwaySubjectsAreNotCountedTwice — nothing in this schema deletes, so
// a merged-away entity keeps existing. If the directory does not filter it, the
// same subject occupies TWO lines under two names: the count is wrong, and
// because the block is truncated, the duplicate also spends a slot a real
// subject needed.
func TestMergedAwaySubjectsAreNotCountedTwice(t *testing.T) {
	s := newWorkerTestServer(t)
	t33Entity(t, s.dal, "en-canonical", "repo", "repo:officraft")
	t33Entity(t, s.dal, "en-dupe", "repo", "repo:offi-craft")
	t33EntityFlag(t, s.dal, "en-dupe", "merged_into", "en-canonical")

	for id, subject := range map[string]string{
		"me-canonical": "en-canonical", "me-dupe": "en-dupe",
	} {
		e := t33Entry(id)
		e.Origin = "agent:Kyle"
		t33Put(t, s.dal, e)
		if err := s.dal.PutLoreSubject(id, subject); err != nil {
			t.Fatalf("file %s against %s: %v", id, subject, err)
		}
	}

	got, err := s.foldLoreSection("m-reader")
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if strings.Contains(got, "repo:offi-craft") {
		t.Errorf("a merged-away entity has its own line in the directory\n--- got ---\n%s", got)
	}
	if n := strings.Count(got, "- repo:officraft"); n != 1 {
		t.Errorf("the merge target appears %d times, want exactly 1\n--- got ---\n%s", n, got)
	}
}

// TestDirectoryIsNotInTheSharedHead — the placement rule, asserted from the
// other side. workerSharedHead returns the SHARED SEED; this directory is
// per-actor, so it belongs in buildWorkerBootContext and nowhere else.
//
// 🔴 THIS TEST IS THE ONLY GUARD, AND THE SENTENCE THAT USED TO STAND HERE
// SAID THE OPPOSITE. It read "TestWorkerSharedHeadMatchesUnfilteredSeedAssembly
// already turns red if the call moves there" — which is false, and it was false
// while TWO other comments (lore_fold.go's "⚠️ MEASURED" block and
// worker_spawn.go's "⚠️ Measured" note) already said so in as many words. That
// test's fixture seeds a user context and NOTHING ELSE, so the ontology is
// empty, this section folds to "" and moving the call changes nothing the
// equality can see.
//
// What makes the old sentence worse than a stale comment: it was a PERMISSION
// SLIP. Anyone deleting this test would have read "another test already covers
// it", deleted the only guard, and watched the suite stay green.
//
// This one says WHY in one line, and pairs the absence with the positive
// control that the assembled worker document does carry it.
func TestDirectoryIsNotInTheSharedHead(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s)

	head, err := s.workerSharedHead()
	if err != nil {
		t.Fatalf("workerSharedHead: %v", err)
	}
	if strings.Contains(head, loreSectionH1) {
		t.Error("對象目錄 is in workerSharedHead. That function's contract is the " +
			"SHARED SEED — bytes identical for every reader — and this directory is " +
			"per-actor. It belongs in buildWorkerBootContext.")
	}
	worker, err := s.buildWorkerBootContext(
		OutsourceWorker{ID: "ow-head", Runtime: RuntimeClaude}, Task{ID: "t-1"}, nil)
	if err != nil {
		t.Fatalf("buildWorkerBootContext: %v", err)
	}
	if !strings.Contains(worker, loreSectionH1) {
		t.Fatal("the assembled worker document has no 對象目錄 either — the absence " +
			"above is not a placement guarantee, it is a missing feature")
	}
	// And in the right place: after the shared head, before the 啟動步驟 tail.
	mem := strings.Index(worker, loreSectionH1)
	boot := strings.Index(worker, bootSequenceH1)
	if boot < 0 || mem > boot {
		t.Errorf("對象目錄 must sit at the tail of slot 3, before 啟動步驟 "+
			"(目錄=%d 啟動步驟=%d)", mem, boot)
	}
}

// TestRetiringTakesTheSubjectOutOfTheBootDirectoryAndRevivingPutsItBack is the
// test that connects the governance seam to the thing it is FOR.
//
// 🔴 THE DAL-LEVEL TESTS DO NOT COVER THIS AND IT WOULD BE EASY TO THINK THEY
// DO. They prove `status` moves and that the by-subject reads exclude retired
// rows. What a boot context actually reads is a THIRD query
// (ListLoreSubjectRoster, via foldLoreSection) with its own WHERE clause — so
// "retiring stops it reaching anyone" was, until this test, a claim about a
// query nothing asserted. Retirement whose only effect is a column change is
// exactly the "looks done, changes nothing" failure this ticket exists to kill.
func TestRetiringTakesTheSubjectOutOfTheBootDirectoryAndRevivingPutsItBack(t *testing.T) {
	s := newWorkerTestServer(t)
	seedLoreDirectoryFixture(t, s)

	before, err := s.foldLoreSection("m-anyone")
	if err != nil {
		t.Fatalf("fold before: %v", err)
	}
	if !strings.Contains(before, "agent:Kyle") {
		t.Fatalf("fixture did not put agent:Kyle in the directory:\n%s", before)
	}

	// agent:Kyle carries exactly the two entries the fixture filed against it, so
	// retiring both is what empties the subject out of the directory. Retiring
	// only one would leave the subject present with a smaller count — a weaker
	// assertion that a broken WHERE clause could still pass.
	for _, id := range []string{"me-a1", "me-a2"} {
		if err := s.dal.RetireLoreEntry(id, LoreRetireExpired, "agent:O-197", "", false, 200); err != nil {
			t.Fatalf("retire %s: %v", id, err)
		}
	}
	after, err := s.foldLoreSection("m-anyone")
	if err != nil {
		t.Fatalf("fold after retire: %v", err)
	}
	if strings.Contains(after, "agent:Kyle") {
		t.Fatalf("retired entries still reach a boot context:\n%s", after)
	}
	// Control: the subject that was NOT retired is still there, so the assertion
	// above is about retirement and not about the directory having gone blank.
	if !strings.Contains(after, "human:Seth") {
		t.Fatalf("the whole directory vanished, so the check above proves nothing:\n%s", after)
	}

	if err := s.dal.ReviveLoreEntry("me-a1", "owner", "the situation came back", true, 300); err != nil {
		t.Fatalf("revive: %v", err)
	}
	back, err := s.foldLoreSection("m-anyone")
	if err != nil {
		t.Fatalf("fold after revive: %v", err)
	}
	if !strings.Contains(back, "agent:Kyle") {
		t.Fatalf("revive did not put the subject back — retirement is one-way in practice:\n%s", back)
	}
}
