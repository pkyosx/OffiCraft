# T-081b — 第二輪獨立審查（lens A：正確性與相容性）

審查者：獨立審查（未參與任何實作）。全程唯讀產品碼；所有實驗都在
`…/scratchpad/` 的副本上跑，worktree 未被我改動。

## 版本快照

| 項目 | 值 |
| --- | --- |
| `git rev-parse HEAD`（base） | `8545b8e117d9553bfa27a90f2c51819ea375084b` |
| 開始時間 | 2026-07-27 17:01:54 CST |
| 結束時間 | 2026-07-27 17:18:11 CST |
| `git diff \| shasum -a 256`（前 12 碼，開始時） | `b52736477b30` |
| 主要分析所用的**釘住快照**（17:14:09） | `589b011435a4` |
| 結束時 | `597ce396a925` |

⚠️ **這棵樹在我審查期間被持續改寫**（實測 diff 雜湊 4 次變動：
`b52736477b30` → `c0ed9cc69103` → `589b011435a4` → `67c3eddbbc88` → `597ce396a925`）。
期間至少發生兩次**產品碼**語意變更：

1. `frontend/src/components/chrome.css` 的寬版外區從
   `max-width: max(1040px, calc(100% - 96px))`（我 17:03 量到寬版 gutter = 48px/側）
   **改回 `max-width: none`**（owner 同日改口：「寬版好像真的看不出什麼」）。
2. `frontend/src/i18n/index.tsx` 的 `sides` 從
   `repeat-y / left top / (無 attachment)` 改成
   `no-repeat / left bottom, right bottom / background-attachment: fixed`。

⇒ 本報告的每一條發現都已**重跑一次**對照 `589b011435a4` 之後的樹（最後一次核對
17:18），下方標「重跑於 17:1x」者為此。若之後再改，請重驗。

---

## 一句話總評

**不建議直接落地。2 個 BLOCKER、5 個 SHOULD。** 內建深色主題的像素不變是真的
（10 種寬度×版面組合實測全同）、前後端驗證對新欄位確實同構（16 組 JSON 雙邊逐一比對，
判決完全一致）、round-1 的 3 個 BLOCKER 與 6 個 SHOULD 都已修好。但這一輪找到兩件
實作者看不見的事：**（一）本票保證「內建主題像素不變」，卻沒有人檢查「既有主題包
是否不變」——實測既有淺色包在拆槽後對比度從 16.28:1 掉到 1.29:1（字直接看不見）；
（二）修好 round-1 BLOCKER-3 的方式，讓交付項目四（版面分區 token）在產品自己的
主題編輯器裡完全編不到。** 另外凍結 wire 契約再次與行為相反（round-1 BLOCKER-2 的同一類
問題，換了兩個欄位重演），以及防回歸 lint 的第 7～9 種繞法。

### 分級清單

| 級別 | # | 標題 |
| --- | --- | --- |
| **BLOCKER** | B1 | 既有**淺色**主題包在拆槽後靜默壞掉（實測 16.28:1 → 1.29:1） |
| **BLOCKER** | B2 | 版面分區 token 在產品自己的主題編輯器裡**完全編不到** |
| SHOULD | S1 | `spec/openapi.json` 兩處與實作**相反**（`sides` 的鋪法、canvas 屬於寬版還窄版） |
| SHOULD | S2 | lint 第 7/8 種繞法：`color-mix(…, transparent)` 但百分比 100% / `transparent 0%` |
| SHOULD | S3 | lint 第 9 種繞法：超過 8 跳的 alias 鏈 |
| SHOULD | S4 | `backgrounds` / `backgroundModes` 完全沒有 conformance 覆蓋 |
| SHOULD | S5 | `theme.css` / `global.css` 的註解仍描述已被改掉的 `sides` 行為 |
| NIT | N1–N5 | 見末節 |

---

# BLOCKER

## B1 — 既有**淺色**主題包在拆槽後靜默壞掉（相容性）

