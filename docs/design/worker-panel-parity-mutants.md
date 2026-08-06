# T-7526 mutant 驗證紀錄

**為什麼這份要落檔**：獨立審查回來時，這包宣稱的 mutant 驗證在 repo 裡找不到任何紀錄，
審查者只好自己重跑一遍。**沒落檔等於不存在。** 下面每一列都可以被獨立重放。

## 方法

1. 把要驗的檔案複製一份到 scratchpad 當備份，`shasum -a 256` 記下雜湊。
2. 施加 mutant（單一、明確、可讀的一處改動 —— 描述欄寫的就是那一處）。
3. 跑該範圍的測試，記下**哪幾條紅**。
4. **從 scratchpad 備份 `cp` 回來**（🔴 **不准 `git checkout --`** —— 那會吃掉別人未提交的編輯），
   再 `shasum -a 256 -c` 驗還原後**逐位元組相同**。
5. Go 的每一支 mutant 前先 `go clean -testcache`，否則會讀到上一支的快取結果。

全部 29 支的還原檢查都是 `OK`（13 支第一輪 ＋ 9 支第二輪 ＋ 7 支第三輪）。

## 第一批：外包面板對齊（`WorkerDetailPanel.tsx`）

| # | Mutant（改了什麼） | 紅了哪條 |
|---|---|---|
| M1 | vm 裡把 `onSaveModelEffort` 加回去（就地模型編輯器復活） | `renders the 模型 and 機器 cells with NO in-place editor on either` |
| M2 | vm 裡把 `machineAction` 加回去（機器格就地控制項復活） | 同上 |
| M3 | 拿掉身分卡的「更改」入口（`{false && (…)}`） | 上述 + `更改 → changing the machine…` + `a rejected submit…` + `換 model: 更改 → save…`（共 5 條） |
| M4 | dialog 送出時不打 relocate（`if (false)`） | `更改 → changing the machine REACHES relocateWorker…` |
| M5 | 送出失敗時吞掉錯誤並關掉 dialog | `a rejected submit keeps the dialog open…` |
| M6 | relocate 排到 model op **之前**（順序反轉） | `saves the launch settings BEFORE relocating…` |
| M7 | 拿掉切換 worker 時的 dialog reset effect | `closes an open dialog when the panel switches to another worker` |

⚠️ **M6 / M7 第一輪是綠的** —— 代表當時**沒有任何斷言**釘住這兩個行為。補了測試才紅。
先寫 mutant、再看它殺不殺得死，是唯一能發現「斷言不存在」的辦法。

## 第二批：初始 PROMPT 卡（`AgentDetailPanel.tsx`）

先做**整體重現**：把**未修補的** `AgentDetailPanel.tsx` 還原回去跑新測試 —— **5 條全紅**
（正職 3 條、外包 2 條；MC-3 後外包補到 3 條）。再逐項：

| # | Mutant | 對應的修補要求 | 紅了哪條 |
|---|---|---|---|
| P1′ | deps 放回 `promptFetch` **並且**加回取消式 cleanup | ① 重繪不該取消進行中的讀取 | 正職＋外包的「重繪中仍顯示得出內容」兩條 |
| P2 | 已載入的章改回在 fetch **開始**時蓋 | ② 章要等讀到才蓋 | `re-expanding after a failed read…` |
| P3 | `.catch` 不落錯誤態（停在載入中） | ③ 失敗要顯示錯誤與重試 | 錯誤/重試相關 3 條 |

⚠️ **P1 的第一版只加了 cleanup、沒把 `promptFetch` 放回 deps —— 它是綠的。**
deps 沒變，cleanup 根本不會執行。**兩個成因是耦合的，mutant 必須兩個一起下。**

⚠️ 另一個坑（是這次真正差點放過去的）：`rerender` 若傳**同一個 element 物件**，React 會 bail out、
**根本不重繪**，於是「重繪中」那條測試**在未修補的碼上就是綠的**（恆真斷言）。
每次都要產生**新的** element。已寫進 `frontend/CLAUDE.md`。

## 第三批：取消喚醒（A）與復活死掉的 session（B）

**先做重現**（先紅才有資格談修好）：3 條重現測試在修補前全紅、3 條負向對照在修補前後都綠。

