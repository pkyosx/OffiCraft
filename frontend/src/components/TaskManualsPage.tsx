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
//                   subtitle + chevron) into their own sub-pages.
//   Sub-pages     — pill tabs (任務定義 / 學習經驗) at the top switch between
//                   the two; content mirrors the guided three questions /
//                   the learnings doc. NO internal filename anywhere (owner's
//                   earlier ruling stands — manuals are content, not files;
//                   the mockup's review-pr.md chip is deliberately not built).
//   Assignee edit — segmented 指定成員/外包 toggle; model = the member panel's
//                   quick-pick chips (MODEL_QUICK_PICKS — the same source as
//                   ModelEffortEditor) + free input; 投入程度 = 低/中/高
//                   segmented; 機器 = 自動分配 + the machines list (states
//                   joined honestly from /api/machines + monitoring agents:
//                   閒置/忙碌/離線); 雇用數量 = −/＋ stepper + 無限 (wire
//                   copies=0 = unlimited, spec TaskManualDTO).

import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import type { Effort, Member } from "../types";
import type {
  ManualAssigneeView,
  TaskManualFieldView,
  TaskManualPatch,
  TaskManualView,
} from "../api/adapter";
import { isHttpStatus } from "../api/errors";
import { useMachines } from "../hooks/useMachines";
import { useMonitoring } from "../hooks/useMonitoring";
import { Markdown } from "./Markdown";
import { InlineEdit } from "./InlineEdit";
import { Breadcrumbs, type Crumb } from "./Breadcrumbs";
import "./stepper.css";
import { ConfirmModal } from "./ConfirmModal";
import { MODEL_QUICK_PICKS, EFFORTS } from "./ModelEffortEditor";
import {
  BriefcaseIcon,
  BulbIcon,
  ChevronRightIcon,
  FileTextIcon,
  PencilIcon,
  TrashIcon,
  UserIcon,
} from "./icons";

