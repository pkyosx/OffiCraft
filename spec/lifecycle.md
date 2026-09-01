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
| `POST /api/login` (owner password **+ TOTP code once enrolled** → token) | `owner` / the fixed single-tenant owner id `"owner"` | DB setting `auth.owner_token_ttl` (default **86400 s**; owner-adjustable via `PATCH /api/settings`, applies from the next login) | none |
| `POST /api/mint` (owner-gated) | `agent` / `body.member_id` | `min(ttl_days*86400, 400 days)` — the 400-day ceiling MUST cap every long-lived agent token, and an `exp` is ALWAYS stamped (`mintJWT` computes `now + ttl` unconditionally; `ttl_days: 0` mints a token that is already expired, never a permanent one). 🔴 For a member whose `kind` is NOT `warden`, the ceiling is not a guarantee of lifetime: the token carries NO exemption from the §1.2 cut 3 agent floor, so it dies the moment that member next reports waking, however many days are left on it (owner 2026-08-30, rc-162a4ace086d option 0 — asked and accepted). A long-lived token handed to an external script therefore stops working at that member's next boot. 🔴 THE `kind="warden"` CASE IS THE EXCEPTION AND IT IS OPEN ON PURPOSE. This route resolves its target with `staffOnly`, which refuses `kind='outsource'` and NOTHING ELSE — so a mint MAY be aimed at a machine (warden) member, and §1.2 cut 3 exempts `kind="warden"` rows by design. The resulting token is therefore an `agent`-scope credential that NO boot report can end. It is NOT unrevocable: it still expires (≤ 400 days), and removing that machine from the roster still refuses it through the cut 2 machine revocation. What is missing is only the boot cut. Two guards were proposed for this — refusing a machine target here, and raising the floor at dispatch time — and owner DEFERRED both on 2026-08-31, verbatim 「都先不加」(rc-b08b0a5d678b, free text, no option selected). That is a POSTPONEMENT, not an accepted permanent gap: this paragraph MUST be revisited rather than read as a settled design. | none |
| `POST /api/bootstrap` (with `member_id`) | `agent` / member id | DB setting `auth.agent_token_ttl` (default **604800 s**) | `member.desired_machine_id` (omitted if empty) |
| reconcile START payload (server-side, per spawn) | `agent` / member id | `auth.agent_token_ttl` | `member.desired_machine_id` |
| machine onboard / boot-command / bootstrap-here exec-token | `agent` / warden member id | **no expiry** (`exp` omitted; response `expires_in=0`) | none (warden tokens carry no placement claim) |
| `POST /api/machines/claim` (public; redeems a one-time claim code) | `agent` / warden member id | **no expiry** (`exp` omitted; response `expires_in=0`) — the same permanent mint used by every warden install path | none (warden tokens carry no placement claim) |

Warden credentials are revoked by removing their machine from the roster, which rejects the
next gated request. Existing finite warden credentials remain finite until that machine is
reinstalled and receives a newly minted permanent credential. The 400-day cap remains the
ceiling for non-warden agent-token mints.

- Login MUST verify the password against the DB-stored argon2id hash (`auth.password_hash`)
  and answer a flat 401 for a wrong password OR no set password, with no distinguishing
  hint (the first-run state is disclosed only by the PUBLIC `GET /api/auth/status`).
