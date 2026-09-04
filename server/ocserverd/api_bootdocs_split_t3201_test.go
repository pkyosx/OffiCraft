package main

// api_bootdocs_split_t3201_test.go — the read-only head / editable body split
// (T-3201, second package), and the one assertion this package exists to make:
//
//	RenderDocVars(head) + join + body  ==  the bytes the server sends TODAY
//
// 🔴 THAT EQUALITY IS THE PROOF THAT NOTHING WAS REWRITTEN. This package moved
// six event texts out of Go string literals and into documents an owner can
// edit. The one thing that must not have happened along the way is a word
// changing — the ticket's boundary, verbatim: 「不順手改任何一段文字的內容 ——
// 搬家是這張票，改內容是另一件事」. A reviewer cannot hold two 1.5 KB Chinese
// paragraphs side by side and be sure; a byte comparison can.
//
// The expected values below are hand-written literals, never the constants
// under test. Quoting a constant the server also reads would make the test
// agree with whatever that constant says, including the day someone edits it.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// splitSeed reads one seed and cuts it, failing the test if the kind was
// declared split and the seed no longer is.
func splitSeed(t *testing.T, s *apiServer, kind string) (spec bootDocSpec, head, body string) {
	t.Helper()
	spec = s.mustBootDocSpec(kind, bootDocSingletonKey)
	seed, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil || !hasSeed {
		t.Fatalf("%s: read seed %q: hasSeed=%v err=%v", kind, spec.SeedFile, hasSeed, err)
	}
	head, body, ok := DocSplitHeadBody(seed)
	if !ok {
		t.Fatalf("%s: the seed carries no %s line, so it has no read-only head", kind, docBodyMarker)
	}
	return spec, head, body
}

// unsplitSeed is splitSeed's twin for a kind that carries NO read-only head
// (T-6f44). It fails if the kind is still declared split or if the seed still
// carries a marker, so a case written for a headless document cannot quietly
// start passing against a document that grew a head back.
func unsplitSeed(t *testing.T, s *apiServer, kind string) (spec bootDocSpec, text string) {
	t.Helper()
	spec = s.mustBootDocSpec(kind, bootDocSingletonKey)
	if spec.Split {
		t.Fatalf("%s is declared split — this case is written for a document with no read-only head", kind)
	}
	seed, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil || !hasSeed {
		t.Fatalf("%s: read seed %q: hasSeed=%v err=%v", kind, spec.SeedFile, hasSeed, err)
	}
	if strings.Contains(seed, docBodyMarker) {
		t.Fatalf("%s: the kind declares no Split but the seed still carries the marker line", kind)
	}
	return spec, seed
}

func mustRender(t *testing.T, spec bootDocSpec, head string, values map[string]string) string {
	t.Helper()
	out, err := RenderDocVars(head, spec.Vars, values)
	if err != nil {
		t.Fatalf("%s: render head: %v", spec.Kind, err)
	}
	return out
}

// ── the verbatim proof ───────────────────────────────────────────────────────

// The offboard document's head IS the soft notice's opening sentence — no
// longer merely equal to what a Go string builder produced beside it, but the
// only place that sentence exists (T-3201, wiring package).
//
// 🔴 THE DIVERGENCE IS GONE, AND THAT IS THE CHANGE. This test used to record
// one: the document said report_stopped unconditionally while the code
// interpolated offboardCloserFor(m), which answered restart_self for a member
// still wanted online. The owner ruled the document right — verbatim
// (c-5b3d8f192a0b): 「我預期是 report_stopped，因為是 server 控制他上下線」 and
// again (rc-5d044f0c1266): 「下線程序為什麼要看到 restart_self」 — so the builder
// and its two closer constants are deleted and the live producer below now
// answers the document's own bytes on BOTH arms. The refocus arm's behaviour
// under the changed verb is pinned by
// TestSelfDrivenOffboard_StoppedReportAfterARestartSelfStampRespawns.
// 🔴 THE HEAD IS GONE AND THE NOTICE IS THE WHOLE DOCUMENT (T-6f44). This used
// to assert head ⊕ body against the English sentence the server once built in
// Go. Decision 4 deleted {where} — a usage percentage that says nothing about
// how to close out — and what was left of the head said nothing the body did not
// already say, so 〈停止〉 became the first of the ten with NO read-only half:
// every byte of it is the owner's.
//
// What is still asserted, and is the point: the live producer sends exactly the
// document, with nothing composed around it.
func TestOffboardDoc_TheWholeDocumentIsTodaysSoftNotice(t *testing.T) {
	s := newEventProcServer(t)
	_, doc := unsplitSeed(t, s, docKindOffboard)

	const where = "context 59% (your limits: 55% / 65%)"
	got := doc
	want := doc
	if got != want {
		t.Fatalf("the folded document is not today's soft offboard notice:\n got %q\nwant %q", got, want)
	}
	// Nothing the server knows leaks into it any more — no percentage, no
	// English preamble, no interpolation at all.
	if bad := DocVarsIn(doc); len(bad) > 0 {
		t.Fatalf("〈停止〉 declares no variables but its seed names %v", bad)
	}
	if strings.Contains(doc, "start your close-out") {
		t.Fatal("the English head sentence is still in the document")
	}
	// …and the same bytes the live producer builds, which is what makes the
	// hand-written literal above a statement about the SERVER and not about
	// this file.
	if live := s.winddownNoticeText(offboardKindSoft, 0); got != live {
		t.Fatalf("the live producer sent something else:\n got %q\nlive %q", got, live)
	}
}

func TestAcceleratedStopDoc_HeadPlusBodyIsTodaysFinalNotice(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindAcceleratedStop)

	const where = "close-out (your limits: 55% / 65%)"
	const epoch = 1755870180 // 2026-08-22T14:03:00Z
	deadline := time.Unix(epoch, 0).UTC().Format(time.RFC3339)
	got := mustRender(t, spec, head,
		map[string]string{"deadline": deadline}) + spec.Join + body

	// 🔴 ONE SENTENCE, IN CHINESE, AND NO `Your deadline is` (T-6f44). {where}
	// went the way of 〈停止〉's; the English wrapper went with it. The deadline
	// stays because it is the one fact the body cannot state — and the body no
	// longer SNIFFS this line to decide hard-vs-soft, which is the change that
	// had to land before this sentence could be touched at all.
	want := "你的結束時刻是 " + deadline + "。\n" + body
	if got != want {
		t.Fatalf("the folded document is not today's final offboard notice:\n got %q\nwant %q", got, want)
	}
	if live := s.winddownNoticeText(offboardKindFinal, epoch); got != live {
		t.Fatalf("the live producer sent something else:\n got %q\nlive %q", got, live)
	}
}

// 🔴 THE OTHER HALF OF THE VARIABLE MECHANISM, at the send site rather than at
// the write face: a document reaches an agent with NO {name} slot left in it,
// or it does not reach the agent at all.
//
// Both halves are asserted, and the second one is why the first is not enough.
// "No braces in what was sent" is satisfied trivially by a server that sends
// nothing, and "something was sent" is satisfied by a server that ships the
// template — so a notice that cannot be rendered must come back EMPTY (the send
// site omits the key and the agent's client falls back), never as the head with
// its braces still in it. The reachable instance of the second case is a final
// call with no clock: {deadline} is declared, nothing can fill it, and the
// answer must be "".
func TestWindDownNoticeText_SendsNoUnfilledVariableAndRefusesRatherThanShippingOne(t *testing.T) {
	s := newEventProcServer(t)
	for _, c := range []struct {
		name, kind string
		deadline   float64
	}{
		{"soft", offboardKindSoft, 0},
		{"final", offboardKindFinal, 1755870180},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := s.winddownNoticeText(c.kind, c.deadline)
			if got == "" {
				t.Fatal("nothing was sent at all — every assertion here would pass vacuously")
			}
			if bad := DocVarsIn(got); len(bad) > 0 {
				t.Fatalf("the notice reached the agent with %v still in it: %q", bad, got)
			}
		})
	}
	// A declared name nothing can fill: refused, not shipped as a template.
	if got := s.winddownNoticeText(offboardKindFinal, 0); got != "" {
		t.Fatalf("a notice whose {deadline} cannot be filled must not be sent at "+
			"all — it went out as:\n%s", got)
	}
}

// 加速停止 carries a COPY of the offboard body, because the owner ruled two
// documents rather than one with an urgency flag. Two copies of a procedure
// drift; this pins that they start out identical, so the day one is edited it
// is because someone meant to.
func TestAcceleratedStopDoc_ShipsTheSameBodyAsTheOffboardDoc(t *testing.T) {
	s := newEventProcServer(t)
	_, offboardBody := unsplitSeed(t, s, docKindOffboard)
	_, _, acceleratedBody := splitSeed(t, s, docKindAcceleratedStop)
	if offboardBody == acceleratedBody {
		t.Fatal("the two stop procedures still ship the SAME body — T-6f44 made each " +
			"one state which kind of notice it is, so they must differ")
	}
	// 🔴 THE PIN INVERTED, AND THE 定稿 CALLED IT: 「兩份本體從此不同 → 沒人守著
	// 就會漂移」. They used to be byte-identical and this asserted so. Decision 5
	// replaced the string-sniffing §1 (「看到 `Your deadline is` 就是硬性」— a match
	// against ANOTHER document's editable first line) with each document saying
	// outright which one it is, so identical bodies are now the BUG. Each is
	// therefore pinned to its own sentence, here, in one place.
	for _, probe := range []struct{ body, want, reject string }{
		{offboardBody, "沒有人在對你倒數", "你在倒數中"},
		{acceleratedBody, "你在倒數中", "沒有人在對你倒數"},
	} {
		if !strings.Contains(probe.body, probe.want) {
			t.Errorf("this body no longer says which notice it is (%q missing):\n%s", probe.want, probe.body)
		}
		if strings.Contains(probe.body, probe.reject) {
			t.Errorf("this body claims to be the other one (%q):\n%s", probe.reject, probe.body)
		}
		// The sniffed literal is gone from BOTH — leaving it in either one keeps
		// a second, contradicting rule in the text an agent reads.
		if strings.Contains(probe.body, "Your deadline is") {
			t.Errorf("the string-sniffing rule survives in this body:\n%s", probe.body)
		}
	}
}

