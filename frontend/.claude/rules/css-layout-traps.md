---
paths:
  - "src/**/*.css"
  - "src/styles/**"
  - "src/components/**"
  - "visual-guards/**"
---

# CSS / 版面陷阱

## 文字與邊界

自由文字與 markdown 的基底 `.doc-md` 要用 `overflow-wrap: anywhere`，讓長 URL、sha 與無空白 token 能收縮 min-content；修在 `.doc-md` 基底（`settings.css`），它是唯一來源，別逐 surface 貼。T-4974 曾逐處貼，結果同一個病從沒貼到的頁面復發，才收斂成 T-d451 的基底修法。下一個修這類 bug 的人若不知這個理由，會重走那條路。

`anywhere` 不是 `break-word`：兩者都會斷已經溢出的行，但只有 `anywhere` 會收縮 min-content；用 `break-word` 以為等價，容器仍不肯縮，會讓修理的人誤以為 bug 已消失。沒有 markdown 繼承的自由文字欄位要自己宣告；`.doc-md` 的 `pre` 與 `table` 是明確允許的橫向 scroll 子區，修整頁面 overflow 時不可把它們的 `overflow-x: auto` 拿掉。守衛要同時量頁面與實際 scroll container，並同時確認 `pre`/`table` 仍能滾。

重驗 mutant 時，整頁 overflow 那條若先炸，測試會中止，底下的 per-surface 斷言根本沒執行；要證明後者，先暫時放寬整頁斷言再跑 mutant，不能把「整體失敗」誤讀成「後面的斷言也驗過」。

固定高度、可收縮 flex item、CJK 標籤同時出現時，用 `white-space: nowrap` 保住單行；中文的 min-content 可能只有一個字，固定高就會被折行溢出，而同一段 CSS 對拉丁字的幾何不等價。不要用 `flex:none` 代替，也不要只看 computed property：要用真瀏覽器量 line box 與實際 overflow。角色誌的 Insight 卡（`.mp-lessons__head`）與 DocCard（`.doc-card__head`）在 ≤720px 讓 header 換行：編輯模式的三顆按鈕在任何手機寬度都跟標題放不進同一列，閱讀模式的「編輯」鈕在窄螢幕也會掉到下一列，多出的列是預期形狀，換來的是標題與字數不折行。不要為了省那一列把斷點縮回去；也不要讓標題群 `min-width: 0` 卻不讓它跟 header 一起換行，那會把標題壓成零寬、一字一行，徽章與字數直接疊到按鈕上，而且沒有任何東西溢出卡片，只看 overflow 的守衛抓不到。任務卡步驟的標題列（`.task-step__row`）是兩欄 grid：標題 `minmax(0, 1fr)`、右欄是固定 64px 的耗時膠囊，`align-items: start`。標題只在自己那一欄裡換行，膠囊永遠在標題第一行的右端、不會被擠到下一行；改回 flex wrap 會讓膠囊在窄寬掉到標題下面（CT `taskcard-sections` 在 320／375／390／1440 中英量）。時間軸圖示的垂直位置是算出來的（`--task-marker-y`：步驟框上緣到標題第一行中線，並行階段是它的標籤行）；改步驟的 padding、標題字級或行高要一起改它。

## 輸入欄位 16px（iOS 聚焦放大）

iOS Safari 在字小於 16px 的 input／textarea／select 聚焦時會放大整頁，放大後頁面可以左右拖；owner 在聊天回覆請示卡、請示頁「稍後再看」的自訂數字都踩過。`styles/global.css` 有一條 `@media (hover: none)` 全域規則：欄位 `font-size: max(16px, 1em)`（1em 是繼承來的大小，大欄位不縮），選擇器刻意是 (0,1,1)：蓋過元件單一 class 的 `font: inherit`／小字，讓給兩個 class 以上、刻意指定字級的規則。新元件不用再各自補 16px；但若用兩個 class 以上把欄位設成小於 16px，就會繞過它。舊的逐元件 16px 規則（chat-search、chat-groups 等）仍在，沒有刪。守衛：`chat-reply-to`（回覆後聚焦的輸入框）與 `reply-desk`（稍後再看的數字與單位）在觸控 390 量聚焦欄位的字級 ≥16，桌面維持原字級。Chromium 不會模擬這個放大，量的是字級本身。

## 浮層與 CSS ownership

絕對定位浮層不可用以視窗左緣為座標的 vw 夾寬度。讓父容器提供 `left:0; right:0; width:auto`，再以 max-width 收上限；量浮層自己與中間 scroll container，不要只量被壓回視窗寬的 flex parent。`documentElement` 沒橫溢出不代表沒 bug，祖先的 `overflow-y:auto` 可能把溢出吸進自己的 scroll container；CT 也要重現真實祖先鏈，裸掛會多出餘裕而假綠。

