# 外包詳情面板 ↔ 正職成員詳情面板 逐項落差盤點（T-7526 步驟 1）

盤點基準 = `origin/main` @ `acac15a0ea56d28bac4f19f2fe4791459e79074e`，**逐行讀原碼核對**，不採信任何二手描述。

涉及三個檔：

- 共用面板 `frontend/src/components/AgentDetailPanel.tsx`（卡片骨架 + 統一 view model `AgentDetailVM`）
- 正職 wrapper `frontend/src/components/MemberDetailPanel.tsx`（T-927a 已改：面板唯讀、設定收進喚醒區）
- 外包 wrapper `frontend/src/components/WorkerDetailPanel.tsx`（未動）

「共用面板的鍵由 wrapper 傳不傳 callback 決定」屬實：
`AgentDetailVM.onSaveModelEffort` 未傳 ⇒ 模型格不長編輯鈕（`AgentDetailPanel.tsx` 的
`{vm.onSaveModelEffort && (<button …model-effort-edit>)}`）；`vm.machineAction` 未傳 ⇒
機器格標題右側無任何控制項。正職兩個都不傳，外包兩個都傳。

狀態欄位說明：**同**＝行為與外觀已一致｜**差**＝需要對齊｜**外包獨有**｜**正職獨有**｜
**待裁定**＝說不出明確期望，交回 owner。

---

## A. 身分卡（identity slot）

| # | 項目 | 正職有什麼 | 外包有什麼 | 差在哪 | 期望行為 |
|---|------|-----------|-----------|--------|---------|
| A1 | 返回鍵 | `mp__back`（共用面板畫） | 同 | 同 | 維持共用，不動 |
| A2 | 頭像上傳／移除 | `AvatarEditor`，未傳 handler 時降級成唯讀 `Avatar` | 同（kind=`outsource`） | 同 | 維持 |
| A3 | 名字 | `InlineEdit` 就地改名 → `onRename` | 無；顯示系統建立的代號 `msg.outsourceLabel(codename)` | 差（結構性） | **保留現狀**。外包代號是系統建立的匿名識別，不是人取的名字；給外包改名等於發明一個後端沒有的欄位 |
| A4 | 成員編號 chip | `member.memberId` badge | 無 | 差（結構性） | **保留現狀**，理由同 A3（代號本身就是識別） |
| A5 | presence 指示 | `PresenceBadge`（點＋角色名） | `LifecycleDot` + `presenceVisual`（同一份映射） | 視覺元件不同、映射同源 | **保留現狀**：`frontend/CLAUDE.md` 明文「presence→視覺的推導只有一份」，兩者都走 `presenceVisual`，未漂移；外包沒有角色名可顯示，套 `PresenceBadge` 會多出一個空欄 |
| A6 | 任務 chip（`T-xxxx`）+ 任務類型 | 無 | 有，可點 → `#tasks/<id>` | 外包獨有 | **保留**。外包的「角色」就是它綁的任務類型，這是 rail 列形的同一條裁定（`frontend/CLAUDE.md` 外包面板節），移除等於拔掉外包唯一的身分線索 |
| A7 | 動作鍵列（喚醒／取消／停止／強制停止） | `MemberActionButtons`，依 `visual` 五態切換按鈕集合 | ~~無此列~~ **已補（owner 2026-07-31）**：身分卡右上角有 `worker-detail-change` ＋ `worker-detail-stop`／`worker-detail-wake` | ~~差~~ **已對齊** | ✅ 見下方「owner 2026-07-31 四項裁定」 |
| A8 | 「更改」鍵 | `mp-change`，`online` 時出現，開啟動設定 dialog | **無** | 差 🔴 | **外包要有等價入口**：開一份同形狀的設定 dialog（執行環境／模型／投入度／機器）。這是步驟 2 的主改動 |
| A9 | 未派送警示 | `DispatchAlert`（`mp-wake-undispatched` / `mp-relocate-undispatched`） | **無** | 差 | **待裁定**：外包的 `relocateWorker` wire 回傳 `OutsourceWorkerView`，**沒有** member 那個 `relocation_pending` 欄位，所以外包端根本沒有訊號可顯示。要對齊得改 `spec/openapi.json`（wire 已凍結，§13）。本票不動 |

