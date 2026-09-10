package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSchedLog(t *testing.T) {
	t.Run("formats operator output with a prefix and newline", func(t *testing.T) {
		got := hubTestStderr(t, func() { schedLog("tick %s", "started") })
		want := "[scheduled] tick started\n"
		if got != want {
			t.Fatalf("stderr = %q, want %q", got, want)
		}
	})

	t.Run("writes one complete line for each message", func(t *testing.T) {
		got := hubTestStderr(t, func() {
			schedLog("tick: 0 schedule(s)")
			schedLog("tick failed: %v", "database offline")
		})
		want := "[scheduled] tick: 0 schedule(s)\n[scheduled] tick failed: database offline\n"
		if got != want {
			t.Fatalf("stderr = %q, want %q", got, want)
		}
	})
}

func TestStartScheduledMessageCadence(t *testing.T) {
	const helperEnv = "OFFICRAFT_SCHEDULED_MESSAGE_CADENCE_HELPER"
	if os.Getenv(helperEnv) == "1" {
		got := hubTestStderr(t, func() {
			(&apiServer{}).startScheduledMessageCadence(time.Hour)
		})
		want := "[scheduled] cadence started (period=3600s)\n"
		if got != want {
			t.Fatalf("stderr = %q, want %q", got, want)
		}
		return
	}

	t.Run("reports the configured period without waiting for a tick", func(t *testing.T) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestStartScheduledMessageCadence$")
		cmd.Env = append(os.Environ(), helperEnv+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cadence helper: %v\n%s", err, output)
		}
	})
}

func TestRunScheduledMessageTick(t *testing.T) {
	t.Run("delivers one due slot and does not repeat it", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "mira")
		const tickNow = 1788746400.0
		const wantSlot = "2026-09-07T09:00+08:00"
		if err := d.PutScheduledMessage(ScheduledMessage{
			ID: "sch-morning", MemberID: "mira", Label: "morning", Body: "good morning",
			Cadence: ScheduledMessageCadenceDaily, Hour: 9, Minute: 0, Timezone: "Asia/Taipei",
			Status: ScheduledMessageStatusEnabled, LastFiredSlot: "2026-09-06T09:00+08:00",
			CreatedTS: 1788740000,
		}); err != nil {
			t.Fatalf("PutScheduledMessage: %v", err)
		}

		api.runScheduledMessageTick(tickNow)
		frame := scheduledMessageFrame("sched:sch-morning", "mira")
		dashboard.wantFrames(frame)
		recipient.wantFrames(frame)

		messages, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(messages) != 1 {
			t.Fatalf("chat messages = %d, want 1", len(messages))
		}
		message := messages[0]
		if message.Sender != "sched:sch-morning" || message.Recipient != "mira" ||
			message.Body != "good morning" || message.TS != tickNow {
			t.Fatalf("delivered message = %+v", message)
		}
		scheduled, ok := message.Meta["scheduled"].(map[string]any)
		if !ok {
			t.Fatalf("scheduled metadata = %#v, want an object", message.Meta["scheduled"])
		}
		if scheduled["schedule_id"] != "sch-morning" || scheduled["label"] != "morning" ||
			scheduled["slot"] != wantSlot {
			t.Fatalf("scheduled metadata = %#v", scheduled)
		}

		stored, err := d.GetScheduledMessage("sch-morning")
		if err != nil || stored == nil {
			t.Fatalf("GetScheduledMessage: %v (%+v)", err, stored)
		}
		if stored.LastFiredSlot != wantSlot || stored.LastFiredTS != tickNow {
			t.Fatalf("delivery cursor = (%q, %v), want (%q, %v)",
				stored.LastFiredSlot, stored.LastFiredTS, wantSlot, tickNow)
		}

		api.runScheduledMessageTick(tickNow)
		dashboard.wantFrames()
		recipient.wantFrames()
		messages, err = d.ListChat()
		if err != nil {
			t.Fatalf("ListChat after repeated tick: %v", err)
		}
		if len(messages) != 1 {
			t.Fatalf("chat messages after repeated tick = %d, want 1", len(messages))
		}
	})

	t.Run("continues with another schedule after a recipient is missing", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "mira")
		const tickNow = 1788746400.0
		const wantSlot = "2026-09-07T09:00+08:00"
		for _, schedule := range []ScheduledMessage{
			{
				ID: "sch-missing", MemberID: "missing", Label: "missing", Body: "not delivered",
				Cadence: ScheduledMessageCadenceDaily, Hour: 9, Minute: 0, Timezone: "Asia/Taipei",
				Status: ScheduledMessageStatusEnabled, LastFiredSlot: "2026-09-06T09:00+08:00",
				LastFiredTS: 1700000000, CreatedTS: 1,
			},
			{
				ID: "sch-good", MemberID: "mira", Label: "good", Body: "delivered",
				Cadence: ScheduledMessageCadenceDaily, Hour: 9, Minute: 0, Timezone: "Asia/Taipei",
				Status: ScheduledMessageStatusEnabled, LastFiredSlot: "2026-09-06T09:00+08:00",
				CreatedTS: 2,
			},
		} {
			if err := d.PutScheduledMessage(schedule); err != nil {
				t.Fatalf("PutScheduledMessage(%q): %v", schedule.ID, err)
			}
		}

		api.runScheduledMessageTick(tickNow)
		frame := scheduledMessageFrame("sched:sch-good", "mira")
		dashboard.wantFrames(frame)
		recipient.wantFrames(frame)

		missing, err := d.GetScheduledMessage("sch-missing")
		if err != nil || missing == nil {
			t.Fatalf("GetScheduledMessage(missing): %v (%+v)", err, missing)
		}
		if missing.LastFiredSlot != "2026-09-06T09:00+08:00" || missing.LastFiredTS != 1700000000 {
			t.Fatalf("missing recipient cursor = (%q, %v), want unchanged", missing.LastFiredSlot, missing.LastFiredTS)
		}
		good, err := d.GetScheduledMessage("sch-good")
		if err != nil || good == nil {
			t.Fatalf("GetScheduledMessage(good): %v (%+v)", err, good)
		}
		if good.LastFiredSlot != wantSlot || good.LastFiredTS != tickNow {
			t.Fatalf("successful recipient cursor = (%q, %v), want (%q, %v)",
				good.LastFiredSlot, good.LastFiredTS, wantSlot, tickNow)
		}
	})

	t.Run("does not deliver a schedule with a host-relative timezone", func(t *testing.T) {
		api, _, d, _ := newAPITestServer(t)
		const tickNow = 1788746400.0
		if err := d.PutScheduledMessage(ScheduledMessage{
			ID: "sch-local", MemberID: "mira", Label: "local", Body: "must not send",
			Cadence: ScheduledMessageCadenceDaily, Hour: 9, Minute: 0, Timezone: "Local",
			Status: ScheduledMessageStatusEnabled, LastFiredSlot: "2026-09-06T09:00+08:00",
			CreatedTS: 1,
		}); err != nil {
			t.Fatalf("PutScheduledMessage: %v", err)
		}

		out := hubTestStderr(t, func() { api.runScheduledMessageTick(tickNow) })
		if !strings.Contains(out, "no fallback zone is applied") {
			t.Fatalf("invalid-timezone log = %q", out)
		}
		messages, err := d.ListChat()
		if err != nil {
			t.Fatalf("ListChat: %v", err)
		}
		if len(messages) != 0 {
			t.Fatalf("chat messages = %d, want 0", len(messages))
		}
		stored, err := d.GetScheduledMessage("sch-local")
		if err != nil || stored == nil {
			t.Fatalf("GetScheduledMessage: %v (%+v)", err, stored)
		}
		if stored.LastFiredSlot != "2026-09-06T09:00+08:00" || stored.LastFiredTS != 0 {
			t.Fatalf("invalid-timezone cursor = (%q, %v), want unchanged", stored.LastFiredSlot, stored.LastFiredTS)
		}
	})
}

