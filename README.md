# OffiCraft

**Craft your own AI office.** OffiCraft 是一間跑在你自己電腦上的 AI 工作室：你僱幾位常駐的 AI 成員，把事情**整件**交給他們，在一個網頁控制台裡看他們做到哪、在他們需要你點頭時回一句。跑的就是你機器上那個 Claude Code 或 Codex——你在它上面串好的設定照舊生效（Claude Code 的 skill / plugin / MCP 原封不動全都在）。

[![OffiCraft 介紹影片](https://img.youtube.com/vi/OQgLX5bAfR0/maxresdefault.jpg)](https://youtu.be/OQgLX5bAfR0)

---

## 安裝

支援 Apple Silicon 的 Mac、x86_64 與 arm64 的 Linux、Windows（WSL2 裡的 Ubuntu）。選你的電腦，照步驟做。

### macOS（Apple Silicon）

- 按 ⌘＋空白鍵，輸入「終端機」後按 Enter，打開終端機。
- 每個指令：複製（滑鼠移到指令框上，右上角有複製按鈕），在終端機按 ⌘V 貼上，再按 Enter。
- 輸入密碼時畫面不會顯示，打完按 Enter。

1. 安裝 Homebrew（跳出安裝「指令列開發者工具」時，按「安裝」）：

   ```bash
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
   ```

2. 接著貼上：

   ```bash
   echo 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> ~/.zprofile && eval "$(/opt/homebrew/bin/brew shellenv)"
   ```

3. 安裝 tmux：

   ```bash
   brew install tmux
   ```

4. 安裝 Claude Code：

   ```bash
   curl -fsSL https://claude.ai/install.sh | bash
   ```

5. 按 ⌘N 開新的終端機視窗，貼上 `claude`，照畫面在瀏覽器登入；登入後輸入 `/exit` 按 Enter：

   ```bash
   claude
   ```

   出現「command not found: claude」時，貼上這行，再重新貼 `claude`：

   ```bash
   echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc && source ~/.zshrc
   ```

6. 安裝 OffiCraft：

   ```bash
   curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
   ```

7. 按住 ⌘ 點終端機最後印出的 `http://127.0.0.1:7755/?code=…` 連結，在瀏覽器設定 owner 密碼。設好後助理 Mira 會自動醒來。

### Linux

- 按 Ctrl＋Alt＋T（或從應用程式打開「終端機」），打開終端機；也可以用 SSH 連進那台機器。
- 每個指令：複製（滑鼠移到指令框上，右上角有複製按鈕），在終端機按 Ctrl＋Shift＋V 貼上，再按 Enter。
- 輸入密碼時畫面不會顯示，打完按 Enter。

1. 安裝 tmux 與 curl（Ubuntu／Debian；其他 Linux 用各自的安裝工具裝 `tmux` 與 `curl`）：

   ```bash
   sudo apt-get update && sudo apt-get install -y tmux curl
   ```

2. 安裝 Claude Code：

   ```bash
   curl -fsSL https://claude.ai/install.sh | bash
   ```

3. 開新的終端機視窗，貼上 `claude`，照畫面在瀏覽器登入；登入後輸入 `/exit` 按 Enter：

   ```bash
   claude
   ```

   出現「command not found: claude」時，貼上這行，再重新貼 `claude`：

   ```bash
   echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
   ```

4. 安裝 OffiCraft：

   ```bash
   curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
   ```

5. 在這台電腦的瀏覽器打開終端機最後印出的 `http://127.0.0.1:7755/?code=…` 連結，設定 owner 密碼。設好後助理 Mira 會自動醒來。

### Windows（WSL2）

- 第 1 步在 PowerShell，其他步驟都在 Ubuntu 視窗（從開始選單點「Ubuntu」打開）；不要貼在 PowerShell。
- 每個指令：複製（滑鼠移到指令框上，右上角有複製按鈕），在視窗裡按滑鼠右鍵貼上，再按 Enter。
- 輸入密碼時畫面不會顯示，打完按 Enter。

1. 開始選單搜尋「PowerShell」→ 按右鍵 →「以系統管理員身分執行」，貼上這行；完成後重新開機：

   ```powershell
   wsl --install -d Ubuntu
   ```

2. 從開始選單打開「Ubuntu」，設定使用者名稱與密碼。
3. 貼上這行：顯示 `OK` 就跳到第 5 步；顯示 `OFF` 就做第 4 步。

   ```bash
   [ -d /run/systemd/system ] && echo OK || echo OFF
   ```

4. 依序貼上這兩行，再從開始選單重新打開「Ubuntu」，重做第 3 步，確認顯示 `OK`。第二行會停掉這台 Windows 上所有正在執行的 Linux。

   ```bash
   sudo sh -c 'f=/etc/wsl.conf; touch "$f"; awk "/^\[boot\]/{print; print \"systemd=true\"; b=1; next} /^systemd *=/{next} {print} END{if(!b) printf \"\n[boot]\nsystemd=true\n\"}" "$f" > /tmp/oc-wsl.conf.new && cat /tmp/oc-wsl.conf.new > "$f"'
   ```

   ```bash
   wsl.exe --shutdown
   ```

   一直顯示 `OFF` 時，在 PowerShell 貼上這行，再重做第 4 步：

   ```powershell
   wsl --update
   ```

5. 安裝 tmux 與 curl：

   ```bash
   sudo apt-get update && sudo apt-get install -y tmux curl
   ```

6. 安裝 Claude Code：

   ```bash
   curl -fsSL https://claude.ai/install.sh | bash
   ```

7. 關掉 Ubuntu 視窗，從開始選單重新打開「Ubuntu」，貼上 `claude`，照畫面在瀏覽器登入；登入後輸入 `/exit` 按 Enter：

   ```bash
   claude
   ```

   出現「command not found: claude」時，貼上這行，再重新貼 `claude`：

   ```bash
   echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc && source ~/.bashrc
   ```

8. 安裝 OffiCraft：

   ```bash
   curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
   ```

9. 把 Ubuntu 視窗最後印出的 `http://127.0.0.1:7755/?code=…` 連結複製到 Windows 的瀏覽器打開，設定 owner 密碼。設好後助理 Mira 會自動醒來。
10. 安裝時印出 `WARN: WSL keep-alive not set up` 時，關掉所有 Ubuntu 視窗後成員會離線；請保留一個 Ubuntu 視窗開著。

裝了不只一套 Linux 時：在 PowerShell 用 `wsl -l -v` 查名稱，再用 `wsl -d 名稱` 打開要裝的那一套。

### 安裝失敗時

看終端機最後印出的原因。最常見的是 7755 埠已經被其他程式占用。

---

## 跟直接開 Claude Code 有什麼不一樣

- **常駐的成員，不是一次性 session** — 成員有穩定身分與記憶，關掉再開還是同一個人，記得你的偏好、專案、上次做到哪。你不必每次重講你是誰。
- **教過一次的事不用再教** — 成員做事學到的判斷、你糾正過的地方、你的偏好，會寫成「傳承」留在工作室裡。下一位接手的成員、換了模型的成員，開工就讀得到，同樣的錯不會再犯一次。
- **每天只要回答準備好的問題** — 需要你拍板的事，成員整理成一張「請示卡」：背景、選項與它的建議都寫好，手機上點一下就決定（要你一次圈好幾項的多選卡才需要勾完按送出），其餘它自己扛。你不用盯著螢幕。
- **有固定做法的事照 SOP 走** — 重複出現的工作寫成「任務手冊」。交代一件事，成員會自己找到對應的手冊，照手冊的步驟做、交給手冊指定的人。每件事拆成有完成準則的步驟，現在第幾步、卡在哪、還剩哪些，一眼看到；換人接手也從正確的下一步繼續。
- **成員之間自己協作** — 每位成員知道同事負責什麼、在哪台電腦上，需要幫忙時直接找對方，不用你轉達。成員可以分佈在你的多台電腦上，一間工作室橫跨好幾台機器。
- **定時的事、外面的事自己進來** — 「每天早上整理昨天的進度」這類工作設成定期訊息，排程記在工作室裡、不在某台電腦上，成員換模型、換電腦照樣準時。CI、GitHub、Slack 的事件透過 webhook 直接送給負責的成員；每個 webhook 有一句用途說明，外部內容要成員做用途以外的事，成員會停下來問你。
- **成果拿來就能看、能改** — 你可以直接把檔案丟給成員；成員交回的報告、圖表、可互動的 HTML 頁面附在聊天、請示卡或任務上，一點就開。在文件上選一段寫註解，一鍵送回給成員修改；改了哪裡，用差異畫面看前後對照。
- **控制台與密鑰都有保護** — 控制台要登入，支援兩步驟驗證與通行密鑰（Touch ID 這類）。給成員用的 API token 存成「密鑰」，存進去之後畫面上不再顯示，成員用工具取用，不必貼進聊天。
- **一個控制台俯視整間工作室** — 辦公室、請示、任務、傳承、監控各一頁，誰在忙什麼、卡在哪、花了多少額度，全在一處。

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
