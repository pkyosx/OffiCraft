# 安裝、升級與移除

這頁說明在 Mac 上安裝、升級、移除 OffiCraft 的指令，以及把第二台 Mac 加進同一間工作室、從工作室移除一台機器時，各個操作的意思和結果。

本頁的指令都在 Mac 內建的「終端機」App 裡執行。「進階」開頭的幾節說明非預設的安裝方式與選項。

---

## 安裝前要準備的東西

| 需要 | 說明 |
| --- | --- |
| **Apple Silicon（M 系列晶片）的 Mac** | 其他電腦在安裝一開始就會被拒絕。 |
| **`tmux` 3.0 以上** | 讓成員在背景持續工作的終端機工具。 |
| **Claude Code 或 Codex，至少一種** | 每位成員背後就是一個 Claude Code 或 Codex。Claude Code 需要 **2.1.287** 以上。 |

補裝缺少項目的指令如下。第一行裝 `tmux`，需要先有 Homebrew（macOS 上常用的軟體安裝工具）；第二行裝 Claude Code，需要先有 Node.js（`npm` 隨 Node.js 一起安裝）：

```bash
brew install tmux
npm install -g @anthropic-ai/claude-code
```

**Claude Code 或 Codex 要先登入，成員才能工作。** 登入可以在終端機完成，也可以在裝好 OffiCraft 之後從控制台完成：控制台「監控」分頁的「機器資訊」裡，每台 Mac 的 Claude 欄與 Codex 欄的版本號選單裡有「登入」。

只裝 Codex 也可以：Codex 登入後，助理 Mira 和新成員第一次啟動時會自動改用 Codex。兩種都裝時用 Claude Code。Mira 起不來時，見 [常見問題與排解](troubleshooting.md) 的「機器只裝了 Codex，Mira 或新成員起不來」。

---

## 安裝

安裝指令：

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
```

這一行會下載最新版的 OffiCraft，確認檔案完整無誤後才安裝；檔案有問題就中止，不會裝任何東西。安裝時會先檢查這台 Mac 符不符合需求，缺東西就停下來，並印出要補裝什麼。

安裝成功時，終端機最後會印出一行設定連結，開頭是 `http://127.0.0.1:7755/?code=`（`127.0.0.1` 指這台 Mac 本身，`7755` 是控制台使用的連線埠號）。在瀏覽器打開這條連結的頁面，是用來設定登入密碼的。

密碼設好後，系統會自動把這台 Mac 準備好，並喚醒助理 Mira。OffiCraft 會在背景一直執行，關掉終端機也不會停，電腦重開後也會自動啟動。

安裝完成之後的第一次使用，見 [你的第一個辦公室](quickstart.md)。

安裝失敗時，終端機會印出原因，各種原因的說明見 [常見問題與排解](troubleshooting.md) 的「安裝」。最常見的原因是 7755 埠已經被其他程式占用。

---

## 升級

升級在控制台的 **設定 › 系統更新與備份** 進行，不需要再執行安裝指令。

- **檢查更新**：向 GitHub（OffiCraft 發佈新版本的網站）查詢有沒有新版。有新版時出現「升級到最新版」。
- **升級到最新版**：OffiCraft 下載新版、確認檔案完整，再自動重新啟動。控制台頁面會自動重新載入，成功後「目前版本」會換成新的版本號。
- **自動更新**：打開時，OffiCraft 偵測到新版就自動升級。預設關閉。

其他 Mac 上的 OffiCraft 會自動跟著更新，通常在 15 分鐘內完成，不需要到每台 Mac 上操作。

出現「檢查更新失敗」時，多半是連不到 GitHub，原因通常是網路或 proxy（公司網路常見的代理伺服器）。

「接收 Beta 版本」「更新檢查間隔」等每一格的意思，見「進階：升級的設定」。

---

## 移除

