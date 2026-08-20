# 開發指南

一般使用者請看 [docs/guide/](../guide/)（產品說明，也是控制台主導覽「使用說明」分頁的來源）；這裡是給改 code 的人。repo-wide 憲章與 land 紀律的權威在根目錄 [CLAUDE.md](../../CLAUDE.md)，各域（`server/` `cli/` `frontend/` `conformance/` `e2e_test/`）另有自己的 `CLAUDE.md`，本文不重複，只給地圖與跑法。

## 技術棧

| 面向 | 技術 |
| --- | --- |
| server | Go（`ocserverd`：REST + SSE + MCP + reconcile，goose migration，SPA 以 go:embed 內嵌，SQLite） |
| frontend | React / TypeScript（Vite） |
| cli | Go —— `ocwarden`（執行手）、`ocagent`（agent runtime） |

（歷史：原 Python backend（FastAPI + alembic）已退役移除；**本 repo 不保留回滾錨點**——那段歷史不在這個 repo 裡。）

## Repo 結構

```
server/       Go server daemon：ocserverd（route 表 / handlers / SSE hub / goose migrations）
frontend/     React/TS web UI（Vite）；build 產物由 go:embed 進 ocserverd
cli/          Go 模組：ocwarden（push-executor）、ocagent（agent runtime）
spec/         凍結的 wire 契約：openapi.json 是權威（動 wire 先改它）；mcp-catalog.json 是
              由 openapi.json 的 x-mcp 產生的 committed 生成物，不手改
seeds/        語言中立 seed .md 資產（boot context；ocserverd runtime 直讀）
conformance/  語言無關黑箱套件：server wire 行為的可執行定義（HTTP-only 回歸權威）
e2e_test/     Playwright 端到端（隔離 port，絕不碰 prod）
bin/          維運指令：ocserver / ocwarden / serve / migrate / build / ci.sh / local-ci.sh …
docs/         設計文件
oc.toml.example  server 設定範本
```

runtime 落點統一在 `~/.officraft/`：`server/`（canonical 安裝）、`warden/`（token / 設定）、`agents/`（各 agent 工作區）。

## 怎麼跑

```bash
# Go server
cd server/ocserverd && go build && go test ./...
bash bin/build           # 部署 binary：npm webdist + go build → .deploy/ocserverd（SPA 內嵌）

# frontend
cd frontend && npm install && npm run dev

# conformance（語言無關黑箱：wire 行為回歸權威；隔離、核心自動配埠）
conformance/run.sh --target go

# e2e（Playwright，隔離 :8791，絕不碰 prod）
cd e2e_test && bash run_all.sh
```

### 🔴 e2e 裡有一類測試會真的花錢，預設不跑（T-c329）

需要一個**活著的 agent 行程**的 spec（目前是機器上線那一支）會 **spawn 真的 `claude`、燒真的 API
額度**——跑一輪就是一筆帳。所以它們**預設不跑**，你什麼都不用設就不會花到錢：

```bash
cd e2e_test && bash run_all.sh              # 預設：不花錢
OC_E2E_LIVE_AGENT=1 bash run_all.sh          # 明確要求：會 spawn 真 agent，會花錢
```

（要連同 `bin/ci.sh` 一起跑的完整那一輪是 `bash bin/local-ci.sh [--live-agent]`，見〈CI〉。
它餵給這裡的就是上面那個變數，機關是同一套、不是另一套。）

- 成員資格由 spec **自己用檔名宣告**（`*.live-agent.spec.js`），`playwright.config.js` 裡**沒有檔名
  清單**——清單要靠每一支新測試記得回去登記，忘記的那支就預設偷偷跑、偷偷付錢。
- 判定是**嚴格** `=== '1'`，所以 `true` / `yes` / `TRUE` 這類打錯字**一律落到「沒跑、沒花錢」**。
- 帶了旗標時，`assert-specs-ran.sh` 那道守衛會**放行**（不會把你的主動選擇報成失敗）。
- 沒帶旗標時它**不會**為那一類多做那兩支重複的 cli build（省下的是 `cli/{ocagent,ocwarden}` 的 in-tree
  副本）。⚠️ **這不代表沒有 `go` 也跑得完**：`setup.sh` 仍**無條件**需要 Go toolchain（`build-bindist`
  的三支 ＋ `ocserverd` 一支），而它跑在這個條件之前。

⚠️ **兩件仍然成立、這張票沒有解決的事**：

1. **舊 checkout 救不回來。** 這個旗標與檔名慣例在舊樹裡是**死的**——沒有任何東西會讀它們，照著設
   只會得到「以為有防護、其實沒有」，那比明知沒防護更容易中招。**先確認機制在你這份 checkout 裡真的
   存在**（`grep -r OC_E2E_LIVE_AGENT e2e_test/`），不要靠記得設變數。
2. **那一類 spec 仍然沒有任何自動守衛。** 雲端刻意不跑它（不想每個 PR 都燒一次額度，owner 於
   `rc-d51e755d3207` 看過「讓雲端也跑」而沒有選）⇒ 那條路徑目前**只有人工在本機跑才驗得到**。
   這張票只讓它不再偷偷花錢，**沒有**讓它變成被守著的東西。

## CI

```bash
bin/ci.sh          # 綠的判準是「rc == 0 且整份輸出的最後一行精確為 [ci] all green」，兩個條件都要
```

### 更寬的那一輪：`bin/local-ci.sh`（不是每次都跑）

`bin/ci.sh` **不跑 playwright spec**（它只透過 `e2e_test/tests_guard` 驗那支腳本的 wiring 形狀，
那一輪一支 spec 都沒跑）。要把 spec 也跑起來的是另外**獨立的一支**：

```bash
bash bin/local-ci.sh                 # bin/ci.sh + e2e spec；預設不跑 live agent，不花錢
bash bin/local-ci.sh --live-agent    # 🔴 連 live-agent 那一類也跑：spawn 真 agent、花真錢
bash bin/local-ci.sh --dry-run       # 只印它會做什麼，什麼都不跑
```

**什麼時候跑**：出 GA 之前，或改到 live-agent 行為想真的看它跑起來的時候。**日常那一輪仍然是
`bash bin/ci.sh`**——這一支會架一整座站、開真瀏覽器，帶旗標時還會花錢。

- 它**不是** `bin/ci.sh` 的改名、也不是取代它：`bin/ci.sh` 一個字沒動，仍然自成一套、仍然是 push
  前跑的那一輪。`local-ci.sh` 只是**呼叫**它，再補上它刻意不做的那一段（`e2e_test/run_all.sh`）。
- 它也**不是** `make ci` 那種彙總項目：它一個檢查都不點名，那一輪是什麼由 `bin/ci.sh` 自己的
  target 陣列決定 ⇒ **repo 裡沒有第二份「CI 跑哪些」的清單**。
- phase 1 的判決照本檔上面那條兩半規則走（rc == 0 **且**末行精確等於權威字串），不是只看 rc。
- 🔴 **live agent 的機關沒有另造一套**：成員資格仍由檔名宣告（`*.live-agent.spec.js`）、
  `playwright.config.js` 仍是嚴格 `=== '1'`、`e2e_test/assert-specs-ran.sh` 仍在事後稽核
  「沒被要求的那一類有沒有偷跑」。`local-ci.sh` 只決定要餵它們哪個值，而且**它自己的旗標是唯一
  輸入**——環境裡既有的 `OC_E2E_LIVE_AGENT` 會被覆蓋並印一行說明，因為「那個 shell 還有沒有
  export 那個變數」不該是「這一輪會不會花錢」的答案的一部分。
