// components/DocumentHistoryEntry.tsx — 版本紀錄 的唯一入口 (T-1f39, owner
// 2026-07-31).
//
// 前身是 DocumentHistoryCard：一張永遠掛在編輯面下方的清單卡。owner 的裁定把它
// 收進編輯列——「可以讓版本資訊直接預設在編輯的時候，出現在其中一個就好嘛？可能
// 是那個重置的位置，可以考慮改個名字…有點選的時候再打 API 就可以，也不會有不知道
// 版本是對哪一個 section 的狀況，所有頁面都這樣處理」——並在兩個選項裡挑了
// 「取代：重置鈕改成版本入口，『初始版本』當清單裡的一項」。
//
// 三件事因此成立，而且是這支檔案在守的：
//   1. 版本紀錄只在**編輯模式**出現，就站在 重置 原本的位置（同一顆 `.doc-btn`）。
//      站在哪一個編輯器裡，就是哪一份文件的歷史——section 的歧義消失在版面上，
//      不是靠標題文案解釋掉的。
//   2. **點了才打 API**。`useDocumentHistory` 的 enabled 就是這件事本身；沒點開
//      的編輯面一通請求都不發。
//   3. **重置變成清單最後一項「初始版本」**。有 seed 預設的文件（`onReset`）才有
//      這一項；沒有的（自訂角色、任務手冊）不能長出一個按了會 404 的入口。它現在
//      是重置的唯一入口，所以走跟還原一模一樣的破壞性確認框。
//
// 清單列本身沿用原本卡片的內容（時間／修改者／逐欄預覽／超上限的不可還原徽章／
// 當時為預設內容），點一列進 DocumentHistoryModal 讀、比、還原，讀完可以退回清單。
//
// T-40f0（owner rc-28885813e065 ①）:「初始版本」那一列**行為與其他版本完全一致**。
// 在此之前它是唯一一列點下去直接跳還原確認的——因為 seed 的內容根本沒交到前端，
// 伺服器只在「重置之後」才吐出它。現在它一樣先進 DocumentHistoryModal（帶 `seed`），
// 先看得到內容與差異，還原仍在同一個破壞性確認框後面。
// 🔴 兩件事刻意沒變：那一列站的位置（入口不變、不會更難找），以及「初始版本不做別人
//    的人質」——它仍然在 GET 版本清單失敗時照樣長出來，而且 seed 內容自己那個 GET
//    失敗時，還原照樣按得下去（modal 的 `seedUnavailable` 誠實說明看不到，而不是
//    假裝這個版本是空白的）。

import { useRef, useState } from "react";
import { useI18n } from "../i18n";
import type { DocumentHistoryEntryView, DocumentKind } from "../types";
import {
  useDocumentHistory,
  useDocumentRevision,
} from "../hooks/useDocumentHistory";
import { useDocumentSeed } from "../hooks/useDocumentSeed";
import { useMembers } from "../hooks/useMembers";
import { useServerSettings } from "../hooks/useServerSettings";
import { OWNER_ACTOR_ID, actorDisplayName } from "../lib/actorLabel";
import { capForKind, contentSizes, docCapBlockedFields } from "../api/docCap";
import type { DocCaps } from "../api/docCap";
import { documentHasContent } from "../lib/docHistoryFields";
import { formatAbsolute } from "../lib/dateFormat";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { DocumentHistoryModal } from "./DocumentHistoryModal";
import { CloseIcon, LayersIcon } from "./icons";
// The list wears the modal shell (`.doc-hist-modal*`) and the row atoms that
// still live in the settings sheet (`.doc-hist__*`, `.doc-btn`, `.set-badge`),
// so it imports BOTH — the styleOwnership rule: draw a sheet's classes, own its
// import.
import "./doc-hist-modal.css";
import "./settings.css";

