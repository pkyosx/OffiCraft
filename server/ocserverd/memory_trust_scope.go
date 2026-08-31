package main

// memory_trust_scope.go — T-33. THE ONE function in the tree that answers "what
// class of knowledge is this entry", derived from the entry's action names.
//
// 🔴 IT IS DERIVED, NEVER STORED. An earlier draft made trust_scope a column on
// lore_entry and reconciled it afterwards. That was rejected: a
// stored class and the actions it came from are two statements about the same
// fact, and the moment an entry's actions are edited the column is a lie that
// nothing reports. The retrieval rule that hangs off this — "trust-class entries
// do not cross to a different subject by default" — has to be judged on every
// read, from the actions as they are NOW.
//
// 🔴 AND THE FALLBACK IS THE WHOLE DESIGN, so read this before touching the
// table below. `actions` is an OPEN set: the agent writing a memory mints the
// action name, so names that do not exist today will exist at run time. That
// single fact kills the obvious protection — a build-time exhaustiveness guard
// over the names visible in the source. Such a guard is green forever and
// discriminates nothing about the names it was built to catch, which makes it a
// silent no-op wearing the costume of a wall. (That shape — a protection that
// only works while someone is looking at it — is the exact failure this whole
// ticket exists to kill, and the first attempt at this function reproduced it.)
//
// ⇒ The wall is at RUN TIME instead, and it has two halves, both required:
//
//  1. FAIL-CLOSED. An action name this table does not recognise resolves to
//     `trust`, the STRICTEST class — not to the permissive `method` default. An
//     unrecognised name is an unknown, and an unknown that quietly reads as
//     "safe to generalise across subjects" is worse than no rule at all.
//
//  2. IT SAYS SO OUT LOUD. Every fall-through is reported: the names come back
//     on the verdict as Unmapped, AND a line is logged. A bare `default:` branch
//     would be indistinguishable, from outside, from a table that had an answer
//     — and "unrecognised input silently took the default" is the disease, not
//     the cure. Callers are expected to surface Unmapped (the Health screen's
//     "action names nobody has mapped yet" list is where it is meant to land).
//
// ⚠️ WHAT THIS DOES NOT PROTECT, stated so nobody mistakes the coverage:
// fail-closed catches names that are MISSING from the table. It cannot catch a
// name that is IN the table under the WRONG class — a trust-class action typed
// into the method list reads as fully recognised and generalises across
// subjects. There is no mechanism for that today.

import (
	"log"
	"sort"
	"strings"
)

// TrustScope is the retrieval class of a memory entry.
type TrustScope string

const (
	// TrustScopeMethod — how to do a thing. Crosses to other subjects as a
	// normal analogy.
	TrustScopeMethod TrustScope = "method"
	// TrustScopeTrust — how far something or someone can be relied on. Does
	// NOT cross to another subject by default; this is also the fail-closed
	// answer for anything unrecognised.
	TrustScopeTrust TrustScope = "trust"
	// TrustScopeCognitive — a failure mode of thinking. Exempt from the
	// staleness judgement, because such an entry does not go out of date the
	// way a fact about a repo does.
	TrustScopeCognitive TrustScope = "cognitive"
)

// TrustScopeVerdict is what memoryTrustScope answers.
//
// 🔴 IT IS A STRUCT RATHER THAN A BARE TrustScope BECAUSE OF REQUIREMENT (2)
// ABOVE. A function that can only return the class has no way to tell its caller
// that it GUESSED, so the fall-through becomes invisible at exactly the boundary
// where it needed to be visible. Unmapped is that channel.
type TrustScopeVerdict struct {
	// Scope is the class to rank and filter by.
	Scope TrustScope
	// Unmapped lists, sorted and de-duplicated, the action names this table
	// did not recognise. Non-empty means Scope was reached by fail-closing,
	// not by knowing.
	Unmapped []string
}

// FellBack reports whether the verdict came from the fail-closed path.
func (v TrustScopeVerdict) FellBack() bool { return len(v.Unmapped) > 0 }

