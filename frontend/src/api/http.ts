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
  Member,
  MonitoringView,
  VersionView,
  ReleaseCheckView,
  GlobalContextView,
  RoleDefView,
  BootstrapView,
  LessonsView,
  OnboardResultView,
  DeleteResultView,
  UninstallResultView,
  UpgradeResultView,
  BootstrapResultView,
  TeardownHereResultView,
  MachineView,
  MemberActivateResult,
  MemberRelocateResult,
} from "../types";
import type {
  Api,
  ChatCursor,
  ChatMessage,
  ChatReadReceipt,
  ChatAttachmentInput,
  GalleryAttachment,
  MemberPatch,
  WebhookEndpoint,
  WebhookCreateInput,
  WebhookUpdate,
  WebhookRequestLog,
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
  TaskManualView,
  TaskManualPatch,
  DocSummaryView,
  DocView,
  RolePatch,
  RoleCreateInput,
  RoleCreateResult,
  AliasPatch,
  OnboardOptions,
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
  toGlobalContext,
  toRoleDef,
  toBootstrap,
  toLessons,
  toOnboardResult,
  toDeleteResult,
  toUninstallResult,
  toUpgradeResult,
  toBootstrapResult,
  toTeardownHereResult,
  toMachine,
  toServerSettings,
  toTask,
  toTaskListItem,
  toOutsourceWorker,
  toTaskType,
  toTaskManual,
  toWebhookEndpoint,
  toWebhookRequestLog,
  fromTaskManualPatch,
  fromTaskReassignInput,
} from "./mappers";
import { ownerToken, setToken } from "./auth";
import { ApiError } from "./errors";
import { client } from "./client";

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
async function credentialPost(
  path: string,
  body: unknown,
  token?: string,
): Promise<{ token: string }> {
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
    );
  }
  return (await res.json()) as { token: string };
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
// Reconnect: unchanged — the browser's native EventSource auto-retry still
// applies to the (single) connection; we never tear it down on transient
// errors, only on last-unsubscribe.
const sseSubscribers = new Set<(topic: string) => void>();
let sseSource: EventSource | null = null;
// The document/window foreground listener that drives the foreground-restore
// resync (installed with the connection, torn down with it). Held module-level
// so the last-unsubscribe teardown can remove exactly the one it added.
let sseVisibilityHandler: (() => void) | null = null;

// The CLOSED SSE topic vocabulary (spec/sse.md §3.1 / §4.1). Replayed one
// synthetic delta per topic to every subscriber on a full resync so each hook
// refetches its snapshot — see resyncAll below. New topics MUST be added here.
const SSE_RESYNC_TOPICS = [
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
  "context",
  "monitoring",
] as const;