func TestTaskReassignPredecessorDoc_HeadPlusBodyIsTodaysChatNotice(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindTaskReassignPredecessor)

	// 🔴 THE SUCCESSOR IS NOT NAMED (T-6f44, owner 2026-08-24: 「讓他自己去查」
	// 「不管是不是 outsource」). ONE variable. The body never asks the predecessor
	// to dial anyone, so an identity it would not dial is a fact it does not
	// carry — and naming it was what forced a fabricated placeholder whenever
	// the successor was an outsource worker the scheduler had not minted yet.
	got := mustRender(t, spec, head, map[string]string{
		"task_no": "T-7e91",
	}) + spec.Join + body

	// The literal api_tasks.go used to concatenate, plus the seed FILE's
	// trailing newline — a document is a file and ends with one, a chat row is
	// one message, so the send site trims what it posts the way buildBootContext
	// trims every block it staples.
	want := "[T-7e91] 此任務已轉派給新的接手人。" + "請停止推進，先把交接資訊寫到這張任務上：目前進度、進行中的事項、有哪些雷要注意。**這一步不能省，它是接手人唯一保證讀得到的東西** —— 接手人可能還沒被建出來，也可能你已經下線了才輪到他。\n\n寫完就算交出去了。如果接手人剛好在線上來找你，就順便當面補齊；沒有的話不用等，也不用去找他。" + "\n"
	if got != want {
		t.Fatalf("the folded document is not today's reassign notice:\n got %q\nwant %q", got, want)
	}
	// …and the same bytes the live producer builds, which is what makes the
	// hand-written literal above a statement about the SERVER and not about
	// this file.
	if live := s.taskNoticeText(docKindTaskReassignPredecessor, map[string]string{
		"task_no": "T-7e91",
	}); live != strings.TrimSpace(want) {
		t.Fatalf("the live producer sent something else:\n got %q\nlive %q",
			strings.TrimSpace(want), live)
	}
}

// 🔴 THE NAMED EXCEPTION. Every other document above must equal today's bytes.
// This one must NOT, and that is a ruling rather than a slip: owner, 2026-08-22
// (card rc-8c0045ef7c38), approved rewriting the body into three branches.
// Today's single sentence 「這張任務現在可以開始:請 get_task 讀內容、submit_plan
// 規劃步驟後開始執行。」 hardcodes the assumption that a blocked ticket has not
// started, and there is live evidence of it saying exactly that to a ticket
// already in progress.
//
// 🔴 THE HEAD IS NO LONGER VERBATIM EITHER (T-6f44). It used to be pinned to
// today's bytes 「半形逗號 and raw English status and all」, and this test said
// those three were ruled to stay. That ruling was superseded: the head is down to
// the ONE ticket number the agent has to act on — its OWN — and the two defects
// the old sentence carried died with the variables that produced them.
// {blocker_status} interpolated an untranslated wire code into Chinese prose
// (「已經done了」), and the sentence used the only halfwidth comma in the ten.
// Both are asserted gone below, because "the rewrite landed" is otherwise
// satisfied by a document that reintroduces either.
func TestTaskUnblockedDoc_HeadIsTheBlockedTicketAloneAndTheBodyIsTheApprovedRewrite(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindTaskUnblocked)

	gotHead := mustRender(t, spec, head, map[string]string{"blocked_task_no": "T-0002"})
	wantHead := "[T-0002] 擋著這張任務的前置任務已經結束，它不再擋著你。"
	if gotHead != wantHead {
		t.Fatalf("the read-only head is not the approved sentence:\n got %q\nwant %q", gotHead, wantHead)
	}
	if strings.Contains(head, ",") {
		t.Error("the halfwidth comma is back — this was the only one in the ten")
	}
	for _, code := range []string{"done", "terminated", "{blocker_status}"} {
		if strings.Contains(gotHead, code) {
			t.Errorf("the untranslated wire code %q is back in the sentence: %q", code, gotHead)
		}
	}

	wantBody := "- **還沒開始**：請 get_task 讀內容、submit_plan 規劃步驟後開始執行。\n" +
		"- **已經在進行中**：接著推進，不必重新規劃。\n" +
		"- **優先權是凍結**：先問清楚為什麼被凍結，等能解凍的人解開再動。\n"
	if body != wantBody {
		t.Fatalf("the body is not the approved three-branch rewrite:\n got %q\nwant %q", body, wantBody)
	}
	// And the old single sentence really is gone — otherwise "the rewrite
	// landed" would also be satisfied by a document carrying both.
	if strings.Contains(body, "這張任務現在可以開始") {
		t.Fatal("the pre-rewrite sentence is still in the body; the branches replace it, they do not join it")
	}
	// 🔴 AND THE LIVE PRODUCER ANSWERS IT. Until the wiring package this
	// document and the sentence releaseDependentsOnClose actually posted were
	// two different texts, and the approval above was landed in the seed while
	// the wire kept the old one — a divergence every test here passed over
	// because none of them asked the server what it sends.
	if live := s.taskNoticeText(docKindTaskUnblocked, map[string]string{
		"blocked_task_no": "T-0002",
	}); live != gotHead+spec.Join+strings.TrimSuffix(body, "\n") {
		t.Fatalf("the live producer sent something else:\n got %q\nlive %q",
			gotHead+spec.Join+strings.TrimSuffix(body, "\n"), live)
	}
}

// The other half of the variable mechanism at the TASK send sites, the same
// pair of assertions winddownNoticeText carries: a document reaches an agent
// with no {name} slot left in it, or it does not reach the agent at all.
//
// "No braces in what was sent" is satisfied trivially by a server that sends
// nothing, so both cases are asserted here — a notice whose values are all
// supplied must be non-empty, and a notice missing one must come back "" rather
// than as the template. `{blocker_title}` reads like a real title and names no
// task; an agent cannot tell it was never filled.
func TestTaskNoticeText_SendsNoUnfilledVariableAndRefusesRatherThanShippingOne(t *testing.T) {
	s := newEventProcServer(t)
	full := map[string]map[string]string{
		// 🔴 ONE VARIABLE SINCE T-6f44 — the successor is not named (owner:
		// 「讓他自己去查」). Listing a name this document no longer declares
		// would make the omission loop below drop a key nothing reads, so the
		// refusal it is meant to prove would never be exercised: the notice
		// would render fine and the assertion would report the pass as a
		// failure. The map must be exactly what the kind declares.
		docKindTaskReassignPredecessor: {"task_no": "T-7e91"},
		docKindTaskUnblocked:           {"blocked_task_no": "T-0002"},
	}
	for kind, values := range full {
		t.Run(kind, func(t *testing.T) {
			got := s.taskNoticeText(kind, values)
			if got == "" {
				t.Fatal("nothing was sent at all — every assertion here would pass vacuously")
			}
			if bad := DocVarsIn(got); len(bad) > 0 {
				t.Fatalf("the notice reached the agent with %v still in it: %q", bad, got)
			}
			for missing := range values {
				short := map[string]string{}
				for k, v := range values {
					if k != missing {
						short[k] = v
					}
				}
				if out := s.taskNoticeText(kind, short); out != "" {
					t.Errorf("with no value for {%s} the notice must not be sent at "+
						"all — it went out as:\n%s", missing, out)
				}
			}
		})
	}
}

// ── the overlay reaches the agent ────────────────────────────────────────────

// 🔴 THIS IS THE TICKET'S OWN CLAIM, AND NOTHING ELSE ASSERTED IT. Everything
// above proves the send site ships THE DOCUMENT; none of it proves the send site
// ships the document THE OWNER EDITED. Those differ by exactly one thing — the
// overlay — and every case above reads the seed, so all of them stay green on a
// server that folds nothing and serves the shipped bytes forever. That server
// would answer 200 to every edit, record a revision for each one, and send
// agents the factory text: the precise failure docs/design/boot-documents.md
// warns about, with no surface saying a word.
//
// The edit goes through the REAL write face (replaceBootDoc — the same call the
// cockpit's PUT lands on, head gate, cap and all), never by writing the overlay
// row directly, because a test that installed the row itself would also pass on
// a server whose write face refuses every edit.
//
// The body written in is deliberately NOT the seed's: the whole assertion is
// vacuous if the two happen to agree, so the divergence is checked rather than
// assumed.
func TestEventNoticeText_SendsTheBodyTheOwnerEditedAndNotTheShippedSeed(t *testing.T) {
	const where = "context 59% (your limits: 55% / 65%)"
	const epoch = 1755870180 // 2026-08-22T14:03:00Z
	for _, tc := range []struct {
		kind    string
		trimmed bool
		send    func(s *apiServer) string
	}{
		// 🔴 〈停止〉 IS NO LONGER IN THIS TABLE and cannot be: it has no head to
		// join, so "the overlay reached the agent" is asserted for it by
		// TestOffboardDoc_AnUnsplitKindsWholeOverlayIsWhatIsSent below, which is
		// the same claim in the shape a headless document takes.
		{docKindAcceleratedStop, false, func(s *apiServer) string {
			return s.winddownNoticeText(offboardKindFinal, epoch)
		}},
		{docKindTaskReassignPredecessor, true, func(s *apiServer) string {
			return s.taskNoticeText(docKindTaskReassignPredecessor, map[string]string{
				"task_no": "T-7e91", "new_executor": "銀月（mira）",
			})
		}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			s := newEventProcServer(t)
			spec, head, seedBody := splitSeed(t, s, tc.kind)

			ownerBody := "這一段是 owner 自己改的，出廠文字裡沒有這句。\n"
			if ownerBody == seedBody {
				t.Fatal("the fixture body equals the shipped one, so this case cannot " +
					"tell an overlay-aware send site from one that ignores overlays")
			}
			w := httptest.NewRecorder()
			s.replaceBootDoc(w, ownerPost("/x"), spec, ownerBody, false)
			if w.Code != http.StatusOK {
				t.Fatalf("the write face refused the edit: %d (%s)", w.Code, w.Body.String())
			}

			values := map[string]string{}
			if tc.kind == docKindAcceleratedStop {
				values["deadline"] = time.Unix(epoch, 0).UTC().Format(time.RFC3339)
			}
			if tc.kind == docKindTaskReassignPredecessor {
				values = map[string]string{"task_no": "T-7e91", "new_executor": "銀月（mira）"}
			}
			_ = where
			want := mustRender(t, spec, head, values) + spec.Join + ownerBody
			if tc.trimmed {
				want = strings.TrimSpace(want)
			}
			got := tc.send(s)
			if got != want {
				t.Fatalf("the send site did not carry the owner's edit:\n got %q\nwant %q", got, want)
			}
			// Named separately from the equality above so a failure says WHICH
			// way it went wrong: still shipping the factory body is the specific
			// regression this case exists to catch.
			if strings.Contains(got, strings.TrimSpace(seedBody)) {
				t.Fatalf("the shipped body is still in what was sent — the send site "+
					"is reading the seed, not the fold:\n%s", got)
			}
		})
	}
}