- **Second factor (TOTP).** While `auth.totp_secret` holds an ACTIVE secret, a correct
  password is NOT sufficient: login MUST also verify a RFC 6238 code (HMAC-SHA1, 6 digits,
  30 s step, ±1 step accepted) and MUST answer the SAME flat 401 when the code is missing
  or wrong — the refusal never says which factor failed, because naming it confirms a
  correct password to someone who guessed only that half. Codes are SINGLE-USE: the
  highest step accepted is persisted as `auth.totp_last_step` and every candidate at or
  below it is refused, so a code cannot be replayed inside its own acceptance window.
  Verifying and advancing that floor MUST be one critical section, or two concurrent
  logins presenting the same code both pass.
  - **Rollout flag.** `auth.mfa_offered` (default **false**, so an install that
    upgrades into this build is unaffected until its owner opts in) decides
    whether the factor may be SET UP: while it is false `…/enroll` and
    `…/activate` MUST answer 403 and the cockpit hides the entry. It is read and
    written by the owner-gated `GET /api/auth/mfa` / `POST /api/auth/mfa/offer`,
    deliberately NOT by `GET /api/settings` (admin_agent floor + MCP tool — the
    owner's credential posture is not an agent read path).
    🔴 It MUST NOT gate verification. While a factor is armed, withdrawing the
    feature leaves login demanding the code, `mfa_required` true, and `…/disable`
    working. A flag that switched verification off would be a bypass: a stolen
    owner token could withdraw the feature and walk past the factor that exists
    to stop exactly that.
  - Arming is a two-step ceremony and BOTH steps are owner-gated: `POST /api/auth/mfa/enroll`
    writes an inert `auth.totp_pending_secret` and returns it once (the only moment a secret
    crosses the wire); `POST /api/auth/mfa/activate` MUST require the current **password**
    as well as a code from that pending secret before promoting it. The password is required
    because ARMING is as destructive as removing — a stolen owner token alone could
    otherwise install a factor the attacker controls and lock the owner out.
  - `POST /api/auth/mfa/disable` MUST require both the password and a live code: a factor a
    stolen session can switch off protects nothing after the theft. It is therefore NOT the
    lost-authenticator path — that is the local `ocserverd mfa-disable` command, which
    substitutes proof of host shell access and takes effect at the next serve start.
  - `GET /api/auth/status` additionally discloses `mfa_required` to unauthenticated callers,
    deliberately: the login wall must render the right fields before any token exists, and a
    distinguishable "password ok, code missing" refusal would leak strictly more.
- **Credential-attempt brake.** It applies to the two PUBLIC credential seams and to
  NOTHING else: `POST /api/login` and `POST /api/auth/set-password`'s claim token.
  `POST /api/auth/change-password` and the `/api/auth/mfa/activate` +
  `/api/auth/mfa/disable` credential checks are NOT braked in any way — no floor, no
  concurrency cap, no call into the throttle at all.

  🔑 **The dividing line is whether an UNAUTHENTICATED caller can reach the seam, not
  whether the caller is logged in.** Those are different sentences and set-password is
  where they come apart: it is not a login, yet it is braked, because it is public, it
  compares a caller-supplied 32-byte secret, and it runs the same argon2id as login. Any
  future seam that is public and verifies a secret belongs behind this brake regardless of
  what it is called.

  The brake keeps NO failure history: there is no attempt counter, no exponential backoff,
  no lockout and no decay window. It is two mechanisms, both on those two seams only:
  - a **concurrency cap** on credential verifications in progress at once, shared by the
    two. A caller refused for concurrency gets **429 + `Retry-After`**; that is the only
    429 these routes can produce. It exists for memory as much as for policy: argon2id is
    ~19 MiB a verification, so an unbounded burst is an OOM kill.
  - a **refusal floor**: a refusal answers no earlier than a fixed interval after the
    request started, while a SUCCESS is answered immediately. Together with the cap this
    bounds the front door at `cap ÷ floor` attempts a second.

  Because both braked seams are public, nothing an AUTHENTICATED caller does can consume a
  slot the login page needs. That coupling existed while the owner-gated seams shared the
  pool — a token holder could fill it and make the owner's own login answer 429 — and
  removing them from the brake removed it. ⚠️ The accepted consequence is that a holder of
  an owner token may guess the current password at change-password without a brake; the
  reasoning is that a stolen owner token is already the disaster this design defends against
  rather than one a delay mitigates.

  🔑 **"Without a brake" is not "unbounded", and the difference is measured rather than
  asserted.** change-password holds the settings write lock across its password
  verification, so those verifications are fully SERIALISED — 8 concurrent calls cost
  7.1–7.9x one call (ratio ≈ N), against login's 4 concurrent at 1.15–1.31x (ratio ≈ 1) as
  the control. A token holder therefore gets roughly one guess per verification, and the
  process-wide ceiling on concurrent password hashing is the brake's cap plus that one.
  Removing the brake from this route changed neither figure — the lock was always the
  binding constraint — only the depth of the queue behind the lock.

  ⚠️ That settings write lock is SHARED with `/api/login`'s second-factor step, so sustained
  traffic on change-password queues every login's code check behind it. This predates the
  brake changes and is recorded here because it was previously written down nowhere.

  There is deliberately **no lockout of any kind**, and that is the point of the shape: the
  earlier counter refused callers BEFORE verifying them, so a stranger reaching the login
  page could hold the owner out with a trickle of failures and only a host shell could lift
  it. Nothing here has state an attacker can drive.

  Three rules are contract, not style:
  - the brake MUST sit AFTER any refusal that consults no credential (the `409`s on
    set-password, mfa/activate and mfa/disable), or a documented 409 becomes a 429;
  - the gate MUST reserve, not merely read: a concurrent burst that only inspects state
    passes in its entirety, which both defeats the floor and admits N simultaneous
    argon2id verifications;
  - the floor MUST be a DEADLINE measured from the start of the request, never a sleep
    added after the decision, and it must be served with no other lock held. Every
    `/api/login` refusal — wrong password, wrong code, or both — MUST be indistinguishable
    by message AND by elapsed time, or the identical refusal message discloses through
    latency exactly what it refuses to say.
- **Password-exposure alert.** The one `/api/login` refusal that carries information is a
  CORRECT password with a wrong second factor: the password has leaked, and no brake
  repairs that. The server therefore posts a durable chat message to the seeded assistant
  asking her to have the owner change it. It MUST be dispatched asynchronously (doing the
  work inline makes that one branch measurably slower than the others and leaks the bit the
  floor is hiding) and MUST be rate-limited to at most one message per window, carrying the
  number of attempts folded into it (the trigger is attacker-controlled). It is a mailbox
  message, not a wake: an offline assistant reads it when she next comes online.
- First-run claim: while no password is set, serve start mints a one-shot
  `auth.claim_token` and prints it ONLY to the local serve log / installer banner;
  `POST /api/auth/set-password` MUST require it (401 mismatch, served no earlier than the
  refusal floor; 409 once a password exists; 429 when the concurrency cap is full) and MUST
  consume it on success. Possession proves host shell access — the gate
  against a public-tunnel visitor claiming a fresh server.
- Machine claim codes: the onboard / boot-command responses mint a **one-time claim code**
  (32 random bytes, base64url) alongside the exec-token, and the `boot_command` one-liner
  carries the CODE (`install.sh?code=`), never the token. The code lives **600 s**, is
  **single-use** (consumed atomically by a successful `POST /api/machines/claim`), and is
  held in memory only — a server restart voids it, which MUST read exactly like expiry.
  Every failed redemption (unknown / expired / already used) is the same flat 401 with no
  distinguishing hint. The legacy `install.sh?token=` surface stays byte-identical
  indefinitely.
- Token verification is stateless with THREE revocation cuts:
  1. **owner scope — the password floor.** Owner-scope tokens whose `iat` is earlier than
     the DB `auth.password_changed_at` (stamped by `POST /api/auth/change-password`) MUST
     be refused (401).
  2. **machine scope — the roster.** The machine roster is the authority over machine
     credentials (T-9cf8). Once a warden's member row is soft-deleted
     (`roster_status="removed"` — set by `DELETE /api/machines/{member_id}` and by a
     CONFIRMED `POST /api/machines/{machine_id}/teardown-here`), every gated route MUST
     refuse (401) both:
     - the machine's own token (`sub` = that warden id — a warden's token carries
       `machine_id: ""` by design, so this arm keys on `sub`), and
     - a token booted ON that machine (`machine_id` claim = that warden id), UNLESS the
       caller's own row now names a DIFFERENT `desired_machine_id` — i.e. the roster has
       already relocated it and only a stale token still points at the deleted host.

     Scope notes, all load-bearing: the cut applies to `kind="warden"` rows ONLY —
     `roster_status="removed"` is ALSO how a released outsource worker and a dismissed
     member are recorded, and a released worker is contractually still working (§6.3
     close-out). A failed roster read MUST NOT revoke (unknown ≠ revoked). `POST
     /api/machines/{member_id}/uninstall` KEEPS the record and therefore does NOT revoke
     anything; the machine stays on the roster and re-installable.

     **Scope of "immediate" — it is per REQUEST, not per connection.** The cut lives in
     the auth gate, which runs when a request (or an SSE handshake) ARRIVES. It therefore
     does NOT tear down an SSE stream that is already open: a deleted machine whose warden
     is mid-stream keeps that stream, and keeps projecting `online`, until it disconnects
     on its own; only its RECONNECT is refused. What it cannot do meanwhile is act — every
     new request 401s, and both `reconcile` and `wardenTargetOf` require an ACTIVE roster
     row, so nothing new is ever enqueued for it. Read "the credentials stop working
     immediately" as "no request succeeds from now on", not "the socket drops now".

     Refusal precedence on `GET /api/events`: the auth gate runs BEFORE the zombie stop
     gate, so a removed WARDEN's reconnect is a 401 (this cut), while a dismissed
     non-warden member's reconnect stays the pre-existing 409 (`sseStopGateRefusal`) —
     the kind restriction above is what keeps those two apart.

     Because this cut turns "the server host's warden row was soft-deleted" into a
     credential revocation that also takes every agent placed on `m-server-self`,
     BOTH verbs that would soft-delete that row MUST refuse it with the same 409:
     `DELETE /api/machines/{member_id}` (pre-existing) and
     `POST /api/machines/{machine_id}/teardown-here` (added with this cut). The
     teardown refusal MUST precede the `ocwarden` subprocess — a 409 written after
     the daemon was booted out is worse than no guard.

  3. **agent scope — the member's own last 開工.** `POST /api/self/waking` MUST raise the
     reporting member's `agent_iat_floor` to the `iat` of the token THAT CALL was made
     with, and every gated route MUST then refuse (401) any `agent`-scope token whose
     `iat` is STRICTLY LESS THAN that member's floor. A member's generations overlap on
     purpose (§6.3 close-out), so the id alone cannot say which one is speaking; the
     caller's `iat` can, and this cut is what makes the NEW generation coming up end the
     previous one's authority (owner 2026-08-30, rc-fe6451abe579: 「新的一輪一上線就失效」,
     a handover cut in half being the accepted cost).

     The stamp MUST be the caller's OWN `iat` and the comparison MUST be strict. Together
     those are what stop the session that raises the floor from being refused by it,
     whatever the gap between its mint and its boot report and whatever the skew between
     the two clocks; a floor stamped from the server clock would refuse a token that
     merely took a few seconds to arrive.

     Scope notes, all load-bearing: `kind="warden"` rows are EXEMPT, and that is a safety
     property rather than an optimisation — a warden credential is `agent` scope with NO
     `exp` (§1.3), so a floor raised above one could never expire out of the way and the
     machine would be off the fleet permanently, recoverable only by a hand re-install.
     A failed roster read MUST NOT refuse (unknown ≠ superseded), and a member that has
     never reported waking (floor 0 — every row predating the cut) refuses nothing.

     ⚠️ **NOT SOLVED, deliberately** (owner 2026-08-28: 「先不管搶同一秒的問題好了」):
     `iat` is whole seconds, so two generations of one member that start inside the SAME
     second are indistinguishable to this comparison and neither is refused. Nothing in
     this cut claims otherwise.

     🔴 **WHAT THIS CUT DOES NOT REACH.** The refusal is evaluated at REQUEST time, so an
     SSE stream that was ALREADY accepted is never re-checked against the floor. On the
     ORDINARY path that is not a narrow race but a routine gap of seconds to minutes,
     because the boot order raises the floor FIRST and connects the stream LAST:

     - Both boot sequences are `1. report_waking` → `2. resume_summary` → `3. ocagent
       listen`, and both state 不可更改順序 in those words (`seeds/boot_sequence.md`,
       `seeds/boot_sequence_codex.md`). `cli/ocwarden/spawn.go` restates the same order in
       its SOP comment (`report_waking → resume_summary → ocagent listen`).
     - NOTHING attaches a listener at spawn. On the claude path warden launches only
       `claude`; the model itself starts `ocagent listen` at step 3. On the codex path the
       seed forbids the model to start one at all (「不要自己啟動 `ocagent listen`」) and
       `cli/ocwarden/codex_session.go` execs it only on the FIRST `turn/completed` — i.e.
       AFTER the turn in which the model has already called `report_waking`.
     - Step 2 is not instant: `resume_summary` can be big enough that the seed tells the
       model to spend a whole sub-agent on it rather than burn its own context.

     So the ordinary sequence is: the successor's `report_waking` raises the floor (step 1)
     → from that instant the outgoing session's ordinary calls 401, WHILE ITS SSE IS STILL
     ATTACHED and, being already accepted, never re-checked → seconds to minutes later the
     successor finally reaches step 3 and its SSE connect kicks the incumbent off
     (`Hub.Connect` is kick-old-admit-new inside ONE critical section: the `delete` and
     `close(old.kicked)` run under the same `h.mu.Lock()` as the insert). For the whole of
     that gap a live stream belongs to a credential the server has already stopped
     accepting, and the outgoing session is a mute listener: it still receives events, it
     just cannot act on any of them.

     Two further shapes, neither closed here:

     - **(a) the successor never reaches step 3.** If it stalls or dies during boot, the
       floor stays up and the incumbent keeps the SSE slot — and keeps the member
       projected `online` — until it drops on its own. Nothing else bounds this.
     - **(b) when the takeover is throttled.** The anti-flap guard (`takeoverBurst` kicks
       within `takeoverWindow`, hub.go) returns 409 BEFORE the delete/close, so a
       throttled successor does not kick anyone and the INCUMBENT keeps the slot. Its
       `report_waking` is a separate call and still raises the floor, so here too the old
       stream outlives the cut, un-re-checked, until it drops on its own — at which point
       its reconnect meets the floor and gets the `X-OC-Auth-Refusal: agent-superseded`
       marker (authz.go).

     None of this is closed by this package. Re-checking live streams against the floor
     would close all of it and is NOT proposed here.

     🔴 **TWO NAMED DEBTS, DEFERRED BY THE OWNER, NOT CLOSED** (2026-08-31, rc-b08b0a5d678b,
     verbatim 「都先不加」, free text, no option selected). Both were put to him as guards for
     the `kind="warden"` exemption above and both were declined FOR NOW; they are recorded
     here so the hole is named rather than implied by the exemption's absence:

     1. **The mint door is open.** `POST /api/mint` resolves its target with
        `staffOnly`, which refuses only `kind='outsource'` — so a mint MAY be aimed at a
        warden member, and the token it returns is `agent` scope that NO boot report can
        ever end (§1.3, mint table). Refusing a machine target at that route was proposed
        and deferred.
     2. **The floor is not raised at dispatch.** Only `report_waking` moves it, so between
        a START dispatch and the new session's own boot report the previous generation's
        token is still good. Raising the floor at dispatch time was proposed and deferred.

     Deferral is a POSTPONEMENT and not an accepted permanent design: revisit both rather
     than reading their absence as settled.

  For every other agent token — a `kind="warden"` credential, and any token on a member
  that has never reported waking — expiry stays the only invalidation.

