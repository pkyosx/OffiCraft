package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The three transport notices are a contract between TWO Go modules that cannot
// import each other: this one prints the lines, and cli/ocwarden's codex sidecar
// matches them with HasPrefix from column 0 before deciding whether a codex
// member ever hears about its own transport.
//
// 🔴 WHY A FILE-READING TEST AND NOT A SHARED CONSTANT. There is no shared
// package — `ocagent` and `ocwarden` are separate modules with no replace
// directive — so "spell it once" is not available, and the contract is
// physically two copies of the same bytes. Two copies with nothing tying them
// together is exactly the failure this test exists for: independent review
// changed ONE side and both suites stayed green while every codex member went
// silent about its transport for the rest of its session. Nothing anywhere
// would have reported it.
//
// So this asks the only question that spans the gap: do the heads this module
// PRINTS still exist, verbatim, in the file that CONSUMES them? It is a weaker
// statement than a compiler would make and it is the strongest one available
// here — and it is far stronger than the nothing that was there before.
//
// A rename on either side reddens this. A head that moves rightward is caught
// by the HasPrefix assertions in listen_notice_test.go, which read the bytes
// this listener really wrote.
func TestListenNoticePrefixesMatchTheSidecarConsumer(t *testing.T) {
	const consumer = "../ocwarden/codex_session.go"

	source, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatalf("cannot read the sidecar that consumes these prefixes (%s): %v — "+
			"if that file moved, this contract check moved with it and must be "+
			"repointed, not deleted", consumer, err)
	}
	text := string(source)

	for _, head := range []string{noticeDisconnected, noticeConnected, noticeGivingUp} {
		want := agentLinePrefix + head
		if !strings.Contains(text, `"`+want+`"`) {
			t.Errorf("%s no longer contains the literal %q.\n"+
				"This module prints that head; the codex sidecar matches it with "+
				"HasPrefix from column 0. Half of the contract has just been "+
				"changed: codex members will stop being told about this transport "+
				"event, silently, for the whole life of every session.", consumer, want)
		}
	}

	// ── THE FOURTH COPY ──────────────────────────────────────────────────────
	// The three heads above are the EXCEPTIONS. The rule they are carved out of
	// is the sidecar's blanket filter, whose head is the common prefix of every
	// transport line this binary prints — and that copy sat outside this list
	// until T-4, held up only indirectly by one behavioural case in ocwarden.
	//
	// Two claims, and both have to hold or the filter is deciding on bytes the
	// producer never emits:
	//   1. it really is the head of what THIS module prints, and
	//   2. the consumer still declares it, verbatim, as its filter head.
	//
	// Move it rightward on the sidecar side and the filter recognises no
	// transport line at all: every retry diagnostic starts becoming a turn on
	// the model, which is precisely the mid-outage noise the owner's ruling
	// (2026-08-30) exists to swallow.
	const transportHead = agentLinePrefix + "listen:"
	for _, head := range []string{noticeDisconnected, noticeConnected, noticeGivingUp} {
		if !strings.HasPrefix(agentLinePrefix+head, transportHead) {
			t.Fatalf("this module now prints %q, which does not start with the "+
				"blanket transport head %q that the sidecar filters on — the "+
				"exceptions and the rule have come apart on THIS side",
				agentLinePrefix+head, transportHead)
		}
	}
	// Matched as a DECLARATION, not as a bare substring: this head is quoted in
	// several prose comments in that same file, so `Contains` alone would keep
	// answering yes long after the code stopped using it.
	decl := regexp.MustCompile(`noticeTransportHead\s*=\s*` +
		regexp.QuoteMeta(`"`+transportHead+`"`))
	if !decl.MatchString(text) {
		t.Errorf("%s no longer declares noticeTransportHead = %q.\n"+
			"That constant is the head of the sidecar's blanket transport filter "+
			"and it is the fourth copy of these bytes across a module boundary "+
			"neither side can import. If it moved, the filter stops recognising "+
			"the lines this module actually prints and every retry diagnostic "+
			"becomes a turn on the model — silently, for the whole life of every "+
			"codex session.", consumer, transportHead)
	}
}

