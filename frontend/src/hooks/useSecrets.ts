// hooks/useSecrets.ts — the owner's Secrets page data: the secret list and the
// access log. No SSE topic carries secrets, so both are read on mount and
// re-read after each of this page's own writes.

import { useCallback, useEffect, useRef, useState } from "react";
import type {
  SecretAccessEntryView,
  SecretCreateInput,
  SecretUpdateInput,
  SecretView,
} from "../types";
import { api } from "../api";
import { serverMessageOf } from "../api/errors";

interface UseSecrets {
  secrets: SecretView[];
  accessLog: SecretAccessEntryView[];
  loading: boolean;
  /** The last load failure's message, or "". */
  loadError: string;
  /** Each write resolves to "" on success or the failure message, so the form
   * that pressed can keep its own error line and stay open. */
  create: (input: SecretCreateInput) => Promise<string>;
  update: (id: string, patch: SecretUpdateInput) => Promise<string>;
  remove: (id: string) => Promise<string>;
}

export function useSecrets(fallbackMessage: string): UseSecrets {
  const [secrets, setSecrets] = useState<SecretView[]>([]);
  const [accessLog, setAccessLog] = useState<SecretAccessEntryView[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const alive = useRef(true);

  const reload = useCallback(async () => {
    try {
      const [list, log] = await Promise.all([
        api.listSecrets(),
        api.listSecretAccessLog(),
      ]);
      if (!alive.current) return;
      setSecrets(list);
      setAccessLog(log);
      setLoadError("");
    } catch (e: unknown) {
      console.warn("useSecrets: load failed", e);
      if (alive.current) setLoadError(serverMessageOf(e) || fallbackMessage);
    } finally {
      if (alive.current) setLoading(false);
    }
  }, [fallbackMessage]);

  useEffect(() => {
    alive.current = true;
    void reload();
    return () => {
      alive.current = false;
    };
  }, [reload]);

  const run = useCallback(
    async (action: () => Promise<unknown>): Promise<string> => {
      try {
        await action();
      } catch (e: unknown) {
        return serverMessageOf(e) || fallbackMessage;
      }
      await reload();
      return "";
    },
    [fallbackMessage, reload],
  );

  const create = useCallback(
    (input: SecretCreateInput) => run(() => api.createSecret(input)),
    [run],
  );
  const update = useCallback(
    (id: string, patch: SecretUpdateInput) =>
      run(() => api.updateSecret(id, patch)),
    [run],
  );
  const remove = useCallback(
    (id: string) => run(() => api.deleteSecret(id)),
    [run],
  );

  return { secrets, accessLog, loading, loadError, create, update, remove };
}
