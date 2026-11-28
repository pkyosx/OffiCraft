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
  - "src/components/IdFilterInput*"
  - "src/components/idFilter.css"
  - "visual-guards/reply-id-filter*"
  - "visual-guards/stories/ReplyIdFilter*"
  - "src/hooks/useChat*"
  - "src/hooks/useReplyCard*"
  # 🔴 THE IDENTITY CACHE IS IN SCOPE (T-196). The 請示卡列表 draws an outsource
  # asker's 代號, avatar AND current task from `useWorkerCodenames`'s per-id
  # read — the one read that covers RELEASED workers, which the rule below
  # depends on. Whoever edits that hook is one of the people the rule is
  # written for, and could not see it.
  - "src/hooks/useWorkerCodenames*"
  - "visual-guards/reply-card-worker-task*"
  - "visual-guards/stories/ReplyCardWorkerTaskStory*"
  - "src/hooks/useScheduledMessages.ts"
  - "src/lib/composerKeys.ts"
  - "src/lib/chatDraftStore*"
  - "src/hooks/useAttachmentStaging*"
  - "src/lib/chatMention*"
  - "src/lib/mentionLinks*"
  - "src/hooks/useMentionOpener.ts"
  - "src/hooks/useMentionComposer.ts"
  - "src/components/MentionText*"
  - "src/components/TaskCard.tsx"
  - "visual-guards/stories/ComposerMention*"
  - "src/lib/autosize.ts"
  - "src/lib/chatDraftStore.ts"
  - "src/lib/replyExcerpt.ts"
  - "src/lib/replySnooze*"
  - "src/lib/replyCardSort*"
  - "src/lib/replyCardReceipt*"
  - "visual-guards/reply-desk*"
  - "visual-guards/stories/ReplyDesk*"
  - "src/lib/hashRoute.ts"
  - "src/lib/hashRoute.test.ts"
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

帶著未讀徽章進房與否，在 render 時同步讀 member.unreadCount 決定（進房後的 mark-read 會把它清成 0）。第一則未讀由**伺服器**回答（`getFirstUnreadChat`＝`GET /api/chat?unread=true&with=<id>&limit=1`：對方送給 owner、在水位之上最舊的一則），房間以它為錨點載入（與跳到原訊息同一條 `loadAround`），divider 顯示在它上方並保留於本 session；查不到、查詢失敗或那一段讀不到就退回最新一頁、落在最底，等最新頁的期間維持轉圈（不先畫「這個範圍還沒有訊息」），那一頁也讀失敗時跟一般 load 失敗一樣記 `loadStale`，下一個聊天事件補抓。⚠️ 錨點還在路上時（帶未讀進房或跳到原訊息），SSE／focus 觸發的 `load()` 會被擋下，但要記 `loadStale`，錨點落地放掉 lease 時補一次 `load()`——錨點那一段是在新訊息到之前讀的，不補的話那一則要等下一個事件才出現。⚠️ 不能從已載入的頁面倒數：未讀超過一頁、或該成員與其他成員的往來塞滿那一頁時，第一則未讀根本不在頁裡，徽章數也可能落後伺服器。標已讀（owner 裁定）：帶未讀進房即以最新一則標已讀，不論未讀多少列、最新一則在不在畫面內，不必捲到底；落點仍停在第一則未讀。跳到原訊息進房不在此列，照舊要捲到底、按回到最新或預覽列才標。watermark 只由明確的 mark-read 推進，listChat 不寫。房內新訊息只有在 owner 不在 near-bottom 時顯示 LINE 式預覽列（寄件者＋一行內容），點擊落在**最新那一則**（T-48 前是落在第一則未見，下面還壓著沒看到的訊息）；落點**不做事後校正**（T-48 owner rc-6c27f486ef9d 具名接受「上方晚載入的內容把目標擠走」，`scrollToLatest` 只捲一次），但那句裁定點名的圖片與請示卡今天都在**來源**擋掉了——縮圖有固定框，請示卡則是**一律先收合成一列**（T-48，owner 2026-09-04 `c-6f054c1cb481`：待回覆的卡也跟已回覆一樣先不展開），那一列要顯示的東西訊息本身就帶著（summary＝訊息正文、狀態＝`reply_card_status` 提示），所以它不撈任何東西、第一幀就是最終高度；被接受的是其餘晚到內容的位移；必須真的捲到底才清除。**最新那一則不在視野內**且**沒有**新訊息時顯示回到最新箭頭，兩者不同時出現（箭頭讓位給預覽列，owner rc-72054864ff88）。⚠️ 判準量的是**最新那一列的底邊**（`lib/scrollToLatest` 的 `isLatestRowInView`），不是「容器捲到底了沒」：`.chat__messages` 是 gap 的 flex 欄且最後一列下面還有零高哨兵，所以容器底部永遠在最新那一列底下一個 gap，用容器問會在最新訊息完整可見時答「不在」（T-48：按了箭頭它不會消失，每一次）。任何把它換回 `scrollHeight - scrollTop - clientHeight <= 某個常數` 的寫法都會把那個 bug 帶回來，而且 gap 一改就沒有人會紅。

三個框打到一半的內容都存在 `lib/chatDraftStore.ts`（模組層級記憶體，重新整理就不見，刻意不寫 localStorage），收合、切頁、元件卸載都不會丟：聊天以 peer id 為鍵；請示卡以 `replyCardAnswerDraftKey`／`replyCardReanswerDraftKey` 為鍵（作答與重新決定各一份，三個面共用同一張卡的那一份），存文字、附件與引用的段落；任務卡訊息框以 `taskMessageDraftKey` 為鍵。請示卡與任務卡的框，文字直接讀寫 store（`useDraftText`），不另存一份元件 state：送出途中切頁再回來時，新掛上的框跟著 store 變，看不到、也送不出那段正在送的字。送出跟聊天一樣先清空、失敗才還原（`restoreChatDraft`，送出途中新打的字優先）；重新決定按「取消」清掉，收合不清，有打字或附件時再展開直接回到重新決定（只按過重新決定、帶著原本引用的那份不算）。附件錯誤訊息：聊天的由 OfficePage 的 `openChatAttachErrorScope` 管，只清聊天對象的；請示卡與任務卡的每個鍵由框自己開 `openDraftAttachErrorScope(key)`，最後一個框離開才清那個鍵，兩邊互不清除。

ChatArea、ReplyComposer、TaskCard 訊息框都是 textarea，送出決策只由 lib/composerKeys.ts 的 enterShouldSend 提供：桌面 Enter 送出、Shift+Enter 換行；手機 Enter 換行、按鈕送出；IME composing 永不送出。autosize 上限 132px，超出由 textarea 自己捲動。

@ 提及在三個框都一樣，只有一份實作 `hooks/useMentionComposer.ts`：聊天 composer、任務卡的傳訊息框（TaskCard）、請示卡的回覆框（ReplyComposer）。`useMentionRoster` 從頁面已經載好的名冊做出清單列（角色用 `t.office.role[m.role] ?? roleName`、特助置頂，即成員清單的預設順序；分群後側欄改依群組排，@ 清單不跟著變）與把連結畫成人的解析器；`useMentionComposer` 是單一 textarea 的清單狀態與按鍵。名冊來源：ChatArea 與 TaskCard 用 props 的 members／workers；請示頁的 light 名冊沒有任務編號與標題，所以另掛 `useOutsourceWorkers`，但**只在至少一張卡展開時才掛**（頁底的 `OutsourceWorkersFeed`，整頁一份，不是每張卡一份；全部收合就卸掉，連同它的 member／task／chat／chat_read 訂閱）。進頁面、卡全收合時只有 light 名冊那一支，不讀完整 `GET /api/members`（T-cf91 刻意避開的全公司未讀掃描），由 `RepliesPage.test.tsx` 的「arrival with every card collapsed…」守著，陽性對照是同檔「opening cards reads the outsource list once…」。回覆框沒有自己的名冊，讀宿主提供的 `MentionRosterContext`（ChatArea 包 ChatReplyCard、TaskCard 包整張卡、RepliesPage 包卡片內文）；沒有 provider 就沒有清單、送出原文。清單開著時，Enter／Tab 是「選這一列」（桌面與手機都是），不送出也不換行；IME composing 的判準與送出共用 `isImeComposingKey`；Esc 要 preventDefault，否則 escapeLayers 會連宿主面一起關。聊天的清單往上開；任務卡與請示卡的框在捲動頁中間，清單往下開（`placement="below"`），往上開會被捲動區頂端切掉且捲不到。任務卡整張是 toggle，濾網要放行 `[role='listbox']`，否則點清單列會收合／展開卡片。草稿與送出失敗的還原一律存使用者打的 `@名字` 原文；只有送出的那份 body／回覆文字經 `lib/mentionLinks.ts` 的 `linkMentions` 轉成 `[@名字](/#office/member|worker/<id>)`。那種連結只在有解析器的面畫成可點的名字：ChatArea 以 `MentionOpenerContext` 包住的訊息本文與放大閱讀，以及請示卡的最終回覆（`MentionText`，回覆是純文字不是 Markdown）。顯示的是名冊上現在的名字、不是訊息裡寫的那段字（連結不能看起來是一個人、點開是另一個人）；id 不在目前名冊（例如已釋出的外包）就只畫寫的字。點下去：聊天裡開在那間聊天室上；任務卡與請示卡帶來源 ID，分別用 `/from/tasks/<task-id>`、`/from/replies/<reply-card-id>`，返回時回到同一張卡並展開。放大閱讀那一份點下去要先關掉覆蓋層再導頁，否則成員詳情開在覆蓋層底下。請示卡的問題本文（agent 寫的 Markdown）沒有 provider，照舊是普通同源連結；其他 Markdown 面也是。只畫純文字的面（訊息列的引用、回覆橫幅、新訊息預覽列、任務卡內收合的已回覆列）用 `unlinkMentions` 還原成 `@名字`；那只是顯示，存的與送的不動。續接摘要的引用與回覆刻意不轉，那是 agent 讀到的原文。提及不通知任何人。

