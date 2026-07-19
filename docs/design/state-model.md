# 狀態模型 — member / warden 的 online 與生命週期

> **owner(Seth)2026-07-10 定案。** 本文件是 member / warden「在不在線」、狀態該存哪、與生命週期管理的**單一權威 logical spec**;相關 code 一律照此。
> 取代舊的「host-mismatch 自動 relocate」機制(見文末「取代了什麼」;舊設計文件 t21-server-reconcile-design.md 已隨 Python 實作退場——歷史見 git tag `py-final`)。
> 現行實作 = Go server(`server/ocserverd/`):「warden 也投影 online」「砍自動 relocate」已落地(hub.go / reconcile.go);**原則 3 的「handshake 機器比對→suicide」未實作**(owner 2026-07-12 定案 code is right:唯一性由 dual-SSE 409+stop gate 承擔、換機維持手動,原則 3 降級為 backlog 設計選項,見 §3)。code 與本文不合時 flag owner 定奪。(較小版:member 起停決策**留在 server**,warden 是純執行手 / remote calling tool;非「決策搬 warden」。)

## WHY — 要解決的根本問題

「一個 member(或 warden)到底在不在線 / 實際在哪台機器」這件事,過去散在多個地方各自記、且彼此對不起來:① DB 的 `online` 欄位 ② SSE 連線在不在 ③ 有沒有 session / pid ④ warden 有沒有回報 ⑤ DB 的 `current_machine_id`(agent 自報實際 host)。這些本該一致,一旦不同步就出現「鬼打牆」狀態——例:連線在但沒 session 的 **phantom**、warden 進程活著卻顯示 offline、member 卡 `stopping` 無限重試、`current_machine_id` 跟真正連上來的機器對不上。

根本病有三個,互相纏繞:
1. **狀態來源不單一**:online / 位置的真相沒有唯一權威,靠多個訊號拼湊 → 訊號一不同步就自相矛盾。
2. **member 的 online 被跟 warden 的生命週期綁死**:分不清「member 掛了」「warden 沒去 spawn 它」還是「warden 自己掛了」。
3. **把「當下實況」存進 DB,製造了會過期的副本**:`online`、`current_machine_id` 這類「此刻的觀測值」一旦落 DB,就多了一份會跟即時真相不同步的快取(最痛案例:prod warden 的 `member.online=0` 明明 SSE 連著,卻騙過診斷、被當成「warden 沒在線」)。

以下的大原則直接治病根 3、三條具體原則治病根 1 / 2 / 並補上唯一性。

## 大原則:intent(存 DB) vs observed(不存 DB,活在記憶體)

一刀切開所有狀態:

- **intent(意圖 / 身分)= durable → 存 DB。** 人或系統「設定的意圖」與穩定身分:重啟後必須記得。例:`desired_state`(online/offline)、`desired_machine_id`(希望它在哪台)、`role_key`、`name`、`kind`、`model`、`effort`、`id`、`owner_id`、`banked_cost`(歷史累積)。(`core` 曾列於此;owner 2026-07-11 裁決該意圖已退役——全鏈路零讀者,seed 成員的保護實際 key 在 `role_key` 的 seed-role 判斷——migration 0028 已將欄位移除。)
- **observed(當下實況投影)= volatile → 只活在記憶體、隨 SSE 生滅,絕不落 DB。** 「此刻的觀測值」,由即時來源重建、不需持久。例:**在不在線**(`online`)、**實際在哪台機器**(以連線 token 的 machine claim 為準)、CPU / RAM 等 **telemetry**。
- **為什麼 observed 不落 DB**:存一份到 DB 的唯一效果,是多一個「會跟即時真相不同步的副本」,除了製造 bug(讀到過期值當真相)沒有任何好處。observed 的真相**就是**即時來源本身(SSE 連線投影 / telemetry push),不需要、也不該再存一份。
- **判準**:一條狀態「重啟後要不要記得」?要 → intent → DB;不要(重連就自然重建)→ observed → 記憶體。

> 這條大原則統攝下面所有具體原則:原則 1(online 是 observed)、唯一性(位置是 observed)、telemetry(是 observed)全都是它的實例。

## 具體原則

### 1. online = 自證的 SSE 連線投影(單一來源,member 與 warden 同一套)

