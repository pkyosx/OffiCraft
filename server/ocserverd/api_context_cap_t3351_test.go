package main

// api_context_cap_t3351_test.go — T-3351: the HARD SIZE CAP on the accumulating
// context documents (a role's lessons doc; a task manual's learnings and
// sop_md).
//
// Owner ruling, two sentences and one number: an update may not push the doc
// past the cap (a setting since T-3aeb; see contextDocMaxCharsDefault for the
// shipped default), and whatever is ALREADY over that is not truncated —
// its next update may only make it smaller.
//
// This is a FAIL-CLOSED gate on the seam an agent uses to hand its experience
// forward, so the four properties below are pinned SEPARATELY and each names
// what it costs to lose:
//
//	(a) over-cap writes are actually refused      — the gate exists at all;
//	(b) legal writes are not caught in the net    — a false refusal silently
//	    destroys a whole session's learnings at handover;
//	(c) an over-cap doc may STILL shrink          — the most expensive one to
//	    lose: six live docs are already over the cap, and without this they
//	    could never be edited again, not even to get smaller;
//	(d) equal length while over-cap is refused    — not shrinking is not
//	    converging.
//
// Every seam is driven through its real handler, and every refusal asserts on a
// READ-BACK of the stored doc: a gate that refuses but writes anyway would
// otherwise pass on the status code alone.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// capDropAnchor is a unique anchor a patch can address; capKeepAnchor has the
// SAME rune length, so replacing one with the other is an exactly-equal-length
// rewrite (case (d) on the patch seams).
const (
	capDropAnchor = "«DROP-THIS-SECTION»"
	capKeepAnchor = "«KEEP-THIS-SECTION»"
)

// capDoc builds a doc of n runes: filler, the unique drop anchor, more filler.
// The two fillers use different letters so no shorter substring of one is
// accidentally an anchor in the other.
func capDoc(t *testing.T, n int) string {
	t.Helper()
	body := n - utf8.RuneCountInString(capDropAnchor)
	if body < 2 {
		t.Fatalf("capDoc: n=%d too small", n)
	}
	head := body * 9 / 10
	doc := strings.Repeat("a", head) + capDropAnchor + strings.Repeat("b", body-head)
	if got := utf8.RuneCountInString(doc); got != n {
		t.Fatalf("capDoc built %d runes, want %d", got, n)
	}
	return doc
}

// ── lessons seams (REST, the wired stack an agent actually calls) ────────────

func capLessonsServer(t *testing.T) (*httptest.Server, *DAL, string) {
	t.Helper()
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	return srv, dal, ownerTok
}

func replaceLessons(t *testing.T, srv *httptest.Server, tok, text string, allowShrink bool) (int, map[string]any) {
	t.Helper()
	body := map[string]any{"text": text}
	if allowShrink {
		body["allow_shrink"] = true
	}
	blob, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return doJSON(t, "POST", srv.URL+"/api/lessons/assistant/general", tok, string(blob))
}

// capErrMessage pulls the refusal text out of the flat error envelope.
func capErrMessage(data map[string]any) string {
	env, _ := data["error"].(map[string]any)
	msg, _ := env["message"].(string)
	return msg
}

