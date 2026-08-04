# T-081b — 第三輪獨立審查(徽章對比 / 名稱驗證 / 內建標記 / 副本名)

- `git rev-parse HEAD`:`8545b8e117d9553bfa27a90f2c51819ea375084b`
- `git diff | shasum -a 256` 前 12 碼:`b56b92bc9266`
- 開始時間:2026-07-27 18:27 CST(結束 19:0x CST)
- 審查者:第三輪獨立審查(未參與實作)
- 工作區:`/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b`,全部未 commit

## 一句話總評

四項的**方向都對、都跑得動**(vitest 163 檔 1295 測全綠、`go test ./...` 綠、`check-token-roles.mjs` 綠、四支產生器零 drift),但**三項各自的「目的」都還能被繞過**:伺服器會收下前端拒收的 `﻿辦公室`(第 2 項的權威端破口)、自訂主題取名「辦公室(內建)」就能在快選單造出兩個一模一樣的選項(第 3 項的標記可偽造)、而第 1 項的 CI 守衛完全沒檢查徽章的**文字色**、也擋不住「在後面再寫一條規則覆蓋」。

## 已驗證通過的部分(先講好消息)

| 項目 | 證據 |
|---|---|
| 徽章換色範圍正確 | `grep -rn "color-danger" frontend/src` — 全樹只有 `chrome.css:270`、`office.css:274`、`office.css:379` 三處以 `--color-danger-badge` 當**不透明背景**;其餘 `--color-danger` 用法全是 `color:` / `border:` / `color-mix(...,transparent)`,沒有第四處「數字底」該換沒換,也沒有不該換的被換掉。三顆徽章都**沒有** hover/focus/border 變體(`grep -rn "nav-tab__badge\|office__tab-badge\|member-card__unread" frontend/src` 只有 3 條 CSS 規則 + 5 處 JSX)。 |
| `--color-danger` 未被動到 | `theme.css:68` 仍為 `#f0736b`。 |
| 守衛對「換寫法」的抗性 | 實測 4 種繞法全部 fail-closed:`rgb(240,115,107)` →「cannot be measured」;`#ba595333`(8 位 hex)→ 同上;`--color-danger-badge: var(--color-danger)` → 2.85:1 違規;把定義搬進 `src/styles/zz-override.css` → 仍被抓到 2.85:1。 |
| i18n / 產生器 | `npm run gen:msgkeys && gen:tokens && gen:fonts && gen:api` 後 `git diff \| shasum` 仍為 `b56b92bc9266` — **零 drift**。zh/en 葉節點路徑與型別完全一致(自寫 parity 測,`onlyZh: [] / onlyEn: []`)。`gen-message-keys.mjs` 雖然多讀了 `zh.ts`,但 `MESSAGE_KEYS` 仍只由 `en.ts` 的 `collect()` 產生,`zh.ts` **只**被 `identityNames` 用到 → **不會**把不該可覆寫的東西放進白名單。 |
| CI 接線 | `bin/ci.sh` 已加 `lint:token-roles`(步驟 4b0b)。 |
| 測試 | `npx vitest run` 163/163 檔、1295/1295 測通過;`go vet ./...` 無輸出、`go test ./...` `ok ocserverd 49.5s`。 |

---

# 分級清單

## BLOCKER-1 — 前後端名稱判決不一致:伺服器收下 `﻿辦公室`,前端拒收

**檔案**:`server/ocserverd/theme_bundle.go:104`(`strings.TrimSpace`)vs `frontend/src/lib/themeBundle.ts:425`(`s.trim()`)

`String.prototype.trim()` 依 ECMAScript WhiteSpace 定義**包含 U+FEFF**;Go 的 `strings.TrimSpace` 走 `unicode.IsSpace`,**不含 U+FEFF**。因此:

