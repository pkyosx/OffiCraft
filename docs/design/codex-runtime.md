# Codex runtime — provider adapter design

> Status: implemented and validated end-to-end on isolated `seth-m1`. Claude remains
> the backward-compatible default.

## Goal and compatibility boundary

OffiCraft selects an AI CLI runtime per permanent member and per outsource worker:
`claude | codex`. Existing rows, clients, task manuals, and warden `start` frames that omit
the field fold to `claude`. The existing Claude launch path and legacy Claude telemetry
fields remain intact.

The server stays provider-neutral. It persists the selection, folds the existing
`persona_context`, chooses a runtime-capable machine, and sends one common `start` frame.
The warden owns the provider adapter:

```text
member / outsource runtime
          |
server: persona + JWT + lifecycle
          |
warden start {runtime, model, effort, ...}
          |
     +----+----+
     |         |
  Claude    Codex App Server
  adapter   adapter + ocagent listener
```

## Codex adapter

The Codex correctness path is a warden-managed `codex app-server` plus the existing
`ocagent listen` lifecycle listener. A TUI may attach remotely for observation or manual
interaction, but TUI presence is not a liveness dependency. The App Server and listener
survive TUI disconnects and preserve the same OffiCraft member identity.

The adapter reuses the Claude persona mechanism:

1. write the server-folded context to the member's private `persona.md`;
2. configure the same OffiCraft MCP endpoint and member JWT;
3. pass only a minimal App Server developer instruction that directs Codex to read and
   obey that persona file.

No generated `AGENTS.md`, duplicate prompt fold, or per-member `CODEX_HOME` is introduced.
Codex uses the machine user's existing login/config, just as Claude uses the shared machine
user's existing login.

The adapter uses App Server's stdio JSON-RPC transport. App Server is still an evolving
upstream surface, so protocol/version handling stays isolated in the sidecar and a failed
initialize/thread start terminates the session visibly for normal OffiCraft reconciliation.

## Global context: one policy, runtime-selected boot tails

OffiCraft keeps one canonical Global Context. Governance, identity, MCP, chat, reply cards,
tasks, lessons/manuals, lifecycle semantics, and `ocagent` commands are provider-neutral
and must not be copied into separate Claude and Codex personas. Only the small, read-only
Boot Sequence is runtime-specific, because listener ownership and the readiness boundary
genuinely differ:

```text
shared Global Context
  + role / lessons / owner additions
  + actor boot semantics (member or outsource)
  + runtime boot sequence (Claude or Codex)
```

This is composition across two independent axes, not four persona copies:

- **Actor semantics** remain the existing member vs outsource distinction:
  members report waking and recover their resume snapshot; workers claim their one task.
- **Runtime mechanics** describe only who owns the listener, context reporting, and
  interactive-question behavior.

The Claude member boot sequence preserves current behavior byte-for-behavior: after boot
readiness, the agent starts bare `ocagent listen` with its Monitor tool; Claude `statusLine`
feeds context telemetry; `AskUserQuestion` stays disabled.

The Codex member boot sequence changes only execution ownership:

1. The sidecar starts a boot-only App Server turn. The member performs
   `report_waking` + resume recovery (T-4595: a worker walks the SAME sequence — its
   `report_waking` is also the assigned → active claim), then completes
   that turn.
2. `turn/completed` is the readiness boundary. Only then does the sidecar launch the same
   bare `ocagent listen` child and consume its stdout; the model must not launch a second
   listener.
3. The sidecar converts listener events into the established idle `turn/start` / active
   `turn/steer` policy. Thus SSE presence still means ready/online and false-online during
   boot remains impossible.
4. The moment the listener reports its stream is up, the sidecar opens ONE turn it authors
   itself, telling the agent to carry on with the post-SSE steps of its boot document.
   Without it the boot ends in a dead stop: this runtime's agent must not mount its own
   listener, so it hands control back after step 1 — and it only ever runs when a listener
   line becomes a turn, while the `connected` line is exactly the one the forwarding filter
   drops. The wake fires ONCE per session and deliberately not on reconnects, which are
   network blips whose only effect would be to interrupt work already under way. Its text
   NAMES the step instead of restating it: the boot document belongs to the owner and
   moves, and a copy of its wording here would be a second source of truth.