打字當下，框裡對得到名冊的 `@名字` 由 `MentionHighlight` 標示：它是疊在 textarea **上方**、文字透明、不收指標事件的一層，執行時複製 textarea 的字型、內距、框線、換行與捲動；底色因此必須是半透明的 `--color-accent-soft`，不透明會蓋住底下的字。標示與送出共用 `findMentions`（`linkMentions` 也是由它組字），所以標示出來的就是會變成連結的，不要另寫一份比對。名字是一個單位：名字後 Backspace、名字前 Delete 整塊刪掉（帶修飾鍵、有反白或 IME 組字中不攔），游標落進名字中間會被移到頭或尾。在名字邊緣按左右鍵由 keydown 直接跨過整個名字；只靠 select 事件把游標移開時，瀏覽器會先把游標畫在名字裡一幀，看得出游標先停在名字中間再跳開。標示層每次 render 都重新同步，另外靠 textarea 的 scroll 事件與 ResizeObserver 跟上沒有 render 的變化（讀者自己捲動、外框變窄）。對齊由 `visual-guards/composer-mention-highlight.ct.spec.tsx` 在正式字型、390／1280 兩寬量著；量之前要等畫完的那一幀，在 React commit 與下一幀之間量會讀到還沒同步的高度，看起來像偏了一行。⚠️ 有一條路沒有守衛：textarea 出現佔寬的捲軸時（Windows、或 macOS 設成一律顯示捲軸），`mirror()` 把右框線加寬同樣的量，讓標示層在同一個字換行。CT 的 headless Chromium 畫不出佔寬的捲軸（強制也一樣），拿掉那一行不會有任何測試失敗，改這裡要自己在有捲軸的瀏覽器看。整塊刪除透過瀏覽器的刪除指令做，Ctrl／Cmd+Z 才救得回來；直接 setValue 會清掉 textarea 的復原紀錄（CT 有一支守著）。

## 回覆卡

聊天室裡（訊息本文、放大閱讀、看原訊息、附件列、圖庫）打開的文件與請示卡裡的連結、提問／回答附件可以下註解，收件人分別是聊天室對象與開卡人（不是訊息寄件人）；契約見 overlays-and-modals.md〈附件連結〉。

RepliesPage 與 ChatReplyCard 共用 ReplyCardBody、ReplyComposer；兩者訂 reply_card，inline 卡另做單卡 refetch。answer、expire 與 waiting-pane 的 owner 動作**把寫入回應合併進畫面上那張卡**（`lib/replyCardReceipt.ts` 的 `mergeReplyCardWrite`：只取 status／answer／answeredTs／expiredTs，其餘留著讀回來的那份），不再重抓、也不再整張換掉——T-91 之後那三個寫入只回傳它自己決定的東西，題目、選項、附件與 task ref 都不在上面，整張換掉會讓 `card.task.title` 這類欄位在存檔後靜靜變空。合併後按 id 保留，直到 waiting 快照不再列出它，或 handled 快照帶著不舊於新狀態的 handled 戳記。其他卡照常採用，不能丟整份快照。refresh() 仍無條件 refetch。

請示卡回覆框的建議回覆 = 卡片任務類型（`card.task?.typeKey`）的任務手冊建議回覆 → 開卡成員（`card.from`）的成員專屬聊天室建議回覆 → `suggestedRepliesReplyCard`，相同文字只顯示在第一次出現的位置（`lib/suggestedReplies.ts` 的 `mergeSuggestedReplies`，聊天室用的同一個，收任意份清單）。`ReplyCardBody` 把 `authorId={card.from}` 與 `taskTypeKey={card.task?.typeKey}` 交給它的兩個 `ReplyComposer`（等待中、重新決定），所以聊天內嵌、請示頁、任務卡內嵌三處一致；沒有任務或自由代辦（`typeKey` 為 `""`）的卡不列任務那份，owner 或沒有專屬句子的成員開的卡不列成員那份。點句子只填回答框，不送出。同一份任務類型清單也排在另外兩個框的最前面：聊天室（`ChatArea` 的 `taskTypeKey`，OfficePage 只在外包對象的 slot 給 `workerPeer.taskTypeKey`；正職與已釋出對象給 `undefined`，因為正職可能同時有多種類型的任務）與任務卡訊息框（`TaskCard` 依 `task.typeKey`，接著 `suggestedRepliesTaskMessage`）。

ChatReplyCard 的 doReanswer 保留單卡 refetch：終態 delta 可能被刻意丟棄，拿掉會留下舊答案。不要把它和 doAnswer 對齊，也不要把「delta 是唯一 reconcile trigger」寫回規則。

## Secret 請示卡

`kind: "secret"` 的卡沒有選項：等待中時 `ReplyCardWaitingBody` 在選項的位置畫遮罩框（`-webkit-text-security`，不是 password input，因為 input 會吃掉換行），`ReplyComposer` 原樣不動。三個面的行為都在 `ReplyCardBody`，各面只接寫入。

- `SecretValueInput` 是請示卡與密鑰頁共用的遮罩輸入框：預設隱藏，眼睛切換；顯示中打字不遮回，值清空才遮回，下一個值才不會直接明文顯示。
- 送出後的卡展開時只顯示狀態（已提供、已取得、已失效）；收合列與近期已處理的時間欄照一般卡顯示已回覆或已過期，且都不帶時間，這是 owner 的裁定。收合列的字只看卡片 status，所以沒讀過卡與讀過後一致。畫面沒有撤回入口；伺服器的標為過期路由仍在，開卡成員用它提早銷毀值，Owner 與 admin agent 也呼叫得到。
- 🔴 內容只活在遮罩框元件的 state：成功清掉、失敗留著讓 owner 重送；不進 store、草稿、log 或錯誤文字，mock 也不存。
- 🔴 焦點只在 owner 打開卡時給。聊天卡的 body 只有展開後才存在；請示頁用 `secretFocusPendingRef` 記「剛打開、還沒給焦點」，只消耗一次，卡在 pane 之間搬動重新掛載不算打開；任務卡沒有打開的動作（等待中的卡一到就畫出來），所以永遠不給。
- ⚠️ 寫入收據沒有 `secret` 區塊，`mergeReplyCardWrite` 從收據推出下一個 state（空答案＝提供），期限仍要靠重讀，它決定卡還會不會變。取得與銷毀都不改 status，所以兩個 status 守衛要多問 `secretCardMayStillChange`，請示頁的 `rowVersion` 要帶 secret 的 state 與期限，否則已打開的卡停在舊狀態；用編號篩出來的卡來自一次性查詢，等待中或還可能變動時也要在 reply_card 事件時重讀，舊回應不能蓋新回應，也不能走 lookupNonce（loading 會卸載輸入框）。

## 請示卡頁的排序、任務列與稍後再看

