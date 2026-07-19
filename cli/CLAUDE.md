# cli/ — Go 自更新 binary(ocagent / ocwarden)

進入 `cli/` 時 nested-load。repo-wide 憲章 + 約定見 root `CLAUDE.md`;本檔記 cli 專屬。兩個獨立 go module,各自 `go.mod`。棧:go1.26。

## 命名(root §10)
- **`cli/ocagent/`** — Plane A:agent-side SSE listener(`ocagent listen` = agent 存活心跳;`ocagent context-report` 等)。folder = `module ocagent` = binary `ocagent`。
- **`cli/ocwarden/`** — Plane B:per-machine warden executor(stateless 執行手,拿 server push 的 token spawn member)。folder = `module ocwarden` = binary `ocwarden`。
- (已拆除)`cli/ocrelease/` 與 `server/ocupdaterd/` 隨 t-dc68 退役:發佈改走 GitHub Releases(`bin/release <tag>` 打包 + `gh release create` 出貨;server 端 update_check.go/upgrade.go 直接對 GitHub API 檢查與升級,見 `server/CLAUDE.md`)。
- ⚠️ **介面契約(已對齊 ocagent/ocwarden 命名,2026-07-09 owner 定案)**:spawn 寫的 bare **`ocagent` shim 呼叫名**(boot prompt 契約:spawned agent 跑 bare `ocagent listen`)+ launchd **label `com.officraft.ocwarden`**。它們是介面名(非 folder/module/binary),改動需 **host 端協調**(shim 重寫 / warden bootout+relaunch)。
- **同機多實例 namespace(`OC_NAMESPACE`,2026-07-11 owner 定案)**:單一 env 鍵所有 per-instance host 資源——root `~/.officraft-<ns>`、label `com.officraft.ocwarden.<ns>`、tmux socket `officraft-<ns>`、agent home(ns 非空時 spawn 額外 export `OC_AGENT_HOME`);字元集鎖 `[a-z0-9-]{1,16}`,非法即拒。**空 namespace = 主實例,輸出一個 byte 都不變**(golden 測試釘死)。導出邏輯單點在 `cli/ocwarden/namespace.go`;傳播線:server oc.toml `[server].namespace` → install.sh / bootstrap-here env → warden plist stamp → spawn export。

## 自更新 binary
- **改 Go → rebuild + commit prebuilt**(root §13)。committed prebuilt 在 `bin/ocagent` / `bin/ocwarden`;CI 第 7 道 golang gate 對 committed 做 parity(gofmt + vet + build + parity + go test),抓 committed ≠ 源。
- **self-update**:content-hash swap oracle + 防自殺 verify-before-swap(swap 前先驗新 binary 可跑)。install `env -u OC_ID`(防 shell OC_ID 污染)+ 餵回同 tokfile。ocwarden 換掉自己後 **exec-in-place**(`syscall.Exec` 同 PID、同 argv/env 原地換新,exec 失敗才 fallback exit(0))——**絕不賭 launchd KeepAlive 重拉**:實測 macOS gui-domain LaunchAgent 對 exit 0 的 warden 不重拉(job 停在 not running 直到人工 kickstart),exit-and-relaunch 舊路會讓每次 self-update 殺死該機看門狗。
- **warden restart 不斷 agent online**:ocagent 是獨立進程、持自己的 SSE,warden 重啟不影響。
- **發佈簽章 = 觀測不 enforce(T-33d5)**:發佈機以穩定 self-signed 憑證簽 bindist 的 ocwarden/ocagent(`bin/codesign-artifact`,讓 TCC 授權跨版延續);self-update swap 後只 **log** 新 binary 的簽章身分(`signatureOf` seam → `codesignIdentity`),**絕不驗簽硬擋**——self-signed 憑證在未信任機器上 verify 必非零,硬擋會 brick fleet。committed prebuilt 永遠不簽。詳見 `docs/dev.md` 發佈簽章節。
- **event-driven kick(T-c93d)+ `update` 動詞(T-5f01)**:self-update 的 15m 輪詢只是 backstop;`updater.Kick()`(buffered-1 去抖)有兩個 producer——SSE transport 每次成功 (re)connect(server 換版必踢斷所有 stream)、與 server push 的 **`update` warden-command 動詞**(owner 座艙一鍵升級 `POST /api/machines/{id}/upgrade` → `CommandDeps.Update` seam → 同一個 Kick)。update 動詞無 receipt(swap 自己會經 telemetry `self_update` 宣告);舊版 warden 對不認得的動詞 = log+skip 安全忽略(transport_test 釘死)。
- **心跳 binaries 指紋(T-5f01)**:30s telemetry payload 順帶 `binaries: {ocwarden, ocagent}`——live binary 的 sha256 12-hex 前綴(`fingerprint.go`,stat (size,mtime) cache 免每 30s 重讀 multi-MB)。server 拿它比對自己 embed 的 bindist hash 算機器表 `bin_status`(current/stale/缺=unknown)。**刻意 content-hash、不埋版號**(同 self-update swap oracle 理由:埋 sha 必造成 update 迴圈 + 抖 CI parity dryrun)。

