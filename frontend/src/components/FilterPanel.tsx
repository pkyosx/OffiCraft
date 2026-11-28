// FilterPanel — the 篩選 shell of a list page. Two shapes:
//
//   · inline (no `collapse` prop) — the fields sit in a row above the list,
//     always visible. 傳承頁 uses this shape.
//   · collapsed (`collapse` given) — the row holds only a funnel button (with a
//     count of the conditions in force) beside the page's actions. The funnel
//     opens the fields in a centred dialog, or a bottom sheet on a phone. 任務頁
//     and 請示頁 use this shape. It always starts closed, on every visit.
//
// Two rules hold in both shapes:
//   1. Every field takes effect the moment it changes — there is no Apply. The
//      list behind an open dialog is already the filtered one.
//   2. The ID field commits on Enter or blur only (IdFilterInput): a committed
//      id is a SERVER request on both hosts, so a commit per keystroke would be
//      a fetch per keystroke.
//
// The fields are the host page's, passed as children, because each page
// filters on different things; so is the judgement of what counts as a
// condition (`onClear`, `collapse.activeCount`).

import {
  createContext,
  useEffect,
  useId,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent,
  type ReactNode,
  type RefObject,
} from "react";
import { createPortal } from "react-dom";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { useIsMobile } from "../hooks/useIsMobile";
import { CloseIcon, FunnelIcon } from "./icons";
import "./confirm-modal.css";
import "./filter-panel.css";

/** True inside the filter dialog / sheet. A dropdown there must float its
 * popover above the layer instead of being clipped by the scrolling box. */
export const FilterLayerContext = createContext(false);

/** One labelled field of the collapsed panel's grid. */
export function FilterField({
  label,
  wide = false,
  children,
}: {
  label: string;
  /** Spans both grid columns (the ID field). */
  wide?: boolean;
  children: ReactNode;
}) {
  return (
    <div className={`filter-panel__field${wide ? " filter-panel__field--wide" : ""}`}>
      <span className="filter-panel__label">{label}</span>
      {children}
    </div>
  );
}

export function FilterPanel({
  testId = "filter-panel",
  clearLabel,
  onClear,
  children,
  actions,
  collapse,
}: {
  testId?: string;
  /** Wording for the 清除篩選 control. Each page supplies its own. */
  clearLabel: string;
  /** Pass it ONLY while something is narrowing the list, `undefined`
   * otherwise — its presence is the decision to show 清除篩選. The host owns
   * that judgement because only it knows what its axes mean (任務頁's default
   * status set narrows). */
  onClear?: () => void;
  children: ReactNode;
  /** Controls that are not filters (顯示方式), held at the row's right end,
   * outside what 清除篩選 clears. */
  actions?: ReactNode;
  /** Opt into the funnel + dialog shape. `activeCount` is the number of fields
   * holding a condition, as the host counts them; it is the funnel's badge. */
  collapse?: { title: string; activeCount: number };
}) {
  if (!collapse) {
    return (
      <div className="filter-panel" data-testid={testId}>
        <div className="filter-panel__fields" data-testid={`${testId}-fields`}>
          {children}
          {onClear && (
            <button
              type="button"
              className="filter-panel__clear"
              data-testid={`${testId}-clear`}
              onClick={onClear}
            >
              {clearLabel}
            </button>
          )}
          {actions && <div className="filter-panel__actions">{actions}</div>}
        </div>
      </div>
    );
  }
  return (
    <CollapsedFilterPanel
      testId={testId}
      clearLabel={clearLabel}
      onClear={onClear}
      actions={actions}
      title={collapse.title}
      activeCount={collapse.activeCount}
    >
      {children}
    </CollapsedFilterPanel>
  );
}

function CollapsedFilterPanel({
  testId,
  clearLabel,
  onClear,
  actions,
  title,
  activeCount,
  children,
}: {
  testId: string;
  clearLabel: string;
  onClear?: () => void;
  actions?: ReactNode;
  title: string;
  activeCount: number;
  children: ReactNode;
}) {
  const { t, msg } = useI18n();
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const label =
    activeCount > 0 ? `${t.common.filterButton} · ${activeCount}` : t.common.filterButton;

  return (
    <div className="filter-panel" data-testid={testId}>
      <div className="filter-panel__bar">
        <button
          ref={triggerRef}
          type="button"
          className={`filter-panel__trigger${activeCount > 0 ? " filter-panel__trigger--active" : ""}`}
          aria-haspopup="dialog"
          aria-expanded={open}
          aria-label={label}
          title={label}
          data-testid={`${testId}-toggle`}
          onClick={() => setOpen((v) => !v)}
        >
          <FunnelIcon size={14} aria-hidden="true" />
          {activeCount > 0 && (
            <span className="filter-panel__badge" aria-hidden="true" data-testid={`${testId}-count`}>
              {activeCount}
            </span>
          )}
        </button>
        {actions}
      </div>
      {open && (
        <FilterLayer
          testId={testId}
          title={title}
          clearLabel={clearLabel}
          onClear={onClear}
          applied={activeCount > 0 ? msg.filterAppliedCount(activeCount) : null}
          onClose={() => setOpen(false)}
          returnFocusTo={triggerRef}
        >
          {children}
        </FilterLayer>
      )}
    </div>
  );
}

