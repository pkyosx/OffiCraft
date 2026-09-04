// components/ChatGalleryPanel.tsx — the member's file & image gallery (M2-3,
// upgraded by Seth M2 acceptance batch 16). Opened from the chat header's
// gallery icon; collects EVERY attachment of the member's WHOLE conversation
// perspective — owner↔member BOTH directions AND the member's inter-agent
// threads (member↔other agent, both ways) — newest→oldest, split into an
// 「圖片」 and a 「檔案」 tab, each row labelled with its sender's display name
// + send time. Batch 18 added an uploader filter under the tabs — options
// derived from the ACTUAL rows' senders (never hardcoded), stacking with the
// image/file tab split. T-51 ② reshaped it from a wrapping chip row into a
// one-line checkbox dropdown and cut its options from the CURRENT TAB (they
// used to come from every row, so the 圖片 tab offered uploaders who had only
// ever sent files and ticking one answered with an empty gallery).
//
// DATA SOURCE: the dedicated gallery query `listChatAttachments(memberId)`
// (`GET /api/chat/attachments?with=`) — the server flattens the rows and
// resolves each sender's display name from the roster (any status, so a
// dismissed sender still reads by name), so this component does no roster
// lookup and no client-side aggregation. READ-ONLY: opening the gallery never
// advances a read watermark — which since T-48 is true of every read door on
// this API, so this is no longer a contrast with the thread's own listing.
//
// OPEN BEHAVIOR (preview/download split, mirroring the server's disposition
// table on the server): a previewable mime
// (image/*, text/* — plain/markdown/html —, application/pdf) opens in a NEW TAB
// (the server serves those inline); anything else (zip and other opaque
// binaries) downloads (the server forces Content-Disposition: attachment).

import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import type { Member } from "../types";
import type { GalleryAttachment, SseDelta } from "../api/adapter";
import { api } from "../api";
import { createDeltaSink } from "../lib/deltaSink";
import { authedAttachmentUrl } from "../api/http";
import { ChevronDownIcon, CloseIcon, FileTextIcon } from "./icons";
import { MarkdownPreviewOverlay } from "./MarkdownPreviewOverlay";

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

/**
 * Could THIS ONE chat delta change what the gallery renders?
 *
 * 🔴 THE INVARIANT IT ENCODES. The gallery query
 * (`DAL.ListChatAttachmentRefsFor`, `server/ocserverd/dal.go` — since T-51 it
 * reads the `chat_attachment_ref` index, not `chat_message.meta`, and the
 * predicate moved with it: `WHERE sender = ?` UNION `WHERE recipient = ? AND
 * sender <> recipient`)
 * keeps a message when — and only when — `m.Sender == with || m.Recipient == with`,
 * where `with` is the member id this panel was opened on. Every row it returns is
 * an attachment of one of those messages. ⇒ a CHAT DELTA naming this member at
 * NEITHER end cannot add, remove or re-order a single row here, and refetching
 * for it buys not a smaller answer but the SAME answer. That is the ordinary
 * case in this product: every agent↔agent line in the whole company used to cost
 * this open panel one `GET /api/chat/attachments`.
 *
 * ⚠️ THIS IS NOT THE OWNER PREDICATE — do not reach for `lib/ownerUnread.ts`.
 * That one asks `to === "owner"` because `UnreadCounts` counts only
 * `m.Recipient == reader` and the cockpit's reader is always the owner. This
 * endpoint is a DIFFERENT fold: BOTH ends count, and the end that matters is
 * THIS MEMBER, not the owner. An agent↔agent line moves no owner unread number
 * yet absolutely does change this gallery when one of those agents IS this
 * member — applying the owner predicate here would SKIP REAL WORK and leave the
 * panel stale.
 *
 * ⚠️ Attachment-awareness would be tighter still ("a message with no files
 * changes nothing here"), but `SseDelta` carries identity only — there is no
 * way to tell, so we do not guess.
 *
 * ⚠️ SCOPE OF THE CLAIM: this is about CHAT deltas only. The handler also
 * resolves each sender's display name from `ListMembers()`, so a RENAME changes
 * what it answers — and that arrives on the `member` topic, which this panel
 * subscribes to neither before nor after this change. Unchanged behaviour, not
 * something this predicate covers; do not read the paragraph above as "nothing
 * else can ever change this view".
 */
export function chatDeltaTouchesMember(d: SseDelta, memberId: string): boolean {
  if (d.topic !== "chat") return false;
  return d.names.from === memberId || d.names.to === memberId;
}

