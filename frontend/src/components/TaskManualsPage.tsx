// TaskManualsPage — 設定 › 任務手冊 (SPEC §5, visual per AI-Office-M3 mockup;
// owner re-adjudicated 2026-07-13): the task-type / playbook surface, sitting
// NEXT TO 角色誌 on the settings landing.
//
//   List (§5.1)   — one row per type: the DISPLAY NAME (fallback type_key —
//                   T-fa76), delete button,
//                   chevron. 出廠不含任何類型 (honest empty state); 新增類型
//                   grows the INLINE create row (the 角色誌 add pattern) — a
//                   display name alone creates a BLANK manual (the server
//                   mints the tm- type_key); delete is confirm-modal'd and a
//                   type with OPEN tasks survives its 409 with the honest
//                   先讓任務結束 message.
//   Hub (§5.2)    — breadcrumb 設定 › 任務手冊 › <type>, the big type title,
//                   the 負責成員 SUMMARY CARD (icon + 「負責成員 · 同類型所有
//                   任務由他負責」 + one-line setting + 編輯 → the member-
//                   panel-style editor expands IN PLACE), then the 任務規劃
//                   section with TWO ENTRY CARDS (任務定義 / 學習經驗, each
//                   subtitle + chevron) that PUSH their own sub-page.
//   Sub-pages     — 任務定義 / 學習經驗 each get their own breadcrumb page
//                   (設定 › 任務手冊 › <type> › 任務定義/學習經驗, owner
//                   2026-07-20 — ex-inline-accordion); content mirrors the
//                   guided three questions / the learnings doc, editing carried
//                   over. NO internal filename anywhere (owner's earlier ruling
//                   stands — manuals are content, not files; the mockup's
//                   review-pr.md chip is deliberately not built).
//   Assignee edit — segmented 指定成員/外包 toggle; model = the member panel's
//                   quick-pick chips (MODEL_QUICK_PICKS — the same source as
//                   ModelEffortEditor) + free input; 投入程度 = 低/中/高/最高
//                   segmented; 機器 = the machines list, one of which must be
//                   chosen for the type to run at all (states joined honestly
//                   from /api/machines + monitoring agents: 閒置/忙碌/離線);
//                   雇用數量 = −/＋ stepper + 無限 (wire
//                   copies=0 = unlimited, spec TaskManualDTO).

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useI18n } from "../i18n";
import { effortText } from "../i18n/compose";
import type { Effort, Member } from "../types";
import type {
  ManualAssigneeView,
  TaskManualFieldView,
  TaskManualPatch,
  TaskManualSummaryView,
  TaskManualView,
} from "../api/adapter";
import { isHttpStatus } from "../api/errors";
import { useMachines } from "../hooks/useMachines";
import { useMonitoring } from "../hooks/useMonitoring";
import { Markdown } from "./Markdown";
import { InlineEdit } from "./InlineEdit";
import { Breadcrumbs, type Crumb } from "./Breadcrumbs";
import { DocumentHistoryEntry } from "./DocumentHistoryEntry";
import "./stepper.css";
import { ConfirmModal } from "./ConfirmModal";
import { CODEX_MODEL_OPTIONS, MODEL_QUICK_PICKS, EFFORTS } from "./ModelEffortEditor";
import {
  BriefcaseIcon,
  BulbIcon,
  ChevronRightIcon,
  FileTextIcon,
  PencilIcon,
  TrashIcon,
  UserIcon,
} from "./icons";

// ── 列表 (§5.1) ───────────────────────────────────────────────────────────────

