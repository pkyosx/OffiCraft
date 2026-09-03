// lib/docLiveContent.ts — read the LIVE ("current") content of one editable
// long-form document, in the SAME field names a retained revision carries.
//
// T-59 second round: a compare attachment's side may name a document at a point
// in time — a retained revision, the shipped default, or `current`. The first
// two already have one route each that answers a content map
// (`/api/document-history/{kind}/{key}/{id}` and `.../seed`). `current` has no
// such route: every kind's live content is served by ITS OWN endpoint returning
// ITS OWN view type, and the reshaping into `{field: text}` is today done by
// hand at each of the seven history call sites.
//
// So the mapping lives HERE, on the reader, and that is a deliberate choice
// rather than the cheap one:
//
//   * The server has no primitive to reuse. The `Snapshot` functions that write
//     a retained revision read the OVERLAY row, not the folded document, so for
//     a document that has never been edited they answer empty where the live
//     reader answers the seed. A server-side `/current` would therefore be a
//     NEW fold-dispatcher over every kind, not a wrapper over an existing one.
//   * A server-side kind switch fails SILENTLY when a kind is added (they are
//     switches on a plain string, and the wire's `kind` has no enum). This table
//     is a `Record<DocumentKind, …>` — a kind added to the union without an
//     entry here does not compile.
//
// The field names are the ones `docHistoryFields.ts` declares, and
// `docLiveContent.test.ts` pins the two tables against each other: a field
// renamed in one place and not the other would otherwise render 「沒有差異」
// against every retained version, which is the worst way to be wrong.

import type { DocumentKind, BootDocKind } from "../types";
import { api } from "../api";

/** Thrown when the address names a document whose live content cannot be read.
 * The compare screen reports it exactly as it reports a pruned revision — one
 * side is not there, so nothing is drawn. */
export class DocLiveContentUnavailable extends Error {}

const bootDoc = (kind: BootDocKind) => async (key: string) => {
  const doc = await api.getBootDoc(kind, key);
  return { text: doc.text };
};

/** kind → how to read that kind's live content as a revision-shaped map.
 * Total over DocumentKind on purpose (see the header). */
const LIVE_CONTENT: Record<
  DocumentKind,
  (key: string) => Promise<Record<string, string>>
> = {
  global_context: async () => ({ text: (await api.getGlobalContext()).text }),
  role_definition: async (key) => ({
    definition_md: (await api.getRole(key)).definitionMd,
  }),
  lessons: async (key) => ({ text: (await api.getLessons(key)).text }),
  insight: async (key) => ({ text: (await api.getInsight(key)).text }),
  // RETIRED: the whole-manual kind answers 400 on every document-history face —
  // it was split into the two single-field kinds below. An address naming it is
  // a side that cannot be read, which is exactly what the honest failure says;
  // the entry exists because the table is total, not because the kind works.
  task_manual: async () => {
    throw new DocLiveContentUnavailable("task_manual is retired");
  },
  task_manual_sop: async (key) => ({
    sop_md: (await api.getTaskManual(key)).sopMd,
  }),
  task_manual_learnings: async (key) => ({
    learnings: (await api.getTaskManual(key)).learnings,
  }),
  // A task's description and title are two fields of the same row, so both read
  // the task — the address's `field` picks which one is compared.
  task_description: async (key) => ({
    description: (await api.getTask(key)).description,
  }),
  task_title: async (key) => ({ title: (await api.getTask(key)).title }),
  system_interaction: bootDoc("system_interaction"),
  boot_sequence: bootDoc("boot_sequence"),
  offboard: bootDoc("offboard"),
  accelerated_stop: bootDoc("accelerated_stop"),
  task_closeout: bootDoc("task_closeout"),
  task_reassign_predecessor: bootDoc("task_reassign_predecessor"),
  task_takeover_with_predecessor: bootDoc("task_takeover_with_predecessor"),
  task_takeover_fresh: bootDoc("task_takeover_fresh"),
  task_unblocked: bootDoc("task_unblocked"),
};

/** True when `kind` is a document kind this cockpit knows. The wire carries it
 * as a free string (the server validates the ADDRESS's shape, not its
 * membership — a kind list on the server would be a second enumeration going
 * stale silently), so the reader is where an unknown one is caught. */
export function isDocumentKind(kind: string): kind is DocumentKind {
  return Object.prototype.hasOwnProperty.call(LIVE_CONTENT, kind);
}

/** The document's live content map, keyed by revision field name. */
export function readLiveDocumentContent(
  kind: DocumentKind,
  key: string
): Promise<Record<string, string>> {
  return LIVE_CONTENT[kind](key);
}