- 請示頁一次列一份清單，由清單上方的單選標籤（`ReplyListTabs`，`role="tablist"`，`replies-tab-<view>`）決定：等待回覆（`waiting`，預設）／稍後再看（`snoozed`）／近期已回覆（`answered`＝已回覆＋已過期）。選中的標籤不存、不進 hash，每次進頁面都是等待回覆；左右鍵、Home、End 換標籤並移焦點（roving tabindex）。標籤上不印數字；標籤列下一行（`.replies__count-line`，`replies-count-line`，樣式同 `.filter-panel__summary`）印選中標籤篩選後的張數（`replies-count`，`msg.replyListedCount`），近期已回覆接著「 · 近 24 小時」（`replies-answered-window`）。近期已回覆在 `handledLoaded` 之前用 provider 的 `handledCount`（`/api/reply-cards/count` 的 answered＋expired；本頁自己回答／標過期一張等待中的卡時立刻 +1）；有篩選且清單沒載入時不印張數（server 不看篩選）。張數不知道時這一行留空但保留高度。篩選面板的四個欄位作用在選中的標籤上，「清除篩選」不動標籤。
- 卡片標題前沒有狀態標籤：近期已回覆下每張卡是已回覆或已過期，由第一行的狀態圖示說明；開卡人下面那一列 `.reply-card__handled-at`（`ReplyCardHandledRow`）是固定 label「處理時間」加絕對時間，只在已回覆／已過期的卡上出現，密鑰卡沒有這一列（owner：密鑰卡不帶時間），等待中／稍後再看沒有時間列，另外兩個標籤下卡的狀態就是標籤本身。已回覆／已過期只列 server 的 24 小時窗（張數那一行的 `replies-answered-window` 寫「近 24 小時」），**只在選了近期已回覆時才撈**；套用編號時不撈，那張卡由 `getReplyCard` 回答。
- 開著的卡自己換了清單時留在原處，直到切換標籤：別處回答或標為過期（`readCard` 讀到 status 變了 → `pinned`，帶當下的標籤），以及等待回覆／稍後再看之間被時間或別的視窗搬動（`heldOpen`：仍展開、上一輪畫在這份清單、不是這一頁自己按的延後／取消延後，`ownMoveIdsRef`）。`setView` 清掉兩者。
- 等待回覆預設依開卡時間新到舊；「顯示方式」的排序依據（`lib/replyCardSort.ts`，localStorage `oc_replies_sort`）只排等待回覆，稍後再看依回來時間、近期已回覆依處理時間，都不受它影響。任務優先序的順序取 `lib/taskPriority.ts` 的 `priorityRank`（跟任務頁同一份），沒有任務的卡排在凍結後面。
- 目前的排序依據不印在標籤旁，只在「顯示方式」面板裡；選的不是 `DEFAULT_WAITING_SORT` 時，`DisplayOptions` 在按鈕右上角畫小圓點（`display-options-changed`），按鈕的 aria-label／title 帶出目前選擇（`msg.displayOptionsChanged`）。這條規則在 `DisplayOptions` 裡，靠呼叫端傳 `defaultValue`，任務頁的分組依據同樣。面板選了就自己收起。標籤、漏斗、顯示方式永遠同一行，張數那一行在下面（電腦與手機相同）；手機上完整標籤放不下時（中文 345 以下、英文 387 以下）改用短標籤。⚠️ 標籤不要加 `transition`：切換後的那一幀會讓 `reply-desk` 的名字欄截圖比對多出 1 階的像素差而紅。量在 `visual-guards/replies-filter-panel.ct.spec.tsx`（320／360／375／390／393／1440，中英）。
- `.reply-card__waited` 在每種裝置與寬度上都是同一個固定 64px 寬的 `.reply-card__waited-pill`（沙漏＋`formatDurationShort` 的單一單位；已延後的卡是 `SnoozeActionIcon`＋`snoozeBackShort` 的 MM/DD），完整說法只放在 `title`／`aria-label`（`replyWaitedFull`／`replySnoozeBack`），畫面上沒有整句（owner：「應該要一致」）。它排在第一行右端、展開箭頭之前（有按鈕列時在按鈕列之後），寬度一樣，每張卡的膠囊右緣、電腦上的抬頭按鈕才對齊在同一個 x；已回覆／已過期卡沒有膠囊，時間在開卡人下面那一列。64px 要放得下「999d」與 MM/DD；改字型或字級要重量，守衛在 `visual-guards/reply-desk.ct.spec.tsx`。
- 任務列的優先序／期限是 server 在回應當下讀任務本身的值（`TaskRefDTO`），卡片不存。⇒ `useReplyCards` 除了 `reply_card` 也要在 `task` delta 重抓，否則改任務的優先序或期限，請示頁要等下一張卡變動才跟上。期限顏色走 `TaskDueChip`（`useDueState`，在跨過邊界的那一刻重繪，不是輪詢）。任務小框畫在第一行正下方、標題之上；外包的「目前任務」那一行永遠照畫，即使跟任務小框是同一個任務。
- 優先序／期限篩選跟開卡人軸同一套：選項與張數從選中標籤的那份清單算，每一軸的張數只套用**其他**軸；各軸彼此 AND、也跟 id AND。
- 🔴 稍後再看的卡仍是 waiting：`listReplyCards("waiting")` 照列、帶 `snoozedUntilTs`；provider 依自己的時鐘把它分到 `snoozed`，`waiting`（也就是徽章）不含它。到時間 server 什麼都不寫、不發事件，所以 provider 自己在最早的 `snoozedUntilTs` 設 timer 讓卡回來，並在 `count.waiting_next_change_ts` 重抓一次。
- 聊天室收合列的標籤：延後中的等待卡標「已延後」（`tasks.replySnoozedTag`，`.reply-tag--snoozed`），其餘等待卡標「待回覆」（`tasks.replyWaitingTag`），跟聊天室標頭徽章不數已延後同一個判準。任務卡內嵌卡（`TaskReplyCard`）的卡頭不標任何狀態（owner：「我覺得都不要顯示好了，包含已延後」）；請示頁的卡同樣沒有。判斷只讀 provider 的 `snoozed`（`useReplyCardSnoozed`），所以時間到、取消延後、回答都由 provider 那份清單帶著翻，卡片自己不設 timer、不另讀；沒有 provider 時答 false（掛裸卡的測試很多，provider 會多出讀取）。`ResumeSummaryCard` 印的是喚醒快照當下的 status 原值，不跟。
- 🔴 `snooze()` 跟 answer／expire 一樣要「握到快照同意為止」：寫入前就在飛的那支 waiting 讀取會帶著舊的 snooze 回來，直接採用會把剛延後的卡放回待回覆（取消延後則相反）。`heldSnoozeRef` 依 id 握住寫入存下的值，直到快照列出同一個值、卡不在 waiting 了、或握住的時間已過。409 跟回答一樣是「卡已經被處理」，要講清楚並 `refresh()`（延後與取消延後各用自己的句子）；其他失敗留在選單裡（取消延後則在卡上）顯示，不清掉畫面。

## 聊天室標頭的請示卡跳轉（`#replies/opener/<memberId>`）

聊天室標頭最左邊那顆（正職、外包、已釋出的對象都有，由 OfficePage 的 `repliesSlots` 接）：徽章是 `useReplyCardWaitingCountFrom`——跟 nav 徽章同一份 waiting 陣列、只數 `from` 是這位的，不另打請求。按下去到 `#replies/opener/<id>`：RepliesPage 選中等待回覆、把開卡人種成那一位、清掉其他欄位與編號（照任務頁 `#tasks/executor/<id>` 的種法），但 hash 留著，重新整理照樣是同一個畫面；畫面上的卡就是徽章數的那幾張，等待回覆的張數也一樣。之後換標籤不動 hash；「清除篩選」導回 `#replies`。route 還在時重新整理會再種一次開卡人與標籤，即使 owner 已經改過（已接受）。`#replies/card/<id>` 進來時反過來清掉開卡人、優先序與期限，查到那張卡後選中它所屬的標籤，那張卡是誰開的、什麼狀態都看得到。空清單：有欄位生效（`narrowing`）時任何標籤都印「沒有符合篩選條件」，否則印該標籤自己的空狀態（等待回覆是 ✓）。測試在 `RepliesPage.id-filter.test.tsx`、`ChatArea.header-actions.test.tsx`、`OfficePage.chat-header-replies.test.tsx`；窄寬的標頭由 `visual-guards/chat-suggested-replies.ct.spec.tsx` 量。

## 請示卡頁的 ID 篩選

欄位收在篩選列的漏斗後面，點開是「篩選請示卡」面板（電腦錨定在漏斗下方，手機是底部 sheet），形狀與規則跟任務頁同一個 `FilterPanel` collapse（見 `tasks-and-outsource.md`「任務頁的篩選與 ID 條件」與 `FilterPanel.tsx` 檔頭）。漏斗徽章數「有值的欄位數」（編號、開卡人、任務優先序、任務到期），跟 `narrowing` 同一個定義；選中的標籤不算；`#replies/opener/<id>` 種下的開卡人讓徽章是 1。`testId="replies-filter"` 以免和任務頁的控制項撞名。第一輪是常駐列但**每敲一個字元就篩一次已載入的那批卡**，owner 兩次否決（「每次都要全部都撈回來才濾不合理」），所以編號欄位一直是 Enter／blur 才提交。

**兩份狀態，不是一份**：`appliedId` 是清單真的被什麼篩，`draftId` 是欄位綁的值。**打字不篩、不發請求；Enter 或 blur 才由 `commitId()` 提交。** ⚠️ 這不是面板時代的殘留——套用 id 會打一支 `api.getReplyCard(id)`，**每按鍵提交＝每按鍵一支請求**，正是被否決兩次的那個形狀（一次獨立審查逐字寫過「一個字元一個請求」）。`commitId` 對相同值是 no-op，所以 Enter 之後再 blur 不會重打。`#replies/card/<id>` 仍然只是把 id 代進 **applied** 那一半（外加捲動 focus）。

**清除有兩個出口**：清空欄位再 Enter，或按面板底部的 `.filter-panel__clear`（只在 `narrowing` 為真時才渲染）。只有那一顆鈕，沒有「N 筆 已篩選」那串字或 chip（owner `c-2423dba8b65b`「清除篩選還是要留著」）。

## 請示卡頁的「開卡人」軸（T-118 後半，owner `c-782404ee53d8`）

多選下拉，空集合＝所有開卡人（無限制），跟任務頁三顆下拉同一個約定。