export function TaskManualsList({
  manuals,
  loading,
  error,
  crumbs,
  onOpen,
  onCreate,
  onDelete,
}: {
  manuals: TaskManualSummaryView[];
  loading: boolean;
  error: boolean;
  /** The unified settings breadcrumb (T-8f6e) — 設定 › 任務手冊. */
  crumbs: Crumb[];
  onOpen: (typeKey: string) => void;
  /** Create by DISPLAY NAME (T-fa76): the server mints the tm- type_key. */
  onCreate: (displayName: string) => Promise<unknown>;
  onDelete: (typeKey: string) => Promise<void>;
}) {
  const { t, msg } = useI18n();

  // Inline create row (the 角色誌 新增 pattern): one DISPLAY-NAME field
  // (T-fa76 — the id is the system's, the text is the human's; the server
  // mints the tm- type_key), Enter/建立 creates, Esc/取消 collapses.
  const [adding, setAdding] = useState(false);
  const [name, setName] = useState("");
  const [createBusy, setCreateBusy] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const composingRef = useRef(false);

  // Delete confirm modal + honest per-cause error line (409 open tasks).
  const [confirmKey, setConfirmKey] = useState<string | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  function resetForm() {
    setAdding(false);
    setName("");
    setCreateError(null);
  }

  async function submitCreate() {
    const displayName = name.trim();
    if (!displayName) {
      setCreateError(t.settings.addManualError);
      return;
    }
    setCreateBusy(true);
    setCreateError(null);
    try {
      await onCreate(displayName);
      resetForm();
    } catch (e) {
      console.warn("TaskManualsPage: create failed", e);
      setCreateError(t.settings.addManualError);
    } finally {
      setCreateBusy(false);
    }
  }

  async function confirmDelete(key: string) {
    setDeleteBusy(true);
    setDeleteError(null);
    try {
      await onDelete(key);
      setConfirmKey(null);
    } catch (e) {
      // 409 = the type still has OPEN (non-terminal) tasks — the actionable
      // human message (spec §5.1 需先讓那些任務結束); else a generic failure.
      setDeleteError(
        isHttpStatus(e, 409)
          ? t.settings.deleteManualOpenTasks
          : t.settings.deleteManualError
      );
    } finally {
      setDeleteBusy(false);
    }
  }

  return (
    <div className="settings">
      {/* Breadcrumb 設定 › 任務手冊 (T-8f6e unified pattern) + page title. */}
      <Breadcrumbs items={crumbs} />
      <h1 className="settings__title settings__title--doc">
        {t.settings.manuals}
      </h1>

      {/* Honest load-failure notice — never render a dead fetch as "no types". */}
      {error && <div className="set-error">{t.settings.manualsLoadError}</div>}

      <div className="set-entries">
        {!loading && !error && manuals.length === 0 && (
          <div className="manuals-empty" data-testid="manuals-empty">
            {t.settings.manualsEmpty}
          </div>
        )}

        {manuals.map((m) => (
          <div className="set-entry-row" key={m.typeKey}>
            <div className="set-entry-row__main">
              {/* Mockup list row: the type_key ONLY (mono) — no purpose
               * subtitle, no leading icon (owner 2026-07-13). */}
              <button
                type="button"
                className="set-entry manual-row"
                data-testid={`manual-open-${m.typeKey}`}
                onClick={() => onOpen(m.typeKey)}
              >
                <span className="set-entry__body">
                  {/* Display name first (T-fa76), falling back to the raw
                   * type_key. */}
                  <span className="set-entry__name manual-key">
                    {m.displayName || m.typeKey}
                  </span>
                </span>
                <ChevronRightIcon size={18} className="set-entry__chev" />
              </button>
              <button
                type="button"
                className="set-entry-row__delete"
                data-testid={`manual-delete-${m.typeKey}`}
                aria-label={t.settings.deleteManual}
                title={t.settings.deleteManual}
                onClick={() => {
                  setConfirmKey(m.typeKey);
                  setDeleteError(null);
                }}
              >
                <TrashIcon size={16} />
              </button>
            </div>
          </div>
        ))}

        {/* 新增類型 — the list's bottom entry (spec §5.1). */}
        {!adding ? (
          <button
            type="button"
            className="add-entry"
            data-testid="manual-add-entry"
            onClick={() => setAdding(true)}
          >
            + {t.settings.addManual}
          </button>
        ) : (
          <div className="set-entry set-add-inline" data-testid="manual-create-row">
            <span className="set-entry__icon set-entry__icon--blue">
              <FileTextIcon size={18} />
            </span>
            <input
              className="set-add-inline__input"
              value={name}
              autoFocus
              placeholder={t.settings.addManualName}
              aria-label={t.settings.addManualName}
              onChange={(e) => setName(e.target.value)}
              onCompositionStart={() => {
                composingRef.current = true;
              }}
              onCompositionEnd={(e) => {
                composingRef.current = false;
                setName(e.currentTarget.value);
              }}
              onKeyDown={(e) => {
                if (
                  e.nativeEvent.isComposing ||
                  e.keyCode === 229 ||
                  composingRef.current
                ) {
                  return;
                }
                if (e.key === "Enter" && !createBusy) void submitCreate();
                if (e.key === "Escape") {
                  // Spent here — see InlineEdit: the shared Esc dispatcher
                  // must not also close the surface around this field.
                  e.preventDefault();
                  resetForm();
                }
              }}
              data-testid="manual-create-key"
            />
            <button
              type="button"
              className="doc-btn"
              disabled={createBusy}
              onClick={resetForm}
            >
              {t.settings.addManualCancel}
            </button>
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              disabled={createBusy}
              onClick={() => void submitCreate()}
              data-testid="manual-create-submit"
            >
              {t.settings.addManualSubmit}
            </button>
          </div>
        )}
        {adding && createError && (
          <div className="set-error" data-testid="manual-create-error">
            {createError}
          </div>
        )}
      </div>

      {confirmKey !== null && (
        <ConfirmModal
          testId="manual-delete-confirm"
          confirmTestId="manual-delete-confirm-btn"
          danger
          // The modal names the type by its DISPLAY face (fallback = key).
          body={msg.deleteManualConfirm(
            manuals.find((m) => m.typeKey === confirmKey)?.displayName ||
              confirmKey
          )}
          error={deleteError}
          busy={deleteBusy}
          cancelLabel={t.settings.addManualCancel}
          confirmLabel={t.settings.deleteManualConfirmAction}
          onCancel={() => {
            setConfirmKey(null);
            setDeleteError(null);
          }}
          onConfirm={() => void confirmDelete(confirmKey)}
        />
      )}
    </div>
  );
}

// ── 詳情 hub (mock-manual-detail: 摘要卡 + 任務規劃入口卡) ─────────────

export function TaskManualHub({
  manual,
  members,
  crumbs,
  onSave,
  onOpenDefinition,
  onOpenLearnings,
}: {
  /** The DIRECTORY row is enough here (T-1170): the hub renders the display
   * name, the 負責成員 card and two links. Neither long document is on this
   * page, so it must not ask for a shape that carries them. */
  manual: TaskManualSummaryView;
  /** The office roster (real assistants) — the assignee member picker. */
  members: Member[];
  /** The unified settings breadcrumb (T-8f6e) — 設定 › 任務手冊 › <type>. */
  crumbs: Crumb[];
  onSave: (patch: TaskManualPatch) => Promise<unknown>;
  /** Navigate into the 任務定義 / 學習經驗 sub-pages (owner 2026-07-20 — the
   * two 任務規劃 cards push a child page instead of expanding in place). */
  onOpenDefinition: () => void;
  onOpenLearnings: () => void;
}) {
  const { t } = useI18n();
  return (
    <div className="settings">
      <Breadcrumbs items={crumbs} />
      {/* 顯示名稱 — edited IN PLACE on the title (owner T-8a4a), the SAME pencil
       * inline-edit affordance as the 角色設定 role title. Value shows the
       * display name (or the typeKey when unset); commit patches displayName.
       * This is the ONLY rename affordance now (moved out of 任務定義). */}
      <h1 className="settings__title settings__title--doc">
        <InlineEdit
          value={manual.displayName || manual.typeKey}
          onCommit={(next) => void onSave({ displayName: next })}
          ariaLabel={t.settings.manualDisplayName}
          placeholder={t.settings.manualDisplayNamePlaceholder}
          displayClassName="manual-key"
        />
      </h1>

      <AssigneeCard manual={manual} members={members} onSave={onSave} />

      {/* 任務規劃 — the two entry cards navigate into their own sub-pages
       * (owner 2026-07-20). The chevron is now the plain 前往 right-caret the
       * other settings rows use, matching the push semantics. */}
      <div className="manual-section-label">
        {t.settings.manualPlanningSection}
      </div>
      <div className="set-entries">
        <button
          type="button"
          className="set-entry manual-entry"
          data-testid="manual-entry-definition"
          onClick={onOpenDefinition}
        >
          <span className="set-entry__icon set-entry__icon--blue">
            <FileTextIcon size={18} />
          </span>
          <span className="set-entry__body">
            <span className="set-entry__name">
              {t.settings.manualTabDefinition}
            </span>
            <span className="set-entry__sub">
              {t.settings.manualDefEntrySub}
            </span>
          </span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
        <button
          type="button"
          className="set-entry manual-entry"
          data-testid="manual-entry-learnings"
          onClick={onOpenLearnings}
        >
          <span className="set-entry__icon set-entry__icon--purple">
            <BulbIcon size={18} />
          </span>
          <span className="set-entry__body">
            <span className="set-entry__name">
              {t.settings.manualTabLearnings}
            </span>
            <span className="set-entry__sub">
              {t.settings.manualLearnEntrySub}
            </span>
          </span>
          <ChevronRightIcon size={18} className="set-entry__chev" />
        </button>
      </div>
    </div>
  );
}

