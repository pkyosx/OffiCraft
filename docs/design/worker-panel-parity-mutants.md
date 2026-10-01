# T-7526 mutant 驗證紀錄

**為什麼這份要落檔**：獨立審查回來時，這包宣稱的 mutant 驗證在 repo 裡找不到任何紀錄，
審查者只好自己重跑一遍。**沒落檔等於不存在。** 下面每一列都可以被獨立重放。

## 方法

1. 把要驗的檔案複製一份到 scratchpad 當備份，`shasum -a 256` 記下雜湊。
2. 施加 mutant（單一、明確、可讀的一處改動 —— 描述欄寫的就是那一處）。
3. 跑該範圍的測試，記下**哪幾條紅**。
4. **從 scratchpad 備份 `cp` 回來**（🔴 **不准 `git checkout --`** —— 那會吃掉別人未提交的編輯），
   再 `shasum -a 256 -c` 驗還原後**逐位元組相同**。
5. Go 的每一支 mutant 前先 `go clean -testcache`，否則會讀到上一支的快取結果。

全部 29 支的還原檢查都是 `OK`（13 支第一輪 ＋ 9 支第二輪 ＋ 7 支第三輪）。

## 第一批：外包面板對齊（`WorkerDetailPanel.tsx`）

| # | Mutant（改了什麼） | 紅了哪條 |
|---|---|---|
| M1 | vm 裡把 `onSaveModelEffort` 加回去（就地模型編輯器復活） | `renders the 模型 and 機器 cells with NO in-place editor on either` |
| M2 | vm 裡把 `machineAction` 加回去（機器格就地控制項復活） | 同上 |
| M3 | 拿掉身分卡的「更改」入口（`{false && (…)}`） | 上述 + `更改 → changing the machine…` + `a rejected submit…` + `換 model: 更改 → save…`（共 5 條） |
| M4 | dialog 送出時不打 relocate（`if (false)`） | `更改 → changing the machine REACHES relocateWorker…` |
| M5 | 送出失敗時吞掉錯誤並關掉 dialog | `a rejected submit keeps the dialog open…` |
| M6 | relocate 排到 model op **之前**（順序反轉） | `saves the launch settings BEFORE relocating…` |
| M7 | 拿掉切換 worker 時的 dialog reset effect | `closes an open dialog when the panel switches to another worker` |

⚠️ **M6 / M7 第一輪是綠的** —— 代表當時**沒有任何斷言**釘住這兩個行為。補了測試才紅。
先寫 mutant、再看它殺不殺得死，是唯一能發現「斷言不存在」的辦法。

## 第二批：初始 PROMPT 卡（`AgentDetailPanel.tsx`）

先做**整體重現**：把**未修補的** `AgentDetailPanel.tsx` 還原回去跑新測試 —— **5 條全紅**
（正職 3 條、外包 2 條；MC-3 後外包補到 3 條）。再逐項：

| # | Mutant | 對應的修補要求 | 紅了哪條 |
|---|---|---|---|
| P1′ | deps 放回 `promptFetch` **並且**加回取消式 cleanup | ① 重繪不該取消進行中的讀取 | 正職＋外包的「重繪中仍顯示得出內容」兩條 |
| P2 | 已載入的章改回在 fetch **開始**時蓋 | ② 章要等讀到才蓋 | `re-expanding after a failed read…` |
| P3 | `.catch` 不落錯誤態（停在載入中） | ③ 失敗要顯示錯誤與重試 | 錯誤/重試相關 3 條 |

⚠️ **P1 的第一版只加了 cleanup、沒把 `promptFetch` 放回 deps —— 它是綠的。**
deps 沒變，cleanup 根本不會執行。**兩個成因是耦合的，mutant 必須兩個一起下。**

⚠️ 另一個坑（是這次真正差點放過去的）：`rerender` 若傳**同一個 element 物件**，React 會 bail out、
**根本不重繪**，於是「重繪中」那條測試**在未修補的碼上就是綠的**（恆真斷言）。
每次都要產生**新的** element。已寫進 `frontend/CLAUDE.md`。

