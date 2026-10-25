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

- **只剩一份狀態，除了編號**。四顆下拉（負責人／類型／狀態／到期）直接寫 applied，點下去就重跑。**編號欄位保留 draft/applied 兩份**：`draftId` 是框裡的字，`appliedId` 是清單真的被什麼篩，**只在 Enter 或 blur 時由 `commitId()` 提交**。⚠️ 這不是殘留的面板遺跡——編號會換一次伺服器請求（`useTasks(…, appliedId)` → `GET /api/tasks/{id}`），**每按鍵提交就是每按鍵發請求**，那正是 owner 連退兩次的形狀（「每次都要全部都撈回來才濾不合理」）。**把 `onCommit` 接到 `onChange` 看起來像整理，實際上是把那個否決倒回去**；`TasksPage.id-filter.test.tsx` 有一支專門釘住「打字但未 Enter 也未 blur ⇒ 列表不動」，mutant 驗過會紅。
- **shell 是 `FilterPanel`**（`components/FilterPanel.tsx`，自帶 CSS、無 `position: fixed`／scrim／z-index）。它現在只收 `testId` 與 `children`，不再持有任何狀態。欄位是 children，由頁面提供。「不是 modal」那條（`c-3b5a0aa66550`「按搜尋時不要再跳出新modal」）**沒有被推翻**，「全部拉出來」是它的同向延伸。
- **沒有「清除全部」鈕，也沒有「案件」子標題**。清空＝逐欄清空（下拉取消勾選、編號欄清空後 Enter）。
- 🔴 **全部條件一起生效，編號也不例外（選項①）**：applied 的編號**不篩已載入的清單**，而是走 `useTasks(DEFAULT_STATUS, appliedId)` 的 `GET /api/tasks/{id}` 拿那一張，再拿它去比其他 applied 軸。編號因此是**精準比對**，不是第二輪的 substring。
- **編號生效後的結局**（owner 2026-09-06 `rc-f603bbd447f4` 拿掉第三輪的兩則 by-id 專屬通知：「為什麼要顯示這種東西 拿掉!」「UI不是本來就秀0筆了嗎」）：
  - **404** 與 **抓到但被其他條件擋掉** → 都是一般的 `tasks-empty-filtered`，不得出 `tasks-empty`（那是在說工作區沒有任務）。
  - **抓到且通過** → 只顯示那一張。
  - 非 404 的失敗（500／離線）**不是 miss**：`tasks-error` 改講「沒有得到伺服器的回覆」，不得說找不到，兩個空狀態都噤聲。
