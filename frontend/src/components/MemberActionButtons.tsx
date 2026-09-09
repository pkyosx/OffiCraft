/**
 * The identity card's action row, for BOTH actor kinds — MemberDetailPanel
 * (正職) and WorkerDetailPanel (外包) render this one component, which is what
 * keeps the labels, the order and WHICH RUNGS EXIST from drifting apart.
 *
 * Two inputs decide the row: `status` (the five-state presence, a LOCAL UI-only
 * union — not the frozen `MemberStatus` contract) picks the non-ladder buttons,
 * and `stage` says how far up 停止 → 加速停止 → 強制停止 this actor already is.
 * Both panels derive `stage` from `stopLadderStageOf` over the same wire fields.
 *
 * Honesty (no dead affordances): every action handler is an OPTIONAL prop. A
 * button is interactive ONLY when its handler is supplied; otherwise it renders
 * disabled and carries no click behaviour. A rung that is not REACHABLE yet is a
 * different thing again — it is not rendered at all, because since owner
 * 2026-08-22 (「同一個按鈕 升級的概念 不是不同按鈕」) the ladder is ONE button
 * that BECOMES the next rung; there is no second slot for an unreachable one.
 */
import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import type { LifecycleVisualStatus } from "./LifecycleDot";

/** UI-only lifecycle status (shares the five-state visual union). */
export type LifecycleStatus = LifecycleVisualStatus;

type ActionKey = "spawn" | "cancel" | "stop" | "accelerated-stop" | "force-stop";

/** How far up the escalation 停止 → 加速停止 → 強制停止 this actor already is.
 *
 * 🔴 OWNER 2026-08-21, and it REPLACES the fixed three-slot ladder this file
 * shipped hours earlier: 「加速停止應該是已經按下停止的狀態下才可以按，強制停止應該
 * 是加速停止按下的狀態才可以按，不是一開始就顯示三個按鈕」「按了才出現」, as narrowed by
 * owner 2026-08-22 to ONE upgrading button: the stage says WHICH rung that
 * button currently IS, not how many rungs are on screen.
 *
 *  - `none`         — nothing is winding down. The button is 停止.
 *  - `soft`         — a soft wind-down is OPEN. The button is 加速停止.
 *  - `accelerated`  — that wind-down is on the clock. The button is 強制停止.
 */
export type StopLadderStage = "none" | "soft" | "accelerated";

/** The observable wind-down facts both actor kinds carry on the wire. Members
 * and outsource workers publish the SAME four (mappers' `toMember` /
 * `toOutsourceWorker`), which is what lets one function answer for both. */
export interface StopLadderFacts {
  /** Wire `presence`, the five-state word — under the name the MEMBER view
   * model gives it. */
  lifecycle?: string;
  /** The same wire `presence`, under the name the WORKER view model gives it.
   * One field, two view-model spellings (`toMember` renames it, the outsource
   * mapper does not); accepting both is what lets one call site per panel pass
   * its own object and still be the same reading. */
  presence?: string;
  /** Wire `desired_state` ("offline" once the owner has asked for a stop). */
  desiredState?: string;
  /** Wire `refocus_since` > 0 → epoch, else null. */
  refocusSince?: number | null;
  /** Wire `refocus_op` — the CAUSE of the open wind-down, "" when none. */
  refocusOp?: string;
}

/** The two `refocus_op` causes the server puts on a clock — winddownKindFor's
 * membership test, verbatim (`context_high` = the SECOND context threshold the
 * system opens by itself; `accelerated_stop` = the owner pressed the button).
 * They are exactly the arm 強制停止 is allowed to appear on. */
const CLOCKED_OPS = new Set(["context_high", "accelerated_stop"]);

