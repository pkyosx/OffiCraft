// api/wire.ts — TS mirror of the BE wire DTOs (the frozen HTTP contract).
//
// SINGLE SOURCE OF TRUTH: these `Wire*` types are now THIN re-exports of the
// generated OpenAPI schema (`./generated/schema.ts`, produced by
// `npm run gen:api` from the committed, frozen `spec/openapi.json` — the M1
// wire-freeze SSOT, itself emitted by `bin/dump-openapi`). They are no longer
// hand-mirrored
// field-for-field against `service/dto.py` — that hand-copy was the source of
// contract drift (e.g. a missing `stopping_timed_out` / `meta` field). The
// generated schema is derived directly from the frozen spec/openapi.json
// (Pydantic v2 DTOs), so any BE DTO change flips these types and the CI drift
// gate (bin/ci.sh) goes red until `schema.ts` is regenerated + committed.
//
// The import PATH and the `Wire*` names are preserved so `mappers.ts` /
// `http.ts` / `mock.ts` need no change — they keep `import type { Wire… } from
// "./wire"`. Only the definition moved from hand-written interface to alias.
//
// Nullability / optionality note: openapi-typescript marks a field OPTIONAL
// (`?`) when the DTO gives it a default OR makes it `| null`. That is the
// honest shape of the wire (a defaulted field may be absent), and it is exactly
// the seam that catches drift at compile time.

import type { components } from "./generated/schema";

/** Mirrors `service/dto.py :: MemberDTO`. Field names match the wire exactly
 * (snake_case). `presence` is the DERIVED five-state
 * (offline/waking/online/stopping/stopped) — the SINGLE presence word on the
 * member wire (the raw `online` / `waking_since` / `stopping_timed_out`
 * projections were removed: no FE/agent consumer ever read them). */
export type WireMember = components["schemas"]["MemberDTO"];

/** Mirrors `service/dto.py :: ChatMessageDTO`. The wire uses `from` / `to`
 * (the wire serialises the `from_` field by its `from` alias). */
export type WireChatMessage = components["schemas"]["ChatMessageDTO"];

/** Mirrors `service/dto.py :: ChatReadDTO`. One per-conversation read
 * watermark. */
export type WireChatRead = components["schemas"]["ChatReadDTO"];

/** Mirrors `WebhookEndpointDTO` (M4 回呼端點): one webhook endpoint bound to a
 * member. `token` is the opaque secret, owner-facing only. */
export type WireWebhookEndpoint = components["schemas"]["WebhookEndpointDTO"];

/** Mirrors `WebhookRequestLogDTO`: one row of an endpoint's /in debug ring
 * buffer (last 5 raw requests, newest first). Owner-only wire. */
export type WireWebhookRequestLog =
  components["schemas"]["WebhookRequestLogDTO"];

/** Mirrors `service/dto.py :: ChatGalleryEntryDTO`. ONE flattened gallery row:
 * an attachment plus its message's sender identity (`from` id + roster-resolved
 * `from_name`) and send time (`GET /api/chat/attachments?with=<member_id>`). */
export type WireChatGalleryEntry = components["schemas"]["ChatGalleryEntryDTO"];

/** Mirrors `ChatAttachmentShareLinkDTO`: the permanent single-file share link
 * (`GET /api/chat/attachments/{attachment_id}/share-link`) — the blob's serve
 * path carrying its `?sig=` file-level HMAC credential. */
export type WireChatAttachmentShareLink =
  components["schemas"]["ChatAttachmentShareLinkDTO"];

// ── Owner credential + settings (B3) ─────────────────────────────────────────

/** Mirrors `AuthStatusDTO` (`GET /api/auth/status`, PUBLIC): the single
 * first-run bit the AuthGate branches on. */
export type WireAuthStatus = components["schemas"]["AuthStatusDTO"];

/** Mirrors `SettingsDTO` (`GET`/`PATCH /api/settings`): the owner-adjustable
 * settings. */
export type WireServerSettings = components["schemas"]["SettingsDTO"];

// ── Monitoring telemetry (mirrors service/dto.py :: Monitoring*DTO) ───────────
// HONESTY CONTRACT (carried from the BE): only fields with a real source are
// ever populated; everything else is null / empty and the mapper passes it
// through untouched. NEVER fabricate a number in the mapper.

/** Mirrors `service/dto.py :: MonitoringSessionDTO`. One live AI session. */
export type WireMonSession = components["schemas"]["MonitoringSessionDTO"];

/** Mirrors `service/dto.py :: MonitoringMachineDTO`. One host machine. */
export type WireMonMachine = components["schemas"]["MonitoringMachineDTO"];

/** Mirrors `service/dto.py :: MonitoringAccountDTO`. One account's usage. */
export type WireMonAccount = components["schemas"]["MonitoringAccountDTO"];

