// LoreEntryList — 「有哪些記憶」:一進來就把全部列出來,照對象分群。
//
// 🔴 這裡本來是一個搜尋表單(對象鍵欄位 + 關鍵字欄位 + 一顆「搜尋」),owner
// 2026-09-02 兩句話否掉了它:「弄個 search 是最好的方式嗎? 感覺殺雞用牛刀」與
// 「我無法一眼看出有哪些對象 我要怎麼搜尋」。
// 他說中的是這一頁的前提錯了 —— 一個要你先想出關鍵字才給你看東西的頁面,在
// 「我根本還不知道有什麼」的時候是死路。站上今天是幾十條的量級,直接列出來就好。
//
// 對象名單也是這樣長出來的:分群的標題就是有哪些對象。站上沒有列對象目錄的
// 路,但那不表示畫面要空著 —— 已經拿到的條目自己就帶著它們的對象。這裡只宣稱
// 「這批記憶落在這些對象上」,沒有宣稱「站上的對象只有這些」。
//
// 篩選框只在清單長到一頁看不完的時候才出現,而且是即時篩(不是打完按送出)。

import { useEffect, useMemo, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { serverMessageOf } from "../api/errors";
import type { LoreEntrySummaryView } from "../types";
import { LoreEntryCard } from "./LoreEntryCard";
import "./lore.css";

/** 超過這個條數才長出篩選框。低於它,篩選框只是多一個要按的東西。 */
const FILTER_THRESHOLD = 12;
/** 一次拿多少。站上限 100;拿滿之後如果還有更多,畫面要說出來,而不是讓人以為
 * 這就是全部。 */
const PAGE_LIMIT = 100;

/** 沒有對象的條目也要有地方站,不能因為分不了群就從清單上消失。 */
const NO_SUBJECT = " none";

/** 一個對象一群,預設收合 —— 收合狀態下這一排標題本身就是「有哪些對象」。
 * 條目最終是「千」的量級而對象是「幾十」的量級,所以能一眼掃完的是對象,不是
 * 條目;把條目全部攤開會把唯一那份看得完的清單淹掉。 */
function SubjectGroup({
  subject,
  entries,
  forceOpen,
}: {
  subject: string;
  entries: LoreEntrySummaryView[];
  forceOpen: boolean;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const shown = open || forceOpen;
  return (
    <section className="lore-list__group">
      <button
        type="button"
        className="lore-list__group-title"
        aria-expanded={shown}
        onClick={() => setOpen(!open)}
      >
        <span className="lore-list__group-name">
          {subject === NO_SUBJECT ? t.lore.listNoSubject : subject}
        </span>
        {/* 這個數字是這一批載到的條目裡屬於這個對象的筆數 —— 全部載得完的時候
            它就是真的。載不完那天要改成後端給的計數,不能讓它默默變成猜的。 */}
        <span className="lore-list__group-count">{entries.length}</span>
        <span className="lore-list__group-toggle">
          {shown ? t.lore.listGroupCollapse : t.lore.listGroupExpand}
        </span>
      </button>
      {shown &&
        entries.map((e) => (
          <LoreEntryCard entry={e} key={`${subject}:${e.entryId}`} />
        ))}
    </section>
  );
}

export function LoreEntryList() {
  const { t } = useI18n();
  const [entries, setEntries] = useState<LoreEntrySummaryView[] | null>(null);
  const [total, setTotal] = useState(0);
  const [truncated, setTruncated] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState("");
  const [onlyFlagged, setOnlyFlagged] = useState(false);

  useEffect(() => {
    let alive = true;
    api
      .searchLore({ limit: PAGE_LIMIT })
      .then((r) => {
        if (!alive) return;
        setEntries(r.entries);
        setTotal(r.total);
        setTruncated(r.truncated);
      })
      .catch((e) => {
        if (alive) setError(serverMessageOf(e));
      });
    return () => {
      alive = false;
    };
  }, []);

  const needle = filter.trim().toLowerCase();
  // 「證偽條件與實例都沒有」的條目數。owner 2026-09-02:「lore 的品質優於數量」
  // ⇒ 顯眼的位置給品質訊號,總條數退成小字。這個數只從已載入的條目算,所以拿滿
  // 上限的那一刻它就不再是全站的數 —— 那時 truncated 那句話跟它同時出現。
  const flagged = useMemo(
    () => (entries ?? []).filter((e) => e.degraded).length,
    [entries]
  );
  const shown = useMemo(
    () =>
      (entries ?? []).filter(
        (e) =>
          (!onlyFlagged || e.degraded) &&
          (needle === "" ||
          e.label.toLowerCase().includes(needle) ||
          e.short.toLowerCase().includes(needle) ||
            e.symptoms.toLowerCase().includes(needle) ||
            e.subjects.some((s) => s.toLowerCase().includes(needle)))
      ),
    [entries, needle, onlyFlagged]
  );

  // 一條可以掛在好幾個對象下,所以它會在好幾群裡各出現一次 —— 那是實話,不是
  // 重複:把它只放進第一個對象會讓另一個對象看起來比實際上空。
  const groups = useMemo(() => {
    const bySubject = new Map<string, LoreEntrySummaryView[]>();
    for (const e of shown) {
      const keys = e.subjects.length > 0 ? e.subjects : [NO_SUBJECT];
      for (const k of keys) {
        const list = bySubject.get(k);
        if (list) list.push(e);
        else bySubject.set(k, [e]);
      }
    }
    return [...bySubject.entries()].sort((a, b) =>
      a[0] === NO_SUBJECT
        ? 1
        : b[0] === NO_SUBJECT
          ? -1
          : a[0].localeCompare(b[0])
    );
  }, [shown]);

  if (error !== null) {
    return (
      <div className="lore-subjects__error">
        {t.lore.listFailed} {error}
      </div>
    );
  }
  if (entries === null) {
    return <div className="lore__note">{t.lore.listLoading}</div>;
  }
  if (entries.length === 0) {
    return <div className="lore__note">{t.lore.listEmpty}</div>;
  }

  return (
    <div className="lore-list">
      {/* 品質訊號在最上面,總條數在它下面且是小字 —— owner:「lore 的品質優於
          數量」。一份誇自己有幾百條的清單,對「這些東西幫得上忙嗎」沒有回答。 */}
      {flagged > 0 && (
        <div className="lore-list__quality" data-testid="lore-quality">
          <span className="lore-list__quality-count">
            {t.lore.qualityFlagged(flagged)}
          </span>
          <span className="lore__note">{t.lore.qualityWhy}</span>
          <button
            type="button"
            className="lore-list__quality-toggle"
            aria-pressed={onlyFlagged}
            onClick={() => setOnlyFlagged(!onlyFlagged)}
          >
            {onlyFlagged ? t.lore.qualityShowAll : t.lore.qualityShowOnly}
          </button>
        </div>
      )}

      <div className="lore-list__head">
        <span className="lore-list__count" data-testid="lore-entry-total">
          {t.lore.listCount(total)}
        </span>
        {/* 拿滿上限時說出來。不說的話,這一頁就是在把「我拿到的」講成「全部」。 */}
        {truncated && (
          <span className="lore__note">{t.lore.listTruncated(PAGE_LIMIT)}</span>
        )}
        {entries.length > FILTER_THRESHOLD && (
          <input
            className="lore-list__filter"
            type="search"
            value={filter}
            placeholder={t.lore.listFilterPlaceholder}
            aria-label={t.lore.listFilterPlaceholder}
            onChange={(ev) => setFilter(ev.target.value)}
          />
        )}
      </div>

      {needle !== "" && shown.length === 0 && (
        <div className="lore__note">{t.lore.listFilterNoHit}</div>
      )}

      {groups.map(([subject, list]) => (
        <SubjectGroup
          subject={subject}
          entries={list}
          // 篩選中時全部攤開:一個篩完還要自己一群群點開的清單,等於沒篩。
          forceOpen={needle !== "" || onlyFlagged}
          key={subject}
        />
      ))}
    </div>
  );
}
