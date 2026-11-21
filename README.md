# OffiCraft

**Craft your own AI office.** OffiCraft 是一間跑在你自己電腦上的 AI 工作室：你僱幾位常駐的 AI 成員，把事情**整件**交給他們，在一個網頁控制台裡看他們做到哪、在他們需要你點頭時回一句。跑的就是你機器上那個 Claude Code 或 Codex——你在它上面串好的設定照舊生效（Claude Code 的 skill / plugin / MCP 原封不動全都在）。

[![OffiCraft 介紹影片](https://img.youtube.com/vi/OQgLX5bAfR0/maxresdefault.jpg)](https://youtu.be/OQgLX5bAfR0)

---

## 安裝

依你的作業系統，選下面其中一節照著做。每一節都從頭寫到尾，只看那一節就能裝好。支援 Apple Silicon（M 系列晶片）的 Mac、x86_64 與 arm64 的 Linux，以及 Windows 上透過 WSL2 執行的 Ubuntu；不支援直接在 Windows 上執行。

每位成員底下就是一個跑在 `tmux` 裡的 Claude Code 或 Codex session，所以每種系統都要先準備兩樣東西：`tmux` 3.0 以上，以及已經登入的 Claude Code（2.1.287 以上）或 Codex，至少一種。下面以 Claude Code 為例。

### macOS（Apple Silicon）

1. **打開「終端機」App。** 可以用 Spotlight（⌘ + 空白鍵）搜尋「終端機」。下面每一步的指令都貼在這個視窗裡，按 Enter 執行。
2. **裝 `tmux`。** 需要先有 Homebrew（macOS 上常用的軟體安裝工具）：

   ```bash
   brew install tmux
   ```

3. **裝 Claude Code。** 需要先有 Node.js（`npm` 隨 Node.js 一起安裝）：

   ```bash
   npm install -g @anthropic-ai/claude-code
   ```

4. **登入 Claude Code。** 執行 `claude`，依畫面指示登入。成員要用這個登入才能工作。

   ```bash
   claude
   ```

5. **安裝 OffiCraft：**

   ```bash
   curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
   ```

   這一行會下載最新版、確認檔案完整無誤後才安裝；這台電腦缺東西時會停下來，並印出要補裝什麼。

6. **打開設定連結。** 安裝成功時，終端機最後會印出一行開頭是 `http://127.0.0.1:7755/?code=` 的設定連結。在瀏覽器打開這條連結，設定 owner 密碼。
7. **密碼設好後，系統會自動把這台電腦準備好，並喚醒助理 Mira。** OffiCraft 在背景一直執行，關掉終端機也不會停，電腦重開後也會自動啟動。

### Linux

1. **打開終端機。** 用桌面環境的終端機，或用 SSH 連進那台機器。下面每一步的指令都貼在終端機裡，按 Enter 執行。
2. **確認 systemd 正在執行。** OffiCraft 在 Linux 上以 systemd 的使用者服務在背景執行；systemd 沒有在執行時，安裝腳本會停下。大多數 Linux 發行版預設就有。
3. **裝 `tmux`。** Ubuntu 與 Debian 用 `apt-get`，`sudo` 會要求輸入這台電腦的登入密碼；其他發行版用各自的軟體安裝工具：

   ```bash
   sudo apt-get install -y tmux
   ```

4. **裝 Claude Code。** 需要先有 Node.js（`npm` 隨 Node.js 一起安裝）：

   ```bash
   npm install -g @anthropic-ai/claude-code
   ```

   Linux 上的 `claude` 通常裝在 `~/.local/bin`，安裝腳本會到這個資料夾尋找。

5. **登入 Claude Code。** 執行 `claude`，依畫面指示登入：

   ```bash
   claude
   ```

6. **安裝 OffiCraft：**

   ```bash
   curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
   ```

   這一行會下載最新版、確認檔案完整無誤後才安裝；這台電腦缺東西時會停下來，並印出要補裝什麼。

7. **打開設定連結。** 安裝成功時，終端機最後會印出一行開頭是 `http://127.0.0.1:7755/?code=` 的設定連結。在這台電腦的瀏覽器打開這條連結，設定 owner 密碼。
8. **密碼設好後，系統會自動把這台電腦準備好，並喚醒助理 Mira。** 安裝時會打開 linger（讓使用者的背景服務在登出後繼續執行、開機時自動啟動），所以關掉終端機、登出或重開機之後，OffiCraft 仍在背景執行。

### Windows（WSL2）

OffiCraft 裝在 WSL2 的 Ubuntu 裡。WSL2 是 Windows 內建的功能，在 Windows 裡執行一套完整的 Linux。

1. **安裝 WSL2 與 Ubuntu。** 在開始選單搜尋 PowerShell，按右鍵選「以系統管理員身分執行」，輸入：

   ```powershell
   wsl --install -d Ubuntu
   ```

   裝好後重新開機，再從開始選單打開「Ubuntu」，依提示建立 Ubuntu 的使用者名稱與密碼。

2. **確認 WSL 的版本是 0.67.6 以上。** 舊版不支援下一步的 systemd。在 PowerShell 查看版本：

   ```powershell
   wsl --version
   ```

   版本太舊時更新：

   ```powershell
   wsl --update
   ```

3. **在 WSL2 裡打開 systemd。** OffiCraft 在 Ubuntu 裡以 systemd 的使用者服務在背景執行，而 WSL2 預設不啟動 systemd；systemd 沒有在執行時，安裝腳本會停下。在 Ubuntu 視窗裡用系統管理員權限打開 `/etc/wsl.conf`：

   ```bash
   sudo nano /etc/wsl.conf
   ```

   加上這兩行後存檔（nano 裡按 Ctrl+O、Enter 存檔，再按 Ctrl+X 離開）：

   ```ini
   [boot]
   systemd=true
   ```

   接著在 PowerShell 執行以下指令關閉 WSL2，再重新打開 Ubuntu 視窗，設定才會生效：

   ```powershell
   wsl.exe --shutdown
   ```

   **注意：**`wsl.exe --shutdown` 會關閉這台 Windows 上所有正在執行的 WSL2 Linux，裡面已經在執行的 OffiCraft 與成員也會一起停止。

4. **之後的指令都貼在 Ubuntu 視窗裡，不要貼在 PowerShell 或命令提示字元。** 這台 Windows 裝了不只一套 Linux 時，開始選單的「Ubuntu」不一定是你要的那一套：在 PowerShell 執行 `wsl -l -v` 列出每一套的名稱，再用 `wsl -d <名稱>` 打開要裝 OffiCraft 的那一套，例如 `wsl -d Ubuntu`。
5. **在 Ubuntu 裡裝 `tmux`。** `sudo` 會要求輸入剛才建立的 Ubuntu 密碼：

   ```bash
   sudo apt-get install -y tmux
   ```

6. **在 Ubuntu 裡裝 Claude Code。** 需要先在 Ubuntu 裡有 Node.js（`npm` 隨 Node.js 一起安裝）：

   ```bash
   npm install -g @anthropic-ai/claude-code
   ```

   裝在 Windows 那一邊的 Claude Code 或 Codex，OffiCraft 找不到、也用不到；一定要裝在 Ubuntu 裡。

7. **在 Ubuntu 裡登入 Claude Code。** 執行 `claude`，依畫面指示登入：

   ```bash
   claude
   ```

8. **安裝 OffiCraft：**

   ```bash
   curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
   ```

   這一行會下載最新版、確認檔案完整無誤後才安裝；Ubuntu 裡缺東西時會停下來，並印出要補裝什麼。

9. **在 Windows 的瀏覽器打開設定連結。** 安裝成功時，Ubuntu 視窗最後會印出一行開頭是 `http://127.0.0.1:7755/?code=` 的設定連結。WSL2 預設會把 Windows 上的 `127.0.0.1` 連線轉進 WSL2，所以直接在 Windows 的瀏覽器打開這條連結，設定 owner 密碼。
10. **密碼設好後，系統會自動把這台電腦準備好，並喚醒助理 Mira。** 安裝時，OffiCraft 在 Windows 的「啟動」資料夾放一個保持 WSL2 執行的檔案，所以關掉所有 Ubuntu 視窗後，成員仍繼續在線；每次登入 Windows 時，這個檔案會再執行一次。安裝時印出 `WARN: WSL keep-alive not set up` 時，這個檔案沒有設定好，關掉所有 Ubuntu 視窗後成員會離線。

### 安裝沒有成功時

終端機會印出原因。最常見的原因是 7755 埠已經被其他程式占用。

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
