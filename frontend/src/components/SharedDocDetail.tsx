// SharedDocDetail — one shared document (#shared-docs/<id>): its name,
// description, who changed it last, and its current content.

import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { authedAttachmentUrl } from "../api/http";
import { useSharedDoc, useSharedDocAction } from "../hooks/useSharedDocs";
import { formatAbsolute } from "../lib/dateFormat";
import type { SharedDocDetailView } from "../types";
import { Markdown } from "./Markdown";
import { isMarkdownAttachment } from "./MarkdownPreviewOverlay";
import { SharedDocFormDialog } from "./SharedDocFormDialog";
import { SharedDocVersionsModal } from "./SharedDocVersionsModal";
import { InlineEdit } from "./InlineEdit";
import { CheckIcon, ChevronLeftIcon, CloseIcon } from "./icons";
import "./inline-edit.css";
import "./secrets.css";
import "./settings.css";
import "./shared-docs.css";

/** Only Markdown and plain text are edited in place; everything else gets a
 * new version by upload. */
function editableText(doc: SharedDocDetailView): boolean {
  if (doc.kind !== "file" || doc.text === null) return false;
  const mime = doc.mime.split(";")[0]!.trim().toLowerCase();
  return isMarkdownAttachment(mime, doc.filename) || mime === "text/plain";
}

function SharedDocContent({ doc }: { doc: SharedDocDetailView }) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const mime = doc.mime.split(";")[0]!.trim().toLowerCase();
  if (doc.kind === "link") {
    return (
      <a
        className="shared-docs__link"
        data-testid="shared-doc-link"
        href={doc.url}
        target="_blank"
        rel="noopener noreferrer"
      >
        {doc.url}
      </a>
    );
  }
  const src = authedAttachmentUrl(doc.url);
  if (mime.startsWith("image/")) {
    return <img className="shared-docs__image" data-testid="shared-doc-image" src={src} alt={doc.name} />;
  }
  if (doc.text !== null) {
    return isMarkdownAttachment(mime, doc.filename) ? (
      <div data-testid="shared-doc-markdown">
        <Markdown source={doc.text} className="doc-md" resolveImageSrc={authedAttachmentUrl} />
      </div>
    ) : (
      <pre className="shared-docs__text" data-testid="shared-doc-text">
        {doc.text}
      </pre>
    );
  }
  const download = (
    <a className="secrets__btn" data-testid="shared-doc-download" href={src} download={doc.filename || doc.name}>
      {d.download}
    </a>
  );
  if (mime === "application/pdf") {
    return (
      <div className="shared-docs__pdf">
        <iframe className="shared-docs__pdf-frame" data-testid="shared-doc-pdf" src={src} title={doc.name} />
        {download}
      </div>
    );
  }
  return (
    <div className="shared-docs__opaque">
      <p className="shared-docs__muted">
        {doc.textOmitted === "too_large" ? d.tooLarge : d.binary}
      </p>
      {download}
    </div>
  );
}

/** The description: click it to edit in a two-line box, ✓ saves (blank clears
 * it), ✗ or Escape cancels. */
function DescriptionEdit({
  value,
  onCommit,
}: {
  value: string;
  onCommit: (next: string) => Promise<boolean>;
}) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [saving, setSaving] = useState(false);

  async function commit() {
    const next = draft.trim();
    if (next === value) {
      setEditing(false);
      return;
    }
    setSaving(true);
    const saved = await onCommit(next);
    setSaving(false);
    if (saved) setEditing(false);
  }

  if (!editing) {
    return (
      <button
        type="button"
        className={`shared-docs__description${value ? "" : " shared-docs__muted"}`}
        data-testid="shared-doc-description"
        title={d.editDescription}
        onClick={() => {
          setDraft(value);
          setEditing(true);
        }}
      >
        {value || d.descriptionPlaceholder}
      </button>
    );
  }
  return (
    <span className="shared-docs__description-edit">
      <textarea
        className="secrets__input shared-docs__description-input"
        data-testid="shared-doc-description-input"
        aria-label={d.editDescription}
        rows={2}
        maxLength={1024}
        autoFocus
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.preventDefault();
            setEditing(false);
          }
        }}
      />
      <button
        type="button"
        className="inline-edit__iconbtn inline-edit__iconbtn--apply"
        data-testid="shared-doc-description-save"
        aria-label={t.common.apply}
        title={t.common.apply}
        disabled={saving}
        onClick={() => void commit()}
      >
        <CheckIcon size={14} />
      </button>
      <button
        type="button"
        className="inline-edit__iconbtn inline-edit__iconbtn--cancel"
        data-testid="shared-doc-description-cancel"
        aria-label={t.common.cancel}
        title={t.common.cancel}
        onClick={() => setEditing(false)}
      >
        <CloseIcon size={14} />
      </button>
    </span>
  );
}

