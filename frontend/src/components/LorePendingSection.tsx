// LorePendingSection — 「等你審核」(T-33)。
//
// owner 2026-09-02 逐字(`rc-754dcbcdb4f5`):
//   「agent 做完功課以後給建議並提出我一眼就可以判斷的資訊,我還是做最後的裁
//    決,lore 的品質優於數量」
//
// 這一塊就是那句話變成的畫面,三件事各自對應:
//   ① **功課**:每一列的依據是伺服器算的,不是這裡湊的。
//   ② **一眼可判斷**:名字、底下有幾條、底下第一條在講什麼、跟誰像、像在哪裡
//      —— 全部攤在同一列上,不用點進去第二層。
//   ③ **他做最後裁決**:沒有自動核可。他 `rc-139a5ab99a19` 逐字裁過「待審,我
//      跟 mira 有 admin 權限的才行」,我問過要不要放寬,他選了不放寬。
//
// 🔴 那句「給建議」曾經被做成一格伺服器算的 `suggestion`,owner 2026-09-05 把它
// 整組裁掉了:「ai 會笨到產生大小寫不一樣的對象嗎」—— 那個機械規則最強的訊號是
// 兩個名字只差大小寫／全半形／底線連字號,而寫的人根本不犯這種錯。他要的替代品
// 是「請 AI 判一輪、人可以同意或回 comment 讓它重判」,那是**另一張票**。在它落
// 地以前這一塊只給證據、不給結論 —— 留一個舊規則會比留白更糟,因為畫面上看不出
// 那是誰的判斷。
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
// 🔴 這三件都是**多給資訊**,不是多給出口:出口還是那兩個(核可、合併),裁決
// 還是他的。(原句還有一截「建議還是伺服器算的」—— 2026-09-05 之後不再成立,
// 見上面那條。能不能按還是由後端的 owner/admin 閘門決定,那一格一個字都沒動。)
//
// ── round 4(owner 2026-09-05 逐字:「改成單一入口:只留一顆合併鈕,按了列出
// 候選讓你挑,再確認」)──────────────────────────────────────────────────
// 上一輪把合併鈕掛在 `similar` 的**每一個**候選上。那是一排一按就送出的不可逆
// 動作,而 `similar` 裡有 `prefix` / `substring` 這種弱匹配 —— 一列上並排著五
// 顆長得一樣的鈕,其中四顆按下去是錯的,而且救不回來。現在是三步:
//   ① 一列**一顆**合併鈕(沒有候選就沒有這一顆)。
//   ② 按了列出候選,每一個候選旁邊印**它為什麼被判為相似**;沒挑就送不出去。
//   ③ 再一個確認步驟,而且 🔴 確認畫面明寫**這個動作無法還原** —— 後端的合併
//      是單向的,沒有 unmerge 路由,這是這一輪存在的理由。
// 出口的數量沒有變多(核可、合併,還是兩個),變的是走到不可逆那一步要幾下。

