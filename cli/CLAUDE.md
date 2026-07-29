# cli/ — Go 自更新 binary(ocagent / ocwarden)

進入 `cli/` 時 nested-load。repo-wide 憲章 + 約定見 root `CLAUDE.md`;本檔記 cli 專屬。兩個獨立 go module,各自 `go.mod`。棧:go1.26。

## 命名(root §10)
- **`cli/ocagent/`** — Plane A:agent-side SSE listener(`ocagent listen` = agent 存活心跳;`ocagent context-report` 等)。folder = `module ocagent` = binary `ocagent`。
- **`cli/ocwarden/`** — Plane B:per-machine warden executor(stateless 執行手,拿 server push 的 token spawn member)。folder = `module ocwarden` = binary `ocwarden`。
- (已拆除)`cli/ocrelease/` 與 `server/ocupdaterd/` 隨 t-dc68 退役:發佈改走 GitHub Releases(`bin/release <tag>` 打包 + `gh release create` 出貨;server 端 update_check.go/upgrade.go 直接對 GitHub API 檢查與升級,見 `server/CLAUDE.md`)。
- ⚠️ **介面契約(已對齊 ocagent/ocwarden 命名,2026-07-09 owner 定案)**:spawn 寫的 bare **`ocagent` shim 呼叫名**(boot prompt 契約:spawned agent 跑 bare `ocagent listen`)+ launchd **label `com.officraft.ocwarden`**。它們是介面名(非 folder/module/binary),改動需 **host 端協調**(shim 重寫 / warden bootout+relaunch)。
- **同機多實例 namespace(`OC_NAMESPACE`,2026-07-11 owner 定案)**:單一 env 鍵所有 per-instance host 資源——root `~/.officraft-<ns>`、label `com.officraft.ocwarden.<ns>`、tmux socket `officraft-<ns>`、agent home(ns 非空時 spawn 額外 export `OC_AGENT_HOME`);字元集鎖 `[a-z0-9-]{1,16}`,非法即拒。**空 namespace = 主實例,輸出一個 byte 都不變**(golden 測試釘死)。導出邏輯單點在 `cli/ocwarden/namespace.go`;傳播線:server oc.toml `[server].namespace` → install.sh / bootstrap-here env → warden plist stamp → spawn export。
  - ⚠️ **跨 module 手抄鏡像(T-5047)**:同一條導出在**五個地方**各寫一遍——`cli/ocwarden/namespace.go`、`cli/ocagent/config.go`(`fallbackAgentsHome`,agents-home fallback root;T-5047 二輪審查前這裡**根本沒有 namespace**,硬寫 `~/.officraft/agents`,所以一個 namespaced ocagent 失去 `OC_AGENT_HOME` 就把 SSE cursor / reply-card seen / context-report stamp 寫進**主 instance** 的 agents 目錄)、`server/ocserverd/onboarding.go`(`wardenLaunchdLabel`/`officraftRootPath`/`wardenTokfilePath`)、`bin/install.sh`(`NS_DOT`/`NS_DASH`,install 與 uninstall 各一份)、`bin/ocserver`。兩個 go module 之間沒有 import、沒有編譯器,**差一個字元的後果不是字串錯**:server 問 launchd「`com.officraft.ocwarden.lab` 在不在?」得到「不在」(因為 warden 註冊的是別的 label),於是判定本機沒有 warden,**在活的 job 上再裝一個**。五份全部對**同一張表** `bin/tests/fixtures/namespace-axes.tsv` 對質(go 側 `cli/ocwarden/namespace_mirror_test.go` / `cli/ocagent/namespace_mirror_test.go` / `onboarding_mirror_test.go`,bash 側 + charset 走 `bin/tests/namespace-mirror-guard.sh`),所以漂掉時**紅的是漂掉那一份**,而不是只說「兩份不一樣」。加 namespace 案例改表就好,不必動五份測試碼。⚠️ ocagent 那份的**負向控制**在 `TestAgentsHomeFallback_RefusesRatherThanFoldingBack`:非法 namespace 必須 derive 出空字串,**絕不准折回主 instance 的 `~/.officraft/agents`**(namespace.go 自己就寫「malformed namespace 靜默折回主實例比硬錯誤糟得多」),而且它是唯一一份把 namespace **join 進路徑**的複本,charset 因此是防路徑逃逸(`../x`)而非裝飾。
