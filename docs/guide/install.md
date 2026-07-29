# 安裝、升級與移除

有兩條路：**官方 release**（一般使用者走這條）與**從原始碼**（開發機）。
兩條路的預設埠、落點、裝的 launchd job 都不一樣——這份文件分開講，不要混著讀。

---

## 前置需求

需求依**機器扮演的角色**分三組，別一股腦全裝——跑官方 release 的一般使用者只碰得到前兩組，第三組是走原始碼／開發那條路才要的。

### A. server 那台一定要的

跑 `ocserverd`（唯一真相源）的那台主機：

| 需求 | 最低版本 | 為什麼 |
| --- | --- | --- |
| **macOS + Apple Silicon**（darwin/arm64） | — | 安裝腳本第一道就擋，其他平台直接拒絕 |
| **7755 埠是空的** | — | 被占用時安裝當場失敗並提示換埠，不會裝一個起不來的服務 |

官方 release 那條路，server 這台**平台與埠只有這兩項**——binary 是預編好的，不需要 Go / node / python3。

> [!IMPORTANT]
> **但安裝腳本另外硬性要求下面 B 組的東西**（T-7f38）：**`tmux` 一定要有**，而且 **`claude` 與 `codex` 至少要有一種**。缺就**在動任何東西之前停下**（沒有 `~/.officraft`、沒有 launchd plist），並印出補齊的指令。
> 原因是缺 `tmux` 的機器以前會**整趟綠燈**——控制台打得開、成員請得到——然後成員永遠停在「waking」，而**沒有任何一處訊號指向真正的原因**。
> `tmux` 沒有 runtime 之分：claude 與 codex 兩條啟動線都走同一個 `tmuxNewSession`（`cli/ocwarden/spawn.go:950`）。runtime 則是**二擇一**，跟 `ocwarden install` 的 `runtime_bin_unresolved` 同一條規則——**只裝 codex 的機器是合法配置，不會被擋**。
> 代價要講清楚：**一台只當 server、成員都跑在別台**的機器，現在也要先備妥 `tmux` 與其中一種 runtime 才裝得起來。**沒有跳過的開關**（給了就等於教人回到那個沉默失敗）。

### B. 任何要跑成員的機器一定要的

**每一台**要 spawn 成員的機器（包含 server 那台，只要你也要它起成員；還有你之後加的每一台）都要有這兩個——成員底下就是一個跑在 tmux 裡的 Claude Code session：

| 需求 | 最低版本 | 為什麼 |
| --- | --- | --- |
| **Claude Code CLI**（`claude`，而且已登入） | **2.1.98 以上**（必須新到內建 **Monitor** tool） | 每位成員底下就是一個 Claude Code session。**claude 與 codex 兩種都解析不到時，安裝腳本當場拒絕、warden 也拒絕安裝**（兩層 fail-closed，並在控制台橫幅說明原因），不會裝一個永遠起不了成員的 warden。只裝 codex 不會被擋。裝法：`npm install -g @anthropic-ai/claude-code` |
| **`tmux`** | 3.0 以上（任何近代 3.x 都行） | 成員的 session 跑在 tmux 裡（`cli/ocwarden/spawn.go` 的 `tmux new-session`，沒有備援）。**解析不到時安裝腳本直接拒絕**，不會裝出一台成員永遠停在「waking」的機器 |

> [!IMPORTANT]
> **`claude` 一定要新到內建 Monitor tool（2.1.98 起）。** 成員靠 **Monitor** 這個內建工具持住 `ocagent listen`
> 那條到 server 的 SSE 長連線——**持著連線＝online**（見 [架構與運作原理](architecture.md)）。`claude` 太舊、沒有
> Monitor tool，成員就掛不住那條連線、**永遠亮不起來**（Waking 卡住或一直 Offline）。升級：`npm install -g @anthropic-ai/claude-code`。
>
> 注意：**安裝器擋「沒裝」，但不擋「太舊」**——它確認 `tmux` 與（claude／codex 至少一種）解析得到（缺就停），但**不比對版本號**。所以「2.1.98 以上」是**你要自己確保**的前提，不是安裝當下會替你把關的東西；裝了太舊的 `claude`，安裝照樣過，但成員之後亮不起來。

> [!NOTE]
> 用 asdf / nvm / volta 裝 `claude` 的人要注意：launchd 的 PATH 很小，找不到 shim。
> 安裝時會解析 Claude Code 與 Codex CLI，找到的路徑都會寫進服務設定；機器只要至少有一種 runtime 即可。真的找不到時，用 `OC_CLAUDE_BIN=/絕對路徑/claude` 或 `OC_CODEX_BIN=/絕對路徑/codex` 重跑安裝（冪等）。