**這是 lens A 的核心發現。** 驗收文件 B 節只證了兩件事：內建深色主題像素不變、
一份**新手調**的淺色主題八項達標。**沒有任何人檢查「已經存在的主題包」。**

七個新槽的預設值全部**等於它們被拆出來的母槽**（`theme.css` 現況，重跑於 17:18）：

```
40:  --color-scrollbar-thumb: #2c3350;   ← --color-indigo: #2c3350
84:  --color-surface-sunken: #000;       ← --color-shadow: #000
85:  --color-backdrop: #000;             ← --color-shadow: #000
86:  --color-on-backdrop: #fff;          ← --color-overlay: #fff
102: --color-on-danger: #fff;            ← --color-overlay: #fff
108: --color-on-indigo: #fff;            ← --color-overlay: #fff
124: --color-knob: #fff;                 ← --color-overlay: #fff
```

因為值相同，**內建主題像素不變**——這就是實作者看得見的那一半。看不見的那一半是：
一個**已匯入**的淺色主題包，為了讓淺色控制台成立，必然覆寫過 `--color-overlay`（改深）
與 `--color-shadow`（改淺）——文件 A 節自己就點名「精靈村」的作者把 `--color-shadow`
設成淺色。拆槽之後，被搬走的那些呼叫點**不再讀母槽**，而是讀新槽，新槽在包裡不存在，
於是**靜默退回內建深色主題的預設值**。

### 重現（真瀏覽器量計算色，Chromium / playwright 1.61.x）

以一份「T-081b 之前寫的淺色包」餵進 base 與 head 的 CSS 樹：

```js
PACK = {"--color-bg":"#f5f6fa","--color-card":"#ffffff","--color-text":"#1a1d24",
        "--color-overlay":"#000000","--color-shadow":"#cccccc","--color-indigo":"#e0e0ff"}
```

量 `.mp-webhook__count`（`frontend/src/components/member-detail.css:682-683`）與
`.mp-webhook__submit`（同檔 `:1168-1170`）：

```
[base] count : color=rgb(0,0,0)       bg=rgb(224,224,255)  contrast=16.28:1
       submit: color=rgb(0,0,0)       bg=rgb(224,224,255)  contrast=16.28:1

[head] --color-surface-sunken=#000  --color-scrollbar-thumb=#2c3350  --color-knob=#fff
       count : color=rgb(255,255,255) bg=rgb(224,224,255)  contrast=1.29:1   ← 看不見
       submit: color=rgb(255,255,255) bg=rgb(224,224,255)  contrast=1.29:1   ← 看不見
```

base 的 `member-detail.css:683` 是 `color: var(--color-overlay)`（包有覆寫 → 黑）；
head 改成 `color: var(--color-on-indigo)`（包沒有這一槽 → `#fff`）。
**這正是本票宣稱要消滅的那個 bug，只是換成由「升級」造成。**

同一機制還會打到：`--color-surface-sunken` 12 個 `color-mix(... N%, transparent)`
下沉表面（包把 `--color-shadow` 設成 `#ccc`，升級後變 `#000`，淺色卡片上的深色淡染，
上面壓的是包設的深色文字）、`--color-knob`（iOS 開關滑塊）、`--color-scrollbar-thumb`。

### 為什麼實作者看不到

- 驗收判準（owner 改過的版本）只要求「證明新槽各自調得到達標」＋「證明拆分前無解」＋
  「防回歸 lint」，**沒有一條是「既有包升級後行為不變」**。
- `docs/T-081b-token-split-mapping.md` §7 有「向後相容」小節，但只涵蓋**版面分區 token**；
  §1–§3 的拆槽完全沒有相容性段落。
- 所有測試（`themeBundle.test.ts` / `themeExport.test.ts` / `avatar_bundle_test.go`）
  都只檢查**驗證器收不收**這份包，沒有任何一項檢查**收下之後長什麼樣**。