- **`ocwarden install` / `teardown` 的 host seam(T-5047)**:所有真主機副作用(launchctl/檔案系統、claude/codex probe、ocagent 下載與 probe)在 production 只在 `realHostSeam()` 一處組裝,而且只能經 package 變數 `newHostSeam` 取得。`hostseam_test.go` 的 `TestMain` 在任何測試跑之前把它換成 fake,所以**凡是經由 `newHostSeam` 取得副作用的 entry point,在測試裡都拿到 fake**——新測試不必 opt-in、不必記得塞 fake,連「把待測碼裡的 guard 刪掉」也升不出去碰主機。
  - 🔴 **這個保證的邊界要講清楚,絕對形式是假的(獨立審查實證)**。舊版本檔寫「所有真主機副作用只在 `realHostSeam()` 一處組裝…兩條靜態守衛釘住這個結構」,而那兩條守衛只釘 **`realSysOps` / `realHostSeam` 兩個識別字**。審查者在 `teardownCmd` 直接寫 `sysOps{run: execRunner{…}.Run, rename: os.Rename, …}` 的 composite literal——不出現那兩個字串——三道守衛全綠、`refuseInTestBinary` 沒被呼叫,test binary 真的對本機 live warden 發出 `launchctl bootout`;測試最後才紅,訊息是「no host seam was constructed」= **事後偵測,不是事前阻止**。seam 保護的是「走過 seam 的 entry point」,不是「任何自己動手接線的碼」。
  - **現在真正的保護範圍(三層,由弱到強)**:(1) `scanHostSeamSource` 的識別字守衛——`realSysOps()` 全非測試碼只准出現一次、任何 entry point 不得直接呼叫 `realHostSeam()`;(2) **釘結構而非釘名字**——`sysOps{` 與 `execRunner{` 兩個 composite literal 在非測試碼各只准出現一次(分別在 `realSysOps` 與 `main.go` 的 `var newCmdRunner`),所以上面那個 hand-assembled mutant 現在在 `TestMain`、`m.Run()` 之前就被拒(親驗:整包零測試執行、`launchctl` PATH shim 零命中);(3) **process 咽喉點**——`execRunner.Run`(`main.go`)的 body 第一行就是 `refuseInTestBinary`。這一層是 hand-assembled struct **繞不過去**的:struct 是誰組的都無所謂,子行程還是得從那裡起。親驗:把 (2) 整條刪掉再跑同一個 mutant,`FATAL: execRunner.Run(launchctl) was reached from a test binary` + `os.Exit(1)`,shim log 空的(launchctl 行程從未被建立)、live warden 還在跑——**擋在碰到主機之前**。
  - 靜態守衛的比對一律只掃 code 行(`countCodeOccurrences` / `countRealSysOpsCalls` 剝註解),否則守衛會被自己的文件滿足——同一個坑 `bin/tests/namespace-mirror-guard.sh` 的 `code_only()` 早已踩過,server 側 `api_machines_childenv_wiring_test.go` 也才剛因此被抓到一條恆真斷言。
  - `TestHostSeam_StructureIsReported` 是**唯一一條** reporting wrapper(不是 enforcement、不是覆蓋率:`TestMain` 已先跑同一個 scan 並 `os.Exit(1)`,所以它的迴圈體恆不執行)。⚠️ **不要再加第二條**:原本這裡有兩條函式本體逐字相同的守護測試,審查者把兩者迴圈體換成 `panic` 跑整包,照樣 `ok` + 兩條 `--- PASS`。新性質加進 `scanHostSeamSource`,不要加新的 no-op 測試。

## 自更新 binary
- **改 Go → fresh build 驗證，binary 永不 commit**(root §13)。CI Go gate 跑 gofmt、vet、build 與 test；只有本機剛好留有 gitignored `bin/ocagent` / `bin/ocwarden` 時才做 parity dryrun。發佈的 binary 由 release 流程 fresh build。
- **self-update**:content-hash swap oracle + 防自殺 verify-before-swap(swap 前先驗新 binary 可跑)。install `env -u OC_ID`(防 shell OC_ID 污染)+ 餵回同 tokfile。ocwarden 換掉自己後 **exec-in-place**(`syscall.Exec` 同 PID、同 argv/env 原地換新,exec 失敗才 fallback exit(0))——**絕不賭 launchd KeepAlive 重拉**:實測 macOS gui-domain LaunchAgent 對 exit 0 的 warden 不重拉(job 停在 not running 直到人工 kickstart),exit-and-relaunch 舊路會讓每次 self-update 殺死該機看門狗。
- **warden restart 不斷 agent online**:ocagent 是獨立進程、持自己的 SSE,warden 重啟不影響。
- 🔴 **TCC identity anchor(`cli/ocwarden/anchor.go`)——launchd job 啟動的不再是 ocwarden 本身**。plist 的 `ProgramArguments` 從 `[BIN, run]` 改成 `[ANCHOR, anchor, BIN, run]`,`ANCHOR` = `$ROOT/warden/ocanchor`,是 ocwarden 的**凍結副本**,由第一次發現它不存在的 install 寫入,之後**任何 install 都不再覆寫**(`installAnchor`)。
  - **為什麼**:macOS 把每一個 TCC 決定歸給 **responsible process**,而 launchd 讓 job 的主 process 成為自己的 responsible;底下整棵樹不管 exec 成什麼 binary 都繼承同一個(實測:tmux server 與每個 agent pane 都回報 plist 指的那支)。ocwarden 是 adhoc 簽章 → TCC 只能用 cdhash 認人 → **self-update 每換一次 binary,全機授權就作廢一次**。
  - **後果不是「拒絕」而是「卡住」**:授權紀錄對不上時 tccd 走的是**跳提示**,而 LaunchAgent 沒有 GUI 可以顯示,於是 `open(2)` 在 kernel 裡永遠不返回。實測症狀:新 pane 裡的 `claude` 一個 byte 都沒印就掛住,同一個 pane 裡 `ls ~/Documents` 一樣掛住,而從正常登入 shell 起的 tmux server 上同一條指令秒回。**任何 log 都沒有東西**。
  - ⚠️ **`installAnchor` 拒絕覆寫既有 anchor,那個「拒絕」就是修復本身**。用同一份 build 的相同 bytes 重寫也一樣會換掉 inode、作廢授權、讓全機 agent 掛住——也就是把這個 bug 原封不動犯回去。釘死它的是 `TestInstallAnchor_NeverOverwritesExisting` 與 `TestRunInstall_ReinstallKeepsTheAnchorButRefreshesTheWarden`(兩條都對「把守衛刪掉」的 mutant 驗證過會紅)。
  - ⚠️ **`anchor` 動詞是 fork,不是 exec**。exec 會保住 pid 但把 code identity 換成我們正想避開的那支 binary = 這個修復整個消失,而且測試全綠、要等幾個月後 operator 的授權過期才會發現。
  - ⚠️ **`anchor.go` 是凍結契約**。field 上那份 anchor 是它安裝當天的那個 ocwarden 版本,永遠不會被更新——改這裡的語意**不會**改到現有機器,只會讓新舊機器行為分岐。真要改語意就換一個 anchor 檔名 + 明文要求重新授權,不要指望 install 會替你換檔。
  - **代價**:每台機器仍需**一次**人工授權(System Settings → 完全取用磁碟 → 加入 anchor 路徑),install 在寫入新 anchor 時會把這行印出來。沒授權之前,agent 碰到受保護目錄是**掛住**不是失敗。
