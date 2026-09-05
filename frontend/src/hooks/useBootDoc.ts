// hooks/useBootDoc.ts — load + mutate ONE boot-context block (T-791e).
//
// Same shape as useGlobalContext (mount fetch + reconcile-by-refetch on the
// document's SSE topic; every write is followed by a re-read, so the UI never
// fabricates the is_default flip locally), with two differences that both come
// straight off this ticket's risk:
//
//   1. `reset` DOES NOT DEPEND ON `doc`. A broken boot sequence means agents
//      never attach to SSE, never come online, and nobody is left to fix it —
//      so the factory restore has to work from a page whose read failed. It
//      calls the adapter with the (kind, key) it was constructed with and then
//      re-reads that same address, and it never reads state that a failed load
//      left empty.
//   2. `kind`/`key` are read fresh on every call rather than closed over once
//      per mount. The claude and codex documents are DIFFERENT documents, and a
//      stale closure here would be the exact defect the ticket forbids: a save
//      typed into one runtime's page landing on the other's key.

import { useCallback, useEffect, useRef, useState } from "react";
import type { BootDocKind, BootDocView } from "../types";
import { api } from "../api";

interface UseBootDoc {
  doc: BootDocView | null;
  loading: boolean;
  /** True when the mount fetch REJECTED (non-401; 401 bounces to login at the
   * http layer). Lets the page tell a failed load apart from an honest doc —
   * and keep offering the factory restore either way. */
  error: boolean;
  refetch: () => Promise<void>;
  /** Sends the EDITABLE HALF (T-3201). The read-only head has no field on the
   * wire, so there is nothing here that could carry an edit to it. */
  save: (body: string) => Promise<void>;
  reset: () => Promise<void>;
}

export function useBootDoc(kind: BootDocKind, key: string): UseBootDoc {
  const [doc, setDoc] = useState<BootDocView | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  // The address this hook is pointed at, read at CALL time. See the header:
  // closing over it once would let a re-pointed page write to the old key.
  const target = useRef({ kind, key });
  target.current = { kind, key };

  const refetch = useCallback(async () => {
    const { kind: k, key: docKey } = target.current;
    setDoc(await api.getBootDoc(k, docKey));
    setError(false);
  }, []);

  // 🔴 T-91: WRITE THEN RE-READ. Both used to adopt the write's own answer,
  // which was the folded document. The boot-doc receipt keeps size/cap/sha256
  // and is_default and drops the text (read_only_head included), so adopting it
  // would leave the editor showing an empty document over a real boot context —
  // and `reset`'s whole job is to be the recovery path when that context is
  // broken, so a silently blank one there is the worst version of this bug.
  // The re-read still does not depend on `doc`: it reads (kind, key) off the ref
  // exactly as the write does, so the factory restore keeps working from a page
  // whose load failed.
  const save = useCallback(
    async (body: string) => {
      const { kind: k, key: docKey } = target.current;
      await api.saveBootDoc(k, docKey, body);
      await refetch();
    },
    [refetch]
  );

  const reset = useCallback(async () => {
    const { kind: k, key: docKey } = target.current;
    await api.resetBootDoc(k, docKey);
    await refetch();
  }, [refetch]);

  useEffect(() => {
    let alive = true;
    setLoading(true);

    const read = (onFail: (e: unknown) => void) =>
      api
        .getBootDoc(kind, key)
        .then((next) => {
          if (alive) {
            setDoc(next);
            setError(false);
          }
        })
        .catch(onFail);

    read((e) => {
      console.warn("useBootDoc: initial load failed", e);
      if (alive) setError(true);
    }).finally(() => {
      if (alive) setLoading(false);
    });

    const unsubscribe = api.subscribeEvents((topic) => {
      // The blocks ride the existing `global_context` topic — see TOPIC_OF in
      // hooks/useDocumentHistory.ts for why a topic named after the block would
      // fan nothing at all.
      if (topic.includes("global_context")) {
        void read((e) => console.warn("useBootDoc: SSE refetch failed", e));
      }
    });

    return () => {
      alive = false;
      unsubscribe();
    };
  }, [kind, key]);

  return { doc, loading, error, refetch, save, reset };
}
