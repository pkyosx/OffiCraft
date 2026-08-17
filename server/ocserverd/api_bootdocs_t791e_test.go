package main

// api_bootdocs_t791e_test.go — the seven statements T-791e is accountable for,
// each able to go red on its own.
//
// EVERYTHING RUNS OVER THE WIRED STACK (newWiredTestServer: real DB, real auth
// gate, real route table). That is not decoration: the write floor lives in the
// ROUTE TABLE, so a test that calls a handler function directly stays green with
// the floor set to anything at all — a trap this repo has documented and fallen
// into before (see the T-1b88 note in server/CLAUDE.md).
//
// The seed comparisons read the shipped bytes through the SAME embed the server
// serves (assetRoot.readSeedFile), never a copy of the text pasted into a
// fixture: a pasted copy asserts that two literals in this file are equal.
//
// MUTANT RECORD (each run after `go clean -testcache`, restored from a
// scratchpad backup — never `git checkout --`) is in the task report.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// ── fixture ──────────────────────────────────────────────────────────────────

type bootDocFixture struct {
	url    string
	secret []byte
	owner  string // owner scope, sub "owner" — NOT a member of anything
	admin  string // agent scope, seeded mira (role_key assistant) ⇒ admin_agent
	plain  string // agent scope, unknown sub ⇒ principalAgent
	root   assetRoot
}

func newBootDocFixture(t *testing.T) bootDocFixture {
	t.Helper()
	srv, secret, _ := newWiredTestServer(t)
	now := time.Now().Unix()
	mint := func(sub, scope string) string {
		tok, err := mintJWT(sub, scope, 300, secret, now, "")
		if err != nil {
			t.Fatalf("mint %s/%s: %v", sub, scope, err)
		}
		return tok
	}
	f := bootDocFixture{url: srv.URL, secret: secret,
		owner: mint("owner", "owner"), admin: mint("mira", "agent"), plain: mint("kyle-t791e", "agent"),
		root: assetRoot("../..")}
	// Premise control: the three tokens really do classify differently. Without
	// it a fixture regression (mira losing her role_key) would make the refusal
	// assertions agree with the permitted ones by accident.
	if got := classifyMember(&Member{ID: "mira", RoleKey: adminRoleKey}); got != principalAdminAgent {
		t.Fatalf("fixture premise: seeded mira must classify as %q, got %q", principalAdminAgent, got)
	}
	if got := classifyMember(nil); got != principalAgent {
		t.Fatalf("fixture premise: an unknown sub must classify as %q, got %q", principalAgent, got)
	}
	return f
}

func (f bootDocFixture) do(t *testing.T, method, path, token string, body any) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, f.url+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func (f bootDocFixture) read(t *testing.T, path string) bootDocDTO {
	t.Helper()
	status, body := f.do(t, http.MethodGet, path, f.owner, nil)
	if status != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, status, body)
	}
	var dto bootDocDTO
	if err := json.Unmarshal([]byte(body), &dto); err != nil {
		t.Fatalf("decode %s: %v (%s)", path, err, body)
	}
	return dto
}

func (f bootDocFixture) replace(t *testing.T, path, token, text string) (int, string) {
	t.Helper()
	return f.do(t, http.MethodPost, path, token, map[string]any{"text": text})
}

// seed reads a shipped seed through the very embed the server folds against.
func (f bootDocFixture) seed(t *testing.T, filename string) string {
	t.Helper()
	text, err := f.root.readSeedFile(filename)
	if err != nil {
		t.Fatalf("read seed %s: %v (run bin/build-seedsdist)", filename, err)
	}
	if strings.TrimSpace(text) == "" {
		t.Fatalf("seed %s is empty — every comparison below would be vacuous", filename)
	}
	return text
}

// the owner-editable documents, addressed the way a client addresses them.
type bootDocCase struct {
	name string
	path string // the read/replace path; reset is this + "/reset"
	seed string // the shipped seed filename
	kind string // the document-history kind
	key  string // the document-history key
}