- 🔴 **打錯字往不花錢那邊倒**：`--live-agent` 是**整個參數**比對，任何不認得的參數在**跑任何東西
  之前**就被拒絕（rc=2）。所以打錯的 opt-in（`--live-agents`）不會變成 opt-in，而更要緊的是打錯的
  **opt-out**（`--live-agent=0`）也不會被讀成同意花錢——前綴比對的 parser 正好會把後者讀成
  「開」（實測：同一條命令，前綴比對的 mutant 讓 specs 收到 `OC_E2E_LIVE_AGENT=1`，本尊 rc=2 什麼
  都沒跑）。

### 一份副本只准跑一輪 — 多輪＝多份副本（T-70c9）

`bin/ci.sh` 對**它自己那份工作副本**上鎖。同一份 clone 裡起第二輪，會拿到一則明確的
拒絕訊息並以**非零 rc** 結束：

```
[ci] REFUSED — this working copy is already running CI.
[ci]   working copy : /path/to/this/clone
[ci]   held by pid  : 12345  (started 2026-08-02T12:00:00Z UTC)
```

**為什麼不是「小心一點就好」**：ci.sh 全程在**原地**寫這份副本——`npm ci` 會把
`frontend/node_modules` 整個刪掉重建、三個 `build-*dist` staging 到固定路徑、而 `drift-*`
那幾個項目會把**五個 committed 生成檔就地重生、再跟自己幾秒前拷的備份逐位元組比對**。兩輪交錯，
那幾道比的就是 A 的備份對上 B 的重生。**失效方向不固定**：可能紅（假紅），也可能**綠**——
綠在一棵這一輪根本沒驗過的樹上。要殺的是後者——**即使合併的判準已經不在這支腳本身上**（owner
2026-08-11 裁定改看雲端那一輪，見〈CI〉）：一份描述著沒人驗過的樹的綠，會讓開發者以為自己那塊
已經確認過、就這樣開 PR 上去，而**假綠跟真綠長得一模一樣**。

**要同時多跑幾輪，就多開幾份副本**：

```bash
git clone <repo> /path/to/another-copy
cd /path/to/another-copy && bash bin/ci.sh   # 與別份副本並跑是支援的做法
```

鎖是**綁副本、不綁機器**（鎖檔就在 clone 內：`<clone>/.ci-lock`，已 gitignore），所以跨 clone
並跑照樣可行——那是唯一支援的並行方式。**沒有略過開關**：沒有環境變數、沒有旗標，也不要加。

**明確不做**：讓同一份 clone 真的能並跑（worktree-safe）。owner 明確排除。這個鎖只讓碰撞
**變大聲**，不讓碰撞**變得可以活下來**。

**當掉之後怎麼救**：ctrl-C / `kill -9` / 當機留下的鎖**會自動接管**——鎖裡記了持有者的
`(pid, 行程起始時間)`，pid 不見了、或那個 pid 已經被回收給別的行程（起始時間對不上），
下一輪就直接接手。真的要人工介入時，救援方式是刪掉那個目錄：`rm -rf <clone>/.ci-lock`。
（這句刻意只寫在這裡、**不寫進拒絕訊息**——一則附上自家解法的拒絕訊息就變成建議，不是守衛。）

⚠️ **殘留**：起始時間只有秒級解析度，所以「pid 被回收、而且新行程剛好在同一個時鐘秒啟動」
會被判成仍持有。那是**卡住一份副本**（安全方向），不是放第二輪進來；上面那行 `rm -rf` 就是解。

**同時能跑幾輪、受什麼限制**（含實測數字、跨副本天花板、git worktree 那一格的期望行為，
以及一條把「conformance 同副本 4 輪並行全綠」推翻掉的實測）：見
[`docs/dev/ci-parallelism.md`](ci-parallelism.md)。

可執行形式是 `bin/lib/ci-lock.sh`，守衛是 `bin/tests/ci-lock-guard.sh`（由 `bin/tests/run.sh` 派出，
也就是 `test-bin-guards` 這個檢查項目；⚠️ 舊文寫「CI step 0b」，而 T-4d88 之後 `bin/ci.sh` 裡已經沒有
編號步驟——它印的是一份 target 名單，`grep -n '^echo "\[ci\] (' bin/ci.sh` 現在零命中）。
守衛**不會**真的並跑兩輪完整 CI：驗一個 mutant 必然要讓鎖失效，那時真並跑會把開發者的樹弄爛——
一條「在它正在測的守衛失效時會造成真實破壞」的測試是定時炸彈。它改用拋棄式目錄 + 輕量替身
行程去驅動 ci.sh 用的**同一份 lib**。

判準為什麼是 **AND**（T-d3e3）：兩半各自都不夠。

- **寬鬆 grep 完全無效**：`test-e2e-isolation-guard` 這個項目跑的 `e2e_test/tests_guard` 第一步就印自己的 `all green`，所以**任何**中途爆掉的 log 裡都已經含有那個子字串。
- **只看最後一行會被 dispatch 的 lane 偽造**：ci.sh 不是這份 log 的唯一寫入者。一個被 dispatch 的 lane 只要 `echo "[ci] all green"; exit 1`，ci.sh 的 `set -e` 就在那裡中止，偽造的權威剛好留在最後一行——這個假綠是真的被做出來過的。
- **只看 rc 也不夠**：這個 repo 有前例，`bin/common.sh` 的 `set -e` 打敗了 `run_all.sh` 刻意的 rc 捕獲，讓失敗訊號默默消失。舊文所以寫「判準是 marker、**不是** exit 0」——那句話講的是 **rc 不足以單獨判綠**，不是「rc 不該被檢查」。要求兩者同時成立比任一半都嚴格，與原意相容。

`bin/tests/ci-success-marker.sh` 是這條規則的可執行形式：它同時掃描 **ci.sh 以及每一個被 dispatch 的 lane 腳本**，要求除了 ci.sh 之外沒有任何 shell 腳本「有能力」印出這個權威字串。

🔴 **合併的判準是雲端那一輪，不是這支腳本**（owner 2026-08-11 於卡 `rc-c16ac4679fab` 選「以雲端那一輪為準」）。他當場補的那句話定義了本機這支現在的角色，逐字照錄：

> 「但是自己開發的時候，當然要自己先確認通過自己新增或是跟自己有關的測試，沒問題才開PR上去跑，確認全部都通過」

⇒ 三段：**開發時**自己先跑動到的那部分 → **開 PR** 讓雲端跑全部 → **雲端全部通過**才算可以合併。**本機這支不是被廢掉，是換了角色**：從「每次都要整輪跑完的判官」變成「開發當下驗自己那塊的工具」。判準搬走的理由是**可見性與時效**——本機那份綠只有跑的人看得到，而且基底一動就過期；PR 上那一輪誰都看得到、誰都能重跑。
⚠️ **不要把「以雲端為準」讀成「本機都不用跑了」**：那正是上面那句原話在擋的誤讀。

