# T-e4ae recon：手機寬表格取捨

日期：2026-08-16（Asia/Taipei）
基準：`origin/main` = `9c27dec0dcd9f3ae93b7c233800cd9e1c5b31660`
工作樹：`/Users/seth_wang/.officraft/agents/ow-da17024abcfd/work/te4ae-recon`

## 量測範圍與方法

這次沒有沿用前一代未驗證的 `517→344px` 或 `+173@390` 數字。重新建立乾淨 worktree，新增只讀 recon fixture 與 CT spec；fixture 的六欄欄位與三筆資料形狀取自 repo 現有的 `docs/design/worker-panel-parity.md`，保留 CJK、inline code 與長說明文字，且刻意不放 `<pre>`／blockquote，讓結果只回答表格本身。這是 issue-shaped stress fixture，不宣稱是目前線上任務 payload 的原樣拷貝：repo 的 mock tasks／chat／taskManuals 都以空集合起始，盤點也沒有找到可直接重用的六欄任務資料。

測試使用真實 Chromium、真實 `TaskCard` 與完整祖先鏈，在 390／320 viewport 測量：卡片可用寬度、表格 client／scroll 寬度、欄名行數、內容最大行數，並把 `scrollLeft` 設為 50 讀回確認是否為表格自己的 scroller。每個變體另存前後截圖。

執行：

```text
npx playwright test -c playwright-ct.config.ts visual-guards/te4ae-recon.ct.spec.tsx
```

## 六欄實測結果

卡片內寬在 390／320 分別是 344／274px；markdown description 寬度是 316／246px。

| 變體 | 390px | 320px | 表格自己的捲動 | 任務列表橫滑 |
|---|---|---|---|---|
| A0 現況 | 欄名 3 行；內容最多 11 行；table 316→316px；不捲 | 欄名 5 行；內容最多 20 行；table 246→246px；不捲 | 否（scrollLeft=0） | 0px |
| B1 只有 `th { white-space: nowrap }` | 欄名全 1 行；table 316→381px；+65px | 欄名全 1 行；table 246→381px；+135px | 是（scrollLeft=50） | 0px |
| B2 `th, td { white-space: nowrap }` | 欄名全 1 行；table 316→1431px；+1115px | 欄名全 1 行；table 246→1431px；+1185px | 是（scrollLeft=50） | 0px |
| B3 固定 `min-width:520px`（保留 `max-width:100%`） | table 實際寬 520px；description +204px；list +189px | table 實際寬 520px；description +274px；list +259px | 否（table 自己 0px） | **違反 AC** |
| H1 `th,td { min-width:96px }`，內容可折行 | table 316→577px；+261px；內容最多 8 行 | table 246→577px；+331px；內容最多 8 行 | 是（scrollLeft=50） | 0px |
| H2 `th,td { min-width:120px }`，內容可折行 | table 316→721px；+405px；內容最多 7 行 | table 246→721px；+475px；內容最多 7 行 | 是（scrollLeft=50） | 0px |

### 內容感知的逐欄混合方案

H1/H2 的問題正如 owner 指出：所有 cell 一律吃下限，會把短的第一欄也撐成 96／120px。再補測 renderer-level 的內容感知策略：先量每欄 cell 的不換行 intrinsic 寬度，短欄保留自己的寬度，只有超過 floor 的欄套 96／120px；`th` 仍 nowrap。

| 變體 | 390px | 320px | 欄寬（#／項目／正職／外包／差／期望） | 任務列表橫滑 |
|---|---|---|---|---|
| C1 內容感知 floor 96px（建議） | table 316→498px；+182px；內容最多 8 行 | table 246→498px；+252px；內容最多 8 行 | 39／96／96／96／74／96px | 0px |
| C2 內容感知 floor 120px | table 316→594px；+278px；內容最多 7 行 | table 246→594px；+348px；內容最多 7 行 | 39／120／120／120／74／120px | 0px |

C1/C2 兩個寬度的欄名都全 1 行，且設 `scrollLeft=50` 都能讀回 50，確認是 table 自己的 scroller。這組是用真實 DOM cell intrinsic 寬度模擬 renderer-level 決策；不是把第一欄用 `:first-child` 硬編碼。

B3 也測過前一代草稿的 `min-width: min(520px, max-content)`：在 390／320 仍是 316／246px、scrollLeft=0，等同無效。原因是 `max-content` 在這個已被 `max-width:100%` 約束的 block 上只解析成目前寬度；固定 min-width 則把超寬推到外層，沒有變成 table 自己的捲軸。

