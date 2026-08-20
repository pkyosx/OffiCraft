package main

// doc_capacity.go — the NEAR-CAP signal for this station's long-lived,
// write-back documents (T-6bd2).
//
// THE DEFECT IT ANSWERS: every one of these documents refuses a write with a
// 400 once it is full, and says NOTHING at all before that. The refusal lands
// at the exact instant an agent is trying to record something, which is the
// moment it has the least slack — and the cheapest way out of it is to delete
// text until the write fits. What gets deleted is the hand-off and the "what I
// did not verify" paragraph, i.e. the whole reason the field exists, and the
// response to that shortened write is a 200. Nothing anywhere remembers the
// sentence existed.
//
// So this file does NOT add a warning to the write path. It computes the same
// two numbers the write path already has (size_chars / cap_chars) at two
// moments that are BOTH before "I am recording something":
//
//   - the wake snapshot (resume_summary), when the agent has the most time; and
//   - the SOFT offboard notice (sse_bands.decideHandoverNotice), because
//     writing memory back is step 4 of the offboard sequence and by then there
//     is no time to compact anything.
//
// 🔴 TWO PROPERTIES ARE LOAD-BEARING AND MUST SURVIVE EDITS:
//
//  1. SILENT WHEN NOTHING IS NEAR. A block that appears on every wake is a
//     block every agent learns to scroll past, and then the one wake that
//     mattered looks like all the others. Nothing near the cap → no rows → the
//     field is absent from the payload entirely.
//
//  2. IT SAYS SOMETHING DIFFERENT TO THE DIFFERENT CLASSES OF DOCUMENT, AND
//     WHAT IT SAYS ABOUT PERMISSION MUST BE TRUE. Telling every reader to
//     "compact it now" would be telling some of them to go and do something
//     that can only be refused. But the opposite error is worse, and this file
//     shipped it: a row that tells a reader "you cannot write this one (it
//     answers 403 to you)" about a document it CAN write is a claim the reader
//     can falsify in one call — and a reminder caught lying is a reminder
//     nobody reads again.
//
//     🔴 MEASURED 2026-08-20 (zero-damage probes: patch_* with an anchor that
//     cannot exist, so the permission gate answers before anything is written):
//
//       patch_insight  role_key=<OWN role>     → 400 validation_error  ⇒ WRITABLE
//       patch_lessons  role_key=<OWN role>     → 400 validation_error  ⇒ WRITABLE
//       patch_insight  role_key=<ANOTHER role> → 403 "an agent may only
//                                                 write its own role's insight"
//       update_role    role=<any>              → 403 "principal not permitted"
//
//     ⇒ An agent CAN write its OWN role's insight and lessons. It cannot write
//     a role definition (a different gate, and not role-scoped), any other
//     role's documents, or the boot documents. The rows split three ways, not
//     two — and the third one exists because "technically writable" and
//     "yours to do under close-out pressure" are NOT the same question.

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"unicode/utf8"
)

// docCapacityCompactor is who to go to for a document the reading agent cannot
// write. The owner ruled the compaction work itself belongs to 銀月 (T-6bd2's
// 界線: 不動整併本身該由誰做), so the signal names her rather than telling the
// reader to do it — see property 2 above.
const docCapacityCompactor = "銀月"

// The row also carries the seeded ADDRESS (seedMiraID) beside the name: a
// display name is editable, and it is deliberately not resolved at read time
// (that would put a member lookup on the boot path to render a sentence), so
// what the reader gets is the id it can actually address a message to.
//
// The two action sentences. They are constants rather than inline strings
// because the ONE thing this feature has to get right is which of them a given
// row carries, and a test that pins the mapping has to be able to name them
// without restating them (a test that restates the wording tests nothing but
// its own copy).
const (
	docCapacityActionSelf = "you can rewrite this one yourself: do it NOW, " +
		"while there is still room, and rewrite it to its CURRENT state instead of appending"
	// Long-term memory the reader CAN write, but should not compact while it is
	// being collected. The permission claim is dropped because it was false
	// (see the header): what is true here is not "you may not", it is "not now,
	// and not alone". Compacting an insight is a judgement about which lessons
	// still earn their space — the exact judgement that goes worst under time
	// pressure, which is the defect this whole file answers.
	docCapacityActionSelfMemory = "you can write this one yourself, but " +
		"compacting long-term memory is not a close-out job: schedule it, or ask " +
		docCapacityCompactor + " (" + seedMiraID + ") to do it"
	// Documents THIS reader genuinely cannot write. This sentence DOES make a
	// permission claim, which is why it is no longer attached by document TYPE:
	// role definitions and the boot documents are gated at principalAdminAgent,
	// so the claim is true for an ordinary member and FALSE for the admin
	// assistant — who would also be told to go find herself. docCapacityFor
	// picks between this and docCapacityActionSelf per reader.
	docCapacityActionAsk = "去找" + docCapacityCompactor + " (" + seedMiraID + ")" +
		": this one is not yours to write, so compacting it is hers to do"
)

