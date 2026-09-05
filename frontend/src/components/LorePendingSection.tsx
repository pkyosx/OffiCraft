// LorePendingSection — 「等你審核」(T-33)。
//
// owner 2026-09-02 逐字(`rc-754dcbcdb4f5`):
//   「agent 做完功課以後給建議並提出我一眼就可以判斷的資訊,我還是做最後的裁
//    決,lore 的品質優於數量」
//
// 這一塊就是那句話變成的畫面,三件事各自對應:
//   ① **功課**:每一列的建議與依據是伺服器算的,不是這裡湊的。
//   ② **一眼可判斷**:名字、底下有幾條、底下第一條在講什麼、跟誰像、像在哪裡
//      —— 全部攤在同一列上,不用點進去第二層。
//   ③ **他做最後裁決**:沒有自動核可。他 `rc-139a5ab99a19` 逐字裁過「待審,我
//      跟 mira 有 admin 權限的才行」,我問過要不要放寬,他選了不放寬。
//
// 🔴 `suggestion` 是空字串就**照實留白**,畫面上不補一個。硬給的建議跟算得出來
// 的長得一模一樣 —— 那正是這張票在治的病。
// 🔴 沒有「駁回」。那個出口 owner 從來沒有裁定過,補一個等於替他決定。
// 🔴 沒有事情等他的時候整塊不出現(回 null),因為「常態就有一排等你按」本身就
// 是設計失敗。
//
// ── round 3(owner 2026-09-04 逐字:「為什麼核可的可見內容這麼少 我根本無從審
// 核起」)───────────────────────────────────────────────────────────────────
// 上面那句「一眼可判斷」原本只兌現了一半:一列上有名字、有幾條、有第一條的前
// 120 字。他要做的判斷是**「這是新對象還是既有對象的錯字」**,而那一列答不了:
//   ① 「底下 0 條」有**兩種**成因(從來沒用過 / 曾經有但都退役了),處置完全相
//      反,而畫面上長得一模一樣 ⇒ 現在兩種各說各的話。
//   ② 「誰鑄出這個名字」是判斷錯字最有用的線索,而它在表裡躺了兩個 migration
//      沒被端出來 ⇒ 現在印在名字底下。
//   ③ 底下不只一條的時候,只看得到第一條的前 120 字 ⇒ 現在**每一條**的第 1 格
//      都列出來,而第 1 格本來就是那一條的標題。
// 🔴 這三件都是**多給資訊**,不是多給出口:按鈕還是那兩顆,建議還是伺服器算
// 的,裁決還是他的。

import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { serverMessageOf } from "../api/errors";
import type { LorePendingEntityView } from "../types";
import "./lore.css";

/** 把伺服器給的像法名字翻成人話。認不得的原樣印出來 —— 一個新的像法被翻成
 * 「不明」會讓人以為系統沒算,而它其實算了。 */
function reasonText(t: ReturnType<typeof useI18n>["t"], reason: string) {
  switch (reason) {
    case "same_normalized":
      return t.lore.reasonSameNormalized;
    case "edit_distance_1":
      return t.lore.reasonEditDistance1;
    case "edit_distance_2":
      return t.lore.reasonEditDistance2;
    case "prefix":
      return t.lore.reasonPrefix;
    case "substring":
      return t.lore.reasonSubstring;
    default:
      return reason;
  }
}

/** 條目狀態翻成人話。`active` 不印 —— 一列上每一條都掛「正常」等於沒有訊號,
 * 而 superseded / underspecified 是真的會改變「這個名字底下有什麼」的讀法。
 * 認不得的原樣印出來,理由跟 reasonText 一樣。 */
function entryStatusText(
  t: ReturnType<typeof useI18n>["t"],
  status: string,
): string {
  switch (status) {
    case "active":
      return "";
    case "superseded":
      return t.lore.pendingEntryStatusSuperseded;
    case "underspecified":
      return t.lore.pendingEntryStatusUnderspecified;
    default:
      return status;
  }
}

/** 「底下有幾條」那一句。
 *
 * 🔴 0 分成兩句,而那正是這一輪的第一件事。`entries` 是**現在還讀得到幾條**,
 * `entriesEver` 把退役的也算進去 —— 兩個都 0 ⇒ 這個名字鑄出來就沒被用過(打錯
 * 字的形狀);現在 0 但曾經有 ⇒ 用過,只是都退役了,跟名字對不對無關。舊畫面
 * 兩種都印同一句「底下還沒有記憶」,於是最值得看的那一列跟最不值得看的那一列
 * 長得一模一樣。 */
function entriesText(
  t: ReturnType<typeof useI18n>["t"],
  row: LorePendingEntityView,
): string {
  if (row.entries > 0) {
    const base = t.lore.pendingEntries(row.entries);
    const retired = row.entriesEver - row.entries;
    // 退役的條數不併進主數字(主數字要跟核可後真的服務得到的量對得起來),
    // 但也不藏起來:一個「3 條」底下其實還躺著 5 條退役的,是兩回事。
    return retired > 0 ? `${base} ${t.lore.pendingAlsoRetired(retired)}` : base;
  }
  return row.entriesEver > 0
    ? t.lore.pendingAllRetired(row.entriesEver)
    : t.lore.pendingNeverUsed;
}

