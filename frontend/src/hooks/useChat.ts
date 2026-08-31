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

// 🔴 THE HOLE IN THE MIDDLE (T-b0bb). Everything from here down to
// `mergeLatestPage` exists for ONE defect, and it is worth stating exactly,
// because its correct and its broken output are the same shape and it never
// throws.
//
// `load()` / `refetch()` ask for `GET /api/chat?with=` with NO cursor and NO
// limit. The server answers with the most recent `chatListDefaultLimit` (30)
// messages of the stream — a SLIDING WINDOW, not a continuation. The old
// `mergeLatestPage` reconciled that page into the thread by SET SUBTRACTION on
// ids and then concatenated (`[...older, ...latest]`). It never asked the one
// question that matters: DOES THE PAGE'S OLDEST ROW JOIN ONTO THE NEWEST ROW WE
// ALREADY HOLD?
//
// It usually does, so the defect hid. It stops doing so the moment MORE than
// one page accumulates between two successful loads — a lost load, a
// backgrounded tab, a burst. Measured on the pre-fix code, to single-message
// precision: 30 new messages → no hole; 31 → 1 lost; 40 → 10 lost. The hole is
// exactly (new messages − 30).
//
// And it was PERMANENT and SILENT:
//   · every later refetch is the same newest window, which has slid FURTHER
//     forward — it can never reach back over the hole;
//   · `loadOlder()` takes its cursor from `messages[0]`, which sits ABOVE the
//     hole, so paging backwards walks past it into older history;
//   · nothing rendered marks the discontinuity, messages carry no sequence
//     number, and — worst — the server pushes the reader's watermark to the
//     newest ts OF THAT PAGE, i.e. PAST the hole. The lost messages are
//     therefore counted as READ: unread goes to 0 and the "以下是未讀" divider
//     does not point at them. "Lost" and "read" are indistinguishable.
//
// THE FIX, and why it has this shape. There is NO forward cursor on the server
// (`HandleListChatApiChatGetParams` is With / Limit / BeforeTs / BeforeId /
// Peek / CallerOnly / Ids — verified against api_chat.go, not against a doc),
// and adding one is out of scope. So we detect the seam and page BACKWARDS into
// it with the cursor that does exist:
//   1. after a newest page lands, compare its OLDEST row against our NEWEST row
//      in the stream's total (ts, id) order;
//   2. if the page's oldest is strictly NEWER than ours, the range between them
//      is uncovered — possibly empty, but we cannot know without asking;
//   3. ask, with `before_ts`+`before_id` taken from the page's oldest row,
//      walking back until a row we already hold appears (the join), or the
//      stream runs out;
//   4. give up after a bounded number of pages — AND SAY SO. See `gapSuspected`.
//
// Why the backfill is safe with respect to the read state: a cursor page is
// served by a branch that returns BEFORE the watermark write (verified by test
// against the real handler: watermark 40 → 40 across a before-cursor page), so
// backfilling cannot advance the watermark over anything.

// One scrollback page — mirrors the server's default recent window. A page
// returning fewer than this means the history is exhausted (hasMore=false).
const CHAT_PAGE_SIZE = 30;

// How many backfill pages one seam may consume before we stop and admit it.
// The hole has NO upper bound (it is "burst size − 30"), so a bound is
// unavoidable; what is NOT acceptable is a bound that gives up quietly. At
// CHAT_BACKFILL_PAGE_SIZE rows a page this covers a burst of ~600 messages
// between two loads, which is far past any measured case, while capping a
// pathological thread at 6 extra requests.
const MAX_BACKFILL_PAGES = 6;
// The backfill asks for a BIGGER page than the default window on purpose: the
// `limit` query parameter is public on the read endpoint and the client had
// never used it. Each round-trip therefore closes up to 100 rows of seam
// instead of 30. It does NOT replace the join check — the hole is unbounded, so
// no single limit can be "big enough" — it only makes the walk cheaper.
const CHAT_BACKFILL_PAGE_SIZE = 100;