- **發佈簽章 = 觀測不 enforce(T-33d5),且 T-588c 起預設不簽**:`bin/codesign-artifact` 可以用穩定 self-signed 憑證簽 bindist 的 ocwarden/ocagent,但**預設完全不跑、連 keychain 都不看**,要簽得明確要求(`bin/release publish --sign`)。所以實務上這個 loop 換進去的 binary 是 adhoc 的。self-update swap 後只 **log** 新 binary 的簽章身分(`signatureOf` seam → `codesignIdentity`),**絕不驗簽硬擋**——self-signed 憑證在未信任機器上 verify 必非零,硬擋會 brick fleet。簽章只活在發佈 artifact，不進 git。
  - ⚠️ **不要寫「簽章讓 TCC 授權跨版延續」,也不要寫「已證明 self-signed 對 TCC 無用」——兩個方向都是過度宣稱**。現況:self-signed codesign 對 macOS TCC 授權是否有效,目前**只是高度懷疑無效、沒有 100% 結論**(owner 2026-07-26;依據:在**有簽章的那段期間他仍碰過不只一次授權詢問**)。預設不簽的理由是**「它卡測試又卡發佈」**,**不是**因為已證明它無用。唯一權威表述在 `bin/codesign-artifact` 檔頭,其他地方一律指向它、不要自行改寫。詳見 `docs/dev/README.md` 發佈簽章節。
  - **上面那個問題與 TCC identity anchor 正交,anchor 沒有回答它**。anchor 不是讓身分「跨 binary 抽換存活」,而是讓**承載身分的那支 binary 根本不被抽換**——所以它對「self-signed 到底有沒有用」不構成任何證據,兩邊不要互相引用作結論。倒是 anchor 解釋了 owner 當時觀察到的現象為何會發生(adhoc → cdhash 認人 → 每次 self-update 換掉它),那段 15 分鐘輪詢的 self-update 正是授權反覆消失的來源。
- **event-driven kick(T-c93d)+ `update` 動詞(T-5f01)**:self-update 的 15m 輪詢只是 backstop;`updater.Kick()`(buffered-1 去抖)有兩個 producer——SSE transport 每次成功 (re)connect(server 換版必踢斷所有 stream)、與 server push 的 **`update` warden-command 動詞**(owner 座艙一鍵升級 `POST /api/machines/{id}/upgrade` → `CommandDeps.Update` seam → 同一個 Kick)。update 動詞無 receipt(swap 自己會經 telemetry `self_update` 宣告);舊版 warden 對不認得的動詞 = log+skip 安全忽略(transport_test 釘死)。
- **心跳 binaries 指紋(T-5f01)**:30s telemetry payload 順帶 `binaries: {ocwarden, ocagent}`——live binary 的 sha256 12-hex 前綴(`fingerprint.go`,stat (size,mtime) cache 免每 30s 重讀 multi-MB)。server 拿它比對自己 embed 的 bindist hash 算機器表 `bin_status`(current/stale/缺=unknown)。**刻意 content-hash、不埋版號**(同 self-update swap oracle 理由:埋 sha 必造成 update 迴圈 + 抖 CI parity dryrun)。

