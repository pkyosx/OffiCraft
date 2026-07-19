# spec/sse.md — the `/api/events` SSE contract (M1 wire freeze)

> Status: **frozen** (M1 spec freeze). This document is the behavioural contract for the
> server-sent-events surface that `spec/openapi.json` cannot express: frame shapes, topic
> vocabulary, connection-lifecycle side effects, and the presence projection. A replacement
> implementation (the Go rewrite) MUST satisfy every MUST/MUST NOT assertion here; anything
> explicitly marked *implementation detail* is free.
>
> Source of truth at freeze time: commit `6dd7280`. The assertions are normative and
> self-contained.

## 1. Endpoint

- `GET /api/events` — **gated** (bearer JWT; see spec/lifecycle.md §1). An unauthenticated
  request MUST be refused `401` before any stream bytes are sent.
- The response MUST be `Content-Type: text/event-stream` and MUST carry
  `Cache-Control: no-cache` and `X-Accel-Buffering: no` headers.
- The stream MUST begin with the comment line `: connected\n\n`.
- **Heartbeat**: whenever the stream has been quiet for **15 seconds**, the server MUST emit
  the comment `: heartbeat\n\n`. The 0.25 s internal poll cadence is an implementation detail; the 15 s heartbeat period is contract (client
  watchdogs and proxies are tuned to it).