// docCapacityNear is the whole threshold rule, in one place.
//
// WHY A BAND TABLE AND NOT ONE PERCENTAGE: the caps on this station span
// 1,000 to 60,000, and a single percentage is wrong at both ends. 20% of the
// 1,000-char duty is 200 characters — under one rewrite's worth, so a flat 20%
// would fire on a duty that is in perfectly good shape. 20% of the 60,000-char
// system-interaction handbook is 12,000 characters — dozens of writes of slack,
// so the same rule would nag about a document nobody is close to filling.
//
// The number each band is set to is an answer to "how many more write-backs
// does the reader still have?", measured on this repo's own write-backs
// (200–600 chars each):
//
//	cap <= 1000    → fire under 25% left (250 chars ≈ one rewrite of the whole
//	                 document, which is what compacting a duty actually is).
//	cap <= 15000   → fire under 20% left (3,000 chars ≈ 5–15 more write-backs:
//	                 enough slack to schedule a compaction rather than perform
//	                 one under pressure) — OR under 1,000 chars left outright.
//	                 That absolute floor is not redundant: it is what makes the
//	                 band right for the 4,000-char step note, where 20% is 800
//	                 characters, i.e. barely one note.
//	cap > 15000    → fire under 10% left (6,000 chars on the handbook).
//
// STRICTLY LESS THAN, on purpose: a document sitting exactly on its threshold
// still has the full budgeted slack, and firing there would make the boundary
// value mean "you are late" when it means "you are on time".
func docCapacityNear(sizeChars, capChars int) bool {
	if capChars <= 0 {
		return false
	}
	remaining := capChars - sizeChars
	if remaining < 0 {
		remaining = 0
	}
	switch {
	case capChars <= 1000:
		return remaining*100 < capChars*25
	case capChars <= 15000:
		return remaining*100 < capChars*20 || remaining < 1000
	default:
		return remaining*100 < capChars*10
	}
}

// docCapacityRow is ONE near-full document. It carries the two numbers the
// reader would otherwise have to go and fetch per document, the arithmetic it
// would otherwise have to do, and — the part that makes it actionable — what
// this particular reader is able to do about it.
type docCapacityRow struct {
	// Doc names the document the way its OWN write face names it, so a reader
	// can go straight to the tool that writes it rather than guessing.
	Doc string `json:"doc"`
	// Writable: true when the READING agent may write this document itself.
	// It is a FACT about permission and nothing else — Action is decided
	// separately (see newDocCapacityRow) — and it is carried as its own field
	// so a client can group by it without parsing prose.
	Writable  bool   `json:"writable"`
	SizeChars int    `json:"size_chars"`
	CapChars  int    `json:"cap_chars"`
	Remaining int    `json:"remaining_chars"`
	Action    string `json:"action"`
}

// newDocCapacityRow builds a row, or nil when the document is not near its cap.
// Every collector below funnels through here, which is what stops one cell from
// acquiring its own private idea of "near".
//
// 🔴 `writable` IS A FACT AND `action` IS A DECISION, AND THEY ARE PASSED
// SEPARATELY ON PURPOSE. They used to be one: action was derived from writable,
// so the only way to route a row to 銀月 was to declare it unwritable — and
// that is exactly how this file came to tell agents "it answers 403 to you"
// about their own insight, which they can write. Keeping them apart means a row
// can say "yours to write, but not now" without lying about permission.
func docCapLog(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[doc-capacity] "+format+"\n", args...)
}