/** The part of the window that is on screen: with the on-screen keyboard up it
 * is shorter than the window, and a sheet sized to the window would keep its
 * 完成 under the keyboard (same as ChatGroupPanel's sheet). */
function useVisibleBox(on: boolean): CSSProperties | undefined {
  const [box, setBox] = useState<CSSProperties | undefined>(undefined);
  useEffect(() => {
    const vv = window.visualViewport;
    if (!on || !vv) return;
    const fit = () => setBox({ top: vv.offsetTop, height: vv.height, bottom: "auto" });
    fit();
    vv.addEventListener("resize", fit);
    vv.addEventListener("scroll", fit);
    return () => {
      vv.removeEventListener("resize", fit);
      vv.removeEventListener("scroll", fit);
    };
  }, [on]);
  return box;
}

function FilterLayer({
  testId,
  title,
  clearLabel,
  onClear,
  applied,
  onClose,
  returnFocusTo,
  children,
}: {
  testId: string;
  title: string;
  clearLabel: string;
  onClear?: () => void;
  applied: string | null;
  onClose: () => void;
  returnFocusTo: RefObject<HTMLButtonElement | null>;
  children: ReactNode;
}) {
  const { t } = useI18n();
  const mobile = useIsMobile();
  const visible = useVisibleBox(mobile);
  const rootRef = useRef<HTMLDivElement>(null);
  const titleId = useId();

  // A focused ID field holding an uncommitted id commits on blur, and an
  // unmounting input gets no blur event — so blur it before the layer goes.
  const close = () => {
    const active = document.activeElement;
    if (active instanceof HTMLElement && rootRef.current?.contains(active)) active.blur();
    onClose();
  };
  useEscapeLayer(close, rootRef);

  // Focus goes in and comes back (the ThemeAppearanceDialog contract). The
  // root, not the first field: on a phone a focused field raises the keyboard.
  useEffect(() => {
    rootRef.current?.focus();
    const back = returnFocusTo.current;
    return () => {
      if (back?.isConnected) back.focus({ preventScroll: true });
    };
  }, [returnFocusTo]);

  const closeOnBackdrop = (e: MouseEvent) => {
    if (e.target === e.currentTarget) close();
  };

  const footer = (applied || onClear) && (
    <div className="filter-panel__foot">
      <span className="filter-panel__applied" data-testid={`${testId}-applied`}>
        {applied}
      </span>
      {onClear && (
        <button
          type="button"
          className="filter-panel__clear"
          data-testid={`${testId}-clear`}
          onClick={onClear}
        >
          {clearLabel}
        </button>
      )}
    </div>
  );
  const grid = (
    <div className="filter-panel__grid" data-testid={`${testId}-fields`}>
      {children}
    </div>
  );

  return createPortal(
    <FilterLayerContext.Provider value={true}>
      {mobile ? (
        <div
          className="filter-panel__sheet-backdrop"
          style={visible}
          data-testid={`${testId}-backdrop`}
          onClick={closeOnBackdrop}
        >
          <div
            ref={rootRef}
            className="filter-panel__sheet"
            role="dialog"
            aria-modal="true"
            aria-labelledby={titleId}
            tabIndex={-1}
            data-testid={`${testId}-sheet`}
          >
            <div className="filter-panel__sheet-head">
              <h2 id={titleId} className="filter-panel__title">
                {title}
              </h2>
              <button
                type="button"
                className="filter-panel__done"
                data-testid={`${testId}-done`}
                onClick={close}
              >
                {t.common.filterDone}
              </button>
            </div>
            <div className="filter-panel__sheet-body">{grid}</div>
            {footer}
          </div>
        </div>
      ) : (
        <div
          className="confirm-modal filter-panel__modal"
          data-testid={`${testId}-backdrop`}
          onClick={closeOnBackdrop}
        >
          <div
            ref={rootRef}
            className="confirm-modal__box filter-panel__box"
            role="dialog"
            aria-modal="true"
            aria-labelledby={titleId}
            tabIndex={-1}
            data-testid={`${testId}-dialog`}
          >
            <div className="filter-panel__head">
              <h2 id={titleId} className="filter-panel__title">
                {title}
              </h2>
              <button
                type="button"
                className="filter-panel__close"
                aria-label={t.common.filterClose}
                title={t.common.filterClose}
                data-testid={`${testId}-close`}
                onClick={close}
              >
                <CloseIcon size={15} />
              </button>
            </div>
            {grid}
            {footer}
          </div>
        </div>
      )}
    </FilterLayerContext.Provider>,
    document.body,
  );
}