## 第三批：取消喚醒（A）與復活死掉的 session（B）

**先做重現**（先紅才有資格談修好）：3 條重現測試在修補前全紅、3 條負向對照在修補前後都綠。

| # | Mutant | 紅了哪條 |
|---|---|---|
| A1 | deactivate 不派 STOP（`_ = cancellingWake`） | `TestHandleDeactivateMember_CancellingAWakeDispatchesAStop` |
| A1b | 每一次停止都立刻派（過度修正：`cancellingWake := true`） | `_OnlineMemberKeepsTheGracefulGrace` + `_OfflineMemberDispatchesNothing` |
| A2 | `report_waking` 又無條件清 `stopping_since` | `TestHandleReportWaking_KeepsTheStopTraceOfACancelledWake` |
| A2b | 永遠不清（過度修正） | `_ClearsTheStopTraceOnAnOrdinaryBoot` |
| B1 | restart 守衛退回 INTENT-only | `TestRestartWorker_RevivesAWorkerWhoseSessionDiedOnItsOwn` |
| B1b | restart 守衛整個拿掉（`if false`） | `TestRestartWorker_ClearsAndRedispatches`（活著的 worker 該 409） |
| B2 | 座艙 toggle 退回 `stopped`-only | 當時的 `a worker whose session died on its own offers 重新啟動…` |
| B2b | toggle 永遠顯示重啟（過度修正） | 當時的 `stop → the worker reads 已停止 and the toggle flips to 重啟` |
| B3 | mock adapter 漂回 INTENT-only 守衛 | 當時的 `a worker whose session died on its own offers 重新啟動…` |

⚠️ B2 / B2b / B3 點名的那兩條測試在**第二輪已改寫**（那顆 toggle 拆成 `worker-detail-stop`
與 `worker-detail-wake` 兩顆，字也從「重新啟動」統一成「喚醒」）。行為本身仍被
`喚醒 ASKS FIRST…` 與 `stop → the dot flips to 已停止…` 釘著；這幾列保留原文是**歷史紀錄**，
不要照字面去找那個測試名。

**B1 / B1b 是雙向的**：一支證明「死掉的要救得回來」，另一支證明「活著的仍要 409」——
守衛的兩半都承重，少任何一半都會被抓到。A1b / A2b / B2b 同理，是防過度修正的哨兵。

⚠️ **A1 的第一版是 `if false {`，Go 編譯不過**（`cancellingWake` 變成未使用）。
**編譯失敗不是「紅」** —— 它什麼都沒證明，得換成 `_ = cancellingWake` 重跑。

## 第四批:樣式所有權(截圖階段才發現的回歸)

**這一條不是測試抓到的,是看截圖看到的** —— dialog 整個沒有樣式。成因:`machine-picker.css`
的最後一個 production importer 隨著外包面板不再驅動 `useRelocateMachine` 而消失,**兩邊**的
設定 dialog 一起變裸,包含這次根本沒動到的正職面板。

| # | Mutant | 紅了哪條 |
|---|---|---|
| S1 | 拿掉正職面板自己的 `import "./machine-picker.css"`(＝原始回歸的形狀) | `component style ownership > every component using .machine-picker__* imports ./machine-picker.css`,而且斷言直接把 `MemberDetailPanel.tsx` 的檔名列出來 |

⚠️ **這類回歸對整套既有檢查是完全隱形的**:jsdom 不算 CSS、`tsc` 不知道 class 字串屬於哪份
stylesheet、而唯一會 render machine picker 的 CT guard 在同一張票裡退場了。護欄因此改成
**原始碼層級**的不變式,而不是靠瀏覽器量測。

## 第五批：owner 2026-07-31 四項裁定（第二輪）

方法同上（scratchpad `cp` 備份 ＋ `shasum -a 256 -c` 逐位元組驗還原，**不用 `git checkout --`**）。
9 支的還原檢查全部 `OK`。