- repo 內沒有任何 `*.theme.json` 檢查樣本可跑（`find . -name "*.theme.json"` → 0 筆），
  所以「舊包升級」這條路徑從來沒被執行過。

### 建議

不必放棄拆槽。把七個新槽的**預設值改成 alias**——`--color-on-indigo: var(--color-indigo)`
等等——即可同時滿足三件事：

1. 內建主題像素**仍然**完全不變（因為現在的字面值本來就相等，實測如上）；
2. 既有包的覆寫**繼續生效**（母槽被覆寫 → alias 跟著走）；
3. 新包**仍然**可以各自指定（明寫該槽即壓過 alias）——拆槽的價值一分不減。

機制上與 §7 對分區 token 用的完全是同一招，`themeExport.ts` 的
`isUnsetAliasDefault` / `THEME_ALIAS_DEFAULT_TOKENS` 已經會處理。
**但注意這與 `check-token-roles.mjs:352-368` 的「aliased back to parent」規則直接衝突**
（那條規則現在會把它判成違規）——所以這是一個需要 owner 拍板的取捨，不是可以順手改的東西：
**要嘛接受既有淺色包會壞（那就必須寫進 doc 的 ACCEPTED-TRADEOFF，並給遷移說明），
要嘛改成 alias 預設並鬆綁 lint 的那一條。** 現在的狀態是兩邊都沒選，也沒有人記錄。

---

## B2 — 版面分區 token 在產品自己的主題編輯器裡完全編不到

Round-1 BLOCKER-3 的修法是「匯出時跳過還坐在 alias 上的分區槽」。修法本身正確
（我獨立重現：見下），但它有一個沒被看見的副作用：**新主題的播種來源就是匯出**，
而編輯器**只能編 `bundle.colors` 裡已經存在的槽**——沒有「新增色槽」的介面。

### 重現

（1）真瀏覽器讀真 `theme.css`，照 `themeExport.ts:48-63` 的邏輯算播種集合：

```
$ node rv-seed.mjs
total tokens: 71   seeded into a new theme: 68
skipped: [["--color-main-bg","alias-unset"],["--color-nav-bg","alias-unset"],["--color-topbar-bg","alias-unset"]]
computed zone values: {"tb":"#191c24","nav":"#191c24","main":"#191c24","bg":"#191c24"}
```

（2）編輯器的色槽清單來源，`frontend/src/components/ThemeSettings.tsx`：

```
119:  const [editColors, setEditColors] = useState<[string, string][]>([]);
261:    setEditColors(Object.entries(bundle.colors));   ← openEdit，唯一的填入來源
399:    setEditColors(next);                            ← setColorAt(i, value)，只改既有索引
```

`groupedColors`（`:487-499`）是對 `editColors` 分組，渲染（`:620-650`）也只走
`indices.map`。**全檔沒有任何新增 token 的路徑。**

⇒ 使用者在產品裡走「新增主題（以辦公室為底）→ 編輯」，看到 68 個色槽，
`--color-topbar-bg` / `--color-nav-bg` / `--color-main-bg` **一個都不在裡面**，
而且**加不進去**。交付項目四（版面分區）在產品內唯一的取得方式是：
匯出 JSON → 用文字編輯器手動加三個 key → 重新匯入。

`frontend/src/lib/themeTokenMeta.ts:116-127` 還特地為這三槽補了中英文人性化標籤
（round-1 SF-2 的修正），但那些標籤**永遠不會被渲染**——這正好說明沒有人真的走過這條 UI 路徑。

### 為什麼實作者看不到

`themeExport.test.ts` 的
「omits an alias-default token still sitting on its alias…」只斷言**匯出**的結果，
`ThemeSettings.test.tsx` 沒有任何一項斷言「編輯器列得出分區槽」。
兩個測試都綠，但沒有一個測到「作者能不能實際用到這個功能」。

### 建議（擇一）

