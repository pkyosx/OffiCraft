# T-081b 拆法定案：三個身兼二職的色槽 + 帶參數文案 + 主題名稱

盤點基準：worktree `feat/T-081b-theme-token-split`，base `origin/main` @ `8545b8e`。
所有計數以 `grep -rn --include='*.css'`（`frontend/src`）當下實測為準。

## 0. 為什麼要拆（一句話）

一個 token 只能有一個值。這三個 token 各自被拿去做**兩件對顏色要求相反**的事，
所以任何主題都只能二選一。內建深色主題剛好兩邊都成立，因此問題到淺色主題才爆。

「精靈村」主題包的填值就是活證據——三個色槽作者都被迫犧牲一邊：

| 色槽 | 精靈村填的值 | 被犧牲的那一邊 |
| --- | --- | --- |
| `--color-overlay` | `#000000` | 7 處實色用法變黑（燈箱關閉鈕在近黑遮罩上 1.45:1） |
| `--color-shadow` | `#c0bda8` | 全站 `box-shadow` 幾乎消失 |
| `--color-indigo` | `#cfe0f2` | 同色的動作鈕／active 底一起變淡 |

T-4527 執行者已確認 overlay 那一項是**明知的取捨**：設白則 175 處邊框與 hover
實質消失，設黑則 7 處實色用法壞掉，他們選了壞比較少的那邊。**拆開之前這無解。**

---

## 1. `--color-overlay`（212 處）

**判準（可機械套用）**：出現在 `color-mix(..., transparent)` 裡 = 半透明疊層 → 留原 token；
直接 `var(--color-overlay)` = 實色 → 依它壓在什麼底上分流。

| 歸屬 | 處數 | 說明 |
| --- | --- | --- |
| 保留 `--color-overlay` | 202 | 半透明疊層基色（背景 87、各式邊框 113、兩處文字疊層）。這是它唯一該有的語意：「把底下的東西提亮或壓暗 N%」。 |
| → `--color-on-danger` | 3 | 壓在 `--color-danger` 紅底上的未讀數字 |
| → `--color-on-indigo` | 2 | 壓在 `--color-indigo` 實色底上的徽章數字與送出鈕文字 |
| → `--color-knob` | 2 | iOS 風格開關的滑塊 |
| → `--color-on-backdrop` | 1 | 圖片燈箱關閉鈕的 × |
| 定義行 | 1 | `styles/theme.css:53` |

逐處（非疊層那 8 處）：

| 檔案:行 | 選擇器 | 新歸屬 |
| --- | --- | --- |
| `components/chrome.css:257` | `.nav-tab__badge` | `--color-on-danger` |
| `components/office.css:275` | `.office__tab-badge` | `--color-on-danger` |
| `components/office.css:380` | `.member-card__unread` | `--color-on-danger` |
| `components/member-detail.css:683` | `.mp-webhook__count` | `--color-on-indigo` |
| `components/member-detail.css:1169` | `.mp-webhook__submit` | `--color-on-indigo` |
| `components/member-detail.css:1059` | `.mp-toggle__knob` | `--color-knob` |
| `components/settings.css:1320` | `.set-toggle__knob` | `--color-knob` |
| `components/office.css:1004` | `.chat__lightbox-close` (`color`) | `--color-on-backdrop` |

（上表 8 處 + 下面這 1 處 = 9 處離開 `--color-overlay`，故保留 202 處。）

➕ **連帶一處**：`components/office.css:1005`（同一個關閉鈕的圓底
`color-mix(overlay 10%, transparent)`）一併改掛 `--color-on-backdrop`。理由：這顆鈕
整個坐在遮罩上，它的字和它的圓底必須跟遮罩一起被主題作者決定；留在 `--color-overlay`
的話，字跟自己的底會被兩個獨立的 token 拉扯。預設 `#fff` → 像素不變。

`--color-on-danger` / `--color-on-indigo` 的命名沿用 repo 既有的 `--color-on-accent`、
`--color-on-warn`（「壓在某個實色底上的前景」）。

## 2. `--color-shadow`（24 處）

