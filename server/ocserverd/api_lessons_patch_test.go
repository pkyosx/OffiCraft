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
	"unicode/utf8"
)

// seedLessonsOverlay writes a known overlay doc directly through the DAL so
// each test starts from a deterministic base (the folded GET then serves it).
func seedLessonsOverlay(t *testing.T, dal *DAL, roleKey, text string) {
	t.Helper()
	if err := dal.PutLessons(Lessons{
		RoleKey: roleKey, Text: text, Tombstoned: false,
	}); err != nil {
		t.Fatalf("PutLessons: %v", err)
	}
}

// getLessonsText reads the folded doc back over REST (the same view
// get_lessons serves — the base patch applies against).
func getLessonsText(t *testing.T, url, token, roleKey string) string {
	t.Helper()
	status, data := doJSON(t, "GET", url+"/api/lessons/"+roleKey, token, "")
	if status != 200 {
		t.Fatalf("get lessons: status %d", status)
	}
	text, _ := data["text"].(string)
	return text
}

func patchLessons(t *testing.T, url, token, roleKey, body string) (int, map[string]any) {
	t.Helper()
	return doJSON(t, "POST", url+"/api/lessons/"+roleKey+"/patch", token, body)
}

func TestPatchLessonsUniqueAnchorReplaceAndAnchors(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	seedLessonsOverlay(t, dal, "assistant",
		"line one\nline two: keep the old habit\nline three\n")

	status, data := patchLessons(t, srv.URL, ownerTok, "assistant",
		`{"edits":[{"old":"line two: keep the old habit","new":"line two: adopt the new habit"}]}`)
	if status != 200 {
		t.Fatalf("unique-anchor patch must land, got %d: %v", status, data)
	}

	// The write landed exactly once, splice-precise.
	text := getLessonsText(t, srv.URL, ownerTok, "assistant")
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
	if got, _ := data["size_chars"].(float64); int(got) != utf8.RuneCountInString(want) {
		t.Fatalf("size_chars anchor mismatch: got %v want %d", data["size_chars"], utf8.RuneCountInString(want))
	}
	if got, _ := data["applied_edits"].(float64); int(got) != 1 {
		t.Fatalf("applied_edits mismatch: %v", data["applied_edits"])
	}
}

func TestPatchLessonsMultiEditAtomicity(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	base := "alpha\nbeta\ngamma\n"
	seedLessonsOverlay(t, dal, "assistant", base)

	// edits[0] would land; edits[1] misses → the WHOLE batch must 400 with
	// ZERO writes (no partial "ALPHA" splice may survive).
	status, data := patchLessons(t, srv.URL, ownerTok, "assistant",
		`{"edits":[{"old":"alpha","new":"ALPHA"},{"old":"never-there","new":"x"}]}`)
	if status != 400 {
		t.Fatalf("batch with a missing anchor must 400, got %d: %v", status, data)
	}
	if msg := errMessage(data); !strings.Contains(msg, "edits[1]") {
		t.Fatalf("error must name the failing edit index, got: %q", msg)
	}
	if text := getLessonsText(t, srv.URL, ownerTok, "assistant"); text != base {
		t.Fatalf("partial write leaked — atomicity broken:\n got: %q\nwant: %q", text, base)
	}

	// Sequential semantics: a later edit sees the earlier edit's result.
	status, _ = patchLessons(t, srv.URL, ownerTok, "assistant",
		`{"edits":[{"old":"beta","new":"beta prime"},{"old":"beta prime","new":"beta prime indeed"}]}`)
	if status != 200 {
		t.Fatalf("sequential edits must land, got %d", status)
	}
	if text := getLessonsText(t, srv.URL, ownerTok, "assistant"); !strings.Contains(text, "beta prime indeed") {
		t.Fatalf("sequential edit result missing: %q", text)
	}
}

func TestPatchLessonsAppendWithEmptyOld(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	seedLessonsOverlay(t, dal, "assistant", "existing lesson") // no trailing \n
	status, _ := patchLessons(t, srv.URL, ownerTok, "assistant",
		`{"edits":[{"old":"","new":"appended lesson"}]}`)
	if status != 200 {
		t.Fatalf("append must land, got %d", status)
	}
	text := getLessonsText(t, srv.URL, ownerTok, "assistant")
	if text != "existing lesson\nappended lesson" {
		t.Fatalf("append must join with one newline, got %q", text)
	}
}

