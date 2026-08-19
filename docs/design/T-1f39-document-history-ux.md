# T-1f39 長文欄位的版本歷史 UX ／ 手冊版控拆包 — 設計定案

狀態：**已核可**（owner 2026-07-31 逐題裁定，見文末「裁定紀錄」；❓ 清單已全部關閉）
撰寫：外包 O-103，2026-07-31
基準：`main` @ 42fe2bc

> 🔴 **部分內容已被 T-40f0 取代（owner 2026-07-31，卡 `rc-28885813e065` ①＋`rc-b69722f81136` ①）。**
> 本檔仍是 T-1f39 的裁定紀錄，**不要拿它當現況讀**。兩處具體被推翻：
> 1. **「初始版本」那一列不再直接跳還原確認**——它現在跟其他版本一樣先開閱讀面（可看內容、可比差異），還原仍在同一個破壞性確認框後面。相關的
>    `doc-history-seed-confirm` / `doc-history-seed-confirm-btn` 兩個 testid **已不存在**，改走
>    `doc-history-modal-restore` → `doc-history-restore-confirm`。
> 2. **差異呈現的摺疊（`@@` 分隔列、`diff-view-skip`、`collapseUnchanged`）已從畫面上移除**——現在整份顯示。
>    `lib/lineDiff.ts` 的 collapse 機制本體仍在（那是該模組自己的 API，仍有測試），只是 `DiffView` 永不要求它，
>    所以本檔 §E / L2 那些提到 `diff-view-skip` 與 `collapse()` mutant 的段落**描述的是當時的呈現層**，不是現況。
>
> 現況（含新的 `GET /api/document-history/{kind}/{key}/seed` 讀取面與 GitHub 式差異呈現）寫在
> `docs/design/T-40f0-history-diff-ux.md`。

owner 原話（2026-07-31，逐字）：

> 長文的學習經驗現在的user experence現在很差，應該要可以在編輯可能可以選擇history之類的，點進去跳出一個modal，預設是秀原本的內容，但右上角可能有個與現在的diff，看能不能像git一樣顯示出哪些有改，而任務手冊的purpose跟識別鍵都不需要版本控制，只要SOP跟學習經驗就好，兩者分開，不用同一包，而角色誌的部分也套用同樣做法

---

## 0. 現況（實查，非引用）

| 面向 | 現況 | 出處 |
| --- | --- | --- |
| 儲存 | 單一 `document_history` 表，`(document_kind, document_key, content_json, created_ts, actor_id)`；append-only | `server/ocserverd/migrations/00043_document_history.sql` |
| 保留 | `documentHistoryKeep = 3`，寫入時在同一交易內修剪 | `server/ocserverd/dal.go` |
| 寫入 | 唯一入口 `DAL.SaveWithDocumentHistory(kind, key, actor, snapshot, write)`；`snapshot` 在交易內重讀「寫入前」的活文件 | 同上 |
| 空文件 | 快照為空或 `{}` 時不留版 ⇒ 第一次寫入沒有歷史 | 同上 |
| kind／key | `global_context`/`global`、`role_definition`/`<role_key>`、`lessons`/`<role_key>::<task_type>`、`task_manual`/`<type_key>` | `server/ocserverd/api_document_history.go` |
| 手冊快照內容 | `{purpose, fields, sop_md, learnings}` **四欄同一包** | `server/ocserverd/api_taskmanuals.go` |
| 角色誌快照內容 | `{text, tombstoned}` — **本來就只有自己一欄** | `server/ocserverd/api_roles.go` |
| 讀取 | `GET /api/document-history/{kind}/{key}`（MCP `list_document_history`，floor=machine） | `server/ocserverd/routes.go` |
| 還原 | `POST /api/document-history/{kind}/{key}/{id}/restore`，`MCPExclude: true`（owner 2026-07-30 裁定：讀是 agent 工具、寫回只有座艙做） | 同上 |
| 座艙 | **已經有一張「版本紀錄」卡** `DocumentHistoryCard`，掛在四個編輯面下方；每列顯示時間／actor／逐欄預覽片段＋「還原這個版本」＋超上限的不可還原徽章 | `frontend/src/components/DocumentHistoryCard.tsx` |
| 座艙缺什麼 | **沒有 modal、沒有任何 diff**；只能看片段預覽 | 同上 |
| 上限 | `contextDocMaxChars`（rune；**寫此文時**是寫死的常數，見下方 §6 補記），`DocCapBlocked(before, after)`：未滿放行／超標但比原本短放行／超標且不短於原本擋下 | `server/ocserverd/domain.go` |
| 上限套用欄位 | 角色誌 `text`、手冊 `sop_md`、手冊 `learnings` **各自獨立計**；`purpose`、`fields`、全域情境、角色定義**不受限** | `api_roles.go` / `api_taskmanuals.go` |

**owner 說的「同一包」確認屬實**：改手冊裡任何一欄（含不需版控的 purpose）都會把四欄整份重存一版，3 版很快被無關改動洗掉。

**角色誌本來就不是同一包**：`lessons` 與 `role_definition` 早就是兩個 kind、兩條版本序列。因此「角色誌同辦」在後端沒有東西要拆（見 ❓Q4）。

---

## 1. 手冊拆包後的資料模型

新增兩個 kind，key 都沿用 `type_key`：

| kind | content_json | 由誰寫 |
| --- | --- | --- |
| `task_manual_sop` | `{"sop_md": ...}` | `update_task_manual`、`patch_task_sop`（都只在 SOP 真的變了時留版） |
| `task_manual_learnings` | `{"learnings": ...}` | `update_task_manual`（僅當學習經驗真的變了）、`write_task_learnings`、`patch_task_learnings` |

- **`purpose`、`fields`（識別鍵）、`display_name`、`assignee` 不再產生任何版本**——改它們不留快照、也不可還原。這是 owner 明示要的，同時也是**能力的刻意移除**（今天還原一版可以把 purpose 一起帶回來，之後不行）。
- 一次 `update_task_manual` 同時改到 SOP 與學習經驗時，在**同一個交易內**各留一版，兩條序列各自獨立修剪到 3 版。
- 刪除手冊時三種 kind（含舊的 `task_manual`）一併清除。

## 2. 角色誌

後端不動（已經獨立）。「同辦」落在**座艙 UX**：角色誌的版本紀錄卡一樣可點開 modal、一樣有 diff 切換。

## 3. 既有歷史的處置 ❓Q1

> ⚠️ **本節是提案，已被 owner 2026-07-31 的 Q1 裁定推翻**（見「裁定紀錄」）：不遷移、直接刪除。
> `00045` 這個編號仍然用上了，但內容是 `DELETE FROM document_history WHERE document_kind = 'task_manual'`，不是拆分。

**建議（未採用）：遷移（migration `00045`），把每一列舊的 `task_manual` 快照就地拆成最多兩列新 kind，保留原本的 `created_ts` 與 `actor_id`，然後移除舊列。**

- 拆完超過 3 版的照樣修剪。
- `sop_md` 或 `learnings` 為空的那半不產生列。
- **Down**：把同 `(key, created_ts, actor_id)` 的兩列合回一列 `task_manual`，`purpose` 與 `fields` 還原成空字串——**這一段是不可逆的損失**，因為新模型從此不存這兩欄。
- 理由：不遷移的話，舊列會變成座艙讀不到、agent 也問不到的殘骸，正是任務要求避免的；而丟掉的恰好是 owner 說「不需要版本控制」的兩欄。
- 代價我明講：**遷移一旦上線，purpose／識別鍵的既有歷史就永久消失**，Down 救不回來。

替代案（若不接受損失）：舊列原封不動、新寫入走新 kind，座艙對手冊多顯示一個唯讀的「舊版整包歷史」區塊直到自然被淘汰。代價是 UI 多一塊只為過渡存在的東西。

## 4. diff 跟誰比

**該版本 vs 目前存的內容**（owner 原話「與現在的 diff」）。不是 vs 下一版。

一個必須講清楚的邊界：使用者可能**正在編輯、尚未存檔**。diff 一律比對**伺服器上目前存著的內容**，不是編輯框裡的草稿；modal 上會標明這一點。

## 5. diff 在哪一端算 — **前端**

理由（依序）：

1. **改對外介面的成本遠高於算 diff 的成本**。後端算就要新增一條路由＋spec／Go／前端型別三份同步＋routes manifest＋auth matrix＋MCP catalog 快照。純算字串不值得付這個。
2. **量級撐得住**。受版控的長文有一份 rune 上限（角色誌、SOP、學習經驗；寫此文時是萬字量級，見 §6 補記），行數量級數百行，LCS 行級 diff 在瀏覽器是毫秒級。
3. **前端算才能反映「目前內容」的即時狀態**——目前內容本來就已經在前端手上（編輯面就是靠它渲染的），後端算反而要多讀一次、還可能與畫面不一致。
4. 未受上限保護的 `global_context`／`role_definition` 理論上可以很長 ⇒ 加一道保險：任一側超過設定的行數上限就不算 diff，退化成只顯示原文並提示原因。

**不引入新套件**：`frontend/package.json` 目前沒有任何 diff 函式庫，本 repo 的慣例是手寫（`Markdown.tsx` 即是）。自寫一支行級 LCS diff 工具＋單元測試。

## 6. 字數上限 — **不變**