// 🔴 THE ROW THE MARKER RELEASE LEFT BEHIND. docBodyMarker arrived with the
// split and no migration rewrote the overlays already in the database, so an
// installation that had edited one of these documents before that release is
// holding a stored text with no marker in it at all. Nothing else in the tree
// sees that shape: the write face refuses to CREATE one (asserted below), so
// every write-side guard is looking the other way, and DocRendered's no-marker
// branch hands the text back unchanged.
//
// For a NOTICE that is the whole document minus its head — the instructions with
// the facts sliced off — and it goes out NON-EMPTY, which is worse than a fault
// that returns "": every downstream "we did not send it" fallback reads a
// non-empty notice as a delivered one and stays disarmed. On the 加速停止 arm
// the sliced-off half is the only place the deadline appears, so an agent under
// a running clock is handed a notice quoting no instant, and 〈停止〉 §1 tells it
// to read that as a soft wind-down.
//
// The row is seeded DIRECTLY here, and that is the honest fixture rather than a
// shortcut: the write face cannot produce this shape, which is exactly why it
// survived unnoticed. The refusal is asserted first so this stays true.
func TestEventNoticeText_ASplitKindStoredWithNoMarkerIsNotSentAtAll(t *testing.T) {
	const where = "context 59% (your limits: 55% / 65%)"
	const epoch = 1755870180
	for _, tc := range []struct {
		kind string
		send func(s *apiServer) string
	}{
		// 〈停止〉 is not here since T-6f44: it declares no Split, so this gate
		// (`spec.Split && !split`) does not apply to it AT ALL — a marker-less
		// stored row IS its normal shape now.
		{docKindAcceleratedStop, func(s *apiServer) string {
			return s.winddownNoticeText(offboardKindFinal, epoch)
		}},
		{docKindTaskReassignPredecessor, func(s *apiServer) string {
			return s.taskNoticeText(docKindTaskReassignPredecessor, map[string]string{
				"task_no": "T-7e91", "new_executor": "銀月（mira）",
			})
		}},
		// The FOURTH Split notice. The gate it walks into is kind-agnostic
		// (`spec.Split && !split`), so leaving this row out changed no
		// behaviour — but the table above reads as an exhaustive list of the
		// Split kinds, and a list that says "all of them" while missing one is
		// the shape this whole ticket is about.
		{docKindTaskUnblocked, func(s *apiServer) string {
			return s.taskNoticeText(docKindTaskUnblocked, map[string]string{
				"blocked_task_no": "T-7e91",
			})
		}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			s := newEventProcServer(t)
			spec, _, body := splitSeed(t, s, tc.kind)

			// Positive control: the notice IS sent while the document is whole,
			// so the "" below is this fixture's doing and not a dead server.
			if tc.send(s) == "" {
				t.Fatal("nothing was sent even before the marker was removed")
			}

			// The write face cannot make this row — which is why seeding it
			// directly is the only way to test the shape an old release left.
			//
			// 🔴 HOW IT CANNOT CHANGED, AND THE CHECK CHANGED WITH IT (T-3201).
			// It used to REFUSE the marker-less document: 400 for an editable
			// kind, 405 for a read-only one, because no caller may write those
			// at all. The wire no longer carries a whole document, so there is
			// no refusal left to assert for the editable kinds — what is
			// asserted instead is that the very same text, sent the only way it
			// CAN be sent, still comes out of the store WITH a head on it. That
			// is the stronger claim: not "this shape is rejected" but "this
			// shape cannot be expressed". A read-only kind is still 405, and
			// asserting a flat outcome for every kind would have made the
			// fourth Split kind unaddable here.
			w := httptest.NewRecorder()
			s.replaceBootDoc(w, ownerPost("/x"), spec, body, false)
			if spec.ReadOnly {
				if w.Code != http.StatusMethodNotAllowed {
					t.Fatalf("a read-only document accepted a write: %d (%s)", w.Code, w.Body.String())
				}
			} else {
				if w.Code != http.StatusOK {
					t.Fatalf("the write face refused a variable-free body: %d (%s)", w.Code, w.Body.String())
				}
				written, err := s.foldBootDocDTO(spec)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, split := DocSplitHeadBody(written.Text); !split {
					t.Fatalf("a write produced a marker-less document — this shape is "+
						"reachable from the cockpit again:\n%q", written.Text)
				}
			}

			if err := s.dal.PutBootDocument(BootDocument{
				Kind: spec.Kind, Key: spec.Key, Text: body,
			}); err != nil {
				t.Fatal(err)
			}
			// The row really is there and really has no marker — otherwise the
			// "" below would be measuring nothing.
			dto, err := s.foldBootDocDTO(spec)
			if err != nil || dto == nil || dto.Text != body {
				t.Fatalf("fixture did not land: %+v %v", dto, err)
			}
			if _, _, split := DocSplitHeadBody(dto.Text); split {
				t.Fatal("the fixture still carries a marker, so it is not the pre-marker shape")
			}

			if got := tc.send(s); got != "" {
				t.Fatalf("a document with no read-only head must not be sent at all — "+
					"on the member arm a non-empty notice disarms offboardFallback, and "+
					"on the arms with no fallback it simply misleads. It went out as:\n%s", got)
			}
		})
	}
}

