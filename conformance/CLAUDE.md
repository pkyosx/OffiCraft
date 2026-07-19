# conformance/ — 語言無關黑箱 conformance 套件

進入 `conformance/` 時 nested-load。repo-wide 憲章見 root `CLAUDE.md`;本檔記 conformance 專屬。

## 定位(為什麼有這個套件)

這是 server wire 行為的**可執行定義**——誕生於 golang-backend-migration 時期(當時同一套測試對 Python 與 Go 兩個 target 一視同仁,是遷移驗收權威;Go 411 全綠後 Python 已退役,回滾錨點 = git tag `py-final`)。現在它是 **Go server 的永久黑箱回歸守衛**:純 HTTP 進出,測試只透過 `OC_TARGET_URL` 指到的 base URL 打 HTTP,語言無關——未來任何 rewrite 仍以本套件全綠為行為等價的判準。

兩套測試**互補、不合併**:

| 套件 | 性質 | 角色 |
|---|---|---|
| `e2e_test/`(Playwright) | FE 驅動煙霧、造真素材 | 測前後端整合 |
| `conformance/`(本套件) | 語言無關黑箱、HTTP-only | wire 行為的回歸權威(743 tests) |

## 黑箱鐵律(機械 enforce)

**conformance 測試碼絕不 import 任何 server 實作模組**(禁字面沿用退役 Python 的 package 名:`service` / `dal` / `domain` / `plumbing` / `backend`——規則不因實作退役而鬆)。一旦 import,它就不再是語言無關的行為定義。這條由兩道機械 gate 守著:

1. `run.sh` 開跑前的 grep gate(黑箱 lint);
2. `bin/ci.sh` 的 conformance blackbox-lint 段(只 lint、不起 server——完整跑法走本目錄的 `run.sh` 入口,太重不掛進 ci.sh)。

route 表(`routes_manifest.json`)是**凍結的 committed 快照**(當年由退役 Python 實作的 ROUTE_SPECS 機械抽出——tag `py-final`),與 `spec/*.json` 同屬 wire-freeze 資產:要改一律 spec-first、過 owner。它沒有再生成器;守漂移的是套件本身——`test_openapi_covers_manifest` 釘 manifest ≡ spec operations,auth 矩陣逐列釘 requires 對 live 行為,manifest 漂了 run 直接紅。

## 怎麼跑

```bash
conformance/run.sh --target go    # 起隔離 ocserverd(:8795、臨時空 SQLite)→ pytest → teardown
```

(`--target py` 已隨 Python backend 退役;歷史回滾 = git tag `py-final`。)

隔離紀律(同 e2e_test 鐵律):**絕不碰 prod**(`:8770` officraft live / `:8766` vibe);e2e 用 `:8791`,conformance 用 `:8795`,不互踩。DB 是 mktemp 下的一次性 SQLite(migrate 到 head),oc.toml 也是臨時生成(`OC_CONFIG` 注入),不動 repo 根的 oc.toml。teardown 只 kill 捕獲到的 listener PID,絕不 pkill 亂槍。

測試側只吃兩個 env(由 run.sh 注入;打別的 target 時自行提供):
- `OC_TARGET_URL` — 受測 server 的 base URL;
- `OC_OWNER_PASSWORD` — owner login 密碼(fixture 走 `POST /api/login` 換 token,再走 HTTP hire/mint 造 agent 身分)。

## auth 矩陣(第一批)

`test_auth_matrix.py`:表驅動,每條 gated 路由 × {無 token、owner、admin_agent、warden、agent 本人、agent 他人} 斷言 status。期望值**照 manifest `requires` 語意機械推導**(RBAC 的行為定義):`requires` 是路由宣告的最低 principal class,階梯 machine/warden(0) < agent(1) < admin_agent(2) < owner(3);低於門檻的 cell 一律推導為 403(表裡**禁止手寫** below-floor cell,`Route.expect` derive),達標 cell 才寫路由語意 status(預設 200)。manifest 帶 `requires` 欄,`test_matrix_requires_match_manifest` 把矩陣每列的 requires 釘死到 manifest 宣告——server 改 requires,矩陣先紅。hire 提權洞回歸(agent/warden 帶 kind/role_key → 403;admin 可)與 deny-first(admin 路由打不存在 target,agent 拿 403 非 404,訊息 "principal not permitted")各有專測。

