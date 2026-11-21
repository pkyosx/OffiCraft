# 常見問題與排解

這頁依畫面上看到的症狀列出原因與處理的地方，涵蓋安裝、登入、成員起不來、回呼端點收不到事件、沒動靜或停不下來、備份還原、升級與移除。每一項依序寫你會看到什麼、原因是什麼、在哪裡處理。

## 安裝

### 安裝腳本說這台電腦不支援

OffiCraft 支援三種電腦：Apple Silicon 的 Mac（darwin/arm64）、x86_64 的 Linux（amd64）、arm64 的 Linux（aarch64）。Intel Mac 與其他處理器架構的 Linux 會在安裝一開始、下載任何東西之前被拒絕。

直接在 Windows 上執行也不支援。Windows 要先裝 WSL2 與 Ubuntu，再在 Ubuntu 裡安裝，步驟見 [安裝、升級與移除](install.md) 的「Windows（WSL2）」。

### 安裝腳本說 systemd 沒有在執行

Linux 上的 OffiCraft 以 systemd 的使用者服務在背景執行（systemd 是 Linux 用來啟動、停止背景服務的系統元件）。systemd 沒有在執行時，安裝腳本會停下，不安裝任何東西。

這個情形最常出現在 Windows 的 WSL2，因為 WSL2 預設不啟動 systemd。打開 systemd 的方式見 [安裝、升級與移除](install.md) 的「Windows（WSL2）」：在 `/etc/wsl.conf` 加入 `[boot]` 與 `systemd=true`，從 Windows 執行 `wsl.exe --shutdown`，重新打開 Ubuntu 視窗後再執行安裝。WSL 版本低於 0.67.6 時不支援 systemd，要先用 `wsl --update` 更新。

在 Ubuntu 視窗裡執行 `ps -p 1 -o comm=`，印出 `systemd` 代表 systemd 正在執行。

### 安裝腳本說缺少 tmux 或 agent runtime

安裝腳本在動到機器之前會先檢查 `tmux`，以及 `claude`（Claude Code）或 `codex`（Codex）至少一種；缺任何一樣就停下，什麼都不安裝。補裝的指令在 [安裝、升級與移除](install.md) 的「步驟 1：安裝 tmux」與「步驟 2：安裝並登入 Claude Code 或 Codex」，補上後重新執行安裝指令即可。只裝其中一種也可以，但裝的那一種必須已經登入，成員才能啟動。Windows（WSL2）上，Claude Code 或 Codex 要裝在 WSL2 的 Ubuntu 裡；裝在 Windows 那一邊的不算。

### 7755 埠被占用

server 預設使用 7755 埠（埠是電腦上區分不同連線服務的編號）。7755 埠被其他程式占用時，安裝腳本會列出占用的程式並停下；此時程式已經裝好、資料庫也已準備好，只是 server 沒有啟動。關掉占用的程式，或讓 server 改用其他埠，再重新執行安裝，都能解決。

server 使用的埠寫在 `oc.toml` 的 `[server]` 段落，例如：

```toml
[server]
port = 7756
```

安裝腳本只從 `$OC_CONFIG` 指定的檔案或執行安裝的資料夾中的 `oc.toml` 讀取 `[server].port`，不讀其他環境變數。server 之後每次啟動都會讀同一份 `oc.toml`，**這份檔案被移走或刪除後，server 就讀不到改過的埠**。server 已經在執行時改埠，安裝腳本會停下並列出前後差異，要加上 `--relocate` 重新執行才會套用。

換埠之後，控制台網址變成 `http://localhost:<新的埠>`。其他安裝選項見 [安裝、升級與移除](install.md) 的「進階：安裝選項」。

### 設定管理密碼的啟用碼弄丟了

啟用碼只能用一次，設好密碼後就失效。

- 已經設過密碼：登入只需要密碼，不再需要啟用碼。
- 還沒設密碼：在 server 那台機器執行 `~/.officraft/bin/ocserverd claim-token` 會印出目前的啟用碼。設過密碼之後，`claim-token` 不再印出啟用碼。

