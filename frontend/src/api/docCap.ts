// api/docCap.ts — the frontend's ONE copy of the server's document size cap.
//
// ⚠️ THE AUTHORITY FOR THIS RULE IS THE SERVER, NOT THIS FILE.
// `DocCapBlocked` in server/ocserverd/domain.go decides whether a write is
// refused, against the live `doc.cap_chars.*` setting for that document
// (T-3aeb made it a setting, T-ae38 split it four ways — the cap is no
// longer a constant on either side, so this module takes it as a parameter); this module exists only so the cockpit can MARK a
// revision as un-restorable BEFORE the owner clicks, instead of letting them
// click and collect an HTTP 400 that reads like a broken system.
//
// It is a TEMPORARY STAND-IN. The right shape is the server returning a
// per-revision `restorable` + `reason` on DocumentHistoryDTO, at which point
// this whole module is deleted and the card reads the flag. That is a wire
// change (spec/openapi.json is frozen — see root CLAUDE.md §13) and is
// currently blocked on owner approval, so until then two implementations of one
// rule exist and are pinned against a SHARED FIXTURE, not against each other:
//   bin/tests/fixtures/doc-cap-cases.tsv   — the table (the shared truth)
//   src/api/docCap.test.ts                 — this side reads it
//   server/ocserverd/doc_cap_mirror_test.go — the other side reads it
// A drift on either side reddens that side's test and names the row.
//
// Guessing was not an option for WHICH FIELD each kind caps, so it is
// transcribed from restoreDocumentHistory (api_document_history.go), not from
// the shape of the DTO: global_context is NOT capped at all (its restore calls
// DocCapBlocked nowhere), lessons and insight cap `text`, role_definition caps
// `definition_md` (T-ae38 — it was uncapped on BOTH doors until then, so
// restoring an old long revision was the way around the edit door), and
// task_manual caps `learnings` AND `sop_md` — either one over the cap refuses
// the whole restore.
//
// T-ae38 also made the cap PER SEGMENT: which number applies is a property of
// the kind, transcribed here from the same switch. Judging a Duty revision
// against the Learning cap would mark a 4,000-char role definition as
// restorable when the server refuses it at 1,000. T-30f1 split the manual's one
// number into two, so the manual's two streams no longer share an answer here
// either.

import type { BootDocKind, DocumentKind } from "../types";

/** One live number per CAPPED SEGMENT — the set `capForKind` routes a kind to.
 * Total over every segment the server caps: the five role/manual ones
 * (server/ocserverd/domain.go: dutyCapCharsDefault + contextDocMaxCharsDefault)
 * and, since T-791e, the two boot-context blocks. Since T-3aeb the live values
 * are the `doc.cap_chars.*` settings, so callers pass them in; the constants
 * below are only the fallback for a caller with no server value yet, and the
 * shared fixture's anchor. Do not inline these numbers anywhere else. */
export interface DocCaps {
  duty: number;
  insight: number;
  learning: number;
  manualSop: number;
  manualLearnings: number;
  systemInteraction: number;
  bootSequence: number;
  offboard: number;
}

/**
 * The SHIPPED DEFAULT cap of each boot-context block, in the same rune unit
 * (T-791e). Sized off the seeds actually in the tree, measured rather than
 * guessed: the system-interaction seed is the largest block, while the two
 * boot sequences are much smaller — so 60000 leaves the system block real room
 * to grow while 15000 is roughly ten times what a boot SOP has ever needed.
 *
 * 🔴 A DEFAULT, not the cap: the number in force arrives on the document's own
 * read (`BootDocView.capChars`), and every enforcement point reads THAT. This
 * constant exists for the mock adapter (which has to answer with something) and
 * as the anchor these numbers are stated once. Do not inline them elsewhere.
 *
 * Keyed by `BootDocKind` rather than folded into `DOC_CAP_CHARS_DEFAULTS`
 * because the page and the mock address these by WIRE kind, not by the
 * view-model field name.
 */
export const BOOT_DOC_CAP_CHARS_DEFAULTS: Record<BootDocKind, number> = {
  system_interaction: 60000,
  boot_sequence: 15000,
  // T-c9c0. Sized with the boot sequences rather than the handbook: the
  // 下線程序 seed is a short ordered checklist an agent works under time
  // pressure (a recycle gives it a bounded window; an offboard gives it none
  // but the owner is waiting), not a reference text.
  offboard: 15000,
};

export const DOC_CAP_CHARS_DEFAULTS: DocCaps = {
  duty: 1000,
  insight: 15000,
  learning: 15000,
  manualSop: 15000,
  manualLearnings: 15000,
  // Not restated: the boot blocks' numbers are stated once, above.
  systemInteraction: BOOT_DOC_CAP_CHARS_DEFAULTS.system_interaction,
  bootSequence: BOOT_DOC_CAP_CHARS_DEFAULTS.boot_sequence,
  offboard: BOOT_DOC_CAP_CHARS_DEFAULTS.offboard,
};

