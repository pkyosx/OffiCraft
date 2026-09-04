---
paths:
  - "src/components/Chat*"
  - "src/components/Reply*"
  - "src/components/RepliesPage*"
  - "src/components/TaskReplyCard*"
  - "src/components/TaskCardMessageBox*"
  - "src/components/ScheduledMessagesCard*"
  - "visual-guards/chat-*"
  - "visual-guards/stories/Chat*"
  - "src/components/replies.css"
  - "src/hooks/useChat*"
  - "src/hooks/useReplyCard*"
  - "src/hooks/useScheduledMessages.ts"
  - "src/lib/composerKeys.ts"
  - "src/lib/autosize.ts"
  - "src/lib/chatDraftStore.ts"
  - "src/lib/hashRoute.ts"
  - "src/api/mock.scheduled-messages.test.ts"
  # 🔴 THE WIRE LAYER IS IN SCOPE, and it was not. The T-4e95 rule below —
  # "the quote content is assembled by the SERVER; the mock says the same thing;
  # the frontend never shortens it again" — is IMPLEMENTED in these three files
  # and nowhere in src/components. Whoever edits the mock is exactly the person
  # this rule is written for, and they could not see it.
  - "src/api/mappers.ts"
  - "src/api/mock.ts"
  - "src/api/adapter.ts"
  # 🔴 THE WAKE SNAPSHOT IS A RENDERING SURFACE FOR THE QUOTE TOO, and it was
  # not listed either — which is exactly why it shipped for months billing the
  # quote's characters to the chat budget and drawing none of them (T-9871).
  # Whoever edits this card is one of the people the quote rules below are
  # written for.
  - "src/components/ResumeSummaryCard*"
  - "src/components/member-detail.css"
  - "visual-guards/resume-chat-quote*"
  # The guard's fixture and the mock's own witness are edited by the same people
  # and are just as easy to get wrong; a rule nobody sees while editing them is
  # the failure this whole block is about.
  - "visual-guards/stories/ResumeChatQuoteStory*"
  - "src/api/mock.reply-to.test.ts"
  - "visual-guards/scheduled-message-*"
  # 多選卡的版面護欄與它的 story —— 改晶片/暫存態/送出列的人就是這條規則要找的人。
  - "visual-guards/reply-multi-select*"
  - "visual-guards/stories/ReplyMultiSelectStory*"
---

# 聊天、composer、定期訊息、回覆卡

## 定期訊息

custom cadence 的 custom_months、custom_days、custom_hours、custom_minutes 是四個顯式集合的交集。每組都要有合法值，交集為空由 server 與 mock 拒絕；前端也要先擋空集合。custom_months 省略代表全年，明確空陣列仍是 422，所以表單送出時要明確帶目前選中的月份。

custom 不讀 hour、minute、day_of_week、day_of_month，也不把自己摘要成 daily。顯示標籤固定為「幾月」「幾號」「幾點」「幾分」；分鐘預設提供 0、5…55，但既有不在選項中的值要保留並可原樣存回。摘要只對從 0 開始、固定間距且整除 60 的分鐘集合說「每 N 分鐘」，零散值列出前幾項後用「另 N 個」。

這組分鐘格的護欄要用真瀏覽器量實際格線、中心點與頁面／面板 overflow；只數 class 或元素數量會在壞版面上照樣綠。mock 與 server 的存取規則也要分開對帳；新增非 generated 的閉集副本時，先由 source query 找全，不要把會過期的手抄清單寫進本檔。

## 聊天與 composer

