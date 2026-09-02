// LorePage — 座艙第六個分頁「傳承」(T-33),版面照 artifacts/lore-tab-mockup.html v8。
//
// 四個子畫面:概覽 / 待審 / 對象 / 健康。只有「對象」有真的資料 ——
// 站上今天只有六條 lore route(寫入、搜尋、讀一條、讀一版、停用、恢復),而設計
// 稿上的對象目錄、待審佇列、核可與合併、撈取次數、每週侵蝕量通通沒有生產者。
//
// 🔴 那些區塊一律畫成「尚無資料來源」並寫明缺的是哪一條路,不畫 0、不畫佔位數
// 字、也不在前端自己湊一個數充當它。一個沒有生產者的 0 讀起來是「我們查過,沒
// 有」——這個分頁存在的理由就是不要再這樣讀。
//
// 設計稿上「要人判斷的」那一區照原意做成清單、不給數字:那些數要靠判斷才有,寫
// 成數字就會被當成量過的。「駁回」沒有人裁定過,所以這裡不長那顆按鈕。

import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { LoreEmptySource } from "./LoreEmptySource";
import { LoreSubjectsView } from "./LoreSubjectsView";
import "./lore.css";

type LoreTab = "overview" | "pending" | "subjects" | "health";

/** 概覽。唯一數得出來的是「可撈取的條目總數」—— 而且那個數是伺服器在搜尋回應
 * 裡給的 total,不是把畫面上的清單長度數出來的。 */
function LoreOverview() {
  const { t } = useI18n();
  const [total, setTotal] = useState<number | null>(null);
  const [truncated, setTruncated] = useState(false);

  useEffect(() => {
    let alive = true;
    api
      .searchLore({})
      .then((r) => {
        if (!alive) return;
        setTotal(r.total);
        setTruncated(r.truncated);
      })
      .catch(() => {
        // 讀不到就維持 null:這一格寧可什麼都不顯示,也不顯示一個零。
        if (alive) setTotal(null);
      });
    return () => {
      alive = false;
    };
  }, []);

  return (
    <div className="lore__view">
      <h3 className="lore__section-title">{t.lore.countsTitle}</h3>
      <div className="lore-overview__stats">
        <div className="lore-overview__stat">
          <span className="lore-overview__stat-label">
            {t.lore.entryTotalLabel}
          </span>
          {total !== null && (
            <span
              className="lore-overview__stat-value"
              data-testid="lore-entry-total"
            >
              {total}
            </span>
          )}
          <span className="lore-subjects__hint">{t.lore.entryTotalNote}</span>
          {truncated && (
            <span className="lore-subjects__hint">
              {t.lore.entryTotalTruncated}
            </span>
          )}
        </div>
      </div>
      <LoreEmptySource
        title={t.lore.subjectTotalLabel}
        why={t.lore.subjectTotalWhy}
        missing={["GET /api/lore/subjects"]}
      />
      <LoreEmptySource
        title={t.lore.pendingCountLabel}
        why={t.lore.pendingCountWhy}
        missing={["GET /api/lore/subjects?status=pending"]}
      />

      {/* 判斷出來的移到這裡,只給清單、不給數字。 */}
      <h3 className="lore__section-title">{t.lore.judgementTitle}</h3>
      <div className="lore__card" data-testid="lore-judgement">
        <div className="lore-overview__row">
          <span className="lore__badge lore__badge--status">
            {t.lore.judgementUnderspecTag}
          </span>
          <div>
            <div className="lore-overview__row-title">
              {t.lore.judgementUnderspecTitle}
            </div>
            <div className="lore-overview__row-body">
              {t.lore.judgementUnderspecBody}
            </div>
          </div>
        </div>
        <div className="lore-overview__row">
          <span className="lore__badge lore__badge--warn">
            {t.lore.judgementConflictTag}
          </span>
          <div>
            <div className="lore-overview__row-title">
              {t.lore.judgementConflictTitle}
            </div>
            <div className="lore-overview__row-body">
              {t.lore.judgementConflictBody}
            </div>
          </div>
        </div>
        <div className="lore-overview__row">
          <span className="lore__badge lore__badge--warn">
            {t.lore.judgementStripTag}
          </span>
          <div>
            <div className="lore-overview__row-title">
              {t.lore.judgementStripTitle}
            </div>
            <div className="lore-overview__row-body">
              {t.lore.judgementStripBody}
            </div>
          </div>
        </div>
        <div className="lore__note">{t.lore.judgementNote}</div>
      </div>

      <h3 className="lore__section-title">{t.lore.attentionTitle}</h3>
      <LoreEmptySource
        title={t.lore.attentionTitle}
        why={t.lore.attentionWhy}
        missing={[
          "GET /api/lore/subjects?status=pending",
          "POST /api/lore/subjects/{id}/approve",
          "POST /api/lore/subjects/{id}/merge",
        ]}
      />

      <h3 className="lore__section-title">{t.lore.mergeChartTitle}</h3>
      <LoreEmptySource
        title={t.lore.mergeChartTitle}
        why={t.lore.mergeChartWhy}
        missing={[]}
      />
    </div>
  );
}

