// api/http.ts — the real-backend adapter.
//
// Programs against the SAME frozen contract (spec/openapi.json) and maps every
// wire DTO through the SAME mappers the mock uses, so index.ts can swap mock↔http
// with zero UI changes.
//
// Every JSON call rides the schema-typed openapi-fetch client (api/client.ts):
// method + path + params + body are checked against generated/schema.ts, so a
// BE verb/path/query rename is a tsc error (ci.sh gate 10a), not a runtime 405.
// The client middleware owns the cross-cutting auth story — owner JWT as
// `Authorization: Bearer` on every request (gated routes are deny-by-default),
// 401 → clear token + oc-auth-expired — and throws an ApiError (api/errors.ts)
// on every non-2xx, carrying `.status`/`.code`/`.serverMessage` off the unified
// error envelope `{"error":{"code","message"}}` (the contract callers branch
// on, e.g. deleteRole's 409 via isHttpStatus).
//
// PERMANENTLY HAND-WRITTEN (schema-external by nature, never migrated):
//   - subscribeEvents (bottom) — an EventSource, not a fetch; the owner JWT
//     rides as `?token=` because EventSource cannot set headers.
//   - authedAttachmentUrl — a `?token=` URL rewrite for bare <img>/<a>, not a
//     network call at all.
//   - auth.ts login() — a public endpoint (no Authorization) whose job is the
//     setToken side effect; routing it through the authed client buys nothing.
// The `?token=` query fallback is OUTSIDE the OpenAPI contract (the schema has
// no token query param) — it is pinned only by service/auth.py + these comments.

import type {
  LoreSearchView,
  LoreEntryDetailView,
  LoreRevisionView,
  LorePendingEntityView,
  LoreEntityGovernanceView,
  Member,
  MonitoringView,
  VersionView,
  ReleaseCheckView,
  BackupHealthView,
  SigningKeyView,
  AuthStatusView,
  MfaEnrollView,
  MfaStateView,
  GlobalContextView,
  BootDocKind,
  BootDocView,
  DocumentKind,
  DocumentHistoryEntryView,
  DocumentHistoryView,
  DocumentRevisionView,
  DocumentSeedView,
  DiffPairView,
  RoleSummaryView,
  RoleDefView,
  BootstrapView,
  LessonsView,
  InsightView,
  OnboardResultView,
  DeleteResultView,
  UninstallResultView,
  BootstrapResultView,
  TeardownHereResultView,
  MachineView,
  MemberActivateResult,
  MemberRelocateResult,
} from "../types";
import type { ThemeBundle } from "../lib/themeBundle";
import type {
  Api,
  ChatCursor,
  ChatAnchor,
  ChatMessage,
  ChatReadReceipt,
  ChatAttachmentInput,
  PushSubscriptionInput,
  GalleryAttachment,
  MemberPatch,
  WebhookEndpoint,
  WebhookCreateInput,
  WebhookUpdate,
  WebhookRequestLog,
  ScheduledMessage,
  ScheduledMessageCreateInput,
  ScheduledMessageUpdate,
  ReplyCard,
  ReplyCardAnswerInput,
  ReplyCardCounts,
  ServerSettingsView,
  ServerSettingsPatch,
  TaskView,
  TaskMessageInput,
  TaskReassignInput,
  OutsourceWorkerView,
  TaskTypeView,
  TaskCountView,
  TaskStepDetailView,
  TaskArtifactView,
  TaskManualSummaryView,
  TaskManualView,
  TaskManualPatch,
  DocSummaryView,
  DocView,
  RolePatch,
  RoleCreateInput,
  RoleCreateResult,
  AliasPatch,
  OnboardOptions,
  SseDelta,
  SseDeltaNames,
  MemberResumeSummaryView,
  ThemeListItem,
  ThemeWriteReceipt,
  ThemeDeleteResult,
  SseConnectionState,
  LoreSearchInput,
  AccountCostResetReceipt,
  CostResetReceipt,
  TaskArtifactVersionView,
} from "./adapter";
import {
  toMember,
  toChatMessage,
  toChatRead,
  toGalleryAttachment,
  toReplyCard,
  toMonitoring,
  toVersion,
  toReleaseCheck,
  toBackupHealth,
  toSigningKeys,
  toGlobalContext,
  toBootDoc,
  toDocumentHistory,
  toTaskArtifactVersion,
  toDocumentHistoryEntry,
  toLoreSearch,
  toLoreEntryDetail,
  toLorePendingEntity,
  toLoreEntityGovernance,
  toLoreRevision,
  toDocumentRevision,
  toDocumentSeed,
  toRoleDef,
  toRoleSummary,
  toBootstrap,
  toLessons,
  toInsight,
  toOnboardResult,
  toDeleteResult,
  toUninstallResult,
  toBootstrapResult,
  toTeardownHereResult,
  toMachine,
  toServerSettings,
  toTask,
  toTaskListItem,
  toTaskStepDetail,
  toTaskArtifact,
  toOutsourceWorker,
  toTaskType,
  toTaskManual,
  toTaskManualSummary,
  toWebhookEndpoint,
  toWebhookRequestLog,
  toScheduledMessage,
  toMemberResumeSummary,
  fromTaskManualPatch,
  fromTaskReassignInput,
  toThemeBundle,
  toThemeListItem,
  toThemeWriteReceipt,
  toThemeDeleteResult,
} from "./mappers";
// The one wire type this seam names directly: GET /api/reply-cards serves a
// UNION (light rows | full cards) and `?view=full` is what picks the second
// arm, so listReplyCards has to narrow to it. See that function.
import type { WireLoreSearchRequest, WireReplyCard } from "./wire";
import { ownerToken, setToken } from "./auth";
import { ApiError, parseRetryAfter } from "./errors";
import { fetchDiffPair } from "./diff";
import type { DiffParams } from "../lib/diffLink";
import { client, handleUnauthorized } from "./client";

// Auth is cross-cutting and lives in ONE place each: owner-JWT sourcing
// (localStorage `oc_token` + VITE_OC_TOKEN fallback) is api/auth.ts
// `ownerToken()`; the Bearer header + 401 → clear token + oc-auth-expired is
// the client middleware (api/client.ts). The only direct ownerToken readers
// left here are the two schema-external `?token=` paths below (SSE downlink +
// attachment URLs — they cannot carry a header).

// A gated attachment blob (`/api/chat/attachment/<id>`) is fetched by a bare
// `<img src>` / download `<a href>`, which cannot carry an `Authorization`
// header → the request would 401. PERMANENTLY HAND-WRITTEN — this is a URL
// rewrite, not a fetch call; there is nothing for an OpenAPI client to type.
// Mirror the SSE downlink: ride the owner JWT as a `?token=` query param
// (service/auth.py accepts the identical query fallback for every gated route;
// the param is OUTSIDE the OpenAPI schema). Only same-origin API paths are tokenised;
// inline `data:`/`blob:` URIs (the composer's pending preview, mock mode) carry
// their bytes and need no auth, so they pass through untouched.
export function authedAttachmentUrl(url: string): string;
export function authedAttachmentUrl(url: undefined): undefined;
export function authedAttachmentUrl(url?: string): string | undefined;
export function authedAttachmentUrl(url?: string): string | undefined {
  if (!url || !url.startsWith("/")) return url;
  const t = ownerToken();
  if (!t) return url;
  const sep = url.includes("?") ? "&" : "?";
  return `${url}${sep}token=${encodeURIComponent(t)}`;
}

// credentialPost is the bare-fetch POST the two password endpoints share:
// public-shaped (an explicit `token` argument, not the middleware) and
// throwing the SAME ApiError the client middleware throws — but WITHOUT the
// 401 → clear-token + oc-auth-expired reaction (a wrong claim/current
// password is an inline form error, never a logout).
async function credentialPost<T = { token: string }>(
  path: string,
  body: unknown,
  token?: string,
): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(path, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    let code = "";
    let serverMessage = "";
    try {
      const parsed: unknown = await res.json();
      const err = (parsed as { error?: { code?: unknown; message?: unknown } })
        ?.error;
      if (typeof err?.code === "string") code = err.code;
      if (typeof err?.message === "string") serverMessage = err.message;
    } catch {
      // Not JSON — keep the honest empties.
    }
    throw new ApiError(
      `http ${res.status} for POST ${path}`,
      res.status,
      code,
      serverMessage,
      parseRetryAfter(res.headers.get("Retry-After")),
    );
  }
  return (await res.json()) as T;
}

// ── shared SSE downlink (connection pool fix) ──────────────────────────────
// ONE EventSource for the whole SPA, fanned out client-side to every
// subscriber — the same shape as the mock's emitTopic fan-out. The old
// one-EventSource-per-subscriber design exhausted Chromium's 6-connections-
// per-host pool (App badge + useMembers + useMonitoring + useChat already held
// 4; each mounted inline ChatReplyCard added one more), after which EVERY
// fetch — the reply POST included — hung forever. Pinned by
// e2e_test/tests/13_reply_cards.spec.js (雙卡同房 leg).
//
// Lifecycle: the connection opens lazily on the first subscriber and is CLOSED
// when the last subscriber unsubscribes. Why close (not keep-alive):
//   1. In practice the set never empties while the SPA is up (the App shell's
//      badge hook subscribes for the app's lifetime), so this path only fires
//      on full teardown/logout — exactly when the connection should go away.
//   2. Reopening re-reads ownerToken(), so a re-login gets a fresh token
//      instead of a connection pinned to a stale JWT.
//   3. Server-side presence projects "online" from live SSE connections; a
//      subscriber-less phantom connection would misrepresent liveness.
// Reconnect: the browser's native EventSource auto-retry handles the TRANSIENT
// drops (readyState back to CONNECTING) and we never tear the connection down
// for those. It does NOT handle a PERMANENT failure: on a non-200 response, a
// wrong `Content-Type`, or a 401 the spec says the browser fails the connection
// and moves it to CLOSED **without ever retrying**. That case is ours — see
// `es.onerror` / `scheduleSseReconnect` below.
const sseSubscribers = new Set<(topic: string, delta?: SseDelta) => void>();
let sseSource: EventSource | null = null;
// The document/window foreground listener that drives the foreground-restore
// resync (installed with the connection, torn down with it). Held module-level
// so the last-unsubscribe teardown can remove exactly the one it added.
let sseVisibilityHandler: (() => void) | null = null;

// ── the downlink's own health, published to the UI ─────────────────────────
// A dead downlink is INVISIBLE by construction: a stream that has stopped
// delivering deltas renders EXACTLY like a stream on which nothing has
// happened. The cockpit then shows a frozen snapshot that looks live, and the
// only way the owner finds out is by noticing that the world has gone
// implausibly quiet (owner 2026-08-21: 「有時候…要 refresh page 才會更新」).
// So the connection state is not an internal detail — it is published, and the
// app renders it (components/ConnectionBanner.tsx). Silent self-healing was
// explicitly rejected as the whole fix: it trades a visible stall for an
// invisible one, and the deltas missed while the stream was down would be gone
// with nobody the wiser. Recovery therefore ALWAYS ends in the existing full
// `resyncAll` (below), and the down period is on screen while it lasts.
//
//   "live"         — the stream is open and delivering.
//   "connecting"   — no open stream right now: first connect, the browser's own
//                    retry, or our permanent-failure retry. What is on screen
//                    may be stale.
//   "unauthorized" — the session is dead (the downlink answered 401/403, or the
//                    token is gone). We STOP retrying: hammering a server that
//                    has already said no is not recovery. `handleUnauthorized`
//                    bounces the app to the login wall.
//   "idle"         — nobody is subscribed (logged out / torn down). Not a fault.
// The state vocabulary itself lives on the seam (api/adapter.ts) — ONE
// declaration, so the mock transport and every UI reader are typed against the
// same set of words and a new state cannot be added to one side only.
let sseState: SseConnectionState = "idle";
const sseStateSubscribers = new Set<(s: SseConnectionState) => void>();

// 🔴 THE THIRD FAN-OUT. There are exactly three loops in this file that hand
// control to code we do not own (`/usr/bin/grep -n "for (const cb of"` — this
// one, `resyncAll`, and the delta fan). The other two were isolated when a
// throwing subscriber was found to be able to freeze the state machine; THIS one
// was missed, and it is the worst-placed of the three because it sits UPSTREAM
// OF THE RETRY SCHEDULER. Measured before this guard: a state subscriber that
// threw on "connecting" left the connection count at 1 after sixty seconds —
// `setSseState` threw, so `scheduleSseReconnect()` on the next line never ran,
// `sseSource` was already null, and nothing would ever reopen it.
//
// 🔑 That is THE ORIGINAL BUG OF THIS TICKET, reached through a different door:
// a cockpit frozen for good with a banner that promises a reconnect nobody
// scheduled. Two defences, because either alone is a single point of failure:
//   HERE       — no subscriber can escape this loop; and
//   CALL SITES — the mechanism runs BEFORE the broadcast, never after, so even
//                an un-isolated throw could not cancel a recovery.
function setSseState(next: SseConnectionState): void {
  if (sseState === next) return;
  sseState = next;
  for (const cb of [...sseStateSubscribers]) {
    try {
      cb(next);
    } catch (e) {
      // A listener's bug must not decide whether this app reconnects.
      console.error("sse state: subscriber threw", next, e);
    }
  }
}

/** The downlink's current health. Exported for tests and for the first render
 * of a subscriber that mounts mid-flight. */
export function sseConnectionState(): SseConnectionState {
  return sseState;
}

/** Watch the downlink's health. Fires IMMEDIATELY with the current state (a
 * subscriber that mounts while the stream is already down must not have to wait
 * for the next transition to learn that), then on every change. */
export function subscribeSseConnection(
  cb: (s: SseConnectionState) => void,
): () => void {
  sseStateSubscribers.add(cb);
  // 🔴 A FOURTH HAND-OFF TO FOREIGN CODE, and the one an enumeration misses.
  // The review that found the other three searched for `for (const cb of` —
  // correct for the three FAN-OUTS, and structurally blind to this one, which is
  // a bare call, not a loop. Found by probing the behaviour instead: a listener
  // that threw on its very first (synchronous, immediate) invocation threw out
  // of `subscribeConnection` itself, i.e. out of the subscriber's mount effect.
  // Same class, same fix. Counting by shape finds what the shape describes; the
  // denominator is only as honest as the pattern that produced it.
  try {
    cb(sseState);
  } catch (e) {
    console.error("sse state: subscriber threw on subscribe", sseState, e);
  }
  return () => {
    sseStateSubscribers.delete(cb);
  };
}

// Backoff for OUR retry (the browser's own retry needs none — it has its own).
// Escalating, capped, and deliberately not jittered: there is exactly ONE
// downlink per tab, so there is no thundering herd to spread out. The cap is
// what keeps a long outage from turning into a request flood while still
// recovering within half a minute of the server coming back.
const SSE_RETRY_DELAYS_MS = [1000, 2000, 5000, 10000, 30000] as const;
let sseRetryTimer: ReturnType<typeof setTimeout> | null = null;
let sseRetryAttempt = 0;

/** "There is a hole in what this client has seen, and the next stream that
 * opens owes us a full resync."
 *
 * 🔴 THIS IS MODULE STATE ON PURPOSE, AND THE REASON IS A BUG THAT ALREADY
 * HAPPENED HERE. The first version of this fix passed the same fact as an
 * ARGUMENT — `ensureSseSource({ reconnect: true })` — from the retry path. That
 * works right up until somebody rebuilds the connection through a DIFFERENT
 * door, and there is another door: `subscribeEvents` calls `ensureSseSource()`
 * too. During an outage `sseSource` is null, so the next component to mount —
 * a reply card scrolling into view, a popover, a peer switch, any of the ~24
 * independent subscriptions in this app — rebuilt the stream itself, with the
 * flag defaulted to false. The connection came back, the state went "live", the
 * banner disappeared, and the deltas from the outage were gone with nobody told.
 * A recovery that skips the resync is strictly WORSE than not recovering: it
 * converts a stall the owner can see into a hole he cannot.
 *
 * As a parameter, every call site had to REMEMBER. As module state, no call site
 * CAN forget: whoever opens the next stream discharges the debt. That is why it
 * is not a second flag next to `opened` but a replacement for the argument.
 *
 * Cleared only by an open that actually ran the resync. Deliberately NOT cleared
 * on last-unsubscribe: an extra resync costs one round-trip per topic, a missed
 * one costs data nobody knows is missing, so the tie breaks toward resyncing. */
let sseGapPending = false;

