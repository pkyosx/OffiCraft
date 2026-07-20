// components/TaskArtifactsPopover.tsx — the task card's 「產物 N」 badge + its
// artifact popover (T-3dc5). The badge renders ONLY when the count > 0 (0 ⇒ no
// badge, by construction — the empty-set assertion).
//
// T-49fb (owner 2026-07-20): the popover used to be TABBED (檔案 N / 圖片 N /
// 連結 N). The tabs are gone — a task's artifact set is small (the whole point
// of pinning is that it is the short list), so paging three near-empty tabs
// cost a click to discover a row that would have fitted on screen anyway. One
// list now shows every artifact at once. What survives is the per-KIND visual
// distinction, which was never the tabs' job: a file leads with the paperclip
// chip, an image with its 44px thumbnail, a link with the external-link glyph
// and the navigate hover accent. Within the list the kinds stay grouped in the
// old tab order (檔案 → 圖片 → 連結) so the eye still reads them as three
// families without a control to operate.
//
// File/image artifacts REUSE the shared attachment renderer (AttachmentStrip +
// Lightbox) and the .md preview overlay — deliverable #2's 「復用聊天附件那套
// 顯示」. A markdown file gets a 預覽 action (in-cockpit render) alongside its
// download; an image opens the Lightbox. Links render as external-link chips.
// The owner may un-pin any artifact (a small × per row) when onRemoveArtifact
// is wired — the executing agent pins via MCP but does not remove.

import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import type { TaskArtifactView, ChatAttachmentView } from "../api/adapter";
import { formatAbsolute } from "../lib/dateFormat";
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

/** T-6338: two pinned artifacts can carry the IDENTICAL filename (the same
 * demo file re-uploaded) — the row must still let the owner tell them apart
 * and trust the delete button they're about to click. `formatAbsolute` only
 * has minute resolution, which is not enough on its own (two uploads in the
 * same minute would print the same string); `a.id` is appended as a ref tag
 * so the two rows are NEVER character-identical regardless of how close the
 * timestamps land. This must be a GUARANTEE, not just unlikely-to-collide —
 * so this uses the id IN FULL (minus its `ta-` kind prefix, which is the same
 * on every artifact and buys nothing): it is exactly the identifier the
 * server already treats as this artifact's unique identity (`"ta-" +
 * newHexID(12)`, server/ocserverd/api_tasks.go), so truncating it would
 * reintroduce a collision risk on top of an already-unique value for no
 * reason. */
