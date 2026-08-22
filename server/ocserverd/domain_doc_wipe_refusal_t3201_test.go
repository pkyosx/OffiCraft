package main

// domain_doc_wipe_refusal_t3201_test.go — the wipe refusal, folded into
// docWipeRefusal (domain.go), must say EXACTLY what it said before the fold.
//
// This is the one place in this repo where comparing a full sentence is the
// point rather than a smell: the fold was sold as "no caller reads a different
// byte", and only a literal, whole-sentence comparison can hold it to that.
//
// 🔴 THE EXPECTED SENTENCES ARE WRITTEN OUT BY HAND HERE, ON PURPOSE. They must
// never be built from docWipeRefusal, from spec.DocName, or from any other
// constant the production code also reads — a test that derives its expectation
// from the thing under test agrees with every rewording, which is precisely the
// change this file exists to catch.
//
// Each case drives the REAL handler, so it pins the arguments the call site
// passes (doc name AND the way-out clause), not just the formatter.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWipeRefusalsAreByteIdenticalToWhatEachSeamSaidBefore(t *testing.T) {
	cases := []struct {
		name string
		// refuse seeds the document with content (so the guard has something to
		// protect) and returns the recorder of the write that tries to empty it.
		refuse func(t *testing.T, api *apiServer) *httptest.ResponseRecorder
		want   string
	}{
		{
			name: "global_context",
			refuse: func(t *testing.T, api *apiServer) *httptest.ResponseRecorder {
				writeGlobalContextOn(t, api, "some global context")
				return writeGlobalContextOn(t, api, "")
			},
			want: "this would replace the existing global context with an empty one — pass allow_shrink=true " +
				"if that is intended, or use reset_global_context; nothing was written",
		},
		{
			name: "lessons",
			refuse: func(t *testing.T, api *apiServer) *httptest.ResponseRecorder {
				writeLearningOn(t, api, seedRoleAssistant, "some lessons")
				return writeLearningOn(t, api, seedRoleAssistant, "")
			},
			want: "this would replace the existing lessons doc with an empty one — pass allow_shrink=true " +
				"if that is intended; nothing was written",
		},
		{
			name: "insight",
			refuse: func(t *testing.T, api *apiServer) *httptest.ResponseRecorder {
				writeInsightOn(t, api, seedRoleAssistant, "some insight")
				return writeInsightOn(t, api, seedRoleAssistant, "")
			},
			want: "this would replace the existing insight doc with an empty one — pass allow_shrink=true " +
				"if that is intended; nothing was written",
		},
		{
			name: "system_interaction",
			refuse: func(t *testing.T, api *apiServer) *httptest.ResponseRecorder {
				writeSystemInteractionOn(t, api, "some system interaction")
				return writeSystemInteractionOn(t, api, "")
			},
			want: "this would replace the existing system interaction block with an empty one — pass allow_shrink=true " +
				"if that is intended, or reset it to the shipped default; nothing was written",
		},
		{
			// The boot documents interpolate a per-document name, so one of them
			// is not enough: this case is what catches a fold that hardcoded the
			// system-interaction name for every kind.
			name: "boot_sequence_claude",
			refuse: func(t *testing.T, api *apiServer) *httptest.ResponseRecorder {
				writeBootSequenceOn(t, api, "claude", "some boot sequence")
				return writeBootSequenceOn(t, api, "claude", "")
			},
			want: "this would replace the existing boot sequence (claude) with an empty one — pass allow_shrink=true " +
				"if that is intended, or reset it to the shipped default; nothing was written",
		},
		{
			// 🔴 NOT folded into docWipeRefusal — its skeleton differs ("with an
			// empty DOC", and the name carries no "doc" suffix). It is asserted
			// here anyway so the decision to leave it alone is a decision the
			// build defends, rather than an oversight the next tidy-up erases.
			name: "task_manual_learnings",
			refuse: func(t *testing.T, api *apiServer) *httptest.ResponseRecorder {
				key := seedManualWithLearnings(t, api, "some learnings")
				return writeLearnings(t, api, key, map[string]any{"text": ""})
			},
			want: "this would replace the existing learnings with an empty doc — pass allow_shrink=true " +
				"if that is intended; nothing was written",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			api := newTasksTestServer(t)
			rec := c.refuse(t, api)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("wiping %s must be refused; got %d %s", c.name, rec.Code, rec.Body.String())
			}
			if got := errorMessage(t, rec); got != c.want {
				t.Fatalf("the %s wipe refusal changed:\n got %q\nwant %q", c.name, got, c.want)
			}
		})
	}
}

func writeGlobalContextOn(t *testing.T, api *apiServer, text string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReplaceGlobalContextApiGlobalContextPost(rec,
		ownerReq(t, http.MethodPost, "/api/global-context", map[string]any{"text": text}))
	return rec
}

func writeSystemInteractionOn(t *testing.T, api *apiServer, text string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReplaceSystemInteractionApiSystemInteractionPost(rec,
		ownerReq(t, http.MethodPost, "/api/system-interaction", map[string]any{"text": text}))
	return rec
}

func writeBootSequenceOn(t *testing.T, api *apiServer, runtimeKey, text string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleReplaceBootSequenceApiBootSequenceRuntimeKeyPost(rec,
		ownerReq(t, http.MethodPost, "/api/boot-sequence/"+runtimeKey, map[string]any{"text": text}), runtimeKey)
	return rec
}
