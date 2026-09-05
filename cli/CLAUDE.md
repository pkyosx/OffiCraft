# cli/ — Go 自更新 binaries (`ocagent` / `ocwarden`)

進入 `cli/` 時讀本檔；repo-wide 規則在根目錄 `CLAUDE.md`。本檔只保留兩個 CLI module 會讓實作者猜錯的現行契約。行為權威看各 module source、wire/spec、conformance、可執行 guard 與測試；不在此列 route、檔案、job 或測試數量清單。

## 1. 身分、命名與 namespace

- `cli/ocagent/` 是 agent-side SSE listener 與 context/telemetry reporter；`cli/ocwarden/` 是 per-machine warden executor。各自是獨立 Go module，folder = module = binary；binary fresh build，不能 commit。release 依根檔，這裡不重抄退役 release daemon 或命令。
- bare `ocagent` 是 spawn/boot prompt 的 shim 呼叫名，`com.officraft.ocwarden` 是 launchd 介面名；兩者不是可任意改名的 folder/module，改動要連 host 端的 shim、plist、bootout/relaunch 一起對齊。
- `OC_NAMESPACE` 是所有 per-instance host 資源的單一輸入：OffiCraft root、launchd label、tmux socket、agent home。空值是 canonical instance，輸出必須 byte-for-byte 維持舊行為；非空值只准 `[a-z0-9-]{1,16}`。非法 namespace 必須拒絕／derive 空結果，不能折回 canonical agent home。
- namespace 軸在 `ocwarden/namespace.go`、`ocagent/config.go`、server onboarding、`bin/install.sh`、`bin/ocserver` 等獨立邊界各有鏡像；用 `bin/tests/fixtures/namespace-axes.tsv` 與 mirror guard 對齊。不要自己新增第二套 join、charset 或 fallback。

## 2. 真主機副作用與自更新

- `ocwarden install`／`teardown` 的 launchctl、filesystem、probe、download 副作用，production 只經 `realHostSeam()`／`newHostSeam`；測試由 `TestMain` 交換 fake seam。這保護「走過 seam 的入口」，不是對手工 composite literal 或新 process starter 的證明；新增 starter 要讓 AST zero-list query 與 process throat guard 看得到，測試不得直接呼叫 real host function。
- self-update 用 content-hash swap oracle，先 verify 新 binary 可執行才 swap。warden 必須 exec-in-place（同 PID、argv、env），不可依賴 launchd 的 KeepAlive 把 self-update 後退出的 warden 拉回來；agent 是獨立 process，warden restart 不應讓 agent 掉出 online。
- repo 已移除 code-signing 機制；不要重新引入 sign／identity knob，也不要把它和 root 規則保留的 TCC 身分錨點混為一談。binary identity 的現行證據是 bytes/hash 與 exec probe。
- 15 分鐘輪詢是 update backstop；SSE reconnect 與 server `update` command 都可 `updater.Kick()`，buffered-1 去抖。舊 warden 對未知 update verb 要安全 log+skip，不可因不認識新動詞而誤執行。
- **同一條 poll loop 有兩種醒法，而第三個生產者不是 `Kick`。** `renew` command 走 `updater.RenewNow`：先舉起換發需求、再 `Kick`。接成 `Kick` 會編譯、會跑、什麼都不換 —— warden 憑證沒有 `exp`，被叫醒的那一輪問「快到期了嗎」得到否定就結束了。這三條接線在 `wireUpdaterSeams`，因為原本它們在 realMain 的長駐分支裡、沒有任何測試進得去。
- **換發需求只在替換憑證真的寫下去之後才清除。** 站台不可達、探測被拒、寫檔失敗都保留需求、下一輪再試：進入時就消耗掉，一次失敗就會讓那台機器停在已退役的金鑰上，而從站台看它跟「還沒輪到」一模一樣。
- 30 秒心跳的 `warden_shape` 是從實際父行程 executable 判斷 `anchor`／`legacy`／`unknown`；每輪重讀，不看磁碟上「有沒有 anchor 檔」。省略與 `unknown` 不同：前者代表舊版未回報，後者代表新 build 讀不到父行程。binaries 只送 content hash，server 比 embedded bindist 算 `bin_status`，不埋版號。

## 3. TCC anchor 與 legacy migration