`bin/ci.sh` 從第一個非零步驟就 fail-fast。**gate 內容以 `bin/ci.sh` 裡那份 target 名單、以及 `Makefile` 的具名項目為準**（`grep -nE '^[a-z][a-z0-9-]*:' Makefile`）——⚠️ 舊文寫「以 `bin/ci.sh` 自己的步驟標頭為準」，而 T-4d88 之後那支腳本裡一道檢查都沒有、也沒有編號步驟了：它只剩鎖、provenance 戳章、以及一份 target 名單。⚠️ **這裡刻意不複製一份清單**：舊文那份（「go gate / 黑箱 lint / gitleaks / FE typecheck+drift」）漏掉的比列到的多，而它從變假的那天到被發現為止，沒有任何東西會叫它。複製品沒有東西釘著它；那份名單有。

（這裡曾經寫過兩個「為什麼判準留在本機」的理由，**兩個今天都不成立，留著是為了讓下一個人知道它們被推翻了、不要再重提**：①「不付 GitHub Actions」——repo 轉 PUBLIC 後就不成立，公開 repo 用標準 runner 是免費的；②「gate 裡有大量 host-shaped 與逐位元組比對的步驟，不想把權威搬到雲上」——**T-4d88 把每一格都改在 macOS runner 上跑之後，那個顧慮消失了**，而 owner 2026-08-11 已裁定判準改看雲端。）

**雲端 check（`.github/workflows/ci.yml`）**：`pull_request` **與 push-to-`main`** 兩個觸發（後者由 T-ab2a 補上：在那之前，合併之後沒有任何東西會跑，所以兩個各自綠的 PR 併起來讓主幹變紅時，要等到下一個開 PR 的人繼承那片紅才會有人發現）。兩個觸發跑的是**同一組檢查、同一份定義**——刻意不為 `main` 另列一份清單。⚠️ **「同一組 job」已經不字面為真**:`notify-main-red`(T-5d3b)與 `auto-beta`(T-9fe3)都是 main-only,PR 上不會跑。字面成立的是**檢查**這一層——那兩個 job 都不檢查任何東西,`main` 不會跑任何 PR 跑不到的檢查——而這一層由 `bin/tests/auto-beta-guard.sh` 用差集擋住(見〈自動發 beta〉)。`main` 上另外**關掉 cancel-in-progress**：被取消的 run 回的是 `cancelled` 而不是紅，那會讓真正弄壞主幹的那顆 commit 完全沒有判決，在 commit 列表上跟「通過了」長得一樣。內容是「雲端跑得動的全部」,而**清單刻意不複製到這裡**(見 `CLAUDE.md`〈文件鐵律〉;本檔下面那句「裡面沒有、也不准有第二份模組清單或閘門清單」講的就是這件事,而這一段本來自己就是那第二份)——以 `Makefile` 的具名項目為準:`grep -nE '^[a-z][a-z0-9-]*:' Makefile`。⚠️ **上行契約那一對是 T-uplink-cloud 從「只在本機」搬上來的，而且兩支一起搬**：它純用 python3 標準函式庫掃 tracked 檔，不要服務、不要金鑰、沒有 macOS 形狀（python3 本來就是這支腳本的硬相依——conformance venv 就在用）。只搬掃描器不搬陽性對照會更糟：沒被驗過的掃描器照樣印綠，那正是「綠得有洞」。⚠️ **這是新增、不是搬離**：`bin/ci.sh` 兩支照跑。⚠️ 🔴 **這一段原本接著寫「`bin/ci.sh` 是完整集合、仍是 land 權威，雲端這份是它在乾淨 Linux 機器上的子集 cross-check」——那句話今天兩個部分都不成立，一併改掉**：(a) **判準已經改在雲端**（owner 2026-08-11，見〈CI〉那段的原話）；(b) **Linux 那一格已經收掉**（T-4d88，全部改在 macOS 上跑）⇒ 沒有「乾淨 Linux 機器」這回事了。今天的實況是：**兩邊呼叫的是同一份做法**（`Makefile` 的具名項目），差別只在**誰在跑、以誰為準**——雲端那一輪是判準，本機那支是開發當下自己驗自己那塊用的。

**主幹紅了誰會知道（T-5d3b）**：上面那句「要等到下一個開 PR 的人繼承那片紅才會有人發現」原本只被修掉一半——判決確實掛在那顆 commit 上了，但「有人收到通知」這半沒有任何機制（GitHub 的預設失敗通知是每個帳號自己的設定，從 repo 這邊讀不到）。現在 workflow 多一個 `notify-main-red` job：**只在 push-to-`main` 且有 job 失敗時**，打一發回呼到維護者（Kyle）的收件匣，內容是一行「哪顆 commit、哪些 job 紅、執行紀錄連結」。owner 裁定（`rc-c2edbfdc36a1`）**只做回呼、不自動開 issue**。

⚠️ **它保證的比你想的少，三條都要知道**：

1. **回呼不會把人叫醒**。收件人離線（換手中）時訊息進佇列，等他上線才讀到 ⇒ 買到的是「**一定會被讀到**」，不是「立刻有人在處理」。
2. **通知到的是維護者、不是 owner**。要不要往上報由他判斷。
3. **沒有做「不依賴那個人存活」的那半**（自動開 issue 被 owner 明確排除）。他剛好被換掉、或那台機器掛了，紀錄就仍然只在那顆 commit 上。

⚠️ **另外四種「main 紅了，而這個機制安靜什麼都不做」的狀態**——上面三條講的是「不會叫醒人」，這四條講的是**根本不會送**：

4. **workflow 檔自己語法壞掉** ⇒ GitHub 以 startup failure 收場、**一個 job 都不會被排程**，包含 `notify-main-red` ⇒ 一發都不送，而 Actions 頁面上該輪與「還沒開始跑」長得一樣。**這不是假想**：本包（T-5d3b）自己就發生過一次——`run:` 的 block scalar 被一行頂到第 1 欄的續行提早結束，整份 ci.yml 不是合法 YAML，那顆 commit 的 run 是 `jobs=0 / conclusion=failure`，PR 上零個 check，而本機完整 CI 與全套守衛**全綠**（本機當時沒有任何一關解析過 workflow YAML）。現在 `bin/tests/main-red-notify-guard.sh` 第一條就是「這個檔解析得了」，**拿不到 YAML 解析器時它會紅、不會 skip**。⚠️ 它只認**一個**解析器（`ruby` + psych，macOS 與 macOS runner 都內建；刻意不留 fallback——兩個解析器對未知 tag、多份文件這類輸入的判決相反，留兩條路等於讓「這台裝了什麼」決定紅綠）。而它證明的是「**一個** YAML 解析器讀得懂這個檔」：GitHub 自己的 workflow 解析器既不是 psych 也不是 PyYAML，所以這一條是**必要條件、不是充分條件**。
5. **通知 job 自己失敗時沒有第二條路**。secret 沒設它就 `::error::` + `exit 1`；curl 用光重試、DNS 掛掉、endpoint 回 4xx/5xx（現在有 `--fail-with-body`，這些會紅而不是靜默 exit 0）——**唯一的表現都是在一個本來就已經紅的 run 上多紅一格**。要有人看到那一格，得先有人去看那個 run，而「有人會去看」正是這整個功能假設不存在的東西。⚠️ **已知限制（沒有要修，是 owner 的判斷）**：連線層失敗時（DNS 解不到、連線被拒），curl 的 stderr 會把 **host 與 port 印進公開的 Actions log**（例如 `curl: (6) Could not resolve host: <host>`）。token 在 URL 的 path/query 上，所以**憑證本身不會出現**（實測所有失敗模式的 stderr 都只有 status code 或 host:port，沒有 path/query）；會外洩的是「維護者的 inlet 掛在哪個 host」。GitHub 對 `secrets.*` 的自動遮蔽是拿**整個 secret 值**做字串比對，host 這種**子字串不會被遮**。
6. **secret 的存在性只在「需要它的那一刻」才第一次被驗證**。這條路徑只在 `failure()` 為真時執行，所以 secret 從沒設成功、被輪替或被誤刪，可以維持任意久而沒有任何跡象。**目前沒有**任何「每次 push-to-main（含綠的）就確認 secret 還在」的探測，也沒有低頻存活探測——要不要加是新功能，由 owner 決定。
7. **run 卡在 queued**（macOS runner 排不到）⇒ 這一輪永遠不會有 conclusion ⇒ 永遠不通知。