// (a) The gate exists: a write that pushes an in-bounds doc past the cap is
// refused, and the stored doc is untouched.
func TestContextDocCap_OverCapWriteIsRefused(t *testing.T) {
	t.Run("replace_lessons", func(t *testing.T) {
		srv, dal, tok := capLessonsServer(t)
		before := capDoc(t, contextDocMaxCharsDefault-10)
		seedLessonsOverlay(t, dal, "assistant", "general", before)

		status, data := replaceLessons(t, srv, tok, capDoc(t, contextDocMaxCharsDefault+1), false)
		if status != http.StatusBadRequest {
			t.Fatalf("over-cap replace must be refused, got %d: %v", status, data)
		}
		if got := getLessonsText(t, srv.URL, tok, "assistant", "general"); got != before {
			t.Fatalf("refused write must leave the doc byte-identical (%d runes stored)",
				utf8.RuneCountInString(got))
		}
	})

	t.Run("patch_lessons", func(t *testing.T) {
		srv, dal, tok := capLessonsServer(t)
		before := capDoc(t, contextDocMaxCharsDefault-10)
		seedLessonsOverlay(t, dal, "assistant", "general", before)

		// The patch itself is tiny; what it PRODUCES is over the cap. The gate
		// must judge the result, not the edit.
		status, data := patchLessons(t, srv.URL, tok, "assistant", "general",
			`{"edits":[{"old":"","new":"`+strings.Repeat("z", 100)+`"}]}`)
		if status != http.StatusBadRequest {
			t.Fatalf("patch whose RESULT is over-cap must be refused, got %d: %v", status, data)
		}
		if got := getLessonsText(t, srv.URL, tok, "assistant", "general"); got != before {
			t.Fatalf("refused patch must write nothing")
		}
	})

	t.Run("write_task_learnings", func(t *testing.T) {
		api := newTasksTestServer(t)
		before := capDoc(t, contextDocMaxCharsDefault-10)
		key := seedManualWithLearnings(t, api, before)

		rec := writeLearnings(t, api, key, map[string]any{"text": capDoc(t, contextDocMaxCharsDefault+1)})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("over-cap learnings replace must be refused, got %d: %s", rec.Code, rec.Body.String())
		}
		if got := storedLearnings(t, api, key); got != before {
			t.Fatalf("refused write must leave learnings byte-identical")
		}
	})

	t.Run("patch_task_learnings", func(t *testing.T) {
		api := newTasksTestServer(t)
		before := capDoc(t, contextDocMaxCharsDefault-10)
		key := seedManualWithLearnings(t, api, before)

		status, data := patchLearnings(t, api, key, map[string]any{
			"edits": []any{edit("", strings.Repeat("z", 100))},
		})
		if status != http.StatusBadRequest {
			t.Fatalf("patch whose RESULT is over-cap must be refused, got %d: %v", status, data)
		}
		if got := storedLearnings(t, api, key); got != before {
			t.Fatalf("refused patch must write nothing")
		}
	})

	// update_task_manual is one of TWO write faces for sop_md (patch_task_sop is
	// the other, and judges the same cap on the RESULT of its patch) and a
	// SECOND write face for learnings — capping only the learnings-specific
	// tools would have left an uncapped door onto the same document.
	t.Run("update_task_manual_learnings", func(t *testing.T) {
		api := newTasksTestServer(t)
		before := capDoc(t, contextDocMaxCharsDefault-10)
		key := seedManualWithLearnings(t, api, before)

		rec := capUpdateManual(t, api, key, map[string]any{
			"learnings": capDoc(t, contextDocMaxCharsDefault+1),
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("over-cap learnings via update_task_manual must be refused, got %d: %s",
				rec.Code, rec.Body.String())
		}
		if got := storedLearnings(t, api, key); got != before {
			t.Fatalf("refused update must leave learnings byte-identical")
		}
	})

	t.Run("update_task_manual_sop_md", func(t *testing.T) {
		api := newTasksTestServer(t)
		key := seedManualWithLearnings(t, api, "")
		before := capDoc(t, contextDocMaxCharsDefault-10)
		setManualSopMD(t, api, key, before)

		rec := capUpdateManual(t, api, key, map[string]any{
			"sop_md": capDoc(t, contextDocMaxCharsDefault+1),
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("over-cap sop_md must be refused, got %d: %s", rec.Code, rec.Body.String())
		}
		if got := storedSopMD(t, api, key); got != before {
			t.Fatalf("refused update must leave sop_md byte-identical")
		}
	})
}

// (b) The net does not catch legal writes. Losing this is not a cosmetic
// annoyance: a false refusal at handover drops that session's learnings on the
// floor with nobody watching.
func TestContextDocCap_LegalWritesAreNotRefused(t *testing.T) {
	t.Run("replace_lessons_at_the_cap_exactly", func(t *testing.T) {
		srv, dal, tok := capLessonsServer(t)
		seedLessonsOverlay(t, dal, "assistant", "general", capDoc(t, 500))

		want := capDoc(t, contextDocMaxCharsDefault) // exactly L — the ≤ boundary
		status, data := replaceLessons(t, srv, tok, want, false)
		if status != http.StatusOK {
			t.Fatalf("a doc of exactly %d chars must be admitted, got %d: %v",
				contextDocMaxCharsDefault, status, data)
		}
		if got := getLessonsText(t, srv.URL, tok, "assistant", "general"); got != want {
			t.Fatalf("admitted write did not land")
		}
	})

	t.Run("patch_lessons_growing_within_the_cap", func(t *testing.T) {
		srv, dal, tok := capLessonsServer(t)
		seedLessonsOverlay(t, dal, "assistant", "general", capDoc(t, 500))

		status, data := patchLessons(t, srv.URL, tok, "assistant", "general",
			`{"edits":[{"old":"","new":"\nan ordinary lesson learned this session\n"}]}`)
		if status != http.StatusOK {
			t.Fatalf("ordinary append must land, got %d: %v", status, data)
		}
		if !strings.Contains(getLessonsText(t, srv.URL, tok, "assistant", "general"),
			"an ordinary lesson learned this session") {
			t.Fatalf("admitted append did not land")
		}
	})

	t.Run("write_task_learnings_within_the_cap", func(t *testing.T) {
		api := newTasksTestServer(t)
		key := seedManualWithLearnings(t, api, capDoc(t, 500))

		want := capDoc(t, contextDocMaxCharsDefault)
		if rec := writeLearnings(t, api, key, map[string]any{"text": want}); rec.Code != http.StatusOK {
			t.Fatalf("in-bounds learnings write must land, got %d: %s", rec.Code, rec.Body.String())
		}
		if storedLearnings(t, api, key) != want {
			t.Fatalf("admitted write did not land")
		}
	})

	t.Run("patch_task_learnings_growing_within_the_cap", func(t *testing.T) {
		api := newTasksTestServer(t)
		key := seedManualWithLearnings(t, api, capDoc(t, 500))

		status, data := patchLearnings(t, api, key, map[string]any{
			"edits": []any{edit("", "\nan ordinary learning\n")},
		})
		if status != http.StatusOK {
			t.Fatalf("ordinary append must land, got %d: %v", status, data)
		}
		if !strings.Contains(storedLearnings(t, api, key), "an ordinary learning") {
			t.Fatalf("admitted append did not land")
		}
	})

	t.Run("update_task_manual_within_the_cap", func(t *testing.T) {
		api := newTasksTestServer(t)
		key := seedManualWithLearnings(t, api, capDoc(t, 500))

		wantSop := capDoc(t, contextDocMaxCharsDefault)
		wantLearn := capDoc(t, contextDocMaxCharsDefault)
		rec := capUpdateManual(t, api, key, map[string]any{
			"sop_md": wantSop, "learnings": wantLearn,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("in-bounds partial update must land, got %d: %s", rec.Code, rec.Body.String())
		}
		if storedSopMD(t, api, key) != wantSop || storedLearnings(t, api, key) != wantLearn {
			t.Fatalf("admitted update did not land")
		}
	})

	// The unit is RUNES, not bytes. These docs are largely Chinese prose at
	// ~3 bytes/char: measured with len() this 9,000-character doc is ~27,000
	// and would be refused, which is a cap of ~3,300 Chinese characters —
	// nowhere near the number the owner signed off on.
	t.Run("chinese_prose_is_measured_in_runes", func(t *testing.T) {
		srv, dal, tok := capLessonsServer(t)
		seedLessonsOverlay(t, dal, "assistant", "general", "起點")

		want := strings.Repeat("教訓", 4500) // 9,000 runes, 27,000 bytes
		if utf8.RuneCountInString(want) > contextDocMaxCharsDefault || len(want) <= contextDocMaxCharsDefault {
			t.Fatalf("fixture must be under the cap in runes and over it in bytes: %d runes, %d bytes",
				utf8.RuneCountInString(want), len(want))
		}
		status, data := replaceLessons(t, srv, tok, want, false)
		if status != http.StatusOK {
			t.Fatalf("a 9,000-CHARACTER doc must be admitted (the cap counts runes), got %d: %v",
				status, data)
		}
		if getLessonsText(t, srv.URL, tok, "assistant", "general") != want {
			t.Fatalf("admitted write did not land")
		}
	})
}

// (c) 🔴 The escape hatch. Six documents were ALREADY over the cap when this
// was written (lessons 43,029 and 12,132 runes; manual learnings 19,336 /
// 14,691 / 11,031 / 10,557). The cap is a SETTING and its shipped default has
// moved since, so the fixtures below are written as contextDocMaxCharsDefault +
// <the same offsets those six sat at>: the property under test is "already over
// the cap", not any particular size.
// Existing content is never truncated, so if a still-over-cap
// result were refused, those six could never be edited again — not even to get
// smaller. This is the single most expensive property in the file to lose.
func TestContextDocCap_OverCapDocMayStillShrink(t *testing.T) {
	t.Run("replace_lessons", func(t *testing.T) {
		srv, dal, tok := capLessonsServer(t)
		seedLessonsOverlay(t, dal, "assistant", "general", capDoc(t, contextDocMaxCharsDefault+33029))

		want := capDoc(t, contextDocMaxCharsDefault+30000) // still far over the cap, but SHORTER
		status, data := replaceLessons(t, srv, tok, want, false)
		if status != http.StatusOK {
			t.Fatalf("an over-cap doc must still be allowed to shrink, got %d: %v", status, data)
		}
		if getLessonsText(t, srv.URL, tok, "assistant", "general") != want {
			t.Fatalf("the shrinking write did not land")
		}
	})

	t.Run("patch_lessons", func(t *testing.T) {
		srv, dal, tok := capLessonsServer(t)
		before := capDoc(t, contextDocMaxCharsDefault+33029)
		seedLessonsOverlay(t, dal, "assistant", "general", before)

		status, data := patchLessons(t, srv.URL, tok, "assistant", "general",
			`{"edits":[{"old":"`+capDropAnchor+`","new":""}]}`)
		if status != http.StatusOK {
			t.Fatalf("an over-cap doc must still be patchable downward, got %d: %v", status, data)
		}
		got := getLessonsText(t, srv.URL, tok, "assistant", "general")
		if utf8.RuneCountInString(got) >= utf8.RuneCountInString(before) {
			t.Fatalf("the shrinking patch did not land: %d runes", utf8.RuneCountInString(got))
		}
		if utf8.RuneCountInString(got) <= contextDocMaxCharsDefault {
			t.Fatalf("fixture bug: the point is that the RESULT is still over the cap")
		}
	})

	t.Run("write_task_learnings", func(t *testing.T) {
		api := newTasksTestServer(t)
		key := seedManualWithLearnings(t, api, capDoc(t, contextDocMaxCharsDefault+9336))

		want := capDoc(t, contextDocMaxCharsDefault+9000)
		if rec := writeLearnings(t, api, key, map[string]any{"text": want}); rec.Code != http.StatusOK {
			t.Fatalf("over-cap learnings must still be allowed to shrink, got %d: %s",
				rec.Code, rec.Body.String())
		}
		if storedLearnings(t, api, key) != want {
			t.Fatalf("the shrinking write did not land")
		}
	})

	t.Run("patch_task_learnings", func(t *testing.T) {
		api := newTasksTestServer(t)
		before := capDoc(t, contextDocMaxCharsDefault+9336)
		key := seedManualWithLearnings(t, api, before)

		status, data := patchLearnings(t, api, key, map[string]any{
			"edits": []any{edit(capDropAnchor, "")},
		})
		if status != http.StatusOK {
			t.Fatalf("over-cap learnings must still be patchable downward, got %d: %v", status, data)
		}
		got := storedLearnings(t, api, key)
		if utf8.RuneCountInString(got) >= utf8.RuneCountInString(before) {
			t.Fatalf("the shrinking patch did not land")
		}
		if utf8.RuneCountInString(got) <= contextDocMaxCharsDefault {
			t.Fatalf("fixture bug: the RESULT must still be over the cap")
		}
	})

	t.Run("update_task_manual", func(t *testing.T) {
		api := newTasksTestServer(t)
		key := seedManualWithLearnings(t, api, capDoc(t, contextDocMaxCharsDefault+9336))
		setManualSopMD(t, api, key, capDoc(t, contextDocMaxCharsDefault+4691))

		wantSop, wantLearn := capDoc(t, contextDocMaxCharsDefault+4000), capDoc(t, contextDocMaxCharsDefault+9000)
		rec := capUpdateManual(t, api, key, map[string]any{
			"sop_md": wantSop, "learnings": wantLearn,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("over-cap docs must still be allowed to shrink, got %d: %s",
				rec.Code, rec.Body.String())
		}
		if storedSopMD(t, api, key) != wantSop || storedLearnings(t, api, key) != wantLearn {
			t.Fatalf("the shrinking update did not land")
		}
	})
}

// (d) Equal length while over the cap is REFUSED. Standing still is not
// converging, and admitting it would let an over-cap doc be rewritten wholesale
// forever — the cap would apply to nobody who is already past it.
func TestContextDocCap_EqualLengthOverCapIsRefused(t *testing.T) {
	t.Run("replace_lessons", func(t *testing.T) {
		srv, dal, tok := capLessonsServer(t)
		before := capDoc(t, contextDocMaxCharsDefault+2132)
		seedLessonsOverlay(t, dal, "assistant", "general", before)

		// Same rune count, different content — a wholesale rewrite that makes
		// no progress downward.
		same := strings.Repeat("q", utf8.RuneCountInString(before))
		status, data := replaceLessons(t, srv, tok, same, false)
		if status != http.StatusBadRequest {
			t.Fatalf("an equal-length over-cap rewrite must be refused, got %d: %v", status, data)
		}
		if getLessonsText(t, srv.URL, tok, "assistant", "general") != before {
			t.Fatalf("refused write must leave the doc byte-identical")
		}
	})

	t.Run("patch_lessons", func(t *testing.T) {
		srv, dal, tok := capLessonsServer(t)
		before := capDoc(t, contextDocMaxCharsDefault+2132)
		seedLessonsOverlay(t, dal, "assistant", "general", before)

		// The two anchors have the same rune length by construction.
		status, data := patchLessons(t, srv.URL, tok, "assistant", "general",
			`{"edits":[{"old":"`+capDropAnchor+`","new":"`+capKeepAnchor+`"}]}`)
		if status != http.StatusBadRequest {
			t.Fatalf("an equal-length over-cap patch must be refused, got %d: %v", status, data)
		}
		if getLessonsText(t, srv.URL, tok, "assistant", "general") != before {
			t.Fatalf("refused patch must write nothing")
		}
	})

	t.Run("write_task_learnings", func(t *testing.T) {
		api := newTasksTestServer(t)
		before := capDoc(t, contextDocMaxCharsDefault+557)
		key := seedManualWithLearnings(t, api, before)

		same := strings.Repeat("q", utf8.RuneCountInString(before))
		if rec := writeLearnings(t, api, key, map[string]any{"text": same}); rec.Code != http.StatusBadRequest {
			t.Fatalf("an equal-length over-cap rewrite must be refused, got %d: %s",
				rec.Code, rec.Body.String())
		}
		if storedLearnings(t, api, key) != before {
			t.Fatalf("refused write must leave learnings byte-identical")
		}
	})

	t.Run("patch_task_learnings", func(t *testing.T) {
		api := newTasksTestServer(t)
		before := capDoc(t, contextDocMaxCharsDefault+557)
		key := seedManualWithLearnings(t, api, before)

		status, data := patchLearnings(t, api, key, map[string]any{
			"edits": []any{edit(capDropAnchor, capKeepAnchor)},
		})
		if status != http.StatusBadRequest {
			t.Fatalf("an equal-length over-cap patch must be refused, got %d: %v", status, data)
		}
		if storedLearnings(t, api, key) != before {
			t.Fatalf("refused patch must write nothing")
		}
	})

	t.Run("update_task_manual_sop_md", func(t *testing.T) {
		api := newTasksTestServer(t)
		key := seedManualWithLearnings(t, api, "")
		before := capDoc(t, contextDocMaxCharsDefault+1031)
		setManualSopMD(t, api, key, before)

		same := strings.Repeat("q", utf8.RuneCountInString(before))
		rec := capUpdateManual(t, api, key, map[string]any{"sop_md": same})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("an equal-length over-cap sop_md rewrite must be refused, got %d: %s",
				rec.Code, rec.Body.String())
		}
		if storedSopMD(t, api, key) != before {
			t.Fatalf("refused update must leave sop_md byte-identical")
		}
	})
}

// The refusal has to be actionable, because the agent reading it is usually
// mid-handover with one shot at writing its experience down. It must name the
// three numbers and the legal way out — and must NOT advertise a bypass, since
// there is none and teaching one would route agents around a cap the owner set
// deliberately.
func TestContextDocCap_RefusalIsActionableAndAdvertisesNoBypass(t *testing.T) {
	srv, dal, tok := capLessonsServer(t)
	before := capDoc(t, contextDocMaxCharsDefault-10)
	seedLessonsOverlay(t, dal, "assistant", "general", before)

	attempt := capDoc(t, contextDocMaxCharsDefault+250)
	status, data := replaceLessons(t, srv, tok, attempt, false)
	if status != http.StatusBadRequest {
		t.Fatalf("expected a refusal, got %d", status)
	}
	msg := capErrMessage(data)
	for _, want := range []string{
		strconv.Itoa(utf8.RuneCountInString(attempt)), // how long the write is
		strconv.Itoa(contextDocMaxCharsDefault),       // what the cap is
		strconv.Itoa(utf8.RuneCountInString(before)),  // how long the doc is now
		"chars",   // the unit, stated
		"SHORTER", // the legal way out
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("refusal must mention %q; got: %s", want, msg)
		}
	}
	// No bypass may be advertised. allow_shrink governs the OPPOSITE failure
	// (shrinking too far) and does not open this gate.
	for _, forbidden := range []string{"allow_shrink", "force", "override", "bypass"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("refusal must not advertise a way around the cap (%q); got: %s", forbidden, msg)
		}
	}
}