## B. 模型／機器 資訊卡（共用面板 `mp-info2`）

| # | 項目 | 正職有什麼 | 外包有什麼 | 差在哪 | 期望行為 |
|---|------|-----------|-----------|--------|---------|
| B1 | AI 執行環境 / 模型 / 投入度 顯示 | 唯讀。`model` 餵 `awake ? member.actualModel : ""`，並掛 `modelIsReported: true` ⇒ 值旁標「最近一次開機回報」 | 唯讀顯示 + **一顆鉛筆「編輯」鍵**（`worker-detail-model-effort-edit`），就地展開 `ModelEffortEditor` | 差 🔴 | **拿掉就地編輯**：wrapper 不再傳 `onSaveModelEffort`；改設定一律走 A8 的 dialog |
| B2 | 模型值的語意 | REPORTED（agent 開機回報的實際值） | CONFIGURED（`worker.model`，owner 意圖值） | 差 | **待裁定**：外包 DTO 沒有 `actual_model` 對應欄，無法呈現「回報值」。硬掛 `modelIsReported` 會是假話。是否要在 wire 加欄，交 owner |
| B3 | 機器格 | 唯讀。`machineText = awake ? machineName : ""`（未喚醒一律 dash，T-2860 presence 契約） | 唯讀值 `worker.machine \|\| 尚未分配`，**加一顆「編輯」鍵**（`worker-detail-relocate`，`useRelocateMachine`） | 差 🔴 | **拿掉就地「編輯」鍵**：機器改為在 A8 dialog 內選。顯示文字維持 `尚未分配`（外包的 `machine` 是「最後一次派工目標」，語意與 member 的 observed 不同，落 dash 反而更不誠實） |
| B4 | 遷移中提示 | `machineTransition`（`→ 要換到 ○○`），`awake && machine !== desiredMachineId` 時顯示 | **無**（wrapper 沒傳 `machineTransition`） | 差 | **補上**：外包同時有 `machine`（最後派工目標）與 `desiredMachineId`（owner 釘選），兩者不同就是移動中，資料齊備。⚠️ 標**待裁定**：這是新增一個畫面元素，且外包的 `machine` 語意是派工目標而非觀測位置，提示文案「現在在 ○○」可能過度宣稱。要不要做、文案怎麼寫，交 owner |
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
| D1 | 喚醒（`spawn`／`t.lifecycle.action.spawn`） | 有：離線／已停止／waking／stopping 皆提供，開設定 dialog 後 `activateMember` | `restartWorker` | ~~差~~ **已修（T-7526 追加範圍，owner 核可）** | ✅ **已完成，不再是開放問題**。原本 `restart` 的守衛是 `desired_state != offline → 409`（問「有沒有人按過停止」），所以 session 自己死掉的外包（`desired_state` 仍是 online）叫不起來。守衛改成 `desired_state != offline && hub.IsOnline(id)`（問「還活著嗎」），控制台的 `noLiveSession = stopped \|\| offline` 讓那顆鍵在死掉的 worker 上顯示「重新啟動」。護欄：`TestRestartWorker_RevivesAWorkerWhoseSessionDiedOnItsOwn` |
| D2 | 取消喚醒（`cancel`） | `waking` 時提供 → `deactivateMember` | 無 | 差 | **待裁定**，同 D1：外包的 `stop` 端點是否吃 `waking` 態，wire 沒明說 |
| D3 | 強制停止 + 二次確認 | `stopping` 時 Stop 升級為 force-stop，`mp-force-stop-confirm` modal | **無此端點**（`spec/openapi.json` 只有 `/api/members/{id}/force-stop`） | 差 | **待裁定**。要對齊得新增 `/api/outsource-workers/{id}/force-stop`，屬 wire 變更（§13 先改 spec + owner 過目） |
| D4 | 「只儲存，不喚醒」 | `mp-settings-save-only`，未喚醒時出現 | 無 | 差 | **不需要**（可自裁定）：外包 dialog 本來就**不啟動任何東西**（只打 `model` 與 `relocate` 兩個端點），所以整份 dialog 就是「只儲存」，多一顆同義鍵反而製造「另一顆會啟動」的錯覺 |
| D5 | 設定意圖註記 | `mp-settings-intent-note`（＋回報值對照的第二句） | 無 | 差 | 外包 dialog 改用 `t.workerDetail.modelNextSpawnNote`（「工作中立即生效；已指派則下次啟動生效」）——那才是外包端點的真語意；照抄正職的「下次啟動要用哪一個」會是假話 |
| D6 | 回呼端點 · WEBHOOK 卡 | `extraExpandCards` 整張卡（列表／啟停／建立／刪除／簽章輪替／事件統計） | 無 | 差 | **保留現狀**。webhook 綁的是常駐 member id；外包是任務結束即 release 的短命身分，掛外部長期入口沒有可對應的生命週期 |
| D7 | 改名 | 有 | 無 | 見 A3 | 同 A3 |

