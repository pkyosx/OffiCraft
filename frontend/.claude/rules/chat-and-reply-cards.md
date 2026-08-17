---
paths:
  - "src/components/Chat*"
  - "src/components/Reply*"
  - "src/components/RepliesPage*"
  - "src/components/TaskReplyCard*"
  - "src/components/TaskCardMessageBox*"
  - "src/components/ScheduledMessagesCard*"
  - "src/components/replies.css"
  - "src/hooks/useChat*"
  - "src/hooks/useReplyCard*"
  - "src/hooks/useScheduledMessages.ts"
  - "src/lib/composerKeys.ts"
  - "src/lib/autosize.ts"
  - "src/lib/chatDraftStore.ts"
  - "src/lib/hashRoute.ts"
  - "src/api/mock.scheduled-messages.test.ts"
  - "visual-guards/scheduled-message-*"
---

# 聊天、composer、定期訊息、回覆卡

## 定期訊息

custom cadence 的 custom_months、custom_days、custom_hours、custom_minutes 是四個顯式集合的交集。每組都要有合法值，交集為空由 server 與 mock 拒絕；前端也要先擋空集合。custom_months 省略代表全年，明確空陣列仍是 422，所以表單送出時要明確帶目前選中的月份。

custom 不讀 hour、minute、day_of_week、day_of_month，也不把自己摘要成 daily。顯示標籤固定為「幾月」「幾號」「幾點」「幾分」；分鐘預設提供 0、5…55，但既有不在選項中的值要保留並可原樣存回。摘要只對從 0 開始、固定間距且整除 60 的分鐘集合說「每 N 分鐘」，零散值列出前幾項後用「另 N 個」。

這組分鐘格的護欄要用真瀏覽器量實際格線、中心點與頁面／面板 overflow；只數 class 或元素數量會在壞版面上照樣綠。mock 與 server 的存取規則也要分開對帳；新增非 generated 的閉集副本時，先由 source query 找全，不要把會過期的手抄清單寫進本檔。

## 聊天與 composer

進房時先在 render 同步 snapshot member.unreadCount，再由 listChat 清 watermark；第一則未讀是對方送給 owner 的未讀訊息中最早的一則，顯示 divider 並保留於本 session。房內新訊息只有在 owner 不在 near-bottom 時顯示浮條；點擊只跳到第一則，必須真的捲到底才清除。

ChatArea、ReplyComposer、TaskCard 訊息框都是 textarea，送出決策只由 lib/composerKeys.ts 的 enterShouldSend 提供：桌面 Enter 送出、Shift+Enter 換行；手機 Enter 換行、按鈕送出；IME composing 永不送出。autosize 上限 132px，超出由 textarea 自己捲動。

## 回覆卡

RepliesPage 與 ChatReplyCard 共用 ReplyCardBody、ReplyComposer；兩者訂 reply_card，inline 卡另做單卡 refetch。answer、expire 與 waiting-pane 的 owner 動作採用寫入端點回傳的新卡，不再重抓；採用後按 id 保留，直到 waiting 快照不再列出它，或 handled 快照帶著不舊於新狀態的 handled 戳記。其他卡照常採用，不能丟整份快照。refresh() 仍無條件 refetch。

ChatReplyCard 的 doReanswer 保留單卡 refetch：終態 delta 可能被刻意丟棄，拿掉會留下舊答案。不要把它和 doAnswer 對齊，也不要把「delta 是唯一 reconcile trigger」寫回規則。

view=full 只在 HTTP list seam 表示整個 pane 的一次請求，不上提到 adapter，也不向 agent 的 MCP tools/list 宣傳；否則 agent 會拿到一次拉整個 pane 的昂貴把手，抵消輕量摘要契約。light/default 行為不變、未知 view 回 400。等待卡的 expire 規則以 server 為準：owner/admin 或卡片作者可過期自己的 waiting 卡；其他人 403，已回答 409。

hash route #office/chat/<id>/msg/<msgId> 只做一次定位與 highlight；若訊息不在最近窗口就誠實落到底。回覆卡的 red badge 與聊天未讀互不清除；任務關聯卡共用卡身，只顯示任務標題與查看詳情連結。
