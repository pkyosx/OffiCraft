// agentDetailVm — the ONE assembly behind the member detail panel's and the
// outsource detail panel's `AgentDetailVM` (T-14 item 2).
//
// `AGENT_DETAIL_SLOTS` already forces both wrappers to answer every CARD; what
// it never touched was the vm literal underneath, where the same twenty-odd
// rules were written out twice. Twice is how they drifted: 模型 was gated on
// "is the agent awake" on one side and preferred a live monitoring session on
// the other, and 模型 carried its 「最近一次開機回報」 tag on one side only —
// neither difference was a decision anyone had made, and nothing anywhere said
// so. So the rules live here now, both wrappers call in, and a change to one of
// them lands on both or on neither.
//
// The split is deliberate: what stays in a wrapper is the part that genuinely
// differs — reading its own domain object (`Member` vs `OutsourceWorkerView`),
// resolving a machine to a display name, and picking its own i18n leaves. What
// comes here is every rule that both sides answer the SAME way. This module
// therefore knows nothing about either domain type, which is what keeps
// `AgentDetailPanel` from having to (see `pendingChange.ts` for the same shape
// applied to the four pending hints).

import type { AgentDetailVM } from "../components/AgentDetailPanel";

/** One machine as the settings dialog's <select> needs it. */
export interface MachineOption {
  machineId: string;
  label: string;
  /** Renders the entry disabled — see `machineOptions`. */
  offline: boolean;
}

/** A machine registry entry, narrowed to what this module reads. */
interface MachineLike {
  machineId: string;
  displayName: string;
  online: boolean;
}

/** What the machine <select> may offer: every online machine, PLUS the agent's
 * own pin when that machine is not online right now — labelled 離線 and
 * disabled.
 *
 * Both halves matter. Without the pinned entry the select's value would match
 * no option (a blank row, with the pin still submitted), and dropping the pin
 * would move an agent the owner deliberately parked. Leaving it *selectable*
 * would wind the agent down onto a machine with no warden. Keeping it visible
 * but disabled is the only shape that is neither.
 *
 * 🔴 This used to be written out in full in BOTH panels, and the parity doc
 * pointed at the pair as an audit device ("compare the two blocks"). The owner
 * retired that on `rc-fc9ab61ad057` (option [2], 「合掉就好」) — the audit is now
 * this one function plus its tests, and `docs/design/worker-panel-parity.md`
 * was corrected in the same change.
 */
export function machineOptions(
  machines: MachineLike[],
  desiredMachineId: string,
  offlineLabel: (displayName: string) => string,
): MachineOption[] {
  const onlineMachines = machines.filter((m) => m.online);
  const pinnedOffline =
    desiredMachineId &&
    !onlineMachines.some((m) => m.machineId === desiredMachineId)
      ? (machines.find((m) => m.machineId === desiredMachineId) ?? {
          machineId: desiredMachineId,
          displayName: desiredMachineId,
        })
      : undefined;
  return [
    ...onlineMachines.map((m) => ({
      machineId: m.machineId,
      label: m.displayName,
      offline: false,
    })),
    ...(pinnedOffline
      ? [
          {
            machineId: pinnedOffline.machineId,
            label: offlineLabel(pinnedOffline.displayName),
            offline: true,
          },
        ]
      : []),
  ];
}

/** 累計總花費 = 已 banked 的歷史成本 + 當前 live session 成本 (DTO 保證兩者分開
 * 不重疊)。
 *
 * Honest: only when BOTH sources are absent is there no figure to show (null ⇒
 * the panel's dash). One present source counts, with the missing half read as
 * "no cost incurred yet" — not as "unknown", which would hide a real number. */
export function totalCostOf(
  liveCost: number | null | undefined,
  bankedCost: number | null | undefined,
): number | null {
  return liveCost == null && bankedCost == null
    ? null
    : (liveCost ?? 0) + (bankedCost ?? 0);
}

/** The wrapper's answers to the questions this module cannot ask on its own.
 *
 * Everything here is either a fact read off the wrapper's own domain object or
 * an i18n leaf the two panels deliberately word differently. Every field the
 * two panels answer the SAME way is derived below instead of being passed. */
