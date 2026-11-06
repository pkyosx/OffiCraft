package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
)

const apiTestCardSecretValue = "dummy-aaaaaaaaaaaa"

func apiTestSecretCardBody(summary string) string {
	return `{"kind":"secret","summary":"` + summary + `","body":"where it goes","options":[],"linked_task":null}`
}

func (f apiTestSecretFixture) openSecretCard(t *testing.T, summary string) string {
	t.Helper()
	return apiTestOpenReplyCard(t, f.h, f.agent, apiTestSecretCardBody(summary))
}

func (f apiTestSecretFixture) provide(t *testing.T, cardID, value string) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"value": value})
	status, data := apiJSON(t, f.h, "POST", "/api/reply-cards/"+cardID+"/secret", f.owner, string(body))
	if status != http.StatusOK {
		t.Fatalf("provide: %d %v", status, data)
	}
	return data
}

func (f apiTestSecretFixture) providedCard(t *testing.T) string {
	t.Helper()
	cardID := f.openSecretCard(t, "the deploy key")
	f.provide(t, cardID, apiTestCardSecretValue)
	return cardID
}

func (f apiTestSecretFixture) card(t *testing.T, id string) ReplyCard {
	t.Helper()
	c, err := f.d.GetReplyCard(id)
	if err != nil || c == nil {
		t.Fatalf("card %s: %v %v", id, c, err)
	}
	return *c
}

func (f apiTestSecretFixture) wantCiphertext(t *testing.T, cardID string, want bool) {
	t.Helper()
	row, err := getReplyCardSecretOn(f.d.rdb, cardID)
	if err != nil {
		t.Fatalf("ciphertext row: %v", err)
	}
	if (row != nil) != want {
		t.Fatalf("%s ciphertext present = %v, want %v", cardID, row != nil, want)
	}
}

func (f apiTestSecretFixture) secretBlock(t *testing.T, cardID string) map[string]any {
	t.Helper()
	status, data := apiJSON(t, f.h, "GET", "/api/reply-cards/"+cardID, f.owner, "")
	if status != http.StatusOK {
		t.Fatalf("get card: %d %v", status, data)
	}
	block, _ := data["secret"].(map[string]any)
	if block == nil {
		t.Fatalf("card %s has no secret block: %v", cardID, data)
	}
	return block
}

func (f apiTestSecretFixture) setDeadline(t *testing.T, cardID string, ts float64) {
	t.Helper()
	c := f.card(t, cardID)
	c.SecretDestroyAfterTS = ts
	if err := f.d.PutReplyCard(c); err != nil {
		t.Fatalf("PutReplyCard: %v", err)
	}
}

func (f apiTestSecretFixture) setRetention(t *testing.T, secs string) {
	t.Helper()
	status, data := apiJSON(t, f.h, "PATCH", "/api/settings", f.owner, `{"secret_card_retrieved_retention_secs":`+secs+`}`)
	if status != http.StatusOK {
		t.Fatalf("patch retention: %d %v", status, data)
	}
}

func apiTestSecretCardFrame(cardID, from, status, trigger string) map[string]any {
	frame := apiTestReplyCardFrame(0, cardID, from, status, trigger)
	frame["seq"] = apiAnyNumber
	frame["data"].(map[string]any)["epoch"] = apiAnyNumber
	return frame
}

func TestCreateSecretReplyCard(t *testing.T) {
	t.Run("a secret card opens without options and reads back in the waiting state", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.openSecretCard(t, "the deploy key")
		status, data := apiJSON(t, f.h, "GET", "/api/reply-cards/"+cardID, f.agent, "")
		if status != http.StatusOK {
			t.Fatalf("get: %d %v", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": cardID, "from": "kip", "kind": "secret", "summary": "the deploy key", "body": "where it goes",
			"options": []any{}, "select_mode": "single", "status": "waiting", "created_ts": apiAnyNumber,
			"attachments": []any{}, "answered_ts": nil, "expired_ts": nil,
			"chat_message_id": apiAnyString, "answer": nil, "task": nil, "task_executor": "",
			"snoozed_until_ts": nil,
			"secret":           map[string]any{"state": "waiting", "provided_ts": nil, "retrieved_ts": nil, "expires_ts": nil},
		})
	})

	t.Run("options on a secret card are refused", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		status, data := apiJSON(t, f.h, "POST", "/api/reply-cards", f.agent,
			`{"kind":"secret","summary":"s","options":[{"text":"a"}],"linked_task":null}`)
		if status != http.StatusBadRequest {
			t.Fatalf("want 400, got %d %v", status, data)
		}
		apiWantError(t, data, "validation_error", "a secret card must not carry options")
	})

	t.Run("an unknown kind names all three", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		status, data := apiJSON(t, f.h, "POST", "/api/reply-cards", f.agent,
			`{"kind":"poll","summary":"s","options":[{"text":"a"}],"linked_task":null}`)
		if status != http.StatusBadRequest {
			t.Fatalf("want 400, got %d %v", status, data)
		}
		apiWantError(t, data, "validation_error", "kind must be 'decision' or 'action' or 'secret'")
	})

	t.Run("other kinds still need an option and carry no secret block", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		status, data := apiJSON(t, f.h, "POST", "/api/reply-cards", f.agent,
			`{"kind":"action","summary":"s","options":[],"linked_task":null}`)
		if status != http.StatusBadRequest {
			t.Fatalf("want 400, got %d %v", status, data)
		}
		apiWantError(t, data, "validation_error", "options must carry at least one choice")
		cardID := apiTestOpenReplyCard(t, f.h, f.agent, `{"kind":"action","summary":"s","options":[{"text":"a"}],"linked_task":null}`)
		_, got := apiJSON(t, f.h, "GET", "/api/reply-cards/"+cardID, f.owner, "")
		if _, present := got["secret"]; present {
			t.Fatalf("a non-secret card carries a secret block: %v", got)
		}
	})
}

