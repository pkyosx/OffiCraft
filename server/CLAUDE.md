# server/ — Go server daemon (`ocserverd`)

進入 `server/` 時讀本檔；repo-wide 身分、權限、文件、CI、PR 與 release 規則在根目錄 `CLAUDE.md`。本檔只保留 server 端會讓實作者猜錯的現行不變量。行為權威依序看 `server/ocserverd/`、`spec/`、`conformance/`、可執行 guard 與測試；本檔不是 route、job 或檢查項目清單。

## 1. 身分、建置與啟動

- `server/ocserverd/` 的 folder、Go module、production binary 都叫 `ocserverd`。binary fresh build，不能 commit；根目錄規則所列的 TCC 身分錨點是唯一例外。
- `ocserverd` 是單一 daemon。seeds、prebuilt binaries、MCP catalog 走 `go:embed`，不從 CWD 的 `seeds/`、`spec/mcp-catalog.json` 或 `bin/ocwarden` 偷讀舊檔；`seedsdist/`、`bindist/` 是建置 staging，不是 source of truth。
- 乾淨 worktree 手跑 server Go tests 前，先執行 `bash bin/build-seedsdist && bash bin/build-docsdist`；`bin/ci.sh` 會自動 staging。`build-bindist` 是單檔 boot/install 的前置，不是 `go test` 的前置。
- 有效 config 只有 `[server].port`、`[server].namespace`、`[storage].dsn`；部署用 `$OC_CONFIG`／`$OC_DATABASE_URL` 明確指定。預設 `oc.toml` 以 CWD 相對位置找，host 固定 loopback；不要把退役 key 或環境探測重新當成設定來源。
- `bin/release`、release preflight 與「CI/main 不等於部署」遵守根檔；server 文件不另抄一份命令或 release 清單。
- 🔴 **新增 migration 的號碼必須大於 `origin/main` 目前宣告的最大號**，兩個來源都算（`migrations/*.sql` 與 `goose.AddNamedMigration*` 註冊的 Go migration）。跳過的號碼永遠留著不補：低於已釋出最大號的新號會讓**已經在跑的站**下次開機在 `runMigrations` 停住、exit 1、不會 listen，而全新安裝完全看不出來（空白 DB 上「缺號」不存在）。**同樣不可逆的另一半：已經釋出的 migration 檔案內容不可以再改**——goose 一個版號只記一次、永不重跑，改動只會落在全新安裝上，兩種站從此 schema 不同而且沒有任何錯誤。要改就開新號。**這一半兩個來源都會紅**：`migrations/*.sql` 比對整個檔案，Go migration 比對它**整份原始碼檔**（不只 up/down 函式主體）——所以一個純機械的改動（改名波及、gofmt）也會叫，那時要由人決定怎麼辦，而不是由這道檢查默默放行。
  ⚠️ **「大於 main 的最大號」是必要條件，不是充分條件**：兩條同時在飛的分支各自對著同一顆 main 取 max+1 會取到同一個號，那是撞號、由 `TestMigrationVersionNumbersAreClaimedByExactlyOneSource` 在兩邊會合之後才看得見。取號前仍要掃所有遠端分支已占用的號。
  上面幾條由 `TestAStationAtTheReleasedVersionCanUpgradeToThisTree` 釘住：它照 `origin/main` 重播出一顆站台狀態的 DB 再跑 production 的 `runMigrations`。**它讀的是本機的 `origin/main` ref、不會自己 fetch**，所以本機跑之前先 `git fetch origin main`——ref 過期只會讓它變寬鬆、不會變嚴格。解析不到 `origin/main` 時本機 skip 並說明（skip 不是綠），在 CI 則是直接紅，因為 CI 沒有基線就等於這道檢查被關掉。

## 2. wire、route 與權限是同一個契約