另外兩件形狀上的事：**被取消的 run 不會通知**（`failure()` 對 cancelled 為假，與上面「cancelled 不是紅」的立場一致）；**pull request 不會通知**（那是送 PR 的人自己的分支，紅就在他眼前）。`needs:` 那份 job 名字清單由 `bin/tests/main-red-notify-guard.sh` 守著——**新增任何一個 job 卻忘了加進去會紅**，而不是安靜地少通知一項。⚠️ **「每一個其他 job」包含 `auto-beta`（T-9fe3）**：它不是閘，但它做的事是發出站台實際會跑的那顆 release，而**發版在 `main` 上失敗，本身就是「已合併」與「使用者拿得到」默默漂開**——那正是 `auto-beta` 存在要堵的洞，不通知等於在旁邊再挖一個。邊只有這一個方向：`auto-beta` **不**能 needs `notify-main-red`（notify 只在失敗時跑，等它會讓 `auto-beta` 永遠不跑；兩邊互等就是 `needs` 循環，GitHub 會整份拒絕、零個 job 被排程）。

⚠️ **T-4d88 起,每一道檢查的做法只存在一份、寫在 repo 根目錄的 `Makefile`（一道檢查一個具名項目）**；workflow YAML 只負責裝釘好版本的 toolchain 然後 `bash bin/run-checks.sh <項目>…`，**裡面沒有、也不准有第二份模組清單或閘門清單**——要加請加一個 Makefile 項目。⚠️ **為什麼不是裸的 `make`**：rc 為 0 只說「沒有東西失敗」，不說「有東西跑過」——一個 recipe 被清空、被註解掉、或被一句提早的 `exit 0` 截斷的項目，會瞬間而且完全安靜地成功。所以每個檢查項目的 recipe **最後一個子句**印出自己的 `[oc-check-done] <項目>`，`bin/run-checks.sh` 則要求**它這次被點名的每一個項目**都出現了自己的那一句，少一句就 `exit 1` 並點名（實測兩顆 mutant：把 `scan-tracked-paths` 的 recipe 清空、以及在它中間插一句 `exit 0`，兩顆在裸 `make` 下都是 rc=0 全綠，走 `run-checks.sh` 都是 rc=1 並點名該項目）。這道保護不是新的：T-4d88 之前雲端 macOS 那一格就是把子集腳本 `tee` 起來再 grep 它的結尾標記，腳本被刪掉時這道保護跟著沒了家。**每一格只驗自己那一格點名的項目**，任何地方都沒有、也不該有一份「全部檢查」的清單。⚠️ **刻意沒有 `make ci` 這種彙總項目**(owner 裁定):彙總就是第二份清單,而沒人看的那份會漂。呼叫端各自點名要跑哪些項目。⚠️ 舊文說子集的定義寫在 `bin/ci-cloud.sh`／`bin/ci-macos-host.sh`——那兩支已經在 T-4d88 刪除,因為它們各自都得把檢查重述一遍,於是同一條規則在 repo 裡有三份、而且已經漂了。

⚠️ **workflow 裡的 go / node 版本釘選是承重的、不是衛生習慣**：一致性檢查斷言的是「重生的位元組與 committed 完全相同」，runner 的 toolchain 一旦浮動超前開發機，這一類就會在「碼完全沒問題」的情況下變紅。

⚠️ **T-4d88 起沒有 ubuntu 那一格了**(owner 裁定:全部改在 macOS 上跑)。下面這段講的 ubuntu／Linux 對照是**歷史**,留著是因為它記著每一項當初為什麼進不了 Linux；今天的實況是每一格都在 macOS,而「Linux 上會怎樣」這件事這個 repo 不再有任何量測。**（歷史）不在 ubuntu `cloud-gates` 裡的**：`bin/tests/run.sh`（Linux 上會紅；根因是 BSD/GNU `mktemp -t` 語意、SIGPIPE 與 macOS 形狀的 `install.sh` fixture，尚未移植。⚠️ **這裡刻意不寫失敗條數**：舊文寫死「16 條」，而那個數字只在量它的那一天為真——`bin/ci-cloud.sh` 的檔頭早就記著它「在套件第一次改動時就過期了」。T-9fe3 又往這個套件加了一整組 `auto-beta-guard.sh`，所以那個數字現在更沒有理由成立，而**我們也沒有 Linux 機器可以重量**——正解是不要斷言一個沒人在維護的數字）、Playwright CT（真瀏覽器版面守衛；macOS↔Linux 的字型與光柵化差異會讓紅燈的意思從「版面壞了」變成「runner 字型不同」，而 Linux 那一側**從來沒被量過**）、gitleaks（內容級機密掃描）、`e2e_test` 的真機端到端測試（要真的 fleet host）。⚠️ **「不在 cloud-gates」不等於「只跑在本機」**：上面這幾項現在都有 macOS runner 上的 job(CT 是 T-0fef 才接上的,在那之前它確實只跑在一台開發機上)。**哪一項跑在哪個 job,這裡刻意不列**(見 `CLAUDE.md`〈文件鐵律〉——這種對照表正是會靜默過期、而且沒有東西會叫它的那種):要對號就讀 `Makefile` 的具名項目(`grep -nE '^[a-z][a-z0-9-]*:' Makefile`),以及 `ci.yml` 裡每一格 `run:` 點名了哪幾個項目(`grep -n 'run-checks' .github/workflows/ci.yml`)——⚠️ 舊文寫「讀那兩支腳本自己的步驟標頭」,那兩支(`bin/ci-cloud.sh` / `bin/ci-macos-host.sh`)在 T-4d88 已經刪除。**雲端流程的每一道閘都不用任何 secret，所以 fork PR 也能跑完整**——T-5d3b 之後 workflow 裡確實有一個 secret（`notify-main-red` 用的回呼網址），但它**不是閘**、只在 push-to-`main` 那條路徑上跑，而 fork PR 本來就拿不到 repo 的 secret ⇒ 對 pull request 而言上面那句性質一字未變。⚠️ 把 secret 加進**任何一道閘**就會改掉它（fork PR 會變成跑一份比我們小的檢查）。