| # | Mutant | 紅了哪條 |
|---|---|---|
| A1 | deactivate 不派 STOP（`_ = cancellingWake`） | `TestHandleDeactivateMember_CancellingAWakeDispatchesAStop` |
| A1b | 每一次停止都立刻派（過度修正：`cancellingWake := true`） | `_OnlineMemberKeepsTheGracefulGrace` + `_OfflineMemberDispatchesNothing` |
| A2 | `report_waking` 又無條件清 `stopping_since` | `TestHandleReportWaking_KeepsTheStopTraceOfACancelledWake` |
| A2b | 永遠不清（過度修正） | `_ClearsTheStopTraceOnAnOrdinaryBoot` |
| B1 | restart 守衛退回 INTENT-only | `TestRestartWorker_RevivesAWorkerWhoseSessionDiedOnItsOwn` |
| B1b | restart 守衛整個拿掉（`if false`） | `TestRestartWorker_ClearsAndRedispatches`（活著的 worker 該 409） |
| B2 | 控制台 toggle 退回 `stopped`-only | 當時的 `a worker whose session died on its own offers 重新啟動…` |
| B2b | toggle 永遠顯示重啟（過度修正） | 當時的 `stop → the worker reads 已停止 and the toggle flips to 重啟` |
| B3 | mock adapter 漂回 INTENT-only 守衛 | 當時的 `a worker whose session died on its own offers 重新啟動…` |

⚠️ B2 / B2b / B3 點名的那兩條測試在**第二輪已改寫**（那顆 toggle 拆成 `worker-detail-stop`
與 `worker-detail-wake` 兩顆，字也從「重新啟動」統一成「喚醒」）。行為本身仍被
`喚醒 ASKS FIRST…` 與 `stop → the dot flips to 已停止…` 釘著；這幾列保留原文是**歷史紀錄**，
不要照字面去找那個測試名。

**B1 / B1b 是雙向的**：一支證明「死掉的要救得回來」，另一支證明「活著的仍要 409」——
守衛的兩半都承重，少任何一半都會被抓到。A1b / A2b / B2b 同理，是防過度修正的哨兵。

⚠️ **A1 的第一版是 `if false {`，Go 編譯不過**（`cancellingWake` 變成未使用）。
**編譯失敗不是「紅」** —— 它什麼都沒證明，得換成 `_ = cancellingWake` 重跑。

## 第四批:樣式所有權(截圖階段才發現的回歸)

**這一條不是測試抓到的,是看截圖看到的** —— dialog 整個沒有樣式。成因:`machine-picker.css`
的最後一個 production importer 隨著外包面板不再驅動 `useRelocateMachine` 而消失,**兩邊**的
設定 dialog 一起變裸,包含這次根本沒動到的正職面板。

| # | Mutant | 紅了哪條 |
|---|---|---|
| S1 | 拿掉正職面板自己的 `import "./machine-picker.css"`(＝原始回歸的形狀) | `component style ownership > every component using .machine-picker__* imports ./machine-picker.css`,而且斷言直接把 `MemberDetailPanel.tsx` 的檔名列出來 |

⚠️ **這類回歸對整套既有檢查是完全隱形的**:jsdom 不算 CSS、`tsc` 不知道 class 字串屬於哪份
stylesheet、而唯一會 render machine picker 的 CT guard 在同一張票裡退場了。護欄因此改成
**原始碼層級**的不變式,而不是靠瀏覽器量測。

## 第五批：owner 2026-07-31 四項裁定（第二輪）

方法同上（scratchpad `cp` 備份 ＋ `shasum -a 256 -c` 逐位元組驗還原，**不用 `git checkout --`**）。
9 支的還原檢查全部 `OK`。

### CSS（`member-detail.css`）— 只有 CT 看得見

| # | Mutant | 紅了哪條 |
|---|---|---|
| R1 | `.mp-identity__buttons { flex-direction: column }`（＝裁定前的形狀） | `identity-actions-row.ct.spec.tsx` **desktop ＋ narrow 兩條都紅** |
| R2 | 刪掉 ≤720px 的 `.mp-identity__buttons` 兩條規則 | **只有 narrow 紅**（`the pair spans the card, not the right margin`） |