> 併入 origin/main 後補記（T-3aeb，owner 2026-07-31）：上限已經不是寫死的 10,000，而是設定值
> （T-ae38 起是四個，T-30f1 起是五個：`doc_cap_chars_duty` / `_insight` / `_learning` / `_manual_sop` / `_manual_learnings`，角色定義有自己一個比較小的預設、其餘四個共用另一個——實際數字見 `server/ocserverd/domain.go`，這裡不複述；
> 角色定義也是 T-ae38 才開始有上限的）。以下這段沿用單一 `doc_cap_chars`
> 設定（範圍的下限＝出廠預設、上限 100,000，只能往上調）。本節寫「10,000」的地方一律讀作**當下設定的上限**；
> 本節主張的「逐欄獨立、拆包不改變上限行為」與那項改動正交，兩邊都成立。

上限今天就是**逐欄獨立**計的（角色誌 `text`、手冊 `sop_md`、手冊 `learnings` 各一份），拆的是「版本快照怎麼分包」，不是欄位本身。所以：

- 拆開後每份的上限仍是各自一份**設定的上限**，**與現行行為完全一致，沒有默默改變任何東西**。
- 還原時的上限檢查跟著縮小範圍：還原 `task_manual_sop` 只檢查 `sop_md`，還原 `task_manual_learnings` 只檢查 `learnings`（今天是兩欄一起檢查，任一超標就整包擋下）。這是拆包的**附帶好處**，也是一項行為改變，需要測試釘住。

## 7. 座艙 UX

1. 四個編輯面（全域情境、角色誌、手冊 SOP、手冊學習經驗）的版本紀錄卡，每一列變成**可點**。
2. 點開 `DocumentHistoryModal`：
   - **預設分頁＝該版本的原本內容**（❓Q5：純文字原樣 vs 渲染後 markdown）。
   - **右上角切換 →「與目前的差異」**：git 風格的 unified diff，逐行標 `+` 新增／`-` 刪除／context，並以顏色區分；標題列寫明「與目前存檔內容比較」。
   - 標頭：版本時間、actor、第幾版。
3. 手冊「任務定義」頁的版本紀錄卡從此只代表 **SOP** 的歷史（purpose／識別鍵不再有版本），標題與說明文案要講明白，否則使用者會以為改 purpose 也留了版。
4. 還原按鈕的落點 ❓Q3。
5. zh／en 兩語文案齊全；leaf 一律純字串（帶參數的句子拆 lead／tail 在 `compose.ts` 組裝，這是 T-081b 的既有規則）；`npm run gen:msgkeys` 重生**兩份** generated 檔。

## 8. 對外介面異動

- `list_document_history` 的 `kind` 列舉新增 `task_manual_sop`、`task_manual_learnings`。
- 舊值 `task_manual` **已退場**（Q2 裁定）：list 與 restore 都回 400，訊息指名兩個新 kind。
- 還原路由沿用同一條，只是 kind 多兩個；`MCPExclude` 不變（還原仍只有座艙做）。
- **不新增任何路由**（diff 在前端算）。
- 需同步：`spec/openapi.json` → `bin/gen-ocapi` → `frontend` `gen:api`；`spec/mcp-catalog.json`（手維護、byte-equality）；`conformance/routes_manifest.json`（路由集合沒變，只有描述／列舉變動）；`spec_catalog_conformance_test.go` 的 drift 基線；`seeds/system_interaction.md` §4。

---

## ❓ 待裁清單（我不自己決定）

| # | 問題 | 我的建議 | 若選另一邊的代價 |
| --- | --- | --- | --- |
| Q1 | 既有 `task_manual` 整包歷史：遷移拆分（丟掉 purpose／識別鍵的歷史、Down 不可逆）還是原封不動只從此刻分流？ | **遷移拆分** | 不遷移＝舊列成為讀不到的殘骸，或要多做一塊只為過渡存在的唯讀 UI |
| Q2 | 舊 kind 名 `task_manual` 還要不要接受？ | **回 400 並指名兩個新名稱** | 回空清單的話，「沒有歷史」與「你用錯 kind」長得一模一樣，錯誤會被吞掉 |
| Q3 | 「還原」按鈕留在卡片列上、搬進 modal、還是兩邊都有？ | **搬進 modal**（看完再決定要不要還原，符合 owner 描述的動線） | 兩邊都有＝同一個危險動作兩個入口；只留卡上＝modal 看完還要關掉回去按 |
| Q4 | 角色誌本來就與角色定義各自獨立，所以「同辦」我理解為**只套 UI、後端不動**。確認？ | 是 | 若 owner 其實想連角色定義也分欄拆包，範圍會擴大 |
| Q5 | modal 預設的「原本內容」顯示成純文字原樣，還是渲染後的 markdown？ | **純文字原樣** | 渲染後好讀，但與 diff 的行對不上、也看不到實際存了什麼字元 |

---

## 裁定紀錄（owner，2026-07-31，全部直接對 owner 取得）

| # | 裁定 | 覆蓋掉的原建議／備註 |
| --- | --- | --- |
| Q1 | **不遷移，舊歷史直接刪除**。owner 原話：「不需要管舊歷史，舊歷史資料直接刪除就好，不要留技術債」；答卡時再次確認「不轉，舊歷史也可直接丟棄，不留技術債」 | 比 §3 的兩個選項都更強——不是遷移、也不是留著，是刪掉 |
| Q1-b | **刪除範圍＝舊 `task_manual` 那 9 筆，不含角色誌／全域情境**；**不先匯出備份**。owner 在被明確告知「刪掉的不只用途欄，SOP 與學習經驗的舊版本也會一起消失、其中最新一筆是當天早上寫的」之後，仍選擇「直接刪，不用備份」 | 我建議先匯出，owner 否決；資訊完整下的決定 |
| Q2 | **舊 kind 名 `task_manual` 回 400 並指名兩個新名稱**（Q1 定為刪除後，「保持可讀」的分支不存在了） | Kyle 先裁、owner 的 Q1 答案確立其前提 |
| Q3 | **還原按鈕搬進 modal**（清單列上不再有） | 照建議 |
| Q4 | 角色誌本來就與角色定義各自獨立 ⇒ **只套 UI、後端不動** | Kyle 依實查事實直接確認 |
| Q5 | **modal 預設顯示渲染後的排版**；**切到差異時改用原始文字逐行比對**。owner 原話：「點開應該是顯示渲染後的排版，點diff…才顯示原本的原始文字作比較？」 | **推翻我的建議**（我建議預設純文字）。理由已回覆 owner：渲染後沒有「行」的概念、標不出哪幾行變了，所以差異檢視必須用原始文字 |
| i18n | **以程式為準（zh／en 兩語），並修掉文件那句「三語含 xian」** | 見下節；這是 owner 對「兩份權威打架」的裁定，不是我判的 |

### 刪除的執行紀律（不可逆動作）

- 單一路徑：只刪 `document_kind = 'task_manual'` 的列，fail-closed（條件不符一列都不動）。
- Down 無法還原資料——這是 owner 知情後選擇的結果，migration 內須寫明，不得假裝可逆。
- 刪完以實查坐實（不以指令成功代替），並確認沒有任何仍在使用的東西指向那些列。

## ⚠️ 另外回報一件文件打架（我沒有自己改任何一邊）

`frontend/CLAUDE.md:12` 寫「i18n 三語 zh / en / xian」；程式碼實際只有兩語（`Locale` 型別是封閉聯集 `"zh" | "en"`，`locales/` 只有 `en.ts`、`zh.ts`，xian 只存在於**佈景主題**而非語系）。本任務的交辦說明也明講那句是過時的假話——但那份說明本身也只是打架的其中一方，不能拿來當裁定。

**owner 2026-07-31 裁定：以程式為準（兩種語言），並修掉文件那句。** 依此裁定，本任務一併修正 `frontend/CLAUDE.md` 的該行；裁定之前我沒有動任何一邊。

---

## Mutant 驗證（後端刪除與舊 kind 拒絕）

**為什麼要落檔**：這一批動的是**不可逆的資料刪除**與**對外行為的收緊**，兩者都屬於「跑起來看不出對錯」的改動。
`worker-panel-parity-mutants.md` 立的規矩照搬：**沒落檔等於不存在**，每一列都要能被獨立重放。

### 方法

1. 把要驗的檔案複製到 scratchpad 備份，`shasum -a 256` 記下雜湊。
2. 施加**單一、明確**的一處改動（描述欄寫的就是那一處）。
3. 跑該範圍的測試，記下**哪幾條紅、紅在哪一行**。
4. **從 scratchpad `cp` 回來**（🔴 不准 `git checkout --`，會吃掉別人未提交的編輯），
   `shasum -a 256` 驗還原後逐位元組相同。
5. **每一支 mutant 前先 `go clean -testcache`** —— 這個 repo 的既有規矩，快取結果在這裡燒過人。

還原檢查：`migrations/00045_drop_legacy_task_manual_history.sql` = `f5fa5f69…16d5c`、
`api_document_history.go` = `b9644401…9cc3`，兩支收工時皆 `OK`。

### A. 遷移的外科精準度 — `migrations/00045_drop_legacy_task_manual_history.sql`

測試：`TestMigration00045DeletesOnlyTheRetiredTaskManualHistory`（`migrate_test.go`）。
fixture 刻意**放進會被錯誤刪掉的東西**：同一個 `document_key` 底下的 `task_manual_sop` /
`task_manual_learnings`（同前綴、只差後綴），以及 `lessons` / `global_context` / `role_definition`。
沒有這些列，這支測試證明的是 fixture，不是碼。

