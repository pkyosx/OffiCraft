// 外包上限設定 popover (seth-member-2 mockup): 最多雇用 −/＋ stepper + 無限 +
// 完成, PATCHing settings.outsource_max_parallel (-1 = 無限, 0 = 暫停指派).
//
// T-66a8 lifted this out of OutsourcePanel: the trigger is no longer the panel
// head's gear but the 招攬新成員 button at the sidebar's BOTTOM, shown only on
// the 外包 tab (owner mockup 2026-07-18). This component is pure presentation +
// its own draft state; OfficePage owns the open/close state and the
// outside-click dismissal (the popover shares the recruit button's anchor
// wrapper). Testids are UNCHANGED from the old panel gear so the cap behaviour
// keeps its coverage.

import { useState } from "react";
import { useI18n } from "../i18n";
import { BriefcaseIcon } from "./icons";
import "./stepper.css";

export function OutsourceCapPopover({
  maxParallel,
  onSave,
}: {
  /** current cap; null = settings not loaded (draft seeds at 0). -1 = 無限. */
  maxParallel: number | null;
  /** PATCH the cap and, on success, dismiss (OfficePage's close callback). A
   * rejection surfaces as the inline error and keeps the popover open. */
  onSave: (n: number) => Promise<void>;
}) {
  const { t } = useI18n();
  /** Draft cap: >= 1 finite, -1 = 無限 (wire), 0 = paused. */
  const [capDraft, setCapDraft] = useState(maxParallel ?? 0);
  const [capBusy, setCapBusy] = useState(false);
  const [capError, setCapError] = useState(false);

  async function commitCap() {
    setCapBusy(true);
    setCapError(false);
    try {
      await onSave(capDraft);
    } catch (e) {
      console.warn("OutsourceCapPopover: cap save failed", e);
      setCapError(true);
    } finally {
      setCapBusy(false);
    }
  }

  return (
    <div
      className="outsource-panel__pop outsource-panel__pop--up"
      data-testid="outsource-cap-pop"
    >
      <div className="outsource-panel__pop-head">
        <span className="outsource-panel__pop-icon">
          <BriefcaseIcon size={15} />
        </span>
        <span className="outsource-panel__pop-title">
          {t.office.outsource.capTitle}
        </span>
      </div>
      <div className="outsource-panel__pop-hint">
        {t.office.outsource.capHint}
      </div>
      <div className="outsource-panel__pop-label">
        {t.office.outsource.capMaxLabel}
      </div>
      <div className="manual-stepper-row">
        <div className="manual-stepper">
          <button
            type="button"
            className="manual-stepper__btn"
            aria-label={t.office.outsource.capDecrease}
            disabled={capBusy || capDraft === 0}
            data-testid="outsource-cap-dec"
            onClick={() =>
              setCapDraft((n) => (n === -1 ? 0 : Math.max(0, n - 1)))
            }
          >
            −
          </button>
          <span
            className="manual-stepper__value"
            data-testid="outsource-cap-value"
          >
            {capDraft === -1 ? "∞" : capDraft}
          </span>
          <button
            type="button"
            className="manual-stepper__btn"
            aria-label={t.office.outsource.capIncrease}
            disabled={capBusy || capDraft === 20}
            data-testid="outsource-cap-inc"
            onClick={() =>
              setCapDraft((n) => (n === -1 ? 1 : Math.min(20, n + 1)))
            }
          >
            ＋
          </button>
        </div>
        <button
          type="button"
          className={`manual-unlimited${
            capDraft === -1 ? " manual-unlimited--active" : ""
          }`}
          aria-pressed={capDraft === -1}
          disabled={capBusy}
          data-testid="outsource-cap-unlimited"
          onClick={() => setCapDraft((n) => (n === -1 ? 0 : -1))}
        >
          {t.office.outsource.capUnlimited}
        </button>
      </div>
      {capError && (
        <div className="outsource-panel__pop-error">
          {t.office.outsource.capError}
        </div>
      )}
      <div className="outsource-panel__pop-footer">
        <button
          type="button"
          className="doc-btn doc-btn--accent"
          disabled={capBusy}
          data-testid="outsource-cap-save"
          onClick={() => void commitCap()}
        >
          {t.office.outsource.capSave}
        </button>
      </div>
    </div>
  );
}
