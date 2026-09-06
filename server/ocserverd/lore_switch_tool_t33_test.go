package main

// lore_switch_tool_t33_test.go — T-33: GET /api/lore-switch (`get_lore_switch`),
// the one route that answers 「傳承功能開著嗎？」.
//
// 🔴 BOTH TESTS BELOW EXIST TO CATCH ONE MISTAKE EACH, AND NEITHER MISTAKE
// ANNOUNCES ITSELF. The route is a nine-line handler over a bool; nothing about
// it can break in an interesting way. What CAN break is the two declarations on
// its row in the table — LoreGated and Requires — and both failures land as a
// 403 that reads, from the caller's side, exactly like the other one and
// exactly like a permission problem the caller caused.
//
//   - LoreGated: true would make this tool refuse in the ONLY situation it is
//     for. Every other test of this route would still pass, because they all
//     run with the feature on.
//   - Requires: principalAdminAgent would leave it working for the owner and
//     for 銀月 — which is who writes and runs the tests — and silently useless
//     for every ordinary member, 正職 and 外包, which is the whole population
//     the owner granted it to (rc-2972dcd48782, 2026-09-06).

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// loreSwitchBody decodes the route's whole body: one field, by design.
type loreSwitchBody struct {
	LoreEnabled bool `json:"lore_enabled"`
}

// TestLoreSwitchAnswersWhileTheFeatureIsSwitchedOff is the reason this route was
// added at all.
//
// 🔴 IT ASSERTS 200 AND THE VALUE `false`, NOT 「not 403」. A route that answered
// 200 with `true` on an off station would pass a weaker test and send its reader
// straight into a 403 from write_lore_entry, which is the confusion this whole
// tool is supposed to end.
//
// 🔴 THE POSITIVE CONTROL IS THE SECOND HALF: the SAME route on the SAME server
// with the switch ON must say `true`. Without it, a handler hard-wired to
// `false` — or one that never reads the setting — passes the first half
// perfectly and is wrong on every station that ever turns the feature on.
//
// MUTANT (verified): adding `LoreGated: true` to this row in routes.go turns
// the first half red with 403 and the 「功能關閉」 refusal body.
func TestLoreSwitchAnswersWhileTheFeatureIsSwitchedOff(t *testing.T) {
	url, _, api, agentTok, _ := loreToggleStack(t)
	disableLoreForTest(api)

	st, body := rosterREST(t, url, agentTok, "GET", "/api/lore-switch", "")
	if st != http.StatusOK {
		t.Fatalf("GET /api/lore-switch with the feature OFF: want 200, got %d %s — "+
			"this route exists to be callable exactly here; if it is 403 the row has "+
			"picked up the lore feature gate and is unusable in the only situation "+
			"anybody asks", st, body)
	}
	var off loreSwitchBody
	if err := json.Unmarshal([]byte(body), &off); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	if off.LoreEnabled {
		t.Errorf("the switch is off and the route says lore_enabled=true: %s", body)
	}
	// The refusal wording must NOT be what came back: a 200 carrying the gate's
	// sentence would mean the gate ran and the body is a lie about it.
	if strings.Contains(body, "lore.enabled = false，預設就是關的") {
		t.Errorf("the route answered with the feature-gate refusal text: %s", body)
	}

	// Control: the same route, same server, switch flipped.
	enableLoreForTest(api)
	st, body = rosterREST(t, url, agentTok, "GET", "/api/lore-switch", "")
	if st != http.StatusOK {
		t.Fatalf("GET /api/lore-switch with the feature ON: want 200, got %d %s", st, body)
	}
	var on loreSwitchBody
	if err := json.Unmarshal([]byte(body), &on); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	if !on.LoreEnabled {
		t.Error("control failed: the switch is ON and the route still says " +
			"lore_enabled=false — the `false` above was not an answer, it was a constant")
	}
}