- 編輯器改成走 `THEME_COLOR_TOKENS` 全集（bundle 沒有的槽以 computed 值當顯示預設、
  **只有被動過的才寫進 bundle**）——這才是「有值 vs 跟隨」的正解，也順手解掉未來每一個新槽；
- 或在編輯器補一個「加入分區色」的明確入口。

---

# SHOULD

## S1 — `spec/openapi.json` 兩處與實作相反（凍結 wire 契約再次說謊）

Round-1 BLOCKER-2 已修（`custom_themes` / `wording` 兩段描述已改寫，
`conformance/test_rest_happy.py::test_settings_custom_theme_unknown_wording_code_is_dropped_not_rejected`
已加）。**但同一類問題在本輪新增的兩個欄位上重演了兩次**，都是實作在我審查期間改動、
spec 沒跟著改：

**（a）`sides` 的鋪法**——`spec/openapi.json` 的 `ThemeBundleDTO.backgroundModes.description`
（重跑於 17:18，`grep -c "repeat-y" spec/openapi.json` → 1）寫：

> `sides` — … pin one copy … against the LEFT viewport edge and one against the RIGHT,
> **each repeating vertically (`repeat-y`)** at its natural size

實作（`frontend/src/i18n/index.tsx:262-278`）現在寫的是：

```ts
"--canvas-bg-repeat",     sides ? "no-repeat, no-repeat" : "repeat"
"--canvas-bg-position",   sides ? "left bottom, right bottom" : "0 0"
"--canvas-bg-attachment", sides ? "fixed, fixed" : "scroll"
```

`no-repeat` ≠ `repeat-y`，`bottom` ≠ `top`，且 spec 完全沒提 `background-attachment: fixed`。
`docs/T-081b-token-split-mapping.md` §9 已更新到新行為，**只有 spec 沒有**。

**（b）canvas 屬於哪一個版面**——`ThemeBundleDTO.backgrounds.description`
（`grep -c "side gutters of the wide layout"` → 1）寫：

> `canvas` — the OUTERMOST canvas, i.e. the area beside the centred content column
> (**the side gutters of the wide layout**)

owner 同日把寬版改回 `max-width: none`（`chrome.css:33-35`），寬版的 gutter 是 **0px**，
背景圖在寬版**完全看不見**。實測（真瀏覽器讀像素，1440×900）：

```
mode=sides wide=false  botL=255,0,0  botR=255,0,0   ← 窄版看得見
mode=sides wide=true   botL=25,28,36 botR=25,28,36  ← 寬版全黑底，圖不存在
```

⇒ spec 把「唯一看得見它的版面」寫成了「唯一看不見它的版面」。

repo `CLAUDE.md` §13「動 wire = 先改 spec + owner 過目再動碼」。這兩處都是 spec 落後於碼。

## S2 — `check-token-roles.mjs` 第 7、8 種繞法：`color-mix` 百分比

Round-1 的六種繞法我逐一驗證**都已堵住**（見末節表）。但 `veilOnly`
（`frontend/scripts/check-token-roles.mjs:261-274`）的修法只檢查
「同伴是不是 `transparent`」，**沒有檢查混合比例**——而 `100% / transparent`
在 CSS 裡就是**完全不透明**。

```
$ node scripts/check-token-roles.mjs                     # 乾淨基準
[token-roles] ok — 3 split tokens keep to one role each; 7 carved-out …
$ # 附加到 src/components/office.css 後重跑
>>> BYPASS (lint PASSED): .zz1 { background: color-mix(in srgb, var(--color-overlay) 100%, transparent); }
>>> BYPASS (lint PASSED): .zz2 { color:      color-mix(in srgb, var(--color-overlay), transparent 0%); }
    caught:               .zz3 { background: var(--color-overlay); }              ← 對照組
```

（重跑於 17:12，對 `589b011435a4` 快照的 `scripts/` + `src/` 副本。）

真瀏覽器確認這兩式**真的**是完全不透明，不是我推測：

