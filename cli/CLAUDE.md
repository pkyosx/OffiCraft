# cli/ — Go 自更新 binary(ocagent / ocwarden)

進入 `cli/` 時 nested-load。repo-wide 憲章 + 約定見 root `CLAUDE.md`;本檔記 cli 專屬。兩個獨立 go module,各自 `go.mod`。棧:go1.26。

## 命名(root §10)
- **`cli/ocagent/`** — Plane A:agent-side SSE listener(`ocagent listen` = agent 存活心跳;`ocagent context-report` 等)。folder = `module ocagent` = binary `ocagent`。
- **`cli/ocwarden/`** — Plane B:per-machine warden executor(stateless 執行手,拿 server push 的 token spawn member)。folder = `module ocwarden` = binary `ocwarden`。
- (已拆除)`cli/ocrelease/` 與 `server/ocupdaterd/` 隨 t-dc68 退役:發佈改走 GitHub Releases(`bin/release <tag>` 打包 + `gh release create` 出貨;server 端 update_check.go/upgrade.go 直接對 GitHub API 檢查與升級,見 `server/CLAUDE.md`)。
- ⚠️ **介面契約(已對齊 ocagent/ocwarden 命名,2026-07-09 owner 定案)**:spawn 寫的 bare **`ocagent` shim 呼叫名**(boot prompt 契約:spawned agent 跑 bare `ocagent listen`)+ launchd **label `com.officraft.ocwarden`**。它們是介面名(非 folder/module/binary),改動需 **host 端協調**(shim 重寫 / warden bootout+relaunch)。
- **同機多實例 namespace(`OC_NAMESPACE`,2026-07-11 owner 定案)**:單一 env 鍵所有 per-instance host 資源——root `~/.officraft-<ns>`、label `com.officraft.ocwarden.<ns>`、tmux socket `officraft-<ns>`、agent home(ns 非空時 spawn 額外 export `OC_AGENT_HOME`);字元集鎖 `[a-z0-9-]{1,16}`,非法即拒。**空 namespace = 主實例,輸出一個 byte 都不變**(golden 測試釘死)。導出邏輯單點在 `cli/ocwarden/namespace.go`;傳播線:server oc.toml `[server].namespace` → install.sh / bootstrap-here env → warden plist stamp → spawn export。
  - ⚠️ **跨 module 手抄鏡像(T-5047)**:同一條導出在**五個地方**各寫一遍——`cli/ocwarden/namespace.go`、`cli/ocagent/config.go`(`fallbackAgentsHome`,agents-home fallback root;T-5047 二輪審查前這裡**根本沒有 namespace**,硬寫 `~/.officraft/agents`,所以一個 namespaced ocagent 失去 `OC_AGENT_HOME` 就把 SSE cursor / reply-card seen / context-report stamp 寫進**主 instance** 的 agents 目錄)、`server/ocserverd/onboarding.go`(`wardenLaunchdLabel`/`officraftRootPath`/`wardenTokfilePath`)、`bin/install.sh`(`NS_DOT`/`NS_DASH`,install 與 uninstall 各一份)、`bin/ocserver`。兩個 go module 之間沒有 import、沒有編譯器,**差一個字元的後果不是字串錯**:server 問 launchd「`com.officraft.ocwarden.lab` 在不在?」得到「不在」(因為 warden 註冊的是別的 label),於是判定本機沒有 warden,**在活的 job 上再裝一個**。五份全部對**同一張表** `bin/tests/fixtures/namespace-axes.tsv` 對質(go 側 `cli/ocwarden/namespace_mirror_test.go` / `cli/ocagent/namespace_mirror_test.go` / `onboarding_mirror_test.go`,bash 側 + charset 走 `bin/tests/namespace-mirror-guard.sh`),所以漂掉時**紅的是漂掉那一份**,而不是只說「兩份不一樣」。加 namespace 案例改表就好,不必動五份測試碼。⚠️ ocagent 那份的**負向控制**在 `TestAgentsHomeFallback_RefusesRatherThanFoldingBack`:非法 namespace 必須 derive 出空字串,**絕不准折回主 instance 的 `~/.officraft/agents`**(namespace.go 自己就寫「malformed namespace 默默折回主實例比硬錯誤糟得多」),而且它是唯一一份把 namespace **join 進路徑**的複本,charset 因此是防路徑逃逸(`../x`)而非裝飾。
