// SharedDocsPage — 檔案庫: documents not tied to any task that every member
// reads by `sd-` id. The list is one card per document, like 傳承; #shared-docs/<id> opens one document.

import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useI18n } from "../i18n";
import { api, viewerIsOwner } from "../api";
import { authedAttachmentUrl } from "../api/http";
import { useActors } from "../hooks/useActorName";
import { avatarKindForMember } from "../lib/avatarKind";
import { useSharedDoc, useSharedDocAction, useSharedDocs } from "../hooks/useSharedDocs";
import { navigateHash, useHashRoute } from "../lib/hashRoute";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { Member, SharedDocView } from "../types";
import { MemberChatChip, MemberChip } from "./MemberChip";
import { ConfirmModal } from "./ConfirmModal";
import {
  SharedDocDetail,
  SharedDocTextEditor,
  editableText,
  isEditableKind,
  saveSharedDocText,
} from "./SharedDocDetail";
import { kindMismatchMessage, sharedDocKindForFile } from "./SharedDocFormDialog";
import { MarkdownPreviewOverlay, PreviewShell } from "./MarkdownPreviewOverlay";
import { RuntimeActionMenu } from "./RuntimeActionMenu";
import { SharedDocVersionsModal } from "./SharedDocVersionsModal";
import {
  ClockIcon,
  DownloadIcon,
  ExternalLinkIcon,
  FileIcon,
  FileTextIcon,
  ImageIcon,
  LinkIcon,
  PencilIcon,
  TrashIcon,
  UploadIcon,
} from "./icons";
import { CopyIdChip } from "./IdChips";
import { formatAbsolute } from "../lib/dateFormat";
import "./confirm-modal.css";
import "./card-meta.css";
import "./inline-edit.css";
import "./lore.css";
import "./md-preview.css";
import "./secrets.css";
import "./shared-docs.css";

/** The name a new upload starts with: the filename without its extension. */
export function defaultDocName(filename: string): string {
  const dot = filename.lastIndexOf(".");
  return dot > 0 ? filename.slice(0, dot) : filename;
}

const NAME_MAX_CHARS = 128;

/** A blank link name becomes the URL's host and last path segment, cut to the
 * server's name limit: a whole URL is often longer than that. */
export function defaultLinkName(url: string): string {
  let name = url;
  try {
    const parsed = new URL(url);
    const last = parsed.pathname.split("/").filter(Boolean).pop();
    let segment = last ?? "";
    try {
      segment = decodeURIComponent(segment);
    } catch {
      // keep the raw segment
    }
    name = segment ? `${parsed.host}/${segment}` : parsed.host || url;
  } catch {
    // not a URL the browser can parse; the server will say so
  }
  return Array.from(name).slice(0, NAME_MAX_CHARS).join("");
}

/** `sd-0c41e97a2b58` → `sd-0c41e9`: six characters after the prefix. */
function shortDocId(id: string): string {
  const dash = id.indexOf("-");
  return dash < 0 ? id : id.slice(0, dash + 7);
}

/** The kind as an icon; its name is the tooltip and the accessible name. */
function KindChip({ doc }: { doc: SharedDocView }) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const label = doc.kind === "link" ? d.kindLink : doc.kind === "image" ? d.kindImage : d.kindFile;
  return (
    <span
      className="lore-badge shared-docs__kind"
      role="img"
      aria-label={label}
      title={label}
      data-testid={`shared-doc-kind-${doc.kind}`}
    >
      {doc.kind === "link" ? (
        <LinkIcon size={13} />
      ) : doc.kind === "image" ? (
        <ImageIcon size={13} />
      ) : (
        <FileIcon size={13} />
      )}
    </span>
  );
}

/** The card's name block: the name opens the content itself — a link in a new
 * tab, anything stored in the preview overlay — with the description under it. */