// ── THE ACK PROTOCOL'S TWO COPIES (T-48) ────────────────────────────────────
// The batch marker and the env var that turns the protocol on are the same
// physical two-copy problem as the notices above, with a worse failure mode:
// they do not merely go quiet, they go quiet in the direction of DATA LOSS.
//
//   - the marker head moves ⇒ the sidecar never recognises the end of a batch,
//     never answers, and every chat drain blocks forever on an ack that is not
//     coming. The member goes deaf and nothing reports it.
//   - the env var name moves ⇒ the listener never enters ack mode, goes back to
//     treating "printed" as "delivered", and every message the sidecar fails to
//     put in the model's conversation is marked read anyway — which is the whole
//     of what this protocol exists to prevent.
func TestAckProtocolLiteralsMatchTheSidecarConsumer(t *testing.T) {
	const consumer = "../ocwarden/codex_session.go"

	source, err := os.ReadFile(consumer)
	if err != nil {
		t.Fatalf("cannot read the sidecar half of the ack protocol (%s): %v", consumer, err)
	}
	text := string(source)

	// The marker must still be a line the sidecar's blanket transport filter
	// swallows — otherwise protocol chatter starts becoming turns on the model.
	if !strings.HasPrefix(agentLinePrefix+noticeBatch, agentLinePrefix+"listen:") {
		t.Fatalf("the batch marker %q no longer wears the transport head, so the "+
			"sidecar would forward it to the agent as content",
			agentLinePrefix+noticeBatch)
	}
	marker := regexp.MustCompile(`noticeBatchPrefix\s*=\s*` +
		regexp.QuoteMeta(`"`+agentLinePrefix+noticeBatch+` "`))
	if !marker.MatchString(text) {
		t.Errorf("%s no longer declares noticeBatchPrefix = %q.\n"+
			"This module prints that marker at the end of every gated chat batch "+
			"and then BLOCKS until the sidecar answers it. If the consumer stops "+
			"recognising it, no answer is ever sent and every drain hangs.",
			consumer, agentLinePrefix+noticeBatch+" ")
	}

	env := regexp.MustCompile(`listenAckEnv\s*=\s*` + regexp.QuoteMeta(`"`+listenAckEnv+`"`))
	if !env.MatchString(text) {
		t.Errorf("%s no longer declares listenAckEnv = %q.\n"+
			"That is the only signal this listener has that its stdout is being "+
			"carried by somebody else. Without it the listener silently returns to "+
			"marking undelivered messages read.", consumer, listenAckEnv)
	}
}

// ---------------------------------------------------------------------------
// "Pinned by TestX" is an ENTRANCE, and an entrance that leads nowhere is worse
// than none.
// ---------------------------------------------------------------------------

// A named test in a production comment is how the next reader confirms a
// guardrail is still standing before they touch the invariant it guards. When
// the test is renamed and the comment is not, the reader greps, finds nothing,
// and is left to decide for themselves whether the guardrail was deleted or
// merely moved — with the comment still asserting, confidently, that something
// holds this.
//
// That is not hypothetical: a681eac2 renamed the test that pins the watermark
// invariant and left reportChatRead pointing at the old name. Nothing reported
// it, because a comment compiles no matter what it says.
//
// SCOPE — this package only, by construction. Cross-module references (server
// comments naming an ocagent test) cannot be resolved from here, so they are
// out; \b is what keeps `newTestListener` and `refuseInTestBinary` from being
// read as references to tests. The six-character tail is the shortest name this
// package actually uses, and it keeps a bare `Test` out of the set.
func TestPinnedTestNamesInCommentsStillExist(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("cannot read the package directory: %v", err)
	}
	ref := regexp.MustCompile(`\bTest[A-Za-z0-9_]{6,}\b`)
	def := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\s*\(`)

	defined := map[string]bool{}
	type mention struct{ file, name string }
	var mentions []mention
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("cannot read %s: %v", name, err)
		}
		if strings.HasSuffix(name, "_test.go") {
			for _, m := range def.FindAllStringSubmatch(string(source), -1) {
				defined[m[1]] = true
			}
			continue
		}
		for _, m := range ref.FindAllString(string(source), -1) {
			mentions = append(mentions, mention{name, m})
		}
	}
	if len(mentions) == 0 {
		t.Fatal("no test names are cited in this package's production files at all — " +
			"either every citation was just deleted or this scan stopped matching; " +
			"an assertion over an empty set proves nothing")
	}
	for _, m := range mentions {
		if !defined[m.name] {
			t.Errorf("%s cites %s, which no test in this package defines. Either the "+
				"test was renamed and this comment was not (repoint it), or the "+
				"guardrail it claims is gone (say so). A reader greps this name to "+
				"check the invariant is still held.", m.file, m.name)
		}
	}
}