5. App Server `thread/tokenUsage/updated` supplies total usage plus
   `modelContextWindow`; the sidecar computes and posts the same context percentage that
   Claude's `statusLine` reports. Existing warning/handover thresholds remain server-owned
   and unchanged.
6. `request_user_input` is disabled and bridged to reply cards as described below.

### Context compaction and refocus

Claude's percentage-based handover remains unchanged. Codex App Server keeps a durable
thread and can compact that thread without ending the session, so a transient context
percentage is not its useful handover signal. The sidecar counts completed
`contextCompaction` items and includes the current count in context telemetry. At three
compactions in one live session, the existing graceful refocus flow runs; a real session
boundary clears the count before the replacement thread reports again. Monitoring renders
Codex as `context N% · ↻ compact x/3`; Claude remains percentage-only.

The cockpit's Global Context editor remains a single shared owner-additions block. The
read-only Claude and Codex Boot Sequence variants are shown together in the preview. A
worker is handed exactly one of those two variants — the one for the runtime it is
actually running — and that seed's own execution-environment section is where the
listener owner, the interactive-question guard and the context-telemetry mechanics come
from. Thus no recipient is asked to ignore another provider's instructions.

T-4595 removed the hand-written outsource-only tail that used to carry those same three
bullets after the seed. It was a second copy of one instruction for an audience staff does
not have, and two copies can only drift; the earlier regression in this area was a codex
worker being handed the Claude seed, which is the same failure one level up.

## Wake and steering policy

OffiCraft events are typed wake signals, not prompt text supplied by an external actor.
The adapter renders its own fixed instruction from validated event metadata and makes the
agent re-read authoritative state through MCP.

- Idle thread: a durable pending wake starts a new turn.
- Active thread: steer the active turn, matching Claude's Monitor delivery semantics.
- Listener connected: transport chatter is never forwarded to the model, but the FIRST such
  line in a session also opens the sidecar-authored post-boot wake turn described above.
  This is the one turn whose trigger is a line the forwarding policy drops.
- Listener or App Server exit: terminate the sidecar. The listener child is killed/reaped,
  SSE presence drops honestly, and existing OffiCraft reconciliation restarts the selected
  runtime. Durable server state and the existing `ocagent` cursor remain authoritative.

This matches the existing Claude mechanism at the architectural level: one private persona,
one lifecycle listener, one provider launch adapter, and MCP as the authority.

### User-input requests become OffiCraft ask cards

Codex command approvals and Codex user-input requests are separate mechanisms.
`approval_policy = "never"` removes approval prompts, but the App Server protocol may still
emit `item/tool/requestUserInput`. OffiCraft must bridge that request into the existing
reply-card mechanism instead of waiting for an unattended TUI:

1. Prefer prevention: start Codex in default collaboration mode with
   `features.default_mode_request_user_input=false`, and instruct it to use OffiCraft
   `create_reply_card` whenever owner input is required. This is the Codex equivalent of
   Claude's `--disallowedTools AskUserQuestion`; the explicit feature override pins the
   current default against version/config drift.
2. If App Server nevertheless emits `item/tool/requestUserInput`, the sidecar validates and
   bounds the question payload, then opens one OffiCraft reply card per question as that
   member. Options map directly up to the existing four-option limit; the first card uses
   normal automatic task/step binding and any additional simultaneous cards use
   `bind="none"`. This preserves the existing one-question-per-card convention.
3. Card creation uses the existing automatic current-task/current-step binding. Therefore
   the task enters `waiting_owner` exactly as it does for Claude; no Codex-only card table,
   status, or UI is introduced.
4. The sidecar immediately completes the App Server request with a fixed deferred marker
   and instructs the active Codex turn to yield. It MUST NOT leave the JSON-RPC request
   pending, because that request belongs to one App Server client connection and would be
   fragile across reconnects.