覆蓋有 teeth:`test_manifest_fully_covered` 斷言 manifest 的 84 條 gated 路由 = 矩陣覆蓋 ∪ 明示 SKIP 表(附理由),新路由進 route 表而沒進矩陣 → run.sh 直接紅。降級測法(如 bootstrap-here 只打不存在的 machine_id,避免真的在 host 上裝 warden)在 `DEGRADED` 表誠實列明,不 silent skip。

## REST happy-path 面(第二批)

`test_rest_happy.py`:矩陣釘「誰能打」,這裡釘「打過門檻後拿回什麼」。每條 manifest 路由以**最低可行身分**(owner / self-report 用 scratch agent / public 匿名)發一發 happy request,斷言 spec 成功 status + response body 對 `spec/openapi.json` 宣告 schema 驗形(`schema_check.py`:純 stdlib 的 $ref/required/type/anyOf 子集 validator,黑箱安全)。schema 表達不了的語意用逐列 `check` hook 釘(echo、bootstrap preview token=null / spawn token 可登入、MCP tools/list ≡ `spec/mcp-catalog.json`、attachment bytes round-trip、install.sh token templating)。非 JSON 列(binaries / install.sh / attachment blob / MCP JSON-RPC)標 `nonjson` 附理由,status 照斷、以 check 代 schema。

覆蓋 teeth 同矩陣:`test_happy_covers_manifest`(HAPPY ∪ SKIPPED_HAPPY = manifest,skip 必附理由);另 `test_openapi_covers_manifest` 把 manifest 路由集合釘死到凍結的 `spec/openapi.json` operation 集合——server 加路由而 spec 沒 freeze(或反向)直接紅。

附件分享連結(`?sig=` 第三授權路徑,只在 blob GET 一條路由上)的語意釘在本檔的 `test_share_sig_*` 系列:無憑證 + 正確 sig → 200 bytes round-trip;壞 sig / 空 sig → 401;A 檔 sig 打 B 檔 → 401(單檔授權);sig 打其他任何 gated 路由 → 401(sig 永不升格為 token);bearer 憑證在場(header 或 `?token=`)時壞 token 照 401、不 fallback 到 sig(順位 header → token → sig)。

## MCP / SSE / lifecycle 面(第三批)

`test_mcp.py`:spec/mcp.md 的 MUST 逐條釘。JSON-RPC 錯誤碼閉集(-32700 壞 JSON、-32600 batch 陣列/非物件 body/method 非字串、-32601 未知 method、-32602 params 各違規**含未知 tool 名**)、id:null 語意、notification → 202(觀測 wire 是 `null` body 非空 body,spec 偏差已回報、測試釘「無 envelope」)、initialize echo/default、tools/list ≡ 凍結快照**逐元素含順序**、tools/call 拆參(path/query/body、None 丟棄、缺 path param 自然 404)、isError ≡ status≥400(4xx 是 result 不是 RPC error)、structuredContent iff JSON object、loopback Authorization 轉發(agent 打 admin-floor tool → 403 envelope)。**catalog_hash 演算法黑箱重算**:manifest 帶 `mcp_tool` 欄(凍結快照的一部分,null=mcp_exclude),對非排除列做 `"{METHOD} {path}"` sort + \n-join + SHA-256 前 16 hex,對比 `/api/version` 與 `/version` 兩探針。

`test_sse.py` + `sse_client.py`:活 SSE client(httpx stream + 背景 thread + queue,純黑箱)。timeout 紀律:**每個等待都先用 HTTP write 觸發事件**(delta 在一個 0.25s poll 內到),絕不空等 15s heartbeat;negative 等待 1–1.5s 限量使用。覆蓋:§1 gate/headers/`: connected`、§2 delta frame 形狀(id==seq==epoch、五鍵 envelope、remove ⇒ deleted+payload:null)、§2.1 seq 嚴格遞增=發佈序、§3 **9 topic 閉集全數觸發觀測**(含 monitoring 與 M2 新增的 reply_card;op patch/remove/signal)、§5 presence 純連線投影(offline→online→offline)、§5.2 first-connect 清 waking / last-disconnect bank cost **恰一次**(pop 防 double-bank)、§5.1 dual-SSE takeover(第二條連線 200 接手、舊 stream 被 server 終結、presence 不閃斷)+ 防震盪 throttle 超額 409 conflict envelope(pre-stream)+ owner 連線豁免、§6 context-high warn 定向帶(無 id: 行、只到 agent 連線、stale-pct guard)、§7 warden-command band(真 onboard warden token 收 START frame、args 全形狀、member_token 可登入、絕不漏到 owner fan-out)。

