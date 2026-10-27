# 安裝、升級與移除

讀完這頁，你能在 Mac 上裝好 OffiCraft、之後升級或移除，把第二台 Mac 加進同一間工作室，以及從工作室移除一台機器。

安裝有兩條路：一般使用者用**官方 release**；要自己改程式碼的人才用**從原始碼安裝**。兩條路裝出來的東西不同，請只照其中一條做。

---

## 前置需求

### 裝 server 的那台 Mac

| 需求 | 說明 |
| --- | --- |
| **macOS，Apple Silicon** | 安裝腳本在其他平台會直接拒絕。 |
| **7755 埠沒有被占用** | 被占用時安裝會停下來，並提示改用其他埠。 |
| **`tmux`，以及 `claude` 或 `codex` 至少一種** | 即使這台只放 server、成員都跑在別台，安裝腳本也會檢查這兩項。缺少時會在動到任何檔案之前停下，並印出補裝的指令。 |

官方 release 是預先編好的程式，這台不需要 Go、node 或 python。

### 每一台要跑成員的機器

包含裝 server 的 Mac（如果也要在 server 的 Mac 上跑成員），以及之後加入的每一台：

| 需求 | 最低版本 | 說明 |
| --- | --- | --- |
| **`claude`（Claude Code CLI）或 `codex`（Codex CLI），至少一種** | Claude Code **2.1.287** | 每位成員底下就是一個 Claude Code 或 Codex session。只裝 `codex` 也可以安裝，但出廠的助理 Mira 設定的是 Claude Code，需要改設定才能喚醒，見 [常見問題與排解](troubleshooting.md)。安裝 Claude Code：`npm install -g @anthropic-ai/claude-code`。 |
| **`tmux`** | 3.0 | 成員的 session 跑在 tmux 裡。 |

runtime 需要登入才能喚醒成員。你可以在那台機器的終端機登入，也可以在控制台登入：到「監控」分頁的「機器資訊」，在那台機器 Claude 或 Codex 欄的版本號上選「登入」，照畫面指示完成。

安裝腳本只檢查 `claude` 或 `codex` 有沒有裝，不檢查版本。Claude Code 低於 2.1.287 時安裝照樣會成功，但 Claude 成員喚醒會失敗，「監控 › 機器資訊」的 Claude 欄會標示「版本太舊」。升級方法：在同一個版本號選單選「升級 Claude Code」，或在那台機器上執行 `npm install -g @anthropic-ai/claude-code`。

> [!NOTE]
> 用 asdf、nvm、volta 這類工具安裝 `claude` 時，背景服務可能找不到它。安裝腳本找不到 runtime 時，請用 `OC_CLAUDE_BIN=/絕對路徑/claude` 或 `OC_CODEX_BIN=/絕對路徑/codex` 指定位置後重跑安裝。重跑安裝不會影響既有資料。

---

## 方式一：官方 release（建議）

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
```

腳本會下載最新 release 的 `officraft-<tag>-darwin-arm64.tar.gz` 與 `checksums.txt`，sha256 驗證通過才安裝，驗證失敗就中止、不安裝任何東西。

也可以手動安裝：到 [GitHub Releases](https://github.com/pkyosx/OffiCraft/releases) 下載 `officraft-<tag>-darwin-arm64.tar.gz`，解開後執行 `./install.sh`。

### 安裝過程

1. 確認是 macOS Apple Silicon。
2. 確認這台機器有 `tmux`，以及 `claude` 或 `codex` 至少一種。
3. 這台已經裝過 OffiCraft 時，先警告並要求確認；用管線執行時沒辦法回答確認，需要加 `--force` 才會繼續。
4. 把 `ocserverd`、`ocwarden`、`ocagent`、`officraft` 裝到 `~/.officraft/bin`，並升級資料庫結構，既有資料保留。
5. 確認埠沒有被占用（預設 7755）。
6. 註冊背景服務 `com.officraft.serve`。服務在背景執行，關掉終端機也不會停，開機後自動啟動。
7. 印出一次性設定連結 `http://127.0.0.1:7755/?code=…`。用瀏覽器打開，設定你的登入密碼。
8. 密碼設好後，server 自動把這台機器的 warden 裝好，並喚醒助理 Mira。其中一步失敗時，控制台上方會出現「自動設定沒有全部完成」的橫幅並寫明原因。

裝好之後照 [你的第一個辦公室](quickstart.md) 走一遍。

### 常用選項

選項接在 `bash -s --` 後面，例如 `curl -fsSL … | bash -s -- --tag v0.4.1`。

