# 安裝、升級與移除

有兩條路：**官方 release**（一般使用者走這條）與**從原始碼**（開發機）。
兩條路的預設埠、落點、裝的 launchd job 都不一樣——這份文件分開講，不要混著讀。

---

## 前置需求

**一定要有**

| | 為什麼 |
| --- | --- |
| **macOS + Apple Silicon**（darwin/arm64） | 安裝腳本第一道就擋，其他平台直接拒絕 |
| **7755 埠是空的** | 被占用時安裝當場失敗並提示換埠，不會裝一個起不來的服務 |

**沒有它工作室就是空的**

| | 為什麼 |
| --- | --- |
| **Claude Code CLI**（`claude`，而且已登入） | 每位成員底下就是一個 Claude Code session。**解析不到 `claude` 時，warden 會直接拒絕安裝**（fail-closed，並在座艙橫幅說明原因），不會裝一個永遠起不了成員的 warden。裝法：`npm install -g @anthropic-ai/claude-code` |
| **`tmux`** | 成員的 session 跑在 tmux 裡 |

> [!NOTE]
> 用 asdf / nvm / volta 裝 `claude` 的人要注意：launchd 的 PATH 很小，找不到 shim。
> 安裝時解析到的路徑會被寫進服務設定；真的找不到時用 `OC_CLAUDE_BIN=/絕對路徑/claude` 重跑安裝（冪等）。

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
   缺 `claude` 或 `tmux` 時它會**明確失敗並說出原因**（顯示在座艙上方的橫幅），而不是裝一個永遠起不了 agent 的 warden。

### 常用選項

| | |
| --- | --- |
| `bash -s -- --tag v0.4.1` | 裝特定版本（或用 `OC_INSTALL_TAG=v0.4.1`） |
| `bash -s -- --force` | 覆蓋既有安裝（**只授權覆寫檔案**） |
| `bash -s -- --restart-live` | 額外授權「重啟一個正在服務的實例」，見下 |
| `./install.sh --foreground` | 開發者模式：serve 跑在這個終端機裡，Ctrl-C 停止，**不裝任何 launchd job** |
| `./install.sh --relocate` | 同意把既有服務搬到不同的埠或設定 |

> [!WARNING]
> **這台機器上已經有一個 OffiCraft 服務正在跑的話**，重裝會把它 bootout 再 bootstrap——
> 那是一次**真正的重啟**，期間所有開著的座艙與連著的 agent 都會斷線（埠與資料庫會被繼承，
> **資料不會掉，掉的是連線**）。
>
> 這件事需要它自己的同意：`--force` 只講「覆寫檔案」，**不**授權斷線。管線安裝沒有 tty 可問，
> 所以會直接中止。要接受重啟請明確加上：
>
> ```bash
> curl -fsSL … | bash -s -- --force --restart-live
> ```
>
> 不想動到現役服務的話，可以用不同 label 併裝：
> `OC_LAUNCHD_LABEL=com.officraft.serve.alt ./install.sh --force`

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
| `~/.officraft/warden/` | ocwarden 常駐程式的目錄。它有**自己的** launchd job（`com.officraft.ocwarden`），移除後仍**保持註冊與執行**——要一併移除請用 `ocwarden teardown` |
| `~/.officraft/server/repo/` | 方式二（從原始碼）裝的那一份，跟這條路共用同一個 `server` 根目錄 |
| `~/.officraft` 本身 | 永遠不會被移除 |

腳本執行時會把上面這份清單連同 agent 工作區的數量一起印出來，`--dry-run` 也印同一份。

> ⚠️ **舊版行為（v0.5.11 及更早）**：只要 `~/.officraft/server/repo/` 不存在
> （也就是這台機器**沒有**用方式二從原始碼裝過——release 安裝的機器都是這樣），預設就會把
> **整個** `~/.officraft` 搬走，含 `agents/` 與 `warden/`，而訊息只說 "nothing was deleted"、
> 一個字都沒提。反而是有 `repo/` 的機器不受影響。那是可回復的（是 `mv` 不是 `rm`），
> 但訊息不足以讓人預判後果。**如果你手上跑的是舊版安裝腳本，這件事仍然成立。**

```bash
curl -fsSL … | bash -s -- --uninstall --dry-run   # 只印出會做什麼，什麼都不動
curl -fsSL … | bash -s -- --uninstall --purge --yes  # 真的刪光，含資料庫，不可回復
```

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

**這條路額外需要**：Go、node/npm、python3 ≥ 3.11；`cloudflared` 只有要開 tunnel 才需要。

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

座艙的 **監控 › 機器** 會給你一行指令，貼到另一台 Mac 上跑，那台就加入同一間工作室了。
那支腳本會先檢查 `tmux` 與 `curl`——**`tmux` 缺的話，只要那台有 Homebrew 就會自動幫你裝**。

之後成員可以被派到那台機器上跑，成員之間也能跨機器互相請託。

---

**agent 的環境變數怎麼設** → [../dev/agent-env.md](../dev/agent-env.md)
**技術棧、repo 結構、怎麼跑測試與 CI** → [../dev/](../dev/)
