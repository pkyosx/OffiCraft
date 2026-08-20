package main

// offboard_discriminator_t0974_test.go — the rule that tells a SOFT offboard
// notice from a HARD one, applied to the notices themselves.
//
// 🔴 THE DEFECT THIS ANSWERS, AND WHY NOTHING CAUGHT IT (measured on the live
// station, 2026-08-20, by the shipping verifier — not a hypothetical):
//
// The document the server staples to every offboard notice opened with the
// discriminator, spelled out for the reader:
//
//	- 通知帶有 `Your deadline is ...`：**硬性**。
//	- 沒有這個標記：**軟性**。
//
// That sentence CONTAINS THE MARKER IT IS DESCRIBING, and the document rides
// inside the very notice being judged. So a SOFT notice — one with no clock on
// it at all — carries the marker once, in the line explaining the rule.
//
//	an agent applying the rule as written  ⇒  every soft notice reads as HARD
//
// The failure direction is the worst one available: "you have room, let your
// sub-agents finish" is read as "you have seconds left", and the agent drops
// in-flight work it was never out of time for. A generation of this developer
// wrote "硬性通知，剩不到一分鐘" into its own hand-off with NO instant quoted
// anywhere — an estimate, not a reading — and left four unanswered cards and
// two unreported delegations behind.
//
// Every existing test around this sentence asserted only that the HARD notice
// CONTAINS the clause. Not one of them ever applied the discriminator to the
// SOFT notice, so the rule was never once evaluated against the thing it
// judges. That is the shape of this whole class of bug: the rule and the rule's
// own statement live in one string, and that string is the object under test.
//
// ⇒ The fix is NOT an exception for "the line that explains the rule". A
// discriminator that needs a carve-out for its own description is matching the
// wrong thing. What actually separates the two notices is not the words — it is
// whether there is A CONCRETE INSTANT. The prose now says so, and this file
// pins it by EVALUATING the rule, not by quoting it.
//
// 🔴 WHAT THIS FILE DOES NOT GUARD — MEASURED, NOT ASSUMED.
//
// Four mutants were run against it. Three go red:
//
//	an example instant pasted into §1's prose        → RED
//	the "?" placeholder restored in the notice        → RED
//	the deadline clause attached to soft notices too  → RED
//
// The fourth one STAYS GREEN, and it is the original defect itself:
//
//	§1 reverted word-for-word to "通知帶有 `Your deadline is ...`：硬性"  → GREEN
//
// That is not an oversight to fix later. The rule this file evaluates is the
// CORRECTED one (marker + instant); the rule an AGENT executes is whatever the
// prose says, and prose is read, not run. A revert to a substring-shaped
// wording produces a document that still contains no instant — so every
// assertion here passes while every soft notice is once again misread.
//
// The tempting patch is to assert the prose does NOT contain a substring-test
// phrasing. That trades this hole for a worse one: a wording whitelist fires a
// FALSE RED on the next legitimate rewrite, and a guard that cries wolf is
// removed, after which nothing is guarded at all.
//
// ⇒ THE HONEST STATE: the §1 wording is held by review and by this comment,
// not by a test. If you are editing §1, the question to answer is not "do the
// tests pass" — it is "would this sentence match ITSELF when it is stapled
// inside the notice being judged?"

import (
	"regexp"
	"strings"
	"testing"
)