// The stream's total order, as the server sorts it: ts, then id as tiebreak.
// Both cursor halves are required together server-side (422 otherwise) for
// exactly this reason — ts alone does not totally order the stream.
function cmpStreamOrder(a: ChatMessage, b: ChatMessage): number {
  if (a.ts !== b.ts) return a.ts < b.ts ? -1 : 1;
  if (a.id === b.id) return 0;
  return a.id < b.id ? -1 : 1;
}

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
  send: (
    body: string,
    attachments?: ChatAttachmentInput[],
    /** T-4e95: the id of the message this send REPLIES TO, when it is a reply.
     * Omitted on an ordinary send. */
    replyTo?: string,
  ) => Promise<void>;
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
  // 🔴 T-b0bb: a newest page did not join onto the loaded thread and the
  // backfill could not close the seam — messages are missing from the MIDDLE
  // of `messages`, count and identity unknown. The view MUST surface this:
  // the whole cost of this defect was that a thread with a hole in it is
  // indistinguishable from a complete one, and the server has already marked
  // the missing rows READ, so unread counts will not betray it either.
  //
  // In particular the "已到最早訊息" marker must not be shown on its own while
  // this is true. That marker answers "is anything missing ABOVE the loaded
  // window", and rendering it beside a known hole turns a narrow truth into a
  // claim of completeness that is false.
  gapSuspected: boolean;
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
  // 🔴 A seam this thread could NOT close (T-b0bb): a newest page did not join
  // onto what we held, and the backfill walk hit MAX_BACKFILL_PAGES (or its own
  // request failed) before reaching a row we already had. Messages are missing
  // from the MIDDLE of `messages` and we do not know which or how many.
  //
  // This exists so that giving up is not silent. It is deliberately STICKY for
  // the life of the conversation view: a later page that joins cleanly does not
  // retroactively deliver the rows we lost, so it must not clear the warning.
  // It resets on a peer switch / remount, and that reset is CORRECT rather than
  // a loss: the effect's setup body clears `messages` first, so the rebuilt
  // thread is a fresh newest window with the hole ABOVE it, not inside it —
  // `loadOlder`'s cursor (messages[0]) then walks back THROUGH the range that
  // was skipped and the rows come back. Scrolling up after a reload / peer
  // switch recovers the messages; the notice going away is not a silent loss of
  // them. (Do not restate the older claim that a reload makes the notice vanish
  // "without the messages having been recovered" — that was measured wrong.)
  //
  // 🟠 NAMED DEBT — THE SERVER-SIDE READ WATERMARK IS NOT FIXED (T-b0bb).
  // What a reload does NOT undo is the read state. While the hole existed the
  // server had already advanced the owner's watermark past it (a no-cursor
  // `listChat` marks up to the newest ts of the page it served), so those rows
  // stay counted as READ: unread does not go back up, and the "以下是未讀"
  // divider will not point at them even after they are scrolled back into view.
  // Nothing on the client can repair that — it needs either a watermark that
  // only advances over rows actually delivered, or a way to rewind it. See
  // server/ocserverd/api_chat_gap_tb0bb_test.go
  // `TestChatWatermarkAdvancesPastMessagesTheCallerNeverReceived`, which is
  // labelled CHARACTERIZATION for exactly this reason: it pins the behaviour we
  // are shipping WITH, not one we fixed.
  //
  // 🟠 NAMED DEBT — ONE ABANDON PATH IS STILL SILENT (T-b0bb, review S1).
  // `backfillSeam` has three ways to stop short. Two of them raise this flag
  // (budget exhausted; a cursor request that failed). The third — a cursor page
  // that comes back EMPTY — returns `joined: true` and stays quiet, on the
  // reasoning that nothing older than the cursor can exist if the rows we
  // already hold are older still. That reasoning is only sound while the server
  // is self-consistent, and the ways it is not (retention trimming between the
  // two requests, a `with`-filter difference, a DAL id tiebreak that disagrees
  // with `cmpStreamOrder`'s JS string compare) are precisely what would send us
  // down this branch. No realistic server state that produces it has been
  // constructed, so it is left as debt rather than fixed here — but it is the
  // one place in this file where giving up is still silent.
  gapSuspected: boolean;
}

/** Does a newest-window `latest` page JOIN onto what we already hold, or is
 * there an uncovered range between them?
 *
 * Joins when: we hold nothing yet; the page is empty; the page repeats a row we
 * already have (overlap proves contiguity); or the page's OLDEST row is not
 * newer than our NEWEST row (its window reaches back far enough to cover us).
 *
 * Returns false only when the page's oldest is strictly newer than our newest —
 * the range strictly between them is covered by NOBODY. That range may in truth
 * be empty; this function cannot tell, and does not guess. Its caller asks the
 * server. */
