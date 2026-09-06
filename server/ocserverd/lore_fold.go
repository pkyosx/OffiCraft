package main

// lore_fold.go — T-33, second round: the ONE place a lore block is
// assembled for a boot context.
//
// 🔴 SCOPE, AND IT IS DELIBERATELY NARROW: THIS ROUND EMITS A DIRECTORY, NOT
// CONTENT. Every line below names a subject and says how many entries hang off
// it. No entry's `short`, `symptoms`, `falsify`, `instance` or `residual_risk`
// text reaches a boot context through this file, and none is meant to yet — how
// much entry BODY a wake can afford is a size question nobody has measured, and
// answering it by accident (one convenient string concatenation) is exactly how
// a boot document doubles without a decision. An agent that wants an entry goes
// and reads it.
//
// 🔴 ONE FUNCTION, TWO CALLERS, ON PURPOSE. foldLoreSection is
// called from buildBootContext (staff, assets.go) and from
// buildWorkerBootContext (outsource, worker_spawn.go), at the SAME relative
// position in both: the tail of slot 3, after 長期筆記 and before 啟動步驟. Two
// assemblers would be two documents that drift, and the drift would be silent —
// nobody reads both boot contexts side by side.
//
// 🔴 IT IS NOT CALLED FROM workerSharedHead, AND MUST NOT BE. That function's
// contract is "the first two blocks of the SHARED SEED" — a seed that is
// assembled once and cached, with no live table read in it. This directory is a
// live query over the ontology, so folding it in there would freeze a table that
// changes under the station into the cached head. It takes a reader id because
// it is assembled per boot, not because the seed could carry it.
// TestDirectoryIsNotInTheSharedHead is the guard that catches the move.
//
// ⚠️ THE ORIGINAL REASON WAS DIFFERENT: this used to be a PER-ACTOR document
// because `private` entries were filtered by reader. That wall is gone
// (rc-26c1fd0c6b3c: 全部共享), so the bytes are now the same for every reader —
// the placement rule survives on the staleness argument above, which is the one
// stated here so nobody re-derives it from a filter that no longer exists.
//
// ⚠️ MEASURED, AND NOT WHAT YOU WOULD ASSUME:
// TestWorkerSharedHeadMatchesUnfilteredSeedAssembly does NOT catch it. That test
// runs on a server with an EMPTY ontology, so this function folds to "" there
// and moving the call changes nothing it can see. The guard therefore had to be
// a test that seeds a directory first — which is why one exists rather than the
// existing equality being relied on.
//
// TODO(T-33, 待裁定): 規範預載 — the high-risk-action list that the detail
// design raises in §10.1 item 3 and marks 「我提出，待裁定」 — is NOT implemented
// here and must not be added to this file until the owner rules on it. It is a
// different thing from this directory: it would put normative CONTENT into every
// boot context, which is the size/authority decision this round explicitly does
// not make.

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"unicode/utf8"
)

// loreSectionH1 is the section's heading — the one string in this file that a
// member actually READS on every boot.
//
// 🔴 THE OTHER REFERENCES DO NOT GUARD IT, AND IT WOULD BE EASY TO THINK THEY
// DO. All seven of them (lore_fold_t33_test.go and worker_spawn_test.go) import
// this constant, so every want moves in lockstep with it. Measured, not read:
// mutating this literal to "# ZZZ" and running the whole suite used to leave it
// GREEN (1909 pass, 0 fail) — a rename in any language, or back to a retired
// name, changed what every member reads on boot and no test said a word.
//
// 🔴 THE GUARD IS TestLoreHeadingEveryMemberReadsOnBoot
// (lore_heading_guard_t33_test.go), and it is the ONLY test that covers this
// line. It spells the heading out by hand instead of importing this constant —
// that is the whole point — and it reads it back out of the documents
// buildBootContext and buildWorkerBootContext actually assemble, so dropping
// the fold from either boot path is as red as rewriting the string. Both
// mutants above now fail it; nothing else in the suite notices either.
//
// ⚠️ RED THERE IS NOT AN EXPECTATION THAT DRIFTED. It says the first line of a
// section in every member's boot document is about to change, so the fix is
// never to sync the want: it is to have the rename ruled on, then move this
// constant and that test's loreHeadingAsShipped together.
const loreSectionH1 = "# 傳承：對象目錄（Lore — Subject Index）"

