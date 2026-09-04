package main

// api_signing_key_observation_reach_t80_test.go — T-80: the observation has
// exactly ONE production call site, and that fact is load-bearing for something
// else.
//
// WHAT THIS IS ACTUALLY PROTECTING, because it is not obvious from the assertion.
// noteTokenKeyObservation resets renewAskedAt whenever the observed key CHANGES,
// which deliberately re-asks a machine that has just moved. Independent review
// measured that this makes the 5-minute throttle bypassable by anything that can
// make the observed key flap — and before the observation moved onto the stream,
// the warden's own renewal produced exactly that flap on every attempt (probe on
// the new key, heartbeat on the old), costing 40 database writes per 20 rounds
// on a single-connection write pool.
//
// That is unreachable NOW, and only because a machine presents exactly one
// credential to exactly one place that records it: the stream it is running on.
// The reset has no test of its own, and writing one would mean pinning a
// scenario nothing can currently produce — a test that asserts no behaviour.
//
// 🔴 SO THE GUARD IS ON THE PREMISE INSTEAD, AND THE PREMISE IS THE FRAGILE PART.
// "Unreachable" is not a property of the code, it is a sentence about which call
// sites exist today, and the change that falsifies it is precisely the small
// well-meant one: someone adds a second place that records an observation —
// a heartbeat, a telemetry hop, "let's also catch it here" — and the throttle
// becomes bypassable again with nothing going red.
//
// This ticket is itself the proof that such a thing happens. The warden renewal
// path was complete, wired, and DEAD in production (its only trigger was an
// expiry that warden credentials do not have). T-80 added one trigger, and every
// pre-existing weakness on that path went from probability zero to non-zero. The
// author of that change — me — did not notice; independent review did.
//
// ⚠️ WHAT THIS SCAN CANNOT DO, stated because the file next door
// (api_chat_attachment_wiring_test.go) learned it the hard way and its author
// wrote it down: an AST scan pins a SPELLING, so a call reached through a method
// value or an alias is invisible to it. That is not a guess here, it is
// measured — the three mutants below were run:
//
//	① a second call site, written the ordinary way    → THIS test goes red, naming the file and line
//	② the only call site deleted                      → THIS test goes red (the zero-sites control)
//	③ a second call via a method value (`f := s.note…`) → THIS TEST STAYS GREEN
//
// ③ is the honest limit, and it is also why this file is not the only guard.
// The same mutant ③ turns
// TestPresentingACredentialOnAnOrdinaryRouteIsNotEvidenceOfRunningOnIt RED,
// because that one asserts the OUTCOME (present a credential on an ordinary
// route, write nothing, require the station has not changed its mind) and does
// not care how the second observation was spelled.
//
// So the pair is deliberate and the division of labour is: the outcome test is
// the guard, this scan is the DIAGNOSIS — it names the offending line for the
// case a well-meaning contributor actually writes, where the outcome test would
// only say that something is wrong. Never treat this file's green as evidence
// that one path exists; its RED is the whole of its value.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// observationHost is the one function allowed to record an observation, and the
// reason it is that one: the SSE stream is opened with the credential the warden
// process actually loaded, so "presented" and "running on" are the same event
// there. Every other route sees credentials a machine may merely be TRYING.
const observationHost = "HandleEventsApiEventsGet"

func TestTheKeyObservationHasExactlyOneProductionCallSite(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}

	type site struct {
		fn  string
		pos string
	}
	var sites []site
	for _, p := range pkgs {
		for _, file := range p.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				ast.Inspect(fn, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "noteTokenKeyObservation" {
						return true
					}
					sites = append(sites, site{fn.Name.Name, fset.Position(call.Pos()).String()})
					return true
				})
			}
		}
	}

	// A scan that has stopped finding its target proves nothing and would stay
	// green forever after a rename. This is the positive control, and it is not
	// optional: without it "zero call sites" and "the feature was deleted" and
	// "this scan is looking at the wrong name" are the same green.
	if len(sites) == 0 {
		t.Fatal("this scan found NO call to noteTokenKeyObservation. Either the " +
			"observation was removed (in which case token_key_current is now " +
			"permanently stale and the owner is reading a frozen number), or the " +
			"function was renamed and this scan is now watching nothing. Both " +
			"need a human; neither is a pass.")
	}

	if len(sites) != 1 || sites[0].fn != observationHost {
		var got []string
		for _, s := range sites {
			got = append(got, s.fn+" at "+s.pos)
		}
		t.Fatalf("the key observation must have exactly ONE production call site, "+
			"in %s; found %d: %s\n\n"+
			"If you just added one: you have made something reachable that was "+
			"relied upon to be unreachable. noteTokenKeyObservation resets the "+
			"renew-ask throttle whenever the observed key CHANGES, and that reset "+
			"has no guard of its own — it is safe today only because a machine's "+
			"key is recorded in exactly one place, the stream it is actually "+
			"running on. A second recorder lets the observed key FLAP (one path "+
			"seeing a credential the machine is only trying, another seeing the "+
			"one it runs on), which bypasses the throttle and writes to a "+
			"one-connection write pool on every request. Measured before this was "+
			"fixed: 40 writes per 20 rounds.\n\n"+
			"It also risks the defect this whole design exists to prevent: a "+
			"credential a machine merely PRESENTED being counted as the one it is "+
			"RUNNING on, so the station reports converged while the machine is "+
			"still on the outgoing key — and the owner presses remove, which has "+
			"no grace period and no undo. See T-80 and "+
			"TestPresentingACredentialOnAnOrdinaryRouteIsNotEvidenceOfRunningOnIt.",
			observationHost, len(sites), strings.Join(got, ", "))
	}
}
