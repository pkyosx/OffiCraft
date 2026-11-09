// useChatRecall — the owner's 「刪除」 flow for one chat room: ask, confirm,
// call the server, and remember what it answered.
//
// The thread itself is refetched by the `chat` SSE delta the recall emits; the
// 200 response is kept here so the row turns into 「此訊息已刪除」 the moment the
// server says so, without waiting for that refetch. A recall cannot be undone,
// so a remembered timestamp can never go stale and is never cleared.
//
// ⚠️ That refetch is the newest page only. A recall made in another owner tab
// of a row this room holds further back would stay unrecalled here, so a `chat`
// delta naming such a row reads it back by id.
//
// State dies with the room: ChatArea is mounted under `key={peerId}`.

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { api } from "../api";
import type { ChatMessage } from "../api/adapter";
import { createDeltaSink } from "../lib/deltaSink";
import { OWNER_ID } from "../lib/ownerUnread";
import { serverMessageOf } from "../api/errors";
import { useI18n } from "../i18n";
import { ConfirmModal } from "../components/ConfirmModal";

export type ChatRecall = {
  /** `m.recalledTs`, or the recall this room just made of `m`; 0 = not recalled. */
  recalledTsOf: (m: ChatMessage) => number;
  /** Open the confirm dialog for message `id`. */
  ask: (id: string) => void;
  /** The confirm dialog, or null. Render it from the caller's tree. */
  dialog: ReactNode;
};

export function useChatRecall(loaded: readonly ChatMessage[]): ChatRecall {
  const { t } = useI18n();
  const [recalled, setRecalled] = useState<ReadonlyMap<string, number>>(
    () => new Map(),
  );
  const remember = useCallback((id: string, ts: number) => {
    setRecalled((prev) => (prev.has(id) ? prev : new Map(prev).set(id, ts)));
  }, []);
  const loadedRef = useRef(loaded);
  loadedRef.current = loaded;
  const recalledRef = useRef(recalled);
  recalledRef.current = recalled;

  useEffect(
    () =>
      api.subscribeEvents(
        createDeltaSink((batch) => {
          for (const d of batch.deltas) {
            const id = d.names.id;
            if (d.topic !== "chat" || d.names.from !== OWNER_ID || !id) continue;
            const held = loadedRef.current.find((m) => m.id === id);
            if (!held || held.recalledTs || recalledRef.current.has(id)) continue;
            void readBack(id);
          }
        }),
      ),
    [],
  );

  async function readBack(id: string) {
    try {
      const m = await api.getChatMessage(id);
      if ((m.recalledTs ?? 0) > 0) remember(m.id, m.recalledTs as number);
    } catch {
      // The row stays as it was; the next newest-page load or a reload corrects it.
    }
  }
  const [target, setTarget] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Same-tick double clicks both read the pre-update `busy`.
  const busyRef = useRef(false);

  const recalledTsOf = useCallback(
    (m: ChatMessage) => m.recalledTs || recalled.get(m.id) || 0,
    [recalled],
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
      const m = await api.recallChatMessage(id);
      remember(m.id, m.recalledTs || Date.now() / 1000);
      setTarget(null);
    } catch (e) {
      setError(serverMessageOf(e) || t.chat.recallFailed);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }

  const dialog =
    target === null ? null : (
      <ConfirmModal
        testId="chat-recall-confirm"
        confirmTestId="chat-recall-confirm-btn"
        body={t.chat.recallConfirmBody}
        error={error}
        cancelLabel={t.common.cancel}
        confirmLabel={t.chat.recallConfirm}
        busy={busy}
        danger
        onCancel={() => setTarget(null)}
        onConfirm={() => void confirm(target)}
      />
    );

  return { recalledTsOf, ask, dialog };
}