### 控制台上方出現「自動設定沒有全部完成」

設好密碼後，server 會自動安裝這台機器的 warden 並喚醒特助 Mira。其中一步失敗時，控制台上方會出現「自動設定沒有全部完成」橫幅，寫出是哪一步、原因，並可以「顯示詳細記錄」。

- 「安裝這台機器」失敗：失敗原因在詳細記錄中。原因排除後，監控 › 機器資訊 中這台機器名稱選單的「安裝」會重做這一步。
- 自動設定被中斷（例如 server 剛好重新啟動）：監控 › 機器資訊 的「安裝」會補做機器安裝；Mira 要另外喚醒。
- 寫著「初始設定沒有完成」：重新啟動 server 後會再做一次初始設定。

## 登入

### 忘記密碼、驗證器遺失或通行密鑰無法使用

這三種情況都用裝 server 那台機器上的 `~/.officraft/bin/ocserverd` 復原指令處理。能在那台機器上執行指令，就證明你是工作室的主人，所以不需要其他驗證。重新啟動 server 的指令：

```bash
# macOS
launchctl kickstart -k gui/$(id -u)/com.officraft.serve
# Linux 與 Windows（WSL2）
systemctl --user restart com.officraft.serve.service
```

- **重設密碼**：`OC_NEW_PASSWORD='新密碼' ~/.officraft/bin/ocserverd set-password`。新密碼從環境變數 `OC_NEW_PASSWORD` 帶入，指令本身不接受參數；server 重新啟動之前，新密碼不會生效。
- **關閉兩步驟驗證**：`~/.officraft/bin/ocserverd mfa-disable`。server 重新啟動之前，登入仍會要求驗證碼。
- **刪除所有通行密鑰**：`~/.officraft/bin/ocserverd passkey-reset`。所有通行密鑰立即刪除，不需要重新啟動 server，之後用密碼登入。

兩步驟驗證與通行密鑰各自的注意事項在 [保護你的工作室](security.md)。

## 成員起不來

### 先看哪裡

成員起不來的原因記在三個地方：

- **成員詳情的「最近操作」**：最後一次喚醒的結果與原因，是最準確的一行。
- **監控 › 機器資訊**：成員所在的機器是否線上；Claude、Codex 欄顯示的是版本、「未安裝」、「未登入」還是「版本太舊」。
- **那台機器上的 warden 紀錄**：`~/.officraft/warden/log/ocwarden.err.log`，記錄 warden 啟動成員時的詳細過程。macOS 與 Linux 的位置相同。

server 送出喚醒後，成員會先顯示「喚醒中」；大約兩分鐘內沒有連上，這次喚醒算失敗，原因寫進最近操作。

### 跳出「這次沒有送出喚醒指令」

這表示 server 這次沒有把喚醒指令送出去，背景會自動重試。常見原因有兩個：

- 目標機器沒有連線。機器是否在線顯示在「監控」。
- 前一次喚醒還在重試中。重試的情形記在這位成員的「最近操作」。

最近操作的時間比你這次按下按鈕更早時，寫的是上一次喚醒的結果，不一定適用於這次。

### 最近操作寫著「未登入 Claude」或「未登入 Codex」

成員所在的機器沒有登入它要用的 AI 執行環境。登入的入口在 監控 › 機器資訊，那台機器 Claude 或 Codex 欄的版本號選單中的「登入」。登入完成後，成員要再喚醒一次才會啟動。

warden 會定期檢查登入狀態，間隔是 設定 › 參數調整 的「登入檢查間隔」與「未登入時重查間隔」。

### 機器只裝了 Codex，Mira 或新成員起不來

成員在機器上啟動時，會依那台機器回報的能力決定用 Claude Code 還是 Codex。以下情況成員會以 Claude Code 啟動，而那台機器上沒有 Claude Code：