- **`ocwarden install` / `teardown` 的 host seam(T-5047)**:所有真主機副作用(launchctl/檔案系統、claude/codex probe、ocagent 下載與 probe)在 production 只在 `realHostSeam()` 一處組裝,而且只能經 package 變數 `newHostSeam` 取得。`hostseam_test.go` 的 `TestMain` 在任何測試跑之前把它換成 fake,所以**凡是經由 `newHostSeam` 取得副作用的 entry point,在測試裡都拿到 fake**——新測試不必 opt-in、不必記得塞 fake,連「把待測碼裡的 guard 刪掉」也升不出去碰主機。
  - 🔴 **這個保證的邊界要講清楚,絕對形式是假的(獨立審查實證)**。舊版本檔寫「所有真主機副作用只在 `realHostSeam()` 一處組裝…兩條靜態守衛釘住這個結構」,而那兩條守衛只釘 **`realSysOps` / `realHostSeam` 兩個識別字**。審查者在 `teardownCmd` 直接寫 `sysOps{run: execRunner{…}.Run, rename: os.Rename, …}` 的 composite literal——不出現那兩個字串——三道守衛全綠、`refuseInTestBinary` 沒被呼叫,test binary 真的對本機 live warden 發出 `launchctl bootout`;測試最後才紅,訊息是「no host seam was constructed」= **事後偵測,不是事前阻止**。seam 保護的是「走過 seam 的 entry point」,不是「任何自己動手接線的碼」。
  - 🔴 **同一個形狀在 T-ff5d 又發生一次,而且證明了「補一條規則」不是修法**。`cutover.go` 帶進**第二個 seam**(`cutoverOps`——真綁定會對 canonical label 下 `launchctl bootout`、覆寫 live plist、丟出 detached 的機器轉換),而底下三層**一條都沒聽過它**:審查者把 T-5047 那個 mutant 原樣搬過來(`cutoverOps{runExit: realRunExit, spawnDetached: spawnDetachedProcess}` 寫在非測試函式裡),全綠,而且**真的從 test binary 起了行程**。`spawnDetachedProcess` 比原案更糟:setsid + Release,子行程**活得比 `go test` 久**,而它 production 的 argv 是一次真的機器轉換。⚠️ 病灶不是「少列了 cutoverOps」,是**守衛的 scope 來自一張人工列舉的清單**——清單過期時它不會紅,它會**默默縮小自己的覆蓋範圍然後保持綠**。
  - **所以現在多一層 (4):零列查詢(`scanProcessStarters`)**。用 AST 列舉**非測試碼裡每一個能生出行程的呼叫點**(`exec.Command` / `exec.CommandContext` / `syscall.Exec` / `ForkExec` / `os.StartProcess` / 手搓 `exec.Cmd{}` literal),減掉 `sanctionedProcessStarters` 這張**寫明理由**的白名單,**要求餘數為空**。新檔、新函式、新 seam 一律自動落進餘數 → TestMain 在 `m.Run()` 之前拒跑。清單過期的兩個方向都是 finding:沒列到 = 紅,列了卻已不存在(stale)= 也紅。白名單裡標 `mustRefuse` 的那幾個(`execRunner.Run`、`spawnDetachedProcess`、`realRunExit`、`runInstallerCombined`、`newSelfUpdater`)**函式本體第一行必須是 `refuseInTestBinary`**,也由 AST 驗(不是字串比對)。⚠️ **這道查詢上線第一跑就自己抓到一個**:`newSelfUpdater` 建的 `execSelf` closure 會 `syscall.Exec` **取代呼叫端的行程映像**——在 test binary 裡就是把測試行程換成 ocwarden,沒有任何守衛看得見。已補上拒絕。
  - **另一半:測試不准直接碰真函式**。`scanTestsForRealHostCalls` 掃 `_test.go` 的 AST,要求對 `realSysOps`/`realHostSeam`/`realCutoverOps`/`realRunExit`/`spawnDetachedProcess`/`runInstallerCombined` 的**直接呼叫數為 0**(用 AST 所以註解與字串字面值天然不算——這個檔自己就在正文裡寫滿這些名字)。`captureInteractiveEnv` **刻意不在名單上**:它跑的是測試自己寫進 `t.TempDir()` 的 stub shell,那就是待測行為;這條線畫的是「會不會影響這台機器的 warden」,不是「會不會 fork」。
  - 🔴 **守衛自己有「已知壞例」正控**(`TestHostSeamGuards_RejectHandAssembledSeams` / `TestProcessStarterQuery_*` / `TestRealHostFunctionsAreNeverCalledFromTests`):把整包非測試原始碼 stage 到 temp dir,**先斷言乾淨副本零違規**(否則每個 mutant case 都會因為別的原因「通過」),再逐一寫入**壞檔**(T-5047 的 sysOps mutant、T-ff5d 的 cutoverOps mutant、新檔裡的 `exec.Command`、手搓 `exec.Cmd{}`、`syscall.Exec`、以及「`spawnDetachedProcess` 的 refusal 被刪掉」),要求掃描**點名它**。⚠️ **這幾條不是上面說的「第二條 reporting wrapper」,別照那條規則刪掉**:`TestHostSeam_StructureIsReported` 重跑的是 TestMain 已經過的**同一棵樹**(迴圈體恆不執行),這幾條餵的是**不同的輸入**,規則被拿掉時它們會紅——親驗四個 mutant 全紅。壞例寫在**測試裡當 fixture**,不寫在註解裡:註解裡的 mutant 保護不了任何東西,這個 repo 已經證明過。
  - **現在真正的保護範圍(四層,由弱到強)**:(1) `scanHostSeamSource` 的識別字守衛——`realSysOps()` 全非測試碼只准出現一次、任何 entry point 不得直接呼叫 `realHostSeam()`;(2) **釘結構而非釘名字**——`sysOps{`、`execRunner{`、`cutoverOps{` 三個 composite literal 在非測試碼各只准出現一次(分別在 `realSysOps`、`main.go` 的 `var newCmdRunner`、`realCutoverOps`),所以上面那個 hand-assembled mutant 現在在 `TestMain`、`m.Run()` 之前就被拒(親驗:整包零測試執行、`launchctl` PATH shim 零命中);(3) **process 咽喉點**——`execRunner.Run`(`main.go`)、以及 `cutover.go` 的 `spawnDetachedProcess` / `realRunExit` / `runInstallerCombined`,body 第一行就是 `refuseInTestBinary`。這一層是 hand-assembled struct **繞不過去**的:struct 是誰組的都無所謂,子行程還是得從那裡起。親驗(T-5047):把 (2) 整條刪掉再跑同一個 mutant,`FATAL: execRunner.Run(launchctl) was reached from a test binary` + `os.Exit(1)`,shim log 空的(launchctl 行程從未被建立)、live warden 還在跑——**擋在碰到主機之前**。親驗(T-ff5d,同法重做一次):把 (2) 的 cutoverOps 規則**與** (4) 一起關掉、把 cutoverOps mutant 放進真的 package、讓它去 spawn 一支會寫檔的 tripwire 腳本 → `FATAL: spawnDetachedProcess(...) was reached from a test binary`,而 **tripwire 檔不存在**(行程從未被建立)。(4) 見下一條的**零列查詢**——它不列舉名字,所以下一個新 seam 不必靠人記得。
  - 靜態守衛的比對一律只掃 code 行(`countCodeOccurrences` / `countRealSysOpsCalls` 剝註解),否則守衛會被自己的文件滿足——同一個坑 `bin/tests/namespace-mirror-guard.sh` 的 `code_only()` 早已踩過,server 側 `api_machines_childenv_wiring_test.go` 也才剛因此被抓到一條恆真斷言。
  - `TestHostSeam_StructureIsReported` 是**唯一一條** reporting wrapper(不是 enforcement、不是覆蓋率:`TestMain` 已先跑同一個 scan 並 `os.Exit(1)`,所以它的迴圈體恆不執行)。⚠️ **不要再加第二條**:原本這裡有兩條函式本體逐字相同的守護測試,審查者把兩者迴圈體換成 `panic` 跑整包,照樣 `ok` + 兩條 `--- PASS`。新性質加進 `scanHostSeamSource`,不要加新的 no-op 測試。

