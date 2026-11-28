// hooks/useShareLinks.ts — the 分享 page's data: every share link still in
// force. No SSE topic carries share links, so the list is read on mount and
// re-read after this page's own revoke.

import { useCallback, useEffect, useRef, useState } from "react";
import type { ShareLinkView } from "../types";
import { api } from "../api";
import { ApiError, serverMessageOf } from "../api/errors";

interface UseShareLinks {
  links: ShareLinkView[];
  loading: boolean;
  loadError: string;
  /** Resolves to "" on success or the failure message. */
  revoke: (id: string) => Promise<string>;
}

export function useShareLinks(loadFailed: string, revokeFailed: string): UseShareLinks {
  const [links, setLinks] = useState<ShareLinkView[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const alive = useRef(true);

  const reload = useCallback(async () => {
    try {
      const list = await api.listShareLinks();
      if (!alive.current) return;
      setLinks([...list].reverse());
      setLoadError("");
    } catch (e: unknown) {
      console.warn("useShareLinks: load failed", e);
      if (alive.current) setLoadError(serverMessageOf(e) || loadFailed);
    } finally {
      if (alive.current) setLoading(false);
    }
  }, [loadFailed]);

  useEffect(() => {
    alive.current = true;
    void reload();
    return () => {
      alive.current = false;
    };
  }, [reload]);

  const revoke = useCallback(
    async (id: string): Promise<string> => {
      try {
        await api.revokeShareLink(id);
      } catch (e: unknown) {
        const alreadyGone = e instanceof ApiError && e.status === 404;
        if (!alreadyGone) return serverMessageOf(e) || revokeFailed;
      }
      await reload();
      return "";
    },
    [reload, revokeFailed],
  );

  return { links, loading, loadError, revoke };
}
