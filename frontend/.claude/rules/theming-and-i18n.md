---
paths:
  - "src/i18n/**"
  - "src/paint/**"
  - "paint-guards/**"
  - "scripts/**"
  - "src/lib/theme*"
  - "src/lib/paint*"
  - "src/lib/seasonal*"
  - "seasonal-test-alias.ts"
  - "src/lib/imageCap*"
  - "src/lib/stillCanvas*"
  - "src/components/ThemeSettings*"
  - "src/components/theme-settings.css"
  - "src/components/FirstRunPage*"
  - "src/components/LoginPage*"
  - "src/components/ProfileDropdown*"
  - "src/AuthGate.tsx"
  - "src/api/auth.ts"
  - "src/styles/**"
---

# 首設與設定、i18n、主題包、用詞、應景主題與 pre-paint

## 首設與伺服器設定

real mode 的 AuthGate：有 token 直接進 App；無 token 先打一次公開 auth/status，未設密碼進 FirstRunPage，已設密碼進 LoginPage。啟用碼可由 query 預填，set-password 成功後直接保存新 token，並以 history.replaceState 移除網址中的 code。網址帶 `?login=`（host 上 `ocserverd login-link` 印的一次性登入連結）時，掛載當下就移除它，並在 "checking" 兌換——已有 token 也照樣兌換（手機上殘留的舊 token 不可默默吞掉有效連結）：成功換上新 token 進 App；失敗時原本有 token 就保留它進 App、不顯示提示，沒有 token 才回登入牆並顯示失效提示（429 顯示節流訊息）。兌換一次頁面載入只送一次——碼是一次性的，StrictMode 重跑 effect 再送一次會把好連結變成拒絕。mock mode 不走這面牆。

設定 › 主題沒有分頁，是上下兩張帶標題列的卡（`.doc-card__head` 的外框）：「外觀」（標題列右邊是恢復預設，先確認）與「主題庫」（右邊是匯入、新增；列表不畫縮圖，免得為了縮圖下載主題包）。外觀這張卡就是 `ThemeAppearancePanel`（選主題、時間表、應景主題開關、背景動畫、恢復預設），兩個宿主：設定頁直接展開它（owner：「直接展開好了，不用再另外跳」），沒存的列由 ThemeSettings 持有、進出匯入／編輯畫面仍在，離開頁面才丟；ProfileDropdown 的 preferences（「主題」標籤下）放一整列可點的 `ThemeEntryRow`（畫面上那套的縮圖、名稱、名稱下一行的小標——時間表那一格「依時段」、應景主題蓋過時「應景」——與箭頭），點了開「主題設定」對話框（`ThemeAppearanceDialog`，裡面就是這張卡，標題列只留恢復預設，不重複「外觀」）。preferences 另有語言與版面。主題庫的列不能點選套用（owner 2026-11-05）。對話框 portal 到 body；從 preferences 開時選單同時收起（不然它疊在對話框上），對話框由 ProfileDropdown 在收起狀態下繼續 render，關閉時焦點回到頭像按鈕。settings 經 getServerSettings/patchServerSettings 即時生效，載入失敗就不渲染設定區，不捏預設。settings 只帶「現在選哪一套主題」（display_theme）、是否顯示應景主題（display_seasonal_theme）與依時間切換的時間表（display_theme_schedule），主題本身不在其中——見「主題編輯與清單」。密碼 set/change 不走會把 401 轉成登出的 typed client，而走 credentialPost；成功後換上 server 新 token。

跨分頁、跨裝置：別處存了設定（`settings` delta，也包含重新連線與回到前景時的 resync），i18n provider 用 refreshServerSettings 重讀 settings 與主題清單並套用，跟登入 reconcile 同一條路。本分頁自己的設定或主題寫入還沒落地時，重疊的讀取不採用（可能是寫入前的值），全部寫完再讀一次。新增、刪除主題不推送（owner 裁定），「外觀」那張卡（兩個宿主）每次掛上時重讀主題清單。時間表的列只有在沒被改過時才跟著別處的更新換；改過、還沒存的列留著，自己按存檔則採用 server 存下的結果。

## i18n

Locale 是 zh/en 封閉聯集；xian 是主題，不是語系。mobile 的 720 斷點由 useIsMobile.ts 與 CSS media query 各自使用，改動時要兩側同改。

