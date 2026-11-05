package main

import (
	"fmt"
	"testing"
)

func TestOwnerSnoozesATaskLessWaitingCard(t *testing.T) {
	api, h, d, owner := newAPITestServer(t)
	executor, steps := apiTestTwoStepTask(t, api, h, owner)
	agent := apiTestAgentToken(t, api, "mira", "")
	plain := apiTestOpenReplyCard(t, h, agent,
		`{"kind":"decision","summary":"plain","options":[{"text":"x"}],"linked_task":null}`)
	other := apiTestOpenReplyCard(t, h, agent,
		`{"kind":"decision","summary":"other","options":[{"text":"x"}],"linked_task":null}`)
	about := apiTestOpenReplyCard(t, h, agent,
		`{"kind":"decision","summary":"about","options":[{"text":"x"}],"linked_task":null,"about_task_id":"T-1"}`)
	bound := apiTestOpenReplyCard(t, h, executor,
		`{"kind":"decision","summary":"bound","options":[{"text":"x"}],`+
			`"linked_task":{"task_id":"T-1","step_id":"`+steps[0]+`"}}`)
	future := nowSecs() + 3600

	snooze := func(token, id string, until any) (int, map[string]any) {
		t.Helper()
		b := `{"snoozed_until_ts":null}`
		if until != nil {
			b = fmt.Sprintf(`{"snoozed_until_ts":%v}`, until)
		}
		return apiJSON(t, h, "PUT", "/api/reply-cards/"+id+"/snooze", token, b)
	}
	waitingCount := func() float64 {
		t.Helper()
		status, data := apiJSON(t, h, "GET", "/api/reply-cards/count", owner, "")
		if status != 200 {
			t.Fatalf("count: %d %v", status, data)
		}
		return data["waiting"].(float64)
	}
	snoozedOf := func() map[string]any {
		t.Helper()
		out := map[string]any{}
		for _, row := range apiTestReplyCardPane(t, h, owner, "") {
			r := row.(map[string]any)
			out[r["summary"].(string)] = r["snoozed_until_ts"]
		}
		return out
	}

	t.Run("a snoozed card stays in the waiting pane, flagged, and leaves the badge count", func(t *testing.T) {
		if status, data := snooze(owner, plain, future); status != 200 || data["snoozed_until_ts"] != future || data["id"] != plain {
			t.Fatalf("snooze: %d %v", status, data)
		}
		got := snoozedOf()
		if len(got) != 4 || got["plain"] != future || got["other"] != nil {
			t.Fatalf("waiting pane snoozed_until_ts = %v", got)
		}
		if n := waitingCount(); n != 3 {
			t.Fatalf("waiting count = %v, want 3", n)
		}
	})
	t.Run("it comes back by itself once the time has passed", func(t *testing.T) {
		card, _ := d.GetReplyCard(plain)
		card.SnoozedUntilTS = nowSecs() - 1
		if err := d.PutReplyCard(*card); err != nil {
			t.Fatal(err)
		}
		if got := snoozedOf()["plain"]; got != nil {
			t.Fatalf("a passed snooze still shows: %v", got)
		}
		if n := waitingCount(); n != 4 {
			t.Fatalf("waiting count = %v, want 4", n)
		}
	})
	t.Run("null unsnoozes", func(t *testing.T) {
		if status, data := snooze(owner, other, future); status != 200 {
			t.Fatalf("snooze: %d %v", status, data)
		}
		if status, data := snooze(owner, other, nil); status != 200 || data["snoozed_until_ts"] != nil {
			t.Fatalf("unsnooze: %d %v", status, data)
		}
		if got := snoozedOf()["other"]; got != nil || waitingCount() != 4 {
			t.Fatalf("unsnoozed card still hidden: %v", got)
		}
	})
	t.Run("a time that is not in the future is refused", func(t *testing.T) {
		if status, data := snooze(owner, plain, nowSecs()-60); status != 400 {
			t.Fatalf("want 400, got %d %v", status, data)
		}
		if status, data := apiJSON(t, h, "PUT", "/api/reply-cards/"+plain+"/snooze", owner, `{}`); status != 422 {
			t.Fatalf("missing field: want 422, got %d %v", status, data)
		}
	})
	t.Run("a card naming a task is never snoozed", func(t *testing.T) {
		for _, id := range []string{about, bound} {
			if status, data := snooze(owner, id, future); status != 409 {
				t.Fatalf("%s: want 409, got %d %v", id, status, data)
			}
		}
	})
	t.Run("nobody but the owner may snooze", func(t *testing.T) {
		admin := apiTestPrincipalToken(t, api, d, principalAdminAgent, "admin-1")
		for _, tok := range []string{agent, admin} {
			if status, data := snooze(tok, plain, future); status != 403 {
				t.Fatalf("want 403, got %d %v", status, data)
			}
		}
	})
	t.Run("a snoozed card can still be answered, and then it is no longer waiting", func(t *testing.T) {
		if status, data := snooze(owner, plain, future); status != 200 {
			t.Fatalf("snooze: %d %v", status, data)
		}
		if status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+plain+"/answer", owner, `{"option_idxs":[0]}`); status != 200 {
			t.Fatalf("answer a snoozed card: %d %v", status, data)
		}
		if status, data := snooze(owner, plain, future); status != 409 {
			t.Fatalf("snooze an answered card: want 409, got %d %v", status, data)
		}
		if status, data := snooze(owner, "rc-000000000000", future); status != 404 {
			t.Fatalf("unknown card: want 404, got %d %v", status, data)
		}
	})
}