```
$ node cm.mjs        # Chromium
a  color(srgb 1 1 1)          ← color-mix(in srgb, var(--color-overlay) 100%, transparent)
b  color(srgb 1 1 1)          ← color-mix(in srgb, var(--color-overlay), transparent 0%)
c  color(srgb 1 1 1 / 0.12)   ← 正常的 12% 疊層（對照組）
```

`^transparent(\s+[\d.]+%)?$` 這條 regex（`:266`）**主動放行了 `transparent 0%`**，
而 `transparent` 不帶百分比配上 `var(--color-overlay) 100%` 也一樣不透明。
兩式都是很自然會被寫出來的形態（「我要一個不透明的疊層色」），不是刻意刁難。

**修法**：`veilOnly` 要同時看比例——`--color-overlay` 那一側的百分比必須 < 100%，
且 `transparent` 那一側的百分比必須 > 0%（或兩側都不帶百分比）。

## S3 — 第 9 種繞法：超過 8 跳的 alias 鏈

`expandVars`（`:189`）有 `if (depth > 8) return value;` 的遞迴上限，超過就**原樣返回未展開的
`var(...)`**，母槽的名字因此不會浮現：

```
>>> BYPASS (lint PASSED): .zzA { --a1: var(--color-shadow); --a2: var(--a1); … --a10: var(--a9); background: var(--a10); }
    caught:               .zzB { --b1: var(--color-shadow); --b2: var(--b1); background: var(--b2); }   ← 對照組
```

比 S2 刻意得多（要 10 跳），所以是 SHOULD 不是 BLOCKER。但 fail-open 的深度上限本身該改成
fail-closed（超限就當違規報出來，讓人去拆鏈），現在是「夠深就免疫」。

## S4 — `backgrounds` / `backgroundModes` 完全沒有 conformance 覆蓋

`conformance/test_rest_happy.py` 只為 wording 的丟棄語意加了一項。
`grep -n "backgrounds\|backgroundModes" conformance/` → **0 筆**。
這是本票唯一一個**新增的 wire 欄位**，而 wire-freeze gate 只驗
`ocapi_gen.go` 能不能從 spec 重生成，不驗行為。⇒ 沿著 S1 的方向，
「spec 說 A、碼做 B」這件事在這兩個欄位上 CI 抓不到。

至少補：`backgrounds.canvas` 合法圖 → 200 且 echo 帶得回來；`backgrounds.topbar` → 422；
`backgroundModes` 無圖 → 422；`backgroundModes` 值不合法 → 422。

## S5 — `theme.css` / `global.css` 的註解仍描述已被改掉的 `sides`

實作已改成 `no-repeat` + `bottom` + `fixed`，但兩處註解沒跟上（重跑於 17:18）：

- `frontend/src/styles/theme.css:16-19`：「預設值就是「tile」的**三個**值」（現在是**四個**，
  多了 `--canvas-bg-attachment`）、「`sides`時由 JS 改寫成兩層、各貼一邊、**只縱向重複**」
  （現在是 `no-repeat`）。
- `frontend/src/styles/global.css:21-24`：「one copy pinned against each viewport edge,
  **repeating vertically only**」。

這 repo 的註解密度很高、而且被當作規格在讀，留著反向敘述比沒有註解更危險。

**另外兩點值得記在同一條下（疑慮，未證實）**：
`background-attachment: fixed` 在 iOS Safari 上是眾所皆知的破功（會退化成 `scroll`
或把圖縮到視窗大小）；我沒有 iOS 環境可驗，只在 Chromium 驗過（正確）。
另外 `sides` 在寬版永遠看不見這件事，`docs` §9 有寫，但**產品 UI 沒有任何提示**——
編輯器只有 `themeCanvasBgModeHint` 一行文案，使用者選了 `sides`、又是寬版使用者的話，
會看到「設了但沒反應」。

---

# NIT