| 歸屬 | 處數 | 說明 |
| --- | --- | --- |
| 保留 `--color-shadow` | 11 | 真的是 `box-shadow` 的投影色 |
| → `--color-surface-sunken` | 10 | 拿來當**背景**的深色淡染：下沉一層的表面 |
| → `--color-backdrop` | 2 | 75% / 80% 的**遮罩**（圖片燈箱、md 預覽） |
| 定義行 | 1 | `styles/theme.css:54` |

逐處（12 處背景用法）：

| 檔案:行 | % | 用途 | 新歸屬 |
| --- | --- | --- | --- |
| `components/member-detail.css:316` | 4 | 最後操作 log | `--color-surface-sunken` |
| `components/member-detail.css:962` | 14 | webhook 請求詳情 | `--color-surface-sunken` |
| `components/member-detail.css:974` | 25 | payload `pre` | `--color-surface-sunken` |
| `components/member-detail.css:997` | 20 | webhook URL 框 | `--color-surface-sunken` |
| `components/member-detail.css:1116` | 20 | webhook 輸入框 | `--color-surface-sunken` |
| `components/office.css:1240` | 18 | 訊息內檔案 chip | `--color-surface-sunken` |
| `components/office.css:1282` | 55 | 分享鈕 | `--color-surface-sunken` |
| `components/onboarding.css:66` | 4 | 引導橫幅細節區 | `--color-surface-sunken` |
| `components/tasks.css:658` | 18 | 產物檔名 chip | `--color-surface-sunken` |
| `components/tasks.css:736` | 30 | 產物動作鈕 | `--color-surface-sunken` |
| `components/office.css:983` | 80 | **圖片燈箱遮罩** | `--color-backdrop` |
| `components/office.css:1355` | 75 | **md 預覽遮罩** | `--color-backdrop` |

**為何遮罩不直接用既有的 `--color-scrim`**：語意上它們確實是 scrim，但 `--color-scrim`
是 `#0a0c12`（實色），這兩處是 `#000` 的 75/80% 疊層——改掛過去會**改變內建主題的像素**，
違反「拆分不得改變現況外觀」。因此另立 `--color-backdrop`（預設 `#000`，像素不變），
並在 `theme.css` 註明它是 `--color-scrim` 的半透明對應物。若日後要合併成一個，是獨立的
視覺決定，不該混在這次的結構性拆分裡。

## 3. `--color-indigo`（20 處）

| 歸屬 | 處數 | 說明 |
| --- | --- | --- |
| 保留 `--color-indigo` | 16 | 實色動作／active／徽章底（13 背景 + 3 邊框），全在 `components/` 下 |
| → `--color-scrollbar-thumb` | 2 | `styles/global.css:29`（Firefox `scrollbar-color`）、`:40`（WebKit thumb 背景） |
| 定義行 / 註解提及 | 1 / 1 | `styles/theme.css:13` / `:16`（後者是註解，非使用點） |

保留的 16 處是同一個語意（「一塊你會在上面放白字的飽和底」），彼此不衝突，**不需再拆**。
真正打架的只有捲軸拇指：它的對比對象是**頁面底色**，而動作鈕的對比對象是**它上面的字**。
兩者對「深或淺」的要求相反，所以只要把捲軸抽出來，衝突就解除。

`::-webkit-scrollbar-thumb:hover`（`global.css:44`）用的是 `--color-text-muted`，不動。

## 3.5 向後相容：七個新槽的預設值是 alias，不是字面值（review round-2 修正）

**這一節是被獨立審查抓出來後補的，原本的做法會弄壞已經匯入的淺色主題包。**

七個新槽最初的預設值是**字面值**（`--color-knob: #fff` 等），恰好等於母槽在內建深色主題
下的值，所以「內建主題像素不變」成立——那是實作者看得見的那一半。看不見的那一半：一份
**已匯入**的淺色包必然覆寫過 `--color-overlay`（改深）與 `--color-shadow`（改淺），因為
那是它被寫出來時唯一存在的名字。拆槽後，搬走的呼叫點改讀新槽，新槽在包裡不存在，於是
**靜默退回內建深色的預設**。實測（webhook 徽章／送出鈕）：**16.28:1 → 1.29:1，字直接看不見**
——本票要消滅的 bug，改由「升級」重新製造一次。

