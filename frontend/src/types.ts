export type MemberStatus = "offline" | "waking" | "online";

/**
 * The REAL derived lifecycle presence the backend emits on `MemberDTO.presence`
 * (server/ocserverd/domain.go PresenceState): the tri-state `MemberStatus` PLUS the two
 * graceful-shutdown projections `stopping` (shutdown in progress, session still
 * winding down) / `stopped` (shutdown done, session gone). This is the honest
 * five-state union — NOT a fabricated activity sub-axis. There is NO
 * awake/sleeping signal and NO `error` presence anywhere in the backend today,
 * so the detail panel's visual union is one-per-state and simply maps
 * `online → online-awake` (see MemberDetailPanel `visual`).
 */
export type MemberLifecycle =
  | "offline"
  | "waking"
  | "online"
  | "stopping"
  | "stopped";

export type Effort = "low" | "medium" | "high";

// Role keys are OPEN since M2-2: the seed "assistant" plus any owner-created
// custom role key (server-minted `r-<hex>`). Labels resolve i18n-first (seed
// keys) then fall back to the member's server-resolved `roleName`.
export type RoleKey = string;

export interface Member {
  id: string;
  memberId: string;
  name: string;
  role: RoleKey;
  /** The role's display TITLE resolved server-side (wire `role_name`): the seed
   * title for a seed role, the custom role's own name for a custom role. UI
   * label rule: i18n label for a known seed key, else this. Honest "" when the
   * member has no/unknown role. OPTIONAL so hand-built test fixtures (and any
   * legacy view object) stay valid — consumers fall back to the raw key. */
  roleName?: string;
  status: MemberStatus;
  /**
   * The REAL five-state lifecycle presence (offline/waking/online/stopping/
   * stopped) mapped straight from the wire `presence`. `status` above stays the
   * frozen tri-state contract the presence dot reads; `lifecycle`
   * additionally carries the graceful-shutdown states the detail panel's
   * lifecycle dot + action button group need. Never fabricated.
   */
  lifecycle: MemberLifecycle;
  model: string;
  effort: Effort;
  // The member's kind (e.g. "assistant" | "warden"). The office roster shows
  // ONLY real AI assistants; machine-layer kinds (warden) are filtered out.
  kind: string;
  // The machine this member is bound to run on (wire `desired_machine_id`). Direct
  // passthrough — used to resolve the warden member behind a monitoring machine row
  // (kind==="warden" + desiredMachineId===machine) so a machine teardown can target the
  // right member_id (the monitoring machine DTO carries no member_id). Honest empty ""
  // when the wire leaves it blank; NEVER fabricated.
  desiredMachineId: string;
  /** The owner's lifecycle intent (wire `desired_state`:
   * "online" | "offline" | "uninstall"). A warden member carrying "uninstall"
   * is a machine mid-uninstall — the machines panel renders the in-progress
   * transitional state from it. OPTIONAL so hand-built test fixtures stay
   * valid (same precedent as `roleName`); the mapper always sets it. */
  desiredState?: string;

  /**
   * Runtime telemetry (honest placeholders).
   * `null` means "no real source yet" → the UI renders "—", never a fake number.
   */
  machine: string | null;
  account: string | null;
  contextPct: number | null;
  estimatedCost: number | null;
  bankedCost: number | null;

  /** tmux session name (`member-<id>`) for `$ tmux -L officraft attach -t <session>`. */
  tmuxSession: string;

  /**
   * Epoch seconds of the last refocus intent (`refocus_since`), or `null` when
   * the member has never been refocused (wire `0` → `null`). Honest: never a
   * fabricated time — the detail panel hides the "last refocus" line when null.
   */
  refocusSince: number | null;

  /**
   * Fleet remote-ops stage 1 — the "most recent operation" receipt the warden
   * reported for this member (folded onto the durable member server-side). Honest:
   * `lastOp` is "" until an op reports; `lastOpOk` is `null` until then (distinct
   * from a recorded `false` = failed); `lastOpAt` is `null` when never (wire `0`).
   * The detail panel shows a "最近操作" block from these — a green ✓ + time on ok,
   * a red ✗ + collapsible log on failure. Never fabricated.
   */
  lastOp: string;
  lastOpOk: boolean | null;
  lastOpLog: string;
  /**
   * Structured one-line cause of the last op when it failed (the warden's
   * `<code>: <detail>` refusal summary, e.g. `session_already_exists: …`,
   * folded server-side onto `last_op_reason`). `""` when the receipt carried
   * none (older records) — the panel then shows status-only, as before.
   * OPTIONAL on the view (test-fixture precedent: `Member.roleName`).
   */
  lastOpReason?: string;
  lastOpAt: number | null;