// The CLOSED SSE topic vocabulary (spec/sse.md §3.1 / §4.1). Replayed one
// synthetic delta per topic to every subscriber on a full resync so each hook
// refetches its snapshot — see resyncAll below. New topics MUST be added here.
//
// EXPORTED so there is exactly ONE copy of the closed set in the frontend
// (T-05db node 4). There were THREE hand-copies: this one, plus transcriptions
// in hooks/sseFanout.test.tsx and api/http.sse-pool.test.ts — both now import
// THIS array. Its own correctness is no longer taken on trust either:
// api/sseResyncTopics.test.ts asserts it EQUALS the spec/sse.md §3.1 table,
// parsed from the repo file at run time rather than transcribed.
//
// Why this matters more than it looks: a topic MISSING here fails silently —
// after a reconnect that topic never refetches, so the data is right, the
// screen is stale, and nothing errors. Do not transcribe the list anywhere
// else; a second copy is the defect relocated, not fixed.
export const SSE_RESYNC_TOPICS = [
  "member",
  "chat",
  "chat_read",
  "reply_card",
  "task",
  "outsource_worker",
  "task_manual",
  "global_context",
  "role_def",
  "lessons",
  "insight",
  "context",
  "monitoring",
] as const;

// The payload fields that name an ENTITY rather than describe one (spec/sse.md
// §2.2 lists the per-topic payload shapes). Everything else in the payload —
// `status`, `priority`, `last_read_ts`, `codename`, `name`, `desired_state` — is
// a VALUE and is dropped here, so no hook can merge one even by accident: §2.2
// forbids it because the payload lacks the server-derived DTO fields the UI
// renders. Naming an item is what makes a one-item refetch possible.
const SSE_NAME_FIELDS = ["id", "from", "to", "reader", "peer"] as const;

/** Project one frame's payload down to its identity fields. Non-string values
 * and unknown fields are dropped; a null payload (the topics that carry none)
 * yields empty names, which every subscriber reads as "refetch the lot". */
export function toSseDelta(topic: string, payload: unknown): SseDelta {
  const names: SseDeltaNames = {};
  const ids: string[] = [];
  if (payload && typeof payload === "object") {
    const bag = payload as Record<string, unknown>;
    for (const field of SSE_NAME_FIELDS) {
      const v = bag[field];
      if (typeof v !== "string" || v === "") continue;
      names[field] = v;
      if (!ids.includes(v)) ids.push(v);
    }
  }
  return { topic, names, ids };
}

// Fan one synthetic delta per closed topic to EVERY current subscriber — the
// missed-gap correction. The stream has NO replay (spec §2.1): a delta emitted
// while the client wasn't receiving is gone, so the client must full-resync
// whenever it may have missed deltas. Shared by BOTH triggers — a genuine
// EventSource reconnect (es.onopen) AND a return to the foreground (a mobile
// tab often PAUSES the connection in the background without a reconnect, so
// onopen never re-fires) — so every delta-backed view (unread badge, roster,
// tasks, reply cards…) re-pulls its truth in ONE place. Snapshot the set: a
// callback may (un)subscribe during the fan-out.
// A resync NAMES NOTHING on purpose: the stream has no replay, so what was
// missed is unknowable and every subscriber has to re-pull its whole snapshot.
// The whole fan is SYNCHRONOUS, which is what lets a subscriber coalesce the 13
// topics into one refetch (lib/deltaSink.ts) — do not make this loop async.
//
// 🔴 EVERY CALL IS ISOLATED, and the comment this replaced is why. It used to
// say "each subscriber's refetch has its own .catch (verified per-hook), so a
// fan into an unstable network fails as keep-the-stale-value + warn, never an
// unhandled rejection" — TRUE OF REJECTED PROMISES, AND NO DEFENCE AT ALL
// AGAINST A SYNCHRONOUS THROW. A `.catch` on the refetch cannot catch a hook
// that throws while BUILDING that refetch (a bad read off a delta, a render-time
// invariant). Measured on the version this replaces: one throwing subscriber
// aborted the whole fan on the FIRST topic, so
//   1. every OTHER subscriber received nothing — a resync silently covering a
//      fraction of the app;
//   2. the throw escaped `es.onopen`, so `sseGapPending` was never cleared and
//      `sseRetryAttempt` never reset;
//   3. `setSseState("live")` never ran ⇒ THE BANNER STAYED UP OVER A HEALTHY,
//      DELIVERING STREAM.
// (3) is the one that made this worth blocking on. This whole change exists to
// make a dead connection visible; a banner that cries disconnected while the
// stream is fine is the same lie with the sign flipped, and the owner has no
// more way to see through it than before. Isolation per (topic, subscriber)
// keeps one broken hook from deciding what the other 23 know — and keeps the
// state machine's bookkeeping, which runs AFTER this returns, always reachable.
function resyncAll(): void {
  for (const topic of SSE_RESYNC_TOPICS) {
    for (const cb of [...sseSubscribers]) {
      try {
        cb(topic, toSseDelta(topic, null));
      } catch (e) {
        // One subscriber's bug is not the other subscribers' problem, and it is
        // certainly not the connection state's problem. Report and carry on.
        //
        // ⚠️ WHAT SWALLOWING THIS COSTS, STATED PLAINLY. This app installs NO
        // error reporter — no window.onerror, no "error"/"unhandledrejection"
        // listener, no ErrorBoundary anywhere in src/ (measured). So this catch
        // is the end of the line: nothing collects it, and a hook that throws on
        // every resync will do so forever with only a console line to show for
        // it. That silence is accepted DELIBERATELY, because the alternative is
        // worse in kind, not degree: an escaping throw freezes the connection
        // state machine and lies to the owner about being disconnected. We trade
        // a quiet log for a truthful UI. `console.error`, not `.warn`, so that
        // the day something IS listening it hears this at the right severity —
        // a subscriber throwing is a bug, never an advisory.
        console.error("sse resync: subscriber threw", topic, e);
      }
    }
  }
}

/** Ask `/api/events` ITSELF what it answers, then drop the stream on the floor.
 *
 * `EventSource.onerror` carries NO status code — the spec hands the page an
 * event with nothing on it, so "the session expired" and "the server is down"
 * arrive as the same object. Guessing between them is the one thing this must
 * not do: guess "expired" and a server blip logs the owner out; guess "blip"
 * and an expired session becomes an unbounded retry loop against a server that
 * has already said no. So we ask, on the SAME URL — not on a stand-in endpoint
 * that could answer differently — and read the real status. The body is never
 * read and the request is aborted the moment the headers land, so the extra
 * connection lives for one round-trip.
 *
 * Returns the HTTP status, or 0 when the request never got an answer at all
 * (offline / DNS / TLS / too slow) — which is emphatically NOT an auth failure.
 *
 * 🔴 THE DEADLINE IS LOAD-BEARING, not politeness. This await is the ONLY thing
 * standing between an outage and the recovery loop: by the time it runs the
 * retry timer has already fired and cleared itself, and there is no EventSource
 * alive. A `fetch` that never settles — a server that accepts the connection and
 * then never sends headers, an overloaded proxy holding the socket — therefore
 * does not "delay" recovery, it ENDS it: no timer, no stream, no further
 * attempt, for the rest of the session. The banner would stay up (so the failure
 * is at least visible, not silent) but the only way out would be a reload.
 * Without a deadline the abort below is pure hygiene; with one it is the escape.
 *
 * `X-OC-SSE-Probe: 1` marks this request as the probe and NOT a real stream. A
 * `fetch` can set a header and an `EventSource` cannot, which makes the header a
 * property of the thing itself rather than a guess about it — the server may
 * ignore it, and the e2e uses it to tell the two apart without leaning on the
 * browser's own request-type classification. */
export const SSE_PROBE_TIMEOUT_MS = 8000;

async function probeEventsEndpoint(): Promise<number> {
  const t = ownerToken();
  if (!t) return 401; // no token at all — the same conclusion, without a request
  const ctrl = new AbortController();
  const deadline = setTimeout(() => ctrl.abort(), SSE_PROBE_TIMEOUT_MS);
  try {
    const res = await fetch(`/api/events?token=${encodeURIComponent(t)}`, {
      method: "GET",
      headers: { Accept: "text/event-stream", "X-OC-SSE-Probe": "1" },
      signal: ctrl.signal,
    });
    return res.status;
  } catch {
    // Includes the deadline firing: a probe that timed out tells us nothing
    // about auth, so it reads exactly like "offline" — retry, log nobody out.
    return 0;
  } finally {
    clearTimeout(deadline);
    // Never consume the stream: we only ever wanted the status line. Without
    // this the probe would hold a second live SSE connection open for the rest
    // of the session, every time it succeeds.
    try {
      ctrl.abort();
    } catch {
      // Abort is best-effort — the request is already on its way out.
    }
  }
}

/** One retry of the downlink, gated on what the endpoint actually answers. */
async function attemptSseReconnect(): Promise<void> {
  // The world may have moved while the timer slept (unsubscribed, or another
  // path already reopened the stream).
  if (sseSubscribers.size === 0 || sseSource) return;
  const status = await probeEventsEndpoint();
  if (sseSubscribers.size === 0 || sseSource) return;
  if (status === 401 || status === 403) {
    // The session is dead. STOP — and say so, rather than retrying forever
    // behind a banner that promises a recovery that can never come.
    handleUnauthorized(); // clears the token + fires oc-auth-expired → login wall
    setSseState("unauthorized"); // mechanism first, broadcast second
    return;
  }
  // Anything else (200, 5xx, 0/offline) is a transport problem, not an auth
  // one: reopen. A reopen is by definition a RECONNECT, so it must resync —
  // the stream has no replay (spec/sse.md §2.1) and everything emitted while we
  // were down is gone.
  ensureSseSource();
  if (!sseSource) {
    // ensureSseSource declined: no token. Same conclusion as a 401, so it must
    // reach the same CONCLUSION — not merely the same local state. Until round
    // 4 this arm only flipped `sseState`, which left the two doors asymmetric:
    // the 401 arm fires `oc-auth-expired` and AuthGate drops the app to the
    // login wall, while this one put up a banner over an app that still looked
    // logged in. Review judged the divergent state unreachable in practice (the
    // probe answers 401 first when the token is gone), so this is hygiene, not
    // a bug fix — but "the arms agree" is the property worth keeping, and an
    // unreachable asymmetry is exactly the kind that stops being unreachable
    // when someone changes the probe.
    handleUnauthorized(); // clears the token + fires oc-auth-expired
    setSseState("unauthorized");
    return;
  }
  // Open is not yet OPENED — es.onopen flips the state to "live" and resets the
  // backoff; a second failure lands in es.onerror and schedules the next try.
}

function scheduleSseReconnect(): void {
  if (sseRetryTimer !== null) return; // one retry in flight is enough
  const idx = Math.min(sseRetryAttempt, SSE_RETRY_DELAYS_MS.length - 1);
  sseRetryAttempt += 1;
  sseRetryTimer = setTimeout(() => {
    sseRetryTimer = null;
    void attemptSseReconnect();
  }, SSE_RETRY_DELAYS_MS[idx]);
}

function cancelSseReconnect(): void {
  if (sseRetryTimer === null) return;
  clearTimeout(sseRetryTimer);
  sseRetryTimer = null;
}

/** Drop the whole downlink back to its module-load state.
 *
 * TEST-ONLY, and the same shape as `__resetMock` in api/mock.ts. It exists
 * because the downlink is deliberately module-level singleton state: a test
 * that fails PART WAY through leaves a live fake EventSource and an armed retry
 * timer behind, and every later test in the file then measures that debris
 * instead of its own subject. Without this, one genuine failure cascades into a
 * dozen fake ones and a mutant sweep can no longer say WHICH assertion caught
 * what. Never called by the UI. */
export function __resetSseDownlinkForTests(): void {
  cancelSseReconnect();
  sseRetryAttempt = 0;
  sseSubscribers.clear();
  if (sseSource) {
    try {
      sseSource.close();
    } catch {
      // Already gone — nothing to do.
    }
  }
  sseSource = null;
  if (sseVisibilityHandler && typeof document !== "undefined") {
    document.removeEventListener("visibilitychange", sseVisibilityHandler);
    if (typeof window !== "undefined") {
      window.removeEventListener("focus", sseVisibilityHandler);
    }
  }
  sseVisibilityHandler = null;
  sseGapPending = false;
  sseState = "idle";
  sseStateSubscribers.clear();
}

function ensureSseSource(): void {
  if (sseSource) return;
  const t = ownerToken();
  if (!t) return; // honest: gated, would 401 (callers already checked too)
  const es = new EventSource(`/api/events?token=${encodeURIComponent(t)}`);
  // The browser reconnects the stream transparently, but there is NO replay
  // (spec/sse.md §2.1): a delta emitted during the drop→reconnect gap is gone.
  // spec §2.2 makes it the CLIENT's job to full-resync on every reconnect. The
  // FIRST open needs none — every hook refetched on mount — so only a
  // SUBSEQUENT open (a genuine reconnect) fans a per-topic resync to all
  // subscribers, refetching each snapshot. Without this a missed delta lingers
  // until a manual reload (T-db62: a lone waiting reply-card badge stuck blank
  // after a reconnect, while chat/task badges self-healed on their next frame).
  // `opened` is per-CONNECTION: has THIS EventSource opened before? That covers
  // the browser's own transparent reconnect, where the same object opens twice.
  let opened = false;
  es.onopen = () => {
    // TWO independent reasons to resync, and the second is the one that is easy
    // to lose:
    //   `opened`         — this same connection dropped and came back by itself.
    //   `sseGapPending`  — a PREVIOUS connection died and this one replaces it,
    //                      no matter who built it or why. See the flag's comment.
    // A FIRST open with no pending gap needs neither: every hook fetched on mount.
    if (opened || sseGapPending) resyncAll();
    opened = true;
    sseGapPending = false; // the debt is discharged only by an open that PAID it
    sseRetryAttempt = 0; // the connection is good again; next outage starts short
    setSseState("live");
  };
  es.onerror = () => {
    // Two very different events arrive here through the same handler, and the
    // ONLY thing separating them is readyState:
    //   CONNECTING — the browser dropped the stream and is retrying it itself.
    //                Leave it alone (tearing it down here is what exhausted the
    //                connection pool before); just say we are not live.
    //   CLOSED     — the browser has GIVEN UP for good (non-200, 401, wrong
    //                content-type). Nothing will ever reopen it. `sseSource` is
    //                still non-null, so every ensureSseSource() from here on
    //                early-returns and the cockpit stays frozen forever, with
    //                no error on screen and no reconnect. This branch is the
    //                bug this whole block exists to fix.
    if (sseSource !== es) return; // a handler from a connection we already replaced
    if (es.readyState !== 2 /* CLOSED */) {
      setSseState("connecting");
      return;
    }
    try {
      es.close();
    } catch {
      // Already closed — closing twice is defined as a no-op, be defensive anyway.
    }
    sseSource = null; // release the guard that would block every rebuild
    // Record the hole BEFORE anything can rebuild the stream — including a
    // component mounting on the very next tick, which does not go through the
    // retry path at all.
    sseGapPending = true;
    if (sseSubscribers.size === 0) {
      setSseState("idle");
      return;
    }
    // Schedule the recovery FIRST, announce it second. Telling the world we are
    // reconnecting is worthless if saying so is what stops us reconnecting.
    //
    // ⚠️ NO TEST GUARDS THIS ORDERING — said plainly so the next person does not
    // go looking for the guard, or "helpfully" swap the lines back. Measured 2×2
    // (isolation in `setSseState` × this ordering):
    //     isolation ON,  schedule-first  → recovers
    //     isolation ON,  announce-first  → recovers   ← indistinguishable
    //     isolation OFF, schedule-first  → recovers   ← THIS LINE'S VALUE
    //     isolation OFF, announce-first  → FROZEN, permanently
    // So the ordering is a genuine second line of defence — it alone saves the
    // app when the isolation is gone — and it is an EQUIVALENT MUTANT while the
    // isolation holds. Writing a test for it would mean writing one that passes
    // either way, which is the exact failure this ticket already made twice
    // (an assertion that names a property it cannot reach). Belt and braces,
    // with the braces documented instead of falsely pinned.
    //
    // 🔴 THE EQUIVALENCE IS CONDITIONAL, AND HERE IS THE CONDITION. "No test can
    // catch this" is true ONLY while the try/catch inside `setSseState` stands.
    // Read the fourth row again: the moment that isolation is removed, weakened,
    // or routed around, this ordering stops being redundant and becomes THE ONLY
    // THING between a throwing listener and a permanently frozen cockpit — and
    // at that moment it MUST be pinned by a test, because it is no longer
    // equivalent to anything. An undocumented premise is how an honest gap turns
    // into a lie six months later: the note stays, the reason for it quietly
    // stops being true, and the next reader inherits a false reassurance. So if
    // you are here because you are changing `setSseState`'s fan-out, this comment
    // is addressed to you: you are about to invalidate it.
    scheduleSseReconnect();
    setSseState("connecting");
  };
  es.onmessage = (e: MessageEvent) => {
    // The parse and the fan-out get SEPARATE handling on purpose. They used to
    // share one try/catch labelled "non-JSON keepalive/comment frame — ignore",
    // which meant a subscriber throwing mid-fan was silently filed as a malformed
    // frame: the remaining subscribers were skipped and the log said nothing that
    // pointed at the real cause. Same defect class as the one in `resyncAll`
    // above, with a misleading label on top.
    let evt: { topic?: string; data?: { payload?: unknown } } | null;
    try {
      evt = JSON.parse(e.data) as typeof evt;
    } catch {
      return; // Non-JSON keepalive/comment frame — genuinely ignorable.
    }
    // 🔴 `!evt` IS NOT REDUNDANT, and leaving it out was a REGRESSION THIS FILE
    // SHIPPED. Splitting the old single try/catch fixed a real bug (see below)
    // but narrowed the protection at the same time: the property access moved
    // OUTSIDE the try, where the old catch had been quietly covering it. And
    // `JSON.parse("null")` does not throw — it returns `null`, so a literal
    // `null` frame walked straight into `evt.topic` and threw a TypeError out of
    // `es.onmessage`. Measured: the seven other malformed shapes (`: keepalive`,
    // `5`, `"hello"`, `[1,2]`, `true`, `{"data":{}}`, empty) are all inert
    // because reading `.topic` off a non-null primitive just yields undefined.
    // `null` is the single value that is neither a parse error nor an object.
    // This path must survive ANYTHING the wire delivers, so it is pinned by
    // http.sse-malformed-frames.test.ts rather than by this comment.
    //
    // 🔴 AND TRUTHY IS NOT ENOUGH — `typeof … === "string"` is the actual
    // guard. Review round 4 probed the FIELD shapes rather than only the root
    // shapes and found `{"topic":123}`, `{"topic":{"a":1}}` and
    // `{"topic":["chat"]}`: none of them throws, so the old `!evt.topic` test
    // waved all three through, and each one then travelled the seam as a TYPE
    // LIE. `SseDelta.topic` is declared `string` and ~24 hooks compare it with
    // `===` against string literals, so a number/object/array topic is never
    // equal to anything, matches no hook, and is indistinguishable from "the
    // server sent nothing" — a silent hole rather than a loud one. The frames
    // that reach here come off a socket; the declared type is a promise this
    // boundary has to keep, not one it may assume.
    if (!evt || typeof evt.topic !== "string" || evt.topic === "") return;
    // Project the frame's payload to the identity fields it names (§2.2 —
    // never the values) so a subscriber can refetch ONE item.
    const delta = toSseDelta(evt.topic, evt.data?.payload ?? null);
    // Snapshot the set: a callback may (un)subscribe during fan-out.
    for (const cb of [...sseSubscribers]) {
      try {
        cb(evt.topic, delta);
      } catch (err) {
        // Same trade as `resyncAll` above (see the note there): nothing in this
        // app collects this, and that is the accepted price of not letting one
        // hook's throw stop a delta reaching the other subscribers.
        console.error("sse delta: subscriber threw", evt.topic, err);
      }
    }
  };
  sseSource = es;
  // Announced only once the connection actually exists — same reason as the
  // scheduler above: the broadcast is the last thing that happens, never a step
  // the mechanism has to survive.
  setSseState("connecting");

  // Foreground-restore resync (T-b86c). A mobile browser tab sent to the
  // background often PAUSES the EventSource without closing it: no reconnect
  // fires, so es.onopen never re-runs and the reconnect resync above never
  // happens — deltas emitted while backgrounded are lost (no replay) and every
  // badge/list stays stale until a manual reload (owner: 手機切走再切回, 未讀
  // 徽章 stuck). On return to the foreground, run the SAME full resync.
  // BOTH visibilitychange AND window focus, mirroring useChat's own foreground
  // hook — owner's case is switching whole APPS on a phone, where the two
  // events do not fire identically across iOS Safari / Android Chrome; listening
  // to both maximises the chance the restore is caught. A double fire is
  // harmless (resyncAll's refetches are idempotent). Guarded for non-DOM
  // environments (SSR / tests without document/window).
  //
  // Installed ONCE per live handler, not once per EventSource: a permanent
  // failure now rebuilds the connection without an unsubscribe in between, and
  // re-adding the listener each time would fan one resync per outage survived.
  if (typeof document !== "undefined" && sseVisibilityHandler === null) {
    sseVisibilityHandler = () => {
      if (document.visibilityState === "visible") resyncAll();
    };
    document.addEventListener("visibilitychange", sseVisibilityHandler);
    if (typeof window !== "undefined") {
      window.addEventListener("focus", sseVisibilityHandler);
    }
  }
}