- `spec/openapi.json` 先於 handler；`bin/gen-ocapi` 產生 `ocapi_gen.go`。route table 在 `routes.go`，它同時承載 auth、`requires`、MCP surface 與 handler；`conformance/routes_manifest.json` 與 spec 是核對來源。新增 endpoint 必須讓這幾個 executable source 同批一致，不能只改 handler；`TestRouteTableCoversSpecSurface` 把 route table 與 `spec/openapi.json` 雙向釘死，conformance 的 `test_openapi_covers_manifest` 再把 spec 與 manifest 釘成對稱相等，漏登記或殘留會在這兩道之一當場紅。⚠️ 這道保證是**跨語言的兩跳**：本機只跑 `go test ./...` 只涵蓋前半段，要連 conformance 一起跑才完整（CI 的 `bin/ci.sh` 兩者都跑）。兩跳都是集合比較，所以**看不見 `routes_manifest.json` 內部的重複列**——重複只會讓同一條 route 被多測一次，不會讓任何 route 逃掉權限測試。
- DTO 的 wire shape 由 `wire.go` 的手寫型別維護；`null`、空字串、缺欄與 `additionalProperties:false` 都是語意。不要因生成 struct 的 `omitempty` 偷掉既定 wire。
- verified token 的 `sub` 是 caller identity，不採信 request body 的 caller id。`requires` 是唯一 route capability floor；boot-time assertion 要拒絕未知 floor、auth／requires 不一致與漏寫 floor。MCP tool 的 caller、target、作用域必須沿用同一條 route gate，不得借 target 的身分。
- capability 階梯是 `machine < agent < admin_agent < owner`。`owner`、`admin_agent` 與 `MCPExclude` 的現行分配只以 `routes.go` 和治理測試為準，不在本檔硬編可變端點名單；`/api/mint`、owner 憑證與個人 push 類能力不能因「方便自動化」而下放。
- machine roster 是 machine token 的撤銷權威：刪除 machine 後下一個 request 應拒絕，檢查不可只靠 cached presence。這裡刻意是「lookup error 不等於 revoked」；未知 row 也不自動當成 revoked，避免資料讀取故障變成全機隊誤撤銷。
- 身分 mint 只在 server；warden、agent、worker 不自行 mint 或 bootstrap token。pull/bootstrap transition 不是新的授權路徑。

## 3. SSE、warden 與 reconcile