- launchd job leader 是永不被 self-update 抽換的 `officraft` TCC anchor，旁邊才 fork `ocwarden run`；anchor 安裝後不得覆寫，即使 bytes 相同也不能換 inode。embed、出貨與 `dist/officraft/` 必須是同一份 bytes。
- legacy→anchor 只看 launchd 實際 argv。沒有 anchor 時，migration 必須 stage 到 `.probe` → chmod／preflight → create-if-absent promote；任何失敗只清 probe，不能截斷或取代既有 anchor。preflight 要驗真正將被 launchd 啟動的 path，也要容忍 probe filename 不同。
- `cutover.failed` 代表本機自動遷移已回滾且永久停止重試；要重試的人必須先讀 cutover log、由 owner/操作員明確清 sentinel 後再 kickstart。不要把「磁碟已有 `officraft`」當成「遷移成功」。
- anchor cutover 會造成短暫 warden offline，但 agent 自己的 SSE 不依賴 warden；健康判定要等新 job 真正穩定。安裝／遷移任一步失敗應回到舊 shape；只有 rollback 也失敗才是需人工介入的非零狀態。

## 4. context、telemetry 與 command receipt

- `context-report` 的 30 秒 stamp 表示「上一輪 POST 全部被 server 接受」；只在成功後寫，失敗不可蓋健康戳。退避另存每 agent 一份 `context_report.backoff`，連續失敗從 30 秒倍增至 300 秒封頂；status 0 的連線故障也算失敗，成功立即清退避。讀檔壞／缺要 fail-open。
- session effort 取 statusLine payload 的 live `effort.level`，不是 `OC_EFFORT` 啟動意圖；model 送 `model.id`，不是 display name。兩者只送 `/api/monitoring/telemetry`，空值省略，不能塞進 `AgentContextIngestDTO`，也不能 fallback 回 roster/config。reported value 是 monitoring 的現況，不是 outsource editor 的 owner intent。
- warden 的 `dispatched … OK` 只在 command receipt 真的送達 server 時可印。若 op 已執行但 receipt 未送達，印 executed-but-undelivered；op 本身失敗永遠優先於 receipt transport error。UNINSTALL 的 receipt 仍是硬條件，不能改成 best-effort。

## 5. agent listener 與 worker session

- `ocagent listen` 對 tmux probe 與 server SSE refusal fail-closed 但有 debounce：tmux `gone` 連續兩次才退出；`unknown` 要連續 8 次且跨 10 分鐘；`/api/events` 409 要連續 4 次且跨 120 秒。連線成功、網路錯、5xx 或短暫 server down 都重置 refusal，避免把抖動當成該自殺。
- **斷線通知只在兩端，加上「我放棄了」**：一次 outage 只打擾 agent 兩次（第一次失敗、重新連上），中間每一次 retry 靜音。**這是 transcript 政策，不是退避政策**——backoff 常數不因此改動，重試頻率一格不變。重試迴圈真的停掉時必須另外印一行，否則「還在重試」與「已經放棄」在沉默裡無法分辨。重新連上那一行要自己講出站台有沒有換版，不要讓讀者去比對兩串 sha。codex 成員經 sidecar 的 forwarding filter 看同一套政策：只放行這三種通知，boot 的第一次連上改為開 post-boot wake 而不重複轉發。**`[ocagent] listen:` 這個開頭不可位移**（sidecar 有三個消費端靠它前綴匹配，其中一個是開機唯一一次的喚醒，漏掉不會報錯）。spawn 的 bare shim 由 `resolveOcAgentBin` 單點維護，順序是 home sibling → repo-relative dev binary（**沒有 `OC_AGENT_BIN` 這一段**——那個環境變數只在 `ocwarden install` 當複製來源，執行期的 warden 是找自己的 sibling）。它回的是**路徑＋那條路徑存不存在**，而且是**每次 spawn 重問一次**，不是開機時算一次：warden 起來的那一刻 sibling 還不在（**任何原因**都算——已知一條是「remote／手動安裝沒有複製 sibling」，由 self-update 的第一個 tick 事後補下來，見 `selfUpdateAgentPath` 的註解）⇒ 開機時算的那個值是一條當時不存在的路徑，而 `os.Symlink` 對不存在的目標照建不誤 ⇒ 成員拿到死捷徑、永遠連不上、沒有任何一層報錯。⚠️ **不要把它寫成「ocagent 是 warden 起來之後才下載的」**：`ocwarden install` 的順序是 `installOcAgent` → `writePlist` → `launchctlReinstall`，那條路上 ocagent 反而先落地。存在位是假時 `start()` 以 `ocagent_not_found` **拒絕這次 spawn**（理由帶回 server），不種死 symlink。
- agent 不直接 `rm` working tree 內容；harness 的 dangerous-rm gate 會讓 headless agent 卡死。agent 把待刪內容移到 `<workdir>/trash/`，warden 在 spawn／teardown 以 best-effort purge；purge 失敗不可改變 spawn/stop 結果，但任一 containment guard 不過就拒絕並留下 `REFUSED` log。只准清精確的 workdir trash：root/workdir 必須是絕對 clean path、workdir 是 root 的直接子目錄、trash 不能是 symlink，且要比較 realpath 以防 workdir symlink 指到鄰居或 root 外。
- 外包 worker 是臨時 session，不借道 member lifecycle；仍使用 `start`／`stop` member verbs、`member-<ow-id>` session 與 `ow-` id，並暫時清理舊 `worker-<id>` session。`worker_start` 已退役、`worker_stop` 只作過渡 alias；worker 沒有 member row，不走 command-result fold-back，成敗由 server 的 `report_waking` 觀察。