可帶參數的文案要拆成可覆寫的靜態葉子，由 i18n/compose.ts 組裝；不可在 dictionary 直接放 interpolation function。句中參數用 lead/tail，只有空格差異用 sp；狀態映射也用靜態可覆寫葉子。compose 測試要釘住 zh/en 的逐字輸出。

⚠️ **這條規則今天沒有被遵守，也沒有任何東西在執行它 —— 這是一筆已知欠款，不是現況的描述**（Kyle 2026-09-06 裁定，T-93 第二輪）。實量：本包之前 `locales/{zh,en}.ts` **各有 22 個** dictionary interpolation function（`origin/main` 與當時 HEAD 同數；量法 `grep -cE '^\s+[A-Za-z_][A-Za-z0-9_]*:\s*\(.*=>'`），本包再加 8，成為各 30。**唯一在乎這件事的機制 `scripts/gen-message-keys.mjs` 只是把 function leaf 排除在白名單外，它不會叫**，所以違反這一句從來不會有任何東西變紅。

代價是具體的：**那 30 個葉子的文字，主題包永遠改不動**。這正是這一句存在的理由，也就是說那個能力今天在 30 個地方是壞的。

⇒ **不要把這一句改成「描述現況」**——那等於把一個已知缺陷升級成設計。也**不要要求下一張碰到它的票當第一個還債的人**：在一張跟 i18n 無關的票裡搬 30 個葉子（外加補 `compose.test.ts` 的 EXPECTED 列），是拿那張票去付別人的欠款。**新的帶參數文案照現況寫沒有錯，直到有人專門開一張票把 30 個一起搬完為止；那張票還沒有被開。**

themeIdentity 子樹是主題自己的身分名稱，產生 message key 時整支跳過；nav.office 仍可覆寫。匯入 wording 遇到未知 code 要丟棄並在 UI 顯示一次 warning，不可把整包判錯，也不可靜默吞掉；真正非法的 token、保留 id 或注入仍拒絕。既存主題包套用時只改既有 string leaf，不因白名單變小而清洗。

## 主題編輯與清單

主題有自己的資源，不搭 settings：清單 `GET /api/themes` **只回 id 與 name**，完整 bundle 一次一套（`GET /api/themes/{id}`），寫入與刪除也是一次一套。理由是量：bundle 內嵌圖片，整組回傳是幾百 KB 到 MB 級，那正是拆家要消滅的東西。

⚠️ **清單列不是 bundle**。需要 colors／wording／avatars／logo／navIcons／backgrounds 的地方一律去取那一套；`ThemeListItem` 與 `ThemeBundle` 是兩個型別，**不要為了少一個型別而合併成「欄位都 optional」的一個**——那會把編譯期錯誤換成一個安靜的空主題。走捷徑用手上已有的 bundle 前，必須先確認它的 id 就是要的那個 id。

wording 值逐字保存，只對「是否為空」做 trim 判斷；句子片段的邊界空白不可被編輯器吃掉。

用詞清單必須一次 render 全部列，不做 virtualization、overscan 或只渲染 N 列的上限。這保留鍵盤 Tab/讀屏順序、瀏覽器 Cmd+F、整頁複製與列印。清單本身仍是固定高度 scroll box；aria-setsize/posinset 依目前搜尋結果更新。大清單測試要縮小 query scope，不要改用 id 查詢而失去 label 綁定的驗證。

匯出主題時跳過仍是裸 alias 的 token，避免把內建跟隨值烘成固定值；alias 名單由 theme token generator 推導，不手抄。

## 內建主題的來源

theme.css 內建主題（office、office-light）的顏色以 repo 根目錄 `themes/<id>.theme.json` 為唯一來源；`theme.css` 各內建區塊裡的 `--color-*` 宣告是 `scripts/gen-builtin-themes.mjs` 產生的，只改值、補缺、刪多餘，註解與非顏色的行（canvas、radius、font、color-scheme）照手寫保留。改顏色就改 JSON 再跑 `npm run gen:builtin-themes`、`npm run gen:tokens`；直接改 theme.css 的值會被 drift-builtin-themes 擋下。theme.css 仍是 token 白名單與各 lint 讀的那份文字，所以白名單照舊從它產生。office 以外的內建主題不可用 `var()`（gen-theme-tokens 會把它收成假的 alias），產生器會拒絕。內建主題檔不會被 seasonal 的 glob 打包成 chunk。

