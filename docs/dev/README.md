# 開發指南

一般使用者請看 repo 根的 [README.md](../../README.md)；這裡是給改 code 的人。repo-wide 憲章與 land 紀律的權威在根目錄 [CLAUDE.md](../../CLAUDE.md)，各域（`server/` `cli/` `frontend/` `conformance/` `e2e_test/`）另有自己的 `CLAUDE.md`，本文不重複，只給地圖與跑法。

## 技術棧

| 面向 | 技術 |
| --- | --- |
| server | Go（`ocserverd`：REST + SSE + MCP + reconcile，goose migration，SPA 以 go:embed 內嵌，SQLite） |
| frontend | React / TypeScript（Vite） |
| cli | Go —— `ocwarden`（執行手）、`ocagent`（agent runtime） |

（歷史：原 Python backend（FastAPI + alembic）已退役移除；**永久回滾錨點 = git tag `py-final`**。）

## Repo 結構

```
server/       Go server daemon：ocserverd（route 表 / handlers / SSE hub / goose migrations）
frontend/     React/TS web UI（Vite）；build 產物由 go:embed 進 ocserverd
cli/          Go 模組：ocwarden（push-executor）、ocagent（agent runtime）
spec/         凍結的 wire 契約（openapi.json / mcp-catalog.json）——動 wire 先改 spec
seeds/        語言中立 seed .md 資產（boot context；ocserverd runtime 直讀）
conformance/  語言無關黑箱套件：server wire 行為的可執行定義（HTTP-only 回歸權威）
e2e_test/     Playwright 端到端（隔離 port，絕不碰 prod）
bin/          維運指令：ocserver / ocwarden / serve / migrate / build / ci.sh …
docs/         設計文件
oc.toml.example  server 設定範本
```

runtime 落點統一在 `~/.officraft/`：`server/`（canonical 安裝）、`warden/`（token / 設定）、`agents/`（各 agent 工作區）。

## 怎麼跑

```bash
# Go server
cd server/ocserverd && go build && go test ./...
bash bin/build           # 部署 binary：npm webdist + go build → .deploy/ocserverd（SPA 內嵌）

# frontend
cd frontend && npm install && npm run dev

# conformance（語言無關黑箱：wire 行為回歸權威；隔離 :8795）
conformance/run.sh --target go

# e2e（Playwright，隔離 :8791，絕不碰 prod）
cd e2e_test && bash run_all.sh
```

## CI

```bash
bin/ci.sh          # 讀到 [ci] all green 才算過（不是 exit 0）
```

CI 跑在本地（不付 GitHub Actions），從第一個非零步驟就 fail-fast；push 前請自己跑到綠。gate 內容：go gate / 黑箱 lint / gitleaks / FE typecheck+drift。

改 Go 之後記得 rebuild + commit 對應的 prebuilt binary（`bin/ocagent` / `bin/ocwarden` / `bin/ocserverd`）—— CI parity gate 會抓 committed ≠ 源。

## wire freeze

wire（HTTP OpenAPI 面、MCP tool 面）已凍結：**動 wire 一律 spec 先行**——先改 `spec/openapi.json` / `spec/mcp-catalog.json`（+ owner 過目），再 `bash bin/gen-ocapi` 重生、動碼。CI 的 wire-freeze gate 擋任何未過 spec 的漂移；行為面由 `conformance/run.sh --target go` 收官。完整紀律見 [CLAUDE.md](../../CLAUDE.md) §13。

## 發佈簽章(穩定 codesign 身分,T-33d5)

macOS TCC 以 code-signing 身分記權限;Go 預設 adhoc 簽章每 build cdhash 都變,fleet 每次 self-update 都會被 TCC 重問權限。解法:**發佈機**持有一張長效 self-signed codesigning 憑證(CN 預設 `OffiCraft Code Signing`),發佈鏈產 artifact 時以穩定身分 + 穩定 identifier 簽署:

- `bin/build-bindist` → 簽 bindist 的 `ocwarden`(`com.officraft.ocwarden`)與 `ocagent`(`com.officraft.ocagent`)——即 ocserverd 內嵌、經 `/api/{warden,agent}/binary` 發給 fleet self-update 的 binary。
- `bin/build` → 簽 `.deploy/ocserverd`(`com.officraft.ocserverd`)——autodeploy / `bin/release` 打包(GitHub Release 出貨)的 artifact。
- 簽署 seam 是 `bin/codesign-artifact`:**keychain 有憑證才簽,沒有就警告照舊**(預設絕不擋 build/deploy);簽完 `codesign --verify --strict`,失敗保留原 binary。
- **發版走 `bin/build-release`,憑證不在就硬擋(T-da4b,owner 裁示 `rc-e43a3aae0912`)**:上面兩個簽署點**都不是發版專用**——`bin/build` 也跑在 autodeploy(prod 主機)、`bin/ocserver install`、和任何 dev Mac 手動 build;`bin/build-bindist` 更是**每次 `bin/ci.sh` 都跑**。所以 `OC_CODESIGN_REQUIRE=1` 沒有「發佈路徑」可以掛——掛進 `bin/build`/`bin/build-bindist` = 連沒憑證的 dev Mac 和 CI 一起擋死(**owner 明確沒選這個**)。`bin/build-release` 就是補上的那個點:**發版者跑它、不跑 `bin/build`**,它 export `OC_CODESIGN_REQUIRE=1` 再委派,三個簽署呼叫(bindist ocwarden/ocagent + `.deploy/ocserverd`)全部繼承 → 憑證不在 = `FAIL-IDENTITY-MISSING` exit 4,**沒有 artifact 可出貨**。`bin/build` 本身**未改、維持預設 off**。
  - 發版:`bash bin/release <tag>` → `gh release create <tag> dist/release/…`(t-dc68 起發佈走 GitHub Releases;`bin/build-release` 仍是「憑證不在就硬擋」的簽章入口)。
  - **代價(owner 已知並接受)**:憑證過期／keychain 沒解鎖／發佈機重灌 → **發版會停,而且不會自己好**,要人去跑 `bash bin/setup-codesign-cert` 重佈。這正是要的行為:發版大聲停掉 > fleet 靜默掉 TCC 授權。
  - **已知缺口(刻意未補)**:這是**選用的入口點,不是出貨閘**。`gh release create` 上傳什麼檔案、**完全不看簽章**,所以有人跑 `bin/build` 再拿那顆去發 release 是擋不住的。要堵死得在發佈前驗 artifact 簽章身分——**那是另一個 owner call**,不在 T-da4b 範圍。
  - **未決**:`bin/autodeploy`(prod 主機)也會經 `bin/build` → `build-bindist` 產**發給 fleet 的** ocwarden/ocagent,但它**維持不擋**(擋它 = prod deploy 停擺,那不是 owner 被問到的那題)。要不要一併 require,需要 owner 再裁一次。