## 6. deploy、teardown 與目標選擇

- 安裝入口是 `ocwarden install`。server-host bootstrap/teardown 只在 server 自己的 host 執行；bootstrap 需顯式以 token `sub` 作身分並清掉繼承的雜散 `OC_ID`。可執行的 teardown core 必須先確認 launchd label 消失、artifact 移除，再 soft-delete roster；非零／timeout 不得把仍活著的 warden 標成 removed。
- **`OC_BASE` 沒有預設值，兩條路都拒絕猜。** `install` 拒絕安裝並非零退出（那個猜測會被寫進 plist，之後每次啟動都繼承）；`run` 停下來但**不退出** —— 不 spawn、不連線、不發任何請求，落一個 sentinel 說明原因。判斷式看的是 **env 有沒有被設定**，不是最終值長什麼樣：站台主機合法地把 `OC_BASE` 設成 loopback，拿值去比預設常數會拒絕那台機器。⚠️ **halt 過的 warden 不會自己好**（事後設對也要重啟）——這是刻意的，站台網址不是會自己到位的東西，「用到時才決定」那條修法在這裡不適用。真正抵達人的訊號是**這台機器不出現在名冊上**。設計理由與那個「退出之後 launchd 到底會不會重啟」的**未裁決矛盾**寫在 `cli/ocwarden/basegate.go` 檔頭，改這一格之前先讀它。
- `teardown-here` 的 HTTP handler 目前對 foreign target 與 server-self target 都回 409；不要從文件推導它會替任意 `{id}` 做破壞性 teardown。保留下層 core 的 explicit-target contract 與測試：canonical 必須顯式 `--canonical`，非空 namespace 只傳 `OC_NAMESPACE` 且不帶旗標，兩者互斥。
- CLI `teardown` 在推導任何 path 前先驗 target；裸 canonical teardown 拒絕且零 mutation。uninstall-RPC 的 self-teardown 刻意不走這個 CLI caller guard，因為 instance target 來自 warden 自己的 launchd environment，不是外部 wrapper 拼出來的。
- canonical 與 namespaced 的 root、serve label、warden label、tmux socket 必須四軸一致；E2E cleanup 每次 subprocess 都顯式帶 namespace。不能用 `pkill` 或含糊的「本機 warden」概念代替 explicit target。

## 7. force-revive

- `activate` 是 force-revive：會清 `stopping_since`／`waking_since`，不被 winding-down gate 擋，即使 member 正 stopping 或 online 也回 200。reconcile 對 genuine stopped terminal 也走相同 revive 規則；不要把 activate 當成只允許 offline 的普通 wake。

## 8. 修改與驗證

- 先讀 source、spec、manifest、seed 與相關測試，再改本檔；改 namespace 要跑 mirror guard，改 host effect 要確認 seam/process guard，改 telemetry 要檢查 wire 與 server projection，改 lifecycle 要同步檢查 frontend/mock/E2E parity。
- 乾淨 worktree 手跑 Go tests 前，依 root/server 的 staging 規則準備 embed assets；不要用缺 staging 造成的紅結果判定 regression。文件不列 ticket/date、mutant 數字、事故日記、現行檔案清單或可變命令清單。