// ...and it is not just the wording: allow_shrink genuinely does not open the
// gate. The two guards face opposite directions and compose — one refuses a
// doc that got too small, the other refuses one that stayed too big.
func TestContextDocCap_AllowShrinkIsNotABypass(t *testing.T) {
	srv, dal, tok := capLessonsServer(t)
	before := capDoc(t, contextDocMaxCharsDefault-10)
	seedLessonsOverlay(t, dal, "assistant", "general", before)

	status, data := replaceLessons(t, srv, tok, capDoc(t, contextDocMaxCharsDefault+1), true)
	if status != http.StatusBadRequest {
		t.Fatalf("allow_shrink must not admit an over-cap write, got %d: %v", status, data)
	}
	if getLessonsText(t, srv.URL, tok, "assistant", "general") != before {
		t.Fatalf("refused write must leave the doc byte-identical")
	}
}

// The two guards can both be satisfied — there is always a legal move out of an
// over-cap doc. Collapsing a 43k doc straight to 4k trips the SHRINK guard (a
// >90% shrink), which allow_shrink is exactly for; the cap does not object
// because the result is under it. Losing this would mean an over-cap doc could
// only ever converge in small steps.
func TestContextDocCap_ShrinkGuardAndCapCompose(t *testing.T) {
	srv, dal, tok := capLessonsServer(t)
	seedLessonsOverlay(t, dal, "assistant", "general", capDoc(t, contextDocMaxCharsDefault+33029))

	small := capDoc(t, 4000) // under the cap, but under a tenth of the doc
	if status, data := replaceLessons(t, srv, tok, small, false); status != http.StatusOK {
		// replace_lessons only guards a full WIPE, so this one is admitted here;
		// the patch seam below is where the shrink guard actually bites.
		t.Fatalf("under-cap replace must land, got %d: %v", status, data)
	}

	// On the patch seam the shrink guard bites, and allow_shrink — not any cap
	// bypass — is the answer. capDoc's head filler is 9/10 of the doc, so
	// deleting all of it leaves under a tenth behind: exactly the >90% shrink
	// LessonsShrinkBlocked exists to catch.
	before := capDoc(t, contextDocMaxCharsDefault+33029)
	seedLessonsOverlay(t, dal, "assistant", "general", before)
	head := strings.Repeat("a", strings.Index(before, capDropAnchor))
	deepShrink := `{"edits":[{"old":"` + capDropAnchor + `","new":""},{"old":"` + head + `","new":""}]}`
	status, _ := patchLessons(t, srv.URL, tok, "assistant", "general", deepShrink)
	if status != http.StatusBadRequest {
		t.Fatalf("a >90%% shrink must still need allow_shrink, got %d", status)
	}
	status, data := patchLessons(t, srv.URL, tok, "assistant", "general",
		`{"allow_shrink":true,`+deepShrink[1:])
	if status != http.StatusOK {
		t.Fatalf("allow_shrink must still admit a deep shrink to under the cap, got %d: %v", status, data)
	}
	if n := utf8.RuneCountInString(getLessonsText(t, srv.URL, tok, "assistant", "general")); n > contextDocMaxCharsDefault {
		t.Fatalf("the deep shrink did not land: %d runes", n)
	}
}

