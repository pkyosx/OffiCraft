# Caller-Identity & Intent-Per-Tool Convention

> Owner-defined 2026-07-10. Applies to ALL officraft API routes and MCP tools.
> Codifies the rules the presence/identity rebuild implements. New endpoints MUST follow both.

## Convention 1 — Caller identity comes from auth, never a param

**RULE: the CALLER's identity ("who am I") is ALWAYS derived from the verified auth
token (`current_actor` = the JWT `sub`). It is NEVER passed as a request/tool
parameter.**

- A `member_id` / `agent_id` / `owner_id` parameter is ONLY ever a **target**
  ("who to operate ON") — never "who am I".
- **Self-operations** take NO identity parameter — the server reads the caller from
  the token. Example self tools: `report_waking()`, `report_stopping()`,
  `report_stopped()`, `ingest_agent_context(...)`, `ingest_telemetry(...)`.
- **Operating on ANOTHER member** takes a `member_id` target parameter AND requires
  the **admin capability** (see RBAC below). Example control tools:
  `activate_member`, `deactivate_member`, `force_stop_member`, `refocus_member`,
  `dismiss_member`.

**Why.** Every gated request already verifies the token `sub`. Making the caller
also hand-carry its own identity is (a) redundant, (b) friction — an agent should
never need to know or look up its own `member_id` at boot (this is what made Mira
"can't find my member_id" a real onboarding blocker), and (c) a semantic error —
identity must be defined by the verified token, not overridable by a parameter.

**RBAC — the admin capability.** Controlling *another* member requires the caller
to be an **admin**: owner-scope OR the caller's role is `assistant` (the
orchestrator role, e.g. mira). This is the `admin_agent` principal class of the
single resolver (`server/ocserverd/authz.go`); the control routes declare it on the route
table (`RouteSpec.requires="admin_agent"`) so future admin roles change one
place, not every handler. A non-admin agent may only act on itself (via the self
tools); it cannot target another member.

- ✗ Anti-pattern: `set_member_presence(member_id=<self>, sessions=[...])`
- ✓ Pattern: `report_waking()` (caller from token, no self id)

## Convention 2 — One intent, one MCP tool (intent-per-tool separation)

**RULE: each distinct intent is its OWN tool, named for the intent, carrying
exactly the parameters that intent needs — NOT a single fat tool with a
mode/phase discriminator parameter.**

- ✓ Pattern: three tools `report_waking()` / `report_stopping()` /
  `report_stopped()`.
- ✗ Anti-pattern: one tool `set_my_presence(phase="waking"|"stopping"|"stopped")`
  multiplexing several intents through a discriminator.

**Why.**
1. Self-documenting — the tool name IS the intent; an agent sees exactly which tool
   to call.
2. Precise parameters — different intents need different params (some need none);
   one tool per intent keeps each tool's parameter set exact instead of a grab-bag
   of mutually-exclusive optionals.
3. Clean MCP surface — the reflected tool schema shows distinct, unambiguous
   actions.

## Presence, restated under both conventions

Presence is now two layers, cleanly separated:

- **Intent** — what the agent actively reports about its own lifecycle:
  `report_waking()` / `report_stopping()` / `report_stopped()` (self tools, no id,
  no session payload).
- **Connection fact** — `online` / machine / liveness are pure SSE-connection
  projections (`SSEHub`); the server derives them, the agent never self-reports
  them. The self-reported `sessions` wire (session_id / pid / last_alive) was
  removed: pid drove no decision, the single-session guard moved to the hub's
  connection count, and waking-completion moved to an `on_first_connect` hub hook.
