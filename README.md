# OffiCraft

**Craft your own AI office.** OffiCraft 是一間跑在你自己電腦上的 AI 工作室：你僱幾位常駐的 AI 成員，把事情**整件**交給他們，在一個網頁控制台裡看他們做到哪、在他們需要你點頭時回一句。跑的就是你機器上那個 Claude Code 或 Codex——你在它上面串好的設定照舊生效（Claude Code 的 skill / plugin / MCP 原封不動全都在）。

[![OffiCraft 介紹影片](https://img.youtube.com/vi/OQgLX5bAfR0/maxresdefault.jpg)](https://youtu.be/OQgLX5bAfR0)

---

## 安裝

支援 Apple Silicon 的 macOS、x86_64 與 arm64 的 Linux，以及 Windows（WSL2）；每個指令成功時會顯示「完成：步驟名稱」。

### macOS（Apple Silicon）

1. **開啟終端機**

   以 ⌘+空白鍵搜尋「終端機」開啟，指令以 ⌘V 貼上後按 Enter。

2. **安裝 Homebrew**

   需輸入本機登入密碼，輸入時不顯示字元。

   **執行位置：終端機**

   ```bash
   (command -v brew >/dev/null 2>&1 || [ -x /opt/homebrew/bin/brew ] || /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)") && echo '完成：安裝 Homebrew'
   ```

3. **設定 Homebrew 環境變數**

   **執行位置：終端機**

   ```bash
   { grep -qs 'brew shellenv' ~/.zprofile || echo 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> ~/.zprofile; } && eval "$(/opt/homebrew/bin/brew shellenv)" && brew --version >/dev/null && echo '完成：設定 Homebrew 環境變數'
   ```

4. **安裝 tmux**

   **執行位置：終端機**

   ```bash
   (command -v tmux >/dev/null 2>&1 || brew install tmux) && tmux -V && echo '完成：安裝 tmux'
   ```

5. **安裝並登入 Claude Code 或 Codex**

   依使用的工具執行對應指令，兩者皆使用時兩個指令皆執行；於瀏覽器完成登入後輸入 /exit 結束。

   **執行位置：終端機｜Claude Code**

   ```bash
   (command -v claude >/dev/null 2>&1 || [ -x "$HOME/.local/bin/claude" ] || curl -fsSL https://claude.ai/install.sh | bash) && echo '完成：安裝 Claude Code' && PATH="$HOME/.local/bin:$PATH" claude; PATH="$HOME/.local/bin:$PATH" claude auth status >/dev/null 2>&1 && echo '完成：登入 Claude Code'
   ```

   **執行位置：終端機｜Codex**

   ```bash
   (command -v codex >/dev/null 2>&1 || [ -x "$HOME/.local/bin/codex" ] || curl -fsSL https://chatgpt.com/codex/install.sh | sh) && echo '完成：安裝 Codex' && PATH="$HOME/.local/bin:$PATH" codex; PATH="$HOME/.local/bin:$PATH" codex login status >/dev/null 2>&1 && echo '完成：登入 Codex'
   ```

6. **安裝 OffiCraft**

   最後出現「✅ OffiCraft is installed and running in the background」即表示完成，以瀏覽器開啟其下「open:」後的設定連結並設定密碼；已安裝時略過此步驟，改由「設定 › 系統更新與備份」升級。

   **執行位置：終端機**

   ```bash
   curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
   ```

### Linux

1. **開啟終端機**

   以 Ctrl+Alt+T 開啟終端機或以 SSH 連線，指令以 Ctrl+Shift+V 貼上後按 Enter。

2. **安裝 tmux 與 curl**

   需輸入本機登入密碼，輸入時不顯示字元。

   **執行位置：終端機**

   ```bash
   sudo apt-get update && sudo apt-get install -y tmux curl && echo '完成：安裝 tmux 與 curl'
   ```

3. **安裝並登入 Claude Code 或 Codex**

   依使用的工具執行對應指令，兩者皆使用時兩個指令皆執行；於瀏覽器完成登入後輸入 /exit 結束。

   **執行位置：終端機｜Claude Code**

   ```bash
   (command -v claude >/dev/null 2>&1 || [ -x "$HOME/.local/bin/claude" ] || curl -fsSL https://claude.ai/install.sh | bash) && echo '完成：安裝 Claude Code' && PATH="$HOME/.local/bin:$PATH" claude; PATH="$HOME/.local/bin:$PATH" claude auth status >/dev/null 2>&1 && echo '完成：登入 Claude Code'
   ```

   **執行位置：終端機｜Codex**

   ```bash
   (command -v codex >/dev/null 2>&1 || [ -x "$HOME/.local/bin/codex" ] || curl -fsSL https://chatgpt.com/codex/install.sh | sh) && echo '完成：安裝 Codex' && PATH="$HOME/.local/bin:$PATH" codex; PATH="$HOME/.local/bin:$PATH" codex login status >/dev/null 2>&1 && echo '完成：登入 Codex'
   ```

4. **安裝 OffiCraft**

   最後出現「✅ OffiCraft is installed and running in the background」即表示完成，以瀏覽器開啟其下「open:」後的設定連結並設定密碼；已安裝時略過此步驟，改由「設定 › 系統更新與備份」升級。

   **執行位置：終端機**

   ```bash
   curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
   ```

### Windows（WSL2）

1. **以系統管理員身分開啟 PowerShell**

   於開始選單搜尋「PowerShell」並按右鍵選擇「以系統管理員身分執行」，指令以右鍵貼上後按 Enter。

   **執行位置：PowerShell（系統管理員）**

   ```powershell
   if (([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { echo '完成：以系統管理員身分開啟 PowerShell' }
   ```

2. **安裝 WSL2 與 Ubuntu**

   完成後重新開機；已安裝 Ubuntu 時略過此步驟。

   **執行位置：PowerShell（系統管理員）**

   ```powershell
   wsl --install -d Ubuntu; if ($?) { echo '完成：安裝 WSL2 與 Ubuntu' }
   ```

3. **建立 Ubuntu 使用者帳號**

   於開始選單開啟「Ubuntu」並依提示設定使用者名稱與密碼，出現「使用者名稱@電腦名稱:~$」即表示已進入 Ubuntu。

   **執行位置：Ubuntu**

   ```bash
   [ "$(id -u)" != 0 ] && whoami && echo '完成：建立 Ubuntu 使用者帳號'
   ```

4. **安裝 tmux 與 curl**

   需輸入 Ubuntu 使用者密碼，輸入時不顯示字元。

   **執行位置：Ubuntu**

   ```bash
   sudo apt-get update && sudo apt-get install -y tmux curl && echo '完成：安裝 tmux 與 curl'
   ```

5. **安裝並登入 Claude Code 或 Codex**

   依使用的工具執行對應指令，兩者皆使用時兩個指令皆執行；於瀏覽器完成登入後輸入 /exit 結束。

   **執行位置：Ubuntu｜Claude Code**

   ```bash
   (command -v claude >/dev/null 2>&1 || [ -x "$HOME/.local/bin/claude" ] || curl -fsSL https://claude.ai/install.sh | bash) && echo '完成：安裝 Claude Code' && PATH="$HOME/.local/bin:$PATH" claude; PATH="$HOME/.local/bin:$PATH" claude auth status >/dev/null 2>&1 && echo '完成：登入 Claude Code'
   ```

   **執行位置：Ubuntu｜Codex**

   ```bash
   (command -v codex >/dev/null 2>&1 || [ -x "$HOME/.local/bin/codex" ] || curl -fsSL https://chatgpt.com/codex/install.sh | sh) && echo '完成：安裝 Codex' && PATH="$HOME/.local/bin:$PATH" codex; PATH="$HOME/.local/bin:$PATH" codex login status >/dev/null 2>&1 && echo '完成：登入 Codex'
   ```

6. **安裝 OffiCraft**

   最後出現「✅ OffiCraft is installed and running in the background」即表示完成，以 Windows 瀏覽器開啟其下「open:」後的設定連結並設定密碼；已安裝時略過此步驟，改由「設定 › 系統更新與備份」升級。

   **執行位置：Ubuntu**

   ```bash
   curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
   ```

---

## 從手機或外面用

server 只綁 `127.0.0.1`，這一點不能改。要從手機或外面連，把本機的 7755 埠轉出去：開一條你自己的 tunnel（如 cloudflared，會給你一個公開 HTTPS 網址），或用 Tailscale 這類私有網路的轉發功能（如 `tailscale serve`）；只加入私有網路、不做轉發是連不到的。開放前先設好夠強的密碼與兩步驟驗證或通行密鑰。完整步驟（含加到手機主畫面）見 [在手機上用](docs/guide/mobile.md)；對外開放前該設好的保護見 [保護你的工作室](docs/guide/security.md)。

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
