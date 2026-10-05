# 外包詳情面板 ↔ 正職成員詳情面板 逐項落差盤點（T-7526 步驟 1）

盤點基準 = `origin/main` @ `acac15a0ea56d28bac4f19f2fe4791459e79074e`，**逐行讀原碼核對**，不採信任何二手描述。

涉及三個檔：

- 共用面板 `frontend/src/components/AgentDetailPanel.tsx`（卡片骨架 + 統一 view model `AgentDetailVM`）
- 正職 wrapper `frontend/src/components/MemberDetailPanel.tsx`（T-927a 已改：面板唯讀、設定收進喚醒區）
- 外包 wrapper `frontend/src/components/WorkerDetailPanel.tsx`（未動）

> ## ⚠️ 2026-08-12（T-0b4f）起，這份文件**不再是「哪個插槽兩邊各傳了什麼」的權威**
>
> 那一半已經改由**型別**強制：共用面板的插槽從 optional props 換成一份必填的
> `AgentDetailSlots`（`AGENT_DETAIL_SLOTS` 是唯一的 key 清單），**兩個 wrapper 都必須對
> 每一個 key 表態**，漏一個是編譯錯誤、而不是畫面上一格靜默的空白。「這一邊不要」是一個
> 帶理由的值 `notHere(<why>)`，理由就寫在該 wrapper 的碼裡。
>
> ⇒ **要看某個插槽今天在兩邊各是什麼，不要讀這份文件的敘述——跑這條查詢**（清單會過期，
> 查詢不會）：
>
> ```
> command grep -an "slots={" -A 20 frontend/src/components/MemberDetailPanel.tsx \
>                                   frontend/src/components/WorkerDetailPanel.tsx
> ```
>
> （`command` 是為了繞過某些機器上被包過的 `grep` 函式，`-a` 讓它不會把檔案judge成
> 非文字而靜默跳過。`-A 20` 而不是剛好夠用的行數——獨立審查指出原本的 `-A 12` 距離
> 截斷只差一行，理由字串多一行就會靜默少印一個 key。）
>
> 這份文件仍然是**下面那些逐項裁定的家**（哪些差異是刻意的、理由是什麼）——那是型別答不出來的。
> 設計與 mutant 證據見 `T-0b4f-exhaustive-slots-mutants.md`。

> ## ⚠️ T-197 起，下面表格裡出現的**外包專屬 wire 名詞全部不存在了**
>
> 中間那一層收成一條之後：
>
> - **沒有 `OutsourceWorkerDTO`**。外包與正職共用 `MemberDTO` 一份投影（`GET /api/members`
>   ＋`kind=outsource` 篩選，單筆是 `GET /api/members/{member_id}`）。漏一個欄位現在是編譯
>   錯誤，不是靜默清成零值。
> - **沒有 `/api/outsource-workers/{id}/…` 這一族路由**（list／單筆讀／relocate／refocus／
>   stop／restart／model／accelerated-stop／force-stop 九條），也**沒有與它們同名的 MCP 工具**。
>   唯一留著 worker 命名空間的是 `GET /api/outsource-workers/{id}/boot-context`。動作打的是
>   同名的 member 路由；停止類（停止／加速停止／強制停止／回報停止）兩種成員已是同一個函式
>   （`member_stop.go`），其餘動作 member handler 看 `kind == outsource` 分流進 worker body。
> - **沒有 `outsource_worker` SSE topic**（closed set 從 12 收成 11）；外包的指派／認領／釋出
>   走 `member` topic。
>
> ⇒ 下面每一格裡的 `OutsourceWorkerDTO` 與 `/api/outsource-workers/{id}/…` 都要讀成
> 「**當時**的名字」。外包與正職今天只剩三處刻意的差異（怎麼被生出來、任務綁定的那幾個欄位、
> 只有外包才有的那幾張卡），每一處在碼裡具名寫了理由。

⚠️ **下面這段是本文件寫成當時（`acac15a`）的觀察，其中一半已經不成立**（T-0b4f 逐句核對）：

- ~~`AgentDetailVM.onSaveModelEffort` 未傳 ⇒ 模型格不長編輯鈕（`AgentDetailPanel.tsx` 的
  `{vm.onSaveModelEffort && (<button …model-effort-edit>)}`）~~ —— **已不成立**：那顆就地編輯器
  連同 `onSaveModelEffort` 這個 prop 本身，已於 **T-7f28 整個移除**。留著這句會讓下一個人去找
  一段不存在的碼。
  ⚠️ 這裡原本寫「今天原碼裡零命中」，**那句話不準**（獨立審查抓到）：這個符號在文件與註解裡
  仍有若干處提及（本檔自己、`frontend/CLAUDE.md`、`AgentDetailPanel.tsx` 一句記錄「它被移除了」
  的註解、`worker-panel-parity-mutants.md`）。
  🔴 **這裡刻意不寫「幾個檔」**：那個計數我連錯三次（零 → 兩處 → 五個檔），而第三次還把一個
  **只存在於某台機器的 build cache 產物**算了進去——**一句在乾淨 checkout 裡不可複現、會隨本機
  狀態變動的計數，被寫成了事實**。⇒ **會過期又會數錯的是「命中幾處」；不會過期的是下面那個
  準確說法**，留那個就好。**準確的說法是「型別與 production 碼裡沒有這個 prop」**，
  不是「repo 裡零命中」。
