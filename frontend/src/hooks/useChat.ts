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
// READING REQUIRES LOOKING (badge-flash fix): loading a thread never marks it
// read — `listChat` is read-only on every path since T-48. The badge clears
// only when ChatArea calls markRead(), which it does while the owner can
// actually see the thread, so a backgrounded window keeps counting even though
// it keeps loading.
//
// 🔴 EVERY LOAD USED TO BE A WRITE, AND WRITES COME BACK AS EVENTS (T-8115):
// the server fanned a `chat_read` delta for the watermark a load had advanced,
// so loading this thread for a delta about a DIFFERENT conversation
// manufactured a second fan-out round out of nothing, once per chat line
// anywhere in the company. markRead() is now the only thing that can start
// that round, but the SSE branches below stay gated on the delta's own
// participants — a load nobody asked for is still a wasted request. See
// frontend/CLAUDE.md 「一則通知 = 一次『只抓它碰到的那一項』」.

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
import { ApiError } from "../api/errors";
import { createDeltaSink } from "../lib/deltaSink";
import { OWNER_ID } from "../lib/ownerUnread";
import { isWindowActive } from "./useWindowActive";
import { openLatches } from "../lib/conversationLatches";
import type { LatchRelease } from "../lib/conversationLatches";
// 🔴 THE THREAD'S ONLY DOOR (T-48). The raw `useState<Thread>` setter lives in
// that module's closure and does not come out; this file gets `commit` /
// `mergeHistory` / `clear` and no fourth way in. That is what makes "commit
// messages to the view without awaiting their reply cards" unwritable rather
// than merely discouraged — the shape of the failure that shipped four times in
// one night, every time with a green suite. `check-async-landing-points.mjs`
// keeps the setter from growing a second home.
import { useThreadCommit } from "../lib/threadCommit";
import type { Thread } from "../lib/threadCommit";

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
// THE FIX, and why it has this shape. There is no forward cursor this hook can
// use (`HandleListChatApiChatGetParams` is With / Limit / BeforeTs / BeforeId /
// StartId / EndId / Cursor / Unread / Sender / Recipient / Ids — verified
// against api_chat.go, not against a doc; `Peek` and `CallerOnly` were both
// removed with T-48). `Unread` walks forwards, but over the caller's OWN unread
// set rather than the open thread, which is a different question from the one
// this seam asks; adopting it here is its own change. So we detect the seam and
// page BACKWARDS into it with the cursor this hook already sends:
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

// 🔴 ONE PAGE OF THE FETCH-TO-THE-LIVE-TAIL (T-48 fix12, owner rc-e1fb80065f8f:
// 「一次撈100則撈完」). The window path (`?start_id=` / `?end_id=`) has always
// taken a `limit`, capped at `chatWindowMaxLimit = 200` server-side
// (api_chat.go) — 100 is well inside it, so nothing on the server or in `spec/`
// moves for this.
//
// Why not the cap itself: the cap is a ROW count and explicitly NOT a payload
// bound — that constant's own note records 200 rows MEASURED at 687 KB. 100
// halves the worst single response for double the request count, and the
// request count is not what hurts: measured (fix11), the cost is dominated by
// ONE React render of the finished thread, which is blind to how the rows
// arrived.
const CHAT_WALK_PAGE_SIZE = 100;

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
  // The peer's last-read watermark in epoch seconds (0 = read nothing). Drives
  // the per-message 「已讀」 badge.
  //
  // 🔴 IT IS A BARE NUMBER AGAIN, AND WHAT MAKES THAT SAFE IS NOT IN THIS FILE
  // (T-48, R13-1). It was a `PeerLastRead` object — value plus whose it is —
  // because this hook was mounted ONCE and swapped between rooms, so it could
  // hand back the previous room's watermark for a commit (R8-2: B's reading lit
  // 已讀 ticks on A's outgoing rows). `ChatArea` is mounted under
  // `key={peerId}`, so this hook is mounted per room and the only watermark it
  // can hand back is the one it fetched for its own `withId`.
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
  // 🔴 THE ONE STATE BEHIND 「這條對話的內容還沒到」 (T-48 fix12, owner
  // c-de666642e77b「不管是進聊天室,或點選元訊息都是這樣」＋ c-d24ebd7f8d78「照
  // 理說應該只有改一個地方吧?就會都有作用?」— he is right, and this is that
  // one place).
  //
  // TRUE while EITHER of two things is true — 「這條對話的內容還沒到」 or
  // 「正在走訪」 — so it covers the TRIP, not just the room:
  //   · an ordinary entry, whose first content is `load()`'s newest page;
  //   · an anchor entry (跳到原訊息 / a kept link), whose first content is
  //     `loadAround`'s window — and since fix12 that window is not shown until
  //     everything from it to the live tail has been fetched;
  //   · 跳到原訊息 AGAIN in a room that is ALREADY OPEN — a second jump on the
  //     same member, the back button, a link the owner kept. `OfficePage` keys
  //     `ChatArea` by `peerId`, so only `route.msgId` changes and nothing
  //     remounts: the room-level 「載過了沒有」 answer is long since yes, and
  //     before fix14 this — the longest wait the feature has, a walk of many
  //     round trips — drew nothing at all (review25 F2).
  //
  // ⚠️ IT IS DELIBERATELY NOT PER-ENTRY. Every door writes the SAME flag, so a
  // third entry added later needs no new wiring to get the same treatment — and
  // a view that reads it needs no knowledge of which door was used. Writing it
  // per-door is the mistake this shape exists to prevent: two flags that have to
  // agree are two flags that can disagree, and the disagreement is invisible
  // (a spinner that never stops looks exactly like a slow network).
  //
  // It says nothing about ordinary LATER loads. A background refresh over a
  // thread that already has content is not a wait the reader is looking at; a
  // walk the reader asked for by name is.
  initialLoading: boolean;
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
  // 🔴 THE LOADED WINDOW IS NOT THE LIVE TAIL (T-48 ③). True while the thread
  // holds a window that does NOT reach the newest message — there is more
  // stream BELOW what is loaded. Everything that means "the newest message is
  // on screen" must consult this: the viewport can be scrolled to the very
  // bottom of a historical window and still be nowhere near the newest row.
  //
  // 🔴 SINCE fix12 THIS IS AN EXCEPTION STATE, NOT THE NORMAL OUTCOME OF A JUMP.
  // `loadAround` fetches through to the live tail before committing, so a
  // successful jump lands with this FALSE. It is true only when that fetch could
  // not finish — a page failed, or the server kept answering with rows we
  // already hold — in which case the thread is committed anyway (the reader gets
  // their message rather than a blank room) and this flag is what tells the
  // truth about it: the 回到最新 arrow stays up, `load()` stands down, and the
  // watermark is not stamped. It must NOT be deleted on the theory that it is
  // always false now.
  hasNewer: boolean;
  // 🔴 THERE IS NO FORWARD PAGER ON THIS INTERFACE ANY MORE (T-48 fix12, owner
  // rc-e1fb80065f8f「一次撈100則撈完」＋ c-6a973512ed77「我是指整個訊息撈完才
  // render」). A jump used to land in the middle of the history and leave the
  // reader to walk home one page per scroll gesture; `loadAround` now fetches
  // everything from the anchor to the live tail BEFORE it commits anything, so
  // 「往新的方向一頁一頁走」 is not a thing any caller can ask for — and the
  // entire gesture apparatus that served it (the 400ms retry clock, the
  // trailing replay for a swallowed gesture, the sight gate, the served-anchor
  // marker, the in-flight-blocked marker, the reader-at-bottom probe, the
  // no-progress marker) is gone with it, because none of its questions can be
  // asked any more.
  // Fetch the window AROUND one message id and make it the thread, so a jump
  // can land on a message that was never loaded. Two requests — the page
  // ending at the target (context above) and the page starting at it (context
  // below) — never the whole history.
  //
  // Resolves with WHICH of the three things happened — see JumpOutcome. The
  // caller must branch on it: the defect this replaces was a miss that drew the
  // same pixels as a hit, and collapsing "someone overtook us" into "missing"
  // is that same defect wearing the fix's clothes.
  loadAround: (msgId: string) => Promise<JumpOutcome>;
  // Leave an anchor window and go back to the LIVE newest window, REPLACING the
  // thread. The historical rows are dropped rather than spliced onto the tail:
  // the range between them is genuinely unloaded, and pretending otherwise is
  // the T-b0bb hole with a friendlier name.
  resetToLatest: () => Promise<void>;
}