function FileBlock({
  doc,
  onPreview,
}: {
  doc: SharedDocView;
  onPreview: (doc: SharedDocView) => void;
}) {
  const title = doc.description ? `${doc.name}\n${doc.description}` : doc.name;
  const name = <span className="shared-docs__name">{doc.name}</span>;
  return (
    <div className="shared-docs__file" data-testid="shared-doc-name">
      <span className="shared-docs__file-text">
        <span className="shared-docs__name-line">
          {doc.kind === "link" ? (
            <a
              className="shared-docs__file-open"
              data-testid="shared-doc-open"
              href={doc.url}
              target="_blank"
              rel="noopener noreferrer"
              title={title}
            >
              {name}
              <ExternalLinkIcon size={13} />
            </a>
          ) : (
            <button
              type="button"
              className="shared-docs__file-open"
              data-testid="shared-doc-open"
              title={title}
              onClick={() => onPreview(doc)}
            >
              {name}
            </button>
          )}
        </span>
        {doc.description && <span className="shared-docs__file-description">{doc.description}</span>}
      </span>
    </div>
  );
}

type NewDoc = { kind: "file"; file: File } | { kind: "link" };

/** The pencil's dialog: name and description for every kind, plus the URL for
 * a link. Only changed fields are sent, so a name or description change makes
 * no new version and a URL change makes one, as the server already decides.
 * Given `create` instead of `doc`, it is the 新增 menu's dialog and 儲存
 * creates the doc: for a file the name starts as the filename and the file is
 * uploaded then; for a link the URL comes first and a blank name is taken from
 * it. */
function SharedDocInfoDialog({
  doc,
  create,
  run,
  onDone,
  onCancel,
}: {
  doc?: SharedDocView;
  create?: NewDoc;
  run: (action: () => Promise<unknown>) => Promise<string>;
  onDone: () => void;
  onCancel: () => void;
}) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const file = create?.kind === "file" ? create.file : undefined;
  const newLink = create?.kind === "link";
  const link = newLink || doc?.kind === "link";
  const title = file ? d.uploadFile : newLink ? d.addLink : link ? d.editLink : d.editInfo;
  // The fields as they were when the dialog opened: the list row behind `doc`
  // is re-read on every shared_doc delta, and diffing against it would send
  // an untouched field back over another writer's change.
  const [base] = useState(() => doc ?? { name: file ? defaultDocName(file.name) : "", description: "", url: "" });
  // A save the server refuses must not upload the same file again on retry.
  const uploaded = useRef("");
  const [name, setName] = useState(base.name);
  const [description, setDescription] = useState(base.description);
  const [url, setUrl] = useState(base.url);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(() => {
    if (!busy) onCancel();
  }, rootRef);
  // Read during the first render: the first field's autoFocus has already moved
  // focus by the time an effect runs.
  const opener = useRef(document.activeElement as HTMLElement | null);
  useEffect(() => {
    const target = opener.current;
    return () => {
      if (target && target.isConnected) target.focus();
    };
  }, []);

  const canSave = !busy && (newLink ? url.trim() !== "" : name.trim() !== "");

  async function createDoc() {
    setBusy(true);
    setError("");
    const err = await run(async () => {
      if (!file) {
        const target = url.trim();
        await api.createSharedDoc({
          kind: "link",
          name: name.trim() || defaultLinkName(target),
          description: description.trim(),
          url: target,
        });
        return;
      }
      uploaded.current ||= await api.uploadAttachment(file, file.name);
      await api.createSharedDoc({
        kind: sharedDocKindForFile(file.name),
        name: name.trim(),
        description: description.trim(),
        attachmentId: uploaded.current,
      });
    });
    setBusy(false);
    if (err) {
      setError(err);
      return;
    }
    onDone();
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!canSave) return;
    if (create) {
      await createDoc();
      return;
    }
    if (!doc) return;
    const change: { name?: string; description?: string; url?: string } = {};
    if (name.trim() !== base.name) change.name = name.trim();
    if (description.trim() !== base.description) change.description = description.trim();
    if (link && url.trim() !== base.url) change.url = url.trim();
    if (Object.keys(change).length === 0) {
      onCancel();
      return;
    }
    setBusy(true);
    setError("");
    const err = await run(() => api.replaceSharedDoc(doc.id, change));
    setBusy(false);
    if (err) {
      setError(err);
      return;
    }
    onDone();
  }

  const urlField = link && (
    <label className="secrets__field">
      <span className="secrets__label">{d.urlLabel}</span>
      <input
        type="url"
        className="secrets__input"
        data-testid="shared-doc-info-url"
        placeholder={newLink ? d.linkUrlPlaceholder : undefined}
        maxLength={2048}
        autoFocus={newLink}
        value={url}
        onChange={(e) => setUrl(e.target.value)}
      />
    </label>
  );

  return (
    <div ref={rootRef} className="confirm-modal" role="dialog" aria-modal="true" aria-label={title}>
      <form
        className="confirm-modal__box shared-docs__form shared-docs__info-dialog"
        data-testid="shared-doc-info-dialog"
        noValidate={newLink}
        onSubmit={submit}
      >
        <div className="secrets__form-title">{title}</div>
        {file && (
          <div className="secrets__field">
            <span className="secrets__label">{d.fileLabel}</span>
            <span className="shared-docs__info-filename" data-testid="shared-doc-info-filename">
              {file.name}
            </span>
          </div>
        )}
        {newLink && urlField}
        <label className="secrets__field">
          <span className="secrets__label">{d.nameLabel}</span>
          <input
            className="secrets__input"
            data-testid="shared-doc-info-name"
            placeholder={newLink ? d.linkNamePlaceholder : undefined}
            maxLength={NAME_MAX_CHARS}
            autoFocus={!newLink}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </label>
        <label className="secrets__field">
          <span className="secrets__label">{d.descriptionLabel}</span>
          <textarea
            className="secrets__input secrets__textarea"
            data-testid="shared-doc-info-description"
            rows={2}
            maxLength={1024}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </label>
        {!newLink && urlField}
        {error && (
          <div className="confirm-modal__error" role="alert" data-testid="shared-doc-info-error">
            {error}
          </div>
        )}
        <div className="secrets__form-actions">
          <button
            type="button"
            className="secrets__btn"
            data-testid="shared-doc-info-cancel"
            disabled={busy}
            onClick={onCancel}
          >
            {d.cancel}
          </button>
          <button
            type="submit"
            className="secrets__btn secrets__submit"
            data-testid="shared-doc-info-save"
            disabled={!canSave}
          >
            {d.save}
          </button>
        </div>
      </form>
    </div>
  );
}