- `vm.machineAction` 未傳 ⇒ 機器格標題右側無任何控制項 —— **仍然成立**（該欄位今天仍是
  `AgentDetailVM` 上的 optional prop）。
- 「共用面板的鍵由 wrapper 傳不傳決定」這個**總結句**，對**插槽**已不再成立（見上方 T-0b4f
  的說明）；對 `machineAction` 這類 **view-model 上的 optional 欄位仍然成立**。
  ⚠️ **這是一個已知的、還沒收掉的缺口，不是本票的範圍**：view model 自己的 optional 欄位
  沒有跟著插槽一起變成必填，所以「兩邊都得表態」目前只涵蓋插槽那一層。要不要把同一個形狀
  套到 view model 上，**交 owner 裁**。

狀態欄位說明：**同**＝行為與外觀已一致｜**差**＝需要對齊｜**外包獨有**｜**正職獨有**｜
**待裁定**＝說不出明確期望，交回 owner。

---

## A. 身分卡（identity slot）

| # | 項目 | 正職有什麼 | 外包有什麼 | 差在哪 | 期望行為 |
|---|------|-----------|-----------|--------|---------|
| A1 | 返回鍵 | `mp__back`（共用面板畫） | 同 | 同 | 維持共用，不動 |
| A2 | 頭像上傳／移除 | `AvatarEditor`，未傳 handler 時降級成唯讀 `Avatar` | 同（kind=`outsource`） | 同 | 維持 |
| A3 | 名字 | `InlineEdit` 就地改名 → `onRename` | 無；顯示系統建立的代號 `msg.outsourceLabel(codename)` | 差（結構性） | **保留現狀**。外包代號是系統建立的匿名識別，不是人取的名字；給外包改名等於發明一個後端沒有的欄位 |
| A4 | 身分 id chip | `member.id` badge（T-5dab 起顯示真 id；在那之前是由 id 推導的 `MB-XXX###` 標籤） | 無 | 差（結構性） | **保留現狀**，理由同 A3（代號本身就是識別） |
| A5 | presence 指示 | `PresenceBadge`（點＋角色名） | `LifecycleDot` + `presenceVisual`（同一份映射） | 視覺元件不同、映射同源 | **保留現狀**：`frontend/CLAUDE.md` 明文「presence→視覺的推導只有一份」，兩者都走 `presenceVisual`，未漂移；外包沒有角色名可顯示，套 `PresenceBadge` 會多出一個空欄 |
| A6 | 任務 chip（任務編號，T-5291 起即 task id 本身）+ 任務類型 | 無 | 有，可點 → `#tasks/<id>` | 外包獨有 | **保留**。外包的「角色」就是它綁的任務類型，這是 rail 列形的同一條裁定（`frontend/CLAUDE.md` 外包面板節），移除等於拔掉外包唯一的身分線索 |
| A7 | 動作鍵列（喚醒／停止／加速停止／強制停止） | `MemberActionButtons`，依 `visual` 五態切換按鈕集合 | 同一個 `MemberActionButtons`，前面接 `worker-detail-change`；沒有活 session 時換成 `worker-detail-wake` | 同 | ✅ 喚醒中兩邊都是「更改＋停止」（owner `rc-8b3d3a366c54`）；停止中兩邊都是「更改＋階梯」、**沒有喚醒**（owner `rc-2e1c96250169`：「停止中不提供喚醒，等它停完再喚醒」，聊天室 ⚡喚醒同）。停止／加速停止／強制停止任一失敗，兩邊都顯示 `mp.stopError`「操作失敗，請稍後重試」，強制停止確認框同時關掉（owner `rc-b2c4fbb5f2b1`） |
| A8 | 「更改」鍵 | `mp-change`，線上、停止中與喚醒中出現，開啟動設定 dialog | `worker-detail-change`，有 session（喚醒中、線上、停止中）時出現，開同形狀的設定 dialog | 同 | ✅ 喚醒中的更改只存設定：換了機器會移到新機器、用新設定重新開起來，只換型號／執行環境／思考強度則下次開起來才生效。⚠️ 換機器的生效時機兩邊不同：外包當場殺掉重派；正職等它開起來後收尾、再到新機器開（或這次開機逾時後下一次直接派到新機器）。註記只寫兩邊都成立的那一句 |
| A9 | 未派送警示 | `DispatchAlert`（`mp-wake-undispatched` / `mp-relocate-undispatched`） | `DispatchAlert`（`worker-detail-wake-undispatched` / `worker-detail-relocate-undispatched`；外包聊天室 ⚡喚醒同正職，`chat-wake-undispatched`） | 同（一處刻意不同） | ✅ owner `rc-e0207591ae4d`：同一則警示。喚醒讀 `activation_pending`；換機器讀 `relocation_pending && !relocation_deferred`。⚠️ 外包的 `relocation_pending` 在 desired_state=offline（已停止、或排在停止後）時也是 true——那是刻意排到下次開機，不是失敗，所以外包只在 desired_state 不是 offline 時才看換機器回執；喚醒模式不送換機器（機器由喚醒自己帶著），只看喚醒回執 |

