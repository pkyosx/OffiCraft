// LoreSubjectsView — 傳承 › 對象。四個畫面裡唯一真的有資料的一個。
//
// 設計稿是三欄:對象目錄 → 該對象底下的條目 → 條目詳情。第一欄做不出來 ——
// 沒有一條路列得出對象目錄(所以也就沒有「已審核一群、未審核一群、已審核的排
// 前面」那個分群)。拿搜尋結果裡出現過的對象湊一份目錄,湊出來的是「這一次撈到
// 的」而不是「站上有的」,所以第一欄改成:你自己打對象鍵,旁邊講清楚缺的是哪
// 一條路。
//
// 回應裡每一個誠實標記都印出來,而且跟結果印在一起:
//   · subjectResolved:false → 這是「站上沒有這個對象」,不是「這個對象底下沒有
//     條目」。伺服器把鍵原樣回音,所以打錯字看得見 —— 這一格畫成空清單,就等於
//     把打錯的字吞掉。
//   · applied → 分層沒有它的軸就會被讀成另一個意思,所以兩者同框。
//   · truncated → 上限切過就講,不然「下面就是全部」是假的。
//   · unmappedActions → 至少有一條是認不得就往嚴格倒分類出來的。

import { useCallback, useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { serverMessageOf } from "../api/errors";
import type { LoreSearchView } from "../types";
import { LoreEntryCard } from "./LoreEntryCard";
import { LoreEmptySource } from "./LoreEmptySource";
import "./lore.css";

export function LoreSubjectsView() {
  const { t } = useI18n();
  const [subject, setSubject] = useState("");
  const [query, setQuery] = useState("");
  const [result, setResult] = useState<LoreSearchView | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const search = useCallback(async (subjectKey: string, text: string) => {
    setBusy(true);
    setError(null);
    try {
      const next = await api.searchLore({
        ...(subjectKey.trim() !== "" ? { subject: subjectKey.trim() } : {}),
        ...(text.trim() !== "" ? { query: text.trim() } : {}),
      });
      setResult(next);
    } catch (e) {
      setResult(null);
      setError(serverMessageOf(e));
    } finally {
      setBusy(false);
    }
  }, []);

  // 開頁就撈一次不帶條件的:這一頁沒有目錄可以先看,空手進來會以為站上什麼都沒有。
  useEffect(() => {
    void search("", "");
  }, [search]);

  return (
    <div className="lore__view">
      <div className="lore-subjects">
        <div className="lore__card">
          <h3 className="lore__section-title">{t.lore.searchTitle}</h3>
          <div className="lore-subjects__field">
            <label className="lore-subjects__label" htmlFor="lore-subject">
              {t.lore.searchSubjectLabel}
            </label>
            <input
              id="lore-subject"
              className="lore-subjects__input"
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
            />
            <div className="lore-subjects__hint">
              {t.lore.searchSubjectHint}
            </div>
          </div>
          <div className="lore-subjects__field">
            <label className="lore-subjects__label" htmlFor="lore-query">
              {t.lore.searchQueryLabel}
            </label>
            <input
              id="lore-query"
              className="lore-subjects__input"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <div className="lore-subjects__hint">{t.lore.searchQueryHint}</div>
          </div>
          <button
            type="button"
            className="lore-subjects__submit"
            disabled={busy}
            onClick={() => void search(subject, query)}
          >
            {busy ? t.lore.searchBusy : t.lore.searchSubmit}
          </button>
          <div className="lore__note">{t.lore.searchCatalogueWhy}</div>
        </div>

        <div className="lore-subjects__results">
          {error !== null && (
            <div className="lore-subjects__error">
              {t.lore.searchFailed} {error}
            </div>
          )}

          {result !== null && (
            <>
              <div className="lore__card">
                <h3 className="lore__section-title">{t.lore.appliedTitle}</h3>
                <dl className="lore-subjects__applied">
                  <dt>{t.lore.appliedSubject}</dt>
                  <dd className="lore__mono">
                    {result.applied.subject === ""
                      ? t.lore.appliedNone
                      : result.applied.subject}
                  </dd>
                  <dt>{t.lore.appliedActions}</dt>
                  <dd className="lore__mono">
                    {result.applied.actions.length === 0
                      ? t.lore.appliedNone
                      : result.applied.actions.join("、")}
                  </dd>
                  <dt>{t.lore.appliedQuery}</dt>
                  <dd className="lore__mono">
                    {result.applied.query === ""
                      ? t.lore.appliedNone
                      : result.applied.query}
                  </dd>
                  <dt>{t.lore.appliedQueryMatch}</dt>
                  <dd className="lore__mono">{result.applied.queryMatch}</dd>
                  <dt>{t.lore.appliedLimit}</dt>
                  <dd className="lore__mono">{result.applied.limit}</dd>
                  <dt>{t.lore.appliedTieredBy}</dt>
                  <dd className="lore__mono">
                    {result.applied.tieredBy.length === 0
                      ? t.lore.appliedTieredByEmpty
                      : result.applied.tieredBy.join("、")}
                  </dd>
                </dl>
                <div className="lore__note">{t.lore.appliedWhy}</div>
              </div>

              {result.unmappedActions.length > 0 && (
                <div className="lore__card">
                  <h3 className="lore__section-title">
                    {t.lore.unmappedActionsTitle}
                  </h3>
                  <div className="lore__mono">
                    {result.unmappedActions.join("、")}
                  </div>
                  <div className="lore__note lore__note--warn">
                    {t.lore.unmappedActionsBody}
                  </div>
                </div>
              )}

              {/* 「站上沒有這個對象」跟「這個對象底下沒有條目」是兩個答案。 */}
              {!result.subjectResolved ? (
                <div className="lore__card" data-testid="lore-unresolved">
                  <h3 className="lore__section-title">
                    {t.lore.unresolvedTitle}
                  </h3>
                  <div>{t.lore.unresolvedBody}</div>
                  <div className="lore__note">
                    {t.lore.unresolvedEcho}：
                    <code className="lore__mono">
                      {result.unresolvedSubject}
                    </code>
                  </div>
                </div>
              ) : (
                <>
                  <div className="lore__section-title">
                    {t.lore.resultsTitle}
                    {" · "}
                    {t.lore.resultsTotalLead}
                    {result.total}
                    {t.lore.resultsTotalTail}
                  </div>
                  {result.truncated && (
                    <div className="lore__note lore__note--warn">
                      {t.lore.resultsTruncated}
                    </div>
                  )}
                  {result.entries.length === 0 ? (
                    <div className="lore__card">{t.lore.resultsEmpty}</div>
                  ) : (
                    result.entries.map((entry) => (
                      <LoreEntryCard entry={entry} key={entry.entryId} />
                    ))
                  )}
                </>
              )}
            </>
          )}
        </div>
      </div>

      <LoreEmptySource
        title={t.lore.subjectTotalLabel}
        why={t.lore.subjectTotalWhy}
        missing={["GET /api/lore/subjects"]}
      />
    </div>
  );
}