- 任何實體(agent member 或 warden)的 online,**只**由「它自己是否正持著一條到 server 的 SSE 連線(`GET /api/events`)」決定:連著 = online、斷 = offline。**二態、無 heartbeat、無 TTL、綁連線生命週期。**
- 這是 online 的**唯一權威來源**。要知道「X 在不在線」一律問 SSE 連線投影(`SSEHub.is_online()`)——**不從 DB flag、不從別人的回報、不從 session / pid 推斷**。`member.online` DB 欄位是 observed 落 DB 的反例,**整個 drop 掉**(不留相容快取——留著就還是不同步來源)。
- **「實際在哪台機器」同理是 observed**:agent 的 SSE 連線 token 本身帶 machine claim(token = who + **where**),要知道「它此刻在哪台」讀 hub 的 listener 即可。DB 的 `current_machine_id`(agent 自報寫入)是同一份資訊的多餘副本,**移出 DB、改從連線投影**。留 DB 的位置只有 `desired_machine_id`(desired_state,intent)。
- **warden 與 agent member 一視同仁**:warden 自己連上 SSE 就 online、斷就 offline。**明確廢除**舊設計「warden 是 infrastructure、不投影 online」——warden 也是「自證 online」的一等公民。
- `waking` 等衍生態仍由 `presence_state()` 從 online + `waking_since` / `stopping_since` 錨點衍生(此機制不變)。

### 2. warden 與 member 的 online 互不推斷(liveness decouple)

- **online 各自自證**:member 的 online 由 member 自證(它自己連 SSE),**不由 warden 推斷、不由 server 代算**;warden 的 online 同理由 warden 自證。兩者的「在不在線」互不推斷、各自獨立。
- **warden 的執行職責**:warden 在自己這台機器上把 member spawn 起來 / stop 掉(執行手,收 server 命令)。
- **member 起停的決策留在 server(較小版)**:server 保留「依 owner 意圖(`desired_state`)決定該起 / 停哪個 member」的單純決策迴圈(`desired_state==online ∧ ¬online → START`、`desired_state==offline ∧ online → STOP`)。**但砍掉自動 relocate**(`observed_host ≠ desired_host` 觸發的自動搬移)那套複雜機制——它是 phantom / robust-stop 殺不掉 / 目標不可達 三重死鎖的來源。member 換機器改由原則 3 的唯一性機制自然達成,不再自動 relocate。
- liveness decouple 的好處正好解掉診斷困惑:member 判為離線時,一眼看得出是誰的問題——
  - member 自己沒連 SSE → **member 掛了**(交給它機器上的 warden 重拉)
  - warden 沒連 SSE → **warden 掛了**(與 member 在不在線無關,各自獨立)

> **徹底版(之後可走,本次不做)**:連 `desired_state→START/STOP` 決策也搬到 warden(warden 自主 reconcile 生死)、server 完全不主動決策。較小版是它的子集,不擋這條路。

### 3. 唯一性:現況(dual-SSE 409 + stop gate)與未實作的 by-construction 設計

**現況(code is right,owner 2026-07-12 定案)**:一個 member 只該有一個 live 實例。這由兩道**已實作**的 handshake gate 承擔(`api_infra.go` pre-stream):

- **dual-SSE 409**(`hub.Connect`,spec/sse.md §5.1):同一 member 已有 live listener → 第二條連線 409。同時只有一條 SSE = 同時只有一個投影中的實例。
- **殭屍 stop gate**(765deb9,`sseStopGateRefusal`):roster 非 active、或 desired_state=offline 且有 stop 錨 → 409 拒連。停用/回收中的 member 連不回來。

**連線的 machine claim 不參與 handshake 判斷**(只作 online 位置投影與下架擋門用)。推論:**member 換機器(desired_machine_id A→B)沒有自動路徑**——A 上舊實例不會退、還佔著唯一 SSE 槽,server 也不對已 online 的 member 發 START;換機是**手動三步:先 desired_state=offline(舊實例退)→ 改 desired_machine_id → 再 online(B 上拉起)**。

**未實作的 by-construction 設計(backlog 選項,之後真需要自動換機再照此補)**:SSE handshake 當場比對機器,不另開偵測迴圈:

- agent 連 `GET /api/events` 時,連線 token 帶 machine claim(它跑在哪台)。server 比對:**這條連線的 machine claim ≠ 該 member 的 desired machine?**
- **不符 → 順著這條剛連上的 SSE 回一個 wind-down / suicide,且不把它投影成 online。** agent 收到後自我了結退出。
  - **為什麼是「回 suicide」而不是別的**:
    - 只「拒絕連線 / SSE 開不起來」→ 它上不了線 ✓,但進程還活著狂 retry = 殭屍,沒回收。**不夠**。
    - 「繞去請那台 warden STOP 它」→ 治本,但要 server「查那台 warden 是誰 → 發 STOP」,那條 routing 正是要砍的 relocate 複雜度。**繞遠**。
    - 「順這條 SSE 回 suicide」→ 唯一性在最上游 by construction、零額外 reconcile 迴圈、進程也被清掉(不留殭屍)。**最乾淨**。
  - 它的 warden **不會**把它重 spawn——server 從不對非 desired machine 發 START。