## B. 模型／機器 資訊卡（共用面板 `mp-info2`）

| # | 項目 | 正職有什麼 | 外包有什麼 | 差在哪 | 期望行為 |
|---|------|-----------|-----------|--------|---------|
| B1 | AI 執行環境 / 模型 / 投入度 顯示 | 唯讀。`model` 餵 `awake ? member.actualModel : ""`，並掛 `modelIsReported: true` ⇒ 值旁標「最近一次開機回報」 | 唯讀顯示 + **一顆鉛筆「編輯」鍵**（`worker-detail-model-effort-edit`），就地展開 `ModelEffortEditor` | 差 🔴 | **拿掉就地編輯**：wrapper 不再傳 `onSaveModelEffort`；改設定一律走 A8 的 dialog |
| B2 | 模型值的語意 | REPORTED（agent 開機回報的實際值） | ~~CONFIGURED（`worker.model`，owner 意圖值）~~ **REPORTED，同正職** | ~~差~~ **已對齊** | ✅ **已裁定（`rc-b8d219446b13` [0]）：兩邊都標「最近一次開機回報」**。原本寫的「外包 DTO 沒有 `actual_model`、硬掛 `modelIsReported` 會是假話」**今天不成立**：`OutsourceWorkerDTO` 自 T-7f28 起就有 `actual_model`（wire 見 `spec/openapi.json`；client 見 `frontend/src/api/adapter.ts` 的 `actualModel` 與 `mappers.ts` 的 `w.actual_model`），外包讀得到回報值。`frontend/src/lib/agentDetailVm.ts` 對兩側一律 `modelIsReported: true`，是有依據的敘述。owner 意圖值不會因此消失——它仍是 A8 設定 dialog round-trip 的 `worker.model` |
| B3 | 機器格 | 唯讀。`machineText = awake || stopping ? machineName : ""`（online／waking／stopping 以外一律 dash） | 唯讀。在線／喚醒中／停止中寫 `worker.machine`（alias‖id）經名冊解析後的顯示名稱，離線／已停止（以及從沒派出去的）寫 dash | 同 | ✅ owner `rc-25c5679371c3`：「正職外包都顯示「—」：機器欄只講「現在在哪台跑」，沒在跑就是「—」」。機器改在更改／喚醒 dialog 內選 |
| B4 | 遷移中提示 | `pendingMachineHint`（`lib/pendingChange`）：回報側先看即時 `machine`、沒有才看 `actual_machine`；兩側都對得到名冊的機器就比 id，否則比名冊顯示名稱 | 同一個 helper | 同 | 兩台機器名冊名稱相同、釘在其中一台而跑在另一台時，兩邊都提示「→ 要換到」 |
| B5 | 「更換中…」／逾時／失敗回執 | **無**（正職 T-927a 已改走 dialog，`useRelocateMachine` 不再驅動正職面板） | 有（`useRelocateMachine` 的 `phase`：relocating / timeout / failed + 伺服器回執原文） | 外包多 | 🔴 **待裁定**。拿掉 B3 的就地鍵＝連帶拿掉這整組進度／逾時／回執的顯示，這是**外包目前獨有、正職沒有**的可觀測性。步驟 2 會用 dialog 內的錯誤行（同正職 `settingsError`，顯示 `ApiError.serverMessage`）承接**失敗**那一半，但**非同步落地的「更換中…」與 30s 逾時判定會消失**。這是「與正職同一套形狀」的直接後果，仍請 owner 明示認可 |
| B6 | Claude / Codex Account | 唯讀，`awake && member.account` 才顯示 | 唯讀，`worker.account \|\| ""` | 同（gate 條件不同但都誠實） | 維持 |

## C. 外包獨有卡片