## context-report 節流戳記綁 POST 成敗(T-b36a step 1;`cli/ocagent/contextreport.go`)
`reportThrottleSecs = 30.0` 的戳記(`reportStampPath`,agent home 下的節流檔)**只在這一輪
所嘗試的每個 POST 都被 server 接受時才寫**(context POST 在 pct 缺席時本來就跳過,不算失敗)。
原本是無條件寫,兩層傷害:(a) 一個剛更新的戳記是這條路上唯一對外可讀的「我有在送」證據,
失敗也蓋等於主動論證一件假的事;(b) 它把 30 秒內的重試整個吃掉——對一個回 422 的 server
打第一次,stderr 印 FAILED、戳記照樣寫下,下一個 tick 被自己失敗的戳記判定為節流,server
早已恢復也要等下一個窗才知道。⚠️ 別把那行 `writeStamp` 搬回 POST 之前,也別「為了避免打爆
server」而在失敗路徑補寫戳記——退避是退避,節流戳記是「上一次成功回報在何時」。

### 失敗退避有上限(T-d11f;同檔的 `context_report.backoff`)
上面那條修好之後留下另一半:`context-report` 是 **one-shot 進程**(Claude Code 每次
statusLine render 都重跑一次,一秒好幾次),窗沒開 = **下一個 tick 立刻重送**。所以對一台
持續拒收的 server,節流等於整個關掉——**實測**(真 binary × 一直回 500 的假端點,20 ticks)
每次 tick 都送、間隔 ~0.4s、無上限。⚠️ **這是三條定期回報路徑裡唯一沒上限的那條**,另兩條
不必動:`cli/ocwarden/main.go` 的 report loop 早有 `backoffStart`1s→`backoffCap`60s 的倍增
(實測 gap 1/2/4/8/16s);`cli/ocwarden/codex_session.go` 的 `allowUsageReport()` 是
**attempt 就蓋** 的 30s 節流,失敗根本不重試(它的問題是另一種:失敗不可見,不在本票)。
- 修法是**第二個檔**,不是把狀態塞回戳記:`context_report.backoff`(戳記的 sibling,內容
  `<連續失敗次數> <上次嘗試時間>`)。兩個 gate 各答各的問題——戳記答「上次**送達**在何時」,
  backoff 答「server 已經拒了多久」。🔴 **絕不可為了退避而在失敗時蓋戳**,那等於把「100%
  被拒的線路看起來很健康」那個 bug 再犯一次。
- 曲線:`reportBackoffSecs(n)` = 30s 起、每多一次連續失敗翻倍、封頂
  `reportBackoffCapSecs = 300`s → **30 / 60 / 120 / 240 / 300 / 300…**。第一次失敗刻意就是
  30s(= 正常窗),所以**失敗中的 reporter 永遠不會比健康的更密**。封頂是刻意的:沒有它,
  一小時的 outage 會把重試間隔推得比 outage 本身還長,server 復活也沒人知道。
- **送達一次就 `clearReportBackoff` 立刻歸零**——退避絕不可活得比造成它的 outage 久。
- ⚠️ **傳輸故障(連不上 / connection refused / timeout,即 `reportPost` 的 status 0)也算失敗、
  也會退避**(實測:server 沒起來時 5 個快 tick 只出 1 個 burst、`failures=1`)。這是**刻意
  跟上面 `ocwarden` 的 `Posted || Status == 0 → reset` 相反**:那條把傳輸故障當「不是伺服器
  的錯」而歸零,但兩者形狀不同——ocwarden 的退避活在 loop 裡、地板本來就是 1 秒一次;
  這支是每次 render 重跑、一秒好幾次,「server 掛了」正是重送風暴最兇、而對方最沒能力吸收的
  情況。這裡跟著歸零 = 對最常見的 outage 完全沒修到。
- 路徑**每個 agent 一份**(`<home>/<id>/…`,無 id 則 `anon`):`cfg.Home` 在 production 是
  **共用的 agents root**,少掉 id 那段會變成整台機器共用一個檔 → 一個 agent 的 outage 靜音
  掉所有 agent。`TestReportStatePathsAreLiteralAndPerAgent` 用**字面路徑**釘住(不呼叫
  `reportStampPath`/`reportBackoffPath` 算期望值——那樣測試只會跟自己同意,少一段也全綠)。
- 讀檔一律 **fail-open**(缺檔 / 空 / 壞格式 = 不抑制),與 `reportThrottled` 同向。
- 測試(`contextreport_test.go`)用 `driveTicks` 在**虛擬時間**上模擬 tick loop(`now` 本來
  就是注入參數,**不准真 sleep**),直接釘死 gap 序列 `[30 60 120 240 300 300 300 300]`;
  `TestRefusedReportBacksOffToAnIntentionalCap` 的**名字與註解就寫明上限是刻意的**——舊的
  `TestRefusedReportDoesNotStampThrottle` 把「每 tick 重送」釘成期望值,看不出無上限是刻意
  還是沒想到。哨兵 `TestHealthyCadenceIsUnchangedByTheFailureBackoff` 釘成功路徑一字未變
  (600s 內 burst 恰在 0/30/60/…,且**不產生** backoff 檔)。

