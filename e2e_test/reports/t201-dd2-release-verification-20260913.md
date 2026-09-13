# T-201 v0.5.369 驗證交接

Owner 目前要求盡快協助 Brad／Tzu-Hui 恢復使用；外部恢復仍未確認，本交接不能以 setup、版本或 API 證據代替 Brad／Tzu-Hui 的實際回覆。

## 狀態

候選／發布版驗證尚未完成；本輪收到 `context-high` 後交接，未宣稱出貨通過。

## 版本與祖先

- 目標發布：`v0.5.369`，tag commit `dd2bf5cffdc80daea0b25bec62358f9a54eb4ed7`。
- seth-m1 隔離工作樹 HEAD：同一個 `dd2bf5cffdc80daea0b25bec62358f9a54eb4ed7`。
- 驗證 `2552734ff15c58fd43ffa96d167d88d8e34bfda3` 可由 `git merge-base --is-ancestor` 證明為候選祖先。
- 隔離 `ocserverd` `/api/version`：`git_sha=dd2bf5cf`、`git_time=2026-09-13T14:07:53+08:00`、`catalog_hash=4b65eb86d2c9a311`。

## 已完成的實測

在 seth-m1 直接使用 Claude `2.1.267 (Claude Code)`，於隔離 `CLAUDE_CONFIG_DIR` 的 `.claude.json`，以實際 physical workdir 作為 `projects[workdir].mcpServers` key，分別種入 1、8、9、12 個 MCP，執行缺席名稱的 `claude mcp get`：

- 1 與 8 個設定：標記在原生輸出中可見。
- 9 與 12 個設定：原生清單只顯示前 8 項，分別以 `(and 1 more)`／`(and 4 more)` 截斷。
- 四次皆以 exit 1 結束，但都有 `Configured servers:` 輸出；因此不能用 exit code 判斷「沒有回答」。
- 9／12 個設定的最後標記未出現，只能說明原生清單省略，不能判定讀錯檔。

原始本機數量控制記錄另存於 agent workspace：
`/Users/seth_wang/.officraft/agents/m-f339ccc950b7/tmp/t201-candidate-mcp-counts-20260913.md`。

## 隔離站生命週期

- 使用 `8791`、repo-local SQLite `var/data/t201-candidate.db`，未使用 7757／7768。
- `e2e_test/setup.sh` 已成功完成 SPA、docs、seeds、bindist staging、migration、serve identity check。
- 隔離 listener 當時為本工作樹 `.state/ocserverd`，PID `89031`；private tmux identity 記錄在 `.state/tmux.socket`／`.state/tmux.session`。
- 尚未完成：真實 candidate warden／Claude consumer 的啟動收據、successful-with-warning 的真實 UI 可見性、probe-positive 的 successful-without-warning 正控制。
- 接手者應先以本工作樹的 `e2e_test/teardown.sh` 精確收掉隔離 tmux／listener，再執行 `ocagent clean` 清理本輪 `/tmp` 暫存；不可碰正式站或保留站。

本輪已執行 `bash e2e_test/teardown.sh` 並以 exit 0 完成：精確停止 `oc-e2e-937a21d4f0eb4e0f914e7ece87ec51a0` private tmux session、釋放 8791、刪除隔離 DB/state、將 `server/ocserverd/webdist` 還原為 pristine；輸出明確列出正式 7755／8770／8780／8766 未管理且未碰觸。

### 暫存清理證據

- 對原始 `/tmp/t201-mcp-counts3.dnTiMl`、`/tmp/t201-mcp-counts.YEunIH` 直接執行 `ocagent clean` 時，工具以 exit 2 拒絕，明確回報路徑在 agent workdir 外，且 `NOTHING was moved`。
- 將這兩個已確認的 exact fixture 移入 `/Users/seth_wang/.officraft/agents/m-f339ccc950b7/tmp/` 後重試成功：兩者均 exit 0，分別 quarantine 至 `trash/tmp/t201-mcp-counts3.dnTiMl` 與 `trash/tmp/t201-mcp-counts.YEunIH`；未使用 `rm -rf`。
- 候選 git worktree 的報告已先提交並推送；接手者可依 branch／SHA 重新取得，不把唯一證據留在暫存目錄。

## artifact 權限交接

報告與原始數量控制記錄已存入 chat attachment：`att-c8df886ff2f9`、`att-fc62b8b65736`。嘗試由本成員直接釘到 T-201 時，server 回覆 `caller is not the task's executor`；T-201 executor Kyle 需使用上述 attachment id 釘成 task artifacts。

Kyle 另提供 #498 固定 SHA 的補充本機紅綠證據 attachment `att-24418f9f2424`；內容明確標示不是獨立審查或 UI 通過，下一代仍須從固定 SHA 進行獨立 review。

## 未涵蓋範圍

Brad／Tzu-Hui 外部恢復未驗證；移除整段 probe 的後續版本尚未取得固定 SHA，未作該版本審查。
