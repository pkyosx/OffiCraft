# agent 的環境變數(`~/.officraft/env`)

## 為什麼需要這個檔

agent 不是你手動開的 shell。它由 launchd 啟動 → `tmux new-session` → **非互動、非 login 的 `zsh -c`**。

zsh **只有互動 shell 才讀 `~/.zshrc`**。所以你放在 `.zshrc` 裡的 API key、token、PATH 補充,
對每一個 agent 來說**都是不存在的** —— 這不是 bug,是 zsh 的設計。

`~/.officraft/env` 就是補這個洞的地方:**只有 agent 啟動這條路徑會讀它**,
不影響你機器上任何其他 shell。

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
[ocwarden spawn] agent env: loaded 3 var(s) from /Users/you/.officraft/env: ANTHROPIC_API_KEY GITHUB_TOKEN PATH
[ocwarden spawn] agent env: WARNING /Users/you/.officraft/env mode is 0644, wider than 0600 — it holds credentials; run: chmod 600 ...
```
