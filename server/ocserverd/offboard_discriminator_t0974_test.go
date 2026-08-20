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
//	an example instant pasted into §1's prose         → RED
//	the "?" placeholder restored in the notice        → RED
//	the deadline clause appended UNCONDITIONALLY      → RED
//
// ⚠️ THE THIRD ONE IS NARROWER THAN IT FIRST READ, and the narrower statement
// is the honest one: an independent review measured the mutant that actually
// resembles a mistake — dropping the `finalCall` half of `finalCall && deadline
// > 0`, keeping `deadline > 0` — and the whole package stayed GREEN, because
// nothing in the tree called offboardNotice with `finalCall=false` alongside a
// deadline. The production caller CAN: offboardNoticeFor passes
// refocusDeadlineOf(...) on both arms and lets the kind decide. That call is
// the one TestOffboardNotice_ASoftNoticeIgnoresADeadlineItWasHanded now makes,
// so the guard covers the mutation instead of only its cruder cousin.
//
// 🔴 AND ONE MORE THING THIS FILE DOES NOT GUARD, found by independent review:
// it reads §1 from the SEED (assetRoot("").readSeedFile), while what actually
// gets stapled to a notice is s.offboardText() — the boot DOCUMENT in the
// database, which the owner can rewrite through replace_offboard. The two agree
// only while nobody has overridden it, and this station's override has been
// cleared and reinstated more than once. So a green here is evidence about the
// factory text, NOT about the sentence a live agent receives; checking the live
// document needs a running station and is not attempted here.
//
// The fourth mutant STAYS GREEN, and it is the original defect itself:
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
	"time"
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
	// BOTH RUNTIMES, because they read DIFFERENT gauge keys and therefore need
	// two separate fallbacks in the source. Running this on claude alone is how
	// the codex arm went on printing "compaction round ?" through a whole
	// ticket that was ABOUT the question mark — an independent review measured
	// it: restoring the literal "?" on the codex arm left the entire package
	// green.
	//
	// ⚠️ WHOLE-STRING comparison (owner ruling 2026-08-20, c-2502de439aaa:
	// 「你如果要比對 context 就是比對一整份要一模一樣」). offboardNoticeFor is
	// deterministic and the document is empty on this server, so the complete
	// expected value is computable — and it pins the ABSENCE of the placeholder
	// together with everything else the sentence must carry, which two keyword
	// assertions could not.
	for _, c := range []struct {
		name    string
		runtime string
		want    string
	}{
		{
			name:    "claude omits the percentage it does not have",
			runtime: RuntimeClaude,
			want: "close-out (your limits: 40% / 50%) — start your close-out: " +
				"work the sequence below, then call restart_self yourself.",
		},
		{
			name:    "codex omits the round it does not have",
			runtime: RuntimeCodex,
			want: "close-out (your limits: round 3 / round 4) — start your close-out: " +
				"work the sequence below, then call restart_self yourself.",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := newReconcileTestServer(t)
			// The two runtimes' limits are set HERE rather than taken from the
			// test server's zero values, because codex's zero values render as
			// "round -1 / round 0" — pinning that would make the expected value
			// a bug report. The claude pair is the shipped default.
			s.codexNoticeRound, s.codexCompactionThreshold = 3, 4
			m := testAgent("m-nopct")
			m.Runtime = c.runtime
			m.RefocusSince = nowSecs()
			m.RefocusOp = refocusOpRefocus
			putTestMember(t, s, m)
			// NO gauge entry at all — which is the refocus arm's NORMAL state,
			// not an edge case: that arm is not triggered by a percentage or a
			// round count, so there has never been one to report.
			//
			// The offboard document is appended verbatim by offboardNotice and
			// is not what this test guards, so it is taken from the server
			// rather than restated — the assertion is about the SENTENCE.
			want := c.want + "\n" + s.offboardText()
			if got := s.offboardNoticeFor(m, offboardKindSoft); got != want {
				t.Fatalf("a missing position must be OMITTED, not printed as a "+
					"placeholder:\n got %q\nwant %q", got, want)
			}
		})
	}
}

// TestOffboardNotice_ASoftNoticeIgnoresADeadlineItWasHanded closes the gap the
// mutant table above names: the soft arm must drop a deadline it is GIVEN, not
// merely go without one because no caller supplies it.
//
// 🔴 WHY THIS CANNOT BE LEFT TO THE CALLER. offboardNoticeFor hands
// refocusDeadlineOf(...) to BOTH arms and lets `kind` decide, so a soft arm
// whose member happens to carry a live refocus epoch reaches this function with
// a positive deadline. If the guard degraded to `deadline > 0`, that member —
// the one the owner ruled has NO clock on it — would be told an instant it is
// not being collected at.
//
// ⚠️ IT COMPARES THE WHOLE STRING, NOT A KEYWORD. Owner ruling 2026-08-20
// (c-cdcaabeaf159 / c-2502de439aaa): 「你如果要比對 context 就是比對一整份要
// 一模一樣，比對部分的關鍵詞增加測試時間卻沒有得到我們想要的測試效果」. A
// substring assertion passes when the wording around it changed meaning and
// fails when a harmless rewording moved the same meaning; a whole-string
// comparison against a value built from the SAME inputs is a real assertion
// about what the function computes.
func TestOffboardNotice_ASoftNoticeIgnoresADeadlineItWasHanded(t *testing.T) {
	const (
		deadline = 1787000000.0
		where    = "context 30% (your limits: 55% / 65%)"
		doc      = "§1 …"
	)
	got := offboardNotice(where, offboardCloserRestartSelf, false, deadline, doc)
	want := where + " — start your close-out: work the sequence below, " +
		"then call " + offboardCloserRestartSelf + " yourself.\n" + doc
	if got != want {
		t.Fatalf("soft notice handed a deadline:\n got %q\nwant %q", got, want)
	}
	// Positive control on the same arguments: the clause is not simply
	// unreachable — flip `finalCall` and the whole sentence becomes the other
	// one, deadline and all.
	gotFinal := offboardNotice(where, offboardCloserRestartSelf, true, deadline, doc)
	wantFinal := where + " — offboard now: work the sequence below, " +
		"then call " + offboardCloserRestartSelf + " yourself." +
		" Your deadline is " + time.Unix(int64(deadline), 0).UTC().Format(time.RFC3339) +
		".\n" + doc
	if gotFinal != wantFinal {
		t.Fatalf("final call:\n got %q\nwant %q", gotFinal, wantFinal)
	}
}
