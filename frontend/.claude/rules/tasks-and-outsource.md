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

**補抓落定後有三種結局，話不一樣，不要合併（owner 2026-09-05 `rc-428906235337`；話術在 2026-09-06 選項①下再細分，見下一節）**：抓到且通過其他條件就顯示那一張；**404 ⇒ 錨點留著**，出口是**常駐的任務編號欄位**——把它清空再按 Enter（或點到外面），`commitId("")` 連 hash 一起還原（T-118 之前這個出口是已篩選條上的「清除全部」鈕，owner 2026-09-06 連同那整條摘要列一起拿掉了；**同一天稍晚他又要求把清除留著**（`c-2423dba8b65b`），所以今天有**兩個**出口：清空編號欄位，或按欄位列最右邊的 `.filter-panel__clear`。後者只在真的有東西被篩住時才出現 —— 錨點 404 而編號欄裡留著那個編號，正是「有東西被篩住」，所以它會在）；**其他失敗（500／離線）⇒ `anchorFailed`**，顯示錯誤並壓住兩個空狀態——沒問出口的問題不得給答案。錨點**不再自己把 hash 拿掉**；釘住這幾格的是 TasksPage.test.tsx、TasksPage.jump.test.tsx、TasksPage.id-filter.test.tsx 與 TasksPage.anchor-fetch.test.tsx。合併時清單列優先，因為輕量列才有 dep_tasks；單張 DTO 沒有時不可覆蓋它。篩選未包含錨點時，depTasks===undefined 表示未知，不表示沒有依賴。

## 任務頁的篩選列與 ID 條件（T-118，owner 2026-09-06 20:07）

四個軸**常駐可見**、選了就生效。第三輪曾把它們收進一個漏斗按鈕後的頁內展開面板，配「取消／套用篩選」與「N 筆 · 已篩選：<chip ×> · 清除全部」條；owner 當天稍晚推翻那個形狀：「不要多filter那一層了，全部拉出來，而任務編號那邊就是按enter或是點外面就視為apply了，然後也不用再顯示14筆已篩選跟那一行跟案件那個子標了，案件跟請示卡都一樣」（`c-c3d681fe05da`）。

- **只剩一份狀態，除了編號**。三顆下拉直接寫 applied，點下去就重跑。**編號欄位保留 draft/applied 兩份**：`draftId` 是框裡的字，`appliedId` 是清單真的被什麼篩，**只在 Enter 或 blur 時由 `commitId()` 提交**。⚠️ 這不是殘留的面板遺跡——編號會換一次伺服器請求（`useTasks(…, appliedId)` → `GET /api/tasks/{id}`），**每按鍵提交就是每按鍵發請求**，那正是 owner 連退兩次的形狀（「每次都要全部都撈回來才濾不合理」）。**把 `onCommit` 接到 `onChange` 看起來像整理，實際上是把那個否決倒回去**；`TasksPage.id-filter.test.tsx` 有一支專門釘住「打字但未 Enter 也未 blur ⇒ 列表不動」，mutant 驗過會紅。
- **shell 是 `FilterPanel`**（`components/FilterPanel.tsx`，自帶 CSS、無 `position: fixed`／scrim／z-index）。它現在只收 `testId` 與 `children`，不再持有任何狀態。欄位是 children，由頁面提供。「不是 modal」那條（`c-3b5a0aa66550`「按搜尋時不要再跳出新modal」）**沒有被推翻**，「全部拉出來」是它的同向延伸。
- **沒有「清除全部」鈕，也沒有「案件」子標題**。清空＝逐欄清空（下拉取消勾選、編號欄清空後 Enter）。分組標題（「未結束 · 14」）**不在這次範圍，保留**。
- 🔴 **全部條件一起生效，編號也不例外（選項①）**：applied 的編號**不篩已載入的清單**，而是走 `useTasks(DEFAULT_STATUS, appliedId)` 的 `GET /api/tasks/{id}` 拿那一張，再拿它去比其他 applied 軸。編號因此是**精準比對**，不是第二輪的 substring。
- 🔴 **三種結局必須長得不一樣，不得合併**（這就是本票存在的理由：第二輪讓「不存在」與「在，只是沒被載進來」渲染成同一句 `沒有符合篩選條件的任務`，owner 在驗收時被它騙過去）：
  - **404** → `task-id-missing`，話術要講「這是跟伺服器要過的結果」。
  - **抓到但被其他條件擋掉** → `task-id-filtered`，**點名是哪一軸**，並給 `task-id-only`「只用編號再找一次」（清掉其他軸、留下編號）。
  - **抓到且通過** → 只顯示那一張。
  - 非 404 的失敗（500／離線）**不是 miss**：`tasks-error` 改講「沒有得到伺服器的回覆」，不得說找不到。
  編號生效時 `tasks-empty` / `tasks-empty-filtered` **兩個空狀態都噤聲**，否則就會退回那句合併的話。