test_sse.py 另含 **zombie stop gate 系列**(殭屍防線 B):stop 生效中(deactivate 後)重連 → pre-stream 409 conflict envelope 且不投影 online、activate 原子解 gate、剛 hire 未 activate 照常連(下界)、dismissed member 拒連、warden 豁免 desired-offline 臂。

`test_lifecycle.py`:spec/lifecycle.md。§1.1 claim envelope(header 恰 HS256/JWT、machine_id 缺席=整鍵省略)、偽造 token 硬化(篡改簽章/alg:none/過期 payload → 401)、§1.3 mint TTL(login 86400、mint ttl_days 與 400 天 cap、onboard 90 天、bootstrap 帶 desired_machine_id claim)、§2 boot fold **byte-for-byte 重建**(seed 檔為語言中立資產直讀;觀測 wire 對 seed 做 `{OWNER_ID}`→`owner` 代換,spec 未載已回報)+ 空 user block 整段跳過 + lessons overlay-wins + unknown role 404、in-memory #2 observed position(agents_on_machine 擋 machine uninstall **與 delete** 409——實際在線判準:斷線即放行,離線綁定不擋)、§4.3 uninstall 一次性意圖(owner 定案 2026-07-11:warden 斷線 → 意圖消化歸零 offline;boot-command 重取等重裝路徑先歸零殘留意圖再裝)。**DEGRADED 誠實表**(附理由、有 teeth):restart honest-empty / seq rollback(黑箱不可重啟 target)、15s heartbeat(單次觀測費 15s)、多 owner fan-out(單租戶無反例)、reconcile timers(分鐘級)、secret tier、boot_ts 直接值(由 stale-guard 間接)、queue cap/at-most-once 掉幀。

## 回覆卡面(M2 reply-card B1)

`test_reply_cards.py`:等我回覆卡的狀態機與 pane 語意。開卡=waiting 且同時騎聊天流(chat 訊息 meta.reply_card_id ↔ 卡 chat_message_id 雙向連結)、**只有回覆能關卡**(選項/打字反問/附件皆算;無 close/skip 路由=by construction,405/404 探測)、一張卡一次性(二次 POST answer 409)、重新決定僅限已回應卡(PUT on waiting 409;換答案不變態、不回徽章)、開卡驗證(kind 閉集、options 1..4 非空白、summary 非空)、待回覆 pane 等最久在前、近期已回覆 pane 新在前、**list 走輕量列(T-3f31 owner 定案:卡只需要 title+決策)**——每列只有 summary+狀態+決策 digest(option_idx+選項原文、answer text 截 200 rune 預覽、附件為 COUNT),**不帶 body/options 全文/chat_message_id**(全卡走 get_reply_card;`test_list_rows_are_light_title_plus_decision_only`),`?limit=N` 在 pane 排序後截前 N(0/負數=全量;`test_list_limit_caps_rows_after_pane_ordering`)、徽章只算 waiting、**答案帶脈絡回 agent**(agent 自己的 SSE 連線收 reply_card delta → get_reply_card 拿回 summary/選項原文/owner 答案+附件 round-trip)。DEGRADED(誠實):近期已回覆的 24h 過期是時間性、黑箱不可觀測——窗界值由 server 單元測試(api_replycards_test.go)釘,本檔釘窗的成員資格+排序。routes 面:6 條新路由進 auth 矩陣(answer 兩條 requires=owner)與 happy 表。**已過期終態(T-1aa4)**:`POST /api/reply-cards/{card_id}/expire`(requires=owner、MCPExclude)= 唯一另一個出口——不是回答(200 帶 expired_ts、無 answer digest)、終態(double-expire/answer/PUT on expired 全 409、expire on answered 409)、徽章 -1、`?status=expired` pane(24h keyed expired_ts)+ count.expired、gate 卡 expire → step/task 退回 in_progress(答卡 resume 的 expire 攣生)、**orphan 卡(task 終態、answer 409)唯一出口 = expire 且關閉任務不動**、expired delta 走同一 reply_card downlink(payload status="expired")。矩陣/happy 各 +1 列(positive face 每次燒一張 fresh waiting 卡——expire 一打即終態)。