/** Mirrors `service/dto.py :: MonitoringDTO`. Three-section telemetry
 * envelope. */
export type WireMonitoring = components["schemas"]["MonitoringDTO"];

// ── Machine lifecycle: onboard / teardown (mirrors service/dto.py) ────────────

/** Mirrors the onboard response `POST /api/machines`
 * (`MachineOnboardResultDTO`). SECURITY: `token` + `boot_command` are secrets —
 * rendered for copy only, never logged. */
export type WireOnboardResult = components["schemas"]["MachineOnboardResultDTO"];

/** Mirrors `GET /api/machines/{machine_id}/boot-command`
 * (`BootCommandResultDTO`). Same secret posture as WireOnboardResult. */
export type WireBootCommand = components["schemas"]["BootCommandResultDTO"];

/** Mirrors `POST /api/machines/{machine_id}/bootstrap-here`
 * (`BootstrapResultDTO`). `ok` / `exit_code` / `log` — verbatim passthrough. */
export type WireBootstrapResult = components["schemas"]["BootstrapResultDTO"];

/** Mirrors one entry of `GET /api/machines` (`MachineDTO`): stable opaque
 * `machine_id` + renamable `display_name`; `online` is warden reachability. */
export type WireMachine = components["schemas"]["MachineDTO"];

/** Mirrors `service/dto.py :: MachineDeleteResultDTO` — the DELETE response
 * `DELETE /api/machines/{member_id}` (pure soft-delete). */
export type WireDeleteResult = components["schemas"]["MachineDeleteResultDTO"];

/** Mirrors `service/dto.py :: MachineUninstallResultDTO` — the uninstall
 * response `POST /api/machines/{member_id}/uninstall`. */
export type WireUninstallResult =
  components["schemas"]["MachineUninstallResultDTO"];

/** Mirrors `MachineUpgradeResultDTO` — the one-click upgrade response
 * `POST /api/machines/{member_id}/upgrade` (T-5f01). */
export type WireUpgradeResult =
  components["schemas"]["MachineUpgradeResultDTO"];

/** Mirrors `POST /api/machines/{machine_id}/teardown-here`
 * (`MachineTeardownHereResultDTO`) — the symmetric inverse of
 * WireBootstrapResult. */
export type WireTeardownHereResult =
  components["schemas"]["MachineTeardownHereResultDTO"];

// ── Reply cards (等我回覆卡, B1 wire) ──────────────────────────────────────────

/** Mirrors `ReplyCardDTO`: one reply card. `status` is the closed set
 * `waiting` | `answered` (the ONLY transition is waiting→answered via an
 * answer); `chat_message_id` links the chat message the card rides in (the
 * jump-to-origin anchor); `answered_ts` / `answer` are null while waiting. */
export type WireReplyCard = components["schemas"]["ReplyCardDTO"];

/** Mirrors `ReplyCardAnswerDTO`: the stored answer on an answered card.
 * `option_idx` is null for a pure free-text answer; `attachments` are served
 * refs into the shared chat-attachment store. */
export type WireReplyCardAnswer = components["schemas"]["ReplyCardAnswerDTO"];

/** Mirrors `ReplyCardCountDTO`: the waiting count behind the nav badge
 * (`GET /api/reply-cards/count`; answered cards never count). */
export type WireReplyCardCount = components["schemas"]["ReplyCardCountDTO"];

// ── Tasks (M3 任務卡 wire) ────────────────────────────────────────────────────

/** Mirrors `TaskDTO`: one task (a multi-node workflow with a DoD). `status` is
 * the eight-state closed set (done/terminated/duplicated terminal;
 * `reassigning` is the transient handover hold a reassign enters — only the
 * NEW executor reports it back to in_progress); `priority`
 * includes `frozen`; `executor_kind='outsource'` with an empty `executor_id` is
 * the transient unassigned state; `deps` are blocking task IDS (display
 * markers); `duplicate_of` is the original this task duplicates ('' unless
 * duplicated); `progress_done`/`progress_total` count step leaves SERVER-SIDE
 * (the FE never recomputes them). */
export type WireTask = components["schemas"]["TaskDTO"];

/** Mirrors `TaskListItemDTO` (`GET /api/tasks` / MCP `list_tasks`): the LIGHT
 * list projection — the collapsed 任務清單 card's fields WITHOUT the heavy
 * `steps`/`description`/`inputs`. Fetch the full `WireTask` via
 * `GET /api/tasks/{id}` (getTask) to hydrate a card on expand. */
export type WireTaskListItem = components["schemas"]["TaskListItemDTO"];

