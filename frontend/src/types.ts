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

export type Effort = "low" | "medium" | "high" | "max";
export type AgentRuntime = "claude" | "codex";

// Role keys are OPEN since M2-2: the seed "assistant" plus any owner-created
// custom role key (server-minted `r-<hex>`). Labels resolve i18n-first (seed
// keys) then fall back to the member's server-resolved `roleName`.
export type RoleKey = string;

export interface Member {
  id: string;
  /** Personal image URL bound to this stable member id. Empty/absent keeps the
   * role-theme avatar and built-in glyph fallback chain. */
  avatarUrl?: string;
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
  /** The owner-CONFIGURED launch runtime — the settings value, not state. */
  runtime?: AgentRuntime;
  /** Runtime last SELF-REPORTED by the live session (wire `actual_runtime`).
   * "" = nothing has ever reported one. NEVER substitute `runtime` above: it
   * is the owner's intent, and serving it here made a runtime change look
   * applied the instant it was saved (T-7f28). */
  actualRuntime?: AgentRuntime | "";
  /** Model last reported by a live boot; absent on older API payloads. */
  actualModel?: string;
  /** Effort last SELF-REPORTED by the live session. There is no member wire
   * field for it — `lib/runtime.joinSessionRuntime` folds it from the
   * monitoring session, so it is absent/"" whenever nothing reported. NEVER
   * substitute `effort` below: that is the owner's launch intent, not state. */
  actualEffort?: string;
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
  compactionCount?: number | null;
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