## listen 自救(fail-closed,zombie 防線 B 的 client 半邊)
`ocagent listen` 兩道自救原本 fail-open(probe 失敗照樣活 = 殭屍永生),已改 fail-closed 帶寬限(`listen.go` 常數 + `listen_run.go` foldProbe/foldRefusal):
- **tmux session probe 三態**:alive / gone(tmux 明確答「無此 session」→ 2 連 miss 即自殺,不變)/ unknown(tmux 解析不到、spawn fault、timeout → 不再永遠當 alive:連續 `probeUnknownMin`(8)次 ∧ 滿 `probeUnknownGrace`(10min)才 self-exit;unknown 會重置 gone debounce,絕不瞬殺健康 listener)。
- **server 409 拒連 fail-closed**:`/api/events` pre-stream 409(server 殭屍 stop gate 或 dual-SSE)是權威「你不該在線」;**連續** `sseRefusalMin`(4)次 ∧ 跨滿 `sseRefusalGrace`(120s,鏡 stop_grace)→ 自我了斷(`suicide` 殺自己 tmux session,headless 則純退出)。任何其他結果(連上、網路錯、5xx、server 短暫掛掉)都**重置**計數——絕不因 server 抖動誤殺健康 agent。
warden `spawn.go` 寫一支 bare `ocagent` shim script → exec 真正的 golang `ocagent` binary。binary 解析順序:`OC_AGENT_BIN` 覆蓋 → home-install sibling `~/.officraft/warden/ocagent` → fallback repoRoot-relative `<repoRoot>/cli/ocagent/ocagent`(dev layout)。`resolveOcAgentBin`(`transport.go`)擁有此邏輯。⚠️ 改 spawn / 路徑前先讀 `spawn_test.go` 斷言(shim 內容精確比對)。

## trash 清理(T-684c;`cli/ocwarden/trash.go`)
「agent `mv`、warden `rm`」的 **rm 半邊**。**基石不是「mv 比 rm 安全」**(實測相對/絕對 × mv/rm 四種組合鑑別力為零)——基石是**刪除的執行者從 agent 換成 warden**:Claude Code harness 有一道**任何 settings/permission 都豁免不掉**的 dangerous-rm 確認提示(`Dangerous rm operation on working directory or its ancestor` + Yes/No),headless agent 前面沒有人按 → **靜默卡死**。warden 是 launchd 直起的獨立 Go 常駐程式、鏈上沒有 claude,它刪檔不在那道檢查的管轄內。seeds(`seeds/system_interaction.md` §10.5 / `seeds/worker_context.md` §6 / 前端逐字副本 `frontend/src/api/seeds.ts` / server 的 task-close nudge `sse_bands.go`)因此改教 agent:**暫存 `mv` 進 `<workdir>/trash/`,不要自己 `rm`**。

- **兩個掛點**:spawn(`spawn.go` `SpawnDeps.start`,MkdirAll workdir 之後,`PurgeTrash` seam)與 teardown(`kill.go` `stop()` 尾端,`sweepSeams.purgeTrash` seam);兩個 seam 都在 `transport.go` `buildCommandDeps` 綁定真實 `purgeTrash`。兩者皆 **nil-skipped、best-effort**:清不掉絕不 fail spawn、也絕不改 stop 的死亡判定。
- **fail-closed**:`purgeTrash(root, workdir, logf)` 只刪 `<workdir>/trash`。任一守衛不過 → **拒絕不動,並在 warden stderr(`<logDir>/ocwarden.err.log`)留 `REFUSED` 行**,不靜默跳過。trash 不存在 = 正常狀態,安靜 no-op、不當異常。
  - **字串層**:root/workdir 任一為空(G1)、非絕對路徑(G2)、非 `filepath.Clean` 正規形(G3,擋 agent id = `../..`)、`Dir(workdir) != root`(G4,直屬子目錄的**精確**判定,不是 HasPrefix——`/x/agentsEVIL` 會被前綴法放行;同時擋掉 workdir==root 與孫層)。
  - **檔案系統層**:`trash` 是 symlink(G5,`Lstat` 不是 `Stat`)、`trash` 不是目錄(G6)、**`EvalSymlinks(workdir) != EvalSymlinks(root)/Base(workdir)`(G7)**、trash 解析後跑出自己的 workdir(G8,G5 的 TOCTOU 備援)。
  - ⚠️ G7 刻意**比「解析後仍是 root 直屬子目錄」更嚴**:workdir symlink 指向**鄰居 agent** 時,鄰居本身也是直屬子目錄,只做 `Dir()==root` 會放行並誤刪鄰居的 trash(review 建議的修法版本就漏了這條,實測紅)。要求 basename 撐過解析 = 「這個 id 必須真的擁有這個目錄」,才是我們實際依賴的性質。
  - 🔴 **G7 是本模組唯一真正的 containment 守衛,別把它跟 G4 混為一談。** G4 只證明**路徑字串**長得對;**workdir 自己是 symlink** 時 G4 照樣成立,但檔案系統上它可以指到任何地方。這個洞在 review 被抓到(第一版把 `EvalSymlinks(trash)` vs `EvalSymlinks(workdir)/trash` 誤當成 ancestor 檢查——那在 G5 存在時是**恆真式**,把它的拒絕分支換成 `panic` 跑整包測試**一次都不會觸發**)。**root 必須被帶進比較,否則等於什麼都沒檢查。** 兩邊都過 `EvalSymlinks` 才不會誤殺 macOS `/var → /private/var` 這種合法祖先 symlink。
