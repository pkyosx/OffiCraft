import { useState } from "react";
import { useI18n } from "../i18n";
import type { MachineView } from "../types";
import "./machine-picker.css";

/**
 * Machine picker (wake / respawn / move). Shown ONLY when there are 2+ online
 * machines (the 0- and 1-online cases are handled by the caller: 0 → disabled,
 * 1 → auto-use). Lists the ONLINE machines as selectable options; the member's
 * currently-bound machine, if OFFLINE, is shown DISABLED (labeled offline) and
 * is never auto-selected. Default selection = the bound machine when it is
 * online, else the first online machine. Address by machineId; DISPLAY the name.
 *
 * Dark-themed, consistent with the Monitor confirm dialog (mon-confirm).
 */
interface MachinePickerProps {
  /** ALL machines (online + offline) — the component filters for online + the
   * bound-offline entry itself. */
  machines: MachineView[];
  /** The member's currently-bound machineId (member.desiredMachineId), or null. */
  boundMachineId?: string | null;
  title: string;
  confirmLabel: string;
  onConfirm: (machineId: string) => void;
  onCancel: () => void;
}

export function MachinePicker({
  machines,
  boundMachineId,
  title,
  confirmLabel,
  onConfirm,
  onCancel,
}: MachinePickerProps) {
  const { t } = useI18n();
  const p = t.machine.picker;

  const online = machines.filter((m) => m.online);
  const boundOffline =
    boundMachineId != null
      ? machines.find((m) => m.machineId === boundMachineId && !m.online)
      : undefined;

  // Default to the bound machine when it is online; else the first online one.
  // NEVER default to the bound-offline entry (it is shown disabled below).
  const boundOnline =
    boundMachineId != null &&
    online.some((m) => m.machineId === boundMachineId)
      ? boundMachineId
      : online[0]?.machineId ?? "";

  const [selected, setSelected] = useState(boundOnline);

  return (
    <div
      className="machine-picker"
      data-testid="machine-picker"
      role="dialog"
      aria-modal="true"
    >
      <div className="machine-picker__box">
        <div className="machine-picker__title">{title}</div>
        <label className="machine-picker__field">
          <span className="machine-picker__label">{p.label}</span>
          <select
            className="machine-picker__select"
            data-testid="machine-picker-select"
            value={selected}
            onChange={(e) => setSelected(e.target.value)}
          >
            {online.map((m) => (
              <option key={m.machineId} value={m.machineId}>
                {m.displayName}
              </option>
            ))}
            {boundOffline && (
              <option value={boundOffline.machineId} disabled>
                {p.offlineOption(boundOffline.displayName)}
              </option>
            )}
          </select>
        </label>
        <div className="machine-picker__actions">
          <button
            type="button"
            className="btn btn--ghost"
            onClick={onCancel}
          >
            {t.common.cancel}
          </button>
          <button
            type="button"
            className="btn btn--accent"
            data-testid="machine-picker-confirm"
            disabled={!selected}
            onClick={() => selected && onConfirm(selected)}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
