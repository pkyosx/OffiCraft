---
paths:
  - "src/components/Task*"
  - "src/components/Outsource*"
  - "src/components/WorkerDetailPanel*"
  - "src/components/OfficePage*"
  - "src/components/tasks.css"
  - "src/hooks/useTask*"
  - "src/hooks/useOutsourceWorkers*"
  - "src/hooks/useWorkerCodenames*"
  - "src/lib/stepBadge.ts"
  - "src/lib/taskNo.ts"
  - "src/lib/duration.ts"
---

# 任務頁、TaskCard、外包面板與任務手冊

## 任務清單與錨點

useTasks 把 statusFilter 轉成重複的 ?statuses=；執行者與類型篩選仍在前端。清除狀態篩選才送空集合，代表使用者要完整清單。不要為 dependencies 再拉全歷史，也不要把每個 task SSE 變成全歷史下載。

跳到 #tasks/<id> 時，清單仍保留原篩選，另以 GET /api/tasks/{id} 補單張錨點。anchor id 是 effect 的參數；anchorPending 在補抓落定前擋住兩個空狀態，否則還在路上的那張會被說成不存在。

**補抓落定後有三種結局，話不一樣，不要合併（owner 2026-09-05 `rc-428906235337`）**：抓到就顯示那一張；**404 ⇒ 錨點留著**，畫面答「沒有符合篩選條件的任務」，出口是「清除篩選」（`anyFilter` 已含 taskIdFilter，所以按鈕本來就在，`clearFilters` 連 hash 一起還原）；**其他失敗（500／離線）⇒ `anchorFailed`**，顯示載入錯誤並壓住兩個空狀態——沒問出口的問題不得給答案。錨點**不再自己把 hash 拿掉**；釘住這三格的是 TasksPage.test.tsx、TasksPage.jump.test.tsx 與 TasksPage.anchor-fetch.test.tsx 各一支。合併時清單列優先，因為輕量列才有 dep_tasks；單張 DTO 沒有時不可覆蓋它。篩選未包含錨點時，depTasks===undefined 表示未知，不表示沒有依賴。

## 任務頁的 ID 篩選欄位（T-93 第二輪，owner 2026-09-06 `rc-44347fc49338`）

第一輪只把欄位做在請示卡頁，owner 在試用站上回「任務沒有出現同樣的filter」——他的交辦逐字是「任務列表**跟**請示卡列表，是不是**都**可以有一個ID的filter」，所以那是漏做，而**全套測試當時是綠的**。

🔴 **這一頁上有兩條 by-id 的路，它們不是同一條，不要合併**：
- **`taskIdFilter`（hash 錨點）**：單獨抓 `GET /api/tasks/{id}`，**蓋過狀態集合**（連結指向已完成任務也跳得到），三種結局的話術見上一節。
- **`idQuery`（打字）**：只篩已載入的那批，**不發任何請求**。獨立審查在請示卡頁把「一個字元一個請求」列為必須改；`TasksPage.id-filter.test.tsx` 有一支專門盯著 `api.getTask` 沒被呼叫。

⚠️ **`anyFilter` 裡 `idQuery !== ""` 是獨立一句，不要以為 `taskIdFilter` 涵蓋它。** 它看起來可以刪，因為狀態預設非空 ⇒ `anyFilter` 恆真 ⇒ 刪掉之後**八支測試全綠**（實測）。唯一打得到它的是**先按清除篩選（狀態集合也清空）再打字**那條路：少了它，畫面窄掉而唯一能放寬的按鈕不在了。那支測試就叫 `after 清除篩選 empties every OTHER axis, typing an id brings the button BACK`。

**欄位寬度取自要裝的編號長度**，透過 `--id-filter-ch`；`.id-filter` 刻意用 `content-box` 對抗全域 `border-box`，否則 padding 會吃掉字數（實測 15 字元的 id 在 15ch 的欄位裡差 13px 裝不下）。

## TaskCard

卡片預設收合；標題與 description 是唯讀 UI，owner 沒有要求編輯入口。進度與 gate 狀態直接使用 server 值；stepBadge 由 lib/stepBadge.ts 統一，superseded 不計入 progress。gate 預告、內嵌 TaskReplyCard、等待外部 banner 與任務訊息框都要維持原 wire 語意。

依賴 chip 讀 task.depTasks，必須區分已解析、查無此任務與 undefined 未提供。

**卡上有兩個「開／關」，owner 對它們的裁定相反，不要把其中一條套到另一條（T-6630）：備註是「不准動畫面」，整張任務卡收合是「要把畫面定位過去」。**