| # | 項目 | 正職有什麼 | 外包有什麼 | 差在哪 | 期望行為 |
|---|------|-----------|-----------|--------|---------|
| C1 | 狀態欄 + 停止／喚醒 | 狀態字收在 `PresenceBadge`；停止走 A7 的 `MemberActionButtons` | ~~`worker-detail-status` 欄 + `worker-detail-stop-toggle`~~ **狀態欄已刪、鍵已搬到身分卡動作列** | ~~位置不同~~ **已對齊** | ✅ **owner 2026-07-31 裁定**：狀態欄整個退場（見下方裁定段），鍵搬到身分卡右上角 |
| C2 | 離線原因 | 無對應（正職走 `最近操作` 卡） | `worker-detail-stuck-reason`：presence=offline 時攤 `lastOpReason`；**狀態欄刪掉後移到身分卡的點下面** | 外包多 | **保留**（位置改了、東西沒少）。理由：spawn 默默失敗時光一個灰點對 owner 無資訊，而 `最近操作` 卡只在 `lastOp` 非空時才渲染——「從沒派出去」正好就是它不渲染的情況 |
| C3 | 委託人 | 無 | `worker-detail-delegator`（真實建票人／系統排程 fallback） | 外包獨有 | **保留**。外包是系統代 owner 生出來的，「誰委託的」是外包才有的來歷資訊，正職沒有對應概念 |
| C4 | 委託任務卡 | 無 | `worker-detail-task`，可點 → `#tasks/<id>` | 外包獨有 | **保留**。外包與任務一對一綁定（任務終態即 release），這是外包存在的理由本身 |

## D. 正職獨有

| # | 項目 | 正職有什麼 | 外包有什麼 | 差在哪 | 期望行為 |
|---|------|-----------|-----------|--------|---------|
| D1 | 喚醒（`spawn`／`t.lifecycle.action.spawn`） | 離線／已停止提供，開設定 dialog 後 `activateMember`；喚醒中、停止中不提供 | 同（同一支 `activateMember`，伺服器依 `kind` 分流） | 同 | ✅ 喚醒中、停止中見 A7。session 自己死掉的外包（`desired_state` 仍是 online）也叫得起來：座艙 `noLiveSession = stopped \|\| offline` |
| D2 | 喚醒中的停止 | 「停止」→ `deactivateMember` | 同一顆「停止」→ 同一支 `deactivateMember` | 同 | ✅ owner `rc-8b3d3a366c54`：喚醒中兩邊都是「更改＋停止」。伺服器兩種成員都收喚醒中的停止（外包 `resolveLiveWorkerOn` 只擋已釋出） |
| D3 | 強制停止 + 二次確認 | `stopping` 時 Stop 升級為 force-stop，`mp-force-stop-confirm` modal | ~~無此端點~~ **已補（T-ed79，owner 2026-08-21「強制殺移到第三顆按鈕」）**：`POST /api/outsource-workers/{id}/force-stop` ＋ `worker-detail-force-stop-confirm` modal | ~~差~~ **已對齊，而且比 D3 原本問的更多** | ✅ **已完成，不再是開放問題**。正職那邊的形狀本身也變了兩次：`stopping` 先是不再「Stop 升級成 force-stop」而是三顆固定位置的階梯，接著 owner 2026-08-21 又把「三顆固定、變灰」推翻成**按了才出現**——沒輪到的那一顆不 render。外包這一列直接 render **同一個 `MemberActionButtons`**，連「這個成員爬到第幾階」都是同一個 `stopLadderStageOf` 讀同一組 wire 欄位（presence／desired_state／refocus_since／refocus_op／forced_stop_live），所以字、順序、哪一顆存在都是同一份實作，不會再各自漂。外包的 `/stop` 也同時從「當場砍」改成優雅收工（見 `offboard-flow.md`）。護欄：`worker_graceful_stop_ted79_test.go`、`WorkerDetailPanel.test.tsx` 的「停止 → the worker goes 停止中…」與「強制停止 ASKS FIRST…」 |
| D4 | 「只儲存，不喚醒」 | 無 | 無 | 同 | ✅ owner `rc-baa00a00dbc9`：「拿掉「只儲存，不喚醒」，不改介面」，兩邊都沒有 |
| D5 | 設定意圖註記 | `mp-settings-intent-note` | `worker-detail-settings-note` | 同 | ✅ owner `rc-71d6a9d0ce54`：依框的種類與狀態只顯示一句（`lib/agentDetailVm` 的 `settingsNoteKey`）——更改框在線 `mp.settingsNoteOnline`、停止中 `mp.settingsNoteAfterStop`、喚醒中 `mp.settingsNoteWaking`；喚醒框 `mp.settingsNoteWake`。有回報模型時兩邊都接 `mp.settingsIntentNoteReported` |
| D6 | 回呼端點 · WEBHOOK 卡 | `WebhooksCard`，掛在 `extraExpandCards`（列表／啟停／建立／刪除／簽章輪替／事件統計） | 同一個 `WebhooksCard` | 同 | 伺服器 `create`／`update`／`revoke` 吃 `anyMember`（`api_webhooks.go`），`00101_webhook_revoke_on_member_exit.sql` 的 trigger 讓成員一退場（正職 soft remove，含刪角色／外包 release）端點就跟著消失 |
| D7 | 改名 | 有 | 無 | 見 A3 | 同 A3 |

## E. 共用卡片（已一致，列出以示涵蓋完整）