export interface DocumentHistoryEntryProps {
  kind: DocumentKind;
  /** "global" | role key | "<role_key>::<task_type>" | type_key. */
  docKey: string;
  /** Names the document the list belongs to, in the list's own header. */
  title: string;
  /** Overrides the one-line note under that header (the 任務定義 page needs it:
   * its 用途／識別鍵 edits are not versioned at all). */
  note?: string;
  /**
   * The LIVE document values, under the same wire field names the revisions
   * use. TWO jobs: the `before` side of the server size cap, and the `after`
   * side of the modal's diff. Pass `undefined` while the doc is still loading:
   * the list then abstains from marking rather than judging every revision
   * against an empty document, and the diff says it cannot compare yet instead
   * of claiming the whole document was deleted. See api/docCap.ts.
   */
  currentContent?: Record<string, string>;
  /**
   * True when THIS document can be deleted whole from the cockpit (a task
   * manual, a custom role). Such a delete keeps no history, so the list states
   * that limit — otherwise it reads as a general undo. Left false where no
   * delete flow exists (global context, seed roles, lessons): a footnote that
   * is false for the document on screen is worse than no footnote.
   */
  docDeletable?: boolean;
  /** Re-read the document this entry sits in, after a successful restore. */
  onRestored?: () => Promise<unknown> | void;
  /**
   * Restore the FILE SEED — the document's shipped default. Present only where
   * one exists; where it does not (custom roles, task manuals) the 初始版本 row
   * must not appear, because the server 404s that reset. This is now the ONLY
   * reset affordance in the cockpit.
   */
  onReset?: () => Promise<unknown> | void;
  /** Dead while the surrounding editor has a write in flight. */
  disabled?: boolean;
}

