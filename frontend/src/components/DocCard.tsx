// components/DocCard.tsx — THE editable-document shell (T-c33e).
//
// It is the module-scope `DocDetail` that used to live inside SettingsPage.tsx,
// lifted out of that file so anything can import it. That it was un-exported is
// the whole reason the cockpit grew a second hand-written implementation of the
// same card: BootDocPage could not reach it, so it copied the markup. The
// owner's ask is the opposite —「我們甚至希望這些 component 是同一個 component
// reuse，讓他成為 single source of truth」— so the shell (breadcrumb, title,
// head, button group, usage readout, over-cap refusal, version-history entry,
// factory restore, save confirmation, error line) lives here exactly once and
// the BODY is the only thing a caller may replace.
//
// WHAT IS A PROP AND WHAT IS NOT. Everything a caller can vary is optional and
// absent means the behaviour this card has always had, so adopting a new
// affordance is a decision each page makes rather than something that arrives
// under it. Today's callers:
//   * 角色定義 / 使用者自訂 (SettingsPage) — the original two, unchanged apart
//     from the over-cap fix below.
//   * 系統互動 / 啟動程序 ×2 (BootDocPage) — which pass `confirmSave`,
//     `replaceNote` and `requireDirty`, and hold no editor state of their own.
// InsightCard / LessonsCard / the two task-manual documents are NOT migrated
// here yet (a separate ticket); the shell is shaped so they can be, which is
// why `renderBody` exists at all.
//
// 🔴 THE OVER-CAP DOOR IS NEW, AND IT IS A BUG FIX, NOT A TIDY-UP. `DocDetail`
// wrapped its save in `try/finally` with NO `catch`, so a role definition over
// its 1,000-char cap failed SILENTLY: measured on the untouched tree, the 完成
// 編輯 button stayed enabled, the usage readout stayed frozen at the stored
// size while the draft was four thousand characters long, the server's refusal
// reached nothing but an unhandled promise rejection, and the editor sat there
// looking like nothing had happened. So: the readout follows the DRAFT while
// editing, a document over its cap refuses in the cockpit with both numbers,
// and a save that fails anyway prints what the server said. Documents that pass
// no `usage` (使用者自訂 genuinely has no cap) are untouched by all of it.

import { useState, type ReactNode } from "react";
import { useI18n } from "../i18n";
import { Breadcrumbs, type Crumb } from "./Breadcrumbs";
import { InlineEdit } from "./InlineEdit";
import { Markdown } from "./Markdown";
import { ChevronDownIcon, ChevronRightIcon, PencilIcon } from "./icons";
import { ConfirmModal } from "./ConfirmModal";
import {
  DocumentHistoryEntry,
  type DocumentHistoryEntryProps,
} from "./DocumentHistoryEntry";
import { serverMessageOf } from "../api/errors";
import { docCapBlocked, runeLength } from "../api/docCap";
import "./settings.css";

/** The folded document this card renders. Structural on purpose: the role
 * definition, the global-context block and the two boot-context blocks are four
 * different view types that all carry these two fields. */
export interface DocCardDoc {
  text: string;
  isDefault: boolean;
}

/** What a replacement body is handed. `commit` is deliberately absent — the
 * save button belongs to the shell, so a body cannot grow its own. */
export interface DocCardBodyProps {
  editing: boolean;
  text: string;
  draft: string;
  setDraft: (next: string) => void;
}

