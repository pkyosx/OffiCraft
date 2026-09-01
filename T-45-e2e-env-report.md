# T-45 — e2e environment evidence

測量基準固定為完整 commit `c8d2506f386bce15f731c50fb53d5c8e06e9c62b`，
worktree 為 `/Users/seth_wang/ai_workspace/officraft-t45-e2e-env-x107`。共用
`/Users/seth_wang/ai_workspace/OffiCraft` 沒有 checkout；正式 port `7755`
本輪一直由既有 `ocserverd` PID `3821` 持有，未碰。所有以下站台操作都在
own worktree 的 `8791` 隔離 DB／binary 上，沒有滿載測試。

## A — 修法前重現

- `evidence/t45-recon-20260901-091510`：setup 在獨立 exec 分別耗時 16s、
  11s，均 rc=0；exec 結束後下一個 health probe 失敗。原始 stderr：
  `curl: (7) Failed to connect to 127.0.0.1 port 8791 after 0 ms: Couldn't connect to server`。
- 同目錄的持續 shell positive control 在 6s 內取得 HTTP 200、listener PID
  `28720`；差異是 exec 邊界，不是 server route。
- 同 shell 的完整 positive control 在
  `evidence/t45-recon-20260901-092650`：setup 9s、health 200 且
  `git_sha=c8d2506f`、login 200、Chromium spec `1 passed / 1s`、teardown
  0s。兩個中間 rc=1 已辨識為量測 wiring（SHA 長短比較、漏傳
  `OC_E2E_PASSWORD`），不是產品根因。

## A — 候選與採用

候選 ① 是每輪唯一私有 tmux socket/session；代價是 tmux 變成明確依賴，
並增加 namespace/state/teardown。候選 ② 是 launchd 等外部 service；代價是
跨平台、權限、job/pid/socket identity 與清理複雜度最高。候選 ③ 是同 shell
明文契約加跨 exec hard-fail guard；代價是 agent 拆步仍不可用，且 guard 必須
可執行並有 red mutant。本包採 ①，未採 ②/③。

採用前的實際 tmux positive control 在
`evidence/t45-recon-20260901-093249-tmux`：私有 socket
`oc-t45-x107-server-20260901`、session `t45-server`，下一個獨立 exec 讀到
`has-session rc=0`、listener PID `42087`、API HTTP 200、
`git_sha=c8d2506f`，command 是 own `.state/ocserverd serve`；exact
`kill-session rc=0`、8791 released，7755 untouched。

## A — 修法後回歸

變更位於：

- `e2e_test/lib/tmux.sh`：tmux prerequisite、`oc-e2e-*` namespace validation、
  exact session start/stop。
- `e2e_test/setup.sh`：每輪建立並記錄唯一 tmux socket/session，server 活過
  setup exec 邊界。
- `e2e_test/teardown.sh`：先停止 state 指定的 exact session，再做既有 exact
  PID/port cleanup；不猜 incomplete identity。
- `e2e_test/tests_guard/run.sh`：接線、缺 tmux、fleet socket refusal、fake
  tmux positive control，以及 `MUT-T-45`。
- `e2e_test/README.md`：獨立 exec 契約與手動流程。
- `e2e_test/CLAUDE.md`：實作者入口的 tmux／獨立 exec 隔離規則。

本輪 `evidence/t45-impl-20260901-setup`：

- setup：11s，rc=0。
- 下一個獨立 exec 的 health：0s，rc=0；tmux
  `oc-e2e-748320878b234bef882786a3367f9ebe` 的 has-session rc=0，listener
  PID `60320`，`/api/version` HTTP 200，`git_sha=c8d2506f`，command 是
  own `.state/ocserverd serve`。
- 再下一個獨立 exec 的同一支
  `tests/02_monitoring_hardware_cards.spec.js`：3s，rc=0，原始 Playwright
  stdout 為 `1 passed (1.3s)`，stderr 空。
- 清理：exact private `kill-session` 成功；teardown 0s、rc=0，原始輸出為
  `[teardown] :8791 released` 與 `[teardown] ✅ clean`；最終 8791 無 listener，
  7755 仍為 PID `3821`。tmux state files 已移除。
- 語法／whitespace：4 個 bash `-n` 與 `git diff --check` 全部 rc=0。
- `e2e_test/tests_guard/run.sh`：最終 strict namespace 版本 35s，rc=0，`PASS=310 FAIL=0`、
  `[tests_guard] all green`。`MUT-T-45` 證明移除 namespace guard 後 fleet
  socket command 會真的通過，因此 shipped guard 是 load-bearing。