- **`#tasks/<id>` 錨點**只是「種下 applied 編號」，且**同時把其他軸清空**——連結沒有對負責人／類型／狀態／到期表示意見。所以「連結指向已完成任務也跳得到」仍然成立，靠的不是錨點豁免狀態軸，而是那時根本沒有狀態軸；`statusAsk` 的 effect 在有 applied 編號時**跳過**，清單 ask 才不會被清空的狀態集合擴成全歷史（432 KB 那條回歸）。`#tasks/executor/<id>` 仍照舊顯式種負責人／類型／狀態（到期清空），並清掉編號。
- **沒有「未結案」「已結案」兩個區塊**（owner `rc-ceb98bb8884f` 選 A）：篩出來的任務是同一份清單，未結案在前（優先度排序）、已結案在後（結案時間新到舊），再照分組依據分 lane（同一個 lane 裡仍是未結案在前）。要看哪一邊只由狀態篩選決定，篩選最上面的「未結案」「已結案」兩列一次勾／取消一整組狀態（`MultiSelectFilter` 的 `groups`），選取剛好等於某一組時觸發鈕寫那一組的名稱，所以預設畫面寫「未結案」。兩列群組下面有一條分隔線（`tasks__ms-sep`），顏色取 `--color-text-muted` 而不是 `--color-border`：office 的 border token 等於卡片色，線會消失（owner `rc-f897cc1c8a5b` 看不到線）；`theme-contrast` 守衛量它在每個主題至少 2.5:1（1.5:1 時 owner 在試用站上仍幾乎看不到）。不要另加一顆未結案／已結案切換鈕：那會讓同一件事有兩個控制項，兩邊衝突時畫面是空的（owner 的「同一個功能的開關只放在一個地方」）。
- **預設不分組，是一份清單**（owner `c-4efb7d5d7812`「預設不要分組」），「分組依據」選單依序是 不分組／到期狀態／優先權／負責人／類型／狀態，「不分組」排第一（owner `c-e17508313a2c`）；「分組依據」可把它分成可收合的 lane（到期狀態：已逾期／即將到期／之後到期／沒有到期日，已結案的任務跟未結案一樣只看到期日分進這四區（owner `rc-4911902c169c` 選 B），所以已逾期 lane 的數量可以大於 nav 的逾期數——nav 計數不算已結案；優先權分組依 高／中／低／凍結（owner `rc-d80d981287af` 補上）；狀態分組也列已完成／終止／重複；另有負責人／類型）。分組依據不是篩選：它在篩選列最右端「顯示方式」按鈕打開的面板裡（owner `rc-a0fa7c9eca81` 選 B），不是 `FilterPanel` 的篩選欄位（它走 `actions`，排在欄位流的最後、靠右；放在欄位流裡是為了手機上和最後一行篩選同一行，不要搬回欄位外面自成一行），清除篩選也不重設它；因為平常看不到目前的分組，負責人／類型／狀態的 lane 標題前面帶軸名（`laneExecutorLead` 等，「負責人：Clara」），到期狀態不帶。到期 lane 的標題是純文字、不帶圖示，跟負責人／類型 lane 一樣（owner `rc-6795d312d76a`）；不要再做成到期色塊：卡上已經沒有到期色塊，標題另做一種會變成多一個符號。狀態 lane 的標題也是純文字（「狀態：尚未執行」），不用卡頭的狀態色塊：owner 要每種分組的標題長得一樣（`rc-f2f0cb7647df`「顯示方式又跟別人不同了」）。到期分類只走 `lib/dueDate.ts`，跟卡上「到期日」列的顏色同一套，換 lane 的時刻由頁面時鐘在下一個到期邊界喚醒；篩選先套用、再分組，沒有任務的 lane 不畫。篩選列不再有已逾期／即將到期這兩顆篩選鈕；到期條件是狀態後面的「到期」多選（只有已逾期／快到期兩項，owner `c-f15b233eced6` 要拿掉有到期日／沒有到期日，勾的選項彼此 OR、再跟其他軸 AND，空集合＝不限），在前端判定：已逾期／快到期用 `lib/dueDate.ts`、頁面的 `nowSec` 與有效快到期提醒，跟 lane 同一套、在同一個邊界換，已結案的任務永遠不算已逾期／快到期，所以已結案的逾期任務在已逾期 lane 裡，勾「已逾期」篩選卻篩不到它；清除篩選也清它。加了這一格之後，帶文字的「顯示方式」在 390px 差約 20px 放不進最後一行篩選，所以 520px 以下只顯示圖示（文字留在無障礙名稱裡）；CT `tasks-display-options` 在 390／360 量它和篩選同一行、在手機寬量文字寬度為 0。任務頁頂端沒有「已逾期 N · 即將到期 N · 進行中 N」摘要行（owner 後來要求整行拿掉，連同點了跳到 lane 的功能）；伺服器計數只剩 任務 nav 分頁的數字、警示三角形與提示在用，頁面只用 `useTaskCount().total` 判斷「目前沒有任務」。**只有第一個 lane 預設展開，其餘預設收合**（owner `rc-0b66903ff6b8`：一打開就看得到每一區的標題與數量）；預設展開的 lane 在套用預設的當下選定並記住 key（`defaultOpenKey`）：第一次有 lane 可畫時、換分組依據時、applied 編號清掉手動收合時，都選當下的第一區。之後新資料在它前面長出新的 lane（任務跨過到期邊界、SSE 推來新任務、全站快到期提醒載入改變分區）時，原本展開的那區保持展開、新 lane 預設收合，不會讓正在看的那區自己收起來；只有記住的那區被清空時，才改由當下的第一區展開，所以不會出現全部預設收合。手動展開／收合永遠優先。分組依據記在這個瀏覽器的 localStorage `oc_tasks_group_by`（owner `c-877f93fd5cb5`「顯示方式可以記住嗎？不要切了別頁就不見了」、`c-80e87066029a`「先不用跨設備沒關係」，所以不存伺服器、不跨裝置）：掛載時讀一次，沒存、值不在 `GROUP_BY_OPTIONS` 裡、或 storage 擲錯都是不分組；寫入失敗時這次掛載仍照選的分組。收合狀態與篩選都不持久化；applied 編號生效時清掉所有手動收合，那張任務所在的 lane 一定展開；這件事吃 applied 編號（不是 hash），打到一張任務並提交時若它的 lane 沒展開，畫面會出現「通過了卻什麼都沒有」的第四種沉默結局。
- **負責人選項的計數讀 applied**（不再有 draft 可與它矛盾）；計數 0 的選項隱藏，但**已勾選的保留**，否則取消不了。
- 測試一律走 `src/test/tasksFilter.ts`（勾＝生效；編號用 `applyIdFilter` 打字＋Enter、`typeIdFilter` 只打字不提交、`blurIdFilter` 點外面；勾了已結案狀態後，不分組的清單用 `waitForListedCards(n)` 等清單數到 n 張再讀卡；測 lane 的先 `chooseGroupBy("due")` 等，再用 `expandLane(key, n)`——已結案的卡跟未結案的同一個到期 lane，只等 lane 出現會在已結案那幾張還沒到時就讀卡），不要在各檔各自複製手勢。