/** Unwrap an openapi-fetch result. The client middleware (client.ts) throws on
 * EVERY non-2xx — `http <status> for <METHOD> <path>`, the same error contract
 * the retired hand-written getJson/sendJson helpers had — so a resolved call
 * ALWAYS carries 2xx data; the cast just tells tsc what the middleware
 * guarantees. */
function unwrap<T>(res: { data?: T }): T {
  return res.data as T;
}

/** View answer input → the wire `ReplyCardAnswerPostDTO` body. POST (answer)
 * and PUT (重新決定) share the exact same body shape. An absent option list /
 * text is sent as its honest wire default (null / ""); attachments are omitted
 * when empty — same convention as postChat.
 *
 * The indices are sorted ASCENDING here, at the seam, so ticking the same boxes
 * in a different order produces a byte-identical body. The server dedupes and
 * sorts too, but the two owners' requests would still differ on the wire, and
 * "the same decision" must not be two different payloads; duplicates collapse
 * for the same reason. An EMPTY list is NOT
 * flattened to null: `[]` is a 400 server-side and the caller means for it to
 * be, so a caller with nothing circled omits the field. */
function toAnswerBody(answer: ReplyCardAnswerInput) {
  const attachments = answer.attachments ?? [];
  return {
    option_idxs: answer.optionIdxs
      ? [...new Set(answer.optionIdxs)].sort((x, y) => x - y)
      : null,
    text: answer.text ?? "",
    ...(attachments.length > 0
      ? {
          attachments: attachments.map((a) => ({
            data_b64: a.dataB64,
            ...(a.filename ? { filename: a.filename } : {}),
            ...(a.mime ? { mime: a.mime } : {}),
          })),
        }
      : {}),
  };
}

