# officraft — builder CLAUDE.md

This file is read by Claude Code agents working in this repo.

## 讀法與範圍

這份檔案只放 repo-wide、讀碼不一定看得出的不變量與工作邊界。`server/`、`cli/`、`frontend/`、`conformance/`、`e2e_test/` 的域內規則，讀各自的 `CLAUDE.md`；`frontend/.claude/rules/` 的規則依 `paths:` 條件載入。不要把域內細節或會頻繁變動的清單複製到這裡。

`.claude/rules/` 與 `paths:` 是既定的隨需載入邊界；`@import` 不會節省 context，不要用另一套拆檔方式取代它。

## 核心不變量

1. **乾淨身分、不借權**：agent 與 warden 只用自己的 scope token；owner token 不外借，warden 不代辦 agent 的個人行為。遇到向上借權或繞過治理 choke 的設計，停下並對齊 owner。

2. **參考邊界**：身分、auth、scope、治理面對齊 vibe-clicking 的成熟行為；presence／lifecycle 依 officraft 的 state model，不要把兩套模型混成一套。

3. **狀態來源分層**：`desired_state`、角色、機器意圖等 durable intent 存在 DB；online 是 member 或 warden 自己持有的 live SSE 連線 projection，不能從 DB flag、別人的回報、session 或 pid 推算。`waking`／`stopping` 是由連線事實與時間錨點衍生的狀態。

   member 與 warden 使用同一套 online 判定；member 的起停決策留在 server，warden 是執行手。`presence_state()` 是唯一的 lifecycle→visual 衍生入口，不要在畫面或另一個 service 再造一份。現在沒有自動 relocate；換機器要依明確的下線、改意圖、再上線流程處理。權威細節在 `docs/design/state-model.md`。

4. **授權單一化**：caller 身分永遠取自 verified token 的 `sub`；`member_id`／`agent_id` 只表示 target，不能表示 caller。principal 由單一 resolver 分類，路由在 `server/ocserverd/routes.go` 宣告最低 `requires`，不得由各 handler 各自猜權限。

   owner 與 admin agent 才能做治理操作；普通 agent 只能做被授權的自身操作。lessons 的 self-role 寫入與跨 role 寫入依同一授權模型，沒有 role 的 worker 不能藉空字串取得寫入權；read 不因 caller 是誰而放寬。warden 不是治理 principal。每條 route 都要在註冊時通過 boot-time fail-closed assertion；漏寫或寫未知 floor 不能靠「目前沒有 agent 知道這條路」當安全邊界。

5. **token 權威在 server**：server 決定要起哪個 member、mint 該 member 的 token，並在派工時交給 warden；warden 與 agent 都不 mint、不自 bootstrap、不自行決定 auth。過渡中的 pull bootstrap 程式碼不是設計目標，先讀相關 spec 再改。

6. **context 跟碼共存**：描述設計意圖的文件與程式碼一起更新；刪 legacy code 時同步處理其 context。doc 與 code 不一致時，不得直接假定 doc 過時；先查 authoritative source，必要時請 owner 決定。

7. **完整 manifest**：push 前逐一審 `git diff --stat`／`git show --stat` 的新增、刪除、移動檔案；任何說不出必要性的檔案都要停下查清。這道人工檢查防的是預期檔以外的密鑰、暫存物或 scratchpad 跟著一起上遠端；path denylist 與 secrets scan 是硬防線，人眼審 manifest 仍是第一道。

## Repo-wide 約定

- 可執行／可 import 單元的 folder、Go module／package、binary 名稱保持同名。`ocagent` 呼叫 shim 與 `com.officraft.ocwarden` launchd label 是另外兩個介面契約，改動需同步處理 host 端。
- 新 server endpoint 走 `RouteSpec` table-driven 路由，包含 handler 與 test；wire 變更遵守下面的 spec-first 流程。
- 對外 DTO 新增欄位預設 optional，避免破壞既有 client；需要破壞相容時先找 Seth／owner 對齊。
- commit message 以 `[why]` 說明動機、`[how]` 說明關鍵改法；署真實執行模型的 Co-Authored-By，不用不實名稱。

## 驗證、CI 與出貨