5. The answer or expiry arrives through the existing directed `reply_card` SSE
   delta. The durable wake starts the next idle Codex turn, which reads the authoritative
   card(s) with `get_reply_card` and continues once the required answers are settled, or
   replans after expiry.
6. If card creation fails, return an explicit non-waiting failure answer to Codex; never
   wait for TUI input. A sidecar restart creates a fresh App Server thread, while cards
   already committed remain authoritative OffiCraft state.

For a request marked `isSecret`, OffiCraft creates an `action` card asking the owner to
complete the sensitive step through an appropriate machine-local or provider flow. The
card must not solicit or persist the secret value itself.

MCP `mcpServer/elicitation/request` remains disabled/declined unless it is deliberately
given a separate product mapping. It must never silently become a TUI dependency.

## Capability, installation, and placement

Install resolves and stamps both `OC_CLAUDE_BIN` and `OC_CODEX_BIN` when available. An
installation is usable when at least one provider resolves; selecting a missing or
logged-out provider fails that spawn clearly without breaking the other provider.

Warden telemetry adds a provider-neutral `runtimes` map:

```json
{
  "claude": {"installed": true, "logged_in": true, "version": "…"},
  "codex": {"installed": true, "logged_in": true, "version": "…"}
}
```

The map contains readiness only—never tokens, credential values, or credential paths.
Legacy Claude probe fields stay for existing clients. Codex placement always requires an
explicit `installed == true` and rejects an explicit `logged_in == false`. During a rolling
upgrade only, a completely absent capability map preserves legacy Claude placement; after
any map is reported, Claude follows the same explicit readiness rule. Null login state
remains eligible. Placement is an explicit decision (owner ruling 2026-07-25): a placement
that is offline or lacks the selected runtime is NOT substituted by another host, and
there is no automatic placement to fall back on — a machine nobody named is no placement
at all. Either way no `start` is dispatched; the stall is named on the row the cockpit
reads (`last_op_reason` — `no_machine_selected`, and `machine_unavailable` for an
outsource worker whose named machine cannot take it), and reconcile retries after
telemetry or placement changes.

## Launch policy

The selected adapter receives the shared launch knobs:

- `model`: provider-specific free string; blank uses that provider's default.
- `effort`: exact shared vocabulary `low | medium | high | max`; omitted uses `medium`.
- Codex sandbox: `danger-full-access`.
- Codex approvals: `never`.

For a blank Codex model, the sidecar tells the boot turn to omit `report_waking.model`.
The model must not guess its own identifier and persist that guess, because the persisted
value becomes an explicit override on the next wake and would replace the machine's Codex
default. An explicit OffiCraft model is reported back verbatim.

These settings intentionally favor capability for this trusted-machine deployment. They do
not expand OffiCraft authorization: member identity, MCP scope, task governance, and
server-side validation remain unchanged.

## Account attribution is runtime-paired

An account key is a *runtime-specific* identity: Codex obtains a ChatGPT account, Claude
obtains its own OAuth account, and the two live in different identity spaces. Telemetry,
however, is ONE shared per-actor entry that every reporter partial-merges into. Without a
rule, a member that changed runtime (or whose machine hosts both) was displayed with
whichever account key happened to be in that entry — the member panel showed a `claude`
member holding the machine's `codex` account.

The rule, implemented in `server/ocserverd/account_display.go`:

- the account key, its provenance stamp (`account_runtime`, internal to the in-memory
  entry — deliberately **not** on any DTO, so no wire surface changes) and the reporter's
  `account_label` move as **one atomic unit**. `applyAccountReport` is their only writer;
- a report whose runtime disagrees with the stored stamp **retires** the stored pairing —
  that key belonged to the runtime the actor just left, and the member row's runtime may
  lag a live switch;
- an account reported **without** a runtime is unprovable: it is neither stored nor
  allowed to leave the previous pairing standing. "Missing runtime" must never degrade
  into "some older runtime". Every shipped reporter already sends `runtime` alongside
  `account` (`cli/ocagent/contextreport.go`, `cli/ocwarden/codex_session.go`), so this
  costs nothing in practice and self-heals on the next report;