進房時先在 render 同步 snapshot member.unreadCount，再由明確的 mark-read 清 watermark（listChat 不再有這個副作用，T-48）；第一則未讀是對方送給 owner 的未讀訊息中最早的一則，顯示 divider 並保留於本 session。房內新訊息只有在 owner 不在 near-bottom 時顯示 LINE 式預覽列（寄件者＋一行內容），點擊落在**最新那一則**（T-48 前是落在第一則未見，下面還壓著沒看到的訊息）；落點**不做事後校正**（T-48 owner rc-6c27f486ef9d 具名接受「上方晚載入的內容把目標擠走」，`scrollToLatest` 只捲一次），但那句裁定點名的圖片與請示卡今天都在**來源**擋掉了——縮圖有固定框，請示卡則是**一律先收合成一列**（T-48，owner 2026-09-04 `c-6f054c1cb481`：待回覆的卡也跟已回覆一樣先不展開），那一列要顯示的東西訊息本身就帶著（summary＝訊息正文、狀態＝`reply_card_status` 提示），所以它不撈任何東西、第一幀就是最終高度；被接受的是其餘晚到內容的位移；必須真的捲到底才清除。**最新那一則不在視野內**且**沒有**新訊息時顯示回到最新箭頭，兩者不同時出現（箭頭讓位給預覽列，owner rc-72054864ff88）。⚠️ 判準量的是**最新那一列的底邊**（`lib/scrollToLatest` 的 `isLatestRowInView`），不是「容器捲到底了沒」：`.chat__messages` 是 gap 的 flex 欄且最後一列下面還有零高哨兵，所以容器底部永遠在最新那一列底下一個 gap，用容器問會在最新訊息完整可見時答「不在」（T-48：按了箭頭它不會消失，每一次）。任何把它換回 `scrollHeight - scrollTop - clientHeight <= 某個常數` 的寫法都會把那個 bug 帶回來，而且 gap 一改就沒有人會紅。

ChatArea、ReplyComposer、TaskCard 訊息框都是 textarea，送出決策只由 lib/composerKeys.ts 的 enterShouldSend 提供：桌面 Enter 送出、Shift+Enter 換行；手機 Enter 換行、按鈕送出；IME composing 永不送出。autosize 上限 132px，超出由 textarea 自己捲動。

## 回覆卡

RepliesPage 與 ChatReplyCard 共用 ReplyCardBody、ReplyComposer；兩者訂 reply_card，inline 卡另做單卡 refetch。answer、expire 與 waiting-pane 的 owner 動作採用寫入端點回傳的新卡，不再重抓；採用後按 id 保留，直到 waiting 快照不再列出它，或 handled 快照帶著不舊於新狀態的 handled 戳記。其他卡照常採用，不能丟整份快照。refresh() 仍無條件 refetch。

ChatReplyCard 的 doReanswer 保留單卡 refetch：終態 delta 可能被刻意丟棄，拿掉會留下舊答案。不要把它和 doAnswer 對齊，也不要把「delta 是唯一 reconcile trigger」寫回規則。

## 選項是一組集合,不是一個位置(T-40)

**「AI 建議」由每個選項自己的 `aiPick` 攜帶,位置不帶任何意義。** 舊碼在三處寫死
`idx === 0`(晶片、最終答案列、`ResumeSummaryCard` 的履歷卡),那是一個碼上沒有
任何東西在執行的約定 —— 改一次選項順序就悄悄改掉了 AI 的建議是哪一個。**測資把
`ai_pick` 放在第一個就等於沒測**:位置讀法在那種測資上永遠蒙對。

**答覆是一份清單。** `ReplyCard.selectMode` 是 `single`|`multi`(與 `kind` 正交:
`kind` 說 owner 要做什麼,`selectMode` 說可以圈幾個)。晶片是**暫存**的 ——
single 點第二個取代第一個、multi toggle,再點同一個一律取消(「什麼都沒勾」必須
到得了,因為那正是送出鍵拒絕送的狀態)。**選項與自由文字由同一顆送出鍵合成同一次
送出**:卡片是一次性關閉,分兩條各自送的第二條必吃 409。

⚠️ **`optionIdxs: []` 不是「沒圈」,它是 server 刻意的 400。** 沒圈就**省略**這個
欄位;把空清單攤平成 `null` 會讓那個 400 永遠打不到,把它照原樣送出則會讓每一次
純文字答覆變成錯誤。索引在 `http.ts` 的 seam 去重＋升冪 —— 勾選順序不同不可以變成
兩份不同的 body。