- **relocate 因此自然達成**(若補實作):owner 把 `desired_machine_id` 從 A 改到 B → A 上的舊實例下次 SSE 動作被判為「非 desired machine」→ 收 suicide 退出 + 不投影 online;server 對 B 發 START → 新實例在 B 起來。**不需要 DB 記 `current_machine_id`、比對、算 relocate**——唯一性與搬移都是 handshake 比對的副產物。
- **復用既有 wire、成本低**:agent 端本來就有「收自己 `/api/events` 的 `member` topic delta → graceful self-stop」的機制(`should_wind_down`,現用於 desired_state=offline 路徑)。補實作只需 **server 端加一段 handshake 判斷 + 復用這條 fan 機制**;**agent 端零改動、warden(Go)完全不碰**。

### 4. 下架 warden 的依賴提醒

- 下架一個 warden 時:若還有 member 的 `desired_machine_id`(desired_state,intent)指向它,這些 member 下架後將**無法在該機器 respawn**。
- 此時做**最低限度的提醒**(列出受影響的 member),由**人**決定把它們的 desired_state 改去哪 / 何時改。**不自動 relocate。**(依賴檢查只看 `desired_machine_id`;`current_machine_id` 已移出 DB,不再是判斷依據。)

## 取代了什麼(owner 2026-07-10 定案:較小版)

**舊模型**:server 端 reconcile producer 每 30s tick 決定該起 / 停哪個 member(單純的 `desired_state → START/STOP` 決策),**並且**在 `observed_host ≠ desired_host` 時自動觸發 relocate(STOP 舊 host → START 新 host),為此在 DB 記 `current_machine_id` 並與 `desired_machine_id` 比對。

本次**砍掉的是後者——自動 relocate 那套複雜機制 + 它賴以運作的 `current_machine_id` DB 副本**:它是三重死鎖的來源(phantom = START 記了但無 session、robust-stop 殺不掉 phantom、relocate 目標不可達),也是把 member online 跟 warden lifecycle 綁死、狀態來源不單一的元兇。**保留**前者——server 單純的 `desired_state → START/STOP` 決策迴圈(warden 仍是執行手)。member 換機器**目前是手動三步**(先下線→改機器→上線,見 §3);若日後要自動化,照原則 3 的 backlog 設計補 handshake 比對,不重建專門的 relocate routing。

**code 對應(Go 實作,`server/ocserverd/`)**:
- **online / 位置統一自證**:`hub.go`(SSE 連線註冊 = online 投影,member 與 warden 同一套);member 無 `online` DB 欄,讀寫全走 hub 的連線投影;實際位置從連線 listener 的 machine claim 投影,不落 DB。
- **唯一性(現況)**:SSE handshake pre-stream 兩道 gate——dual-SSE 409(`hub.go` `Connect`:member 已有 live listener 即拒)+ 殭屍 stop gate(`api_infra.go` `sseStopGateRefusal`);**machine claim 不參與 handshake 判斷**(原則 3 的 by-construction 比對未實作,backlog)。
- **無自動 relocate**:`reconcile.go` 只有單純 `desired_state→START/STOP` 決策 + producer 決策迴圈,沒有 host-mismatch relocate 決策臂。
- **下架擋門(owner 定案 2026-07-11:實際在線判準)**:machine uninstall / delete 的 409 擋門只計「**現在真的在線**(hub online)且 live SSE machine claim 指此 warden」的 agent(`hub.AgentsOnMachine`);離線但 `desired_machine_id` 綁定在此的 agent **不擋**——這台上全部 agent 離線 = 可直接移除 / 解除(`api_machines.go`;FE 警示清單同判準)。
- **uninstall 意圖一次性(owner 定案 2026-07-11)**:`desired_state="uninstall"` 是一次性意圖,絕不永久掛著——server 觀察到該 warden **真的斷線** 即消化歸零(`desired_state→offline`,record 保留可重裝;SSE disconnect edge 事件驅動 + reconcile tick pass 當 restart-amnesia backstop);任何重裝路徑(boot-command 重取 / bootstrap-here)先把殘留意圖歸零再裝。否則殘留意圖 = 常駐殺令,warden 重連即再被解除(2026-07 實際事故:無限 uninstall→重裝迴圈)。
- **telemetry 已符合**:warden / agent 的 CPU / RAM / rate-limit telemetry 純記憶體(no DB),本就是 observed 該有的樣子。
- **auth 模型不變**:server mint token → push;warden / agent 零自決 auth,不受本次影響。
