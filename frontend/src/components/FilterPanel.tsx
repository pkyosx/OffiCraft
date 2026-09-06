// FilterPanel — the 篩選 shell both list pages wear (T-93 round 2).
//
// WHAT THE OWNER ASKED FOR, AND WHY THE SHAPE IS NOT NEGOTIABLE HERE
// ──────────────────────────────────────────────────────────────────────────
// Round 1 put the filters on a permanently-visible 篩選列 that took effect on
// every keystroke. owner 2026-09-06 rejected that twice, and the second time he
// sent a picture of the affordance he wanted from another of our own products
// (c-7496afccb304 — the Master B/L List filter):
//
//   「我想像是可以出現類似這樣的filter圖示 / 點開來後,上面會出現一個filter的
//     面板,可以選擇 cancel or apply,按了面板就又會再消失」
//   「按搜尋時不要再跳出新modal」                      (c-3b5a0aa66550)
//
// Four things follow from that, and each is load-bearing:
//
//   1. 🔴 IT IS NOT A MODAL. The panel expands IN THE PAGE and pushes the list
//      down. No scrim, no fixed positioning, no z-index, nothing over the
//      content. He said this in his own words after seeing an overlay; a future
//      change that floats this panel is reversing a decision, not tidying CSS.
//   2. THE FIELDS ARE A DRAFT. Typing and ticking change NOTHING until Apply.
//      That is the whole answer to「每次都要全部都撈回來才濾不合理」— nothing is
//      fetched or narrowed per keystroke, so there is nothing to throttle.
//   3. BOTH BUTTONS CLOSE THE PANEL. Cancel discards the draft, Apply commits
//      it; either way the panel collapses (「按了面板就又會再消失」).
//   4. WHAT IS APPLIED IS VISIBLE WHILE THE PANEL IS SHUT. The reference has a
//      「N Results · Filtered By: <chip ×> · Clear All」 strip, and it is the
//      part that stops a collapsed panel from hiding a filter that is silently
//      still narrowing the list.
//
// This component owns the SHELL only — header row, the expand, the buttons, the
// summary strip. The FIELDS are the host page's, passed as children, because
// 請示 and 任務 filter on different things and a shared field set would have to
// be widened for every page that ever adopts this.

import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import { CloseIcon, FunnelIcon } from "./icons";
import "./filter-panel.css";

/** One applied filter, as it appears in the 「已篩選」 strip. */
export interface FilterChip {
  /** Stable across renders; also the React key. */
  key: string;
  /** What the owner reads, e.g. 「編號：T-93」. */
  label: string;
  /** Drop just this one axis and re-run. */
  onRemove: () => void;
}

export function FilterPanel({
  title,
  count,
  open,
  onOpenChange,
  chips,
  onClearAll,
  onApply,
  onCancel,
  applyDisabled = false,
  testId = "filter-panel",
  children,
}: {
  /** The list's own name, kept on the header row beside the funnel. */
  title: string;
  /** How many rows the CURRENT filters leave. Shown on the summary strip so a
   * narrowed list says how narrow it is; null hides the count (a page that
   * cannot state one honestly must not invent one). */
  count: number | null;
  open: boolean;
  onOpenChange: (next: boolean) => void;
  /** Applied filters. Empty = no summary strip at all. */
  chips: FilterChip[];
  onClearAll: () => void;
  /** Commit the draft. The panel closes itself — the host does not have to. */
  onApply: () => void;
  /** Discard the draft. The host restores its draft from the applied state. */
  onCancel: () => void;
  applyDisabled?: boolean;
  testId?: string;
  /** The draft fields. */
  children: ReactNode;
}) {
  const { t } = useI18n();
  const filtered = chips.length > 0;

  return (
    <div className="filter-panel" data-testid={testId}>
      {/* ── the panel, ABOVE everything, pushing the page down ──────────────
          Rendered before the header row on purpose: that is the order in the
          owner's reference, and it means the fields appear between the reader
          and nothing — the list they are about to narrow stays below, where a
          growing panel slides it down rather than covering it. */}
      {open && (
        <div className="filter-panel__form" data-testid={`${testId}-form`}>
          <div className="filter-panel__fields">{children}</div>
          <div className="filter-panel__actions">
            <button
              type="button"
              className="filter-panel__btn"
              data-testid={`${testId}-cancel`}
              onClick={() => {
                onCancel();
                onOpenChange(false);
              }}
            >
              {t.filterPanel.cancel}
            </button>
            <button
              type="button"
              className="filter-panel__btn filter-panel__btn--apply"
              data-testid={`${testId}-apply`}
              disabled={applyDisabled}
              onClick={() => {
                onApply();
                onOpenChange(false);
              }}
            >
              {t.filterPanel.apply}
            </button>
          </div>
        </div>
      )}

      {/* ── what is applied, while the panel is shut ──────────────────────
          Variant D keeps it as its own row but drops the box; B folds it into
          the header row below; C shows only a count on the funnel. Whichever
          survives, the invariant is the same: a collapsed panel must never be
          able to hide a filter that is still narrowing the list. */}
      {filtered && (
        <div
          className="filter-panel__summary"
          data-testid={`${testId}-summary`}
        >
          <>
          {count !== null && (
            <span className="filter-panel__count">
              {t.filterPanel.results(count)}
            </span>
          )}
          <span className="filter-panel__summary-label">
            {t.filterPanel.filteredBy}
          </span>
          {chips.map((chip) => (
            <span
              key={chip.key}
              className="filter-panel__chip"
              data-testid={`${testId}-chip`}
            >
              {chip.label}
              <button
                type="button"
                className="filter-panel__chip-x"
                data-testid={`${testId}-chip-x`}
                aria-label={t.filterPanel.removeChip(chip.label)}
                onClick={chip.onRemove}
              >
                <CloseIcon size={13} />
              </button>
            </span>
          ))}
          <button
            type="button"
            className="filter-panel__clear"
            data-testid={`${testId}-clear`}
            onClick={onClearAll}
          >
            {t.filterPanel.clearAll}
          </button>
        </>
        </div>
      )}

      {/* ── the list's own header row ─────────────────────────────────────── */}
      <div className="filter-panel__header">
        <span className="filter-panel__title">{title}</span>
        <button
          type="button"
          className={`filter-panel__toggle${
            filtered ? " filter-panel__toggle--on" : ""
          }`}
          aria-expanded={open}
          data-testid={`${testId}-toggle`}
          onClick={() => onOpenChange(!open)}
        >
          <FunnelIcon size={14} />
          {t.filterPanel.filter}
        </button>
      </div>
    </div>
  );
}