⚠️ **有兩條互不相干的答案線,而且不共用任何型別。** 卡片本體走
`ReplyCard.answer.optionIdxs`;履歷摘要卡走完全獨立的
`ChatInlineReplyCardView.answerOptionIdxs`(`adapter.ts` → `mappers.ts` →
`ResumeSummaryCard.tsx`)。**只改一條,tsc 一聲不吭,履歷摘要卡會安靜地顯示零個
「已選」** —— 這和上面「引文有不只一個渲染面」是同一張卡上的同一個陷阱,而且它已經
發生過一次。顯示「你選的」的面共五個:最終答案列、「目前」標記(這兩面由三個 wrapper
共用同一份 `ReplyCardBody`)、`TaskReplyCard` 的收合一行、`ResumeSummaryCard` 履歷卡;
第五個 `ChatReplyCard` 收合列只印 summary,不讀答案 —— 而它現在是**每一張卡的預設樣子**,不再只有已回覆/已過期那兩種。

🔴 **前端這半沒有任何機械保護**(2026-08-31 配陽性對照確認):`api/dtoParity.ts`
不含 ReplyCard、style ownership 的 `OWNED_SHEETS` 不含 `replies.css`、payload
parity 的 roll-call 不列卡片內部欄位。守著這一切的只有
`ReplyCardBody.multi-select.test.tsx`、`TaskReplyCard.test.tsx`、
`ResumeSummaryCard.payload-parity.test.tsx` 與 `http.mutations.test.ts` 裡那幾條。

class 名 `.reply-tag--ai` / `.reply-option--ai` **不要改**:`TaskReplyCard` 借用前者
畫自己的徽章。

view=full 只在 HTTP list seam 表示整個 pane 的一次請求，不上提到 adapter，也不向 agent 的 MCP tools/list 宣傳；否則 agent 會拿到一次拉整個 pane 的昂貴把手，抵消輕量摘要契約。light/default 行為不變、未知 view 回 400。等待卡的 expire 規則以 server 為準：owner/admin 或卡片作者可過期自己的 waiting 卡；其他人 403，已回答 409。

hash route #office/chat/<id>/msg/<msgId> 只做一次定位與 highlight。產生它的是「請示」頁的**跳到原訊息**與任務卡內嵌回覆卡的**在聊天室回覆**（外加使用者自己留存的舊 URL）；聊天氣泡引用列的**看原訊息**不走這條，它撈那一則開覆蓋層（見下方「看原訊息」一節）。⚠️ T-0b78 曾把那兩顆也改成覆蓋層，owner 2026-08-29 裁定「1 跟 2 變回去原本那樣」—— 所以不要順手把它們改回覆蓋層。

**目標不在最近視窗時是撈，不是落到底（T-48，取代上面那一段原本的「知情接受」）。** owner 後來把那個暫緩解掉了（「都可以正確定位到該訊息」），所以這條路現在是：**進房當下就以那個 id 開窗**——`useChat(peer, jumpToMsgId)` 收到 anchor 就**完全不載最新那一頁**，ChatArea 的 jump reactor 從一個空 thread 直接打 `loadAround`（`?end_id=` 往舊、`?start_id=` 往新，兩端都含，兩頁而已，不是整條歷史）。⚠️ 這條鏈有一個**沒有機械保護的不變量**：anchor 被指定時**一定要有人真的去撈**，否則房間永遠空白；今天唯一的撈家是那個 reactor，它的 miss 分支再退回 `resetToLatest`。

`loadAround` 回的是一組具名結果（`JumpOutcome`），不是 bool：`found` / `missing`（404、失敗、或**存在但屬於別條對話**——server 解析錨點不套 participant 過濾，那種 id 兩個請求都回 200＋空陣列，採用它會把聊天室寫成空白）/ `superseded`（被更晚的載入超車，**訊息還在**）/ `cancelled`（這一趟被叫停 —— 房間走了，或更晚的一次跳轉取代了它；見下面走訪那一段）。**不要把 superseded 併回 missing**：那會對著一則還在的訊息說「可能已經被清掉了」，而且跳轉閂已經用掉，沒有重試也沒有按鈕。**也不要把 cancelled 併回 superseded**：前者要 caller 什麼都不做，後者要它重排一次。畫面語言只屬於前三者（`chat.jumpTargetMissing` / `chat.jumpTargetInterrupted`），重排有上限；`cancelled` 沒有畫面語言，因為它不對讀的人說話。

