// Skeleton generated from server/ocserverd/api_scheduled_messages.go by gen_test_skeletons.py.
// Every case is a t.Skip placeholder: fill the body, keep or rewrite the name.

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// apiScheduledStandupRow is the whole schedule the `standup` create below
// lands, as the listing serves it. last_fired_slot names the wall-clock slot
// current when the row was created, so only its READING can be written down —
// apiWantSlotReading pins that half.
func apiScheduledStandupRow(id string) map[string]any {
	return map[string]any{
		"id":              id,
		"member_id":       "kip",
		"label":           "AM",
		"body":            "standup",
		"cadence":         "daily",
		"day_of_week":     0,
		"day_of_month":    1,
		"hour":            9,
		"minute":          30,
		"custom_months":   []any{},
		"custom_days":     []any{},
		"custom_hours":    []any{},
		"custom_minutes":  []any{},
		"timezone":        "Asia/Taipei",
		"status":          "enabled",
		"last_fired_slot": apiAnyString,
		"last_fired_ts":   0,
		"created_ts":      apiAnyNumber,
	}
}

// apiTestStandup creates that schedule and answers its minted id.
func apiTestStandup(t *testing.T, h http.Handler, caller, memberID string) string {
	t.Helper()
	status, data := apiJSON(t, h, "POST", "/api/members/"+memberID+"/scheduled-messages", caller,
		`{"label":"AM","body":"standup","cadence":"daily","timezone":"Asia/Taipei","hour":9,"minute":30}`)
	if status != 200 {
		t.Fatalf("create scheduled message: %d %v", status, data)
	}
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatalf("create must mint an id: %v", data)
	}
	return id
}

func apiWantScheduledList(t *testing.T, h http.Handler, caller, memberID string, want ...map[string]any) {
	t.Helper()
	apiWantJSONArray(t, h, "/api/members/"+memberID+"/scheduled-messages", caller, "scheduled-messages", want...)
}

// apiWantSlotReading pins the half of the delivery cursor that can be written
// down: the wall-clock reading and offset it names. The DATE is whatever day
// the test runs on, so it is not written down and not asserted.
func apiWantSlotReading(t *testing.T, data map[string]any, reading string) {
	t.Helper()
	slot, _ := data["last_fired_slot"].(string)
	if !strings.HasSuffix(slot, reading) {
		t.Fatalf("last_fired_slot: want a slot reading %s, got %q", reading, slot)
	}
}

