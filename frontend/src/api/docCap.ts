// api/docCap.ts — the frontend's ONE copy of the server's document size cap.
//
// ⚠️ THE AUTHORITY FOR THIS RULE IS THE SERVER, NOT THIS FILE.
// `DocCapBlocked` in server/ocserverd/domain.go decides whether a write is
// refused, against the live `doc.cap_chars` setting (T-3aeb — the cap is no
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
// the shape of the DTO: global_context and role_definition are NOT capped at
// all (their restore calls DocCapBlocked nowhere), lessons caps `text`, and
// task_manual caps `learnings` AND `sop_md` — either one over the cap refuses
// the whole restore.

import type { DocumentKind } from "../types";

/** contextDocMaxCharsDefault (server/ocserverd/domain.go) — the SHIPPED DEFAULT
 * of the cap, not the cap itself. Since T-3aeb the live value is the
 * `doc_cap_chars` setting, so callers pass the cap in; this constant exists only
 * as the fallback for a caller that has no server value yet, and as the shared
 * fixture's anchor. Do not inline this number anywhere else. */
export const DOC_CAP_CHARS_DEFAULT = 10000;

/** Length in UNICODE CODE POINTS — the unit the server measures in
 * (utf8.RuneCountInString). `String.length` is UTF-16 units and would count an
 * astral character (emoji) TWICE; a byte count would count CJK prose 3× and
 * turn the owner's 10,000-character cap into a ~4,000-character one. Both are
 * the exact mistakes the fixture's multi-byte rows exist to catch. */
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
  const n = runeLength(after);
  if (n <= cap) return false;
  return n >= runeLength(before);
}

/** The wire field names each kind's restore runs the cap over. Empty = this
 * kind's restore is never refused on size (global_context / role_definition). */
export const CAPPED_FIELDS: Record<DocumentKind, readonly string[]> = {
  global_context: [],
  role_definition: [],
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
};

/**
 * Which of a revision's fields would make the server refuse this restore.
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
  content: Record<string, string>,
  current: Record<string, string> | undefined,
  cap: number | undefined
): string[] {
  if (!current || cap === undefined) return [];
  return CAPPED_FIELDS[kind].filter(
    (field) =>
      current[field] !== undefined &&
      docCapBlocked(cap, current[field], content[field] ?? "")
  );
}
