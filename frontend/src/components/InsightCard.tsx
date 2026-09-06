// components/InsightCard.tsx — the per-role INSIGHT card (T-3809): the role
// journal's third block, rendered beside Duty (the role_definition doc this
// page already shows) and Learning (LessonsCard).
//
// Built against LessonsCard rather than beside it, because the two are the same
// editor over different documents. Two deliberate differences, each with a
// reason that is not "tidier":
//
//  1. The header carries {size_chars} / {cap_chars}. 🔴 This is the ONLY place
//     in the cockpit an owner can see the live doc.cap_chars.insight value without
//     being admin — the settings surface that otherwise shows it is admin-only,
//     and the alternative way to learn the limit is to be refused by it.
//  2. The empty state is a FIRST-CLASS reading, not a fallback — for a role
//     with NO file seed, "empty" is the honest answer to "has this role moved
//     anything over yet?". It must never be confused with a failed load, which
//     is why `error` renders separately.
//     🔴 Since T-e1e3 that is no longer the ONLY reading: insight folds against
//     a PER-ROLE seed (`seeds/insight_<roleKey>.md`, today only `assistant`), so
//     a non-empty doc may be FACTORY wording rather than something the role
//     wrote. `isDefault` is the only thing that tells them apart, and the badge
//     below is where this card says so.
//
// The card is NOT a privacy boundary and says so on its face: READ is
// unrestricted by owner ruling (rc-dc171587220c). Insight is SEPARATE, not
// private; only WRITE narrowed. Do not reword insightShared into a promise of
// confidentiality this system does not keep.

import { useState } from "react";
import { useI18n } from "../i18n";
import { serverMessageOf } from "../api/errors";
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
    reset: resetInsight,
  } = useInsight(roleKey);

  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [busy, setBusy] = useState(false);
  // 🔴 The server's REASON, not a flag (owner ruling 2026-08-03). `null` = no
  // failure; a string = failed, and that string is what the server said. The
  // doc-cap refusal carries instructions the person needs (how far over, what
  // the cap is, that stored text is NOT truncated, delete stale lines first) —
  // as a boolean, none of it could reach the screen. `""` is a real state: the
  // call failed with nothing quotable, and the render falls back to the i18n
  // copy rather than showing an empty error line.
  const [saveError, setSaveError] = useState<string | null>(null);
  const text = insight?.text ?? "";

  function startEdit() {
    setDraft(text);
    setSaveError(null);
    setEditing(true);
  }

  function cancelEdit() {
    setEditing(false);
    setDraft("");
    setSaveError(null);
  }

  async function commit() {
    setBusy(true);
    setSaveError(null);
    try {
      await saveInsight(draft);
      setEditing(false);
      setDraft("");
    } catch (e) {
      setSaveError(serverMessageOf(e));
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
          {/* 🔴 THE FACTORY BADGE (T-e1e3). Insight now folds against a PER-ROLE
            * file seed, so `text` being non-empty no longer proves a person
            * wrote it: an untouched `assistant` reads the factory wording.
            * Without this badge the cockpit renders shipped wording exactly like
            * something the role authored — the ticket's acceptance #4, and the
            * whole reason is_default had to be surfaced here at all.
            *
            * Gated on non-empty text as well as isDefault: a role with NO seed
            * is also is_default=true, and calling its blank card 「預設」 would
            * label an absence as a factory document. */}
          {insight?.isDefault && insight.text.trim() !== "" && (
            <span className="set-badge" data-testid="insight-default-badge">
              {t.settings.defaultBadge}
            </span>
          )}
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
            {/* 版本紀錄 (T-1f39). docKey is the BARE role_key.
              *
              * 🔴 THE 初始版本 ROW IS GATED ON hasSeed, NEVER ON isDefault
              * (T-6501). DocumentHistoryEntry's own rule is that the row may
              * only appear where a seed PROVABLY exists — a row that 404s is a
              * dead affordance. `isDefault` answers a DIFFERENT question ("has
              * this role written yet"), and the state where the two disagree is
              * the one that matters: a seeded role that has since written its
              * own reads hasSeed=true / isDefault=false, which is precisely
              * when the reset is worth offering. Gating on isDefault would hide
              * the row exactly there, and show it on every custom role the
              * server 404s.
              *
              * Also NOT RoleDefDTO.is_seed: that is a fact about the role's
              * DUTY and is derived from the factory ROLE ROSTER, while
              * Insight's roster is the SET OF FILES (seeds/insight_<role>.md),
              * decoupled on purpose. A role can carry a Duty seed and no
              * Insight seed. That conflation misled two people on 2026-08-04
              * and is the reason this field is named hasSeed.
              *
              * The confirm step is NOT wired here on purpose — it lives inside
              * DocumentHistoryEntry, so the Duty and Insight rows share ONE
              * implementation that cannot drift into two.
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
                // 🔴 `finally`, not a plain sequence: the restore has ALREADY
                // landed by the time this runs, so the draft below is stale no
                // matter what the re-read does. T-91 wrapped the re-read in
                // DocumentHistoryEntry so a failed re-read stops showing as a
                // failed restore — but that made a rejection here SKIP the
                // line below and then get swallowed, which closed the modal
                // silently and left the editor holding the pre-restore draft.
                // Leaving edit mode is what the comment above promises; it
                // must not be conditional on the re-read succeeding.
                try {
                  await refetch();
                } finally {
                  cancelEdit();
                }
              }}
              onReset={
                insight?.hasSeed
                  ? async () => {
                      await resetInsight();
                      cancelEdit();
                    }
                  : undefined
              }
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
            {saveError !== null && (
              <div className="mp-lessons__error">
                {saveError || t.mp.insightSaveError}
              </div>
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
