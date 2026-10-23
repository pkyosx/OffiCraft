# 常見問題與排解

讀完這頁，你能依照畫面上看到的症狀找出原因並自行排除，涵蓋安裝、登入、成員起不來或沒動靜、連線、備份還原、升級與移除。每一項先寫你會看到什麼，再寫怎麼處理。

## 安裝

### 安裝腳本說只支援 macOS Apple Silicon

OffiCraft 只支援 Apple Silicon 的 Mac（darwin/arm64）。Intel Mac、Linux、Windows 都會在安裝一開始被拒絕。

### 安裝腳本說缺少 tmux 或 agent runtime

安裝腳本在動到機器之前會先檢查兩樣東西，缺了就停下，什麼都不安裝：

- `tmux`：用 `brew install tmux` 安裝。
- `claude`（Claude Code）或 `codex`（Codex），至少一種。Claude Code 用 `npm install -g @anthropic-ai/claude-code` 安裝。

補上後重新執行安裝指令即可。只裝其中一種也可以，但必須先登入，成員才能用它啟動。

### 7755 埠被占用

server 預設使用 7755 埠。7755 埠被其他程式占用時，安裝腳本會列出占用的程式並停下；此時程式已經裝好、資料庫也已準備好，只是 server 沒有啟動。處理方式二選一：

- 關掉占用的程式。用 `lsof -nP -iTCP:7755 -sTCP:LISTEN` 找出是誰。
- 改用其他埠：在執行安裝的資料夾建立 `oc.toml`，寫入：

  ```toml
  [server]
  port = 7756
  ```

  然後在同一個資料夾重新執行安裝。安裝腳本只從 `$OC_CONFIG` 指定的檔案或目前資料夾的 `oc.toml` 讀取 `[server].port`，不讀其他環境變數。server 之後每次啟動都會讀這份 `oc.toml`，請把它留在原處。

  server 已經在執行、想把它改到另一個埠時，安裝腳本會停下並列出前後差異，確認無誤後加上 `--relocate` 重新執行。

換埠之後，控制台網址變成 `http://127.0.0.1:<新的埠>`。完整說明見 [安裝、升級與移除](install.md)。

### 設定管理密碼的啟用碼弄丟了

啟用碼只能用一次，設好密碼後就失效。

- 已經設過密碼：不需要啟用碼，直接開 `http://127.0.0.1:7755` 用密碼登入。
- 還沒設密碼：在 server 那台機器執行 `~/.officraft/bin/ocserverd claim-token`，會印出目前的啟用碼。設過密碼之後，`claim-token` 不再印出啟用碼。

### 控制台上方出現「自動設定沒有全部完成」

設好密碼後，server 會自動安裝這台機器的 warden 並喚醒特助 Mira。其中一步失敗時，控制台上方會出現「自動設定沒有全部完成」橫幅，寫出是哪一步、原因，並可以「顯示詳細記錄」。

- 「安裝這台機器」失敗：看詳細記錄找出原因，修正後到 監控 › 機器資訊，在這台機器名稱的選單選「安裝」重試。
- 自動設定被中斷（例如 server 剛好重新啟動）：同樣到 監控 › 機器資訊 選「安裝」，再喚醒 Mira。
- 寫著「初始設定沒有完成」：重新啟動 server 後再試一次。

## 登入

### 忘記密碼、驗證器遺失或通行密鑰無法使用

在裝 server 的那台機器上，用 `~/.officraft/bin/ocserverd` 的復原指令：

- **重設密碼**：`OC_NEW_PASSWORD='新密碼' ~/.officraft/bin/ocserverd set-password`，再執行 `launchctl kickstart -k gui/$(id -u)/com.officraft.serve` 重新啟動 server。新密碼從環境變數帶入，指令本身不接受參數；重新啟動之前，新密碼還不會生效。
- **關閉兩步驟驗證**：`~/.officraft/bin/ocserverd mfa-disable`。
- **刪除所有通行密鑰**：`~/.officraft/bin/ocserverd passkey-reset`。
- **產生一次性登入連結**：第一次使用前先執行 `~/.officraft/bin/ocserverd login-link enable`，再執行 `~/.officraft/bin/ocserverd login-link --base-url <控制台網址>`。

兩步驟驗證、通行密鑰與一次性登入連結各自的注意事項在 [保護你的工作室](security.md)。

### 從手機或其他裝置連不上

server 預設只接受本機（`127.0.0.1`）的連線，其他裝置連不到是正常的。要從手機使用，需要自己架設 tunnel（例如 cloudflared）或 VPN（例如 Tailscale）。不要把埠直接開放到公開網路上；對外開放前，先設強密碼並開啟兩步驟驗證或通行密鑰。步驟見 [在手機上用](mobile.md) 與 [保護你的工作室](security.md)。

