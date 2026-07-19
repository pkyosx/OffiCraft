/**
 * Layer-4 handover UI skeleton — PURE PRESENTATION shell.
 *
 * Given a lifecycle status, renders the status-appropriate set of action
 * buttons. This is an ISOLATED component: it is NOT wired to a real member and
 * is NOT connected to the frozen `MemberStatus` data contract — the status
 * union below is a LOCAL, UI-only type. Wiring to MemberDetailPanel happens in
 * a later phase.
 *
 * Honesty (no dead affordances): every action handler is an OPTIONAL prop. A
 * button is interactive ONLY when its handler is supplied; otherwise it renders
 * disabled and carries no click behaviour.
 */
import { useI18n } from "../i18n";
import type { LifecycleVisualStatus } from "./LifecycleDot";

/** UI-only lifecycle status (shares the five-state visual union). */
export type LifecycleStatus = LifecycleVisualStatus;

type ActionKey = "spawn" | "cancel" | "stop" | "force-stop";

/** Per-status button set (order = display order), aligned to the backend's real
 * five-state presence and its real mutation endpoints (activate / deactivate).
 * (Refocus is deliberately NOT a header action — it lives with the
 * context cell in MemberDetailPanel, so the header never duplicates it. Dismiss
 * is deliberately NOT offered here either — owner acceptance removed the UI
 * entry; DELETE /api/members stays a pure backend seam with no button.) The
 * winding-down states (`stopping` / `waking`) can WEDGE: a
 * member can get stuck in `stopping` (still alive — SSE holding — yet pinned by a
 * stale stop marker: the survived-stop / SSE-reconnect case) or mid-`waking` if the
 * old stop command never lands (crashed warden / lost signal). So both states now
 * ALSO offer Spawn (=wake) as a FORCE-REVIVE rescue path — backed by the same
 * activate endpoint, which unconditionally clears the winding-down anchors and always
 * revives the member ("always revive from a wrong state"). Spawn is listed FIRST in
 * these wedged states = rescue-first. In `stopping` the Stop button becomes
 * FORCE-STOP (=force-stop endpoint): the member is already inside its 120s graceful-
 * stop grace, so the escalation is an IMMEDIATE kill (robust STOP straight to the
 * warden), not another graceful deactivate — the parent gates it behind a confirm.
 * `online-awake` keeps the ordinary graceful
 * Stop (=deactivate); `waking` keeps Cancel (deactivate / cancel-wake) alongside the
 * Spawn rescue. All backed by real endpoints (no dead affordance). */
const BUTTON_SETS: Record<LifecycleStatus, ActionKey[]> = {
  offline: ["spawn"],
  waking: ["cancel", "spawn"],
  "online-awake": ["stop"],
  stopping: ["spawn", "force-stop"],
  stopped: ["spawn"],
};

/** Destructive actions get the danger-ghost styling. */
const DANGER_ACTIONS = new Set<ActionKey>(["stop", "force-stop"]);

// (Refocus was removed as a header action — see MemberDetailPanel's context cell.)

interface MemberActionButtonsProps {
  status: LifecycleStatus;
  onSpawn?: () => void;
  onCancel?: () => void;
  onStop?: () => void;
  /** Force-stop (immediate kill) — offered ONLY in the `stopping` state, where the
   * Stop button IS this action. The parent should gate it behind a confirm. */
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
  onSpawn,
  onCancel,
  onStop,
  onForceStop,
  reasons,
  labels,
}: MemberActionButtonsProps) {
  const { t } = useI18n();

  const handlers: Record<ActionKey, (() => void) | undefined> = {
    spawn: onSpawn,
    cancel: onCancel,
    stop: onStop,
    "force-stop": onForceStop,
  };

  const keys = BUTTON_SETS[status];

  return (
    <div className="member-actions">
      {keys.map((key) => {
        const handler = handlers[key];
        const variant = DANGER_ACTIONS.has(key)
          ? "btn--danger-ghost"
          : "btn--accent-ghost";
        const reason = !handler ? reasons?.[key] : undefined;
        return (
          <button
            key={key}
            type="button"
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
