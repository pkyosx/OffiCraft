package main

// T-8327: anchor-addressed lessons patch (MCP patch_lessons) + the boot-context
// lessons-title duplication fix.
//
// Why patch exists: replace_lessons is a WHOLE-DOC write, so its cost grows
// with the doc — a 76k-char lessons doc no longer fits in one model output and
// becomes physically unwritable. patch_lessons makes the write cost ∝ the
// change. These tests drive the REAL wired stack (REST + MCP loopback), the
// same seams an agent uses.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// seedLessonsOverlay writes a known overlay doc directly through the DAL so
// each test starts from a deterministic base (the folded GET then serves it).
func seedLessonsOverlay(t *testing.T, dal *DAL, roleKey, taskType, text string) {
	t.Helper()
	if err := dal.PutLessons(Lessons{
		RoleKey: roleKey, TaskType: taskType, Text: text, Tombstoned: false,
	}); err != nil {
		t.Fatalf("PutLessons: %v", err)
	}
}

// getLessonsText reads the folded doc back over REST (the same view
// get_lessons serves — the base patch applies against).
func getLessonsText(t *testing.T, url, token, roleKey, taskType string) string {
	t.Helper()
	status, data := doJSON(t, "GET", url+"/api/lessons/"+roleKey+"/"+taskType, token, "")
	if status != 200 {
		t.Fatalf("get lessons: status %d", status)
	}
	text, _ := data["text"].(string)
	return text
}

func patchLessons(t *testing.T, url, token, roleKey, taskType, body string) (int, map[string]any) {
	t.Helper()
	return doJSON(t, "POST", url+"/api/lessons/"+roleKey+"/"+taskType+"/patch", token, body)
}

func TestPatchLessonsUniqueAnchorReplaceAndAnchors(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	seedLessonsOverlay(t, dal, "assistant", "general",
		"line one\nline two: keep the old habit\nline three\n")

	status, data := patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[{"old":"line two: keep the old habit","new":"line two: adopt the new habit"}]}`)
	if status != 200 {
		t.Fatalf("unique-anchor patch must land, got %d: %v", status, data)
	}

	// The write landed exactly once, splice-precise.
	text := getLessonsText(t, srv.URL, ownerTok, "assistant", "general")
	want := "line one\nline two: adopt the new habit\nline three\n"
	if text != want {
		t.Fatalf("patched doc mismatch:\n got: %q\nwant: %q", text, want)
	}

	// The receipt's verification anchors describe the RESULTING doc — the
	// caller can confirm the write without re-reading the full text.
	sum := sha256.Sum256([]byte(want))
	if got, _ := data["sha256"].(string); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 anchor mismatch: %v", data["sha256"])
	}
	if got, _ := data["size"].(float64); int(got) != len(want) {
		t.Fatalf("size anchor mismatch: got %v want %d", data["size"], len(want))
	}
	if got, _ := data["applied_edits"].(float64); int(got) != 1 {
		t.Fatalf("applied_edits mismatch: %v", data["applied_edits"])
	}
}

func TestPatchLessonsMultiEditAtomicity(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	base := "alpha\nbeta\ngamma\n"
	seedLessonsOverlay(t, dal, "assistant", "general", base)

	// edits[0] would land; edits[1] misses → the WHOLE batch must 400 with
	// ZERO writes (no partial "ALPHA" splice may survive).
	status, data := patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[{"old":"alpha","new":"ALPHA"},{"old":"never-there","new":"x"}]}`)
	if status != 400 {
		t.Fatalf("batch with a missing anchor must 400, got %d: %v", status, data)
	}
	if msg := errMessage(data); !strings.Contains(msg, "edits[1]") {
		t.Fatalf("error must name the failing edit index, got: %q", msg)
	}
	if text := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); text != base {
		t.Fatalf("partial write leaked — atomicity broken:\n got: %q\nwant: %q", text, base)
	}

	// Sequential semantics: a later edit sees the earlier edit's result.
	status, _ = patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[{"old":"beta","new":"beta prime"},{"old":"beta prime","new":"beta prime indeed"}]}`)
	if status != 200 {
		t.Fatalf("sequential edits must land, got %d", status)
	}
	if text := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); !strings.Contains(text, "beta prime indeed") {
		t.Fatalf("sequential edit result missing: %q", text)
	}
}

func TestPatchLessonsAppendWithEmptyOld(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	seedLessonsOverlay(t, dal, "assistant", "general", "existing lesson") // no trailing \n
	status, _ := patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[{"old":"","new":"appended lesson"}]}`)
	if status != 200 {
		t.Fatalf("append must land, got %d", status)
	}
	text := getLessonsText(t, srv.URL, ownerTok, "assistant", "general")
	if text != "existing lesson\nappended lesson" {
		t.Fatalf("append must join with one newline, got %q", text)
	}
}