import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { serverMessageOf } from "../api/errors";
import { ConfirmModal } from "./ConfirmModal";
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
  // 合併的三步:idle ⇒ 挑一個候選 ⇒ 確認。核可不走這條路,它一步就到底,因為
  // 核可是可以回頭的(名字還在,底下的記憶還是它的),而合併不是。
  const [step, setStep] = useState<"idle" | "pick" | "confirm">("idle");
  const [pickedId, setPickedId] = useState("");
  const picked = row.similar.find((s) => s.entityId === pickedId) ?? null;

  function closeMerge() {
    setStep("idle");
    setPickedId("");
    setError(null);
  }

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

      {/* 🔴 這裡原本還有一塊 `lore-pending__suggestion`,印伺服器算出來的
          「建議:核可 / 建議:併進 X / 沒有明確的建議」。owner 2026-09-05 裁掉
          了整組建議 ——「ai 會笨到產生大小寫不一樣的對象嗎」—— 所以畫面上現在
          只剩證據,沒有結論。之後會換成 AI 判一輪、人可以同意或回 comment 讓它
          重判,那是另一張票;在它落地以前這一列刻意留白,而不是留一個舊規則。 */}
      {/* 「像誰」這一排還是**證據**,不再是出口:每一個候選旁邊各一顆合併鈕的
          做法在 owner 2026-09-05 被裁掉了(逐字:「改成單一入口:只留一顆合併
          鈕,按了列出候選讓你挑,再確認」)。一排鈕代表一排一按就送出的不可逆
          動作,而其中有些候選只是 prefix / substring 的弱匹配。 */}
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
        {/* 🔴 一列一顆合併鈕。沒有候選就沒有這一顆 —— 一顆按下去只會告訴你
            「沒得挑」的鈕,是一個假的出口。 */}
        {row.similar.length > 0 && (
          <button
            type="button"
            className="lore-pending__btn"
            data-testid="lore-pending-merge-start"
            disabled={busy || step !== "idle"}
            onClick={() => {
              setError(null);
              setStep("pick");
            }}
          >
            {t.lore.pendingMergeStart(row.similar.length)}
          </button>
        )}
      </div>

      {/* ── 第二步:挑一個候選 ────────────────────────────────────────────
          每一個候選都印**它為什麼被判為相似**。這不是裝飾:`same_normalized`
          幾乎一定是同一個東西,而 `prefix` / `substring` 常常是兩個真的不同的
          名字,不把理由攤出來,使用者就是在猜。 */}
      {step === "pick" && (
        <div
          className="lore-pending__picker"
          data-testid="lore-pending-merge-picker"
        >
          <div className="lore__note">
            {t.lore.pendingMergePickLead(row.canonical)}
          </div>
          <ul className="lore-pending__candidates">
            {row.similar.map((s) => (
              <li key={s.entityId}>
                <label
                  className="lore-pending__candidate"
                  data-testid="lore-pending-merge-candidate"
                >
                  <input
                    type="radio"
                    name={`lore-merge-${row.entityId}`}
                    value={s.entityId}
                    checked={pickedId === s.entityId}
                    onChange={() => setPickedId(s.entityId)}
                  />
                  <span className="lore-pending__name">{s.canonical}</span>
                  {/* 🔴 理由跟候選同一列,不是 tooltip:要比較的就是它。 */}
                  <span className="lore-pending__candidate-reason">
                    {reasonText(t, s.reason)}
                  </span>
                </label>
              </li>
            ))}
          </ul>
          <div className="lore-pending__actions">
            <button
              type="button"
              className="lore-pending__btn"
              data-testid="lore-pending-merge-next"
              // 🔴 沒挑就送不出去。鈕上的字自己說得出為什麼是死的。
              disabled={picked === null}
              onClick={() => setStep("confirm")}
            >
              {t.lore.pendingMergePickSubmit(picked?.canonical ?? "")}
            </button>
            <button
              type="button"
              className="lore-pending__btn"
              data-testid="lore-pending-merge-cancel"
              onClick={closeMerge}
            >
              {t.common.cancel}
            </button>
          </div>
        </div>
      )}

      {/* ── 第三步:確認,而且明寫這一步無法還原 ──────────────────────────
          🔴 這一步存在的唯一理由。後端的合併是單向的:沒有 unmerge 路由,按錯
          了沒有任何一條路把那個名字拿回來。所以確認畫面不只問「確定嗎」,它把
          「無法還原」印在正文裡。 */}
      {step === "confirm" && picked !== null && (
        <ConfirmModal
          testId="lore-pending-merge-confirm"
          confirmTestId="lore-pending-merge-confirm-ok"
          danger
          busy={busy}
          cancelLabel={t.common.cancel}
          confirmLabel={t.lore.pendingMerge(picked.canonical)}
          body={
            <div data-testid="lore-pending-merge-confirm-body">
              {t.lore.pendingMergeConfirmBody(row.canonical, picked.canonical)}
            </div>
          }
          error={error}
          // 取消退回挑的那一步,不是退回原地:他已經挑過了,把那個選擇丟掉等於
          // 罰他再挑一次。這一條路上一次 API 都沒有打。
          onCancel={() => {
            setError(null);
            setStep("pick");
          }}
          onConfirm={() =>
            run(() => api.mergeLoreEntity(row.entityId, picked.entityId))
          }
        />
      )}

      {/* 確認框自己會印失敗;這一行是核可那條路的失敗出口。 */}
      {error !== null && step !== "confirm" && (
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
