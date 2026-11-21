# OffiCraft

**Craft your own AI office.** OffiCraft 是一間跑在你自己電腦上的 AI 工作室：你僱幾位常駐的 AI 成員，把事情**整件**交給他們，在一個網頁控制台裡看他們做到哪、在他們需要你點頭時回一句。跑的就是你機器上那個 Claude Code 或 Codex——你在它上面串好的設定照舊生效（Claude Code 的 skill / plugin / MCP 原封不動全都在）。

[![OffiCraft 介紹影片](https://img.youtube.com/vi/OQgLX5bAfR0/maxresdefault.jpg)](https://youtu.be/OQgLX5bAfR0)

---

## 安裝

### 系統需求

- macOS（Apple Silicon）、Linux（x86_64、arm64），或 Windows 10（版本 2004 以上）／11（透過 WSL2）
- tmux 3.0 以上
- Claude Code 或 Codex，至少一種，並已登入

### Windows：準備 WSL2

OffiCraft 在 Windows 上透過 WSL2 中的 Ubuntu 執行。macOS 與 Linux 使用者略過此節。

1. 以系統管理員身分開啟 PowerShell，執行：

   ```powershell
   wsl --install -d Ubuntu
   ```

2. 首次安裝 WSL 需重新開機。之後從開始選單開啟「Ubuntu」，依提示設定 Ubuntu 的使用者名稱與密碼。

之後的所有指令都在 Ubuntu 視窗中執行，從開始選單點「Ubuntu」開啟。

### 步驟 1：安裝 tmux

macOS（需先安裝 [Homebrew](https://brew.sh)）：

```bash
brew install tmux
```

Linux、WSL：

```bash
sudo apt-get update && sudo apt-get install -y tmux curl
```

確認：執行下列指令，顯示版本號即表示安裝成功。

```bash
tmux -V
```

### 步驟 2：安裝並登入 Claude Code 或 Codex

依使用的工具擇一安裝，也可兩者都安裝。

Claude Code：

```bash
curl -fsSL https://claude.ai/install.sh | bash
```

安裝完成後開啟新的終端機視窗，執行 `claude`，依提示於瀏覽器完成登入。

Codex：

```bash
curl -fsSL https://chatgpt.com/codex/install.sh | sh
```

安裝完成後開啟新的終端機視窗，執行 `codex`，選擇「Sign in with ChatGPT」完成登入。

確認：執行對應的指令，顯示版本號與登入資訊即表示安裝並登入成功。

```bash
claude --version && claude auth status --text
```

```bash
codex --version && codex login status
```

### 步驟 3：安裝 OffiCraft

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
```

安裝程式會先檢查系統需求，缺少項目時停止並列出需要補裝的工具。

### 步驟 4：開啟設定連結

安裝完成時，終端機會輸出下列訊息與設定連結：

```
[install] ✅ OffiCraft is installed and running in the background (…)

  open:    http://127.0.0.1:7755/?code=…
```

以瀏覽器開啟該連結並設定登入密碼，即進入控制台。Windows 使用者以 Windows 的瀏覽器開啟。

### 確認安裝成功

以設定的密碼登入控制台後，「辦公室」頁面顯示助理 Mira 在線上，並能回覆聊天訊息，即表示 OffiCraft 安裝完成。

### 更新

- tmux、Claude Code、Codex 已安裝者，略過步驟 1、2。
- OffiCraft 的更新在控制台「設定 › 系統更新與備份」進行，不需重新執行安裝指令。

### 移除

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash -s -- --uninstall
```

移除時，程式、資料庫與背景服務設定會搬到 `~/.officraft.bak-<時間>`，不會刪除資料。

### 疑難排解

**出現 `command not found: claude` 或 `command not found: codex`**

1. 開啟新的終端機視窗後重新執行。
2. 仍出現時，執行下列指令（macOS），Linux、WSL 將 `~/.zshrc` 改為 `~/.bashrc`，再開啟新的終端機視窗：

   ```bash
   echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc
   ```

**WSL 中安裝程式提示 systemd 未啟用**

1. 在 Ubuntu 視窗執行：

   ```bash
   printf '[boot]\nsystemd=true\n' | sudo tee -a /etc/wsl.conf
   ```

2. 在 PowerShell 執行：

   ```powershell
   wsl --shutdown
   ```

3. 重新開啟 Ubuntu 視窗，再執行一次步驟 3 的安裝指令。

**開始選單有多個 Ubuntu**

1. 在 PowerShell 執行下列指令，查看每一套 Ubuntu 的名稱：

   ```powershell
   wsl -l -v
   ```

2. 逐一以名稱執行下列指令，顯示 `.officraft` 的那一套就是安裝 OffiCraft 的 Ubuntu：

   ```powershell
   wsl -d <名稱> --cd ~ -- ls -d .officraft
   ```

3. 之後都以下列指令開啟那一套 Ubuntu：

   ```powershell
   wsl -d <名稱>
   ```

**安裝程式提示 7755 埠已被占用**

關閉占用 7755 埠的程式後，執行下列指令重新安裝（程式此時已安裝，需加上 `--force`）：

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash -s -- --force
```

---

## 從這台電腦以外的地方連線

server 只綁 `127.0.0.1`，這一點不能改。要從手機或其他電腦連線，對 Mira 說「我想從這台電腦以外的地方連到控制台」。說明見 [在手機上用](docs/guide/mobile.md)。

---

## 使用說明

完整的使用說明在 **[docs/guide/](docs/guide/)**，控制台「設定 › 使用說明」讀的也是同一份。依你現在要做的事，從對應的一組開始讀。

### 開始使用

第一次接觸 OffiCraft、準備安裝，或剛裝好想走一遍時讀。

- [為什麼是 OffiCraft](docs/guide/why.md) — 判斷 OffiCraft 適不適合你。
- [安裝、升級與移除](docs/guide/install.md) — 前置需求、安裝、升級、移除，以及加入或移除其他機器。
- [你的第一個辦公室](docs/guide/quickstart.md) — 十分鐘走完設密碼、交出第一件事、回覆第一張請示卡。
- [保護你的工作室](docs/guide/security.md) — 從手機或外面連進來之前，該設好的密碼、兩步驟驗證與通行密鑰。

### 日常使用

每天交代工作、回覆請示卡、看進度與用量時讀。

- [交代工作與回覆請示卡](docs/guide/daily.md) — 聊天、附件、@ 成員、把成員分群組、回覆請示卡，以及在成員交來的文件上寫註解。
- [追蹤任務進度](docs/guide/tasks.md) — 看懂任務的狀態、步驟與關卡，驗收成果或強制結案。
- [管理 AI 帳號與用量](docs/guide/usage.md) — 在「監控」分頁看每個帳號的用量、估計花費與每位成員用的帳號與模型，替機器登入帳號、升級 Claude Code。
- [在手機上用](docs/guide/mobile.md) — 從手機連上控制台、加到主畫面、開推播通知。

### 讓工作室自己運轉

想把重複、定時或從外部來的工作交出去，或想增加成員時讀。

- [讓 AI 照 SOP 做重複的事（任務手冊）](docs/guide/sop.md) — 寫任務手冊，決定哪些步驟要停下來等你。
- [定時做某件事（定期訊息）](docs/guide/schedule.md) — 設定期訊息，讓成員在固定時刻收到預先寫好的交代（成員要在線上才會處理）。
- [讓外部事件自動送給成員（回呼端點）](docs/guide/webhook.md) — 建回呼端點，把其他服務送來的通知（例如 Slack 的新訊息）交給負責的成員。
- [讓成員記住你教過的事（傳承）](docs/guide/lore.md) — 寫下你是誰、哪些事不能碰（使用者自訂）、各角色職責，以及成員學到的經驗（傳承）。
- [把帳號金鑰交給成員用（密鑰）](docs/guide/secrets.md) — 存密鑰，讓成員取用而不必貼進聊天。
- [增加成員與發外包](docs/guide/members.md) — 新增正職、發外包、喚醒與停止成員、換模型與機器、看懂狀態圖示，以及判斷該外包還是加常駐成員。

### 查詢與參考

想查某個欄位、參數或名詞，或想知道底層怎麼運作時讀。

- [介面說明](docs/guide/interface.md) — 每個分頁與欄位的對照。
- [設定與參數](docs/guide/settings.md) — 設定的六個子頁與每格參數的範圍。
- [主題（外觀與用語）](docs/guide/theme.md) — 換外觀、依時段切換、背景動畫、自訂與匯入匯出主題、應景主題。
- [架構與運作原理](docs/guide/architecture.md) — server、機器與成員怎麼分工。
- [名詞表](docs/guide/glossary.md) — OffiCraft 用到的名詞。

### 遇到問題

安裝或使用時遇到問題，或要自己備份、還原資料庫時讀。

- [常見問題與排解](docs/guide/troubleshooting.md) — 安裝失敗、成員起不來、回呼端點收不到事件、升級沒生效，以及備份與還原。