func TestHandleProvideReplyCardSecretApiReplyCardsCardIdSecretPost(t *testing.T) {
	t.Run("the value is sealed, the card answers with an empty answer and author and owner are told", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.openSecretCard(t, "the deploy key")
		dashboard := apiTestListen(t, f.api, "")
		author := apiTestListen(t, f.api, "kip")

		data := f.provide(t, cardID, apiTestCardSecretValue)

		apiWantBody(t, data, map[string]any{
			"id": cardID, "status": "answered", "answered_ts": apiAnyNumber, "expired_ts": nil,
			"answer":  map[string]any{"option_idxs": nil, "text": "", "attachments": []any{}},
			"task_id": "", "step_id": "",
		})
		want := apiTestSecretCardFrame(cardID, "kip", "answered", "owner")
		dashboard.wantFrames(want)
		author.wantFrames(want)
		f.wantCiphertext(t, cardID, true)
		stored := f.card(t, cardID)
		if stored.AnswerText != "" || stored.SecretProvidedTS != stored.AnsweredTS ||
			stored.SecretDestroyAfterTS != stored.SecretProvidedTS+secretCardUnretrievedTTLSecsDefault {
			t.Fatalf("stored card = %#v", stored)
		}
		apiWantValue(t, "secret", any(f.secretBlock(t, cardID)), any(map[string]any{
			"state": "provided", "provided_ts": apiAnyNumber, "retrieved_ts": nil, "expires_ts": apiAnyNumber,
		}))
	})

	t.Run("a bound card frees its step and task in the same write", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		executor, steps := apiTestTwoStepTask(t, f.api, f.h, f.owner)
		cardID := apiTestOpenReplyCard(t, f.h, executor,
			`{"kind":"secret","summary":"key","options":[],"linked_task":{"task_id":"T-1","step_id":"`+steps[0]+`"}}`)
		apiTestWantStepStatuses(t, f.d, "T-1", "waiting_owner", "pending")
		data := f.provide(t, cardID, apiTestCardSecretValue)
		apiWantValue(t, "receipt.step_id", data["step_id"], steps[0])
		apiTestWantStepStatuses(t, f.d, "T-1", "in_progress", "pending")
	})

	t.Run("only the owner may provide, and the route stays off MCP", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.openSecretCard(t, "k")
		body := `{"value":"` + apiTestCardSecretValue + `"}`
		for name, token := range map[string]string{"author": f.agent, "admin": f.admin, "machine": f.machine} {
			if rec := apiRequest(t, f.h, "POST", "/api/reply-cards/"+cardID+"/secret", token, body); rec.Code != http.StatusForbidden {
				t.Fatalf("%s answered %d %s, want 403", name, rec.Code, rec.Body)
			}
		}
		if got := f.card(t, cardID).Status; got != "waiting" {
			t.Fatalf("status = %q after refused provides", got)
		}
		for _, r := range specsFor(f.api) {
			if r.Path == "/api/reply-cards/{card_id}/secret" && (!r.MCPExclude || r.MCPTool != "") {
				t.Fatalf("route on the MCP surface: %+v", r)
			}
		}
	})

	t.Run("refusals name the field or limit and never echo the value or its length", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.openSecretCard(t, "k")
		over := strings.Repeat("a", secretValueMaxChars+1)
		cases := []struct {
			name, body string
			status     int
			message    string
		}{
			{"empty", `{"value":""}`, 400, "value must not be empty"},
			{"over the rune limit", `{"value":"` + over + `"}`, 400, "value is over the 65536-char limit"},
			{"over the body limit", `{"value":"` + strings.Repeat("a", replyCardSecretBodyBytes+1) + `"}`, 400, "value is over the 65536-char limit"},
			{"missing", `{}`, 422, "field required: value"},
			{"unknown key", `{"value":"x","note":"dummy-aaaaaaaaaaaa"}`, 422, "invalid request body: expected a JSON object with one string field, value"},
			{"bad escape inside the value", `{"value":"dummy-\q"}`, 422, "invalid request body: expected a JSON object with one string field, value"},
		}
		for _, tc := range cases {
			status, data := apiJSON(t, f.h, "POST", "/api/reply-cards/"+cardID+"/secret", f.owner, tc.body)
			if status != tc.status {
				t.Fatalf("%s: status %d %v, want %d", tc.name, status, data, tc.status)
			}
			msg, _ := data["error"].(map[string]any)["message"].(string)
			if msg != tc.message {
				t.Fatalf("%s: message %q, want %q", tc.name, msg, tc.message)
			}
		}
		f.wantCiphertext(t, cardID, false)
		if got := f.card(t, cardID).Status; got != "waiting" {
			t.Fatalf("status = %q after refused provides", got)
		}
	})

	t.Run("a value at the limit is accepted", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.openSecretCard(t, "k")
		f.provide(t, cardID, strings.Repeat("a", secretValueMaxChars))
		f.wantCiphertext(t, cardID, true)
	})

	t.Run("only a waiting secret card on an open task takes a value", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		value := `{"value":"` + apiTestCardSecretValue + `"}`
		post := func(id string) (int, map[string]any) {
			return apiJSON(t, f.h, "POST", "/api/reply-cards/"+id+"/secret", f.owner, value)
		}
		if status, _ := post("rc-000000000000"); status != http.StatusNotFound {
			t.Fatalf("unknown card: %d", status)
		}
		plain := apiTestOpenReplyCard(t, f.h, f.agent, `{"kind":"decision","summary":"s","options":[{"text":"a"}],"linked_task":null}`)
		status, data := post(plain)
		if status != http.StatusConflict {
			t.Fatalf("plain card: %d %v", status, data)
		}
		apiWantError(t, data, "conflict", "reply card '"+plain+"' is not a secret card")

		provided := f.providedCard(t)
		status, data = post(provided)
		if status != http.StatusConflict {
			t.Fatalf("provided card: %d %v", status, data)
		}
		apiWantError(t, data, "conflict", "reply card '"+provided+"' is answered — only a waiting secret card takes a value")

		replied := f.openSecretCard(t, "k")
		if status, data := apiJSON(t, f.h, "POST", "/api/reply-cards/"+replied+"/answer", f.owner, `{"text":"why do you need it?"}`); status != 200 {
			t.Fatalf("answer: %d %v", status, data)
		}
		if status, _ := post(replied); status != http.StatusConflict {
			t.Fatalf("replied card: %d", status)
		}
		f.wantCiphertext(t, replied, false)

		executor, _ := apiTestTwoStepTask(t, f.api, f.h, f.owner)
		about := apiTestOpenReplyCard(t, f.h, executor, `{"kind":"secret","summary":"k","options":[],"linked_task":null,"about_task_id":"T-1"}`)
		c := f.card(t, about)
		task, err := f.d.GetTask(c.TaskID)
		if err != nil || task == nil {
			t.Fatalf("GetTask: %v %v", task, err)
		}
		task.Status = TaskStatusTerminated
		if err := f.d.inTx(func(tx *writeTx) error { return putTaskOn(tx, *task, taskWriteUpsert) }); err != nil {
			t.Fatalf("close task: %v", err)
		}
		if status, _ := post(about); status != http.StatusConflict {
			t.Fatalf("closed task: %d", status)
		}
		f.wantCiphertext(t, about, false)
	})

	t.Run("a missing key with sealed card rows answers 503 instead of minting a new key", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		f.providedCard(t)
		if err := os.Remove(f.keyPath); err != nil {
			t.Fatal(err)
		}
		f.api.vault = newSecretVault(f.keyPath)
		next := f.openSecretCard(t, "second")
		status, data := apiJSON(t, f.h, "POST", "/api/reply-cards/"+next+"/secret", f.owner, `{"value":"`+apiTestCardSecretValue+`"}`)
		if status != http.StatusServiceUnavailable {
			t.Fatalf("status %d %v, want 503", status, data)
		}
		apiWantError(t, data, "service_unavailable", errVaultKeyMissing.Error())
		status, data = apiJSON(t, f.h, "POST", "/api/secrets", f.owner, `{"name":"b","value":"fake-value-2"}`)
		if status != http.StatusServiceUnavailable {
			t.Fatalf("stored-secret create: %d %v, want 503", status, data)
		}
		if _, err := os.Stat(f.keyPath); !os.IsNotExist(err) {
			t.Fatalf("a replacement key was minted: %v", err)
		}
		f.wantCiphertext(t, next, false)
	})

	t.Run("with every sealed value gone, a fresh key may be minted", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		if status, data := apiJSON(t, f.h, "POST", "/api/reply-cards/"+cardID+"/expire", f.owner, `{}`); status != 200 {
			t.Fatalf("expire: %d %v", status, data)
		}
		if err := os.Remove(f.keyPath); err != nil {
			t.Fatal(err)
		}
		f.api.vault = newSecretVault(f.keyPath)
		f.provide(t, f.openSecretCard(t, "second"), apiTestCardSecretValue)
		if _, err := os.Stat(f.keyPath); err != nil {
			t.Fatalf("no key was minted: %v", err)
		}
	})
}

