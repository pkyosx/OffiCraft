package main

// scheduled_message_patch_realign_test.go — the cursor guard for the SAVE
// BUTTON, at the route the button actually calls.
//
// The bug this exists to catch: PATCH deciding "re-aim the delivery cursor" from
// which fields APPEARED in the body rather than from whether any of them changed
// VALUE. A row editor's save sends the whole form every time — label, body,
// cadence, the date field the cadence reads, hour, minute, timezone — so under
// the presence rule EVERY save re-seeds the cursor to now, including the saves
// that changed nothing about when the schedule fires. Land one in the window
// between a slot elapsing and the next tick (up to 60s) and that occurrence is
// swallowed for good: no error, no log line, and the card's 上次送出 still reads
// exactly as it should.
//
// c1df2c1 moved the decision to comparison-by-value while no editor existed, so
// the path was unreachable; ae18d65 shipped the save button and made it live.
// This file drives that traffic shape end to end.

import (
	"testing"
	"time"
	"unicode/utf8"
)

const (
	plantedSlot = "1999-01-01T00:00+08:00"
	plantedTS   = 915148800.0
)

// plantStaleCursor points a schedule's cursor at a distinctive long-past
// delivery, so any movement by a later request is unmistakable in the failure
// message rather than a near-miss between two plausible timestamps.
func plantStaleCursor(t *testing.T, api *apiServer, id string) {
	t.Helper()
	stored, err := api.dal.GetScheduledMessage(id)
	if err != nil || stored == nil {
		t.Fatalf("read %s before planting the cursor: %v %v", id, stored, err)
	}
	stored.LastFiredSlot = plantedSlot
	stored.LastFiredTS = plantedTS
	if err := api.dal.PutScheduledMessage(*stored); err != nil {
		t.Fatalf("plant cursor on %s: %v", id, err)
	}
}

func cursorOf(t *testing.T, resp map[string]any) (string, float64) {
	t.Helper()
	slot, ok := resp["last_fired_slot"].(string)
	if !ok {
		t.Fatalf("response carries no last_fired_slot: %v", resp)
	}
	ts, ok := resp["last_fired_ts"].(float64)
	if !ok {
		t.Fatalf("response carries no last_fired_ts: %v", resp)
	}
	return slot, ts
}

