---
paths:
  - "src/components/MarkdownPreviewOverlay*"
  - "src/components/AttachmentStrip*"
  - "src/components/DocComments*"
  - "src/components/doc-comments.css"
  - "src/lib/docComments.ts"
  - "src/components/ComposerAttachmentPreview*"
  - "src/components/md-preview.css"
  - "src/components/ConfirmModal*"
  - "src/components/*Modal*"
  - "src/components/*Popover*"
  - "src/components/DocCard*"
  - "src/components/DocUsage*"
  - "src/components/TaskManualsPage*"
  - "src/api/docCap.ts"
  - "visual-guards/manual-doc-usage.ct.spec.tsx"
  - "src/components/BootDocPage*"
  - "src/components/SettingsPage*"
  - "src/components/DiffView*"
  - "src/components/diff-view.css"
  - "src/components/DiffScreen*"
  - "src/components/DiffPage*"
  - "src/components/DiffModalHost*"
  - "src/components/AttachmentModalHost*"
  - "src/hooks/useAttachmentOpener.tsx"
  - "src/lib/attachmentLink.ts"
  - "src/components/DiffShareLinkButton*"
  - "src/components/diff-screen.css"
  - "src/lib/diffLink.ts"
  - "src/components/DocumentHistory*"
  - "src/components/TaskArtifactVersions*"
  - "src/components/task-artifact-versions.css"
  - "src/lib/escapeLayers.ts"
  - "src/lib/useEscapeLayer.ts"
  - "src/lib/escapeLayerOwnership.test.ts"
  - "src/lib/lineDiff.ts"
  - "src/lib/wordDiff.ts"
  - "src/lib/shareLink.ts"
  - "visual-guards/image-zoom-pan.ct.spec.tsx"
---

# 全幅閱覽 overlay、Esc 分層、DocCard、差異呈現

## 全幅閱覽

MarkdownPreviewOverlay 是唯一的全幅面，props 是互斥的 url+attachmentId、source、imageSrc 或 diffParams（diffParams 是一份比較，由 /diff 網址的兩個位址指名）。url 自己 fetch，保留下載與以 att- blob id mint 的分享連結；source 是聊天本文，不 fetch、不可下載或分享；imageSrc 是尚未上傳的圖片 bytes，只能下載。

分頁（pager）是 optional，而且「下一張是誰」永遠由呼叫端回答，overlay 自己不猜：它只收到 index、total 與 onGo，換到哪一張是呼叫端重新 render 的結果。呼叫端要翻的那份清單，就是使用者當下看得見的那一份 —— 例如相簿面板翻的是套完 tab 與上傳者篩選後的結果，附件列翻的是那一列的完整顯示順序（圖片與下載列一起，因為畫面上的計數要對得上使用者數得出來的東西）。清單只有一項時不要傳 pager：傳一個 1/1 會畫出兩顆永遠按不動的控制項，還會白掛一組方向鍵監聽。⚠️ 相簿面板今天還沒有照這一句做（它無條件傳 pager），所以**不要拿它當這一句的參考實作**；那是 T-123 記下來、還沒有人去修的落差，不是這一句寫錯了。方向鍵只在圖片模式生效，文字內容的方向鍵留給捲動，兩顆 chevron 則兩種模式都能翻。

圖片縮放必須改變 layout：用圖片 width/height 乘 fit box 與 zoom，不能只用 transform；量測 fit 前要移除 inline 尺寸，resize 在所有倍率重算，並解除 stylesheet 百分比 cap。pointer drag 與原生捲動共用 scrollLeft/scrollTop。控制列要在 scroll container 外，矮視窗的兩條高度 cap 都要扣除 overlay chrome。

單指 touch 交給圖片自己的原生捲動，避免 double-apply；雙指由 overlay 接管，frame 用 touch-action: pan-x pan-y，並以 non-passive touch handler 改 zoom。iOS 的 gesture 事件與 touch 事件用互斥旗標；不可用 user-scalable=no 或 maximum-scale=1 禁掉整頁縮放。

