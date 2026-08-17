package main

// api_chat_budget_setting_tc9b4_test.go — T-c9b4: the wake snapshot's chat
// block budget is the `chat.budget_chars` SETTING, not a constant.
//
// 🔴 EVERY test here is written so it CANNOT pass on the pre-change code. That
// is the whole point of the ticket: a guard that would stay green if someone
// reverted the change is not a guard.
//
//   - The default test asserts 6000. The constant it replaced was 8000, so a
//     revert moves the number.
//   - The two-values test packs the SAME conversation twice under two different
//     settings and requires the carried message count AND chat_chars to DIFFER.
//     A constant budget makes the two runs identical, so a revert cannot satisfy
//     it by accident — and neither can a server that reads the setting once at
//     boot and caches it.
//   - The one-source test asserts the peek's numbers against the snapshot's
//     under a NON-DEFAULT budget. Under the default the two faces agree for a
//     trivial reason (both would be reading the same constant); a value nobody
//     ships makes the agreement mean something.
//
// The control test (chat_earlier_omitted) exists because this ticket promised
// to change ONLY where the budget comes from — the packing algorithm and the
// cut reporting had to stay put.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// chatBudgetCorpus seeds a stream far larger than any legal budget, so the
// packer always has to choose. Uniform 200-rune messages make "fewer messages"
// a well-defined observation.
func chatBudgetCorpus(t *testing.T, api *apiServer) {
	t.Helper()
	chunk := strings.Repeat("字", 200)
	peers := []string{"m-quiet", "m-peer", "m-third", wireOwnerID}
	ts := 1.0
	for i := 0; i < 60; i++ {
		for _, peer := range peers {
			putChat(t, api, fmt.Sprintf("cb-%s-%d", peer, i), peer, "m-exec", chunk, ts, nil)
			ts++
		}
	}
}

// patchChatBudget drives the REAL settings write face — the one the settings
// page calls — rather than assigning the field. The ticket's acceptance is
// "adjustable from the settings page", and a test that sets the struct field
// proves nothing about the PATCH handler, the DB row, or the validation range.
func patchChatBudget(t *testing.T, api *apiServer, n int) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleUpdateSettingsApiSettingsPatch(rec,
		taskReq(t, http.MethodPatch, "/api/settings",
			map[string]any{"chat_budget_chars": n}, "owner", "owner"))
	return rec
}

func chatBudgetSettings(t *testing.T, api *apiServer) settingsDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	api.HandleGetSettingsApiSettingsGet(rec,
		taskReq(t, http.MethodGet, "/api/settings", nil, "owner", "owner"))
	if rec.Code != http.StatusOK {
		t.Fatalf("get settings: %d %s", rec.Code, rec.Body.String())
	}
	return decodeBody[settingsDTO](t, rec)
}

// TestChatBudget_UnsetIsSixThousandOnEveryFace pins the shipped default on the
// three places it has to agree: the load face (a DB with no row), the read face
// (GET /api/settings), and the accessor the packer actually reads.
func TestChatBudget_UnsetIsSixThousandOnEveryFace(t *testing.T) {
	api := resumeCtxServer(t)

	if got := api.chatBudget(); got != 6000 {
		t.Fatalf("unset chat budget must be 6000, got %d", got)
	}
	if got := chatBudgetSettings(t, api).ChatBudgetChars; got != 6000 {
		t.Fatalf("GET /api/settings must report 6000 when unset, got %d", got)
	}

	// The load face, on a database that has never had the row written.
	loaded, err := loadAuthSettings(api.dal, defaultConfig(), func(string) {})
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if loaded.chatBudgetChars != 6000 {
		t.Fatalf("boot-time load must default to 6000, got %d", loaded.chatBudgetChars)
	}
}

// TestChatBudget_SettingIsWritableAndReadsBack is the settings-page round trip:
// PATCH, read back through GET, and confirm the value survived to the DB row so
// it is still there after a restart.
func TestChatBudget_SettingIsWritableAndReadsBack(t *testing.T) {
	api := resumeCtxServer(t)

	if rec := patchChatBudget(t, api, 4000); rec.Code != http.StatusOK {
		t.Fatalf("patch chat_budget_chars=4000: %d %s", rec.Code, rec.Body.String())
	}
	if got := chatBudgetSettings(t, api).ChatBudgetChars; got != 4000 {
		t.Fatalf("read-back after patch: got %d, want 4000", got)
	}
	if got := api.chatBudget(); got != 4000 {
		t.Fatalf("live accessor after patch: got %d, want 4000", got)
	}
	// Survives a restart: reload from the DB the way boot does.
	loaded, err := loadAuthSettings(api.dal, defaultConfig(), func(string) {})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.chatBudgetChars != 4000 {
		t.Fatalf("reload after patch: got %d, want 4000", loaded.chatBudgetChars)
	}
}

