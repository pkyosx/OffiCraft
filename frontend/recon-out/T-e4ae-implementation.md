# T-e4ae：C1 實作與驗證

日期：2026-08-16（Asia/Taipei）
裁定：owner 於 reply-card `rc-511c2e373f29` 選擇 **C1：內容感知逐欄 floor 96px**。

## 實作範圍

- `src/components/Markdown.tsx` 新增 opt-in 的 `tableSizing="content-aware"`。renderer 在真實 DOM 量每欄 cell 的不換行 intrinsic width，短欄保留自己的寬度，長欄套 96px floor；未 opt-in 的 Markdown host 不變。
- `src/components/TaskCard.tsx` 只替任務卡 description opt-in。
- `src/components/tasks.css` 只在手機 breakpoint 啟用產生的欄寬變數，並讓表頭保持單行；共用 `.doc-md table` 沒有改動，因此聊天、文件卡、說明頁沒有被這包改變。
- `visual-guards/te4ae-mobile-table.ct.spec.tsx` 用真實 `TaskCard` 與完整祖先鏈覆蓋 390／320px；`stories/TaskCardSixColTableStory.tsx` 使用 `docs/design/worker-panel-parity.md` 的既有六欄表格形狀與內容，並在報告中揭露 repo 沒有可直接重用的六欄 mock task payload。

## 真瀏覽器結果

兩個 viewport 都確認：表頭每欄 1 行、內容最大 8 行、短的第一欄沒有被統一撐到 96px、至少一個長欄達到 90px 以上；table 自己 `scrollLeft=50` 可讀回，任務列表／頁面／卡片／description 的橫向溢出皆不超過 1px。

- 390px：description 316px → table 498px，table 自己多 182px 可捲。
- 320px：description 246px → table 498px，table 自己多 252px 可捲。

## 鑑別力驗證

以下暫時 mutant 均曾實際套用、看到對應 guard 變紅後還原：

- 移除 `TaskCard` opt-in：marker／table scroller／header contract 失敗。
- 移除手機表頭 `nowrap`：超過 96px 的長表頭測試失敗。
- 將所有 cell 統一設為 `min-width:96px`：短的第一欄寬度斷言失敗。
- 移除 T-4aa0 的祖先 constraint：任務列表橫溢出斷言失敗。
- 把 table overflow 改成 `visible`：table `scrollLeft` contract 失敗。

## 執行過的檢查

- `npm test`：249 test files、2175 tests 全部通過。
- `npx playwright test -c playwright-ct.config.ts visual-guards/te4ae-mobile-table.ct.spec.tsx visual-guards/taskcard-md-overflow.ct.spec.tsx visual-guards/taskcard-longtoken-wrap.ct.spec.tsx`：9/9 通過。
- `npm run test:ct`：完整 CT 342 passed、4 skipped；其中 paint guard 11/11 passed。
- `npm run typecheck`（由 `npm run build` 執行）：通過；Vite production build 通過。
- `npm run lint:tokens`、`npm run lint:token-roles`、`git diff --check`：通過。

## 獨立審查

delegated reviewer `01a00b32-e075-7510-9e07-1717b9ec38d5`（Euler）讀取實作基準 SHA `6725526441b1251d74c5ace18149ad64e89da71b`，`git merge-base --is-ancestor` 回傳 0；其 review 檔為 `frontend/recon-out/T-e4ae-independent-review.md`，四項 DoD 結論為 PASS。該 reviewer 沒有重跑 npm／Playwright，測試證據以上一節的主執行者紀錄為準。

## 文件對質

已用本包關鍵字 `tableSizing`、`content-aware`、`data-md-table-sizing`、`md-table-column-min-width`、`96px`、`C1` 對 `docs/guide`、`README`、`docs/dev`、註解與 `go:embed` 資產做全庫掃描。殘留只有本包的程式／guard／報告與 recon 引用的 `docs/design/worker-panel-parity.md`；使用說明目前只描述一般 Markdown／手機導覽，沒有與本包互相矛盾的表格 sizing 承諾，因此沒有額外文件文字需要改動。

## 限制

repo 的 mock tasks、chat、replyCards、taskManuals 以空集合起始，沒有可直接重用的六欄任務 payload；guard 以現存設計文件的六欄表格作為 grounded stress fixture，而不是宣稱涵蓋完整線上資料分佈。後續若取得真實 payload，應再補一輪分佈對照。