- One connection serves exactly one owner scope (the token's owner); a listener MUST receive
  only frames addressed to its owner (or a system broadcast, §4).

## 2. Delta frame shape

Every entity delta MUST be emitted as one SSE event of exactly this shape:

```
id: <seq>
data: {"seq":42,"topic":"member","op":"patch","data":{"entity":"member","key":"owner::m-1a2b3c","epoch":42,"deleted":false,"payload":{"id":"m-1a2b3c","name":"kyle","status":"active","desired_state":"online","owner_id":"owner"}},"ts":1752192000.123,"trigger":"owner"}
```

- The `id:` field MUST equal the frame's `seq`.
- The `data:` JSON object MUST carry exactly `{seq, topic, op, data, ts, trigger}` where
  `data = {entity, key, epoch, deleted, payload}` (`trigger` joined the envelope in the
  T-f39c listen-readability batch, owner-approved 方案 A — an ADDITIVE envelope field; a
  client MUST tolerate a frame without it, reading an absent/blank trigger as unknown and
  never suppressing on it).
- `ts` is a float unix-epoch timestamp. Compact JSON separators (`,`/`:`) are an
  implementation detail; field presence and values are contract.
- `trigger` names WHO caused the durable write the delta reports — see §2.3.
- `epoch` MUST equal `seq` (a single process-local counter serves both).
- When `deleted` is `true`, `payload` MUST be `null` and `op` MUST be
  `"remove"`.

### 2.1 `seq` semantics — restart rollback is allowed

- `seq` MUST be strictly monotonically increasing **within one server process**
  (pre-increment per publish, starting at 1). The counter is a single process-local
  sequence shared across all connections; because fan-out is per-recipient (§4), a filtered
  connection observes a monotonic **subsequence** of it — the `seq` values it sees increase
  but are not contiguous (gaps mark frames addressed to other members). Gaps are expected and
  MUST NOT be treated as loss: there is no replay, and a client full-resyncs on reconnect.
- `seq`/`epoch`/`id` carry **no cross-restart guarantee**: the counter is process-local and
  resets to 0 on restart (in-memory state inventory #8). A client MUST NOT treat a lower
  `seq` after reconnect as an error, MUST NOT use `Last-Event-ID` for replay (the server
  implements no replay), and MUST perform a full resync (refetch) on every reconnect.
- A future implementation MUST NOT "helpfully" persist the counter — clients are contracted
  to tolerate rollback, and persistence would be an observable behaviour change.

### 2.2 Reconcile-by-refetch (the client contract)

- A delta means "this topic changed → refetch its list/snapshot".
- `payload` is carried **for convenience only**; a client MUST NOT merge it in place — it
  lacks server-derived DTO fields. Payloads are deliberately partial:
  - `member`: `{id, name, status, desired_state, owner_id}`
  - `chat` (new message): `{id, from, to}`
  - `chat_read`: `{reader, peer, last_read_ts}`
  - `reply_card`: `{id, from, status}` (create / answer / answer revision / expire all
    ride `patch`; refetch the card for the full context — the payload never carries the
    answer)
  - `task`: `{id, status, priority}` (any durable task write — status/priority/plan/
    steps/deps/executor; one topic per task entity, the member granularity)
  - `outsource_worker`: `{id, codename, status}` (assignment / claim / release)
  - `task_manual`: `payload` is `null` (manual create/edit/delete, learnings write-back)
  - `global_context` / `role_def` / `lessons`: `payload` is `null`
  - `context` / `monitoring` signals: `payload` is `null`
- `key` MUST be treated as an **opaque change hint**. Current key formats (clients MUST NOT
  parse them): `{owner}::{id}` for member/chat/reply_card/task/outsource_worker/role_def,
  `{owner}::{type_key}` for task_manual,
  `{owner}::{reader}::{peer}` for chat_read,
  `{owner}::{role_key}::{task_type}` for lessons, the bare owner id for global_context, the bare agent id for context/monitoring signals.

### 2.3 `trigger` — actor attribution (and the client-side echo rule)

- `trigger` is the verified identity of the principal whose action caused the durable
  write this delta reports. Closed vocabulary of forms:
  - `"owner"` — an owner-scope request (the cockpit / owner API);
  - `"server"` — a server-internal producer with no acting request (reconcile,
    the outsource scheduler, an inbound webhook fold);
  - otherwise an **agent-scope token `sub`** — a member id (`m-…`), an outsource worker
    id (`ow-…`), or a warden member id.
- The value is taken from the verified token `sub` / scope of the request that performed
  the write (the caller-identity convention — never a client-supplied field). The SSE
  connect/disconnect edge writes (§5.2 waking-clear / cost-bank) attribute to the member
  whose connection edge fired them.
- `trigger` is **attribution metadata only**: it MUST NOT change fan-out. §4 audiences
  are computed exactly as before, and the owner/dashboard connection remains 全量 —
  the cockpit sees every frame regardless of trigger.
- **Echo suppression is a CLIENT-side rule** (the `ocagent listen` contract, owner-approved
  方案 A): an agent listener SHOULD silently drop a delta whose `trigger` equals its own
  member id — an agent connection only ever receives frames addressed to itself (§4), so
  `trigger == self` is by construction its own action echoed back (e.g. its own
  `update_step_status` fanning its own task delta), pure token burn. Frames triggered by
  the owner, the server, or ANY other member MUST still be processed; an absent/blank
  trigger MUST be processed (fail-open — old producer, unknown actor). **Exemption: the
  `member` topic is NOT suppressed** — a member delta naming self is a lifecycle NUDGE
  (wind-down / recycle hooks), not printed content, and the self-requested recycle
  (`restart_self`, T-4c71) deliberately rides a SELF-triggered member delta whose SOP
  wake must still land; suppressing it would break graceful handover for zero token gain.

## 3. Topic and op vocabulary

### 3.1 Topics — the closed set (12 topics)

The server MUST emit deltas on exactly these topics and no others (`reply_card`
joined the set in the M2 reply-card batch; `task` / `outsource_worker` /
`task_manual` joined in the M3 task batch — the owner-tasked M3 scope [SPEC.md
M3 任務系統] covers the task surface wholesale, these are its necessary
delta topics; everything else is the M1 freeze):

| topic | trigger | op |
|---|---|---|
| `member` | any roster write (upsert / hard delete) | patch / remove |
| `chat` | message append; cascade delete | patch |
| `chat_read` | read-watermark advance; cascade delete | patch |
| `reply_card` | reply-card create / answer / answer revision / expire | patch |
| `task` | any durable task write (create / status / priority / plan / step / deps / executor assignment / terminate) | patch |
| `outsource_worker` | worker assignment / first claim (active) / release | patch |
| `task_manual` | manual create / edit / delete / learnings write-back | patch |
| `global_context` | user-context overlay write/reset | patch |
| `role_def` | role overlay write/reset/delete | patch |
| `lessons` | lessons overlay write/cascade delete | patch |
| `context` | agent context-gauge ingest (`POST /api/agent/context`) | signal |
| `monitoring` | warden telemetry ingest (`POST /api/monitoring/telemetry`) | signal |

⚠️ **Known code-internal inconsistency at freeze, resolved in favour of the wire**: the
frozen implementation's internal topic lists were incomplete (its declared topic constant
and docs listed fewer topics, and the publish seam never validated against them) —
the actual wire emitted all of the above except `reply_card` (added M2). This spec froze
the **observed wire** (8 topics at M1; 9 with the approved M2 addition; 12 with the M3
task batch). The
directed band topics `context-high` and `warden-command` (§6, §7) are a separate envelope
family, not entity-delta topics.

- A delta build MUST never raise into the durable write it follows:
  fan-out failure must not fail the HTTP write that triggered it.

### 3.2 Ops — closed vocabulary

`op ∈ {patch, remove, signal}`:

- `patch` — the topic's data changed; refetch. Note: an overlay **reset** (tombstone back to
  seed) rides as `patch`, not `remove` — the doc still exists, it fell back to the seed.