⚠️ **R2 第一版是綠的，而且綠得很有道理** —— 拿掉窄螢幕規則後，`.mp-identity__actions` 的
`align-items: stretch` 仍然把那一列撐滿，所以「同一列／沒溢出／在卡片內」**每一條都還是真的**；
真正壞掉的是 `justify-content: flex-end` 讓兩顆鍵**擠在右邊界**——正是 owner 說的「擠成一團」。
斷言因此改成量**跨距**（span > 卡寬 70%）與**均分**（兩顆寬度差 < 卡寬 20%）。
教訓與 T-d451 那條同族：**「東西還在」不等於「東西擺對」**。

### 行為（`WorkerDetailPanel.tsx` / `MemberDetailPanel.tsx`）

| # | Mutant | 對應裁定 | 紅了哪條 |
|---|---|---|---|
| M8 | 喚醒鍵改成直接 `onWake?.()`（＝裁定前的「按了就送」） | ④ | `喚醒 ASKS FIRST…` ＋ 另外 3 條（共 4） |
| M9 | `openSettings` 的機器 seed 改成優先第一台**線上**機器 | ④ | `…PRE-SEEDED…` ＋ `…pinned to a SLEEPING machine never silently re-pins it` |
| M10 | 喚醒排到 model／relocate **之前** | ④ | `喚醒 stores the launch settings and the pin BEFORE it wakes…` |
| M11 | 無編輯的早退也套用到喚醒（照原值確認＝什麼都不做） | ④ | `喚醒 ASKS FIRST…` |
| M12 | 狀態格（含「已釋放」）加回去 | ② | `released…and NO 已釋放 status cell remains` |
| M13 | 離線原因跟著狀態格一起被刪 | ② | `離線: the dot reads 離線 and the structured reason survives…` |
| M14 | 喚醒的字改回 worker 私有葉子 | ③ | `stop → the dot flips to 已停止 and the row swaps 更改／停止 for 喚醒` |
| M15 | 外包的 `.mp-identity__buttons` 換掉（兩顆不再同一列） | ① | 同上那條 |
| M16 | 正職的 `.mp-identity__buttons` 換掉 | ① | `puts 更改 and the stop action in the same button row, 更改 first` |
| M16b | 正職那一列裡把「更改」拿掉（順序／存在性半邊） | ① | 上述 ＋ 既有的 `says 更改 for an online member…` |

🔴 **M9 第一版是綠的，而且是這一輪最危險的一支**：那兩條測試的 fixture **沒有任何線上機器**，
所以「優先第一台線上機器」這個 mutant **無處可去**，fallback 仍落回原本的釘選——
斷言在**有缺陷的碼上照樣通過**。補上 `__setMockMemberOnline("warden-mbp5", true)`（＝「有別的地方
可以被偷偷搬過去」）之後才紅。
**陽性對照必須讓被測的那個錯誤有機會發生**，否則它證明的是 fixture、不是碼。

🔴 **M16b 是 M16 的另一半**：M16 證明「兩顆在同一列」，M16b 證明「那一列裡真的有『更改』」。
少了後者，一個只渲染停止鍵的面板會讓 M16 的斷言變成恆真（`change` 根本取不到就不會比較）。

## 覆蓋面的證明（四條必須回 0 行的 grep）

「改了 A 要掃過所有引用 A 的 B」不能靠列清單自證 —— 清單漏一項就看不出來。以下四條在 repo 根目錄
執行，**每一條都必須輸出 0 行**。`docs/T-081b-evidence/` 與 `docs/T-081b-token-split-mapping.md`
是**凍結的歷史審查快照**（某個過去狀態的存證，刻意不隨碼改寫），本檔與 `worker-panel-parity.md`
本身在講的就是這些退場的東西，四者一律排除。

