# OffiCraft

[![Release](https://img.shields.io/github/v/release/pkyosx/OffiCraft?style=flat-square)](https://github.com/pkyosx/OffiCraft/releases)
![Platform](https://img.shields.io/badge/macOS-Apple%20Silicon-000?style=flat-square)
[![License](https://img.shields.io/badge/License-MIT-blue?style=flat-square)](LICENSE)

**Craft your own AI office.** OffiCraft 是一間跑在你自己 Mac 上的 AI 工作室：你僱幾位常駐的
AI 成員，把事情整件交給他們，在一個網頁座艙裡看他們做到哪、在他們需要你點頭時回一句。
跑的就是你機器上那個 Claude Code。

![OffiCraft 座艙](docs/assets/cockpit-office.png)

### 亮點

- **成員是常駐的** — 有名字、職位、和跨對話不會忘的記憶；你關掉瀏覽器，他們還在跑。
- **任務不是指令** — 一件事拆成多個節點，每節寫明「怎樣算做完」，你隨時看得到卡在哪一節。
- **需要你決定時，只給你決定** — 問題被整理成一張帶選項的卡放進 Ask，你回一句就好。
- **你原本的 Claude Code 原封不動** — 你串好的 skill / plugin / MCP 全都在；多台 Mac 可以串成同一間公司。

<p align="center">
  <img src="docs/assets/cockpit-mobile-ask.png" alt="手機上的 Ask 頁" width="280" />
</p>
<p align="center"><sub>需要你決定的事會變成一張卡。手機上點一下就推進了。</sub></p>

**它到底能幫你做什麼、為什麼這樣設計** → [docs/why.md](docs/why.md)

### 安裝

需要 **Apple Silicon 的 Mac**、**`tmux`**、以及一個**已登入的 Claude Code CLI**
（`npm install -g @anthropic-ai/claude-code`）。

```bash
curl -fsSL https://github.com/pkyosx/OffiCraft/releases/latest/download/install.sh | bash
```

打開它印出來的那個連結（`http://127.0.0.1:7755/?code=…`），設好 owner 密碼——它會自己把
本機的 warden 裝好、把助理 **Mira** 叫醒。服務常駐在背景，關掉終端機不會停。

> [!NOTE]
> 座艙只綁 loopback（`127.0.0.1`）。想從手機或別的裝置連，要自己開一條 tunnel——
> 做法與「加到主畫面」的步驟見 [docs/mobile.md](docs/mobile.md)。

### 它長什麼樣

```
你 (owner)
 └─ 成員 ── 有名字、角色、模型，和不會忘的記憶
      └─ 任務 ── 拆成多個節點，每節有明確的完成定義
           └─ 需要你點頭 ──> 一張卡進 Ask ──> 你回一句 ──> 繼續
```

**任務怎麼運作（含一個完整例子）** → [docs/tasks.md](docs/tasks.md)　
**完整安裝、升級與移除** → [docs/install.md](docs/install.md)
**在手機上用** → [docs/mobile.md](docs/mobile.md)　
**agent 的環境變數** → [docs/agent-env.md](docs/agent-env.md)　
**開發** → [docs/dev.md](docs/dev.md)