| 選項 | 用途 |
| --- | --- |
| `--tag <版本>` | 安裝指定版本（也可以設環境變數 `OC_INSTALL_TAG`）。 |
| `--force` | 覆蓋既有安裝的檔案。 |
| `--restart-live` | 允許重啟正在執行中的 OffiCraft 服務。 |
| `--relocate` | 允許把既有服務搬到不同的埠或設定。 |
| `--namespace <名稱> --port <埠>` | 在同一台機器上裝第二個獨立的工作室。 |
| `--foreground` | 讓服務在目前的終端機前景執行，按 Ctrl-C 停止，不註冊背景服務。 |

> [!WARNING]
> 這台機器上的 OffiCraft 正在執行時，重裝會重啟服務。重啟期間所有開著的控制台與成員都會斷線，資料不會遺失。`--force` 只允許覆蓋檔案，不允許重啟；要重啟請同時加上 `--restart-live`：
>
> ```bash
> curl -fsSL … | bash -s -- --force --restart-live
> ```

### 同一台機器裝第二個工作室

```bash
./install.sh --namespace lab --port 7756
```

| 項目 | `--namespace lab` | 主工作室（不加 `--namespace`） |
| --- | --- | --- |
| 資料目錄 | `~/.officraft-lab` | `~/.officraft` |
| 背景服務 | `com.officraft.serve.lab` | `com.officraft.serve` |
| 設定檔 | `~/.officraft-lab/server/oc.toml` | `$OC_CONFIG` 或目前目錄的 `./oc.toml` |
| 資料庫 | `~/.officraft-lab/server/data/` | `~/.officraft/server/data/` |
| warden 背景服務 | `com.officraft.ocwarden.lab` | `com.officraft.ocwarden` |

- 名稱只能用小寫英文、數字與 `-`，最長 16 個字元，不符合就報錯中止。也可以用環境變數 `OC_NAMESPACE` 指定。
- 一定要加 `--port`，而且不能和主工作室同一個埠。
- 已經存在的工作室要換埠時，安裝腳本會拒絕；請直接修改該工作室的 `oc.toml`。
- 要隔離第二個工作室，請用 `--namespace`，不要用改 `$HOME` 的方式。改 `$HOME` 不會改變背景服務的名稱，重裝時仍會重啟主工作室的服務。

### 升級

升級不必重跑安裝腳本。到 **設定 › 系統更新與備份**：

- **目前版本**：這台 server 正在跑的版本，旁邊是「檢查更新」與「查看 release」。狀態寫「尚未確認最新版本」代表 server 啟動後還沒成功連上 GitHub 查過，不代表已是最新版。
- 按「檢查更新」，有新版時按「升級到最新版」。server 會下載新版、用 release 附的 sha256 檢查碼驗證、確認新程式能執行後才替換自己並重新啟動，控制台頁面會自動重新載入。
- 出現「檢查更新失敗」時，多半是連不到 GitHub（網路或 proxy 問題），稍後再試。
- **自動更新**：打開後，偵測到新版時會在背景自動升級並重新啟動。預設關閉。
- **接收 Beta 版本**：打開後，檢查更新時也會納入 GitHub 上的預發佈版本。預設關閉。
- **更新檢查間隔**：背景多久向 GitHub 確認一次，1–60 分鐘，預設 5 分鐘。按「檢查更新」與升級一律當場查，不受這一格影響。

server 升級後，各台機器上的 warden 與 ocagent 會自動跟著更新，不需要到每台機器上操作。新版的 warden 與 ocagent 包含在 server 裡；各台 warden 每 15 分鐘向 server 確認一次，重新連上 server 或收到 server 的指令時也會立即確認，發現新版就自我更新，通常在 15 分鐘內完成。

