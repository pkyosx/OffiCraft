package main

// routes_manifest_parity_t61_test.go — T-61: a DIRECT binding between the route
// table the mux is built from and conformance/routes_manifest.json.
//
// ── READ THIS FIRST: THE TICKET'S PREMISE WAS WRONG ─────────────────────────
//
// T-61 was opened on the belief that "a new API nobody registers in
// routes_manifest.json gets zero permission coverage and nothing goes red".
// That is FALSE, and it was false before this file existed. Two legs already
// close the loop, and they close it in BOTH directions:
//
//	routes.go  ≡  spec/openapi.json   TestRouteTableCoversSpecSurface (server_test.go)
//	openapi    ≡  routes_manifest     test_openapi_covers_manifest (conformance,
//	                                  a symmetric `spec_ops == manifest_ops`)
//
// Measured (T-61 round-3, by hand): adding one `MCPExclude: true` row to
// routeSpecs fails TestRouteTableCoversSpecSurface with "route table row not in
// the spec (wire freeze)". So an unregistered route — MCP-visible or infra —
// already reddens, and the earlier belief that the infra rows were bare was an
// artifact of a truncated search, not of the code.
//
// ⚠️ Two claims that stood in this header for three commits were false: that
// nothing pinned the route table to the manifest, and that nothing pinned
// buildHandler's registration (TestBusinessRoutesServeThroughTheWiredStack
// does, for the rows it covers). Both were NEGATIVE UNIVERSALS written from
// somebody else's search rather than my own. If you are about to add another
// "nothing in this repo does X" to this file: build the denominator yourself
// first, without a `| head`.
//
// ── SO WHAT IS THIS FILE FOR ────────────────────────────────────────────────
//
// It is not a missing safety net. It is a DIRECT edge where there were two
// hops, and it buys four things that the two-hop chain does not:
//
//  1. ONE HOP. The chain routes ≡ openapi ≡ manifest holds only while BOTH legs
//     hold; this compares the two ends to each other, so a manifest that drifts
//     is named as a manifest problem instead of a spec problem.
//  2. IN GO. The manifest leg lives in conformance, which needs a live server.
//     This runs in the unit suite, seconds after the edit that broke it.
//  3. IT NAMES THE PERMISSION CONSEQUENCE. "not in the permission manifest ⇒
//     test_auth_matrix.py never fires a request at it" is the sentence someone
//     needs; "not in the spec (wire freeze)" is a different diagnosis of the
//     same fact.
//  4. A DECLARED EXEMPTION PATH. Neither existing leg has one, so a route that
//     genuinely belongs outside would have to be argued each time it reddens.
//
// If a reviewer decides that is not worth another gate, deleting this file
// costs no coverage. Say so out loud rather than keeping it for its story.
//
// ── WHERE THE DENOMINATOR COMES FROM ────────────────────────────────────────
//
// routeSpecs() (routes.go) is not a snapshot of the surface: it IS the surface.
// buildHandler (server.go) builds the mux by ranging over it — one mux.Handle
// per row — and the only other registration is the "/" static fallback.
//
// ── WHAT THIS GATE DOES NOT DO ──────────────────────────────────────────────
//
// It compares MEMBERSHIP (method+path), not the auth/requires/mcp_tool columns.
// requires is graded by live requests in test_auth_matrix.py; mcp_tool is held
// by test_mcp.py's test_catalog_hash_algorithm (the matrix never mentions that
// column). Duplicating either here would add a second opinion, not a gate.
//
// It reads the TABLE, not the mux: "the table is right" is one step short of
// "the mux was built from it". TestBusinessRoutesServeThroughTheWiredStack
// covers that step for the rows it exercises; no test covers it for every row.
//
// AND THE EXEMPTION LISTS ARE NOT COMPILER-PROOF. exemptRoute() is the intended
// door and its four parameters make an unreasoned entry impossible to WRITE
// through it — but routeExemption is an ordinary struct in this package, so a
// struct literal filling only method and path compiles. What stops that literal
// is the runtime check in validateExemptions, which has its own positive
// control below. The one thing that IS structural is reach: the type, the
// constructor and both lists live in this _test.go file, so no product code can
// name them. And nothing here can judge whether a reason is HONEST — a fluent
// excuse passes. What it buys is that adding one MUST appear in the diff.
//
// ── WHY THE JUDGEMENT IS ONE PURE FUNCTION ──────────────────────────────────
//
// 🔴 Three reviews in a row found the same shape: a control that bites
// something the gate does not actually depend on. First the comparison was
// duplicated; then the exemption checks had never executed; then the gate threw
// away what the pure comparison returned (`_, _ = missing, stale`) and every
// control stayed green. Each fix MOVED the hole instead of closing it, because
// each time the seam between "the logic" and "the reporting" was left
// unguarded. So the whole judgement — floors, duplicates, exemptions, both
// directions — is gateFindings(), and the test does nothing but print what it
// returns. Gut any part of it and TestGateFindingsHaveTeeth reddens.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// ── the exemption shape ─────────────────────────────────────────────────────

