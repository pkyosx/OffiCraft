package main

import (
	"reflect"
	"testing"
)

func TestSortReplyCardQueue(t *testing.T) {
	str := func(s string) *string { return &s }
	num := func(f float64) *float64 { return &f }
	rows := []replyCardListItemDTO{
		{ID: "low", CreatedTS: 1, replyCardTaskStanding: replyCardTaskStanding{Priority: str("low")}},
		{ID: "no-task-old", CreatedTS: 2},
		{ID: "mid-due-late", CreatedTS: 9, replyCardTaskStanding: replyCardTaskStanding{Priority: str("mid"), DueTS: num(900)}},
		{ID: "frozen", CreatedTS: 0, replyCardTaskStanding: replyCardTaskStanding{Priority: str("frozen")}},
		{ID: "high", CreatedTS: 50, replyCardTaskStanding: replyCardTaskStanding{Priority: str("high")}},
		{ID: "mid-due-soon", CreatedTS: 8, replyCardTaskStanding: replyCardTaskStanding{Priority: str("mid"), DueTS: num(500)}},
		{ID: "mid-new", CreatedTS: 3, replyCardTaskStanding: replyCardTaskStanding{Priority: str("mid")}},
	}
	sortReplyCardQueue(rows)
	var got []string
	for _, r := range rows {
		got = append(got, r.ID)
	}
	want := []string{"high", "mid-due-soon", "mid-due-late", "no-task-old", "mid-new", "low", "frozen"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("queue order = %v, want %v", got, want)
	}
}

func TestReplyCardShowsItsTasksPriorityAndDue(t *testing.T) {
	api, h, _, owner := newAPITestServer(t)
	executor, steps := apiTestTwoStepTask(t, api, h, owner)
	setTask := func(path, body string) {
		t.Helper()
		if status, data := apiJSON(t, h, "POST", "/api/tasks/T-1/"+path, owner, body); status != 200 {
			t.Fatalf("set %s: %d %v", path, status, data)
		}
	}
	setTask("priority", `{"priority":"high"}`)
	setTask("due", `{"due_ts":1900000000,"due_soon_lead_minutes":90}`)
	agent := apiTestAgentToken(t, api, "mira", "")
	get := func(id string) map[string]any {
		t.Helper()
		status, card := apiJSON(t, h, "GET", "/api/reply-cards/"+id, owner, "")
		if status != 200 {
			t.Fatalf("get card: %d %v", status, card)
		}
		return card
	}
	want := func(card map[string]any, priority, due, lead any) {
		t.Helper()
		got := []any{card["priority"], card["due_ts"], card["due_soon_lead_minutes"]}
		if !reflect.DeepEqual(got, []any{priority, due, lead}) {
			t.Fatalf("card priority/due_ts/due_soon_lead_minutes = %v, want %v", got, []any{priority, due, lead})
		}
	}

	plain := apiTestOpenReplyCard(t, h, agent,
		`{"kind":"decision","summary":"plain","options":[{"text":"x"}],"linked_task":null}`)
	about := apiTestOpenReplyCard(t, h, agent,
		`{"kind":"decision","summary":"about","options":[{"text":"x"}],"linked_task":null,"about_task_id":"T-1"}`)
	bound := apiTestOpenReplyCard(t, h, executor,
		`{"kind":"decision","summary":"bound","options":[{"text":"x"}],`+
			`"linked_task":{"task_id":"T-1","step_id":"`+steps[0]+`"}}`)

	t.Run("a card naming no task has no priority, no due date and no lead", func(t *testing.T) {
		want(get(plain), nil, nil, nil)
	})
	t.Run("a card naming a task shows the task's priority, due date and lead", func(t *testing.T) {
		want(get(about), "high", float64(1900000000), float64(90))
		want(get(bound), "high", float64(1900000000), float64(90))
	})
	t.Run("the waiting pane ranks task cards by the task and a card naming no task as mid with no due date", func(t *testing.T) {
		var got []string
		for _, row := range apiTestReplyCardPane(t, h, owner, "") {
			got = append(got, row.(map[string]any)["summary"].(string))
		}
		if want := []string{"about", "bound", "plain"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("waiting pane = %v, want %v", got, want)
		}
	})

	setTask("priority", `{"priority":"low"}`)
	setTask("due", `{"due_ts":null}`)

	t.Run("a later change to the task shows on its cards", func(t *testing.T) {
		want(get(about), "low", nil, nil)
		want(get(bound), "low", nil, nil)
		want(get(plain), nil, nil, nil)
	})
	t.Run("and moves them in the waiting pane", func(t *testing.T) {
		var got []string
		for _, row := range apiTestReplyCardPane(t, h, owner, "") {
			r := row.(map[string]any)
			got = append(got, r["summary"].(string))
			if r["summary"] == "about" && r["priority"] != "low" {
				t.Fatalf("list row did not follow the task: %v", r)
			}
		}
		if want := []string{"plain", "about", "bound"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("waiting pane = %v, want %v", got, want)
		}
	})

	t.Run("create_reply_card has no priority or due_ts to send", func(t *testing.T) {
		for _, extra := range []string{`"priority":"high"`, `"due_ts":1800000000`} {
			before := len(apiTestReplyCardPane(t, h, owner, ""))
			status, data := apiJSON(t, h, "POST", "/api/reply-cards", agent,
				`{"kind":"decision","summary":"f","options":[{"text":"x"}],"linked_task":null,`+extra+`}`)
			if status != 422 {
				t.Fatalf("%s: want 422, got %d (%v)", extra, status, data)
			}
			if after := len(apiTestReplyCardPane(t, h, owner, "")); after != before {
				t.Fatalf("%s: a refused card was opened: %d -> %d", extra, before, after)
			}
		}
	})
}
