# seven_gate/ — 任務路徑關卡

進入 `e2e_test/seven_gate/` 時讀本檔；上層規則在 `e2e_test/CLAUDE.md` 與根目錄 `CLAUDE.md`。資料夾名是歷史遺跡：目前 `judge.py` 的 `STEPS` 有九格，但其中 `report_waking` 與 `step_done` 是 observation，不是 gate；不要在別的文件複製步驟清單。

## 1. 載體契約：只判 server 事實

- 這套關卡驗的是「全新 agent 能否沿開機說明走出一條真實任務路徑」，不是 actor 自述或 log 漂亮。`judge.py` 不和 agent 對話，只讀 `journal.ndjson`；`collect.py` 只做輪詢與 append，兩者分開讓純判定可在無 server 的 guard fixture 上測。
- 每次 run 使用 `OC_SEEDS_SRC` 重建候選 seeds、重新僱 member；不修改被追蹤的 `seeds/`，也不重用帶有舊知識的 member。`judge.py` 從 server 事實判定：chat/nonce、task creator、同一張 task 的 steps、reply card、closeout、peer reply、image answer。
- 所有 server 呼叫只能走 `lib/http.sh` 的 `sg_http`。每一通 method、path、HTTP status、response body 都進 `http.log`／`run.log`；裸 curl 或把 response 丟到 `/dev/null` 都是載體 bug。actor rc 只記錄，不是證據。
- `friction.md` 是問句唯一來源；run 完固定逐字追問兩題，`friction.txt` 只保存 agent 原文或「沒回答」，載體不代寫、不摘要、不評分。

## 2. 九格的判定界線

- `report_waking` 只輸出 `OBSERVED`：live 路徑中 warden 先接走 START 時，reconcile 自己會 stamp `waking_since`；這不是 agent 報到的證據。stub 沒 warden，不能用 stub 的不同來源替 live 路徑背書。
- `step_done` 只輸出 `OBSERVED`：server 只知道 agent 回報鍵被按下的時間，不知道兩次回報之間是否真的工作；沒有 agent-only、可被 collector 讀到的 durable fact，就不能猜一個秒數把它重新武裝成 gate。
- 其餘 gate 必須用 server 上的 caller/target 和同一張票判定。開票、plan、step、closeout 不可各挑一張碰巧符合的 task；多張票目前取 `created_ts` 最早者，但 judge 必須警告並點名所有票，因為 server 沒有「這輪票」的事實。
- resume/chat nonce 證明的是內容被讀到並帶回，不證明特定 tool 曾被呼叫。peer gate 要同時滿足 `from == agent`、`to == peer` 與帶回 peer nonce；沒有 peer 或圖片種植失敗時要報 `This is a HARNESS red, not an agent red`，仍然 FAIL，不可把載體錯誤算成 agent 綠。
- 圖片答案每輪重抽六位數，只存在 PNG 像素；不在正文、檔名、mime、task、plan、PNG text metadata、server 文字或 agent 可讀檔案。`scene.json` 只存 salt、hash、長度，judge 對 agent 訊息中的等長數字做 hash 比對。
- 圖片秘密、peer nonce、scene nonce 不得從 actor 傳進 live warden：spawn 前 scrub 整個 `OC_SG_*`／`SG_*` namespace，並用陽性對照證明 scrub 真的有抓到秘密。live agent 與載體同 user/host 的主動翻 repo 或暴力搜尋仍是明確未解的界線。
- `judge.py` 每格都印 evidence；observation 印 `OBSERVED`、`verdict.json.passed` 為 `null`，不能成為 RED。判定前要說明 gate 數，`all green` 只代表七道 gate 綠。無法產生 verdict 時要 fail-closed 並明說修 `judge.py`，不能借用相反的 evidence 指控 agent。

## 3. task/card/peer 的載體條件

- reply card 若由 active task 的 agent 開出，server 會把卡綁到一個 `in_progress` step 並進 `waiting_owner`；owner 端必須在背景只回答本 run agent 的卡，否則卡成功反而鎖住 task、後續 closeout 不可能成立。收尾用確切 PID 清 owner responder，不用模糊 kill。
- `actors/stub.sh` 是 REST actor，不是 agent；它只供判定讀取與 skip-case 的負向控制。`actors/live.sh` 才是實 agent：onboard machine、啟動真 `ocwarden run`、owner activate 帶 `machine_id`、spawn claude；它只做 owner 端交辦、給機器、回卡、friction，不代做 agent 的 task/plan/step/closeout。
- live actor 預設嚴格關閉，只在 `OC_SG_LIVE_AGENT=1` 才可能花真 API 額度；錯值不得啟動。真 live 路徑至今未執行過，不能把 stub 綠、相依項可解析或另一支 E2E 的結果宣稱成 live 通過。
- 不用 `task_system_e2e.sh` 當本載體的 live actor：它起的是 outsource worker、走另一套 namespaced 安裝／全站 reset，而本載體要 member token 與 `creator_id == agent`。可借用的只是呼叫記錄與磁碟產物驗收原語。