func newDocCapacityRow(doc string, sizeChars, capChars int, writable bool, action string) *docCapacityRow {
	if !docCapacityNear(sizeChars, capChars) {
		return nil
	}
	remaining := capChars - sizeChars
	if remaining < 0 {
		remaining = 0
	}
	// The ONE part of "does this sentence lie?" a machine can decide. The ask
	// sentence makes a permission CLAIM, and `writable` states the same fact as
	// a boolean two keys away — so a row carrying both is self-contradicting on
	// its face, whatever the prose says. Everything else about these sentences
	// needs a human (the literals in doc_capacity_t6bd2_test.go force one to
	// read them), but this pairing does not: it survives someone rewriting the
	// sentence AND updating the test literal in the same commit, which is the
	// one move the literal test cannot catch.
	//
	// 🔴 IT OMITS THE ROW, IT DOES NOT PANIC, AND THAT IS NOT TIMIDITY. An
	// independent review measured a panic here on the real path: mispair one row,
	// and /api/resume-summary
	// answers a bare EOF — no status, no body — DETERMINISTICALLY, so the agent
	// reading it can never boot; the same function runs inside the SSE handler,
	// so that agent could never be told to close out either. Dropping the row
	// instead loses one reminder and keeps both payloads, which is the trade
	// docCapacityFor's own header already made (see below). In development the
	// signal is if anything louder: the same mispair fails
	// TestResumeSummaryDocCapacityFiresForEveryCarrier AND both halves of
	// ...SplitsWhatTheReaderCanActOn, rather than blowing the test process up.
	if action == docCapacityActionAsk && writable {
		// Dropped, but NOT silently. An omitted row is indistinguishable from a
		// document that is simply not near its cap — docCapacityLines answers ""
		// for an empty set, so this row's absence reads exactly like a healthy
		// station (an independent review measured that: mispair the only near-cap
		// document and the whole ⚠️ block disappears from the offboard notice).
		// The header's best-effort omission covers a FAILED READ, which is
		// temporary and self-healing; this is a PROGRAMMER ERROR, which is
		// permanent and never heals, so it must not share that exit unannounced.
		docCapLog("row %q pairs the ask sentence with writable=true — one of the "+
			"two is wrong; the row was dropped", doc)
		return nil
	}
	return &docCapacityRow{
		Doc:       doc,
		Writable:  writable,
		SizeChars: sizeChars,
		CapChars:  capChars,
		Remaining: remaining,
		Action:    action,
	}
}