func bootDocCases() []bootDocCase {
	return []bootDocCase{
		{"system_interaction", "/api/system-interaction", systemInteractionSeedMD, docKindSystemInteraction, systemInteractionDocKey},
		{"boot_sequence_claude", "/api/boot-sequence/claude", bootSequenceSeedClaude, docKindBootSequence, bootSequenceKeyClaude},
		{"boot_sequence_codex", "/api/boot-sequence/codex", bootSequenceSeedCodex, docKindBootSequence, bootSequenceKeyCodex},
		// 下線程序 (T-c9c0) joined the family later; it is listed LAST so the
		// index-addressed cases above keep pointing at the documents they name.
		{"offboard", "/api/offboard", offboardSeedMD, docKindOffboard, offboardDocKey},
	}
}

// ── #1 — replace then read back, byte for byte ───────────────────────────────

func TestBootDoc_ReplacedTextReadsBackByteIdentical(t *testing.T) {
	f := newBootDocFixture(t)
	for _, c := range bootDocCases() {
		t.Run(c.name, func(t *testing.T) {
			seed := f.seed(t, c.seed)
			// Precondition: it really is the factory text right now, so what
			// follows measures a MOVE rather than a document that was already
			// what we are about to write.
			before := f.read(t, c.path)
			if before.Text != seed || !before.IsDefault {
				t.Fatalf("precondition: %s should read the seed with is_default=true; got is_default=%v, %d chars",
					c.name, before.IsDefault, utf8.RuneCountInString(before.Text))
			}
			// Multi-byte + trailing whitespace + an empty line: a comparison
			// that only survives ASCII would prove much less.
			want := "# " + c.name + " 被改過了\n\n這一行有中文與 emoji 🚀，還有尾端空白  \n\n\t制表符\n"
			status, body := f.replace(t, c.path, f.owner, want)
			if status != http.StatusOK {
				t.Fatalf("replace %s: %d %s", c.path, status, body)
			}
			var wrote bootDocDTO
			if err := json.Unmarshal([]byte(body), &wrote); err != nil {
				t.Fatalf("decode replace response: %v", err)
			}
			if wrote.Text != want {
				t.Fatalf("the replace RESPONSE is not what was written:\n got %q\nwant %q", wrote.Text, want)
			}
			if wrote.IsDefault {
				t.Fatal("is_default must be false after an edit — true says the cockpit is showing factory wording")
			}
			if wrote.SizeChars != utf8.RuneCountInString(want) {
				t.Fatalf("size_chars %d disagrees with the text it just accepted (%d runes)",
					wrote.SizeChars, utf8.RuneCountInString(want))
			}
			// And the READ face agrees: the response was not a projection that
			// nothing durable backs.
			if after := f.read(t, c.path); after.Text != want || after.IsDefault {
				t.Fatalf("GET after replace: is_default=%v, text %q; want the edit verbatim", after.IsDefault, after.Text)
			}
		})
	}
}

// ── #2 — reset lands on the embedded seed, byte for byte ─────────────────────

func TestBootDoc_ResetRestoresTheEmbeddedSeedByteIdentical(t *testing.T) {
	f := newBootDocFixture(t)
	for _, c := range bootDocCases() {
		t.Run(c.name, func(t *testing.T) {
			seed := f.seed(t, c.seed)
			const edit = "# 被改壞的版本\n\nthis is not the factory text\n"
			if edit == seed {
				t.Fatal("fixture bug: the edit must DIFFER from the seed or the assertion below is vacuous")
			}
			if status, body := f.replace(t, c.path, f.owner, edit); status != http.StatusOK {
				t.Fatalf("replace: %d %s", status, body)
			}
			if mid := f.read(t, c.path); mid.Text != edit || mid.IsDefault {
				t.Fatalf("precondition: the document should be the edit; got is_default=%v", mid.IsDefault)
			}

			status, body := f.do(t, http.MethodPost, c.path+"/reset", f.owner, nil)
			if status != http.StatusOK {
				t.Fatalf("reset: %d %s", status, body)
			}
			var dto bootDocDTO
			if err := json.Unmarshal([]byte(body), &dto); err != nil {
				t.Fatalf("decode reset response: %v", err)
			}
			if dto.Text != seed {
				t.Fatalf("reset answered %d chars, want the %d-char shipped seed VERBATIM",
					utf8.RuneCountInString(dto.Text), utf8.RuneCountInString(seed))
			}
			if !dto.IsDefault || !dto.HasSeed {
				t.Fatalf("after a reset: is_default=%v has_seed=%v, want both true", dto.IsDefault, dto.HasSeed)
			}
			if after := f.read(t, c.path); after.Text != seed {
				t.Fatalf("GET after reset is not the seed (%d chars vs %d)",
					utf8.RuneCountInString(after.Text), utf8.RuneCountInString(seed))
			}
		})
	}
}

