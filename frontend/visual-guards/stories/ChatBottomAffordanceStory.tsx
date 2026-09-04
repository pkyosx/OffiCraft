// CT story for T-48 的兩個底部元件（回到最新箭頭 ①、新訊息預覽列 ②），用的是
// production 的 DOM 形狀與 production 的 office.css —— 這裡要量的東西 jsdom 一
// 個都量不到：絕對定位落在哪、圓不圓、兩行有沒有被裁、換內容時高度會不會跳、
// 預覽列排在回覆橫幅上面還是下面、換 theme 之後顏色有沒有跟著換。
//
// 🔴 元件是 REAL 的（<ChatJumpLatestButton> / <ChatNewMsgPreview>），只有外殼是
// 手寫的：外殼要照 <ChatArea> 的實際巢狀（.chat > .chat__body > .chat__messages，
// 箭頭是 .chat__body 的絕對定位子元素；預覽列是 .chat__composer 的第一個子元素，
// 回覆橫幅緊接在後）。外殼一旦寫錯，量到的是一個不會出貨的版面。
import { useLayoutEffect, useRef, useState } from "react";
import { isLatestRowInView } from "../../src/lib/scrollToLatest";
import { ChatJumpLatestButton } from "../../src/components/ChatJumpLatestButton";
import { ChatNewMsgPreview } from "../../src/components/ChatNewMsgPreview";
import { I18nProvider, useI18n } from "../../src/i18n";
import { LONG_BODY, LONG_WHO } from "./chatBottomAffordanceFixtures";

function Shell({ children }: { children: React.ReactNode }) {
  // .chat 是 height:100% 的 flex column，CT 的掛載容器沒有高度，所以外面補一個。
  //
  // 🔴 兩層底色不是裝飾，是對比度量測的前提。預覽列的底是半透明的
  // （`color-mix(--color-card 60%, transparent)`），箭頭浮在訊息面板上，兩者最後
  // 被畫成什麼顏色取決於**下面疊了什麼**。CT 的掛載點沒有任何底色，量到的會是
  // 瀏覽器的白畫布 —— 一個永遠不會出貨的組合。app shell 實際疊的是
  // `--color-main-bg` over `--color-bg`，這裡照抄那兩層。
  return (
    <I18nProvider>
      <div style={{ height: 600, background: "var(--color-bg)" }}>
        <div
          style={{
            height: "100%",
            display: "flex",
            background: "var(--color-main-bg)",
          }}
        >
          {children}
        </div>
      </div>
    </I18nProvider>
  );
}

/**
 * 完整外殼：訊息串 + 底部兩個元件 + 回覆橫幅，用 props 選要畫哪些。
 * 兩個元件同時 `true` 只有測試會這樣要 —— production 的互斥由
 * `lib/chatBottomAffordance` 保證（jsdom 已釘），這裡是為了在同一次量測裡
 * 比較兩者的位置關係時能一起看到。
 */
export function ChatBottomAffordanceStory({
  arrow = true,
  preview = false,
  banner = false,
  who = LONG_WHO,
  body = LONG_BODY,
}: {
  arrow?: boolean;
  preview?: boolean;
  banner?: boolean;
  who?: string;
  body?: string;
} = {}) {
  return (
    <Shell>
      <div className="chat" style={{ flex: 1, minWidth: 0 }}>
        <div className="chat__body">
          <div className="chat__messages" data-testid="chat-messages">
            {Array.from({ length: 12 }, (_, i) => (
              <div className="chat__msg" data-msg-id={`c-${i}`} key={i}>
                <div className="chat__msg-line">
                  <div className="chat__msg-content">
                    <div className="chat__msg-bubble">
                      <div className="chat__msg-text doc-md">訊息 {i}</div>
                    </div>
                  </div>
                </div>
              </div>
            ))}
            {/* ChatArea 的零高哨兵。它不是裝飾:因為它在,最後一則訊息下面就永遠
              * 還有一個 flex gap,盒子的底部於是不等於最新那一列的底部 —— 那正是
              * ① 曾經壞掉的地方。外殼少了它,量到的是一個不會出貨的版面。 */}
            <div className="chat__scroll-anchor" aria-hidden />
          </div>
          {arrow && <ChatJumpLatestButton onClick={() => {}} />}
        </div>
        <footer className="chat__composer">
          {preview && (
            <ChatNewMsgPreview
              who={who}
              body={body}
              onJump={() => {}}
              onDismiss={() => {}}
            />
          )}
          {banner && (
            <div className="chat__reply-banner" data-testid="chat-reply-banner">
              <span className="chat__reply-banner__text">
                <span className="chat__reply-banner__who">正在回覆 Mira → 韓立</span>
                <span className="chat__reply-banner__body">{body}</span>
              </span>
              <button
                type="button"
                className="chat__reply-banner__x"
                aria-label="取消回覆"
                data-testid="reply-banner-x"
              >
                ×
              </button>
            </div>
          )}
          <div className="chat__composer-row" data-testid="composer-row">
            <textarea className="chat__input" rows={1} defaultValue="" />
          </div>
        </footer>
      </div>
    </Shell>
  );
}