function pageJoinsThread(have: ChatMessage[], latest: ChatMessage[]): boolean {
  if (have.length === 0 || latest.length === 0) return true;
  const haveIds = new Set(have.map((m) => m.id));
  if (latest.some((m) => haveIds.has(m.id))) return true;
  const ourNewest = have.reduce((a, b) => (cmpStreamOrder(a, b) >= 0 ? a : b));
  const pageOldest = latest.reduce((a, b) => (cmpStreamOrder(a, b) <= 0 ? a : b));
  return cmpStreamOrder(pageOldest, ourNewest) <= 0;
}

/** Walk BACKWARDS from a newest page's oldest row until the seam is closed.
 *
 * The only cursor the server offers pages toward the past (`before_ts` +
 * `before_id`, required together), so closing a forward seam means paging
 * backwards into it from above. Stops at the first page that overlaps what we
 * hold, at the start of the stream, or at MAX_BACKFILL_PAGES.
 *
 * NEVER REJECTS. A failed backfill request returns `joined: false` with
 * whatever it managed to collect, so the caller can still adopt the newest page
 * AND raise the gap flag. Letting it throw would have thrown the newest page
 * away too, trading a marked hole for a stale thread. */
async function backfillSeam(
  withId: string,
  have: ChatMessage[],
  latest: ChatMessage[],
): Promise<{ filled: ChatMessage[]; joined: boolean }> {
  const haveIds = new Set(have.map((m) => m.id));
  const ourNewest = have.reduce((a, b) => (cmpStreamOrder(a, b) >= 0 ? a : b));
  let cursor = latest.reduce((a, b) => (cmpStreamOrder(a, b) <= 0 ? a : b));
  const filled: ChatMessage[] = [];
  try {
    for (let i = 0; i < MAX_BACKFILL_PAGES; i++) {
      const page = await api.listChat(withId, CHAT_BACKFILL_PAGE_SIZE, {
        beforeTs: cursor.ts,
        beforeId: cursor.id,
      });
      // Nothing older than the cursor exists ⇒ the seam is empty, not lost.
      // 🟠 THE ONE SILENT ABANDON PATH (named debt — see Thread.gapSuspected).
      // This is the only one of the three stops that does NOT raise the flag,
      // and it is the one that trusts the server's answer. If the server is not
      // self-consistent with what we hold, rows go missing here with no notice.
      if (page.length === 0) return { filled, joined: true };
      filled.unshift(...page);
      // A row we already hold ⇒ the two ranges now touch. Done.
      if (page.some((m) => haveIds.has(m.id))) return { filled, joined: true };
      const pageOldest = page.reduce((a, b) => (cmpStreamOrder(a, b) <= 0 ? a : b));
      // …or this page reached back past our newest row, which is the same
      // thing when ids happen not to repeat.
      if (cmpStreamOrder(pageOldest, ourNewest) <= 0) {
        return { filled, joined: true };
      }
      cursor = pageOldest;
    }
  } catch (e) {
    console.warn("useChat: backfill failed, gap left marked", e);
    return { filled, joined: false };
  }
  console.warn(
    `useChat: backfill gave up after ${MAX_BACKFILL_PAGES} pages; ` +
      "the thread may be missing messages in the middle",
  );
  return { filled, joined: false };
}