// docCapacityFor collects every near-full document in the READER's reach.
//
// 🔴 IT RETURNS NO ERROR, AND THAT IS THE DESIGN. Its two callers are the wake
// snapshot and the offboard notice — the two payloads an agent cannot work
// without. A failed read of one document must never take either of them down:
// losing one reminder is a missed compaction, losing the wake snapshot is an
// agent that cannot boot. So every read here is best-effort and a fault simply
// omits that row. The rows that DO appear are still exact — nothing is
// estimated to paper over a failed read.
//
// stepNotes carries the caller's open steps' notes, already loaded by
// resumeTasksFor; passing them in is what keeps step notes free on this path
// rather than a second pass over the same rows.
func (s *apiServer) docCapacityFor(actor string, stepNotes []docCapacityRow) []docCapacityRow {
	rows := []docCapacityRow{}
	if actor == "" {
		return rows
	}
	add := func(row *docCapacityRow) {
		if row != nil {
			rows = append(rows, *row)
		}
	}

	// 🔴 WHO IS READING DECIDES WHAT `writable` SAYS, for the rows whose write
	// face is gated at principalAdminAgent. The admin assistant (ROLE_KEY
	// "assistant" — the admin_agent discriminator is role_key; classifyMember
	// checks Member.Kind first, but only to take wardens out, and Kind is
	// "assistant" on every ordinary 正職 too) may write role definitions and
	// the boot documents, so telling HER "this one is not yours to write, go
	// find 銀月"
	// is two falsehoods in one sentence: a permission claim that is wrong, and
	// an instruction to go find herself. `writable` is documented as a FACT
	// about THIS READER's permissions, so it has to be read off this reader.
	//
	// ⚠️ The insight / role-lessons / task-manual / step-note rows are NOT
	// affected: their gate is "your own", which an ordinary member already
	// passes, so admin capability changes nothing there.
	adminCapable := false
	if m, err := s.dal.GetMember(actor); err == nil && m != nil {
		adminCapable = principalAtLeast(classifyMember(m), principalAdminAgent)
	}
	adminGatedAction := docCapacityActionAsk
	if adminCapable {
		adminGatedAction = docCapacityActionSelf
	}

	// ── the reader's own role documents ──────────────────────────────────────
	// A contractor has no role and an unknown sub resolves to nothing; both mean
	// "no role documents", not an error.
	if m, err := s.dal.GetMember(actor); err == nil && m != nil && m.RoleKey != "" {
		if duty, err := s.foldRoleDefDTO(m.RoleKey); err == nil && duty != nil {
			// update_role answers 403 "principal not permitted" to an ordinary
			// member — for its OWN role too. An admin-capable reader passes the
			// same gate, hence the pair above rather than a constant false.
			add(newDocCapacityRow("role definition ("+m.RoleKey+")",
				duty.SizeChars, duty.CapChars, adminCapable, adminGatedAction))
		}
		if ins, err := s.foldInsightDTO(m.RoleKey); err == nil && ins != nil {
			// 🔴 The reader's OWN insight — patch_insight lets it through
			// (measured: 400, not 403). Writable, and the sentence says so.
			add(newDocCapacityRow("insight ("+m.RoleKey+")",
				ins.SizeChars, ins.CapChars, true, docCapacityActionSelfMemory))
		}
		if les, err := s.foldLessonsDTO(m.RoleKey, seedLessonsTaskType); err == nil && les != nil {
			// Same gate as insight, same measurement (400, not 403).
			add(newDocCapacityRow("role lessons ("+m.RoleKey+"/"+seedLessonsTaskType+")",
				les.SizeChars, les.CapChars, true, docCapacityActionSelfMemory))
		}
	}

	// ── the three boot documents ─────────────────────────────────────────────
	// Station-wide rather than per-reader, and gated at principalAdminAgent to
	// write (NOT owner-only — that is why the pair above is read per reader).
	// They are in
	// scope because they fail in the identical way (recon measured the refusal:
	// same docCapRefusal sentence, same moment) — the only difference is that
	// the person who hits it is usually the owner, which is precisely why no
	// agent has ever been in a position to notice.
	for _, spec := range s.docCapacityBootSpecs(actor) {
		if dto, err := s.foldBootDocDTO(spec); err == nil && dto != nil {
			add(newDocCapacityRow(spec.DocName, dto.SizeChars, dto.CapChars,
				adminCapable, adminGatedAction))
		}
	}

	// ── the task manuals of the reader's OPEN tasks ──────────────────────────
	// Scoped to the manuals it is actually working under: listing every manual
	// on the station would make most rows something the reader has no occasion
	// to write, which is the noise property 1 forbids.
	sopCap, learningsCap := s.manualSopCap(), s.manualLearningsCap()
	for _, typeKey := range s.docCapacityOpenTaskTypes(actor) {
		manual, err := s.dal.GetTaskManual(typeKey)
		if err != nil || manual == nil {
			continue
		}
		add(newDocCapacityRow("task manual SOP ("+typeKey+")",
			utf8.RuneCountInString(manual.SopMD), sopCap, true,
			docCapacityActionSelf))
		add(newDocCapacityRow("task manual learnings ("+typeKey+")",
			utf8.RuneCountInString(manual.Learnings), learningsCap, true,
			docCapacityActionSelf))
	}

	// ── the reader's open steps' notes ───────────────────────────────────────
	// The most frequently hit cell of the nine, and the only one whose numbers
	// the reader could not compute even if it wanted to (recon: the wholesale
	// receipt and the get_task step view both omitted them until this ticket).
	rows = append(rows, stepNotes...)
	return rows
}

