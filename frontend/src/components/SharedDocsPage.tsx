// SharedDocsPage — 檔案庫: documents not tied to any task that every member
// reads by `sd-` id. The list is a table; #shared-docs/<id> opens one document.

import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useI18n } from "../i18n";
import { api, viewerIsOwner } from "../api";
import { authedAttachmentUrl } from "../api/http";
import { useMembers } from "../hooks/useMembers";
import { useOwnerDisplayName } from "../hooks/useOwnerName";
import { useSharedDoc, useSharedDocAction, useSharedDocs } from "../hooks/useSharedDocs";
import { copyText } from "../lib/clipboard";
import { formatAbsolute } from "../lib/dateFormat";
import { useHashRoute } from "../lib/hashRoute";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { SharedDocView } from "../types";
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
import {
  CheckIcon,
  CloseIcon,
  DownloadIcon,
  ExternalLinkIcon,
  FileTextIcon,
  ImageIcon,
  LayersIcon,
  PaperclipIcon,
  PencilIcon,
  TrashIcon,
  UploadIcon,
} from "./icons";
import "./confirm-modal.css";
import "./inline-edit.css";
import "./lore.css";
import "./md-preview.css";
import "./secrets.css";
import "./theme-settings.css";
import "./shared-docs.css";

function useActorName(): (id: string) => string {
  const { t } = useI18n();
  const { members } = useMembers();
  const ownerName = useOwnerDisplayName(t.user);
  return useCallback(
    (id: string) => {
      if (id === "owner") return ownerName;
      return members.find((m) => m.id === id)?.name || id;
    },
    [members, ownerName],
  );
}

function useFlash(): [string | null, (key: string) => void] {
  const [flashed, setFlashed] = useState<string | null>(null);
  const timer = useRef<number | null>(null);
  useEffect(
    () => () => {
      if (timer.current != null) window.clearTimeout(timer.current);
    },
    [],
  );
  const flash = useCallback((key: string) => {
    setFlashed(key);
    if (timer.current != null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setFlashed(null), 1600);
  }, []);
  return [flashed, flash];
}

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

function KindMedia({ doc }: { doc: SharedDocView }) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const label = doc.kind === "link" ? d.kindLink : doc.kind === "image" ? d.kindImage : d.kindFile;
  return (
    <span
      className="shared-docs__kind"
      data-testid={`shared-doc-kind-${doc.kind}`}
      title={label}
      aria-label={label}
    >
      {doc.kind === "link" ? (
        <ExternalLinkIcon size={15} />
      ) : doc.kind === "image" ? (
        <ImageIcon size={15} />
      ) : (
        <PaperclipIcon size={15} />
      )}
    </span>
  );
}

/** The 檔案 cell: the name opens the content itself — a link in a new tab,
 * anything stored in the preview overlay — and the pencil after it opens the
 * name/description dialog. */
function FileBlock({
  doc,
  onPreview,
  onEditInfo,
}: {
  doc: SharedDocView;
  onPreview: (doc: SharedDocView) => void;
  onEditInfo: (doc: SharedDocView) => void;
}) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const title = doc.description ? `${doc.name}\n${doc.description}` : doc.name;
  const name = <span className="shared-docs__name">{doc.name}</span>;
  const infoLabel = doc.kind === "link" ? d.editLink : d.editInfo;
  return (
    <div className="shared-docs__file" data-testid="shared-doc-name">
      <KindMedia doc={doc} />
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
          <button
            type="button"
            className="shared-docs__name-edit"
            data-testid="shared-doc-edit-info"
            aria-label={`${infoLabel} ${doc.name}`}
            title={infoLabel}
            onClick={() => onEditInfo(doc)}
          >
            <PencilIcon size={13} />
          </button>
        </span>
        {doc.description && <span className="shared-docs__file-description">{doc.description}</span>}
      </span>
    </div>
  );
}

/** The pencil's dialog: name and description for every kind, plus the URL for
 * a link. Only changed fields are sent, so a name or description change makes
 * no new version and a URL change makes one, as the server already decides. */