func TestSecretCardValueNeverLeaves(t *testing.T) {
	f := newAPITestSecretFixture(t)
	cardID := f.openSecretCard(t, "the deploy key")
	dashboard := apiTestListen(t, f.api, "")
	author := apiTestListen(t, f.api, "kip")
	receipt := apiRequest(t, f.h, "POST", "/api/reply-cards/"+cardID+"/secret", f.owner, `{"value":"`+apiTestCardSecretValue+`"}`)
	refusal := apiRequest(t, f.h, "POST", "/api/reply-cards/"+cardID+"/secret", f.owner, `{"value":"`+apiTestCardSecretValue+`"}`)
	refusedReanswer := apiRequest(t, f.h, "PUT", "/api/reply-cards/"+cardID+"/answer", f.owner, `{"text":"changed"}`)

	surfaces := map[string]string{
		"provide receipt":  receipt.Body.String(),
		"second provide":   refusal.Body.String(),
		"reanswer refusal": refusedReanswer.Body.String(),
		"get":              apiRequest(t, f.h, "GET", "/api/reply-cards/"+cardID, f.owner, "").Body.String(),
		"get as author":    apiRequest(t, f.h, "GET", "/api/reply-cards/"+cardID, f.agent, "").Body.String(),
		"answered list":    apiRequest(t, f.h, "GET", "/api/reply-cards?status=answered", f.owner, "").Body.String(),
		"chat history":     apiRequest(t, f.h, "GET", "/api/chat?with=kip", f.owner, "").Body.String(),
		"resume_summary":   apiRequest(t, f.h, "GET", "/api/resume-summary", f.agent, "").Body.String(),
		"another's reveal": apiRequest(t, f.h, "POST", "/api/self/secrets/reveal", f.admin, `{"ids":["`+cardID+`"]}`).Body.String(),
	}
	for name, body := range surfaces {
		if strings.Contains(body, apiTestCardSecretValue) {
			t.Fatalf("%s carries the value: %s", name, body)
		}
	}
	if !strings.Contains(surfaces["answered list"], cardID) || !strings.Contains(surfaces["chat history"], cardID) ||
		!strings.Contains(surfaces["get"], `"state":"provided"`) {
		t.Fatalf("a surface did not actually carry the card: %v", surfaces)
	}
	for name, l := range map[string]*apiTestListener{"owner": dashboard, "author": author} {
		for {
			raw := l.l.pop()
			if raw == nil {
				break
			}
			if strings.Contains(string(raw), apiTestCardSecretValue) {
				t.Fatalf("%s SSE frame carries the value: %s", name, raw)
			}
		}
	}
	row, err := getReplyCardSecretOn(f.d.rdb, cardID)
	if err != nil || row == nil || strings.Contains(string(row.ValueCT), apiTestCardSecretValue) {
		t.Fatalf("ciphertext row %v %v holds the plaintext", row, err)
	}

	// Positive control: the same value does come back through the one door that serves it.
	status, data := f.reveal(t, f.agent, cardID)
	if status != http.StatusOK {
		t.Fatalf("reveal: %d %v", status, data)
	}
	apiWantBody(t, data, map[string]any{"secrets": []any{map[string]any{
		"id": cardID, "name": "the deploy key", "note": "", "value": apiTestCardSecretValue,
	}}})
}

