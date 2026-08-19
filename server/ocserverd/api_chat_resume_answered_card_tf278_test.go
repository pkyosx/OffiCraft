package main

import (
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ── T-f278: the answer landed and nobody picked it up ────────────────────────
//
// The state this file exists for: the owner answers a reply card, the server
// releases the hold, and the bound step goes BACK to in_progress — the same
// value a step being actively worked carries. Nothing then tracks it. A card
// answered on Monday sat untouched until Wednesday while every board and every
// status field showed a perfectly normal ticket.
//
// The fix is a POINTER on the wake snapshot, not a state change: releaseCardHold
// keeps doing exactly what it does (an answer may well be 不通過／改做, so the
// server must never read one as completion), and the resuming agent is simply
// told which of its steps are sitting on an already-answered card.

const (
	// The fixture's answered step, written out so the size expectation below is
	// a literal and not a re-computation of the code under test.
	answeredStepName = "整合 uplink 通道" // 12 runes
	// step id ("ts-" + 12 hex) + card id ("rc-" + 12 hex) + the name above.
	wantAnsweredCardChars = 15 + 15 + 12
)

// TestResumeSnapshotNamesStepsSittingOnAnAnsweredCard pins the whole signal on
// one snapshot that carries all three shapes at once: a step whose card the
// owner ANSWERED (must be named), a step still WAITING on its card (must not
// be — that one is the owner's turn, not yours), and a step being worked with
// no card at all (must not be). Overview counts and sizes them, and the peek —
// which carries no rows — still reports both, because an agent that has not
// pulled the snapshot yet is exactly the reader this signal was built for.
func TestResumeSnapshotNamesStepsSittingOnAnAnsweredCard(t *testing.T) {
	api := resumeCtxServer(t)

	answeredTask := createAdHocTask(t, api, "m-exec")
	answeredPlan := submitPlan(t, api, answeredTask.ID, "m-exec", []map[string]any{
		{"name": answeredStepName, "dod": "通道跑得起來"},
		{"name": "收尾", "dod": "文件補完"},
	})
	startFirstStep(t, api, answeredTask.ID, "m-exec")
	card := openGateCard(t, api, answeredTask.ID, "m-exec",
		answeredPlan.Steps[0].ID, "要走哪一條路？")
	if rec := answerCard(t, api, card.ID,
		map[string]any{"option_idx": 1}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}

	waitingTask := createAdHocTask(t, api, "m-exec")
	waitingPlan := submitPlan(t, api, waitingTask.ID, "m-exec", []map[string]any{
		{"name": "等 owner 回覆", "dod": "他回了"},
	})
	startFirstStep(t, api, waitingTask.ID, "m-exec")
	openGateCard(t, api, waitingTask.ID, "m-exec", waitingPlan.Steps[0].ID, "還沒回的問題")

	workingTask := createAdHocTask(t, api, "m-exec")
	submitPlan(t, api, workingTask.ID, "m-exec", []map[string]any{
		{"name": "純粹在做", "dod": "做完"},
	})
	startFirstStep(t, api, workingTask.ID, "m-exec")

	// The two ONE-CONDITION-OFF fixtures. The three above differ from the
	// answered case in BOTH conjuncts at once, so either one alone would still
	// reject them and neither conjunct is actually load-bearing. These two flip
	// exactly one each, and both name a mistake that happens for real:
	//
	// expiredTask — the step IS back at in_progress with a card bound, because
	// expire runs the SAME hold release an answer does; only the card status
	// tells them apart. Drop the card check and an expired ask reads as an
	// answer waiting to be picked up.
	expiredTask := createAdHocTask(t, api, "m-exec")
	expiredPlan := submitPlan(t, api, expiredTask.ID, "m-exec", []map[string]any{
		{"name": "問了但過期", "dod": "另想辦法"},
		{"name": "後面還有", "dod": "做完"},
	})
	startFirstStep(t, api, expiredTask.ID, "m-exec")
	expiredCard := openGateCard(t, api, expiredTask.ID, "m-exec",
		expiredPlan.Steps[0].ID, "沒人回的問題")
	if rec := expireCardReq(t, api, expiredCard.ID, "owner", "owner"); rec.Code != http.StatusOK {
		t.Fatalf("expire: %d %s", rec.Code, rec.Body.String())
	}

	// pickedUpTask — the card IS answered and the step still carries its id
	// (nothing ever clears reply_card_id), but the agent has consumed the answer
	// and moved the step to done. Drop the in_progress check and this pointer
	// never goes away.
	pickedUpTask := createAdHocTask(t, api, "m-exec")
	pickedUpPlan := submitPlan(t, api, pickedUpTask.ID, "m-exec", []map[string]any{
		{"name": "問了也接手了", "dod": "照答案做完"},
		{"name": "還沒開始的下一步", "dod": "做完"},
	})
	startFirstStep(t, api, pickedUpTask.ID, "m-exec")
	pickedUpCard := openGateCard(t, api, pickedUpTask.ID, "m-exec",
		pickedUpPlan.Steps[0].ID, "已經消化掉的問題")
	if rec := answerCard(t, api, pickedUpCard.ID,
		map[string]any{"option_idx": 0}); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	if rec := reportStepStatus(t, api, pickedUpTask.ID, pickedUpPlan.Steps[0].ID,
		"m-exec", StepStatusDone, ""); rec.Code != http.StatusOK {
		t.Fatalf("done: %d %s", rec.Code, rec.Body.String())
	}

	// ANTI-VACUITY: the answered step must really be back at in_progress with an
	// answered card bound. That indistinguishability IS the bug — if the fixture
	// stopped reproducing it, every assertion below would be about nothing.
	answeredView := getTaskView(t, api, answeredTask.ID)
	if answeredView.Steps[0].Status != StepStatusInProgress {
		t.Fatalf("the answered step must be back at in_progress (that is the whole "+
			"problem — it looks like work in flight): %+v", answeredView.Steps[0])
	}
	if answeredView.Steps[0].ReplyCardStatus != replyCardStatusAnswered {
		t.Fatalf("the bound card must read answered: %+v", answeredView.Steps[0])
	}

	// ANTI-VACUITY for the two one-off fixtures: each must differ from the
	// answered case in exactly ONE conjunct, or it stops discriminating.
	expiredView := getTaskView(t, api, expiredTask.ID)
	if expiredView.Steps[0].Status != StepStatusInProgress ||
		expiredView.Steps[0].ReplyCardID != expiredCard.ID ||
		expiredView.Steps[0].ReplyCardStatus != replyCardStatusExpired {
		t.Fatalf("the expired fixture must be in_progress on an EXPIRED bound card "+
			"(only the card status may differ from the answered case): %+v",
			expiredView.Steps[0])
	}
	pickedUpView := getTaskView(t, api, pickedUpTask.ID)
	if pickedUpView.Steps[0].Status != StepStatusDone ||
		pickedUpView.Steps[0].ReplyCardID != pickedUpCard.ID ||
		pickedUpView.Steps[0].ReplyCardStatus != replyCardStatusAnswered {
		t.Fatalf("the picked-up fixture must be DONE on an answered bound card "+
			"(only the step status may differ from the answered case): %+v",
			pickedUpView.Steps[0])
	}

	snap := resumeSnapshot(t, api, "m-exec")
	rows := map[string]resumeTaskDTO{}
	for _, r := range snap.Tasks {
		rows[r.ID] = r
	}
	if len(rows) != 5 {
		t.Fatalf("expected the five seeded tasks on the snapshot, got %d", len(rows))
	}

	named := rows[answeredTask.ID].AnsweredCardSteps
	if len(named) != 1 {
		t.Fatalf("the answered-card step must be named exactly once: %+v", named)
	}
	if named[0].StepID != answeredPlan.Steps[0].ID {
		t.Fatalf("step_id: want %q, got %q", answeredPlan.Steps[0].ID, named[0].StepID)
	}
	if named[0].StepName != answeredStepName {
		t.Fatalf("step_name: want %q, got %q", answeredStepName, named[0].StepName)
	}
	if named[0].CardID != card.ID {
		t.Fatalf("card_id must point at the answered card: want %q, got %q",
			card.ID, named[0].CardID)
	}

	// A card the owner has NOT answered yet is HIS turn — surfacing it here
	// would put the executor's own waiting back on the executor's plate.
	if got := rows[waitingTask.ID].AnsweredCardSteps; len(got) != 0 {
		t.Fatalf("a still-waiting card must not be named: %+v", got)
	}
	if got := rows[workingTask.ID].AnsweredCardSteps; len(got) != 0 {
		t.Fatalf("a plain in_progress step with no card must not be named: %+v", got)
	}
	// An EXPIRED card released the hold the same way an answer does. There is no
	// answer to pick up — naming it would send the agent to read one that does
	// not exist.
	if got := rows[expiredTask.ID].AnsweredCardSteps; len(got) != 0 {
		t.Fatalf("an in_progress step on an EXPIRED card must not be named: %+v", got)
	}
	// The agent already acted on this answer and closed the step. reply_card_id
	// is never cleared, so only the step status can retire the pointer.
	if got := rows[pickedUpTask.ID].AnsweredCardSteps; len(got) != 0 {
		t.Fatalf("a step already moved to done must not be named: %+v", got)
	}

	if snap.Overview.StepsOnAnsweredCard != 1 {
		t.Fatalf("steps_on_answered_card: want 1, got %d",
			snap.Overview.StepsOnAnsweredCard)
	}
	if snap.Overview.StepsOnAnsweredCardChars != wantAnsweredCardChars {
		t.Fatalf("steps_on_answered_card_chars: want %d, got %d",
			wantAnsweredCardChars, snap.Overview.StepsOnAnsweredCardChars)
	}

	// The peek carries no rows, so the overview counts are the ONLY way it can
	// say this — and its headline number must include what the rows will cost.
	peek := peekResumeSize(t, api, "m-exec")
	if peek.Overview != snap.Overview {
		t.Fatalf("peek overview must equal the snapshot's:\n peek=%+v\n full=%+v",
			peek.Overview, snap.Overview)
	}
	otherBlocks := peek.Overview.ChatChars + peek.Overview.TasksDetailChars +
		peek.Overview.RosterChars + peek.Overview.MachinesChars
	if got := peek.EstimatedTotalChars - otherBlocks; got != wantAnsweredCardChars {
		t.Fatalf("estimated_total_chars must carry the answered-card pointers: "+
			"want the other blocks + %d, got them + %d",
			wantAnsweredCardChars, got)
	}
}

// TestResumeSnapshotSaysNothingWhenNoAnswerIsWaiting is the OFF case: the same
// server, one task being worked normally and one card the owner still owes an
// answer on. Every part of the signal must read empty — a pointer that fires on
// the ordinary shape of a working day is noise, and noise is ignored.
func TestResumeSnapshotSaysNothingWhenNoAnswerIsWaiting(t *testing.T) {
	api := resumeCtxServer(t)

	task := createAdHocTask(t, api, "m-exec")
	plan := submitPlan(t, api, task.ID, "m-exec", []map[string]any{
		{"name": "動手做", "dod": "做完"},
		{"name": "問一下", "dod": "問到了"},
	})
	startFirstStep(t, api, task.ID, "m-exec")
	openGateCard(t, api, task.ID, "m-exec", plan.Steps[1].ID, "順便問的問題")

	snap := resumeSnapshot(t, api, "m-exec")
	if len(snap.Tasks) != 1 {
		t.Fatalf("expected the one seeded task, got %d", len(snap.Tasks))
	}
	if got := snap.Tasks[0].AnsweredCardSteps; len(got) != 0 {
		t.Fatalf("nothing is answered — the row must name no step: %+v", got)
	}
	if snap.Overview.StepsOnAnsweredCard != 0 ||
		snap.Overview.StepsOnAnsweredCardChars != 0 {
		t.Fatalf("overview must stay silent: %+v", snap.Overview)
	}
	peek := peekResumeSize(t, api, "m-exec")
	otherBlocks := peek.Overview.ChatChars + peek.Overview.TasksDetailChars +
		peek.Overview.RosterChars + peek.Overview.MachinesChars
	if peek.EstimatedTotalChars != otherBlocks {
		t.Fatalf("with nothing to point at, the estimate must not grow: "+
			"want %d, got %d", otherBlocks, peek.EstimatedTotalChars)
	}
}

// TestResumeProseNamesTheAnsweredCardSignal: a field nobody was told about is a
// field nobody reads. The wake snapshot's own note is where an agent learns what
// its task rows mean, and the peek's note is where it learns what the size
// number is made of — both must name this signal, or it ships invisible.
//
// And neither may call the count a TOTAL. It is taken over the bounded task
// rows only (resumeTasksN), so an agent holding more tasks than the cap can be
// stuck on an answered card and still read 0 — a reader who believes "total"
// draws exactly the wrong conclusion from that 0, which is the failure this
// whole signal exists to prevent. Both notes must say so out loud.
func TestResumeProseNamesTheAnsweredCardSignal(t *testing.T) {
	for _, tc := range []struct {
		name, text, field string
	}{
		{"resumeNote", resumeNote, "answered_card_steps"},
		{"resumeNote/overview", resumeNote, "steps_on_answered_card"},
		{"peekNote", peekNote, "steps_on_answered_card_chars"},
		{"peekNote/count", peekNote, "steps_on_answered_card"},
	} {
		if !strings.Contains(tc.text, tc.field) {
			t.Errorf("%s must name %q — an unexplained field is an unread field",
				tc.name, tc.field)
		}
	}
	if strings.Contains(resumeNote, "`steps_on_answered_card` 是總數") {
		t.Error("resumeNote must not call the count a total — it counts only the " +
			"bounded task rows the snapshot carries")
	}
	for _, tc := range []struct{ name, text, phrase string }{
		{"resumeNote", resumeNote, "不是你所有任務的總數"},
		{"resumeNote/zero", resumeNote, "0 不等於沒有"},
		{"peekNote", peekNote, "not across all your tasks"},
		{"peekNote/zero", peekNote, "0 does not prove there is none"},
	} {
		if !strings.Contains(tc.text, tc.phrase) {
			t.Errorf("%s must state the bound (%q): a count read as a total makes a "+
				"0 mean 'nothing is stuck', which it does not", tc.name, tc.phrase)
		}
	}
}

// TestEveryFaceOfThePeekSumMatchesWhatTheServerActuallyAdds: the arithmetic
// behind estimated_total_chars is written out in prose on seven hand-written
// faces, and it has now been extended four separate times, each time missing
// one of them.
//
// WHAT THIS COMPARES, and why the obvious two designs both failed here.
//
// Asking each face whether it CONTAINS the new addend's name was walked past
// three ways, all green: name it in a neighbouring sentence while the sum still
// says four; write the sum in a wording the marker did not match; add a sixth
// addend to one face only.
//
// Making the faces agree WITH EACH OTHER was then walked past too: a decoy
// chain — "…all four reported in overview (the overview reports a + b + c + d +
// e)" — put the correct arithmetic beside the false claim on every face at
// once. The faces agreed, the package was green, and the lie "this total is
// four things" was still what an agent read in the tool list. Two faces
// agreeing is not evidence either is right.
//
// So the reference is neither a hard-coded list nor the other faces: it is
// PARSED OUT OF THE SERVER, from the expression that computes the field. Each
// face must state that sum, and every addition chain THAT NAMES AT LEAST ONE
// REAL ADDEND must state it — not merely the longest one, which is what the
// decoy exploited.
//
// KNOWN GAPS — measured green, do not read this guard as covering them:
//
//   - The NUMERAL is not checked. Every face also says "all six reported in
//     overview" (five until T-6bd2 added doc_capacity_chars); editing that word
//     to "five" while leaving the six names in place is exactly the original lie
//     in a shorter form, and this guard is blind to it. Nothing else in the
//     package reads that word either.
//   - A decoy chain naming NO real addend is dropped, not compared. Writing
//     "…all four reported in overview (chat_count + task_count + roster_count +
//     machine_count)" beside a correct chain stays green. The filter is what
//     stops an unrelated `a + b` elsewhere in the prose from being read as a
//     claim about this total, and it cannot tell that use apart from a decoy.
//   - Only the faces listed below are read. A NEW face — another spec field,
//     another constant — is not discovered; it has to be added here by hand,
//     which is the failure mode that produced this test in the first place.
func TestEveryFaceOfThePeekSumMatchesWhatTheServerActuallyAdds(t *testing.T) {
	want := addendsTheServerSums(t)
	if len(want) < 2 {
		t.Fatalf("parsed %v out of the estimated_total_chars expression — a sum of "+
			"fewer than two addends means the parse broke, and every comparison "+
			"below would be vacuous", want)
	}

	rawAPI, err := os.ReadFile("../../spec/openapi.json")
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	var api openapiSpec
	if err := json.Unmarshal(rawAPI, &api); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}
	op, ok := api.Paths["/api/resume-summary-size"]["get"]
	if !ok {
		t.Fatalf("openapi has no GET /api/resume-summary-size — this test has stopped discriminating")
	}
	dto, ok := api.Components.Schemas["ResumeSummarySizeDTO"]
	if !ok {
		t.Fatalf("openapi has no ResumeSummarySizeDTO schema — this test has stopped discriminating")
	}

	faces := map[string]string{
		"peekNote":                  peekNote,
		"openapi.summary":           op.Summary,
		"openapi.description":       op.Description,
		"openapi.x-mcp.description": op.XMCP.Description,
		// The frozen descriptor is prose an agent reads in the tool list.
		// bin/gen-mcp-catalog pins it equal to x-mcp.description, so it cannot
		// drift from that one — but both are hand-written here, and a single
		// edit that changes the pair together is exactly the edit that has gone
		// wrong four times.
		"openapi.x-mcp.legacy.descriptor": op.XMCP.Legacy.Descriptor,
		"openapi.ResumeSummarySizeDTO":    dto.Description,
	}
	for _, spec := range defaultRouteSpecs() {
		if spec.Path == "/api/resume-summary-size" && strings.EqualFold(spec.Method, "get") {
			faces["routes.go/Summary"] = spec.Summary
		}
	}
	if _, ok := faces["routes.go/Summary"]; !ok {
		t.Fatalf("GET /api/resume-summary-size is not on the routes table — this test has stopped discriminating")
	}
	// spec/mcp-catalog.json is deliberately NOT a face: bin/gen-mcp-catalog
	// refuses to render when it disagrees with x-mcp.description, and
	// make drift-mcp-catalog refuses a stale file — measured both ways.

	for _, name := range sortedFaceNames(faces) {
		text := faces[name]
		if text == "" {
			t.Errorf("%s carries no prose at all — a face this guard cannot read is a "+
				"face nothing is checking; if it was renamed, re-anchor the lookup", name)
			continue
		}
		chains := additionChains(text, want)
		if len(chains) == 0 {
			t.Errorf("%s states no addition chain over %v — either it stopped saying what "+
				"the total is made of, or it now says so in a shape this guard cannot "+
				"see. Point the guard at whatever replaced it rather than deleting the "+
				"claim.", name, want)
			continue
		}
		for i, got := range chains {
			if sameAddends(got, want) {
				continue
			}
			t.Errorf("%s states a sum the server does not compute (chain %d of %d):\n"+
				"  face:   %v\n  server: %v\n"+
				"Every chain on a face is checked, not just the longest — a correct sum "+
				"written BESIDE a false one leaves the false one in front of the reader.",
				name, i+1, len(chains), got, want)
		}
	}
}