function SharedDocInfoDialog({
  doc,
  run,
  onDone,
  onCancel,
}: {
  doc: SharedDocView;
  run: (action: () => Promise<unknown>) => Promise<string>;
  onDone: () => void;
  onCancel: () => void;
}) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const link = doc.kind === "link";
  // The fields as they were when the dialog opened: the list row behind `doc`
  // is re-read on every shared_doc delta, and diffing against it would send
  // an untouched field back over another writer's change.
  const [base] = useState(doc);
  const [name, setName] = useState(base.name);
  const [description, setDescription] = useState(base.description);
  const [url, setUrl] = useState(base.url);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(() => {
    if (!busy) onCancel();
  }, rootRef);
  // Read during the first render: the name field's autoFocus has already moved
  // focus by the time an effect runs.
  const opener = useRef(document.activeElement as HTMLElement | null);
  useEffect(() => {
    const target = opener.current;
    return () => {
      if (target && target.isConnected) target.focus();
    };
  }, []);

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (busy || name.trim() === "") return;
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

  return (
    <div ref={rootRef} className="confirm-modal" role="dialog" aria-modal="true" aria-label={link ? d.editLink : d.editInfo}>
      <form
        className="confirm-modal__box shared-docs__form shared-docs__info-dialog"
        data-testid="shared-doc-info-dialog"
        onSubmit={submit}
      >
        <div className="secrets__form-title">{link ? d.editLink : d.editInfo}</div>
        <label className="secrets__field">
          <span className="secrets__label">{d.nameLabel}</span>
          <input
            className="secrets__input"
            data-testid="shared-doc-info-name"
            maxLength={NAME_MAX_CHARS}
            autoFocus
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
        {link && (
          <label className="secrets__field">
            <span className="secrets__label">{d.urlLabel}</span>
            <input
              type="url"
              className="secrets__input"
              data-testid="shared-doc-info-url"
              maxLength={2048}
              value={url}
              onChange={(e) => setUrl(e.target.value)}
            />
          </label>
        )}
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
            disabled={busy || name.trim() === ""}
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

/** 最後修改 is the way into the doc's own page: its versions and description. */
function UpdatedButton({ text, onOpen }: { text: string; onOpen: () => void }) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  return (
    <button
      type="button"
      className="shared-docs__updated-open"
      data-testid="shared-doc-updated"
      aria-label={`${text} · ${d.openHistory}`}
      title={d.openHistory}
      onClick={onOpen}
    >
      {text}
    </button>
  );
}

/** 上傳新版本 | 永久刪除, in the same places on every row; a link cannot take
 * an upload, so its button stays put, dimmed, with a tooltip saying why. */
function RowActions({
  doc,
  isOwner,
  busy,
  onUpload,
  onDelete,
}: {
  doc: SharedDocView;
  isOwner: boolean;
  busy: boolean;
  onUpload: () => void;
  onDelete: () => void;
}) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const link = doc.kind === "link";
  const uploadTitle = link ? d.uploadVersionUnavailable : d.uploadVersion;
  return (
    <span className="shared-docs__actions">
      <button
        type="button"
        className="ts-icon-btn"
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
          className="ts-icon-btn ts-icon-btn--danger"
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

/** 新增連結: a URL and a name entered right on the page. */
function AddLinkForm({
  run,
  onDone,
  onCancel,
}: {
  run: (action: () => Promise<unknown>) => Promise<string>;
  onDone: () => void;
  onCancel: () => void;
}) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  const [url, setUrl] = useState("");
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    const target = url.trim();
    if (!target || busy) return;
    setBusy(true);
    setError("");
    const err = await run(() =>
      api.createSharedDoc({ kind: "link", name: name.trim() || defaultLinkName(target), description: "", url: target }),
    );
    setBusy(false);
    if (err) {
      setError(err);
      return;
    }
    onDone();
  }

  return (
    <form
      className="shared-docs__card shared-docs__link-form"
      data-testid="shared-docs-link-form"
      onSubmit={submit}
      onKeyDown={(e) => {
        if (e.key === "Escape" && !busy) {
          e.preventDefault();
          onCancel();
        }
      }}
    >
      <ExternalLinkIcon size={15} />
      <input
        type="url"
        className="secrets__input shared-docs__link-url"
        data-testid="shared-docs-link-url"
        aria-label={d.urlLabel}
        placeholder={d.linkUrlPlaceholder}
        maxLength={2048}
        autoFocus
        value={url}
        onChange={(e) => setUrl(e.target.value)}
      />
      <input
        className="secrets__input shared-docs__link-name"
        data-testid="shared-docs-link-name"
        aria-label={d.rename}
        placeholder={d.linkNamePlaceholder}
        maxLength={NAME_MAX_CHARS}
        value={name}
        onChange={(e) => setName(e.target.value)}
      />
      <button
        type="submit"
        className="inline-edit__iconbtn inline-edit__iconbtn--apply"
        data-testid="shared-docs-link-save"
        aria-label={t.common.apply}
        title={t.common.apply}
        disabled={busy || url.trim() === ""}
      >
        <CheckIcon size={14} />
      </button>
      <button
        type="button"
        className="inline-edit__iconbtn inline-edit__iconbtn--cancel"
        aria-label={t.common.cancel}
        title={t.common.cancel}
        disabled={busy}
        onClick={onCancel}
      >
        <CloseIcon size={14} />
      </button>
      {error && (
        <div className="shared-docs__link-error" role="alert" data-testid="shared-docs-link-error">
          {error}
        </div>
      )}
    </form>
  );
}