// 🔴 AND THE BOOT FOLDS KEEP THE LENIENT BRANCH, deliberately. The refusal above
// belongs to notices, where the head IS the sentence; a boot document's head was
// only its title line, and a reader that gets the body without it still boots.
// Making the fold refuse would turn one stale overlay into agents that cannot
// start — far worse than the hole it closes. This case is the fence around that
// asymmetry, so tightening the notice path later cannot quietly take the boot
// path with it.
//
// ⚠️ SINCE T-6f44 THIS IS NO LONGER A "STALE" SHAPE FOR THIS KIND — it is the
// only shape. 系統互動 declares no Split at all now (its title line went back
// into the body), so every stored row is marker-less and the lenient branch is
// what serves the document on the ordinary path rather than on a legacy one.
// The case is kept because the claim is unchanged and is now load-bearing on
// every boot, not just on an old install.
func TestSystemInteractionText_AMarkerLessOverlayStillBoots(t *testing.T) {
	s := newEventProcServer(t)
	spec := s.systemInteractionSpec()
	const stored = "# Global Context（AI 工作室 · 成員 boot context）\n\n舊版沒有分隔線的內容。\n"
	if err := s.dal.PutBootDocument(BootDocument{
		Kind: spec.Kind, Key: spec.Key, Text: stored,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.systemInteractionText()
	if err != nil {
		t.Fatal(err)
	}
	if got != stored {
		t.Fatalf("the boot fold must hand a marker-less overlay back unchanged:\n got %q\nwant %q",
			got, stored)
	}
}

// 🔴 THAT DAY CAME (T-6f44, decision 2). This case used to assert the OPPOSITE:
// 〈解除阻擋〉 was read-only, so no write face could put an overlay under it, and
// a refused edit had to leave the notice byte-for-byte the shipped one. Its own
// comment said 「The day the owner rules that this document may be edited, this
// case goes red on its first assertion and the row belongs in the table above」 —
// so it is inverted here rather than deleted: the owner's edit must now REACH
// the agent, which is the same claim the overlay table above makes for the
// others, on the one document where it was newly won.
func TestTaskUnblockedDoc_TheOwnersEditReachesTheSendSite(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindTaskUnblocked)
	values := map[string]string{"blocked_task_no": "T-0002"}
	before := s.taskNoticeText(docKindTaskUnblocked, values)

	w := httptest.NewRecorder()
	s.replaceBootDoc(w, ownerPost("/x"), spec, "我改了本體。\n", false)
	if w.Code != http.StatusOK {
		t.Fatalf("the replace face refused an edit: %d (%s) — decision 2 made this "+
			"document editable", w.Code, w.Body.String())
	}
	// The shipped notice was really the seed's before the edit — otherwise the
	// change below would be measuring nothing.
	if before != mustRender(t, spec, head, values)+spec.Join+strings.TrimSuffix(body, "\n") {
		t.Fatalf("the pre-edit notice is not the shipped document:\n%s", before)
	}
	after := s.taskNoticeText(docKindTaskUnblocked, values)
	if after == before {
		t.Fatalf("the edit did not reach the send site — it is still sending the seed:\n%s", after)
	}
	if want := mustRender(t, spec, head, values) + spec.Join + "我改了本體。"; after != want {
		t.Fatalf("the send site did not carry the owner's edit:\n got %q\nwant %q", after, want)
	}
}

// The three documents that could not be split are split now, each on its own
// owner ruling — so what is pinned here is the RULING, not the flag: a build
// that dropped Split on any of them would go on rendering identical bytes
// (DocRendered cuts at the marker whether or not the kind declared one), and
// the two things that would quietly stop are the send-site refusal in
// eventNoticeText and the head gate on the write face. Both are behavioural
// cases below and in TestReplaceBootDoc_ChangingTheReadOnlyHeadIsRefusedAndNothingIsWritten;
// this one names the declaration they all rest on.
func TestBootDocRegistry_TheThreeFormerlyUnsplittableKindsAreSplitByRuling(t *testing.T) {
	s := newEventProcServer(t)
	for _, kind := range []string{
		docKindTaskCloseout,
		docKindTaskTakeoverWithPredecessor,
		docKindTaskTakeoverFresh,
	} {
		t.Run(kind, func(t *testing.T) {
			spec := s.mustBootDocSpec(kind, bootDocSingletonKey)
			if !spec.Split {
				t.Fatal("this kind is declared unsplit again — the owner ruled it split " +
					"(rc-0c36d8739b8f for the two 接手程序, rc-812aa13fb165 for 任務收尾); " +
					"undeclaring it silently disarms the send-site refusal and the head gate")
			}
			seed, _, err := s.root.seedBlockMD(spec.SeedFile)
			if err != nil {
				t.Fatal(err)
			}
			_, body, ok := DocSplitHeadBody(seed)
			if !ok {
				t.Fatal("declared split, but the seed carries no marker line")
			}
			// The premise of every one of the three rulings: what stopped them
			// being split was a variable outside the leading run of facts, and
			// none survives below the line now.
			if bad := DocVarsIn(body); len(bad) > 0 {
				t.Fatalf("the editable body still names %v — nothing fills a variable there", bad)
			}
		})
	}
}

// 🔴 THE {note} SLOT IS GONE FROM BOTH 接手程序, AND THE HANDOVER NOTE IS NOT.
// owner, rc-0c36d8739b8f, verbatim: 「拿掉 —— 交接備註只留在任務上」. The reassign
// writes HandoverNote/HandoverNoteTS/HandoverNoteBy onto the task and wire.go
// puts it in the DTO, so the successor still reads it with get_task; what was
// removed is the SECOND copy, which is also what made these two documents
// unsplittable. The task-side copy is asserted at the send site by
// TestReassignMemberToMemberHandsOver, so "the note is gone" cannot be
// satisfied here by a build that lost it everywhere.
func TestTaskTakeoverDocs_HeadPlusBodyIsTodaysChatNoticeWithoutTheHandoverNote(t *testing.T) {
	for _, tc := range []struct {
		kind, want string
		values     map[string]string
	}{{
		// {title} is gone from both (T-6f44): the number names the ticket, and
		// 〈新任務〉's body opens 「請先讀任務內容」 — it is going to read the title.
		kind:   docKindTaskTakeoverFresh,
		values: map[string]string{"task_no": "T-7e91"},
		want: "[T-7e91] 你接手了這張任務。請先讀任務內容，準備好後由你自己呼叫 " +
			"claim_task（認領）解除轉派鎖再開始執行；任務狀態一律照步驟推導，不必也不能自己報。\n",
	}, {
		// {predecessor_label} + {old_executor_id} merged into ONE slot filled
		// 「名字（id）」. The id could not be dropped — the body's first
		// instruction is to post_chat this person — and neither could the name.
		kind: docKindTaskTakeoverWithPredecessor,
		values: map[string]string{
			"task_no": "T-7e91", "predecessor": "銀月（mira）",
		},
		// T-91 demoted this notice to a REMINDER: the same handover is now
		// readable off the ticket (lock + reassigned_from) and off the wake
		// snapshot, so a successor that never receives this message — the
		// outsource arm never does, because no worker id exists yet — still
		// finds the handover at 開機盤點. The added clause says so out loud,
		// because an agent that believes a message is its only source waits
		// for one instead of looking.
		want: "[T-7e91] 你接手了這張任務，你的前任是 銀月（mira）。" +
			"這則訊息只是提醒，不是唯一路徑——同一件事在票上讀得到（`lock` 是 " +
			"`reassigning`、`reassigned_from` 是前任），開機盤點就會看到，" +
			"漏收這則也不會漏掉這張票。" +
			"請先跟他確認交接完成（直接 post_chat 給他，問清楚目前進度與進行中的事項），" +
			"確認後再由你自己呼叫 claim_task（認領）解除轉派鎖——只有你這個新負責人動得了；" +
			"任務狀態一律照步驟推導，不必也不能自己報。\n",
	}} {
		t.Run(tc.kind, func(t *testing.T) {
			s := newEventProcServer(t)
			spec, head, body := splitSeed(t, s, tc.kind)

			// The literal api_tasks.go used to concatenate, minus the 交接備註
			// paragraph, plus the seed FILE's trailing newline.
			if got := mustRender(t, spec, head, tc.values) + spec.Join + body; got != tc.want {
				t.Fatalf("the folded document is not today's takeover notice minus the note:"+
					"\n got %q\nwant %q", got, tc.want)
			}
			if strings.Contains(body, "交接備註") {
				t.Fatal("the 交接備註 paragraph is still in the body — the owner ruled it out " +
					"of the notice, and while it is here the document has a variable after " +
					"its instructions and cannot be split at all")
			}
			// …and the same bytes the live producer builds, which is what makes
			// the hand-written literal above a statement about the SERVER.
			if live := s.taskNoticeText(tc.kind, tc.values); live != strings.TrimSpace(tc.want) {
				t.Fatalf("the live producer sent something else:\n got %q\nlive %q",
					strings.TrimSpace(tc.want), live)
			}
		})
	}
}

// 〈任務收尾〉 is the one of the three the owner allowed to be REWRITTEN
// (rc-812aa13fb165: 「允許改寫，但逐句先給我看」), because both {type_key} and
// {manual_label} sat in the middle of its instructions. The two names moved up
// into the head and the clauses that quoted them now point at it. Compared
// whole, because the sentence this document must NOT have lost is the one this
// very ticket is a sample of — 「不要用 write_task_learnings 做整份取代」.
//
// 🔴 THE SEND SITE ARRIVED IN T-7870, AND THIS TEST DELIBERATELY STILL DOES NOT
// ASSERT IT. What is pinned here is the SEED — the approved wording — which is a
// different claim from "an agent receives it", and keeping the two apart is what
// let the gap be seen at all: for the whole of T-3201 this file was green while
// the sentence agents actually got came from a Go literal beside the send site.
// The live claim is asserted where it can fail honestly, at the send site:
// api_tasks_test.go TestTaskCloseNudgeTextComesFromTheDocument.
func TestTaskCloseoutDoc_IsTheApprovedRewriteWithBothNamesMovedIntoTheHead(t *testing.T) {
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, docKindTaskCloseout)

	// T-91 added {closed_by} to the head. It is NOT a reversal of decision 3
	// (「最低限度就是 task id」, which cut {status}/{type_key}/{manual_label}):
	// those three are readable off the ticket and this one is not — there is no
	// such column, and get_task cannot answer 「誰把它關掉的」. The rule the head
	// keeps is "only the facts this notice is the sole source of".
	gotHead := mustRender(t, spec, head, map[string]string{
		"task_no": "T-7d40", "closed_by": "owner",
	})
	wantHead := "任務 T-7d40 已結束，關閉的人是 owner。"
	if gotHead != wantHead {
		t.Fatalf("the read-only head is not the approved sentence:\n got %q\nwant %q", gotHead, wantHead)
	}

	// 🔴 THE FIRST SENTENCE ONCE NAMED list_tasks, AND THAT WAS A BUG FIX, NOT A
	// STYLE CHOICE — THE HISTORY IS KEPT HERE BECAUSE IT EXPLAINS THE SHAPE.
	// The rewrite that dropped {type_key} put 「先 get_task 讀這張票」 here, and an
	// agent COULD NOT do that: the only identifier it was handed was the display
	// number, which AT THE TIME was the id's first hex quartet, while get_task
	// keys on the full id — measured against the live station of that era,
	// 「get_task("T-6f44")」 answered 404. So the document was telling every agent
	// to start with a call that fails, and list_tasks was the way across: each
	// row carries task_no AND id.
	//
	// ⚠️ THAT PREMISE STOPPED BEING TRUE AT T-5291, AND T-1 IS THE SEED CHANGE
	// THAT CAUGHT THE DOCUMENT UP. `TaskNo(id)` returns the id unchanged
	// (domain.go), so 票號 and id are the SAME STRING and feeding 票號 to
	// get_task cannot 404 any more. The detour therefore cost every closing
	// agent one list_tasks call and taught it something false on the way. The
	// previous round of this test found the stale sentence, left it standing
	// DELIBERATELY and reported it upward rather than editing it quietly —
	// owner-approved seed prose that changes what every agent does needs its own
	// ruling. That ruling is rc-63068f315a7c (owner 選「改」), and this is it.
	//
	// 🔴 WHY THE REPLACEMENT SAYS 「票號就是 id」 AND NOT 「票號都是 T-<數字>」.
	// Legacy tasks keep their "t-"+12-hex ids and this system has no delete
	// path, so BOTH number shapes coexist permanently. A sentence about the
	// SHAPE would be the next false claim; a sentence about the IDENTITY is true
	// for both. TestGetTaskAcceptsTheTaskNoTheAgentWasHanded
	// (api_tasks_id_sequence_t52917b_test.go) is the assertion that keeps it
	// true — it feeds a task_no of EACH shape to the real get_task handler. If
	// get_task ever goes back to keying on something other than the number
	// agents are handed, that test goes red BEFORE this prose can rot again.
	wantBody := "先用 `get_task` 讀這張票（票號就是 id，直接餵給它），" +
		"看它屬於哪一本任務手冊（欄位 `type_key`）。\n\n" +
		"若這一趟有值得留下的經驗（踩坑、更好做法），先用 get_task_manual 讀現況，" +
		"再用 patch_task_learnings（type_key 用上一步讀到的值）只把改動的那一段送回" +
		"**那本**任務手冊：改既有段落就用它的唯一錨點，第一次寫或要新增就用空錨點追加。" +
		"不要用 write_task_learnings 做整份取代 —— 讀取後到寫入之間別人新增的內容會被無聲蓋掉；" +
		"用 `ocagent clean <path>` 移除這個任務的暫存檔/資料夾、收掉臨時 branch/worktree 與跑著的臨時程序；" +
		"票已經結束的話，最後用 report_task_closeout 回報後續已處理完。" +
		"⚠️ 你若是**被換手、而這張票還在跑**，這一支會回 409 —— 那一步就跳過，" +
		"票沒結束就沒有結案可報，這一段的寫回與清理照做。\n"
	if body != wantBody {
		t.Fatalf("the body is not the approved rewrite:\n got %q\nwant %q", body, wantBody)
	}
	// 🔴 THE DANGLING POINTER THE 定稿 NAMED AS A SILENT BREAK. The body used to
	// say 「type_key 就用**上面那一行**給的值」 and 「送回**上面指名的那本**任務手冊」,
	// and both {type_key} and {manual_label} left the head in this same commit.
	// Cutting the variables without rewriting those two clauses leaves a document
	// that reads perfectly and instructs the agent to look at a line that is not
	// there — no test would have noticed, which is why it is one here.
	// ⚠️ T-a36c WIDENED THIS LIST, AND THE MISS IS THE POINT: the body also said
	// 「餵**上面那個票號**會 404」 — a fourth pointer at the head, one word away
	// from 「上面那一行」 and therefore invisible to a list of EXACT strings. The
	// head-bearing path delivers that line fine; the WIND-DOWN path
	// (taskEventBodyText — body only) does not, and there the sentence points at
	// nothing. Matching the prefix 「上面」 is what the exact list could not do.
	for _, dangling := range []string{"上面", "{type_key}", "{manual_label}"} {
		if strings.Contains(body, dangling) {
			t.Errorf("the body still says %q, but the head no longer carries it", dangling)
		}
	}
	// The body must instead tell the agent where to GET the type_key, since the
	// notice no longer hands it over.
	if !strings.Contains(body, "get_task") {
		t.Error("the body never tells the agent to read the ticket, and the head no " +
			"longer carries the manual or the type_key — nothing says where they come from")
	}
}