// ── 任務定義 / 學習經驗 sub-pages (owner 2026-07-20 — pushed from the hub) ───
// Each is a breadcrumb sub-page (設定 › 任務手冊 › <type> › 任務定義/學習經驗)
// wrapping the SAME card the hub used to expand inline, so the editing
// affordance is carried over untouched.

export function TaskManualDefinitionPage({
  manual,
  loadError,
  crumbs,
  onSave,
  onRestored,
}: {
  /** The manual IN FULL — `null` until its own read lands (T-1170: the SOP is
   * not on the list answer). The page keeps its breadcrumb and title either
   * way; only the card waits, because a card seeded from a fabricated blank
   * manual would let 完成編輯 write an empty SOP over a real one. */
  manual: TaskManualView | null;
  /** That read REJECTED — said out loud, because the list row can be there
   * while this document could not be fetched. */
  loadError?: boolean;
  crumbs: Crumb[];
  onSave: (patch: TaskManualPatch) => Promise<unknown>;
  /** Re-read the manual after a 版本紀錄 restore (T-7d33). A restore writes ONE
   * field of the manual back (T-1f39), but the manual is fetched whole, so both
   * sub-pages still refresh the same way. */
  onRestored?: () => Promise<unknown> | void;
}) {
  const { t } = useI18n();
  return (
    <div className="settings">
      <Breadcrumbs items={crumbs} />
      <h1 className="settings__title settings__title--doc">
        {t.settings.manualTabDefinition}
      </h1>
      {/* 版本紀錄 lives in this card's own edit toolbar (T-1f39, owner
        * 2026-07-31) and covers the SOP ONLY — this page also edits 用途 and
        * 識別鍵, which are not versioned at all, so the list names the SOP in
        * both its heading and its note. */}
      {loadError && (
        <div className="set-error" data-testid="manual-doc-load-error">
          {t.settings.manualsLoadError}
        </div>
      )}
      {manual && (
        <DefinitionCard
          manual={manual}
          onSave={onSave}
          onRestored={onRestored}
        />
      )}
    </div>
  );
}

export function TaskManualLearningsPage({
  manual,
  loadError,
  crumbs,
  onSave,
  onRestored,
}: {
  /** See TaskManualDefinitionPage — `null` until this page's own read lands. */
  manual: TaskManualView | null;
  loadError?: boolean;
  crumbs: Crumb[];
  onSave: (patch: TaskManualPatch) => Promise<unknown>;
  /** Re-read the manual after a 版本紀錄 restore (T-7d33). */
  onRestored?: () => Promise<unknown> | void;
}) {
  const { t } = useI18n();
  return (
    <div className="settings">
      <Breadcrumbs items={crumbs} />
      <h1 className="settings__title settings__title--doc">
        {t.settings.manualTabLearnings}
      </h1>
      {/* The manual's learnings have their OWN revision series since T-1f39, so
        * a SOP rewrite no longer washes the list out — and restoring from the
        * card's own 版本紀錄 puts back the learnings alone. */}
      {loadError && (
        <div className="set-error" data-testid="manual-doc-load-error">
          {t.settings.manualsLoadError}
        </div>
      )}
      {manual && (
        <LearningsCard manual={manual} onSave={onSave} onRestored={onRestored} />
      )}
    </div>
  );
}


/** True when two field lists are byte-equivalent (name/required/isKey) — used
 * to skip a no-op PATCH on a blur that changed nothing. */
function fieldsEqual(a: TaskManualFieldView[], b: TaskManualFieldView[]) {
  if (a.length !== b.length) return false;
  return a.every(
    (f, i) =>
      f.name === b[i].name &&
      f.required === b[i].required &&
      f.isKey === b[i].isKey
  );
}

/** Grow a textarea to fit its content (autosize) so a long purpose is fully
 * visible without an inner scrollbar. Height floors at the CSS min-height. */
function autosize(el: HTMLTextAreaElement | null) {
  if (!el) return;
  el.style.height = "auto";
  el.style.height = `${el.scrollHeight}px`;
}

/** The three numbered blocks of 任務定義, in render order. The block number is
 * the section's identity everywhere: state, testids, PATCH key. */
type DefBlock = 1 | 2 | 3;

/** One block's edit switch, sitting at the end of that block's own heading:
 * 編輯 while the block is read-only, 取消/完成編輯 while it is open. It knows
 * nothing about the other two blocks — any number of them may be open at once.
 *
 * Module-level on purpose. Declared inside DefinitionCard it would be a new
 * component type on every keystroke, remounting `children` — and block ③'s
 * child is the 版本紀錄 entry, whose open list would close itself the moment the
 * SOP draft changed underneath it. */
function SectionEditSwitch({
  block,
  sectionTitle,
  editing,
  busy,
  onEdit,
  onCancel,
  onDone,
  children,
}: {
  block: DefBlock;
  /** The block's own question — the only thing that tells the three otherwise
   * identical 編輯 buttons apart for a screen reader. */
  sectionTitle: string;
  editing: boolean;
  busy: boolean;
  onEdit: () => void;
  onCancel: () => void;
  onDone: () => void;
  /** Extra controls for this block's edit row (block ③'s 版本紀錄 entry). */
  children?: ReactNode;
}) {
  const { t, msg } = useI18n();
  if (editing) {
    return (
      <div className="manual-sec__switch doc-card__actions">
        {children}
        <button
          type="button"
          className="doc-btn"
          disabled={busy}
          data-testid={`manual-def-cancel-${block}`}
          onClick={onCancel}
        >
          {t.settings.cancel}
        </button>
        <button
          type="button"
          className="doc-btn doc-btn--accent"
          disabled={busy}
          data-testid={`manual-def-done-${block}`}
          onClick={onDone}
        >
          {t.settings.doneEdit}
        </button>
      </div>
    );
  }
  return (
    <button
      type="button"
      className="manual-sec__switch doc-btn doc-btn--edit"
      aria-label={msg.manualEditSection(sectionTitle)}
      data-testid={`manual-def-edit-${block}`}
      onClick={onEdit}
    >
      <PencilIcon size={14} />
      <span>{t.settings.edit}</span>
    </button>
  );
}