**修法**：預設值改成 alias（`--color-knob: var(--color-overlay)` …），同時滿足三件事：
內建主題像素**仍然**不變（字面值本來就相等）、既有包的覆寫**繼續生效**、新包**仍可**各自
指定（明寫該槽即壓過 alias）。與 §7 分區 token 用的是同一招。

**護欄要跟著改**：`check-token-roles.mjs` 原本把「alias 回母槽」判成違規。那條規則的前提
（alias＝把兩個工作又合併回去）**是錯的**：alias 只是預設，主題明寫就分得開。真正該紅的是
**呼叫點改回母槽**與**新槽沒人用**，那兩條都還在，而且逐一實測過會紅（見驗收報告 C 節）。
`expandVars` 同步改成在新槽處停止展開——否則每一個**正確**的呼叫點都會被誤判成「你用了母槽」。

**證據**：`docs/T-081b-evidence/` 的相容性實測——同一份 pre-T-081b 淺色包餵進
`origin/main` 與本分支，七個受影響的計算色**逐一相同**；不帶任何主題包時內建深色主題也逐一相同。

## 4. 新 token 一覽與預設值（內建主題像素完全不變）

```css
--color-on-danger:      var(--color-overlay);
--color-on-indigo:      var(--color-overlay);
--color-knob:           var(--color-overlay);
--color-on-backdrop:    var(--color-overlay);
--color-surface-sunken: var(--color-shadow);
--color-backdrop:       var(--color-shadow);
--color-scrollbar-thumb: var(--color-indigo);
```

預設值是 **alias 而非字面值**——理由與實測見 §3.5。內建深色主題下這些 alias 解析出的
色值與拆分前逐一相同（`#fff` / `#000` / `#2c3350`），所以像素仍然不變；差別在於一份
**只覆寫母槽**的既有主題包，覆寫會**繼續傳遞**到新槽,而不是靜默退回內建值。

7 個新 token，全部由 `gen-theme-tokens.mjs` 從 `theme.css` 自動掃進前後端兩份白名單
（本節這 7 槽讓白名單 61 → 68；再加上 §7 的 3 個分區槽，最終為 **71 槽**）。
**既有使用者主題包不會壞**：沒有任何 token 被改名或移除，舊 bundle 的 key 集合仍然合法，
而新槽會**跟隨它被拆出來的那一槽**（§3.5）——這一點原本是錯的，由 review round-2 抓出並修正。

## 5. 帶參數文案（票面說 19 條，實測 34 條）

全部 48 個 interpolation 函式葉子中，**34 條**含有主題包會想換掉的世界觀用語，
需要改成「可覆寫靜態片段 + 參數」；其餘 14 條是純機械文字（日期、時長、計數、
檔案大小、單純的人名代入），不需要拆。

票面的 19 是「任務／等我回覆」核心那一圈；漏掉的 15 條是機器、成員、設定那一族
（`monitor.machine.*` 5、`mp.*` 3、`settings.*` 3、`chat.*` 3、`machine.picker.*` 1），
它們同樣帶「成員／機器／強制停止／重新聚焦」等用語，其中兩條甚至有一個講同一句話、
但**已經可覆寫**的靜態孿生 key——不一起處理會留下「同一句話有時換得掉、有時換不掉」。

34 條清單：
`tasks.progress` `.planningBy` `.blockedBy` `.blockedByMissing` `.copyTaskNo`
`.terminateConfirmBody` `.markDuplicateBody` `.duplicateOf` `.reassignTitle`；
`replies.waited` `.openedAt` `.answeredAt` `.expiredAt` `.expireConfirmBody`；
`office.outsource.label`；`workerDetail.statusLabel` `.refocusSinceLabel`；
`chat.offlineTitle` `.offlineQueueHint` `.wakeQueueHint` `.composerOffline` `.interAgentExpand`；
`mp.forceStopConfirmBody` `.effortLevel` `.refocusSinceLabel`；`machine.picker.offlineOption`；
`monitor.machine.bootstrapErrorDetail` `.bootstrapFailed` `.uninstallConfirmBody`
`.uninstallWarnBody` `.deleteConfirmBody`；
`settings.themeDeleteConfirm` `.deleteRoleConfirm` `.deleteManualConfirm`