export const httpApi: Api = {
  async listMembers(opts?: { light?: boolean }): Promise<Member[]> {
    // GET /api/members -> MemberDTO[]. ?fields=light (T-cf91) is the
    // identity-only projection — same wire shape, presence/machine/unread
    // honest-empty, the whole-chat unread scan skipped server-side. Only a
    // name+role surface (請示卡頁) requests it.
    const wire = unwrap(
      await client.GET("/api/members", {
        params: {
          query: opts?.light ? { fields: "light" } : {},
        },
      }),
    );
    return wire.map(toMember);
  },

  async getMember(id: string): Promise<Member> {
    // GET /api/members/{id} -> MemberDTO
    const wire = unwrap(
      await client.GET("/api/members/{member_id}", {
        params: { path: { member_id: id } },
      }),
    );
    return toMember(wire);
  },

  async updateMemberAvatar(id: string, file: File): Promise<string> {
    // Materialize the bytes before crossing openapi-fetch's schema boundary.
    // Passing a File object through a generated `string` body type is
    // cross-realm fragile (and can stringify to "[object File]"); ArrayBuffer
    // is an unambiguous BodyInit in browsers and tests alike.
    const bytes = await new Promise<ArrayBuffer>((resolve, reject) => {
      const reader = new FileReader();
      reader.onerror = () =>
        reject(reader.error ?? new Error("failed to read avatar file"));
      reader.onload = () => resolve(reader.result as ArrayBuffer);
      reader.readAsArrayBuffer(file);
    });
    const wire = unwrap(
      await client.PUT("/api/members/{member_id}/avatar", {
        params: {
          path: { member_id: id },
          query: {
            filename: file.name || undefined,
            mime: file.type || undefined,
          },
        },
        headers: { "Content-Type": "application/octet-stream" },
        // openapi-fetch defaults to JSON serialization. The spec types binary
        // bodies as string, while browsers correctly expose them as Blob/File;
        // this serializer preserves the raw bytes on the wire.
        body: bytes as unknown as string,
        bodySerializer: (body) => body as unknown as BodyInit,
      }),
    );
    return wire.avatar_url ?? "";
  },

  async removeMemberAvatar(id: string): Promise<void> {
    unwrap(
      await client.DELETE("/api/members/{member_id}/avatar", {
        params: { path: { member_id: id } },
      }),
    );
  },

  async activateMember(
    id: string,
    machineId?: string,
  ): Promise<MemberActivateResult> {
    // POST /api/members/{id}/activate {machine_id?} -> MemberDTO (writes
    // desired_state=online INTENT only; server does NOT flip online). When machineId is
    // given it BINDS the agent to that machine (the field was renamed host →
    // machine_id) — the spawn/wake path and the permanent "move agent" rebind
    // both go through here. Presence contract: the caller refetches and lets
    // server-driven presence surface waking → online. The body must be a present
    // object (MemberActivateDTO) — `{}` is the honest "no machine override".
    //
    // 🔴 The response body is READ, not discarded (T-7fa1). `activation_pending`
    // is the server's only report that no START went out on this attempt; a 200
    // alone cannot say that, because the intent is persisted before any dispatch
    // is attempted. The field is set ONLY on that shape (never `false`), and the
    // schema types it `boolean | null`, optional — so absent, null and false all
    // mean the same thing and `=== true` reads the wire without inventing a
    // default.
    const body = machineId !== undefined ? { machine_id: machineId } : {};
    const wire = unwrap(
      await client.POST("/api/members/{member_id}/activate", {
        params: { path: { member_id: id } },
        body,
      }),
    );
    return { activationPending: wire.activation_pending === true };
  },

  async relocateMember(
    id: string,
    machineId: string,
  ): Promise<MemberRelocateResult> {
    // POST /api/members/{id}/relocate {machine_id} -> MemberDTO (admin-gated
    // 改機器). PLACEMENT ONLY: writes the owner-pinned desired_machine_id and runs
    // the server's event-driven reconcile (a live member migrates onto the pin;
    // an offline member re-pins for the next wake) — it NEVER touches
    // desired_state (the activate contrast: a relocate is not a wake). Does NOT
    // flip online; the caller refetches and lets server-driven presence surface
    // the migration.
    // Same read-the-response discipline as activateMember (T-7fa1):
    // `relocation_pending` is set ONLY when a decided recycle STOP/START was
    // refused by the warden it was addressed to. Absent/null therefore means
    // "nothing was left undelivered" — NOT "the member is now running on the
    // pin"; a delivered STOP still needs the next tick's START to land.
    const wire = unwrap(
      await client.POST("/api/members/{member_id}/relocate", {
        params: { path: { member_id: id } },
        body: { machine_id: machineId },
      }),
    );
    // …and `relocation_deferred` says which of pending's two causes this is: a
    // deliberately deferred move (wind-down open) must not be alerted on.
    return {
      relocationPending: wire.relocation_pending === true,
      relocationDeferred: wire.relocation_deferred === true,
    };
  },

  async deactivateMember(id: string): Promise<void> {
    // POST /api/members/{id}/deactivate -> MemberDTO. Writes desired_state=offline +
    // stamps stopping_since (graceful STOP; retains the row). The handler takes
    // NO body. Caller refetches and lets server-driven presence surface
    // stopping → stopped (no optimistic state change here).
    await client.POST("/api/members/{member_id}/deactivate", {
      params: { path: { member_id: id } },
    });
  },

  async resetMemberCost(id: string): Promise<CostResetReceipt> {
    // POST /api/members/{id}/cost/reset -> CostResetDTO. Clears BOTH halves of
    // the actor's estimated spend (durable banked figure + live telemetry
    // figure) — clearing one would let the number reappear on the next read.
    // Takes no body. IRREVERSIBLE: nothing is retained server-side and there is
    // no undo route, so callers gate it behind a confirm.
    //
    // The receipt carries the PRE-reset figures: that response is the last
    // moment they exist anywhere. Nulls are passed through unchanged (null =
    // nothing was there to clear, not zero cleared). Caller refetches; the
    // 估計$ cell falls back to the dash on its own.
    const { data } = await client.POST("/api/members/{member_id}/cost/reset", {
      params: { path: { member_id: id } },
    });
    return {
      memberId: data?.member_id ?? id,
      clearedCost: data?.cleared_cost ?? null,
      clearedBankedCost: data?.cleared_banked_cost ?? null,
    };
  },

  async resetAccountCost(account: string): Promise<AccountCostResetReceipt> {
    // POST /api/accounts/cost/reset {account} -> AccountCostResetDTO. Zeroes the
    // ACCOUNT's own accumulated spend and NO member figure (owner ruling
    // rc-5c5d7c7c6dcd) — the two are separate numbers and separate buttons.
    //
    // The account key rides in the BODY, not the path: a real key contains "/"
    // and "@", and an encoded slash a proxy decodes would silently retarget an
    // irreversible call.
    //
    // IRREVERSIBLE, so callers gate it behind a confirm. The receipt carries the
    // PRE-reset figure — the last moment it exists — and null is passed through
    // unchanged (null = nothing was there to clear, not zero cleared). An
    // account nobody has reported under answers the same 200 with null rather
    // than a 404. Caller refetches monitoring; the card is folded from that read.
    const { data } = await client.POST("/api/accounts/cost/reset", {
      body: { account },
    });
    return {
      account: data?.account ?? account,
      clearedCost: data?.cleared_cost ?? null,
    };
  },

  async forceStopMember(id: string): Promise<void> {
    // POST /api/members/{id}/force-stop -> MemberDTO. Escalates a *stopping* member
    // to an IMMEDIATE kill: the server dispatches the robust STOP straight to the
    // warden (the warden SIGKILLs). It is NOT a shortcut past a countdown — the
    // server arms none on this arm. Three things end a soft offboard and this is
    // the last rung: the agent's own report_stopped, the deadline the owner opens
    // with 加速停止 (acceleratedStopMember), and this. Takes no body. Caller
    // refetches; presence surfaces stopped.
    await client.POST("/api/members/{member_id}/force-stop", {
      params: { path: { member_id: id } },
    });
  },

  async acceleratedStopMember(id: string): Promise<void> {
    // POST /api/members/{id}/accelerated-stop -> MemberDTO. Puts an ALREADY-OPEN
    // wind-down on the server's stop.accelerated_grace_secs clock and tells the
    // member. 409 when nothing is winding down, when there is no live session, or
    // when the member was already cut off by 強制停止 — the 409 is the contract,
    // not an edge case: it is what keeps this from being a second stop button.
    // Takes no body. Caller refetches; the member keeps its presence and gains a
    // refocus_deadline.
    await client.POST("/api/members/{member_id}/accelerated-stop", {
      params: { path: { member_id: id } },
    });
  },

  async dismissMember(id: string): Promise<void> {
    // DELETE /api/members/{id} -> MemberDTO (soft delete: status=removed +
    // desired_state=offline). Caller refetches (the row drops from the roster) and
    // navigates back.
    await client.DELETE("/api/members/{member_id}", {
      params: { path: { member_id: id } },
    });
  },

  async patchMember(id: string, patch: MemberPatch): Promise<Member> {
    // PATCH /api/members/{id} {name?, model?, effort?} -> MemberDTO. PATCH
    // semantics — only supplied fields ride the body (an absent field must NOT
    // arrive as null, which the server would reject / misread). model/effort
    // are launch intents (take effect on the next wake).
    const body: {
      name?: string;
      runtime?: "claude" | "codex";
      model?: string;
      effort?: string;
    } = {};
    if (patch.name !== undefined) body.name = patch.name;
    if (patch.runtime !== undefined) body.runtime = patch.runtime;
    if (patch.model !== undefined) body.model = patch.model;
    if (patch.effort !== undefined) body.effort = patch.effort;
    const wire = unwrap(
      await client.PATCH("/api/members/{member_id}", {
        params: { path: { member_id: id } },
        body,
      }),
    );
    return toMember(wire);
  },

  async refocusMember(id: string): Promise<void> {
    // POST /api/members/{id}/refocus -> MemberDTO (online-only; 409 otherwise)
    await client.POST("/api/members/{member_id}/refocus", {
      params: { path: { member_id: id } },
    });
  },

  async listWebhooks(memberId: string): Promise<WebhookEndpoint[]> {
    // GET /api/members/{id}/webhooks -> WebhookEndpointDTO[]
    const wire = unwrap(
      await client.GET("/api/members/{member_id}/webhooks", {
        params: { path: { member_id: memberId } },
      }),
    );
    return wire.map(toWebhookEndpoint);
  },

  async createWebhook(
    memberId: string,
    input: WebhookCreateInput,
  ): Promise<WebhookEndpoint> {
    // POST /api/members/{id}/webhooks {endpoint_id, purpose?, platform?,
    // signing_secret?} -> WebhookEndpointDTO
    const body: {
      endpoint_id: string;
      purpose?: string;
      platform?: "generic" | "slack" | "github";
      signing_secret?: string;
    } = {
      endpoint_id: input.endpointId,
    };
    if (input.purpose !== undefined) body.purpose = input.purpose;
    if (input.platform !== undefined) body.platform = input.platform;
    if (input.signingSecret !== undefined)
      body.signing_secret = input.signingSecret;
    const wire = unwrap(
      await client.POST("/api/members/{member_id}/webhooks", {
        params: { path: { member_id: memberId } },
        body,
      }),
    );
    return toWebhookEndpoint(wire);
  },

  async updateWebhook(
    memberId: string,
    endpointId: string,
    patch: WebhookUpdate,
  ): Promise<WebhookEndpoint> {
    // PATCH /api/members/{id}/webhooks/{endpoint_id} {status?, purpose?,
    // signing_secret?} -> WebhookEndpointDTO. PATCH semantics — only supplied
    // fields ride the body. `platform` is immutable and never sent here.
    const body: {
      status?: string;
      purpose?: string;
      signing_secret?: string;
    } = {};
    if (patch.status !== undefined) body.status = patch.status;
    if (patch.purpose !== undefined) body.purpose = patch.purpose;
    if (patch.signingSecret !== undefined)
      body.signing_secret = patch.signingSecret;
    const wire = unwrap(
      await client.PATCH("/api/members/{member_id}/webhooks/{endpoint_id}", {
        params: { path: { member_id: memberId, endpoint_id: endpointId } },
        body,
      }),
    );
    return toWebhookEndpoint(wire);
  },

  async deleteWebhook(memberId: string, endpointId: string): Promise<void> {
    // DELETE /api/members/{id}/webhooks/{endpoint_id} -> WebhookEndpointDTO (the
    // deleted row echo; the caller refetches the list).
    await client.DELETE("/api/members/{member_id}/webhooks/{endpoint_id}", {
      params: { path: { member_id: memberId, endpoint_id: endpointId } },
    });
  },

  async listWebhookRequests(
    memberId: string,
    endpointId: string,
  ): Promise<WebhookRequestLog[]> {
    // GET /api/members/{id}/webhooks/{endpoint_id}/requests ->
    // WebhookRequestLogDTO[] (last 5 raw /in requests, newest first).
    const wire = unwrap(
      await client.GET(
        "/api/members/{member_id}/webhooks/{endpoint_id}/requests",
        { params: { path: { member_id: memberId, endpoint_id: endpointId } } },
      ),
    );
    return wire.map(toWebhookRequestLog);
  },

  async listScheduledMessages(memberId: string): Promise<ScheduledMessage[]> {
    // GET /api/members/{id}/scheduled-messages -> ScheduledMessageDTO[]
    const wire = unwrap(
      await client.GET("/api/members/{member_id}/scheduled-messages", {
        params: { path: { member_id: memberId } },
      }),
    );
    return wire.map(toScheduledMessage);
  },

  async createScheduledMessage(
    memberId: string,
    input: ScheduledMessageCreateInput,
  ): Promise<ScheduledMessage> {
    // POST /api/members/{id}/scheduled-messages {body, cadence, timezone,
    // hour?, minute?, label?, day_of_week?, day_of_month?, custom_months?,
    // custom_days?, custom_hours?, custom_minutes?} -> ScheduledMessageDTO.
    // The three unconditionally required fields always ride; every other one
    // only when supplied (an absent field must not arrive as null). `hour` and
    // `minute` left the unconditional set in T-49e7 so a `custom` schedule does
    // not have to send two values it never reads.
    //
    // 🔴 `custom_months` must keep its `undefined`/`[]` distinction all the way
    // to the socket: absent means every month, `[]` is a 422. The `!== undefined`
    // guard below is what preserves it — a `?? []` anywhere on this path would
    // turn every pre-round-2-shaped create into a server refusal.
    const body: {
      body: string;
      cadence: "daily" | "weekly" | "monthly" | "custom";
      timezone: string;
      hour?: number;
      minute?: number;
      label?: string;
      day_of_week?: number;
      day_of_month?: number;
      custom_months?: number[];
      custom_days?: number[];
      custom_hours?: number[];
      custom_minutes?: number[];
    } = {
      body: input.body,
      cadence: input.cadence,
      timezone: input.timezone,
    };
    if (input.hour !== undefined) body.hour = input.hour;
    if (input.minute !== undefined) body.minute = input.minute;
    if (input.label !== undefined) body.label = input.label;
    if (input.dayOfWeek !== undefined) body.day_of_week = input.dayOfWeek;
    if (input.dayOfMonth !== undefined) body.day_of_month = input.dayOfMonth;
    if (input.customMonths !== undefined)
      body.custom_months = input.customMonths;
    if (input.customDays !== undefined) body.custom_days = input.customDays;
    if (input.customHours !== undefined) body.custom_hours = input.customHours;
    if (input.customMinutes !== undefined)
      body.custom_minutes = input.customMinutes;
    const wire = unwrap(
      await client.POST("/api/members/{member_id}/scheduled-messages", {
        params: { path: { member_id: memberId } },
        body,
      }),
    );
    return toScheduledMessage(wire);
  },

  async updateScheduledMessage(
    memberId: string,
    scheduleId: string,
    patch: ScheduledMessageUpdate,
  ): Promise<ScheduledMessage> {
    // PATCH /api/members/{id}/scheduled-messages/{schedule_id} ->
    // ScheduledMessageDTO. PATCH semantics — only supplied fields ride the
    // body; `id` and `member_id` are immutable and never sent.
    const body: {
      label?: string;
      body?: string;
      cadence?: "daily" | "weekly" | "monthly" | "custom";
      day_of_week?: number;
      day_of_month?: number;
      hour?: number;
      minute?: number;
      // Same undefined-vs-[] contract as on create: absent means "leave the
      // stored months alone" (or, on a switch to `custom` from a row that has
      // none, every month), `[]` means the caller asked for a schedule that
      // never fires and is refused.
      custom_months?: number[];
      custom_days?: number[];
      custom_hours?: number[];
      custom_minutes?: number[];
      timezone?: string;
      status?: "enabled" | "disabled";
    } = {};
    if (patch.label !== undefined) body.label = patch.label;
    if (patch.body !== undefined) body.body = patch.body;
    if (patch.cadence !== undefined) body.cadence = patch.cadence;
    if (patch.dayOfWeek !== undefined) body.day_of_week = patch.dayOfWeek;
    if (patch.dayOfMonth !== undefined) body.day_of_month = patch.dayOfMonth;
    if (patch.customMonths !== undefined)
      body.custom_months = patch.customMonths;
    if (patch.customDays !== undefined) body.custom_days = patch.customDays;
    if (patch.customHours !== undefined) body.custom_hours = patch.customHours;
    if (patch.customMinutes !== undefined)
      body.custom_minutes = patch.customMinutes;
    if (patch.hour !== undefined) body.hour = patch.hour;
    if (patch.minute !== undefined) body.minute = patch.minute;
    if (patch.timezone !== undefined) body.timezone = patch.timezone;
    if (patch.status !== undefined) body.status = patch.status;
    const wire = unwrap(
      await client.PATCH(
        "/api/members/{member_id}/scheduled-messages/{schedule_id}",
        {
          params: { path: { member_id: memberId, schedule_id: scheduleId } },
          body,
        },
      ),
    );
    return toScheduledMessage(wire);
  },

  async deleteScheduledMessage(
    memberId: string,
    scheduleId: string,
  ): Promise<void> {
    // DELETE /api/members/{id}/scheduled-messages/{schedule_id} ->
    // ScheduledMessageDTO (the deleted row echo; the caller refetches).
    await client.DELETE(
      "/api/members/{member_id}/scheduled-messages/{schedule_id}",
      { params: { path: { member_id: memberId, schedule_id: scheduleId } } },
    );
  },

  async getMemberResumeSummary(
    memberId: string,
  ): Promise<MemberResumeSummaryView> {
    // GET /api/members/{id}/resume-summary -> ResumeSummaryDTO (owner /
    // admin-agent only — an ordinary agent token gets 403). Called ONLY when
    // the panel's RESUME SUMMARY section is expanded (never on panel mount).
    const wire = unwrap(
      await client.GET("/api/members/{member_id}/resume-summary", {
        params: { path: { member_id: memberId } },
      }),
    );
    return toMemberResumeSummary(wire);
  },

  async listChat(
    withId: string,
    limit?: number,
    before?: ChatCursor,
  ): Promise<ChatMessage[]> {
    // GET /api/chat?with=<id>[&limit=<n>][&before_ts=&before_id=] ->
    // ChatMessageDTO[]. `limit` mirrors the server param: omitted → the
    // server's recent window (default 30); -1 → the whole history (the M2-3
    // gallery's full-history path). `before` (T-bf82 scrollback) rides as the
    // composite keyset cursor — the server then serves the strictly-older
    // history page. Undefined params are dropped from the query by the
    // client's serializer, so the cursorless wire shape is unchanged.
    //
    // THE WIRE ANSWERS AN OBJECT (T-48): `{messages, next_cursor}`. This method
    // reads `messages` and DROPS `next_cursor` on purpose — the cockpit pages
    // with the deprecated-but-supported `before_ts`/`before_id` pair it derives
    // from the oldest row it holds, and taking the token as well would leave two
    // cursors in play with nothing saying which one wins. Moving the scrollback
    // onto `?cursor=` is its own change, with its own test surface.
    //
    // READ-ONLY on every path (T-48): GET /api/chat advances no read
    // watermark, cursor or not. Marking a conversation read is
    // POST /api/chat/mark-read only. A `peekChat` twin of this method existed
    // until T-48 to opt out of an auto-mark that no longer exists; it was
    // merged back in here rather than left standing as a second name for the
    // same request.
    const wire = unwrap(
      await client.GET("/api/chat", {
        params: {
          query: {
            with: withId,
            limit,
            before_ts: before?.beforeTs,
            before_id: before?.beforeId,
          },
        },
      }),
    );
    return wire.messages.map(toChatMessage);
  },

  async listChatWindow(
    withId: string,
    anchor: ChatAnchor,
    limit: number,
  ): Promise<ChatMessage[]> {
    // GET /api/chat?with=<id>&limit=<n>[&start_id=][&end_id=] ->
    // ChatMessageDTO[], oldest→newest, both anchors INCLUSIVE. The undefined
    // half is dropped by the client's serializer, so sending one end alone is
    // one parameter on the wire, not an empty one.
    //
    // `limit` is REQUIRED here and is bounded 1..200 server-side on this path
    // (422 outside it) — unlike the legacy paths, which keep their own
    // "omitted = the recent window, -1 = everything" semantics. There is no
    // sensible "the whole history" reading of an anchor window, so the
    // signature does not offer one.
    //
    // READ-ONLY, like every read door on this route since T-48.
    const wire = unwrap(
      await client.GET("/api/chat", {
        params: {
          query: {
            with: withId,
            limit,
            start_id: anchor.startId,
            end_id: anchor.endId,
          },
        },
      }),
    );
    return wire.messages.map(toChatMessage);
  },

  async getChatMessage(id: string): Promise<ChatMessage> {
    // GET /api/chat?ids=<id> -> ChatMessageDTO[] with exactly one row. The
    // server's by-ids door is all-or-nothing, so an id that names nothing is a
    // 404 for the whole call and `unwrap` throws — which is what the caller
    // wants: it has one thing to open and either opens it or says it could not.
    //
    // ⚠️ ONE ID, ON PURPOSE — see Api.getChatMessage. The endpoint takes up to
    // 20; this adapter never sends more than one, because the only caller is a
    // click on one quote row. A `listChatByIds(ids[])` shape lived here until
    // 2026-08-21 and existed to feed a background batch refetcher; that machine
    // was deleted and the plural signature was the seam it grew out of.
    const wire = unwrap(
      await client.GET("/api/chat", { params: { query: { ids: [id] } } }),
    );
    const first = wire.messages[0];
    if (!first) {
      // Defensive: the server answers 404 rather than an empty array for an
      // unknown id, so this is unreachable today. It is a throw and not a null
      // because the caller's contract is "give me the message or fail" — a null
      // here would hand it a fourth state to draw.
      throw new Error(`GET /api/chat?ids=${id} returned no row`);
    }
    return toChatMessage(first);
  },

  async listChatAttachments(withId: string): Promise<GalleryAttachment[]> {
    // GET /api/chat/attachments?with=<memberId> -> ChatGalleryEntryDTO[]. The
    // M2 gallery query: the member's WHOLE attachment perspective (owner↔member
    // both directions + inter-agent threads), flattened newest→oldest with the
    // sender id + server-resolved display name per row. READ-ONLY — no
    // read-watermark side effect — as with listChat, which marks nothing
    // read either.
    const wire = unwrap(
      await client.GET("/api/chat/attachments", {
        params: { query: { with: withId } },
      }),
    );
    return wire.map(toGalleryAttachment);
  },

  async getChatAttachmentShareLink(attachmentId: string): Promise<string> {
    // GET /api/chat/attachments/{attachment_id}/share-link -> {url}: the
    // blob's serve path + its ?sig= HMAC credential (grants reading exactly
    // that one blob; no expiry, but voided when the signing key it was made
    // under leaves the ring — T-62). The caller absolutizes with the page
    // origin — the server never knows its public host.
    const wire = unwrap(
      await client.GET("/api/chat/attachments/{attachment_id}/share-link", {
        params: { path: { attachment_id: attachmentId } },
      }),
    );
    return wire.url;
  },

  async postChat(msg: {
    to: string;
    body: string;
    attachments?: ChatAttachmentInput[];
    replyTo?: string;
  }): Promise<ChatMessage> {
    // POST /api/chat {to, body, attachments?} -> ChatMessageDTO (server stamps
    // from/id/ts from the verified JWT sub). Addressing is by id (msg.to is a
    // member id). Pasted images AND/OR picked files ride together as the
    // generic `attachments` list of {data_b64, filename?, mime?} objects
    // (data_b64 = data-URI or bare base64) — all on the SAME message; omitted
    // when empty so a text-only post is unchanged. The old singular
    // `attachment` field was removed server-side (beta — the list is the sole
    // path, capped at 10 per message).
    const attachments = msg.attachments ?? [];
    const wire = unwrap(
      await client.POST("/api/chat", {
        body: {
          to: msg.to,
          body: msg.body,
          // The quote link. ALWAYS SENT — "" is the wire's "replies to
          // nothing", the same shape `body` uses, and what the generated
          // request type requires. (An earlier version of this comment said the
          // field was omitted on an ordinary post; it never was, and a comment
          // describing a wire shape the code does not produce is worse than no
          // comment.) The server checks a non-empty value EXISTS — and only
          // that, since 2026-08-21 — and is the only writer of the stored link.
          reply_to: msg.replyTo ?? "",
          ...(attachments.length > 0
            ? {
                attachments: attachments.map((a) => ({
                  data_b64: a.dataB64,
                  ...(a.filename ? { filename: a.filename } : {}),
                  ...(a.mime ? { mime: a.mime } : {}),
                })),
              }
            : {}),
        },
      }),
    );
    return toChatMessage(wire);
  },

  async markChatRead(mark: {
    peer: string;
    lastReadTs: number;
  }): Promise<ChatReadReceipt> {
    // POST /api/chat/mark-read {peer, last_read_ts} -> ChatReadDTO. The reader is
    // stamped server-side from the verified JWT sub (anti-spoof); the watermark is
    // monotonic (a stale ts is a server-side no-op). Returns the effective receipt.
    const wire = unwrap(
      await client.POST("/api/chat/mark-read", {
        body: { peer: mark.peer, last_read_ts: mark.lastReadTs },
      }),
    );
    return toChatRead(wire);
  },

  async listChatReads(peer: string): Promise<ChatReadReceipt[]> {
    // GET /api/chat/reads?with=<peer> -> ChatReadDTO[]. The FE reads the peer's
    // receipt to know how far the peer has read the owner's messages.
    const wire = unwrap(
      await client.GET("/api/chat/reads", {
        params: { query: { with: peer } },
      }),
    );
    return wire.map(toChatRead);
  },

  async listReplyCards(
    status: "waiting" | "answered" | "expired",
  ): Promise<ReplyCard[]> {
    // GET /api/reply-cards?status=&view=full -> ReplyCardDTO[] (T-a3e4).
    //
    // T-3f31 took the body / full options text OUT of the list wire, because
    // the AGENT-facing list_reply_cards tool shares this route and must stay
    // small (owner ruling: 卡只需要 title+決策). But the cockpit panes render
    // the FULL card (option chips, body, attachment refs), so this adapter used
    // to follow the light list with one GET /api/reply-cards/{id} PER ROW —
    // opening one waiting pane cost one ROUND TRIP PER WAITING CARD. `view=full`
    // serves the same pane, in the same order, as whole cards in ONE request.
    //
    // 🔴 The win is the ROUND TRIPS, not the bytes. Measured on a real
    // ocserverd waiting pane: 26 requests / 49,970 B → 1 request / 44,183 B —
    // 25 fewer round trips, but only 11.6% fewer bytes. Do not sell this as
    // saving bandwidth: on a slow link the latency is the whole cost, and a
    // full pane is very nearly the same size either way.
    //
    // The server order is preserved (waiting = longest-waiting first, answered =
    // last-24h newest answer first). RepliesPage re-sorts the waiting pane
    // newest-first for DISPLAY only (T-b07f) — the adapter still hands over
    // server order.
    //
    // ⚠️ `view` lives ONLY here, in the http seam: it is not an adapter concept
    // (mock has always returned whole cards, so parity is unchanged) and it is
    // deliberately absent from the list_reply_cards MCP tool, so agents cannot
    // ask for it. Do not lift it into the adapter signature.
    const rows = unwrap(
      await client.GET("/api/reply-cards", {
        params: { query: { status, view: "full" } },
      }),
    );
    // The response schema is a union (light rows | full cards) because ONE route
    // serves both projections; `view=full` is what selects the second arm, so
    // narrow to it here. Asserted on the wire, not just typed: the server test
    // TestListReplyCardsViewFullRowsEqualTheSingleCardResponse pins each row as
    // byte-identical to that card's own GET /api/reply-cards/{card_id}.
    return (rows as WireReplyCard[]).map(toReplyCard);
  },

  async getReplyCard(id: string): Promise<ReplyCard> {
    // GET /api/reply-cards/{card_id} -> ReplyCardDTO. B3's inline chat card
    // pulls the full card (options/status/answer) from the message's
    // meta.reply_card_id; unknown id → 404 (ApiError via middleware).
    const wire = unwrap(
      await client.GET("/api/reply-cards/{card_id}", {
        params: { path: { card_id: id } },
      }),
    );
    return toReplyCard(wire);
  },

  async getReplyCardCount(): Promise<ReplyCardCounts> {
    // GET /api/reply-cards/count -> {waiting, answered, expired}. Cheap counts
    // (the nav badge's waiting; the 等我回覆 page's recently-handled header
    // sum), refetched on every reply_card SSE delta without pulling the lists.
    const wire = unwrap(await client.GET("/api/reply-cards/count"));
    return {
      waiting: wire.waiting,
      answered: wire.answered,
      expired: wire.expired,
    };
  },

  async getChatUnreadCount(): Promise<number> {
    // GET /api/chat/unread-count -> {unread}. The 辦公室 nav red dot's cheap
    // count path (refetched on every "chat" / "chat_read" SSE delta without
    // pulling the roster).
    const wire = unwrap(await client.GET("/api/chat/unread-count"));
    return wire.unread;
  },

  async answerReplyCard(
    id: string,
    answer: ReplyCardAnswerInput,
  ): Promise<ReplyCard> {
    // POST /api/reply-cards/{card_id}/answer -> ReplyCardDTO (the one-shot
    // close; already-answered → 409, empty/out-of-range → 400, all thrown as
    // ApiError by the client middleware). Attachments ride the same input
    // shape as chat attachments.
    const wire = unwrap(
      await client.POST("/api/reply-cards/{card_id}/answer", {
        params: { path: { card_id: id } },
        body: toAnswerBody(answer),
      }),
    );
    return toReplyCard(wire);
  },

  async reanswerReplyCard(
    id: string,
    answer: ReplyCardAnswerInput,
  ): Promise<ReplyCard> {
    // PUT /api/reply-cards/{card_id}/answer -> ReplyCardDTO (重新決定: same
    // body + validation as POST; a waiting card is a 409). Status stays
    // answered; answered_ts re-stamps server-side.
    const wire = unwrap(
      await client.PUT("/api/reply-cards/{card_id}/answer", {
        params: { path: { card_id: id } },
        body: toAnswerBody(answer),
      }),
    );
    return toReplyCard(wire);
  },

  async expireReplyCard(id: string): Promise<ReplyCard> {
    // POST /api/reply-cards/{card_id}/expire -> ReplyCardDTO (標為過期 — the
    // terminal exit that is NOT an answer; no body). Callers: the card's own
    // AUTHOR (T-1b88, owner 2026-08-07 card rc-3ff94b116970 — revising T-6020,
    // which held this at the admin floor), the owner, or an admin agent. Someone
    // else's card → 403, answered / already-expired → 409, unknown id → 404
    // (ApiError via middleware). The cockpit only ever calls this as the owner.
    const wire = unwrap(
      await client.POST("/api/reply-cards/{card_id}/expire", {
        params: { path: { card_id: id } },
      }),
    );
    return toReplyCard(wire);
  },

  async listTasks(opts?: {
    open?: boolean;
    statuses?: string[];
  }): Promise<TaskView[]> {
    // GET /api/tasks -> TaskListItemDTO[] (LIGHT: no steps/description/inputs).
    // ?open=true (T-2b9d) drops the terminal rows server-side for the default
    // 未結束-only view; omitted → the full population (清除篩選 全部). ?statuses=
    // (T-a3e4) is the SET the 狀態 dropdown ticked, sent as one repeated param
    // per state (openapi-fetch's default form/explode serialisation, the shape
    // the spec declares) — the page asks for the rows it renders instead of
    // filtering a full download. An EMPTY set is omitted entirely: it means
    // 所有狀態, and sending nothing is exactly that. A card hydrates its heavy
    // detail on expand via getTask.
    const query: { open?: string; statuses?: string[] } = {};
    if (opts?.open) query.open = "true";
    if (opts?.statuses && opts.statuses.length > 0) {
      query.statuses = opts.statuses;
    }
    const wire = unwrap(await client.GET("/api/tasks", { params: { query } }));
    return wire.map(toTaskListItem);
  },

  async getTask(id: string): Promise<TaskView> {
    // GET /api/tasks/{task_id} -> TaskDTO (FULL: steps/description/inputs). The
    // per-card hydration path the 任務清單 uses when a card is expanded — the
    // list itself only carries the light projection.
    const wire = unwrap(
      await client.GET("/api/tasks/{task_id}", {
        params: { path: { task_id: id } },
      }),
    );
    return toTask(wire);
  },

  async getTaskStep(taskId: string, stepId: string): Promise<TaskStepDetailView> {
    // GET /api/tasks/{task_id}/steps/{step_id} -> TaskStepDetailDTO (T-66).
    // The ONE read that carries a step note's text: getTask reports each step's
    // note_size_chars and stopped carrying the note itself, so the card opens
    // this on demand. A step that belongs to another task 404s through the
    // client middleware as an ApiError.
    const wire = unwrap(
      await client.GET("/api/tasks/{task_id}/steps/{step_id}", {
        params: { path: { task_id: taskId, step_id: stepId } },
      }),
    );
    return toTaskStepDetail(wire);
  },

  async listTaskArtifacts(taskId: string): Promise<TaskArtifactView[]> {
    // GET /api/tasks/{task_id}/artifacts -> TaskArtifactListDTO (T-66). The ONE
    // read that carries an artifact's url/filename/mime/kind/is_image: getTask
    // carries an id+label INDEX and stopped carrying the rest, so anything that
    // DRAWS an artifact opens this. One call answers the whole ticket; an
    // unknown task 404s through the client middleware as an ApiError.
    const wire = unwrap(
      await client.GET("/api/tasks/{task_id}/artifacts", {
        params: { path: { task_id: taskId } },
      }),
    );
    return (wire.artifacts ?? []).map(toTaskArtifact);
  },

  async getTaskCount(): Promise<TaskCountView> {
    // GET /api/tasks/count -> {open, total}. The nav badge's cheap count path
    // (refetched on every "task" SSE delta without pulling the list); `total`
    // (T-a3e4) is the unfiltered count the 任務頁 words its empty state from.
    const wire = unwrap(await client.GET("/api/tasks/count"));
    return { open: wire.open, total: wire.total ?? 0 };
  },

  async terminateTask(id: string): Promise<TaskView> {
    // POST /api/tasks/{task_id}/terminate -> TaskDTO. The ONLY owner-side
    // status change (spec §3.7); non-terminal only (409 throws via the client
    // middleware). No body — the FE owns the double-confirm.
    const wire = unwrap(
      await client.POST("/api/tasks/{task_id}/terminate", {
        params: { path: { task_id: id } },
      }),
    );
    return toTask(wire);
  },

  async markTaskDuplicate(id: string, duplicateOf: string): Promise<TaskView> {
    // POST /api/tasks/{task_id}/duplicate {duplicate_of} -> TaskDTO. Marks the
    // task a duplicate of the original (T-02c9); a third terminal status. The
    // server enforces the depth-1 graph (self/already-duplicated/already-an-
    // original are all 409) and rejects a closed task (409) — all throw.
    const wire = unwrap(
      await client.POST("/api/tasks/{task_id}/duplicate", {
        params: { path: { task_id: id } },
        body: { duplicate_of: duplicateOf },
      }),
    );
    return toTask(wire);
  },

  async setTaskPriority(id: string, priority: string): Promise<void> {
    // POST /api/tasks/{task_id}/priority {priority} -> TaskPriorityReceiptDTO.
    // high|mid|low|frozen — freeze/unfreeze ride the same knob (spec §3.3); a
    // closed task is a 409, an out-of-vocabulary value a 422 (both throw).
    // The write answers with a bounded receipt (T-a98d), not the task; the
    // cockpit refetches, exactly as it already did.
    unwrap(
      await client.POST("/api/tasks/{task_id}/priority", {
        params: { path: { task_id: id } },
        body: { priority },
      }),
    );
  },

  async updateTaskDescription(
    id: string,
    description: string,
  ): Promise<TaskView> {
    // POST /api/tasks/{task_id}/description {description} -> TaskDTO (T-e271).
    // The field is ALWAYS sent, even when empty: the wire treats an absent
    // `description` as "change nothing" and an explicit "" as "clear it", so
    // omitting it on a clear would silently turn the write into a no-op that
    // still answers 200 with the old text.
    //
    // No 409 branch to document here — a closed task is accepted on purpose
    // (see the adapter's note); the faces that do throw are 404 (unknown task)
    // and 403 (a caller who is neither the executor nor admin-capable).
    const wire = unwrap(
      await client.POST("/api/tasks/{task_id}/description", {
        params: { path: { task_id: id } },
        body: { description },
      }),
    );
    return toTask(wire);
  },

  async updateTaskTitle(id: string, title: string): Promise<TaskView> {
    // POST /api/tasks/{task_id}/title {title} -> TaskDTO (T-2ebe). The field is
    // ALWAYS sent for the same reason the description twin always sends its
    // own: an absent `title` is "change nothing" on the wire, so omitting it
    // would turn a write into a silent no-op that still answers 200.
    //
    // 🔴 Unlike the twin, a blank title does NOT clear the field — the server
    // answers 400 `title must not be blank`, and the caller surfaces that. The
    // seam does not pre-empt the refusal by trimming to a no-op here: inventing
    // a local refusal would hide which door said no, and a local SUCCESS would
    // be a lie. The server also trims what it stores, so the task that comes
    // back may differ from what was sent by exactly that whitespace.
    //
    // Other faces: 404 (unknown task) and 403 (neither executor nor
    // admin-capable). A closed task is accepted on purpose.
    const wire = unwrap(
      await client.POST("/api/tasks/{task_id}/title", {
        params: { path: { task_id: id } },
        body: { title },
      }),
    );
    return toTask(wire);
  },

  async reassignTask(id: string, input: TaskReassignInput): Promise<TaskView> {
    // POST /api/tasks/{task_id}/reassign {target, note?} -> TaskDTO. The whole
    // handover is the server's (card expiry / step rewind / old-worker dismiss
    // / fresh mint / both-sides notice); the FE only names the target. A closed
    // task is a 409, a frozen one a 400, a bad member target a 400/409 — all
    // throw via the client middleware. The task lands in `reassigning`; the NEW
    // executor reports it back to in_progress.
    const wire = unwrap(
      await client.POST("/api/tasks/{task_id}/reassign", {
        params: { path: { task_id: id } },
        body: fromTaskReassignInput(input),
      }),
    );
    return toTask(wire);
  },

  async removeTaskArtifact(taskId: string, artifactId: string): Promise<void> {
    // DELETE /api/tasks/{task_id}/artifact/{artifact_id} ->
    // TaskArtifactReceiptDTO. The owner/admin un-pin (T-3dc5); unknown
    // task/artifact → 404, wrong-task → 400 (both throw via the client
    // middleware). The write answers with a bounded receipt (T-a98d), not the
    // task; the caller refetches. The live blob is left intact, but the artifact's
    // retained versions (and the blobs only they referenced) are deleted with it.
    unwrap(
      await client.DELETE("/api/tasks/{task_id}/artifact/{artifact_id}", {
        params: { path: { task_id: taskId, artifact_id: artifactId } },
      }),
    );
  },

  async listTaskArtifactVersions(
    taskId: string,
    artifactId: string,
  ): Promise<TaskArtifactVersionView[]> {
    // GET /api/tasks/{task_id}/artifact/{artifact_id}/history ->
    // TaskArtifactVersionDTO[], newest first, at most the retained depth (the
    // server trims). Cockpit-only (T-60): the agent that just replaced a
    // deliverable already knows what it replaced. Read-only — there is no
    // restore verb to pair with it. An artifact that was never replaced answers
    // [] rather than 404; unknown task/artifact → 404, wrong-task → 400 (all
    // throw through the client middleware).
    const wire = unwrap(
      await client.GET(
        "/api/tasks/{task_id}/artifact/{artifact_id}/history",
        { params: { path: { task_id: taskId, artifact_id: artifactId } } },
      ),
    );
    return wire.map(toTaskArtifactVersion);
  },

  async postTaskMessage(id: string, msg: TaskMessageInput): Promise<void> {
    // POST /api/tasks/{task_id}/message {body, attachments?} -> ChatMessageDTO.
    // The server posts ONE ordinary chat message owner → the executor with the
    // task context auto-attached in meta ({task_id, task_title, task_type}).
    // Unassigned executor → 409, empty message → 400 (both throw). The return
    // message is not needed here — the chat thread reconciles via its own
    // "chat" SSE topic.
    const attachments = msg.attachments ?? [];
    await client.POST("/api/tasks/{task_id}/message", {
      params: { path: { task_id: id } },
      body: {
        body: msg.body,
        ...(attachments.length > 0
          ? {
              attachments: attachments.map((a) => ({
                data_b64: a.dataB64,
                ...(a.filename ? { filename: a.filename } : {}),
                ...(a.mime ? { mime: a.mime } : {}),
              })),
            }
          : {}),
      },
    });
  },

  async listOutsourceWorkers(): Promise<OutsourceWorkerView[]> {
    // GET /api/outsource-workers -> OutsourceWorkerDTO[]. LIVE workers only —
    // released ones drop off (their tasks then render the bare 外包 label;
    // honest, never a fabricated codename).
    const wire = unwrap(await client.GET("/api/outsource-workers"));
    return wire.map(toOutsourceWorker);
  },

  async getOutsourceWorker(id: string): Promise<OutsourceWorkerView> {
    // GET /api/outsource-workers/{id} -> OutsourceWorkerDTO. The SAME projection
    // the list serves, for the detail panel's post-relocate refresh. Unknown /
    // released → 404 (unwrap throws ApiError; the panel self-heals to the roster).
    const wire = unwrap(
      await client.GET("/api/outsource-workers/{id}", {
        params: { path: { id } },
      }),
    );
    return toOutsourceWorker(wire);
  },

  async relocateWorker(
    id: string,
    machineId: string,
  ): Promise<OutsourceWorkerView> {
    // POST /api/outsource-workers/{id}/relocate {machine_id} -> OutsourceWorkerDTO
    // (改機器; admin-gated since P7c — the member relocate floor). Writes the
    // pinned placement, kills the current
    // session, and clears pacing so the next scheduler tick re-spawns on the
    // chosen machine (no lifecycle change). Returns the freshly-projected worker;
    // the outsource_worker SSE delta also fans so the list refetches.
    const wire = unwrap(
      await client.POST("/api/outsource-workers/{id}/relocate", {
        params: { path: { id } },
        body: { machine_id: machineId },
      }),
    );
    return toOutsourceWorker(wire);
  },

  async refocusWorker(id: string): Promise<OutsourceWorkerView> {
    // POST /api/outsource-workers/{id}/refocus -> OutsourceWorkerDTO (owner/admin-agent,
    // online-only 409). Graceful (T-ea82): stamps the handover + nudges the worker
    // to flush, then the server kills+re-spawns a fresh worker on the same task;
    // the outsource_worker SSE delta also fans so the list refetches.
    //
    // 🔴 THERE IS NO CEILING ON THIS ONE. Workers read the SAME judgement as
    // members (wire.go's winddownDeadlineOf → recycleGraceFor → winddownKindFor,
    // and autoHandoverWorker's in-flight arm), and 重新聚焦 is soft, so nothing
    // collects this epoch on a clock: the drivers are the worker's own stopped
    // report and the offline fallback. This comment used to claim "a flat
    // StoppingTimeoutSecs (~120s) ceiling — workers do NOT go through
    // recycleGraceFor", which has been false since T-fe5e and is more false now
    // that every member cause except the two 加速停止 arms (context_high and the
    // owner-pressed accelerated_stop) is soft too.
    const wire = unwrap(
      await client.POST("/api/outsource-workers/{id}/refocus", {
        params: { path: { id } },
      }),
    );
    return toOutsourceWorker(wire);
  },

  async stopWorker(id: string): Promise<OutsourceWorkerView> {
    // POST /api/outsource-workers/{id}/stop -> OutsourceWorkerDTO (owner/admin-agent).
    // Since T-ed79 this ASKS: it holds the worker down and shows it the 〈停止〉,
    // and the 收口 is the worker's own report_stopped. The kill moved to
    // forceStopWorker below.
    const wire = unwrap(
      await client.POST("/api/outsource-workers/{id}/stop", {
        params: { path: { id } },
      }),
    );
    return toOutsourceWorker(wire);
  },

  async acceleratedStopWorker(id: string): Promise<OutsourceWorkerView> {
    // POST /api/outsource-workers/{id}/accelerated-stop -> OutsourceWorkerDTO
    // (owner/admin-agent). The MIDDLE rung: puts an ALREADY-OPEN wind-down on the
    // clock and tells the worker. 409 when nothing is open.
    const wire = unwrap(
      await client.POST("/api/outsource-workers/{id}/accelerated-stop", {
        params: { path: { id } },
      }),
    );
    return toOutsourceWorker(wire);
  },

  async forceStopWorker(id: string): Promise<OutsourceWorkerView> {
    // POST /api/outsource-workers/{id}/force-stop -> OutsourceWorkerDTO
    // (owner/admin-agent). The THIRD rung: kill NOW, hold down, say nothing.
    const wire = unwrap(
      await client.POST("/api/outsource-workers/{id}/force-stop", {
        params: { path: { id } },
      }),
    );
    return toOutsourceWorker(wire);
  },

  async restartWorker(id: string): Promise<OutsourceWorkerView> {
    // POST /api/outsource-workers/{id}/restart -> OutsourceWorkerDTO (owner/admin-agent,
    // 409 only when the worker is actually alive — T-7526). Clears the stop and
    // re-dispatches; a worker whose session died on its own is revivable here.
    const wire = unwrap(
      await client.POST("/api/outsource-workers/{id}/restart", {
        params: { path: { id } },
      }),
    );
    return toOutsourceWorker(wire);
  },

  async setWorkerModel(
    id: string,
    patch: {
      runtime?: "claude" | "codex";
      model: string;
      effort?: string;
    },
  ): Promise<OutsourceWorkerView> {
    // POST /api/outsource-workers/{id}/model {model, effort?} -> OutsourceWorkerDTO
    // (owner/admin-agent). Active+online → kill+respawn now; otherwise persist for the
    // next spawn. model is always sent (blank ⇒ launcher default); effort only
    // when supplied (an absent field must not arrive as null).
    const body: {
      runtime?: "claude" | "codex";
      model: string;
      effort?: string;
    } = { model: patch.model };
    if (patch.runtime !== undefined) body.runtime = patch.runtime;
    if (patch.effort !== undefined) body.effort = patch.effort;
    const wire = unwrap(
      await client.POST("/api/outsource-workers/{id}/model", {
        params: { path: { id } },
        body,
      }),
    );
    return toOutsourceWorker(wire);
  },

  async getWorkerBootContext(id: string): Promise<string> {
    // GET /api/outsource-workers/{id}/boot-context -> WorkerBootContextDTO
    // (owner/admin-agent). The server re-runs the spawn fold over the CURRENT rows;
    // no token rides the response (a UI preview mints none).
    const wire = unwrap(
      await client.GET("/api/outsource-workers/{id}/boot-context", {
        params: { path: { id } },
      }),
    );
    return wire.context;
  },

  async listTaskTypes(): Promise<TaskTypeView[]> {
    // GET /api/task-manuals -> TaskManualListItemDTO[], narrowed to the
    // {typeKey, displayName, purpose} the type filter reads.
    //
    // T-1170: the `?view=list` escape hatch is GONE, because the light shape is
    // now what the route answers by default — an opt-in flag left the expensive
    // shape pointed at every naive caller, which is the whole reason the
    // default changed. The full-body read stays the per-type getTaskManual on
    // the settings detail page.
    const wire = unwrap(await client.GET("/api/task-manuals"));
    return wire.map(toTaskType);
  },

  async listTaskManuals(): Promise<TaskManualSummaryView[]> {
    // GET /api/task-manuals -> TaskManualDTO[] — the SAME wire read as
    // listTaskTypes. T-1170: this answer is the DIRECTORY (sop_md / learnings
    // absent, their char counts and caps present), so it is mapped to the
    // summary; the 任務定義 / 學習經驗 sub-pages read their document through
    // getTaskManual.
    const wire = unwrap(await client.GET("/api/task-manuals"));
    return wire.map(toTaskManualSummary);
  },

  async getTaskManual(typeKey: string): Promise<TaskManualView> {
    // GET /api/task-manuals/{type_key} -> TaskManualDTO (404 throws).
    const wire = unwrap(
      await client.GET("/api/task-manuals/{type_key}", {
        params: { path: { type_key: typeKey } },
      }),
    );
    return toTaskManual(wire);
  },

  async createTaskManual(displayName: string): Promise<TaskManualView> {
    // POST /api/task-manuals {display_name} -> TaskManualDTO (the blank
    // manual). T-fa76: the server MINTS the tm- type_key (echoed back on the
    // DTO) — type_key is deliberately NOT sent (that is the deprecated
    // legacy path). Blank name → 400 (throws ApiError). On the wire null
    // assignee = absent (the owner sets the assignee via the edit face;
    // agents may not carry it at all — 403).
    const wire = unwrap(
      await client.POST("/api/task-manuals", {
        body: { type_key: null, display_name: displayName, assignee: null },
      }),
    );
    return toTaskManual(wire);
  },

  async updateTaskManual(
    typeKey: string,
    patch: TaskManualPatch,
  ): Promise<TaskManualView> {
    // POST /api/task-manuals/{type_key} (partial edit) -> TaskManualDTO. On
    // the wire null = unchanged; assignee {} = unset (see fromTaskManualPatch).
    const wire = unwrap(
      await client.POST("/api/task-manuals/{type_key}", {
        params: { path: { type_key: typeKey } },
        body: fromTaskManualPatch(patch),
      }),
    );
    return toTaskManual(wire);
  },

  async deleteTaskManual(typeKey: string): Promise<void> {
    // DELETE /api/task-manuals/{type_key} -> delete receipt. OPEN tasks of
    // the type → 409 (throws — the UI surfaces the 先讓任務結束 message).
    unwrap(
      await client.DELETE("/api/task-manuals/{type_key}", {
        params: { path: { type_key: typeKey } },
      }),
    );
  },

  async listDocs(): Promise<DocSummaryView[]> {
    // GET /api/docs -> DocSummaryDTO[] (slug + title).
    const wire = unwrap(await client.GET("/api/docs"));
    return wire.map((d) => ({ slug: d.slug, title: d.title }));
  },

  async getDoc(slug: string): Promise<DocView> {
    // GET /api/docs/{slug} -> DocDTO (404 throws). markdown_md carries relative
    // image paths already rewritten to /api/docs/assets/ by the server.
    const wire = unwrap(
      await client.GET("/api/docs/{slug}", {
        params: { path: { slug } },
      }),
    );
    return { slug: wire.slug, title: wire.title, markdownMd: wire.markdown_md };
  },

  async getMonitoring(): Promise<MonitoringView> {
    // GET /api/monitoring -> MonitoringDTO.
    const wire = unwrap(await client.GET("/api/monitoring"));
    return toMonitoring(wire);
  },

  async patchAccount(id: string, patch: AliasPatch): Promise<void> {
    // PATCH /api/accounts/{id} {display_name} -> AliasDTO {id, display_name,
    // owner_id} (a NARROW object, not the monitoring row). We ignore the return:
    // the caller refetches monitoring for the fresh label. The client throws on
    // non-2xx (422 blank name) → the component catches and surfaces the error.
    await client.PATCH("/api/accounts/{account_id}", {
      params: { path: { account_id: id } },
      body: { display_name: patch.displayName },
    });
  },

  async patchMachine(id: string, patch: AliasPatch): Promise<void> {
    // PATCH /api/machines/{id} {display_name} -> AliasDTO (narrow; see above).
    // Return ignored; caller refetches monitoring. Throws on 422 (blank).
    await client.PATCH("/api/machines/{machine_id}", {
      params: { path: { machine_id: id } },
      body: { display_name: patch.displayName },
    });
  },

  async onboardMachine(
    displayName: string,
    opts?: OnboardOptions,
  ): Promise<OnboardResultView> {
    // POST /api/machines {display_name, ttl_days?} -> OnboardResultDTO
    // {member_id, machine_id, token, expires_in, boot_command, claim_code,
    // claim_expires_in}. boot_command embeds the short-lived single-use
    // claim_code (install.sh?code=), never the token. There is NO host
    // field anymore — a machine is created by display name only and the server
    // owns the opaque machine_id. Owner/mira governance token required (the
    // client middleware attaches the owner JWT; 401 bounces to login). ttl_days
    // is left OFF the body when absent so the server applies its own default.
    // SECURITY: the returned token/boot_command are secrets — never logged; the
    // UI renders boot_command into a copy control only.
    const body: { display_name: string; ttl_days?: number } = {
      display_name: displayName,
    };
    if (opts?.ttlDays !== undefined) body.ttl_days = opts.ttlDays;
    const wire = unwrap(await client.POST("/api/machines", { body }));
    return toOnboardResult(wire);
  },

  async listMachines(): Promise<MachineView[]> {
    // GET /api/machines -> WireMachine[] {machine_id, display_name, online}. The
    // machine registry the picker + machines panel read. Honest passthrough.
    const wire = unwrap(await client.GET("/api/machines"));
    return wire.map(toMachine);
  },

  async deleteMachine(memberId: string): Promise<DeleteResultView> {
    // DELETE /api/machines/{member_id} -> MachineDeleteResultDTO {member_id, machine_id,
    // removed}. A PURE roster soft-delete (delete ≠ uninstall ≠ stop): no warden
    // command is dispatched and there is NO teardown_command anymore. The path
    // param is the warden member_id (== machineId). Caller refetches afterwards
    // (the row drops).
    const wire = unwrap(
      await client.DELETE("/api/machines/{member_id}", {
        params: { path: { member_id: memberId } },
      }),
    );
    return toDeleteResult(wire);
  },

  async uninstallMachine(memberId: string): Promise<UninstallResultView> {
    // POST /api/machines/{member_id}/uninstall -> MachineUninstallResultDTO
    // {member_id, machine_id, dispatched}. Writes the owner intent desired_state="uninstall"
    // so the server reconcile arm drives the single `uninstall` RPC to the warden
    // (which runs `ocwarden uninstall` on its box). The record is KEPT
    // (re-installable) — the row does NOT drop. `dispatched` is TRUE when the
    // warden was online (RPC driven), FALSE when already offline. A non-2xx
    // (transport/gate) throws via the client. Caller refetches afterwards.
    const wire = unwrap(
      await client.POST("/api/machines/{member_id}/uninstall", {
        params: { path: { member_id: memberId } },
      }),
    );
    return toUninstallResult(wire);
  },

  async getMachineBootCommand(machineId: string): Promise<string> {
    // GET /api/machines/{machine_id}/boot-command -> BootCommandDTO {machine_id,
    // boot_command, token, expires_in, claim_code, claim_expires_in}. Re-mints a
    // fresh token + one-time claim code and returns the ready-to-run boot_command
    // (which embeds the CODE — install.sh?code= — never the token). Owner-gated
    // (the client middleware attaches the owner JWT; 401 bounces to login).
    // SECURITY: we return ONLY the boot_command string for the UI's copy
    // control — token/expires_in stay on the wire and are never logged.
    const wire = unwrap(
      await client.GET("/api/machines/{machine_id}/boot-command", {
        params: { path: { machine_id: machineId } },
      }),
    );
    return wire.boot_command;
  },

  async bootstrapOnServer(machineId: string): Promise<BootstrapResultView> {
    // POST /api/machines/{machine_id}/bootstrap-here -> BootstrapResultDTO
    // {machine_id, ok, exit_code, log}. Installs THIS machine's warden on the
    // server host in one click (owner/admin-agent). A non-2xx (transport/gate) throws
    // via the client; an install that RAN but failed returns ok=false with the
    // reason in `log` (the caller surfaces it, never swallows).
    const wire = unwrap(
      await client.POST("/api/machines/{machine_id}/bootstrap-here", {
        params: { path: { machine_id: machineId } },
      }),
    );
    return toBootstrapResult(wire);
  },

  async teardownOnServer(machineId: string): Promise<TeardownHereResultView> {
    // POST /api/machines/{machine_id}/teardown-here -> TeardownHereResultDTO
    // {machine_id, ok, exit_code, log, removed}. Tears THIS machine's warden down on
    // the server host in one click (owner/admin-agent). A non-2xx (transport/gate) throws
    // via the client; a teardown that RAN but failed returns ok=false with the reason
    // in `log` and removed=false (the daemon was NOT confirmed torn down, so the
    // member row is kept — the caller surfaces the log, never swallows it).
    const wire = unwrap(
      await client.POST("/api/machines/{machine_id}/teardown-here", {
        params: { path: { machine_id: machineId } },
      }),
    );
    return toTeardownHereResult(wire);
  },

  async getVersion(): Promise<VersionView> {
    // GET /api/version -> VersionDTO.
    const wire = unwrap(await client.GET("/api/version"));
    return toVersion(wire);
  },

  async checkRelease(): Promise<ReleaseCheckView> {
    // GET /api/release/check -> ReleaseCheckDTO. The explicit 檢查更新: the
    // server asks GitHub Releases synchronously and answers up_to_date /
    // update_available / unknown (GitHub unreachable) — always a 200; only
    // transport/gate failures reject.
    const wire = unwrap(await client.GET("/api/release/check"));
    return toReleaseCheck(wire);
  },

  async getBackupHealth(): Promise<BackupHealthView> {
    // GET /api/backup-health -> BackupHealthDTO (T-da06). Its own endpoint,
    // not a field on the monitoring fold: the topbar indicator is mounted
    // app-wide, and monitoring re-fetches on every telemetry event.
    const wire = unwrap(await client.GET("/api/backup-health"));
    return toBackupHealth(wire);
  },

  async getSigningKeys(): Promise<SigningKeyView[]> {
    // GET /api/auth/signing-keys -> SigningKeysDTO (T-62). Owner-gated and off
    // the MCP surface: it describes the key that authenticates every caller.
    const wire = unwrap(await client.GET("/api/auth/signing-keys"));
    return toSigningKeys(wire);
  },

  async rotateSigningKey(): Promise<SigningKeyView[]> {
    // POST /api/auth/signing-keys/rotate -> the ring AFTER the rotation.
    const wire = unwrap(await client.POST("/api/auth/signing-keys/rotate"));
    return toSigningKeys(wire);
  },

  async removeSigningKey(keyId: string): Promise<SigningKeyView[]> {
    // POST /api/auth/signing-keys/{key_id}/remove -> the ring AFTER the
    // removal. 409 when the key is still signing, 404 when the id is unknown;
    // both surface as a rejected promise the card renders as its own message.
    const wire = unwrap(
      await client.POST("/api/auth/signing-keys/{key_id}/remove", {
        params: { path: { key_id: keyId } },
      }),
    );
    return toSigningKeys(wire);
  },

  async getAuthStatus(): Promise<AuthStatusView> {
    // GET /api/auth/status (PUBLIC) -> AuthStatusDTO. Rides the typed client
    // (a public route never 401s, so the auth-expired middleware is inert).
    //
    // `mfa_required` is OPTIONAL on the wire so an older server keeps working;
    // absent means no second factor, which is the pre-MFA behaviour exactly.
    const wire = unwrap(await client.GET("/api/auth/status"));
    return {
      passwordSet: wire.password_set,
      mfaRequired: wire.mfa_required ?? false,
    };
  },

  async getMfaState(): Promise<MfaStateView> {
    // Owner-gated, so the typed client is right: a 401 here really does mean the
    // session died, and bouncing the auth wall is the honest reaction.
    const wire = unwrap(await client.GET("/api/auth/mfa"));
    return { offered: wire.offered, enrolled: wire.enrolled };
  },

  async setMfaOffered(offered: boolean): Promise<MfaStateView> {
    const wire = unwrap(
      await client.POST("/api/auth/mfa/offer", { body: { offered } }),
    );
    return { offered: wire.offered, enrolled: wire.enrolled };
  },

  async enrollMfa(): Promise<MfaEnrollView> {
    // POST /api/auth/mfa/enroll (owner-gated) -> MfaStateDTO carrying the
    // PENDING secret. Rides the typed client: this one IS owner-gated, so a 401
    // here genuinely means the session died and bouncing the auth wall is right.
    const wire = unwrap(await client.POST("/api/auth/mfa/enroll"));
    return {
      secret: wire.secret ?? "",
      otpauthUri: wire.otpauth_uri ?? "",
    };
  },

  async activateMfa(password: string, code: string): Promise<void> {
    // POST /api/auth/mfa/activate. HAND-WRITTEN via credentialPost like the
    // other credential seams: a wrong password or code is an inline form error,
    // and the typed client's middleware would turn its 401 into a logout instead.
    await credentialPost<unknown>(
      "/api/auth/mfa/activate",
      { password, code },
      ownerToken(),
    );
  },

  async disableMfa(password: string, code: string): Promise<void> {
    // POST /api/auth/mfa/disable — same reasoning as activateMfa: the 401 here
    // means "wrong password or code", never "your session expired".
    await credentialPost<unknown>(
      "/api/auth/mfa/disable",
      { password, code },
      ownerToken(),
    );
  },

  async setPassword(password: string, claimToken: string): Promise<void> {
    // POST /api/auth/set-password (PUBLIC, claim-token gated) -> TokenDTO.
    // HAND-WRITTEN like auth.ts login(): the typed client's middleware turns
    // EVERY 401 into clear-token + oc-auth-expired, but a wrong claim token
    // must surface as an inline form error, never bounce the auth wall.
    const data = await credentialPost("/api/auth/set-password", {
      password,
      claim_token: claimToken,
    });
    setToken(data.token);
  },

  async changePassword(
    currentPassword: string,
    newPassword: string,
  ): Promise<void> {
    // POST /api/auth/change-password (owner-gated) -> TokenDTO. HAND-WRITTEN
    // for the same reason as setPassword: a wrong CURRENT password is a 401
    // that must stay an inline form error — the client middleware would log
    // the owner out over a typo. The server revokes every pre-change owner
    // session; persisting the fresh token keeps THIS session alive.
    const data = await credentialPost(
      "/api/auth/change-password",
      { current_password: currentPassword, new_password: newPassword },
      ownerToken(),
    );
    setToken(data.token);
  },

  async getServerSettings(): Promise<ServerSettingsView> {
    // GET /api/settings -> SettingsDTO.
    const wire = unwrap(await client.GET("/api/settings"));
    return toServerSettings(wire);
  },

  async patchServerSettings(
    patch: ServerSettingsPatch,
  ): Promise<ServerSettingsView> {
    // PATCH /api/settings {owner_token_ttl?, agent_token_ttl?, handover_pct?, outsource_max_parallel?}
    // -> SettingsDTO (the settings after the change — durable + live
    // immediately; an outsource_max_parallel outside -1..20 is a 422; -1 = 無限).
    const body: {
      owner_token_ttl?: number;
      agent_token_ttl?: number;
      handover_pct?: number;
      notice_pct?: number;
      codex_notice_round?: number;
      codex_compaction_threshold?: number;
      monitoring_refresh_seconds?: number;
      accelerated_grace_secs?: number;
      outsource_max_parallel?: number;
      doc_cap_chars_duty?: number;
      doc_cap_chars_insight?: number;
      doc_cap_chars_learning?: number;
      doc_cap_chars_manual_sop?: number;
      doc_cap_chars_manual_learnings?: number;
      doc_cap_chars_system_interaction?: number;
      doc_cap_chars_boot_sequence?: number;
      doc_cap_chars_offboard?: number;
      chat_budget_chars?: number;
      backup_retain?: number;
      updater_receive_beta?: boolean;
      updater_auto_update?: boolean;
      org_name?: string;
      owner_name?: string;
      push_contact_email?: string;
      display_theme?: string;
      display_language?: string;
      display_wide?: boolean;
      lore_enabled?: boolean;
      onboarding_dismissed?: boolean;
    } = {};
    if (patch.ownerTokenTtl !== undefined)
      body.owner_token_ttl = patch.ownerTokenTtl;
    if (patch.agentTokenTtl !== undefined)
      body.agent_token_ttl = patch.agentTokenTtl;
    if (patch.handoverPct !== undefined) body.handover_pct = patch.handoverPct;
    if (patch.noticePct !== undefined) body.notice_pct = patch.noticePct;
    if (patch.codexNoticeRound !== undefined)
      body.codex_notice_round = patch.codexNoticeRound;
    if (patch.codexCompactionThreshold !== undefined)
      body.codex_compaction_threshold = patch.codexCompactionThreshold;
    if (patch.monitoringRefreshSeconds !== undefined)
      body.monitoring_refresh_seconds = patch.monitoringRefreshSeconds;
    if (patch.acceleratedGraceSecs !== undefined)
      body.accelerated_grace_secs = patch.acceleratedGraceSecs;
    if (patch.outsourceMaxParallel !== undefined) {
      body.outsource_max_parallel = patch.outsourceMaxParallel;
    }
    if (patch.docCapCharsDuty !== undefined) {
      body.doc_cap_chars_duty = patch.docCapCharsDuty;
    }
    if (patch.docCapCharsInsight !== undefined) {
      body.doc_cap_chars_insight = patch.docCapCharsInsight;
    }
    if (patch.docCapCharsLearning !== undefined) {
      body.doc_cap_chars_learning = patch.docCapCharsLearning;
    }
    if (patch.docCapCharsManualSop !== undefined) {
      body.doc_cap_chars_manual_sop = patch.docCapCharsManualSop;
    }
    if (patch.docCapCharsManualLearnings !== undefined) {
      body.doc_cap_chars_manual_learnings = patch.docCapCharsManualLearnings;
    }
    if (patch.docCapCharsSystemInteraction !== undefined) {
      body.doc_cap_chars_system_interaction =
        patch.docCapCharsSystemInteraction;
    }
    if (patch.docCapCharsBootSequence !== undefined) {
      body.doc_cap_chars_boot_sequence = patch.docCapCharsBootSequence;
    }
    if (patch.docCapCharsOffboard !== undefined) {
      body.doc_cap_chars_offboard = patch.docCapCharsOffboard;
    }
    if (patch.chatBudgetChars !== undefined) {
      body.chat_budget_chars = patch.chatBudgetChars;
    }
    if (patch.backupRetain !== undefined) {
      body.backup_retain = patch.backupRetain;
    }
    if (patch.updaterReceiveBeta !== undefined) {
      body.updater_receive_beta = patch.updaterReceiveBeta;
    }
    if (patch.updaterAutoUpdate !== undefined) {
      body.updater_auto_update = patch.updaterAutoUpdate;
    }
    if (patch.orgName !== undefined) body.org_name = patch.orgName;
    if (patch.ownerName !== undefined) body.owner_name = patch.ownerName;
    if (patch.pushContactEmail !== undefined)
      body.push_contact_email = patch.pushContactEmail;
    if (patch.displayTheme !== undefined)
      body.display_theme = patch.displayTheme;
    if (patch.displayLanguage !== undefined) {
      body.display_language = patch.displayLanguage;
    }
    if (patch.displayWide !== undefined) {
      body.display_wide = patch.displayWide;
    }
    if (patch.loreEnabled !== undefined) {
      body.lore_enabled = patch.loreEnabled;
    }
    if (patch.onboardingDismissed !== undefined) {
      body.onboarding_dismissed = patch.onboardingDismissed;
    }
    const wire = unwrap(await client.PATCH("/api/settings", { body }));
    return toServerSettings(wire);
  },

  async fetchThemeFromLink(url: string): Promise<string> {
    // POST /api/theme/fetch {url} -> {content} (T-29c7). `content` is the RAW
    // text the link served; it goes straight to the caller so the shared
    // parseImportedBundle does the parsing. Parsing here would put a second
    // theme parser on the import path.
    //
    // No pre-flight url check on this side, deliberately: the server's format
    // rule is the only one, and a stricter client rule would refuse links the
    // server accepts. 422 (bad url / too large / not a theme) and 502
    // (unreachable link) both throw via the client middleware.
    const wire = unwrap(
      await client.POST("/api/theme/fetch", { body: { url } }),
    );
    return wire.content;
  },

  async listThemes(): Promise<ThemeListItem[]> {
    // GET /api/themes -> ThemeListItemDTO[] (T-83ef) — id + name ONLY. The
    // bundles are deliberately NOT here: a theme embeds its images, so the
    // whole set is hundreds of KB to MB, and that payload is what this
    // resource exists to stop serving. One theme in full = getTheme.
    const wire = unwrap(await client.GET("/api/themes"));
    return wire.map(toThemeListItem);
  },

  async getTheme(id: string): Promise<ThemeBundle> {
    // GET /api/themes/{theme_id} -> ThemeBundleDTO (unknown id → 404, thrown
    // as ApiError by the client middleware).
    const wire = unwrap(
      await client.GET("/api/themes/{theme_id}", {
        params: { path: { theme_id: id } },
      }),
    );
    return toThemeBundle(wire);
  },

  async putTheme(bundle: ThemeBundle): Promise<ThemeWriteReceipt> {
    // PUT /api/themes/{theme_id} {ThemeBundleDTO} -> ThemeWriteReceiptDTO.
    // The path key IS the bundle's own id, so the server's "they must match"
    // rule (422 otherwise) cannot be broken from here. The answer is the
    // receipt, never the bundle echoed back — echoing would resend the
    // embedded images, the payload this split removes. 422 (validator /
    // create-past-cap) throws and nothing is written.
    const wire = unwrap(
      await client.PUT("/api/themes/{theme_id}", {
        params: { path: { theme_id: bundle.id } },
        body: bundle,
      }),
    );
    return toThemeWriteReceipt(wire);
  },

  async deleteTheme(id: string): Promise<ThemeDeleteResult> {
    // DELETE /api/themes/{theme_id} -> ThemeDeleteResultDTO (unknown id →
    // 404, throws). Deleting the ACTIVE theme resets display_theme to "" in
    // the same request; display_theme_reset reports it, so the caller does not
    // re-read settings to notice.
    const wire = unwrap(
      await client.DELETE("/api/themes/{theme_id}", {
        params: { path: { theme_id: id } },
      }),
    );
    return toThemeDeleteResult(wire);
  },

  async getPushPublicKey(): Promise<string> {
    const wire = unwrap(await client.GET("/api/push/public-key"));
    return wire.public_key;
  },

  async savePushSubscription(
    subscription: PushSubscriptionInput,
  ): Promise<void> {
    unwrap(
      await client.POST("/api/push/subscription", {
        body: {
          endpoint: subscription.endpoint,
          expiration_time: subscription.expirationTime,
          keys: subscription.keys,
        },
      }),
    );
  },

  async removePushSubscription(endpoint: string): Promise<void> {
    unwrap(
      await client.DELETE("/api/push/subscription", { body: { endpoint } }),
    );
  },

  async triggerUpgrade(): Promise<void> {
    // POST /api/update/upgrade — the owner's EXPLICIT trigger. A 200 means
    // the verified swap already LANDED and the server is restarting (the
    // card then watches /api/version for the new git_sha). Non-2xx rejects
    // as ApiError (409 preconditions / 502 download-verify-swap failures —
    // the old build keeps serving) — the card surfaces `.serverMessage`.
    unwrap(await client.POST("/api/update/upgrade"));
  },

  async getGlobalContext(): Promise<GlobalContextView> {
    // GET /api/global-context -> GlobalContextDTO — the 使用者自訂 (user-custom)
    // ADDITIVE block of the 3-block boot context (empty text/is_default=true when
    // never written). The other two blocks — system-interaction and
    // boot-sequence — became editable in T-791e and have their own routes; see
    // getBootDoc below.
    const wire = unwrap(await client.GET("/api/global-context"));
    return toGlobalContext(wire);
  },

  async saveGlobalContext(text: string): Promise<GlobalContextView> {
    // POST /api/global-context {text} -> GlobalContextDTO (whole-block replace,
    // isDefault=false). NOTE the POST verb — the frozen route surface
    // registers POST, not PUT; a PUT here 405s against the real backend — and
    // is now ALSO a compile error (the schema's /api/global-context has no put).
    const wire = unwrap(
      // allow_shrink: see saveLessons — the T-2d99 wipe guard targets blind
      // agent write-backs; the owner clearing this textarea is explicit intent.
      await client.POST("/api/global-context", {
        body: { text, allow_shrink: true },
      }),
    );
    return toGlobalContext(wire);
  },

  async resetGlobalContext(): Promise<GlobalContextView> {
    // POST /api/global-context/reset -> GlobalContextDTO (idempotent tombstone →
    // empty/is_default=true). NOTE: a dedicated POST reset route, NOT a DELETE on
    // the doc path (405 against the real backend, compile error against schema).
    const wire = unwrap(await client.POST("/api/global-context/reset"));
    return toGlobalContext(wire);
  },

  // ── boot-context / lifecycle documents (T-791e, T-3201) ─────────────────
  // ONE ROUTE FAMILY, and that is the point. These methods used to branch on
  // `kind` because the named routes had two different shapes (a singleton with
  // no key segment, and a keyed one) — composing one template string for both
  // is what produced the 404 they shipped with. T-3201 added the generic
  // `/api/boot-docs/{kind}/{key}` family, so the branch is gone: every
  // document, editable or read-only, is addressed the same way, and a tenth
  // document needs no code here at all.
  //
  // The named routes (/api/system-interaction, /api/boot-sequence/{runtime_key},
  // /api/offboard) still exist on the server for MCP callers; the cockpit no
  // longer uses them, so there is exactly one frontend path to test.

  async getBootDoc(kind: BootDocKind, key: string): Promise<BootDocView> {
    // 404 (not 400) for an unknown kind/key: the pair addresses A DOCUMENT, and
    // a document this server does not have is not found.
    return toBootDoc(
      unwrap(
        await client.GET("/api/boot-docs/{kind}/{key}", {
          params: { path: { kind, key } },
        }),
      ),
    );
  },

  async saveBootDoc(
    kind: BootDocKind,
    key: string,
    body: string,
  ): Promise<BootDocView> {
    // Replace the EDITABLE HALF, POST — same verb contract as
    // /api/global-context: NOT a PUT and NOT a DELETE-then-write.
    //
    // 🔴 `body`, NOT `text` (T-3201). The wire has no field for the read-only
    // head at all: the server joins the shipped one back on, so the cockpit
    // never composes the two halves and cannot express an edit to the one it
    // may not touch. Send back the `body` the read handed over.
    //
    // allow_shrink stays FALSE here, the opposite of saveGlobalContext. There
    // the owner clearing a textarea of their own additions is explicit intent
    // worth honouring; here the same gesture ships agents a boot sequence with
    // no instructions, and the server's refusal names the recovery path (reset
    // to the shipped default) instead. Emptying is not what this surface is
    // for — the 還原出廠版 button is.
    //
    // A read-only document refuses this with 405, and the refusal says what the
    // document IS rather than that the caller lacks a permission.
    return toBootDoc(
      unwrap(
        await client.POST("/api/boot-docs/{kind}/{key}", {
          params: { path: { kind, key } },
          body: { body, allow_shrink: false },
        }),
      ),
    );
  },

  async resetBootDoc(kind: BootDocKind, key: string): Promise<BootDocView> {
    return toBootDoc(
      unwrap(
        await client.POST("/api/boot-docs/{kind}/{key}/reset", {
          params: { path: { kind, key } },
        }),
      ),
    );
  },

  async listDocumentHistory(
    kind: DocumentKind,
    key: string,
  ): Promise<DocumentHistoryEntryView[]> {
    // GET /api/document-history/{kind}/{key} -> DocumentHistoryDTO[], newest
    // first, at most 3 (the server prunes). `key` carries the "::" composite
    // for lessons verbatim — openapi-fetch encodes it as one path segment.
    //
    // T-1170: the answer IS the directory — `field_chars` + `tombstoned`, no
    // text at all. A caller that wants a revision's prose names it through
    // `getDocumentRevision`.
    const wire = unwrap(
      await client.GET("/api/document-history/{kind}/{key}", {
        params: { path: { kind, key } },
      }),
    );
    return wire.map(toDocumentHistoryEntry);
  },

  async getDocumentRevision(
    kind: DocumentKind,
    key: string,
    id: number,
  ): Promise<DocumentRevisionView> {
    // GET /api/document-history/{kind}/{key}/{id} -> DocumentHistoryVersionDTO.
    // The ONE document-history read that carries text (T-1170): the list is a
    // picker, and this is the revision the reader picked. A pruned or unknown
    // id 404s exactly where the restore of that id would, so the reader can say
    // "this version could not be read" instead of drawing an empty document
    // next to a destructive button.
    const wire = unwrap(
      await client.GET("/api/document-history/{kind}/{key}/{id}", {
        params: { path: { kind, key, id } },
      }),
    );
    return toDocumentRevision(wire);
  },

  async getDocumentSeed(
    kind: DocumentKind,
    key: string,
  ): Promise<DocumentSeedView> {
    // GET /api/document-history/{kind}/{key}/seed -> DocumentSeedDTO. Reading
    // only — it is what lets 初始版本 be compared before it is restored. A
    // document with no shipped default rejects with a 404 ApiError; the caller
    // treats that as "there is nothing to compare", never as "the default is
    // empty" (for the global block, empty IS the default and comes back 200).
    const wire = unwrap(
      await client.GET("/api/document-history/{kind}/{key}/seed", {
        params: { path: { kind, key } },
      }),
    );
    return toDocumentSeed(wire);
  },

  async getDiff(params: DiffParams): Promise<DiffPairView> {
    // GET /api/diff?before=&after=[&label_before=][&label_after=][&sig=] — the
    // whole comparison in one answer. Hand-written rather than routed through
    // the typed client, because the signed flavour is answered with NO session
    // and must not be able to log the owner out; api/diff.ts owns that reason
    // and the response shape.
    return fetchDiffPair(params);
  },

  async getDiffShareLink(params: DiffParams): Promise<string> {
    // GET /api/diff/share-link?before=&after=[&label_before=][&label_after=]
    // -> {url}: the /diff page path carrying the server's ?sig= HMAC over all
    // four values. Unlike getDiff this one DOES ride the typed client — it is
    // answered only for a session, so a 401 here really is a dead session and
    // the middleware's reading of it is the right one.
    //
    // `params.sig` is deliberately not forwarded: the signature is the OUTPUT.
    // Sending one back would ask the server to sign a query that includes a
    // signature, which is not a query this route declares.
    const wire = unwrap(
      await client.GET("/api/diff/share-link", {
        params: {
          query: {
            before: params.before,
            after: params.after,
            ...(params.labelBefore ? { label_before: params.labelBefore } : {}),
            ...(params.labelAfter ? { label_after: params.labelAfter } : {}),
          },
        },
      }),
    );
    return wire.url;
  },

  async restoreDocumentHistory(
    kind: DocumentKind,
    key: string,
    id: number,
  ): Promise<DocumentHistoryView> {
    // POST /api/document-history/{kind}/{key}/{id}/restore -> the restored
    // revision DTO. This OVERWRITES the live document; a 404 (pruned id) and a
    // 400 (restoring would breach the doc size cap) both reject as ApiError so
    // the card can surface an honest failure instead of a silent no-op.
    const wire = unwrap(
      await client.POST("/api/document-history/{kind}/{key}/{id}/restore", {
        params: { path: { kind, key, id } },
      }),
    );
    return toDocumentHistory(wire);
  },

  async listRoles(): Promise<RoleSummaryView[]> {
    // GET /api/roles -> RoleDefListItemDTO[]. T-1170: the roster answer is the
    // DIRECTORY (no definition_md, only size_chars + cap_chars), so it maps to
    // the summary; the role page reads its document through getRole.
    const wire = unwrap(await client.GET("/api/roles"));
    return wire.map(toRoleSummary);
  },

  async getRole(key: string): Promise<RoleDefView> {
    // GET /api/roles/{key} -> RoleDefDTO
    const wire = unwrap(
      await client.GET("/api/roles/{role}", {
        params: { path: { role: key } },
      }),
    );
    return toRoleDef(wire);
  },

  async saveRole(key: string, patch: RolePatch): Promise<RoleDefView> {
    // POST /api/roles/{key} {name?, definition_md?} -> RoleDefDTO. View model uses
    // camelCase (definitionMd); the wire body is snake_case (RoleDefUpdateDTO).
    const wire = unwrap(
      await client.POST("/api/roles/{role}", {
        params: { path: { role: key } },
        body: { name: patch.name, definition_md: patch.definitionMd },
      }),
    );
    return toRoleDef(wire);
  },

  async resetRole(key: string): Promise<RoleDefView> {
    // POST /api/roles/{key}/reset -> RoleDefDTO (idempotent tombstone → seed).
    // NOTE the POST-reset route — the old DELETE verb here never matched the
    // route table (405), and DELETE /api/roles/{key} is now the HARD custom-role
    // delete (M2-2), a destructive different verb.
    const wire = unwrap(
      await client.POST("/api/roles/{role}/reset", {
        params: { path: { role: key } },
      }),
    );
    return toRoleDef(wire);
  },

  async createRole(input: RoleCreateInput): Promise<RoleCreateResult> {
    // POST /api/roles {name, member_name?, model?, effort?} ->
    // RoleCreateResultDTO {role, member}. One custom role + its ONE founding
    // member per call; the server mints both ids; the member starts offline.
    // member_name omitted ⇒ the server picks a fresh pool name (M2 隨機成員名).
    const body: {
      name: string;
      member_name?: string;
      runtime?: "claude" | "codex";
      model?: string;
      effort?: string;
    } = { name: input.name };
    if (input.memberName !== undefined) body.member_name = input.memberName;
    if (input.runtime !== undefined) body.runtime = input.runtime;
    if (input.model !== undefined) body.model = input.model;
    if (input.effort !== undefined) body.effort = input.effort;
    const wire = unwrap(await client.POST("/api/roles", { body }));
    return { role: toRoleDef(wire.role), member: toMember(wire.member) };
  },

  async deleteRole(key: string): Promise<void> {
    // DELETE /api/roles/{key} -> RoleDeleteResultDTO. HARD cascade delete of a
    // CUSTOM role (seed → 403, online member → 409 — both throw an ApiError via
    // the client middleware; the caller branches on `.status` via isHttpStatus
    // (api/errors.ts) to surface 「有成員在線上，無法刪除」). The receipt counts
    // are not needed by the UI — the caller refetches roles + members.
    await client.DELETE("/api/roles/{role}", {
      params: { path: { role: key } },
    });
  },

  async getBootstrap(role: string): Promise<BootstrapView> {
    // POST /api/bootstrap {role} -> BootstrapDTO. We send ONLY `role` (no
    // member_id) so the server mints no token (token=null) — a UI preview must
    // never receive an agent JWT. toBootstrap drops token from the view anyway.
    const wire = unwrap(
      await client.POST("/api/bootstrap", { body: { role } }),
    );
    return toBootstrap(wire);
  },

  async getLessons(roleKey: string): Promise<LessonsView> {
    // GET /api/lessons/{role_key} -> LessonsDTO (folded overlay ⊕ seed).
    // PER-ROLE doc, and role_key is the WHOLE address: T-2 removed the
    // task_type axis.
    const wire = unwrap(
      await client.GET("/api/lessons/{role_key}", {
        params: { path: { role_key: roleKey } },
      }),
    );
    return toLessons(wire);
  },

  async saveLessons(roleKey: string, text: string): Promise<LessonsView> {
    // POST /api/lessons/{role_key} {text} -> LessonsDTO (folded,
    // isDefault=false). Whole-doc replace matching the backend
    // `handle_replace_lessons`. NOTE the POST verb — do NOT copy the
    // global-context save's PUT/DELETE, which mismatch this contract. PER-ROLE
    // doc addressed by role_key alone. WRITE authz is per-role
    // and keyed on the PRINCIPAL CLASS, not the token scope (T-5336): a caller
    // at or above admin_agent — the owner (this UI's scope) and the admin agent
    // — may write ANY role; every other agent may write only its own role.
    const wire = unwrap(
      await client.POST("/api/lessons/{role_key}", {
        params: { path: { role_key: roleKey } },
        // allow_shrink: the server's T-2d99 wipe guard refuses a non-empty →
        // empty whole-doc replace unless the caller says so explicitly. That
        // guard exists for BLIND agent write-backs; here a human is looking at
        // the editor they just cleared, so the intent is already explicit.
        body: { text, allow_shrink: true },
      }),
    );
    return toLessons(wire);
  },

  async getInsight(roleKey: string): Promise<InsightView> {
    // GET /api/insight/{role_key} -> InsightDTO (T-3809). PER-ROLE doc keyed on
    // the BARE role_key, and there
    // IS a PER-ROLE file seed (T-e1e3). An untouched doc for a role that ships
    // one — today only `assistant`, from seeds/insight_assistant.md — comes
    // back with the FACTORY text and is_default true; only a role with no seed
    // file reads text "" with is_default true.
    // READ is unrestricted by owner ruling — insight is SEPARATE, not private.
    const wire = unwrap(
      await client.GET("/api/insight/{role_key}", {
        params: { path: { role_key: roleKey } },
      }),
    );
    return toInsight(wire);
  },

  async saveInsight(roleKey: string, text: string): Promise<InsightView> {
    // POST /api/insight/{role_key} {text} -> InsightDTO (folded,
    // isDefault=false). Same POST-verb contract as saveLessons — do NOT copy the
    // global-context save's PUT/DELETE.
    const wire = unwrap(
      await client.POST("/api/insight/{role_key}", {
        params: { path: { role_key: roleKey } },
        // allow_shrink: identical reasoning to saveLessons — the server's wipe
        // guard targets BLIND agent write-backs, and here a human is looking at
        // the editor they just cleared, so the intent is already explicit. The
        // doc.cap_chars.insight cap is checked UNCONDITIONALLY and this does not bypass
        // it; allow_shrink governs the opposite direction.
        body: { text, allow_shrink: true },
      }),
    );
    return toInsight(wire);
  },

  async resetInsight(roleKey: string): Promise<InsightView> {
    // POST /api/insight/{role_key}/reset -> InsightDTO (idempotent tombstone →
    // the folded read is the per-role file seed again, isDefault true). Same
    // POST-reset shape as resetRole; a role with no seed file 404s.
    const wire = unwrap(
      await client.POST("/api/insight/{role_key}/reset", {
        params: { path: { role_key: roleKey } },
      }),
    );
    return toInsight(wire);
  },

  subscribeEvents(
    onTopic: (topic: string, delta?: SseDelta) => void,
  ): () => void {
    // GET /api/events (SSE downlink). PERMANENTLY HAND-WRITTEN — an EventSource,
    // not a fetch, so no OpenAPI runtime client can generate it. EventSource
    // cannot set an Authorization header, so the owner JWT rides as a ?token=
    // query param (server auth path accepts it; the param is OUTSIDE the OpenAPI
    // schema — pinned only by service/auth.py + this comment).
    // Reconcile-by-refetch: each event carries a `topic`; we hand the topic name
    // to the caller, which refetches the resource (never merges the payload).
    // No token → skip the subscription (honest: gated, would 401).
    //
    // ALL subscribers share the ONE module-level EventSource (see the shared
    // SSE downlink block above) — never one connection per subscriber, which
    // exhausted the browser's per-host connection pool.
    const t = ownerToken();
    if (!t) return () => {};
    // Wrap the callback so the SAME function subscribed twice (two mounts) is
    // two independent subscriptions, not one Set entry killed by either
    // unsubscribe. The wrapper also makes unsubscribe naturally idempotent.
    const sub = (topic: string, delta?: SseDelta) => onTopic(topic, delta);
    sseSubscribers.add(sub);
    ensureSseSource();
    return () => {
      sseSubscribers.delete(sub);
      // NOTE the missing `&& sseSource`: between a permanent failure and the
      // next retry there IS no EventSource, and the teardown still has to run —
      // otherwise the pending retry timer and the foreground listener outlive
      // the last subscriber and reopen a connection nobody is listening to.
      if (sseSubscribers.size === 0) {
        cancelSseReconnect();
        sseRetryAttempt = 0;
        // The whole teardown happens BEFORE the "idle" broadcast; measured, a
        // listener throwing on "idle" used to leave the EventSource open and the
        // foreground listeners attached. Same status as the ordering in
        // `es.onerror` above, including the same expiry condition: no test can
        // separate this from the reverse order WHILE `setSseState` isolates its
        // callbacks, and it becomes the only defence — and so test-worthy — the
        // moment that isolation goes.
        if (sseSource) sseSource.close();
        sseSource = null;
        // Tear down the foreground-restore listeners with the connection so a
        // visibilitychange/focus never fans a resync onto an empty subscriber
        // set (and nothing leaks across a close→reopen cycle).
        if (sseVisibilityHandler && typeof document !== "undefined") {
          document.removeEventListener(
            "visibilitychange",
            sseVisibilityHandler,
          );
          if (typeof window !== "undefined") {
            window.removeEventListener("focus", sseVisibilityHandler);
          }
        }
        sseVisibilityHandler = null;
        setSseState("idle");
      }
    };
  },

  // ── T-33 傳承 (lore), read side ────────────────────────────────────────
  async searchLore(input: LoreSearchInput = {}): Promise<LoreSearchView> {
    // POST /api/lore/search -> LoreSearchResultDTO.
    //
    // 🔴 A POST for a READ, and that is the contract, not a slip: every
    // selection condition is a BODY key, because an undeclared body key is
    // refused 422 BY NAME while an undeclared query parameter is silently
    // ignored on every route this station serves. A mistyped condition on the
    // query side would hand back a plausible answer to a question nobody
    // asked, and nothing would report it.
    //
    // Only the fields the caller actually set are sent. `limit` is 1..100 and
    // out of range is REFUSED rather than clamped, so passing a caller's 0
    // through would turn "I did not choose" into a rejected request.
    const body: Partial<WireLoreSearchRequest> = {};
    if (input.subject !== undefined) body.subject = input.subject;
    if (input.actions !== undefined) body.actions = input.actions;
    if (input.query !== undefined) body.query = input.query;
    if (input.limit !== undefined) body.limit = input.limit;
    if (input.forceTrustAnalogy !== undefined) {
      body.force_trust_analogy = input.forceTrustAnalogy;
    }
    const wire = unwrap(
      // The generator marks every defaulted field REQUIRED, so the partial
      // body the route actually accepts does not fit the generated type. The
      // cast is on the shape only — sending a filled-in default instead would
      // change what is asked for, which is the one thing this hop must not do.
      await client.POST("/api/lore/search", {
        body: body as WireLoreSearchRequest,
      }),
    );
    return toLoreSearch(wire);
  },

  async getLoreEntry(entryId: string): Promise<LoreEntryDetailView> {
    // GET /api/lore/entries/{entry_id} -> LoreEntryDetailDTO. Addressing is
    // ENTIRELY in the path and there are no query parameters, deliberately:
    // `?revision=3` would otherwise have been a way to ask for one revision
    // and quietly receive the latest. A wrong path is a 404, which is loud.
    const wire = unwrap(
      await client.GET("/api/lore/entries/{entry_id}", {
        params: { path: { entry_id: entryId } },
      }),
    );
    return toLoreEntryDetail(wire);
  },

  async getLoreRevision(
    entryId: string,
    revisionId: number,
  ): Promise<LoreRevisionView> {
    // GET /api/lore/entries/{entry_id}/revisions/{revision_id} ->
    // LoreRevisionDTO. The entry id is a CONSTRAINT: revision ids are global,
    // so a revision belonging to a DIFFERENT entry 404s here rather than
    // being served — a mistyped entry id must not hand back somebody else's
    // text with nothing to signal it.
    const wire = unwrap(
      await client.GET("/api/lore/entries/{entry_id}/revisions/{revision_id}", {
        // `revision_id` is an integer everywhere in the DTOs and a STRING in
        // the path parameter — a path segment has no other form. Stringify
        // here rather than widening the domain type to match a URL detail.
        params: {
          path: { entry_id: entryId, revision_id: String(revisionId) },
        },
      }),
    );
    return toLoreRevision(wire);
  },

  // ── T-33 對象審核 ────────────────────────────────────────────────────────
  async listPendingLoreEntities(): Promise<LorePendingEntityView[]> {
    // GET /api/lore/entities/pending -> LorePendingEntityRowDTO[]。這條是唯一
    // 一條看得到「待審」的路:待審資料本來就在 DB 裡,但在這條路存在之前沒有出
    // 口,所以座艙畫得出佇列的那一天就是這條路落地的那一天。
    const wire = unwrap(await client.GET("/api/lore/entities/pending", {}));
    return (wire ?? []).map(toLorePendingEntity);
  },

  async approveLoreEntity(
    entityId: string,
    reason = "",
  ): Promise<LoreEntityGovernanceView> {
    // 回的是治理收據,不是 204:呼叫端要看得到它實際落在哪個狀態,而不是靠「沒
    // 有報錯」推論。
    const wire = unwrap(
      await client.POST("/api/lore/entities/{entity_id}/approve", {
        params: { path: { entity_id: entityId } },
        body: { reason },
      }),
    );
    return toLoreEntityGovernance(wire);
  },

  async mergeLoreEntity(
    entityId: string,
    into: string,
    reason = "",
  ): Promise<LoreEntityGovernanceView> {
    const wire = unwrap(
      await client.POST("/api/lore/entities/{entity_id}/merge", {
        params: { path: { entity_id: entityId } },
        body: { into, reason },
      }),
    );
    return toLoreEntityGovernance(wire);
  },

  subscribeConnection(
    onState: (state: SseConnectionState) => void,
  ): () => void {
    // A thin re-export of the module-level downlink health (the shared-downlink
    // block above owns it). It is on the Api seam rather than imported straight
    // from http.ts so the UI keeps ONE swap point (api/index.ts) and mock mode
    // answers the same question with its own honest constant.
    return subscribeSseConnection(onState);
  },
};