/**
 * 三條預覽列，內容長短天差地遠，放在同一個 composer 裡（同寬度，高度才可比）。
 *
 * 🔴 這是「更新內容不堆疊」的另一半：不堆疊代表同一條列會被反覆換掉內容，於是
 * 「高度不隨內容變」不再只是好看的問題 —— 每來一則訊息輸入框就會在打字的人手底下
 * 上下跳一次。空 body 那條是真的會發生的（純附件訊息）。
 */
export function NewMsgPreviewHeightStory() {
  const cases: Array<[string, string, string]> = [
    ["short", "Mira", "好"],
    ["long", LONG_WHO, LONG_BODY],
    ["empty", "Mira", ""],
  ];
  return (
    <Shell>
      <div className="chat" style={{ flex: 1, minWidth: 0 }}>
        <div className="chat__body">
          <div className="chat__messages" />
        </div>
        <footer className="chat__composer">
          {cases.map(([id, who, body]) => (
            <div key={id} data-testid={`preview-case-${id}`}>
              <ChatNewMsgPreview
                who={who}
                body={body}
                onJump={() => {}}
                onDismiss={() => {}}
              />
            </div>
          ))}
          <div className="chat__composer-row">
            <textarea className="chat__input" rows={1} defaultValue="" />
          </div>
        </footer>
      </div>
    </Shell>
  );
}

/**
 * ③ 跳轉提示列（`.chat__jump-miss`），兩句話各畫一條，放在同一個 composer 裡。
 *
 * 🔴 為什麼要量它：這條列是**寫在 composer 上面**的，而它的兩句話長度差很多
 * （英文那句更長）。折行本身不是罪，但它會把輸入框往下推 —— 而且這條列的出現
 * 時機是「跳轉剛落空」，正是使用者準備打字的那一刻。jsdom 量不到任何一格：
 * 沒有版面、沒有 @media、offsetHeight 永遠 0。
 *
 * DOM 形狀照抄 ChatArea 的實際輸出（composer 的第一個子元素，一個 span + 一顆
 * ×），文字由 I18nProvider 的真字串提供，不是手打的假字。
 */
export function ChatJumpNoticeStory({
  text,
  retry = false,
}: {
  /** 讀取失敗那一種收尾多一顆「再試一次」—— 在同一條固定高度的列裡多一個元素,
   * 窄版才是它會現形的地方。 */
  retry?: boolean;
  /** 真正的出貨文案，由 spec 從 locale 模組直接餵進來 —— 兩個語系的長度差很多，
   * 而**最長的那一句才是會現形的那一句**。手打的假字或只量預設語系，等於量一個
   * 一定塞得下的 fixture（LONG_BODY 那格已經踩過一次）。 */
  text: string;
}) {
  return (
    <Shell>
      <div className="chat" style={{ flex: 1, minWidth: 0 }}>
        <div className="chat__body">
          <div className="chat__messages" />
        </div>
        <footer className="chat__composer">
          <JumpMissRow text={text} retry={retry} />
          <div className="chat__composer-row" data-testid="composer-row">
            <textarea className="chat__input" rows={1} defaultValue="" />
          </div>
        </footer>
      </div>
    </Shell>
  );
}

function JumpMissRow({ text, retry }: { text: string; retry: boolean }) {
  const { t } = useI18n();
  return (
    <div className="chat__jump-miss" role="status" data-testid="jump-miss">
      <span data-testid="jump-miss-text">{text}</span>
      {retry && (
        <button
          type="button"
          className="chat__jump-miss__retry"
          data-testid="jump-miss-retry"
        >
          {t.chat.jumpTargetRetry}
        </button>
      )}
      <button
        type="button"
        className="chat__jump-miss__x"
        aria-label={t.chat.jumpTargetMissingDismiss}
        title={t.chat.jumpTargetMissingDismiss}
        data-testid="jump-miss-x"
      >
        ×
      </button>
    </div>
  );
}

