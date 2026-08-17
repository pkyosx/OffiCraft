# e2e_test/ — Playwright 端到端

進入 `e2e_test/` 時讀本檔；repo-wide 規則在根目錄 `CLAUDE.md`。本檔只保留 e2e harness 會讓實作者猜錯的隔離、生命週期與驗證邊界。

## 1. target 與一次 run

- Go `ocserverd` 是唯一 target；入口是 `bash e2e_test/run_all.sh`。每輪使用隔離 port（預設由 e2e config 指定）、repo-local SQLite、臨時 owner password、fresh DB、exact-PID teardown；不能碰 repo 根 config 或 production server。產生隔離 `oc.toml` 時已有檔案要拒絕覆蓋，因為它可能正指向正式 DB。
- setup 必須在 server 前 stage 全部 embed assets：SPA→`webdist`、docs→`docsdist`、seeds→`seedsdist`、binaries/catalog→`bindist`，再 fresh build/migrate/serve。缺一項可能讓 server 起得來但 agent boot、MCP catalog 或 binary route 假綠／假紅。
- 失敗時 teardown 仍跑，但只處理本輪捕獲的 listener/process；不能用模糊 process kill。prod safety 依 `lib/common.sh` 從現行 source 取得 production ports、identity、residue 與 explicit isolation/destructiveness ack，不能把某個歷史 port 當唯一防線。

## 2. CI、本機與 live-agent 分界

- `bin/ci.sh` 的 e2e 相關守衛會在 disposable tree 執行/檢查 `run_all.sh` wiring；它不代表 Playwright specs 已跑。spec 的自動驗收由 workflow 的 macOS e2e job 與其 log 證明，本機要真的跑整套可用 `bin/local-ci.sh`。
- `run_all.sh` 必須把 Playwright rc 立即保存並印出，EXIT trap 必須經 `oc_e2e_teardown_on_exit`；不要直接呼叫 teardown 或讓後續命令覆蓋 spec rc。`assert-specs-ran.sh` 的呼叫者以 source query 為準，不在文件列 job/spec 清單。
- live-agent spec 由檔名後綴 `.live-agent.spec.js` 自我宣告，預設不跑；只有 `OC_E2E_LIVE_AGENT === "1"` 才 build/啟動真 ocagent/ocwarden、花 API 額度。`true`、`yes` 等值都視為未要求；不要改成 exclude flag，也不要自行把 live class 加進 CI。
- `playwright.config.js` 的 workers 必須維持單一 server/SQLite 的序列語意；要改並行先用 source/guard 證明隔離，不以加 workers 掩蓋假紅。

## 3. seven_gate 是另一條載體

- `e2e_test/seven_gate/` 不在 Playwright、`run_all.sh` 或 CI service run 裡；CI 只守它的 hermetic `tests_guard`。其 server-fact journal/judge、七 gate/兩 observation、live 未真跑界線與產物規則只讀 `seven_gate/CLAUDE.md`，本檔不複製步驟清單。

## 4. online member 與誠實前置條件

- server 的 online 純由 SSE `GET /api/events` 連線生命週期判定，不靠 TTL/heartbeat；需要真 online member 時，用隔離 tmux 內持續執行 `ocagent listen` 並帶 member token。`observed_host` 另由 presence POST 設定，machine token 由 server mint。
- STOP robust-stop、relocate 等鏈若真的需要 online session 或 observed host，隔離 runtime 不穩定時標 `precondition-blocked`，不要硬燒 token 只為得到一個看似完整的數字。relocate 的乾淨完成信號是 warden 回報的 `last_op*`，不是 member DTO 的 presence phase 或 reconcile 內部 phase。
- 新增/修改 spec 時先讀 setup/run/teardown、Playwright config、spec 自身與 production guard；把「沒跑過」與「跑過且通過」分開報告，不用本機 wiring 綠替代 PR 雲端 spec log。
