// One runtime's boot document must never carry the OTHER runtime's listener
// instruction (T-51b0, restored on the T-99a6 review).
//
// 🔴 WHY THIS ONE CAME BACK WHEN THE REST OF THAT FILE DID NOT. The owner's
// ruling (2026-08-15) was that guards pinning HIS WORDING are worthless: he
// rewrites those documents by hand and every edit turned tests red with nothing
// broken. That ruling stands, and the four wording guards stay deleted.
//
// This assertion is a different kind. It pins no phrasing and forbids no way of
// writing anything — it only says a document must not contain the INSTRUCTION
// belonging to the OTHER runtime, which is a fact about the runtimes, not about
// prose. The review demonstrated the gap by pasting codex's "the sidecar owns
// the listener, do not start it yourself" into the CLAUDE seed: the whole
// ocserverd suite stayed green. A claude agent reading that hands control back
// to a sidecar that does not exist for it, and then never comes online — and an
// agent that never comes online cannot report that anything is wrong.
//
// 🔴 BOTH SIDES PROBE SENTENCES, NEVER A BARE WORD. The first version of this
// file banned the bare word "sidecar" from the claude seed, and review caught
// what that costs: 「claude 這側沒有 sidecar，listener 是你自己掛的」 is a
// CORRECT sentence — accurate, useful, and exactly what a reader needs — and it
// would have turned this test red. A guard that reddens on a true sentence in
// the owner's own document is the very thing his ruling was about; the second
// such false alarm is when he stops believing any of them. The exclusions below
// name instruction-shaped phrases, which is why a negation of them stays green
// (pinned by the legitimate-negation case in the table below, which is a control
// on THIS guard, not on the seeds).
//
// The sibling guard in worker_spawn_test.go is NOT this: it checks that the
// right FILE reaches the right runtime. It is alive (flipping the runtime→seed
// mapping reds a dozen assertions) and it says nothing about what is written
// inside the file it delivered.
package main

import (
	"strings"
	"testing"
)

// codexOnlyInstructions are the sentences that only make sense to a session
// whose lifecycle a sidecar owns. A claude session has no sidecar, so any of
// these reaching its document arrived by being copied from the other one.
var codexOnlyInstructions = []string{
	"把控制權交回 sidecar",
	"不要自己啟動 `ocagent listen`",
	"sidecar 持有你的生命週期",
	"它掛好之後會再叫你一次",
}

// claudeOnlyInstructions are the reverse: claude's own positive order to mount
// the listener. Codex's document legitimately names Monitor inside a
// PROHIBITION, which is why the probe is the instruction and not the bare word.
var claudeOnlyInstructions = []string{
	"用內建 Monitor 工具在背景掛住",
	"在背景掛住 `ocagent listen`",
}

// crossRuntimeSeedViolations returns one message per instruction that landed in
// the wrong document. Split out from the seed test so the guard itself can be
// driven with documents that are NOT the shipped seeds — including the
// legitimate negation that must stay green.
func crossRuntimeSeedViolations(claude, codex string) []string {
	var out []string
	for _, codexOnly := range codexOnlyInstructions {
		if strings.Contains(claude, codexOnly) {
			out = append(out, "claude/"+codexOnly)
		}
	}
	for _, claudeOnly := range claudeOnlyInstructions {
		if strings.Contains(codex, claudeOnly) {
			out = append(out, "codex/"+claudeOnly)
		}
	}
	return out
}

func missingInstructions(document, variableName string, instructions []string) []string {
	var out []string
	for _, instruction := range instructions {
		if !strings.Contains(document, instruction) {
			out = append(out, variableName+"/"+instruction)
		}
	}
	return out
}

func TestNeitherBootSeedCarriesTheOtherRuntimesListenerInstruction(t *testing.T) {
	claude, err := assetRoot("").readSeedFile("boot_sequence.md")
	if err != nil {
		t.Fatalf("read boot_sequence.md: %v", err)
	}
	codex, err := assetRoot("").readSeedFile("boot_sequence_codex.md")
	if err != nil {
		t.Fatalf("read boot_sequence_codex.md: %v", err)
	}

	// Positive control FIRST: each document must still carry ITS OWN
	// instruction, so the absences below cannot be satisfied by a pair of
	// documents that say nothing about the listener at all.
	if !strings.Contains(claude, "Monitor") {
		t.Fatal("the claude seed no longer tells its reader to hold the listener " +
			"under Monitor — the exclusions below would then be vacuous")
	}
	if !strings.Contains(codex, "sidecar") {
		t.Fatal("the codex seed no longer mentions the sidecar that owns its " +
			"listener — the exclusions below would then be vacuous")
	}

	// And the probes must still MATCH the shipped documents. Without this, an
	// owner rewrite that rephrases every sentence leaves a guard that forbids
	// text nobody writes any more: permanently green, permanently useless, and
	// indistinguishable from a guard that is working.
	missing := append(
		missingInstructions(codex, "codexOnlyInstructions", codexOnlyInstructions),
		missingInstructions(claude, "claudeOnlyInstructions", claudeOnlyInstructions)...,
	)
	if len(missing) > 0 {
		t.Fatalf("swapping the two documents did NOT flag every probe: the "+
			"probes have drifted away from what the seeds actually say. "+
			"missing %v", missing)
	}

	if got := crossRuntimeSeedViolations(claude, codex); len(got) > 0 {
		t.Errorf("a boot document carries the OTHER runtime's listener "+
			"instruction: %v. Under claude the session hangs `ocagent listen` "+
			"itself, so a reader that hands control back to a sidecar waits "+
			"forever for something that will never call it; under codex two "+
			"listeners on one identity is what the server refuses with 409, so "+
			"the second one never connects. Either way the agent never comes "+
			"online and reports nothing.", got)
	}
}

func TestMissingInstructionsNamesOnlyTheMissingProbe(t *testing.T) {
	missing := missingInstructions(
		strings.Join(codexOnlyInstructions[:len(codexOnlyInstructions)-1], "\n"),
		"codexOnlyInstructions",
		codexOnlyInstructions,
	)
	want := []string{"codexOnlyInstructions/" + codexOnlyInstructions[len(codexOnlyInstructions)-1]}
	if strings.Join(missing, "\n") != strings.Join(want, "\n") {
		t.Fatalf("missing instructions = %v, want %v", missing, want)
	}
}

func TestCrossRuntimeSeedViolationsDoesNotFireOnATrueSentence(t *testing.T) {
	// The control this guard exists to keep: describing the OTHER runtime
	// accurately is not contamination. Both of these are correct sentences that
	// an earlier bare-word probe would have reddened.
	for _, legitimate := range []string{
		"claude 這側沒有 sidecar，listener 是你自己掛的。",
		"這個 runtime 不像 codex 那樣由 sidecar 持有生命週期；SSE 由你自己掛。",
	} {
		if got := crossRuntimeSeedViolations(legitimate, ""); len(got) > 0 {
			t.Errorf("a TRUE sentence about the other runtime was flagged as "+
				"contamination: %q → %v. A guard that reddens on correct prose "+
				"in the owner's own document is what his 2026-08-15 ruling was "+
				"about.", legitimate, got)
		}
	}

	// …and it still catches the real thing, so the case above is not passing
	// merely because the checker never fires.
	contaminated := "3. 掛上 SSE: 結束你目前這一輪、把控制權交回 sidecar，由它掛上 SSE。"
	if got := crossRuntimeSeedViolations(contaminated, ""); len(got) == 0 {
		t.Error("the checker did not flag codex's own instruction pasted into " +
			"the claude document — the negative cases above prove nothing")
	}
}