/**
 * Where on the ladder this actor stands — the ONE reading of the ruling, shared
 * by 正職 and 外包 so the two panels can never disagree about which rung exists.
 *
 * 🔴 WHY THESE FIELDS (implementer's reading — the owner ruled the BEHAVIOUR,
 * not the column):
 *
 *  - "已經觸發軟下線" is NOT "the owner pressed 停止". He said so explicitly
 *    (「應該說已經觸發軟下線的人可以被觸發加速下線」), and the difference is a
 *    whole arm: a wind-down the SYSTEM opens at a context threshold stamps
 *    `refocus_since` with `desired_state` still online, so `PresenceState`
 *    projects it as plain `online` — presence alone cannot see it, and a
 *    `status === "stopping"` test (what this file used to do) hides 加速停止 on
 *    exactly the members the owner named. So the test is the SERVER'S OWN
 *    acceptance gate, mirrored: `HandleAcceleratedStop…` takes the 下線 arm
 *    (`desired_state == offline && stopping_since > 0`) or the 換手 arm
 *    (`refocus_since > 0`). `stopping_since` is deliberately not on the wire, so
 *    the cockpit reads the projection derived from it — presence `stopping` —
 *    which is the same substitution `api/mock.ts` already makes for its 409 gate.
 *
 *  - "已進入加速臂" is `refocus_op`, not `refocus_deadline`. Both answer the same
 *    question today (the deadline is DERIVED from the op through
 *    `winddownKindFor`), but the op is the CAUSE and the deadline folds in two
 *    unrelated conditions (`forcedEpochLive`, a zero `stopping_since`) that would
 *    silently change which buttons exist for reasons that have nothing to do with
 *    which arm we are on. The wire doc names the op as the field that "tells the
 *    two apart"; this is that sentence, executed.
 *
 * Monotone by construction: `accelerated` is only ever returned from inside the
 * open-wind-down branch, so 強制停止 can never appear on an actor that has no
 * wind-down at all — whatever a stale `refocus_op` says.
 */
export function stopLadderStageOf(f: StopLadderFacts): StopLadderStage {
  const presence = f.lifecycle ?? f.presence;
  const open =
    (f.desiredState === "offline" && presence === "stopping") ||
    (f.refocusSince ?? 0) > 0;
  if (!open) return "none";
  return CLOCKED_OPS.has(f.refocusOp ?? "") ? "accelerated" : "soft";
}

/** THE RUNG THE ONE LADDER BUTTON IS AT, per stage.
 *
 * 🔴 OWNER 2026-08-22 (reply card rc-2afe8b557e9c, option [D]) — this REPLACES
 * the revealed-side-by-side row: 「停止 → 加速停止 → 強制停止 UI 顯示怪怪的，他
 * 應該體感上像是同一個按鈕 升級的概念 不是不同按鈕」. So the ladder is ONE
 * button in ONE slot whose label, action and testid change as the actor climbs;
 * the spent rung is no longer kept beside it.
 *
 * 🔴 WHAT THAT COSTS, so the next reader does not "restore" it by accident: the
 * previous row's first guard was SLOT SEPARATION — the pressed rung stayed put,
 * spent, so the newly revealed one took a fresh slot and no slot was ever
 * recycled into a harsher action. One button IS that recycled slot, by
 * construction. The owner was told that verbatim on the card, was offered a
 * long-press top rung [C] and a confirm dialog [B], and chose [D] — the plain
 * upgrade with no new guard. LADDER_ARM_MS below is therefore the ONLY thing
 * standing between a burst of clicks and 強制停止; do not weaken it, and do not
 * add a guard here that the owner declined. */
const RUNG_BY_STAGE: Record<StopLadderStage, ActionKey> = {
  none: "stop",
  soft: "accelerated-stop",
  accelerated: "force-stop",
};

/**
 * 🔴 THE DOUBLE-CLICK GUARD, and it is the whole reason this component owns a
 * timer instead of staying a pure function of its props.
 *
 * The escalation upgrades under the cursor that just pressed the rung below it,
 * and it gets LESS reversible as it climbs (強制停止 sends nothing and cuts the
 * session dead). Since owner 2026-08-22 the upgrade happens IN PLACE, so this is
 * the whole of the protection: A RUNG THAT JUST UPGRADED IS INERT. The button
 * renders with its new label already — the upgrade is honest and visible — but
 * disabled for this window, arming itself after it, so a click that was already
 * travelling when the action changed lands on nothing.
 *
 * COST, stated plainly: an owner who deliberately climbs the ladder waits this
 * long at each step, and a presentational component now holds state. 350ms is
 * chosen to cover a double-click (the platform threshold is ~250–500ms) while
 * staying under the round trip that upgrades the button in the first place, so
 * in practice it is spent, not waited.
 */