export type ManualSubTab = "definition" | "learnings";

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
  manuals: TaskManualView[];
  loading: boolean;
  error: boolean;
  /** The unified settings breadcrumb (T-8f6e) — 設定 › 任務手冊. */
  crumbs: Crumb[];
  onOpen: (typeKey: string) => void;
  /** Create by DISPLAY NAME (T-fa76): the server mints the tm- type_key. */
  onCreate: (displayName: string) => Promise<unknown>;
  onDelete: (typeKey: string) => Promise<void>;
}) {
  const { t } = useI18n();

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
                if (e.key === "Escape") resetForm();
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
          body={t.settings.deleteManualConfirm(
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

// ── 詳情 hub (mock-manual-detail: 摘要卡 + 任務規劃手風琴) ─────────────

export function TaskManualHub({
  manual,
  members,
  crumbs,
  onSave,
}: {
  manual: TaskManualView;
  /** The office roster (real assistants) — the assignee member picker. */
  members: Member[];
  /** The unified settings breadcrumb (T-8f6e) — 設定 › 任務手冊 › <type>. */
  crumbs: Crumb[];
  onSave: (patch: TaskManualPatch) => Promise<unknown>;
}) {
  const { t } = useI18n();
  // The two 任務規劃 cards are independent accordions — each toggles its own
  // editor open/closed in place (owner 2026-07-14, ex-sub-page). Both start
  // collapsed.
  const [openSet, setOpenSet] = useState<Set<ManualSubTab>>(new Set());
  function toggle(tab: ManualSubTab) {
    setOpenSet((prev) => {
      const next = new Set(prev);
      if (next.has(tab)) next.delete(tab);
      else next.add(tab);
      return next;
    });
  }
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

      {/* 任務規劃 — the two accordion cards; expanding one inlines its editor. */}
      <div className="manual-section-label">
        {t.settings.manualPlanningSection}
      </div>
      <div className="set-entries">
        <div className="manual-accordion">
          <button
            type="button"
            className="set-entry manual-entry"
            data-testid="manual-entry-definition"
            aria-expanded={openSet.has("definition")}
            onClick={() => toggle("definition")}
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
            <ChevronRightIcon
              size={18}
              className={`set-entry__chev manual-entry__caret${
                openSet.has("definition") ? " manual-entry__caret--open" : ""
              }`}
            />
          </button>
          {openSet.has("definition") && (
            <div className="manual-accordion__body">
              <DefinitionCard manual={manual} onSave={onSave} />
            </div>
          )}
        </div>
        <div className="manual-accordion">
          <button
            type="button"
            className="set-entry manual-entry"
            data-testid="manual-entry-learnings"
            aria-expanded={openSet.has("learnings")}
            onClick={() => toggle("learnings")}
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
            <ChevronRightIcon
              size={18}
              className={`set-entry__chev manual-entry__caret${
                openSet.has("learnings") ? " manual-entry__caret--open" : ""
              }`}
            />
          </button>
          {openSet.has("learnings") && (
            <div className="manual-accordion__body">
              <LearningsCard manual={manual} onSave={onSave} />
            </div>
          )}
        </div>
      </div>
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

/** 任務定義 — the three numbered sections (spec T-8a4a, mockup
 * att-fed0a5a3d1fa). §1「這是什麼任務?」/ §2「需要哪些資訊?」/ §3「該怎麼做?」
 * share ONE explicit edit switch (owner T-8a4a round 3): the whole card is
 * READ-ONLY by default (purpose text / field rows / rendered SOP markdown);
 * 編輯 flips all three into their editors at once; 完成編輯 persists every
 * change in a SINGLE partial `update_task_manual` PATCH ({purpose} / {fields} /
 * {sopMd}, only the changed keys); 取消 drops the drafts and returns to
 * read-only. There is NO per-section save any more — the ex-always-editable
 * §1/§2 blur saves and the ex-per-section §3 SOP 編輯 toggle are folded into
 * this one switch for a consistent experience.
 *
 * Server quality gate (migration 00010): an identity-key field MUST be
 * required. The 必填/識別鍵 badges enforce it here — marking 識別鍵 forces 必填
 * on; clearing 必填 also clears 識別鍵 — so the UI can never emit the isKey &&
 * !required combination the server rejects with a 400. */
function DefinitionCard({
  manual,
  onSave,
}: {
  manual: TaskManualView;
  onSave: (patch: TaskManualPatch) => Promise<unknown>;
}) {
  const { t } = useI18n();
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [saveError, setSaveError] = useState(false);
  // Edit-mode drafts — seeded from the manual on 編輯 (startEdit), discarded on
  // 取消. The read-only view renders `manual.*` directly (always server-fresh),
  // so an SSE refetch can never clobber an in-flight edit.
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
    setEditing(false);
    setSaveError(false);
  }, [manual]);

  // Keep the purpose textarea sized to its content while editing.
  useLayoutEffect(() => {
    if (editing) autosize(purposeRef.current);
  }, [editing, purposeDraft]);

  function startEdit() {
    setPurposeDraft(manual.purpose);
    setFieldsDraft(structuredClone(manual.fields));
    setSopDraft(manual.sopMd);
    setSaveError(false);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setSaveError(false);
  }

  // 完成編輯 — persist every changed section in ONE partial PATCH, then leave
  // edit mode. Blank-named field rows are drafts-in-progress: dropped from the
  // payload (the server rejects a blank name). A no-op edit skips the call.
  async function commit() {
    const patch: TaskManualPatch = {};
    if (purposeDraft !== manual.purpose) patch.purpose = purposeDraft;
    const fieldsPayload = fieldsDraft.filter((f) => f.name.trim() !== "");
    if (!fieldsEqual(fieldsPayload, manual.fields)) patch.fields = fieldsPayload;
    if (sopDraft !== manual.sopMd) patch.sopMd = sopDraft;
    if (Object.keys(patch).length === 0) {
      setEditing(false);
      return;
    }
    setBusy(true);
    setSaveError(false);
    try {
      await onSave(patch);
      setEditing(false);
    } catch (e) {
      console.warn("TaskManualsPage: definition save failed", e);
      setSaveError(true);
    } finally {
      setBusy(false);
    }
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
      {/* One edit switch for the whole task-definition area (owner T-8a4a r3):
       * 編輯 in read-only, 取消/完成編輯 while editing. */}
      <div className="manual-def__head">
        {editing ? (
          <div className="doc-card__actions">
            <button
              type="button"
              className="doc-btn"
              disabled={busy}
              data-testid="manual-def-cancel"
              onClick={cancelEdit}
            >
              {t.settings.cancel}
            </button>
            <button
              type="button"
              className="doc-btn doc-btn--accent"
              disabled={busy}
              data-testid="manual-def-done"
              onClick={() => void commit()}
            >
              {t.settings.doneEdit}
            </button>
          </div>
        ) : (
          <button
            type="button"
            className="doc-btn doc-btn--edit"
            data-testid="manual-def-edit"
            onClick={startEdit}
          >
            <PencilIcon size={14} />
            <span>{t.settings.edit}</span>
          </button>
        )}
      </div>

      {/* ① 這是什麼任務? — purpose. Read-only text by default; autosizing textarea
       * while editing. */}
      <section className="manual-sec" data-testid="manual-section-1">
        <div className="manual-sec__head">
          <span className="manual-sec__num">1</span>
          <span className="manual-sec__title">{t.settings.manualQ1}</span>
        </div>
        <div className="manual-sec__sub">{t.settings.manualQ1Hint}</div>
        {editing ? (
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
        </div>
        <div className="manual-sec__sub">{t.settings.manualQ2Hint}</div>
        {editing ? (
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
        </div>
        <div className="doc-card manual-sop-card">
          <div className="doc-card__body">
            {editing ? (
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
  manual: TaskManualView;
  members: Member[];
  onSave: (patch: TaskManualPatch) => Promise<unknown>;
}) {
  const { t } = useI18n();
  const roster = members.filter((m) => m.kind === "assistant");
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
  const [modelDraft, setModelDraft] = useState("");
  const [effortDraft, setEffortDraft] = useState("medium");
  // copies: >=1 = a finite count; 0 = 無限 (wire spec TaskManualDTO).
  const [copiesDraft, setCopiesDraft] = useState(1);
  const [machineDraft, setMachineDraft] = useState("auto");

  function startEdit() {
    const a = manual.assignee;
    setKindDraft(a === null ? null : a.kind);
    setMemberDraft(a?.kind === "member" ? a.memberId : (roster[0]?.id ?? ""));
    setModelDraft(a?.kind === "outsource" ? a.model : "");
    setEffortDraft(a?.kind === "outsource" ? a.effort || "medium" : "medium");
    setCopiesDraft(a?.kind === "outsource" ? Math.max(0, a.copies) : 1);
    setMachineDraft(a?.kind === "outsource" ? a.machine || "auto" : "auto");
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
              model: modelDraft.trim(),
              effort: effortDraft,
              copies: Math.max(0, Math.floor(copiesDraft)),
              machine: machineDraft || "auto",
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
      t.mp.effortLevel((a.effort || "medium") as Effort) ?? a.effort;
    const model = a.model || "—";
    const machine =
      !a.machine || a.machine === "auto"
        ? t.settings.assigneeMachineAuto
        : machineName(a.machine);
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
              {/* 模型 — the member panel's quick-pick vocabulary
               * (MODEL_QUICK_PICKS, same source as ModelEffortEditor) as
               * segmented chips + the authoritative free input. */}
              <div className="manual-assignee-editor__section">
                <div className="manual-assignee-editor__label">
                  {t.settings.assigneeModelLabel}
                </div>
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
                <input
                  className="manual-input manual-assignee__model"
                  value={modelDraft}
                  placeholder={t.settings.assigneeModelPlaceholder}
                  aria-label={t.settings.assigneeModelPlaceholder}
                  data-testid="manual-assignee-model"
                  onChange={(e) => setModelDraft(e.target.value)}
                />
              </div>

              {/* 投入程度 — 低/中/高 segmented. */}
              <div className="manual-assignee-editor__section">
                <div className="manual-assignee-editor__label">
                  {t.settings.assigneeEffort}
                </div>
                <Segmented
                  options={EFFORTS.map((e) => ({
                    value: e,
                    label: t.mp.effortLevel(e),
                  }))}
                  value={effortDraft as (typeof EFFORTS)[number]}
                  onPick={(v) => setEffortDraft(v)}
                  testidPrefix="manual-assignee-effort"
                  ariaLabel={t.settings.assigneeEffort}
                />
              </div>

              {/* 機器 — 自動分配 + the honest machines list. */}
              <div className="manual-assignee-editor__section">
                <div className="manual-assignee-editor__label">
                  {t.settings.assigneeMachineLabel}
                </div>
                <div className="manual-pick-list" role="radiogroup">
                  <button
                    type="button"
                    role="radio"
                    aria-checked={machineDraft === "auto"}
                    className={`manual-pick-row${
                      machineDraft === "auto" ? " manual-pick-row--active" : ""
                    }`}
                    data-testid="manual-assignee-machine-auto"
                    onClick={() => setMachineDraft("auto")}
                  >
                    <span
                      className={`manual-pick-row__check${
                        machineDraft === "auto"
                          ? " manual-pick-row__check--on"
                          : ""
                      }`}
                    />
                    <span className="manual-pick-row__name">
                      {t.settings.assigneeMachineAuto}
                    </span>
                    <span className="manual-pick-row__state">
                      {t.settings.assigneeMachineAutoHint}
                    </span>
                  </button>
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
}: {
  manual: TaskManualView;
  onSave: (patch: TaskManualPatch) => Promise<unknown>;
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
