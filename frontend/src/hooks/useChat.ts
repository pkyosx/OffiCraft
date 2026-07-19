// hooks/useChat.ts — load a member's chat thread through the api client + keep
// it fresh. Mirrors useMembers: reconcile-by-refetch (contract B) on the "chat"
// SSE topic (never merge an event payload). In M1 the mock's subscribeEvents is
// a no-op, so refresh is driven by the send() callback's refetch — but the
// wiring is identical for the real backend, where an agent's async reply arrives
// as a "chat" topic and we refetch to pick it up.
//
// READ RECEIPTS: alongside the messages we track the peer's per-conversation
// last-read watermark (readReceipts: peer → last_read_ts). The peer's watermark
// advancing arrives as a "chat_read" topic → we refetch the receipts. The owner
// side calls markRead() when it enters / scrolls to the bottom of the thread.
//
// READING REQUIRES LOOKING (badge-flash fix): `listChat(?with=)` is "list 即讀"
// — the server advances the owner's read watermark as a side effect. That side
// effect is legitimate ONLY while the owner can actually see the thread. When
// the window is backgrounded / the tab hidden (isWindowActive() false), every
// load here goes through the READ-ONLY `peekChat` instead — the thread keeps
// updating (new messages still render on return) but the unread badge keeps
// counting. Coming back to the foreground re-runs the marking listChat, so the
// badge clears exactly when the owner really looks.

// SCROLLBACK (T-bf82): the thread starts as the newest page (server default
// 30) and grows BACKWARDS through loadOlder() — a keyset-cursor page
// (`before_ts`+`before_id` = the current oldest message's (ts, id)) prepended
// in front. History pages are READ-ONLY server-side (never advance the read
// watermark). Because the thread may now hold MORE than the newest page,
// SSE/refetch reconciliation MERGES the refetched newest page into the
// existing thread by id (older messages kept in front) instead of replacing
// the whole array — a replace would silently eat the loaded history.

import { useCallback, useEffect, useRef, useState } from "react";
import type { ChatMessage, ChatAttachmentInput } from "../api/adapter";
import { api } from "../api";
import { isWindowActive } from "./useWindowActive";

// One scrollback page — mirrors the server's default recent window. A page
// returning fewer than this means the history is exhausted (hasMore=false).
const CHAT_PAGE_SIZE = 30;

interface UseChat {
  messages: ChatMessage[];
  // The peer id `messages` actually belongs to. On a conversation switch the
  // hook clears the thread in an effect — ONE COMMIT AFTER the caller already
  // renders the new peer — so for that commit `messages` is still the previous
  // peer's thread. Consumers whose logic anchors on the thread (ChatArea's
  // entry positioning) MUST gate on `messagesPeer === <current peer>` instead
  // of trusting `messages` blindly.
  messagesPeer: string;
  // The peer's last-read watermark for THIS conversation (epoch seconds), or 0
  // when the peer has not read anything yet. Drives the per-message "read ✓" badge.
  peerLastReadTs: number;
  // Send text and/or a LIST of staged attachments (files + images mixed) — all
  // riding the SAME message.
  send: (body: string, attachments?: ChatAttachmentInput[]) => Promise<void>;
  // Mark this conversation read up to `lastReadTs` (the owner's own watermark) —
  // called when the owner enters / scrolls to the bottom of the thread.
  markRead: (lastReadTs: number) => Promise<void>;
  // Whether older history MAY still exist above the loaded window (T-bf82).
  // Starts true; flips false once a page (initial or older) comes back
  // shorter than the page size. Drives the "已到最早訊息" marker and stops
  // further top-of-scroll loads.
  hasMore: boolean;
  // Load ONE page of older history and PREPEND it (keyset cursor = the
  // current oldest message's (ts, id)). Concurrency-locked (a second call
  // while one is in flight is a no-op) and read-only server-side (a history
  // page never advances the read watermark). No-op on an empty thread or
  // when hasMore is already false.
  loadOlder: () => Promise<void>;
}

// Topics that mutate the chat thread → trigger a refetch. "chat_read" advances a
// participant's last-read watermark (the peer read our messages).
const CHAT_TOPICS = new Set(["chat", "chat_read"]);