// 🔴 The reset must not depend on ANY member existing, because the state it
// exists to recover from is "a bad edit stopped every agent from booting". The
// token here is owner scope with a sub that is on nobody's roster, and the path
// is plain REST — no MCP loopback, no agent, no member row.
func TestBootDoc_ResetNeedsNoMemberIdentityAtAll(t *testing.T) {
	f := newBootDocFixture(t)
	nobody, err := mintJWT("nobody-is-this-member", "owner", 300, f.secret, time.Now().Unix(), "")
	if err != nil {
		t.Fatal(err)
	}
	// Premise: that sub really is not a member — otherwise this asserts nothing
	// beyond the ordinary owner path.
	if status, body := f.do(t, http.MethodGet, "/api/members/nobody-is-this-member", f.owner, nil); status != http.StatusNotFound {
		t.Fatalf("fixture premise: the sub must NOT be on the roster; GET member gave %d %s", status, body)
	}
	for _, c := range bootDocCases() {
		if status, body := f.replace(t, c.path, f.owner, "broken by an edit\n"); status != http.StatusOK {
			t.Fatalf("seed the broken state: %d %s", status, body)
		}
		if status, body := f.do(t, http.MethodPost, c.path+"/reset", nobody, nil); status != http.StatusOK {
			t.Fatalf("%s: a rosterless owner token must be able to restore the factory text; got %d %s",
				c.name, status, body)
		}
		if got, want := f.read(t, c.path).Text, f.seed(t, c.seed); got != want {
			t.Fatalf("%s: after the rosterless reset the document is not the seed", c.name)
		}
	}
}

// ── #3 — over the cap is refused, and the refusal counts out loud ────────────

func TestBootDoc_OverCapIsRefusedWithAllThreeNumbers(t *testing.T) {
	f := newBootDocFixture(t)
	for _, c := range bootDocCases() {
		t.Run(c.name, func(t *testing.T) {
			stored := f.read(t, c.path)
			cap := stored.CapChars
			if cap <= 0 {
				t.Fatalf("the read face reports cap_chars=%d — the assertion below could not fail", cap)
			}
			// One rune over the cap, and LONGER than what is stored: the rule is
			// "over the cap AND not shorter than what is there", so an over-cap
			// write that is shorter is legal and would prove nothing here.
			over := strings.Repeat("字", cap+1)
			if utf8.RuneCountInString(over) < utf8.RuneCountInString(stored.Text) {
				t.Fatalf("fixture bug: the probe (%d runes) must not be shorter than what is stored (%d)",
					utf8.RuneCountInString(over), utf8.RuneCountInString(stored.Text))
			}
			status, body := f.replace(t, c.path, f.owner, over)
			if status != http.StatusBadRequest {
				t.Fatalf("an over-cap write must be refused; got %d %s", status, body)
			}
			// The message has to carry all THREE numbers: what you wrote, the
			// cap, and what is already stored. Being refused is otherwise the
			// only way to learn any of them, and a refusal that names none is a
			// dead end.
			for _, want := range []string{
				strconv.Itoa(utf8.RuneCountInString(over)),
				strconv.Itoa(cap),
				strconv.Itoa(utf8.RuneCountInString(stored.Text)),
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("refusal must name %s (wrote / cap / stored); got %s", want, body)
				}
			}
			// Nothing was written — a half-applied refusal is worse than none.
			if after := f.read(t, c.path); after.Text != stored.Text {
				t.Fatal("the refused write changed the document")
			}
			// Positive control: exactly AT the cap is accepted, so the refusal
			// above is the ceiling and not a route that refuses everything.
			atCap := strings.Repeat("字", cap)
			if status, body := f.replace(t, c.path, f.owner, atCap); status != http.StatusOK {
				t.Fatalf("a write exactly AT the cap must be accepted; got %d %s", status, body)
			}
		})
	}
}

