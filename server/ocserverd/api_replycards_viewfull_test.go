package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// T-a3e4 node 8: GET /api/reply-cards?view=full.
//
// The cockpit's panes and its inline chat cards render the WHOLE card, so the
// T-3f31 light list forced the http adapter into one GET /api/reply-cards/{id}
// PER ROW. Opening one waiting pane therefore cost one round trip per waiting
// card. `view=full` serves the same pane in one request.
//
// 🔴 What these tests are FOR, and why each one is not a tautology:
//
//   - The value being bought is ROUND TRIPS, not bytes — a full pane is very
//     nearly the same size either way. So there is nothing here that asserts a
//     size; the request-count assertions live on the client side (the adapter
//     is what used to fan out), in frontend/src/api/http.view-full.test.ts.
//     What the SERVER has to guarantee is that one full row is worth exactly as
//     much as the per-row GET it replaces — otherwise collapsing the fan-out
//     would silently change what the cockpit renders. Hence the byte-identity
//     test below: it compares each row against that card's OWN
//     GET /api/reply-cards/{card_id} response.
//   - Every test first proves its corpus can tell the two projections apart
//     (the light row genuinely lacks body/options). Without that, "the full row
//     has a body" would be satisfied by a fixture whose body is "" anyway.
//   - The default-unchanged test is the owner's red line: `view` is additive,
//     and a caller that does not send it must get the byte-for-byte historical
//     response.

// viewFullCorpus seeds three cards whose FULL shape is distinguishable from
// their light shape: a non-empty body and a real option list (the two things
// T-3f31 took OUT of the list wire).
//
// 🔴 One of them is TASK-BOUND, and that is load-bearing, not decoration. The
// full row must be built by the same replyCardDTOOf the single-card GET uses,
// and the only thing that function adds over the bare newReplyCardDTO is the
// task-reference join. With an all-unbound corpus the two builders produce
// identical bytes, so swapping one for the other reddened NOTHING — measured,
// not guessed: that mutant was green across this whole file until this card was
// added. A byte-identity test whose corpus cannot exercise the join is testing
// air.
func viewFullCorpus(t *testing.T, s *apiServer) (waiting []string) {
	t.Helper()
	now := nowSecs()
	if err := s.dal.PutTask(Task{
		ID: "t-vf", Title: "the bound task", Status: TaskStatusInProgress,
		Priority: "mid", TypeKey: "review-pr",
		ExecutorKind: TaskExecutorStaff, ExecutorID: "m-a",
		CreatedTS: now, UpdatedTS: now,
	}); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	// Two waiting cards, deliberately created out of order so the pane's
	// longest-waiting-first ordering is observable under either projection.
	// The older one is the task-bound card (see the note above).
	older := waitingCard("rc-vf-older", now-900)
	older.Body = "the long ask body that the light row does not carry"
	older.Options = []ReplyCardOption{{Text: "照做", AIPick: true}, {Text: "先等等"}, {Text: "不要"}}
	older.TaskID = "t-vf"
	newer := waitingCard("rc-vf-newer", now-10)
	newer.Body = "another body"
	newer.Options = []ReplyCardOption{{Text: "好", AIPick: true}, {Text: "不要"}}
	// An answered card, so the answered pane is exercised too: the light row
	// carries a DIGEST (attachment COUNT), the full row carries real refs —
	// the two shapes are not merely longer/shorter, they disagree on a type.
	done := answeredCard("rc-vf-done", now-800, now-30)
	done.Body = "answered body"
	done.Options = []ReplyCardOption{{Text: "是", AIPick: true}, {Text: "否"}}
	done.AnswerOptionIdxs = []int{0}
	done.AnswerText = "picked the first one"
	for _, c := range []ReplyCard{older, newer, done} {
		if err := s.dal.PutReplyCard(c); err != nil {
			t.Fatalf("seed %s: %v", c.ID, err)
		}
	}
	return []string{"rc-vf-older", "rc-vf-newer"}
}

func listReplyCards(t *testing.T, s *apiServer, status, view string) *httptest.ResponseRecorder {
	t.Helper()
	params := HandleListReplyCardsApiReplyCardsGetParams{}
	url := "/api/reply-cards?"
	if status != "" {
		params.Status = &status
		url += "status=" + status + "&"
	}
	if view != "" {
		params.View = &view
		url += "view=" + view
	}
	rec := httptest.NewRecorder()
	s.HandleListReplyCardsApiReplyCardsGet(rec, httptest.NewRequest("GET", url, nil), params)
	return rec
}