  /**
   * M2-1 roster unread badge (the red dot upgraded to a COUNT): how many chat
   * messages this member has sent the CALLER (the owner, in this UI) newer
   * than the caller's read watermark for that conversation — the pure inverse
   * of the chat_read receipt, computed server-side (wire `unread_count`; the
   * old boolean `unread` is gone). Only messages ADDRESSED TO the caller count
   * (agent↔agent coordination never counts) and it is INDEPENDENT of presence
   * (an offline member can be unread). Honest passthrough — the FE never
   * computes it.
   */
  unreadCount: number;
}

/**
 * `/api/bootstrap` preview: the assembled agent boot persona (role definition ⊕
 * global context ⊕ lessons). Excludes the member JWT BY DESIGN — a UI preview
 * mints no token and must never carry an agent credential (see WireBootstrap).
 */
export interface BootstrapView {
  role: string;
  name: string;
  taskType: string;
  context: string;
}

/**
 * The folded PER-ROLE lessons doc for one `roleKey` + `task_type` (the single
 * fixed task_type key is "general"). Scoped to a role (per-role-learnings step1):
 * agents sharing a role share it, but a researcher's learnings no longer pollute
 * an assistant's. Kept minimal (like `BootstrapView` drops token): the UI needs
 * only the text + `isDefault`, so `owner_id` / `schema_version` are dropped BY
 * DESIGN. `isDefault` true → the text IS the file seed (dal/seeds/lessons.md).
 */
export interface LessonsView {
  roleKey: string;
  taskType: string;
  text: string;
  isDefault: boolean;
}

// ── Machine lifecycle view models (onboard / teardown) ────────────────────────

/**
 * Result of onboarding a machine (`onboardMachine`). `bootCommand` is the
 * operator string the owner copies and runs on the target machine to bring the
 * warden online; it EMBEDS `token`. SECURITY: neither `token` nor `bootCommand`
 * is ever logged — only rendered into a copy control. `expiresIn` is seconds
 * until the token expires. Every field is a verbatim passthrough (never faked).
 */
export interface OnboardResultView {
  memberId: string;
  /** The stable, opaque machine id the server minted (member_id == machine_id).
   * Replaces the old free-typed `host` — a machine is now created by display name
   * only; the server owns the id. Verbatim passthrough (never fabricated). */
  machineId: string;
  token: string;
  expiresIn: number;
  bootCommand: string;
}

/**
 * Result of installing THIS machine's warden on the server host in one click
 * (`bootstrapOnServer` → `POST /api/machines/{machineId}/bootstrap-here`).
 * `ok` is whether the install succeeded; `exitCode` is the installer's exit
 * status; `log` carries the installer output — and on `ok === false` it is the
 * failure reason (e.g. the one-warden guard message). The UI NEVER swallows
 * `log`: on failure it surfaces the text so the owner sees why. Every field is a
 * verbatim passthrough (never fabricated).
 */
export interface BootstrapResultView {
  ok: boolean;
  exitCode: number;
  log: string;
}

/**
 * Result of tearing a machine's warden down ON THE SERVER HOST in one click
 * (`teardownOnServer` → `POST /api/machines/{machineId}/teardown-here`). The
 * symmetric inverse of `BootstrapResultView`. `ok` is the success flag; `exitCode`
 * the teardown status; `log` the output (on `ok === false` the failure reason — the
 * UI NEVER swallows it). `removed` reports whether the warden member was soft-deleted
 * (true iff `ok` — CONFIRM-THEN-REMOVE: the row drops ONLY when the daemon is
 * confirmed torn down). Every field is a verbatim passthrough (never fabricated).
 */
export interface TeardownHereResultView {
  ok: boolean;
  exitCode: number;
  log: string;
  removed: boolean;
}

/**
 * One machine in the registry (`listMachines` → `GET /api/machines`). A machine
 * has a stable, opaque `machineId` (the warden member id; the activate/rebind
 * and teardown target) and a renamable `displayName`; `online` is whether its
 * warden is currently reachable. Address a machine by `machineId`; only ever
 * DISPLAY `displayName`. Honest passthrough — `online` is never fabricated.
 */