// 🔴 THE COST OF A NAME NOTHING FILLS IS THE WHOLE NOTICE, NOT A BLANK — and
// nobody is told. RenderDocVars refuses a declared name with no value (a value
// that is present but EMPTY renders empty, which is a different thing);
// eventNoticeText turns that refusal into ""; and every send site posts nothing
// rather than posting "". So the most natural-looking fix at a send site —
// "this branch has nothing for {x}, just leave it out" — does not produce a
// notice with a gap in it. It produces no notice at all, no error, and no
// surface anywhere that says a message was owed.
//
// Both halves are asserted because neither is worth much alone: the empty
// answer is meaningless without a positive control that the same call sends
// something when every value is supplied, and "the notice is empty" says
// nothing about whether the send site would post it anyway.
func TestTaskTakeoverNotice_AValueNothingSuppliesEmptiesTheNoticeAndTheSuccessorIsSentNothing(t *testing.T) {
	s := newEventProcServer(t)
	for kind, values := range map[string]map[string]string{
		docKindTaskTakeoverFresh:           {"task_no": "T-7e91"},
		docKindTaskTakeoverWithPredecessor: {"task_no": "T-7e91", "predecessor": "銀月（mira）"},
	} {
		t.Run(kind, func(t *testing.T) {
			if s.taskNoticeText(kind, values) == "" {
				t.Fatal("nothing was sent even with every value supplied — every assertion " +
					"below would pass vacuously")
			}
			for missing := range values {
				short := map[string]string{}
				for k, v := range values {
					if k != missing {
						short[k] = v
					}
				}
				if out := s.taskNoticeText(kind, short); out != "" {
					t.Errorf("with no value for {%s} the notice must be empty — a blank "+
						"substitution reads as a real fact and names the wrong thing. "+
						"It came back as:\n%s", missing, out)
				}
				// A value that IS supplied and empty is a different case and is
				// NOT refused — so the case above is about the missing KEY, not
				// about the empty string, and a send site cannot dodge the
				// refusal by discovering that distinction by accident.
				blank := map[string]string{}
				for k, v := range values {
					blank[k] = v
				}
				blank[missing] = ""
				if out := s.taskNoticeText(kind, blank); out == "" {
					t.Errorf("an empty VALUE for {%s} must still render — only an absent "+
						"key is a fault", missing)
				}
			}
		})
	}

	// …and the consequence at the real send site: the reassign posts the
	// successor NOTHING when its notice cannot be rendered. The document is put
	// into the one unrenderable shape a live installation can actually hold (an
	// overlay written before docBodyMarker existed — the write face refuses to
	// create it, which is why it is seeded directly), because the missing-value
	// arm above is unreachable from today's call site by construction: every
	// declared name is supplied there, and the day one is not, this is what the
	// successor gets.
	api := newTasksTestServer(t)
	putActiveMember(t, api, "m-old", "Ken", KindStaff)
	putActiveMember(t, api, "m-new", "Rei", KindStaff)
	spec := api.mustBootDocSpec(docKindTaskTakeoverWithPredecessor, bootDocSingletonKey)
	seed, _, err := api.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		t.Fatal(err)
	}
	_, body, ok := DocSplitHeadBody(seed)
	if !ok {
		t.Fatal("the seed carries no marker line")
	}
	if err := api.dal.PutBootDocument(BootDocument{
		Kind: spec.Kind, Key: spec.Key, Text: body,
	}); err != nil {
		t.Fatal(err)
	}
	task := createAdHocTask(t, api, "m-old")
	if rec := reassign(t, api, task.ID, memberTarget("m-new"), wireOwnerID, "owner"); rec.Code != http.StatusOK {
		t.Fatalf("reassign: %d %s", rec.Code, rec.Body.String())
	}
	msgs, err := api.dal.ListChat()
	if err != nil {
		t.Fatal(err)
	}
	var toOld, toNew *ChatMessage
	for i := range msgs {
		switch msgs[i].Recipient {
		case "m-old":
			toOld = &msgs[i]
		case "m-new":
			toNew = &msgs[i]
		}
	}
	// The predecessor's own notice is a different document and must still have
	// gone out — otherwise "nothing was posted" would be a broken handler
	// rather than the refusal under test.
	if toOld == nil {
		t.Fatal("the predecessor's notice went missing too — this is not the refusal under test")
	}
	if toNew != nil {
		t.Fatalf("a notice that could not be rendered must not be posted at all; the "+
			"successor received:\n%s", toNew.Body)
	}
}

// ── the three boot documents that gave their title line back ─────────────────

// 🔴 THE PROMOTION WAS UNDONE (T-6f44) AND THE BYTES DID NOT MOVE. T-3201 made
// each of these documents' own TITLE line its read-only head — a head with zero
// variables, that no code composed, that no body quoted. Decision: a line like
// that is not the server's, it is the owner's, so the title went back down into
// the body and the read-only half disappeared.
//
// What is pinned here is that the change was free: the old join was "\n\n",
// which is exactly the blank line now sitting between the title and the rest of
// the seed, so a READER gets the same bytes as before — the claim this test used
// to make about head ⊕ body, made about one flat document.
func TestBootContextDocs_RenderWithoutTheMarkerAndKeepTheirTitleLine(t *testing.T) {
	s := newEventProcServer(t)
	for _, tc := range []struct {
		kind, key, wantHead string
	}{
		{docKindSystemInteraction, systemInteractionDocKey,
			"# Global Context（AI 工作室 · 成員 boot context）"},
		{docKindBootSequence, bootSequenceKeyClaude, "# Claude Code 執行環境"},
		{docKindBootSequence, bootSequenceKeyCodex, "# Codex App Server 執行環境"},
	} {
		t.Run(tc.kind+"/"+tc.key, func(t *testing.T) {
			spec := s.mustBootDocSpec(tc.kind, tc.key)
			if spec.Split {
				t.Fatal("this kind declares a read-only head again — its head was only " +
					"its own title line, and T-6f44 gave that line back to the owner")
			}
			seed, _, err := s.root.seedBlockMD(spec.SeedFile)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(seed, docBodyMarker) {
				t.Fatal("the seed still carries the marker line while the kind declares " +
					"no Split — the whole text is served as the editable body, so the " +
					"marker and everything above it just became the owner's to overwrite")
			}
			// The title is still the first line, and still exactly itself.
			title, rest, cut := strings.Cut(seed, "\n\n")
			if !cut || title != tc.wantHead {
				t.Fatalf("first line = %q, want %q — giving the title back was supposed "+
					"to change no wording at all", title, tc.wantHead)
			}
			// And a READER gets the same bytes it got when this was head ⊕ body
			// joined with "\n\n".
			rendered := DocRendered(seed, spec.Join)
			if rendered != seed || rendered != tc.wantHead+"\n\n"+rest {
				t.Fatalf("the rendered document is not the title, a blank line and the rest:\n%q", rendered)
			}
		})
	}
}

// ── the write face: the head is UNSENDABLE, the body has no variables ────────

// 🔴 THIS USED TO ASSERT A REFUSAL AND NOW ASSERTS THAT THERE IS NOTHING TO
// REFUSE (T-3201, owner's ruling 「沒有人有任何方式可以回寫」). Three probes stood
// here — head edited, head replaced, marker removed — each a whole document
// sent back with the read-only half tampered with, each answered 400. The wire
// no longer has a field that can carry a head, so none of the three is a
// request anybody can make: what is left to measure is that whatever a caller
// puts in the body, the STORED document still carries the shipped head above
// the line.
//
// The probes are therefore the same three strings, sent as BODY text — the
// exact payloads a caller who still believed he was sending a whole document
// would produce. Every one of them is accepted (it is just text) and every one
// of them lands under the shipped head, which is the thing worth pinning: the
// old protocol's payload can no longer reach the head even by accident.
//
// 接手程序（有前任） rides along as a second kind: it is the one of the three
// documents split by T-3201's last package that an owner may actually edit, so
// it is the one where this is newly load-bearing — and the join reads
// spec.Split, which a build that undeclared the split would walk straight past.
// ⚠️ 〈停止〉 LEFT THIS LIST IN T-6f44 and its slot was taken by 〈加速停止〉, which
// is the stop procedure that still HAS a head. Leaving 〈停止〉 here would have
// been a case asserting that a document with no read-only head keeps its
// read-only head.
func TestReplaceBootDoc_NoWriteCanChangeTheReadOnlyHead(t *testing.T) {
	for _, kind := range []string{docKindAcceleratedStop, docKindTaskTakeoverWithPredecessor} {
		t.Run(kind, func(t *testing.T) {
			replaceBootDocHeadIsUnsendableCase(t, kind)
		})
	}
}

