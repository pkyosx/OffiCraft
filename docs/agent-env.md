# agent 的環境變數

agent 的環境是**兩層疊起來的**:

| 層 | 來源 | 誰維護 | 什麼時候需要動它 |
|---|---|---|---|
| **1. 基底** | 你的**互動 shell**(`~/.zshrc` 等)—— spawn 時自動撈 | 不用維護,自動 | 平常都不用動 |
| **2. 覆寫** | `~/.officraft/env` | 你手寫 | 只在要**單獨蓋掉某個變數**、或給 agent 一個互動 shell 沒有的值時 |

**同名時第 2 層贏。** 檔案不存在(或空的)⇒ 第 2 層完全沒作用,agent 就拿你互動 shell 的環境。

## 為什麼需要第 1 層(自動繼承)

agent 不是你手動開的 shell。它由 launchd 啟動 → `tmux new-session` → **非互動、非 login 的 `zsh -c`**。

三層原因疊在一起:
1. warden 的 launchd plist 只給一組寫死的最小 `EnvironmentVariables`;
2. **launchd 從不 source `~/.zshrc`**;
3. 啟動鏈路走的是**非互動** shell,而 zsh **只有互動 shell 才讀 `~/.zshrc`**。

2026-07-20 實測:互動 shell 有、agent 沒有的變數共 **20 個**(其中 11 個是憑證),
外加 `PATH` 少三條(`~/.local/bin`、`~/.asdf/shims`、`/opt/homebrew/sbin`)。這不是 bug,是 zsh 的設計。

所以 warden 在**每次 spawn 時**都直接問一個互動 shell:

```
/bin/zsh -i -c '/usr/bin/env -0'
```

拿到的變數當作 agent 的環境基底。實測成本約 **0.12 秒**,每次 spawn 付一次。

> **為什麼是 `env -0` 而不是 `export -p`?** zsh 的 `export -p` 會把 `PATH` / `FPATH` 這種
> **tied array** 印成 `export -T PATH path=( ... )` 陣列語法。用 `KEY=value` 解析器讀它會
> **靜靜地整個漏掉 `PATH`** —— 而 `PATH` 正是這件事最重要的那個變數。`env -0` 是 NUL 分隔的
> `KEY=VALUE`,沒有引號方言、沒有陣列語法,值裡面可以有 `=`、換行、引號、空白,每個 byte 都是字面值。

### 有幾個變數**不會**被繼承

這些不是憑證,是**描述「撈取當下那個 shell」的簿記變數**,搬到 agent 身上會是錯的:

| 變數 | 為什麼不繼承 |
|---|---|
| `PWD` / `OLDPWD` | 啟動行先 `cd <workdir>` 再套環境。繼承會讓 `$PWD` **從此說謊**(shell 實際在 workdir,但 `$PWD` 指向 warden 的目錄) |
| `TMUX` / `TMUX_PANE` | 從 tmux pane 裡跑 ocwarden 時會有。繼承會讓 agent 的 tmux 指令**打到你自己的 session** |
| `TERM` / `COLUMNS` / `LINES` | 描述撈取當下的終端。agent 的終端是它自己的 tmux pane,覆蓋掉會讓 claude TUI 畫面亂掉 |
| `SHLVL` / `_` | shell 簿記,搬過去沒有意義 |
| `OC_*` | warden 自己的身分命名空間(`OC_TOKEN` / `OC_BASE` / `OC_SESSION` …)。`.zshrc` 裡不小心 export 到不能改到 agent 的身分 |

**其他全給** —— 包含全部憑證,正職與外包一致。

### 關掉自動繼承

萬一哪天繼承本身出問題,不用改碼就能退回舊行為:

```bash
launchctl setenv OC_AGENT_ENV_INHERIT 0   # 然後重啟 warden
```

設成 `0` / `false` / `no` / `off` 都算關。關掉之後 agent 就只剩最小環境 + `~/.officraft/env`。

## 為什麼還需要第 2 層(`~/.officraft/env`)

`~/.officraft/env` 是**覆寫層**:**只有 agent 啟動這條路徑會讀它**,
不影響你機器上任何其他 shell。用它來:

- 給 agent 一個**跟你自己不一樣**的值(例如指向 staging 的 token);
- 給 agent 一個**互動 shell 根本沒有**的變數;
- 你不填 ⇒ 完全沒作用。

## 怎麼用

```bash
touch ~/.officraft/env
chmod 600 ~/.officraft/env     # 它會裝憑證,權限請收緊
```

檔案不存在**完全沒關係** —— agent 照常啟動,只是沒有額外變數。這是正常狀態,不是錯誤。

改完**不需要重啟 warden**:下一次 spawn 出來的 agent 就會拿到新值
(已經在跑的 agent 不受影響,要重開才會吃到)。

## 格式

一行一個 `KEY=value`。就這樣。

```bash
# 井號開頭是註解
ANTHROPIC_API_KEY=sk-xxxxxxxx
export GITHUB_TOKEN=ghp_xxxxxxxx        # 容忍 export 前綴,寫不寫都行
MY_MESSAGE="有 空 格 要 用 引號"          # 單雙引號等價,會被剝掉一層
```

> ⚠️ **`PATH` 不要照抄 `.zshrc` 的寫法。** 它在這個檔裡是「整條取代」,
> 而且 `$PATH` 不會展開。詳見下面第 2 節 —— 寫錯會讓 agent 找不到任何系統指令。

