package main

// upgrade_instructions_t79_test.go — the guard rails for 換版交代單 (T-79).
//
// WHY THESE PARTICULAR SEVEN. Every one of them covers a failure that produces
// NO error, NO panic and NO red anywhere else: the message silently loses a
// section, an instruction silently stops being handed over, a read failure
// silently reads like "he asked for nothing", a second tick silently rewrites
// who did the work, or a verb silently admits somebody it should not. A feature
// whose whole job is to carry an instruction to a reader fails by going quiet,
// and quiet is exactly what an ordinary test suite calls passing.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ─── helpers ───────────────────────────────────────────────────────────────

// t79Instruction is one open instruction at a fixed ts, so the rendered line is
// deterministic.
func t79Instruction(id, body string, ts float64) UpgradeInstruction {
	return UpgradeInstruction{ID: id, Body: body, CreatedTS: ts, CreatedBy: wireOwnerID}
}

// t79ReqAs builds a request already carrying a VERIFIED identity, the way the
// auth gate hands one to a handler. sub "owner" gets owner scope; anybody else
// gets agent scope and is resolved through the roster, which is the same path
// production takes.
func t79ReqAs(method, target, sub, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	claims := map[string]any{"sub": sub, "scope": scopeFor(sub)}
	return req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
}

// t79SeedInstruction writes one instruction straight through the DAL, so an
// authz test never depends on the write verb it is not testing.
func t79SeedInstruction(t *testing.T, s *apiServer, id, body string) {
	t.Helper()
	if err := s.dal.PutUpgradeInstruction(t79Instruction(id, body, 1_788_000_000)); err != nil {
		t.Fatalf("seed instruction %s: %v", id, err)
	}
}

// ─── 1. silence when there is nothing to do ────────────────────────────────

// 🔴 THE QUIET SHAPE IS THE FEATURE, NOT AN OPTIMISATION. Most upgrades touch
// nothing the assistant cares about; the one-line body exists so she can skim
// them. A section that printed "0 張交代單" would give every harmless upgrade a
// heading, and a heading on every upgrade is how a signal gets trained into
// noise — which is the exact failure this whole ticket was opened to avoid.
// Nothing else in the suite would go red if the empty case started printing.
func TestUpgradeInstructionsSection_NoOpenInstructionsPrintsNothingAtAll(t *testing.T) {
	if got := upgradeInstructionsSection(nil, nil); got != "" {
		t.Errorf("an upgrade with no instructions grew a section:\n%s", got)
	}
	if got := upgradeInstructionsSection([]UpgradeInstruction{}, nil); got != "" {
		t.Errorf("an EMPTY (non-nil) open set grew a section:\n%s", got)
	}

	// And the whole message must be byte-for-byte the quiet body — not the
	// quiet body plus a separator, which would be the same regression wearing
	// a thinner disguise.
	n := pendingUpgradeNotice{FromVersion: "v0.5.311", FromSHA: "aaaaaaaaaaaa", ToVersion: "v0.5.312", ToSHA: "cccccccccccc"}
	body := upgradeNoticeBody(n, []string{"server/ocserverd/upgrade.go"}, false, nil)
	full := upgradeNoticeMessage(n, []string{"server/ocserverd/upgrade.go"}, false, nil, nil, nil)
	if full != body {
		t.Errorf("with no instructions the message is no longer the quiet body:\nwant %q\ngot  %q", body, full)
	}
}

// ─── 2. the instructions come first ────────────────────────────────────────