export interface AgentDetailVmInput {
  /** data-testid prefix — each page's existing stable test surface. */
  testIdPrefix: string;
  /** The agent's session is really up: gates the refocus button (the server
   * 409s an offline refocus on both kinds).
   *
   * ⚠️ The two wrappers do NOT compute this the same way, and folding them was
   * deliberately left out of T-14 item 2 (out of its scope: the three rulings
   * it carried were 模型 provenance, the 模型/思考強度 gate, and the machine
   * option list). The member panel reads `member.status`, the FROZEN tri-state,
   * and `toMember` collapses `stopping → online` there — so a member winding
   * down still counts as online here. The worker panel reads
   * `worker.presence === "online"`, the five-state word, where `stopping` is
   * its own state and does NOT count. ⇒ a member may refocus mid-wind-down and
   * a worker may not. Nothing today says which is right; settling it is a
   * behaviour change and needs an owner ruling of its own. */
  online: boolean;
  /** The agent is awakened (presence online or waking) — owner presence
   * contract T-2860. Gates 模型 / 思考強度 here, and the wrapper applies it to
   * its own 機器 / Claude Account cells. */
  awake: boolean;
  /** The owner-CONFIGURED launch runtime. */
  runtime: "claude" | "codex" | undefined;
  /** Runtime last SELF-REPORTED by the live session; "" ⇒ nothing reported. */
  actualRuntime: "claude" | "codex" | "" | undefined;
  /** Model / effort last SELF-REPORTED. Never the configured twin. */
  actualModel: string | undefined;
  actualEffort: string | undefined;
  pending: {
    runtime?: string;
    model?: string;
    effort?: string;
    machine?: string;
  };
  /** Already resolved AND already gated by the wrapper: the member panel reads
   * a bare dash when not awake, the worker panel falls back to 「尚未分配」. */
  machineText: string;
  /** Already resolved readable account name — never a raw credential key.
   *
   * ⚠️ Gated by the wrapper, and the two gates differ: the member panel blanks
   * it when not awake (T-2860), the worker panel shows whatever the DTO
   * resolved. Same shape of question as `machineText`, and likewise out of
   * T-14 item 2's scope — folding it would change what one of the two panels
   * shows, which is not something to do without asking. */
  accountText: string;
  contextPct: number | null | undefined;
  compactionCount: number | null | undefined;
  /** The live session's cost and the banked history; see `totalCostOf`. */
  liveCost: number | null | undefined;
  bankedCost: number | null | undefined;
  onRefocus?: () => void | Promise<unknown>;
  /** 成本歸零. Absent ⇒ the panel renders no button at all, which is how a
   * surface that must not offer it opts out. It is allowed to REJECT — the
   * panel keeps its confirm open and shows the failure rather than pretending. */
  onResetCost?: () => void | Promise<unknown>;
  refocusSince: number | null | undefined;
  refocusOp: string | undefined;
  refocusDeadline: number | null | undefined;
  /** i18n leaves the two panels word differently — kept per-wrapper on
   * purpose: 「已送出」 and the 換手 history line read differently for a member
   * and for an outsource worker. */
  refocusSubmittedNote: string;
  refocusSinceLabel: (t: string) => string;
  /** The last operation's own name, plus the two verbs it may be rendered as.
   * The two panels keep SEPARATE 開機 / 停止 leaves because both key sets sit on
   * the theme-overridable whitelist in `messageKeys.generated.ts`. Folding them
   * would delete keys an existing theme pack may override, and that override
   * would then be dropped SILENTLY: both validators prune an off-whitelist code
   * and merely report it through `skipped` — never a rejection (`themeWording.ts`
   * validateWording, `wording_bundle.go` dropUnknownWordingCodes; owner ruling
   * rc-1599a0026a80, which exists precisely so such a pack stays importable).
   * The pack still imports; its wording just stops taking effect, unnoticed.
   * Folding would also permanently remove the ability to word the two panels
   * differently. So the leaves stay two and only the CHOICE between them is
   * shared. */
  lastOp: string | undefined;
  lastOpStartText: string;
  lastOpStopText: string;
  lastOpOk: boolean | null | undefined;
  lastOpLog: string | undefined;
  lastOpReason: string | undefined;
  lastOpAt: number | null | undefined;
  tmuxSession: string;
  terminalHint: string;
  prompt?: AgentDetailVM["prompt"];
}