## ⚠️ 四個會踩到而且踩了看不出來的地方

### 1. 值是「字面」的 —— **不展開 `$VAR`**

這個檔**不是 shell**。`$HOME`、`$PATH`、`` `cmd` ``、`$(cmd)` 都**不會**被展開或執行,
會原封不動變成字串的一部分。

```bash
❌ TOOLS=$HOME/bin          # 變數的值真的就是字串 "$HOME/bin"
✅ TOOLS=/Users/you/bin     # 寫絕對路徑
```

**這是刻意的設計**:能展開就等於能執行,這個檔就會變成第二個 `.zshrc`。
它只承載資料,不承載邏輯。

### 2. 🔴 `PATH` 是**整條取代**,不是附加

這是最容易踩、後果也最大的一個。`PATH=` 在這個檔裡的意思是
「agent 的搜尋路徑**就只有**這些」,**不是**「在原本的後面再加這些」。

```bash
❌ PATH=/opt/homebrew/sbin:$PATH    # $PATH 不展開,值是字串 "...:$PATH" —— 兩個錯疊在一起
❌ PATH=/opt/homebrew/sbin          # agent 的 PATH 就只剩這一條!
                                    # /bin /usr/bin 全部不見 → cat、ls、git 全都找不到
✅ PATH=/opt/homebrew/bin:/opt/homebrew/sbin:/Users/you/.local/bin:/usr/bin:/bin:/usr/sbin:/sbin
   # 要哪些就完整寫哪些,包含系統那幾條
```

**最安全的做法是:能不設 `PATH` 就不要設。**
如果你只是要讓某個工具能被找到,考慮改用絕對路徑的變數(例如 `MY_TOOL=/opt/homebrew/bin/foo`)。

你一旦在這個檔裡寫了 `PATH=`,**log 一定會出現一行警告**提醒你這件事(見最後一節)。

> 補充:agent 自己的 workdir 會被接在你設定的 PATH **前面**,
> 而 warden 取 agent 憑證用的是絕對路徑,**不受你的 PATH 影響** ——
> 所以就算這裡寫壞了,agent 仍然起得來、仍然能認證,只是它自己要執行的指令會找不到。

### 3. 行尾註解:**只有加了引號才有效**

`#` 是密碼裡的合法字元,所以沒引號時無從判斷它是註解還是值的一部分。

```bash
✅ KEY="abc" # 這是註解      → 值是 abc(引號劃清了邊界,註解被拿掉)
⚠️ KEY=abc # 這是註解        → 值是「abc # 這是註解」整串!(會警告)
✅ KEY=abc#def              → 值是 abc#def(# 前面沒空白,就是普通字元)
```

沒加引號又寫了行尾註解時,**log 會警告你**(見下面「怎麼確認有沒有生效」),
值不會被偷偷截斷 —— 因為截斷一個真的含 `#` 的密碼,比留著更難查。

### 4. `OC_` 開頭的變數會被拒絕

`OC_TOKEN`、`OC_BASE`、`OC_SESSION` 等是 warden 用來標定 agent 身分的,
在這裡設會被直接忽略並記錄原因(否則這個檔就能冒充其他成員或把 agent 指向別的 server)。

## 其他規則

| 規則 | 行為 |
| --- | --- |
| key 的合法形狀 | `^[A-Za-z_][A-Za-z0-9_]*$`,不合法就跳過並記錄 |
| 同一個 key 寫兩次 | 後面的贏,會記錄 |
| 某一行寫錯 | **只有那一行被跳過**,其他變數照常生效 |
| 值含 NUL | 拒絕該行(NUL 進不了環境變數,硬送會變成截斷的假值) |
| 設了 `PATH=` | 照設定值生效(整條取代),但一定會警告 |
| 引號沒包住整個值(如 `K="a" x`) | 保留字面(含引號)並警告 |
| 權限比 600 寬 | 仍然載入,但會警告(它裝憑證) |
| 檔案不存在 / 讀不到 / 太大 | 當成「沒有額外變數」,agent 照常啟動 |

## 怎麼確認有沒有生效

所有診斷都寫進 warden 的 log:

```bash
grep "agent env" ~/.officraft/warden/log/ocwarden.err.log
```

會看到像這樣的行 —— **只有變數名稱和原因,永遠不會印出值**:

```
[ocwarden spawn] interactive env: inherited 24 var(s): PATH JIRA_TOKEN GEMINI_API_KEY HOMEBREW_PREFIX ...
[ocwarden spawn] agent env: /Users/you/.officraft/env overrides the interactive shell for: JIRA_TOKEN
[ocwarden spawn] agent env: 25 var(s) for the agent (24 inherited from the interactive shell, 2 from /Users/you/.officraft/env): PATH JIRA_TOKEN ...
[ocwarden spawn] agent env: WARNING /Users/you/.officraft/env mode is 0644, wider than 0600 — it holds credentials; run: chmod 600 ...
```

撈不到互動環境時(shell 壞掉、逾時、輸出畸形),**spawn 不會失敗** —— 退回最小環境繼續起,
並留下一行說明。warden 負責起機器上**每一個** agent,這條路徑絕不能有辦法把整個工作室弄掛:

```
[ocwarden spawn] interactive env: capture failed (timed out after 10s); spawning with the minimal environment — the agent will be missing whatever ~/.zshrc exports
```