```sh
EXCL=":!docs/T-081b-evidence :!docs/T-081b-token-split-mapping.md \
      :!docs/design/worker-panel-parity.md :!docs/design/worker-panel-parity-mutants.md"

# G1 退場的識別字在 PRODUCTION 碼裡一個都不剩（解釋退場的註解不算，所以只比對「使用位置」
#    ——testid 字面、t.<key> 取值、prop 名——而不是裸字串出現）
git grep -nE '"worker-detail-status"|"worker-detail-stop-toggle"|\bworkerStatusText\(|t\.workerDetail\.(statusOf|status|starting|offline|working|stopped|restart|restarting)\b|\bonRestart\b' \
  -- 'frontend/src/**/*.ts' 'frontend/src/**/*.tsx' 'frontend/visual-guards' \
     ':!frontend/src/**/*.test.ts' ':!frontend/src/**/*.test.tsx'

# G2 兩份 locale 與 TWO 份 generated key 清單（TS + Go）同時清乾淨
#    —— 只清 zh/en 而漏掉 generated,主題包白名單會繼續宣稱那些代碼可覆寫
git grep -nE '^\s*(status|statusOf|starting|offline|working|stopped|restart|restarting):' \
  -- frontend/src/i18n/locales | sed -n '/workerDetail/p'
git grep -nE '"workerDetail\.(statusOf|status|starting|offline|working|stopped|restart|restarting)' \
  -- frontend/src/i18n/messageKeys.generated.ts server/ocserverd/message_keys_gen.go

# G3 樣式所有權（styleOwnership.test.ts 只掃 src/components,CT story 不在它的範圍內）
git grep -l 'mp-identity__buttons' -- 'frontend/src/components/*.tsx' 'frontend/visual-guards/stories/*.tsx' \
  | grep -v '\.test\.tsx$' | while read f; do grep -q 'member-detail.css' "$f" || echo "$f"; done

# G4 owner 面前的「重新啟動」這個 UI 字沒有殘留(server / session 的重啟是另一件事,不在此列)
git grep -nE '\*\*重新啟動\*\*|「重新啟動」|翻成\*\*重新啟動|restart: "重|restarting: "' \
  -- docs/guide frontend/src $EXCL
```

🔴 **寫這些 grep 時踩到的 shell 陷阱（會讓「回 0 行」變成假的）**：
`zsh` **不會**對未加引號的參數展開做斷詞。把排除清單放進一個字串變數再寫 `$EXCL`，
整串會被當成**一個** pathspec，**每一條排除都默默失效**。
排除清單必須用**陣列**（`EXCL=(':!a' ':!b')` ＋ `"${EXCL[@]}"`）。
本檔第一次寫時就是這個形狀，只是碰巧當時**不加排除也是 0 行**（＝比預期更嚴格），
結論沒受影響；但同一個寫法在別處會讓一條什麼都沒排除的 grep 看起來像「已排除、且乾淨」。

跑這四條時實際抓到、而列舉清單漏掉的三處：`frontend/CLAUDE.md` 拿 `workerDetail.statusOf`
當「查表葉子」的範例（那個 key 已經不存在）、`WorkerDetailPanel.test.tsx` 的 describe 分隔註解
仍寫「停止・重啟」、以及 `server/ocserverd/message_keys_gen.go`（**Go 那份 generated 清單**
——`npm run gen:msgkeys` 一次寫兩個檔，只看 TS 那份會漏）。

## 第六批：已結案要由身分那一層講（owner 2026-07-31 追加裁定）

判準是「同一個事實，不論從哪個入口，說同一句話、來自同一個來源」，所以 mutant 也分成兩族：
**「有沒有說」**與**「是不是同一個來源」**。後者才是這一批的重點 —— 前者用一份複製的字串
就能通過。

| # | Mutant | 打哪一半 | 紅了哪條 |
|---|---|---|---|
| N1 | 詳情入口退回默默掉到 roster（`false &&` 掉合成） | 有沒有說 | `the chat entry and the detail entry render the SAME released sentence` |
| N2 | 面板不再認得 released（`false && status === "released"`） | 有沒有說 | 上述 ＋ `released: the panel says 已結案 in the SAME words…`（2 條） |
| N3 | 🔴 **面板自己留一份幾乎一樣的字串**，不讀共用葉子 | **同一個來源** | 同上 2 條 |
| N4 | released view 整句拿掉（只剩灰身分） | 有沒有說 | 同上 2 條 |
| N5 | 合成的 view 改回 `status: "active"`（生命週期按鍵復活） | 誠實 | `the chat entry and the detail entry…` |
| N6 | 過度修正：`released \|\| noLiveSession`（凡是沒在跑的都當已結案） | 分不分得出來 | **11 條**，含專門的對照 `released vs merely OFFLINE are told apart…` |

