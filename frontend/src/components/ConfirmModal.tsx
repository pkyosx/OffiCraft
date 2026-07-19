import { useEffect, type ReactNode } from "react";
import "./confirm-modal.css";

/**
 * Minimal reusable centered confirm modal (overlay + card), dark-themed to
 * match the existing dialog language (mon-confirm / machine-picker) without
 * coupling to their page-scoped stylesheets. No third-party lib.
 *
 * Behavior contract:
 *   - Esc or the cancel button closes (both are ignored while `busy` — a
 *     committed action must not lose its pending state mid-flight).
 *   - The confirm button runs the caller's action; the caller decides whether
 *     to close (success) or surface `error` and keep the modal open.
 */
export function ConfirmModal({
  body,
  error,
  cancelLabel,
  confirmLabel,
  busy = false,
  danger = false,
  onCancel,
  onConfirm,
  testId,
  confirmTestId,
}: {
  body: ReactNode;
  /** Honest inline failure line; the modal stays open so the user can retry. */
  error?: string | null;
  cancelLabel: string;
  confirmLabel: string;
  busy?: boolean;
  /** Red-accented confirm for destructive actions. */
  danger?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  testId?: string;
  confirmTestId?: string;
}) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape" && !busy) onCancel();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [busy, onCancel]);

  return (
    <div
      className="confirm-modal"
      data-testid={testId}
      role="dialog"
      aria-modal="true"
    >
      <div
        className={`confirm-modal__box${danger ? " confirm-modal__box--danger" : ""}`}
      >
        <div className="confirm-modal__body">{body}</div>
        {error && <div className="confirm-modal__error">{error}</div>}
        <div className="confirm-modal__actions">
          <button
            type="button"
            className="confirm-modal__btn"
            disabled={busy}
            onClick={onCancel}
          >
            {cancelLabel}
          </button>
          <button
            type="button"
            className={`confirm-modal__btn${danger ? " confirm-modal__btn--danger" : " confirm-modal__btn--accent"}`}
            data-testid={confirmTestId}
            disabled={busy}
            onClick={onConfirm}
          >
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