/**
 * How many retained revisions a boot-context block keeps (T-791e). TEN, where
 * every other document keeps three — the owner's ruling, for the workflow this
 * surface is for: proposals land one section at a time, so an afternoon is a
 * dozen small saves and three slots would lose the version the afternoon
 * started from before it ended.
 *
 * 🔴 Counted in WRITES, not in time, and the cockpit has to SAY so — an owner
 * who reads it as "the last ten days" reads a normal editing session as the
 * cockpit throwing his work away. The page's own note is composed from this
 * constant (i18n/compose.ts `bootDocNoteHistory`) rather than restating the
 * number, so the sentence cannot end up describing a retention nobody applies.
 */
export const BOOT_DOC_HISTORY_KEPT = 10;

/** The single number the shared fixture (bin/tests/fixtures/doc-cap-cases.tsv)
 * anchors its rows to. That table tests the PREDICATE, which takes the cap as a
 * parameter and is unchanged by any of the splits, so it keeps one anchor. */
export const DOC_CAP_CHARS_DEFAULT = DOC_CAP_CHARS_DEFAULTS.learning;

/** Length in UNICODE CODE POINTS — the unit the server measures in
 * (utf8.RuneCountInString). `String.length` is UTF-16 units and would count an
 * astral character (emoji) TWICE; a byte count would count CJK prose 3× and
 * would shrink the owner's cap to roughly a third of the number he signed off
 * on. Both are the exact mistakes the fixture's multi-byte rows exist to
 * catch. */
export function runeLength(s: string): number {
  return [...s].length;
}

/**
 * Mirrors DocCapBlocked: replacing `before` with `after` is refused when the
 * proposal is over the cap AND is not getting shorter. The three branches,
 * boundaries included:
 *   - after ≤ cap                    → allowed (the ordinary case);
 *   - after > cap AND after < before → allowed (an over-cap doc may converge
 *     downward — the escape hatch that keeps existing over-cap docs editable);
 *   - after > cap AND after ≥ before → REFUSED, EQUAL LENGTH INCLUDED.
 */
export function docCapBlocked(
  cap: number,
  before: string,
  after: string
): boolean {
  return docCapBlockedBySize(cap, runeLength(before), runeLength(after));
}

/** The same predicate over SIZES rather than texts — the shape the version
 * list has since T-1170 (the directory carries each field's char count, never
 * its text). `docCapBlocked` is the text-taking face and is what the shared
 * fixture (bin/tests/fixtures/doc-cap-cases.tsv) drives, so the rule stays
 * measured against the server's twin in exactly one place; this is the same
 * three branches with the measuring already done. */
export function docCapBlockedBySize(
  cap: number,
  beforeChars: number,
  afterChars: number
): boolean {
  if (afterChars <= cap) return false;
  return afterChars >= beforeChars;
}

/** The wire field names each kind's restore runs the cap over. Empty = this
 * kind's restore is never refused on size (global_context / task_description /
 * task_title). */
export const CAPPED_FIELDS: Record<DocumentKind, readonly string[]> = {
  global_context: [],
  // T-ae38: Duty is capped now, on BOTH doors. It was capped on neither, and
  // one door alone would have been decorative — edit the definition down to
  // 999 and restore a 4,000-char earlier revision and the cap is gone.
  role_definition: ["definition_md"],
  lessons: ["text"],
  // T-3809: insight's restore runs the cap over `text` too, and deliberately —
  // an older, larger revision is still a write, so letting history walk the doc
  // back over the limit would make the cap a suggestion
  // (api_document_history.go, case "insight").
  insight: ["text"],
  // The retired bundle has no restore path left at all (both routes 400 since
  // T-1f39); its entry is kept only because this table is total over
  // DocumentKind. The two split kinds each write back exactly ONE field, and
  // restoreTaskManualField judges the cap on that field alone — an over-cap
  // learnings doc no longer blocks a SOP restore.
  task_manual: ["learnings", "sop_md"],
  task_manual_sop: ["sop_md"],
  task_manual_learnings: ["learnings"],
  // EMPTY on purpose (T-e271): the description has never had a length cap on
  // the create side either, so the server runs no cap on this restore. A cap
  // listed here would mark revisions as unrestorable that the server would
  // happily accept — the cockpit inventing a refusal is worse than not having
  // one, because there is no way for the owner to tell it is imaginary.
  task_description: [],
  // EMPTY on purpose (T-2ebe): create_task has never capped a title, and the
  // edit route deliberately declines to introduce a ceiling only the edit door
  // would enforce — so its restore runs no cap either.
  task_title: [],
  // T-791e. Both boot-context blocks are capped on their restore, on `text`,
  // against their own `doc.cap_chars.*` setting — so `capForKind` answers for
  // them and this pair marks for real.
  system_interaction: ["text"],
  boot_sequence: ["text"],
  // T-c9c0, same rule: capped on restore, on `text`, against
  // `doc.cap_chars.offboard`.
  offboard: ["text"],
};