func compact(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		t.Fatalf("compact %s: %v", raw, err)
	}
	return buf.Bytes()
}

// Each ?view=full row must equal that card's own get_reply_card response, byte
// for byte. This is the promise that makes collapsing the per-row fan-out safe:
// the client is not getting a THIRD shape it has to special-case, it is getting
// the same object it used to fetch one at a time.
func TestListReplyCardsViewFullRowsEqualTheSingleCardResponse(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	viewFullCorpus(t, s)

	for _, status := range []string{replyCardStatusWaiting, replyCardStatusAnswered} {
		rec := listReplyCards(t, s, status, replyCardViewFull)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s view=full: %d %s", status, rec.Code, rec.Body.String())
		}
		rows := decodeBody[[]json.RawMessage](t, rec)
		if len(rows) == 0 {
			t.Fatalf("%s pane is empty — the corpus proves nothing", status)
		}
		// Corpus self-check for the waiting pane: the task join must actually
		// have something to resolve, or byte-identity below cannot distinguish
		// replyCardDTOOf from the bare newReplyCardDTO (see viewFullCorpus).
		if status == replyCardStatusWaiting {
			bound := 0
			for _, row := range rows {
				var probe struct {
					Task *struct{ ID string } `json:"task"`
				}
				if err := json.Unmarshal(row, &probe); err != nil {
					t.Fatalf("probe: %v", err)
				}
				if probe.Task != nil && probe.Task.ID != "" {
					bound++
				}
			}
			if bound == 0 {
				t.Fatal("no row resolved a task reference — the join is not being " +
					"exercised, so byte-identity here would pass on a builder that " +
					"drops it")
			}
		}
		for i, row := range rows {
			var ident struct{ ID string }
			if err := json.Unmarshal(row, &ident); err != nil {
				t.Fatalf("row %d: %v", i, err)
			}
			single := httptest.NewRecorder()
			s.HandleGetReplyCardApiReplyCardsCardIdGet(single,
				httptest.NewRequest("GET", "/api/reply-cards/"+ident.ID, nil), ident.ID)
			if single.Code != http.StatusOK {
				t.Fatalf("get %s: %d", ident.ID, single.Code)
			}
			want := compact(t, single.Body.Bytes())
			got := compact(t, row)
			if !bytes.Equal(got, want) {
				t.Fatalf("%s row %d (%s) is not byte-identical to its own get_reply_card:\n"+
					" list:   %s\n single: %s", status, i, ident.ID, got, want)
			}
		}
	}
}

// The projection has to actually differ, and in the direction claimed: the full
// row carries body + the full option list, the light row carries neither. If
// this ever went green with both shapes equal, the byte-identity test above
// would be measuring nothing.
func TestListReplyCardsLightRowStillWithholdsWhatFullCarries(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	viewFullCorpus(t, s)

	light := listReplyCards(t, s, replyCardStatusWaiting, "")
	if light.Code != http.StatusOK {
		t.Fatalf("light: %d %s", light.Code, light.Body.String())
	}
	// Decode the light pane as raw objects: the point is which KEYS are on the
	// wire, and decoding into the DTO would silently drop unexpected ones.
	lightRows := decodeBody[[]map[string]json.RawMessage](t, light)
	if len(lightRows) == 0 {
		t.Fatal("light waiting pane is empty — nothing is being compared")
	}
	for _, row := range lightRows {
		for _, forbidden := range []string{"body", "options", "chat_message_id", "attachments"} {
			if _, present := row[forbidden]; present {
				t.Fatalf("the light row must not carry %q (T-3f31): %v", forbidden, row)
			}
		}
	}

	full := listReplyCards(t, s, replyCardStatusWaiting, replyCardViewFull)
	fullRows := decodeBody[[]replyCardDTO](t, full)
	if len(fullRows) != len(lightRows) {
		t.Fatalf("the two projections must describe the SAME pane: light %d, full %d",
			len(lightRows), len(fullRows))
	}
	for _, row := range fullRows {
		if row.Body == "" {
			t.Fatalf("full row %s lost its body — the corpus cannot tell the "+
				"projections apart, so every other assertion here is hollow", row.ID)
		}
		if len(row.Options) < 2 {
			t.Fatalf("full row %s lost its option list: %+v", row.ID, row.Options)
		}
	}
}