- hub 的 connect 是 online 的觀測來源；`/api/events` 在 stop gate 命中時要先回 409，再註冊 hub。SSE delta 的 topic、seq、epoch 由 `sseTopics`／hub 實作決定，不能在文件列 topic 或數字清單；seq 是 process-local，不是 durable cursor。
- warden child process 只收明確 allowlist 環境變數。instance 選擇、token 檔與 data path 由 server 的實例上下文決定；不可把 parent 的整包 environment 傳進 bootstrap／teardown。
- `reconcileDecide` 是 desired × observed 的純決策；cadence 是 sleep-then-tick，`--no-reconcile` 才停用 producer。不要為了讓 member 與 worker 表面對稱而合併它們的觀測輸入、lifecycle mask 或 task state。
- 外包 worker 的 machine placement：`desired_machine_id` 是 hard pin，無法使用時**什麼都不派**（碼裡的用詞是 STALL），只留下一張 `machine_unavailable` 的 receipt，不 fallback。⚠️ **這裡沒有 STOP**——在這個 repo 裡 STOP 是一個真的會送到 warden 的命令，照那個詞去找停止事件的人會找不到，因為根本沒有事件。碼自己的用詞在 `worker_spawn.go`，逐字：「the spawn **STALLS** with a receipt」、「**Nothing is dispatched**, and the stall is stamped onto the worker row」；`last_machine_id` 是 soft preference，無法使用才 fall through；第一次 launch 才走設定的 original chain。`last_machine_id` 寫實際 observed host，不寫 dispatch target，因為派出不等於送達。
- owner 對外包的 relocate、restart、model/runtime/effort 變更走同一個 owner-op handover funnel：先保留 session 狀態，再以 refocus epoch 與 announce 開一個收尾窗，收口是 worker 自己的 `report_stopped`——這三個成因 T-ed79 起**都沒有時鐘**（`winddownKindFor` 只把 `context_high` 與 owner 按的 `accelerated_stop` 判成加速停止）；沒有 live session 或已收尾的 epoch 不應重複收尾。正職與外包刻意不對稱：共用純 reconcile 決策，但 worker 仍有 task、codename、release 與 close-out 專屬生命週期。
- deactivate 打到 waking member 時，**要在覆寫 `stopping_since` 前完成的是那個判讀**（`cancellingWake`）——因為蓋上 `stopping_since` 本身就會終結 waking 投影，讀晚了就永遠讀不到；robust stop 的 dispatch 在 `putMember` 之後才發。已 offline 的 member 不派 stop。**已 online 的 member 不上任何時鐘**：owner 2026-08-16 裁定下線不兜底，`decideDown` 在 online 這一臂只把 member 放進 `stopping` 並且什麼都不派，收口是 agent 自己的 `report_stopped`（那一呼自己 dispatch robust stop）、owner 按強制下線，或 owner 按加速停止——最後這條會重蓋 `stopping_since` ＋ 寫 `refocus_op=accelerated_stop`，`decideDown` 才在 `stopping_since + RecycleGrace` 收（T-ed79；那是 owner 起的時鐘，不是兜底）。`stop_deadline`／`stop_grace` 的舊路仍在碼上，但被 `SoftOffboardGrace > 0` 擋住而不可達（常數 `SoftOffboardGraceSecs = 600`，`decideDown` 在那個條件成立時直接 return「收口是 agent 自己的 stopped report 或 owner 的強制停止」）—— **把它設成 0 反而會整套還原舊的計時收尾**，那條路只有測試在驅動。不要照著它實作一個計時器。dead worker 的 restart 依 `hub.IsOnline` 判 liveness，不能只看 `desired_state`。
- session boot anchor `member.session_boot_ts` 是 durable、不上 wire。reconnect 從持久值恢復 gauge；每一條成功 enqueue 的新 session start 與 stop boundary 都要清 anchor，使用 `SetMemberSessionBootTS` 單欄更新，不用整列 `PutMember` 代替。已知邊界：舊 snapshot 的整列 upsert 仍可能復活舊 anchor；若要改變該資料競態，另行取得 owner 裁定。
- session boundary 清掉的不只 anchor：`clearSessionBootTS` 把整組 **session-scoped gauge 讀數**一起刪（anchor、codex 輪數、context 讀數的兩半）。理由是這份 gauge 有**兩個讀者而且判準不同**——門檻端 `actionableContextPct` 要求 `context_pct_ts > boot_ts`，座艙／`get_monitoring` 端 `foldActorRuntime` 讀 raw 值不做這個檢查。留著舊 session 的讀數會讓「畫面顯示的百分比」與「門檻判斷用的百分比」變成兩個數字。不要為了讓面板不出現破折號而「保留」其中任何一半。
- `member.handover_noticed_ts`（換手提醒的 once-per-session 認領，同樣 durable、同樣不上 wire）刻意採**相反**策略：它的欄位建構子標了 `insertOnly`，所以**整列寫入只在建列時帶它、永遠不會蓋到既有的列**，只有 `SetMemberHandoverNoticedTS` 能移動它。理由是 `memberFromWorker` 從零重建 Member、不帶這個欄位，所以每次 `PutOutsourceWorker` 都會送 0 進來——拿掉那個 `insertOnly` 會讓外包 worker 的認領在每次狀態寫入時被歸零，等於讓「一個 session 只提醒一次」從另一扇門失效。這個約束由 `TestHandoverNotice_ClaimSurvivesAWholeRowUpsert` 守住，不是靠註解。
- `member.agent_iat_floor`（T-14 項目 4B，migrations/00063，同樣 durable、同樣不上 wire）跟 `handover_noticed_ts` 走**同一個策略**：欄位建構子標了 `insertOnly`，只有 `SetMemberAgentIatFloor` 能移動它，而且那一支是 SQL 裡的 `max()`、**只准往前**（同一份單調性也宣告在建構子的 `forwardOnly` 上，所以哪天它被允許蓋到既有的列，`max()` 已經是這一欄的性質、不必有人記得補）。理由多一條：它是**撤銷底線**，`memberFromWorker` 從零重建 Member 不帶這個欄位（所以每次 `PutOutsourceWorker` 都送 0），而 `report_waking` 抬高底線的同時，旁邊還有一堆手上拿著**抬高之前**的 member snapshot 的 HTTP 面——其中任何一個落地都等於把上一輪的憑證放回去用。Go 端 read-modify-write 也不行：兩輪靠近時輸的那個最後落地，底線會倒退。
- 蓋的值是**呼叫者自己 token 的 `iat`**，不是 `nowSecs()`；`requireAuth` 那一側是**嚴格小於**。這兩件事合起來才保證「抬高底線的那一輪不會被自己關在門外」，不受發證到開機的時間差與時鐘偏移影響。⚠️ **`iat` 只到整秒，同一秒起來的兩輪分不出來**（owner 2026-08-28 裁定先不管），這件事**沒有解決**。
- 🔴 讀的那一側（`authz.go agentIatFloorRefusal`）**明確把 `Kind == machineKind` 排除**，寫的那一側則不分 kind。這是安全性質不是最佳化：warden 憑證是 `scope="agent"` 且**沒有 `exp`**，底線一旦蓋過它就**永遠**過不了期，整台機器只能重裝才回得來。warden 今天不呼叫 `report_waking`，但那是今天的 client 而不是契約。由 `TestAgentIatFloor_WardenPermanentTokenIsExempt` 守住，不是靠註解。
- ⚠️ **上面兩欄不是「整列寫入不得帶」的全部，這份文件也不打算列全。** 那份清單會長，逐一寫在這裡就是一份會靜默過期的固定列舉（它已經過期過一次：`banked_cost` 搬走時沒有人回來加一行）。**權威是可執行的**：`single_column_writes_t14_test.go` 的 `singleColumnOwnedFields` 註冊表就是那份清單，`TestPutMemberNeverOverwritesSingleColumnOwnedFields` 會在任何一欄的 `insertOnly` 被拿掉時**當場紅並指名該欄**（`TestMemberColumnPropertiesAreDeclaredInOnePlace` 從另一頭釘住同一件事：它比對整份分類，欄位改變身分就紅）。要知道「今天有哪幾欄被搬走了」讀那張表，不要讀這裡。上面兩欄留在這裡是因為它們各自帶著**只有散文講得清楚的理由**（warden 憑證的永久豁免、`memberFromWorker` 從零重建會送 0），不是因為它們是清單。
- 🔴 搬一欄出去是**三件一組，缺一件等於沒做**：一支只寫該欄的寫入、**把該欄的建構子標成 `insertOnly`**（T-63 起沒有手寫的 `SET` 清單了，整列寫入蓋哪幾欄由這個旗標推導）、往註冊表加一列。只做前一件是這個 repo 已經發生過的形狀——`session_boot_ts` 與 `waking_since` 今天就各有一支單欄寫入、而兩欄都沒有標 `insertOnly`（上面 session anchor 那一條自承那是「已知邊界」），所以任何舊快照的整列寫入照樣蓋得回去。註冊表那支測試裡寫死的 `len(singleColumnOwnedFields) != N` 計數是**刻意**的：加一列就把 N 加一，**不准改成 `len(...)` 自比自**，否則「有人把整列刪掉」不會有任何東西看得見。

