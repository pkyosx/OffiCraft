// FilterPanel — the 篩選 shell of a list page: a funnel button (with a count of
// the conditions in force) beside the page's actions. The funnel opens the
// fields in a dropdown panel anchored under it (the 顯示方式 panel's look), or a
// bottom sheet on a phone. It always starts closed, on every visit.
//
// Two rules hold:
//   1. Every field takes effect the moment it changes — there is no Apply. The
//      list behind an open panel is already the filtered one.
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
  useLayoutEffect,
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
import { FunnelIcon } from "./icons";
import "./filter-panel.css";

/** True inside the filter panel / sheet. A dropdown there must float its
 * popover above the layer instead of being clipped by the scrolling box. */
export const FilterLayerContext = createContext(false);

/** One labelled field of the panel's grid. */
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
  summary,
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
  /** Muted text at the row's left end (「5 個任務」). */
  summary?: ReactNode;
  /** `activeCount` is the number of fields holding a condition, as the host
   * counts them; it is the funnel's badge. */
  collapse: { title: string; activeCount: number };
}) {
  return (
    <CollapsedFilterPanel
      testId={testId}
      clearLabel={clearLabel}
      onClear={onClear}
      actions={actions}
      summary={summary}
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
  summary,
  title,
  activeCount,
  children,
}: {
  testId: string;
  clearLabel: string;
  onClear?: () => void;
  actions?: ReactNode;
  summary?: ReactNode;
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
      {summary != null && (
        <span className="filter-panel__summary" data-testid={`${testId}-summary`}>
          {summary}
        </span>
      )}
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
          trigger={triggerRef}
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

const ANCHOR_GAP = 4;
const VIEWPORT_EDGE = 8;
const ANCHORED_WIDTH = 440;

/** The desktop panel hangs under the funnel with its right edge on the
 * funnel's. It is portalled and `fixed` because the host page's list is a
 * scroll box the panel would otherwise be clipped by (or would scroll). */
function useAnchoredBox(
  on: boolean,
  trigger: RefObject<HTMLButtonElement | null>,
): CSSProperties | undefined {
  const [box, setBox] = useState<CSSProperties | undefined>(undefined);
  useLayoutEffect(() => {
    if (!on) return;
    const place = () => {
      const r = trigger.current?.getBoundingClientRect();
      if (!r) return;
      const width = Math.min(ANCHORED_WIDTH, window.innerWidth - 2 * VIEWPORT_EDGE);
      const left = Math.max(VIEWPORT_EDGE, Math.min(r.right - width, window.innerWidth - VIEWPORT_EDGE - width));
      const top = r.bottom + ANCHOR_GAP;
      setBox({ left, top, width, maxHeight: Math.max(0, window.innerHeight - top - VIEWPORT_EDGE) });
    };
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [on, trigger]);
  return box;
}

/** The phone layer: a sheet rising from the bottom over a dimmed backdrop, a
 * title row with 完成, and a scrolling body. The caller owns focus and Esc. */
export function BottomSheet({
  testId,
  title,
  rootRef,
  onClose,
  footer,
  children,
}: {
  testId: string;
  title: string;
  rootRef: RefObject<HTMLDivElement>;
  onClose: () => void;
  footer?: ReactNode;
  children: ReactNode;
}) {
  const { t } = useI18n();
  const visible = useVisibleBox(true);
  const titleId = useId();
  const closeOnBackdrop = (e: MouseEvent) => {
    if (e.target === e.currentTarget) onClose();
  };
  return (
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
          <button type="button" className="filter-panel__done" data-testid={`${testId}-done`} onClick={onClose}>
            {t.common.filterDone}
          </button>
        </div>
        <div className="filter-panel__sheet-body">{children}</div>
        {footer}
      </div>
    </div>
  );
}

function FilterLayer({
  testId,
  title,
  clearLabel,
  onClear,
  applied,
  onClose,
  trigger,
  children,
}: {
  testId: string;
  title: string;
  clearLabel: string;
  onClear?: () => void;
  applied: string | null;
  onClose: () => void;
  trigger: RefObject<HTMLButtonElement | null>;
  children: ReactNode;
}) {
  const mobile = useIsMobile();
  const anchored = useAnchoredBox(!mobile, trigger);
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

  // Focus goes in, and comes back to the funnel unless the click that closed
  // the panel put it somewhere else. The root, not the first field: on a phone
  // a focused field raises the keyboard.
  useEffect(() => {
    rootRef.current?.focus();
    const back = trigger.current;
    return () => {
      const now = document.activeElement;
      if (now && now !== document.body) return;
      if (back?.isConnected) back.focus({ preventScroll: true });
    };
  }, [trigger]);

  // The funnel is left out: its own click toggles the panel shut.
  useEffect(() => {
    if (mobile) return;
    const onDocDown = (e: globalThis.MouseEvent) => {
      const target = e.target as Node;
      if (rootRef.current?.contains(target) || trigger.current?.contains(target)) return;
      close();
    };
    document.addEventListener("mousedown", onDocDown, true);
    return () => document.removeEventListener("mousedown", onDocDown, true);
  });

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
        <BottomSheet testId={testId} title={title} rootRef={rootRef} onClose={close} footer={footer}>
          {grid}
        </BottomSheet>
      ) : (
        <div
          ref={rootRef}
          className="filter-panel__pop"
          style={anchored}
          role="dialog"
          aria-labelledby={titleId}
          tabIndex={-1}
          data-testid={`${testId}-pop`}
        >
          <div id={titleId} className="filter-panel__pop-title">
            {title}
          </div>
          {grid}
          {footer}
        </div>
      )}
    </FilterLayerContext.Provider>,
    document.body,
  );
}
