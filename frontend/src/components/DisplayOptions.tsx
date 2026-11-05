// 顯示方式: a button at the right end of a list page's 篩選 row that opens a
// small panel of view choices (the 任務頁's 分組依據, the 請示 page's 排序依據),
// kept apart from the filters and from what 清除篩選 clears. Dismissal follows
// GallerySenderFilter (outside mousedown, Esc through the layer stack).

import { useEffect, useId, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { SlidersIcon } from "./icons";
import "./display-options.css";

export function DisplayOptions<V extends string>({
  label,
  value,
  options,
  onChange,
  selectTestId,
}: {
  /** The select's label inside the panel. */
  label: string;
  value: V;
  options: readonly { value: V; label: string }[];
  onChange: (next: V) => void;
  selectTestId: string;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const panelId = useId();

  useEscapeLayer(
    () => {
      setOpen(false);
      triggerRef.current?.focus();
    },
    boxRef,
    open
  );

  useEffect(() => {
    if (!open) return;
    // Focusing the select itself makes phone browsers pop its picker open the
    // moment the panel appears.
    panelRef.current?.focus();
    const onDocDown = (e: MouseEvent) => {
      if (!boxRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDocDown, true);
    return () => document.removeEventListener("mousedown", onDocDown, true);
  }, [open]);

  return (
    <div className="display-options" ref={boxRef}>
      <button
        ref={triggerRef}
        type="button"
        className="display-options__trigger"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        data-testid="display-options"
        onClick={() => setOpen((v) => !v)}
      >
        <SlidersIcon size={14} aria-hidden="true" />
        <span className="display-options__label">{t.tasks.displayOptions}</span>
      </button>
      {open && (
        <div
          ref={panelRef}
          id={panelId}
          tabIndex={-1}
          className="display-options__pop"
          role="dialog"
          aria-labelledby={`${panelId}-title`}
          data-testid="display-options-panel"
        >
          <div id={`${panelId}-title`} className="display-options__title">
            {t.tasks.displayOptions}
          </div>
          <label className="display-options__row">
            <span>{label}</span>
            <select
              className="display-options__select"
              value={value}
              onChange={(e) => {
                onChange(e.target.value as V);
                setOpen(false);
                triggerRef.current?.focus();
              }}
              data-testid={selectTestId}
            >
              {options.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          </label>
        </div>
      )}
    </div>
  );
}