- the read side (`telemetryAccount`, used by the monitoring session/machine folds and by
  `foldActorRuntime` for the outsource-worker DTO) admits the key only when the stamp
  matches the actor's runtime. It never falls back to the entry's ordinary `runtime`
  field, which every later heartbeat rewrites.

Withholding is scoped, not global: the owner's **accounts overview** still lists every
reported key (it is global observability and the surface where the owner aliases a key);
only the per-actor session/machine attribution cells go honestly empty. Display naming is
unchanged and orthogonal — the owner's hand-set alias stays visible to every caller rank,
while the reporter-supplied `account_label` stays owner-only PII (T-260e).

### The Codex account key identifies a person, not a workspace (v2)

`codexAccountKey` (`cli/ocwarden/codex_session.go`) hashes the id_token claim
`https://api.openai.com/auth`.`chatgpt_user_id` under the versioned prefix
`officraft-codex-account-v2:`.

v1 hashed `tokens.account_id` from `~/.codex/auth.json`, which is the ChatGPT
**workspace/organization** id — byte-identical to the id_token's `chatgpt_account_id`
claim (verified on a live machine). Its comment claimed only the half it got right
("equal ChatGPT accounts on separate machines map to the same monitoring account") and
was silent on the half it broke: two *different* people in one workspace also mapped to
one key. Measured consequence: two machines logged in as different humans produced the
identical key `codex:89064106…`, so their spend summed into one row and their separate
5h/7d windows overwrote each other — the "latest report wins" rule for usage windows
assumes one key means one quota, and for Codex that assumption did not hold.

The version prefix moved to `v2` because the *input semantics* changed; v1 and v2 keys
must not merge into one row. Rejected identifiers and the reasoning (email/name are
mutable PII, `sub` is IdP-connection scoped, `sid`/`jti` are per-token) live in the
function comment, which is the authority.

**What the version bump actually does on upgrade.** The accounts fold groups by the key
an actor is reporting *right now*, and `banked_cost` is a durable per-actor column that
the fold adds under that current key. So the v1 row does **not** freeze: nobody reports
it any more, so it disappears from `/api/monitoring`, and each actor's banked history
re-attaches to its machine's v2 personal key — money that used to sit in the shared
workspace row is re-credited to whoever is logged in on that machine now. What genuinely
is stranded is the owner's hand-set alias: `account_alias` rows are keyed on the v1
string and become orphans, so the owner re-aliases the new key once; until then the
accounts row shows a bare `codex:…` digest. During a *mixed* fleet one person appears as
two rows (old wardens send v1, new ones send v2) and converges only when the last warden
is upgraded — upgrade the fleet in one pass.

**Known trade-off: fail-empty is silent on the wire.** `applyAccountReport` treats
"account empty + runtime present" as a no-op — it neither stores nor clears the pairing —
so a machine that stops being able to read the claim keeps being served under its last
successfully reported key until the server restarts, and no telemetry field separates
"no Codex account here" from "could not read one". Accepted deliberately: the obvious
remedy (fall back to the workspace id) is the defect being removed. If it ever needs
solving, solve it server-side with an explicit unknown-account signal.

Degradation is deliberately fail-empty: no `auth.json`, unparsable JSON, a malformed or
undecodable id_token, or a missing/blank claim all yield `""` ("this machine has no
identifiable Codex account"). It never falls back to the workspace id, because that
would silently restore the v1 collision on exactly the machines whose token could not be
read, with nothing in the telemetry to show which ones. Sentinels for both directions
(different people → different keys; one person on two machines → one key) and for every
degradation branch are in `cli/ocwarden/codex_session_test.go`; they run against
`codexAccountKeyForHome(t.TempDir())` fixtures and never read the real home directory,
which holds live credentials.

## Attribution

OffiCraft-authored Codex commits use the human git author only and do not add a Codex
co-author trailer. Existing Claude attribution behavior is unchanged.