## 4. attachment、文件與 context

- chat、task message、create/answer reply card 的四個 attachment 寫入面，都要先驗證全部 attachment（至少 `id` 與 `data_b64`）再寫任何 row；使用帶 attachments 的原子 DAL seam，不把 blob、message、card 拆成可留下半筆的多次呼叫。
- 一般 blob reference、avatar reference、artifact reference 不可混用。avatar 是 stable member id 綁 `member.avatar_attachment_id` 的單一 owner blob；replacement、remove、hard delete 與 rollback 同 transaction 維護 pointer/blob。SSE 只發 delta，不攜帶圖片 bytes。
- `GET /api/chat/attachments/{attachment_id}/share-link` 的授權在 mint seam（`principalMachine`），回傳 server-relative 的 path；blob GET 仍是 streaming／MCPExclude。不要把 share link 當成登入後的短期 URL。sig **沒有到期時間、也不能單條撤銷**，但 T-62 之後**不再是不可撤銷**：它由當時在簽的那把金鑰派生，那把金鑰離開簽章金鑰環（設定 › 簽章金鑰的「移除」）時，它簽過的每一條連結同時失效 —— 粗粒度、由人決定時機。`?sig=` 有**兩種**，另一種是比較連結（`GET /api/diff`，T-59），兩者的 HMAC 標籤刻意分開、不能互相冒用，但**派生的是同一把環上金鑰**，所以生死完全一致；domain separation 是分「哪一種授權」，不是分壽命。哪一列吃哪一種，由 `RouteSpec.ShareSig` 帶的 verifier 決定，不在這裡列。
- 文件 history list 只回 metadata；body 用指定 kind + key 的 `get_document_version` 逐筆取，seed 用 `get_document_seed`。role definition、task manual SOP、task manual learnings 的 restore 需要文件仍存在；lessons 才能在 deleted role 上以 overlay + seed 恢復。SOP 與 learnings 是兩條各自有 cap/history 的序列，purpose、display、assignee 不在 version body 裡。
- context cap 的五個 key 是 duty、insight、learning、manual SOP、manual learnings，各自 accessor、預設值到上限 100000；`global_context` 與 `task.description` 不套這組 cap。長度單位是 Unicode rune，不是 byte。
- 五個受 cap 的寫面都遵守同一條：新內容未超過下限可寫；超過下限時必須嚴格短於舊內容；等長或變長拒絕。比較 caller-visible 的 folded content（含 seed/overlay），`allow_shrink` 是另一個 wipe guard，不是 cap bypass。回應要同時報 size 與 cap；manual 的 SOP、learnings 分開報。
- Duty、Insight、Learning 是不同文件，不自動互搬；Insight key 只有 `role_key`，沒有 task type。agent 編輯 Insight 前讀 server role docs 與 per-role seed；無 role 的 caller 沒有自己的 Insight capability。

