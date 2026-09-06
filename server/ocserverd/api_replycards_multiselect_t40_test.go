package main

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/pressly/goose/v3"
)

// openUnboundCard mints one plain 請示 with the given options and select_mode
// (empty = let the server default it) and returns the served card.
func openUnboundCard(t *testing.T, api *apiServer, selectMode string,
	options []map[string]any) replyCardDTO {
	t.Helper()
	body := map[string]any{
		"kind": "decision", "summary": "which way?",
		"options": options, "linked_task": nil,
	}
	if selectMode != "" {
		body["select_mode"] = selectMode
	}
	rec := createCardRaw(t, api, "m-exec", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("open card: %d %s", rec.Code, rec.Body.String())
	}
	// T-91: create_reply_card answers a receipt, so the card comes back through
	// get_reply_card. That is the right read for these cases anyway — every
	// claim below is about what the server STORED and serves (the resolved
	// select_mode, the per-option ai_pick flags), never about the create echo.
	return createdCardView(t, api, rec)
}

func threeOptions() []map[string]any {
	return []map[string]any{{"text": "A"}, {"text": "B"}, {"text": "C"}}
}

// An EMPTY option list is not an answer. This is its own test because the guard
// it protects changed shape: it used to compare a *int against nil, where
// "absent" was the only way to carry no option. Against a LIST, `[]` decodes to
// a non-nil empty slice, so a nil check passes it — and a card would close, and
// a held task would resume, on a decision the owner never made.
func TestAnswerReplyCardRejectsAnEmptyOptionIdxsList(t *testing.T) {
	api := newTasksTestServer(t)
	card := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())

	rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": []int{}})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an empty option_idxs list must be refused, got %d %s",
			rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != "answer must carry an option, text, or an attachment" {
		t.Fatalf("refusal message: %q", msg)
	}
	stored, err := api.dal.GetReplyCard(card.ID)
	if err != nil || stored == nil {
		t.Fatalf("reread card: %v %v", stored, err)
	}
	if stored.Status != replyCardStatusWaiting {
		t.Fatalf("a refused answer must leave the card waiting, got %q", stored.Status)
	}
	if stored.AnswerOptionIdxs != nil || stored.AnsweredTS != 0 {
		t.Fatalf("a refused answer must store nothing: %+v", stored)
	}
}

// The owner's CLICK ORDER is not part of the decision: [2,0] and [0,2] say the
// same thing and must land in the database as the same bytes. A reader that
// could tell them apart once read a re-ordered re-answer as a CHANGED one and
// swallowed a delivery.
func TestAnswerReplyCardStoresOptionIdxsDedupedAndAscending(t *testing.T) {
	api := newTasksTestServer(t)
	descending := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())
	ascending := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())

	if rec := answerCard(t, api, descending.ID,
		map[string]any{"option_idxs": []int{2, 0}}); rec.Code != http.StatusOK {
		t.Fatalf("answer [2,0]: %d %s", rec.Code, rec.Body.String())
	}
	if rec := answerCard(t, api, ascending.ID,
		map[string]any{"option_idxs": []int{0, 2}}); rec.Code != http.StatusOK {
		t.Fatalf("answer [0,2]: %d %s", rec.Code, rec.Body.String())
	}

	a, err := api.dal.GetReplyCard(descending.ID)
	if err != nil {
		t.Fatalf("reread [2,0] card: %v", err)
	}
	b, err := api.dal.GetReplyCard(ascending.ID)
	if err != nil {
		t.Fatalf("reread [0,2] card: %v", err)
	}
	if !reflect.DeepEqual(a.AnswerOptionIdxs, []int{0, 2}) {
		t.Fatalf("[2,0] must store as [0 2], got %v", a.AnswerOptionIdxs)
	}
	if !reflect.DeepEqual(a.AnswerOptionIdxs, b.AnswerOptionIdxs) {
		t.Fatalf("[2,0] and [0,2] must store identically, got %v and %v",
			a.AnswerOptionIdxs, b.AnswerOptionIdxs)
	}

	dup := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())
	if rec := answerCard(t, api, dup.ID,
		map[string]any{"option_idxs": []int{1, 1, 0}}); rec.Code != http.StatusOK {
		t.Fatalf("answer [1,1,0]: %d %s", rec.Code, rec.Body.String())
	}
	stored, err := api.dal.GetReplyCard(dup.ID)
	if err != nil {
		t.Fatalf("reread dup card: %v", err)
	}
	if !reflect.DeepEqual(stored.AnswerOptionIdxs, []int{0, 1}) {
		t.Fatalf("duplicates must collapse, got %v", stored.AnswerOptionIdxs)
	}
}