export function SharedDocsPage({ docId }: { docId?: string }) {
  const [, setRoute] = useHashRoute();
  const actorName = useActorName();
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
  return (
    <SharedDocList actorName={actorName} onOpen={(id) => setRoute({ page: "shared_docs", sharedDocId: id })} />
  );
}

type PickTarget = { mode: "create" } | { mode: "replace"; doc: SharedDocView };

function SharedDocList({
  actorName,
  onOpen,
}: {
  actorName: (id: string) => string;
  onOpen: (id: string) => void;
}) {
  const { t, msg } = useI18n();
  const d = t.sharedDocs;
  const { docs, loading, loadError, reload } = useSharedDocs(d.loadFailed);
  const run = useSharedDocAction(d.actionFailed);
  const [addingLink, setAddingLink] = useState(false);
  const [actionError, setActionError] = useState("");
  const [uploading, setUploading] = useState<string[]>([]);
  const [infoId, setInfoId] = useState<string | null>(null);
  const [preview, setPreview] = useState<SharedDocView | null>(null);
  const [editing, setEditing] = useState<SharedDocView | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<SharedDocView | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const [flashed, flash] = useFlash();
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
    if (target.mode === "replace") {
      const mismatch = kindMismatchMessage(target.doc.kind, file.name, d);
      if (mismatch) {
        setActionError(mismatch);
        return;
      }
    }
    const busyKey = target.mode === "replace" ? target.doc.id : "";
    setUploading((keys) => [...keys, busyKey]);
    let createdId = "";
    const err = await run(async () => {
      const attachmentId = await api.uploadAttachment(file, file.name);
      if (target.mode === "replace") {
        await api.replaceSharedDoc(target.doc.id, { attachmentId });
        return;
      }
      createdId = await api.createSharedDoc({
        kind: sharedDocKindForFile(file.name),
        name: defaultDocName(file.name),
        description: "",
        attachmentId,
      });
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
    if (createdId) setInfoId(createdId);
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
        <button
          type="button"
          className="secrets__btn shared-docs__bar-btn"
          data-testid="shared-docs-add-link"
          onClick={() => {
            setActionError("");
            setAddingLink(true);
          }}
        >
          <ExternalLinkIcon size={14} />
          {d.addLink}
        </button>
        <button
          type="button"
          className="secrets__btn secrets__submit shared-docs__bar-btn"
          data-testid="shared-docs-upload"
          disabled={uploading.includes("")}
          onClick={() => pick({ mode: "create" })}
        >
          <UploadIcon size={14} />
          {d.uploadFile}
        </button>
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

      {addingLink && (
        <AddLinkForm
          run={run}
          onCancel={() => setAddingLink(false)}
          onDone={() => {
            setAddingLink(false);
            void reload();
          }}
        />
      )}
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
        <div className="shared-docs__card shared-docs__table-wrap">
          <table className="shared-docs__table" data-testid="shared-docs-table">
            <thead>
              <tr>
                <th className="shared-docs__col-id">{d.colId}</th>
                <th className="shared-docs__col-file">{d.colFile}</th>
                <th className="shared-docs__col-updated">{d.colUpdated}</th>
                <th className="shared-docs__col-actions">{d.colActions}</th>
              </tr>
            </thead>
            <tbody>
              {docs.map((doc) => (
                <tr
                  key={doc.id}
                  className="shared-docs__row"
                  data-testid="shared-doc-row"
                  data-doc-id={doc.id}
                >
                  <td className="shared-docs__col-id">
                    <span className="shared-docs__id-cell">
                      <button
                        type="button"
                        className="lore-row__id-badge"
                        data-testid="shared-doc-id"
                        aria-label={msg.sharedDocCopyId(doc.id)}
                        title={msg.sharedDocCopyId(doc.id)}
                        onClick={async () => {
                          if (await copyText(doc.id)) flash(`id:${doc.id}`);
                        }}
                      >
                        {flashed === `id:${doc.id}` ? <CheckIcon size={13} /> : <LayersIcon size={13} />}#
                        {doc.id}
                      </button>
                    </span>
                  </td>
                  <td className="shared-docs__col-file">
                    <FileBlock
                      doc={doc}
                      onPreview={setPreview}
                      onEditInfo={(target) => {
                        setActionError("");
                        setInfoId(target.id);
                      }}
                    />
                  </td>
                  <td className="shared-docs__updated">
                    <UpdatedButton
                      text={msg.sharedDocUpdated(actorName(doc.updatedBy), formatAbsolute(doc.updatedTs, nowSecs))}
                      onOpen={() => onOpen(doc.id)}
                    />
                  </td>
                  <td className="shared-docs__col-actions">
                    <RowActions
                      doc={doc}
                      isOwner={isOwner}
                      busy={uploading.includes(doc.id)}
                      onUpload={() => pick({ mode: "replace", doc })}
                      onDelete={() => {
                        setDeleteError("");
                        setDeleteTarget(doc);
                      }}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
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