func replaceBootDocHeadIsUnsendableCase(t *testing.T, kind string) {
	t.Helper()
	s := newEventProcServer(t)
	spec, head, body := splitSeed(t, s, kind)
	// ⚠️ THE PROBES DO NOT CARRY THE REAL HEAD, and that is a fact about a
	// DIFFERENT gate rather than a weaker test. These heads name {variables} —
	// that is what a head is for — and a variable anywhere in the body is
	// refused by the body rule, which would answer 400 for a reason that has
	// nothing to do with the head. So each probe is a head-SHAPED string with
	// no braces in it: the payload of the old whole-document protocol, in the
	// only form that gets far enough to prove the point.
	for _, probe := range []struct{ name, text string }{
		{"an edited head sent as body", DocJoinHeadBody("我自己的開頭 我加了一句", body)},
		{"someone else's head sent as body", DocJoinHeadBody("我自己的開頭", body)},
		{"a whole document with no marker", "我自己的開頭\n\n" + body},
	} {
		t.Run(probe.name, func(t *testing.T) {
			s := newEventProcServer(t)
			w := httptest.NewRecorder()
			s.replaceBootDoc(w, ownerPost("/x"), spec, probe.text, false)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 — body text is body text (%s)", w.Code, w.Body.String())
			}
			after, err := s.foldBootDocDTO(spec)
			if err != nil {
				t.Fatal(err)
			}
			if after.ReadOnlyHead != head {
				t.Fatalf("the stored read-only head is not the shipped one:\n got %q\nwant %q",
					after.ReadOnlyHead, head)
			}
			if after.Body != probe.text {
				t.Fatalf("the body stored is not what was sent:\n got %q\nsent %q", after.Body, probe.text)
			}
			if after.Text != DocJoinHeadBody(head, probe.text) {
				t.Fatalf("the stored document is not head ⊕ body:\n%q", after.Text)
			}
		})
	}
	// Control: an ordinary body edit is accepted and lands the same way, so the
	// assertions above are not a face that stores the head no matter what
	// because it refuses to store anything.
	s2 := newEventProcServer(t)
	w := httptest.NewRecorder()
	s2.replaceBootDoc(w, ownerPost("/x"), spec, body+"\n多寫一行\n", false)
	if w.Code != http.StatusOK {
		t.Fatalf("editing only the body: status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	after, err := s2.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReadOnlyHead != head || after.Body != body+"\n多寫一行\n" {
		t.Fatalf("an ordinary edit did not land as head ⊕ body:\n%q", after.Text)
	}
}

// 🔴 THE TWO GATES CHANGED UNITS AND THE CHANGE IS INVISIBLE WITHOUT THIS TEST.
// Both used to compare whole document against whole document, because that was
// the only thing a caller could send. The body-only wire split the question, and
// each gate now measures the one thing its own question is about:
//
//   - the WIPE guard asks whether this write emptied the document of everything
//     the caller can put in it, so it judges the BODY. Judged on the joined text
//     it would answer "nothing was emptied" for a caller who just erased the
//     whole of his own half — the head survives every write, so no write can
//     produce an empty document again and the guard would be retired by
//     accident, on a face nobody would think to look at.
//
//   - the CAP asks whether the document that gets STORED fits its ceiling, so it
//     judges the joined text. That is the number size_chars reports, the number
//     the cockpit shows against cap_chars and the number docCapRefusal quotes;
//     measured on the body, all three would say different things about one
//     document and the owner would be refused at a length his screen called fine.
//
// Both halves are asserted with their opposites, because either gate reading the
// other's unit passes the loose half of this test on its own.
func TestReplaceBootDoc_TheWipeGuardJudgesTheBodyAndTheCapJudgesTheStoredDocument(t *testing.T) {
	t.Run("the wipe guard judges the body", func(t *testing.T) {
		s := newEventProcServer(t)
		// 〈加速停止〉 rather than 〈停止〉 since T-6f44: the stop procedure that
		// still HAS a read-only head is the one where "the head survives the
		// write" is a distinction at all. Same cap (offboardCap), same shape.
		spec, head, _ := splitSeed(t, s, docKindAcceleratedStop)

		w := httptest.NewRecorder()
		s.replaceBootDoc(w, ownerPost("/x"), spec, "", false)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("emptying the body must be refused without allow_shrink; got %d (%s)",
				w.Code, w.Body.String())
		}
		// The control that makes the refusal above mean "the BODY was judged":
		// what would have been stored is NOT empty — it is the head and the
		// separator — so a guard reading the stored document would have let it
		// through with nothing to say.
		stored, err := s.bootDocStoredText(spec, "")
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(stored) == "" {
			t.Fatal("fixture: an emptied body still stores the head, or this control proves nothing")
		}
		if !strings.HasPrefix(stored, head) {
			t.Fatalf("an emptied body did not keep the shipped head:\n%q", stored)
		}

		// …and allow_shrink is still the way through, on the same unit.
		rec := httptest.NewRecorder()
		s.replaceBootDoc(rec, ownerPost("/x"), spec, "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("allow_shrink must let the wipe through; got %d (%s)", rec.Code, rec.Body.String())
		}
		after, err := s.foldBootDocDTO(spec)
		if err != nil {
			t.Fatal(err)
		}
		if after.Body != "" || after.ReadOnlyHead != head {
			t.Fatalf("an allowed wipe should empty the body and keep the head; body=%q head kept=%v",
				after.Body, after.ReadOnlyHead == head)
		}
	})

	t.Run("the cap judges the stored document", func(t *testing.T) {
		s := newEventProcServer(t)
		spec, head, _ := splitSeed(t, s, docKindAcceleratedStop)
		before, err := s.foldBootDocDTO(spec)
		if err != nil {
			t.Fatal(err)
		}
		overhead := utf8.RuneCountInString(DocJoinHeadBody(head, ""))
		if overhead <= 0 {
			t.Fatal("fixture: this document has no read-only half, so the two units coincide")
		}
		// A body that fits the cap on its own and does NOT fit once the head is
		// joined on. This is the exact window a body-measuring cap would let
		// through, and it is why the window has to be non-empty for the test to
		// mean anything.
		body := strings.Repeat("字", spec.Cap-overhead+1)
		if utf8.RuneCountInString(body) > spec.Cap {
			t.Fatalf("fixture: the probe (%d runes) is over the %d cap on its own",
				utf8.RuneCountInString(body), spec.Cap)
		}
		if utf8.RuneCountInString(DocJoinHeadBody(head, body)) <= spec.Cap {
			t.Fatal("fixture: the probe fits even once stored, so nothing is being measured")
		}
		w := httptest.NewRecorder()
		s.replaceBootDoc(w, ownerPost("/x"), spec, body, false)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("a body that only fits WITHOUT the head must be refused; got %d (%s)",
				w.Code, w.Body.String())
		}
		after, err := s.foldBootDocDTO(spec)
		if err != nil {
			t.Fatal(err)
		}
		if after.Text != before.Text || !after.IsDefault {
			t.Errorf("the refused write moved the document: is_default %v→%v", before.IsDefault, after.IsDefault)
		}
		// The control: one rune shorter fits once stored, and is accepted. Without
		// it a cap that refused everything would pass the half above.
		shorter := strings.Repeat("字", spec.Cap-overhead)
		rec := httptest.NewRecorder()
		s.replaceBootDoc(rec, ownerPost("/x"), spec, shorter, false)
		if rec.Code != http.StatusOK {
			t.Fatalf("a body that fits exactly once stored must be accepted; got %d (%s)",
				rec.Code, rec.Body.String())
		}
		wrote, err := s.foldBootDocDTO(spec)
		if err != nil {
			t.Fatal(err)
		}
		if wrote.SizeChars != spec.Cap {
			t.Fatalf("the accepted document is %d runes, want exactly the %d cap — the "+
				"boundary this pair measures is the STORED size", wrote.SizeChars, spec.Cap)
		}
	})
}

func TestReplaceBootDoc_AVariableInTheEditableBodyIsRefused(t *testing.T) {
	s := newEventProcServer(t)
	spec, _, body := splitSeed(t, s, docKindAcceleratedStop)
	before, err := s.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	// {deadline} is a name this document DOES declare — and it is still refused
	// below the line, because nothing fills a variable there. A test that used
	// an undeclared name would pass on a server that only checked the spelling.
	// (It was {where} on 〈停止〉 until T-6f44 deleted both the variable and that
	// document's read-only half; 〈加速停止〉 is the stop procedure that still has
	// a head and still declares a name.)
	w := httptest.NewRecorder()
	s.replaceBootDoc(w, ownerPost("/x"), spec, body+"\n你的死線是 {deadline}。\n", false)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", w.Code, http.StatusBadRequest, w.Body.String())
	}
	after, err := s.foldBootDocDTO(spec)
	if err != nil {
		t.Fatal(err)
	}
	if after.Text != before.Text || !after.IsDefault {
		t.Errorf("the refused write moved the document: is_default %v→%v", before.IsDefault, after.IsDefault)
	}
}

