// components/ChatGalleryPanel.tsx — the member's file & image gallery (M2-3,
// upgraded by Seth M2 acceptance batch 16). Opened from the chat header's
// gallery icon; collects EVERY attachment of the member's WHOLE conversation
// perspective — owner↔member BOTH directions AND the member's inter-agent
// threads (member↔other agent, both ways) — newest→oldest, split into an
// 「圖片」 and a 「檔案」 tab, each row labelled with its sender's display name
// + send time. Batch 18 adds an uploader filter chip row under the tabs —
// options derived from the ACTUAL rows' senders (never hardcoded), stacking
// with the image/file tab split.
//
// DATA SOURCE: the dedicated gallery query `listChatAttachments(memberId)`
// (`GET /api/chat/attachments?with=`) — the server flattens the rows and
// resolves each sender's display name from the roster (any status, so a
// dismissed sender still reads by name), so this component does no roster
// lookup and no client-side aggregation. READ-ONLY: unlike the thread's
// listChat, opening the gallery never advances a read watermark.
//
// OPEN BEHAVIOR (preview/download split, mirroring the server's disposition
// table on the server): a previewable mime
// (image/*, text/* — plain/markdown/html —, application/pdf) opens in a NEW TAB
// (the server serves those inline); anything else (zip and other opaque
// binaries) downloads (the server forces Content-Disposition: attachment).

import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import type { Member } from "../types";
import type { GalleryAttachment } from "../api/adapter";
import { api } from "../api";
import { authedAttachmentUrl } from "../api/http";
import { copyAttachmentShareLink } from "../lib/shareLink";
import { CheckIcon, CloseIcon, CopyIcon, FileTextIcon } from "./icons";

// The owner's sender id — the real backend stamps `from` from the verified JWT
// sub ("owner"); same constant as ChatArea's OWNER_ID (kept local to avoid an
// import cycle: ChatArea imports this component).
const OWNER_ID = "owner";

/** FE mirror of the server's preview/download split
 * (the server previewable-mime table): previewable mimes are served
 * inline → open in a new tab; the rest are forced downloads. */
export function isPreviewableMime(mime: string): boolean {
  return (
    mime.startsWith("image/") ||
    mime.startsWith("text/") ||
    mime === "application/pdf"
  );
}

/** The two gallery tabs: images vs every other file kind. */
type GalleryTab = "images" | "files";

/** Uploader filter sentinel: 「全部」 = no sender filtering. A real sender id
 * is never empty (the backend stamps `from` from the verified JWT sub). */
const ALL_SENDERS = "";

/** Format an epoch-second ts as a local "M/D hh:mm" — gallery history spans
 * days, so the bare hh:mm of the thread is not enough. Never fabricated. */