辦公桌三套（`modern-office-desk-day`／`-dusk`／`-night`，`BUNDLED_BUILTIN_THEMES`）也是內建主題，但不是 theme.css 區塊：它們帶背景圖與頭像，以整份 `themes/<id>.theme.json` 出貨，由 `lib/bundledBuiltinThemes.ts` 的 lazy glob 各自成一個 chunk、第一次要畫或預覽時才抓（seasonal 的 glob 把它們排除），畫法跟自訂主題一樣疊在 office 底上，pre-paint 也一樣讀 `LS_THEME_PAINT`／`LS_SCHEDULE_PAINT`。`isBuiltinTheme` 只指 theme.css 那兩套（`data-theme` 畫的）；辦公桌三套用 `isBundledBuiltinTheme`，選單與主題庫用 `PICKER_BUILTIN_THEMES`。它們的 id 是保留 id：前端 RESERVED_THEME_IDS 與驗證文法、server 的 displayThemeAllowed／reservedThemeIDs 都擋自訂主題用它；只有 paint 記錄的驗證（`allowBundledBuiltinId`）放行，否則它們的快取畫面永遠過不了驗證。

瀏覽器原生控制項（日期輸入的日曆圖示、下拉選單、捲軸）跟著 `color-scheme` 畫，不讀顏色 token：office 區塊寫 `color-scheme: dark`、office-light 寫 `light`；主題包沒有這個欄位，`applyThemeToRoot` 依包裡的 `--color-bg` 亮度在根元素寫 `color-scheme`（讀不出顏色就不寫），並記進 ledger，換主題時跟 token 一起清掉。少了它，深色主題上的日曆圖示是黑色、看不到（owner `c-fc36b24d278c`）。

## 圖片與背景

主題包的 backgrounds 只接受 canvas。圖片仍共用 PNG/JPEG/WEBP MIME、magic bytes 與嚴格 base64 驗證，SVG 永拒；topbar 等有文字的區域不可順手開背景圖。

頭像、logo、導覽圖示沿用 64 KiB decoded / 96 KiB encoded cap；canvas background 是 1 MiB decoded / 1400 KiB encoded cap，兩層都要同步調整。TS 端由 imageCap.test.ts 對 bin/tests/fixtures/image-cap-cases.tsv 驗，Go 端沒有測試讀這張表；ThemeSettings 的背景 picker 必須走 isValidBackgroundValue。CSS 同時保留 background-color 與可選的 --canvas-bg-image，窄版不因背景產生 layout overflow。

動態的 canvas（animated WebP、APNG）可以只畫第一格：provider 套上主題後把它轉成一張靜態 blob 圖寫進 `--canvas-bg-image-still`（同一套鋪法、記進 ledger，換主題時一起清），CSS 有它就優先用它。何時轉由外觀卡的「背景動畫」決定：自動（預設）只在觸控裝置（`(hover: none)`，主要輸入無法 hover）轉，不看寬度與方向——手機橫放、平板照樣靜止，用滑鼠的電腦縮到手機寬度照樣播放；版面的 720 斷點與它無關。靜止是任何裝置都轉、播放從不轉。這個選項只存在這台瀏覽器的 localStorage（`oc.bgMotion`），不送 server、不跟著 settings 同步，登出（`resetPreferences`）也不清；provider 以 `bgMotion`／`setBgMotion` 提供、寫到根元素的 `data-bg-motion`，改了立即生效；恢復預設也把它改回自動。選項文字只寫「自動／播放／靜止」，「自動」的意思寫在名稱下的提示行（`themeBgMotionSub`），select 用 `param-select` 依內容定寬、手機寬也跟名稱同一行；不要把說明塞回選項，也不要套 `ts-pick-row`（會把 select 擠到下一行）。「自動」的條件在 TS 只寫在 stillCanvas.ts 的 `AUTO_STILL_QUERY`、CSS 只寫在 global.css 那條 media query，改一邊要改另一邊。pre-paint 不轉，所以開頁到轉好之前會先畫一下動畫；轉檔失敗或第一格全透明就一直畫動畫。`--canvas-bg-image-still` 不可在 theme.css 宣告預設值，否則 fallback 永遠不生效。