// TestLoreSwitchIsReachableByAnOrdinaryMemberAndAnOutsourceWorker pins the floor
// the owner ruled on: 「新一支 tool，只回這個開關（權限半徑就一格）」 was the
// alternative he took to opening `get_settings` to ordinary members, so a floor
// that keeps ordinary members out gives back the thing he chose.
//
// 🔴 外包 IS ASSERTED SEPARATELY FROM 正職 EVEN THOUGH ONE LADDER RANKS BOTH.
// classifyMember ranks them identically today (neither is a warden, neither
// carries the assistant role_key), so one check would technically cover both —
// and that is precisely why the second one is written down: the station DOES
// have hard rules keyed on Member.Kind == KindOutsource (isOutsourceMember, the
// 正職授權矩陣), so 「outsource is refused here」 is a live shape elsewhere in
// this codebase and would not look like a bug if it appeared on this row.
//
// 🔴 THE NEGATIVE CONTROL IS THE THIRD CASE: an unauthenticated call must still
// be refused. Without it, a row accidentally declared authPublic would pass both
// halves above and would be handing a station's feature inventory to anybody who
// can reach the port.
//
// MUTANT (verified): changing this row's Requires to principalAdminAgent turns
// both member cases red with 403.
func TestLoreSwitchIsReachableByAnOrdinaryMemberAndAnOutsourceWorker(t *testing.T) {
	url, dal, api, staffTok, _ := loreToggleStack(t)
	disableLoreForTest(api) // the situation the tool is for

	if err := dal.PutMember(Member{
		ID: "ow-lore-switch", Name: "O-33", Kind: KindOutsource, Effort: "medium",
		DesiredState: DesiredStateOffline, RosterStatus: RosterStatusActive,
	}); err != nil {
		t.Fatalf("put outsource member: %v", err)
	}
	workerTok, err := mintJWT("ow-lore-switch", "agent", 3600,
		api.keys.signingSecret(), time.Now().Unix(), "")
	if err != nil {
		t.Fatalf("mint outsource token: %v", err)
	}

	for _, c := range []struct{ name, token string }{
		{"正職", staffTok},
		{"外包", workerTok},
	} {
		st, body := rosterREST(t, url, c.token, "GET", "/api/lore-switch", "")
		if st != http.StatusOK {
			t.Errorf("%s member calling get_lore_switch: want 200, got %d %s — "+
				"owner ruling rc-2972dcd48782 put this tool at the ordinary-member "+
				"floor precisely so it would not need the settings read's floor",
				c.name, st, body)
		}
	}

	// Negative control: no credential, no answer.
	st, body := rosterREST(t, url, "", "GET", "/api/lore-switch", "")
	if st == http.StatusOK {
		t.Errorf("an unauthenticated caller read the station's lore switch: %d %s",
			st, body)
	}
}

// TestLoreOffSectionNamesAToolTheStationActuallyServes closes the loop between
// the two halves of this change: the boot section printed on an OFF station
// tells its reader to call a tool by NAME, and a name nobody serves is worse
// than no line at all — it spends the reader's one attempt and teaches him the
// feature is broken rather than merely off.
//
// It reads the name out of the SERVED TABLE rather than out of the constant, so
// renaming the tool in routes.go without touching the notice is red.
func TestLoreOffSectionNamesAToolTheStationActuallyServes(t *testing.T) {
	s := newWorkerTestServer(t)
	disableLoreForTest(s)

	text, _, err := s.foldLoreSectionWithSurfacing("m-anyone")
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	var served string
	for _, spec := range specsFor(s) {
		if spec.Path == "/api/lore-switch" {
			served = spec.MCPTool
		}
	}
	if served == "" {
		t.Fatal("the table serves no /api/lore-switch row — the notice below points nowhere")
	}
	if !strings.Contains(text, served) {
		t.Errorf("the OFF 傳承 section does not name the served tool %q: %s", served, text)
	}
	// The other half of the owner's ruling on that section: it must name the
	// alternative, in the spirit of loreDisabledMessage. A notice that only says
	// 「off」 turns a refusal into lost knowledge.
	for _, want := range []string{"lore.enabled", "patch_lessons", "write_task_learnings"} {
		if !strings.Contains(text, want) {
			t.Errorf("the OFF 傳承 section does not mention %q: %s", want, text)
		}
	}
}
