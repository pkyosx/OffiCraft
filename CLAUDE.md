# officraft — builder CLAUDE.md

This file is read by Claude Code agents working in this repo.

## officraft builder 架構憲章 v1

1. **乾淨身分、不借權**:agent/warden 只用自己 scope 的 token 操作 server;遇「向上借權」的設計立刻停下、回報 owner align,不默默接受。owner token 不外借、warden 特權不代勞 agent 個人行為。

2. **對齊 vibe-clicking golden reference——限身分/auth/scope/治理面**:這些面 vibe-clicking(:8766)是成熟參考,officraft 不自創偏離。要偏離先講清為什麼。(注意:presence/status 不在此列——見 §3,那是 officraft 自走的設計。)

3. **presence:投影 ≠ 狀態,分開存**(officraft 自走設計、非對齊 vibe——vibe 現況不同、未來也許才跟進):online 是純 SSE 連線投影(連上=True/斷=False,二態);waking_since / stopping_since 才是衍生 tri-state 狀態的錨點。兩者不同變數、不塞同一欄,presence_state() 是唯一衍生函數。**member 與 warden 同一套 online 投影**——warden 也「自己連上 SSE = online」(owner 2026-07-10 廢除舊「warden 不投影 online」);member 起停決策留 server、但**已砍自動 relocate**。權威 logical spec:`docs/design/state-model.md`。

4. **治理層 = {owner, admin_agent} only**(方案 B 語意面已落地):改 global context / role / lessons、mint 身分 token、bootstrap(mint member JWT 的 seam)這類動作,只有 **owner 或 admin agent** 能執行。admin agent = **agent-scope token 且其 member `role_key=="assistant"`**(耐久角色判定,非 hardcode id;seeded Mira 是第一個實例——舊「sub==mira」hardcode 判法已廢,split-brain 統一到 role 側,見 §14 / `docs/design/caller-identity-convention.md`)。⚠️ **warden 不是治理 principal**(machine class 在階梯最底——warden 零自決 auth,見 §6)。agent 不能自我提權呼叫治理端點;每條路由必須在表上宣告明確 principal 門檻,不靠「agent 不知端點存在」當安全邊界。**閉環**:bootstrap 同鎖 admin 以上,否則任意 agent token 可 `bootstrap {member_id:"mira"}` 拿 admin token 繞過治理 choke;hire 的 kind/role_key 提權面同鎖 admin 以上(否則 agent 可自僱一個 assistant)。

5. **RBAC 收斂到單一 resolver + 路由表宣告**:principal 分類收斂到 `server/ocserverd/authz.go`(單一 resolver:token scope + member kind/role_key → class `machine < agent < admin_agent < owner`,deny-by-default);每條路由在 route 表(`server/ocserverd/routes.go`)的 `RouteSpec.requires` 宣告最低 class,註冊時掛單一 choke,boot assertion fail-closed 驗每條路由都有宣告。治理寫入 + bootstrap 套 `admin_agent`、mint 套 `owner`,全走這一個 choke,不各自散寫。漏一個宣告 = app 拒起,不是一個裸奔洞。

6. **Spawn / token 權威模型(baseline · 正解 target)**:**server 是唯一 auth 權威**。reconcile 決策該起哪個 member 時,由 **server 自己 mint 該 member 的 token**,並在派工指令當下把 token **隨指令 push 給 warden**。**warden 與 agent 一律零 mint、零自 bootstrap、零自決 auth**:warden 是 stateless 執行手,拿 server 給的 token spawn;agent 拿注入的 token 幹活。⚠️ **現行 `warden-self-bootstrap` pull 路徑是過渡態**——正解 push 路徑的 scaffold 已在碼裡但 dormant(gate OFF),只差 server 端接線通電(P4),pull 路徑 P4 通電後退役。**別拿現行 live pull 碼當設計 target 重建心智模型**——正解是 server-mint → push,不是 warden/agent 自己去要 token。

7. **push 前審完整檔案 manifest**:push 前用 `git diff --stat`(未 commit)/ `git show --stat`(已 commit)把**每一個**新增、刪除、移動的檔逐一過一遍,確認**都有明確必要性理由**——不是只掃「預期改的檔」就放行。⚠️ 這是曾把 `owner_token`(真機密)+ scratchpad 垃圾誤 commit 進 origin/main 的**根因**:當時 review 只審預期檔、對雜檔視而不見,漏網檔就這樣上了遠端。manifest 上任何一個你講不出必要性的檔 = 停下、查清、清掉再 push。CI 第 7 道(path denylist + gitleaks)是硬防線,但**人眼審 manifest 是第一道**,別把它讓給 gate。