/**
 * 「最新那一則在不在視野內」這個判準,跑在**真的版面**上(production 的
 * `isLatestRowInView` + production 的 office.css)。
 *
 * 🔴 jsdom 量不到這一條,而這一條就是 ① 出貨時壞掉的那一格:`.chat__messages` 的
 * flex gap 加上零高哨兵,讓盒子的可捲底部落在最新那一列底下 12px。用盒子底部回答
 * 「最新那一則在不在視野內」,在一個最新訊息完整可見的畫面上答「不在」,箭頭於是
 * 永遠不走。這個 story 把兩個答案一起吐出來:`distance`(盒子還剩多少)與
 * `inView`(產品的判準),讓 guard 可以斷言它們**不是同一件事**。
 */
export function LatestRowInViewStory({
  at = "bottom",
}: { at?: "bottom" | "top" | "just-below" } = {}) {
  const boxRef = useRef<HTMLDivElement>(null);
  const [probe, setProbe] = useState("");
  useLayoutEffect(() => {
    const box = boxRef.current;
    if (!box) return;
    const rows = box.querySelectorAll<HTMLElement>("[data-msg-id]");
    // 落點跟 `scrollToLatest` 逐字相同 —— guard 量的必須是產品真的會停的地方。
    if (at === "bottom") rows[rows.length - 1].scrollIntoView({ block: "end" });
    else if (at === "just-below") {
      // 🔴 The case the other two cannot fail on. "bottom" lands the row flush
      // and "top" puts it thousands of pixels away, so a tolerance inflated to
      // swallow the 12px gap passes BOTH — measured: SUBPIXEL_PX raised from 1
      // to 40 left every assertion green (independent review #17, F-2). Here the
      // newest row's bottom is just SIX pixels under the fold: still clipped, and
      // a reader would still want the arrow.
      rows[rows.length - 1].scrollIntoView({ block: "end" });
      // Clipped by HALF THE FLEX GAP, read from CSS rather than typed in. A bare
      // number here would be the same species as the tolerance it is testing —
      // it would stop tracking the layout the moment someone changed the gap,
      // and nothing would say so. Half, because that is the largest clip the
      // tolerance could ever be argued up to on gap grounds and still be wrong.
      // 🔴 NO SILENT FALLBACK. A `|| 0` here plus a floor would quietly turn
      // this case into a 2px clip the moment `rowGap` stopped resolving, and a
      // 2px clip passes almost any tolerance — the guard would keep reporting
      // green while measuring nothing (independent review #19, F7). If the gap
      // cannot be read, the probe says so and the assertion fails on it.
      const gap = parseFloat(getComputedStyle(box).rowGap);
      if (!Number.isFinite(gap) || gap <= 0) {
        setProbe(JSON.stringify({ error: `rowGap unreadable: ${gap}` }));
        return;
      }
      box.scrollTop -= gap / 2;
    } else box.scrollTop = 0;
    const last = rows[rows.length - 1].getBoundingClientRect();
    setProbe(
      JSON.stringify({
        distance: Number(
          (box.scrollHeight - box.scrollTop - box.clientHeight).toFixed(2),
        ),
        rowBottomGap: Number(
          (last.bottom - box.getBoundingClientRect().bottom).toFixed(2),
        ),
        inView: isLatestRowInView(box),
      }),
    );
  }, [at]);
  return (
    <Shell>
      <div className="chat" style={{ flex: 1, minWidth: 0 }}>
        <div className="chat__body">
          <div className="chat__messages" ref={boxRef} data-testid="chat-messages">
            {Array.from({ length: 30 }, (_, i) => (
              <div className="chat__msg" data-msg-id={`c-${i}`} key={i}>
                <div className="chat__msg-line">
                  <div className="chat__msg-content">
                    <div className="chat__msg-bubble">
                      <div className="chat__msg-text doc-md">訊息 {i}</div>
                    </div>
                  </div>
                </div>
              </div>
            ))}
            <div className="chat__scroll-anchor" aria-hidden />
          </div>
        </div>
        <footer className="chat__composer">
          <div className="chat__composer-row">
            <textarea className="chat__input" rows={1} defaultValue="" />
          </div>
        </footer>
      </div>
      <div data-testid="latest-probe">{probe}</div>
    </Shell>
  );
}