// The diff half is skimmable by design; the instructions are the half somebody
// is waiting on. Order them the other way and the actionable part sits below a
// section whose whole design invites skipping — the message still contains
// everything, and still fails at its job. Nothing but position is wrong, so
// nothing but a position assertion catches it.
func TestUpgradeNoticeMessage_TheInstructionsSitAboveTheDiff(t *testing.T) {
	n := pendingUpgradeNotice{FromVersion: "v0.5.311", FromSHA: "aaaaaaaaaaaa", ToVersion: "v0.5.312", ToSHA: "cccccccccccc"}
	open := []UpgradeInstruction{t79Instruction("uin-1", "把 T-33 的 migration 先 land", 1_788_000_000)}
	files := []string{"seeds/system_interaction.md"}

	msg := upgradeNoticeMessage(n, files, false, nil, open, nil)

	instrAt := strings.Index(msg, "把 T-33 的 migration 先 land")
	if instrAt < 0 {
		t.Fatalf("the instruction never reached the message:\n%s", msg)
	}
	// Anchor the diff half on something only it produces.
	diffAt := strings.Index(msg, "seeds/system_interaction.md")
	if diffAt < 0 {
		t.Fatalf("the diff half never reached the message:\n%s", msg)
	}
	if instrAt > diffAt {
		t.Errorf("the instruction (at %d) is BELOW the diff (at %d) — the actionable half is under the skimmable one:\n%s",
			instrAt, diffAt, msg)
	}
}

// ─── 3. a read failure must not read like "he asked for nothing" ───────────

// 🔴 THE TWO OPPOSITE FACTS. "He asked for nothing" and "I could not find out
// what he asked for" lead to opposite actions, and folding the second into the
// first is free to write and impossible to notice: the message looks perfectly
// normal, the assistant reads it as all-clear, and an instruction the owner is
// waiting on is dropped with no error anywhere.
func TestUpgradeInstructionsSection_AReadFailureIsNeverRenderedAsNone(t *testing.T) {
	readErr := errors.New("database is locked")
	got := upgradeInstructionsSection(nil, readErr)

	if got == "" {
		t.Fatal("a FAILED read rendered exactly like a successful empty read — the assistant is told nothing and will act as if the owner asked for nothing")
	}
	if !strings.Contains(got, readErr.Error()) {
		t.Errorf("the section hides what actually failed:\n%s", got)
	}
	if !strings.Contains(got, "這不等於「沒有交代單」") {
		t.Errorf("the section never states the thing a reader is about to get wrong:\n%s", got)
	}

	// And it must survive into the delivered message rather than being
	// swallowed by the assembler.
	n := pendingUpgradeNotice{FromVersion: "v0.5.311", FromSHA: "aaaaaaaaaaaa", ToVersion: "v0.5.312", ToSHA: "cccccccccccc"}
	msg := upgradeNoticeMessage(n, nil, false, nil, nil, readErr)
	if !strings.Contains(msg, "這不等於「沒有交代單」") {
		t.Errorf("the read failure was dropped on the way into the message:\n%s", msg)
	}
}

// ─── 4. the cap must say how many it dropped ───────────────────────────────

// A cap that truncates silently is worse than no cap: the reader sees a
// complete-looking list, ticks all of it, and the backlog he never saw is
// handed over again next time with no explanation. The count is what makes the
// truncation self-declaring.
func TestUpgradeInstructionsSection_TheCapStatesHowManyItLeftOut(t *testing.T) {
	const total = upgradeNoticeMaxInstructions + 3
	var open []UpgradeInstruction
	for i := 0; i < total; i++ {
		open = append(open, t79Instruction(fmt.Sprintf("uin-%02d", i), fmt.Sprintf("instruction number %02d", i), float64(1_788_000_000+i)))
	}

	got := upgradeInstructionsSection(open, nil)

	if !strings.Contains(got, "instruction number 19") {
		t.Errorf("the 20th instruction (the last one inside the cap) was not printed:\n%s", got)
	}
	if strings.Contains(got, "instruction number 20") {
		t.Errorf("the cap did not hold — a 21st instruction was printed:\n%s", got)
	}
	if !strings.Contains(got, "另外還有 3 張") {
		t.Errorf("the section dropped 3 instructions WITHOUT saying so:\n%s", got)
	}
	// The headline count is the honest total, not the printed count: it is the
	// number that tells the reader the list is short.
	if !strings.Contains(got, fmt.Sprintf("還沒完成的有 %d 張", total)) {
		t.Errorf("the headline reports the printed count instead of the real open count (%d):\n%s", total, got)
	}
}

// ─── 5. the first tick wins ────────────────────────────────────────────────