## 5. telemetry、monitoring 與 avatar

- telemetry 的 `hardware`、`claude`、`runtimes` 是 producer 回報的 block；per-sample `hardware_ts`／`runtimes_ts` 與 entry `ts` 分開。freshness 由 server 算並上 wire，client 不自行重算。
- hardware stale 會收回數值但保留 timestamp；runtimes stale 保留 map 並標 `runtime_capabilities_stale`，因為 placement 仍需要最後能力資訊。machines 的列集合由 active warden roster 決定，不由 telemetry keys 決定；離線仍在冊要列，removed 不列，telemetry 不會復活 removed machine。
- `warden_shape` 是 warden 自報的 closed enum；`bin_status` 才是 server 以回報指紋和 embedded binary 比對出的結果。不要從另一個欄位推導缺席值。hardware 錯型別保留後由 read side 以 `hardware_invalid` 指名 fresh sample 的宣告鍵；runtimes 錯型別在 ingest 400，未宣告 hardware key 不算 invalid。
- `GET /api/monitoring` 的 sessions = active staff + live outsource workers；每個 model、runtime、effort、machine 等 telemetry 欄都只讀該 actor 的自報值，沒有 roster/config fallback。reported launch facts 落 durable 欄位，re-exec 後仍在；outsource DTO 的 model/effort 仍是 owner intent，不能拿 monitoring 值回寫編輯設定。released worker 不在 sessions，但仍在 actors/cost。
- avatar endpoint 的 owner gate、`MCPExclude`、warden/machine target 422、raw PNG/JPEG/WEBP magic bytes 與 64 KiB 上限是現行 wire；一般 `PutMember` 不得改 avatar pointer，專用 mutator 才能改，每次 replacement 產生新的 `ava-` id。

## 6. command receipt 與操作可見性

