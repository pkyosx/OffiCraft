// MultiSelectFilter — the 任務頁 篩選列's multi-select dropdown (T-be18). One
// trigger pill (summary of the current selection) that opens a checkbox
// popover; the same click-outside/aria-expanded pattern as the task card's
// 狀態 / 優先權 dropdowns (TaskCard statusRef / prioRef).
//
// Selection semantics are owned by the PAGE, but the summary follows one rule:
//   · empty OR every option checked → allLabel   (no constraint on this axis)
//   · exactly one checked           → that option's label
//   · otherwise                     → `${noun} · N`
// so a partial status set (the default: terminals unchecked) reads as
// 「狀態 · 4」 and opening the popover shows exactly which four — the owner can
// see the terminals are excluded and re-check them. Options may carry a count
// badge (負責人 per-owner task counts, T-be18 point 3).

import { useEffect, useRef, useState } from "react";
import { ChevronDownIcon } from "./icons";

export interface MultiSelectOption {
  value: string;
  label: string;
  /** Optional trailing count badge (負責人 task counts). */
  count?: number;
}

interface MultiSelectFilterProps {
  /** aria-label + the "· N" summary noun (e.g. 負責人 / 類型 / 狀態). */
  noun: string;
  /** Summary shown when nothing — or everything — is selected (no constraint). */
  allLabel: string;
  options: MultiSelectOption[];
  selected: Set<string>;
  onChange: (next: Set<string>) => void;
  /** data-testid on the trigger; each option row gets `${testId}-opt-<value>`. */
  testId: string;
}

export function MultiSelectFilter({
  noun,
  allLabel,
  options,
  selected,
  onChange,
  testId,
}: MultiSelectFilterProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function onDown(e: MouseEvent) {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  function toggle(value: string) {
    const next = new Set(selected);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    onChange(next);
  }

  // Summary — count only options that still exist (a stale key never inflates N).
  const present = options.filter((o) => selected.has(o.value));
  let summary: string;
  if (present.length === 0 || present.length === options.length) {
    summary = allLabel;
  } else if (present.length === 1) {
    summary = present[0].label;
  } else {
    summary = `${noun} · ${present.length}`;
  }
  const constrained = present.length > 0 && present.length < options.length;

  return (
    <div className="tasks__ms" ref={ref}>
      <button
        type="button"
        className={`tasks__filter tasks__ms-trigger${
          constrained ? " tasks__ms-trigger--active" : ""
        }`}
        aria-haspopup="true"
        aria-expanded={open}
        aria-label={noun}
        data-testid={testId}
        onClick={() => setOpen((o) => !o)}
      >
        <span className="tasks__ms-summary">{summary}</span>
        <ChevronDownIcon
          size={13}
          className={`tasks__ms-caret${open ? " tasks__ms-caret--open" : ""}`}
        />
      </button>
      {open && (
        <div className="tasks__ms-pop" role="group" aria-label={noun}>
          {options.map((o) => (
            <label
              key={o.value}
              className="tasks__ms-opt"
              data-testid={`${testId}-opt-${o.value}`}
            >
              <input
                type="checkbox"
                className="tasks__ms-check"
                checked={selected.has(o.value)}
                onChange={() => toggle(o.value)}
              />
              <span className="tasks__ms-opt-label">{o.label}</span>
              {o.count !== undefined && (
                <span
                  className="tasks__ms-count"
                  data-testid={`${testId}-count-${o.value}`}
                >
                  {o.count}
                </span>
              )}
            </label>
          ))}
        </div>
      )}
    </div>
  );
}