// addendsTheServerSums reads the addends out of the expression that actually
// computes EstimatedTotalChars, so the guard above has a reference that cannot
// be satisfied by prose agreeing with prose. Parsing the server rather than
// listing the names here is what makes a SIXTH addend protected on the day it
// is written, under whatever name it is given.
func addendsTheServerSums(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("api_chat.go")
	if err != nil {
		t.Fatalf("read api_chat.go: %v", err)
	}
	// The trailing comma is load-bearing: it forces the match to cover the
	// WHOLE right-hand side. Without it a sum ending in a term this pattern
	// cannot read — `… + overview.MachinesChars + len(peekNote),` — matched its
	// overview-only prefix and returned a reference one addend short, so every
	// face kept claiming the old sum and the guard stayed green. Measured.
	m := regexp.MustCompile(`EstimatedTotalChars:\s*((?:overview\.\w+\s*\+\s*)*overview\.\w+),`).
		FindSubmatch(src)
	if m == nil {
		t.Fatal("could not find the EstimatedTotalChars assignment in api_chat.go as a " +
			"plain sum of overview fields — the field may have been renamed, the sum " +
			"moved behind a helper, or an addend added that is not an overview field; " +
			"re-anchor this parse rather than replacing it with a hard-coded list, " +
			"which is what this guard exists to avoid")
	}
	var out []string
	for _, term := range strings.Split(string(m[1]), "+") {
		out = append(out, snakeCase(strings.TrimPrefix(strings.TrimSpace(term), "overview.")))
	}
	return out
}