func TestPatchLessonsAmbiguousAnchorRejected(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	base := "dup marker\nmiddle\ndup marker\n"
	seedLessonsOverlay(t, dal, "assistant", "general", base)
	status, data := patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[{"old":"dup marker","new":"resolved"}]}`)
	if status != 400 {
		t.Fatalf("ambiguous anchor must 400, got %d: %v", status, data)
	}
	if msg := errMessage(data); !strings.Contains(msg, "2 locations") {
		t.Fatalf("error must report the hit count, got: %q", msg)
	}
	if text := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); text != base {
		t.Fatalf("ambiguous rejection must write nothing, got %q", text)
	}
}

func TestPatchLessonsWipeGuardNeedsExplicitFlag(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	big := strings.Repeat("a hard-won lesson line\n", 30) // ≫ guard threshold
	seedLessonsOverlay(t, dal, "assistant", "general", big)

	// 1. Emptying the doc without the flag is refused, zero writes (r-76).
	wipe := fmt.Sprintf(`{"edits":[{"old":%q,"new":""}]}`, big)
	status, data := patchLessons(t, srv.URL, ownerTok, "assistant", "general", wipe)
	if status != 400 {
		t.Fatalf("wipe without allow_shrink must 400, got %d: %v", status, data)
	}
	if msg := errMessage(data); !strings.Contains(msg, "allow_shrink") {
		t.Fatalf("refusal must teach the flag, got: %q", msg)
	}
	if text := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); text != big {
		t.Fatalf("guarded wipe must write nothing")
	}

	// 2. Near-zero shrink (non-empty result) is guarded too.
	shrink := fmt.Sprintf(`{"edits":[{"old":%q,"new":"tiny"}]}`, big)
	if status, _ := patchLessons(t, srv.URL, ownerTok, "assistant", "general", shrink); status != 400 {
		t.Fatalf("near-zero shrink without allow_shrink must 400, got %d", status)
	}

	// 3. The explicit flag makes the same wipe legal.
	wipeFlagged := fmt.Sprintf(`{"edits":[{"old":%q,"new":""}],"allow_shrink":true}`, big)
	if status, _ := patchLessons(t, srv.URL, ownerTok, "assistant", "general", wipeFlagged); status != 200 {
		t.Fatalf("wipe WITH allow_shrink must land, got %d", status)
	}
	if text := getLessonsText(t, srv.URL, ownerTok, "assistant", "general"); text != "" {
		t.Fatalf("flagged wipe must persist the empty doc, got %q", text)
	}
}

func TestPatchLessonsEmptyEditsRejected(t *testing.T) {
	srv, _, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	if status, _ := patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{"edits":[]}`); status != 422 {
		t.Fatalf("empty edits must 422, got %d", status)
	}
	if status, _ := patchLessons(t, srv.URL, ownerTok, "assistant", "general",
		`{}`); status != 422 {
		t.Fatalf("missing edits must 422, got %d", status)
	}
}