🔴 **跳轉是一路撈到活尾巴，而且撈完才 render（T-48 fix12，owner `rc-e1fb80065f8f`「可以直接在這票做，並且一次撈100則撈完」＋ `c-6a973512ed77`「我是指整個訊息撈完才 render」）。** `loadAround` 開完錨點窗之後，如果往新那一頁**滿了**（`CHAT_WALK_PAGE_SIZE = 100`，window 路徑的 `limit` 上限是 server 的 `chatWindowMaxLimit = 200`），就用 `fetchToLatest` 在**記憶體裡**一頁一頁往前收，直到某頁回不滿 100 為止，然後**一次** `commit`。畫面上沒有任何中間狀態。

⚠️ **這一段取代了先前的「一次手勢一頁」，而那一整套機關已經刪除**：`loadNewer` 的 `human` 參數、`HUMAN_RETRY_MIN_MS` 400ms 節流、被吞手勢的 trailing 補送、`pageUnseenIdRef` 視覺閘、`lastServedAnchorRef`、`blockedInFlightRef`、`forwardExhaustedRef`、`ChatArea` 的 `session.lastScrollTop` 方向判準與 `readerAtBottom` 探針，以及護欄 `visual-guards/chat-forward-walk.ct.spec.tsx`（含它的 story／fixtures）。**它們守的問題已經不存在**（沒有人在用手勢買頁），不要因為「這段註解寫得很仔細」就把它們搬回來。

`fetchToLatest` 有**兩道停**，而且都不是「界」而是終止證明：短頁（＝到尾巴）；以及**滿頁卻一列新的都沒有**（server 在自我矛盾，錨點沒有前進 ⇒ 下一通會問一模一樣的問題 ⇒ 忙迴圈）。⚠️ 這兩道只在「尾巴長得比走訪慢」的前提下才是終止證明，那個前提今天沒有被守 —— 要不要加列數／頁數上限是另一張還沒裁的卡，**不要順手加界**。它**永不 reject**：某一頁失敗就把手上收到的照樣交出去。

🔴 **但它可以被叫停。** 走訪是一個對著網路跑的 `for(;;)`，所以它拿 `loadAround` 給的 `isCurrent()`——`alive`（房間／對話沒了）＋**世代票**（更晚的一次跳轉取代了它）——並且在**每一頁之前與之後**各問一次。被叫停時它回 `cancelled: true`，`loadAround` 就**什麼都不 commit**、回傳 `JumpOutcome` 的第五態 `cancelled`。⚠️ **`cancelled` 不是 `superseded`**：`superseded` 要 caller 重排一次，`cancelled` 要 caller **什麼都不做**（取代它的那一趟已經在撈了），把兩者併起來會讓讀的人早就放棄的目標又被撈一次。釘在 `useChat.scrollback.test.ts` 的「走訪途中離開房間」與「新的跳轉取代舊的」。

🔴 **失敗照樣 commit，並且用 `hasNewer` 說實話。** 一次 commit 的代價是「不 commit ＝ 讀的人盯著空房間」，所以撈到一半失敗時仍然把手上有的貼上去，`hasNewer` 留 `true` —— 於是 `回到最新` 箭頭留在畫面上（唯一的出口，因為捲到極限的箱子連 scroll 事件都送不出來）、`load()` 繼續讓開、已讀水位繼續不准動。**`hasNewer` 因此變成例外狀態而不是常態，但它一行都不能刪。**