- **N1** TS 與 Go 的**錯誤訊息順序**不同（判決一致，只是訊息不同）。
  輸入 `{"wording":{"zh":{"nav.tasks":"a\nb"},"xian":{"nav.tasks":"x"}}}`：
  TS → `wording[zh][nav.tasks] must not contain control characters`；
  Go → `wording language "xian" is not allowed`。原因是 Go 先把**所有**語言的
  語言集合＋條數上限掃完才驗值（`wording_bundle.go:60-71`），TS 是逐語言一次驗完
  （`themeBundle.ts:413-447`）。兩邊都拒收，只是使用者離線看到的理由和上線後不同。
- **N2** 編輯器儲存會**丟掉** `backgroundModes: {canvas: "tile"}`
  （`ThemeSettings.tsx:472-474` 只寫非預設模式）。語意等價，但「匯入 → 開編輯 → 存檔」
  的 JSON 不是同一份，`serializeBundle` 的往返因此不是位元等價。
- **N3** 一份 wording 全數被丟棄的包，儲存與 echo 會留下 `"wording":{"zh":{}}`
  （Go 與 TS 雙邊實測皆如此）。匯入路徑沒有像 `handleSaveEdit:431` 那樣剪掉空語言 map。
- **N4** `validateThemeBundles`（`themeBundle.ts:540`）呼叫時**沒有**傳 `skipped`，
  所以「編輯器存檔」與「mock PATCH」這兩條路上的丟棄依然沒有任何訊號——
  只有匯入那一條有警告橫幅。owner 的裁定是使用者面靜默可接受，但這兩條路上連
  `skippedWording` 這個現成通道都沒接。
- **N5** `check-token-roles.mjs` 只走 `frontend/src/**/*.css`；`index.html` 的 `<style>`、
  inline `style={{}}`、CSS-in-JS 一律不管（round-1 已列，此處僅確認仍然如此）。

---

# Round-1 項目重新驗證

| Round-1 | 狀態 | 我的驗證 |
| --- | --- | --- |
| BLOCKER-1 lint 六種繞法 | ✅ 六種**全部**堵住 | 逐一重跑（A 選擇器/B 非首宣告：`declarations()` 改成 brace-aware 且 `selector` 成為第一級欄位；C 不透明 color-mix：`veilOnly` 加了 transparent 檢查；D theme.css 豁免：`if (d.rel === THEME) continue` 已移除；E `var()` fallback：`expandVars` 會展開 fallback；F 大寫 `VAR(`：全部 regex 加 `i`；G 多跳 alias：遞迴展開）。**但新找到第 7/8/9 種**（S2、S3） |
| BLOCKER-2 spec 未更新 | ✅ 已修（wording 那一組） | `spec/openapi.json` 兩段描述已改寫、conformance 已加。**但同類問題在 backgrounds/backgroundModes 上重演**（S1） |
| BLOCKER-3 匯出烘死分區槽 | ✅ 已修 | 真瀏覽器獨立重現：71 槽 → 播種 68，三個分區槽以 `alias-unset` 被跳過。**但引出 B2** |
| SF-1 編輯器 `trim()` 毀掉空白片段 | ✅ 已修 | `ThemeSettings.tsx:429` 現在是 `kept[code] = val`（只有空值判斷才 trim） |
| SF-2 10 個 token 沒有人性化標籤 | ✅ 已修 | 重算：`tokens 71 with meta 71 MISSING 0` |
| SF-3 lint 對合法 box-shadow 誤報 | ✅ 已修 | brace-aware 解析後 `prop` 不再黏到選擇器；乾淨基準 `rc=0` |
| SF-4 舊 DB 列不重驗 | ✅ 已修 | `settings.go:344-353` 讀取時跑 `dropUnknownWordingCodes`（只裁剪不拒收） |
| SF-5 丟棄無 server log | ✅ 已修 | `wording_bundle.go:83-127` 每次丟棄寫一行，最多列 10 個 |
| SF-6 `tasks.elapsed` 未拆 | ⚠️ 未查 | 屬 lens B（文案）範圍，本輪未重驗 |

---

# 我驗過、確認乾淨的部分