```
name = "﻿辦公室"   → TS: REJECT(reserved for a built-in theme)  Go: ACCEPT
name = "辦公室﻿"   → TS: REJECT                                  Go: ACCEPT
name = "﻿Office"   → TS: REJECT                                  Go: ACCEPT
name = "Office﻿"   → TS: REJECT                                  Go: ACCEPT
```

**重現**(兩支探針,跑完已刪除):

- Go:在 `server/ocserverd/` 放一支 `TestRound3Probe`,對每個名字呼叫 `validateThemeBundles([]ThemeBundleDTO{{Id:"midnight", Name:n, Colors:{"--color-bg":"#101018"}}})`;`go test -run TestRound3Probe -count=1 .` → `ok`,輸出 `bom_prefix_zh: ACCEPT`。
- TS:在 `frontend/src/lib/` 放一支 vitest 讀同一份 `/tmp/cases.json`,呼叫 `validateThemeBundle({id:"midnight", name, colors:{...}})` → `bom_prefix_zh: REJECT: theme: name "..." is reserved for a built-in theme`。

**為什麼是 BLOCKER**:U+FEFF 是零寬的,`"﻿辦公室"` 在挑選器裡**就是「辦公室」**。伺服器是權威端;任何不經本 UI 的寫入(conformance script、API client、匯入既有 settings)都會被收下並存進 `custom_themes`,第 2 項要防的「兩個辦公室、找不回內建的」原封不動地回來。反向副作用同樣存在:一旦這種主題存在於伺服器,前端的 `validateThemeBundles` 會拒收整包 `custom_themes`。

**另外一個反方向的不一致(同根源,見 SHOULD-6)**:`"OFFİCE"`(U+0130)Go 拒、TS 收。

---

## BLOCKER-2 — 第 3 項的「內建」標記可被完全偽造,快選單出現兩個一模一樣的「辦公室(內建)」

**檔案**:`frontend/src/components/ProfileDropdown.tsx:276-277`

```tsx
<option value="office">{msg.themeBuiltinOption(t.themeIdentity.office)}</option>
{customThemes.map((b) => <option key={b.id} value={b.id}>{b.name}</option>)}
```

內建那列是「名稱 + (內建)」的**純文字**;自訂那列是**裸名稱、沒有任何標記**。而 `"辦公室(內建)"` 這個名字**兩端驗證器都放行**(它不等於保留的 `辦公室`)。

**重現**(實際渲染,非推測):

- Go:`TestRound3Probe2` → `spoof_builtin_marker_zh: ACCEPT`、`spoof_builtin_marker_en: ACCEPT`。
- 前端渲染:在 `frontend/src/components/` 放一支 vitest,`api.patchServerSettings({customThemes:[{id:"spoofpack", name:"辦公室(內建)", colors:{"--color-bg":"#101018"}}]})` 後開啟 ProfileDropdown 偏好,印出 `<option>`:

```
OPTIONS [["office","辦公室(內建)"],["spoofpack","辦公室(內建)"]]
```

兩個選項的可見文字**完全相同**。這正是 owner 2026-07-27 回報的症狀,換一扇門原封不動地重現。

**加乘**:`settings.themeBuiltinTag` 本身在可覆寫白名單裡(`messageKeys.generated.ts:573`)。主題包可以把它蓋成「系統」——實測 `themeBuiltinOption` 變成 `"辦公室(系統)"`——同時把自己取名「辦公室(內建)」,於是**假的那個**看起來才像內建。

> 設定頁那份清單不受影響:`ThemeSettings.tsx:1153` 給每個自訂主題掛了 `ts-tag--custom` chip,是**結構性**標記、無法用名字偽造。破口只在 `<option>` 這條「標記只能是文字」的路徑上。可行的修法方向:自訂列也加後綴(`msg.themeCustomOption(b.name)`),讓「有沒有後綴」不再是判準;或改用 `<optgroup>` 分組。

---

## SHOULD-3 — `check-token-roles.mjs` 的徽章檢查有三個繞法,「改壞產品但 CI 全綠」