export interface DocCardProps {
  title: string;
  /** Rename the doc's TITLE (custom roles only — the 角色名 is owner-editable
   * there; seed roles pass none and keep the plain heading). Renders the shared
   * pencil InlineEdit in the heading; commits ride the role PATCH choke. */
  onRenameTitle?: (name: string) => Promise<void> | void;
  doc: DocCardDoc | null;
  /** The unified settings breadcrumb (T-8f6e) — 設定 › 角色誌 › <this doc>. */
  crumbs: Crumb[];
  /** Save/reset are omitted for read-only docs. */
  onSave?: (text: string) => Promise<void> | void;
  onReset?: () => Promise<void> | void;
  /** The document's 版本紀錄 (T-1f39). Rendered in the EDIT toolbar, in the
   * slot 重置 used to hold; the reset itself survives as the list's 初始版本
   * row and is wired from `onReset` here, so a document without a seed simply
   * does not grow that row. */
  history?: Omit<DocumentHistoryEntryProps, "onReset" | "disabled">;
  /** Optional content rendered BELOW the card (e.g. the persona page's
   * <InsightCard> / <LessonsCard>). */
  extra?: ReactNode;
  /** An honest load-failure line, rendered above the card. */
  errorNote?: ReactNode;
  /** Read-only mode: no edit/reset affordances, just the rendered markdown. */
  readOnly?: boolean;
  /** Overrides the "Default" is_default badge. */
  badge?: string;
  /** The 預設 verdict, when the caller knows it independently of `doc` (the
   * roles page reads it off the roster row). Wins over the doc's own flag, and
   * is what lets the badge stay honest while the body read is in flight or
   * failed — without it a null `doc` can only say nothing. */
  isDefault?: boolean;
  /** This document's size budget, `{size, cap}` in CHARACTERS (T-ae38). Passed
   * only by documents that HAVE a cap; the global-context views omit it because
   * they genuinely have none, and showing a "0 / 0" there would invent a limit
   * the server does not enforce.
   *
   * `size` is the STORED size. While the editor is open the readout — and the
   * refusal below it — follow the draft instead, which is the only way a
   * cockpit-side block can be honest about a document nobody has saved yet. */
  usage?: { size: number; cap: number };
  /** One line under the card head saying that a save REPLACES THE WHOLE
   * DOCUMENT. Absent by default: it is a fact about every document here, but
   * only the pages whose editor used to be per-section owe the sentence, and
   * adding it under a page that never offered anything else would be noise. */
  replaceNote?: string;
  /** One line under the card head that is about THIS document rather than about
   * saving — today, why a read-only document has no editor (T-3201). Separate
   * from `replaceNote` because the two are never both true: a document nobody
   * may write cannot be warned that saving replaces it. */
  note?: ReactNode;
  /** Ask before saving. The body is the caller's, because the consequence is:
   * a mangled boot sequence means agents never come online and nobody is left
   * to fix it, and that sentence is FALSE of the system-interaction block. */
  confirmSave?: { body: string; confirmLabel: string };
  /** Refuse to save a draft identical to the stored text. Off by default: the
   * two original callers have always let 完成編輯 through unconditionally, and
   * a no-op write there is harmless. The boot-context pages turn it on because
   * a no-op write still flips the document out of 預設 for ever. */
  requireDirty?: boolean;
  /** Replace the body. Default: one textarea over the whole document while
   * editing, rendered markdown otherwise. */
  renderBody?: (props: DocCardBodyProps) => ReactNode;
  /** Put the whole card behind its own heading, CLOSED on mount (T-6278).
   *
   * 🔴 NO CALLER PASSES THIS TODAY (T-bac4). It was built for the page that
   * stacked TWO full documents — 啟動程序, Claude then Codex — and that page is
   * gone: 啟動程序 is now an index of two rows, one document per page, so
   * nothing has two cards to fold. This docstring used to describe that stacked
   * page in the present tense, which the independent review caught: a reader
   * would have believed the page still exists.
   *
   * WHY IT WAS KEPT rather than deleted with the page. The same round DID
   * delete `.settings-stack`, which also lost its only consumer, so the
   * asymmetry needs a reason and it is not "one felt bigger": `.settings-stack`
   * was internal CSS with no external surface, while this branch's labels
   * (`docExpand` / `docCollapse`) are THEME WORDING CODES. A theme pack the
   * owner has already customised may carry values for them, and removing the
   * codes makes those values silently vanish on import (unknown codes are
   * dropped). That is an external effect outside T-bac4's scope, so the call
   * belongs to whoever owns the wording surface.
   *
   * ⚠️ THEREFORE IT IS TESTED, not left as unexercised dead code: the collapse
   * behaviour and its 🔴 accessibility rule below are pinned in
   * DocCard.collapsible.test.tsx. If you retire the prop, retire that file and
   * the two wording codes together — and if you bring it back into a page,
   * re-read T-fc57 first (see the note above the collapsed branch).
   *
   * …AND THAT "together" IS A POLICY, NOT A MECHANISM. The review checked:
   * there is no unused-key lint in this repo, so you CAN delete this branch and
   * leave `docExpand` / `docCollapse` in the locales, and the theme whitelist
   * would not move. They are bundled here because a wording code with no render
   * site anywhere is its own kind of falsehood — a theme author would be
   * editing a string that can never appear. Retire them as a pair by choice; do
   * not read this as "deleting the branch forces a whitelist change", which
   * would make the branch look harder to remove than it is.
   *
   * The shape it took, recorded because the reasoning is not re-derivable: the
   * owner met the stacked page on a phone, scrolled to the bottom of the first
   * document, and read the end of the card as the end of the PAGE. Adding a
   * separator was considered and REJECTED by the same observation — there
   * already IS one, and it sits so far down that nobody reaches it. */
  collapsible?: boolean;
}