  /** Which operation opened the in-flight wind-down (wire `refocus_op`):
   * "relocate" | "runtime/model" | "context_notice" | "context_high" |
   * "refocus" | "restart_self" | "token_expiry" | "accelerated_stop"; "" when
   * none. The last three arrived with T-ed79 and the server sends all of them;
   * they were missing from this list, which is the whole value of writing the
   * set down. */
  refocusOp?: string;
  /** Epoch by which that wind-down is collected at the latest (wire
   * `refocus_deadline`), null when none is in flight. A CEILING, not a
   * prediction — the collect fires as soon as the agent reports stopped.
   *
   * 🔴 null ALSO means "in flight, but on no clock at all", which since T-ed79
   * is every cause except the two 加速停止 arms, `context_high` and
   * `accelerated_stop`. `refocusOp` is what tells the two apart; never read null
   * as "nothing is happening". */
  refocusDeadline?: number | null;
  /** The DURABLE last-observed machine (wire `actual_machine`). `machine`
   * above blanks the moment the member stops running; this survives, so a
   * pending relocation stays legible while it is offline. */
  actualMachine?: string;

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
 * What an activate ACTUALLY did (T-7fa1) — the response the cockpit used to
 * throw away.
 *
 * The activate endpoint always answers 200: the wake INTENT is persisted before
 * anything is dispatched, so it cannot fail. But a 200 says nothing about
 * whether a START reached a warden, and the server has told us the difference
 * since T-ba62 (`activation_pending`, wire-optional). Dropping it meant "the
 * START went out" and "nothing was dispatched and nothing will be until the
 * next cadence tick" arrived at the UI as the SAME value — so the optimistic
 * 「喚醒中…」 bridge had no way to know it was lying, and sat there forever.
 *
 * `activationPending === true` means: intent stored, reconcile will retry, but
 * NOTHING has been dispatched — the member is not waking. False/absent means a
 * START actually landed on a warden (or the member was already online).
 */
export interface MemberActivateResult {
  activationPending: boolean;
}

/**
 * The relocate twin of {@link MemberActivateResult} (wire `relocation_pending`,
 * server-side since T-8655 and equally unconsumed until T-7fa1): the owner-pinned
 * move is recorded, but the recycle STOP/START that would land it could not be
 * delivered — "move scheduled, not yet landed".
 */
export interface MemberRelocateResult {
  relocationPending: boolean;
  /**
   * WHY the move has not landed (wire `relocation_deferred`, T-927a). True means
   * the server deliberately held it back: the member is live with uncollected
   * state, so a graceful wind-down window was opened and the move lands with the
   * agent's wrap-up. `relocationPending` is true for that case AND for a move no
   * warden would accept, so a "nothing was dispatched" alert must be suppressed
   * while this is true — the alert is for the failure, not for the design.
   */
  relocationDeferred?: boolean;
}

/**
 * `/api/bootstrap` preview: the assembled agent boot persona (role definition ⊕
 * global context ⊕ lessons). Excludes the member JWT BY DESIGN — a UI preview
 * mints no token and must never carry an agent credential (see WireBootstrap).
 */
export interface BootstrapView {
  role: string;
  name: string;
  context: string;
}

/**
 * The folded PER-ROLE lessons doc for one `roleKey` — the WHOLE address since
 * T-2 removed the `task_type` axis. Agents sharing a role share it, but a
 * researcher's learnings no longer pollute an assistant's. Kept minimal (like `BootstrapView` drops token): the UI needs
 * only the text + `isDefault`, so `owner_id` / `schema_version` are dropped BY
 * DESIGN. `isDefault` true → the text IS the file seed (dal/seeds/lessons.md).
 */
export interface LessonsView {
  roleKey: string;
  text: string;
  isDefault: boolean;
  /** Size of `text` in CHARACTERS (Unicode code points) — cap_chars' unit. */
  sizeChars: number;
  /** The `doc.cap_chars.learning` setting now in force, in the same unit.
   *
   * T-ae38: these two were on the wire since T-3aeb and the mapper threw them
   * away, so the Learning card was the only journal block that showed no usage
   * — an agent found out it was full by being refused, which happens in the
   * last minutes before a handover, taking the round's learnings with it. */
  capChars: number;
}

/**
 * The folded PER-ROLE insight doc for one `roleKey` (T-3809) — the role
 * journal's third block, beside Duty (the role definition) and Learning (the
 * lessons doc).
 *
 * ⚠️ UNLIKE `LessonsView` this DOES carry `sizeChars` / `capChars`, and that is
 * load-bearing rather than tidy. `capChars` is the live `doc.cap_chars.insight`
 * setting (its OWN one since T-ae38 — it no longer shares a number with Learning),
 * and the settings surface that otherwise shows it is admin-only — the insight
 * card's header is the one place an owner sees the number a write will be judged
 * against without being refused first. Dropping these two fields the way
 * `LessonsView` drops `owner_id` would quietly delete that.
 *
 * `isDefault` means "this role has never written its own insight". 🔴 Since
 * T-e1e3 that no longer implies an empty `text`: insight folds against a
 * PER-ROLE file seed (`seeds/insight_<roleKey>.md`, today only `assistant`), so
 * an untouched role either reads its FACTORY wording (seed) or "" (no seed) —
 * `isDefault` is true in both. The card must read this field, not the emptiness
 * of `text`, or it renders shipped wording as if a person had written it; and a
 * genuinely empty doc must still render as an honest empty, never a failed load.
 */
export interface InsightView {
  roleKey: string;
  text: string;
  isDefault: boolean;
  /** Size of `text` in CHARACTERS (Unicode code points) — cap_chars' unit. */
  sizeChars: number;
  /** The `doc.cap_chars.insight` setting now in force, in the same unit. */
  capChars: number;
  /**
   * True when a FACTORY version of this role's insight exists to fall back to
   * (`seeds/insight_<role_key>.md` ships). Gate the 初始版本 reset row on THIS,
   * never on `isDefault`: that one says whether the role has written yet, and a
   * seeded role that HAS written reads hasSeed=true / isDefault=false — exactly
   * when the reset is worth offering. `resetInsight` 404s when it is false.
   */
  hasSeed: boolean;
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
   * Which launchd shape this machine's warden reports it is actually running
   * under. Passthrough of the wire `warden_shape` — see `WardenShape` for why
   * the null case is its own fact and not a synonym for "unknown".
   */
  wardenShape: WardenShape;
  /**
   * Whether that cutover is actually IN EFFECT for the processes that carry
   * agents on this machine. Passthrough of the wire `cutover_effect` — see
   * `CutoverEffect` for why "unproven" is its own state and never a green one.
   */
  cutoverEffect: CutoverEffect;
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
  runtimeCapabilities?: Partial<
    Record<
      AgentRuntime,
      {
        installed: boolean | null;
        loggedIn: boolean | null;
        version: string | null;
      }
    >
  >;
}

/** The machine binary-freshness verdict vocabulary (`bin_status`). */
export type BinStatus = "current" | "stale" | null;

/**
 * The launchd-shape vocabulary a warden REPORTS about itself (`warden_shape`).
 * Four states, and the fourth is the absence of the other three:
 *   "anchor"  — converted to the new shape
 *   "legacy"  — still on the old shape (never converted, or converted and
 *               rolled back)
 *   "unknown" — the reporting build ran but could not read its own parent
 *   null      — this warden does not report a shape AT ALL: it has not received
 *               the anchor-cutover release yet
 * `unknown` and `null` are DIFFERENT FACTS and must never be folded together —
 * one says "the new build is on the box and confused", the other says "the new
 * build is not on the box". The server deliberately never infers one from the
 * other (unlike `bin_status`, this is reported, not computed), so neither may
 * the FE.
 */
export type WardenShape = "anchor" | "legacy" | "unknown" | null;

/**
 * Whether the anchor cutover has actually TAKEN EFFECT for the agent-carrying
 * processes on a machine (`cutover_effect`). Four states again, and the third
 * one is the reason this type exists:
 *   "effective"     — proven: the carriers were created under the new identity
 *   "not_effective" — proven otherwise: a carrier predates that identity
 *   "unproven"      — could not be shown either way
 *   null            — this warden does not report the verdict AT ALL
 *
 * 🔴 "unproven" is NOT a shade of "effective". A machine whose cutover had not
 * taken effect showed a green badge for three hours because the only signal
 * available was two-valued; folding the third state back into the good one
 * re-creates that exact defect. Absent/unrecognised narrows to null, never to
 * one of the three verdicts.
 */
export type CutoverEffect =
  | "effective"
  | "not_effective"
  | "unproven"
  | null;

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
  /** REPORTED runtime; "" until something reports one (T-7f28). */
  runtime: "claude" | "codex" | "";
  /** presence tri-state mapped 1:1 onto the member status. */
  status: MemberStatus;
  contextPct: number | null;
  compactionCount: number | null;
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
  /** Same reported shape as `MachineView.wardenShape` (registry row). */
  wardenShape: WardenShape;
  /** Same reported verdict as `MachineView.cutoverEffect` (registry row). */
  cutoverEffect: CutoverEffect;
  /** Same probe columns as the registry row (`MachineView.claude*`). */
  claudeVersion: string | null;
  runtimeCapabilities?: MachineView["runtimeCapabilities"];
  /**
   * Epoch seconds when `runtimeCapabilities` was probed; null = never reported.
   */
  runtimeCapabilitiesTs: number | null;
  /**
   * Whether `runtimeCapabilities` is older than the server's freshness window;
   * null = never reported. The verdict is computed SERVER-side (one home for
   * the threshold) — the UI only decides how to SHOW an old answer, and the
   * answer is: show it, marked. Telemetry is never cleared on disconnect, so a
   * machine that probed once keeps this map forever; rendering it plain would
   * make a second field confidently wrong the way the hardware numbers were.
   */
  runtimeCapabilitiesStale: boolean | null;
  /**
   * Epoch seconds when the served hardware sample was measured; null = none.
   * A non-null stamp with null cpu/ram/battery means the sample EXPIRED (the
   * server withholds stale numbers) — a different fact from "never measured",
   * and the only reason an operator can tell the two apart.
   */
  hardwareTs: number | null;
  /**
   * The server's verdict on that stamp: true = the sample expired, which is WHY
   * cpu/ram/battery/acPower are null on this row; false = the nulls are honest
   * absences in a live sample; null = nothing was ever measured. The UI must
   * read THIS rather than compare `hardwareTs` against its own clock — the 90s
   * window has one home (server-side), and "all four values are null" is not a
   * substitute (a fresh report whose probes all failed looks identical).
   */
  hardwareStale: boolean | null;
  /**
   * The declared hardware keys that arrived with the WRONG VALUE TYPE in the
   * served sample (server-computed, honest-empty). This is the third reason a
   * hardware cell can be blank, and the only one that used to be invisible:
   * `cpu_pct: "47"` is accepted, stored, and read back as null, producing a row
   * byte-for-byte identical to a machine that has never had a CPU probe. Read
   * it PER KEY — one broken probe says nothing about its siblings — and render
   * it as its own mark, distinct from the stale mark: "measured but unreadable"
   * (someone's reporter is broken) and "measured a while ago" (nobody has
   * looked lately) send the operator to different places.
   */
  hardwareInvalid: string[];
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
  /** `measuredAt` is the epoch second `usagePct` was last REPORTED (honest-null
   * when nothing stamped it). It is not decoration: `usagePct` freezes the
   * moment the last agent on this account goes away, while `timePct` keeps
   * advancing with the wall clock, so without the age the two read as if they
   * were taken together (T-3b90 — the owner saw a days-old 43% presented as
   * current). The card renders the age whenever it has one, at every moment,
   * so the answer to "is this number old?" never depends on catching the page
   * at the right time. */
  fiveHour: {
    usagePct: number | null;
    timePct: number | null;
    measuredAt: number | null;
  } | null;
  sevenDay: {
    usagePct: number | null;
    timePct: number | null;
    measuredAt: number | null;
    /** BE verdict only — the card never derives this from the two percentages
     * itself. A snapshot nobody has refreshed lately arrives with no verdict
     * at all (pace null → false), because comparing a frozen number against a
     * moving clock describes the clock, not the account. */
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
 * Build identity (Settings › 系統更新與備份). `version` is the single human-facing
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
 * The folded global-context doc (Settings › 全域情境 › 使用者自訂). `isDefault` true
 * → the text IS the file seed (label "預設"); false → owner-edited.
 */
export interface GlobalContextView {
  text: string;
  ownerId: string;
  schemaVersion: number;
  isDefault: boolean;
}

/**
 * Which editable long-form document a retained revision belongs to. The
 * companion `key` is "global" for global_context, the role key for
 * role_definition, the role key for lessons too (T-2 dropped the
 * "<role_key>::<task_type>" composite), the type_key for
 * every task_manual kind, and the TASK id for task_description / task_title.
 *
 * `task_description` (T-e271) is the odd one out in what it keys on: every
 * other kind names a document that belongs to a TYPE or a role, this one names
 * a single task's own text. It rides the same series machinery all the same —
 * the ruling was to reuse the shipped version history, not to grow a second
 * one — so listing and restoring it are the same two routes.
 *
 * `task_manual` is the RETIRED four-field bundle: T-1f39 split a manual's SOP
 * and learnings into their own series (purpose and the identifier fields are no
 * longer versioned at all), migration 00044 deleted every existing row, and
 * BOTH document-history routes now answer 400 for it, naming the two
 * replacements. The name stays in this union only so the cockpit can still
 * spell what the server refuses — nothing may list, restore or write it.
 */
export type DocumentKind =
  | "global_context"
  | "role_definition"
  | "lessons"
  // T-3809: the role journal's third block. Its key is the BARE role_key —
  // which since T-2 is also what lessons uses.
  | "insight"
  | "task_manual"
  | "task_manual_sop"
  | "task_manual_learnings"
  | "task_description"
  // T-2ebe: the description's twin, keyed on the task id in the same way. A
  // SEPARATE series over that shared key — restoring a title never disturbs the
  // description's own three retained revisions, and the other way round.
  | "task_title"
  // T-791e: the two boot-context blocks that used to be read-only seed
  // previews. `system_interaction` is keyed "global" (one document for the
  // whole studio); `boot_sequence` is keyed by RUNTIME ("claude" / "codex")
  // and the two keys are DIFFERENT DOCUMENTS, not two views of one — their
  // third step means opposite things, so nothing may copy one over the other.
  | "system_interaction"
  | "boot_sequence"
  // T-c9c0: the 〈停止〉 document. A SINGLETON keyed "global" like
  // system_interaction — being collected is the same procedure whatever
  // runtime an agent runs, so there is deliberately no runtime axis here.
  | "offboard"
  // T-3201: the six lifecycle procedures that used to be Go string literals.
  // All singletons keyed "global".
  // ⚠️ T-6f44 (owner's decision 2): the last two are NO LONGER read-only. The
  // reason they were locked was recorded as 「以前 global context 是固定內容 我們
  // 也是會顯示 只是不給改」 — precedent, not a property of the text. Read-only is
  // still a property of the DOCUMENT that arrives on its own read rather than a
  // list the cockpit keeps; today no shipped document sets it.
  | "accelerated_stop"
  | "task_closeout"
  | "task_reassign_predecessor"
  | "task_takeover_with_predecessor"
  | "task_takeover_fresh"
  | "task_unblocked";

/** The DocumentKinds that carry a seeded boot-context / lifecycle document
 * (T-791e, widened by T-3201). Narrower than DocumentKind on purpose: the
 * adapter's boot-doc methods take THIS, so no caller can address `lessons`
 * through them.
 *
 * 🔴 THIS UNION IS ONE HALF OF A PAIR, AND THE WIRE HOLDS THE OTHER HALF. It
 * must stay identical to the frozen spec's `BootDocKind` enum: `toBootDoc`
 * assigns the generated wire type straight into this one, so a value added to
 * the enum and not added here does not compile (T-3201, owner's ruling 「加上
 * enum 並且前端自己寫死，我接受新增 enum 的人要去改前端的 code 找到他對應顯示的
 * 位置」).
 *
 * Adding it here is then not enough either: the settings list's `BOOT_DOC_ROWS`
 * (components/SettingsPage.tsx) is a `Record<BootDocKind, …>`, so the new kind
 * has no place to be shown until somebody gives it one, and that is a compile
 * error too. Two links, one chain — a document that ships and that the cockpit
 * never shows is the failure it exists to make loud. */
export type BootDocKind =
  | "system_interaction"
  | "boot_sequence"
  | "offboard"
  | "accelerated_stop"
  | "task_closeout"
  | "task_reassign_predecessor"
  | "task_takeover_with_predecessor"
  | "task_takeover_fresh"
  | "task_unblocked";

/**
 * One seeded boot-context block as the cockpit reads it (T-791e) — the folded
 * GET of `system_interaction/global`, `boot_sequence/claude` or
 * `boot_sequence/codex`. Three INDEPENDENT streams: nothing is shared between
 * them, and in particular the two boot_sequence keys are separate documents.
 *
 * `isDefault` true = the shipped seed is what agents boot with (nobody has
 * written this block). `hasSeed` says a factory version exists to restore to;
 * it is what makes the 還原出廠版 affordance honest rather than a button that
 * 404s.
 */
export interface BootDocView {
  kind: BootDocKind;
  key: string;
  /** The WHOLE stored document, marker line and all. What the version-history
   * modal diffs against; NEVER what a save sends. */
  text: string;
  /** The half the server fills in and refuses to take back (T-3201). `""` on a
   * document that carries none. Shown, never edited — the owner's standing rule
   * for these documents is 「以前 global context 是固定內容 我們也是會顯示 只是不
   * 給改」 — and since the write face has no field for it, showing it is the only
   * thing the cockpit can do with it. */
  readOnlyHead: string;
  /** The EDITABLE half, and byte for byte what `saveBootDoc` takes back. The
   * editor holds this, not `text`: the cockpit never composes the two halves,
   * never learns the marker, and cannot express an edit to the head. */
  body: string;
  /** Size of `text` — the WHOLE document — in CHARACTERS (Unicode code points),
   * capChars' unit. It counts the head the owner cannot edit, because the cap
   * is enforced on the document that gets stored. */
  sizeChars: number;
  /** The cap the SERVER enforces for this kind, in the same unit. The cockpit
   * blocks over-cap saves against this number rather than a local constant, so
   * a raised cap does not need a frontend release to take effect. */
  capChars: number;
  isDefault: boolean;
  hasSeed: boolean;
  /** True when the server SHOWS this document but refuses every write to it
   * (405). Read off the document itself (T-3201) — the cockpit never carries
   * its own list of which documents are read-only, because that list would go
   * stale silently the day one changes. */
  readOnly: boolean;
}

/**
 * ONE ROW of the version list — the DIRECTORY shape (T-1170). Identity, who,
 * when, and HOW BIG each field was; never the text.
 *
 * 🔴 It is a separate type from `DocumentHistoryView` on purpose, and the
 * separation is the guard: the list can no longer hand a caller a `content` it
 * does not have, so "read the text off the list row" is a compile error rather
 * than a blank pane. The text arrives from `getDocumentRevision`, named one
 * revision at a time.
 */
export interface DocumentHistoryEntryView {
  id: number;
  createdTs: number;
  /** Who wrote the version that replaced this one (owner id / member id). */
  actorId: string;
  /** The overlay was a TOMBSTONE — "follow the shipped default". A flag, never
   * a content field: the surfaces render it as the 預設內容 badge and the
   * reader substitutes the seed for it. */
  tombstoned: boolean;
  /** Per WIRE FIELD size in CHARACTERS (Unicode code points — `runeLength`'s
   * unit, the one the server's cap is measured in). This is what lets the list
   * mark an un-restorable revision and tell an empty revision from a full one
   * WITHOUT the text. A field the revision does not carry is absent, which is
   * not the same as 0 — `docCapBlockedFields` reads absence as "no such
   * field", exactly as it read a missing key out of `content` before. */
  sizes: Record<string, number>;
}

/**
 * The BODY of ONE named retained revision — what `getDocumentRevision` answers
 * (T-1170).
 *
 * 🔴 It carries `content` and NOTHING ELSE about the revision, because that is
 * all the server's named-revision route carries. Who wrote it, when, whether it
 * was a tombstone and how long its fields were all live on the DIRECTORY row
 * (`DocumentHistoryEntryView`) the reader opened to get here — so putting them
 * on this type too would mean inventing values the wire never sent.
 */
export interface DocumentRevisionView {
  id: number;
  content: Record<string, string>;
}

/**
 * ONE retained revision of an editable long-form document, IN FULL. `content`
 * is the field→value snapshot of the doc as it stood BEFORE the write that
 * retained it — the field names belong to the kind, so the view keeps them
 * verbatim rather than inventing a per-kind view model. At most 3 are kept per
 * doc.
 *
 * Since T-1170 this is the RESTORE RECEIPT's shape. The list does not carry it
 * (see `DocumentHistoryEntryView`) and neither does the named-revision read
 * (see `DocumentRevisionView`).
 */
export interface DocumentHistoryView {
  id: number;
  content: Record<string, string>;
  createdTs: number;
  /** Who wrote the version that replaced this one (owner id / member id). */
  actorId: string;
}

/**
 * The document's SHIPPED DEFAULT — the 初始版本 row of the version list
 * (`GET /api/document-history/{kind}/{key}/seed`, T-40f0).
 *
 * `content` uses the SAME field names a retained revision does, which is the
 * whole point: the reader and the diff that serve every other row serve this
 * one unchanged, so 初始版本 can be COMPARED before anyone decides to go back
 * to it. Reading it writes nothing.
 *
 * Only the two documents that own a reset have one (the global block's default
 * is the empty document, a seed role's is its file seed); everywhere else the
 * route 404s, exactly where the 初始版本 row is not rendered either.
 */
export interface DocumentSeedView {
  kind: DocumentKind;
  key: string;
  content: Record<string, string>;
}

/**
 * ONE ROW of the role roster (`GET /api/roles`) — everything a role has EXCEPT
 * its persona body. `name` is the role title; `isDefault` true → seed ("預設").
 *
 * 🔴 T-1170 split this off `RoleDefView`. The roster answer no longer carries
 * `definition_md` — only its size and the cap in force — so the list type must
 * not promise one. `useRoles` used to hand the SAME array to the roster and to
 * the role page, which is why the page needed no fetch of its own; that shared
 * array is now a directory, and the page reads its document through
 * `useRole` (`GET /api/roles/{key}`).
 */
export interface RoleSummaryView {
  /** Size of `definitionMd` in CHARACTERS (Unicode code points) — capChars'
   * unit. On a directory row this is the ONLY thing that says how much text is
   * there. */
  sizeChars: number;
  /** The `doc.cap_chars.duty` setting now in force, in the same unit (T-ae38).
   *
   * Duty had no cap AND neither of these fields until T-ae38: an agent that had
   * just condensed its own role definition had no way to tell how much room was
   * left, and had to ask someone else to measure the doc. There is usually no
   * such someone. `0` = a server too old to report them. */
  capChars: number;
  key: string;
  name: string;
  ownerId: string;
  schemaVersion: number;
  isDefault: boolean;
  /** TRUE for an out-of-box seed role (assistant — resettable, NOT deletable);
   * FALSE for an owner-created custom role (deletable; the server re-enforces —
   * this flag only drives the UI affordance). */
  isSeed: boolean;
}

/**
 * The folded role-definition doc IN FULL (Settings › 角色誌 › 角色定義) —
 * a roster row plus the persona body it describes (from the real seed, never
 * the mockup's illustrative Chinese desc).
 *
 * Answered by `GET /api/roles/{key}` and by every role WRITE (the response IS
 * the folded doc). Never by the roster list.
 */
export interface RoleDefView extends RoleSummaryView {
  definitionMd: string;
}

/** The PUBLIC pre-auth probe (`GET /api/auth/status`), both bits.
 *
 * `mfaRequired` is deliberately readable without a token: the login wall has to
 * know how many fields to render before anyone is authenticated. The only
 * alternative — letting /api/login answer differently for "password right, code
 * missing" — would disclose strictly more, because it confirms a correct
 * password. */
export interface AuthStatusView {
  passwordSet: boolean;
  mfaRequired: boolean;
}

/** The one-time output of `POST /api/auth/mfa/enroll`.
 *
 * This is the ONLY moment a TOTP secret crosses the wire — an authenticator
 * cannot be enrolled without it. The server never echoes an ACTIVE secret back,
 * so this view has no "read my current secret" counterpart, and it must not be
 * persisted anywhere by the cockpit. */
/** The owner's second-factor state (`GET /api/auth/mfa`, owner-gated).
 *
 * `offered` is the ship-dark rollout flag — whether this server lets the factor
 * be SET UP. It is NOT a second opinion on whether one is armed (`enrolled`),
 * and it never affects whether login demands a code: withdrawing the feature
 * leaves an armed factor fully enforced, or the flag would be a way for a stolen
 * session to walk past it. */
export interface MfaStateView {
  offered: boolean;
  enrolled: boolean;
}

export interface MfaEnrollView {
  /** Base32 secret, for manual entry into an authenticator app. */
  secret: string;
  /** `otpauth://totp/…` URI — the QR / deep-link form of the same secret. */
  otpauthUri: string;
}

/**
 * Whether the SCHEDULED database backup is still producing retreat points
 * (`GET /api/backup-health`, T-da06). Read by the 備份健康 block under
 * 設定 › 系統更新與備份 (T-5e71 moved it off the topbar and the monitor page).
 *
 * `status` is a CLOSED three-value set and `unknown` is NOT a soft healthy:
 * it means "the watchdog has not evaluated / its state could not be read", and
 * the whole point of the ticket is that a missing retreat point must never
 * look like a present one. Anything the mapper does not recognise lands on
 * `unknown` — never on `healthy`.
 *
 * `code` names WHICH failure this is ("" while healthy). The USER-FACING
 * sentence is derived from `code` via i18n; `detail` is the server's English
 * diagnostic string and is only ever shown as SECONDARY text.
 */
/** ONE signing key as the cockpit may know it (T-62).
 *
 * 🔴 There is no field for key material, and there is not meant to be: the wire
 * carries none (SigningKeyDTO), because on an install predating the key ring
 * the signing key IS a SHA-256 of the owner password, so even a fingerprint of
 * it would be an offline dictionary attack on that password. */
export interface SigningKeyView {
  keyId: string;
  /** When this key was made (epoch seconds), or null for the key an install has
   * been using since before the ring existed — its creation time was never
   * recorded. The wire says that with a 0, which the mapper narrows to null so
   * no component can render it as 1970. */
  createdTs: number | null;
  /** Exactly one key in a ring carries this: the one that SIGNS new tokens.
   * Every other key still verifies until someone removes it. */
  isSigning: boolean;
}

export interface BackupHealthView {
  status: BackupHealthStatus;
  code: BackupHealthCode;
  /** Server-authored English diagnostic. Secondary text only — never the
   * primary user-facing sentence (that comes from `code` via i18n). */
  detail: string;
  /** Newest SCHEDULED backup (epoch seconds); null = none has ever landed. */
  newestBackupTs: number | null;
  /** Age of that newest scheduled backup in seconds; null when there is none. */
  newestBackupAgeSecs: number | null;
  /** The server's derived freshness window in seconds (never recomputed here). */
  staleAfterSecs: number;
  /** When the CURRENT incident started (epoch seconds); null while healthy. */
  sinceTs: number | null;
  /** When the watchdog last evaluated; null = never (that IS the unknown case). */
  checkedTs: number | null;
}

/** The closed backup-health verdict. `unknown` is "we cannot tell", never a
 * quieter `healthy`. */
export type BackupHealthStatus = "healthy" | "unhealthy" | "unknown";

/** Which backup failure this is; "" while healthy (or while unknown — an
 * unevaluated watchdog names no failure). */
export type BackupHealthCode = "" | "never_ran" | "stale" | "failed";

// ── T-33 傳承 (lore) view models ───────────────────────────────────────────
//
// 🔴 WHAT IS NOT HERE IS THE POINT. There is no `score`, no `retrievedCount`,
// no `approved` flag and no `pendingCount` on any of these types, because the
// station serves no field that could fill one. An OPTIONAL field with no
// producer reads as 「we looked and there was none」 on every screen that
// renders it, which is exactly the lie this ticket exists to stop: the page
// must be able to say 「尚無資料來源」, and it can only say that if the type
// system never handed it a plausible zero to print instead.

/** One retrieved entry, as `POST /api/lore/search` answers it.
 *
 * 🔴 A HIT CARRIES ONLY 第 1、2 格. 第 3、4、5 格 are reached with
 * `getLoreEntry`, because a search answer is what you PICK from — putting every
 * entry's events into the list would be a size decision nobody has made. */
export interface LoreEntrySummaryView {
  entryId: string;
  /** 第 1 格「什麼時候要記起來」— when you would want to remember this. It is
   * REQUIRED on write, and it doubles as the entry's TITLE: 五格 has no `label`
   * cell and this one carries no length cap. */
  trigger: string;
  /** 第 2 格「內容」— the compressed body, and the only cell that ever enters a
   * boot context. Also required on write. */
  content: string;
  /** Subject keys (`repo:officraft`) this entry is filed under. */
  subjects: string[];
  actions: string[];
  /** Whose knowledge this is (`human:Seth`, `agent:Kyle`). */
  origin: string;
  /** `T1` matched every axis asked on; `T2` reached the caller across an axis
   * that was NOT asked about. Meaningless without `tieredBy` — read together. */
  tier: string;
  tierNote: string;
  /** `method` / `trust` / `cognitive`. */
  trustScope: string;
  /** True when the class was reached by FAILING CLOSED on an action name
   * nothing recognised, rather than by the mapping table. A guessed class must
   * not look identical to a known one. */
  trustFellBack: boolean;
}

/** What the server ACTUALLY applied, echoed back. Required beside the tiers:
 * a tier without its axes is read under a meaning it no longer has. */
export interface LoreSearchAppliedView {
  subject: string;
  actions: string[];
  query: string;
  /** How `query` was matched. Today always `literal-substring`. */
  queryMatch: string;
  limit: number;
  /** The axes the tier was computed over. Empty ⇒ nothing in the answer
   * reached the caller across an axis it did not ask about. */
  tieredBy: string[];
}

/** One search answer, with every honesty marker the wire carries. */
export interface LoreSearchView {
  entries: LoreEntrySummaryView[];
  /** How many matched BEFORE the count cap — NOT `entries.length`. */
  total: number;
  /** True when the cap dropped some of them. */
  truncated: boolean;
  /** False ONLY when a `subject` was given and named nothing. 「this subject
   * has no entries」 and 「this subject does not exist」 are different answers. */
  subjectResolved: boolean;
  /** The subject key that named nothing, echoed so a typo is visible. */
  unresolvedSubject: string;
  applied: LoreSearchAppliedView;
  /** Action names the trust table did not recognise — non-empty means at least
   * one entry was classified by failing closed. */
  unmappedActions: string[];
}

/** One catalogue line of an entry's revision history — no text at all. */
export interface LoreRevisionRowView {
  revisionId: number;
  createdTs: number;
  actorId: string;
  sha256: string;
  /** How many characters this write REMOVED compared with the previous one.
   * 🔴 The ONE place an entry being quietly hollowed out is visible: the entry
   * count does not move when an entry is emptied. */
  shrinkChars: number;
}

/** 第 5 格「相關的完整資訊」— ONE event: 時／事／人／地／物.
 *
 * 🔴 `actor` / `place` / `object` COME BACK EMPTY WHEN NOBODY KNEW THEM, and
 * they are never back-filled with 「未知」 — not on the wire and not on the
 * screen. 「查不出是誰」 and 「還沒有人去查」 have to stay distinguishable, and a
 * placeholder collapses them into one.
 *
 * ⚠️ Empty is the ONLY unconstrained value these three take. A NON-EMPTY one is
 * validated on write (`loreEventError`): it must be shaped `type:name` and the
 * prefix must be an approved entity type, or the WHOLE write is refused 422.
 * So anything that arrives non-empty is a well-formed key — but this surface
 * still must not derive a type badge from it, because 「no key」 carries no
 * prefix to derive from and would need a placeholder to render. */
export interface LoreEventView {
  /** When the event HAPPENED — not when anybody wrote it down. Required. */
  happenedTs: number;
  /** What happened, in the ACTIVE VOICE, so `actor` is always the one who did
   * it rather than the one it happened to. Required. */
  what: string;
  /** 人 — who did it. May be empty; an act that never went through the API has
   * nobody the server could stamp. */
  actor: string;
  /** 地 — which MACHINE it happened on. May be empty. */
  place: string;
  /** 物 — what was acted upon. May be empty. */
  object: string;
}

/** One entry in full, plus the original preserved beside it. */
export interface LoreEntryDetailView {
  entryId: string;
  /** 第 1 格, required, and the entry's title. */
  trigger: string;
  /** 第 2 格, required — the only cell that enters a boot context. */
  content: string;
  /** 第 3 格「什麼時候不需要了」— free text, no closed value set. May be empty
   * — and an EMPTY one is rendered as an empty field with its name printed,
   * never omitted: 「blank」 and 「no such section」 must not look the same. */
  retireWhen: string;
  /** 第 4 格「之前發生過什麼問題」— optional as a field while being the
   * substance of the entry. May be empty, same rendering rule. */
  problem: string;
  /** 第 5 格, IN THE ORDER THE EVENTS HAPPENED — empty array when the entry
   * carries none, which the surface states rather than omits. */
  events: LoreEventView[];
  subjects: string[];
  actions: string[];
  origin: string;
  /** `active`, `superseded`, `retired` or `underspecified`. */
  status: string;
  /** The FULL text as last written, every named field including the blank
   * ones AND the `events:` block. Empty ONLY for an entry written before the
   * mechanism existed. */
  original: string;
  /** Digest of `original`, so a reader can tell it holds what was stored. */
  sha256: string;
  /** The entry this one took over from; empty when none. */
  supersedes: string;
  /** Who wrote the LATEST revision. */
  writtenBy: string;
  /** Oldest first, no text. */
  revisions: LoreRevisionRowView[];
}

/** ONE revision's exact stored text. */
export interface LoreRevisionView {
  revisionId: number;
  entryId: string;
  body: string;
  sha256: string;
  createdTs: number;
  actorId: string;
  shrinkChars: number;
}

// ── T-33 對象審核 (待審 / 核可 / 合併) ────────────────────────────────────
//
// owner 2026-09-02 逐字:「agent 做完功課以後給建議並提出我一眼就可以判斷的資
// 訊,我還是做最後的裁決」⇒ 每一列除了「是什麼」還要帶「伺服器算出來的建議、
// 以及那個建議的依據」。依據是**像法的名字**(正規化後相同/編輯距離 1/前綴),
// 刻意不是一個相似度分數:分數是判斷冒充數字,而這張票在治的就是那個。

/** 一個既有對象跟待審這一筆像在哪裡。 */
export interface LoreEntitySimilarView {
  entityId: string;
  /** `type:name`。 */
  canonical: string;
  /** 最強的那一種像法的名字,不是分數。 */
  reason: string;
}

/** 待審佇列的一列:對象本身 ＋ 伺服器做完的功課。 */
export interface LorePendingEntityView {
  entityId: string;
  canonical: string;
  type: string;
  name: string;
  createdTs: number;
  /** 這個對象底下已經有幾條記憶。 */
  entries: number;
  /** `approve` / `merge` / **空字串 ＝ 算不出明確結論**。空的時候畫面不准補一
   * 個 —— 硬給的建議跟算得出來的長得一模一樣。 */
  suggestion: string;
  /** `suggestion` 是 merge 時,建議併進哪一個。 */
  mergeTarget: string;
  similar: LoreEntitySimilarView[];
  /** 這個對象底下第一條記憶的**第 2 格 `content`**,截斷過。空 ⇒ 底下還沒有記憶。
   * ⚠️ `sampleShort` / wire 上的 `sample_short` 是六格時代 `short` 留下的名字,
   * 讀的欄位早就是 `content` 了。名字沒有跟著改是刻意的:它在線上、座艙讀它,
   * 改名是一次 wire 變更,不順手夾帶。 */
  sampleShort: string;
}

/** 核可或合併之後,伺服器回的那一筆治理紀錄。 */
export interface LoreEntityGovernanceView {
  entityId: string;
  canonical: string;
  pending: boolean;
  mergedInto: string;
  kind: string;
  reason: string;
  actorId: string;
  createdTs: number;
}