- `codex` 裝了但沒登入，所以不算可用。這次啟動不會把 Claude Code 存成成員的設定，登入 Codex 後再喚醒，成員就會改用 Codex。
- 那台機器的 warden 還沒回報過能力。同樣不會存下來，warden 回報後再喚醒，成員就會用 Codex。
- 成員的執行環境已經存成 Claude Code：有人在成員設定裡選了 Claude Code，或是從舊版升級上來的成員。

第三種情況要在 監控 › 機器資訊 登入 Codex，並在成員詳情的「AI 執行環境」改成 Codex（成員在線上或喚醒中時，這項設定在「更改」按鈕裡；不在線上時在「喚醒」按鈕裡）。成員正在喚醒中時，新的執行環境要等下一次啟動才生效。之後在這台機器新增的成員會自動使用 Codex；**已經存成 Claude Code 的成員不會自動改**，每一位都要各自更改。

### 最近操作寫著 Codex 沒有某個系列的型號

成員設定的 Codex 型號系列在那台機器的 Codex 版本裡找不到，或那台機器上的 OffiCraft 程式太舊、還不認得型號系列。更新那台機器的 Codex 或 OffiCraft，或在成員詳情改設那台機器可用的型號，都能解決。

### 最近操作寫著 notify_claude_too_old

那台機器的 Claude Code 版本太舊，無法載入 OffiCraft 通知模組，warden 沒有啟動成員。版本太舊的機器在 監控 › 機器資訊 的 Claude 欄會標「版本太舊」，同一欄版本號選單中的「升級 Claude Code」會升級它；升級後成員要再喚醒一次。

### 最近操作寫著 notify_mod_not_loaded 或 wake_timeout

Claude Code 啟動了，但 OffiCraft 通知模組沒有在時間內載入，成員因此沒有連上 server。warden 會先自動重新啟動 Claude Code 一次；還是失敗時，訊息後面會寫「已自動重啟 Claude Code 一次仍沒載入」。過一段時間後，最近操作會改顯示較籠統的 `wake_timeout`，原因只留在 warden 紀錄。

實際原因記在那台機器上：warden 紀錄 `~/.officraft/warden/log/ocwarden.err.log` 中含 `notify-mod` 的行，以及成員資料夾 `~/.officraft/agents/<成員 ID>/` 裡的兩個標記檔 `.officraft-mod-started`（模組開始執行時產生）與 `.officraft-mod-loaded`（模組啟動連線程式時產生）。各種紀錄的意思：

- 紀錄有 `.officraft-mod-started written ...s after launch`：模組有執行，但 Claude Code 啟動太慢。紀錄中以 `pane|` 開頭的幾行是當時成員畫面的最後幾行，看得出停在哪個對話框或錯誤。
- 紀錄有 `.officraft-mod-started absent`：模組完全沒有執行。最常見的是 Claude Code 啟動時同步組織的 plugin 有變動，畫面顯示 `Plugins changed. Run /reload-plugins to activate.`；warden 會自動送一次 `/reload-plugins`。仍然失敗時，其他可能的原因是工作目錄沒有被 Claude Code 信任，或 Claude Code 設定停用了 hook 或 plugin（例如 `disableAllHooks`、`--safe-mode`、公司統一管理的設定）。
- `.officraft-mod-loaded` 存在但成員仍不在線：模組啟動過連線程式，但連線程式後來結束了。`pgrep -fl 'ocagent listen'` 會列出它是否還在執行。

### Claude Code 或 Codex 用版本管理工具安裝，成員找不到它