export const LADDER_ARM_MS = 350;

/** Destructive actions get the danger-ghost styling. */
const DANGER_ACTIONS = new Set<ActionKey>([
  "stop",
  "accelerated-stop",
  "force-stop",
]);

/** The non-ladder part of each status's button set, in display order. The
 * winding-down states (`stopping` / `waking`) can WEDGE — a member can get stuck
 * `stopping` (still alive, SSE holding, pinned by a stale stop marker) or
 * mid-`waking` if the old stop command never lands (crashed warden, lost
 * signal) — so both ALSO offer Spawn (=wake) as a rescue, backed by the same
 * activate endpoint. A live session is kept in place; an offline generation
 * clears the old wind-down before the stop→start handoff. Spawn leads in those
 * states: rescue first.
 * (Refocus is deliberately NOT a header action — it lives with the context cell
 * in MemberDetailPanel. Dismiss is not offered either: owner acceptance removed
 * the UI entry and DELETE /api/members stays a pure backend seam.) */
const PREFIX_SETS: Record<LifecycleStatus, ActionKey[]> = {
  offline: ["spawn"],
  waking: ["cancel", "spawn"],
  "online-awake": [],
  stopping: ["spawn"],
  stopped: ["spawn"],
};

/** Which statuses carry the ladder at all. `offline` / `stopped` / `waking`
 * have no live session to wind down, so the ladder button does not belong there
 * at all — `waking` keeps Cancel, which IS its stop. */
const LADDER_STATUSES = new Set<LifecycleStatus>(["online-awake", "stopping"]);

/** The two ways the single ladder button renders and is deliberately NOT
 * pressable, each of which says why rather than going silent:
 * `alreadyStopping` — it still reads 停止 while a stop is visibly running;
 * `justAppeared`    — it JUST upgraded, and is serving out LADDER_ARM_MS. */
type LadderReasonKey = "alreadyStopping" | "justAppeared";

// (Refocus was removed as a header action — see MemberDetailPanel's context cell.)

interface MemberActionButtonsProps {
  status: LifecycleStatus;
  /** How far up 停止 → 加速停止 → 強制停止 this actor already is — i.e. WHICH
   * rung the single ladder button currently is. Both panels derive it from the
   * SAME `stopLadderStageOf` over the SAME wire fields.
   * Defaults to `none`: a caller that knows nothing about wind-downs (the
   * IdentityRealIdStory fixture) gets the un-escalated row, never a kill button.
   */
  stage?: StopLadderStage;
  onSpawn?: () => void;
  onCancel?: () => void;
  onStop?: () => void;
  /** 加速停止 — what the one ladder button BECOMES at stage `soft`: put the
   * wind-down that is already open on the server's clock and TELL the member. Not a kill, so it needs no confirm; the
   * member is still given the grace and can still finish early. */
  onAcceleratedStop?: () => void;
  /** Force-stop (immediate kill) — what the one ladder button becomes at stage
   * `accelerated`. The parent should gate it behind a confirm. */
  onForceStop?: () => void;
  /** Optional per-action hint shown as the button `title` when that action is
   * DISABLED (no handler) — e.g. "no online machine" on spawn. Honest: it only
   * annotates an already-dead affordance, never enables one. */
  reasons?: Partial<Record<ActionKey, string>>;
  /** Optional per-action label override — the in-progress presentation the
   * Monitor machine table uses ("安裝中…" swapped into the disabled install
   * button): the parent swaps e.g. the spawn label to "喚醒中…" while a wake is
   * pending, keeping the feedback INSIDE the button instead of a side note. */
  labels?: Partial<Record<ActionKey, string>>;
}

