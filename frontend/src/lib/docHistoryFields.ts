// lib/docHistoryFields.ts — which fields a retained revision of each document
// kind carries, and in what order.
//
// Extracted from DocumentHistoryCard when the history MODAL (T-1f39) started
// reading the same snapshots: the list row previews them, the modal renders
// them and the diff pane pairs them against the live document. One table, so a
// kind whose wire shape changes cannot end up ordered one way on the row and
// another way inside the modal.
//
// The wire map's own key order is NOT a contract, which is the whole reason
// this exists; an unknown field is appended verbatim rather than dropped, so a
// field the server grows before this file learns about it is still shown.

import type { DocumentKind } from "../types";

/** Display order per kind. `tombstoned` is never a content field — it is a
 * flag the surfaces render as a badge. */
export const DOC_FIELD_ORDER: Record<DocumentKind, readonly string[]> = {
  global_context: ["text"],
  // The role's NAME is not versioned (owner ruling 2026-07-31 「名稱不用留版
  // 本」— the role doc says what the role DOES, not what it is called), so it
  // is not a field of a revision. Rows written before that ruling still carry
  // one; see IGNORED_FIELDS.
  role_definition: ["definition_md"],
  lessons: ["text"],
  insight: ["text"],
  task_manual: ["purpose", "fields", "sop_md", "learnings"],
  task_manual_sop: ["sop_md"],
  task_manual_learnings: ["learnings"],
  // T-e271. One field, because a task's description IS one field — there is no
  // second thing a revision of it could carry.
  task_description: ["description"],
  // T-2ebe. Same shape as the description's, one field for the same reason.
  task_title: ["title"],
  // T-791e. One field each — a boot-context block IS its text. `tombstoned`
  // rides alongside on both (they are overlay kinds with a factory version to
  // fall back to) and is a flag, not content, so it is not listed.
  system_interaction: ["text"],
  boot_sequence: ["text"],
  // T-c9c0. Same overlay shape, same single field.
  offboard: ["text"],
  // T-3201. The six lifecycle procedures are the same overlay shape again —
  // a document IS its text. The two read-only ones keep no versions at all
  // (nothing may write them), and their entry costs nothing: this table is
  // total over DocumentKind, so a kind with no rows is simply never asked.
  accelerated_stop: ["text"],
  task_closeout: ["text"],
  task_reassign_predecessor: ["text"],
  task_takeover_with_predecessor: ["text"],
  task_takeover_fresh: ["text"],
  task_unblocked: ["text"],
};

/** Keys a revision may CARRY but that are not content of the document.
 * `tombstoned` is a flag every kind renders as a badge; `name` is a role's
 * label, de-versioned on 2026-07-31 — rows written before that still hold one,
 * and the append-unknown-fields rule below would otherwise put it straight back
 * on screen, which is exactly what the ruling removed. */
const IGNORED_FIELDS: Partial<Record<DocumentKind, readonly string[]>> = {
  role_definition: ["name"],
};

function isContentField(kind: DocumentKind, name: string): boolean {
  return (
    name !== "tombstoned" &&
    !(IGNORED_FIELDS[kind] ?? []).includes(name) &&
    !DOC_FIELD_ORDER[kind].includes(name)
  );
}

/** The field names present in `content`, in display order, unknown ones last. */
export function documentFieldNames(
  kind: DocumentKind,
  content: Record<string, string>
): string[] {
  return [
    ...DOC_FIELD_ORDER[kind].filter((f) => f in content),
    ...Object.keys(content).filter((f) => isContentField(kind, f)),
  ];
}

/** The same order, but as [name, value] pairs and with blank values dropped —
 * what a reader is shown. */
export function documentFields(
  kind: DocumentKind,
  content: Record<string, string>
): [string, string][] {
  return documentFieldNames(kind, content)
    .map((name): [string, string] => [name, content[name] ?? ""])
    .filter(([, value]) => value.trim() !== "");
}

/**
 * Does this revision carry any content at all, judged from the DIRECTORY row's
 * size map (T-1170) rather than from text the list no longer has?
 *
 * ⚠️ One honest difference from `documentFields`, which drops a value whose
 * `trim()` is empty: a whitespace-only field has a non-zero size, so it counts
 * as content here. The row would then say nothing instead of 「(當時是空白內容)」,
 * and the reader one click deeper — which does hold the text — still gets it
 * right. Nobody can tell whitespace from text by counting characters, and
 * inventing an answer is the worse of the two.
 */
export function documentHasContent(
  kind: DocumentKind,
  sizes: Record<string, number>
): boolean {
  return Object.entries(sizes).some(
    ([name, size]) =>
      size > 0 &&
      (DOC_FIELD_ORDER[kind].includes(name) || isContentField(kind, name))
  );
}

/**
 * The fields a comparison must walk: everything either side carries, in one
 * order. A field the revision has and the current document does not (or the
 * other way round) is a DIFFERENCE, so taking one side's names alone would
 * hide exactly the change the reader opened the diff for.
 */
export function comparedFieldNames(
  kind: DocumentKind,
  content: Record<string, string>,
  current: Record<string, string>
): string[] {
  const names = documentFieldNames(kind, content);
  for (const name of documentFieldNames(kind, current)) {
    if (!names.includes(name)) names.push(name);
  }
  // A field that is blank on BOTH sides has nothing to compare and no content
  // to show; it is not evidence of anything.
  return names.filter(
    (name) => (content[name] ?? "") !== "" || (current[name] ?? "") !== ""
  );
}