用 asdf、nvm、volta 等工具安裝的 `claude` 或 `codex`，背景服務可能找不到，因為背景服務只在少數固定的資料夾中尋找程式（PATH，也就是系統尋找程式的資料夾清單）。安裝腳本會記下安裝當下找到的路徑；安裝時找不到，就要在 server 那台機器重新執行安裝，並用 `OC_CLAUDE_BIN=/絕對路徑/claude` 或 `OC_CODEX_BIN=/絕對路徑/codex` 指定位置，例如：

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | OC_CLAUDE_BIN=/絕對路徑/claude bash
```

重新執行安裝不會影響既有資料。

### 整台機器的成員都起不來

原因看 監控 › 機器資訊 中那台機器的狀態：

- **離線**：那台機器關機或連不到 server。warden 每台獨立運作，一台離線只影響在那台上的成員。
- **長時間離線、連上網路也沒恢復**：可能是機器憑證過期。每台機器的憑證有效期是 設定 › 參數調整 的「機器憑證壽命」（預設 30 天），warden 會在到期前自動換新；機器長時間關機而錯過換新時，要重新安裝那台機器。機器名稱選單中的「安裝」（線上時顯示「重新安裝」）會產生要在那台機器上執行的安裝指令。
- **機器欄位有驚嘆號寫著「未生效」**：這台機器上的成員全部停止、再逐一喚醒後恢復。

### 外包一直沒有人接

- 辦公室側欄「外包」頁籤顯示「已暫停指派」：外包上限是 0。上限在「外包」頁籤「招攬新成員」裡的「最多雇用」。
- 任務手冊的負責成員是外包、但沒有選機器，或選的機器離線：外包只會在選定的機器上啟動。機器在 設定 › 任務手冊 中選定，選定的機器上線後外包才會啟動。

### Windows（WSL2）關掉終端機後，成員會不會離線

不會。Windows 只在還有從 Windows 這一側啟動的 Linux 程式時，讓 WSL2 的 Ubuntu 繼續執行；Ubuntu 視窗算在內，systemd 的使用者服務不算。所以在 WSL2 上安裝時，OffiCraft 會在 Windows 的「啟動」資料夾放一個保持執行檔，並立刻執行一次。保持執行檔在背景啟動一個不做任何事、也不會結束的 Linux 程式（`sleep infinity`），畫面上不會多出視窗；有了 `sleep infinity`，所有 Ubuntu 視窗都關掉後，Ubuntu 與裡面的 warden、成員仍繼續執行。每次登入 Windows 時，「啟動」資料夾裡的保持執行檔會再執行一次。

- **保持執行檔的檔名是 `OffiCraft keep WSL running (<名稱>).vbs`。** `<名稱>` 是裝 OffiCraft 的那一套 Linux 的名稱，例如 `Ubuntu`；在 PowerShell 執行 `wsl -l -v` 可以列出名稱。第二個工作室的 warden（用 `OC_NAMESPACE` 安裝）另有一個檔名是 `OffiCraft keep WSL running (<名稱>, <工作室名稱>).vbs` 的保持執行檔。按 Windows 鍵加 R，輸入 `shell:startup` 後按 Enter，會打開「啟動」資料夾。保持執行檔的內容只有一行，名稱是 `Ubuntu` 時如下：

  ```vb
  CreateObject("WScript.Shell").Run "wsl.exe -d Ubuntu --exec sleep infinity", 0, False
  ```

- **安裝時印出 `WARN: WSL keep-alive not set up`，代表保持執行檔沒有設定好。** 常見原因是裝 OffiCraft 的 Linux 關掉了 Windows 互通功能（在 Linux 裡執行 Windows 程式的功能，由 `/etc/wsl.conf` 的 `[interop]` 設定）。這時所有 Ubuntu 視窗都關掉後，Windows 會把整套 Ubuntu 停掉，這台機器在「機器資訊」顯示離線；重新打開 Ubuntu 視窗後不需要其他操作，warden 隨 Ubuntu 自動啟動，原本在這台機器上的成員約半分鐘內重新上線。要在關掉視窗後讓成員繼續工作，在 Windows 的 PowerShell 執行 WARN 那一行印出的指令，名稱是 `Ubuntu` 時如下：

  ```powershell
  Start-Process wsl.exe -ArgumentList '-d','Ubuntu','--exec','sleep','infinity' -WindowStyle Hidden
  ```

  PowerShell 指令啟動的 `sleep infinity` 只持續到 Windows 登出或重新開機。要每次登入 Windows 都自動啟動，用記事本建立內容是上面那一行 `CreateObject(…)` 的檔案，存成副檔名為 `.vbs` 的檔案放進「啟動」資料夾。

- **停止的方式**：在 PowerShell 執行 `wsl --terminate <名稱>`，Ubuntu 會連同裡面的 OffiCraft 與成員一起停止。不要在登入 Windows 時自動執行，就把保持執行檔從「啟動」資料夾刪掉。用 `ocwarden teardown` 移除 warden 時，保持執行檔會一併刪除，已經在執行的 `sleep infinity` 持續到 Windows 登出或重新開機。
- **電腦進入睡眠時，成員一樣會離線。** 要讓成員持續工作，這台電腦要保持喚醒。

## 回呼端點

### 外部服務送了事件，成員卻沒收到

外部服務那一邊顯示成功，不代表成員收到了：不論請求是送達、被丟棄，還是 token 根本不存在，外部服務收到的回應都相同（請求內容超過 1 MiB 時例外，外部服務會收到 413 錯誤；Slack 的驗證握手也會收到不同的回應）。丟棄原因記在成員詳情「回呼端點 · WEBHOOK」卡中該列的「事件統計」：

- **驗簽失敗**：Slack 或 GitHub 那一邊與這裡的簽章密鑰不一致。兩邊設成同一把密鑰後恢復。
- **停用中被打**：端點是停用狀態，事件送到時被丟棄。「啟用中」開關打開後恢復。
- **成員已不存在**：端點所屬的成員已經離開，端點要在其他成員身上重建。
- **oversize**：請求內容超過 1 MiB，系統整筆拒收。

成員離線或已停止時，送達的事件會留著，等成員下次上線才讀到。各原因的完整說明見 [介面說明](interface.md) 的「回呼端點」。

## 成員線上但沒動靜

成員的圓點是「線上」，卻遲遲沒有回應或進度。可能的原因有以下幾種：

- **名字旁有驚嘆號**：成員現在沒辦法正常使用 AI 模型，驚嘆號裡寫著原因：
  - **已達用量上限**：帳號用量用完了，訊息會寫今天或明天幾點重置，重置後成員會繼續。用量顯示在 監控 › 帳號資訊。
  - **登入失效**、**未登入 Claude／未登入 Codex**：那台機器的登入已經失效，在 監控 › 機器資訊 重新登入 Claude 或 Codex 後恢復。
  - **伺服器異常**：模型服務那一邊的錯誤，通常稍後會自行恢復。
  - **模型呼叫失敗（附代碼）**：失敗的詳情記在成員詳情的「最近操作」，代碼表示失敗的種類。
- **運作狀態圖示**：「工作中」表示成員正在做事（可能是子代理在做，子代理是成員另外開出、替它處理一部分工作的 AI 工作者）；「等待中」表示在等背景執行的指令或排程；「閒置」表示成員沒有在做事。細節顯示在「運行狀況」。
- **有請示卡在等你**：成員遇到需要你決定的事會開請示卡並停下，你回覆後才繼續；特助也可以代你回覆。同一張任務還有其他請示卡沒回時，任務會維持「等我回覆」。請示卡列在請示分頁。
- **任務的狀態**：
  - **等待外部**：在等權限、對方回覆或某個時間，不是成員本身的問題。
  - **可結案**：所有步驟都完成了，等負責人結案。負責人沒有收尾時，任務卡上「可結案」標籤的選單中有「強制結案」；**強制結案無法復原**。
  - **凍結**：優先權是凍結的任務會暫停推進。

  任務狀態的意思見 [追蹤任務進度](tasks.md)。
- **記憶用量很高**：記憶快滿時成員回應會變差。成員詳情的「重新聚焦」會讓線上的成員把進度寫成交接，換一個新的工作階段從交接接著做。見 [增加成員與發外包](members.md)。

## 成員停不下來

### 成員停在「停止中」很久

一般的停止沒有時間限制，server 會一直等成員自己收尾完成。成員詳情的「加速停止」會給收尾一個期限（預設 120 秒）；「強制停止」會立即結束成員，**未儲存的工作會遺失**。

## 備份與還原

### 備份健康在哪裡看

工作室每 6 小時自動備份一次資料庫。備份狀態在 設定 › 系統更新與備份 的「備份健康」：

| 狀態 | 意思 |
| --- | --- |
| 備份正常 | 最近一次排程備份在期限內。 |
| 備份異常 | 從來沒有備份過、最新的備份太舊（排程可能停了），或最近一次備份失敗；會寫出已經異常多久。 |
| 無法判斷 | 還沒評估過，或控制台問不到 server。不代表沒問題，請當成需要確認。 |

只有排程備份會讓狀態變回正常。手動執行 `ocserverd backup` 或升級前的自動備份都不算；重新啟動 server 也不會讓異常消失。**備份健康不會主動通知你**，備份異常只會顯示在 設定 › 系統更新與備份。

### 自己備份資料庫

資料庫在 `~/.officraft/server/data/`，由 `officraft.db`、`officraft.db-wal`、`officraft.db-shm` 三個檔組成。**server 執行中時只複製 `officraft.db` 會少掉資料**：最近寫入的資料在 `-wal` 檔裡，只複製主檔的備份看起來正常，還原後卻會少掉最近的資料。

完整的備份有兩種：

- **不停機**：`~/.officraft/bin/ocserverd backup` 會產生一份已包含全部資料的單一檔案，放在 `~/.officraft/server/data/backups/`。執行前它會印出使用的設定檔與資料庫路徑，其中 `database` 是實際被備份的資料庫。
- **停機後複製**：server 停止後，三個檔一起複製，或直接複製整個 `data` 資料夾。

自動備份放在同一個 `backups/` 資料夾，檔名是 `officraft-<UTC 時間>-<種類>.db`，種類有 `scheduled`（排程）、`premigration`（升級前）、`manual`（手動）。日常備份與升級前備份各自保留「備份保留份數」份（設定 › 參數調整，預設 5，範圍 1–20），超過的會在下一次備份時刪除。

### 換電腦時要帶走 vault.key

資料庫備份（不論自動或手動）都不包含 `vault.key`，密鑰的值與 Secret 請示卡尚未刪除的內容都是用 `vault.key` 加密的。在同一台電腦上還原時，`vault.key` 還在原本的資料夾，不用處理；把備份搬到別台電腦還原時，要連同 `vault.key` 一起帶過去，否則還原後的密鑰無法讀取。詳細說明見 [保護你的工作室](security.md) 的「密鑰與 vault.key」。

### 從備份還原

還原是在 server 停止時，用備份檔取代 `~/.officraft/server/data/` 中的資料庫：現有的 `officraft.db`、`officraft.db-wal`、`officraft.db-shm` 三個檔移到別處，再把備份檔複製成 `officraft.db`（例如 `cp backups/officraft-20261020-060000-scheduled.db officraft.db`）。同一個資料夾裡要有當時的 `vault.key`，少了它，還原後的密鑰無法讀取。

停止與啟動 server 的指令，macOS：

```bash
launchctl bootout gui/$(id -u)/com.officraft.serve
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.officraft.serve.plist
```

Linux 與 Windows（WSL2）：

```bash
systemctl --user stop com.officraft.serve.service
systemctl --user start com.officraft.serve.service
```

## 升級

### 升級沒有生效或檢查更新失敗

出現「檢查更新失敗」時，多半是連不到 GitHub（OffiCraft 的更新發佈在 GitHub；網路或 proxy 問題都會造成），網路恢復後檢查更新就會成功。其他機器上的 warden 會在 server 升級後自行更新，通常在 15 分鐘內完成。升級的完整步驟與各項更新設定，見 [安裝、升級與移除](install.md) 的「升級」與「進階：升級的設定」。

## 移除

移除一台機器（解除安裝、刪除）或移除整個工作室的步驟，見 [安裝、升級與移除](install.md) 的「移除」與「從控制台移除一台機器」。

## 相關文件

- [安裝、升級與移除](install.md)
- [保護你的工作室](security.md)
- [增加成員與發外包](members.md)
- [追蹤任務進度](tasks.md)
- [架構與運作原理](architecture.md)