// TestChatBudget_RangeIsOneThousandToThirteenThousand pins the two boundaries
// this ticket ruled on, and both of them are load-bearing:
//
//   - The FLOOR is 1000, NOT the shipped default. Copying the doc.cap_chars.*
//     rule (floor == default) would make the knob raise-only, and "the owner can
//     dial it down to 4000" is exactly what the ticket asked for. 4000 being
//     accepted above is that rule in action; 999 being refused pins the edge.
//   - The CEILING is 13000 because resumeChatFetch (500) × the cheapest possible
//     message (27 runes) = 13,500 must exceed the budget. Raising this without
//     raising that constant breaks the packer's no-starvation guarantee.
func TestChatBudget_RangeIsOneThousandToThirteenThousand(t *testing.T) {
	api := resumeCtxServer(t)

	for _, tc := range []struct {
		n    int
		want int
	}{
		{999, http.StatusUnprocessableEntity},
		{1000, http.StatusOK},
		{13000, http.StatusOK},
		{13001, http.StatusUnprocessableEntity},
	} {
		rec := patchChatBudget(t, api, tc.n)
		if rec.Code != tc.want {
			t.Fatalf("chat_budget_chars=%d: got %d, want %d (%s)",
				tc.n, rec.Code, tc.want, rec.Body.String())
		}
	}

	// A refused value must not have been half-applied.
	if got := api.chatBudget(); got != 13000 {
		t.Fatalf("a refused patch must leave the last accepted value in place, got %d", got)
	}

	// A hand-edited DB row outside the range must stop the server booting, not
	// install a budget the PATCH face would have refused.
	if err := api.dal.PutSetting(settingChatBudgetChars, "99999"); err != nil {
		t.Fatalf("seed out-of-range row: %v", err)
	}
	if _, err := loadAuthSettings(api.dal, defaultConfig(), func(string) {}); err == nil {
		t.Fatal("an out-of-range chat.budget_chars row must fail the load, not be accepted")
	}
}

// TestChatBudget_TwoValuesPackTheSameConversationDifferently is THE proof that
// the packer reads the setting rather than a constant.
//
// Same server, same messages, two budgets — the carried message count and
// chat_chars must both move. Under a constant they would be identical, so this
// test cannot go green on the reverted code.
func TestChatBudget_TwoValuesPackTheSameConversationDifferently(t *testing.T) {
	api := resumeCtxServer(t)
	chatBudgetCorpus(t, api)

	if rec := patchChatBudget(t, api, 2000); rec.Code != http.StatusOK {
		t.Fatalf("patch 2000: %d %s", rec.Code, rec.Body.String())
	}
	small := resumeSnapshot(t, api, "m-exec")

	if rec := patchChatBudget(t, api, 12000); rec.Code != http.StatusOK {
		t.Fatalf("patch 12000: %d %s", rec.Code, rec.Body.String())
	}
	large := resumeSnapshot(t, api, "m-exec")

	// Anti-vacuity: both runs must actually have carried something and both must
	// have had to drop something, or "they differ" could be an artefact of one
	// of them being empty.
	if len(small.Chat) == 0 || len(large.Chat) == 0 {
		t.Fatalf("fixture bug: a run served nothing (small=%d large=%d)",
			len(small.Chat), len(large.Chat))
	}

	if len(large.Chat) <= len(small.Chat) {
		t.Fatalf("a larger budget must carry MORE messages: 2000 -> %d messages, "+
			"12000 -> %d messages (the packer is not reading the setting)",
			len(small.Chat), len(large.Chat))
	}
	if large.Overview.ChatChars <= small.Overview.ChatChars {
		t.Fatalf("a larger budget must spend MORE characters: 2000 -> %d chars, "+
			"12000 -> %d chars", small.Overview.ChatChars, large.Overview.ChatChars)
	}

	// Each run must still respect its own ceiling — "different" is not enough.
	if small.Overview.ChatChars > 2000 {
		t.Fatalf("chat_chars %d exceeds the 2000 budget it was packed under",
			small.Overview.ChatChars)
	}
	if large.Overview.ChatChars > 12000 {
		t.Fatalf("chat_chars %d exceeds the 12000 budget it was packed under",
			large.Overview.ChatChars)
	}
}