- **憑證檢查是「先收集再比對」,不可改回 pipeline(T-da4b)**:`security find-identity | grep -Fq` 會在第一行命中就關 pipe,`security` 吃 SIGPIPE(141),`set -o pipefail` 讓整條 pipeline 取 141 → **憑證明明在,卻被判成不在,靜默降級出 adhoc**。務必把輸出整個收進變數再比對。`bin/setup-codesign-cert` 的同款檢查也已一併改掉。
- **哨兵與陽性訊號(T-da4b)**:憑證確認存在時會印 `identity CONFIRMED present in keychain` —— 只有失敗才叫的哨兵,沒叫時分不出「正常」還是「哨兵自己壞了」,所以好路徑也要留證。**檢查本身壞掉**(`security` 讀不到、輸出沒有 `N valid identities found` trailer)→ `FAIL-CHECK-BROKEN` **exit 3 硬擋**,絕不當成「憑證不在」降級。**`OC_CODESIGN_REQUIRE=1`** → 憑證不在時 `FAIL-IDENTITY-MISSING` **exit 4 硬擋**;預設 off,所以沒憑證的 dev/CI 機照常 build。
- **同類掃描:`pipefail` + 提前關 pipe 的消費者(T-da4b,已掃全 repo,結論=不動)**:全 repo 24 個帶 `set -o pipefail` 的 shell script 都掃過 `| grep -q` / `| head` / `| sed -n Np` 這種「讀夠就關 pipe」的組合。**除了已修的 `codesign-artifact` / `setup-codesign-cert`,其餘一律不改**,因為判準不是「構造在不在」,而是**誤判往哪個方向倒**:
  - **rc 根本沒被消費** → `| head -1 || true`(`bin/ocserver:103`、`conformance/run.sh:178`)。`|| true` 吃掉 141,無害。
  - **倒向假紅(誤報失敗)** → `e2e_test/a1_zombie_e2e.sh:506/510/512`(`sed`/`head`/`tail | grep -qE`)、`e2e_test/tests_guard/run.sh:106/111`、`e2e_test/setup.sh:108`。SIGPIPE → 141 → 測試紅／腳本中止。**吵,但不會騙人**,而且這些檔案 `bin/ci.sh` 只跑 tests_guard,其餘沒有活體證據可驗 —— 改了也證不了,是純 churn。
  - **倒向「看得見的 skip」** → `bin/tests/run.sh:368`(`openssl version | grep -q '^OpenSSL 3'` 守著一個 red control)。**這是唯一另一個「檢查可能不執行」的方向**,但 else 分支會**印出 `skip — red control needs OpenSSL 3.x`**,不是靜默消失;且 `openssl version` 一口氣寫 ~25 bytes 就退出,窗開不起來。
  - **`echo "$VAR" | grep -q` 一律低risk**:writer 是 builtin、字串遠小於 64KB pipe buffer,grep 收到 EOF 前 write 早已完成。
  - **通則(給後面的人):`pipefail` + 早關 pipe 只有在「141 會把某個 `if`/`if !` 翻成『壞事不存在』並讓流程靜默往下走」時才是地雷。** `codesign-artifact` 之所以是地雷,是因為它是唯一一個誤判會**靜默翻轉「出不出貨」**的點。倒向紅、倒向可見 skip、rc 被 `|| true` 吃掉的,都不是同一種病。
- **committed prebuilt(`bin/ocwarden` 等)永遠不簽**,維持素 `go build` 產物——CI parity gate 與任何 dev 機 rebuild 都不需要 keychain,repo/CI 完全不受影響;簽章只活在發佈 artifact 上。
- self-update 側**不驗簽**(self-signed 憑證在未信任機器上 verify 必非零,硬擋會 brick fleet),只在 swap 後 log 新 binary 的簽章身分(`cli/ocwarden/selfupdate.go` 的 signature observability)。

發佈機一次性佈署:`bash bin/setup-codesign-cert`(冪等;產憑證 → 匯入 login keychain → sudo 信任 codeSign policy → 預授權 codesign 用 key → smoke test)。注意:換新憑證 = 新 TCC 身分,fleet 會再被問一輪權限。簽章腳本的 hermetic 測試在 `bin/tests/run.sh`(CI step 0b)。

## 安裝器內部

`bin/ocserver install` 的逐步細節（canonical layout、oc.toml 渲染、launchd plists、health check、首設啟用碼 banner、env override `OC_SERVER_ROOT` / `OC_SERVE_PORT` / `OC_CLOUDFLARED_CONFIG`）都寫在 `bin/ocserver` 檔頭註解與各 step 註解裡，那份是權威；tunnel 一律不代 provision，config + tunnel id + cloudflared binary 三者齊全才會掛 tunnel job。