/** 任務定義 — the three numbered sections (spec T-8a4a, mockup
 * att-fed0a5a3d1fa). §1「這是什麼任務?」/ §2「需要哪些資訊?」/ §3「該怎麼做?」
 * are edited SEPARATELY (owner 2026-07-31, proposal P1): each block carries its
 * own 編輯 switch in its own heading, and 完成編輯 writes a PATCH holding THAT
 * BLOCK'S KEY ALONE ({purpose} / {fields} / {sopMd}). The card-wide single
 * switch it replaces is why the 版本紀錄 entry read as the page's: only the SOP
 * is versioned, so the entry now lives in block ③'s edit row and nowhere else.
 *
 * ALL THREE MAY BE OPEN AT ONCE, each holding its own draft (owner 2026-07-31,
 * superseding the one-block-at-a-time rule this shipped with). That rule
 * disabled the other two switches so a block change could not discard typing —
 * which avoided the problem rather than removing it. Per-block drafts remove
 * it: opening, cancelling or saving one block does not touch another's state,
 * so "switching lost my typing" has nowhere left to happen and no switch needs
 * to be dead.
 *
 * Scoping the PATCH to the committing block is what makes that safe. With the
 * neighbours open and dirty, a card-wide payload would push their unfinished
 * drafts to the server on someone else's 完成編輯 — and the screen looks
 * identical either way. Same reasoning covers a concurrent write: only the
 * committing block's draft was seeded from what the owner actually saw.
 *
 * Server quality gate (migration 00010): an identity-key field MUST be
 * required. The 必填/識別鍵 badges enforce it here — marking 識別鍵 forces 必填
 * on; clearing 必填 also clears 識別鍵 — so the UI can never emit the isKey &&
 * !required combination the server rejects with a 400. */