overlay portal 到 document.body，避免宿主 stacking context 困住它；需要 click-outside 的 caller 以 .md-preview 選擇器辨識，不再靠 ancestor contains。render 與 CT 查詢從 document.body/page 找 overlay。圖片、文件都要戴共用 .doc-md；source 開 breaks，url 不開。圖片展開鈕只在有本文的 incoming bubble，位置由 bubble padding 讓位。

### 文件註解

url 模式的 markdown 文件把獨立成行的圖片畫出來，走的就是使用說明頁那條 `resolveImageSrc={authedAttachmentUrl}`（同一個 SAFE_IMG_SRC_RE 閘）；source 模式（訊息本文）不傳，`![…](…)` 照舊是字。

註解模式開在 url 模式的文字文件（markdown、純文字、JSON；圖片附件沒有），開不開由 overlay 的 `commentable` 一個條件決定，而選取與點圖都是在 bodyRef 裡找，所以要放寬就是改那個條件。呼叫端要傳 `docComments`（收件人＋任務產物資訊）；沒傳就沒有「註解」鈕。收件人由開啟的那個面決定、overlay 不猜（owner：「從哪邊開，就是回在哪個聊天室」）：聊天室裡的附件列、圖庫與訊息本文連結都回給這個聊天室的對象——包括別的成員寄給他、出現在他串裡的訊息，不是寄件人；請示卡裡的連結與它的提問／回答附件回給開卡人；任務卡裡的連結與任務產物回給負責人。收件人的 roster 是四態，聊天室、請示卡與任務卡都由 `lib/docComments.ts` 的 `docCommentRecipient` 判（owner 與空 id 回 null；listed 由各面自己的名冊給，任務卡是 members 加 live workers）：收件人是 server 蓋的身分（system、`hook:`、`sched:`，見 `lib/syntheticSender.ts`）是 not-member，顯示「不是成員寄出的」而不是「已離開」；live 名冊（members 或 live 外包 workers）查得到是 active；名冊還沒載完（呼叫端的 rosterSettled 為 false）是 pending——送出鈕停用但不說對方已離開，因為查不到可能只是還沒到；載完仍查不到才是 left、顯示原因。送出的本文格式由 `lib/docComments.ts` 的 `formatDocCommentMessage` 獨占——它是給 AI 讀的固定格式，不走 i18n、不讓主題用詞覆寫；引言要寫在清單項的續行，因為 Markdown.tsx 把清單項第一行當 inline 讀，寫在第一行的 `>` 不會變成引言。註解續行開頭的區塊符號（`>`、`#`、`-`/`*`/`+`、`1.`、fence、`---`）與檔名裡的 `\`、`[`、`]`、反引號都以反斜線跳脫，Markdown.tsx 照 CommonMark 把反斜線後的 ASCII 標點當字面字元，否則註解裡的一行 `> …` 或 `2. 原文：` 會畫成另一段原文或另一項。送出成功只從草稿刪掉送出的那幾則（依 id），另一個開著的同份文件在送出途中加的註解要留著。標色用 CSS Custom Highlight API，不改 Markdown 畫出來的 DOM（React 還管著那些節點）；圖片註解（`kind: "image"`，存 alt 與 src）例外地由 effect 在 `<img>` 上設 `data-doc-comment`——React 沒有 render 那個屬性，reconcile 不會動它，cleanup 會拿掉。存下的 src 是文件寫的位址：同源就去 host、去掉 authedAttachmentUrl 加的 `?token=`（會進聊天訊息）；送出時引言是 `> 圖片：[alt 或檔名](src)`，alt 走檔名同一套 escapeLinkText——放在連結文字裡，因為 Markdown.tsx 在連結外不認反斜線跳脫的 `[`。找原文時忽略空白，因為選取文字在區塊之間帶換行、DOM 文字節點沒有。草稿存 localStorage（以附件 id 為鍵，讀寫都包 try/catch）；沒有 kind 的一則就是文字註解，那也是舊版存的形狀，讀取不能要求它帶 kind。註解面板以 attachmentId 為 key：pager 換到另一份時整個重掛，否則上一份的註解會出現在下一份、存進下一份的草稿、掛在下一份的連結下送出。

### 附件連結

訊息本文裡指向單一附件的同源連結（`/api/chat/attachment/att-<12hex>`，query 不看，例如文件註解訊息的開頭與圖片引言）由 `AttachmentModalHost`（App 根，跟 `DiffModalHost` 同一個模式：context 在 `hooks/useAttachmentOpener.tsx`，沒有 provider 就是普通連結）在原頁以 url 模式開這個 overlay。與比較連結不同，**所有點擊都攔**（含 ⌘/ctrl/shift 與中鍵）：serve route 要 token，新分頁一定 401，而 token 不能寫進 href 或網址列；只剩右鍵選單的「在新分頁開啟」攔不到。連結本身沒有 mime／檔名，也沒有回附件 metadata 的 route：型態先用 `attachmentMimeForName(連結文字)` 判（跟 server 同一張副檔名表）；判不出可預覽的型態（圖片引言的連結文字是 alt，常常沒有副檔名）就對 serve route 送 HEAD（Go ServeMux 的 `GET` pattern 也接 HEAD，token 只在請求裡），等回應時 overlay 以 `typePending` 顯示載入中、不 fetch 內容，回應的 Content-Type 決定型態；HEAD 失敗才是「無法預覽」加下載。連結本身答不出收件人，由畫出連結的那個面以同檔的 `AttachmentLinkCommentsContext` 提供，開啟請求帶著它、host 轉成 overlay 的 docComments：ChatArea 包住訊息本文、放大閱讀與看原訊息給聊天室對象；請示卡（`ReplyCardBody.tsx` 的 `ReplyCardLinkComments`，聊天內嵌、任務步驟內嵌、請示卡頁三處共用，卡上的提問／回答附件列也讀它）給開卡人；TaskCard 整張給負責人。最內層的 provider 勝出，所以任務卡裡的請示卡回給開卡人；請示卡一律自己提供（解不出開卡人就是 null），不繼承外層。沒有 provider 或值為 null（續接摘要、使用說明、未指派的任務、開卡人是 owner）就是唯讀閱覽。收件人的 roster 狀態要跟著名冊即時變（名冊載完、對方離開時送出鈕跟著變）：opener 回傳一個 refresher，連結在 context 值變動時呼叫它，host 只更新還是同一次開啟的那份、值相同就不動；docComments 不放進觸發型態探測的 state，否則每次名冊變動都會重送 HEAD。型態還在問（`typePending`）時沒有註解鈕。從連結與從附件列打開同一份附件是同一份草稿（鍵是附件 id）。

## Esc 分層

只有 lib/escapeLayers.ts 綁 window keydown。useEscapeLayer(onEscape, ref, active) 以 DOM 包含關係找最內層；互不包含的面以後註冊者為 tie-break。被包住的面一定傳自己的 root ref，關閉中的常駐面以 active=false 退出。

層內要吞 Esc 就在 handler 判斷；input 自己的 Escape handler 也要 preventDefault，否則取消編輯會再關掉宿主面。這套 layer 不是 focus trap；不要在每個 modal 另綁 window listener。

## DocCard

DocCard 是設定頁可編輯長文件的共用外殼：標題、字數、版本入口、超上限阻擋、儲存確認與錯誤列；body 由 renderBody 提供。BootDocPage 使用它但保持唯讀，不能長回自己的 editor。新能力一律 optional，不傳就保留既有 caller 行為；不要加回已退場的 above、factoryReset、boot-doc.css 或 reset 分支。還原只在編輯模式的版本紀錄預設內容列提供。

`doc.readOnlyHead` 是文件本身帶的唯讀上半，由 DocCard 畫在編輯框上方（編輯中也留著），不經 renderBody —— BootDocPage 不准提到那個 prop，它的測試會 grep 原始碼。沒有 readOnlyHead 的文件行為完全不變。

編輯框裝的是**可以編輯的那一半**，送出的也是它；boot document 的唯讀上半在 wire 上沒有欄位，前端沒有辦法送。字數與 cap 判的是**存下來的整份**：`usage.size` 減掉編輯框實際裝著的那段字數，就是草稿沒有涵蓋的那一半，要加回去，所以編輯框只裝半份時螢幕上的數字仍然是 server 會拿去判的那個。編輯中字數讀 draft，超上限在送出前擋下，server 失敗顯示原話。沒有 usage 的文件不受 cap 影響。

🔴 **上面那句「送出前擋下、失敗顯示原話」只對 DocCard 成立，對任務手冊那張卡是假的（T-100）。** 那張卡走的是 `DocUsage`，**只借了讀數，沒有借前置阻擋**：`TaskManualsPage` 的 `saveError` 是**布林值**、server 的訊息進 `console.warn`，畫面只給一句固定的「儲存失敗，請稍後重試」——而超上限重試多少次都不會成功。對照組是 DocCard：它有 `overCap`、送出鈕會 `disabled`，而且另外畫一列說出被拒的理由。讀數這一側也不會提醒：`DocUsage` 的 className 恆為 `doc-card__usage`，**超上限不換樣式**，所以「18001 / 18000」就這樣走過去。**這是刻意留著的缺口，不是還沒做完**（本包只補讀數，不改存檔行為）。⇒ **在那張卡上不要把這條規則讀成「已經有人擋住了」**；要改那條路徑請回頭讀 `DocUsage.tsx` 檔頭。

🔴 **那段計算不在 DocCard 裡（T-100 起）**：它是 `api/docCap.ts` 的 `shownDocSize`，DocCard 與任務手冊編輯器共用的 `DocUsage` 都呼叫同一支。收成一支的理由是它會**在一邊悄悄算錯**——一邊讀草稿、另一邊讀已存檔字數，兩個數字都長得像對的。要改讀數的算法就改那一支，不要在任一個呼叫端補一份。

樣式（`.doc-card__usage`）由 settings.css 擁有。**任務手冊的 SOP 現在也用它**（透過 `DocUsage`），Insight 仍走 member-detail.css 的 `.mp-insight__size`、且只顯示**已存檔**字數（打字當下不會動），不要順手改。
⚠️ 這個 class 有 `white-space: nowrap`，不是裝飾：任務手冊第三塊的標題列是一條會收縮的 flex 行，390px 下實測把「15796 / 18000」擠成三行。護欄在 `visual-guards/manual-doc-usage.ct.spec.tsx`，jsdom 看不到。

## 差異呈現

lineDiff 只負責行結構，wordDiff 只負責相鄰取代列的 token，DiffView 只負責畫面。DiffView 永遠 render 完整 result.rows，不讀 hunks、不畫 @@；options 只暴露它真正擁有的 maxLines。相同版本與過大版本要是兩種不同空態，過大態要報行數。

token 標亮只在兩側有共同非空白 token 時做，每側有上限；顏色由真瀏覽器守衛驗，jsdom 只驗 tint 的結構位置。

## 比較的對外連結

一份比較有兩種網址：沒有簽章的內部連結要登入才打得開，帶 `?sig=` 的對外連結誰都打得開。簽章只由 server 現鑄（`GET /api/diff/share-link`），回的是 server-relative path，由瀏覽器補 origin——跟附件分享連結同一套 `lib/shareLink.ts`。呼叫端手上原本的 `sig` 一律不回送：簽章是這個呼叫的產出，不是輸入。

複製這條連結的控制項是 `DiffShareLinkButton`，圖示、沒有可見文字，三種狀態三個圖示；成功才說成功，鑄造或剪貼簿失敗要畫成看得出來的失敗，不可以退回閒置圖示。

**這個控制項只畫在「確定有 session」的地方**，因為 mint route 跟其他 route 一樣要授權：studio 裡的 modal 是結構性的（`DiffModalHost` 在 AuthGate 內，獨立頁不掛這個 overlay）；獨立頁以 `params.sig === undefined` 當閘——main.tsx 把帶簽章的網址掛在 AuthGate 之前，那位讀者可能根本沒有帳號，不能給他一個按了會失敗、又能重鑄連結的按鈕。

## 產物版本閱讀器

TaskArtifactVersionsModal 是任務產物「被換過幾次、換掉的是什麼」的唯一讀面，形狀沿用 DocumentHistoryModal（左版本清單、右內容／差異），由產物列在 versionCount > 1 時的入口開啟。它 portal 到 document.body，所以產物 popover 的 click-outside 述詞要認得它的 root，跟 .md-preview 同一格。

「目前版本」一律在開啟時向 server 讀（listTaskArtifacts——T-92 之後 getTask 只回 artifact_count，一列都不回，拿不到版本讀面要的 kind／url／mime），不得改讀 popover 手上的 SSE 快取——差異說的話必須等於伺服器現在的狀態，這條與文件閱讀器同一條判準。server 說產物已不在任務上就照實講，不拿手上最後知道的那份當現況。

差異依產物型態分三種畫面，不是一種畫面留三個洞：兩份文字餵共用的 DiffView（不新刻比對元件、不改 DiffView／lineDiff，要放寬上限只能由呼叫端傳 DiffViewOptions.maxLines）；圖片與非文字檔改成前後切換；連結列出舊網址與新網址。

「這份 bytes 是不是文字」問的是回應本身的 Content-Type，不是產物列上的 mime——列上的 mime 與 bytes 的 content type 是兩句話，讀的是後者；尤其不可拿 live 產物的 mime 去判某一版，那是另一個版本的事實。不是文字的回應不讀 body。

版本 wire 的 `url`／`mime`／`filename`／`is_image` 都由 server 從那一版自己的 blob 解出，跟 live 產物走同一條解析：file/image 版本的 `url` 是 blob 端點；link 版本的 `url` 是從它自己的 `text/uri-list` blob 讀出來的目標。兩張產物表的 `url` 欄都已被 00086 移除（T-92），沒有欄可抄——當年照抄那一欄（對 file/image 是空字串）會讓每個檔案版本在前端讀成 gone。

🔴 但 mime 不是唯一判準：`text/*` 是文字、`image/*` 是圖片，兩者皆非時再看檔名副檔名，命中一份封閉的文字副檔名清單就當文字讀。副檔名要從哪裡拿，兩側不對稱：版本的 `filename` 由 wire 從它自己的 attachment_id 解出，是那一版自己的事實；live 產物在 T-92 之後**沒有 filename 欄**，而它的 `name` 是**作者寫的標題**（`artifactDisplayName` 第一順位就回 stored name，T-92 又讓寫入必填），常態上根本沒有副檔名——只讀 `name` 會讓一份 `.md` 報告的 live 側判成 opaque，差異分頁整個消失。所以 live 側的檔名改從**下載回應自己的 `Content-Disposition`** 取（`filename*=UTF-8''…` 優先、`filename="…"` 次之；那個 header 是 server 對每個非圖片附件都會寫的），兩邊任一看起來是文字就當文字。清單本身仍是封閉的，不因此放寬。理由是 agent 上傳的報告回來是 `application/octet-stream`——那是上傳端「不知道」，不是「這是二進位」；只信 mime 會把報告、log、spec 這些最常見的產物全部推去前後切換，永遠 diff 不到。清單是封閉的，不在清單上的仍然不讀 body。

沒有還原面，server 也沒有還原動詞；舊版要回來是往前 replace。
