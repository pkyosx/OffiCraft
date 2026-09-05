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
// 🔴 THE ROWS ARE FETCHED WHEN THE PANEL OPENS, and they are the ONLY reason
// this component talks to the server. T-66 took url / filename / mime / kind /
// is_image / attachment_id / created_by / created_ts off the task read (owner
// c-cd063427fb2f:「我覺得任務產物，只需要預設給標題跟ID, 有需要再透過另一隻去拿
// 就好了」), so a hydrated task carries an id+label INDEX and nothing this file
// can draw a row from. `api.listTaskArtifacts` is 「另一隻」, and it answers the
// WHOLE ticket in one call (owner c-f2d0fecb1168:「應該是指名任務？」) — which is
// exactly the shape this panel needs, because it opens onto every row at once.
//
// There is therefore NO SEED and no fallback to whatever the card had: the
// reader gets a LOADING state and then either rows or a visible failure. A
// silent blank panel would say 「還沒有產物」 about a task the badge has just
// told them has N of them, which is the one thing this must never do.
//
// File/image artifacts REUSE the shared attachment renderer (AttachmentStrip)
// and its preview overlay — deliverable #2's 「復用聊天附件那套顯示」. Every
// blob row, file or image, opens the one MarkdownPreviewOverlay (T-f014 retired
// the separate Lightbox). Links render as external-link chips.
// The owner may un-pin any artifact (a small × per row) when onRemoveArtifact
// is wired — the executing agent pins via MCP but does not remove.

import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import type { TaskArtifactView, ChatAttachmentView } from "../api/adapter";
import { formatAbsolute } from "../lib/dateFormat";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { AttachmentStrip } from "./AttachmentStrip";
import { TaskArtifactVersionsModal } from "./TaskArtifactVersionsModal";
import {
  CloseIcon,
  ExternalLinkIcon,
  PaperclipIcon,
  TrashIcon,
} from "./icons";

/** Project a file/image artifact onto the ChatAttachmentView the shared
 * AttachmentStrip renders (id/url/filename/mime/isImage — the exact reuse
 * surface).
 *
 * 🔴 THREE OF THOSE FIELDS NO LONGER ARRIVE AS FIELDS (T-92), and each is
 * derived here from one that does rather than dropped:
 *   · `filename` ← `a.name`, which the SERVER now derives (from the blob's own
 *     filename when the row has no stored name), so the `a.filename || a.label`
 *     chain that used to live here has moved server-side and this is its result.
 *   · `isImage` ← the mime's prefix. It was always exactly that; carrying both
 *     was one fact in two fields.
 *   · `backingAttachmentId` ← the tail of `url`, which for a file/image IS
 *     `/api/chat/attachment/{id}`. ⚠️ It is derived by PATH SHAPE, so a change
 *     to that route silently empties it — that is the cost of the id leaving
 *     the wire, and it is written down here rather than discovered later. */
function asAttachmentView(a: TaskArtifactView): ChatAttachmentView {
  return {
    id: a.id,
    backingAttachmentId: a.url.startsWith(BLOB_SERVE_PREFIX)
      ? a.url.slice(BLOB_SERVE_PREFIX.length)
      : "",
    url: a.url,
    filename: a.name,
    mime: a.mime,
    isImage: a.mime.startsWith("image/"),
  };
}

/** The serve path every file/image artifact's `url` is built from. */
const BLOB_SERVE_PREFIX = "/api/chat/attachment/";

/** What a LINK row is called on screen.
 *
 * 🔴 IT CAN NEVER RETURN "", and that is still the whole reason it exists —
 * but since T-92 the fallback chain lives on the SERVER (`artifactDisplayName`
 * in wire.go: stored name → blob filename → link target → id tail), so `a.name`
 * is already non-empty on any current server. The tail kept here is not
 * duplication of that: it is what this component does when the name arrives
 * empty ANYWAY — an older server, a hand-built fixture, a field that got
 * dropped somewhere in between. The old chain rendered an anchor with NO TEXT
 * in that case: invisible, unclickable, and silently one row short of the count
 * the badge promised. The id tail is the same identifier `artifactMetaLabel`
 * already prints beneath the name, so the worst case is a row named after
 * itself rather than a row named nothing. */
