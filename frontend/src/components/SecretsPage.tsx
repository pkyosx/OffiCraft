// 🔴 A VALUE NEVER COMES BACK. No route returns one, so this page has nothing
// to show; the value lives only in the create/edit input until the request is
// sent, and the input is cleared whether the save succeeds or not.

import { useEffect, useRef, useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { useIsMobile } from "../hooks/useIsMobile";
import { useSecrets } from "../hooks/useSecrets";
import { copyText } from "../lib/clipboard";
import { formatAbsolute } from "../lib/dateFormat";
import type { SecretView } from "../types";
import { ConfirmModal } from "./ConfirmModal";
import {
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  KeyIcon,
  PencilIcon,
  TrashIcon,
} from "./icons";
import "./lore.css";
import "./theme-settings.css";
import "./secrets.css";

export function SecretsPage() {
  const { t, msg } = useI18n();
  const d = t.secrets;
  const { secrets, loading, loadError, create, update, remove } =
    useSecrets(d.loadFailed);

  const [adding, setAdding] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<SecretView | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const nowSecs = Math.floor(Date.now() / 1000);
  const isMobile = useIsMobile();

  async function confirmDelete() {
    if (!deleteTarget) return;
    setDeleteBusy(true);
    const err = await remove(deleteTarget.id);
    setDeleteBusy(false);
    if (err) {
      setDeleteError(err);
      return;
    }
    setDeleteTarget(null);
    setDeleteError("");
  }

  return (
    <div className="secrets" data-testid="secrets-page">
      {loadError && (
        <div className="lore__error" data-testid="secrets-load-error">
          {loadError}
        </div>
      )}

      {!loading && secrets.length === 0 && !loadError && (
        <div className="lore__empty" data-testid="secrets-empty">
          {d.empty}
        </div>
      )}

      {secrets.length > 0 && (
        <div
          className={isMobile ? "secrets__stack" : "secrets__card"}
          data-testid="secrets-list"
        >
          {secrets.map((s) =>
            editingId === s.id ? (
              <SecretForm
                key={s.id}
                mode="edit"
                initial={s}
                onCancel={() => setEditingId(null)}
                onSubmit={async (fields) => {
                  const patch: { name?: string; note?: string; value?: string } =
                    {};
                  if (fields.name !== s.name) patch.name = fields.name;
                  if (fields.note !== s.note) patch.note = fields.note;
                  if (fields.value !== "") patch.value = fields.value;
                  const err = await update(s.id, patch);
                  if (!err) setEditingId(null);
                  return err;
                }}
              />
            ) : (
              <SecretRow
                key={s.id}
                secret={s}
                nowSecs={nowSecs}
                stacked={isMobile}
                onEdit={() => setEditingId(s.id)}
                onDelete={() => {
                  setDeleteError("");
                  setDeleteTarget(s);
                }}
              />
            ),
          )}
        </div>
      )}

      {adding ? (
        <div className="secrets__card">
          <SecretForm
            mode="create"
            onCancel={() => setAdding(false)}
            onSubmit={async (fields) => {
              const err = await create({
                name: fields.name,
                value: fields.value,
                note: fields.note,
              });
              if (!err) setAdding(false);
              return err;
            }}
          />
        </div>
      ) : (
        <button
          type="button"
          className="add-entry"
          data-testid="secrets-add"
          onClick={() => setAdding(true)}
        >
          + {d.add}
        </button>
      )}

      {deleteTarget && (
        <ConfirmModal
          body={msg.secretDeleteConfirmBody(deleteTarget.name)}
          error={deleteError || null}
          cancelLabel={d.cancel}
          confirmLabel={d.deleteConfirm}
          busy={deleteBusy}
          danger
          onCancel={() => setDeleteTarget(null)}
          onConfirm={confirmDelete}
          testId="secrets-delete-confirm"
          confirmTestId="secrets-delete-confirm-btn"
        />
      )}
    </div>
  );
}

function SecretRow({
  secret,
  nowSecs,
  onEdit,
  onDelete,
  stacked,
}: {
  secret: SecretView;
  nowSecs: number;
  onEdit: () => void;
  onDelete: () => void;
  stacked: boolean;
}) {
  const { t, msg } = useI18n();
  const d = t.secrets;
  // Same contract as the 傳承 entry id: 已複製 only when the clipboard write
  // actually succeeded, and the timer dies with the row.
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);
  const copiedTimer = useRef<number | null>(null);
  useEffect(
    () => () => {
      if (copiedTimer.current != null) window.clearTimeout(copiedTimer.current);
    },
    [],
  );
  async function copyId() {
    const ok = await copyText(secret.id);
    if (!ok) return;
    setCopied(true);
    if (copiedTimer.current != null) window.clearTimeout(copiedTimer.current);
    copiedTimer.current = window.setTimeout(() => setCopied(false), 1600);
  }

  // Same toggle contract as the 傳承 row (LorePage onRowToggleClick): a click
  // on a control does that control's job only, and ending a text selection
  // does not fold the row.
  function onRowToggleClick(e: React.MouseEvent<HTMLElement>) {
    const hit = (e.target as HTMLElement).closest(
      "button, a, textarea, input, select, [role='button'], [role='dialog']",
    );
    if (hit && hit !== e.currentTarget) return;
    const sel = window.getSelection();
    if (sel && sel.rangeCount > 0 && !sel.isCollapsed) return;
    setExpanded((v) => !v);
  }
  function onRowToggleKeyDown(e: React.KeyboardEvent<HTMLElement>) {
    if (e.target !== e.currentTarget) return;
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      setExpanded((v) => !v);
    }
  }

  const idBadge = (
    <button
      type="button"
      className="lore-row__id-badge"
      data-testid="secret-id"
      aria-label={msg.secretCopyId(secret.id)}
      title={msg.secretCopyId(secret.id)}
      onClick={copyId}
    >
      {copied ? <CheckIcon size={13} /> : <KeyIcon size={13} />}
      {secret.id}
      {copied && (
        <span
          className="lore-row__id-badge-copied"
          role="status"
          data-testid="secret-id-copied"
        >
          {d.idCopied}
        </span>
      )}
    </button>
  );
  const name = (
    <span className="secrets__name" data-testid="secret-name">
      {secret.name}
    </span>
  );
  const actions = (
    <>
      <button
        type="button"
        className="ts-icon-btn"
        data-testid="secret-edit"
        aria-label={`${d.edit} ${secret.name}`}
        title={d.edit}
        onClick={onEdit}
      >
        <PencilIcon size={15} />
      </button>
      <button
        type="button"
        className="ts-icon-btn ts-icon-btn--danger"
        data-testid="secret-delete"
        aria-label={`${d.delete} ${secret.name}`}
        title={d.delete}
        onClick={onDelete}
      >
        <TrashIcon size={15} />
      </button>
    </>
  );
  const updated = formatAbsolute(secret.updatedTs, nowSecs);
  const lastAccessed =
    secret.lastAccessedTs == null
      ? d.neverAccessed
      : formatAbsolute(secret.lastAccessedTs, nowSecs);

  if (stacked) {
    return (
      <article
        className="secrets__row secrets__row--stacked"
        data-testid="secret-row"
        data-secret-id={secret.id}
      >
        <div className="secrets__stack-head">
          <div className="secrets__stack-title">
            {idBadge}
            {name}
          </div>
          <span className="secrets__row-actions">{actions}</span>
        </div>
        {secret.note && (
          <div
            className="secrets__stack-line secrets__stack-line--block"
            data-testid="secret-note"
          >
            <span className="secrets__label">{d.colNote}</span>
            <span className="secrets__note">{secret.note}</span>
          </div>
        )}
        <div className="secrets__stack-line" data-testid="secret-last-accessed">
          <span className="secrets__label">{d.colLastAccessed}</span>
          <span className="secrets__stack-value">{lastAccessed}</span>
        </div>
        <div className="secrets__stack-line" data-testid="secret-updated">
          <span className="secrets__label">{d.colUpdated}</span>
          <span className="secrets__stack-value">{updated}</span>
        </div>
      </article>
    );
  }

  return (
    <article
      className="secrets__row"
      data-testid="secret-row"
      data-secret-id={secret.id}
      role="button"
      tabIndex={0}
      aria-expanded={expanded}
      aria-label={expanded ? d.collapseRow : d.expandRow}
      onClick={onRowToggleClick}
      onKeyDown={onRowToggleKeyDown}
    >
      <div className="secrets__row-head">
        {idBadge}
        {name}
        <span className="secrets__updated" data-testid="secret-updated">
          <span className="secrets__label">{d.colUpdated}</span>
          {updated}
        </span>
        <span className="secrets__row-actions">
          {actions}
          <span className="lore-row__expand-mark" aria-hidden="true">
            {expanded ? <ChevronDownIcon size={18} /> : <ChevronRightIcon size={18} />}
          </span>
        </span>
      </div>
      {expanded && (
        <div className="secrets__detail" data-testid="secret-detail">
          <div className="secrets__detail-line" data-testid="secret-note">
            <span className="secrets__label">{d.colNote}</span>
            {secret.note ? (
              <span className="secrets__note">{secret.note}</span>
            ) : (
              <span className="secrets__muted">{d.noNote}</span>
            )}
          </div>
          <div className="secrets__detail-line" data-testid="secret-last-accessed">
            <span className="secrets__label">{d.colLastAccessed}</span>
            {lastAccessed}
          </div>
        </div>
      )}
    </article>
  );
}