function DefinitionCard({
  manual,
  onSave,
  onRestored,
}: {
  manual: TaskManualView;
  onSave: (patch: TaskManualPatch) => Promise<unknown>;
  /** Re-read the manual after a 版本紀錄 restore. */
  onRestored?: () => Promise<unknown> | void;
}) {
  const { t } = useI18n();
  /** Which blocks are open. Any number of them, independently — see the card's
   * doc comment. Empty = the whole card read-only. */
  const [openBlocks, setOpenBlocks] = useState<ReadonlySet<DefBlock>>(
    () => new Set()
  );
  /** The block whose 完成編輯 is in flight — only ITS buttons go dead. */
  const [savingBlock, setSavingBlock] = useState<DefBlock | null>(null);
  const [saveError, setSaveError] = useState(false);
  // Edit-mode drafts — the open block's is seeded from the manual on 編輯
  // (startEdit), discarded on 取消. The read-only view renders `manual.*`
  // directly (always server-fresh), so an SSE refetch can never clobber an
  // in-flight edit.
  const [purposeDraft, setPurposeDraft] = useState(manual.purpose);
  const [fieldsDraft, setFieldsDraft] = useState<TaskManualFieldView[]>(() =>
    structuredClone(manual.fields)
  );
  const [sopDraft, setSopDraft] = useState(manual.sopMd);
  const seededKey = useRef(manual.typeKey);
  const purposeRef = useRef<HTMLTextAreaElement>(null);

  // If the manual identity (typeKey) changes under us, drop any edit state so
  // the card never shows one manual's draft over another's content.
  useEffect(() => {
    if (seededKey.current === manual.typeKey) return;
    seededKey.current = manual.typeKey;
    setOpenBlocks(new Set());
    setSaveError(false);
  }, [manual]);

  // Keep the purpose textarea sized to its content while §1 is open.
  useLayoutEffect(() => {
    if (openBlocks.has(1)) autosize(purposeRef.current);
  }, [openBlocks, purposeDraft]);

  /** Add/remove ONE block from the open set, leaving the others exactly as they
   * were — open, and holding whatever their owner has typed. */
  function setBlockOpen(block: DefBlock, open: boolean) {
    setOpenBlocks((prev) => {
      const next = new Set(prev);
      if (open) next.add(block);
      else next.delete(block);
      return next;
    });
  }

  function startEdit(block: DefBlock) {
    // Seed THIS block's draft from the manual. The other two are untouched:
    // re-seeding them here is what would throw away a neighbour's typing.
    if (block === 1) setPurposeDraft(manual.purpose);
    if (block === 2) setFieldsDraft(structuredClone(manual.fields));
    if (block === 3) setSopDraft(manual.sopMd);
    setSaveError(false);
    setBlockOpen(block, true);
  }

  /** 取消 — discard THIS block's draft (back to the stored content) and close
   * it. The other blocks keep both their editors and their drafts. */
  function cancelEdit(block: DefBlock) {
    if (block === 1) setPurposeDraft(manual.purpose);
    if (block === 2) setFieldsDraft(structuredClone(manual.fields));
    if (block === 3) setSopDraft(manual.sopMd);
    setBlockOpen(block, false);
    setSaveError(false);
  }

  // 完成編輯 — persist THIS BLOCK's change as a partial PATCH carrying its key
  // alone, then leave edit mode. Blank-named field rows are drafts-in-progress:
  // dropped from the payload (the server rejects a blank name). A no-op edit
  // skips the call.
  async function commit(block: DefBlock) {
    const patch: TaskManualPatch = {};
    if (block === 1 && purposeDraft !== manual.purpose) {
      patch.purpose = purposeDraft;
    }
    if (block === 2) {
      const fieldsPayload = fieldsDraft.filter((f) => f.name.trim() !== "");
      if (!fieldsEqual(fieldsPayload, manual.fields)) {
        patch.fields = fieldsPayload;
      }
    }
    if (block === 3 && sopDraft !== manual.sopMd) patch.sopMd = sopDraft;
    if (Object.keys(patch).length === 0) {
      setBlockOpen(block, false);
      return;
    }
    setSavingBlock(block);
    setSaveError(false);
    try {
      await onSave(patch);
      setBlockOpen(block, false);
    } catch (e) {
      console.warn("TaskManualsPage: definition save failed", e);
      setSaveError(true);
    } finally {
      setSavingBlock(null);
    }
  }

  /** The switch every block's heading ends with — same shape for all three, the
   * 版本紀錄 entry passed in for ③ only. */
  function switchFor(block: DefBlock, sectionTitle: string, extra?: ReactNode) {
    return (
      <SectionEditSwitch
        block={block}
        sectionTitle={sectionTitle}
        editing={openBlocks.has(block)}
        busy={savingBlock === block}
        onEdit={() => startEdit(block)}
        onCancel={() => cancelEdit(block)}
        onDone={() => void commit(block)}
      >
        {extra}
      </SectionEditSwitch>
    );
  }

  function mapField(
    idx: number,
    fn: (f: TaskManualFieldView) => TaskManualFieldView
  ) {
    return fieldsDraft.map((f, i) => (i === idx ? fn(f) : f));
  }
  // isKey ⟹ required (migration 00010): key ON forces required ON; required
  // OFF also clears key. Draft-only — persisted on 完成編輯.
  function toggleRequired(idx: number) {
    setFieldsDraft(
      mapField(idx, (f) => {
        const required = !f.required;
        return { ...f, required, isKey: required ? f.isKey : false };
      })
    );
  }
  function toggleKey(idx: number) {
    setFieldsDraft(
      mapField(idx, (f) => {
        const isKey = !f.isKey;
        return { ...f, isKey, required: isKey ? true : f.required };
      })
    );
  }

  return (
    <div className="manual-def" data-testid="manual-definition-card">
      {/* ① 這是什麼任務? — purpose. Read-only text by default; autosizing textarea
       * while editing. */}
      <section className="manual-sec" data-testid="manual-section-1">
        <div className="manual-sec__head">
          <span className="manual-sec__num">1</span>
          <span className="manual-sec__title">{t.settings.manualQ1}</span>
          {switchFor(1, t.settings.manualQ1)}
        </div>
        <div className="manual-sec__sub">{t.settings.manualQ1Hint}</div>
        {openBlocks.has(1) ? (
          <textarea
            ref={purposeRef}
            className="manual-input manual-input--purpose"
            value={purposeDraft}
            placeholder={t.settings.manualQ1Placeholder}
            aria-label={t.settings.manualQ1}
            data-testid="manual-purpose-input"
            onChange={(e) => {
              setPurposeDraft(e.target.value);
              autosize(e.target);
            }}
          />
        ) : manual.purpose ? (
          <p className="manual-readonly-text" data-testid="manual-purpose-view">
            {manual.purpose}
          </p>
        ) : (
          <span className="manual-q__empty">{t.settings.manualEmptyHint}</span>
        )}
      </section>

      {/* ② 需要哪些資訊? — the field list. Read-only rows (name + 必填/選填 +
       * 識別鍵 badges) by default; editable rows (+新增/刪除/toggle) while editing. */}
      <section className="manual-sec" data-testid="manual-section-2">
        <div className="manual-sec__head">
          <span className="manual-sec__num">2</span>
          <span className="manual-sec__title">{t.settings.manualQ2}</span>
          {switchFor(2, t.settings.manualQ2)}
        </div>
        <div className="manual-sec__sub">{t.settings.manualQ2Hint}</div>
        {openBlocks.has(2) ? (
          <div className="manual-fields" data-testid="manual-fields-editor">
            {fieldsDraft.map((f, idx) => (
              <div className="manual-field manual-field--edit" key={idx}>
                <input
                  className="manual-input manual-field__name-input"
                  value={f.name}
                  placeholder={t.settings.manualFieldNamePlaceholder}
                  aria-label={t.settings.manualFieldNamePlaceholder}
                  data-testid={`manual-field-name-${idx}`}
                  onChange={(e) =>
                    setFieldsDraft((prev) =>
                      prev.map((x, i) =>
                        i === idx ? { ...x, name: e.target.value } : x
                      )
                    )
                  }
                />
                <button
                  type="button"
                  className={`manual-pill manual-pill--required${
                    f.required ? " manual-pill--on" : ""
                  }`}
                  aria-pressed={f.required}
                  data-testid={`manual-field-required-${idx}`}
                  onClick={() => toggleRequired(idx)}
                >
                  {t.settings.manualFieldRequired}
                </button>
                <button
                  type="button"
                  className={`manual-pill manual-pill--key${
                    f.isKey ? " manual-pill--on" : ""
                  }`}
                  aria-pressed={f.isKey}
                  data-testid={`manual-field-key-${idx}`}
                  onClick={() => toggleKey(idx)}
                >
                  {t.settings.manualFieldKey}
                </button>
                <button
                  type="button"
                  className="manual-field__remove"
                  aria-label={t.settings.manualRemoveField}
                  title={t.settings.manualRemoveField}
                  data-testid={`manual-field-remove-${idx}`}
                  onClick={() =>
                    setFieldsDraft((prev) => prev.filter((_, i) => i !== idx))
                  }
                >
                  <TrashIcon size={14} />
                </button>
              </div>
            ))}
            <button
              type="button"
              className="manual-add-field"
              data-testid="manual-field-add"
              onClick={() =>
                setFieldsDraft((prev) => [
                  ...prev,
                  { name: "", required: false, isKey: false },
                ])
              }
            >
              + {t.settings.manualAddField}
            </button>
          </div>
        ) : manual.fields.length > 0 ? (
          <div className="manual-fields" data-testid="manual-fields-view">
            {manual.fields.map((f, idx) => (
              <div
                className="manual-field manual-field--view"
                key={idx}
                data-testid={`manual-field-view-${idx}`}
              >
                <span className="manual-field__name manual-key">{f.name}</span>
                <span
                  className={`manual-pill manual-pill--required${
                    f.required ? " manual-pill--on" : ""
                  }`}
                >
                  {f.required
                    ? t.settings.manualFieldRequired
                    : t.settings.manualFieldOptional}
                </span>
                {f.isKey && (
                  <span className="manual-pill manual-pill--key manual-pill--on">
                    {t.settings.manualFieldKey}
                  </span>
                )}
              </div>
            ))}
          </div>
        ) : (
          <span className="manual-q__empty">{t.settings.manualNoFields}</span>
        )}
      </section>

      {/* ③ 該怎麼做? — the SOP. Rendered markdown by default; raw editor while
       * editing (no per-section 編輯 toggle any more — the card switch owns it). */}
      <section className="manual-sec" data-testid="manual-section-3">
        <div className="manual-sec__head">
          <span className="manual-sec__num">3</span>
          <span className="manual-sec__title">{t.settings.manualQ3}</span>
          <span className="manual-sec__aside">{t.settings.manualQ3Hint}</span>
          {/* 版本紀錄 — the ONLY place it appears on this page: only the SOP is
            * versioned, and this is the SOP's own edit row. A task manual has no
            * file seed, so its list carries no 初始版本 row. */}
          {switchFor(
            3,
            t.settings.manualQ3,
            <DocumentHistoryEntry
              kind="task_manual_sop"
              docKey={manual.typeKey}
              title={t.settings.historySopTitle}
              note={t.settings.historySopSub}
              currentContent={{ sop_md: manual.sopMd }}
              docDeletable
              // A restore rewrote the SOP under the editor — leaving the draft
              // up would turn 完成編輯 into an undo of the restore.
              onRestored={async () => {
                await onRestored?.();
                cancelEdit(3);
              }}
              disabled={savingBlock === 3}
            />
          )}
        </div>
        <div className="doc-card manual-sop-card">
          <div className="doc-card__body">
            {openBlocks.has(3) ? (
              <textarea
                className="doc-editor manual-input--sop"
                value={sopDraft}
                spellCheck={false}
                placeholder={t.settings.editorPlaceholder}
                aria-label={t.settings.manualQ3}
                data-testid="manual-sop-input"
                onChange={(e) => setSopDraft(e.target.value)}
              />
            ) : manual.sopMd ? (
              <Markdown source={manual.sopMd} className="doc-md" />
            ) : (
              <span className="manual-q__empty">
                {t.settings.manualEmptyHint}
              </span>
            )}
          </div>
        </div>
      </section>

      {saveError && <div className="set-error">{t.settings.manualSaveError}</div>}
    </div>
  );
}