- 測試在 `trash_test.go`,**兩個方向都釘**:trash 內的東西會被清掉;trash 以外(workdir 的 `tmp/`、`keep.txt`、`.oc-token`、鄰居 agent 的 workdir 與其 trash、agents root 本身、被 symlink 指到的 workdir 外資料、以及 **workdir 自己是 symlink 指向 root 外 / 指向鄰居 agent**)一個都不准動——每個拒絕案例都額外斷言「受害者檔案還在」當**負向控制組**,只斷言回傳 false 不算數。⚠️ 最後那兩條(workdir symlink)是 G7 的專屬鑑別測試,**刪了 G7 就靠它們變紅**——第一版沒有它們,所以加一整道新守衛時既有測試零反應。

## 外包 worker 臨時 session 形態(M3 Phase 6;`cli/ocwarden/worker.go`)
owner 拍板「乾淨新建」:warden 長出**臨時 session** 形態伺候外包 worker(ow- id),**不借道成員通道、不污染成員生命週期**。與成員共享的只有純機制(Phase 2 spawn executor + Phase 3 robust-stop ladder + §7 PUSH band transport);不同的全在 worker.go:
- **A案 P5b 命名收斂:外包走成員動詞**——worker spawn 就是 `start`(member_id=ow-id、role="outsource-worker"),session = `member-<ow-id>`、workdir 在 agents/;kill 就是 `stop` {member_id}。
- **過渡 guard(舊 `worker-<ow-id>` 殘留不可永遠殺不掉)**:`stop` 帶 ow- 前綴 member_id 時額外掃殺派生的 legacy `worker-<id>` session(EXACT、絕不 pattern);legacy 動詞 `worker_stop` 仍收(舊 server 過渡 alias,走同一 Stop closure、workdir 依 prefix 解析 workers/);`worker_start` 已退役(unknown-rpc:log+skip)。
- **kill ladder 外門擴為「member- 或 worker- 才准」**(kill.go stop();其他 session 一律拒)。
- **無 command_result 回報**:worker 無 member row,fold-back 通道不適用;喚醒成敗由 server 從 worker 自己的 get_my_task 領工觀察。

## deploy
唯一安裝入口是 **`ocwarden install`**(Go,`cli/ocwarden/install.go`;flip 時期的 bash `bin/warden-install` 已退役刪除)。install 把發佈流程 fresh build 的 binary 安到 `~/.officraft/warden/`(home,per-machine)並 render 真實 plist;plist template 在 `cli/ocwarden/deploy/`(REFERENCE,實際 plist 由 install 於 runtime 寫)。cutover 史料見 `cli/ocwarden/CUTOVER.md`。

**claude 路徑鏈(OC_CLAUDE_BIN stamp)**:launchd warden 的 minimal PATH 找不到 version-manager(asdf/nvm/volta)的 claude → runtime `resolveClaudeBin`(transport.go)的 ②LookPath/③common-dirs 全 miss。解法是**在還找得到的環節解析、stamp 進 plist 讓優先序① 命中**:(a) `ocwarden install` 於安裝環境解析 claude(`resolveClaudeForInstall`,install.go:OC_CLAUDE_BIN env → LookPath → common dirs),用 `--version` 在 minimal PATH 下實測——過 = 只 stamp OC_CLAUDE_BIN;不過但在 installer PATH 下過(shim/env-shebang)= 連 installer PATH 一起 stamp 進 plist;都找不到 = 印人話 WARNING+指引(裝 claude 或 export OC_CLAUDE_BIN 重跑),不 fatal。(b) bootstrap-here 鏈(server 在 launchd minimal env 下跑 `ocwarden install`):`bin/ocserver install`(使用者互動 shell 跑)先解析 claude、stamp OC_CLAUDE_BIN(+必要時 full PATH)進 **serve plist**;bootstrap-here 的 env passthrough(`api_machines.go`)原樣帶給 ocwarden install → 其解析優先序① 命中 → 轉 stamp 進 warden plist。foreground `ocwarden run` 的 OC_CLAUDE_BIN env 優先序不變(同一個優先序①)。

