// 🔴 A VALUE NEVER COMES BACK. No route returns one, so this page has nothing
// to show; the value lives only in the create/edit input until the request is
// sent, and the input is cleared whether the save succeeds or not.

import { useEffect, useRef, useState, type FormEvent } from "react";
import { useI18n } from "../i18n";
import { useSecrets } from "../hooks/useSecrets";
import { copyText } from "../lib/clipboard";
import { formatAbsolute } from "../lib/dateFormat";
import type { SecretView } from "../types";
import { ConfirmModal } from "./ConfirmModal";
import {
  CheckIcon,
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
        <div className="secrets__grid" data-testid="secrets-list">
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
}: {
  secret: SecretView;
  nowSecs: number;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t, msg } = useI18n();
  const d = t.secrets;
  // Same contract as the 傳承 entry id: 已複製 only when the clipboard write
  // actually succeeded, and the timer dies with the row.
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

  return (
    <article
      className="secrets__row"
      data-testid="secret-row"
      data-secret-id={secret.id}
    >
      <div className="secrets__row-head">
        <div className="secrets__row-title">
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
          <span className="secrets__name" data-testid="secret-name">
            {secret.name}
          </span>
        </div>
        <span className="secrets__row-actions">
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
        </span>
      </div>
      {secret.note && (
        <div
          className="secrets__row-line secrets__row-line--block"
          data-testid="secret-note"
        >
          <span className="secrets__label">{d.colNote}</span>
          <span className="secrets__note">{secret.note}</span>
        </div>
      )}
      <div className="secrets__row-line" data-testid="secret-last-accessed">
        <span className="secrets__label">{d.colLastAccessed}</span>
        <span className="secrets__row-value">
          {secret.lastAccessedTs == null
            ? d.neverAccessed
            : formatAbsolute(secret.lastAccessedTs, nowSecs)}
        </span>
      </div>
      <div className="secrets__row-line" data-testid="secret-updated">
        <span className="secrets__label">{d.colUpdated}</span>
        <span className="secrets__row-value">
          {formatAbsolute(secret.updatedTs, nowSecs)}
        </span>
      </div>
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