### 1.4 Credential renewal (`POST /api/machines/renew-credential`)

A machine MUST be able to replace its own credential without anyone reinstalling that
host. Reinstalling is the only way to change a machine's credential today, and it stops
the running daemon to do it — a cost that buys nothing when the credential is the only
thing that needed replacing.

- The endpoint MUST take **no request body and no target parameter**. The machine acted
  on is the caller's verified `sub`. "One machine cannot renew another's credential" is
  therefore a property of the request SHAPE, and an implementation MUST NOT satisfy it
  instead with a target field plus a comparison.
- It MUST refuse any caller whose member row is not an ACTIVE machine (`kind` = warden,
  roster status active). The route's principal class MUST NOT be relied on for this:
  the machine class ranks BELOW the ordinary agent class, so the choke that would admit
  a warden admits an ordinary agent too.
- On success it MUST mint through the same warden mint every install path uses, and
  answer the token, its `expires_in`, and the `machine_id` it is bound to.
- The previous credential MUST NOT be invalidated by the renewal. Verification is
  stateless, so both remain valid until they expire; renewal is additive. A caller that
  cannot persist the new credential MUST therefore still be running on the old one.
- A machine removed from the roster MUST NOT be able to renew. This follows from §1.2
  rather than from this endpoint, and an implementation MUST pin WHICH refusal answers:
  while warden credentials carry no `exp`, the permanent-credential refusal answers
  first and the roster-revocation arm is never reached on this route.

## 2. Boot context — the three-block assembly

The single shared fold both boot paths use (`POST /api/bootstrap` and the reconcile START
payload) — they MUST produce byte-identical context for the same inputs.

### 2.1 Role resolution

`role_key := explicit role param → member.role_key → "assistant"`. The
resolved role folds as: owner overlay (non-tombstoned) wins; else the file seed; neither →
fail (HTTP 404 on the bootstrap endpoint; the reconcile producer fails closed with no START). Lessons
fold per `role_key` alone (T-2 removed the `task_type` axis): overlay wins, else the shared file seed.
The user-custom block folds from the owner's user-context row; absent/tombstoned → empty.

### 2.2 Assembly order — normative

The boot context is these blocks, in this order, joined into one document:

1. **系統互動** — the shared system-interaction file seed;
2. **使用者自訂** — the owner's additive block;
3. **角色定義** (`# Role:`) — what this role does;
4. **判準** (`# Insight`) — how this role weighs things;
5. **學習筆記** (`# Lessons`) — what it has learned doing it;
6. **啟動步驟** — the boot-sequence file seed, selected by the READER'S OWN runtime
   (`claude | codex`, blank folding to `claude`), and carrying that runtime's 執行環境
   section. It is LAST — the recency-authoritative tail — and nothing may be appended
   after it.

Blocks 3-5 are the persona. Two blocks are dropped entirely when they fold blank —
使用者自訂 and 判準 — so a role that has never written a 判準 simply has no such section,
rather than an empty heading.

One normative BEHAVIOUR of the 學習筆記 block is stated here rather than delegated,
because it is a contract and not a formatting detail: the title injection MUST be
**idempotent** (T-8327). A generation that treats its own boot segment as the document
base and writes it back turns the injected title into document content, so an assembler
that blindly prepends stacks one more title per generation. The assembler MUST strip any
leading copies of the exact title line before prepending exactly one. ⚠️ This rule is
pinned by `server/ocserverd/api_lessons_patch_test.go`, **not** by the conformance suite —
no conformance case feeds in a lessons doc that already starts with its own title, so
satisfying conformance alone does NOT get you this behaviour.

**The remaining assembly rules are deliberately not restated here.** The exact section
titles, string formats, separator and trailing newline, and the seed placeholder
substitution live in the implementation (`buildBootContext`) and are pinned byte-for-byte
by `conformance/test_lifecycle.py`. That suite is what a rewrite must satisfy for those;
this list is only the shape. A prose copy of formatting details is a second source that
goes stale in silence — this very section did exactly that when 判準 was added to the
fold and the list here was not updated.

An outsource worker's boot context is this same document **with the persona removed** —
it has no role, so it carries no 角色定義, no 判準 and no 學習筆記 — in this same order,
with no outsource-specific document of any kind. 使用者自訂 used to sit between 學習筆記
and 啟動步驟; T-4595 moved it above the persona so that the staff and outsource
assemblies would line up, leaving the persona as their only difference.

"No outsource-specific document of any kind" is normative and exhaustive: the outsource
assembly MUST NOT carry an outsource overlay seed, an identity block, the worker's bound
task, its task-type manual, or a second copy of the runtime's execution-environment
instructions. Identity is supplied the way it is for staff (the launcher's appended system
prompt); the task and the manual are fetched by the worker itself after boot, so a
boot-time copy could only be a stale snapshot. Byte-for-byte, the outsource context equals
the staff context with the persona removed, and that equality is the testable form of
this paragraph.

The seed `.md` files under the repo-root `seeds/` are language-neutral assets; a rewrite MUST
consume the same files (byte-for-byte block content equality is testable across
implementations).

### 2.3 Bootstrap response token

`POST /api/bootstrap` returns a freshly minted member JWT only when `member_id` was supplied
(a warden spawn); a UI preview (no `member_id`) MUST get `token: null`.

## 3. In-memory lifecycle stores (restart amnesia is contract — one named exception)

These stores are **volatile by design** (state-model.md 大原則: observed state never enters
the DB). A rewrite MUST keep them ephemeral — persisting any of them is a behaviour change.
**One narrow, deliberate exception**: the warden-command FIFO mirrors the `update` verb (and
only that verb) to a durable table, because that one command has no compensating re-decision;
the row is written on enqueue and forgotten when the frame reaches the socket. Nothing else
in this section may be persisted, and the mirror stores an ORDER, not observed state:

