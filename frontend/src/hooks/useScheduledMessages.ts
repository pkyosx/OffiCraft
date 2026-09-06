// hooks/useScheduledMessages.ts — load a member's scheduled messages (T-f059
// 定期訊息) through the api client + expose create/update/delete mutations that
// refetch. Scoped to one member (the detail panel's 定期訊息 section).
//
// 🔴 EVERY mutation awaits a refetch before it resolves. The acceptance wording
// is "改完立即生效、不需重開成員" — the list the owner is looking at IS the
// evidence, so the list has to come back from the server rather than from an
// optimistic local splice that could disagree with what will actually fire.

import { useCallback, useEffect, useState } from "react";
import type {
  ScheduledMessage,
  ScheduledMessageCreateInput,
  ScheduledMessageUpdate,
} from "../api/adapter";
import { api } from "../api";

interface UseScheduledMessages {
  items: ScheduledMessage[];
  loading: boolean;
  /** True when the mount fetch REJECTED (non-401). Distinguishes a failed load
   * from honest-empty. */
  error: boolean;
  refetch: () => Promise<void>;
  /** Create, then refetch. Resolves with the id the SERVER minted — T-91: the
   * write answers a receipt, not the row, so the row itself comes from the
   * refetch this awaits (`items`), never from the write. */
  create: (input: ScheduledMessageCreateInput) => Promise<{ id: string }>;
  /** Edit, then refetch. Same shape and same reason as `create`. */
  update: (
    scheduleId: string,
    patch: ScheduledMessageUpdate
  ) => Promise<{ id: string }>;
  remove: (scheduleId: string) => Promise<void>;
}

export function useScheduledMessages(memberId: string): UseScheduledMessages {
  const [items, setItems] = useState<ScheduledMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const refetch = useCallback(async () => {
    const next = await api.listScheduledMessages(memberId);
    setItems(next);
    setError(false);
  }, [memberId]);

  useEffect(() => {
    let alive = true;
    setLoading(true);
    api
      .listScheduledMessages(memberId)
      .then((next) => {
        if (alive) {
          setItems(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useScheduledMessages: initial load failed", e);
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
    async (input: ScheduledMessageCreateInput) => {
      const created = await api.createScheduledMessage(memberId, input);
      // The schedule exists from the line above — the list read only places it.
      try {
        await refetch();
      } catch (e) {
        console.warn(
          "useScheduledMessages: post-create refetch failed (the schedule was created)",
          e
        );
      }
      return created;
    },
    [memberId, refetch]
  );

  const update = useCallback(
    async (scheduleId: string, patch: ScheduledMessageUpdate) => {
      const updated = await api.updateScheduledMessage(
        memberId,
        scheduleId,
        patch
      );
      // The patch has landed; the list read is the separate step.
      try {
        await refetch();
      } catch (e) {
        console.warn(
          "useScheduledMessages: post-update refetch failed (the schedule was updated)",
          e
        );
      }
      return updated;
    },
    [memberId, refetch]
  );

  const remove = useCallback(
    async (scheduleId: string) => {
      await api.deleteScheduledMessage(memberId, scheduleId);
      // The schedule is deleted whatever the list read does next.
      try {
        await refetch();
      } catch (e) {
        console.warn(
          "useScheduledMessages: post-delete refetch failed (the schedule was deleted)",
          e
        );
      }
    },
    [memberId, refetch]
  );

  return { items, loading, error, refetch, create, update, remove };
}