### 三條拆不乾淨的，處理方式

1. **`chat.interAgentExpand`** — 英文內嵌單複數規則（`message` / `messages`），中文用「則」
   沒有複數。靜態片段表達不了那個 `s`。**做法**：拆成兩條可覆寫字串（單數、複數），
   分支邏輯留在程式碼裡；中文兩條填一樣的字即可。
2. **`tasks.terminateConfirmBody` 與同族的 `markDuplicateBody`、`expireConfirmBody`** —
   被引用的標題在句子中間，中英引號不同（`“”` / `「」`），中文句尾要「嗎？」而英文沒有對應物。
   **做法**：拆成「前段 + 後段」兩條可覆寫片段，各語言各自填；不強求前後段語意對齊。
3. **`monitor.machine.uninstallWarnBody`** — 兩個參數、數字卡在句中，英文 `member(s)`
   偽複數、中文量詞「位」。**做法**：每語言三段片段，並在鍵名上標明順序。

### 兩條順手併掉的重複

`monitor.machine.bootstrapErrorDetail` 改用既有的 `monitor.machine.bootstrapError`；
`office.outsource.label` 改用既有的 `office.outsource.title`——都是「同一句話兩個 key」。

### 兩條該改成查表而非模板

`workerDetail.statusLabel` 與 `mp.effortLevel` 實際上是「狀態 → 顯示字」的對照表，
不是模板。改成靜態物件葉子（repo 既有 `tasks.status` / `tasks.priority` 就是這個寫法），
產生器本來就會把巢狀物件成員收進白名單。

## 6. 主題名稱 bug（owner 2026-07-27 回報）

**現象**：套用精靈村主題包後，偏好設定的主題清單裡找不到「辦公室」——內建主題被改名成
「精靈村」，跟自訂主題同名。

**根因**：`profile.themeOffice` 同時是「辦公室這個場所的稱呼」和「內建主題的身分名稱」，
而它在可覆寫白名單裡。使用點：主題下拉選單、主題設定頁標題、以及**匯出內建主題時寫進
檔案的 `name`**（所以匯出檔也會被改名）。

**修法**：主題的**身分名稱**不可被主題包覆寫——由白名單產生器排除，不是另外手維一份清單
（手維的第二份清單正是這個 repo 一路在避免的東西）。導覽列那種「場所稱呼」（`nav.office`）
不受影響，照舊可以換。

**驗證**：一道測試在 `profile.themeOffice` 重新出現在可覆寫清單時會失敗；必須實際把它放回去
跑一次、看到紅了才算數。

## 7. 版面分區 token（owner 2026-07-27 加的範圍）

**起因**：整個控制台能被主題換色的結構只有兩層——頁底與卡片底。`.topbar`、`.nav-tabs`、
`.app__main` 都沒有自己的 `background`，一律吃 `body` 的 `--color-bg`，所以一個主題
只有「底色多深」這一個旋鈕，做不出區域層次。

新增三槽，預設值皆為 `var(--color-bg)`：

```css
--color-topbar-bg: var(--color-bg); /* 頂列 */
--color-nav-bg:    var(--color-bg); /* 頁籤列 */
--color-main-bg:   var(--color-bg); /* 內容區 */
```

主題作者因此有 5 層可用：`--color-bg`（最外畫布）／頂列／頁籤列／內容區／`--color-card`。

**向後相容**：預設值指回 `--color-bg`，所以只填 `--color-bg` 的既有主題包三區自動跟著走，
外觀零變化。預設值裡的 `var()` 只存在於產品自己的 CSS；主題包 JSON 仍只接受具體色。

