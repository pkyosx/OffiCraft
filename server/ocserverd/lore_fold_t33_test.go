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
func seedLoreDirectoryFixture(t *testing.T, s *apiServer) {
	t.Helper()
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
	body := t33Entry("me-probe")

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
			"short": body.Short, "symptoms": body.Symptoms, "falsify": body.Falsify,
			"instance": body.Instance, "residual_risk": body.ResidualRisk,
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

// TestDirectoryIsNotInTheSharedHead — the placement rule, asserted from the
// other side. workerSharedHead returns the SHARED SEED; this directory is
// per-actor, so it belongs in buildWorkerBootContext and nowhere else.
//
// TestWorkerSharedHeadMatchesUnfilteredSeedAssembly already turns red if the
// call moves there; this one says WHY in one line, and pairs the absence with
// the positive control that the assembled worker document does carry it.
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