**Go 測試一律 `-count=1`（T-bedc）**：跑 `go test` 的地方一律帶 `-count=1`——⚠️ **今天那個地方是 `Makefile` 的 `test-go` recipe，不在 `bin/ci.sh` 裡**（T-4d88 把每道檢查的做法收斂成 Makefile 裡唯一一個具名項目；舊文寫「`bin/ci.sh` 裡跑 `go test` 的那一步」與更舊的「CI step 1e」都已不成立，而子標籤本身就是會漂的東西，指它不如指下面那道守衛）。`-count=1` 是「不吃 go 的測試結果快取」，**不可省**。省掉的後果是實測過的——log 裡出現 `ok  ocwarden  (cached)`，那格綠燈認證的是一次**根本沒執行**的跑。兩個獨立理由：(a) 快取 key 只涵蓋 package 的**輸入**，不涵蓋測試真正碰的世界（port、時鐘、launchd、host fleet、staged embed assets 的**效果**），所以今天會紅的 package 照樣報 ok；(b) 它**結構性地藏 flake**——一個 suite 只在「第一個改到它輸入的 commit」上跑過一次，間歇性失敗於是被攤平到近乎零觀測機率，`[ci] all green` 變成在講快取而不是在講碼。可執行形式是 `bin/tests/go-test-nocache-guard.sh`（由 `bin/tests/run.sh` 派出）：它以**命令位置解析**（不是 substring grep——那會匹配到說明文字）掃全 repo 的 shell 腳本**以及 tracked 的 make 檔**，任何 `go test` 呼叫點少了 `-count=1` 就紅。⚠️ **掃描集合長出 make 檔是 T-4d88 補的**：呼叫點搬進 `test-go` 的 recipe 之後，只掃 shell 腳本的舊版本掃到 0 個呼叫點——那是空集合上的真空綠，連 mutant 都造不出來。注意 `go build` / `go vet` 的快取**刻意不管**：那是對編譯本身做 content-addressed，命中等價於未命中；只有**測試結果**快取會宣稱「行為被觀察過」而其實沒有。

改 Go 後只需 fresh build 驗證；`bin/ocagent`、`bin/ocwarden`、`bin/ocserverd` 若出現都是 gitignored build artifact，**永不 commit**。CI 一律編譯 source。部署 binary 由 `bin/release` / GitHub Release fresh build 產出。

**唯一的例外是 TCC 身分錨點 `dist/officraft/officraft`（owner 明確核可，T-5831）**：它是 launchd 的 responsible process，而 TCC 用 bytes 認身分，所以那份 bytes 本身就是要被審的東西。`.gitignore` 只放行 `dist/officraft/` 底下四個路徑，其餘 `dist/` 照舊全擋。它附兩份紀錄（`source.sha256` 與 `binary.sha256`），由 `bin/check-officraft-dist` 比對（`scan-tcc-anchor` 這個檢查項目；⚠️ 舊文寫「CI step 3」，T-4d88 之後已無編號步驟）；重建方式與**為什麼 build 一定要帶 `-trimpath -buildvcs=false`** 寫在 `dist/officraft/BUILD.md`。

## wire freeze

wire（HTTP OpenAPI 面、MCP tool 面）已凍結：**動 wire 一律 spec 先行**——先改 `spec/openapi.json`（+ owner 過目），再 `bash bin/gen-ocapi` 重生、動碼。

⚠️ **MCP tool 面的入口也是 `spec/openapi.json`，不是 `spec/mcp-catalog.json`（T-2590 起）**：目錄是 committed 生成物，由 `bin/gen-mcp-catalog` 從每個 operation 的 `x-mcp` 區塊渲染。動工具面 = 改該 operation 的 `x-mcp` → 跑 `bin/gen-mcp-catalog`（預設就地寫 `spec/mcp-catalog.json`）→ 兩個檔同批 commit。`make drift-mcp-catalog` 會把 committed 目錄跟重新渲染的結果逐位元比對，忘了重生就會紅。

CI 的 wire-freeze gate 擋任何未過 spec 的漂移；行為面由 `conformance/run.sh --target go` 收官。完整紀律見 [CLAUDE.md](../../CLAUDE.md)「驗證、CI 與出貨」、產生器與兩道守衛的分工見 [spec/mcp.md](../../spec/mcp.md) §5。

## 發版指令(T-588c)

發版只有兩條指令,`bin/release` 全包,**不再有「印一行 `gh release create` 給人貼」的半套形式**(舊的 `bash bin/release <tag>` 已移除,打它會拿到非零退出 + 正確替代指令):

```
bin/release publish --beta <tag> --target <sha> [--dry-run] [--no-settle]
bin/release promote <tag>                       [--dry-run]
```

- `publish` 從 `<sha>` 切一個**丟棄式 detached staging worktree**(不是「當前 tree 乾淨就好」——bytes 來自你指名的那個 commit),在裡面 build、打包、**上傳前先驗 artifact**(tarball member list、三顆 binary 的 arm64 mach-o、從 `go version -m` 讀 ocserverd 真正被 link 進去的 `appVersion`/`buildSHA`、`shasum -c`),然後**一次** `gh release create --prerelease --target <sha>` 帶齊三個 asset(所以不存在「release 已建立但 asset 只上傳一半」的視窗)。
- 🔴 **`publish` 在 build 之前會先在那個 staging worktree 裡跑一次完整 `bin/ci.sh`,不綠就不發(T-b65e,owner 2026-08-02 明令)**。驗的是**即將出貨的那一棵樹**,不是「這台機器上碰巧有一份綠的紀錄」:rc 取自 `ci.sh` 自己、log **末行**必須精確等於 `[ci] all green`、跑完 tracked 檔不得有任何變動,證據落在 `dist/release/ci/<short>-<utc>-<pid>/ci.log`(per-run 唯一目錄,`mkdir` 非 `-p`,撞名硬錯)。任何一項不過 ⇒ 以非零退出中止,**不 build、不打包、不上傳、不打 tag**。為什麼要有這道閘:合併端已放寬(雲端門禁過就按),而 beta 會被站台的 auto_update 自己撿去上正式站 ⇒ **這是上線前唯一一道行為驗證**。⚠️ **沒有跳過開關,也不要加**(owner 卡 `rc-ffb4b06ad1d9` 拍板,與 CI 互斥鎖「刻意不留 bypass」同一立場)。`--dry-run` **照跑**這道閘——彩排不跑最可能擋下發版的那一步就不算彩排。
  - **代價要知道**:staging worktree 是全新 checkout,所以那一輪 CI 沒有 `node_modules` 可重用——完整 `npm ci` + 四個 Go module 的 `-count=1` 測試 + Playwright CT + conformance,**估 10 分鐘以上**,而且需要網路、gitleaks、Playwright 瀏覽器快取。`--dry-run` 彩排現在一樣貴。證據目錄**不會自動清理**(每次 publish/彩排留一份完整 CI log 在 `dist/release/ci/`,gitignored,自己看著清)。
  - `promote` **刻意不再驗一次**:它不重 build,出貨的 bytes 就是那顆 beta 已經被這道閘驗過的 bytes;再跑一輪只是換一棵樹重驗,不會更真。