func scheduledMessageFrame(sender, recipient string) map[string]any {
	return map[string]any{
		"seq":   1,
		"topic": "chat",
		"op":    "patch",
		"data": map[string]any{
			"entity":  "chat",
			"key":     apiAnyString,
			"epoch":   1,
			"deleted": false,
			"payload": map[string]any{
				"id":   apiAnyString,
				"from": sender,
				"to":   recipient,
			},
		},
		"ts":      apiAnyNumber,
		"trigger": "server",
	}
}

func TestDeliverScheduledMessage(t *testing.T) {
	t.Run("a delivery writes the message under the schedule's own sender and fans the chat delta to the member and the owner", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		recipient := apiTestListen(t, api, "kip")
		bystander := apiTestListen(t, api, "mira")

		if err := api.deliverScheduledMessage(ScheduledMessage{
			ID: "sm-1", MemberID: "kip", Label: "standup", Body: "daily standup",
		}, "2026-09-07T09:00", 1700000000); err != nil {
			t.Fatalf("deliverScheduledMessage: %v", err)
		}
		frame := map[string]any{
			"seq":   1,
			"topic": "chat",
			"op":    "patch",
			"data": map[string]any{
				"entity":  "chat",
				"key":     apiAnyString,
				"epoch":   1,
				"deleted": false,
				"payload": map[string]any{
					"id":   apiAnyString,
					"from": "sched:sm-1",
					"to":   "kip",
				},
			},
			"ts":      apiAnyNumber,
			"trigger": "server",
		}
		dashboard.wantFrames(frame)
		recipient.wantFrames(frame)
		bystander.wantFrames()

		rec := apiRequest(t, h, "GET", "/api/chat?with=kip&limit=5", owner, "")
		if rec.Code != 200 {
			t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
		}
		var chat any
		if err := json.Unmarshal(rec.Body.Bytes(), &chat); err != nil {
			t.Fatalf("non-JSON body: %s", rec.Body.String())
		}
		apiWantValue(t, "chat", chat, map[string]any{
			"messages": []any{map[string]any{
				"id":                 apiAnyString,
				"from":               "sched:sm-1",
				"from_name":          "",
				"to":                 "kip",
				"to_name":            "",
				"body":               "daily standup",
				"body_omitted_chars": 0,
				"ts":                 1700000000,
				"ts_display":         "",
				"meta": map[string]any{
					"scheduled": map[string]any{
						"schedule_id": "sm-1",
						"label":       "standup",
						"slot":        "2026-09-07T09:00",
					},
				},
				"reply_card_status": "",
				"attachments":       []any{},
				"reply_to":          "",
			}},
		})
	})

	t.Run("a schedule whose member is gone answers not found and fans nothing", func(t *testing.T) {
		api, _, _, _ := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		err := api.deliverScheduledMessage(ScheduledMessage{
			ID: "sm-1", MemberID: "nope", Label: "standup", Body: "daily standup",
		}, "2026-09-07T09:00", 1700000000)
		if err != errNotFound {
			t.Fatalf("want errNotFound, got %v", err)
		}
		dashboard.wantFrames()
	})
}