## bootstrap / teardown on server(一鍵,server 本機跑;server-side handlers)
server RUN ON 被操作的機器時,owner 不用 copy-paste shell,座艙一鍵讓 **server 在本機**跑 warden 起 / 收:
- **bootstrap-here**(`POST /api/machines/{id}/bootstrap-here`):server 解析 ocwarden binary(503 若缺)→ 跑 **`ocwarden install --force`**(帶 install 需要的 `OC_BASE`/`OC_TOKEN`;identity 只來自 token `sub`,**不注入 OC_ID 且會清掉 server process 繼承來的雜散 OC_ID**——對齊 self-update 的 `env -u OC_ID` 防污染)。`--force` = **一律 OVERWRITE** 前一個 warden(重裝、跳 skip-if-present),讓重裝可靠冪等。handler `handle_bootstrap_here`。
- **teardown-here**(`POST /api/machines/{id}/teardown-here`):bootstrap-here 的對稱反向。server 在**自己 host** 跑 **`ocwarden teardown`(顯式目標,見下方 §teardown 顯式目標契約:canonical instance 送 `--canonical`、namespaced instance 送 `OC_NAMESPACE` 且**不**帶旗標)**(= `launchctl bootout` + **poll `launchctl print` 至 launchd 回報 label 真消失**(bootout 是 async;走 install 同一支 `bootoutUntilGone`)+ 移除 install artifacts,**靠 launchd 停 daemon、絕不 pkill**)。**CONFIRM-THEN-REMOVE**:僅在 daemon 確認 torn down(`exit_code == 0`,= label 確認消失 + artifacts 移除)才 soft-delete warden member;非零 / timeout 則 member 留在 roster(`removed=false`)——失敗的本機 teardown 不會把還活著的 daemon 從 registry 孤兒化,`log`(stdout+stderr)帶原因給 FE。handler `handle_teardown_here`。teardown 身分無關(只讀 HOME + uid),不需 token 接線。
  🔴 **T-42a0(2026-07-27):上面整段描述的是那條路徑「會做什麼」,但它現在對任何 `{id}` 都不會被走到——兩道 409 擋在 subprocess 之前。**
  上一句「teardown 身分無關(只讀 HOME + uid)」正是缺陷本身:**沒有機器選擇器 ⇒ 它只能拆 server 自己這台**,而 handler 從前照樣把 `RosterStatusRemoved` 寫到**被指名的那台**頭上——一次點擊,本機 daemon 被 launchd bootout、一台從沒被連絡過的機器掉出名冊(T-9cf8 之後連憑證一起撤銷)。現在:
  - **指名別台 → 409**(`teardownHereForeignTargetMsg`):這個動詞碰不到那台。別台退役走 `uninstall_machine` → `delete_machine`。
  - **指名 server 這台 → 409**(`serverSelfUndeletableMsg`,T-9cf8):修這台的 warden 用 **bootstrap-here / `install_warden_on_server_host`**(`install --force` 本來就覆蓋既有安裝,不需先拆)。
  兩道拒絕收斂在 `server/ocserverd/api_machines.go` 的**單一** `teardownHereRefusal`(寫成兩道連續 `if` 時第二個是可證恆真的死條件,獨立審查實測 `if true` 全綠)。**CONFIRM-THEN-REMOVE 那段因此經 HTTP 不可達**;端點要不要退役是待 owner 裁定的獨立問題。詳見 `server/CLAUDE.md` 的 T-42a0 條。
- **治理**:兩者都是 `requires="admin_agent"`(`server/ocserverd/authz.go` 單一 choke)——T-6020(owner 2026-07-26 拍板)把它們從 owner-only 降到 remote-command uninstall / DELETE **同一級**:在 server host 上裝機拆機是 admin 助理該能跑的辦公室維運。**一般 agent 與 warden 仍是 flat 403**(這仍是在 server host 上跑碼的特權本機動作,只是治理層而非 owner 一人)。若座艙沒跑在該機上,fallback 是 copy-command 到遠端 shell 貼上跑。

### teardown 顯式目標契約(T-2257,2026-07-25 事故後)
`ocwarden teardown` **fail-closed、不再有隱含目標**。事故:一個 namespaced E2E cleanup 跑了沒帶 `--namespace` 的 `ocwarden teardown`,fallback 到 **canonical** warden,在一台 fleet 機上殺掉 live warden、刪掉 canonical launchd plist 與 `exec-warden.tok`(不可復原),7 個 live agent 失去監管。

- **裸 `ocwarden teardown`(OC_NAMESPACE 空、無 `--canonical`)= 拒絕 exit 1,零 mutation**。拒絕訊息**先講後果**(會拆掉本機 canonical warden、停掉它監管的所有 agent)、再給 namespaced 逃生路,最後才提授權旗標——事故向量是**自動化**,一個掃 stderr 找修法的 wrapper 應該先撞到安全選項。
- **canonical 一定要顯式 `--canonical`**;`--canonical` 與非空 `OC_NAMESPACE` **互斥**(同時給也拒絕)——別讓一個 namespaced 呼叫端以為自己拿到了「更保險」的旗標。
- **守衛的實際覆蓋範圍(別寫成「單一判定點」——`doTeardown` 有兩個入口,只有一個被守)**:
  - **CLI 入口 `teardownCmd`(`teardown.go`)= 被守的那個**。`validateTeardownTarget` 在 `resolveTeardownPaths` 推導出任何路徑**之前**跑,拒絕即 exit 1、零 mutation。這裡的目標來自**呼叫端的環境**(`OC_NAMESPACE` + argv),而呼叫端可能是一個丟了 namespace 的自動化 wrapper——**目標可能是錯的,所以必須驗**。
  - **uninstall-RPC 入口 `CommandDeps.Teardown`(`transport.go`)= 刻意不守**。它直接 `resolveTeardownPaths` + `doTeardown`,**從不經過 `validateTeardownTarget`**,而且這是對的:這條路是 warden **拆自己**,目標由「這個 warden 是照哪份 plist 起來的」決定(launchd 注入的 `OC_NAMESPACE` = 它自己的 instance),不是由外部呼叫端拼出來的。加上守衛只會讓 canonical warden 的自我卸載被自己的守衛拒絕——把正確的目標弄壞,而事故的形狀(拆錯目標)在這條路上根本不存在。
  - ⚠️ 所以本票防的是「**CLI 被餵了隱含目標**」,不是「`doTeardown` 一律要驗」。敘述別再擴張成後者。