## 任務面(M3 任務批 Phase 1)

`test_tasks.py`:M3 任務系統的狀態機、gate↔回覆卡打通與手冊生命週期。全迴圈(建 ad-hoc 任務→in_progress→交計劃(含 gate 預告 = is_gate ∧ 無卡)→推節點→open_gate 生效(真 M2 卡:騎聊天流、待回覆 pane 帶 task 物件、step 綁 reply_card_id)→owner 走**既有** answer 路由答卡→**server 自動把 step 與任務還原 in_progress(T-68b7:waiting_owner 完全由卡片生命週期夾住;僅解除 hold、絕不代推節點——H4「不代推工作進度」核心保留,其「答卡後停在 waiting」語意由 T-68b7 取代)**→agent 自行推進→done(closed_ts、progress 3/3、徽章 -1))、去重(開放同鍵 → 200 + deduped:true 回舊任務;異鍵造新;終態撞鍵重開造新,H1/H2;缺必填 input → 400)、終態防呼(status/plan/deps/step/gate/二次 terminate 全 409;steps 原樣凍結)、狀態機 wire 守衛(waiting_owner/terminated 不可直設 409、未知值 400、非法轉換 409、waiting_external 必帶原因 422 且離開清空)、deps 純標示(自指/未知 422、status 不動、全量替換)、任務訊息框(owner→executor 普通 chat + meta task 脈絡、空訊息 400)、手冊 CRUD(部分編輯、agent learnings whole-doc replace、撞鍵 409、刪除防呼:有開放任務 409 → 關閉後可刪 → 讀 404)、get_my_task 成員 404 / warden 403(agent 級 floor 首用)、**T-1aea replan 保留已答卡節點**(答卡後 replan → 該節點凍結 `superseded` 終態(原位、finished_ts、卡 join 留存)、waiting 卡節點照舊被替換、superseded 不計 progress、報離/重綁 gate 皆 409)。**DEGRADED(誠實)**:外包 worker 黑箱不可鑄(Phase 2 排程器是唯一鑄造 seam)——領工正面(assigned→active + 手冊快照)與解僱由 server 單元測試(api_tasks_test.go)釘。routes 面:21 條任務路由進 auth 矩陣(agent 級首用;執行者守衛 = agent_other 403 override)與 happy 表。**§6.2/§6.3 批**:close-out 回報(`POST /api/tasks/{id}/closeout`——開放中 409、done/terminated 皆可報、冪等重報 200 no-op、執行者守衛、`closeout_reported` 旗標)與 resume-summary 任務段(caller 為 executor 的非終態任務;**T-3f31 輕量列**:每張只有 task_no/標題/type/狀態/優先權/當前節點 id+**名稱**/進度/`detail_chars`(省略掉的計畫文字 rune 數),**不帶 steps/DoD 全文**(細節 get_task 拉);另帶 `overview` size/概要塊(chat_count、tasks_returned/tasks_open_total(cap 外真實總數)/tasks_detail_chars、caller 自己的 cards_waiting/cards_answered_recent 隨答卡即時 fold)——peek-then-decide;identity-lock、上限 5 張依 updated_ts 新→舊)一併釘在本檔。

## 錯誤 envelope 面(第二批)

`test_error_envelope.py`:全站錯誤唯一 wire 形狀 `{"error":{code,message}}`(docs/design/api-error-envelope.md)。代表性錯誤路徑逐類發射:401(無 token / 壞 token / 錯密碼)、403(below-floor 打 owner 路由)、404(語意 unknown member + 框架 unknown path 兩源)、405(框架 wrong method)、409(refocus offline)、400(handler 輸入拒絕:壞 base64 附件、非數字 context_pct、全空 telemetry——lifecycle.md §3 釘 flat 400 非 422)、422(RequestValidationError 源:login 缺欄、install.sh 缺 token)。斷言:body 恰好 `{error:{code,message}}` 兩鍵、code 符合閉集 `CODE_BY_STATUS`(側鏡重宣告、不 import)、message 非空、無 legacy `{"detail":…}` 殘留。