### CSS（`member-detail.css`）— 只有 CT 看得見

| # | Mutant | 紅了哪條 |
|---|---|---|
| R1 | `.mp-identity__buttons { flex-direction: column }`（＝裁定前的形狀） | `identity-actions-row.ct.spec.tsx` **desktop 紅**（當時的斷言名 `one row of four`；同一條現名 **`one row, whole cluster`**，量的還是 `[ALL.length]`） |
| R2 | 刪掉 ≤720px 的 `.mp-identity__buttons` 規則 | **只有 narrow 紅**（當時的斷言名 `band 1 = 更改, band 2 = the whole ladder`；那條後來拆成樹裡現存的三條 **`band 1 = 更改 alone`**／**`the rest of the cluster keeps its order below 更改`**／**`at most two bands — 不要擠成一團`**） |

⚠️ **R2 第一版是綠的，而且綠得很有道理** —— 拿掉窄螢幕規則後，`.mp-identity__actions` 的
`align-items: stretch` 仍然把那一列撐滿，所以「同一列／沒溢出／在卡片內」**每一條都還是真的**；
真正壞掉的是 `justify-content: flex-end` 讓兩顆鍵**擠在右邊界**——正是 owner 說的「擠成一團」。
斷言因此改成量**跨距**（span > 卡寬 70%）與**均分**（寬度差 < 卡寬 20%）。
教訓與 T-d451 那條同族：**「東西還在」不等於「東西擺對」**。

#### T-ed79 重測：三顆鍵之後，這條護欄釘的事實換了一個

> ⚠️ **這一小節（含下一小節）描述的是 owner 2026-08-22 之前的世界：階梯是三顆並排的鍵。**
> 2026-08-22 之後階梯收成同一格，顆數不再是變數 —— 見本節最後的
> 〈三改：同一個按鈕 升級的概念〉。下面保留原文是**歷史紀錄**。

owner 2026-08-21 把單顆 停止 換成 停止 → 加速停止 → 強制停止 的階梯，於是
**「更改 ＋ 停止 同一列」在窄寬度下不再是這個元件的事實**：四顆四字標籤在 375 的卡片
（內寬 289）上排不下——光是階梯本身自然寬就要 ~300。護欄改成**依寬度分形狀**：
desktop 仍然一列四顆（2026-07-31 那條裁定在有空間的地方照舊成立），narrow 釘的是
**刻意的兩帶**（更改 一帶、整條階梯一帶、依 owner 順序、均分），外加**兩個寬度都成立**的
「任兩顆不重疊（矩形交集）／四個邊都不出卡片／標籤不被自己的鍵裁掉／頁面不長橫捲軸」。
**沒有任何一條斷言被放寬**——`toBeLessThan(4)` 那條的 same-row 容忍度原封不動，只是改成
用來分帶；重疊檢查從「停止 起點在 更改 右邊」升級成真正的矩形交集，卡片邊界從 2 條（左右）
升成 4 條，並新增了 clientWidth/scrollWidth 的裁字檢查。

版面同時修了（`flex: 1 1 0` → `flex: 1 1 100%` ＋ `align-items: stretch`）：舊規則把卡片
**對半分**給 更改 和整組階梯，階梯在自己那半裡疊成三層，`align-items: center` 再把這塊
三層的方塊對著單顆 更改 垂直置中——這就是 `s.y - c.y = 38` 的來源，也就是為什麼
owner 最想按的 停止 反而跑到 更改 **上面**。實測 320／360／375／390／430／600／720
七個寬度：全部兩帶、階梯一行、`scrollWidth ≤ width`、`pageOver = 0`。