## listen 自救(fail-closed,zombie 防線 B 的 client 半邊)
`ocagent listen` 兩道自救原本 fail-open(probe 失敗照樣活 = 殭屍永生),已改 fail-closed 帶寬限(`listen.go` 常數 + `listen_run.go` foldProbe/foldRefusal):
- **tmux session probe 三態**:alive / gone(tmux 明確答「無此 session」→ 2 連 miss 即自殺,不變)/ unknown(tmux 解析不到、spawn fault、timeout → 不再永遠當 alive:連續 `probeUnknownMin`(8)次 ∧ 滿 `probeUnknownGrace`(10min)才 self-exit;unknown 會重置 gone debounce,絕不瞬殺健康 listener)。
- **server 409 拒連 fail-closed**:`/api/events` pre-stream 409(server 殭屍 stop gate 或 dual-SSE)是權威「你不該在線」;**連續** `sseRefusalMin`(4)次 ∧ 跨滿 `sseRefusalGrace`(120s,鏡 stop_grace)→ 自我了斷(`suicide` 殺自己 tmux session,headless 則純退出)。任何其他結果(連上、網路錯、5xx、server 短暫掛掉)都**重置**計數——絕不因 server 抖動誤殺健康 agent。
warden `spawn.go` 寫一支 bare `ocagent` shim script → exec 真正的 golang `ocagent` binary。binary 解析順序:`OC_AGENT_BIN` 覆蓋 → home-install sibling `~/.officraft/warden/ocagent` → fallback repoRoot-relative `<repoRoot>/cli/ocagent/ocagent`(dev layout)。`resolveOcAgentBin`(`transport.go`)擁有此邏輯。⚠️ 改 spawn / 路徑前先讀 `spawn_test.go` 斷言(shim 內容精確比對)。

## 外包 worker 臨時 session 形態(M3 Phase 6;`cli/ocwarden/worker.go`)
owner 拍板「乾淨新建」:warden 長出**臨時 session** 形態伺候外包 worker(ow- id),**不借道成員通道、不污染成員生命週期**。與成員共享的只有純機制(Phase 2 spawn executor + Phase 3 robust-stop ladder + §7 PUSH band transport);不同的全在 worker.go:
- **A案 P5b 命名收斂:外包走成員動詞**——worker spawn 就是 `start`(member_id=ow-id、role="outsource-worker"),session = `member-<ow-id>`、workdir 在 agents/;kill 就是 `stop` {member_id}。
- **過渡 guard(舊 `worker-<ow-id>` 殘留不可永遠殺不掉)**:`stop` 帶 ow- 前綴 member_id 時額外掃殺派生的 legacy `worker-<id>` session(EXACT、絕不 pattern);legacy 動詞 `worker_stop` 仍收(舊 server 過渡 alias,走同一 Stop closure、workdir 依 prefix 解析 workers/);`worker_start` 已退役(unknown-rpc:log+skip)。
- **kill ladder 外門擴為「member- 或 worker- 才准」**(kill.go stop();其他 session 一律拒)。
- **無 command_result 回報**:worker 無 member row,fold-back 通道不適用;喚醒成敗由 server 從 worker 自己的 get_my_task 領工觀察。

## deploy
唯一安裝入口是 **`ocwarden install`**(Go,`cli/ocwarden/install.go`;flip 時期的 bash `bin/warden-install` 已退役刪除)。install 把 committed prebuilt 安到 `~/.officraft/warden/`(home,per-machine)並 render 真實 plist;plist template 在 `cli/ocwarden/deploy/`(REFERENCE,實際 plist 由 install 於 runtime 寫)。cutover 史料見 `cli/ocwarden/CUTOVER.md`。