- warden `command_result` receipt 是「op 真的執行」的唯一證據；POST receipt 是 best-effort。server 只在 frame enqueue 成功後 arm `receipt_watch`，90 秒門檻來自 warden budget 推導、不是端到端量測。
- `receipt_missing` 由 server sweep 寫進既有 `last_op*`，語意是 `UNKNOWN` 而非 failed；解除條件是**它等的那台機器**的 receipt 抵達，必須在 `foldCommandResult` 的 early return 前 note。每次 dispatch 只 stamp 一次，且與其他 last-op reason 共用單槽；UNINSTALL 不掛這道死線。
- receipt 的「是哪一台回的」一律取自**已驗證 token**（`receiptReporterMachine`）：warden 憑證的 `sub` 就是 machine id、且刻意不帶 `machine_id` claim。`CommandResult` 不得為此長出 warden/機器欄位。解析不出來時回 `""` = UNKNOWN（不是「沒有人」），所有讀它的地方都必須退回改動前的行為。
- worker stop 重試判「刀砍下去了沒」不能只看 presence：**目標機器**回的 `no_such_session` receipt 就是收工證據（`noteWorkerStopNoSuchSession`）。別台廣播回的一律忽略。warden 一次 stop 只會回**三種** reason（`cli/ocwarden/command.go`，`rpc=stop` 與 legacy `rpc=worker_stop` 各自三種），今天只有中間那種收工：
  - `ok=true reason="stopped"` —— 真的殺掉了，**目前不收工，重試會再送一刀**（已知邊界，這一輪不修）。它會自癒：既然真的殺掉了，重送的那一刀打在已死的 session 上，warden 下一輪回的就是 `no_such_session`，於是收工 —— 代價是**最多多一次重送**，不是無限重試，方向也在安全那一側（寧可多殺一次，不要留殘活 session）。要不要收窄是另一個決定。
  - `ok=true reason="no_such_session: …"` —— 唯一收工的一種。
  - `ok=false reason="stop incomplete (…)"` —— **相反**的證據（session 還在），必須繼續重試，絕不可折成收工。

## 7. SQLite 與 backup

- write pool 開 WAL、上限 1、`_txlock=immediate`、`busy_timeout=5000`；read pool 8 條、`mode=ro`，且必須在 write pool migrate 後才開。`IMMEDIATE` 是讓 WAL 下第二個 handle 的 busy timeout 等待生效，不是取代 process 內 write pool 的序列化。
- DAL 欄位用 `wdb`／`rdb`，write seam 不可餵 read pool；守衛掃 package 內所有非-test Go source，不以 `dal*.go` 或現行檔名清單假設未來新增檔案會自動被涵蓋。
- WAL 下資料不再是單檔快照；禁止直接 cp、rename、move database path。使用 SQLite online backup（例如 `VACUUM INTO`）並保留其 WAL-safe 語意。`assertJournalMode` 問資料庫實際模式，異常 warning 但不因字串看似正確而漏報。
- backup health 只有 `backup_health.go` 一個 evaluator，API 只讀持久裁決；watchdog 在 `cmdServe` 同步接線，狀態落 `setting` 表。只有 scheduled backup 是排程存活證據；manual／pre-migration 不算。failure 立即可見、future timestamp 不算證據、`unknown` 永不折成 `healthy`，門檻一律由 `backupStaleAfter()` 和 wire `stale_after_secs` 共同決定。

## 8. task、conformance 與已知邊界

- task list 的 `statuses` 是可重複 query filter，與既有 `status` 並存；filter 互相 AND。`dep_tasks` 由一次 `ListTasks` map enrich，不做 N+1；`TaskCountDTO.total` 是未過濾總數。改動 tool surface 要同批更新 MCP catalog 與 conformance。
- 預設 config 是 CWD-relative；WAL 長 reader 可能讓 WAL 持續偏大，禁止以 read transaction 掩蓋這個約束；watchdog 自己若未啟動，沒有更高層監控替它報警。這些是已知邊界，不要在文件裡假裝已有自動修復。
- 新增或改 server 行為時，先讀實際 source、spec、manifest、seed 與相關測試，再改文件。新增 route 要同批補 wire/conformance；新增 write path 要確認 pool、transaction、receipt、SSE 與 durable projection；改 agent/worker 互動要同步檢查 mock／frontend parity。不要把 ticket、日期、mutant 計數、事故日記或可變清單重新寫回本檔。
