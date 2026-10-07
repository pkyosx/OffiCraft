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
  - "src/components/ThemeSettings*"
  - "src/components/theme-settings.css"
  - "src/components/FirstRunPage*"
  - "src/components/LoginPage*"
  - "src/components/ProfileDropdown*"
  - "src/AuthGate.tsx"
  - "src/api/auth.ts"
  - "src/styles/**"
---

# 首設與設定、i18n、主題包、用詞與 pre-paint

## 首設與伺服器設定

real mode 的 AuthGate：有 token 直接進 App；無 token 先打一次公開 auth/status，未設密碼進 FirstRunPage，已設密碼進 LoginPage。啟用碼可由 query 預填，set-password 成功後直接保存新 token，並以 history.replaceState 移除網址中的 code。網址帶 `?login=`（host 上 `ocserverd login-link` 印的一次性登入連結）時，掛載當下就移除它，並在 "checking" 兌換——已有 token 也照樣兌換（手機上殘留的舊 token 不可默默吞掉有效連結）：成功換上新 token 進 App；失敗時原本有 token 就保留它進 App、不顯示提示，沒有 token 才回登入牆並顯示失效提示（429 顯示節流訊息）。兌換一次頁面載入只送一次——碼是一次性的，StrictMode 重跑 effect 再送一次會把好連結變成拒絕。mock mode 不走這面牆。

ProfileDropdown 的 preferences 內含主題、語言與 server settings；settings 經 getServerSettings/patchServerSettings 即時生效，載入失敗就不渲染設定區，不捏預設。settings 只帶「現在選哪一套主題」（display_theme），主題本身不在其中——見「主題編輯與清單」。密碼 set/change 不走會把 401 轉成登出的 typed client，而走 credentialPost；成功後換上 server 新 token。

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

## 圖片與背景

主題包的 backgrounds 只接受 canvas。圖片仍共用 PNG/JPEG/WEBP MIME、magic bytes 與嚴格 base64 驗證，SVG 永拒；topbar 等有文字的區域不可順手開背景圖。

頭像、logo、導覽圖示沿用 64 KiB decoded / 96 KiB encoded cap；canvas background 是 512 KiB decoded / 704 KiB encoded cap，兩層都要同步調整。TS 端由 imageCap.test.ts 對 bin/tests/fixtures/image-cap-cases.tsv 驗，Go 端沒有測試讀這張表；ThemeSettings 的背景 picker 必須走 isValidBackgroundValue。CSS 同時保留 background-color 與可選的 --canvas-bg-image，窄版不因背景產生 layout overflow。

## pre-paint

pre-React 上色的三道守衛分工固定：記錄驗證、build artifact 形狀、真實瀏覽器逐幀載入。fixtures 由 paintFixtures.ts 統一，stub server 使用的 JSON twin 要由測試 deep-compare。

逐幀守衛必須在登入態、server 認得的主題與 VITE_USE_MOCK=false 下執行，並斷言 settings 真的回 200、主題真的套上；沒有前提就 setup error，不可空跑變綠。正向案例要同時驗顏色、字體、canvas 圖與 canvas mode 在 React mount 前出現；探針必須讓 runner 以非零 exit code 失敗，取樣數要達最低門檻，不可只判 >0。stub server 要同時服務 settings 與 themes 兩面，且延遲要一起套用：只延遲其中一面，量到的閃爍視窗會比真實短。

stub server 的埠不可寫死：playwright-paint.config.ts 用 allocateFreePorts() 向 OS 要沒人用的埠，spec 端一律由 PAINT_GUARD_OK_URL／PAINT_GUARD_UNKNOWN_URL 讀回來，沒有預設值——預設值就是釘死的埠，兩份工作副本同時跑就會搶同一個。settingsStub.mjs 的 listen 失敗一定要自己講出失敗原因（埠被佔用就說埠被佔用、說它自己沒壞），否則 runner 只會印「web server 起不來」，跟 stub 本身壞掉長得一模一樣。

注入案例要用 server 不認得任何主題的 stub，否則合法的 server theme 會偽裝成 pre-paint 洩漏。paint 記錄只在真的拿到 bundle 之後才寫；拿不到就不動它——用空值覆蓋等於自己清掉快取的畫面，下一次 pre-auth 載入就會閃。pre-paint 與 i18n 只能透過 api/auth 的 TOKEN_KEY 與 themePaint 的 LS_THEME、LS_THEME_PAINT 取儲存鍵，不可在 source 或探針硬寫 key。

## 應景主題

應景主題的檔案與時段是資料，放在 repo 根目錄 `themes/`（`seasonal-schedule.json` 與它指名的 `*.theme.json`）；TS 不寫日期或 id。時段以觀看者當地時間計，起點含、終點不含。畫面上的主題 = 時段內且 `display_seasonal_theme` 為 true 時是應景主題，否則是 display_theme；display_theme 從不被改寫，應景主題不進主題清單，server 也不接受它當 display_theme。邊界由排到下一個邊界的 timer 切換，不輪詢。

`lib/seasonalSchedule.ts` 會被 pre-paint 的 esbuild 單獨打包，不可放 `import.meta.glob` 或其他 Vite 專屬語法（esbuild 不認得，pre-paint 會整段失效而被 try/catch 吞掉）；theme 檔只經 `lib/seasonalTheme.ts` 的 lazy glob 載入，時段外不會被抓。應景主題有自己的 paint 記錄（`LS_SEASONAL_PAINT`），不寫進 `LS_THEME_PAINT`，所以時段結束時選定主題的快取還在；時段內 pre-paint 只畫應景記錄或什麼都不畫，時段外記錄被清掉。

測試不可跟著真實日期變：vitest 與 CT 用 `seasonal-test-alias.ts` 把 schedule 換成空的，應景行為用明確的時段測；paint guard 跑的是帶真實 schedule 的 build，所以每支 spec 都先釘住 Date（`frameProbe.pinClock`）。不要改用 `page.clock`：它連 requestAnimationFrame 一起假造，取樣器會記到瀏覽器從沒畫出來的幀。
