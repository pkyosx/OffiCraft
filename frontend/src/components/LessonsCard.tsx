// components/LessonsCard.tsx — the ONE shared, owner-editable per-role lessons
// card. Extracted from MemberDetailPanel so the persona (role-definition) page
// and any future host render the SAME editor by construction (no copy-paste
// drift). Per-role-learnings step1: the doc is scoped to `roleKey` + `taskType`.
//
// Behaviour mirrors the role_def DocDetail card: Edit → textarea → Cancel/Save,
// button-commit (so no IME composition guard is needed). Always a titled card
// (no collapse). Owner scope (this UI) may write any role; the SSE "lessons"
// topic reconciles by refetch inside useLessons.

import { useState } from "react";
import { useI18n } from "../i18n";
import { useLessons } from "../hooks/useLessons";
import { Markdown } from "./Markdown";
import { LayersIcon, PencilIcon } from "./icons";
import "./member-detail.css";

interface LessonsCardProps {
  /** Role this lessons doc belongs to (per-role-learnings step1). */
  roleKey: string;
  /** The single fixed task_type key today is "general". */
  taskType?: string;
}

export function LessonsCard({ roleKey, taskType = "general" }: LessonsCardProps) {
  const { t } = useI18n();
  const {
    lessons,
    loading,
    error,
    save: saveLessons,
  } = useLessons(roleKey, taskType);

  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [saveError, setSaveError] = useState(false);
  const text = lessons?.text ?? "";

  function startEdit() {
    setDraft(text);
    setSaveError(false);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setDraft("");
    setSaveError(false);
  }

  async function commit() {
    setBusy(true);
    setSaveError(false);
    try {
      await saveLessons(draft);
      setEditing(false);
      setDraft("");
    } catch {
      setSaveError(true);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mp-card mp-lessons">
      <div className="mp-lessons__head">
        <span className="mp-lessons__title">
          <LayersIcon size={15} className="mp-lessons__icon" />
          <span>{t.mp.lessons}</span>
        </span>
        {editing ? (
          <div className="mp-lessons__actions">
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
              onClick={commit}
              disabled={busy}
            >
              {t.settings.doneEdit}
            </button>
          </div>
        ) : (
          <button
            type="button"
            className="doc-btn doc-btn--edit"
            onClick={startEdit}
            disabled={loading || error}
          >
            <PencilIcon size={14} />
            <span>{t.settings.edit}</span>
          </button>
        )}
      </div>
      <div className="mp-lessons__note">{t.mp.lessonsShared}</div>
      <div className="mp-lessons__body">
        {editing ? (
          <>
            <textarea
              className="doc-editor"
              value={draft}
              autoFocus
              spellCheck={false}
              placeholder={t.settings.editorPlaceholder}
              onChange={(e) => setDraft(e.target.value)}
            />
            {saveError && (
              <div className="mp-lessons__error">{t.mp.lessonsSaveError}</div>
            )}
          </>
        ) : loading ? (
          <span className="mp-expand__empty">{t.mp.lessonsLoading}</span>
        ) : error ? (
          <span className="mp-expand__empty">{t.mp.lessonsError}</span>
        ) : text.trim() ? (
          <Markdown source={text} className="doc-md" />
        ) : (
          <span className="mp-expand__empty">{t.mp.lessonsEmpty}</span>
        )}
      </div>
    </div>
  );
}