### 分頁列上方出現「即時更新已中斷」

控制台和 server 的即時連線斷了，畫面上的內容可能不是最新的。控制台會自動重新連線；想立即更新就按「重新整理」。一直出現時，確認 server 還在執行、網路或 tunnel 是否正常。

## 成員起不來

### 先看哪裡

成員起不來時，依序看三個地方：

1. **成員詳情的「最近操作」**：最後一次喚醒的結果與原因。這是最準確的一行。
2. **監控 › 機器資訊**：成員所在的機器是否線上；Claude、Codex 欄顯示的是版本、「未安裝」、「未登入」還是「版本太舊」。
3. **那台機器上的 warden 紀錄**：`~/.officraft/warden/log/ocwarden.err.log`。

server 送出喚醒後，成員會先顯示「喚醒中」；大約兩分鐘內沒有連上，這次喚醒算失敗，原因寫進最近操作。

### 跳出「這次沒有送出喚醒指令」

這表示 server 這次沒有把喚醒指令送出去，背景會自動重試。常見原因有兩個：

- 目標機器沒有連線。到「監控」看那台機器是否在線。
- 前一次喚醒還在重試中。看這位成員的「最近操作」。

最近操作的時間比你這次按下按鈕更早時，寫的是上一次喚醒的結果，不一定適用於這次。

### 最近操作寫著「未登入 Claude」或「未登入 Codex」

成員所在的機器沒有登入它要用的 AI 執行環境。到 監控 › 機器資訊，在那台機器的 Claude 或 Codex 欄按版本號，選「登入」，照對話框的步驟授權。登入完成後再喚醒成員。

warden 會定期檢查登入狀態，間隔是 設定 › 參數調整 的「登入檢查間隔」與「未登入時重查間隔」。

### 機器只裝了 Codex，Mira 或新成員起不來

成員第一次在機器上啟動時，會依那台機器回報的能力決定用 Claude Code 還是 Codex。以下情況會讓成員被設成 Claude Code，而那台機器上沒有 Claude Code：

- `codex` 裝了但沒登入，所以不算可用。
- 那台機器的 warden 還沒回報過能力。
- 有人在成員設定裡明確選了 Claude Code。

處理方式：先在 監控 › 機器資訊 的 Codex 欄登入 Codex。然後開成員詳情，按「更改」，把「AI 執行環境」改成 Codex 並送出。之後在這台機器新增的成員會自動使用 Codex；已經存成 Claude Code 的成員不會自動改，要逐一手動更改。

### 最近操作寫著 Codex 沒有某個系列的型號

成員設定的 Codex 型號系列在那台機器的 Codex 版本裡找不到，或那台機器上的 OffiCraft 程式太舊、還不認得型號系列。依訊息更新那台機器的 Codex，或在成員詳情改設可用的型號。

### 最近操作寫著 notify_claude_too_old

那台機器的 Claude Code 版本太舊，無法載入 OffiCraft 通知模組，warden 沒有啟動成員。到 監控 › 機器資訊，在那台機器 Claude 欄的版本號選「升級 Claude Code」，升級後再喚醒。版本太舊的機器在 Claude 欄會標「版本太舊」。

### 最近操作寫著 notify_mod_not_loaded 或 wake_timeout

Claude Code 啟動了，但 OffiCraft 通知模組沒有在時間內載入，成員因此沒有連上 server。warden 會先自動重新啟動 Claude Code 一次；還是失敗時，訊息後面會寫「已自動重啟 Claude Code 一次仍沒載入」。過一段時間後，最近操作會改顯示較籠統的 `wake_timeout`，原因只留在 warden 紀錄。

到那台機器上查：

```bash
grep 'notify-mod' ~/.officraft/warden/log/ocwarden.err.log
ls ~/.officraft/agents/<成員 ID>/.officraft-mod-started
ls ~/.officraft/agents/<成員 ID>/.officraft-mod-loaded
```

- 紀錄有 `.officraft-mod-started written ...s after launch`：模組有執行，但 Claude Code 啟動太慢。紀錄中以 `pane|` 開頭的幾行是當時成員畫面的最後幾行，看得出停在哪個對話框或錯誤。
- 紀錄有 `.officraft-mod-started absent`：模組完全沒有執行。最常見的是 Claude Code 啟動時同步組織的 plugin 有變動，畫面顯示 `Plugins changed. Run /reload-plugins to activate.`；warden 會自動送一次 `/reload-plugins`。仍然失敗時，檢查工作目錄是否被 Claude Code 信任，以及 Claude Code 設定是否停用了 hook 或 plugin（例如 `disableAllHooks`、`--safe-mode`、受管設定）。
- `.officraft-mod-loaded` 存在但成員仍不在線：模組啟動過連線程式，但它後來結束了。用 `pgrep -fl 'ocagent listen'` 確認它是否還在執行。