### C. 只有走「從原始碼／開發」那條才需要的

官方 release 是預編 binary，**不需要**這一組；只有用下面的**方式二（從原始碼）**自己 build 才要：

| 需求 | 最低版本 | 為什麼 |
| --- | --- | --- |
| **Go** | 1.26 以上 | 從原始碼 build `ocserverd` / `ocwarden` / `ocagent` |
| **node + npm** | node 18 以上（建議 20 LTS 以上），npm 隨 node 附帶 | build 前端控制台 |
| **python3** | 3.11 以上 | 開發用腳本 |
| **`cloudflared`** | — | **只有**要開對外 tunnel 才需要；不開就不裝 |

> [!NOTE]
> 版本依據：**Go 1.26** 取自 repo 的 `go.mod`（`go 1.26.4`）；**node 18** 是前端工具鏈（Vite 5）的下限，
> 建議跟上現行 LTS；**Claude Code 2.1.98** 是 Monitor tool 首度內建的版本（沒有它成員亮不起來，見上）；
> **tmux 3.0** 是保守下限，repo 未硬性指定版本，任何近代 3.x 都可以。

---

## 方式一：官方 release（建議）

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
```

它會抓最新 release 的 `officraft-<tag>-darwin-arm64.tar.gz` 與 `checksums.txt`，
**sha256 驗證通過才安裝**（驗證失敗直接中止，什麼都不裝），解到暫存目錄後委派包內的 install.sh。

也可以手動：到 [GitHub Releases](https://github.com/pkyosx/OffiCraft/releases) 下載
`officraft-<tag>-darwin-arm64.tar.gz`，解開後跑 `./install.sh`。

### 它做了什麼（照順序）

1. **平台閘** — 只支援 macOS Apple Silicon，其他平台直接拒絕。
2. **既有安裝閘** — `~/.officraft/bin` 有 binary 或已有資料庫時大聲警告並要求確認（互動 y/N 預設否；非互動要 `--force`），否則中止、什麼都不動。
3. **現役服務閘** — 見下方警告。
4. **裝 binary + 資料庫升級** — `ocserverd` / `ocwarden` / `ocagent` 落到 `~/.officraft/bin`，跑 migration（資料保留、原地升級）。
5. **埠閘** — 預設 **7755**（`$OC_CONFIG` 或當前目錄的 `./oc.toml` 可覆蓋）。
6. **註冊背景服務** — launchd job `com.officraft.serve`（`RunAtLoad` / `KeepAlive`，log 落在 `~/.officraft/server/log/serve.log`）。**不佔用你的終端機，關掉也不會停。**
7. **印出一次性設定連結** — `http://127.0.0.1:7755/?code=…`，打開它設定 owner 密碼。
8. **設完密碼後 server 自己接手最後兩步** — 把這台機器的 warden 裝好、把預設助理 **Mira** 叫醒。你不用自己去機器頁按安裝，也不用自己把助理設成上線。
   缺 `claude` 或 `tmux` 時它會**明確失敗並說出原因**（顯示在控制台上方的橫幅），而不是裝一個永遠起不了 agent 的 warden。
9. **一次性的 macOS 隱私授權**（見下）。

### 每台機器一次：授權 TCC identity anchor

warden 的 launchd job 啟動的是 `~/.officraft/warden/ocanchor` —— ocwarden 的一份**凍結副本**，它再去 fork 真正的
ocwarden。macOS 把隱私權限綁在 launchd 啟動的那支 binary 上，而 ocwarden 會自我更新；讓 job 直接指向它，等於**每次
發版都把這台機器的授權作廢一次**。anchor 只在第一次安裝時寫入、之後永不覆寫，所以授權給它就會一直有效。

安裝時若寫入了新的 anchor，畫面會印出這段。做一次：

> 系統設定 → 隱私權與安全性 → **完全取用磁碟** → `+` → 按 <kbd>⌘⇧G</kbd> 貼上 `~/.officraft/warden/ocanchor`

⚠️ **沒授權之前，agent 碰到受保護目錄（`~/Documents` 之類）是「卡住」不是「失敗」。** macOS 對認不得的來源會
跳授權提示，而 launchd job 沒有 GUI 可以顯示它，於是那個檔案開啟呼叫永遠不返回 —— 症狀是 agent 一個字都不吐、
任何 log 都沒有東西。看到這個症狀，先確認這一步做了沒有。

驗證方式（在 warden 的 tmux 裡跑，秒回就是好了）：