// TestChatBudget_PeekAndSnapshotReadOneSource pins that
// peek_resume_summary_size and resume_summary are bounded by the SAME live
// setting — not by two copies that happen to agree today.
//
// 🔴 Asserted under a budget nobody ships. Under the default, "the two faces
// agree" is satisfied by two independent constants of the same value; a value
// only this test writes makes the agreement evidence of a shared read.
func TestChatBudget_PeekAndSnapshotReadOneSource(t *testing.T) {
	api := resumeCtxServer(t)
	chatBudgetCorpus(t, api)

	for _, budget := range []int{2000, 12000} {
		if rec := patchChatBudget(t, api, budget); rec.Code != http.StatusOK {
			t.Fatalf("patch %d: %d %s", budget, rec.Code, rec.Body.String())
		}
		snap := resumeSnapshot(t, api, "m-exec")
		peek := peekResumeSize(t, api, "m-exec")

		if len(snap.Chat) == 0 {
			t.Fatalf("budget %d: fixture bug, the snapshot carried nothing", budget)
		}
		if peek.Overview.ChatChars != snap.Overview.ChatChars {
			t.Fatalf("budget %d: peek chat_chars %d != snapshot chat_chars %d — "+
				"the two faces are not reading one budget",
				budget, peek.Overview.ChatChars, snap.Overview.ChatChars)
		}
		if peek.Overview.ChatCount != snap.Overview.ChatCount ||
			peek.Overview.ChatCount != len(snap.Chat) {
			t.Fatalf("budget %d: peek chat_count %d, snapshot chat_count %d, "+
				"messages carried %d", budget,
				peek.Overview.ChatCount, snap.Overview.ChatCount, len(snap.Chat))
		}
		if peek.EstimatedTotalChars < peek.Overview.ChatChars {
			t.Fatalf("budget %d: estimated_total_chars %d cannot be below the chat "+
				"block it is derived from (%d)",
				budget, peek.EstimatedTotalChars, peek.Overview.ChatChars)
		}
	}
}

// TestChatBudget_CutReportingIsUnchangedByTheSetting is the CONTROL. The ticket
// changed only where the budget comes from — chat_earlier_omitted and its hint
// must behave exactly as before at every budget: raised when something was left
// out, absent when nothing was.
func TestChatBudget_CutReportingIsUnchangedByTheSetting(t *testing.T) {
	// (a) A stream that overruns any legal budget always reports the cut, and
	//     always with the same hint text — at the floor, at the default and at
	//     the ceiling.
	for _, budget := range []int{minChatBudgetChars, chatBudgetCharsDefault, maxChatBudgetChars} {
		api := resumeCtxServer(t)
		chatBudgetCorpus(t, api)
		if rec := patchChatBudget(t, api, budget); rec.Code != http.StatusOK {
			t.Fatalf("patch %d: %d %s", budget, rec.Code, rec.Body.String())
		}
		snap := resumeSnapshot(t, api, "m-exec")
		if !snap.ChatEarlierOmitted.Omitted {
			t.Fatalf("budget %d: messages were dropped, the cut must be reported", budget)
		}
		if snap.ChatEarlierOmitted.Hint != resumeChatCutHint {
			t.Fatalf("budget %d: cut hint changed: %q", budget, snap.ChatEarlierOmitted.Hint)
		}
	}

	// (b) A stream that fits reports NO cut, even at the smallest legal budget.
	//     Without this half, "always reports the cut" would be satisfied by a
	//     server that raised the flag unconditionally.
	api := resumeCtxServer(t)
	putChat(t, api, "cb-tiny", "m-peer", "m-exec", "短", 1.0, nil)
	if rec := patchChatBudget(t, api, minChatBudgetChars); rec.Code != http.StatusOK {
		t.Fatalf("patch floor: %d %s", rec.Code, rec.Body.String())
	}
	snap := resumeSnapshot(t, api, "m-exec")
	if len(snap.Chat) != 1 {
		t.Fatalf("fixture bug: expected the one seeded message, got %d", len(snap.Chat))
	}
	if snap.ChatEarlierOmitted.Omitted || snap.ChatEarlierOmitted.Hint != "" {
		t.Fatalf("nothing was left out, so no cut may be reported: %+v",
			snap.ChatEarlierOmitted)
	}
}