⚠️ **匯出必須跳過「還坐在 alias 上」的分區槽**（review round-1 BLOCKER-3）：匯出走
`getComputedStyle`，它會把 `var(--color-bg)` **解析成具體色**。若原樣寫進包裡，
`ThemeSettings.handleAddNew` 用 `exportOfficeBaseTheme` 播種的**每一個新主題**，三個分區
都會被釘死在內建的 `#191c24`——作者改 `--color-bg` 只有 body 會動，分層當場失效
（既有主題包不受影響：它們沒有分區槽，fallback 照舊）。
- 判準**由 token layer 推導、不寫死三個名字**：`gen-theme-tokens.mjs` 掃 theme.css，把
  「定義值就是一個裸 `var(--other)`」的 token 連同它指向的目標一起產出成
  `THEME_ALIAS_DEFAULT_TOKENS`；`themeExport.ts` 的 `isUnsetAliasDefault` 只在該 token
  解析值 **等於** 其 alias 目標的解析值時跳過。作者真的挑了一個分區色就會不相等，照常匯出。
- 護欄：`lib/themeExport.test.ts`「omits an alias-default token still sitting on its
  alias…」+ 真瀏覽器量測（見下）。

**實測（真實瀏覽器量計算色，9 種寬度 × 兩種版面）**：
- 不填三槽 → 四層皆 `rgb(25,28,36)`，全部組合一致。
- 填入不同值 → 四層分明，窄版寬版皆成立。
- 播種→只改 `--color-bg` 的作者流程（Chromium 1440px）：修好之後種出 **68** 槽、
  `seededZones=[]`，四層皆 `rgb(58,160,106)`；把跳過那一行拿掉當對照組則種出 71 槽、
  三個分區被烘成 `rgb(25,28,36)` 不動——即 BLOCKER-3 的原症狀。

⚠️ **寬窄版的陷阱**：「寬版／窄版」不是視窗寬度，是使用者偏好（`<html data-layout="wide">`，
T-756f），開啟後解除內容欄的 1040px 上限。實測 gutter（1440px）：**窄版 200px／側，
寬版 0px**。

本票一度依 owner 2026-07-27 的「寬版左右兩邊還是要有一點外區」改成每側 48px，**同日他看過
實際效果後改回吃滿**（原話：「寬版好像真的看不出什麼，不然寬版就不要留白好了」）。
⇒ **最外層畫布（含背景圖）是窄版限定的一層，寬版完全看不到**——主題的層次感絕不能只押在
最外層那一色。chrome.css 保留了那條不變式的說明（單寫 `calc(100% - 96px)` 會讓寬版在
720~1136px 之間比窄版還窄），給下一個要加外區的人，不必重踩。

## 8. 主題包驗證的相容性政策（owner 2026-07-27 拍板）

§6 把主題身分名稱移出可覆寫清單後，既有主題包若覆寫過該 key 會**整包**被拒收
（實測：「精靈村」包 186 條用詞中僅此 1 條不合法，61 個顏色全數合法）。

owner 於回覆卡 `rc-1599a0026a80` 選項二拍板改為**寬容**：驗證遇到不認識的用詞代碼時
**略過該項、其餘照收**，不再整包拒收。他明確接受的代價是「主題作者打錯字會靜靜地沒有效果」，
換得「舊主題包不會因為產品收回某個可覆寫項目而整包失效」。

範圍僅限 `wording`；顏色與字型的驗證維持原樣（他裁的是用詞這一案）。用詞驗證的其餘規則
全部維持嚴格：語言允許清單、每語言條數上限（以**原始**送入條數計，避免用垃圾 key 灌爆）、
控制字元、值長度。

**這是 wire 行為的改變，spec 必須同步（root CLAUDE.md §13 spec-first）**：
`spec/openapi.json` 的 `ThemeBundleDTO.wording` 與 `SettingsUpdateDTO.custom_themes`
原本寫「the key whitelist → 422」「any violation is a 422 and nothing is written」，
與改後行為**相反**，已改寫成「不認得的代碼 = 丟棄 + 200 + 回傳被裁剪過的 echo」，
並重跑 `bin/gen-ocapi`（絕對路徑）＋ `npm run gen:api`。行為面由
`conformance/test_rest_happy.py::test_settings_custom_theme_unknown_wording_code_is_dropped_not_rejected`
釘住（同時釘「語言仍是 422」那一半，證明寬容只及於 code）。