- strict namespace guard 加入後的最後一輪實際重跑：setup 21s rc=0；下一個
  exec health 0s rc=0（socket/session
  `oc-e2e-24770e711bc745c0a071b84986ac34dd`、listener PID `9943`、API 200、
  SHA `c8d2506f`）；再下一個 exec spec 2s rc=0、`1 passed (1.2s)`；exact
  kill rc=0、teardown 0s rc=0，8791 released、7755 仍 PID `3821`。

沒有開 PR、沒有 merge；交由 Kyle 複審與後續 land。

## B — cmux/browser discovery（獨立、尚未定案）

Joey 提供的三次歷史一手紀錄只能以相鄰訊息區間表示：

- `2026-08-31 18:35:13.577–18:36:24.084 +08`
- `2026-08-31 19:31:15.660–19:33:28.492 +08`
- `2026-08-31 21:26:14.502–21:27:21.040 +08`

三次 `agent.browsers.getForUrl(...)` 原文都是 `No browser is available`，
接著 `agent.browsers.list()` 原文都是 `[]`；沒有 backend/driver/port/PID
或可操作 tab。區間不是呼叫瞬間，不能冒充精確 timestamp。

Joey 在 2026-09-01 自己的 member session 做了現在的對照：

- `CMUX_WORKSPACE_ID=<unset>`、`CMUX_SURFACE_ID=<unset>`、
  `CMUX_PANE_ID=<unset>`，printf rc=0。
- 前後 `cmux workspace list`：stdout 空；stderr 原文
  `Error: Failed to write to socket (Broken pipe, errno 32)`；rc=1。
- 唯一一次 `cmux browser open "https://officraft.hardcoretech.link/"`：
  stdout 空；同一 stderr；rc=1；未點擊、未登入，沒有可觀測 workspace/surface/tab
  變化。

Kyle 的目前 session 另有不對稱對照：`cmux version` rc=0、
`cmux browser status` rc=0 且輸出 `enabled`；確定不存在的子命令輸出
`Error: Unknown command: definitely-not-a-command`；只有 workspace/socket
路徑回 broken pipe。這證明不是「所有 cmux 路徑都死」，但仍不能把 broken
pipe 分成「無 workspace」或「socket 本身壞掉」。Chrome extension 路徑不列入
B，因為它和 cmux browser 是不同子系統。

在 recon 當時，射程問題「成員設計上是否應該用得到 cmux browser」仍無答案。
以上只證明 Joey 現在的環境，不回推昨日三次；也不能把 B 寫成 cmux 程式
bug 或設計上不用。四次被擋是事實，並未因 A 找到機制而結案。

## B — owner 裁定與可執行契約

2026-09-01 owner 回覆白話決策卡 `rc-6f9385ec29f6`，選項 `[0]`：**成員做
e2e 不使用 cmux browser，一律走 A 的隔離 Playwright；cmux browser 不算
支援路徑。** 因此 B 不被記成 cmux 程式已修好或已證明壞掉，而是把使用者
從不支援的 browser-tool 路徑導回正式 e2e 路徑。

本包將這個決定落在 own worktree：

- `e2e_test/run_all.sh` 預設宣布 `Playwright`；明確設定
  `OC_E2E_BROWSER_BACKEND=cmux` 時，在 setup 前以 rc=2 拒絕，原文為：
  `[run_all] FATAL: OffiCraft members do not use cmux browser for e2e; run 'bash e2e_test/run_all.sh' or setup.sh -> Playwright -> teardown.sh instead. NOT a server failure.`
- `e2e_test/README.md` 與 `e2e_test/CLAUDE.md` 都明寫成員不走 cmux；看到
  `No browser is available`／`[]` 時停止重試 cmux，改跑
  `bash e2e_test/run_all.sh` 或 setup → Playwright → teardown。
- `e2e_test/tests_guard/run.sh` 新增 B 契約檢查與 `MUT-T-45/B`；移除 cmux
  refusal line 會被測試抓到。語法檢查與 `git diff --check` rc=0，tests_guard
  最終為 rc=0、`PASS=317 FAIL=0`、`[tests_guard] all green`。

這個 repo 沒有 cmux CLI 的攔截入口，因此守衛能保證的是本 harness 的明確
backend 選擇與使用者指引；它不宣稱能攔截 repo 外部的 `cmux browser open`。
Joey 歷史三次與現在 session 的原始證據仍保留為 B 的背景，且不回推成同一
根因。`rc-2bb4e5f7d7c1` 的站台重啟歸因是另一個 owner 問題，不由本包代判。
