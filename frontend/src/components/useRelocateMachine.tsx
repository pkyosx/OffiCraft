import { useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import type { MachineView, MemberRelocateResult } from "../types";
import { MachinePicker } from "./MachinePicker";
import { PencilIcon } from "./icons";

interface UseRelocateMachineOpts {
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
}

/**
 * The ONE 改機器 control both detail panels render (P7b convergence): the 編輯
 * button next to the 機器 label plus its machine-picker overlay, with the
 * shared 0/1/2+ online rule — 0 → disabled (tooltip), 1 → move straight to it,
 * 2+ → open the picker. Placement-only: it never wakes the agent.
 */
export function useRelocateMachine({
  machines,
  boundMachineId,
  onRelocate,
  testId,
  pickerTitle,
  pickerConfirmLabel,
  noOnlineTitle,
  withIcon,
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
  const onlineMachines = machines.filter((m) => m.online);

  const run = (machineId: string) => {
    setPickerOpen(false);
    setBusy(true);
    setUndispatched(false); // a fresh attempt clears the previous verdict
    void (async () => {
      try {
        const result = await onRelocate?.(machineId);
        if (result?.relocationPending) setUndispatched(true);
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

  return { relocateAction, relocatePicker, relocateUndispatched: undispatched };
}