func TestHandleListScheduledMessagesApiMembersMemberIdScheduledMessagesGet(t *testing.T) {
	t.Run("a member nobody has scheduled anything for answers an empty list", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		apiWantScheduledList(t, h, owner, "kip")
		dashboard.wantFrames()
	})

	t.Run("a member's schedules come back oldest first, each carrying its whole shape", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		daily := apiTestStandup(t, h, owner, "kip")
		_, created := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"sweep","cadence":"custom","timezone":"Asia/Taipei","custom_days":[1],"custom_hours":[9],"custom_minutes":[0]}`)
		custom, _ := created["id"].(string)
		dashboard := apiTestListen(t, api, "")

		apiWantScheduledList(t, h, owner, "kip", apiScheduledStandupRow(daily), map[string]any{
			"id":              custom,
			"member_id":       "kip",
			"label":           "",
			"body":            "sweep",
			"cadence":         "custom",
			"day_of_week":     0,
			"day_of_month":    1,
			"hour":            0,
			"minute":          0,
			"custom_months":   []any{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
			"custom_days":     []any{1},
			"custom_hours":    []any{9},
			"custom_minutes":  []any{0},
			"timezone":        "Asia/Taipei",
			"status":          "enabled",
			"last_fired_slot": apiAnyString,
			"last_fired_ts":   0,
			"created_ts":      apiAnyNumber,
		})
		dashboard.wantFrames()
	})

	t.Run("one member's schedules never appear under another member", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		apiTestStandup(t, h, owner, "kip")

		apiWantScheduledList(t, h, owner, "mira")
	})

	t.Run("a member this server does not have answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/members/ghost/scheduled-messages", owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ghost' not found")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, _ := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")

		status, data := apiJSON(t, h, "GET", "/api/members/kip/scheduled-messages", agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, _ := newAPITestServer(t)

		status, data := apiJSON(t, h, "GET", "/api/members/kip/scheduled-messages", "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
	})
}

func TestHandleCreateScheduledMessageApiMembersMemberIdScheduledMessagesPost(t *testing.T) {
	t.Run("a create answers the bounded receipt, lands the whole schedule and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"label":"AM","body":"standup","cadence":"daily","timezone":"Asia/Taipei","hour":9,"minute":30}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":              apiAnyString,
			"member_id":       "kip",
			"label":           "AM",
			"body_size_chars": 7,
			"cadence":         "daily",
			"custom_months":   []any{},
			"day_of_month":    1,
			"day_of_week":     0,
			"status":          "enabled",
			"last_fired_slot": apiAnyString,
			"last_fired_ts":   0,
			"created_ts":      apiAnyNumber,
		})
		apiWantSlotReading(t, data, "T09:30+08:00")
		id, _ := data["id"].(string)
		apiWantScheduledList(t, h, owner, "kip", apiScheduledStandupRow(id))
		dashboard.wantFrames()
		bystander.wantFrames()
	})

	t.Run("an assistant is as legal a recipient as any other member", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		id := apiTestStandup(t, h, owner, "mira")
		want := apiScheduledStandupRow(id)
		want["member_id"] = "mira"
		apiWantScheduledList(t, h, owner, "mira", want)
	})

	t.Run("a custom schedule that names no months is stored as all twelve", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"sweep","cadence":"custom","timezone":"Asia/Taipei","custom_days":[1],"custom_hours":[9],"custom_minutes":[0]}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":              apiAnyString,
			"member_id":       "kip",
			"label":           "",
			"body_size_chars": 5,
			"cadence":         "custom",
			"custom_months":   []any{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
			"day_of_month":    1,
			"day_of_week":     0,
			"status":          "enabled",
			"last_fired_slot": apiAnyString,
			"last_fired_ts":   0,
			"created_ts":      apiAnyNumber,
		})
		apiWantSlotReading(t, data, "T09:00+08:00")
	})

	t.Run("a custom schedule sending an empty months set answers 422 and creates nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"sweep","cadence":"custom","timezone":"Asia/Taipei","custom_months":[],"custom_days":[1],"custom_hours":[9],"custom_minutes":[0]}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"custom_months cannot be empty when cadence is 'custom'; list every value that should fire (an empty set would be read as either 'always' or 'never', and those must not be one keystroke apart) (to mean every month, OMIT the field entirely rather than sending [])")
		apiWantScheduledList(t, h, owner, "kip")
		dashboard.wantFrames()
	})

	t.Run("a custom schedule naming no sets at all answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"sweep","cadence":"custom","timezone":"Asia/Taipei"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"custom_days cannot be empty when cadence is 'custom'; list every value that should fire (an empty set would be read as either 'always' or 'never', and those must not be one keystroke apart)")
		apiWantScheduledList(t, h, owner, "kip")
	})

	t.Run("a daily schedule that omits the hour answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"standup","cadence":"daily","timezone":"Asia/Taipei"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"hour is required when cadence is 'daily'; only 'custom' reads the custom_hours set instead, and an omitted hour must never be taken to mean midnight")
		apiWantScheduledList(t, h, owner, "kip")
	})

	t.Run("an hour outside the clock answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"standup","cadence":"daily","timezone":"Asia/Taipei","hour":99,"minute":30}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "hour must be between 0 and 23; got 99")
		apiWantScheduledList(t, h, owner, "kip")
	})

	t.Run("a blank message answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"   ","cadence":"daily","timezone":"Asia/Taipei","hour":9,"minute":30}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "body cannot be blank")
		apiWantScheduledList(t, h, owner, "kip")
	})

	t.Run("a body naming no message answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"cadence":"daily","timezone":"Asia/Taipei","hour":9,"minute":30}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "field required: body")
		apiWantScheduledList(t, h, owner, "kip")
	})

	t.Run("a cadence outside the closed set answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"standup","cadence":"hourly","timezone":"Asia/Taipei","hour":9,"minute":30}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"cadence must be one of ['daily' 'weekly' 'monthly' 'custom']; got 'hourly'")
		apiWantScheduledList(t, h, owner, "kip")
	})

	t.Run("a timezone that names the server's own zone answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"standup","cadence":"daily","timezone":"Local","hour":9,"minute":30}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"timezone 'Local' means the zone the SERVER happens to be in, which would move every schedule when the server moves; state the schedule's own IANA timezone name (e.g. 'Asia/Taipei', or 'UTC' if that is genuinely what is meant)")
		apiWantScheduledList(t, h, owner, "kip")
	})

	t.Run("a timezone the tz database does not know answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"standup","cadence":"daily","timezone":"Mars/Olympus","hour":9,"minute":30}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "timezone 'Mars/Olympus' is not a known IANA timezone name")
		apiWantScheduledList(t, h, owner, "kip")
	})

	t.Run("a body carrying a key this route does not declare answers 422 and creates nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"standup","cadence":"daily","timezone":"Asia/Taipei","hour":9,"minute":30,"note":"x"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: json: unknown field \"note\"")
		apiWantScheduledList(t, h, owner, "kip")
	})

	t.Run("a member this server does not have answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/ghost/scheduled-messages", owner,
			`{"body":"standup","cadence":"daily","timezone":"Asia/Taipei","hour":9,"minute":30}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "member 'ghost' not found")
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", agent,
			`{"body":"standup","cadence":"daily","timezone":"Asia/Taipei","hour":9,"minute":30}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiWantScheduledList(t, h, owner, "kip")
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", "",
			`{"body":"standup","cadence":"daily","timezone":"Asia/Taipei","hour":9,"minute":30}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiWantScheduledList(t, h, owner, "kip")
	})
}

func TestHandleUpdateScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdPatch(t *testing.T) {
	t.Run("an edit that touches no timing answers the receipt with the cursor where it stood and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/scheduled-messages/"+id, owner, `{"label":"PM"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":              id,
			"member_id":       "kip",
			"label":           "PM",
			"body_size_chars": 7,
			"cadence":         "daily",
			"custom_months":   []any{},
			"day_of_month":    1,
			"day_of_week":     0,
			"status":          "enabled",
			"last_fired_slot": apiAnyString,
			"last_fired_ts":   0,
			"created_ts":      apiAnyNumber,
		})
		apiWantSlotReading(t, data, "T09:30+08:00")
		want := apiScheduledStandupRow(id)
		want["label"] = "PM"
		apiWantScheduledList(t, h, owner, "kip", want)
		dashboard.wantFrames()
		bystander.wantFrames()
	})

	t.Run("moving the hour re-aims the cursor onto the slot the new reading names", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/scheduled-messages/"+id, owner, `{"hour":8}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantSlotReading(t, data, "T08:30+08:00")
		want := apiScheduledStandupRow(id)
		want["hour"] = 8
		apiWantScheduledList(t, h, owner, "kip", want)
	})

	t.Run("disabling suspends the schedule and leaves everything else standing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/scheduled-messages/"+id, owner, `{"status":"disabled"}`)
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id":              id,
			"member_id":       "kip",
			"label":           "AM",
			"body_size_chars": 7,
			"cadence":         "daily",
			"custom_months":   []any{},
			"day_of_month":    1,
			"day_of_week":     0,
			"status":          "disabled",
			"last_fired_slot": apiAnyString,
			"last_fired_ts":   0,
			"created_ts":      apiAnyNumber,
		})
		apiWantSlotReading(t, data, "T09:30+08:00")
		want := apiScheduledStandupRow(id)
		want["status"] = "disabled"
		apiWantScheduledList(t, h, owner, "kip", want)
	})

	t.Run("a status outside the closed set answers 422 and changes nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/scheduled-messages/"+id, owner, `{"status":"paused"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "status must be one of ['enabled' 'disabled']; got 'paused'")
		apiWantScheduledList(t, h, owner, "kip", apiScheduledStandupRow(id))
		dashboard.wantFrames()
	})

	t.Run("switching to custom without the sets it reads answers 422 and changes nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/scheduled-messages/"+id, owner, `{"cadence":"custom"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"custom_days cannot be empty when cadence is 'custom'; list every value that should fire (an empty set would be read as either 'always' or 'never', and those must not be one keystroke apart)")
		apiWantScheduledList(t, h, owner, "kip", apiScheduledStandupRow(id))
	})

	t.Run("a timezone that names the server's own zone answers 422 and changes nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/scheduled-messages/"+id, owner, `{"timezone":"Local"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error",
			"timezone 'Local' means the zone the SERVER happens to be in, which would move every schedule when the server moves; state the schedule's own IANA timezone name (e.g. 'Asia/Taipei', or 'UTC' if that is genuinely what is meant)")
		apiWantScheduledList(t, h, owner, "kip", apiScheduledStandupRow(id))
	})

	t.Run("a body carrying a key this route does not declare answers 422 and changes nothing", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/scheduled-messages/"+id, owner, `{"member_id":"mira"}`)
		if status != 422 {
			t.Fatalf("want 422, got %d (%v)", status, data)
		}
		apiWantError(t, data, "validation_error", "invalid request body: json: unknown field \"member_id\"")
		apiWantScheduledList(t, h, owner, "kip", apiScheduledStandupRow(id))
	})

	t.Run("a schedule id this server does not carry answers 404", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/scheduled-messages/sch-000000000000", owner, `{"label":"PM"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "scheduled message 'sch-000000000000' not found")
	})

	t.Run("another member's schedule answers 404 and leaves that schedule alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "mira")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/scheduled-messages/"+id, owner, `{"label":"PM"}`)
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "scheduled message '"+id+"' not found")
		want := apiScheduledStandupRow(id)
		want["member_id"] = "mira"
		apiWantScheduledList(t, h, owner, "mira", want)
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/scheduled-messages/"+id, agent, `{"label":"PM"}`)
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiWantScheduledList(t, h, owner, "kip", apiScheduledStandupRow(id))
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")

		status, data := apiJSON(t, h, "PATCH", "/api/members/kip/scheduled-messages/"+id, "", `{"label":"PM"}`)
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiWantScheduledList(t, h, owner, "kip", apiScheduledStandupRow(id))
	})
}

func TestHandleDeleteScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdDelete(t *testing.T) {
	t.Run("a delete answers the receipt, empties the listing and fans nothing", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")
		dashboard := apiTestListen(t, api, "")
		bystander := apiTestListen(t, api, "kip")

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip/scheduled-messages/"+id, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": id, "member_id": "kip", "deleted": true})
		apiWantScheduledList(t, h, owner, "kip")
		dashboard.wantFrames()
		bystander.wantFrames()
	})

	t.Run("deleting the same schedule twice answers 404 the second time", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")
		apiJSON(t, h, "DELETE", "/api/members/kip/scheduled-messages/"+id, owner, "")

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip/scheduled-messages/"+id, owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "scheduled message '"+id+"' not found")
	})

	t.Run("only the addressed schedule is removed", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		kept := apiTestStandup(t, h, owner, "kip")
		_, created := apiJSON(t, h, "POST", "/api/members/kip/scheduled-messages", owner,
			`{"body":"sweep","cadence":"weekly","timezone":"Asia/Taipei","hour":1,"minute":2,"day_of_week":5}`)
		gone, _ := created["id"].(string)

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip/scheduled-messages/"+gone, owner, "")
		if status != 200 {
			t.Fatalf("want 200, got %d (%v)", status, data)
		}
		apiWantBody(t, data, map[string]any{"id": gone, "member_id": "kip", "deleted": true})
		apiWantScheduledList(t, h, owner, "kip", apiScheduledStandupRow(kept))
	})

	t.Run("another member's schedule answers 404 and leaves that schedule alone", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "mira")

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip/scheduled-messages/"+id, owner, "")
		if status != 404 {
			t.Fatalf("want 404, got %d (%v)", status, data)
		}
		apiWantError(t, data, "not_found", "scheduled message '"+id+"' not found")
		want := apiScheduledStandupRow(id)
		want["member_id"] = "mira"
		apiWantScheduledList(t, h, owner, "mira", want)
	})

	t.Run("an authenticated agent identity answers 403 because this row requires admin_agent", func(t *testing.T) {
		api, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")
		agent := apiTestAgentToken(t, api, "kip", "")
		dashboard := apiTestListen(t, api, "")

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip/scheduled-messages/"+id, agent, "")
		if status != 403 {
			t.Fatalf("want 403, got %d (%v)", status, data)
		}
		apiWantError(t, data, "forbidden", "principal not permitted")
		apiWantScheduledList(t, h, owner, "kip", apiScheduledStandupRow(id))
		dashboard.wantFrames()
	})

	t.Run("a request without a token answers 401", func(t *testing.T) {
		_, h, _, owner := newAPITestServer(t)
		id := apiTestStandup(t, h, owner, "kip")

		status, data := apiJSON(t, h, "DELETE", "/api/members/kip/scheduled-messages/"+id, "", "")
		if status != 401 {
			t.Fatalf("want 401, got %d (%v)", status, data)
		}
		apiWantError(t, data, "unauthorized", "missing credentials")
		apiWantScheduledList(t, h, owner, "kip", apiScheduledStandupRow(id))
	})
}

