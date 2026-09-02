// LoreEmptySource — 傳承分頁上「尚無資料來源」的那一種區塊 (T-33)。
//
// 🔴 這個元件是這張票的核心,不是一個樣式包裝。站上今天只有六條 lore route
// (寫入、搜尋、讀一條、讀一版、停用、恢復);設計稿上「對象目錄」「待審佇列」
// 「核可/合併」「撈取次數」「每週被磨掉幾字」通通沒有生產者。那些格子畫成 0、
// 畫成佔位數字,或在前端拿手上的清單湊一個數,讀起來都會是「我們查過,沒有」——
// 而那正是這張票要治的病。
//
// 所以這個元件做兩件事,而且只做這兩件:說出缺的是哪一條路,以及不印任何數字。
// `missing` 收的是路徑本身(識別字,不是文案,所以不進 i18n 字典)。

import { useI18n } from "../i18n";
import "./lore.css";

export function LoreEmptySource({
  title,
  why,
  missing,
}: {
  /** 這一格本來要放什麼。 */
  title: string;
  /** 為什麼今天填不出來。 */
  why: string;
  /** 缺的那幾條路,例如 `GET /api/lore/subjects`。空陣列代表缺的不是一條路,
   * 而是一個還沒有人在產生的訊號 —— 那種情況由 `why` 自己講完。 */
  missing: string[];
}) {
  const { t } = useI18n();
  return (
    <div className="lore-nosource" data-testid="lore-no-source">
      <div className="lore-nosource__head">
        <span className="lore-nosource__chip">{t.lore.noSource}</span>
        <span className="lore-nosource__title">{title}</span>
      </div>
      <div className="lore-nosource__body">{why}</div>
      {missing.length > 0 && (
        <div className="lore-nosource__body">
          {t.lore.noSourceMissing}{" "}
          {missing.map((route) => (
            <code className="lore-nosource__route" key={route}>
              {route}
            </code>
          ))}
        </div>
      )}
      <div className="lore-nosource__foot">{t.lore.noSourceNoNumber}</div>
      <div className="lore-nosource__foot">{t.lore.routesToday}</div>
    </div>
  );
}