## TaskCard

卡片預設收合；標題與 description 是唯讀 UI，owner 沒有要求編輯入口。進度與 gate 狀態直接使用 server 值；stepBadge 由 lib/stepBadge.ts 統一，superseded 不計入 progress。gate 預告、內嵌 TaskReplyCard、等待外部 banner 與任務訊息框都要維持原 wire 語意。

卡內（描述、步驟、備註）的附件連結可以下註解、回給負責人，步驟內嵌的請示卡則回給開卡人；契約見 overlays-and-modals.md〈附件連結〉。

到期日不在卡頭徽章列（那排只剩編號／優先權／狀態／類型），是 `.task-card__meta` 裡緊接在「建立者」下面的「到期日」列，收合時也看得到。值寫日期（`taskDueDate`：「2026/10/30（五）」／“Fri, Oct 30, 2026”）；存的時刻不是當地 23:59:59 時，日期後面空一格加 24 小時制 HH:MM（`taskDueDateTime`：「2026/10/22（四） 14:30」／“Thu, Oct 22, 2026 14:30”），判斷走 `lib/dueDate.ts` 的 `hasDueTime`。卡上任何地方都不寫「N 天後到期」「明天到期」「逾期 N 天」這種相對字樣；未結案任務已逾期時日期文字本身是紅色（`--color-danger`），在快到期提醒的時間內是橘色（`--color-dot-waking`），旁邊不加圖示（owner `rc-6795d312d76a`）；「已逾期 N 天」／「快到期」只放在視覺隱藏的 `task-card__due-status`，由日期的 `aria-describedby` 讀出，畫面上沒有 hover 提示；已結案只有日期、不上色。日期文字對它那顆 chip 的 AA（4.5:1）由 `theme-contrast` 守衛實量。可編輯時點日期開到期日 popover，沒有日期時值寫淡灰的「未設定」（`dueUnset`，`task-card__due-date--unset`），寫狀態而不是動作，跟負責人的「未指派」同一種寫法（owner `rc-4129a580e2a4` 選 A），點了一樣開 popover，`title` 是「設定到期日」；沒有日期又不可編輯就整列不畫。popover 固定 280px 寬（不固定的話提醒輸入會把它撐滿整張卡），第一行在任何寬度都是同一行的兩格選擇欄：日期格（`task-due-date-pick`）寫「2026/10/30」，沒有日期寫淡灰「未設定」（`dueUnset`），會伸縮；時間格（`task-due-time-pick`）寫 24 小時制「15:41」，沒有時間寫淡灰「不指定時間」（`dueTimeUnset`），照內容寬（owner `c-75a6c94e6925`「手機版變兩行了，另外我覺得不用特別給輸入，就都從日曆跟時鐘設定就好」）。日期與時間只從瀏覽器的日曆與時鐘設定，不能打字：每一格裡是透明疊在格子上的原生 `type="date"`／`type="time"`（`task-due-input`／`task-due-time-input`），點在格子上就點在原生欄位上，手機瀏覽器照原生行為開自己的選擇器，點擊時另外呼叫 `showPicker()`（桌面 Chrome 點欄位本體不會開日曆；不支援或擲錯時改成 focus 欄位）；欄位吞掉一般按鍵（數字、字母、方向鍵），Space 開選擇器，Enter 存檔、Escape 取消、Tab 照常移焦點，Ctrl／⌘ 組合鍵不攔；在時間欄按 Delete 或 Backspace 把草稿時間清成空白、popover 不關（owner `rc-42abf01b8125`「不能按delete就好嗎」，所以沒有另外的清除時間鈕），跟其他改動一樣等 Enter／離開才存；整個到期日的「清除」仍是底下那一項。焦點離開 popover 時只有落到別的元素（relatedTarget 不是 null）才存檔：手機自己的日期／時間選擇器開關時焦點會移到「沒有元素」，那時存檔會在使用者還沒填完時把 popover 關掉（owner 同一張卡回報「選了一個時間還沒填其他的就跳出來了」）；點 popover 外面仍由 document 的 mousedown 存檔。popover 最底下一列左邊是整個到期日的「清除」（有到期日時才有），右邊是跟 `InlineEdit` 同一組的 ✓（`task-due-apply`，存檔）與 ✗（`task-due-cancel`，取消）圖示鈕（owner `rc-b0a30dd5a22d`「還是可以有一個存檔或取消鍵」），所以 TaskCard 自己 import `inline-edit.css`。時間可不填，空白＝那天當地 23:59:59，填了＝那天當地的那一分（秒為 00），草稿沒有日期時時間與快到期提醒（數字與單位）都停用。再打開時，存的時刻是 23:59:59 就讓時間框空白，否則填 HH:MM；日期與時間都沒動就原樣保留存的時刻（API 設的到期時刻可能帶秒數），只改日期時沿用框裡的時間。逾期／快到期都比對確切時刻，所以有時間的到期日在那一分就轉紅，不是等到當天結束；「已逾期 N 天」仍數當地日曆日，同一天過了時間只讀「已逾期」。手機（≤720px）popover 從那一格的右緣往左展開，因為值欄從標籤欄後面才開始，往右展會超出卡片內容寬（CT `taskcard-due-keyboard` 在 320／390 量 popover、日期格、時間格同一行且都在卡內，快到期提醒的數字、單位與「前」同一行在卡內；在 1280 量 popover 不超過 300px 且日期與時間同一行；同一檔用真的鍵盤打字確認日期與時間的值不變，選擇器的選擇用 `fill` 模擬）。

