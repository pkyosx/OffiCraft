import { useEffect, useRef } from "react";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { MAX_AVATAR_POOL_ITEMS, type PoolAvatarKind } from "../lib/themeBundle";

export function ThemeAvatarPoolModal({
  kind,
  title,
  hint,
  pool,
  addLabel,
  replaceLabel,
  removeLabel,
  clearLabel,
  closeLabel,
  doneLabel,
  emptyLabel,
  error,
  onAdd,
  onReplace,
  onRemove,
  onClear,
  onClose,
}: {
  kind: PoolAvatarKind;
  title: string;
  hint: string;
  pool: string[];
  addLabel: string;
  replaceLabel: string;
  removeLabel: string;
  clearLabel: string;
  closeLabel: string;
  doneLabel: string;
  emptyLabel: string;
  error?: string;
  onAdd: () => void;
  onReplace: (index: number) => void;
  onRemove: (index: number) => void;
  onClear: () => void;
  onClose: () => void;
}) {
  const rootRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const titleId = `theme-avatar-pool-${kind}-title`;

  // Esc goes through the shared layer stack, never a listener of our own:
  // `lib/escapeLayers.ts` is the ONLY module allowed to bind window keydown
  // (see frontend/CLAUDE.md). The ref is the surface root, so nesting — not
  // registration order — decides whether Esc reaches us.
  useEscapeLayer(onClose, rootRef);

  const focusable = () => {
    const dialog = dialogRef.current;
    if (!dialog) return [];
    return Array.from(
      dialog.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ),
    );
  };

  useEffect(() => {
    focusable()[0]?.focus();
    // Mount-only: refocusing on every pool edit would yank the caret away from
    // whichever control the owner just used.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // The focus trap stays element-level. It is about Tab, not Esc, so it owes
  // the layer stack nothing — and bound here it only ever sees keys pressed
  // while focus is already inside this dialog.
  function onTabKeyDown(event: React.KeyboardEvent<HTMLDivElement>) {
    if (event.key !== "Tab") return;
    const items = focusable();
    if (items.length === 0) return;
    const first = items[0];
    const last = items[items.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  return (
    <div
      ref={rootRef}
      className="ts-avatar-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      onKeyDown={onTabKeyDown}
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div ref={dialogRef} className="ts-avatar-modal__box">
        <div className="ts-avatar-modal__header">
          <div>
            <h2 id={titleId} className="ts-avatar-modal__title">
              {title}
            </h2>
            <div className="ts-wording-sub">{hint}</div>
          </div>
          <button
            type="button"
            className="doc-btn"
            aria-label={`${closeLabel} ${title}`}
            onClick={onClose}
          >
            ×
          </button>
        </div>

        {pool.length === 0 && (
          <div className="ts-avatar-modal__empty">{emptyLabel}</div>
        )}
        <div className="ts-avatar-grid">
          {pool.map((src, index) => (
            <div key={`${kind}-${index}`} className="ts-avatar-grid__item">
              <img src={src} alt="" draggable={false} />
              <div className="ts-avatar-grid__actions">
                <button
                  type="button"
                  className="doc-btn"
                  aria-label={`${replaceLabel} ${index + 1}`}
                  onClick={() => onReplace(index)}
                >
                  {replaceLabel}
                </button>
                <button
                  type="button"
                  className="doc-btn"
                  aria-label={`${removeLabel} ${index + 1}`}
                  onClick={() => onRemove(index)}
                >
                  {removeLabel}
                </button>
              </div>
            </div>
          ))}
          <button
            type="button"
            className="ts-avatar-grid__add"
            disabled={pool.length >= MAX_AVATAR_POOL_ITEMS}
            onClick={onAdd}
          >
            <span aria-hidden="true">＋</span>
            <span>{addLabel}</span>
          </button>
        </div>

        {error && (
          <div className="set-error" role="alert">
            {error}
          </div>
        )}
        <div className="ts-avatar-modal__footer">
          <span className="ts-wording-sub">
            {pool.length} / {MAX_AVATAR_POOL_ITEMS}
          </span>
          <div className="ts-avatar-modal__footer-actions">
            {pool.length > 0 && (
              <button type="button" className="doc-btn" onClick={onClear}>
                {clearLabel}
              </button>
            )}
            <button type="button" className="btn btn--primary" onClick={onClose}>
              {doneLabel}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