| # | T-ed79 新增 mutant | 紅了哪條 |
|---|---|---|
| R3 | ≤720px 改 `flex-wrap: nowrap` ＋ `> * { flex: 0 0 auto }` | narrow：當時叫 `band 1 = 更改, band 2 = the whole ladder`，現存的對應斷言是 **`band 1 = 更改 alone`**／**`the rest of the cluster keeps its order below 更改`**／**`at most two bands — 不要擠成一團`** |
| R3' | ≤720px 的 `.member-actions` 釘死 `width: 500px; flex-wrap: nowrap`（＝真的溢出卡片，帶形還是對的） | narrow：`mp-change right edge` —— 這是**執行期組出來的訊息**，樣板是 spec 裡的 **`` `${id} right edge` ``**（grep 找字面會 0 行，要找樣板） |
| R4 | `.member-actions .btn + .btn { margin-left: -40px }`（＝真的互相疊上去） | narrow：`member-action-stop and member-action-accelerated-stop overlap by 32x30` —— 同樣是執行期訊息，樣板 **`` `${ALL[i]} and ${ALL[j]} overlap by ${overlapX}x${overlapY}` ``**。⚠️ 2026-08-22 之後**這個配對本身不可能再出現**（階梯只剩一格），現在能配對的是 更改／喚醒／那一格 |
| R5 | 階梯每顆鍵鎖成 `flex: 0 0 52px; overflow: hidden`（＝標籤被裁掉，boundingBox 看不出來） | narrow：`member-action-accelerated-stop label clipped vertically` —— 執行期訊息，樣板 **`` `${id} label clipped vertically` ``** |

#### 再重測：「按了才出現」把顆數也變成變數（**已退場的世界，見下一小節**）

owner 2026-08-21 第二條裁定（「不是一開始就顯示三個按鈕」「按了才出現」）之後，這一列的
**顆數本身**是狀態的函數：2（更改＋停止）／3（系統開的軟下線多一顆 加速停止）／
4（owner 按過 停止，`stopping` 另外帶一顆 喚醒 救援）／5（上了時鐘再多 強制停止）。
CASES 因此從兩種形狀改成**四種**，最寬的五顆那個 case 一字未動，另外補上最窄的兩顆——
**沒有一條斷言被放寬**（same-row 容忍度 4、矩形交集、四邊卡片邊界、clientWidth/scrollWidth
裁字檢查、跨距 70%／均分 20%、頁面橫捲軸全部原封不動）。desktop 那條 `one row of four`
只是改名成 `one row, whole cluster`，量的還是 `[ALL.length]`。

🔴 **上面這段的 2／3／4／5 顆在 2026-08-22 之後不再成立**：階梯收成同一格之後只剩
**2**（更改 ＋ 那一格）與 **3**（`stopping` 多一顆 喚醒 楔形救援）。四／五顆的最壞情況
是**在產品裡消失了**，不是被量測放過。
🔴 owner `rc-2e1c96250169`（停止中不提供喚醒）之後，那顆楔形救援也不在了：這一列恆為 **2** 顆。

新增的行為 mutant（`MemberActionButtons.tsx`）：

| # | Mutant | 紅了哪條 |
|---|---|---|
| M17 | 出現條件拿掉（當時的 `LADDER_BY_STAGE["accelerated"]`＝三顆一開始就都在；那個識別字已於 2026-08-22 換成 `RUNG_BY_STAGE`） | `MemberActionButtons > reveals one more rung per stage and renders NO unreachable rung`：`online, nothing winding down: rungs revealed: expected [ 'member-action-stop', …(2) ] to deeply equal [ 'member-action-stop' ]`；外包端同時紅 `停止 → the worker goes 停止中 and 加速停止 is REVEALED, 強制停止 still is not` 與 `強制停止 appears only after 加速停止…`（`expected <button…> to be null`） |
| M18 | 拿掉剛出現那顆的不可按窗口（`armed` 直接讀 `stage`） | 當時叫 `MemberActionButtons > holds a rung that JUST appeared inert until it arms` |

⚠️ **M17／M18 這兩列點名的測試名在現在的樹裡是 0 hit**（連同 M17 引的
`停止 → the worker goes 停止中 and 加速停止 is REVEALED, 強制停止 still is not` 與
`強制停止 appears only after 加速停止…`）—— 它們是三顆並排世界的測試名，2026-08-22 那次
改寫把它們換掉了。**不要照字面去找。** 現在承接同一件事的是下一小節列出的那五條。