到期日 popover 裡的「快到期提醒」一行讀作「[1] [天] 前」：數字輸入框、單位選單（`task-due-lead-unit`：天／小時／分鐘，en days／hours／minutes）、淡灰的「前」（`dueLeadBefore`，en before，owner `c-919bde3a6e5b`；設定頁 `param-due-soon-lead` 那一行也在單位選單後面接同一個「前」），存的是分鐘（`due_soon_lead_minutes`，1 分鐘到 30 天＝1..43200，跟設定頁 `param-due-soon-lead` 同範圍與同一組數字＋單位），不是預設選項的選單；空白＝跟全站設定（存 null）。跟全站設定時，數字框的 placeholder 只寫全站設定在目前單位下的數字（「1」，換成小時就是「24」），單位選單是全站設定的單位，兩者都是淡灰（`--color-text-muted`，單位選單掛 `task-card__due-lead-unit--default`）——灰色就代表跟預設（owner `c-38b7e84010bd`）；打了數字之後單位選單回到一般文字色。「全站設定 1 天」（`taskDueLeadWorkspace`）不顯示在畫面上，只在數字框空白時透過 `aria-describedby` 讀給螢幕閱讀器。把數字刪成空白時，單位選單立刻回到全站設定的單位、整行回到淡灰，先前選的單位不會留著（owner `c-e3762e269384`）；number input 解析不了的文字也報空字串，那不算刪除，單位不動。打開時已有的提醒用能整除它的最大單位顯示（1440→1 天、120→2 小時、90→90 分鐘，`lib/dueDate.ts` 的 `splitLeadMinutes`）；任何地方用文字寫提醒長度都走同一個自然單位（`taskDueLead`）。跟日期、時間一樣只在 Enter、焦點離開 popover、點外面時存檔，Escape 取消，焦點在 popover 裡的欄位之間移動（例如日期 Tab 到時間、數字 Tab 到單位）不算離開，換單位本身不存也不關 popover，打字途中的「1」（要打 14）不得存。不合法的值（數字不是整數、總長 <1 分鐘或 >30 天、或 number input 解析不了的文字——那種情況 `value` 是空字串，不可當成「跟全站設定」）不存：日期有改就照存，提醒維持原值，卡上用 `task-card__error` 顯示 `dueLeadRangeError`，因為 popover 已經關了；再打開 popover 時這句就清掉，不會在沒改任何東西之後還留著。

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