// snakeCase turns a Go field name into the wire name the prose uses. It is a
// re-derivation, not the json tag: a field named with an acronym (MCPChars ->
// m_c_p_chars) or carrying a tag that is not its lower-snake name produces a
// reference the prose can never match, which shows up as a confusing red rather
// than a silent pass. Read the tag out of wire.go if that day comes.
func snakeCase(field string) string {
	var b strings.Builder
	for i, r := range field {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// additionChains returns EVERY run of `+`-joined wire names in text, keeping
// the runs that mention at least one addend the server actually adds.
//
// The token shape is a plain snake_case identifier rather than the server's own
// list, and that asymmetry is deliberate. Building the pattern out of the known
// names looks tighter and is strictly weaker in one direction: when an addend
// is REMOVED from the server, its name leaves the pattern, a chain still
// naming it gets cut short at that word, and the truncated remainder matches
// the shortened sum exactly — so prose left behind by a removal reads as
// correct. Measured: with the fifth addend deleted from the server and all
// seven faces still claiming it, that form of this function stayed green.
//
// The "at least one known addend" filter is what keeps an ordinary `a + b` in
// some unrelated sentence from being read as a claim about this total. It is
// also this function's hole: a chain naming none of them is invisible, so a
// decoy sum written in names the server does not use passes untouched. It is
// not clear how to close that without also flagging every unrelated sum in the
// prose, so it is left open and named in the test's KNOWN GAPS instead.
//
// Backticks are tolerated because the OpenAPI prose wraps names in them. A lone
// mention is deliberately NOT a chain: naming an addend in a paragraph is not
// the same as putting it in the sum, and treating the two alike is how an
// earlier form of this guard was walked past.
func additionChains(text string, known []string) [][]string {
	isKnown := map[string]bool{}
	for _, name := range known {
		isKnown[name] = true
	}
	const name = "`{0,2}([a-z][a-z0-9_]*)`{0,2}"
	re := regexp.MustCompile(name + `(?:\s*\+\s*` + name + `)+`)
	var out [][]string
	for _, match := range re.FindAllString(text, -1) {
		var chain []string
		mentionsAnAddend := false
		for _, term := range strings.Split(match, "+") {
			word := strings.Trim(strings.TrimSpace(term), "`")
			chain = append(chain, word)
			if isKnown[word] {
				mentionsAnAddend = true
			}
		}
		if mentionsAnAddend {
			out = append(out, chain)
		}
	}
	return out
}

func sameAddends(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, x := range a {
		seen[x]++
	}
	for _, x := range b {
		seen[x]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

func sortedFaceNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