## 4. 時間、設定與花錢前檢查

- collector 收集窗必須嚴格涵蓋 actor 預算（machine + spawn + live + friction + card round-trip），關係由 `lib/window.sh` 的單一 defaults 推導；不要在 `run.sh`、actor 或 collector 再寫第二個等待常數。`collect.py --seconds` 必填、無 900 秒 default，`run.sh` 必須把 `sg_collect_seconds` 的變數傳入；關係不成立就拒跑。
- `OC_SG_RUNTIME`／`OC_SG_MODEL`／`OC_SG_EFFORT` 在 activate 前 PATCH member，然後一定 GET 讀回；不一致或 server 拒絕就不花錢。讀回值寫進 `scene.json`，記錄實際 run 而非只記意圖。
- `live.sh` 在 activate／spawn／開始計費前做 PRE-SPEND PREFLIGHT：展開後半段所有等待變數、解析 friction，並以 `lib/varcheck.py` 掃 seven_gate 下每一支 `.sh` 的未綁定 `$VAR`。每支 shell 都要能在原廠 `/bin/bash`、`PATH=/usr/bin:/bin` 靜態 parse；`find` walk 用 `-L`，避免 symlink 後的危險檔逃過檢查。
- preflight 只能擋變數／語法與部分載體錯誤，不能證明 server 中途行為或 live agent 正確；完整 guard 要在花錢前以靜態 fixture 驗證，不能用一次真跑代替。

## 5. 產物、失敗歸因與 log

- 每次 run 一個不覆蓋的 `runs/<UTC stamp>/`；至少保留 `run.log`、`scene.json`、`journal.ndjson`、`collect.log`、`actor.log`、`http.log`、`verdict.json`、`rc`、`outer.rc`、`outer.status`、`friction.txt`、圖片與 live 的 `warden.log`。答案明文不得進 `scene.json`。
- judge 每格都報 `PASS`／`FAIL` 或 `OBSERVED`，最後一行報最先失敗的 gate；後續格仍要印，讓人分辨自己的前置沒發生還是自己失敗。退出碼與 reason 必須可從 `verdict.json`、`outer.rc`、`outer.status` 對回；等待者盯 `outer.rc`，因為 judge 的 `rc` 可能尚未存在。
- harness planting、判定程式無 verdict、agent 未完成三種紅要分開說。載體沒有種 peer/image/nonce 時修 run/plant，不要照著 evidence 去修 agent；`judge.py` fail-closed marker 只說修 judge，不等於把該格判綠。

## 6. carrier 與 owned-kill 安全

- `run.sh` 啟動即 re-exec 到新 session；前景呼叫的 rc 合約不變，由原 pid 等 detached child。EXIT/signal trap 以及 watchdog 必須寫 `outer.rc`／`outer.status`；SIGKILL 由 watchdog 補 `137/vanished`。這保證的是呼叫者 group 被殺時可見，不保證 warden 依 PPID tree 收 member 時載體仍活著；relocate/warden kill 的限制要誠實保留。
- live actor 只殺本 run 自己建立且寫入 ledger 的 session/PID；沒有 ledger 就殺零個。每輪使用隨機 non-canonical `OC_NAMESPACE` 與對應 tmux socket，明確拒絕 canonical／空 socket；socket 隔離與 ownership ledger 兩層都不能省。
- `e2e_test/seven_gate/` 下所有 `.sh` 都在禁令射程內：不得把危險 process/file/port 操作放進會被 `tests_guard` 執行的原檔；mutant 與陽性對照只能寫 disposable copy。掃描要以目錄查詢加檔數下限，不用會過期的檔名清單；註解中的禁令說明不應讓掃描自我觸發。

## 7. 明確未做到與修改規則

- 目前沒有真 agent 走完本關卡；live onboarding、warden、第二次帶 machine id 的 activate、真 claude、friction 皆未被端到端執行。stub 只證明載體能讀 server facts；它不證明新 agent 讀開機說明後會自發選擇這條路。
- `report_waking`／`step_done` 降級的理由與代價都不可省略：今天可能讓「根本沒報到」的 run 不被任何 gate 擋下，這是 owner 裁定的觀察界線，不是待偷偷補回的測試漏洞。
- 新增或改 shell、judge、collector、actor 時，先讀 source 與相關 guard，再更新本檔；補 guard 要測正向、負向與 scope 漏洞。不要把票號、日期、mutant 紅數、案例編號或可變檔案清單寫回這裡。