- **守衛「有沒有被呼叫」自己也要被釘**:`TestValidateTeardownTarget_*` / `TestTeardownRefusal_*` 只驗那個函式**本身**正確,把 `teardownCmd` 裡的呼叫整行刪掉它們全綠。第二道高度是 `scanTeardownGuardSource`(`teardown_test.go`,鏡 T-5047 的 `scanHostSeamSource`):**原始碼掃描 `teardown.go`**,只看 code 行,要求 `teardownCmd` 本體確實出現 `validateTeardownTarget(env, canonicalExplicit)`、且**排在第一個 `resolveTeardownPaths(` 之前**(fail-closed 在推導任何路徑之前)。唯一 runner 是 `TestTeardownGuard_IsCalledFromTheCLIEntryPoint`——**別加第二條同體測試**。行為面另有 `TestRealMain_BareTeardownRefusesBeforeAnyHostOperation`。
- **teardown-here 兩臂**(`server/ocserverd/api_machines.go`):`s.namespace == ""` → argv 追加 `--canonical`;namespaced → 傳 `OC_NAMESPACE`、**不**帶旗標。任一臂錯 = CLI exit 1 → CONFIRM-THEN-REMOVE 永不確認 → 機器永遠留在 roster。兩臂都有測試——⚠️ **不再走 HTTP handler**(T-42a0 之後那條路被兩道 409 擋死,經 handler 驗這個契約已經不可能):現在是 `server/ocserverd/api_machines_teardown_target_t42a0_test.go` 的 **`TestTeardownHere_CoreStillSpellsItsOwnTarget`**,**直接驅動 `runWardenTeardownHere` 這個 core**,從 `runOcwarden` seam 上讀真正會交給 `exec.Cmd.Env` 的 argv+env。(舊名 `TestHandleTeardownHere_ExplicitTargetContract` 連同 `newTeardownHereServer`/`postTeardownHere` helper **已刪**,別再照著找;它同時斷言「被指名的 m-box 要被標成 removed」,那一半是缺陷不是契約。)**絕不真的 exec**:除了各測試自己 bind 的 recorder,`TestMain` 還把 package 預設的 `runOcwarden` 換成拒跑的 `refuseToExecOcwarden`——真的 `execOcwarden` 在測試 binary 裡根本沒接上。
- ⚠️ **任何驅動 `realMain(..."teardown"...)` 的測試,一律不得繞過 `newHostSeam`**(`install.go:230` 的 `var newHostSeam = realHostSeam`;`hostseam_test.go:135` 的 `TestMain` 在 `m.Run()` 之前把它整包重綁成 `fakeHostSeam`,所以測試不必、也不該自己注入什麼——`teardownCmd` 取 `newHostSeam().sys` 就自動拿到假貨,斷言讀 `hostSeamFakes`)。執行期兜底有三處 `refuseInTestBinary`:`install.go:133`(`realSysOps`)、`install.go:215`(`realHostSeam`)、`main.go:203`(`execRunner.Run`)——最後這處是 hand-assembled struct 也繞不過去的 process 咽喉點。realMain 的 launchd 目標來自**真實 `os.Getuid()` + canonical label**——假 HOME 只隔離**檔案路徑**、不隔離 launchd。2026-07-26 這個缺口在 review 期間兩度 bootout 掉本機 live warden。守衛本身不是測試的防護,seam 才是。
  - 📌 T-2257 原本自帶的 `teardownSysOps` package-var + `withFakeTeardownSys` helper **已在 rebase onto T-5047 時移除**(理由見 `namespace_test.go` 的 REBASE NOTE):它會構成第二個 `realSysOps` 非測試引用,`scanHostSeamSource` 直接拒跑整包。碼裡已無這兩個識別字,別再照著找。
- **e2e 側**(`e2e_test/lib/oc_lifecycle.sh`):`oc_assert_teardown_instance` 在**第一個 mutation 之前**驗 root/serve label/warden label/tmux socket 四軸與選定 instance 一致(canonical 也是一種顯式模式,混合軸即死);`oc_teardown_warden` 每次 subprocess 都顯式帶 `OC_NAMESPACE`。守衛的鑑別力由 `tests_guard/run.sh` 案例 18c(把 2026-07-25 事故的兩行 rm 注進 lib 副本,tripwire 必須真的響)與 18d/18e(混合軸必須在兩個 call site 各自死掉、且死在任何 bootout 之前)釘住。

## force-revive(activate 清 stopping/waking)
`activate`(wake)是 **force-revive**:清掉 member 的 `stopping_since` / `waking_since` 錨點,**不受 winding-down gate 擋**(即使 member 正 stopping / 甚至 online,wake 也回 200 不回 409)——讓「正在收」的 member 被重新拉回 online。行為釘在 conformance 套件與 `server/ocserverd/reconcile_test.go`;reconcile 對 genuine *stopped* terminal 也走 force-revive 覆蓋(`server/ocserverd/reconcile.go`)。