func TestRevealSecretCard(t *testing.T) {
	t.Run("the first retrieval returns the value and stamps the retention, and the delta reaches the owner only", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		dashboard := apiTestListen(t, f.api, "")
		author := apiTestListen(t, f.api, "kip")

		status, data := f.reveal(t, f.agent, cardID)
		if status != http.StatusOK {
			t.Fatalf("reveal: %d %v", status, data)
		}
		apiWantBody(t, data, map[string]any{"secrets": []any{map[string]any{
			"id": cardID, "name": "the deploy key", "note": "", "value": apiTestCardSecretValue,
		}}})
		dashboard.wantFrames(apiTestSecretCardFrame(cardID, "kip", "answered", "kip"))
		author.wantFrames()
		stored := f.card(t, cardID)
		if stored.Status != "answered" || stored.SecretRetrievedTS <= 0 ||
			stored.SecretDestroyAfterTS != stored.SecretRetrievedTS+secretCardRetrievedRetentionSecsDefault {
			t.Fatalf("stored card = %#v", stored)
		}
		f.wantCiphertext(t, cardID, true)
		apiWantValue(t, "secret", any(f.secretBlock(t, cardID)), any(map[string]any{
			"state": "retrieved", "provided_ts": apiAnyNumber, "retrieved_ts": apiAnyNumber, "expires_ts": apiAnyNumber,
		}))
	})

	t.Run("a second retrieval inside the window changes nothing and tells nobody", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		if status, data := f.reveal(t, f.agent, cardID); status != 200 {
			t.Fatalf("first: %d %v", status, data)
		}
		before := f.card(t, cardID)
		dashboard := apiTestListen(t, f.api, "")
		status, data := f.reveal(t, f.agent, cardID)
		if status != 200 {
			t.Fatalf("second: %d %v", status, data)
		}
		apiWantValue(t, "value", data["secrets"].([]any)[0].(map[string]any)["value"], apiTestCardSecretValue)
		after := f.card(t, cardID)
		if after.SecretRetrievedTS != before.SecretRetrievedTS || after.SecretDestroyAfterTS != before.SecretDestroyAfterTS {
			t.Fatalf("deadline moved: %#v -> %#v", before, after)
		}
		dashboard.wantFrames()
	})

	t.Run("after the window a retrieval is refused and nothing changes", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		if status, _ := f.reveal(t, f.agent, cardID); status != 200 {
			t.Fatalf("first: %d", status)
		}
		f.setDeadline(t, cardID, nowSecs()-1)
		before := f.card(t, cardID)
		status, data := f.reveal(t, f.agent, cardID)
		if status != http.StatusConflict {
			t.Fatalf("status %d %v, want 409", status, data)
		}
		apiWantError(t, data, "conflict", "no retrievable value on secret card(s): "+cardID+
			" — not provided, already destroyed, past its deadline, or its task is closed")
		if after := f.card(t, cardID); after.SecretDestroyAfterTS != before.SecretDestroyAfterTS {
			t.Fatalf("a refused reveal wrote: %#v", after)
		}
	})

	t.Run("retention 0 destroys the ciphertext in the retrieving write", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		f.setRetention(t, "0")
		cardID := f.providedCard(t)
		status, data := f.reveal(t, f.agent, cardID)
		if status != 200 {
			t.Fatalf("reveal: %d %v", status, data)
		}
		apiWantValue(t, "value", data["secrets"].([]any)[0].(map[string]any)["value"], apiTestCardSecretValue)
		f.wantCiphertext(t, cardID, false)
		apiWantValue(t, "secret", any(f.secretBlock(t, cardID)), any(map[string]any{
			"state": "retrieved", "provided_ts": apiAnyNumber, "retrieved_ts": apiAnyNumber, "expires_ts": nil,
		}))
		if status, _ := f.reveal(t, f.agent, cardID); status != http.StatusConflict {
			t.Fatalf("second: %d, want 409", status)
		}
	})

	t.Run("anyone but the author is refused, and nothing is written", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		if err := f.d.putMemberWholeRowForTest(Member{ID: "mira", Name: "Mira", Kind: KindStaff, RoleKey: "engineer", RosterStatus: RosterStatusActive}); err != nil {
			t.Fatal(err)
		}
		mira := apiTestAgentToken(t, f.api, "mira", "")
		status, data := f.reveal(t, mira, cardID)
		if status != http.StatusForbidden {
			t.Fatalf("other member: %d %v", status, data)
		}
		apiWantError(t, data, "forbidden", "only a secret card's author may retrieve its value: "+cardID)
		if status, _ := f.reveal(t, f.admin, cardID); status != http.StatusForbidden {
			t.Fatalf("admin agent: %d", status)
		}
		if status, _ := f.reveal(t, f.owner, cardID); status != http.StatusForbidden {
			t.Fatalf("owner: %d", status)
		}
		if ts := f.card(t, cardID).SecretRetrievedTS; ts != 0 {
			t.Fatalf("a refused reveal stamped retrieved_ts %v", ts)
		}
	})

	t.Run("an author off the active roster is refused", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		for _, who := range f.notActiveAIMembers(t) {
			if status, _ := f.reveal(t, who.token, cardID); status != who.status {
				t.Fatalf("%s: %d, want %d", who.name, status, who.status)
			}
		}
	})

	t.Run("an unknown or non-secret card reads as unknown", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		plain := apiTestOpenReplyCard(t, f.h, f.agent, `{"kind":"decision","summary":"s","options":[{"text":"a"}],"linked_task":null}`)
		status, data := f.reveal(t, f.agent, plain, "rc-000000000000", "sec-000000000000")
		if status != http.StatusNotFound {
			t.Fatalf("status %d %v", status, data)
		}
		apiWantError(t, data, "not_found", "unknown secret id(s): "+plain+", rc-000000000000, sec-000000000000")
	})

	t.Run("a waiting card, a replied card and a closed task are refused as no retrievable value", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		waiting := f.openSecretCard(t, "k")
		replied := f.openSecretCard(t, "k")
		if status, _ := apiJSON(t, f.h, "POST", "/api/reply-cards/"+replied+"/answer", f.owner, `{"text":"no"}`); status != 200 {
			t.Fatalf("answer failed")
		}
		for _, id := range []string{waiting, replied} {
			if status, data := f.reveal(t, f.agent, id); status != http.StatusConflict {
				t.Fatalf("%s: %d %v", id, status, data)
			}
		}
		executor, _ := apiTestTwoStepTask(t, f.api, f.h, f.owner)
		about := apiTestOpenReplyCard(t, f.h, executor, `{"kind":"secret","summary":"k","options":[],"linked_task":null,"about_task_id":"T-1"}`)
		f.provide(t, about, apiTestCardSecretValue)
		task, err := f.d.GetTask("T-1")
		if err != nil || task == nil {
			t.Fatalf("GetTask: %v", err)
		}
		task.Status = TaskStatusTerminated
		if err := f.d.inTx(func(tx *writeTx) error { return putTaskOn(tx, *task, taskWriteUpsert) }); err != nil {
			t.Fatal(err)
		}
		if status, data := f.reveal(t, executor, about); status != http.StatusConflict {
			t.Fatalf("closed task: %d %v", status, data)
		}
	})

	t.Run("mixed ids are all or nothing: one refusal returns no value and writes nothing", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			bad    func(f apiTestSecretFixture) string
			status int
		}{
			{"unknown secret", func(apiTestSecretFixture) string { return "sec-000000000000" }, http.StatusNotFound},
			{"unknown card", func(apiTestSecretFixture) string { return "rc-000000000000" }, http.StatusNotFound},
			{"waiting card", func(f apiTestSecretFixture) string { return f.openSecretCard(t, "k") }, http.StatusConflict},
			{"someone else's card", func(f apiTestSecretFixture) string {
				return apiTestOpenReplyCard(t, f.h, f.admin, apiTestSecretCardBody("theirs"))
			}, http.StatusForbidden},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newAPITestSecretFixture(t)
				sec, _ := f.create(t, "n", apiTestSecretValue, "")["id"].(string)
				good := f.providedCard(t)
				dashboard := apiTestListen(t, f.api, "")
				rec := apiRequest(t, f.h, "POST", "/api/self/secrets/reveal", f.agent,
					`{"ids":["`+sec+`","`+good+`","`+tc.bad(f)+`"]}`)
				if rec.Code != tc.status {
					t.Fatalf("status %d %s, want %d", rec.Code, rec.Body, tc.status)
				}
				if strings.Contains(rec.Body.String(), apiTestCardSecretValue) || strings.Contains(rec.Body.String(), apiTestSecretValue) {
					t.Fatalf("a refusal carries a value: %s", rec.Body)
				}
				f.wantNeverAccessed(t, sec)
				if ts := f.card(t, good).SecretRetrievedTS; ts != 0 {
					t.Fatalf("refused call stamped retrieved_ts %v", ts)
				}
				f.wantCiphertext(t, good, true)
				for raw := dashboard.l.pop(); raw != nil; raw = dashboard.l.pop() {
					if strings.Contains(string(raw), good) {
						t.Fatalf("a refused reveal published: %s", raw)
					}
				}
			})
		}
	})

	t.Run("a card and a stored secret come back together in request order", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		sec, _ := f.create(t, "n", apiTestSecretValue, "note")["id"].(string)
		cardID := f.providedCard(t)
		status, data := f.reveal(t, f.agent, cardID, sec)
		if status != 200 {
			t.Fatalf("reveal: %d %v", status, data)
		}
		apiWantBody(t, data, map[string]any{"secrets": []any{
			map[string]any{"id": cardID, "name": "the deploy key", "note": "", "value": apiTestCardSecretValue},
			map[string]any{"id": sec, "name": "n", "note": "note", "value": apiTestSecretValue},
		}})
	})

	t.Run("a card id of the wrong shape is a 400", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		status, data := f.reveal(t, f.agent, "rc-ABC")
		if status != http.StatusBadRequest {
			t.Fatalf("status %d %v", status, data)
		}
		apiWantError(t, data, "validation_error", "ids must be secret ids (sec- followed by 12 lowercase hex digits) "+
			"or secret card ids (rc- followed by 12 lowercase hex digits)")
	})
}

