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
        <span className="lore__note">
          {row.entries > 0
            ? t.lore.pendingEntries(row.entries)
            : t.lore.pendingNoEntries}
        </span>
      </div>

      {/* 底下第一條在講什麼 —— 這是「一眼可判斷」最重的一格。 */}
      {row.sampleShort !== "" && (
        <div className="lore-pending__sample">{row.sampleShort}</div>
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