## 觀察、推論、結論

### 觀察

- A0 確實重現問題：欄名在窄寬被拆成 3／5 行，且表格沒有任何內部橫捲。
- B1 是最小改動就能讓欄名保持一行、並讓表格自身可捲的變體；外層 `.tasks` 仍是 0px 橫滑。
- B2 也保住外層 0px 橫滑，但把內容寬度推到約 1.4kpx，手機需要長距離拖曳。
- B3 在現有 markup／CSS 下不是可直接採用的解法：固定 min-width 會讓 `.tasks` 外層橫滑，而不是讓 table 自己捲。
- H1/H2 把最小寬度放在 cell，而不是 table：欄名都維持 1 行、長內容仍然換行，且 overflow 留在 table 自己；這正好補上 B1「只保住欄名、沒有保住內容欄寬」的缺口。
- H1 把內容最長行數從 B1 的 11 行降到 8 行；H2 再降到 7 行，但需要比 H1 多拖 144px（390）／144px（320）。兩者都沒有把外層任務列表推出去。
- H1/H2 會讓第一欄這類短內容欄位也變成 96／120px，視覺上浪費空間；C1/C2 保留短欄的 39／74px intrinsic 寬度，仍把長欄從一字一行拉開。
- 嘗試只靠 CSS 的 `min-width: min(96/120px, max-content)` 與 `min-width: fit-content(96/120px)`（H3–H6）在 Chromium 中均回到 A0：欄名 3／5 行、table 不捲。要按內容判斷，應在 markdown renderer／表格呈現層量測或產生逐欄寬度，而不是期待目前共用 CSS 自己讀懂 cell 文字。

### 資料分佈限制

只讀資料盤點分身確認：`frontend/src/api/mock.ts` 的 tasks、chatLog、replyCards、taskManuals 都以空集合起始；與任務／聊天／文件最接近的既有 parser／story fixture 主要是 2–4 欄，沒有可直接拿來當六欄任務 payload 的樣本。因此六欄 fixture 的欄位形狀引用現有設計表，內容長度保留真實 repo 文件常見的 CJK、inline code 與說明文字；它足以測量這個六欄 stress case，但不是「生產資料分佈已被完全代表」的證明。後續若 owner 核可實作，護欄 fixture 還要再以可取得的實際資料長度補一輪對照。

### 推論

- 若只要求「欄名不再一字一行」，B1 是較小、較可逆的取捨；但同一份 fixture 實測內容仍最多 11 行，不能回答「欄名短、內容長」的可讀性問題。
- C1/C2 是內容感知混合取捨：短欄不浪費寬度，長欄有最小可讀寬度，內容仍可折行。C1 拖曳成本較低；C2 的內容更舒展，但仍不是單行。
- H1/H2 是簡單但較粗的 fallback；若不做 renderer-level 判斷才考慮它們。
- 若要求每個 cell 也維持單行可讀，B2 能做到，但 1.1kpx 以上的水平距離會把閱讀成本轉成大量拖曳。
- 若 owner 要選 min-width，實作前還需要額外的可捲 wrapper／markup 設計；不能只把 `min-width` 貼在目前這個 table 上。

### 結論（待 owner 裁定）

目前證據支持的可行方向是：

1. **採 C1（我的建議）**：內容感知逐欄 floor 96px；短欄保留 39／74px，長欄 96px，內容最多 8 行，table 內部捲動 +182／+252px，任務列表仍不可橫滑。
2. **採 C2**：內容感知逐欄 floor 120px；短欄保留 39／74px，長欄 120px，內容最多 7 行，table 內部捲動 +278／+348px，任務列表仍不可橫滑。
3. **採 B2**：欄名與 cell 都 nowrap；內容保持單行，但需接受約 +1115／+1185px 的 table 內部捲動。
4. **維持現況**：不改行為；保留 3／5 行欄名與不捲的現況。

## 證據檔

- CT spec：`frontend/visual-guards/te4ae-recon.ct.spec.tsx`
- 六欄 fixture：`frontend/visual-guards/stories/TaskCardSixColTableStory.tsx`
- 截圖：本目錄下 `6col-390-*` 與 `6col-320-*`；另有 `5col-*` 作為已存在五欄 fixture 的對照。

目前這一步只做量測與候選方案比較，沒有修改 production CSS，也沒有宣稱已修復。