// docCapacityBootSpecs resolves the three boot documents to check for this
// reader: the shared handbook, the shared offboard sequence, and the boot
// sequence for the reader's OWN runtime — the two runtimes' sequences share one
// cap but are separate documents, and quoting the other one's size would be a
// number about a document this reader never reads.
func (s *apiServer) docCapacityBootSpecs(actor string) []bootDocSpec {
	specs := []bootDocSpec{s.systemInteractionSpec(), s.offboardSpec()}
	runtime := ""
	if m, err := s.dal.GetMember(actor); err == nil && m != nil {
		runtime = m.Runtime
	} else if w, err := s.dal.GetOutsourceWorker(actor); err == nil && w != nil {
		runtime = w.Runtime
	}
	if spec, ok := s.bootSequenceSpecFor(bootSequenceDocKey(runtime)); ok {
		specs = append(specs, spec)
	}
	return specs
}

// docCapacityOpenTaskTypes lists the DISTINCT type_keys of the reader's open
// tasks, in a stable order. Distinct because two open tasks of one type would
// otherwise put the same manual on the list twice, and a reader that sees the
// same document listed twice reasonably concludes the block is unreliable.
func (s *apiServer) docCapacityOpenTaskTypes(actor string) []string {
	tasks, err := s.dal.ListOpenTasksByExecutor(actor, resumeTasksN)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	keys := []string{}
	for _, t := range tasks {
		if t.TypeKey == "" || seen[t.TypeKey] {
			continue
		}
		seen[t.TypeKey] = true
		keys = append(keys, t.TypeKey)
	}
	sort.Strings(keys)
	return keys
}

// stepNoteCapacityRow is the step-note cell, built from a step row the caller
// already holds. Split out so both the wake snapshot (which folds every open
// step) and any future caller measure the note against the SAME ceiling the
// write face enforces — chatBodyMaxChars, read here rather than re-typed.
func stepNoteCapacityRow(taskNo, stepName, note string) *docCapacityRow {
	label := "step note (" + taskNo
	if stepName != "" {
		label += ": " + stepName
	}
	label += ")"
	return newDocCapacityRow(label, utf8.RuneCountInString(note), chatBodyMaxChars, true,
		docCapacityActionSelf)
}

// stepNoteCapacityFor is the step-note collector for callers that do NOT
// already hold the step rows — the offboard notice, which fires once per
// session and so can afford the reads the wake snapshot gets for free.
//
// Same fail-soft posture as docCapacityFor: a read fault yields no rows rather
// than an error, because the notice this feeds must go out either way.
func (s *apiServer) stepNoteCapacityFor(actor string) []docCapacityRow {
	rows := []docCapacityRow{}
	if actor == "" {
		return rows
	}
	tasks, err := s.dal.ListOpenTasksByExecutor(actor, resumeTasksN)
	if err != nil {
		return rows
	}
	for _, t := range tasks {
		steps, err := s.dal.ListTaskSteps(t.ID)
		if err != nil {
			continue
		}
		for _, st := range steps {
			if StepIsTerminal(st.Status) {
				continue
			}
			if row := stepNoteCapacityRow(TaskNo(t.ID), st.Name, st.Note); row != nil {
				rows = append(rows, *row)
			}
		}
	}
	return rows
}

// docCapacityLines renders the rows for a notice that carries TEXT rather than
// JSON (the offboard band). "" when there is nothing near — the caller appends
// it unconditionally and gets silence for free, so the once-per-session notice
// cannot be made noisier by this feature on a station whose documents are fine.
//
// 🔴 It is appended AFTER the approved notice sentence and the offboard
// document, never woven into either: the sentence is the owner's wording
// (T-a9d6, card rc-ec5859a4c384) and the steps are the DOCUMENT's, carried
// verbatim. This block is a third thing standing beside them.
func docCapacityLines(rows []docCapacityRow) string {
	if len(rows) == 0 {
		return ""
	}
	out := "\n\n⚠️ Long-lived documents close to their cap — compact BEFORE you " +
		"write memory back (that is step 4 of the sequence above, and by then it is too late):"
	for _, row := range rows {
		out += "\n- " + row.Doc + ": " + strconv.Itoa(row.SizeChars) + "/" +
			strconv.Itoa(row.CapChars) + " chars, " + strconv.Itoa(row.Remaining) +
			" left → " + row.Action
	}
	return out
}