R3'／R4／R5 是這次特地種來證明**新護欄仍有鑑別力**的三支：分別打「溢出卡片」「互相重疊」
「看起來有寬度其實字被吃掉」，三支都紅。還原一律 `cp` 備份覆蓋回去，最後 `git diff --stat`
確認只剩下真正要留的兩個檔。

#### 三改：owner 2026-08-22「同一個按鈕 升級的概念 不是不同按鈕」（T-ed79 本包）

reply card rc-2afe8b557e9c 選項 [D]：「停止 → 加速停止 → 強制停止 UI 顯示怪怪的，他應該
體感上像是同一個按鈕 升級的概念 不是不同按鈕」。於是階梯收成**同一格**——label／action／
testid 隨階段換，用過的那一階不再留在旁邊。實作上 `LADDER_BY_STAGE`（回傳一個陣列）換成
`RUNG_BY_STAGE`（`Record<StopLadderStage, ActionKey>`，回傳**單一** rung）。

CT 的 CASES 仍然是四個，只是變數從「幾顆鍵」換成「那一格是哪一階」——`強制停止` 是三個標籤
裡最長的，一格裝得下 `停止` **不等於**裝得下它，所以那是獨立的一次量測。
**沒有一條斷言被放寬**（same-row 容忍度 4、矩形交集、四邊卡片邊界、clientWidth/scrollWidth
裁字檢查、跨距 70%／均分 20%、頁面橫捲軸全部原封不動）；新增的是「階梯永遠只有一格」這條
計數斷言。

| # | Mutant | 紅了哪條 |
|---|---|---|
| R0 | 階梯改回三階並排（`RUNG_BY_STAGE` 退回回傳陣列） | `identity-actions-row.ct.spec.tsx`：**每一個寬度、兩個面板都紅**，在 `the ladder is ONE button, whatever rung it is on` |

🔴 **R0 這一列的來源和其他列不同，誠實標明**：它是 T-ed79 收尾的**文件修補**時，從 CT spec
檔頭（`frontend/visual-guards/identity-actions-row.ct.spec.tsx` 的
`Mutants (all MEASURED, see docs/design/worker-panel-parity-mutants.md)`）**轉錄**過來的，
補的是「spec 指著這份 md、這份 md 裡卻沒有 R0」這條斷鏈。**寫下這一列的人沒有重跑那支
mutant**；要獨立重放，請照本檔開頭的方法自己跑一次。

同一件事在單元層的護欄（下面五條**都在現在的樹裡**，grep 得到）：

* `MemberActionButtons > renders ONE ladder button and UPGRADES it — never a second rung beside it`
* `MemberActionButtons > holds the button inert for LADDER_ARM_MS after it upgrades`
* `MemberActionButtons > re-arms on EVERY upgrade, so 加速停止 → 強制停止 is guarded too`
* `WorkerDetailPanel > 停止 → the worker goes 停止中 and the ladder cell UPGRADES to 加速停止, not to a second button`
* `WorkerDetailPanel > 強制停止 is reached only by upgrading through 加速停止, ASKS FIRST, then kills and collapses the row to 喚醒`

🔴 **這一次拿掉的是「槽位分離」這道保護**，記在這裡免得下一個人以為它還在：三顆並排時
按過的那一階留在原地（作廢），新出現的那一階拿到**新的槽**，沒有任何一個槽會被回收成更狠的
動作。一格就是那個被回收的槽。owner 在卡上被逐字告知這一點，也被提了長按 [C] 與確認框 [B]，
他選了 [D]——不加任何新防護。因此 `LADDER_ARM_MS` 是唯一剩下的東西，不要弱化它，也不要在
這裡加一道 owner 明白拒絕過的保護。

### 行為（`WorkerDetailPanel.tsx` / `MemberDetailPanel.tsx`）