// 🔴 THE ASSISTANT RACING HERSELF IS THE ORDINARY CASE — she is handed the
// whole open set at every upgrade. A second tick that overwrote done_by/done_ts
// would answer 200, look perfect, and quietly replace the record of who
// actually did the work with the record of who read it last. No error, no
// conflict, nothing to see.
func TestMarkUpgradeInstructionDone_TheSecondTickChangesNothing(t *testing.T) {
	d := newTestDAL(t)
	if err := d.PutUpgradeInstruction(t79Instruction("uin-race", "重跑 drift 檢查", 1_788_000_000)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	first, err := d.MarkUpgradeInstructionDone("uin-race", "mira", 1_788_000_100)
	if err != nil || !first {
		t.Fatalf("the first tick must report that it closed the row: closed=%v err=%v", first, err)
	}
	second, err := d.MarkUpgradeInstructionDone("uin-race", "owner", 1_788_000_999)
	if err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if second {
		t.Error("the SECOND tick reported that it closed the row — the loser of a race is being told it won")
	}

	after, err := d.GetUpgradeInstruction("uin-race")
	if err != nil || after == nil {
		t.Fatalf("read back: %v (row=%v)", err, after)
	}
	if after.DoneBy != "mira" {
		t.Errorf("done_by is %q — the second tick overwrote who did the work", after.DoneBy)
	}
	if after.DoneTS != 1_788_000_100 {
		t.Errorf("done_ts is %v — the second tick moved when it was done", after.DoneTS)
	}

	// And the open set must no longer carry it, or the station hands it over
	// forever regardless of the tick.
	openRows, err := d.ListOpenUpgradeInstructions()
	if err != nil {
		t.Fatalf("list open: %v", err)
	}
	for _, u := range openRows {
		if u.ID == "uin-race" {
			t.Error("a ticked instruction is still in the open set — it will be handed over at every upgrade forever")
		}
	}
}

// ─── 6. the two narrowings the ladder cannot express ───────────────────────

// WRITING AND WITHDRAWING ARE THE OWNER'S ALONE. The route floor is
// admin_agent because the tick needs it, so the ladder lets the assistant
// through the door — the handler is the ONLY thing standing between her and
// authoring her own instructions, which would make the row evidence of
// nothing. Delete either check and every route test still passes.
func TestUpgradeInstructionHandlers_WriteAndWithdrawAreTheOwnersAlone(t *testing.T) {
	for _, caller := range []string{seedMiraID, "m-someone-else"} {
		s := t79Server(t)
		t79SeedInstruction(t, s, "uin-existing", "已經在的一張")

		rec := httptest.NewRecorder()
		s.HandleCreateUpgradeInstructionApiUpgradeInstructionsPost(rec,
			t79ReqAs("POST", "/api/upgrade-instructions", caller, `{"body":"我自己交代自己"}`))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s WROTE an instruction (status %d) — the record stops being the owner's order", caller, rec.Code)
		}

		rec = httptest.NewRecorder()
		s.HandleDeleteUpgradeInstructionApiUpgradeInstructionsInstructionIdDelete(rec,
			t79ReqAs("DELETE", "/api/upgrade-instructions/uin-existing", caller, ""), "uin-existing")
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s WITHDREW the owner's instruction (status %d)", caller, rec.Code)
		}
		if row, err := s.dal.GetUpgradeInstruction("uin-existing"); err != nil || row == nil {
			t.Errorf("%s: the refused withdraw still removed the row (err=%v)", caller, err)
		}
	}

	// The owner himself must get through both, or the refusals above are
	// passing for the wrong reason.
	s := t79Server(t)
	rec := httptest.NewRecorder()
	s.HandleCreateUpgradeInstructionApiUpgradeInstructionsPost(rec,
		t79ReqAs("POST", "/api/upgrade-instructions", wireOwnerID, `{"body":"換版後把 drift 檢查重跑一次"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("the OWNER cannot write an instruction: %d %s", rec.Code, rec.Body.String())
	}
	var created UpgradeInstructionDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v (%s)", err, rec.Body.String())
	}
	if created.CreatedBy != wireOwnerID {
		t.Errorf("created_by is %q — it did not come from the verified token", created.CreatedBy)
	}
	rec = httptest.NewRecorder()
	s.HandleDeleteUpgradeInstructionApiUpgradeInstructionsInstructionIdDelete(rec,
		t79ReqAs("DELETE", "/api/upgrade-instructions/"+created.Id, wireOwnerID, ""), created.Id)
	if rec.Code != http.StatusOK {
		t.Fatalf("the OWNER cannot withdraw his own instruction: %d %s", rec.Code, rec.Body.String())
	}
}

// TICKING ADMITS THE OWNER AND THE ASSISTANT, AND NOBODY ELSE. A third party
// ticking would certify work that never happened, and the row would still read
// as done forever after.
func TestUpgradeInstructionHandlers_TickAdmitsTheOwnerAndTheAssistantOnly(t *testing.T) {
	s := t79Server(t)
	t79SeedInstruction(t, s, "uin-a", "第一張")
	t79SeedInstruction(t, s, "uin-b", "第二張")

	rec := httptest.NewRecorder()
	s.HandleCompleteUpgradeInstructionApiUpgradeInstructionsInstructionIdDonePost(rec,
		t79ReqAs("POST", "/api/upgrade-instructions/uin-a/done", "m-someone-else", ""), "uin-a")
	if rec.Code != http.StatusForbidden {
		t.Errorf("an ordinary member ticked an instruction off (status %d) — it now reads as done and nobody did it", rec.Code)
	}
	if row, _ := s.dal.GetUpgradeInstruction("uin-a"); row == nil || row.Done {
		t.Error("the refused tick still closed the row")
	}

	// The assistant is who the instructions are addressed to.
	rec = httptest.NewRecorder()
	s.HandleCompleteUpgradeInstructionApiUpgradeInstructionsInstructionIdDonePost(rec,
		t79ReqAs("POST", "/api/upgrade-instructions/uin-a/done", seedMiraID, ""), "uin-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("the ASSISTANT cannot tick off an instruction addressed to her: %d %s", rec.Code, rec.Body.String())
	}
	if row, _ := s.dal.GetUpgradeInstruction("uin-a"); row == nil || !row.Done || row.DoneBy != seedMiraID {
		t.Errorf("the assistant's tick did not land as hers: %+v", row)
	}

	rec = httptest.NewRecorder()
	s.HandleCompleteUpgradeInstructionApiUpgradeInstructionsInstructionIdDonePost(rec,
		t79ReqAs("POST", "/api/upgrade-instructions/uin-b/done", wireOwnerID, ""), "uin-b")
	if rec.Code != http.StatusOK {
		t.Fatalf("the OWNER cannot tick off his own instruction: %d %s", rec.Code, rec.Body.String())
	}
}

// 🔴 THE HANDLER NARROWING IS ONLY HALF THE ANSWER: the assistant still has to
// clear the ROUTE floor before any handler runs, and that half lives in two
// files nobody edits together — the route rows say admin_agent, and the seed
// row is what makes her one. Break either (drop her role_key, raise a floor to
// owner) and she is locked out of instructions addressed to her, with a 403
// that looks exactly like a correct refusal.
func TestUpgradeInstructionRoutes_TheAssistantClearsTheAdminAgentFloor(t *testing.T) {
	d := newTestDAL(t)
	if err := seedOutOfBox(d); err != nil {
		t.Fatalf("seed out of box: %v", err)
	}
	mira, err := d.GetMember(seedMiraID)
	if err != nil || mira == nil {
		t.Fatalf("the out-of-box seed has no assistant row: %v", err)
	}
	if got := classifyMember(mira); got != principalAdminAgent {
		t.Fatalf("the seeded assistant classifies as %q, not %q — she cannot reach any route at the admin_agent floor",
			got, principalAdminAgent)
	}

	want := map[string]bool{
		"GET /api/upgrade-instructions":                        false,
		"POST /api/upgrade-instructions":                       false,
		"POST /api/upgrade-instructions/{instruction_id}/done": false,
		"DELETE /api/upgrade-instructions/{instruction_id}":    false,
	}
	for _, spec := range routeSpecs(&ServerInterfaceWrapper{}) {
		key := spec.Method + " " + spec.Path
		if _, ok := want[key]; !ok {
			continue
		}
		want[key] = true
		if spec.Requires != principalAdminAgent {
			t.Errorf("%s requires %q, want %q", key, spec.Requires, principalAdminAgent)
		}
		if spec.Auth != authGated {
			t.Errorf("%s is not behind the auth gate", key)
		}
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("route %s is not in the table at all", key)
		}
	}
}

// ─── 7. the delivered message actually carries them ────────────────────────

// Everything above tests the machinery. This one tests that the UPGRADE PATH
// reaches it: the open instructions must be read at delivery, printed in the
// body, and named by id in Meta. Unplug the read and every test above still
// passes while the assistant is handed a message that mentions nothing.
func TestDeliverPendingUpgradeNotice_TheHandOverCarriesTheOpenInstructions(t *testing.T) {
	srv := t79CompareServer(t, []string{"server/ocserverd/upgrade.go"})
	s := t79Server(t)
	s.releaseAPIBase = srv.URL
	t79SeedInstruction(t, s, "uin-open-1", "換版後把 drift 檢查重跑一次")
	t79SeedInstruction(t, s, "uin-open-2", "確認 T-33 的 migration 已經 land")
	// A ticked one must NOT ride along.
	t79SeedInstruction(t, s, "uin-done", "這張已經做完了")
	if _, err := s.dal.MarkUpgradeInstructionDone("uin-done", seedMiraID, 1_788_000_100); err != nil {
		t.Fatalf("tick: %v", err)
	}

	s.processSHA = "aaaaaaaaaaaa"
	s.recordPendingUpgradeNotice("v0.5.312", "cccccccccccc")
	s.processSHA = "cccccccccccc"

	if !s.deliverPendingUpgradeNotice() {
		t.Fatal("delivery reported nothing sent")
	}
	msgs, err := s.dal.ListChatInvolving(seedMiraID, 10)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("assistant received %d messages (err=%v), want 1", len(msgs), err)
	}
	body := msgs[0].Body

	for _, wantText := range []string{"換版後把 drift 檢查重跑一次", "確認 T-33 的 migration 已經 land", "uin-open-1", "uin-open-2"} {
		if !strings.Contains(body, wantText) {
			t.Errorf("the hand-over message never mentions %q:\n%s", wantText, body)
		}
	}
	if strings.Contains(body, "這張已經做完了") {
		t.Errorf("a ticked instruction was handed over again:\n%s", body)
	}

	// Meta must name the ids, so a reader that wants to act on one does not
	// have to parse them back out of prose.
	raw, ok := msgs[0].Meta["upgrade_instructions"]
	if !ok {
		t.Fatalf("Meta carries no upgrade_instructions key: %#v", msgs[0].Meta)
	}
	ids := t79MetaIDs(t, raw)
	if len(ids) != 2 || ids[0] != "uin-open-1" || ids[1] != "uin-open-2" {
		t.Errorf("Meta.upgrade_instructions = %v, want [uin-open-1 uin-open-2]", ids)
	}
}

// t79MetaIDs reads the id list back out of Meta, which survives a round trip
// through JSON as []any rather than []string.
func t79MetaIDs(t *testing.T, raw any) []string {
	t.Helper()
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				t.Fatalf("Meta.upgrade_instructions holds a %T, not a string", e)
			}
			out = append(out, s)
		}
		return out
	default:
		t.Fatalf("Meta.upgrade_instructions is a %T, not a list", raw)
		return nil
	}
}

// upgradeInstructionIDs must answer an EMPTY LIST, never nil: a JSON null and
// an empty array read differently to a client, and "there were none" is a fact
// worth stating rather than an absence.
func TestUpgradeInstructionIDs_NoneIsAnEmptyListNotANull(t *testing.T) {
	ids := upgradeInstructionIDs(nil)
	if ids == nil {
		t.Fatal("no open instructions produced a nil slice — it serialises as null, which reads as \"unknown\" rather than \"none\"")
	}
	if len(ids) != 0 {
		t.Errorf("want an empty list, got %v", ids)
	}
}
