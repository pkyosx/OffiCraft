# conformance/ — 語言無關黑箱 conformance 套件

進入 `conformance/` 時讀本檔；repo-wide 規則在根目錄 `CLAUDE.md`。本套件是 server wire 行為的可執行定義：測試只對 `OC_TARGET_URL` 發 HTTP，不 import server implementation。未來換 backend，行為等價仍以這套黑箱結果為準。

## 1. 黑箱與 source of truth

- `run.sh` 開跑前做 blackbox lint；`bin/ci.sh` 也做同一檢查並在隔離 ocserverd 上完整跑 conformance。不要把「平常不跑」或測試數量寫成文件規則；以 CI／`run.sh` 實際輸出為準。
- `routes_manifest.json` 是 committed、凍結的 route snapshot；`spec/openapi.json`、`spec/mcp-catalog.json` 與 manifest 是 wire-freeze 資產。改 route、auth、MCP surface 必須 spec-first、owner 過目、測試同批更新；沒有生成器可替代這個裁決。
- manifest 的 row set 由 Go 側的 `TestEveryServedRouteIsInThePermissionManifest`（T-61）釘回 `routes.go` 的 route table，兩個方向都會紅並指名。本目錄能自己抓到的是**非 `MCPExclude`** 的漏登記（`test_catalog_hash_algorithm` 拿伺服器的 `catalog_hash` 對帳）；**`MCPExclude` 那一族**（`mcp_tool: null`）**從本目錄內部**看不到 —— 但那不表示沒人守：Go 側的 `TestRouteTableCoversSpecSurface` 已經雙向釘住 route table ≡ `spec/openapi.json`，而 `test_openapi_covers_manifest` 是對稱相等 ⇒ 漏登記的 route 本來就會紅（實測：加一列 `MCPExclude` route，前者報 `route table row not in the spec (wire freeze)`）。
- manifest 與 OpenAPI operation 集合必須相等；每條 gated route 必須出現在 auth matrix 或有理由的 `SKIP`；每條 happy route 也必須被 happy 表或有理由的 skip 覆蓋。這些是集合檢查，不是手抄數量。

## 2. 執行與隔離

- 標準入口：`conformance/run.sh --target go`。Python target 已退役，不要另造回滾路徑。server 使用核心自動配埠（預設 port 0，讀回實際值）；需要重現時才明設 `OC_CONF_PORT`。
- DB、`oc.toml`、config 都在 mktemp/隔離目錄，透過 `OC_CONFIG` 注入；不讀寫 repo 根 config。prod guard 以現行 source 的 production ports/identity/residue 判斷，保護現行與退役 port，不能用硬編單一 port 取代。
- teardown 只處理本次捕獲的 listener PID，禁止模糊 process kill。conformance 只需要 `OC_TARGET_URL` 與 `OC_OWNER_PASSWORD`；其餘 fixture 身分由 HTTP login/hire/mint 建立。

## 3. auth、REST 與 error envelope

- auth matrix 是 table-driven：每條 route 對無 token、owner、admin_agent、warden、agent self/other 判 status。`requires` 由 manifest 推導 capability floor：`machine/warden < agent < admin_agent < owner`；低於 floor 的 cell 不手寫預期，統一 403；達標 cell 才寫 route 語意 status。deny-first 必須先 403，再決定 target 是否存在，避免 agent 從 admin route 得 404 洩漏。
- REST happy 面使用最低可行 caller，驗 spec status 與 response schema；`schema_check.py` 只做黑箱安全的 stdlib `$ref`/required/type/anyOf 子集。binary、install script、attachment blob、MCP JSON-RPC 等 non-JSON 面改用明確 check，不繞過 status 驗證。
- attachment `?sig=` 是只給 blob GET 的第三授權路徑：正確 sig 可匿名取同一檔 bytes，壞/空 sig 401，A 檔 sig 不可取 B 檔，也不可升格打任何 gated route；bearer 在場且無效時不能 fallback 到 sig。
- 全站錯誤 wire 只有 `{"error":{"code","message"}}`：body 恰兩鍵、code 在閉集、message 非空，不得回 legacy `detail`。代表性 400/401/403/404/405/409/422 來源都要由 HTTP 驗到；宣告 telemetry block 型別錯誤的 422 與未宣告 block/empty telemetry 的 400 不可混為一類。