## pre-paint

pre-React 上色的三道守衛分工固定：記錄驗證、build artifact 形狀、真實瀏覽器逐幀載入。fixtures 由 paintFixtures.ts 統一，stub server 使用的 JSON twin 要由測試 deep-compare。

逐幀守衛必須在登入態、server 認得的主題與 VITE_USE_MOCK=false 下執行，並斷言 settings 真的回 200、主題真的套上；沒有前提就 setup error，不可空跑變綠。正向案例要同時驗顏色、字體、canvas 圖與 canvas mode 在 React mount 前出現；探針必須讓 runner 以非零 exit code 失敗，取樣數要達最低門檻，不可只判 >0。stub server 要同時服務 settings 與 themes 兩面，且延遲要一起套用：只延遲其中一面，量到的閃爍視窗會比真實短。

stub server 的埠不可寫死：playwright-paint.config.ts 用 allocateFreePorts() 向 OS 要沒人用的埠，spec 端一律由 PAINT_GUARD_OK_URL／PAINT_GUARD_UNKNOWN_URL 讀回來，沒有預設值——預設值就是釘死的埠，兩份工作副本同時跑就會搶同一個。settingsStub.mjs 的 listen 失敗一定要自己講出失敗原因（埠被佔用就說埠被佔用、說它自己沒壞），否則 runner 只會印「web server 起不來」，跟 stub 本身壞掉長得一模一樣。

注入案例要用 server 不認得任何主題的 stub，否則合法的 server theme 會偽裝成 pre-paint 洩漏。paint 記錄只在真的拿到 bundle 之後才寫；拿不到就不動它——用空值覆蓋等於自己清掉快取的畫面，下一次 pre-auth 載入就會閃。pre-paint 與 i18n 只能透過 api/auth 的 TOKEN_KEY 與 themePaint 的 LS_THEME、LS_THEME_PAINT、LS_SEASONAL_THEME、LS_SEASONAL_PAINT、LS_THEME_SCHEDULE、LS_SCHEDULE_PAINT 取儲存鍵，不可在 source 或探針硬寫 key。

## 應景主題

應景主題的檔案與時段是資料，放在 repo 根目錄 `themes/`（`seasonal-schedule.json` 與它指名的 `*.theme.json`）；TS 不寫日期或 id。時段以觀看者當地時間計，起點含、終點不含。時段重疊時，以 seasonal-schedule.json 裡排在前面的那一筆為準（owner 裁定）。畫面上的主題 = 時段內且 `display_seasonal_theme` 為 true 時是應景主題，否則是時間表當下那一列或 display_theme（見「依時間切換主題」）；display_theme 從不被改寫，應景主題不進主題清單，server 也不接受它當 display_theme。邊界由排到下一個邊界的 timer 切換，不輪詢；頁面重新可見或取得焦點時也重讀時鐘（timer 不計睡眠時間）。應景主題檔載入失敗就在這次頁面期間一直顯示 display_theme，不重試、也不再經過應景路徑（瀏覽器對同一個 URL 的失敗 dynamic import 會快取，重試只會立刻再失敗；只有重新整理能恢復）；載入中則不顯示 display_theme 的自訂主題。應景主題的 id 在前端的 RESERVED_THEME_IDS 裡：新增與匯入的自訂主題不可用它；但 server 不保留它，所以已存在 server 上、用這個 id 的自訂主題仍可編輯儲存——驗證文法（validateThemeBundle／validateThemeBundleWith）只擋內建 id，應景主題自己的 paint 記錄也要經過它。

seasonal-schedule.json 每一筆是 `{id, start, end}` 加上二選一：`theme`（整個時段一個主題檔，主題包的 id 要等於這筆的 id），或 `schedule`（2～6 列 `{start: "HH:MM", theme: "<x>.theme.json"}`，規則跟使用者時間表相同，該檔的 id 要等於檔名主幹 x）。`seasonalEntryProblem` 是形狀的唯一判準，`seasonalTheme.test.ts` 拿它與 bundle 文法驗真實檔案。有 `schedule` 的時段依觀看者當地時間挑列（`seasonalPickAt`，同 `lib/dayParts.ts` 的 `activeDayPart`：當天第一列之前是前一晚最後一列），只 lazy 載那一列的檔；列與列的邊界跟時段邊界走同一個 timer（`nextSeasonalBoundary` 只算開著的時段的列），不重新整理就換。時段內使用者時間表不生效（應景主題仍優先）；顯示應景主題關掉就回到使用者自己的設定。某一列載入失敗只讓那一列在這次頁面期間改顯示使用者的設定，不重試。