| store | keyed by | written by | read by | restart semantics |
|---|---|---|---|---|
| context gauge (inventory #3) | verified caller `sub` | `POST /api/agent/context` (merge: MUST NOT clobber `boot_ts`; stamps `context_pct`, `rate_limits`, `ts`, `context_pct_ts`); SSE connect stamps `boot_ts` | context-high band, auto-recycle, monitoring fold | empty on restart (honest-empty; reporter refills) |
| warden telemetry (inventory #5) | verified caller `sub` | `POST /api/monitoring/telemetry` — partial-report MERGE: only supplied fields (`rate_limits`/`tokens`/`hardware`/`cost`/`effort`/`runtime`/`runtimes`/`self_update`/`command_result`, `machine`/`account` tags) overwrite; `runtimes` is a value-free provider readiness map and MUST NOT contain credential material; an all-absent body is 400 | monitoring fold; disconnect-edge bank and runtime-capable placement | empty on restart; a purely-banked account disappears from the monitoring fold until re-reported (honest-empty by design) |
| reconcile store (inventory #7) | member id | producer tick (per-member reconcile state: `last_command`, `last_command_at`, `stop_deadline`, attempts/backoff/circuit) | producer tick | forgotten on restart → the "awaiting presence"/dedupe windows reset; the next tick re-decides from presence (self-healing) |
| warden-command FIFO (inventory #6) | warden member id | producer dispatch | SSE warden band | pending frames dropped **for every verb except `update`**, and re-folded next tick from observed presence; `update` alone has a durable mirror (`warden_command_queue`) and is restored into the FIFO on restart, because nothing re-derives "the owner asked this machine to upgrade". START is excluded on top of that — its `args` carry a live `member_token` (spec/sse.md §7) |

**Cost is a dual-state field**: `cost` lives live in telemetry (memory) and is folded into
the durable `member.banked_cost` exactly once per online→offline edge, then popped
(spec/sse.md §5.2). A rewrite MUST preserve exactly-once-per-edge banking
(pop-after-fold makes the fold idempotent against a re-fired edge).

Identity-from-token: both ingest stores MUST key on the **verified token `sub`**, never a
self-reported agent id. A non-numeric `context_pct`, or
a wrong-typed telemetry field, is a flat **400** (not 422) — **EXCEPTION**: the three
telemetry blocks whose nested shape the spec DECLARES (`hardware` / `claude` / `runtimes`,
T-90be) are typed as objects, so a non-object THERE is refused by the decoder as a **422**
before the handler runs. The undeclared blocks (`binaries` / `rate_limits` / `tokens` /
`command_result` / `self_update`) still answer the flat 400, and so does the all-absent body
— `{}` and `{"hardware": null}` alike, because a JSON null decodes to an ABSENT field, not
to a wrong type. The split is where the refusal happens, not how strict the wire is: the
declared blocks' CONTENTS stay permissive (`additionalProperties` true — an unknown nested
key must still land, see `TestHandleIngestTelemetry_UndeclaredNestedKeyStillLands`), and an
empty `hardware: {}` is a 200, because a report whose every probe failed is still a sample.
The whole table is executable in
`server/ocserverd/api_monitoring_test.go::TestHandleIngestTelemetry_WrongTypedBlockStatusTable`,
so this paragraph can only drift from the wire across a red test.

Their nested VALUES stay permissive too, and that is a separate decision from the one above
(T-aad2). A declared key carrying the wrong TYPE — `hardware: {"cpu_pct": "47"}` — is a
**200**, stored verbatim: refusing it is the same fail-closed tightening the owner ruled out
for these blocks (rc-55861dd893c6), and its blast radius is the whole heartbeat, not one
field. What changes is that the READ path no longer hides it. `MonitoringMachineDTO.
hardware_invalid` names the declared keys of the SERVED sample that arrived unreadable, so
"measured but unusable" stops being byte-identical to "never measured" — the same job
`hardware_stale` does for the expired case, and the reason all three blanks are now tellable
apart. Honest-empty for a clean, stale or absent sample; key names only, never the offending
value; per key, because one broken probe says nothing about its siblings; and NEVER for an
undeclared key, which has no declared type to violate. The producer side of the same
contract is a CI guard over our own payloads
(`cli/ocwarden/telemetry_wire_test.go::TestWardenTelemetryValueTypesMatchFrozenSchema`) —
it constrains the warden, not the wire.

**Coverage, exactly** — the three declared blocks are protected by three different
mechanisms, and conflating them sends the next reader to the wrong place:

| block | wrong-typed nested VALUE | why |
| --- | --- | --- |
| `runtimes` | **400 at ingest**, per key (`installed` / `logged_in` / `version`) | pre-existing handler validation; the value never reaches the store, so a read-side marker there could never fire |
| `hardware` | **200, stored, and NAMED on the read side** (`hardware_invalid`) | nothing is refused (owner ruling), so the read path is the only place it can surface — and it is the block the cockpit renders values from |
| `claude` | **200, stored, read back as null, SILENT** | the remaining hole. Only guard is the warden-side CI test over OUR OWN payloads, so an older or third-party warden drifting there is invisible at runtime |

That last row is known, deliberately out of scope here, and tracked separately. The
read-side marking is `hardware` only because that is where the cockpit reads values from
(MonitorPage's machine table joins registry rows for identity against THIS fold for every
hardware cell; `MachineDTO` carries no hardware at all).

## 4. Reconcile producer — the decision surface

The server owns desired-state reconciliation; the warden is a stateless executor. Commands reach the warden over the SSE warden-command band
(spec/sse.md §7).

### 4.1 Cadence and candidates

- A background tick MUST run every **30 s** and is **always on in production**. Since
  T-14 item 5 there is ONE cadence loop, not two: it runs the reconcile half
  (`runReconcileTick`) and then the outsource half (`runOutsourceTick`), each under its own
  lock and never both at once, and each half is skipped when ITS serve flag is passed.
  `--no-reconcile` is the shadow-deployment kill-switch for THIS producer: it disables it
  WHOLESALE (its half of the cadence tick AND every event-driven warden-command dispatch it
  owns), which
  covers **every command this producer DISPATCHES** at a staff-member or warden row — START,
  STOP, UNINSTALL, and the machine-upgrade `update` kick (`api_machines.go`). Every
  member-facing HTTP verb funnels into those seams.
  🔴 **Two holes, and neither flag closes them.**
  (a) **Restore is not dispatch.** The durable `update` mirror (§3) is rehydrated into the
  FIFO at assembly (`BindWardenCommandStore`, wired unconditionally before the flag is even
  read) and drained onto the first warden that connects, with no flag consulted on either
  side. So a shadow assembled over a copy of production data — which, on a flagged shadow, is
  when that table is non-empty at all, since the flag blocks the only path that mints such a
  row — can still deliver a pending upgrade kick to a real machine. Three things
  BOUND this without closing it: rows older than the 24 h `wardenCommandTTL` are swept before
  the restore; the frame is addressed to one warden's own id, so it drains only if that real
  warden connects to the shadow; and the kick is idempotent at the warden (content-hash swap
  oracle, spec/sse.md §7).
  (b) **Outsource workers.** Worker spawn and stop dispatch (`worker_spawn.go`) never consults
  `--no-reconcile`; the mirror flag `--no-outsource` gates only the assignment PRODUCER
  (`outsourceTickNow`, plus that producer's half of the cadence tick). Everything else that reaches
  `respawnWorkerForOwnerOp` / `enqueueWorkerStop` consults **neither** flag — the owner verbs
  (restart, model change, relocate, stop, refocus), a task terminate that dismisses its
  workers, and the worker's own `report_stopped` — so a shadow server with both flags set
  still spawns and kills real worker sessions. This list is not exhaustive; the invariant to
  rely on is the negative one: **the flag gates the reconcile producer and nothing else.**
  What "the producer" covers is wider than dispatch, including: its dispatch seams; the
  desired-state control writes that ride the same posture (`consumeUninstallOnDisconnect`
  enqueues nothing and is still gated); the row stamps the decide pass itself performs
  (`stampWakeObservability`, `stampMemberPlacementBlocked`, and — T-14 #4 — `armDecidedHandover`,
  which opens a relocate wind-down epoch on the row); the one HTTP kick that borrows it
  (`api_machines.go`); and — because the whole reconcile HALF of the cadence tick is skipped —
  every roster pass that half runs BEFORE it decides anything (context-high recycle stamping,
  recycle-marker and stale-stopping clears, uninstall-intent consumption,
  lapsed-receipt sweep). Each of those passes persists real member rows. A
  rewrite that gates only the dispatches leaves every one of them running against real data.
  A shadow deployment must keep those paths away from real machines by some other means;
  there is no flag for it today.
  Each flag is a deployment-mode flag, not a production control — nothing in the frozen wire
  contract turns them on or off, and no API surface exposes them.
  (Appendix B #1 records the freeze-time state, when the flag did not yet exist.)
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
the persisted runtime (`claude | codex`; absent/blank legacy rows fold to `claude`),
the live `online` fact (**the SSE hub's `is_online` is the single online truth**),
`refocus_since`, the agent-reported stopped fact, and the selected machine's volatile
runtime capability report.

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
  server-side: fold the persona via the shared boot core (§2) + mint the member JWT (§1.3)
  + include the normalized runtime. The payload is otherwise provider-neutral and the
  warden selects the Claude or Codex adapter from that field;
  a missing/inactive member, unknown role, or missing secret MUST fail closed — no START,
  state not advanced.
- online ∧ `refocus_since > 0` → **recycle** (§4.5).

**desired_state=offline** — the one-command model:

**This arm runs NO CLOCK.** Owner ruling 2026-08-16 (card `rc-27d1710174dd`, option ①):
「不要兜底：只有你按強制下線才收它」. The server does not arm a deadline here and never
decides that time is up.

- ¬online → converged; reset bookkeeping.
- online → the member is `stopping` and the producer dispatches **NOTHING**, indefinitely.
  The agent has been handed the offboard sequence and is working it; a clock here would
  cut off a session that was told there is no countdown.
- Collection **on this online arm** has three sources, and the server arms none of them:
  1. **the agent's own `report_stopped`** — that call itself dispatches the SINGLE robust
     **STOP**, event-driven, not on the next tick;
  2. **the owner pressing 加速停止** (T-ed79) — `POST /api/members/{id}/accelerated-stop`
     re-stamps `stopping_since` and writes `refocus_op = accelerated_stop`, so `decideDown`
     collects at `stopping_since + stop.accelerated_grace_secs` and `offboardKindOf`
     quotes that same instant to the agent. This does NOT reopen `rc-27d1710174dd`: the
     ruling is about the SERVER deciding time is up, and nothing here arms without the
     owner's press; and
  3. **the owner pressing 強制下線** — the SAME command, it only skips the waiting.
  (A member still `waking` is a different case and is NOT covered by this arm: deactivating
  a wake that has been dispatched but never connected force-stops it outright — nobody is
  inside being told anything. See §4.2 and `docs/design/offboard-flow.md` §三.)
- There is no separate force-kill RPC either way: the warden self-escalates the kill.
- 🔴 **An OUTSOURCE worker now has all three arms too (T-ed79, owner 2026-08-21
  「往正職靠：外包那顆改成優雅停止，強制殺移到第三顆按鈕」).**
  `POST /api/outsource-workers/{id}/stop` is a GRACEFUL close-out: it sets
  `desired_state=offline`, stamps `stopping_since`, clears any in-flight refocus epoch,
  fans the 〈停止〉 notice at the worker's own session and **returns**. It does **not**
  kill and it does **not** stamp `forced_stop_at` — that anchor is what keeps the FORCED
  verb silent, and this verb's whole point is that the notice arrives. The 收口 is the
  worker's own `report_stopped`, exactly as on the staff 下線 arm. An OFFLINE worker (no
  session to hear the notice) still takes the immediate kill.
  ⚠️ **The kill moved, it was not removed**: it is now
  `POST /api/outsource-workers/{id}/force-stop`, which stamps `forced_stop_at` **and**
  `stopping_since` (T-c996's pairing, unchanged) and says nothing. The middle rung
  `POST /api/outsource-workers/{id}/accelerated-stop` covers BOTH worker arms — it used
  to 409 on a `desired_state=offline` worker, which was right while 停止 killed on the
  spot and would be a dead rung now.
  ⚠️ **The gap this bullet used to record is closed**: there IS now a verb that gives an
  outsource worker the graceful 下線 a staff member gets, and it is the one named 停止.
- ✅ **The second outsource gap is closed (T-fe5e, owner 2026-08-19 `rc-5c478001de8a`)**: an
  outsource **重新聚焦** used to be collected on a flat 120 s clock — `autoHandoverWorker`
  (`worker_spawn.go`) timed a worker's in-flight handover out at
  `refocus_since + StoppingTimeoutSecs` and **never read `refocus_op`** — while the notice
  that worker was sent said nothing about time. Both the in-flight arm and the worker DTO's
  `refocus_deadline` now read the SAME `refocus_op`-aware judgement the staff side reads —
  each at its own layer, not both through both: the in-flight arm asks `recycleGraceFor`
  directly, while the DTO asks `winddownDeadlineOf`, the two-axis expression that wraps it
  (`recycleGraceFor` on the 下線 axis, `refocusDeadlineOf` on the 換手 one). So 重新聚焦 runs
  no clock on either kind. (T-ed79 then widened that set:
  the only causes left on a clock, staff or worker, are `context_high` and the
  owner-pressed `accelerated_stop`.) The owner ruled the two kinds identical and rejected the asymmetry argument that had
  been offered for keeping the clock (「如果正職只有一個任務 那跟外包的代價不一樣嗎」): a
  staff member holding a single task pays exactly what a worker holding one pays.
  ⚠️ **That parity was half-kept until T-14 item 3**: the worker DTO read only the 換手 axis
  (`refocusDeadlineOf` on `refocus_since`), so an owner-pressed 加速停止 on a **下線** worker
  — which re-anchors `stopping_since`, not `refocus_since` — quoted `refocus_deadline = 0`
  while `runOutsourceTick`'s stop arm was collecting it at `stopping_since + grace`. Neither
  the cockpit nor the worker's own notice was told about a clock that was running. The DTO
  now calls `winddownDeadlineOf` — the member face's own two-axis expression — on the
  `memberFromWorker` projection its presence word already goes through.
  ⚠️ **What that ruling costs, stated rather than buried**: a worker that never answers its
  stopped report now waits indefinitely, holding its task with it. The exit is the owner's
  own hand — the same escalation the staff side has, and since T-ed79 the same three rungs:
  停止 → 加速停止 → 強制停止.
- 🔴 **Neither of those two paths re-dispatches.** Both go through the one-shot
  `dispatchRobustStopNow`, which enqueues once and does NOT write `last_command` /
  `last_command_at` — so the producer's de-dupe/re-dispatch discipline below never engages
  for them. **If that STOP frame is lost, nothing on the server re-sends it; the remaining
  escalation is the owner's hand.**
- De-dupe and re-dispatch (MUST NOT re-issue while `last_command==STOP` within
  `stop_retry`; once `stop_retry` elapses and the member is STILL online, MUST re-dispatch
  — at-least-once over the at-most-once band, re-firing being an idempotent warden-side
  no-op) belong to the **producer-dispatched** STOP, i.e. the timed arm below. **Under
  today's production constants that arm is not reached**, so this rule currently governs
  nothing on the offline path; it still governs `desired_state=uninstall` (§4.3) and stays
  contract for the timed arm.

🔴 **`stop_deadline` / `stop_grace` still exist — under the production config they are
UNREACHABLE, not deleted.** The timed wind-down they drive is still written in
`decideDown`; it is entered **only when `SoftOffboardGrace == 0`**, and
`SoftOffboardGraceSecs` is a compile-time constant of 600 s (deliberately not an
owner-facing setting), so production never takes that branch. Tests DO reach it by
injecting zero — the timers below are "all injectable", and there are sub-tests on both
arms — so a grep will find live code and live tests. Read this as **"the clock is off in
production"**, not as "the clock was removed", and not as "that constant is zero".

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
| `start_timeout` | `WakingTTLSecs` | START unconfirmed → failed spawn |
| `stop_grace` | 120 s | self-stop window before the robust stop — **unreachable today**: the arm that consumes it is guarded by `SoftOffboardGrace == 0` (see §4.3) |
| `stop_retry` | 90 s | STOP/UNINSTALL re-dispatch window (lost-frame recovery) |
| `recycle_grace` | 120 s (owner-settable) | dump-stuck fallback from `refocus_since` — but the wait is **`recycleGraceFor(refocus_op)`, which answers *whether there is a clock at all* as well as how long**, and since T-ed79 both it and the sentence (`offboardKindOf`) read ONE judgement, `winddownKindFor`. 加速停止 has TWO causes since 2026-08-21: `context_high` (the SECOND context threshold) and `accelerated_stop` (the owner pressing the button — the middle rung of 停止 → 加速停止 → 強制停止). Every other cause (`refocus`, `context_notice`, `relocate`, `runtime/model`, `restart_self`, `token_expiry`, and anything unnamed) is a plain **停止**: no clock at all, collected only by the agent's own stopped report or by 強制停止. FINAL is a positive condition, not a fallthrough — that is what stopped an owner verb the owner never put on a clock from carrying one. **The 120 is a DEFAULT, not a constant, since 2026-08-21**: `stop.accelerated_grace_secs` (10..3600) moves it, and it is deliberately ONE key for every clocked cause — the clock, the wire deadline and the sentence all reach it through the same `recycleGraceFor` pair, so an owner cannot end up with a countdown quoted to the agent that differs from the one the tick collects on. It says HOW LONG and never WHO: a soft cause stays uncollected at every value the key accepts |
| `soft_offboard_grace` | 600 s | how long a close-out may say NOTHING before its anchor is treated as residue — **not a deadline, and no longer measured from the anchor**. Neither soft arm escalates any more: 下線 never did (`rc-27d1710174dd`) and 重新聚焦 stopped on 2026-08-19 (`rc-c540367065ad`), so what this value still does is make `decideDown` run **no clock at all** (§4.3) and set the silence `clearStaleStoppingOnOnline` requires before it sweeps (§4.5) — which is what keeps the 強制下線 button on screen for as long as the close-out is still reporting. Compile-time constant (`SoftOffboardGraceSecs`), deliberately not owner-settable |
| `backoff_base` / `backoff_cap` | 5 s / 300 s | exponential start backoff |
| `circuit_threshold` / `circuit_cooldown` | 5 / 120 s | sticky breaker (verified hard failures only) |

### 4.5 Recycle (refocus / context-high auto-handover)

- **Context pressure opens a wind-down at BOTH thresholds, and they are different
  kinds (T-ed79).** `stampContextHighRecycle` stamps `refocus_op = context_notice`
  when the session crosses the FIRST threshold (`ctx.notice_pct`; codex: the notice
  round, 60% through it) — a plain **停止**, no clock, nothing in the sentence about
  time — and `refocus_op = context_high` at the SECOND (`ctx.handover_pct`; codex:
  the compaction threshold) — **加速停止**, on the `recycle_grace` clock. Before this
  the first threshold opened nothing at all: it emitted one SSE band (§6 of
  spec/sse.md, unchanged) and the wind-down began only at the second, so an agent
  that missed that one frame met the final call with no close-out started.
- **The ONE promotion is `context_notice` → `context_high`**, and it **MUST re-stamp
  `refocus_since`**: the deadline is `refocus_since + grace`, so promoting in place
  would quote a deadline already in the past and collect the member on the same tick
  that announced it. The promotion needs no frame of its own — the notice rides every
  write to the member row, and the promotion IS a write. Nothing else is ever
  promoted: an epoch opened by the owner (`refocus` / `relocate` / `runtime/model`)
  or by the agent (`restart_self`) stays a 停止 at any percentage, because turning the
  owner's explicit no-clock stop into a clocked one would take that decision away from
  him where he cannot see it. A member that has already reported stopped is not
  promoted either — it is collected on this tick.

- **Token expiry opens a 停止 an hour before the token dies (T-ed79, owner
  2026-08-21).** `stampTokenExpiryWinddown` runs in the same tick, straight after
  the context pass, and stamps `refocus_op = token_expiry` on any live agent session
  — staff **or outsource worker** — whose agent token is inside its last
  `tokenExpiryLeadSecs`. It is a plain 停止:
  the owner's model for a token renewal is the same as for a refocus or a model
  change — 「就是呼叫軟下線，然後等他 report_stopped 以後再呼叫上線」 — so the
  collection is the agent's own stopped report or 強制停止, exactly as for every
  other soft cause.
  🔴 **The expiry is DERIVED, not stored**: `session_boot_ts + auth.agent_token_ttl`
  (`tokenExpiryOf`). The token is minted at START dispatch and the anchor is
  stamped later, on the SSE first-connect edge, so the derivation is an **upper
  bound** — the trigger fires a little LATE, never before the token exists. It also
  reads the CURRENT TTL setting rather than the one the token was minted with, so
  changing `auth.agent_token_ttl` mid-session moves the estimate for sessions whose
  tokens did not move. Both error terms are deliberate: recording the real `exp`
  would need a durable per-session column, and this cause is soft, so being wrong
  costs a wasted handover rather than a cut-off one.
  🔴 **WHY IT HAS TO EXIST**: every step of the offboard sequence is an MCP call on
  the session's own bearer token, so an expired token does not degrade the close-out
  — it makes the close-out impossible.
  🔴 **The ONE exempt kind is `warden`, and the reason is its CREDENTIAL, not its
  role**: `mintWardenToken` mints with no `exp` claim at all, so there is no expiry
  to lead. `tokenExpiryOf` therefore excludes warden by name (T-170e). It used to
  allow-list `assistant`, which swept outsource in with warden even though a
  worker's token comes from the same `mintAgentToken` with the same
  `auth.agent_token_ttl` — the exemption was wider than the reason written beside it.
  Ordering matters: the context pass runs FIRST, because both passes skip a member
  that already carries `refocus_since` and `canPromoteToAcceleratedStop` only
  promotes a `context_notice` epoch — a token stamp landing first would therefore
  block the second context threshold's 加速停止 on that member entirely.

- A recycle never flips `desired_state` — it stays `online` throughout; the flow is:
  `refocus_since` stamped → member delta fans → the agent-side listener REFETCHES the
  member row and, on a confirmed NEW refocus epoch, surfaces the 〈停止〉 text the
  SERVER PUSHED in that delta (`offboard_notice`) as the handover wake to
  its interactive session, which persists its state and self-reports over MCP
  (`report_stopping` → `report_stopped`; the runtime never auto-reports on the
  session's behalf) → robust STOP once the agent reports stopped
  (the first stopped report of a refocus-marked, still-desired-online member fires
  the kill event-driven, not on the next tick) OR `recycleGraceFor(refocus_op)` elapses
  (the dead-session fallback — an unresponsive session that never reports is force-stopped
  by the server; the agent side needs no timeout of its own. **The wait is not one number**:
  `stop.accelerated_grace_secs` (default 120 s) for the TWO 加速停止 causes —
  `context_high` (the second context threshold) and `accelerated_stop` (the owner's
  press) — and **no fallback at all** for every other cause,
  which waits indefinitely for the stopped report or the owner's 加速停止 / 強制停止 — see
  the `recycle_grace` row in §4.4) → the SSE drop makes
  ¬online → the next tick's plain START respawns.
- **The wake text is the document, not client copy**: the SERVER composes the
  sentence, folds the 〈停止〉 document into it and PUSHES both in the member delta
  (`offboard_notice`), so an owner editing 〈停止〉 changes what the next collected
  session is told with no client release.
  🔴 This REPLACES the pull model this section used to specify, and with it the
  argument for pull ("a pushed payload would fail silently on a flaky link and be
  indistinguishable from an empty document") — the owner ruled 「改回真的推播」
  (T-a9d6). What that argument was pointing at survives as the client's fallback:
  a frame that arrives WITHOUT the notice cannot be repaired from the client side,
  so the agent still prints a one-line notice that it is being collected and names
  `get_offboard` as the way to fetch the checklist itself. One wake per refocus epoch,
  failed read included.
- **Auto-stamp**: before deciding, the tick MUST stamp `refocus_since` on any candidate
  whose actionable context pct (same stale/boot-storm guards as the SSE band —
  spec/sse.md §6) is in the HANDOVER band, only when the member is online and not already
  recycling. This replaces any SSE handover emit.
- **Loop-break**: the tick MUST clear `refocus_since`/`stopped_since`/`stopping_since` the
  moment it observes the respawn-pending state (`desired_state==online ∧ ¬online ∧
  refocus_since>0`) so a slow/never-waking respawn can never be re-killed off a stale marker.
- **Stale-stopping clear**: a desired-online member OBSERVED online while carrying
  `stopping_since > 0` MUST have the anchor cleared (survived-stop / reconnect path)
  — but ONLY once it has been SILENT for `soft_offboard_grace`, where silence means
  the later of `stopping_since` and the member's own last context report (the gauge's
  `ts`) is that far in the past. 🔴 The clock is the member's silence, NOT the anchor's
  age: a close-out is told to collect its sub-agents first, and that routinely runs
  longer than the window, so dating the sweep from the anchor erased the owner's only
  signal while the member was still working (T-2123 bought one window of grace; T-7723
  changed the clock). A member with NO gauge record has said nothing the server can
  date — the store is in-memory and a station re-exec blanks it fleet-wide — so that
  case falls back to the anchor's age rather than reading the amnesia as silence.
  ⚠️ The report is NOT a heartbeat: it is throttled to at most one burst per
  `soft_offboard_grace`-independent 30 s window, and it is driven by the agent's
  activity (Claude wires it to the statusLine redraw, codex to a tokenUsage-updated
  event). So this discriminator sees a member that is still producing activity and
  does NOT see one blocked inside a single long call — a close-out that spends the
  whole window waiting on one sub-agent is still swept. Strict improvement, not a
  complete fix.
  ⚠️ Do NOT read that as "no clock-driven signal exists". One does, for codex only:
  the session runs a 30 s identityHeartbeat whose report is stamped into a
  DIFFERENT store (telemetry, not the gauge) and keeps ticking through a long tool
  call. Reading it here is a behaviour change with its own trade-offs and is not
  part of this rule; the point is that the gap above has a known candidate.
  Two deliberate consequences, both owner-facing: a member that reports stopping
  and then resumes ordinary work reads `stopping` for the rest of that session,
  and while it reads `stopping` the cockpit offers 強制下線 rather than the
  ordinary graceful 下線. `report_stopped` and any reboot clear the anchor, and
  so does `activate` — but the non-destructive route to it is the chat's 就地喚醒
  row, NOT the detail panel's Spawn, which for a `stopping` member opens the
  settings dialog and never sends activate.
- 🔴 **These passes reach an OUTSOURCE WORKER through a projection, not through the
  reconcile roster (T-170e).** The reconcile tick's roster read is `ListMembers`,
  which is `kind != 'outsource'` by construction, so a worker is never offered to
  any of them; `runOutsourceTick` calls `runWorkerLifecyclePasses`
  (`lifecycle_roster.go`), which projects its ACTIVE, not-held-down workers with
  `memberFromWorker` and folds the four wind-down fields back onto the worker rows.
  Until T-170e only the context pass was projected, so a worker had no token-expiry
  lead (its session simply died mid-task with no close-out) and no survived-stop
  sweep (a `stopping_since` left by a stop it survived read 停止中 in the cockpit for
  the life of the session).
- 🔴 **WHICH passes run, and in what order, is now ONE list: `lifecycleRosterPasses`
  (`lifecycle_roster.go`), read by `runReconcileTick` and `runWorkerLifecyclePasses`
  alike (T-170e stage 3).** There is no second call list to keep in step. Each pass
  declares its own `AppliesTo`, so a formality added to the list reaches BOTH sides
  by construction and one that must not has to write the restriction down where a
  reader — and `lifecycle_roster_parity_t170e_test.go` — sees it by name. Exactly
  one pass is restricted today: `recycle_loop_break` is staff-only, because a worker
  already has a loop-break in `autoHandoverWorker` asking a different question
  (`boot_ts > refocus_since`). A wind-down rule that goes ON this list and is then
  quietly narrowed to one side fails by name in that test.
  - 🔴 **KNOWN GAP — `LIFECYCLE-LIST-IS-OPT-IN-T170E`.** An earlier draft of this
    bullet claimed "a wind-down rule that is not on this list does not apply to
    anybody, however member-shaped its code looks." That is a **claim, not a
    mechanism**, and it is corrected here rather than deleted because it was the
    stronger-sounding half. Measured: a new pre-decide roster loop written the old
    way — inline in `runReconcileTick`, staff roster only, never entered into
    `lifecycleRosterPasses` — is caught by **nothing**. The list guards narrowing a
    listed formality; it does not force a formality onto the list. Both historical
    failures (token-expiry lead, survived-stop sweep) had the second shape, not the
    first. Closing it needs an AST-level guard over both producers plus an explicit
    exclusion list — T-170e stage 5, deliberately out of stage 3's scope.
  - 🟡 **NARROWED BY T-170e stage 5, STILL OPEN — `lifecycle_identity_gate_t170e_test.go`.**
    `TestTickProducersHaveNoUndeclaredRosterLoop` walks `runReconcileTick` and
    `runOutsourceTick` with `go/parser` and requires every iteration in them to be
    accounted for by name AND by count in `lifecycleProducerLoopRulings`. Re-measured
    on the mutant above: it now fails, naming the producer and the loop, and it does
    so **without any kind expression to find** — which is the whole point, since
    neither historical failure had one. Its sibling
    `TestIdentityGatesAreEachOnTheRecord` is the other half: every `Kind` comparison,
    kind switch, kind-seam call and member-kind struct stamp in the package's
    production sources must carry a written reason.
    - **What is now caught:** the loop written INLINE IN A PRODUCER'S OWN BODY.
      That is the spelling the mutant above has, and the spelling both historical
      failures had.
    - 🔴 **What is still caught by nothing: the same loop one call frame down.**
      Measured 2026-08-27: lift that staff-only loop verbatim into a new method
      and call it from `runReconcileTick` — every test in the package stays green.
      Gate (1) sees no kind expression; gate (2) sees a CALL, not a loop, in the
      producer body. Putting the loop inside `runLifecycleRosterPasses`' own body
      is green for the same reason. Nothing in the tree guards this today, and no
      work is scheduled on it — it is written down so the next person measures
      rather than assumes.
    - Two further scope limits, stated in that file's header: the scan is
      **`server/ocserverd` only**, and neither gate can tell a true reason from a
      fluent false one. The second is not hypothetical — a shipped ledger reason
      was already found describing the wrong mechanism, twice.
- 🔴 **The ENTRY filter is one function too: `lifecyclePolicyFor(m).ShouldExist()`.**
  It is the only place the 正職/外包 difference may be spelled at the door — the
  owner's ruling that 「正職會不會有 instance 存活取決於 人物設定有沒有這個角色，外包則是取決於
  task 還是不是未完成狀態。其餘的部分應該要統一才對」. It replaced four hand-copies
  (`runReconcileTick`, `reconcileMemberNow`, `runOutsourceTick`'s projection filter,
  and the copy inside the test helper `workerTickPass`).
- 🔴 **The 停止 → 加速停止 → 強制停止 ladder binds the WORKER side too, and it binds
  it at every stamp site — there are FIVE, not one.** `armRefocusEpoch` is the one
  way an epoch is OPENED (the fifth row below only PROMOTES one that is already
  open, which is why it survives that word); before T-170e three worker sites
  hand-wrote the same four anchors instead, so none of them carried the ladder check
  and each was the same bug wearing a different button. Those three now stamp through
  `armRefocusEpoch` on a `memberFromWorker` projection, folding back only the four
  fields it mutates. **Enumerate this table before adding a stamp site — the T-170e
  bug WAS three sites nobody had enumerated:**
  | site | verb | on a ladder refusal |
  | --- | --- | --- |
  | `openOwnerOpHandover` (`worker_spawn.go`) | 改機器 / 換 model | the change is SAVED, the stage does not move; the existing wind-down keeps its own deadline and owns the move |
  | `HandleRefocusOutsourceWorker…` (`api_outsource.go`) | 重新聚焦 | **409** — the owner pressed a button, so he gets an answer (the staff twin `HandleRefocusMember` refuses on the same rule; the sentence differs in exactly one noun, `this worker` vs `this member`) |
  | `workerRestartSelf` (`worker_spawn.go`) | `restart_self` | **409** — the refusal is written by `HandleRestartSelfApiSelfRefocusPost` itself, VERBATIM the sentence its own staff arm writes further down in the same function (`m.Kind == KindOutsource` arm vs the fall-through `armRefocusEpoch` arm); the two arms are one rule |
  | `HandleAcceleratedStopOutsourceWorker…` | 加速停止 | n/a — it ADVANCES the ladder, and it deliberately does not zero the anchors (the twin of the staff 加速停止 arm) |
  | `stampContextHighRecycle` promotion arm (`reconcile.go`, the `if promoting` branch) | none — the reconcile tick's own context pass, projected onto workers by `runWorkerLifecyclePasses` (`lifecycle_roster.go`), which `runOutsourceTick` calls | n/a — it also ADVANCES, and only forwards: `canPromoteToAcceleratedStop` lets it move `context_notice` → `context_high` and nothing else. It hand-writes `refocus_since` / `refocus_op` INSTEAD of calling `armRefocusEpoch` on purpose — that helper zeroes the wind-down anchors, and here they belong to a close-out already in flight (see the `armRefocusEpoch is deliberately NOT used` note directly above that assignment) |
  ⚠️ **`重啟` (restart) is a deliberate hole in this table, not a missing row.**
  `ownerOpDisplacesTheSession(restart) == true` (`worker_spawn.go`), so the
  `!ownerOpDisplacesTheSession(op) && s.workerHasStateToFlush(w)` arm in
  `respawnWorkerForOwnerOp` (`worker_spawn.go`, the arm *after* the
  `DesiredStateOffline` held-down one) is never taken for 重啟 and the ladder never
  sees it. That is intended and predates T-170e — but **NOT because 重啟 can only arrive at a worker
  the owner has already stopped.** It can arrive at any live worker:
  `HandleRestartOutsourceWorkerApiOutsourceWorkersIdRestartPost`
  (`api_outsource.go`) has exactly two preconditions — the row exists and it is
  not `released` — and **no desired-offline gate at all**. Press 重啟 on a worker with
  `desired_state="online"` that is mid-加速停止 and it answers **200**, zeroes
  `refocus_since` / `refocus_op` / `stopping_since` / `stopped_since`, and the deadline
  goes with them.
  The reason that is right is that **重啟 is not a wind-down cause at all — it is a
  kill+respawn.** It does not ask the current session for a close-out; it displaces
  it (`respawnWorkerForOwnerOp` → `respawnWorkerForOwnerOpNow` → `respawnWorkerNow`,
  which kills the session on the resolved target before it re-spawns), and the handler says so on the row itself: its `if s.hub.IsOnline(id)`
  arm (`api_outsource.go`) stamps the `session_alive` receipt *"this worker was
  still running — 重啟 is replacing that session, not starting a first one. If it
  does not come back, its previous session was still holding the slot"*. The four
  anchors it clears all DATE THE SESSION BEING REPLACED; carrying them into the
  successor is what makes the next 改機器 / 換 model read them as "this epoch's
  wind-down is already collected". So clearing them is a correct clean sheet for a new
  session, **not a way around the ladder** — there is no ladder step left to be on once
  the session the ladder was counting for is gone. (`forced_stop_at` is deliberately
  KEPT, per the staff activate's rule.) Winding 重啟 down instead would fan an SOP 預告
  at a session that is about to be killed regardless and then wait out a deadline for
  an answer that changes nothing. A reader of the rows above would otherwise reasonably
  assume 重啟 is covered; it is not, and it should not be.

### 4.6 Dispatch discipline

- Dispatch is **fire-and-forget**: acceptance ≠ outcome; results return asynchronously via
  presence. Correlation is zero-field — no command id exists; the
  server re-derives everything from observed presence each tick. **`update` is the one verb
  this does not cover**: nothing re-derives "the owner asked this machine to upgrade", so it
  is re-enqueued on a failed write and mirrored durably across a restart (spec/sse.md §7,
  which is normative for that verb).
- **Target-reachability gate**: a command MUST NOT be enqueued for a target warden that is
  not online (no live SSE downstream to drain it) — the dispatch fails closed
  (`accepted=false`), state does not advance, and the tick re-decides when the warden
  connects (no phantom START / ghost STOP).
- Queue-key resolution: a command for member M is enqueued under the member id of the
  ACTIVE warden on M's `desired_machine_id` (the machine id IS the warden's own member id);
  a warden target addresses itself.
- Placement MUST filter for an online machine whose latest telemetry reports the selected
  runtime `installed == true` and `logged_in != false`. **There is NO automatic placement:**
  an explicit machine that is offline, inactive, or lacks that readiness makes the dispatch
  STALL, and no other machine is substituted. A member with no machine selected is likewise
  not placed anywhere. This follows the owner's 2026-07-25 ruling that removed automatic
  placement: a pin is an instruction, not a hint, and booting somewhere nobody chose is the
  mis-placement that ruling removed. Outsource workers add ONE softer tier — the machine the
  worker's last confirmed session ran on is a preference that may be fallen through — but
  every tier it falls through to is still an owner-authored source
  (`resolveStickyWorkerPlacement`).
- **Owner-visible refusal, and the asymmetry in it**: a WORKER stalls with a stamped reason
  in all three cases (`resolveWorkerPlacement` → `stampWorkerPlacementBlocked`). A MEMBER
  stamps its row (`stampMemberPlacementBlocked`) only when no machine is selected or the pin
  is not an active machine; a pin that is merely OFFLINE, or that lacks the runtime, stalls
  with a server log line and an unlanded-dispatch flag but writes NO row receipt today. A
  rewrite MUST NOT read this as "the reason is always on the row". Nor is the no-receipt set
  closed at those two: an unbuildable START payload (no persona/token) fails closed with a
  log line only.
  A placement stall arms no backoff of its own — it keeps the prior reconcile state, and the
  cadence re-decides every candidate every tick. It is NOT, however, a promise that the next
  tick after the obstruction clears will dispatch: a member that previously timed out a
  LANDED start is still subject to the §4.3 backoff / circuit-open gate — and to the
  zombie-confirm arm (`decideUp`; not specified in §4.3 today): once a landed START has
  bounced off the warden's clobber guard, the tick dispatches NOTHING while the member has
  been continuously offline for less than `2 × waking TTL`, and once that window lapses it
  answers with a STOP — not a START — to reap the squatting session. The window is anchored
  on continuous-offline time, so a reconnect inside it resets the wait; withholding is the
  point, because a presence-deaf zombie and a session mid-reconnect are indistinguishable at
  that instant.

## 5. Installer / binary surface (one line — OpenAPI covers it)

`GET /install.sh?token=<jwt>` templates the request base URL + token into a `text/plain`
bootstrap script (PUBLIC — the token authorizes the eventual install, not the fetch), and
the script pulls the warden binary from the PUBLIC `GET /api/warden/binary`. The `?code=`
variant probes that binary route (HEAD) BEFORE redeeming the one-time claim code — a
server that cannot serve the binary (503) exits with a plain-language error and the
single-use code survives for a retry. The binary routes (`/api/warden/binary`,
`/api/agent/binary`) and the MCP catalog serve the release-built assets embedded in the
server binary; CWD files never shadow those bytes. This keeps a repo-less single-file
deploy self-contained. Shapes and status codes are in `spec/openapi.json`; no hidden
behaviour beyond the string templating.

## Appendix A — in-memory state covered by this document

| # | item | section |
|---|---|---|
| 3 | context gauge | §3 |
| 5 | warden telemetry + banked-cost edge | §3 (edge mechanics: spec/sse.md §5.2) |
| 6 | warden-command FIFO loss/re-send semantics (in-memory except the durable `update` mirror) | §4.6, §3 (queue: spec/sse.md §7) |
| 7 | reconcile producer bookkeeping | §3, §4 |

## Appendix B — doc↔code discrepancies found at freeze (code wins; spec follows code)

> **Scope of "code wins": this appendix is a RECORD of how these specific
> discrepancies were adjudicated AT THE M1 FREEZE — it is not a standing rule that
> code beats docs whenever the two disagree.** Each item below was decided once,
> at freeze, and the decision is frozen with the spec; do not re-derive it. For a
> discrepancy found *now*, the standing rule applies instead: stop and ask the
> owner (`seeds/system_interaction.md` §4.1), because the reason code "won" here
> was a freeze-time judgement about a shipped wire, not evidence that code is the
> more trustworthy authority in general.

1. **`--no-reconcile` did not exist AT FREEZE.** The migration plan (Phase 4 §2) requires a
   reconcile kill-switch for shadow deployment; the implementation at freeze was
   unconditionally always-on. This spec froze the always-on
   behaviour; the kill-switch remained a REQUIRED feature of any shadow-mode deployment of a
   second implementation (a shadow without it would spawn real agents), but it is a
   deployment-mode flag, not part of the frozen production contract.
   **Since then it has been implemented** — `--no-reconcile` is a serve flag today
   (`server/ocserverd/main.go` `parseServeFlags`, mounted in `server.go`), and §4.1
   describes what it gates. Note the shadow-deployment requirement this entry states is
   NOT met by that one flag alone — see §4.1 for the two holes it leaves open (the restore
   path, and outsource workers). This entry stays as the
   record of the freeze-time adjudication — read the first paragraph as history and the
   **Since then** note as the pointer to where the current behaviour is specified (§4.1).
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