export interface MachineView {
  machineId: string;
  displayName: string;
  online: boolean;
  /**
   * True ONLY for the well-known server-self machine (the warden for the host
   * running the officraft server itself). It is always rendered FIRST, has NO
   * delete action, and its Install is an in-place bootstrap-on-server (no dialog).
   * Every onboarded (remote) machine is `false`.
   */
  isSelf: boolean;
  /**
   * Server-computed binary-freshness verdict for this machine's warden+agent
   * binaries: "current" (heartbeat fingerprints match the server's embedded
   * latest), "stale" (any differs), or null = unknown (no heartbeat
   * fingerprints yet — e.g. an older warden build — or nothing embedded to
   * compare). Passthrough — the FE never computes or fabricates it.
   */
  binStatus: BinStatus;
  /**
   * The local claude CLI version this machine's warden heartbeat probed
   * (`--version` first token, e.g. "2.1.211"); null = unknown (claude
   * unresolved, probe failed, or an older warden that never probes) — the
   * machine table's claude column shows "—".
   */
  claudeVersion: string | null;
  /**
   * Where the machine's claude CLI credentials live (server-synthesized from
   * the warden's presence probes): "file" | "keychain" | "both" | "none";
   * null = unknown. Wire passthrough kept for parity; not displayed.
   */
  claudeCredSource: ClaudeCredSource;
  /**
   * Whether `claudeAiOauth.subscriptionType` is readable from the credentials
   * file; null = unknown. Wire passthrough kept for parity; not displayed
   * (informational only since T-f694 — the account key no longer reads it).
   */
  claudeSubReadable: boolean | null;
}

/** The machine binary-freshness verdict vocabulary (`bin_status`). */
export type BinStatus = "current" | "stale" | null;

/** The machine claude credential-source vocabulary (`claude_cred_source`). */
export type ClaudeCredSource = "file" | "keychain" | "both" | "none" | null;

/**
 * Result of DELETING a machine (`deleteMachine` → `DELETE /api/machines/{id}`).
 * DELETE is a PURE soft-delete of the roster record (delete ≠ uninstall ≠ stop):
 * it removes the machine from the roster and dispatches NO warden command — it
 * does NOT tear the warden daemon off the box (that is `uninstallMachine`). There
 * is NO command string here (the old `teardown_command` placeholder is gone).
 * `removed` reports the soft-delete outcome (true). Verbatim passthrough.
 */
export interface DeleteResultView {
  memberId: string;
  machineId: string;
  removed: boolean;
}

/**
 * Result of UNINSTALLING a machine (`uninstallMachine` →
 * `POST /api/machines/{id}/uninstall`). UNINSTALL is the MACHINE-lifecycle verb:
 * it writes the owner intent `desired_state="uninstall"` so the server reconcile arm
 * drives the single `uninstall` RPC down to the warden (which runs
 * `ocwarden uninstall` on its box). The record is KEPT (re-installable) — the row
 * does NOT drop (contrast DELETE). `dispatched` reports whether an uninstall RPC
 * will be driven: TRUE when the warden is online; FALSE when it is already
 * offline (treated as already uninstalled — nothing to command). Verbatim
 * passthrough (never fabricated).
 */
export interface UninstallResultView {
  memberId: string;
  machineId: string;
  dispatched: boolean;
}

/**
 * Result of `POST /api/machines/{member_id}/upgrade` (one-click upgrade):
 * `dispatched` reports whether the `update` command was actually enqueued
 * onto the warden's live SSE downstream (false = offline, nothing commanded).
 * Passthrough (never fabricated).
 */
export interface UpgradeResultView {
  memberId: string;
  machineId: string;
  dispatched: boolean;
}

// ── Monitoring view models (camelCase; mapped from the Wire* mon shapes) ──────
// Same honesty rule as `Member`: `null` means "no real source yet" → the UI
// renders "—", never a fabricated number.

/** One live AI session row (Monitor §3 "AI 會話"). */
export interface MonSessionView {
  id: string;
  name: string;
  role: RoleKey;
  model: string;
  /** REAL live effort self-reported from the statusLine telemetry; "" (→ "—")
   * until reported — NOT the roster's owner-intent `member.effort`. */
  effort: string;
  machine: string;
  account: string;
  /** presence tri-state mapped 1:1 onto the member status. */
  status: MemberStatus;
  contextPct: number | null;
  cost: number | null;
  bankedCost: number | null;
}