- `remove` — the entity was deleted (`deleted: true`, `payload: null`).
- `signal` — a volatile in-memory store changed (`context`, `monitoring`); no durable entity
  behind it, `payload` always `null`.

## 4. Per-recipient fan-out

Every delta is ADDRESSED. The publish seam MUST deliver a frame to a listener iff one of:

- the listener is an **owner/dashboard connection** (projects no member online, §5) — it
  receives **every** frame (the全局 cockpit view; this is the single-owner degenerate of the
  historical `frame.owner_id is None || == listener.owner_id` broadcast rule); OR
- the listener is an **agent connection** whose member id is in the frame's **audience** (the
  set of member ids the delta concerns).

A frame with an empty agent audience still fans to the owner connection(s); an agent's stream
receives **only** the frames addressed to it. Fan-out is buffer-backed per listener; delivery
order per connection MUST be publish order.

### 4.1 Audience per topic

The audience of each delta topic is fixed by the entity it concerns — computed from the
written entity's own fields, never a subscription and never a dependency walk (coordination
between agents is done by pulling — `list_tasks` / `get_task` / `get_chat` — and by the
server's own deps-fulfill, not by eavesdropping on another member's stream):

| topic | agent audience (owner always receives) |
|---|---|
| `member` | the subject member (its own delta drives the wind-down / recycle hooks) |
| `chat` | the sender and the recipient |
| `chat_read` | — (no agent consumes it; owner cockpit only) |
| `reply_card` | the initiator (`from`) |
| `task` | the executor and the creator (NOT dependents); a reassign additionally fans one delta to the OLD executor — the row's executor just changed, so the person unassigned would otherwise be silently dropped from the audience |
| `outsource_worker` | — (owner cockpit only; an `ow-` id has no roster/presence) |
| `task_manual` | — (owner cockpit only) |
| `global_context` / `role_def` / `lessons` | — (owner cockpit only) |
| `context` / `monitoring` | — (owner cockpit only; `context` also drives the server-side §6 band) |

A blank id in an audience (an unassigned executor, an absent creator on a pre-column row, an
empty peer) is dropped — it narrows the set, never widens it. An implementation MAY carry a
system-broadcast audience (reaches every agent) for a future producer; no current producer
emits one.

## 5. Presence = pure connection projection

Presence is **derived from live SSE connections only** — never stored, never reported
(docs/design/state-model.md 原則 1; the `member.online` and `current_machine_id` DB columns
were dropped).

- **Who projects online**: a connection authenticated with an **agent-scope** token projects
  its token `sub` online for the connection's lifetime. This applies uniformly to ordinary agents AND wardens (a warden
  connects with its own agent-scoped token — no infra exemption). An owner/dashboard
  connection projects **no** member online (a viewer is not an online member).
- `is_online(owner, member_id)` MUST be true iff ≥1 live listener carries that
  `(owner_id, member_id)`. First connect → true; last disconnect →
  false. This is the **single** source of online truth (reconcile, monitoring, and the FE
  roster all read it).
- **Observed position** (`machine_of`): the machine an agent runs on is projected live from
  its connection's token `machine_id` placement claim. No live
  connection, or a connection without the claim (warden tokens carry none) → unknown
  (`null`). Rebuilt on reconnect; MUST NOT be persisted.
