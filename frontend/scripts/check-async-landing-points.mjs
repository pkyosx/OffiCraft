#!/usr/bin/env node
// T-48 — the ASYNC LANDING POINT census for the chat surface.
//
// Every callback reachable from `ChatArea` that can commit AFTER the screen has
// moved on must be on the register below, with a verdict. It is a census, not a
// correctness proof — it cannot tell a guarded commit from an unguarded one.
// What it does is make the POPULATION self-maintaining, which is the thing that
// failed eleven times.
//
// 🔴 WHY IT EXISTS (R9-3). The stale-visit family had, by the ninth review,
// produced eleven instances. The criterion for finding them was fixed twice and
// the table format once, and it still missed:
//
//   * R7-1 (`useChat.loadAround`), R8-1 (`useChat.refetch`) and R9-1
//     (`useAttachmentStaging`) were each named in the inventory BEFORE they were
//     found, classified as safe, and therefore never looked at again;
//   * the census used to be re-derived by grepping for the EXISTING guards. That
//     start point can only ever find paths that already have a guard; a path
//     with no guard at all scores zero hits and passes silently. R9-1 was
//     exactly that, and it passed eight reviews that way;
//   * "how many `await`s between the guard and the commit" cannot describe R9-1
//     either — that path has NO `await`. It is a `FileReader.onload` callback.
//
// So the criterion is NOT `await`. It is: **from the moment this callback is
// queued to the moment it commits, can the screen have moved on?** `await` is
// one syntax for that gap. `setTimeout`, `requestAnimationFrame`,
// `queueMicrotask`, an event listener, a Resize/IntersectionObserver, an SSE
// handler and `FileReader.onload` are others, and every one is scanned below.
//
// HOW IT ENUMERATES. The file set is WALKED, not typed in: it starts at
// `ChatArea.tsx` and follows relative imports transitively through
// `src/components`, `src/hooks` and `src/lib`. A new hook that ChatArea starts
// calling joins the population by itself. Each (file, kind) pair carries a
// COUNT, so a second `setTimeout` in a file that already had one still reddens.
//
// 🔴 WHAT IT DOES NOT DO. It cannot check that a landing point is correctly
// guarded — a verdict is a human's claim, and a wrong claim goes green (three of
// them did). Its whole job is to guarantee that the claim EXISTS and is
// re-examined whenever the code under it moves.
//
// 🔴 WHY IT IS A LINT AND NOT A VITEST FILE (T-48, R13-6). It used to live in
// `src/components/asyncLandingPoints.test.ts`, mixed in with the behaviour
// tests, so "the census is out of date" and "the chat window is broken" were the
// same colour of red. This repo already has a home for a rule that reads source
// text — the `lint-*` family in `bin/ci.sh` — and this is one of those.
//
// The boundary is written down rather than assumed, and since R16 D-3 there are
// TWO of them because there are two questions:
//
//   * the ASYNC-LANDING census excludes `src/api` and `src/i18n`. They hold no
//     per-conversation React state — the transport layer's `await`s land in
//     `useChat`/`ChatArea` callbacks, which ARE in scope — and including
//     `api/http.ts` would put 126 unrelated `await`s under a count that churns
//     on every new endpoint.
//   * the MODULE-STATE census includes `src/api`, because that reason is about
//     await COUNTS and says nothing about whether one room's value can reach
//     another. `api/http.ts` already holds eight module-level mutable values and
//     an SSE transport is the likeliest home for a per-room cache; the
//     sixteenth review put one there and the census exited 0. `src/i18n` stays
//     out of both, and that one IS just inherited — measured, it declares no
//     top-level `let`/`var` and no unregistrable container, so walking it adds
//     no row either way. It is left out because the async half needs it out, not
//     because the state half has an argument; move it in the day i18n grows
//     something writable.
//
// Disagree by editing ASYNC_SCOPE / STATE_SCOPE.
//
// 🔴 IT CARRIES TWO MORE CENSUSES, BECAUSE IT ALREADY WALKS THE GRAPH (T-48,
// R14-3.1 / R14-1.6). Both are the same shape of question — "is this claim still
// true of the WHOLE chat surface, not of the one file somebody remembered?" —
// and both were being kept by prose:
//
//   * MODULE_STATE. `key={peerId}` deleted per-conversation component state, so
//     the only place the twelve-instance defect family can come back is state
//     that lives OUTSIDE a component. `chatDraftStore.ts` says in its header
//     that everything surviving a room switch now lives there, and
//     `chatDraftStore.test.ts` checks that sentence against ONE file — the file
//     that already obeys it. The instance that actually happened was
//     `liveComposers`, a second module-level per-room table grown in
//     `ChatArea.tsx`, and a one-file census cannot see that. This one reads
//     every file in the walk.
//   * useQuotedMessageOverlay's SINGLE CALLER. The full-screen overlay gave up
//     its own visit stamp on the grounds that ChatArea is its only caller and
//     ChatArea is keyed. That was true, checked by hand, and guarded by a
//     comment: a second caller whose own key is a card id would put room A's
//     message over room B, which is R8-3's original shape.
//
// Run: `npm run lint:async-landing` (also wired into bin/ci.sh).

import ts from "typescript";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// ASYNC_LANDING_SRC re-points the scanned tree — the ONLY reason it exists is
// check-async-landing-points.test.ts, which copies the real sources to a temp
// dir, sabotages one and asserts this script goes red. A guard nobody has
// watched fail is not a guard.
const SRC = process.env.ASYNC_LANDING_SRC
  ? resolve(process.env.ASYNC_LANDING_SRC)
  : resolve(dirname(fileURLToPath(import.meta.url)), "..", "src");
const ENTRY = join(SRC, "components", "ChatArea.tsx");
const rel = (file) =>
  file
    .slice(SRC.length + 1)
    .split("\\")
    .join("/");

// 🔴 TWO POPULATIONS, TWO BOUNDARIES, WRITTEN DOWN SEPARATELY (T-48, R16 D-3).
// There used to be one `inScope`, excluding `src/api`, and the reason recorded
// for the exclusion was about AWAIT COUNTS: `api/http.ts` holds 126 unrelated
// `await`s whose count would churn on every new endpoint. That reason is sound
// for the async-landing census and it does NOT transfer to the module-state
// census, which asks a completely different question — can a value written in
// one room be read in another? `api/http.ts` already holds eight module-level
// mutable values (`sseSource`, `sseState`, `sseRetryTimer`, `sseSubscribers`…)
// and an SSE transport is the single likeliest home for a per-room cache. The
// sixteenth review put `const lastSeenPerRoom = new Map<string, string>();`
// there and the census exited 0. So the state census walks `src/api` too, and
// the await census does not.
//
// Expressed RELATIVE to the scanned root so the selftest's temp copy is scanned
// the same way the real tree is (a root-absolute pattern silently matched
// nothing there, which would have made every sabotage below "pass").
const ASYNC_SCOPE = /^(components|hooks|lib)\//;
const STATE_SCOPE = /^(components|hooks|lib|api)\//;

// 🔴 THE ONE FILE THE STATE CENSUS SKIPS, AND WHY IT IS A NAME AND NOT A GLOB.
// `api/mock.ts` is the mock BACKEND — thirty-five module-level tables that stand
// in for the server's database while `VITE_USE_MOCK` is on. Every one of them is
// shared across every room, on purpose, because that is what a server is; asking
// "what keys it" of `chatLog` has no useful answer, and thirty-five rows saying
// so would bury the twenty-five that matter. It is excluded BY NAME so that
// `api/http.ts` — the real transport, and the likeliest home for a per-room
// cache — stays in, and so that adding a second mock file is a deliberate edit
// here rather than something a directory glob absorbs in silence.
const STATE_SKIP = /^api\/mock\.ts$/;

