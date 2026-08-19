---
paths:
  - "src/i18n/**"
  - "src/paint/**"
  - "paint-guards/**"
  - "scripts/**"
  - "src/lib/theme*"
  - "src/lib/paint*"
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

real mode 的 AuthGate：有 token 直接進 App；無 token 先打一次公開 auth/status，未設密碼進 FirstRunPage，已設密碼進 LoginPage。啟用碼可由 query 預填，set-password 成功後直接保存新 token，並以 history.replaceState 移除網址中的 code。mock mode 不走這面牆。

ProfileDropdown 的 preferences 內含主題、語言與 server settings；settings 經 getServerSettings/patchServerSettings 即時生效，載入失敗就不渲染設定區，不捏預設。settings 只帶「現在選哪一套主題」（display_theme），主題本身不在其中——見「主題編輯與清單」。密碼 set/change 不走會把 401 轉成登出的 typed client，而走 credentialPost；成功後換上 server 新 token。

## i18n

Locale 是 zh/en 封閉聯集；xian 是主題，不是語系。mobile 的 720 斷點由 useIsMobile.ts 與 CSS media query 各自使用，改動時要兩側同改。

可帶參數的文案要拆成可覆寫的靜態葉子，由 i18n/compose.ts 組裝；不可在 dictionary 直接放 interpolation function。句中參數用 lead/tail，只有空格差異用 sp；狀態映射也用靜態可覆寫葉子。compose 測試要釘住 zh/en 的逐字輸出。

themeIdentity 子樹是主題自己的身分名稱，產生 message key 時整支跳過；nav.office 仍可覆寫。匯入 wording 遇到未知 code 要丟棄並在 UI 顯示一次 warning，不可把整包判錯，也不可靜默吞掉；真正非法的 token、保留 id 或注入仍拒絕。既存主題包套用時只改既有 string leaf，不因白名單變小而清洗。

## 主題編輯與清單

主題有自己的資源，不搭 settings：清單 `GET /api/themes` **只回 id 與 name**，完整 bundle 一次一套（`GET /api/themes/{id}`），寫入與刪除也是一次一套。理由是量：bundle 內嵌圖片，整組回傳是幾百 KB 到 MB 級，那正是拆家要消滅的東西。

⚠️ **清單列不是 bundle**。需要 colors／wording／avatars／logo／navIcons／backgrounds 的地方一律去取那一套；`ThemeListItem` 與 `ThemeBundle` 是兩個型別，**不要為了少一個型別而合併成「欄位都 optional」的一個**——那會把編譯期錯誤換成一個安靜的空主題。走捷徑用手上已有的 bundle 前，必須先確認它的 id 就是要的那個 id。

wording 值逐字保存，只對「是否為空」做 trim 判斷；句子片段的邊界空白不可被編輯器吃掉。

用詞清單必須一次 render 全部列，不做 virtualization、overscan 或只渲染 N 列的上限。這保留鍵盤 Tab/讀屏順序、瀏覽器 Cmd+F、整頁複製與列印。清單本身仍是固定高度 scroll box；aria-setsize/posinset 依目前搜尋結果更新。大清單測試要縮小 query scope，不要改用 id 查詢而失去 label 綁定的驗證。

匯出主題時跳過仍是裸 alias 的 token，避免把內建跟隨值烘成固定值；alias 名單由 theme token generator 推導，不手抄。

## 圖片與背景

主題包的 backgrounds 只接受 canvas。圖片仍共用 PNG/JPEG/WEBP MIME、magic bytes 與嚴格 base64 驗證，SVG 永拒；topbar 等有文字的區域不可順手開背景圖。

頭像、logo、導覽圖示沿用 64 KiB decoded / 96 KiB encoded cap；canvas background 是 512 KiB decoded / 704 KiB encoded cap，兩層都要同步調整。TS/Go 以 bin/tests/fixtures/image-cap-cases.tsv 做 twin；ThemeSettings 的背景 picker 必須走 isValidBackgroundValue。CSS 同時保留 background-color 與可選的 --canvas-bg-image，窄版不因背景產生 layout overflow。

## pre-paint

pre-React 上色的三道守衛分工固定：記錄驗證、build artifact 形狀、真實瀏覽器逐幀載入。fixtures 由 paintFixtures.ts 統一，stub server 使用的 JSON twin 要由測試 deep-compare。

逐幀守衛必須在登入態、server 認得的主題與 VITE_USE_MOCK=false 下執行，並斷言 settings 真的回 200、主題真的套上；沒有前提就 setup error，不可空跑變綠。正向案例要同時驗顏色、字體、canvas 圖與 canvas mode 在 React mount 前出現；探針必須讓 runner 以非零 exit code 失敗，取樣數要達最低門檻，不可只判 >0。stub server 要同時服務 settings 與 themes 兩面，且延遲要一起套用：只延遲其中一面，量到的閃爍視窗會比真實短。

注入案例要用 server 不認得任何主題的 stub，否則合法的 server theme 會偽裝成 pre-paint 洩漏。paint 記錄只在真的拿到 bundle 之後才寫；拿不到就不動它——用空值覆蓋等於自己清掉快取的畫面，下一次 pre-auth 載入就會閃。pre-paint 與 i18n 只能透過 api/auth 的 TOKEN_KEY 與 themePaint 的 LS_THEME、LS_THEME_PAINT 取儲存鍵，不可在 source 或探針硬寫 key。