## 自更新 binary
- **改 Go → fresh build 驗證，binary 永不 commit**(root §13,唯一例外是 TCC 身分錨點 `dist/officraft/officraft`,見該節)。CI Go gate 跑 gofmt、vet、build 與 test；發佈的 binary 由 release 流程 fresh build。
- **self-update**:content-hash swap oracle + 防自殺 verify-before-swap(swap 前先驗新 binary 可跑)。install `env -u OC_ID`(防 shell OC_ID 污染)+ 餵回同 tokfile。ocwarden 換掉自己後 **exec-in-place**(`syscall.Exec` 同 PID、同 argv/env 原地換新,exec 失敗才 fallback exit(0))——**絕不賭 launchd KeepAlive 重拉**:實測 macOS gui-domain LaunchAgent 對 exit 0 的 warden 不重拉(job 停在 not running 直到人工 kickstart),exit-and-relaunch 舊路會讓每次 self-update 殺死該機看門狗。
- **warden restart 不斷 agent online**:ocagent 是獨立進程、持自己的 SSE,warden 重啟不影響。
- **完全不做 code signing(T-0398,owner 2026-07-31 拍板「全部拿掉,連手動簽章的逃生門一起刪」)**:repo 裡已**沒有**簽章機制——`bin/codesign-artifact`、`bin/setup-codesign-cert`、`bin/build-release`、`bin/release publish --sign`、`OC_CODESIGN_*` env knob 全部刪除。所以 fleet self-update 換進去的 binary 一律是素 `go build` 的 adhoc 產物。這個 loop 也**不再 log 簽章身分**:舊的 `signatureOf` seam → `codesignIdentity`(shell out `/usr/bin/codesign -dv`,只記錄不驗證)一併移除,`hostseam_test.go` 的兩份名單也隨之少一筆。防自殺閘門只有 exec probe + content hash,就這兩道。
  - ⚠️ **不要把這條跟 TCC 身分錨點搞混**:錨點(`cli/officraft` / `dist/officraft/officraft`)是**用 bytes 認身分**的,從來不依賴簽章憑證,owner 核可保留(見下方 plist 那條紅字)。也**不要寫「簽章讓 TCC 授權跨版延續」或「已證明 self-signed 對 TCC 無用」**——兩個方向都是過度宣稱;現況只是**高度懷疑無效、沒有 100% 結論**(owner 2026-07-26)。刪除的理由是作業面的:它卡測試又卡發佈。詳見 `docs/dev/README.md`〈發佈簽章 —— 已整個移除〉。
- **event-driven kick(T-c93d)+ `update` 動詞(T-5f01)**:self-update 的 15m 輪詢只是 backstop;`updater.Kick()`(buffered-1 去抖)有兩個 producer——SSE transport 每次成功 (re)connect(server 換版必踢斷所有 stream)、與 server push 的 **`update` warden-command 動詞**(owner 控制台一鍵升級 `POST /api/machines/{id}/upgrade` → `CommandDeps.Update` seam → 同一個 Kick)。update 動詞無 receipt(swap 自己會經 telemetry `self_update` 宣告);舊版 warden 對不認得的動詞 = log+skip 安全忽略(transport_test 釘死)。
- **心跳 warden_shape(T-ff5d)**:30s telemetry payload 另帶 `warden_shape`(`anchor`/`legacy`/`unknown`),值由 **`cutover.go` 的同一支 `detectShape`** 從**父行程 exe** 讀出(不是「磁碟上有沒有 anchor 檔」——機器可以有檔卻仍被 legacy plist 起來,那正是本遷移要修的狀態),經 `newCutoverOps` seam 取得,所以測試 binary 摸不到真 `ps`。每輪重讀不快取:cutover 會 bootout 再由 launchd 重起,快取的行程不是回報的那個。⚠️ **省略 ≠ `unknown`**:省略 = 這台的 warden build 還沒收到本次發佈,`unknown` = 新 build 跑了但讀不到父行程,server 兩者互不推導(`warden_shape` 只有非空才進 payload,鏡 `machine`/`binaries`/`claude` 的條件寫法;anchorPath 解不出來時回報 `unknown` 而非空字串——空 anchorPath 會讓 `detectShape` 把**每一台** launchd 起的 warden 都判成 `legacy`)。同批 `binaries` 多一支 `officraft`(TCC 錨點,self-update 刻意永不換它,所以只有指紋看得出機器跑的是哪一份錨點);anchorPath 一律由 `resolvePaths` 供給,不在 fingerprint.go 再推導一次。
- **心跳 binaries 指紋(T-5f01)**:30s telemetry payload 順帶 `binaries: {ocwarden, ocagent}`——live binary 的 sha256 12-hex 前綴(`fingerprint.go`,stat (size,mtime) cache 免每 30s 重讀 multi-MB)。server 拿它比對自己 embed 的 bindist hash 算機器表 `bin_status`(current/stale/缺=unknown)。**刻意 content-hash、不埋版號**(同 self-update swap oracle 理由:埋 sha 會造成 update 迴圈)。

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

### session effort 走 telemetry,取 payload 的 LIVE 值(T-e12c;同檔 `telemetryBody.Effort`)
effort 原本**只**被畫進 status line 字串,從不進任何 POST body ⇒ 監控台每個 claude session 的
effort **永遠是空字串**。而空字串正是「這個 session 還沒回報」的誠實形狀 —— 故障與正常同形,
所以躺到 owner 自己看畫面才發現(server 的解析/儲存/回吐、凍結 spec 的 `effort` 欄、前端的
badge 全都早就備好了;codex sidecar 一直有送,是這條鏈可用的活證據)。
- 🔴 **來源是 statusLine payload 的 `effort.level`,不是 `OC_EFFORT`,而且沒有 fallback。**
  payload 帶的是**當下**的等級(會跟著 session 中途 `/effort` 改變),`OC_EFFORT` 只是 warden
  spawn 注入的**啟動意圖**。owner 2026-07-31 定調:成員面板與監控台一律顯示回報回來的狀態、
  不得顯示設定值——退回 `OC_EFFORT` 等於讓一個中途降到 low 的 session 繼續顯示啟動時的 high,
  而且從畫面上分辨不出來。模型不支援 effort 參數時 Claude Code **整塊省略**,所以讀不到就是
  誠實的「這個 session 沒有 effort」。
- 送的是 `effortValue`/`effortLevel`(**verbatim**),不是 `effortLabel`——後者把 `medium` 縮成
  `med` 只為版面,讓它上線等於在監控台顯示一個 `/effort` 與啟動旗標都叫不出來的值。
- **只掛 `/api/monitoring/telemetry`**;`AgentContextIngestDTO` 沒宣告 effort 且
  `additionalProperties:false`,塞進 context POST 會 422 掉整個 gauge 回報並拖上面那條退避。
- 空值 `omitempty` 省略,**絕不送空字串**——那會把「沒回報」變成「回報了一個空的」。
- 釘住它的是 `telemetry_wire_test.go` 的 `TestContextReportSendsSessionEffort`(在/不在/不縮寫)
  與 server 側 `TestGetMonitoring_SessionEffortRoundTrips`(ingest→GET 往返,且未回報者不得
  退回名冊設定值)。⚠️ 這條路先前**零測試**,所以它從未送過這個欄位時沒有任何東西會紅。

### session model 同一條路,同一個 bug,晚一批修(`telemetryBody.Model`)
`model` 與 `effort` 是同一份 statusLine payload 裡的兩個欄位,**上行契約相同**(回報狀態、量不到就
省略),而且**犯的是同一個錯**——⚠️ 但**落地之後不對稱**:server 把 model 落進持久欄 `actual_model`、
effort 只留在 in-memory entry(**沒有 `actual_effort`**),所以別從一個推論另一個的儲存行為。
`modelEffortSegment` 從第一天就讀它來畫狀態列那個 `◆ Opus 4.5`,**卻從來沒有進過任何 POST body**。
差別在後果更重——監控台的模型欄因此只能退回 owner 的**設定值**,而**外包 worker 在那條讀取路徑上
根本沒有設定值可退**,所以每個 worker 的模型欄結構上永遠是空的。
- 🔴 **送 `model.id`,不是 `model.display_name`**(`modelValue`/`modelID`)。狀態列畫的是
  display_name,但上線的必須是 id:(a) `seeds/boot_sequence.md` 早就教成員「填 Claude Code
  提供的**真實 model id**,不要猜值」,用 display_name 會讓正職自報與這支自報變成兩種詞彙;
  (b) **只有 id 帶 `[1m]`**——display_name 對 1M 與標準版都寫「Opus 4.5」,送它等於把兩種
  不同的 session 併成同一個字串,而控制台現在就在顯示這個區別。