func TestAnswerOnSecretCard(t *testing.T) {
	t.Run("an ordinary answer closes the card as replied and admin agents may give it", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.openSecretCard(t, "k")
		status, data := apiJSON(t, f.h, "POST", "/api/reply-cards/"+cardID+"/answer", f.admin, `{"text":"which environment?"}`)
		if status != 200 {
			t.Fatalf("answer: %d %v", status, data)
		}
		apiWantValue(t, "secret", any(f.secretBlock(t, cardID)), any(map[string]any{
			"state": "replied", "provided_ts": nil, "retrieved_ts": nil, "expires_ts": nil,
		}))
		f.wantCiphertext(t, cardID, false)
	})

	t.Run("an option index is out of range on a card with no option list", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.openSecretCard(t, "k")
		status, data := apiJSON(t, f.h, "POST", "/api/reply-cards/"+cardID+"/answer", f.owner, `{"option_idxs":[0]}`)
		if status != 400 {
			t.Fatalf("status %d %v", status, data)
		}
		apiWantError(t, data, "validation_error", "option_idxs out of range")
	})

	t.Run("re-deciding works on replied and is refused once a value was provided", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		replied := f.openSecretCard(t, "k")
		apiJSON(t, f.h, "POST", "/api/reply-cards/"+replied+"/answer", f.owner, `{"text":"first"}`)
		status, data := apiJSON(t, f.h, "PUT", "/api/reply-cards/"+replied+"/answer", f.owner, `{"text":"second"}`)
		if status != 200 {
			t.Fatalf("reanswer: %d %v", status, data)
		}
		apiWantValue(t, "answer.text", data["answer"].(map[string]any)["text"], "second")

		provided := f.providedCard(t)
		status, data = apiJSON(t, f.h, "PUT", "/api/reply-cards/"+provided+"/answer", f.owner, `{"text":"changed"}`)
		if status != http.StatusConflict {
			t.Fatalf("reanswer on provided: %d %v", status, data)
		}
		apiWantError(t, data, "conflict", "reply card '"+provided+"' holds a value the owner provided — it has no answer to re-decide")
		status, data = apiJSON(t, f.h, "POST", "/api/reply-cards/"+provided+"/answer", f.owner, `{"text":"changed"}`)
		if status != http.StatusConflict {
			t.Fatalf("answer on provided: %d %v", status, data)
		}
		apiWantError(t, data, "conflict", "reply card '"+provided+"' already holds the value the owner provided — it takes no answer")
	})
}