/** 待審。四個佇列一個都沒有來源,而這一頁確定要存在 —— 所以它照實說出缺什麼,
 * 不是被藏起來。 */
function LorePendingView() {
  const { t } = useI18n();
  return (
    <div className="lore__view">
      <div className="lore__note lore__note--warn">{t.lore.pendingRuling}</div>
      <LoreEmptySource
        title={t.lore.pendingQueueEntities}
        why={t.lore.pendingEntitiesWhy}
        missing={[
          "GET /api/lore/subjects?status=pending",
          "POST /api/lore/subjects/{id}/approve",
          "POST /api/lore/subjects/{id}/merge",
        ]}
      />
      <LoreEmptySource
        title={t.lore.pendingQueueTypes}
        why={t.lore.pendingTypesWhy}
        missing={["GET /api/lore/subject-types/requests"]}
      />
      <LoreEmptySource
        title={t.lore.pendingQueueConflicts}
        why={t.lore.pendingConflictsWhy}
        missing={[]}
      />
      <LoreEmptySource
        title={t.lore.pendingQueueStrip}
        why={t.lore.pendingStripWhy}
        missing={[]}
      />
      <div className="lore__note">{t.lore.pendingNoRejectNote}</div>
    </div>
  );
}

/** 健康。三塊「沒幫助 / 有幫助 / 幫倒忙」加侵蝕,四塊都沒有來源。
 * 侵蝕唯一看得見的地方在「對象」頁的版本時間軸,所以這裡指過去。 */
function LoreHealthView() {
  const { t } = useI18n();
  return (
    <div className="lore__view">
      <div className="lore-health__grid">
        <div className="lore__card">
          <h3 className="lore-health__title">{t.lore.unhelpfulTitle}</h3>
          <p className="lore-health__body">{t.lore.unhelpfulBody}</p>
          <LoreEmptySource
            title={t.lore.unhelpfulTitle}
            why={t.lore.unhelpfulWhy}
            missing={["GET /api/lore/retrievals"]}
          />
        </div>
        <div className="lore__card">
          <h3 className="lore-health__title">
            {t.lore.helpfulTitle}
            <span className="lore__badge lore__badge--warn">
              {t.lore.helpfulWeak}
            </span>
          </h3>
          <p className="lore-health__body">{t.lore.helpfulBody}</p>
          <LoreEmptySource
            title={t.lore.helpfulTitle}
            why={t.lore.helpfulWhy}
            missing={["GET /api/lore/feedback"]}
          />
        </div>
        <div className="lore__card">
          <h3 className="lore-health__title">{t.lore.harmfulTitle}</h3>
          <p className="lore-health__body">{t.lore.harmfulBody}</p>
          <LoreEmptySource
            title={t.lore.harmfulTitle}
            why={t.lore.helpfulWhy}
            missing={["GET /api/lore/feedback"]}
          />
        </div>
      </div>

      <h3 className="lore__section-title">{t.lore.erosionTitle}</h3>
      <LoreEmptySource
        title={t.lore.erosionTitle}
        why={t.lore.erosionBody}
        missing={[]}
      />
      <div className="lore__note">{t.lore.erosionWhere}</div>
    </div>
  );
}

export function LorePage() {
  const { t } = useI18n();
  const [tab, setTab] = useState<LoreTab>("overview");

  const tabs: [LoreTab, string][] = [
    ["overview", t.lore.tabOverview],
    ["pending", t.lore.tabPending],
    ["subjects", t.lore.tabSubjects],
    ["health", t.lore.tabHealth],
  ];

  return (
    <div className="lore">
      <div className="lore__subtabs" role="tablist">
        {/* 設計稿的「待審 9」少了那個 9:沒有一條路數得出來。 */}
        {tabs.map(([key, label]) => (
          <button
            type="button"
            role="tab"
            aria-selected={tab === key}
            className={`lore__subtab${
              tab === key ? " lore__subtab--active" : ""
            }`}
            onClick={() => setTab(key)}
            key={key}
          >
            {label}
          </button>
        ))}
      </div>

      {tab === "overview" ? (
        <LoreOverview />
      ) : tab === "pending" ? (
        <LorePendingView />
      ) : tab === "subjects" ? (
        <LoreSubjectsView />
      ) : (
        <LoreHealthView />
      )}
    </div>
  );
}