`lib/seasonalSchedule.ts` 會被 pre-paint 的 esbuild 單獨打包，不可放 `import.meta.glob` 或其他 Vite 專屬語法（esbuild 把 `import.meta` 換成空物件，模組初始化就丟 TypeError；那發生在 prePaint 的 try/catch 之外，整段 pre-paint 不會執行）；theme 檔只經 `lib/seasonalTheme.ts` 的 lazy glob 載入，時段外不會被抓。應景主題有自己的 paint 記錄（`LS_SEASONAL_PAINT`，PaintRecord 陣列，這個時段看過的每一列一筆，以主題包 id 對，只有當下那一列帶圖、其他只留顏色——下一列的 bundle 照樣提前 10 分鐘取進記憶體，到點切換不等網路，切過去時記錄跟著升降；應景主題不在畫面上時全部只留顏色——時段開始前 10 分鐘預取的第一列也是，時段一開始由畫面上那頁補上它的圖；別的時段的記錄不留；讀的時候也收舊版的單筆），不寫進 `LS_THEME_PAINT`，所以時段結束時選定主題的快取還在；時段內 pre-paint 只畫當下那一列的記錄或什麼都不畫，時段外記錄被清掉。從深色列換到淺色列時，`color-scheme` 由新那包的 `--color-bg` 決定，舊那包的 inline 屬性由 ledger 整批清掉——兩條路徑都有測試釘住。

測試不可跟著真實日期變：vitest 與 CT 用 `seasonal-test-alias.ts` 把 schedule 換成空的，應景行為用明確的時段測；paint guard 跑的是帶真實 schedule 的 build，所以每支 spec 都先釘住 Date（`frameProbe.pinClock`）。不要改用 `page.clock`：它連 requestAnimationFrame 一起假造，取樣器會記到瀏覽器從沒畫出來的幀。

## 依時間切換主題

時間表 `display_theme_schedule` 是 `{start: "HH:MM", theme}` 的清單，空清單是關閉；否則 2～6 列、start 不重複，theme 是內建 id 或已存在的自訂主題，server 依 start 排序存。畫面上的主題 = 應景主題（時段內且開著）＞ 時間表當下那一列（start 最晚且不晚於觀看者當地時間的那列；當天第一個 start 之前是前一天最後一列）＞ display_theme；display_theme 不被時間表改寫。到點切換與應景主題共用同一個 timer 與 visibilitychange／focus 重讀。刪除自訂主題時 server 在同一個 transaction 移除用到它的列、剩不到 2 列就清空，收據的 `display_theme_schedule` 是刪完的結果，前端照收，不自己重算。

預設外觀是時間表 06:00 白天／16:00 黃昏／18:30 晚上（辦公桌三套）、應景主題開著，display_theme 是 `modern-office-desk-day`（關掉時間表時顯示的那套）；前端在 `lib/themeSchedule.ts` 的 `DEFAULT_THEME_SCHEDULE`／`DEFAULT_DISPLAY_THEME`，server 端唯一的寫入者是 migration 00120：全新安裝與當時還在 office／office-light（或沒選過）的站各換一次，之後再選回 office 就留著；它不寫應景主題開關——沒設過就讀成開著，關掉過的站升級後維持關（owner 裁定）；同一個 migration 刪掉以辦公桌 id 存成自訂主題的舊匯入。兩邊的字面值是雙胞胎，改一邊要改另一邊（已釋出的 migration 不能改，要開新號）。「主題設定」的「恢復預設」先確認，再以一次 settings PATCH 寫回這三項（`restoreDefaultAppearance`），成功後把這台裝置的背景動畫改回自動，不動自訂主題。mock 的新站跟真的新站一樣是這份預設；要舊樣子（沒選過主題、沒有時間表）的測試在 `__resetMock()` 後呼叫 `__setMockNoAppearance()`。

