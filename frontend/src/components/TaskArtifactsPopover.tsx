// components/TaskArtifactsPopover.tsx — the task card's 「產物 N」 badge + its
// artifact popover (T-3dc5, owner-chosen A design: a count badge in the badge
// row + a tabbed popover). The badge renders ONLY when the count > 0 (0 ⇒ no
// badge, by construction — the empty-set assertion). Clicking it opens a
// popover styled like the chat 檔案與圖片 gallery: three segmented tabs 檔案 /
// 圖片 / 連結.
//
// LINK gets its OWN tab (not folded into 檔案): a link has no blob and a wholly
// different interaction (open in a new tab) from a file (download) or an image
// (Lightbox), so one tab per interaction model keeps each tab uniform — the
// 「較自然」 choice the spec left to the implementer.
//
// File/image artifacts REUSE the shared attachment renderer (AttachmentStrip +
// Lightbox) and the .md preview overlay — deliverable #2's 「復用聊天附件那套
// 顯示」. A markdown file gets a 預覽 action (in-cockpit render) alongside its
// download; an image opens the Lightbox. Links render as external-link chips.
// The owner may un-pin any artifact (a small × per row) when onRemoveArtifact
// is wired — the executing agent pins via MCP but does not remove.

import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import type { TaskArtifactView, ChatAttachmentView } from "../api/adapter";
import { AttachmentStrip, Lightbox } from "./AttachmentStrip";
import {
  MarkdownPreviewOverlay,
  isMarkdownAttachment,
} from "./MarkdownPreviewOverlay";
import {
  CloseIcon,
  ExternalLinkIcon,
  EyeIcon,
  PaperclipIcon,
  TrashIcon,
} from "./icons";

type ArtifactTab = "files" | "images" | "links";

/** Project a file/image artifact onto the ChatAttachmentView the shared
 * AttachmentStrip renders (id/url/filename/mime/isImage — the exact reuse
 * surface). */
function asAttachmentView(a: TaskArtifactView): ChatAttachmentView {
  return {
    id: a.id,
    url: a.url,
    filename: a.filename || a.label,
    mime: a.mime,
    isImage: a.isImage,
  };
}

export function TaskArtifactsBadge({
  task,
  onHydrate,
  onRemoveArtifact,
}: {
  task: { id: string; artifactCount?: number; artifacts?: TaskArtifactView[] };
  /** Hydrate the FULL task to read its artifact rows (the light list carries
   * only the count). Reused from the card's existing hydrate seam. */
  onHydrate: (id: string) => Promise<{ artifacts?: TaskArtifactView[] }>;
  /** Owner/admin un-pin. Absent ⇒ the popover is display-only (no × affordance). */
  onRemoveArtifact?: (taskId: string, artifactId: string) => Promise<void>;
}) {
  const { t } = useI18n();
  const count = task.artifactCount ?? 0;
  const [open, setOpen] = useState(false);

  // 0 artifacts ⇒ NO badge (the empty-set assertion — nothing to show).
  if (count === 0) return null;

  return (
    <span className="task-artifacts-anchor">
      <button
        type="button"
        className="task-badge task-badge--artifacts"
        data-testid="task-artifacts-badge"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={t.tasks.artifacts.open}
        title={t.tasks.artifacts.open}
        onClick={(e) => {
          e.stopPropagation();
          setOpen((v) => !v);
        }}
      >
        <PaperclipIcon size={12} />
        {t.tasks.artifacts.badge} {count}
      </button>
      {open && (
        <ArtifactsPopover
          taskId={task.id}
          seed={task.artifacts}
          onHydrate={onHydrate}
          onRemoveArtifact={onRemoveArtifact}
          onClose={() => setOpen(false)}
        />
      )}
    </span>
  );
}