- 🔴 **出 GA 前的那一輪由人自己跑,不在 `promote` 裡**:`publish` 那道閘跑的是完整 `bin/ci.sh`,而那一輪**一支 playwright spec 都沒跑**(它只驗 `run_all.sh` 的 wiring 形狀)。要在出 GA 前真的把 spec 跑過一遍,跑 `bash bin/local-ci.sh`(改到 live-agent 行為時加 `--live-agent`,會花錢),見〈CI〉。**這刻意沒有被接進 `bin/release`**——那會讓每一顆 beta 都多付一輪 e2e,而自動發 beta 是無人值守的。
- `promote` 把**既有且已驗過**的 prerelease 翻成正式版,**不重 build**——大家測的 bytes 就是出貨的 bytes。翻完回讀,若 asset 集合在翻的過程中變了(有人偷偷重傳)那是**失敗**,不是警告。
- `--dry-run`:build + 驗完就停,印出它本來會跑的上傳指令,**什麼都不上傳**。彩排用這個。
- `--no-settle`:**不等站台升上來**(跳過第 8 步)。理由是 owner 2026-08-05 裁定一:OffiCraft 是大家自架的服務、不是這個 repo 在營運的 SaaS,所以「某台站有沒有升上去」不是發版的成功條件;而無人值守的 runner 根本連不到私人站台,不給旗標的話每一輪自動發版都會死在一個跟 artifact 無關的理由上。**只是明示的 per-invocation opt-out**:預設一個字都沒變(人工發版照樣等站台、等不到照樣紅),第 7 步的回讀與 build 前的 CI 閘都**不受影響**,而且開了旗標那一輪會印一行講明為什麼不驗、結尾成功訊息也**不會**宣稱站台已在那顆 commit 上。守衛在 `bin/tests/release-guard.sh`(E3/E3b/E3c/E3d):E3 是它的**陽性對照**——同一組假站台狀態(站在別的 commit),不帶旗標仍以 `station-settle` 失敗,帶了才回 0。

### 回讀查證(publish 的第 7、8 步)

發完不靠人記得手動確認。`publish` 會**問 GitHub 它到底存了什麼**並逐項要求:每個預期 asset 都在且 `state=uploaded`、size 非零、沒有多餘 asset、`targetCommitish == <sha>`、`isDraft == false`、`isPrerelease == true`;然後 poll 線上站台的 `GET /api/version` 直到 `git_sha` 對得上 `<sha>`(prefix,至少 7 字元)、且 `GET /api/health` 答 ok。**任何一項不合就 exit 6 並指名是哪一項**:`[release] VERIFY-FAILED [asset-uploaded]: …`。這條的可執行守衛是 `bin/tests/release-guard.sh`(由 `bin/tests/run.sh` 派出,即 CI step 0b;PATH-shim 假 `gh` + 假 `curl`,完全不碰網路、不建任何 release、不連任何站台)。

**回讀的 payload 形狀是「量過的」,不是猜的**(2026-07-26,對 `pkyosx/OffiCraft` 的 `v0.5.38` 實測 `gh release view --json assets,isDraft,isPrerelease,targetCommitish`):

```
isDraft False | isPrerelease True | targetCommitish fb89a69aad8c
{'name': 'checksums.txt',                         'state': 'uploaded', 'size': 181}
{'name': 'install.sh',                            'state': 'uploaded', 'size': 70730}
{'name': 'officraft-v0.5.38-darwin-arm64.tar.gz', 'state': 'uploaded', 'size': 16842394}
```

也就是 asset 子欄位 `name`/`state`/`size` 確實存在、`state` 就是字串 `"uploaded"`、`size` 是非零整數——正好是回讀真正依賴的三件事。**為什麼要特別量**:同一張票裡,`verify_artifacts` 的架構檢查就是因為「猜 `file -b` 的輸出順序」而寫成永不可能命中的 pattern(`file` 實際輸出 `Mach-O 64-bit executable arm64`,架構在最後),導致每次 publish 都死在 `[artifact-arch]`。假設外部工具的輸出格式是同一類 bug,所以這裡改成量。要改形狀前**先重量一次**。

**第 8 步的語意:publish 不觸發升級,它只「觀察」升級發生**(⚠️ 這一步可以用 `--no-settle` 明示地不做——見上面那條;第 7 步不行、沒有旗標關得掉)。站台是靠 owner 帳號上的 **auto_update** 自己去撿新 release 的,而 **prerelease 也算**:2026-07-26 實測,`v0.5.38`(`isPrerelease=true`)建立後約 **2–3 分鐘**站台自動升上去、`/api/version` 的 git_sha 回讀查證。預設等待預算 60 × 5s = 5 分鐘,約為實測延遲的兩倍。所以「發完等站台升上來」是**正確的流程期待,不是設計缺陷**;但若哪天 auto_update 被關掉,這一步就會**合理地**失敗,而失敗訊息會明講「只有這一項沒達成、asset 與 release 本身都對」,以免下一個人跑去查 artifact。⚠️ **這一步能成立的前提是那台站把兩個開關都打開了,不是 auto_update 的普遍行為**:`updater.receive_beta`(關著就只收正式 release)與 `updater.auto_update` **預設皆為 OFF**(見下方〈主幹綠自動發 beta〉與 `server/CLAUDE.md`)。所以上面那個「若哪天 auto_update 被關掉」不是假想:對一個預設狀態的站來說,那才是常態。

## 主幹綠自動發 beta(T-9fe3)

**push 到 `main` 那一輪 check 全綠 ⇒ 自動發一顆 beta prerelease。** 沒有人要記得發版了。owner 2026-08-05 兩條裁定:

1. **站台有沒有升上去不是成功條件**(自架服務,不是這個 repo 營運的 SaaS)⇒ 自動路徑帶 `--no-settle`。
2. **GA 維持人工**:自動路徑**不碰** beta→final 的翻版子命令、不動 Latest 指向。owner 已知情接受「**他那台**會自動吃進每個 beta、而且沒有退版按鈕」⇒ **刻意不加任何保護、節流或確認閘**,要加就是在推翻一個已經做過的決定。

⚠️ **不要把這件事讀成「合併就等於上線」。** 自動發出來的是 **prerelease**,而一個站要真的換過去,`updater.receive_beta`(**開**了才連 GitHub prerelease 一起吃;關著就只收正式 release)與 `updater.auto_update` **兩個都得開**。兩者的常數、struct 欄位與 `getBool` 讀取全在 `server/ocserverd/settings.go`,DB 沒有那一列時不寫 dst ⇒ 維持 bool 零值 **`false`**(`auto_update.go` 是跑迴圈的地方,不是預設值所在)。預設狀態的站兩個都是關的 ⇒ 對它來說「land ≠ 上站」完全沒變。owner 那台兩個都開著,所以會;別人的站要不要跟,是那個站主自己的設定。

實作分三塊,`.github/workflows/ci.yml` 裡只有呼叫、沒有邏輯(照該檔檔頭「WHAT runs 一律住在 repo 的腳本裡」):