// routeExemption is one deliberate absence. Build it with exemptRoute; see the
// header for what that does and does not stop.
type routeExemption struct {
	method string
	path   string
	reason string
	ruling string
}

// exemptionReasonFloor is a length, not a quality bar. It stops "n/a" and
// "legacy"; it cannot stop a fluent sentence, and it is not meant to.
const exemptionReasonFloor = 40

// exemptRoute is the intended way to build a routeExemption — every field is
// required by the signature.
//
//	method, path — must match a row exactly ({param} names included)
//	reason       — why this route is deliberately outside the permission suite
//	ruling       — who decided (ticket key, or "owner YYYY-MM-DD")
func exemptRoute(method, path, reason, ruling string) routeExemption {
	return routeExemption{method: method, path: path, reason: reason, ruling: ruling}
}

// servedButUnlisted: routes the server serves ON PURPOSE without a manifest row
// (and therefore with no permission-matrix cell). EMPTY today — every one of
// the 171 served rows is listed. An entry here is a route nobody's permission
// test will touch again.
var servedButUnlisted = []routeExemption{}

// listedButUnserved: manifest rows kept ON PURPOSE for a path the server no
// longer serves. Also empty today.
var listedButUnserved = []routeExemption{}

// Corpus floors. This repo has shipped "assert empty, then range over the empty
// set" before, so both sides prove they were populated BEFORE any verdict is
// read. Far below the real counts (171 each) — they catch a reader that stopped
// seeing, not growth or shrinkage.
const (
	servedRowFloor   = 100
	manifestRowFloor = 100
)

type manifestRow struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

func loadRoutesManifest(t *testing.T) []manifestRow {
	t.Helper()
	raw, err := os.ReadFile("../../conformance/routes_manifest.json")
	if err != nil {
		t.Fatalf("cannot read conformance/routes_manifest.json: %v", err)
	}
	var rows []manifestRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("conformance/routes_manifest.json is not a JSON array of rows: %v", err)
	}
	return rows
}

// routeKey joins a row's method and path VERBATIM — no case folding. Both sides
// write methods in upper case, so folding changed nothing that is true today
// while making a case difference between the two sources invisible (round-2
// mutant). Verbatim, a disagreement about case is a disagreement.
func routeKey(method, path string) string {
	return method + " " + path
}

// duplicateKeys reports every key appearing more than once, in order of first
// repeat.
func duplicateKeys(keys []string) []string {
	seen := map[string]bool{}
	var dups []string
	for _, k := range keys {
		if seen[k] {
			dups = append(dups, k)
		}
		seen[k] = true
	}
	return dups
}

// validateExemptions returns the problems in one exemption list plus its keys.
// PURE so the control can feed it a synthetic list: with both real lists empty
// it would otherwise never execute, and an unexercised guard is
// indistinguishable from a deleted one (round-2 review, measured).
//
// An entry naming nothing in `corpus` is STALE: an excuse standing over a hole
// that moved.
func validateExemptions(list []routeExemption, corpus map[string]bool, label, goneMeans string) (index map[string]bool, problems []string) {
	index = map[string]bool{}
	for _, e := range list {
		k := routeKey(e.method, e.path)
		if index[k] {
			problems = append(problems, fmt.Sprintf("%s exemption for %s is declared twice", label, k))
		}
		index[k] = true
		if n := len(strings.TrimSpace(e.reason)); n < exemptionReasonFloor {
			problems = append(problems, fmt.Sprintf(
				"%s exemption for %s carries a %d-character reason; this gate asks "+
					"for at least %d. The length is not the point — a route outside "+
					"the permission suite needs a sentence a reviewer can disagree with.",
				label, k, n, exemptionReasonFloor))
		}
		if strings.TrimSpace(e.ruling) == "" {
			problems = append(problems, fmt.Sprintf(
				"%s exemption for %s names no ruling — put the ticket key or "+
					"\"owner YYYY-MM-DD\" that decided it, so the next reader can go "+
					"read the decision instead of re-deriving it", label, k))
		}
		if !corpus[k] {
			problems = append(problems, fmt.Sprintf(
				"STALE %s exemption: %s — %s, so this entry now excuses nothing "+
					"while still reading like a decision someone made. Delete it. "+
					"(reason on file: %q)", label, k, goneMeans, e.reason))
		}
	}
	return index, problems
}