/** 編輯 on a text file: the file's content in an editor over the list. Closing
 * it any way but 儲存 drops the edit, as the detail page's 取消 does. */
function SharedDocEditOverlay({
  id,
  title,
  onClose,
  onSaved,
  onUploadVersion,
}: {
  id: string;
  title: string;
  onClose: () => void;
  onSaved: () => void;
  onUploadVersion: () => void;
}) {
  const { t, msg } = useI18n();
  const d = t.sharedDocs;
  const { doc, notFound, loadError } = useSharedDoc(id, d.loadFailed);
  const run = useSharedDocAction(d.actionFailed);
  const [draft, setDraft] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // Seeded once: the doc is re-read on every shared_doc delta, and a later read
  // must not overwrite what is being typed.
  useEffect(() => {
    if (doc && draft === null && editableText(doc)) setDraft(doc.text ?? "");
  }, [doc, draft]);

  const close = () => {
    if (!busy) onClose();
  };

  async function save() {
    if (!doc || draft === null) return;
    setBusy(true);
    setError("");
    const err = await run(() => saveSharedDocText(doc, draft));
    setBusy(false);
    if (err) {
      setError(err);
      return;
    }
    onSaved();
  }

  let body: ReactNode;
  if (notFound) {
    body = <div className="md-preview__status">{d.notFound}</div>;
  } else if (!doc) {
    body = <div className="md-preview__status">{loadError || t.chat.mdPreview.loading}</div>;
  } else if (draft !== null) {
    body = (
      <>
        {error && (
          <div className="secrets__error" role="alert" data-testid="shared-doc-edit-error">
            {error}
          </div>
        )}
        <SharedDocTextEditor
          draft={draft}
          onDraft={setDraft}
          busy={busy}
          autoFocus
          onCancel={close}
          onSave={() => void save()}
        />
      </>
    );
  } else {
    body = (
      <div className="shared-docs__empty-state">
        <div className="shared-docs__empty-state-card" data-testid="shared-doc-edit-unavailable">
          <span className="shared-docs__empty-state-icon">
            <FileTextIcon size={26} />
          </span>
          <div className="shared-docs__empty-state-title" role="status">
            {doc.textOmitted === "too_large" ? d.tooLargeToEdit : d.editUnavailable}
          </div>
          <div className="shared-docs__empty-state-meta">{doc.filename || doc.name}</div>
          <div className="shared-docs__empty-state-meta">{msg.sharedDocEditHint()}</div>
          <div className="shared-docs__empty-state-actions">
            <a
              className="secrets__btn secrets__submit shared-docs__bar-btn"
              data-testid="shared-doc-download"
              href={authedAttachmentUrl(doc.url)}
              download={doc.filename || doc.name}
            >
              <DownloadIcon size={14} />
              {d.download}
            </a>
            <button
              type="button"
              className="secrets__btn shared-docs__bar-btn"
              data-testid="shared-doc-edit-upload"
              onClick={onUploadVersion}
            >
              <UploadIcon size={14} />
              {d.uploadVersion}
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <PreviewShell
      title={title}
      onClose={close}
      closeLabel={d.cancel}
      closeDisabled={busy}
      dismissOnBackdrop={false}
      testId="shared-doc-edit-overlay"
    >
      <div className="md-preview__body shared-docs__overlay-body">{body}</div>
    </PreviewShell>
  );
}

/** 最後更新 opens the doc's version history over the list; the doc's own
 * page stays at #shared-docs/<id>. */
function UpdatedButton({ text, onOpen }: { text: string; onOpen: () => void }) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  return (
    <button
      type="button"
      className="card-meta__chip card-meta__chip--link shared-docs__updated-open"
      data-testid="shared-doc-updated"
      aria-label={`${text} · ${d.openHistory}`}
      title={d.openHistory}
      onClick={onOpen}
    >
      <ClockIcon size={13} />
      <span className="shared-docs__chip-text">{text}</span>
    </button>
  );
}