## E. 共用卡片（已一致，列出以示涵蓋完整）

| # | 項目 | 狀態 |
|---|------|------|
| E1 | 運行狀況 · context% | 同（`vm.contextPct`；codex 另帶壓縮次數） |
| E2 | 重新聚焦鍵（`*-refocus`） | 同（兩邊都傳 `onRefocus`，皆 online-only，皆有送出註記與上次時間） |
| E3 | 估計 $（live + banked 合計） | 同（兩邊 wrapper 都用同一口徑折算） |
| E4 | 最近操作回執卡 | 同（含失敗原因、記錄展開） |
| E5 | 終端 · TMUX 複製鍵 | 同（session 名分別為 `member.tmuxSession` / `member-<workerId>`） |
| E6 | 初始 PROMPT 展開卡 | 同（正職按 role 抓 `/api/bootstrap`；外包按 id 抓 boot-context，另帶誠實 caveat 註記） |

---

## 「待裁定」清單（交回 owner）

> **狀態（最後更新：owner 2026-07-31 四項裁定完成後）**：原本 8 格，現在剩 **5 格**開放。
> 已關掉的三格：**D1**（owner 核可並已實作完成，見上表 D1 列）、**B5**（owner 明示核可，見下方裁定段）、
> **C1**（owner 2026-07-31 裁定，見下方「owner 2026-07-31 四項裁定」）。
> 此表與上面的逐項表、與下方連帶後果段**必須同批更新**——文件把已完成的事仍標成待裁定，
> 下一個人就會拿它去問一個已經有答案的問題。

| 代號 | 一句話 |
|------|--------|
| A9 | 外包 relocate 的 wire 回傳沒有 `relocation_pending`，無法對齊正職的「已釘選但沒派出去」警示。要對齊＝改凍結 wire |
| B2 | 外包 DTO 無 `actual_model`，模型格無法像正職那樣標「最近一次開機回報」。要不要加欄？ |
| B4 | 要不要補「→ 要換到 ○○」遷移提示？外包的 `machine` 是派工目標而非觀測位置，文案有過度宣稱風險 |
| D2 | `waking` 的外包無「取消喚醒」。`stop` 端點是否吃 waking 態，wire 未明說 |
| D3 | 外包無「強制停止」端點。要不要新增 `/api/outsource-workers/{id}/force-stop`？ |

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

**沒有任何一格因為我判斷「該移除」而被移除。** B1 / B3 兩項就地編輯鍵的移除，是派工單步驟 2
DoD 第 1 條明文指定的改動，且**能力本身沒有消失**（改模型、改機器都改由 A8 的 dialog 承接）。

---

## 步驟 2 實作範圍（依上表推導）