function artifactDisplayName(a: TaskArtifactView): string {
  return a.name || a.url || `#${a.id.replace(/^ta-/, "")}`;
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
  onRemoveArtifact,
}: {
  /** Only the id and the COUNT are read here. The card's own `artifacts` are an
   * id+label index since T-66 and carry nothing a row could be drawn from, so
   * this component deliberately does not take them — it fetches. */
  task: { id: string; artifactCount?: number };
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
  // The ref is on the ANCHOR, which wraps BOTH the badge and the popover, so a
  // mousedown on the badge is INSIDE and never closes-then-reopens — the badge's
  // own onClick stays the single toggle (the classic bug).
  //
  // 🔴 THE PREVIEW OVERLAY IS NO LONGER INSIDE THE ANCHOR (T-76cd). It used to
  // render in place, and containment alone then carried the ruling: a click on
  // its backdrop landed inside `anchorRef` and this popover stayed open. It now
  // portals to `document.body` so no ancestor stacking context can confine it
  // (see MarkdownPreviewOverlay.tsx), which makes `contains()` FALSE for every
  // point of the preview — backdrop, panel and close button alike. Containment
  // is therefore not the whole predicate any more: the overlay is matched by its
  // own root instead, and "inside" means inside EITHER. Dropping that half puts
  // the ruling straight back into the bug it was written against — one click on
  // the preview's grey backdrop and the artifacts panel is gone.
  //
  // `mousedown` (not `click`) matches the siblings and fires before the anchor
  // is torn down. Esc is handled by the popover itself (see ArtifactsPopover).
  useEffect(() => {
    if (!open) return;
    function onDown(e: MouseEvent) {
      const target = e.target as Node | null;
      if (anchorRef.current?.contains(target ?? null)) return;
      if (target instanceof Element && target.closest(".md-preview")) return;
      // 🔴 The SAME ruling, for the SAME reason, one surface later (T-60): the
      // version reader also portals to `document.body`, so `contains()` is
      // false for every point of it — panel, scrim and close button alike. Drop
      // this arm and one click anywhere in the version reader closes the
      // artifacts panel out from under it.
      if (target instanceof Element && target.closest(".ta-versions")) return;
      setOpen(false);
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
          onRemoveArtifact={onRemoveArtifact}
          onClose={() => setOpen(false)}
        />
      )}
    </span>
  );
}