/** Mirrors `TaskReassignDTO` (`POST /api/tasks/{task_id}/reassign` / MCP
 * `reassign_task`): the new executor target + an optional handover note the
 * server appends to the new executor's notification. `target.kind='member'`
 * re-points at an ACTIVE roster member; `target.kind='outsource'` mints a
 * fresh worker on the spot from `model` / `effort` / `machine`. */
export type WireTaskReassign = components["schemas"]["TaskReassignDTO"];

/** Mirrors `TaskStepDTO`: one workflow node. Gate projection: `is_gate` with an
 * empty `reply_card_id` = ANNOUNCED (dashed 等我回覆); non-empty = ARMED (a live
 * M2 reply card is bound). */
export type WireTaskStep = components["schemas"]["TaskStepDTO"];

/** Mirrors `TaskArtifactDTO` (T-3dc5): one pinned deliverable on a task's
 * artifact set. `kind` is file|image|link; file/image carry the blob serve
 * `url` + `mime`/`filename`/`is_image` (from the shared chat_attachment store),
 * link carries a bare external `url`. Folded into the full `WireTask.artifacts`
 * (get_task); the light list carries only `artifact_count`. */
export type WireTaskArtifact = components["schemas"]["TaskArtifactDTO"];

/** Mirrors `TaskCountDTO` (`GET /api/tasks/count`): the open (non-terminal)
 * task count behind the tasks nav badge. */
export type WireTaskCount = components["schemas"]["TaskCountDTO"];

/** Mirrors `OutsourceWorkerDTO` (`GET /api/outsource-workers`): one LIVE
 * (not-yet-released) outsource worker — codename / model / effort + its ONE
 * bound task. Released workers drop off the list. */
export type WireOutsourceWorker = components["schemas"]["OutsourceWorkerDTO"];

/** Mirrors `TaskManualDTO` (`GET /api/task-manuals`): one task type / playbook.
 * The tasks page reads only type_key (+ purpose) for its type filter; the
 * manual editor (設定 › 任務手冊) reads the whole shape. */
export type WireTaskManual = components["schemas"]["TaskManualDTO"];

/** Mirrors `TaskManualFieldDTO`: one Q2 input field of a manual — name,
 * required/optional, and whether it is (part of) the 識別鍵 (is_key). */
export type WireTaskManualField = components["schemas"]["TaskManualFieldDTO"];

/** Mirrors `TaskManualDeleteResultDTO` (`DELETE /api/task-manuals/{type_key}`):
 * the delete receipt (open tasks of the type → 409 instead). */
export type WireTaskManualDeleteResult =
  components["schemas"]["TaskManualDeleteResultDTO"];

/** Mirrors `TaskManualUpdateDTO` (`POST /api/task-manuals/{type_key}`): the
 * partial manual edit body — only supplied fields change; `assignee: {}`
 * unsets. */
export type WireTaskManualUpdate = components["schemas"]["TaskManualUpdateDTO"];

/** Mirrors `TaskRefDTO`: the LIGHT task reference a reply card carries when it
 * was armed from a task gate (SPEC §3.6 請示 → 任務跳轉). */
export type WireTaskRef = components["schemas"]["TaskRefDTO"];

// ── Settings: build identity + role journal (mirrors service/dto.py) ──────────

/** Mirrors `service/dto.py :: VersionDTO`. Build identity. HONESTY: `version`
 * stays "0.0.0" until a real release; `git_sha` / `git_time` are the
 * human-facing identity. */
export type WireVersion = components["schemas"]["VersionDTO"];

/** Mirrors `service/dto.py :: GlobalContextDTO`. The folded global-context
 * doc. `is_default` = seed (true) vs owner-edited (false). */
export type WireGlobalContext = components["schemas"]["GlobalContextDTO"];

/** Mirrors `service/dto.py :: RoleDefDTO`. The folded role-definition doc. */
export type WireRoleDef = components["schemas"]["RoleDefDTO"];

/** Mirrors `service/dto.py :: RoleCreateResultDTO` — the created custom-role +
 * founding-member pair (`POST /api/roles`, M2-2). */
export type WireRoleCreateResult = components["schemas"]["RoleCreateResultDTO"];

/** Mirrors `service/dto.py :: RoleDeleteResultDTO` — the hard-delete cascade
 * receipt (`DELETE /api/roles/{role}`, M2-2). */
export type WireRoleDeleteResult = components["schemas"]["RoleDeleteResultDTO"];

/** Mirrors `service/dto.py :: BootstrapDTO`. `context` is the assembled agent
 * boot persona; `token` is the member JWT (null on a UI preview). The view
 * NEVER maps token in. */
export type WireBootstrap = components["schemas"]["BootstrapDTO"];

/** Mirrors `service/dto.py :: LessonsDTO`. The folded PER-ROLE lessons doc for
 * one `role_key` + `task_type`. `is_default` = seed vs owner-edited. */
export type WireLessons = components["schemas"]["LessonsDTO"];