8. **context 與碼共存(context lives with code)**:設計/context 文件與其描述的碼 **co-located**——放在一起(模組旁的 doc、或 `docs/` 下對應路徑),讓後人一眼找得到某塊碼的意圖。紀律:(a) **改碼與更新其 context doc 在同一個 commit**,doc 永不落後於碼;(b) **刪 legacy 碼時同一 commit 刪掉它的 context doc**,不留孤兒化石;(c) **動某塊碼前先讀它的 context doc** 建立正確心智模型,改完**同步更新**。理由:過時的 doc/碼會讓後來的 builder 在**錯誤概念**上疊床架屋,愈疊愈歪——主動防之。⚠️ **關鍵護欄**:doc 與碼**不一致時,別擅自假設「doc 是舊的」就刪 doc 遷就碼**——很可能是**碼漂離了原始設計、而 doc 才是意圖(design intent)**。git timeline 只當線索、不當判決;**最終真相源是 owner(Seth)**:發現 doc↔碼 misalignment,**先跟 Seth 確認哪個對,別自行裁定**、更別靜默改掉任一邊。

9. **reviewer code-hygiene checklist(每次 land-flow review 必查)**:review 一個 land 前,除既有 §7 manifest 審查外,逐條過這四點——(a) 有無**該清而沒清的 legacy 碼 / 過時 doc** 被留下?(b) 本次改動有無**建在過時或錯誤的概念**上(疊在已漂移的碼/doc 之上)?(c) 動到的碼,其 **context doc 有沒有隨碼同一波更新**(§8)?**特別地:凡動到 agent 互動面(MCP 工具、`ocagent` CLI、agent 要照做的流程),`seeds/`(global context)必須同一批更新**——agent 只知道 seed 教的做法,seed 不更新=新能力對全 fleet 隱形(owner 定調 2026-07-12;反例:attachment 送端 land 了、seed 卻還教舊法)?(d) 一旦發現 **doc↔碼 misalignment,就 flag Seth**、不自裁哪個對。任何一點不過 = 擋下、align 清楚再放行。

---

## 開發約定 (conventions) — 每次 land 都適用

以下 §10–§13 是 repo-wide 的可操作約定,補足 §1–§9 憲章。**各域(server/ cli/ frontend/ conformance/ e2e_test/)另有自己的 `CLAUDE.md`**,記該域專屬慣例、進入該目錄自動 nested-load;本檔只記 repo-wide invariant + convention。CI 對其中可機驗的部分設 gate 硬 enforce(見 §13)。

10. **命名鐵律 folder = go-module = binary 同名**:一個可執行 / 可 import 的單元,其**資料夾名、模組 / 套件名、產出 binary 名三者必須同名**,不用連字號變體或縮寫別名。範例:`cli/ocagent/`(資料夾)→ `module ocagent`(go.mod)→ `ocagent`(binary),不是 `cli/ocagent/`。理由:讓人與 AI 從任一名字都能直接推到其他兩者,消除「folder 叫 A、binary 叫 B」的定位摩擦。⚠️ **介面名已對齊(2026-07-09 owner 定案)**:CLI 的**呼叫子命令名**(spawn 寫的 bare `ocagent` shim)與 **launchd label**(`com.officraft.ocwarden`)已一併收斂到 ocagent/ocwarden 命名。它們仍是**獨立的介面契約**(boot-prompt 呼叫名 + launchd 註冊 label,不是 folder/module/binary),改動需 **host 端協調**(重寫 shim / warden bootout+relaunch),不在 folder=module=binary 的 CI gate 覆蓋內(見 §13)。

11. **commit 訊息格式 `[why]` / `[how]`**:每個 commit 用 `[why] <為什麼要改:講清問題 / 動機,讓後人不必考古就懂意圖>` 換行 `[how] <怎麼改的:改了哪些關鍵處>`;body **避免 emoji**;結尾固定一行 `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`。理由:`[why]` 讓未來的人與 AI 直接讀到改動意圖,對齊 §8 context-with-code。

12. **結構約定**:
    - **路由 table-driven**:新增 server 端點 = 在 route 表(`server/ocserverd/routes.go`)加一行 `RouteSpec` + handler + test,不散寫 mount(且 wire 已凍結——先走 spec,見 §13)。
    - **DTO 向後相容**:對外 DTO 加欄一律 `optional`(不破既有 client);要破壞相容**先問 Seth**(§8 同源)。

