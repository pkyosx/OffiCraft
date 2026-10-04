// hooks/useDocumentSeed.ts — the DEFAULT CONTENT of one editable long-form
// document, so 預設內容 can be READ and COMPARED before anyone restores it
// (T-40f0, owner rc-28885813e065 ①).
//
// Deliberately NOT folded into useDocumentHistory, even though both feed the
// same list:
//   * it is a DIFFERENT request with a different failure meaning. The retained
//     versions come and go; a document's shipped default does not change while
//     the cockpit is open, so this fetches once per (kind, key) and subscribes
//     to nothing. Giving it a slot in the history hook would put it behind that
//     hook's SSE refetch for no reason.
//   * the two failures must stay independent. 預設內容 is the only reset the
//     cockpit has left, and T-1f39 already had to un-couple it from the history
//     GET once (rendering it inside the success branch made 重置 the hostage of
//     an unrelated request). `error` here therefore means "cannot show or
//     compare the default", NEVER "there is no default to restore" — the row and
//     its restore stay reachable regardless.
//
// `enabled` is the same literal reading of the owner's 「有點選的時候再打 API」
// as the history hook's: nothing is requested until the list is opened.

import { useEffect, useState } from "react";
import type { DocumentKind } from "../types";
import { api } from "../api";

interface UseDocumentSeed {
  /** The default's content under the revision field names, or `undefined` while
   * loading, when the load failed, or when this document HAS no default. */
  content?: Record<string, string>;
  loading: boolean;
  /** True when the load REJECTED. A 404 is NOT an error — it is the honest
   * answer "this document ships no default", and the surfaces that render a
   * 預設內容 row only render one where a reset exists anyway. */
  error: boolean;
}

export function useDocumentSeed(
  kind: DocumentKind,
  key: string,
  options: { enabled?: boolean } = {}
): UseDocumentSeed {
  const enabled = options.enabled ?? true;
  const [content, setContent] = useState<Record<string, string> | undefined>();
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);

  useEffect(() => {
    if (!enabled) return;
    let alive = true;
    setLoading(true);
    setError(false);
    api
      .getDocumentSeed(kind, key)
      .then((seed) => {
        if (alive) setContent(seed.content);
      })
      .catch((e) => {
        console.warn("useDocumentSeed: load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [kind, key, enabled]);

  return { content, loading, error };
}