🔴 **「一個都沒勾」和「全部都勾」不是同一件事，畫面上也不准長得一樣**（owner 2026-09-06 `rc-33dfe1ff14cb`：「有勾選的時候，就不要顯示所有人…我覺得完全沒勾跟有勾的情況本來就是不同的」）。空集合是一條持續生效的「不限制」，**後來才出現的人自動被涵蓋**；全集合只是**勾選當下那批名字的快照**。兩者曾經都顯示 `allLabel`，於是踩得到這個坑：收合時把開卡人全勾（畫面說「所有開卡人」）→ 切到近期已回覆 → 新載入的開卡人沒被勾、他的卡被擋掉，而你從沒取消勾選過他。⇒ 今天只有空集合印 `allLabel`；全勾印該有的名字或「開卡人 · N」，並且 pill 會呈現 active，與「清除篩選」（讀 `narrowing`，即 `size > 0`）講同一件事。**這一格由 `MultiSelectFilter` 一份實作管兩頁**，改它同時改到任務頁。**選項清單與每個人的張數，是從「開卡人軸還沒收窄之前」那批 pane 資料算出來的**——這一格不是隨手寫的，它是這個軸能不能用的關鍵：拿已收窄的列去數，勾了某個人之後其他人的張數全變 0、從清單裡消失，就換不回來了。

**開卡人軸與 id 是 AND，不是取代**：id 指名一張卡、由 server 回答；那張卡的開卡人若不通過這個軸，正確答案是一般的「篩完是空的」，不是把卡顯示出來。

🔴 **它做在前端，這是 owner 選的，不是漏做。** owner 曾說「都不可以在前端做篩選，要透過後端API篩選」（`c-b3f5a7fe2431`）；被說明「server 只回選中那個人的卡 ⇒ 選項清單只剩那個人」之後，他改口「那我們先跟任務用同樣的做法就好」（`c-7e4374094273`），並裁定不另開票（`c-1cd7f4d96d0d`）。⇒ 要搬到後端，得先解掉選項清單怎麼來的問題，不是一行改動。

🔴 **id 是跟 server 要的，不是拿已載入的列去比對**（owner 2026-09-06 選了第①案）。套用 id 時打一支 `api.getReplyCard(id)`，**三種結局在畫面上必須長得不一樣**，這正是這張票存在的理由——這一頁只握著待回覆卡與 24 小時內的已處理卡，第一輪於是把「這張卡更舊，沒載進來」講成跟「這個編號不存在」一模一樣的一句話，owner 在驗收時就被這個併句騙過去：

1. **找到** ⇒ 顯示那一張，**即使它不在任何一個 pane 裡**（三天前回過的卡也找得到），而且 24 小時的窗**不套用在它身上**。
2. **404** ⇒ `replies.lookupMissing`：說明白這是**跟伺服器要過**的結果，不是還沒載進來。
3. **其他失敗（500／離線）** ⇒ `replies.lookupFailed`，**不准說「找不到」**——沒問到伺服器就沒有資格對存不存在下判斷。

🔴 **「找到了但畫面上沒有」和假的空是同一種沉默**：查到卡的那一刻（每個 id 一次，`autoViewForRef`）自動選中它所屬的標籤，直接畫出撈回來的那張（不等 `handledLoaded`）；它所屬標籤的張數是 1、其他是 0。之後 owner 自己切到別的標籤，那裡是「沒有符合篩選條件的請示」；卡在顯示中自己換了狀態（例如在別處被回答）則留在原標籤，不把頁面拖去別的標籤。

空狀態的句子**不可合併**：沒有篩選時是各標籤自己的那句（等待回覆是「✓ 目前沒有待處理的請示」），篩選中是「沒有符合篩選條件的請示」，另外兩句是上面的 404 與失敗。

⚠️ 第一輪那台「篩選中順便把已處理載進來、一次 visit 只載一次」的機關（`handledAutoloadTried`）**已經刪掉**：卡從 server 來，能不能找到不再取決於哪個 pane 剛好載過。不要因為註解寫得仔細就把它搬回來。

⚠️ 動作（answer／reanswer／expire）成功後要 bump `lookupNonce` 重讀那一張，否則按 id 撈回來的快照會留著舊狀態。

每一格都有一支測試釘著（`RepliesPage.id-filter.test.tsx`）。

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

class 名 `.reply-tag--ai` / `.reply-option--ai` **不要改**:`ReplyCardBody` 的 AI 建議標籤與
`reply-multi-select.ct.spec.tsx` 都用這個名字找它。

`GET /api/reply-cards` 只回一種形狀 —— 輕量列（`ReplyCardRow`），`?view=full` 已經移除（owner 2026-09-07）。**pane 的卡預設收合，點開才打單張 `getReplyCard`**，跟聊天串內嵌卡（`ChatReplyCard`）同一套做法；再點一次會收合，收合會把讀回來的卡丟掉，所以重開是一次新的讀。🔴 **例外已經沒有了**：待回覆第一張自己打開那套（`autoOpenOffRef`）被 owner 2026-09-11「預設全部折疊」推翻，連同 `rc-cd351785b83d`／`rc-fa7e4c9bce42` 的行為一起刪掉；進頁面時**一支 `getReplyCard` 都不會發**，這件事由 `RepliesPage.id-filter.test.tsx` 的兩支「arrival must read no card at all」守著。今天仍會自己打開的只有**深連結指名的那一張**（`#replies/card/<id>`）——那是明確要求，不是預設。🔴 **折疊列＝卡自己的抬頭，不是另一套版面**（owner 2026-09-11「折疊起來那一列的內容不要另外設計」）：第一行（狀態圖示、自己的編號 chip、跳到原訊息、稍後再看、標為過期、複製整張卡片內容、`已等你 <時長>` 膠囊、展開箭頭），卡有任務時第一行正下方是任務小框，再來是請示卡標題與開卡人列。這些全部來自輕量列；點開只是在下面**補上問題內容**（body、附件、選項），抬頭不重畫、標題不重印。⚠️ **`跳到原訊息` 是例外**：輕量列沒有 `chatMessageId`（wire 的 `ReplyCardListItemDTO` 也沒有），所以那顆按鈕在還沒讀過的卡上是**按下去才讀**一次再導頁——不是 render 時讀。🔴 **卡頭沒有絕對時間**（owner 2026-09-11「我不想知道絕對時間 相對時間已經夠了」）：`data-testid="opened-at"` 這個節點不存在了，e2e `15_card_waited_time.spec.js` 反過來守它不可以回來；絕對時間只剩已回覆／已過期卡的 `已回覆／已過期 <時間>`。⚠️ **整張卡是開關**（`role="button"` ＋ `onCardToggleClick` 的互動過濾），做法照抄 `TaskCard`；卡內任何 button/a/input/`[role=button]` 的點擊都不翻卡。測試要點開卡就點 article 本身（`[data-reply-card-id][aria-expanded]`）。

🔴 **抬頭上那一行「這個外包正在做哪張任務」是 WORKER 的，不是卡的**（T-196，owner `rc-dce285c5274c`：「我覺得只在 UI 上補上顯示就好 就像在 chat 那邊 使用者列表上 outsource worker 會顯示他在進行的工作是哪一個」）。它是開卡人下面的「目前任務」列（`.card-meta` 裡的 `.card-meta__row.reply-card__worker-task`，值是任務那一行加標題，label 對齊任務那一行），用的是辦公室左欄外包列**自己的兩個元件**：`OutsourceTaskLine`（可點的任務編號 chip → `#tasks/<id>`，加任務類型）與 `CurrentTaskTitle`（真實標題、兩行截斷、hover 全文、沒有任務時的灰字空狀態）。**不要為這一面另寫一份**——那兩個元件的檔頭注解逐字寫著它們存在的理由就是「兩個 surface 不能漂移」，再加一份就是這個 repo 那個「同一件事在外包與正職各寫一份」的缺陷族。

⚠️ **它是唯一不來自輕量列的抬頭內容**，所以上面那句「全部來自輕量列」對它不成立：資料來自 `useWorkerCurrentTasks`，也就是 `useWorkerCodenames` 那份 per-id identity cache（`GET /api/members/{id}`）——**同一支讀取**，代號與頭像本來就走它，不是為了任務多打一支。選它而不選 `useOutsourceWorkers` 清單是因為**清單會跳過已釋出的外包**（`api_members.go` 的列表端明確跳過 `roster_status='removed'`），而這一頁滿是已釋出的開卡人；用清單會讓同一列出現「代號有、任務沒有」的兩套說法。

⚠️ **只有這個 accessor 訂 SSE（`task` / `member`），另外兩個不訂，這是刻意的**（T-197 起是 `member`：`outsource_worker` 這個 topic 已從 closed set 移除，聽它等於什麼都聽不到，而且不會有任何錯誤）：代號與頭像開著頁面不會變，當前任務會變，不重讀的話那一行是載入當下的舊事實，而畫面上沒有任何東西講得出來。它**不照 batch 的 ids 收窄**——`task` delta 名的是任務、這份 cache 以 worker 為鍵，拿 worker id 去比會對每一次任務事件答「都不是我的」，那一行就永遠不更新。重讀失敗時**保留上一列**：讀失敗不是任務不見了的證據。`kind === "outsource"` 以外的開卡人這一行**什麼都不畫**（`MemberDTO` 根本沒有任務欄位）；worker id 沒解析出來時**也什麼都不畫**——沉默是誠實的，畫空狀態等於用沒有的證據宣稱「它沒有任務」。