// The pane is the SAME pane: same rows, same order, and ?limit= still caps
// AFTER the ordering. A projection that reordered or re-selected rows would be
// a different endpoint wearing the same name.
func TestListReplyCardsViewFullKeepsThePaneOrderAndLimit(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	want := viewFullCorpus(t, s) // longest-waiting first

	full := listReplyCards(t, s, replyCardStatusWaiting, replyCardViewFull)
	rows := decodeBody[[]replyCardDTO](t, full)
	if len(rows) != len(want) {
		t.Fatalf("waiting pane: want %d rows, got %d", len(want), len(rows))
	}
	for i, id := range want {
		if rows[i].ID != id {
			t.Fatalf("view=full must preserve the pane's order: want %v, got row %d = %s",
				want, i, rows[i].ID)
		}
	}

	one := 1
	rec := httptest.NewRecorder()
	view := replyCardViewFull
	s.HandleListReplyCardsApiReplyCardsGet(rec,
		httptest.NewRequest("GET", "/api/reply-cards?view=full&limit=1", nil),
		HandleListReplyCardsApiReplyCardsGetParams{Limit: &one, View: &view})
	capped := decodeBody[[]replyCardDTO](t, rec)
	if len(capped) != 1 || capped[0].ID != want[0] {
		t.Fatalf("limit must cap AFTER the ordering (the pane's first N survive): %+v", capped)
	}
}

// 🔴 The owner's red line: `view` is OPTIONAL and the default is unchanged. A
// caller that does not send it — every client that existed before this change,
// including the list_reply_cards MCP tool, which cannot send it at all — gets
// the historical response.
func TestListReplyCardsDefaultProjectionIsUnchanged(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	viewFullCorpus(t, s)

	for _, status := range []string{
		replyCardStatusWaiting, replyCardStatusAnswered, replyCardStatusExpired,
	} {
		absent := listReplyCards(t, s, status, "")
		explicit := listReplyCards(t, s, status, replyCardViewLight)
		if absent.Code != http.StatusOK || explicit.Code != http.StatusOK {
			t.Fatalf("%s: absent %d, view=light %d", status, absent.Code, explicit.Code)
		}
		if !bytes.Equal(absent.Body.Bytes(), explicit.Body.Bytes()) {
			t.Fatalf("%s: ?view=light must be the DEFAULT, byte for byte:\n"+
				" absent: %s\n light:  %s", status, absent.Body.String(), explicit.Body.String())
		}
		// And it is still the light DTO, not the full one: decoding the rows
		// with DisallowUnknownFields against the light DTO would pass for a
		// full row too (it is a superset by key name), so assert the KEY that
		// only the full shape has is absent.
		rows := decodeBody[[]map[string]json.RawMessage](t, absent)
		for _, row := range rows {
			if _, present := row["body"]; present {
				t.Fatalf("%s: the default projection must stay LIGHT: %v", status, row)
			}
		}
	}

	// An empty pane is still an empty ARRAY, not null — unchanged too.
	if got := listReplyCards(t, s, replyCardStatusExpired, "").Body.String(); got != "[]\n" &&
		got != "[]" {
		t.Fatalf("an empty pane must serialise as [], got %q", got)
	}
}

// An unrecognised `view` is a 400 that names both accepted values. Falling back
// to light would restore the per-row fan-out with NO signal — a typo would look
// like a working light query, which is the exact cost this parameter removes.
func TestListReplyCardsRejectsAnUnknownView(t *testing.T) {
	s := &apiServer{dal: newTestDAL(t), hub: NewHub()}
	viewFullCorpus(t, s)

	for _, bad := range []string{"Full", "FULL", "list", "complete", "1"} {
		rec := listReplyCards(t, s, replyCardStatusWaiting, bad)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("view=%q must 400 (silently serving light hides a typo), got %d %s",
				bad, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, name := range []string{"light", "full"} {
			if !bytes.Contains([]byte(body), []byte(name)) {
				t.Fatalf("view=%q refusal must name %q so the caller can fix it: %s",
					bad, name, body)
			}
		}
	}

	// Positive control: the two accepted values, and absence, are NOT refused —
	// otherwise the loop above would pass on a handler that rejects everything.
	for _, ok := range []string{"", replyCardViewLight, replyCardViewFull} {
		if rec := listReplyCards(t, s, replyCardStatusWaiting, ok); rec.Code != http.StatusOK {
			t.Fatalf("view=%q must be accepted, got %d %s", ok, rec.Code, rec.Body.String())
		}
	}
}