function ArtifactsPopover({
  taskId,
  seed,
  onHydrate,
  onRemoveArtifact,
  onClose,
}: {
  taskId: string;
  /** Artifacts already in hand (an expanded card's hydrated detail); the popover
   * still refetches for liveness, but this avoids an empty first frame. */
  seed?: TaskArtifactView[];
  onHydrate: (id: string) => Promise<{ artifacts?: TaskArtifactView[] }>;
  onRemoveArtifact?: (taskId: string, artifactId: string) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [artifacts, setArtifacts] = useState<TaskArtifactView[]>(seed ?? []);
  const [tab, setTab] = useState<ArtifactTab>("files");
  // Open overlays (mutually exclusive; the caller-owned state pattern).
  const [lightboxSrc, setLightboxSrc] = useState<string | null>(null);
  const [preview, setPreview] = useState<{ title: string; url: string } | null>(
    null,
  );

  // Fetch the full artifact set on open, and keep it live while open (a task
  // delta fans when an artifact is pinned/removed) — the ChatGalleryPanel
  // pattern. A hydrate failure keeps the seed rather than blanking.
  useEffect(() => {
    let alive = true;
    const refetch = () => {
      onHydrate(taskId)
        .then((full) => {
          if (alive) setArtifacts(full.artifacts ?? []);
        })
        .catch((e) => console.warn("ArtifactsPopover: hydrate failed", e));
    };
    refetch();
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "task") refetch();
    });
    return () => {
      alive = false;
      unsubscribe();
    };
  }, [taskId, onHydrate]);

  // Esc closes the popover (only when no overlay is capturing Esc itself).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !lightboxSrc && !preview) onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, lightboxSrc, preview]);

  const files = artifacts.filter((a) => a.kind === "file");
  const images = artifacts.filter((a) => a.kind === "image");
  const links = artifacts.filter((a) => a.kind === "link");
  const shown = tab === "files" ? files : tab === "images" ? images : links;

  // Per-item extra actions on the file/image strip: 預覽 for a .md file, plus
  // the owner un-pin ×. Bound by artifact id (the strip carries att views).
  const renderExtra = (att: ChatAttachmentView): ReactNode => {
    const art = artifacts.find((a) => a.id === att.id);
    const canPreview =
      art?.kind === "file" && isMarkdownAttachment(att.mime, att.filename);
    // An image row is [thumbnail][name chip][actions] — the name chip is added
    // here rather than inside AttachmentStrip so the image branch (and its
    // Lightbox click) stays untouched. It gives the image row the SAME
    // three-part shape as a file/link row, and the same hover-for-full-name.
    // Both filename and label are optional server-side, so fall back the way
    // the file branch does — an image row must never lose its chip, or it
    // stops matching the other two kinds.
    const imageName =
      art?.kind === "image" ? att.filename || t.chat.imageAlt : "";
    return (
      <>
        {imageName && (
          <span className="task-artifacts__chip" title={imageName}>
            <span className="task-artifacts__chip-name">{imageName}</span>
          </span>
        )}
        <span className="task-artifacts__actions">
        {canPreview && (
          <button
            type="button"
            className="task-artifacts__action"
            aria-label={t.tasks.artifacts.previewHint}
            title={t.tasks.artifacts.previewHint}
            onClick={() => setPreview({ title: att.filename, url: att.url })}
          >
            <EyeIcon size={13} />
          </button>
        )}
          {onRemoveArtifact && <RemoveButton taskId={taskId} artifactId={att.id} onRemove={onRemoveArtifact} />}
        </span>
      </>
    );
  };

  return (
    <div
      className="task-artifacts"
      role="dialog"
      aria-label={t.tasks.artifacts.panelTitle}
      onClick={(e) => e.stopPropagation()}
    >
      <div className="task-artifacts__header">
        <span className="task-artifacts__title">{t.tasks.artifacts.panelTitle}</span>
        <button
          type="button"
          className="task-artifacts__close"
          aria-label={t.tasks.artifacts.close}
          onClick={onClose}
        >
          <CloseIcon size={15} />
        </button>
      </div>
      {/* 檔案 / 圖片 / 連結 segmented tabs — the gallery seg vocabulary. */}
      <div className="task-artifacts__tabs" role="tablist">
        <TabButton label={`${t.tasks.artifacts.tabFiles} ${files.length}`} active={tab === "files"} onClick={() => setTab("files")} />
        <TabButton label={`${t.tasks.artifacts.tabImages} ${images.length}`} active={tab === "images"} onClick={() => setTab("images")} />
        <TabButton label={`${t.tasks.artifacts.tabLinks} ${links.length}`} active={tab === "links"} onClick={() => setTab("links")} />
      </div>
      <div className="task-artifacts__body">
        {shown.length === 0 ? (
          <div className="task-artifacts__empty">
            {tab === "files"
              ? t.tasks.artifacts.emptyFiles
              : tab === "images"
                ? t.tasks.artifacts.emptyImages
                : t.tasks.artifacts.emptyLinks}
          </div>
        ) : tab === "links" ? (
          <div className="task-artifacts__links">
            {links.map((a) => (
              <div key={a.id} className="task-artifacts__item">
                {/* `title` carries the FULL name (the label truncates). The
                    aria-label must keep the NAME in it — a bare 「開啟連結」
                    would override the link text and make every link row
                    announce identically to a screen reader (T-90df). */}
                <a
                  className="task-artifacts__chip task-artifacts__link"
                  href={a.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  title={a.label || a.url}
                  aria-label={`${t.tasks.artifacts.openLinkHint}: ${a.label || a.url}`}
                >
                  <ExternalLinkIcon size={14} />
                  <span className="task-artifacts__chip-name">{a.label || a.url}</span>
                </a>
                <span className="task-artifacts__actions">
                  {onRemoveArtifact && (
                    <RemoveButton taskId={taskId} artifactId={a.id} onRemove={onRemoveArtifact} />
                  )}
                </span>
              </div>
            ))}
          </div>
        ) : (
          <AttachmentStrip
            attachments={shown.map(asAttachmentView)}
            className="task-artifacts__strip"
            itemClassName="task-artifacts__item"
            imageClassName="task-artifacts__thumb"
            fileChipClassName="task-artifacts__chip"
            fileNameClassName="task-artifacts__chip-name"
            onOpenImage={(src) => setLightboxSrc(src)}
            renderExtra={renderExtra}
          />
        )}
      </div>
      <Lightbox src={lightboxSrc} onClose={() => setLightboxSrc(null)} />
      {preview && (
        <MarkdownPreviewOverlay
          title={preview.title}
          url={preview.url}
          onClose={() => setPreview(null)}
        />
      )}
    </div>
  );
}

function TabButton({ label, active, onClick }: { label: string; active: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      className={`task-artifacts__tab${active ? " task-artifacts__tab--active" : ""}`}
      onClick={onClick}
    >
      {label}
    </button>
  );
}

function RemoveButton({
  taskId,
  artifactId,
  onRemove,
}: {
  taskId: string;
  artifactId: string;
  onRemove: (taskId: string, artifactId: string) => Promise<void>;
}) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  return (
    <button
      type="button"
      className="task-artifacts__action task-artifacts__remove"
      aria-label={t.tasks.artifacts.remove}
      title={t.tasks.artifacts.remove}
      disabled={busy}
      onClick={() => {
        if (!window.confirm(t.tasks.artifacts.removeConfirm)) return;
        setBusy(true);
        void onRemove(taskId, artifactId).finally(() => setBusy(false));
      }}
    >
      <TrashIcon size={13} />
    </button>
  );
}
