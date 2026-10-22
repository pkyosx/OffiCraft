# OffiCraft

**Craft your own AI office.** OffiCraft 是一間跑在你自己 Mac 上的 AI 工作室：你僱幾位常駐的 AI 成員，把事情**整件**交給他們，在一個網頁控制台裡看他們做到哪、在他們需要你點頭時回一句。跑的就是你機器上那個 Claude Code 或 Codex——你在它上面串好的設定照舊生效（Claude Code 的 skill / plugin / MCP 原封不動全都在）。

[![OffiCraft 介紹影片](https://img.youtube.com/vi/RAZuchCozVE/maxresdefault.jpg)](https://youtu.be/RAZuchCozVE)

---

## 跟直接開 Claude Code 有什麼不一樣

- **常駐的成員，不是一次性 session** — 成員有穩定身分與記憶，關掉再開還是同一個人，記得你的偏好、專案、上次做到哪。你不必每次重講你是誰。
- **教過一次的事不用再教** — 成員做事學到的判斷、你糾正過的地方、你的偏好，會寫成「傳承」留在工作室裡。下一位接手的成員、換了模型的成員，開工就讀得到，同樣的錯不會再犯一次。
- **每天只要回答準備好的問題** — 需要你拍板的事，成員整理成一張「請示卡」：背景、選項與它的建議都寫好，手機上點一下就決定（要你一次圈好幾項的多選卡才需要勾完按送出），其餘它自己扛。你不用盯著螢幕。
- **有固定做法的事照 SOP 走** — 重複出現的工作寫成「任務手冊」。交代一件事，成員會自己找到對應的手冊，照手冊的步驟做、交給手冊指定的人。每件事拆成有完成準則的步驟，現在第幾步、卡在哪、還剩哪些，一眼看到；換人接手也從正確的下一步繼續。
- **成員之間自己協作** — 每位成員知道同事負責什麼、在哪台電腦上，需要幫忙時直接找對方，不用你轉達。成員可以分佈在你的多台 Mac 上，一間工作室橫跨好幾台機器。
- **定時的事、外面的事自己進來** — 「每天早上整理昨天的進度」這類工作設成定期訊息，排程記在工作室裡、不在某台電腦上，成員換模型、換電腦照樣準時。CI、GitHub、Slack 的事件透過 webhook 直接送給負責的成員；每個 webhook 有一句用途說明，外部內容要成員做用途以外的事，成員會停下來問你。
- **成果拿來就能看、能改** — 你可以直接把檔案丟給成員；成員交回的報告、圖表、可互動的 HTML 頁面附在聊天、請示卡或任務上，一點就開。在文件上選一段寫註解，一鍵送回給成員修改；改了哪裡，用差異畫面看前後對照。
- **控制台與密鑰都有保護** — 控制台要登入，支援兩步驟驗證與通行密鑰（Touch ID 這類）。給成員用的 API token 存成「密鑰」，存進去之後畫面上不再顯示，成員用工具取用，不必貼進聊天。
- **一個控制台俯視整間工作室** — 辦公室、請示、任務、傳承、監控各一頁，誰在忙什麼、卡在哪、花了多少額度，全在一處。

---

## 安裝（macOS Apple Silicon）

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
```

裝完會印出一行一次性設定連結（`http://127.0.0.1:7755/?code=…`），打開它設個 owner 密碼就進控制台了。完整前置需求、升級與移除見 [安裝、升級與移除](docs/guide/install.md)。

> 需要 `tmux`，以及已登入的 `claude`（Claude Code CLI）或 `codex`（Codex CLI）至少一種——每位成員底下就是一個跑在 tmux 裡的 Claude Code 或 Codex session。

---

## 從手機或外面用

server 只綁 `127.0.0.1`，預設不對外。要從手機或外面連，開一條你自己的 tunnel（如 cloudflared，會給你一個公開 HTTPS 網址）或走 VPN，設一組夠強的密碼即可。完整步驟（含加到手機主畫面）見 [在手機上用控制台](docs/guide/mobile.md)。

---

## 使用說明

完整的使用者文件在 **[docs/guide/](docs/guide/)**，控制台裡的「使用說明」分頁讀的也是同一份：

- **為什麼是 OffiCraft（從這裡開始）** → [為什麼是 OffiCraft](docs/guide/why.md)
- **十分鐘走完第一次** → [你的第一個辦公室](docs/guide/quickstart.md)
- **安裝、升級與移除** → [安裝、升級與移除](docs/guide/install.md)
- **介面每個欄位是什麼** → [介面說明](docs/guide/interface.md)
- **任務怎麼運作** → [任務是怎麼運作的](docs/guide/tasks.md)
- **成員與外包** → [成員與外包](docs/guide/members.md)
- **主題（外觀與用語）** → [主題（外觀與用語）](docs/guide/theme.md)
- **設定與參數** → [設定與參數](docs/guide/settings.md)
- **建議用法** → [建議用法](docs/guide/best-practices.md)
- **在手機上用** → [在手機上用控制台](docs/guide/mobile.md)
- **底下怎麼運作** → [架構與運作原理](docs/guide/architecture.md)
- **卡住了** → [常見問題與排解](docs/guide/troubleshooting.md)
- **名詞表** → [名詞表](docs/guide/glossary.md)
