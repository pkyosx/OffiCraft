---
paths:
  - "src/components/MarkdownPreviewOverlay*"
  - "src/components/AttachmentStrip*"
  - "src/components/ComposerAttachmentPreview*"
  - "src/components/md-preview.css"
  - "src/components/ConfirmModal*"
  - "src/components/*Modal*"
  - "src/components/*Popover*"
  - "src/components/DocCard*"
  - "src/components/BootDocPage*"
  - "src/components/SettingsPage*"
  - "src/components/DiffView*"
  - "src/components/diff-view.css"
  - "src/components/DiffScreen*"
  - "src/components/DiffPage*"
  - "src/components/DiffModalHost*"
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

MarkdownPreviewOverlay 是唯一的全幅面，props 是互斥的 url+attachmentId、source 或 imageSrc。url 自己 fetch，保留下載與以 att- blob id mint 的分享連結；source 是聊天本文，不 fetch、不可下載或分享；imageSrc 是尚未上傳的圖片 bytes，只能下載。

圖片縮放必須改變 layout：用圖片 width/height 乘 fit box 與 zoom，不能只用 transform；量測 fit 前要移除 inline 尺寸，resize 在所有倍率重算，並解除 stylesheet 百分比 cap。pointer drag 與原生捲動共用 scrollLeft/scrollTop。控制列要在 scroll container 外，矮視窗的兩條高度 cap 都要扣除 overlay chrome。

單指 touch 交給圖片自己的原生捲動，避免 double-apply；雙指由 overlay 接管，frame 用 touch-action: pan-x pan-y，並以 non-passive touch handler 改 zoom。iOS 的 gesture 事件與 touch 事件用互斥旗標；不可用 user-scalable=no 或 maximum-scale=1 禁掉整頁縮放。

overlay portal 到 document.body，避免宿主 stacking context 困住它；需要 click-outside 的 caller 以 .md-preview 選擇器辨識，不再靠 ancestor contains。render 與 CT 查詢從 document.body/page 找 overlay。圖片、文件都要戴共用 .doc-md；source 開 breaks，url 不開。圖片展開鈕只在有本文的 incoming bubble，位置由 bubble padding 讓位。

## Esc 分層

只有 lib/escapeLayers.ts 綁 window keydown。useEscapeLayer(onEscape, ref, active) 以 DOM 包含關係找最內層；互不包含的面以後註冊者為 tie-break。被包住的面一定傳自己的 root ref，關閉中的常駐面以 active=false 退出。

層內要吞 Esc 就在 handler 判斷；input 自己的 Escape handler 也要 preventDefault，否則取消編輯會再關掉宿主面。這套 layer 不是 focus trap；不要在每個 modal 另綁 window listener。

## DocCard

DocCard 是設定頁可編輯長文件的共用外殼：標題、字數、版本入口、超上限阻擋、儲存確認與錯誤列；body 由 renderBody 提供。BootDocPage 使用它但保持唯讀，不能長回自己的 editor。新能力一律 optional，不傳就保留既有 caller 行為；不要加回已退場的 above、factoryReset、boot-doc.css 或 reset 分支。還原只在編輯模式的版本紀錄初始版本列提供。

`doc.readOnlyHead` 是文件本身帶的唯讀上半，由 DocCard 畫在編輯框上方（編輯中也留著），不經 renderBody —— BootDocPage 不准提到那個 prop，它的測試會 grep 原始碼。沒有 readOnlyHead 的文件行為完全不變。

編輯框裝的是**可以編輯的那一半**，送出的也是它；boot document 的唯讀上半在 wire 上沒有欄位，前端沒有辦法送。字數與 cap 判的是**存下來的整份**：DocCard 由 `usage.size - runeLength(text)` 推出草稿沒有涵蓋的那一段再加回去，所以編輯框只裝半份時螢幕上的數字仍然是 server 會拿去判的那個。編輯中字數讀 draft，超上限在送出前擋下，server 失敗顯示原話。沒有 usage 的文件不受 cap 影響。樣式由 settings.css 擁有；Insight、Lessons 與任務手冊尚未遷移，不要順手改。

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