- **先查 source of truth**：任何「完成」「能跑」「現況是 X」都回到 git、實際輸出或 CI 讀回驗證；exit code 或工具回應成功本身不等於驗收完成。比較基準用 `origin/main`，不要把本地孤兒 `main` 當現況。
- **wire spec-first**：HTTP／MCP wire 先改 `spec/openapi.json` 並讓 owner 過目，再依生成流程更新產物；`spec/mcp-catalog.json` 是生成物，不是手改入口。行為面由 conformance 收官。
- **完整與點名 CI 的判綠不同**：完整 `bash bin/ci.sh` 必須同時是 rc 0 且最後一行精確為 `[ci] all green`；`bash bin/run-checks.sh <target>...` 只看被點名項目的各自完成標記，不要套用完整 CI 的 marker。要跑哪些項目，以 `.github/workflows/ci.yml` 的實際指令與 `Makefile` 的具名 target 為準，不在文件裡另抄清單。
- **Actions 守衛是可執行權威**：不要在文件裡列 job 名稱、數量或 gate 對應；清單會在 workflow 變更後靜默過期，而不會提醒讀者。以 workflow 內的 `oc-job-role`、workflow 的 trigger／job `if`／`needs`／permissions 與 `bin/tests/auto-beta-guard.sh` 的檢查為準；job 不得用 `continue-on-error` 偽裝成功。expression 分隔符使用雙引號可能讓 workflow 啟動失敗而變成零 jobs；它與 shell／普通 YAML 字串、單引號字面值不是同一格，修改時讓 guard 驗證，不要做寬鬆的全域引號正規化。
- **PR 才是 land 流程**：從 `origin/main` 開分支，先跑與改動相關的本機檢查，再 push 分支、開 PR，讀回確認分支與 check；受保護的 `main` 不直推。合併判準看 PR 上的雲端整輪 checks，不能用本機綠代替；rebase 後以 `git push --force-with-lease` 更新分支，禁止裸 `--force`，並重新取得雲端結論。main／CI 綠也不等於已部署，部署要走 release 並從版本 source of truth 驗證。
- **release 先做不可跳過的 CI**：`bin/release publish --beta <tag> --target <sha>` 會在目標 commit 的乾淨 staging worktree 內跑完整 `bin/ci.sh`；必須 rc 0、最後一行精確為 `[ci] all green` 且 tracked tree 沒變，否則不 build、不 package、不 upload、不 tag。這道 preflight **沒有 skip switch**；`--dry-run`／`--no-settle` 不是 CI bypass。
- **CI 副本隔離**：同一份 clone 一次只跑一輪 `bin/ci.sh`；要並行就另開 clone。不要把 `.ci-lock` 當成可忽略的雜訊。
- **binary 與 TCC 例外**：binary 預設 fresh build、不 commit；唯一 owner 核准的例外是 `dist/officraft/officraft` 與其三份 manifest，依 `dist/officraft/BUILD.md` 和 `bin/check-officraft-dist` 驗證，不得擴張例外。
- **程序安全**：隔離站啟動時記下自己的 PID／port，收尾只處理自己捕獲的 exact PID；禁止 `pkill -f`、`killall` 或按程式名批次殺行程。

## Repo map

- `server/`：Go production server（REST、SSE、MCP、reconcile、migration、SPA embed）；先讀 `server/CLAUDE.md`。
- `cli/`：`ocagent` 與 `ocwarden` 的自更新工具；先讀 `cli/CLAUDE.md`。
- `frontend/`：React／Vite／TypeScript SPA；共通規則在 `frontend/CLAUDE.md`，窄範圍規則在 `frontend/.claude/rules/`。
- `conformance/`：HTTP-only、語言無關的 wire 行為回歸權威；`e2e_test/`：隔離環境的 Playwright 流程，絕不碰 production。
- `spec/`：凍結 wire 契約；`seeds/`：runtime seed 資產；`bin/`／`Makefile`：可執行檢查與建置；`docs/`：較長的設計與操作說明。

## 變更前自問

拿掉一段內容前，必須能回答：「誰會因為少了它而做錯什麼事？」能指出具體受影響主詞，就保留或移到真正對應的程式碼／域文件；只能證明可能過時、但無法自行判死的內容，列入疑似過時清單交人判斷。
