// useChatDelete — the owner's 「刪除」 flow for one chat room: ask, confirm,
// call the server, and remember what it answered.
//
// The thread itself is refetched by the `chat` SSE delta the delete emits; the
// 200 response is kept here so the row turns into 「此訊息已刪除」 the moment the
// server says so, without waiting for that refetch. A delete cannot be undone,
// so a remembered timestamp can never go stale and is never cleared.
//
// ⚠️ That refetch is the newest page only. A delete made in another owner tab
// of a row this room holds further back would stay undeleted here, so the
// delete's own delta, which carries `deleted_ts`, marks it directly.
//
// State dies with the room: ChatArea is mounted under `key={peerId}`.

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { api } from "../api";
import type { ChatMessage } from "../api/adapter";
import { createDeltaSink } from "../lib/deltaSink";
import { serverMessageOf } from "../api/errors";
import { useI18n } from "../i18n";
import { ConfirmModal } from "../components/ConfirmModal";

export type ChatDelete = {
  /** `m.deletedTs`, or the delete this room just made of `m`; 0 = not deleted. */
  deletedTsOf: (m: ChatMessage) => number;
  /** Open the confirm dialog for message `id`. */
  ask: (id: string) => void;
  /** The confirm dialog, or null. Render it from the caller's tree. */
  dialog: ReactNode;
};

export function useChatDelete(): ChatDelete {
  const { t } = useI18n();
  const [deleted, setDeleted] = useState<ReadonlyMap<string, number>>(
    () => new Map(),
  );
  const remember = useCallback((id: string, ts: number) => {
    setDeleted((prev) => (prev.has(id) ? prev : new Map(prev).set(id, ts)));
  }, []);
  useEffect(
    () =>
      api.subscribeEvents(
        createDeltaSink((batch) => {
          for (const d of batch.deltas) {
            if (d.topic === "chat" && d.names.id && d.deletedTs) remember(d.names.id, d.deletedTs);
          }
        }),
      ),
    [remember],
  );

  const [target, setTarget] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Same-tick double clicks both read the pre-update `busy`.
  const busyRef = useRef(false);

  const deletedTsOf = useCallback(
    (m: ChatMessage) => m.deletedTs || deleted.get(m.id) || 0,
    [deleted],
  );

  const ask = useCallback((id: string) => {
    setError(null);
    setTarget(id);
  }, []);

  async function confirm(id: string) {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    try {
      const m = await api.deleteChatMessage(id);
      remember(m.id, m.deletedTs || Date.now() / 1000);
      setTarget(null);
    } catch (e) {
      setError(serverMessageOf(e) || t.chat.deleteFailed);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }

  const dialog =
    target === null ? null : (
      <ConfirmModal
        testId="chat-delete-confirm"
        confirmTestId="chat-delete-confirm-btn"
        body={t.chat.deleteConfirmBody}
        error={error}
        cancelLabel={t.common.cancel}
        confirmLabel={t.chat.deleteConfirm}
        busy={busy}
        danger
        onCancel={() => setTarget(null)}
        onConfirm={() => void confirm(target)}
      />
    );

  return { deletedTsOf, ask, dialog };
}