// loreSubjectIndexMaxSubjects caps how many subject LINES the
// directory prints.
//
// ⚠️ 40 是佔位值，不是算出來的，trial 之後要校. It is a placeholder: nobody has
// yet watched a real station's ontology grow, so there is no measurement behind
// this number. It exists so the block has a ceiling at all — an uncapped
// directory grows with the ontology and silently eats the boot budget — not
// because 40 is the right ceiling.
const loreSubjectIndexMaxSubjects = 40

// loreSubjectIndexMaxChars caps the assembled block in runes (the
// repo's length unit; it counts a Chinese name and an English one the same way).
//
// ⚠️ 3000 是佔位值，不是算出來的，trial 之後要校. Same story as the count cap
// above: a placeholder ceiling, to be calibrated once there is a real corpus to
// measure against.
//
// The two caps are BOTH enforced because they fail differently: a station with
// many short subject names hits the count first, one with a few very long
// display names hits the character budget first, and a directory that only
// guarded one of them would blow the other without saying so.
const loreSubjectIndexMaxChars = 3000

// foldLoreSection assembles the lore block for ONE
// reader, or returns "" when there is nothing to say.
//
// 🔴 EMPTY MEANS THE SECTION DOES NOT EXIST — not an empty heading. This is the
// same rule 使用者自訂 follows (buildBootContext skips the whole block when the
// owner text is blank): an orphan title with nothing under it teaches a reader
// that this section is usually empty, which is the one thing it must never say
// once it has content.
//
// actorID is the reader — a member id, an outsource worker id, or "" for the
// role-only fold with no member behind it.
//
// ⚠️ IT CURRENTLY CHANGES NOTHING. Its only job was choosing which `private`
// entries the reader could be told about, and that wall is gone
// (rc-26c1fd0c6b3c: 全部共享), so every reader now gets identical bytes. The
// parameter is kept rather than removed because both boot callers already thread
// a reader id through, and a per-actor axis is what this directory is expected to
// grow again — dropping it would churn both call sites twice.
func (s *apiServer) foldLoreSection(actorID string) (string, error) {
	text, _, err := s.foldLoreSectionWithSurfacing(actorID)
	return text, err
}

// loreSurfacing is the receipt for ONE assembly of the directory: who it was
// assembled for, which subjects actually got printed, and how many were left
// out by the caps.
//
// 🔴 OMITTED IS THE WHOLE REASON THIS TYPE EXISTS. loreSubjectsWithinCaps has
// always computed it, and until now it was spent on one notice line and then
// dropped. That number is the only place the station can ever learn that its
// directory does not fit — a truncation nobody counted is a truncation nobody
// can decide about, and the caps above are admitted placeholders waiting for
// exactly this measurement.
type loreSurfacing struct {
	ActorID string
	// Subjects are the canonical names of the kept rows, in the order the
	// roster produced them. It is the canonical and not the entity id because
	// canonical is the string the reader saw on the line.
	Subjects []string
	Omitted  int
}

// surfaced answers whether this assembly put anything in front of anybody. An
// empty directory folds to "" and never reaches a boot document, so there is
// nothing to record about it.
func (sur loreSurfacing) surfaced() bool { return len(sur.Subjects) > 0 }

