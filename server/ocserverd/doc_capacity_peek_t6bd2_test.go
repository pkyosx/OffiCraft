package main

// doc_capacity_peek_t6bd2_test.go — the peek's SIXTH addend (T-6bd2 blocker 2).
//
// doc_capacity is text the wake snapshot CARRIES, so the size-only peek has to
// count it: that peek is the ONE number a waking agent decides against before
// pulling the payload. It shipped uncounted, which is the third time the same
// omission has had to be fixed on this endpoint (T-1b09 roster/machines, then
// T-f278's answered-card pointers, whose own comment called it "the same
// mistake").
//
// The expected numbers below are recomputed from what arrived on the wire, never
// read off the code under test.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// t6bd2Peek pulls the size-only peek through the REAL route and returns the
// decoded body.
func t6bd2Peek(t *testing.T, s *apiServer, actor string) (map[string]float64, int) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.HandlePeekResumeSummarySizeApiResumeSummarySizeGet(rec,
		taskReq(t, "GET", "/api/resume-summary-size", nil, actor, "agent"))
	if rec.Code != http.StatusOK {
		t.Fatalf("peek: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Overview            map[string]float64 `json:"overview"`
		EstimatedTotalChars int                `json:"estimated_total_chars"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode peek: %v", err)
	}
	return body.Overview, body.EstimatedTotalChars
}

// TestPeekCountsEveryBlockThePayloadCarries — BLOCKER 2.
//
// 🔴 THE DEFECT, AND WHY IT IS THE THIRD OF ITS KIND. doc_capacity is text the
// wake snapshot CARRIES, and the peek exists to say what carrying it costs — so
// it is an addend of estimated_total_chars by the same rule that added
// roster/machines (T-1b09) and the answered-card pointers (T-f278, whose own
// comment called it "the same mistake"). T-6bd2 shipped the block uncounted:
// measured on this fixture, the peek reported 872 while the doc_capacity block
// on the wire was 1350 bytes.
//
// The expected number is recomputed HERE from the rows that actually arrived on
// the resume_summary wire, field by field. It deliberately does NOT call
// docCapacityChars: a test that asks the code under test what the answer is
// moves with any mutant that changes it, which is precisely how a
// three-times-repeated omission stays green.
func TestPeekCountsEveryBlockThePayloadCarries(t *testing.T) {
	s := t6bd2Server(t)
	t6bd2FillAll(t, s, seedMiraID)

	rows := t6bd2Capacity(t, s, seedMiraID)
	if len(rows) == 0 {
		t.Fatal("fixture is wrong: the snapshot must carry a doc_capacity block " +
			"for this test to be about anything")
	}
	// What the payload actually carries, counted off the wire.
	carried := 0
	for _, r := range rows {
		carried += len([]rune(r["doc"].(string))) + len([]rune(r["action"].(string)))
		for _, k := range []string{"size_chars", "cap_chars", "remaining_chars"} {
			carried += len(strconv.Itoa(int(r[k].(float64))))
		}
		carried += len(strconv.FormatBool(r["writable"].(bool)))
	}

	overview, total := t6bd2Peek(t, s, seedMiraID)

	reported, ok := overview["doc_capacity_chars"]
	if !ok {
		t.Fatal("the peek's overview must report doc_capacity_chars — the block " +
			"is text the snapshot carries, and an uncounted block is exactly what " +
			"makes the boot threshold decide against a number that does not " +
			"describe the payload")
	}
	if int(reported) != carried {
		t.Fatalf("doc_capacity_chars must equal what the block on the wire carries: "+
			"peek says %d, the %d rows carry %d", int(reported), len(rows), carried)
	}

	// And it has to reach the single number the boot threshold gates on, not
	// merely appear beside it: "computed but not folded in" is the whole defect
	// wearing a newer hat.
	sum := 0
	for _, k := range []string{"chat_chars", "tasks_detail_chars", "roster_chars",
		"machines_chars", "steps_on_answered_card_chars", "doc_capacity_chars"} {
		v, ok := overview[k]
		if !ok {
			t.Fatalf("overview is missing %s", k)
		}
		sum += int(v)
	}
	if total != sum {
		t.Fatalf("estimated_total_chars must be the sum of the six blocks the "+
			"payload carries or omits: got %d, the six add to %d", total, sum)
	}
	if total-int(reported) == total {
		t.Fatal("the doc_capacity block sized to 0 on a fixture where nine " +
			"documents are near their cap — the addend is not measuring anything")
	}
}

// TestPeekDocCapacityIsZeroWhenNothingIsNear is the reverse direction. The block
// is absent from the payload on an ordinary station, so its addend must be 0 and
// the total must be byte-for-byte the number it was before T-6bd2 — otherwise
// this fix would move every boot threshold on every station.
func TestPeekDocCapacityIsZeroWhenNothingIsNear(t *testing.T) {
	s := t6bd2Server(t)

	if rows := t6bd2Capacity(t, s, seedMiraID); len(rows) != 0 {
		t.Fatalf("fixture is wrong: nothing was filled, so the block must be "+
			"absent; got %v", t6bd2Docs(rows))
	}
	overview, total := t6bd2Peek(t, s, seedMiraID)
	if got := overview["doc_capacity_chars"]; got != 0 {
		t.Fatalf("nothing is near a cap, so the addend must be 0, got %v", got)
	}
	five := 0
	for _, k := range []string{"chat_chars", "tasks_detail_chars", "roster_chars",
		"machines_chars", "steps_on_answered_card_chars"} {
		five += int(overview[k])
	}
	if total != five {
		t.Fatalf("on a station with room, the peek must report exactly what it "+
			"reported before T-6bd2: got %d, want %d", total, five)
	}
}
