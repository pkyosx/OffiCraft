# 常見問題與排解

卡住時來這裡查。每一條先講**你會看到什麼**，再講**怎麼辦**。深入的機制在別的章節，這裡只給最短的解法與連結。

---

## 安裝階段

### 「只支援 macOS Apple Silicon」／安裝直接被拒

OffiCraft 只跑在 **macOS + Apple Silicon（darwin/arm64）**。安裝腳本第一道閘就擋其他平台，這是刻意的，不是 bug。Intel Mac、Linux、Windows 都不支援。

### 「7755 埠被占用」，安裝失敗

server 的標準埠是 **7755**。被別的程式占用時，安裝會**當場失敗並提示換埠**——它不會裝一個起不來的服務。兩個做法：

- 找出占用的程式關掉（`lsof -nP -iTCP:7755 -sTCP:LISTEN`），或
- 換一個埠：在 `oc.toml` 設 `[server].port`，或用 `OC_SERVE_PORT` 重跑安裝。

換了埠之後，控制台網址就跟著變成 `http://127.0.0.1:<你的埠>`。完整說明見 [安裝、升級與移除](install.md)。

### 設定連結（`?code=…`）失效或找不到了

那行連結是**一次性**的：**密碼一設好，`code` 立刻失效，重裝也不會再印。**

- **已經設過密碼**：不需要那個 code 了——直接開 <http://127.0.0.1:7755> 用密碼登入。
- **還沒設密碼、但 code 弄丟了**：重跑一次安裝以取得新的啟用憑證（release 路徑重跑安裝腳本；原始碼路徑 `bin/ocserver install`）。

### 控制台上方橫幅說「缺 claude / tmux」

設好密碼後，server 會自己去裝這台機器的 warden、叫醒助理 Mira。這一步需要兩個東西，缺了它會**明確失敗並在控制台上方橫幅說出原因**（fail-closed，而不是裝一個永遠起不了成員的 warden）：

- **`claude`（Claude Code CLI，且已登入）** — 裝法：`npm install -g @anthropic-ai/claude-code`，然後登入。
- **`tmux`** — 裝法：`brew install tmux`。

補上缺的那個，再回控制台重試即可。

---

## 成員起不來 / 一直離線

### 新請的成員亮不起來（Waking 卡住或一直 Offline）

先確認**那位成員被指派到的機器上**有已登入的 `claude` 與 `tmux`——warden 靠它們把成員 spawn 起來。缺任一個，成員就起不來。

**最常見的一個原因：Claude Code 太舊、沒有 Monitor tool。** 成員靠 `claude` 內建的 **Monitor** tool 持住 `ocagent listen` 那條到 server 的 SSE 長連線——**持著連線才算 online**。`claude` 太舊、沒有 Monitor tool（**2.1.98 起才內建**），成員就**掛不住 listen**、於是 Waking 卡住或一直 Offline。看那位成員被指派到的機器上 `claude --version`，太舊就升級：

```bash
npm install -g @anthropic-ai/claude-code
```

（控制台 **監控 › 機器** 也看得到每台機器上 warden 探到的 `claude` 版本。）

一個常見坑：用 **asdf / nvm / volta** 裝 `claude` 的人，launchd 的 PATH 很小、找不到 shim。解法是用絕對路徑重跑安裝（冪等）：

```bash
OC_CLAUDE_BIN=/絕對路徑/claude ./install.sh
```

### 成員被判離線，怎麼知道是誰的問題

OffiCraft 把「線上」定義得很乾淨：**成員此刻有沒有持著那條到 server 的長連線**——持著＝線上，斷了＝離線，沒有心跳、沒有 TTL。好處是診斷不「鬼打牆」：

- **成員自己沒連上** → 它那一手掛了，交給它機器上的 **warden 重拉**（或你在成員面板按 **Wake**）。
- **warden 沒連上** → warden 掛了，這跟個別成員在不在線是兩件事。

warden 與成員**各自自證在線、互不推斷**。原理見 [架構與運作原理](architecture.md)。

### 成員線上但沒動靜

先看它是不是卡在一張**等我回覆卡**上——去 **Ask（請示）** 分頁看有沒有在等你回。成員遇到「只有你能決定」的事會停下來開卡等你，回一句它就會繼續。任務也可能是**等待外部**（權限沒開、對方沒回、時間窗沒到），那不是它卡住，是這件事不在我們手上。任務狀態的意思見 [任務是怎麼運作的](tasks.md)。

真的要它換一手更清爽的記憶重開，線上時可在成員面板按 **Refocus（重新聚焦）**，見 [成員與外包](members.md)。

---

## 從手機或別台裝置連不上

**server 只綁 loopback（`127.0.0.1`）**——預設不對外，所以同一台機器以外的裝置**連不到是正常的**。要從手機連，得自己開一條通道：**同網段 + tunnel（如 cloudflared）**，或 **VPN / Tailscale 這類私有網路**。完整步驟（含加到主畫面）見 [在手機上用控制台](mobile.md)。

> ⚠️ 要對外開之前，先確定是走 tunnel 或 VPN，**別把埠直接曝在公網上**——控制台背後是你的整間工作室。

---

## 移除時遇到的怪現象

### `--purge` 的打字確認按不下去

走 `curl … | bash` 這個形式時，標準輸入正被用來把腳本餵給 bash，所以 `--purge` 的打字確認**送不進去、必定失敗**——結果是**什麼都不會被刪**（fail-closed）。解法：走管線時 `--purge` 一定要配 `--yes`；想要互動確認就先把腳本存成檔案再跑。

### 移除的最後一行印了紅字 `curl: (23|56) Failure writing output…`

**那不是失敗。** 用 `curl … | bash` 跑提早結束的動作（`--uninstall`、`--help`）時，腳本在 curl 還沒送完就結束了，curl 的下一次寫入因此失敗。**判斷成功與否要看腳本自己印的完成行**（例如 `DRYRUN complete — nothing on the machine was changed`），不是看最後一行有沒有紅字。

### 我怕移除會不會誤刪資料

先跑 `--dry-run`——它只印出會做什麼、什麼都不動。預設的移除是**停用 + 搬走，不是刪除**（搬到 `~/.officraft.bak-<timestamp>`，可還原）。`agents/`、`warden/`、`~/.officraft` 本身都不會被碰。

> ⚠️ 但**舊版（v0.5.12 及更早）**在某些情況會把整個 `~/.officraft` 搬走而訊息只說 "nothing was deleted"。如果你手上跑的是舊版安裝腳本，**先跑 `--dry-run` 看清楚**。完整說明與各種旗標見 [安裝、升級與移除](install.md)。

---

## 升級沒生效

**升級不必重跑安裝腳本。** 去 **設定 › 軟體更新** 按「檢查更新」→ 一鍵升級即可。若「檢查更新失敗」，多半是連不到 GitHub Releases（網路 / 代理）——稍後重試。想吃 prerelease 記得先打開「接收 Beta 版本」。細節見 [設定與參數](settings.md)。

---

## 相關文件

- 安裝、升級與移除的完整說明 → [安裝、升級與移除](install.md)
- 手機連線與加到主畫面 → [在手機上用控制台](mobile.md)
- 成員上線／下線／換手 → [成員與外包](members.md)
- 底下到底怎麼運作 → [架構與運作原理](architecture.md)
- 任務狀態的意思 → [任務是怎麼運作的](tasks.md)