```bash
tmux -L officraft new-session -d 'ls ~/Documents && echo OK > /tmp/anchor-ok'
```

### 常用選項

| | |
| --- | --- |
| `bash -s -- --tag v0.4.1` | 裝特定版本（或用 `OC_INSTALL_TAG=v0.4.1`） |
| `bash -s -- --force` | 覆蓋既有安裝（**只授權覆寫檔案**） |
| `bash -s -- --restart-live` | 額外授權「重啟一個正在服務的實例」，見下 |
| `./install.sh --foreground` | 開發者模式：serve 跑在這個終端機裡，Ctrl-C 停止，**不裝任何 launchd job** |
| `./install.sh --relocate` | 同意把既有服務搬到不同的埠或設定 |
| `./install.sh --namespace <ns> --port <port>` | 在同一台機器上裝**第二個完全隔離的實例**，見下 |

> [!WARNING]
> **這台機器上已經有一個 OffiCraft 服務正在跑的話**，重裝會把它 bootout 再 bootstrap——
> 那是一次**真正的重啟**，期間所有開著的控制台與連著的 agent 都會斷線（埠與資料庫會被繼承，
> **資料不會掉，掉的是連線**）。
>
> 這件事需要它自己的同意：`--force` 只講「覆寫檔案」，**不**授權斷線。管線安裝沒有 tty 可問，
> 所以會直接中止。要接受重啟請明確加上：
>
> ```bash
> curl -fsSL … | bash -s -- --force --restart-live
> ```
>
> 不想動到現役服務的話，用 `--namespace` 併裝（見下一節）。
>
> ⚠️ **不要用改 `$HOME` 的方式當隔離**。launchd label 是這個 uid 的 GUI domain 裡的
> singleton，**它不跟著 `HOME` 走**：換了 `HOME` 只是把檔案搬走，job 目標仍然解析到
> 同一個正在服役的服務，於是「我以為我在沙箱裡」的那次執行照樣把它 bootout。
> 只設 `OC_LAUNCHD_LABEL` 也只解決一半——job 換了，檔案還是寫進第一個實例的
> `~/.officraft`。要隔離就用 `--namespace`，它一把鑰匙同時決定 label 與 root。

### 同機第二個實例（`--namespace`）

要在同一台機器上再跑一個互不干擾的 OffiCraft，用一個名字把每一項 per-instance
資源一次決定完：

```bash
./install.sh --namespace lab --port 7756
```

| 這個名字決定了 | `--namespace lab` | 主實例（不給 `--namespace`） |
| --- | --- | --- |
| 資料根目錄 | `~/.officraft-lab` | `~/.officraft` |
| launchd label | `com.officraft.serve.lab` | `com.officraft.serve` |
| 設定檔 | `~/.officraft-lab/server/oc.toml`（安裝時寫入） | `$OC_CONFIG` 或 `./oc.toml` |
| 資料庫 | `~/.officraft-lab/server/data/` | `~/.officraft/server/data/` |
| 它裝出來的 warden | `com.officraft.ocwarden.lab` | `com.officraft.ocwarden` |

規則：

- `<ns>` 必須符合 `[a-z0-9-]{1,16}`。**不合法就直接報錯中止**——絕不會默默退回主實例的
  路徑與 label（那比報錯糟得多）。也可以用 `OC_NAMESPACE=<ns>` 給。
- **`--port` 是必填的**。預設的 7755 是主實例的埠，沿用它不是撞在埠檢查上，就是更糟——
  看起來裝好了，實際上兩個實例在搶同一個 socket。
- 具名安裝**刻意忽略** `$OC_CONFIG` 與 `./oc.toml`：你已經指名要哪一個實例，
  站在哪個目錄裡不該有權改道。
- 已存在的實例改埠是**搬遷、不是重載**，會被擋下且什麼都不動——要嘛沿用原埠重跑，
  要嘛自己去改那個實例的 `oc.toml`。
- 移除也吃同一把鑰匙：`--uninstall --namespace lab` 只動 `~/.officraft-lab` 與
  `com.officraft.serve.lab`；不給就是照舊處理主實例。

### 升級

**之後的升級不必重跑 install.sh。** 設定 › 軟體更新 有「檢查更新」與一鍵升級
（從 GitHub Releases 下載、sha256 驗證後原地抽換重啟）；打開「自動更新」則在背景自動升級。
「接收 Beta」= 也吃 GitHub prerelease。