🔴 **N3 是這一批唯一非做不可的一支。** N1／N2／N4 只證明「面板會說一句話」——
**把那句話寫死碼成第二份副本，這三支全部照樣綠**，而「兩份副本各自漂移」正是這次要修的病本身。
測試因此不是比對字面，而是**兩個入口互比 ＋ 兩邊都比字典**：第一條擋「其中一邊不顯示」，
第二條擋「兩份人工同步的副本」。少任何一條，N3 就殺不死。

🔴 **N6 是 N-族的過度修正哨兵**，也是「對照組不是裝飾」的證據：`presence` 對 released 與
對「從沒派工過」**都是 `undefined`**，光看那顆點永遠分不出來。沒有那條 offline 對照，
「面板會說已結案」會被一個**對每個灰 worker 都這樣說**的實作滿足。

### ⚠️ 一支**刻意不紅**的 mutant（誠實記錄，不是漏網）

| # | Mutant | 結果 |
|---|---|---|
| N7 | 把葉子的措辭改回聊天室專用的「以下為歷史對話（唯讀）」 | **綠** |

**為什麼綠是對的**：測試比對的是「兩個入口 vs 同一片葉子」，改葉子會同時改動兩邊，
所以它證明的是**單一來源**，不是**措辭**。**「這句話對兩個入口都為真」是 review-time 的性質，
沒有機械護欄** —— 寫死「不可包含『以下』」這種斷言跨語言會立刻變成噪音。
把它記在這裡，是為了讓下一個改這片葉子的人知道：**沒有測試會擋你，請自己確認新措辭在面板上
也不是假話。**

### 第六批的覆蓋面 grep（同樣必須回 0 行）

```sh
# ⚠️ 陣列,不是字串 —— 見上面的 zsh 陷阱
EXCL=(':!docs/T-081b-evidence' ':!docs/T-081b-token-split-mapping.md'
      ':!docs/design/worker-panel-parity.md' ':!docs/design/worker-panel-parity-mutants.md'
      ':!frontend/CLAUDE.md')   # 後三個檔的工作就是點名那個被退場的 key

# G5 聊天室專用的舊 key 名沒有任何存活的引用
git grep -nE 'releasedChatSub|releasedChatTitle' -- . "${EXCL[@]}"

# G6 那句話的「字串字面」只存在於兩份 locale,別處一律靠讀葉子
#    (刻意比對帶引號的位置 —— 註解裡引述那句話是合法的,複製成第二份字串不是)
git grep -nE '"[^"]*(已結案釋出|was released when its task closed)' \
  -- frontend/src ':!frontend/src/i18n/locales'

# G7 TS 與 Go 兩份 generated 清單都有新葉子、都沒有舊葉子
for k in releasedSub releasedTitle; do
  for f in frontend/src/i18n/messageKeys.generated.ts server/ocserverd/message_keys_gen.go; do
    grep -q "office.outsource.$k\"" $f || echo "MISSING $k in $f"
  done
done
grep -n 'releasedChat' frontend/src/i18n/messageKeys.generated.ts server/ocserverd/message_keys_gen.go
```

### ⚠️ 改 message key 名字的連帶後果（主題包）

`themeBundle.ts` 對**不認得的 key 是 DROP + 回報 `skipped`,不是拒絕匯入**
（owner 2026-07-27 裁定 rc-1599a0026a80,T-081b 退場 theme-identity keys 時立的規矩）。
所以把 `releasedChatSub` 改名成 `releasedSub` **不會讓既有主題包無法匯入**,
但**那個包對這句話的覆寫會默默失效**（匯入 UI 會在 `skipped` 裡說）。
第二輪退場的 `workerDetail.status` / `restart` / `statusOf.*` 等等同理。
`docs/T-081b-evidence/shots-pack/smurf-village.theme.json` 就是一份會受影響的範例包
（它覆寫了 `office.outsource.releasedChatTitle`/`ChatSub`）——**那是凍結的存證,刻意不改**。
