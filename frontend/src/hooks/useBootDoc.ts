// hooks/useBootDoc.ts — load + mutate ONE boot-context block (T-791e).
//
// Same shape as useGlobalContext (mount fetch + reconcile-by-refetch on the
// document's SSE topic; the mutation response IS the folded doc, so the UI never
// fabricates the is_default flip locally), with two differences that both come
// straight off this ticket's risk:
//
//   1. `reset` DOES NOT DEPEND ON `doc`. A broken boot sequence means agents
//      never attach to SSE, never come online, and nobody is left to fix it —
//      so the factory restore has to work from a page whose read failed. It
//      calls the adapter with the (kind, key) it was constructed with and
//      adopts the response, and it never reads state that a failed load left
//      empty.
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
  save: (text: string) => Promise<void>;
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

  const save = useCallback(async (text: string) => {
    const { kind: k, key: docKey } = target.current;
    setDoc(await api.saveBootDoc(k, docKey, text));
    setError(false);
  }, []);

  const reset = useCallback(async () => {
    const { kind: k, key: docKey } = target.current;
    setDoc(await api.resetBootDoc(k, docKey));
    setError(false);
  }, []);

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
