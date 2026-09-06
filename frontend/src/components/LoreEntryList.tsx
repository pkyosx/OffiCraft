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
import { isHttpStatus, serverMessageOf } from "../api/errors";
import { useHashRoute } from "../lib/hashRoute";
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
  anchorId,
}: {
  subject: string;
  entries: LoreEntrySummaryView[];
  forceOpen: boolean;
  /** The `#lore/entry/<id>` target, or "". A group holding it is force-opened
   * by the caller — see LoreEntryList's `anchorSubjects`. */
  anchorId: string;
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
          <div
            key={`${subject}:${e.entryId}`}
            className={
              e.entryId === anchorId ? "lore-list__anchored" : undefined
            }
            data-testid={
              e.entryId === anchorId ? "lore-anchored-entry" : undefined
            }
          >
            <LoreEntryCard entry={e} />
          </div>
        ))}
    </section>
  );
}

export function LoreEntryList() {
  const { t } = useI18n();
  const [route, setRoute] = useHashRoute();
  const anchorId = (route.page === "lore" && route.loreEntryId) || "";
  const [entries, setEntries] = useState<LoreEntrySummaryView[] | null>(null);
  const [total, setTotal] = useState(0);
  const [truncated, setTruncated] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState("");

  // ── 錨點 (#lore/entry/<id>) ────────────────────────────────────────────────
  //
  // 🔴 THE ANCHORED ENTRY IS FETCHED ON ITS OWN AND MERGED IN — 「Merge, never
  // replace」, copied from useTasks' `#tasks/<id>` anchor because this page has
  // the SAME problem in two flavours the list fetch cannot solve:
  //
  //   · RETIRED. The list rides POST /api/lore/search, whose DAL filters
  //     `status <> 'retired'`. Retirement means the entry is no longer
  //     RETRIEVED — it is still readable BY ID (GetLoreEntry has no status
  //     filter), so a link to one lands here perfectly well and the list simply
  //     never had it.
  //   · PAST THE CAP. The list asks for PAGE_LIMIT (100), which is the server's
  //     maximum (loreSearchLimitMax); an entry outside that page is missing for
  //     a reason that has nothing to do with the entry.
  //
  // Without the补抓, both of those would render as 「查無此條目」 — a false
  // statement about an entry that is sitting in the database.
  //
  // 🔴 AND THE THREE OUTCOMES ARE THREE DIFFERENT SENTENCES (owner ruling
  // rc-428906235337, 2026-09-05, given for the task page and applied here
  // verbatim with 任務→傳承): FOUND ⇒ show it; 404 ⇒ the anchor STAYS and the
  // page says 「沒有符合篩選條件的條目」 with 清除定位 as the exit; ANY OTHER
  // failure (500 / offline) ⇒ `anchorFailed`, show the load error and suppress
  // BOTH empty states — 「沒問出口的問題不得給答案」.
  //
  // 🔴 THE ANCHOR NEVER STRIPS ITS OWN HASH. A route that healed itself would
  // make a 404 flash and vanish, leaving a normal list and no explanation —
  // which is the silent self-heal the task page used to do and the owner
  // overturned.
  const [anchor, setAnchor] = useState<{
    id: string;
    entry: LoreEntrySummaryView | null;
    failed: boolean;
  }>({ id: "", entry: null, failed: false });

  useEffect(() => {
    if (anchorId === "") {
      setAnchor({ id: "", entry: null, failed: false });
      return;
    }
    let alive = true;
    api
      .getLoreEntry(anchorId)
      .then((d) => {
        // ⚠️ THIS READ IS JOURNALLED, AND THAT IS CORRECT HERE. GET
        // /api/lore/entries/{id} files a recall row. Somebody following a link
        // to an entry IS reading it, so the row is true. What must NEVER go
        // through this route is a mere EXISTENCE PROBE — asking 「does this id
        // resolve?」 to pick a message — because that files a row saying an
        // entry was used when nobody read it, into the table the governance
        // side uses to decide what to retire. The difference is INTENT, not the
        // route: the lore activity panel therefore resolves its headings from
        // /api/members/{id}/lore-activity, which reads and writes nothing.
        if (!alive) return;
        setAnchor({
          id: anchorId,
          entry: {
            entryId: d.entryId,
            heading: d.heading,
            subjects: [...d.subjects],
          },
          failed: false,
        });
      })
      .catch((e) => {
        console.warn("LoreEntryList: anchor entry fetch failed", e);
        // A 404 is an ANSWER (「沒有這一條」); anything else is the ABSENCE of
        // one, and the page says different things about them.
        const notFound = isHttpStatus(e, 404);
        if (alive) setAnchor({ id: anchorId, entry: null, failed: !notFound });
      });
    return () => {
      alive = false;
    };
  }, [anchorId]);

  // Only ever true once the fetch has SETTLED on this id: while it is pending
  // the page must say nothing at all about why the entry is missing.
  const anchorPending = anchorId !== "" && anchor.id !== anchorId;
  const anchorFailed = !anchorPending && anchorId !== "" && anchor.failed;

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
  // 🔴 這裡本來還有一排「N 條沒有證偽條件、也沒有實例」的品質訊號,以及一顆
  // 「只看這些」的過濾器。它們整個拿掉了,不是漏掉:owner 2026-09-03 裁定
  // rc-1e32c690018d 把 degraded 這個概念**整個移除**(「第 1 格的硬擋就夠了,
  // 不要第二層」),線上已經沒有這個欄位可讀。留一顆從別的欄位重算的近似品質
  // 訊號會更糟 —— 它跟原本那個伺服器算的長得一模一樣,而它是猜的。
  //
  // ⚠️ 一起消失的是 owner 2026-09-02「lore 的品質優於數量」在這一頁的唯一表示。
  // 那句話沒有被推翻,只是今天站上沒有任何一個欄位撐得起它。缺口留在票上。
  //
  // 篩選比對的是標題與對象 —— 搜尋回應只帶得回這些,其他格要展開那一條才讀得
  // 到,拿沒有的東西去篩會篩掉本來該中的。
  // Merge, never replace: when the loaded page ALREADY has the anchored entry
  // the list row is the one to keep (same shape, and it is what the grouping
  // was built from); the fetched copy only ever fills a gap.
  const merged = useMemo(() => {
    const base = entries ?? [];
    const a = anchor.entry;
    if (a === null || base.some((e) => e.entryId === a.entryId)) return base;
    return [...base, a];
  }, [entries, anchor.entry]);

  const shown = useMemo(
    () =>
      // 🔴 THE ANCHOR OVERRIDES THE FILTER ENTIRELY — the same rule the task
      // page's `matches()` follows: `#lore/entry/<id>` is an explicit 「show me
      // THIS one」, the exact opposite of narrowing a list, so a live 篩選 box
      // must not be able to hide the thing the link was for.
      anchorId !== ""
        ? merged.filter((e) => e.entryId === anchorId)
        : merged.filter(
        (e) =>
          needle === "" ||
          // 🔴 篩的是標題，不是內容 —— 清單這一層拿到的就是標題（深度②），
          // 內容要點開那一條才讀得到。拿沒有的東西去篩會篩掉本來該中的。
          e.heading.toLowerCase().includes(needle) ||
          e.subjects.some((s) => s.toLowerCase().includes(needle))
      ),
    [merged, needle, anchorId]
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

  // 🔴 THE THREE ANCHOR OUTCOMES, IN THE ORDER THEY MUST BE DECIDED
  // (owner ruling rc-428906235337, given for the task page, applied verbatim
  // here with 任務→傳承).
  //
  // ① OTHER FAILURE (500 / offline) — the load error, and it SUPPRESSES both
  //    empty states below. 「沒問出口的問題不得給答案」: we do not know whether
  //    the entry exists, so we must not offer 清除定位 as if the answer were
  //    「it does not」.
  if (error !== null || anchorFailed) {
    return (
      <div className="lore-subjects__error" data-testid="lore-list-error">
        {t.lore.listFailed} {error ?? t.lore.anchorLoadFailed}
      </div>
    );
  }
  // ② STILL IN FLIGHT — say nothing about why anything is missing yet.
  if (entries === null || anchorPending) {
    return (
      <div className="lore__note" data-testid="lore-list-loading">
        {t.lore.listLoading}
      </div>
    );
  }
  // 「站上一條都沒有」 is a claim about the whole store and only the unanchored
  // list can support it: with an anchor live the list is narrowed to one id, so
  // an empty result there means 「找不到那一條」 and is handled below.
  if (anchorId === "" && entries.length === 0) {
    return <div className="lore__note">{t.lore.listEmpty}</div>;
  }

  return (
    <div className="lore-list">
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

      {/* ③ 404 — the anchor STAYS (it is not stripped) and the page says so in
          words, with 清除定位 as the exit. This is the one the owner overturned
          the task page's silent self-heal for: a hash that healed itself would
          leave a normal list and no explanation, and the reader would conclude
          they had seen something they had not. */}
      {anchorId !== "" && shown.length === 0 && (
        <div className="lore__note" data-testid="lore-anchor-missing">
          {t.lore.anchorNotFound(anchorId)}{" "}
          <button
            type="button"
            className="lore-list__clear-anchor"
            data-testid="lore-clear-anchor"
            onClick={() => setRoute({ page: "lore" })}
          >
            {t.lore.anchorClear}
          </button>
        </div>
      )}

      {needle !== "" && anchorId === "" && shown.length === 0 && (
        <div className="lore__note">{t.lore.listFilterNoHit}</div>
      )}

      {groups.map(([subject, list]) => (
        <SubjectGroup
          subject={subject}
          entries={list}
          // 篩選中時全部攤開:一個篩完還要自己一群群點開的清單,等於沒篩。
          // 🔴 定位時也全部攤開 —— 而且是「含這一條的每一群」而不是第一群。
          // 一條掛在 N 個對象底下就在 N 個群裡各出現一次(那是刻意的,見上面
          // groups 的註解),只展開其中一群等於替使用者挑了一個他沒挑的對象。
          // 這裡 `shown` 已經被錨點窄化成那一條,所以「全部攤開」就正好是
          // 「含它的每一群」。
          forceOpen={needle !== "" || anchorId !== ""}
          anchorId={anchorId}
          key={subject}
        />
      ))}
    </div>
  );
}