🔴 **這一行只在成員 `status` 是 `assigned` 或 `active` 時才畫 —— 白名單，不是把終態列出來。** 列終態是預測：server 之後多一個終態，它就會以「還活著」的樣子出現在這一面。已釋出（released）的外包因此什麼都不畫，而這一格是獨立審查打穿過的。 釋出只翻 status、**`task_id` 原封不動**（`dal_tasks.go` 的 `ReleaseWorkerByID`），而 per-id 讀取又完整服務已釋出的列——也就是這一頁能解析它們的原因。所以那一列是「它做完的那張票」的真實紀錄，同時是「它現在在做什麼」的假話，照畫會配一顆活的可點 chip。左欄不會犯這個錯，因為**清單端點直接跳過已釋出的**；這一面沒有那道過濾，就得自己讀 `status`。**不要改成顯示「已釋出」**：那句話已經有家（聊天室橫幅），i18n 檔自己的註解明令不要複製第二份。

⚠️ **沒有任務時，整條 `OutsourceTaskLine` 不畫，只留空狀態那一行；判準是 `taskNo` 不是 `taskId`。** server 無條件寫 `task_id`，但 `task_no`／`task_title`／`task_type_*` 只有在它解析得到那張票時才填，所以拿 `taskId` 當判準會讓「票解析不到」那條路原地復活同一個矛盾。 那個元件的 type 欄位是**無條件**渲染的，全空時 fallback 成 `自由代辦`——在左欄為真（列出來的外包一定有票，那句話的意思是「這張票沒有類型」），在這一面是假的，會變成同一列同時說「自由代辦」和「無當前任務」。第一版的測試 fixture 剛好留著 `taskTypeName`（一個 server 產不出來的狀態），把這個洞蓋住了。

⚠️ **SSE 重讀是「合併成一輪補跑」，不是「丟掉」。** 一輪還在飛的時候又來一批 delta，那一輪的請求是在 delta **之前**送出的，帶不回它報的改變；只讓那一輪跑完就等於把新狀態丟掉，要等下一批**不相干的** delta 才會修正 —— 而安靜的工作室裡，螢幕上那個外包被釋出很可能就是那一小時的最後一個事件，「下一批」等於沒有。所以被超車的那一輪結束後再跑一次，就一次。

釘住的是 `RepliesPage.worker-task.test.tsx`、`useWorkerCodenames.test.ts` 與 `visual-guards/reply-card-worker-task.ct.spec.tsx`（截斷與 hover 全文只有真瀏覽器量得到；那支 spec 的檔頭記了哪兩顆 mutant 會紅、以及哪一顆**不會**）。

輕量列只帶 title/status/時間戳/task ref —— 不要把它補成 `ReplyCard`（body/options/chat message id 在單張才有，補成 "" 會畫出一張問題不見了的卡而且不會丟錯）。等待卡的 expire 規則以 server 為準：owner/admin 或卡片作者可過期自己的 waiting 卡；其他人 403，已回答 409。

hash route #office/chat/<id>/msg/<msgId> 只做一次定位與 highlight。產生它的是「請示」頁的**跳到原訊息**、任務卡內嵌回覆卡卡頭的**跳到原訊息**、「看原訊息」覆蓋層上的**跳到原訊息**，以及聊天室搜尋（搜尋列的 ▲／▼ 與結果清單）（外加使用者自己留存的舊 URL）。ChatArea 內一律經 `jumpToMessage` 產生：路由已經指著同一則時它改呼叫 `retryJump` 重新定位，因為同一個 hash 不會觸發路由變化；聊天氣泡引用列的**看原訊息**本身不走這條，它撈那一則開覆蓋層（見下方「看原訊息」一節）。⚠️ T-0b78 曾把那兩顆也改成覆蓋層，owner 2026-08-29 裁定「1 跟 2 變回去原本那樣」—— 所以不要順手把它們改回覆蓋層。

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

回覆卡的 red badge 與聊天未讀互不清除；任務關聯卡共用卡身，聊天室的卡只顯示任務編號 chip（點了到任務）與任務標題。

## 「回覆這則」（T-4e95，owner 2026-08-21 改設計）

**引用內容由 server 隨每次讀取現組，前端只讀不找。** 每則 `reply_to` 非空的
訊息，server 會在**每一個**讀取出口（listing、history page、`?ids=`、POST 回
應、wake snapshot）附上
`reply_to_chat = {id, from, from_name, to, to_name, content}`（「回覆這段」另帶 `reply_excerpt`）。前端
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

**但「一起改」不等於「長一樣」。** 快照卡在比聊天 pane 更窄的詳情面板裡，聊天面那一行
截短一行名字的版面搬過去會把名字切掉大半；它也沒有 `openQuotedMessage` 那套
overlay 與再撈，而它本身已經是一張有邊框的卡。
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

**被引訊息已刪除（deleted）時，server 給的快照帶 `deleted: true`、`content` 與 `reply_excerpt` 都是空的**：
這仍是「server 給了快照」那一態，畫名字＋固定的 `chat.deletedQuote`「已刪除的訊息」，
「看原訊息」照給（owner 讀得回原文）。它跟 attachment-only 的空 `content` 是兩件事，
不可互相折疊；每一個引用面（訊息列、`ResumeSummaryCard`）都要分辨。

**owner 自己那則已刪除的訊息，列上只畫 `chat.deletedPlaceholder`「此訊息已刪除」，不畫任何原文或附件**；
點它跟「看原訊息」走同一個 `quotedMessage.open(id)`、開同一片覆蓋層（owner 裁定：不要在列上展開）。
不要為這一列另寫一份展開或另一個 overlay。
server 的聊天列表預設不列已刪除訊息（任何呼叫者都一樣）；聊天視窗的讀取（`listChat`／`listChatWindow`）在 http seam 一律帶 `include_deleted=true` 才拿得到這些列，未讀探針（`unread=true`）不能帶，server 會 422。
刪除時 server 寄給原收件人的 system 通知（meta 帶 `delete_notice`）在 owner 的聊天視窗不顯示：`listChat`／`listChatWindow` 在 http seam 一律帶 `hide_delete_notices=true`（Owner rc-432acb8e2706），未讀探針同樣不能帶（422）。聊天列表不帶參數時照樣列出通知，因為成員要靠它；resume summary 預設就不列（要看帶 `include_delete_notices=true`），所以 `ResumeSummaryCard` 不帶參數。mock 的對應讀取一律不列通知，測試用 `__mockDeleteNotices()` 看它有沒有寄出。