- **`#tasks/<id>` 錨點**只是「種下 applied 編號」，且**同時把其他三軸清空**——連結沒有對負責人／類型／狀態表示意見。所以「連結指向已完成任務也跳得到」仍然成立，靠的不是錨點豁免狀態軸，而是那時根本沒有狀態軸；`statusAsk` 的 effect 在有 applied 編號時**跳過**，清單 ask 才不會被清空的狀態集合擴成全歷史（432 KB 那條回歸）。`#tasks/executor/<id>` 仍照舊顯式種三軸，並清掉編號。
- **已結束區的自動展開吃 applied 編號**（不是 hash）：打到一張已結案任務並提交時若不展開，畫面會出現「通過了卻什麼都沒有」的第四種沉默結局。
- **負責人選項的計數讀 applied**（不再有 draft 可與它矛盾）；計數 0 的選項隱藏，但**已勾選的保留**，否則取消不了。
- 測試一律走 `src/test/tasksFilter.ts`（勾＝生效；編號用 `applyIdFilter` 打字＋Enter、`typeIdFilter` 只打字不提交、`blurIdFilter` 點外面），不要在各檔各自複製手勢。

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

外包列顯示 O- 代號、task type 加真實 presence 點、可點的任務編號與 unread badge，**以及綁定任務的標題（獨立一行，hover 給全文）**；不顯模型與狀態字。⚠️ 標題那條不要照舊規則拔掉：2026-07-16 的「不顯標題」已被 owner 2026-07-23 推翻（T-3451），現行畫面由 `OutsourcePanel.test.tsx` 的 `outsource-task-title-<id>` 斷言釘住——照舊句去拔 title 會直接弄紅那一條。任務編號就是 task id 本身（T-5291 起不再截短），所以「不顯識別鍵」那條**不適用於它**——那串就是要給人抄走貼回去用的。排序以 task created_ts 為準，終態 worker 從 live list 消失。聊天使用 ow- id；header 可用 synthetic member，但不要在 chat header 重複 rail presence。那個 synthetic member 的 `lifecycle` 是 worker 的**真實 presence**（缺值落 offline），不是寫死的 online——外包聊天室因此跟正職走同一條路：非 online 時 composer 不鎖、顯示排隊提示與就地 ⚡喚醒，喚醒打的是 `restartWorker`（正職是 `activateMember`）。⚠️ 這兩半是一件事：只把寫死的 online 拿掉而不接 `onWake`，composer 會鎖死且沒有出口。不要為外包另開分支、元件或旗標。上限 -1 是無限、0 是暫停指派；settings 未載入時只顯目前數，不捏上限。

## 任務手冊

GET /api/task-manuals 的列表只靠 type_key。partial POST 中 null 是 no-op，assignee:{} 才是清除；非終態手冊不可刪。詳細頁可讀 definition，但不顯示內部檔名。

欄位要標 required/key；指派可為 member 或 outsource，並保留 model、effort、machine、copies 語意：copies=0 表示無限，machine 必須是實際機器，不自動 fallback，也不要送空 machine。離線時不自動改派。