**丟棄對 operator 不靜默**：使用者面的靜默是 owner 的裁定，但 `wording_bundle.go` 的
`dropUnknownWordingCodes` 每次丟棄都寫一行 server log（bundle 位置＋代碼，最多列 10 個、
其餘由 count 承載）——對齊 `api_helpers.go` 的「不要靜默丟棄」原則。

**舊資料列在讀取時也裁剪**：`settings.go` 載入 `display.custom_themes` 後同樣跑一次
`dropUnknownWordingCodes`（**只裁剪、不再拒收**：白名單縮小不該讓設定載入整個爆掉）。
沒有這一步，T-081b 之前寫進 DB 的 `profile.themeOffice` 會一路被 `GET /api/settings`
回顯到 owner 下次剛好又 PATCH 主題為止。護欄：`TestLoadAuthSettingsCustomThemes`。

## 9. 外區背景圖與它的鋪法（owner 2026-07-27 兩次裁定併進本票）

主題包多一個**只給最外層畫布**的背景圖欄位。頂列／頁籤列／內容區三個分區**不開放吃圖**：
它們底下坐著文字,文字壓在花紋上沒有可讀性保證。

```jsonc
"backgrounds":     { "canvas": "data:image/png;base64,…" },      // 圖(格式檢查同頭像,大小上限自己一份 — 見下方 T-72da 註)
"backgroundModes": { "canvas": "tile" | "sides" | "cover" }      // 鋪法,預設 tile
```

- **圖片驗證沿用既有的頭像那一套**(相同格式白名單、magic bytes),當時**解碼後 ≤64 KiB**、
  沒有另開規則、沒有放寬上限。
  > 🔴 **上面那句的後半(「沒有放寬上限」)已於 T-72da 失效,保留原文以存記錄。**
  > owner 於 **2026-08-03** 親自推翻 T-081b 這條裁定:背景圖鋪滿整個視窗,他現有的背景圖
  > 已貼著 64 KiB 上限、連講三次「太糊」。⇒ **`backgrounds` 改為解碼後 ≤512 KiB**
  > (data-URI 字串長度上限同步從 96 KiB 提到 704 KiB——那一層在解碼之前,不動它等於沒放寬),
  > 而 **avatars / logo / navIcons 維持 64 KiB 一個字都沒動**。
  > 當時之所以寫成「不另開規則」,前提是**背景與頭像共用同一道 gate**,所以放寬背景必然
  > 連頭像一起放寬;T-72da 把兩個 size cap 變成閘的參數之後,那個前提就不成立了。
  > 格式白名單 / magic bytes / SVG 拒絕這半**完全沒有放寬**,它跟大小正交。
- **`tile`**(預設,也是這個欄位存在之前的唯一行為):雙軸重複鋪滿。**沒填 mode 的主題包
  ——包含這個欄位出現之前寫的每一個包——鋪法一個像素都不會變。**
- **`sides`**(owner 回覆卡 `rc-f73e58129f06` 選項一):**整個畫面左右各一份、兩軸都不重複**,
  貼齊視窗底部、原尺寸不縮放,其餘留 `--color-bg`。給「左右各一棵樹」這種**站得住的物件**用,
  不是紋理。
  - **釘在視窗上(`background-attachment: fixed`),不是釘在文件上。** 實測(5000px 高的內容)
    這個控制台的文件**真的會捲**,而畫布背景預設跟著文件捲——那會讓第二棵樹隨捲動出現,
    而且**圖畫多高都救不了**(頁面能一直長)。owner 看到早期版本的第一反應正是
    「為什麼還會有重複的樹」,`fixed` + `no-repeat` 才是真的解決它。
- **不提供鏡像**(owner 2026-07-27 原話:「不應該提供鏡像功能,要鏡像應該由主題包自己
  處理就好」)。⇒ 右側是同一張圖、同一個方向;要左右對望請把對稱畫進圖裡。