export function SharedDocDetail({
  id,
  actorName,
  onBack,
  startEditing = false,
}: {
  id: string;
  actorName: (actorId: string) => string;
  onBack: () => void;
  /** Open the text editor as soon as the document arrives (the list's 編輯). */
  startEditing?: boolean;
}) {
  const { t, msg } = useI18n();
  const d = t.sharedDocs;
  const { doc, notFound, loadError, reload } = useSharedDoc(id, d.loadFailed);
  const run = useSharedDocAction(d.actionFailed);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [replacing, setReplacing] = useState(false);
  const [versionsOpen, setVersionsOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const nowSecs = Math.floor(Date.now() / 1000);
  const [pendingEdit, setPendingEdit] = useState(startEditing);
  const [editTooLarge, setEditTooLarge] = useState(false);

  useEffect(() => {
    if (!pendingEdit || !doc) return;
    setPendingEdit(false);
    if (!editableText(doc)) {
      setEditTooLarge(doc.textOmitted === "too_large");
      return;
    }
    setDraft(doc.text ?? "");
    setEditing(true);
  }, [pendingEdit, doc]);

  async function act(action: () => Promise<unknown>): Promise<boolean> {
    setBusy(true);
    setError("");
    const err = await run(action);
    setBusy(false);
    if (err) {
      setError(err);
      return false;
    }
    await reload();
    return true;
  }

  async function saveEdit() {
    if (!doc) return;
    const filename = doc.filename || `${doc.name}.md`;
    const ok = await act(async () => {
      const attachmentId = await api.uploadAttachment(
        new Blob([draft], { type: doc.mime || "text/plain" }),
        filename,
      );
      await api.replaceSharedDoc(doc.id, { attachmentId });
    });
    if (ok) setEditing(false);
  }

  const back = (
    <button type="button" className="shared-docs__back" data-testid="shared-doc-back" onClick={onBack}>
      <ChevronLeftIcon size={14} />
      {d.back}
    </button>
  );

  if (notFound) {
    return (
      <div className="shared-docs" data-testid="shared-doc-detail">
        {back}
        <div className="shared-docs__empty" data-testid="shared-doc-not-found">
          {d.notFound}
        </div>
      </div>
    );
  }
  if (!doc) {
    return (
      <div className="shared-docs" data-testid="shared-doc-detail">
        {back}
        {loadError && <div className="secrets__error">{loadError}</div>}
      </div>
    );
  }

  return (
    <div className="shared-docs" data-testid="shared-doc-detail">
      {back}
      <section className="shared-docs__card shared-docs__head">
        <div className="shared-docs__head-text">
          <h2 className="shared-docs__title" data-testid="shared-doc-title">
            <InlineEdit
              value={doc.name}
              ariaLabel={d.rename}
              onCommit={(name) => void act(() => api.replaceSharedDoc(doc.id, { name }))}
            />
          </h2>
          <DescriptionEdit
            value={doc.description}
            onCommit={(description) =>
              act(() => api.replaceSharedDoc(doc.id, { description }))
            }
          />
          <p className="shared-docs__muted" data-testid="shared-doc-updated">
            {d.updatedLead}
            {msg.sharedDocUpdated(actorName(doc.updatedBy), formatAbsolute(doc.updatedTs, nowSecs))}
          </p>
        </div>
        <div className="shared-docs__head-actions">
          {editableText(doc) && !editing && (
            <button
              type="button"
              className="secrets__btn"
              data-testid="shared-doc-edit"
              onClick={() => {
                setDraft(doc.text ?? "");
                setEditing(true);
              }}
            >
              {d.edit}
            </button>
          )}
          <button
            type="button"
            className="secrets__btn"
            data-testid="shared-doc-upload-version"
            onClick={() => setReplacing(true)}
          >
            {d.uploadVersion}
          </button>
          <button
            type="button"
            className="secrets__btn"
            data-testid="shared-doc-versions"
            onClick={() => setVersionsOpen(true)}
          >
            {d.versions}
          </button>
        </div>
      </section>
      {editTooLarge && (
        <div className="shared-docs__notice" role="status" data-testid="shared-doc-edit-too-large">
          {d.tooLargeToEdit}
        </div>
      )}
      {error && (
        <div className="secrets__error" role="alert" data-testid="shared-doc-error">
          {error}
        </div>
      )}
      <section className="shared-docs__card shared-docs__body">
        {editing ? (
          <div className="shared-docs__editor">
            <textarea
              className="secrets__input secrets__textarea shared-docs__editor-input"
              data-testid="shared-doc-editor"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
            />
            <div className="secrets__form-actions">
              <button type="button" className="secrets__btn" disabled={busy} onClick={() => setEditing(false)}>
                {d.cancel}
              </button>
              <button
                type="button"
                className="secrets__btn secrets__submit"
                data-testid="shared-doc-editor-save"
                disabled={busy}
                onClick={() => void saveEdit()}
              >
                {d.save}
              </button>
            </div>
          </div>
        ) : (
          <SharedDocContent doc={doc} />
        )}
      </section>
      {replacing && (
        <SharedDocFormDialog
          docId={doc.id}
          kind={doc.kind}
          run={run}
          onCancel={() => setReplacing(false)}
          onDone={() => {
            setReplacing(false);
            void reload();
          }}
        />
      )}
      {versionsOpen && (
        <SharedDocVersionsModal
          docId={doc.id}
          actorName={actorName}
          run={run}
          onClose={() => {
            setVersionsOpen(false);
            void reload();
          }}
        />
      )}
    </div>
  );
}