🔴 **已讀水位的守衛換人了，這是 fix12 自己造出來的風險。** 以前擋 mark-read 的是 `hasNewer`，而它成立是因為讓 `hasNewer` 變 false 的每一步都是讀的人親手捲出來的 ——「握得到活尾巴」和「看過活尾巴」剛好是同一件事。現在走訪自己撈完，`hasNewer` 落地就是 false 而讀的人還停在半年前那一則上，而 `ChatArea` 那支 mark-read effect **完全不看視窗位置**。所以守衛是 `ChatArea` 的 **`tailSeen`**（讀的人有沒有真的到過這條線的底部）：跳轉開始抓時設 false，**捲進底部帶／按下回到最新或預覽列／跳轉沒中而退回底部**三者之一才設回 true。`mayMarkRead = !hasNewer && !jumpPending && tailSeen`。⚠️ **送訊息不解除它** —— 停在歷史裡送一句話不代表看過中間那幾百則。釘在 `ChatArea.anchor-entry.test.tsx` 的「走訪把中間幾百則載進來,一則都不准被標成已讀」（真 ChatArea ＋ 真 useChat，量 `markChatRead` 的 ts）與 e2e `20_chat_jump_to_origin.spec.js`（量 server 端未讀數）。

錨點視窗期間（也就是走訪還沒完成、或走訪失敗停在半路）`hasNewer=true`，這時**不標已讀**、**不跑週期性/SSE 的最新頁載入**（把活尾巴併進歷史視窗會造出一段沒人撈過卻被畫成相鄰的縫），而且捲動位置反應器**不 auto-follow**（`if (!hasNewer) endRef.scrollIntoView()`）—— 不跟的條件是 `hasNewer`，**其他 auto-follow（活尾巴新訊息、自己剛送出、進房定位）一律不動**。

🔴 **載入指示是一個狀態，不是每個入口各一份（owner `c-de666642e77b`「不管是進聊天室，或點選元訊息都是這樣」＋ `c-d24ebd7f8d78`「照理說應該只有改一個地方吧？」）。** `useChat` 對外的 **`initialLoading`**＝「這條對話的內容還沒到」**或**「正在走訪」，兩件事在那**一個**推導裡 OR 起來，`ChatArea` 只有**一處** render `<ChatThreadLoading />`，而且那一處問在 `messages.length` **之前**。⚠️ **順序不是格式問題**：問在後面的話，「起跳時畫面上已經有內容」的等待永遠走不到那個分支 —— 而那正是**同一間房的第二次跳到原訊息**（`OfficePage` 的 key 是 `peerId`，只換 `route.msgId` 不會 remount：第二次跳轉、瀏覽器上一頁、留存的舊連結），也正是這個功能**最長**的一段等待。⚠️ 走訪那半的旗標必須在 **commit 之前**放下，不能放在 `finally`：轉圈是**取代**訊息區的，commit 落在還是轉圈的那一幀時，ChatArea 的 jump reactor 查不到那一列，跳轉就沒有人捲。三條路（一般進房／第一次跳轉／同房第二次跳轉）由 `ChatArea.anchor-entry.test.tsx`「轉圈是一個狀態,三條路都要有」一支釘住，三個斷言刻意用 `expect.soft`，好讓「只紅一條」與「三條都紅」在輸出上分得出來。轉圈**延遲 150ms 才出現**（`CHAT_LOADING_DELAY_MS`）：快的時候完全不出現，因為閃一下比不出現更糟；那是**延遲**不是最短顯示時間，內容一到立刻消失。顏色全部走 theme token（`--color-border` / `--color-accent`），`prefers-reduced-motion` 換成脈動而不是靜止。護欄 `visual-guards/chat-thread-loading.ct.spec.tsx`（兩個入口 × 窄寬兩寬，加「一次落地」「快的時候不閃」）。

📏 **成本量過了，數字在 `work/T-48-docs/fix11-render-cost.md`**（真 Chromium，附原始輸出）：`ChatArea` 沒有 virtualization，8,000 列一次載進畫面 0.58 秒 / 49 MB heap / 156k DOM 節點；**一頁一頁 commit 則是 12.4 秒**（二次式，因為每次 commit 都重跑整條）。這就是「撈完才 render」不只是偏好的原因。⚠️ 舊註解裡那句「8,000 則約兩分鐘、2.6 GB」**沒有任何產物支撐，而且重量之後不成立**，已經從 e2e spec 20 移除。

回到活尾巴的路只剩一條：**`resetToLatest`**（`回到最新` 箭頭／預覽列）——**取代**不是合併，並且負責解除 anchor 的載入 hold-off（不解除的話那間房從此不再刷新）。往下捲那條走訪已經沒有對外的把手了。