/** The one rule for writing a watermark, so no caller carries it.
 *
 * 🔴 KEEP THE LARGER, AND THAT IS DELIBERATE (T-48, R11-7). A watermark is a
 * claim about something that ALREADY HAPPENED — the peer read up to here — and
 * reading cannot be undone. The caller passes 0 whenever `listChatReads` comes
 * back without a receipt row, and before this rule that zero turned the 已讀
 * ticks off. It should not: "no row this time" is never evidence against a row
 * we have already seen. Its realistic causes are a partial 200 and a hard
 * receipt delete (`DeleteChatReadsInvolving`, which takes the messages with
 * it), and in neither case is "un-tick what the owner already saw" honest.
 *
 * The cost is bounded on purpose: this state is per-room and this hook is
 * mounted per room, so the watermark is rebuilt from the server on the next
 * entry. Monotonic within a visit, never across one.
 *
 * ⚠️ IT USED TO ANSWER A SECOND QUESTION — "is this even my room's watermark?"
 * — because the hook outlived a room switch. That half is gone with the switch
 * (R13-1); leaving it in would have been a comparison that can no longer fail.
 */
function mergePeerRead(prev: number, next: number): number {
  return next > prev ? next : prev;
}

// What `loadAround` actually did, and the reason it is not a boolean (T-48, F3).
//
//   • "found"      — the target is in the thread now.
//   • "missing"    — the server has no such row in this conversation (404), or
//                    refused the id as unusable (422). The reader is told,
//                    truthfully, that the message could not be located.
//   • "unreachable"— THE READ FAILED, which is not the same fact and does not
//                    lead the reader to the same next move. 「已經被清掉了」 ends
//                    the matter: nobody retries a message that is gone. A 5xx, a
//                    dropped connection or a timeout means the message is
//                    probably right there and the honest thing to say is "can't
//                    read it right now, try again" — with a way to try.
//   • "superseded" — a LATER load committed while our two windows were in the
//                    air, so this result was dropped to keep the thread in
//                    order. THE MESSAGE IS STILL THERE. Reporting this as
//                    "missing" put 「找不到那則訊息,可能已經被清掉了」 on screen
//                    about a message that exists, with no retry and no button —
//                    a lie with a dead end behind it. The caller must retry or
//                    re-schedule, never accuse.
//   • "cancelled"  — THIS TRIP WAS CALLED OFF, and by the only two things that
//                    can call one off: the room was left (or swapped) while the
//                    walk to the live tail was in flight, or a NEWER jump
//                    replaced it. Nothing was committed and nothing should be:
//                    unlike "superseded" there is no re-schedule to make, because
//                    whatever cancelled this is already doing the work. Telling
//                    the caller to retry here would resurrect a jump the reader
//                    has moved on from, on top of the one they are waiting for.
export type JumpOutcome =
  | "found"
  | "missing"
  | "unreachable"
  | "superseded"
  | "cancelled";

// Topics that mutate the chat thread → trigger a refetch. "chat_read" advances a
// participant's last-read watermark (the peer read our messages).
const CHAT_TOPICS = new Set(["chat", "chat_read"]);

// The loaded window for THE conversation this hook instance was mounted for.
//
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
      // 🔴 AN EMPTY CURSOR PAGE IS A CONTRADICTION, NOT AN ENDING (review S1).
      // The tempting reading is "nothing older than the cursor exists ⇒ the
      // seam was empty" — and it is WRONG here, definitionally. backfillSeam is
      // only ever entered when `pageJoinsThread` returned false, and that
      // function's first line is `if (have.length === 0 …) return true`, so we
      // are ALWAYS holding messages when this runs, and every one of them is
      // older than this cursor (the cursor starts at the newest page's oldest
      // row and only walks backwards). A server that answers "there is nothing
      // older" is therefore contradicting rows we are holding in our hand.
      //
      // The ways it happens are ordinary, not exotic: retention trimmed the
      // range between the two requests; the `with` filter does not resolve the
      // same set both times; the DAL's id tiebreak disagrees with this file's
      // JS string compare in `cmpStreamOrder`. All of them lose rows out of the
      // MIDDLE of the thread — the exact defect this whole file exists for.
      //
      // So this is a give-up like the other two, and it says so. Measured: the
      // 10-row case went `missing = 10, gapSuspected = false, warns = 0` before
      // this line changed. There is no false-positive cost to weigh against it,
      // because the "we really had reached the start of the stream" reading
      // cannot occur on this path at all.
      if (page.length === 0) {
        console.warn(
          "useChat: backfill got an EMPTY page for a cursor we hold older " +
            "messages than; the seam cannot be confirmed closed",
        );
        return { filled, joined: false };
      }
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
      messages: latest,
      hasMore: latest.length >= CHAT_PAGE_SIZE,
      gapSuspected: gap,
      hasNewer: prev.hasNewer,
    };
  }
  const pageIds = new Set(latest.map((m) => m.id));
  const fill = backfill.filter((m) => !pageIds.has(m.id));
  const fillIds = new Set(fill.map((m) => m.id));
  const older = prev.messages.filter(
    (m) => !pageIds.has(m.id) && !fillIds.has(m.id),
  );
  return {
    messages: [...older, ...fill, ...latest],
    hasMore: prev.hasMore,
    gapSuspected: gap,
    hasNewer: prev.hasNewer,
  };
}

// `entryAnchorMsgId` — THE ROOM IS ENTERED AT THE ANCHOR, NOT AT THE TAIL
// (T-48, owner ruling: 「你應該直接打成我們希望的流程」).
//
// Arriving through 跳到原訊息 / a kept link used to load the NEWEST page first
// and let ChatArea's anchor window replace it a moment later: one wasted
// round-trip, and — far worse — a real intermediate screen showing the live tail
// to a reader who is on their way somewhere else entirely. Every unpleasant
// consequence of that intermediate screen (the unread run being consumed before
// the jump landed, the entry scroll fighting the jump's) then needed its own
// patch to hold it back.
//
// So the caller names the anchor UP FRONT and this hook simply does not fetch a
// newest page on entry; ChatArea fetches the window around the id instead
// (`loadAround`, which it owns because the jump's viewport, highlight and
// miss-notice are its business). Passing `undefined` — every entry that is not
// a jump — is byte-for-byte the old path, and `useChat.scrollback.test.ts` pins
// exactly that.
/** Everything that belongs to ONE conversation and must not survive a switch
 * to another. `latches` is the lease record (lib/conversationLatches — its
 * state is unreachable except through a handle); `dropDebt` is the handle for
 * the "last load never landed" debt, which is TAKEN by the load that failed
 * and PAID by the next load that lands, so it cannot live on either call's
 * stack. Both are built together, once per mount. */
type ConversationSlot = {
  readonly latches: ReturnType<typeof openLatches>;
  dropDebt: LatchRelease | null;
};