## 4. MCP、SSE 與 lifecycle

- MCP JSON-RPC 的 parse/invalid request/unknown method/invalid params、notification、initialize、tools/list、tools/call、loopback Authorization 都以黑箱驗；`tools/list` 逐元素含順序等於凍結 catalog。`isError` 對應 HTTP status≥400，structuredContent 只在 JSON object 時存在。
- catalog hash 從 manifest 的非排除 route 以 `METHOD path` 排序、換行、SHA-256 前 16 hex 黑箱重算，對 `/api/version` 與 `/version` 一致。`MCPExclude` 不得以 token 或 caller 旁路。
- SSE client 用 stream/queue；每個等待先用 HTTP write 觸發事件，不空等 heartbeat。closed topic 集合每次由 `spec/sse.md` 的契約段 fail-loud 解析，再與 trigger 表對質；parser 找不到 heading 或解析零 topic 必須拒跑，不能 fallback 解析更寬的文本。directed bands 不屬 `Publish` closed set，交由各自測試。
- SSE 驗 headers、connected、frame envelope/seq/epoch、delete payload、嚴格發佈序、所有 closed topics、dual-SSE takeover 與 stop gate。對可回顯的 topic 要綁本次 write 的值，不能只驗「有某個舊 frame」；先 drain backlog 的 barrier 要等到靜默，不可用固定 sleep。
- lifecycle 驗 claim envelope、JWT/TTL、mint floor、boot fold 的 seed bytes/順序、空 block 跳過、overlay-wins、unknown role 與 uninstall intent。黑箱做不到的重啟、heartbeat timing、multi-owner 等要列 `DEGRADED` 和理由，不得 silent skip 或宣稱未測。

## 5. reply cards 與 tasks

- reply card 狀態機：開卡同時建立 chat link、只有 answer 能關 waiting、一次性 answer、re-answer 只對已回答卡；kind/select_mode/options（含每選項 ai_pick 與其數量上限）/summary 先驗；答覆側的索引清單先正規化（去重＋升冪）再驗範圍與單選卡的數量。pane 的 waiting/answered 排序、badge 與 SSE 回 agent 都是 wire。
- list 預設是輕量摘要：title/summary、status、decision digest、answer preview、attachment count；body/options/chat message id 需走 full card。`?view=full` 必須是同一 pane 的完整列，未知 view 400；`view` 不向 agent 的 tools/list 宣傳，client 不得假設第三種 shape。
- expire 是 answer 以外的終態出口；answered/expired 的再次操作、answer/PUT on expired 必須拒絕，expired delta 與 pane/count 需一致。現行 route floor 與 handler caller rule 以 manifest/source 為準：作者可處理自己的卡，owner/admin 不因此能改已回答卡；不要把舊 owner-only 文字抄回來。
- task lifecycle 驗 dedupe、required inputs、plan/step/gate/card binding、合法 transition、waiting_owner／waiting_external 原因、deps、task message、closeout、manual CRUD 與終態防呼。答卡只解除 hold，不替 agent 推進工作進度；replan 要保留已答卡節點為 superseded，並正確計 progress。
- `get_my_task` 已退役，不能在 tools/list、HTTP 或 seed 留殘影；以 `get_task`／`report_waking` 的陽性對照確認不是整個 self surface 消失。task routes 的數量以 manifest set coverage 讀回，不在文件或測試手抄。

## 6. 修改規則

- 新 route/欄位/permission/tool 先改 spec、manifest 或 catalog 的 authoritative source，再補 matrix、happy、MCP、SSE、error 與 lifecycle 覆蓋；新增 skip 必須附理由。不要以 server import、fixture 特判或固定數字代替黑箱證據。
- 先讀 `run.sh`、測試表、spec 與 guard，再改本檔。文件不保存票號、日期、mutant 紅數、過期 port/job/route 清單；需要知道現況時現場讀 executable source 與 CI 輸出。