**claude 路徑鏈(OC_CLAUDE_BIN stamp)**:launchd warden 的 minimal PATH 找不到 version-manager(asdf/nvm/volta)的 claude → runtime `resolveClaudeBin`(transport.go)的 ②LookPath/③common-dirs 全 miss。解法是**在還找得到的環節解析、stamp 進 plist 讓優先序① 命中**:(a) `ocwarden install` 於安裝環境解析 claude(`resolveClaudeForInstall`,install.go:OC_CLAUDE_BIN env → LookPath → common dirs),用 `--version` 在 minimal PATH 下實測——過 = 只 stamp OC_CLAUDE_BIN;不過但在 installer PATH 下過(shim/env-shebang)= 連 installer PATH 一起 stamp 進 plist;都找不到 = 印人話 WARNING+指引(裝 claude 或 export OC_CLAUDE_BIN 重跑),不 fatal。(b) bootstrap-here 鏈(server 在 launchd minimal env 下跑 `ocwarden install`):`bin/ocserver install`(使用者互動 shell 跑)先解析 claude、stamp OC_CLAUDE_BIN(+必要時 full PATH)進 **serve plist**;bootstrap-here 的 env passthrough(`api_machines.go`)原樣帶給 ocwarden install → 其解析優先序① 命中 → 轉 stamp 進 warden plist。foreground `ocwarden run` 的 OC_CLAUDE_BIN env 優先序不變(同一個優先序①)。

## bootstrap / teardown on server(一鍵,server 本機跑;server-side handlers)
server RUN ON 被操作的機器時,owner 不用 copy-paste shell,座艙一鍵讓 **server 在本機**跑 warden 起 / 收:
- **bootstrap-here**(`POST /api/machines/{id}/bootstrap-here`):server 解析 ocwarden binary(503 若缺)→ 跑 **`ocwarden install --force`**(帶 install 需要的 `OC_BASE`/`OC_TOKEN`;identity 只來自 token `sub`,**不注入 OC_ID 且會清掉 server process 繼承來的雜散 OC_ID**——對齊 self-update 的 `env -u OC_ID` 防污染)。`--force` = **一律 OVERWRITE** 前一個 warden(重裝、跳 skip-if-present),讓重裝可靠冪等。handler `handle_bootstrap_here`。
- **teardown-here**(`POST /api/machines/{id}/teardown-here`):bootstrap-here 的對稱反向。server 在**自己 host** 跑 **`ocwarden teardown`**(= `launchctl bootout` + **poll `launchctl print` 至 launchd 回報 label 真消失**(bootout 是 async;走 install 同一支 `bootoutUntilGone`)+ 移除 install artifacts,**靠 launchd 停 daemon、絕不 pkill**)。**CONFIRM-THEN-REMOVE**:僅在 daemon 確認 torn down(`exit_code == 0`,= label 確認消失 + artifacts 移除)才 soft-delete warden member;非零 / timeout 則 member 留在 roster(`removed=false`)——失敗的本機 teardown 不會把還活著的 daemon 從 registry 孤兒化,`log`(stdout+stderr)帶原因給 FE。handler `handle_teardown_here`。teardown 身分無關(只讀 HOME + uid),不需 token 接線。
- **治理**:兩者都是 **OWNER-ONLY**(路由表 `requires="owner"`,`service/authz.py` 單一 choke),**刻意比** remote-command uninstall / DELETE 的 `requires="admin_agent"` **更嚴**——這是在 server host 上跑碼的特權本機動作,連 mira(admin)治理 agent 都不准觸發。若座艙沒跑在該機上,fallback 是 copy-command 到遠端 shell 貼上跑。

## force-revive(activate 清 stopping/waking)
`activate`(wake)是 **force-revive**:清掉 member 的 `stopping_since` / `waking_since` 錨點,**不受 winding-down gate 擋**(即使 member 正 stopping / 甚至 online,wake 也回 200 不回 409)——讓「正在收」的 member 被重新拉回 online。行為釘在 conformance 套件與 `server/ocserverd/reconcile_test.go`;reconcile 對 genuine *stopped* terminal 也走 force-revive 覆蓋(`server/ocserverd/reconcile.go`)。