**每則訊息的動作（回覆這則・複製訊息・放大閱讀／刪除）桌面是一條浮在氣泡上緣的工具列，觸控是長按選單**（owner 2026-11-24：「不然試試看hover好了，slack好像就是這樣做的」；手機「都用長按好了，我看line也是這樣」）。氣泡**不預留**任何寬度，文字用滿整個氣泡；工具列 hover 或**鍵盤**聚焦（`:has(:focus-visible)`，不是 `:focus-within`：滑鼠點按鈕、關掉彈窗都會把焦點留在按鈕上）才出現（opacity，不改版面），自己的訊息也一樣，不再常駐；複製成功時只有複製鈕本身變成 ✓，旁邊不另畫任何東西，「已複製」只放在 `ChatMessageCopyStatus`（一直掛著的 role=status）裡的 `.chat__sr-text` 給螢幕閱讀器，滑鼠離開後工具列照常收起；複製失敗時 `ChatMessageCopyStatus` 顯示 `messageCopyFailed`（`.chat__msg-copy-status--failed`），只有它會讓工具列在滑鼠離開後留著、只剩那個標籤。對方訊息的工具列用一份隱形的名字（`data-sender`）墊在前面，不蓋寄件者名字。手指長按 500ms（任何裝置都算，`pointerType === "touch"`；移動超過 10px 就當成捲動）開 `ChatMessagePressMenu`：從畫面下方升起的選單（owner：開在訊息旁邊的選單位置會跳），按著的訊息加外框；焦點一打開就落在第一項，但項目本身 `outline: none`，在選單裡按過鍵（`.chat__msg-sheet--keyed`）才畫 accent 外框與底色（owner：「手機長按出現的選單 有時候有藍匡」——手指打開後的程式聚焦，瀏覽器有時當成 `:focus-visible` 畫出預設藍框）；手指放開時瀏覽器補送的 click 會落在背景或選項上，所以只有在背景／選單裡按下去的那一下才算。項目跟工具列同一份清單（`ChatArea` 的 `messageActions`）再加「選取文字」；選單裡按「複製」，選單收起就是回饋，成功時畫面上不出現任何東西（跟 iOS 原生長按選單一樣），失敗才在那則訊息上顯示 `messageCopyFailed`；只能觸控（`hover: none`）的裝置看不到工具列，但按鈕留在無障礙樹裡、鍵盤聚焦時出現。「選取文字」進入那一則的**選取模式**（owner「我想像中是進入選取模式，像line一樣」）：其餘畫面模糊、`pointer-events: none`，點模糊處（一次 click，不是 pointerdown，捲動不算）離開；模式只做選字・複製・回覆（owner：「收合 標為過期這些都不是在這個介面做的事情」）：那一列裡的連結、提及、附件、圖片、引用列的「看原訊息」等動作，`ChatArea` 在 document 的 capture 階段把 click／auxclick／Enter・空白鍵吞掉（preventDefault＋stopPropagation，先於 React 的 listener），字照樣選得到；那一列的 `a`、`img` 另設 `-webkit-touch-callout: none`（iOS 長按連結／圖片的預覽會提供開啟；Chromium 與 Playwright WebKit 都不認這個屬性，CT 量不到；選字的拷貝選單是否照常出現也要實機看）；離開模式後這些 listener 都拔掉，連結、圖片、看原訊息照常動作；請示卡本身不在這份清單裡，由 `ChatReplyCard` 的 `selecting` 讓點擊與 Enter／空白鍵不收合／展開（空白鍵仍 preventDefault，不捲動聊天串）；在模式裡才掛上的卡也一樣展開。請示卡進模式時記下原本的收合／展開並展開，離開（取消・Esc・點模糊處）時回到原狀，只有列上的「回覆」引用進卡的回答框時（`keepOpenAfterSelecting`）保持展開；其餘畫面同時設 `inert`（含 `.chat__jump-latest`／`.chat__new-msg`，effect 依 `bottomAffordance` 重跑）；模式期間 `.chat__composer` 設 `hidden`（不卸載，草稿照舊；列上的回覆在同一次點按裡先 `flushSync` 讓輸入區回來、再把焦點放進輸入框，iOS 才會叫出鍵盤；取消／Esc 把焦點還給那則訊息的第一個動作；owner：「下面的部分，應該只需要我們新增的部分」），`ChatSelectionModeBar`（同一個 `useSelectionPick`）在聊天室最底給取消・複製（選取的那段原文）・回覆，沒有選取時兩顆停用；三顆按鈕用 `chat.selectionCancel`／`copySelection`／`selectionReply`（取消／複製／回覆），節慶主題不覆寫這三個（owner rc-2f10b36c69a7）；選取用 `pickInSelectedRow`：那一列內容裡選到的字都算 excerpt（一般訊息的正文；請示卡的標題與內文），選到寄件者、被引用的訊息等內容以外的字時複製與回覆都停用（看不到或 `user-select: none` 的字不算）；複製的字與 excerpt 同一份，都經 `excerptFromSelection` 把換行連同前後空白收成一個換行（瀏覽器在區塊間塞空行，引用框只顯示三行；伺服器那半 `normalizeReplyExcerpt` 仍只 trim），請示卡在模式裡只列標題與內文（owner：選項不能回覆就不要列出來）：選項與回答區（包在 `.reply-card__answer-side`，`display: contents`）、任務列 `.reply-card__task`、標為過期 `.reply-card__icon-btn`、問題附件 `.reply-card__question-atts`、錯誤訊息 `.reply-card__error`、展開卡 meta 下的分隔線 `.reply-card__divider` 都 `display: none`（不卸載，回答草稿與引用目標都在；`.reply-card__chat-head` 只剩任務列與標為過期時整列一起隱藏；沒有任務的卡標題在卡頭裡，卡頭留著），所以從標題／內文拖進任務列的位置或整張卡全選，複製與回覆照樣可用、excerpt 只有標題與內文；列上的回覆先 `flushSync` 結束模式再引用，請示卡的回答框才拿得到焦點；沒有提示行，複製成功時複製鈕只畫 ✓（`CopiedMark`，字放在 `.chat__sr-text` 給螢幕閱讀器），失敗顯示 `messageCopyFailed`，另有一個看不見、不佔位的 role=status；手機上輸入欄位至少 16px 由 `styles/global.css` 一條全域規則負責（見 css-layout-traps.md「輸入欄位 16px」），聊天輸入框不再各自宣告；列是疊在 `.chat__body` 底部的 absolute，它把自己蓋住的高度寫進 `.chat__body` 的 `--chat-select-bar-reserve`（模式期間當 `.chat__body` 的 padding-bottom），聊天串這個捲動容器因此**在列的上緣就結束**，內容與 iOS 的選取把手都不會跑到列底下（以前是在聊天串裡墊 padding／scroll-padding、列疊在捲動區上：owner 實機在長訊息的末段選字後按不到「複製」——「好像因為跟訊息顯示重疊了 往上拉就可以了」，iOS 選取把手的觸控範圍蓋到列上的按鈕；不要改回疊在捲動區上）；選取範圍的結尾（`endRectOf`）落在列上緣 48px（`SELECTION_END_CLEARANCE_PX`）以內或底下時，selectionchange 停 120ms 後把聊天串往上捲到離列 48px（不在拖動把手的每一步跟著捲，否則會在手指下一路捲走；進模式時整則選起的那個選取不算，進入的捲動位置照舊；把手的實際範圍只能實機看），進入時先還原進入前的 scrollTop（輸入區收起時瀏覽器會先夾住它，所以在 render 時就讀），再在那一列長大時（請示卡進模式後才展開）把那一列捲到列的上方，已經看得到就不動；那一列的頂端已經在聊天串上緣之上（長訊息從中間長按進來、或讀者已捲進那則裡）時也不動，只往下捲、不往上捲（以前會在任何 resize——轉向、圖片載入、請示卡重讀——時把聊天串拉回那則的頂端，從中間長按進來時按著的那行還會跑到列底下）；離開時輸入區回來、聊天串變矮，`ChatArea` 再把那一列捲回看得到的地方；已回覆／已過期的卡選得到、複製得了，但 `reply` 是 null，回覆停用。CI 的 runner 可能開著減少動態效果，CT 用 CDP 把 `prefers-reduced-motion`／`prefers-reduced-transparency` 釘住再量模糊與淡化。手指不經長按選到字時（同時有滑鼠與觸控螢幕的裝置，從訊息以外的字拖進來；`ChatSelectionReply` 記下最後一次 pointerdown 的 pointerType），浮動的「回覆這段」固定在畫面最下方（iOS／Android 的選字工具列在選取範圍旁邊，會蓋住它）；聊天室與請示頁的訊息、卡片在 `hover: none` 上本來就不能用瀏覽器自己的長按選字，所以那裡的手指一律走選取模式。氣泡與卡片在 `hover: none` 上、以及手指按著訊息串（`data-touch-press`）時關掉瀏覽器自己的長按選字／iOS callout，選取模式只對那一則打開選字；回覆、取消、Esc 或點模糊處就結束。請示卡那一列也是同一條工具列／同一個選單（回覆・複製），複製的是問題：summary、空一行、body 原文（提及轉 `@名字`），按下去才讀那張卡（收合列不帶 body，掛載時一律不讀）。「刪除」出不出現仍只看 `chatDeletable`。CT `chat-reply-to` 量文字寬、hover 不擠動、工具列不蓋字、對比、觸控長按與選取。