function ArtifactsPopover({
  taskId,
  onRemoveArtifact,
  onClose,
}: {
  taskId: string;
  onRemoveArtifact?: (taskId: string, artifactId: string) => Promise<void>;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [artifacts, setArtifacts] = useState<TaskArtifactView[]>([]);
  // The reader has a PHASE, not just a list. "loaded" is what separates 「還沒有
  // 產物」 from 「還沒回來」, and `failed` is what separates BOTH of them from a
  // fetch that died: with one flat list, all three render as an empty panel and
  // the reader is told the task has no deliverables — which the badge they just
  // clicked has already denied.
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  /** The artifact whose version reader is open, if any. The reader is opened by
   * id rather than by row so it re-reads its own state from the server (it does
   * NOT diff against these rows — see TaskArtifactVersionsModal's header). */
  const [versionsFor, setVersionsFor] = useState<string | null>(null);
  // A pinned artifact's timestamp never ticks live — this only decides
  // whether formatAbsolute prefixes the year, so a plain render-time read is
  // fine (no state/interval needed, unlike RepliesPage's counters).
  const nowTs = Date.now() / 1000;

  // Fetch the full artifact set ON OPEN — this effect runs when the popover
  // mounts, which is the click on the badge — and keep it live while open (a
  // task delta fans when an artifact is pinned/removed), the ChatGalleryPanel
  // pattern. Nothing is fetched while the panel is shut.
  //
  // 🔴 A FAILURE IS SHOWN, NEVER SWALLOWED. The previous version logged to the
  // console and kept the seed, which was survivable when the rows already rode
  // the task read; now there is no seed, so a swallowed failure IS an empty
  // panel. A failed REFETCH keeps whatever is already on screen (stale rows beat
  // no rows) and adds the failure line above them.
  useEffect(() => {
    let alive = true;
    const refetch = () => {
      api
        .listTaskArtifacts(taskId)
        .then((rows) => {
          if (!alive) return;
          setArtifacts(rows);
          setFailed(false);
          setLoaded(true);
        })
        .catch((e) => {
          console.warn("ArtifactsPopover: listTaskArtifacts failed", e);
          if (alive) setFailed(true);
        });
    };
    refetch();
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "task") refetch();
    });
    return () => {
      alive = false;
      unsubscribe();
    };
  }, [taskId]);

  // Esc closes the popover — but only while it is the TOP layer. A preview
  // overlay opened from one of its rows registers above it, so the first Esc
  // reaches the overlay alone and this popover stays open. It no longer has to
  // ask whether an overlay is up (the old flag was read after the overlay had
  // already unmounted and cleared it).
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onClose, rootRef);

  // ONE list, grouped 檔案 → 圖片 → 連結 (the old tab order). File and image
  // rows share the AttachmentStrip renderer, so they are handed to it in a
  // single call; links are anchors and render after. The two wrappers this
  // leaves behind are dissolved in tasks.css so the rows read as one list —
  // the row rhythm lives on `.task-artifacts__body`, which is the flex column.
  const files = artifacts.filter((a) => a.kind === "file");
  const images = artifacts.filter((a) => a.kind === "image");
  const links = artifacts.filter((a) => a.kind === "link");
  const blobs = [...files, ...images];

  // Per-item extra: the owner un-pin ×. T-7bc2 (owner 2026-07-21): the .md
  // preview trigger moved OFF this cluster and onto the file chip itself
  // (click-the-filename-to-preview, the same contract the image thumbnail
  // already had), so this no longer
  // renders a separate 眼睛 button. Bound by artifact id (the strip carries
  // att views).
  const renderExtra = (att: ChatAttachmentView): ReactNode => {
    const art = artifacts.find((a) => a.id === att.id);
    // An image row is [thumbnail][name chip][actions] — the name chip is added
    // here rather than inside AttachmentStrip so the image branch (and its
    // click-to-preview) stays untouched. It gives the image row the SAME
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
          {art && (
            <VersionsButton
              artifact={art}
              onOpen={() => setVersionsFor(art.id)}
            />
          )}
          {onRemoveArtifact && (
            <RemoveButton taskId={taskId} artifactId={att.id} onRemove={onRemoveArtifact} />
          )}
        </span>
      </>
    );
  };

  return (
    <div
      ref={rootRef}
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
        {/* The three not-a-list states are DISTINCT lines, never one blank
            panel: 「載入中」 while the fetch is out, 「讀取失敗」 when it died,
            and 「還沒有產物」 only once a fetch has actually come back empty. */}
        {failed && (
          <div className="task-artifacts__empty" data-testid="task-artifacts-error">
            {t.tasks.artifacts.loadFailed}
          </div>
        )}
        {!loaded && !failed ? (
          <div className="task-artifacts__empty" data-testid="task-artifacts-loading">
            {t.tasks.artifacts.loading}
          </div>
        ) : artifacts.length === 0 ? (
          !failed && (
            <div className="task-artifacts__empty">{t.tasks.artifacts.empty}</div>
          )
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
                      title={artifactDisplayName(a)}
                      aria-label={`${artifactDisplayName(a)} — ${artifactMetaLabel(a, nowTs)} — ${t.tasks.artifacts.openLinkHint}`}
                    >
                      <ExternalLinkIcon size={14} />
                      <span className="task-artifacts__chip-text">
                        <span className="task-artifacts__chip-name">
                          {artifactDisplayName(a)}
                        </span>
                        <span className="task-artifacts__chip-meta">
                          {artifactMetaLabel(a, nowTs)}
                        </span>
                      </span>
                    </a>
                    <span className="task-artifacts__actions">
                      <VersionsButton
                        artifact={a}
                        onOpen={() => setVersionsFor(a.id)}
                      />
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
      {versionsFor && (
        <TaskArtifactVersionsModal
          taskId={taskId}
          artifactId={versionsFor}
          onClose={() => setVersionsFor(null)}
        />
      )}
    </div>
  );
}

/** The row's 「N版」 entry — present ONLY when there is more than one version to
 * look at (T-60). `versionCount` counts the LIVE version too, so 1 is "never
 * replaced" and 0 is an older server that never said; both mean there is
 * nothing to open, which is why the test is `> 1` and not `!== 1`. */
function VersionsButton({
  artifact,
  onOpen,
}: {
  artifact: TaskArtifactView;
  onOpen: () => void;
}) {
  const { t, msg } = useI18n();
  if (artifact.versionCount <= 1) return null;
  return (
    <button
      type="button"
      className="task-artifacts__action task-artifacts__versions"
      data-testid={`task-artifact-versions-${artifact.id}`}
      aria-label={t.tasks.artifacts.versionsEntry}
      title={t.tasks.artifacts.versionsEntry}
      onClick={onOpen}
    >
      {msg.taskArtifactVersionCount(artifact.versionCount)}
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