// Reconcile a refetched NEWEST page into the existing thread: messages the
// page does not carry (the loaded history above the newest window) stay in
// front, the page's own rows land after them — the page is authoritative for
// what it covers (e.g. a reply_card_status that flipped). hasMore is
// (re)derived from the page ONLY while the thread is still just the newest
// window (nothing prepended yet); once history is loaded, loadOlder owns it.
//
// `backfill` (T-b0bb) is whatever backfillSeam recovered from between the
// thread's newest row and the page's oldest one. It slots BETWEEN them, which
// is the only place it can belong, and is de-duplicated against both sides.
// Passing none keeps the pre-fix behaviour byte for byte.
function mergeLatestPage(
  prev: Thread,
  latest: ChatMessage[],
  backfill: ChatMessage[] = [],
  gapSuspected?: boolean,
): Thread {
  const gap = gapSuspected ?? prev.gapSuspected;
  if (prev.messages.length === 0) {
    return {
      peer: prev.peer,
      messages: latest,
      hasMore: latest.length >= CHAT_PAGE_SIZE,
      gapSuspected: gap,
    };
  }
  const pageIds = new Set(latest.map((m) => m.id));
  const fill = backfill.filter((m) => !pageIds.has(m.id));
  const fillIds = new Set(fill.map((m) => m.id));
  const older = prev.messages.filter(
    (m) => !pageIds.has(m.id) && !fillIds.has(m.id),
  );
  return {
    peer: prev.peer,
    messages: [...older, ...fill, ...latest],
    hasMore: prev.hasMore,
    gapSuspected: gap,
  };
}