**請示頁的卡片用同一套**（owner `rc-7cffa413b846`：請示頁的卡要跟聊天室的請示卡一樣）。沒有第二份實作：`RepliesPage` 在 `.replies` 上掛同一個 `useLongPress`，按到哪張卡由 `pressedReplyCard`（`ChatMessagePressMenu.tsx`，聊天室也用它）判斷，選單是 `ChatMessagePressMenu`，複製走 `useMessageCopy`＋`replyCardQuestionText`（`ChatMessageCopyButton.tsx`，按下去才讀卡，同一條 ClipboardItem 路），選取模式是 `ChatSelectionModeBar`（`rowSelector=".reply-card--selecting"`、`placement="screen"`）＋`guardSelectionMode`（document capture 的 click／auxclick／Enter・空白鍵；卡片 article 本身就是會收合的 role=button，所以它的 click 也一起吞，聊天室則用 `except` 留給 `ChatReplyCard` 自己忽略）。桌面的複製是卡頭按鈕列最前面的 `ChatMessageCopyButton`＋`ChatMessageCopyStatus`（`.reply-card__copy`），一直顯示、不做 hover 工具列，跟這一頁卡頭其他常駐的按鈕一樣。選單依序是跳到原訊息，等待中的卡接著稍後再看（已延後的卡是取消延後）・標為過期，最後複製訊息・選取文字（`pressMenuItems`；這個順序是 owner 在試用站要的，跟聊天室選單把複製放前面不同，不要為了一致改回去）：只能觸控（`hover: none`）的裝置卡頭不畫按鈕列，這個選單是那些動作唯一的入口，所以卡頭有的它都要有，而且每一項呼叫卡頭按鈕同一個處理（稍後再看經 `ReplySnoozeMenu` 的 `openRequested`（只亮一次 render 的脈衝，頁面在下一個 effect 清掉）打開同一份選單，一律以底部選單呈現，因為觸發鈕看不見，關閉時焦點回到卡片；標為過期走同一個 `ConfirmModal`）；圖示與用詞跟卡頭同一份（`replyCardActionIcons.tsx`、`t.replies.*`）；🔴 **不要為了跟聊天室對齊把「回覆這則」加回來**（owner `rc-a460204f1b6e`：「請示卡頁面好像不應該有回覆整則的功能」，卡片自己的回答框就在這一頁上）；聊天室的選單照舊有回覆這則。選取模式列上的「回覆」照舊，把選取的那段放進卡片的回答框。模式：進入時記下卡片原本開或關並打開，離開回到原狀，只有列上的回覆保持打開；其餘畫面由 `inertAround` 從那張卡往上到 `.replies`、把每一層的兄弟設 `inert`（每次 render 重算，清單在模式中刷新也蓋得到；卡片在一次 render 裡找不到了——例如在別處被回答、清單先少了它——就結束模式）；`guardSelectionMode` 收的是依 id 查卡的函式，卡片換段重新掛載後點卡內不會被當成點外面；模式中 article 拿掉 `role="button"`／`aria-expanded`／`aria-label` 與 onClick，不再是一顆什麼都不做的收合鈕，模糊沿用聊天室那組規則（`.replies--selecting [inert]`）；列是 fixed 在畫面最下方，`ChatSelectionModeBar` 把它蓋住的高度寫到捲動容器的父層（聊天室是 `.chat__body`、請示頁是 `.app__main`）的 `--chat-select-bar-reserve`，`.app__main:has(> .replies--selecting)` 用它當 padding-bottom，`.replies` 因此在列的上緣就結束。卡片在模式裡隱藏的東西跟聊天室同一條規則（`:is(.chat__msg--selecting, .reply-card--selecting)`），這一頁多藏整個卡頭 `.reply-card__head`（頭像、發問者、外包的當前任務、已等你、按鈕列）與讀取中的提示 `.reply-card__hint`，卡片只剩標題與內容，整張全選也複製與回覆得了（owner c-c839cc6b52e0：選取模式只做選字・複製・回覆；卡頭的字留著的話不算內容，選到就把兩顆鈕停用）；回答區包在 `.reply-card__answer-side` 裡。`hover: none` 上卡頭的按鈕列移出版面（absolute 在第一行右端），按鈕本身裁成 1px、不畫，跟聊天室工具列一樣留在無障礙樹裡，鍵盤聚焦（`:has(:focus-visible)`）時整列回到版面；從選單複製失敗的「複製失敗」照樣畫在卡頭右端、跟時間戳垂直置中（按鈕列的外框是卡頭的全高；卡頭只有一行），顯示的那一兩秒蓋住時間戳，這是接受的樣子：它不在版面流裡，時間戳與名字欄都不動；鍵盤叫出的按鈕是 `hover: none` 的 26px。取消／Esc 把焦點還給卡頭的複製鈕（Esc 之後鍵盤焦點會讓按鈕列出現）。⚠️ `useLongPress` 吞掉長按後手指放開補送的那一下 click，旗標要等下一次 pointerdown 傳到容器才清掉；選單在容器的 React 樹外，點選單不會清它。所以它只吞 `detail` 不為 0 的 click：鍵盤 Enter／空白鍵、讀屏的點按（`detail === 0`）一律放行，否則長按後第一次用鍵盤按卡上或從選單打開的東西（稍後再看選單、標為過期確認框；聊天室訊息的按鈕也一樣）會被吃掉。不要改成在選單關閉時清旗標：選單也會因捲動而關，手指還按著時被清掉，放開那一下就會翻卡或打開手指下的連結。CT `reply-desk` 量 1440 滑鼠與 320／390 觸控。

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
覆蓋層畫正文，下面接那則的附件（同一個 `AttachmentStrip`、同一組聊天氣泡的 class）；正文空白時只畫附件，不畫空的文字區（owner 裁定）。
這一段是刻意合成的：以前它問「那則在不在視窗」來決定給不給鈕，owner 把它改成一律
給、按了才撈，所以**不要再把窗口成員資格的判斷加回去**。
引用列上的「看原訊息」**沒有看得見的圖示或字，整列引用就是點擊目標**（owner `rc-4f3663c42554`；
圖示版本會被看成訊息工具列的一部分）：`.chat__msg-quote__jump` 仍是一顆真的
`<button>`（`data-testid="msg-quote-jump"` 不變），內容是空的、透明、`position: absolute; inset: 0`
蓋滿整個 `.chat__msg-quote`。「看原訊息」這句只留在 `aria-label` 與 `title`。hover 時的淡底
（`--chat-bubble-ink` 的 12%）由引用列自己透過 `:has()` 畫在字的**下面**，不是畫在覆蓋鈕上；
focus-visible 的外框畫在覆蓋鈕上，正好框住整列。淡底需要的留白用負 margin 加等量 padding 借來，
靜止時字的位置不動；分隔線改成 `::after` 畫，寬度仍跟字一樣。`quotedMessage.failureNotice`
的「拿不到這則訊息」留在第一行，`position: relative; z-index: 1` 疊在覆蓋鈕上面。原訊息已不存在
（`quoted` 為空）時沒有這顆鈕，整列不可點。CT `chat-reply-to` 量著：覆蓋鈕的框＝引用列的框、
不碰工具列（工具列按鈕中心點到的仍是那顆按鈕）、靜止透明 hover 有底、失敗訊息不被蓋住。

**覆蓋層上的「跳到原訊息」（T-684，owner `rc-9194339a2f84`、`rc-38583da201d3`）。** 一律先開覆蓋層顯示原文，
跨聊天室也一樣；覆蓋層 header（✕ 左邊）才有跳到原訊息，按下關掉覆蓋層、寫上面那條 hash route。它跟請示卡的跳到原訊息同一個圖示（`JumpToMessageIcon`）與同一個用語（`replies.jumpToChat`，放在 `aria-label`／`title`），外框照覆蓋層 header 其他圖示鈕（owner `rc-94eeef1b229c`：要認得出是同一個動作）。

請示卡的卡頭動作（`RepliesPage` 的 `renderHead`、`ReplySnoozeMenu` 的觸發鈕、聊天室卡的標為過期、任務卡內嵌回覆卡 `TaskReplyCard` 的跳到原訊息）是沒有外框的圖示鈕 `.reply-card__icon-btn`，畫法跟旁邊的複製鈕（`.chat__msg-copy`）一樣，名字只在 `aria-label` 與 `title`（用 `t.replies.*`，節慶主題的用詞照樣出現在提示裡）。同一個功能全站同一個圖示（owner）：跳到原訊息・稍後再看・取消延後・標為過期的圖示只從 `replyCardActionIcons.tsx` 拿（目前是 `ExternalLinkIcon`、`ClockIcon`、`UnsnoozeIcon`、`ExpireIcon`），各面不直接 import `icons.tsx` 的那幾個；要換圖示只改那一個檔。複製是 `CopyIcon`。⚠️ 標為過期不要用 `CloseIcon`（全站是關閉／清除）或 `HourglassIcon`（成員在等待）。`.reply-card__jump` 仍是文字鈕（稍後再看底部選單的取消），不要拿它改成圖示樣式。`ExpireIcon` 畫的是封存箱。`.reply-card__icon-btn` 與卡頭的複製鈕 22px（按鈕列 gap 在電腦上 10px、圖示中心距離 32px，其他 4px），`hover: none` 上 26px（聊天室卡的標為過期；請示頁卡頭在觸控上不畫按鈕，見上一段）；尺寸與間距由 reply-desk、chat-card-timestamp-align CT 量。第一行（`.reply-card__head`）：狀態圖示（等待中＝人加時鐘、已延後＝時鐘、已回覆＝圈勾、已過期＝封存箱，`ReplyCardStatusIcon`）、自己的編號 chip（`ReplyCardIdChip`，`rc-` 加 6 碼，點了複製完整編號），右端（`.reply-card__head-end`）是按鈕列（只有用滑鼠時畫出來）、膠囊（只有等待中與已延後）、展開箭頭。右端放不下時整組換到下一行、仍靠右，不拆開；觸控上沒有按鈕，右端跟編號同一行。複製卡片內容的按鈕與長按選單項目叫「複製整張卡片內容」（`replies.copyWholeCard`），跟編號 chip 的複製分得開。開卡人列（`.card-meta`，label/value 格線，跟任務卡、金鑰卡、檔案庫卡、傳承卡同一份 `card-meta.css`：label 欄寬是這張卡最寬的 label、同一個顏色與透明度、跟值垂直置中，見 css-layout-traps.md〈卡片的 label/value 列〉）是一顆 chip（`ReplyCardOpenerChip`：小頭像、名字、角色，點了開成員或外包詳情）；名字與角色依各自寬度比例讓出寬度，各自以「…」截斷並帶 `title`（角色不能先縮到零：那樣畫不出「…」，看起來像沒有角色）。外包任務的第一行（`.outsource-row__task-line`：任務編號 chip＋`.outsource-row__type`）在左欄是 flex 列；放進開卡人列時 `replies.css` 把那一行改成一行文字（block＋`ellipsis`，chip 與類型 inline），放不下的部分收成一個「…」：先截類型的字，再把整個 chip 換成「…」；左欄的樣式不動。手機上外包的任務標題（`.current-task-title`）一行、`ellipsis`，電腦上兩行截斷。`.reply-card__copy` 的「複製失敗」在觸控上 absolute 疊在第一行右端（膠囊上），不撐寬也不推動編號。量在 `visual-guards/reply-desk.ct.spec.tsx` 的 `expectHeadLine`（320／390 觸控與滑鼠、640／720 觸控、700 滑鼠、721 觸控、1440 滑鼠，另有 1024 觸控；短名字、長名字、長角色、外包長任務編號＋長類型＋長標題，等待中、已延後、已回覆、複製失敗）：狀態圖示與編號同一行、右端在第一行右緣、展開箭頭最後且各卡同一 x、膠囊 64px 且各卡右緣同一 x、觸控上沒有畫出任何按鈕、開卡人 chip 與外包任務在卡內、截斷的行要真的畫出「…」並有全文的 `title`；任務小框同樣量。