| # | Mutant（改了什麼） | 紅了哪條斷言 |
|---|---|---|
| D1 | `WHERE document_kind = 'task_manual'` → `LIKE 'task_manual%'`（前綴放寬） | `migrate_test.go:1103` **點名 4 列**：`task_manual_sop/tm-alpha`、`task_manual_sop/tm-beta`、`task_manual_learnings/tm-alpha`、`task_manual_learnings/tm-beta`；＋`:1108` `holds 3 rows … want the 7 non-legacy rows`；＋`:1123` 回滾列數 |
| D2 | WHERE 整條拿掉（`DELETE FROM document_history;`） | `:1103` **點名 7 列**（上述 4 列 ＋ `lessons/r-assistant::review-pr`、`global_context/global`、`role_definition/r-assistant`）；＋`:1108` `holds 0 rows … want the 7`；＋`:1123` |
| D3 | Down 假裝可逆：從 `task_manual_sop` 合成 `task_manual` 列塞回去 | 只有 `:1123` `rollback changed the table to 9 rows, want the same 7 — the Down is a no-op and the deletion is irreversible by design` |

D1／D2 是**同一半的兩個力道**：D1 證明「同前綴的鄰居不能碰」，D2 證明「別的 kind 一列都不能碰」。
D3 打的是另一半 —— 沒有它，一份謊稱可逆的 Down 不會被任何斷言擋下。

### B. 舊 kind 的 400 — `api_document_history.go`

測試：`TestDocumentHistoryRefusesTheRetiredTaskManualKind`（`api_document_history_roundtrip_test.go`），
list ／ restore 兩個 subtest ＋ 尾端的陽性對照（兩個新 kind 在**同一本手冊**上照常 list／restore）。

| # | Mutant | 紅了哪條斷言 |
|---|---|---|
| H1 | 400 → 404（`writeError(w, http.StatusNotFound, …)`） | 兩個 subtest 的 `:424` `list/restore under the retired kind = 404 …, want 400` |
| H2 | 舊 kind 直接 fall through（當成活的 kind，list 回空陣列） | list `:424` `= 200 []`、restore `:424` `= 404 {"…document history version not found"}` — **這正是要防的「錯誤被吞掉」的形狀** |
| H3 | 訊息不再指名兩個新 kind（改成 "use the per-field kinds instead"） | `:428` **四條**（list／restore × `task_manual_sop`／`task_manual_learnings`）`the … refusal does not name %q, so a caller on the old name cannot tell which series it wanted` |

🔴 **H3 是這一批唯一非做不可的一支。** H1／H2 只證明「會拒絕」——把訊息換成一句沒有資訊的
「不支援」，那兩支照樣紅不了，而 Q2 裁定要的**恰恰是那兩個名字**。

### C. 覆蓋面的證明（三條必須回 0 行的 grep）

「改了 A 要掃過所有引用 A 的 B」不能靠列清單自證。以下三條在 repo 根目錄執行。
本設計檔 §0 的「現況」表是**改動前的實查快照**（刻意不隨碼改寫），三條一律排除它。

```sh
# G1 production 碼裡對退場 kind 的引用,只剩「宣告這個名字」與「認出它並拒絕」兩處
git grep -n '\bdocKindTaskManual\b' -- 'server/ocserverd/*.go' ':!server/ocserverd/*_test.go' \
  | grep -vE 'api_taskmanuals\.go:[0-9]+:\s*docKindTaskManual +=|api_document_history\.go:[0-9]+:\s*case docKindTaskManual:'

# G2 除了 00045 那支刪除以外,沒有任何 SQL 拿這個 kind 當字面值讀寫
git grep -nE "document_kind[^)]*task_manual'" -- server \
  ':!server/ocserverd/migrations/00045_drop_legacy_task_manual_history.sql' ':!server/ocserverd/*_test.go'

# G3a 沒有任何 fixture／文件還拿舊 kind 去定址那兩條路由
git grep -n 'document-history/task_manual/' -- . ':!frontend'

# G3b 沒有任何散文／catalog 還把舊 kind 跟其他三種並列成「可版控的文件種類」
git grep -nE '(global_context|role_definition|lessons)[^"]*[^_a-z]task_manual([^_a-z]|$)' \
  -- docs seeds spec conformance server/CLAUDE.md ':!docs/design/T-1f39-document-history-ux.md'
```

**G1／G2／G3a 目前回 0 行。**
**G3b 目前回「恰好一行」：`spec/mcp-catalog.json:2889`** —— `list_document_history` 的工具敘述仍寫
`(global_context | role_definition | lessons | task_manual)`。那份與 `spec/openapi.json` 是
**byte-equality 的線上契約凍結檔**，本批**刻意不動**（另一個 wire-freeze 步驟處理）。
那一步落地後 G3b 必須回 0 行；在那之前，這一行就是它的待辦清單本身。

⚠️ **`task_manual` 這個字串在別的命名空間仍然是活的，不是漏網**：SQL 資料表 `task_manual`
（`migrations/00004_tasks.sql`）與 SSE topic `task_manual`（`hub.go` 的 12-topic 閉集）。
上面的 pattern 因此全部綁在 **document-kind 的語境**上（`docKindTaskManual` 識別字、
`document_kind` 欄、`document-history/` 路徑、與其他三種 kind 並列的散文），
而不是裸字串出現 —— 一條盯裸字串的 grep 在這裡只會變成永遠回不了 0 的噪音。

### C2. 修改者顯示「名字（代號）」 — `lib/actorLabel.ts` / `i18n/compose.ts` / `DocumentHistoryCard.tsx`

owner 2026-07-31 追加的要求（原話：「還會標上是誰改的嗎？如果是我改的或是AI session改的，會顯示是誰？」
→ 裁定「名字與代號並列，查不到名字就只顯示代號」；釋出／解僱的身分「先簡單做，就顯示代號就好」）。
名冊只列在職者，所以查不到名字的路徑是**正常路徑**，不是錯誤路徑——它得跟「查得到」一樣被釘住。

測試：`components/DocumentHistoryCard.actor.test.tsx`，兩例（在職成員／已釋出外包）。
fixture 刻意同時放入**兩種 actor**：只放其中一種，另一半的斷言不可能紅。

| # | Mutant | 紅了哪條斷言 |
|---|---|---|
| A1 | 解析器查不到名字時回傳代號（`?? actorId`）而非空字串 | 「bare id」那條：`expected '…修改者 ow-c975fff254f7（ow…' not to contain 'ow-c975fff254f7（'` — 代號被自己包了一次 |
| A2 | `docHistoryActor` 拿掉 `name ?` 判斷，一律加括號 | 同一條：空名字產生 `（ow-…）`，正是 owner 看不懂的形狀 |
| A3 | 卡片只顯示名字、不帶代號 | 「name + id」那條：`Kyle.*m-f663f3c5de9a` 的 regex 找不到 |

A1／A2 是**同一個洞的兩個方向**（解析端補、格式端補），A3 打的是另一半——沒有它，
一份「只顯示名字」的實作照樣全綠，而代號正是改名後唯一還認得出人的東西。
還原檢查：三支檔案收工時 `shasum -a 256 -c` 皆 `OK`。

### D. 這一批動到、但**沒有**機械護欄的一件事

`spec/openapi.json` 的 `kind` path 參數是 `{"type": "string"}`，**沒有 enum**，所以退場一個 kind
在 openapi 那一側**沒有東西會紅**。`spec_catalog_conformance_test.go` 的 drift 基線同理只看
路由集合，不看敘述。舊 kind 的收緊因此**只被 Go handler 測試釘住**（上表 H1–H3）——
改 `documentHistoryAllowed` 的下一個人請注意：spec 那邊不會提醒你。

### E. 前端行為的 mutant（modal／diff／欄位對照）

**為什麼要落檔**：後端那批（A／B／C2）都是「資料被刪掉」或「請求被拒絕」，錯了會在
整合測試或現場立刻炸開。這一批前端的不會——`lineDiff.ts` 的 LCS、`DiffView.tsx` 的
`-`／`+` 對應、`DocumentHistoryModal.tsx` 的預設分頁與還原閘門、`docHistoryFields.ts`
的欄位對照表，錯了畫面照樣渲染，只是**內容說謊**：明明是這一版的內容卻標成現在的、
明明還沒按確認卻已經覆蓋、手冊 SOP 的版本裡混進了學習經驗的欄位。這種錯誤不會讓
build 失敗，只會讓 owner 在某一次真的照著畫面按下「還原」時，還原了不是他以為的
東西。跟 A／B／C2 一樣，**沒落檔等於不存在**，方法沿用同一套：scratchpad 備份、單一
明確改動、跑範圍測試記紅在哪、`cp` 還原（不用 `git checkout --`／`restore`／`stash`，
本樹還有別人未提交的工作要活下來）、`shasum -a 256 -c` 驗證還原。

還原檢查：`lineDiff.ts` = `508aaedb…b25dcc`、`DiffView.tsx` = `4045c8f4…54094`、
`DocumentHistoryModal.tsx` = `27713ed0…5460a4a`、`docHistoryFields.ts` = `f5c9a788…d10e6fa29`、
`DocumentHistoryCard.tsx` = `b7664704…26d23615`，五支收工時皆 `OK`（詳細雜湊見
scratchpad `before.sha`）。CSS 版面的三支 mutant（`position: fixed`／`overflow-y: auto`／
`min-height: 0`）已經記在 `visual-guards/doc-history-modal.ct.spec.tsx` 檔頭，這裡不重複。

