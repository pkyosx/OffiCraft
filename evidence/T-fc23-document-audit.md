# T-fc23 文件對質

## 範圍

盤點 `docs/`、`README`、`CLAUDE.md`、腳本註解、`seeds/`、前端 seed import 與產生的 embed 資產，確認本次移除倉庫 CI 記號與瘦身後沒有殘留的舊交叉引用或過期數字。

## 結果

- `rule:conflicting-authorities` 與 `defers-to:`：working tree 的 `seeds/system_interaction.md`、`docs/guide/best-practices.md`、`CLAUDE.md`、`seedsdist`、`docsdist` 均無命中。HEAD 原本在 seed 與使用說明各有命中，這次已移除。
- 舊的 `system_interaction.md §4.1`／「第三種開卡理由」：目標文件、seed、腳本與前端 component 無命中；`CLAUDE.md` 與使用說明的交叉引用已改為 §2.2「來源衝突時」。其他 `§4.1` 命中屬 SSE 規格或測試的不同章節，未改動。
- `system_interaction.md`、`boot_sequence.md` 等檔名：前端 `?raw` import、seed parity test、generated API schema、server boot assembly 註解都是 source-of-truth／資料流說明，保留；沒有把檔名渲染到設定頁內容。settings list 的脆弱檔名文字斷言已移除，detail page 的 filename-chip DOM 斷言保留。
- 舊的 45,000／45,045 字數註解：`docCap.ts`、real-seed visual guard、story、BootDocPage、其測試與 frontend overlay rule 已改成不依賴過期固定數字的描述；owner 最新 live seed 實測為 11,776 chars／27,035 bytes。
- rule-defer guard：已從「seed HTML marker＋site hash」改為 test-plane review digest。它只在 canonical「來源衝突時」段落改動後要求重讀 restatement，不宣稱檢查複述是否忠實；`bin/tests/rule-defer-guard.sh` 實跑 5 ok／0 failed。

## Owner 裁定後的測試契約處理

owner 透過 `rc-fd04e10a6a7a` 明確裁定：「我們不應該驗證系統互動要有什麼內容」。因此移除只鎖 boot context 歷史文案、段落位置、固定 risk／退休措辭、任意總字數的測試；保留 lessons API 權限、worker／staff 組裝、runtime boot seed 路由、worker spawn wire、seed export/fold 與 dangling-reference 結構驗證。`worker_crossref_test.go` 的 parser positive control 改用獨立 synthetic probe，不再要求新版 seed 必須含任意數量的 heading／reference。

本次刪除的內容守衛包括：`worker_handover_lessons_t4595_test.go` 的 §8b／§10.4 固定文案斷言、`worker_sharedcore_test.go` 的固定 risk／退休文案斷言、`worker_crossref_test.go` 的 30,000 字與 risk vocabulary 守衛，以及 `frontend/src/api/seeds.test.ts` 對特定 seed 句子、大小與已不存在的 `{OWNER_ID}` placeholder 的斷言。未新增「新版 context 必須包含某段文字」的替代測試；seed parity 仍驗證前端匯出值與 repo seed 的結構性一致。

owner 另要求保護固定 code block 的介面名稱，避免工具更新後文件過期；新增 `bin/tests/system-interaction-examples.sh` 與 `test-system-interaction-examples` CI target。它會解析 `seeds/system_interaction.md` 的 fenced blocks，驗證 MCP 範例的 jsonc／`tools/call`／註記與 payload 名稱，並對目前 `spec/mcp-catalog.json` 的工具清單；CLI 範例則驗證 text block、註記與 command line 一致，並以實際 `ocagent --help` 確認命令名稱仍存在。這是文件介面同步測試，不是 agent 行為 E2E。

## 驗證命令

- `bash bin/build-seedsdist`
- `bash bin/build-docsdist`
- `bin/tests/rule-defer-guard.sh`
- `bash -n bin/tests/run.sh`
- `bash -n bin/tests/rule-defer-guard.sh`
- `make test-system-interaction-examples`（7 MCP examples、2 個標記 CLI commands、5 個 help command lines：pass）
- `git diff --check`
- `go test ./server/ocserverd` focused boot-context／assembly／cross-reference／lessons behavior tests（pass）
- `bash bin/ci.sh`（commit `0a81268b79ab21554820ab05353b2ce57645c112`、session `84927`）：rc=0、28/28 checks 完成，`server/ocserverd` package `ok`（197.208s）、frontend unit／CT、paint guards、conformance（1,235 passed）均通過；最後一行逐字為 `[ci] all green`。這是 owner 最新 live seed 同步後的實際全量結果；若後續只修改證據檔，仍需對新 commit 重跑 CI。

早先在隔離 worktree 尚未安裝 `vitest` 時的 focused run 曾被 `vitest: command not found` 阻擋；後續完整 CI 的 `build-frontend-deps` 已安裝依賴並完成 frontend unit／CT／paint guards，該中間狀態不再是目前驗證結論。