// ── #4 — a plain member may read but not write ───────────────────────────────

func TestBootDoc_PlainAgentIsRefusedEveryWriteButStillReads(t *testing.T) {
	f := newBootDocFixture(t)
	for _, c := range bootDocCases() {
		t.Run(c.name, func(t *testing.T) {
			// Reading is at the machine floor on purpose: this text is already
			// in that agent's own boot context. If reading broke, the refusals
			// below would be indistinguishable from "the route is walled off".
			if status, body := f.do(t, http.MethodGet, c.path, f.plain, nil); status != http.StatusOK {
				t.Fatalf("a plain agent must still READ this block; got %d %s", status, body)
			}
			for _, target := range []string{c.path, c.path + "/reset"} {
				status, body := f.do(t, http.MethodPost, target, f.plain, map[string]any{"text": "sneaky"})
				if status != http.StatusForbidden {
					t.Fatalf("POST %s as a plain agent: want 403, got %d %s", target, status, body)
				}
				if !strings.Contains(body, `"code":"forbidden"`) {
					t.Fatalf("POST %s: want the forbidden envelope, got %s", target, body)
				}
			}
			// The admin 助理 (agent scope, role assistant) DOES get through —
			// otherwise the 403s above could just mean the row is owner-only.
			if status, body := f.replace(t, c.path, f.admin, "written by the admin assistant\n"); status != http.StatusOK {
				t.Fatalf("the admin assistant must be able to write; got %d %s", status, body)
			}
			if status, body := f.do(t, http.MethodPost, c.path+"/reset", f.admin, nil); status != http.StatusOK {
				t.Fatalf("the admin assistant must be able to reset; got %d %s", status, body)
			}
		})
	}
}

// ── #5 — the two boot sequences are separate documents ───────────────────────
//
// 🔴 THE ONE THAT COSTS AN AGENT ITS BOOT. Step 3 of the two sequences says
// opposite things, so a shared row (or a key derived from the wrong runtime)
// hands one runtime the other's instructions and that agent never comes online.
func TestBootDoc_EditingClaudeLeavesCodexAlone(t *testing.T) {
	f := newBootDocFixture(t)
	claudeSeed := f.seed(t, bootSequenceSeedClaude)
	codexSeed := f.seed(t, bootSequenceSeedCodex)
	if claudeSeed == codexSeed {
		t.Fatal("the two shipped boot sequences are byte-identical — this test cannot discriminate; that would itself be the bug")
	}

	const edit = "# 只改 claude 這一份\n"
	if status, body := f.replace(t, "/api/boot-sequence/claude", f.owner, edit); status != http.StatusOK {
		t.Fatalf("replace claude: %d %s", status, body)
	}
	if got := f.read(t, "/api/boot-sequence/claude").Text; got != edit {
		t.Fatalf("claude did not take the edit: %q", got)
	}
	codex := f.read(t, "/api/boot-sequence/codex")
	if codex.Text != codexSeed || !codex.IsDefault {
		t.Fatalf("editing claude moved codex: is_default=%v, %d chars (want the untouched %d-char codex seed)",
			codex.IsDefault, utf8.RuneCountInString(codex.Text), utf8.RuneCountInString(codexSeed))
	}
	// And the other direction, so a swapped mapping cannot pass by symmetry.
	const codexEdit = "# 只改 codex 這一份\n"
	if status, body := f.replace(t, "/api/boot-sequence/codex", f.owner, codexEdit); status != http.StatusOK {
		t.Fatalf("replace codex: %d %s", status, body)
	}
	if got := f.read(t, "/api/boot-sequence/claude").Text; got != edit {
		t.Fatalf("editing codex moved claude: %q", got)
	}
	if got := f.read(t, "/api/boot-sequence/codex").Text; got != codexEdit {
		t.Fatalf("codex did not take its own edit: %q", got)
	}
	// A runtime with no boot sequence is a 404 that NAMES the two that exist —
	// never a silent fallback to claude, which is how the wrong sequence gets
	// served in the first place.
	status, body := f.do(t, http.MethodGet, "/api/boot-sequence/opus", f.owner, nil)
	if status != http.StatusNotFound {
		t.Fatalf("an unknown runtime must 404, got %d %s", status, body)
	}
	if !strings.Contains(body, bootSequenceKeyClaude) || !strings.Contains(body, bootSequenceKeyCodex) {
		t.Fatalf("the 404 must name the runtimes that DO exist; got %s", body)
	}
}