### Claude Code 或 Codex 用版本管理工具安裝，成員找不到它

用 asdf、nvm、volta 等工具安裝的 `claude` 或 `codex`，在背景服務的精簡 PATH 下可能找不到。安裝腳本會記下安裝當下找到的路徑；如果安裝時它不在 PATH 上，在 server 那台機器用絕對路徑重新執行安裝：

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | OC_CLAUDE_BIN=/絕對路徑/claude bash
```

Codex 改用 `OC_CODEX_BIN=/絕對路徑/codex`。

### 整台機器的成員都起不來

到 監控 › 機器資訊 看那台機器的狀態：

- **離線**：確認那台 Mac 開著、能連到 server。warden 每台獨立運作，一台離線只影響在那台上的成員。
- **長時間離線、連上網路也沒恢復**：可能是機器憑證過期。每台機器的憑證有效期是 設定 › 參數調整 的「機器憑證壽命」（預設 30 天），warden 會在到期前自動換新；機器長時間關機而錯過換新時，要在機器名稱的選單選「安裝」（機器離線時選單上顯示「安裝」，線上時顯示「重新安裝」），到那台機器執行安裝指令。
- **機器欄位有驚嘆號寫著「未生效」**：先把這台機器上的成員全部停止，再逐一喚醒。

## 成員線上但沒動靜

成員的圓點是「線上」，卻遲遲沒有回應或進度。依序檢查：

1. **名字旁有沒有驚嘆號。** 點開會看到原因：
   - **已達用量上限**：帳號用量用完了，訊息會寫今天或明天幾點重置，重置後成員會繼續。用量可以在 監控 › 帳號資訊 查看。
   - **登入失效**、**未登入 Claude／未登入 Codex**：到 監控 › 機器資訊，在那台機器的 Claude 或 Codex 欄按版本號 →「登入」，重新登入。
   - **伺服器異常**：模型服務端的錯誤，通常稍後會自行恢復。
2. **運作狀態圖示。** 「工作中」表示它正在做事（可能是子代理在做）；「等待中」表示在等背景指令或排程；「閒置」表示它沒有在做事。點圖示的「查看運行狀況」看細節。
3. **有沒有請示卡在等你。** 到請示分頁看。成員遇到需要你決定的事會開請示卡並停下，你回覆後才繼續；特助也可以代你回覆。同一張任務還有其他請示卡沒回時，任務會維持「等我回覆」。
4. **任務的狀態。**
   - **等待外部**：在等權限、對方回覆或某個時間，不是成員本身的問題。
   - **可結案**：所有步驟都完成了，等負責人結案。負責人沒有收尾時，你可以在任務卡的狀態選單選「強制結案」。
   - **凍結**：優先權是凍結的任務會暫停推進。

   任務狀態的意思見 [追蹤任務進度](tasks.md)。
5. **記憶用量是否很高。** 記憶快滿時成員回應會變差。成員在線時，可以在成員詳情按「重新聚焦」，讓它整理記憶後換一個新的工作階段。見 [增加成員與發外包](members.md)。

### 外包一直沒有人接

- 辦公室側欄「外包」頁籤顯示「已暫停指派」：外包上限是 0。按「招攬新成員」調整「最多雇用」。
- 任務手冊的負責成員是外包、但沒有選機器，或選的機器離線：外包只會在選定的機器上啟動。到 設定 › 任務手冊 選一台線上的機器，或讓那台機器上線。

### 成員停在「停止中」很久

大部分停止都沒有時間限制，server 會一直等成員自己收尾完成。想讓它快點結束，在成員詳情按「加速停止」給它期限（預設 120 秒）；不想等就再按「強制停止」，未儲存的工作會遺失。

## 備份與還原

### 備份健康在哪裡看

工作室每 6 小時自動備份一次資料庫。備份狀態在 設定 › 系統更新與備份 的「備份健康」：

| 狀態 | 意思 |
| --- | --- |
| 備份正常 | 最近一次排程備份在期限內。 |
| 備份異常 | 從來沒有備份過、最新的備份太舊（排程可能停了），或最近一次備份失敗；會寫出已經異常多久。 |
| 無法判斷 | 還沒評估過，或控制台問不到 server。不代表沒問題，請當成需要確認。 |

只有排程備份會讓狀態變回正常。手動執行 `ocserverd backup` 或升級前的自動備份都不算；重新啟動 server 也不會讓異常消失。備份健康不會主動通知你，要自己到 設定 › 系統更新與備份 查看。

### 自己備份資料庫

資料庫在 `~/.officraft/server/data/`，由 `officraft.db`、`officraft.db-wal`、`officraft.db-shm` 三個檔組成。server 執行中時不要只複製 `officraft.db`：最近寫入的資料在 `-wal` 檔裡，只複製主檔的備份看起來正常，還原後卻會少掉最近的資料。

正確做法有兩種：

- **不停機**：執行 `~/.officraft/bin/ocserverd backup`。它會產生一份已包含全部資料的單一檔案，放在 `~/.officraft/server/data/backups/`。執行前它會印出使用的設定檔與資料庫路徑，請確認印出的 `database` 路徑是你要備份的資料庫。
- **停機後複製**：先停止 server，再把三個檔一起複製，或直接複製整個 `data` 資料夾。

自動備份放在同一個 `backups/` 資料夾，檔名是 `officraft-<UTC 時間>-<種類>.db`，種類有 `scheduled`（排程）、`premigration`（升級前）、`manual`（手動）。日常備份與升級前備份各自保留「備份保留份數」份（設定 › 參數調整，預設 5，範圍 1–20），超過的會在下一次備份時刪除。

### 一定要另外保存 vault.key

密鑰分頁的值是用 `~/.officraft/server/data/vault.key` 加密的。資料庫備份（不論自動或手動）都不包含 `vault.key`。沒有它，還原後的密鑰無法讀取，也無法修改或新增密鑰，只能刪掉現有的密鑰再重新建立。請把 `vault.key` 另外存到安全的地方，不要和資料庫備份放在一起公開分享。見 [把帳號金鑰交給成員用](secrets.md)。

### 從備份還原

1. 停止 server：

   ```bash
   launchctl bootout gui/$(id -u)/com.officraft.serve
   ```

2. 進入 `~/.officraft/server/data/`，把現有的 `officraft.db`、`officraft.db-wal`、`officraft.db-shm` 移到別的資料夾保留。
3. 把要還原的備份檔複製成 `officraft.db`，例如 `cp backups/officraft-20261020-060000-scheduled.db officraft.db`。
4. 確認同一個資料夾裡有當時的 `vault.key`。
5. 啟動 server：

   ```bash
   launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.officraft.serve.plist
   ```

## 升級

### 升級沒有生效或檢查更新失敗

升級不需要重跑安裝腳本。到 設定 › 系統更新與備份 按「檢查更新」，再按「升級到最新版」。升級完成後 server 會重新啟動，控制台頁面會自動重新載入。

- 出現「檢查更新失敗」：多半是連不到 GitHub（網路或 proxy 問題），稍後再試。
- 想使用預發佈版本：先打開「接收 Beta 版本」。
- 想自動升級：打開「自動更新」。檢查頻率是「更新檢查間隔」。

其他機器上的 warden 會在 server 升級後自行更新，通常在 15 分鐘內完成。見 [設定與參數](settings.md)。

## 移除

### 從控制台移除一台機器

到 監控 › 機器資訊，在機器名稱的選單選：

- **解除安裝**：請那台機器的 warden 移除自己。成功後機器變成離線，紀錄保留，之後可以再「安裝」。只有線上的機器可以解除安裝；那台機器上還有成員在線時，會先跳出警告，建議先讓那些成員停止。
- **刪除**：把機器從清單移除，它的憑證立即失效，在該機器上的成員也會斷線。機器上的程式不會被移除，無法復原，要恢復只能重新安裝。

要完整移除一台機器，先解除安裝，再刪除。

### 移除整個工作室

在 server 那台機器上執行：

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash -s -- --uninstall
```

- 預設不刪除資料：會停止 server，把 `~/.officraft/bin/` 與 `~/.officraft/server/` 底下的 `data`、`oc.toml`、`log` 搬到 `~/.officraft.bak-<時間>`，並印出還原指令。
- `~/.officraft/agents/`（成員工作目錄）與 `~/.officraft/warden/`（warden 與它的背景服務）不會被動到。warden 要另外用 `~/.officraft/warden/ocwarden teardown --canonical` 移除。
- 先加 `--dry-run` 可以只印出會做什麼，不做任何變更。
- 加 `--purge` 會直接刪除、無法復原。透過 `curl … | bash` 執行時無法輸入確認文字，`--purge` 一定要搭配 `--yes`，否則什麼都不會刪。

完整說明見 [安裝、升級與移除](install.md)。

## 相關文件

- [安裝、升級與移除](install.md)
- [保護你的工作室](security.md)
- [增加成員與發外包](members.md)
- [追蹤任務進度](tasks.md)
- [架構與運作原理](architecture.md)