使用某個 block class 的元件要自己 import 該 block 的 stylesheet；不可依賴 transitive import。最後一個間接 importer 消失時，仍使用同一 class 的另一個 dialog 會一起變成原生樣式；styleOwnership test 是必要護欄，因為 jsdom 與 tsc 都看不出 class 字串和 stylesheet 的關係。

## 篩選欄位的 pill 樣式今天有兩份逐格相同的拷貝（T-93）

`.id-filter`（`idFilter.css`）與 `.tasks__filter`（`tasks.css`）逐屬性相同。這是刻意的取捨——元件自己帶著外觀，才不會依賴宿主頁的 stylesheet（同一節「用了哪份 CSS 的 class，就要自己 import 那份 CSS」）——但代價是**改其中一份不會有任何東西提醒你另一份還是舊的**。

⇒ 動這幾格的 padding／radius／border／字級時，兩份一起看；只改一份就會讓兩頁的篩選欄位在同一個畫面上長得不一樣，而測試與 lint 都不會叫。

清除鈕只有一份：`filter-panel.css` 的 `.filter-panel__clear`，長在篩選面板的底部。

請示頁的時間膠囊 `.reply-card__waited-pill`（`replies.css`）沒有拷貝：任務卡的步驟耗時與任務已歷時直接用同一個 class。改它的寬度、高度或字級，請示頁與任務卡一起變，兩邊的守衛（`reply-desk`、`taskcard-sections`）都要跑。

## 卡片的 label/value 列

任務卡、請示卡（請示頁、聊天室、任務步驟裡）、金鑰卡、檔案庫卡、傳承卡的 label/value 列都是 `card-meta.css` 的 `.card-meta`／`.card-meta__label`／`.card-meta__row`，各元件自己 import（styleOwnership 有登記）。owner 看過請示卡的「開卡人」比任務卡的列低、顏色不同、欄距不同：那是 `replies.css` 自己抄了一份（baseline、10px 欄距、沒有 opacity）。不要在各頁的 stylesheet 再寫一份格線或 label 樣式；值的樣式（chip、時間）才放在各頁。label 欄寬是那張卡最寬的 label（max-content），所以 label 字數不同的卡（例如有「處理時間」的請示卡）欄寬本來就不同。兩行的值（請示卡的目前任務）在那一列自己用 `align-self: baseline`。CT `card-meta-rows` 在 320／390／1440 中英拿任務卡的列當基準量另外四種卡：label 顏色、透明度、字級相同，label 在列的左緣，值在最寬 label 加同一個欄距之後，一行的值跟 label 垂直置中。

## lazy fetch

lazy prompt 的 fetch function 若由 wrapper inline 建立，不得直接放進 effect deps。用 ref 保存讀取函式，deps 只放真正的 cache key；in-flight 與 loaded key 分開，只有文字成功到手才蓋 loaded key。重繪不能取消仍有效的讀取，失敗要落 error state 並提供 retry。測試要在讀取途中用新 element 觸發 rerender、覆蓋成功、失敗重試與收合再展開。

## column flex 的 `align-items: flex-start` 會讓子元素被自己最寬的內容綁架(T-4aa0)

`flex-direction: column` 下,`align-items` 決定子元素的寬度；`flex-start` 讓它採
fit-content,下限是 min-content。子孫只要有不肯縮的 `<pre>` 或寬表格,子元素就會撐破
父容器的寬度限制。任務卡在真實祖先鏈量到 +148 @390；拿掉該宣告後 `<pre>` 回到
自己的內部捲動。

- 數字要註明真實祖先鏈：裸掛同一 bug 只有 +104，補上 `.app__main` 的 22px padding
  才是 +148；裸掛量測會低估真畫面。
- `min-width: 0` 治的是 flex item 的自動最小尺寸，不是這裡的 cross size；實測整條
  祖先鏈都無效。
- 不要從視覺上最明顯的區塊猜兇手；逐一隱藏子元素，看哪個消失才讓溢出消失，才是因果
  判定。按鈕移除後遺留的 `flex-start` 也可能成為同一顆地雷。

## 用了哪份 CSS 的 class,就要自己 import 那份 CSS(T-7526)
使用某個 block class 的元件要自己 import 該 block 的 stylesheet；不可依賴 transitive import。
styleOwnership test 防止最後一個間接 importer 消失後 dialog 變成原生樣式；jsdom 與 tsc
都看不出 class 字串和 stylesheet 的關係。