## 內建深色主題像素不變 —— 10 種組合全同

真瀏覽器（Chromium，playwright 1.61.x）比對 base（`8545b8e`）與 head 的**完整 CSS 樹**，
5 種視窗寬 × 窄/寬版，全頁截圖 SHA-256：

```
vw=1440 wide=n  shot IDENTICAL      vw=1440 wide=Y  shot IDENTICAL
vw=1280 wide=n  shot IDENTICAL      vw=1280 wide=Y  shot IDENTICAL
vw=1024 wide=n  shot IDENTICAL      vw=1024 wide=Y  shot IDENTICAL
vw= 720 wide=n  shot IDENTICAL      vw= 720 wide=Y  shot IDENTICAL
vw= 390 wide=n  shot IDENTICAL      vw= 390 wide=Y  shot IDENTICAL
```

唯一的計算色差異就是三個分區元素從 `rgba(0,0,0,0)` 變成 `rgb(25,28,36)`
（＝身後 body 的同一色），幾何完全不變。寬版改回 `max-width: none` 之後，
連 round-1 量到的 48px 外區差異也消失了。

## 只填 `--color-bg` 的既有包 —— 四層自動跟著走

```
PACK = {"--color-bg":"#3aa06a", …}
{ body: 'rgb(58,160,106)', topbar: 'rgb(58,160,106)', nav: 'rgb(58,160,106)', main: 'rgb(58,160,106)' }
```

分區 token 的 `var(--color-bg)` 預設確實成立（這一半的相容性是真的；壞掉的是 B1 那一半）。

## 前後端驗證同構 —— 16 組 JSON 雙邊逐一比對，判決全部一致

同一批 JSON 分別餵給 Go（`validateThemeBundles`，scratch 副本加了一支 probe test）
與 TS（`validateThemeBundle`，scratch vitest）：

| # | 輸入要點 | Go | TS |
| --- | --- | --- | --- |
| 00 | `backgrounds.canvas` 合法 PNG | OK | OK |
| 01 | ＋`backgroundModes.canvas="tile"` | OK | OK |
| 02 | ＋`"sides"` | OK | OK |
| 03 | **只有 mode 沒有圖** | ERR `has no image in backgrounds[canvas]` | ERR 同 |
| 04 | 兩者皆 `{}` | OK | OK |
| 05 | 兩者皆 `null` | OK | OK |
| 06 | `backgrounds.canvas=""` | ERR | ERR |
| 07 | `backgroundModes.canvas=""` | ERR `not a valid mode` | ERR 同 |
| 08 | `backgrounds.topbar` | ERR `only canvas` | ERR 同 |
| 09 | `backgroundModes.topbar` | ERR `only canvas` | ERR 同 |
| 10 | key 大小寫 `CANVAS` | ERR | ERR |
| 11 | 壞語言＋壞值同時出現 | ERR（語言） | ERR（值）→ **N1** |
| 12–14 | 不認得的 code，值分別是空白／控制字元／BOM | OK（丟棄） | OK（丟棄） |
| 15 | backgrounds＋sides＋不認得的 code | OK | OK |

⇒ **「有沒有 `backgroundModes`」這件事上，一邊收一邊拒的輸入我找不到。**
「`backgrounds` 存在但沒有 `backgroundModes`」（案 00）雙邊都收，且
`i18n/index.tsx:256` 的 `=== "sides"` 讓它落回 tile——舊包鋪法零變化。

## `sides` / `tile` 的真實 CSS 行為

真瀏覽器讀像素（1440×900，含 5000px 高的長頁與捲到 y=3000）：

```
mode=sides wide=n tall=n  scrollY=0     botL=255,0,0 botL_x45=25,28,36 topL=25,28,36 botR=255,0,0 botR_x45=25,28,36
mode=sides wide=n tall=Y  scrollY=0     同上
mode=sides wide=n tall=Y  scrollY=3000  同上   ← fixed 生效，長頁不會再冒出第二棵樹
mode=sides wide=Y tall=n  scrollY=0     全部 25,28,36   ← 寬版看不見（S1(b)）
mode=tile  wide=n tall=Y  scrollY=3000  全部 255,0,0（內容欄內 25,28,36）
```

