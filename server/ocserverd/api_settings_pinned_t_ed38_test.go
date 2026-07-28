package main

// T-ed38 — display.pinned_member_ids on GET/PATCH /api/settings.
//
// The pinned set is the owner's manual roster order. Four properties matter,
// and each of them is a way the feature can be silently wrong:
//
//   - it reads back as an ARRAY, never null (a null would make the frontend's
//     "settings read failed → []" degradation indistinguishable from "nothing
//     is pinned");
//   - the ORDER round-trips verbatim (the array order IS the display order —
//     a set-semantics store would scramble the owner's manual ordering);
//   - it is a WHOLE-SET replace, and omitting the key changes nothing;
//   - a blank or duplicate id is a 422 and NOTHING is written (validate-all
//     before write — a half-applied pin list is worse than a refusal).

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func pinnedFromSettings(t *testing.T, data map[string]any) []string {
	t.Helper()
	raw, ok := data["pinned_member_ids"]
	if !ok {
		t.Fatalf("pinned_member_ids must always be on the wire: %v", data)
	}
	if raw == nil {
		t.Fatalf("pinned_member_ids must be an array, never null: %v", data)
	}
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("pinned_member_ids must be an array, got %T: %v", raw, data)
	}
	out := make([]string, len(items))
	for i, v := range items {
		out[i], _ = v.(string)
	}
	return out
}

func TestPinnedMemberIDsRoundTripInOrder(t *testing.T) {
	_, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "",
		`{"password":"settings-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	// Never set → an EMPTY ARRAY, not null.
	status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, "")
	if status != 200 {
		t.Fatalf("get settings: %d", status)
	}
	if got := pinnedFromSettings(t, data); len(got) != 0 {
		t.Fatalf("an untouched install must read back []: %v", got)
	}

	// A pinned set whose order is NOT sorted, NOT insertion-agnostic — the
	// owner's own order, newest pin first.
	want := []string{"m-zeta", "m-alpha", "m-mid"}
	body, _ := json.Marshal(map[string]any{"pinned_member_ids": want})
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, string(body))
	if status != 200 {
		t.Fatalf("patch: %d %v", status, data)
	}
	assertPinnedOrder(t, pinnedFromSettings(t, data), want, "the PATCH echo")

	// Durable: a fresh GET returns the same order.
	status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, "")
	if status != 200 {
		t.Fatalf("get after patch: %d", status)
	}
	assertPinnedOrder(t, pinnedFromSettings(t, data), want, "a later GET")

	// An UNRELATED patch must not touch it (omitted = unchanged).
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"handover_pct":55}`)
	if status != 200 {
		t.Fatalf("unrelated patch: %d %v", status, data)
	}
	assertPinnedOrder(t, pinnedFromSettings(t, data), want, "an unrelated PATCH")

	// A whole-set replace, not a merge: the new set REPLACES the old one.
	body, _ = json.Marshal(map[string]any{"pinned_member_ids": []string{"m-only"}})
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, string(body))
	if status != 200 {
		t.Fatalf("replace patch: %d %v", status, data)
	}
	assertPinnedOrder(t, pinnedFromSettings(t, data), []string{"m-only"}, "a replacing PATCH")

	// [] clears.
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, `{"pinned_member_ids":[]}`)
	if status != 200 {
		t.Fatalf("clear patch: %d %v", status, data)
	}
	if got := pinnedFromSettings(t, data); len(got) != 0 {
		t.Fatalf("[] must clear the set, got %v", got)
	}
}

func assertPinnedOrder(t *testing.T, got, want []string, where string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: want %v, got %v", where, want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: order must round-trip verbatim; want %v, got %v", where, want, got)
		}
	}
}