- 空值 `omitempty` 省略,**絕不送空字串**(與 effort 同理)。
- **只掛 `/api/monitoring/telemetry`**;`AgentContextIngestDTO` 沒宣告 model 且
  `additionalProperties:false`,塞進 context POST 會 422 掉整個 gauge 回報。
- **codex 那半在 `ocwarden/codex_session.go`**:那條 runtime 沒有 Claude Code 狀態列,sidecar
  是唯一知道 session 跑什麼模型的東西,所以它的 telemetry post 也帶 `model`(`s.model` 為空
  代表 OffiCraft 沒設、機器的 Codex 預設生效 = 真的不知道名字 ⇒ 省略,不送 `""`)。
- 釘住它的是 `TestContextReportSendsSessionModel`(id/1M 標記/缺 model/只有 display_name 四例)、
  `TestReportTokenUsageSendsSessionModel`(codex),與 server 側
  `TestGetMonitoring_SessionModelRoundTrips` + `TestGetMonitoring_ReportedModelSurvivesATelemetryWipe`。
## `dispatched ... OK` 只在收據真的送達時才印(T-b36a step 3;`command.go` / `transport.go`)
`handlePayload` 的 `dispatched %s OK` 原本只證明「dispatchCommand 沒回 error」,卻被寫成對外的
成功宣告——而 start/stop 的 `deps.report(...)` 是**裸述句**,收據 POST 失敗(傳輸故障 / 非 2xx)
的回傳值直接被丟掉。**實測**(2026-07-28,一台機器 8 天的 `ocwarden.out.log`):5,805 行
`dispatched ... OK`,`receipt` / `command_result` / `500` **零命中**——一個收據被回 500 的 op,
log 讀起來跟完美執行一模一樣。與 step 1 的 context-report 戳記同一個 bug 家族:**失敗也蓋成功戳**。
- 修法:start / stop / worker_stop 接住 `deps.report` 的回傳,包成 `errReceiptUndelivered`
  由 dispatchCommand 回出;`handlePayload` 用 `errors.Is` 分岔,印
  `%s EXECUTED but its receipt did not reach the server` 而**不**印 OK 那行。
- 🔴 **排序是刻意的:op 自己的失敗永遠壓過收據失敗**(spawn 被拒 → 回 spawn 的 error;stop
  incomplete → 回 stop 的 error)。把兩者摻在一起會把「claude 沒起來」這個可行動的事實埋進
  傳輸故障底下,而 `errReceiptUndelivered` 也會變成兩種意思。`command_receipt_test.go` 兩個
  排序測試釘死這件事。
- 🔴 **這條不是本票的 owner 訊號**,別這樣宣稱。實測 `ocwarden.err.log` / `out.log` **零 reader**
  (現場 `lsof` 唯一持有者是 warden 自己的寫端 fd;repo 全域 grep 到的命中全是寫端,或 server 把
  「去看那個檔」塞進 `last_op_reason` 的提示字串)。真正到得了 owner 的訊號在 server 端
  (`server/ocserverd/receipt_watch.go` 的 `receipt_missing`)。這一半**移除的是反向訊號**——
  一行主動宣稱相反事實的假成功。
- **UNINSTALL 不變**:它的收據本來就是硬條件(`reportErr != nil` → 不 self-exit),沒有被改道
  進新的 error class(`TestDispatchCommand_UninstallReceiptContractUnchanged` 釘住)。

## listen 自救(fail-closed,zombie 防線 B 的 client 半邊)
`ocagent listen` 兩道自救原本 fail-open(probe 失敗照樣活 = 殭屍永生),已改 fail-closed 帶寬限(`listen.go` 常數 + `listen_run.go` foldProbe/foldRefusal):
- **tmux session probe 三態**:alive / gone(tmux 明確答「無此 session」→ 2 連 miss 即自殺,不變)/ unknown(tmux 解析不到、spawn fault、timeout → 不再永遠當 alive:連續 `probeUnknownMin`(8)次 ∧ 滿 `probeUnknownGrace`(10min)才 self-exit;unknown 會重置 gone debounce,絕不瞬殺健康 listener)。
- **server 409 拒連 fail-closed**:`/api/events` pre-stream 409(server 殭屍 stop gate 或 dual-SSE)是權威「你不該在線上」;**連續** `sseRefusalMin`(4)次 ∧ 跨滿 `sseRefusalGrace`(120s,鏡 stop_grace)→ 自我了斷(`suicide` 殺自己 tmux session,headless 則純退出)。任何其他結果(連上、網路錯、5xx、server 短暫掛掉)都**重置**計數——絕不因 server 抖動誤殺健康 agent。
warden `spawn.go` 寫一支 bare `ocagent` shim script → exec 真正的 golang `ocagent` binary。binary 解析順序:`OC_AGENT_BIN` 覆蓋 → home-install sibling `~/.officraft/warden/ocagent` → fallback repoRoot-relative `<repoRoot>/cli/ocagent/ocagent`(dev layout)。`resolveOcAgentBin`(`transport.go`)擁有此邏輯。⚠️ 改 spawn / 路徑前先讀 `spawn_test.go` 斷言(shim 內容精確比對)。

## trash 清理(T-684c;`cli/ocwarden/trash.go`)
「agent `mv`、warden `rm`」的 **rm 半邊**。**基石不是「mv 比 rm 安全」**(實測相對/絕對 × mv/rm 四種組合鑑別力為零)——基石是**刪除的執行者從 agent 換成 warden**:Claude Code harness 有一道**任何 settings/permission 都豁免不掉**的 dangerous-rm 確認提示(`Dangerous rm operation on working directory or its ancestor` + Yes/No),headless agent 前面沒有人按 → **無聲卡死**。warden 是 launchd 直起的獨立 Go 常駐程式、鏈上沒有 claude,它刪檔不在那道檢查的管轄內。seeds(`seeds/system_interaction.md` §10.5 / `seeds/worker_context.md` §6 / 前端逐字副本 `frontend/src/api/seeds.ts` / server 的 task-close nudge `sse_bands.go`)因此改教 agent:**暫存 `mv` 進 `<workdir>/trash/`,不要自己 `rm`**。