/** 編輯資訊 | 上傳新版本 | 永久刪除, in the same places on every card; a link
 * cannot take an upload, so its button stays put, dimmed, with a tooltip
 * saying why. */
function RowActions({
  doc,
  isOwner,
  busy,
  onEditInfo,
  onUpload,
  onDelete,
}: {
  doc: SharedDocView;
  isOwner: boolean;
  busy: boolean;
  onEditInfo: () => void;
  onUpload: () => void;
  onDelete: () => void;
}) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const link = doc.kind === "link";
  const uploadTitle = link ? d.uploadVersionUnavailable : d.uploadVersion;
  const infoLabel = link ? d.editLink : d.editInfo;
  return (
    <span className="lore-row__actions shared-docs__actions">
      <button
        type="button"
        className="lore-row__icon-btn"
        data-testid="shared-doc-edit-info"
        aria-label={`${infoLabel} ${doc.name}`}
        title={infoLabel}
        onClick={onEditInfo}
      >
        <PencilIcon size={15} />
      </button>
      <button
        type="button"
        className="lore-row__icon-btn"
        data-testid="shared-doc-upload"
        aria-label={`${uploadTitle} ${doc.name}`}
        aria-disabled={link || busy || undefined}
        title={uploadTitle}
        onClick={() => {
          if (!link && !busy) onUpload();
        }}
      >
        <UploadIcon size={15} />
      </button>
      {isOwner && (
        <button
          type="button"
          className="lore-row__icon-btn lore-row__icon-btn--danger"
          data-testid="shared-doc-delete"
          aria-label={`${d.delete} ${doc.name}`}
          title={d.delete}
          onClick={onDelete}
        >
          <TrashIcon size={15} />
        </button>
      )}
    </span>
  );
}

/** 建立者: who made the doc, as an avatar chip that opens their chat; the owner
 * wears the profile pill's avatar and has no chat. */
function CreatorChip({ id, name, member }: { id: string; name: string; member: Member | undefined }) {
  const { t } = useI18n();
  const avatar = member ? { kind: avatarKindForMember(member), src: member.avatarUrl } : null;
  if (id === "owner") {
    return <MemberChip name={name} avatar={{ kind: "owner" }} testId="shared-doc-creator" />;
  }
  return (
    <MemberChatChip
      name={name}
      avatar={avatar}
      label={t.tasks.messageCreator}
      onOpen={() => navigateHash({ page: "office", chatId: id })}
      testId="shared-doc-creator"
    />
  );
}