// discriminator is the rule from 下線程序 §1 expressed as something executable:
// the marker FOLLOWED BY an RFC3339 UTC instant. Deliberately not "contains the
// literal marker" — that is the defect — and deliberately not anchored to a
// line position either: tying it to "the first line" would trade a self-match
// bug for a layout dependency, and layout is the easier of the two to break by
// accident.
var discriminator = regexp.MustCompile(
	`Your deadline is \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)

// TestOffboardDiscriminator_AppliedToTheNoticesThemselves is the guard that was
// missing. It runs the rule over BOTH notices AND over the document alone.
func TestOffboardDiscriminator_AppliedToTheNoticesThemselves(t *testing.T) {
	doc, err := assetRoot("").readSeedFile(offboardSeedMD)
	if err != nil {
		t.Fatalf("read the 下線程序 seed: %v", err)
	}
	// DENOMINATOR FIRST. A comparison whose two sides are both empty reports
	// "the same" and tells you nothing; the shipping verifier lost a round to
	// exactly that (a shell-eaten sed extracted "" from both revisions and the
	// diff came back clean). So prove the seed is really here before drawing any
	// conclusion from what it does or does not contain.
	if len(doc) < 200 || !strings.Contains(doc, "## 1.") {
		t.Fatalf("the seed did not load — every assertion below would pass "+
			"vacuously (%d bytes)", len(doc))
	}

	const where = "context 62% (your limits: 60% / 75%)"
	const deadline = 1_787_000_000.0 // 2026-08-17T20:53:20Z

	soft := offboardNotice(where, offboardCloserRestartSelf, false, 0, doc)
	hard := offboardNotice(where, offboardCloserRestartSelf, true, deadline, doc)

	// ── the two that matter ──────────────────────────────────────────────────
	if discriminator.MatchString(soft) {
		t.Errorf("A SOFT notice must NOT read as hard. The rule matched %q "+
			"inside a notice that carries no clock at all — which is the "+
			"original defect: the line stating the rule is itself being "+
			"matched.\n\n%s",
			discriminator.FindString(soft), soft)
	}
	if !discriminator.MatchString(hard) {
		t.Errorf("A HARD notice must read as hard, and this one does not — so "+
			"the rule and the sentence have drifted apart:\n\n%s", hard)
	}

	// ── the regression itself, stated directly ───────────────────────────────
	// The document travels inside both notices. If the rule matches the
	// document ON ITS OWN, then it matches every notice that carries it, and
	// the discriminator is worthless no matter what the two cases above say.
	if discriminator.MatchString(doc) {
		t.Errorf("the 下線程序 document must not match the rule it states — "+
			"it rides inside EVERY notice, so a self-match makes every notice "+
			"read as hard. Found %q", discriminator.FindString(doc))
	}

	// The document must still SHOW the reader the marker; a rule the reader
	// cannot recognise on sight is not a usable rule. This is what stops the
	// fix from degenerating into "delete the marker from the prose", which
	// would pass every assertion above and leave the reader with nothing.
	if !strings.Contains(doc, "Your deadline is") {
		t.Error("the document must still name the marker — otherwise the " +
			"reader has an abstract rule and no way to apply it")
	}
}

// TestOffboardNotice_NoQuestionMarkForAMissingPercentage pins the other half of
// what the shipping verification found: the sentence used to carry a LITERAL
// "?" ("context ?% (your limits: 55% / 65%)") whenever the gauge held no
// context_pct — which is EVERY refocus-triggered close-out, because that arm is
// not fired by a percentage at all. The same file omits a whole clause rather
// than printing a placeholder everywhere else, and two spellings of "no value"
// in one output is the next reader's trap.
func TestOffboardNotice_NoQuestionMarkForAMissingPercentage(t *testing.T) {
	s := newReconcileTestServer(t)
	m := testAgent("m-nopct")
	m.Runtime = RuntimeClaude
	m.RefocusSince = nowSecs()
	m.RefocusOp = refocusOpRefocus
	putTestMember(t, s, m)
	// NO gauge entry at all — which is the refocus arm's NORMAL state, not an
	// edge case: that arm is not triggered by a percentage, so there has never
	// been one to report.
	notice := s.offboardNoticeFor(m, offboardKindSoft)

	if strings.Contains(notice, "?%") || strings.Contains(notice, "context ?") {
		t.Errorf("a missing percentage must not print a literal question mark; "+
			"omit the field instead:\n%s", notice)
	}
	// It must still say which band the reader is in — that is the part of the
	// clause carrying information, and dropping it too would be over-correcting.
	if !strings.Contains(notice, "your limits:") {
		t.Errorf("the limits must survive a missing percentage:\n%s", notice)
	}
}