測試範圍：`src/lib/lineDiff.test.ts`、`src/components/DiffView.test.tsx`、
`src/components/DocumentHistoryModal.test.tsx`、`src/components/DocumentHistoryCard.actor.test.tsx`、
`src/components/SettingsPage.document-history.test.tsx`（僅 vitest，未跑全量 CI）。

#### E1. `lib/lineDiff.ts` — LCS 行級 diff

測試：`src/lib/lineDiff.test.ts`。

| # | Mutant（改了什麼） | 紅了哪條斷言 |
|---|---|---|
| L1 | `buildRows` 裡 `removed`／`added` 兩個 kind 字面值互換 | 9 條全部翻車：`:35`「emits only the changed middle line as removed-then-added」、`:44` 純新增、`:52` 純刪除、`:60`／`:70` 空側、`:84` 新增空行、`:103` 收合 hunk、`:123` 遠距離 hunk、`:153` 剛好卡在上限 —— kind 欄位全部對調，`removed`/`added` 的意義整個反過來 |
| L2 | `collapse()` 的 `start = Math.max(cursor, index - contextRadius)` 改成 `- contextRadius - 1`（context 半徑多算一行） | `:98` `expect(hunk.skippedBefore).toBe(7)` 收到 `6`；`:119` 第二個 hunk 的 `skippedBefore` 收到 `0`（預期 `1`）——收合的「跳過幾行」報數不對，`@@` 分隔列上顯示的數字會跟著錯 |
| L3 | 過大拒算的邊界 `a.length > maxLines` 改成 `>= maxLines`（兩側恰好等於上限也被拒） | `:154`「still diffs when both sides sit exactly on the threshold」：`expect(result.status).toBe("diffed")` 收到 `"too-large"` |

L1 是三個行為裡最基本的一個：把「哪一行是刪除、哪一行是新增」的判斷寫反，這是
git-style diff 讀者第一眼就會看錯方向的錯誤。L2 打的是「收合未變更區塊、標出跳過幾行」
這件事本身——`collapse()` 有兩處會影響 `skippedBefore` 的計算（`start` 的算法、以及
`k - end > contextRadius * 2` 的合併距離判斷），第一種順著現有測試的斷言路徑走，紅得
很直接；下面 §E1a 記另一種**沒有**紅的改法。L3 打的是「過大拒算」的邊界本身，而不是
拒算之後的行為（那由 DiffView 的 too-large 畫面測試釘住）。

##### E1a. GREEN——`collapse()` 的「合併距離」判斷沒有邊界測試

嘗試的 mutant：`collapse()` 裡「下一個變更的前導 context 是否碰到這一個」的判斷
```
} else if (k - end > contextRadius * 2) {
```
改成
```
} else if (k - end > contextRadius * 2 + 1) {
```
（把兩個變更合併成同一個 hunk 的距離門檻放寬一格）。跑 `src/lib/lineDiff.test.ts`
（17 條）：**全綠**，`vitest run` 回報 `17 passed (17)`，沒有任何一條紅。

**這是真的洞，不是誤判**：現有測試裡「splits distant changes into separate hunks」
（`:113`）用的兩個變更距離足夠遠（`contextRadius: 1` 時中間隔了 20 行），鬆放一格門檻
不會讓它們被錯誤合併；也沒有另一條測試把兩個變更精確擺在「門檻的邊界上」（即
`k - end` 恰等於 `contextRadius * 2` 或 `contextRadius * 2 + 1`）來檢查合併與否的臨界值。
換句話說，**「兩個變更多近會被算成同一個 hunk、多遠會被拆成兩個 hunk」這條邊界目前
沒有任何一條斷言釘住**——差一行仍然可能悄悄把本該分開的兩個變更黏在一起，或反過來
把該黏在一起的拆開，多顯示一段不必要的 `@@` 分隔與跳過行數。我沒有補這條測試（不屬於
「一眼能補」的範圍——要選一組能剛好卡在邊界上的行號組合，屬於這個模組本來就該有的
一條專門測試，留給下一個碰這支檔案的人）。

#### E2. `components/DiffView.tsx` — 哪一側是 `-`／`+`

測試：`src/components/DiffView.test.tsx`、`src/components/DocumentHistoryModal.test.tsx`。

| # | Mutant | 紅了哪條斷言 |
|---|---|---|
| D1 | `diffLines(before, after, …)` 呼叫時兩個參數對調成 `diffLines(after, before, …)` | `DiffView.test.tsx:41`「renders a one-line edit as one removed row and one added row with both side's line numbers」；連帶 `DocumentHistoryModal.test.tsx:109`「toggles to the diff, showing this version as - and the current as +」、`:166`「diffs each field a multi-field revision carries」（`-助理`／`+總管` 的方向整個反過來，且此測試斷言用的是**具體文字**而非只看 kind，所以真的翻車）、`:186`「compares a field the CURRENT document has and the revision does not」 |

D1 是「這一版是 `-`、現在存的內容是 `+`」這個 owner 明確裁定（§7-2）的行為本身；它同時
也驗證了 DiffView 的呼叫慣例被 Modal／SettingsPage 兩層都吃到，一次 mutant 打穿三個
測試檔的多條斷言，說明「哪一側是哪一側」在這個 repo 裡是**被多處交叉釘住**的，不是單點。

#### E3. `components/DocumentHistoryModal.tsx` — 預設分頁／還原閘門／過上限的死按鈕

測試：`src/components/DocumentHistoryModal.test.tsx`。

| # | Mutant | 紅了哪條斷言 |
|---|---|---|
| M1 | `useState<Pane>("content")` 改成 `useState<Pane>("diff")`（預設分頁換成差異） | `:85`「opens on the version's content, RENDERED as markdown」：`body.querySelector("h2")?.textContent` 預期 `"標題"`，收到 `undefined`（渲染出來的是 diff 面板，找不到 markdown 的標題元素） |
| M2 | 「還原」按鈕的 `onClick` 從 `setConfirming(true)` 改成直接 `commitRestore()`（跳過確認框） | 四條斷言全紅：`:237`「restore asks first…」`expect(onRestore).not.toHaveBeenCalled()` 收到已呼叫 1 次；另外三條找不到 `doc-history-restore-confirm-btn`／`doc-history-restore-confirm` 這兩個 testid——確認框根本沒出現過 |
| M3 | 還原按鈕的 `disabled={blocked}` 改成 `disabled={false}` | `:224`「judges the cap on the ONE field the restored series writes back」與 `:303`「cannot restore an over-cap revision, and says why」：兩條的 `restore.disabled` 預期 `true`，收到 `false` |

M1 打的是 owner 2026-07-31 對 Q5 的裁定本身（預設渲染後排版，不是差異）；M2 打的是
「還原是破壞性動作、一定要先過確認框」這條規矩——四條斷言一次全紅，說明這個閘門
在測試裡被從「按下按鈕」到「確認框出現」到「呼叫時機」三個角度重複釘住；M3 打的是
「過上限的版本連還原鈕都是死的」，這是 modal 專屬（不是卡片列的）的行為，兩條斷言
（拆包 kind 與整包 kind 各一）分別釘住。

#### E4. `lib/docHistoryFields.ts` — 手冊拆包後兩個 kind 只秀自己的欄位

測試：嘗試跑 `src/components/DocumentHistoryModal.test.tsx`、
`src/components/DocumentHistoryCard.actor.test.tsx`、`src/components/SettingsPage.document-history.test.tsx`。

| # | Mutant | 結果 |
|---|---|---|
| F1 | `DOC_FIELD_ORDER.task_manual_sop` 從 `["sop_md"]` 放寬成 `["sop_md", "learnings"]`（SOP 這個 kind 也認得 learnings 欄位） | **GREEN**——上面三個測試檔合計 55 條全過，沒有任何一條紅 |

**這是真的洞**：`DocumentHistoryModal.test.tsx:202`「judges the cap on the ONE field the
restored series writes back」是全庫**唯一**一處用 `kind: "task_manual_sop"` 開 modal 的
測試，但它給的 `content` 剛好兩個欄位都有值（`{ sop_md: "短 SOP", learnings: overCap }`），
斷言也只檢查還原鈕的 disabled 狀態（走的是 `api/docCap.ts` 自己另一份 `CAP_FIELDS`
常數，跟 `docHistoryFields.ts` 的 `DOC_FIELD_ORDER` 是兩份不同的表），完全沒有檢查
「分頁裡實際渲染出幾個欄位、哪幾個」。`api/mock.document-history.test.ts` 雖然大量用到
`task_manual_sop`／`task_manual_learnings`，但測的是**寫入路徑**（誰寫哪個 kind、剪
到 3 版），不是**顯示路徑**的欄位過濾。換句話說：**手冊 SOP 的版本歷史，理論上今天
可以在 modal／卡片列上把學習經驗欄位一起秀出來，不會有任何一條既有測試發現**——這正
是 §1 拆包要拆開的東西（「兩個新 kind 各自只認自己的欄位」）在前端顯示層目前**沒有
機械護欄**。我沒有補這條測試（多欄位情境下要同時起 modal／card 兩層斷言，不算「一眼
能補」，留給下一個碰這支檔案或安排前端補測的人）。