| # | 項目 | 狀態 |
|---|------|------|
| E1 | 運行狀況 · context% | 同（`vm.contextPct`；codex 另帶壓縮次數） |
| E2 | 重新聚焦鍵（`*-refocus`） | 同（兩邊都傳 `onRefocus`；只有 presence=online 可按，停止中／已停止都灰、寫「僅線上可重新聚焦」——owner `rc-137e6bf933d5`，同一條判斷在 `lib/agentDetailVm`） |
| E3 | 估計 $（live + banked 合計） | 同（兩邊 wrapper 都用同一口徑折算） |
| E4 | 最近操作回執卡 | 同（含失敗原因、記錄展開） |
| E5 | 終端 · TMUX 複製鍵 | 同（session 名分別為 `member.tmuxSession` / `member-<workerId>`） |
| E6 | 初始 PROMPT 展開卡 | 同（正職按成員 id 抓 `/api/members/{member_id}/boot-context`，外包按 id 抓 `/api/outsource-workers/{id}/boot-context`，兩邊都帶誠實 caveat 註記） |
| E7 | 履歷摘要展開卡（`mp-resume-*`） | 同（T-4595：兩邊都畫共用的 `ResumeSummaryCard`，同一個 `getMemberResumeSummary(agentId)`、同一組 test id、同樣展開才抓）。⚠️ 這一列**在 T-4595 之前是「差」**——外包那格是 `notHere()` 佔位，因為 `resolveMember` 對 `KindOutsource` 回 404；server 端多開一支 `resolveResumeSummaryTarget`（**只**放行這一支動詞的 `ow-` id，其餘 member 動詞的 floor 一字未動）之後才共用得起來 |

---

## 「待裁定」清單（交回 owner）

> **狀態**：下表沒有開放項目。
> 已關掉的：**D1**（owner 核可並已實作完成，見上表 D1 列）、**B5**（owner 明示核可，見下方裁定段）、
> **C1**（owner 2026-07-31 裁定，見下方「owner 2026-07-31 四項裁定」）、**A9**（T-ed79 #5／#12 補了三個
> optional 欄位）、**B2**（owner `rc-b8d219446b13` [0]；前提早在 T-7f28 加欄時就過期）。
> 此表與上面的逐項表、與下方連帶後果段**必須同批更新**——文件把已完成的事仍標成待裁定，
> 下一個人就會拿它去問一個已經有答案的問題。

| 代號 | 一句話 |
|------|--------|
| A9 | ~~外包 relocate 的 wire 回傳沒有 `relocation_pending`，無法對齊正職的「已釘選但沒派出去」警示。要對齊＝改凍結 wire~~ → T-ed79 #5／#12 已補上三個 optional 欄位（T-91 起改由 relocate／restart 的專屬回執攜帶，不再是讀取 DTO 的欄位），見上表 A9 |
| B2 | ~~外包 DTO 無 `actual_model`，模型格無法像正職那樣標「最近一次開機回報」。要不要加欄？~~ → 欄早在 T-7f28 就加了，owner `rc-b8d219446b13` [0] 裁定兩邊都標，見上表 B2 列 |
| B4 | ~~要不要補「→ 要換到 ○○」遷移提示？~~ → 已有，兩邊同一個 `pendingMachineHint`，見上表 B4 |
| D2 | ~~`waking` 的外包無「取消喚醒」~~ → owner `rc-8b3d3a366c54`：喚醒中兩邊都是「更改＋停止」，見上表 D2 |

### B5 的裁定結果與連帶後果

✅ **B5 已由 owner 明示核可**（「進度顯示拿掉、對齊成正職的形狀」）：拿掉機器格的就地「編輯」鍵，
連帶失去外包原本獨有的「更換中…／30s 逾時／伺服器回執原文」進度顯示；失敗那一半由 dialog 的
錯誤行（`ApiError.serverMessage`）承接。以下是它的下游後果，不是新的待裁定：

- **`frontend/visual-guards/relocate-progress-720.ct.spec.tsx`**（整支 spec）已移除 —— `mv` 進
  `trash/T-7526/`，**trash 裡就只有這一個檔**。理由與 T-927a 移除**正職那一半**時逐字相同：
  外包面板不再有 改機器 鍵、「更換中…」字樣與 30s 逾時提示，這份量測不是紅、是**無從表達**；
  而外包的 機器 標題列現在完全沒有控制項，沒有寬度風險可量。
- **`frontend/visual-guards/stories/RelocateProgressStory.tsx` 沒有被移除，是就地編輯**：只刪掉
  `WorkerRelocateProgressStory` 這一個 export 與它的 worker fixture／import。檔案本身還在、仍在
  CT 跑，因為 `member-machine-transition.ct.spec.tsx` 還在用同檔的 `MemberMachineTransitionStory`。