/** The op names that render as 開機 / 停止. The worker wire prefixes its own
 * (`worker_start` / `worker_stop`), so one list covers both sides.
 *
 * This list is deliberately PERMISSIVE, not a claim about what each side can
 * hold. What keeps a member row off the worker verbs is ROUTING, not a verb
 * allowlist: a receipt keyed to an `ow-` id is handed to the worker fold
 * (`foldWorkerCommandResult`), the rest to the member fold — and BOTH folds
 * then assign the verb verbatim (`m.LastOp = rpc` / `w.LastOp = rpc`, bare,
 * unchecked). So if a worker-prefixed verb ever reached the member fold it
 * would be stored, and this list would render it as 開機 rather than as its
 * raw string. That is the intended failure mode (a readable word beats a raw
 * one), but do not read this comment as "it cannot happen" — nothing enforces
 * that. An earlier draft of this comment asserted it did. */
const START_OPS = ["start", "worker_start"];
const STOP_OPS = ["stop", "worker_stop"];

/** Build the ONE view model both detail panels render through.
 *
 * The three rules this settled, each an owner ruling on 2026-08-28 rather than
 * a judgement call made here:
 *
 *  - `modelIsReported` is ALWAYS true (`rc-b8d219446b13`, option [0]): the 模型
 *    row states what the agent reported, on both kinds, so both carry the tag
 *    that says so. The outsource panel grew that line; it did not have it.
 *  - 模型 / 思考強度 are gated on `awake` (`rc-8a129bc3a188`, option [1]): not
 *    awake ⇒ the dash. The outsource panel used to prefer a live monitoring
 *    session's value with no gate at all, so it could report a model for an
 *    agent that was not up.
 *  - the machine option list is shared (`rc-fc9ab61ad057`, option [2]).
 */
export function buildAgentDetailVm(input: AgentDetailVmInput): AgentDetailVM {
  const lastOp = input.lastOp ?? "";
  return {
    testIdPrefix: input.testIdPrefix,
    online: input.online,
    runtime: input.runtime || "claude",
    // The READOUT is the REPORTED runtime — the honest dash until something
    // reports one. The configured value above only labels the account row.
    reportedRuntime: input.actualRuntime ?? "",
    // The panel states what is RUNNING. A configured launch model/effort is
    // intentionally kept out of this read-only surface — it lives in the 設定
    // dialog, the only place it may be shown or written.
    model: input.awake ? (input.actualModel ?? "") : "",
    effort: input.awake ? (input.actualEffort ?? "") : "",
    // …and because the row shows a REPORTED value rather than the configured
    // one, it carries the tag that says so — otherwise the settings dialog,
    // which edits the configured value, looks like it disagrees with the panel.
    modelIsReported: true,
    pending: input.pending,
    machineText: input.machineText,
    accountText: input.accountText,
    contextPct: input.contextPct ?? null,
    compactionCount: input.compactionCount ?? null,
    cost: totalCostOf(input.liveCost, input.bankedCost),
    onRefocus: input.onRefocus
      ? async () => void (await input.onRefocus!())
      : undefined,
    onResetCost: input.onResetCost
      ? async () => void (await input.onResetCost!())
      : undefined,
    refocusSince: input.refocusSince ?? null,
    refocusOp: input.refocusOp,
    refocusDeadline: input.refocusDeadline,
    refocusSubmittedNote: input.refocusSubmittedNote,
    refocusSinceLabel: input.refocusSinceLabel,
    lastOp,
    lastOpVerb: START_OPS.includes(lastOp)
      ? input.lastOpStartText
      : STOP_OPS.includes(lastOp)
        ? input.lastOpStopText
        : lastOp,
    lastOpOk: input.lastOpOk ?? null,
    lastOpLog: input.lastOpLog ?? "",
    lastOpReason: input.lastOpReason ?? "",
    lastOpAt: input.lastOpAt ?? null,
    tmuxSession: input.tmuxSession,
    terminalHint: input.terminalHint,
    prompt: input.prompt,
  };
}