useOutsourceWorkers 只讀 `GET /api/members`（篩 `kind=outsource`；單筆是 `GET /api/members/{member_id}`）與 settings，並訂 member、task、chat、chat_read；不可加回 tasks 或 task-manuals 全歷史 join。server DTO 已帶 task_no、created_ts、type key/name。⚠️ **T-197 起沒有 `/api/outsource-workers` 這條列表路由，也沒有 `outsource_worker` 這個 SSE topic**（外包的指派／認領／釋出走 `member`）；讀到舊句不要照著接回去。外包與正職共用同一份 `MemberDTO` 投影——**要多帶一個欄位就加在那一份上，不要為外包另開第二份**。

外包列顯示 O- 代號、task type 加真實 presence 點、可點的任務編號與 unread badge，**以及綁定任務的標題（獨立一行，hover 給全文）**；不顯模型與狀態字。⚠️ 標題那條不要照舊規則拔掉：2026-07-16 的「不顯標題」已被 owner 2026-07-23 推翻（T-3451），現行畫面由 `OutsourcePanel.test.tsx` 的 `outsource-task-title-<id>` 斷言釘住——照舊句去拔 title 會直接弄紅那一條。任務編號就是 task id 本身（T-5291 起不再截短），所以「不顯識別鍵」那條**不適用於它**——那串就是要給人抄走貼回去用的。排序以 task created_ts 為準，終態 worker 從 live list 消失。聊天使用 ow- id；header 可用 synthetic member，但不要在 chat header 重複 rail presence。那個 synthetic member 的 `lifecycle` 是 worker 的**真實 presence**（缺值落 offline），不是寫死的 online——外包聊天室因此跟正職走同一條路：非 online 時 composer 不鎖、顯示排隊提示與就地 ⚡喚醒，喚醒打的是 `activateMember`（T-197 起兩邊同一支——`restartWorker` 這個 adapter 方法已經不存在，server 端由 member handler 看 `kind == outsource` 分流）。⚠️ 這兩半是一件事：只把寫死的 online 拿掉而不接 `onWake`，composer 會鎖死且沒有出口。不要為外包另開分支、元件或旗標。上限 -1 是無限、0 是暫停指派；settings 未載入時只顯目前數，不捏上限。

## 任務手冊

GET /api/task-manuals 的列表只靠 type_key。partial POST 中 null 是 no-op，assignee:{} 才是清除；非終態手冊不可刪。詳細頁可讀 definition，但不顯示內部檔名。

欄位要標 required/key；指派可為 member 或 outsource，並保留 model、effort、machine、copies 語意：copies=0 表示無限，machine 必須是實際機器，不自動 fallback，也不要送空 machine。離線時不自動改派。

內建手冊隨 OffiCraft 出貨（`is_seed`），使用者另外可以建立自訂手冊。列表以分組標題「內建／自訂」區分（沿用 ThemeSettings 的做法與 `themeMarkers` 用詞，列上不加徽章），自訂組只在有自訂手冊時出現；內建列的刪除鈕保留但停用，伺服器也會以 403 拒絕。編輯內建手冊是覆蓋在出廠版本之上（`is_default` 轉 false），出廠版本不會被改寫。重置只有一個入口：SOP 版本紀錄的「預設內容」，只對內建手冊出現；它會把整本手冊（含負責成員）還原成出廠版本，所以確認文字用 `historyManualSeedConfirm`，不要換回只講 SOP 的那句。