func TestExpireSecretCard(t *testing.T) {
	expire := func(t *testing.T, f apiTestSecretFixture, token, id string) (int, map[string]any) {
		t.Helper()
		return apiJSON(t, f.h, "POST", "/api/reply-cards/"+id+"/expire", token, `{}`)
	}

	t.Run("a waiting card expires as any other and no value can follow", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.openSecretCard(t, "k")
		if status, data := expire(t, f, f.agent, cardID); status != 200 || data["status"] != "expired" {
			t.Fatalf("expire: %d %v", status, data)
		}
		apiWantValue(t, "secret", any(f.secretBlock(t, cardID)), any(map[string]any{
			"state": "expired", "provided_ts": nil, "retrieved_ts": nil, "expires_ts": nil,
		}))
		if status, _ := apiJSON(t, f.h, "POST", "/api/reply-cards/"+cardID+"/secret", f.owner, `{"value":"x"}`); status != http.StatusConflict {
			t.Fatalf("provide after expiry: %d", status)
		}
	})

	t.Run("a provided, unretrieved card loses its ciphertext and expires with the normal notice", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		dashboard := apiTestListen(t, f.api, "")
		author := apiTestListen(t, f.api, "kip")
		status, data := expire(t, f, f.owner, cardID)
		if status != 200 {
			t.Fatalf("expire: %d %v", status, data)
		}
		apiWantBody(t, data, map[string]any{
			"id": cardID, "status": "expired", "answered_ts": nil, "expired_ts": apiAnyNumber,
			"answer": nil, "task_id": "", "step_id": "",
		})
		want := apiTestSecretCardFrame(cardID, "kip", "expired", "owner")
		dashboard.wantFrames(want)
		author.wantFrames(want)
		f.wantCiphertext(t, cardID, false)
		apiWantValue(t, "secret", any(f.secretBlock(t, cardID)), any(map[string]any{
			"state": "expired", "provided_ts": apiAnyNumber, "retrieved_ts": nil, "expires_ts": nil,
		}))
		if status, _ := f.reveal(t, f.agent, cardID); status != http.StatusConflict {
			t.Fatalf("reveal after expiry: %d", status)
		}
	})

	t.Run("a retrieved card loses its ciphertext, stays answered, and only the owner is told", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		f.reveal(t, f.agent, cardID)
		dashboard := apiTestListen(t, f.api, "")
		author := apiTestListen(t, f.api, "kip")
		status, data := expire(t, f, f.agent, cardID)
		if status != 200 || data["status"] != "answered" {
			t.Fatalf("expire: %d %v", status, data)
		}
		dashboard.wantFrames(apiTestSecretCardFrame(cardID, "kip", "answered", "kip"))
		author.wantFrames()
		f.wantCiphertext(t, cardID, false)
		apiWantValue(t, "secret", any(f.secretBlock(t, cardID)), any(map[string]any{
			"state": "retrieved", "provided_ts": apiAnyNumber, "retrieved_ts": apiAnyNumber, "expires_ts": nil,
		}))
	})

	t.Run("every other state is the existing 409", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		destroyed := f.providedCard(t)
		f.reveal(t, f.agent, destroyed)
		if status, _ := expire(t, f, f.owner, destroyed); status != 200 {
			t.Fatalf("first expire: %d", status)
		}
		replied := f.openSecretCard(t, "k")
		apiJSON(t, f.h, "POST", "/api/reply-cards/"+replied+"/answer", f.owner, `{"text":"no"}`)
		expired := f.openSecretCard(t, "k")
		if status, _ := expire(t, f, f.owner, expired); status != 200 {
			t.Fatalf("expire waiting: %d", status)
		}
		plain := apiTestOpenReplyCard(t, f.h, f.agent, `{"kind":"decision","summary":"s","options":[{"text":"a"}],"linked_task":null}`)
		apiJSON(t, f.h, "POST", "/api/reply-cards/"+plain+"/answer", f.owner, `{"option_idxs":[0]}`)
		for id, status := range map[string]string{destroyed: "answered", replied: "answered", expired: "expired", plain: "answered"} {
			code, data := expire(t, f, f.owner, id)
			if code != http.StatusConflict {
				t.Fatalf("%s: %d %v", id, code, data)
			}
			apiWantError(t, data, "conflict", "reply card '"+id+"' is already "+status+" — only a waiting card can expire")
		}
	})

	t.Run("another member may not expire it", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := apiTestOpenReplyCard(t, f.h, f.admin, apiTestSecretCardBody("theirs"))
		f.provide(t, cardID, apiTestCardSecretValue)
		if status, _ := expire(t, f, f.agent, cardID); status != http.StatusForbidden {
			t.Fatalf("status %d, want 403", status)
		}
		f.wantCiphertext(t, cardID, true)
	})
}