// ── #6 — the edit really reaches the assembled boot context ──────────────────
//
// The point of the whole ticket. Compared by taking ONE string from the edit
// face and looking for it in what buildBootContext / the worker fold assemble —
// never by eye.
func TestBootDoc_EditedBlocksReachTheAssembledBootContext(t *testing.T) {
	api := newTasksTestServer(t)

	const sysMark = "«SYS-MARK-T791e»"
	const claudeMark = "«CLAUDE-BOOT-MARK-T791e»"
	const codexMark = "«CODEX-BOOT-MARK-T791e»"

	// Baseline: the marks are NOT in the shipped seeds, so finding one later
	// can only mean the edit was folded in.
	base, err := api.buildBootContext("", &Member{ID: "m-x", Name: "X", RoleKey: seedRoleAssistant}, "")
	if err != nil || base == nil {
		t.Fatalf("baseline boot context: %v", err)
	}
	for _, mark := range []string{sysMark, claudeMark, codexMark} {
		if strings.Contains(base.Context, mark) {
			t.Fatalf("baseline already contains %s — the assertions below would be vacuous", mark)
		}
	}

	write := func(kind, key, text string) {
		t.Helper()
		if err := api.dal.PutBootDocument(BootDocument{Kind: kind, Key: key, Text: text}); err != nil {
			t.Fatalf("write %s/%s: %v", kind, key, err)
		}
	}
	write(docKindSystemInteraction, systemInteractionDocKey, "# 系統互動\n\n"+sysMark+"\n")
	write(docKindBootSequence, bootSequenceKeyClaude, "# 啟動程序\n\n"+claudeMark+"\n")
	write(docKindBootSequence, bootSequenceKeyCodex, "# 啟動程序\n\n"+codexMark+"\n")

	// (a) staff, claude runtime.
	claudeBoot, err := api.buildBootContext("", &Member{ID: "m-c", Name: "C", RoleKey: seedRoleAssistant, Runtime: RuntimeClaude}, "")
	if err != nil || claudeBoot == nil {
		t.Fatalf("claude boot context: %v", err)
	}
	if !strings.Contains(claudeBoot.Context, sysMark) {
		t.Fatal("the edited 系統互動 block never reached the assembled boot context")
	}
	if !strings.Contains(claudeBoot.Context, claudeMark) {
		t.Fatal("the edited claude 啟動程序 never reached the assembled boot context")
	}
	if strings.Contains(claudeBoot.Context, codexMark) {
		t.Fatal("a claude member was handed the CODEX boot sequence — that is the failure this ticket must not introduce")
	}

	// (b) staff, codex runtime — the opposite pairing, so a mapping that always
	// answers "claude" cannot pass.
	codexBoot, err := api.buildBootContext("", &Member{ID: "m-x2", Name: "X2", RoleKey: seedRoleAssistant, Runtime: RuntimeCodex}, "")
	if err != nil || codexBoot == nil {
		t.Fatalf("codex boot context: %v", err)
	}
	if !strings.Contains(codexBoot.Context, codexMark) || strings.Contains(codexBoot.Context, claudeMark) {
		t.Fatal("a codex member did not get the codex 啟動程序 (or got the claude one)")
	}

	// (c) the OUTSOURCE fold reads the same two documents — staff and outsource
	// must not be able to disagree about what the shared blocks say.
	head, err := api.workerSharedHead()
	if err != nil {
		t.Fatalf("workerSharedHead: %v", err)
	}
	if !strings.Contains(head, sysMark) {
		t.Fatal("the outsource fold still reads the shipped 系統互動 seed, not the edit")
	}
	tail, err := api.workerBootSequence(RuntimeCodex)
	if err != nil {
		t.Fatalf("workerBootSequence: %v", err)
	}
	if !strings.Contains(tail, codexMark) || strings.Contains(tail, claudeMark) {
		t.Fatal("a codex worker did not get the edited codex 啟動程序")
	}
}