- `useRelocateMachine.tsx`（連同 `MachinePicker.tsx`）在本次改動後**已無任何 production
  importer**（僅剩自己的測試與 `MemberDetailPanel` 註解裡的 twin-implementation 交叉引用）。
  依 §9(a) 這是該清的 legacy，但刪掉一個 hook ＋ 它整份測試 ＋ 一個元件，範圍遠大於本票，
  **列為 follow-up 交 owner 裁定，本票不刪**。
  <br>**後續（T-170e）**：該 follow-up 已執行 —— `useRelocateMachine.tsx`、它的 colocated 測試、
  以及隨之變成孤兒的 `MachinePicker.tsx` 都已刪除。上面這段是 T-7526 當下的記錄，保留不改寫；
  今天讀到這兩個檔名時，樹上已經沒有它們。

**沒有任何一格因為我判斷「該移除」而被移除。** B1 / B3 兩項就地編輯鍵的移除，是派工單步驟 2
DoD 第 1 條明文指定的改動，且**能力本身沒有消失**（改模型、改機器都改由 A8 的 dialog 承接）。

---

## 步驟 2 實作範圍（依上表推導）

1. `WorkerDetailPanel` 不再傳 `onSaveModelEffort`（B1）與 `machineAction`（B3）給共用面板。
2. 身分卡動作列新增「更改」鍵（A8），開一份與正職同形狀的設定 dialog：
   `ModelEffortEditor`（執行環境／模型／投入度）＋ 機器 `<select>`（線上機器 ＋ 自己那台離線釘選，
   標「離線」且 disabled），底部 取消／更改。
   <br>**這條規則今天仍然有效，出處已經換過兩次**。當年寫的是「照 `MachinePicker` 的規則」，
   而那個元件已經刪除（T-170e）；接手的寫法是兩支面板**各自實作**一份 `pinnedOfflineMachine` ＋
   `settingsMachineOptions`，並把「互為對照、要 audit 請比對那兩段」當成稽核手段。
   <br>🔴 **那句話從 2026-08-28 起是假話，不要照它推理**：owner 於 `rc-fc9ab61ad057` 選 [2]
   「合掉就好，接受少一個 audit 手段」，親自翻掉了「刻意留兩份互為對照」那條裁定（T-14 項目 2）。
   今天只有**一份**實作：`frontend/src/lib/agentDetailVm.ts` 的 `machineOptions()`，兩支面板都呼叫它。
   <br>**合掉之後靠什麼 audit**：① 讀 `machineOptions()` 本身 —— 它是唯一一份，沒有第二段可以漂走；
   ② 它的測試 `frontend/src/lib/agentDetailVm.test.ts`（線上機器、釘選離線機器、釘選不在 registry、沒有釘選 四種形狀）；
   ③ mutant 判準 —— 改 `machineOptions()` 一處，**兩支面板的測試必須同時紅**；只紅一邊就代表某一側又長回了自己的副本。
   ⚠️ 那一份只承接「留在清單裡 ＋ 標離線」；**`disabled` 那一半不在裡面**，它在各自的 `<option disabled={machine.offline}>`，要一起看。
3. 確認送出：`launchChanged` → `api.setWorkerModel`；`machineChanged` → `api.relocateWorker`。
   PATCH 先於 relocate（正職 `saveSettings` 的同一條理由：relocate 會重生 session，
   設定必須先落地，否則新 session 用舊模型起來）。
4. 失敗顯示 `ApiError.serverMessage`，fallback `t.mp.modelEffortError`（正職同一條）。
5. 切換到另一個外包時關掉 dialog、清錯誤（正職 `[member.id]` effect 的同一個理由：
   兩個呼叫端都沒傳 `key`，dialog 會帶著上一個外包的草稿活下來）。
6. `docs/guide/members.md`「你能做的幾個動作」逐條標明適用面板，讓每一句對外包為真。

---

## owner 2026-07-31 四項裁定（第二輪）

owner 凌晨連續下了四條，逐條落地如下。四條共同的方向是「**外包沒有理由跟正職長得不一樣**」。

### ① 「全部變成左右並排」——更改 ＋ 停止 同一列

`.mp-identity__actions` 原本是 `flex-direction: column`：它當初是為了讓狀態按鈕列**疊在**已退役的
「更換機器」之上，「更改」後來繼承了那個下方位子。現在 buttons 抽成 `.mp-identity__buttons`
（row / wrap / justify-end），`.mp-identity__actions` 維持 column **但只裝 `DispatchAlert`**
——關於某次點擊的判決要在那顆按鈕**下面**，不是旁邊。

⚠️ **≤720px 的 media query 是一起改的，不是順帶**：舊規則
（`align-items: stretch` + `.member-actions { width: 100% }`）是為了把一個 **column** 撐開；
原封不動套在 row 上，`justify-content: flex-end` 會讓兩顆鍵擠在右邊界——owner 明講不要的那個。
當時窄螢幕下用 `.mp-identity__buttons > * { flex: 1 1 0 }` 讓兩顆均分整張卡的寬度。
護欄：`visual-guards/identity-actions-row.ct.spec.tsx`（desktop ＋ narrow 兩個 viewport）。
**兩個 viewport 各自承重**，見 mutants 檔的 R1 / R2。

