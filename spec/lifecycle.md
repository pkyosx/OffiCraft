# spec/lifecycle.md — identity, boot, and reconcile lifecycle contract (M1 wire freeze)

> Status: **frozen** (M1 spec freeze). Behavioural contract for the lifecycle surfaces that
> `spec/openapi.json` cannot express: the JWT claim envelope and secret resolution, the boot
> context assembly, token TTL semantics, and the server-reconcile producer's decision
> surface (the START/STOP/UNINSTALL state machine and its timers). A replacement
> implementation MUST satisfy every MUST/MUST NOT here.
>
> Source of truth at freeze time: commit `6dd7280`; docs/design/state-model.md is the
> owner-approved logical spec.

## 1. JWT — the one identity token (REST / MCP / SSE)

### 1.1 Format and claims

- Compact JWS: three base64url segments `header.payload.signature`, HMAC-SHA256 over
  `header.payload` keyed by the symmetric secret. Header MUST be
  `{"alg":"HS256","typ":"JWT"}`.
- Claim envelope:

```json
{"sub":"m-1a2b3c","scope":"agent","iat":1752192000,"exp":1752278400,"machine_id":"m-9f8e7d"}
```

  - `sub` — the identity id (the owner id, or a member id); the write-attribution key.
    MUST be non-empty (mint refuses an empty sub; verify rejects a missing sub).
  - `scope` — capability scope, closed vocabulary `"owner" | "agent"`.
  - `iat` / `exp` — integer unix seconds; `exp = iat + ttl`.
  - `machine_id` — OPTIONAL placement claim ("token = who + where"): the machine the agent
    was minted onto. Omitted entirely when empty. Owner tokens and warden
    tokens carry no `machine_id`. The SSE listener projects observed position from this
    claim (spec/sse.md §5).
- Verification MUST, in order: check the 3-segment shape; reject any header `alg` other
  than `HS256` (no `alg:none` downgrade); compare the signature **constant-time**; require a
  numeric `exp` and reject `now >= exp` (expired); require a non-empty `sub`. Failures MUST map to 401 at the HTTP gate.

### 1.2 Signing secret — DB settings authority

The signing secret lives in the DB settings store (`auth.jwt_secret`, base64url of the raw
key bytes), loaded ONCE at app assembly. It is decoupled from the owner password: a
password change never rotates it, so already-issued tokens keep verifying.

Provisioning, first match wins:

1. an existing DB `auth.jwt_secret` value (the steady state);
2. one-shot import from a pre-settings-table install's `oc.toml`: an explicit
   `[auth].secret` (UTF-8 bytes) verbatim, else — when the file carries a password — the
   password-DERIVED secret `SHA-256(b"officraft.jwt.hs256.v1:" + password_utf8)`
   (this derivation string stays contract for the migration: every token such an install
   had already issued is signed with that derived key, so importing it means zero
   token invalidation);
3. a fresh 32-byte random mint (a truly new install).

HS256 + a shared secret means tokens minted by one implementation verify in another. The
retired `var/jwt_secret` fallback file has no successor.

### 1.3 Mint surfaces and TTL semantics

| mint | scope / sub | ttl | machine_id claim |
|---|---|---|---|
| `POST /api/login` (owner password → token) | `owner` / the fixed single-tenant owner id `"owner"` | DB setting `auth.token_ttl` (default **86400 s**; owner-adjustable via `PATCH /api/settings`, applies from the next login) | none |
| `POST /api/tokens/mint` (owner-gated) | `agent` / `body.member_id` | `min(ttl_days*86400, 400 days)` — the 400-day ceiling MUST cap every long-lived agent token | none |
| `POST /api/bootstrap` (with `member_id`) | `agent` / member id | `token_ttl` | `member.desired_machine_id` (omitted if empty) |
| reconcile START payload (server-side, per spawn) | `agent` / member id | `token_ttl` | `member.desired_machine_id` |
| machine onboard exec-token | `agent` / warden member id | default **90 days**, still capped at 400 days | none (warden tokens carry no placement claim) |
| `POST /api/machines/claim` (public; redeems a one-time claim code) | `agent` / warden member id | default **90 days**, still capped at 400 days — the same mint onboard performs | none (warden tokens carry no placement claim) |

