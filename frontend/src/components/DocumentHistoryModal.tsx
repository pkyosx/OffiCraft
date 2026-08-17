// components/DocumentHistoryModal.tsx — reading ONE retained revision (T-1f39).
//
// The list row used to be the whole story: a clamped preview per field and a
// restore button. You could not read a version, and you could not see what
// restoring it would actually change. Both now live here, behind a click on the
// row.
//
// TWO PANES, one surface (owner 2026-07-31):
//   - DEFAULT — the version's own content, RENDERED as markdown through the
//     shared Markdown.tsx. These documents are written in markdown and read
//     everywhere else in the cockpit as markdown; the raw-source view would be
//     a different document from the one the owner wrote.
//   - TOP-RIGHT toggle → the line-by-line diff against the CURRENT content
//     (DiffView, raw text). Raw is the point on this side: a rendered diff
//     cannot show which LINE moved, and the line numbers are the answer to
//     "what would restoring this undo".
//
// The diff's `after` is what the SERVER currently stores, not the draft in the
// editor above — `historyDiffNote` says so on screen, because the two differ
// exactly when the owner is mid-edit and that is when the diff is read.
//
// 初始版本 USES THE SAME READER (T-40f0, owner rc-28885813e065 ①). The list's
// bottom row — the document's shipped default — used to go STRAIGHT to a restore
// confirmation, because the seed text was something the server only ever handed
// back AFTER a reset had already overwritten the document. So the ONE entry whose
// restore is least reversible was also the only one nobody could look at first.
// It now arrives here as a pseudo-version (`seed`), reads and diffs through the
// very same panes, and restores through the very same confirmation — the row
// itself did not move, so the entry is no harder to find than it was.
//
// A TOMBSTONED REVISION IS NOT AN EMPTY ONE (T-40f0 node 11, owner screenshot).
// `tombstoned="true"` is the overlay's way of saying "follow the shipped
// default" — the row's text column is EMPTY in the database because the text
// lives in the seed file, not because anybody ever wrote an empty document.
// Reading that empty column as literal content made all three panes lie, and
// the worst of them was the diff: a 285-line document rendered as "every line
// goes away", i.e. 「還原＝清空」, next to a destructive button. What restoring
// such a revision ACTUALLY does is write a tombstone back
// (`restoreDocumentHistory` → `Tombstoned: true`), which folds to the shipped
// default. So the effective content of a tombstoned revision IS the seed, and
// this file uses it for BOTH panes.
//
// 🔴 THE ONE CRITERION: what the diff says must equal the state a restore
// leaves behind. That is why the substitution happens here rather than in
// `documentFields`/`comparedFieldNames` — those are shape functions with no
// notion of a document's default — and why it is here rather than in each host
// card: every kind whose snapshot carries a `tombstoned` flag goes through this
// one reader.
//
// WHAT THAT REACH ACTUALLY IS, stated honestly rather than as a slogan:
//   * `role_definition` and `global_context` — verified end to end (a retained
//     tombstone opened through DocumentHistoryEntry against the shared mock).
//   * `insight` — verified end to end since `api/mock.ts` learned to serve its
//     seed; before that the mock 404'd where the server answers, so the cockpit
//     could only ever trade one wrong screen for a differently wrong one.
//   * `lessons` — NOT reachable today, and saying "one fix covers lessons too"
//     was a blank cheque. `Lessons.Tombstoned` has exactly one writer that can
//     set it true (`restoreDocumentHistory`), and both ordinary write doors
//     (api_roles.go's replace and patch) hard-code `false`. There is no
//     `reset_lessons` route and no reset tool, so the state cannot bootstrap
//     itself. If anyone ever adds one, a tombstoned lessons revision lands
//     straight in the "the default cannot be read" branch below — `lessons` has
//     no `onReset`, so its host never fetches a seed to substitute.
//
// 🔴 The CAP verdict deliberately still judges THIS REVISION's own sizes, NOT
// the effective content: the server's restore checks `content["text"]` too (it
// writes the tombstone, not the seed text), so judging the seed here would grey
// out revisions the server accepts — the exact direction api/docCap.ts refuses
// to be wrong in. Since T-1170 those sizes arrive from the DIRECTORY row, so
// the verdict is also ready before the revision's text is.
//
// RESTORE MOVED IN HERE and is reachable nowhere else. Everything the row-level
// button carried came with it, unchanged: it is DESTRUCTIVE so it still goes
// through ConfirmModal; a failure surfaces the SERVER's own message and leaves
// both dialogs open; and a revision the server would refuse on size is inert
// here too — marked, told why, and its control dead.

