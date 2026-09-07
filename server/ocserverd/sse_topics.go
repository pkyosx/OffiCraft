// sse_topics.go — the `sse-topics` subcommand: render the CLOSED entity-delta
// topic vocabulary as a MACHINE-READABLE asset (spec/sse-topics.json).
//
// ── WHY IT LIVES IN package main ─────────────────────────────────────────────
// The vocabulary IS `sseTopics` in hub.go — the map the publish seam consults
// before it fans anything — and that identifier is not reachable from outside
// this package. A standalone renderer would have to re-type the list, which is
// the very duplication this asset exists to remove: the list was carried in
// three places (this map, spec/sse.md §3.1's table, and a markdown parser in
// conformance/test_sse.py), each pinned to the others by a test, so a topic
// added in one place and forgotten in another was a live failure mode.
//
// 🔴 THE OUTPUT IS 100% DERIVED. Nothing in spec/sse-topics.json is typed by a
// human: the topic list is this package's map, and the header fields are the
// two constants below. spec/sse.md stays HAND-WRITTEN and is NOT generated from
// here — adding, renaming or removing a topic means editing hub.go, re-running
// bin/gen-sse-topics, and updating §3.1's table by hand.
//
// ── SCOPE ────────────────────────────────────────────────────────────────────
// ENTITY-DELTA topics only — the ones that ride hub.Publish. The directed bands
// (`context-high` §6, `token-expiry` §6.1, `warden-command` §7) go out through
// PushDirected, bypass Publish entirely, and are a separate envelope family by
// design. Their absence here is deliberate.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

// The two header fields, so a reader of the artifact can tell at a glance that
// it is a product and where to regenerate it from. They are the ONLY strings in
// the output that are not a topic name.
const (
	sseTopicsGeneratedBy = "bin/gen-sse-topics"
	sseTopicsSource      = "server/ocserverd/hub.go (sseTopics)"
)

// sseTopicsWriteMarker is the last thing the subcommand prints on a successful
// write — the line bin/gen-sse-topics requires before believing anything ran.
// A zero exit says "nothing failed", not "something happened".
const sseTopicsWriteMarker = "[gen-sse-topics] wrote"

// sseTopicsDocument is the artifact's shape. Sorted `topics` so the render is
// deterministic (map iteration order is not) — every drift gate in this repo
// asserts "regenerating produces byte-identical output".
type sseTopicsDocument struct {
	GeneratedBy string   `json:"generated_by"`
	Source      string   `json:"source"`
	Topics      []string `json:"topics"`
}

// renderSSETopics renders the artifact bytes from the closed vocabulary, or
// refuses. An EMPTY set is an error, never an empty answer: every consumer of
// this file confronts its own coverage with it, and an empty set makes each of
// those confrontations pass while proving nothing.
func renderSSETopics(topics map[string]bool) ([]byte, int, error) {
	names := make([]string, 0, len(topics))
	for name, ok := range topics {
		if ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, 0, fmt.Errorf("the closed topic set is EMPTY — hub.go's sseTopics carries no " +
			"enabled topic, and an empty vocabulary would make every consumer's coverage " +
			"confrontation vacuous")
	}
	sort.Strings(names)
	blob, err := json.MarshalIndent(sseTopicsDocument{
		GeneratedBy: sseTopicsGeneratedBy,
		Source:      sseTopicsSource,
		Topics:      names,
	}, "", "  ")
	if err != nil {
		return nil, 0, fmt.Errorf("marshal topic document: %w", err)
	}
	return append(blob, '\n'), len(names), nil
}

// cmdSSETopics is the `sse-topics` subcommand: write the rendered vocabulary to
// the named path. The path is always explicit (no default) so the caller owns
// it — bin/gen-sse-topics passes the committed spec/sse-topics.json, and the
// drift-sse-topics gate passes a temp file so the committed bytes are never
// touched by the comparison.
func cmdSSETopics(args []string, out io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(out, "usage: ocserverd sse-topics <out.json>")
		fmt.Fprintln(out, "  renders hub.go's closed entity-delta topic vocabulary to <out.json>")
		fmt.Fprintln(out, "Run it through bin/gen-sse-topics, which knows the committed path.")
		return 2
	}
	dst := args[0]
	blob, n, err := renderSSETopics(sseTopics)
	if err != nil {
		fmt.Fprintf(out, "[sse-topics] %v\n", err)
		return 1
	}
	if err := os.WriteFile(dst, blob, 0o644); err != nil {
		fmt.Fprintf(out, "[sse-topics] write %s: %v\n", dst, err)
		return 1
	}
	fmt.Fprintf(out, "%s %s: %d topic(s)\n", sseTopicsWriteMarker, dst, n)
	return 0
}