// ── #7 — a save that changes nothing keeps no version ────────────────────────

func TestBootDoc_UnchangedSaveRetainsNoVersion(t *testing.T) {
	f := newBootDocFixture(t)
	for _, c := range bootDocCases() {
		t.Run(c.name, func(t *testing.T) {
			const first = "# 第一版\n\nsome real content\n"
			if status, body := f.replace(t, c.path, f.owner, first); status != http.StatusOK {
				t.Fatalf("first write: %d %s", status, body)
			}
			afterFirst := f.history(t, c.kind, c.key)

			// The same bytes again: nothing changed, so nothing may be retained.
			if status, body := f.replace(t, c.path, f.owner, first); status != http.StatusOK {
				t.Fatalf("repeat write: %d %s", status, body)
			}
			if got := f.history(t, c.kind, c.key); len(got) != len(afterFirst) {
				t.Fatalf("an unchanged save retained a version: %d → %d", len(afterFirst), len(got))
			}

			// POSITIVE CONTROL: a write that really changes the text DOES
			// retain one. Without it, "no new version" would also be satisfied
			// by a history that never records anything at all.
			if status, body := f.replace(t, c.path, f.owner, first+"one more line\n"); status != http.StatusOK {
				t.Fatalf("changed write: %d %s", status, body)
			}
			after := f.history(t, c.kind, c.key)
			if len(after) != len(afterFirst)+1 {
				t.Fatalf("a real edit must retain exactly one version: %d → %d", len(afterFirst), len(after))
			}
			if after[0].Content["text"] != first {
				t.Fatalf("the retained version is not the text this write replaced: %q", after[0].Content["text"])
			}
		})
	}
}

// These three documents keep TEN versions, not the default three — the depth is
// a ruling, so it gets an assertion rather than a comment.
func TestBootDoc_KeepsTenVersionsWhileEveryOtherKindKeepsThree(t *testing.T) {
	f := newBootDocFixture(t)
	c := bootDocCases()[1] // one document is enough; the depth is per KIND
	for i := 0; i < 14; i++ {
		if status, body := f.replace(t, c.path, f.owner, "# version "+strconv.Itoa(i)+"\n\ncontent\n"); status != http.StatusOK {
			t.Fatalf("write %d: %d %s", i, status, body)
		}
	}
	got := f.history(t, c.kind, c.key)
	if len(got) != 10 {
		t.Fatalf("boot_sequence kept %d versions, want 10 (the T-791e ruling)", len(got))
	}
	if want := documentHistoryKeepFor(c.kind); want != 10 {
		t.Fatalf("documentHistoryKeepFor(%q) = %d, want 10", c.kind, want)
	}
	// The raise is deliberately NOT global: every other kind stays at three.
	if want := documentHistoryKeepFor("global_context"); want != documentHistoryKeepDefault {
		t.Fatalf("global_context depth = %d, want the unchanged default %d", want, documentHistoryKeepDefault)
	}
}

// ── document-history faces: seed comparison, restore, unknown keys ───────────

func TestBootDoc_HistorySeedFaceServesTheShippedText(t *testing.T) {
	f := newBootDocFixture(t)
	for _, c := range bootDocCases() {
		t.Run(c.name, func(t *testing.T) {
			status, body := f.do(t, http.MethodGet, "/api/document-history/"+c.kind+"/"+c.key+"/seed", f.owner, nil)
			if status != http.StatusOK {
				t.Fatalf("seed face: %d %s", status, body)
			}
			var dto struct {
				Kind    string            `json:"kind"`
				Key     string            `json:"key"`
				Content map[string]string `json:"content"`
			}
			if err := json.Unmarshal([]byte(body), &dto); err != nil {
				t.Fatal(err)
			}
			if dto.Content["text"] != f.seed(t, c.seed) {
				t.Fatalf("the compare view does not show the shipped seed (%d chars vs %d)",
					utf8.RuneCountInString(dto.Content["text"]), utf8.RuneCountInString(f.seed(t, c.seed)))
			}
			// 🔴 The FIELD NAME matters: the cockpit diffs this map against a
			// retained revision key by key, so a mismatched name renders "no
			// difference" against every version instead of failing loudly.
			if _, ok := dto.Content["text"]; !ok {
				t.Fatal("the seed content must be keyed `text`, the same key a retained revision carries")
			}
		})
	}
}