func TestPatchLessonsAmbiguousAnchorRejected(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	base := "dup marker\nmiddle\ndup marker\n"
	seedLessonsOverlay(t, dal, "assistant", base)
	status, data := patchLessons(t, srv.URL, ownerTok, "assistant",
		`{"edits":[{"old":"dup marker","new":"resolved"}]}`)
	if status != 400 {
		t.Fatalf("ambiguous anchor must 400, got %d: %v", status, data)
	}
	if msg := errMessage(data); !strings.Contains(msg, "2 locations") {
		t.Fatalf("error must report the hit count, got: %q", msg)
	}
	if text := getLessonsText(t, srv.URL, ownerTok, "assistant"); text != base {
		t.Fatalf("ambiguous rejection must write nothing, got %q", text)
	}
}

func TestPatchLessonsWipeGuardNeedsExplicitFlag(t *testing.T) {
	srv, dal, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	big := strings.Repeat("a hard-won lesson line\n", 30) // ≫ guard threshold
	seedLessonsOverlay(t, dal, "assistant", big)

	// 1. Emptying the doc without the flag is refused, zero writes (r-76).
	wipe := fmt.Sprintf(`{"edits":[{"old":%q,"new":""}]}`, big)
	status, data := patchLessons(t, srv.URL, ownerTok, "assistant", wipe)
	if status != 400 {
		t.Fatalf("wipe without allow_shrink must 400, got %d: %v", status, data)
	}
	if msg := errMessage(data); !strings.Contains(msg, "allow_shrink") {
		t.Fatalf("refusal must teach the flag, got: %q", msg)
	}
	if text := getLessonsText(t, srv.URL, ownerTok, "assistant"); text != big {
		t.Fatalf("guarded wipe must write nothing")
	}

	// 2. Near-zero shrink (non-empty result) is guarded too.
	shrink := fmt.Sprintf(`{"edits":[{"old":%q,"new":"tiny"}]}`, big)
	if status, _ := patchLessons(t, srv.URL, ownerTok, "assistant", shrink); status != 400 {
		t.Fatalf("near-zero shrink without allow_shrink must 400, got %d", status)
	}

	// 3. The explicit flag makes the same wipe legal.
	wipeFlagged := fmt.Sprintf(`{"edits":[{"old":%q,"new":""}],"allow_shrink":true}`, big)
	if status, _ := patchLessons(t, srv.URL, ownerTok, "assistant", wipeFlagged); status != 200 {
		t.Fatalf("wipe WITH allow_shrink must land, got %d", status)
	}
	if text := getLessonsText(t, srv.URL, ownerTok, "assistant"); text != "" {
		t.Fatalf("flagged wipe must persist the empty doc, got %q", text)
	}
}