// trustScopeByAction is the mapping table, and it is HAND-WRITTEN AND
// PROVISIONAL.
//
// 🔴 TODO(T-33, needs a ruling): this seed list is the implementer's reading, not
// a decision anybody made. The detail design names the three classes and their
// retrieval behaviour but never enumerates which action names belong to which —
// and, per the note at the top of this file, a wrong entry here is the one
// failure fail-closed cannot see. It should be reviewed by whoever owns the
// retrieval rules before the recall path is wired up, and it is deliberately
// SHORT so that the fail-closed path (which is safe) carries the unknowns rather
// than this table (which is guessed) absorbing them.
var trustScopeByAction = map[string]TrustScope{
	// Method — the shape of "here is how this is done".
	"build":     TrustScopeMethod,
	"test":      TrustScopeMethod,
	"debug":     TrustScopeMethod,
	"deploy":    TrustScopeMethod,
	"migrate":   TrustScopeMethod,
	"configure": TrustScopeMethod,
	"query":     TrustScopeMethod,
	"review":    TrustScopeMethod,

	// Trust — the shape of "here is how far this can be relied on".
	"estimate": TrustScopeTrust,
	"delegate": TrustScopeTrust,
	"rely-on":  TrustScopeTrust,
	"escalate": TrustScopeTrust,

	// Cognitive — the shape of "here is how thinking goes wrong".
	"assume":     TrustScopeCognitive,
	"infer":      TrustScopeCognitive,
	"generalise": TrustScopeCognitive,
}

// memoryTrustScope derives an entry's class from its action names.
//
// The combination rule is STRICTEST-WINS, for the same reason the fallback is:
// an entry that carries one trust-class action is a trust-class entry no matter
// what else it carries, because letting a method-class sibling downgrade it
// would hand any writer a one-word bypass of the cross-subject wall.
//
// 🔴 TODO(T-33, needs a ruling): the mixed cognitive/method case is NOT decided
// anywhere. `cognitive` is not "stricter" than `method` — it is a different axis
// (it turns the staleness check off rather than restricting where the entry may
// appear), so "strictest wins" does not order the two. This code answers
// `method` for that mixture, on the grounds that the visibility axis is the one
// being asked about here; that is a choice made to keep the function total, not
// a ruling, and it should be put to whoever owns §3.22.
//
// An EMPTY action list fails closed too: an entry that names no action has not
// been classified, and "not classified" is an unknown like any other.
func memoryTrustScope(actions []string) TrustScopeVerdict {
	var (
		unmapped     []string
		seenUnmapped = map[string]bool{}
		sawTrust     bool
		sawMethod    bool
		sawCognitive bool
	)
	for _, raw := range actions {
		action := strings.ToLower(strings.TrimSpace(raw))
		if action == "" {
			continue
		}
		switch trustScopeByAction[action] {
		case TrustScopeMethod:
			sawMethod = true
		case TrustScopeTrust:
			sawTrust = true
		case TrustScopeCognitive:
			sawCognitive = true
		default:
			if !seenUnmapped[action] {
				seenUnmapped[action] = true
				unmapped = append(unmapped, action)
			}
		}
	}
	sort.Strings(unmapped)

	verdict := TrustScopeVerdict{Unmapped: unmapped}
	switch {
	case len(unmapped) > 0 || sawTrust:
		// 🔴 THE FAIL-CLOSED BRANCH. Flipping this to TrustScopeMethod is the
		// mutant TestMemoryTrustScopeUnknownActionFailsClosedToTrust exists to
		// catch — and it is the single most tempting edit in this file, because
		// `method` is what makes unfamiliar entries show up where people expect
		// to see them.
		verdict.Scope = TrustScopeTrust
	case sawCognitive && !sawMethod:
		verdict.Scope = TrustScopeCognitive
	case sawMethod:
		verdict.Scope = TrustScopeMethod
	default:
		// No action survived trimming — an entry with no classification at all.
		verdict.Scope = TrustScopeTrust
	}

	if len(unmapped) > 0 {
		logUnmappedMemoryActions(unmapped, verdict.Scope)
	}
	return verdict
}

// logUnmappedMemoryActions is the audible half of the fail-closed rule.
//
// A log line is the floor, not the ceiling: the design wants these names
// counted and listed on the Memory → Health screen. That surface does not exist
// yet, so the names travel on the verdict (see TrustScopeVerdict.Unmapped) and
// this line makes them visible on a station in the meantime. When the health
// counter lands it should be incremented HERE, so there is still exactly one
// place that knows a fall-through happened.
func logUnmappedMemoryActions(unmapped []string, scope TrustScope) {
	log.Printf("[lore] unmapped_actions=%q fell_back_to=%s count=%d",
		strings.Join(unmapped, ","), scope, len(unmapped))
}
