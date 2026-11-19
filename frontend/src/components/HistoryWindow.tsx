// The version-history window: a list of retained versions, and the reader one
// click deeper (the version's identity top-left, the 內容 / 差異 toggle
// top-right, 返回版本列表 / 關閉 / 還原 along the foot). The documents'
// history (DocumentHistoryEntry + DocumentHistoryModal) and the 檔案庫's both
// draw it from here, so the two cannot grow apart.
//
// Restore is DESTRUCTIVE, so it always goes through ConfirmModal; a failure
// shows the SERVER's own message and leaves both dialogs open (closing the
// reader would take the reason with it); while it is in flight Esc and the
// backdrop are inert — a committed destructive action must not lose its dialog
// mid-request.

import { useRef, useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import { ApiError } from "../api/errors";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { ConfirmModal } from "./ConfirmModal";
import { CloseIcon, LayersIcon } from "./icons";
// The shell sheet, plus settings.css for the shared atoms it wears — `.doc-btn`
// and the `.doc-hist__*` list rows.
import "./doc-hist-modal.css";
import "./settings.css";

export type HistoryPane = "content" | "diff";

export function HistoryListWindow({
  title,
  note,
  active = true,
  onClose,
  children,
}: {
  title: string;
  note: ReactNode;
  /** False while a reader opened from this list is on screen. */
  active?: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  const { t } = useI18n();
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onClose, rootRef, active);
  return (
    <div
      ref={rootRef}
      className="doc-hist-modal"
      data-testid="doc-history-list"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onClick={onClose}
    >
      <div className="doc-hist-modal__panel" onClick={(e) => e.stopPropagation()}>
        <div className="doc-hist-modal__header">
          <span className="doc-hist__title">
            <LayersIcon size={15} className="doc-hist__icon" />
            <span>{title}</span>
          </span>
          <div className="doc-hist-modal__actions">
            <button
              type="button"
              className="doc-hist-modal__close"
              data-testid="doc-history-list-close"
              aria-label={t.settings.historyClose}
              onClick={onClose}
            >
              <CloseIcon size={16} />
            </button>
          </div>
        </div>
        <div className="doc-hist-modal__body">
          <div className="doc-hist__note">{note}</div>
          {children}
        </div>
      </div>
    </div>
  );
}

export type HistoryRestore = {
  label: string;
  /** The version cannot be restored; the host says why in `belowBody`. */
  disabled?: boolean;
  confirmBody: string;
  confirmLabel: string;
  /** Rejects on failure; the window shows the server's message and stays. */
  run: () => Promise<void>;
};

export function HistoryReaderWindow({
  ident,
  pane,
  onPane,
  belowBody,
  onBack,
  onClose,
  restore,
  children,
}: {
  ident: ReactNode;
  /** The current pane; absent ⇒ there is nothing to compare and no toggle. */
  pane?: HistoryPane;
  onPane?: (pane: HistoryPane) => void;
  belowBody?: ReactNode;
  /** Back to the list this reader was opened from; absent where there is none. */
  onBack?: () => void;
  onClose: () => void;
  /** Absent ⇒ no restore control: the subject has no restore verb. */
  restore?: HistoryRestore;
  children: ReactNode;
}) {
  const { t } = useI18n();
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [restoreError, setRestoreError] = useState<string | null>(null);

  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(() => {
    if (!busy) onClose();
  }, rootRef);

  async function commitRestore() {
    if (!restore) return;
    setBusy(true);
    setRestoreError(null);
    try {
      await restore.run();
      setConfirming(false);
      onClose();
    } catch (e) {
      setRestoreError(
        e instanceof ApiError && e.serverMessage ? e.serverMessage : t.settings.historyRestoreError,
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      ref={rootRef}
      className="doc-hist-modal"
      data-testid="doc-history-modal"
      role="dialog"
      aria-modal="true"
      aria-label={t.settings.historyTitle}
      onClick={() => {
        if (!busy && !confirming) onClose();
      }}
    >
      <div className="doc-hist-modal__panel" onClick={(e) => e.stopPropagation()}>
        <div className="doc-hist-modal__header">
          <div className="doc-hist-modal__ident">{ident}</div>
          <div className="doc-hist-modal__actions">
            {pane !== undefined && onPane && (
              <div className="doc-hist-modal__tabs" role="group" aria-label={t.settings.historyPaneLabel}>
                {(["content", "diff"] as HistoryPane[]).map((which) => (
                  <button
                    key={which}
                    type="button"
                    className={`doc-hist-modal__tab${pane === which ? " doc-hist-modal__tab--on" : ""}`}
                    data-testid={`doc-history-pane-${which}`}
                    aria-pressed={pane === which}
                    onClick={() => onPane(which)}
                  >
                    {which === "content" ? t.settings.historyPaneContent : t.settings.historyPaneDiff}
                  </button>
                ))}
              </div>
            )}
            <button
              type="button"
              className="doc-hist-modal__close"
              data-testid="doc-history-modal-close"
              aria-label={t.settings.historyClose}
              onClick={onClose}
            >
              <CloseIcon size={16} />
            </button>
          </div>
        </div>

        <div className="doc-hist-modal__body" data-pane={pane ?? "content"}>
          {children}
        </div>

        {belowBody}

        {(onBack || restore) && (
          <div className="doc-hist-modal__footer">
            {onBack && (
              <button
                type="button"
                className="doc-btn doc-hist-modal__back"
                data-testid="doc-history-modal-back"
                onClick={onBack}
              >
                {t.settings.historyBack}
              </button>
            )}
            {restore && (
              <button
                type="button"
                className="doc-btn doc-hist-modal__restore"
                data-testid="doc-history-modal-restore"
                disabled={restore.disabled}
                onClick={() => {
                  setConfirming(true);
                  setRestoreError(null);
                }}
              >
                {restore.label}
              </button>
            )}
          </div>
        )}
      </div>

      {confirming && restore && (
        <ConfirmModal
          testId="doc-history-restore-confirm"
          confirmTestId="doc-history-restore-confirm-btn"
          danger
          body={restore.confirmBody}
          error={restoreError}
          busy={busy}
          cancelLabel={t.settings.cancel}
          confirmLabel={restore.confirmLabel}
          onCancel={() => {
            setConfirming(false);
            setRestoreError(null);
          }}
          onConfirm={() => void commitRestore()}
        />
      )}
    </div>
  );
}