function formatDateTime(ts: number): string {
  return new Date(ts * 1000).toLocaleString([], {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function ChatGalleryPanel({
  member,
  resolveSender,
  onClose,
}: {
  member: Member;
  // ChatArea's nameOf: resolves an id the server left unnamed (an outsource
  // sender — GET /api/members excludes kind='outsource', so its from_name is
  // "") to the SAME codename label the thread bubbles show. Optional — absent
  // keeps the raw-id fallback.
  resolveSender?: (id: string) => string;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [entries, setEntries] = useState<GalleryAttachment[]>([]);
  const [tab, setTab] = useState<GalleryTab>("images");
  // Uploader filter (batch 18): the selected sender id, ALL_SENDERS = 「全部」.
  const [sender, setSender] = useState<string>(ALL_SENDERS);
  // Honest empty state: 「還沒有…」 only AFTER the fetch settles — never
  // flash it while loading.
  const [loaded, setLoaded] = useState(false);
  // The row whose share link was just copied (transient 「已複製」 feedback).
  const [shareCopiedId, setShareCopiedId] = useState<string | null>(null);

  // Copy the row's permanent share link (?sig= HMAC — lib/shareLink.ts);
  // feedback only after fetch + clipboard both succeeded.
  async function onCopyShareLink(attachmentId: string) {
    try {
      await copyAttachmentShareLink(attachmentId);
      setShareCopiedId(attachmentId);
      window.setTimeout(
        () => setShareCopiedId((cur) => (cur === attachmentId ? null : cur)),
        2000,
      );
    } catch (e) {
      console.warn("ChatGalleryPanel: copy share link failed", e);
    }
  }

  useEffect(() => {
    let alive = true;
    const refetch = () => {
      // The server-flattened member gallery: every conversation the member is
      // in (owner↔member + inter-agent), sender-labelled, newest→oldest.
      api
        .listChatAttachments(member.id)
        .then((rows) => {
          if (!alive) return;
          setEntries(rows);
          setLoaded(true);
          // If the selected uploader vanished from the fresh rows (e.g. after
          // a member switch), fall back to 「全部」 — never a stuck-blank filter.
          setSender((cur) =>
            cur !== ALL_SENDERS && !rows.some((r) => r.from === cur)
              ? ALL_SENDERS
              : cur,
          );
        })
        .catch((e) => console.warn("ChatGalleryPanel: load failed", e));
    };
    refetch();
    // Keep the open panel live: a new message may carry new attachments.
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic === "chat") refetch();
    });
    return () => {
      alive = false;
      unsubscribe();
    };
  }, [member.id]);

  // Esc closes the panel (bound while mounted — the panel only mounts open).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  // Sender label: the owner reads as 「我」; everyone else by the SERVER-resolved
  // display name (fromName). A sender the server left unnamed (an outsource
  // worker — never in the members roster) resolves through the caller's
  // resolveSender (codename chain), then falls back to its id — mirrors the
  // thread's roster fallback, never fabricated.
  const senderLabel = (e: GalleryAttachment): string =>
    e.from === OWNER_ID
      ? t.chat.me
      : e.fromName || resolveSender?.(e.from) || e.from;

  // Uploader filter options — derived from the ACTUAL rows' senders (never
  // hardcoded), deduped in row order (rows are newest→oldest), labelled with
  // the same senderLabel the list rows use (owner → 「我」, others → fromName,
  // fallback id — the raw internal id never renders when a name exists).
  const senders: { id: string; label: string }[] = [];
  for (const e of entries) {
    if (!senders.some((s) => s.id === e.from)) {
      senders.push({ id: e.from, label: senderLabel(e) });
    }
  }

  // The two dimensions STACK: the 圖片/檔案 tab split (same server-derived
  // isImage flag the thread bubbles use) AND the uploader filter.
  const shown = entries.filter(
    (e) =>
      (tab === "images" ? e.isImage : !e.isImage) &&
      (sender === ALL_SENDERS || e.from === sender),
  );

  return (
    <div
      className="chat__gallery"
      role="dialog"
      aria-label={t.chat.galleryLabel}
    >
      <div className="chat__gallery-header">
        <span className="chat__gallery-title">{t.chat.galleryLabel}</span>
        <button
          type="button"
          className="chat__gallery-close"
          aria-label={t.chat.galleryClose}
          onClick={onClose}
        >
          <CloseIcon size={16} />
        </button>
      </div>
      {/* 圖片 / 檔案 segmented tabs — same seg pattern as the preferences
       * switches (profile-dd__seg), muted by default, active gets the card
       * highlight. */}
      <div className="chat__gallery-tabs" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={tab === "images"}
          className={`chat__gallery-tab${
            tab === "images" ? " chat__gallery-tab--active" : ""
          }`}
          onClick={() => setTab("images")}
        >
          {t.chat.galleryTabImages}
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === "files"}
          className={`chat__gallery-tab${
            tab === "files" ? " chat__gallery-tab--active" : ""
          }`}
          onClick={() => setTab("files")}
        >
          {t.chat.galleryTabFiles}
        </button>
      </div>
      {/* Uploader filter chips (batch 18) — grey chips under the tabs, same
       * muted seg vocabulary; only rendered once loaded (never flash while
       * loading) and only when there is something to filter. Stacks with the
       * tab split above. */}
      {loaded && senders.length > 0 && (
        <div
          className="chat__gallery-senders"
          role="group"
          aria-label={t.chat.gallerySenderFilterLabel}
        >
          <button
            type="button"
            aria-pressed={sender === ALL_SENDERS}
            className={`chat__gallery-sender-chip${
              sender === ALL_SENDERS ? " chat__gallery-sender-chip--active" : ""
            }`}
            onClick={() => setSender(ALL_SENDERS)}
          >
            {t.chat.gallerySenderAll}
          </button>
          {senders.map((s) => (
            <button
              key={s.id}
              type="button"
              aria-pressed={sender === s.id}
              className={`chat__gallery-sender-chip${
                sender === s.id ? " chat__gallery-sender-chip--active" : ""
              }`}
              onClick={() => setSender(s.id)}
            >
              {s.label}
            </button>
          ))}
        </div>
      )}
      {!loaded ? null : shown.length === 0 ? (
        <div className="chat__gallery-empty">
          {tab === "images" ? t.chat.galleryEmptyImages : t.chat.galleryEmptyFiles}
        </div>
      ) : (
        <div className="chat__gallery-list">
          {shown.map((e) => {
            const href = authedAttachmentUrl(e.url);
            const previewable = isPreviewableMime(e.mime);
            return (
              <a
                key={`${e.messageId}-${e.id}`}
                className="chat__gallery-item"
                href={href}
                // Preview/download split: previewable → new tab (server serves
                // inline); else → download (server forces attachment).
                {...(previewable
                  ? {
                      target: "_blank",
                      rel: "noopener noreferrer",
                      title: t.chat.galleryPreviewHint,
                    }
                  : {
                      download: e.filename || undefined,
                      title: t.chat.galleryDownloadHint,
                    })}
              >
                {e.isImage ? (
                  <img
                    className="chat__gallery-thumb"
                    src={href}
                    alt={e.filename || t.chat.imageAlt}
                  />
                ) : (
                  <span className="chat__gallery-fileicon" aria-hidden>
                    <FileTextIcon size={20} />
                  </span>
                )}
                <div className="chat__gallery-meta">
                  <span className="chat__gallery-name">
                    {e.filename || t.chat.downloadAttachment}
                  </span>
                  <span className="chat__gallery-sub">
                    {senderLabel(e)} · {formatDateTime(e.ts)}
                  </span>
                </div>
                {/* 複製分享連結 — a button INSIDE the row anchor: stop the
                 * click from bubbling into the preview/download navigation. */}
                <button
                  type="button"
                  className="chat__share-btn chat__gallery-share"
                  aria-label={
                    shareCopiedId === e.id
                      ? t.chat.shareLinkCopied
                      : t.chat.copyShareLink
                  }
                  title={
                    shareCopiedId === e.id
                      ? t.chat.shareLinkCopied
                      : t.chat.copyShareLink
                  }
                  onClick={(ev) => {
                    ev.preventDefault();
                    ev.stopPropagation();
                    void onCopyShareLink(e.id);
                  }}
                >
                  {shareCopiedId === e.id ? (
                    <CheckIcon size={13} />
                  ) : (
                    <CopyIcon size={13} />
                  )}
                </button>
              </a>
            );
          })}
        </div>
      )}
    </div>
  );
}
