package main

import (
	"encoding/json"
	"os"
	"testing"
)

// ChatAttachmentInputDTO's description is carried by SIX surfaces: the OpenAPI
// component (which the generators put in front of Go and TypeScript callers)
// and a verbatim copy inside the `$defs` of each x-mcp.legacy.descriptor that
// takes attachments — and the descriptor is what tools/list actually shows an
// agent, so the copies are the ones agents read.
//
// Nothing else compares them. T-59's independent review deleted the sentence
// from ONE nested copy and every drift gate plus every catalog-conformance test
// stayed green: the catalog checks a tool's own name and description and never
// descends into its inputSchema. So the failure this closes is silent by
// construction — the component says one thing, five agent-facing copies say
// another, and the disagreement surfaces only when an agent is refused for a
// reason its own tool description never mentioned.
//
// This mirrors TestChatByIDs_SchemaAndToolCatalogCarryTheSameSentence rather
// than adding a scanner: it names the one description that is actually
// duplicated, and it fails on the copies rather than on a pattern.
func TestChatAttachmentInputDefinitionIsIdenticalInEveryAgentFacingCopy(t *testing.T) {
	raw, err := os.ReadFile("../../spec/openapi.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Description string `json:"description"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	want := spec.Components.Schemas["ChatAttachmentInputDTO"].Description
	if want == "" {
		t.Fatal("ChatAttachmentInputDTO carries no description in components.schemas — " +
			"this test compares copies against that one, so an empty original " +
			"would let every copy pass while saying nothing")
	}

	seen := 0
	for path, methods := range spec.Paths {
		for method, opRaw := range methods {
			var op struct {
				XMCP struct {
					Legacy struct {
						Descriptor string `json:"descriptor"`
					} `json:"legacy"`
				} `json:"x-mcp"`
			}
			if json.Unmarshal(opRaw, &op) != nil || op.XMCP.Legacy.Descriptor == "" {
				continue
			}
			var descriptor struct {
				InputSchema struct {
					Defs map[string]struct {
						Description string `json:"description"`
					} `json:"$defs"`
				} `json:"inputSchema"`
			}
			if err := json.Unmarshal([]byte(op.XMCP.Legacy.Descriptor), &descriptor); err != nil {
				t.Fatalf("%s %s: legacy descriptor is not valid JSON: %v", method, path, err)
			}
			def, ok := descriptor.InputSchema.Defs["ChatAttachmentInputDTO"]
			if !ok {
				continue
			}
			seen++
			if def.Description != want {
				t.Errorf("%s %s: the ChatAttachmentInputDTO an agent reads has drifted from "+
					"the one in components.schemas.\n  component: %.160s…\n  descriptor: %.160s…",
					method, path, want, def.Description)
			}
		}
	}

	// A count, because the loop above is vacuously green if the descriptors
	// stop carrying the definition at all — which is exactly what a refactor
	// that "cleaned up" the nested copies would look like from here.
	if seen == 0 {
		t.Fatal("no legacy descriptor carries ChatAttachmentInputDTO — either the " +
			"attachment-taking tools lost it, or this test stopped finding it; " +
			"both mean the mirror is no longer being checked")
	}
}