沙盒:把 `frontend/src` + `frontend/scripts` 複製到 scratchpad 後就地改,基準線為綠。

### (A) 徽章的**文字色**根本沒被檢查

`BADGE_SELECTORS` 迴圈只找 `d.prop === "background"`(`check-token-roles.mjs:479-497`),完全沒有驗證 `color:` 是不是 `--color-on-danger`。

```
把 chrome.css .nav-tab__badge 的 `color: var(--color-on-danger)` 改成 `color: #8a8a8a`
→ node scripts/check-token-roles.mjs
→ [token-roles] ok — ... unread badge 4.52:1 on text / 3.76:1 on page.   exit=0
```

守衛聲稱「unread badge 4.52:1 on text」,但實際畫面上的字已經是 `#8a8a8a`(對 `#ba5953` 約 1.9:1)。整個 AA 主張建立在一個**沒有被鎖住**的前提上。

### (B) 在後面再寫一條規則覆蓋 → 不會被抓到

`decls.find(...)` 取的是**第一條**符合的規則,CSS 串接卻是**最後一條**贏。

```
在 chrome.css 檔尾追加:  .nav-tab__badge { background: var(--color-danger); }
→ [token-roles] ok ... exit=0
```

這是三個繞法裡**最可能在正常重構中無意發生**的一個(例如某個 theme-variant 區塊、某個 `@media` 裡再寫一次)。

### (C) 把「好值」藏在不會生效的情境裡

`concreteValue()` 取 `defs.get(token).at(-1)` —— 最後一次宣告,不管它在哪個 at-rule 底下。

```
theme.css:114 改成 --color-danger-badge: #f0736b;   (即舊的 2.85:1 值)
檔尾追加:  @media print { :root { --color-danger-badge: #ba5953; } }
→ [token-roles] ok ... unread badge 4.52:1 on text / 3.76:1 on page.   exit=0
```

螢幕上實際生效的是 `#f0736b`(2.85:1),守衛卻報 4.52:1。注意這是**順序相依**而非正確:同一機制在「壞值在後」時是抓得到的(EXP G 的 `zz-override.css` 就被抓到了)——也就是說它現在會過只是運氣。

**建議**:`concreteValue` 應只採計 `THEME` 檔內、選擇器恰為 `:root`、且不在任何 at-rule 內的宣告(現行 parser 的 `stack` 已經帶得到這個資訊);`BADGE_SELECTORS` 迴圈應改成 `filter` 後檢查**每一條**規則,且同時檢查 `background` 與 `color` 兩個屬性。

---

## SHOULD-4 — 徽章對比是對「錯的底色」量的;在 active 分頁上實際只有 2.74:1,低於守衛自己的 3:1 門檻

`check-token-roles.mjs:499` 用 `checkRatio("--color-bg", MIN_PILL_VS_PAGE=3, "the pill stops reading as a pill on the page.")`,但那三顆徽章實際坐的底**都不是** `--color-bg`:

| 徽章 | 實際底色 | 對比 |
|---|---|---|
| `.nav-tab__badge` 在 **active** 分頁上 | `.nav-tab--active { background: var(--color-indigo) }` = `#2c3350`(`chrome.css:252-255`) | **2.74:1 —— 低於守衛自己的 3:1 門檻** |
| `.member-card__unread` 在選中的成員卡上 | `.member-card--selected { background: var(--color-card) }` = `#242832`(`office.css`) | 3.26:1 |
| 守衛量的 | `--color-bg` = `#191c24` | 3.76:1 |

`App.tsx:190-202` 顯示徽章**沒有**在 active 時被隱藏(只有 `chatUnread > 0` 這個條件),所以 2.74:1 是會真的出現的畫面。

雪上加霜:**本票自己新增的** `--color-nav-bg`(`chrome.css:201`、`theme.css`)讓分頁列有了獨立底色槽,預設 `var(--color-bg)`。主題只要填了 `--color-nav-bg`,守衛量的 `--color-bg` 就更加不是徽章的底了。