export function useChat(withId: string): UseChat {
  const [thread, setThread] = useState<Thread>(() => ({
    peer: withId,
    messages: [],
    hasMore: true,
    gapSuspected: false,
  }));
  const [peerLastReadTs, setPeerLastReadTs] = useState(0);
  // Live mirror of `thread` for the async loadOlder (a state read inside an
  // await would be a stale closure) + the in-flight lock: one older page at a
  // time, so a scroll handler firing repeatedly near the top can't stack
  // duplicate cursor requests.
  const threadRef = useRef(thread);
  threadRef.current = thread;
  const loadingOlderRef = useRef(false);
  // 🔴 LOAD GENERATIONS (T-b0bb, review B2). Before the backfill, a load was
  // "fetch → setThread" with ZERO awaits in between, so two overlapping loads
  // could only interleave if the network answered out of order. The backfill
  // put up to MAX_BACKFILL_PAGES round-trips between the fetch resolving and the
  // commit — and it opens that window exactly during a burst, i.e. exactly when
  // the peer is still typing and another load is most likely to start and finish
  // inside it. Measured on the unguarded code: load A stalls in its backfill, a
  // newer load B completes, then A commits on top — 75 rows, none missing, none
  // duplicated, and the newest 5 sitting at the TOP of the conversation, plus the
  // same seam backfilled twice because both loads compared against the same stale
  // threadRef. `alive` does not cover this: it only says "the effect was torn
  // down", never "a newer load already landed".
  //
  // So every load takes a ticket when it STARTS and may only commit while no
  // later ticket has committed. A superseded load is dropped whole, which is
  // safe because the later load fetched a newer window and backfilled from it
  // down to the same held rows — its result is a superset of the one we drop.
  // `ChatArea.groupMessages` "only partitions, never reorders", so array order
  // IS screen order: a late commit is not a cosmetic race.
  const loadSeqRef = useRef(0);
  const committedSeqRef = useRef(0);
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
    // Takes a generation ticket like load() does — see loadSeqRef.
    const seq = ++loadSeqRef.current;
    const next = await api.listChat(withId);
    // T-b0bb: close the seam BEFORE merging (see backfillSeam). threadRef is
    // the live mirror — reading `thread` here would be a stale closure.
    const cur = threadRef.current;
    let fill: ChatMessage[] = [];
    let gap: boolean | undefined;
    let superseded = seq < committedSeqRef.current;
    if (
      !superseded &&
      cur.peer === withId &&
      !pageJoinsThread(cur.messages, next)
    ) {
      const r = await backfillSeam(withId, cur.messages, next);
      // A newer load committed while we were paging backwards ⇒ this page and
      // its backfill are stale. Dropping them keeps the thread in order.
      superseded = seq < committedSeqRef.current;
      fill = r.filled;
      if (!r.joined) gap = true;
    }
    // The peer's watermark is still worth pulling even when our own page is
    // stale, so the drop is a skipped COMMIT, not an early return.
    if (superseded) {
      await refetchReads();
      return;
    }
    committedSeqRef.current = seq;
    setThread((prev) => {
      // A peer switch mid-flight: this page belongs to the peer the owner has
      // already left — DROP it (same guard loadOlder's setThread already has).
      // The previous else-arm wrote `{ peer: withId, messages: next }`, i.e. it
      // replaced the current conversation's thread AND re-registered the OLD
      // peer as the thread's owner, so the window kept rendering the old
      // conversation until some later event for the current peer overwrote it.
      if (prev.peer !== withId) return prev;
      return mergeLatestPage(prev, next, fill, gap);
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
    setThread({ peer: withId, messages: [], hasMore: true, gapSuspected: false });
    setPeerLastReadTs(0);

    // ONE load path (initial + SSE + refocus). Only a load fired while the
    // owner is actually looking may take the side-effectful "list 即讀" route;
    // a background window loads through the READ-ONLY peek so the thread stays
    // fresh WITHOUT consuming the unread state. Never swallow a rejection into
    // a phantom-empty thread — log it (a 401 is already handled at the http
    // layer, which bounces to login).
    const load = () => {
      // The generation ticket is taken at FIRE time, so a load that started
      // later always outranks one that started earlier, however long each of
      // them spends in the backfill. See loadSeqRef.
      const seq = ++loadSeqRef.current;
      const fetching = isWindowActive()
        ? api.listChat(withId)
        : api.peekChat(withId);
      fetching
        .then(async (next) => {
          if (!alive) return;
          // Landed ⇒ whatever we owed is paid off.
          loadStaleRef.current = false;
          // A newer load already committed while this one was in flight.
          if (seq < committedSeqRef.current) return;
          // 🔴 T-b0bb: this page is the newest WINDOW, not a continuation. If
          // its oldest row does not join onto our newest one, page backwards
          // into the seam before merging — otherwise the uncovered range is
          // lost permanently and silently, and the server has already counted
          // it as read. backfillSeam never rejects, so a failed backfill costs
          // a marked gap, not the page we just fetched.
          const cur = threadRef.current;
          let fill: ChatMessage[] = [];
          let gap: boolean | undefined;
          if (cur.peer === withId && !pageJoinsThread(cur.messages, next)) {
            const r = await backfillSeam(withId, cur.messages, next);
            if (!alive) return;
            // 🔴 THE ORDERING GUARD (review B2). The backfill above is up to 6
            // round-trips long; a load that started AFTER this one can have
            // fetched, backfilled and committed inside that window. Committing
            // now would splice an older newest-page on top of a newer thread —
            // nothing lost, nothing duplicated, and the newest messages moved
            // to the top of the screen. Drop instead.
            if (seq < committedSeqRef.current) return;
            fill = r.filled;
            if (!r.joined) gap = true;
          }
          committedSeqRef.current = seq;
          // MERGE the newest page into whatever is already loaded for this
          // peer (see mergeLatestPage) — replacing would eat the scrollback.
          setThread((prev) =>
            prev.peer === withId
              ? mergeLatestPage(prev, next, fill, gap)
              : {
                  peer: withId,
                  messages: next,
                  hasMore: next.length >= CHAT_PAGE_SIZE,
                  gapSuspected: false,
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
    async (
      body: string,
      attachments?: ChatAttachmentInput[],
      replyTo?: string,
    ) => {
      const trimmed = body.trim();
      // Allow sending when EITHER text or attachments are present; reject only a
      // truly empty message (no text AND no attachments) — mirrors the server's 400.
      if (!trimmed && !(attachments && attachments.length > 0)) return;
      await api.postChat({ to: withId, body: trimmed, attachments, replyTo });
      // Reconcile by refetch so the sent message appears immediately.
      //
      // 🔴 ONCE THE POST HAS RETURNED, THIS SEND HAS SUCCEEDED. The refresh
      // that follows is a different promise about a different thing, and it
      // must not be allowed to reject this one: `refetch` calls `api.listChat`
      // unguarded (unlike its own `refetchReads`, which already swallows), so a
      // blip on the refresh used to surface to the caller as "the send failed".
      // The composer believes that: it restores the message the owner just
      // successfully sent — and since T-4e95 it restores it into the room's
      // DRAFT, which outlives the page. The owner comes back to a composer
      // holding a line that is already in the thread, with the reply banner
      // still up, and Enter sends it a second time.
      // A failed refresh costs a stale window until the next SSE delta; a
      // rejected send costs a duplicate message. Log it and let the send stand.
      try {
        await refetch();
      } catch (e) {
        console.warn("useChat: post-send refetch failed (message was sent)", e);
      }
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
          gapSuspected: prev.gapSuspected,
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
    gapSuspected: thread.gapSuspected,
  };
}