export function useChat(
  withId: string,
  entryAnchorMsgId?: string,
): UseChat {
  // The thread, its mirror and its generation clock — all three behind
  // `lib/threadCommit`, which is the only thing that can write them.
  const view = useThreadCommit();
  const thread = view.thread;
  const [peerLastReadTs, setPeerLastReadTs] = useState(0);
  // See `UseChat.initialLoading`. FALSE until the first load of this
  // conversation has settled — succeeded, failed, or been refused — through
  // whichever door it came.
  const [firstLoadSettled, setFirstLoadSettled] = useState(false);
  const settleFirstLoad = useCallback(() => setFirstLoadSettled(true), []);
  // 🔴 THE OTHER HALF OF THE SAME WAIT (T-48 fix14, review25 F2). `firstLoadSettled`
  // answers 「這條對話載過了沒有」, which is a fact about the ROOM. The wait the
  // reader is looking at is a fact about the TRIP: 跳到原訊息 into a room that is
  // ALREADY OPEN (a second jump on the same member, the browser's back button, a
  // kept link) changes only `route.msgId` — `OfficePage` keys `ChatArea` by
  // `peerId`, so nothing remounts, the thread is non-empty and the room-level
  // flag is long since true. Without this the whole walk — which is the LONGEST
  // wait this feature has, many round trips — drew nothing at all.
  //
  // ⚠️ IT IS STILL ONE STATE AND ONE RENDER (owner c-de666642e77b「不管是進聊天
  // 室,或點選原訊息都是這樣」; c-d24ebd7f8d78「照理說應該只有改一個地方吧?」).
  // This is not a second flag the view reads — the view reads `initialLoading`
  // and only that, and `initialLoading` is where the two facts are OR'd, once.
  const [anchorWalking, setAnchorWalking] = useState(false);
  // 🔴 THE WALK'S CANCELLATION, IN THE TWO SHAPES `load()` ALREADY USES (T-48
  // fix14, review25 F1). `fetchToLatest` is an unbounded `for(;;)` over the
  // network; before this it closed over nothing that could stop it, so leaving
  // the room or asking for a different message left the old walk paging away and
  // able to commit onto a screen that had moved on.
  //   · `walkAliveRef` — this instance / this conversation is gone (`alive`).
  //   · `walkGenRef`   — a NEWER jump has replaced this one (the generation).
  // A walk that fails either stops asking for pages AND commits nothing.
  // ⚠️ This is cancellation ONLY. Whether the walk should also stop after N rows
  // is a separate open question (rc-e6b1d822def1) and deliberately not answered
  // here — do not turn either of these into a bound.
  const walkAliveRef = useRef(true);
  const walkGenRef = useRef(0);
  useEffect(() => {
    walkAliveRef.current = true;
    return () => {
      walkAliveRef.current = false;
    };
  }, [withId]);
  // 🔴 LOAD GENERATIONS (T-b0bb, review B2). Before the backfill, a load was
  // "fetch → commit" with ZERO awaits in between, so two overlapping loads
  // could only interleave if the network answered out of order. The backfill
  // put up to MAX_BACKFILL_PAGES round-trips between the fetch resolving and the
  // commit — and it opens that window exactly during a burst, i.e. exactly when
  // the peer is still typing and another load is most likely to start and finish
  // inside it. Measured on the unguarded code: load A stalls in its backfill, a
  // newer load B completes, then A commits on top — 75 rows, none missing, none
  // duplicated, and the newest 5 sitting at the TOP of the conversation, plus the
  // same seam backfilled twice because both loads compared against the same stale
  // mirror. `alive` does not cover this: it only says "the effect was torn
  // down", never "a newer load already landed".
  //
  // So every load takes a ticket when it STARTS and may only commit while no
  // later ticket has committed. A superseded load is dropped whole, which is
  // safe because the later load fetched a newer window and backfilled from it
  // down to the same held rows — its result is a superset of the one we drop.
  // `ChatArea.groupMessages` "only partitions, never reorders", so array order
  // IS screen order: a late commit is not a cosmetic race.
  //
  // 🔴 THE CLOCK ITSELF NOW LIVES IN `lib/threadCommit` (T-48), because the
  // re-check has to happen at the COMMIT rather than at each call site and there
  // must be exactly one copy of that ordering. `view.takeTicket()` mints;
  // `view.commit(seq, …)` is the only thing that can advance the committed
  // watermark, and it re-asks the ticket at the moment it writes — a load that
  // started later and finished sooner must not be judged superseded by a page
  // it precedes.
  // The entry anchor, mirrored for the subscription effect below. Read in the
  // effect's SETUP body only — never a dependency, or a route that keeps the
  // msgId in the hash (it does) would re-subscribe the whole SSE sink.
  const entryAnchorRef = useRef(entryAnchorMsgId);
  entryAnchorRef.current = entryAnchorMsgId;
  // 🔴 EVERY LATCH IN THIS HOOK IS A LEASE ON ONE CONVERSATION, AND THAT IS
  // NOW SAID IN THE TYPE RATHER THAN IN A COMMENT (T-48, fourth-review
  // rebuild).
  //
  // The same defect was found four times in four different places: a boolean
  // or counter that gates the newest-page load was left set by a conversation
  // the owner had ALREADY LEFT (F2, R3-1) — or by a call that had already
  // ended (R4-1, R4-2) — and the room never loaded again: permanently blank,
  // never marked read, nothing on screen to say so. Each earlier fix was one
  // more line somebody had to remember to write, and the review after it found
  // the line nobody had written.
  //
  // So the state stops being fields on a record. `openLatches` closes over it
  // (see lib/conversationLatches): there is no property to read, none to
  // assign, and `as any` reaches nothing. Setting a latch means `acquire`,
  // dropping one means calling the handle `acquire` returned, and that handle
  // is bound to THE RECORD IT CAME FROM.
  //
  // What that buys mechanically, rather than by discipline:
  //   · A latch can never gate another conversation's load: the record is per
  //     conversation, built with the hook and dying with it.
  //   · A switch resets ALL of them at once, and `openLatches` is one function
  //     initialising the whole set — there is no per-latch reset line to miss.
  //   · "Release whatever record is current" IS NOT WRITABLE HERE. There is no
  //     lookup-by-peer function any more; the only way to reach a record is to
  //     have captured it, so a late finally settles into its own record. That
  //     was R4-1 — a release that re-looked-up the current record decremented
  //     a counter it never incremented (to -1, which disables the `> 0` gate
  //     outright) and unlatched an anchor still in the air, while all 1672
  //     tests stayed green.
  //   · An anchor lease cannot be released on some endings and not others: the
  //     handle is dropped in one `finally` and dropping it is what ends the
  //     entry-anchor window (R3-3).
  //
  // What each latch holds off:
  //  • `entryAnchor` — this subscription started at an anchor, so the thread
  //    is deliberately EMPTY until ChatArea's `loadAround` lands. A `load()`
  //    in that window (SSE burst, focus, visibilitychange) would put the live
  //    tail on screen — reinstating the very intermediate screen anchor-first
  //    entry exists to remove — and would then be replaced again. IT MUST
  //    NEVER BE LEFT SET: while it is, this conversation does not refresh.
  //  • `anchorFetch` — a `loadAround` pair is in the air right now (see the
  //    lease's own note for the F3 measurement).
  //  • `loadStale` — a load was lost and the next relevant burst must be let
  //    through even when the per-conversation filter would skip it (T-929f,
  //    see below).
  //  • `loadingOlder` / `loadingNewer` — same-direction mutexes, so a scroll
  //    handler firing repeatedly cannot stack duplicate cursor requests.
  //
  // ⚠️ NOT in here, deliberately: the generation clock (`view.takeTicket()` /
  // the watermark inside `lib/threadCommit`). Those are a
  // MONOTONIC clock, not a latch — a ticket taken later must outrank one taken
  // earlier, and resetting them mid-conversation would let a stale in-flight
  // load out-rank a fresh one. They are `useRef`s in `useThreadCommit`, so they
  // start at zero with the hook, which since R13-5 means with the conversation.
  //
  // 🔴 BUILT ONCE PER MOUNT, NOT PER RENDER OR PER EFFECT RUN (fourth-review
  // R4-2, R13-5). The rebuild used to live in the subscription effect's setup
  // body, which re-runs WITHOUT the conversation changing — StrictMode's
  // setup→cleanup→setup does exactly that on every mount. Rebuilding there
  // re-armed `entryAnchor` behind a `loadAround` that had already run, and
  // ChatArea's jump latch is one-shot per id, so no second `loadAround` was
  // coming to clear it: R3-1's symptom reached without a peer switch. It then
  // lived in a `useKeyedRecord` keyed on `withId`, which is the same thing as a
  // mount now that `ChatArea` is mounted under `key={peerId}`.
  const convRef = useRef<ConversationSlot | null>(null);
  if (convRef.current === null) {
    convRef.current = {
      latches: openLatches(entryAnchorRef.current !== undefined),
      dropDebt: null,
    };
  }
  const conv = convRef.current;
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
  // ⚠️ StrictMode: the record is NOT rebuilt by this effect at all any more —
  // it is built once, during the first render of this mount. That is what makes
  // setup→cleanup→setup harmless, where the old rebuild-in-the-setup-body
  // re-armed a latch behind a job that had already run (R4-2).

  // The PEER's watermark for this conversation: the receipt whose READER is the
  // peer and whose PEER is the owner — i.e. how far the peer has read the
  // OWNER's messages. That is the "已讀" cutoff the outgoing rows draw.
  //
  // 🔴 THE QUERY ARGUMENT IS THE OWNER, NOT THE PEER, AND THAT IS THE WHOLE
  // FIX (T-48). `GET /api/chat/reads?with=X` is `WHERE peer_id = X` with NO
  // reader filter (server/ocserverd/dal.go ListChatReads, called with reader=""
  // from api_chat.go). So `?with=<peer>` returns every receipt ABOUT the peer's
  // conversation — the owner's own watermark among them — and never the row
  // this hook wants, whose peer_id is the OWNER.
  //
  // It used to appear to work because of a row that should never have existed:
  // `GET /api/chat?with=` wrote an auto read-receipt as a side effect, so an
  // agent merely polling its own conversation grew a SELF row (reader=X,
  // peer=X). That row satisfied `?with=X` + `readerId === X` and was read as
  // "the peer has read up to here" — a watermark minted by a poll, not by a
  // reader. Commit 8cd4fff9 deleted that side effect on this branch, which
  // means the old lookup now matches NOTHING, forever, with no error anywhere:
  // every 已讀 tick would silently stop lighting up.
  const refetchReads = useCallback(async () => {
    try {
      const reads = await api.listChatReads(OWNER_ID);
      // 🔴 THIS IS NOT "THE SAME PERSON'S DATA, ONE STEP STALE" (T-48, R8-2).
      // The inventory used to exempt this commit on that reasoning, and the
      // reasoning was wrong: the subscription effect fires `void refetchReads()`
      // on ENTRY, `peerLastReadTs` is one `useState` on a hook that is never
      // remounted, and the `withId` this call captured is the peer it was fired
      // for. So entering B, leaving before its receipts land, and coming back to
      // A writes *B's* watermark into A's room — read ticks lit on A's outgoing
      // rows off somebody else's reading, and nothing corrects it until the next
      // `chat_read` delta or the next entry. One mis-click reaches it.
      const peerReceipt = reads.find((r) => r.readerId === withId);
      // No staleness guard here, and that is deliberate: a watermark that lands
      // late is still this room's own watermark, merely older, and
      // `mergePeerRead` keeps the larger of the two rather than letting it fall
      // (R11-7). This hook belongs to one room, so there is no other room's
      // watermark that could arrive here.
      setPeerLastReadTs((prev) =>
        mergePeerRead(prev, peerReceipt ? peerReceipt.lastReadTs : 0),
      );
    } catch (e) {
      console.warn("useChat: reads refetch failed", e);
    }
  }, [withId, conv]);

  // Leave an anchor window and REPLACE the thread with the live newest window.
  //
  // 🔴 REPLACE, NOT MERGE, AND THAT IS THE HONEST CHOICE. An anchor window sits
  // somewhere in the past; between its newest row and the live tail is a range
  // NOBODY has fetched. Concatenating the two would draw them as adjacent, and
  // a thread that lies about being contiguous is the whole T-b0bb defect. The
  // dropped rows are not lost: `loadOlder`'s cursor walks straight back into
  // them, exactly as it does after any reload.
  //
  // `gapSuspected` is carried over rather than cleared — it is sticky for the
  // life of the conversation view on purpose (see Thread.gapSuspected).
  const resetToLatest = useCallback(async () => {
    // 「回到最新」 is the owner saying, in as many words, that they want the live
    // tail — so whatever the entry anchor was holding back is released HERE,
    // before the fetch rather than after it. Leaving it set would leave the
    // conversation with no periodic/SSE refresh for the rest of the session:
    // a room that quietly stops receiving, which is the failure shape this
    // ticket exists to stop shipping.
    // Taking an anchor lease and dropping it on the spot is the ONE door out
    // of the entry-anchor window (dropping such a lease is what ends it), and
    // deliberately the same door the real fetch uses — there is no second,
    // by-name way to unlatch an anchor, because a second way is what R4-1 was.
    // The counter is left exactly as it was found.
    conv.latches.acquire("anchorFetch")?.();
    const seq = view.takeTicket();
    try {
      const next = await api.listChat(withId);
      // 🔴 A LATE RESET MUST NOT BURN A GENERATION TICKET (T-48, R5-1; bound to
      // the VISIT in R6-1). The watermark used to be raised HERE, on the line
      // above the commit, unconditionally: a `resetToLatest` belonging to a
      // conversation the owner has LEFT (ChatArea fires one from the anchor
      // fetch's miss branch when the thread is empty) would raise the global
      // watermark and then drop its own page — and every load the NEW
      // conversation had already started, ticketed lower, was silently judged
      // superseded and thrown away. No spinner, no error; the room just sits
      // there until the next SSE burst happens to heal it. The clock is
      // deliberately global and never reset (see its note), which is exactly why
      // writing to it has to be earned — so `commit` is the only writer, and it
      // writes AFTER its own await, having re-asked the same question.
      const ok = await view.commit(seq, (prev) => ({
        messages: next,
        hasMore: next.length >= CHAT_PAGE_SIZE,
        gapSuspected: prev.gapSuspected,
        hasNewer: false,
      }));
      // 🔴 NOTHING IS CLEARED HERE ANY MORE, AND THAT IS A DELETION RATHER
      // THAN AN OVERSIGHT (T-48 fix12). Two lines used to hang off `ok`: the
      // forward walk's no-progress marker and the pending-gesture drop. Both
      // belonged to a forward pager driven by reader gestures, and there is no
      // such pager — `loadAround` fetches through to the tail before it
      // commits, so nothing outlives a thread replacement that would have to be
      // told about it.
      void ok;
    } catch (e) {
      console.warn("useChat: resetToLatest failed", e);
    }
  }, [withId, conv, view]);

  const refetch = useCallback(async () => {
    // Post-send refetch. MERGE the newest page (id-dedupe, history kept in
    // front) — never replace, or the loaded scrollback would vanish under the
    // owner. Takes a generation ticket like load() does — see the generations note.
    //
    // SENDING RETURNS TO THE LIVE TAIL (T-48 ③). While the thread is an anchor
    // window the message we just sent is BEYOND it, so a merge would drop it on
    // the floor and the composer's own line would never appear. Nothing else
    // can honestly be done with a newest page here — see resetToLatest.
    if (view.current().hasNewer) {
      await resetToLatest();
      await refetchReads();
      return;
    }
    const seq = view.takeTicket();
    const next = await api.listChat(withId);
    // T-b0bb: close the seam BEFORE merging (see backfillSeam). `view.current()`
    // is the live mirror — reading `thread` here would be a stale closure.
    const cur = view.current();
    let fill: ChatMessage[] = [];
    let gap: boolean | undefined;
    if (!pageJoinsThread(cur.messages, next)) {
      const r = await backfillSeam(withId, cur.messages, next);
      fill = r.filled;
      if (!r.joined) gap = true;
    }
    // 🔴 THE ONLY THING THAT MAY STOP A COMMIT HERE IS A NEWER TICKET (T-48,
    // R8-1, simplified by R13-3). This used to also ask "is the visit that
    // started this refresh still on screen?", because a send survives a
    // conversation switch and the hook did not: the POST was in the air, the
    // owner clicked away and back, and this refresh replaced the new visit's
    // anchor window with the previous visit's live tail. The hook is mounted per
    // room now, so a refresh started before the switch commits into a component
    // React has already discarded.
    //
    // This path tolerates a long await window — backfillSeam puts up to
    // MAX_BACKFILL_PAGES round trips right here — and `commit` re-checks the
    // ticket at the moment it writes, which is the one place that ordering is
    // decided.
    const ok = await view.commit(seq, (prev) =>
      mergeLatestPage(prev, next, fill, gap),
    );
    if (!ok) {
      // A newer load committed while we were paging backwards ⇒ this page and
      // its backfill are stale. The peer's watermark is still worth pulling, so
      // the drop is a skipped COMMIT, not an early return.
      await refetchReads();
      return;
    }
    // ⚠️ THIS PULL NO LONGER HAS A CAUSE, AND SAYING SO IS THE POINT (T-48).
    // It used to read: "listChat itself marks the owner's read watermark
    // server-side; pull the peer's watermark alongside so the badges
    // reconcile." That reason died with the side effect — GET /api/chat writes
    // nothing now, and a POST of OUR OWN message cannot move the PEER's
    // watermark either, so no part of a send can change the value this fetches.
    //
    // What it is instead, checked rather than guessed: an unconditional refresh
    // on a moment when the owner is demonstrably looking. It is a safety net
    // for a `chat_read` delta that went missing WITHOUT the connection
    // dropping (a drop is already covered — `es.onopen` → resyncAll). No test
    // pins it (nothing asserts a receipts pull after a send) and nothing
    // observable depends on it; it costs one GET per sent message. It is kept
    // rather than deleted because removing it is a behaviour change with no
    // guard of its own, not because a reason was found for it.
    await refetchReads();
  }, [withId, conv, refetchReads, resetToLatest, view]);

  useEffect(() => {
    let alive = true;
    // 🔴 THE LATCHES ARE NOT RESET HERE ANY MORE, AND THAT IS THE POINT
    // (fourth-review R4-2). They are built once per mount, above — this effect
    // re-runs for reasons that have nothing to do with the conversation
    // (StrictMode's setup→cleanup→setup, and any future dependency), and a
    // rebuild here re-arms a latch behind a one-shot caller that is never
    // coming back. It also used to pull an in-flight `loadOlder`/`loadNewer`
    // mutex out from under itself on an unrelated re-subscribe.

    // 🔴 ONE INSTANCE OF THIS HOOK IS ONE CONVERSATION (T-48, R13-3). The
    // supported way to change room is to MOUNT AGAIN — `ChatArea` is rendered
    // under `key={peerId}`, and `lint-chat-area-key` keeps it that way — which
    // is why `Thread` no longer carries the peer it belongs to and no commit
    // point here asks whose thread it is holding.
    //
    // These two lines are the safety NET for a caller that swaps `withId` on a
    // live instance instead: the thread converges on the new room rather than
    // staying on the old one. They are not a guarantee, and the difference
    // matters — an effect runs AFTER the commit that already painted, so such a
    // caller still gets one frame of the previous room's messages under the new
    // room's header. That frame is the whole of R11-1, and deleting it is what
    // the `key` did.
    //
    // 🔴 THERE IS NO SUCH CALLER TODAY, AND THAT IS WRITTEN DOWN RATHER THAN
    // LEFT TO BE REDISCOVERED (T-48, R14-3.3). `OfficePage`'s three `<ChatArea>`
    // branches all pass `key={x.id}` with the same `x` they pass as `member`,
    // so `key` IS `withId` and a room change is always a remount: nothing in
    // the product reaches this path. What it costs is one extra `clear()` per
    // mount, into a thread that is already empty. It stays because the shape it
    // catches is a shape somebody can go back to — drop the key, or mount this
    // hook from a surface that has no key of its own, and the convergence is
    // here waiting rather than being re-derived after the next report of a
    // room's messages under another room's header. `useChat.test.ts`'s "converges
    // on the new peer when a live instance is handed a different withId" is the
    // property, and it is a real one whether or not anything exercises it today.
    //
    // hasMore resets optimistic-true; the first landed page derives it honestly
    // (mergeLatestPage's empty-thread arm).
    //
    // 🔴 STILL SYNCHRONOUS, AND NO `await` MAY BE ADDED HERE (T-48). `clear()`
    // takes no parameters precisely so it can stay this way: there are no
    // messages in an empty thread, so there are no cards to wait for, and an
    // await on this line would paint one extra frame of the conversation the
    // owner has just left — the R11-1 frame, put back by hand.
    view.clear();
    setPeerLastReadTs(0);
    // A new conversation has not loaded yet, whichever door is about to load it.
    setFirstLoadSettled(false);

    // ONE load path (initial + SSE + refocus), and since T-48 ONE door: the
    // load never marks anything read, so a backgrounded window loads exactly
    // like a foreground one and the unread state is left to markRead(). Never
    // swallow a rejection into a phantom-empty thread — log it (a 401 is
    // already handled at the http layer, which bounces to login).
    const load = () => {
      // 🔴 AN ANCHOR WINDOW IS NOT REFRESHED BY A NEWEST PAGE (T-48 ③). See
      // Thread.hasNewer: merging the live tail into a historical window creates
      // a seam the T-b0bb machinery would spend six round-trips failing to
      // close, and would then report as LOST messages.
      //
      // 🟠 THERE IS NO PEER GUARD ON THIS READ ANY MORE, AND THAT IS A LATENT
      // BUG, NOT A PROPERTY. `Thread` dropped its `peer` field and the latches
      // dropped their peer stamp when `ChatArea` became `key={peerId}` (see
      // lib/threadCommit's header and lib/conversationLatches'). So on a
      // `withId` switch WITHOUT a remount, the effect calls `clear()` — which
      // deliberately does not advance the mirror — and this line then reads the
      // PREVIOUS room's thread. If that room was an anchor window, `hasNewer`
      // is true and the new room's first load returns here and never fires:
      // 0 requests, `messages: []`, forever. What makes that unreachable today
      // is the key alone (one hook instance per room, enforced by
      // scripts/check-chat-area-key.mjs) — nothing in this hook. Do NOT close
      // it by making `clear()` advance the mirror as a drive-by: that changes
      // this decision on every path and needs its own measurement.
      if (view.current().hasNewer) {
        return;
      }
      // 🔴 …AND NEITHER WHILE THE ANCHOR IS STILL COMING (T-48). `hasNewer`
      // only says "the thread I am holding is a historical window"; it cannot
      // say "the window is on its way", which is precisely the interval that
      // needs covering on an anchor-first entry. See the two latches.
      //
      // R3-1 was measured on this gate: an anchor still in the air for the
      // conversation the owner had just LEFT silenced this one's first load,
      // permanently (the new room stayed at 0 rows for 22s and never fetched
      // again). The fix was NOT a peer stamp on the latches — that stamp was
      // removed with `Thread.peer` (conversationLatches' header records why).
      // What closes it is that a latch record is CREATED BY, and dies with, one
      // conversation's hook, and `ChatArea`'s `key={peerId}` is what gives each
      // conversation its own hook. Same caveat as the read above: the guarantee
      // lives in the key, not here.
      if (
        conv.latches.isHeld("entryAnchor") ||
        conv.latches.isHeld("anchorFetch")
      ) {
        return;
      }
      // The generation ticket is taken at FIRE time, so a load that started
      // later always outranks one that started earlier, however long each of
      // them spends in the backfill. See the generations note above.
      const seq = view.takeTicket();
      api
        .listChat(withId)
        .then(async (next) => {
          // 🔴 SETTLED BEFORE THE `alive` GUARD, AND BEFORE THE BACKFILL. This
          // says 「the first load of this conversation has come back」, which is
          // true whatever we then decide to do with the page; putting it after
          // the guard would leave a torn-down-and-remounted room spinning on a
          // load it will never see the end of.
          settleFirstLoad();
          if (!alive) return;
          // Landed ⇒ whatever we owed is paid off. The handle was kept by the
          // load that failed; calling it settles THAT record's debt, which is
          // an orphan's no-op if the conversation has moved on since.
          conv.dropDebt?.();
          conv.dropDebt = null;
          // 🔴 T-b0bb: this page is the newest WINDOW, not a continuation. If
          // its oldest row does not join onto our newest one, page backwards
          // into the seam before merging — otherwise the uncovered range is
          // lost permanently and silently, and the server has already counted
          // it as read. backfillSeam never rejects, so a failed backfill costs
          // a marked gap, not the page we just fetched.
          const cur = view.current();
          let fill: ChatMessage[] = [];
          let gap: boolean | undefined;
          if (!pageJoinsThread(cur.messages, next)) {
            const r = await backfillSeam(withId, cur.messages, next);
            if (!alive) return;
            fill = r.filled;
            if (!r.joined) gap = true;
          }
          // 🔴 THE ORDERING GUARD (review B2) NOW LIVES INSIDE `commit`. The
          // backfill above is up to 6 round-trips long; a load that started
          // AFTER this one can have fetched,
          // backfilled and committed inside that window. Committing then would
          // splice an older newest-page on top of a newer thread — nothing lost,
          // nothing duplicated, and the newest messages moved to the top of the
          // screen. `commit` re-asks at the moment it writes and drops instead.
          //
          // ⚠️ `alive` is asked on BOTH sides of the commit and stays out of
          // `commit` itself: whether this effect is still mounted is the
          // effect's knowledge, not the thread's, and a door that guessed at it
          // would be guessing for every caller.
          const ok = await view.commit(seq, (prev) =>
            // MERGE the newest page into whatever is already loaded for this
            // peer (see mergeLatestPage) — replacing would eat the scrollback.
            mergeLatestPage(prev, next, fill, gap),
          );
          if (!alive || !ok) return;
        })
        .catch((e) => {
          // Same guard as the .then arm, and for a sharper reason: a load
          // belonging to a torn-down instance can reject AFTER the next
          // conversation's record exists. `conv` is the second half of that
          // guard — this closure holds the record it started on, so a late
          // rejection writes its debt into that one and never onto the new
          // conversation ("any debt the PREVIOUS peer left behind is not this
          // conversation's to pay").
          settleFirstLoad();
          if (!alive) return;
          // Do NOT retry here (T-929f). Record the debt only; the SSE sink
          // below pays it on the next relevant burst.
          conv.dropDebt = conv.latches.acquire("loadStale");
          console.warn("useChat: load failed", e);
        });
    };

    // 🔴 ANCHOR-FIRST ENTRY (T-48). This call is unconditional and it is
    // the `entryAnchor` lease inside `load()` that turns it into a no-op when an
    // anchor was named — ONE gate rather than two, because the entry is not the
    // only load that has to hold off (the SSE sink and the focus listener below
    // reach the same function) and a second copy of the condition here would be
    // a copy no test could tell from the original.
    //
    // ⚠️ THE INVARIANT THIS RESTS ON: somebody must actually fetch the anchor.
    // ChatArea does, unconditionally, from an empty thread (its jump reactor's
    // "not in the DOM" branch — an empty thread has nothing in the DOM), and its
    // miss branch falls back to `resetToLatest`. Both endings clear the pending
    // flag. If that ever stops being true this room stays blank.
    load();
    void refetchReads();

    // SSE: reconcile the thread by refetching on the relevant topics — but only
    // when the delta is about THIS conversation.
    //
    // 🔴 THE SELF-DRIVE (T-8115), AND WHY THE GATE OUTLIVED ITS CAUSE.
    // `load()` USED TO take a `GET /api/chat?with=` that was a DURABLE WRITE:
    // the server advanced the watermark and fanned a `chat_read` delta straight
    // back at this client, which re-ran the roster / office-total / worker
    // fan-out — so a `load()` fired for a delta about a DIFFERENT conversation
    // manufactured a second event round out of nothing, once per chat line
    // anywhere in the company. T-48 removed that write, so the load is now
    // "merely" a wasted request; the gate stays because a request nobody asked
    // for is still one, and because the write it used to trigger has only moved
    // (ChatArea's mark-read fans the same echo). Deltas name their
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
          conv.latches.isHeld("loadStale") ||
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

    // Coming BACK to the foreground while this thread is open: refresh, so what
    // accumulated in the background is on screen when the owner looks. (The
    // badge is cleared by ChatArea's markRead, not by this load.)
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
  }, [withId, refetchReads, conv, settleFirstLoad]);

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
    const cur = view.current();
    // Guards: the thread must really be THIS peer's (a switch is one commit
    // behind), non-empty (no cursor yet), still paged (hasMore), and no other
    // older-page fetch may be in flight (the concurrency lock).
    if (cur.messages.length === 0 || !cur.hasMore) return;
    const release = conv.latches.acquire("loadingOlder");
    if (!release) return;
    try {
      const oldest = cur.messages[0];
      const page = await api.listChat(withId, CHAT_PAGE_SIZE, {
        beforeTs: oldest.ts,
        beforeId: oldest.id,
      });
      // 🔴 A NEW AWAIT POINT, AND A CHEAP ONE. `mergeHistory` takes no ticket —
      // a history page is additive and can neither supersede nor be superseded —
      // but it goes through the same door, so a card riding a history page is
      // in hand before the rows are painted. Expect ZERO fetches in practice:
      // history pages are almost entirely answered/expired cards, and those are
      // never prefetched (they mount collapsed — owner rule 已回覆卡預設不載).
      await view.mergeHistory((prev) => {
        const have = new Set(prev.messages.map((m) => m.id));
        const older = page.filter((m) => !have.has(m.id));
        return {
          gapSuspected: prev.gapSuspected,
          hasNewer: prev.hasNewer,
          messages: [...older, ...prev.messages],
          // A short page = the history is exhausted (keyset paging makes this
          // exact; an exactly-full last page just costs one empty follow-up).
          hasMore: page.length >= CHAT_PAGE_SIZE,
        };
      });
    } catch (e) {
      console.warn("useChat: loadOlder failed", e);
    } finally {
      release();
    }
  }, [withId, conv, view]);

  // 🔴 FETCH FORWARD TO THE LIVE TAIL, IN MEMORY, COMMITTING NOTHING (T-48
  // fix12; owner rc-e1fb80065f8f 「一次撈100則撈完」 ＋ c-6a973512ed77 「我是指
  // 整個訊息撈完才 render」).
  //
  // The mirror image of `loadOlder`, and the direction the old API could not
  // express at all: page FORWARDS from the newest row we hold with `?start_id=`.
  // The anchor is inclusive, so a full page carries CHAT_WALK_PAGE_SIZE-1 new
  // rows plus the row we already hold; the duplicate is dropped on merge and the
  // page LENGTH (not the merged count) is what decides whether more remains.
  //
  // 🔴 IT TAKES NO TICKET AND WRITES NOTHING. It is a pure function of the
  // network plus its seed — the whole point of「撈完才 render」is that the view
  // sees ONE commit, made by its caller, with the finished thread. That also
  // means it cannot be superseded halfway and leave a half-thread on screen:
  // there is nothing on screen to leave.
  //
  // WHAT THIS REPLACES, AND WHY THE REPLACEMENT IS SMALLER. It used to be one
  // page per reader gesture, and「一次手勢一頁」needed a machine to be true: a
  // 400ms retry clock, a trailing replay for the gesture that clock swallowed (a
  // scroller pinned at its limit emits no further `scroll`, measured in
  // Chromium), a sight gate so a reader could not buy a page they had not been
  // shown, a served-anchor marker to tell one flick's 60 events from two flicks,
  // an in-flight-blocked marker, and a reader-at-bottom probe reaching out of
  // this hook into the viewport. None of those questions exist once nobody is
  // asking for pages.
  //
  // 📏 MEASURED (fix11, real Chromium, visual-guards/chat-render-cost.ct.spec.tsx,
  // zero network latency): rendering the finished thread costs 79ms at 1,000
  // rows, 236ms at 3,000, 581ms at 8,000 (heap 49 MB, 156k DOM nodes at 8,000).
  // Committing page-by-page instead cost 0.23s / 1.79s / 12.4s for the same
  // three — quadratic, because `ChatArea` has no virtualization and re-renders
  // the whole thread per commit. That measurement is why ONE commit is not
  // merely a preference here.
  //
  // 🔴 IT NEVER REJECTS. A failed page returns what it has plus
  // `reachedTail: false`, because the caller's alternative to committing a short
  // thread is committing NOTHING — a blank room where the reader asked to see a
  // message. See `loadAround`'s commit.
  //
  // 🔴 IT CAN BE CALLED OFF (T-48 fix14, review25 F1). `isCurrent` is the
  // caller's answer to 「這一趟還是現在這一趟嗎」 — see `loadAround`, which builds
  // it out of the mount's `alive` flag and the walk generation. It is asked
  // BEFORE every page (so a cancelled walk buys no further round trips) and
  // AGAIN after each one lands (so the very last page in flight cannot smuggle a
  // result past the caller's commit guard). A cancelled walk returns what it
  // happens to hold with `cancelled: true`, and its caller commits NOTHING —
  // the screen it was fetching for is not there any more.
  const fetchToLatest = useCallback(
    async (
      seed: ChatMessage[],
      isCurrent: () => boolean,
    ): Promise<{
      messages: ChatMessage[];
      reachedTail: boolean;
      cancelled: boolean;
    }> => {
      const out = [...seed];
      const have = new Set(out.map((m) => m.id));
      const stop = () => ({
        messages: out,
        reachedTail: false,
        cancelled: true,
      });
      try {
        for (;;) {
          if (!isCurrent()) return stop();
          const newest = out[out.length - 1];
          const page = await api.listChatWindow(
            withId,
            { startId: newest.id },
            CHAT_WALK_PAGE_SIZE,
          );
          if (!isCurrent()) return stop();
          let added = 0;
          for (const m of page) {
            if (have.has(m.id)) continue;
            have.add(m.id);
            out.push(m);
            added += 1;
          }
          // Short page ⇒ this window reached the live tail.
          if (page.length < CHAT_WALK_PAGE_SIZE) {
            return { messages: out, reachedTail: true, cancelled: false };
          }
          // 🔴 A FULL PAGE THAT ADDED NOTHING IS THE ONLY WAY THIS LOOP CAN FAIL
          // TO TERMINATE, and it is a server contradicting itself rather than an
          // ending — so it stops AND SAYS SO, with `reachedTail: false`. The
          // thread is then committed with `hasNewer` true: the 回到最新 arrow
          // stays up and the watermark is not stamped, which is the truth.
          if (added === 0) {
            console.warn(
              "useChat: the fetch to the live tail got a FULL page carrying " +
                "nothing new; stopping rather than asking the same question " +
                "again — the thread will be marked as not-the-tail",
            );
            return { messages: out, reachedTail: false, cancelled: false };
          }
        }
      } catch (e) {
        // No retry and no replay. What we have is committed with `hasNewer`
        // true, so the reader gets the message they jumped to AND an arrow that
        // says they are not at the latest — an affordance that is always there,
        // unlike the scroll event a pinned scroller cannot emit.
        console.warn("useChat: fetch to the live tail failed part-way", e);
        return { messages: out, reachedTail: false, cancelled: false };
      }
    },
    [withId],
  );

  // 🔴 THE JUMP THAT CANNOT MISS SILENTLY (T-48 ③). Two windows around one
  // message id — `end_id` for the context ABOVE it, `start_id` for the context
  // BELOW — and the target is in BOTH (the anchors are inclusive), so a
  // successful pair always contains it. Neither call pulls the whole history:
  // the cost of landing on a message from two years ago is two pages.
  //
  // Why two requests and not the three the proposal sketched ("fetch the row,
  // then a page each way"): the row comes back inside both windows already, and
  // an id no message carries is a 404 on either of them — so the separate
  // "does it exist" request answers a question these two have answered before
  // it could be asked.
  //
  // Returns whether the target really is in the thread now. The caller MUST
  // branch on it: the defect being fixed here was a miss (target older than the
  // loaded window) that landed the reader at the bottom, pixel-identical to a
  // hit on a recent message.
  // 🔴 THE ANCHOR LEASE IS TAKEN AND DROPPED IN ONE PLACE (T-48, R3-3 +
  // fourth-review R4-1). Two rules used to be comments somebody had to keep;
  // both are now shapes the type enforces:
  //
  //  1. RELEASE ON EVERY ENDING. The superseded branch used to keep the anchor
  //     latch SET on purpose — "the caller re-schedules" — and hand the
  //     clearing to ChatArea, whose only clearing path fires when the thread is
  //     EMPTY. But `superseded` means another load committed, i.e. the thread
  //     is NOT empty: with the retries spent, nobody cleared it and the
  //     conversation stopped refreshing for the rest of the session. Now the
  //     end of the entry window IS the drop of this lease, in one `finally`.
  //  2. DROP THE LEASE THAT WAS TAKEN, never a re-lookup. Releasing "whatever
  //     record is current" decremented a counter this call never incremented
  //     (`anchorFetch` to -1, which disables the gate outright) and unlatched
  //     an anchor still in the air, putting the live tail on screen on the next
  //     burst — the very intermediate screen this ticket removed. It stayed
  //     green through all 1672 tests. There is no longer a function that
  //     answers "the record current NOW", so that line cannot be written.
  //
  // `loadAround` itself has neither an acquire nor a release to get wrong.
  //
  // (An earlier generation pointed here at a `latch-inventory.md`. That file was
  // never in this repo — it lived in a contractor's scratch directory and its
  // copy still describes an API this file no longer has. The rule it carried is
  // the one stated above and in lib/conversationLatches; there is nothing else
  // to go and read.)
  const withAnchorFetch = useCallback(
    async <T,>(body: () => Promise<T>): Promise<T> => {
      const release = conv.latches.acquire("anchorFetch");
      try {
        return await body();
      } finally {
        // The handle, never a fresh lookup — and a fresh lookup is not
        // writable any more (see the record's note). A record whose
        // conversation the owner has already left is an orphan nobody reads,
        // so releasing it is a no-op, which is the correct answer.
        release?.();
      }
    },
    [conv],
  );

  const loadAround = useCallback(
    async (msgId: string): Promise<JumpOutcome> => {
      // 🔴 THE TRIP'S OWN GENERATION, TAKEN BEFORE THE FIRST REQUEST (T-48
      // fix14, review25 F1). `view.takeTicket()` orders COMMITS; this orders
      // TRIPS, which is a different question and asked earlier: a jump that has
      // been replaced must stop buying pages long before it has anything to
      // commit. `walkAliveRef` covers the other ending — the room is gone.
      const gen = ++walkGenRef.current;
      const isCurrent = () => walkAliveRef.current && walkGenRef.current === gen;
      // 🔴 AND THE WAIT GOES UP HERE, NOT AT THE MOUNT. This is the only line
      // that makes 「第二次跳到原訊息」 draw anything at all (review25 F2): the
      // room is already open, so nothing else in this hook is in a waiting
      // state. See `anchorWalking`.
      setAnchorWalking(true);
      // 🔴 EVERY ENDING SETTLES THE FIRST LOAD, INCLUDING THE ONES THAT THROW.
      // This is the anchor door's half of `initialLoading`; the ordinary door's
      // half is in `load()`. There are exactly two, they write the same flag,
      // and neither can end without writing it.
      try {
        return await withAnchorFetch(async () => {
        const seq = view.takeTicket();
        let older: ChatMessage[];
        let newer: ChatMessage[];
        try {
          [older, newer] = await Promise.all([
            api.listChatWindow(withId, { endId: msgId }, CHAT_PAGE_SIZE),
            // The forward half is the FIRST PAGE OF THE FETCH TO THE TAIL, so
            // it is asked at the walk's page size rather than the history one.
            api.listChatWindow(withId, { startId: msgId }, CHAT_WALK_PAGE_SIZE),
          ]);
        } catch (e) {
          // An unknown id is a 404 here, NOT an empty page — the server refuses
          // to make "no such message" look like "a window that happens to be
          // empty".
          //
          // 🔴 BUT A FAILED READ IS NOT A MISSING MESSAGE (T-48). Both used to
          // come back as one word, and the screen then said 「可能已經被清掉了」 to
          // somebody whose message was sitting right there behind a 502. The two
          // answers point the reader at opposite next moves — one ends the matter,
          // the other is worth retrying — so the split is by what the server
          // actually said:
          //   • 404 — this conversation carries no such row.
          //   • 422 — the id is not a usable id; sending it again cannot help.
          //   ⇒ both are "missing": retrying changes nothing.
          //   • anything else (5xx, 429, a rejected fetch with no status at all)
          //     ⇒ "unreachable": the read failed, and a retry is exactly the
          //     right thing to offer.
          console.warn("useChat: loadAround failed", e);
          const status = e instanceof ApiError ? e.status : 0;
          return status === 404 || status === 422 ? "missing" : "unreachable";
        }
        const byId = new Map<string, ChatMessage>();
        for (const m of [...older, ...newer]) byId.set(m.id, m);
        const window = [...byId.values()].sort(cmpStreamOrder);
        // 🔴 NOT MERELY DEFENSIVE — THIS IS A REACHABLE 200 (T-48, F1). The server
        // resolves the anchor WITHOUT the participant filter on purpose
        // (api_chat.go: "a window anchored outside it simply comes back empty,
        // which is the honest answer"), so a msgId that EXISTS but belongs to a
        // DIFFERENT conversation answers both calls with 200 + an empty array.
        // Adopting that window writes `messages: []` into the thread: the room
        // goes blank, the miss notice does not light, and nothing is logged.
        // Refusing turns it into the ordinary miss, which is what it is.
        if (!window.some((m) => m.id === msgId)) return "missing";
        // 🔴 EVERYTHING FROM THE ANCHOR TO THE LIVE TAIL, BEFORE ANYTHING IS
        // COMMITTED (T-48 fix12, owner c-6a973512ed77 逐字:「我是指整個訊息撈完
        // 才 render」). A full forward half means the stream continues below the
        // window; the rest of it is collected in memory here and lands in the
        // SAME commit as the anchor window.
        //
        // Why it is not a page-by-page commit with a spinner off the side:
        // measured (fix11), committing per page costs 12.4s of main-thread work
        // at 8,000 rows against 0.58s for one commit, because there is no
        // virtualization and every commit re-renders the whole thread. It is
        // also what makes the un-read watermark safe by construction — see
        // ChatArea's `tailSeen`: with one commit there is never a moment where
        // the thread holds the tail while the reader is still being moved.
        let all = window;
        let reachedTail = newer.length < CHAT_WALK_PAGE_SIZE;
        if (!reachedTail) {
          const r = await fetchToLatest(window, isCurrent);
          // 🔴 A CALLED-OFF WALK COMMITS NOTHING (T-48 fix14, review25 F1). The
          // reader has left the room or asked for a different message; the
          // half-thread in hand belongs to a screen that no longer wants it,
          // and the trip that replaced this one is the one that gets to paint.
          if (r.cancelled) return "cancelled";
          all = r.messages;
          reachedTail = r.reachedTail;
        }
        // …and the same question once more at the door, for the trip whose
        // forward half was short enough never to enter the walk at all.
        if (!isCurrent()) return "cancelled";
        // 🔴 THE WAIT COMES DOWN BEFORE THE COMMIT, NEVER AFTER IT (T-48
        // fix14). The spinner REPLACES the thread body, so the frame that
        // paints the fetched rows must not still be under it: `ChatArea`'s jump
        // reactor locates its target by querying the painted DOM, and a commit
        // that lands while the pane is still a spinner is a commit whose target
        // is never found — the reader ends up on a thread nobody scrolled.
        // Measured, not theorised: with this line in the `finally` instead,
        // `ChatArea.anchor-entry.test.tsx` loses the row and paints the
        // new-message strip in place of the 回到最新 arrow. React batches the
        // two into one frame when it can; this makes the ORDER not matter when
        // it cannot. The `finally` below still covers every ending that never
        // reaches a commit.
        setAnchorWalking(false);
        // 🔴 THE CARD PREFILL LANDS INSIDE THE `withAnchorFetch` LEASE, AND THAT
        // IS DELIBERATE (T-48). This is the one commit point where the extra
        // round trip is not merely tolerable but WANTED: while the lease is held
        // `load()` stands down, so the interval spent waiting for the jump
        // target's cards is also an interval in which the live tail cannot flash
        // onto the screen underneath the jump. The lease still releases in the
        // same `finally` — do not move this out of it.
        const ok = await view.commit(seq, (prev) => ({
          messages: all,
          // A full older page means history may continue above it; the tail
          // question is answered by the fetch above rather than by one page's
          // length now.
          hasMore: older.length >= CHAT_PAGE_SIZE,
          gapSuspected: prev.gapSuspected,
          // 🔴 COMMIT WHAT WE HAVE EVEN WHEN THE FETCH GAVE UP, AND SAY SO.
          // The alternative — refusing to commit — is a blank room for a reader
          // who asked to see a specific message. So the short thread lands and
          // this flag carries the truth: the 回到最新 arrow stays up, `load()`
          // stands down, and the watermark is not stamped.
          hasNewer: !reachedTail,
        }));
        // Overtaken, NOT missing — and the difference is the whole of F3. The
        // caller re-schedules; the latch is NOT what carries that decision
        // across the gap (see the finally below).
        if (!ok) return "superseded";
        return "found";
        });
      } finally {
        settleFirstLoad();
        // Only the CURRENT trip takes the wait down. A superseded one ending
        // later must not clear the spinner its replacement is still under.
        if (walkGenRef.current === gen) setAnchorWalking(false);
      }
    },
    [withId, withAnchorFetch, view, fetchToLatest, settleFirstLoad],
  );

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
    peerLastReadTs,
    // 🔴 DERIVED, NOT STORED — so it cannot get out of step with the thread.
    // Content on screen ends the wait no matter which door delivered it.
    // 🔴 ONE STATE, TWO FACTS, OR'D EXACTLY HERE (T-48 fix14, review25 F2).
    // 「這條對話的內容還沒到」 (the room has never been filled) OR 「正在走訪」
    // (a jump is fetching from its anchor to the live tail). The second is not a
    // special case of the first: 跳到原訊息 into a room that is already open has
    // content on screen and a settled first load, and it is the LONGEST wait
    // this feature has.
    initialLoading:
      anchorWalking || (!firstLoadSettled && thread.messages.length === 0),
    send,
    markRead,
    hasMore: thread.hasMore,
    loadOlder,
    gapSuspected: thread.gapSuspected,
    hasNewer: thread.hasNewer,
    loadAround,
    resetToLatest,
  };
}