function artifactMetaLabel(a: TaskArtifactView, nowTs: number): string {
  return `${formatAbsolute(a.createdTs, nowTs)} · #${a.id.replace(/^ta-/, "")}`;
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
  const anchorRef = useRef<HTMLSpanElement>(null);

  // Click-outside dismissal (T-49fb, owner 2026-07-20: 「點其他地方都不會自動
  // 關閉,一定要點 X」). Same shape as every other popover in the cockpit —
  // TaskCard's 優先度/狀態 menus, MultiSelectFilter, OfficePage, ProfileDropdown
  // all run `mousedown` + `anchorRef.contains(e.target)`; there is no shared
  // hook to reuse, so this follows the majority spelling rather than inventing
  // a fourth one.
  //
  // The ref is on the ANCHOR, which wraps BOTH the badge and the popover. Two
  // things fall out of that and are the reason this doesn't need extra guards:
  //   · a mousedown on the badge is INSIDE, so it never closes-then-reopens —
  //     the badge's own onClick stays the single toggle (the classic bug).
  //   · the Lightbox and the MarkdownPreviewOverlay render INSIDE the popover
  //     (neither uses createPortal), so opening a preview and clicking its
  //     backdrop stays inside the anchor and leaves the popover open.
  // `mousedown` (not `click`) matches the siblings and fires before the anchor
  // is torn down. Esc is handled by the popover itself (see ArtifactsPopover).
  useEffect(() => {
    if (!open) return;
    function onDown(e: MouseEvent) {
      if (!anchorRef.current?.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  // 0 artifacts ⇒ NO badge (the empty-set assertion — nothing to show).
  if (count === 0) return null;

  return (
    <span className="task-artifacts-anchor" ref={anchorRef}>
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
  // A pinned artifact's timestamp never ticks live — this only decides
  // whether formatAbsolute prefixes the year, so a plain render-time read is
  // fine (no state/interval needed, unlike RepliesPage's counters).
  const nowTs = Date.now() / 1000;
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

  // ONE list, grouped 檔案 → 圖片 → 連結 (the old tab order). File and image
  // rows share the AttachmentStrip renderer, so they are handed to it in a
  // single call; links are anchors and render after. The two wrappers this
  // leaves behind are dissolved in tasks.css so the rows read as one list —
  // the row rhythm lives on `.task-artifacts__body`, which is the flex column.
  const files = artifacts.filter((a) => a.kind === "file");
  const images = artifacts.filter((a) => a.kind === "image");
  const links = artifacts.filter((a) => a.kind === "link");
  const blobs = [...files, ...images];

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
      art?.kind === "image" ? att.filename || t.tasks.artifacts.imageName : "";
    return (
      <>
        {imageName && art && (
          <span className="task-artifacts__chip" title={imageName}>
            <span className="task-artifacts__chip-text">
              <span className="task-artifacts__chip-name">{imageName}</span>
              <span className="task-artifacts__chip-meta">
                {artifactMetaLabel(art, nowTs)}
              </span>
            </span>
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
      <div className="task-artifacts__body">
        {artifacts.length === 0 ? (
          <div className="task-artifacts__empty">{t.tasks.artifacts.empty}</div>
        ) : (
          <>
            <AttachmentStrip
              attachments={blobs.map(asAttachmentView)}
              className="task-artifacts__strip"
              itemClassName="task-artifacts__item"
              imageClassName="task-artifacts__thumb"
              fileChipClassName="task-artifacts__chip"
              fileNameClassName="task-artifacts__chip-name"
              fileNameColClassName="task-artifacts__chip-text"
              onOpenImage={(src) => setLightboxSrc(src)}
              renderExtra={renderExtra}
              renderMeta={(att) => {
                const art = artifacts.find((a) => a.id === att.id);
                return art ? (
                  <span className="task-artifacts__chip-meta">
                    {artifactMetaLabel(art, nowTs)}
                  </span>
                ) : null;
              }}
            />
            {links.length > 0 && (
              <div className="task-artifacts__links">
                {links.map((a) => (
                  <div key={a.id} className="task-artifacts__item">
                    {/* `title` carries the FULL name (the label truncates). The
                        aria-label must keep the NAME in it — a bare 「開啟連結」
                        would override the link text and make every link row
                        announce identically to a screen reader (T-90df). The
                        visible text comes FIRST so the accessible name begins
                        with what the eye reads (WCAG 2.5.3 Label in Name, and
                        speech input matches on the visible words). T-6338: the
                        aria-label REPLACES all DOM content for AT, so the
                        `.task-artifacts__chip-meta` line (visible to sighted
                        users) must be folded in here too — otherwise two
                        same-named link rows still announce identically to a
                        screen reader even after the sighted fix. */}
                    <a
                      className="task-artifacts__chip task-artifacts__link"
                      href={a.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      title={a.label || a.url}
                      aria-label={`${a.label || a.url} — ${artifactMetaLabel(a, nowTs)} — ${t.tasks.artifacts.openLinkHint}`}
                    >
                      <ExternalLinkIcon size={14} />
                      <span className="task-artifacts__chip-text">
                        <span className="task-artifacts__chip-name">{a.label || a.url}</span>
                        <span className="task-artifacts__chip-meta">
                          {artifactMetaLabel(a, nowTs)}
                        </span>
                      </span>
                    </a>
                    <span className="task-artifacts__actions">
                      {onRemoveArtifact && (
                        <RemoveButton taskId={taskId} artifactId={a.id} onRemove={onRemoveArtifact} />
                      )}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </>
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