/** The two gallery tabs: images vs every other file kind. */
type GalleryTab = "images" | "files";

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

/** The uploader filter: one line closed, a fixed-height scrolling list of
 * checkboxes open (T-51 ②, the shape the owner named — a Jira-style checkbox
 * dropdown, reference screenshot pinned on the ticket).
 *
 * ⚠️ IT LISTS EVERY UPLOADER IT IS GIVEN, INCLUDING PEOPLE WHO HAVE LEFT. The
 * server resolves a sender's name from the roster at ANY status on purpose
 * (`api_chat.go`: "ANY roster status — dismissed still reads by name"), and
 * hiding a departed colleague here would make their files unreachable — the
 * gallery is where the owner goes to find an old file, and old files come from
 * people who have since gone. "Folded into a dropdown" is not "removed from the
 * list".
 *
 * ⚠️ NO SEARCH BOX. An earlier version put one inside the popover as a
 * shortcut; the owner removed it outright (2026-09-02, `c-8fa2806cb0e3`:
 * 「不需要有搜尋這功能」). That is consistent with the objection that shaped this
 * whole control — 「我怎麼會知道有誰，沒辦法打字」: you cannot type a name you have
 * not seen, so the list itself has to be scannable. Do not add one back. */
export function GallerySenderFilter({
  senders,
  selected,
  onChange,
}: {
  senders: { id: string; label: string; count: number }[];
  selected: ReadonlySet<string>;
  onChange: (next: ReadonlySet<string>) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);

  // Esc closes the POPOVER, not the gallery behind it: this layer nests inside
  // the panel's, so it takes the key first and the panel only sees the next one.
  useEscapeLayer(() => setOpen(false), boxRef, open);

  // A click anywhere else closes it. Bound only while open, and on the capture
  // phase so a click that unmounts its own target still counts as outside.
  useEffect(() => {
    if (!open) return;
    const onDocClick = (e: MouseEvent) => {
      if (!boxRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDocClick, true);
    return () => document.removeEventListener("mousedown", onDocClick, true);
  }, [open]);

  const toggle = (id: string) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange(next);
  };

  return (
    <div className="chat__gallery-senders" ref={boxRef}>
      <button
        type="button"
        className={`chat__gallery-sender-toggle${
          selected.size > 0 ? " chat__gallery-sender-toggle--active" : ""
        }`}
        aria-expanded={open}
        aria-haspopup="dialog"
        aria-label={t.chat.gallerySenderFilterLabel}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="chat__gallery-sender-toggle-text">
          {selected.size === 0
            ? t.chat.gallerySenderAll
            : t.chat.gallerySenderSelected(selected.size)}
        </span>
        <ChevronDownIcon size={14} />
      </button>
      {open && (
        <div
          className="chat__gallery-sender-menu"
          role="dialog"
          aria-label={t.chat.gallerySenderFilterLabel}
        >
          <div className="chat__gallery-sender-options">
            {senders.map((s) => (
              <label className="chat__gallery-sender-option" key={s.id}>
                <input
                  type="checkbox"
                  checked={selected.has(s.id)}
                  onChange={() => toggle(s.id)}
                />
                <span className="chat__gallery-sender-option-name">
                  {s.label}
                </span>
                <span className="chat__gallery-sender-option-count">
                  {s.count}
                </span>
              </label>
            ))}
          </div>
          {selected.size > 0 && (
            <button
              type="button"
              className="chat__gallery-sender-clear"
              onClick={() => onChange(new Set())}
            >
              {t.chat.gallerySenderClear}
            </button>
          )}
        </div>
      )}
    </div>
  );
}