### 移除

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash -s -- --uninstall
```

預設是**停用並搬走，不刪除**：停掉背景服務，把這次安裝的程式、資料庫與背景服務設定搬到 `~/.officraft.bak-<時間>`。腳本會印出還原指令，照著執行就能恢復。

以下這些不是安裝腳本裝的，移除時不會動：

| 留在原處 | 說明 |
| --- | --- |
| `~/.officraft/agents/` | 這台機器上每位成員的工作區。 |
| `~/.officraft/warden/` | warden 與它自己的背景服務 `com.officraft.ocwarden`，移除後仍會繼續執行。 |
| `~/.officraft` 資料夾本身 | 不會被刪除。 |

要一併移除這台機器的 warden，有兩種做法：

- 移除 server 之前，在控制台「監控 › 機器資訊」的機器名稱選單選「解除安裝」。
- 在那台機器上執行 `~/.officraft/warden/ocwarden teardown --canonical`。第二個工作室的 warden 改用 `OC_NAMESPACE=<名稱> ~/.officraft-<名稱>/warden/ocwarden teardown`。

其他移除選項：

```bash
curl -fsSL … | bash -s -- --uninstall --dry-run          # 只列出會做什麼，不做任何變更
curl -fsSL … | bash -s -- --uninstall --purge --yes      # 永久刪除，含資料庫，無法復原
curl -fsSL … | bash -s -- --uninstall --namespace lab    # 只移除第二個工作室
```

用 `curl … | bash` 執行時，`--purge` 的確認提示無法輸入，必須同時加 `--yes` 才會刪除；沒有加 `--yes` 時什麼都不會刪。想要互動確認，請先把腳本存成檔案再執行。

判斷移除有沒有成功，請看腳本自己印出的完成訊息。舊版安裝腳本在提早結束時，最後可能多印一行 `curl: (23)` 或 `curl: (56)` 的紅字；那一行不代表移除失敗，仍以腳本印出的完成訊息為準。

---

## 方式二：從原始碼安裝

要自己修改 OffiCraft 程式碼的人才需要這條路。

額外需要：

| 需求 | 最低版本 | 用途 |
| --- | --- | --- |
| Go | 1.26 | 編譯 server、warden 與 CLI |
| node、npm | node 18（建議用目前的 LTS） | 編譯控制台前端 |
| python3 | 3.11 | 安裝時產生並檢查 `oc.toml`，以及開發用腳本；沒有 3.11 以上時 `bin/ocserver install` 會停下 |
| `cloudflared` | — | 只有要開對外 tunnel 時才需要 |

在 repo 根目錄執行：

```bash
bin/ocserver install
```

裝完會印出一次性的**啟用碼**。打開控制台，在「設定管理密碼」畫面貼上啟用碼並設定密碼。密碼設好後啟用碼立即失效。

```bash
bin/ocserver install --dry-run                       # 只列出會做什麼，不做任何變更
bin/ocserver install --force                         # 重跑每一步，不動既有密碼
bin/ocserver install --namespace <名稱> --port <埠>   # 同一台機器裝第二個工作室
```

預設埠同樣是 7755，可以在 `oc.toml` 或用環境變數 `OC_SERVE_PORT` 修改。

這條路的檔案都在 `~/.officraft/server/`：

```
~/.officraft/server/
  repo/     程式碼
  data/     資料庫（工作室的所有資料）
  oc.toml   本機設定（埠、資料庫位置），重裝不會覆蓋
  log/      執行紀錄
```

另外註冊三個開機自動啟動的背景服務：

| 背景服務 | 用途 |
| --- | --- |
| `com.officraft.serve` | server 本體 |
| `com.officraft.autodeploy` | 偵測 git 遠端有新程式碼時，自動拉取、編譯、重啟 |
| `com.officraft.tunnel` | cloudflared 對外通道；機器上沒有 cloudflared 設定時自動略過 |

`oc.toml` 接受 `[server]`（`port`、`namespace`）與 `[storage]`（`dsn`）。拼錯的欄位或不認得的區段會讓 server 拒絕啟動，避免設定默默失效。

移除：

```bash
bin/ocserver uninstall             # 移除背景服務與 ~/.officraft/server，保留 data/ 與 oc.toml
bin/ocserver uninstall --purge     # 全部刪除，含資料庫與密碼（會要求確認，加 --yes 跳過）
bin/ocserver uninstall --dry-run   # 只列出會做什麼，不做任何變更
```

---

## 加入或移除機器

### 加第二台機器

另一台 Mac 必須連得到這個 server。請先用那台 Mac 連得到的網址（tunnel 或私有網路轉發的網址）打開控制台，再複製安裝指令；用 `localhost` 打開時複製出來的指令，在別台機器上連不到。見 [保護你的工作室](security.md)。

1. 在控制台打開「監控」分頁，在「機器資訊」按「新增機器 / 上線」，輸入機器名稱後按「建立」。
2. 在新機器的名稱選單選「安裝」，在「安裝機器」視窗按「複製安裝指令」。
3. 到另一台 Mac 的終端機貼上指令並執行。
4. 安裝指令會先檢查 `tmux` 與 `curl`。缺 `tmux` 時，如果那台有 Homebrew，會自動安裝。
5. 裝好之後，那台機器出現在「機器資訊」的清單上，狀態變成「線上」。

那台機器同樣需要 `claude` 或 `codex` 至少一種並且已登入。之後在成員詳情裡就能把成員派到那台機器上跑，成員之間也能跨機器互相請託。

### 從控制台移除一台機器

到 監控 › 機器資訊，在機器名稱的選單選：

- **解除安裝**：請那台機器的 warden 移除自己。成功後機器變成離線，紀錄保留，之後可以再「安裝」。只有線上的機器可以解除安裝；那台機器上還有成員在線時，會先跳出警告，建議先讓那些成員停止。
- **刪除**：把機器從清單移除，它的憑證立即失效，在該機器上的成員也會斷線。機器上的程式不會被移除。刪除無法復原，要恢復只能重新安裝。

要完整移除一台機器，先解除安裝，再刪除。

---

## 相關文件

- [你的第一個辦公室](quickstart.md)
- [保護你的工作室](security.md)
- [設定與參數](settings.md)
- [常見問題與排解](troubleshooting.md)