// A role with NO overlay reads the shared seed as its current doc, so the cap
// is judged against what get_lessons serves — the doc the caller is editing.
// The seed is tiny (tens of chars), so in practice a first write is judged on
// the cap alone, which is the intended "no prior doc" semantics. Pinned because
// the seam is easy to get wrong: HandlePatchLessons folds seed ⊕ overlay and
// writes the WHOLE folded result back, so an unbounded first patch on a seed
// role would be the one way to mint an over-cap doc from nothing.
func TestContextDocCap_SeedRoleFirstWriteIsCapped(t *testing.T) {
	srv, _, tok := capLessonsServer(t)

	// No overlay was seeded: this role folds to the shared seed.
	if got := getLessonsText(t, srv.URL, tok, "r-fresh", "general"); utf8.RuneCountInString(got) > contextDocMaxCharsDefault {
		t.Fatalf("fixture assumption broken: the seed itself is over the cap (%d runes)",
			utf8.RuneCountInString(got))
	}
	status, data := replaceLessons2(t, srv, tok, "r-fresh", capDoc(t, contextDocMaxCharsDefault+1))
	if status != http.StatusBadRequest {
		t.Fatalf("a first over-cap write on a seed role must be refused, got %d: %v", status, data)
	}
	status, data = patchLessons(t, srv.URL, tok, "r-fresh", "general",
		`{"edits":[{"old":"","new":"`+strings.Repeat("z", contextDocMaxCharsDefault+1)+`"}]}`)
	if status != http.StatusBadRequest {
		t.Fatalf("a first over-cap PATCH on a seed role must be refused, got %d: %v", status, data)
	}
}