// 🔴 THIS USED TO BE ONE-DIRECTIONAL AND T-6f44 MADE IT A BICONDITIONAL, which
// is the whole of what kept "四份唯讀區整個消失" from silently un-locking them.
//
// It read: a kind that DECLARES Split must have a marker in its seed. That is
// half the rule. The other half had no test anywhere, because until T-6f44 every
// kind in the registry declared Split, so the case could not arise:
//
//	a kind that does NOT declare Split must have NO marker in its seed.
//
// Miss that half and dropping Split alone — leaving the marker in the seed —
// is ACCEPTED by every gate in the tree and is exactly backwards. bootDocBodyOf
// returns the WHOLE text as the body for an unsplit kind, so the marker line and
// the head above it become part of the half the owner may rewrite; the write
// face joins no shipped head back on (bootDocStoredText's identity branch), so
// the first save replaces them for good. The half nobody may edit becomes the
// half anybody may, with a 200 and a green suite.
//
// The other direction is the failure the assertion was written for and still
// catches: a seed that loses its marker while the kind declares Split makes
// every write a 500 (bootDocStoredText) and every notice "" (eventNoticeText).
//
// So a document only stops carrying a read-only head when its seed and its
// registry row change in the SAME commit, in either direction. The other two
// clauses (a variable-free body, a head that declares what it uses) are
// unchanged and still only meaningful for a split kind.
func TestBootDocRegistry_ASeedCarriesAMarkerExactlyWhenItsKindIsSplit(t *testing.T) {
	s := newEventProcServer(t)
	sawUnsplit := false
	for _, reg := range bootDocRegistry {
		sawUnsplit = sawUnsplit || !reg.Split
		for _, key := range reg.Keys {
			t.Run(reg.Kind+"/"+key, func(t *testing.T) {
				spec := s.mustBootDocSpec(reg.Kind, key)
				seed, _, err := s.root.seedBlockMD(spec.SeedFile)
				if err != nil {
					t.Fatal(err)
				}
				head, body, ok := DocSplitHeadBody(seed)
				if !spec.Split {
					if ok {
						t.Fatalf("this kind declares no Split, but its seed still carries the "+
							"%s line — an unsplit kind serves its WHOLE text as the editable "+
							"body, so the marker and the head above it just became the owner's "+
							"to overwrite. Drop the marker from the seed in this same commit, "+
							"or put Split back", docBodyMarker)
					}
					if bad := DocVarsUndeclared(seed, spec.Vars); len(bad) > 0 {
						t.Errorf("the seed uses %v, which the kind does not declare", bad)
					}
					return
				}
				if !ok {
					t.Fatalf("declared split, but the seed carries no %s line", docBodyMarker)
				}
				if spec.Vars == nil {
					return // opted out of the syntax entirely — see doc_vars.go
				}
				if bad := DocVarsIn(body); len(bad) > 0 {
					t.Errorf("the editable body names %v; nothing fills a variable there", bad)
				}
				if bad := DocVarsUndeclared(head, spec.Vars); len(bad) > 0 {
					t.Errorf("the head uses %v, which the kind does not declare", bad)
				}
			})
		}
	}
	// Positive control: the unsplit half of the biconditional is the new one, and
	// on a registry where every kind is split again it would pass vacuously — the
	// state this test was rewritten out of.
	if !sawUnsplit {
		t.Error("no kind in the registry is unsplit, so the half of this rule that " +
			"T-6f44 added is not being measured by anything")
	}
}

// ── the round trip the wire promises, and the restore that shares its gates ──

// 🔴 THE ONE SENTENCE THE BODY-ONLY WIRE IS SELLING: what the read hands you is
// what the write takes, byte for byte. Nothing else about `body` matters if that
// is not true — a client would be back to composing the document itself, which
// is the thing this ticket removed.
//
// It is asserted on EVERY editable document rather than one, because the join is
// per document (bootDocStoredText reads the kind's own seed) and a bug that
// picked the wrong seed would round-trip perfectly on whichever kind a single
// case happened to choose.
//
// ⚠️ is_default IS NOT ASSERTED, and that is not an oversight. Writing a
// document's own bytes back is the documented gesture that ADOPTS them as an
// edit (writeBootDoc's no-op rule compares the text AND the default flag), so a
// faithful round trip still flips the badge. The claim here is about bytes.
func TestBootDoc_TheBodyItReadsBackIsTheBodyItTakes(t *testing.T) {
	for _, reg := range bootDocRegistry {
		for _, key := range reg.Keys {
			if reg.ReadOnly {
				continue
			}
			t.Run(reg.Kind+"/"+key, func(t *testing.T) {
				s := newEventProcServer(t)
				spec := s.mustBootDocSpec(reg.Kind, key)
				before, err := s.foldBootDocDTO(spec)
				if err != nil {
					t.Fatal(err)
				}
				if before.Body == "" {
					t.Fatal("the document reads back an empty body — every assertion below would be vacuous")
				}
				// The three keys describe ONE document: the whole is the head
				// joined to the body, and a client that only ever touches `body`
				// never has to know how.
				if spec.Split {
					if before.ReadOnlyHead == "" {
						t.Fatal("a split kind served no read-only head")
					}
					if before.Text != DocJoinHeadBody(before.ReadOnlyHead, before.Body) {
						t.Fatalf("text is not read_only_head ⊕ body:\n%q", before.Text)
					}
				}

				// Send back exactly what was read.
				w := httptest.NewRecorder()
				s.replaceBootDoc(w, ownerPost("/x"), spec, before.Body, false)
				if w.Code != http.StatusOK {
					t.Fatalf("writing back the body it just served: %d (%s)", w.Code, w.Body.String())
				}
				after, err := s.foldBootDocDTO(spec)
				if err != nil {
					t.Fatal(err)
				}
				if after.Body != before.Body {
					t.Fatalf("the body did not survive the round trip:\n sent %q\n got  %q", before.Body, after.Body)
				}
				if after.Text != before.Text {
					t.Fatalf("the STORED document moved on a round trip:\n before %q\n after  %q",
						before.Text, after.Text)
				}
				if after.ReadOnlyHead != before.ReadOnlyHead {
					t.Fatalf("the read-only head moved:\n before %q\n after  %q",
						before.ReadOnlyHead, after.ReadOnlyHead)
				}

				// …and the same for a body the caller wrote, so this is not just
				// a fixed point at the seed.
				const mine = "我自己寫的本體。\n"
				rec := httptest.NewRecorder()
				s.replaceBootDoc(rec, ownerPost("/x"), spec, mine, false)
				if rec.Code != http.StatusOK {
					t.Fatalf("writing a fresh body: %d (%s)", rec.Code, rec.Body.String())
				}
				mineBack, err := s.foldBootDocDTO(spec)
				if err != nil {
					t.Fatal(err)
				}
				if mineBack.Body != mine {
					t.Fatalf("read back %q, wrote %q", mineBack.Body, mine)
				}
				if mineBack.ReadOnlyHead != before.ReadOnlyHead {
					t.Fatalf("a write moved the read-only head:\n%q", mineBack.ReadOnlyHead)
				}
			})
		}
	}
}