// TestUpdateScheduledMessageWholeFormSaveReAimsOnlyOnAChangedTime drives the
// exact payload the row editor's 儲存 emits (ScheduledMessagesCard.wirePayload:
// label, body, cadence, the date field this cadence reads, hour, minute,
// timezone — status is not part of the form) and pins BOTH directions:
//
//	unchanged form → the cursor does not move, not one field
//	changed time   → the cursor re-aims to the slot current now
//
// Only asserting the first half would go green on a handler that never re-aims
// at all, which resurrects the original bug from the other side: an edit that
// moves a schedule from 09:00 to 08:00 at noon would retroactively deliver
// today's 08:00.
func TestUpdateScheduledMessageWholeFormSaveReAimsOnlyOnAChangedTime(t *testing.T) {
	srv, secret, api := scheduledStack(t)
	ownerTok, _ := mintJWT("owner", "owner", 300, secret, time.Now().Unix(), "")

	// Weekly, because it is the cadence whose save carries the MOST fields: a
	// weekly form sends day_of_week on top of everything a daily one sends, so
	// the presence-vs-value question is asked of one more field here than a
	// daily fixture could ask it of.
	status, created := doJSON(t, "POST", srv.URL+"/api/members/mira/scheduled-messages",
		ownerTok, `{"label":"晨會提醒","body":"09:00 站立會議","cadence":"weekly","day_of_week":3,"hour":9,"minute":0,"timezone":"Asia/Taipei"}`)
	if status != 200 {
		t.Fatalf("create: %d %v", status, created)
	}
	id, _ := created["id"].(string)
	path := srv.URL + "/api/members/mira/scheduled-messages/" + id

	const sameForm = `{"label":"晨會提醒","body":"09:00 站立會議","cadence":"weekly","day_of_week":3,"hour":9,"minute":0,"timezone":"Asia/Taipei"}`

	plantStaleCursor(t, api, id)
	status, saved := doJSON(t, "PATCH", path, ownerTok, sameForm)
	if status != 200 {
		t.Fatalf("saving the form unchanged: %d %v", status, saved)
	}
	slot, ts := cursorOf(t, saved)
	if slot != plantedSlot {
		t.Fatalf("a save that changed NOTHING moved last_fired_slot from %q to %q — "+
			"a save landing in the gap between a slot elapsing and the next tick now "+
			"swallows that delivery permanently, with nothing to show for it",
			plantedSlot, slot)
	}
	if ts != plantedTS {
		t.Fatalf("a save that changed NOTHING rewrote last_fired_ts from %v to %v — "+
			"that field says when a delivery happened, and a save is not one",
			plantedTS, ts)
	}

	// The same whole form with only the TEXT edited: a real edit, still not a
	// re-aim. This is also what stops the assertion above from passing on a
	// handler that quietly persists nothing — the body must actually change.
	//
	// T-91: the update receipt no longer echoes the body (the caller sent it);
	// it reports body_size_chars instead. The anti-vacuity check is therefore
	// made against the STORED ROW, which is a stronger place for it — the whole
	// worry this line exists for is a handler that persists nothing.
	const editedBody = "09:00 站立會議(改)"
	status, saved = doJSON(t, "PATCH", path, ownerTok,
		`{"label":"晨會提醒","body":"`+editedBody+`","cadence":"weekly","day_of_week":3,"hour":9,"minute":0,"timezone":"Asia/Taipei"}`)
	if status != 200 {
		t.Fatalf("saving an edited body: %d %v", status, saved)
	}
	if stored, err := api.dal.GetScheduledMessage(id); err != nil || stored == nil {
		t.Fatalf("load the edited schedule: %v %v", stored, err)
	} else if stored.Body != editedBody {
		t.Fatalf("the edit did not land: stored body is %q — the cursor assertions "+
			"above were measuring a write that never happened", stored.Body)
	}
	if got, _ := saved["body_size_chars"].(float64); int(got) != utf8.RuneCountInString(editedBody) {
		t.Fatalf("the receipt must report the stored body's size in RUNES: got %v, want %d",
			saved["body_size_chars"], utf8.RuneCountInString(editedBody))
	}
	slot, ts = cursorOf(t, saved)
	if slot != plantedSlot || ts != plantedTS {
		t.Fatalf("editing the message TEXT moved the cursor to (%q, %v) — the slots "+
			"did not move, so the cursor may not either", slot, ts)
	}

	// Control: the same whole form with the hour genuinely changed. The cursor
	// MUST re-aim now, otherwise the next tick delivers the slot this edit just
	// crossed.
	status, saved = doJSON(t, "PATCH", path, ownerTok,
		`{"label":"晨會提醒","body":"09:00 站立會議(改)","cadence":"weekly","day_of_week":3,"hour":8,"minute":0,"timezone":"Asia/Taipei"}`)
	if status != 200 {
		t.Fatalf("saving a changed hour: %d %v", status, saved)
	}
	slot, ts = cursorOf(t, saved)
	if slot == plantedSlot || slot == "" {
		t.Fatalf("moving the hour left last_fired_slot at %q — the slot the edit "+
			"crossed will be delivered retroactively on the next tick", slot)
	}
	if aimed := mustParseSlot(t, slot); aimed.After(time.Now()) {
		t.Fatalf("the re-aimed cursor %q is in the FUTURE — it must be the slot most "+
			"recently elapsed, not the next one", slot)
	}
	if ts != plantedTS {
		t.Fatalf("re-aiming rewrote last_fired_ts from %v to %v — re-aiming skips a "+
			"slot, it does not deliver one", plantedTS, ts)
	}

	// And the same for the date field a weekly form carries: day_of_week is a
	// slot field, so changing it re-aims exactly like the hour does.
	plantStaleCursor(t, api, id)
	status, saved = doJSON(t, "PATCH", path, ownerTok,
		`{"label":"晨會提醒","body":"09:00 站立會議(改)","cadence":"weekly","day_of_week":5,"hour":8,"minute":0,"timezone":"Asia/Taipei"}`)
	if status != 200 {
		t.Fatalf("saving a changed day_of_week: %d %v", status, saved)
	}
	if slot, _ = cursorOf(t, saved); slot == plantedSlot {
		t.Fatalf("moving the weekday left last_fired_slot at %q — day_of_week picks "+
			"which slots exist, so changing it re-aims", slot)
	}
}
