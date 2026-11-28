// 顯示方式: a button at the right end of a list page's 篩選 row that opens a
// small panel of view choices (the 任務頁's 分組依據, the 請示 page's 排序依據),
// kept apart from the filters and from what 清除篩選 clears. The current choice
// is shown only inside the panel; a choice other than `defaultValue` puts a
// dot on the button. On a computer the panel hangs under the button and closes
// on an outside mousedown; on a phone it is FilterPanel's bottom sheet. Esc
// goes through the layer stack either way.

import { useEffect, useId, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { useIsMobile } from "../hooks/useIsMobile";
import { BottomSheet } from "./FilterPanel";
import { SlidersIcon } from "./icons";
import "./display-options.css";

export function DisplayOptions<V extends string>({
  label,
  value,
  defaultValue,
  options,
  onChange,
  selectTestId,
}: {
  /** The select's label inside the panel. */
  label: string;
  value: V;
  /** The choice that needs no reminder on the button. */
  defaultValue: V;
  options: readonly { value: V; label: string }[];
  onChange: (next: V) => void;
  selectTestId: string;
}) {
  const { t, msg } = useI18n();
  const [open, setOpen] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const panelId = useId();
  const mobile = useIsMobile();

  const close = () => {
    setOpen(false);
    triggerRef.current?.focus();
  };
  useEscapeLayer(close, mobile ? panelRef : boxRef, open);

  useEffect(() => {
    if (!open) return;
    // Focusing the select itself makes phone browsers pop its picker open the
    // moment the panel appears.
    panelRef.current?.focus();
    if (mobile) return;
    const onDocDown = (e: MouseEvent) => {
      if (!boxRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDocDown, true);
    return () => document.removeEventListener("mousedown", onDocDown, true);
  }, [open, mobile]);

  const changed = value !== defaultValue;
  const changedLabel = changed
    ? msg.displayOptionsChanged(options.find((o) => o.value === value)?.label ?? value)
    : undefined;

  const row = (
    <label className="display-options__row">
      <span>{label}</span>
      <select
        className="display-options__select"
        value={value}
        onChange={(e) => {
          onChange(e.target.value as V);
          close();
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
  );

  return (
    <div className="display-options" ref={boxRef}>
      <button
        ref={triggerRef}
        type="button"
        className="display-options__trigger"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open && !mobile ? panelId : undefined}
        aria-label={changedLabel}
        title={changedLabel}
        data-testid="display-options"
        onClick={() => setOpen((v) => !v)}
      >
        <SlidersIcon size={14} aria-hidden="true" />
        <span className="display-options__label">{t.tasks.displayOptions}</span>
        {changed && (
          <span
            className="display-options__dot"
            aria-hidden="true"
            data-testid="display-options-changed"
          />
        )}
      </button>
      {open &&
        (mobile ? (
          createPortal(
            <BottomSheet testId="display-options" title={t.tasks.displayOptions} rootRef={panelRef} onClose={close}>
              {row}
            </BottomSheet>,
            document.body
          )
        ) : (
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
            {row}
          </div>
        ))}
    </div>
  );
}