import { useRef, useState } from "react";
import { useI18n } from "../i18n";
import type { DocumentKind } from "../types";
import { capForKind, docCapBlockedFields } from "../api/docCap";
import type { DocCaps } from "../api/docCap";
import { ApiError } from "../api/errors";
import { comparedFieldNames, documentFields } from "../lib/docHistoryFields";
import { formatAbsolute } from "../lib/dateFormat";
import { useEscapeLayer } from "../lib/useEscapeLayer";
import { ConfirmModal } from "./ConfirmModal";
import { DiffView } from "./DiffView";
import { Markdown } from "./Markdown";
import { CloseIcon } from "./icons";
// Its own shell sheet, plus settings.css for the two SHARED atoms it wears —
// `.doc-btn` (the cockpit's document button) and `.set-badge`. Importing the
// sheet whose classes it draws with is the rule styleOwnership.test.ts exists
// for: free-riding on the card's import would leave this modal unstyled the
// moment it is mounted from anywhere else (the CT story already is).
import "./doc-hist-modal.css";
import "./settings.css";

type Pane = "content" | "diff";

export function DocumentHistoryModal({
  kind,
  createdTs,
  tombstoned,
  sizes,
  content: revisionContent,
  contentLoading,
  actorLine,
  currentContent,
  docCaps,
  onBack,
  onClose,
  onRestore,
  seed,
  seedUnavailable,
  seedContent,
}: {
  kind: DocumentKind;
  /** When the revision was retained (`0` for the seed — nobody wrote it). */
  createdTs: number;
  /** This revision was a TOMBSTONE — "follow the shipped default". Read off the
   * directory row rather than off the text, so it is known before (and without)
   * the content read. */
  tombstoned: boolean;
  /** The revision's per-field sizes — what the cap verdict judges. It comes
   * from the DIRECTORY (T-1170), so a revision the server would refuse is
   * marked and its 還原 dead from the first frame, rather than only once the
   * text arrives. */
  sizes: Record<string, number>;
  /**
   * The revision's own field→value snapshot — `undefined` while the per-revision
   * read is in flight or failed. The panes then say so (`contentLoading`
   * separates the two); NEITHER may call the revision empty, which is a
   * different and false claim to make beside a destructive button.
   */
  content?: Record<string, string>;
  /** The content read has not finished. Distinguishes 「載入中」 from 「讀不到」;
   * both leave 還原 live, because restoring needs nothing from this client. */
  contentLoading?: boolean;
  /** Who wrote this revision, already resolved to "name (id)" — or the bare id
   * when the roster cannot name them. Resolved by the host card, which is the
   * one holding the roster: a modal that pulled its own would fetch the whole
   * roster every time a row is clicked. */
  actorLine: string;
  /** The LIVE document under the same wire field names — the diff's `after`
   * side AND the size cap's `before`. `undefined` while the host's document is
   * still loading: the diff then says so instead of comparing against nothing,
   * and the cap verdict abstains exactly as it does on the list. */
  currentContent?: Record<string, string>;
  /** The live document size caps (the `doc.cap_chars.*` settings, T-3aeb /
   * T-ae38 / T-30f1) — not a constant on either side any more, and since T-ae38
   * not one number either: `docCapBlockedFields` picks the one that judges THIS kind.
   * Resolved by the host for the same reason `actorLine` is: a modal pulling
   * its own copy would refetch the settings every time a row is clicked.
   * `undefined` while it loads, which makes the cap verdict abstain rather than
   * judge by the shipped default — a cap can only ever be RAISED, so the
   * default can only ever mark a revision the server would have accepted. */
  docCaps?: DocCaps;
  /** Step back to the version LIST this reader was opened from (T-1f39, owner
   * 2026-07-31). Omitted where there is no list behind it. Distinct from
   * `onClose`, which leaves the history altogether: a reader you can only exit
   * by closing makes comparing two versions a round trip through the editor. */
  onBack?: () => void;
  onClose: () => void;
  /** Restore THIS revision over the live document. Rejects on failure — the
   * modal maps the rejection to the message it shows and stays open. */
  onRestore: () => Promise<void>;
  /**
   * TRUE when `version` is not a retained revision but the document's SHIPPED
   * DEFAULT (初始版本). It has no timestamp and no actor — nobody wrote it — so
   * the header names it instead of pretending someone did, and the confirmation
   * uses the reset's own wording.
   */
  seed?: boolean;
  /**
   * `seed` only: the default's content could not be read. Neither pane may then
   * claim the default is EMPTY (that is a different, and false, statement), so
   * both say so instead — while restore stays live, because putting the document
   * back on its default needs nothing from this client.
   */
  seedUnavailable?: boolean;
  /**
   * The document's SHIPPED DEFAULT under the revision field names — what a
   * tombstoned revision actually restores to. `undefined` while the seed GET is
   * in flight, when it failed, or where this document ships no default at all
   * (the host only fetches it where a reset exists). A tombstoned revision then
   * says its content cannot be read rather than claiming it is empty, which is
   * the same honesty `seedUnavailable` buys the 初始版本 row.
   */
  seedContent?: Record<string, string>;
}) {
  const { t, msg } = useI18n();
  const [pane, setPane] = useState<Pane>("content");
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [restoreError, setRestoreError] = useState<string | null>(null);

  // Esc closes — as a LAYER, so the confirm dialog rendered inside this root
  // takes the key while it is open and this surface only sees it once that
  // dialog is gone. While a restore is in flight the key is swallowed: a
  // committed destructive action must not lose its dialog mid-request.
  const rootRef = useRef<HTMLDivElement>(null);
  useEscapeLayer(() => {
    if (!busy) onClose();
  }, rootRef);

  const when = formatAbsolute(createdTs, Date.now() / 1000);
  /** What the diff's `-` side is called, and what the confirmation names. The
   * seed has no timestamp to name it by — 初始版本 IS its name. */
  const versionLabel = seed
    ? t.settings.historySeedTitle
    : msg.docHistoryVersionLabel(when);
  // One confirmation code path, two sentences: the retained-revision wording
  // names the timestamp it is going back to; the seed's says the current content
  // is overwritten by the shipped default.
  const confirmBody = seed
    ? t.settings.historySeedConfirm
    : msg.docHistoryRestoreConfirm(when);
  const fieldLabel = (name: string) =>
    (t.settings.historyField as Record<string, string>)[name] ?? name;

  // See the header note: the tombstone is a POINTER at the shipped default, so
  // the content this version would restore to is the seed, not the empty text
  // column the wire carries. `seed` is excluded because the 初始版本
  // pseudo-version's `content` ALREADY IS the default — substituting there would
  // make that row depend on a second copy of what it is holding, and it has its
  // own `seedUnavailable` for the case where that copy is missing.
  const effectiveContent = tombstoned && !seed ? seedContent : revisionContent;
  // Neither pane may call a tombstoned revision empty when the default could
  // not be read — that is a different, and false, statement. This branch is
  // LOAD-BEARING, not a formality: without it a tombstoned retained revision
  // whose seed GET failed falls straight back to the empty text column and the
  // diff paints the whole live document as an addition — the exact lie this
  // file exists to stop, resurrected under a green suite.
  const contentUnreadable = seedUnavailable || effectiveContent === undefined;
  /** …and it must say so in ITS OWN words. `historySeedUnavailable` names 初始
   * 版本 twice; printed on a revision that HAS an id, a timestamp and an author
   * it misidentifies the version standing next to a destructive button, which
   * is the same family of defect as the one above. */
  const unreadableNotice = contentLoading
    ? t.settings.historyLoading
    : seedUnavailable
      ? t.settings.historySeedUnavailable
      : t.settings.historyDefaultUnreadable;
  const unreadableTestId = contentLoading
    ? "doc-history-content-loading"
    : seedUnavailable
      ? "doc-history-seed-unavailable"
      : "doc-history-default-unreadable";
  const content = effectiveContent ?? {};

  const blockedFields = docCapBlockedFields(
    kind,
    sizes,
    currentContent,
    docCaps
  );
  const blocked = blockedFields.length > 0;
  const fields = documentFields(kind, content);
  const compared = currentContent
    ? comparedFieldNames(kind, content, currentContent)
    : [];
  /** What an EMPTY pane means: a document that really was blank, or one that
   * was sitting on a shipped default which is itself empty (the global block —
   * its default IS the empty document). Collapsing the two would put 「這個版本
   * 沒有任何內容」 back on a version that has content, just not its own. */
  const emptyNotice = tombstoned
    ? t.settings.historyModalDefaultContent
    : t.settings.historyModalEmpty;

  async function commitRestore() {
    setBusy(true);
    setRestoreError(null);
    try {
      await onRestore();
      setConfirming(false);
      onClose();
    } catch (e) {
      // The server's own message when it has one (404 pruned id / 400 size
      // cap); the generic line otherwise. BOTH dialogs stay open — closing the
      // reader on a failed restore would take the reason with it.
      setRestoreError(
        e instanceof ApiError && e.serverMessage
          ? e.serverMessage
          : t.settings.historyRestoreError
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      ref={rootRef}
      className="doc-hist-modal"
      data-testid="doc-history-modal"
      role="dialog"
      aria-modal="true"
      aria-label={t.settings.historyTitle}
      onClick={() => {
        if (!busy && !confirming) onClose();
      }}
    >
      <div
        className="doc-hist-modal__panel"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="doc-hist-modal__header">
          {/* WHICH version this is — the timestamp and the actor, the same two
            * facts the row carries, so the modal is never an anonymous body of
            * text the reader has to trace back to a row. The SEED has neither:
            * nobody wrote it and it has no time, so it is named instead of being
            * given a fabricated 修改者 line. */}
          <div className="doc-hist-modal__ident">
            <span className="doc-hist-modal__when">
              {seed ? t.settings.historySeedTitle : when}
            </span>
            {!seed && (
              <span className="doc-hist-modal__actor">
                {t.settings.historyByLabel} {actorLine}
              </span>
            )}
            {tombstoned && (
              <span className="set-badge">{t.settings.historyDefaultBadge}</span>
            )}
            {blocked && (
              <span className="set-badge set-badge--blocked">
                {t.settings.historyBlockedBadge}
              </span>
            )}
          </div>
          <div className="doc-hist-modal__actions">
            <div
              className="doc-hist-modal__tabs"
              role="group"
              aria-label={t.settings.historyPaneLabel}
            >
              {(["content", "diff"] as Pane[]).map((which) => (
                <button
                  key={which}
                  type="button"
                  className={`doc-hist-modal__tab${pane === which ? " doc-hist-modal__tab--on" : ""}`}
                  data-testid={`doc-history-pane-${which}`}
                  aria-pressed={pane === which}
                  onClick={() => setPane(which)}
                >
                  {which === "content"
                    ? t.settings.historyPaneContent
                    : t.settings.historyPaneDiff}
                </button>
              ))}
            </div>
            <button
              type="button"
              className="doc-hist-modal__close"
              data-testid="doc-history-modal-close"
              aria-label={t.settings.historyClose}
              onClick={onClose}
            >
              <CloseIcon size={16} />
            </button>
          </div>
        </div>

        <div className="doc-hist-modal__body" data-pane={pane}>
          {/* The default's content did not load. Saying 「這個版本沒有內容」 here
            * would be a different — and false — claim, so both panes say what
            * actually happened and the footer's restore stays live. */}
          {contentUnreadable ? (
            <p className="doc-hist-modal__notice" data-testid={unreadableTestId}>
              {unreadableNotice}
            </p>
          ) : pane === "content" ? (
            fields.length === 0 ? (
              <p className="doc-hist-modal__notice">{emptyNotice}</p>
            ) : (
              fields.map(([name, value]) => (
                <section className="doc-hist-modal__field" key={name}>
                  {/* A single-field kind (SOP, 學習經驗, 全域情境) needs no
                    * label — the modal's own document IS that field. A kind
                    * that carries several keeps them named and apart. */}
                  {fields.length > 1 && (
                    <h3 className="doc-hist-modal__field-name">
                      {fieldLabel(name)}
                    </h3>
                  )}
                  <Markdown
                    source={value}
                    className="doc-hist-modal__md doc-md"
                  />
                </section>
              ))
            )
          ) : currentContent === undefined ? (
            <p
              className="doc-hist-modal__notice"
              data-testid="doc-history-diff-pending"
            >
              {t.settings.historyDiffPending}
            </p>
          ) : (
            <>
              <p className="doc-hist-modal__diff-note">
                {t.settings.historyDiffNote}
              </p>
              {compared.length === 0 ? (
                <p className="doc-hist-modal__notice">{emptyNotice}</p>
              ) : (
                compared.map((name) => (
                  <section className="doc-hist-modal__field" key={name}>
                    {compared.length > 1 && (
                      <h3 className="doc-hist-modal__field-name">
                        {fieldLabel(name)}
                      </h3>
                    )}
                    <DiffView
                      before={content[name] ?? ""}
                      after={currentContent[name] ?? ""}
                      beforeLabel={versionLabel}
                      afterLabel={t.settings.historyCurrentLabel}
                      testId={`doc-history-diff-${name}`}
                    />
                  </section>
                ))
              )}
            </>
          )}
        </div>

        {blocked && (
          <div
            className="doc-hist-modal__blocked"
            data-testid="doc-history-modal-blocked"
          >
            {msg.docHistoryBlockedReason(
              blockedFields.map(fieldLabel),
              (docCaps && capForKind(kind, docCaps)) ?? 0
            )}
          </div>
        )}

        <div className="doc-hist-modal__footer">
          {onBack && (
            <button
              type="button"
              className="doc-btn doc-hist-modal__back"
              data-testid="doc-history-modal-back"
              onClick={onBack}
            >
              {t.settings.historyBack}
            </button>
          )}
          <button
            type="button"
            className="doc-btn"
            data-testid="doc-history-modal-close-footer"
            onClick={onClose}
          >
            {t.settings.historyClose}
          </button>
          <button
            type="button"
            className="doc-btn doc-hist-modal__restore"
            data-testid="doc-history-modal-restore"
            disabled={blocked}
            onClick={() => {
              setConfirming(true);
              setRestoreError(null);
            }}
          >
            {seed ? t.settings.historySeedRestore : t.settings.historyRestore}
          </button>
        </div>
      </div>

      {confirming && (
        <ConfirmModal
          testId="doc-history-restore-confirm"
          confirmTestId="doc-history-restore-confirm-btn"
          danger
          body={confirmBody}
          error={restoreError}
          busy={busy}
          cancelLabel={t.settings.cancel}
          confirmLabel={t.settings.historyRestoreConfirmAction}
          onCancel={() => {
            setConfirming(false);
            setRestoreError(null);
          }}
          onConfirm={() => void commitRestore()}
        />
      )}
    </div>
  );
}