- **兩個掛點**:spawn(`spawn.go` `SpawnDeps.start`,MkdirAll workdir 之後,`PurgeTrash` seam)與 teardown(`kill.go` `stop()` 尾端,`sweepSeams.purgeTrash` seam);兩個 seam 都在 `transport.go` `buildCommandDeps` 綁定真實 `purgeTrash`。兩者皆 **nil-skipped、best-effort**:清不掉絕不 fail spawn、也絕不改 stop 的死亡判定。
- **fail-closed**:`purgeTrash(root, workdir, logf)` 只刪 `<workdir>/trash`。任一守衛不過 → **拒絕不動,並在 warden stderr(`<logDir>/ocwarden.err.log`)留 `REFUSED` 行**,不默默跳過。trash 不存在 = 正常狀態,安靜 no-op、不當異常。
  - **字串層**:root/workdir 任一為空(G1)、非絕對路徑(G2)、非 `filepath.Clean` 正規形(G3,擋 agent id = `../..`)、`Dir(workdir) != root`(G4,直屬子目錄的**精確**判定,不是 HasPrefix——`/x/agentsEVIL` 會被前綴法放行;同時擋掉 workdir==root 與孫層)。
  - **檔案系統層**:`trash` 是 symlink(G5,`Lstat` 不是 `Stat`)、`trash` 不是目錄(G6)、**`EvalSymlinks(workdir) != EvalSymlinks(root)/Base(workdir)`(G7)**、trash 解析後跑出自己的 workdir(G8,G5 的 TOCTOU 備援)。
  - ⚠️ G7 刻意**比「解析後仍是 root 直屬子目錄」更嚴**:workdir symlink 指向**鄰居 agent** 時,鄰居本身也是直屬子目錄,只做 `Dir()==root` 會放行並誤刪鄰居的 trash(review 建議的修法版本就漏了這條,實測紅)。要求 basename 撐過解析 = 「這個 id 必須真的擁有這個目錄」,才是我們實際依賴的性質。
  - 🔴 **G7 是本模組唯一真正的 containment 守衛,別把它跟 G4 混為一談。** G4 只證明**路徑字串**長得對;**workdir 自己是 symlink** 時 G4 照樣成立,但檔案系統上它可以指到任何地方。這個洞在 review 被抓到(第一版把 `EvalSymlinks(trash)` vs `EvalSymlinks(workdir)/trash` 誤當成 ancestor 檢查——那在 G5 存在時是**恆真式**,把它的拒絕分支換成 `panic` 跑整包測試**一次都不會觸發**)。**root 必須被帶進比較,否則等於什麼都沒檢查。** 兩邊都過 `EvalSymlinks` 才不會誤殺 macOS `/var → /private/var` 這種合法祖先 symlink。
- 測試在 `trash_test.go`,**兩個方向都釘**:trash 內的東西會被清掉;trash 以外(workdir 的 `tmp/`、`keep.txt`、`.oc-token`、鄰居 agent 的 workdir 與其 trash、agents root 本身、被 symlink 指到的 workdir 外資料、以及 **workdir 自己是 symlink 指向 root 外 / 指向鄰居 agent**)一個都不准動——每個拒絕案例都額外斷言「受害者檔案還在」當**負向控制組**,只斷言回傳 false 不算數。⚠️ 最後那兩條(workdir symlink)是 G7 的專屬鑑別測試,**刪了 G7 就靠它們變紅**——第一版沒有它們,所以加一整道新守衛時既有測試零反應。

## 外包 worker 臨時 session 形態(M3 Phase 6;`cli/ocwarden/worker.go`)
owner 拍板「乾淨新建」:warden 長出**臨時 session** 形態伺候外包 worker(ow- id),**不借道成員通道、不污染成員生命週期**。與成員共享的只有純機制(Phase 2 spawn executor + Phase 3 robust-stop ladder + §7 PUSH band transport);不同的全在 worker.go:
- **A案 P5b 命名統一:外包走成員動詞**——worker spawn 就是 `start`(member_id=ow-id、role="outsource-worker"),session = `member-<ow-id>`、workdir 在 agents/;kill 就是 `stop` {member_id}。
- **過渡 guard(舊 `worker-<ow-id>` 殘留不可永遠殺不掉)**:`stop` 帶 ow- 前綴 member_id 時額外掃殺派生的 legacy `worker-<id>` session(EXACT、絕不 pattern);legacy 動詞 `worker_stop` 仍收(舊 server 過渡 alias,走同一 Stop closure、workdir 依 prefix 解析 workers/);`worker_start` 已退役(unknown-rpc:log+skip)。
- **kill ladder 外門擴為「member- 或 worker- 才准」**(kill.go stop();其他 session 一律拒)。
- **無 command_result 回報**:worker 無 member row,fold-back 通道不適用;喚醒成敗由 server 從 worker 自己的 get_my_task 領任務觀察。

## deploy
唯一安裝入口是 **`ocwarden install`**(Go,`cli/ocwarden/install.go`;flip 時期的 bash `bin/warden-install` 已退役刪除)。

🔴 **plist 起的不是 ocwarden,是 TCC 身分錨點 `officraft`(T-5831)**:launchd 的 job leader 是整棵樹的 TCC responsible process,而 adhoc 簽章的 binary 是用 bytes 的雜湊被認出來的——plist 指向會被 self-update 抽換的 ocwarden 時,每更新一次就作廢一次全機授權(症狀是**卡住、無 log**,不是被拒絕)。錨點只 fork 隔壁的 ocwarden(帶 `run`)、轉發停止訊號、用 child 的結束狀態當自己的;**裝過就永不覆寫**(連相同 bytes 也不行,重寫會換 inode)。它同時被 embed 進 ocwarden(`anchor_embed.go`,`bin/build-bindist` staging 進 `anchordist/`),因為控制台的一鍵安裝只下載 ocwarden 一支——embed、出貨、`dist/officraft/` 三份是**同一次 build 的同一份 bytes**,三份不同就是三個身分。
install 把發佈流程 fresh build 的 binary 安到 `~/.officraft/warden/`(home,per-machine)並 render 真實 plist;plist template 在 `cli/ocwarden/deploy/`(REFERENCE,實際 plist 由 install 於 runtime 寫)。⚠️ `cli/ocwarden/CUTOVER.md` 是 **2024 年 python→golang 的歷史 runbook**,跟下面這條 anchor 遷移無關(它那句「No plist backup is kept」講的是退休 `com.officraft.warden`/`com.officraft.telemetry` 兩份 python 期 plist,別拿來當本節的依據)。

### legacy→anchor 自動遷移(T-ff5d;`cutover.go`)— 面向「要處理一台使用者機器」的人

**為什麼存在**:anchor shape 出貨了,但 self-update 只換 **binary**,不動 plist。所以 anchor 之前裝的機器會**永遠**留在 legacy plist(`ProgramArguments = […/warden/ocwarden, run]`),而那正是「每次 self-update 都作廢一次全機 TCC 授權」的形狀(launchd 的 job leader 就是那個被抽換的檔)。OffiCraft 有外部使用者,「請大家重跑一次 installer」不是遷移計畫。