- `agents_on_machine(owner, machine_id)` MUST return exactly the member ids of live agent
  listeners whose `machine_id` claim matches — the teardown guard's input. Warden and dashboard connections MUST be excluded.

### 5.1 Dual-SSE takeover (single listener per member; owner rc-39de4270b280)

- A member is single-session: at most one live listener per `(owner, member_id)` at any
  instant. Opening a second live listener for the same member MUST **take over**: the new
  connection is admitted and the old listener is replaced **atomically** (removed from the
  registry and instructed to terminate in the same critical section the new listener is
  inserted in). At no instant does the member hold zero or two registered listeners, so the
  §5 online projection never flickers across the handover and every published delta is
  delivered to exactly one of the two connections. The new connection MUST NOT wait for the
  displaced handler to finish.
- The displaced connection's stream MUST be terminated promptly by the server (no further
  frames are delivered to it; its close MUST NOT fire the §5.2 last-disconnect hooks while
  the new listener keeps the member online).
- **Anti-flap throttle**: to stop two live clients from flapping one member's slot, an
  implementation MUST clamp takeovers per member — at most a bounded burst within a sliding
  window (this implementation: 3 takeovers per 60 s). An over-budget connect attempt MUST be
  refused with HTTP **409** (`{"error":{"code":"conflict","message":"member 'm-…' already
  holds a live SSE connection (takeover throttled: …)"}}` — envelope per
  docs/design/api-error-envelope.md). The window MUST be shorter than the ocagent listener's
  refusal self-terminate grace (120 s), so a single legitimate client sliding out of the
  window always gets a successful takeover before it could self-terminate on an unbroken
  409 run.
- The 409 MUST surface as a proper HTTP status **before** stream headers are sent (the
  listener is opened synchronously in the endpoint).
- Owner/dashboard connections (no projected member) are exempt — any number may be open;
  they never take over one another and never enter the throttle accounting.
- Message wording and the burst/window sizes are not contract (beyond window < client
  grace); the takeover semantics, the throttle's 409 status and its pre-stream timing are.

### 5.2 Connect/disconnect edge hooks — exactly once per edge

Two side effects are bound to the presence edges. These are
**behavioural contract**, the most easily lost part of a rewrite:

- **First-connect (offline→online)**: the instant a member's FIRST listener opens, the
  server MUST clear that member's `waking_since` (set to 0.0) iff it is > 0. The wake is complete when the agent holds `/api/events` — no liveness
  report is involved. The roster write fans one `member` delta. MUST fire exactly once per
  online span (guarded on the first-listener fact); a member with no
  roster row or not waking is a no-op. Best-effort: a failure MUST NOT break the connection
  setup.
- **Last-disconnect (online→offline)**: the instant a member's LAST listener drops, the
  server MUST fold the member's live telemetry `cost` (from the in-memory telemetry store,
  see spec/lifecycle.md §3 and inventory #5) into the durable `member.banked_cost`, then
  **pop** the live `cost` entry so a re-fired edge cannot double-bank.
  Edge-safe: the hook fires only when the close actually removed a live listener AND no
  other listener keeps the member online; an idempotent/repeated close
  MUST NOT re-fire it. No telemetry entry → skip. Best-effort: never raises into teardown.
- **boot_ts stamp**: opening an agent connection MUST stamp `boot_ts = now` into that
  agent's context-gauge entry, merging (never clobbering) other gauge fields. This anchors the context-high stale-pct guard (§6).

## 6. Context-high band (directed signal, agent connections only)

A directed reminder pushed down an **agent's own** connection when its reported context
percentage fills.
Owner/dashboard connections MUST never receive it.

- Frame shape — a bare `data:` event, **no `id:` line** (not part of the replayable delta
  stream):

```
data: {"topic":"context-high","data":{"topic":"context-high","to":"m-1a2b3c","level":"warn","pct":42,"reason":"context 42% — start converging; ..."}}
```

  The inner payload duplicates `topic` and carries `{topic, to, level, pct, reason}`.
  `reason` wording is not contract;
  the envelope shape, `to` (the target agent id) and `level` are.
- **Only `level:"warn"` is ever emitted on the wire.** The HANDOVER band (≥ handover_pct)
  MUST NOT emit an SSE signal — it is owned by the server-side producer auto-recycle
  (spec/lifecycle.md §4.5).