export function SharedDocsPage({ docId }: { docId?: string }) {
  const [, setRoute] = useHashRoute();
  const actors = useActors();
  const actorName = actors.name;
  if (docId) {
    return (
      <SharedDocDetail
        key={docId}
        id={docId}
        actorName={actorName}
        onBack={() => setRoute({ page: "shared_docs" })}
      />
    );
  }
  return <SharedDocList actorName={actorName} actorMember={actors.member} />;
}

type PickTarget = { mode: "create" } | { mode: "replace"; doc: SharedDocView };

function SharedDocList({
  actorName,
  actorMember,
}: {
  actorName: (id: string) => string;
  actorMember: (id: string) => Member | undefined;
}) {
  const { t, msg } = useI18n();
  const d = t.sharedDocs;
  const { docs, loading, loadError, reload } = useSharedDocs(d.loadFailed);
  const run = useSharedDocAction(d.actionFailed);
  const [actionError, setActionError] = useState("");
  const [uploading, setUploading] = useState<string[]>([]);
  const [infoId, setInfoId] = useState<string | null>(null);
  const [creating, setCreating] = useState<NewDoc | null>(null);
  const [preview, setPreview] = useState<SharedDocView | null>(null);
  const [editing, setEditing] = useState<SharedDocView | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<SharedDocView | null>(null);
  const [historyId, setHistoryId] = useState<string | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);
  const pickTarget = useRef<PickTarget>({ mode: "create" });
  const isOwner = viewerIsOwner();
  const infoDoc = infoId === null ? undefined : docs.find((doc) => doc.id === infoId);
  const nowSecs = Math.floor(Date.now() / 1000);

  function pick(target: PickTarget) {
    const input = fileInput.current;
    if (!input) return;
    setActionError("");
    pickTarget.current = target;
    input.accept = target.mode === "replace" && target.doc.kind === "image" ? "image/*" : "";
    input.value = "";
    input.click();
  }

  async function onPicked(file: File) {
    const target = pickTarget.current;
    setActionError("");
    if (target.mode === "create") {
      setCreating({ kind: "file", file });
      return;
    }
    const mismatch = kindMismatchMessage(target.doc.kind, file.name, d);
    if (mismatch) {
      setActionError(mismatch);
      return;
    }
    const busyKey = target.doc.id;
    setUploading((keys) => [...keys, busyKey]);
    const err = await run(async () => {
      const attachmentId = await api.uploadAttachment(file, file.name);
      await api.replaceSharedDoc(target.doc.id, { attachmentId });
    });
    setUploading((keys) => {
      const at = keys.indexOf(busyKey);
      return at < 0 ? keys : [...keys.slice(0, at), ...keys.slice(at + 1)];
    });
    if (err) {
      setActionError(err);
      return;
    }
    await reload();
  }

  async function confirmDelete() {
    if (!deleteTarget) return;
    setDeleteBusy(true);
    const err = await run(() => api.deleteSharedDoc(deleteTarget.id));
    setDeleteBusy(false);
    if (err) {
      setDeleteError(err);
      return;
    }
    setDeleteTarget(null);
    setDeleteError("");
    await reload();
  }

  return (
    <div className="shared-docs" data-testid="shared-docs-page">
      <div className="shared-docs__bar">
        <span className="shared-docs__count" data-testid="shared-docs-count">
          {!loading && !loadError && msg.sharedDocListedCount(docs.length)}
        </span>
        <RuntimeActionMenu
          label={d.add}
          testIdPrefix="shared-docs-add"
          buttonClassName="secrets__btn secrets__submit shared-docs__bar-btn"
          items={[
            {
              key: "upload",
              label: d.uploadFile,
              icon: <UploadIcon size={15} />,
              onSelect: () => pick({ mode: "create" }),
            },
            {
              key: "link",
              label: d.addLink,
              icon: <ExternalLinkIcon size={15} />,
              onSelect: () => {
                setActionError("");
                setCreating({ kind: "link" });
              },
            },
          ]}
        >
          {(chevron) => (
            <>
              <span aria-hidden="true">+</span>
              {d.add}
              {chevron}
            </>
          )}
        </RuntimeActionMenu>
        <input
          ref={fileInput}
          type="file"
          hidden
          data-testid="shared-docs-file-input"
          onChange={(e) => {
            const file = e.target.files?.[0];
            if (file) void onPicked(file);
          }}
        />
      </div>

      {actionError && (
        <div className="secrets__error" role="alert" data-testid="shared-docs-action-error">
          {actionError}
        </div>
      )}
      {loadError && (
        <div className="secrets__error" data-testid="shared-docs-load-error">
          {loadError}
        </div>
      )}
      {!loading && !loadError && docs.length === 0 && (
        <div className="shared-docs__empty" data-testid="shared-docs-empty">
          {d.empty}
        </div>
      )}

      {docs.length > 0 && (
        <div className="lore-section__body" role="list" data-testid="shared-docs-list">
          {docs.map((doc) => (
            <div
              key={doc.id}
              role="listitem"
              className="lore-row shared-docs__row"
              data-testid="shared-doc-row"
              data-doc-id={doc.id}
            >
              <div className="lore-row__head">
                <div className="lore-row__badges">
                  <CopyIdChip
                    value={doc.id}
                    text={`#${shortDocId(doc.id)}`}
                    label={msg.sharedDocCopyId(doc.id)}
                    copiedLabel={d.idCopied}
                    className="lore-row__id-badge"
                    copiedClassName="lore-row__id-badge-copied"
                    testId="shared-doc-id"
                    copiedTestId="shared-doc-id-copied"
                  />
                  <KindChip doc={doc} />
                </div>
                <RowActions
                  doc={doc}
                  isOwner={isOwner}
                  busy={uploading.includes(doc.id)}
                  onEditInfo={() => {
                    setActionError("");
                    setInfoId(doc.id);
                  }}
                  onUpload={() => pick({ mode: "replace", doc })}
                  onDelete={() => {
                    setDeleteError("");
                    setDeleteTarget(doc);
                  }}
                />
              </div>
              <FileBlock doc={doc} onPreview={setPreview} />
              <div className="card-meta">
                <span className="card-meta__label">{d.createdByLabel}</span>
                <CreatorChip id={doc.createdBy} name={actorName(doc.createdBy)} member={actorMember(doc.createdBy)} />
                <span className="card-meta__label">{d.updatedLabel}</span>
                <UpdatedButton
                  text={msg.sharedDocUpdated(actorName(doc.updatedBy), formatAbsolute(doc.updatedTs, nowSecs))}
                  onOpen={() => setHistoryId(doc.id)}
                />
              </div>
            </div>
          ))}
        </div>
      )}

      {preview && (
        <MarkdownPreviewOverlay
          title={preview.name}
          filename={preview.filename}
          url={preview.url}
          attachmentId={preview.attachmentId}
          mime={preview.mime}
          onEdit={
            isEditableKind(preview)
              ? () => {
                  setPreview(null);
                  setEditing(preview);
                }
              : undefined
          }
          onClose={() => setPreview(null)}
        />
      )}
      {editing && (
        <SharedDocEditOverlay
          id={editing.id}
          title={editing.name}
          onClose={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            void reload();
          }}
          onUploadVersion={() => {
            setEditing(null);
            pick({ mode: "replace", doc: editing });
          }}
        />
      )}
      {historyId !== null && (
        <SharedDocVersionsModal
          docId={historyId}
          actorName={actorName}
          onRestored={() => void reload()}
          onClose={() => setHistoryId(null)}
        />
      )}
      {creating && (
        <SharedDocInfoDialog
          create={creating}
          run={run}
          onCancel={() => setCreating(null)}
          onDone={() => {
            setCreating(null);
            void reload();
          }}
        />
      )}
      {infoDoc && (
        <SharedDocInfoDialog
          key={infoDoc.id}
          doc={infoDoc}
          run={run}
          onCancel={() => setInfoId(null)}
          onDone={() => {
            setInfoId(null);
            void reload();
          }}
        />
      )}
      {deleteTarget && (
        <ConfirmModal
          body={msg.sharedDocDeleteConfirmBody(deleteTarget.name)}
          error={deleteError || null}
          cancelLabel={d.cancel}
          confirmLabel={d.delete}
          busy={deleteBusy}
          danger
          onCancel={() => setDeleteTarget(null)}
          onConfirm={() => void confirmDelete()}
          testId="shared-doc-delete-confirm"
          confirmTestId="shared-doc-delete-confirm-btn"
        />
      )}
    </div>
  );
}