func TestSweepSecretCards(t *testing.T) {
	t.Run("nothing is due: the value stays and nobody is told", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		dashboard := apiTestListen(t, f.api, "")
		n, err := f.api.sweepSecretCards(nowSecs(), "sweep")
		if err != nil || n != 0 {
			t.Fatalf("sweep: %d %v", n, err)
		}
		f.wantCiphertext(t, cardID, true)
		dashboard.wantFrames()
	})

	t.Run("an unretrieved value past its deadline expires the card with the normal notice", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		dashboard := apiTestListen(t, f.api, "")
		author := apiTestListen(t, f.api, "kip")
		n, err := f.api.sweepSecretCards(f.card(t, cardID).SecretDestroyAfterTS, "sweep")
		if err != nil || n != 1 {
			t.Fatalf("sweep: %d %v", n, err)
		}
		f.wantCiphertext(t, cardID, false)
		stored := f.card(t, cardID)
		if stored.Status != "expired" || stored.ExpiredTS <= 0 || stored.SecretDestroyAfterTS != 0 {
			t.Fatalf("stored %#v", stored)
		}
		want := apiTestSecretCardFrame(cardID, "kip", "expired", "sweep")
		dashboard.wantFrames(want)
		author.wantFrames(want)
	})

	t.Run("a retrieved value past its deadline is destroyed quietly, and the card stays answered", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		f.reveal(t, f.agent, cardID)
		dashboard := apiTestListen(t, f.api, "")
		author := apiTestListen(t, f.api, "kip")
		n, err := f.api.sweepSecretCards(f.card(t, cardID).SecretDestroyAfterTS+1, "sweep")
		if err != nil || n != 1 {
			t.Fatalf("sweep: %d %v", n, err)
		}
		f.wantCiphertext(t, cardID, false)
		dashboard.wantFrames(apiTestSecretCardFrame(cardID, "kip", "answered", "sweep"))
		author.wantFrames()
		apiWantValue(t, "secret", any(f.secretBlock(t, cardID)), any(map[string]any{
			"state": "retrieved", "provided_ts": apiAnyNumber, "retrieved_ts": apiAnyNumber, "expires_ts": nil,
		}))
	})

	t.Run("dismissing the author destroys the value at once", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		if status, data := apiJSON(t, f.h, "DELETE", "/api/members/kip", f.owner, ""); status != 200 {
			t.Fatalf("dismiss: %d %v", status, data)
		}
		f.wantCiphertext(t, cardID, false)
		if got := f.card(t, cardID).Status; got != "expired" {
			t.Fatalf("status %q, want expired", got)
		}
	})

	t.Run("a removed author is also caught by the sweep alone", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		m, err := f.d.GetMember("kip")
		if err != nil || m == nil {
			t.Fatal(err)
		}
		m.RosterStatus = RosterStatusRemoved
		if err := f.d.putMemberWholeRowForTest(*m); err != nil {
			t.Fatal(err)
		}
		if n, err := f.api.sweepSecretCards(nowSecs(), "sweep"); err != nil || n != 1 {
			t.Fatalf("sweep: %d %v", n, err)
		}
		f.wantCiphertext(t, cardID, false)
	})

	t.Run("closing the task destroys the value at once, retrieved or not", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		executor, _ := apiTestTwoStepTask(t, f.api, f.h, f.owner)
		open := func() string {
			id := apiTestOpenReplyCard(t, f.h, executor, `{"kind":"secret","summary":"k","options":[],"linked_task":null,"about_task_id":"T-1"}`)
			f.provide(t, id, apiTestCardSecretValue)
			return id
		}
		unretrieved, retrieved := open(), open()
		if status, data := f.reveal(t, executor, retrieved); status != 200 {
			t.Fatalf("reveal: %d %v", status, data)
		}
		if status, data := apiJSON(t, f.h, "POST", "/api/tasks/T-1/mark-terminated", f.owner, `{}`); status != 200 {
			t.Fatalf("terminate: %d %v", status, data)
		}
		f.wantCiphertext(t, unretrieved, false)
		f.wantCiphertext(t, retrieved, false)
		if got := f.card(t, unretrieved).Status; got != "expired" {
			t.Fatalf("unretrieved status %q", got)
		}
		if got := f.card(t, retrieved).Status; got != "answered" {
			t.Fatalf("retrieved status %q", got)
		}
	})

	for _, tc := range []struct {
		name string
		end  func(t *testing.T, f apiTestSecretFixture)
	}{
		{"closing its task", func(t *testing.T, f apiTestSecretFixture) {
			task, err := f.api.resolveTask("T-1")
			if err != nil {
				t.Fatalf("resolveTask: %v", err)
			}
			if err := apiTestCloseTask(f.api, task, TaskStatusDone, nowSecs(), "owner"); err != nil {
				t.Fatalf("closeTask: %v", err)
			}
		}},
		{"releasing it alone", func(t *testing.T, f apiTestSecretFixture) {
			f.api.dismissOutsourceWorkerByID("ow-abc123", nowSecs(), "owner")
		}},
	} {
		t.Run("a worker's unbound card loses its value on "+tc.name, func(t *testing.T) {
			f := newAPITestSecretFixture(t)
			apiJSON(t, f.h, "POST", "/api/tasks", f.owner, `{"title":"Ship it","executor_member_id":"kip"}`)
			if err := f.d.PutOutsourceWorker(OutsourceWorker{
				ID: "ow-abc123", Codename: "Contractor", TaskID: "T-1",
				Status: WorkerStatusAssigned, Runtime: "claude", Model: "sonnet", Effort: "medium",
			}); err != nil {
				t.Fatalf("PutOutsourceWorker: %v", err)
			}
			cardID := apiTestOpenReplyCard(t, f.h, apiTestAgentToken(t, f.api, "ow-abc123", ""), apiTestSecretCardBody("k"))
			f.provide(t, cardID, apiTestCardSecretValue)
			f.wantCiphertext(t, cardID, true)

			tc.end(t, f)

			f.wantCiphertext(t, cardID, false)
			if got := f.card(t, cardID).Status; got != "expired" {
				t.Fatalf("status %q, want expired", got)
			}
		})
	}

	t.Run("a task gone from the database counts as closed", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		c := f.card(t, cardID)
		c.TaskID = "T-gone"
		if err := f.d.PutReplyCard(c); err != nil {
			t.Fatal(err)
		}
		if n, err := f.api.sweepSecretCards(nowSecs(), "sweep"); err != nil || n != 1 {
			t.Fatalf("sweep: %d %v", n, err)
		}
		f.wantCiphertext(t, cardID, false)
	})

	t.Run("the tick runs the same sweep", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		cardID := f.providedCard(t)
		f.api.runSecretCardSweepTick(f.card(t, cardID).SecretDestroyAfterTS + 1)
		f.wantCiphertext(t, cardID, false)
	})
}