回覆卡的 red badge 與聊天未讀互不清除；任務關聯卡共用卡身，只顯示任務標題與查看詳情連結。

## 「回覆這則」（T-4e95，owner 2026-08-21 改設計）

**引用內容由 server 隨每次讀取現組，前端只讀不找。** 每則 `reply_to` 非空的
訊息，server 會在**每一個**讀取出口（listing、history page、`?ids=`、POST 回
應、wake snapshot）附上
`reply_to_chat = {id, from, from_name, to, to_name, content}`。前端
畫引用列就是讀 `m.replyToChat`，**沒有查表、沒有 fallback、沒有補撈**。

**引用列畫的是「寄件者 → 收件者」，而那個收件者是被引訊息自己的 `to`。**
不是這條線的對方 —— 引用可以跨對話（2026-08-21 裁定），兩者剛好在那個情況下不
一樣，而那正是這個欄位存在的理由。`nameOf` 本來就會退到原始 id，所以兩邊永遠有
字可印：**沒有空白態、沒有「未知」佔位字**。`from_name` 與 `to_name` 同一組規
則：只有 wake snapshot 那條讀法會填，其餘一律 `""`，要指認人一律用 id。
composer 上方的橫幅走同一個形狀（它從已載入視窗解析，是第二條到同一句話的路）。

**引用列與橫幅都是兩行：第一行「寄件者 → 收件者」，第二行被引的那句話。**
（owner 2026-08-22 裁定）原本兩者擠在同一行，而 flex 的收縮順序讓句子先讓位，所以
「→ 收件者」一加上去就等於直接從句子身上扣寬度：實測 vw=721（app shell 的成員欄
把 pane 砍到 347px）時名字那半 101px、句子那半只剩 18px —— 本機 3/61 字、CI runner
0/61 字。**兩行是把競爭拿掉，不是去仲裁它**：句子獨占一整行之後，任何長度的名字都
拿不走它的寬度。代價是垂直空間（引用列 20.8px→36.7px，橫幅 34px→42.4px）。
名字那半在極窄 pane 仍然只是 `text-overflow: ellipsis` 截短（同一個 span、同一個
箭頭、同一個順序），**不是第三種畫法**。

**引文有不只一個渲染面，改動要先把它們找齊**——它們是各自獨立的碼路（訊息列與
喚醒快照卡各自讀 `replyToChat`，composer 橫幅讀 `messageById`），只改其中一面
會讓同一句引文在不同地方長得不一樣，或在某一面乾脆不出現。
**不要記數量，也不要照抄清單**：這一行原本寫死「兩個渲染面」，而當時已經有第三
個（`ResumeSummaryCard` 的快照卡）——那張卡的預算**一直在為引文的字計費**，畫面
上卻一個字都沒有，而規則的 `paths:` 也沒有涵蓋它，所以改引文的人看不到這條規則，
被漏掉的那一面也不會有任何東西變紅（T-9871）。
**找齊的方法是從資料查回來，而不是相信這裡的列舉**：以 view model 的欄位為起點
`grep -rn "replyToChat\|messageById" frontend/src`，命中的每一個 component 都是
一個面；新增一個面時，把它加進本檔的 `paths:`，否則下一個人同樣看不到。

**但「一起改」不等於「長一樣」。** 快照卡不在 `chat-pane` 這個 container 裡，所以
聊天面靠 `@container chat-pane` 收掉 jump label 的那條規則在它身上**永遠不會觸發**；
它也沒有 `openQuotedMessage` 那套 overlay 與再撈，而它本身已經是一張有邊框的卡。
所以那一面是**沒有控制項、靠換行與 `overflow-wrap` 自己撐住**的另一種畫法，幾何由
`visual-guards/resume-chat-quote.ct.spec.tsx` 在寬窄兩端量著。要對齊的是**內容與兩
態規則**（有快照就畫、沒有就畫固定的 `chat.replyQuoteGone`；`content` 為空是合法的
第一態，不可折進第二態），不是版面。