**① 步驟備註 — 不在卡內展開，用閱讀面開（`task-step__note-open`）。** 備註只在**有備註的 step** 才渲染一個**右下角小入口**；點它用 `MarkdownPreviewOverlay` 跳出閱讀面（owner 2026-08-16：「備註不是很常按，可以放在 step 的右下角，點開再跳出另一個 Modal…像我們開 .md 檔那種方式，只是沒有下載或分享連結」）。
- **餵它 `source` 而不是 `url`**：沒有下載、沒有分享連結是那個元件對「呼叫者手上已有的文字」的既有契約，不是這裡拔掉的。改成 `url` 會把兩者靜默長回來——`taskcard-note-entry` 會紅（實測 10 條）。
- 🔴 **入口必須是真的 `<button>`，且保留 44px 觸控目標**：整張 `.task-card` 是 `role=button`，角落控制項四周全是「收掉整個任務」。實測誤觸環帶：入口周圍 10px 內有 **96%** 的像素按下去會收掉整張任務；水平容錯半徑從上一版整列的 ±156px（手機）掉到 **±34px**。這個代價 owner 知情並裁定接受。
- 🔴 **保護閱讀面內的點擊不會收掉卡片的，不是 portal**：React portal 沿 **React tree** 冒泡，overlay 就渲染在帶 `onClick` 的 `<article>` 裡。真正在擋的是濾網裡的 `[role='dialog']` ＋ panel 自己的 `stopPropagation`；刪掉那一個 token 曾經**零測試會紅**，現在 `taskcard-note-entry` 有一條專門點 backdrop 的（實測 2 紅）。
- **開／關閱讀面都不得改變 `.tasks` 的 scrollTop**（owner ①：畫面不動）。跳窗之後版面不 reflow，這條是結構上成立的，但仍嚴格斷言（`taskcard-note-anchor`）；不得改用 `scrollIntoView`。
- **外觀不設護欄**（owner 裁定：顏色由他一次確認），面板色取主題的 `--color-bg`／`--color-border`，**不要改回 `color-mix(--color-overlay …)`**——混色在淺色主題會變灰，是他實際看到回報的。

**② 整張任務卡收合 — 要定位到那則任務。** owner：「收和整個任務時，最後應該要定位到那則任務」。收合當下卡片頂端若已捲到畫面上方，把它的頂端拉回捲動區頂端（寫**真正在捲的那個祖先**的 scrollTop，見 `lib/scrollPort.ts`；仍不用 `scrollIntoView`）。校正是**單向**的：頂端還看得到就一 px 都不動，**展開方向完全不校正**（那個方向仍歸①管，護欄兩邊都釘）。定位選「把卡頂對齊捲動區頂端」而不是「最小移動」：任務頁沒有任何 sticky 表頭（實掃 `tasks.css` 無 `position: sticky`），所以頂端不會被別的東西蓋住，對齊頂端就是「這張卡從頭給你看」。物理上限：收合清單最後一張時捲動範圍在它下面已無餘裕，卡片停在畫面中下方而非頂端，但必須整張仍在視窗內——護欄斷言的是「看得到」，不是「在頂端」。

外包 chip 在任務卡描述 launch intent；監控頁的自報 runtime/model/effort 是另一條規則，不可混用。worker 已 release 時不捏造代號；未指派與零節點狀態要分別顯示等待指派與規劃中。

## 外包面板與聊天

useOutsourceWorkers 只讀 /api/outsource-workers 與 settings，並訂 outsource_worker、task、chat、chat_read；不可加回 tasks 或 task-manuals 全歷史 join。server DTO 已帶 task_no、created_ts、type key/name。

外包列顯示 O- 代號、task type 加真實 presence 點、可點的任務編號與 unread badge，**以及綁定任務的標題（獨立一行，hover 給全文）**；不顯模型與狀態字。⚠️ 標題那條不要照舊規則拔掉：2026-07-16 的「不顯標題」已被 owner 2026-07-23 推翻（T-3451），現行畫面由 `OutsourcePanel.test.tsx` 的 `outsource-task-title-<id>` 斷言釘住——照舊句去拔 title 會直接弄紅那一條。任務編號就是 task id 本身（T-5291 起不再截短），所以「不顯識別鍵」那條**不適用於它**——那串就是要給人抄走貼回去用的。排序以 task created_ts 為準，終態 worker 從 live list 消失。聊天使用 ow- id；header 可用 synthetic member，但不要在 chat header 重複 rail presence。上限 -1 是無限、0 是暫停指派；settings 未載入時只顯目前數，不捏上限。

## 任務手冊

GET /api/task-manuals 的列表只靠 type_key。partial POST 中 null 是 no-op，assignee:{} 才是清除；非終態手冊不可刪。詳細頁可讀 definition 與 learnings，但不顯示內部檔名。

欄位要標 required/key；指派可為 member 或 outsource，並保留 model、effort、machine、copies 語意：copies=0 表示無限，machine 必須是實際機器，不自動 fallback，也不要送空 machine。離線時不自動改派。