🔴 **T-ed79 之後，窄螢幕那一半不再成立**：owner 2026-08-21 把單顆 停止 換成
停止 → 加速停止 → 強制停止，這一列變成四顆鍵。`flex: 1 1 0` 是把卡片對半分給
更改 和「整組 `.member-actions`」，階梯在自己那半塞不下三顆四字鍵就往下疊三層，
父層的 `align-items: center` 再把那塊方塊對著單顆 更改 垂直置中——結果 停止 跑到
更改 **上面**。四顆同列在窄寬度做不到（階梯自然寬 ~300px，375 的卡片內寬 289），
所以 ≤720px 改成 `flex: 1 1 100%` ＋ `align-items: stretch`：**一個直接子元素一條帶**，
帶一 更改、帶二 整條階梯三顆均分。desktop 一列四顆不變。護欄跟著換釘的事實
（desktop 一列四顆 / narrow 刻意兩帶 ＋ 兩個寬度都要「不重疊、不出卡片、標籤不被裁」），
斷言沒有放寬，見 mutants 檔「T-ed79 重測」那節的 R1／R2／R3'／R4／R5。

🔴 **「按了才出現」之後這一列不再是固定顆數**：2／3／4／5 顆都會出現（沒有收尾＝更改＋停止；
系統開的軟下線＝再加 加速停止；owner 按過 停止＝多一顆 喚醒 救援；上了時鐘＝再加 強制停止）。
CT 護欄因此改成量**四種形狀**，五顆那個最寬的 case 原封不動留著，另一端補上兩顆的 case——
容忍度、矩形交集、裁字檢查一條都沒動。

### ② 「外包為什麼需要工作狀態這個UI介面」——狀態卡退場

`statusDelegatorCard` 的狀態格顯示五種字，其中**四種**（工作中／啟動中／已停止／離線）與身分卡那顆
`LifecycleDot` 完全重複——同一個事實寫兩次，而且是第二個會從 `presenceVisual` 漂走的地方。
第五種「已釋放」是 released worker。**owner 原本明示**聊天室那條橫幅已經講夠、面板不必再留
—— 這一條**在同一天稍後被 owner 自己推翻**，見下方「② 的後續：已結案要由身分那一層講」。

⇒ 狀態格、`t.workerDetail.status` / `starting` / `offline` / `working` / `stopped` /
`statusOf`、以及只為餵它而存在的 `compose.ts::workerStatusText` **一起刪掉**。
剩下的卡只有「委託人」（外包獨有、別處沒有），所以不再是 `.mp-info2` 兩欄格，改成單欄 `.mp-card`。

⚠️ **有一樣東西刻意沒跟著刪**：`worker-detail-stuck-reason`（離線原因）。
它不是狀態字的複述，而是「為什麼是灰的」的唯一答案；而 `最近操作` 回執卡是
`hasLastOp` gated 的，**「一次都沒派出去」剛好就是它不渲染的情況**。
所以它搬到身分卡、掛在那顆點底下。

### ③ 「應該要統一」——「重啟」退場，一律叫「喚醒」

`t.workerDetail.restart` / `restarting` 兩片葉子刪掉；外包的喚醒字直接用正職那一份
`t.lifecycle.action.spawn`＝「喚醒」。**兩個面板同一個葉子**，主題包換詞只換一次。

🔴 **當時 REST 路徑一個字都沒動**：是 `POST /api/outsource-workers/{id}/restart`
（凍結 wire，§13），`api.restartWorker` 也維持原名。只有 panel 的 prop 從
`onRestart` 改成 `onWake`——那是顯示層的名字，不是契約。
⚠️ **T-197 之後那句話不再成立**：worker 專屬的那條路由連同其他六條一起收掉了，喚醒打的是
`POST /api/members/{member_id}/activate`（member handler 看 `kind == outsource` 分流進
`handleRestartOutsourceWorker`）。**語意仍然一個字沒變**，變的只有路徑。

### ④ 「喚醒時四格應該先預設跟原本一樣」＋「將外包統一跟正職一樣，不是釘死」

外包的喚醒**不再是按了就送**（舊行為：`POST …/restart` 直接設 `desired_state=online` 並立刻
respawn，什麼都不問）。現在它開的是**與更改同一份 dialog**，四格預設成 worker 現在的值，
**四格都可以改**。

**設定怎麼落地——全部走既有端點，沒有新增任何一條**：

| 步驟 | 端點 | 為什麼是這個順序 |
|------|------|------------------|
| 1 | `POST …/model`（`runtime` / `model` / `effort`） | 喚醒會重生 session，設定必須先落地，否則新 session 用舊模型起來 |
| 2 | `POST /api/members/{member_id}/activate`（機器有改時帶 `machine_id`） | 唯一會把它叫起來的那條，釘選也由它寫入——與正職同形，一次喚醒只送這一個請求 |

🔴 **喚醒不先打 relocate**：對一個已經停好的 worker，伺服器把 relocate 當成「停止後的重啟」並當場派出
START，接著的 activate 會再殺一次、再派一次——一次喚醒變成兩次開機加一刀。

