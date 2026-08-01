# 開發指南

一般使用者請看 [docs/guide/](../guide/)（產品說明，也是控制台主導覽「使用說明」分頁的來源）；這裡是給改 code 的人。repo-wide 憲章與 land 紀律的權威在根目錄 [CLAUDE.md](../../CLAUDE.md)，各域（`server/` `cli/` `frontend/` `conformance/` `e2e_test/`）另有自己的 `CLAUDE.md`，本文不重複，只給地圖與跑法。

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

# conformance（語言無關黑箱：wire 行為回歸權威；隔離、核心自動配埠）
conformance/run.sh --target go

# e2e（Playwright，隔離 :8791，絕不碰 prod）
cd e2e_test && bash run_all.sh
```

## CI

```bash
bin/ci.sh          # 綠的判準是「rc == 0 且整份輸出的最後一行精確為 [ci] all green」，兩個條件都要
```

判準為什麼是 **AND**（T-d3e3）：兩半各自都不夠。

- **寬鬆 grep 完全無效**：step 0 的 `e2e_test/tests_guard` 第一步就印自己的 `all green`，所以**任何**中途爆掉的 log 裡都已經含有那個子字串。
- **只看最後一行會被 dispatch 的 lane 偽造**：ci.sh 不是這份 log 的唯一寫入者。一個被 dispatch 的 lane 只要 `echo "[ci] all green"; exit 1`，ci.sh 的 `set -e` 就在那裡中止，偽造的權威剛好留在最後一行——這個假綠是真的被做出來過的。
- **只看 rc 也不夠**：這個 repo 有前例，`bin/common.sh` 的 `set -e` 打敗了 `run_all.sh` 刻意的 rc 捕獲，讓失敗訊號默默消失。舊文所以寫「判準是 marker、**不是** exit 0」——那句話講的是 **rc 不足以單獨判綠**，不是「rc 不該被檢查」。要求兩者同時成立比任一半都嚴格，與原意相容。

`bin/tests/ci-success-marker.sh` 是這條規則的可執行形式：它同時掃描 **ci.sh 以及每一個被 dispatch 的 lane 腳本**，要求除了 ci.sh 之外沒有任何 shell 腳本「有能力」印出這個權威字串。

CI 跑在本地、`bin/ci.sh` 是 land 權威，從第一個非零步驟就 fail-fast；push 前請自己跑到綠。gate 內容：go gate / 黑箱 lint / gitleaks / FE typecheck+drift。

（舊文寫「不付 GitHub Actions」——repo 轉 PUBLIC 後那個理由已不成立，公開 repo 用標準 runner 是免費的。真正的理由是這份 gate 裡有大量 host-shaped 與「重生後逐位元組比對」的步驟，我們不想把那些的權威搬到雲上。）

**PR 上的雲端 check（`.github/workflows/ci.yml`）**：`pull_request` 觸發，跑「雲端跑得動的全部」——**單元測試**（`e2e_test` 的 hermetic isolation-guard、Go 各模組的格式／靜態／編譯／測試、FE typecheck/vitest）、**hygiene**（tracked-file path denylist）、**一致性檢查**（gen-ocapi / FE schema.ts / 主題色票 / 訊息鍵 / 字型白名單的 regenerate-and-diff 漂移閘 + 兩個 token lint）、**黑箱行為**（完整 conformance 套件，起真 ocserverd 綁隔離 port）。它是在乾淨 Linux 機器上的 cross-check，**不是 land 權威**——`bin/ci.sh` 才是。

⚠️ 子集的定義只有一份、寫在 `bin/ci-cloud.sh`（repo 內的 bash）；workflow YAML 只負責裝釘好版本的 toolchain 然後呼叫它，**裡面沒有、也不准有第二份模組清單或閘門清單**——要加請加進 `bin/ci-cloud.sh`。

⚠️ **workflow 裡的 go / node 版本釘選是承重的、不是衛生習慣**：一致性檢查斷言的是「重生的位元組與 committed 完全相同」，runner 的 toolchain 一旦浮動超前開發機，這一類就會在「碼完全沒問題」的情況下變紅。

**留在本機的**：`bin/tests/run.sh`（Linux 上目前有 16 條 assertion 失敗；根因是 BSD/GNU `mktemp -t` 語意、SIGPIPE 與 macOS 形狀的 `install.sh` fixture，尚未移植）、Playwright CT（真瀏覽器版面守衛；macOS↔Linux 的字型與光柵化差異會讓紅燈的意思從「版面壞了」變成「runner 字型不同」）、gitleaks（內容級機密掃描）、`e2e_test` 的真機端到端測試（要真的 fleet host）。tracked-file path denylist 與 `e2e_test` 的 hermetic isolation-guard 已在雲端流程執行。整條雲端流程不用任何 secret，所以 fork PR 也能跑完整。

**Go 測試一律 `-count=1`（T-bedc）**：CI step 1e 是 `go test -count=1 ./...`，`-count=1` 是「不吃 go 的測試結果快取」，**不可省**。省掉的後果是實測過的——log 裡出現 `ok  ocwarden  (cached)`，那格綠燈認證的是一次**根本沒執行**的跑。兩個獨立理由：(a) 快取 key 只涵蓋 package 的**輸入**，不涵蓋測試真正碰的世界（port、時鐘、launchd、host fleet、staged embed assets 的**效果**），所以今天會紅的 package 照樣報 ok；(b) 它**結構性地藏 flake**——一個 suite 只在「第一個改到它輸入的 commit」上跑過一次，間歇性失敗於是被攤平到近乎零觀測機率，`[ci] all green` 變成在講快取而不是在講碼。可執行形式是 `bin/tests/go-test-nocache-guard.sh`（CI step 0b 派出）：它以**命令位置解析**（不是 substring grep——那會匹配到 ci.sh 與守衛自己的說明文字）掃全 repo 的 shell 腳本，任何 `go test` 呼叫點少了 `-count=1` 就紅。注意 `go build` / `go vet` 的快取**刻意不管**：那是對編譯本身做 content-addressed，命中等價於未命中；只有**測試結果**快取會宣稱「行為被觀察過」而其實沒有。

改 Go 後只需 fresh build 驗證；`bin/ocagent`、`bin/ocwarden`、`bin/ocserverd` 若出現都是 gitignored build artifact，**永不 commit**。CI 一律編譯 source；只有本機恰有 prebuilt 時才做 parity dryrun。部署 binary 由 `bin/release` / GitHub Release fresh build 產出。

**唯一的例外是 TCC 身分錨點 `dist/officraft/officraft`（owner 明確核可，T-5831）**：它是 launchd 的 responsible process，而 TCC 用 bytes 認身分，所以那份 bytes 本身就是要被審的東西。`.gitignore` 只放行 `dist/officraft/` 底下四個路徑，其餘 `dist/` 照舊全擋。它附兩份紀錄（`source.sha256` 與 `binary.sha256`），由 `bin/check-officraft-dist` 在 CI step 3 比對；重建方式與**為什麼 build 一定要帶 `-trimpath -buildvcs=false`** 寫在 `dist/officraft/BUILD.md`。

## wire freeze

wire（HTTP OpenAPI 面、MCP tool 面）已凍結：**動 wire 一律 spec 先行**——先改 `spec/openapi.json` / `spec/mcp-catalog.json`（+ owner 過目），再 `bash bin/gen-ocapi` 重生、動碼。CI 的 wire-freeze gate 擋任何未過 spec 的漂移；行為面由 `conformance/run.sh --target go` 收官。完整紀律見 [CLAUDE.md](../../CLAUDE.md) §13。

## 發版指令(T-588c)

發版只有兩條指令,`bin/release` 全包,**不再有「印一行 `gh release create` 給人貼」的半套形式**(舊的 `bash bin/release <tag>` 已移除,打它會拿到非零退出 + 正確替代指令):

```
bin/release publish --beta <tag> --target <sha> [--dry-run]
bin/release promote <tag>                       [--dry-run]
```

- `publish` 從 `<sha>` 切一個**丟棄式 detached staging worktree**(不是「當前 tree 乾淨就好」——bytes 來自你指名的那個 commit),在裡面 build、打包、**上傳前先驗 artifact**(tarball member list、三顆 binary 的 arm64 mach-o、從 `go version -m` 讀 ocserverd 真正被 link 進去的 `appVersion`/`buildSHA`、`shasum -c`),然後**一次** `gh release create --prerelease --target <sha>` 帶齊三個 asset(所以不存在「release 已建立但 asset 只上傳一半」的視窗)。
- `promote` 把**既有且已驗過**的 prerelease 翻成正式版,**不重 build**——大家測的 bytes 就是出貨的 bytes。翻完回讀,若 asset 集合在翻的過程中變了(有人偷偷重傳)那是**失敗**,不是警告。
- `--dry-run`:build + 驗完就停,印出它本來會跑的上傳指令,**什麼都不上傳**。彩排用這個。

### 回讀查證(publish 的第 7、8 步)

發完不靠人記得手動確認。`publish` 會**問 GitHub 它到底存了什麼**並逐項要求:每個預期 asset 都在且 `state=uploaded`、size 非零、沒有多餘 asset、`targetCommitish == <sha>`、`isDraft == false`、`isPrerelease == true`;然後 poll 線上站台的 `GET /api/version` 直到 `git_sha` 對得上 `<sha>`(prefix,至少 7 字元)、且 `GET /api/health` 答 ok。**任何一項不合就 exit 6 並指名是哪一項**:`[release] VERIFY-FAILED [asset-uploaded]: …`。這條的可執行守衛是 `bin/tests/release-guard.sh`(由 `bin/tests/run.sh` 派出,即 CI step 0b;PATH-shim 假 `gh` + 假 `curl`,完全不碰網路、不建任何 release、不連任何站台)。

**回讀的 payload 形狀是「量過的」,不是猜的**(2026-07-26,對 `pkyosx/OffiCraft` 的 `v0.5.38` 實測 `gh release view --json assets,isDraft,isPrerelease,targetCommitish`):

```
isDraft False | isPrerelease True | targetCommitish fb89a69aad8c
{'name': 'checksums.txt',                         'state': 'uploaded', 'size': 181}
{'name': 'install.sh',                            'state': 'uploaded', 'size': 70730}
{'name': 'officraft-v0.5.38-darwin-arm64.tar.gz', 'state': 'uploaded', 'size': 16842394}
```

也就是 asset 子欄位 `name`/`state`/`size` 確實存在、`state` 就是字串 `"uploaded"`、`size` 是非零整數——正好是回讀真正依賴的三件事。**為什麼要特別量**:同一張票裡,`verify_artifacts` 的架構檢查就是因為「猜 `file -b` 的輸出順序」而寫成永不可能命中的 pattern(`file` 實際輸出 `Mach-O 64-bit executable arm64`,架構在最後),導致每次 publish 都死在 `[artifact-arch]`。假設外部工具的輸出格式是同一類 bug,所以這裡改成量。要改形狀前**先重量一次**。

**第 8 步的語意:publish 不觸發升級,它只「觀察」升級發生**。站台是靠 owner 帳號上的 **auto_update** 自己去撿新 release 的,而 **prerelease 也算**:2026-07-26 實測,`v0.5.38`(`isPrerelease=true`)建立後約 **2–3 分鐘**站台自動升上去、`/api/version` 的 git_sha 回讀查證。預設等待預算 60 × 5s = 5 分鐘,約為實測延遲的兩倍。所以「發完等站台升上來」是**正確的流程期待,不是設計缺陷**;但若哪天 auto_update 被關掉,這一步就會**合理地**失敗,而失敗訊息會明講「只有這一項沒達成、asset 與 release 本身都對」,以免下一個人跑去查 artifact。

## 發佈簽章 —— 已整個移除(T-0398,owner 2026-07-31)

**這個 repo 不做 code signing,而且沒有任何開關可以打開它。** 原本的機制(`bin/codesign-artifact`、`bin/setup-codesign-cert`、`bin/build-release`、`bin/release publish --sign`、`OC_CODESIGN_*` 全套 env knob、`bin/tests/run.sh` 裡的 hermetic 簽章測試、`cli/ocwarden/selfupdate.go` 的 `signatureOf`/`codesignIdentity` 觀測路徑)在 T-0398 **全部刪除**——owner 拍板「全部拿掉,連手動簽章的逃生門一起刪」,所以不是預設關閉、不是留著等召回,是不在了。出貨的 binary 就是 `go build` 的產物(adhoc 簽章,cdhash 每 build 都變),`bin/release publish` 只有一種 builder。

**被抽掉之後什麼不再被守**:CI 原本有一道守簽章的檢查(step 0b 的 `bin/tests/run.sh`),它守的是「預設路徑不碰共用 login keychain」「憑證檢查壞掉不可被讀成憑證不在」「`OC_CODESIGN_REQUIRE=1` 下絕不默默降級出 adhoc」。這些現在都不再被守——因為被守的東西整個不存在了,不是漏掉。要重新引入簽章,請當成一張新票、連守衛一起帶回來,不要只補一支腳本。

⚠️ **不要把這條跟 TCC 身分錨點搞混**:`dist/officraft/officraft`(launchd 的 responsible process)是**用 bytes 認身分**的,所以它 byte-pinned、`bin/check-officraft-dist` 在 CI step 3 比對雜湊、裝過就永不覆寫。那套機制從來不依賴簽章憑證,**owner 核可保留,與本節無關**。

📌 **一份不隨簽章走的結論已搬家**:本節原本夾帶一份 repo 級的 shell 掃描結論(`pipefail` + 提前關 pipe 的消費者),它的主要案例當年是 `codesign-artifact`,但通則與逐檔判定跟簽章無關 ⇒ 見本文件最後一節〈shell 陷阱:`pipefail` 與提前關 pipe 的消費者〉。

⚠️ **兩句話都不要寫**:不要寫「不簽章的代價是 macOS 會重問 TCC 權限」,也不要寫「已證實 self-signed 對 TCC 完全無用」。self-signed codesign 對 macOS TCC 授權是否有效,**只是高度懷疑無效、從未有 100% 結論**(owner 2026-07-26;依據:在有簽章的那段期間他仍碰過不只一次授權詢問)。刪除的理由是**作業面的**:它卡測試又卡發佈。真的需要答案就在真機上量,拿證據回來。

## 安裝器內部

`bin/ocserver install` 的逐步細節（canonical layout、oc.toml 渲染、launchd plists、health check、首設啟用碼 banner、env override `OC_SERVER_ROOT` / `OC_SERVE_PORT` / `OC_CLOUDFLARED_CONFIG`）都寫在 `bin/ocserver` 檔頭註解與各 step 註解裡，那份是權威；tunnel 一律不代 provision，config + tunnel id + cloudflared binary 三者齊全才會掛 tunnel job。

## shell 陷阱:`pipefail` 與提前關 pipe 的消費者(T-da4b,已掃全 repo,結論=不動)

> **這一節與簽章無關,不要因為它的主要案例當年在 `codesign-artifact` 就把它一起丟掉。** 它原本寫在〈發佈簽章〉節裡,T-0398 移除簽章時搬到這裡並保留逐檔判定與通則——刪掉它的代價是下一個人碰到同一個構造時要重掃一次全 repo。

當年掃過全 repo **24 支**帶 `set -o pipefail` 的 shell script(其中 3 支——`codesign-artifact`／`setup-codesign-cert`／`build-release`——已於 T-0398 隨簽章刪除),逐一檢查 `| grep -q` / `| head` / `| sed -n Np` 這種「讀夠就關 pipe」的組合:writer 還在寫時消費者關掉 pipe,writer 吃 SIGPIPE(141),`set -o pipefail` 讓整條 pipeline 取 141。**結論是其餘一律不改**,因為判準不是「構造在不在」,而是**誤判往哪個方向倒**:

- **rc 根本沒被消費** → `| head -1 || true`(`bin/ocserver:103`)。`|| true` 吃掉 141,無害。(原本這裡還列了 `conformance/run.sh` 的 listener 查詢;**T-a3ba 已把它整段刪掉**——那行 `lsof … | head -1 || true` 換成「候選數 ≠ 1 就 FATAL」的 while-read 迴圈,`head -1` 的默默取第一個本來就是這張票要殺的東西,不再是本節的案例。)
- **倒向假紅(誤報失敗)** → `e2e_test/a1_zombie_e2e.sh:506/510/512`(`sed`/`head`/`tail | grep -qE`)、`e2e_test/tests_guard/run.sh`(`run_snippet` 的 `grep -q` 斷言)、`e2e_test/setup.sh:121`(`printf '%s' "$RESP" | py -c …`,T-a3ba 後 writer 是 builtin `printf` 而非 `curl`,窗更小)、`e2e_test/setup.sh:186`(登入取 token)。SIGPIPE → 141 → 測試紅／腳本中止。**吵,但不會騙人**,而且這些檔案 `bin/ci.sh` 只跑 tests_guard,其餘沒有活體證據可驗 —— 改了也證不了,是純 churn。
  > 行號是寫作當下的快照,會漂;以構造(不是行號)為準。上一版這兩行的行號在寫下時就已經對不上了。
- **倒向「看得見的 skip」**:**這是唯一另一個「檢查可能不執行」的方向**——`if <pipeline>` 守著一段可選的測試,141 讓它靜靜跳過而不是報錯。當年唯一的實例是 `bin/tests/run.sh` 裡 `openssl version | grep -q '^OpenSSL 3'` 守著一個 red control,而它**已隨簽章測試於 T-0398 一併刪除,repo 現在沒有這個構造的實例**。判準本身保留,因為方向分類是下面那條通則的一部分:當年它之所以可接受,是 else 分支會**印出 `skip — …`**、不是默默消失;再加上 `openssl version` 一口氣寫 ~25 bytes 就退出,窗開不起來。**新寫這種 guard 時要同時滿足這兩點。**
- **`echo "$VAR" | grep -q` 一律低risk**:writer 是 builtin、字串遠小於 64KB pipe buffer,grep 收到 EOF 前 write 早已完成。
- **通則(給後面的人):`pipefail` + 早關 pipe 只有在「141 會把某個 `if`/`if !` 翻成『壞事不存在』並讓流程默默往下走」時才是地雷。** 倒向紅、倒向可見 skip、rc 被 `|| true` 吃掉的,都不是同一種病。當年那個地雷是 `bin/codesign-artifact`(已刪):它是唯一一個誤判會**默默翻轉「出不出貨」**的點——`security find-identity | grep -Fq` 在第一行命中就關 pipe,憑證明明在卻被判成不在,於是默默降級出貨。修法是**把輸出整個收進變數再比對**(collect-then-compare),那支腳本已不存在,但**修法與判準對下一個同型構造仍然適用**。