/** One host machine row (Monitor §2 "機器資訊"). */
export interface MonMachineView {
  /** Stable id (host string) — React key / dedupe / PATCH target. */
  machine: string;
  /** Owner-editable display label (BE fallback = id, always non-empty). */
  displayName: string;
  agents: number;
  accounts: string[];
  cpuPct: number | null;
  ramPct: number | null;
  batteryPct: number | null;
  acPower: boolean | null;
  /** Same verdict as `MachineView.binStatus` (registry row), null = unknown. */
  binStatus: BinStatus;
  /** Same probe columns as the registry row (`MachineView.claude*`). */
  claudeVersion: string | null;
  claudeCredSource: ClaudeCredSource;
  claudeSubReadable: boolean | null;
}

/** One account usage card (Monitor §1 "帳號資訊"). Empty in M1; shape is ready
 * so the warden slice can render real accounts with no UI change. */
export interface MonAccountView {
  /** Stable id (account tag string) — React key / dedupe / PATCH target. */
  account: string;
  /** Owner-editable display label (BE fallback = id, always non-empty). */
  displayName: string;
  /** Reporter-supplied raw label "email(org)" (T-260e). OWNER-ONLY on the
   * wire (absent for non-owner callers) and honest-null when never reported —
   * the detail modal derives email/org from it, never from displayName. */
  accountLabel: string | null;
  machine: string;
  cost: number | null;
  fiveHour: { usagePct: number | null; timePct: number | null } | null;
  sevenDay: {
    usagePct: number | null;
    timePct: number | null;
    overheated: boolean;
  } | null;
}

/** Monitoring telemetry envelope (three sections). */
export interface MonitoringView {
  sessions: MonSessionView[];
  machines: MonMachineView[];
  accounts: MonAccountView[];
}

// ── Settings view models (camelCase; mapped from the Wire* settings shapes) ───

/**
 * Build identity (Settings › 軟體更新). `version` is the single human-facing
 * version identity: an OFFICIAL package (bin/release) carries its GitHub
 * Release tag; a self-build keeps the honest "0.0.0" → only then does the UI
 * fall back to the composed build label v<yymmdd>-<hhmm>-<shortsha> from
 * `gitSha` + `gitTime` (lib/versionFormat; missing `gitTime` degrades to the
 * short sha alone). `updateAvailable`/`latestVersion` mirror the server's
 * cached GitHub Releases check; a phantom newer version is NEVER fabricated.
 */
export interface VersionView {
  version: string;
  gitSha: string;
  gitTime: string | null;
  catalogHash: string;
  updateAvailable: boolean;
  latestVersion: string | null;
}

/**
 * Verdict of the explicit 檢查更新 click (GET /api/release/check): the server
 * asks GitHub Releases synchronously and answers `status` "up_to_date" |
 * "update_available" (latestTag + releaseUrl then point at the newer release)
 * | "unknown" (GitHub unreachable — the honest degraded verdict).
 */
export interface ReleaseCheckView {
  status: "up_to_date" | "update_available" | "unknown";
  currentVersion: string;
  latestTag: string | null;
  releaseUrl: string | null;
}

/**
 * The folded global-context doc (Settings › 角色誌 › 全域情境). `isDefault` true
 * → the text IS the file seed (label "預設"); false → owner-edited.
 */
export interface GlobalContextView {
  text: string;
  ownerId: string;
  schemaVersion: number;
  isDefault: boolean;
}

/**
 * The folded role-definition doc (Settings › 角色誌 › 角色定義). `name` is the
 * role title and `definitionMd` the persona body (both from the real seed —
 * never the mockup's illustrative Chinese desc). `isDefault` true → seed ("預設").
 */
export interface RoleDefView {
  key: string;
  name: string;
  definitionMd: string;
  ownerId: string;
  schemaVersion: number;
  isDefault: boolean;
  /** TRUE for an out-of-box seed role (assistant — resettable, NOT deletable);
   * FALSE for an owner-created custom role (deletable; the server re-enforces —
   * this flag only drives the UI affordance). */
  isSeed: boolean;
}