#### E5. `components/DocumentHistoryCard.tsx` — 列上沒有還原鈕

測試：`src/components/SettingsPage.document-history.test.tsx`
（`it("offers no restore control on the row itself — only inside the version")`）。

| # | Mutant | 紅了哪條斷言 |
|---|---|---|
| C1 | 在 `.doc-hist__row` 裡加回一個還原 `<button data-testid="doc-history-restore-{id}">`，直接呼叫 `restore(v.id)` | `SettingsPage.document-history.test.tsx:152`：`expect(within(row).queryByText(s.historyRestore)).toBeNull()` 收到一個真的按鈕元素，不是 `null` |

這條測試就是這批唯一直接對著「T-1f39 把還原搬進 modal」這件事本身寫的：它不是測
「還原能不能用」（那是 E3 的範圍），而是測「舊入口真的不見了」——把還原鈕加回列上，
一支斷言就抓到，說明「單一入口」這個 owner 裁定（Q3）在前端有機械護欄看著。

#### 小結

本節共跑 9 支 mutant：8 支紅、1 支綠（E1a 是額外多驗的邊界嘗試，連同 E4 的 F1，
一共驗了 10 個改動點，其中兩個綠）。**綠的兩個是未受保護的行為，明講在這裡**：

- `lib/lineDiff.ts` 的 `collapse()`：兩個變更「多近會被合併成同一個 hunk」的門檻邊界
  沒有測試釘住（§E1a）。
- `lib/docHistoryFields.ts` 的 `DOC_FIELD_ORDER`：手冊拆包後 `task_manual_sop`／
  `task_manual_learnings` 兩個 kind「只秀自己欄位」這條規則，在顯示層（modal／卡片）
  沒有任何測試檢查實際渲染出的欄位集合（§E4）。

### F. 版本入口搬進編輯列（owner 2026-07-31 裁定）

**裁定本身**（owner 原話，逐字）：

> 可以讓版本資訊直接預設在編輯的時候，出現在其中一個就好嘛？可能是那個重置的位置，
> 可以考慮改個名字，如果是有預設的，除了歷史三個版本，多一個是初始，這樣有點選的
> 時候再打 API 就可以，也不會有不知道版本是對哪一個 section 的狀況，所有頁面都這樣處理

並在明示的二選一卡片上選了：**取代：重置鈕改成版本入口，「初始版本」當清單裡的一項**。
追加一句（同日）：修改者若是 owner，顯示座艙自己的 `t.user`，不要裸代號。

落地的形狀：`DocumentHistoryCard`（永遠掛在編輯面下方的卡）**退場**，改成
`DocumentHistoryEntry` —— 一顆站在編輯列（原本 重置 的位置）的 `.doc-btn`，點開才
`GET`、點開才渲染清單；清單列點進去仍是原封不動的 `DocumentHistoryModal`（只多一顆
「返回版本列表」）；有 seed 預設的文件（`onReset`）在清單**最後**多一列「初始版本」，
它就是重置的唯一入口，走跟還原同一個破壞性確認框。五個面（全域情境、角色定義、
角色學習經驗、手冊 SOP、手冊學習經驗）各自在自己的編輯列上掛同一顆按鈕。

**為什麼要落檔**：這一批改的全是「錯了也照樣渲染」的東西——少發一個請求跟多發一個
請求在畫面上長得一樣；「初始版本」多長一列或少長一列在畫面上也長得一樣，只有在
使用者真的點下去的那一刻才會分別變成 404 或是一次沒人要的整份覆蓋。方法沿用本檔
既有的規矩：scratchpad 備份、單一明確改動、跑範圍測試記紅在哪、`cp` 還原（🔴 不用
`git checkout --`／`restore`／`stash`，本樹還有別人未提交的工作要活下來）、
`shasum -a 256 -c` 驗還原逐位元組相同。

還原檢查（收工時四支皆 `OK`）：
`hooks/useDocumentHistory.ts` = `18506320…c44c659`、
`components/DocumentHistoryEntry.tsx` = `1847a282…99e3162f`、
`components/SettingsPage.tsx` = `626887c5…05cc7f97`、
`components/DocumentHistoryModal.tsx` = `5420d946…cbab3120`。

測試範圍：`src/components/SettingsPage.document-history.test.tsx`、
`src/components/SettingsPage.roles.test.tsx`、
`src/components/DocumentHistoryEntry.actor.test.tsx`、
`src/components/DocumentHistoryModal.test.tsx`（僅 vitest 與 `tsc --noEmit`，未跑全量 CI）。

| # | Mutant（改了什麼） | 紅了哪條斷言 |
|---|---|---|
| F1 | `useDocumentHistory` 的 `if (!enabled) return;` 拿掉（回到 mount 就載入） | `SettingsPage.document-history.test.tsx`「asks the server for nothing until the entry is clicked」：`expected "listDocumentHistory" to not be called at all, but actually been called 1 times` |
| F2 | 「初始版本」那一列的 `{onReset && …}` 改成 `{true && …}`（沒有 seed 的文件也長出重置入口） | 「carries 初始版本 exactly where the document has a file seed」：自訂角色那一輪 `expected { surface: 'r-…', seeded: true } to deeply equal { …, seeded: false }`；連帶「gives the manual's SOP and learnings their own history」`expected …(2) to have a length of 1 but got 2` |
| F3 | 「初始版本」的 `onClick` 從 `setResetting(true)` 改成直接 `commitReset()`（跳過確認框） | 兩條：「resets through the 初始版本 row, and only after the same confirmation」`expected "resetGlobalContext" to not be called at all, but actually been called 1 times`；「cancelling the 初始版本 confirmation resets nothing」找不到 `doc-history-seed-confirm` |
| F4 | `actorLine` 的 `actorId === OWNER_ACTOR_ID ? t.user : …` 分岔拿掉 | `DocumentHistoryEntry.actor.test.tsx`「calls the OWNER by the cockpit's own label」：`expected '…修改者 owner檢視這個版本…' to contain 'CEO（你）'` |
| F5 | 在編輯列把 重置 按鈕加回去（版本入口旁邊，兩個入口並存） | 兩條：`SettingsPage.roles.test.tsx`「edit mode offers a way back to the seed…」與 document-history 的「resets through the 初始版本 row…」，皆 `expected <button …> to be null` |
| F6 | modal 的 `onBack={() => setReading(null)}` 改成 `onBack={closeAll}`（返回等於關閉） | 「offers no restore control on the row itself — only inside the version」：按下返回後 `Unable to find an element by: [data-testid="doc-history-list"]` |
| G1 | 「初始版本」整段搬到 `<ul>` 的**最前面**（清單第一列而非最後一列） | **第一次跑：GREEN**——見下 |

F1 打的是裁定裡唯一一句講到時序的話（「有點選的時候再打 API」）；F2／F5 是同一個
洞的兩個方向（該有的沒有／不該有的有了），F3 打的是「重置從一鍵變成一列之後**沒有
變便宜**」，F4 打的是 owner 追加的那一句本身，F6 打的是「清單→版本」這條動線的回程。

#### F-G1. 一開始 GREEN 的一支：「初始版本」排在清單哪一端

把整段 `{onReset && <li …初始版本…/>}` 從 `<ul>` 的尾端搬到最前面，跑
`SettingsPage.document-history.test.tsx`＋`SettingsPage.roles.test.tsx`＋
`DocumentHistoryEntry.actor.test.tsx` 合計 37 條：**全綠**。

這是真的洞，不是誤判：清單是新到舊排的，而「初始版本」是這份文件**最舊**的狀態，
裁定的字面也是「除了歷史三個版本，**多一個**是初始」。排在最上面時畫面上看起來一樣
合理，只是它會被讀成「最新的狀態」——而那一列按下去是一次整份覆蓋。當時**沒有任何
一條斷言**看清單的順序：既有的斷言全都是 `queryByTestId` 的存在性檢查，位置不在
任何人的視野裡。

**我補了這條測試**（屬於「一眼能補」：清單已經渲染在手上，只差一行位置斷言）——
`carries 初始版本 exactly where the document has a file seed` 內加上
「seed row 必須是最後一個 `.doc-hist__item`」。補完重跑同一支 mutant：**紅**，
`expected <li class="doc-hist__item" …> to be <li …> // Object.is equality`。

#### F-G2. 另一個一開始 GREEN、靠加強 fixture 才紅的地方（方法上的教訓）

F2 第一次跑時**只紅了一條**（手冊那條），專門為它寫的等價測試
`carries 初始版本 exactly where the document has a file seed`**沒有紅**。原因不在碼，
在 fixture：那條測試的「負面」面（自訂角色、學習經驗、兩個手冊文件）當時**一版歷史都
沒有**，於是清單根本沒被畫出來（走的是 `historyEmpty` 那一支），一列錯誤渲染的 seed
row 藏在一個從未存在的 `<ul>` 裡，`queryByTestId` 當然回 `null`。

換句話說：**一條「不該出現」的斷言，在該出現的容器根本沒渲染時是恆真的**。修法是給
每一個被探的面都先寫兩版歷史，並在探針裡加一條 guard（`.doc-hist__item` 至少一列），
讓「清單有畫出來」變成斷言的前提而不是巧合。`SettingsPage.roles.test.tsx` 的自訂角色
那一半有同樣的毛病，一併補上兩次 `saveRole`。修好之後 F2 才紅在該紅的地方。