func TestBootDoc_RestoreIsAdminGatedAndPutsTheOldTextBack(t *testing.T) {
	f := newBootDocFixture(t)
	c := bootDocCases()[0]
	const v1 = "# 版本一\n\nthe wording I want back\n"
	if status, body := f.replace(t, c.path, f.owner, v1); status != http.StatusOK {
		t.Fatalf("v1: %d %s", status, body)
	}
	if status, body := f.replace(t, c.path, f.owner, "# 版本二\n\nbroken\n"); status != http.StatusOK {
		t.Fatalf("v2: %d %s", status, body)
	}
	versions := f.history(t, c.kind, c.key)
	if len(versions) == 0 {
		t.Fatal("no retained versions — the restore below would assert nothing")
	}
	target := versions[0]
	if target.Content["text"] != v1 {
		t.Fatalf("the newest retained version should be v1, got %q", target.Content["text"])
	}
	path := "/api/document-history/" + c.kind + "/" + c.key + "/" + strconv.Itoa(int(target.Id)) + "/restore"
	// A plain agent may not walk the document back.
	if status, body := f.do(t, http.MethodPost, path, f.plain, nil); status != http.StatusForbidden {
		t.Fatalf("restore as a plain agent: want 403, got %d %s", status, body)
	}
	if status, body := f.do(t, http.MethodPost, path, f.owner, nil); status != http.StatusOK {
		t.Fatalf("restore as owner: %d %s", status, body)
	}
	if got := f.read(t, c.path).Text; got != v1 {
		t.Fatalf("after the restore the document is %q, want v1 back", got)
	}
}

// A key this server does not serve must be refused, not answered with an empty
// version list: "you used the wrong key" and "this document has no versions
// yet" would otherwise look the same.
func TestBootDoc_HistoryRefusesAKeyThisServerDoesNotServe(t *testing.T) {
	f := newBootDocFixture(t)
	for _, bad := range []string{
		"/api/document-history/boot_sequence/opus",
		"/api/document-history/system_interaction/not-global",
	} {
		status, body := f.do(t, http.MethodGet, bad, f.owner, nil)
		if status != http.StatusBadRequest {
			t.Fatalf("GET %s: want 400, got %d %s", bad, status, body)
		}
	}
	// Positive control: the real keys are served.
	for _, good := range []string{
		"/api/document-history/boot_sequence/claude",
		"/api/document-history/system_interaction/global",
	} {
		if status, body := f.do(t, http.MethodGet, good, f.owner, nil); status != http.StatusOK {
			t.Fatalf("GET %s: want 200, got %d %s", good, status, body)
		}
	}
}

// history lists the retained versions and then fetches each body through the
// real mux — which is also the only place this ticket's new route is exercised
// as a ROUTE (pattern precedence: “/{id}“ must not swallow the “/seed“
// sibling, and an in-process handler call would never notice if it did).
func (f bootDocFixture) history(t *testing.T, kind, key string) []historyRow {
	t.Helper()
	status, body := f.do(t, http.MethodGet, "/api/document-history/"+kind+"/"+key, f.owner, nil)
	if status != http.StatusOK {
		t.Fatalf("history %s/%s: %d %s", kind, key, status, body)
	}
	var rows []DocumentHistoryDTO
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("decode history: %v (%s)", err, body)
	}
	out := make([]historyRow, 0, len(rows))
	for _, row := range rows {
		path := fmt.Sprintf("/api/document-history/%s/%s/%d", kind, key, row.Id)
		vstatus, vbody := f.do(t, http.MethodGet, path, f.owner, nil)
		if vstatus != http.StatusOK {
			t.Fatalf("version %s: %d %s", path, vstatus, vbody)
		}
		var version DocumentHistoryVersionDTO
		if err := json.Unmarshal([]byte(vbody), &version); err != nil {
			t.Fatalf("decode version: %v (%s)", err, vbody)
		}
		out = append(out, historyRow{DocumentHistoryDTO: row, Content: version.Content})
	}
	return out
}