13. **land / verify 紀律(AI-friendly:宣告前先坐實)**:
    - **verify-at-source > 自報**:任何「做完了 / 能跑 / 現況是 X」的 claim,回**最源頭**坐實(git / curl / CI 實際輸出),不憑記憶或 exit code。CI 綠的判準是讀到字面 `[ci] all green`,**不是 exit 0**。⚠️ 連自跑 CI 也可能被 **working tree(含未 commit 的 fix)**騙——land 前確認 **working tree == commit**(`git status` clean),驗的是 commit 不是 tree。
    - **recon 一律基於 `origin/main`**:此 repo 的 `local main` 是條**孤兒 old-flat-layout 歷史**(與 origin/main 無共同祖先),讀它 = 全錯 layout。verify-at-source 的 source = `origin/main`。
    - **wire 已凍結(M1)**:動 wire(HTTP OpenAPI 面或 MCP tool 面)= **先改 `spec/*.json`(spec/openapi.json / spec/mcp-catalog.json)+ owner 過目,再動碼**;ci wire-freeze gate(step 1g gen-ocapi drift:committed `ocapi_gen.go` 必須從凍結 spec byte-identical 重生 + step 4 FE schema drift;MCP 描述子由 ocserverd 直服凍結 `spec/mcp-catalog.json`,by construction 不漂)會擋任何未過 spec 的漂移;行為面由 `conformance/run.sh --target go` 收官。
    - **land pipeline**:off `origin/main` 開分支 → 隔離 worktree、標死 scope → 親驗全 diff(無 scope-creep)+ 親讀關鍵邏輯碼 → `bash bin/ci.sh` 讀 `[ci] all green` → FF fast-forward push(`git push origin <b>:main`)→ 雙源坐實(`git ls-remote` + `gh api .../commits/main --jq .sha`)→ autodeploy 盯 `/api/version` git_sha(公開免 auth;version 欄恆 0.0.0,對賬看 git_sha)→ 收退路(`worktree remove` + `branch -D`)。
    - **改 Go → rebuild + commit prebuilt binary**(CI parity gate 抓 committed ≠ 源)。
    - **§7 push 前審完整 manifest**:逐檔必要性,別把第一道審查讓給 gate。

14. **caller 身分來自 auth、非參數;intent-per-tool**(owner 定案 2026-07-10;對齊 §1「乾淨身分、不借權」;全 spec 見 `docs/design/caller-identity-convention.md`):
    - **caller 身分永遠取自 verified token `sub`(`current_actor`),絕不當請求 / tool 參數傳**。`member_id` / `agent_id` 參數**只**當**操作對象(target)**、永不是「我是誰」。
    - **自身操作(self-ops)不帶身分參數**——server 從 token 讀 caller(如 `report_waking` / `report_stopping` / `report_stopped`)。
    - **控制別的 member** 才帶 `member_id` target,且需 **admin capability**(owner-scope OR role `assistant`,如 mira——`server/ocserverd/authz.go` 的 `admin_agent` class,路由表 `requires="admin_agent"` 宣告);非 admin agent 只能操作自己。單一 resolver + 表上宣告,別每個 handler 各判。
    - **intent-per-tool**:每個 intent 是**自己一個 MCP tool**、只帶該 intent 需要的參數,**不**做「一個肥 tool + mode/phase discriminator」。

---

## 域地圖 — repo map(AI 定位用)

top-level:
- **`server/`** — Go server daemon:`ocserverd/`(**production server**:REST + SSE + MCP + reconcile,goose migrations,SPA go:embed)。folder = module = binary 同名;prebuilt 在 `bin/ocserverd`(勿與 bash installer `bin/ocserver` 混淆)。詳見 `server/CLAUDE.md`。
- **`cli/`** — Go 自更新 binary:`ocagent/`(Plane A:agent-side SSE listener)· `ocwarden/`(Plane B:per-machine warden executor)。folder = module = binary 同名。詳見 `cli/CLAUDE.md`。
- **`frontend/`** — React18 + Vite5 + TS5 SPA。seam 分層 wire→mappers→types→adapter→mock→http→hooks→component。詳見 `frontend/CLAUDE.md`。
- **`conformance/`** — 語言無關黑箱套件(HTTP-only,743 tests):wire 行為的回歸權威。詳見 `conformance/CLAUDE.md`。
- **`e2e_test/`** — Playwright 端到端(隔離 port,**絕不碰 prod**)。詳見 `e2e_test/CLAUDE.md`。
- **其他**:`spec/`(凍結 wire 契約 SSOT)· `seeds/`(語言中立 seed .md 資產,ocserverd runtime 磁碟優先直讀、單檔部署走 go:embed fallback)· `bin/`(`ci.sh` gate + 部署 / 安裝腳本)· `docs/`(架構文件)· `CLAUDE.md`(本檔,repo-wide 憲章 + 約定)· `.githooks/`(pre-commit 鏡像 CI gate)。

歷史:原 Python backend(FastAPI)已於 2026-07 退役移除;**永久回滾錨點 = git tag `py-final`**(最後一個含完整 Python 實作的 commit)。