| # | Mutant | 對應裁定 | 紅了哪條 |
|---|---|---|---|
| M8 | 喚醒鍵改成直接 `onWake?.()`（＝裁定前的「按了就送」） | ④ | `喚醒 ASKS FIRST…` ＋ 另外 3 條（共 4） |
| M9 | `openSettings` 的機器 seed 改成優先第一台**線上**機器 | ④ | `…PRE-SEEDED…` ＋ `…pinned to a SLEEPING machine never silently re-pins it` |
| M10 | 喚醒排到 model／relocate **之前** | ④ | 現名 `喚醒 onto another machine stores the launch settings first, then sends ONE activate carrying the pin`（喚醒已不打 relocate；舊名 `喚醒 stores the launch settings and the pin BEFORE it wakes…`） |
| M11 | 無編輯的早退也套用到喚醒（照原值確認＝什麼都不做） | ④ | `喚醒 ASKS FIRST…` |
| M12 | 狀態格（含「已釋放」）加回去 | ② | `released…and NO 已釋放 status cell remains` |
| M13 | 離線原因跟著狀態格一起被刪 | ② | `離線: the dot reads 離線 and the structured reason survives…` |
| M14 | 喚醒的字改回 worker 私有葉子 | ③ | 現名 `停止 → the worker goes 停止中 and the ladder cell UPGRADES to 加速停止, not to a second button`（2026-08-22 收成同一格時最後一次改名；再之前叫 `停止 → the worker goes 停止中 and 加速停止 is REVEALED, 強制停止 still is not`，最早叫 `stop → the dot flips to 已停止 and the row swaps 更改／停止 for 喚醒`——外包的 停止 從當場砍改成優雅收工，那顆斷言的結論整個反過來了） |
| M15 | 外包的 `.mp-identity__buttons` 換掉（兩顆不再同一列） | ① | 同上那條 |
| M16 | 正職的 `.mp-identity__buttons` 換掉 | ① | `puts 更改 and the stop action in the same button row, 更改 first` |
| M16b | 正職那一列裡把「更改」拿掉（順序／存在性半邊） | ① | 上述 ＋ 既有的 `says 更改 for an online member…` |

🔴 **M9 第一版是綠的，而且是這一輪最危險的一支**：那兩條測試的 fixture **沒有任何線上機器**，
所以「優先第一台線上機器」這個 mutant **無處可去**，fallback 仍落回原本的釘選——
斷言在**有缺陷的碼上照樣通過**。補上 `__setMockMemberOnline("warden-mbp5", true)`（＝「有別的地方
可以被偷偷搬過去」）之後才紅。
**陽性對照必須讓被測的那個錯誤有機會發生**，否則它證明的是 fixture、不是碼。

🔴 **M16b 是 M16 的另一半**：M16 證明「兩顆在同一列」，M16b 證明「那一列裡真的有『更改』」。
少了後者，一個只渲染停止鍵的面板會讓 M16 的斷言變成恆真（`change` 根本取不到就不會比較）。

## 覆蓋面的證明（四條必須回 0 行的 grep）

「改了 A 要掃過所有引用 A 的 B」不能靠列清單自證 —— 清單漏一項就看不出來。以下四條在 repo 根目錄
執行，**每一條都必須輸出 0 行**。本檔與 `worker-panel-parity.md` 本身在講的就是這些退場的
東西，兩者一律排除。