- Login MUST verify the password against the DB-stored argon2id hash (`auth.password_hash`)
  and answer a flat 401 for a wrong password OR no set password, with no distinguishing
  hint (the first-run state is disclosed only by the PUBLIC `GET /api/auth/status`).
- First-run claim: while no password is set, serve start mints a one-shot
  `auth.claim_token` and prints it ONLY to the local serve log / installer banner;
  `POST /api/auth/set-password` MUST require it (401 mismatch; 409 once a password
  exists) and MUST consume it on success. Possession proves host shell access — the gate
  against a public-tunnel visitor claiming a fresh server.
- Machine claim codes: the onboard / boot-command responses mint a **one-time claim code**
  (32 random bytes, base64url) alongside the exec-token, and the `boot_command` one-liner
  carries the CODE (`install.sh?code=`), never the token. The code lives **600 s**, is
  **single-use** (consumed atomically by a successful `POST /api/machines/claim`), and is
  held in memory only — a server restart voids it, which MUST read exactly like expiry.
  Every failed redemption (unknown / expired / already used) is the same flat 401 with no
  distinguishing hint. The legacy `install.sh?token=` surface stays byte-identical
  indefinitely.
- Token verification is stateless with ONE revocation cut: owner-scope tokens whose `iat`
  is earlier than the DB `auth.password_changed_at` (stamped by
  `POST /api/auth/change-password`) MUST be refused (401). Agent/warden tokens are
  unaffected; for them expiry stays the only invalidation.

## 2. Boot context — the three-block assembly

The single shared fold both boot paths use (`POST /api/bootstrap` and the reconcile START
payload) — they MUST produce byte-identical context for the same inputs.

### 2.1 Role resolution

`role_key := explicit role param → member.role_key → "assistant"`. The
resolved role folds as: owner overlay (non-tombstoned) wins; else the file seed; neither →
fail (HTTP 404 on the bootstrap endpoint; the reconcile producer fails closed with no START). `task_type` defaults to the seed lessons task type (`"general"`). Lessons
fold per `(role_key, task_type)`: overlay wins, else the shared file seed.
The user-custom block folds from the owner's user-context row; absent/tombstoned → empty.

### 2.2 Assembly order and format — normative

The context MUST be the following parts joined with `"\n\n"`, plus a single trailing `"\n"`:

1. the 系統互動 (system-interaction) file seed, stripped — read-only by construction (no
   write endpoint exists for it). In every file seed (this block, role-def seeds, lessons
   seeds) the literal placeholder `{OWNER_ID}` MUST be substituted with the owner id
   (`"owner"`) at read time;
2. `# Role: {name or key}\n\n{definition_md.strip()}`;
3. `# Lessons ({role_key} / {task_type})\n\n{lessons_text.strip()}`;
4. `# 使用者自訂（Owner Additions）\n\n{user_text.strip()}` — **skipped entirely** when the
   owner text is blank (no noise header);
5. the 啟動程序 (boot-sequence) file seed, stripped — appended LAST (the
   recency-authoritative tail).

The seed `.md` files under the repo-root `seeds/` are language-neutral assets; a rewrite MUST
consume the same files (byte-for-byte block content equality is testable across
implementations).

### 2.3 Bootstrap response token

`POST /api/bootstrap` returns a freshly minted member JWT only when `member_id` was supplied
(a warden spawn); a UI preview (no `member_id`) MUST get `token: null`.

## 3. In-memory lifecycle stores (restart amnesia is contract)

These stores are **volatile by design** (state-model.md 大原則: observed state never enters
the DB). A rewrite MUST keep them ephemeral — persisting any of them is a behaviour change:

| store | keyed by | written by | read by | restart semantics |
|---|---|---|---|---|
| context gauge (inventory #3) | verified caller `sub` | `POST /api/agent/context` (merge: MUST NOT clobber `boot_ts`; stamps `context_pct`, `rate_limits`, `ts`, `context_pct_ts`); SSE connect stamps `boot_ts` | context-high band, auto-recycle, monitoring fold | empty on restart (honest-empty; reporter refills) |
| warden telemetry (inventory #5) | verified caller `sub` | `POST /api/monitoring/telemetry` — partial-report MERGE: only supplied fields (`rate_limits`/`tokens`/`hardware`/`cost`/`effort`/`self_update`/`command_result`, `machine`/`account` tags) overwrite; an all-absent body is 400 | monitoring fold; disconnect-edge bank | empty on restart; a purely-banked account disappears from the monitoring fold until re-reported (honest-empty by design) |
| reconcile store (inventory #7) | member id | producer tick (per-member reconcile state: `last_command`, `last_command_at`, `stop_deadline`, attempts/backoff/circuit) | producer tick | forgotten on restart → the "awaiting presence"/dedupe windows reset; the next tick re-decides from presence (self-healing) |
| warden-command FIFO (inventory #6) | warden member id | producer dispatch | SSE warden band | pending frames dropped; re-folded next tick (spec/sse.md §7) |

**Cost is a dual-state field**: `cost` lives live in telemetry (memory) and is folded into
the durable `member.banked_cost` exactly once per online→offline edge, then popped
(spec/sse.md §5.2). A rewrite MUST preserve exactly-once-per-edge banking
(pop-after-fold makes the fold idempotent against a re-fired edge).

Identity-from-token: both ingest stores MUST key on the **verified token `sub`**, never a
self-reported agent id. A non-numeric `context_pct`, or
a wrong-typed telemetry field, is a flat **400** (not 422).

## 4. Reconcile producer — the decision surface

The server owns desired-state reconciliation; the warden is a stateless executor. Commands reach the warden over the SSE warden-command band
(spec/sse.md §7).

### 4.1 Cadence and candidates

- A background tick MUST run every **30 s** and is
  **always on** — the reconcile producer is mounted unconditionally.
  There is NO kill-switch flag in the current implementation (see Appendix B #1).
- An event-driven immediate tick for one member fires on activate/deactivate;
  it MUST share the cadence's reconcile store
  and be serialized with it (one tick mutex) so the two never race — a START recorded by the
  instant tick makes the next cadence tick a no-op (idempotent, no double spawn).
- Candidate set per cadence tick: every ACTIVE non-warden member, plus any ACTIVE warden
  whose `desired_state == "uninstall"` (wardens are never spawn/stop candidates — no warden
  reconciles another warden).
- The tick loop MUST survive any single tick fault (log and continue).

### 4.2 Inputs

Per member: `desired_state` intent (`online | offline | uninstall`; junk-safe parse — any
unrecognised value MUST be treated as `offline`, fail-safe never-spawn),
the live `online` fact (**the SSE hub's `is_online` is the single online truth**), `refocus_since`, and the agent-reported stopped fact.

### 4.3 Decision rules (pure state machine)

**desired_state=online**:

- online ∧ no refocus marker → converged: no command; failure bookkeeping MUST reset.
- ¬online ∧ a START in flight (`last_command==START` within `start_timeout`) → wait
  ("starting: awaiting presence").
- ¬online ∧ START timed out → register a failure that arms exponential backoff
  (`min(base·2^(attempts−1), cap)`) but MUST NOT count toward the sticky circuit breaker
  (a silent timeout is indistinguishable from an at-most-once delivery miss). Circuit-open → no respawn until cooldown; cooldown lapse
  half-opens with a fresh retry budget (attempts reset).
- ¬online, clear of backoff/circuit → dispatch **START**. The START payload MUST be built
  server-side: fold the persona via the shared boot core (§2) + mint the member JWT (§1.3);
  a missing/inactive member, unknown role, or missing secret MUST fail closed — no START,
  state not advanced.
- online ∧ `refocus_since > 0` → **recycle** (§4.5).

**desired_state=offline** — the one-command model:

- ¬online → converged; reset bookkeeping.
- online, no grace armed → arm `stop_deadline = now + stop_grace` and dispatch NOTHING
  (the agent gets the grace window to self-stop).
- online, within grace → wait.
- online, grace elapsed → dispatch the SINGLE robust **STOP** (the warden self-escalates the
  kill; there is no separate force-kill RPC). De-dupe: MUST NOT re-issue while
  `last_command==STOP` within `stop_retry`; once `stop_retry` elapses and the member is
  STILL online, MUST re-dispatch (at-least-once over the at-most-once band; re-firing is an
  idempotent no-op warden-side).

**desired_state=uninstall** (warden members only; owner-revised 2026-07-11 — the intent is
ONE-SHOT, never a standing order):

- online → dispatch **UNINSTALL** immediately (no grace — it is an explicit owner action),
  with the same `stop_retry` de-dupe/re-dispatch discipline. While the warden stays online
  the intent stays live.
- ¬online → converged AND **consumed**: a warden observed offline while still carrying the
  uninstall intent MUST have `desired_state` folded back to `"offline"` (row kept,
  re-installable) — the offline box IS the uninstall goal state, and a residual intent is a
  standing kill order that would answer every future reconnect (a re-install) with another
  UNINSTALL (the 2026-07 uninstall→re-install loop incident). Consumption is event-driven
  on the warden's SSE disconnect edge, with a cadence-tick roster pass as the
  restart-amnesia backstop (which also self-heals stale intents already in the DB).
- Receipt fast path: a warden `command_result` with `rpc=="uninstall"`, `ok==true` folded
  via telemetry ingest ALSO flips `desired_state` back to `"offline"` (row kept);
  `ok==false` leaves the intent in place for retry while the warden remains online.
- Install-path hygiene: every (re-)install entry point (the boot-command re-fetch, the
  bootstrap-here install) MUST zero a residual uninstall intent BEFORE installing — a fresh
  warden never boots into a leftover kill order.

### 4.4 Timers (defaults are contract; all injectable for tests)

| timer | value | meaning |
|---|---|---|
| cadence | 30 s | tick period |
| `start_timeout` | 90 s | START unconfirmed → failed spawn |
| `stop_grace` | 120 s | self-stop window before the robust stop |
| `stop_retry` | 90 s | STOP/UNINSTALL re-dispatch window (lost-frame recovery) |
| `recycle_grace` | 120 s | dump-stuck fallback from `refocus_since` |
| `backoff_base` / `backoff_cap` | 5 s / 300 s | exponential start backoff |
| `circuit_threshold` / `circuit_cooldown` | 5 / 120 s | sticky breaker (verified hard failures only) |

### 4.5 Recycle (refocus / context-high auto-handover)

- A recycle never flips `desired_state` — it stays `online` throughout; the flow is:
  `refocus_since` stamped → member delta fans → the agent-side listener surfaces a
  handover SOP wake to its interactive session, which persists its state and
  self-reports over MCP (`report_stopping` → `report_stopped`; the runtime never
  auto-reports on the session's behalf) → robust STOP once the agent reports stopped
  (the first stopped report of a refocus-marked, still-desired-online member fires
  the kill event-driven, not on the next tick) OR `recycle_grace` elapses (the
  dead-session fallback — an unresponsive session that never reports is force-stopped
  by the server; the agent side needs no timeout of its own) → the SSE drop makes
  ¬online → the next tick's plain START respawns.
- **Auto-stamp**: before deciding, the tick MUST stamp `refocus_since` on any candidate
  whose actionable context pct (same stale/boot-storm guards as the SSE band —
  spec/sse.md §6) is in the HANDOVER band, only when the member is online and not already
  recycling. This replaces any SSE handover emit.
- **Loop-break**: the tick MUST clear `refocus_since`/`stopped_since`/`stopping_since` the
  moment it observes the respawn-pending state (`desired_state==online ∧ ¬online ∧
  refocus_since>0`) so a slow/never-waking respawn can never be re-killed off a stale marker.
- **Stale-stopping clear**: a desired-online member OBSERVED online while carrying
  `stopping_since > 0` MUST have the anchor cleared (survived-stop / reconnect path).

### 4.6 Dispatch discipline

- Dispatch is **fire-and-forget**: acceptance ≠ outcome; results return asynchronously via
  presence. Correlation is zero-field — no command id exists; the
  server re-derives everything from observed presence each tick.
- **Target-reachability gate**: a command MUST NOT be enqueued for a target warden that is
  not online (no live SSE downstream to drain it) — the dispatch fails closed
  (`accepted=false`), state does not advance, and the tick re-decides when the warden
  connects (no phantom START / ghost STOP).
- Queue-key resolution: a command for member M is enqueued under the member id of the
  ACTIVE warden on M's `desired_machine_id` (the machine id IS the warden's own member id);
  a warden target addresses itself.

## 5. Installer / binary surface (one line — OpenAPI covers it)

`GET /install.sh?token=<jwt>` templates the request base URL + token into a `text/plain`
bootstrap script (PUBLIC — the token authorizes the eventual install, not the fetch), and
the script pulls the warden binary from the PUBLIC `GET /api/warden/binary`. The `?code=`
variant probes that binary route (HEAD) BEFORE redeeming the one-time claim code — a
server that cannot serve the binary (503) exits with a plain-language error and the
single-use code survives for a retry. The binary routes (`/api/warden/binary`,
`/api/agent/binary`) and the MCP catalog serve disk-first (the committed `bin/` +
`spec/` files under the CWD) and fall back to copies embedded in the server binary
(server-platform builds only) — a repo-less single-file deploy still serves them. Shapes
and status codes are in `spec/openapi.json`; no hidden behaviour beyond the string
templating.

## Appendix A — in-memory state covered by this document

| # | item | section |
|---|---|---|
| 3 | context gauge | §3 |
| 5 | warden telemetry + banked-cost edge | §3 (edge mechanics: spec/sse.md §5.2) |
| 6 | warden-command FIFO loss/re-send semantics | §4.6 (queue: spec/sse.md §7) |
| 7 | reconcile producer bookkeeping | §3, §4 |

## Appendix B — doc↔code discrepancies found at freeze (code wins; spec follows code)

1. **`--no-reconcile` does not exist.** The migration plan (Phase 4 §2) requires a
   reconcile kill-switch for shadow deployment; the current implementation is
   unconditionally always-on. This spec freezes the always-on
   behaviour; the kill-switch remains a REQUIRED feature of any shadow-mode deployment of a
   second implementation (a shadow without it would spawn real agents), but it is a
   deployment-mode flag, not part of the frozen production contract.
2. **state-model.md 原則 3 (handshake machine-claim mismatch → wind-down) is not
   implemented.** state-model.md itself flags this ("code 尚未做到"); no wind-down/suicide
   path exists in the SSE connection handling at freeze. The frozen contract is the code:
   uniqueness is enforced only by the dual-SSE single-session rule (takeover + anti-flap
   throttle, spec/sse.md §5.1), not by desired-machine comparison.
3. **Stale internal docs** (behaviour already landed despite "待審/placeholder/stub"
   wording in the frozen implementation's internal comments): the warden-command drain band
   IS live ("nothing drains this queue" was stale); the producer DOES bind dispatch to a
   real warden ("PLACEHOLDER… does NOT bind to a real warden" was stale); `/api/events` and
   `/api/mcp` were listed as "stub (build order B)" — both are real.