**這一段取代了原本的「只帶 id，前端自己撈」設計，理由要記住：** 舊設計裡撈得到
／撈不到／還沒撈到是三個狀態，而它們在畫面上長得一模一樣，所以出錯時沒有人看得
出來 —— 二十輪審查裡最多阻擋項就長在那台狀態機上。**不要因為「反正那則就在畫面
上，省一次查詢」而把查表加回來**：有時會夾、有時不夾的優化，代價是 client 必須
為「沒夾」的情況準備一條後路，而那條後路就是被刪掉的那台機器。

**引用只有兩態，不准生出第三態**：server 給了快照（畫出來），或 server 沒給
（畫**固定**的 `chat.replyQuoteGone`「這則訊息已不存在」）。**不重試、不補撈、
不自癒**，也不准出現「…」這種還在等的樣子 —— 沒有任何東西在飛。
`content` 是空字串是**合法**的（被引用的那則只有附件），要畫成「有名字、內容空
白」，不可以折進「已不存在」。

**橫幅有自己的文案，不准借用訊息列那一句。** composer 上方的「正在回覆」橫幅沒有
`reply_to_chat` 可讀（那是回覆**送出後**才存在的東西），它只認**已載入視窗**裡的
那則（`messageById`）。而「不在已載入視窗」跟「訊息不存在」是兩件事：往上捲載入
scrollback → 瞄準一則舊訊息 → 切到別的成員再切回來（草稿連同對象一起還原，但視窗
只重載最新一頁）⇒ 橫幅認不出對象，**而那則訊息還在、照送也會成功**（`reply_to`
存得對，讀回來的 `reply_to_chat` 內容完整）。
所以橫幅畫的是**與狀態無關的實話** `chat.replyingToEarlier`「正在回覆較早的一則
訊息」；斷定句 `chat.replyQuoteGone`「這則訊息已不存在」**只屬於訊息列**，因為那
一格的資料來源是 server 這次讀取的答案，有資格做這個斷定。
**不要把兩個 key 指到同一句，也不要為了這一格把查詢或補撈加回來。**

**截短是 server 做的，前端不准再切一次。** `chatReplyQuoteMaxChars`（60 runes）
＋收斂空白都在 server 做完才上線（原本的 `QUOTE_EXCERPT_CHARS` 已刪）。畫面上每一行的
長度限制交給 CSS `text-overflow: ellipsis`（引用列有兩行，各自裁各自的）。

⚠️ **但那個數字有第二份副本**：`frontend/src/api/mock.ts` 的
`MOCK_REPLY_QUOTE_MAX_CHARS`，離線預覽用（mock 沒有 server 可以問）。這一行以前
寫「截短長度只有 server 有」，那是假的，而且兩邊的測試各自寫死 60，所以改 server
不會弄紅前端。現在 `mock.reply-to.test.ts` 會去讀 `server/ocserverd/wire.go` 那一
行，兩個數字不一致就紅 —— 要改長度，兩份一起改。

**引用內容只從 wire 來，「看原訊息」是點下去才撈 —— 兩者都不准在 render 或
effect 裡發請求。** 引用列畫的是 server 這次隨訊息一起送來的 `reply_to_chat`，
沒有就畫 `chat.replyQuoteGone`，**不准為了補齊它去發任何請求**（那台背景補撈的
機器 `useQuotedMessages` 已於 2026-08-21 刪除；`ChatArea.quote-no-fetch.test.tsx`
的第一條就是「畫一則有引用、一則引用不見，api 一次都沒被碰」）。

「看原訊息」按鈕則是 owner 2026-08-21 的裁定（`rc-8559fd6d3c94`：「全部統一就撈
那一則顯示出來就好」）：**每一則有引用的列都給，不問那則在不在已載入視窗** ——
`ChatArea.tsx` 的 render 條件是 `m.replyTo && quoted`，完全不查 `messages`。點下去
用 `api.getChatMessage(replyTo)` 撈那一則，開跟 放大閱讀 同一片覆蓋層，**不捲動**。
這一段是刻意合成的：以前它問「那則在不在視窗」來決定給不給鈕，owner 把它改成一律
給、按了才撈，所以**不要再把窗口成員資格的判斷加回去**。