**建議**:第二個門檻應該對 `--color-indigo`(active 分頁)與 `--color-card`(選中卡片)各量一次,或把門檻對象改成一個明確列舉的「可能底色」集合。目前這一行給出的是**假保證**。

---

## SHOULD-5 — 第 4 項只修了一半:下載檔名的「(副本)」是**主題可覆寫**的字串,主題包仍能讓產品發出自己拒收的檔案

`compose.ts:177`:`themeCopyName: (name) => \`${name}${sp}(${set.themeCopyTag})\``,而 `settings.themeCopyTag` 在可覆寫白名單裡(`messageKeys.generated.ts:587`、`message_keys_gen.go:588`)。`validateWording` 對用詞值只擋控制字元(`themeBundle.ts:493`),**不擋 bidi**,長度上限是 **200**,而主題名稱上限是 **80**。

**重現**(vitest,`applyWording(zh, {"settings.themeCopyTag": tag})` → `makeMessages(...).themeCopyName(zh.themeIdentity.office)` → 丟回 `validateThemeBundle`):

```
CASE rlo   | tag="副本‮"    | wordingValidate=OK | reimport=theme: name must not contain control or bidirectional formatting characters
CASE long  | tag="x"×200        | wordingValidate=OK | reimport=theme: name must be 1..80 characters after trimming
CASE zwsp  | tag="副​本"    | wordingValidate=OK | reimport=OK
```

也就是說:**裝了某個主題包之後,按內建主題的「下載」鈕,拿到的還是一個產品自己拒收的檔案**——正是第 4 項存在的理由,只是換成從 `themeCopyTag` 這一側進來。

順帶暴露一個更廣的不對稱:**主題名稱擋 bidi,主題用詞不擋**。`hasBidiFormatChar` 只用在 `validateThemeBundle` 的 `name` 上;`validateWording` 的每個值都能塞 U+202E,而那些值會渲染在整個控制台裡。「存起來的字串和顯示出來的字串不一致」這個理由對用詞值一樣成立。

**建議**:(a) `validateWording` 一併套 `hasBidiFormatChar`(Go 側 `validateWording` 同步);(b) 匯出前對組出來的 `name` 跑一次 `validateThemeBundle`,不過就退回未覆寫的預設 tag。

---

## SHOULD-6 — 反方向的前後端不一致:`OFFİCE`(U+0130)Go 拒、TS 收

Go 的 `strings.ToLower` 是 simple case mapping,`ToLower(U+0130) == 'i'`,所以 `"OFFİCE"` → `"office"` → **REJECT**。JS 的 `toLowerCase()` 是 full case mapping,`"İ".toLowerCase() === "i̇"`(i + 組合點),所以 `"OFFİCE"` → `"offi̇ce"` ≠ `"office"` → **ACCEPT**。

實測(同 BLOCKER-1 的兩支探針):`dotted_capital_I` → `GO REJECT / TS ACCEPT`。

影響比 BLOCKER-1 輕(方向是「本機可建、送伺服器 400」),但它是同一個根源:兩邊的 normalize 各自用了語言內建的實作,沒有共同定義。**建議**把 normalize 明確寫成一個雙邊逐字對照的規則(例如:只 trim ASCII+Unicode 空白的明確清單、只做 ASCII 大小寫折疊),而不是各自呼叫 `trim/TrimSpace` + `toLowerCase/ToLower`。

---

## SHOULD-7 — 零寬字元兩端都放行,「辦<ZWSP>公室」照樣冒充內建

bidi 清單擋的是**會重排**的字元;不擋**零寬但不重排**的那一批。以下名字**兩端都 ACCEPT**,而且在挑選器裡看起來和「辦公室」/「Office」一模一樣:

| 輸入 | Go | TS |
|---|---|---|
| `辦​公室`(ZERO WIDTH SPACE) | ACCEPT | ACCEPT |
| `Off​ice` | ACCEPT | ACCEPT |
| `Off‍ice`(ZWJ) | ACCEPT | ACCEPT |
| `Office⁠`(WORD JOINER) | ACCEPT | ACCEPT |
| `Off­ice`(SOFT HYPHEN) | ACCEPT | ACCEPT |
| `Office᠎`(MONGOLIAN VOWEL SEPARATOR) | ACCEPT | ACCEPT |
| `Off؜ice`(ARABIC LETTER MARK — 這其實**是**一個 bidi 格式字元,只是不在清單裡) | ACCEPT | ACCEPT |

至少 `U+061C` 應該直接補進 `BIDI_FORMAT_CODEPOINTS` / `bidiFormatRunes`(它是 Unicode 正式的 bidi formatting character,清單漏了)。其餘零寬字元則建議在 `normalizeThemeName` 裡一併剝除後再比對——這比「擋掉」溫和,也不會誤傷合法名稱。

> 同形字(Cyrillic `О`、Greek `Ο`、全形 `ＯＦＦＩＣＥ`、數學粗體 `𝗢𝗳𝗳𝗶𝗰𝗲`)兩端也都放行。我認為那**超出本票範圍**、且擋起來誤傷極大,列在這裡只為完整,不建議處理。

---

## NIT-8 — 前端沒有 Go 那條「推導守衛」;新增第二個內建主題時前端會**靜默**失效

Go 有 `TestIsBuiltinThemeName`(`theme_bundle_test.go:80-95`),對每個 `reservedThemeIDs` 斷言 `themeIdentityNames[id]` 非空,並附註「否則名稱守衛會對它靜默通過」。**前端沒有對應的測試**:`grep -rn "THEME_IDENTITY_NAMES" frontend/src` 只有 `themeBundle.ts` 一處使用,`themeBundle.test.ts` 沒有任何斷言。`themeBundle.ts:438` 的 `(THEME_IDENTITY_NAMES[id] ?? [])` 這個 `?? []` 正是靜默失效的形狀。

具體失效情境:新內建主題的 **theme id 是 kebab-case**(如 `office-dark`),而 i18n 子樹的 key 是 camelCase(現有兩個是 `office` / `newTheme`)。`RESERVED_THEME_IDS` 放 `"office-dark"`、`themeIdentity` 放 `officeDark` → 交集為空 → 前端守衛對新主題完全不作用。Go 那條測試會 fail(所以不至於上線),但**破的是前端**、報的卻是後端,診斷成本高。

**建議**:在 `themeBundle.test.ts` 補一條與 Go 對稱的測試。

---

## NIT-9 — `--color-danger-badge` 的定義位置沒有像其他拆出 token 一樣被釘住

`SPLIT_FROM` 裡的 7 個 token 都有「必須定義在 `styles/theme.css`、否則 missing」的檢查(`check-token-roles.mjs:381-390`);`--color-danger-badge` 不在 `SPLIT_FROM` 裡,`badgeDef` 只是 `decls.find(d => d.rel === THEME && ...)`,而 `concreteValue()` 讀的是**全樹**的 `defs`。實測:把定義搬出 `theme.css` 而值不變,守衛不會有任何反應(`badgeDef` 為 `undefined` 但在 ratio 正常的路徑上從未被解參考)。

另外 `--color-danger-badge` 在可覆寫 token 白名單裡(`themeTokens.generated.ts:23`、`theme_colornames_gen.go:25`),所以 AA 下限**只綁內建主題**,主題包可以任意設回 2.85:1。這與其他 token 一致、我認為可接受,但守衛輸出的那句「unread badge 4.52:1」值得寫清楚它只描述內建主題。

---

## 清理

本輪所有暫存檔已刪除,`git status --porcelain` 與開審時完全相同,`git diff | shasum -a 256` 仍為 `b56b92bc9266…`。未修改任何產品碼。