**這台機器上會多出什麼**(全部在 `~/.officraft/warden/`,namespaced 實例則是 `~/.officraft-<ns>/warden/`):

| 路徑 | 是什麼 | 可不可以手動刪 |
|---|---|---|
| `officraft` | TCC 身分錨點,新的 launchd job leader。**裝過就永不覆寫**(連相同 bytes 也不行——重寫換 inode = 換身分 = 掉授權)。**沒有的話,轉換會先 stage/probe 再升格出一份**(見下方 stage→probe→promote 節)⚠️ **轉換的拒絕路徑也可能留下它**:物化排在取鎖之前,所以鎖被佔或 spawn 失敗時,anchor 已經在了而轉換沒發生——無害(下次啟動直接走既有 anchor),但別把「有這個檔」讀成「轉換成功過」 | ❌ 刪了下次 install 會重放一份**新**的,等於換身分、重問授權 |
| `officraft.probe` | 上面那份的**暫存副本**,只在 stage→probe→promote 中間活著,**每條退出路徑都會刪掉** | ✅ 安全(機器在物化中途被砍才會留下;下次啟動會覆寫它) |
| `cutover.lock` | O_EXCL 鎖,只防兩個 warden 同時轉。轉完就刪;超過 15 分鐘的視為屍體自動清掉 | ✅ 安全(機器在轉換中途被砍才會留下) |
| `cutover.failed` | **哨兵:這台試過、而且回滾了**。存在 = 這台**永遠不再嘗試遷移** | ⚠️ 見下 |
| `log/cutover.log` | detached 轉換行程的完整 stdout+stderr(installer 的逐步敘述都在裡面) | ✅ 純 log |
| `~/Library/LaunchAgents/com.officraft.ocwarden.plist.prev` | 轉換前那份 plist 的**唯一**副本(`writePlist` 是無條件覆寫、自己不留底) | ⚠️ 這是手動回滾唯一的依據,救援結束前別刪 |

**怎麼從外面看出一台轉了沒**:
- **控制台(不用登入那台)**:機器表的 `warden_shape` 欄——`anchor` = 轉好了、`legacy` = 還沒、`unknown` = 新 build 跑了但讀不到父行程。**空白/null 不是「沒轉」**,是「這台的 warden build 還沒收到這次發佈,根本不會回報」。這欄每 30s 隨心跳更新。
- **在那台機器上**:`launchctl print gui/$(id -u)/com.officraft.ocwarden | grep -A3 arguments` —— 印出 `…/warden/officraft` = anchor;印出 `…/warden/ocwarden run` = legacy。(判準刻意是 **launchd 實際在跑什麼**,不是「磁碟上有沒有 officraft 這個檔」——機器可以有檔卻仍被 legacy plist 起著,那正是要修的狀態。)
- 轉換發生過就一定有 `log/cutover.log`;什麼都沒有 = 這台從沒進過這條路。

**它做了什麼**(給要判讀 log 的人,不是實作導覽):`ocwarden run` 啟動時看一次自己的**父行程**。是 legacy 才動作,而且是丟出一個 **detached 孫行程**(`ocwarden cutover-anchor`,setsid 自成 session)後**立刻返回**——因為轉換第一件事就是把自己這個 launchd job bootout 掉,不脫離就會連自己一起死。孫行程備份 plist → 跑既有的 `ocwarden install --force`(**零新安裝邏輯**:部署 anchor、render 新 plist、bootout、bootstrap、健康驗證)。

#### 🔴 preflight 曾經把**它自己要建立的東西**當前置條件(v0.5.55 出貨即失效,已修)
第一版的 gate 順序是 shape → sentinel → **`anchorPreflight`** → lock,而 `anchorPreflight` 要求 `warden/officraft` **已經**在磁碟上可執行。**唯一**會放那個檔的是 `ocwarden install`(`copyAnchorIfAbsent`)——也就是這個 gate 擋在前面的那個東西。於是:

- **anchor shape 之前裝的機器** = legacy plist **且沒有 anchor 檔**;self-update 只換 ocwarden/ocagent(`selfupdate.go` 從不碰 plist、也從不部署 anchor),那個檔**不會自己出現**。
- ⇒ gate **恆偽**,轉換**一次都不會啟動**。而**被擋掉的正是這個遷移唯一要救的母群體**。
- 現場(fleet 三台跑 T-ff5d build 的機器,實查其中一台的 `log/ocwarden.err/out.log`):每次啟動都印 `anchor cutover: skipped — anchor preflight: cannot execute …/warden/officraft: no such file or directory`,而且**沒有** `cutover.lock`、**沒有** `cutover.failed`、**沒有** `log/cutover.log` —— 孫行程從未被生出來。心跳照實回報 `legacy`,控制台照實顯示 LEGACY;**回報是誠實的,失效的是動作**。這也是為什麼「等它自己收斂」是錯的建議:那個 skip 是決定性的,每次啟動都會再跳過一次。

**修法**:gate 中間插一步 `ensureAnchorPresent`(順序變成 shape → sentinel → **anchor-present** → preflight → lock),形狀是 **stage → probe → promote**:

1. bytes 寫到**另一個路徑** `officraft.probe`(`anchorProbeSuffix`);
2. 在那裡 chmod、在那裡 preflight;
3. **答對了才升格**成 `officraft`,而升格用的是 `os.Link`(**create-if-absent**,目標存在就 `EEXIST`,不是覆寫);
4. `officraft.probe` **每一條退出路徑都刪掉**(含成功路徑)。

⚠️ **`anchorPath` 從頭到尾沒有被寫過、沒有被截斷過、沒有被取代過**——它要嘛不存在,要嘛裝著一份「已經自己通過 preflight」的副本。這不是靠小心記帳達成的,是形狀本身,所以下面幾件事**結構性**消失:

- **never-replace**:升格是 create-if-absent,**不管先前那個 stat 說了什麼**。⚠️ 這條刻意**不**依賴開頭那個 `modTime` 快速路徑——`modTime != nil` 會把 EACCES/EIO/dangling symlink 一律讀成「不存在」,而 home 安裝下 `anchorSrc == anchorPath`,於是會把機器自己的 anchor 原樣寫回去:**bytes 一模一樣、inode 換了、TCC 身分換了**。快速路徑現在純粹是省事,拿掉它 never-replace 照樣成立(親驗:刪掉那三行,相關測試全綠)。
- **半截檔磚化**:ENOSPC/EIO 截斷的是 `.probe`。若截斷落在 `anchorPath`,下次開機 `modTime` 會成功 → 短路 → preflight 對著壞檔失敗 → 跳過,而 `copyAnchorIfAbsent` 只保留不取代 ⇒ **那份壞檔永久變成這台的身分**,而且外觀與「已經轉好了」無法區分。
- **chmod 失敗磚化**:同上,不可執行的是 `.probe`。
- **「這一輪是不是我建的」記帳**:沒有 `created` bool、沒有條件式清理,所以沒有記錯的餘地。