// The MCP face: patch_lessons rides the same identity-default folding as
// get/replace (blank role_key → caller's own role; blank task_type → general),
// so an agent can patch its own doc with a minimal argument set.
func TestPatchLessonsMCPIdentityDefaults(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	now := time.Now().Unix()
	const customRole = "r-25debddcf5dd"
	if err := dal.PutMember(Member{
		ID: "joey", Kind: KindAssistant, RoleKey: customRole,
		DesiredState: DesiredStateOnline,
	}); err != nil {
		t.Fatalf("PutMember: %v", err)
	}
	joeyTok, _ := mintJWT("joey", "agent", 300, secret, now, "")
	seedLessonsOverlay(t, dal, customRole, "general", "own doc base\n")

	if isErr, code, _ := lessonsCall(t, srv.URL, joeyTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"patch_lessons","arguments":{"edits":[{"old":"own doc base","new":"own doc patched"}]}}}`); isErr {
		t.Fatalf("agent patch_lessons with no role/task args must land, got code=%q", code)
	}
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	if text := getLessonsText(t, srv.URL, ownerTok, customRole, "general"); !strings.Contains(text, "own doc patched") {
		t.Fatalf("MCP patch must hit the caller's own (role, general) doc, got %q", text)
	}
}

// errMessage digs the unified error envelope's message.
func errMessage(data map[string]any) string {
	env, _ := data["error"].(map[string]any)
	msg, _ := env["message"].(string)
	return msg
}

// ── boot-context lessons-title duplication (the +38-char drift) ──────────────

// The boot context wraps the authoritative lessons doc in a section title the
// doc itself does not carry. When a generation writes its boot segment back as
// the doc base, the title becomes doc content — and a naive unconditional
// prepend then stacks one more title per generation. The injection must be
// IDEMPOTENT: exactly one title in the assembled context, always.
func TestBootContextLessonsTitleInjectionIsIdempotent(t *testing.T) {
	_, dal, _ := newLessonsTestServer(t)
	api := newAPIServer(dal, NewHub(), []byte(interopSecret), 3600, "../..")
	title := "# Lessons (assistant / general)"

	countTitle := func(doc string) int {
		boot, err := api.buildBootContext("assistant", nil, "general")
		if err != nil || boot == nil {
			t.Fatalf("buildBootContext: %v", err)
		}
		if !strings.Contains(boot.Context, doc) {
			t.Fatalf("assembled context must carry the doc body %q", doc)
		}
		return strings.Count(boot.Context, title)
	}

	// 1. A clean doc gets exactly one injected title.
	seedLessonsOverlay(t, dal, "assistant", "general", "clean lesson body")
	if n := countTitle("clean lesson body"); n != 1 {
		t.Fatalf("clean doc: want exactly 1 title, got %d", n)
	}

	// 2. A doc that ALREADY leads with the title (one write-back generation)
	//    must still assemble with exactly one — never two.
	seedLessonsOverlay(t, dal, "assistant", "general",
		title+"\n\npoisoned-once body")
	if n := countTitle("poisoned-once body"); n != 1 {
		t.Fatalf("once-poisoned doc: want exactly 1 title, got %d", n)
	}

	// 3. Multi-generation accumulation self-heals in the assembled context.
	seedLessonsOverlay(t, dal, "assistant", "general",
		title+"\n\n"+title+"\n\npoisoned-twice body")
	if n := countTitle("poisoned-twice body"); n != 1 {
		t.Fatalf("twice-poisoned doc: want exactly 1 title, got %d", n)
	}

	// 4. A title that is merely the PREFIX of a longer first line is content,
	//    not a duplicate — it must survive (plus the one injected title = 2
	//    occurrences of the prefix).
	seedLessonsOverlay(t, dal, "assistant", "general",
		title+" is what the boot header looks like — do not confuse it")
	boot, err := api.buildBootContext("assistant", nil, "general")
	if err != nil || boot == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	if !strings.Contains(boot.Context, title+" is what the boot header looks like") {
		t.Fatalf("prefix-of-longer-line content must be preserved:\n%s", boot.Context)
	}
	if n := strings.Count(boot.Context, title+"\n\n"); n != 1 {
		t.Fatalf("want exactly 1 bare title line, got %d", n)
	}
}

// Guard against marshal drift: the receipt is the wire shape conformance's
// schema check pins (all eight keys present).
func TestPatchLessonsReceiptWireShape(t *testing.T) {
	raw, err := json.Marshal(lessonsPatchResultDTO{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"role_key", "task_type", "applied_edits", "size", "sha256",
		"owner_id", "schema_version", "is_default",
	} {
		if !strings.Contains(string(raw), `"`+key+`"`) {
			t.Fatalf("receipt missing wire key %q: %s", key, raw)
		}
	}
}