// gateFindings is THE judgement — floors, duplicates, exemptions and both
// directions of the parity comparison, as one pure function. The test below
// only prints what this returns, so there is no seam between deciding and
// reporting for a mutant to hide in. See the header for why that matters here
// specifically.
//
// Empty result = the two corpora agree.
func gateFindings(specs []RouteSpec, rows []manifestRow, unlistedExempt, unservedExempt []routeExemption) []string {
	var out []string

	// (0) Populated corpora first — "no difference" is also what an empty set
	// answers.
	if len(specs) < servedRowFloor {
		return append(out, fmt.Sprintf(
			"routeSpecs returned %d rows, below the floor of %d — the route table "+
				"is the denominator of this gate and it looks truncated; every "+
				"verdict would be about a table nobody built", len(specs), servedRowFloor))
	}
	if len(rows) < manifestRowFloor {
		return append(out, fmt.Sprintf(
			"routes_manifest.json carries %d rows, below the floor of %d — a "+
				"truncated manifest makes this gate shout about routes that are fine",
			len(rows), manifestRowFloor))
	}

	servedKeys := make([]string, 0, len(specs))
	served := map[string]bool{}
	for _, s := range specs {
		k := routeKey(s.Method, s.Path)
		servedKeys = append(servedKeys, k)
		served[k] = true
	}
	for _, k := range duplicateKeys(servedKeys) {
		out = append(out, fmt.Sprintf("route table declares %s twice — the mux "+
			"registration in server.go would panic on the second one", k))
	}
	listedKeys := make([]string, 0, len(rows))
	listed := map[string]bool{}
	for _, r := range rows {
		k := routeKey(r.Method, r.Path)
		listedKeys = append(listedKeys, k)
		listed[k] = true
	}
	for _, k := range duplicateKeys(listedKeys) {
		out = append(out, fmt.Sprintf("routes_manifest.json lists %s twice", k))
	}

	// Exemptions are validated BEFORE they are honoured: a stale one is a silent
	// hole, so it reddens rather than quietly excusing nothing.
	unlisted, problems := validateExemptions(unlistedExempt, served,
		"served-but-unlisted", "the route table no longer serves it")
	out = append(out, problems...)
	unserved, problems := validateExemptions(unservedExempt, listed,
		"listed-but-unserved", "the manifest no longer lists it")
	out = append(out, problems...)

	var missing, stale []string
	for k := range served {
		if !listed[k] && !unlisted[k] {
			missing = append(missing, k)
		}
	}
	for k := range listed {
		if !served[k] && !unserved[k] {
			stale = append(stale, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)

	for _, k := range missing {
		out = append(out, fmt.Sprintf("SERVED BUT NOT IN THE PERMISSION MANIFEST: %s\n"+
			"    the server routes this (routes.go builds the mux from that row) but "+
			"conformance/routes_manifest.json has no line for it, so "+
			"test_auth_matrix.py never fires a single request at it from any "+
			"identity. It is not failing the permission suite; it is not IN it.\n"+
			"    Fix: add the row to conformance/routes_manifest.json (and the "+
			"spec/openapi.json operation the wire freeze will then demand). "+
			"Deliberately outside? Add an exemptRoute(...) entry to "+
			"servedButUnlisted, with a reason and a ruling.", k))
	}
	for _, k := range stale {
		out = append(out, fmt.Sprintf("IN THE PERMISSION MANIFEST BUT NOT SERVED: %s\n"+
			"    conformance/routes_manifest.json declares this route and the "+
			"permission matrix grades cells for it, but the server's route table "+
			"has no such row — the cells are graded against a path that answers "+
			"404.\n"+
			"    Fix: drop the row from conformance/routes_manifest.json, or add an "+
			"exemptRoute(...) entry to listedButUnserved.", k))
	}
	return out
}

// TestEveryServedRouteIsInThePermissionManifest is the gate.
func TestEveryServedRouteIsInThePermissionManifest(t *testing.T) {
	specs := defaultRouteSpecs()
	rows := loadRoutesManifest(t)
	for _, f := range gateFindings(specs, rows, servedButUnlisted, listedButUnserved) {
		t.Error(f)
	}
	t.Logf("compared %d served routes against %d manifest rows "+
		"(exemptions: %d served-but-unlisted, %d listed-but-unserved)",
		len(specs), len(rows), len(servedButUnlisted), len(listedButUnserved))
}

// TestGateFindingsHaveTeeth bites gateFindings END TO END — the same call the
// gate makes, over doctored copies of the real corpora. Every defect class the
// gate claims to catch has a case here, and gutting any of them reddens this.
func TestGateFindingsHaveTeeth(t *testing.T) {
	specs := defaultRouteSpecs()
	rows := loadRoutesManifest(t)
	if len(specs) == 0 || len(rows) == 0 {
		t.Fatal("empty corpus — every case below would pass vacuously")
	}
	if f := gateFindings(specs, rows, nil, nil); len(f) != 0 {
		t.Fatalf("the undoctored corpora already report findings, so nothing below "+
			"proves anything: %v", f)
	}

	ghost := RouteSpec{Method: "GET", Path: "/api/t61-route-that-does-not-exist"}
	ghostKey := routeKey(ghost.Method, ghost.Path)
	good := "a long enough sentence explaining why this route sits outside the suite"

	mustReport := func(name, want string, f []string) {
		t.Helper()
		if len(f) == 0 {
			t.Errorf("%s produced NO finding", name)
			return
		}
		for _, s := range f {
			if strings.Contains(s, want) {
				return
			}
		}
		t.Errorf("%s was reported but never named %q: %v", name, want, f)
	}

	// served but unlisted / listed but unserved.
	mustReport("a served, unlisted route", ghostKey,
		gateFindings(append(append([]RouteSpec{}, specs...), ghost), rows, nil, nil))
	mustReport("a listed, unserved route", ghostKey,
		gateFindings(specs, append(append([]manifestRow{}, rows...),
			manifestRow{Method: ghost.Method, Path: ghost.Path}), nil, nil))

	// duplicates on either side.
	mustReport("a route declared twice", specs[0].Method+" "+specs[0].Path,
		gateFindings(append(append([]RouteSpec{}, specs...), specs[0]), rows, nil, nil))
	mustReport("a manifest row listed twice", rows[0].Method+" "+rows[0].Path,
		gateFindings(specs, append(append([]manifestRow{}, rows...), rows[0]), nil, nil))

	// corpus floors.
	mustReport("a truncated route table", "below the floor",
		gateFindings(specs[:1], rows, nil, nil))
	mustReport("a truncated manifest", "below the floor",
		gateFindings(specs, rows[:1], nil, nil))

	// exemptions: each defect, including the constructor bypass.
	doctored := append(append([]RouteSpec{}, specs...), ghost)
	mustReport("a stale exemption", "STALE",
		gateFindings(specs, rows, []routeExemption{exemptRoute(ghost.Method, ghost.Path, good, "T-61")}, nil))
	mustReport("an exemption with a shrug for a reason", "character reason",
		gateFindings(doctored, rows, []routeExemption{exemptRoute(ghost.Method, ghost.Path, "legacy", "T-61")}, nil))
	mustReport("an exemption with no ruling", "names no ruling",
		gateFindings(doctored, rows, []routeExemption{exemptRoute(ghost.Method, ghost.Path, good, " ")}, nil))
	// 🔴 the bypass itself: a struct literal that skipped exemptRoute.
	mustReport("an exemption that skipped the constructor", ghostKey,
		gateFindings(doctored, rows, []routeExemption{{method: ghost.Method, path: ghost.Path}}, nil))

	// and a well-formed exemption must actually silence its route — the escape
	// hatch has to work, or someone will delete the gate instead of using it.
	if f := gateFindings(doctored, rows,
		[]routeExemption{exemptRoute(ghost.Method, ghost.Path, good, "T-61")}, nil); len(f) != 0 {
		t.Errorf("a well-formed exemption did not silence its own route: %v", f)
	}

	// routeKey must not fold case, or a case disagreement between the two
	// sources would compare equal and the gate would report nothing.
	if routeKey("GET", "/x") == routeKey("get", "/x") {
		t.Error("routeKey folds case")
	}
}