/** The shapes that put a gap between "queued" and "commits". */
const KINDS = [
  ["await", /\bawait\b/g],
  [".then/.catch/.finally", /\.(then|catch|finally)\s*\(/g],
  ["setTimeout/setInterval", /\bset(Timeout|Interval)\s*\(/g],
  ["queueMicrotask", /\bqueueMicrotask\s*\(/g],
  ["requestAnimationFrame", /\brequestAnimationFrame\s*\(/g],
  ["addEventListener", /\baddEventListener\s*\(/g],
  ["Observer", /\bnew (Resize|Intersection|Mutation)Observer\b/g],
  [
    "FileReader/Image handler",
    /\bnew FileReader\b|\.(onload|onerror|onloadend)\s*=/g,
  ],
  ["JSX onLoad/onError", /\bon(Load|Error)=\{/g],
  ["subscribe", /\bsubscribe[A-Za-z]*\s*\(/g],
  // 🔴 THE CATCH-ALL (R10-5 B4). The nine shapes above were the ones this
  // codebase happened to use, and the list was CLOSED against nothing:
  // `requestIdleCallback`, `window.onmessage =`, `new Promise(` and
  // `new BroadcastChannel(...).onmessage =` could all be added to `ChatArea` and
  // the census stayed green. This row is written to catch a shape nobody has
  // used yet, so it is deliberately broad; the lookaheads only keep it from
  // double-counting the rows above it.
  [
    "other async primitive",
    /\.on(?!load\b|error\b|loadend\b)[a-z]+\s*=|\bnew (?!Resize|Intersection|Mutation)[A-Z]\w*Observer\b|\brequestIdleCallback\s*\(|\bnew (Promise|BroadcastChannel|MessageChannel|Worker|EventSource|WebSocket|SharedWorker)\s*\(/g,
  ],
];

/** The whole list of shapes, pinned. Deleting a KIND is the one edit that
 * shrinks the population without adding a single row to the register — every
 * file that only ever landed through that shape simply stops being counted, and
 * both directions of the comparison below agree. Swapping one shape out for
 * another keeps the LENGTH the same, so the list itself is pinned, not its
 * length (R11-11). */
const KIND_NAMES = [
  "await",
  ".then/.catch/.finally",
  "setTimeout/setInterval",
  "queueMicrotask",
  "requestAnimationFrame",
  "addEventListener",
  "Observer",
  "FileReader/Image handler",
  "JSX onLoad/onError",
  "subscribe",
  "other async primitive",
];

/** file → kind → count, each with the verdict its author signed.
 *
 * A verdict answers ONE question: when this callback commits, can the screen be
 * showing something other than what queued it — and if so, what stops the write
 * from landing in the wrong place? */
const REGISTRY = [
  // ─── Mounted under a key that changes with the thing they belong to — the
  //     row, the card, or (since R13-5) the conversation itself. A switch
  //     UNMOUNTS them, so React drops every write these make. ───
  // ⚠️ The key is the ONLY thing saving these: none of them carries a guard of
  // its own. Hoisting any of them above the keyed element reopens R9-1's shape.
  {
    file: "components/AttachmentStrip.tsx",
    kind: ".then/.catch/.finally",
    count: 2,
    verdict: "share-link copy; keyed row, and the id is globally unique",
  },
  {
    file: "components/AttachmentStrip.tsx",
    kind: "setTimeout/setInterval",
    count: 1,
    verdict: "the 已複製 flash; keyed row, cleared on unmount",
  },
  {
    file: "components/ChatReplyCard.tsx",
    kind: "await",
    count: 4,
    verdict: "refetch / doAnswer / doReanswer; keyed row",
  },
  {
    file: "components/ChatReplyCard.tsx",
    kind: ".then/.catch/.finally",
    count: 2,
    verdict: "the same three calls' arms; keyed row, unmounted by the switch",
  },
  {
    file: "components/ChatReplyCard.tsx",
    kind: "subscribe",
    count: 1,
    verdict: "card SSE; unsubscribed in the effect cleanup",
  },
  {
    file: "components/ReplyCardBody.tsx",
    kind: "await",
    count: 2,
    verdict: "answer submit; keyed row / keyed card",
  },
  {
    file: "components/ReplyComposer.tsx",
    kind: "await",
    count: 1,
    verdict:
      'send; keyed row / keyed card, and its staging declares "remounts-per-conversation"',
  },

  // ─── ChatArea itself. Since R13-5 `OfficePage` mounts it under
  //     `key={peerId}`, so a conversation switch unmounts it and every setter
  //     below writes into a component React has discarded. That is what
  //     `lint-chat-area-key` keeps true; there is no per-callback guard left. ──
  {
    file: "components/ChatArea.tsx",
    kind: "await",
    count: 2,
    verdict:
      "submit(); the room's own draft is written to the store BEFORE the on-screen restore, so a failed send survives even when nobody is looking (§3 rule 4)",
  },
  {
    file: "components/ChatArea.tsx",
    kind: ".then/.catch/.finally",
    count: 3,
    verdict:
      "loadAround + the wake button's two arms; all three write only this room's state, and this room is this mount",
  },
  {
    file: "components/ChatArea.tsx",
    kind: "setTimeout/setInterval",
    count: 1,
    verdict:
      "highlight clear; cleared in the effect cleanup, and the state it writes dies with the room. The centring settle timer that used to be the second one was deleted with the jump's re-center loop (T-48, owner rc-6c27f486ef9d)",
  },
  {
    file: "components/ChatThreadLoading.tsx",
    kind: "setTimeout/setInterval",
    count: 1,
    verdict:
      "\u{1F534} THE SPINNER'S SHOW-AFTER DELAY (T-48 fix12). It writes ONE boolean, `shown`, into this component's own state, and this component is mounted only while `useChat.initialLoading` is true \u2014 i.e. only while the pane it belongs to is still empty. Every way the wait can end unmounts it, and the effect's cleanup clears the timer on that same unmount, so the callback cannot run after the content it was covering has arrived. There is no room identity to get wrong either: it is rendered under `ChatArea`, which is mounted under `key={peerId}`, so a conversation switch destroys the timer with the component. \u26a0\ufe0f It is a DELAY, not a minimum display time \u2014 nothing here keeps the spinner up once the thread has landed",
  },
  {
    file: "components/ChatArea.tsx",
    kind: "Observer",
    count: 1,
    verdict:
      "the PURE-READ latestInView observer (T-48). It writes one boolean derived from this mount's own scroller and nothing else — no scrollTop — so a callback that lands after the screen has moved on can only re-answer a question about a room React has already discarded; disconnected in the same effect's cleanup",
  },
  {
    file: "components/ChatArea.tsx",
    kind: "subscribe",
    count: 1,
    verdict:
      "the composer's own draft-TEXT subscription (R14-1.3), onto this peer's slice; a failed send's words are written to the store and this is what puts them back on screen. React unsubscribes on unmount, and the room is this mount",
  },
  {
    file: "hooks/useChat.ts",
    kind: "await",
    count: 24,
    verdict:
      "one hook instance per room (R13-3), so a commit from a room the owner has left lands in a discarded component; the generation clock is what orders the commits WITHIN a room. 🔴 17 → 23 (T-48): every one of the six thread-writing paths now goes through `lib/threadCommit`, which awaits the page's WAITING reply cards before the rows reach the view — resetToLatest / refetch / load() / loadAround each traded a bare setter for `await commit(seq, …)`, and loadOlder for `await mergeHistory(…)`. The re-signed verdict is the same one: the extra awaits widen the window a newer load can commit inside, and `commit` is where the ticket is re-asked AFTER its own await. 🔴 23 → 24 (T-48 fix12): the forward PAGER is gone and `fetchToLatest` is here instead — a loop that awaits `listChatWindow` once per 100 rows, from the jump anchor to the live tail. It is by far the longest await window in this file (79 round trips at 8,000 rows, measured) and it is also the only one that CANNOT land in the wrong place, because it commits nothing: it accumulates into a local array and hands the finished thread to `loadAround`, which makes the single ticketed commit. A load that lands inside that window is not raced by it — it is judged by `commit` in the ordinary way, once, at the end",
  },
  {
    file: "hooks/useChat.ts",
    kind: ".then/.catch/.finally",
    count: 2,
    verdict:
      "load()'s arms; the effect's own `alive` flag, re-asked after every await",
  },
  {
    file: "hooks/useChat.ts",
    kind: "addEventListener",
    count: 2,
    verdict: "focus / visibilitychange; removed in the same effect's cleanup",
  },
  {
    file: "hooks/useChat.ts",
    kind: "subscribe",
    count: 1,
    verdict: "the SSE delta sink; unsubscribed in the same cleanup",
  },
  {
    file: "hooks/useQuotedMessageOverlay.tsx",
    kind: "await",
    count: 1,
    verdict:
      "open(); its state dies with the room that opened it (R13-5), and `busyRef` keeps one click to one request",
  },
  {
    file: "hooks/useAttachmentStaging.ts",
    kind: "FileReader/Image handler",
    count: 2,
    verdict:
      "🔴 R9-1 lived here with ZERO guard and no `await` to hint at one. Now the commit NAMES ITS SLOT: `updateChatDraftAttachments(peer, …)` writes into the draft of the room the file was picked for, whatever is on screen and whether or not any composer is mounted. R9-1, R10-4 and R11-2 have no shape left to happen in (S12/S13/S14/S18/S19/S20)",
  },
  {
    file: "hooks/useAttachmentStaging.ts",
    kind: "subscribe",
    count: 1,
    verdict:
      "the one `subscribe` callback both `useSyncExternalStore` calls share, onto this peer's draft slice; React unsubscribes on unmount, and a per-mount caller gets `peerId === null` and subscribes to nothing at all",
  },
  {
    file: "lib/chatDraftStore.ts",
    kind: "subscribe",
    count: 1,
    verdict:
      "the per-peer draft subscription; each composer unsubscribes on unmount, and a write is delivered to whoever is showing that peer — which is the point",
  },
  {
    file: "lib/chatDraftStore.ts",
    kind: "queueMicrotask",
    count: 1,
    verdict:
      "the chat page's notice scope closing one microtask late (R16 D-2), so StrictMode's synchronous setup→cleanup→setup cannot destroy a refusal raised before the mount. It drops only the peers that had a notice AT CLOSE TIME, so a FileReader landing in that gap is not swept up by a decision that predates it, and it bails if a surface reopened the scope",
  },

  // ─── Rendered by ChatArea without a key of their own — they live and die
  //     with ChatArea, which lives and dies with the conversation. ───
  {
    file: "components/ChatGalleryPanel.tsx",
    kind: ".then/.catch/.finally",
    count: 2,
    verdict:
      "gallery fetch; `alive` + deps [member.id], and the panel dies with the room",
  },
  {
    file: "components/ChatGalleryPanel.tsx",
    kind: "addEventListener",
    count: 1,
    verdict: "Escape layer; removed on unmount",
  },
  {
    file: "components/ChatGalleryPanel.tsx",
    kind: "subscribe",
    count: 1,
    verdict: "gallery SSE; unsubscribed on unmount",
  },
  {
    file: "components/Avatar.tsx",
    kind: "JSX onLoad/onError",
    count: 1,
    verdict:
      "<img onError>; records the URL that failed, which can never match another avatar, and the [personal, theme] effect clears it",
  },
  // ⚠️ WHO UNMOUNTS THIS OVERLAY — four mount points, two owners, and the
  // eleventh review caught this line claiming one of them for all four (R11-10).
  // `ChatArea` mounts it three times from `mdPreview`; `AttachmentStrip` mounts
  // it a fourth time from its own `preview`, and a strip is not inside a
  // conversation at all — two of its three call sites are a reply card and the
  // task-artifacts popover. That one is bounded instead by the list it renders:
  // the preview is looked up in `attachments` every render, so it cannot survive
  // the list it was opened from (R11-1).
  {
    file: "components/MarkdownPreviewOverlay.tsx",
    kind: "await",
    count: 1,
    verdict:
      "share-link copy; writes only this overlay's own state, and the overlay dies with whatever opened it",
  },
  {
    file: "components/MarkdownPreviewOverlay.tsx",
    kind: ".then/.catch/.finally",
    count: 5,
    verdict:
      "blob fetch + copy; writes only this overlay's own state, and the overlay dies with whatever opened it",
  },
  {
    file: "components/MarkdownPreviewOverlay.tsx",
    kind: "setTimeout/setInterval",
    count: 2,
    verdict: "the 已複製 flash timers; write only this overlay's own state",
  },
  {
    file: "components/MarkdownPreviewOverlay.tsx",
    kind: "addEventListener",
    count: 13,
    verdict:
      "keydown pager, wheel/touch/gesture zoom, resize, pointermove; all removed on unmount, and the overlay unmounts with whatever opened it",
  },
  {
    file: "components/MarkdownPreviewOverlay.tsx",
    kind: "JSX onLoad/onError",
    count: 1,
    verdict: "<img onLoad> sizing; writes only this overlay's own layout",
  },
  // ⚠️ READ THIS BEFORE TRUSTING ANY VERDICT HERE. The five rows above used to
  // point at an exemption reading: `.md-preview` is `position: fixed; inset: 0`
  // with a backdrop, so while it is open nothing can change the peer. It was
  // careful, explicit about its own assumptions, and WRONG — the site routes on
  // the hash, so back/forward changed the peer under the open overlay, and this
  // census had nothing to say about it because a verdict is a human's claim.
  // That has now cost one live bug, so it is a demonstrated limit rather than a
  // theoretical one.

  // ─── The comparison screen, reached from that same overlay (T-59). Both files
  //     enter this walk through `MarkdownPreviewOverlay`, which ChatArea mounts
  //     three times from `mdPreview` — so the exemption the ⚠️ note above
  //     demolishes (a fixed overlay means the peer cannot change) is NOT
  //     available to them either, and neither verdict below leans on it. What
  //     they lean on instead is that a comparison is addressed by `params`, not
  //     by a conversation: the same link opened from any room reads the same
  //     pair, so there is no room's value here for a late commit to carry
  //     across. ───
  {
    file: "components/DiffScreen.tsx",
    kind: ".then/.catch/.finally",
    count: 2,
    verdict:
      "the one `GET /api/diff` read's two arms. The screen CAN move on inside it — the hash route can change the peer under the open overlay — and what stops the write landing wrong is the effect's own `alive` flag, set false by the cleanup and re-asked at the top of BOTH arms (the `.catch` also logs unconditionally, which writes nothing). The deps are the five address fields, so a re-addressed comparison tears the old read down rather than racing it, and the only state either arm writes is this mount's own `pair`/`failed`, derived from those deps and from nothing about the conversation",
  },
  {
    file: "components/DiffShareLinkButton.tsx",
    kind: "await",
    count: 1,
    verdict:
      "`copyDiffShareLink(params)` under the click. The link is minted by the server, so the screen can move on inside this await; the button writes only its OWN two booleans (`copied`/`copyFailed`), and `params` was captured by the click — the clipboard therefore gets the link for the comparison the reader clicked on, which is the gesture's own subject and not a room's state. A commit after the host closed lands in a component React has discarded",
  },
  {
    file: "components/DiffShareLinkButton.tsx",
    kind: "setTimeout/setInterval",
    count: 1,
    verdict:
      "the 2s feedback flash clearing those same two booleans. ⚠️ Said exactly rather than flatteringly: the unmount effect clears `timer.current`, and each `onCopy` clears the previous one, but this timer is ARMED AFTER the await — so an unmount that happens inside the await leaves it running and it fires unguarded. That is harmless for the reason above and only for that reason (it sets two booleans on a discarded mount, carrying no room's value), NOT because it is cleared — it is not",
  },

  // ─── Global / conversation-independent ───
  {
    file: "hooks/sharedServerSettings.ts",
    kind: "addEventListener",
    count: 2,
    verdict: "storage + auth invalidation; global, not per conversation",
  },
  {
    file: "hooks/useIsMobile.ts",
    kind: "addEventListener",
    count: 1,
    verdict: "matchMedia breakpoint; global",
  },
  {
    file: "hooks/useWindowActive.ts",
    kind: "addEventListener",
    count: 3,
    verdict: "focus/blur/visibility; global",
  },
  {
    file: "hooks/useOwnerName.tsx",
    kind: ".then/.catch/.finally",
    count: 4,
    verdict: "the owner's own nickname; global, one provider",
  },
  {
    file: "hooks/useWorkerCodenames.ts",
    kind: ".then/.catch/.finally",
    count: 2,
    verdict:
      "module-level cache keyed by globally-unique `ow-` ids; setTick only asks for a repaint",
  },
  {
    file: "lib/deltaSink.ts",
    kind: "queueMicrotask",
    count: 1,
    verdict:
      "one coalescing decision per burst; the sink is torn down with its subscription",
  },
  {
    file: "lib/escapeLayers.ts",
    kind: "addEventListener",
    count: 1,
    verdict: "the shared Escape stack; layers deregister on unmount",
  },
  {
    file: "lib/hashRoute.ts",
    kind: "addEventListener",
    count: 1,
    verdict:
      "hashchange; it is the thing that CHANGES the conversation, not something that outlives one",
  },
  {
    file: "lib/hashRoute.ts",
    kind: "subscribe",
    count: 1,
    verdict: "route subscribers; same",
  },
  {
    file: "lib/shareLink.ts",
    kind: "await",
    count: 6,
    verdict:
      "returns a value to its caller; commits nothing itself. The count was 2 while this census counted LINES — one line here holds two awaits (R10-5 B5). 🔴 3 → 6 (T-59): the comparison pair `copyDiffShareLink`/`diffShareLinkUrl` was added beside the attachment pair, and it is the SAME shape twice over — `…ShareLinkUrl` awaits the server's mint and absolutizes it (1 each), `copy…` awaits `clipboard.writeText(await …ShareLinkUrl(…))` (2 each). Same verdict for the same reason: every one of the six is in a function that RETURNS or throws, none holds React state or a module table, so a resolve after the screen moved on can only hand a value back to a caller that is itself registered here (MarkdownPreviewOverlay, DiffShareLinkButton)",
  },
  {
    file: "lib/sharedSnapshot.ts",
    kind: ".then/.catch/.finally",
    count: 1,
    verdict:
      "single-flight settings snapshot; global generation, not per conversation",
  },
];

/** MODULE-LEVEL MUTABLE STATE, across the whole walk (T-48, R14-3.1 / R16 D-3).
 *
 * A module-level table outlives every mount, so it is the one place a value can
 * still cross from one conversation into another now that `key={peerId}` takes
 * the component state with the room. Each row says WHAT KEYS IT — and "keyed by
 * peer" is the only answer that makes a per-conversation table safe. The
 * deleted `liveComposers` (a second peer-keyed table, in `ChatArea.tsx`, not in
 * the store) is the shape this exists to catch coming back — under any NAME, and
 * in any file the chat surface imports.
 *
 * 🔴 "UNDER ANY NAME" IS THE TRUE HALF; "IN ANY SHAPE" WAS NEVER CLAIMED AND
 * MUST NOT BE READ IN (R17 A-7). Renaming and moving are closed: the rule is
 * about declarations and write-throughs, not about spellings. The SHAPE of the
 * declaration is a different axis and it has a ceiling, written out on
 * scanModuleState. This sentence used to run the two together, and sixty lines
 * below it the ceiling contradicted it — two sentences in one file disagreeing
 * about the same guarantee, with the confident one on top.
 *
 * WHAT COUNTS AS STATE is decided by the AST, not by a line pattern — see
 * `scanModuleState` for the three clauses and, more importantly, for the shapes
 * that still get past them. The regex this replaced was anchored on
 * `(\s*)\s*;?\s*$`, so an end-of-line comment walked through the exact shape
 * the census advertised catching, and array/object literals were not in its
 * grammar at all — which is why `escapeLayers.ts`'s `layers` sat in the walk,
 * beside a registered sibling, unseen.
 *
 * 🔴 THE POPULATION INCLUDES `src/api` (R16 D-3). It did not, by inheritance
 * from the async census's exclusion — and that exclusion's stated reason is
 * about await COUNTS, which says nothing about whether one room's value can
 * reach another. The eight rows below from `api/http.ts` are the answer to
 * that: the SSE transport is exactly where a per-room cache would be put. */
const MODULE_STATE = [
  {
    file: "api/http.ts",
    name: "sseSubscribers",
    verdict:
      "the set of delta sinks on the ONE app-wide EventSource; a subscriber is a callback that its own hook removes on unmount, and no room's data is stored here — only who to hand a delta to",
  },
  {
    file: "api/http.ts",
    name: "sseSource",
    verdict:
      "the single live EventSource, or null; a connection is not per conversation and holds no room's value",
  },
  {
    file: "api/http.ts",
    name: "sseVisibilityHandler",
    verdict:
      "the one visibilitychange listener paired with that connection, kept so it can be removed again; global",
  },
  {
    file: "api/http.ts",
    name: "sseState",
    verdict:
      "the connection's own state machine (idle/connecting/open/…); describes the socket, not any room",
  },
  {
    file: "api/http.ts",
    name: "sseStateSubscribers",
    verdict:
      "repaint callbacks for that connection banner; each unsubscribes on unmount, and the value delivered is the socket's state",
  },
  {
    file: "api/http.ts",
    name: "sseRetryTimer",
    verdict:
      "the reconnect backoff timer handle; one connection, one timer, no room in it",
  },
  {
    file: "api/http.ts",
    name: "sseRetryAttempt",
    verdict:
      "the backoff index for that timer; a counter, not a value any room can read back",
  },
  {
    file: "api/http.ts",
    name: "sseGapPending",
    verdict:
      "one boolean saying a resync is owed after a dropped connection; it triggers a refetch in every mounted hook rather than carrying data itself",
  },
  {
    file: "api/errorCodes.ts",
    name: "ERROR_CODE_VOCABULARY",
    verdict:
      "the closed error-code vocabulary, built once from the checked-in spec JSON and only ever asked `.has(…)`; a lookup table, identical for every room",
  },
  {
    file: "hooks/useAttachmentStaging.ts",
    name: "pendingAttachmentSeq",
    verdict:
      "a monotonic key mint for staged rows; carries no per-conversation meaning and is never read back",
  },
  {
    file: "hooks/useAttachmentStaging.ts",
    name: "NO_ROWS",
    verdict:
      "the one empty list handed to a caller with no peer, so `useSyncExternalStore` sees a stable snapshot; never written to, and a peerless composer has no room to leak into",
  },
  {
    file: "hooks/useWorkerCodenames.ts",
    name: "cache",
    verdict:
      "keyed by the globally-unique `ow-` worker id, which names one worker in every room alike",
  },
  {
    file: "hooks/useWorkerCodenames.ts",
    name: "inflight",
    verdict:
      "the same `ow-` ids, de-duplicating requests; not per conversation",
  },
  {
    file: "hooks/useWorkerCodenames.ts",
    name: "listeners",
    verdict:
      "repaint callbacks for that global cache; each unsubscribes on unmount",
  },
  {
    file: "components/Markdown.tsx",
    name: "BARE_URL_RE",
    verdict:
      "🔴 A `g`-FLAGGED REGEX, so it really does carry mutable state across calls — `lastIndex` — and that is why it is here rather than filed as a constant (clause 2 sees it: `BARE_URL_RE.lastIndex = 0`). What keys it: NOTHING, and it needs no key, because the value it can carry between calls is an offset into a string, not a room's content — there is no conversation in a scan cursor, and the pattern itself is the same in every room. Nor can the offset survive one call: `autolinkBareUrls` is its ONE reader (measured: `BARE_URL_RE` appears in no other file and in no other function) and it resets `lastIndex = 0` as its first statement, so a residue left by an earlier scan — or by a `return`/throw out of the middle of the loop — is overwritten before the next `exec`. The scan is synchronous and non-reentrant: the loop body calls only `trimUrlTail` (pure) and `createElement`, never back into itself, so no second string can interleave with a first",
  },
  {
    file: "lib/chatDraftStore.ts",
    name: "drafts",
    verdict:
      "keyed by PEER — the one table that survives a room switch on purpose, because a draft is what the owner has composed and not yet sent",
  },
  {
    file: "lib/chatDraftStore.ts",
    name: "attachErrors",
    verdict:
      "keyed by PEER, and dropped when the last chat surface closes its scope (R14-2.1 / R16 D-2) so a refusal cannot outlive the surface that raised it",
  },
  {
    file: "lib/chatDraftStore.ts",
    name: "listeners",
    verdict: "keyed by PEER; a write notifies only that room's subscribers",
  },
  {
    file: "lib/chatDraftStore.ts",
    name: "attachErrorScopes",
    verdict:
      "how many chat surfaces hold the notice scope open (R16 D-2); a count, so no room's value can be stashed in it — the peers a close will drop live in that close's own closure",
  },
  {
    file: "lib/chatDraftStore.ts",
    name: "scopeEpoch",
    verdict:
      "which scope-close a deferred sweep belongs to, so StrictMode's synchronous remount cancels the sweep its own fake cleanup queued (R16 D-2); a counter, carrying no peer",
  },
  {
    file: "lib/escapeLayers.ts",
    name: "layers",
    verdict:
      "🔴 REGISTERED BY R16 D-3, having been in the walk and invisible to the regex census the whole time. The Esc stack: one entry per dismissible surface, pushed by that surface's effect and spliced out by the same effect's cleanup. Not keyed at all and deliberately so — the owner of a key press is decided by DOM CONTAINMENT at dispatch time, not by any id — and it holds callbacks, never a room's data, so a stale entry is impossible rather than merely unlikely (a forced unmount still runs the cleanup, which is the only release path)",
  },
  {
    file: "lib/escapeLayers.ts",
    name: "listening",
    verdict:
      "one boolean saying whether the shared keydown listener is attached; layers deregister on unmount",
  },
  {
    file: "lib/sharedSnapshot.ts",
    name: "registry",
    verdict:
      "the set of global settings snapshots to reset on auth change; global by definition, not per conversation",
  },
  {
    file: "lib/themeBundleCore.ts",
    name: "THEME_TOKEN_SET",
    verdict:
      "a closed allowlist built once from the generated THEME_COLOR_TOKENS and only ever asked `.has(…)`; a lookup table, identical for every room",
  },
  {
    file: "lib/themeBundleCore.ts",
    name: "THEME_FONT_TOKEN_SET",
    verdict:
      "the same, over THEME_FONT_TOKENS; read-only membership, no room in it",
  },
  {
    file: "lib/themeBundleCore.ts",
    name: "SAFE_FONT_STACK_SET",
    verdict:
      "the same, over SAFE_FONT_FAMILIES' stacks; the font-injection allowlist, read-only",
  },
  {
    file: "lib/themeWording.ts",
    name: "MESSAGE_KEY_SET",
    verdict:
      "a closed allowlist built once from the generated MESSAGE_KEYS and only ever asked `.has(…)`; a lookup table, identical for every room",
  },
];

/** THE ONE FILE THAT MAY HOLD THE THREAD (T-48).
 *
 * 🔴 WHY IT IS A LINT AND NOT A COMMENT. The chat thread's reply cards are
 * fetched separately from and later than its messages, so a WAITING card above a
 * scroll target grows after the fact and pushes the target down (measured +254px
 * at 1280 wide; NOT 0 at 390 — that half of this sentence was wrong until
 * 2026-09-04: visual-guards/chat-jump-card-shift.ct.spec.tsx measures +200px
 * there and 08_unread_jump +215.78px in the browser. Only a card ABOVE THE FOLD
 * is absorbed by `overflow-anchor: auto`, which is a placement, not a width). The
 * fix is that every commit must hold those cards BEFORE the rows reach the view
 * — and the way that fix fails is N hand-written `await prefetch(...)` calls at
 * N commit points, one of which the next reader forgets. That failure has a
 * measured record on this very ticket: four silent regressions in one night,
 * every one with a green suite.
 *
 * So the raw `useState<Thread>` setter lives in `lib/threadCommit.ts`'s closure
 * and does not come out. This rule is what stops a second home for it growing —
 * the same call `conversationLatches` made when it took the mutable fields out
 * of the latch type, and enforced the same way: not by asking whether a call
 * site is careful, but by making the careless form unwritable.
 *
 * 🟠 "UNWRITABLE" IS A CLAIM WITH A BOUNDARY, AND IT IS WRITTEN DOWN. The rule
 * asks the TYPE CHECKER whether a `useState`/`useReducer`/`useRef` holds
 * something with `messages: ChatMessage[]` in it (or a React setter for such a
 * thing), so no spelling — no annotation at all, a renamed import, an alias, an
 * inferred object literal — gets past it. What DOES get past it is a thread
 * whose messages are not `ChatMessage`: a locally redeclared twin of that
 * interface, or `unknown[]`. See `declaresThreadState` for why the anchor is
 * there and not somewhere looser.
 *
 * It runs over the WALK, so a new hook the chat surface starts calling is
 * covered without anybody adding it to a list. */
const THREAD_STATE_OWNER = "lib/threadCommit.ts";

/** Does this file declare React state over the chat thread, or a setter for one?
 *
 * 🔴 IT ASKS THE TYPE CHECKER, NOT THE TEXT (T-48, independent review F2). This
 * used to match on the SPELLING of a type annotation: a `useState`/`useReducer`/
 * `useRef` whose type argument text matched `/\bThread\b/`, or a binding
 * annotated `SetStateAction<…Thread…>`. All three shapes therefore needed
 * somebody to have WRITTEN the word `Thread`, and the screen does not consume
 * that name — it consumes `messages: ChatMessage[]`. So the rule's own claim
 * ("the careless form is unwritable") was only true of the annotated form:
 *
 *     const EMPTY2 = { messages: [] as ChatMessage[], hasMore: true,
 *                      gapSuspected: false, hasNewer: false };
 *     const [xthread, setXThread] = useState(EMPTY2);
 *
 * was a second thread state with a second setter, and both this census and
 * `tsc --noEmit` passed it. (Measured, in `useChat.ts`, before this rewrite.)
 *
 * What it asks now is a question about the TYPE, which no spelling can dodge:
 *   · `carriesThread` — does this type structurally have a property `messages`
 *     whose type is an array of the `ChatMessage` INTERFACE ITSELF (type
 *     identity against `api/adapter.ts`, not a name match)? An inferred object
 *     literal, a type alias, an import rename, `typeof EMPTY2` and a hand-
 *     written `{ messages: ChatMessage[]; … }` are all the same type here.
 *   · …or is it a REACT SETTER for such a type — a call signature whose first
 *     parameter is a union of that shape and of a function returning it, which
 *     is what `Dispatch<SetStateAction<T>>` IS structurally. Written that
 *     narrowly on purpose: a plain updater `(prev: Thread) => Thread` is NOT a
 *     union and so does not fire, which keeps every `commit(seq, next)` call
 *     site in `useChat` out of the rule.
 *
 * 🟠 WHAT IT STILL DOES NOT CATCH, SAID PLAINLY — and this list is what the
 * failure message is held to, so keep the two in step. Three residues, each an
 * anchor the rule cannot give up without firing on everything:
 *   · the ELEMENT TYPE. A thread whose messages are held under a locally
 *     declared twin of `ChatMessage`, or under `unknown[]`/`any[]`, is
 *     invisible — `ChatMessage` from `api/adapter` is where identity is
 *     anchored, and anything the chat surface can actually render comes from
 *     there.
 *   · the PROPERTY NAME. `{ rows: ChatMessage[]; … }` passes (measured, review
 *     F-D case D). The rule asks for a property literally named `messages`;
 *     firing on ANY property holding `ChatMessage[]` would redden every list
 *     view in the tree.
 *   · the MODULE ANCHOR on the callee. `reactHookOf` recognises React's hooks
 *     by the specifier `"react"` and the exported name, not by symbol identity
 *     against `@types/react` — see there for why. A hook re-exported through a
 *     local barrel is out of reach.
 *
 * ⚠️ AND ONE WAY IT DIES LOUDLY RATHER THAN SILENTLY: the anchor is looked up
 * as an INTERFACE declaration in `api/adapter.ts`. Refactoring `ChatMessage`
 * into a `type` alias resolves nothing, and the vacuity check below then fails
 * the run — noisy, not silent, but it is the next reader's first surprise.
 *
 * Over-approximating is otherwise the safe direction — a false positive is one
 * line of argument in review, a false negative is the defect back. */
function threadTypeProbe() {
  if (threadProbe) return threadProbe;
  // ONE program over the walked files. React's own types need not resolve (a
  // temp-tree run under ASYNC_LANDING_SRC has no node_modules beside it) — the
  // rule reads the type ARGUMENT or the type of the INITIAL VALUE, both of
  // which are the caller's own code and resolve through relative imports.
  const program = ts.createProgram(
    files,
    {
      target: ts.ScriptTarget.Latest,
      jsx: ts.JsxEmit.Preserve,
      module: ts.ModuleKind.ESNext,
      moduleResolution: ts.ModuleResolutionKind.Bundler,
      skipLibCheck: true,
      noEmit: true,
      strict: true,
    },
  );
  const checker = program.getTypeChecker();
  const adapter = program.getSourceFile(join(SRC, "api", "adapter.ts"));
  let chatMessage = null;
  for (const st of adapter?.statements ?? []) {
    if (ts.isInterfaceDeclaration(st) && st.name.text === "ChatMessage") {
      chatMessage = checker.getTypeAtLocation(st);
    }
  }
  threadProbe = { program, checker, chatMessage };
  return threadProbe;
}
let threadProbe = null;

const REACT_HOOKS = /^use(State|Reducer|Ref)$/;
const REACT_MODULE = "react";

/** The module specifier the import that DECLARED this binding names, or null. */
function importedFrom(decl) {
  const imp = ts.isImportSpecifier(decl)
    ? decl.parent.parent.parent
    : ts.isNamespaceImport(decl)
      ? decl.parent.parent
      : ts.isImportClause(decl)
        ? decl.parent
        : null;
  const spec = imp?.moduleSpecifier;
  return spec && ts.isStringLiteral(spec) ? spec.text : null;
}

/** WHICH React hook does this callee actually name — resolved, not spelled?
 *
 * 🔴 THE OTHER HALF OF RULE 7 USED TO BE A LITERAL IDENTIFIER TEST (T-48,
 * independent review F-D). `/^use(State|Reducer|Ref)$/.test(callee.text)` while
 * the TYPE half asked the checker, and the rule's own message promised it fires
 * "however the shape is spelled". It did not. Measured on a temp tree, with the
 * author's own un-annotated mutant as a positive control (rc=1, rule 7 fires):
 *
 *     React.useState(EMPTY2)                          rc=0  passed
 *     const us = useState; us(EMPTY2)                 rc=0  passed
 *     import { useState as useStore } … useStore(…)   rc=0  passed
 *
 * All three are ordinary spellings, and all three were a second thread setter
 * that this census called clean. So the callee is now RESOLVED to the binding
 * it came from: a named import (through its `propertyName`, so a rename is the
 * export's own name again), a namespace or default import of `react`, and a
 * local alias chased through its initialiser.
 *
 * 🟠 WHAT THIS IS NOT. It is not symbol identity against React's own
 * declarations — that would need `@types/react` in the program, and this script
 * has to run over a temp tree with no `node_modules` beside it (see
 * `threadTypeProbe`). The anchor is therefore the MODULE SPECIFIER `"react"`
 * plus the EXPORTED name, both read off the import that declared the binding.
 * A hook re-exported through a local barrel is out of reach that way, and so is
 * a call whose callee resolves to nothing at all — so the old literal name test
 * stays on as a floor, ORed with the resolution rather than reached only when
 * nothing resolved, because losing coverage is the one direction this rule may
 * not move in. A barrel import resolves to a declaration, just not one of the
 * recognised ones, and it was the `decls.length === 0` shape of this floor that
 * let `export { useState } from "react"` through (measured: old script rc=1,
 * new rc=0 on the same tree). */
function reactHookOf(callee, checker, depth = 0) {
  if (depth > 8) return null;
  if (ts.isPropertyAccessExpression(callee)) {
    // `React.useState(…)` — the property name is React's export name; what has
    // to be resolved is that `React` really is the react module.
    if (!REACT_HOOKS.test(callee.name.text)) return null;
    const obj = unwrap(callee.expression);
    if (!ts.isIdentifier(obj)) return null;
    const decls = checker.getSymbolAtLocation(obj)?.getDeclarations?.() ?? [];
    return decls.some(
      (d) =>
        (ts.isNamespaceImport(d) || ts.isImportClause(d)) &&
        importedFrom(d) === REACT_MODULE,
    )
      ? callee.name.text
      : null;
  }
  if (!ts.isIdentifier(callee)) return null;
  const decls = checker.getSymbolAtLocation(callee)?.getDeclarations?.() ?? [];
  for (const d of decls) {
    if (ts.isImportSpecifier(d)) {
      // `import { useState as useStore }` — `propertyName` is `useState`.
      const exported = (d.propertyName ?? d.name).text;
      if (importedFrom(d) === REACT_MODULE && REACT_HOOKS.test(exported))
        return exported;
    }
    if (ts.isVariableDeclaration(d) && d.initializer) {
      // `const us = useState` / `const us = React.useState` — chase it.
      const via = reactHookOf(unwrap(d.initializer), checker, depth + 1);
      if (via) return via;
    }
  }
  // THE FLOOR, and it is an OR — not a fallback for `decls.length === 0`. A
  // callee that resolves to a declaration this function does not recognise (a
  // local re-export barrel, a global, an unresolvable module) still fires on
  // its NAME, because that is the set the rule covered before it learned to
  // resolve anything, and losing coverage is the one direction it may not move.
  return REACT_HOOKS.test(callee.text) ? callee.text : null;
}

/** How many callees rule 7 resolved to a React hook over the whole walk. Zero
 * means the resolution above matched nothing anywhere, i.e. the rule is looking
 * at no call sites at all — reported rather than passed. */
let reactHookCallsSeen = 0;

function declaresThreadState(file) {
  const { program, checker, chatMessage } = threadTypeProbe();
  const source = program.getSourceFile(file);
  if (!source || !chatMessage) return [];

  const isChatMessageArray = (t) => {
    if (!t) return false;
    const el = checker.getElementTypeOfArrayType?.(t);
    return !!el && el === chatMessage;
  };
  /** The thread's own shape: `messages: ChatMessage[]`, however spelled. */
  const isThreadShape = (t) => {
    if (!t || typeof t.getProperty !== "function") return false;
    const p = t.getProperty("messages");
    if (!p) return false;
    return isChatMessageArray(checker.getTypeOfSymbolAtLocation(p, source));
  };
  /** `Dispatch<SetStateAction<thread>>`, structurally: one call signature whose
   * first parameter is a union holding BOTH the value and an updater for it. */
  const isThreadSetter = (t) => {
    if (!t || typeof t.getCallSignatures !== "function") return false;
    return t.getCallSignatures().some((sig) => {
      const p = sig.getParameters()[0];
      if (!p) return false;
      const pt = checker.getTypeOfSymbolAtLocation(p, source);
      if (!pt.isUnion?.()) return false;
      const parts = pt.types;
      return (
        parts.some(isThreadShape) &&
        parts.some((c) =>
          (c.getCallSignatures?.() ?? []).some((s) =>
            isThreadShape(checker.getReturnTypeOfSignature(s)),
          ),
        )
      );
    });
  };
  const carriesThread = (t) => isThreadShape(t) || isThreadSetter(t);

  const hits = [];
  eachNode(source, (n) => {
    const hook = ts.isCallExpression(n)
      ? reactHookOf(unwrap(n.expression), checker)
      : null;
    if (hook) {
      reactHookCallsSeen += 1;
      const types = [
        ...(n.typeArguments ?? []).map((a) => checker.getTypeFromTypeNode(a)),
        ...n.arguments.map((a) => checker.getTypeAtLocation(a)),
      ];
      if (types.some(carriesThread)) {
        hits.push(`${hook}(…) over a { messages: ChatMessage[] } shape`);
      }
    }
    if (
      (ts.isVariableDeclaration(n) ||
        ts.isParameter(n) ||
        ts.isPropertySignature(n)) &&
      n.type &&
      isThreadSetter(checker.getTypeFromTypeNode(n.type))
    ) {
      hits.push(`a React setter for that shape, stashed in a binding`);
    }
  });
  return [...new Set(hits)];
}

/** The ONE caller `useQuotedMessageOverlay` is allowed to have (T-48, R14-1.6).
 * The hook dropped its own visit stamp because `ChatArea` is keyed, so its
 * state dies with the room. That reasoning is about the CALLER, not the hook: a
 * second caller mounted under a card id (`ChatReplyCard`, `ReplyComposer`) does
 * not unmount on a room switch, and would paint room A's message full-screen
 * over room B. Adding a caller here is a decision to re-check that, not a
 * formality. */
const QUOTED_OVERLAY_CALLERS = ["components/ChatArea.tsx"];

// ── THE AST LAYER (T-48, R16 D-3) ────────────────────────────────────────────
// The module-state and caller censuses used to be REGEXES over lines, and the
// sixteenth review walked through both of them five different ways: an
// end-of-line comment defeated the `\(\s*\)\s*;?\s*$` anchor on the very shape
// the census advertised; array and object literals were not in the grammar at
// all; a renamed import (`import { X as Y }`) defeated the caller substring;
// and a line break before `(` did too. Every one of those is a LEXICAL accident
// — the parser has no such accidents, and `typescript` is already a dependency
// of this repo. So both censuses now read the syntax tree.
//
// 🔴 WHAT THIS DOES AND DOES NOT BUY. It closes the whole class of "same
// meaning, different spelling": comments, whitespace, line breaks, type
// arguments, `as const`, renamed and namespaced imports, re-exports. It does
// NOT make the rule below omniscient — see the note on scanModuleState for the
// shapes that still get through and why the boundary is where it is.
const astCache = new Map();
function ast(file) {
  let a = astCache.get(file);
  if (!a) {
    a = ts.createSourceFile(
      file,
      readFileSync(file, "utf8"),
      ts.ScriptTarget.Latest,
      /* setParentNodes */ true,
      /\.tsx$/.test(file) ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
    );
    astCache.set(file, a);
  }
  return a;
}

function eachNode(node, fn) {
  fn(node);
  ts.forEachChild(node, (c) => eachNode(c, fn));
}

const resolveSpec = (fromFile, spec) => {
  if (!spec.startsWith(".")) return null;
  const base = join(dirname(fromFile), spec);
  for (const c of [
    base,
    `${base}.ts`,
    `${base}.tsx`,
    join(base, "index.ts"),
    join(base, "index.tsx"),
  ]) {
    // `existsSync` alone matched the DIRECTORY `./foo` for `import "./foo"`,
    // which only ever stayed harmless because the scope filter dropped it
    // before anything tried to read it (R16 D-3).
    if (existsSync(c) && statSync(c).isFile() && !/\.(css|json|svg)$/.test(c))
      return c;
  }
  return null;
};

/** Path aliases the walk cannot follow. `resolveSpec` returns null for anything
 * not starting with `.`, which is correct for `react` and wrong for `@/lib/x`.
 * No alias is configured today, so the walk is complete — but the day someone
 * adds one, files would drop out of the population SILENTLY, which is the exact
 * failure mode (R10-5 B1/B2/B3) this census has already been bitten by twice.
 * A loud stop is the cheap half of the fix; teaching `resolveSpec` the mapping
 * is the other half, and it belongs to whoever adds the alias. */
function unfollowableAliases() {
  const tsconfig = join(dirname(SRC), "tsconfig.json");
  if (!existsSync(tsconfig)) return [];
  const { config } = ts.parseConfigFileTextToJson(
    tsconfig,
    readFileSync(tsconfig, "utf8"),
  );
  const co = config?.compilerOptions ?? {};
  const names = Object.keys(co.paths ?? {});
  if (co.baseUrl !== undefined) names.push(`baseUrl: ${co.baseUrl}`);
  return names;
}

/** Every module `file` imports, in EVERY syntax — static, `export … from`,
 * side-effect, dynamic `import()`, `require()`. `ts.preProcessFile` is the
 * compiler's own answer to this question, so a syntax nobody here thought of is
 * still found. */
function importedSpecs(file) {
  return ts
    .preProcessFile(readFileSync(file, "utf8"), true, true)
    .importedFiles.map((r) => r.fileName);
}

/** The population: everything reachable from ChatArea through `scope`. */
function walkFromChatArea(scope) {
  const seen = new Set();
  const queue = [ENTRY];
  while (queue.length > 0) {
    const file = queue.shift();
    if (seen.has(file)) continue;
    seen.add(file);
    for (const spec of importedSpecs(file)) {
      const r = resolveSpec(file, spec);
      if (r && !r.includes(".test.") && scope.test(rel(r))) queue.push(r);
    }
  }
  return [...seen].sort();
}

// ── MODULE-LEVEL MUTABLE STATE, BY AST ───────────────────────────────────────
const MUTATING_METHODS = new Set([
  "set",
  "add",
  "delete",
  "clear",
  "push",
  "pop",
  "shift",
  "unshift",
  "splice",
  "sort",
  "reverse",
  "fill",
  "copyWithin",
]);
const CONTAINER_CTORS = new Set([
  "Map",
  "Set",
  "WeakMap",
  "WeakSet",
  "Array",
  "Object",
]);

const unwrap = (e) => {
  while (
    e &&
    (ts.isAsExpression(e) ||
      ts.isParenthesizedExpression(e) ||
      ts.isSatisfiesExpression(e) ||
      ts.isTypeAssertionExpression(e) ||
      ts.isNonNullExpression(e))
  ) {
    e = e.expression;
  }
  return e;
};

/** The name a write lands ON, dug out from under whatever was wrapped around it.
 *
 * 🔴 THE WRAPPERS ARE NOT DECORATION — one of them was a live hole (R17 A-7).
 * `roomBuf.get(peer)!.push(v)` mutates a per-room table and the census passed it,
 * because the receiver of `.push` is a CallExpression under a `!` rather than a
 * bare identifier. That is the FIFTH evasion of this rule and it was not on the
 * ceiling list, which made the list itself misleading: a reader would have taken
 * the four named shapes for the whole boundary.
 *
 * So this walks calls, non-null assertions, parens and casts as well as property
 * and element access. It OVER-approximates on purpose — `makeTable().rows.push(x)`
 * is attributed to `makeTable` — and over-approximating can only ever ADD a row
 * to the register, never hide one (see mutatedNames). */
const rootIdentifier = (e) => {
  for (;;) {
    if (ts.isPropertyAccessExpression(e) || ts.isElementAccessExpression(e))
      e = e.expression;
    else if (ts.isCallExpression(e)) e = e.expression;
    else if (ts.isNonNullExpression(e)) e = e.expression;
    else if (ts.isParenthesizedExpression(e)) e = e.expression;
    else if (ts.isAsExpression(e) || ts.isTypeAssertionExpression(e))
      e = e.expression;
    else break;
  }
  return ts.isIdentifier(e) ? e.text : null;
};

/** Every identifier WRITTEN THROUGH anywhere in the population: `x.set(…)`,
 * `x.push(…)`, `x[k] = …`, `x.k += …`, `delete x[k]`. By NAME rather than by
 * binding, deliberately: an exported table mutated by its importer must count,
 * and over-approximating (two files, same name) only ever adds a row to the
 * register — it can never hide one. */
function mutatedNames(files) {
  const out = new Set();
  for (const file of files) {
    eachNode(ast(file), (n) => {
      if (
        ts.isCallExpression(n) &&
        ts.isPropertyAccessExpression(n.expression) &&
        MUTATING_METHODS.has(n.expression.name.text)
      ) {
        const r = rootIdentifier(n.expression.expression);
        if (r) out.add(r);
      }
      if (
        ts.isBinaryExpression(n) &&
        n.operatorToken.kind >= ts.SyntaxKind.FirstAssignment &&
        n.operatorToken.kind <= ts.SyntaxKind.LastAssignment &&
        !ts.isIdentifier(n.left)
      ) {
        const r = rootIdentifier(n.left);
        if (r) out.add(r);
      }
      if (ts.isDeleteExpression(n)) {
        const r = rootIdentifier(n.expression);
        if (r) out.add(r);
      }
    });
  }
  return out;
}

/** Top-level consts across the population, for resolving `new Set(TOKENS)`. */
function topLevelInitializers(files) {
  const out = new Map();
  for (const file of files) {
    for (const st of ast(file).statements) {
      if (!ts.isVariableStatement(st)) continue;
      for (const d of st.declarationList.declarations) {
        if (ts.isIdentifier(d.name)) out.set(d.name.text, d.initializer);
      }
    }
  }
  return out;
}

/** Is this initializer STATICALLY, VISIBLY non-empty — i.e. does the file make
 * it obvious that the container is a fixed lookup table rather than something
 * to be filled in later? Only a literal with elements, or an identifier that
 * resolves to one, qualifies. Anything clever (`new Map(Object.entries({}))`,
 * a spread, a call) does NOT, and falls to the state side: the default is
 * "register it and say what keys it", not "assume it is a constant". */
function provablyNonEmpty(init, consts, depth = 0) {
  const e = unwrap(init);
  if (!e || depth > 2) return false;
  if (ts.isArrayLiteralExpression(e)) {
    return (
      e.elements.length > 0 && e.elements.every((x) => !ts.isSpreadElement(x))
    );
  }
  if (ts.isObjectLiteralExpression(e)) {
    return (
      e.properties.length > 0 &&
      e.properties.every((x) => !ts.isSpreadAssignment(x))
    );
  }
  if (ts.isIdentifier(e) && consts.has(e.text)) {
    return provablyNonEmpty(consts.get(e.text), consts, depth + 1);
  }
  return false;
}

const containerArgs = (init) => {
  const e = unwrap(init);
  if (!e) return null;
  if (ts.isArrayLiteralExpression(e) || ts.isObjectLiteralExpression(e))
    return [e];
  if (
    ts.isNewExpression(e) &&
    ts.isIdentifier(e.expression) &&
    CONTAINER_CTORS.has(e.expression.text)
  ) {
    return [...(e.arguments ?? [])];
  }
  return null;
};

/** Top-level mutable state declared in `file`, by AST.
 *
 * 🔴 THE RULE, AND ITS CEILING (T-48, R16 D-3). A binding is module state when:
 *
 *   1. it is a top-level `let`/`var` — it can be reassigned, full stop; or
 *   2. it is a top-level `const` that is WRITTEN THROUGH somewhere in the
 *      population (`.set`/`.push`/`x[k] =`/`delete x[k]`); or
 *   3. it is a top-level `const` bound to a mutable container (array literal,
 *      object literal, `new Map/Set/WeakMap/WeakSet/Array/Object`) that is not
 *      provably non-empty at construction.
 *
 * Clause 2 is the load-bearing one and it is SOUND for the question asked: a
 * container that is never written to after module init cannot carry a value
 * from one room to another — every room sees exactly what the module literally
 * put there at load time. It is also what finally sees
 * `const layers: Layer[] = []` in `escapeLayers.ts`, an instance that was
 * sitting in the walk, next to a sibling that WAS registered, for the whole life
 * of the regex version. Clause 3 is a cheap extra that catches a table declared
 * before it is wired up — an empty container is a promise to write to it — which
 * is the shape the sixteenth review's mutants took.
 *
 * WHAT STILL GETS THROUGH, said plainly rather than discovered later: a table
 * obtained from a factory and never touched syntactically here
 * (`const t = makeStore();` then `t` handed to something that fills it), a class
 * static field, and state stashed on an imported object. A module-level regex's
 * `lastIndex` used to be named here too and it is only HALF out of reach: the
 * one in the walk (`Markdown.tsx`'s `BARE_URL_RE`) assigns `lastIndex`
 * explicitly, which is clause 2 and is why it carries a row — a `g` regex that
 * only ever calls `.exec` would still walk past. Those are not closed by adding a fourth clause of the same
 * kind — the honest boundary is "declared here, or written through here", and
 * anything outside it is a REVIEW's job, not this script's.
 *
 * 🔴 THIS LIST WAS WRONG ONCE, AND THE WAY IT WAS WRONG IS THE WARNING (R17 A-7).
 * It named four shapes and a fifth was live: `roomBuf.get(peer)!.push(v)` — seed
 * the table, then append into a room's slot. It is now CLOSED (rootIdentifier
 * digs through calls, `!`, parens and casts, and the selftest pins it), but the
 * lesson is not the shape, it is that a list of exceptions reads as a boundary.
 * A reader takes "here are the four" for "there are four". If you find a sixth,
 * the list was never the boundary — "declared here, or written through here" is.
 *
 * 🔑 AND THE REASON IT STAYED OPEN WAS A MEASUREMENT NOBODY RE-TOOK. Widening
 * this rule was refused on the grounds that a shape-only prototype produced 82
 * register rows, and an unread register is the failure mode. That number was
 * real — for a DIFFERENT rule. It came from matching on shape alone (every
 * top-level container, written to or not); this widening only changes WHERE a
 * write-through is allowed to point, so the population it admits is the same
 * one. Measured after the change: still 26 rows. ⇒ An estimate taken against
 * one design does not transfer to another that merely sounds similar, and the
 * cost of re-measuring here was one command. */
function scanModuleState(file, mutated, consts) {
  const names = [];
  for (const st of ast(file).statements) {
    if (!ts.isVariableStatement(st)) continue;
    const isConst = (st.declarationList.flags & ts.NodeFlags.Const) !== 0;
    for (const d of st.declarationList.declarations) {
      if (!ts.isIdentifier(d.name)) continue;
      if (!isConst || mutated.has(d.name.text)) {
        names.push(d.name.text);
        continue;
      }
      const args = containerArgs(d.initializer);
      if (
        args &&
        !(args.length > 0 && args.every((a) => provablyNonEmpty(a, consts)))
      ) {
        names.push(d.name.text);
      }
    }
  }
  return names;
}

// ── useQuotedMessageOverlay's CALLERS, BY AST ────────────────────────────────
const OVERLAY_FILE = join(SRC, "hooks", "useQuotedMessageOverlay.tsx");
const OVERLAY_EXPORT = "useQuotedMessageOverlay";

/** For every file: the local names that ARE the hook, and the namespace
 * bindings through which it can be reached. Renames (`import { X as Y }`),
 * namespace imports (`import * as Q`) and re-export chains (`export { X as Y }
 * from "…"`, `export * from "…"`) all resolve here, because two of the three
 * were ways past the old substring match and the third only worked by accident.
 * A fixpoint, because a re-export chain can be any length. */
/** Joins `<alias> <module>` in the namespace set. It is U+001F UNIT SEPARATOR
 * and NOT a NUL: two literal NUL bytes used to live in this file, and a NUL
 * makes `grep` treat the whole file as binary and print NOTHING — not "binary
 * file matches", nothing at all. Every grep-based CI check and every human
 * grepping this file got a silent, confident "no hits" (pre-existing; origin/main
 * has 0, c7b6a7d8 already had 2). U+001F cannot occur in an identifier or a
 * module specifier either, and greps fine. */
const NS_SEP = "\x1f";

function overlayBindings(files) {
  const exportedAs = new Map([[OVERLAY_FILE, new Set([OVERLAY_EXPORT])]]);
  const locals = new Map();
  const namespaces = new Map();
  const get = (m, k) => {
    let v = m.get(k);
    if (!v) m.set(k, (v = new Set()));
    return v;
  };
  for (let pass = 0; pass <= files.length + 1; pass += 1) {
    let grew = false;
    const note = (set, name) => {
      if (!set.has(name)) {
        set.add(name);
        grew = true;
      }
    };
    for (const file of files) {
      for (const st of ast(file).statements) {
        const spec = st.moduleSpecifier;
        const target =
          spec && ts.isStringLiteral(spec)
            ? resolveSpec(file, spec.text)
            : null;
        if (ts.isImportDeclaration(st) && target) {
          const b = st.importClause?.namedBindings;
          if (b && ts.isNamespaceImport(b)) {
            note(get(namespaces, file), `${b.name.text}${NS_SEP}${target}`);
          }
          if (b && ts.isNamedImports(b)) {
            for (const el of b.elements) {
              const source = (el.propertyName ?? el.name).text;
              if (exportedAs.get(target)?.has(source))
                note(get(locals, file), el.name.text);
            }
          }
        }
        if (ts.isExportDeclaration(st)) {
          if (target && !st.exportClause) {
            for (const n of exportedAs.get(target) ?? [])
              note(get(exportedAs, file), n);
          }
          if (st.exportClause && ts.isNamedExports(st.exportClause)) {
            for (const el of st.exportClause.elements) {
              const source = (el.propertyName ?? el.name).text;
              const known = target
                ? exportedAs.get(target)?.has(source)
                : locals.get(file)?.has(source) ||
                  (file === OVERLAY_FILE && source === OVERLAY_EXPORT);
              if (known) note(get(exportedAs, file), el.name.text);
            }
          }
        }
      }
    }
    if (!grew) break;
  }
  return { locals, namespaces, exportedAs };
}

function overlayCallers(files) {
  const { locals, namespaces, exportedAs } = overlayBindings(files);
  const callers = [];
  for (const file of files) {
    if (file === OVERLAY_FILE) continue;
    const local = locals.get(file) ?? new Set();
    const ns = [...(namespaces.get(file) ?? [])].map((s) => s.split(NS_SEP));
    let hit = false;
    eachNode(ast(file), (n) => {
      if (!ts.isCallExpression(n)) return;
      const callee = unwrap(n.expression);
      if (ts.isIdentifier(callee) && local.has(callee.text)) hit = true;
      if (
        ts.isPropertyAccessExpression(callee) &&
        ts.isIdentifier(callee.expression) &&
        ns.some(
          ([alias, target]) =>
            alias === callee.expression.text &&
            exportedAs.get(target)?.has(callee.name.text),
        )
      ) {
        hit = true;
      }
    });
    if (hit) callers.push(rel(file));
  }
  return callers.sort();
}

/** Every source file under the scanned root — NOT just the walk. A new caller of
 * the quoted-message overlay is exactly the file nobody has imported from
 * ChatArea yet, so the walk is the wrong population for that one question. */
function allSourceFiles(dir) {
  const out = [];
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, e.name);
    if (e.isDirectory()) {
      out.push(...allSourceFiles(full));
      continue;
    }
    if (!/\.tsx?$/.test(e.name) || /\.test\.tsx?$/.test(e.name)) continue;
    out.push(full);
  }
  return out;
}

function scan(files) {
  const found = new Map();
  for (const file of files) {
    const lines = readFileSync(file, "utf8").split("\n");
    for (const [kind, re] of KINDS) {
      let n = 0;
      for (const line of lines) {
        const t = line.trim();
        // Prose about a landing point is not a landing point.
        if (t.startsWith("//") || t.startsWith("*")) continue;
        // 🔴 OCCURRENCES, NOT LINES (R10-5 B5): a second `setTimeout` appended
        // to an already-counted line used to be free.
        n += line.match(re)?.length ?? 0;
      }
      if (n > 0) found.set(`${file.slice(SRC.length + 1)} | ${kind}`, n);
    }
  }
  return found;
}

const problems = [];

// 1. The shapes scanned for are the shapes that have ever been scanned for.
const kinds = KINDS.map(([k]) => k);
if (JSON.stringify(kinds) !== JSON.stringify(KIND_NAMES)) {
  problems.push(
    `the scanned SHAPES changed:\n  scanning: ${kinds.join(", ")}\n  pinned:   ${KIND_NAMES.join(", ")}`,
  );
}

// 2. Every registered row says something.
for (const r of REGISTRY) {
  if (r.verdict.trim().length < 20) {
    problems.push(
      `${r.file} | ${r.kind}: an entry with nothing said about it is not an entry`,
    );
  }
}

// 3. The population is WALKED, not typed in. Two walks, two boundaries: the
//    async census keeps `src/api` out (126 unrelated `await`s), the state census
//    does not (an SSE transport is where a per-room cache would land).
const files = walkFromChatArea(ASYNC_SCOPE);
const stateFiles = walkFromChatArea(STATE_SCOPE).filter(
  (f) => !STATE_SKIP.test(rel(f)),
);

// 3b. A path alias the walk cannot follow drops files SILENTLY, which is how a
//     population census dies. There is no alias today; if one appears, stop.
const aliases = unfollowableAliases();
if (aliases.length > 0) {
  problems.push(
    `tsconfig declares module path aliases the walk cannot resolve (${aliases.join(", ")}).\n  resolveSpec follows RELATIVE specifiers only, so any file imported through an alias silently leaves the population — teach resolveSpec the mapping before using it.`,
  );
}
if (!files.includes(join(SRC, "hooks", "useAttachmentStaging.ts"))) {
  problems.push(
    "the walk no longer reaches hooks/useAttachmentStaging.ts — it is derived from ChatArea's imports, and R9-1 lived in a file nobody had typed in",
  );
}
if (files.length <= 30) {
  problems.push(
    `the walk reached only ${files.length} files; it used to reach more than 30, so something stopped being followed`,
  );
}

// 4. Both directions at once: a NEW landing point with no verdict, a count that
//    grew, and a register row describing code that no longer exists all read out
//    of the same comparison.
const found = scan(files);
const registered = new Map(
  REGISTRY.map((r) => [`${r.file} | ${r.kind}`, r.count]),
);
const rows = (m) => [...m].map(([k, n]) => `${k} | ${n}`).sort();
const foundRows = rows(found);
const regRows = rows(registered);
const missing = foundRows.filter((r) => !regRows.includes(r));
const stale = regRows.filter((r) => !foundRows.includes(r));
if (missing.length > 0) {
  problems.push(
    `landing points in the code that the register does not match:\n${missing.map((r) => `    + ${r}`).join("\n")}`,
  );
}
if (stale.length > 0) {
  problems.push(
    `register rows that no longer match the code:\n${stale.map((r) => `    - ${r}`).join("\n")}`,
  );
}

// 5. Module-level mutable state, both directions, over the SAME walk. A table
//    that outlives every mount is the only place left for one room's value to
//    reach another; an unregistered one is an unanswered question about which
//    room it belongs to.
const mutated = mutatedNames(stateFiles);
const consts = topLevelInitializers(stateFiles);
const foundState = [];
for (const file of stateFiles) {
  for (const name of scanModuleState(file, mutated, consts)) {
    foundState.push(`${rel(file)} | ${name}`);
  }
}
const regState = MODULE_STATE.map((r) => `${r.file} | ${r.name}`);
for (const r of MODULE_STATE) {
  if (r.verdict.trim().length < 20) {
    problems.push(
      `${r.file} | ${r.name}: module state with nothing said about what keys it is not registered`,
    );
  }
}
const newState = foundState.filter((r) => !regState.includes(r)).sort();
const goneState = regState.filter((r) => !foundState.includes(r)).sort();
if (newState.length > 0) {
  problems.push(
    `module-level state the register does not know about — say what keys it, and if that key is not the PEER say why one room's value may reach another:\n${newState.map((r) => `    + ${r}`).join("\n")}`,
  );
}
if (goneState.length > 0) {
  problems.push(
    `registered module-level state that no longer exists:\n${goneState.map((r) => `    - ${r}`).join("\n")}`,
  );
}

// 7. The thread's setter has exactly ONE home. Over the walk, so a hook the
//    chat surface starts calling tomorrow is covered without an edit here.
for (const file of files) {
  const where = rel(file);
  const hits = declaresThreadState(file);
  if (hits.length > 0 && where !== THREAD_STATE_OWNER) {
    problems.push(
      `${where} declares the chat thread's own state (${hits.join(", ")}).\n  Only ${THREAD_STATE_OWNER} may hold it. Every write to the thread must go through its commit / mergeHistory / clear doors, because those are what await the page's WAITING reply cards before the rows reach the view — a second setter is a second way to paint messages whose cards are still in the air, and the scroll target then moves under the reader (measured +254px at 1280).\n  Both halves are RESOLVED, not spelled. The shape is asked of the TYPE CHECKER (identity against \`ChatMessage\` in src/api/adapter.ts), so no annotation, an inferred literal, an alias or an import rename makes no difference; the callee is resolved back to the import that declared it, so \`React.useState(x)\`, \`const us = useState; us(x)\` and \`import { useState as useStore }\` all fire (measured).
  IT DOES NOT FIRE ON, and this list is exhaustive: (1) a thread whose messages are not \`ChatMessage\` — a locally redeclared twin, \`unknown[]\`; (2) a thread whose property is not literally named \`messages\` (\`{ rows: ChatMessage[] }\` passes); (3) a React hook reached through a local re-export barrel AND renamed on the way (\`useState\` re-exported as \`useStore\`) — the resolved anchor is the module specifier "react" plus the exported name, not symbol identity with @types/react, because this script must run over a tree with no node_modules, and a barrel is out of that anchor's reach; the barrel keeping the hook's own name still fires, on the literal-name floor the resolution is ORed with. See declaresThreadState for why each anchor is where it is.`,
    );
  }
}
if (threadTypeProbe().chatMessage === null) {
  problems.push(
    `rule 7 could not resolve the \`ChatMessage\` interface from src/api/adapter.ts, so its structural test compares against nothing and every file passes it vacuously. Point it at wherever the chat message type lives now.`,
  );
}
// Only meaningful once the anchor above resolved: `declaresThreadState` returns
// early without a `ChatMessage`, so the counter would be 0 for that reason and
// this message would blame the wrong half.
if (threadTypeProbe().chatMessage !== null && reactHookCallsSeen === 0) {
  problems.push(
    `rule 7 resolved NO callee anywhere in the walk to React's useState/useReducer/useRef, so its callee test is matching nothing and every file passes that half vacuously. Either the chat surface stopped using React state entirely, or reactHookOf's anchor (the module specifier "react" plus the exported name) no longer describes how these files import their hooks.`,
  );
}
if (declaresThreadState(join(SRC, "lib", "threadCommit.ts")).length === 0) {
  problems.push(
    `${THREAD_STATE_OWNER} no longer declares the thread's state — the rule above would then be vacuously true of every file, which is how a census dies quietly. If the state moved, move this rule with it.`,
  );
}

// 6. The quoted-message overlay's caller list, over the WHOLE tree. Its state
//    is protected by its caller's key and by nothing else.
const callers = overlayCallers(allSourceFiles(SRC));
if (
  JSON.stringify(callers) !== JSON.stringify([...QUOTED_OVERLAY_CALLERS].sort())
) {
  problems.push(
    `useQuotedMessageOverlay's callers changed:\n    calling:    ${callers.join(", ") || "(nobody)"}\n    registered: ${QUOTED_OVERLAY_CALLERS.join(", ")}\n  The overlay keeps NO room stamp of its own — it relies on its caller being unmounted by a room switch. A caller keyed on anything but the peer paints one room's message over another (R8-3).`,
  );
}

if (problems.length > 0) {
  console.error(
    "[async-landing] FAIL — the chat surface's async census is out of date.",
  );
  console.error(
    "Each row answers: when this callback commits, can the screen have moved on — and what stops the write from landing in the wrong place?",
  );
  for (const p of problems) console.error(`  ${p}`);
  console.error(
    "Fix by updating REGISTRY in frontend/scripts/check-async-landing-points.mjs, with a verdict for anything new.",
  );
  process.exit(1);
}

console.log(
  `[async-landing] ok — ${files.length} files walked from ChatArea (${stateFiles.length} incl. src/api for the state census), ${found.size} (file, shape) pairs and ${foundState.length} module-level state declarations on the register, ${callers.length} quoted-overlay caller(s)`,
);