- **`bin/next-beta-tag`** —— 唯讀算版號,`v<major>.<minor>.<patch+1>-beta.1`,基底是**現存最大 semver**。「現存」= **git tag ∪ GitHub release 的 tag** 兩者聯集(release 被刪掉會留下 git tag、手推的 tag 從來沒有 release,只讀一邊算出來的名字會撞);比較是 **semver 語意**不是字串排序(`beta.10` > `beta.9`,`v0.5.78` > `v0.5.9`);候選集合是空的就**硬失敗**,不會退回 `v0.0.1-beta.1`(空集合的現實原因是查詢壞了,而那個「貼心的」退路會在一個有上百個 release 的 repo 上重發史前版號)。任一邊查詢失敗一律致命。
- **`ci.yml` 的 `auto-beta` job** —— `needs` 是**全部** gate job(「主幹綠」必須是指全部檢查),`if` 同時限制 `event_name == 'push'` 與 `ref == 'refs/heads/main'`,`runs-on: macos-15`(`bin/release publish` 只跑 darwin/arm64,而且它會在 staging worktree 裡跑完整 `bin/ci.sh` 再 build),`permissions: contents: write` **只掛在這一個 job**、workflow 層維持 `read`、用內建 `GITHUB_TOKEN`(無 PAT、無 repo secret)。publish 那一步釘的是**觸發那一顆 commit**、不是 job 開跑時 `main` 指到哪,而值是**走 `env:` 進去的**(`OC_TARGET_SHA: ${{ github.sha }}`,`run:` 裡用加引號的 `"$OC_TARGET_SHA"`)——**不是把 `${{ }}` 直接插進 `run:` 腳本本體**。⚠️ 這一句以前寫的是插值形狀,而樹上早就是 `env:` 了;照舊文改回插值不會有任何東西紅(守衛只認語意、不認寫法,見下一條),所以這裡把實際形狀寫死。理由在 `ci.yml` 那段註解裡:插值是 shell 之前的文字替換,`env:` 不需要「值剛好安全」這種運氣。
- **`bin/tests/auto-beta-guard.sh`** —— 由 `bin/tests/run.sh` 派出(即 `make test-bin-guards`)。⚠️ **這是全 repo 唯一會解析 `.github/workflows/*.yml` 的東西**:曾經有一次改動本機全綠、GitHub 直接 startup failure(零 job、什麼都沒跑,而畫面上跟「沒紅」一樣),所以第一條斷言就是「它 parse 得動」,而且**拿不到 parser 是 FAIL 不是 skip**。parser 用 **ruby + 內建 psych**——hosted macOS runner **沒有 PyYAML**,ruby 兩邊都有;另有一組**陽性對照**先確認那個 parser 是真的(餵一份確定無效的 YAML 必須被拒、餵一份最小合法 workflow 必須被接受),因為「解析器有解析到」是其餘每條斷言的前提,而審查者實測用一個假 `ruby` 讓一份壞掉的 ci.yml 拿到滿分。其餘擋:`needs` 與**已宣告的 gate job 集合**的**差集兩個方向都必須為空**(判準是計算式不是列舉表:少一個 gate 會叫,把非 gate 的 job 塞進 `needs` 也會叫)。⚠️ **差集的對象是 gate 集合、不是「全部 job 扣掉自己」**——T-5d3b 的 `notify-main-red` 只在失敗時跑,`auto-beta` 等它就永遠不會跑,而 `notify-main-red` 又必須 needs `auto-beta`,兩條加起來會是 `needs` 循環、GitHub 整份拒絕零個 job。**哪個 job 是 gate 由 job 自己在 `ci.yml` 裡用 `# oc-job-role:` 標記宣告,並且由守衛強制**:沒標記、標兩個、值讀不懂、或文字掃描與 YAML parser 對「有哪些 job」看法不一致,一律 **FAIL 並點名那幾行**(分類不出來絕不默默歸邊);標記還要被佐證——宣告非 gate 的 job 必須把 `if` 釘在 `refs/heads/main`,宣告 gate 的不准釘,所以「給一道真的閘加 `if` 讓它在 PR 上跳過」不能拿來把它移出必等集合。⚠️ **另有 W1x:宣告 not-a-gate 的 job 集合必須恰好是 `{notify-main-red, auto-beta}`**——老實標、`if` 也釘對的**第三個**非閘 job 一樣會紅並被點名行號。理由是「離開 auto-beta 的必等集合」是一個有 owner 裁定在背後的決定(見 `CLAUDE.md`「驗證、CI 與出貨」 的豁免名單),這一格把那句散文變成會紅的機制:要加成員只能改這道守衛本身,而那就是裁定要求的有意識動作。其餘擋:`if` 兩個條件都在、`contents: write` 只在這個 job、`.github/workflows/` 內**不得出現** beta→final 那個子命令的名字、**也不得出現會搬動 Latest 指向或發佈 draft 的那組 `gh release edit` 旗標**、publish 呼叫必須帶 `--no-settle` 且 `--target`／`--beta` 的值**在語意上**綁到觸發 commit 的 SHA 與算出來的 tag(走 `env:` 或直接插值都算,守衛不寫死其中一種形狀)、publish step 必須被 staleness 那一關 gate 住、checkout 必須 `fetch-depth: 0` **且** `fetch-tags: true`、`.github/workflows/` 底下**只能有 `ci.yml`**。版號規則本身用假 tag 清單直接餵函式測(`beta.9` vs `beta.10`、只有正式版、清單為空、撞名拒絕),**候選集怎麼湊出來的另有一組(S 段)直接驅動 `nbt_collect_candidate_tags`**:兩源聯集真的是聯集、任一源非零退出致命、任一源 rc=0 但回空**也**致命。
  - ⚠️ **兩個宣稱要讀窄**:①「GA 不可被自動化」實際保證的是「那個子命令的名字 + 那組旗標」這兩個形狀,workflow 仍可經由它呼叫的腳本、`gh api` 或 REST 繞過;②「主幹綠 = 全部檢查」實際作用域是 **ci.yml 這一個檔案內的 job**(GitHub 沒有跨檔 `needs`),所以守衛改成硬性要求「只有 ci.yml」——多一個 workflow 檔就紅,逼人當場決定,而不是靜靜放寬「綠」的含意。

**這條路徑上仍然沒有任何跳過開關**:`publish` 在 build 前跑完整 `bin/ci.sh` 那道閘照跑,第 7 步回讀照跑。

### ⚠️ job 紅 ≠ 沒有 release(要人工清理的那一格)

`bin/release` 第 6 步 `gh release create` 在第 7 步回讀**之前**,而**全檔沒有任何 rollback**(沒有 `gh release delete`)。所以:

- **gate job 紅** ⇒ `auto-beta` 根本不跑 ⇒ 確實沒有 beta。
- **`auto-beta` 自己在 upload 之後失敗**(回讀某一項不符、runner 中途死、asset 只上傳一半——`gh release create` 那句 die 訊息本身就寫著「check GitHub for a partially created release」)⇒ **留下一顆沒通過回讀的 prerelease,沒有人回收它**。

嚴重度分兩種:存成 **draft** 的無害(`update_check.go` 的 admission 會把 draft 濾掉,站台看不到);**asset 缺失或不全**的**站台看得見**(admission 只看 draft/prerelease,不檢查 asset),它會是 semver 最大的那顆,`auto_update` 開著的站會挑上它然後下載失敗,直到下一次 merge 發出更大的 tag 才被蓋過去。

**人工清理**:確認 job 紅的原因在 upload 之後,然後
```
gh release view <tag> --repo pkyosx/OffiCraft --json assets,isDraft,isPrerelease
gh release delete <tag> --repo pkyosx/OffiCraft --cleanup-tag --yes
```
(`--cleanup-tag` 連 git tag 一起收,否則那個 tag 會留下來繼續參與版號計算與撞名。)確認清乾淨再重新 merge 或手動發版。

