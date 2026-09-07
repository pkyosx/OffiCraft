// Skeleton generated from server/ocserverd/scheduled_message.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"encoding/json"
	"testing"
)

func TestSchedLog(t *testing.T) {
	t.Skip("TODO: 需要人工判斷這個函式的可觀察結果是什麼")
}

func TestStartScheduledMessageCadence(t *testing.T) {
	t.Skip("TODO: startScheduledMessageCadence mounts the always-on tick loop.")
}

func TestRunScheduledMessageTick(t *testing.T) {
	t.Skip("TODO: runScheduledMessageTick is ONE pass over every armed schedule — the unit the tests drive directly.")
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
