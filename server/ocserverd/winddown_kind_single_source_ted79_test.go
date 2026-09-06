package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// winddown_kind_single_source_ted79_test.go — the clock and the sentence are
// ONE judgement, asserted through the two readers rather than through the
// judgement function, so it stays red whichever half somebody edits alone.
//
// The two halves are separately reachable, and each half alone is a distinct
// harm:
//
//   - a clock with no sentence: the agent is told to work its sequence with no
//     countdown, and is cut off mid-hand-off with no warning at all;
//   - a sentence with no clock: the agent is told it has until an instant that
//     nothing is counting, so it cuts a hand-off short to beat a deadline that
//     does not exist.
//
// Both were reachable while recycleGraceFor and offboardKindOf each carried
// their own copy of the list (T-ed79).

// everyWindDownCause is the closed set of refocus_op values plus one value no
// constant names. The unknown op is the load-bearing case for "final is the
// POSITIVE condition": under the old fallthrough it would have been clocked,
// which is how a cause nobody ruled on ends up carrying a deadline.
//
// 🔴 This list only guards what it NAMES. It shipped missing
// refocusOpContextNotice — the one value T-ed79 itself added — and with that
// hole a mutant that gave the FIRST context threshold an unannounced 120 s
// deadline (`if refocusOp == refocusOpContextNotice { return cfg.RecycleGrace,
// true }` at the top of recycleGraceFor) passed BOTH tests below. A closed set
// that lags the constants it is closing over is not a guard. Every constant in
// member_ownerop_winddown.go's two const blocks belongs here; add the value in
// the same edit that adds the constant. ownerOpRestart ("restart") is in for a
// different reason: it WAS the one verb the (now deleted, T-65 包④)
// ownerOpDisplacesTheSession deny-list named
// out of the wind-down, so it never lands in refocus_op today — but that
// deny-list is documented as "a verb added later gets the wind-down by
// default", so the day it stops being denied the ruling must already be pinned.
var everyWindDownCause = []string{
	refocusOpRefocus,
	refocusOpRestartSelf,
	refocusOpContextHigh,
	refocusOpContextNotice,
	refocusOpTokenExpiry,
	refocusOpAcceleratedStop,
	memberOpRelocate,
	memberOpModel,
	ownerOpRestart,
	"an_op_no_constant_names",
}

func TestWindDownKind_TheClockAndTheSentenceCannotDisagree(t *testing.T) {
	cfg := defaultReconcileConfig()

	for _, op := range everyWindDownCause {
		m := testAgent("m-ed79-kind")
		m.RefocusSince = 1_000_000.0
		m.RefocusOp = op

		kind, carries := offboardKindOf(m, m.RefocusSince+1)
		if !carries {
			t.Fatalf("%s: a member with a refocus epoch must carry a notice at all", op)
		}
		grace, clocked := recycleGraceFor(op, cfg)
		sentenceSaysFinal := kind == offboardKindFinal

		if sentenceSaysFinal != clocked {
			t.Fatalf("%s: the sentence says kind=%q and the clock says clocked=%v — "+
				"one of them is lying to the agent", op, kind, clocked)
		}
		// The wire field the cockpit renders comes off the same judgement: a
		// countdown on screen that the reconcile tick has no intention of
		// honouring is the same split seen from the owner's side.
		deadline := refocusDeadlineOf(m.RefocusSince, cfg, op)
		if (deadline > 0) != clocked {
			t.Fatalf("%s: clocked=%v but refocus_deadline=%v", op, clocked, deadline)
		}
		if clocked && deadline != m.RefocusSince+grace {
			t.Fatalf("%s: deadline %v is not the grace the tick collects on (%v)",
				op, deadline, m.RefocusSince+grace)
		}
	}
}

// The ruling itself, stated as membership: 加速停止 has exactly TWO causes —
// the SECOND context threshold, and the owner pressing the button (T-ed79,
// 「停止 → 加速停止 → 強制停止」). Every other cause — the owner's other verbs,
// the agent's own restart_self, the FIRST context threshold, token expiry — is a
// plain 停止. The manual cause is on this list rather than carved out precisely
// because it is a clock: the owner asking for one is what makes it legal, and
// nothing else may grow one without being typed onto this line.
func TestWindDownKind_加速停止HasExactlyTwoCauses(t *testing.T) {
	for _, op := range everyWindDownCause {
		kind, clocked := winddownKindFor(op)
		wantFinal := op == refocusOpContextHigh || op == refocusOpAcceleratedStop
		if (kind == offboardKindFinal) != wantFinal || clocked != wantFinal {
			t.Fatalf("%s: got kind=%q clocked=%v, want final=%v", op, kind, clocked, wantFinal)
		}
	}
}

