// Skeleton generated from server/ocserverd/api_bootdocs.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import "testing"

func TestBootDocRegFor(t *testing.T) {
	t.Skip("TODO: bootDocRegFor finds the row for a kind.")
}

func TestServes(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestMustBootDocSpec(t *testing.T) {
	t.Skip("TODO: mustBootDocSpec resolves a pair this binary is built with.")
}

func TestBootDocSpecFor(t *testing.T) {
	t.Skip("TODO: bootDocSpecFor resolves ANY (kind, key) pair naming an editable boot-context block — the form the document-history faces address documents in.")
}

func TestBootDocHistoryKeyKnown(t *testing.T) {
	t.Skip("TODO: bootDocHistoryKeyKnown is bootDocSpecFor's server-free half: does this (kind, key) name one of these documents at all?")
}

func TestUnknownBootDocKeyMsg(t *testing.T) {
	t.Skip("TODO: unknownBootDocKeyMsg names the keys that DO exist for this kind, for the same reason writeUnknownBootSequence does: a caller holding a typo needs to be able to tell it from a document that is simply empty.")
}

func TestFoldBootDocDTO(t *testing.T) {
	t.Skip("TODO: 🔴 DocName IS USER-FACING PROSE, NOT AN IDENTIFIER (T-6f44).")
}

func TestSystemInteractionText(t *testing.T) {
	t.Skip("TODO: systemInteractionText / bootSequenceText are what the BOOT FOLDS read (buildBootContext for staff, worker_sharedcore.go for outsource).")
}

func TestWinddownNoticeText(t *testing.T) {
	t.Skip("TODO: winddownNoticeText is the WHOLE notice a member being wound down receives: the document for this kind, its {variables} filled from the live facts, and its two halves joined the way this kind joins them (T-3201).")
}

func TestTaskEventBodyText(t *testing.T) {
	t.Skip("TODO: taskEventBodyText is the EDITABLE HALF of a task-event document, with no read-only head — for a caller that wants the document's INSTRUCTIONS without its statement of fact.")
}

func TestTaskNoticeText(t *testing.T) {
	t.Skip("TODO: taskNoticeText is the WHOLE chat notice one TASK event posts to the executor it concerns: the document for this kind, its {variables} filled from the live task facts, and its two halves joined the way this kind joins them (T-3201).")
}

func TestEventNoticeText(t *testing.T) {
	t.Skip("TODO: eventNoticeText is the one road from a document to the bytes an agent reads: fold the overlay over the seed, fill the names this kind declares, join the halves.")
}

func TestBootSequenceText(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestBootDocSnapshotIn(t *testing.T) {
	t.Skip("TODO: bootDocSnapshotIn is what SaveWithDocumentHistory calls from INSIDE the write transaction — the same posture insightSnapshotIn takes, and for the same reason: the retained revision must be the state THIS write replaced, not a value the handler folded earlier, or two racing writers retain one common ancestor and the version written between them becomes unrecoverable.")
}

func TestBootDocHistorySnapshot(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestPublishBootDoc(t *testing.T) {
	t.Skip("TODO: publishBootDoc fans the change on the `global_context` topic.")
}

func TestWriteBootDoc(t *testing.T) {
	t.Skip("TODO: writeBootDoc is the one write path shared by replace and reset, and the one place the no-op rule lives.")
}

func TestReplaceBootDoc(t *testing.T) {
	t.Skip("TODO: replaceBootDoc is the whole-BODY replace shared by every write face.")
}

func TestBootDocReceiptOf(t *testing.T) {
	t.Skip("TODO: bootDocReceiptOf reduces the READ face's fold to the WRITE face's receipt (T-91).")
}

func TestBootDocStoredText(t *testing.T) {
	t.Skip("TODO: bootDocStoredText turns a caller's BODY into the bytes that get stored, by joining the SHIPPED head back on.")
}

func TestBootDocBodyOf(t *testing.T) {
	t.Skip("TODO: bootDocBodyOf reads the EDITABLE half out of a stored document.")
}

func TestBootDocBodyRefusal(t *testing.T) {
	t.Skip("TODO: bootDocBodyRefusal is the ONE content rule left on the write face: the editable half names no variables.")
}

func TestResetBootDoc(t *testing.T) {
	t.Skip("TODO: resetBootDoc tombstones the overlay so the folded read falls back to the SHIPPED seed.")
}

func TestHandleGetSystemInteractionApiSystemInteractionGet(t *testing.T) {
	t.Run("a well-formed GET /api/system-interaction answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/system-interaction request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/system-interaction reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/system-interaction request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleReplaceSystemInteractionApiSystemInteractionPost(t *testing.T) {
	t.Run("a replace answers the receipt over the stored document and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "system_interaction",
			"key":        "global",
			"is_default": false,
			"size_chars": 2,
			"cap_chars":  60000,
			"sha256":     "3696ad59777e09d5f2daacb544022d435e7cd053d87c5781241fabd8fdf2a90d",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("writing the same body twice changes nothing the second time and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "system_interaction",
			"key":        "global",
			"is_default": false,
			"size_chars": 2,
			"cap_chars":  60000,
			"sha256":     "3696ad59777e09d5f2daacb544022d435e7cd053d87c5781241fabd8fdf2a90d",
		})
		dashboard.wantFrames()
	})

	t.Run("emptying a document that had content answers 400 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":""}`)
		if status != 400 {
			t.Fatalf("want 400, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "this would replace the existing system interaction block with an empty one — pass allow_shrink=true if that is intended, or reset it to the shipped default; nothing was written")
		dashboard.wantFrames()
	})

	t.Run("emptying a document with allow_shrink answers the empty receipt and still fans the delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"","allow_shrink":true}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "system_interaction",
			"key":        "global",
			"is_default": false,
			"size_chars": 0,
			"cap_chars":  60000,
			"sha256":     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
	})

	t.Run("a body carrying a key this route does not declare answers 422 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1","note":"x"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: json: unknown field \"note\"")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/system-interaction", "", `{"body":"S1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleResetSystemInteractionApiSystemInteractionResetPost(t *testing.T) {
	t.Run("resetting an edited document answers the shipped receipt flagged default and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/system-interaction", owner, `{"body":"S1"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "system_interaction",
			"key":        "global",
			"is_default": true,
			"size_chars": 16617,
			"cap_chars":  60000,
			"sha256":     "ee4f1371c5600a6f538d9ce03d22b869c62cb19356bb9a6fdc8bb93f1d3fda58",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("resetting a document nobody has edited changes nothing and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/system-interaction/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "system_interaction",
			"key":        "global",
			"is_default": true,
			"size_chars": 16617,
			"cap_chars":  60000,
			"sha256":     "ee4f1371c5600a6f538d9ce03d22b869c62cb19356bb9a6fdc8bb93f1d3fda58",
		})
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/system-interaction/reset", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetOffboardApiOffboardGet(t *testing.T) {
	t.Run("a well-formed GET /api/offboard answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/offboard request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/offboard reaches this handler and no other row", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/offboard request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleReplaceOffboardApiOffboardPost(t *testing.T) {
	t.Run("a replace answers the receipt over the stored document and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "offboard",
			"key":        "global",
			"is_default": false,
			"size_chars": 2,
			"cap_chars":  15000,
			"sha256":     "5b1f94750d53ab84b5f3fb4bf25b8bf15b162f800f004ae2709b56fdc53a5fb7",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/offboard", "", `{"body":"O1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleResetOffboardApiOffboardResetPost(t *testing.T) {
	t.Run("resetting an edited document answers the shipped receipt flagged default and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/offboard", owner, `{"body":"O1"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/offboard/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "offboard",
			"key":        "global",
			"is_default": true,
			"size_chars": 1770,
			"cap_chars":  15000,
			"sha256":     "67ee17b8a0747672b862d3f167f26e848eb8eca9c0417993bfc583bd311a2b9b",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/offboard/reset", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleGetBootSequenceApiBootSequenceRuntimeKeyGet(t *testing.T) {
	t.Run("a well-formed GET /api/boot-sequence/{runtime_key} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/boot-sequence/{runtime_key} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/boot-sequence/{runtime_key} reaches this handler with runtime_key bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/boot-sequence/{runtime_key} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleReplaceBootSequenceApiBootSequenceRuntimeKeyPost(t *testing.T) {
	t.Run("a replace of one runtime's steps answers the receipt and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/claude", owner, `{"body":"B1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "boot_sequence",
			"key":        "claude",
			"is_default": false,
			"size_chars": 2,
			"cap_chars":  15000,
			"sha256":     "5b950e77941d01cdf246d00b1ece546bc95234b77d98b44c9187e2733afa696a",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a runtime with no boot sequence answers 404 naming the runtimes that have one and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/Codex", owner, `{"body":"B1"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "no boot sequence for runtime 'Codex' — the runtimes with their own boot sequence are 'claude' and 'codex'")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/claude", "", `{"body":"B1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleResetBootSequenceApiBootSequenceRuntimeKeyResetPost(t *testing.T) {
	t.Run("resetting one runtime's edited steps answers the shipped receipt flagged default and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-sequence/claude", owner, `{"body":"B1"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/claude/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "boot_sequence",
			"key":        "claude",
			"is_default": true,
			"size_chars": 3124,
			"cap_chars":  15000,
			"sha256":     "793565bdb6fb6e013666f64fed90e925d60b06023a465f8e42c91fc6cd0cb622",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a runtime with no boot sequence answers 404 naming the runtimes that have one and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/Codex/reset", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "no boot sequence for runtime 'Codex' — the runtimes with their own boot sequence are 'claude' and 'codex'")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/boot-sequence/claude/reset", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestWriteUnknownBootSequence(t *testing.T) {
	t.Skip("TODO: writeUnknownBootSequence NAMES the runtimes that exist.")
}

func TestBootDocReadOnlyRefusal(t *testing.T) {
	t.Skip("TODO: bootDocReadOnlyRefusal is the ONE sentence every write face answers for a read-only document, the way docCapRefusal and docWipeRefusal are the one text behind their gates.")
}

func TestGenericBootDocSpec(t *testing.T) {
	t.Skip("TODO: genericBootDocSpec resolves the {kind}/{key} pair the three generic faces take, answering the SAME refusal all three times: a kind nobody registered says so, and a key that kind does not serve is told which keys it does.")
}

func TestHandleGetBootDocApiBootDocsKindKeyGet(t *testing.T) {
	t.Run("a well-formed GET /api/boot-docs/{kind}/{key} answers 200", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/boot-docs/{kind}/{key} request without a token answers 401", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a request to GET /api/boot-docs/{kind}/{key} reaches this handler with kind, key bound from the path", func(t *testing.T) { t.Skip("TODO") })
	t.Run("a GET /api/boot-docs/{kind}/{key} request the wire layer rejects (malformed body, wrong content type, over the size cap) answers a 4xx without reaching the domain", func(t *testing.T) { t.Skip("TODO") })
}

func TestHandleReplaceBootDocApiBootDocsKindKeyPost(t *testing.T) {
	t.Run("a replace addressed by kind and key answers the receipt and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner, `{"body":"C1"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "task_closeout",
			"key":        "global",
			"is_default": false,
			"size_chars": 77,
			"cap_chars":  15000,
			"sha256":     "90b30624f1af74c931c34589a098966ba90f28247e3f7278e409797f387fa4bb",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   1,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   1,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a kind this server does not serve answers 404 and writes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/bogus/global", owner, `{"body":"C1"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "document history kind 'bogus' names no editable document on this server")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", "", `{"body":"C1"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleResetBootDocApiBootDocsKindKeyResetPost(t *testing.T) {
	t.Run("resetting an edited document addressed by kind and key answers the shipped receipt and fans the owner-only global_context delta", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global", owner, `{"body":"C1"}`)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global/reset", owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"kind":       "task_closeout",
			"key":        "global",
			"is_default": true,
			"size_chars": 511,
			"cap_chars":  15000,
			"sha256":     "62a219c766233550b8c5c91e1a6f30a5b6d9833019e60cd302e50616a34d0108",
		})
		dashboard.wantFrames(map[string]any{
			"seq":   2,
			"topic": "global_context",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "global_context",
				"key":     "owner",
				"epoch":   2,
				"deleted": false,
				"payload": nil,
			},
			"ts":      apiAnyNumber,
			"trigger": "owner",
		})
		bystander.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/boot-docs/task_closeout/global/reset", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}