func TestPatchLessonsEmptyEditsRejected(t *testing.T) {
	srv, _, secret := newLessonsTestServer(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")
	if status, _ := patchLessons(t, srv.URL, ownerTok, "assistant",
		`{"edits":[]}`); status != 422 {
		t.Fatalf("empty edits must 422, got %d", status)
	}
	if status, _ := patchLessons(t, srv.URL, ownerTok, "assistant",
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
	seedLessonsOverlay(t, dal, customRole, "own doc base\n")

	if isErr, code, _ := lessonsCall(t, srv.URL, joeyTok,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"patch_lessons","arguments":{"edits":[{"old":"own doc base","new":"own doc patched"}]}}}`); isErr {
		t.Fatalf("agent patch_lessons with no role/task args must land, got code=%q", code)
	}
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, now, "")
	if text := getLessonsText(t, srv.URL, ownerTok, customRole); !strings.Contains(text, "own doc patched") {
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
	api := newAPIServer(dal, NewHub(), singleKeyring([]byte(interopSecret)), 3600, "../..")
	title := "# Lessons (assistant)"

	countTitle := func(doc string) int {
		boot, err := api.buildBootContext("assistant", nil)
		if err != nil || boot == nil {
			t.Fatalf("buildBootContext: %v", err)
		}
		if !strings.Contains(boot.Context, doc) {
			t.Fatalf("assembled context must carry the doc body %q", doc)
		}
		return strings.Count(boot.Context, title)
	}

	// 1. A clean doc gets exactly one injected title.
	seedLessonsOverlay(t, dal, "assistant", "clean lesson body")
	if n := countTitle("clean lesson body"); n != 1 {
		t.Fatalf("clean doc: want exactly 1 title, got %d", n)
	}

	// 2. A doc that ALREADY leads with the title (one write-back generation)
	//    must still assemble with exactly one — never two.
	seedLessonsOverlay(t, dal, "assistant",
		title+"\n\npoisoned-once body")
	if n := countTitle("poisoned-once body"); n != 1 {
		t.Fatalf("once-poisoned doc: want exactly 1 title, got %d", n)
	}

	// 3. Multi-generation accumulation self-heals in the assembled context.
	seedLessonsOverlay(t, dal, "assistant",
		title+"\n\n"+title+"\n\npoisoned-twice body")
	if n := countTitle("poisoned-twice body"); n != 1 {
		t.Fatalf("twice-poisoned doc: want exactly 1 title, got %d", n)
	}

	// 4. A title that is merely the PREFIX of a longer first line is content,
	//    not a duplicate — it must survive (plus the one injected title = 2
	//    occurrences of the prefix).
	seedLessonsOverlay(t, dal, "assistant",
		title+" is what the boot header looks like — do not confuse it")
	boot, err := api.buildBootContext("assistant", nil)
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

// TestBootContextStripsThePreT2LessonsTitle is the OTHER half of the strip, and
// the one a naive T-2 edit would have dropped.
//
// The title carried the lessons bucket until T-2: "# Lessons (assistant /
// general)". A document poisoned BEFORE that change carries the old wording, so
// a strip that only knew the NEW title would leave it wedged at the top of the
// document permanently — the self-heal could never reach it again, and the
// assembled context would show one stale title plus one fresh one, i.e. exactly
// the drift the idempotency above exists to prevent, reintroduced by the
// rename. This is the named assertion a mutant that removes the legacy form
// from the strip has to turn red.
func TestBootContextStripsThePreT2LessonsTitle(t *testing.T) {
	_, dal, _ := newLessonsTestServer(t)
	api := newAPIServer(dal, NewHub(), singleKeyring([]byte(interopSecret)), 3600, "../..")
	const title = "# Lessons (assistant)"
	const legacy = "# Lessons (assistant / general)"

	seedLessonsOverlay(t, dal, "assistant", legacy+"\n\npre-T-2 poisoned body")
	boot, err := api.buildBootContext("assistant", nil)
	if err != nil || boot == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	if !strings.Contains(boot.Context, "pre-T-2 poisoned body") {
		t.Fatalf("the document body was lost:\n%s", boot.Context)
	}
	if strings.Contains(boot.Context, legacy) {
		t.Fatalf("the PRE-T-2 lessons title survived into the assembled context. A document "+
			"poisoned before the axis was dropped can never self-heal if the strip only "+
			"knows the current wording — the stale header stays at the top of the doc "+
			"forever, beside the freshly injected one:\n%s", boot.Context)
	}
	if n := strings.Count(boot.Context, title+"\n\n"); n != 1 {
		t.Fatalf("want exactly 1 injected title line, got %d:\n%s", n, boot.Context)
	}

	// Both generations at once — the legacy title and the new one stacked — must
	// also collapse to one.
	seedLessonsOverlay(t, dal, "assistant", legacy+"\n\n"+title+"\n\nboth-generations body")
	boot, err = api.buildBootContext("assistant", nil)
	if err != nil || boot == nil {
		t.Fatalf("buildBootContext: %v", err)
	}
	if !strings.Contains(boot.Context, "both-generations body") {
		t.Fatalf("the document body was lost:\n%s", boot.Context)
	}
	if strings.Contains(boot.Context, legacy) {
		t.Fatalf("a mixed-generation document kept its legacy title:\n%s", boot.Context)
	}
	if n := strings.Count(boot.Context, title+"\n\n"); n != 1 {
		t.Fatalf("mixed generations: want exactly 1 title line, got %d:\n%s", n, boot.Context)
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
		"role_key", "applied_edits", "size_chars", "cap_chars",
		"sha256", "owner_id", "schema_version", "is_default",
	} {
		if !strings.Contains(string(raw), `"`+key+`"`) {
			t.Fatalf("receipt missing wire key %q: %s", key, raw)
		}
	}
	// T-2: the classification axis is gone from the receipt too. Echoing a
	// task_type back would tell the caller its bucket name was accepted, which
	// is precisely the false confirmation this change removes.
	if strings.Contains(string(raw), `"task_type"`) {
		t.Fatalf("the receipt still echoes task_type: %s", raw)
	}
	// The old unit-less name is GONE, not merely joined by a new one (T-3aeb:
	// the owner ruled a size field must carry its unit in its name). Without
	// this, keeping `size` alongside `size_chars` would pass the loop above.
	if strings.Contains(string(raw), `"size"`) {
		t.Fatalf("the unit-less `size` key must be gone, not kept alongside size_chars: %s", raw)
	}
}
