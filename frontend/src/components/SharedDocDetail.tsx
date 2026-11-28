// SharedDocDetail — one shared document (#shared-docs/<id>): its name,
// description, who changed it last, and its current content.

import { useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { authedAttachmentUrl } from "../api/http";
import { useSharedDoc, useSharedDocAction } from "../hooks/useSharedDocs";
import { formatAbsolute } from "../lib/dateFormat";
import type { SharedDocDetailView, SharedDocView } from "../types";
import { ContentView } from "./ContentView";
import { isInlineDisplayableMime } from "./MarkdownPreviewOverlay";
import { usePreviewHref } from "../hooks/usePreviewHref";
import { contentKindOf, isTextKind } from "../lib/contentKind";
import { SharedDocFormDialog } from "./SharedDocFormDialog";
import { SharedDocVersionsModal } from "./SharedDocVersionsModal";
import { InlineEdit } from "./InlineEdit";
import { CheckIcon, ChevronLeftIcon, CloseIcon } from "./icons";
import "./inline-edit.css";
import "./secrets.css";
import "./settings.css";
import "./shared-docs.css";

/** Any text file is edited in place, as plain text; everything else gets a
 * new version by upload. An HTML page is text too: it is edited as its source
 * while it is still shown as its page. */
export function isEditableKind(doc: SharedDocView): boolean {
  if (doc.kind !== "file") return false;
  const kind = contentKindOf(doc.mime, doc.filename);
  return isTextKind(kind) || kind === "html";
}

export function editableText(doc: SharedDocDetailView): boolean {
  return isEditableKind(doc) && doc.text !== null;
}

/** Puts edited text in as the doc's new version, under its current filename. */
export async function saveSharedDocText(doc: SharedDocDetailView, draft: string): Promise<void> {
  const attachmentId = await api.uploadAttachment(
    new Blob([draft], { type: doc.mime || "text/plain" }),
    doc.filename || `${doc.name}.md`,
  );
  await api.replaceSharedDoc(doc.id, { attachmentId });
}

export function SharedDocTextEditor({
  draft,
  onDraft,
  busy,
  autoFocus,
  onCancel,
  onSave,
}: {
  draft: string;
  onDraft: (next: string) => void;
  busy: boolean;
  autoFocus?: boolean;
  onCancel: () => void;
  onSave: () => void;
}) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  return (
    <div className="shared-docs__editor">
      <textarea
        className="secrets__input secrets__textarea shared-docs__editor-input"
        data-testid="shared-doc-editor"
        autoFocus={autoFocus}
        value={draft}
        onChange={(e) => onDraft(e.target.value)}
      />
      <div className="secrets__form-actions">
        <button
          type="button"
          className="secrets__btn"
          data-testid="shared-doc-editor-cancel"
          disabled={busy}
          onClick={onCancel}
        >
          {d.cancel}
        </button>
        <button
          type="button"
          className="secrets__btn secrets__submit"
          data-testid="shared-doc-editor-save"
          disabled={busy}
          onClick={onSave}
        >
          {d.save}
        </button>
      </div>
    </div>
  );
}

function SharedDocContent({ doc }: { doc: SharedDocDetailView }) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const kind = contentKindOf(doc.mime, doc.filename);
  const needsShareLink =
    doc.kind !== "link" &&
    (kind === "html" || kind === "pdf" || kind === "video" || kind === "binary") &&
    isInlineDisplayableMime(doc.mime);
  const share = usePreviewHref(doc.attachmentId, needsShareLink, "SharedDocContent");
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
  const download = (
    <a className="secrets__btn" data-testid="shared-doc-download" href={src} download={doc.filename || doc.name}>
      {d.download}
    </a>
  );
  if (isTextKind(kind) && doc.text === null) {
    return (
      <div className="shared-docs__opaque">
        <p className="shared-docs__muted">
          {doc.textOmitted === "too_large" ? d.tooLarge : d.binary}
        </p>
        {download}
      </div>
    );
  }
  const view = (
    <ContentView
      kind={kind}
      text={doc.text}
      src={src}
      label={doc.name}
      shareHref={share.href}
      shareFailed={share.failed}
      newTabLink
      unavailable={d.binary}
      resolveImageSrc={authedAttachmentUrl}
      testId={`shared-doc-${kind}`}
    />
  );
  if (kind === "pdf" || kind === "video" || kind === "binary" || (kind === "html" && share.failed)) {
    return (
      <div className="shared-docs__opaque">
        {view}
        {download}
      </div>
    );
  }
  return view;
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
}: {
  id: string;
  actorName: (actorId: string) => string;
  onBack: () => void;
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
    if (await act(() => saveSharedDocText(doc, draft))) setEditing(false);
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
      {error && (
        <div className="secrets__error" role="alert" data-testid="shared-doc-error">
          {error}
        </div>
      )}
      <section className="shared-docs__card shared-docs__body">
        {editing ? (
          <SharedDocTextEditor
            draft={draft}
            onDraft={setDraft}
            busy={busy}
            onCancel={() => setEditing(false)}
            onSave={() => void saveEdit()}
          />
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
          onRestored={() => void reload()}
          onClose={() => setVersionsOpen(false)}
        />
      )}
    </div>
  );
}