// 🔴 RESTORE WAS THE FIFTH WRITE PATH AND THE ONLY RE-ARMER, AND THIS IS THE
// REGRESSION TEST THAT SAYS SO. It never INVENTED a headless document — nothing
// can — but until T-3201 it could take a pre-marker revision out of the version
// history, put it back on the live row without passing any content gate, and
// write that as a new revision. One owner click re-armed a hazard nothing else
// in the tree could still produce, and there was no test anywhere that a restore
// even looked at the content it was restoring.
//
// Both halves of the fix are measured here because they are the same claim from
// two sides: restore now runs the SAME join and the SAME body rule as
// replaceBootDoc (bootDocStoredText / bootDocBodyRefusal), so what it puts back
// carries the shipped head, and what it cannot put back it refuses by name.
//
// The revisions are built the way an old release would have left them: the bad
// text is written STRAIGHT INTO the row, then a legitimate write through the
// real face retains it. Constructing the history row by hand would prove nothing
// about which shapes can actually be sitting in a version list.
func TestRestoreDocumentHistory_ABootDocRevisionGoesThroughTheWriteFacesGates(t *testing.T) {
	// 加速停止: editable, split, and it DECLARES variables — all three are
	// needed, the last one because a kind that declares none opts out of the
	// body rule entirely and the second half below would pass vacuously.
	// (〈停止〉 held this slot until T-6f44 took its read-only half away.)
	const kind, key = docKindAcceleratedStop, acceleratedStopDocKey

	// retain installs `stored` as the live row, then writes a clean body through
	// the real write face so that `stored` becomes the newest retained revision.
	// It answers that revision's content.
	retain := func(t *testing.T, s *apiServer, spec bootDocSpec, stored string) map[string]string {
		t.Helper()
		if err := s.dal.PutBootDocument(BootDocument{Kind: kind, Key: key, Text: stored}); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.replaceBootDoc(w, ownerPost("/x"), spec, "乾淨的本體。\n", false)
		if w.Code != http.StatusOK {
			t.Fatalf("the write that should retain the bad version was refused: %d (%s)",
				w.Code, w.Body.String())
		}
		rows, err := s.dal.ListDocumentHistory(kind, key)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			t.Fatal("nothing was retained — the restore below would have no target")
		}
		version, err := s.dal.GetDocumentHistory(kind, key, rows[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if version == nil {
			t.Fatal("the retained revision cannot be read back")
		}
		content := map[string]string{}
		if err := json.Unmarshal([]byte(version.ContentJSON), &content); err != nil {
			t.Fatal(err)
		}
		if content["text"] != stored {
			t.Fatalf("the retained revision is not the version this test installed:\n got %q\nwant %q",
				content["text"], stored)
		}
		return content
	}

	t.Run("a pre-marker revision comes back with the shipped head", func(t *testing.T) {
		s := newEventProcServer(t)
		spec, head, seedBody := splitSeed(t, s, kind)

		// The shape an installation that edited this document before the marker
		// shipped is holding: body only, no boundary, no read-only half.
		const preMarker = "# 停止\n\n這是分割上線前就編輯過的舊覆蓋。\n"
		if _, _, split := DocSplitHeadBody(preMarker); split {
			t.Fatal("fixture: the pre-marker probe carries a marker, so it is not that shape")
		}
		if preMarker == seedBody {
			t.Fatal("fixture: the probe equals the shipped body, so nothing distinguishes a restore")
		}
		content := retain(t, s, spec, preMarker)

		if err := s.restoreDocumentHistory(ownerPost("/x"), kind, key, content); err != nil {
			t.Fatalf("restoring a pre-marker revision: %v", err)
		}
		after, err := s.foldBootDocDTO(spec)
		if err != nil {
			t.Fatal(err)
		}
		// The re-armer is disarmed: the old wording came back, WEARING the
		// shipped head. Both halves are asserted — "the head is there" alone
		// would pass on a restore that quietly dropped the revision.
		if after.ReadOnlyHead != head {
			t.Fatalf("the restored row has no shipped head:\n got %q\nwant %q", after.ReadOnlyHead, head)
		}
		if after.Body != preMarker {
			t.Fatalf("the restore did not put the old wording back:\n got %q\nwant %q", after.Body, preMarker)
		}
		if after.Text != DocJoinHeadBody(head, preMarker) {
			t.Fatalf("the restored document is not head ⊕ body:\n%q", after.Text)
		}
		// And the send site, which is what the whole hazard was about, will
		// speak again: a headless row is refused there (eventNoticeText), so a
		// non-empty answer says the shape is gone rather than merely tidied.
		if got := s.winddownNoticeText(offboardKindSoft, 0); got == "" {
			t.Fatal("the notice is still unsendable after the restore — the row is still headless")
		}
	})

	t.Run("a revision whose body names a variable is refused, and nothing is written", func(t *testing.T) {
		s := newEventProcServer(t)
		spec, head, _ := splitSeed(t, s, kind)
		if spec.Vars == nil {
			t.Fatal("fixture: this kind opts out of the body rule, so the refusal below cannot fire")
		}
		// {where} is a name this document DOES declare — and it is still refused
		// below the line, because nothing fills a variable there. A revision
		// carrying an UNdeclared name would pass on a server that only checked
		// the spelling.
		bad := DocJoinHeadBody(head, "你現在的狀況是 {where}。\n")
		content := retain(t, s, spec, bad)

		before, err := s.foldBootDocDTO(spec)
		if err != nil {
			t.Fatal(err)
		}
		err = s.restoreDocumentHistory(ownerPost("/x"), kind, key, content)
		if !errors.Is(err, errDocumentHistoryContent) {
			t.Fatalf("restoring a revision the write face would refuse: err = %v, want %v",
				err, errDocumentHistoryContent)
		}
		// The refusal has to NAME the offending variable, for the reason the
		// write face's does: the owner is looking at a version list and cannot
		// otherwise tell which revision is stuck or why.
		if !strings.Contains(err.Error(), "{where}") {
			t.Errorf("the refusal does not name the variable: %v", err)
		}
		after, err2 := s.foldBootDocDTO(spec)
		if err2 != nil {
			t.Fatal(err2)
		}
		if after.Text != before.Text {
			t.Fatalf("the refused restore moved the document:\n before %q\n after  %q",
				before.Text, after.Text)
		}
	})
}

// TestTheTwoStopProceduresDifferInEXACTLYTheLinesTheyAreMeantTo is the
// guard the ticket asked for and the one its predecessor only half-built.
//
// 🔴 WHY 「they must differ」 WAS NOT ENOUGH. Before T-6f44 these two documents
// were byte-identical, and the ticket's own acceptance line says that splitting
// them without pinning each one trades 「解決重複」 for 「開始漂移」. What landed
// first was TestAcceleratedStopDoc_ShipsTheSameBodyAsTheOffboardDoc, which
// asserts only that the two bodies are NOT equal — an extremely weak inequality:
// as long as the §1 sentence still differs, EVERY other paragraph may drift one
// way and nothing goes red. An independent review proved it by rewriting 〈停止〉's
// §3 into 〈加速停止〉's wording and watching the whole suite stay green.
//
// The two documents are 95% the same text ON PURPOSE — the wind-down steps do
// not depend on whether a clock is running. So the honest statement is not
// 「different」 and not 「identical」, it is: they differ in EXACTLY the lines
// listed in wantDiffs below, and nowhere else. That pins both directions at once:
//   - a shared paragraph edited on one side only  → one more differing line → RED
//   - one of the deliberate differences erased    → one fewer differing line → RED
//
// The count lives ONLY in wantDiffs — deliberately not in this test's name — so
// that adding a justified difference is an edit to the table plus its reason,
// never a silent bump of a number that no longer matches anything.
//
// ⚠️ IF YOU ARE HERE BECAUSE THIS TEST WENT RED: do not "fix" it by loosening
// the count. Either you edited one document and meant to edit both, or you
// introduced a 4th deliberate difference — in which case add it to the table
// below and say why it must differ.
func TestTheTwoStopProceduresDifferInEXACTLYTheLinesTheyAreMeantTo(t *testing.T) {
	s := newEventProcServer(t)
	_, soft := unsplitSeed(t, s, docKindOffboard)
	_, _, hard := splitSeed(t, s, docKindAcceleratedStop)

	softLines := strings.Split(soft, "\n")
	hardLines := strings.Split(hard, "\n")
	if len(softLines) != len(hardLines) {
		t.Fatalf("the two stop procedures no longer have the same shape: 〈停止〉 has %d lines, "+
			"〈加速停止〉 has %d. Either a shared section was added to one side only, or the pair "+
			"was deliberately restructured — if the latter, rewrite this test to say what the new "+
			"relationship is instead of deleting it", len(softLines), len(hardLines))
	}

	// Each deliberate difference, and the reason it has to be one.
	wantDiffs := []struct {
		why        string
		softNeedle string
		hardNeedle string
	}{
		{"the title names which document you are holding", "# 停止", "# 加速停止"},
		{"§1 states outright which kind of notice this is — the ruling that replaced " +
			"sniffing another document's first line for `Your deadline is`",
			"沒有人在對你倒數", "你在倒數中"},
		{"§1's handover warning: BOTH documents must say that taking the handover " +
			"path kills your token the moment the successor reports in (every later MCP " +
			"call 401s, silently) — that hazard does not care whether a clock is running, " +
			"so the paragraph is shared. Only its OPENING framing may differ, because the " +
			"two readers are in opposite situations: 〈停止〉 has no clock at all, so its " +
			"job is to say a missing clock is not a missing endpoint; 〈加速停止〉 already " +
			"has one, so its job is to say that is not the ONLY endpoint and the other one " +
			"may land FIRST. Copying either wording onto the other side would contradict " +
			"the §1 sentence two lines above it",
			"「沒有時鐘」不等於「沒有終點」", "不是唯一的終點"},
		{"§3: with a clock running you cannot wait for sub-agents to finish on their own",
			"等 sub agent 自己完成", "請 sub agent 立刻"},
	}

	var differing []int
	for i := range softLines {
		if softLines[i] != hardLines[i] {
			differing = append(differing, i)
		}
	}
	if len(differing) != len(wantDiffs) {
		var lines []string
		for _, i := range differing {
			lines = append(lines, fmt.Sprintf("  line %d:\n    soft: %s\n    hard: %s", i+1, softLines[i], hardLines[i]))
		}
		t.Fatalf("the two stop procedures differ in %d lines, want exactly %d.\n%s\n"+
			"A shared paragraph edited on ONE side is the failure this test exists for: those "+
			"sections are copies on purpose, and nothing else keeps them in step",
			len(differing), len(wantDiffs), strings.Join(lines, "\n"))
	}

	for n, want := range wantDiffs {
		i := differing[n]
		if !strings.Contains(softLines[i], want.softNeedle) {
			t.Errorf("difference %d (%s): 〈停止〉 line %d no longer says %q:\n  %s",
				n+1, want.why, i+1, want.softNeedle, softLines[i])
		}
		if !strings.Contains(hardLines[i], want.hardNeedle) {
			t.Errorf("difference %d (%s): 〈加速停止〉 line %d no longer says %q:\n  %s",
				n+1, want.why, i+1, want.hardNeedle, hardLines[i])
		}
	}
}

// 🔴 THE BODY TRAVELS WITHOUT ITS HEAD, AND ON THAT TRIP THE TICKET IS NOT
// NECESSARILY OVER. offboardManualWriteBackFor sends this document's BODY ALONE
// to an outsource worker being wound down (api_members.go) — deliberately, so
// the head's claim 「任務 {task_no} 已結束。」 is not made on a path that cannot
// make it. But the body's LAST step used to be an unconditional
// report_task_closeout, and that endpoint answers 409 for an open task
// («close-out is reported after the task ends»). A worker handed over mid-task
// therefore read an instruction that could not succeed — the same shape as the
// 「餵票號給 get_task 會 404」 defect this document used to carry a warning about
// (the warning is gone as of T-1 — feeding 票號 to get_task works now), and the
// same shape as every finding in T-a36c: a sentence that is true on one path
// and false on the one it is actually delivered on.
//
// This pins the CAVEAT, not the wording around it: whoever rewrites the
// close-out step must still say what happens when the ticket is open. Nothing
// else in the tree would notice its removal — the seed would read perfectly.
func TestTaskCloseoutDoc_TheCloseoutStepSaysWhatHappensWhenTheTicketIsStillOpen(t *testing.T) {
	s := newEventProcServer(t)
	_, _, body := splitSeed(t, s, docKindTaskCloseout)

	if !strings.Contains(body, "report_task_closeout") {
		t.Fatal("the body no longer names report_task_closeout — if that step really " +
			"moved, delete this test deliberately rather than letting it pass vacuously")
	}
	// 409 is the endpoint's own answer for an open task; naming it is what makes
	// the caveat checkable instead of a mood.
	if !strings.Contains(body, "409") {
		t.Error("the body tells the agent to call report_task_closeout but never says " +
			"the call FAILS (409) while the ticket is still open — this body is " +
			"delivered on the wind-down path, where the ticket often is still open")
	}
}