export function ChatGalleryPanel({
  member,
  resolveSender,
  onClose,
}: {
  member: Member;
  // ChatArea's nameOf: resolves an id the server left unnamed to the SAME
  // codename label the thread bubbles show. Optional — absent keeps the raw-id
  // fallback.
  //
  // ⚠️ THE REASON WRITTEN HERE WAS WRONG, AND HAD BEEN FOR A WHILE. It said the
  // gallery handler's names table is `WHERE kind != 'outsource'` so no outsource
  // sender is ever named. Two things falsify that: the handler
  // (HandleListChatAttachments, api_chat.go) had ALREADY moved to the wider
  // roster read before T-14 項目 6, and that ticket then deleted the narrow query
  // altogether. The names table is keyed by member id and an outsource row IS a
  // member row, so a contractor sender normally DOES arrive named.
  //
  // 🔴 DO NOT DELETE THIS PROP ON THAT BASIS. What it covers is now "the server
  // left this id unnamed", whatever the cause — an id absent from the roster
  // read, or a row whose name is empty — and nobody has re-measured how often
  // that happens. Removing it re-prints raw ow- ids the moment it does.
  resolveSender?: (id: string) => string;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const [entries, setEntries] = useState<GalleryAttachment[]>([]);
  const [tab, setTab] = useState<GalleryTab>("images");
  // Uploader filter (batch 18, reshaped by T-51 ②): the SET of selected sender
  // ids — empty = 「全部」, because "nobody is ticked" already says it and a set
  // with a magic member in it does not.
  //
  // 🔴 ONE SET PER TAB, and that is not tidiness. The two tabs hold DIFFERENT
  // populations (the uploaders with an image are not the uploaders with a
  // file), so a tick belongs to the population it was made in. An earlier draft
  // kept one shared set and pruned it whenever the options changed; the
  // independent review showed what that costs: tick someone on 圖片, glance at
  // 檔案, come back — and the tick is gone, silently, because the glance pruned
  // it. Keeping them apart means a round trip changes nothing, and no pruning
  // is needed for the tab at all (rows disappearing entirely is a different
  // matter, and the refetch below still handles that).
  const [senderSelByTab, setSenderSelByTab] = useState<{
    images: ReadonlySet<string>;
    files: ReadonlySet<string>;
  }>({ images: new Set(), files: new Set() });
  const senderSel = senderSelByTab[tab];
  const setSenderSel = (next: ReadonlySet<string>) =>
    setSenderSelByTab((cur) => ({ ...cur, [tab]: next }));
  // Honest empty state: 「還沒有…」 only AFTER the fetch settles — never
  // flash it while loading.
  const [loaded, setLoaded] = useState(false);
  // T-51 ① — WHICH row the preview is showing, held as the row's own key
  // rather than the row object, because the list underneath is live: an SSE
  // refetch replaces every object, and a filter change re-slices the list. A
  // key survives both and re-resolves to a POSITION, which is what paging needs
  // and what a stored object cannot give. If the row is gone from the current
  // list (deleted, or filtered out), the lookup fails and the overlay closes —
  // the honest outcome, and the only one that cannot show a stale file.
  const [previewKey, setPreviewKey] = useState<string | null>(null);

  useEffect(() => {
    // A NEW MEMBER IS A NEW GALLERY, so the ticks do not travel. They were made
    // about one person's uploaders; carried across, they filter rows they were
    // never about — measured by the independent review: tick 「我」 on member A's
    // 圖片, switch to member B who has images but none from me, and the panel
    // says 「還沒有圖片」 about a gallery that is not empty. The refetch prune
    // below cannot catch it (it only drops ids absent from EVERY row, and 「我」
    // is in plenty of B's rows — just not B's images).
    setSenderSelByTab({ images: new Set(), files: new Set() });
    // T-48 R10-7: THIS PANEL'S OWN INVARIANT, not a fix for an observed
    // symptom. A key minted for one member's row would still resolve against
    // the rows on screen until the new member's fetch replaced them — but from
    // the one caller in the tree that cannot happen: `galleryOpen` is keyed on
    // the visit, so the switching render closes the panel and unmounts it
    // before this effect could ever run with a changed `member.id`. It is kept
    // as defence for the day somebody renders this panel outside that gate;
    // removing it reddens `ChatGalleryPanel.test.tsx` and nothing else, and
    // that is the honest extent of what it holds.
    setPreviewKey(null);
  }, [member.id]);

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
          // Drop any selected uploader that vanished from the fresh rows (e.g.
          // after a member switch) — never a stuck filter that matches nothing.
          // Dropping ALL of them lands back on 「全部」 by construction. This is
          // about rows that no longer EXIST, which is why it reads the whole
          // answer rather than one tab's slice.
          setSenderSelByTab((cur) => {
            if (cur.images.size === 0 && cur.files.size === 0) return cur;
            const alive = new Set(rows.map((r) => r.from));
            const keep = (sel: ReadonlySet<string>) => {
              const kept = new Set([...sel].filter((id) => alive.has(id)));
              return kept.size === sel.size ? sel : kept;
            };
            const images = keep(cur.images);
            const files = keep(cur.files);
            return images === cur.images && files === cur.files
              ? cur
              : { images, files };
          });
        })
        .catch((e) => console.warn("ChatGalleryPanel: load failed", e));
    };
    refetch();
    // Keep the open panel live: a new message may carry new attachments — but
    // ONLY a CHAT DELTA naming this member can change what the server answers
    // for it (a rename does too, but that is the `member` topic — see the SCOPE
    // note there, and note this panel subscribes to neither before nor after).
    // See
    // `chatDeltaTouchesMember` above.
    const unsubscribe = api.subscribeEvents(
      createDeltaSink((batch) => {
        if (!batch.topics.has("chat")) return;
        // Named NOTHING (a resync, a null payload, or a transport that supplies
        // no delta at all) is the honest "you may have missed anything" — never
        // reason about names there, just re-pull.
        if (batch.unnamed) {
          refetch();
          return;
        }
        // Whole-burst, not per-delta: one refetch answers "what is the gallery
        // now", so a mixed burst (one unrelated line AND one of ours, same
        // microtask) still refetches exactly once.
        if (batch.deltas.some((d) => chatDeltaTouchesMember(d, member.id))) {
          refetch();
        }
      }),
    );
    return () => {
      alive = false;
      unsubscribe();
    };
  }, [member.id]);

  // Esc closes the panel — while it is the TOP layer. The preview overlay it
  // renders registers above it, so an open preview takes the first Esc and the
  // gallery is not asked to guess whether one is up.
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(onClose, rootRef);

  // Sender label: the owner reads as 「我」; everyone else by the SERVER-resolved
  // display name (fromName). A sender the server left unnamed (an outsource
  // worker — never in the members roster) resolves through the caller's
  // resolveSender (codename chain), then falls back to its id — mirrors the
  // thread's roster fallback, never fabricated.
  const senderLabel = (e: GalleryAttachment): string =>
    e.from === OWNER_ID
      ? t.chat.me
      : e.fromName || resolveSender?.(e.from) || e.from;

  // Everything the CURRENT TAB holds. Both the uploader options and the list
  // are cut from this one slice, and that is the fix, not a tidy-up:
  //
  // 🔴 THE OPTIONS USED TO BE BUILT FROM `entries` — every row, both tabs —
  // while the list applied the tab. Two different populations, so on 「圖片」 the
  // filter offered every uploader who had ever sent ANY file, and ticking one
  // who had only ever sent zips answered with an empty gallery. Measured on the
  // owner's own line (Kyle, 2026-09-02): 114 uploaders in the row, but only 48
  // have an image — 66 of them were dead options. A filter whose options do not
  // come from the same population as its results is not a filter.
  const inTab = entries.filter((e) =>
    tab === "images" ? e.isImage : !e.isImage,
  );

  // Uploader options — derived from the ACTUAL rows' senders (never hardcoded),
  // deduped in row order (rows are newest→oldest), labelled with the same
  // senderLabel the list rows use (owner → 「我」, others → fromName, fallback id
  // — the raw internal id never renders when a name exists). The count is what
  // makes the long tail legible: 「只丟過 1 個檔案的人」 is most of this list.
  // Keyed by id rather than scanned per row: this runs on every render, and a
  // linear `find` inside the loop is rows x uploaders — on the corpus this
  // ticket was measured against (2,200 rows, 114 uploaders) that is six figures
  // of comparisons for a list that has not changed.
  const senderById = new Map<
    string,
    { id: string; label: string; count: number }
  >();
  for (const e of inTab) {
    const seen = senderById.get(e.from);
    if (seen) seen.count += 1;
    else
      senderById.set(e.from, { id: e.from, label: senderLabel(e), count: 1 });
  }
  const senders = [...senderById.values()];
  // 🔴 ORDER DECIDES WHETHER A LIST OF A HUNDRED PEOPLE IS USABLE. Row order
  // (newest first) reads as no order at all once you are scrolling past twenty
  // names. BY NAME — the owner's ruling (2026-09-02, `c-6143bd5a861d`:
  // 「依照名字排序就好」), which overturned the first version's by-volume order.
  // What it buys: a reader who knows WHO they want can jump straight to the
  // letter, which is the half a removed search box was doing. What it costs,
  // named so nobody "fixes" it back: the busiest uploaders (「我」 included) no
  // longer float to the top — the per-row count is what still tells you who
  // sent a lot. Ties keep row order, so equal names stay in newest-first order.
  senders.sort((a, b) => a.label.localeCompare(b.label, "zh-Hant"));

  // The two dimensions STACK: the 圖片/檔案 tab split (same server-derived
  // isImage flag the thread bubbles use) AND the uploader filter. No ticks = no
  // uploader filtering.
  const shown =
    senderSel.size === 0 ? inTab : inTab.filter((e) => senderSel.has(e.from));

  // The row key is the same one the list items are keyed by — one definition,
  // used by both, so a paging lookup can never disagree with what is rendered.
  const rowKey = (e: GalleryAttachment): string => `${e.messageId}-${e.id}`;
  const previewIndex =
    previewKey === null ? -1 : shown.findIndex((e) => rowKey(e) === previewKey);
  const preview = previewIndex >= 0 ? shown[previewIndex] : null;

  return (
    <div
      ref={rootRef}
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
      {/* Uploader filter (T-51 ②) — ONE LINE when closed, whatever the number of
       * uploaders. The chip row this replaces had no cap and no scroll
       * container, so it grew a line per uploader: measured on a 2,200-file
       * corpus it stood 1,168px tall inside a 696px panel and pushed the file
       * list clean off the screen. The owner rejected the two cheaper shapes
       * himself — collapse-and-expand (「然後呢？」: the expanded thing is the
       * same wall) and search-only (「我怎麼會知道有誰，沒辦法打字」: you cannot
       * type a name you have not seen) — and named this one. */}
      {loaded && senders.length > 0 && (
        <GallerySenderFilter
          senders={senders}
          selected={senderSel}
          onChange={setSenderSel}
        />
      )}
      {!loaded ? null : shown.length === 0 ? (
        <div className="chat__gallery-empty">
          {/* TWO EMPTIES, AND THEY ARE NOT THE SAME SENTENCE. 「還沒有圖片」 is a
           * statement about the gallery; with a filter on it is a statement
           * about the filter, and saying the first while the second is true
           * tells the reader their files are gone. Reachable without a member
           * switch: a refetch can remove a ticked uploader's images while
           * their files remain, so the tick survives the prune with nothing
           * left to show on this tab. */}
          {senderSel.size > 0
            ? t.chat.galleryEmptyFiltered
            : tab === "images"
              ? t.chat.galleryEmptyImages
              : t.chat.galleryEmptyFiles}
        </div>
      ) : (
        <div className="chat__gallery-list">
          {shown.map((e) => {
            const href = authedAttachmentUrl(e.url);
            return (
              <div
                key={rowKey(e)}
                className="chat__gallery-item"
                role="button"
                tabIndex={0}
                title={t.chat.galleryPreviewHint}
                onClick={() => setPreviewKey(rowKey(e))}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    setPreviewKey(rowKey(e));
                  }
                }}
              >
                {e.isImage ? (
                  <img
                    className="chat__gallery-thumb"
                    src={href}
                    alt={e.filename || t.chat.imageAlt}
                    /* Every thumbnail byte is a `chat_attachment.data` blob
                     * read, so an eager row costs a DB round trip. Deferring
                     * cuts that to what the browser decides it needs — which is
                     * more than the visible rows (it prefetches ahead), not
                     * only them. It does NOT reduce how many rows render.
                     *
                     * How much it prefetches is the browser's call, so the
                     * number is not portable: measured twice at 54 requests on
                     * open (1440x900, 1,200 image rows, no scrolling), and an
                     * independent reviewer got 57. #405's commit message says
                     * 300 — that one could not be reproduced under any stated
                     * conditions; treat it as wrong, not as a third data point.
                     * Scrolling the whole list to the bottom does fetch all
                     * 1,200, one request each, with none dropped. */
                    loading="lazy"
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
              </div>
            );
          })}
        </div>
      )}
      {preview && (
        <MarkdownPreviewOverlay
          title={preview.filename || t.chat.downloadAttachment}
          url={preview.url}
          attachmentId={preview.id}
          mime={preview.mime}
          /* T-51 ① — paging is over the list the reader is LOOKING AT (both
           * filters applied), not over every attachment the member ever sent:
           * stepping out of the tab or the uploader they picked would answer a
           * question nobody asked. */
          pager={{
            index: previewIndex,
            total: shown.length,
            onGo: (i) => setPreviewKey(rowKey(shown[i])),
          }}
          onClose={() => setPreviewKey(null)}
        />
      )}
    </div>
  );
}