// The pinned set lives in ONE settings JSON row that boot deserialises into the
// in-memory snapshot and every GET /api/settings echoes back, and the write
// floor is admin_agent — i.e. any role_key=="assistant" agent, not just a human
// at the keyboard. An unbounded array is therefore the only way to bloat that
// row, which is exactly the reasoning maxCustomThemes already wrote down for
// the neighbouring field (theme_bundle.go). Both edges matter: the cap must
// refuse what is over it AND admit what sits exactly on it.
func TestPinnedMemberIDsBoundTheArrayAndEachID(t *testing.T) {
	_, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "",
		`{"password":"settings-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("m-%d", i)
		}
		return out
	}

	// EXACTLY at the cap is legal — a cap that also refuses its own boundary
	// would be a different (and unannounced) limit.
	atCap := ids(maxPinnedMemberIDs)
	body, _ := json.Marshal(map[string]any{"pinned_member_ids": atCap})
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, string(body))
	if status != 200 {
		t.Fatalf("exactly %d ids must be accepted, got %d %v",
			maxPinnedMemberIDs, status, data)
	}
	assertPinnedOrder(t, pinnedFromSettings(t, data), atCap, "a full-cap PATCH")

	// One over the cap, and one id longer than a member id can be. Each must
	// 422 AND leave the stored set untouched (validate-all-then-write).
	tooMany, _ := json.Marshal(map[string]any{
		"pinned_member_ids": ids(maxPinnedMemberIDs + 1),
	})
	tooLong, _ := json.Marshal(map[string]any{
		"pinned_member_ids": []string{
			"m-ok", strings.Repeat("x", maxPinnedMemberIDLen+1),
		},
	})
	for _, bad := range []string{string(tooMany), string(tooLong)} {
		if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, bad); status != 422 {
			t.Fatalf("an over-cap set must be 422, got %d %v", status, data)
		}
		status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, "")
		if status != 200 {
			t.Fatalf("get after refusal: %d", status)
		}
		assertPinnedOrder(t, pinnedFromSettings(t, data), atCap,
			"after refusing an over-cap set")
	}

	// The boundary of the per-id length is legal too.
	edge := []string{strings.Repeat("y", maxPinnedMemberIDLen)}
	body, _ = json.Marshal(map[string]any{"pinned_member_ids": edge})
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, string(body))
	if status != 200 {
		t.Fatalf("an id of exactly %d chars must be accepted, got %d %v",
			maxPinnedMemberIDLen, status, data)
	}
	assertPinnedOrder(t, pinnedFromSettings(t, data), edge, "a max-length id")
}

// Padding is not part of an id. The handler rejects an id that is empty AFTER
// TrimSpace, so the trimmed form is already the one it validates — storing the
// raw form instead would accept " m-bob ", which can never match a real member
// id and becomes a pin the owner can neither see nor clear. org_name /
// owner_name in the same handler trim first and store the trimmed value; this
// pins that same shape here, on both halves: the stored value is trimmed, and
// the duplicate check sees the trimmed values (otherwise "m-bob" and " m-bob "
// would both land in the set and the second would be the unreachable one).
func TestPinnedMemberIDsAreStoredTrimmed(t *testing.T) {
	_, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "",
		`{"password":"settings-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	// A padded id is accepted, but what lands is the trimmed id.
	body, _ := json.Marshal(map[string]any{
		"pinned_member_ids": []string{" m-bob ", "\tm-cara\n"},
	})
	status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, string(body))
	if status != 200 {
		t.Fatalf("a padded id must be accepted: %d %v", status, data)
	}
	want := []string{"m-bob", "m-cara"}
	assertPinnedOrder(t, pinnedFromSettings(t, data), want, "the PATCH echo")

	// Durable, not just an echo — the padding is gone from the stored row too.
	status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, "")
	if status != 200 {
		t.Fatalf("get after patch: %d", status)
	}
	assertPinnedOrder(t, pinnedFromSettings(t, data), want, "a later GET")

	// A padded id that collides with an already-trimmed one is a DUPLICATE,
	// not a second entry — and the refusal writes nothing.
	dup, _ := json.Marshal(map[string]any{
		"pinned_member_ids": []string{"m-bob", " m-bob "},
	})
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, string(dup)); status != 422 {
		t.Fatalf("a padded duplicate must be 422, got %d %v", status, data)
	}
	status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, "")
	if status != 200 {
		t.Fatalf("get after refusal: %d", status)
	}
	assertPinnedOrder(t, pinnedFromSettings(t, data), want,
		"after refusing a padded duplicate")
}

func TestPinnedMemberIDsRejectBlankAndDuplicateWithoutWriting(t *testing.T) {
	_, srv, _, _ := newSettingsTestServer(t, "settings-pass")
	status, data := doJSON(t, "POST", srv.URL+"/api/login", "",
		`{"password":"settings-pass"}`)
	if status != 200 {
		t.Fatalf("login: %d", status)
	}
	owner := data["token"].(string)

	// Seed a known-good set so we can prove a refusal left it untouched.
	good := []string{"m-a", "m-b"}
	body, _ := json.Marshal(map[string]any{"pinned_member_ids": good})
	if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, string(body)); status != 200 {
		t.Fatalf("seed patch: %d %v", status, data)
	}

	for _, bad := range []string{
		`{"pinned_member_ids":["m-a",""]}`,
		`{"pinned_member_ids":["m-a","   "]}`,
		`{"pinned_member_ids":["m-a","m-b","m-a"]}`,
	} {
		if status, data = doJSON(t, "PATCH", srv.URL+"/api/settings", owner, bad); status != 422 {
			t.Fatalf("%s must be 422, got %d %v", bad, status, data)
		}
		// …and NOTHING was written: the refusal is validate-all-then-write.
		status, data = doJSON(t, "GET", srv.URL+"/api/settings", owner, "")
		if status != 200 {
			t.Fatalf("get after refusal: %d", status)
		}
		assertPinnedOrder(t, pinnedFromSettings(t, data), good, "after refusing "+bad)
	}
}