```sh
EXCL=":!docs/design/worker-panel-parity.md :!docs/design/worker-panel-parity-mutants.md"

# G1 退場的識別字在 PRODUCTION 碼裡一個都不剩（解釋退場的註解不算，所以只比對「使用位置」
#    ——testid 字面、t.<key> 取值、prop 名——而不是裸字串出現）
git grep -nE '"worker-detail-status"|"worker-detail-stop-toggle"|\bworkerStatusText\(|t\.workerDetail\.(statusOf|status|starting|offline|working|stopped|restart|restarting)\b|\bonRestart\b' \
  -- 'frontend/src/**/*.ts' 'frontend/src/**/*.tsx' 'frontend/visual-guards' \
     ':!frontend/src/**/*.test.ts' ':!frontend/src/**/*.test.tsx'

# G2 兩份 locale 與 TWO 份 generated key 清單（TS + Go）同時清乾淨
#    —— 只清 zh/en 而漏掉 generated,主題包白名單會繼續宣稱那些代碼可覆寫
git grep -nE '^\s*(status|statusOf|starting|offline|working|stopped|restart|restarting):' \
  -- frontend/src/i18n/locales | sed -n '/workerDetail/p'
git grep -nE '"workerDetail\.(statusOf|status|starting|offline|working|stopped|restart|restarting)' \
  -- frontend/src/i18n/messageKeys.generated.ts server/ocserverd/message_keys_gen.go

# G3 樣式所有權（styleOwnership.test.ts 只掃 src/components,CT story 不在它的範圍內）
git grep -l 'mp-identity__buttons' -- 'frontend/src/components/*.tsx' 'frontend/visual-guards/stories/*.tsx' \
  | grep -v '\.test\.tsx$' | while read f; do grep -q 'member-detail.css' "$f" || echo "$f"; done

# G4 owner 面前的「重新啟動」這個 UI 字沒有殘留(server / session 的重啟是另一件事,不在此列)
git grep -nE '\*\*重新啟動\*\*|「重新啟動」|翻成\*\*重新啟動|restart: "重|restarting: "' \
  -- docs/guide frontend/src $EXCL
```

🔴 **寫這些 grep 時踩到的 shell 陷阱（會讓「回 0 行」變成假的）**：
`zsh` **不會**對未加引號的參數展開做斷詞。把排除清單放進一個字串變數再寫 `$EXCL`，
整串會被當成**一個** pathspec，**每一條排除都默默失效**。
排除清單必須用**陣列**（`EXCL=(':!a' ':!b')` ＋ `"${EXCL[@]}"`）。
本檔第一次寫時就是這個形狀，只是碰巧當時**不加排除也是 0 行**（＝比預期更嚴格），
結論沒受影響；但同一個寫法在別處會讓一條什麼都沒排除的 grep 看起來像「已排除、且乾淨」。

跑這四條時實際抓到、而列舉清單漏掉的三處：`frontend/CLAUDE.md` 拿 `workerDetail.statusOf`
當「查表葉子」的範例（那個 key 已經不存在）、`WorkerDetailPanel.test.tsx` 的 describe 分隔註解
仍寫「停止・重啟」、以及 `server/ocserverd/message_keys_gen.go`（**Go 那份 generated 清單**
——`npm run gen:msgkeys` 一次寫兩個檔，只看 TS 那份會漏）。

## 第六批：已結案要由身分那一層講（owner 2026-07-31 追加裁定）

判準是「同一個事實，不論從哪個入口，說同一句話、來自同一個來源」，所以 mutant 也分成兩族：
**「有沒有說」**與**「是不是同一個來源」**。後者才是這一批的重點 —— 前者用一份複製的字串
就能通過。

| # | Mutant | 打哪一半 | 紅了哪條 |
|---|---|---|---|
| N1 | 詳情入口退回默默掉到 roster（`false &&` 掉合成） | 有沒有說 | `the chat entry and the detail entry render the SAME released sentence` |
| N2 | 面板不再認得 released（`false && status === "released"`） | 有沒有說 | 上述 ＋ `released: the panel says 已結案 in the SAME words…`（2 條） |
| N3 | 🔴 **面板自己留一份幾乎一樣的字串**，不讀共用葉子 | **同一個來源** | 同上 2 條 |
| N4 | released view 整句拿掉（只剩灰身分） | 有沒有說 | 同上 2 條 |
| N5 | 合成的 view 改回 `status: "active"`（生命週期按鍵復活） | 誠實 | `the chat entry and the detail entry…` |
| N6 | 過度修正：`released \|\| noLiveSession`（凡是沒在跑的都當已結案） | 分不分得出來 | **11 條**，含專門的對照 `released vs merely OFFLINE are told apart…` |