// The thread and the peer it belongs to, updated TOGETHER (one state) so a
// consumer can never observe new-peer identity with old-peer messages.
// hasMore rides along for the same reason — it describes THIS peer's loaded
// window, never the previous one's.
interface Thread {
  peer: string;
  messages: ChatMessage[];
  hasMore: boolean;
}

// Reconcile a refetched NEWEST page into the existing thread: messages the
// page does not carry (the loaded history above the newest window) stay in
// front, the page's own rows land after them — the page is authoritative for
// what it covers (e.g. a reply_card_status that flipped). hasMore is
// (re)derived from the page ONLY while the thread is still just the newest
// window (nothing prepended yet); once history is loaded, loadOlder owns it.
function mergeLatestPage(prev: Thread, latest: ChatMessage[]): Thread {
  if (prev.messages.length === 0) {
    return {
      peer: prev.peer,
      messages: latest,
      hasMore: latest.length >= CHAT_PAGE_SIZE,
    };
  }
  const pageIds = new Set(latest.map((m) => m.id));
  const older = prev.messages.filter((m) => !pageIds.has(m.id));
  return { peer: prev.peer, messages: [...older, ...latest], hasMore: prev.hasMore };
}

export function useChat(withId: string): UseChat {
  const [thread, setThread] = useState<Thread>(() => ({
    peer: withId,
    messages: [],
    hasMore: true,
  }));
  const [peerLastReadTs, setPeerLastReadTs] = useState(0);
  // Live mirror of `thread` for the async loadOlder (a state read inside an
  // await would be a stale closure) + the in-flight lock: one older page at a
  // time, so a scroll handler firing repeatedly near the top can't stack
  // duplicate cursor requests.
  const threadRef = useRef(thread);
  threadRef.current = thread;
  const loadingOlderRef = useRef(false);

  // The PEER's watermark for this conversation: their receipt is the one whose
  // reader is the peer (readerId === withId). That is how far the peer has read
  // the owner's messages → the "read ✓" cutoff.
  const refetchReads = useCallback(async () => {
    try {
      const reads = await api.listChatReads(withId);
      const peerReceipt = reads.find((r) => r.readerId === withId);
      setPeerLastReadTs(peerReceipt ? peerReceipt.lastReadTs : 0);
    } catch (e) {
      console.warn("useChat: reads refetch failed", e);
    }
  }, [withId]);

  const refetch = useCallback(async () => {
    // Post-send refetch: sending is a user action in the foreground, so the
    // marking listChat is honest here (the owner is looking at what they sent).
    // MERGE the newest page (id-dedupe, history kept in front) — never
    // replace, or the loaded scrollback would vanish under the owner.
    const next = await api.listChat(withId);
    setThread((prev) =>
      prev.peer === withId
        ? mergeLatestPage(prev, next)
        : { peer: withId, messages: next, hasMore: next.length >= CHAT_PAGE_SIZE },
    );
    // listChat itself marks the owner's read watermark server-side; pull the
    // peer's watermark alongside so the badges reconcile.
    await refetchReads();
  }, [withId, refetchReads]);

  useEffect(() => {
    let alive = true;

    // Switching conversations: drop the PREVIOUS peer's thread/receipt state
    // immediately instead of letting it linger under the new peer's header
    // until the refetch lands. ChatArea's entry positioning (first-unread jump)
    // depends on this — it must anchor on the NEW peer's first loaded batch,
    // never on a stale thread. No-op on first mount (already empty).
    // hasMore resets optimistic-true; the first landed page derives it
    // honestly (mergeLatestPage's empty-thread arm).
    setThread({ peer: withId, messages: [], hasMore: true });
    setPeerLastReadTs(0);

    // ONE load path (initial + SSE + refocus). Only a load fired while the
    // owner is actually looking may take the side-effectful "list 即讀" route;
    // a background window loads through the READ-ONLY peek so the thread stays
    // fresh WITHOUT consuming the unread state. Never swallow a rejection into
    // a phantom-empty thread — log it (a 401 is already handled at the http
    // layer, which bounces to login).
    const load = () => {
      const fetching = isWindowActive()
        ? api.listChat(withId)
        : api.peekChat(withId);
      fetching
        .then((next) => {
          if (!alive) return;
          // MERGE the newest page into whatever is already loaded for this
          // peer (see mergeLatestPage) — replacing would eat the scrollback.
          setThread((prev) =>
            prev.peer === withId
              ? mergeLatestPage(prev, next)
              : {
                  peer: withId,
                  messages: next,
                  hasMore: next.length >= CHAT_PAGE_SIZE,
                },
          );
        })
        .catch((e) => console.warn("useChat: load failed", e));
    };

    load();
    void refetchReads();

    // SSE: reconcile the thread by refetching on the relevant topics. A "chat"
    // event refetches messages; a "chat_read" event refetches the receipts.
    const unsubscribe = api.subscribeEvents((topic) => {
      if (!CHAT_TOPICS.has(topic)) return;
      if (topic === "chat") load();
      // Both "chat" (a new message may carry a fresh peer read) and "chat_read"
      // re-pull the receipts.
      void refetchReads();
    });

    // Coming BACK to the foreground while this thread is open: the owner is now
    // actually looking → run the marking listChat so everything accumulated in
    // the background is read now (and the roster badge clears now, not before).
    const onMaybeActive = () => {
      if (isWindowActive()) load();
    };
    window.addEventListener("focus", onMaybeActive);
    document.addEventListener("visibilitychange", onMaybeActive);

    return () => {
      alive = false;
      unsubscribe();
      window.removeEventListener("focus", onMaybeActive);
      document.removeEventListener("visibilitychange", onMaybeActive);
    };
  }, [withId, refetchReads]);

  const send = useCallback(
    async (body: string, attachments?: ChatAttachmentInput[]) => {
      const trimmed = body.trim();
      // Allow sending when EITHER text or attachments are present; reject only a
      // truly empty message (no text AND no attachments) — mirrors the server's 400.
      if (!trimmed && !(attachments && attachments.length > 0)) return;
      await api.postChat({ to: withId, body: trimmed, attachments });
      // Reconcile by refetch so the sent message appears immediately.
      await refetch();
    },
    [withId, refetch],
  );

  const loadOlder = useCallback(async () => {
    const cur = threadRef.current;
    // Guards: the thread must really be THIS peer's (a switch is one commit
    // behind), non-empty (no cursor yet), still paged (hasMore), and no other
    // older-page fetch may be in flight (the concurrency lock).
    if (cur.peer !== withId || cur.messages.length === 0 || !cur.hasMore) return;
    if (loadingOlderRef.current) return;
    loadingOlderRef.current = true;
    try {
      const oldest = cur.messages[0];
      const page = await api.listChat(withId, CHAT_PAGE_SIZE, {
        beforeTs: oldest.ts,
        beforeId: oldest.id,
      });
      setThread((prev) => {
        // A peer switch mid-flight: the page belongs to the OLD peer — drop it.
        if (prev.peer !== withId) return prev;
        const have = new Set(prev.messages.map((m) => m.id));
        const older = page.filter((m) => !have.has(m.id));
        return {
          peer: prev.peer,
          messages: [...older, ...prev.messages],
          // A short page = the history is exhausted (keyset paging makes this
          // exact; an exactly-full last page just costs one empty follow-up).
          hasMore: page.length >= CHAT_PAGE_SIZE,
        };
      });
    } catch (e) {
      console.warn("useChat: loadOlder failed", e);
    } finally {
      loadingOlderRef.current = false;
    }
  }, [withId]);

  const markRead = useCallback(
    async (lastReadTs: number) => {
      if (lastReadTs <= 0) return;
      try {
        await api.markChatRead({ peer: withId, lastReadTs });
      } catch (e) {
        console.warn("useChat: markRead failed", e);
      }
    },
    [withId],
  );

  return {
    messages: thread.messages,
    messagesPeer: thread.peer,
    peerLastReadTs,
    send,
    markRead,
    hasMore: thread.hasMore,
    loadOlder,
  };
}