// 🔴 …AND THE CLOSURE IS CHECKED BY MACHINE, because "I read the constants and
// they are all in the list" is precisely the step that failed. everyWindDownCause
// shipped missing refocusOpContextNotice — the value this ticket itself added —
// and a hand-written list can only ever lag the declarations it claims to close
// over. This test re-derives the declarations from the package source and fails
// on the DIFFERENCE, so adding a constant without adding its value is a red
// build rather than a silent hole in two other tests.
//
// The prefixes are the naming convention the three const blocks already use
// (refocusOp* in member_ownerop_winddown.go, memberOp* beside it, ownerOp* in
// worker_spawn.go). A cause added under a FOURTH prefix would still slip past —
// so the failure message names the convention, which is the thing to keep.
func TestWindDownKind_TheClosedSetIsActuallyClosed(t *testing.T) {
	const prefixes = "refocusOp, memberOp, ownerOp"

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package source: %v", err)
	}

	declared := map[string]string{} // wire value -> the constant that declares it
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				gd, ok := d.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if !strings.HasPrefix(name.Name, "refocusOp") &&
							!strings.HasPrefix(name.Name, "memberOp") &&
							!strings.HasPrefix(name.Name, "ownerOp") {
							continue
						}
						if i >= len(vs.Values) {
							continue
						}
						lit, ok := vs.Values[i].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						v, err := strconv.Unquote(lit.Value)
						if err != nil || v == "" {
							continue
						}
						declared[v] = name.Name
					}
				}
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("found no wind-down cause constants at all — the naming convention " +
			"this test keys on (" + prefixes + ") has moved, and the closure check " +
			"is silently checking nothing")
	}

	covered := map[string]bool{}
	for _, op := range everyWindDownCause {
		covered[op] = true
	}
	for value, constName := range declared {
		if !covered[value] {
			t.Errorf("everyWindDownCause is missing %s = %q. It calls itself the "+
				"closed set of refocus_op values, and a value it omits is a value "+
				"BOTH guards in this file are blind to — that is how a mutant giving "+
				"the first context threshold an undeclared 120 s deadline passed "+
				"them. Add the value in the same edit that adds the constant.",
				constName, value)
		}
	}
}

// TestOffboardKindOf_AFinalCallAlwaysHasAClock is the same invariant on the
// OFFLINE arm, and it did not exist until the documents were wired to the send
// site (T-3201).
//
// 🔴 WHY THE OFFLINE ARM NEEDS ITS OWN. The test above walks 換手: it reads
// offboardKindOf's refocus branch and refocusDeadlineOf, and neither of those
// is what a 停止 goes through. 下線 anchors on stopping_since, so the pair that
// has to agree there is offboardKindOf's `DesiredState == offline` branch —
// "is a graceful stop epoch open", then winddownKindFor — against
// winddownDeadlineOf's own zero conditions: !clocked, and the same epoch
// question. Until now the offline arm had only happy-path cases (加速停止
// pressed, notice arrives), and an independent review measured the gap: nothing
// anywhere asserted that the two must keep coinciding.
//
// 🔴 THE "TWO HAND-WRITTEN SPELLINGS IN TWO FILES" THIS PARAGRAPH USED TO NAME
// ARE GONE (T-170e stage 2 ②). Both sites now call gracefulStopEpochOpen, and
// one of the two spellings was the NEGATION of the other, which is the shape a
// reader checks against itself and gets wrong. This test is NOT redundant for
// that: it still holds the sentence and the clock to each other across
// winddownKindFor, which is a different function and a different way to split.
//
// 🔴 AND THE COST OF THEM COMING APART CHANGED. Before the wiring, a final call
// whose deadline was 0 merely lost a clause — the Go builder wrote the sentence
// and skipped the deadline half. The sentence is now the 加速停止 document's
// read-only head, and that head has a {deadline} slot with nothing else able to
// fill it: the send site either refuses (the agent gets NO notice while a clock
// runs on it) or, if someone ever "fixes" the refusal by substituting a blank,
// the agent reads 「你的結束時刻是 。」 — a sentence that names no instant while a
// clock is running. Either way the split is no longer a missing sentence, it is
// a broken one.
//
// ⚠️ WHAT NO LONGER FOLLOWS FROM IT (T-6f44). This used to add 「and 下線程序 §1
// tells it to treat that as hard」 — the sniffed-marker rule, which decision 5
// deleted. §1 no longer reads the notice at all: each document states which kind
// it is, and the server picks the document by `kind`. A blank instant is now a
// document that contradicts itself rather than one that is misclassified.
func TestOffboardKindOf_AFinalCallAlwaysHasAClock(t *testing.T) {
	cfg := defaultReconcileConfig()
	const t0 = 1_000_000.0

	finals := 0
	for _, op := range everyWindDownCause {
		for _, stopping := range []float64{0, t0} {
			// forced_stop_at straddles stopping_since on both sides AND lands
			// exactly on it: force-stop stamps the two from two nowSecs() calls
			// with no I/O between them, so equality is the NORMAL path there
			// (forcedEpochLive's >= is load-bearing for that reason), and a
			// past-epoch stamp is what activate leaves behind.
			for _, forced := range []float64{0, t0 - 1, t0, t0 + 1} {
				m := testAgent("m-3201-offline")
				m.DesiredState = DesiredStateOffline
				m.RefocusOp = op
				m.StoppingSince = stopping
				m.ForcedStopAt = forced

				kind, carries := offboardKindOf(m, t0+1)
				deadline := winddownDeadlineOf(m, cfg)
				if kind == offboardKindFinal {
					finals++
				}
				if !carries {
					// Nothing is sent, so there is no sentence to disagree
					// with — but a clock must not be running either.
					if deadline > 0 {
						t.Fatalf("op=%s stopping=%v forced=%v: no notice is sent and "+
							"yet the tick collects at %v — the session is cut off "+
							"with no warning at all", op, stopping, forced, deadline)
					}
					continue
				}
				if (kind == offboardKindFinal) != (deadline > 0) {
					t.Fatalf("op=%s stopping=%v forced=%v: the sentence says kind=%q "+
						"and the clock says deadline=%v — one of them is lying to "+
						"the agent, and the final call's document cannot render "+
						"without the instant", op, stopping, forced, kind, deadline)
				}
			}
		}
	}
	// DENOMINATOR. Every assertion above is satisfied by a server that never
	// answers "final" on this arm at all, and 加速停止 on a 停止 is exactly what
	// T-ed79 added — so prove the case under test is reachable before reading
	// anything into the green.
	if finals == 0 {
		t.Fatal("no combination above produced a final call, so the invariant was " +
			"never once evaluated on the arm it is about")
	}
}