## 發佈簽章 —— 已整個移除(T-0398,owner 2026-07-31)

**這個 repo 不做 code signing,而且沒有任何開關可以打開它。** 原本的機制(`bin/codesign-artifact`、`bin/setup-codesign-cert`、`bin/build-release`、`bin/release publish --sign`、`OC_CODESIGN_*` 全套 env knob、`bin/tests/run.sh` 裡的 hermetic 簽章測試、`cli/ocwarden/selfupdate.go` 的 `signatureOf`/`codesignIdentity` 觀測路徑)在 T-0398 **全部刪除**——owner 拍板「全部拿掉,連手動簽章的逃生門一起刪」,所以不是預設關閉、不是留著等召回,是不在了。出貨的 binary 就是 `go build` 的產物(adhoc 簽章,cdhash 每 build 都變),`bin/release publish` 只有一種 builder。

**被抽掉之後什麼不再被守**:CI 原本有一道守簽章的檢查(`bin/tests/run.sh` 派出的,也就是今天的 `test-bin-guards` 項目),它守的是「預設路徑不碰共用 login keychain」「憑證檢查壞掉不可被讀成憑證不在」「`OC_CODESIGN_REQUIRE=1` 下絕不默默降級出 adhoc」。這些現在都不再被守——因為被守的東西整個不存在了,不是漏掉。要重新引入簽章,請當成一張新票、連守衛一起帶回來,不要只補一支腳本。

⚠️ **不要把這條跟 TCC 身分錨點搞混**:`dist/officraft/officraft`(launchd 的 responsible process)是**用 bytes 認身分**的,所以它 byte-pinned、`bin/check-officraft-dist` 在 `scan-tcc-anchor` 那個項目比對雜湊、裝過就永不覆寫。那套機制從來不依賴簽章憑證,**owner 核可保留,與本節無關**。

📌 **一份不隨簽章走的結論已搬家**:本節原本夾帶一份 repo 級的 shell 掃描結論(`pipefail` + 提前關 pipe 的消費者),它的主要案例當年是 `codesign-artifact`,但通則與逐檔判定跟簽章無關 ⇒ 見本文件最後一節〈shell 陷阱:`pipefail` 與提前關 pipe 的消費者〉。

⚠️ **兩句話都不要寫**:不要寫「不簽章的代價是 macOS 會重問 TCC 權限」,也不要寫「已證實 self-signed 對 TCC 完全無用」。self-signed codesign 對 macOS TCC 授權是否有效,**只是高度懷疑無效、從未有 100% 結論**(owner 2026-07-26;依據:在有簽章的那段期間他仍碰過不只一次授權詢問)。刪除的理由是**作業面的**:它卡測試又卡發佈。真的需要答案就在真機上量,拿證據回來。

## 安裝器內部

`bin/ocserver install` 的逐步細節（canonical layout、oc.toml 渲染、launchd plists、health check、首設啟用碼 banner、env override `OC_SERVER_ROOT` / `OC_SERVE_PORT` / `OC_CLOUDFLARED_CONFIG`）都寫在 `bin/ocserver` 檔頭註解與各 step 註解裡，那份是權威；tunnel 一律不代 provision，config + tunnel id + cloudflared binary 三者齊全才會掛 tunnel job。

## shell 陷阱:`pipefail` 與提前關 pipe 的消費者(T-da4b,已掃全 repo,結論=不動)

> **這一節與簽章無關,不要因為它的主要案例當年在 `codesign-artifact` 就把它一起丟掉。** 它原本寫在〈發佈簽章〉節裡,T-0398 移除簽章時搬到這裡並保留逐檔判定與通則——刪掉它的代價是下一個人碰到同一個構造時要重掃一次全 repo。

當年掃過全 repo **24 支**帶 `set -o pipefail` 的 shell script(其中 3 支——`codesign-artifact`／`setup-codesign-cert`／`build-release`——已於 T-0398 隨簽章刪除),逐一檢查 `| grep -q` / `| head` / `| sed -n Np` 這種「讀夠就關 pipe」的組合:writer 還在寫時消費者關掉 pipe,writer 吃 SIGPIPE(141),`set -o pipefail` 讓整條 pipeline 取 141。**結論是其餘一律不改**,因為判準不是「構造在不在」,而是**誤判往哪個方向倒**:

- **rc 根本沒被消費** → `| head -1 || true`(`bin/ocserver:103`)。`|| true` 吃掉 141,無害。(原本這裡還列了 `conformance/run.sh` 的 listener 查詢;**T-a3ba 已把它整段刪掉**——那行 `lsof … | head -1 || true` 換成「候選數 ≠ 1 就 FATAL」的 while-read 迴圈,`head -1` 的默默取第一個本來就是這張票要殺的東西,不再是本節的案例。)
- **倒向假紅(誤報失敗)** → `e2e_test/a1_zombie_e2e.sh:506/510/512`(`sed`/`head`/`tail | grep -qE`)、`e2e_test/tests_guard/run.sh`(`run_snippet` 的 `grep -q` 斷言)、`e2e_test/setup.sh:121`(`printf '%s' "$RESP" | py -c …`,T-a3ba 後 writer 是 builtin `printf` 而非 `curl`,窗更小)、`e2e_test/setup.sh:186`(登入取 token)。SIGPIPE → 141 → 測試紅／腳本中止。**吵,但不會騙人**,而且這些檔案 `bin/ci.sh` 只跑 tests_guard,其餘沒有活體證據可驗 —— 改了也證不了,是純 churn。
  > 行號是寫作當下的快照,會漂;以構造(不是行號)為準。上一版這兩行的行號在寫下時就已經對不上了。
- **倒向「看得見的 skip」**:**這是唯一另一個「檢查可能不執行」的方向**——`if <pipeline>` 守著一段可選的測試,141 讓它靜靜跳過而不是報錯。當年唯一的實例是 `bin/tests/run.sh` 裡 `openssl version | grep -q '^OpenSSL 3'` 守著一個 red control,而它**已隨簽章測試於 T-0398 一併刪除,repo 現在沒有這個構造的實例**。判準本身保留,因為方向分類是下面那條通則的一部分:當年它之所以可接受,是 else 分支會**印出 `skip — …`**、不是默默消失;再加上 `openssl version` 一口氣寫 ~25 bytes 就退出,窗開不起來。**新寫這種 guard 時要同時滿足這兩點。**
- **`echo "$VAR" | grep -q` 一律低risk**:writer 是 builtin、字串遠小於 64KB pipe buffer,grep 收到 EOF 前 write 早已完成。
- **通則(給後面的人):`pipefail` + 早關 pipe 只有在「141 會把某個 `if`/`if !` 翻成『壞事不存在』並讓流程默默往下走」時才是地雷。** 倒向紅、倒向可見 skip、rc 被 `|| true` 吃掉的,都不是同一種病。當年那個地雷是 `bin/codesign-artifact`(已刪):它是唯一一個誤判會**默默翻轉「出不出貨」**的點——`security find-identity | grep -Fq` 在第一行命中就關 pipe,憑證明明在卻被判成不在,於是默默降級出貨。修法是**把輸出整個收進變數再比對**(collect-then-compare),那支腳本已不存在,但**修法與判準對下一個同型構造仍然適用**。
