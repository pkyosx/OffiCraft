// hooks/useWebhooks.ts — load a member's webhook endpoints (M4 回呼端點) through
// the api client + expose create/toggle/delete mutations that refetch. Scoped
// to one member (the member-detail panel's 回呼端點 section).

import { useCallback, useEffect, useState } from "react";
import type {
  WebhookEndpoint,
  WebhookCreateInput,
  WebhookUpdate,
} from "../api/adapter";
import { api } from "../api";

interface UseWebhooks {
  webhooks: WebhookEndpoint[];
  loading: boolean;
  /** True when the mount fetch REJECTED (non-401). Distinguishes a failed load
   * from honest-empty. */
  error: boolean;
  refetch: () => Promise<void>;
  create: (input: WebhookCreateInput) => Promise<WebhookEndpoint>;
  update: (
    endpointId: string,
    patch: WebhookUpdate
  ) => Promise<WebhookEndpoint>;
  remove: (endpointId: string) => Promise<void>;
}

export function useWebhooks(memberId: string): UseWebhooks {
  const [webhooks, setWebhooks] = useState<WebhookEndpoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    const next = await api.listWebhooks(memberId);
    setWebhooks(next);
    setError(false);
  }, [memberId]);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    api
      .listWebhooks(memberId)
      .then((next) => {
        if (alive) {
          setWebhooks(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useWebhooks: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [memberId]);

  const create = useCallback(
    async (input: WebhookCreateInput) => {
      const created = await api.createWebhook(memberId, input);
      await refetch();
      return created;
    },
    [memberId, refetch]
  );

  const update = useCallback(
    async (endpointId: string, patch: WebhookUpdate) => {
      const updated = await api.updateWebhook(memberId, endpointId, patch);
      await refetch();
      return updated;
    },
    [memberId, refetch]
  );

  const remove = useCallback(
    async (endpointId: string) => {
      await api.deleteWebhook(memberId, endpointId);
      await refetch();
    },
    [memberId, refetch]
  );

  return { webhooks, loading, error, refetch, create, update, remove };
}