- Decision inputs and guards (all MUST hold for an emit):
  - the gauge pct is **actionable**: `context_pct_ts` present and strictly >
    the connection's `boot_ts` (stale-pct guard) — a predecessor
    session's leftover pct MUST NOT trigger;
  - band thresholds: defaults warn=40, handover=50, configurable via the DB
    settings (`ctx.warn_pct` / `ctx.handover_pct`); a threshold ≤ 0 disables that
    band (kill-switch);
  - the reminder is **deduped per remind-bucket** — the gauge is sliced into
    `remind_step_pct`-wide buckets (default 5%, `ctx.remind_step_pct`): a WARN
    fires when the gauge first enters the band, and again ONLY when it climbs
    into a HIGHER bucket. Sitting in the same bucket (e.g. drifting 40→41→42)
    MUST stay silent — it must not re-remind every tick. A drop to a lower
    bucket re-arms it (so a later re-climb reminds again); dropping below WARN
    fully resets.
- Band state (`last_bucket`, the highest remind-bucket already reminded in the
  current in-band run) is **per-connection, in-memory** (inventory #4); it MUST
  reset on reconnect and MUST NOT be persisted.
- Fail-safe: a missing/non-numeric/stale pct or any internal error MUST emit nothing and
  MUST NOT disturb the delta stream.

## 7. Warden-command band (directed commands, warden connections only)

The NAT transport: the server cannot dial into a warden, so server→warden commands ride the
warden's outbound SSE.

- **Eligibility**: a connection drains command frames iff it authenticated with an
  agent-scope token whose `sub` resolves to a member of `kind == "warden"`. The addressing key is that warden **member id** (the verified token
  `sub`) — never a client-supplied host. Any other connection MUST never touch the queue.
- Frame shape — bare `data:` event, no `id:` line:

```
data: {"topic":"warden-command","data":{"rpc":"start","args":{"member_id":"m-1a2b3c","persona_context":"…","member_token":"<jwt>","role":"assistant","task_type":"default","model":"","effort":"","session_name":""}}}
```

- `rpc` vocabulary and `args` shapes:
  - `start`: `{member_id, persona_context, member_token, role, task_type, model, effort, session_name}`
    (blank `effort`/`model`/`session_name` mean warden defaults; `session_name` is always
    `""` today — the warden derives `member-<id>`).
  - `stop`: `{member_id}` — the single ROBUST stop; the warden self-escalates the kill.
  - `uninstall`: `{member_id}` — the warden removes itself from its box.
  - `update` (T-5f01 — the owner's one-click machine upgrade, pushed by
    `POST /api/machines/{member_id}/upgrade`): `{member_id}` — the warden kicks its OWN
    self-update reconcile NOW (the T-c93d event-driven seam: download + verify-before-swap
    + atomic swap + exec-in-place) instead of waiting out its poll backstop. Fire-and-forget
    and idempotent by the content-hash swap oracle: no receipt rides back — the swap
    announces itself through the telemetry `self_update` field and convergence shows as the
    next heartbeat's `binaries` fingerprints flipping the machine row's `bin_status` to
    `current`. `member_id` is informational addressing only (the frame already rides the
    warden's own connection). A warden build that predates this verb MUST treat it as any
    unknown rpc: log + skip, reader loop unharmed — which is what makes shipping the verb
    fleet-wide non-breaking.
  - **Outsource workers ride the SAME `start`/`stop` verbs** (A案 P5b naming
    convergence — the former `worker_start`/`worker_stop` verbs are RETIRED):
    a worker spawn is a plain `start` with `member_id == <ow-id>`,
    `role == "outsource-worker"`, and a server-minted token (JWT `sub == ow-id`,
    agent-class floor) — the warden derives session `member-<ow-id>` under the
    agents/ workdir root, indistinguishable from a member spawn by mechanism. A
    worker kill/reclaim is a plain `stop` `{member_id: <ow-id>}` (MAY be
    re-pushed / broadcast to several wardens — stopping an absent session is a
    clean no-op, so redundant delivery is harmless by design).
  - **P5b transition guard (legacy `worker-<ow-id>` residuals)**: sessions
    spawned by a pre-P5b build live under the retired `worker-*` namespace. A
    `stop` whose `member_id` carries the `ow-` prefix ALSO reaps the exact
    derived `worker-<ow-id>` session (never a pattern), and the warden keeps
    accepting the legacy `worker_stop` `{worker_id}` verb as an alias for one
    transition window (an old server reclaiming through a new warden). The
    retired `worker_start` is refused as unknown-rpc (logged + skipped, reader
    loop unharmed).
- **Confidentiality**: `member_token` / `worker_token` is a secret riding inside `args`. A
  command frame MUST be written only onto the addressed warden's connection — never the
  owner-scope entity fan-out.
- **Queue semantics**:
  - per-warden FIFO; drain MUST pop all pending frames in FIFO order;
  - delivery is **at-most-once** onto the downstream (fire-and-forget). A frame drained into
    a dying connection is lost by design — recovery is NOT redelivery but re-decision from
    presence on the next reconcile tick (the zero-field declarative guarantee;
    spec/lifecycle.md §4.6);
  - the queue is in-memory only (inventory #6): a restart drops pending frames; this MUST be
    harmless because the reconcile producer re-folds and re-enqueues;
  - unbounded by default; a configured positive cap makes enqueue fail (→ dispatch reports
    not-accepted → retry next tick) rather than grow a wedged backlog.
- Band evaluation happens on quiet ticks only (buffered entity deltas drain first); relative priority among bands is an implementation detail.

## 8. Task-close nudge band (directed signal, the executor's connection only)

A directed reminder pushed down the **task executor's own** connection when its task lands
in a terminal status (M3 Phase 6C): walk the §6.3 close-out — fold this run's learnings
back into the type's manual (`write_task_learnings`), clean the task's scratch, then
report the follow-ups done (`report_task_closeout`). Owner/dashboard connections MUST
never receive it.

- Frame shape — a bare `data:` event, **no `id:` line** (not part of the replayable delta
  stream; same family as §6/§7):

```
data: {"topic":"task-close","data":{"topic":"task-close","to":"m-1a2b3c","task_id":"t-7d40aabbccdd","task_no":"T-7d40","type":"review-pr","status":"done","reason":"任務 T-7d40 已結束（done）。…write_task_learnings…report_task_closeout…"}}
```

  The inner payload duplicates `topic` and carries `{topic, to, task_id, task_no, type,
  status, reason}`. `reason` wording is not contract; the envelope shape, `to` (the
  executor id), `task_id`, `type` and `status` are.
- Emission rules (all MUST hold):
  - the task just entered a **terminal** status — `done` AND `terminated` both nudge
    (a terminated run's lessons are worth folding back too);
  - the task **has a type** (`type_key` non-blank) — an ad-hoc task has no manual to
    write learnings into;
  - the task **has an executor** — an unassigned task has nobody to remind.
- Delivery is **best-effort at-most-once** onto the executor's live connection: no live
  connection at close time → the frame is dropped, never queued (a nudge is a reminder,
  not a command — contrast the §7 FIFO). No per-connection band state, no cooldown: one
  close, at most one frame.
- Fail-safe: a marshal fault or a missing listener MUST emit nothing and MUST NOT fail
  the terminal-status write it follows.

## 9. What is deliberately NOT in this contract

- The internal buffer/queue/poll mechanics and the 0.25 s poll cadence
  — implementation-free (any concurrency model is fine) provided §1–§8 hold.
- Topic-list validation as a mechanism — an implementation MAY enforce the closed set at
  the publish seam (recommended), so long as all 12 topics of §3.1 pass.
- Frame ordering **across** connections, and timing between a durable commit and its frame's
  arrival (only per-connection publish order is contract, §4).

## Appendix A — in-memory state covered by this document

From the migration inventory (§0.5), items assigned to spec/sse.md:

| # | item | section |
|---|---|---|
| 1 | presence projection (`is_online`/`online_members`) | §5 |
| 2 | observed position (`machine_of`, token machine claim) | §5 (claim shape: lifecycle §1) |
| 4 | context-high per-connection band state | §6 |
| 6 | warden-command FIFO | §7 |
| 8 | seq/epoch counter, restart rollback | §2.1 |

(#3 context gauge and #5 telemetry/bank edge live in spec/lifecycle.md; the disconnect-bank
edge itself is §5.2 here because it is a connection-lifecycle fact.)