interface SecretFields {
  name: string;
  value: string;
  note: string;
}

function SecretForm({
  mode,
  initial,
  onCancel,
  onSubmit,
}: {
  mode: "create" | "edit";
  initial?: SecretView;
  onCancel: () => void;
  /** Resolves to "" on success, else the message to show. */
  onSubmit: (fields: SecretFields) => Promise<string>;
}) {
  const { t } = useI18n();
  const d = t.secrets;
  const [name, setName] = useState(initial?.name ?? "");
  const [value, setValue] = useState("");
  const [note, setNote] = useState(initial?.note ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const canSubmit =
    !busy && name.trim() !== "" && (mode === "edit" || value !== "");

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    setBusy(true);
    setError("");
    const sent = { name: name.trim(), value, note };
    setValue("");
    const err = await onSubmit(sent);
    setBusy(false);
    if (err) setError(err);
  }

  return (
    <form
      className="secrets__form"
      data-testid={mode === "create" ? "secret-create-form" : "secret-edit-form"}
      onSubmit={submit}
    >
      <div className="secrets__form-title">
        {mode === "create" ? d.createTitle : d.editTitle}
      </div>
      <label className="secrets__field">
        <span className="secrets__label">{d.nameLabel}</span>
        <input
          className="secrets__input"
          data-testid="secret-name-input"
          value={name}
          maxLength={128}
          onChange={(e) => setName(e.target.value)}
        />
      </label>
      <label className="secrets__field">
        <span className="secrets__label">{d.valueLabel}</span>
        {/* A textarea, not a password input: an input strips newlines, which
            silently corrupts PEM and SSH keys. Masked by CSS instead. */}
        <textarea
          className="secrets__input secrets__textarea secrets__value"
          data-testid="secret-value-input"
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
          value={value}
          placeholder={mode === "edit" ? d.valueKeepPlaceholder : undefined}
          onChange={(e) => setValue(e.target.value)}
        />
      </label>
      <label className="secrets__field">
        <span className="secrets__label">{d.noteLabel}</span>
        <textarea
          className="secrets__input secrets__textarea"
          data-testid="secret-note-input"
          value={note}
          maxLength={4000}
          placeholder={d.notePlaceholder}
          onChange={(e) => setNote(e.target.value)}
        />
      </label>
      {error && (
        <div className="secrets__error" data-testid="secret-form-error">
          {error}
        </div>
      )}
      <div className="secrets__form-actions">
        <button type="button" className="secrets__btn" disabled={busy} onClick={onCancel}>
          {d.cancel}
        </button>
        <button
          type="submit"
          className="secrets__btn secrets__submit"
          data-testid="secret-save"
          disabled={!canSubmit}
        >
          {d.save}
        </button>
      </div>
    </form>
  );
}
