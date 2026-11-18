// SharedDocsPage — 檔案庫: documents not tied to any task that every member
// reads by `sd-` id. The list is a table; #shared-docs/<id> opens one document.

import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { api, viewerIsOwner } from "../api";
import { authedAttachmentUrl } from "../api/http";
import { useMembers } from "../hooks/useMembers";
import { useOwnerDisplayName } from "../hooks/useOwnerName";
import { useSharedDocAction, useSharedDocs } from "../hooks/useSharedDocs";
import { copyText } from "../lib/clipboard";
import { formatAbsolute } from "../lib/dateFormat";
import { formatHash, useHashRoute } from "../lib/hashRoute";
import type { SharedDocView } from "../types";
import { ConfirmModal } from "./ConfirmModal";
import { SharedDocDetail } from "./SharedDocDetail";
import { SharedDocFormDialog } from "./SharedDocFormDialog";
import {
  CheckIcon,
  CopyIcon,
  ExternalLinkIcon,
  LayersIcon,
  PaperclipIcon,
  TrashIcon,
} from "./icons";
import "./lore.css";
import "./office.css";
import "./secrets.css";
import "./theme-settings.css";
import "./shared-docs.css";

/** The in-site address of one document — what 複製連結 puts on the clipboard. */
export function sharedDocLink(id: string): string {
  return `${window.location.origin}${window.location.pathname}${formatHash({
    page: "shared_docs",
    sharedDocId: id,
  })}`;
}

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

function KindMedia({ doc }: { doc: SharedDocView }) {
  const { t } = useI18n();
  const d = t.sharedDocs;
  if (doc.kind === "image") {
    return (
      <img
        className="shared-docs__thumb"
        data-testid="shared-doc-kind-image"
        src={authedAttachmentUrl(doc.url)}
        alt={d.kindImage}
      />
    );
  }
  const link = doc.kind === "link";
  return (
    <span
      className="shared-docs__kind"
      data-testid={`shared-doc-kind-${doc.kind}`}
      title={link ? d.kindLink : d.kindFile}
      aria-label={link ? d.kindLink : d.kindFile}
    >
      {link ? <ExternalLinkIcon size={15} /> : <PaperclipIcon size={15} />}
    </span>
  );
}

function FileBlock({ doc, onOpen }: { doc: SharedDocView; onOpen: (id: string) => void }) {
  return (
    <button
      type="button"
      className="shared-docs__file"
      data-testid="shared-doc-name"
      title={doc.description ? `${doc.name}\n${doc.description}` : doc.name}
      onClick={() => onOpen(doc.id)}
    >
      <KindMedia doc={doc} />
      <span className="shared-docs__file-text">
        <span className="shared-docs__name">{doc.name}</span>
        {doc.description && (
          <span className="shared-docs__file-description">{doc.description}</span>
        )}
      </span>
    </button>
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
    <SharedDocList
      actorName={actorName}
      onOpen={(id) => setRoute({ page: "shared_docs", sharedDocId: id })}
    />
  );
}

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
  const [creating, setCreating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<SharedDocView | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState("");
  const [flashed, flash] = useFlash();
  const isOwner = viewerIsOwner();
  const nowSecs = Math.floor(Date.now() / 1000);

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
          className="secrets__btn secrets__submit"
          data-testid="shared-docs-add"
          onClick={() => setCreating(true)}
        >
          {d.add}
        </button>
      </div>

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
                  onClick={(e) => {
                    if ((e.target as Element).closest("button, a")) return;
                    onOpen(doc.id);
                  }}
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
                      <button
                        type="button"
                        className="chat__share-btn"
                        data-testid="shared-doc-copy-link"
                        aria-label={flashed === `link:${doc.id}` ? d.linkCopied : d.copyLink}
                        title={flashed === `link:${doc.id}` ? d.linkCopied : d.copyLink}
                        onClick={async () => {
                          if (await copyText(sharedDocLink(doc.id))) flash(`link:${doc.id}`);
                        }}
                      >
                        {flashed === `link:${doc.id}` ? <CheckIcon size={13} /> : <CopyIcon size={13} />}
                      </button>
                    </span>
                  </td>
                  <td className="shared-docs__col-file">
                    <FileBlock doc={doc} onOpen={onOpen} />
                  </td>
                  <td className="shared-docs__updated" data-testid="shared-doc-updated">
                    {msg.sharedDocUpdated(actorName(doc.updatedBy), formatAbsolute(doc.updatedTs, nowSecs))}
                  </td>
                  <td className="shared-docs__col-actions">
                    {isOwner && (
                      <button
                        type="button"
                        className="ts-icon-btn ts-icon-btn--danger"
                        data-testid="shared-doc-delete"
                        aria-label={`${d.delete} ${doc.name}`}
                        title={d.delete}
                        onClick={() => {
                          setDeleteError("");
                          setDeleteTarget(doc);
                        }}
                      >
                        <TrashIcon size={15} />
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {creating && (
        <SharedDocFormDialog
          mode="create"
          run={run}
          onCancel={() => setCreating(false)}
          onDone={(id) => {
            setCreating(false);
            onOpen(id);
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
