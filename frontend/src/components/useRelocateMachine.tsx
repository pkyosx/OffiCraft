import { useEffect, useRef, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import type { MachineView, MemberRelocateResult } from "../types";
import { MachinePicker } from "./MachinePicker";
import { PencilIcon } from "./icons";

interface UseRelocateMachineOpts {
  /** The agent this control is bound to (`member.id` / `worker.id`).
   *
   * 🔴 REQUIRED, and it is the notice's identity — not decoration (review r2,
   * the relocate twin of r1 SHOULD-1). Neither detail panel is remounted when
   * the owner selects a different agent (no `key` at either call site), so this
   * hook's `undispatched` verdict outlives the agent it was decided for unless
   * something resets it.
   *
   * ⚠️ SCOPE OF THAT CLAIM — and this comment splits by PROVENANCE, because an
   * earlier draft of it over-claimed twice (see the commit that fixed it):
   *
   *   MEASURED (r3's 2-factor experiment): reverting the observability gate did
   *   NOT turn the leak test green. So this is a PRE-EXISTING leak that three
   *   review rounds missed — NOT a regression introduced alongside that gate.
   *
   *   DERIVED, not measured (from r2's probe P2 plus the `undispatched &&
   *   !landed` boolean; nobody ran a positive control for it): the gate masks
   *   the leak only for agents that are BOTH unobservable AND pinned, where
   *   `landed` comes out accidentally true and swallows it. Treat this half as
   *   a reading of the code, not as an experimental result. */
  subjectId: string;
  /** ALL machines (online + offline) — the caller's ONE useMachines() result. */
  machines: MachineView[];
  /** The agent's owner-pinned machine id (desiredMachineId), or null. */
  boundMachineId: string | null;
  /** Fire the relocate. Undefined ⇒ no button (the affordance is hidden). The
   * panels lean on the member / outsource_worker SSE refetch for the post-move
   * refresh, so the handler need only fire. */
  onRelocate?: (
    machineId: string,
  ) => void | Promise<MemberRelocateResult | void>;
  testId: string;
  pickerTitle: string;
  pickerConfirmLabel: string;
  /** Tooltip shown on the disabled button when no machine is online. */
  noOnlineTitle: string;
  /** Render the pencil icon inside the button (the member panel's look). */
  withIcon?: boolean;
  /** The agent's CURRENT machine (`member.machine`), i.e. where it actually is —
   * as opposed to `boundMachineId`, which is only where the owner PINNED it.
   *
   * 🔴 This is the self-heal signal for the "move scheduled, not landed" notice
   * (review r1 SHOULD-2). Without it `undispatched` was cleared ONLY by another
   * relocate attempt, so once the background cadence actually landed the move,
   * the panel kept telling the owner it had not — the notice's own promise
   * ("the server keeps retrying") had no path back. When the current machine
   * reaches the pin, the move HAS landed and the notice is false.
   *
   * 🔴 CALLERS MUST PASS null WHEN THE PLACEMENT IS NOT OBSERVABLE (review r2).
   * `member.machine` is NOT a pure observation: `observedHost`
   * (`server/ocserverd/api_helpers.go:240-253`) falls back to
   * `m.DesiredMachineID` when the hub has no session and telemetry has nothing
   * — so for a member nobody can see, `member.machine === desiredMachineId` is
   * TRUE BY CONSTRUCTION and means "we do not know where it is", never "it got
   * there". Feeding that in raw made the notice self-heal on a move that had
   * not happened. MemberDetailPanel already gates its 機器 cell on `awake` for
   * this exact reason (its own comment names the desired_machine residual); the
   * self-heal signal is gated with it. */
  currentMachineId?: string | null;
}

/**
 * The ONE 改機器 control both detail panels render (P7b convergence): the 編輯
 * button next to the 機器 label plus its machine-picker overlay, with the
 * shared 0/1/2+ online rule — 0 → disabled (tooltip), 1 → move straight to it,
 * 2+ → open the picker. Placement-only: it never wakes the agent.
 */
export function useRelocateMachine({
  subjectId,
  machines,
  boundMachineId,
  onRelocate,
  testId,
  pickerTitle,
  pickerConfirmLabel,
  noOnlineTitle,
  withIcon,
  currentMachineId,
}: UseRelocateMachineOpts): {
  relocateAction: ReactNode | undefined;
  relocatePicker: ReactNode | undefined;
  /** True once a relocate came back `relocation_pending` (T-7fa1). */
  relocateUndispatched: boolean;
} {
  const { t } = useI18n();
  const [pickerOpen, setPickerOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  // T-7fa1: the relocate answered 200 but its recycle STOP/START never reached a
  // warden — pinned, not landed. Surfaced by the caller as a DispatchAlert.
  const [undispatched, setUndispatched] = useState(false);
  // The verdict is about ONE agent: drop it when the panel switches to another.
  useEffect(() => {
    setUndispatched(false);
  }, [subjectId]);
  // …and the reset above is a reset, not a CANCEL: a relocate still in flight
  // during the switch resolves afterwards. Same render-time ref discipline as
  // MemberDetailPanel's wake.
  const subjectIdRef = useRef(subjectId);
  subjectIdRef.current = subjectId;
  const onlineMachines = machines.filter((m) => m.online);

  // Self-heal: the pin is where it was ASKED to be, `currentMachineId` is where
  // it is OBSERVED to be (null when nobody can see it — see the prop doc).
  // Satisfied ⇒ the move landed ⇒ the notice is stale.
  //
  // The null/"" guards are load-bearing, not defensive noise: an unpinned member
  // (`desiredMachineId: ""` → boundMachineId null) with no observed machine
  // would otherwise compare null === null and swallow a live verdict.
  //
  // 🔴 `"auto"` is a legal pin (`types.ts`: "" = unpinned, "auto" = idlest-online,
  // else a concrete machine id) and NO concrete machine id ever equals it, so the
  // plain equality left an auto-pinned member's notice stuck on screen forever —
  // the r1 SHOULD-2 stickiness this self-heal exists to end. "auto" delegates the
  // choice to the scheduler, so ANY observed placement satisfies it.
  const observedMachineId =
    currentMachineId != null && currentMachineId !== "" ? currentMachineId : null;
  const landed =
    observedMachineId != null &&
    (boundMachineId === "auto" || observedMachineId === boundMachineId);
  useEffect(() => {
    if (landed) setUndispatched(false);
  }, [landed]);

  const run = (machineId: string) => {
    setPickerOpen(false);
    setBusy(true);
    setUndispatched(false); // a fresh attempt clears the previous verdict
    const firedFor = subjectId; // whose move this verdict belongs to
    void (async () => {
      try {
        const result = await onRelocate?.(machineId);
        if (subjectIdRef.current !== firedFor) return;
        if (result?.relocationPending) setUndispatched(true);
      } catch {
        if (subjectIdRef.current !== firedFor) return;
        // NIT-3 (review r1): the openapi-fetch middleware throws on EVERY
        // non-2xx, so a rejected relocate is a real path, not a theoretical
        // one. Before this it escaped as an unhandled rejection. There is no
        // verdict to show — a rejected relocate never reached the server's
        // pending determination — so we only make sure a STALE verdict does
        // not survive the new attempt.
        setUndispatched(false);
      } finally {
        // The SSE delta refetches the panel; clear the in-flight guard either
        // way (a rejected relocate lets the owner retry).
        setBusy(false);
      }
    })();
  };

  const canRelocate = Boolean(onRelocate) && onlineMachines.length >= 1;
  const handleClick =
    canRelocate && !busy
      ? () => {
          if (onlineMachines.length === 1) run(onlineMachines[0].machineId);
          else setPickerOpen(true);
        }
      : undefined;

  const relocateAction = onRelocate ? (
    <button
      type="button"
      className="doc-btn doc-btn--edit"
      data-testid={testId}
      disabled={!handleClick}
      title={onlineMachines.length === 0 ? noOnlineTitle : undefined}
      onClick={handleClick}
    >
      {withIcon && <PencilIcon size={14} />}
      <span>{t.settings.edit}</span>
    </button>
  ) : undefined;

  const relocatePicker = pickerOpen ? (
    <MachinePicker
      machines={machines}
      boundMachineId={boundMachineId}
      title={pickerTitle}
      confirmLabel={pickerConfirmLabel}
      onConfirm={run}
      onCancel={() => setPickerOpen(false)}
    />
  ) : undefined;

  return {
    relocateAction,
    relocatePicker,
    // `landed` wins over a stale flag even before the effect flushes.
    relocateUndispatched: undispatched && !landed,
  };
}