/**
 * Which of a revision's fields would make the server refuse this restore.
 *
 * `sizes` is the revision's per-field CHARACTER COUNT — since T-1170 that is
 * what the version list holds (the text is one read deeper), and it is all this
 * verdict ever needed: the rule compares lengths. A field the revision does not
 * carry is absent, and weighs nothing.
 *
 * `current` holds the LIVE document's values under the SAME wire field names.
 * Pass `undefined` (or omit a field) while the live doc has not loaded: the
 * verdict then abstains rather than judging the revision against an empty
 * string, which would mark a perfectly restorable revision as blocked. An
 * abstention is the honest degraded state — the owner can still click, and the
 * server's own 400 surfaces in the dialog exactly as it did before.
 *
 * `cap` abstains the same way and for the same reason (T-3aeb). Falling back to
 * the shipped default while the setting is still loading would be WRONG in the
 * one direction that matters: the cap can only ever be RAISED, so judging by
 * the default can only ever grey out a revision the server would have accepted
 * — the "greyed out with a reason that is not true" failure this module's
 * header calls the worse of the two.
 */
export function docCapBlockedFields(
  kind: DocumentKind,
  sizes: Record<string, number>,
  current: Record<string, string> | undefined,
  caps: DocCaps | undefined
): string[] {
  const cap = caps && capForKind(kind, caps);
  if (!current || cap === undefined) return [];
  return CAPPED_FIELDS[kind].filter(
    (field) =>
      current[field] !== undefined &&
      docCapBlockedBySize(
        cap,
        runeLength(current[field]),
        // A field the revision does not carry weighs nothing — the same
        // reading `content[field] ?? ""` had.
        sizes[field] ?? 0
      )
  );
}

/** A document's own field sizes, for a caller holding the TEXT rather than a
 * directory row (the reader, once it has fetched the revision it is showing).
 * One measuring rule for both faces of `docCapBlockedFields`. */
export function contentSizes(
  content: Record<string, string>
): Record<string, number> {
  const sizes: Record<string, number> = {};
  for (const [field, value] of Object.entries(content)) {
    if (field !== "tombstoned") sizes[field] = runeLength(value);
  }
  return sizes;
}

/** WHICH cap judges this kind — transcribed from the same switch
 * in restoreDocumentHistory as CAPPED_FIELDS. `undefined` for the kinds that
 * are never refused on size, so a caller cannot accidentally judge them by
 * whichever number happened to be nearest.
 *
 * The task manual's two documents answer to `manualSop` / `manualLearnings`,
 * NOT to any of the three role-journal segments: they are keyed by type_key, so
 * they are assets of a task TYPE rather than entries in a role's journal, and
 * since T-30f1 they answer to one number EACH. */
export function capForKind(
  kind: DocumentKind,
  caps: DocCaps
): number | undefined {
  switch (kind) {
    case "role_definition":
      return caps.duty;
    case "insight":
      return caps.insight;
    case "lessons":
      return caps.learning;
    case "task_manual_sop":
      return caps.manualSop;
    // The retired bundle kind covers BOTH documents, and one number cannot
    // judge two. It gets the learnings cap for the same reason the wire's
    // deprecated `cap_chars` does — and it has no restore path left at all, so
    // nothing reaches this arm today.
    case "task_manual":
    case "task_manual_learnings":
      return caps.manualLearnings;
    case "global_context":
    case "task_description":
    case "task_title":
      return undefined;
    // T-791e. These abstained while the two blocks' caps existed only as the
    // number the server reports on the document's OWN read — there was no live
    // value to hand this table, and inventing one (the shipped 60000/15000)
    // could only ever grey out a revision the server would accept. The same
    // change made them `doc.cap_chars.system_interaction` /
    // `doc.cap_chars.boot_sequence` settings, so the live value now arrives the
    // way every other segment's does and abstaining would leave the one
    // revision the server WILL refuse looking restorable.
    case "system_interaction":
      return caps.systemInteraction;
    // ONE number across both runtimes: the setting is per BLOCK, and claude and
    // codex are two documents of the same block, each measured on its own text.
    case "boot_sequence":
      return caps.bootSequence;
    case "offboard":
      return caps.offboard;
  }
}
