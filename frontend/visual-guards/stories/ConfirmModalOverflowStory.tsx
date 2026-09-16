// CT story (T-192) — the SHARED <ConfirmModal> carrying a body taller than the
// window, at a window short enough that the box cannot fit.
//
// The real <ConfirmModal> is mounted (not a hand-copied shell), so the thing
// measured is the shipped component against its own confirm-modal.css. The body
// markup mirrors TaskCard.tsx's force-done form — same classes, so tasks.css's
// own rules for it are in the measurement too.
//
// 🔴 THE BODY IS THIS GUARD'S OWN FIXTURE, NOT PRODUCT COPY. It used to be
// `en.tasks.forceDoneConfirmBody`, on the reasoning that the defect was a
// property of that real paragraph's height. That coupling made the guard die of
// an unrelated edit: the owner's ruling rc-bdfd2fc07305 shortened the
// force-close body to one sentence, which fits the window — and a body that
// fits turns "the box must scroll" into a claim about nothing. What this guard
// is really about is the SHARED shell's behaviour when any caller passes more
// content than the window holds, so the height it needs is its own to keep.
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

const FIXTURE_SENTENCE =
  "This paragraph belongs to the overflow guard and to nothing else: its only job is to be taller than the window, so that what gets measured is the shared confirm shell's own behaviour when a caller hands it more content than the viewport can hold.";

/** Comfortably past the 420px guard window at the box's width — the premise
 * assertion in the spec fails loudly if that ever stops being true. */
export const LONG_BODY = Array.from(
  { length: 8 },
  (_, i) => `(${i + 1}) ${FIXTURE_SENTENCE}`
).join(" ");

export const LONG_TESTID = "overflow-confirm";
export const SHORT_TESTID = "short-confirm";

export function ConfirmModalOverflowStory() {
  return (
    <ConfirmModal
      testId={LONG_TESTID}
      confirmTestId="overflow-confirm-btn"
      danger
      cancelLabel="Cancel"
      confirmLabel="Confirm"
      onCancel={() => {}}
      onConfirm={() => {}}
      body={
        <div className="task-card__force-done-form">
          <div data-testid="overflow-copy">{LONG_BODY}</div>
          <label className="task-card__force-done-label" htmlFor="fixture-reason">
            Reason (optional)
          </label>
          <textarea
            id="fixture-reason"
            className="task-card__force-done-reason-input"
            data-testid="overflow-reason"
            rows={3}
            defaultValue=""
            placeholder="Why?"
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
      cancelLabel="Cancel"
      confirmLabel="Apply"
      onCancel={() => {}}
      onConfirm={() => {}}
      body={<div data-testid="short-copy">Delete this?</div>}
    />
  );
}
