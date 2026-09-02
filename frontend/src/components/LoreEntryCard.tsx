// LoreEntryCard — 一條傳承記錄:摺起來是搜尋結果的一列,展開是設計稿裡的條目詳情
// (六格 → 原文 → 版本時間軸 → 中繼資料 → 動作)。
//
// 展開才去讀 GET /api/lore/entries/{id}:搜尋回應本身不帶原文、不帶版本目錄,
// 拿摘要去湊一份詳情等於把「沒讀到」畫成「讀到了」。
//
// 六格空著的時候照樣把欄位名印出來(fieldEmpty),因為「空著」跟「沒有這一節」
// 必須長得不一樣 —— 一條證偽條件與實例都空的記錄,要一眼看得出它是口號。

import { useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { serverMessageOf } from "../api/errors";
import type {
  LoreEntryDetailView,
  LoreEntrySummaryView,
  LoreRevisionRowView,
} from "../types";
import { LoreEmptySource } from "./LoreEmptySource";
import "./lore.css";

/** 一格:欄位名永遠印,值空的時候印出「空白」而不是把整格藏起來。 */
function Field({ name, value }: { name: string; value: string }) {
  const { t } = useI18n();
  const empty = value.trim() === "";
  return (
    <div className="lore-entry__field">
      <div className="lore-entry__field-name">{name}</div>
      <div
        className={`lore-entry__field-value${
          empty ? " lore-entry__field-value--empty" : ""
        }`}
      >
        {empty ? t.lore.fieldEmpty : value}
      </div>
    </div>
  );
}

function RevisionRow({
  entryId,
  row,
}: {
  entryId: string;
  row: LoreRevisionRowView;
}) {
  const { t } = useI18n();
  const [body, setBody] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const shrunk = row.shrinkChars > 0;

  async function toggle() {
    if (body !== null) {
      setBody(null);
      return;
    }
    setError(null);
    try {
      const rev = await api.getLoreRevision(entryId, row.revisionId);
      setBody(rev.body);
    } catch (e) {
      setError(serverMessageOf(e));
    }
  }

  return (
    <>
      <div
        className={`lore-entry__rev${
          shrunk ? " lore-entry__rev--shrunk" : ""
        }`}
      >
        <div>
          <span className="lore-entry__rev-meta">
            {t.lore.revisionLabel}
            {row.revisionId}
            {t.lore.revisionLabelTail}
            {" · "}
            {row.actorId}
          </span>
          <div
            className={
              shrunk ? "lore-entry__rev-shrink" : "lore-entry__rev-meta"
            }
          >
            {shrunk
              ? `${t.lore.revisionShrinkLead}${row.shrinkChars}${t.lore.revisionShrinkTail}`
              : t.lore.revisionNoShrink}
          </div>
        </div>
        <button
          type="button"
          className="lore-entry__rev-btn"
          onClick={toggle}
        >
          {body === null ? t.lore.revisionView : t.lore.revisionHide}
        </button>
      </div>
      {error !== null && (
        <div className="lore-subjects__error">
          {t.lore.revisionFailed} {error}
        </div>
      )}
      {body !== null && <pre className="lore-entry__raw">{body}</pre>}
    </>
  );
}

export function LoreEntryCard({ entry }: { entry: LoreEntrySummaryView }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [detail, setDetail] = useState<LoreEntryDetailView | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function toggle() {
    if (open) {
      setOpen(false);
      return;
    }
    setOpen(true);
    if (detail !== null) return;
    setLoading(true);
    setError(null);
    try {
      setDetail(await api.getLoreEntry(entry.entryId));
    } catch (e) {
      setError(serverMessageOf(e));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="lore-entry" data-testid="lore-entry">
      <button
        type="button"
        className="lore-entry__head"
        onClick={toggle}
        aria-expanded={open}
        aria-label={open ? t.lore.entryClose : t.lore.entryOpen}
      >
        <div className="lore-entry__title">
          {entry.label.trim() === "" ? t.lore.entryNoLabel : entry.label}
        </div>
        <div className="lore-entry__axes">
          {entry.subjects.map((s) => (
            <span className="lore__badge lore__badge--subject" key={s}>
              {s}
            </span>
          ))}
          {entry.actions.map((a) => (
            <span className="lore__badge lore__badge--action" key={a}>
              {a}
            </span>
          ))}
          <span className="lore__badge lore__badge--status">
            {t.lore.entryTierLabel} {entry.tier}
          </span>
          <span className="lore__badge lore__badge--status">
            {t.lore.entryTrustScopeLabel} {entry.trustScope}
          </span>
          {/* 猜出來的類別不可以跟查表查出來的長得一樣。 */}
          {entry.trustFellBack && (
            <span className="lore__badge lore__badge--warn">
              {t.lore.entryTrustFellBack}
            </span>
          )}
          {entry.degraded && (
            <span className="lore__badge lore__badge--warn">
              {t.lore.entryDegraded}
            </span>
          )}
          <span>
            {t.lore.entryOriginLabel} {entry.origin}
          </span>
        </div>
        {entry.tierNote.trim() !== "" && (
          <div className="lore-entry__short">
            {t.lore.entryTierNoteLabel}：{entry.tierNote}
          </div>
        )}
        <div className="lore-entry__short">{entry.short}</div>
      </button>

      {open && (
        <div className="lore-entry__body">
          {loading && (
            <div className="lore-entry__block">{t.lore.entryLoading}</div>
          )}
          {error !== null && (
            <div className="lore-entry__block">
              <div className="lore-subjects__error">
                {t.lore.entryFailed} {error}
              </div>
            </div>
          )}
          {detail !== null && (
            <>
              <div className="lore-entry__block">
                <Field name={t.lore.fieldSymptoms} value={detail.symptoms} />
                <Field name={t.lore.fieldShort} value={detail.short} />
                <Field name={t.lore.fieldFalsify} value={detail.falsify} />
                <Field name={t.lore.fieldInstance} value={detail.instance} />
                <Field
                  name={t.lore.fieldResidual}
                  value={detail.residualRisk}
                />
                <div className="lore__note">{t.lore.fieldsNote}</div>
              </div>

              <div className="lore-entry__block">
                <div className="lore-entry__field-name">
                  {t.lore.detailStatusLabel}：{detail.status}
                  {" · "}
                  {t.lore.detailWrittenByLabel}：{detail.writtenBy}
                  {detail.supersedes !== "" && (
                    <>
                      {" · "}
                      {t.lore.detailSupersedesLabel}：{detail.supersedes}
                    </>
                  )}
                </div>
              </div>

              <div className="lore-entry__block">
                <div className="lore-entry__field-name">
                  {t.lore.originalTitle}
                </div>
                {detail.original.trim() === "" ? (
                  <div className="lore-entry__field-value lore-entry__field-value--empty">
                    {t.lore.originalEmpty}
                  </div>
                ) : (
                  <pre className="lore-entry__raw">{detail.original}</pre>
                )}
                <div className="lore-entry__rev-meta">
                  {t.lore.shaLabel}：
                  {detail.sha256 === "" ? t.lore.shaEmpty : detail.sha256}
                </div>
              </div>

              <div className="lore-entry__block">
                <div className="lore-entry__field-name">
                  {t.lore.revisionsTitle}
                </div>
                {detail.revisions.length === 0 ? (
                  <div className="lore-entry__field-value lore-entry__field-value--empty">
                    {t.lore.revisionsEmpty}
                  </div>
                ) : (
                  detail.revisions.map((row) => (
                    <RevisionRow
                      entryId={detail.entryId}
                      row={row}
                      key={row.revisionId}
                    />
                  ))
                )}
                <div className="lore__note">{t.lore.revisionsNote}</div>
              </div>

              {/* 撈取次數那一排:設計稿有,而沒有任何一條路回得出來。 */}
              <div className="lore-entry__block">
                <LoreEmptySource
                  title={t.lore.metaTitle}
                  why={t.lore.metaWhy}
                  missing={["GET /api/lore/entries/{id}/stats"]}
                />
              </div>

              {/* 回報／不再撈取／給分:停用與恢復兩條路存在,但這個分頁只接讀的
                  那三條;給分那一格資料庫今天根本沒有。兩件事都照實講。 */}
              <div className="lore-entry__block">
                <LoreEmptySource
                  title={t.lore.actionsTitle}
                  why={t.lore.actionsWhy}
                  missing={[]}
                />
                <div className="lore__note">{t.lore.scoreWhy}</div>
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}