// A single-select card accepts ONE circled option. Silently keeping the first of
// two would record a decision the owner did not make, and the card would look
// perfectly well-formed afterwards.
func TestAnswerReplyCardRejectsTwoIndicesOnASingleSelectCard(t *testing.T) {
	api := newTasksTestServer(t)
	card := openUnboundCard(t, api, replyCardSelectModeSingle, threeOptions())

	rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": []int{0, 2}})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("two indices on a single-select card must be refused, got %d %s",
			rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != "this card is single-select: option_idxs may carry at most one index" {
		t.Fatalf("refusal message: %q", msg)
	}
	stored, err := api.dal.GetReplyCard(card.ID)
	if err != nil || stored == nil {
		t.Fatalf("reread card: %v %v", stored, err)
	}
	if stored.Status != replyCardStatusWaiting || stored.AnswerOptionIdxs != nil {
		t.Fatalf("a refused answer must store nothing: %+v", stored)
	}

	// The same card takes ONE index.
	if rec := answerCard(t, api, card.ID,
		map[string]any{"option_idxs": []int{2}}); rec.Code != http.StatusOK {
		t.Fatalf("one index must be accepted: %d %s", rec.Code, rec.Body.String())
	}
	stored, _ = api.dal.GetReplyCard(card.ID)
	if !reflect.DeepEqual(stored.AnswerOptionIdxs, []int{2}) {
		t.Fatalf("single-select answer: %v", stored.AnswerOptionIdxs)
	}

	// A MULTI card of the same shape takes both — proving the refusal above is
	// the select_mode gate and not a blanket ban on two indices.
	multi := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())
	if rec := answerCard(t, api, multi.ID,
		map[string]any{"option_idxs": []int{0, 2}}); rec.Code != http.StatusOK {
		t.Fatalf("two indices on a multi card must be accepted: %d %s",
			rec.Code, rec.Body.String())
	}
}

func TestAnswerReplyCardRejectsAnOutOfRangeIndex(t *testing.T) {
	api := newTasksTestServer(t)
	card := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())

	for _, idxs := range [][]int{{3}, {-1}, {0, 3}} {
		rec := answerCard(t, api, card.ID, map[string]any{"option_idxs": idxs})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%v must be refused, got %d %s", idxs, rec.Code, rec.Body.String())
		}
		if msg := errorMessageOf(t, rec); msg != "option_idxs out of range" {
			t.Fatalf("%v refusal message: %q", idxs, msg)
		}
	}
	stored, _ := api.dal.GetReplyCard(card.ID)
	if stored.Status != replyCardStatusWaiting {
		t.Fatalf("a refused answer must leave the card waiting, got %q", stored.Status)
	}
}