// foldLoreSectionWithSurfacing is the assembly, and the ONLY one. It returns
// the block plus the receipt for it; foldLoreSection is the thin wrapper for
// the callers that only want the text (the cockpit previews).
//
// 🔴 IT DOES NOT WRITE THE JOURNAL ROW, AND THAT IS THE POINT. Four upstream
// paths reach this fold through the two boot assemblers, and two of them are
// PREVIEWS — the worker boot-context endpoint (api_outsource.go) and
// /api/bootstrap without a member_id (api_auth.go, whose own comment says so in
// as many words). Recording here would file "this was put in front of
// a member" for a document the cockpit rendered for the owner and nobody ever
// booted with. A journal that logs things that did not happen cannot be used to
// decide anything, which makes it decoration. The three paths that really hand
// the document to somebody call recordLoreSurfacing themselves, after the
// handover is certain.
func (s *apiServer) foldLoreSectionWithSurfacing(actorID string) (string, loreSurfacing, error) {
	sur := loreSurfacing{ActorID: actorID}
	// 🔴 THE FEATURE SWITCH IS CHECKED HERE, IN THE ONE SHARED ASSEMBLER, AND
	// NOWHERE ELSE ON THE BOOT PATHS (T-33). This function already exists
	// because 正職 (assets.go buildBootContext) and 外包
	// (worker_spawn.go buildWorkerBootContextWithSurfacing) must not each own a
	// copy of how this block is built — see the ONE FUNCTION, TWO CALLERS block
	// at the top of this file. A switch tested in each caller instead would put
	// the divergence back into the exact place it was just taken out of, and it
	// would be a SILENT divergence: nobody reads the two boot contexts side by
	// side, and 「off, so the section is absent」 looks identical to 「the fold
	// broke, so the section is absent」.
	//
	// Returning the SAME zero values as an empty ontology is deliberate and not
	// laziness: the callers' contract is already 「"" means this section does not
	// exist」 (an orphan heading is forbidden — see foldLoreSection's comment),
	// so an OFF station produces a boot document with no lore section at all
	// rather than a heading explaining an absence. And the empty loreSurfacing
	// means surfaced() is false, so recordLoreSurfacing files no journal row —
	// a directory nobody was shown must not be recorded as shown.
	//
	// ⚠️ THIS IS THE ONE PLACE THE SWITCH IS NOT LIVE, AND IT CANNOT BE. A boot
	// context is assembled ONCE at wake and handed over; an agent already
	// running keeps the document it was given until it boots again. Flipping the
	// switch on therefore reaches the routes immediately and this section only
	// at the next boot. That side effect was stated to the owner rather than
	// engineered around: re-reading the table per turn would mean the document
	// an agent was told to trust changes under it mid-session.
	if !s.loreEnabledSnapshot() {
		return "", sur, nil
	}
	rows, err := s.dal.ListLoreSubjectRoster(actorID)
	if err != nil {
		return "", sur, err
	}
	if len(rows) == 0 {
		return "", sur, nil
	}
	kept, omitted := loreSubjectsWithinCaps(rows)
	if len(kept) == 0 {
		return "", sur, nil
	}
	sur.Omitted = omitted
	for _, r := range kept {
		sur.Subjects = append(sur.Subjects, r.Canonical)
	}
	return renderLoreSubjectIndex(kept, omitted), sur, nil
}

// loreRecallQueryBoot is the `query` cell every boot-assembled directory is
// filed under. It is a MARKER, not a search string: there is no query behind a
// boot fold. Its only job is to keep this path separable from the retrieval
// path that will write into the same table later — two very different events
// (a whole directory nobody asked for, versus one deliberate lookup) that are
// indistinguishable once they are mixed without a label.
const loreRecallQueryBoot = "boot-fold"

// recordLoreSurfacing files the journal row for ONE directory that really was
// handed to somebody.
//
// 🔴 IT DOES NOT TOUCH lore_meta.surfaced_count OR recall_count, AND MUST NOT.
// What went out is the SUBJECT DIRECTORY: names and counts, not one line of any
// entry's body (see the scope block at the top of this file). Bumping a
// per-entry counter for every entry hanging off a listed subject would assert
// that each of them was put in front of the reader, which is false — and the
// consequence is the failure the design warns about by name: every entry looks
// used, so no entry can ever be argued down. Those counters belong to a path
// that actually shows an entry.
//
// 🔴 FAIL-OPEN. A failed journal write is logged and swallowed: booting a
// member matters and recording that we did is bookkeeping. The inverse — a
// station that cannot start an agent because a log table is unhappy — trades
// the thing for the record of the thing.
func (s *apiServer) recordLoreSurfacing(sur loreSurfacing) {
	if !sur.surfaced() {
		return
	}
	returned, err := json.Marshal(struct {
		Subjects []string `json:"subjects"`
		Omitted  int      `json:"omitted"`
	}{Subjects: sur.Subjects, Omitted: sur.Omitted})
	if err != nil {
		log.Printf("[lore] recall journal: encoding the surfaced set for %q failed: %v",
			sur.ActorID, err)
		return
	}
	// subject_id stays EMPTY and hop stays 0 on purpose. This row is one
	// directory covering many subjects, so there is no single subject to name,
	// and nothing was walked to reach it — a hop count would be inventing a
	// traversal that did not happen.
	//
	// 🔴 IT GOES THROUGH recordLoreRecall LIKE EVERY OTHER PATH, and the anchor
	// mode is the load-bearing argument. A boot fold is dispatched to a session
	// that has NOT connected yet, so it has no anchor of its own — and in
	// reconcileOne this call sits one line BEFORE clearSessionBootTS, so asking
	// the roster here would file the OUTGOING session's anchor as this row's.
	// That number would look right and belong to a session that had ended.
	s.recordLoreRecall(LoreRecall{
		ActorID:  sur.ActorID,
		Query:    loreRecallQueryBoot,
		Returned: string(returned),
	}, loreAnchorSessionNotStartedYet)
}