**任何一步失敗,機器與進來時一模一樣。**

- **來源優先序照抄 `copyAnchorIfAbsent`**:sibling `anchorSrc` → 本 binary 內嵌的那份(`anchor_embed.go`)。⚠️ home 安裝下 `anchorSrc` **就是** `anchorPath`,所以實際落地的**一定是內嵌那份**——而 embed / 出貨 / `dist/officraft/` 三份是同一次 build 的同一份 bytes,換來源就是換身分。
- **兩邊都拿不到 anchor(無 sibling、無內嵌)= 照樣拒絕轉換**。把母群體救回來不等於「無條件轉」。
- **最終 gate 仍然是對 `anchorPath` 做 preflight**,不是對 `.probe`——重要的是**launchd 真正會起的那個檔**答對。已經有 anchor 的機器只會發生這一次 probe。

**🔴 為什麼 preflight 留在 cutover 這側,而不是搬進 `runInstall`**(這條**不是**順序問題——preflight 大可放在 install 的 anchor deploy 之後、仍然早於任何破壞性步驟):**理由是失敗的後果**。放進 `runInstall`,一次 preflight 失敗 = install 非零退出 = `runCutover` 判定轉換失敗 ⇒ 回滾**並寫下 `cutover.failed`** ⇒ **這台永遠不再嘗試**。一台只是這次開機剛好被 Gatekeeper 隔離的機器,會因此被永久排除在遷移之外。留在這一側,同樣的失敗只是一次安靜的 skip,重試機會全部保留——那才是對一個通常是暫時性的狀況該有的反應。

**🔴 這個形狀的前提:anchor 對 argv[0] 不敏感**(`.probe` 這個檔名下也必須 exit 2,否則等於「驗的是 A、部署的是 B」)。`cli/officraft` 的 `realMain` 只看 `len(args)`,但這種東西日後很容易被「順手」改掉而全樹無人察覺——所以 `TestAnchorPreflightAgreesWithTheRealAnchorBinary` 直接**拿真 binary 複製成 `officraft.probe` 再驗一次**。實測三種檔名(`officraft` / `officraft.probe` / 完全不相干的名字)皆 exit 2。

**內嵌 anchor 非空是這個修法的前提,已實測坐實(非讀碼推論)**:把 `dist/officraft/officraft`(1,742,642 bytes、`sha256 710a2525…`,= `dist/officraft/binary.sha256`)當 needle,在**實際出貨、fleet 正在跑的** ocwarden(`sha256 7bbd5491…`)裡做完整 byte 包含搜尋 → **命中於 offset 2953472**。所以 gate 修好之後 `install --force` 確實拿得到 anchor;若內嵌是空的,gate 修了也只會在下一步失敗,而**外觀與修好一模一樣**。

⚠️ **寫測試的人注意:`anchordist/` 是 gitignored,而 `bin/ci.sh` 在跑 `go test` 之前會 stage embed assets** ⇒ **CI 底下 `embeddedAnchor()` 是非空的**。所以「這個測試需要空的內嵌 anchor」**絕不能靠不去 stage 它**——那種測試在乾淨 checkout 綠、在 CI 紅、而且**任何跑過一次 `ci.sh` 的開發機從此都紅,`git status` 還什麼都不顯示**。兩個方向都要自己綁(`withEmbeddedAnchor` / `withoutEmbeddedAnchor`)。這條已經真的發生過一次。

**bootout→bootstrap 之間有一段真空**:那幾秒該機**沒有 warden**(server 看到它 offline)。**agent 不受影響**——`ocagent` 是獨立行程、各自持有自己的 SSE,warden 重啟不斷線。install 的健康驗證會等到新 job 真的穩定(看到 pid 後再連續 6 秒)才算數。

**失敗一律回到「舊 shape、warden 活著」**。任何 non-zero 的 install 結束都會把 `.prev` 放回去、重新 bootstrap、再驗一次。**只有回滾自己也失敗**才 exit 非零——那是唯一需要人介入的狀態。

**`cutover.failed` 要講白**:一台回滾過的機器會留下它,而它的意思是**這台永遠不會再自動嘗試遷移**——不是「等下次」、不是「隔天重試」,是**永久**,直到有人手動刪掉那個檔。這是刻意的:沒有它,一台會失敗的機器每次開機都重跑同一個失敗,無限迴圈。所以**要重試就是 `rm ~/.officraft/warden/cutover.failed` 然後重啟 warden**(`launchctl kickstart -k gui/$(id -u)/com.officraft.ocwarden`),而且重試之前先讀 `log/cutover.log` 弄清楚上次為什麼退回來。

**手動回滾(自動回滾也失敗時)**:
```sh
launchctl bootout gui/$(id -u)/com.officraft.ocwarden          # 可能已經不在,忽略錯誤
cp ~/Library/LaunchAgents/com.officraft.ocwarden.plist{.prev,} # 唯一的舊 shape 副本
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.officraft.ocwarden.plist
launchctl print gui/$(id -u)/com.officraft.ocwarden | head     # 確認真的起來了
```
留著 `cutover.failed`(否則下次啟動又會再轉一次)。

**10 分鐘的 install 預算(`cutoverInstallBudget`)不是安全邊際,是正確性**:短預算會把「慢的機器」變成「永久拒絕的機器」——installer 被砍讀起來就是 install 失敗,於是回滾**並且**寫下那個永不重試的哨兵。數字是從 install 自己的有界步驟**推導**的,不是拍的:claude resolve ≤40s + codex resolve ≤40s + ocagent 下載 ≤60s + bootout poll ≤5s + 健康驗證 ≤36s ≈ **181s**,取約 3 倍餘裕。超估的代價是一個閒置的 detached 行程,低估的代價是一台機器,永久。

**claude 路徑鏈(OC_CLAUDE_BIN stamp)**:launchd warden 的 minimal PATH 找不到 version-manager(asdf/nvm/volta)的 claude → runtime `resolveClaudeBin`(transport.go)的 ②LookPath/③common-dirs 全 miss。解法是**在還找得到的環節解析、stamp 進 plist 讓優先序① 命中**:(a) `ocwarden install` 於安裝環境解析 claude(`resolveClaudeForInstall`,install.go:OC_CLAUDE_BIN env → LookPath → common dirs),用 `--version` 在 minimal PATH 下實測——過 = 只 stamp OC_CLAUDE_BIN;不過但在 installer PATH 下過(shim/env-shebang)= 連 installer PATH 一起 stamp 進 plist;都找不到 = 印人話 WARNING+指引(裝 claude 或 export OC_CLAUDE_BIN 重跑),不 fatal。(b) bootstrap-here 鏈(server 在 launchd minimal env 下跑 `ocwarden install`):`bin/ocserver install`(使用者互動 shell 跑)先解析 claude、stamp OC_CLAUDE_BIN(+必要時 full PATH)進 **serve plist**;bootstrap-here 的 env passthrough(`api_machines.go`)原樣帶給 ocwarden install → 其解析優先序① 命中 → 轉 stamp 進 warden plist。foreground `ocwarden run` 的 OC_CLAUDE_BIN env 優先序不變(同一個優先序①)。