### 移除

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash -s -- --uninstall
```

**預設 = 停用 + 搬走，不是刪除**：停掉 launchd job，把 release 路徑裝的那一份
（`bin/` 與 `server/` 裡屬於這次安裝的資料，含資料庫）連同那個 launchd plist 一起搬到
`~/.officraft.bak-<timestamp>`，不刪。最壞情況是「東西還在，只是不跑了」，不是「資料沒了」。
腳本會印出一行還原指令，**檔案與 launchd 註冊兩邊都會回來**（plist 留在備份裡就是為了這個）。
這一步**不需要下載任何 tarball**——安裝器偵測到 `--uninstall` 會直接短路進移除邏輯。

**它不會碰什麼**（這些都不是這支安裝器裝的，所以移除也不該動它們）：

| 留在原地 | 為什麼 |
|---|---|
| `~/.officraft/agents/` | 這台機器上**每一個 agent 的工作區**。安裝器從未建立它們 |
| `~/.officraft/warden/` | ocwarden 常駐程式的目錄。它有**自己的** launchd job（`com.officraft.ocwarden`），移除後仍**保持註冊與執行**——要一併移除請用 `ocwarden teardown --canonical`（具名實例是 `OC_NAMESPACE=<ns> ocwarden teardown`；teardown 拒絕隱含目標，裸指令會 exit 1） |
| `~/.officraft/server/repo/` | 方式二（從原始碼）裝的那一份，跟這條路共用同一個 `server` 根目錄 |
| `~/.officraft` 本身 | 永遠不會被移除 |

腳本執行時會把上面這份清單連同 agent 工作區的數量一起印出來，`--dry-run` 也印同一份。

> ⚠️ **舊版行為（v0.5.12 及更早，也就是這個修正發佈之前的每一版）**：只要 `~/.officraft/server/repo/` 不存在
> （也就是這台機器**沒有**用方式二從原始碼裝過——release 安裝的機器都是這樣），預設就會把
> **整個** `~/.officraft` 搬走，含 `agents/` 與 `warden/`，而訊息只說 "nothing was deleted"、
> 一個字都沒提。反而是有 `repo/` 的機器不受影響。那是可回復的（是 `mv` 不是 `rm`），
> 但訊息不足以讓人預判後果。**如果你手上跑的是舊版安裝腳本，這件事仍然成立。**

```bash
curl -fsSL … | bash -s -- --uninstall --dry-run   # 只印出會做什麼，什麼都不動
curl -fsSL … | bash -s -- --uninstall --purge --yes  # 真的刪光，含資料庫，不可回復
curl -fsSL … | bash -s -- --uninstall --namespace lab  # 只移除具名的那個實例
```

> ℹ️ **舊版會在最後多印一行紅字，那不是失敗。** 在這個修正發佈之前，用
> `curl … | bash` 跑**提早結束**的動作（`--uninstall`、`--help`、「已經很乾淨」）
> 常常會在自己的輸出之後，再被 curl 補上一行
> `curl: (23|56) Failure writing output to destination, passed N returned 0`。
> 原因與移除本身無關：**腳本在 curl 還沒送完的時候就結束了**，curl 的下一次寫入因此失敗。
> （不是「檔案太大」——實測一個明顯小於管線緩衝區的舊版本，在慢速連線下反而每次都重現；
> 決定性的條件是「讀的那一端先走了，而寫的那一端還有東西沒送完」。）
> **判斷成功與否要看腳本自己印的完成行**（例如 `DRYRUN complete — nothing on the machine was changed`），
> 不是看最後一行有沒有紅字。
> 這個修正發佈之後，腳本會在結束前把剩下的內容收乾淨，正常網速下這行就不會再出現。
> **但它有上限**：收乾淨這件事最多等一小段時間，連線**非常**慢的時候（獨立複驗在這台機器上量到
> 大約每秒 3–4 KB 之間是分界，每秒 4 KB 以上都乾淨）腳本不會為此一直等下去，那行還是會回來
> ——它依然只是雜訊，判斷成功與否的方式不變。
> **另外，如果你手上跑的是舊版安裝腳本（包含目前 `releases/latest` 上的那一版），上面整段都仍然成立。**

> ⚠️ **`--purge` 的打字確認在 `curl … | bash` 這個形式下按不下去。** 那個提示讀的是標準輸入，
> 而標準輸入正被用來把腳本本身餵給 bash，所以你打的字送不進去、確認必定失敗 ——
> 結果是**什麼都不會被刪**（fail-closed，而且畫面會印出中止訊息）。
> 也就是說：走管線時 `--purge` 一定要配 `--yes` 才有作用。想要互動確認就先把腳本存成檔案再跑。
> （這是已知限制，另案處理；在這裡寫出來是因為「以為有一道確認、其實按不到」本身就會誤導。）

**所有權判斷**：只認「這支安裝器裝的那一套」（比對 launchd plist 的 `ProgramArguments[0]`
與 release-path 專屬檔案），尊重 `OC_LAUNCHD_LABEL`（用 alt label 併裝的那一套，移除時也要
帶同一個 label）。判斷不出是自己裝的就拒絕移除並說明原因，不會誤刪別人的東西。

---

## 方式二：從原始碼（開發機）

在 repo 根目錄跑：

```bash
bin/ocserver install
```

裝完會印出一格 banner，裡面有一次性的**啟用碼**：打開瀏覽器、貼上啟用碼、設定 owner 密碼。
密碼一設好啟用碼立即失效；重裝（密碼已設）不會再印任何憑證。

```bash
bin/ocserver install --dry-run  # 只印出打算做什麼，什麼都不動
bin/ocserver install --force    # 重跑每一步（不動既有密碼）
```

**這條路額外需要**前置需求的 **C 組**（Go 1.26+、node 18+／npm、python3 3.11+；`cloudflared` 只有要開 tunnel 才需要）——版本與依據見上面〔前置需求〕。

> [!NOTE]
> **標準埠是 7755，兩條路一樣。** 兩者都可以用 `oc.toml`（或 `OC_SERVE_PORT`）覆蓋。

### 這條路裝完機器上多了什麼

所有東西都落在 `~/.officraft/server/`：

```
~/.officraft/server/
  repo/     server 程式碼（autodeploy 會自己 pull + 重建）
  data/     SQLite 資料庫（你的所有資料都在這）
  oc.toml   本機設定（port、DSN；重裝不會被覆蓋。密碼不在這——DB 只存雜湊）
  log/      執行 log