export function MemberActionButtons({
  status,
  stage = "none",
  onSpawn,
  onCancel,
  onStop,
  onAcceleratedStop,
  onForceStop,
  reasons,
  labels,
}: MemberActionButtonsProps) {
  const { t } = useI18n();

  // The stage the row is allowed to ACT on. It starts wherever the row first
  // mounted — opening a panel on an already-accelerated member arms the button
  // immediately, because nothing changed under anybody's finger — and then
  // follows `stage` one LADDER_ARM_MS behind, which is the inert window the
  // button spends after upgrading, before it will take a click.
  const [armedStage, setArmedStage] = useState<StopLadderStage>(stage);
  useEffect(() => {
    if (stage === armedStage) return;
    const id = setTimeout(() => setArmedStage(stage), LADDER_ARM_MS);
    return () => clearTimeout(id);
  }, [stage, armedStage]);

  // Exactly one ladder cell, or none where there is no live session to wind
  // down. The rung is the stage's, so the SAME slot carries 停止 → 加速停止 →
  // 強制停止 as the actor climbs.
  const rung = LADDER_STATUSES.has(status) ? RUNG_BY_STAGE[stage] : undefined;
  const shown = rung ? [rung] : [];
  // The button is live only once the stage it is showing is the one it has
  // ARMED. Any change of rung — up or down — restarts the window, because after
  // the 2026-08-22 ruling every change swaps the action under a resting cursor.
  const armedRung = LADDER_STATUSES.has(status)
    ? RUNG_BY_STAGE[armedStage]
    : undefined;

  const ladderReason: Partial<Record<ActionKey, LadderReasonKey>> = {};
  const handlers: Record<ActionKey, (() => void) | undefined> = {
    spawn: onSpawn,
    cancel: onCancel,
    stop: onStop,
    "accelerated-stop": onAcceleratedStop,
    "force-stop": onForceStop,
  };
  // 停止 is spent once the stop it asked for is visibly running. Normally the
  // upgrade has already moved this cell on to 加速停止 by then; this covers the
  // residual world where presence says `stopping` but the wind-down facts have
  // not (yet) put the actor on a rung — pressing 停止 again only re-stamps an
  // anchor nothing on screen reflects.
  if (status === "stopping" && rung === "stop") {
    handlers.stop = undefined;
    ladderReason.stop = "alreadyStopping";
  }
  if (rung && rung !== armedRung) {
    handlers[rung] = undefined;
    ladderReason[rung] = "justAppeared";
  }

  const keys = [...PREFIX_SETS[status], ...shown];

  return (
    <div className="member-actions">
      {keys.map((key) => {
        const handler = handlers[key];
        const variant = DANGER_ACTIONS.has(key)
          ? "btn--danger-ghost"
          : "btn--accent-ghost";
        // The parent's own reason wins (it knows things this component cannot,
        // e.g. "no online machine"); the ladder reason is the fallback that
        // explains a rung disabled by the ESCALATION ORDER rather than by a
        // missing handler.
        const ladderKey = ladderReason[key];
        const reason = handler
          ? undefined
          : reasons?.[key] ?? (ladderKey ? t.lifecycle.reason[ladderKey] : undefined);
        return (
          <button
            key={key}
            type="button"
            // Address actions by IDENTITY, not by position (review r1 SHOULD-3).
        // The ladder cell's id CHANGES as it upgrades (member-action-stop →
        // -accelerated-stop → -force-stop): the id names the action the button
        // will perform, which is the only thing a test or a screen reader can
        // safely act on.
            // PREFIX_SETS puts spawn FIRST only for offline/stopped — in
            // `waking` the first button is Cancel. A test that reaches for
            // ".member-actions button" therefore silently clicks the wrong
            // action the moment its fixture is not offline.
            data-testid={`member-action-${key}`}
            className={`btn ${variant}`}
            disabled={!handler}
            {...(reason ? { title: reason } : {})}
            {...(handler ? { onClick: handler } : {})}
          >
            {labels?.[key] ?? t.lifecycle.action[key]}
          </button>
        );
      })}
    </div>
  );
}
