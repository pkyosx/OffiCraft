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
// contract is "the first two blocks of the SHARED SEED" — bytes that are
// identical for every reader. This directory is PER-ACTOR (private entries are
// filtered by the reader). Putting it there would make a per-person document
// masquerade as the shared core. TestDirectoryIsNotInTheSharedHead is the guard
// that catches the move.
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
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// loreSectionH1 is the section's heading — the one string in this file that a
// member actually READS on every boot.
//
// 🔴 NOTHING GUARDS ITS VALUE, and this comment used to claim the opposite. It
// said the tests pin the literal rather than importing it. They do not: all
// seven references (lore_fold_t33_test.go and worker_spawn_test.go) import this
// constant, so every want moves in lockstep with it — the very failure the old
// comment named as the thing to avoid. Measured, not read: mutating this
// literal to "# ZZZ" and running the whole suite leaves it GREEN (1909 pass, 0
// fail). Rewriting the heading in any language, or back to a retired name,
// changes what every member sees and no test says a word.
//
// A guard belongs here (T-33 named debt). Until it exists, do not cite any test
// as covering this line.
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
// role-only fold with no member behind it. It is used ONLY to decide which
// `private` entries this reader may be told about; nothing else in the output
// varies by reader, so two agents on one station see the same directory apart
// from what is genuinely walled off from one of them.
func (s *apiServer) foldLoreSection(actorID string) (string, error) {
	rows, err := s.dal.ListLoreSubjectRoster(actorID)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	kept, omitted := loreSubjectsWithinCaps(rows)
	if len(kept) == 0 {
		return "", nil
	}
	return renderLoreSubjectIndex(kept, omitted), nil
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