```

外加三個 launchd job（`~/Library/LaunchAgents/`，開機自動起）：

| job | 做什麼 |
| --- | --- |
| `com.officraft.serve` | server 本體（預設 port 7755，`oc.toml` 可改，只綁 loopback） |
| `com.officraft.autodeploy` | 盯 git 遠端，有新 code 自動 pull → build → 重啟 |
| `com.officraft.tunnel` | cloudflared 對外通道（**選用**：機器上沒有 cloudflared 設定就自動略過） |

**除此之外不碰你機器上任何其他東西。**

### `oc.toml` 的設定範圍

OffiCraft 接受 `[server]`（`port`、`namespace`）與 `[storage]`（`dsn`）的既定設定；舊版的 `[auth]`、`[server].host`、`[sse_context_high]` 與 `[storage].database_url` 仍可讀取以便遷移，但會提示你移除。其他拼錯欄位或未知區域會拒絕啟動，避免默默套用預設值。若是由擴充功能管理的設定，請明確放在 `[extensions]` 下，例如：

```toml
[extensions.example]
enabled = true
```

> release tarball 那條路只落 `~/.officraft/bin` + 資料庫，並註冊 `com.officraft.serve`
> 一個 job——**不裝 autodeploy 與 tunnel**，那兩個是原始碼路徑才有的。

### 移除

```bash
bin/ocserver uninstall             # 卸掉三個 launchd job、刪 ~/.officraft/server
                                   # 但保留 data/（資料庫，含密碼雜湊）與 oc.toml——之後重裝即回原狀
bin/ocserver uninstall --purge     # 全部刪光，含資料庫與密碼（會要求輸入確認；--yes 跳過）
bin/ocserver uninstall --dry-run   # 只印出會做什麼，什麼都不動
```

---

## 加第二台機器

控制台的 **監控 › 機器** 會給你一行指令，貼到另一台 Mac 上跑，那台就加入同一間工作室了。
那支腳本會先檢查 `tmux` 與 `curl`——**`tmux` 缺的話，只要那台有 Homebrew 就會自動幫你裝**。

之後成員可以被派到那台機器上跑，成員之間也能跨機器互相請託。

---

裝好之後想立刻走一遍，看 [你的第一個辦公室](quickstart.md)。安裝或起不來時，先翻 [常見問題與排解](troubleshooting.md)——埠被占用、缺 `claude`/`tmux`、成員亮不起來、移除的怪紅字都在那裡。

---

這兩份是給改 code 的人的開發者文件，不隨產品打包，所以只在 GitHub 上讀得到：

**agent 的環境變數怎麼設** → [docs/dev/agent-env.md](https://github.com/pkyosx/OffiCraft/blob/main/docs/dev/agent-env.md)
**技術棧、repo 結構、怎麼跑測試與 CI** → [docs/dev/](https://github.com/pkyosx/OffiCraft/tree/main/docs/dev)
