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
//
// 🔴 AND BECAUSE IT IS A WRITE, IT COMES BACK AS AN EVENT (T-8115): the server
// fans a `chat_read` delta for the watermark this client just advanced. Loading
// this thread for a delta about a DIFFERENT conversation therefore manufactures a
// second fan-out round out of nothing, once per chat line anywhere in the company.
// Both SSE branches below are gated on the delta's own participants for that
// reason — see frontend/CLAUDE.md 「一則通知 = 一次『只抓它碰到的那一項』」.

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
import { createDeltaSink } from "../lib/deltaSink";
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
  // 🔴 "THE LAST LOAD NEVER LANDED" (T-929f). `load()` below used to end a
  // rejection at `console.warn` and nothing else. Two failure worlds, and only
  // one of them was already covered:
  //  - the CONNECTION really drops ⇒ `es.onopen` fires again ⇒ api/http.ts
  //    `resyncAll()` ⇒ the screen catches up within ~2s. That half works and is
  //    untouched here.
  //  - the connection is still open in the client's eyes (esOpen=1, esError=0)
  //    and just THAT ONE load fails ⇒ the delta was received, the load was lost,
  //    and NOTHING ever tried again. Measured: 90s idle with the thread frozen;
  //    a synthesised `focus` filled it in 0.0s — i.e. the data was one request
  //    away the whole time and no code path was going to make it.
  //
  // The fix is the smallest one that closes it: MARK, DON'T RETRY. No timer, no
  // backoff, no retry loop, and emphatically not "re-pull the whole history on
  // every reconnect" — what gets re-fetched is the same newest page `load()`
  // would have fetched anyway. The flag only widens WHEN we are allowed to
  // fetch: the next relevant burst gets through even when the per-conversation
  // filter (`touchesThisThread`) would have skipped it, because that filter
  // reasons about "is this delta about us", which cannot tell us anything about
  // a page we already know we are missing.
  //
  // ⚠️ THE GAP THIS LEAVES OPEN, VERBATIM AND ON PURPOSE:
  // 「下一個事件來就補」意味著:如果那條線之後再也沒有任何事件,就還是不會補。
  // If no further chat/chat_read delta ever arrives on this connection, the
  // thread stays behind until a focus/visibilitychange, a peer switch, or a
  // reconnect happens to re-run `load()`. That residual is a KNOWN, ACCEPTED
  // trade made by the owner in exchange for a smaller change (2026-08-20). This
  // is not "the stale-thread bug is fixed"; it is "the thread now self-heals on
  // the next event instead of never".
  //
  // ⚠️ Scope, honestly: only `load()` sets this. `refetchReads()` /
  // `loadOlder()` / `markRead()` still swallow their rejections the old way, so
  // a lost read-receipt pull is still silent (tracked separately, not widened
  // here).
  //
  // ⚠️ StrictMode: written in the effect's SETUP body, never only in cleanup —
  // a cleanup-only ref write gets stuck off forever under setup→cleanup→setup.
  const loadStaleRef = useRef(false);

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
    setThread((prev) => {
      // A peer switch mid-flight: this page belongs to the peer the owner has
      // already left — DROP it (same guard loadOlder's setThread already has).
      // The previous else-arm wrote `{ peer: withId, messages: next }`, i.e. it
      // replaced the current conversation's thread AND re-registered the OLD
      // peer as the thread's owner, so the window kept rendering the old
      // conversation until some later event for the current peer overwrote it.
      if (prev.peer !== withId) return prev;
      return mergeLatestPage(prev, next);
    });
    // listChat itself marks the owner's read watermark server-side; pull the
    // peer's watermark alongside so the badges reconcile.
    await refetchReads();
  }, [withId, refetchReads]);

  useEffect(() => {
    let alive = true;
    // Setup-body write (see loadStaleRef's StrictMode note): a fresh
    // subscription — including a peer switch — starts by loading
    // unconditionally, so it owes nothing yet, and any debt the PREVIOUS peer
    // left behind is not this conversation's to pay.
    loadStaleRef.current = false;

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
          // Landed ⇒ whatever we owed is paid off.
          loadStaleRef.current = false;
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
        .catch((e) => {
          // Same guard as the .then arm, and for a sharper reason: this ref
          // OUTLIVES the effect instance. A load belonging to a torn-down
          // instance can reject AFTER the next setup body has already cleared
          // the mark, and without this line it would write its debt onto its
          // SUCCESSOR — making the setup-body comment above ("any debt the
          // PREVIOUS peer left behind is not this conversation's to pay") false.
          if (!alive) return;
          // Do NOT retry here (T-929f). Record the debt only; the SSE sink
          // below pays it on the next relevant burst.
          loadStaleRef.current = true;
          console.warn("useChat: load failed", e);
        });
    };

    load();
    void refetchReads();

    // SSE: reconcile the thread by refetching on the relevant topics — but only
    // when the delta is about THIS conversation.
    //
    // 🔴 THE SELF-DRIVE (T-8115). `load()` takes the marking `GET /api/chat?with=`
    // whenever the owner is looking, and that read is a DURABLE WRITE: the server
    // advances the watermark and fans a `chat_read` delta straight back at this
    // client, which re-runs the roster / office-total / worker fan-out. So a
    // `load()` fired for a delta about a DIFFERENT conversation does not merely
    // waste a request — it manufactures a second event round out of nothing, and
    // one arrives for every chat line anywhere in the company. Deltas name their
    // participants (spec §2.2 payloads), so the gate is exact rather than
    // heuristic; a delta that names nobody (a resync, or a transport that carries
    // no delta) still loads, because then it really might be about us.
    const touchesThisThread = (names: { from?: string; to?: string }) =>
      names.from === undefined && names.to === undefined
        ? true
        : names.from === withId || names.to === withId;

    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        if (![...batch.topics].some((t) => CHAT_TOPICS.has(t))) return;
        const chats = batch.deltas.filter((d) => d.topic === "chat");
        const reads = batch.deltas.filter((d) => d.topic === "chat_read");
        // A resync (unnamed: no delta, or a delta naming nobody) reloads
        // unconditionally — see above.
        //
        // 🔴 T-929f: `touchesThisThread` answers "is this delta about us", and
        // that is the right question ONLY while the thread we are holding is
        // the truth. Once a load has failed we are knowingly holding a stale
        // page, and no amount of reasoning about a DIFFERENT conversation's
        // participants can fill it in — so a relevant burst (any chat /
        // chat_read topic, already established above) forces the load through
        // exactly when we owe one. It re-fetches the SAME newest page the lost
        // load wanted; it is not a history re-pull.
        const ourChat =
          loadStaleRef.current ||
          (batch.topics.has("chat") &&
            (chats.length === 0 ||
              chats.some((d) => touchesThisThread(d.names))));
        if (ourChat) load();
        // `peerLastReadTs` is the PEER's watermark and nothing else, so only a
        // read whose READER is the peer can move it. Our own read echo names US
        // as the reader — re-pulling the receipts for it is the second half of
        // the same self-drive, and it can never change the value. A new message
        // in THIS thread still re-pulls them (it may carry a fresh peer read).
        const peerRead =
          batch.topics.has("chat_read") &&
          (reads.length === 0 ||
            reads.some(
              (d) => d.names.reader === undefined || d.names.reader === withId
            ));
        if (ourChat || peerRead) void refetchReads();
      })
    );

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
