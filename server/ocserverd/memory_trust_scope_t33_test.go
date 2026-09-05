package main

// T-33 — the fail-closed wall in front of the trust class.
//
// The property these tests defend is NOT "the mapping table is correct" — that
// table is provisional and a wrong entry in it is a hole nothing here can see.
// It is the narrower and more durable one: an action name nobody has classified
// must land in the STRICTEST class and must SAY SO. Both halves are asserted,
// because a fail-closed rule that nobody can observe is indistinguishable from a
// permissive one that happens to be quiet.

import (
	"reflect"
	"testing"
)

// 🔴 THE MUTANT TEST. Change the fail-closed branch in memoryTrustScope from
// TrustScopeTrust to TrustScopeMethod and this fails. That edit is the tempting
// one — `method` is what makes an unfamiliar entry appear where a reader expects
// it — and it silently removes the cross-subject wall for every action name
// invented after this file was written.
func TestMemoryTrustScopeUnknownActionFailsClosedToTrust(t *testing.T) {
	got := memoryTrustScope([]string{"negotiate-with-vendor"})
	if got.Scope != TrustScopeTrust {
		t.Fatalf("an unrecognised action must fail closed to %q, got %q",
			TrustScopeTrust, got.Scope)
	}
	if !got.FellBack() {
		t.Fatalf("an unrecognised action must be reported as a fall-back, verdict = %+v", got)
	}
	if !reflect.DeepEqual(got.Unmapped, []string{"negotiate-with-vendor"}) {
		t.Fatalf("Unmapped = %v, want the one unrecognised name", got.Unmapped)
	}
}

// A recognised method action must NOT fail closed — otherwise the test above
// would still pass with a function that returns `trust` unconditionally, and the
// wall would be indistinguishable from a wall around everything.
func TestMemoryTrustScopeKnownMethodActionIsNotAFallBack(t *testing.T) {
	got := memoryTrustScope([]string{"deploy"})
	if got.Scope != TrustScopeMethod {
		t.Fatalf("Scope = %q, want %q", got.Scope, TrustScopeMethod)
	}
	if got.FellBack() {
		t.Fatalf("a recognised action must not report a fall-back, Unmapped = %v", got.Unmapped)
	}
}

// One unknown name poisons the whole entry, even alongside recognised ones.
// The alternative — "it had a known action, so trust that one" — hands any
// writer a one-word bypass: add `deploy` and the unclassified name stops
// mattering.
func TestMemoryTrustScopeOneUnknownActionOutranksKnownOnes(t *testing.T) {
	got := memoryTrustScope([]string{"deploy", "assume", "vibe-check"})
	if got.Scope != TrustScopeTrust {
		t.Fatalf("Scope = %q, want %q — an unknown name must not be outvoted",
			got.Scope, TrustScopeTrust)
	}
	if !reflect.DeepEqual(got.Unmapped, []string{"vibe-check"}) {
		t.Fatalf("Unmapped = %v, want only the unrecognised name", got.Unmapped)
	}
}

// Strictest-wins among RECOGNISED names, for the same reason.
func TestMemoryTrustScopeTrustBeatsMethod(t *testing.T) {
	got := memoryTrustScope([]string{"deploy", "delegate"})
	if got.Scope != TrustScopeTrust {
		t.Fatalf("Scope = %q, want %q", got.Scope, TrustScopeTrust)
	}
	if got.FellBack() {
		t.Fatalf("both names are recognised; this must not report a fall-back: %v", got.Unmapped)
	}
}

// Cognitive survives on its own — it is the class that switches the staleness
// judgement off, so collapsing it into method would quietly re-enable ageing on
// entries that do not age.
func TestMemoryTrustScopeCognitiveAloneStaysCognitive(t *testing.T) {
	got := memoryTrustScope([]string{"assume", "infer"})
	if got.Scope != TrustScopeCognitive {
		t.Fatalf("Scope = %q, want %q", got.Scope, TrustScopeCognitive)
	}
}

// An entry that names no action at all has not been classified, and "not
// classified" is an unknown like any other — so it fails closed too. It reports
// NO unmapped names, because there were none to report: there is nothing for a
// human to go and map.
func TestMemoryTrustScopeEmptyActionsFailsClosed(t *testing.T) {
	for _, actions := range [][]string{nil, {}, {"", "   "}} {
		got := memoryTrustScope(actions)
		if got.Scope != TrustScopeTrust {
			t.Fatalf("actions %v: Scope = %q, want %q", actions, got.Scope, TrustScopeTrust)
		}
		if len(got.Unmapped) != 0 {
			t.Fatalf("actions %v: Unmapped = %v, want none", actions, got.Unmapped)
		}
	}
}

// Names are matched case- and whitespace-insensitively, and a name repeated in
// the input is reported once. A Health screen that lists "Deploy", "deploy " and
// "deploy" as three separate unmapped names is a screen nobody reads.
func TestMemoryTrustScopeNormalisesAndDeduplicates(t *testing.T) {
	if got := memoryTrustScope([]string{" Deploy "}); got.Scope != TrustScopeMethod || got.FellBack() {
		t.Fatalf("a padded, capitalised known name must still be recognised: %+v", got)
	}
	got := memoryTrustScope([]string{"Vibe-Check", "vibe-check", " vibe-check"})
	if !reflect.DeepEqual(got.Unmapped, []string{"vibe-check"}) {
		t.Fatalf("Unmapped = %v, want one normalised entry", got.Unmapped)
	}
}
