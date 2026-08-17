// test/documentHistory.ts — read a document's retained revisions WITH their
// text, the way the cockpit does since T-1170.
//
// 🔴 The two reads are the change under test, so this helper performs both
// rather than hiding one. The list answers a DIRECTORY (identity, actor, time,
// the tombstone flag, per-field sizes); the text of a revision comes from
// NAMING it. A helper that fabricated `content` out of the list row would be a
// fake more generous than the adapter it stands in for — the exact failure
// api/dtoParity.ts exists to talk about — and every assertion below it would be
// measured against a server that does not exist.

import { vi } from "vitest";
import type { Api } from "../api/adapter";
import { ApiError } from "../api/errors";
import { contentSizes } from "../api/docCap";
import { toDocumentHistoryEntry } from "../api/mappers";
import type {
  DocumentHistoryEntryView,
  DocumentHistoryView,
  DocumentKind,
} from "../types";

export type RevisionWithContent = DocumentHistoryEntryView & {
  content: Record<string, string>;
};

/** Every retained revision of one document, newest first, each with the text
 * its own read carries. */
export async function documentRevisions(
  api: Pick<Api, "listDocumentHistory" | "getDocumentRevision">,
  kind: DocumentKind,
  key: string
): Promise<RevisionWithContent[]> {
  const rows = await api.listDocumentHistory(kind, key);
  return Promise.all(
    rows.map(async (row) => ({
      ...row,
      content: (await api.getDocumentRevision(kind, key, row.id)).content,
    }))
  );
}

/**
 * Stand in for BOTH document-history reads from a set of full revisions.
 *
 * 🔴 Fakes that answer only `listDocumentHistory` are the shape this repo has
 * been burned by before (api/dtoParity.ts): a fake more generous than the wire
 * measures every assertion above it against a server that does not exist. Since
 * T-1170 the list answers a DIRECTORY, so a fixture written as "here are the
 * revisions" has to be projected — the row loses its text here, exactly where
 * the adapter loses it, and the text is served only to a caller that NAMES an
 * id. An unknown id rejects, the way a pruned one does.
 */
export function stubDocumentHistory(
  api: Pick<Api, "listDocumentHistory" | "getDocumentRevision">,
  revisions: DocumentHistoryView[]
): void {
  vi.spyOn(api, "listDocumentHistory").mockImplementation(async () =>
    revisions.map((r) =>
      toDocumentHistoryEntry({
        id: r.id,
        created_ts: r.createdTs,
        actor_id: r.actorId,
        tombstoned: r.content["tombstoned"] === "true",
        field_chars: contentSizes(r.content),
      })
    )
  );
  vi.spyOn(api, "getDocumentRevision").mockImplementation(async (_k, _key, id) => {
    const found = revisions.find((r) => r.id === id);
    if (!found) {
      throw new ApiError(
        `http 404 for GET /api/document-history/${_k}/${_key}/${id}`,
        404,
        "not_found",
        `document revision ${id} is no longer retained`
      );
    }
    return { id: found.id, content: found.content };
  });
}