function PendingRow({
  row,
  onDone,
}: {
  row: LorePendingEntityView;
  onDone: (entityId: string) => void;
}) {
  const { t } = useI18n();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const target = row.similar.find((s) => s.entityId === row.mergeTarget);

  async function run(action: () => Promise<unknown>) {
    setBusy(true);
    setError(null);
    try {
      await action();
      onDone(row.entityId);
    } catch (e) {
      // 失敗就留在原地並說出原因。把它從清單上拿掉會讓「沒成功」長得像「成功」。
      setError(serverMessageOf(e));
      setBusy(false);
    }
  }

  return (
    <div className="lore-pending__row" data-testid="lore-pending-row">
      <div className="lore-pending__head">
        <span className="lore-pending__name">{row.canonical}</span>
        <span className="lore__note" data-testid="lore-pending-entries">
          {entriesText(t, row)}
        </span>
      </div>

      {/* 誰鑄出這個名字。判斷「是不是打錯字」的時候,名字之後最有用的就是它 ——
          原樣印 actor id,不在這裡翻成顯示名。沒有記錄就照實說沒有記錄,不要
          印一個空白讓人以為系統沒查。 */}
      <div className="lore__note" data-testid="lore-pending-minter">
        {row.createdBy !== ""
          ? t.lore.pendingMintedBy(row.createdBy)
          : t.lore.pendingMintedByUnknown}
      </div>

      {/* 底下第一條在講什麼 —— 這是「一眼可判斷」最重的一格。 */}
      {row.sampleShort !== "" && (
        <div className="lore-pending__sample">{row.sampleShort}</div>
      )}

      {/* 🔴 底下**每一條**,不是只有第一條的前 120 字。第 1 格「什麼時候要記起
          來」本來就兼任標題,所以列第 1 格是最便宜、又真的答得了「這個名字底下
          到底放了什麼」的做法。內容不放這裡:要看內容就打開那一條。 */}
      {row.entryRefs.length > 0 && (
        <div className="lore-pending__entries">
          <span className="lore__note">{t.lore.pendingEntryListLead}</span>
          <ul className="lore-pending__entrylist">
            {row.entryRefs.map((e) => {
              const status = entryStatusText(t, e.status);
              return (
                <li
                  className="lore-pending__entry"
                  key={e.entryId}
                  data-testid="lore-pending-entry"
                >
                  <span className="lore-pending__entry-trigger">
                    {e.trigger}
                  </span>
                  {status !== "" && (
                    <span className="lore-pending__entry-status">{status}</span>
                  )}
                  <span className="lore__note">{e.entryId}</span>
                </li>
              );
            })}
          </ul>
        </div>
      )}

      <div className="lore-pending__suggestion">
        {row.suggestion === "approve" && t.lore.pendingSuggestApprove}
        {row.suggestion === "merge" &&
          t.lore.pendingSuggestMerge(target?.canonical ?? row.mergeTarget)}
        {row.suggestion === "" && (
          <span className="lore-pending__suggestion--none">
            {t.lore.pendingSuggestNone}
          </span>
        )}
      </div>

      {row.similar.length > 0 && (
        <div className="lore__note">
          {t.lore.pendingSimilarLead}{" "}
          {row.similar.map((s) => (
            <span className="lore-pending__similar" key={s.entityId}>
              {s.canonical}（{reasonText(t, s.reason)}）
            </span>
          ))}
        </div>
      )}

      <div className="lore-pending__actions">
        <button
          type="button"
          className="lore-pending__btn"
          disabled={busy}
          onClick={() => run(() => api.approveLoreEntity(row.entityId))}
        >
          {busy ? t.lore.pendingBusy : t.lore.pendingApprove}
        </button>
        {/* 合併按鈕只在伺服器算得出一個目標時出現。沒有目標卻給一顆按鈕,等於要
            他自己去想併到哪 —— 那正是這一塊要替他做掉的功課。 */}
        {row.mergeTarget !== "" && (
          <button
            type="button"
            className="lore-pending__btn"
            disabled={busy}
            onClick={() =>
              run(() => api.mergeLoreEntity(row.entityId, row.mergeTarget))
            }
          >
            {t.lore.pendingMerge(target?.canonical ?? row.mergeTarget)}
          </button>
        )}
      </div>

      {error !== null && (
        <div className="lore-subjects__error">
          {t.lore.pendingActionFailed} {error}
        </div>
      )}
    </div>
  );
}

export function LorePendingSection() {
  const { t } = useI18n();
  const [rows, setRows] = useState<LorePendingEntityView[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    api
      .listPendingLoreEntities()
      .then((r) => {
        if (alive) setRows(r);
      })
      .catch((e) => {
        if (alive) setError(serverMessageOf(e));
      });
    return () => {
      alive = false;
    };
  }, []);

  // 讀不到跟「沒有待審」必須長得不一樣:一個是他沒事要做,另一個是我沒讀到。
  if (error !== null) {
    return (
      <div className="lore-subjects__error">
        {t.lore.pendingFailed} {error}
      </div>
    );
  }
  if (rows === null) {
    return <div className="lore__note">{t.lore.pendingLoading}</div>;
  }
  if (rows.length === 0) {
    return <div className="lore__note">{t.lore.pendingEmpty}</div>;
  }

  return (
    <div className="lore-pending">
      {rows.map((row) => (
        <PendingRow
          row={row}
          key={row.entityId}
          onDone={(id) =>
            setRows((cur) => (cur ?? []).filter((r) => r.entityId !== id))
          }
        />
      ))}
    </div>
  );
}