移除指令：

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash -s -- --uninstall
```

這一行會停掉 OffiCraft，把程式、資料庫與背景服務的設定搬到 `~/.officraft.bak-<時間>` 資料夾。**預設不會刪除任何資料。** 腳本最後會印出還原指令，執行那條還原指令就能恢復。

移除是否成功，以腳本最後印出的完成訊息為準。

移除時不會動到成員的工作區（`~/.officraft/agents/`），也不會移除這台 Mac 上的 warden（負責啟動、停止成員的背景程式）。連 warden 一起移除、或永久刪除資料的選項，見「進階：移除的其他選項」。

---

## 加入或移除機器

### 加第二台機器

**另一台 Mac 要連得到這台 Mac 上的控制台。** 控制台複製出來的安裝指令，帶的是打開控制台時所用的網址。用 `localhost`（指「這台電腦本身」的網址）打開控制台時，複製出來的指令在別台 Mac 上會連回那台 Mac 自己，連不到這台的控制台；要用另一台 Mac 連得到的網址（例如轉發後的網址）打開控制台，複製出來的指令才能用。轉發的方式見 [在手機上用](mobile.md)，開放前要做的保護見 [保護你的工作室](security.md)。

加入機器用到的畫面與指令：

- **新增機器 / 上線**：在「監控」分頁的「機器資訊」，輸入機器名稱建立一筆機器紀錄。剛建立的機器還沒有裝任何東西。
- **安裝**：機器名稱選單裡的「安裝」會打開「安裝機器」視窗，視窗裡的「複製安裝指令」複製的是給這台機器專用的安裝指令。
- **安裝指令**：在另一台 Mac 的終端機執行。指令會先檢查 `tmux` 與 `curl`（macOS 內建的下載工具）；缺 `tmux` 時，如果那台 Mac 有 Homebrew，會自動安裝。裝好之後，那台機器出現在「機器資訊」的清單上，狀態是「線上」。

那台 Mac 同樣要符合「安裝前要準備的東西」表格裡的版本需求（`tmux` 3.0 以上；Claude Code 2.1.287 以上，或改用 Codex），並且已經登入。機器上線後，成員詳情裡就能把成員派到那台 Mac 上工作，成員之間也能跨機器互相幫忙。

### 從控制台移除一台機器

監控 › 機器資訊 裡，機器名稱的選單有兩個移除選項：

- **解除安裝**：讓那台機器移除自己身上的 OffiCraft。成功後機器變成離線，紀錄保留，之後可以再「安裝」。只有線上的機器可以解除安裝；那台機器上還有成員在線時，會先跳出警告。
- **刪除**：把機器從清單移除，那台機器的登入憑證立即失效，在那台機器上的成員也會斷線。機器上的程式不會被移除。**刪除無法復原**，要恢復只能重新安裝。

完整移除一台機器需要兩者都做，順序是先解除安裝、再刪除。

---

## 進階：安裝選項

選項接在 `bash -s --` 後面，例如 `curl -fsSL … | bash -s -- --tag v0.4.1`。

| 選項 | 用途 |
| --- | --- |
| `--tag <版本>` | 安裝指定版本（也可以設環境變數 `OC_INSTALL_TAG`）。 |
| `--force` | 覆蓋既有安裝的檔案。 |
| `--restart-live` | 允許重啟正在執行中的 OffiCraft 服務。 |
| `--relocate` | 允許把既有服務搬到不同的埠或設定。 |
| `--namespace <名稱> --port <埠>` | 在同一台 Mac 上裝第二個獨立的工作室。 |
| `--foreground` | 讓服務在目前的終端機前景執行，按 Ctrl-C 停止，不註冊背景服務。 |

> [!WARNING]
> 這台 Mac 上的 OffiCraft 正在執行時，重裝會重啟服務。重啟期間所有開著的控制台與成員都會斷線，資料不會遺失。這台已經裝過 OffiCraft 時，安裝腳本會先警告並要求確認；用 `curl … | bash` 執行時沒辦法回答確認，需要加 `--force` 才會繼續。`--force` 只允許覆蓋檔案，不允許重啟；要重啟必須同時加上 `--restart-live`：
>
> ```bash
> curl -fsSL … | bash -s -- --force --restart-live
> ```

手動安裝用的是 [GitHub Releases](https://github.com/pkyosx/OffiCraft/releases) 上的 `officraft-<tag>-darwin-arm64.tar.gz` 壓縮檔，解開後執行其中的 `./install.sh`。

> [!NOTE]
> 用 asdf、nvm、volta 這類管理多個程式版本的工具安裝 `claude` 時，背景服務可能找不到 `claude`。安裝腳本找不到 Claude Code 或 Codex 時，環境變數 `OC_CLAUDE_BIN=/絕對路徑/claude` 或 `OC_CODEX_BIN=/絕對路徑/codex` 可以告訴安裝腳本程式的位置，設定後重跑安裝就會使用指定的位置。重跑安裝不會影響既有資料。

### 安裝腳本實際做了什麼

1. 確認是 macOS Apple Silicon。
2. 確認這台 Mac 有 `tmux`，以及 `claude` 或 `codex` 至少一種。即使這台只放 server、成員都跑在別台，也會檢查這兩項；缺少時會在動到任何檔案之前停下，並印出補裝的指令。安裝腳本只檢查有沒有裝，不檢查版本：Claude Code 低於 2.1.287 時安裝照樣會成功，但 Claude 成員喚醒會失敗，「監控 › 機器資訊」的 Claude 欄會標示「版本太舊」。Claude Code 的升級方式有兩種：同一個版本號選單裡的「升級 Claude Code」，或在那台 Mac 上執行 `npm install -g @anthropic-ai/claude-code`。
3. 下載最新 release 的 `officraft-<tag>-darwin-arm64.tar.gz` 與 `checksums.txt`，用 sha256 檢查碼確認下載的檔案和發佈時完全一樣，通過才繼續；驗證失敗就中止、不安裝任何東西。
4. 這台已經裝過 OffiCraft 時，先警告並要求確認。
5. 把 `ocserverd`、`ocwarden`、`ocagent`、`officraft` 裝到 `~/.officraft/bin`，並升級資料庫結構，既有資料保留。這幾支程式各自的用途見 [架構與運作原理](architecture.md)。
6. 確認埠沒有被占用（預設 7755）。
7. 註冊背景服務 `com.officraft.serve`。服務在背景執行，關掉終端機也不會停，開機後自動啟動。
8. 印出一次性設定連結 `http://127.0.0.1:7755/?code=…`。
9. 密碼設好後，server 自動把這台 Mac 的 warden 裝好，並喚醒助理 Mira。其中一步失敗時，控制台上方會出現「自動設定沒有全部完成」的橫幅並寫明原因。