⚠️ 但「點下去才撈」有兩條紀律：**一次點擊只准一次請求**（`quoteBusyRef` 這個
in-flight latch，不是 state —— 同一個 tick 的兩次點擊都會讀到更新前的 state），
失敗只記**一個** id、原地說一句，**不重試、不排隊、不在下一個 SSE 事件自癒**。
第一條由 `ChatArea.quote-no-fetch.test.tsx` 的
「a click on the quote costs exactly one request, and repainting costs none」釘住；
第二條由 `ChatArea.reply-to.test.tsx` 的
「says so, in place and once, when that one read fails」釘住 —— 失敗路徑的證人在
`reply-to` 那個檔案，不在 `quote-no-fetch`（後者的 api proxy 刻意不註冊失敗態）。

⚠️ **但要精確：第二條那個測試釘住的比這句話列舉的少。** 它實際斷言四件事——
①失敗訊息說在原地、②引用列文字不變（不會變成「這則訊息已不存在」）、③不開覆蓋
層、④`getChatMessage` 只被呼叫一次（不重試、不排隊）。**「只記一個 id」（單值
語意，last click wins）與「不在下一個 SSE 事件自癒」這兩件今天沒有任何測試守
著**：全庫的測試檔裡，只有 `ChatArea.reply-to.test.tsx` 這一個檔碰得到
`msg-quote-error`（陽性對照：`msg-quote-jump` 命中 4 個測試檔）。它們
目前只由碼本身保證——`quoteOpenFailedId` 是單一 state、`openQuotedMessage` 是唯
一寫入點。**動這兩件事不會有測試變紅，請自己看碼。**

⚠️ 還有第三條：**讀取途中不准把那顆按鈕 `disabled`**。有過一個 loading 態這樣
做，實測在真 Chromium 裡 disable 一顆**正被聚焦**的按鈕會讓它 blur，
`MarkdownPreviewOverlay` 掛載時抓到的 opener 就成了 `<body>`，關掉覆蓋層時鍵盤
使用者被丟回頁面最上面。防連點是 `quoteBusyRef` 的工作，不是 `disabled` 的。
這條由 `ChatArea.quote-no-fetch.test.tsx` 的
「stays enabled while its read is in flight」釘住；注意 **jsdom 不會**因為
disabled 而 blur，所以那條測試裡真正有牙的是那句 `jump.disabled` 斷言本身，焦點
那兩條在這一層自己不會紅。

`messageById` 還活著，但它只回答**一個**問題：composer 上方的橫幅認不認得回覆
對象。它**不**回答「能不能看原訊息」。

**取消回覆的 x 只清回覆對象。** 不准順手清 `draft`、不准清
`pendingAttachments`：取消回覆不是取消訊息。這條有測試釘住
（`ChatArea.reply-to.test.tsx`）。

**送出後一定要清掉回覆對象**，否則它會默默黏在下一則訊息上。送出**失敗**時的還
原不准蓋掉使用者飛行中重新瞄準的對象。

**每一則都有回覆入口 —— 包含 agent 之間的訊息。** server 端「必須同一場對話」
的檢查已於 2026-08-21 拿掉，owner 的原話是要能「引用另外兩個人對話裡的一句話來
介入詢問」。所以 `replyable` 這個 gate 已刪：入口出現在視窗裡的每一則上，回覆
仍然寄給這條線的 peer，只是引用的是 owner 指到的那一句。

**唯一的例外是位置，不是有無：請示卡那種訊息**的氣泡被 `<ChatReplyCard>` 換掉
（那是一整片有自己 header 控制項的表面，浮一顆按鈕上去會撞在一起），所以入口留
在列上。回得動，只是不在角落。

以上都有測試釘住（`ChatArea.reply-to.test.tsx`、`ChatArea.quote-no-fetch.test.tsx`、
`ChatArea.reply-card-quote.test.tsx`、`mock.reply-to.test.ts`、`mappers.reply-to.test.ts`）。
其中 `ChatArea.quote-no-fetch.test.tsx` 是「那台狀態機真的不在了」的證人：它把
api client 換成記錄用的 proxy，畫一則有引用、一則引用不見的訊息，然後斷言**一次
呼叫都沒有**。