左右各一份、貼齊視窗左右下角、不橫向重複、不隨捲動移動、內容欄從未被滲入、
所有情況 `scrollWidth === clientWidth`（沒有多出橫向捲動）。
`dir="rtl"` 下 `left`/`right` 是物理方位，量到的結果與 ltr 完全相同（正確）。

## `--canvas-bg-*` 的清理

`i18n/index.tsx:215-216` 的 `removeProperty` 迴圈跑在 effect 最前面，且四個 canvas 變數
都有 push 進 `appliedTokensRef`（`:279-284`），所以換主題／換回內建都會清乾淨。
`index.test.tsx` 的「and all four are cleared again when the next theme carries no image」
確實釘住這件事。切主題不會殘留。

## 其他

- `npx vitest run` → **164 files / 1291 tests passed**（17:07 那一版樹）。
- `check-token-roles.mjs` 乾淨基準 `rc=0`。
- 兩個生成器的 drift 我沒有重跑（round-1 已驗，且本輪產品碼有變動中，重跑意義不大）。

---

# 對抗式測試檢視：「我要怎麼改壞產品碼而它還是綠的」

| 測試 | 可以這樣改壞它而測試仍綠 |
| --- | --- |
| `themeExport.test.ts` 「omits an alias-default token…」 | 測試自己用 `freshRoot()` 手動把三個分區槽設成 `#191c24`，**從來沒有讀過真的 `theme.css`**（jsdom 不載 CSS）。把 `theme.css` 的分區預設從 `var(--color-bg)` 改成 `var(--color-card)`，生成器會照樣產出 alias map、測試照樣綠，但內建外觀已經變了。 |
| 同上 | 測試只斷言「匯出跳過」，沒有任何測試斷言「編輯器列得出這三槽」⇒ **B2 就是從這個縫掉出去的**。 |
| `themeBundle.test.ts` `validateBackgroundModes` | 只驗 `undefined/tile/sides/未知 zone/未知 mode/無圖`。把 `BACKGROUND_MODE_SET` 換成 `new Set(["tile","sides","cover"])` 之外的任何**放寬**都會被抓；但把 Go 那邊的 `backgroundModeAllowed` 加一個值、TS 不加，**沒有任何測試會紅**（兩份清單沒有任何交叉檢查，只有註解說「the twin of」）。 |
| `i18n/index.test.tsx` canvas 那一項 | 斷言的是 JS 寫出去的**字串常數**（`"no-repeat, no-repeat"` 等），不是 CSS 效果。把 `global.css` 的 `background-repeat: var(--canvas-bg-repeat, repeat)` 整行刪掉，測試仍全綠、圖照樣鋪滿。**沒有任何自動化測試連到真實 CSS。** |
| `check-token-roles.mjs` 本身 | 沒有自我測試（沒有 `check-token-roles.test.*`）。把 `veilOnly` 改成 `return true` ⇒ lint 永遠綠、CI 全綠、沒有任何測試會紅。 |
| conformance | `backgrounds`/`backgroundModes` 零覆蓋（S4）：把 Go 的 `validateBackgrounds` 改成 `return nil`，`go test ./...` 會紅（有 unit test），但**行為面的權威（conformance）不會**。 |

---

# ACCEPTED-TRADEOFF（沿用，未重議）

- 不認得的用詞代碼對主題作者靜默（owner `rc-1599a0026a80`）——匯入路徑已有橫幅，
  編輯器存檔路徑仍靜默（N4）。
- `sides` 不做鏡像（owner 2026-07-27）。
- 寬版吃滿、最外層畫布成為窄版限定的一層（owner 2026-07-27 改口）——**但 spec 沒跟上（S1b）**。