官方 release 是預先編好的程式，這台 Mac 不需要 Go、node 或 python。

---

## 進階：同一台 Mac 裝第二個工作室

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
- 已經存在的工作室要換埠時，安裝腳本會拒絕；換埠只能透過修改該工作室的 `oc.toml`。
- 改 `$HOME` 的方式無法隔離出第二個工作室：背景服務的名稱不會跟著改變，重裝時仍會重啟主工作室的服務。隔離第二個工作室要用 `--namespace`。

---

## 進階：升級的設定

**設定 › 系統更新與備份** 上的每一格：

- **目前版本**：這台 server 正在跑的版本，旁邊是「檢查更新」與「查看 release」。狀態寫「尚未確認最新版本」代表 server 啟動後還沒成功連上 GitHub 查過，不代表已是最新版。
- **升級到最新版**：server 會下載新版、用 release 附的 sha256 檢查碼驗證、確認新程式能執行後，才替換自己並重新啟動。
- **自動更新**：打開後，偵測到新版時會在背景自動升級並重新啟動。預設關閉。
- **接收 Beta 版本**：打開後，檢查更新時也會納入 GitHub 上的預發佈版本。預設關閉。
- **更新檢查間隔**：背景多久向 GitHub 確認一次，1–60 分鐘，預設 5 分鐘。按「檢查更新」與升級一律當場查，不受這一格影響。

server 升級後，各台機器上的 warden 與 ocagent 會自動跟著更新。新版的 warden 與 ocagent 包含在 server 裡；各台 warden 每 15 分鐘向 server 確認一次，重新連上 server 或收到 server 的指令時也會立即確認，發現新版就自我更新，通常在 15 分鐘內完成。

---

## 進階：移除的其他選項

移除時以下這些不會動：

| 留在原處 | 說明 |
| --- | --- |
| `~/.officraft/agents/` | 這台 Mac 上每位成員的工作區。 |
| `~/.officraft/warden/` | warden 與 warden 自己的背景服務 `com.officraft.ocwarden`，移除後仍會繼續執行。 |
| `~/.officraft` 資料夾本身 | 不會被刪除。 |

這台 Mac 的 warden 有兩種移除方式：

- 移除 server 之前，在控制台「監控 › 機器資訊」的機器名稱選單選「解除安裝」。
- 在那台 Mac 上執行 `~/.officraft/warden/ocwarden teardown --canonical`。第二個工作室的 warden 改用 `OC_NAMESPACE=<名稱> ~/.officraft-<名稱>/warden/ocwarden teardown`。

其他移除選項：

```bash
curl -fsSL … | bash -s -- --uninstall --dry-run          # 只列出會做什麼，不做任何變更
curl -fsSL … | bash -s -- --uninstall --purge --yes      # 永久刪除，含資料庫，無法復原
curl -fsSL … | bash -s -- --uninstall --namespace lab    # 只移除第二個工作室
```

用 `curl … | bash` 執行時，`--purge` 的確認提示無法輸入，必須同時加 `--yes` 才會刪除；沒有加 `--yes` 時什麼都不會刪。把腳本存成檔案再執行時，確認提示可以正常輸入。

舊版安裝腳本在提早結束時，最後可能多印一行 `curl: (23)` 或 `curl: (56)` 的紅字；那一行不代表移除失敗，仍以腳本印出的完成訊息為準。

---

## 進階：從原始碼安裝

這條路是給要自己修改 OffiCraft 程式碼的人。官方 release 與從原始碼安裝裝出來的東西不同，兩條路只能擇一。

額外需要：

| 需求 | 最低版本 | 用途 |
| --- | --- | --- |
| Go | 1.26 | 編譯 server、warden 與 CLI（在終端機下指令操作的程式） |
| node、npm | node 18 | 編譯控制台前端 |
| python3 | 3.11 | 安裝時產生並檢查 `oc.toml`，以及開發用腳本；沒有 3.11 以上時 `bin/ocserver install` 會停下 |
| `cloudflared` | — | 只有要開對外 tunnel（讓外部網路連回這台 Mac 的通道）時才需要 |

安裝指令，在 repo（程式碼資料夾）根目錄執行：

```bash
bin/ocserver install
```

裝完會印出一次性的**啟用碼**。啟用碼用在控制台的「設定管理密碼」畫面，和密碼一起送出；密碼設好後啟用碼立即失效。

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

## 相關文件

- [你的第一個辦公室](quickstart.md)
- [保護你的工作室](security.md)
- [設定與參數](settings.md)
- [常見問題與排解](troubleshooting.md)
