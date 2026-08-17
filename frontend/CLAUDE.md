# frontend/ — React SPA

進入 frontend/ 時 nested-load。本檔只放前端共通路由與驗證；repo-wide 憲章見根目錄 CLAUDE.md。

## path-scoped 規則

細規則在 frontend/.claude/rules/，依主題拆成七份。每份都必須保留 paths frontmatter。

- paths 的 glob 相對 rules 檔所在的 frontend/ 目錄；寫 src/...，不要寫 frontend/src/...。
- 漏掉 paths 會變成無條件全域載入；glob 太窄或 base 寫錯會讓真正需要它的人完全看不到規則，而且不會警告。
- 判不準時往寬寫；新增規則先檢查實際 source path 能命中。

| 規則檔 | 主要不變量 |
|---|---|
| data-layer.md | API seam、錯誤 envelope、SSE reconcile、共享 settings snapshot |
| presence-and-badges.md | presence 視覺映射、未讀 badge、自報 telemetry、機器時效 |
| chat-and-reply-cards.md | 排程 custom、聊天跳轉、composer、回覆卡與任務跳轉 |
| tasks-and-outsource.md | 任務篩選/錨點、TaskCard、外包面板、任務手冊 |
| overlays-and-modals.md | 全幅閱覽、Esc 分層、DocCard、差異呈現 |
| css-layout-traps.md | 長 token、nowrap、浮層邊界、CSS ownership、lazy fetch、動作列 |
| theming-and-i18n.md | 首設與設定、i18n、主題包、用詞編輯、pre-paint |

各規則檔只留讀碼才能知道的契約與護欄；事故日記、票號、一次性量測、mutant 數字與過時清單不在這裡。

## verify

純前端 UI 改動的本機順序是 headless build → preview:4173 → Playwright。開 PR 後，以雲端那一輪全綠作為可合併判準；不上 production 驗證。Monitor.tsx 的 mock 是純前端資料，不代表 telemetry backend。
