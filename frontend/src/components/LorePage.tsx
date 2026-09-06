// LorePage — 座艙第六個分頁「傳承」(T-33)。
//
// 這一頁只回答使用者的兩個問題,順序就是它們的優先權:
//   1. 有哪些**在等我審核**? —— 要能當場按下核可或合併。
//   2. **有哪些記憶**? —— 要能搜、能打開看原文與改寫歷史。
//
// 🔴 owner 2026-09-02 逐字:「這是給使用者的頁面 不是你拿來跟我對 spec 的
// design doc」。上一版把「這一格缺 GET /api/lore/xxx」印在畫面上,那是工程筆記
// 貼在使用者臉上 —— 全部拿掉了。同一輪砍掉的還有「概覽」與「健康」兩個子分頁:
// 它們的每一格都要靠站上今天不存在的訊號(撈取次數、回饋、侵蝕量),沒有數字就
// 等於沒有內容,而owner 明說看不出它們要做什麼。缺口留在票上,不留在他的介面裡。
//
// 不畫假數字這條沒有變,只是換了做法:數不出來的東西現在是**不出現**,而不是
// 出現成一個講著缺哪條路的框。

import { useI18n } from "../i18n";
import { LorePendingSection } from "./LorePendingSection";
import { LoreEntryList } from "./LoreEntryList";
import "./lore.css";

export function LorePage() {
  const { t } = useI18n();
  return (
    <div className="lore">
      <section className="lore__section">
        <h2 className="lore__section-title">{t.lore.pendingTitle}</h2>
        <LorePendingSection />
      </section>

      <section className="lore__section">
        <h2 className="lore__section-title">{t.lore.entriesTitle}</h2>
        <LoreEntryList />
      </section>
    </div>
  );
}