時間表沒有「未設定」的第三態，登入 reconcile 一律採用 server 的值。`setThemeSchedule` 不是樂觀寫入：server 收下、當下那一格的主題取回之後才切換，所以存檔當下不會閃內建主題；被拒就 reject，畫面照舊。

pre-paint 讀 `LS_THEME_SCHEDULE`（`parseThemeSchedule` 驗過，不合格就當沒有時間表）挑出當下那列；自訂主題的畫面在 `LS_SCHEDULE_PAINT`（PaintRecord 陣列，一套一筆），每筆都經 `readValidatedSchedulePaint` 走同一個驗證器，id 對不上就不畫。開頁、reconcile、喚醒時只取當下那一格的主題（`scheduleBundles`），下一格在它開始前 10 分鐘（`SCHEDULE_PREFETCH_LEAD_MS`；開頁時已在這 10 分鐘內就立刻）先取，所以到點不等網路；應景時段的列用同一套規則（owner rc-56f6d5f48fe1）。記錄每取到一套就寫或更新那一筆，表中其他 id 已有的記錄留著、不在表中的 id 刪掉，所以去過的格子下次開頁不閃；從沒看過的那套開頁時會先短暫畫 office，這是接受的代價。⚠️ 只有當下那一格與下一格的記錄帶背景圖，其他看過的格子只留顏色與字體（`tieredPaintRecords`／`colourPaintRecordFor`），格子移動時跟著升降；Safari 的 localStorage 約 2.5M 字元、寫入被拒時整個 key 會被清掉，三套辦公桌都帶圖就會超過（測試以真實主題檔量測預設時間表，上限 1.6M）。⚠️ 應景主題在畫面上時（時段內、開著、當下那一列沒有載入失敗），只有應景主題當下那一列帶圖：`LS_SCHEDULE_PAINT` 每筆都只留顏色，`LS_THEME_PAINT` 若在就換成同一套只留顏色的記錄（讀的人本來就收沒有圖的記錄，所以換而不刪，時段結束時顏色還在）；開頁時存著的舊整筆記錄也照這條改寫。應景主題離開畫面（時段結束、關掉、那一列載入失敗）就回到上面的分級：手上有 bundle 的格子立刻帶回圖，其他的等它被取到（當下那格、或下一格開始前 10 分鐘）時帶回。改分級時先寫要拿掉圖的那一邊，再寫要帶圖的那一邊。測試以真實感恩節主題檔量測時段內（預設時間表、與帶 1 MiB 背景的自訂 display_theme 兩種）與時段開始前 10 分鐘整個 origin 的字元數，上限 2.5M。只有顏色的記錄 pre-paint 照樣畫對顏色與 color-scheme，圖等 bundle 到了才出現。時間表開著時 display_theme 不在畫面上：`LS_THEME_PAINT` 清掉（不留一份跟某格重複的大記錄），它的 bundle 也不取，等它真的上畫面（時間表關掉、應景時段結束而沒有時間表）才取（`loadDisplayBundle`）；應景時段內同樣不取，記錄留著但只留顏色（見上）。應景時段開始前與結束前 10 分鐘也各預取一次（時段第一列、時段結束時使用者那一格）。喚醒跨過邊界、那套還沒取到時先畫它的快取記錄再取。某套取不到就保留它已快取的那筆，不用空值覆蓋；但整筆寫入被拒（容量滿）時要移除這個 key，因為留下的舊紀錄可能早於上次編輯，下次開頁會先畫改之前的顏色——寧可不畫。「主題設定」面板裡「依時段切換」開著時只顯示時間表、不顯示單選的「主題」選單，兩者不會同時出現，所以不會有「選了沒反應」的狀態；從對話框開時，關掉對話框就丟掉沒存的列。面板的縮圖要自訂主題的 bundle：每次掛上一套只取一次（`useThemeSwatches`），失敗就畫空白，不重試；preferences 那一列的縮圖直接用 provider 手上畫在畫面上的那套（`shownThemeBundle`），不另取。`lib/themeSchedule.ts` 與 `lib/dayParts.ts` 會被 pre-paint 打包，同樣不可放 Vite 專屬語法；`dayParts.ts` 不 import 任何東西，因為 seasonalSchedule.ts 要用它，而 themeBundleCore 又 import seasonalSchedule。