任務小框（`ReplyCardTaskBox`，請示頁與聊天室卡同一個元件）在第一行正下方、淡底色：第一行是任務編號 chip 加優先序，第二行是任務標題（一行、以「…」截斷、帶 `title`），有期限時第三行是「到期」加日期與剩餘時間（`TaskDueChip`，在框裡畫成可換行的文字，不是 chip，320 寬才放得下完整日期與已逾期）。編號 chip 是框裡唯一的按鈕：跟外包列的任務 chip 是同一個元件（`TaskChips.tsx` 的 `TaskIdChip`，`.outsource-row__chip--task`，左邊是跳轉圖示），點它到 `#tasks/<id>`，名字在 `aria-label`／`title`（`t.replies.openTask`），click 與 keydown 都 stopPropagation，所以不會順便開合卡片；框本身不能點、沒有 › 箭頭。⚠️ 任務狀態圖示沒有畫：`TaskRefDTO` 沒有任務狀態，要畫得先改 wire。量在 `reply-desk.ct.spec.tsx` 的 `expectTaskRow`：框在第一行下方、標題上方、卡內，chip 是唯一的按鈕且帶跳轉圖示，優先序跟 chip 同一行，標題一行在 chip 下面，期限一行在標題下面。聊天室內嵌卡展開後是同一個第一行（只有標為過期一顆按鈕）、同一個任務小框、標題，沒有開卡人列（聊天室就是開卡人的）；已回覆／已過期一樣有「處理時間」那一列（密鑰卡沒有）。收合時仍是 T-48 那一列。任務卡步驟裡的內嵌卡（`TaskReplyCard`）展開時也是同一套：第一行（狀態圖示、編號 chip、跳到原訊息；已回覆／已過期另有收回一行的箭頭鈕 `task-reply-card-collapse`）、標題、開卡人列、已回覆／已過期的「處理時間」列（密鑰卡沒有）、分隔線，再來才是內文、附件、選項與回答框；沒有任務小框（卡就在那張任務裡）。開卡人的名字由 `TaskCard` 傳進來的 `members`／`workers` 解析，兩份都沒有的才用 `useWorkerCodenames` 逐筆讀。已回覆／已過期收合成一行、等待中的卡展開掛上但不搶焦點，這些行為不變。展開的卡（請示頁與聊天室）在 meta 最後一列（開卡人或處理時間）下方畫一條分隔線（`hr.reply-card__divider`；聊天室沒有處理時間列的卡沒有 meta，線在標題下），內文從線下開始；收合的卡沒有這條線。量在 `reply-desk` 與 `chat-card-timestamp-align` 的 `expectDivider`。
跳到哪間聊天室由 `lib/quotedMessageThread.ts` 決定（選項 A）：目前聊天室的對象是原訊息的寄件人或收件人就留在這
間；都不是就去寄件人那間，寄件人是 owner 或 server 蓋的身分（`isSyntheticSender`）時改去收件人那間；兩端都沒有
聊天室、或原訊息已刪除（`deletedTs`）就不給鈕。已讀照跳轉那條的 `tailSeen` 規則，這顆鈕不另加已讀邏輯（owner 裁定維持）。
⚠️ 路由已經指著同一則時 hash 不會變、reactor 的 latch 已經用掉，`ChatArea` 的 `jumpToMessage` 因此改呼叫
`retryJump` 放掉 latch，否則第二次按下去畫面不動、也不會有任何錯誤。
⚠️ 目標是成員間的訊息時，它在聊天室裡收在預設收合的成員間對話區塊裡，畫面上沒有那一列；reactor 先把那個區塊展開
（`expandedGroups` 在 deps 裡，展開後再跑一次）才定位得到。少了這一步，跳轉會停在原地、不標示，也不出任何提示。

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

**「回覆這段」＝回覆對象＋一段選取原文（`reply_excerpt`）。** 選取完全落在同一個可回覆文字區塊時才浮出按鈕：一般聊天訊息的氣泡正文（`.chat__msg-text`），或等待中請示卡的摘要／內文（`.reply-card__summary`／`.reply-card__body`，見下一段，那條路不送聊天訊息）。跨兩個文字區塊（包含跨訊息、跨卡片或請示卡摘要與內文），或選到按鈕、選項、回答框等互動區域時，不浮出按鈕（正文裡的提及與連結雖然畫成按鈕／連結，算正文，可以選過去）（`ChatSelectionReply`，portal 到 body、fixed 座標夾在視窗內）。超過 1000 字時按鈕停用並顯示上限（`lib/replyExcerpt.ts`，與 server 的 `chatReplyExcerptMaxChars` 同一個數，`mock.reply-to.test.ts` 會讀 Go 原始碼對帳）：server 是**拒絕不截斷**，所以前端也不截。excerpt 跟回覆對象**同生共死**：存在草稿、x 一起清、整則回覆會把它清掉、送出失敗一起還原。有 excerpt 時，橫幅與每一個引用面（訊息列、`ResumeSummaryCard`）畫 `replyToChat.replyExcerpt`，否則照舊畫 `content`；「已不存在」那一態不變。⚠️ server **不檢查**它是不是原文的子字串（畫面上選到的字不等於原始 markdown），不要在前端或 mock 補這個檢查。

**請示卡的「回覆這段」是回答那張卡，不是送聊天訊息**（owner `rc-cf02eac19977` 圈 A：請示頁與聊天視窗都改，同一張卡兩處行為一致）。選段落在等待中卡片的摘要／內文時，按鈕把那段放進**這張卡自己的回答框上方**（`ReplyComposer` 的 `quote`，有 x），游標移進回答框；送出回答帶 `ReplyCardAnswerPostDTO.reply_excerpt`，跟聊天 excerpt 同一套 trim 與 1000 rune 上限、超過就拒絕（server 共用 `chatReplyExcerptMaxChars`）。引用不是回答：單有引用仍是「空答覆」400。路由靠 `ReplyCardQuoteProvider`：等待中卡的 `ReplyCardWaitingBody` 用 `useReplyCardQuoteTarget(card.id, …)` 登記自己，`ChatSelectionReply` 只在 `[data-reply-card-id]` 有登記時才浮出按鈕——所以已回覆／已過期的卡（沒有回答框）不浮出，宿主沒包 provider 的面（任務卡內嵌的回覆卡）也不浮出。provider 要同時包住 `ChatSelectionReply` 與選取根（ChatArea 的訊息串、RepliesPage 整頁）；`ChatSelectionReply` 要放在卡片 article **外面**，portal 的 click 會沿 React 樹冒泡，放在卡裡會翻卡。⚠️ 重新決定（PUT）是**整份換掉**答案，沒帶 `reply_excerpt` 就等於拿掉引用；所以編輯模式預設帶著原本的引用（`ReplyCardAnsweredBody` 的 `startEditing`），按 x 才拿掉。顯示引用的面從資料欄位找：`grep -rn "replyExcerpt\|answerReplyExcerpt" frontend/src`（最終答案列由 `ReplyCardBody` 給三個 wrapper 共用，另有 `ResumeSummaryCard`）；`TaskReplyCard` 收合的那一行只印選項或文字摘要，不印引用。agent 端讀得到的地方是 `get_reply_card` 的 `answer.reply_excerpt`、請示卡清單的 `answer.reply_excerpt` 預覽、喚醒快照的 `card.answer_reply_excerpt`，以及 `ocagent` 的 `reply-card … answered:` 那一行的 `quoting "…"`。

**取消回覆的 x 只清回覆對象。** 不准順手清 `draft`、不准清
`pendingAttachments`：取消回覆不是取消訊息。這條有測試釘住
（`ChatArea.reply-to.test.tsx`）。

**送出後一定要清掉回覆對象**，否則它會默默黏在下一則訊息上。送出**失敗**時的還
原不准蓋掉使用者飛行中重新瞄準的對象。

**每一則都有回覆入口 —— 包含 agent 之間的訊息。** server 端「必須同一場對話」
的檢查已於 2026-08-21 拿掉，owner 的原話是要能「引用另外兩個人對話裡的一句話來
介入詢問」。所以 `replyable` 這個 gate 已刪：入口出現在視窗裡的每一則上，回覆
仍然寄給這條線的 peer，只是引用的是 owner 指到的那一句。

請示卡那種訊息的氣泡被 `<ChatReplyCard>` 換掉，工具列改掛在那一列的內容框上緣
（卡片自己的 13px 上內距，碰不到卡頭右上的「標為過期」）。

以上都有測試釘住（`ChatArea.reply-to.test.tsx`、`ChatArea.quote-no-fetch.test.tsx`、
`ChatArea.reply-card-quote.test.tsx`、`mock.reply-to.test.ts`、`mappers.reply-to.test.ts`）。
其中 `ChatArea.quote-no-fetch.test.tsx` 是「那台狀態機真的不在了」的證人：它把
api client 換成記錄用的 proxy，畫一則有引用、一則引用不見的訊息，然後斷言**一次
呼叫都沒有**。