// loreSubjectsWithinCaps applies both ceilings and reports how many
// subjects were left out.
//
// 🔴 A SUBJECT CARRYING A `human:`-ORIGIN ENTRY IS NEVER TRUNCATED AWAY. THIS IS
// A HARD RULE, NOT A WEIGHT: those rows are taken FIRST and unconditionally, and
// only then is the remaining budget spent on the rest. A weighting would merely
// make them likely to survive — likely is not the promise. Something the owner
// said in person is the one class of knowledge whose disappearance nobody can
// reconstruct afterwards, so it does not compete for room.
//
// The consequence is stated rather than hidden: if human-origin subjects alone
// exceed BOTH caps, the block goes over budget. That is the deliberate choice —
// blowing a size ceiling is visible in the very next boot document, whereas
// dropping the owner's own knowledge is not visible at all.
func loreSubjectsWithinCaps(rows []LoreSubjectRosterRow) ([]LoreSubjectRosterRow, int) {
	var human, rest []LoreSubjectRosterRow
	for _, r := range rows {
		if r.HumanOrigin {
			human = append(human, r)
		} else {
			rest = append(rest, r)
		}
	}
	kept := human
	used := 0
	for _, r := range human {
		used += utf8.RuneCountInString(loreSubjectLine(r))
	}
	omitted := 0
	for _, r := range rest {
		cost := utf8.RuneCountInString(loreSubjectLine(r))
		if len(kept) >= loreSubjectIndexMaxSubjects ||
			used+cost > loreSubjectIndexMaxChars {
			omitted++
			continue
		}
		kept = append(kept, r)
		used += cost
	}
	return kept, omitted
}

// loreSubjectLine renders ONE subject: its identifier, its display
// name when it has one, and how many entries are filed against it.
//
// The identifier is `canonical` — the globally unique primary name, the string a
// caller can actually look the subject up by. `display` is decoration and is
// omitted when blank or when it merely repeats the identifier, because a line
// that says the same word twice trains the reader to skip the column.
func loreSubjectLine(r LoreSubjectRosterRow) string {
	name := r.Canonical
	if d := strings.TrimSpace(r.Display); d != "" && d != r.Canonical {
		name += "（" + d + "）"
	}
	return fmt.Sprintf("- %s — %d 條", name, r.Entries)
}

// renderLoreSubjectIndex groups the kept rows by entity type and
// prints the truncation notice when there is one.
//
// 🔴 THE TRUNCATION NOTICE IS A LINE OF THE SECTION, DIRECTLY UNDER THE HEADING
// — NOT A FOOTNOTE. It has to be read by someone who reads the first two lines
// and skims the rest, because "this list is incomplete" changes how every line
// below it should be trusted. At the bottom of a forty-line list it would be
// arrived at only by the readers who least needed it.
func renderLoreSubjectIndex(kept []LoreSubjectRosterRow, omitted int) string {
	var b strings.Builder
	b.WriteString(loreSectionH1)
	b.WriteString("\n\n")
	b.WriteString("這是目錄，不是內容：每一行只說「有這個對象、底下有幾條」，" +
		"沒有任何一條的正文。要看正文請自己去讀那一條。\n")
	if omitted > 0 {
		b.WriteString("\n" + loreTruncationLine(omitted) + "\n")
	}

	byType := map[string][]LoreSubjectRosterRow{}
	var types []string
	for _, r := range kept {
		if _, seen := byType[r.Type]; !seen {
			types = append(types, r.Type)
		}
		byType[r.Type] = append(byType[r.Type], r)
	}
	// Sorted so the document is a function of the DATA, not of the order the
	// rows happened to arrive in: two boots over an unchanged table must produce
	// the same bytes, or every diff of a boot context is noise.
	sort.Strings(types)
	for _, typ := range types {
		b.WriteString("\n## " + typ + "\n")
		lines := byType[typ]
		sort.Slice(lines, func(i, j int) bool {
			if lines[i].Canonical != lines[j].Canonical {
				return lines[i].Canonical < lines[j].Canonical
			}
			return lines[i].EntityID < lines[j].EntityID
		})
		for _, r := range lines {
			b.WriteString(loreSubjectLine(r) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// loreTruncationLine is the one sentence that says the directory is
// incomplete. It is its own function so the tests can assert the EXACT wording
// reaches both boot documents — a notice that a refactor quietly drops is
// indistinguishable, to a reader, from a directory that was complete.
func loreTruncationLine(omitted int) string {
	return fmt.Sprintf("🔴 這份目錄被截斷了：還有 %d 個對象沒有列在下面。"+
		"下面這份名單是不完整的，不要把「沒列到」讀成「不存在」。", omitted)
}