正職與外包詳情面板共用 `.mp-identity__actions` 的 column 外殼與 row buttons；更改在前、停止在後，沒在跑時只顯示喚醒。改 row/column 時手機 media query 要按新形狀重新驗跨距與均分，不能只驗「元素仍存在」；REST 仍是 `/restart`，退場的是 UI 用語，不是凍結 wire。

喚醒先開與更改相同的設定 dialog，預設保留原執行環境、模型、思考強度與已釘機器；落地順序是 model，再一支 activate，機器由那支 activate 的 machine_id 帶著（外包只在選到不同機器時帶）。喚醒不要先 relocate：已停好的外包會把 relocate 當成「停止後的重啟」當場派出 START，接著的 activate 再殺一次、再派一次。睡著的已釘機器照原樣帶，不能 fallback 到第一台線上機器；只有沒有固定機器時才預設第一台線上機器。

兩邊都沒有「只儲存，不喚醒」（owner `rc-baa00a00dbc9`）。released worker 的身分文字與入口共用，依 worker.status 判定；released 不畫生命週期卡或 dead action，offline 對照仍要保留。

## 說明收進 ⓘ

ⓘ 只放讀者需要知道、又沒辦法從標題或控制項自己推出來的說明。只是把標題或控制項重講一遍的說明，**刪掉文字**，不留 ⓘ，也不留灰字：

- 要加 ⓘ：單位或語意不明（「Codex 第一次通知」的數字是第幾次記憶壓縮，不是百分比）、範圍或生效時機（「機器憑證壽命」調小不影響已發出的、「背景動畫」只影響這台裝置）、標題看不出它是什麼（「系統互動」是每位成員開機都會讀的系統說明）。
- 不加，刪掉：說明只是換句話重講標題（「任務結案」→「任務結案時給 agent 的收尾指示」）、子頁或控制項已經看得到（「帳號安全」→「修改密碼、兩步驟驗證、通行密鑰」，點進去就是這三列）、選項自己說得清楚（「外包上限設定」→「設為無限則不限制」，旁邊就是「無限」）。

說明放在標題後的 ⓘ（`InfoHint`；接在一段標題文字後面用 `TitleWithInfo`）；沒有說明的列不傳 hint，`SetEntryText`、`RowText` 就不畫 ⓘ，列按鈕也不帶 `aria-describedby`。會讓人做錯事的字**留在畫面上**：警告與後果（會刪除、要重新安裝、不會喚醒、還原會覆蓋、會被別人看到）、目前狀態（可編輯、開著、份數）、錯誤、空狀態、正在進行的步驟該怎麼做。同一句兩種都有就拆成兩個 key，後果那一句照舊寫在標題下面；狀態改成標籤（例如全域情境列的「可編輯」用 `.set-badge`），不要跟著說明一起藏。拿不準時問：「沒點開 ⓘ 的人會不會因此做錯事？」會就留著。

- ⓘ 是 InstantHint 的 span（`role="img"`、名稱「關於 標題」），不是 button：可以放進列按鈕或 `<label>` 裡，點擊不冒泡、在 label 裡不會聚焦欄位，Esc 走 `useEscapeLayer`，所以對話框裡的 Esc 只收提示。
- 放進列按鈕時，按鈕用 `aria-labelledby` 指向標題（與狀態標籤），否則列的讀名會變成「標題 關於 標題」；並傳 `descriptionId`、按鈕用 `aria-describedby` 指向它：按鈕裡不能有可聚焦元素（與 presence-and-badges.md 的圓點同一條），所以列裡的 ⓘ 不進 Tab 順序、`aria-hidden`，說明改成按鈕的 description（隱藏的 span），滑鼠 hover 與手機點一下照舊。代價是看得見畫面的鍵盤使用者 focus 列時看不到泡泡。放進 `<h2>` 時同理給標題自己的名字。`<label>` 裡的 ⓘ 要求欄位自己帶 `aria-label`。
- 不換行的黏合是 `TitleWithInfo` 把最後一個字（中文是最後一個字元）和 ⓘ 包進 `white-space: nowrap`。不要改回 word joiner 或尾端 padding：Chromium 在 inline-block 前面一定給斷行點，兩種都擋不住 ⓘ 單獨掉到下一行。守衛在 `visual-guards/info-hint.ct.spec.tsx`（含逐寬度掃描），jsdom 看不出來。
- 會被硬擠窄、而最後一個字很長的地方（磁碟用量分項），容器可以把 `.info-hint__glue` 放回 `white-space: normal`，寧可 ⓘ 換行也不要壓到右邊的數值。
