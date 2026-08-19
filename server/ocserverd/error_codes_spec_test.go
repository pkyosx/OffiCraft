package main

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

// The server edge of the ONE status → envelope-code table, docs/design/api-error-envelope.codes.json.
//
// 🔴 WHY (T-fd2c): the frontend's mock adapter answered 400 with `bad_request`
// — a code errorCodeForStatus has never produced (400/422 are
// `validation_error`; an unmapped 4xx is `client_error`). Nothing was red,
// because each side only ever confronted its own hand-copy: the Go map, the
// Python CODE_BY_STATUS in conformance/test_error_envelope.py, and the mock's
// per-call-site literals were three restatements of one table with no seam
// between them. This test is that seam on the Go side: the map is now pinned
// cell-by-cell — fallback buckets included — against the file the frontend
// derives its codes from and conformance pins its own table against.
//
// ⚠️ Go's test cache does NOT track ../../docs/design/api-error-envelope.codes.json, so editing only
// the JSON and re-running `go test` locally can hand back a CACHED ok. Same
// trap and same answer as TestSSETopicsMatchSpec: pass -count=1 when you run
// this by hand after touching the spec (CI's bin/tests/go-test-nocache-guard.sh
// requires -count=1 on every `go test` invocation, so no CI green is a replay).
//
// Same precedent as the openapi / mcp-catalog / sse.md readers in this package:
// os.ReadFile("../../spec/…"), not go:embed — embed cannot reach outside the
// module directory, and this asset must be readable by the frontend and by
// conformance too.

type errorCodeSpec struct {
	ByStatus      map[string]string `json:"by_status"`
	Fallback5xx   string            `json:"fallback_5xx"`
	FallbackOther string            `json:"fallback_other"`
}

func readErrorCodeSpec(t *testing.T) errorCodeSpec {
	t.Helper()
	raw, err := os.ReadFile("../../docs/design/api-error-envelope.codes.json")
	if err != nil {
		t.Fatalf("read docs/design/api-error-envelope.codes.json: %v", err)
	}
	var spec errorCodeSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse docs/design/api-error-envelope.codes.json: %v", err)
	}
	if len(spec.ByStatus) == 0 || spec.Fallback5xx == "" || spec.FallbackOther == "" {
		t.Fatalf("docs/design/api-error-envelope.codes.json parsed empty (%+v) — the guard would be "+
			"vacuous; the file's shape changed", spec)
	}
	return spec
}

func TestErrorCodeForStatusMatchesSpec(t *testing.T) {
	spec := readErrorCodeSpec(t)

	for raw, want := range spec.ByStatus {
		status, err := strconv.Atoi(raw)
		if err != nil {
			t.Fatalf("docs/design/api-error-envelope.codes.json: by_status key %q is not a status", raw)
		}
		if got := errorCodeForStatus(status); got != want {
			t.Errorf("errorCodeForStatus(%d) = %q, docs/design/api-error-envelope.codes.json says %q",
				status, got, want)
		}
	}

	// The two fallback branches are part of the contract, not leftovers: an
	// unmapped 4xx is what a frontend that invents its own code drifts into.
	fallbacks := 0
	for status := 100; status < 600; status++ {
		if _, mapped := spec.ByStatus[strconv.Itoa(status)]; mapped {
			continue
		}
		want := spec.FallbackOther
		if status >= 500 {
			want = spec.Fallback5xx
		}
		if got := errorCodeForStatus(status); got != want {
			t.Errorf("errorCodeForStatus(%d) = %q, docs/design/api-error-envelope.codes.json fallback says %q",
				status, got, want)
		}
		fallbacks++
	}
	if fallbacks < 100 {
		t.Fatalf("only %d unmapped statuses swept — the loop is mis-rooted", fallbacks)
	}
}

// The vocabulary is CLOSED: every code the server can ever put on the wire is
// one the spec file names. A new code added to errorCodeForStatus without a row
// here would leave the frontend and conformance unable to name it.
func TestErrorCodeVocabularyIsClosed(t *testing.T) {
	spec := readErrorCodeSpec(t)
	known := map[string]bool{spec.Fallback5xx: true, spec.FallbackOther: true}
	for _, code := range spec.ByStatus {
		known[code] = true
	}
	for status := 100; status < 600; status++ {
		if code := errorCodeForStatus(status); !known[code] {
			t.Errorf("errorCodeForStatus(%d) = %q, which docs/design/api-error-envelope.codes.json does not name",
				status, code)
		}
	}
}