// Fan one synthetic delta per closed topic to EVERY current subscriber — the
// missed-gap correction. The stream has NO replay (spec §2.1): a delta emitted
// while the client wasn't receiving is gone, so the client must full-resync
// whenever it may have missed deltas. Shared by BOTH triggers — a genuine
// EventSource reconnect (es.onopen) AND a return to the foreground (a mobile
// tab often PAUSES the connection in the background without a reconnect, so
// onopen never re-fires) — so every delta-backed view (unread badge, roster,
// tasks, reply cards…) re-pulls its truth in ONE place. Snapshot the set: a
// callback may (un)subscribe during the fan-out. Each subscriber's refetch has
// its own .catch (verified per-hook), so a fan into an unstable network fails
// as "keep the stale value + warn", never an unhandled rejection.
function resyncAll(): void {
  for (const topic of SSE_RESYNC_TOPICS) {
    for (const cb of [...sseSubscribers]) cb(topic);
  }
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
  let opened = false;
  es.onopen = () => {
    // FIRST open needs no resync — every hook refetched on mount. Only a
    // SUBSEQUENT open (a genuine reconnect after the browser dropped and
    // re-established the stream) replays the missed gap.
    if (opened) resyncAll();
    opened = true;
  };
  es.onmessage = (e: MessageEvent) => {
    try {
      const evt = JSON.parse(e.data) as { topic?: string };
      if (!evt.topic) return;
      // Snapshot the set: a callback may (un)subscribe during fan-out.
      for (const cb of [...sseSubscribers]) cb(evt.topic);
    } catch {
      // Non-JSON keepalive/comment frame — ignore.
    }
  };
  sseSource = es;

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
  if (typeof document !== "undefined") {
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
 * and PUT (重新決定) share the exact same body shape. An absent option/text is
 * sent as its honest wire default (null / ""); attachments are omitted when
 * empty — same convention as postChat. */
function toAnswerBody(answer: ReplyCardAnswerInput) {
  const attachments = answer.attachments ?? [];
  return {
    option_idx: answer.optionIdx ?? null,
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
    return { relocationPending: wire.relocation_pending === true };
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

  async forceStopMember(id: string): Promise<void> {
    // POST /api/members/{id}/force-stop -> MemberDTO. Escalates a *stopping* member
    // to an IMMEDIATE kill: the server dispatches the robust STOP straight to the
    // warden, bypassing the 120s graceful-stop grace (the warden SIGKILLs). Takes
    // no body. Caller refetches; presence surfaces stopped.
    await client.POST("/api/members/{member_id}/force-stop", {
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
    const body: { name?: string; model?: string; effort?: string } = {};
    if (patch.name !== undefined) body.name = patch.name;
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
    // history page and NEVER advances the read watermark. Undefined params
    // are dropped from the query by the client's serializer, so the
    // cursorless wire shape is unchanged.
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
    return wire.map(toChatMessage);
  },

  async peekChat(withId: string, limit = 30): Promise<ChatMessage[]> {
    // READ-ONLY conversation view (no "list 即讀" watermark side effect): the
    // server ?peek=true (T-cf91) filters by ?with= and caps by limit EXACTLY
    // like the marking path, but does not advance the read watermark. Replaces
    // the old workaround of pulling the WHOLE company stream (limit=-1) and
    // filtering client-side just to dodge the ?with= auto-mark — that payload
    // was the entire chat history and grew without bound. Default 30 mirrors
    // the server default; the server applies the filter BEFORE the cap, so the
    // thread is never starved.
    const wire = unwrap(
      await client.GET("/api/chat", {
        params: { query: { with: withId, limit, peek: "true" } },
      }),
    );
    return wire.map(toChatMessage);
  },

  async listChatAttachments(withId: string): Promise<GalleryAttachment[]> {
    // GET /api/chat/attachments?with=<memberId> -> ChatGalleryEntryDTO[]. The
    // M2 gallery query: the member's WHOLE attachment perspective (owner↔member
    // both directions + inter-agent threads), flattened newest→oldest with the
    // sender id + server-resolved display name per row. READ-ONLY — no
    // read-watermark side effect (unlike listChat's ?with= auto-mark).
    const wire = unwrap(
      await client.GET("/api/chat/attachments", {
        params: { query: { with: withId } },
      }),
    );
    return wire.map(toGalleryAttachment);
  },

  async getChatAttachmentShareLink(attachmentId: string): Promise<string> {
    // GET /api/chat/attachments/{attachment_id}/share-link -> {url}: the
    // blob's serve path + its permanent ?sig= HMAC credential (grants reading
    // exactly that one blob; no expiry). The caller absolutizes with the page
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
    // GET /api/reply-cards?status= -> ReplyCardListItemDTO[] (T-3f31: the list
    // wire went LIGHT — summary + decision digest only, no body/options; the
    // agent-facing list_reply_cards tool shares this route and must stay
    // small). The cockpit panes render the FULL card (option chips, body,
    // attachment refs), so hydrate each row via GET /api/reply-cards/{id},
    // preserving the server's pane order (waiting = longest-waiting first,
    // answered = last-24h newest answer first). RepliesPage re-sorts the
    // waiting pane newest-first for DISPLAY only (T-b07f) — the adapter still
    // hands over server order.
    const rows = unwrap(
      await client.GET("/api/reply-cards", {
        params: { query: { status } },
      }),
    );
    return Promise.all(
      rows.map(async (row) =>
        toReplyCard(
          unwrap(
            await client.GET("/api/reply-cards/{card_id}", {
              params: { path: { card_id: row.id } },
            }),
          ),
        ),
      ),
    );
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
    // owner-only terminal exit that is NOT an answer; no body). answered /
    // already-expired → 409, unknown id → 404 (ApiError via middleware).
    const wire = unwrap(
      await client.POST("/api/reply-cards/{card_id}/expire", {
        params: { path: { card_id: id } },
      }),
    );
    return toReplyCard(wire);
  },

  async listTasks(opts?: { open?: boolean }): Promise<TaskView[]> {
    // GET /api/tasks -> TaskListItemDTO[] (LIGHT: no steps/description/inputs).
    // ?open=true (T-2b9d) drops the terminal rows server-side for the default
    // 未結束-only view; omitted → the full population (清除篩選 全部, the
    // outsource-panel join). A card hydrates its heavy detail on expand via
    // getTask.
    const wire = unwrap(
      await client.GET("/api/tasks", {
        params: {
          query: opts?.open ? { open: "true" } : {},
        },
      }),
    );
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

  async getTaskCount(): Promise<number> {
    // GET /api/tasks/count -> {open}. The tasks nav badge's cheap count path
    // (refetched on every "task" SSE delta without pulling the list).
    const wire = unwrap(await client.GET("/api/tasks/count"));
    return wire.open;
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

  async setTaskPriority(id: string, priority: string): Promise<TaskView> {
    // POST /api/tasks/{task_id}/priority {priority} -> TaskDTO. high|mid|low|
    // frozen — freeze/unfreeze ride the same knob (spec §3.3); a closed task
    // is a 409, an out-of-vocabulary value a 422 (both throw).
    const wire = unwrap(
      await client.POST("/api/tasks/{task_id}/priority", {
        params: { path: { task_id: id } },
        body: { priority },
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
      })
    );
    return toTask(wire);
  },

  async removeTaskArtifact(taskId: string, artifactId: string): Promise<TaskView> {
    // DELETE /api/tasks/{task_id}/artifact/{artifact_id} -> TaskDTO. The owner/
    // admin un-pin (T-3dc5); unknown task/artifact → 404, wrong-task → 400 (both
    // throw via the client middleware). Returns the task with artifacts folded
    // fresh; the blob itself is left intact.
    const wire = unwrap(
      await client.DELETE("/api/tasks/{task_id}/artifact/{artifact_id}", {
        params: { path: { task_id: taskId, artifact_id: artifactId } },
      }),
    );
    return toTask(wire);
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
    // POST /api/outsource-workers/{id}/refocus -> OutsourceWorkerDTO (owner-only,
    // online-only 409). Graceful (T-ea82): stamps the handover + nudges the worker
    // to flush (~120s grace), then the server kills+re-spawns a fresh worker on the
    // same task; the outsource_worker SSE delta also fans so the list refetches.
    const wire = unwrap(
      await client.POST("/api/outsource-workers/{id}/refocus", {
        params: { path: { id } },
      }),
    );
    return toOutsourceWorker(wire);
  },

  async stopWorker(id: string): Promise<OutsourceWorkerView> {
    // POST /api/outsource-workers/{id}/stop -> OutsourceWorkerDTO (owner-only).
    // Kills the session and holds the worker down (presence "stopping"/"stopped").
    const wire = unwrap(
      await client.POST("/api/outsource-workers/{id}/stop", {
        params: { path: { id } },
      }),
    );
    return toOutsourceWorker(wire);
  },

  async restartWorker(id: string): Promise<OutsourceWorkerView> {
    // POST /api/outsource-workers/{id}/restart -> OutsourceWorkerDTO (owner-only,
    // 409 if not stopped). Clears the stop and re-dispatches.
    const wire = unwrap(
      await client.POST("/api/outsource-workers/{id}/restart", {
        params: { path: { id } },
      }),
    );
    return toOutsourceWorker(wire);
  },

  async setWorkerModel(
    id: string,
    patch: { model: string; effort?: string },
  ): Promise<OutsourceWorkerView> {
    // POST /api/outsource-workers/{id}/model {model, effort?} -> OutsourceWorkerDTO
    // (owner-only). Active+online → kill+respawn now; otherwise persist for the
    // next spawn. model is always sent (blank ⇒ launcher default); effort only
    // when supplied (an absent field must not arrive as null).
    const body: { model: string; effort?: string } = { model: patch.model };
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
    // (owner-only). The server re-runs the spawn fold over the CURRENT rows;
    // no token rides the response (a UI preview mints none).
    const wire = unwrap(
      await client.GET("/api/outsource-workers/{id}/boot-context", {
        params: { path: { id } },
      }),
    );
    return wire.context;
  },

  async listTaskTypes(): Promise<TaskTypeView[]> {
    // GET /api/task-manuals?view=list -> TaskManualDTO[] (T-ec2c light
    // projection: type_key / display_name / purpose meaningful, the heavy
    // sop_md/learnings/fields/assignee honest-empty), narrowed to the
    // {typeKey, displayName, purpose} the type filter reads. The full-body
    // read stays the per-type getTaskManual on the settings detail page.
    const wire = unwrap(
      await client.GET("/api/task-manuals", {
        params: { query: { view: "list" } },
      }),
    );
    return wire.map(toTaskType);
  },

  async listTaskManuals(): Promise<TaskManualView[]> {
    // GET /api/task-manuals -> TaskManualDTO[] — the SAME wire read as
    // listTaskTypes, mapped FULL for the 設定 › 任務手冊 list page.
    const wire = unwrap(await client.GET("/api/task-manuals"));
    return wire.map(toTaskManual);
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

  async upgradeMachine(memberId: string): Promise<UpgradeResultView> {
    // POST /api/machines/{member_id}/upgrade -> MachineUpgradeResultDTO
    // {member_id, machine_id, dispatched}. Enqueues the `update` warden-command
    // (the warden kicks its own self-update reconcile). Fire-and-forget: no
    // durable write — `dispatched` is TRUE when the warden was online, FALSE
    // when offline. A non-2xx (transport/gate) throws via the client. The
    // caller refetches later to observe binStatus converging to "current".
    const wire = unwrap(
      await client.POST("/api/machines/{member_id}/upgrade", {
        params: { path: { member_id: memberId } },
      }),
    );
    return toUpgradeResult(wire);
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
    // server host in one click (owner-only). A non-2xx (transport/gate) throws
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
    // the server host in one click (owner-only). A non-2xx (transport/gate) throws
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

  async getAuthStatus(): Promise<boolean> {
    // GET /api/auth/status (PUBLIC) -> AuthStatusDTO. Rides the typed client
    // (a public route never 401s, so the auth-expired middleware is inert).
    const wire = unwrap(await client.GET("/api/auth/status"));
    return wire.password_set;
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
    // PATCH /api/settings {token_ttl?, handover_pct?, outsource_max_parallel?}
    // -> SettingsDTO (the settings after the change — durable + live
    // immediately; an outsource_max_parallel outside -1..20 is a 422; -1 = 無限).
    const body: {
      token_ttl?: number;
      handover_pct?: number;
      outsource_max_parallel?: number;
      updater_receive_beta?: boolean;
      updater_auto_update?: boolean;
      org_name?: string;
      owner_name?: string;
      display_theme?: string;
      display_language?: string;
      custom_themes?: {
        id: string;
        name: string;
        colors: Record<string, string>;
        wording?: Record<string, Record<string, string>>;
        fonts?: Record<string, string>;
        avatars?: { member?: string; outsource?: string };
      }[];
    } = {};
    if (patch.tokenTtl !== undefined) body.token_ttl = patch.tokenTtl;
    if (patch.handoverPct !== undefined) body.handover_pct = patch.handoverPct;
    if (patch.outsourceMaxParallel !== undefined) {
      body.outsource_max_parallel = patch.outsourceMaxParallel;
    }
    if (patch.updaterReceiveBeta !== undefined) {
      body.updater_receive_beta = patch.updaterReceiveBeta;
    }
    if (patch.updaterAutoUpdate !== undefined) {
      body.updater_auto_update = patch.updaterAutoUpdate;
    }
    if (patch.orgName !== undefined) body.org_name = patch.orgName;
    if (patch.ownerName !== undefined) body.owner_name = patch.ownerName;
    if (patch.displayTheme !== undefined) body.display_theme = patch.displayTheme;
    if (patch.displayLanguage !== undefined) {
      body.display_language = patch.displayLanguage;
    }
    if (patch.customThemes !== undefined) body.custom_themes = patch.customThemes;
    const wire = unwrap(await client.PATCH("/api/settings", { body }));
    return toServerSettings(wire);
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
    // never written). The read-only system-interaction / boot-sequence blocks
    // have NO endpoint by construction.
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

  async listRoles(): Promise<RoleDefView[]> {
    // GET /api/roles -> RoleDefDTO[]
    const wire = unwrap(await client.GET("/api/roles"));
    return wire.map(toRoleDef);
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
      model?: string;
      effort?: string;
    } = { name: input.name };
    if (input.memberName !== undefined) body.member_name = input.memberName;
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

  async getLessons(roleKey: string, taskType: string): Promise<LessonsView> {
    // GET /api/lessons/{role_key}/{task_type} -> LessonsDTO (folded overlay ⊕
    // seed). PER-ROLE doc (per-role-learnings step1): scoped to role_key; the
    // single fixed task_type key is "general".
    const wire = unwrap(
      await client.GET("/api/lessons/{role_key}/{task_type}", {
        params: { path: { role_key: roleKey, task_type: taskType } },
      }),
    );
    return toLessons(wire);
  },

  async saveLessons(
    roleKey: string,
    taskType: string,
    text: string,
  ): Promise<LessonsView> {
    // POST /api/lessons/{role_key}/{task_type} {text} -> LessonsDTO (folded,
    // isDefault=false). Whole-doc replace matching the backend
    // `handle_replace_lessons`. NOTE the POST verb — do NOT copy the
    // global-context save's PUT/DELETE, which mismatch this contract. PER-ROLE
    // doc; "general" is the single fixed task_type key. WRITE authz is per-role:
    // owner (this UI's scope) may write any role.
    const wire = unwrap(
      await client.POST("/api/lessons/{role_key}/{task_type}", {
        params: { path: { role_key: roleKey, task_type: taskType } },
        // allow_shrink: the server's T-2d99 wipe guard refuses a non-empty →
        // empty whole-doc replace unless the caller says so explicitly. That
        // guard exists for BLIND agent write-backs; here a human is looking at
        // the editor they just cleared, so the intent is already explicit.
        body: { text, allow_shrink: true },
      }),
    );
    return toLessons(wire);
  },

  subscribeEvents(onTopic: (topic: string) => void): () => void {
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
    const sub = (topic: string) => onTopic(topic);
    sseSubscribers.add(sub);
    ensureSseSource();
    return () => {
      sseSubscribers.delete(sub);
      if (sseSubscribers.size === 0 && sseSource) {
        sseSource.close();
        sseSource = null;
        // Tear down the foreground-restore listeners with the connection so a
        // visibilitychange/focus never fans a resync onto an empty subscriber
        // set (and nothing leaks across a close→reopen cycle).
        if (sseVisibilityHandler && typeof document !== "undefined") {
          document.removeEventListener("visibilitychange", sseVisibilityHandler);
          if (typeof window !== "undefined") {
            window.removeEventListener("focus", sseVisibilityHandler);
          }
        }
        sseVisibilityHandler = null;
      }
    };
  },
};
