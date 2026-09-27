package main

// Delivery rides the ORDINARY chat path — no new event type, SSE topic or
// mailbox (owner 2026-08-10).

import (
	"fmt"
	"os"
	"time"
)

// The finest schedulable grain is a minute; the fire/skip test is slot identity,
// not elapsed time, so a late tick still delivers exactly once.
const scheduledMessageCadence = time.Minute

func schedLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[scheduled] "+format+"\n", args...)
}

func (s *apiServer) startScheduledMessageCadence(period time.Duration) {
	go func() {
		for {
			time.Sleep(period)
			s.runScheduledMessageTick(nowSecs())
		}
	}()
	schedLog("cadence started (period=%gs)", period.Seconds())
}

func (s *apiServer) runScheduledMessageTick(now float64) {
	rows, err := s.dal.ListAllEnabledScheduledMessages()
	if err != nil {
		schedLog("tick: listing schedules failed: %v", err)
		return
	}
	at := time.Unix(int64(now), 0)
	for _, sm := range rows {
		// Re-checked here on purpose: time.LoadLocation accepts `Local` (the HOST's
		// zone) and `` (UTC), and a row can predate the write-seam rule or be written
		// straight into the DB. Skipped, NOT retried in UTC — delivering at a guessed
		// hour looks exactly like delivering correctly.
		if err := ValidateScheduledMessageTimezone(sm.Timezone); err != nil {
			schedLog("skip %s: %v — no fallback zone is applied", describeSchedule(sm), err)
			continue
		}
		slot, ok := mostRecentSlot(sm, at)
		if !ok {
			continue
		}
		key := slotKey(slot)
		if !slotIsAfterCursor(slot, sm.LastFiredSlot) {
			continue
		}
		if err := s.deliverScheduledMessage(sm, key, now); err != nil {
			schedLog("skip %s: delivery failed: %v", describeSchedule(sm), err)
			continue
		}
		if err := s.dal.MarkScheduledMessageFired(sm.ID, key, now); err != nil {
			schedLog("delivered %s slot %s but the cursor did NOT advance (%v) — the next tick will resend", describeSchedule(sm), key, err)
		}
	}
}

// resolveChatRecipient, NOT resolveMember: a scheduled message takes chat's
// recipient rule, which also reaches `ow-` outsource workers. A gone recipient
// fails the row WITHOUT advancing its cursor — the cursor means "delivered".
func (s *apiServer) deliverScheduledMessage(sm ScheduledMessage, slot string, now float64) error {
	recipient, err := s.resolveChatRecipient(sm.MemberID)
	if err != nil {
		return err
	}
	msg := ChatMessage{
		ID:        "c-" + newHexID(12),
		Sender:    "sched:" + sm.ID,
		Recipient: recipient,
		Body:      sm.Body,
		TS:        now,
		Meta: map[string]any{
			"scheduled": map[string]any{
				"schedule_id": sm.ID,
				"label":       sm.Label,
				"slot":        slot,
			},
		},
	}
	if err := s.dal.PutChat(msg); err != nil {
		return err
	}
	s.hub.Publish("chat", "patch", "chat", wireOwnerID+"::"+msg.ID,
		map[string]any{"id": msg.ID, "from": msg.Sender, "to": msg.Recipient},
		audienceMembers(msg.Sender, msg.Recipient), triggerServer)
	return nil
}
