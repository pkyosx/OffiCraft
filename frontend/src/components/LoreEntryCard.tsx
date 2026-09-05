// LoreEntryCard — 一條傳承記錄:摺起來是搜尋結果的一列,展開是設計稿裡的條目詳情
// (五格 → 原文 → 版本時間軸 → 中繼資料 → 動作)。
//
// 展開才去讀 GET /api/lore/entries/{id}:搜尋回應本身不帶第 3、4、5 格、不帶原文、
// 不帶版本目錄,拿摘要去湊一份詳情等於把「沒讀到」畫成「讀到了」。
//
// 五格裡選填的那幾格空著的時候照樣把欄位名印出來(fieldEmpty),因為「空著」跟
// 「沒有這一節」必須長得不一樣 —— 一條連「之前發生過什麼問題」都空的記錄,要一眼
// 看得出它還沒有任何真的發生過的事撐著。
//
// 🔴 第 5 格(事件)同一條規則,而且更嚴格:人／地／物**空著是合法的**,所以那一格
// 印的是欄位名加上一個明說「沒有記下」的標記,**不是「未知」**。「查不出是誰」跟
// 「還沒有人去查」如果長得一樣,這一格就沒有在記錄任何東西了。一條事件都沒有的
// 條目也照樣印出事件這一節並說出來 —— 跟後端 loreRevisionBody 永遠印 `events:`
// 是同一個理由:沒有事件跟事件被某次改寫弄丟,不能長得一樣。
//
// 摺起來那一列上沒有分層、信任類別、tier note、活動徽章 —— 而它們現在是**線上
// 根本不存在**,不是這一層選擇不畫。owner 2026-09-05 裁掉了「活動」這個檢索軸
// (「只有subject 沒有 action因為後者太多可能性」),T1/T2 分層與信任類別都是從它
// 推導出來的,一起沒了。留下的是他認得的東西:標題、對象、來源、第 1 格。
//
// 🔴 「證偽條件與實例都空」那個 degraded 徽章不見了,不是漏掉:owner 2026-09-03
// 裁定 rc-1e32c690018d 把整個概念拿掉了(「第 1 格的硬擋就夠了,不要第二層」)。
// 線上已經沒有這個欄位可讀。

import { useState } from "react";
import { useI18n } from "../i18n";
import { api } from "../api";
import { serverMessageOf } from "../api/errors";
import type {
  LoreEntryDetailView,
  LoreEntrySummaryView,
  LoreEventView,
  LoreRevisionRowView,
} from "../types";
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

/** 事件的一格:人／地／物。空著就明說「沒有記下」——**不是「未知」**,兩者是不同
 * 的事實,而只有其中一個是這一格真的知道的。 */
function EventCell({ name, value }: { name: string; value: string }) {
  const { t } = useI18n();
  const empty = value.trim() === "";
  return (
    <span className="lore-event__cell">
      <span className="lore-event__cell-name">{name}</span>
      <span
        className={`lore-event__cell-value${
          empty ? " lore-event__cell-value--blank" : ""
        }`}
      >
        {empty ? t.lore.eventBlank : value}
      </span>
    </span>
  );
}

/** 一筆事件:時／事 一定有,人／地／物 三格永遠印出來(空的也印)。 */
function EventRow({ ev }: { ev: LoreEventView }) {
  const { t } = useI18n();
  return (
    <div className="lore-event" data-testid="lore-event">
      <div className="lore-event__what">{ev.what}</div>
      <div className="lore-event__meta">
        <span className="lore-event__cell">
          <span className="lore-event__cell-name">{t.lore.eventWhen}</span>
          <span className="lore-event__cell-value">
            {formatHappenedTs(ev.happenedTs)}
          </span>
        </span>
        <EventCell name={t.lore.eventActor} value={ev.actor} />
        <EventCell name={t.lore.eventPlace} value={ev.place} />
        <EventCell name={t.lore.eventObject} value={ev.object} />
      </div>
    </div>
  );
}