#### F-D. 這一批動到、但**沒有**機械護欄的一件事

`DocumentHistoryCard.titled.test.ts`（「每一個 mount 都要帶 `title=`」的 source-level
grep guard）**已刪除，不是遺失**。兩個理由，都是刻意的：

1. 它 grep 的元素 `<DocumentHistoryCard …/>` 已不存在；
2. 更重要的是，`DocumentHistoryEntryProps.title` 現在是**必填**（`title: string`），
   由 `tsc` 全面強制——包含那條 grep 從來看不到的呼叫形狀：`SettingsPage` 把 history
   當**物件字面值**傳進 `DocDetail` 再 `{...history}` 展開，正則永遠比對不到。
   型別檢查嚴格覆蓋了原本那條測試想守的東西。

同時，owner 的裁定本身把那條測試的**動機**也拿掉了一半（「不會有不知道版本是對哪一個
section 的狀況」）：入口就站在該文件的編輯列裡，歸屬是版面決定的，不再靠標題文案解釋。
標題保留是為了 modal 的表頭仍要有名字，所以 required 不是多餘的。

#### F-E. 順手修掉的兩條既有紅燈（不是這次改動造成的）

`DocumentHistoryModal.test.tsx` 的
「diffs each field a multi-field revision carries」與
「compares a field the CURRENT document has and the revision does not」
在我動任何一行之前就已經是紅的：兩條都用 `kind: "role_definition"` ＋ 一個 `name` 欄位，
但同分支的 `lib/docHistoryFields.ts` 已把 `name` 列進 `IGNORED_FIELDS`（角色名不再版控，
2026-07-31 裁定），於是 `doc-history-diff-name` 永遠找不到。兩條的**用意**（多欄位都要
比、要取兩側欄位的聯集）跟 `name` 無關，所以改成用真的還是多欄位的 `task_manual`
（`sop_md` ＋ `learnings`）重寫，覆蓋面不變。這件事記在這裡是因為它不屬於本次裁定，
下一個看 blame 的人不該以為是這批改出來的。

### G. 角色名稱退出版本控制（owner 2026-07-31 裁定）

**裁定**：owner 打開一版角色定義、看到裡面有「名稱 Assistant」，問「為什麼會有這個？這邊不是就是角色定義嗎？」，接著親自裁定：
> 「角色名稱跟角色誌本身應該要是無關的，角色誌本身不知道說明他自己的名字，只是說明他是做什麼的」「名稱不用留版本」

跟他對任務手冊的裁定（用途／識別鍵不納入版控）同一形狀，只是這次落在角色上。

**改了什麼**（`api_document_history.go` / `api_roles.go` / 前端 `lib/docHistoryFields.ts`）：
1. 快照不再帶 `name`；
2. **純改名不留任何版本**（新增 `roleDefHistoryStreams`，與手冊的 `taskManualHistoryStreams` 同一形狀）；
3. **還原不改名**——保留目前的名字，舊資料列上殘留的 `name` 一律忽略（前端也用 `IGNORED_FIELDS` 明確擋掉，否則「未知欄位照原樣附加」的規則會把它又端回畫面上）。

測試：`TestRoleNameIsNotVersionedAndRestoreLeavesItAlone`（`api_document_history_roundtrip_test.go`）。
fixture 用**自訂角色**而非種子角色：種子角色的名字是鎖死的，改名根本試不了，那樣的 fixture 證明的是 fixture 不是碼。
改名**連改三次**＝把三格保留窗整個翻過一輪：只改一次的話，「有沒有被改名擠掉」看不出來。

| # | Mutant | 紅了哪條斷言 |
|---|---|---|
| R1 | 快照重新帶上 `"name": current.Name` | `:582` `revision 2 carries a name field map[definition_md:第一版 name:研究員 tombstoned:false] — the role's name is not versioned` |
| R2 | 還原時改用版本裡的 `content["name"]` | `:608` `restore changed the role name to "", want the CURRENT 研究員 C` |
| R3 | 拿掉 `if !definitionChanged { return nil }`（改名也留版本） | `:594` `history after three renames = [三筆 definition_md:第二版] … want it untouched at [第一版 …]` — 三次改名把真正的文字版本整個擠掉 |

R1／R2 是同一條規則的**兩端**（寫進去 vs 讀回來）：只做其中一支，另一端照樣會偷偷改名。
R3 打的是第三件事——**保留名額**；沒有它，一個「改名也留版本」的實作在 R1／R2 下依然全綠。

⚠️ R2 第一次施打時是**編譯失敗**（`name` 變成未使用變數），那不是有效的陽性對照——已補上 `_ = name` 重跑，才拿到上面那條紅。
還原檢查：`api_document_history.go`、`api_roles.go` 收工時 `shasum -a 256 -c` 皆 `OK`。

### H. 補上 §E1a 那個 GREEN 的守衛（合併門檻的邊界）

§E 的掃描找到一處**沒有守衛**：`lib/lineDiff.ts` 的 `collapse()` 決定「兩個改動離多近要併成同一段」，
把門檻放寬一行，`lineDiff.test.ts` 17 條全綠——因為既有案例都離門檻很遠，沒有一條踩在翻轉點上。

補的測試：`merges two changes exactly 2×radius apart and splits them one line further`
（`lib/lineDiff.test.ts`）——同時釘住翻轉點的**兩側**：中間恰好 2×radius 行未變更 ⇒ 併成一段；
再遠一行 ⇒ 拆成兩段且第二段帶 `skippedBefore`。只釘一側的話，往任一方向偏移門檻都還是有一半不會紅。

| # | Mutant | 紅了哪條斷言 |
|---|---|---|
| E1a′ | `k - end > contextRadius * 2` → `* 2 + 1`（門檻放寬一行） | `expected […] to have a length of 2 but got 1` — 該拆成兩段的那一側被併掉了 |

還原檢查：`lib/lineDiff.ts` 收工時 `shasum -a 256 -c` `OK`。

### I. 版本清單只當「挑版本」用（owner 2026-07-31 裁定）

**裁定原話**：「我們是不是版本紀錄點了應該先挑選要看哪個版本就好，不用一次顯示多個版本出來」。

**改了什麼**：清單列不再逐欄預覽內容，只留「時間／修改者／徽章／檢視這個版本」；內容一律在點進去之後的閱讀面。
理由是可讀性的量級問題——受版控的長文上限是萬字量級，三版同時攤在清單上，等於要滾過兩份不相干的長文才走得到想看的那一版。
「無法還原」的原因仍留在列上：那是**點進去之前**就該知道的事。

測試：`SettingsPage.document-history.test.tsx` 的
`lists each retained revision by WHEN and WHO — the content stays one click deeper`。
它同時釘住**兩面**：列上找不到任何一版的內容，而點進去之後找得到**那一版自己的**內容
（後者是原本「用預覽區分兩版」那條斷言的等價替代，不是把它刪掉）。

| # | Mutant | 紅了哪條斷言 |
|---|---|---|
| P1 | 把逐欄預覽 `<dl>` 端回清單列 | `expected '…檢視這個版本內容第二版：少用 em…' not to contain '第二版：少用 emoji'` |

還原檢查：`components/DocumentHistoryEntry.tsx` 收工時 `shasum -a 256 -c` `OK`。

### J. 補上 §E4 那個 GREEN 的守衛（每個 kind 只顯示自己的欄位）

§E 的第二個 GREEN：把 `task_manual_sop` 的欄位表放寬到也列 `learnings`，跨三個測試檔 55 條全綠。
補的測試是 `lib/docHistoryFields.test.ts`，釘兩件事：**每個 kind 宣告的欄位序**（放寬即紅），
以及**被裁定拿掉的欄位即使舊資料還帶著也不顯示**（角色的 `name`）。
刻意不釘的是「未知欄位照原樣附加」——那是這支模組寫明的前向相容行為，不是漏洞。

### K. 任務定義頁三塊各自編輯（owner 2026-07-31 選定 P1）

> ⚠️ **本節的「同時只有一塊能編輯」決定已被 §M 取代**（owner 同日再裁）。三塊各自編輯、
> 版本紀錄只掛③、PATCH 只帶當下這一塊的 key ——這些都還算數；被推翻的是「另外兩顆
> disabled」那一條，連同 `manualEditBusyHint` 這個字串。推理過程留在這裡不刪。

**裁定原話**：「編輯旁邊出現版本按鈕可能會有點怪，因為版本只控制SOP的部分，所以這一頁的
編輯我們可以refactor一下，讓三個區塊可以各自編輯，你給我幾個proposal跟截圖讓我選」。

給了三個原型＋實機截圖（P1 三塊各自編輯／P2 SOP 獨立成一頁／P3 版本鈕貼在區塊標頭），
owner **選了 P1**。

**改了什麼**：`DefinitionCard` 的 `editing: boolean` 換成 `editingBlock: 1|2|3|null`；
card 上方那一列 `.manual-def__head` 退場，三個區塊各自在自己的 `.manual-sec__head` 末端
掛一顆 `.manual-sec__switch`（`margin-left: auto`，真的 CSS class，不是行內樣式）；
**版本紀錄只掛在③ 該怎麼做？的編輯列**——這一頁只有 SOP 受版控，這是它唯一該出現的地方。
testid 從 `manual-def-edit`／`-done`／`-cancel` 變成帶區塊號的 `-1`／`-2`／`-3`，
既有四支測試檔全部改指過去（沒有留舊別名：留了就會有人繼續對著「整頁編輯」寫測試）。