1. `WorkerDetailPanel` 不再傳 `onSaveModelEffort`（B1）與 `machineAction`（B3）給共用面板。
2. 身分卡動作列新增「更改」鍵（A8），開一份與正職同形狀的設定 dialog：
   `ModelEffortEditor`（執行環境／模型／投入度）＋ 機器 `<select>`（線上機器 ＋ 自己那台離線釘選，
   照 `MachinePicker` 的規則標「離線」且 disabled），底部 取消／更改。
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
現在窄螢幕下 `.mp-identity__buttons > * { flex: 1 1 0 }` 讓兩顆均分整張卡的寬度。
護欄：`visual-guards/identity-actions-row.ct.spec.tsx`（desktop ＋ narrow 兩個 viewport）。
**兩個 viewport 各自承重**，見 mutants 檔的 R1 / R2。

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

🔴 **REST 路徑一個字都沒動**：仍是 `POST /api/outsource-workers/{id}/restart`
（凍結 wire，§13），`api.restartWorker` 也維持原名。只有 panel 的 prop 從
`onRestart` 改成 `onWake`——那是顯示層的名字，不是契約。

### ④ 「喚醒時四格應該先預設跟原本一樣」＋「將外包統一跟正職一樣，不是釘死」

外包的喚醒**不再是按了就送**（舊行為：`POST …/restart` 直接設 `desired_state=online` 並立刻
respawn，什麼都不問）。現在它開的是**與更改同一份 dialog**，四格預設成 worker 現在的值，
**四格都可以改**。

**設定怎麼落地——全部走既有端點，沒有新增任何一條**：

| 步驟 | 端點 | 為什麼是這個順序 |
|------|------|------------------|
| 1 | `POST …/model`（`runtime` / `model` / `effort`） | relocate 與 restart 都會重生 session，設定必須先落地，否則新 session 用舊模型起來 |
| 2 | `POST …/relocate`（只在機器有改時） | **`/restart` 不吃 machine_id**，所以釘選只能由 relocate 寫。這正是外包與正職的形狀差異：正職的 `activate(machineId)` 自己帶機器 |
| 3 | `POST …/restart` | 唯一會把它叫起來的那條 |

對一個 **stopped**（`desired_state=offline`）的 worker，步驟 1、2 是**純存下來**——
server 的 `respawnWorkerForOwnerOp` 在 `desired_state=offline` 時只記錄、不啟動任何東西
（`spawnReasonHeldDown` 回執），所以步驟 3 是唯一一次派工。

🔴 **釘住的機器只是「睡著」時不可被偷改**這條規則跟著一起搬過來了
（`openSettings` 逐字 seed `worker.desiredMachineId`，不 fallback 第一台線上機器）。
它防的缺陷是：**開設定只想改模型，結果人被默默重新釘到別台**。
「預設保留原本那台」與「使用者可以改」不衝突：預設是起點，不是鎖。

**沒有「只儲存，不喚醒」那顆鍵**，而且這一條**刻意不與正職對齊**：正職的「只儲存」是
PATCH ＋ placement-only relocate，兩者都不啟動任何東西；外包的 relocate **不是** placement-only
（`desired_state` 不是 offline 時它會 kill + re-dispatch），所以對「session 自己死掉、
`desired_state` 仍是 online」的 worker，一顆說「存了但沒啟動」的鍵會是假話。
要提供它得新增一條 pin-only 的外包端點＝動凍結 wire（§13）——**未做，列為 follow-up**。

### ④ 的已知代價（誠實記錄，不是待裁定）

對一個 **offline**（session 自己死掉、`desired_state` 仍是 online）的 worker，**且**owner 在
dialog 裡改了機器：步驟 2 的 relocate 本身就會 kill + re-dispatch，步驟 3 的 restart 再做一次
——**多一次 kill＋spawn 的 churn**。終態是對的（跑在選的那台、用新設定），
FE 也沒有可靠訊號能分辨「relocate 已經派出去了」，因為 relocate 的回應與 held-down 的回應
形狀相同。要消掉它得讓 relocate 回報「有沒有真的派」＝改凍結 wire。
**不改機器時（最常見）沒有這個 churn**，因為 relocate 根本不會被呼叫。

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
| 合成的誠實邊界 | 只填**我們真的知道的那一個欄位（id）**。`codename` 留空、面板回退到誠實的 released 標籤 —— **不為一個已經查不到的 id 捏一個代號** |
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
