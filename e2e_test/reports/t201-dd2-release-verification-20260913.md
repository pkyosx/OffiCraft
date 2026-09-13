# T-201 v0.5.369 驗證交接

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

## 未涵蓋範圍

Brad／Tzu-Hui 外部恢復未驗證；移除整段 probe 的後續版本尚未取得固定 SHA，未作該版本審查。