對一個 **stopped**（`desired_state=offline`）的 worker，步驟 1 在這次請求裡不派工——
server 在 `desired_state=offline` 時只記下，所以步驟 2 是這次唯一會派工的一步。

🔴 **釘住的機器只是「睡著」時不可被偷改**這條規則跟著一起搬過來了
（`openSettings` 先 seed `worker.desiredMachineId`，即使那台目前離線也照帶；只有沒有固定機器時才預設選第一台線上機器）。
因此喚醒只在「使用者選了另一台」或「原本沒有固定機器、帶了預設的第一台線上機器」時才帶 `machine_id`；有固定機器且沒改，就不帶。
它防的缺陷是：**開設定只想改模型，結果人被默默重新釘到別台**。
「預設保留原本那台」與「使用者可以改」不衝突：預設是起點，不是鎖。

**沒有「只儲存，不喚醒」那顆鍵**——正職也沒有了（owner `rc-baa00a00dbc9`：「拿掉「只儲存，不喚醒」，不改介面」），兩邊一致。

---

## ② 的後續：已結案要由身分那一層講（owner 2026-07-31 追加裁定）

**怎麼被抓到的**：② 落地後我在回報裡點名一個隱憂 —— 拿掉狀態卡之後，released worker 在面板上
沒有任何「已結案」的字。把它攤給 owner，他的回覆是：

> **「為什麼從不同進入頁面會有不同的顯示方式？不是應該要一致嗎」**

**實際情況比原本的隱憂更糟（逐行讀原碼確認）**：released worker 被 server 從 LIVE 名單濾掉
（`api_outsource.go:126`），所以 `outsource.workers.find(...)` 一定 miss，
`#office/worker/<ow-id>` **不是顯示一顆灰點，而是默默掉回 roster**、什麼都不說。
同一個 worker 從聊天室進去卻明白寫著「已結案釋出」。**那就是 owner 說的不一致。**

### 判準

同一個事實，不論從哪個入口看到那個外包，**說同一句話、來自同一個來源**。
能看到 released worker 的入口只有兩個（它已從 LIVE 名單掉出去，別無他處）：聊天室、直接開的詳情。

### 做法

| 面向 | 決定 |
|------|------|
| 判 released 的來源 | `worker.status === "released"`（`WorkerStatusReleased` 那條線）。**不動 `presenceVisual`**：`presence` 對 released 與「從沒派工過」**都是 `undefined`**，五態 switch 分不出來，而拓寬它會波及正職 roster |
| 文案的家 | `office.outsource.releasedTitle` / `releasedSub` **一份**。原本叫 `releasedChatTitle`/`ChatSub`，**名字裡的 Chat 就是病灶**：它在邀請下一個人為面板再複製一份 |
| 措辭 | 改成**與入口無關**：原文「以下為歷史對話（唯讀）」對聊天室為真、**對面板是假話**。新文案「這裡是唯讀的歷史紀錄」兩邊逐字為真，所以**不需要組字、不需要第二片葉子** |
| 誰負責畫 | **只有 `WorkerDetailPanel`**。`OfficePage` 對那條路由合成一個只帶 `id` 的 released view 丟給它，而不是自己再畫一份 |
| 合成的誠實邊界 | 只填**我們真的知道的欄位**：id，以及 `GET /api/members/{id}` 讀回的代號（聊天室入口讀的是同一支，兩個入口的名字因此一致）。讀不到時 `codename` 留空、面板回退到誠實的 released 標籤 —— **不為一個已經查不到的 id 捏一個代號** |
| 已結案還顯示什麼 | 共用卡片全部不畫。released worker 沒有 session／機器／context%／live 花費／boot context，八張卡會是八個 dash ——**八個誠實的 dash 不比一句話更誠實，只是把那句話埋了**。生命週期按鍵也全拿掉：server 對 released worker 的 `/stop` `/restart` `/model` `/relocate` `/refocus` **一律 404**，留著就是 by construction 的 dead affordance |

護欄：`OfficePage.jump-outsource.test.tsx` 的
`the chat entry and the detail entry render the SAME released sentence`
—— **同一個 id、兩個入口各開一次，先斷言兩邊相等，再斷言兩邊都等於字典那片葉子**。
只比「兩邊相等」擋不住有人維護兩份同步的副本；只比「等於字典」擋不住其中一個入口根本不顯示。
兩條一起才是 owner 要的那件事。

### 順帶修掉的一個 mock↔http parity 缺口（讀碼發現，未改）

`mock.ts::listOutsourceWorkers` 的註解寫著「LIVE workers only」，但它**沒有濾掉
`status === "released"`** —— 它只是因為 mock 的釋出是「整列刪掉」才看起來對。真 server 是明確
`continue` 掉。目前沒有測試靠這個差異（我的 released 測試是直接 inject 一列 released 進去，
那正好是這個缺口讓它到得了面板），**但這是一個 mock 說得比做得多的地方**，列為 follow-up。