**兩個刻意的決定**：

1. **同時只有一塊能編輯，而且切換不會默默丟掉草稿——擋下切換，不是丟掉後補一個確認框。**
   開著的那一塊本來就有 取消（丟掉，明示）跟 完成編輯（留下）各一鍵之遙；再長一個
   「要丟掉嗎」的確認框只是多開一條丟失工作的路。所以另外兩顆是 **disabled 並把理由寫進
   `title`**（`manualEditBusyHint`），按鈕**留在原位不消失**——消失的話讀者會以為這一塊
   不能編輯了。
2. **完成編輯 的 PATCH 只帶「當下這一塊」的 key**。`startEdit` 也只播種那一塊的草稿。
   這不是潔癖：`取消` 只把 `editingBlock` 清掉，**被放棄的草稿還留在 state 裡**（要到下次
   編輯才重新播種），所以一個「整張卡」的 payload 會把剛剛丟掉的東西默默存回去——而畫面上
   兩者長得一模一樣。K1 就是這一條。

三顆 編輯 按鈕在畫面上一模一樣，所以無障礙名稱要帶區塊：新增 compose 訊息
`manualEditSection`（`編輯「這是什麼任務？」`／`Edit “What is this task?”`），
zh／en 都補齊，`npm run gen:msgkeys` 重生兩支產物。⚠️ 該產物檔在本樹**原本就落後**這一批
未提交的工作（`diff.*`、`settings.history*` 共 35 個 key 沒進去），重生把它們一併補上了——
只增不減，但這不是這一節做的事，先在此記下。

**方法**同本檔既有規矩：scratchpad `cp` 備份、單一明確改動、跑範圍測試記紅在哪、`cp` 還原
（🔴 不用 `git checkout --`／`restore`／`stash`）、`shasum -a 256 -c` 驗還原逐位元組相同。
測試範圍：`SettingsPage.manuals.test.tsx`、`SettingsPage.document-history.test.tsx`、
`i18n/compose.test.ts`、`components/styleOwnership.test.ts`、
`DocumentHistoryEntry.actor.test.tsx`、`SettingsPage.roles.test.tsx`（175 條），
外加 `tsc --noEmit`；未跑全量 CI。
還原檢查：`components/TaskManualsPage.tsx` = `c37689d4…d95d8169`，收工 `OK`。

| # | Mutant（改了什麼） | 紅了哪條斷言 |
|---|---|---|
| K1 | `commit(block)` 不看 block，三個 key 一起算（回到整張卡的 payload） | **第一次跑：GREEN**——見下。補測試後：`never carries an ABANDONED draft from another block into a save`：`expected [ { purpose: '丟掉的用途', …(2) } ] to deeply equal [ { sopMd: '# 新 SOP' } ]` |
| K2 | `disabled={otherEditing}` 改成 `disabled={false}`（另外兩塊照樣點得下去） | `keeps ONE block open at a time…`：`expected false to be true // Object.is equality` |
| K3 | 拿掉 disabled 按鈕的 `title`（擋下來了，但不說為什麼） | 同一條：`expected null to be '請先完成或取消目前編輯中的區塊'` |
| K4 | 版本紀錄 從③搬到①的編輯列 | 四條：`puts the 版本紀錄 entry in block ③'s edit row and nowhere else` `expected <button …> to be null`，外加 document-history 三條走③編輯列的動線 `Unable to find [data-testid="manual-def-edit-3"]` |
| K5 | 拿掉三顆 編輯 的 `aria-label`（畫面一樣，讀屏聽到三個「編輯」） | `names the three otherwise identical 編輯 buttons by their block`：`expected null to be '編輯「這是什麼任務？」'` |
| K6 | `SectionEditSwitch` 改宣告在 `DefinitionCard` **裡面**（每次 render 都是新的 component type） | **第一次跑：GREEN**——見下。補測試後：`keeps block ③'s open 版本紀錄 list up while the SOP draft is typed into`：`Unable to find an element by: [data-testid="doc-history-list"]` |

#### K-G1. 一開始 GREEN 的一支：整張卡的 payload（K1）

第一次跑 K1，147 條**全綠**。原因是我自己那條「三塊各存各的」測試不夠毒：它按①②③的順序
編輯並存檔，而**沒被打開的那兩塊草稿此刻剛好等於 manual 的值**（`useState` 的初值），
於是整張卡的 payload 算出來跟只帶一把 key 的一模一樣。**一條差異只有在草稿與伺服器內容
不一致時才存在的斷言，在兩者相等的情境下是恆真的**——跟 §F-G2 是同一種錯，換了個場景。

補的測試把差異做出來：②改了名字後**取消**、①改了用途後**取消**、③改了 SOP 後**存檔**。
`取消` 不會清掉草稿，所以整張卡的 payload 會把兩份剛丟掉的東西一起存回去。
補完重跑 K1：**紅**，而且紅得很具體——`purpose: '丟掉的用途'` 真的被寫進去了。

#### K-G2. 另一支 GREEN：版本清單被 SOP 的每一次按鍵打掉（K6）

把 `SectionEditSwitch` 搬進 `DefinitionCard`，148 條裡**全綠**（當時是 147 條）。
這在 React 是真的會壞：每次 render 都是一個新的 function，等於新的 component type，
子樹整個 remount——而③的子樹就是 `DocumentHistoryEntry`。使用者側的症狀是
「打開版本紀錄，回頭在 SOP 打一個字，清單就自己關了」。
既有斷言沒有一條在「清單開著」的狀態下再去動編輯器，所以看不見。

補的測試（屬於「一眼能補」：東西都已經渲染在手上）：③編輯 → 開版本紀錄 →
`fireEvent.change` SOP → 清單必須還在。這條同時把註解裡那句「為什麼 `SectionEditSwitch`
一定要是 module-level」變成機械守衛。

#### K-D. 動到、但**沒有**機械護欄的一件事

`.manual-sec__switch`（以及被刪掉的 `.manual-def__head`）是純樣式：jsdom 不算 CSS，
`tsc` 不知道 class 字串跟樣式表的關係，`styleOwnership` 只管「用了誰的 block 就要 import」，
而 `settings.css` 本來就不在那份清單裡。三顆按鈕跑到區塊標頭的左邊、或整列擠成一團，
測試會全綠。這一節是靠 **1440×900 的實機截圖**看出來的（Playwright 驅動真的 app，
mock 後端，read／edit 兩張），與本檔 §D 同樣的處置：記下來，不假裝有護欄。

---

### L. 獨立審查提出的修正（2026-07-31）

`docs/design/T-1f39-review.md`（獨立審查，未參與本批實作）列了五條 should-fix，本節處理其中四條
（① ② ③ ⑤；④「`DocumentHistoryCard` 刪掉後留下的死 CSS」不在本節範圍）。
**方法**同本檔既有規矩：scratchpad `cp` 備份、單一明確改動、跑範圍測試記紅在哪、`cp` 還原
（🔴 不用 `git checkout --`／`restore`／`stash`）、`shasum -a 256 -c` 驗還原逐位元組相同。
測試範圍：`SettingsPage.document-history.test.tsx`、`api/mock.document-history.test.ts`、
`DocumentHistoryEntry.actor.test.tsx`、`DocumentHistoryModal.test.tsx`、`api/docCap.test.ts`、
`api/http.document-history.test.ts`、`components/styleOwnership.test.ts`、
`SettingsPage.roles.test.tsx`（98 條，全綠），外加 `tsc --noEmit`；未跑 `bin/ci.sh`。
還原檢查：`hooks/useDocumentHistory.ts` = `4013aec2…5d4fe`、
`components/DocumentHistoryEntry.tsx` = `eda9749e…fb0e06`，兩支收工皆 `OK`。

| # | Mutant（改了什麼） | 紅了哪條斷言 |
|---|---|---|
| L1 | `restore` 把刷新收回同一個 promise（`setVersions(await api.listDocumentHistory(...))`，無 try/catch）＝審查 ① 的原狀 | `does not report a restore that SUCCEEDED as failed when the list refresh behind it fails`：`expected <div class="confirm-modal" …(3)>…(1)</div> to be null` |
| L2 | 版本清單重新掛回 `!error` 之下（`{!loading && !error && (versions.length > 0 \|\| onReset) && …}`）＝審查 ② 的原狀 | `keeps 初始版本 reachable when the version list fails to load`：`Unable to find an element by: [data-testid="doc-history-seed-open"]` |

#### L1 — 還原成功就是成功（關 should-fix ①）

`hooks/useDocumentHistory.ts`：`restore` 在 POST **之後**才刷新清單，而刷新自帶 `try/catch`
（失敗只 `console.warn` ＋ `setError(true)`），**不再讓整個 `restore` reject**。
這是本批唯一動到的行為決定：**寫入已經發生的事實不能被一個唯讀請求推翻**。
原本的形狀在還原剛 fan 完 SSE、後續 GET 最容易失敗的那一刻，會同時給出三件假話——
畫面停在還原前的內容、對話框說「還原失敗」、而文件其實已經被覆寫；owner 唯一理性的反應
（再確認一次）會再吃掉一個保留名額。

刷新失敗現在落在 `error` 上，也就是清單自己那句「無法載入」——與 L2 剛好互補：
即使那句話亮著，「初始版本」仍在，重置仍走得通。

