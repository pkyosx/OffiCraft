// components/InsightCard.tsx — the per-role INSIGHT card (T-3809): the role
// journal's third block, rendered beside Duty (the role_definition doc this
// page already shows) and Learning (LessonsCard).
//
// Built against LessonsCard rather than beside it, because the two are the same
// editor over different documents. Three deliberate differences, each with a
// reason that is not "tidier":
//
//  1. NO taskType prop. Insight has no task_type axis, so its document-history
//     key is the BARE role_key — not the "<role_key>::<task_type>" composite.
//     Passing the composite here would address a document that does not exist.
//  2. The header carries {size_chars} / {cap_chars}. 🔴 This is the ONLY place
//     in the cockpit an owner can see the live doc.cap_chars value without
//     being admin — the settings surface that otherwise shows it is admin-only,
//     and the alternative way to learn the limit is to be refused by it.
//  3. The empty state is a FIRST-CLASS reading, not a fallback. This doc has no
//     seed, so "empty" is the honest answer to "has this role moved anything
//     over yet?" — the one observable this ticket ships. It must never be
//     confused with a failed load, which is why `error` renders separately.
//
// The card is NOT a privacy boundary and says so on its face: READ is
// unrestricted by owner ruling (rc-dc171587220c). Insight is SEPARATE, not
// private; only WRITE narrowed. Do not reword insightShared into a promise of
// confidentiality this system does not keep.

import { useState } from "react";
import { useI18n } from "../i18n";
import { useInsight } from "../hooks/useInsight";
import { Markdown } from "./Markdown";
import { DocumentHistoryEntry } from "./DocumentHistoryEntry";
import { LayersIcon, PencilIcon } from "./icons";
import "./member-detail.css";

interface InsightCardProps {
  /** Role this insight doc belongs to. The document key IS this string. */
  roleKey: string;
}

export function InsightCard({ roleKey }: InsightCardProps) {
  const { t } = useI18n();
  const {
    insight,
    loading,
    error,
    refetch,
    save: saveInsight,
  } = useInsight(roleKey);

  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  const [saveError, setSaveError] = useState(false);
  const text = insight?.text ?? "";

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
      await saveInsight(draft);
      setEditing(false);
      setDraft("");
    } catch {
      setSaveError(true);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mp-card mp-lessons mp-insight">
      <div className="mp-lessons__head">
        <span className="mp-lessons__title">
          <LayersIcon size={15} className="mp-lessons__icon" />
          <span>{t.mp.insight}</span>
          {/* Always rendered once the doc has loaded — including at 0 chars.
            * Hiding it while empty would remove the cap exactly when someone is
            * about to write the first thing into the document. */}
          {insight && (
            <span className="mp-insight__size">
              {insight.sizeChars} / {insight.capChars}
            </span>
          )}
        </span>
        {editing ? (
          <div className="mp-lessons__actions">
            {/* 版本紀錄 (T-1f39). docKey is the BARE role_key — insight has no
              * task_type axis, so there is no composite to build. This doc has
              * no file seed either, so its list carries no 初始版本 row.
              *
              * 🔴 onRestored refetches LOCALLY, which is why the person who
              * clicked always sees the new value. Every OTHER open surface
              * depends on the server fanning an `insight` delta from
              * publishDocumentHistoryRestore — a switch with no default branch.
              * If that case is ever dropped, this button still returns 200, the
              * database still changes, nothing logs, and every other tab silently
              * keeps the old text. */}
            <DocumentHistoryEntry
              kind="insight"
              docKey={roleKey}
              title={t.settings.historyInsightTitle}
              currentContent={insight ? { text: insight.text } : undefined}
              onRestored={async () => {
                await refetch();
                cancelEdit();
              }}
              disabled={busy}
            />
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
            // Same load gate as LessonsCard (T-2d99): you cannot edit what has
            // not arrived, or the first commit is a whole-doc replace of "".
            disabled={loading || error}
          >
            <PencilIcon size={14} />
            <span>{t.settings.edit}</span>
          </button>
        )}
      </div>
      <div className="mp-lessons__note">{t.mp.insightShared}</div>
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
              <div className="mp-lessons__error">{t.mp.insightSaveError}</div>
            )}
          </>
        ) : loading ? (
          <span className="mp-expand__empty">{t.mp.insightLoading}</span>
        ) : error ? (
          <span className="mp-expand__empty">{t.mp.insightError}</span>
        ) : text.trim() ? (
          <Markdown source={text} className="doc-md" />
        ) : (
          <span className="mp-expand__empty">{t.mp.insightEmpty}</span>
        )}
      </div>
    </div>
  );
}
