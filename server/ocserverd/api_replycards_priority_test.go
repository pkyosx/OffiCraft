package main

import (
	"reflect"
	"testing"
)

func TestSortReplyCardQueue(t *testing.T) {
	cards := []ReplyCard{
		{ID: "low", Priority: "low", CreatedTS: 1},
		{ID: "mid-old", CreatedTS: 2},
		{ID: "mid-due-late", Priority: "mid", DueTS: 900, CreatedTS: 9},
		{ID: "high", Priority: "high", CreatedTS: 50},
		{ID: "mid-due-soon", Priority: "mid", DueTS: 500, CreatedTS: 8},
		{ID: "mid-new", Priority: "mid", CreatedTS: 3},
	}
	sortReplyCardQueue(cards)
	var got []string
	for _, c := range cards {
		got = append(got, c.ID)
	}
	want := []string{"high", "mid-due-soon", "mid-due-late", "mid-old", "mid-new", "low"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("queue order = %v, want %v", got, want)
	}
}

func TestReplyCardPriorityAndDue(t *testing.T) {
	api, h, _, owner := newAPITestServer(t)
	executor, steps := apiTestTwoStepTask(t, api, h, owner)
	if status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/priority", owner, `{"priority":"high"}`); status != 200 {
		t.Fatalf("set priority: %d %v", status, data)
	}
	if status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/due", owner, `{"due_ts":1900000000}`); status != 200 {
		t.Fatalf("set due: %d %v", status, data)
	}
	agent := apiTestAgentToken(t, api, "mira", "")
	open := func(token, body string) map[string]any {
		t.Helper()
		id := apiTestOpenReplyCard(t, h, token, body)
		status, card := apiJSON(t, h, "GET", "/api/reply-cards/"+id, owner, "")
		if status != 200 {
			t.Fatalf("get card: %d %v", status, card)
		}
		return card
	}
	want := func(card map[string]any, priority string, due any) {
		t.Helper()
		if card["priority"] != priority || !reflect.DeepEqual(card["due_ts"], due) {
			t.Fatalf("card priority/due = %v/%v, want %v/%v", card["priority"], card["due_ts"], priority, due)
		}
	}

	t.Run("an unbound card that names neither field is mid with no due date", func(t *testing.T) {
		want(open(agent, `{"kind":"decision","summary":"a","options":[{"text":"x"}],"linked_task":null}`), "mid", nil)
	})
	t.Run("an unbound card keeps the priority and due date it was sent", func(t *testing.T) {
		want(open(agent, `{"kind":"decision","summary":"b","options":[{"text":"x"}],"linked_task":null,`+
			`"priority":"low","due_ts":1800000000}`), "low", float64(1800000000))
	})
	t.Run("a card naming a task with about_task_id takes the task's priority and due date when it names neither", func(t *testing.T) {
		want(open(agent, `{"kind":"decision","summary":"c","options":[{"text":"x"}],"linked_task":null,`+
			`"about_task_id":"T-1"}`), "high", float64(1900000000))
	})
	t.Run("a card bound to a step takes the task's priority and due date when it names neither", func(t *testing.T) {
		want(open(executor, `{"kind":"decision","summary":"d","options":[{"text":"x"}],`+
			`"linked_task":{"task_id":"T-1","step_id":"`+steps[0]+`"}}`), "high", float64(1900000000))
	})
	t.Run("a card bound to a step keeps what it was sent over the task's values", func(t *testing.T) {
		want(open(executor, `{"kind":"decision","summary":"e","options":[{"text":"x"}],`+
			`"linked_task":{"task_id":"T-1","step_id":"`+steps[1]+`"},"priority":"low","due_ts":1700000000}`),
			"low", float64(1700000000))
	})

	for _, tc := range []struct{ name, extra, msg string }{
		{"a priority outside high, mid and low answers 400", `"priority":"frozen"`, replyCardPriorityMsg},
		{"a due_ts that is not greater than 0 answers 400", `"due_ts":0`, taskDueTSPositiveMsg},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(apiTestReplyCardPane(t, h, owner, ""))
			status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
				`{"kind":"decision","summary":"f","options":[{"text":"x"}],"linked_task":null,`+tc.extra+`}`)
			if status != 400 {
				t.Fatalf("want 400, got %d (%v)", status, data)
			}
			apiWantError(t, data, "validation_error", tc.msg)
			if after := len(apiTestReplyCardPane(t, h, owner, "")); after != before {
				t.Fatalf("a refused card was opened: %d -> %d", before, after)
			}
		})
	}

	t.Run("the waiting pane lists by priority, then due date, then age, and each row carries both fields", func(t *testing.T) {
		var got []string
		for _, row := range apiTestReplyCardPane(t, h, owner, "") {
			r := row.(map[string]any)
			if _, ok := r["due_ts"]; !ok || r["priority"] == nil {
				t.Fatalf("row lacks priority/due_ts: %v", r)
			}
			got = append(got, r["summary"].(string))
		}
		want := []string{"c", "d", "a", "e", "b"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("waiting pane = %v, want %v", got, want)
		}
	})

	t.Run("answering or expiring a card keeps its priority and due date", func(t *testing.T) {
		body := `{"kind":"decision","summary":"g","options":[{"text":"x"}],"linked_task":null,` +
			`"priority":"high","due_ts":1850000000}`
		answered := open(agent, body)
		if status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+answered["id"].(string)+"/answer", owner,
			`{"option_idxs":[0]}`); status != 200 {
			t.Fatalf("answer: %d %v", status, data)
		}
		expired := open(agent, body)
		if status, data := apiJSON(t, h, "POST", "/api/reply-cards/"+expired["id"].(string)+"/expire", owner, ""); status != 200 {
			t.Fatalf("expire: %d %v", status, data)
		}
		for _, id := range []string{answered["id"].(string), expired["id"].(string)} {
			_, card := apiJSON(t, h, "GET", "/api/reply-cards/"+id, owner, "")
			if card["status"] == "waiting" {
				t.Fatalf("card %s still waiting", id)
			}
			want(card, "high", float64(1850000000))
		}
	})
}