/** Which confirmation is open. Saving is the only one this card raises: the
 * factory restore lives in the history list's 初始版本 row (owner 2026-08-14,
 * card rc-f1950f4d286e — "完全照 insight"), and that row carries its own. */
type Pending = { kind: "save" } | null;

export function DocCard({
  title,
  onRenameTitle,
  doc,
  crumbs,
  onSave,
  onReset,
  history,
  extra,
  errorNote,
  readOnly = false,
  badge,
  isDefault: isDefaultOverride,
  usage,
  replaceNote,
  note,
  confirmSave,
  requireDirty = false,
  renderBody,
  collapsible = false,
}: DocCardProps) {
  const { t, msg } = useI18n();
  const [collapsed, setCollapsed] = useState(collapsible);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<Pending>(null);
  /** The server's REASON, not a flag — same rule as the journal cards: the
   * doc-cap refusal carries the instructions the person needs, and `""` is the
   * real state "failed with nothing quotable". */
  const [failed, setFailed] = useState<string | null>(null);

  const text = doc ? doc.text : "";
  // 🔴 A null doc means the body has NOT been read (loading, or the read
  // failed) — it does not mean the document is untouched. The old fallback
  // here was `true`, which turned "I do not know" into the positive claim 預設:
  // an owner-EDITED role whose `getRole` failed was badged as shipped-default,
  // beside an empty body, and nothing said otherwise. The badge is a claim, so
  // an unknown document makes none.
  //
  // `isDefaultOverride` exists because a caller can often know this WITHOUT the
  // body: the roles page holds the roster row, and the roster has carried
  // `is_default` all along. Passing it keeps the badge true through exactly the
  // window where the doc read is pending or broken.
  const isDefault = isDefaultOverride ?? (doc ? doc.isDefault : false);

  // While editing, both the readout and the refusal judge the DRAFT. Mirrors
  // the server's own rule (docCapBlocked): over the cap is refused unless the
  // document is getting shorter, so an already-over-cap document can still be
  // edited downward instead of being frozen.
  const shownSize = usage ? (editing ? runeLength(draft) : usage.size) : 0;
  const overCap =
    usage !== undefined && editing && docCapBlocked(usage.cap, text, draft);
  const unchanged = requireDirty && draft === text;

  // ⚠️ NO SCROLL CORRECTION HERE, AND THE REASON IS THAT NONE IS POSSIBLE —
  // NOT, as this comment claimed until the independent review measured it, that
  // nothing moves.
  //
  // The owner's rule for a DISCLOSURE — a body that opens and closes under its
  // own heading (2026-08-15, T-6630 ①) — is that the screen must not move:
  // content grows downward from the heading and collapses back up into it. The
  // FIRST card gets that for free — everything a collapse removes sits below its
  // heading, and a heading that is scrolled to is at the top of what is above it.
  //
  // ⚠️ NOT EVERY COLLAPSE IN THE COCKPIT IS UNDER THAT RULE. Collapsing a whole
  // TASK CARD was ruled the other way in the same ticket (③, 2026-08-16:「收和整
  // 個任務時,最後應該要定位到那則任務」) and does carry a correction — see
  // TaskCard.tsx and lib/scrollPort.ts. Whether this card should follow the task
  // card is an OPEN QUESTION for the owner — he is the only one who can rule on
  // it — deliberately not answered here: it was outside T-6630's scope, which
  // was the task page.
  // ⚠️ A ruling of "yes, follow it" would still NOT fix the case measured just
  // below: the task-card correction brings a card back to the fold, and in the
  // last-card situation here the arithmetic to do that does not exist. It would
  // buy the OTHER cases. Do not read the open question as "this measured limit
  // is merely unfinished work".
  //
  // 🔴 THE LAST CARD DOES NOT. Measured at 390×844 AND 1040×844: open both
  // documents, scroll to the very bottom, collapse the LAST one by its own
  // heading → the heading moves 389.9 → 753.9, 364px. The page has genuinely
  // become shorter than the current scroll position, so the browser clamps
  // scrollTop and the content slides down under a viewport that cannot follow
  // it. Restoring the heading would need scrollTop 7330 against a new maximum
  // of 6966: THE CORRECTION IS ARITHMETICALLY UNAVAILABLE, which is why a
  // scroll correction was written here and then removed rather than kept as a
  // no-op. What survives is that the heading stays ON SCREEN (753.9 < 844) — it
  // is pushed, never lost. (The task-step notes have no correction either any
  // more: T-6630 removed T-4e39's, on the same owner rule as this comment's.)
  //
  // Do not "fix" this with a scroll correction. The only real fixes change the
  // layout — e.g. reserving the collapsed height, or a sticky heading — and
  // that is a decision for the owner, not a patch.

  function startEdit() {
    setDraft(text);
    setFailed(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setDraft("");
    setFailed(null);
  }

  async function run(action: () => Promise<void> | void) {
    setBusy(true);
    setFailed(null);
    try {
      await action();
      setPending(null);
      setEditing(false);
      setDraft("");
    } catch (e) {
      setFailed(serverMessageOf(e));
    } finally {
      setBusy(false);
    }
  }

  function requestSave() {
    if (!onSave) return;
    setFailed(null);
    if (confirmSave) {
      setPending({ kind: "save" });
      return;
    }
    void run(() => onSave(draft));
  }

  // No requestReset here any more: the only reset affordance is the 初始版本
  // row inside DocumentHistoryEntry (edit mode), which runs its OWN confirm.
  // Removed with the top-level button rather than left dangling — a dead
  // private function reads like a path something still takes.

  const body = renderBody ? (
    renderBody({ editing: editing && !readOnly, text, draft, setDraft })
  ) : editing && !readOnly ? (
    <textarea
      className="doc-editor"
      data-testid="doc-card-editor"
      value={draft}
      autoFocus
      spellCheck={false}
      placeholder={t.settings.editorPlaceholder}
      onChange={(e) => setDraft(e.target.value)}
    />
  ) : (
    <Markdown source={text} className="doc-md" />
  );

  return (
    <div className="settings">
      <Breadcrumbs items={crumbs} />
      <h1
        className={
          "settings__title settings__title--doc" +
          (collapsible ? " settings__title--toggle" : "")
        }
      >
        {/* The toggle takes the TITLE TEXT with it, so the whole heading is the
          * click target — the owner asked to expand by pressing the document,
          * not by hunting a chevron. A renameable title cannot go inside a
          * button (its InlineEdit owns the click), so those pages get the
          * chevron alone and keep the InlineEdit beside it. No caller combines
          * the two today; the branch is here so that one does not silently
          * lose its rename. */}
        {collapsible && (
          <button
            type="button"
            className="doc-collapse"
            data-testid="doc-card-collapse"
            aria-expanded={!collapsed}
            /* 🔴 NO aria-label HERE. One stood here saying 展開這份文件 /
             * 收合這份文件, and the independent review measured what it cost:
             * BOTH buttons — and, through them, both <h1>s — reported the same
             * accessible name, so a screen reader could not tell 啟動程序
             * (Claude Code) from (Codex CLI), and getByRole('button', {name:
             * '啟動程序（Claude Code）'}) matched nothing. That is this
             * ticket's own defect (two documents you cannot tell apart) rebuilt
             * in the accessibility tree, plus WCAG 2.5.3 Label in Name.
             * The visible title IS the name; aria-expanded carries the state,
             * which is what a screen reader announces as expanded/collapsed. */
            /* …EXCEPT when the title is renameable and therefore lives OUTSIDE
             * this button: then there is no visible text inside it to be the
             * name, and a nameless button is worse than a generic one. No
             * caller combines the two today. */
            aria-label={
              onRenameTitle
                ? collapsed
                  ? t.settings.docExpand
                  : t.settings.docCollapse
                : undefined
            }
            onClick={() => setCollapsed((c) => !c)}
          >
            {collapsed ? (
              <ChevronRightIcon size={18} />
            ) : (
              <ChevronDownIcon size={18} />
            )}
            {onRenameTitle ? null : <span>{title}</span>}
          </button>
        )}
        {onRenameTitle ? (
          <InlineEdit
            value={title}
            onCommit={(next) => void onRenameTitle(next)}
            ariaLabel={t.settings.renameRole}
            placeholder={t.settings.addRoleName}
          />
        ) : (
          !collapsible && title
        )}
      </h1>

      {/* There is no top-level reset button. It used to stand here with no
        * prerequisites, on the argument that a document booting every agent
        * must be restorable from a page whose read failed. The owner OVERRODE
        * that on 2026-08-14 (card rc-f1950f4d286e, option 2: "完全照 insight")
        * with the trade-off spelled out to him — insight keeps its reset inside
        * edit mode, and he wants these blocks to look the same. The reset is
        * therefore reached exactly as insight's is: the 初始版本 row inside
        * DocumentHistoryEntry, in edit mode. Do not restore this button as a
        * bug fix; T-791e's ticket carries the superseded red line and says so. */}

      {/* Collapsed hides THIS document — its load error included, since the
        * line is about the body that is no longer on screen. `extra` stays: it
        * carries OTHER documents (the persona page's insight / lessons cards),
        * and folding a role definition must not take them with it. */}
      {collapsed ? null : (
        <>
      {errorNote}

      <div className="doc-card">
        <div className="doc-card__head">
          {/* No filename chip here — docs are presented as CONTENT, not files
           * (the role page's internal role-….md name was implementation detail
           * the owner should never see). The head keeps only the badge. */}
          <span className="doc-card__file">
            {badge ? (
              <span className="set-badge">{badge}</span>
            ) : (
              isDefault && (
                <span
                  className="set-badge"
                  data-testid="doc-card-default-badge"
                >
                  {t.settings.defaultBadge}
                </span>
              )
            )}
            {/* Always rendered when this document has a cap — including while
              * editing, which is precisely when the number is wanted, and
              * including at 0 chars, since that is when someone is about to
              * write the first thing into it. */}
            {usage && (
              <span
                className="doc-card__usage"
                data-testid="doc-card-usage"
                title={t.settings.docUsage}
              >
                {shownSize} / {usage.cap}
              </span>
            )}
          </span>
          {readOnly ? null : editing ? (
            <div className="doc-card__actions">
              {/* 版本紀錄 stands where 重置 stood (owner 2026-07-31). The reset
               * did not disappear — it is the 初始版本 row inside, and only
               * where a seed exists (onReset omitted ⇒ no such row, e.g. a
               * custom role whose reset the server 404s). */}
              {history && (
                <DocumentHistoryEntry
                  {...history}
                  // A restore rewrote the document under the editor, so the
                  // editor's draft is now a pending overwrite of the version
                  // just restored. Leave edit mode with it — the same exit the
                  // reset has always made.
                  onRestored={async () => {
                    await history.onRestored?.();
                    cancelEdit();
                  }}
                  onReset={onReset ? () => void run(() => onReset()) : undefined}
                  disabled={busy}
                />
              )}
              <button
                type="button"
                className="doc-btn"
                onClick={cancelEdit}
                disabled={busy}
              >
                {t.settings.cancel}
              </button>
              <button
                type="button"
                className="doc-btn doc-btn--accent"
                data-testid="doc-card-save"
                onClick={requestSave}
                disabled={busy || overCap || unchanged}
              >
                {t.settings.doneEdit}
              </button>
            </div>
          ) : (
            <button
              type="button"
              className="doc-btn doc-btn--edit"
              data-testid="doc-card-edit"
              onClick={startEdit}
              /* T-2d99: a null doc means the mount fetch has not landed (or
               * failed) — NOT "an empty doc". Editing then seeds draft from
               * text="" and the editor opens blank over content the user has
               * never seen; committing that sends a whole-doc replace of ""
               * which, because this call site passes allow_shrink, sails past
               * the server's wipe guard. Gate the affordance on the load
               * instead of weakening the guard: you cannot edit what has not
               * arrived. */
              disabled={doc === null}
            >
              <PencilIcon size={14} />
              <span>{t.settings.edit}</span>
            </button>
          )}
        </div>

        {/* Blocked in the cockpit, with BOTH numbers, before anything is sent —
          * the alternative is a refusal the owner only meets as a 400, or (as
          * this card did until T-c33e) as nothing at all. */}
        {overCap && usage && (
          <div className="set-error" data-testid="doc-card-over-cap">
            {msg.docOverCap(shownSize, usage.cap)}
          </div>
        )}
        {/* A failure that got past the cockpit still has to SAY what the server
          * said; `""` means it failed with nothing quotable. Not shown while a
          * confirmation is open — the modal carries the same line there. */}
        {failed !== null && !pending && (
          <div className="set-error" data-testid="doc-card-save-error">
            {failed || t.settings.docActionFailed}
          </div>
        )}
        {replaceNote && (
          <div className="doc-card__note" data-testid="doc-card-replace-note">
            {replaceNote}
          </div>
        )}
        {note && (
          <div className="doc-card__note" data-testid="doc-card-note">
            {note}
          </div>
        )}

        <div className="doc-card__body">{body}</div>
      </div>
        </>
      )}
      {extra}

      {pending && (
        <ConfirmModal
          testId="doc-card-save-confirm"
          confirmTestId="doc-card-save-confirm-btn"
          danger
          body={confirmSave?.body ?? ""}
          error={failed}
          busy={busy}
          cancelLabel={t.settings.cancel}
          confirmLabel={confirmSave?.confirmLabel ?? t.settings.doneEdit}
          onCancel={() => {
            setPending(null);
            setFailed(null);
          }}
          onConfirm={() => void run(() => onSave?.(draft))}
        />
      )}
    </div>
  );
}