// ── 負責成員 (hub summary card + panel-style editor) ─────────────────────────

/** One full-width segmented control (the mockup's toggle/chips language). */
function Segmented<T extends string>({
  options,
  value,
  onPick,
  testidPrefix,
  ariaLabel,
}: {
  options: { value: T; label: string }[];
  value: T | null;
  onPick: (v: T) => void;
  testidPrefix: string;
  ariaLabel: string;
}) {
  return (
    <div className="manual-seg" role="radiogroup" aria-label={ariaLabel}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          className={`manual-seg__cell${
            value === o.value ? " manual-seg__cell--active" : ""
          }`}
          data-testid={`${testidPrefix}-${o.value}`}
          onClick={() => onPick(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** 負責成員 — the type's executor setting. Summary card on the hub; 編輯
 * expands the member-panel-style editor in place (mock-manual-assignee-edit).
 * Assignment itself stays the server's — this card only writes the setting. */
function AssigneeCard({
  manual,
  members,
  onSave,
}: {
  manual: TaskManualSummaryView;
  members: Member[];
  onSave: (patch: TaskManualPatch) => Promise<unknown>;
}) {
  const { t } = useI18n();
  const roster = members.filter((m) => m.kind === "staff");
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [saveError, setSaveError] = useState(false);

  // Machines (the 機器 section): registry = /api/machines (online flag);
  // busy = monitoring's per-machine live agent count (>0 on an online machine
  // ⇒ 忙碌; online with 0 ⇒ 閒置; offline ⇒ 離線). Honest join of EXISTING
  // data — no fabricated load metric.
  const { machines } = useMachines();
  const { monitoring } = useMonitoring();
  const agentsOf = new Map(
    (monitoring?.machines ?? []).map((m) => [m.machine, m.agents])
  );
  function machineStateText(machineId: string, online: boolean): string {
    if (!online) return t.settings.assigneeMachineOffline;
    return (agentsOf.get(machineId) ?? 0) > 0
      ? t.settings.assigneeMachineBusy
      : t.settings.assigneeMachineIdle;
  }

  // Draft axes for the editor: kind + the per-kind knobs.
  const [kindDraft, setKindDraft] = useState<"member" | "outsource" | null>(
    null
  );
  const [memberDraft, setMemberDraft] = useState("");
  const [runtimeDraft, setRuntimeDraft] = useState<"claude" | "codex">("claude");
  const [modelDraft, setModelDraft] = useState("");
  const [effortDraft, setEffortDraft] = useState("medium");
  // copies: >=1 = a finite count; 0 = 無限 (wire spec TaskManualDTO).
  const [copiesDraft, setCopiesDraft] = useState(1);
  // "" = no machine chosen; the type then starts no worker until one is picked.
  const [machineDraft, setMachineDraft] = useState("");

  function startEdit() {
    const a = manual.assignee;
    setKindDraft(a === null ? null : a.kind);
    setMemberDraft(a?.kind === "member" ? a.memberId : (roster[0]?.id ?? ""));
    setRuntimeDraft(
      a?.kind === "outsource" ? a.runtime || "claude" : "claude"
    );
    setModelDraft(a?.kind === "outsource" ? a.model : "");
    setEffortDraft(a?.kind === "outsource" ? a.effort || "medium" : "medium");
    setCopiesDraft(a?.kind === "outsource" ? Math.max(0, a.copies) : 1);
    setMachineDraft(a?.kind === "outsource" ? a.machine : "");
    setSaveError(false);
    setEditing(true);
  }

  async function commitAssignee(assignee: ManualAssigneeView) {
    setBusy(true);
    setSaveError(false);
    try {
      await onSave({ assignee });
      setEditing(false);
    } catch (e) {
      console.warn("TaskManualsPage: assignee save failed", e);
      setSaveError(true);
    } finally {
      setBusy(false);
    }
  }

  function commit() {
    const assignee: ManualAssigneeView =
      kindDraft === null
        ? null
        : kindDraft === "member"
          ? { kind: "member", memberId: memberDraft }
          : {
              kind: "outsource",
              runtime: runtimeDraft,
              model: modelDraft.trim(),
              effort: effortDraft,
              copies: Math.max(0, Math.floor(copiesDraft)),
              machine: machineDraft,
            };
    void commitAssignee(assignee);
  }

  function machineName(id: string): string {
    return machines.find((m) => m.machineId === id)?.displayName ?? id;
  }

  // Role label for a member pick row — the shared resolution order (same as
  // PresenceBadge / RepliesPage): i18n label for a known seed key, else the
  // server-resolved custom-role title (roleName), else the raw key. Empty when
  // the member carries no role data → the row omits the label (honest, no
  // fabricated text).
  function roleLabel(m: Member): string {
    return (
      (t.office.role as Record<string, string>)[m.role] ??
      (m.roleName || m.role)
    );
  }

  // The honest one-line summary (mockup: 外包 · Opus 4.6 · 中 · 自動分配 · ×1).
  function assigneeText(): string {
    const a = manual.assignee;
    if (a === null) return t.settings.assigneeUnset;
    if (a.kind === "member") {
      const m = roster.find((x) => x.id === a.memberId);
      return `${t.settings.assigneeKindMember} · ${m?.name ?? a.memberId}`;
    }
    const effort =
      effortText(t, (a.effort || "medium") as Effort) ?? a.effort;
    const model = a.model || "—";
    const machine = a.machine
      ? machineName(a.machine)
      : t.settings.assigneeMachineUnset;
    const copies =
      a.copies === 0 ? t.settings.assigneeUnlimited : `×${a.copies}`;
    return `${t.settings.assigneeKindOutsource} · ${model} · ${effort} · ${machine} · ${copies}`;
  }

  return (
    <div
      className={`manual-assignee-card${
        editing ? " manual-assignee-card--editing" : ""
      }`}
      data-testid="manual-assignee-card"
    >
      {!editing ? (
        <div className="manual-assignee-card__row">
          <span
            className={`manual-assignee-card__icon${
              manual.assignee?.kind === "member"
                ? " manual-assignee-card__icon--member"
                : ""
            }`}
          >
            {manual.assignee?.kind === "member" ? (
              <UserIcon size={18} />
            ) : (
              <BriefcaseIcon size={18} />
            )}
          </span>
          <span className="manual-assignee-card__body">
            <span className="manual-assignee-card__sub">
              {t.settings.assigneeSummarySub}
            </span>
            <span
              className="manual-assignee-card__value"
              data-testid="manual-assignee"
            >
              {assigneeText()}
            </span>
          </span>
          <button
            type="button"
            className="doc-btn doc-btn--edit"
            onClick={startEdit}
            data-testid="manual-assignee-edit"
          >
            <PencilIcon size={14} />
            <span>{t.settings.edit}</span>
          </button>
        </div>
      ) : (
        <div className="manual-assignee-editor" data-testid="manual-assignee-editor">
          {/* 指定成員 / 外包 — full-width two-cell segmented toggle. */}
          <Segmented
            options={[
              { value: "member", label: t.settings.assigneeToggleMember },
              { value: "outsource", label: t.settings.assigneeToggleOutsource },
            ]}
            value={kindDraft}
            onPick={(v) => setKindDraft(v)}
            testidPrefix="manual-assignee-kind"
            ariaLabel={t.settings.assigneeTitle}
          />

          {kindDraft === "member" && (
            <div className="manual-assignee-editor__section">
              <div className="manual-assignee-editor__label">
                {t.settings.assigneeToggleMember}
              </div>
              {roster.length === 0 ? (
                <div className="manual-q__empty">
                  {t.settings.assigneeNoMembers}
                </div>
              ) : (
                <div className="manual-pick-list" role="radiogroup">
                  {roster.map((m) => (
                    <button
                      key={m.id}
                      type="button"
                      role="radio"
                      aria-checked={memberDraft === m.id}
                      className={`manual-pick-row${
                        memberDraft === m.id ? " manual-pick-row--active" : ""
                      }`}
                      data-testid={`manual-assignee-member-${m.id}`}
                      onClick={() => setMemberDraft(m.id)}
                    >
                      <span
                        className={`manual-pick-row__check${
                          memberDraft === m.id
                            ? " manual-pick-row__check--on"
                            : ""
                        }`}
                      />
                      <span className="manual-pick-row__name">{m.name}</span>
                      {/* Role at the flex end — mirrors the machine list's
                       * right-side state text, so the owner can tell who is
                       * what role while picking. Omitted when unknown. */}
                      {roleLabel(m) && (
                        <span
                          className="manual-pick-row__role"
                          data-testid={`manual-assignee-member-role-${m.id}`}
                        >
                          {roleLabel(m)}
                        </span>
                      )}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}

          {kindDraft === "outsource" && (
            <>
              <div className="manual-assignee-editor__section">
                <div className="manual-assignee-editor__label">
                  {t.mp.agentRuntime}
                </div>
                <select
                  className="manual-input"
                  value={runtimeDraft}
                  data-testid="manual-assignee-runtime"
                  onChange={(e) => {
                    setRuntimeDraft(e.target.value as "claude" | "codex");
                    setModelDraft("");
                  }}
                >
                  <option value="claude">Claude Code</option>
                  <option value="codex">Codex</option>
                </select>
              </div>
              {/* 模型 — the member panel's quick-pick vocabulary
               * (MODEL_QUICK_PICKS, same source as ModelEffortEditor) as
               * segmented chips + the authoritative free input. */}
              <div className="manual-assignee-editor__section">
                <div className="manual-assignee-editor__label">
                  {t.settings.assigneeModelLabel}
                </div>
                {runtimeDraft === "claude" && (
                  <Segmented
                    options={MODEL_QUICK_PICKS.map((m) => ({
                      value: m,
                      label: m,
                    }))}
                    value={
                      (MODEL_QUICK_PICKS as readonly string[]).includes(
                        modelDraft
                      )
                        ? (modelDraft as (typeof MODEL_QUICK_PICKS)[number])
                        : null
                    }
                    onPick={(v) => setModelDraft(v)}
                    testidPrefix="manual-assignee-model"
                    ariaLabel={t.settings.assigneeModelLabel}
                  />
                )}
                {runtimeDraft === "codex" ? (
                  <>
                    <Segmented
                      options={CODEX_MODEL_OPTIONS.map((m) => ({
                        value: m,
                        label: m,
                      }))}
                      value={
                        (CODEX_MODEL_OPTIONS as readonly string[]).includes(modelDraft)
                          ? (modelDraft as (typeof CODEX_MODEL_OPTIONS)[number])
                          : null
                      }
                      onPick={setModelDraft}
                      testidPrefix="manual-assignee-model"
                      ariaLabel={t.settings.assigneeModelLabel}
                    />
                    <input
                      className="manual-input manual-assignee__model"
                      value={modelDraft}
                      placeholder={t.settings.assigneeModelPlaceholder}
                      aria-label={t.settings.assigneeModelPlaceholder}
                      data-testid="manual-assignee-model"
                      onChange={(e) => setModelDraft(e.target.value)}
                    />
                  </>
                ) : (
                  <input
                    className="manual-input manual-assignee__model"
                    value={modelDraft}
                    placeholder={t.settings.assigneeModelPlaceholder}
                    aria-label={t.settings.assigneeModelPlaceholder}
                    data-testid="manual-assignee-model"
                    onChange={(e) => setModelDraft(e.target.value)}
                  />
                )}
              </div>

              {/* 投入程度 — 低/中/高/最高 segmented. */}
              <div className="manual-assignee-editor__section">
                <div className="manual-assignee-editor__label">
                  {t.settings.assigneeEffort}
                </div>
                <Segmented
                  options={EFFORTS.map((e) => ({
                    value: e,
                    label: effortText(t, e),
                  }))}
                  value={effortDraft as (typeof EFFORTS)[number]}
                  onPick={(v) => setEffortDraft(v)}
                  testidPrefix="manual-assignee-effort"
                  ariaLabel={t.settings.assigneeEffort}
                />
              </div>

              {/* 機器 — the honest machines list; no machine chosen means no worker starts. */}
              <div className="manual-assignee-editor__section">
                <div className="manual-assignee-editor__label">
                  {t.settings.assigneeMachineLabel}
                </div>
                <div className="manual-pick-list" role="radiogroup">
                  {machines.map((m) => (
                    <button
                      key={m.machineId}
                      type="button"
                      role="radio"
                      aria-checked={machineDraft === m.machineId}
                      className={`manual-pick-row${
                        machineDraft === m.machineId
                          ? " manual-pick-row--active"
                          : ""
                      }`}
                      data-testid={`manual-assignee-machine-${m.machineId}`}
                      onClick={() => setMachineDraft(m.machineId)}
                    >
                      <span
                        className={`manual-pick-row__check${
                          machineDraft === m.machineId
                            ? " manual-pick-row__check--on"
                            : ""
                        }`}
                      />
                      <span className="manual-pick-row__name manual-key">
                        {m.displayName}
                      </span>
                      <span
                        className={`manual-pick-row__state manual-pick-row__state--${
                          m.online
                            ? (agentsOf.get(m.machineId) ?? 0) > 0
                              ? "busy"
                              : "idle"
                            : "offline"
                        }`}
                      >
                        {machineStateText(m.machineId, m.online)}
                      </span>
                    </button>
                  ))}
                </div>
                <div className="manual-assignee-editor__note">
                  {t.settings.assigneeMachineNote}
                </div>
              </div>

              {/* 雇用數量 — −/＋ stepper + 無限 (copies=0 on the wire). */}
              <div className="manual-assignee-editor__section">
                <div className="manual-assignee-editor__label">
                  {t.settings.assigneeCopies}
                </div>
                <div className="manual-stepper-row">
                  <div className="manual-stepper">
                    <button
                      type="button"
                      className="manual-stepper__btn"
                      aria-label={t.settings.assigneeCopiesDecrease}
                      disabled={copiesDraft === 1}
                      data-testid="manual-assignee-copies-dec"
                      onClick={() =>
                        setCopiesDraft((n) => (n === 0 ? 1 : Math.max(1, n - 1)))
                      }
                    >
                      −
                    </button>
                    <span
                      className="manual-stepper__value"
                      data-testid="manual-assignee-copies"
                    >
                      {copiesDraft === 0 ? "∞" : copiesDraft}
                    </span>
                    <button
                      type="button"
                      className="manual-stepper__btn"
                      aria-label={t.settings.assigneeCopiesIncrease}
                      data-testid="manual-assignee-copies-inc"
                      onClick={() =>
                        setCopiesDraft((n) => (n === 0 ? 1 : n + 1))
                      }
                    >
                      ＋
                    </button>
                  </div>
                  <button
                    type="button"
                    className={`manual-unlimited${
                      copiesDraft === 0 ? " manual-unlimited--active" : ""
                    }`}
                    aria-pressed={copiesDraft === 0}
                    data-testid="manual-assignee-copies-unlimited"
                    onClick={() => setCopiesDraft((n) => (n === 0 ? 1 : 0))}
                  >
                    {t.settings.assigneeUnlimited}
                  </button>
                </div>
              </div>
            </>
          )}

          <div className="manual-assignee-editor__footer">
            {manual.assignee !== null && (
              // 解除設定 — the wire's honest third state ({} unsets); the
              // segmented toggle alone cannot express it.
              <button
                type="button"
                className="doc-btn manual-assignee-editor__clear"
                disabled={busy}
                data-testid="manual-assignee-clear"
                onClick={() => void commitAssignee(null)}
              >
                {t.settings.assigneeClear}
              </button>
            )}
            <button
              type="button"
              className="doc-btn"
              onClick={() => setEditing(false)}
              disabled={busy}
            >
              {t.settings.cancel}
            </button>
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              onClick={commit}
              disabled={busy}
              data-testid="manual-assignee-done"
            >
              {t.settings.doneEdit}
            </button>
          </div>
          {saveError && (
            <div className="set-error">{t.settings.manualSaveError}</div>
          )}
        </div>
      )}
      {!editing && saveError && (
        <div className="set-error">{t.settings.manualSaveError}</div>
      )}
    </div>
  );
}

/** 學習經驗 — the type's accumulated feedback (agent write-back on task close;
 * owner-editable). The DocDetail edit pattern, learnings-scoped. */
function LearningsCard({
  manual,
  onSave,
  onRestored,
}: {
  manual: TaskManualView;
  onSave: (patch: TaskManualPatch) => Promise<unknown>;
  /** Re-read the manual after a 版本紀錄 restore. */
  onRestored?: () => Promise<unknown> | void;
}) {
  const { t } = useI18n();
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [saveError, setSaveError] = useState(false);

  function startEdit() {
    setDraft(manual.learnings);
    setSaveError(false);
    setEditing(true);
  }

  async function commit() {
    setBusy(true);
    setSaveError(false);
    try {
      await onSave({ learnings: draft });
      setEditing(false);
    } catch (e) {
      console.warn("TaskManualsPage: learnings save failed", e);
      setSaveError(true);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="doc-card" data-testid="manual-learnings-card">
      <div className="doc-card__head">
        <span className="doc-card__file" />
        {editing ? (
          <div className="doc-card__actions">
            <DocumentHistoryEntry
              kind="task_manual_learnings"
              docKey={manual.typeKey}
              title={t.settings.historyManualLearningsTitle}
              currentContent={{ learnings: manual.learnings }}
              docDeletable
              onRestored={async () => {
                await onRestored?.();
                setEditing(false);
              }}
              disabled={busy}
            />
            <button
              type="button"
              className="doc-btn"
              onClick={() => setEditing(false)}
              disabled={busy}
            >
              {t.settings.cancel}
            </button>
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              onClick={() => void commit()}
              disabled={busy}
              data-testid="manual-learnings-done"
            >
              {t.settings.doneEdit}
            </button>
          </div>
        ) : (
          <button
            type="button"
            className="doc-btn doc-btn--edit"
            onClick={startEdit}
            data-testid="manual-learnings-edit"
          >
            <PencilIcon size={14} />
            <span>{t.settings.edit}</span>
          </button>
        )}
      </div>
      <div className="doc-card__body">
        <div className="manual-q__hint">{t.settings.manualLearningsHint}</div>
        {editing ? (
          <textarea
            className="doc-editor"
            value={draft}
            autoFocus
            spellCheck={false}
            placeholder={t.settings.editorPlaceholder}
            aria-label={t.settings.manualTabLearnings}
            data-testid="manual-learnings-input"
            onChange={(e) => setDraft(e.target.value)}
          />
        ) : manual.learnings ? (
          <Markdown source={manual.learnings} className="doc-md" />
        ) : (
          <span className="manual-q__empty">{t.settings.manualEmptyHint}</span>
        )}
        {saveError && (
          <div className="set-error">{t.settings.manualSaveError}</div>
        )}
      </div>
    </div>
  );
}