// replaceLessons2 is replaceLessons for an arbitrary role key.
func replaceLessons2(t *testing.T, srv *httptest.Server, tok, roleKey, text string) (int, map[string]any) {
	t.Helper()
	blob, err := json.Marshal(map[string]any{"text": text})
	if err != nil {
		t.Fatal(err)
	}
	return doJSON(t, "POST", srv.URL+"/api/lessons/"+roleKey+"/general", tok, string(blob))
}

// ── task-manual helpers ──────────────────────────────────────────────────────

func capUpdateManual(t *testing.T, api *apiServer, typeKey string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateTaskManualApiTaskManualsTypeKeyPost(rec, taskReq(t, "POST",
		"/api/task-manuals/"+typeKey, body, "m-exec", "agent"), typeKey)
	return rec
}

func setManualSopMD(t *testing.T, api *apiServer, typeKey, sop string) {
	t.Helper()
	m, err := api.dal.GetTaskManual(typeKey)
	if err != nil || m == nil {
		t.Fatalf("read manual %s: %+v %v", typeKey, m, err)
	}
	m.SopMD = sop
	if err := api.dal.PutTaskManual(*m); err != nil {
		t.Fatalf("seed sop_md: %v", err)
	}
}

func storedSopMD(t *testing.T, api *apiServer, typeKey string) string {
	t.Helper()
	m, err := api.dal.GetTaskManual(typeKey)
	if err != nil || m == nil {
		t.Fatalf("read manual %s: %+v %v", typeKey, m, err)
	}
	return m.SopMD
}