// ai_pick is now a per-option flag, so "which one does the AI suggest" is a
// question the card answers by itself. A single-select card may answer it at
// most once: two recommendations on a card that accepts one choice is a question
// with no honest reading.
func TestCreateReplyCardEnforcesTheAiPickBudget(t *testing.T) {
	api := newTasksTestServer(t)
	two := []map[string]any{{"text": "A", "ai_pick": true}, {"text": "B", "ai_pick": true}}

	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind": "decision", "summary": "which way?", "options": two,
		"select_mode": replyCardSelectModeSingle, "linked_task": nil,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("two ai_picks on a single-select card must be refused, got %d %s",
			rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != "a single-select card may mark at most one option ai_pick" {
		t.Fatalf("refusal message: %q", msg)
	}

	multi := openUnboundCard(t, api, replyCardSelectModeMulti, two)
	if !reflect.DeepEqual(multi.Options, []ReplyCardOption{
		{Text: "A", AIPick: true}, {Text: "B", AIPick: true}}) {
		t.Fatalf("a multi card keeps both ai_picks: %+v", multi.Options)
	}
}

// select_mode defaults to single, is served back on every card, and is a closed
// set at the door (a 400, not the decoder's 422 — the same posture kind has).
func TestCreateReplyCardSelectMode(t *testing.T) {
	api := newTasksTestServer(t)

	defaulted := openUnboundCard(t, api, "", threeOptions())
	if defaulted.SelectMode != replyCardSelectModeSingle {
		t.Fatalf("an omitted select_mode must default to single, got %q", defaulted.SelectMode)
	}
	stored, _ := api.dal.GetReplyCard(defaulted.ID)
	if stored.SelectMode != replyCardSelectModeSingle {
		t.Fatalf("the default must be persisted, got %q", stored.SelectMode)
	}

	asked := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())
	if asked.SelectMode != replyCardSelectModeMulti {
		t.Fatalf("select_mode=multi must be kept, got %q", asked.SelectMode)
	}

	rec := createCardRaw(t, api, "m-exec", map[string]any{
		"kind": "decision", "summary": "which way?", "options": threeOptions(),
		"select_mode": "many", "linked_task": nil,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown select_mode must be a 400, got %d %s", rec.Code, rec.Body.String())
	}
	if msg := errorMessageOf(t, rec); msg != "select_mode must be 'single' or 'multi'" {
		t.Fatalf("refusal message: %q", msg)
	}
}

// The light list row is the agent-facing contract, so a multi-select answer must
// show EVERY circled option there. Reporting only the first would tell the asker
// the owner chose less than it did, with nothing malformed to notice.
func TestReplyCardListItemDigestCarriesEveryCircledOption(t *testing.T) {
	api := newTasksTestServer(t)
	card := openUnboundCard(t, api, replyCardSelectModeMulti, threeOptions())
	if rec := answerCard(t, api, card.ID,
		map[string]any{"option_idxs": []int{2, 0}}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	stored, _ := api.dal.GetReplyCard(card.ID)

	row, err := api.replyCardListItemOf(*stored)
	if err != nil {
		t.Fatalf("list item: %v", err)
	}
	if row.Answer == nil {
		t.Fatal("an answered row must carry the digest")
	}
	if !reflect.DeepEqual(row.Answer.OptionIdxs, []int{0, 2}) {
		t.Fatalf("digest indices: %v", row.Answer.OptionIdxs)
	}
	if !reflect.DeepEqual(row.Answer.Options, []string{"A", "C"}) {
		t.Fatalf("digest must carry every circled option's wording, got %v", row.Answer.Options)
	}
}

// ── the 00065 rebuild, run for real ─────────────────────────────────────────
//
// The 00065 rebuild is where the "options[0] is the AI pick" convention is
// cashed in — the one and only time it is ever executed, against real owner
// data, with a Down that CANNOT undo it (it is lossy by construction). That
// makes the shipped SQL the highest-risk code in this change, so this test
// drives the SHIPPED FILE: it walks a temp database back to the version before
// 00065, seeds rows in the OLD schema, and runs `goose up` through 00065 out of
// the same embedded migrations/ the server ships. A mutant in
// 00065_reply_card_multi_select.sql turns this red.
//
// (An earlier version of this test hand-copied the Up expressions into Go
// helpers and seeded through them. It asserted against its own copy, so the
// shipped file was free to say anything at all.)
const (
	migration00065Version      = 65
	migration00065PriorVersion = 64
)

// migration00065LegacyRow is one reply_card row in the PRE-00065 schema:
// options is a JSON array of bare strings and answer_option_idx is one INTEGER
// or NULL.
type migration00065LegacyRow struct {
	id        string
	options   string
	answerIdx any
	why       string
}

// migration00065Fixture. ai_pick must land on index 0 and NOWHERE else, so the
// options fixtures differ in LENGTH and in wording — a rebuild that marked the
// wrong index, marked all of them, or marked none is visible rather than merely
// countable. The answer indices cover NULL, 0 (the falsy one a `WHEN idx THEN`
// style guard would drop) and a non-zero index.
func migration00065Fixture() []migration00065LegacyRow {
	return []migration00065LegacyRow{
		{"rc-legacy-answered", `["甲","乙"]`, 1, "answered on the NON-AI option"},
		{"rc-legacy-answered-zero", `["甲","乙","丙"]`, 0, "answered on index 0 — a falsy index is still an answer"},
		{"rc-legacy-unanswered", `["甲","乙"]`, nil, "never answered — NULL must stay NULL"},
		{"rc-legacy-nooptions", `[]`, nil, "no options at all — stays []"},
		{"rc-legacy-oneoption", `["只有一個"]`, nil, "a lone option IS the AI pick"},
	}
}

// migration00065World brings a temp database to the version just before 00065
// and seeds the legacy fixture into the OLD reply_card schema.
func migration00065World(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openSQLite(filepath.Join(t.TempDir(), "reply-card-multi.db"))
	if err != nil {
		t.Fatalf("open temp sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := runMigrations(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}
	if err := goose.DownTo(db, "migrations", migration00065PriorVersion); err != nil {
		t.Fatalf("down to %d: %v", migration00065PriorVersion, err)
	}
	// The seeds below are written in the OLD schema, so prove we are actually
	// standing in it — otherwise a `goose down` that quietly did nothing would
	// leave this test seeding and reading the same post-00065 shape.
	var legacyCols int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('reply_card')
		   WHERE name IN ('answer_option_idx')`).Scan(&legacyCols); err != nil {
		t.Fatalf("read pre-00065 columns: %v", err)
	}
	if legacyCols != 1 {
		t.Fatalf("expected the PRE-00065 reply_card (answer_option_idx present), got %d", legacyCols)
	}
	for _, r := range migration00065Fixture() {
		if _, err := db.Exec(`INSERT INTO reply_card
			(id, kind, status, created_ts, options, answer_option_idx, summary)
			VALUES (?, 'decision', 'waiting', 1, ?, ?, 's')`,
			r.id, r.options, r.answerIdx); err != nil {
			t.Fatalf("seed %s: %v", r.id, err)
		}
	}
	// ANTI-VACUITY: assertions over an empty table are indistinguishable from a
	// working migration.
	var seeded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reply_card`).Scan(&seeded); err != nil {
		t.Fatalf("count seeded rows: %v", err)
	}
	if seeded != len(migration00065Fixture()) {
		t.Fatalf("seeded %d rows, wrote %d — the fixture did not land",
			seeded, len(migration00065Fixture()))
	}
	return db
}

// TestReplyCardMultiSelectMigrationCarriesLegacyRowsForward runs the shipped
// 00065 Up over legacy rows and reads the result back through the DAL — the
// same path the server reads cards on.
func TestReplyCardMultiSelectMigrationCarriesLegacyRowsForward(t *testing.T) {
	db := migration00065World(t)
	if err := goose.UpTo(db, "migrations", migration00065Version); err != nil {
		t.Fatalf("goose up through %d: %v", migration00065Version, err)
	}
	dal := NewDAL(db)

	// Worked out by hand from what 00065 CLAIMS, never derived from its SQL.
	want := map[string]struct {
		options []ReplyCardOption
		idxs    []int
	}{
		"rc-legacy-answered": {
			[]ReplyCardOption{{Text: "甲", AIPick: true}, {Text: "乙"}}, []int{1}},
		"rc-legacy-answered-zero": {
			[]ReplyCardOption{{Text: "甲", AIPick: true}, {Text: "乙"}, {Text: "丙"}}, []int{0}},
		"rc-legacy-unanswered": {
			[]ReplyCardOption{{Text: "甲", AIPick: true}, {Text: "乙"}}, nil},
		"rc-legacy-nooptions": {nil, nil},
		"rc-legacy-oneoption": {
			[]ReplyCardOption{{Text: "只有一個", AIPick: true}}, nil},
	}
	for _, r := range migration00065Fixture() {
		card, err := dal.GetReplyCard(r.id)
		if err != nil {
			t.Fatalf("get %s: %v", r.id, err)
		}
		w := want[r.id]
		if len(card.Options) != len(w.options) ||
			(len(w.options) > 0 && !reflect.DeepEqual(card.Options, w.options)) {
			t.Errorf("%s (%s): options carried forward wrong.\n got: %+v\nwant: %+v\n\n"+
				"00065 must put ai_pick on the FIRST option and on no other — that is the "+
				"old positional convention being cashed in, and it is unrepeatable.",
				r.id, r.why, card.Options, w.options)
		}
		if !reflect.DeepEqual(card.AnswerOptionIdxs, w.idxs) {
			t.Errorf("%s (%s): answer_option_idx %v must become %v, got %v",
				r.id, r.why, r.answerIdx, w.idxs, card.AnswerOptionIdxs)
		}
		if card.SelectMode != replyCardSelectModeSingle {
			t.Errorf("%s: every legacy card is single-select, got %q", r.id, card.SelectMode)
		}
	}
}
