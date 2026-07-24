# OffiCraft

**Craft your own AI office.** OffiCraft 是一間跑在你自己 Mac 上的 AI 工作室：你僱幾位常駐的 AI 成員，把事情**整件**交給他們，在一個網頁控制台裡看他們做到哪、在他們需要你點頭時回一句。跑的就是你機器上那個 Claude Code——你原本串好的 skill / plugin / MCP 原封不動全都在。

[![OffiCraft 介紹影片](https://img.youtube.com/vi/RAZuchCozVE/maxresdefault.jpg)](https://youtu.be/RAZuchCozVE)

---

## 跟直接開 Claude Code 有什麼不一樣

- **常駐的成員，不是一次性 session** — 成員有穩定身分與記憶，關掉再開還是同一個人，記得你的偏好、專案、上次做到哪；學到的東西會固化下來，換手也接得住。你不必每次重講你是誰。
- **只在該你決定時被叫住** — 需要你拍板的事，成員收斂成一張「請示卡」放進一個地方，手機點一下就決定，其餘它自己扛。你不用盯著螢幕。
- **任務進度一目瞭然，接手不掉棒** — 每件事拆成有「完成準則」的節點，現在第幾步、卡在哪、還剩哪些，一眼看到；跑再久也不怕忘，任何接手的成員都從正確的下一步繼續，多位成員還能平行分工。
- **一個控制台俯視整間工作室** — 辦公室、請示、任務、監控四頁，誰在忙什麼、卡在哪、花了多少，全在一處。
- **檔案雙向流動，成果一點就看** — 你可以直接把檔案丟給成員；也能請他們做出 HTML 文件、報告、圖表，附在聊天、請示卡或任務上，一點擊就是完整可讀的成果，不用在終端機裡翻。
- **跨電腦協作** — 成員可以分佈在你的多台電腦上，透過 server 彼此協作；一間工作室橫跨好幾台機器，把它們變成同一間公司的不同辦公室。

---

## 安裝（macOS Apple Silicon）

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
```

裝完會印出一行一次性設定連結（`http://127.0.0.1:7755/?code=…`），打開它設個 owner 密碼就進控制台了。完整前置需求、升級與移除見 [安裝、升級與移除](docs/guide/install.md)。

> 需要已登入的 `claude`（Claude Code CLI）與 `tmux`——每位成員底下就是一個 Claude Code session。

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