/** `happened_ts` 是**事情發生的時間**,不是寫下的時間。秒為單位的 epoch。
 * 轉不出來的時候印原始數字而不是印一個像樣的假日期:一個看起來正常的錯日期,
 * 比一個看得出沒轉成功的數字危險得多。 */
function formatHappenedTs(ts: number): string {
  const d = new Date(ts * 1000);
  if (!Number.isFinite(ts) || Number.isNaN(d.getTime())) return String(ts);
  return d.toISOString().replace("T", " ").slice(0, 16) + "Z";
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
        {/* 第 1 格兼任標題。它在寫入路徑上是硬性必填(擋在 PutLoreEntry 這個
            原始 upsert 縫上),所以這裡沒有「這條沒有名字」的退路 —— 那個退路
            存在的前提(label 選填)已經沒有了。長度也沒有上限。 */}
        {/* 🔴 標題坐在標題的位置，而它以前坐的是第 1 格。v8 把兩者分開之後，
            這一行就是 owner 說的那個「決定要不要看內容」的指標
            （2026-09-05:「title 應該就是 agent 透過 target 會看到的列表
            因為這會決定他們要不要看內容」）。 */}
        <div className="lore-entry__title">{entry.heading}</div>
        <div className="lore-entry__axes">
          {entry.subjects.map((s) => (
            <span className="lore__badge lore__badge--subject" key={s}>
              {s}
            </span>
          ))}
          <span>
            {t.lore.entryOriginLabel} {entry.origin}
          </span>
        </div>
        {/* 第 1 格坐在副標的位置：它是這條**為什麼在這份清單裡**的理由（對象），
            不是給人讀的那一行。
            🔴 這裡以前印的是第 2 格「內容」。拿掉它是這一層存在的理由：清單那一
            層倒出整段內容，正是這張票在治的病。內容要點開才讀得到（深度③）。 */}
        <div className="lore-entry__short">{entry.trigger}</div>
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
                {/* 🔴 標題排在最前面，而那不是版面偏好：它是「列出來」那一層唯一
                    被讀到的東西（owner 2026-09-05:「title 應該就是 agent 透過
                    target 會看到的列表 因為這會決定他們要不要看內容」）。畫面上
                    把它排在第 1 格後面，會讓讀的人以為第 1 格才是這條的名字 ——
                    那正是 v8 推翻掉的 v7 說法。 */}
                <Field name={t.lore.fieldHeading} value={detail.heading} />
                <Field name={t.lore.fieldTrigger} value={detail.trigger} />
                <Field name={t.lore.fieldContent} value={detail.content} />
                <Field
                  name={t.lore.fieldRetireWhen}
                  value={detail.retireWhen}
                />
                <Field name={t.lore.fieldImpact} value={detail.impact} />
                {/* 星等：owner 2026-09-05「評分也改了不用 用星等取代 因為 impact
                    本就是重要性」⇒ 這一格就是這條條目的重要性，不是註腳。
                    🔴 0 印成「還沒判」而不是 0 顆星：0 與 1（沒弄壞任何東西）
                    必須分得開，否則沒有人查得出誰漏填。 */}
                <Field
                  name={t.lore.fieldImpactStars}
                  value={
                    detail.impactStars === 0
                      ? ""
                      : "★".repeat(detail.impactStars)
                  }
                />

                {/* 第 5 格。一筆都沒有的時候這一節照樣在,並且說出來 —— 跟後端
                    永遠渲染 `events:` 是同一條規則。 */}
                <div className="lore-entry__field">
                  <div className="lore-entry__field-name">
                    {t.lore.fieldEvents}
                  </div>
                  {detail.events.length === 0 ? (
                    <div className="lore-entry__field-value lore-entry__field-value--empty">
                      {t.lore.eventsEmpty}
                    </div>
                  ) : (
                    <div className="lore-events" data-testid="lore-events">
                      {detail.events.map((ev, i) => (
                        <EventRow ev={ev} key={`${ev.happenedTs}:${i}`} />
                      ))}
                    </div>
                  )}
                </div>

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
            </>
          )}
        </div>
      )}
    </div>
  );
}