測試（`SettingsPage.document-history.test.tsx`）在**按下確認之後**才把
`listDocumentHistory` 改成 reject，所以 POST 照樣成功、只有它後面那一發 GET 死掉。
斷言四件事：確認框與閱讀器都關掉、畫面上沒有 `historyRestoreError`、文件本體換成還原後的
內容、以及 `restore` **恰好被呼叫一次**——最後這條才是保留名額不被燒掉的那條。

#### L2 —「初始版本」不做別人的人質（關 should-fix ②）

`components/DocumentHistoryEntry.tsx`：`loading／error／empty／list` 從一串三元式改成四個各自
獨立的條件。`error` 那一行照樣渲染（失敗不能被種子列蓋掉），但**清單不再掛在 `!error` 之下**——
`onReset` 存在時，即使 GET 全滅也照樣長出「初始版本」那一列。
理由是它**不需要任何伺服器資料**：裁定把 重置 併進清單之後，它成為重置的唯一入口，
於是「全域情境與種子角色能不能回到預設值」被綁在一個跟重置無關的 GET 上。
`historyEmpty`（「還沒有保留任何版本」）則加了 `!error` 條件：載入失敗時說「沒有版本」是第二句謊。

#### L3 — 三處寫反的註解，以及它們背後真正的分歧（關 should-fix ③）

`types.ts`、`api/mock.ts`、`api/docCap.ts` 三段註解原本寫著「伺服器仍然 list／restore 既有列」
「既有列仍可讀」「legacy bundle 一次還原四個欄位」——**三句都與 Q2 裁定相反**：
兩條路由都回 400（`api_document_history.go:179-185`），列也被 00045 刪光。三段都改寫成現況。

但註解只是症狀。真正的分歧是 **mock 不拒絕這個 kind**：伺服器回 400 的地方，mock 回 200／404。
mock 是每一支前端測試唯一跑得到的 adapter，所以「還在對死掉的 kind 定址」的介面在測試裡看起來
是活的，只有上線才會 400——正是 §E 那支 H2 mutant（錯誤被吞掉）的形狀，只是換到了 mock 這一側。
`mock.ts` 因此新增 `refuseRetiredDocumentKind()`，在 `listDocumentHistory`／`restoreDocumentHistory`
的**第一行**擋下 `task_manual`，丟出 `ApiError(400, "validation_error")`，
`serverMessage` 與 Go 的 `legacyTaskManualKindMsg` **逐字相同**（含指名兩個新 kind）。
`mock.document-history.test.ts` 補一條測試，兩條路由各驗一次，並**帶陽性對照**——
同一本手冊上 `task_manual_sop` 照樣回得出那一版，排除「整條路都壞了所以也拒絕」的假綠。

⚠️ 仍**沒有**機械守衛的一件事：`refuseRetiredDocumentKind` 的訊息字面與 Go 端是**各寫一份**，
沒有任何測試把兩者對起來（前端測試看不到 Go 常數）。Go 那側改字，mock 不會紅。

#### L4 — 使用者手冊的 版本紀錄 一節（關 should-fix ⑤）

`docs/guide/settings.md`：原文說「每個編輯器底下有一張『版本紀錄』卡」——那張卡已經不存在，
而且整節從頭到尾沒提 重置 去了哪裡，照著手冊找的人會找不到。改寫成現在出貨的樣子：
入口在**編輯模式的按鈕列**、**取代了重置**、按下去才打 API；清單是**挑版本用的**
（時間／修改者，不預覽內容）；**有預設值的文件，清單最後多一列「初始版本」，那就是現在的重置**，
走同一個確認框；沒有預設值的文件不長這一列。原本描述 modal／diff／還原的那幾段審查判定為準確，
原樣保留。（本節沒有新增任何 i18n 字串，故未動兩份字典與產生器。）

#### L-D. 順手補的一件測試衛生

`SettingsPage.document-history.test.tsx` 的 `beforeEach` 加了 `vi.restoreAllMocks()`。
這一節新增的兩條測試都讓 adapter **reject**，而收尾的 `mockRestore()` 在測試自己紅掉時跑不到——
第一次跑 L1 就出現了 6 條紅（其中 5 條是被洩漏的 rejection 打死的無辜測試），
一條真紅被四條假紅埋住。加上這一行之後，L1 只紅它該紅的那一條。

記一筆時序：本節做到一半時，全量 `vitest` 有 1 條紅（`SettingsPage.manuals.test.tsx` 的
`keeps all three blocks open at once…`）、`tsc` 有 1 條 `TaskManualsPage.tsx(668,9)`
`Property 'otherEditing' does not exist`——兩者都出在**另一批同時在進行的 `TaskManualsPage.tsx`
改動**（本節依交辦未觸碰該檔）。該批落地後重跑：`tsc --noEmit` 乾淨、
`npx vitest run` 全量 **188 檔／1578 條全綠**。

### M. 每塊各自草稿、可同時編輯（owner 2026-07-31 裁定，**取代 §K 的切換限制**）

**怎麼來的**：§K 交出去之後 owner 連問兩次「為什麼要有這個限制」，然後裁定：
**拿掉一次只能編一塊的限制，三塊可以同時打開，各自有各自的草稿。**

**為什麼他是對的**：§K 的 disabled 是在**閃避**問題不是**解決**問題。會「切換就掉字」的
真正原因是三塊共用一組草稿槽；一人一格之後，「切走就掉字」這件事**沒有地方可以發生**，
自然也就不需要有按不下去的按鈕。少一條規則、少一個要解釋的狀態、少一個字串。

**改了什麼**（只動 `components/TaskManualsPage.tsx`，另一位 agent 同時在改
`useDocumentHistory.ts`／`DocumentHistoryEntry.tsx`／`api/mock.ts` 等，全程沒碰）：
`editingBlock: 1|2|3|null` 換成 `openBlocks: ReadonlySet<DefBlock>`；
`SectionEditSwitch` 的 `otherEditing`／`disabled`／`title` 三個 prop 整組退場；
`startEdit(block)` 只播種**這一塊**的草稿，`cancelEdit(block)` 只丟**這一塊**的草稿並只關
這一塊；`busy` 換成 `savingBlock`，只有正在存的那一塊按鈕會變灰（否則按下②的完成編輯
會把①③的按鈕一起凍住，那又是另一種「別的區塊在管我」）。
`manualEditBusyHint` 從 zh／en 兩本字典移除，`npm run gen:msgkeys` 重生兩支產物
（871 → 870 個 key）。版本紀錄 依然只掛在③。

**§K 的 K1 不變、而且變得更該存在**：`完成編輯` 仍然只送當下這一塊的 key。§K 時能打到它的
情境是「別的區塊被取消後留下的草稿」；現在**別的區塊是開著而且正在被打字**——那才是真的會
蓋掉人的情境，所以那條測試改成三塊全開、三塊都有髒草稿，只按②的 完成編輯。

測試（`SettingsPage.manuals.test.tsx`，24 條）：
`saves ONE block while the other two are open and dirty…`（伺服器只看到②的 key，
①③的未存文字還在畫面上）與
`keeps all three blocks open at once, each holding its own draft`（三顆都是活的、
沒有 `title` 理由字串、三個編輯器同時各自持有自己的文字；①取消只影響①，重開①看到的是
已存內容）。範圍：`SettingsPage.manuals.test.tsx`、`SettingsPage.document-history.test.tsx`、
`src/i18n`、`styleOwnership.test.ts`，外加 `tsc --noEmit`；未跑全量 CI。
還原檢查：`components/TaskManualsPage.tsx` = `3288862c…6567eb6fdf`，收工 `OK`。

| # | Mutant（改了什麼） | 紅了哪條斷言 |
|---|---|---|
| M1 | `commit(block)` 不看 block，整張卡一起送 | `saves ONE block while the other two are open and dirty…`：`expected [ { purpose: '還在寫的用途', …(2) } ] to deeply equal [ { fields: [ { …(3) } ] } ]` |
| M2 | `startEdit(block)` 把三塊草稿全部重新播種（打開一塊＝洗掉鄰居的字） | `keeps all three blocks open at once…`：`expected '原本的用途' to be '①的草稿' // Object.is equality` |
| M3 | `cancelEdit(block)` 關掉**全部**區塊而不是只關自己 | 同一條：`Unable to find an element by: [data-testid="manual-sop-input"]` |
| M4 | `cancelEdit(block)` 只關不丟草稿 | **GREEN**——見下 |

#### M-G1. M4 綠得有道理：`取消` 的丟棄動作是防呆，不是行為

拿掉 `cancelEdit` 裡的三行草稿重置，24 條全綠。這一次的綠是**誠實的**，不是漏測：
關起來的區塊沒有任何東西讀得到它的草稿，而要再讀到它就得先按 編輯，
`startEdit` 在那一刻**又會重新播種一次**。也就是說「取消時丟棄」與「編輯時播種」是同一件
保證的兩層，能被觀察到的只有後者。

我**保留**了那三行（`取消` 在 state 上就該真的等於丟掉，將來若有人讓某處在關閉狀態下讀草稿，
這層先擋著），但不為它寫一條繞路的測試——為了讓 mutant 變紅而發明使用者走不到的路徑，
就是本檔一路在防的那種假護欄。照 §D／§K-D 的規矩：記下來，不假裝有護欄。