- **`cover`**(owner 回覆卡 `rc-f0e23286d75e` 選項二併進本票):一張圖縮放填滿整個視窗、
  置中、不重複、同樣釘在視窗上。

### `cover` 為什麼不是「多一個列舉值」而已

**實測**(`docs/T-081b-evidence/canvas-cover*`):只填圖不做別的,`cover` **看不到**——頂列/
頁籤列/內容區三層各有自己的**不透明**底色,把圖整片蓋掉,可見範圍跟 `sides` 一樣只剩兩側。
要做出 owner 想的「控制台浮在一張淡淡的村莊上」,必須**同時把那三層調成半透明**。

好消息是那**不需要新欄位**:顏色文法本來就收 `#RRGGBBAA` 與 `rgba()`,所以主題包今天就能
把三層(以及 `--color-card`,owner 另外問過)填半透明。實測四項全過:三層調透 → 圖從三層
透出;5084px 長頁捲到 2000px → 仍蓋滿、無橫向溢出;**手機 375px 也看得到**(這是 `cover`
與 `tile`/`sides` 最大的差別,後兩者在無外區時完全不可見)。

⚠️ **代價要講明白**:那三層底下坐著文字(頂列的工作室名稱、頁籤名與未讀數字、區塊標題),
調透就等於把文字放到圖上,**產品不再能保證可讀性**——這正是本票其餘部分刻意迴避的事。
owner 於 `rc-f0e23286d75e` 在看過這個取捨後選擇併進本票,**知情接受**。產品端能做的是把
話講清楚(編輯器在選 `cover` 時直接顯示這段提醒),對比度由主題自己負責。

**實作刻意只動 CSS 變數,不加 DOM 圖層**:`body` 的 `background-repeat` / `background-position` /
`background-attachment` 改吃 `--canvas-bg-repeat` / `--canvas-bg-position` /
`--canvas-bg-attachment`,三者在 theme.css 的**預設值就是 tile 的值**;`sides` 時由
`i18n/index.tsx` 把同一個 url 寫成兩層。少一個圖層就少一種疊放次序與 z-index 的風險。

**驗證側**:`backgroundModes` 與圖共用同一組區域 key,值只收 `tile`/`sides`,而且
**該區域必須真的有圖**——只填鋪法不填圖畫不出任何東西,那是筆誤,值得指名而不是忽略。
前後端兩份驗證同構(`themeBundle.ts` / `avatar_bundle.go`),spec 同步後重跑
`bin/gen-ocapi` ＋ `npm run gen:api`。

**實測(真瀏覽器讀像素,窄版 8 種寬度 × 寬版 7 種寬度,`docs/T-081b-evidence/canvas-sides*`)**:
左右各一份貼齊視窗底部、**上方與內側都量到純底色(證明兩軸都沒重複)**、內容欄從未被滲入、
**所有寬度都沒有多出橫向捲動**;另跑一個 5084px 的長頁捲到 2000px,圖仍釘在視窗底部且仍只有
一份;`tile` 對照組仍雙軸重複、無圖對照組仍純色。

⚠️ **兩種鋪法在外區 0px 時都看不見**——那包含 ≤1040px 的視窗、手機,**以及寬版的全部寬度**
(owner 已改回寬版吃滿,見 §7)。這是最外層畫布的性質,不是 `sides` 造成的:
**背景圖是加分項、不是骨幹**,實際看得到它的情境是「窄版 + 視窗寬於 1040px」。

---

## 附註：驗收判準已由 owner 改過

原票面的「跑 audit.py 到低於 3:1 歸零」經實測**不具鑑別力**（改動前就已經是 0，詳見
基準線報告）。owner 2026-07-27 於回覆卡 `rc-800a7adb224f` 選項一拍板改為「雙面驗證 +
防回歸檢查」：要證明每個新 token 的兩種用途能各自調到達標，且要證明**拆分前不存在**
單一值能同時滿足兩邊，另加一道被合併回去就會紅的自動檢查。
