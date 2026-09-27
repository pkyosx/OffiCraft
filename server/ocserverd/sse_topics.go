// 🔴 spec/sse-topics.json is 100% derived from hub.go's `sseTopics`; spec/sse.md
// stays HAND-WRITTEN. Adding, renaming or removing a topic means editing
// hub.go, re-running bin/gen-sse-topics, and updating spec/sse.md §3.1's table
// by hand.
//
// Entity-delta topics only. The directed bands are deliberately absent because
// none goes through Publish: `context-high` and `token-expiry` are written by the
// SSE loop onto the member's own connection, and `warden-command` is queued in the
// warden's durable FIFO and drained onto the warden's own connection.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
)

const (
	sseTopicsGeneratedBy = "bin/gen-sse-topics"
	sseTopicsSource      = "server/ocserverd/hub.go (sseTopics)"
)

// bin/gen-sse-topics requires this line before believing anything ran.
const sseTopicsWriteMarker = "[gen-sse-topics] wrote"

// Drift gates assert regenerating produces byte-identical output.
type sseTopicsDocument struct {
	GeneratedBy string   `json:"generated_by"`
	Source      string   `json:"source"`
	Topics      []string `json:"topics"`
}

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
