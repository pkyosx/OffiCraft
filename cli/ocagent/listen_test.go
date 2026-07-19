package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// scanSSE — the pure wire parser (copy-twin of ocwarden, + id/comment hooks).
// ---------------------------------------------------------------------------

func collectData(t *testing.T, raw string) []string {
	t.Helper()
	var got []string
	if err := scanSSE(strings.NewReader(raw), sseSink{
		onData: func(p []byte) { got = append(got, string(p)) },
	}); err != nil {
		t.Fatalf("scanSSE err: %v", err)
	}
	return got
}

func TestScanSSE_SingleDataFrame(t *testing.T) {
	got := collectData(t, "data: {\"topic\":\"chat\"}\n\n")
	if want := []string{`{"topic":"chat"}`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestScanSSE_MultiLineDataJoined(t *testing.T) {
	got := collectData(t, "data: a\ndata: b\n\n")
	if want := []string{"a\nb"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestScanSSE_CommentsIdEventIgnoredForData(t *testing.T) {
	raw := ": connected\n\n" +
		"id: 7\nevent: delta\ndata: {\"topic\":\"member\"}\n\n" +
		": heartbeat\n\n" +
		"retry: 3000\ndata: {\"topic\":\"task\"}\n\n"
	got := collectData(t, raw)
	if want := []string{`{"topic":"member"}`, `{"topic":"task"}`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestScanSSE_CRLFTolerated(t *testing.T) {
	if got := collectData(t, "data: hi\r\n\r\n"); !reflect.DeepEqual(got, []string{"hi"}) {
		t.Fatalf("got %v", got)
	}
}

func TestScanSSE_LeadingSpaceStrippedOnce(t *testing.T) {
	if got := collectData(t, "data:  x\n\n"); !reflect.DeepEqual(got, []string{" x"}) {
		t.Fatalf("got %q want %q", got, []string{" x"})
	}
}

func TestScanSSE_IncompleteFinalDiscarded(t *testing.T) {
	if got := collectData(t, "data: no-terminator\n"); len(got) != 0 {
		t.Fatalf("expected no payloads for unterminated event, got %v", got)
	}
}

func TestScanSSE_IDHookFiresTrimmed(t *testing.T) {
	var ids []string
	_ = scanSSE(strings.NewReader("id: 42\ndata: {}\n\n"), sseSink{
		onID: func(s string) { ids = append(ids, s) },
	})
	if !reflect.DeepEqual(ids, []string{"42"}) {
		t.Fatalf("id hook got %v want [42]", ids)
	}
}

func TestScanSSE_CommentSelfExitStopsScan(t *testing.T) {
	// onComment returns true on the FIRST comment ⇒ scanSSE returns errSelfExit and
	// stops (the later data frame is never dispatched).
	var data []string
	err := scanSSE(strings.NewReader(": heartbeat\n\ndata: {\"topic\":\"task\"}\n\n"), sseSink{
		onData:    func(p []byte) { data = append(data, string(p)) },
		onComment: func() bool { return true },
	})
	if err != errSelfExit {
		t.Fatalf("err = %v, want errSelfExit", err)
	}
	if len(data) != 0 {
		t.Fatalf("scan must stop before the data frame, got %v", data)
	}
}

// ---------------------------------------------------------------------------
// nextBackoff — exponential, capped, jittered (Python next_backoff).
// ---------------------------------------------------------------------------

func TestNextBackoff_DoublesFlooredCappedJittered(t *testing.T) {
	start, capd := 1*time.Second, 15*time.Second
	// jitter=1.0 ⇒ no reduction: 1s→2s, 8s→15s(cap), below-floor→start*2.
	cases := []struct{ cur, want time.Duration }{
		{1 * time.Second, 2 * time.Second},
		{8 * time.Second, 15 * time.Second}, // 16 clamps to cap
		{0, 2 * time.Second},                // floored at start then doubled
		{15 * time.Second, 15 * time.Second},
	}
	for _, c := range cases {
		if got := nextBackoff(c.cur, start, capd, 1.0); got != c.want {
			t.Fatalf("nextBackoff(%s, jf=1) = %s, want %s", c.cur, got, c.want)
		}
	}
	// jitter=0.5 halves the (doubled, capped) value: 1s→doubled 2s→*0.5=1s.
	if got := nextBackoff(1*time.Second, start, capd, 0.5); got != 1*time.Second {
		t.Fatalf("jf=0.5: got %s want 1s", got)
	}
	// defaultJitter always lands in [0.5, 1.0).
	for i := 0; i < 200; i++ {
		if jf := defaultJitter(); jf < 0.5 || jf >= 1.0 {
			t.Fatalf("defaultJitter out of [0.5,1.0): %v", jf)
		}
	}
}

// ---------------------------------------------------------------------------
// cursor persistence.
// ---------------------------------------------------------------------------

func TestCursorPathAndRoundtrip(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Home: dir, ID: "Kyle"}
	p := cursorPath(cfg)
	if want := filepath.Join(dir, "kyle", "sse-cursor"); p != want {
		t.Fatalf("cursorPath = %q want %q (id lowercased)", p, want)
	}
	if readCursor(p) != "" {
		t.Fatalf("missing cursor must read empty")
	}
	writeCursor(p, "99")
	if got := readCursor(p); got != "99" {
		t.Fatalf("roundtrip got %q want 99", got)
	}
	// anon fallback when no id.
	if got := cursorPath(Config{Home: dir}); got != filepath.Join(dir, "anon", "sse-cursor") {
		t.Fatalf("anon cursor path = %q", got)
	}
}

// ---------------------------------------------------------------------------
// wake gates + handler + strOrEmpty.
// ---------------------------------------------------------------------------

func TestShouldDispatch(t *testing.T) {
	for _, tc := range []struct {
		frame map[string]any
		want  bool
	}{
		{map[string]any{"topic": "action"}, true},
		{map[string]any{"topic": "task"}, true},
		{map[string]any{"topic": "chat"}, false},
		{map[string]any{"topic": "member"}, false},
		{nil, false},
		{map[string]any{}, false},
	} {
		if got := shouldDispatch(tc.frame); got != tc.want {
			t.Fatalf("shouldDispatch(%v) = %v want %v", tc.frame, got, tc.want)
		}
	}
}

func TestShouldWindDown(t *testing.T) {
	mine := map[string]any{"topic": "member", "data": map[string]any{"key": "owner::kyle"}}
	if !shouldWindDown(mine, "kyle") {
		t.Fatal("a member delta scoped to my id must gate true")
	}
	if !shouldWindDown(mine, "KYLE") {
		t.Fatal("id match must be case-insensitive")
	}
	other := map[string]any{"topic": "member", "data": map[string]any{"key": "owner::someone"}}
	if shouldWindDown(other, "kyle") {
		t.Fatal("a member delta naming someone else must gate false")
	}
	if shouldWindDown(map[string]any{"topic": "task"}, "kyle") {
		t.Fatal("non-member topic must gate false")
	}
	if shouldWindDown(mine, "") {
		t.Fatal("no identity must gate false")
	}
	// no :: prefix — whole key is the id.
	if !shouldWindDown(map[string]any{"topic": "member", "data": map[string]any{"key": "kyle"}}, "kyle") {
		t.Fatal("a bare key equal to my id must gate true")
	}
}

func TestHandleEventOutput(t *testing.T) {
	var out bytes.Buffer
	handleEvent(map[string]any{"seq": float64(5), "topic": "task"}, "owner", &out)
	if got := out.String(); got != "[ocagent] wake seq=5 topic=task · by owner\n" {
		t.Fatalf("handleEvent out = %q", got)
	}
	// nil seq renders Python-style None.
	out.Reset()
	handleEvent(map[string]any{"topic": "action"}, "", &out)
	if got := out.String(); got != "[ocagent] wake seq=None topic=action\n" {
		t.Fatalf("handleEvent nil-seq out = %q", got)
	}
}

func TestHandleDirectedBandPrintsTheServerMessage(t *testing.T) {
	// context-high: the server-composed reason IS the printed message.
	var out bytes.Buffer
	handleDirectedBand(map[string]any{
		"topic": "context-high",
		"data": map[string]any{
			"topic": "context-high", "to": "m-1", "level": "warn",
			"pct":    float64(45),
			"reason": "context 45% — start converging; flush in-flight state",
		},
	}, &out)
	if got := out.String(); got != "[ocagent] signal context-high: context 45% — start converging; flush in-flight state\n" {
		t.Fatalf("context-high out = %q", got)
	}

	// task-close: same shape, task fields riding along.
	out.Reset()
	handleDirectedBand(map[string]any{
		"topic": "task-close",
		"data": map[string]any{
			"topic": "task-close", "to": "m-1", "task_id": "t-7d40aabbccdd",
			"task_no": "T-7d40", "type": "review-pr", "status": "done",
			"reason": "任務 T-7d40 已結束（done）。請用 write_task_learnings 整併回手冊。",
		},
	}, &out)
	if got := out.String(); got != "[ocagent] signal task-close: 任務 T-7d40 已結束（done）。請用 write_task_learnings 整併回手冊。\n" {
		t.Fatalf("task-close out = %q", got)
	}

	// Junk-safe: no reason (and even no data) degrades to a composed line —
	// never a panic, never silence.
	out.Reset()
	handleDirectedBand(map[string]any{
		"topic": "task-close",
		"data": map[string]any{
			"task_no": "T-7d40", "type": "review-pr", "status": "terminated",
		},
	}, &out)
	if got := out.String(); !strings.Contains(got, "T-7d40") ||
		!strings.Contains(got, "terminated") ||
		!strings.Contains(got, "write_task_learnings") {
		t.Fatalf("reason-less task-close fallback = %q", got)
	}
	out.Reset()
	handleDirectedBand(map[string]any{"topic": "context-high"}, &out)
	if got := out.String(); !strings.HasPrefix(got, "[ocagent] signal context-high:") {
		t.Fatalf("data-less context-high fallback = %q", got)
	}
}

func TestDispatchRoutesDirectedBandsAndIgnoresUnknownTopics(t *testing.T) {
	var out bytes.Buffer
	l := &listener{out: &out}
	frame := func(topic string, data map[string]any) []byte {
		raw, err := json.Marshal(map[string]any{"topic": topic, "data": data})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	// Both directed band topics print their message through dispatch().
	l.dispatch(frame("context-high", map[string]any{"reason": "context 45% — start converging"}))
	l.dispatch(frame("task-close", map[string]any{"reason": "任務 T-7d40 已結束（done）"}))
	got := out.String()
	if !strings.Contains(got, "[ocagent] signal context-high: context 45% — start converging") ||
		!strings.Contains(got, "[ocagent] signal task-close: 任務 T-7d40 已結束（done）") {
		t.Fatalf("dispatch must surface both directed bands, got %q", got)
	}
	// An unknown topic stays silent (the pre-existing contract).
	out.Reset()
	l.dispatch(frame("mystery-topic", map[string]any{"reason": "boo"}))
	l.dispatch([]byte("not json at all"))
	if got := out.String(); got != "" {
		t.Fatalf("unknown topics must stay silent, got %q", got)
	}
}

func TestStrOrEmpty(t *testing.T) {
	for _, tc := range []struct {
		in   any
		want string
	}{
		{nil, ""}, {"", ""}, {"x", "x"}, {float64(0), ""}, {float64(3), "3"},
		{false, ""}, {true, "True"},
	} {
		if got := strOrEmpty(tc.in); got != tc.want {
			t.Fatalf("strOrEmpty(%v) = %q want %q", tc.in, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// fetch_chat / drain_chat over httptest — R7 refetch downlink.
// ---------------------------------------------------------------------------

func chatServer(t *testing.T, list string) (*httptest.Server, *string) {
	t.Helper()
	var gotWith string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			gotWith = r.URL.Query().Get("with")
			w.WriteHeader(200)
			_, _ = w.Write([]byte(list))
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)
	return srv, &gotWith
}

func TestDrainChat_UnreadForMeOnly_AdvancesSeen(t *testing.T) {
	// ts is 130s in the past ⇒ the printed age is "2m" (minute-truncated, and far
	// enough from the 120s/180s edges that test wall-time cannot flip it).
	list := fmt.Sprintf(`[
	  {"id":"m1","from":"boss","to":"kyle","body":"hello","ts":%d},
	  {"id":"m2","from":"kyle","to":"boss","body":"mine-to-someone","ts":%d},
	  {"id":"m3","from":"peer","to":"other","body":"not-for-me","ts":%d}
	]`, time.Now().Unix()-130, time.Now().Unix(), time.Now().Unix())
	srv, gotWith := chatServer(t, list)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	seen := map[string]bool{}
	var out bytes.Buffer

	n := drainChat(srv.Client(), cfg, seen, &out, false)
	if n != 1 {
		t.Fatalf("unread-for-me count = %d want 1", n)
	}
	if *gotWith != "kyle" {
		t.Fatalf("with= param = %q want kyle", *gotWith)
	}
	if got := out.String(); got != "[ocagent] chat from boss (id, 2m ago): hello\n" {
		t.Fatalf("drain out = %q", got)
	}
	if !seen["m1"] {
		t.Fatal("m1 must be marked seen")
	}
	// Second drain: m1 already seen ⇒ nothing new, nothing printed.
	out.Reset()
	if n2 := drainChat(srv.Client(), cfg, seen, &out, false); n2 != 0 || out.Len() != 0 {
		t.Fatalf("second drain n=%d out=%q, want 0 and empty", n2, out.String())
	}
}

func TestDrainChat_SilentAdvancesWithoutPrint(t *testing.T) {
	srv, _ := chatServer(t, `[{"id":"m1","from":"boss","to":"kyle","body":"hi"}]`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	seen := map[string]bool{}
	var out bytes.Buffer
	n := drainChat(srv.Client(), cfg, seen, &out, true) // silent baseline
	if n != 1 || out.Len() != 0 {
		t.Fatalf("silent drain n=%d out=%q, want count 1 and NO print", n, out.String())
	}
	if !seen["m1"] {
		t.Fatal("silent drain must still advance the seen cursor")
	}
}

func TestDrainChat_MissingTsPrintsIdTagOnly(t *testing.T) {
	srv, _ := chatServer(t, `[{"id":"m1","from":"boss","to":"kyle","body":"hi"}]`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	drainChat(srv.Client(), cfg, map[string]bool{}, &out, false)
	if got := out.String(); got != "[ocagent] chat from boss (id): hi\n" {
		t.Fatalf("no-ts drain out = %q", got)
	}
}

func TestDrainChat_ImageAttachmentAppendsBadge(t *testing.T) {
	srv, _ := chatServer(t, `[{"id":"m1","from":"boss","to":"kyle","body":"看這張",
		"attachments":[{"id":"a1","mime":"image/png","is_image":true,"filename":"x.png"}]}]`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	drainChat(srv.Client(), cfg, map[string]bool{}, &out, false)
	if got := out.String(); got != "[ocagent] chat from boss (id): 看這張 📎1圖\n" {
		t.Fatalf("image attachment drain out = %q", got)
	}
}

func TestDrainChat_EmptyBodyWithAttachmentsPrintsBadgeOnly(t *testing.T) {
	srv, _ := chatServer(t, `[{"id":"m1","from":"boss","to":"kyle","body":"",
		"attachments":[{"id":"a1","is_image":true},{"id":"a2","is_image":true}]}]`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	drainChat(srv.Client(), cfg, map[string]bool{}, &out, false)
	if got := out.String(); got != "[ocagent] chat from boss (id): 📎2圖\n" {
		t.Fatalf("empty-body attachment drain out = %q", got)
	}
}

func TestDrainChat_MixedAttachmentsCountsImagesAndFiles(t *testing.T) {
	srv, _ := chatServer(t, `[{"id":"m1","from":"boss","to":"kyle","body":"附件",
		"attachments":[{"id":"a1","is_image":true},
		{"id":"a2","is_image":false,"mime":"application/pdf"},
		{"id":"a3","is_image":false,"mime":"text/plain"}]}]`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	drainChat(srv.Client(), cfg, map[string]bool{}, &out, false)
	if got := out.String(); got != "[ocagent] chat from boss (id): 附件 📎1圖 2檔\n" {
		t.Fatalf("mixed attachment drain out = %q", got)
	}
}

func TestDrainChat_NoAttachmentsByteIdentical(t *testing.T) {
	srv, _ := chatServer(t, `[{"id":"m1","from":"boss","to":"kyle","body":"hi","attachments":[]}]`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	drainChat(srv.Client(), cfg, map[string]bool{}, &out, false)
	if got := out.String(); got != "[ocagent] chat from boss (id): hi\n" {
		t.Fatalf("zero-attachment drain out = %q", got)
	}
}

func TestFmtAgo(t *testing.T) {
	for _, tc := range []struct {
		secs float64
		want string
	}{
		{-5, "0s"}, {0, "0s"}, {10, "10s"}, {59, "59s"}, {60, "1m"}, {130, "2m"},
		{3599, "59m"}, {3600, "1h"}, {7300, "2h"}, {86399, "23h"}, {86400, "1d"},
		{3 * 86400, "3d"},
	} {
		if got := fmtAgo(tc.secs); got != tc.want {
			t.Fatalf("fmtAgo(%v) = %q want %q", tc.secs, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// reply-card downlink — refetch + wake for MY answered card.
// ---------------------------------------------------------------------------

func replyCardServer(t *testing.T, status int, cardJSON string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/reply-cards/") {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(cardJSON))
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func replyCardFrame(id, from string) map[string]any {
	return map[string]any{"topic": "reply_card", "data": map[string]any{
		"key":     "owner::" + id,
		"payload": map[string]any{"id": id, "from": from, "status": "answered"},
	}}
}

// testReplySeen builds a PRIMED empty seen store in a temp dir — the live
// handler's normal posture (a baseline exists, nothing surfaced yet).
func testReplySeen(t *testing.T) *replyCardSeen {
	t.Helper()
	return &replyCardSeen{path: filepath.Join(t.TempDir(), "replycards-seen"),
		m: map[string]float64{}, primed: true}
}

func TestHandleReplyCard_AnsweredPrintsOptionTextAndAttachments(t *testing.T) {
	srv, _ := replyCardServer(t, 200, `{"id":"rc-1","from":"kyle","status":"answered",
		"summary":"先做 A 還是 B?","options":["做 A","做 B"],
		"answer":{"option_idx":1,"text":"順便補測試","attachments":[{"id":"a1"}]}}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-1", "kyle"), testReplySeen(t), "owner", &out)
	want := "[ocagent] reply-card rc-1 answered: picked [1] \"做 B\" — \"順便補測試\" — " +
		"+1 attachment(s) | asked: 先做 A 還是 B? · by owner\n"
	if got := out.String(); got != want {
		t.Fatalf("answered out = %q want %q", got, want)
	}
}

func TestHandleReplyCard_TextOnlyAnswerPrintsText(t *testing.T) {
	srv, _ := replyCardServer(t, 200, `{"id":"rc-2","from":"kyle","status":"answered",
		"summary":"要不要上?","options":["上"],"answer":{"option_idx":null,"text":"先等 CI","attachments":[]}}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-2", "kyle"), testReplySeen(t), "owner", &out)
	if got := out.String(); got != "[ocagent] reply-card rc-2 answered: \"先等 CI\" | asked: 要不要上? · by owner\n" {
		t.Fatalf("text-only out = %q", got)
	}
}

// A pathological reply-card answer text (past the 64 KiB valve) is truncated
// with a pointer to get_reply_card, never dumped whole. Positive control:
// TestHandleReplyCard_TextOnlyAnswerPrintsText above prints a short answer
// verbatim with NO truncation marker.
func TestHandleReplyCard_PathologicalAnswerTrippedBySafetyValve(t *testing.T) {
	huge := strings.Repeat("嘮叨", 20000) // ≈ 120 KiB — over the 64 KiB valve
	srv, _ := replyCardServer(t, 200, `{"id":"rc-big","from":"kyle","status":"answered",
		"summary":"要不要上?","options":["上"],"answer":{"option_idx":null,"text":"`+huge+`","attachments":[]}}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-big", "kyle"), testReplySeen(t), "owner", &out)
	line := out.String()
	if !strings.Contains(line, "safety valve") || !strings.Contains(line, "get_reply_card") {
		t.Fatalf("valve trip must point at get_reply_card: %q", line[:min(len(line), 200)])
	}
	if strings.Contains(line, huge) {
		t.Fatal("a valve-tripped answer must not print the full text")
	}
	if !utf8.ValidString(line) {
		t.Fatal("rune-boundary cut must not split a multi-byte char")
	}
}

// A multi-line reply-card summary within the cap prints in full, indented so
// the answered line stays one readable event block.
func TestHandleReplyCard_MultiLineSummaryPrintedInFull(t *testing.T) {
	srv, _ := replyCardServer(t, 200, `{"id":"rc-ml","from":"kyle","status":"answered",
		"summary":"選項一\n選項二","options":["上"],"answer":{"option_idx":null,"text":"先等 CI","attachments":[]}}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-ml", "kyle"), testReplySeen(t), "owner", &out)
	want := "[ocagent] reply-card rc-ml answered: \"先等 CI\" | asked: 選項一\n    選項二 · by owner\n"
	if got := out.String(); got != want {
		t.Fatalf("multi-line summary:\n got %q\nwant %q", got, want)
	}
	if strings.Contains(out.String(), "truncated") {
		t.Fatal("under-cap summary must not be truncated")
	}
}

func TestHandleReplyCard_OtherMembersCardIgnoredWithoutRefetch(t *testing.T) {
	srv, hits := replyCardServer(t, 200, `{}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-3", "someone"), testReplySeen(t), "owner", &out)
	if *hits != 0 || out.Len() != 0 {
		t.Fatalf("someone else's delta must not refetch nor print: hits=%d out=%q", *hits, out.String())
	}
}

func TestHandleReplyCard_AuthorityFromOverridesPayloadFrom(t *testing.T) {
	// A lying/junk payload claims the card is mine; the refetched authority says
	// it is not — silence (the payload never decides).
	srv, hits := replyCardServer(t, 200, `{"id":"rc-4","from":"someone","status":"answered",
		"summary":"s","options":["o"],"answer":{"option_idx":0,"text":"","attachments":[]}}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-4", "kyle"), testReplySeen(t), "owner", &out)
	if *hits != 1 || out.Len() != 0 {
		t.Fatalf("authority-from mismatch must refetch once and print nothing: hits=%d out=%q",
			*hits, out.String())
	}
}

func TestHandleReplyCard_WaitingCardStaysSilent(t *testing.T) {
	// My own create rides the same fan (status waiting) — no wake yet.
	srv, _ := replyCardServer(t, 200, `{"id":"rc-5","from":"kyle","status":"waiting",
		"summary":"s","options":["o"],"answer":null}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-5", "kyle"), testReplySeen(t), "owner", &out)
	if out.Len() != 0 {
		t.Fatalf("a waiting card must print nothing, got %q", out.String())
	}
}

func TestHandleReplyCard_RefetchFailurePrintsHonestLine(t *testing.T) {
	srv, _ := replyCardServer(t, 404, `{"error":{"code":"not_found"}}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-6", "kyle"), testReplySeen(t), "owner", &out)
	want := "[ocagent] reply-card rc-6 changed but refetch failed (HTTP 404) — " +
		"read it manually (get_reply_card).\n"
	if got := out.String(); got != want {
		t.Fatalf("refetch-failure out = %q want %q", got, want)
	}
}

func TestHandleReplyCard_JunkFramesIgnored(t *testing.T) {
	srv, hits := replyCardServer(t, 200, `{}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	for _, frame := range []map[string]any{
		nil,
		{"topic": "reply_card"},
		{"topic": "reply_card", "data": map[string]any{}},
		{"topic": "reply_card", "data": map[string]any{"payload": map[string]any{"from": "kyle"}}},
	} {
		handleReplyCard(srv.Client(), cfg, frame, testReplySeen(t), "owner", &out)
	}
	if *hits != 0 || out.Len() != 0 {
		t.Fatalf("junk frames must neither refetch nor print: hits=%d out=%q", *hits, out.String())
	}
}

func TestHandleReplyCard_ReanswerPrintsRevisedAnswer(t *testing.T) {
	// 重新決定 (PUT re-answer) fans the SAME delta shape and bumps answered_ts —
	// the handler prints the refetched answer each time (the seen dedup keys on
	// the ts, so a NEW answer is never swallowed), so the revision reaches the
	// session too.
	answers := []string{
		`{"id":"rc-7","from":"kyle","status":"answered","summary":"s","options":["A","B"],
			"answered_ts":100,"answer":{"option_idx":0,"text":"","attachments":[]}}`,
		`{"id":"rc-7","from":"kyle","status":"answered","summary":"s","options":["A","B"],
			"answered_ts":200,"answer":{"option_idx":1,"text":"改走 B","attachments":[]}}`,
	}
	var call int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&call, 1)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(answers[n-1]))
	}))
	defer srv.Close()
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	seen := testReplySeen(t)
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-7", "kyle"), seen, "owner", &out)
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-7", "kyle"), seen, "owner", &out)
	want := "[ocagent] reply-card rc-7 answered: picked [0] \"A\" | asked: s · by owner\n" +
		"[ocagent] reply-card rc-7 answered: picked [1] \"B\" — \"改走 B\" | asked: s · by owner\n"
	if got := out.String(); got != want {
		t.Fatalf("re-answer out = %q want %q", got, want)
	}
}

func TestHandleReplyCard_DuplicateDeltaSameAnswerPrintsOnce(t *testing.T) {
	srv, _ := replyCardServer(t, 200, `{"id":"rc-8","from":"kyle","status":"answered",
		"summary":"s","options":["A"],"answered_ts":100,
		"answer":{"option_idx":0,"text":"","attachments":[]}}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	seen := testReplySeen(t)
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-8", "kyle"), seen, "owner", &out)
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-8", "kyle"), seen, "owner", &out)
	if got := strings.Count(out.String(), "rc-8 answered"); got != 1 {
		t.Fatalf("the same answer must print exactly once, printed %d:\n%s", got, out.String())
	}
}

func TestHandleReplyCard_ExpiredPrintsGuidanceLine(t *testing.T) {
	// T-1aa4: an owner-expired card wakes the initiator with a self-carrying
	// guidance line (reopen fresh vs move on) — not the answered line.
	srv, _ := replyCardServer(t, 200, `{"id":"rc-x1","from":"kyle","status":"expired",
		"summary":"還要等這個嗎?","options":["等"],"expired_ts":100,"answer":null}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-x1", "kyle"), testReplySeen(t), "owner", &out)
	want := "[ocagent] reply-card rc-x1 EXPIRED by owner (no answer) | asked: 還要等這個嗎? — " +
		"the question may be stale: if it still matters, open a FRESH card with " +
		"current context; if not, proceed / close out. Any held step/task was " +
		"already restored to in_progress · by owner\n"
	if got := out.String(); got != want {
		t.Fatalf("expired out = %q want %q", got, want)
	}
}

func TestHandleReplyCard_DuplicateDeltaSameExpiryPrintsOnce(t *testing.T) {
	srv, _ := replyCardServer(t, 200, `{"id":"rc-x2","from":"kyle","status":"expired",
		"summary":"s","options":["A"],"expired_ts":150,"answer":null}`)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	seen := testReplySeen(t)
	var out bytes.Buffer
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-x2", "kyle"), seen, "owner", &out)
	handleReplyCard(srv.Client(), cfg, replyCardFrame("rc-x2", "kyle"), seen, "owner", &out)
	if got := strings.Count(out.String(), "rc-x2 EXPIRED"); got != 1 {
		t.Fatalf("the same expiry must print exactly once, printed %d:\n%s", got, out.String())
	}
}

// ---------------------------------------------------------------------------
// reply-card boot/reconnect drain — the offline-answer catch-up.
// ---------------------------------------------------------------------------

// drainListServer serves GET /api/reply-cards?status=answered from *list and
// ?status=expired from an empty pane (mutable between drains; drain callers
// run single-goroutine in these tests). Expired-pane cases use
// drainPanesServer below.
func drainListServer(t *testing.T, status *int, list *string) *httptest.Server {
	empty := `[]`
	return drainPanesServer(t, status, list, &empty)
}

// drainPanesServer serves both drain panes: ?status=answered from *answered,
// ?status=expired from *expired.
func drainPanesServer(t *testing.T, status *int, answered, expired *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/reply-cards" {
			switch r.URL.Query().Get("status") {
			case "answered":
				w.WriteHeader(*status)
				_, _ = w.Write([]byte(*answered))
				return
			case "expired":
				w.WriteHeader(*status)
				_, _ = w.Write([]byte(*expired))
				return
			}
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// answeredCardJSON is one answered LIGHT list row — the exact shape the server
// serves on GET /api/reply-cards since T-3f31 (卡只需要 title+決策): NO body /
// options full text; the decision digest carries the picked option's ORIGINAL
// wording as answer.option and the attachments as a COUNT (a JSON number).
func answeredCardJSON(id string, ts float64, summary string) string {
	return fmt.Sprintf(`{"id":%q,"from":"kyle","kind":"decision","status":"answered",
		"answered_ts":%v,"summary":%q,"task":null,
		"answer":{"option_idx":0,"option":"ok","text":"","attachments":0}}`,
		id, ts, summary)
}

func TestDrainReplyCards_LightRowDigestPrintsWordingAndAttachmentCount(t *testing.T) {
	// The drain consumes the LIGHT pane rows directly (no per-id refetch):
	// the printed line must take the picked option's wording from the digest's
	// answer.option (no options array exists on a light row) and the
	// attachment count from the digest's NUMBER — the two spots the pre-T-3f31
	// renderer would have silently dropped.
	status := 200
	list := `[{"id":"rc-light","from":"kyle","kind":"decision","status":"answered",
		"answered_ts":100,"summary":"走哪個方案?","task":null,
		"answer":{"option_idx":1,"option":"方案 B","text":"補個理由","attachments":2}}]`
	srv := drainListServer(t, &status, &list)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	seen := testReplySeen(t)
	var out bytes.Buffer
	if n := drainReplyCards(srv.Client(), cfg, seen, &out); n != 1 {
		t.Fatalf("drain printed %d, want 1: %q", n, out.String())
	}
	want := "[ocagent] reply-card rc-light answered: picked [1] \"方案 B\" — " +
		"\"補個理由\" — +2 attachment(s) | asked: 走哪個方案?\n"
	if got := out.String(); got != want {
		t.Fatalf("light-row drain out = %q want %q", got, want)
	}
}

func TestDrainReplyCards_FirstRunPrimesSilently_ThenNextProcessPrintsNewAnswer(t *testing.T) {
	status := 200
	list := `[` + answeredCardJSON("rc-a", 100, "old?") + `]`
	srv := drainListServer(t, &status, &list)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	path := filepath.Join(t.TempDir(), "replycards-seen")
	var out bytes.Buffer

	// First run ever (no state file): prime the baseline, print NOTHING.
	if n := drainReplyCards(srv.Client(), cfg, loadReplyCardSeen(path), &out); n != 0 || out.Len() != 0 {
		t.Fatalf("first-run drain must baseline silently: n=%d out=%q", n, out.String())
	}
	// A new answer lands while the agent is DEAD; a fresh process (reload from
	// the same file) drains it — and only it — on boot.
	list = `[` + answeredCardJSON("rc-b", 200, "new?") + `,` + answeredCardJSON("rc-a", 100, "old?") + `]`
	if n := drainReplyCards(srv.Client(), cfg, loadReplyCardSeen(path), &out); n != 1 {
		t.Fatalf("boot drain must print exactly the offline-answered card, n=%d out=%q", n, out.String())
	}
	if got := out.String(); got != "[ocagent] reply-card rc-b answered: picked [0] \"ok\" | asked: new?\n" {
		t.Fatalf("drain line = %q", got)
	}
}

func TestDrainReplyCards_PrintsOnlyMyNewAnswersOldestFirst(t *testing.T) {
	status := 200
	// Pane order is newest-first; rc-other belongs to someone else; rc-seen was
	// already surfaced at this exact answered_ts.
	list := `[` + answeredCardJSON("rc-new2", 300, "later?") + `,
		{"id":"rc-other","from":"someone","status":"answered","answered_ts":250,
		 "summary":"not mine","options":["x"],"answer":{"option_idx":0,"text":"","attachments":[]}},` +
		answeredCardJSON("rc-seen", 150, "seen?") + `,` + answeredCardJSON("rc-new1", 100, "earlier?") + `]`
	srv := drainListServer(t, &status, &list)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	seen := testReplySeen(t)
	seen.m["rc-seen"] = 150
	var out bytes.Buffer
	if n := drainReplyCards(srv.Client(), cfg, seen, &out); n != 2 {
		t.Fatalf("n=%d want 2, out=%q", n, out.String())
	}
	want := "[ocagent] reply-card rc-new1 answered: picked [0] \"ok\" | asked: earlier?\n" +
		"[ocagent] reply-card rc-new2 answered: picked [0] \"ok\" | asked: later?\n"
	if got := out.String(); got != want {
		t.Fatalf("drain out = %q want %q (mine only, oldest first)", got, want)
	}
}

func TestDrainReplyCards_SkipsWhatTheLiveDeltaAlreadyPrinted(t *testing.T) {
	// The live handler surfaced the answer (and recorded it) while connected —
	// the next reconnect drain must stay quiet about it.
	cardSrv, _ := replyCardServer(t, 200, `{"id":"rc-live","from":"kyle","status":"answered",
		"summary":"live?","options":["ok"],"answered_ts":100,
		"answer":{"option_idx":0,"text":"","attachments":[]}}`)
	cfg := Config{Base: cardSrv.URL, Token: "t", ID: "kyle"}
	seen := testReplySeen(t)
	var out bytes.Buffer
	handleReplyCard(cardSrv.Client(), cfg, replyCardFrame("rc-live", "kyle"), seen, "owner", &out)
	if !strings.Contains(out.String(), "rc-live answered") {
		t.Fatalf("live delta must print first: %q", out.String())
	}

	status := 200
	list := `[` + answeredCardJSON("rc-live", 100, "live?") + `]`
	srv := drainListServer(t, &status, &list)
	cfg.Base = srv.URL
	out.Reset()
	if n := drainReplyCards(srv.Client(), cfg, seen, &out); n != 0 || out.Len() != 0 {
		t.Fatalf("drain must not re-print the live-surfaced answer: n=%d out=%q", n, out.String())
	}
}

func TestDrainReplyCards_RevisionReprints(t *testing.T) {
	status := 200
	list := `[` + answeredCardJSON("rc-rev", 200, "again?") + `]`
	srv := drainListServer(t, &status, &list)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	seen := testReplySeen(t)
	seen.m["rc-rev"] = 100 // surfaced at the OLD answer's ts; 重新決定 bumped it
	var out bytes.Buffer
	if n := drainReplyCards(srv.Client(), cfg, seen, &out); n != 1 {
		t.Fatalf("a revised answer must re-print: n=%d out=%q", n, out.String())
	}
}

func TestDrainReplyCards_PrunesEntriesAgedOutOfThePane(t *testing.T) {
	status := 200
	list := `[` + answeredCardJSON("rc-kept", 200, "kept?") + `]`
	srv := drainListServer(t, &status, &list)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	seen := testReplySeen(t)
	seen.m["rc-kept"] = 200
	seen.m["rc-aged"] = 50 // no longer listed — past the 24h window, can never drain again
	var out bytes.Buffer
	drainReplyCards(srv.Client(), cfg, seen, &out)
	raw, err := os.ReadFile(seen.path)
	if err != nil {
		t.Fatalf("state file must persist: %v", err)
	}
	var m map[string]float64
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("state file must be JSON: %v", err)
	}
	if want := map[string]float64{"rc-kept": 200}; !reflect.DeepEqual(m, want) {
		t.Fatalf("persisted state = %v want %v (aged-out entry pruned)", m, want)
	}
}

func TestDrainReplyCards_FaultPrintsNothingAndKeepsState(t *testing.T) {
	status := 500
	list := `boom`
	srv := drainListServer(t, &status, &list)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	path := filepath.Join(t.TempDir(), "replycards-seen")
	seen := loadReplyCardSeen(path)
	var out bytes.Buffer
	if n := drainReplyCards(srv.Client(), cfg, seen, &out); n != 0 || out.Len() != 0 {
		t.Fatalf("a drain fault must stay silent: n=%d out=%q", n, out.String())
	}
	if seen.primed {
		t.Fatal("a fault must not prime the baseline")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("a fault must not write the state file: %v", err)
	}
	// The fault did NOT burn the first-run posture: the next good drain still
	// primes silently rather than flooding history.
	status, list = 200, `[`+answeredCardJSON("rc-a", 100, "old?")+`]`
	if n := drainReplyCards(srv.Client(), cfg, seen, &out); n != 0 || out.Len() != 0 {
		t.Fatalf("post-fault first drain must baseline silently: n=%d out=%q", n, out.String())
	}
}

// expiredCardJSON is one expired LIGHT list row (T-1aa4): no answer digest —
// expiry is not an answer; the row carries expired_ts instead.
func expiredCardJSON(id string, ts float64, summary string) string {
	return fmt.Sprintf(`{"id":%q,"from":"kyle","kind":"decision","status":"expired",
		"answered_ts":null,"expired_ts":%v,"summary":%q,"task":null,"answer":null}`,
		id, ts, summary)
}

func TestDrainReplyCards_ExpiredPaneCatchesUpOnceThenStaysQuiet(t *testing.T) {
	// A card expired while the agent was offline drains as the SAME guidance
	// line the live handler prints, once — the shared seen state (keyed off
	// expired_ts) keeps every later drain quiet, and the answered pane still
	// drains alongside.
	status := 200
	answered := `[` + answeredCardJSON("rc-ans", 200, "answered?") + `]`
	expired := `[` + expiredCardJSON("rc-exp", 300, "stale?") + `]`
	srv := drainPanesServer(t, &status, &answered, &expired)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	seen := testReplySeen(t)
	var out bytes.Buffer
	if n := drainReplyCards(srv.Client(), cfg, seen, &out); n != 2 {
		t.Fatalf("drain printed %d, want 2: %q", n, out.String())
	}
	if !strings.Contains(out.String(), "rc-ans answered") ||
		!strings.Contains(out.String(), "rc-exp EXPIRED by owner (no answer) | asked: stale?") {
		t.Fatalf("drain must surface both panes: %q", out.String())
	}
	out.Reset()
	if n := drainReplyCards(srv.Client(), cfg, seen, &out); n != 0 || out.Len() != 0 {
		t.Fatalf("second drain must stay quiet: n=%d out=%q", n, out.String())
	}
}

func TestDrainReplyCards_ExpiredPaneFaultLeavesStateUntouched(t *testing.T) {
	// The expired pane failing must not half-rebuild the seen state off the
	// answered pane alone (that would re-print answered history next drain).
	answeredStatus := 200
	answered := `[` + answeredCardJSON("rc-kept", 200, "kept?") + `]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/reply-cards" && r.URL.Query().Get("status") == "answered" {
			w.WriteHeader(answeredStatus)
			_, _ = w.Write([]byte(answered))
			return
		}
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	seen := testReplySeen(t)
	seen.m["rc-kept"] = 200
	var out bytes.Buffer
	if n := drainReplyCards(srv.Client(), cfg, seen, &out); n != 0 || out.Len() != 0 {
		t.Fatalf("faulted drain must print nothing: n=%d out=%q", n, out.String())
	}
	if !seen.has("rc-kept", 200) {
		t.Fatalf("faulted drain must leave the seen state untouched: %v", seen.m)
	}
}

func TestLoadReplyCardSeen_MissingOrCorruptStartsUnprimed(t *testing.T) {
	dir := t.TempDir()
	if s := loadReplyCardSeen(filepath.Join(dir, "nope")); s.primed || len(s.m) != 0 {
		t.Fatalf("missing file must load unprimed-empty: %+v", s)
	}
	corrupt := filepath.Join(dir, "corrupt")
	if err := os.WriteFile(corrupt, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := loadReplyCardSeen(corrupt); s.primed || len(s.m) != 0 {
		t.Fatalf("corrupt file must load unprimed-empty: %+v", s)
	}
	// record → reload roundtrip primes with the recorded value.
	good := filepath.Join(dir, "good")
	(&replyCardSeen{path: good, m: map[string]float64{}}).record("rc-1", 42)
	if s := loadReplyCardSeen(good); !s.primed || !s.has("rc-1", 42) {
		t.Fatalf("roundtrip must prime with the recorded answer: %+v", s)
	}
}

func TestFetchChat_NonListOrErrorYieldsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	if got := fetchChat(srv.Client(), cfg, "kyle"); got != nil {
		t.Fatalf("a non-200 must yield nil, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// WindDownHook — desired_state=offline graceful self-stop (intent-only, seams injected).
// ---------------------------------------------------------------------------

func TestWindDown_OfflineReportsStoppingThenStopped_OneShot(t *testing.T) {
	var phases []string
	terminated := 0
	h := &windDownHook{
		cfg:           Config{ID: "kyle"},
		out:           &bytes.Buffer{},
		fetchDesired:  func() (string, bool) { return "offline", true },
		reportPhase:   func(p string) int { phases = append(phases, p); return 200 },
		selfTerminate: func() { phases = append(phases, "suicide"); terminated++ },
	}
	frame := map[string]any{"topic": "member", "data": map[string]any{"key": "owner::kyle"}}
	if !h.maybeWindDown(frame) {
		t.Fatal("a confirmed offline must declare the stop intent")
	}
	// Self-terminate fires AFTER stopped is reported (the SSE-drop lever, not before).
	if !reflect.DeepEqual(phases, []string{"stopping", "stopped", "suicide"}) {
		t.Fatalf("phases = %v want [stopping stopped suicide]", phases)
	}
	if terminated != 1 {
		t.Fatalf("self-terminate must fire exactly once, got %d", terminated)
	}
	// One-shot: a repeated delta does NOT re-report NOR re-terminate.
	if h.maybeWindDown(frame) {
		t.Fatal("second delta must be a no-op (one-shot)")
	}
	if terminated != 1 {
		t.Fatalf("one-shot violated: self-terminate fired %d times", terminated)
	}
}

func TestWindDown_SkipsWhenNotOfflineOrNotMine(t *testing.T) {
	var reported, terminated int
	newH := func(desired_state string) *windDownHook {
		return &windDownHook{
			cfg:           Config{ID: "kyle"},
			out:           &bytes.Buffer{},
			fetchDesired:  func() (string, bool) { return desired_state, true },
			reportPhase:   func(string) int { reported++; return 200 },
			selfTerminate: func() { terminated++ },
		}
	}
	mine := map[string]any{"topic": "member", "data": map[string]any{"key": "owner::kyle"}}
	if newH("online").maybeWindDown(mine) {
		t.Fatal("desired_state=online must NOT wind down")
	}
	notmine := map[string]any{"topic": "member", "data": map[string]any{"key": "owner::other"}}
	if newH("offline").maybeWindDown(notmine) {
		t.Fatal("a delta naming someone else must NOT wind down")
	}
	if reported != 0 {
		t.Fatalf("no phase report expected, got %d", reported)
	}
	if terminated != 0 {
		t.Fatalf("no self-terminate expected when it does not wind down, got %d", terminated)
	}
}

func TestWindDown_ReportFailedNotMasked(t *testing.T) {
	var out bytes.Buffer
	h := &windDownHook{
		cfg:          Config{ID: "kyle"},
		out:          &out,
		fetchDesired: func() (string, bool) { return "offline", true },
		reportPhase:  func(string) int { return 0 }, // transport fault ⇒ falsy status
	}
	frame := map[string]any{"topic": "member", "data": map[string]any{"key": "owner::kyle"}}
	h.maybeWindDown(frame)
	if !strings.Contains(out.String(), "FAILED (HTTP 0)") {
		t.Fatalf("a failed report must be logged FAILED, not masked:\n%s", out.String())
	}
}

// ---------------------------------------------------------------------------
// RecycleHook — desired_state=online ∧ refocus_since>0 (wake-only, epoch one-shot).
// ---------------------------------------------------------------------------

func TestRecycle_WakesSessionWithHandoverSOP_EpochOneShot(t *testing.T) {
	var out bytes.Buffer
	member := map[string]any{"desired_state": "online", "refocus_since": float64(100)}
	h := &recycleHook{
		cfg:         Config{ID: "kyle"},
		out:         &out,
		fetchMember: func() (map[string]any, bool) { return member, true },
	}
	frame := map[string]any{"topic": "member", "data": map[string]any{"key": "owner::kyle"}}
	if !h.maybeRecycle(frame) {
		t.Fatal("online + refocus must wake the session with the handover SOP")
	}
	// The full SOP prints, one prefixed line each, baton addressed to MY OWN id.
	for _, line := range handoverSOP("kyle") {
		if !strings.Contains(out.String(), "[ocagent] "+line+"\n") {
			t.Fatalf("wake output missing SOP line %q:\n%s", line, out.String())
		}
	}
	if !strings.Contains(out.String(), "to=kyle") {
		t.Fatalf("baton step must address the agent's OWN id:\n%s", out.String())
	}
	// SAME epoch again (e.g. the member deltas fanned by the session's own
	// stopping/stopped reports) ⇒ no re-print.
	out.Reset()
	if h.maybeRecycle(frame) || out.Len() != 0 {
		t.Fatalf("same refocus epoch must not re-wake; out=%q", out.String())
	}
	// The session reporting stopped does NOT make ocagent act — the kill is the
	// server's (event-driven robust STOP on the stopped report).
	member["stopped_since"] = float64(150)
	if h.maybeRecycle(frame) || out.Len() != 0 {
		t.Fatalf("a stopped report must not re-trigger anything client-side; out=%q", out.String())
	}
	// NEW, larger epoch ⇒ re-arms the wake.
	member["refocus_since"] = float64(200)
	if !h.maybeRecycle(frame) {
		t.Fatal("a new refocus epoch must re-wake")
	}
	if !strings.Contains(out.String(), "換手 SOP") {
		t.Fatalf("re-armed wake must print the SOP again:\n%s", out.String())
	}
}

func TestRecycle_SkipsOfflineOrNoRefocus(t *testing.T) {
	frame := map[string]any{"topic": "member", "data": map[string]any{"key": "owner::kyle"}}
	outs := map[string]*bytes.Buffer{}
	mk := func(name string, m map[string]any) *recycleHook {
		outs[name] = &bytes.Buffer{}
		return &recycleHook{
			cfg:         Config{ID: "kyle"},
			out:         outs[name],
			fetchMember: func() (map[string]any, bool) { return m, true },
		}
	}
	if mk("off", map[string]any{"desired_state": "offline", "refocus_since": float64(100)}).maybeRecycle(frame) {
		t.Fatal("desired_state=offline is wind-down's job, not recycle")
	}
	if mk("norf", map[string]any{"desired_state": "online", "refocus_since": float64(0)}).maybeRecycle(frame) {
		t.Fatal("no refocus marker must not recycle")
	}
	for name, out := range outs {
		if out.Len() != 0 {
			t.Fatalf("%s: no wake output expected, got %q", name, out.String())
		}
	}
}

// ---------------------------------------------------------------------------
// listener end-to-end over an httptest mock SSE server.
// ---------------------------------------------------------------------------

// eventsServer streams `frames` on the FIRST /api/events connection then holds it open
// until the client cancels; later connections just block. It answers /api/chat with
// EMPTY on the first call (so the silent boot baseline advances the cursor over no
// history) and `chatList` on every call thereafter (so a chat-delta refetch surfaces
// the NEW message). It captures the Last-Event-ID header of connection #1.
func eventsServer(frames []string, chatList string, gotLastEventID *string, conns *int32) *httptest.Server {
	var first sync.Once
	var chatCalls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			w.WriteHeader(200)
			if atomic.AddInt32(&chatCalls, 1) == 1 {
				_, _ = w.Write([]byte("[]")) // silent baseline sees no history
			} else {
				_, _ = w.Write([]byte(chatList))
			}
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/reply-cards/") {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"rc-9","from":"kyle","status":"answered",
				"summary":"ship it?","options":["ship","hold"],
				"answer":{"option_idx":0,"text":"","attachments":[]}}`))
			return
		}
		if !strings.HasPrefix(r.URL.Path, eventsPath) {
			w.WriteHeader(404)
			return
		}
		atomic.AddInt32(conns, 1)
		first.Do(func() {
			if gotLastEventID != nil {
				*gotLastEventID = r.Header.Get("Last-Event-ID")
			}
		})
		w.Header().Set("Content-Type", "text/event-stream")
		fl, ok := w.(http.Flusher)
		if !ok {
			return
		}
		fl.Flush()
		for _, f := range frames {
			_, _ = w.Write([]byte(f))
			fl.Flush()
		}
		<-r.Context().Done()
	}))
}

// syncBuf is a mutex-guarded writer so a listener goroutine can write while the test
// goroutine polls its contents (a plain bytes.Buffer is not concurrency-safe).
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newTestListener(srv *httptest.Server, cfg Config, out io.Writer) *listener {
	return &listener{
		cfg:              cfg,
		api:              srv.Client(),
		streamClient:     srv.Client(),
		sleep:            func(time.Duration) {},
		backoffStart:     time.Millisecond,
		backoffCap:       time.Millisecond,
		jitter:           func() float64 { return 1.0 },
		out:              out,
		clock:            time.Now,
		probeUnknownSpan: probeUnknownGrace,
		refusalGraceSpan: sseRefusalGrace,
		cursorPath:       filepath.Join(cfgTempDir, "cursor"),
		seen:             map[string]bool{},
		replySeen:        loadReplyCardSeen(filepath.Join(cfgTempDir, "replycards-seen")),
	}
}

var cfgTempDir string

func TestListener_EndToEnd_DispatchAndCursor(t *testing.T) {
	cfgTempDir = t.TempDir()
	chatList := `[{"id":"c1","from":"boss","to":"kyle","body":"ping"}]`
	frames := []string{
		": connected\n\n",
		"id: 7\n\n", // persist cursor
		"data: {\"topic\":\"task\",\"seq\":3}\n\n", // WORK wake
		"data: {\"topic\":\"chat\"}\n\n",           // chat NUDGE → refetch → print unread
		// reply_card NUDGE → refetch the card → wake with the answer
		"data: {\"topic\":\"reply_card\",\"data\":{\"key\":\"owner::rc-9\"," +
			"\"payload\":{\"id\":\"rc-9\",\"from\":\"kyle\",\"status\":\"answered\"}}}\n\n",
		"data: {\"topic\":\"other\"}\n\n", // ignored
	}
	var lastEventID string
	var conns int32
	srv := eventsServer(frames, chatList, &lastEventID, &conns)
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "tok", ID: "kyle"}
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	// seed a cursor so the Last-Event-ID replay header is asserted.
	writeCursor(l.cursorPath, "5")
	l.winddown = newWindDownHook(srv.Client(), cfg, noEnv, out)
	l.recycle = newRecycleHook(srv.Client(), cfg, out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()

	waitForCond(t, func() bool {
		return strings.Contains(out.String(), "wake seq=3 topic=task") &&
			strings.Contains(out.String(), "chat from boss (id): ping") &&
			strings.Contains(out.String(),
				"reply-card rc-9 answered: picked [0] \"ship\" | asked: ship it?")
	}, "work wake + chat refetch + reply-card wake dispatched over the wire")

	cancel()
	<-done

	if lastEventID != "5" {
		t.Fatalf("Last-Event-ID replay header = %q want 5", lastEventID)
	}
	// the id:7 frame advanced the persisted cursor.
	if got := readCursor(l.cursorPath); got != "7" {
		t.Fatalf("cursor after id:7 = %q want 7", got)
	}
}

func TestListener_ReconnectsAfterDrop(t *testing.T) {
	cfgTempDir = t.TempDir()
	var conns int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") || strings.HasPrefix(r.URL.Path, "/api/reply-cards") {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("[]"))
			return
		}
		atomic.AddInt32(&conns, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"topic\":\"task\"}\n\n"))
		// handler returns → body EOF → client reconnects.
	}))
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	l := newTestListener(srv, cfg, &out)
	l.winddown = newWindDownHook(srv.Client(), cfg, noEnv, &out)
	l.recycle = newRecycleHook(srv.Client(), cfg, &out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// stop the loop once a reconnect is proven.
	l.sleep = func(time.Duration) {
		if atomic.LoadInt32(&conns) >= 2 {
			cancel()
		}
	}
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	<-done
	if atomic.LoadInt32(&conns) < 2 {
		t.Fatalf("expected ≥2 connections (reconnect), got %d", conns)
	}
}

func TestListener_WatchdogReconnectsSilentStream(t *testing.T) {
	cfgTempDir = t.TempDir()
	var conns int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") || strings.HasPrefix(r.URL.Path, "/api/reply-cards") {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("[]"))
			return
		}
		atomic.AddInt32(&conns, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		<-r.Context().Done() // never emit a frame — silently dead / half-open
	}))
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	l := newTestListener(srv, cfg, &out)
	l.idleReadTimeout = 40 * time.Millisecond
	l.winddown = newWindDownHook(srv.Client(), cfg, noEnv, &out)
	l.recycle = newRecycleHook(srv.Client(), cfg, &out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l.sleep = func(time.Duration) {
		if atomic.LoadInt32(&conns) >= 2 {
			cancel()
		}
	}
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()
	waitForCond(t, func() bool { return atomic.LoadInt32(&conns) >= 2 },
		"watchdog to force-drop the silent stream and reconnect")
	cancel()
	<-done
}

func TestListener_ReconnectDrainPrintsOfflineAnswerOnce(t *testing.T) {
	cfgTempDir = t.TempDir()
	// Drain 1 (connection 1) sees an empty pane — first run: primes silently —
	// and the stream drops. The owner answers rc-off during the offline gap (no
	// replay on /api/events — the delta is gone for good), so drain 2 must
	// surface it, and drain 3+ must NOT re-print it.
	var drains, conns int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/chat"):
			w.WriteHeader(200)
			_, _ = w.Write([]byte("[]"))
		case r.URL.Path == "/api/reply-cards":
			w.WriteHeader(200)
			if r.URL.Query().Get("status") != "answered" {
				_, _ = w.Write([]byte(`[]`)) // the expired pane stays empty here
				return
			}
			if atomic.AddInt32(&drains, 1) == 1 {
				_, _ = w.Write([]byte(`[]`)) // nothing answered yet at boot
			} else {
				_, _ = w.Write([]byte(`[` + answeredCardJSON("rc-off", 100, "offline?") + `]`))
			}
		case strings.HasPrefix(r.URL.Path, eventsPath):
			n := atomic.AddInt32(&conns, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			if fl, ok := w.(http.Flusher); ok {
				fl.Flush()
			}
			if n <= 2 {
				return // stream drops → reconnect
			}
			<-r.Context().Done()
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.winddown = newWindDownHook(srv.Client(), cfg, noEnv, out)
	l.recycle = newRecycleHook(srv.Client(), cfg, out)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()

	waitForCond(t, func() bool {
		return atomic.LoadInt32(&conns) >= 3 &&
			strings.Contains(out.String(), "rc-off answered")
	}, "the reconnect drain to surface the offline-answered card")
	cancel()
	<-done

	want := "[ocagent] reply-card rc-off answered: picked [0] \"ok\" | asked: offline?\n"
	if got := strings.Count(out.String(), want); got != 1 {
		t.Fatalf("the offline answer must print exactly once across reconnects, printed %d:\n%s",
			got, out.String())
	}
}

// ---------------------------------------------------------------------------
// self-exit — the tmux-session lifecycle tie (probe debounce + heartbeat trip).
// ---------------------------------------------------------------------------

func TestFoldProbe_DebounceAndFaultSafe(t *testing.T) {
	l := &listener{out: &bytes.Buffer{}, clock: time.Now,
		probeUnknownSpan: probeUnknownGrace}
	// nil probe ⇒ never self-exit.
	if l.foldProbe() {
		t.Fatal("nil probe must never self-exit")
	}
	// alive resets; two consecutive GONE misses trip.
	verdict := probeAlive
	l.probe = func() probeVerdict { return verdict }
	if l.foldProbe() { // alive → miss 0
		t.Fatal("alive must not trip")
	}
	verdict = probeGone
	if l.foldProbe() { // miss 1
		t.Fatal("one miss must not trip (debounce)")
	}
	if !l.foldProbe() { // miss 2 → trip
		t.Fatal("two consecutive misses must self-exit")
	}
	// a panicking probe folds as UNKNOWN: never an instant verdict, and it
	// RESETS the GONE debounce (unverifiable ≠ evidence of a gone session).
	l.miss = 1
	l.probe = func() probeVerdict { panic("boom") }
	if l.foldProbe() {
		t.Fatal("a panicking probe must not self-exit instantly")
	}
	if l.miss != 0 {
		t.Fatalf("an UNKNOWN probe must reset the GONE debounce to 0, got %d", l.miss)
	}
	if l.unknowns != 1 {
		t.Fatalf("a panicking probe must fold as one UNKNOWN, got %d", l.unknowns)
	}
}

func TestFoldProbe_UnknownFailsClosedAfterBothBounds(t *testing.T) {
	// Fake clock: the fail-closed trip needs probeUnknownMin consecutive
	// unknowns AND probeUnknownSpan of wall clock — neither alone suffices.
	now := time.Unix(1_000_000, 0)
	l := &listener{out: &bytes.Buffer{},
		clock:            func() time.Time { return now },
		probeUnknownSpan: probeUnknownGrace,
		probe:            func() probeVerdict { return probeUnknown },
	}
	// Count bound crossed, time bound not: many unknowns inside the grace.
	for i := 0; i < probeUnknownMin*3; i++ {
		if l.foldProbe() {
			t.Fatalf("unknown #%d tripped inside the wall-clock grace", i+1)
		}
	}
	// Time bound crossed too → trip.
	now = now.Add(probeUnknownGrace)
	if !l.foldProbe() {
		t.Fatal("unknown past BOTH bounds must fail-closed self-exit")
	}
	// An ALIVE verdict resets the run completely.
	l2 := &listener{out: &bytes.Buffer{},
		clock:            func() time.Time { return now },
		probeUnknownSpan: probeUnknownGrace,
	}
	l2.probe = func() probeVerdict { return probeUnknown }
	for i := 0; i < probeUnknownMin-1; i++ {
		_ = l2.foldProbe()
	}
	l2.probe = func() probeVerdict { return probeAlive }
	_ = l2.foldProbe()
	if l2.unknowns != 0 || !l2.firstUnknownAt.IsZero() {
		t.Fatal("an alive probe must reset the unknown run")
	}
	// Time bound alone (few probes, long elapsed) must not trip either: the
	// first unknown of a fresh run re-anchors the clock.
	l2.probe = func() probeVerdict { return probeUnknown }
	now = now.Add(24 * time.Hour)
	if l2.foldProbe() {
		t.Fatal("a single unknown after a long quiet period must not trip")
	}
}

func TestFoldRefusal_BothBoundsAndReset(t *testing.T) {
	now := time.Unix(2_000_000, 0)
	l := &listener{out: &bytes.Buffer{},
		clock:            func() time.Time { return now },
		refusalGraceSpan: sseRefusalGrace,
	}
	// Count bound alone (grace not elapsed) never trips.
	for i := 0; i < sseRefusalMin*2; i++ {
		if l.foldRefusal() {
			t.Fatalf("refusal #%d tripped inside the grace window", i+1)
		}
	}
	// Grace elapsed + consecutive count → trip.
	now = now.Add(sseRefusalGrace)
	if !l.foldRefusal() {
		t.Fatal("refusals past BOTH bounds must trip fail-closed")
	}
	// resetRefusals (any non-409 outcome) breaks the run: the next refusal
	// re-anchors the clock and the count restarts.
	l.resetRefusals()
	if l.foldRefusal() {
		t.Fatal("a fresh refusal after a reset must not trip (count restarted)")
	}
	if l.refusals != 1 || l.firstRefusalAt != now {
		t.Fatalf("reset must restart the run: refusals=%d firstAt=%v", l.refusals, l.firstRefusalAt)
	}
}

func TestListener_SelfExitsOnHeartbeatWhenSessionGone(t *testing.T) {
	cfgTempDir = t.TempDir()
	// A stream that sends heartbeats forever; the probe reports the session GONE, so the
	// heartbeat-line probe (#2) trips self-exit and run() returns 0 without reconnecting.
	var conns int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") || strings.HasPrefix(r.URL.Path, "/api/reply-cards") {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("[]"))
			return
		}
		atomic.AddInt32(&conns, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		fl.Flush()
		tk := time.NewTicker(5 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-tk.C:
				if _, err := w.Write([]byte(": heartbeat\n\n")); err != nil {
					return
				}
				fl.Flush()
			}
		}
	}))
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	l := newTestListener(srv, cfg, &out)
	l.winddown = newWindDownHook(srv.Client(), cfg, noEnv, &out)
	l.recycle = newRecycleHook(srv.Client(), cfg, &out)
	l.probe = func() probeVerdict { return probeGone } // session always gone

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()

	select {
	case rc := <-done:
		if rc != 0 {
			t.Fatalf("self-exit rc = %d want 0", rc)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not self-exit within 3s on a gone session")
	}
	if !strings.Contains(out.String(), "self-exiting") {
		t.Fatalf("expected a self-exit log line:\n%s", out.String())
	}
	if got := atomic.LoadInt32(&conns); got != 1 {
		t.Fatalf("self-exit must NOT reconnect; saw %d connection(s)", got)
	}
}

func TestListener_SelfExitAtReconnectTop(t *testing.T) {
	// probe #1 (reconnect top): with the session already gone for the debounce limit,
	// run() self-exits BEFORE ever dialing /api/events.
	cfgTempDir = t.TempDir()
	var eventsHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, eventsPath) {
			atomic.AddInt32(&eventsHits, 1)
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	var out bytes.Buffer
	l := newTestListener(srv, cfg, &out)
	l.winddown = newWindDownHook(srv.Client(), cfg, noEnv, &out)
	l.recycle = newRecycleHook(srv.Client(), cfg, &out)
	l.probe = func() probeVerdict { return probeGone }
	l.miss = sessionMissLimit - 1 // one more miss trips at probe #1

	rc := l.run(context.Background())
	if rc != 0 {
		t.Fatalf("rc = %d want 0", rc)
	}
	if got := atomic.LoadInt32(&eventsHits); got != 0 {
		t.Fatalf("self-exit at reconnect-top must NOT dial /api/events; saw %d", got)
	}
}

// ---------------------------------------------------------------------------
// fail-closed server refusal — the zombie stop gate's client half.
// ---------------------------------------------------------------------------

func TestListener_SelfTerminatesAfterPersistentSSERefusal(t *testing.T) {
	cfgTempDir = t.TempDir()
	// The server ALWAYS refuses /api/events with the stop-gate 409 (the zombie
	// scenario: a stop is in effect for this member). The listener must stop
	// hammering: after sseRefusalMin consecutive refusals spanning the grace
	// (test: grace 0 so the count bound alone gates), it self-terminates via
	// the suicide seam and run() returns instead of reconnecting forever.
	var conns int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("[]"))
			return
		}
		atomic.AddInt32(&conns, 1)
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"member 'kyle' has a stop in effect"}}`))
	}))
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.winddown = newWindDownHook(srv.Client(), cfg, noEnv, out)
	l.recycle = newRecycleHook(srv.Client(), cfg, out)
	l.refusalGraceSpan = 0 // count bound only — no wall-clock wait in tests
	var terminated int32
	l.selfTerminate = func() { atomic.AddInt32(&terminated, 1) }

	done := make(chan int, 1)
	go func() { done <- l.run(context.Background()) }()

	select {
	case rc := <-done:
		if rc != 0 {
			t.Fatalf("rc = %d want 0", rc)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not fail-closed within 3s of persistent 409s")
	}
	if got := atomic.LoadInt32(&conns); got != sseRefusalMin {
		t.Fatalf("want exactly %d refused dials before the fail-closed exit, saw %d",
			sseRefusalMin, got)
	}
	if atomic.LoadInt32(&terminated) != 1 {
		t.Fatal("the suicide seam must fire exactly once on the fail-closed exit")
	}
	if !strings.Contains(out.String(), "fail-closed") {
		t.Fatalf("expected the honest fail-closed log line:\n%s", out.String())
	}
	// The server's refusal reason must surface in the log (honest, not masked).
	if !strings.Contains(out.String(), "stop in effect") {
		t.Fatalf("expected the server's refusal body in the log:\n%s", out.String())
	}
}

func TestListener_NonRefusalOutcomesNeverTripFailClosed(t *testing.T) {
	cfgTempDir = t.TempDir()
	// A briefly-unavailable server (5xx) must NEVER accumulate toward the
	// fail-closed kill, and a refusal run BROKEN by such an outcome restarts:
	// 409,…,409,500 repeating never reaches sseRefusalMin consecutively.
	var dials int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/chat") {
			w.WriteHeader(200)
			_, _ = w.Write([]byte("[]"))
			return
		}
		n := atomic.AddInt32(&dials, 1)
		if n%int32(sseRefusalMin) == 0 {
			w.WriteHeader(500) // breaks every refusal run one short of the bound
			return
		}
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":{"code":"conflict","message":"nope"}}`))
	}))
	defer srv.Close()

	cfg := Config{Base: srv.URL, Token: "t", ID: "kyle"}
	out := &syncBuf{}
	l := newTestListener(srv, cfg, out)
	l.winddown = newWindDownHook(srv.Client(), cfg, noEnv, out)
	l.recycle = newRecycleHook(srv.Client(), cfg, out)
	l.refusalGraceSpan = 0 // even with NO grace, the broken run must never trip
	var terminated int32
	l.selfTerminate = func() { atomic.AddInt32(&terminated, 1) }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- l.run(ctx) }()

	waitForCond(t, func() bool { return atomic.LoadInt32(&dials) >= int32(sseRefusalMin*3) },
		"several broken refusal runs to elapse")
	select {
	case <-done:
		t.Fatalf("listener exited — a broken refusal run must never fail-closed:\n%s", out.String())
	default:
	}
	cancel()
	<-done
	if atomic.LoadInt32(&terminated) != 0 {
		t.Fatal("selfTerminate must never fire when refusals are not consecutive")
	}
}

// ---------------------------------------------------------------------------
// cmdListen — production entry: mis-wire guard.
// ---------------------------------------------------------------------------

func TestCmdListen_NoTokenExitsQuietly(t *testing.T) {
	var out bytes.Buffer
	if rc := cmdListen(Config{ID: "kyle"}, func(string) string { return "" }, false, &out); rc != 0 {
		t.Fatalf("rc = %d want 0", rc)
	}
	if !strings.Contains(out.String(), "no OC_ID/OC_TOKEN") {
		t.Fatalf("expected the mis-wire line, got %q", out.String())
	}
}

// ---------------------------------------------------------------------------
// helpers.
// ---------------------------------------------------------------------------

// noEnv is a headless env accessor (every key ""): the default selfTerminate wiring
// reads OC_SESSION through it, so a listener test's hooks resolve to a no-op suicide.
func noEnv(string) string { return "" }

func waitForCond(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

// ---------------------------------------------------------------------------
// T-f39c: echo suppression (trigger==self drops client-side) + readable task
// event lines (task_no + title + what moved + by whom) + preview truncation.
// ---------------------------------------------------------------------------

func TestIsSelfEcho(t *testing.T) {
	for _, tc := range []struct {
		trigger, my string
		want        bool
	}{
		{"kyle", "kyle", true},
		{"KYLE", "kyle", true}, // ids compare case-insensitively (config casing drift)
		{"owner", "kyle", false},
		{"server", "kyle", false},
		{"m-other", "kyle", false},
		{"", "kyle", false}, // blank trigger = unknown attribution — NEVER an echo
		{"kyle", "", false}, // no own id (mis-wire) — never suppress
	} {
		if got := isSelfEcho(tc.trigger, tc.my); got != tc.want {
			t.Fatalf("isSelfEcho(%q, %q) = %v want %v", tc.trigger, tc.my, got, tc.want)
		}
	}
}

func TestPreviewLine(t *testing.T) {
	if got := previewLine("a\nb\t c", 10); got != "a b c" {
		t.Fatalf("whitespace collapse = %q", got)
	}
	long := strings.Repeat("字", 200)
	got := previewLine(long, 160)
	if got != strings.Repeat("字", 160)+"…" {
		t.Fatalf("rune truncation = %q", got)
	}
	if got := previewLine("short", 160); got != "short" {
		t.Fatalf("short passthrough = %q", got)
	}
}

// dispatchFrame marshals one delta frame for listener.dispatch.
func dispatchFrame(t *testing.T, topic, trigger string, payload map[string]any) []byte {
	t.Helper()
	frame := map[string]any{"topic": topic, "seq": 9}
	if trigger != "" {
		frame["trigger"] = trigger
	}
	if payload != nil {
		frame["data"] = map[string]any{"payload": payload}
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDispatch_SelfTriggeredEchoSuppressed(t *testing.T) {
	// The three owner-mandated acceptance cases, client side:
	//   1. my own trigger  → NOTHING printed, NO refetch (echo dropped);
	//   2. owner trigger   → processed as before;
	//   3. server trigger  → processed as before.
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	var out bytes.Buffer
	l := newTestListener(srv, Config{Base: srv.URL, Token: "tok", ID: "kyle"}, &out)

	// 1. self echo: a chat delta I triggered must not even refetch.
	l.dispatch(dispatchFrame(t, "chat", "kyle", nil))
	if atomic.LoadInt32(&hits) != 0 || out.String() != "" {
		t.Fatalf("self-triggered delta must be dropped without refetch/print: hits=%d out=%q",
			hits, out.String())
	}
	// case-insensitive: config casing drift must not defeat the gate.
	l.dispatch(dispatchFrame(t, "chat", "KYLE", nil))
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatal("case-drifted self trigger must still suppress")
	}

	// 2. owner-triggered → refetch happens (chat drain runs).
	l.dispatch(dispatchFrame(t, "chat", "owner", nil))
	if atomic.LoadInt32(&hits) == 0 {
		t.Fatal("an owner-triggered delta must be processed")
	}

	// 3. server-triggered → refetch happens too.
	before := atomic.LoadInt32(&hits)
	l.dispatch(dispatchFrame(t, "chat", "server", nil))
	if atomic.LoadInt32(&hits) == before {
		t.Fatal("a server-triggered delta must be processed")
	}

	// 4. blank trigger (older producer) → fail-open, processed.
	before = atomic.LoadInt32(&hits)
	l.dispatch(dispatchFrame(t, "chat", "", nil))
	if atomic.LoadInt32(&hits) == before {
		t.Fatal("a trigger-less delta must be processed (fail-open)")
	}
}

func TestDispatch_MemberTopicExemptFromEchoSuppression(t *testing.T) {
	// spec §2.3 exemption: a SELF-triggered member delta must STILL nudge the
	// hooks — restart_self (T-4c71) stamps refocus_since via the agent's OWN
	// request, and the handover-SOP wake rides exactly that self-triggered
	// member delta. Suppressing it would break graceful self-recycle.
	var out bytes.Buffer
	fetches := 0
	l := &listener{cfg: Config{ID: "kyle"}, out: &out}
	l.winddown = &windDownHook{cfg: l.cfg, out: &out,
		fetchDesired: func() (string, bool) { fetches++; return "online", true },
		reportPhase:  func(string) int { return 200 },
	}
	l.recycle = &recycleHook{cfg: l.cfg, out: &out,
		fetchMember: func() (map[string]any, bool) {
			fetches++
			return map[string]any{"desired_state": "online", "refocus_since": 42.0}, true
		},
	}
	raw, _ := json.Marshal(map[string]any{"topic": "member", "trigger": "kyle",
		"data": map[string]any{"key": "owner::kyle"}})
	l.dispatch(raw)
	if fetches == 0 {
		t.Fatal("a self-triggered member delta must still nudge the hooks (restart_self SOP)")
	}
	if !strings.Contains(out.String(), "recycle:") {
		t.Fatalf("the self-requested recycle SOP wake must land, got %q", out.String())
	}
}

// taskEventServer serves GET /api/tasks/{id} with the given DTO JSON.
func taskEventServer(t *testing.T, status int, taskJSON string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/tasks/") {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(taskJSON))
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func taskFrame(id string) map[string]any {
	return map[string]any{"topic": "task", "data": map[string]any{
		"key":     "owner::" + id,
		"payload": map[string]any{"id": id, "status": "in_progress", "priority": "normal"},
	}}
}

func TestHandleTaskEvent_ReadableLineAndDiff(t *testing.T) {
	cfg := Config{ID: "kyle", Token: "tok"}
	snaps := map[string]taskSnap{}
	var out bytes.Buffer

	// First sight: state the current position (no diff base yet).
	srv, _ := taskEventServer(t, 200, `{"id":"t-be18aabbccdd","task_no":"T-be18",
		"title":"修 listener 事件行","status":"in_progress","progress_done":1,"progress_total":5}`)
	cfg.Base = srv.URL
	handleTaskEvent(srv.Client(), cfg, taskFrame("t-be18aabbccdd"), snaps, "owner", &out)
	if got := out.String(); got != "[ocagent] task T-be18「修 listener 事件行」status=in_progress (1/5) · by owner\n" {
		t.Fatalf("first-sight line = %q", got)
	}

	// Step progress: same status, done count moved → "step done".
	out.Reset()
	srv2, _ := taskEventServer(t, 200, `{"id":"t-be18aabbccdd","task_no":"T-be18",
		"title":"修 listener 事件行","status":"in_progress","progress_done":2,"progress_total":5}`)
	cfg.Base = srv2.URL
	handleTaskEvent(srv2.Client(), cfg, taskFrame("t-be18aabbccdd"), snaps, "owner", &out)
	if got := out.String(); got != "[ocagent] task T-be18「修 listener 事件行」step done (2/5) · by owner\n" {
		t.Fatalf("step-done line = %q", got)
	}

	// Status flip: prints the transition.
	out.Reset()
	srv3, _ := taskEventServer(t, 200, `{"id":"t-be18aabbccdd","task_no":"T-be18",
		"title":"修 listener 事件行","status":"done","progress_done":5,"progress_total":5}`)
	cfg.Base = srv3.URL
	handleTaskEvent(srv3.Client(), cfg, taskFrame("t-be18aabbccdd"), snaps, "server", &out)
	if got := out.String(); got != "[ocagent] task T-be18「修 listener 事件行」status in_progress → done (5/5) · by server\n" {
		t.Fatalf("status-flip line = %q", got)
	}

	// Nothing visible moved (plan/deps/notes) → terse "updated".
	out.Reset()
	handleTaskEvent(srv3.Client(), cfg, taskFrame("t-be18aabbccdd"), snaps, "owner", &out)
	if got := out.String(); got != "[ocagent] task T-be18「修 listener 事件行」updated (5/5) · by owner\n" {
		t.Fatalf("updated line = %q", got)
	}
}

func TestHandleTaskEvent_LongTitleTruncated(t *testing.T) {
	long := strings.Repeat("很長的標題", 30) // 150 runes
	srv, _ := taskEventServer(t, 200, `{"id":"t-1","task_no":"T-0001","title":"`+long+`",
		"status":"in_progress","progress_done":0,"progress_total":0}`)
	cfg := Config{Base: srv.URL, ID: "kyle", Token: "tok"}
	var out bytes.Buffer
	handleTaskEvent(srv.Client(), cfg, taskFrame("t-1"), map[string]taskSnap{}, "owner", &out)
	line := out.String()
	if !strings.Contains(line, "…") || len([]rune(line)) > 110 {
		t.Fatalf("long title must truncate to one short line, got %d runes: %q",
			len([]rune(line)), line)
	}
	// A stepless task shows no (done/total) counter.
	if strings.Contains(line, "(0/0)") {
		t.Fatalf("stepless task must not print a 0/0 counter: %q", line)
	}
}

func TestHandleTaskEvent_RefetchFailurePrintsHonestLine(t *testing.T) {
	srv, _ := taskEventServer(t, 500, `boom`)
	cfg := Config{Base: srv.URL, ID: "kyle", Token: "tok"}
	var out bytes.Buffer
	handleTaskEvent(srv.Client(), cfg, taskFrame("t-9"), map[string]taskSnap{}, "owner", &out)
	if got := out.String(); got != "[ocagent] task t-9 changed but refetch failed (HTTP 500) — read it manually (get_task) · by owner\n" {
		t.Fatalf("honest fault line = %q", got)
	}
}

func TestHandleTaskEvent_JunkFrameFallsBackToGenericWake(t *testing.T) {
	srv, hits := taskEventServer(t, 200, `{}`)
	cfg := Config{Base: srv.URL, ID: "kyle", Token: "tok"}
	var out bytes.Buffer
	frame := map[string]any{"topic": "task", "seq": float64(3)} // no payload id
	handleTaskEvent(srv.Client(), cfg, frame, map[string]taskSnap{}, "owner", &out)
	if *hits != 0 {
		t.Fatal("an id-less frame must not fire a refetch")
	}
	if got := out.String(); got != "[ocagent] wake seq=3 topic=task · by owner\n" {
		t.Fatalf("fallback wake line = %q", got)
	}
}

// A must-read chat body prints IN FULL — the whole point of T-4272 (ocagent
// already refetched it, so a preview would only cost a second get_chat). This
// body is ~1.2 KiB of CJK, far below the 64 KiB safety valve.
func TestDrainChat_UndersizeBodyPrintedInFull(t *testing.T) {
	full := strings.Repeat("囉嗦", 200) // 400 runes ≈ 1.2 KiB — well under the valve
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"c-full","from":"boss","to":"kyle","body":"` + full + `"}]`))
	}))
	defer srv.Close()
	cfg := Config{Base: srv.URL, ID: "kyle", Token: "tok"}
	var out bytes.Buffer
	if n := drainChat(srv.Client(), cfg, map[string]bool{}, &out, false); n != 1 {
		t.Fatalf("drain = %d", n)
	}
	line := out.String()
	if want := "[ocagent] chat from boss (id): " + full + "\n"; line != want {
		t.Fatalf("under-cap body must print verbatim:\n got %q\nwant %q", line, want)
	}
	// Positive control for the truncation assertions below: an under-cap body
	// carries NO truncation marker.
	if strings.Contains(line, "truncated") || strings.Contains(line, "…") {
		t.Fatalf("under-cap body must not be truncated: %q", line)
	}
}

// A multi-line chat body within the cap prints every line — continuation lines
// indented so the block reads as ONE event, never several [ocagent] lines.
func TestDrainChat_MultiLineBodyPrintedIndentedAsOneBlock(t *testing.T) {
	// A body whose own text starts a line with the event prefix must NOT be able
	// to pose as a separate event — the indent defends the block boundary.
	body := `交接 SOP:\n1. 先接手 listen\n[ocagent] 這行看起來像事件但其實是內文`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"c-ml","from":"boss","to":"kyle","body":"` + body + `"}]`))
	}))
	defer srv.Close()
	cfg := Config{Base: srv.URL, ID: "kyle", Token: "tok"}
	var out bytes.Buffer
	drainChat(srv.Client(), cfg, map[string]bool{}, &out, false)
	want := "[ocagent] chat from boss (id): 交接 SOP:\n" +
		"    1. 先接手 listen\n" +
		"    [ocagent] 這行看起來像事件但其實是內文\n"
	if got := out.String(); got != want {
		t.Fatalf("multi-line body block:\n got %q\nwant %q", got, want)
	}
	// No continuation line begins at column 0 with the event prefix (block
	// integrity: exactly ONE line is a real [ocagent] event).
	events := 0
	for _, ln := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n") {
		if strings.HasPrefix(ln, "[ocagent] ") {
			events++
		}
	}
	if events != 1 {
		t.Fatalf("multi-line body split into %d apparent events, want 1", events)
	}
}

// A long-but-realistic must-read chat body (the 5,000-char case the owner named
// as must-print) prints IN FULL — it sits under the safety valve, and
// truncating a message the agent will read anyway is pure loss (T-4272 判準).
func TestDrainChat_LongMustReadBodyPrintedInFull(t *testing.T) {
	long := strings.Repeat("交", 5000) // 5000 runes ≈ 15 KiB — under the 64 KiB valve
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"c-5k","from":"boss","to":"kyle","body":"` + long + `"}]`))
	}))
	defer srv.Close()
	cfg := Config{Base: srv.URL, ID: "kyle", Token: "tok"}
	var out bytes.Buffer
	drainChat(srv.Client(), cfg, map[string]bool{}, &out, false)
	if want := "[ocagent] chat from boss (id): " + long + "\n"; out.String() != want {
		t.Fatalf("5k-char must-read body must print verbatim (len got %d want %d)",
			len(out.String()), len(want))
	}
	if strings.Contains(out.String(), "safety valve") {
		t.Fatal("a 15 KiB message must NOT hit the 64 KiB valve")
	}
}

// Only a PATHOLOGICAL body (past the 64 KiB safety valve) is cut, on a rune
// boundary, with a pointer to the full-text authority (get_chat).
func TestDrainChat_PathologicalBodyTrippedBySafetyValve(t *testing.T) {
	huge := strings.Repeat("囉嗦", 20000) // 40000 runes ≈ 120 KiB — over the 64 KiB valve
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"c-huge","from":"boss","to":"kyle","body":"` + huge + `"}]`))
	}))
	defer srv.Close()
	cfg := Config{Base: srv.URL, ID: "kyle", Token: "tok"}
	var out bytes.Buffer
	drainChat(srv.Client(), cfg, map[string]bool{}, &out, false)
	line := out.String()
	if !strings.Contains(line, "safety valve") || !strings.Contains(line, "get_chat") {
		t.Fatalf("valve trip must point at get_chat: %q", line[:min(len(line), 200)])
	}
	if strings.Contains(line, huge) {
		t.Fatal("a valve-tripped body must not print the full text")
	}
	if !strings.HasPrefix(line, "[ocagent] chat from boss (id): 囉嗦") {
		t.Fatal("a valve-tripped body must still print the head")
	}
	if len(line) > messageBodyValve+256 { // head (≤valve) + prefix + hint
		t.Fatalf("valve-tripped line too long: %d bytes", len(line))
	}
	if !utf8.ValidString(line) {
		t.Fatal("rune-boundary cut must not split a multi-byte char")
	}
}