func TestSecretCardSettings(t *testing.T) {
	t.Run("defaults and ranges", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		_, got := apiJSON(t, f.h, "GET", "/api/settings", f.owner, "")
		apiWantValue(t, "ttl", got["secret_card_unretrieved_ttl_secs"], 86400)
		apiWantValue(t, "retention", got["secret_card_retrieved_retention_secs"], 600)
		for _, tc := range []struct {
			field   string
			value   string
			status  int
			message string
		}{
			{"secret_card_unretrieved_ttl_secs", "59", 422, "secret_card_unretrieved_ttl_secs must be between 60 and 2592000 seconds"},
			{"secret_card_unretrieved_ttl_secs", "2592001", 422, "secret_card_unretrieved_ttl_secs must be between 60 and 2592000 seconds"},
			{"secret_card_unretrieved_ttl_secs", "60", 200, ""},
			{"secret_card_unretrieved_ttl_secs", "2592000", 200, ""},
			{"secret_card_retrieved_retention_secs", "-1", 422, "secret_card_retrieved_retention_secs must be between 0 and 86400 seconds"},
			{"secret_card_retrieved_retention_secs", "86401", 422, "secret_card_retrieved_retention_secs must be between 0 and 86400 seconds"},
			{"secret_card_retrieved_retention_secs", "0", 200, ""},
			{"secret_card_retrieved_retention_secs", "86400", 200, ""},
		} {
			status, data := apiJSON(t, f.h, "PATCH", "/api/settings", f.owner, `{"`+tc.field+`":`+tc.value+`}`)
			if status != tc.status {
				t.Fatalf("%s=%s: %d %v", tc.field, tc.value, status, data)
			}
			if tc.status == 200 {
				if n, _ := data[tc.field].(float64); strconv.FormatFloat(n, 'f', -1, 64) != tc.value {
					t.Fatalf("%s=%s answered %v", tc.field, tc.value, data[tc.field])
				}
				continue
			}
			apiWantError(t, data, "validation_error", tc.message)
		}
	})

	t.Run("a saved value survives a reload of the settings", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		apiJSON(t, f.h, "PATCH", "/api/settings", f.owner, `{"secret_card_unretrieved_ttl_secs":120,"secret_card_retrieved_retention_secs":30}`)
		auth, err := loadAuthSettings(f.d, defaultConfig(), func(string) {})
		if err != nil {
			t.Fatalf("loadAuthSettings: %v", err)
		}
		if auth.secretCardUnretrievedTTLSecs != 120 || auth.secretCardRetrievedRetentionSecs != 30 {
			t.Fatalf("reloaded ttl %d retention %d", auth.secretCardUnretrievedTTLSecs, auth.secretCardRetrievedRetentionSecs)
		}
	})

	t.Run("a change affects later events only", func(t *testing.T) {
		f := newAPITestSecretFixture(t)
		earlier := f.providedCard(t)
		deadline := f.card(t, earlier).SecretDestroyAfterTS
		apiJSON(t, f.h, "PATCH", "/api/settings", f.owner, `{"secret_card_unretrieved_ttl_secs":60,"secret_card_retrieved_retention_secs":30}`)
		if got := f.card(t, earlier).SecretDestroyAfterTS; got != deadline {
			t.Fatalf("existing deadline moved: %v -> %v", deadline, got)
		}
		later := f.providedCard(t)
		stored := f.card(t, later)
		if stored.SecretDestroyAfterTS != stored.SecretProvidedTS+60 {
			t.Fatalf("later provide used %v", stored.SecretDestroyAfterTS-stored.SecretProvidedTS)
		}
		f.reveal(t, f.agent, earlier)
		retrieved := f.card(t, earlier)
		if retrieved.SecretDestroyAfterTS != retrieved.SecretRetrievedTS+30 {
			t.Fatalf("retrieval used %v", retrieved.SecretDestroyAfterTS-retrieved.SecretRetrievedTS)
		}
	})
}
