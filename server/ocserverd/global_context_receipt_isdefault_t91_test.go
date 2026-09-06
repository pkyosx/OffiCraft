package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// globalContextReceiptDTO.is_default must ANSWER THE FOLD, not the verb.
//
// 🔴 WHY THIS FILE EXISTS. `is_default` is the one place in T-91 where a VALUE
// changed rather than a field: the replace handler used to stamp `false`
// unconditionally, and the reshape made both verbs read the flag back off the
// fold instead. Nothing pinned that. A regression to the hard-coded literal
// would be right for replace and WRONG FOR RESET — reset's whole job is to put
// the document back to the shipped default, so a reset that reported
// is_default:false would be telling the cockpit the block is still customised
// the moment it stopped being.
//
// The reason the two verbs can be wrong in one place is that they SHARE one
// constructor: globalContextReceiptOf (api_roles.go), whose own comment says so
// verbatim — "Both write verbs go through it so they cannot answer with two
// different shapes for one document." That is exactly why one test must drive
// BOTH verbs: a fixture that only exercised replace would go green on a
// build that had hard-coded false back in.
//
// It reads the raw JSON rather than the struct because absence, `false` and
// `true` must stay three distinguishable answers, and because the key set is
// asserted at the same time — decoding into globalContextReceiptDTO would
// discard anything extra and let a whole-document echo pass.

func globalContextReceiptRaw(t *testing.T, rec *httptest.ResponseRecorder, what string) map[string]json.RawMessage {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", what, rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("%s: decode: %v (%s)", what, err, rec.Body.String())
	}
	// Anti-vacuity, and the T-91 shape in one line: a build that answered the
	// whole document would carry `text` and fail here, so the is_default
	// assertions below cannot be satisfied by a fatter body.
	if len(raw) != 3 {
		t.Fatalf("%s: receipt keys = %v, want exactly is_default/size_chars/sha256: %s",
			what, keysOf(raw), rec.Body.String())
	}
	for _, want := range []string{"is_default", "size_chars", "sha256"} {
		if _, ok := raw[want]; !ok {
			t.Fatalf("%s: receipt is missing %q: %s", what, want, rec.Body.String())
		}
	}
	return raw
}

func keysOf(raw map[string]json.RawMessage) []string {
	out := make([]string, 0, len(raw))
	for k := range raw {
		out = append(out, k)
	}
	return out
}

func TestGlobalContextReceiptIsDefaultFollowsTheFoldNotTheVerb(t *testing.T) {
	api := newTasksTestServer(t)

	// Replace: an overlay now exists, so the block is no longer the default.
	replace := httptest.NewRecorder()
	api.HandleReplaceGlobalContextApiGlobalContextPost(replace,
		taskReq(t, "POST", "/api/global-context",
			map[string]any{"text": "the owner's own block"}, "owner", "owner"))
	raw := globalContextReceiptRaw(t, replace, "POST /api/global-context")
	if got := string(raw["is_default"]); got != "false" {
		t.Fatalf("replace_global_context reported is_default=%s, want false — an "+
			"overlay exists, so this block is not the shipped default: %s",
			got, replace.Body.String())
	}

	// Reset: the overlay is gone, so the SAME constructor must now say true.
	// This is the half a hard-coded `false` gets wrong, and the half no test
	// held before T-91.
	reset := httptest.NewRecorder()
	api.HandleResetGlobalContextApiGlobalContextResetPost(reset,
		taskReq(t, "POST", "/api/global-context/reset", nil, "owner", "owner"))
	raw = globalContextReceiptRaw(t, reset, "POST /api/global-context/reset")
	if got := string(raw["is_default"]); got != "true" {
		t.Fatalf("reset_global_context reported is_default=%s, want true — reset is "+
			"the way back to the shipped default, and a receipt that still says "+
			"customised is the one answer this verb can never give: %s",
			got, reset.Body.String())
	}

	// The read face agrees, so the receipt is a projection of the stored
	// document and not a per-verb constant that happens to match.
	read := httptest.NewRecorder()
	api.HandleGetGlobalContextApiGlobalContextGet(read,
		taskReq(t, "GET", "/api/global-context", nil, "owner", "owner"))
	if read.Code != http.StatusOK {
		t.Fatalf("GET /api/global-context: %d %s", read.Code, read.Body.String())
	}
	served := decodeBody[globalContextDTO](t, read)
	if !served.IsDefault {
		t.Fatalf("after reset the read face still calls the block customised: %s",
			read.Body.String())
	}
}
