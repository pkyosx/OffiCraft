// hooks/useSharedDocs.ts — the 共用文件 list and one document, each re-read on
// mount, after this page's own writes, and on every `shared_doc` delta (members
// write these too, so a delta is how another writer's change reaches the page).

import { useCallback, useEffect, useRef, useState } from "react";
import type { SharedDocDetailView, SharedDocView } from "../types";
import { api } from "../api";
import { isHttpStatus, serverMessageOf } from "../api/errors";

function useSharedDocTopic(reload: () => Promise<void>) {
  useEffect(
    () =>
      api.subscribeEvents((topic) => {
        if (topic === "shared_doc") void reload();
      }),
    [reload],
  );
}

/** Runs one write and answers "" on success or the message to show. */
export function useSharedDocAction(fallbackMessage: string) {
  return useCallback(
    async (action: () => Promise<unknown>): Promise<string> => {
      try {
        await action();
        return "";
      } catch (e: unknown) {
        return serverMessageOf(e) || fallbackMessage;
      }
    },
    [fallbackMessage],
  );
}

export function useSharedDocs(includeArchived: boolean, fallbackMessage: string) {
  const [docs, setDocs] = useState<SharedDocView[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const alive = useRef(true);

  const reload = useCallback(async () => {
    try {
      const list = await api.listSharedDocs(includeArchived);
      if (!alive.current) return;
      setDocs(list);
      setLoadError("");
    } catch (e: unknown) {
      console.warn("useSharedDocs: load failed", e);
      if (alive.current) setLoadError(serverMessageOf(e) || fallbackMessage);
    } finally {
      if (alive.current) setLoading(false);
    }
  }, [includeArchived, fallbackMessage]);

  useEffect(() => {
    alive.current = true;
    void reload();
    return () => {
      alive.current = false;
    };
  }, [reload]);
  useSharedDocTopic(reload);

  return { docs, loading, loadError, reload };
}

export function useSharedDoc(id: string, fallbackMessage: string) {
  const [doc, setDoc] = useState<SharedDocDetailView | null>(null);
  const [notFound, setNotFound] = useState(false);
  const [loadError, setLoadError] = useState("");
  const alive = useRef(true);

  const reload = useCallback(async () => {
    try {
      const next = await api.getSharedDoc(id);
      if (!alive.current) return;
      setDoc(next);
      setNotFound(false);
      setLoadError("");
    } catch (e: unknown) {
      if (!alive.current) return;
      if (isHttpStatus(e, 404)) {
        setDoc(null);
        setNotFound(true);
        return;
      }
      console.warn("useSharedDoc: load failed", e);
      setLoadError(serverMessageOf(e) || fallbackMessage);
    }
  }, [id, fallbackMessage]);

  useEffect(() => {
    alive.current = true;
    setDoc(null);
    setNotFound(false);
    void reload();
    return () => {
      alive.current = false;
    };
  }, [reload]);
  useSharedDocTopic(reload);

  return { doc, notFound, loadError, reload };
}