🔴 **N3 是這一批唯一非做不可的一支。** N1／N2／N4 只證明「面板會說一句話」——
**把那句話寫死碼成第二份副本，這三支全部照樣綠**，而「兩份副本各自漂移」正是這次要修的病本身。
測試因此不是比對字面，而是**兩個入口互比 ＋ 兩邊都比字典**：第一條擋「其中一邊不顯示」，
第二條擋「兩份人工同步的副本」。少任何一條，N3 就殺不死。

🔴 **N6 是 N-族的過度修正哨兵**，也是「對照組不是裝飾」的證據：`presence` 對 released 與
對「從沒派工過」**都是 `undefined`**，光看那顆點永遠分不出來。沒有那條 offline 對照，
「面板會說已結案」會被一個**對每個灰 worker 都這樣說**的實作滿足。

### ⚠️ 一支**刻意不紅**的 mutant（誠實記錄，不是漏網）

| # | Mutant | 結果 |
|---|---|---|
| N7 | 把葉子的措辭改回聊天室專用的「以下為歷史對話（唯讀）」 | **綠** |

**為什麼綠是對的**：測試比對的是「兩個入口 vs 同一片葉子」，改葉子會同時改動兩邊，
所以它證明的是**單一來源**，不是**措辭**。**「這句話對兩個入口都為真」是 review-time 的性質，
沒有機械護欄** —— 寫死「不可包含『以下』」這種斷言跨語言會立刻變成噪音。
把它記在這裡，是為了讓下一個改這片葉子的人知道：**沒有測試會擋你，請自己確認新措辭在面板上
也不是假話。**

### 第六批的覆蓋面 grep（同樣必須回 0 行）

```sh
# ⚠️ 陣列,不是字串 —— 見上面的 zsh 陷阱
EXCL=(':!docs/design/worker-panel-parity.md' ':!docs/design/worker-panel-parity-mutants.md'
      ':!frontend/CLAUDE.md')   # 這三個檔的工作就是點名那個被退場的 key

# G5 聊天室專用的舊 key 名沒有任何存活的引用
git grep -nE 'releasedChatSub|releasedChatTitle' -- . "${EXCL[@]}"

# G6 那句話的「字串字面」只存在於兩份 locale,別處一律靠讀葉子
#    (刻意比對帶引號的位置 —— 註解裡引述那句話是合法的,複製成第二份字串不是)
git grep -nE '"[^"]*(已結案釋出|was released when its task closed)' \
  -- frontend/src ':!frontend/src/i18n/locales'

# G7 TS 與 Go 兩份 generated 清單都有新葉子、都沒有舊葉子
for k in releasedSub releasedTitle; do
  for f in frontend/src/i18n/messageKeys.generated.ts server/ocserverd/message_keys_gen.go; do
    grep -q "office.outsource.$k\"" $f || echo "MISSING $k in $f"
  done
done
grep -n 'releasedChat' frontend/src/i18n/messageKeys.generated.ts server/ocserverd/message_keys_gen.go
```

### ⚠️ 改 message key 名字的連帶後果（主題包）

`themeBundle.ts` 對**不認得的 key 是 DROP + 回報 `skipped`,不是拒絕匯入**
（owner 2026-07-27 裁定 rc-1599a0026a80,T-081b 退場 theme-identity keys 時立的規矩）。
所以把 `releasedChatSub` 改名成 `releasedSub` **不會讓既有主題包無法匯入**,
但**那個包對這句話的覆寫會默默失效**（匯入 UI 會在 `skipped` 裡說）。
第二輪退場的 `workerDetail.status` / `restart` / `statusOf.*` 等等同理。
T-081b 當時的證據堆裡就存過一份會受影響的範例主題包（它覆寫了
`office.outsource.releasedChatTitle`/`ChatSub`）——**任何覆寫舊 key 的既有主題包都是這個形狀：
包還是匯得進來,那一句的覆寫默默失效**。