func TestResolveCustomMonths(t *testing.T) {
	sent := []int{2, 8}
	empty := []int{}
	for _, tt := range []struct {
		name    string
		cadence string
		stored  []int
		sent    *[]int
		want    []int
	}{
		{name: "sent values are used verbatim", cadence: ScheduledMessageCadenceCustom, stored: []int{1}, sent: &sent, want: []int{2, 8}},
		{name: "sent empty values stay empty", cadence: ScheduledMessageCadenceCustom, stored: []int{1}, sent: &empty, want: []int{}},
		{name: "non custom keeps stored values", cadence: ScheduledMessageCadenceDaily, stored: []int{4, 9}, want: []int{4, 9}},
		{name: "custom keeps an existing set", cadence: ScheduledMessageCadenceCustom, stored: []int{3, 7}, want: []int{3, 7}},
		{name: "new custom defaults to every month", cadence: ScheduledMessageCadenceCustom, want: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCustomMonths(tt.cadence, tt.stored, tt.sent)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("resolveCustomMonths(%q, %#v, %#v) = %#v, want %#v", tt.cadence, tt.stored, tt.sent, got, tt.want)
			}
		})
	}
}

func TestResolveScheduledMessageReturnsOnlyTheAddressedMembersSchedule(t *testing.T) {
	api, h, _, owner := newAPITestServer(t)
	id := apiTestStandup(t, h, owner, "kip")

	got, err := api.resolveScheduledMessage("kip", id)
	if err != nil {
		t.Fatalf("resolveScheduledMessage() error = %v", err)
	}
	if got == nil || got.ID != id || got.MemberID != "kip" {
		t.Fatalf("resolveScheduledMessage() = %#v, want schedule %q for kip", got, id)
	}

	for _, tt := range []struct {
		name       string
		memberID   string
		scheduleID string
	}{
		{name: "unknown member", memberID: "ghost", scheduleID: id},
		{name: "unknown schedule", memberID: "kip", scheduleID: "missing"},
		{name: "schedule owned by another member", memberID: "mira", scheduleID: id},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := api.resolveScheduledMessage(tt.memberID, tt.scheduleID)
			if got != nil || !errors.Is(err, errNotFound) {
				t.Fatalf("resolveScheduledMessage(%q, %q) = %#v, %v; want nil, errNotFound", tt.memberID, tt.scheduleID, got, err)
			}
		})
	}
}

func TestValidateScheduledMessageWritesTheFirstInvalidInvariant(t *testing.T) {
	api, _, _, _ := newAPITestServer(t)
	valid := ScheduledMessage{
		Body: "standup", Cadence: ScheduledMessageCadenceDaily,
		DayOfWeek: 0, DayOfMonth: 1, Hour: 9, Minute: 30,
		Timezone: "Asia/Taipei",
	}

	t.Run("valid schedule passes without writing a response", func(t *testing.T) {
		rec := httptest.NewRecorder()
		if !api.validateScheduledMessage(rec, valid) {
			t.Fatal("validateScheduledMessage() rejected a valid schedule")
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("valid schedule wrote %q", rec.Body.String())
		}
	})

	for _, tt := range []struct {
		name    string
		mutate  func(*ScheduledMessage)
		message string
	}{
		{
			name:    "invalid cadence",
			mutate:  func(m *ScheduledMessage) { m.Cadence = "hourly" },
			message: "cadence must be one of ['daily' 'weekly' 'monthly' 'custom']; got 'hourly'",
		},
		{
			name:    "blank body",
			mutate:  func(m *ScheduledMessage) { m.Body = " \t" },
			message: "body cannot be blank",
		},
		{
			name:    "invalid wall clock",
			mutate:  func(m *ScheduledMessage) { m.Hour = 24 },
			message: "hour must be between 0 and 23; got 24",
		},
		{
			name: "empty custom set",
			mutate: func(m *ScheduledMessage) {
				m.Cadence = ScheduledMessageCadenceCustom
				m.CustomMonths = []int{}
				m.CustomDays = []int{1}
				m.CustomHours = []int{9}
				m.CustomMinutes = []int{0}
			},
			message: "custom_months cannot be empty when cadence is 'custom'; list every value that should fire (an empty set would be read as either 'always' or 'never', and those must not be one keystroke apart) (to mean every month, OMIT the field entirely rather than sending [])",
		},
		{
			name:    "unknown timezone",
			mutate:  func(m *ScheduledMessage) { m.Timezone = "Mars/Olympus" },
			message: "timezone 'Mars/Olympus' is not a known IANA timezone name",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			candidate := valid
			tt.mutate(&candidate)
			rec := httptest.NewRecorder()
			if api.validateScheduledMessage(rec, candidate) {
				t.Fatal("validateScheduledMessage() accepted an invalid schedule")
			}
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body.String())
			}
			apiWantError(t, apiHelpersWritten(t, rec), "validation_error", tt.message)
		})
	}
}