## bootstrap / teardown on server(一鍵,server 本機跑;server-side handlers)
server RUN ON 被操作的機器時,owner 不用 copy-paste shell,控制台一鍵讓 **server 在本機**跑 warden 起 / 收:
- **bootstrap-here**(`POST /api/machines/{id}/bootstrap-here`):server 解析 ocwarden binary(503 若缺)→ 跑 **`ocwarden install --force`**(帶 install 需要的 `OC_BASE`/`OC_TOKEN`;identity 只來自 token `sub`,**不注入 OC_ID 且會清掉 server process 繼承來的雜散 OC_ID**——對齊 self-update 的 `env -u OC_ID` 防污染)。`--force` = **一律 OVERWRITE** 前一個 warden(重裝、跳 skip-if-present),讓重裝可靠冪等。handler `handle_bootstrap_here`。
- **teardown-here**(`POST /api/machines/{id}/teardown-here`):bootstrap-here 的對稱反向。server 在**自己 host** 跑 **`ocwarden teardown`(顯式目標,見下方 §teardown 顯式目標契約:canonical instance 送 `--canonical`、namespaced instance 送 `OC_NAMESPACE` 且**不**帶旗標)**(= `launchctl bootout` + **poll `launchctl print` 至 launchd 回報 label 真消失**(bootout 是 async;走 install 同一支 `bootoutUntilGone`)+ 移除 install artifacts,**靠 launchd 停 daemon、絕不 pkill**)。**CONFIRM-THEN-REMOVE**:僅在 daemon 確認 torn down(`exit_code == 0`,= label 確認消失 + artifacts 移除)才 soft-delete warden member;非零 / timeout 則 member 留在 roster(`removed=false`)——失敗的本機 teardown 不會把還活著的 daemon 從 registry 孤兒化,`log`(stdout+stderr)帶原因給 FE。handler `handle_teardown_here`。teardown 身分無關(只讀 HOME + uid),不需 token 接線。
  🔴 **T-42a0(2026-07-27):上面整段描述的是那條路徑「會做什麼」,但它現在對任何 `{id}` 都不會被走到——兩道 409 擋在 subprocess 之前。**
  上一句「teardown 身分無關(只讀 HOME + uid)」正是缺陷本身:**沒有機器選擇器 ⇒ 它只能拆 server 自己這台**,而 handler 從前照樣把 `RosterStatusRemoved` 寫到**被指名的那台**頭上——一次點擊,本機 daemon 被 launchd bootout、一台從沒被連絡過的機器掉出名冊(T-9cf8 之後連憑證一起撤銷)。現在:
  - **指名別台 → 409**(`teardownHereForeignTargetMsg`):這個動詞碰不到那台。別台退役走 `uninstall_machine` → `delete_machine`。
  - **指名 server 這台 → 409**(`serverSelfUndeletableMsg`,T-9cf8):修這台的 warden 用 **bootstrap-here / `install_warden_on_server_host`**(`install --force` 本來就覆蓋既有安裝,不需先拆)。
  兩道拒絕整併在 `server/ocserverd/api_machines.go` 的**單一** `teardownHereRefusal`(寫成兩道連續 `if` 時第二個是可證恆真的死條件,獨立審查實測 `if true` 全綠)。**CONFIRM-THEN-REMOVE 那段因此經 HTTP 不可達**;端點要不要退役是待 owner 裁定的獨立問題。詳見 `server/CLAUDE.md` 的 T-42a0 條。
- **治理**:兩者都是 `requires="admin_agent"`(`server/ocserverd/authz.go` 單一 choke)——T-6020(owner 2026-07-26 拍板)把它們從 owner-only 降到 remote-command uninstall / DELETE **同一級**:在 server host 上裝機拆機是 admin 助理該能跑的辦公室維運。**一般 agent 與 warden 仍是 flat 403**(這仍是在 server host 上跑碼的特權本機動作,只是治理層而非 owner 一人)。若控制台沒跑在該機上,fallback 是 copy-command 到遠端 shell 貼上跑。

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
- ⚠️ **任何驅動 `realMain(..."teardown"...)` 的測試,一律不得繞過 `newHostSeam`**(`install.go:230` 的 `var newHostSeam = realHostSeam`;`hostseam_test.go:135` 的 `TestMain` 在 `m.Run()` 之前把它整包重綁成 `fakeHostSeam`,所以測試不必、也不該自己注入什麼——`teardownCmd` 取 `newHostSeam().sys` 就自動拿到假貨,斷言讀 `hostSeamFakes`)。執行期的保底防線有三處 `refuseInTestBinary`:`install.go:133`(`realSysOps`)、`install.go:215`(`realHostSeam`)、`main.go:203`(`execRunner.Run`)——最後這處是 hand-assembled struct 也繞不過去的 process 咽喉點。realMain 的 launchd 目標來自**真實 `os.Getuid()` + canonical label**——假 HOME 只隔離**檔案路徑**、不隔離 launchd。2026-07-26 這個缺口在 review 期間兩度 bootout 掉本機 live warden。守衛本身不是測試的防護,seam 才是。
  - 📌 T-2257 原本自帶的 `teardownSysOps` package-var + `withFakeTeardownSys` helper **已在 rebase onto T-5047 時移除**(理由見 `namespace_test.go` 的 REBASE NOTE):它會構成第二個 `realSysOps` 非測試引用,`scanHostSeamSource` 直接拒跑整包。碼裡已無這兩個識別字,別再照著找。
- **e2e 側**(`e2e_test/lib/oc_lifecycle.sh`):`oc_assert_teardown_instance` 在**第一個 mutation 之前**驗 root/serve label/warden label/tmux socket 四軸與選定 instance 一致(canonical 也是一種顯式模式,混合軸即死);`oc_teardown_warden` 每次 subprocess 都顯式帶 `OC_NAMESPACE`。守衛的鑑別力由 `tests_guard/run.sh` 案例 18c(把 2026-07-25 事故的兩行 rm 注進 lib 副本,tripwire 必須真的響)與 18d/18e(混合軸必須在兩個 call site 各自死掉、且死在任何 bootout 之前)釘住。

## force-revive(activate 清 stopping/waking)
`activate`(wake)是 **force-revive**:清掉 member 的 `stopping_since` / `waking_since` 錨點,**不受 winding-down gate 擋**(即使 member 正 stopping / 甚至 online,wake 也回 200 不回 409)——讓「正在收」的 member 被重新拉回 online。行為釘在 conformance 套件與 `server/ocserverd/reconcile_test.go`;reconcile 對 genuine *stopped* terminal 也走 force-revive 覆蓋(`server/ocserverd/reconcile.go`)。
