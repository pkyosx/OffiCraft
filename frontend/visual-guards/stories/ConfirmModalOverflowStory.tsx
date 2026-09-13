// CT story (T-192) — the SHARED <ConfirmModal> carrying the 強制結案 body, at a
// window short enough that the box cannot fit.
//
// The real <ConfirmModal> is mounted (not a hand-copied shell), so the thing
// measured is the shipped component against its own confirm-modal.css. The only
// thing staged here is WHICH body it carries, and that comes from the real i18n
// dictionaries rather than invented lorem: the defect is a property of THIS
// paragraph's height (499.2px en / 394.6px zh, measured in a real browser), and
// a fixture that made up its own text would be answering a different question.
// The body markup mirrors TaskCard.tsx's force-done form — same classes, so
// tasks.css's own rules for it are in the measurement too.
//
// The SHORT variant exists to separate two claims the fix must both satisfy:
//   • LONG  — the box must be capped AND scrollable, i.e. the paragraph is
//             reachable at a short window.
//   • SHORT — a dialog whose content fits must be BYTE-FOR-BYTE unaffected: no
//             scrollbar, no cap binding, nothing to scroll. Worst case for
//             every other caller of this shared shell is "same as today", and
//             this is the arm that measures it.
import "../../src/components/tasks.css";
import "../../src/styles/global.css";
import { ConfirmModal } from "../../src/components/ConfirmModal";
import { en } from "../../src/i18n/locales/en";
import { zh } from "../../src/i18n/locales/zh";

/** The real shipped copy — the paragraph whose whole job is to be read before
 * an irreversible action. English is the taller of the two (499.2 vs 394.6), so
 * it is the arm the guard runs. */
export const FORCE_DONE_BODY_EN = en.tasks.forceDoneConfirmBody;
export const FORCE_DONE_BODY_ZH = zh.tasks.forceDoneConfirmBody;

export const LONG_TESTID = "force-done-confirm";
export const SHORT_TESTID = "short-confirm";

export function ConfirmModalOverflowStory({
  locale = "en",
}: {
  locale?: "en" | "zh";
}) {
  const d = locale === "zh" ? zh : en;
  return (
    <ConfirmModal
      testId={LONG_TESTID}
      confirmTestId="force-done-confirm-btn"
      danger
      cancelLabel={d.common.cancel}
      confirmLabel={d.tasks.forceDoneConfirm}
      onCancel={() => {}}
      onConfirm={() => {}}
      body={
        <div className="task-card__force-done-form">
          <div data-testid="force-done-copy">
            {d.tasks.forceDoneConfirmBody}
          </div>
          <label className="task-card__force-done-label" htmlFor="force-reason">
            {d.tasks.forceDoneReasonLabel}
          </label>
          <textarea
            id="force-reason"
            className="task-card__force-done-reason-input"
            data-testid="force-done-reason"
            rows={3}
            defaultValue=""
            placeholder={d.tasks.forceDoneReasonPlaceholder}
          />
        </div>
      }
    />
  );
}

/** One short line — the shape every OTHER caller of this shell passes (a
 * delete confirm, a cost reset, an expire). */
export function ConfirmModalShortStory() {
  return (
    <ConfirmModal
      testId={SHORT_TESTID}
      confirmTestId="short-confirm-btn"
      cancelLabel={en.common.cancel}
      confirmLabel={en.common.apply}
      onCancel={() => {}}
      onConfirm={() => {}}
      body={<div data-testid="short-copy">Delete this?</div>}
    />
  );
}