export function DocumentHistoryEntry({
  kind,
  docKey,
  title,
  note,
  currentContent,
  docDeletable,
  onRestored,
  onReset,
  disabled,
}: DocumentHistoryEntryProps) {
  const { t, msg } = useI18n();
  const [open, setOpen] = useState(false);
  // What the reader is showing: one retained revision, or the shipped default.
  // A discriminated union rather than two booleans — "both at once" is not a
  // state this surface can be in, so it must not be representable.
  const [reading, setReading] = useState<
    | { kind: "version"; version: DocumentHistoryEntryView }
    | { kind: "seed" }
    | null
  >(null);

  // 點了才載入 — 這是 owner 裁定的字面意思，不是效能微調。
  const { versions, loading, error, restore } = useDocumentHistory(
    kind,
    docKey,
    { enabled: open }
  );
  // Identity only — this list never shows presence or unread counts, so the
  // light roster is the right pull (no refetch when anyone speaks in chat).
  const { members } = useMembers({ light: true });
  // The cap is a SETTING (T-3aeb), so the un-restorable marking has to follow
  // the LIVE value: judging by the shipped default would grey out revisions the
  // server accepts the moment the owner raises it. `undefined` until it loads,
  // which makes the marking abstain (api/docCap.ts).
  //
  // T-ae38 (widened by T-30f1): one value PER SEGMENT, and which one judges this list is a property of
  // `kind`. Handing one number down would have judged a Duty revision by the
  // Learning cap — a 4,000-char role definition would read as restorable while
  // the server refuses it at 1,000.
  const settings = useServerSettings().settings;
  const docCaps: DocCaps | undefined = settings ? {
    duty: settings.docCapCharsDuty,
    insight: settings.docCapCharsInsight,
    learning: settings.docCapCharsLearning,
    manualSop: settings.docCapCharsManualSop,
    manualLearnings: settings.docCapCharsManualLearnings,
    systemInteraction: settings.docCapCharsSystemInteraction,
    bootSequence: settings.docCapCharsBootSequence,
    offboard: settings.docCapCharsOffboard,
  } : undefined;
  // The shipped default, so the 初始版本 row can be READ and COMPARED like every
  // other row (T-40f0). Fetched only where that row exists (`onReset`) and only
  // once the list is open — same 「點了才打 API」 rule the history itself follows.
  const seedDoc = useDocumentSeed(kind, docKey, {
    enabled: open && onReset !== undefined,
  });
  // T-1170 「要看內文時真的去取內文」: the list carries no text, so the reader's
  // document is fetched for the revision that was actually picked — and only
  // then. `null` while the list is up or the 初始版本 row is being read (that
  // row's content is the seed, which has its own read above).
  const revision = useDocumentRevision(
    kind,
    docKey,
    reading?.kind === "version" ? reading.version.id : null
  );

  const listRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(() => setOpen(false), listRef, open && reading === null);

  const actorLine = (actorId: string) =>
    // 座艙自己寫的版本掛的是 owner token 的 sub，名冊裡永遠查不到 —— 沒有這一支
    // 分岔，owner 在自己的修改上看到的只有 "owner" 四個字母 (裁定 2026-07-31)。
    actorId === OWNER_ACTOR_ID
      ? t.user
      : msg.docHistoryActor(actorDisplayName(actorId, members), actorId);

  const nowSecs = Date.now() / 1000;
  const fieldLabel = (name: string) =>
    (t.settings.historyField as Record<string, string>)[name] ?? name;

  function closeAll() {
    setOpen(false);
    setReading(null);
  }

  return (
    <>
      <button
        type="button"
        className="doc-btn"
        data-testid={`doc-history-entry-${kind}`}
        disabled={disabled}
        onClick={() => setOpen(true)}
      >
        {t.settings.historyTitle}
      </button>

      {open && reading === null && (
        <div
          ref={listRef}
          className="doc-hist-modal"
          data-testid="doc-history-list"
          role="dialog"
          aria-modal="true"
          aria-label={title}
          onClick={() => setOpen(false)}
        >
          <div
            className="doc-hist-modal__panel"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="doc-hist-modal__header">
              <span className="doc-hist__title">
                <LayersIcon size={15} className="doc-hist__icon" />
                <span>{title}</span>
              </span>
              <div className="doc-hist-modal__actions">
                <button
                  type="button"
                  className="doc-hist-modal__close"
                  data-testid="doc-history-list-close"
                  aria-label={t.settings.historyClose}
                  onClick={() => setOpen(false)}
                >
                  <CloseIcon size={16} />
                </button>
              </div>
            </div>

            <div className="doc-hist-modal__body">
              <div className="doc-hist__note">
                {note ?? t.settings.historySub}
              </div>
              {loading && (
                <div className="doc-hist__empty">
                  {t.settings.historyLoading}
                </div>
              )}
              {/* A failed load is reported, but it does NOT take 初始版本 with
                * it. That row is the only reset affordance left in the cockpit
                * and it needs no server data at all — gating it on this GET
                * made 重置 the hostage of an unrelated request. */}
              {!loading && error && (
                <div className="set-error">{t.settings.historyError}</div>
              )}
              {!loading && !error && versions.length === 0 && !onReset && (
                <div className="doc-hist__empty">{t.settings.historyEmpty}</div>
              )}
              {!loading && (versions.length > 0 || onReset) && (
                <ul className="doc-hist__list">
                  {versions.map((v) => {
                    const when = formatAbsolute(v.createdTs, nowSecs);
                    const hasContent = documentHasContent(kind, v.sizes);
                    // A revision the server WOULD refuse is still listed —
                    // hiding it would deny the owner the one place that content
                    // still exists. It is marked and told why HERE, and the
                    // modal it opens repeats the verdict with a dead control.
                    const blockedFields = docCapBlockedFields(
                      kind,
                      v.sizes,
                      currentContent,
                      docCaps
                    );
                    const blocked = blockedFields.length > 0;
                    return (
                      <li
                        className={`doc-hist__item${blocked ? " doc-hist__item--blocked" : ""}`}
                        key={v.id}
                        data-testid={`doc-history-item-${v.id}`}
                        data-blocked={blocked ? "true" : undefined}
                      >
                        {/* The WHOLE row is the control — a blocked revision
                          * opens too, because reading it is precisely what is
                          * still possible when restoring it is not. */}
                        {/* role="button" rather than a real <button>: the row's
                          * body is a <dl> of previews, which a button may not
                          * contain (phrasing content only). */}
                        <div
                          className="doc-hist__row"
                          data-testid={`doc-history-open-${v.id}`}
                          role="button"
                          tabIndex={0}
                          title={t.settings.historyOpen}
                          onClick={() => setReading({ kind: "version", version: v })}
                          onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === " ") {
                              e.preventDefault();
                              setReading({ kind: "version", version: v });
                            }
                          }}
                        >
                          <div className="doc-hist__meta">
                            <span className="doc-hist__when">{when}</span>
                            <span className="doc-hist__actor">
                              {t.settings.historyByLabel}{" "}
                              {actorLine(v.actorId)}
                            </span>
                            {v.tombstoned && (
                              <span className="set-badge">
                                {t.settings.historyDefaultBadge}
                              </span>
                            )}
                            {blocked && (
                              <span className="set-badge set-badge--blocked">
                                {t.settings.historyBlockedBadge}
                              </span>
                            )}
                            <span className="doc-hist__open">
                              {t.settings.historyOpen}
                            </span>
                          </div>
                          {blocked && (
                            <div
                              className="doc-hist__blocked"
                              data-testid={`doc-history-blocked-${v.id}`}
                            >
                              {msg.docHistoryBlockedReason(
                                blockedFields.map(fieldLabel),
                                (docCaps && capForKind(kind, docCaps)) ?? 0
                              )}
                            </div>
                          )}
                          {/* NO content preview here (owner 2026-07-31:
                            * 「點了應該先挑選要看哪個版本就好，不用一次顯示多個
                            * 版本出來」). This list is a PICKER: several long
                            * revisions previewed at once is the wall of text
                            * the reader has to scroll past to reach the one
                            * they came for. The content lives one click deeper,
                            * where it gets the whole panel. The only thing kept
                            * beside the identity line is a revision that CANNOT
                            * be restored, which has to say so before the click,
                            * not after. */}
                          {/* Two reasons a row previews nothing, and they are
                            * NOT the same statement (T-40f0 node 11). A
                            * tombstoned revision's text column is empty because
                            * the text lives in the seed file — restoring it
                            * puts the document back ON that default, so
                            * 「（當時是空白內容）」 is false there. That line is
                            * kept for the version that really did store an
                            * empty string. */}
                          {!hasContent && (
                            <div className="doc-hist__empty">
                              {v.tombstoned
                                ? t.settings.historyDefaultContent
                                : t.settings.historyNoContent}
                            </div>
                          )}
                        </div>
                      </li>
                    );
                  })}

                  {/* 初始版本 — the file seed, at the BOTTOM because it is the
                    * oldest thing this document has ever been, and the list is
                    * newest-first. Only where a seed exists. */}
                  {onReset && (
                    <li
                      className="doc-hist__item doc-hist__item--seed"
                      data-testid="doc-history-seed"
                    >
                      <div
                        className="doc-hist__row"
                        data-testid="doc-history-seed-open"
                        role="button"
                        tabIndex={0}
                        title={t.settings.historyOpen}
                        onClick={() => setReading({ kind: "seed" })}
                        onKeyDown={(e) => {
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            setReading({ kind: "seed" });
                          }
                        }}
                      >
                        <div className="doc-hist__meta">
                          <span className="doc-hist__when">
                            {t.settings.historySeedTitle}
                          </span>
                          {/* 「檢視這個版本」, the same affordance every other row
                            * offers — the row no longer promises a restore it is
                            * not about to perform (T-40f0). */}
                          <span className="doc-hist__open">
                            {t.settings.historyOpen}
                          </span>
                        </div>
                        <div className="doc-hist__seed-note">
                          {t.settings.historySeedNote}
                        </div>
                      </div>
                    </li>
                  )}
                </ul>
              )}
              {docDeletable && (
                <p
                  className="doc-hist__scope"
                  data-testid="doc-history-scope-note"
                >
                  {t.settings.historyDeleteNote}
                </p>
              )}
            </div>
          </div>

        </div>
      )}

      {reading && (
        <DocumentHistoryModal
          kind={kind}
          // The seed still rides in as a PSEUDO-version so the reader, the diff
          // and the cap verdict stay one code path. Its timestamp is the honest
          // "never" (`seed` makes the modal name it 初始版本 instead of
          // rendering a fabricated 修改者 line), and its content is `undefined`
          // only while the seed GET is in flight or failed — which is the case
          // `seedUnavailable` exists to state out loud rather than let it read
          // as 「這個版本沒有內容」.
          createdTs={reading.kind === "version" ? reading.version.createdTs : 0}
          // A retained revision's flag comes off the DIRECTORY row; the seed's
          // comes off its own document, which carries `tombstoned` because
          // restoring it is what puts the doc back ON the default.
          tombstoned={
            reading.kind === "version"
              ? reading.version.tombstoned
              : seedDoc.content?.tombstoned === "true"
          }
          // Same split for the cap verdict's input: the list already measured
          // the retained revision, and the seed is measured from the document
          // in hand (it is not a list row, so nothing measured it upstream).
          sizes={
            reading.kind === "version"
              ? reading.version.sizes
              : contentSizes(seedDoc.content ?? {})
          }
          content={
            reading.kind === "version" ? revision.content : seedDoc.content
          }
          contentLoading={reading.kind === "version" && revision.loading}
          seed={reading.kind === "seed"}
          seedUnavailable={
            reading.kind === "seed" && seedDoc.content === undefined
          }
          // The shipped default is ALSO what a TOMBSTONED retained revision
          // restores to (T-40f0 node 11) — the reader and the diff use it as
          // that revision's effective content, so the diff describes the state
          // the restore actually leaves behind instead of announcing that the
          // whole document would be deleted.
          seedContent={seedDoc.content}
          actorLine={
            reading.kind === "version" ? actorLine(reading.version.actorId) : ""
          }
          currentContent={currentContent}
          docCaps={docCaps}
          // Reading one version is a step INTO the list, so there is a step
          // back out of it — closing is what leaves the history altogether.
          onBack={() => setReading(null)}
          onClose={closeAll}
          onRestore={async () => {
            if (reading.kind === "seed") {
              // The reset, unchanged in everything but where its confirmation
              // lives: same call, same destructiveness, and it deliberately does
              // NOT run `onRestored` — the reset's own caller already re-reads
              // the document and leaves edit mode (SettingsPage's doReset).
              await onReset?.();
              return;
            }
            await restore(reading.version.id);
            await onRestored?.();
          }}
        />
      )}
    </>
  );
}
